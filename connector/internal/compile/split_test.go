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

// An undeclared tag can fall back to the default outbound, so every referenced builtin must be declared.
func TestABuiltinOutboundIsDeclaredAndItsRuleIsEmitted(t *testing.T) {
	for _, builtin := range []string{"direct", "block"} {
		got, err := Split(policyWith(CoreRule{Domain: []string{"example.net"}, Outbound: builtin}), oneTag)
		if err != nil {
			t.Fatalf("Split(%s): %v", builtin, err)
		}
		if len(got.Rules) != 1 {
			t.Fatalf("%s did not emit its rule: %v", builtin, got.Rules)
		}
		if fmt.Sprint(got.Rules[0]["outboundTag"]) != builtin {
			t.Errorf("rule #%d names %v, want the tag the policy wrote", 0, got.Rules[0]["outboundTag"])
		}
		if len(got.Unrepresentable) != 0 {
			t.Errorf("%s produced reports %+v; the measurement retired the refusal", builtin, got.Unrepresentable)
		}
		if len(got.BuiltIns) != 1 || got.BuiltIns[0] != builtin {
			t.Errorf("BuiltIns = %v, want exactly [%s]", got.BuiltIns, builtin)
		}
		doc := builtinOutbound(builtin)
		want := map[string]string{"direct": "freedom", "block": "blackhole"}[builtin]
		if fmt.Sprint(doc["protocol"]) != want {
			t.Errorf("builtinOutbound(%s) is %v, want protocol %s behind the tag", builtin, doc, want)
		}
	}
}

func TestBuiltinOutboundsComeInFixedOrderAndDeduplicate(t *testing.T) {
	reversed, err := Split(policyWith(
		CoreRule{Domain: []string{"geosite:category-ads-all"}, Outbound: "block"},
		CoreRule{IP: []string{"10.0.0.0/8"}, Outbound: "direct"},
		CoreRule{Domain: []string{"example.net"}, Outbound: "block"},
	), oneTag)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(reversed.BuiltIns) != 2 || reversed.BuiltIns[0] != "direct" || reversed.BuiltIns[1] != "block" {
		t.Errorf("BuiltIns = %v, want [direct block] whatever order the rules named them", reversed.BuiltIns)
	}
	if len(reversed.Rules) != 3 {
		t.Errorf("%d rules emitted, want all three including the duplicate built-in rule", len(reversed.Rules))
	}
}

func TestNetworkAndProtocolArraysPassThrough(t *testing.T) {
	got, err := Split(policyWith(
		CoreRule{Network: []string{"udp"}, Port: "53", Outbound: "srv-3f2a1c9e"},
		CoreRule{Protocol: []string{"bittorrent"}, Outbound: "srv-3f2a1c9e"},
		CoreRule{Network: []string{"tcp", "udp"}, Protocol: []string{"http"}, Domain: []string{"example.net"}, Outbound: "srv-3f2a1c9e"},
	), oneTag)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(got.Rules) != 3 {
		t.Fatalf("got %d rules, want 3: %v", len(got.Rules), got.Rules)
	}
	if len(got.Unrepresentable) != 0 {
		t.Fatalf("reports %v; the array form is measured against the pinned release", got.Unrepresentable)
	}
	if fmt.Sprint(got.Rules[0]["network"]) != "[udp]" {
		t.Errorf("rule #0 network = %v, want the model's array verbatim", got.Rules[0]["network"])
	}
	if fmt.Sprint(got.Rules[1]["protocol"]) != "[bittorrent]" {
		t.Errorf("rule #1 protocol = %v, want the model's array verbatim", got.Rules[1]["protocol"])
	}
	if fmt.Sprint(got.Rules[2]["network"]) != "[tcp udp]" || fmt.Sprint(got.Rules[2]["protocol"]) != "[http]" {
		t.Errorf("rule #2 is %v, want both arrays verbatim", got.Rules[2])
	}
}

func TestSniffingRidesOnlyWhenEnabled(t *testing.T) {
	on := policyWith()
	on.CorePlane.Sniffing = Sniffing{Enabled: boolPtr(true), Protocols: []string{"http", "tls"}, RouteOnly: boolPtr(true)}
	got, err := Split(on, oneTag)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if got.Sniffing == nil {
		t.Fatal("an enabled sniffing block did not ride to the assembler")
	}
	if fmt.Sprint(got.Sniffing.Protocols) != "[http tls]" {
		t.Errorf("protocols = %v; the assembler renames to destOverride, stage 2 carries the model whole", got.Sniffing.Protocols)
	}

	off := policyWith()
	off.CorePlane.Sniffing = Sniffing{Enabled: boolPtr(false), Protocols: []string{"http"}}
	absent, err := Split(off, oneTag)
	if err != nil {
		t.Fatalf("Split(disabled): %v", err)
	}
	if absent.Sniffing != nil {
		t.Errorf("a disabled block rode anyway: %+v", absent.Sniffing)
	}
}

// doc 28 §7's rank-4 row and doc 36 §6's `rank: 5` event both assume every policy rule arrives with an `outboundTag`.
// The model's field is `outbound`, so the rename is what stage 2 owns.
func TestARepresentableRuleRenamesOutboundAndKeepsEveryOtherName(t *testing.T) {
	got, err := Split(policyWith(
		CoreRule{Domain: []string{"geosite:category-ads-all"}, Outbound: "srv-3f2a1c9e"},
		CoreRule{IP: []string{"10.0.0.0/8"}, Port: "0-65535", Invert: boolPtr(false), Outbound: "srv-3f2a1c9e"},
	), oneTag)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	want := []Document{
		{"outboundTag": "srv-3f2a1c9e", "domain": []string{"geosite:category-ads-all"}},
		{"outboundTag": "srv-3f2a1c9e", "ip": []string{"10.0.0.0/8"}, "port": "0-65535"},
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

func TestAnInvertedRuleIsReportedWholeWithoutDeclaringItsOutbound(t *testing.T) {
	got, err := Split(policyWith(
		CoreRule{Domain: []string{"private.example"}, Invert: boolPtr(true), Outbound: "block"},
		CoreRule{Network: []string{"tcp"}, Outbound: "direct"},
	), oneTag)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unrepresentable) != 1 || got.Unrepresentable[0].Source != "corePlane rule #0" || !strings.Contains(got.Unrepresentable[0].Reason, "invert") {
		t.Fatalf("missing positional invert report: %+v", got.Unrepresentable)
	}
	if strings.Contains(fmt.Sprint(got.Unrepresentable), "private.example") {
		t.Error("report leaked the rule's destination")
	}
	if len(got.Rules) != 1 || got.Rules[0]["outboundTag"] != "direct" || fmt.Sprint(got.BuiltIns) != "[direct]" {
		t.Fatalf("inverted rule leaked into config: %+v", got)
	}
}

func TestWebSocketRuleIsReportedWhole(t *testing.T) {
	for _, protocols := range [][]string{{"ws"}, {"http", "ws"}, {"ws", "tls"}} {
		t.Run(strings.Join(protocols, "_"), func(t *testing.T) {
			policy := policyWith(
				CoreRule{Port: "80", Outbound: "direct"},
				CoreRule{Domain: []string{"private.example"}, Protocol: protocols, Outbound: "block"},
				CoreRule{Port: "443", Outbound: "srv-3f2a1c9e"},
			)
			before := fmt.Sprint(protocols)
			got, err := Split(policy, oneTag)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Unrepresentable) != 1 || got.Unrepresentable[0].Source != "corePlane rule #1" ||
				!strings.Contains(got.Unrepresentable[0].Reason, "ws") {
				t.Fatalf("missing positional ws report: %+v", got.Unrepresentable)
			}
			if strings.Contains(fmt.Sprint(got.Unrepresentable), "private.example") {
				t.Error("report leaked the rule's destination")
			}
			if len(got.Rules) != 2 || got.Rules[0]["port"] != "80" || got.Rules[1]["port"] != "443" ||
				fmt.Sprint(got.BuiltIns) != "[direct]" {
				t.Fatalf("ws rule was emitted or adjacent rules changed: %+v", got)
			}
			if fmt.Sprint(policy.CorePlane.Rules[1].Protocol) != before {
				t.Fatal("authored protocol list was mutated")
			}
		})
	}
}

// TestFakednsRuleIsReportedWhole is the same silent-miss class as the ws case, from the other side: Xray adds the
// fakedns metadata sniffer only when a FakeDNS engine is registered, and this compiler emits no `fakedns` section,
// so the predicate can never match the config it writes.
func TestFakednsRuleIsReportedWhole(t *testing.T) {
	for _, protocols := range [][]string{{"fakedns"}, {"http", "fakedns"}} {
		t.Run(strings.Join(protocols, "_"), func(t *testing.T) {
			policy := policyWith(
				CoreRule{Port: "53", Outbound: "direct"},
				CoreRule{Domain: []string{"private.example"}, Protocol: protocols, Outbound: "block"},
			)
			got, err := Split(policy, oneTag)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Unrepresentable) != 1 || got.Unrepresentable[0].Source != "corePlane rule #1" ||
				!strings.Contains(got.Unrepresentable[0].Reason, "fakedns") {
				t.Fatalf("missing positional fakedns report: %+v", got.Unrepresentable)
			}
			if strings.Contains(fmt.Sprint(got.Unrepresentable), "private.example") {
				t.Error("report leaked the rule's destination")
			}
			if len(got.Rules) != 1 || got.Rules[0]["port"] != "53" || fmt.Sprint(got.BuiltIns) != "[direct]" {
				t.Fatalf("fakedns rule was emitted or the adjacent rule changed: %+v", got)
			}
		})
	}
}

func TestSniffingRejectsUnsupportedValues(t *testing.T) {
	for _, protocol := range []string{"mp4", "mkv", "ps", "www"} {
		policy := policyWith()
		policy.CorePlane.Sniffing.Protocols = []string{protocol}
		if _, err := Split(policy, oneTag); err == nil {
			t.Errorf("accepted unsupported sniffing protocol %s", protocol)
		}
	}
	policy := policyWith()
	policy.CorePlane.Sniffing.RouteOnly = boolPtr(false)
	if _, err := Split(policy, oneTag); err == nil {
		t.Error("accepted destination rewriting")
	}
}

func TestSniffingEmitsInboundFieldsAndDefaultsToRouteOnly(t *testing.T) {
	for _, protocols := range [][]string{nil, {"http", "tls", "quic", "fakedns"}} {
		policy := policyWith()
		policy.CorePlane.Sniffing = Sniffing{Enabled: boolPtr(true), Protocols: protocols}
		routing, err := Split(policy, oneTag)
		if err != nil {
			t.Fatal(err)
		}
		in := inboundDocument(oneTag[0], 20100, loopbackHost, routing.Sniffing)
		sniffing := in["sniffing"].(Document)
		if sniffing["enabled"] != true || sniffing["routeOnly"] != true {
			t.Fatalf("wrong sniffing defaults: %v", sniffing)
		}
		if fmt.Sprint(sniffing["destOverride"]) != fmt.Sprint(protocols) && len(protocols) > 0 {
			t.Errorf("protocols changed: %v", sniffing)
		}
		if _, exists := sniffing["protocols"]; exists {
			t.Error("model field leaked into Xray config")
		}
		if _, exists := in["settings"].(Document)["sniffing"]; exists {
			t.Error("sniffing was placed inside SOCKS settings")
		}
	}
	if _, exists := inboundDocument(oneTag[0], 20100, loopbackHost, nil)["sniffing"]; exists {
		t.Error("disabled sniffing was emitted")
	}
}

func TestTheShippedRoutingExamplesCompileClean(t *testing.T) {
	minimal := example[RoutingPolicy](t, "routing.minimal.json")
	got, err := Split(&minimal, oneTag)
	if err != nil {
		t.Fatalf("Split(routing.minimal.json): %v", err)
	}
	if len(got.Rules) != 1 || len(got.Unrepresentable) != 0 {
		t.Errorf("minimal policy: %d rules, %d reports; its udp/53 rule matches on network, an array the pinned release reads", len(got.Rules), len(got.Unrepresentable))
	}
	if len(got.BuiltIns) != 1 || got.BuiltIns[0] != "direct" {
		t.Errorf("minimal policy BuiltIns = %v, want [direct] for its one direct rule", got.BuiltIns)
	}
	if fmt.Sprint(got.Rules[0]["network"]) != "[udp]" || fmt.Sprint(got.Rules[0]["port"]) != "53" {
		t.Errorf("the udp/53 rule emitted as %v, want the network array and the port string verbatim", got.Rules[0])
	}

	twoPlane := example[RoutingPolicy](t, "routing.two-plane.json")
	// The three servers `profile.fastest.json` selects, which is the profile that names this policy.
	tags := []string{"srv-3f2a1c9e", "srv-8b1d4e70", "srv-e2b7c4a9"}
	got, err = Split(&twoPlane, tags)
	if err != nil {
		t.Fatalf("Split(routing.two-plane.json): %v", err)
	}
	if len(got.Rules) != 2 || len(got.Unrepresentable) != 2 {
		t.Errorf("two-plane policy: %d rules, %d reports, want 2 and 2 (the authored ones): %+v", len(got.Rules), len(got.Unrepresentable), got.Unrepresentable)
	}
	if len(got.BuiltIns) != 2 || got.BuiltIns[0] != "direct" || got.BuiltIns[1] != "block" {
		t.Errorf("two-plane BuiltIns = %v, want [direct block] though the block rule is written first", got.BuiltIns)
	}
	if got.Sniffing == nil || got.Sniffing.RouteOnly == nil || !*got.Sniffing.RouteOnly {
		t.Errorf("two-plane sniffing = %+v; the shipped block carries routeOnly true and the one-plane rule keeps it", got.Sniffing)
	}
}
