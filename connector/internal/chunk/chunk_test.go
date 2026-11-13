package chunk

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func fragment(t *testing.T, data []byte, parts int) (hash string, chunks []string) {
	t.Helper()
	size := (len(data) + parts - 1) / parts
	for i := 0; i < parts; i++ {
		lo := i * size
		hi := lo + size
		if hi > len(data) {
			hi = len(data)
		}
		chunks = append(chunks, base64.StdEncoding.EncodeToString(data[lo:hi]))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), chunks
}

func TestReassemblyMatchesDeclaredHash(t *testing.T) {
	doc := []byte(`{"profiles":[],"servers":[],"mode":"merge"}`)
	hash, chunks := fragment(t, doc, 3)

	r, err := New(hash, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r.Complete() {
		t.Fatal("a fresh group is not complete")
	}
	// Out-of-order arrival is the normal transport case; the contract orders the
	// concatenation by chunkSeq, not by arrival.
	for _, seq := range []int{2, 0, 1} {
		if err := r.Add(seq, chunks[seq]); err != nil {
			t.Fatalf("Add(%d): %v", seq, err)
		}
	}
	if !r.Complete() {
		t.Fatal("three of three slices is complete")
	}
	got, err := r.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(got, doc) {
		t.Fatalf("reassembled %q, want %q", got, doc)
	}
}

func TestHashMismatchIsIntegrityMismatch(t *testing.T) {
	doc := []byte(`{"profiles":[]}`)
	_, chunks := fragment(t, doc, 2)

	other := make([]byte, len(doc))
	copy(other, doc)
	other[0] = '[' // one flipped byte is enough; the shape stays one plausible JSON document
	otherSum := sha256.Sum256(other)
	wrongHash := hex.EncodeToString(otherSum[:])

	r, err := New(wrongHash, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for seq, c := range chunks {
		if err := r.Add(seq, c); err != nil {
			t.Fatalf("Add(%d): %v", seq, err)
		}
	}
	_, err = r.Bytes()
	if !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("Bytes error = %v, want ErrIntegrityMismatch", err)
	}
	if !strings.Contains(err.Error(), wrongHash[:8]) {
		t.Fatalf("error should name the declared hash: %v", err)
	}
}

func TestIncompleteRefusesBytes(t *testing.T) {
	doc := []byte(`{"profiles":[]}`)
	hash, chunks := fragment(t, doc, 2)

	r, _ := New(hash, 2)
	if err := r.Add(0, chunks[0]); err != nil {
		t.Fatalf("Add(0): %v", err)
	}
	if _, err := r.Bytes(); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Bytes before every slice = %v, want ErrIncomplete", err)
	}
}

func TestDuplicateAndOutOfRangeSequences(t *testing.T) {
	hash, chunks := fragment(t, []byte(`{"profiles":[]}`), 2)
	r, _ := New(hash, 2)
	if err := r.Add(0, chunks[0]); err != nil {
		t.Fatalf("Add(0): %v", err)
	}
	if err := r.Add(0, chunks[0]); err == nil {
		t.Fatal("a duplicate chunkSeq must be refused")
	}
	if err := r.Add(2, chunks[0]); err == nil {
		t.Fatal("chunkSeq beyond the declared count must be refused")
	}
	if err := r.Add(-1, chunks[0]); err == nil {
		t.Fatal("a negative chunkSeq must be refused")
	}
}

func TestNonBase64PayloadRefused(t *testing.T) {
	r, _ := New(strings.Repeat("a", 64), 2)
	if err := r.Add(0, "not!!base64@@"); err == nil {
		t.Fatal("a fragment that is not base64 must be refused")
	}
}

func TestNewRefusesSingleSliceAndBadHash(t *testing.T) {
	if _, err := New(strings.Repeat("a", 64), 1); err == nil {
		t.Fatal("chunkCount 1 is not a chunked transfer")
	}
	if _, err := New("zz", 2); err == nil {
		t.Fatal("a non-hex contentHash must be refused")
	}
	if _, err := New(strings.Repeat("a", 63), 2); err == nil {
		t.Fatal("a 63-character contentHash must be refused")
	}
}
