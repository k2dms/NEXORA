// Package frames implements the Native Messaging wire framing the contract declares in
// packages/schema/protocol/framing.json: a four-byte unsigned little-endian length prefix, then exactly one
// UTF-8 JSON object.
//
// The codec owns the byte limits and nothing else — no envelope semantics, no retries, no queueing. Both
// directions pass the limit they want: the extension-to-host number is enforced by the *sender* before a byte
// is written, because a receiver-side check means the frame was already lost.
package frames

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// PrefixBytes is the width of the length prefix declared in
// packages/schema/protocol/framing.json#/encoding/lengthPrefix.
const PrefixBytes = 4

// ErrClosed reports that the peer closed the pipe cleanly at a frame boundary. It is not an error condition
// for a host or a service shutting down a connection; a reader loop treats it as end of stream.
var ErrClosed = errors.New("frames: peer closed the pipe")

// ErrEmpty reports a zero-length payload. A frame is one JSON object, so zero bytes is never legal — and a
// reader that accepted one would desynchronise the stream.
var ErrEmpty = errors.New("frames: empty payload")

// ErrTruncated reports a frame that ended before its declared length. The stream is desynchronised from here;
// the caller must close the connection rather than read another frame.
var ErrTruncated = errors.New("frames: payload ended before its declared length")

// TooLargeError reports a payload outside the limit for its direction. On the write side it is returned
// before any byte reaches the pipe; on the read side the length prefix has been consumed, so the caller must
// treat the connection as broken instead of skipping the frame.
type TooLargeError struct {
	Bytes int
	Limit int
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("frames: payload of %d B exceeds the %d B limit", e.Bytes, e.Limit)
}

// Read returns the payload of the next frame.
func Read(r io.Reader, limit int) ([]byte, error) {
	var hdr [PrefixBytes]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, ErrClosed
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("frames: truncated length prefix: %w", err)
		}
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	switch {
	case n == 0:
		return nil, ErrEmpty
	case int(n) > limit:
		return nil, &TooLargeError{Bytes: int(n), Limit: limit}
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTruncated, err)
	}
	return payload, nil
}

// Write emits one frame. It refuses a payload the receiving side cannot accept before writing, and writes the
// prefix and the payload in a single call so an interleaved writer cannot splice two frames together.
func Write(w io.Writer, payload []byte, limit int) error {
	if len(payload) == 0 {
		return ErrEmpty
	}
	if len(payload) > limit {
		return &TooLargeError{Bytes: len(payload), Limit: limit}
	}
	buf := make([]byte, PrefixBytes+len(payload))
	binary.LittleEndian.PutUint32(buf[:PrefixBytes], uint32(len(payload)))
	copy(buf[PrefixBytes:], payload)
	_, err := w.Write(buf)
	return err
}
