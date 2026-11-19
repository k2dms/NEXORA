package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/envelope"
)

// TestAChunkedImportIsAnsweredByProgressFramesAndOneResponse runs the decided slice through the service's own
// respond path: the recorded fixture's answer shape — progress frames between the slices, one response for the
// group — is what the Connector actually sends.
func TestAChunkedImportIsAnsweredByProgressFramesAndOneResponse(t *testing.T) {
	cfg := jsonlConfig(t, true)
	cfg.protector = &importTestProtector{}
	s, err := newService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.respond(mustParse(t, requestLine(t, "seed-hello-01", "connector.hello", recordedHelloArgs))); err != nil {
		t.Fatal(err)
	}
	// The keystore is seeded whole, because a credential never travels on a chunked frame: the reassembled
	// import references the secret the single-frame import put there.
	seed := mustParse(t, requestLine(t, "seed-import-1", "profile.import", stringMustJSON(testImportArgs(false))))
	if answer, err := s.respond(seed); err != nil || answer.OK == nil || !*answer.OK {
		t.Fatal("the seeding import failed")
	}

	doc := testImportArgs(false)
	delete(doc, "credentials")
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []string
	step := (len(raw) + 2) / 3
	for lo := 0; lo < len(raw); lo += step {
		hi := lo + step
		if hi > len(raw) {
			hi = len(raw)
		}
		chunks = append(chunks, base64.StdEncoding.EncodeToString(raw[lo:hi]))
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])

	chunkedLine := func(seq int) string {
		chunk := map[string]any{"contentHash": hash, "chunkSeq": seq, "chunkCount": len(chunks), "encoding": "base64"}
		args := map[string]any{"profiles": []any{}, "mode": "merge", "contentHash": hash, "chunk": chunk, "payload": chunks[seq]}
		argsJSON, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(map[string]any{
			"v": 1, "id": "chunked-imp-01", "type": "request", "method": "profile.import",
			"sentAt": envelope.Now(), "chunk": chunk, "args": json.RawMessage(argsJSON),
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(wire)
	}

	for seq, want := range []int{1, 2} {
		answer, err := s.respond(mustParse(t, chunkedLine(seq)))
		if err != nil {
			t.Fatalf("slice %d failed: %v", seq, err)
		}
		if answer.Type != envelope.TypeProgress {
			t.Fatalf("slice %d was answered with a %s frame, want progress", seq, answer.Type)
		}
		if answer.ID != "chunked-imp-01" || answer.InReplyTo == nil || *answer.InReplyTo != "chunked-imp-01" {
			t.Fatalf("the progress frame does not carry the group id: id=%q inReplyTo=%v", answer.ID, answer.InReplyTo)
		}
		if answer.Progress == nil || answer.Progress.Stage != "reassembling" || answer.Progress.Done != want || answer.Progress.Total == nil || *answer.Progress.Total != len(chunks) {
			t.Fatalf("slice %d was answered with %+v, want reassembling %d/%d", seq, answer.Progress, want, len(chunks))
		}
	}
	final, err := s.respond(mustParse(t, chunkedLine(len(chunks)-1)))
	if err != nil {
		t.Fatalf("the final slice failed: %v", err)
	}
	if final.Type != envelope.TypeResponse || final.OK == nil || !*final.OK {
		t.Fatalf("the final slice was answered %#v", final)
	}
	var result struct {
		Imported struct {
			ProfileIDs []string `json:"profileIds"`
			ServerIDs  []string `json:"serverIds"`
		} `json:"imported"`
	}
	if err := json.Unmarshal(final.Result, &result); err != nil {
		t.Fatalf("the group's answer is not the import result: %v", err)
	}
	if len(result.Imported.ProfileIDs) != 1 || len(result.Imported.ServerIDs) != 1 {
		t.Fatalf("the chunked import imported %#v", result.Imported)
	}
	if strings.Contains(strings.ToLower(string(final.Result)), "keystore://") {
		t.Fatalf("the result leaked a reference: %s", final.Result)
	}
}

// TestAReassembledImportCannotCarryCredentials pins the fourth allOf clause at the receiver: a chunked import
// whose document carries credentials is refused whole, whatever its profiles validate to.
func TestAReassembledImportCannotCarryCredentials(t *testing.T) {
	cfg := jsonlConfig(t, true)
	cfg.protector = &importTestProtector{}
	s, err := newService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.respond(mustParse(t, requestLine(t, "seed-hello-02", "connector.hello", recordedHelloArgs))); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(testImportArgs(false))
	if err != nil {
		t.Fatal(err)
	}
	var chunks []string
	step := (len(raw) + 1) / 2
	for lo := 0; lo < len(raw); lo += step {
		hi := lo + step
		if hi > len(raw) {
			hi = len(raw)
		}
		chunks = append(chunks, base64.StdEncoding.EncodeToString(raw[lo:hi]))
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	chunkedLine := func(seq int) string {
		chunk := map[string]any{"contentHash": hash, "chunkSeq": seq, "chunkCount": len(chunks), "encoding": "base64"}
		args := map[string]any{"profiles": []any{}, "mode": "merge", "contentHash": hash, "chunk": chunk, "payload": chunks[seq]}
		argsJSON, _ := json.Marshal(args)
		wire, _ := json.Marshal(map[string]any{
			"v": 1, "id": "cred-imp-0001", "type": "request", "method": "profile.import",
			"sentAt": envelope.Now(), "chunk": chunk, "args": json.RawMessage(argsJSON),
		})
		return string(wire)
	}
	if answer, err := s.respond(mustParse(t, chunkedLine(0))); err != nil {
		t.Fatal(err)
	} else if answer.Type != envelope.TypeProgress {
		t.Fatalf("slice 0 was answered with a %s frame", answer.Type)
	}
	final, err := s.respond(mustParse(t, chunkedLine(1)))
	if err != nil {
		t.Fatalf("the final slice failed: %v", err)
	}
	if final.OK == nil || *final.OK {
		t.Fatal("a reassembled document carrying credentials was applied")
	}
	if final.Error == nil || final.Error.Code != "BadRequest" {
		t.Fatalf("the refusal was %#v, want BadRequest", final.Error)
	}
}
