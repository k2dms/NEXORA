// Package chunk reassembles the slice bodies of a chunked profile.import.
//
// The fragment model the contract records (docs/architecture/25-native-messaging-protocol.md
// §5, protocol/framing.json#/oversizedPayloads/mechanism/sliceBody): every slice
// carries its fragment as base64 in args.payload, and chunk.contentHash is sha256
// over the concatenation of the decoded fragments of one group in chunkSeq order.
// The reassembled bytes are the whole import document; a reassembly whose hash
// differs from the declared one is an IntegrityMismatch, never a partial apply.
package chunk

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrIntegrityMismatch is the sentinel the caller maps to the protocol error
// code: the reassembled bytes are not the document the sender declared.
var ErrIntegrityMismatch = errors.New("reassembled payload hash differs from the declared contentHash")

// ErrIncomplete is returned by Bytes before every declared slice has arrived.
// It is a liveness state, not an error the caller reports mid-transfer.
var ErrIncomplete = errors.New("chunk group has not delivered every slice it declared")

// Reassembler collects the base64 fragments of one chunk group. One group per
// envelope.id plus contentHash; a second hash on the same group is a caller
// concern (it opens a fresh Reassembler and the transfer fails).
type Reassembler struct {
	hash    string
	count   int
	parts   [][]byte
	arrived int
	// indexed by seq; true once that slice has arrived
	declared []bool
}

// New opens a group that declared count slices hashing to contentHash. A
// single-slice transfer is not a chunked transfer and is refused here the way
// the schema refuses it (chunkCount >= 2).
func New(contentHash string, count int) (*Reassembler, error) {
	if count < 2 {
		return nil, fmt.Errorf("chunkCount %d: a single-slice payload is not a chunked transfer", count)
	}
	if _, err := hex.DecodeString(contentHash); err != nil || len(contentHash) != 64 {
		return nil, fmt.Errorf("contentHash %q is not sha256 hex", contentHash)
	}
	return &Reassembler{
		hash:     contentHash,
		count:    count,
		parts:    make([][]byte, count),
		declared: make([]bool, count),
	}, nil
}

// Add records one slice: seq in 0..count-1, payload the base64 fragment of the
// serialized import document. Duplicate and out-of-range sequences are refused;
// a fragment that does not decode is refused with the frame it came from still
// answerable by BadRequest.
func (r *Reassembler) Add(seq int, payload string) error {
	if seq < 0 || seq >= r.count {
		return fmt.Errorf("chunkSeq %d outside 0..%d", seq, r.count-1)
	}
	if r.declared[seq] {
		return fmt.Errorf("chunkSeq %d arrived twice", seq)
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("payload of slice %d is not base64: %w", seq, err)
	}
	r.parts[seq] = raw
	r.declared[seq] = true
	r.arrived++
	return nil
}

// Complete reports whether every declared slice has arrived.
func (r *Reassembler) Complete() bool { return r.arrived == r.count }

// Bytes returns the reassembled document once the group is complete, verified
// against the hash declared at New. A hash mismatch is ErrIntegrityMismatch
// wrapping the two hashes, and the caller rejects the whole import.
func (r *Reassembler) Bytes() ([]byte, error) {
	if !r.Complete() {
		return nil, fmt.Errorf("%w: %d of %d slices", ErrIncomplete, r.arrived, r.count)
	}
	total := 0
	for _, p := range r.parts {
		total += len(p)
	}
	all := make([]byte, 0, total)
	for _, p := range r.parts {
		all = append(all, p...)
	}
	sum := sha256.Sum256(all)
	got := hex.EncodeToString(sum[:])
	if got != r.hash {
		return nil, fmt.Errorf("%w: got sha256 %s, declared %s", ErrIntegrityMismatch, got, r.hash)
	}
	return all, nil
}
