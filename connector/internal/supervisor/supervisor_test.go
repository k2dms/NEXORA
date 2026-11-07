package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fake Core is this test binary re-execed with one of the modes below, because doc 22 §4 asks a supervisor
// to own a child process and ADR-0019 rule 4 keeps any real binary out of this repository. The modes are the
// only behaviour a Core needs for these tests: run until asked to stop, or exit.
const (
	fakeMode      = "NEXORA_FAKE_CORE_MODE"
	fakeMark      = "NEXORA_FAKE_CORE_MARK"
	fakeRecord    = "NEXORA_FAKE_CORE_RECORD"
	fakeConfig    = "NEXORA_FAKE_CORE_CONFIG"
	fakeDuration  = "NEXORA_FAKE_CORE_DURATION"
	fakeTrigger   = "NEXORA_FAKE_CORE_TRIGGER"
	modeWait      = "wait"
	modeExit      = "exit"
	modeExitAfter = "exit-after"
	modeExitWhen  = "exit-when"
	modeOnce      = "once"
	modeParent    = "parent"
	defaultExit   = 7
)

// TestFakeCore is not a test; it is the child. Reaching it without the mode set is a mistake, and skipping is
// how a helper stays out of the run's report.
func TestFakeCore(t *testing.T) {
	mode := os.Getenv(fakeMode)
	if mode == "" {
		t.Skip("helper process for the supervisor tests")
	}
	if p := os.Getenv(fakeRecord); p != "" {
		// data-ownership's core-config-file row: the config path is what Core was handed. Writing it out is how
		// the parent test proves the child received it rather than that the parent meant to send it.
		if mode != modeParent {
			if err := os.WriteFile(p, []byte(os.Getenv(fakeConfig)), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		} else {
			hold(p)
		}
	}
	switch mode {
	case modeExit:
		os.Exit(defaultExit)
	case modeExitWhen:
		// A child that dies the moment a trigger file appears: the deterministic unexpected death, because an
		// external kill races the supervisor's own observation of liveness and a test that means to measure the
		// ladder's arithmetic must not be measuring a kill's timing instead.
		trigger := os.Getenv(fakeTrigger)
		for {
			if _, err := os.Stat(trigger); err == nil {
				os.Exit(defaultExit)
			}
			time.Sleep(10 * time.Millisecond)
		}
	case modeExitAfter:
		// A child that lives for the duration it was told and then dies on its own: the shape a stability
		// window needs, because "stayed up long enough" is only observable with a child whose lifetime the
		// test set in advance.
		if d, err := time.ParseDuration(os.Getenv(fakeDuration)); err == nil && d > 0 {
			time.Sleep(d)
		}
		os.Exit(defaultExit)
	case modeOnce:
		// The first child that finds the marker missing dies; the second one is the recovered Core, and it stays
		// up so that the recovery is a recovery rather than another death.
		mark := os.Getenv(fakeMark)
		if _, err := os.Stat(mark); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(mark, []byte("first child"), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			os.Exit(defaultExit)
		}
		stayUp()
	default:
		stayUp()
	}
}

// stayUp is a child's "run until asked to stop". A bare `select {}` is the honest expression of forever and Go's
// deadlock detector turns it into a fatal error inside a test binary, so every modeWait child would die on its own
// and a test that meant to stop a live Core would be reporting an exit it caused nothing. A sleep is the same wait
// with a wake-up the runtime owes the goroutine, which keeps the child a process that is merely alive.
func stayUp() {
	for {
		time.Sleep(time.Hour)
	}
}

// hold is modeParent's body: it puts one child into a real scope through the platform seam and then exits
// without waiting for it and without closing the scope by hand. That is the only way to put a parent-death
// promise in front of a kernel from inside a test, because the alternative is to kill the test process. The
// record carries the pid the outer test polls and the containment answer the seam gave.
func hold(recordPath string) {
	cmd := exec.Command(os.Args[0], "-test.run=TestFakeCore", "-test.count=1")
	// The record name is the holder's own and the child has nothing to put in it; left inherited it would be a
	// second writer of the file the outer test is waiting on.
	cmd.Env = append(os.Environ(), fakeMode+"="+modeWait, fakeRecord+"=")
	sc, err := newScope()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	sc.prepare(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	pid := cmd.Process.Pid
	contained, note := sc.attach(pid)
	// The same liveness check the outer test repeats, taken while the holder is still here: without this half of
	// the record, a child that never started would look exactly like a child that was contained.
	record := []byte(fmt.Sprintf("%d\t%v\t%s\t%v", pid, contained, note, processAlive(pid)))
	if err := os.WriteFile(recordPath, record, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func fakeLaunch(mode string) Launch {
	return func(configPath string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestFakeCore", "-test.count=1")
		cmd.Env = append(os.Environ(), fakeMode+"="+mode, fakeConfig+"="+configPath)
		return cmd
	}
}

// alwaysProves is the verifier a test that is not about integrity passes: doc 33 §6's pre-exec hash is asserted
// by TestVerifyRunsBeforeTheSpawnAndADenialLeavesNothingRunning, not by every test in the file.
var alwaysProves Verify = func(string) error { return nil }

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) publish(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Kind
	}
	return out
}

func (r *recorder) count(kind string) int {
	n := 0
	for _, k := range r.kinds() {
		if k == kind {
			n++
		}
	}
	return n
}

func (r *recorder) last(t *testing.T, kind string) Event {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Kind == kind {
			return r.events[i]
		}
	}
	t.Fatalf("no %s event was ever published", kind)
	return Event{}
}

func (r *recorder) wait(t *testing.T, kind string, timeout time.Duration) Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, e := range r.events {
			if e.Kind == kind {
				r.mu.Unlock()
				return e
			}
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s event within %s; saw %v", kind, timeout, r.kinds())
	return Event{}
}

func newTestSupervisor(t *testing.T, mode string, death Death) (*Supervisor, *recorder) {
	t.Helper()
	ev := new(recorder)
	s, err := New(Config{
		Dir:    t.TempDir(),
		Launch: fakeLaunch(mode),
		Verify: alwaysProves,
		Death:  death,
		Events: ev.publish,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, ev
}

// waitForFile gives a child's own write a deadline: the supervisor publishes core.started as soon as the spawn
// returns, and the child is free to record what it was handed a moment later.
func waitForFile(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared within %s: %v", path, timeout, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// New refuses the three places a caller could hand it an invention instead of a fact.
func TestNewRefusesToGuessItsSeams(t *testing.T) {
	dir := t.TempDir()
	launch := func(string) *exec.Cmd { return exec.Command(os.Args[0], "-test.run=TestFakeCore") }
	anyVerify := func(string) error { return nil }

	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"no state directory", Config{Launch: launch, Verify: anyVerify}, "no state directory"},
		{"no command line", Config{Dir: dir, Verify: anyVerify}, "no Launch"},
		{"no pre-exec proof", Config{Dir: dir, Launch: launch}, "no Verify"},
		{"attempts outside the schema", Config{Dir: dir, Launch: launch, Verify: anyVerify, Death: Death{Attempts: 11, Stability: time.Hour}}, "0 to 10"},
		{"attempts with no wait", Config{Dir: dir, Launch: launch, Verify: anyVerify, Death: Death{Attempts: 2, Stability: time.Hour}}, "minItems 1"},
		{"a backoff below the schema's floor", Config{Dir: dir, Launch: launch, Verify: anyVerify, Death: Death{Attempts: 1, Backoff: []time.Duration{time.Millisecond}, Stability: time.Hour}}, "1 to 3600"},
		{"attempts with no stability window", Config{Dir: dir, Launch: launch, Verify: anyVerify, Death: Death{Attempts: 2, Backoff: []time.Duration{time.Second}}}, "stability window"},
	} {
		if _, err := New(tc.cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: want an error naming %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestVerifyRunsBeforeTheSpawnAndADenialLeavesNothingRunning(t *testing.T) {
	s, _ := newTestSupervisor(t, modeWait, Death{})
	refused := errors.New("the receipt does not match")
	s.cfg.Verify = func(string) error { return refused }

	if _, err := s.Start(context.Background(), StartParams{ConfigPath: "config.json", ConfigHash: hash("a")}); err == nil {
		t.Fatal("a Core was spawned although the pre-exec check refused it (ADR-0019 rule 4)")
	} else if !errors.Is(err, refused) {
		t.Fatalf("Start lost the verifier's reason: %v", err)
	}
	if st := s.Status(); st.Running {
		t.Fatal("Status reports a process the verifier refused")
	}
}

func TestStartHandsTheConfigPathOnTheCommandLineAndRecordsTheProcess(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "handed")
	ev := new(recorder)
	s, err := New(Config{
		Dir: dir,
		Launch: func(configPath string) *exec.Cmd {
			cmd := fakeLaunch(modeWait)(configPath)
			cmd.Env = append(cmd.Env, fakeRecord+"="+record)
			return cmd
		},
		Verify: alwaysProves,
		Death:  Death{},
		Events: ev.publish,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _, _ = s.Stop(context.Background(), false) }()

	const cfgPath = "applied/config.json"
	st, err := s.Start(context.Background(), StartParams{ConfigPath: cfgPath, ConfigHash: hash("a"), ProfileID: "prof-1", Inbounds: []Inbound{{Listen: "127.0.0.1", Port: 20011, Tag: "in-srv-aaaa1111"}}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !st.Running || st.PID < 1 {
		t.Fatalf("Start reported %+v", st)
	}
	if len(st.Inbounds) != 1 || st.Inbounds[0].Port != 20011 {
		t.Errorf("the compiler's inbound table did not survive the start: %+v", st.Inbounds)
	}
	ev.wait(t, KindStarted, 5*time.Second)

	if got := string(waitForFile(t, record, 20*time.Second)); got != cfgPath {
		t.Errorf("Core was handed %q, want %q", got, cfgPath)
	}
	// data-ownership's applied-state row names four fields; this asserts all four are reachable again from disk.
	st2, err := ReadState(dir)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if st2.AppliedHash != hash("a") || st2.SelectedProfile != "prof-1" || st2.LastStartPID != st.PID {
		t.Errorf("the applied-state record is %+v", st2)
	}
}

func TestSameHashStartIsANoOpAndADifferentHashRefuses(t *testing.T) {
	s, _ := newTestSupervisor(t, modeWait, Death{})
	first, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _, _ = s.Stop(context.Background(), false) }()

	again, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")})
	if err != nil {
		t.Fatalf("a repeat core.start failed: %v", err)
	}
	if again.PID != first.PID {
		t.Errorf("doc 22 §4 invariant 2: the same hash returned a new pid, %d after %d", again.PID, first.PID)
	}
	if _, err := s.Start(context.Background(), StartParams{ConfigPath: "b.json", ConfigHash: hash("b")}); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("a different hash over a running Core: want ErrAlreadyRunning, got %v", err)
	}
}

// The ladder is the half of doc 30 §4 a test can prove: one unexpected exit, the block declared before any
// retry, one bounded wait, one recovery — and the record of the death kept in between.
func TestUnexpectedExitIsRestartedByTheLadderAndRecoveryIsPublished(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "first-child-exited")
	launch := func(configPath string) *exec.Cmd {
		cmd := fakeLaunch(modeOnce)(configPath)
		cmd.Env = append(cmd.Env, fakeMark+"="+mark)
		return cmd
	}
	ev := new(recorder)
	s, err := New(Config{
		Dir:    dir,
		Launch: launch,
		Verify: alwaysProves,
		Death:  Death{Attempts: 2, Backoff: []time.Duration{time.Second, time.Second}, Stability: time.Hour, Notify: true},
		Events: ev.publish,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _, _ = s.Stop(context.Background(), false) }()

	st, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	first := st.PID

	exited := ev.wait(t, KindExited, 25*time.Second)
	if exited.Exit.Code != defaultExit {
		t.Errorf("the exit record says code %d, want the %d the child exited with", exited.Exit.Code, defaultExit)
	}
	// doc 30 §4's ordering, measured rather than narrated: the verdict is out before the first retry waits.
	blocked := ev.wait(t, KindBlocked, 3*time.Second)
	if blocked.Hash != hash("a") {
		t.Errorf("the block names %q, not the config that died", blocked.Hash)
	}
	recovered := ev.wait(t, KindRestart, 20*time.Second)
	if recovered.Attempt != 1 {
		t.Errorf("the ladder reported attempt %d, want the first one to recover", recovered.Attempt)
	}
	later := s.Status()
	if !later.Running || later.PID == first {
		exit := "none"
		if later.LastExit != nil {
			exit = fmt.Sprintf("code %d, signalled %v %q", later.LastExit.Code, later.LastExit.Signalled, later.LastExit.Signal)
		}
		t.Fatalf("the ladder left the process at %+v after the first child exited (events %v, last exit %s)",
			later, ev.kinds(), exit)
	}
	if later.Blocked {
		t.Error("the block declared for the death was still in force after the recovery lifted it")
	}
	if later.LastExit == nil || later.LastExit.Code != defaultExit {
		t.Errorf("the death of the first child is missing from the record: %+v", later.LastExit)
	}
}

// A ladder that runs out is a verdict, and doc 30 §4 plus the schema's own words for `notify` make the verdict
// visible whatever the flag says. The attempts fail in the pre-exec check here, which is the only way to make a
// retry fail rather than succeed-then-die, and it proves the same row of doc 33 §6 runs inside the ladder.
func TestExhaustedLadderIsBlockedAndSurfacedWithNotifyOff(t *testing.T) {
	var mu sync.Mutex
	spawns := 0
	ev := new(recorder)
	s, err := New(Config{
		Dir:    t.TempDir(),
		Launch: fakeLaunch(modeExit),
		Verify: func(string) error {
			mu.Lock()
			defer mu.Unlock()
			spawns++
			if spawns > 1 {
				return errors.New("the receipt stopped matching")
			}
			return nil
		},
		Death:  Death{Attempts: 2, Backoff: []time.Duration{time.Second, time.Second}, Stability: time.Hour, Notify: false},
		Events: ev.publish,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev.wait(t, KindExited, 10*time.Second)
	ev.wait(t, KindBlocked, 10*time.Second)

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := spawns == 3
		mu.Unlock()
		if done {
			if st := s.Status(); !st.Restarting {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := s.Status(); st.Blocked != true || st.Running || st.Restarting {
		t.Errorf("after exhaustion Status is %+v", st)
	}
	if ev.count(KindRestart) != 0 {
		t.Error("notify=false raised a recovery event for a recovery that did not happen; the verdict is loud and the noise is not (doc 30 §4)")
	}
	mu.Lock()
	n := spawns
	mu.Unlock()
	if n != 3 {
		t.Errorf("the ladder reached %d spawn attempts for restartAttempts=2, want the first start plus two retries", n)
	}
	if st := s.Status(); st.AttemptsMade != 2 {
		t.Errorf("AttemptsMade is %d, want the two the policy allows", st.AttemptsMade)
	}
}

func TestZeroAttemptsBlocksImmediatelyWithNoRetry(t *testing.T) {
	ev := new(recorder)
	s, err := New(Config{
		Dir:    t.TempDir(),
		Launch: fakeLaunch(modeExit),
		Verify: alwaysProves,
		Death:  Death{Notify: true},
		Events: ev.publish,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev.wait(t, KindExited, 15*time.Second)
	blocked := ev.wait(t, KindBlocked, 5*time.Second)
	if blocked.Hash != hash("a") {
		t.Errorf("the verdict names %q, not the config that died", blocked.Hash)
	}
	if n := ev.count(KindStarted); n != 1 {
		t.Errorf("restartAttempts=0 produced %d core.started events; the schema's floor is one spawn and no retry", n)
	}
	if st := s.Status(); !st.Blocked || st.Running {
		t.Errorf("with no attempts allowed the block is the whole answer, got %+v", st)
	}
}

// TestADeathInsideTheWindowContinuesTheEpisodeAndTheBudgetRunsOut is ODEC-38's answer as behaviour: a Core that
// dies again before the stability window elapsed does not re-arm the ladder, so a crash loop spends one budget
// and then sits in the permanent block until a user action, instead of restarting forever.
func TestADeathInsideTheWindowContinuesTheEpisodeAndTheBudgetRunsOut(t *testing.T) {
	dir := t.TempDir()
	spawns := 0
	var mu sync.Mutex
	launch := func(configPath string) *exec.Cmd {
		mu.Lock()
		n := spawns
		spawns++
		mu.Unlock()
		cmd := fakeLaunch(modeOnce)(configPath)
		if n > 0 {
			// The recovered children hold; the trigger file is the only thing that ends one. The first child keeps
			// the modeOnce marker behaviour so the first death is its own.
			cmd = fakeLaunch(modeExitWhen)(configPath)
			cmd.Env = append(cmd.Env, fakeTrigger+"="+filepath.Join(dir, "die-"+strconv.Itoa(n)))
		}
		return cmd
	}
	ev := new(recorder)
	s, err := New(Config{
		Dir:    dir,
		Launch: launch,
		Verify: alwaysProves,
		Death:  Death{Attempts: 2, Backoff: []time.Duration{time.Second, time.Second}, Stability: time.Hour, Notify: true},
		Events: ev.publish,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _, _ = s.Stop(context.Background(), false) }()

	if _, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// child 1 dies at once (inside the window): the ladder takes attempt 1, whose child stays up. Killing that
	// child from the outside is an unexpected death inside the window, so the ladder resumes at attempt 2; the
	// same kill after attempt 2 finds the budget spent and the block permanent, with no third attempt ever made.
	// dieOnce ends one unexpected death deterministically: the child exits itself the moment the trigger file
	// appears, so the test measures the ladder's arithmetic and not the timing of an external kill. The trigger is
	// removed before the next child spawns, and a new name is used per death so a pending exit cannot carry over.
	triggers := []string{}
	dieOnce := func() {
		path := filepath.Join(dir, "die-"+strconv.Itoa(len(triggers)))
		triggers = append(triggers, path)
		if err := os.WriteFile(path, []byte("die"), 0o600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for {
			st := s.Status()
			if !st.Running {
				os.Remove(path)
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("the death triggered at %s was never observed; status %+v", path, s.Status())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitCount := func(kind string, n int, timeout time.Duration) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if ev.count(kind) >= n {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s never reached %d events (have %d: %v)", kind, n, ev.count(kind), ev.kinds())
	}
	dieOnce() // in-window death of the Start child or of attempt 1's child
	waitCount(KindRestart, 1, 20*time.Second)
	dieOnce() // in-window death again
	waitCount(KindRestart, 2, 20*time.Second)
	dieOnce() // the budget is spent: this death must produce a verdict, not an attempt
	waitCount(KindBlocked, 2, 10*time.Second)
	blocked := ev.last(t, KindBlocked)
	if blocked.Hash != hash("a") {
		t.Errorf("the spent-episode verdict names %q", blocked.Hash)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := spawns
		mu.Unlock()
		if n >= 4 {
			t.Fatalf("spawn #%d happened after the episode's budget was spent; a crash loop must not restart forever", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := s.Status(); !st.Blocked || st.Running || st.Restarting {
		t.Errorf("a spent episode left Status at %+v, want the permanent block", st)
	}
}

// TestAStableCoreEarnsAFreshBudget is the other half of the same field: a child that stayed up past the window
// died into a new episode, and the budget the policy allows is the episode's, not the session's.
func TestAStableCoreEarnsAFreshBudget(t *testing.T) {
	spawns := 0
	var mu sync.Mutex
	launch := func(configPath string) *exec.Cmd {
		mu.Lock()
		spawns++
		mu.Unlock()
		cmd := fakeLaunch(modeExitAfter)(configPath)
		cmd.Env = append(cmd.Env, fakeDuration+"=700ms")
		return cmd
	}
	ev := new(recorder)
	s, err := New(Config{
		Dir:    t.TempDir(),
		Launch: launch,
		Verify: alwaysProves,
		Death:  Death{Attempts: 1, Backoff: []time.Duration{time.Second}, Stability: 300 * time.Millisecond, Notify: true},
		Events: ev.publish,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _, _ = s.Stop(context.Background(), false) }()

	if _, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Each child lives 700 ms -- past the 300 ms window -- and dies on its own, so every death re-arms the
	// single attempt: three children in a row is the fresh budget made visible, where a per-session budget
	// would have gone permanent after the first.
	waitCount := func(kind string, n int, timeout time.Duration) {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if ev.count(kind) >= n {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s never reached %d events (have %d: %v)", kind, n, ev.count(kind), ev.kinds())
	}
	waitCount(KindExited, 2, 15*time.Second)
	waitCount(KindRestart, 2, 15*time.Second)
	mu.Lock()
	n := spawns
	mu.Unlock()
	if n != 3 {
		t.Errorf("%d children were spawned, want one per episode across two stable deaths", n)
	}
	if st := s.Status(); st.Blocked {
		t.Error("a death past the window produced a spent-episode verdict")
	}
}

func TestStopReportsTheRequestAndADeadCoreIsAlreadyStopped(t *testing.T) {
	s, ev := newTestSupervisor(t, modeWait, Death{})
	st, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	res, err := s.Stop(context.Background(), false)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !res.WasRunning || res.TerminatedBy != TerminatedByRequest {
		t.Errorf("Stop returned %+v for the pid %d it was asked to end", res, st.PID)
	}
	ev.wait(t, KindExited, 5*time.Second)
	if live := s.Status(); live.Running {
		t.Fatal("Status still reports the Core this call stopped")
	}
	// doc 22 §4's point that a stop is not a death to recover from: no ladder was ever started for it.
	if ev.count(KindBlocked) != 0 || ev.count(KindRestart) != 0 {
		t.Errorf("a requested stop produced ladder verdicts: %v", ev.kinds())
	}
	again, err := s.Stop(context.Background(), false)
	if err != nil {
		t.Fatalf("the second Stop: %v", err)
	}
	if again.WasRunning || again.TerminatedBy != TerminatedByAlreadyStopped {
		t.Errorf("the second Stop returned %+v, want an honest already-stopped", again)
	}
	if _, err := s.Restart(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Restart of a stopped Core: want ErrNotRunning, got %v", err)
	}
}

// core.restart's result carries the two numbers that say a different process answers now.
func TestRestartKeepsTheConfigAndReplacesTheProcess(t *testing.T) {
	s, _ := newTestSupervisor(t, modeWait, Death{})
	first, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	res, err := s.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	defer func() { _, _ = s.Stop(context.Background(), false) }()

	if res.PreviousPID != first.PID {
		t.Errorf("previousPid is %d, want the process Stop ended (%d)", res.PreviousPID, first.PID)
	}
	if !res.Running || res.PID == first.PID {
		t.Fatalf("Restart returned %+v; a restart that kept the pid did not restart", res)
	}
	if res.Hash != hash("a") {
		t.Errorf("Restart changed the applied hash to %q, which doc 30 §4 forbids it from doing", res.Hash)
	}
}

// LIVE-13's sentence, in the only form this host can give it: a new process over the same directory can still
// say what it applied.
func TestAppliedStateOutlivesTheSupervisorThatWroteIt(t *testing.T) {
	dir := t.TempDir()
	first, err := New(Config{Dir: dir, Launch: fakeLaunch(modeWait), Verify: alwaysProves, Death: Death{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := first.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a"), ProfileID: "prof-9"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := first.Stop(context.Background(), false); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	second, err := New(Config{Dir: dir, Launch: fakeLaunch(modeWait), Verify: alwaysProves, Death: Death{}})
	if err != nil {
		t.Fatalf("New over the same directory: %v", err)
	}
	st := second.Status()
	if st.Running {
		t.Error("a supervisor that spawned nothing reports a running Core")
	}
	if st.Hash != hash("a") {
		t.Errorf("the reloaded record says %q, want the hash the first run applied", st.Hash)
	}
	if st.LastExit == nil {
		t.Error("core.status's summary promises the exit record survives a restart of the Core, and it did not survive a restart of the process")
	}
	if got := second.st.SelectedProfile; got != "prof-9" {
		t.Errorf("the selection reloaded as %q", got)
	}
}

// Containment is reported, never assumed: ADR-0005 rule 5's answer is the platform's, and core.start's own gloss
// says a false is a condition the Connector carries into its report rather than one it hides by refusing to
// start the Core at all.
func TestStartReportsContainmentAsThePlatformAnswersIt(t *testing.T) {
	s, _ := newTestSupervisor(t, modeWait, Death{})
	st, err := s.Start(context.Background(), StartParams{ConfigPath: "a.json", ConfigHash: hash("a")})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _, _ = s.Stop(context.Background(), false) }()

	if st.Contained != (st.ContainmentNote == "") {
		t.Errorf("contained=%v with note=%q: the two are one answer", st.Contained, st.ContainmentNote)
	}
	if !st.Contained {
		t.Logf("this platform reports the Core could outlive the Connector: %s", st.ContainmentNote)
	}
}

// ADR-0005 rule 6's chain, in the one form a test can prove on every platform: when the process that made the
// containment promise dies, the child it made dies with it. Windows does it by losing the job handle, Linux by
// PR_SET_PDEATHSIG, and the promise is the same sentence in both.
func TestParentDeathEndsTheContainedChild(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "holder")
	holder := exec.Command(os.Args[0], "-test.run=TestFakeCore", "-test.count=1")
	holder.Env = append(os.Environ(), fakeMode+"="+modeParent, fakeRecord+"="+record)
	out, err := holder.CombinedOutput()
	if err != nil {
		t.Fatalf("the holder exited with %v: %s", err, out)
	}
	fields := strings.Split(string(waitForFile(t, record, 20*time.Second)), "\t")
	if len(fields) < 4 {
		t.Fatalf("the holder recorded %q", fields)
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("the holder recorded pid %q: %v", fields[0], err)
	}
	defer func() {
		// Only reachable if the promise failed; an orphan that keeps selecting is not something to leave behind.
		if processAlive(pid) {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
	}()
	if fields[1] != "true" {
		t.Skipf("this platform made no containment promise (attach answered %q), so there is no death to trace", fields[2])
	}
	if fields[3] != "true" {
		t.Fatalf("the child as %d was already gone while the process that contained it was still running, so the second answer proves nothing: %q", pid, fields)
	}

	deadline := time.Now().Add(10 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Errorf("ADR-0005 rule 5: the child as %d outlived the process that contained it", pid)
	}
}

// hash keeps the fixtures readable: the hash is opaque to this package (ODEC-36), so a test only needs distinct
// 64-hex strings, not real digests.
func hash(letter string) string {
	return strings.Repeat(hexOf(letter), 64)[:64]
}

func hexOf(letter string) string {
	sum := 0
	for _, r := range letter {
		sum += int(r)
	}
	return fmt.Sprintf("%02x", sum%256)
}
