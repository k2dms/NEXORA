package keystore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// Protector is the OS seam: the thing that makes a value unreadable to a process that is not this user in this
// session. Only one implementation is bound by an ADR today (ADR-0011, Windows DPAPI, in platform_windows.go),
// and the constructor that takes a Protector explicitly exists so the store's own logic is testable on a runner
// that has no DPAPI — not so a production caller can pick a protector that protects nothing.
type Protector interface {
	Protect(plain []byte) ([]byte, error)
	Unprotect(blob []byte) ([]byte, error)
}

// Store holds one protected blob per secret in one directory (registry `atRest.blobs`, ADR-0011 rule 1).
type Store struct {
	dir string
	p   Protector
}

// DefaultDir is ADR-0011's path: `%LOCALAPPDATA%\NEXORA\keystore`. It returns an error on a platform no ADR has
// bound, which is the same answer Open gives and for the same reason — a directory that exists is not a keystore.
func DefaultDir() (string, error) { return platformKeystoreDir() }

// Open prepares the store at dir using this platform's protector. It is the only constructor a production caller
// has, and on Windows that protector is DPAPI with user scope and no application entropy.
func Open(dir string) (*Store, error) {
	p, err := platformProtector()
	if err != nil {
		return nil, unavailable("open", "", err)
	}
	return New(dir, p)
}

// New builds a store with an explicit protector. The parameter is required rather than defaulted: a store that
// writes plaintext is a store somebody had to ask for, and the asking stays visible at the call site.
func New(dir string, p Protector) (*Store, error) {
	if dir == "" {
		return nil, &Error{Op: "open", Cause: ErrUnavailable}
	}
	if p == nil {
		return nil, &Error{Op: "open", Cause: ErrUnavailable}
	}
	// ADR-0011 rule 1: the Connector creates the directory, and it inherits the user profile's ACL — no explicit
	// ACL work here, which is also what keeps this call from needing elevation. The 0700 is the portable half of
	// that rule; on Windows the inherited descriptor is what actually governs access.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, unavailable("open", "", err)
	}
	return &Store{dir: dir, p: p}, nil
}

// Dir reports where the blobs live. It is a path, never a secret.
func (s *Store) Dir() string { return s.dir }

// Resolve is the reason this package exists: the one point where a reference becomes a value again, per
// doc 32 §4's first two rows. A missing blob is InvalidProfile (the user retypes), an OS refusal is
// KeystoreUnavailable (nobody who types helps), and the two never share an answer.
func (s *Store) Resolve(ref string) (Secret, error) {
	r, err := Parse(ref)
	if err != nil {
		return Secret{}, err
	}
	blob, err := os.ReadFile(s.path(r))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Secret{}, &Error{Op: "resolve", Ref: r.String(), Cause: ErrNotFound}
		}
		return Secret{}, unavailable("resolve", r.String(), err)
	}
	value, err := s.p.Unprotect(blob)
	if err != nil {
		return Secret{}, unavailable("resolve", r.String(), err)
	}
	if len(value) == 0 {
		// A blob that opens onto nothing is not a credential; handing back an empty value would let the compiler
		// write a config with an empty userId, which is the failure doc 32 §12's first-run ordering exists to
		// prevent.
		return Secret{}, unavailable("resolve", r.String(), errors.New("the stored secret is empty"))
	}
	return Secret{Ref: r, Value: value}, nil
}

// Put writes a secret, minting nothing: the reference is the one the ingest door already handed back
// (doc 32 §6). Re-sending the same reference overwrites, which is rotation and needs no separate call.
func (s *Store) Put(ref string, value []byte) error {
	r, err := Parse(ref)
	if err != nil {
		return err
	}
	if len(value) == 0 {
		return &Error{Op: "put", Ref: r.String(), Cause: ErrEmptyValue}
	}
	blob, err := s.p.Protect(value)
	if err != nil {
		return unavailable("put", r.String(), err)
	}
	if len(blob) == 0 || bytes.Equal(blob, value) {
		// ADR-0011 rule 5 forbids the silent plaintext fallback, and the way a protector gets into that state is
		// by returning what it was handed. Refusing here is the only check that does not need to know the format.
		return unavailable("put", r.String(), errors.New("the protector returned the value unchanged"))
	}

	// Written to a temporary file and renamed, because a half-written credential is the worst state a secret store
	// can be in: it resolves, it is wrong, and nothing about the file says so.
	tmp, err := os.CreateTemp(s.dir, ".keystore-*")
	if err != nil {
		return unavailable("put", r.String(), err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup of the scratch file; the rename below takes it out of the error path when it works.
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return unavailable("put", r.String(), err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return unavailable("put", r.String(), err)
	}
	if err := tmp.Close(); err != nil {
		return unavailable("put", r.String(), err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return unavailable("put", r.String(), err)
	}
	if err := os.Rename(tmpName, s.path(r)); err != nil {
		return unavailable("put", r.String(), err)
	}
	return nil
}

// Delete removes one secret, reporting whether anything was there. It returns nil on a missing blob because
// deletion is expected to be retried against a store the user may already have emptied; keystore.purge's own
// accounting (doc 32 §7) reads the boolean rather than an error.
func (s *Store) Delete(ref string) (bool, error) {
	r, err := Parse(ref)
	if err != nil {
		return false, err
	}
	switch err := os.Remove(s.path(r)); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, unavailable("delete", r.String(), err)
	}
}

// Has reports whether a reference resolves without carrying the value. It is what `profile.validate`-shaped
// questions and the purge scopes use, and it is the only method here that never touches a plaintext.
func (s *Store) Has(ref string) (bool, error) {
	r, err := Parse(ref)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(s.path(r))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, unavailable("has", r.String(), err)
	}
}

// path joins the blob name under the store directory. The name comes from a parsed reference, whose grammar
// admits no path separator and no `..`, so this join cannot leave the directory the store was opened at.
func (s *Store) path(r Ref) string { return filepath.Join(s.dir, r.blobName()) }

// Secret is a resolved reference. Its String is reference-only so a `%v` in a log line cannot print a credential
// (ADR-0011 rule 6); the bytes are reached through the accessors, which refuse the wrong shape.
type Secret struct {
	Ref   Ref
	Value []byte
}

func (s Secret) String() string {
	return "secret " + s.Ref.String() + " (" + strconv.Itoa(len(s.Value)) + " B)"
}

// Text returns the value for a string-shaped field and refuses the object-shaped one, because a header map
// printed as one string is a secret with its structure hidden.
func (s Secret) Text() (string, error) {
	if s.Ref.IsObject() {
		return "", &Error{Op: "text", Ref: s.Ref.String(), Cause: ErrMalformedReference}
	}
	return string(s.Value), nil
}

// Headers decodes the object-shaped field into the map the registry says it is. ODEC-45: apply materializes this
// map into the compiled config's headers block; storing and reading it is the ingest door's job (doc 32 §6).
func (s Secret) Headers() (map[string]string, error) {
	if !s.Ref.IsObject() {
		return nil, &Error{Op: "headers", Ref: s.Ref.String(), Cause: ErrMalformedReference}
	}
	var out map[string]string
	if err := json.Unmarshal(s.Value, &out); err != nil {
		return nil, &Error{Op: "headers", Ref: s.Ref.String(), Cause: ErrMalformedReference}
	}
	if out == nil {
		return nil, &Error{Op: "headers", Ref: s.Ref.String(), Cause: ErrMalformedReference}
	}
	return out, nil
}
