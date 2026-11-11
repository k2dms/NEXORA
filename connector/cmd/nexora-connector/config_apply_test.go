package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/apply"
	"github.com/k2dms/nexora/connector/internal/compile"
	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/contractgen"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/keystore"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

// These tests are the slice's claim: `config.apply` is one path through internal/compile, internal/apply and
// internal/supervisor. Two of them therefore run the real supervisor against a real child process, and the rest
// drive the real compiler, the real writer and the real keystore against a process seam, because a path is only
// one path if every package inside it is the one under test.

// re64 is `config.apply.json#/result/properties/configHash`'s pattern, asserted rather than trusted: a hash that
// is not 64 lowercase hex digits is a frame the peer refuses.
var re64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// The fake Core is this test binary re-execed, the device internal/supervisor's tests already use and for the same
// reason: ADR-0019 rule 4 keeps a real Xray binary out of this repository, and a command surface that owns a
// process has to be tested against one. The two modes are what `config.apply` can observe: run until asked to
// stop, or die.
const (
	agentMode    = "NEXORA_TEST_CORE_MODE"
	agentConfig  = "NEXORA_TEST_CORE_CONFIG"
	agentRecord  = "NEXORA_TEST_CORE_RECORD"
	modeHold     = "hold"
	secretValue  = "1f0e9d8c-7b6a-4958-8f7e-6d5c4b3a2910" // nexora-secret-allow: synthetic credential value, nothing behind it
	profileID    = "b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11" // nexora-secret-allow: synthetic profile id, nothing behind it
	serverID     = "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01" // nexora-secret-allow: synthetic server id, nothing behind it
	userIDRef    = "keystore://server/3f2a1c9e/user-id"
	shapeAPort   = 20100
	firstPortTag = "srv-3f2a1c9e"
)

// TestFakeCoreAgent is not a test; it is the child. Reaching it without the mode set is a mistake, and skipping is
// how a helper stays out of the run's report.
func TestFakeCoreAgent(t *testing.T) {
	mode := os.Getenv(agentMode)
	if mode == "" {
		t.Skip("helper process for the config.apply tests")
	}
	if p := os.Getenv(agentRecord); p != "" {
		// Publish the complete record before waitForFile can observe it.
		tmp := p + ".tmp"
		err := os.WriteFile(tmp, []byte(os.Getenv(agentConfig)), 0o600)
		if err == nil {
			err = os.Rename(tmp, p)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	switch mode {
	default:
		// Hold until the parent stops us. `select {}` is the honest expression of "forever" and Go's deadlock
		// detector turns it into a fatal error inside a test binary — a runtime artifact with nothing to do with
		// the behaviour under test. A sleeping goroutine is the same wait, with a wake-up the runtime owes it and
		// therefore a reason not to declare the process dead.
		for {
			time.Sleep(time.Hour)
		}
	}
}

func agentLaunch(mode, record string) supervisor.Launch {
	return func(configPath string) (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=TestFakeCoreAgent", "-test.count=1")
		cmd.Env = append(os.Environ(), agentMode+"="+mode, agentConfig+"="+configPath, agentRecord+"="+record)
		return cmd, nil
	}
}

// testProtector is the DPAPI seam's stand-in — the same base64 shape internal/keystore's own tests use. This
// package may not reach the platform, and a store whose protection is an encoding that reverses exercises every
// other rule the materializer depends on. It is deliberately not the identity function: the store fail-closes on
// a protector that returns what it was handed (ADR-0011 rule 5), so a test wiring that one cannot store anything.
type testProtector struct{}

func (testProtector) Protect(plain []byte) ([]byte, error) {
	return []byte(base64.StdEncoding.EncodeToString(plain)), nil
}

func (testProtector) Unprotect(blob []byte) ([]byte, error) {
	return base64.StdEncoding.DecodeString(string(blob))
}

// brokenProtector is the OS refusing at read time — a DPAPI call that fails or the wrong user session, which doc 32
// §11 sends to `KeystoreUnavailable` and no amount of retyping helps. Writing still works, because the refusal this
// case is about is the one that happens when the config is compiled, not when a credential is stored.
type brokenProtector struct{}

func (brokenProtector) Protect(plain []byte) ([]byte, error) {
	return []byte(base64.StdEncoding.EncodeToString(plain)), nil
}

func (brokenProtector) Unprotect([]byte) ([]byte, error) {
	return nil, errors.New("the protection could not be undone")
}

// corpus reads one contract example, the way internal/compile's tests do: the intent documents this surface
// compiles are the shipped ones, not fixtures a test invented.
func corpus(t *testing.T, name string) []byte {
	t.Helper()
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("locating the repository root: %v", err)
	}
	path := filepath.Join(root, "packages", "schema", "examples", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return data
}

func example[T any](t *testing.T, name string) T {
	t.Helper()
	value, err := compile.Decode[T](corpus(t, name))
	if err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	return value
}

func ptr[T any](v T) *T { return &v }

// baseIntent is the smallest desired state the shipped examples describe: one profile, one server, one routing
// policy, the shipped defaults. Every case that is not about a refusal starts here.
func baseIntent(t *testing.T) Intent {
	t.Helper()
	return Intent{
		Profiles: []compile.Profile{example[compile.Profile](t, "profile.fixed.json")},
		Servers:  []compile.Server{example[compile.Server](t, "server.vision-tcp-tls.json")},
		Routing:  ptr(example[compile.RoutingPolicy](t, "routing.minimal.json")),
		Settings: example[compile.Settings](t, "settings.default.json"),
		Failure:  example[compile.Failure](t, "failure-policy.default.json"),
	}
}

// fakeRecord is the applied-state record as a recorder: reads pass through to the real file, and every write —
// with its port table — is kept as the test's evidence.
type fakeRecord struct {
	mu       sync.Mutex
	inner    stateRecord
	writeErr error
	writes   []map[string]int
	reads    int
}

func (r *fakeRecord) Read() (supervisor.State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	return r.inner.Read()
}

func (r *fakeRecord) Write(st supervisor.State) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return r.writeErr
	}
	r.writes = append(r.writes, st.Held)
	return r.inner.Write(st)
}

func (r *fakeRecord) counts() (reads, writes int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads, len(r.writes)
}

// fakeCore is internal/supervisor.Supervisor's surface without a process: enough to see whether a call stopped,
// started, or left Core alone, which is most of what this command decides.
//
// It writes the applied-state record, because that is not a detail of the real supervisor — it is the record the
// surface compares the next call's hash against and the one `previousConfigHash` reports. A seam that skipped it
// would make every hash-only-no-op assertion in this file test the seam instead of the decision.
type fakeCore struct {
	mu        sync.Mutex
	status    supervisor.Status
	startErr  error
	stopErr   error
	starts    []supervisor.StartParams
	stopCalls int
	stateDir  string
}

func (c *fakeCore) Start(_ context.Context, p supervisor.StartParams) (*supervisor.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.startErr != nil {
		return nil, c.startErr
	}
	if c.stateDir != "" {
		st := supervisor.State{AppliedHash: p.ConfigHash, SelectedProfile: p.ProfileID, LastStartPID: 4000 + len(c.starts) + 1}
		if err := supervisor.WriteState(c.stateDir, st); err != nil {
			return nil, err
		}
	}
	c.starts = append(c.starts, p)
	c.status.Running = true
	c.status.Hash = p.ConfigHash
	c.status.PID = 4000 + len(c.starts)
	// The live inbound table, the way `statusLocked` reports it: what the caller handed the process, and only while
	// it is running. `server.test` reads this seam for the port to probe, so a fake that dropped it would make the
	// prober's `CoreNotRunning` rule untestable from this package.
	c.status.Inbounds = p.Inbounds
	out := c.status
	return &out, nil
}

func (c *fakeCore) Stop(_ context.Context, _ bool) (*supervisor.StopResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopCalls++
	if c.stopErr != nil {
		return nil, c.stopErr
	}
	c.status.Running = false
	c.status.Inbounds = nil
	return &supervisor.StopResult{WasRunning: true, TerminatedBy: supervisor.TerminatedByRequest}, nil
}

func (c *fakeCore) Status() supervisor.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *fakeCore) calls() (starts, stops int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.starts), c.stopCalls
}

// countingResolver forwards to the store and says how many times it was asked, which is how "touch nothing" is
// proved rather than asserted.
type countingResolver struct {
	mu    sync.Mutex
	inner apply.Resolver
	reads int
}

func (r *countingResolver) Resolve(ref string) (keystore.Secret, error) {
	r.mu.Lock()
	r.reads++
	r.mu.Unlock()
	return r.inner.Resolve(ref)
}

func (r *countingResolver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads
}

// harness is one wired surface plus everything a test needs to ask whether it touched the world.
type harness struct {
	surface *configApply
	intent  Intent
	record  *fakeRecord
	core    *fakeCore
	dirs    dirs
	store   *keystore.Store
}

type dirs struct{ state, config, keys string }

func newDirs(t *testing.T) dirs {
	t.Helper()
	return dirs{state: t.TempDir(), config: t.TempDir(), keys: t.TempDir()}
}

// newHarness builds the whole wiring with the seams: the real compiler, writer and keystore, a process that is not
// a process. Tests that need the real supervisor build their own wiring (see TestApplyIsOnePath…).
func newHarness(t *testing.T, intent Intent, p keystore.Protector) *harness {
	t.Helper()
	d := newDirs(t)
	store, err := keystore.New(d.keys, p)
	if err != nil {
		t.Fatalf("keystore.New: %v", err)
	}
	if err := store.Put(userIDRef, []byte(secretValue)); err != nil {
		t.Fatalf("storing the credential: %v", err)
	}
	h := &harness{
		intent: intent,
		record: &fakeRecord{inner: fileStateRecord{dir: d.state}},
		core:   new(fakeCore),
		dirs:   d,
		store:  store,
	}
	h.core.stateDir = d.state
	h.surface, err = newConfigApply(applyConfig{StateDir: d.state, ConfigDir: d.config},
		h.record, store, h.core)
	if err != nil {
		t.Fatalf("newConfigApply: %v", err)
	}
	return h
}

// ask is the one way these tests reach the surface, and it returns the JSON the wire would carry rather than the Go
// struct, so a field whose name or presence is wrong is caught here rather than in a struct comparison. The
// harness's intent rides every object-shaped call — a frame without it is the ids-only call the contract refuses,
// and the refusals that test that refusal pass args that are not objects at all.
func (h *harness) ask(t *testing.T, args string) (applyAnswer, map[string]any, *dispatch.Refusal) {
	t.Helper()
	if strings.HasPrefix(strings.TrimSpace(args), "{") {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(args), &obj); err != nil {
			t.Fatalf("test args are not an object: %v", args)
		}
		if _, ok := obj["intent"]; !ok {
			carried, err := json.Marshal(h.intent)
			if err != nil {
				t.Fatal(err)
			}
			obj["intent"] = carried
			fixed, err := json.Marshal(obj)
			if err != nil {
				t.Fatal(err)
			}
			args = string(fixed)
		}
	}
	return h.askRaw(t, args)
}

// askRaw sends the args exactly as given, which is how the ids-only refusal itself is tested.
func (h *harness) askRaw(t *testing.T, args string) (applyAnswer, map[string]any, *dispatch.Refusal) {
	t.Helper()
	f, err := envelope.NewRequest("apply-0001", "config.apply", json.RawMessage(args))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	result, ref := h.surface.Handle(context.Background(), f)
	if ref != nil {
		if _, ok := contract.LookupCode(ref.Code); !ok {
			t.Fatalf("%s is not a member of the projected taxonomy (%s)", ref.Code, ref.Message)
		}
		if len(ref.Message) > maxApplyMessage {
			t.Fatalf("message is %d bytes, past the envelope's %d and a frame the codec refuses: %s",
				len(ref.Message), maxApplyMessage, ref.Message)
		}
		return applyAnswer{}, nil, ref
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var answer applyAnswer
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return answer, raw, nil
}

func (h *harness) appliedConfig(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dirs.config, apply.FileName))
	if err != nil {
		t.Fatalf("the compiled config is not on disk: %v", err)
	}
	return data
}

func (h *harness) noConfigFile(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(h.dirs.config, apply.FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a call that refused or previewed left a config behind (stat: %v)", err)
	}
}

// TestApplyIsOnePathThroughCompileTheWriterAndTheSupervisor is the slice in one test: a frame naming one profile id
// ends with a config file whose credential position holds a value, a real child process handed exactly that path,
// and an applied-state record holding the hash the answer reported.
func TestApplyIsOnePathThroughCompileTheWriterAndTheSupervisor(t *testing.T) {
	d := newDirs(t)
	store, err := keystore.New(d.keys, testProtector{})
	if err != nil {
		t.Fatalf("keystore.New: %v", err)
	}
	if err := store.Put(userIDRef, []byte(secretValue)); err != nil {
		t.Fatalf("storing the credential: %v", err)
	}
	intent := baseIntent(t)
	record := filepath.Join(t.TempDir(), "handed")
	sup, err := supervisor.New(supervisor.Config{
		Dir:    d.state,
		Launch: agentLaunch(modeHold, record),
		Verify: func(string) error { return nil },
		Death:  supervisor.Death{},
	})
	if err != nil {
		t.Fatalf("supervisor.New: %v", err)
	}
	defer func() { _, _ = sup.Stop(context.Background(), false) }()

	h := &harness{intent: intent, record: &fakeRecord{inner: fileStateRecord{dir: d.state}}, dirs: d, store: store}
	h.surface, err = newConfigApply(applyConfig{StateDir: d.state, ConfigDir: d.config},
		h.record, store, sup)
	if err != nil {
		t.Fatalf("newConfigApply: %v", err)
	}

	answer, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("the path refused: %s — %s", ref.Code, ref.Message)
	}
	if !answer.Applied || !answer.Restarted || answer.UnchangedBecauseHashMatched || answer.DryRun {
		t.Errorf("first apply answered %+v", answer)
	}
	if !re64.MatchString(answer.ConfigHash) {
		t.Errorf("configHash %q is not a lowercase sha256", answer.ConfigHash)
	}
	if answer.PreviousConfigHash != "" {
		t.Errorf("nothing was applied before this call, so previousConfigHash must be absent, got %q", answer.PreviousConfigHash)
	}
	if len(answer.InboundPorts) != 1 || answer.InboundPorts[0].Port != shapeAPort || answer.InboundPorts[0].Listen != "127.0.0.1" {
		t.Errorf("inboundPorts came back as %+v", answer.InboundPorts)
	}
	if strings.Join(answer.ServerTags, ",") != firstPortTag {
		t.Errorf("serverTags came back as %v", answer.ServerTags)
	}

	// The file is the materialized one and the hash is the handle-bearing one. Both halves of that sentence are
	// ODEC-36's, and this is the wiring choosing one of them.
	data := h.appliedConfig(t)
	if !strings.Contains(string(data), secretValue) {
		t.Error("the credential position was never materialized; the file still holds a handle")
	}
	if strings.Contains(string(data), "keystore://") {
		t.Error("a handle survived into the file Core loads")
	}

	// data-ownership's core-config-file row, proven from the child's side: the path it was handed.
	got := string(waitForFile(t, record, 20*time.Second))
	if want := filepath.Join(d.config, apply.FileName); got != want {
		t.Errorf("Core was handed %q, want %q", got, want)
	}

	st, err := supervisor.ReadState(d.state)
	if err != nil {
		t.Fatal(err)
	}
	if st.AppliedHash != answer.ConfigHash {
		t.Errorf("the applied-state record holds %q and the answer said %q", st.AppliedHash, answer.ConfigHash)
	}
	if st.SelectedProfile != profileID {
		t.Errorf("the selection recorded is %q", st.SelectedProfile)
	}
	if st.LastStartPID != sup.Status().PID {
		exit := "none"
		if st.LastExit != nil {
			exit = fmt.Sprintf("%+v", *st.LastExit)
		}
		t.Errorf("the record's pid %d is not the running child's %d; last exit %s, note %q", st.LastStartPID, sup.Status().PID, exit, sup.Status().ContainmentNote)
	}
	if reads, writes := h.record.counts(); reads != 1 || writes != 1 {
		t.Errorf("the applied-state record saw %d reads and %d reservation writes, want 1 and 1", reads, writes)
	}
	if len(h.record.writes) == 1 && h.record.writes[0][firstPortTag] != shapeAPort {
		t.Errorf("the reservation table the surface stored is %v", h.record.writes[0])
	}
	if st.Held[firstPortTag] != shapeAPort {
		t.Errorf("the record on disk holds the port table %v, want %s under %s", st.Held, firstPortTag, firstPortTag)
	}

	// The same frame again. doc 22 §3 invariant 4 says a hash match restarts nothing, and the answer must say so
	// in the field the command declares for it.
	before := sup.Status()
	second, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("the repeat apply refused: %s — %s", ref.Code, ref.Message)
	}
	if !second.UnchangedBecauseHashMatched || second.Restarted || second.ConfigHash != answer.ConfigHash {
		t.Errorf("the repeat apply answered %+v", second)
	}
	if after := sup.Status(); after.PID != before.PID || !after.Running {
		t.Errorf("the repeat apply moved the process: %+v after %+v", after, before)
	}
	if _, err := sup.Stop(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	third, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("connecting after stop refused: %s — %s", ref.Code, ref.Message)
	}
	if !third.Restarted || third.UnchangedBecauseHashMatched || third.ConfigHash != answer.ConfigHash {
		t.Errorf("connecting after stop answered %+v", third)
	}
	if after := sup.Status(); !after.Running || after.PID == before.PID {
		t.Errorf("connecting after stop did not start a fresh process: %+v", after)
	}
}

// TestAChangedIntentReplacesTheFileAndTheProcess is the other half of replace-and-reconcile: the second call sees a
// different server and must both rewrite the config and end the child that loaded the old one.
func TestAChangedIntentReplacesTheFileAndTheProcess(t *testing.T) {
	d := newDirs(t)
	store, err := keystore.New(d.keys, testProtector{})
	if err != nil {
		t.Fatalf("keystore.New: %v", err)
	}
	for _, ref := range []string{userIDRef, "keystore://server/0000beef/user-id"} {
		if err := store.Put(ref, []byte(secretValue)); err != nil {
			t.Fatalf("storing %s: %v", ref, err)
		}
	}
	intent := baseIntent(t)
	first := filepath.Join(t.TempDir(), "handed-first")
	second := filepath.Join(t.TempDir(), "handed-second")
	var mu sync.Mutex
	spawns := 0
	records := []string{first, second}
	h := &harness{intent: intent, record: &fakeRecord{inner: fileStateRecord{dir: d.state}}, dirs: d, store: store}
	sup, err := supervisor.New(supervisor.Config{
		Dir: d.state,
		Launch: func(configPath string) (*exec.Cmd, error) {
			mu.Lock()
			defer mu.Unlock()
			at := spawns
			if at >= len(records) {
				at = len(records) - 1
			}
			spawns++
			cmd := exec.Command(os.Args[0], "-test.run=TestFakeCoreAgent", "-test.count=1")
			cmd.Env = append(os.Environ(), agentMode+"="+modeHold, agentConfig+"="+configPath,
				agentRecord+"="+records[at])
			return cmd, nil
		},
		Verify: func(string) error { return nil },
		Death:  supervisor.Death{},
	})
	if err != nil {
		t.Fatalf("supervisor.New: %v", err)
	}
	defer func() { _, _ = sup.Stop(context.Background(), false) }()
	h.surface, err = newConfigApply(applyConfig{StateDir: d.state, ConfigDir: d.config},
		h.record, store, sup)
	if err != nil {
		t.Fatalf("newConfigApply: %v", err)
	}

	old, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("the first apply refused: %s — %s", ref.Code, ref.Message)
	}
	beforePID := sup.Status().PID
	// The child that loaded the first config proves what it was handed before anything replaces it. Waiting here is
	// the difference between a proof and a race: the second apply ends this process, and a child that had not
	// finished booting would leave no record at all.
	if got := string(waitForFile(t, first, 20*time.Second)); got != filepath.Join(d.config, apply.FileName) {
		t.Errorf("the first child was handed %q", got)
	}

	// A second server in the same profile: a new tag, a new port, a new hash. Membership is the profile's own list,
	// so the registry alone would compile the identical config.
	extra := secondServer(t)
	intent.Servers = append(intent.Servers, extra)
	intent.Profiles[0].ServerIDs = append(intent.Profiles[0].ServerIDs, extra.ID)
	h.intent = intent

	next, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("the second apply refused: %s — %s", ref.Code, ref.Message)
	}
	if next.ConfigHash == old.ConfigHash {
		t.Fatal("the hash did not move, so this test proved nothing about reconciliation")
	}
	if !next.Applied || !next.Restarted || next.UnchangedBecauseHashMatched {
		t.Errorf("the reconciling apply answered %+v", next)
	}
	if len(next.InboundPorts) != 2 {
		t.Errorf("the second config carries %+v", next.InboundPorts)
	}
	if after := sup.Status(); after.Running && after.PID == beforePID {
		t.Error("Core is the same process as the one that loaded the previous config")
	}
	if got := string(waitForFile(t, second, 20*time.Second)); got != filepath.Join(d.config, apply.FileName) {
		t.Errorf("the reconciling apply's child was handed %q; doc 27 §8's stable name is why a replaced config is a restart and not a new file", got)
	}
	if data := h.appliedConfig(t); !strings.Contains(string(data), "srv-0000beef") {
		t.Error("the replaced config does not carry the new server's tag")
	}
}

func secondServer(t *testing.T) compile.Server {
	s := example[compile.Server](t, "server.plain-tcp.json")
	s.ID = "0000beef-1a85-4e37-9c50-6d3b8f2a4e01" // nexora-secret-allow: synthetic second server id, nothing behind it
	s.Tag = ""
	s.UserIDRef = "keystore://server/0000beef/user-id"
	return s
}

// TestSameFrameTwiceIsStillANoOp pins the no-op on the answer ODEC-36 made the only one: the second call's
// `configHash` is the first call's own string — the handle-bearing document's digest, which is deliberately not
// the hash of the bytes on disk, because the file Core loads holds the credential the intent only references.
func TestSameFrameTwiceIsStillANoOp(t *testing.T) {
	h := newHarness(t, baseIntent(t), testProtector{})
	first, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("the first apply refused: %s — %s", ref.Code, ref.Message)
	}

	answer, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("the second apply refused: %s — %s", ref.Code, ref.Message)
	}
	if !answer.UnchangedBecauseHashMatched || answer.Restarted || answer.ConfigHash != first.ConfigHash {
		t.Errorf("the second apply answered %+v, want a hash-only no-op under the first call's hash", answer)
	}
	if answer.ConfigHash == compile.Hash(h.appliedConfig(t)) {
		t.Error("configHash equals the digest of the materialized file; the written bytes are not the subject ODEC-36 named")
	}
	if starts, stops := h.core.calls(); starts != 1 || stops != 0 {
		t.Errorf("the process seam saw %d starts and %d stops, want the second call to change nothing", starts, stops)
	}
}

// TestHashesDifferBetweenTheTwoSpans died with the span: ODEC-36's answer is one byte string, and the test that
// existed to keep the two apart would now only assert that a decision has one value.

func TestDryRunTouchesNothing(t *testing.T) {
	h := newHarness(t, baseIntent(t), testProtector{})
	h.surface.resolver = &countingResolver{inner: h.store}

	answer, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"],"dryRun":true}`)
	if ref != nil {
		t.Fatalf("the dry run refused: %s — %s", ref.Code, ref.Message)
	}
	if answer.Applied || !answer.DryRun || answer.Restarted {
		t.Errorf("a preview answered %+v", answer)
	}
	if !re64.MatchString(answer.ConfigHash) {
		t.Errorf("a preview that reports no hash is not a preview: %q", answer.ConfigHash)
	}
	// `dryRun`'s own description: "Compile, validate and report -- touch nothing."
	h.noConfigFile(t)
	if starts, stops := h.core.calls(); starts != 0 || stops != 0 {
		t.Errorf("the preview touched Core: %d starts, %d stops", starts, stops)
	}
	if _, writes := h.record.counts(); writes != 0 {
		t.Error("the preview stored a port table for ports it never took")
	}
	if got := h.surface.resolver.(*countingResolver).count(); got != 0 {
		t.Errorf("the preview read the keystore %d times; the handle-bearing hash needs no secret", got)
	}
}

// The values span's preview-is-a-keystore-refusal test died with it: under the handles answer a preview hashes
// references and the store is never read, which TestDryRunTouchesNothing now asserts as the only behaviour.

func TestCompareAndSwapMismatchIsBusy(t *testing.T) {
	h := newHarness(t, baseIntent(t), testProtector{})
	if err := supervisor.WriteState(h.dirs.state, supervisor.State{AppliedHash: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	_, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"],"expectedAppliedHash":"`+strings.Repeat("b", 64)+`"}`)
	// doc 26 §2.1: a CAS mismatch is Busy, and Busy is retryable because the other caller finishes.
	if ref == nil || ref.Code != "Busy" {
		t.Fatalf("a stale apply answered %+v, want Busy", ref)
	}
	if reads, writes := h.record.counts(); reads != 1 || writes != 0 {
		t.Errorf("the refused apply read the record %d times and wrote it %d times, want the one read and no write", reads, writes)
	}
	if starts, stops := h.core.calls(); starts != 0 || stops != 0 {
		t.Errorf("the refused apply touched Core: %d starts, %d stops", starts, stops)
	}
	h.noConfigFile(t)

	// The matching case is the one that proves the comparison is against the record and not against a constant.
	if _, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"],"expectedAppliedHash":"`+strings.Repeat("a", 64)+`"}`); ref != nil {
		t.Errorf("an apply whose belief matches the record was refused: %s — %s", ref.Code, ref.Message)
	}
}

func TestSharedApplyRejectsForeignOwnerBeforeEffects(t *testing.T) {
	for _, restarting := range []bool{false, true} {
		for _, policy := range []string{restartAuto, restartNever} {
			t.Run(fmt.Sprintf("restarting=%t/policy=%s", restarting, policy), func(t *testing.T) {
				h := newHarness(t, baseIntent(t), testProtector{})
				h.surface.cfg.preserveOwner = true
				live := supervisor.Status{Running: !restarting, Restarting: restarting, ProfileID: importProfileID, Hash: statusHash}
				h.core.status = live
				resolver := &countingResolver{inner: h.store}
				h.surface.resolver = resolver
				if err := supervisor.WriteState(h.dirs.state, supervisor.State{AppliedHash: statusHash, SelectedProfile: importProfileID}); err != nil {
					t.Fatal(err)
				}
				before, err := supervisor.ReadState(h.dirs.state)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(h.dirs.config, apply.FileName)
				if err := os.WriteFile(path, []byte("private-config"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"],"restartPolicy":"`+policy+`","expectedAppliedHash":"`+strings.Repeat("a", 64)+`"}`)
				if ref == nil || ref.Code != "CoreAlreadyRunning" || ref.Close {
					t.Fatalf("foreign apply refusal = %+v, want CoreAlreadyRunning before CAS", ref)
				}
				if reads, writes := h.record.counts(); reads != 0 || writes != 0 || resolver.count() != 0 {
					t.Fatalf("foreign apply touched record or keystore: reads=%d writes=%d secrets=%d", reads, writes, resolver.count())
				}
				if starts, stops := h.core.calls(); starts != 0 || stops != 0 || !reflect.DeepEqual(h.core.Status(), live) {
					t.Fatal("foreign apply changed Core")
				}
				if raw, err := os.ReadFile(path); err != nil || string(raw) != "private-config" {
					t.Fatal("foreign apply changed the materialized config")
				}
				if after, err := supervisor.ReadState(h.dirs.state); err != nil || !reflect.DeepEqual(after, before) {
					t.Fatal("foreign apply changed persisted state")
				}
				if len(h.surface.force.servers) != 0 || h.surface.force.endpoint != "" {
					t.Fatal("foreign apply changed the in-force intent")
				}
			})
		}
	}
}

func TestSharedApplyAllowsOwnerPreviewAndStoppedCore(t *testing.T) {
	for _, tc := range []struct {
		name     string
		preserve bool
		live     supervisor.Status
		dryRun   bool
	}{
		{"same running owner", true, supervisor.Status{Running: true, ProfileID: profileID}, false},
		{"same restarting owner", true, supervisor.Status{Restarting: true, ProfileID: profileID}, false},
		{"stopped foreign owner", true, supervisor.Status{ProfileID: importProfileID}, false},
		{"foreign preview", true, supervisor.Status{Running: true, ProfileID: importProfileID}, true},
		{"nonshared foreign owner", false, supervisor.Status{Running: true, ProfileID: importProfileID}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, baseIntent(t), testProtector{})
			h.surface.cfg.preserveOwner = tc.preserve
			h.core.status = tc.live
			resolver := &countingResolver{inner: h.store}
			h.surface.resolver = resolver
			answer, _, ref := h.ask(t, fmt.Sprintf(`{"profileIds":["%s"],"dryRun":%t}`, profileID, tc.dryRun))
			if ref != nil || answer.Applied == tc.dryRun || answer.DryRun != tc.dryRun {
				t.Fatalf("answer = %+v, refusal = %+v", answer, ref)
			}
			if tc.dryRun {
				h.noConfigFile(t)
				if starts, stops := h.core.calls(); starts != 0 || stops != 0 || resolver.count() != 0 {
					t.Fatal("foreign preview touched Core or keystore")
				}
				if _, writes := h.record.counts(); writes != 0 {
					t.Fatal("foreign preview wrote state")
				}
			} else {
				wantStops := 0
				if tc.live.Running || tc.live.Restarting {
					wantStops = 1
				}
				if starts, stops := h.core.calls(); starts != 1 || stops != wantStops {
					t.Fatalf("starts=%d stops=%d, want 1/%d", starts, stops, wantStops)
				}
				if h.core.starts[0].ProfileID != profileID || len(h.appliedConfig(t)) == 0 {
					t.Fatal("accepted apply did not hand the selected profile/config to Core")
				}
			}
		})
	}
}

func TestRestartPolicyNeverLeavesTheProcessAlone(t *testing.T) {
	h := newHarness(t, baseIntent(t), testProtector{})
	answer, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"],"restartPolicy":"never"}`)
	if ref != nil {
		t.Fatalf("apply refused: %s — %s", ref.Code, ref.Message)
	}
	if !answer.Applied || answer.Restarted || answer.UnchangedBecauseHashMatched {
		t.Errorf("restartPolicy never answered %+v, want the file applied and no process change", answer)
	}
	if data := h.appliedConfig(t); !strings.Contains(string(data), secretValue) {
		t.Error("the config was not materialized")
	}
	if starts, stops := h.core.calls(); starts != 0 || stops != 0 {
		t.Errorf("restartPolicy never still touched Core: %d starts, %d stops", starts, stops)
	}
	// The applied-state record is the supervisor's to write, so a never-restart apply must not have faked one.
	st, err := supervisor.ReadState(h.dirs.state)
	if err != nil {
		t.Fatal(err)
	}
	if st.AppliedHash != "" {
		t.Errorf("the record claims %q is applied although nothing was ever started", st.AppliedHash)
	}
}

func TestApplyArgRefusals(t *testing.T) {
	h := newHarness(t, baseIntent(t), testProtector{})
	sixtyFive := strings.TrimSuffix(strings.Repeat(`"`+profileID+`",`, 65), ",")
	cases := []struct {
		name string
		args string
		why  string
	}{
		{"no args at all", ``, "profileIds"},
		{"not an object", `[]`, "not a JSON object"},
		{"an undeclared field", `{"profileIds":["` + profileID + `"],"force":true}`, "does not declare"},
		{"profileIds missing", `{"dryRun":true}`, "profileIds is required"},
		{"profileIds empty", `{"profileIds":[]}`, "between 1 and 64"},
		{"profileIds not strings", `{"profileIds":[7]}`, "array of strings"},
		{"profileIds above the ceiling", `{"profileIds":[` + sixtyFive + `]}`, "between 1 and 64"},
		{"profileIds not a UUID", `{"profileIds":["Profile-1"]}`, "lowercase UUID"},
		{"profileIds repeated", `{"profileIds":["` + profileID + `","` + profileID + `"]}`, "same id twice"},
		{"routingPolicyId does not exist any more", `{"profileIds":["` + profileID + `"],"routingPolicyId":"legacy"}`, "does not declare"},
		{"intent empty", `{"profileIds":["` + profileID + `"],"intent":null}`, "intent.profiles must hold between 1 and 64"},

		{"a profile id the intent does not carry", `{"profileIds":["11111111-1111-4111-8111-111111111111"]}`, "which the intent documents do not carry"}, // nexora-secret-allow: synthetic foreign id, nothing behind it
		{"dryRun not a bool", `{"profileIds":["` + profileID + `"],"dryRun":"yes"}`, "dryRun is not a boolean"},
		{"hash not a digest", `{"profileIds":["` + profileID + `"],"expectedAppliedHash":"deadbeef"}`, "sha256"},
		{"restartPolicy off the enum", `{"profileIds":["` + profileID + `"],"restartPolicy":"maybe"}`, "auto|never"},
	}
	for _, tc := range cases {
		_, _, ref := h.ask(t, tc.args)
		if ref == nil {
			t.Errorf("%s: the args were accepted (%s)", tc.name, tc.args)
			continue
		}
		if ref.Code != "BadRequest" {
			t.Errorf("%s: answered %s (%s), want BadRequest", tc.name, ref.Code, ref.Message)
		}
		if !strings.Contains(ref.Message, tc.why) {
			t.Errorf("%s: message %q does not name the rule %q", tc.name, ref.Message, tc.why)
		}
	}

	// The ids-only call, sent exactly as the contract that preceded the intent payload allowed it: the harness's
	// own injection is what every other case relies on, so this one goes out raw.
	if _, _, ref := h.askRaw(t, `{"profileIds":["`+profileID+`"]}`); ref == nil || !strings.Contains(ref.Message, "intent is required") {
		t.Errorf("the ids-only call was answered %+v, want the intent-is-required refusal", ref)
	}

	// Two shapes the pattern allows and a caller may mean differently: null, and the empty string.
	for _, args := range []string{
		`{"profileIds":["` + profileID + `"],"expectedAppliedHash":null}`,
		`{"profileIds":["` + profileID + `"],"expectedAppliedHash":""}`,
	} {
		if _, _, ref := h.ask(t, args); ref != nil && ref.Code == "BadRequest" {
			t.Errorf("%s is legal on the wire and was refused as %s: %s", args, ref.Code, ref.Message)
		}
	}
}

// TestRefusalCodesDrivesTheRealRefusals is the table the slice turns on. Each case makes one of the three packages
// refuse for real — a pipeline stage, the keystore, the writer, the reservation store, the process owner — and
// asserts the taxonomy code that reaches the wire plus the cause the message kept. A reworded refusal in any of
// them fails a row here rather than quietly changing an answer a client acts on.
func TestRefusalCodesDrivesTheRealRefusals(t *testing.T) {
	const args = `{"profileIds":["` + profileID + `"]}`

	cases := []struct {
		name  string
		build func(t *testing.T) *harness
		want  string
		cause string
	}{
		{
			// doc 27 §2 stage 1's own sentence: "dangling ⇒ InvalidProfile".
			name: "a profile naming a server that is not there",
			build: func(t *testing.T) *harness {
				return harnessWith(t, func(_ *testing.T, in *Intent) { in.Servers = nil })
			},
			want: "InvalidProfile", cause: "which is not in the registry",
		},
		{
			// The prefix default: anything else the pipeline refuses is a document that failed entity validation.
			name: "a profile of the legacy kind",
			build: func(t *testing.T) *harness {
				return harnessWith(t, func(_ *testing.T, in *Intent) { in.Profiles[0].Kind = "legacy" })
			},
			want: "InvalidProfile", cause: "compiles `connector` profiles only",
		},
		{
			name:  "a flow the transport does not admit",
			build: func(t *testing.T) *harness { return harnessWith(t, wsVisionServer) },
			want:  "UnsupportedCombination", cause: "the schema admits that flow only over",
		},
		{
			// doc 22 §3 invariant 6, the use of the code ODEC-03 keeps open.
			name: "settings asking for a non-loopback bind",
			build: func(t *testing.T) *harness {
				return harnessWith(t, func(_ *testing.T, in *Intent) { in.Settings.Ports.Host = "0.0.0.0" })
			},
			want: "UnsupportedCombination", cause: "only ever binds",
		},
		{
			// doc 30 §8, the other use ODEC-03 keeps open.
			name: "a block range that overlaps the allocation range",
			build: func(t *testing.T) *harness {
				return harnessWith(t, func(_ *testing.T, in *Intent) { in.Failure.Block.Range.Min = 20100 })
			},
			want: "UnsupportedCombination", cause: "overlaps the block range",
		},
		{
			// doc 27 §9's invariant row and doc 29's ceiling, in the compiler's own words.
			name: "more inbounds than a config may carry",
			build: func(t *testing.T) *harness {
				return harnessWith(t, func(_ *testing.T, in *Intent) {
					in.Servers = manyServers(t, 65)
					in.Profiles[0].ServerIDs = idsOf(in.Servers)
				})
			},
			want: "QuotaExceeded", cause: "which is what QuotaExceeded names",
		},
		{
			name: "an allocation range with no room left",
			build: func(t *testing.T) *harness {
				return harnessWith(t, func(_ *testing.T, in *Intent) {
					in.Servers = manyServers(t, 4)
					in.Profiles[0].ServerIDs = idsOf(in.Servers)
					in.Settings.Ports.Range.Max = 20101
				})
			},
			want: "QuotaExceeded", cause: "no free port left in",
		},
		{
			// doc 32 §11: a missing credential is the one that sends the user to retype.
			name:  "a credential that was never stored",
			build: func(t *testing.T) *harness { return harnessWithoutSecret(t) },
			want:  "InvalidProfile", cause: "no secret is stored",
		},
		{
			// doc 32 §11: the store itself refusing, which retyping cannot help.
			name:  "a store the platform will not open",
			build: func(t *testing.T) *harness { return harnessWithProtector(t, brokenProtector{}) },
			want:  "KeystoreUnavailable", cause: "the keystore is unavailable",
		},
		{
			// The fourth code doc 32 §11 does not name and the keystore's own table supplies.
			name: "a reference the grammar rejects",
			build: func(t *testing.T) *harness {
				return harnessWith(t, func(_ *testing.T, in *Intent) { in.Servers[0].UserIDRef = "keystore://server/3f2a1c9e" })
			},
			want: "SchemaMismatch", cause: "does not match the recorded shape",
		},
		{
			name:  "a config directory that is a file",
			build: harnessConfigDirIsAFile,
			want:  "ConfigApplyFailed", cause: "",
		},
		{
			name: "the port table refusing to be stored",
			build: func(t *testing.T) *harness {
				h := newHarness(t, baseIntent(t), testProtector{})
				h.record.writeErr = errors.New("the reservation store is full")
				return h
			},
			want: "ConfigApplyFailed", cause: "reservation store is full",
		},
		{
			name: "Core refusing to stop",
			build: func(t *testing.T) *harness {
				h := newHarness(t, baseIntent(t), testProtector{})
				h.core.status.Running = true
				h.core.status.Hash = strings.Repeat("1", 64)
				h.core.stopErr = errors.New("the graceful window closed and the hard cap passed")
				return h
			},
			want: "ConfigApplyFailed", cause: "did not stop",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.build(t)
			_, _, ref := h.ask(t, args)
			if ref == nil {
				t.Fatal("the path accepted what this case was built to refuse")
			}
			if ref.Code != tc.want {
				t.Errorf("code %s, want %s (%s)", ref.Code, tc.want, ref.Message)
			}
			if tc.cause != "" && !strings.Contains(ref.Message, tc.cause) {
				t.Errorf("message %q lost the cause %q", ref.Message, tc.cause)
			}
		})
	}
}

// TestThePreExecProofRefusalIsIntegrityMismatch is doc 22 §4's sentence — an unbootable Core is surfaced as
// IntegrityMismatch — driven by the real supervisor's real pre-exec check on the real path.
func TestThePreExecProofRefusalIsIntegrityMismatch(t *testing.T) {
	d := newDirs(t)
	store, err := keystore.New(d.keys, testProtector{})
	if err != nil {
		t.Fatalf("keystore.New: %v", err)
	}
	if err := store.Put(userIDRef, []byte(secretValue)); err != nil {
		t.Fatalf("storing the credential: %v", err)
	}
	sup, err := supervisor.New(supervisor.Config{
		Dir:    d.state,
		Launch: agentLaunch(modeHold, filepath.Join(d.state, "never-written")),
		Verify: func(string) error { return errors.New("the install receipt does not match the file") },
		Death:  supervisor.Death{},
	})
	if err != nil {
		t.Fatalf("supervisor.New: %v", err)
	}
	h := &harness{intent: baseIntent(t), record: &fakeRecord{inner: fileStateRecord{dir: d.state}}, dirs: d, store: store}
	h.surface, err = newConfigApply(applyConfig{StateDir: d.state, ConfigDir: d.config},
		h.record, store, sup)
	if err != nil {
		t.Fatalf("newConfigApply: %v", err)
	}
	_, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref == nil {
		t.Fatal("a Core the verifier refused was reported as applied")
	}
	if ref.Code != "IntegrityMismatch" {
		t.Errorf("code %s, want IntegrityMismatch (%s)", ref.Code, ref.Message)
	}
	if !strings.Contains(ref.Message, "install receipt") {
		t.Errorf("the verifier's own reason was lost: %s", ref.Message)
	}
	if st := sup.Status(); st.Running {
		t.Error("the supervisor reports a process the pre-exec proof refused")
	}
	// The config is on disk and Core never ran: that is the state a caller has to be able to see, and it is why
	// the answer is a security code rather than a config one.
	if _, err := os.Stat(filepath.Join(d.config, apply.FileName)); err != nil {
		t.Errorf("the write did not happen before the integrity refusal: %v", err)
	}
}

// TestASpawnFailureAfterTheRenameIsReportedAndNotRolledBack pins the deliberate half of doc 27 §8 and doc 32 §13:
// a config that is on disk with a Core that never came up is reported, and the previous bytes are not put back.
func TestASpawnFailureAfterTheRenameIsReportedAndNotRolledBack(t *testing.T) {
	d := newDirs(t)
	store, err := keystore.New(d.keys, testProtector{})
	if err != nil {
		t.Fatalf("keystore.New: %v", err)
	}
	if err := store.Put(userIDRef, []byte(secretValue)); err != nil {
		t.Fatalf("storing the credential: %v", err)
	}
	sup, err := supervisor.New(supervisor.Config{
		Dir: d.state,
		// A command line no ADR-0019 repository could contain: the exec itself fails, after the rename.
		Launch: func(string) (*exec.Cmd, error) { return exec.Command("nexora-core-binary-this-does-not-exist"), nil },
		Verify: func(string) error { return nil },
		Death:  supervisor.Death{},
	})
	if err != nil {
		t.Fatalf("supervisor.New: %v", err)
	}
	h := &harness{intent: baseIntent(t), record: &fakeRecord{inner: fileStateRecord{dir: d.state}}, dirs: d, store: store}
	h.surface, err = newConfigApply(applyConfig{StateDir: d.state, ConfigDir: d.config},
		h.record, store, sup)
	if err != nil {
		t.Fatalf("newConfigApply: %v", err)
	}
	_, _, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref == nil || ref.Code != "ConfigApplyFailed" {
		t.Fatalf("a Core that could not exec answered %+v, want ConfigApplyFailed", ref)
	}
	if _, err := os.Stat(filepath.Join(d.config, apply.FileName)); err != nil {
		t.Errorf("the apply rolled the file back, which doc 27 §8 does not ask for and doc 32 §13 makes unnecessary: %v", err)
	}
	// No record: the supervisor writes it only for a process it actually spawned, so the next Connector restart
	// will not believe this config is in force.
	st, err := supervisor.ReadState(d.state)
	if err != nil || st.AppliedHash != "" {
		t.Errorf("a Core that never spawned left the record claiming %q (err %v)", st.AppliedHash, err)
	}
}

func TestNewConfigApplyRefusesUndecidedWiring(t *testing.T) {
	complete := applyConfig{StateDir: "state", ConfigDir: "config"}
	record, resolver, core := stateRecord(fileStateRecord{dir: "state"}), apply.Resolver(&countingResolver{}), coreProcess(new(fakeCore))
	cases := []struct {
		name    string
		cfg     applyConfig
		record  stateRecord
		resolve apply.Resolver
		core    coreProcess
		want    string
	}{
		{"everything present", complete, record, resolver, core, ""},
		{"no state directory", applyConfig{ConfigDir: "config"}, record, resolver, core, "state directory"},
		{"no config directory", applyConfig{StateDir: "state"}, record, resolver, core, "config directory"},
		{"no applied-state record", complete, nil, resolver, core, "no applied-state record"},
		{"no keystore resolver", complete, record, nil, core, "no keystore resolver"},
		{"no Core process owner", complete, record, resolver, nil, "no Core process owner"},
	}
	for _, tc := range cases {
		_, err := newConfigApply(tc.cfg, tc.record, tc.resolve, tc.core)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: the wiring was refused: %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error naming %q", tc.name, err, tc.want)
		}
	}
}

func TestTheAnswerCarriesOnlyFieldsTheResultSchemaDeclares(t *testing.T) {
	h := newHarness(t, baseIntent(t), testProtector{})
	_, raw, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("apply refused: %s — %s", ref.Code, ref.Message)
	}
	// `config.apply.json#/result` is additionalProperties:false, so an extra name here is a frame the peer refuses.
	declared := map[string]bool{
		"applied": true, "dryRun": true, "configHash": true, "previousConfigHash": true,
		"restarted": true, "unchangedBecauseHashMatched": true, "inboundPorts": true,
		"serverTags": true, "unrepresentable": true,
	}
	for k := range raw {
		if !declared[k] {
			t.Errorf("the result carries %q, which the command does not declare", k)
		}
	}
	for _, k := range []string{"applied", "configHash", "restarted"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("the result is missing %q, which the command requires", k)
		}
	}
	ports, ok := raw["inboundPorts"].([]any)
	if !ok {
		t.Fatal("inboundPorts is absent or not an array")
	}
	for _, entry := range ports {
		p, _ := entry.(map[string]any)
		if listen, _ := p["listen"].(string); listen != "127.0.0.1" && listen != "::1" {
			t.Errorf("inboundPorts.listen is %q, outside the enum the result declares", listen)
		}
		port, _ := p["port"].(float64)
		if port < 1 || port > 65535 {
			t.Errorf("inboundPorts.port is %v, outside 1..65535", port)
		}
	}
}

func TestAReportedRuleTravelsInTheAnswer(t *testing.T) {
	policy := example[compile.RoutingPolicy](t, "routing.two-plane.json")
	h := harnessWith(t, func(t *testing.T, in *Intent) {
		in.Routing = &policy
	})
	answer, raw, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil {
		t.Fatalf("apply refused: %s — %s", ref.Code, ref.Message)
	}
	if !answer.Applied {
		t.Fatal("the answer says nothing was applied, so the report has no apply to travel with")
	}
	entries, ok := raw["unrepresentable"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("the answer carries %+v, want the shipped policy's two authored reports", raw["unrepresentable"])
	}
	for i, e := range entries {
		entry, _ := e.(map[string]any)
		want := policy.CorePlane.Unrepresentable[i]
		if entry["source"] != want.Source || entry["reason"] != want.Reason {
			t.Errorf("authored report changed: got %v, want %+v", entry, want)
		}
	}
	if got := len(answer.Unrepresentable); got != 2 {
		t.Errorf("the typed answer carries %+v, so one of the two reports was read as an absence", answer.Unrepresentable)
	}
	loaded := string(h.appliedConfig(t))
	if !strings.Contains(loaded, `"sniffing"`) {
		t.Error("the compiled config carries no sniffing block; the shipped policy enabled it and the inbound field is the position the pinned release reads")
	}
	if !strings.Contains(loaded, `"blackhole"`) || !strings.Contains(loaded, `"freedom"`) {
		t.Error("the compiled config carries no declared built-ins; a rule naming block or direct would have no destination at runtime")
	}
}

// harnessWith builds a wired surface over a mutated intent, the shape most refusal cases need.
func harnessWith(t *testing.T, mutate func(*testing.T, *Intent)) *harness {
	t.Helper()
	intent := baseIntent(t)
	mutate(t, &intent)
	return newHarness(t, intent, testProtector{})
}

func harnessWithProtector(t *testing.T, p keystore.Protector) *harness {
	t.Helper()
	return newHarness(t, baseIntent(t), p)
}

// harnessWithoutSecret is the same wiring with the store empty: the reference is legal, the value behind it is not
// there, and doc 32 §11 says that is the user's problem rather than the machine's.
func harnessWithoutSecret(t *testing.T) *harness {
	t.Helper()
	d := newDirs(t)
	store, err := keystore.New(d.keys, testProtector{})
	if err != nil {
		t.Fatalf("keystore.New: %v", err)
	}
	h := &harness{intent: baseIntent(t), record: &fakeRecord{inner: fileStateRecord{dir: d.state}}, dirs: d, store: store, core: new(fakeCore)}
	h.core.stateDir = d.state
	h.surface, err = newConfigApply(applyConfig{StateDir: d.state, ConfigDir: d.config},
		h.record, store, h.core)
	if err != nil {
		t.Fatalf("newConfigApply: %v", err)
	}
	return h
}

// harnessConfigDirIsAFile is the wiring with the place the compiled config lives replaced by a regular file: doc 27
// §8's rename has nowhere to land, and the refusal is the writer's, not the pipeline's.
func harnessConfigDirIsAFile(t *testing.T) *harness {
	t.Helper()
	d := newDirs(t)
	blocker := filepath.Join(t.TempDir(), "config-dir")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := keystore.New(d.keys, testProtector{})
	if err != nil {
		t.Fatalf("keystore.New: %v", err)
	}
	if err := store.Put(userIDRef, []byte(secretValue)); err != nil {
		t.Fatalf("storing the credential: %v", err)
	}
	h := &harness{intent: baseIntent(t), record: &fakeRecord{inner: fileStateRecord{dir: d.state}}, dirs: dirs{state: d.state, config: blocker, keys: d.keys}, store: store, core: new(fakeCore)}
	h.core.stateDir = d.state
	h.surface, err = newConfigApply(applyConfig{StateDir: d.state, ConfigDir: blocker},
		h.record, store, h.core)
	if err != nil {
		t.Fatalf("newConfigApply: %v", err)
	}
	return h
}

// wsVisionServer is doc 23 §10's refusal: REALITY-class flow over a websocket transport. The stored tag is dropped
// rather than rewritten because doc 23 §6 makes it a derivation, and a case about the transport has no business
// carrying a second, unrelated refusal.
func wsVisionServer(t *testing.T, in *Intent) {
	s := example[compile.Server](t, "server.ws-tls.json")
	s.ID = serverID
	s.Tag = ""
	flow := "xtls-rprx-vision"
	s.Flow = &flow
	in.Servers = []compile.Server{s}
}

// manyServers builds n distinct server documents from the shipped plain-TCP example. The ids are UUID-shaped
// because the tag derives from their first eight characters, and doc 23 §6's derivation is part of what the
// ceiling cases exercise. The first entry keeps the shipped example's own id so that a case about a ceiling is not
// silently also a case about the profile's `defaultServerId` pointing at nothing.
func manyServers(t *testing.T, n int) []compile.Server {
	t.Helper()
	base := example[compile.Server](t, "server.plain-tcp.json")
	out := make([]compile.Server, 0, n)
	for i := 0; i < n; i++ {
		s := base
		if i == 0 {
			s.ID = serverID
			s.UserIDRef = userIDRef
		} else {
			s.ID = fmt.Sprintf("%08x-1a85-4e37-9c50-6d3b8f2a4e01", i)
			s.UserIDRef = fmt.Sprintf("keystore://server/%08x/user-id", i)
		}
		s.Tag = ""
		out = append(out, s)
	}
	return out
}

func idsOf(servers []compile.Server) []string {
	out := make([]string, len(servers))
	for i, s := range servers {
		out[i] = s.ID
	}
	return out
}

func waitForFile(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if data, err := os.ReadFile(path); err == nil {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared, so the child never recorded what it was handed", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWebSocketPredicateReportTravelsInTheApplyAnswer(t *testing.T) {
	h := harnessWith(t, func(t *testing.T, in *Intent) {
		policy := example[compile.RoutingPolicy](t, "routing.minimal.json")
		policy.CorePlane.Rules = []compile.CoreRule{
			{Domain: []string{"private.example"}, Protocol: []string{"http", "ws"}, Outbound: "block"},
			{Network: []string{"tcp"}, Outbound: "direct"},
		}
		in.Routing = &policy
	})
	answer, raw, ref := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if ref != nil || !answer.Applied {
		t.Fatalf("apply with reported ws predicate: answer=%+v refusal=%+v", answer, ref)
	}
	if len(answer.Unrepresentable) != 1 || answer.Unrepresentable[0].Source != "corePlane rule #0" ||
		!strings.Contains(answer.Unrepresentable[0].Reason, "ws") {
		t.Fatalf("missing ws report: %+v", answer.Unrepresentable)
	}
	entries, ok := raw["unrepresentable"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("wire answer lost ws report: %+v", raw["unrepresentable"])
	}
	loaded := string(h.appliedConfig(t))
	if strings.Contains(loaded, "private.example") || strings.Contains(loaded, `"blackhole"`) ||
		!strings.Contains(loaded, `"freedom"`) {
		t.Fatalf("reported rule leaked into config or adjacent rule disappeared: %s", loaded)
	}
}
