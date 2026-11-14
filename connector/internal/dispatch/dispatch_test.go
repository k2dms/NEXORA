package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/contractgen"
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

// TestTheGateMessageNamesTheStateRatherThanOneHistory pins §4.13's finding. The boolean this gate reads has two
// producers: no hello has ever crossed this stream, and a hello did and was then withdrawn by a `VersionMismatch`
// (internal/hello's Session.Record). A sentence about which history produced the state is a sentence that is false
// half the time, so the message states the state and states it identically both times.
func TestTheGateMessageNamesTheStateRatherThanOneHistory(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"core.status": h}, Now: now.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := Identity{SenderID: "ext-nexora-pinned", Allowlisted: true}
	_, before := d.Dispatch(context.Background(), id, request(t, "req-gate-none-1", "core.status"))
	if before == nil || before.Code != "HandshakeFailed" {
		t.Fatalf("a gated method with no negotiation answered %+v, want HandshakeFailed", before)
	}
	id.Negotiated = true
	if _, r := d.Dispatch(context.Background(), id, request(t, "req-gate-open-1", "core.status")); r != nil {
		t.Fatalf("the negotiated sender was refused: %+v", r)
	}
	// The withdrawal lives in the session, and what reaches the front door afterwards is the boolean again -- which
	// is exactly why the wording cannot lean on which of the two histories handed it over.
	id.Negotiated = false
	_, after := d.Dispatch(context.Background(), id, request(t, "req-gate-gone-1", "core.status"))
	if after == nil || after.Code != "HandshakeFailed" {
		t.Fatalf("a withdrawn negotiation answered %+v, want HandshakeFailed", after)
	}
	if before.Message != after.Message {
		t.Errorf("the gate tells two stories for one state: %q then %q", before.Message, after.Message)
	}
	for _, phrase := range []string{"in force", "withdrawn"} {
		if !strings.Contains(after.Message, phrase) {
			t.Errorf("the message does not say %s: %q", phrase, after.Message)
		}
	}
	if strings.Contains(after.Message, "before") {
		t.Errorf("the message claims an order the state cannot support: %q", after.Message)
	}
	if *ran != 1 {
		t.Errorf("handler ran %d times, want 1 — only the negotiated request may run", *ran)
	}

	// The message goes on the wire inside an envelope, and the envelope bounds it:
	// `packages/schema/protocol/envelope.schema.json#/properties/error/properties/message/maxLength`. No refusal on
	// this path is clipped, so the bound is checked against the widest name the allowlist publishes.
	for _, name := range contract.MethodNames() {
		m, _ := contract.LookupMethod(name)
		if m.AvailableBeforeHello {
			continue
		}
		_, r := d.Dispatch(context.Background(), Identity{SenderID: strings.Repeat("e", 32), Allowlisted: true},
			request(t, "req-gate-width", name))
		if r == nil || len(r.Message) > 200 {
			t.Errorf("%s's gate message is %d characters, over the envelope's 200: %+v",
				name, len(r.Message), r)
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

	// core.status is published and this build registers it; connector.version is published and does not.
	if _, r := d.Dispatch(context.Background(), id, request(t, "req-ok-status-", "core.status")); r != nil {
		t.Fatalf("a registered method was refused: %+v", r)
	}
	_, r := d.Dispatch(context.Background(), id, request(t, "req-missing-1", "connector.version"))
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

// TestTheUnpublishedNamesInTheCorpusAreAnsweredHereNotInTheCodec is the two halves of this module meeting on the
// frames the corpus itself records against names the allowlist does not publish. internal/envelope accepts them
// — its half of the contract is a pattern, not a membership list — which is what lets them arrive here, where the
// closed set is resolved. Had the codec refused them, internal/peer would have ended the stream on the way, and
// the exchange the corpus records, a request answered UnknownMethod, could not have happened in one process.
func TestTheUnpublishedNamesInTheCorpusAreAnsweredHereNotInTheCodec(t *testing.T) {
	negatives := map[string]string{"req-000000ff": "core.snooze", "po-req-0004": "connector.ping"}

	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "packages", "schema", "fixtures", "protocol", "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixture files in the repository: %v", err)
	}
	arrived := map[string]bool{}
	recorded := map[string]*envelope.Error{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
			f, err := envelope.Parse(line)
			if err != nil {
				t.Fatalf("%s: the codec rejected a corpus frame: %v", filepath.Base(file), err)
			}
			if name, asking := negatives[f.ID]; asking && f.Type == envelope.TypeRequest && f.Method != nil && *f.Method == name {
				arrived[f.ID] = true
			}
			if f.Type == envelope.TypeResponse && f.InReplyTo != nil && f.Error != nil {
				if _, ok := negatives[*f.InReplyTo]; ok {
					recorded[*f.InReplyTo] = f.Error
				}
			}
		}
	}

	ran, h := counting(t)
	d, err := New(Config{Handlers: map[string]Handler{"core.start": h}, Now: (&clock{t: time.Unix(0, 0)}).Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := negotiated(contract.CapabilityNames()...)

	for reqID, name := range negatives {
		if !arrived[reqID] {
			t.Errorf("the corpus no longer records %s as a request for %q, so this test referees nothing", reqID, name)
			continue
		}
		_, r := d.Dispatch(context.Background(), id, request(t, reqID, name))
		if r == nil || r.Code != "UnknownMethod" {
			t.Errorf("%s asking for %q got %+v, want UnknownMethod", reqID, name, r)
			continue
		}
		answer, ok := recorded[reqID]
		if !ok {
			t.Errorf("%s is refused here and the corpus records no answer to compare", reqID)
			continue
		}
		if answer.Code != r.Code {
			t.Errorf("the corpus answers %s with %q, this package refuses %q", reqID, answer.Code, r.Code)
		}
		c, _ := contract.LookupCode(r.Code)
		if answer.Retryable != nil && *answer.Retryable != c.Retryable {
			t.Errorf("the corpus marks %s' answer retryable=%v, the taxonomy says %v", reqID, *answer.Retryable, c.Retryable)
		}
	}
	if *ran != 0 {
		t.Error("a name the allowlist does not publish reached a handler")
	}
}

func dedupeDispatcher(t *testing.T, now *clock, handlers map[string]Handler) *Dispatcher {
	t.Helper()
	d, err := New(Config{Handlers: handlers, Now: now.Now})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func answer(t *testing.T, d *Dispatcher, id Identity, f *envelope.Frame) any {
	t.Helper()
	result, r := d.Dispatch(context.Background(), id, f)
	if r != nil {
		t.Fatalf("dispatch refused: %+v", r)
	}
	return result
}

func resultJSON(t *testing.T, result any) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestConcurrentAndRepeatedRequestsShareOneStoredAnswer(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	id := negotiated(contract.CapabilityNames()...)
	started, release := make(chan struct{}), make(chan struct{})
	var runs atomic.Int32
	var d *Dispatcher
	d = dedupeDispatcher(t, now, map[string]Handler{"config.apply": func(context.Context, *envelope.Frame) (any, *Refusal) {
		n := runs.Add(1)
		// A handler may call back into dispatch counters; holding d.mu here would deadlock.
		d.Refused(id.SenderID)
		if n == 1 {
			close(started)
		}
		<-release
		return map[string]any{"applied": true, "generation": n}, nil
	}})
	f := request(t, "req-concurrent-1", "config.apply")
	type reply struct {
		result any
		r      *Refusal
	}
	const callers = 12 // Under the pipe floor, even though every reply counts against it.
	replies := make(chan reply, callers)
	launch := func() {
		result, r := d.Dispatch(context.Background(), id, f)
		replies <- reply{result, r}
	}
	go launch()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler could not read dispatch counters; d.mu deadlock")
	}
	var launched sync.WaitGroup
	launched.Add(callers - 1)
	for i := 1; i < callers; i++ {
		go func() { launched.Done(); launch() }()
	}
	launched.Wait()
	close(release)
	want := []byte(`{"applied":true,"generation":1}`)
	for i := 0; i < callers; i++ {
		select {
		case got := <-replies:
			if got.r != nil || !bytes.Equal(resultJSON(t, got.result), want) {
				t.Fatalf("concurrent answer: %s / %+v", resultJSON(t, got.result), got.r)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("identical calls did not complete; waiter or d.mu deadlock")
		}
	}
	if got := resultJSON(t, answer(t, d, id, f)); !bytes.Equal(got, want) || runs.Load() != 1 {
		t.Fatalf("repeat got %s, side effects=%d, want the stored result and one effect", got, runs.Load())
	}
}

func TestImportARetryDoesNotOverwriteLaterCredentialB(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	writes, protected := 0, ""
	d := dedupeDispatcher(t, now, map[string]Handler{"profile.import": func(_ context.Context, f *envelope.Frame) (any, *Refusal) {
		// A protected-write stand-in: real Store/profileImport is outside dispatch's dependencies.
		var args struct {
			Credential string `json:"credential"`
		}
		if err := json.Unmarshal(f.Args, &args); err != nil {
			return nil, &Refusal{Code: "BadRequest", Message: "invalid import"}
		}
		protected = args.Credential
		writes++
		return map[string]any{"applied": true, "write": writes}, nil
	}})
	id := negotiated(contract.CapabilityNames()...)
	a, b := request(t, "req-import-A", "profile.import"), request(t, "req-import-B", "profile.import")
	a.Args, b.Args = json.RawMessage(`{"credential":"X"}`), json.RawMessage(`{"credential":"Y"}`)
	storedA := resultJSON(t, answer(t, d, id, a))
	answer(t, d, id, b)
	// Both named mutation slots are spent. Same id+method means the stored answer, not
	// a second write or an args digest comparison, even if the caller changes its args.
	a.Args = json.RawMessage(`{"credential":"must-not-be-written"}`)
	replay := resultJSON(t, answer(t, d, id, a))
	if writes != 2 || protected != "Y" || !bytes.Equal(replay, storedA) {
		t.Fatalf("A/B/replay A: writes=%d, protected=%q, replay=%s, stored=%s", writes, protected, replay, storedA)
	}
	cache := d.responses[id.SenderID]
	if strings.Contains(string(cache.entries[a.ID].result), "credential") || strings.Contains(string(cache.entries[a.ID].result), "must-not-be-written") {
		t.Fatal("response cache retained request/credential material")
	}
}

func TestDedupeExpiryStartsAtFirstCompletionAndReplayDoesNotExtendIt(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	runs := 0
	d := dedupeDispatcher(t, now, map[string]Handler{"config.apply": func(context.Context, *envelope.Frame) (any, *Refusal) {
		runs++
		if runs == 1 {
			now.After(2 * time.Minute) // Time in the handler is not deducted from the response window.
		}
		return runs, nil
	}})
	id := negotiated(contract.CapabilityNames()...)
	f := request(t, "req-expiry-1", "config.apply")
	want := resultJSON(t, answer(t, d, id, f))
	completed := now.Now()
	now.After(time.Duration(contract.DedupeWindowSeconds)*time.Second - time.Second)
	if got := resultJSON(t, answer(t, d, id, f)); !bytes.Equal(got, want) || runs != 1 {
		t.Fatalf("reply expired before its completion-based window: %s, runs=%d", got, runs)
	}
	if !d.responses[id.SenderID].entries[f.ID].completed.Equal(completed) {
		t.Fatal("replay extended the expiry")
	}
	now.After(time.Second)
	answer(t, d, id, f)
	if runs != 2 || d.DedupeStats().AgeEvictions != 1 {
		t.Fatalf("expiry boundary: runs=%d, stats=%+v", runs, d.DedupeStats())
	}
}

func TestEntryEvictionIsBoundedObservableAndNotReplayLRU(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	runs, h := counting(t)
	d := dedupeDispatcher(t, now, map[string]Handler{"connector.version": h})
	id := negotiated(contract.CapabilityNames()...)
	first := request(t, "req-entry-0000", "connector.version")
	for i := 0; i < contract.DedupeMaxEntries; i++ {
		answer(t, d, id, request(t, fmt.Sprintf("req-entry-%04d", i), "connector.version"))
		now.After(time.Second) // Stay under the pipe floor and inside the 300s window.
	}
	answer(t, d, id, first) // Does not promote it past a later first completion.
	answer(t, d, id, request(t, "req-entry-extra", "connector.version"))
	cache := d.responses[id.SenderID]
	if len(cache.entries) != contract.DedupeMaxEntries || cache.entries[first.ID] != nil || d.DedupeStats().EntryEvictions != 1 {
		t.Fatalf("entry bound/oldest eviction: entries=%d, stats=%+v", len(cache.entries), d.DedupeStats())
	}
	answer(t, d, id, first)
	if *runs != contract.DedupeMaxEntries+2 || len(cache.entries) > contract.DedupeMaxEntries {
		t.Fatalf("evicted request did not run fresh within the bound: runs=%d, entries=%d", *runs, len(cache.entries))
	}
}

func TestByteEvictionAndOversizedResponsesStayBounded(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	runs := 0
	payload := strings.Repeat("x", contract.HostToExtensionBytes*3/4)
	d := dedupeDispatcher(t, now, map[string]Handler{"connector.version": func(context.Context, *envelope.Frame) (any, *Refusal) {
		runs++
		return payload, nil
	}})
	id := negotiated(contract.CapabilityNames()...)
	size := len(resultJSON(t, payload))
	fit := contract.DedupeMaxBytes / size
	first := request(t, "req-bytes-0000", "connector.version")
	for i := 0; i <= fit; i++ {
		answer(t, d, id, request(t, fmt.Sprintf("req-bytes-%04d", i), "connector.version"))
		cache := d.responses[id.SenderID]
		if cache.bytes > contract.DedupeMaxBytes || len(cache.entries) > contract.DedupeMaxEntries {
			t.Fatalf("dedupe exceeded its bounds: bytes=%d, entries=%d", cache.bytes, len(cache.entries))
		}
		now.After(time.Second)
	}
	cache := d.responses[id.SenderID]
	if cache.entries[first.ID] != nil || d.DedupeStats().ByteEvictions != 1 {
		t.Fatalf("byte eviction did not remove/observe oldest answer: %+v", d.DedupeStats())
	}
	answer(t, d, id, first)
	if runs != fit+2 {
		t.Fatalf("byte-evicted request was replayed, runs=%d", runs)
	}

	// One answer larger than the entire byte budget is delivered but cannot be retained.
	payload = strings.Repeat("z", contract.DedupeMaxBytes+1)
	oversized := request(t, "req-bytes-large", "connector.version")
	beforeBytes, beforeEntries := cache.bytes, len(cache.entries)
	beforeEvictions := d.DedupeStats().ByteEvictions
	answer(t, d, id, oversized)
	answer(t, d, id, oversized)
	if cache.bytes != beforeBytes || len(cache.entries) != beforeEntries || cache.entries[oversized.ID] != nil || d.DedupeStats().ByteEvictions != beforeEvictions+2 {
		t.Fatalf("oversized answer poisoned/displaced the cache: bytes=%d, entries=%d, stats=%+v", cache.bytes, len(cache.entries), d.DedupeStats())
	}
}

func TestAdmissionIsRecheckedBeforeCachedAnswersAndCollisions(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	runs, h := counting(t)
	d := dedupeDispatcher(t, now, map[string]Handler{"config.snapshot": h})
	id := negotiated(contract.CapabilityNames()...)
	f := request(t, "req-admission-1", "config.snapshot")
	want := resultJSON(t, answer(t, d, id, f))
	for _, gate := range []struct {
		name, code string
		change     func(*Identity)
	}{
		{"unauthorized", "UnauthorizedExtension", func(id *Identity) { id.Allowlisted = false }},
		{"hello withdrawn", "HandshakeFailed", func(id *Identity) { id.Negotiated = false }},
		{"capability withdrawn", "HandshakeFailed", func(id *Identity) { id.Capabilities = nil }},
	} {
		t.Run(gate.name, func(t *testing.T) {
			withdrawn := id
			gate.change(&withdrawn)
			result, r := d.Dispatch(context.Background(), withdrawn, f)
			if result != nil || r == nil || r.Code != gate.code {
				t.Fatalf("cached answer bypassed admission: result=%v, refusal=%+v", result, r)
			}
		})
	}
	for method, code := range map[string]string{"connector.version": "Internal", "core.snooze": "UnknownMethod"} {
		result, r := d.Dispatch(context.Background(), id, request(t, f.ID, method))
		if result != nil || r == nil || r.Code != code {
			t.Fatalf("%s did not precede collision lookup: %v / %+v", method, result, r)
		}
	}
	if got := resultJSON(t, answer(t, d, id, f)); !bytes.Equal(got, want) || *runs != 1 {
		t.Fatal("admission refusals damaged the original stored answer")
	}
}

func TestMethodCollisionIsSanitizedIncludingInFlightAndProbe(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	runs, h := counting(t)
	d := dedupeDispatcher(t, now, map[string]Handler{"config.apply": h, "core.stop": h, "server.test": h})
	id := negotiated(contract.CapabilityNames()...)
	f := request(t, "req-secret-collision", "config.apply")
	m, _ := contract.LookupMethod("config.apply")
	entry, _, r := d.beginResponse(context.Background(), id, f.ID, m)
	if r != nil || entry == nil {
		t.Fatalf("reserve: %+v", r)
	}
	for _, completed := range []bool{false, true} {
		if completed {
			d.completeResponse(id.SenderID, f.ID, entry, json.RawMessage(`{"applied":true}`))
		}
		for _, method := range []string{"core.stop", "server.test"} {
			collision := request(t, f.ID, method)
			collision.Args = json.RawMessage(`{"credential":"secret-collision-value"}`)
			result, r := d.Dispatch(context.Background(), id, collision)
			if result != nil || r == nil || r.Code != "BadRequest" || r.Message != "request id is already bound to another method" {
				t.Fatalf("collision completed=%v: %v / %+v", completed, result, r)
			}
			if strings.Contains(r.Message, f.ID) || strings.Contains(r.Message, "secret-collision-value") {
				t.Fatal("collision answer disclosed request material")
			}
		}
	}
	if *runs != 0 {
		t.Fatal("colliding method reached a side effect")
	}
}

func TestSenderScopeAndNewDispatcher(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	runs, h := counting(t)
	handlers := map[string]Handler{"profile.import": h}
	d := dedupeDispatcher(t, now, handlers)
	one, two := negotiated(contract.CapabilityNames()...), negotiated(contract.CapabilityNames()...)
	two.SenderID = "ext-another-pinned"
	f := request(t, "req-sender-key", "profile.import")
	answer(t, d, one, f)
	answer(t, d, two, f)
	// A new identity snapshot shares the sender key only within this dispatcher. This
	// does not assert Connector/port adoption: creating a new dispatcher below loses the cache.
	renewed := negotiated(contract.CapabilityNames()...)
	answer(t, d, renewed, f)
	answer(t, d, two, f)
	if *runs != 2 {
		t.Fatalf("per-sender/port scope: runs=%d, want two", *runs)
	}
	fresh := dedupeDispatcher(t, now, handlers)
	answer(t, fresh, one, f)
	if *runs != 3 {
		t.Fatal("a new Connector dispatcher inherited a response cache")
	}
}

func TestProbeAndHelloAreNeverCachedAndFlagsGateDedupe(t *testing.T) {
	for _, m := range []contract.Method{
		{Name: "config.apply", Replayable: false, Idempotent: true},
		{Name: "config.apply", Replayable: true, Idempotent: false},
		{Name: "server.test", Replayable: true, Idempotent: true},
		{Name: "connector.hello", Replayable: true, Idempotent: true, AvailableBeforeHello: true},
	} {
		if cacheable(m) {
			t.Errorf("dedupe admitted %+v", m)
		}
	}
	now := &clock{t: time.Unix(0, 0)}
	probeRuns, probe := counting(t)
	helloRuns, hello := counting(t)
	d := dedupeDispatcher(t, now, map[string]Handler{"server.test": probe, "connector.hello": hello})
	id := negotiated(contract.CapabilityNames()...)
	f := request(t, "req-probe-repeat", "server.test")
	rl, _ := contract.RateLimitFor("server.test")
	for i := 0; i < rl.MaxRequests; i++ {
		answer(t, d, id, f)
	}
	if _, r := d.Dispatch(context.Background(), id, f); r == nil || r.Code != "Busy" || *probeRuns != rl.MaxRequests {
		t.Fatalf("repeated probe did not run/pay its budget each time: runs=%d, refusal=%+v", *probeRuns, r)
	}
	for i := 0; i < 2; i++ {
		answer(t, d, Identity{SenderID: id.SenderID, Allowlisted: true}, request(t, "req-hello-repeat", "connector.hello"))
	}
	if *helloRuns != 2 || len(d.responses) != 0 {
		t.Fatal("probe or per-connection hello was cached")
	}
}

func TestHandlerFailuresAreNotCachedOrCountedAsFrontDoorRefusals(t *testing.T) {
	for _, code := range []string{"Busy", "BadRequest", "CoreNotRunning"} {
		t.Run(code, func(t *testing.T) {
			now := &clock{t: time.Unix(0, 0)}
			runs := 0
			d := dedupeDispatcher(t, now, map[string]Handler{"core.status": func(context.Context, *envelope.Frame) (any, *Refusal) {
				runs++
				if runs == 1 {
					return nil, &Refusal{Code: code, Message: "handler refused"}
				}
				return runs, nil
			}})
			id := negotiated(contract.CapabilityNames()...)
			f := request(t, "req-error-retry", "core.status")
			if _, r := d.Dispatch(context.Background(), id, f); r == nil || r.Code != code {
				t.Fatalf("handler failure: %+v", r)
			}
			want := resultJSON(t, answer(t, d, id, f))
			if got := resultJSON(t, answer(t, d, id, f)); !bytes.Equal(got, want) || runs != 2 || d.Refused(id.SenderID) != 0 {
				t.Fatalf("failure poisoned retry or guard: result=%s, runs=%d", got, runs)
			}
		})
	}
}

func TestCachedReplayBypassesMutationBudgetButPaysPipeFloor(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	runs, h := counting(t)
	d := dedupeDispatcher(t, now, map[string]Handler{"core.start": h})
	id := negotiated(contract.CapabilityNames()...)
	rl, _ := contract.RateLimitFor("core.start")
	floor := contract.CatchAllRateLimit()
	f := request(t, "req-rate-0000", "core.start")
	for i := 0; i < rl.MaxRequests; i++ {
		answer(t, d, id, request(t, fmt.Sprintf("req-rate-%04d", i), "core.start"))
	}
	for i := rl.MaxRequests; i < floor.MaxRequests; i++ {
		answer(t, d, id, f)
	}
	if _, r := d.Dispatch(context.Background(), id, f); r == nil || r.Code != "Busy" {
		t.Fatalf("replay bypassed pipe floor: %+v", r)
	}
	if *runs != rl.MaxRequests {
		t.Fatalf("replays repeated mutations: %d", *runs)
	}
	now.After(time.Second)
	for i := 0; i < floor.MaxRequests; i++ {
		answer(t, d, id, f)
	}
	if _, r := d.Dispatch(context.Background(), id, f); r == nil || r.Code != "Busy" {
		t.Fatalf("next-window replay bypassed the pipe floor: %+v", r)
	}
	fresh := request(t, "req-rate-fresh", "core.start")
	if _, r := d.Dispatch(context.Background(), id, fresh); r == nil || r.Code != "Busy" {
		t.Fatalf("a new mutation bypassed the still-spent method budget: %+v", r)
	}
	if d.responses[id.SenderID].entries[fresh.ID] != nil {
		t.Fatal("budget refusal left a cached answer/reservation")
	}
	now.After(time.Duration(rl.WindowSeconds) * time.Second)
	answer(t, d, id, fresh)
	if *runs != rl.MaxRequests+1 {
		t.Fatal("budget Busy poisoned a later same-id retry")
	}
}

func TestMutableHandlerAndReplayResultsCannotChangeStoredBytes(t *testing.T) {
	for _, shape := range []string{"map", "raw", "nil"} {
		t.Run(shape, func(t *testing.T) {
			now := &clock{t: time.Unix(0, 0)}
			shared := map[string]any{"nested": []any{map[string]any{"value": "original"}}}
			raw := json.RawMessage(`{"value":"original"}`)
			var returned any
			switch shape {
			case "map":
				returned = shared
			case "raw":
				returned = raw
			}
			d := dedupeDispatcher(t, now, map[string]Handler{"config.snapshot": func(context.Context, *envelope.Frame) (any, *Refusal) {
				return returned, nil
			}})
			id := negotiated(contract.CapabilityNames()...)
			f := request(t, "req-mutable-1", "config.snapshot")
			first := answer(t, d, id, f)
			want := resultJSON(t, first)
			shared["nested"].([]any)[0].(map[string]any)["value"] = "modified"
			raw[0] = '['
			replay, ok := answer(t, d, id, f).(json.RawMessage)
			if !ok || !bytes.Equal(replay, want) {
				t.Fatalf("handler alias changed cached bytes: %s, want %s", replay, want)
			}
			replay[0] = '['
			if next := resultJSON(t, answer(t, d, id, f)); !bytes.Equal(next, want) {
				t.Fatalf("caller mutated stored replay: %s, want %s", next, want)
			}
		})
	}
}

func TestInFlightReservationsAreBoundedNeverEvictedAndWaitsCanCancel(t *testing.T) {
	now := &clock{t: time.Unix(0, 0)}
	d := dedupeDispatcher(t, now, nil)
	id := negotiated(contract.CapabilityNames()...)
	m, _ := contract.LookupMethod("config.apply")
	var first *cachedResponse
	for i := 0; i < contract.DedupeMaxEntries; i++ {
		entry, replay, r := d.beginResponse(context.Background(), id, fmt.Sprintf("req-pending-%04d", i), m)
		if entry == nil || replay != nil || r != nil {
			t.Fatalf("reservation %d: %+v", i, r)
		}
		if i == 0 {
			first = entry
		}
	}
	now.After(time.Duration(contract.DedupeWindowSeconds+1) * time.Second)
	if _, _, r := d.beginResponse(context.Background(), id, "req-pending-over", m); r == nil || r.Code != "Busy" {
		t.Fatalf("all in-flight slots were not bounded: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, r := d.beginResponse(ctx, id, "req-pending-0000", m); r == nil || r.Code != "Busy" {
		t.Fatalf("canceled waiter did not return retryable Busy: %+v", r)
	}
	if d.DedupeStats() != (DedupeStats{}) || len(d.responses[id.SenderID].entries) != contract.DedupeMaxEntries {
		t.Fatal("pending entries aged out or were evicted")
	}
	d.completeResponse(id.SenderID, "req-pending-0000", first, json.RawMessage(`{"applied":true}`))
	if entry, _, r := d.beginResponse(context.Background(), id, "req-pending-over", m); entry == nil || r != nil || d.DedupeStats().EntryEvictions != 1 {
		t.Fatalf("completed entry could not free a slot: %+v, stats=%+v", r, d.DedupeStats())
	}
}

func TestPanicAndEncodingFailureReleaseReservations(t *testing.T) {
	for _, failure := range []string{"panic", "encoding"} {
		t.Run(failure, func(t *testing.T) {
			now := &clock{t: time.Unix(0, 0)}
			runs := 0
			d := dedupeDispatcher(t, now, map[string]Handler{"core.status": func(context.Context, *envelope.Frame) (any, *Refusal) {
				runs++
				if runs == 1 {
					if failure == "panic" {
						panic("handler panic")
					}
					return make(chan int), nil
				}
				return "recovered", nil
			}})
			id := negotiated(contract.CapabilityNames()...)
			f := request(t, "req-failure-1", "core.status")
			func() {
				defer func() {
					if failure == "panic" && recover() == nil {
						t.Error("expected the handler panic to propagate")
					}
				}()
				if _, r := d.Dispatch(context.Background(), id, f); failure == "encoding" && (r == nil || r.Code != "Internal" || r.Message != "handler result could not be encoded") {
					t.Fatalf("encoding failure was not sanitized: %+v", r)
				}
			}()
			answer(t, d, id, f)
			answer(t, d, id, f)
			if runs != 2 {
				t.Fatalf("failed result poisoned the reservation, runs=%d", runs)
			}
		})
	}
}
