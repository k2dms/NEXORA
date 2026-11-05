package compile

import (
	"fmt"
	"strings"
	"testing"
)

// policyWith is the smallest policy that exercises stage 2: one core plane, the rules in the order they are written.
// The id is a real UUID because Split names it in every refusal.
func policyWith(rules ...CoreRule) *RoutingPolicy {
	return &RoutingPolicy{
		ID:          idPolicy,
		DisplayName: "test policy",
		CorePlane: CorePlane{
			DomainStrategy: "IPIfNonMatch",
			Rules:          rules,
		},
	}
}

func boolPtr(b bool) *bool { return &b }

var oneTag = []string{"srv-3f2a1c9e"}

// A policy with no rules is the shape doc 23 §9's three illustrations end in, and the empty list is emitted as an
// array, not omitted: doc 28 §7's rank-5 row depends on the routing section always being present.
func TestAnEmptyCorePlaneStillEmitsASection(t *testing.T) {
	got, err := Split(policyWith(), oneTag)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if got.DomainStrategy != "IPIfNonMatch" {
		t.Errorf("domainStrategy = %q; the policy states it and doc 28 §8 says the compiler emits it from the policy", got.DomainStrategy)
	}
	if len(got.Rules) != 0 || got.Unrepresentable != nil {
		t.Errorf("empty plane produced %v rules and %v reports", got.Rules, got.Unrepresentable)
	}

	none, err := Split(nil, oneTag)
	if err != nil {
		t.Fatalf("Split(nil policy): %v", err)
	}
	if none.DomainStrategy != defaultDomainStrategy {
		t.Errorf("no policy gave domainStrategy %q, want the value doc 23 §9's shapes show", none.DomainStrategy)
	}
}

// `#/$defs/coreOutbound` admits three shapes and only one of them names something the compiler emits today.
func TestABuiltinOutboundIsReportedNotInvented(t *testing.T) {
	for _, builtin := range []string{"direct", "block"} {
		got, err := Split(policyWith(CoreRule{Domain: []string{"example.net"}, Outbound: builtin}), oneTag)
		if err != nil {
			t.Fatalf("Split(%s): %v", builtin, err)
		}
		if len(got.Rules) != 0 {
			t.Errorf("%s compiled to %v; the emitted outbound protocol behind the tag is ODEC-37's", builtin, got.Rules)
		}
		if len(got.Unrepresentable) != 1 {
			t.Fatalf("%s produced %d reports, want 1", builtin, len(got.Unrepresentable))
		}
		r := got.Unrepresentable[0]
		if r.Source != "corePlane rule #0" {
			t.Errorf("source = %q; a core rule has no name field, so the position is the only identifier that leaks nothing", r.Source)
		}
		if !strings.Contains(r.Reason, builtin) || !strings.Contains(r.Reason, "ODEC-37") {
			t.Errorf("reason %q names neither the tag nor the decision that owes it", r.Reason)
		}
		if len(r.Reason) > maxReportReason || len(r.Source) > maxReportSource {
			t.Errorf("the composed report is past the bound the schema declares: %q", r.Reason)
		}
	}
}

// A rule that matches on `network` or `protocol` cannot be placed either: the model holds both as arrays and no file
// in this repository shows whether Xray's routing rule takes an array or one comma-joined string.
func TestAUndecidedMatchFieldReportsTheWholeRule(t *testing.T) {
	got, err := Split(policyWith(
		CoreRule{Network: []string{"udp"}, Port: "53", Outbound: "srv-3f2a1c9e"},
		CoreRule{Protocol: []string{"bittorrent"}, Outbound: "srv-3f2a1c9e"},
		CoreRule{Network: []string{"tcp", "udp"}, Protocol: []string{"http"}, Domain: []string{"example.net"}, Outbound: "srv-3f2a1c9e"},
	), oneTag)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(got.Rules) != 0 {
		t.Errorf("a rule with an undecided field was emitted as %v; half a rule matches more traffic than the user wrote", got.Rules)
	}
	if len(got.Unrepresentable) != 3 {
		t.Fatalf("got %d reports, want 3", len(got.Unrepresentable))
	}
	for i, want := range []string{"network", "protocol", "network and protocol"} {
		if !strings.Contains(got.Unrepresentable[i].Reason, want) {
			t.Errorf("report #%d does not name %s: %q", i, want, got.Unrepresentable[i].Reason)
		}
	}
}

// doc 28 §7's rank-4 row and doc 36 §6's `rank: 5` event both assume every policy rule arrives with an `outboundTag`.
// The model's field is `outbound`, so the rename is what stage 2 owns.
func TestARepresentableRuleRenamesOutboundAndKeepsEveryOtherName(t *testing.T) {
	got, err := Split(policyWith(
		CoreRule{Domain: []string{"geosite:category-ads-all"}, Outbound: "srv-3f2a1c9e"},
		CoreRule{IP: []string{"10.0.0.0/8"}, Port: "0-65535", Invert: boolPtr(true), Outbound: "srv-3f2a1c9e"},
	), oneTag)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	want := []Document{
		{"outboundTag": "srv-3f2a1c9e", "domain": []string{"geosite:category-ads-all"}},
		{"outboundTag": "srv-3f2a1c9e", "ip": []string{"10.0.0.0/8"}, "port": "0-65535", "invert": true},
	}
	if len(got.Rules) != len(want) {
		t.Fatalf("got %d rules, want %d", len(got.Rules), len(want))
	}
	for i, rule := range got.Rules {
		if len(rule) != len(want[i]) {
			t.Errorf("rule #%d is %v, want %v", i, rule, want[i])
		}
		for k, v := range want[i] {
			if fmt.Sprint(rule[k]) != fmt.Sprint(v) {
				t.Errorf("rule #%d field %q: got %v, want %v", i, k, rule[k], v)
			}
		}
		if _, found := rule["outbound"]; found {
			t.Errorf("rule #%d still carries the model's field name; Core reads outboundTag", i)
		}
	}
	// Priority is position: doc 28 §8 permits reordering "for matching efficiency" and nothing yet proves a reorder
	// preserves meaning, so the emitted order is the authored order.
	if got.Rules[0]["domain"] == nil {
		t.Error("the two rules swapped; a core rule's position is its rank")
	}
}

// doc 28 §8's "may not invent an outbound tag" has a second half the schema cannot check: a `srv-` tag that is
// syntactically valid but not in this config.
func TestATagNoServerEmitsIsACompileError(t *testing.T) {
	_, err := Split(policyWith(CoreRule{Domain: []string{"example.net"}, Outbound: "srv-00000000"}), oneTag)
	if err == nil {
		t.Fatal("a rule pointing at an outbound the config does not carry compiled")
	}
	for _, want := range []string{"srv-00000000", "f1e2d3c4", "invent an outbound tag"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// `#/$defs/coreOutbound` is `^(direct|block|srv-[0-9a-f]{8})$`; reaching this refusal means a payload broke a pattern
// the schema states, which is a caller bug and not a routing decision.
func TestAnOutboundOutsideThePatternIsRefused(t *testing.T) {
	for _, bad := range []string{"", "proxy-us", "srv-3F2A1C9E", "srv-3f2a1c9", "srv-3f2a1c9e1", "DIRECT"} {
		if _, err := Split(policyWith(CoreRule{Domain: []string{"example.net"}, Outbound: bad}), oneTag); err == nil {
			t.Errorf("outbound %q passed the #/$defs/coreOutbound check", bad)
		} else if !strings.Contains(err.Error(), "coreOutbound") {
			t.Errorf("refusal for %q does not name the pattern it broke: %v", bad, err)
		}
	}
}

// The policy's own report is authored for the same policy this compile is about, and doc 27 §7 says the field is
// declared twice on purpose so both the policy author and the operator see it.
func TestThePolicysOwnReportIsCarriedIntoTheApplyResult(t *testing.T) {
	policy := policyWith(CoreRule{Domain: []string{"example.net"}, Outbound: "srv-3f2a1c9e"})
	policy.CorePlane.Unrepresentable = []Report{
		{Source: "WeekdayCondition days=YYYYNNN", Reason: "Xray routing has no calendar-time predicate"},
	}
	got, err := Split(policy, oneTag)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(got.Unrepresentable) != 1 || got.Unrepresentable[0].Source != "WeekdayCondition days=YYYYNNN" {
		t.Errorf("carried report = %v", got.Unrepresentable)
	}
	if len(got.Rules) != 1 {
		t.Errorf("a carried report suppressed the rule beside it: %v", got.Rules)
	}
}

// The bounds are the schema's, enforced rather than truncated: a report the operator cannot read is a drop wearing a
// bound, and doc 27 §7 rule 1 forbids the drop.
func TestReportBoundsAreEnforcedNotTruncated(t *testing.T) {
	long := strings.Repeat("x", 201)
	policy := policyWith()
	policy.CorePlane.Unrepresentable = []Report{{Source: "a rule", Reason: long}}
	if _, err := Split(policy, oneTag); err == nil {
		t.Error("a 201-character reason passed the bound config.apply declares")
	} else if !strings.Contains(err.Error(), "201 characters") {
		t.Errorf("the refusal does not name the length: %v", err)
	}

	empty := policyWith()
	empty.CorePlane.Unrepresentable = []Report{{Source: "", Reason: "no source"}}
	if _, err := Split(empty, oneTag); err == nil {
		t.Error("an entry with no source passed the minLength 1 the schema declares")
	}

	many := policyWith()
	for i := 0; i < maxReports+1; i++ {
		many.CorePlane.Unrepresentable = append(many.CorePlane.Unrepresentable,
			Report{Source: fmt.Sprintf("rule %d", i), Reason: "reported"})
	}
	if _, err := Split(many, oneTag); err == nil {
		t.Errorf("%d entries fit the report; the apply result declares maxItems %d", maxReports+1, maxReports)
	} else if !strings.Contains(err.Error(), fmt.Sprint(maxReports)) {
		t.Errorf("the refusal does not name the capacity: %v", err)
	}
}

// The two shipped routing examples are the corpus's own policies; both are reported, and neither is a failure.
func TestTheShippedRoutingExamplesCompileIntoReports(t *testing.T) {
	minimal := example[RoutingPolicy](t, "routing.minimal.json")
	got, err := Split(&minimal, oneTag)
	if err != nil {
		t.Fatalf("Split(routing.minimal.json): %v", err)
	}
	if len(got.Rules) != 0 || len(got.Unrepresentable) != 1 {
		t.Errorf("minimal policy: %d rules, %d reports; its one rule names direct and matches on network", len(got.Rules), len(got.Unrepresentable))
	}
	if !strings.Contains(got.Unrepresentable[0].Reason, "direct") {
		t.Errorf("the minimal policy reported %q; its blocker is the built-in outbound", got.Unrepresentable[0].Reason)
	}

	twoPlane := example[RoutingPolicy](t, "routing.two-plane.json")
	// The three servers `profile.fastest.json` selects, which is the profile that names this policy.
	tags := []string{"srv-3f2a1c9e", "srv-8b1d4e70", "srv-e2b7c4a9"}
	got, err = Split(&twoPlane, tags)
	if err != nil {
		t.Fatalf("Split(routing.two-plane.json): %v", err)
	}
	// 2 authored entries + 1 sniffing + 2 rules naming block and direct.
	if len(got.Rules) != 0 || len(got.Unrepresentable) != 5 {
		t.Errorf("two-plane policy: %d rules, %d reports, want 0 and 5: %+v", len(got.Rules), len(got.Unrepresentable), got.Unrepresentable)
	}
	var sniffing bool
	for _, r := range got.Unrepresentable {
		if r.Source == "corePlane.sniffing" {
			sniffing = true
			if !strings.Contains(r.Reason, "ODEC-37") {
				t.Errorf("the sniffing report names no decision: %q", r.Reason)
			}
		}
	}
	if !sniffing {
		t.Error("a policy with sniffing.enabled true reported no sniffing entry; doc 27 §6 puts routeOnly in the same undecided group")
	}
}
