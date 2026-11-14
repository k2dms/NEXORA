package logstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSink is the test's lever: a sink that records exactly the lines it was handed after applying its own
// mask, the way every real sink does — masking is the sink's job, never the store's.
type fakeSink struct {
	name  string
	mask  *Masker
	lines []string
}

func (f *fakeSink) Name() string { return f.name }
func (f *fakeSink) Append(_ Record, line string) error {
	if f.mask != nil {
		line = f.mask.Apply(line)
	}
	f.lines = append(f.lines, line)
	return nil
}

func rec(level, component, msg string) Record {
	return Record{Level: level, Plane: "connector", Component: component, Msg: msg}
}

// The ship caps are observability.json#/sinks' connector-ring row: 2000 records, 1 MiB.
func TestTheShippedRingCapsAreTheContractS(t *testing.T) {
	if DefaultRingRecords != 2000 || DefaultRingBytes != 1048576 {
		t.Fatalf("ring caps are %d records / %d bytes, want the connector-ring row's 2000 / 1 MiB",
			DefaultRingRecords, DefaultRingBytes)
	}
}

// One render, every sink (doc 36 §8 rule 1): both sinks receive byte-identical masked lines for the same
// record, and both stamp the same seq — which is what makes a gap in one sink a gap in all of them.
func TestOneRenderReachesEverySink(t *testing.T) {
	first, second := &fakeSink{name: "connector-ring"}, &fakeSink{name: "connector-log-file"}
	s := NewStore("connector", first, second)
	if err := s.Append(rec("info", "supervisor", "core exited after 3 attempts")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(first.lines) != 1 || len(second.lines) != 1 {
		t.Fatalf("one record produced %d and %d lines, want one each", len(first.lines), len(second.lines))
	}
	if first.lines[0] != second.lines[0] {
		t.Fatalf("the two sinks received different bytes:\n%s\n%s", first.lines[0], second.lines[0])
	}
	var a, b Record
	if err := json.Unmarshal([]byte(first.lines[0]), &a); err != nil {
		t.Fatalf("the ring line is not the flat record shape: %v", err)
	}
	if err := json.Unmarshal([]byte(second.lines[0]), &b); err != nil {
		t.Fatalf("the file line is not the flat record shape: %v", err)
	}
	if a.Seq != 1 || b.Seq != 1 {
		t.Fatalf("seq is %d and %d, want the same monotonic 1 in both sinks", a.Seq, b.Seq)
	}
	if a.Plane != "connector" || b.Plane != "connector" {
		t.Fatal("the store did not stamp its own plane")
	}
}

// Masking is keyed by value at the sink (doc 32 §8): a keystore value inside a message is replaced whole,
// and nothing about the call site had to cooperate.
func TestTheMaskRunsAtTheSinkOverBareStrings(t *testing.T) {
	mask, err := NewMasker("[masked]", []string{"s3cr3t-keystore-value"})
	if err != nil {
		t.Fatalf("NewMasker: %v", err)
	}
	sink := &fakeSink{name: "connector-ring", mask: mask}
	ring := NewRing("connector-ring", mask, DefaultRingRecords, DefaultRingBytes)
	s := NewStore("connector", ring, sink)
	// The interesting secret in a log is in a message: the exact case the legacy replacer lost.
	if err := s.Append(rec("warn", "keystore", "resolve failed for s3cr3t-keystore-value after rotation")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	rendered := sink.lines[0]
	if strings.Contains(rendered, "s3cr3t-keystore-value") {
		t.Fatalf("the keystore value survived into the sink line: %s", rendered)
	}
	if !strings.Contains(rendered, "[masked]") {
		t.Fatalf("the mask is absent from the sink line: %s", rendered)
	}
	// The ring holds the masked line too: its record's msg no longer carries the value.
	held := ring.Records()
	if len(held) != 1 || strings.Contains(held[0].Msg, "s3cr3t-keystore-value") {
		t.Fatalf("the ring does not hold the masked record: %+v", held)
	}
}

func TestTheMaskRefusesShortAndEmptyInputs(t *testing.T) {
	if _, err := NewMasker("", nil); err == nil {
		t.Fatal("an empty mask was accepted")
	}
	if _, err := NewMasker("[masked]", []string{"ab"}); err == nil {
		t.Fatal("a 2-byte value was accepted into the table")
	}
}

// seq is the gap detector (doc 36 §5's rule): a ring that evicted shows it — its oldest surviving seq is
// above 1 while the newest keeps climbing — and that is the only difference between an evicted ring and a
// lost record.
func TestRingEvictionByCountKeepsSeqMonotonicAndShowsTheGap(t *testing.T) {
	mask, _ := NewMasker("[masked]", nil)
	ring := NewRing("connector-ring", mask, 5, DefaultRingBytes)
	s := NewStore("connector", ring)
	for i := 0; i < 12; i++ {
		if err := s.Append(rec("info", "dispatch", "record")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	got := ring.Records()
	if len(got) != 5 {
		t.Fatalf("ring holds %d records, want the 5-record cap", len(got))
	}
	if got[0].Seq != 8 {
		t.Fatalf("the oldest surviving seq is %d, want 8: seven records evicted", got[0].Seq)
	}
	if got[4].Seq != 12 || s.LastSeq() != 12 {
		t.Fatalf("the newest seq is %d and the store's is %d, want 12", got[4].Seq, s.LastSeq())
	}
}

func TestRingEvictionByBytesWhenCountsWouldFit(t *testing.T) {
	mask, _ := NewMasker("[masked]", nil)
	// Four records at ~150 rendered bytes each fit 2000 records but not 512 bytes: the byte cap wins.
	ring := NewRing("connector-ring", mask, DefaultRingRecords, 512)
	s := NewStore("connector", ring)
	for i := 0; i < 4; i++ {
		if err := s.Append(rec("info", "dispatch", strings.Repeat("x", 100))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if got := ring.Records(); len(got) >= 4 {
		t.Fatalf("the byte cap evicted nothing: %d records survive", len(got))
	}
}

func TestRingKeepsASingleOversizedRecord(t *testing.T) {
	mask, _ := NewMasker("[masked]", nil)
	ring := NewRing("connector-ring", mask, DefaultRingRecords, 64)
	s := NewStore("connector", ring)
	big := rec("error", "supervisor", strings.Repeat("y", 200))
	if err := s.Append(big); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got := ring.Records()
	if len(got) != 1 || got[0].Msg != big.Msg {
		t.Fatalf("the byte cap evicted the only witness: %d records survive", len(got))
	}
}

// The file sink rotates oldest-first at its byte cap and never carries more files than it declared.
func TestFileSinkRotatesAndCapsTheFileCount(t *testing.T) {
	dir := t.TempDir()
	mask, _ := NewMasker("[masked]", []string{"s3cr3t-keystore-value"})
	f, err := NewFileSink("connector-log-file", dir, "connector.log", mask, 256, 3, 30)
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}
	defer func() { _ = f.Close() }()
	s := NewStore("connector", f)
	for i := 0; i < 30; i++ {
		if err := s.Append(rec("info", "dispatch", "rotate me s3cr3t-keystore-value")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	rotated := 0
	for i := 0; i < 3; i++ {
		data, err := os.ReadFile(filepath.Join(dir, "connector.log."+string(rune('0'+i))))
		if err != nil {
			t.Fatalf("reading rotation %d: %v", i, err)
		}
		if len(data) > 256*2 {
			t.Fatalf("rotation %d carries %d bytes, over its cap", i, len(data))
		}
		if strings.Contains(string(data), "s3cr3t-keystore-value") {
			t.Fatalf("rotation %d holds an unmasked keystore value", i)
		}
		if len(data) > 0 {
			rotated++
		}
	}
	if rotated == 0 {
		t.Fatal("no rotation file has content: rotation never ran")
	}
}

// retentionDays 0 means the current session only: truncated at start, never appended (doc 36 §10).
func TestRetentionZeroTruncatesAtStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connector.log.0")
	if err := os.WriteFile(path, []byte("previous session's records\n"), 0o600); err != nil {
		t.Fatalf("seeding the previous session: %v", err)
	}
	mask, _ := NewMasker("[masked]", nil)
	f, err := NewFileSink("connector-log-file", dir, "connector.log", mask, 4096, 3, 0)
	if err != nil {
		t.Fatalf("NewFileSink: %v", err)
	}
	defer func() { _ = f.Close() }()
	s := NewStore("connector", f)
	if err := s.Append(rec("info", "dispatch", "current session only")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if strings.Contains(string(data), "previous session's records") {
		t.Fatalf("retentionDays 0 appended to the previous session: %s", data)
	}
	if !strings.Contains(string(data), "current session only") {
		t.Fatalf("the current session's record is absent: %s", data)
	}
}
