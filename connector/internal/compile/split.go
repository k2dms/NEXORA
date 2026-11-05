// Stage 2 of docs/architecture/27-config-compilation-pipeline.md §2: split — "each rule → browser plane | core plane |
// unrepresentable". The policy arrives already split (routing-policy.schema.json makes `browserPlane` and `corePlane`
// required, and docs/architecture/28-routing-model.md §2 says exactly one plane decides), so what is left for the
// compiler is the core plane's half: turn each `coreRule` into an Xray routing rule, or report it.
//
// Two things this file does NOT do, and the reasons are load-bearing:
//
//  1. it does not lower browser-plane rules into core rules. docs/architecture/28-routing-model.md §4 promises
//     lowering for four of the twelve condition types, but a rule that is not lowered is still evaluated by the
//     browser, so skipping the optimization fails open in no direction;
//  2. it does not invent an emitted spelling the corpus never shows. §8's "may not invent an outbound tag" is about
//     tags; the same discipline applies to bytes. Where the contract names a field the compiler cannot place, the rule
//     is reported, because doc 27 §7 rule 1 says "report, never drop" and rule 3 says a report is not an error.
package compile

import (
	"fmt"
	"strings"
)

// The three bounds of the report, taken from the two places they are declared identically:
// `packages/schema/routing-policy.schema.json#/properties/corePlane/properties/unrepresentable` and
// `packages/schema/protocol/commands/config.apply.json#/result/properties/unrepresentable`.
const (
	maxReports      = 256
	maxReportSource = 200
	maxReportReason = 200
)

// defaultDomainStrategy is the value the three canonical shapes of docs/architecture/23-unified-server-model.md §9
// emit when no policy is supplied — all three show `"routing": {"domainStrategy": "AsIs", "rules": []}`. It is NOT
// the schema's default: `IPIfNonMatch` (doc 27 §6) is what a *policy* that omits the field means, and
// `routing-policy.schema.json` makes the field required, so a policy always states one. The no-policy case is the
// only one this compiler decides, and it decides it by copying the shape it is required to reproduce.
const defaultDomainStrategy = "AsIs"

// Routing is stage 2's output: the parts of the `routing` section, kept apart from the section itself so the
// assembler stays the only place the document's shape is decided, plus the report the apply result must carry.
type Routing struct {
	DomainStrategy  string
	Rules           []Document
	Unrepresentable []Report
}

// Split turns the core plane into Xray routing rules, in policy order.
//
// Order is preserved because a core rule's position is its priority: docs/architecture/28-routing-model.md §8 lists
// "reorder core rules for matching efficiency" among the things the compiler *may* do, and §9's row
// "Lowering a rule never changes its meaning" is still owed a check, so the permitted optimization is the one that
// needs no proof — emit them in the order the user wrote them.
//
// `tags` is stage 1's output: a rule may name only a tag the config actually emits (`#/$defs/coreOutbound` allows
// `direct`, `block` or a derived `srv-<8 hex>`, and JSON Schema cannot check membership against a sibling document).
func Split(policy *RoutingPolicy, tags []string) (*Routing, error) {
	out := &Routing{DomainStrategy: defaultDomainStrategy}
	if policy == nil {
		return out, nil
	}
	out.DomainStrategy = policy.CorePlane.DomainStrategy

	// The policy's own report is authored data about this same compile, so it belongs in the apply result too:
	// doc 27 §7 states the field twice on purpose, "so that both the policy author and the operator see it".
	for _, r := range policy.CorePlane.Unrepresentable {
		out.Unrepresentable = append(out.Unrepresentable, r)
	}

	if enabled := policy.CorePlane.Sniffing.Enabled; enabled != nil && *enabled {
		// Xray's `sniffing` is an inbound setting, not a routing one, and no document in this repository says where
		// the compiler puts it — doc 27 §6 says only that `routeOnly: true` is required by the one-plane rule. A
		// section this file guessed the position of would be a routing control silently not in force. ODEC-37.
		out.Unrepresentable = append(out.Unrepresentable, Report{
			Source: "corePlane.sniffing",
			Reason: "the emitted position of sniffing is unstated (routing section or per-inbound settings); ODEC-37 decides it",
		})
	}

	for i, rule := range policy.CorePlane.Rules {
		if !isCoreOutbound(rule.Outbound) {
			// `#/$defs/coreOutbound` is `^(direct|block|srv-[0-9a-f]{8})$`. Reaching here means the payload broke a
			// pattern the schema states, which is a caller bug rather than a routing decision, so it is an error.
			return nil, fmt.Errorf("compile: routing policy %s rule #%d names the outbound %q, which #/$defs/coreOutbound does not admit",
				policy.ID, i, rule.Outbound)
		}
		if !strings.HasPrefix(rule.Outbound, "srv-") {
			// Checked first because it is the deeper limit: an `outboundTag` Core cannot resolve is not a rule that
			// lost a field, it is a rule with no destination. `freedom` and `blackhole` — the protocols behind these
			// two tags — appear nowhere in this repository, which is ODEC-37's first item.
			out.Unrepresentable = append(out.Unrepresentable, Report{
				Source: fmt.Sprintf("corePlane rule #%d", i),
				Reason: "outbound " + rule.Outbound + " is a built-in tag whose emitted outbound is undecided; ODEC-37",
			})
			continue
		}
		if !contains(tags, rule.Outbound) {
			return nil, fmt.Errorf("compile: routing policy %s rule #%d sends traffic to %s, which no selected server emits; the compiler may not invent an outbound tag (doc 28 §8)",
				policy.ID, i, rule.Outbound)
		}
		if undecidable := undecidedMatch(rule); undecidable != "" {
			// The rule has a match field whose emitted form no document states; report it whole rather than emit a
			// rule missing that field, which would match more traffic than the user asked for.
			out.Unrepresentable = append(out.Unrepresentable, Report{
				Source: fmt.Sprintf("corePlane rule #%d", i),
				Reason: rule.reasonFor(undecidable),
			})
			continue
		}
		out.Rules = append(out.Rules, rule.section())
	}

	if len(out.Unrepresentable) > maxReports {
		// Truncation would be doc 27 §7's forbidden drop wearing a bound, so the config is refused instead and
		// `packages/schema/protocol/commands/config.apply.json` keeps its maxItems promise.
		return nil, fmt.Errorf("compile: routing policy %s produces %d unrepresentable entries, past the %d the report may carry",
			policy.ID, len(out.Unrepresentable), maxReports)
	}
	for _, r := range out.Unrepresentable {
		if err := bound(r, policy.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// section is the model→Xray rename doc 28 §7's rank-5 row requires: the field the user edits is `outbound`
// (`#/$defs/coreRule/properties/outbound`) and the byte Core reads is `outboundTag`. Every other name is the same in
// both, so it passes through — `domain` and `ip` stay arrays (routing.two-plane.json puts a `geosite:` selector in
// `domain`, which is Xray's own syntax and therefore verbatim by intent), `port` stays one string of the same
// `^\\d+(-\\d+)?$` shape, and `invert` stays a boolean.
func (r CoreRule) section() Document {
	rule := Document{"outboundTag": r.Outbound}
	if len(r.Domain) > 0 {
		rule["domain"] = r.Domain
	}
	if len(r.IP) > 0 {
		rule["ip"] = r.IP
	}
	if r.Port != "" {
		rule["port"] = r.Port
	}
	if r.Invert != nil {
		rule["invert"] = *r.Invert
	}
	return rule
}

// undecidedMatch names a match field whose emitted form the corpus does not state, or "" when every field the rule
// carries has a spelling shown somewhere. `network` and `protocol` are the two fields the model holds as arrays and
// Xray's routing documentation holds as one comma-joined string; no file in this repository shows which, so a
// compiler that joined them invented a serialization and one that passed an array through invented a type.
func undecidedMatch(r CoreRule) string {
	switch {
	case len(r.Network) > 0 && len(r.Protocol) > 0:
		return "network and protocol"
	case len(r.Network) > 0:
		return "network"
	case len(r.Protocol) > 0:
		return "protocol"
	}
	return ""
}

// reasonFor keeps a composed reason under the schema's bound and says what the missing decision is. The rule's match
// *values* never appear: doc 27 §7 rule 2 says a report is logged, and a `coreRule` has no name field to report
// instead, so the only identifier the compiler composes is the rule's position.
func (r CoreRule) reasonFor(field string) string {
	return "the emitted form of " + field + " is unstated (array or comma-joined string); ODEC-37 decides it"
}

// isCoreOutbound is `#/$defs/coreOutbound`'s pattern, re-checked here because the compiler reads payloads the schema
// validated elsewhere and doc 27 §2 stage 7 inspects the compiled result rather than trusting its input.
func isCoreOutbound(s string) bool {
	if s == "direct" || s == "block" {
		return true
	}
	const prefix = "srv-"
	if len(s) != len(prefix)+8 || !strings.HasPrefix(s, prefix) {
		return false
	}
	for _, c := range s[len(prefix):] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// bound is the 200-character rule of both `unrepresentable` declarations, enforced rather than truncated: an entry
// whose reason was cut to fit is a report the operator cannot act on.
func bound(r Report, policyID string) error {
	if len(r.Source) == 0 || len(r.Source) > maxReportSource {
		return fmt.Errorf("compile: routing policy %s reports an unrepresentable entry whose source is %d characters; the bound is 1-%d",
			policyID, len(r.Source), maxReportSource)
	}
	if len(r.Reason) == 0 || len(r.Reason) > maxReportReason {
		return fmt.Errorf("compile: routing policy %s reports %q with a reason of %d characters; the bound is 1-%d",
			policyID, r.Source, len(r.Reason), maxReportReason)
	}
	return nil
}
