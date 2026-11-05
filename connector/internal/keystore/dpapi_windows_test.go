//go:build windows

package keystore

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The DPAPI half of this package is one syscall pair, and on Windows there is no reason to test anything else:
// the round trip below is the ADR's rule 1 measured rather than quoted. It is a real credential store on a real
// user profile, in a temporary directory — the ADR's directory itself is what DefaultDir names, and creating it
// under %LOCALAPPDATA% from a test would leave secrets-shaped files behind on every machine that runs the suite.
func TestDPAPIRoundTrip(t *testing.T) {
	p, err := platformProtector()
	if err != nil {
		t.Fatalf("platformProtector: %v", err)
	}
	blob, err := p.Protect([]byte(userID))
	if err != nil {
		t.Fatalf("CryptProtectData: %v", err)
	}
	if bytes.Equal(blob, []byte(userID)) {
		t.Fatal("crypt32 returned the credential unchanged")
	}
	if len(blob) == 0 {
		t.Fatal("crypt32 returned an empty blob")
	}
	back, err := p.Unprotect(blob)
	if err != nil {
		t.Fatalf("CryptUnprotectData: %v", err)
	}
	if string(back) != userID {
		t.Errorf("round trip gave %q, want %q", back, userID)
	}

	// A blob that is not DPAPI must fail rather than answer with something: the store treats any refusal as
	// KeystoreUnavailable, so a protector that guesses would turn a corrupt file into a wrong credential.
	if _, err := p.Unprotect([]byte("DPAPI")); err == nil {
		t.Error("CryptUnprotectData accepted bytes it did not produce")
	}
}

// Open is the production door: this is the store with the real protector, writing a file that is not the
// credential and reading it back.
func TestStoreOverDPAPI(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keystore")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if _, err := s.Delete(userIDRef); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	if err := s.Put(userIDRef, []byte(userID)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the store wrote %d files for one reference", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), userID) {
		t.Error("the DPAPI blob contains the credential it protects")
	}
	got, err := s.Resolve(userIDRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got.Value) != userID {
		t.Errorf("resolved %q through DPAPI, want %q", got.Value, userID)
	}
}

// ADR-0011 rule 1 is a path, and this is the only place the tree can check that the path this binary would use
// is the one the ADR names.
func TestDefaultDirIsADRsPath(t *testing.T) {
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		t.Skip("%LOCALAPPDATA% is unset on this host; ADR-0011 names no other root and DefaultDir refuses")
	}
	want := filepath.Join(base, "NEXORA", "keystore")
	if dir != want {
		t.Errorf("DefaultDir = %q, want %q", dir, want)
	}
}
