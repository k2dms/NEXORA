package keystore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
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
	mu  sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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

	// Stage the protected bytes, then atomically install this one credential.
	tmpName, err := s.stageBlob(blob)
	if err != nil {
		return unavailable("put", r.String(), err)
	}
	defer os.Remove(tmpName)
	if err := os.Rename(tmpName, s.path(r)); err != nil {
		return unavailable("put", r.String(), err)
	}
	return nil
}

// stageBlob writes only encrypted bytes. Callers own the returned scratch file.
// It never locks: Put and PutBatch hold the store lock across the whole operation.
func (s *Store) stageBlob(blob []byte) (name string, err error) {
	tmp, err := os.CreateTemp(s.dir, ".keystore-*")
	if err != nil {
		return "", err
	}
	name = tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if _, err = tmp.Write(blob); err != nil {
		return name, err
	}
	if err = tmp.Sync(); err != nil {
		return name, err
	}
	if err = tmp.Close(); err != nil {
		return name, err
	}
	err = os.Chmod(name, 0o600)
	return name, err
}

// PutBatch protects and stages every credential before installing any target.
// A later rename failure restores the prior encrypted bytes without invoking the
// protector again, and removes only targets this batch created. This is rollback
// on reported errors, not crash-atomic multi-file replacement. Other operations
// on this Store cannot observe or interleave with the install/rollback sequence.
func (s *Store) PutBatch(values map[string][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	type entry struct {
		ref     Ref
		tmp     string
		old     []byte
		existed bool
	}
	keys := make([]string, 0, len(values))
	for ref := range values {
		keys = append(keys, ref)
	}
	sort.Strings(keys)
	entries := make([]entry, 0, len(keys))
	defer func() {
		for _, e := range entries {
			if e.tmp != "" {
				_ = os.Remove(e.tmp)
			}
		}
	}()
	// Validate all references and retain all prior encrypted blobs first.
	for _, key := range keys {
		r, err := Parse(key)
		if err != nil {
			return err
		}
		if len(values[key]) == 0 {
			return &Error{Op: "put-batch", Ref: r.String(), Cause: ErrEmptyValue}
		}
		old, err := os.ReadFile(s.path(r))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return unavailable("put-batch", r.String(), err)
		}
		entries = append(entries, entry{ref: r, old: old, existed: err == nil})
	}
	for i := range entries {
		e := &entries[i]
		value := values[e.ref.String()]
		blob, err := s.p.Protect(value)
		if err != nil {
			return unavailable("put-batch", e.ref.String(), err)
		}
		if len(blob) == 0 || bytes.Equal(blob, value) {
			return unavailable("put-batch", e.ref.String(), errors.New("the protector returned the value unchanged"))
		}
		e.tmp, err = s.stageBlob(blob)
		if err != nil {
			return unavailable("put-batch", e.ref.String(), err)
		}
	}
	for i, e := range entries {
		if err := os.Rename(e.tmp, s.path(e.ref)); err != nil {
			var failure error = unavailable("put-batch", e.ref.String(), err)
			for j := i - 1; j >= 0; j-- {
				prior := entries[j]
				var rollbackErr error
				if prior.existed {
					var tmp string
					tmp, rollbackErr = s.stageBlob(prior.old)
					if rollbackErr == nil {
						rollbackErr = os.Rename(tmp, s.path(prior.ref))
						_ = os.Remove(tmp)
					}
				} else {
					rollbackErr = os.Remove(s.path(prior.ref))
				}
				if rollbackErr != nil {
					failure = errors.Join(failure, unavailable("rollback", prior.ref.String(), rollbackErr))
				}
			}
			return failure
		}
	}
	return nil
}

// Delete removes one secret, reporting whether anything was there. It returns nil on a missing blob because
// deletion is expected to be retried against a store the user may already have emptied; keystore.purge's own
// accounting (doc 32 §7) reads the boolean rather than an error.
func (s *Store) Delete(ref string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
