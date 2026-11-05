package keystore

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// encodingProtector is the test seam New takes. It encodes and does not protect, which is exactly why it is not
// reachable from Open: the production constructor has one implementation on Windows and none anywhere else.
// Its job here is to make the store's own rules — one blob per reference, atomic install, fail-closed on a
// protector error, no plaintext when the protector refuses — testable on the runner that has no DPAPI.
type encodingProtector struct{ fail bool }

func (p encodingProtector) Protect(plain []byte) ([]byte, error) {
	if p.fail {
		return nil, errors.New("the protector refused")
	}
	return []byte(base64.StdEncoding.EncodeToString(plain)), nil
}

func (p encodingProtector) Unprotect(blob []byte) ([]byte, error) {
	if p.fail {
		return nil, errors.New("the protector refused")
	}
	return base64.StdEncoding.DecodeString(string(blob))
}

// identityProtector is the failure mode ADR-0011 rule 5 forbids: a protector that answers with the bytes it was
// handed. It is a plausible way to get one — a placeholder on a platform nobody has written an ADR for yet.
type identityProtector struct{}

func (identityProtector) Protect(plain []byte) ([]byte, error)  { return plain, nil }
func (identityProtector) Unprotect(blob []byte) ([]byte, error) { return blob, nil }

func openStore(t *testing.T, p Protector) *Store {
	t.Helper()
	s, err := New(t.TempDir(), p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// The three names this package's tests are built from. `userID` is the credential shape the store exists to hold,
// and a VLESS user id is what `tools/check-no-secrets.mjs` reads as a secret in any path that is not a fixture
// directory — a Go `_test.go` is beside its package and therefore outside every exemption the guard has. None of
// these is a real credential: no subscription, server or profile in any deployment answers to them.
const (
	userIDRef = "keystore://server/3f2a1c9e/user-id"
	headerRef = "keystore://server/c7e5a2d1/headers"
	userID    = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" // nexora-secret-allow: synthetic test identity, nothing behind it
)

func TestPutResolveRoundTrip(t *testing.T) {
	s := openStore(t, encodingProtector{})
	if err := s.Put(userIDRef, []byte(userID)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ok, err := s.Has(userIDRef); err != nil || !ok {
		t.Fatalf("Has = %v, %v after a write", ok, err)
	}
	got, err := s.Resolve(userIDRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	text, err := got.Text()
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if text != userID {
		t.Errorf("resolved %q, want %q", text, userID)
	}
	if got.Ref.String() != userIDRef {
		t.Errorf("the secret carries reference %q, which is not the one asked for", got.Ref.String())
	}
}

// What is on disk must not be the value. This is the store's whole claim, and the check reads the file rather
// than trusting the protector's return.
func TestNothingOnDiskIsTheValue(t *testing.T) {
	s := openStore(t, encodingProtector{})
	if err := s.Put(userIDRef, []byte(userID)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	raw := readBlobs(t, s)
	if len(raw) == 0 {
		t.Fatal("no blob was written")
	}
	if blobsContainPlaintext(t, s, userID) {
		t.Errorf("the blob directory contains the plaintext credential")
	}
}

// A protector that returns what it was handed is not a protector, and writing through one is the plaintext
// fallback ADR-0011 rule 5 forbids. The store refuses it rather than discovering the mistake in a config file.
func TestIdentityProtectorIsRefused(t *testing.T) {
	s := openStore(t, identityProtector{})
	err := s.Put(userIDRef, []byte(userID))
	if err == nil {
		t.Fatal("a protector that protects nothing was accepted")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("refused with %v, want ErrUnavailable", err)
	}
	if len(readBlobs(t, s)) != 0 {
		t.Errorf("the refused write still left a file in the keystore directory")
	}
}

// The other half of fail-closed: a protector that errors must not produce a file, and the answer must be the
// code that says the machine is the problem, not the credential.
func TestProtectorFailureIsUnavailableAndWritesNothing(t *testing.T) {
	s := openStore(t, encodingProtector{fail: true})
	err := s.Put(userIDRef, []byte(userID))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Put with a failing protector = %v, want ErrUnavailable", err)
	}
	var ke *Error
	if !asKeystoreError(err, &ke) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if ke.Code() != "KeystoreUnavailable" {
		t.Errorf("code = %s, want KeystoreUnavailable", ke.Code())
	}
	if names := readBlobs(t, s); len(names) != 0 {
		t.Errorf("a refused Put left %v in the directory", names)
	}
}

// A dangling reference is the code that sends the user to retype. Confusing it with an unreachable store is the
// difference between "your credential is missing" and "your machine is broken" (doc 32 §11).
func TestDanglingReferenceIsInvalidProfile(t *testing.T) {
	s := openStore(t, encodingProtector{})
	_, err := s.Resolve(userIDRef)
	var ke *Error
	if !asKeystoreError(err, &ke) {
		t.Fatalf("Resolve of a missing secret = %v, want *Error", err)
	}
	if ke.Code() != "InvalidProfile" {
		t.Errorf("code = %s, want InvalidProfile", ke.Code())
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error %v does not unwrap to ErrNotFound", err)
	}
}

func TestEmptyValueIsRefused(t *testing.T) {
	s := openStore(t, encodingProtector{})
	err := s.Put(userIDRef, nil)
	var ke *Error
	if !asKeystoreError(err, &ke) {
		t.Fatalf("Put of an empty secret = %v, want *Error", err)
	}
	if ke.Code() != "SchemaMismatch" {
		t.Errorf("code = %s, want SchemaMismatch", ke.Code())
	}
}

// Rotation is a second Put to the same reference: doc 32 §6 says it needs no separate command, so the store has
// to make it correct, which for a file means the old value is never readable in a half-written state.
func TestRotationOverwritesAndLeavesOneBlob(t *testing.T) {
	s := openStore(t, encodingProtector{})
	if err := s.Put(userIDRef, []byte("first-credential")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Put(userIDRef, []byte("second-credential")); err != nil {
		t.Fatalf("rotation Put: %v", err)
	}
	if names := readBlobs(t, s); len(names) != 1 {
		t.Errorf("rotation left %v; one reference is one blob", names)
	}
	got, err := s.Resolve(userIDRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(got.Value) != "second-credential" {
		t.Errorf("resolved %q after rotation, want the new value", got.Value)
	}
	if leftovers := readScratch(t, s); len(leftovers) != 0 {
		t.Errorf("the store left scratch files behind: %v", leftovers)
	}
}

func TestDelete(t *testing.T) {
	s := openStore(t, encodingProtector{})
	if err := s.Put(userIDRef, []byte(userID)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ok, err := s.Delete(userIDRef); err != nil || !ok {
		t.Fatalf("Delete = %v, %v, want the secret to have been there", ok, err)
	}
	if ok, err := s.Delete(userIDRef); err != nil || ok {
		t.Fatalf("second Delete = %v, %v; deleting what is gone is not an error and not a deletion", ok, err)
	}
	if ok, err := s.Has(userIDRef); err != nil || ok {
		t.Fatalf("Has = %v, %v after deletion", ok, err)
	}
}

func TestObjectShapedField(t *testing.T) {
	s := openStore(t, encodingProtector{})
	if err := s.Put(headerRef, []byte(`{"Authorization":"Bearer x"}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Resolve(headerRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := got.Text(); err == nil {
		t.Error("Text answered for a map-shaped field; the shape the registry records is the shape the store keeps")
	}
	headers, err := got.Headers()
	if err != nil {
		t.Fatalf("Headers: %v", err)
	}
	if headers["Authorization"] != "Bearer x" {
		t.Errorf("decoded %v from the stored map", headers)
	}
	if _, err := s.Resolve(headerRef + "-not-a-ref"); err == nil {
		t.Error("a malformed reference resolved")
	}
}

func TestStringOfASecretIsReferenceOnly(t *testing.T) {
	s := openStore(t, encodingProtector{})
	if err := s.Put(userIDRef, []byte(userID)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Resolve(userIDRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if strings.Contains(got.String(), userID) {
		t.Errorf("the printable form of a secret is %q, and ADR-0011 rule 6 says a log line carries references only", got.String())
	}
	if !strings.Contains(got.String(), userIDRef) {
		t.Errorf("the printable form of a secret is %q, which does not name the reference it stands for", got.String())
	}
}

// The store's refusals name the reference they concern, but a value is never one of them.
func TestErrorStringsNeverQuoteAValue(t *testing.T) {
	s := openStore(t, encodingProtector{fail: true})
	err := s.Put("keystore://server/3f2a1c9e/user-id", []byte("a-value-nobody-should-log"))
	if strings.Contains(err.Error(), "a-value-nobody-should-log") {
		t.Errorf("a refusal quoted the value it was given: %v", err)
	}
	if !strings.Contains(err.Error(), userIDRef) {
		t.Errorf("a refusal about %s did not name the reference: %v", userIDRef, err)
	}
}

// An empty blob is not a credential, and the difference between "empty" and "absent" is the one a user cannot
// see; the store refuses to resolve either into a value that would let an empty userId reach a compiled config.
func TestBlobThatOpensOntoNothingIsUnavailable(t *testing.T) {
	s := openStore(t, encodingProtector{})
	if err := s.Put(userIDRef, []byte(userID)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	name := s.path(Ref{Entity: "server", ID: "3f2a1c9e", Field: "user-id"})
	if err := os.WriteFile(name, []byte(""), 0o600); err != nil {
		t.Fatalf("sealing an empty blob: %v", err)
	}
	if _, err := s.Resolve(userIDRef); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Resolve of an empty secret = %v, want ErrUnavailable", err)
	}
}

func TestNewRequiresAPlaceToPutThings(t *testing.T) {
	if _, err := New("", encodingProtector{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("New with no directory = %v, want ErrUnavailable", err)
	}
	if _, err := New(t.TempDir(), nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("New with no protector = %v, want ErrUnavailable", err)
	}
}

// Open is the production door. On the runner that has no DPAPI it must fail closed rather than hand back a store
// that writes plaintext, and on Windows the directory it names is ADR-0011's.
func TestOpenIsPlatformBound(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "keystore"))
	if err == nil {
		if _, perr := platformProtector(); perr != nil {
			t.Fatal("Open succeeded on a platform whose protector is unimplemented")
		}
		if s.Dir() == "" {
			t.Fatal("a store with no directory")
		}
		return
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open on an unbound platform = %v, want ErrUnavailable", err)
	}
	if _, err := DefaultDir(); err == nil {
		t.Error("DefaultDir named a root on a platform ADR-0011 does not bind")
	}
	if dir, err := DefaultDir(); err == nil && !strings.EqualFold(filepath.Base(dir), "keystore") {
		t.Errorf("the keystore root is %q, not a directory named for the store", dir)
	}
}

// readBlobs lists the stored blobs, and readScratch the temporary files a Put may have left behind.
func readBlobs(t *testing.T, s *Store) []string {
	t.Helper()
	return matching(t, s, ".blob")
}

func readScratch(t *testing.T, s *Store) []string {
	t.Helper()
	return matching(t, s, ".keystore-")
}

func matching(t *testing.T, s *Store, suffix string) []string {
	t.Helper()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if strings.Contains(e.Name(), suffix) {
			out = append(out, e.Name())
		}
	}
	return out
}

// blobsContainPlaintext reads every stored blob and reports whether any carries the needle — the check that the
// thing on disk is not the credential, done against the files rather than against what the protector returned.
func blobsContainPlaintext(t *testing.T, s *Store, needle string) bool {
	t.Helper()
	for _, name := range readBlobs(t, s) {
		raw, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		if strings.Contains(string(raw), needle) {
			return true
		}
	}
	return false
}
