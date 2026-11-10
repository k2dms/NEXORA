// This file is `server.test` as one path. docs/architecture/22-component-contracts.md §8 puts the HealthProber's
// command surface in this package, and the three things it joins are each already tested as neighbours: the
// internal/health prober over loopback SOCKS5, the contract's own rate limit (8 per 10 seconds per sender, which
// internal/dispatch reads from the projection before a frame reaches this handler), and the closed field list the
// command doc states.
//
// The two facts that kept this handler out of the build's inventory until now are each carried by a document the
// Connector has, and not by a constant here:
//
//   - the frame names a server id and nothing else, and the server documents are the ones `config.apply` was
//     handed — doc 20 §3 keeps the store read in the surface, and this file's surface is the same object that
//     compiled them (see `inForce` in config_apply.go, and ADR-0010 for why the read is in memory);
//   - the egress probe's endpoint is `connector-settings.schema.json#/properties/core/properties/egressProbe/
//     properties/endpoint` and its expected address is the tested server's own `address` — ODEC-42, answered
//     2026-11-07, whose runtime half is `newHealthSurface` below and whose compile-time half is the field internal/
//     compile now mirrors.
//
// One reading this file states because the command doc cannot: `server.test#/errors` does not declare
// `BadRequest` — only six of the nineteen commands do — so an args payload that violates the call contract is
// answered `HealthCheckFailed`, the command's own umbrella for "the probe did not complete", with a message naming
// the rule at fault. This follows the reading internal/hello records for the same situation (`connector.hello`
// answers `HandshakeFailed` for unreadable args because that is literally what failed), and it keeps doc 25 §8
// rule 2 true: the codes this handler can answer are exactly the codes the command cites.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/k2dms/nexora/connector/internal/compile"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/health"
)

// prober is internal/health.Prober's surface as this command needs it. The seam keeps this file's tests about
// decoding and refusal mapping; the prober's own tests drive a real one over loopback.
type prober interface {
	Probe(ctx context.Context, req health.Request) (*health.Answer, error)
}

// serverTest is the handler: the prober and nothing else. There are no directories to carry and no span to pick —
// every fact a probe needs is either in the frame or behind the prober's own wiring.
type serverTest struct {
	probes prober
}

func newServerTest(p prober) (*serverTest, error) {
	if p == nil {
		return nil, errors.New("server.test: no prober; a handler without one would have to invent an answer §8 reserves for a measurement")
	}
	return &serverTest{probes: p}, nil
}

// newHealthSurface builds the handler over the wiring the process already has: the applied intent for the server
// table and the endpoint, and the Core process owner for the live inbound table. It takes the apply surface rather
// than its pieces because the fact a probe needs is exactly the fact that surface put into Core — a prober aimed at
// a different document would be measuring a config nobody applied.
//
// `Failover` is left at its zero value, which internal/health reads as "no failover policy configured". The policy
// is a field of a profile document (`connector-profile.schema.json#/properties/failover`) and the prober's streak
// bounds are build-time; carrying the selected profile's numbers is doc 30's reconciliation loop, which is the
// poller that does not exist yet, not this command surface. What the zero value changes is the retained records
// (`Mark`/`Due`), never a probe's rows: every layer still measures.
func newHealthSurface(applied *configApply) (*serverTest, error) {
	if applied == nil {
		return nil, errors.New("server.test: no apply surface; the server id and the egress endpoint a probe reads are what config.apply learned from the frame, and no other path has them")
	}
	p, err := health.New(health.Config{
		Server:   applied.serverFor,
		Inbounds: func() []compile.Inbound { return healthInbounds(applied.core.Status()) },
		Egress:   applied.egressEndpointFor,
	})
	if err != nil {
		return nil, fmt.Errorf("server.test: %w", err)
	}
	return newServerTest(p)
}

// probeArgs is the decoded form of `packages/schema/protocol/commands/server.test.json#/args`. egressProbe and
// repeat keep the wire's own spelling because they are optional and their defaults are the schema's own.
type probeArgs struct {
	serverID    string
	layers      []string
	egressProbe bool
	repeat      int
}

// reProbeLayer is `server.test.json#/args/properties/layers/items/enum`. `egress-ip` is deliberately absent: it is
// `#/result/properties/layers/items`'s fifth name, the answer of an egress probe rather than a requestable layer.
var reProbeLayer = map[string]bool{
	"dns":           true,
	"tcp":           true,
	"core-outbound": true,
	"proxied-http":  true,
}

// readProbeArgs is this command's side of internal/hello's convention: decode by hand against the closed field
// list, because `additionalProperties:false` is stated by the command doc and a payload carrying an undeclared
// name is a caller that believes it asked for something. Messages name the schema key at fault and never its
// value — `framing.json#/senderPolicy/refusals/loggedFields` forbids quoting args on this pipe, and an unknown
// layer name is attacker-chosen text.
func readProbeArgs(f *envelope.Frame) (probeArgs, *dispatch.Refusal) {
	if len(f.Args) == 0 {
		return probeArgs{}, badProbeArgs("args are required: serverId, layers")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &raw); err != nil {
		return probeArgs{}, badProbeArgs("args are not a JSON object")
	}
	for k := range raw {
		switch k {
		case "serverId", "layers", "egressProbe", "repeat":
		default:
			return probeArgs{}, badProbeArgs("args carry a field server.test does not declare")
		}
	}
	args := probeArgs{repeat: 1}

	if v, ok := raw["serverId"]; !ok {
		return probeArgs{}, badProbeArgs("serverId is required")
	} else if err := json.Unmarshal(v, &args.serverID); err != nil {
		return probeArgs{}, badProbeArgs("serverId is not a string")
	} else if !reApplyID.MatchString(args.serverID) {
		return probeArgs{}, badProbeArgs("serverId does not match the UUID the server schema declares")
	}

	lv, ok := raw["layers"]
	if !ok {
		return probeArgs{}, badProbeArgs("layers is required")
	}
	var layers []string
	if err := json.Unmarshal(lv, &layers); err != nil {
		return probeArgs{}, badProbeArgs("layers is not an array of strings")
	}
	if len(layers) == 0 || len(layers) > 4 {
		return probeArgs{}, badProbeArgs("layers must hold between 1 and 4 names, and it holds a number outside that")
	}
	seen := make(map[string]bool, len(layers))
	for _, l := range layers {
		if !reProbeLayer[l] {
			return probeArgs{}, badProbeArgs("layers carries a name outside the enum dns|tcp|core-outbound|proxied-http")
		}
		if seen[l] {
			return probeArgs{}, badProbeArgs("layers carries the same name twice, and the command's layers is uniqueItems")
		}
		seen[l] = true
	}
	args.layers = layers

	if v, ok := raw["egressProbe"]; ok {
		if err := json.Unmarshal(v, &args.egressProbe); err != nil {
			return probeArgs{}, badProbeArgs("egressProbe is not a boolean")
		}
	}
	if v, ok := raw["repeat"]; ok {
		if err := json.Unmarshal(v, &args.repeat); err != nil {
			return probeArgs{}, badProbeArgs("repeat is not an integer")
		}
		if args.repeat < 1 || args.repeat > 10 {
			return probeArgs{}, badProbeArgs("repeat is outside the command's 1 to 10")
		}
	}
	return args, nil
}

func badProbeArgs(rule string) *dispatch.Refusal {
	return &dispatch.Refusal{Code: "HealthCheckFailed", Message: "server.test " + rule}
}

// Handle is the whole path: decode, probe, answer. There is no ordering question to state here — a probe writes
// nothing, so the only thing a refusal can leave behind is the socket it never opened.
func (s *serverTest) Handle(ctx context.Context, f *envelope.Frame) (any, *dispatch.Refusal) {
	args, bad := readProbeArgs(f)
	if bad != nil {
		return nil, bad
	}
	ans, err := s.probes.Probe(ctx, health.Request{
		ServerID: args.serverID,
		Layers:   args.layers,
		Egress:   args.egressProbe,
		Repeat:   args.repeat,
	})
	if err != nil {
		return nil, refuseProbe(err, "server.test: the probe of the layers this frame named could not run to an answer")
	}
	return ans, nil
}

// codeForProbe is the classification table. A refusal from the prober carries its own code — internal/health's
// init() guarantees that code is one `server.test#/errors` declares — and anything else is a defect in this wiring
// rather than an answer to a caller, which is the only reading `Internal` honestly supports.
func codeForProbe(err error) string {
	var he *health.Error
	if errors.As(err, &he) {
		return he.Code()
	}
	return "Internal"
}

// refuseProbe turns one prober refusal into the frame the command answers with. The prober's messages already name
// the server and the layer doc 30 §4 requires them to name; the clip keeps the frame inside
// `envelope.schema.json`'s own message bound.
func refuseProbe(err error, why string) *dispatch.Refusal {
	return &dispatch.Refusal{
		Code:    codeForProbe(err),
		Message: clip(fmt.Sprintf("%s: %v", why, err), maxApplyMessage),
	}
}
