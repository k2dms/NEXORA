package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

func snapshotFrame(args string) *envelope.Frame {
	m := "config.snapshot"
	return &envelope.Frame{V: 1, ID: "snap-1", Type: envelope.TypeRequest, Method: &m, Args: json.RawMessage(args)}
}

func writeSnapshotState(t *testing.T, dir, hash string, held map[string]int) {
	t.Helper()
	st := supervisor.State{AppliedHash: hash, Held: held}
	if err := supervisor.WriteState(dir, st); err != nil {
		t.Fatalf("state write: %v", err)
	}
}

func TestConfigSnapshotNoStateRefuses(t *testing.T) {
	dir := t.TempDir()
	s, err := newConfigSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	ans, ref := s.Handle(context.Background(), snapshotFrame(""))
	if ref == nil || ref.Code != "CoreNotRunning" || ans != nil {
		t.Fatalf("want CoreNotRunning refusal, got answer=%v refusal=%+v", ans, ref)
	}
	if !strings.Contains(ref.Message, "no applied config is on record") {
		t.Fatalf("refusal does not name the empty first-apply reading: %s", ref.Message)
	}
}

func TestConfigSnapshotNoStateDirCannotBuild(t *testing.T) {
	if _, err := newConfigSnapshot(""); err == nil || !strings.Contains(err.Error(), "no state directory") {
		t.Fatalf("want state-directory refusal, got %v", err)
	}
}

func TestConfigSnapshotAnswersHashAndTags(t *testing.T) {
	dir := t.TempDir()
	writeSnapshotState(t, dir, strings.Repeat("ab", 32), map[string]int{"srv-3f2a1c9e": 20100, "srv-0d2c4e6f": 20101})
	s, err := newConfigSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, ref := s.Handle(context.Background(), snapshotFrame(""))
	if ref != nil {
		t.Fatalf("refusal: %+v", ref)
	}
	b, _ := json.Marshal(raw)
	var ans struct {
		AppliedConfigHash    string   `json:"appliedConfigHash"`
		MatchesExtensionHash *bool    `json:"matchesExtensionHash"`
		Drift                bool     `json:"drift"`
		ServerTags           []string `json:"serverTags"`
		MaskedConfig         *any     `json:"maskedConfig"`
	}
	if err := json.Unmarshal(b, &ans); err != nil {
		t.Fatal(err)
	}
	if ans.AppliedConfigHash != strings.Repeat("ab", 32) {
		t.Fatalf("hash: %s", ans.AppliedConfigHash)
	}
	if ans.MatchesExtensionHash != nil {
		t.Fatalf("matchesExtensionHash must be null without an expectation, got %v", *ans.MatchesExtensionHash)
	}
	if ans.Drift {
		t.Fatal("drift must be false without an expectation: there is nothing the extension believes to compare")
	}
	if strings.Join(ans.ServerTags, ",") != "srv-0d2c4e6f,srv-3f2a1c9e" {
		t.Fatalf("tags not sorted from the Held table: %v", ans.ServerTags)
	}
	if ans.MaskedConfig != nil {
		t.Fatal("maskedConfig must be null when includeMaskedConfig is absent")
	}
}

func TestConfigSnapshotExpectationMatchesAndDrifts(t *testing.T) {
	dir := t.TempDir()
	applied := strings.Repeat("cd", 32)
	writeSnapshotState(t, dir, applied, nil)
	s, err := newConfigSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	same := `{"expectation":"` + applied + `"}`
	raw, ref := s.Handle(context.Background(), snapshotFrame(same))
	if ref != nil {
		t.Fatalf("refusal: %+v", ref)
	}
	b, _ := json.Marshal(raw)
	var ans struct {
		MatchesExtensionHash *bool `json:"matchesExtensionHash"`
		Drift                bool  `json:"drift"`
	}
	json.Unmarshal(b, &ans)
	if ans.MatchesExtensionHash == nil || !*ans.MatchesExtensionHash || ans.Drift {
		t.Fatalf("equal expectation must answer matches=true drift=false, got %+v", ans)
	}
	other := `{"expectation":"` + strings.Repeat("ef", 32) + `"}`
	raw, ref = s.Handle(context.Background(), snapshotFrame(other))
	if ref != nil {
		t.Fatalf("refusal: %+v", ref)
	}
	b, _ = json.Marshal(raw)
	json.Unmarshal(b, &ans)
	if ans.MatchesExtensionHash == nil || *ans.MatchesExtensionHash || !ans.Drift {
		t.Fatalf("differing expectation must answer matches=false drift=true, got %+v", ans)
	}
}

func TestConfigSnapshotRefusesBadArgs(t *testing.T) {
	dir := t.TempDir()
	writeSnapshotState(t, dir, strings.Repeat("ab", 32), nil)
	s, err := newConfigSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args string
		want string
	}{
		{`{"expectation":"XYZ"}`, "lowercase sha256"},
		{`{"expectation":42}`, "not a string"},
		{`{"includeMaskedConfig":"yes"}`, "not a boolean"},
		{`{"maxBytes":"big"}`, "not an integer"},
		{`{"nope":1}`, "does not declare"},
	}
	for _, c := range cases {
		_, ref := s.Handle(context.Background(), snapshotFrame(c.args))
		if ref == nil || ref.Code != "Internal" || !strings.Contains(ref.Message, c.want) {
			t.Fatalf("args %s: want refusal naming %q, got %+v", c.args, c.want, ref)
		}
	}
}

func TestConfigSnapshotRefusesMaskedConfig(t *testing.T) {
	dir := t.TempDir()
	writeSnapshotState(t, dir, strings.Repeat("ab", 32), nil)
	s, err := newConfigSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, ref := s.Handle(context.Background(), snapshotFrame(`{"includeMaskedConfig":true}`))
	if ref == nil || ref.Code != "SecretRedacted" {
		t.Fatalf("want SecretRedacted refusal, got %+v", ref)
	}
	if !strings.Contains(ref.Message, "value enumeration") {
		t.Fatalf("refusal does not name the missing enumeration: %s", ref.Message)
	}
	// The refusal must not have disturbed the state record the next plain call reads.
	raw, ref := s.Handle(context.Background(), snapshotFrame(""))
	if ref != nil {
		t.Fatalf("post-refusal read: %+v", ref)
	}
	b, _ := json.Marshal(raw)
	if !strings.Contains(string(b), strings.Repeat("ab", 32)) {
		t.Fatalf("hash lost after a refused masked ask: %s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, supervisor.StateFileName)); err != nil {
		t.Fatalf("state file: %v", err)
	}
}
