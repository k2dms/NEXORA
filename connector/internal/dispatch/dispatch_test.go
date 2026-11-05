package dispatch

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
)

// clock is the contract's own unit of time for these tests: the budgets are declared in seconds, so a test that
// advances a fake clock by the contract number says something the wall clock cannot.
type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func (c *clock) After(d time.Duration) { c.t = c.t.Add(d) }

// request builds the frame the peer would hand over after envelope.Parse accepted it. Dispatch never re-validates
// an envelope — that is the codec's job, already done — so these frames are built rather than issued.
func request(t *testing.T, id, method string) *envelope.Frame {
	t.Helper()
	if len(id) < 8 {
		t.Fatalf("request id %q is below the envelope's eight-character floor", id)
	}
	m := method
	return &envelope.Frame{V: contract.FrameFormatVersion, ID: id, Type: envelope.TypeRequest, Method: &m,
		SentAt: envelope.Now()}
}

// negotiated builds a sender the host allowlisted and whose hello succeeded, announcing the capability names given.
// The full registry is what a real hello reports today — asserted in both directions by
// packages/schema/scripts/verify.mjs — so a test that wants a gate to be open says so with that call.
func negotiated(capNames ...string) Identity {
	return Identity{SenderID: "ext-nexora-pinned", Allowlisted: true, Negotiated: true, Capabilities: capNames}
}

func counting(t *testing.T) (*int, Handler) {
	t.Helper()
	n := 0
	return &n, func(context.Context, *envelope.Frame) (any, *Refusal) {
		n++
		return "handled", nil
	}
}

func TestTheCallerIsResolvedBeforeTheMethodIsNamed(t *testing.T) {
	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"connector.hello": h}, Now: (&clock{t: time.Unix(0, 0)}).Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// An unallowlisted sender asking for something that does not even exist gets the sender error, not the method
	// error: ADR-0004 rule 6 answers it "only with UnauthorizedExtension", which also means it learns no surface.
	_, r := d.Dispatch(context.Background(), Identity{SenderID: "ext-someone-elses"}, request(t, "req-foreign-1", "core.start"))
	if r == nil || r.Code != "UnauthorizedExtension" {
		t.Fatalf("foreign sender got %+v, want UnauthorizedExtension", r)
	}
	if c, _ := contract.LookupCode(r.Code); c.Retryable {
		t.Error("UnauthorizedExtension is retryable, so an auto-retry becomes a persistent attempt on the keystore's pipe")
	}
	if *ran != 0 {
		t.Error("a refused sender reached a handler")
	}

	// The same sender asking a real method gets the same single answer, and the unknown-sender rule is what makes
	// "does not touch state" checkable rather than asserted.
	_, r = d.Dispatch(context.Background(), Identity{SenderID: "ext-someone-elses"}, request(t, "req-foreign-2", "connector.hello"))
	if r == nil || r.Code != "UnauthorizedExtension" {
		t.Fatalf("a foreign sender asking hello got %+v, want the same sender error", r)
	}
}

func TestAnUnknownMethodIsRefusedWithNoFallthrough(t *testing.T) {
	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"core.start": h}, Now: (&clock{t: time.Unix(0, 0)}).Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := negotiated(contract.CapabilityNames()...)

	// deniedByDefault means the closest match is not a match: this name differs from a published method by one
	// character and must still be refused.
	for _, name := range []string{"core.starte", "core.Start", "start", ""} {
		_, r := d.Dispatch(context.Background(), id, request(t, "req-unknown-01", name))
		if r == nil || r.Code != "UnknownMethod" {
			t.Errorf("%q got %+v, want UnknownMethod", name, r)
		}
	}
	if *ran != 0 {
		t.Error("an unknown method reached the handler for the method it resembles")
	}
}

func TestNothingIsDispatchableBeforeHelloExceptHello(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	ids := map[string]*int{}
	handlers := map[string]Handler{}
	for _, name := range contract.MethodNames() {
		ran, h := counting(t)
		ids[name] = ran
		handlers[name] = h
	}
	d, err := New(Config{Handlers: handlers, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A sender that has not negotiated: gated methods get HandshakeFailed, and hello alone is admitted. Each name
	// gets its own sender because the amplification guard counts refusals per sender, and nineteen gated requests
	// from one identity would be a test of the guard rather than of this gate.
	for _, name := range contract.MethodNames() {
		pre := Identity{SenderID: "ext-gated-" + strings.TrimPrefix(name, "connector."), Allowlisted: true}
		_, r := d.Dispatch(context.Background(), pre, request(t, "req-prehello-01", name))
		m, _ := contract.LookupMethod(name)
		if m.AvailableBeforeHello {
			if r != nil {
				t.Errorf("%s is availableBeforeHello but was refused %v", name, r)
			}
			continue
		}
		if r == nil || r.Code != "HandshakeFailed" {
			t.Errorf("%s arrived before hello and got %+v, want HandshakeFailed", name, r)
		}
	}
	for _, name := range contract.MethodNames() {
		if m, _ := contract.LookupMethod(name); m.AvailableBeforeHello {
			continue
		}
		if *ids[name] != 0 {
			t.Errorf("%s ran its handler before the negotiation that gates it", name)
		}
	}
}

func TestAMethodNeedsTheCapabilityItsHelloAnnounced(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"config.snapshot": h}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m, ok := contract.LookupMethod("config.snapshot")
	if !ok || len(m.Capabilities) == 0 {
		t.Fatalf("config.snapshot carries %v capabilities; this test needs it to need one", m.Capabilities)
	}

	// Negotiated, but a hello that announced nothing: the §7 rule is that a command with a non-empty
	// capabilities[] MUST NOT be sent until hello reported each name, and doc 31 puts a negotiation that did not
	// agree on the HandshakeFailed side of the two-code split.
	if _, r := d.Dispatch(context.Background(), negotiated(), request(t, "req-nocaps-01", "config.snapshot")); r == nil ||
		r.Code != "HandshakeFailed" {
		t.Fatalf("an unannounced capability got %+v, want HandshakeFailed", r)
	}
	if _, r := d.Dispatch(context.Background(), negotiated(m.Capabilities...),
		request(t, "req-caps-0001", "config.snapshot")); r != nil {
		t.Fatalf("the announced capability was still refused: %+v", r)
	}
	if *ran != 1 {
		t.Errorf("handler ran %d times, want 1 — only the admitted request may run", *ran)
	}
}

func TestABudgetRefusalIsBusyRetryableAndReachesNoHandler(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"server.test": h}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := negotiated(contract.CapabilityNames()...)
	rl, ok := contract.RateLimitFor("server.test")
	if !ok {
		t.Fatal("the projection gives server.test no per-sender budget")
	}

	for i := 0; i < rl.MaxRequests; i++ {
		if _, r := d.Dispatch(context.Background(), id, request(t, fmt.Sprintf("req-probe-%04d", i), "server.test")); r != nil {
			t.Fatalf("request %d of %d was refused: %+v", i+1, rl.MaxRequests, r)
		}
	}
	_, r := d.Dispatch(context.Background(), id, request(t, "req-probe-over", "server.test"))
	if r == nil || r.Code != "Busy" {
		t.Fatalf("the request past the budget got %+v, want Busy", r)
	}
	if c, _ := contract.LookupCode(r.Code); !c.Retryable {
		t.Error("a rate-limit refusal answered with a non-retryable code cannot be told 'back off' from 'no'")
	}
	if *ran != rl.MaxRequests {
		t.Errorf("the handler ran %d times against a budget of %d — the budget is not a form of address", *ran, rl.MaxRequests)
	}
	if strings.Contains(r.Message, "QuotaExceeded") {
		t.Error("a budget refusal was dressed as a storage error")
	}

	// The window is a window: the same sender is admitted again once the oldest requests leave it.
	now.After(time.Duration(rl.WindowSeconds) * time.Second)
	if _, r := d.Dispatch(context.Background(), id, request(t, "req-probe-after", "server.test")); r != nil {
		t.Fatalf("%ds after the window a probe is still refused: %+v", rl.WindowSeconds, r)
	}
}

func TestTheCatchAllFloorIsUnderEveryNamedBudget(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	ran, h := counting(t)
	// connector.version carries no named budget, so only the "*" row bounds it.
	if _, ok := contract.RateLimitFor("connector.version"); ok {
		t.Fatal("connector.version has a named budget; this test is about the floor")
	}
	d, err := New(Config{Handlers: map[string]Handler{"connector.version": h}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := negotiated(contract.CapabilityNames()...)
	floor := contract.CatchAllRateLimit()

	for i := 0; i < floor.MaxRequests; i++ {
		if _, r := d.Dispatch(context.Background(), id, request(t, fmt.Sprintf("req-read-%06d", i), "connector.version")); r != nil {
			t.Fatalf("read %d of %d refused: %+v", i+1, floor.MaxRequests, r)
		}
	}
	_, r := d.Dispatch(context.Background(), id, request(t, "req-read-over", "connector.version"))
	if r == nil || r.Code != "Busy" {
		t.Fatalf("past the catch-all floor got %+v, want Busy", r)
	}
	if *ran != floor.MaxRequests {
		t.Errorf("handler ran %d times, want the floor %d", *ran, floor.MaxRequests)
	}

	// One second, not the whole window: this budget is the protection against a spinning worker.
	now.After(time.Second)
	if _, r := d.Dispatch(context.Background(), id, request(t, "req-read-next", "connector.version")); r != nil {
		t.Fatalf("the one-second floor did not slide: %+v", r)
	}
}

func TestABudgetIsPerSenderAndNotPerProcess(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"diagnostics.get": h}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rl, ok := contract.RateLimitFor("diagnostics.get")
	if !ok || rl.MaxRequests != 1 {
		t.Fatalf("diagnostics.get budget %+v, want a named budget of one request", rl)
	}
	first := negotiated(contract.CapabilityNames()...)
	second := first
	first.SenderID, second.SenderID = "ext-nexora-one-", "ext-nexora-two-"

	if _, r := d.Dispatch(context.Background(), first, request(t, "req-diag-first", "diagnostics.get")); r != nil {
		t.Fatalf("the first diagnostics.get was refused: %+v", r)
	}
	if _, r := d.Dispatch(context.Background(), first, request(t, "req-diag-secon", "diagnostics.get")); r == nil || r.Code != "Busy" {
		t.Fatalf("the same sender's second call got %+v, want Busy", r)
	}
	// scopeOfBudget is "per sender id, per Connector process" — a second sender shares the process, not the budget.
	if _, r := d.Dispatch(context.Background(), second, request(t, "req-diag-other", "diagnostics.get")); r != nil {
		t.Fatalf("a different sender paid for the first one's call: %+v", r)
	}
	if *ran != 2 {
		t.Errorf("the handler ran %d times, want 2 — one per sender", *ran)
	}
}

func TestTenRefusalsInTenSecondsClosesThePort(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"connector.hello": h}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	foreign := Identity{SenderID: "ext-not-allowlisted"}

	for i := 0; i < contract.RefusalsPerWindowMax; i++ {
		_, r := d.Dispatch(context.Background(), foreign, request(t, fmt.Sprintf("req-loop-%06d", i), "core.start"))
		if r == nil || r.Code != "UnauthorizedExtension" || r.Close {
			t.Fatalf("refusal %d got %+v, want the sender error and an open port", i+1, r)
		}
	}
	_, r := d.Dispatch(context.Background(), foreign, request(t, "req-loop-over-", "core.start"))
	if r == nil || !r.Close {
		t.Fatalf("the request past the guard got %+v, want the port to close", r)
	}
	if r.Code != "" {
		t.Errorf("the guard answered %q; its rule is close the port rather than keep answering", r.Code)
	}
	if got := d.Refused(foreign.SenderID); got <= contract.RefusalsPerWindowMax {
		t.Errorf("Refused() = %d, want the count the guard tripped on", got)
	}
	if *ran != 0 {
		t.Error("a sender under the guard reached a handler")
	}

	// The guard window slides too, and closing the pipe is not a shutdown signal: the dispatcher keeps working.
	now.After(time.Duration(contract.RefusalWindowSeconds) * time.Second)
	if got := d.Refused(foreign.SenderID); got != 0 {
		t.Errorf("Refused() = %d after the window, want 0", got)
	}
	if _, r := d.Dispatch(context.Background(), foreign, request(t, "req-loop-after", "core.start")); r == nil || r.Close {
		t.Errorf("after the window the guard still closes the port: %+v", r)
	}
}

func TestAHandlerErrorIsAnAnswerNotARefusal(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	failing := func(context.Context, *envelope.Frame) (any, *Refusal) {
		return nil, &Refusal{Code: "CoreNotRunning", Message: "Core is not running"}
	}
	d, err := New(Config{Handlers: map[string]Handler{"core.status": failing}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := negotiated(contract.CapabilityNames()...)
	if _, ok := contract.RateLimitFor("core.status"); ok {
		t.Fatal("core.status has a named budget; this test must not be measuring that instead")
	}

	// Twelve of one method, all admitted by the one-second floor of twenty, all answered by a handler that fails.
	// A method that returns an error is an answer the user needs, so it must not spend the guard's refusal budget
	// and must not be able to close the sender's port.
	for i := 0; i < 12; i++ {
		_, r := d.Dispatch(context.Background(), id, request(t, fmt.Sprintf("req-status-%05d", i), "core.status"))
		if r == nil || r.Code != "CoreNotRunning" {
			t.Fatalf("request %d got %+v, want the handler's own answer", i+1, r)
		}
		if r.Close {
			t.Fatalf("a failing handler closed the port at request %d", i+1)
		}
	}
	if got := d.Refused(id.SenderID); got != 0 {
		t.Errorf("Refused() = %d, want 0 — handler failures are not front-door refusals", got)
	}
}

func TestAPublishedMethodWithNoHandlerSaysInternal(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	d, err := New(Config{Handlers: map[string]Handler{"core.status": func(context.Context, *envelope.Frame) (any, *Refusal) {
		return "ok", nil
	}}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := negotiated(contract.CapabilityNames()...)

	// core.status is published and this build registers it; core.select is published and does not.
	if _, r := d.Dispatch(context.Background(), id, request(t, "req-ok-status-", "core.status")); r != nil {
		t.Fatalf("a registered method was refused: %+v", r)
	}
	_, r := d.Dispatch(context.Background(), id, request(t, "req-missing-1", "core.select"))
	if r == nil || r.Code != "Internal" {
		t.Fatalf("an unregistered published method got %+v, want Internal", r)
	}
	if got := d.Methods(); len(got) != 1 || got[0] != "core.status" {
		t.Errorf("Methods() = %v, want the surface this build actually has", got)
	}

	// A handler for a name the contract does not publish is a build error, not a runtime refusal: it would be the
	// fallthrough deniedByDefault forbids, wearing a Go map.
	if _, err := New(Config{Handlers: map[string]Handler{"core.stoppe": func(context.Context, *envelope.Frame) (any, *Refusal) {
		return nil, nil
	}}}); err == nil {
		t.Fatal("New accepted a handler for a method the allowlist does not publish")
	}
}

func TestANonRequestFrameIsNotDispatched(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"connector.hello": h}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := negotiated(contract.CapabilityNames()...)

	f := request(t, "req-event-0001", "connector.hello")
	f.Type = envelope.TypeEvent
	if _, r := d.Dispatch(context.Background(), id, f); r == nil || r.Code != "BadRequest" {
		t.Fatalf("an event frame got %+v, want BadRequest", r)
	}
	if *ran != 0 {
		t.Error("a frame that is not a request reached a handler")
	}
}
