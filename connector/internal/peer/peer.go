// Package peer is the framing peer: one pipe endpoint, one frame per envelope object, and the four transport
// rules the contract states but a codec cannot own — sender-side rejection before the write, backpressure on
// the outstanding-request budget, a late answer discarded by id, and the three-missed-heartbeat deadline, which
// a receiving peer arms for itself (packages/schema/protocol/framing.json,
// docs/architecture/25-native-messaging-protocol.md §4, §9, §11).
//
// It is symmetric: the same code is the host's endpoint on the browser hop and either endpoint of the local pipe,
// with the direction chosen by Config. What it deliberately does not do is decide anything about the connector's
// or Core's lifetime: a closed port is the end of a stream, never a shutdown signal
// (docs/adr/0005-two-process-connector.md rule 4).
package peer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/frames"
)

// ErrTooManyRequests reports the concurrency cap reached: the sender stops issuing rather than queueing
// (packages/schema/protocol/framing.json#/ordering/backpressure — "neither direction holds an unbounded queue").
var ErrTooManyRequests = errors.New("peer: the concurrent-request cap is reached")

// ErrBudgetExhausted reports that the in-flight byte budget cannot take another frame. The concurrency cap
// alone does not bound memory when frames are large, which is why the contract declares both.
var ErrBudgetExhausted = errors.New("peer: the in-flight byte budget is exhausted")

// ErrPeerLost reports the deadline passed with the peer silent. The transport is torn down and every outstanding
// request is failed: it means the peer is unreachable, not that Core died
// (docs/architecture/25-native-messaging-protocol.md §11).
var ErrPeerLost = errors.New("peer: the peer missed its heartbeat deadline")

// ErrPeerClosed reports that the pipe ended at a frame boundary. Serve returns nil for it, so a caller cannot
// mistake "the browser went away" for an error to act on.
var ErrPeerClosed = errors.New("peer: the peer closed the pipe")

// Config is the peer's transport budget. The zero value is not usable; use one of the two constructors, which
// take their numbers from the generated contract rather than restating them.
type Config struct {
	// WriteLimit refuses a frame too large for its direction before a byte reaches the pipe.
	WriteLimit int
	// ReadLimit treats an oversized inbound frame as a transport error; it never truncates.
	ReadLimit int
	// MaxConcurrentRequests and MaxInFlightBytes are the sender's backpressure budget.
	MaxConcurrentRequests int
	MaxInFlightBytes      int
	// HeartbeatInterval and MissedHeartbeats are the liveness deadline, and neither constructor sets them: the
	// contract puts the heartbeat's direction at host-to-extension, so a Go peer arms this only when it is the
	// one receiving the beats. Zero disables the watchdog.
	HeartbeatInterval time.Duration
	MissedHeartbeats  int
	// Tick shortens only how often the watchdog looks, never the deadline it measures. Tests set it; production
	// leaves it nil and watches once per interval.
	Tick *time.Duration
	// Now is the clock the liveness deadline reads. Tests pass a fake one.
	Now func() time.Time
	// Handler receives every inbound frame that is not the answer to one of our requests: requests we serve,
	// events, and progress.
	Handler func(*envelope.Frame)
	// PipeFloor is the pipe version this endpoint parses inbound frames against. Zero means the contract's
	// MinPipeVersion. It is a constructor override, not a runtime knob: a test needs it because the shipped
	// floor of 1 is subsumed by the envelope's 1..255 bound, so the below-floor answer branch (VER-89) cannot
	// be produced by any frame the schema accepts until a real floor above 1 ships.
	PipeFloor int
}

// ToExtension is the host's endpoint facing the browser: it may send the host-to-extension ceiling and must
// refuse what arrives above the extension-to-host one.
//
// It arms no liveness deadline, and the reason is the direction the contract declares:
// `packages/schema/protocol/framing.json#/liveness/heartbeatDirection` is `host-to-extension`, so this endpoint is
// the one that would send the beats and the extension is the one that counts misses. A deadline on inbound
// silence here would tear the transport down under an idle browser, whose silence is not a symptom of anything —
// the port closing is the contract's own "the client is gone" signal (§11 of
// `docs/architecture/25-native-messaging-protocol.md`, rule 4 of `docs/adr/0005-two-process-connector.md`). A peer
// that does receive beats arms the watchdog by setting the two fields itself.
func ToExtension() Config {
	return Config{
		WriteLimit:            contract.HostToExtensionBytes,
		ReadLimit:             contract.ExtensionToHostBytes,
		MaxConcurrentRequests: contract.MaxConcurrentRequests,
		MaxInFlightBytes:      contract.MaxInFlightBytes,
	}
}

// ToHost is the sender facing a host: the extension-to-host number is enforced here, by the sender, before the
// write, because Chrome drops the frame otherwise and a receiver-side check means it was already lost.
// It arms no deadline either — the contract declares a heartbeat on the browser hop only, and the local pipe's
// liveness is ADR-0005 rule 2's business, not a fifth transport number.
func ToHost() Config {
	return Config{
		WriteLimit:            contract.ExtensionToHostBytes,
		ReadLimit:             contract.HostToExtensionBytes,
		MaxConcurrentRequests: contract.MaxConcurrentRequests,
		MaxInFlightBytes:      contract.MaxInFlightBytes,
	}
}

// waiters are what an unanswered request costs, so the budget cannot be reserved and then leak.
type waiters struct {
	concurrent int
	bytes      int
}

type pending struct {
	ch   chan *envelope.Frame
	wait *waiters
}

// Peer is one pipe endpoint.
type Peer struct {
	pipe io.ReadWriteCloser
	cfg  Config

	writeMu sync.Mutex // one frame is written in one call, so two senders cannot splice frames

	mu        sync.Mutex
	wait      waiters
	pending   map[string]*pending
	discarded int64
	heartbeat *time.Time

	lost     chan struct{}
	lostOnce sync.Once
	done     chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// New takes ownership of the pipe: Serve closes it when the stream ends, and Close is safe to call twice and
// from another goroutine while Serve is blocked in Read.
func New(pipe io.ReadWriteCloser, cfg Config) *Peer {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PipeFloor == 0 {
		cfg.PipeFloor = contract.MinPipeVersion
	}
	// A freshly opened pipe has just been heard from; a peer that never speaks still has to miss three beats.
	now := cfg.Now()
	return &Peer{
		pipe: pipe, cfg: cfg, pending: map[string]*pending{},
		heartbeat: &now, lost: make(chan struct{}), done: make(chan struct{}),
	}
}

// Serve reads until the pipe ends, the peer misses its deadline, or a frame cannot be trusted. It returns nil
// for a clean close.
//
// A frame that fails validation ends the stream rather than being skipped: the framing is message-delimited, so
// the peer has either desynchronised the pipe or sent something outside the contract, and neither is fixed by
// reading on (docs/architecture/25-native-messaging-protocol.md §8 rule 1).
func (p *Peer) Serve() error {
	stop := p.watchLiveness()
	defer stop()
	defer p.Close()
	for {
		data, err := frames.Read(p.pipe, p.cfg.ReadLimit)
		if err != nil {
			if p.Lost() {
				return ErrPeerLost
			}
			if errors.Is(err, frames.ErrClosed) {
				return nil
			}
			return err
		}
		f, err := envelope.ParseWithFloor(data, p.cfg.PipeFloor)
		if err != nil {
			// A frame below the pipe floor is the one rejected frame the contract asks to answer rather than
			// only to end on: framing.json#/versionFloors/tooOld names VersionMismatch, in reply to the
			// frame's own id (VER-89). The stream still ends — a sender below the floor speaks a framing
			// this build cannot continue reading in step — but it is told why, by the code the contract
			// names, before the port closes.
			var inv *envelope.Invalid
			if errors.As(err, &inv) && inv.BelowPipeFloor > 0 {
				if answer := VersionMismatchAnswer(inv, p.cfg.PipeFloor); answer != nil {
					_ = p.Send(answer)
				}
			}
			return fmt.Errorf("peer: inbound frame rejected: %w", err)
		}
		p.deliver(f)
	}
}

// versionMismatchAnswer builds the VersionMismatch answer a below-floor frame is owed. The reply carries this
// build's own framing version — answering in the sender's v would be speaking the framing that just failed — and
// is only sent when the rejected frame carried a parseable id: an id-less frame has nothing to reply to, and the
// stream end is the whole answer it gets.
// VersionMismatchAnswer is the exported form: serveLines answers the same frame the framed mode does,
// so both transports tell a below-floor sender the same thing by the code the contract names.
func VersionMismatchAnswer(inv *envelope.Invalid, floor int) *envelope.Frame {
	if inv == nil || inv.RejectedID == "" {
		return nil
	}
	head := &envelope.Frame{
		V:       contract.FrameFormatVersion,
		ID:      inv.RejectedID,
		TraceID: inv.RejectedTrace,
	}
	res, err := head.Fail("VersionMismatch", fmt.Sprintf(
		"frame v %d is below the pipe floor %d this build accepts (framing.json#/versionFloors/pipe); the port closes after this answer",
		inv.BelowPipeFloor, floor))
	if err != nil {
		// VersionMismatch is in the taxonomy, so this is unreachable; a refusal to answer a version
		// failure with a broken frame is still the safe arm.
		return nil
	}
	return res
}

// deliver resolves a request we issued, or hands the frame to the handler. A response whose id is no longer in
// flight is counted and dropped: the deadline already told its caller, and re-applying a late replace-class
// effect would be a configuration the user did not ask for (§9).
func (p *Peer) deliver(f *envelope.Frame) {
	now := p.cfg.Now()
	p.mu.Lock()
	p.heartbeat = &now
	if f.Type == envelope.TypeResponse && f.InReplyTo != nil {
		w, waiting := p.pending[*f.InReplyTo]
		if !waiting {
			p.discarded++
			p.mu.Unlock()
			return
		}
		delete(p.pending, *f.InReplyTo)
		p.wait.concurrent -= w.wait.concurrent
		p.wait.bytes -= w.wait.bytes
		p.mu.Unlock()
		w.ch <- f
		return
	}
	handler := p.cfg.Handler
	p.mu.Unlock()
	if handler != nil {
		handler(f)
	}
}

// Send emits a frame we did not request — a response we are serving, an event, progress. The in-flight budget
// covers requests awaiting answers, so this path enforces only the write limit.
func (p *Peer) Send(f *envelope.Frame) error {
	data, err := f.Marshal()
	if err != nil {
		return err
	}
	return p.write(data)
}

// Request issues a method and waits for its answer. The id is caller-chosen because it is the idempotency key:
// a retry of the same intent reuses it (ADR-0008). ctx is the caller-owned deadline — the extension owns
// timeouts, and this call synthesises the local timeout the contract describes by returning ctx.Err().
func (p *Peer) Request(ctx context.Context, id, method string, args any) (*envelope.Frame, error) {
	f, err := envelope.NewRequest(id, method, args)
	if err != nil {
		return nil, err
	}
	data, err := f.Marshal()
	if err != nil {
		return nil, err
	}
	// The sender refuses before the pipe, so an oversize request costs the caller nothing on the wire.
	if len(data) > p.cfg.WriteLimit {
		return nil, &frames.TooLargeError{Bytes: len(data), Limit: p.cfg.WriteLimit}
	}
	ch, err := p.reserve(id, len(data))
	if err != nil {
		return nil, err
	}
	if err := p.write(data); err != nil {
		p.release(id, ch.wait)
		return nil, err
	}
	select {
	case reply := <-ch.ch:
		return reply, nil
	case <-ctx.Done():
		p.release(id, ch.wait)
		return nil, ctx.Err()
	case <-p.lost:
		p.release(id, ch.wait)
		return nil, ErrPeerLost
	case <-p.done:
		p.release(id, ch.wait)
		return nil, ErrPeerClosed
	}
}

func (p *Peer) reserve(id string, bytes int) (*pending, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, dup := p.pending[id]; dup {
		return nil, fmt.Errorf("peer: request id %q is already in flight", id)
	}
	if p.wait.concurrent >= p.cfg.MaxConcurrentRequests {
		return nil, ErrTooManyRequests
	}
	if p.wait.bytes+bytes > p.cfg.MaxInFlightBytes {
		return nil, ErrBudgetExhausted
	}
	w := &waiters{concurrent: 1, bytes: bytes}
	ch := &pending{ch: make(chan *envelope.Frame, 1), wait: w}
	p.pending[id] = ch
	p.wait.concurrent++
	p.wait.bytes += bytes
	return ch, nil
}

// release forgets a request and gives its budget back. It is not an error to release an id that already
// resolved, so a timeout and a late answer cannot double-free.
func (p *Peer) release(id string, w *waiters) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pending[id]; !ok {
		return
	}
	delete(p.pending, id)
	p.wait.concurrent -= w.concurrent
	p.wait.bytes -= w.bytes
}

func (p *Peer) write(data []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return frames.Write(p.pipe, data, p.cfg.WriteLimit)
}

// Missed reports how many heartbeat intervals have passed with the peer silent, for a peer that was armed with
// an interval at all. An inbound frame of any kind resets it, because the contract declares no heartbeat frame
// type to reset it instead: `framing.json#/liveness` names the interval, the miss count and a direction, and no
// frame carries any of them, so what a heartbeat would occupy on the wire is a decision the data has not made yet
// (docs/PROJECT-STATE.md §9).
func (p *Peer) Missed() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.missedLocked(p.cfg.Now())
}

// missedLocked counts intervals of silence against a clock the caller already read, because Now may be a test
// clock that takes p.mu.
func (p *Peer) missedLocked(now time.Time) int {
	if p.cfg.HeartbeatInterval <= 0 || p.heartbeat == nil {
		return 0
	}
	elapsed := now.Sub(*p.heartbeat)
	if elapsed < p.cfg.HeartbeatInterval {
		return 0
	}
	// Beats are due every interval, so silence of 75 s means the ones scheduled at 25, 50 and 75 are all missed --
	// which is the number the contract names for declaring the peer gone.
	return int(elapsed / p.cfg.HeartbeatInterval)
}

// Lost reports the deadline passed: the peer is unreachable, which says nothing about Core.
func (p *Peer) Lost() bool {
	select {
	case <-p.lost:
		return true
	default:
		return false
	}
}

// Discarded counts answers dropped because their request had already timed out.
func (p *Peer) Discarded() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.discarded
}

// InFlight reports the outstanding requests and the bytes they reserve, which is what the two budget caps
// measure.
func (p *Peer) InFlight() (requests, bytes int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.wait.concurrent, p.wait.bytes
}

// Close ends the stream. Outstanding requests learn about it; the peer is not declared lost, because hanging
// up is not missing a heartbeat.
func (p *Peer) Close() error {
	p.closeOnce.Do(func() {
		close(p.done)
		p.closeErr = p.pipe.Close()
	})
	return p.closeErr
}

// watchLiveness starts the deadline check and returns its stop. A ticker, not a per-frame timer: the rule is
// "three missed heartbeats", so one clock must be watching continuously.
func (p *Peer) watchLiveness() func() {
	if p.cfg.HeartbeatInterval <= 0 {
		return func() {}
	}
	interval := p.cfg.HeartbeatInterval
	if p.cfg.Tick != nil {
		interval = *p.cfg.Tick
	}
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				p.mu.Lock()
				missed := p.missedLocked(now)
				p.mu.Unlock()
				if missed < p.cfg.MissedHeartbeats {
					continue
				}
				p.lostOnce.Do(func() { close(p.lost); _ = p.pipe.Close() })
				return
			}
		}
	}()
	return func() { close(stop) }
}
