package main

// These tests drive the decode and the refusal mapping: the two things this file owns that no other test covers.
// The prober behind the seam is a stub, because internal/health's own suite drives a real one over loopback SOCKS5
// and the wire shapes it answers with are asserted there.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/health"
)

const probeServerID = "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01" // nexora-secret-allow: fixture id invented for this test

// probeMethod is the wire name the frames in this file carry; the handler never reads it, but a frame that is not
// a request for `server.test` would not be on this path.
const probeMethod = "server.test"

type stubProber struct {
	req    health.Request
	answer *health.Answer
	err    error
}

func (s *stubProber) Probe(_ context.Context, req health.Request) (*health.Answer, error) {
	s.req = req
	return s.answer, s.err
}

func requestFrame(t *testing.T, args string) *envelope.Frame {
	t.Helper()
	m := probeMethod
	return &envelope.Frame{V: 1, ID: "probe-1", Type: envelope.TypeRequest, Method: &m, Args: json.RawMessage(args)}
}

func TestProbeArgsDecodeWhatTheCommandDeclares(t *testing.T) {
	f := requestFrame(t, `{"serverId":"`+probeServerID+`","layers":["tcp","dns"],"egressProbe":true,"repeat":3}`)
	args, refusal := readProbeArgs(f)
	if refusal != nil {
		t.Fatalf("a legal payload was refused: %s %s", refusal.Code, refusal.Message)
	}
	if args.serverID != probeServerID {
		t.Errorf("serverId = %q", args.serverID)
	}
	// The caller's order is carried as given; the canonical order is the prober's own normalization, which
	// probe_test.go asserts over the wire shapes.
	if len(args.layers) != 2 || args.layers[0] != "tcp" || args.layers[1] != "dns" {
		t.Errorf("layers = %v", args.layers)
	}
	if !args.egressProbe || args.repeat != 3 {
		t.Errorf("egressProbe = %v, repeat = %d", args.egressProbe, args.repeat)
	}

	f = requestFrame(t, `{"serverId":"`+probeServerID+`","layers":["dns"]}`)
	args, refusal = readProbeArgs(f)
	if refusal != nil {
		t.Fatalf("the minimal payload was refused: %s %s", refusal.Code, refusal.Message)
	}
	if args.egressProbe || args.repeat != 1 {
		t.Errorf("defaults: egressProbe = %v, repeat = %d, want false and the schema's own 1", args.egressProbe, args.repeat)
	}
}

func TestProbeArgsRefusalsNameTheRuleAndNeverTheValue(t *testing.T) {
	id := probeServerID
	cases := []struct {
		name string
		args string
	}{
		{"no args at all", ``},
		{"args are not an object", `["dns"]`},
		{"an undeclared field", `{"serverId":"` + id + `","layers":["dns"],"probe":"deep"}`},
		{"serverId missing", `{"layers":["dns"]}`},
		{"serverId not a string", `{"serverId":42,"layers":["dns"]}`},
		{"serverId not the schema's UUID", `{"serverId":"not-a-uuid","layers":["dns"]}`},
		{"layers missing", `{"serverId":"` + id + `"}`},
		{"layers not an array", `{"serverId":"` + id + `","layers":"dns"}`},
		{"layers empty", `{"serverId":"` + id + `","layers":[]}`},
		{"layers over the schema's ceiling", `{"serverId":"` + id + `","layers":["dns","tcp","core-outbound","proxied-http","dns"]}`},
		{"a layer name the enum does not declare", `{"serverId":"` + id + `","layers":["dns","socks-handshake"]}`},
		{"egress-ip named as a request", `{"serverId":"` + id + `","layers":["egress-ip"]}`},
		{"the same layer twice", `{"serverId":"` + id + `","layers":["dns","dns"]}`},
		{"egressProbe not a boolean", `{"serverId":"` + id + `","layers":["dns"],"egressProbe":"yes"}`},
		{"repeat not an integer", `{"serverId":"` + id + `","layers":["dns"],"repeat":"thrice"}`},
		{"repeat under the floor", `{"serverId":"` + id + `","layers":["dns"],"repeat":0}`},
		{"repeat over the ceiling", `{"serverId":"` + id + `","layers":["dns"],"repeat":11}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, refusal := readProbeArgs(requestFrame(t, tc.args))
			if refusal == nil {
				t.Fatalf("%s was accepted; the closed field list is the contract, not a suggestion", tc.name)
			}
			if refusal.Code != "HealthCheckFailed" || !strings.HasPrefix(refusal.Message, "server.test ") {
				t.Errorf("refusal %s / %q is not the command's own umbrella code with its own name", refusal.Code, refusal.Message)
			}
			// The value that broke the rule is attacker-chosen text: `framing.json#/senderPolicy/refusals`
			// forbids quoting args, and the decode table is where that rule is checkable rather than intended.
			for _, quoted := range []string{"socks-handshake", "not-a-uuid", "thrice", `"deep"`} {
				if strings.Contains(refusal.Message, quoted) {
					t.Errorf("refusal %q quotes the caller's value %s", refusal.Message, quoted)
				}
			}
		})
	}
}

func TestHandleCarriesTheFrameToTheProberAndItsAnswerOut(t *testing.T) {
	stub := &stubProber{answer: &health.Answer{ServerID: probeServerID, Overall: health.OverallPass}}
	h, err := newServerTest(stub)
	if err != nil {
		t.Fatalf("newServerTest: %v", err)
	}
	f := requestFrame(t, `{"serverId":"`+probeServerID+`","layers":["tcp","core-outbound"],"egressProbe":true,"repeat":2}`)
	ans, refusal := h.Handle(context.Background(), f)
	if refusal != nil {
		t.Fatalf("a legal frame was refused: %s %s", refusal.Code, refusal.Message)
	}
	if ans.(*health.Answer).Overall != health.OverallPass {
		t.Error("the answer out is not the answer the prober gave")
	}
	if stub.req.ServerID != probeServerID || !stub.req.Egress || stub.req.Repeat != 2 {
		t.Errorf("the prober saw %+v", stub.req)
	}
	if len(stub.req.Layers) != 2 || stub.req.Layers[0] != "tcp" || stub.req.Layers[1] != "core-outbound" {
		t.Errorf("the prober saw layers %v", stub.req.Layers)
	}
}

func TestARefusalFromTheProberKeepsTheCodeItAlreadyCarries(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"no live inbound", &health.Error{Op: "tcp", ServerID: probeServerID, Cause: health.ErrCoreNotRunning}, "CoreNotRunning"},
		{"an id the store does not have", &health.Error{Op: "args", ServerID: probeServerID, Cause: health.ErrNoServer}, "HealthCheckFailed"},
		{"the caller's budget expired", &health.Error{Op: "egress-ip", ServerID: probeServerID, Cause: health.ErrDeadline}, "Timeout"},
		{"a defect in this wiring", errors.New("nil map write"), "Internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codeForProbe(tc.err); got != tc.code {
				t.Errorf("codeForProbe = %s, want %s", got, tc.code)
			}
		})
	}
}

func TestHandleAnswersAProberRefusalWithTheCommandShape(t *testing.T) {
	stub := &stubProber{err: &health.Error{Op: "tcp", ServerID: probeServerID, Cause: health.ErrCoreNotRunning}}
	h, err := newServerTest(stub)
	if err != nil {
		t.Fatalf("newServerTest: %v", err)
	}
	f := requestFrame(t, `{"serverId":"`+probeServerID+`","layers":["tcp"]}`)
	_, refusal := h.Handle(context.Background(), f)
	if refusal == nil {
		t.Fatal("a refused probe returned an answer")
	}
	if refusal.Code != "CoreNotRunning" {
		t.Errorf("code = %s, want CoreNotRunning, which is doc 30 §4's first rule and the fixture's own code", refusal.Code)
	}
	// The message names the server so the extension can say which of the user's servers is unreachable, and it
	// stays inside `envelope.schema.json#/properties/error/properties/message/maxLength`.
	if !strings.Contains(refusal.Message, probeServerID) {
		t.Errorf("message %q does not name the server", refusal.Message)
	}
	if len(refusal.Message) > maxApplyMessage {
		t.Errorf("message is %d bytes, over the %d the envelope allows", len(refusal.Message), maxApplyMessage)
	}
}

func TestANewServerTestWithoutAProberIsRefused(t *testing.T) {
	if _, err := newServerTest(nil); err == nil {
		t.Fatal("a handler without a prober was built; it would have to invent the answer §8 reserves for a measurement")
	}
}
