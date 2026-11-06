package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// StateFileName is the applied-state record. The corpus names four properties of this file, its writer, its
// lifetime and its bound — `packages/schema/protocol/data-ownership.json`'s `applied-state` row, repeated in
// `docs/architecture/34-data-split.md` §7 as "applied state (hash, selection, PID, lastExit)" — and never a
// path, so the directory is the caller's and only this name is this package's.
const StateFileName = "state.json"

// State is those four fields and no fifth. Containment is not among them, which is the corpus being right rather
// than thin: whether a child is inside a Job Object is a fact about a process that no longer exists once this
// file is read, and doc 30 §3 makes the containment answer belong to the call that spawned it.
type State struct {
	AppliedHash     string `json:"appliedConfigHash"`
	SelectedProfile string `json:"selectedProfile,omitempty"`
	LastStartPID    int    `json:"lastStartPid,omitempty"`
	LastExit        *Exit  `json:"lastExit,omitempty"`
}

// ReadState loads the record. A directory with no file is not an error and not a corruption: it is the first
// apply, and the empty State it answers with is what makes `previousConfigHash` an empty string rather than a
// sentinel, the same reading `connector/internal/apply`'s AppliedHash takes from
// `packages/schema/protocol/commands/config.apply.json#/result/properties/previousConfigHash`'s pattern.
func ReadState(dir string) (State, error) {
	if dir == "" {
		return State{}, errors.New("supervisor: no state directory given")
	}
	data, err := os.ReadFile(filepath.Join(dir, StateFileName))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("supervisor: the applied-state file could not be read: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("supervisor: the applied-state file is not the JSON this package wrote: %w", err)
	}
	return st, nil
}

// WriteState installs the record with the same temp-and-rename discipline as the config file beside it, so that
// one rule about atomic replacement holds in this module rather than two: create, write, sync, close, mode,
// rename, and a temp that disappears when anything fails.
//
// The file is owner-only for the same reason the config is, with one difference worth stating because it is the
// reason this write exists: applied-state holds no credential (data-ownership's own `holdsValue` answer for it
// is "no"), so 0600 here is about a user's server list and PIDs rather than about secret material.
func WriteState(dir string, st State) error {
	if dir == "" {
		return errors.New("supervisor: no state directory given")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("supervisor: the state directory is not there and cannot be made: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("supervisor: the applied-state record cannot be encoded: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+StateFileName+"-*.tmp")
	if err != nil {
		return fmt.Errorf("supervisor: no temp for the applied-state record: %w", err)
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("supervisor: the applied-state record was not written: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("supervisor: the applied-state record was not flushed: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("supervisor: the applied-state temp would not close: %w", err)
	}
	if err = os.Chmod(name, 0o600); err != nil {
		return fmt.Errorf("supervisor: the applied-state record could not be made owner-only: %w", err)
	}
	if err = os.Rename(name, filepath.Join(dir, StateFileName)); err != nil {
		return fmt.Errorf("supervisor: the applied-state record was not installed: %w", err)
	}
	return nil
}
