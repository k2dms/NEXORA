package compile

import (
	"fmt"
	"strings"
)

const (
	maxReports      = 256
	maxReportSource = 200
	maxReportReason = 200
)

const defaultDomainStrategy = "AsIs"

type Routing struct {
	DomainStrategy  string
	Rules           []Document
	Unrepresentable []Report
	BuiltIns        []string
	Sniffing        *Sniffing
}

var builtinOrder = []string{"direct", "block"}

func builtinOutbound(tag string) Document {
	protocol := "freedom"
	if tag == "block" {
		protocol = "blackhole"
	}
	return Document{"tag": tag, "protocol": protocol}
}

func Split(policy *RoutingPolicy, tags []string) (*Routing, error) {
	out := &Routing{DomainStrategy: defaultDomainStrategy}
	if policy == nil {
		return out, nil
	}
	out.DomainStrategy = policy.CorePlane.DomainStrategy
	out.Unrepresentable = append(out.Unrepresentable, policy.CorePlane.Unrepresentable...)

	sniffing := policy.CorePlane.Sniffing
	if sniffing.RouteOnly != nil && !*sniffing.RouteOnly {
		return nil, fmt.Errorf("compile: sniffing.routeOnly must be true; destination rewriting violates the one-plane rule")
	}
	for _, protocol := range sniffing.Protocols {
		if !contains([]string{"http", "tls", "quic", "fakedns"}, protocol) {
			return nil, fmt.Errorf("compile: unsupported sniffing protocol %q", protocol)
		}
	}
	if sniffing.Enabled != nil && *sniffing.Enabled {
		out.Sniffing = &sniffing
	}

	builtins := make(map[string]bool)
	// Policy order is first-match priority; never sort rules along with their outbound declarations.
	for i, rule := range policy.CorePlane.Rules {
		if !isCoreOutbound(rule.Outbound) {
			return nil, fmt.Errorf("compile: routing policy %s rule #%d names the outbound %q, which #/$defs/coreOutbound does not admit",
				policy.ID, i, rule.Outbound)
		}
		if strings.HasPrefix(rule.Outbound, "srv-") && !contains(tags, rule.Outbound) {
			return nil, fmt.Errorf("compile: routing policy %s rule #%d sends traffic to %s, which no selected server emits; the compiler may not invent an outbound tag (doc 28 §8)",
				policy.ID, i, rule.Outbound)
		}
		if rule.Invert != nil && *rule.Invert {
			out.Unrepresentable = append(out.Unrepresentable, Report{
				Source: fmt.Sprintf("corePlane rule #%d", i),
				Reason: "Xray 24.11.11 ignores invert; the rule cannot be represented without changing its match",
			})
			continue
		}
		if !strings.HasPrefix(rule.Outbound, "srv-") {
			builtins[rule.Outbound] = true
		}
		out.Rules = append(out.Rules, rule.section())
	}
	for _, tag := range builtinOrder {
		if builtins[tag] {
			out.BuiltIns = append(out.BuiltIns, tag)
		}
	}

	if len(out.Unrepresentable) > maxReports {
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
	if len(r.Network) > 0 {
		rule["network"] = r.Network
	}
	if len(r.Protocol) > 0 {
		rule["protocol"] = r.Protocol
	}
	return rule
}

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
