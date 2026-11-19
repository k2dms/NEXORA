package dispatch

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
)

// chunkClock is the same fake clock the budget tests use; the chunk gate reads it for the import budget and
// the group timeout.
type chunkClock struct{ t time.Time }

func (c *chunkClock) Now() time.Time { return c.t }

// fragment splits a document the way the sender does: sha256 over the concatenation in chunkSeq order, each
// fragment base64 in args.payload — the fragment model ODEC-34 decided.
func fragment(doc []byte, parts int) (hash string, chunks []string) {
	step := (len(doc) + parts - 1) / parts
	for lo := 0; lo < len(doc); lo += step {
		hi := lo + step
		if hi > len(doc) {
			hi = len(doc)
		}
		chunks = append(chunks, base64.StdEncoding.EncodeToString(doc[lo:hi]))
	}
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:]), chunks
}

func chunkedSlice(id string, seq, count int, hash, payload string) *envelope.Frame {
	m := "profile.import"
	c := &envelope.Chunk{ContentHash: hash, ChunkSeq: seq, ChunkCount: count, Encoding: "base64"}
	args, _ := json.Marshal(map[string]any{
		"profiles": []any{}, "mode": "merge",
		"contentHash": hash, "chunk": c, "payload": payload,
	})
	return &envelope.Frame{V: contract.FrameFormatVersion, ID: id, Type: envelope.TypeRequest, Method: &m,
		Args: args, SentAt: envelope.Now(), Chunk: c}
}

// chunkDispatcher wires the counting handler under profile.import with the full hello the capability gate needs.
func chunkDispatcher(t *testing.T) (*Dispatcher, *int, *chunkClock) {
	t.Helper()
	n := 0
	clk := &chunkClock{t: time.Unix(0, 0)}
	d, err := New(Config{Handlers: map[string]Handler{"profile.import": func(context.Context, *envelope.Frame) (any, *Refusal) {
		n++
		return "handled", nil
	}, "connector.version": func(context.Context, *envelope.Frame) (any, *Refusal) {
		return map[string]any{}, nil
	}}, Now: clk.Now})
	if err != nil {
		t.Fatalf("dispatch.New: %v", err)
	}
	return d, &n, clk
}

func chunkedIdentity() Identity { return negotiated("chunked.import") }

func dispatchSlice(t *testing.T, d *Dispatcher, f *envelope.Frame) (any, *Refusal) {
	t.Helper()
	return d.Dispatch(context.Background(), chunkedIdentity(), f)
}

func refuseCode(t *testing.T, r *Refusal) string {
	t.Helper()
	if r == nil {
		t.Fatal("the frame was admitted where a refusal was required")
	}
	return r.Code
}

func TestAChunkGroupIsAnsweredByProgressFramesAndOneResponse(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	doc := []byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"servers":[],"mode":"merge"}`)
	hash, chunks := fragment(doc, 3)
	if len(chunks) != 3 {
		t.Fatalf("the fixture split into %d fragments", len(chunks))
	}
	slice := func(seq int) *envelope.Frame { return chunkedSlice("pi-req-0001", seq, 3, hash, chunks[seq]) }

	for seq, want := range []int{1, 2} {
		answer, r := dispatchSlice(t, d, slice(seq))
		if r != nil {
			t.Fatalf("slice %d was refused: %v", seq, r)
		}
		p, ok := answer.(*ChunkProgress)
		if !ok {
			t.Fatalf("slice %d was answered %T, not a progress answer", seq, answer)
		}
		if p.Stage != "reassembling" || p.Done != want || p.Total != 3 {
			t.Fatalf("slice %d was answered %+v, want reassembling %d/3", seq, p, want)
		}
	}
	if *ran != 0 {
		t.Fatal("an intermediate slice reached the handler before the group was whole")
	}
	answer, r := dispatchSlice(t, d, slice(2))
	if r != nil {
		t.Fatalf("the final slice was refused: %v", r)
	}
	if text, ok := answer.(string); !ok || text != "handled" {
		t.Fatalf("the final slice was answered %v", answer)
	}
	if *ran != 1 {
		t.Fatalf("the handler ran %d times for one import", *ran)
	}
	// The answer lives under the group id: any later frame with that id replays it, slices included.
	replay, r := dispatchSlice(t, d, slice(0))
	if r != nil {
		t.Fatalf("a re-sent slice of a completed group was refused: %v", r)
	}
	if raw, ok := replay.(json.RawMessage); !ok || string(raw) != `"handled"` {
		t.Fatalf("the re-sent slice replayed %v, not the group's final answer", replay)
	}
	replay, r = dispatchSlice(t, d, slice(2))
	if r != nil {
		t.Fatalf("a re-sent final slice was refused: %v", r)
	}
	if raw, ok := replay.(json.RawMessage); !ok || string(raw) != `"handled"` {
		t.Fatalf("the re-sent final slice replayed %v", replay)
	}
	if *ran != 1 {
		t.Fatal("a replay re-ran the handler")
	}
}

func TestTheImportBudgetIsSpentOncePerGroup(t *testing.T) {
	d, _, _ := chunkDispatcher(t)
	doc := []byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge"}`)
	hash, chunks := fragment(doc, 2)
	// One complete group: the named budget records one import, not two slice frames plus a final.
	for seq, payload := range chunks {
		if _, r := dispatchSlice(t, d, chunkedSlice("grp-a-0001", seq, 2, hash, payload)); r != nil {
			t.Fatalf("slice %d was refused: %v", seq, r)
		}
	}
	// A second import in the window is the budget's own allowance; a third is the bug the budget answers.
	if _, r := dispatchSlice(t, d, chunkedSlice("grp-b-0001", 0, 2, hash, chunks[0])); r != nil {
		t.Fatalf("a second import group was refused: %v", r)
	}
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("grp-c-0001", 0, 2, hash, chunks[0]))); code != "Busy" {
		t.Fatalf("a third opening in one window was answered %s, want Busy", code)
	}
}

func mustRefuse(t *testing.T, d *Dispatcher, f *envelope.Frame) *Refusal {
	t.Helper()
	_, r := dispatchSlice(t, d, f)
	return r
}

func TestAnOutOfOrderSliceFailsTheWholeImport(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	doc := []byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge"}`)
	hash, chunks := fragment(doc, 3)
	if _, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", 0, 3, hash, chunks[0])); r != nil {
		t.Fatalf("slice 0 was refused: %v", r)
	}
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0001", 2, 3, hash, chunks[2]))); code != "IntegrityMismatch" {
		t.Fatalf("a jump from slice 0 to slice 2 was answered %s, want IntegrityMismatch", code)
	}
	// The whole import failed, not just the jumping slice: nothing is open to receive the missing one.
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0001", 1, 3, hash, chunks[1]))); code != "IntegrityMismatch" {
		t.Fatalf("the slice after a dropped group was answered %s, want IntegrityMismatch", code)
	}
	if *ran != 0 {
		t.Fatal("a group that failed its order rule reached the handler")
	}
}

func TestASplitHashIsRefusedTheWayTheRecordedGroupIs(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	hash, chunks := fragment([]byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge"}`), 2)
	sum := sha256.Sum256([]byte(`{"profiles":[]}`))
	other := hex.EncodeToString(sum[:])
	if _, r := dispatchSlice(t, d, chunkedSlice("pi-req-0002", 0, 2, hash, chunks[0])); r != nil {
		t.Fatalf("slice 0 was refused: %v", r)
	}
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0002", 1, 2, other, chunks[1]))); code != "IntegrityMismatch" {
		t.Fatalf("a slice declaring a different contentHash was answered %s, want IntegrityMismatch", code)
	}
	if *ran != 0 {
		t.Fatal("a split group reached the handler")
	}
}

func TestAReaskedSliceIsAnsweredFromState(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	doc := []byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge"}`)
	hash, chunks := fragment(doc, 3)
	first, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", 0, 3, hash, chunks[0]))
	if r != nil {
		t.Fatalf("slice 0 was refused: %v", r)
	}
	again, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", 0, 3, hash, chunks[0]))
	if r != nil {
		t.Fatalf("a re-asked slice was refused: %v", r)
	}
	a, b := first.(*ChunkProgress), again.(*ChunkProgress)
	if a.Done != b.Done || a.Total != b.Total {
		t.Fatalf("the re-ask answered %+v where the first ask answered %+v", b, a)
	}
	if *ran != 0 {
		t.Fatal("a re-asked slice ran the handler")
	}
	for seq := 1; seq < 3; seq++ {
		if _, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", seq, 3, hash, chunks[seq])); r != nil {
			t.Fatalf("slice %d was refused after the re-ask: %v", seq, r)
		}
	}
	if *ran != 1 {
		t.Fatalf("the import ran %d times after a re-asked slice", *ran)
	}
}

func TestAnUndecodableFragmentRefusesAndDropsTheGroup(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	hash, chunks := fragment([]byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge"}`), 2)
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0001", 0, 2, hash, "!!!not base64"))); code != "BadRequest" {
		t.Fatalf("an undecodable fragment was answered %s, want BadRequest", code)
	}
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0001", 1, 2, hash, chunks[1]))); code != "IntegrityMismatch" {
		t.Fatalf("the group survived a fragment that could not be recorded: %s", code)
	}
	if *ran != 0 {
		t.Fatal("a group with an undecodable fragment reached the handler")
	}
}

func TestTheReassembledDocumentMayNotCarryWhatAChunkedImportMustNot(t *testing.T) {
	for _, field := range []string{"credentials", "contentHash", "payload", "chunk"} {
		d, ran, _ := chunkDispatcher(t)
		doc := []byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge","` + field + `":{}}`)
		hash, chunks := fragment(doc, 2)
		if _, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", 0, 2, hash, chunks[0])); r != nil {
			t.Fatalf("%s: slice 0 was refused: %v", field, r)
		}
		if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0001", 1, 2, hash, chunks[1]))); code != "BadRequest" {
			t.Fatalf("a reassembled document carrying %q was answered %s, want BadRequest", field, code)
		}
		if *ran != 0 {
			t.Fatalf("a document carrying %q reached the handler", field)
		}
	}
}

func TestAHashMismatchOnACompleteGroupIsIntegrityMismatch(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	_, chunks := fragment([]byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge"}`), 2)
	wrong := strings.Repeat("ab", 32)
	if _, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", 0, 2, wrong, chunks[0])); r != nil {
		t.Fatalf("slice 0 was refused: %v", r)
	}
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0001", 1, 2, wrong, chunks[1]))); code != "IntegrityMismatch" {
		t.Fatalf("a reassembly that does not hash to the declared hash was answered %s", code)
	}
	if *ran != 0 {
		t.Fatal("an unverified reassembly reached the handler")
	}
}

func TestTheReassembledCapIsTheSchemaMaximum(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "packages", "schema", "subscription-source.schema.json"))
	if err != nil {
		t.Fatalf("subscription-source.schema.json: %v", err)
	}
	var schema struct {
		Properties struct {
			Limits struct {
				Properties struct {
					MaxBytes struct {
						Maximum int `json:"maximum"`
					} `json:"maxBytes"`
				} `json:"properties"`
			} `json:"limits"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("the schema did not parse: %v", err)
	}
	if got := schema.Properties.Limits.Properties.MaxBytes.Maximum; got != reassembledCapBytes {
		t.Fatalf("the gate's cap is %d where subscription-source limits.maxBytes's maximum is %d", reassembledCapBytes, got)
	}
}

func TestACapOverflowRefusesTheGroup(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	big := base64.StdEncoding.EncodeToString(make([]byte, reassembledCapBytes+1))
	hash := strings.Repeat("cd", 32)
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0001", 0, 2, hash, big))); code != "BadRequest" {
		t.Fatalf("a slice larger than the cap was answered %s, want BadRequest", code)
	}
	if *ran != 0 {
		t.Fatal("an over-cap group reached the handler")
	}
}

func TestAChunkFrameForAnotherMethodIsRefused(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	hash := strings.Repeat("ef", 32)
	m := "connector.version"
	c := &envelope.Chunk{ContentHash: hash, ChunkSeq: 0, ChunkCount: 2, Encoding: "base64"}
	f := &envelope.Frame{V: contract.FrameFormatVersion, ID: "req-ver-0001", Type: envelope.TypeRequest,
		Method: &m, SentAt: envelope.Now(), Chunk: c}
	if code := refuseCode(t, mustRefuse(t, d, f)); code != "BadRequest" {
		t.Fatalf("a chunk frame on connector.version was answered %s, want BadRequest", code)
	}
	if *ran != 0 {
		t.Fatal("a chunk frame on another method reached its handler")
	}
}

func TestAWholeFrameMayNotBorrowAnOpenGroupId(t *testing.T) {
	d, ran, _ := chunkDispatcher(t)
	hash, chunks := fragment([]byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge"}`), 2)
	if _, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", 0, 2, hash, chunks[0])); r != nil {
		t.Fatalf("slice 0 was refused: %v", r)
	}
	m := "profile.import"
	whole := &envelope.Frame{V: contract.FrameFormatVersion, ID: "pi-req-0001", Type: envelope.TypeRequest,
		Method: &m, Args: json.RawMessage(`{"profiles":[{"id":"x"}]}`), SentAt: envelope.Now()}
	if code := refuseCode(t, mustRefuse(t, d, whole)); code != "BadRequest" {
		t.Fatalf("a whole-frame request on an open group's id was answered %s, want BadRequest", code)
	}
	// The group is unharmed: the import completes normally.
	if _, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", 1, 2, hash, chunks[1])); r != nil {
		t.Fatalf("the final slice was refused after the borrowed-id attempt: %v", r)
	}
	if *ran != 1 {
		t.Fatalf("the import ran %d times", *ran)
	}
}

func TestAStalledGroupExpiresPastItsCommandTimeout(t *testing.T) {
	d, ran, clk := chunkDispatcher(t)
	hash, chunks := fragment([]byte(`{"profiles":[{"id":"b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11"}],"mode":"merge"}`), 2)
	if _, r := dispatchSlice(t, d, chunkedSlice("pi-req-0001", 0, 2, hash, chunks[0])); r != nil {
		t.Fatalf("slice 0 was refused: %v", r)
	}
	// The command's own deadline is 60 s; a group that outlives it is a stalled buffer, not a transfer.
	clk.t = clk.t.Add(61 * time.Second)
	if code := refuseCode(t, mustRefuse(t, d, chunkedSlice("pi-req-0001", 1, 2, hash, chunks[1]))); code != "IntegrityMismatch" {
		t.Fatalf("a slice for a group past its command timeout was answered %s, want IntegrityMismatch", code)
	}
	if *ran != 0 {
		t.Fatal("an expired group reached the handler")
	}
}
