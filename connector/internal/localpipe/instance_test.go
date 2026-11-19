package localpipe

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contractgen"
)

// TestInstanceRecordMatchesTheFramingContract is the drift check between the two readers of one file: the record
// `framing.json#/singleInstance/recordFile` describes is the record this package writes and the shim decodes. The
// path, the four field names and their JSON spelling all come from the data, so a field added here without a
// contract change fails instead of producing a file a peer silently ignores.
func TestInstanceRecordMatchesTheFramingContract(t *testing.T) {
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "packages", "schema", "protocol", "framing.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SingleInstance struct {
			RecordFile struct {
				Path   string   `json:"path"`
				Fields []string `json:"fields"`
			} `json:"recordFile"`
		} `json:"singleInstance"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	declared := doc.SingleInstance.RecordFile
	if declared.Path == "" || len(declared.Fields) == 0 {
		t.Fatalf("framing.json#/singleInstance/recordFile names no path or no fields: %+v", declared)
	}

	want := filepath.Join("per-user-root", filepath.FromSlash(declared.Path))
	if got := InstanceRecordPath("per-user-root"); got != want {
		t.Errorf("InstanceRecordPath = %q, want %q", got, want)
	}

	encoded, err := json.Marshal(InstanceRecord{})
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(shape))
	for name := range shape {
		got = append(got, name)
	}
	sort.Strings(got)
	sorted := append([]string(nil), declared.Fields...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(got, sorted) {
		t.Errorf("InstanceRecord fields = %v, framing.json declares %v", got, declared.Fields)
	}
	if InstanceRecordFile != filepath.Base(want) {
		t.Errorf("InstanceRecordFile = %q, the contract's path ends in %q", InstanceRecordFile, filepath.Base(want))
	}
}

func TestInstanceRecordRoundTripsAndKeepsItsIdentity(t *testing.T) {
	root := t.TempDir()
	if _, err := ReadInstanceRecord(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a root with no record answered %v, want a not-exist error", err)
	}
	before := InstanceRecord{PID: 4242, PipeVersion: 3, StartedAt: "2026-11-19T00:00:00Z", ConfigHash: "previous"}
	if err := WriteInstanceRecord(root, before); err != nil {
		t.Fatal(err)
	}
	if err := UpdateInstanceConfigHash(root, "next"); err != nil {
		t.Fatal(err)
	}
	after, err := ReadInstanceRecord(root)
	if err != nil {
		t.Fatal(err)
	}
	before.ConfigHash = "next"
	if after != before {
		t.Fatalf("record after the update = %+v, want %+v", after, before)
	}
}
