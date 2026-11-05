package apply

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/compile"
)

func TestWriteInstallsOneOwnerOnlyFile(t *testing.T) {
	dir := t.TempDir()
	got, err := Write(dir, serverDoc(), fakeStore{userIDRef: userID})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got.Path != filepath.Join(dir, FileName) {
		t.Errorf("wrote to %s, want %s", got.Path, filepath.Join(dir, FileName))
	}
	if strings.Contains(string(got.Bytes), "keystore://") {
		t.Error("the file Core reads still carries a handle")
	}
	if !strings.Contains(string(got.Bytes), userID) {
		t.Error("the file Core reads does not carry the value doc 32 §13 says it must")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("the directory holds %v; a failed or finished write must leave exactly the applied config", names)
	}

	// 0600 is the portable half of "owner-only". On Windows the inherited profile descriptor governs and Go reports
	// 0666 for a file it did not mark read-only, so the assertion below would be measuring the wrong mechanism;
	// ADR-0011 rule 3 leaves that residual to a live host, and the ubuntu cell of the connector job runs this.
	if runtime.GOOS == "windows" {
		t.Logf("mode assertion skipped on %s: the ACL is inherited, not stored in the mode bits", runtime.GOOS)
	} else if info, err := os.Stat(got.Path); err != nil {
		t.Fatalf("Stat: %v", err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("the compiled config is readable beyond its owner: mode %v", info.Mode())
	}
}

func TestWrittenFileHashesToWhatTheWriterReported(t *testing.T) {
	dir := t.TempDir()
	got, err := Write(dir, serverDoc(), fakeStore{userIDRef: userID})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	onDisk, err := AppliedHash(dir)
	if err != nil {
		t.Fatalf("AppliedHash: %v", err)
	}
	if onDisk != got.ValueHash {
		t.Errorf("the file on disk hashes to %s and the writer reported %s; doc 25 §11's drift check compares these", onDisk, got.ValueHash)
	}
	if got.PreviousHash != "" {
		t.Errorf("first write reported previousConfigHash %q; the contract's pattern means an empty string as 'nothing was applied'", got.PreviousHash)
	}
}

func TestHashCoveredByODEC36IsReportedNotChosen(t *testing.T) {
	dir := t.TempDir()
	got, err := Write(dir, serverDoc(), fakeStore{userIDRef: userID})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got.HandleHash == got.ValueHash {
		t.Fatal("the handle-bearing and materialized byte strings hashed the same, so one of the two is not what it claims")
	}
	if got.HandleHash == "" || got.ValueHash == "" {
		t.Fatal("Write reported only one of the two hashes ODEC-36 chooses between")
	}
}

func TestAFailedApplyLeavesThePreviousConfigOnDisk(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, serverDoc(), fakeStore{userIDRef: userID}); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	firstHash, err := AppliedHash(dir)
	if err != nil {
		t.Fatalf("AppliedHash: %v", err)
	}

	// The second document names a secret nothing stored: the failure lands after the handle-bearing emit and
	// before any byte is written, which is doc 27 §8's "a failure at any stage leaves the previous config applied".
	broken := compile.Document{"outbounds": []any{map[string]any{"settings": map[string]any{"id": "keystore://server/deadbeef/user-id"}}}}
	if _, err := Write(dir, broken, fakeStore{userIDRef: userID}); err == nil {
		t.Fatal("a failing apply reported success")
	} else if !strings.Contains(err.Error(), "outbounds[0].settings.id") {
		t.Errorf("the failure did not name its position: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("ReadFile after the failed apply: %v", err)
	}
	if string(after) != string(before) {
		t.Error("a failed apply changed the config Core reads")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("the failed apply left %d files behind", len(entries))
	}
	if hash, err := AppliedHash(dir); err != nil || hash != firstHash {
		t.Errorf("previousConfigHash after a failed apply = %q, %v, want %q unchanged", hash, err, firstHash)
	}
}

func TestWriteCreatesTheDirectoryOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nexora", "config")
	if _, err := Write(dir, serverDoc(), fakeStore{userIDRef: userID}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Logf("mode assertion skipped on %s: the directory inherits the profile descriptor", runtime.GOOS)
		return
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("the directory holding the plaintext config is reachable beyond its owner: mode %v", info.Mode())
	}
}

func TestAppliedHashOfAnEmptyDirectory(t *testing.T) {
	got, err := AppliedHash(t.TempDir())
	if err != nil {
		t.Fatalf("AppliedHash: %v", err)
	}
	if got != "" {
		t.Errorf("AppliedHash = %q for a directory with no config, want the empty string", got)
	}
}

func TestWriteRefusesNoDirectory(t *testing.T) {
	if _, err := Write("", serverDoc(), fakeStore{userIDRef: userID}); err == nil {
		t.Fatal("Write accepted an empty directory and would have written into the current one")
	}
}
