package peer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/frames"
)

// newPipe returns the two ends of an in-memory pipe. Closing one end makes the other read io.EOF, which is how
// a browser hanging up on a native-messaging port is modelled here.
func newPipe(t *testing.T) (io.ReadWriteCloser, io.ReadWriteCloser) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	return a, b
}

// drain keeps the far end reading so a write never blocks on a peer this test does not serve.
func drain(t *testing.T, end io.ReadWriteCloser) {
	t.Helper()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := end.Read(buf); err != nil {
				return
			}
		}
	}()
}

// clock is the liveness test's lever: the deadline arithmetic is the contract's own 25 s, and no test may sleep
// for 75 seconds to reach it.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) After(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// requestBytes marshals a request frame, so a test can size its budget against what actually goes on the wire.
func requestBytes(t *testing.T, id, method string, args any) []byte {
	t.Helper()
	f, err := envelope.NewRequest(id, method, args)
	if err != nil {
		t.Fatalf("NewRequest(%s): %v", id, err)
	}
	data, err := f.Marshal()
	if err != nil {
		t.Fatalf("Marshal(%s): %v", id, err)
	}
	return data
}

// answerBytes marshals the response a connector would send to requestID.
func answerBytes(t *testing.T, requestID string) []byte {
	t.Helper()
	req, err := envelope.NewRequest(requestID, "config.apply", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res, err := req.Respond(json.RawMessage(`{"applied":true}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := res.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// waitFor polls a condition the way these tests need it: a reservation is visible immediately, a delivery is
// not, and a fixed sleep would be the flake. The budget is five seconds rather than one because run
// `42086433856` lost a poll that a local machine wins a hundred times in a row: `go test ./...` runs every
// package's binary at once on a two- to four-core runner, and a goroutine that had not been scheduled inside
// the old second was reported as a peer that had not reserved.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 2500; i++ {
		if ok() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// consume reads n bytes straight from the far end of the pipe, with a deadline so that a frame which never
// arrives is reported as that rather than as an infinite wait. `net.Pipe` has no buffer: the writer's `Write`
// returns only once the reader has taken the whole frame, which is what makes this an event instead of a poll.
func consume(t *testing.T, end io.ReadWriteCloser, n int) {
	t.Helper()
	c, ok := end.(net.Conn)
	if !ok {
		t.Fatalf("the pipe end is a %T, not a net.Conn with a read deadline", end)
	}
	if err := c.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("setting the read deadline: %v", err)
	}
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	if _, err := io.ReadFull(c, make([]byte, n)); err != nil {
		t.Fatalf("reading the %d bytes the peer issued: %v", n, err)
	}
}

func TestTheBudgetsAreTheContractsAndCrossTheTwoDirections(t *testing.T) {
	toExt, toHost := ToExtension(), ToHost()
	if toExt.WriteLimit != contract.HostToExtensionBytes || toExt.ReadLimit != contract.ExtensionToHostBytes {
		t.Errorf("host-to-extension write %d / read %d, want %d / %d",
			toExt.WriteLimit, toExt.ReadLimit, contract.HostToExtensionBytes, contract.ExtensionToHostBytes)
	}
	if toHost.WriteLimit != contract.ExtensionToHostBytes || toHost.ReadLimit != contract.HostToExtensionBytes {
		t.Errorf("extension-to-host write %d / read %d, want %d / %d",
			toHost.WriteLimit, toHost.ReadLimit, contract.ExtensionToHostBytes, contract.HostToExtensionBytes)
	}
	if toHost.WriteLimit < contract.EffectiveCrossBrowserBytes {
		t.Errorf("the sender limit %d is below the cross-browser floor %d",
			toHost.WriteLimit, contract.EffectiveCrossBrowserBytes)
	}
	if toExt.MaxConcurrentRequests != contract.MaxConcurrentRequests || toExt.MaxInFlightBytes != contract.MaxInFlightBytes {
		t.Errorf("backpressure budget %d/%d, want %d/%d", toExt.MaxConcurrentRequests, toExt.MaxInFlightBytes,
			contract.MaxConcurrentRequests, contract.MaxInFlightBytes)
	}
	// Neither endpoint arms the deadline: `framing.json#/liveness/heartbeatDirection` is host-to-extension, so on
	// the declared hop this code sends the beats and the extension is the one that counts misses. A watchdog on
	// inbound silence would tear the transport down under a browser that is merely idle.
	if toExt.HeartbeatInterval != 0 || toExt.MissedHeartbeats != 0 {
		t.Errorf("the endpoint toward the browser armed a deadline (%v/%d); it sends the beats, it does not count misses",
			toExt.HeartbeatInterval, toExt.MissedHeartbeats)
	}
	if toHost.HeartbeatInterval != 0 || toHost.MissedHeartbeats != 0 {
		t.Errorf("the endpoint toward a host armed a deadline (%v/%d); the contract declares no heartbeat on that hop",
			toHost.HeartbeatInterval, toHost.MissedHeartbeats)
	}
}

// The contract's rule is about the wire: the sender refuses before writing, so the frame the browser would drop
// is never offered, and the caller keeps an unsent request.
func TestAnOversizedRequestIsRefusedBeforeThePipe(t *testing.T) {
	mine, theirs := newPipe(t)
	drain(t, theirs)

	blob := strings.Repeat("a", contract.ExtensionToHostBytes+1)
	p := New(mine, ToHost())
	_, err := p.Request(context.Background(), "req-oversize", "profile.import",
		json.RawMessage(`{"contentHash":"`+strings.Repeat("0", 64)+`","blob":"`+blob+`"}`))
	var tooLarge *frames.TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("want *frames.TooLargeError, got %v", err)
	}
	if tooLarge.Limit != contract.ExtensionToHostBytes {
		t.Errorf("refused at limit %d, want the extension-to-host number %d",
			tooLarge.Limit, contract.ExtensionToHostBytes)
	}
	if reqs, bytes := p.InFlight(); reqs != 0 || bytes != 0 {
		t.Errorf("a refused request still holds %d requests / %d B of the budget", reqs, bytes)
	}
}

func TestTheConcurrencyCapStopsIssuing(t *testing.T) {
	mine, theirs := newPipe(t)
	drain(t, theirs)

	cfg := ToHost()
	cfg.MaxConcurrentRequests = 3
	p := New(mine, cfg)
	t.Cleanup(func() { _ = p.Close() })
	ids := []string{"req-hold-a", "req-hold-b", "req-hold-c"}
	for _, id := range ids {
		id := id
		go func() { _, _ = p.Request(context.Background(), id, "connector.version", nil) }()
	}
	waitFor(t, "three reservations", func() bool { r, _ := p.InFlight(); return r == len(ids) })

	if _, err := p.Request(context.Background(), "req-fourth", "connector.version", nil); !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("the fourth concurrent request got %v, want ErrTooManyRequests", err)
	}
}

// The concurrency cap alone does not bound memory when frames are large, which is why the contract declares a
// byte budget beside it. This test holds the cap out of the way to prove the budget binds on its own.
//
// There is no `drain` here on purpose: the frame the peer issues is read by this goroutine, and because
// `net.Pipe` hands bytes directly from writer to reader, the moment those bytes arrive the reservation the peer
// took is already visible to `InFlight` — the budget assertion stops being a race with the scheduler and becomes
// a happens-before fact.
func TestTheInFlightByteBudgetStopsIssuing(t *testing.T) {
	mine, theirs := newPipe(t)

	blob := strings.Repeat("b", 4096)
	single := len(requestBytes(t, "req-budget-1", "profile.import", json.RawMessage(`{"blob":"`+blob+`"}`)))

	cfg := ToHost()
	cfg.MaxConcurrentRequests = 64
	cfg.MaxInFlightBytes = single // exactly one frame of this size: the budget is spent the moment it is held
	p := New(mine, cfg)
	t.Cleanup(func() { _ = p.Close() })
	done := make(chan struct{})
	go func() {
		_, _ = p.Request(context.Background(), "req-budget-1", "profile.import", json.RawMessage(`{"blob":"`+blob+`"}`))
		close(done)
	}()
	consume(t, theirs, frames.PrefixBytes+single)

	if reqs, bytes := p.InFlight(); reqs != 1 || bytes != single {
		t.Fatalf("the frame reached the far end but the budget reports %d requests / %d B, want 1 / %d", reqs, bytes, single)
	}

	if _, err := p.Request(context.Background(), "req-budget-2", "profile.import", json.RawMessage(`{"blob":"`+blob+`"}`)); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("a request that would overrun the byte budget got %v, want ErrBudgetExhausted", err)
	}
	if _, err := p.Request(context.Background(), "req-budget-3", "connector.version", nil); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatal("even a one-byte frame was issued over an exhausted budget")
	}
	_ = p.Close()
	<-done
}

// A late answer is the outage this rule prevents: the caller was already told Timeout, so applying what the
// answer carries would be a configuration the user did not ask for.
func TestALateAnswerIsDiscardedAndItsEffectNotReapplied(t *testing.T) {
	mine, theirs := newPipe(t)
	drain(t, theirs) // the request this peer issues must be consumed by the far end, or the write never returns

	served := false
	cfg := ToHost()
	cfg.Handler = func(*envelope.Frame) { served = true }
	p := New(mine, cfg)
	serveErr := make(chan error, 1)
	go func() { serveErr <- p.Serve() }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		_, err := p.Request(ctx, "req-late-0001", "config.apply", json.RawMessage(`{}`))
		errs <- err
	}()
	if err := <-errs; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the caller saw %v, want the local deadline the extension owns", err)
	}

	if err := frames.Write(theirs, answerBytes(t, "req-late-0001"), contract.HostToExtensionBytes); err != nil {
		t.Fatalf("writing the late answer: %v", err)
	}
	waitFor(t, "the late answer to be discarded", func() bool { return p.Discarded() == 1 })
	if served {
		t.Error("a discarded late answer reached the handler, so its effect could be re-applied")
	}
	if r, b := p.InFlight(); r != 0 || b != 0 {
		t.Errorf("the timed-out request still holds %d requests / %d B", r, b)
	}
}

func TestSilenceIsCountedInTheContractsOwnUnits(t *testing.T) {
	mine, theirs := newPipe(t)

	// The fake clock starts at the wall clock so the watchdog, which ticks on real time, can never mistake the
	// advanced deadline for silence; the arithmetic below is still measured in the contract's own 25 s units.
	// The interval is armed by hand because this is the only peer that ever counts misses — a receiving one — and
	// it arms it with the contract's two numbers rather than the constructor's, which declare neither.
	now := &clock{t: time.Now()}
	interval := time.Duration(contract.HeartbeatSeconds) * time.Second
	cfg := ToExtension()
	cfg.HeartbeatInterval = interval
	cfg.MissedHeartbeats = contract.MissedHeartbeatsBeforeDisconnect
	cfg.Now = now.Now
	p := New(mine, cfg)

	now.After(2*interval - time.Second)
	if got := p.Missed(); got != 1 {
		t.Errorf("Missed() = %d after 49 s of silence, want 1", got)
	}
	now.After(interval + time.Second)
	if got := p.Missed(); got != contract.MissedHeartbeatsBeforeDisconnect {
		t.Errorf("Missed() = %d at the 75 s mark, want %d", got, contract.MissedHeartbeatsBeforeDisconnect)
	}
	if p.Lost() {
		t.Error("the deadline arithmetic is not the verdict; only the watchdog declares a peer lost")
	}

	// Any inbound frame is the peer speaking, and resets the count. Serve is started first: the pipe is
	// synchronous, so a write with nothing reading the other end blocks until the frame is taken.
	serveErr := make(chan error, 1)
	go func() { serveErr <- p.Serve() }()
	// The extension end writes within its own sender limit; this peer reads with the same number.
	if err := frames.Write(theirs, answerBytes(t, "req-never-issued"), contract.ExtensionToHostBytes); err != nil {
		t.Fatalf("writing a frame: %v", err)
	}
	waitFor(t, "the inbound frame to reset liveness", func() bool { return p.Missed() == 0 })

	// A port closed by the browser is the client going away. It is not an error, and it is not the heartbeat
	// verdict, and it is never a shutdown signal (docs/adr/0005-two-process-connector.md rule 4).
	if err := theirs.Close(); err != nil {
		t.Fatalf("closing the peer end: %v", err)
	}
	if err := <-serveErr; err != nil {
		t.Fatalf("Serve after a clean close = %v, want nil", err)
	}
	if p.Lost() {
		t.Error("a closed port declared the peer lost; the two signals mean different things")
	}
}

func TestThreeMissedHeartbeatsTearTheTransportDown(t *testing.T) {
	mine, theirs := newPipe(t)
	defer theirs.Close()

	tick := 5 * time.Millisecond
	cfg := ToExtension()
	cfg.HeartbeatInterval = 20 * time.Millisecond
	cfg.MissedHeartbeats = contract.MissedHeartbeatsBeforeDisconnect
	cfg.Tick = &tick
	p := New(mine, cfg)

	done := make(chan error, 1)
	go func() { done <- p.Serve() }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrPeerLost) {
			t.Fatalf("Serve = %v, want ErrPeerLost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watchdog never declared the peer lost")
	}
	if !p.Lost() {
		t.Error("Serve returned ErrPeerLost while Lost() still reports the peer present")
	}
	// An unreachable peer is not a Core failure and not a shutdown request; a request after the verdict fails
	// rather than hanging.
	if _, err := p.Request(context.Background(), "req-after-lost", "connector.version", nil); err == nil {
		t.Fatal("a request on a torn-down peer returned no error")
	}
}

func TestAnOversizedInboundFrameIsATransportErrorNotATruncation(t *testing.T) {
	mine, theirs := newPipe(t)

	cfg := ToExtension()
	cfg.ReadLimit = 64 // the receiver's own number for this hop
	p := New(mine, cfg)
	done := make(chan error, 1)
	go func() { done <- p.Serve() }()

	// The sender stays within its direction's limit, so the frame is legal to write and illegal to accept. The
	// write itself may end half-consumed once the reader tears the transport down; what is being asserted here is
	// the reader's verdict, which must not be ErrTruncated -- truncation would mean the stream slipped, and a
	// receiver that skipped the frame would read the rest of the pipe out of step.
	go func() { _ = frames.Write(theirs, []byte(strings.Repeat("c", 65)), contract.ExtensionToHostBytes) }()
	err := <-done
	var tooLarge *frames.TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Serve = %v, want *frames.TooLargeError", err)
	}
	if errors.Is(err, frames.ErrTruncated) {
		t.Fatal("an oversized frame was reported as truncation")
	}
}

// A frame that is not a legal envelope desynchronises a message-delimited pipe, so the stream ends rather than
// the frame being skipped and the handler left to guess what arrived.
func TestAnInvalidInboundFrameEndsTheStream(t *testing.T) {
	mine, theirs := newPipe(t)

	reached := false
	cfg := ToHost()
	cfg.Handler = func(*envelope.Frame) { reached = true }
	p := New(mine, cfg)
	done := make(chan error, 1)
	go func() { done <- p.Serve() }()

	// No sentAt, which packages/schema/protocol/envelope.schema.json#/required demands.
	bad := []byte(`{"v":1,"id":"req-notsentat","type":"request","method":"connector.version"}`)
	if err := frames.Write(theirs, bad, contract.HostToExtensionBytes); err != nil {
		t.Fatalf("writing a malformed frame: %v", err)
	}
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "inbound frame rejected") {
		t.Fatalf("Serve = %v, want a rejection that names the inbound frame", err)
	}
	if reached {
		t.Error("a rejected frame reached the handler")
	}
}
