package main

import (
	"testing"

	"github.com/k2dms/nexora/connector/internal/localpipe"
)

// TestASharedApplyRepointsTheInstanceRecordAtTheConfigItPutInForce is `framing.json#/singleInstance/recordFile`
// after the one event that can move its `configHash`: a `--shared` Connector's record names the config in force,
// and an apply that moved the hash has to move the record with it. The identity fields are the owner's and do not
// move — the process that applied the config is the process the shim will adopt.
func TestASharedApplyRepointsTheInstanceRecordAtTheConfigItPutInForce(t *testing.T) {
	h := newHarness(t, baseIntent(t), testProtector{})
	root := t.TempDir()
	before := localpipe.InstanceRecord{PID: 4242, PipeVersion: 1, StartedAt: "2026-11-19T00:00:00Z", ConfigHash: "previous"}
	if err := localpipe.WriteInstanceRecord(root, before); err != nil {
		t.Fatal(err)
	}
	surface, err := newConfigApply(applyConfig{StateDir: h.dirs.state, ConfigDir: h.dirs.config, InstanceRoot: root},
		h.record, h.store, h.core)
	if err != nil {
		t.Fatal(err)
	}
	h.surface = surface

	answer, _, refusal := h.ask(t, `{"profileIds":["`+profileID+`"]}`)
	if refusal != nil {
		t.Fatalf("apply refused: %s: %s", refusal.Code, refusal.Message)
	}
	if answer.ConfigHash == "" || answer.ConfigHash == before.ConfigHash {
		t.Fatalf("the apply did not move the hash: answer %q, record %q", answer.ConfigHash, before.ConfigHash)
	}
	after, err := localpipe.ReadInstanceRecord(root)
	if err != nil {
		t.Fatal(err)
	}
	before.ConfigHash = answer.ConfigHash
	if after != before {
		t.Fatalf("record = %+v, want %+v: only configHash may move across an apply", after, before)
	}
}
