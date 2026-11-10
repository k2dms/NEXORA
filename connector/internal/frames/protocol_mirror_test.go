package frames

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contract"
)

// mirrorFixture is the frames the TypeScript codec wrote for the corpus's own protocol fixtures, one JSON record
// per frame.
type mirrorFixture struct {
	File         string `json:"file"`
	Frame        int    `json:"frame"`
	Direction    string `json:"direction"`
	PayloadBytes int    `json:"payloadBytes"`
	PrefixHex    string `json:"prefixHex"`
	PayloadB64   string `json:"payloadB64"`
}

// TestTheTypeScriptCodecFramesTheSamePayloads reads packages/protocol/testdata/framed.jsonl and requires this
// package to write those exact bytes and to read them back. docs/architecture/25-native-messaging-protocol.md §2
// says a bug in the codec is a bug on both sides; that is a sentence until one implementation is compared against
// the other's output rather than against its own re-derivation.
func TestTheTypeScriptCodecFramesTheSamePayloads(t *testing.T) {
	path := filepath.Join("..", "..", "..", "packages", "protocol", "testdata", "framed.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("the TypeScript codec's mirror fixture is not readable: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	seen := 0
	for scanner.Scan() {
		var record mirrorFixture
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("mirror record %d: %v", seen, err)
		}
		payload, err := base64.StdEncoding.DecodeString(record.PayloadB64)
		if err != nil {
			t.Fatalf("%s frame %d: payload is not base64: %v", record.File, record.Frame, err)
		}
		prefix, err := hex.DecodeString(record.PrefixHex)
		if err != nil {
			t.Fatalf("%s frame %d: prefix is not hex: %v", record.File, record.Frame, err)
		}
		if len(prefix) != PrefixBytes {
			t.Fatalf("%s frame %d: the TypeScript prefix is %d bytes, the contract says %d", record.File, record.Frame, len(prefix), PrefixBytes)
		}
		if len(payload) != record.PayloadBytes {
			t.Fatalf("%s frame %d: the record declares %d payload bytes and carries %d", record.File, record.Frame, record.PayloadBytes, len(payload))
		}
		limit := mirrorLimit(t, record.Direction)

		var buf bytes.Buffer
		if err := Write(&buf, payload, limit); err != nil {
			t.Fatalf("%s frame %d: this codec refused a frame the TypeScript codec wrote: %v", record.File, record.Frame, err)
		}
		want := append(append([]byte{}, prefix...), payload...)
		if !bytes.Equal(buf.Bytes(), want) {
			t.Fatalf("%s frame %d: the two codecs disagree about the frame: got % x want % x", record.File, record.Frame, buf.Bytes(), want)
		}
		back, err := Read(bytes.NewReader(want), limit)
		if err != nil {
			t.Fatalf("%s frame %d: this codec could not read the TypeScript frame back: %v", record.File, record.Frame, err)
		}
		if !bytes.Equal(back, payload) {
			t.Fatalf("%s frame %d: reading the TypeScript frame changed the payload", record.File, record.Frame)
		}
		seen++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("mirror fixture: %v", err)
	}
	if seen == 0 {
		t.Fatal("the mirror fixture carried no frames, so the comparison proved nothing")
	}
	t.Logf("%d frames written and read byte-for-byte as the TypeScript codec framed them", seen)
}

// mirrorLimit is the ceiling the direction's own contract number gives: 1 MiB out of the extension, 1 GiB into it.
func mirrorLimit(t *testing.T, direction string) int {
	t.Helper()
	switch direction {
	case "extension-to-host":
		return contract.ExtensionToHostBytes
	case "host-to-extension":
		return contract.HostToExtensionBytes
	default:
		t.Fatalf("the mirror fixture names a direction this test was not told about: %q", direction)
		return 0
	}
}
