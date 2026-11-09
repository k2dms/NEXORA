// The assembler: stages 1 to 6 of docs/architecture/27-config-compilation-pipeline.md §2 in one call, because the
// stages share the two things that make the result deterministic — the resolved server set and the allocated port
// table — and splitting them across a public API would let a caller combine a stage-1 output with a stage-3 input
// that no longer agrees with it.
//
// Compile is pure: no filesystem, no keystore, no Core. That is what makes the golden files of §3 possible at all,
// and it is why the two things the pipeline reads but cannot itself resolve — the secret behind a `userIdRef`, the
// header map behind a `headersRef` — arrive as handles (apply materializes them; ODEC-45).
package compile

import (
	"fmt"
)

// loopbackHost is `connector-settings.schema.json#/properties/ports/properties/host`'s const, and ADR-0006's whole
// enforcement point: doc 27 §2 calls stage 7 "what makes ADR-0006 a fact instead of an intention", because the
// compiled document is inspected before Core ever sees it.
const loopbackHost = "127.0.0.1"

// noAccessLog is `observability.json#/coreLog/accessLogDecision/value`, and doc 36 §9's point that it "is not a user
// setting": an Xray access log is one line per connection with its destination, on a home user's disk.
const noAccessLog = "none"

// coreLevels is `packages/schema/observability.json#/levels/toCore`, copied rather than read at runtime — the
// Connector ships as one binary and the corpus is a build-time artefact. compile_test.go asserts this table against
// that file, which is the only reason a copy is not a second answer.
var coreLevels = map[string]string{
	"error": "error",
	"warn":  "warning",
	"info":  "info",
	"debug": "debug",
}

// Inbound is one entry of `packages/schema/protocol/commands/config.apply.json#/result/properties/inboundPorts`,
// whose `listen` is an enum of the two loopback spellings while `connector-settings.schema.json` allows only
// `127.0.0.1` — so `listen` here is set from the validated host, not from a free string.
type Inbound struct {
	Listen string `json:"listen"`
	Port   int    `json:"port"`
	Tag    string `json:"tag"`
}

// BlockTarget is stage 4's first embedded decision, carried out rather than in: doc 30 §7 makes the block a
// `chrome.proxy` FixedProfile aimed at a loopback port nothing listens on, so its home is the extension's proxy
// settings and not the Core config file. The range is carried, not one port, because doc 30 §8 puts "which port is
// in force" in the Connector's failure-policy state, where reconnect reconciliation compares it.
type BlockTarget struct {
	Scheme       string
	Host         string
	PortRange    Range
	AssertClosed bool
}

// RestartBudget is stage 4's second embedded decision, carried for the same reason: the restart budget belongs to
// the CoreSupervisor, and `packages/schema/observability.json`'s `coreLog` section has no field for it.
type RestartBudget struct {
	Attempts       int
	BackoffSeconds []int
	Notify         bool
}

// Input is the desired state, as four contract documents plus the allocator's memory of what it handed out before.
type Input struct {
	Profiles []Profile
	Servers  []Server
	Routing  *RoutingPolicy
	Settings Settings
	Failure  Failure

	// Held is the reservation table of doc 27 §4's "Persistence" row, keyed by outbound tag. It comes in and goes
	// out (Result.Assigned) rather than living in this package, because §4's "Reservation" row puts the store in the
	// Connector, and a pure compiler is the only version of it that a golden file can drive.
	Held map[string]int
}

// Result is what `config.apply` answers with, minus the four fields that describe the apply rather than the compile
// (`applied`, `restarted`, `dryRun`, `previousConfigHash`) — those belong to the caller that wrote the file.
type Result struct {
	Document Document
	Bytes    []byte
	Hash     string

	Inbounds        []Inbound
	ServerTags      []string
	Unrepresentable []Report
	Block           BlockTarget
	Restart         RestartBudget

	// Assigned is the new reservation table: what the caller persists as Input.Held for the next compile.
	Assigned map[string]int
}

// Compile runs the pipeline. Any stage's refusal aborts the whole compile, which is doc 27 §8's atomicity rule seen
// from the compiler's side: a half-built config is never what `config.apply` writes, and a caller that ignores the
// error and writes the previous document keeps the previous *believed* state honest via `previousConfigHash`.
func Compile(in Input) (*Result, error) {
	resolved, err := Resolve(in.Profiles, in.Servers, in.Routing)
	if err != nil {
		return nil, err
	}
	routing, err := Split(in.Routing, resolved.Tags)
	if err != nil {
		return nil, err
	}

	ports := Range{Min: in.Settings.Ports.Range.Min, Max: in.Settings.Ports.Range.Max}
	block := Range{Min: in.Failure.Block.Range.Min, Max: in.Failure.Block.Range.Max}
	assigned, err := Allocate(resolved.Tags, ports, block, in.Held)
	if err != nil {
		return nil, err
	}

	host := in.Settings.Ports.Host
	if host != "" && host != loopbackHost {
		// `connector-settings.schema.json` pins this to a const, so the only way to reach this line is a payload from
		// something that did not validate it. ADR-0006 is not a field to pass through.
		return nil, fmt.Errorf("compile: the settings ask inbounds to listen on %q; the Connector only ever binds %s (ADR-0006)", host, loopbackHost)
	}
	if host == "" {
		host = loopbackHost
	}

	logLevel, err := coreLogLevel(in.Settings.Telemetry.LogLevel)
	if err != nil {
		return nil, err
	}
	obs, err := buildOutbounds(resolved.Servers)
	if err != nil {
		return nil, err
	}
	for _, b := range routing.BuiltIns {
		obs = append(obs, builtinOutbound(b))
	}
	// Without these fallbacks, every unmatched inbound uses the first server's outbound.
	for _, tag := range resolved.Tags {
		routing.Rules = append(routing.Rules, Document{"inboundTag": []string{"in-" + tag}, "outboundTag": tag})
	}

	doc := Document{
		// doc 36 §9: the Compiler writes this section as part of every config it emits, and shapes B and C of doc 23 §9
		// omit it only because they are illustrations of the outbound. `error` is deliberately absent — it is a path,
		// and doc 27 §3.2 keeps paths out of the hashed bytes for the same reason §3.3 keeps `installRoot` out.
		"log": Document{
			"loglevel": logLevel,
			"access":   noAccessLog,
			"dnsLog":   false,
		},
		"inbounds":  inbounds(resolved.Servers, assigned, host, routing.Sniffing),
		"outbounds": obs,
		// doc 23 §9's shape A shows the policy section with no levels; it is part of the shape the golden files pin.
		"policy":  Document{"levels": Document{}},
		"routing": routingSection(routing),
	}

	out := &Result{
		Document:        doc,
		ServerTags:      resolved.Tags,
		Unrepresentable: routing.Unrepresentable,
		Assigned:        assigned,
		Block: BlockTarget{
			Scheme:       in.Failure.Block.Scheme,
			Host:         in.Failure.Block.Host,
			PortRange:    block,
			AssertClosed: in.Failure.Block.AssertClosed == nil || *in.Failure.Block.AssertClosed,
		},
		Restart: RestartBudget{
			Attempts:       in.Failure.CoreDeath.RestartAttempts,
			BackoffSeconds: in.Failure.CoreDeath.RestartBackoffSeconds,
			Notify:         in.Failure.CoreDeath.Notify,
		},
	}
	if len(out.Unrepresentable) == 0 {
		out.Unrepresentable = nil
	}

	data, err := Emit(doc)
	if err != nil {
		return nil, err
	}
	out.Bytes = data
	out.Hash = Hash(data)

	out.Inbounds = inboundList(resolved.Servers, assigned, host)
	if err := validate(doc, out.Inbounds); err != nil {
		return nil, err
	}
	return out, nil
}

// coreLogLevel maps, it does not pass through — the rule of docs/architecture/36-observability.md §4: "The NEXORA
// vocabulary is 'warn' and Xray's is 'warning'". A pass-through would produce a config invalid at the one field
// nothing else reads, which doc 36 §4 calls the worst kind of failure: the apply that succeeds and then behaves as
// if the default level had been used.
func coreLogLevel(level string) (string, error) {
	if level == "" {
		// `connector-settings.schema.json` gives `telemetry.logLevel` a default, and a payload that omitted the field
		// is the settings document saying the default; the Connector's store holds the resolved value.
		return "", fmt.Errorf("compile: the settings carry no telemetry.logLevel, so the compiled config would have to guess the Core's verbosity")
	}
	mapped, ok := coreLevels[level]
	if !ok {
		return "", fmt.Errorf("compile: telemetry.logLevel is %q, which is not one of the four names observability.json#/levels/names admits; the Compiler maps to Xray's spelling and does not pass through (doc 36 §4)", level)
	}
	return mapped, nil
}

func routingSection(r *Routing) Document {
	rules := make([]any, len(r.Rules))
	for i, rule := range r.Rules {
		rules[i] = rule
	}
	return Document{"domainStrategy": r.DomainStrategy, "rules": rules}
}

// Sniffing is an inbound sibling, not a SOCKS setting; routeOnly prevents destination rewriting.
func inboundDocument(tag string, port int, host string, sniffing *Sniffing) Document {
	in := Document{
		"tag":      "in-" + tag,
		"port":     port,
		"protocol": "socks",
		"listen":   host,
		"settings": Document{"auth": "noauth", "udp": true},
	}
	if sniffing != nil {
		s := Document{"enabled": true}
		if len(sniffing.Protocols) > 0 {
			s["destOverride"] = sniffing.Protocols
		}
		s["routeOnly"] = true
		in["sniffing"] = s
	}
	return in
}

func inbounds(servers []Server, assigned map[string]int, host string, sniffing *Sniffing) []any {
	list := make([]any, len(servers))
	for i, s := range servers {
		tag := ServerTag(s.ID)
		list[i] = inboundDocument(tag, assigned[tag], host, sniffing)
	}
	return list
}

func inboundList(servers []Server, assigned map[string]int, host string) []Inbound {
	list := make([]Inbound, len(servers))
	for i, s := range servers {
		tag := ServerTag(s.ID)
		list[i] = Inbound{Listen: host, Port: assigned[tag], Tag: "in-" + tag}
	}
	return list
}
