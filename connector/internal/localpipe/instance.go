package localpipe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// InstanceRecordFile is the lock file `packages/schema/protocol/framing.json#/singleInstance/recordFile` names: the
// Connector's answer to "which process owns this root", written where doc 34 §7's tree puts state.
const InstanceRecordFile = "connector-instance.json"

// InstanceRecord is that file's body and exactly its four fields. Both processes read this type — the Connector
// writes it, the shim reads it to name the owner it refuses to replace — so the shape is one declaration rather
// than two decoders that have to agree.
type InstanceRecord struct {
	PID         int    `json:"pid"`
	PipeVersion int    `json:"pipeVersion"`
	StartedAt   string `json:"startedAt"`
	ConfigHash  string `json:"configHash"`
}

// NewInstanceRecord stamps the facts only a running process has. ConfigHash is the hash of the config in force,
// which is a fact the caller holds rather than one this package can resolve.
func NewInstanceRecord(pipeVersion int, configHash string) InstanceRecord {
	return InstanceRecord{
		PID:         os.Getpid(),
		PipeVersion: pipeVersion,
		StartedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		ConfigHash:  configHash,
	}
}

// InstanceRecordPath is the record's address under a per-user root: the state directory doc 34 §7 names, beside
// the applied-state file internal/supervisor keeps there.
func InstanceRecordPath(root string) string {
	return filepath.Join(root, "state", InstanceRecordFile)
}

// ReadInstanceRecord loads the record. A missing file is an error the caller can test with errors.Is(err,
// os.ErrNotExist): the absence is a fact — nobody has written the record yet — rather than a zero value that
// would name pid 0 as the owner.
func ReadInstanceRecord(root string) (InstanceRecord, error) {
	data, err := os.ReadFile(InstanceRecordPath(root))
	if err != nil {
		return InstanceRecord{}, fmt.Errorf("read the Connector instance record: %w", err)
	}
	var rec InstanceRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return InstanceRecord{}, fmt.Errorf("the Connector instance record is not the JSON this package wrote: %w", err)
	}
	return rec, nil
}

// WriteInstanceRecord installs the record with the temp-and-rename discipline every other file in this module is
// written with, so a reader never sees half a record: create, write, sync, close, rename, and a temp that
// disappears when anything fails.
func WriteInstanceRecord(root string, rec InstanceRecord) error {
	dir := filepath.Dir(InstanceRecordPath(root))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("the instance record directory is not there and cannot be made: %w", err)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("the instance record cannot be encoded: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".connector-instance-*")
	if err != nil {
		return fmt.Errorf("no temp for the instance record: %w", err)
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("the instance record was not written: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("the instance record was not flushed: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("the instance record temp would not close: %w", err)
	}
	if err = os.Rename(name, InstanceRecordPath(root)); err != nil {
		return fmt.Errorf("the instance record was not installed: %w", err)
	}
	return nil
}

// UpdateInstanceConfigHash repoints the record's configHash at the config a completed apply put in force, leaving
// the identity fields the record was stamped with. The owner does not change across an apply, and an apply cannot
// run without a record to update — the record is written before the first frame this process accepts.
func UpdateInstanceConfigHash(root, hash string) error {
	rec, err := ReadInstanceRecord(root)
	if err != nil {
		return err
	}
	rec.ConfigHash = hash
	return WriteInstanceRecord(root, rec)
}
