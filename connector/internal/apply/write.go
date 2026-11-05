package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/k2dms/nexora/connector/internal/compile"
)

// FileName is the one compiled config. docs/architecture/34-data-split.md §6's row for the file Core reads says
// "one file", and doc 27 §8's rename-over-the-previous is only expressible against a stable name; §5's granularity
// row says "one file per applied hash", which the rename satisfies by keeping exactly the applied hash on disk.
// The directory is the caller's to choose — no ADR names a path for this file the way ADR-0011 rule 1 names the
// keystore's — so Write takes it and does not guess it.
const FileName = "config.json"

// Applied is what one successful write knows afterwards. The two hashes are both reported because ODEC-36 is open:
// whichever byte string `configHash` turns out to mean, the answer is already in this struct rather than chosen by
// a writer that wanted a single field.
type Applied struct {
	Path         string
	Bytes        []byte
	ValueHash    string
	HandleHash   string
	PreviousHash string
}

// Write materializes doc, emits it with compile's own serializer, and installs it at dir/FileName.
//
// The sequence is doc 27 §8's and the same one keystore.Store.Put already uses, so there is one temp-and-rename
// discipline in this module to audit rather than two: create, write, sync, close, mode, rename. Anything that fails
// before the rename leaves the previous file byte-identical and removes the temp, which is the half of VER-29 that
// a test can prove; the half it cannot prove is a Core that was told to reload.
//
// What sync does not buy: a directory-level fsync has no Windows equivalent, so a power loss between the rename and
// the metadata flush can, in principle, lose the rename. doc 32 §13 calls that the timing risk of the exception and
// §17 lists the orphaned-file half as unsolved, and the config is re-derivable from intent plus keystore, so the
// answer is a re-apply rather than a recovery.
func Write(dir string, doc compile.Document, r Resolver) (*Applied, error) {
	if dir == "" {
		return nil, errors.New("apply: Write was given no directory; the compiled config is a file, not a buffer somebody keeps")
	}
	handleBytes, err := compile.Emit(doc)
	if err != nil {
		return nil, fmt.Errorf("apply: the compiled document could not be emitted: %w", err)
	}
	valueDoc, err := Materialize(doc, r)
	if err != nil {
		return nil, err
	}
	valueBytes, err := compile.Emit(valueDoc)
	if err != nil {
		return nil, fmt.Errorf("apply: the materialized document could not be emitted: %w", err)
	}

	previous, err := AppliedHash(dir)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("apply: %s: %w", dir, err)
	}
	target := filepath.Join(dir, FileName)

	tmp, err := os.CreateTemp(dir, "."+FileName+"-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("apply: %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Every failure below has to leave the directory holding exactly what it held before, or the "previous config
	// stays applied" rule is a sentence about a temp file nobody cleans up.
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(valueBytes); err != nil {
		return nil, fmt.Errorf("apply: %s: %w", target, err)
	}
	if err = tmp.Sync(); err != nil {
		return nil, fmt.Errorf("apply: %s could not be flushed: %w", target, err)
	}
	if err = tmp.Close(); err != nil {
		return nil, fmt.Errorf("apply: %s could not be closed: %w", target, err)
	}
	// doc 32 §13's owner is the only ACL this file inherits, and 0600 is the portable half of that claim: on Windows
	// the inherited user-profile descriptor governs, exactly as ADR-0011 rule 3 says of a keystore blob.
	if err = os.Chmod(tmpName, 0o600); err != nil {
		return nil, fmt.Errorf("apply: %s could not be made owner-only: %w", target, err)
	}
	if err = os.Rename(tmpName, target); err != nil {
		return nil, fmt.Errorf("apply: %s could not be installed: %w", target, err)
	}

	return &Applied{
		Path:         target,
		Bytes:        valueBytes,
		ValueHash:    compile.Hash(valueBytes),
		HandleHash:   compile.Hash(handleBytes),
		PreviousHash: previous,
	}, nil
}

// AppliedHash reports the hash of the config currently on disk, in compile's own rule over the file's exact bytes.
// A directory with no config yet answers the empty string, which is what `previousConfigHash` records for "nothing
// was applied": `packages/schema/protocol/commands/config.apply.json#/result/properties/previousConfigHash` allows
// either a lowercase sha256 or nothing at all, and a caller that had to invent a sentinel would be inventing a
// pattern the schema does not accept.
func AppliedHash(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("apply: %s: %w", dir, err)
	}
	return compile.Hash(data), nil
}
