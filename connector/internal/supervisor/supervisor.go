// Package supervisor owns the Xray child process and nothing else: the five verbs `docs/architecture/22-component-contracts.md`
// §4 gives it — spawn with the compiled config, contain it, watch the process itself, restart it on a bounded
// ladder, stop it in order.
//
// What it does not know is that section's own non-goals: what a profile is, what a route means, whether a server
// is healthy. It receives a path, an opaque hash and the compiler's inbound table, and it answers with facts
// about one process.
//
// Three seams carry the facts this package refuses to invent:
//
//   - Launch builds argv. The corpus says the config path travels on the command line
//     (`packages/schema/protocol/data-ownership.json`'s `core-config-file` row) and never spells Xray's flag, so
//     the wiring owns the command line and a supervisor never guesses one.
//   - Verify is `docs/architecture/33-binary-integrity.md` §6's pre-exec hash. ADR-0019 rule 4 forbids any path
//     that runs "whatever binary is on disk", so a supervisor built without a verifier refuses to spawn.
//   - Dir arrives from the caller, for the same reason `connector/internal/apply`'s write directory does: the
//     corpus names four properties of the applied-state file and never a path for it.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// The four kinds this package publishes are four of the names in
// `packages/schema/events.schema.json#/properties/name/enum`, the catalogue doc 36 §6 keeps in parity with the
// schema. `config.drift` belongs to the reconciliation doc 30 §4 describes and this process does not own.
const (
	KindStarted = "core.started"
	KindExited  = "core.exited"
	KindRestart = "core.restart"
	KindBlocked = "core.blocked"
)

// ErrAlreadyRunning is core.start's `CoreAlreadyRunning`: replacing a running Core is core.restart's job. The
// same hash is a successful no-op instead, per doc 22 §4 invariant 2 and core.start's own summary.
var ErrAlreadyRunning = errors.New("supervisor: Core is already running with a different config")

// ErrNotRunning is core.status's `CoreNotRunning` and core.restart's `CoreExited`, asked of a Core this process
// is not running and holds no ladder for.
var ErrNotRunning = errors.New("supervisor: Core is not running")

// Death is `packages/schema/failure-policy.schema.json#/properties/coreDeath` in durations rather than in the
// schema's integer seconds, because the ladder waits instead of counting.
//
// `action` has no field here on purpose: the schema makes it a single-value enum of `block`, and blocking is a
// `chrome.proxy` write that doc 30 §7 keeps out of this process — `core.blocked` is a verdict, not an act.
type Death struct {
	Attempts int
	Backoff  []time.Duration

	// Notify gates the ladder's noise and not the verdict, which is that field's own description in the schema
	// and doc 30 §4's point that a terminal block is surfaced whatever it says.
	Notify bool
}

// Inbound is one row of the `inboundPorts` table `config.apply` and `core.start` both report. It is the
// compiler's claim passed through: nothing in this package observes a bound socket, which is stage 7's probe and
// still needs a Core binary.
type Inbound struct {
	Listen string `json:"listen"`
	Port   int    `json:"port"`
	Tag    string `json:"tag"`
}

// StartParams is the desired state as far as a process owner can see it.
type StartParams struct {
	// ConfigPath is the file `connector/internal/apply` installed. This package never writes it.
	ConfigPath string
	// ConfigHash is opaque by design. ODEC-36 still decides which byte string configHash covers, and a
	// supervisor choosing one would be deciding it in the wrong place.
	ConfigHash string
	// ProfileID is carried into the state record and never read: doc 22 §4 makes "does not know what a profile
	// is" a non-goal, and data-ownership's applied-state row still names the selection as one of its four fields.
	ProfileID string
	Inbounds  []Inbound
}

// Exit is what one observed termination knows. `docs/architecture/30-failure-policy.md` §3's rule that
// "exitCode is reported, interpreted nowhere" is why there is no classification method on this struct: of the
// four classes doc 22 §4 invariant 3 names, only the code and the signal have data behind them, and no file in
// this corpus states Xray's exit-code semantics.
//
// There is no message field. The text a dying Core could add is its stderr, and the config it was handed is
// plaintext credential material (`docs/architecture/34-data-split.md` §7), so a tail of it in a state file would
// be a secret outside the keystore and outside doc 32's sink masking. Nothing in this package ever sets
// Cmd.Stderr, so there is no stream to leak from; a later command surface that wants Core's log must route it
// through doc 32's masker, not through this struct.
type Exit struct {
	At        time.Time `json:"at"`
	Code      int       `json:"exitCode"`
	Signalled bool      `json:"-"`
	Signal    string    `json:"signal,omitempty"`
}

// Status is core.status's process-level facts, plus the two the ladder needs a reader to have: Restarting and
// Blocked. Blocked is not a field of that command's result; it is the verdict the event surface carries.
type Status struct {
	Running   bool
	PID       int
	Hash      string
	StartedAt time.Time
	Contained bool
	// ContainmentNote is why Contained is false, in the platform seam's own words. ADR-0005 rule 5 requires the
	// containment result to be reported rather than assumed, and its Phase-2 list requires a false to surface.
	ContainmentNote string
	Inbounds        []Inbound
	Restarting      bool
	AttemptsMade    int
	Blocked         bool
	LastExit        *Exit
}

// StopResult is core.stop's result. `terminatedBy` admits four values and this process can honestly produce two:
// `parent-death` is unreachable from inside the thing that died (containment is what makes that true), and
// `crash` would be a stop request claiming a death it did not cause, which is what the exit record and
// core.exited already say.
type StopResult struct {
	WasRunning   bool
	ExitCode     int
	TerminatedBy string
	DurationMS   int64
}

// TerminatedBy's values are `packages/schema/protocol/commands/core.stop.json#/result/properties/terminatedBy`'s.
const (
	TerminatedByRequest        = "request"
	TerminatedByAlreadyStopped = "already-stopped"
)

// RestartResult is core.restart's result: the new process and the two numbers that compare with it.
type RestartResult struct {
	Status
	PreviousPID int
	DowntimeMS  int64
}

// Launch builds the child for one config file. See the package doc for why argv is not this package's fact.
type Launch func(configPath string) *exec.Cmd

// Verify is doc 33 §6's table row "every core.start / core.restart, before exec: sha256 of the file being
// spawned, compared to the install receipt". It runs before every spawn, a ladder retry included, because that
// row names the calls rather than the first one.
type Verify func(binaryPath string) error

// Config is the wiring. New refuses its zero value rather than defaulting it: no state directory, no command
// line and no verifier are three inventions waiting to happen.
type Config struct {
	Dir    string
	Launch Launch
	Verify Verify
	Death  Death

	// Events receives the process's and the ladder's news. nil means nobody publishes, which is a supervisor
	// that still keeps its records.
	//
	// It may be called while this Supervisor's lock is held, because a publication that races the record it
	// reports would be a core.exited whose own Status does not yet know about the death. So a sink must neither
	// call back into this Supervisor nor block: doc 26's progress-frame writer belongs behind a channel of its
	// own, because internal/peer calls its handler from the read loop and a stalled handler stalls the pipe.
	Events func(Event)

	// Now is the clock the records read. Tests pass a fake one; production leaves it nil.
	Now func() time.Time
}

// Event is one publication, sized to what the four kinds above need. The command surface that does not exist yet
// turns these into progress frames; this package does not know about a wire.
type Event struct {
	Kind    string
	PID     int
	Hash    string
	Attempt int
	Exit    Exit
}

// Supervisor is safe for concurrent use: the front door answers frames from one process while the ladder and the
// exit watcher run in goroutines of their own.
type Supervisor struct {
	cfg Config

	mu       sync.Mutex
	want     *StartParams
	live     *child
	run      *ladder
	st       State
	attempts int
	blocked  bool
}

// child is one spawned process and the containment scope it was created into. The scope is per child so that
// closing it — which process exit does for free — is what ends the Core, per doc 21 §11's
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and ADR-0005 rule 5.
type child struct {
	cmd       *exec.Cmd
	pid       int
	hash      string
	startedAt time.Time
	scope     *scope
	contained bool
	note      string
	done      chan struct{}
}

// ladder is one in-flight restart sequence: cancel stops it and running closes when it has.
type ladder struct {
	cancel  context.CancelFunc
	running chan struct{}
}

// New builds the process owner over its seams and reads whatever the last run of this directory left, because
// data-ownership's `applied-state` row makes this package the writer and doc 34 §7 keeps the file for exactly
// one reason: it must survive a browser restart, which is LIVE-13.
func New(cfg Config) (*Supervisor, error) {
	if cfg.Dir == "" {
		return nil, errors.New("supervisor: no state directory, and the corpus names no path for the applied-state file; %LOCALAPPDATA%\\NEXORA\\config is a fact this package is not allowed to invent")
	}
	if cfg.Launch == nil {
		return nil, errors.New("supervisor: no Launch, and Xray's command line is stated nowhere in packages/schema or docs/")
	}
	if cfg.Verify == nil {
		return nil, errors.New("supervisor: no Verify, and ADR-0019 rule 4 forbids running a binary nobody proved")
	}
	if cfg.Death.Attempts < 0 || cfg.Death.Attempts > 10 {
		return nil, fmt.Errorf("supervisor: restartAttempts is %d, outside failure-policy.schema.json's 0 to 10", cfg.Death.Attempts)
	}
	if len(cfg.Death.Backoff) > 10 {
		return nil, fmt.Errorf("supervisor: %d backoff steps, outside restartBackoffSeconds' 1 to 10", len(cfg.Death.Backoff))
	}
	if cfg.Death.Attempts > 0 && len(cfg.Death.Backoff) == 0 {
		return nil, errors.New("supervisor: attempts without a wait between them; restartBackoffSeconds is minItems 1 for that reason")
	}
	for _, w := range cfg.Death.Backoff {
		if w < time.Second || w > time.Hour {
			return nil, fmt.Errorf("supervisor: a backoff step of %s is outside restartBackoffSeconds' 1 to 3600 seconds", w)
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	st, err := ReadState(cfg.Dir)
	if err != nil {
		// The row's own purge column says "a corrupt file means recompile, not data loss": applied-state is
		// observation, so a file this process cannot read is one it may overwrite. It is not a reason to refuse.
		st = State{}
	}
	return &Supervisor{cfg: cfg, st: st}, nil
}

// Status reports the process, not the servers. A Core that exited a moment ago and a Core nobody ever started
// differ in LastExit rather than in Running, which is why doc 30 §3 refuses to read "no CoreExited" as life.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Supervisor) statusLocked() Status {
	out := Status{Hash: s.st.AppliedHash, Blocked: s.blocked, AttemptsMade: s.attempts, LastExit: s.st.LastExit}
	if c := s.live; c != nil {
		out.Running = true
		out.PID = c.pid
		out.Hash = c.hash
		out.StartedAt = c.startedAt
		out.Contained = c.contained
		out.ContainmentNote = c.note
		if s.want != nil {
			out.Inbounds = s.want.Inbounds
		}
	}
	if s.run != nil {
		out.Restarting = true
	}
	return out
}

// Start converges onto one config. Same hash while running or restarting is a successful no-op; a different
// hash while a process is live is ErrAlreadyRunning; a different hash while a ladder is in flight retargets it,
// because the caller has applied something new and doc 30 §4 forbids this package from rewriting a config behind
// the ladder's back.
func (s *Supervisor) Start(ctx context.Context, p StartParams) (*Status, error) {
	if p.ConfigPath == "" {
		return nil, errors.New("supervisor: a start names no config path")
	}
	if p.ConfigHash == "" {
		return nil, errors.New("supervisor: a start names no config hash, and the no-op test of doc 22 §4 invariant 2 has nothing to compare")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.live != nil {
		if s.live.hash == p.ConfigHash {
			st := s.statusLocked()
			return &st, nil
		}
		return nil, ErrAlreadyRunning
	}
	if s.run != nil {
		if s.want != nil && s.want.ConfigHash == p.ConfigHash {
			st := s.statusLocked()
			return &st, nil
		}
		// Retarget: stop the ladder and let this caller own the desired state. Waiting for it here would block a
		// command surface holding a 15 s deadline, so the in-flight attempt is cancelled and its goroutine finds
		// `want` already replaced.
		s.run.cancel()
		s.run = nil
	}
	s.want = &p
	s.blocked = false
	s.attempts = 0
	st, err := s.spawnLocked(ctx, p, 0)
	if err != nil {
		// Nothing is running and nothing is desired: a failed start leaves no intent behind, because a ladder
		// for a config that never spawned would be doc 30 §4's recovery of a death that never happened.
		s.want = nil
		return nil, err
	}
	return st, nil
}

// spawnLocked is one exec: verify, build, contain, start, record, publish. It is called with s.mu held.
func (s *Supervisor) spawnLocked(ctx context.Context, p StartParams, attempt int) (*Status, error) {
	cmd := s.cfg.Launch(p.ConfigPath)
	if cmd == nil {
		return nil, errors.New("supervisor: Launch returned no command")
	}
	if cmd.Path == "" {
		return nil, errors.New("supervisor: the command line names no binary to verify or run")
	}
	if err := s.cfg.Verify(cmd.Path); err != nil {
		return nil, fmt.Errorf("supervisor: refusing to spawn %s: %w", cmd.Path, err)
	}

	// Containment is attempted and its outcome reported, never assumed and never a reason not to start:
	// core.start's own gloss of `contained` is "false means the Core could outlive the Connector, which is a
	// reported condition, not a silent one", and doc 30 §3 turns that report into a verdict with an integrity
	// alarm rather than into a Core that never ran.
	sc, scopeErr := newScope()
	if scopeErr == nil {
		sc.prepare(cmd)
	}
	if err := cmd.Start(); err != nil {
		if sc != nil {
			_ = sc.close()
		}
		return nil, fmt.Errorf("supervisor: spawn failed: %w", err)
	}
	c := &child{cmd: cmd, pid: cmd.Process.Pid, hash: p.ConfigHash, startedAt: s.cfg.Now(), scope: sc, done: make(chan struct{})}
	if sc != nil {
		c.contained, c.note = sc.attach(c.pid)
	} else {
		c.note = "no containment scope could be made before the spawn: " + scopeErr.Error()
	}

	s.st = State{AppliedHash: p.ConfigHash, SelectedProfile: p.ProfileID, LastStartPID: c.pid, LastExit: s.st.LastExit}
	if err := WriteState(s.cfg.Dir, s.st); err != nil {
		// A Core nobody recorded cannot be reconciled after a Connector restart, and every hash comparison
		// doc 26 §2.1's compare-and-swap rests on is a comparison against this record. So the start fails and
		// takes the process with it rather than leaving traffic routed by an invisible child. The watcher has
		// not started, so this is the only reap; a killed process gives Wait immediately.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if sc != nil {
			_ = sc.close()
		}
		return nil, fmt.Errorf("supervisor: the applied-state write failed, so the Core spawned as %d was stopped again: %w", c.pid, err)
	}
	s.live = c
	s.publish(Event{Kind: KindStarted, PID: c.pid, Hash: c.hash, Attempt: attempt})
	go s.watch(c)
	st := s.statusLocked()
	return &st, nil
}

// watch is doc 22 §4's "health of the process itself": the one goroutine allowed to say Core is gone.
func (s *Supervisor) watch(c *child) {
	defer close(c.done)
	err := c.cmd.Wait()
	if !s.recordExit(c, err) {
		return // Stop closed this child out first, and Stop owns the ladder decision for it.
	}
	s.mu.Lock()
	ladderFor := s.want != nil && s.live == nil
	var hash string
	if ladderFor {
		hash = s.want.ConfigHash
	}
	s.mu.Unlock()
	if !ladderFor {
		return // stopped on request, or a newer child already owns the desired state.
	}
	go s.ladderRun(hash)
}

// recordExit turns one Wait into the record and the event. It is separate from watch because Stop also waits on
// the same child, and the two must not both claim one death.
func (s *Supervisor) recordExit(c *child, waitErr error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live != c {
		return false
	}
	s.live = nil
	ex := s.exitOf(c, waitErr)
	s.st.LastExit = &ex
	_ = WriteState(s.cfg.Dir, s.st)
	s.publish(Event{Kind: KindExited, Hash: c.hash, Exit: ex})
	_ = c.scope.close()
	return true
}

// exitOf reads the wait status. Go's -1 is the spelling of "terminated by a signal", which is the half of
// doc 22 §4 invariant 3 that has data behind it; the other two classes would need Xray's own numbers, and the
// corpus states none.
func (s *Supervisor) exitOf(c *child, waitErr error) Exit {
	ex := Exit{At: s.cfg.Now()}
	if c.cmd.ProcessState != nil {
		code := c.cmd.ProcessState.ExitCode()
		ex.Code = code
		if code == -1 {
			ex.Signalled = true
			if sig, ok := c.cmd.ProcessState.Sys().(interface{ String() string }); ok {
				ex.Signal = sig.String()
			}
		}
		return ex
	}
	if waitErr != nil {
		// Wait failed before the process reported anything, so there is no status to read and no code to name.
		ex.Code = -1
	}
	return ex
}

// ladderRun is doc 30 §4's bounded ladder: no jitter (that section says so explicitly), the policy's own
// backoff list, and a worst case it tabulates as restartAttempts times core.start's deadline plus the backoff
// sum. It is converge-to-desired-state, so an extra core.start during it is legal rather than a second spawn.
//
// The ordering here is that section's headline decision, not an implementation detail: the block is declared
// before the first retry, because with Core dead the port the browser points at is already closed so writing a
// block changes no packet, while it does change an undeclared failure into a dated visible one that
// connector.health can answer `blocked` for instead of `unknown`.
func (s *Supervisor) ladderRun(hash string) {
	ctx, cancel := context.WithCancel(context.Background())
	wait := make(chan struct{})
	s.mu.Lock()
	if s.want == nil || s.want.ConfigHash != hash || s.run != nil || s.live != nil {
		s.mu.Unlock()
		close(wait)
		cancel()
		return
	}
	s.run = &ladder{cancel: cancel, running: wait}
	s.blocked = true
	s.mu.Unlock()
	s.publish(Event{Kind: KindBlocked, Hash: hash})

	attempt := 0
	recovered := false
	defer func() {
		cancel()
		s.mu.Lock()
		if s.run != nil && s.run.running == wait {
			s.run = nil
		}
		if recovered {
			// The block the ladder declared is lifted by the recovery, and doc 30 §4's split says `notify` gates
			// exactly this noise: "a death that recovers on attempt two must not raise a system notification on a
			// machine whose user saw nothing but a slow page".
			s.blocked = false
		}
		s.mu.Unlock()
		close(wait)
		if recovered && s.cfg.Death.Notify {
			s.publish(Event{Kind: KindRestart, Hash: hash, Attempt: attempt})
		}
	}()

	for attempt = 1; attempt <= s.cfg.Death.Attempts; attempt++ {
		d := s.cfg.Death.Backoff[attempt-1]
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.mu.Lock()
		if s.want == nil || s.want.ConfigHash != hash || s.live != nil {
			s.mu.Unlock()
			return
		}
		s.attempts = attempt
		_, err := s.spawnLocked(context.Background(), *s.want, attempt)
		s.mu.Unlock()
		if err == nil {
			recovered = true
			return
		}
	}
	// Exhausted: the block stays in force, which is what the recorded answer says rather than what a second
	// verdict event would say — doc 30 §4's own scene emits core.blocked once, before the attempts, and its
	// terminal message is "restart attempt 3 of 3 failed; block profile left in force".
}

// Stop is core.stop. Graceful is honoured where the platform has a signal to ask with; nothing in this corpus
// states a polite channel for a Windows child, so there the answer is a kill and terminatedBy still reads
// "request", because the request is what caused it.
func (s *Supervisor) Stop(ctx context.Context, graceful bool) (*StopResult, error) {
	started := s.cfg.Now()

	s.mu.Lock()
	had := s.live
	lad := s.run
	want := s.want
	s.want = nil
	if lad != nil {
		s.run = nil
	}
	pid := 0
	if had != nil {
		pid = had.pid
	}
	s.mu.Unlock()

	if lad != nil {
		lad.cancel()
		<-lad.running
		s.mu.Lock()
		had = s.live
		if had != nil {
			pid = had.pid
		}
		s.mu.Unlock()
	}
	if had == nil {
		return &StopResult{WasRunning: false, TerminatedBy: TerminatedByAlreadyStopped, DurationMS: elapsed(started, s.cfg.Now())}, nil
	}

	asked := false
	if graceful {
		asked = had.askGraceful()
		if asked {
			waitDone(had.done, ctx)
		}
	}
	if !asked || !isDone(had.done) {
		had.kill()
		if !waitDone(had.done, ctx) {
			return nil, fmt.Errorf("supervisor: the child as %d would not report its own exit: %w", pid, ctx.Err())
		}
	}
	s.mu.Lock()
	s.blocked = false
	s.mu.Unlock()
	return &StopResult{
		WasRunning:   true,
		ExitCode:     s.stopExitCode(had),
		TerminatedBy: terminatedBy(want),
		DurationMS:   elapsed(started, s.cfg.Now()),
	}, nil
}

// isDone reports whether a child's watcher has already returned, without waiting.
func isDone(done chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// kill is the one stop this module has on every platform: doc 22 §4 gives the supervisor orderly stop as a
// responsibility and ADR-0005 rule 5 gives it containment as the guarantee underneath, so a child that will not
// answer a signal is killed rather than left routing traffic nobody owns.
func (c *child) kill() {
	_ = c.cmd.Process.Kill()
}

// stopExitCode reports the code the watcher recorded for this child, if the watcher got there first, and takes
// it from the process state otherwise. The two paths must not disagree, which is why both read ProcessState and
// only one writes the record.
func (s *Supervisor) stopExitCode(c *child) int {
	if c.cmd.ProcessState != nil {
		return c.cmd.ProcessState.ExitCode()
	}
	return -1
}

func terminatedBy(want *StartParams) string {
	if want == nil {
		return TerminatedByAlreadyStopped
	}
	return TerminatedByRequest
}

// Restart is core.restart: the same config, a new process. It takes no path and no hash, which is doc 30 §4's
// no-silent-rewrite rule seen from here — a restart that disagreed with the applied config would be a config
// change, and that is `config.drift` for the reconciliation to resolve.
func (s *Supervisor) Restart(ctx context.Context) (*RestartResult, error) {
	s.mu.Lock()
	if s.want == nil {
		s.mu.Unlock()
		return nil, ErrNotRunning
	}
	p := *s.want
	prev := 0
	if s.live != nil {
		prev = s.live.pid
	}
	s.mu.Unlock()

	stopped, err := s.Stop(ctx, true)
	if err != nil {
		return nil, err
	}
	started, err := s.Start(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("supervisor: Core is stopped and the restart did not bring it back: %w", err)
	}
	return &RestartResult{Status: *started, PreviousPID: prev, DowntimeMS: stopped.DurationMS}, nil
}

func waitDone(done chan struct{}, ctx context.Context) bool {
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

func elapsed(from, to time.Time) int64 {
	if d := to.Sub(from); d > 0 {
		return d.Milliseconds()
	}
	return 0
}

func (s *Supervisor) publish(e Event) {
	if s.cfg.Events == nil {
		return
	}
	s.cfg.Events(e)
}
