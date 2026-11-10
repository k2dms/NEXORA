package frames

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contract"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte(`{"v":1,"id":"req-00000001","type":"request","method":"connector.hello","sentAt":"2026-11-04T10:00:00Z"}`)
	if err := Write(&buf, payload, contract.EffectiveCrossBrowserBytes); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(&buf, contract.EffectiveCrossBrowserBytes)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round trip changed the payload: got %s want %s", got, payload)
	}
}

// TestPrefixIsLittleEndian pins the four header bytes the contract names: length of the payload only,
// unsigned, little-endian. A big-endian host would read 16 MiB for a 16-byte frame.
func TestPrefixIsLittleEndian(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, []byte(`{"a":1}`), 1024); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := []byte{7, 0, 0, 0}
	if got := buf.Bytes()[:PrefixBytes]; !bytes.Equal(got, want) {
		t.Fatalf("prefix is %v, want %v", got, want)
	}
}

// TestWriteRefusesOversizeBeforeTouchingThePipe is the sender-side rule of
// packages/schema/protocol/framing.json#/limits/extensionToHostBytes/enforcement: the rejection has to happen
// before a byte is written, because a receiver-side check means the frame is already lost.
func TestWriteRefusesOversizeBeforeTouchingThePipe(t *testing.T) {
	var buf bytes.Buffer
	limit := 64
	payload := bytes.Repeat([]byte("x"), limit+1)
	err := Write(&buf, payload, limit)
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Write returned %v, want *TooLargeError", err)
	}
	if tooLarge.Bytes != limit+1 || tooLarge.Limit != limit {
		t.Fatalf("TooLargeError carries %d/%d, want %d/%d", tooLarge.Bytes, tooLarge.Limit, limit+1, limit)
	}
	if buf.Len() != 0 {
		t.Fatalf("the codec wrote %d bytes while refusing the frame; a refused frame must cost the pipe nothing", buf.Len())
	}
}

func TestReadRefusesOversizeFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := binaryWrite(&buf, 100, bytes.Repeat([]byte("y"), 100)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := Read(&buf, 64)
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Read returned %v, want *TooLargeError", err)
	}
}

func TestReadTreatsACleanCloseAsEndOfStream(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	if _, err := Read(buf, 1024); !errors.Is(err, ErrClosed) {
		t.Fatalf("Read on an empty pipe returned %v, want ErrClosed", err)
	}
}

func TestReadDetectsATruncatedLengthPrefix(t *testing.T) {
	buf := bytes.NewBuffer([]byte{4, 0})
	if _, err := Read(buf, 1024); err == nil || errors.Is(err, ErrClosed) {
		t.Fatalf("Read on a two-byte prefix returned %v, want an error that is not ErrClosed", err)
	}
}

func TestReadDetectsATruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := binaryWrite(&buf, 32, []byte("only a part of it")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Read(&buf, 1024); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Read returned %v, want ErrTruncated", err)
	}
}

func TestEmptyPayloadIsNeverAFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := binaryWrite(&buf, 0, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Read(&buf, 1024); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Read on a zero-length frame returned %v, want ErrEmpty", err)
	}
	if err := Write(io.Discard, nil, 1024); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Write of an empty payload returned %v, want ErrEmpty", err)
	}
}

// TestContractLimitsAreTheOrderTheDocumentClaims guards the projection the codec is pointed at: the effective
// cross-browser cap is the binding one, and the direction that binds it is host-to-extension, because that is the
// cap every browser family states.
func TestContractLimitsAreTheOrderTheDocumentClaims(t *testing.T) {
	if !(contract.EffectiveCrossBrowserBytes <= contract.HostToExtensionBytes &&
		contract.HostToExtensionBytes <= contract.ExtensionToHostBytes) {
		t.Fatalf("limits are out of order: %d <= %d <= %d is what framing.json declares",
			contract.EffectiveCrossBrowserBytes, contract.HostToExtensionBytes, contract.ExtensionToHostBytes)
	}
	var buf bytes.Buffer
	if err := Write(&buf, bytes.Repeat([]byte("z"), contract.EffectiveCrossBrowserBytes), contract.EffectiveCrossBrowserBytes); err != nil {
		t.Fatalf("a frame at exactly the cap must be writable: %v", err)
	}
	if err := Write(&buf, bytes.Repeat([]byte("z"), contract.EffectiveCrossBrowserBytes+1), contract.EffectiveCrossBrowserBytes); err == nil {
		t.Fatal("a frame one byte above the cap must be refused")
	}
}

func binaryWrite(w io.Writer, length int, payload []byte) error {
	var hdr [PrefixBytes]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(length))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}
