package main

import (
	"context"
	"encoding/json"
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

const statusHash = "733d971ecd9fbd405687109213c458ada91e337e493004f463b16783c559f3c7"

func statusSurface(t *testing.T, st supervisor.Status, installed bool) (*coreStatus, string) {
	t.Helper()
	dir := t.TempDir()
	if installed {
		payload, err := json.Marshal(map[string]string{
			"tag":         "v24.11.11",
			"pinsVersion": "1",
			"sha256":      strings.Repeat("0", 64),
			"verifiedAt":  "2026-11-11T00:00:00Z",
			"path":        filepath.Join(dir, "xray"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "receipt.json"), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := newCoreStatus(&fakeCore{status: st}, dir)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 11, 10, 10, 43, 0, time.UTC) }
	return s, dir
}

func statusFrame(t *testing.T, args string) *envelope.Frame {
	t.Helper()
	m := "core.status"
	return &envelope.Frame{V: 1, ID: "status-1", Type: envelope.TypeRequest, Method: &m, Args: json.RawMessage(args)}
}

func statusAnswer(t *testing.T, s *coreStatus, args string) map[string]any {
	t.Helper()
	ans, refusal := s.Handle(context.Background(), statusFrame(t, args))
	if refusal != nil {
		t.Fatalf("core.status refused a legal call: %s %s", refusal.Code, refusal.Message)
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

func TestCoreStatusAnswersTheProcessTheSupervisorHolds(t *testing.T) {
	started := time.Date(2026, 11, 11, 10, 10, 1, 0, time.UTC)
	s, _ := statusSurface(t, supervisor.Status{
		Running:   true,
		PID:       8144,
		Hash:      statusHash,
		StartedAt: started,
		Contained: true,
	}, true)

	got := statusAnswer(t, s, `{}`)
	if got["running"] != true || got["pid"] != float64(8144) || got["contained"] != true {
		t.Errorf("running/pid/contained = %v %v %v", got["running"], got["pid"], got["contained"])
	}
	if got["configHash"] != statusHash {
		t.Errorf("configHash = %v, want the hash the process was handed", got["configHash"])
	}
	if got["coreVersion"] != "24.11.11" {
		t.Errorf("coreVersion = %v, want the version the install receipt pins", got["coreVersion"])
	}
	if got["startedAt"] != "2026-11-11T10:10:01Z" {
		t.Errorf("startedAt = %v", got["startedAt"])
	}
	if got["uptimeSeconds"] != float64(42) {
		t.Errorf("uptimeSeconds = %v, want 42", got["uptimeSeconds"])
	}
	if got["lastExit"] != nil {
		t.Errorf("lastExit = %v, want null for a process that has not died", got["lastExit"])
	}
}

func TestCoreStatusReportsOnlyLiveSingleProfileAndInbounds(t *testing.T) {
	inbounds := []supervisor.Inbound{
		{Listen: "127.0.0.1", Port: shapeAPort, Tag: firstPortTag},
		{Listen: "::1", Port: shapeAPort + 1, Tag: "block"},
	}
	for _, tc := range []struct {
		name       string
		running    bool
		restarting bool
		profile    string
		selected   any
	}{
		{"running single UUID", true, false, profileID, profileID},
		{"running no selection", true, false, "", nil},
		{"running multiple UUIDs", true, false, profileID + "," + importProfileID, nil},
		{"running uppercase UUID", true, false, strings.ToUpper(profileID), nil},
		{"running malformed selection", true, false, "not-a-profile", nil},
		{"stopped retained selection", false, false, profileID, nil},
		{"restarting retained selection", false, true, profileID, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := statusSurface(t, supervisor.Status{
				Running: tc.running, Restarting: tc.restarting, Hash: statusHash,
				ProfileID: tc.profile, Inbounds: inbounds,
			}, false)
			got := statusAnswer(t, s, `{}`)
			if selected, present := got["selectedProfileId"]; !present || selected != tc.selected {
				t.Errorf("selectedProfileId = %v (present=%t), want %v", selected, present, tc.selected)
			}
			want := []any{}
			if tc.running {
				for _, in := range inbounds {
					want = append(want, map[string]any{"listen": in.Listen, "port": float64(in.Port), "tag": in.Tag})
				}
			}
			if !reflect.DeepEqual(got["inboundPorts"], want) || got["running"] != tc.running {
				t.Errorf("running/inboundPorts = %v/%v, want %t/%v", got["running"], got["inboundPorts"], tc.running, want)
			}
		})
	}
}

func TestCoreStatusAnswersWhenCoreIsDownAndKeepsWhatOutlivedIt(t *testing.T) {
	died := time.Date(2026, 11, 11, 10, 40, 0, 0, time.UTC)
	s, _ := statusSurface(t, supervisor.Status{
		Hash: statusHash,
		LastExit: &supervisor.Exit{
			At:        died,
			Code:      137,
			Signalled: true,
			Signal:    "SIGKILL",
		},
	}, true)

	got := statusAnswer(t, s, `{}`)
	if got["running"] != false {
		t.Errorf("running = %v, want false", got["running"])
	}
	for _, name := range []string{"pid", "startedAt", "uptimeSeconds", "contained"} {
		if got[name] != nil {
			t.Errorf("%s = %v, want null: there is no process to measure", name, got[name])
		}
	}
	if got["configHash"] != statusHash {
		t.Errorf("configHash = %v, want the applied-state record's hash", got["configHash"])
	}
	exit, ok := got["lastExit"].(map[string]any)
	if !ok {
		t.Fatalf("lastExit = %v, want the retained exit", got["lastExit"])
	}
	if exit["at"] != "2026-11-11T10:40:00Z" || exit["exitCode"] != float64(137) || exit["signal"] != "SIGKILL" {
		t.Errorf("lastExit = %v", exit)
	}
	if _, present := exit["message"]; present {
		t.Error("lastExit carries a message; internal/supervisor keeps none because a dying Core's stderr is credential material")
	}
}

func TestCoreStatusVersionIsNullWhenNoReceiptNamesOne(t *testing.T) {
	s, _ := statusSurface(t, supervisor.Status{}, false)
	got := statusAnswer(t, s, `{}`)
	if got["coreVersion"] != nil {
		t.Errorf("coreVersion = %v, want null with no install receipt", got["coreVersion"])
	}
	if got["running"] != false || got["configHash"] != nil {
		t.Errorf("a first-run answer is %v, want running false and no hash on record", got)
	}
}

func TestCoreStatusArgsAreTheClosedEmptyObject(t *testing.T) {
	s, _ := statusSurface(t, supervisor.Status{}, true)
	for _, args := range []string{``, `null`, `{}`} {
		if _, refusal := s.Handle(context.Background(), statusFrame(t, args)); refusal != nil {
			t.Errorf("args %q was refused: %s %s", args, refusal.Code, refusal.Message)
		}
	}
	for _, args := range []string{`{"verbose":true}`, `[]`, `"now"`} {
		_, refusal := s.Handle(context.Background(), statusFrame(t, args))
		if refusal == nil {
			t.Fatalf("args %q was accepted; core.status#/args is closed and empty", args)
		}
		if refusal.Code != "Internal" {
			t.Errorf("args %q refused with %s, want Internal", args, refusal.Code)
		}
		if !strings.Contains(refusal.Message, "core.status") {
			t.Errorf("refusal message %q does not name the command", refusal.Message)
		}
	}
}

func TestCoreStatusAnswersOnlyFieldsTheCommandDeclares(t *testing.T) {
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "packages", "schema", "protocol", "commands", "core.status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Result struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	s, _ := statusSurface(t, supervisor.Status{Running: true, PID: 1, Hash: statusHash, StartedAt: time.Now()}, true)
	got := statusAnswer(t, s, `{}`)
	var names []string
	for name := range got {
		if _, declared := doc.Result.Properties[name]; !declared {
			t.Errorf("the answer carries %q, which core.status#/result does not declare", name)
		}
		names = append(names, name)
	}
	for _, name := range doc.Result.Required {
		if !slices.Contains(names, name) {
			t.Errorf("the answer omits %q, which core.status#/result requires", name)
		}
	}
	for _, absent := range []string{"activeServerTag", "stats"} {
		if _, present := got[absent]; present {
			t.Errorf("%s is answered without a selection or counters producer", absent)
		}
	}
}

func TestCoreStatusRealSupervisorLifecyclePersistsAfterStop(t *testing.T) {
	d := newDirs(t)
	record := filepath.Join(d.config, "handed")
	cfg := supervisor.Config{
		Dir:    d.state,
		Launch: agentLaunch(modeHold, record),
		Verify: func(string) error { return nil },
		Death:  supervisor.Death{},
	}
	sup, err := supervisor.New(cfg)
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
	s, err := newCoreStatus(sup, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(d.config, "core.json")
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
	live := sup.Status()
	if !live.Running || live.PID <= 0 || live.PID != started.PID || live.Hash != statusHash || live.StartedAt.IsZero() {
		t.Fatalf("unexpected live supervisor status: %+v", live)
	}
	s.now = func() time.Time { return live.StartedAt.Add(42 * time.Second) }
	got := statusAnswer(t, s, `{}`)
	want := map[string]any{
		"running": true, "pid": float64(live.PID), "configHash": statusHash,
		"contained": live.Contained, "startedAt": live.StartedAt.UTC().Format(time.RFC3339Nano),
		"uptimeSeconds": float64(42), "coreVersion": nil, "lastExit": nil,
		"selectedProfileId": nil, "inboundPorts": []any{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("live answer = %v, want %v", got, want)
	}
	persisted, err := supervisor.ReadState(d.state)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.AppliedHash != statusHash || persisted.LastStartPID != live.PID {
		t.Errorf("running state = %+v", persisted)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	stopped, err := sup.Stop(stopCtx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.WasRunning || stopped.TerminatedBy != supervisor.TerminatedByRequest {
		t.Errorf("Stop = %+v", stopped)
	}
	down := sup.Status()
	if down.Running || down.Hash != statusHash || down.LastExit == nil {
		t.Fatalf("stopped supervisor status = %+v", down)
	}
	exit := map[string]any{
		"at": down.LastExit.At.UTC().Format(time.RFC3339Nano), "exitCode": float64(stopped.ExitCode),
	}
	if down.LastExit.Signal != "" {
		exit["signal"] = down.LastExit.Signal
	}
	want = map[string]any{
		"running": false, "pid": nil, "configHash": statusHash, "contained": nil,
		"startedAt": nil, "uptimeSeconds": nil, "coreVersion": nil, "lastExit": exit,
		"selectedProfileId": nil, "inboundPorts": []any{},
	}
	if got := statusAnswer(t, s, `{}`); !reflect.DeepEqual(got, want) {
		t.Errorf("stopped answer = %v, want %v", got, want)
	}
	persisted, err = supervisor.ReadState(d.state)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.AppliedHash != statusHash || persisted.LastStartPID != live.PID || persisted.LastExit == nil {
		t.Fatalf("stopped state = %+v", persisted)
	}
	if !persisted.LastExit.At.Equal(down.LastExit.At) || persisted.LastExit.Code != stopped.ExitCode || persisted.LastExit.Signal != down.LastExit.Signal {
		t.Errorf("persisted exit = %+v, want %+v", persisted.LastExit, down.LastExit)
	}
	reopened, err := supervisor.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reopenedStatus, err := newCoreStatus(reopened, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := statusAnswer(t, reopenedStatus, `{}`); got["running"] != false || got["configHash"] != statusHash ||
		got["pid"] != nil || got["startedAt"] != nil || got["uptimeSeconds"] != nil || got["contained"] != nil {
		t.Errorf("reopened answer = %v", got)
	}
	if ex := reopened.Status().LastExit; ex == nil || !ex.At.Equal(down.LastExit.At) || ex.Code != stopped.ExitCode {
		t.Errorf("reopened exit = %+v, want %+v", ex, down.LastExit)
	}
}

func TestCoreStatusServiceWiringRequiresHelloAndLoadsPersistedState(t *testing.T) {
	cfg := jsonlConfig(t, true)
	_, cfg.coreDir = statusSurface(t, supervisor.Status{}, true)
	died := time.Date(2026, 11, 11, 10, 40, 0, 123, time.UTC)
	if err := supervisor.WriteState(cfg.stateDir, supervisor.State{
		AppliedHash: statusHash, LastStartPID: 8144,
		LastExit: &supervisor.Exit{At: died, Code: 17, Signal: "SIGKILL"},
	}); err != nil {
		t.Fatal(err)
	}
	s, err := newService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f := mustParse(t, requestLine(t, "req-status-before", "core.status", `{}`))
	res, err := s.respond(f)
	if err != nil {
		t.Fatal(err)
	}
	checkRefusal(t, res, f.ID, "HandshakeFailed")
	helloFrame := mustParse(t, requestLine(t, "req-status-hello", "connector.hello", recordedHelloArgs))
	res, err = s.respond(helloFrame)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK == nil || !*res.OK || !s.sess.Negotiated() {
		t.Fatalf("hello did not open the gate: %+v", res)
	}
	f = mustParse(t, requestLine(t, "req-status-after", "core.status", `{}`))
	res, err = s.respond(f)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK == nil || !*res.OK || res.Error != nil || res.InReplyTo == nil || *res.InReplyTo != f.ID {
		t.Fatalf("core.status response = %+v, want ok:true", res)
	}
	var got map[string]any
	if err := json.Unmarshal(res.Result, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"running": false, "pid": nil, "configHash": statusHash, "contained": nil,
		"startedAt": nil, "uptimeSeconds": nil, "coreVersion": "24.11.11",
		"selectedProfileId": nil, "inboundPorts": []any{},
		"lastExit": map[string]any{"at": died.Format(time.RFC3339Nano), "exitCode": float64(17), "signal": "SIGKILL"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wired answer = %v, want %v", got, want)
	}
}

func TestCoreStatusUptimeClampsWhenClockMovesBackward(t *testing.T) {
	started := time.Date(2026, 11, 11, 10, 10, 1, 0, time.UTC)
	s, _ := statusSurface(t, supervisor.Status{
		Running: true, PID: 8144, Hash: statusHash, StartedAt: started, Contained: true,
	}, true)
	s.now = func() time.Time { return started.Add(-42 * time.Second) }
	got := statusAnswer(t, s, `{}`)
	if got["uptimeSeconds"] != float64(0) || got["running"] != true ||
		got["startedAt"] != started.Format(time.RFC3339Nano) || got["configHash"] != statusHash {
		t.Errorf("backward-clock answer = %v, want zero uptime and unchanged process facts", got)
	}
}

func TestCoreStatusMalformedReceiptLeavesVersionNullAndProcessFactsIntact(t *testing.T) {
	valid := `{"tag":"v24.11.11","pinsVersion":"1","sha256":"` + strings.Repeat("0", 64) +
		`","verifiedAt":"2026-11-11T00:00:00Z","path":"unused-core"}`
	cases := []struct{ name, receipt string }{
		{"truncated JSON", `{"tag":"v24.11.11"`},
		{"null receipt", `null`},
		{"missing fields", `{}`},
		{"null tag", strings.Replace(valid, `"tag":"v24.11.11"`, `"tag":null`, 1)},
		{"non-string tag", strings.Replace(valid, `"tag":"v24.11.11"`, `"tag":24`, 1)},
		{"invalid hash", strings.Replace(valid, strings.Repeat("0", 64), "bad-hash", 1)},
		{"invalid timestamp", strings.Replace(valid, "2026-11-11T00:00:00Z", "not-a-time", 1)},
		{"empty trimmed version", strings.Replace(valid, `"tag":"v24.11.11"`, `"tag":"v"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "receipt.json"), []byte(tc.receipt), 0o600); err != nil {
				t.Fatal(err)
			}
			started := time.Date(2026, 11, 11, 10, 10, 1, 0, time.UTC)
			s, err := newCoreStatus(&fakeCore{status: supervisor.Status{
				Running: true, PID: 8144, Hash: statusHash, StartedAt: started, Contained: true,
			}}, dir)
			if err != nil {
				t.Fatalf("receipt prevented status construction: %v", err)
			}
			s.now = func() time.Time { return started.Add(42 * time.Second) }
			got := statusAnswer(t, s, `{}`)
			want := map[string]any{
				"running": true, "pid": float64(8144), "configHash": statusHash, "contained": true,
				"startedAt": started.Format(time.RFC3339Nano), "uptimeSeconds": float64(42),
				"coreVersion": nil, "lastExit": nil,
				"selectedProfileId": nil, "inboundPorts": []any{},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("answer = %v, want %v", got, want)
			}
		})
	}
}
