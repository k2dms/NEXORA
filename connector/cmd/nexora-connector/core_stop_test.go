package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/contractgen"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

type stopCore struct {
	*fakeCore
	stop func(context.Context, bool) (*supervisor.StopResult, error)
}

func (c *stopCore) Stop(ctx context.Context, graceful bool) (*supervisor.StopResult, error) {
	return c.stop(ctx, graceful)
}

func stopSurface(t *testing.T, core coreProcess) *coreStop {
	t.Helper()
	s, err := newCoreStop(core, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func stopFrame(args string) *envelope.Frame {
	m := "core.stop"
	return &envelope.Frame{V: 1, ID: "stop-1", Type: envelope.TypeRequest, Method: &m, Args: json.RawMessage(args)}
}

func stopAnswer(t *testing.T, s *coreStop, args string) map[string]any {
	t.Helper()
	ans, refusal := s.Handle(context.Background(), stopFrame(args))
	if refusal != nil {
		t.Fatalf("core.stop refused a legal call: %s %s", refusal.Code, refusal.Message)
	}
	raw, err := json.Marshal(ans)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type stopContract struct {
	TimeoutMS int      `json:"timeoutMs"`
	Errors    []string `json:"errors"`
	Result    struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	} `json:"result"`
}

func readStopContract(t *testing.T) stopContract {
	t.Helper()
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "packages", "schema", "protocol", "commands", "core.stop.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc stopContract
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestNewCoreStopRequiresProcessOwner(t *testing.T) {
	if s, err := newCoreStop(nil, t.TempDir()); err == nil || s != nil {
		t.Fatalf("newCoreStop(nil) = %v, %v", s, err)
	}
	if s, err := newCoreStop(new(fakeCore), ""); err == nil || s != nil {
		t.Fatal("core.stop accepted no compiled-config directory")
	}
	stopSurface(t, new(fakeCore))
}

func TestCoreStopGracefulArgs(t *testing.T) {
	cases := []struct {
		args     string
		graceful bool
	}{
		{``, true},
		{`null`, true},
		{`{}`, true},
		{` { } `, true},
		{`{"graceful":true}`, true},
		{`{"graceful":false}`, false},
		{`{"reason":""}`, true},
		{`{"reason":"private-stop-note"}`, true},
		{`{"reason":"` + strings.Repeat("a", 40) + `"}`, true},
		{`{"reason":"` + strings.Repeat("界", 40) + `"}`, true},
		{`{"reason":"` + strings.Repeat(`\u754c`, 40) + `"}`, true},
		{`{"graceful":false,"reason":"private-stop-note"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			calls := 0
			s := stopSurface(t, &stopCore{stop: func(_ context.Context, graceful bool) (*supervisor.StopResult, error) {
				calls++
				if graceful != tc.graceful {
					t.Errorf("graceful = %v, want %v", graceful, tc.graceful)
				}
				return &supervisor.StopResult{TerminatedBy: supervisor.TerminatedByAlreadyStopped}, nil
			}})
			got := stopAnswer(t, s, tc.args)
			if calls != 1 || got["running"] != false || got["wasRunning"] != false {
				t.Errorf("calls = %d, answer = %v", calls, got)
			}
		})
	}
}

func TestCoreStopRejectsClosedAndMalformedArgsWithoutStopping(t *testing.T) {
	const private = "private-stop-note"
	cases := []string{
		` `, `[]`, `[{}]`, `"` + private + `"`, `true`, `false`, `7`,
		`{"reason":"` + private + `"`, `{} {}`, `{"reason":}`, `{"graceful":true,}`,
		`{"force":true}`, `{"` + private + `":true}`, `{"Graceful":false}`,
		`{"graceful":null}`, `{"graceful":0}`, `{"graceful":"` + private + `"}`,
		`{"graceful":[]}`, `{"graceful":{}}`,
		`{"reason":null}`, `{"reason":false}`, `{"reason":7}`, `{"reason":[]}`,
		`{"reason":{"` + private + `":true}}`,
		`{"reason":"` + strings.Repeat("a", 41) + `"}`,
		`{"reason":"` + strings.Repeat("界", 41) + `"}`,
		`{"reason":"` + strings.Repeat(`\u754c`, 41) + `"}`,
	}
	calls := 0
	s := stopSurface(t, &stopCore{stop: func(context.Context, bool) (*supervisor.StopResult, error) {
		calls++
		return &supervisor.StopResult{}, nil
	}})
	for _, args := range cases {
		ans, refusal := s.Handle(context.Background(), stopFrame(args))
		if ans != nil || refusal == nil || refusal.Code != "Internal" || refusal.Close {
			t.Errorf("args %q: answer = %v, refusal = %v, want Internal", args, ans, refusal)
			continue
		}
		if !strings.HasPrefix(refusal.Message, "core.stop:") || strings.Contains(refusal.Message, private) || len(refusal.Message) > maxApplyMessage {
			t.Errorf("args %q: unsafe refusal message %q", args, refusal.Message)
		}
	}
	if calls != 0 {
		t.Errorf("invalid args invoked Stop %d times", calls)
	}
}

func TestCoreStopErrorMappingUsesDeclaredCodesAndKeepsCausesPrivate(t *testing.T) {
	doc := readStopContract(t)
	const private = "private-stop-note"
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"internal", errors.New(private), "Internal"},
		{"canceled", context.Canceled, "Timeout"},
		{"deadline", context.DeadlineExceeded, "Timeout"},
		{"wrapped cancellation", fmt.Errorf("%s: %w", private, context.Canceled), "Timeout"},
		{"wrapped deadline", fmt.Errorf("%s: %w", private, context.DeadlineExceeded), "Timeout"},
		{"not a context sentinel", errors.New("context deadline exceeded: " + private), "Internal"},
		{"not running sentinel", supervisor.ErrNotRunning, "Internal"},
		{"integrity sentinel", supervisor.ErrIntegrity, "Internal"},
		{"already running sentinel", supervisor.ErrAlreadyRunning, "Internal"},
		{"no result", nil, "Internal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := stopSurface(t, &stopCore{stop: func(context.Context, bool) (*supervisor.StopResult, error) {
				return nil, tc.err
			}})
			ans, refusal := s.Handle(context.Background(), stopFrame(`{"reason":"`+private+`"}`))
			if ans != nil || refusal == nil || refusal.Code != tc.code || refusal.Close {
				t.Fatalf("answer = %v, refusal = %v, want %s", ans, refusal, tc.code)
			}
			if !slices.Contains(doc.Errors, refusal.Code) {
				t.Errorf("%s is not declared by core.stop", refusal.Code)
			}
			if !strings.HasPrefix(refusal.Message, "core.stop:") || strings.Contains(refusal.Message, private) || len(refusal.Message) > maxApplyMessage {
				t.Errorf("unsafe refusal message %q", refusal.Message)
			}
		})
	}
}

func TestCoreStopBoundsContextByContractAndPreservesEarlierDeadline(t *testing.T) {
	limit := time.Duration(readStopContract(t).TimeoutMS) * time.Millisecond
	for _, parentLimit := range []time.Duration{0, time.Minute, time.Second} {
		t.Run(parentLimit.String(), func(t *testing.T) {
			parent := context.Background()
			if parentLimit != 0 {
				var cancel context.CancelFunc
				parent, cancel = context.WithTimeout(parent, parentLimit)
				defer cancel()
			}
			var received context.Context
			before := time.Now()
			s := stopSurface(t, &stopCore{stop: func(ctx context.Context, _ bool) (*supervisor.StopResult, error) {
				received = ctx
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("Stop received an unbounded context")
				}
				if parentDeadline, ok := parent.Deadline(); ok && parentLimit < limit {
					if !deadline.Equal(parentDeadline) {
						t.Errorf("deadline = %v, want parent's %v", deadline, parentDeadline)
					}
				} else if deadline.Before(before.Add(limit)) || deadline.After(time.Now().Add(limit)) {
					t.Errorf("deadline = %v, want the contract's %v bound", deadline, limit)
				}
				return &supervisor.StopResult{TerminatedBy: supervisor.TerminatedByAlreadyStopped}, nil
			}})
			if _, refusal := s.Handle(parent, stopFrame(`{}`)); refusal != nil {
				t.Fatal(refusal)
			}
			if !errors.Is(received.Err(), context.Canceled) {
				t.Errorf("stop context not released after Handle: %v", received.Err())
			}
		})
	}
}

func TestCoreStopParentCancellationAndDeadlineMapToTimeout(t *testing.T) {
	for _, mode := range []string{"canceled before call", "canceled during stop", "expired before call", "expires during stop"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			switch mode {
			case "canceled before call":
				cancel()
			case "expired before call":
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			case "expires during stop":
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
			}
			defer cancel()
			s := stopSurface(t, &stopCore{stop: func(stopCtx context.Context, _ bool) (*supervisor.StopResult, error) {
				if mode == "canceled during stop" {
					cancel()
				}
				<-stopCtx.Done()
				return nil, errors.New("private-stop-note")
			}})
			ans, refusal := s.Handle(ctx, stopFrame(`{}`))
			if ans != nil || refusal == nil || refusal.Code != "Timeout" || strings.Contains(refusal.Message, "private-stop-note") {
				t.Fatalf("answer = %v, refusal = %v, want Timeout", ans, refusal)
			}
		})
	}
}

func TestCoreStopConvertsOnlyDeclaredResultFields(t *testing.T) {
	doc := readStopContract(t)
	for _, by := range []string{"request", "parent-death", "crash", "already-stopped"} {
		t.Run(by, func(t *testing.T) {
			result := supervisor.StopResult{WasRunning: by != "already-stopped", ExitCode: -1, TerminatedBy: by, DurationMS: 42}
			s := stopSurface(t, &stopCore{stop: func(context.Context, bool) (*supervisor.StopResult, error) {
				return &result, nil
			}})
			got := stopAnswer(t, s, `{}`)
			want := map[string]any{
				"running": false, "wasRunning": result.WasRunning, "exitCode": float64(result.ExitCode),
				"terminatedBy": result.TerminatedBy, "durationMs": float64(result.DurationMS),
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("answer = %v, want %v", got, want)
			}
			for name := range got {
				if _, declared := doc.Result.Properties[name]; !declared {
					t.Errorf("answer carries undeclared field %q", name)
				}
			}
			for _, name := range doc.Result.Required {
				if _, present := got[name]; !present {
					t.Errorf("answer omits required field %q", name)
				}
			}
		})
	}
}

func TestCoreStopCleanupPreservesConfigOnStopFailure(t *testing.T) {
	s := stopSurface(t, &stopCore{stop: func(context.Context, bool) (*supervisor.StopResult, error) {
		return nil, context.DeadlineExceeded
	}})
	if err := os.WriteFile(s.configPath, []byte("private-config"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, ref := s.Handle(context.Background(), stopFrame(`{}`))
	if ref == nil || ref.Code != "Timeout" {
		t.Fatalf("stop failure = %+v", ref)
	}
	if raw, err := os.ReadFile(s.configPath); err != nil || string(raw) != "private-config" {
		t.Fatal("failed stop removed or changed the running config")
	}
}

func TestCoreStopReportsCleanupFailureAndRetries(t *testing.T) {
	s := stopSurface(t, new(fakeCore))
	if err := os.Mkdir(s.configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(s.configPath, "private-name")
	if err := os.WriteFile(child, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, ref := s.Handle(context.Background(), stopFrame(`{}`))
	if ref == nil || ref.Code != "Internal" || strings.Contains(ref.Message, child) {
		t.Fatalf("cleanup refusal = %+v", ref)
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	stopAnswer(t, s, `{}`)
	if _, err := os.Stat(s.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cleanup retry left the config")
	}
}

func TestCoreStopRealSupervisorLifecycleAndRepeatStop(t *testing.T) {
	for _, args := range []string{`{}`, `{"graceful":false}`} {
		t.Run(args, func(t *testing.T) {
			d := newDirs(t)
			record := filepath.Join(d.config, "handed")
			events := make(chan supervisor.Event, 4)
			sup, err := supervisor.New(supervisor.Config{
				Dir:    d.state,
				Launch: agentLaunch(modeHold, record),
				Verify: func(string) error { return nil },
				Death:  supervisor.Death{},
				Events: func(e supervisor.Event) { events <- e },
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if _, err := sup.Stop(ctx, false); err != nil {
					t.Errorf("cleanup Stop: %v", err)
				}
			})
			s, err := newCoreStop(sup, d.config)
			if err != nil {
				t.Fatal(err)
			}
			if got := stopAnswer(t, s, ``); got["running"] != false || got["wasRunning"] != false || got["terminatedBy"] != "already-stopped" {
				t.Fatalf("first-run stop = %v", got)
			}
			configPath := s.configPath
			if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			started, err := sup.Start(ctx, supervisor.StartParams{ConfigPath: configPath, ConfigHash: statusHash})
			if err != nil {
				t.Fatal(err)
			}
			if got := string(waitForFile(t, record, 20*time.Second)); got != configPath {
				t.Fatalf("child received %q, want %q", got, configPath)
			}
			if live := sup.Status(); !live.Running || live.PID != started.PID || live.PID <= 0 {
				t.Fatalf("live status = %+v", live)
			}
			got := stopAnswer(t, s, args)
			if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stopped Core retained its materialized config: %v", err)
			}
			down := sup.Status()
			if down.Running || down.Restarting || down.LastExit == nil || down.Hash != statusHash {
				t.Fatalf("stopped status = %+v", down)
			}
			if got["running"] != false || got["wasRunning"] != true || got["terminatedBy"] != "request" || got["exitCode"] != float64(down.LastExit.Code) {
				t.Errorf("stop answer = %v, exit = %+v", got, down.LastExit)
			}
			if duration, ok := got["durationMs"].(float64); !ok || duration < 0 {
				t.Errorf("durationMs = %v", got["durationMs"])
			}
			persisted, err := supervisor.ReadState(d.state)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.AppliedHash != statusHash || persisted.LastStartPID != started.PID || persisted.LastExit == nil {
				t.Fatalf("persisted state = %+v, stopped status = %+v", persisted, down)
			}
			if !persisted.LastExit.At.Equal(down.LastExit.At) || persisted.LastExit.Code != down.LastExit.Code || persisted.LastExit.Signal != down.LastExit.Signal {
				t.Errorf("persisted exit = %+v, want %+v", persisted.LastExit, down.LastExit)
			}
			for _, kind := range []string{supervisor.KindStarted, supervisor.KindExited} {
				select {
				case e := <-events:
					if e.Kind != kind || e.Hash != statusHash {
						t.Errorf("event = %+v, want %s", e, kind)
					}
				default:
					t.Errorf("missing %s event", kind)
				}
			}
			if repeat := stopAnswer(t, s, `null`); repeat["running"] != false || repeat["wasRunning"] != false || repeat["terminatedBy"] != "already-stopped" {
				t.Errorf("repeat stop = %v", repeat)
			}
			if after := sup.Status(); after.Running || after.Restarting || !reflect.DeepEqual(after.LastExit, down.LastExit) {
				t.Errorf("repeat stop changed status: %+v", after)
			}
			select {
			case e := <-events:
				t.Errorf("repeat stop emitted an extra event: %+v", e)
			default:
			}
		})
	}
}
