package supervisor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The applied-state row of `packages/schema/protocol/data-ownership.json` is the whole specification of this
// file: four fields, one writer, and a purge column that says "a corrupt file means recompile, not data loss".
// These tests are that row, read literally.
func TestStateRoundTripsTheFourFieldsTheRowNames(t *testing.T) {
	dir := t.TempDir()
	want := State{
		AppliedHash:     hash("a"),
		SelectedProfile: "prof-1",
		LastStartPID:    4242,
		LastExit:        &Exit{At: time.Unix(1730000000, 0).UTC(), Code: 7, Signal: "code-7"},
	}
	if err := WriteState(dir, want); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	got, err := ReadState(dir)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.AppliedHash != want.AppliedHash || got.SelectedProfile != want.SelectedProfile || got.LastStartPID != want.LastStartPID {
		t.Errorf("the record came back as %+v", got)
	}
	if got.LastExit == nil || got.LastExit.Code != want.LastExit.Code || !got.LastExit.At.Equal(want.LastExit.At) {
		t.Fatalf("the exit record is %+v, want the one written", got.LastExit)
	}

	info, err := os.Stat(filepath.Join(dir, StateFileName))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Logf("mode assertion skipped on %s: the ACL is inherited, not stored in the mode bits", runtime.GOOS)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("the applied-state record is readable by %v, want owner-only", info.Mode().Perm())
	}
}

// One rule about atomic replacement holds in this module rather than two: the record is renamed in, and a write
// that fails leaves no temp behind for the next run to mistake for a record.
func TestStateLeavesNoTempAndReplacesItself(t *testing.T) {
	dir := t.TempDir()
	for _, letter := range []string{"a", "b", "c"} {
		if err := WriteState(dir, State{AppliedHash: hash(letter), LastStartPID: 1}); err != nil {
			t.Fatalf("WriteState %s: %v", letter, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != StateFileName {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("after three writes the directory holds %v, want only %s", names, StateFileName)
	}
	got, err := ReadState(dir)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.AppliedHash != hash("c") {
		t.Errorf("the last write is not the one on disk: %q", got.AppliedHash)
	}
}

// An absent record is the first apply, and it is an empty answer rather than an error, which is what makes
// config.apply's previousConfigHash an empty string instead of a sentinel.
func TestMissingStateIsTheFirstApply(t *testing.T) {
	got, err := ReadState(t.TempDir())
	if err != nil {
		t.Fatalf("a directory with no record is not an error: %v", err)
	}
	if got.AppliedHash != "" || got.SelectedProfile != "" || got.LastStartPID != 0 || got.LastExit != nil || len(got.Held) != 0 {
		t.Errorf("the empty record came back as %+v", got)
	}
	if _, err := ReadState(""); err == nil || !strings.Contains(err.Error(), "no state directory") {
		t.Errorf("ReadState over no directory: %v", err)
	}
	if err := WriteState("", State{}); err == nil || !strings.Contains(err.Error(), "no state directory") {
		t.Errorf("WriteState over no directory: %v", err)
	}
}

// The row's purge column, tested: a file this package cannot read is one it may overwrite, and New treats it as
// a fresh run rather than refusing to start.
func TestCorruptStateIsReadAsFreshByNew(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, StateFileName)
	if err := os.WriteFile(name, []byte("{ not the JSON this package wrote"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(dir); err == nil || !strings.Contains(err.Error(), "not the JSON this package wrote") {
		t.Fatalf("ReadState hid the corruption: %v", err)
	}
	s, err := New(Config{Dir: dir, Launch: fakeLaunch(modeWait), Verify: alwaysProves, Death: Death{}})
	if err != nil {
		t.Fatalf("New refused a corrupt record instead of overwriting it: %v", err)
	}
	if st := s.Status(); st.Hash != "" || st.LastExit != nil {
		t.Errorf("the corrupt record was halfbelieved: %+v", st)
	}
	if _, err := s.Start(t.Context(), StartParams{ConfigPath: "a.json", ConfigHash: hash("b")}); err != nil {
		t.Fatalf("Start over a corrupt record: %v", err)
	}
	defer func() { _, _ = s.Stop(t.Context(), false) }()
	got, err := ReadState(dir)
	if err != nil {
		t.Fatalf("the overwrite is not readable: %v", err)
	}
	if got.AppliedHash != hash("b") {
		t.Errorf("the corrupt record survived: %+v", got)
	}
}
