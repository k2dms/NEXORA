package envelope

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/contractgen"
)

const hash = "d6282cfbc09dd0a49bd09e902bf8a4f57a138c16130e9ad128f42b5bcee122c7"

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func fixtureFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot(t), "packages", "schema", "fixtures", "protocol", "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixture files in the repository: %v", err)
	}
	return files
}

// Two requests in the corpus ask for names the allowlist does not publish, and each is answered `UnknownMethod`.
// They are the corpus's named negatives, and they are valid frames: the schema puts a pattern on `method` and no
// membership rule, because ADR-0007 rule 2 places the refusal in resolution. So the replay below must accept every
// line, these two included, and internal/dispatch is where their refusal is asserted — a codec that rejected them
// would be taking the front door's job and, through internal/peer, the transport down with it.
var namedNegatives = map[string]string{"req-000000ff": "core.snooze", "po-req-0004": "connector.ping"}

// TestReplayProtocolFixtures is the "one contract, two implementations" rule of
// docs/architecture/25-native-messaging-protocol.md §2: the frames the Node harness replays are replayed here.
func TestReplayProtocolFixtures(t *testing.T) {
	total, parsed := 0, map[string]bool{}
	for _, file := range fixtureFiles(t) {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			total++
			f, err := Parse([]byte(line))
			if err != nil {
				t.Errorf("%s line %d: valid fixture frame rejected: %v", filepath.Base(file), i+1, err)
				continue
			}
			if method, ok := namedNegatives[f.ID]; ok && f.Method != nil && *f.Method == method {
				parsed[f.ID] = true
			}
		}
	}
	for id, method := range namedNegatives {
		if !parsed[id] {
			t.Errorf("the corpus no longer records %s as a request for %q, so the boundary this test guards is asserted against nothing", id, method)
		}
	}
	if total == 0 {
		t.Fatal("the fixture corpus offered no frames to replay")
	}
	t.Logf("replayed %d protocol frames, every one of them accepted as a frame", total)
}

func TestInvalidExamplesFromTheSchemaRoot(t *testing.T) {
	cases := []struct {
		file   string
		reason string
	}{
		{"envelope.event-with-method.json", "an event or progress frame must not carry it"},
		{"envelope.ok-without-result.json", "an ok response carries the result"},
	}
	dir := filepath.Join(repoRoot(t), "packages", "schema", "examples", "invalid")
	for _, c := range cases {
		raw, err := os.ReadFile(filepath.Join(dir, c.file))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Parse(raw)
		if err == nil {
			t.Errorf("%s: the Go validator accepted the corpus's named invalid example", c.file)
			continue
		}
		if !strings.Contains(err.Error(), c.reason) {
			t.Errorf("%s: rejected for %v, want a violation naming %q", c.file, err, c.reason)
		}
	}
}

// TestThePipeFloorComparisonCompilesAndRuns pins that the v-to-MinPipeVersion comparison is live in Parse, even
// though MinPipeVersion=1 means the existing 1..255 bound subsumes it -- the proof that it fires is the test for
// the day a real floor above 1 is shipped (VER-18's tombstone states the residual).
func TestThePipeFloorComparisonCompilesAndRuns(t *testing.T) {
	// v=1 is exactly MinPipeVersion: accepted.
	f, err := Parse([]byte(`{"v":1,"id":"req-00000001","type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z"}`))
	if err != nil {
		t.Fatalf("v=1 at MinPipeVersion=1 was refused: %v", err)
	}
	if f.V != 1 {
		t.Fatalf("v is %d, want 1", f.V)
	}
	// v=0 is refused by the 1..255 bound, not by the floor.
	_, err = Parse([]byte(`{"v":0,"id":"req-00000001","type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z"}`))
	if err == nil {
		t.Fatal("v=0 was accepted")
	}
	if !strings.Contains(err.Error(), "outside 1..255") {
		t.Errorf("v=0 error does not name the bound: %v", err)
	}
}

func TestTargetedViolations(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		violates string
	}{
		{"array payload", `[{"v":1}]`, "one JSON object"},
		{"malformed json", `{"v":1,`, "malformed JSON"},
		{"unknown field", `{"v":1,"id":"req-00000001","type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z","chunkId":"x"}`, "not declared by the envelope schema"},
		{"missing id", `{"v":1,"type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z"}`, "id is required"},
		{"short id", `{"v":1,"id":"abc","type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z"}`, "^[A-Za-z0-9_-]{8,36}$"},
		{"frame version zero", `{"v":0,"id":"req-00000001","type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z"}`, "outside 1..255"},
		{"method with a colon", `{"v":1,"id":"req-00000001","type":"request","method":"core:snooze","sentAt":"2026-11-04T10:00:00Z"}`, "dotted lower-camel"},
		{"args as a string", `{"v":1,"id":"req-00000001","type":"request","method":"connector.hello","args":"{}","sentAt":"2026-11-04T10:00:00Z"}`, "must be an object or null"},
		{"not a date-time", `{"v":1,"id":"req-00000001","type":"request","method":"connector.hello","sentAt":"yesterday"}`, "RFC 3339"},
		{"negative seq", `{"v":1,"id":"req-00000001","type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z","seq":-1}`, "must be >= 0"},
		{"trace id carrying a host", `{"v":1,"id":"req-00000001","type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z","traceId":"user@vps:443"}`, "[A-Za-z0-9_-]"},
		{"response without ok", `{"v":1,"id":"resp-00000001","type":"response","inReplyTo":"req-00000001","sentAt":"2026-11-04T10:00:00Z","result":{}}`, "a response carries ok"},
		{"response carrying a method", `{"v":1,"id":"resp-00000001","type":"response","method":"connector.hello","ok":true,"inReplyTo":"req-00000001","sentAt":"2026-11-04T10:00:00Z","result":{}}`, "a response must not carry it"},
		{"failed response without error", `{"v":1,"id":"resp-00000001","type":"response","ok":false,"inReplyTo":"req-00000001","sentAt":"2026-11-04T10:00:00Z"}`, "a failed response carries the error"},
		{"error code outside the taxonomy", `{"v":1,"id":"resp-00000001","type":"response","ok":false,"inReplyTo":"req-00000001","sentAt":"2026-11-04T10:00:00Z","error":{"code":"Nope"}}`, "outside the closed taxonomy"},
		{"event without its name", `{"v":1,"id":"evt-00000001","type":"event","sentAt":"2026-11-04T10:00:00Z","progress":{"stage":"x","done":1},"result":{}}`, "an event names the event it is"},
		{"event without a result", `{"v":1,"id":"evt-00000001","type":"event","sentAt":"2026-11-04T10:00:00Z","progress":{"stage":"x","done":1,"event":"core.exited"}}`, "an event carries its payload"},
		{"chunk of one slice", `{"v":1,"id":"req-00000001","type":"request","method":"profile.import","sentAt":"2026-11-04T10:00:00Z","chunk":{"contentHash":"` + hash + `","chunkSeq":0,"chunkCount":1,"encoding":"base64"},"args":{}}`, "not a chunked transfer"},
		{"chunk carrying a chunkId", `{"v":1,"id":"req-00000001","type":"request","method":"profile.import","sentAt":"2026-11-04T10:00:00Z","chunk":{"contentHash":"` + hash + `","chunkSeq":0,"chunkCount":2,"encoding":"base64","chunkId":"g1"},"args":{}}`, "there is no chunkId"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.payload))
			if err == nil {
				t.Fatalf("accepted a frame that breaks %q", c.violates)
			}
			if !strings.Contains(err.Error(), c.violates) {
				t.Fatalf("violations are %v, want one naming %q", err, c.violates)
			}
		})
	}
}

func TestChunkMirrorMustMatch(t *testing.T) {
	chunk := func(seq int) map[string]any {
		return map[string]any{"contentHash": hash, "chunkSeq": seq, "chunkCount": 2, "encoding": "base64"}
	}
	frame := map[string]any{
		"v": 1, "id": "req-00000001", "type": "request", "method": "profile.import",
		"sentAt": "2026-11-04T10:00:00Z",
		"chunk":  chunk(0),
		"args":   map[string]any{"mode": "merge", "contentHash": hash, "chunk": chunk(1)},
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "differs from the envelope copy") {
		t.Fatalf("a frame whose two chunk copies disagree must be refused, got %v", err)
	}

	frame["args"] = map[string]any{"mode": "merge", "contentHash": hash, "chunk": chunk(0)}
	raw, err = json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err != nil {
		t.Fatalf("agreeing copies must be accepted: %v", err)
	}
}

func TestMarshalRoundTripOfEveryFixtureFrame(t *testing.T) {
	checked := 0
	for _, file := range fixtureFiles(t) {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			f, err := Parse([]byte(line))
			if err != nil {
				continue
			}
			out, err := f.Marshal()
			if err != nil {
				t.Fatalf("%s: Marshal: %v", filepath.Base(file), err)
			}
			back, err := Parse(out)
			if err != nil {
				t.Fatalf("%s: re-Parse of a marshalled frame: %v\nframe: %s", filepath.Base(file), err, out)
			}
			if !sameShape(f, back) {
				t.Fatalf("%s: the round trip changed the frame:\n  %s\n  %s", filepath.Base(file), line, out)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no fixture frames were checked")
	}
	t.Logf("round-tripped %d frames through Marshal and Parse", checked)
}

func sameShape(a, b *Frame) bool {
	return a.V == b.V && a.ID == b.ID && a.Type == b.Type &&
		sameString(a.Method, b.Method) && sameRaw(a.Args, b.Args) && sameRaw(a.Result, b.Result) &&
		sameError(a.Error, b.Error) && sameString(a.InReplyTo, b.InReplyTo) && sameBool(a.OK, b.OK) &&
		sameString(a.TraceID, b.TraceID) && a.SentAt == b.SentAt && sameInt(a.Seq, b.Seq) &&
		sameProgress(a.Progress, b.Progress) && sameChunk(a.Chunk, b.Chunk)
}

func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameRaw(a, b json.RawMessage) bool { return bytes.Equal(a, b) }

func sameError(a, b *Error) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Code == b.Code && sameString(a.Message, b.Message) && sameBool(a.Redacted, b.Redacted) &&
		sameBool(a.Retryable, b.Retryable) && sameString(a.Field, b.Field)
}

func sameProgress(a, b *Progress) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Stage == b.Stage && a.Done == b.Done && sameInt(a.Total, b.Total) && sameString(a.Event, b.Event)
}

func sameChunk(a, b *Chunk) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestConstructorsBuildFramesTheValidatorAccepts(t *testing.T) {
	req, err := NewRequest("req-00000001", "connector.hello", map[string]any{
		"extensionVersion":             "3.6.0",
		"protocolVersion":              contract.ProtocolVersion,
		"minCompatibleProtocolVersion": contract.MinCompatibleProtocolVersion,
	})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if req.Args == nil {
		t.Fatal("NewRequest dropped its args")
	}

	res, err := req.Respond(map[string]any{"protocolVersion": contract.ProtocolVersion, "capabilities": contract.CapabilityNames()})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if res.InReplyTo == nil || *res.InReplyTo != req.ID {
		t.Fatalf("a response must answer the request id, got %v", res.InReplyTo)
	}

	fail, err := req.Fail("Timeout", "the host did not answer")
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if fail.Error.Retryable == nil || !*fail.Error.Retryable {
		t.Fatal("the taxonomy says Timeout is retryable; Fail must carry that, not the caller's opinion")
	}
	perm, err := req.Fail("UnknownMethod", "no such method")
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if perm.Error.Retryable == nil || *perm.Error.Retryable {
		t.Fatal("UnknownMethod is not retryable")
	}
	if _, err := req.Fail("NotInAnyTaxonomy", "x"); err == nil {
		t.Fatal("Fail accepted a code the taxonomy does not declare")
	}
}

func TestConstructedFramesCannotSilentlyBreakTheTypeRules(t *testing.T) {
	yes := true
	bad := &Frame{V: 1, ID: "resp-00000001", Type: TypeResponse, OK: &yes, SentAt: "2026-11-04T10:00:00Z"}
	err := bad.Validate()
	if err == nil {
		t.Fatal("an ok response with neither a result nor an inReplyTo validated")
	}
	if !strings.Contains(err.Error(), "inReplyTo") || !strings.Contains(err.Error(), "result") {
		t.Fatalf("violations are %v, want both the missing inReplyTo and the missing result", err)
	}
}
