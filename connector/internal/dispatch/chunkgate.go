// The chunk gate: how the front door serves the slices of a chunked profile.import.
//
// The contract's answer is recorded, not invented — packages/schema/fixtures/protocol/import-chunked.jsonl
// shows a group whose intermediate slices are answered by progress frames (stage "reassembling", done/total,
// in reply to the group's own envelope.id) and whose final answer is one response frame. The gate reproduces
// exactly that: a slice frame never reaches a handler and never enters the response cache; the fragment is
// recorded here, and the last declared slice reassembles, verifies, and re-enters the normal
// reservation-and-handler path with the document as its args.
//
// The group is one request for every gate that counts:
//   - the named profile.import budget (2/60 s) is spent once, when the group opens with slice 0 — the budget's
//     own why ("a second import inside a minute is either a retry of the first or a bug") counts imports, and
//     a chunked group is one import;
//   - every slice, re-asked slice and the final slice pay only the catch-all floor;
//   - the response-cache answer lives under the group id, so any later frame with that id replays the final
//     answer the existing dedupe already guarantees.
//
// A slice that would break the group — one that arrives out of chunkSeq order, declares a different
// contentHash or chunkCount, or does not decode — is refused and the whole group is dropped: a gap fails the
// whole import rather than partially applying (framing.json#/oversizedPayloads/mechanism/ordering). A group
// that sits without slices past the command's own timeout is dropped silently; a later slice is answered as
// a gap, and the sender re-sends the whole import under a fresh id.
package dispatch

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/k2dms/nexora/connector/internal/chunk"
	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
)

// reassembledCapBytes is the ceiling on a group's reassembled size: framing.json#/oversizedPayloads/mechanism/cap
// bounds the total by subscription-source limits.maxBytes, "so chunking cannot be used to smuggle an arbitrarily
// large payload past the size guard" (docs/architecture/25-native-messaging-protocol.md §5 rule 8). The number is
// the schema's own maximum, asserted as data by the gate's test rather than trusted here.
const reassembledCapBytes = 8388608

// ChunkProgress is a slice frame's answer: reassembly state in reply to the group id. The service layer turns it
// into a progress frame; it is never a cached response, so no field of it may look like a result.
type ChunkProgress struct {
	Stage string
	Done  int
	Total int
}

// openChunk is one group the gate is reassembling. It lives only while slices are still arriving: the final
// slice drops it the moment the document is in the handler's hands, and silence past the command timeout
// drops it without an answer (a silent expiry is a liveness fact, not a frame to answer).
type openChunk struct {
	r      *chunk.Reassembler
	opened time.Time
	bytes  int
}

// chunkKey scopes a group to its sender: two senders may legally reuse an id, and the response cache they
// share the key space with is already per sender.
func chunkKey(senderID, requestID string) string { return senderID + "\x1f" + requestID }

// chunkOpen reports whether a group is open for this sender and id. Dispatch asks before serving a
// whole-frame request: a frame without chunk that arrives on an id a group is using would cache its answer
// under the group's id and later replay it as the group's final answer, so it is refused instead.
func (d *Dispatcher) chunkOpen(senderID, requestID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.groups[chunkKey(senderID, requestID)]
	return ok
}

// dispatchChunk serves one chunk slice. It returns the slice's answer — a ChunkProgress for every slice but
// the last, and the command's own result for the slice that completes the group.
func (d *Dispatcher) dispatchChunk(ctx context.Context, id Identity, f *envelope.Frame, m contract.Method, handler Handler) (any, *Refusal) {
	// A completed group's answer is a cached response like any other: a re-sent slice replays it, and so
	// does a whole-frame request that carries the same id. This peek must not reserve an in-flight slot —
	// slices that only record fragments have no reservation to hold.
	if replay, r := d.cachedReplay(id, f.ID, m); r != nil || replay != nil {
		if r != nil {
			return nil, r
		}
		if r := d.charge(id, m, true); r != nil {
			return nil, r
		}
		return replay, nil
	}

	c := f.Chunk
	if c.Encoding != "base64" {
		return nil, d.refuse(id, "BadRequest",
			fmt.Sprintf("chunk encoding %q names no arrangement this receiver reassembles: the fragment model is base64 (framing.json#/oversizedPayloads/mechanism/sliceBody)", c.Encoding))
	}
	if m.Name != "profile.import" {
		return nil, d.refuse(id, "BadRequest",
			fmt.Sprintf("%s is not a chunked sender: envelope.chunk is the oversized-payload mechanism of profile.import alone (envelope.schema.json#/properties/chunk)", m.Name))
	}

	key := chunkKey(id.SenderID, f.ID)
	d.mu.Lock()
	d.expireChunkLocked(d.now())
	progress, final, g, r := d.admitSliceLocked(id, key, d.groups[key], m, c, f)
	d.mu.Unlock()
	if r != nil {
		return nil, r
	}
	if !final {
		return progress, nil
	}

	// The last declared slice: reassemble, verify, and answer the way the normal path does — a reservation
	// a concurrent retry can wait on, the catch-all floor only (the named budget went out with slice 0),
	// and the encoded answer cached under the group id. Reassembly runs outside the lock because a
	// completed group is never mutated again: every sequence is recorded, so no further Add can land.
	reassembled, r := reassemble(g)
	if r != nil {
		d.dropChunk(key)
		return nil, r
	}
	doc, r := reassembledArgs(reassembled)
	if r != nil {
		d.dropChunk(key)
		return nil, r
	}
	sub, err := reassembledFrame(f, doc)
	if err != nil {
		d.dropChunk(key)
		return nil, &Refusal{Code: "Internal", Message: "the reassembled document did not frame as a request"}
	}

	entry, replay, rr := d.beginResponse(ctx, id, f.ID, m)
	if rr != nil {
		d.dropChunk(key)
		return nil, rr
	}
	var encoded json.RawMessage
	if entry != nil {
		defer func(requestID string) {
			d.completeResponse(id.SenderID, requestID, entry, encoded)
			d.dropChunk(key)
		}(f.ID)
	}
	if r := d.charge(id, m, true); r != nil {
		return nil, r
	}
	if replay != nil {
		return replay, nil
	}
	result, refusal := handler(ctx, sub)
	if entry != nil && refusal == nil {
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, &Refusal{Code: "Internal", Message: "handler result could not be encoded"}
		}
		encoded = json.RawMessage(raw)
	}
	return result, refusal
}

// admitSliceLocked checks the slice against its group, spends what the slice costs, and records the fragment.
// It is called with d.mu held. progress is nil exactly when the slice is the group's last, and final is true
// only for a slice whose recording leaves the group ready to reassemble; final returns the group, whose
// reassembly the caller runs outside the lock.
func (d *Dispatcher) admitSliceLocked(id Identity, key string, g *openChunk, m contract.Method, c *envelope.Chunk, f *envelope.Frame) (*ChunkProgress, bool, *openChunk, *Refusal) {
	switch {
	case g == nil && c.ChunkSeq != 0:
		// A gap: no group opened with this id, so whatever this slice belongs to can never complete.
		// framing.json names the answer and so does the recorded split-group fixture.
		return nil, false, nil, d.refuseLocked(id, "IntegrityMismatch",
			fmt.Sprintf("chunkSeq %d arrived with no group opened under %q: a gap fails the whole import, and the sender re-sends it whole under a fresh id", c.ChunkSeq, f.ID))

	case g == nil:
		// Opening the group is the one act that counts against the import budget: it is the budget's own
		// unit ("a second import inside a minute is either a retry of the first or a bug"), and it is what
		// keeps a sender from holding unbounded reassembly buffers open. The floor is spent here too — the
		// opening slice is answered like any other.
		if r := d.chargeLocked(id, m, false); r != nil {
			return nil, false, nil, r
		}
		re, err := chunk.New(c.ContentHash, c.ChunkCount)
		if err != nil {
			return nil, false, nil, d.refuseLocked(id, "BadRequest", "profile.import: "+err.Error())
		}
		size, r := fragmentLen(f)
		if r != nil {
			return nil, false, nil, r
		}
		if size > reassembledCapBytes {
			return nil, false, nil, d.refuseLocked(id, "BadRequest",
				fmt.Sprintf("slice 0 alone would reassemble more than the %d bytes subscription-source limits.maxBytes bounds a group to", reassembledCapBytes))
		}
		if r := addFragment(id, re, f, 0); r != nil {
			return nil, false, nil, r
		}
		g = &openChunk{r: re, opened: d.now(), bytes: size}
		d.groups[key] = g
		return &ChunkProgress{Stage: "reassembling", Done: re.Arrived(), Total: re.Count()}, false, nil, nil
	}

	// Agreement: one contentHash and one chunkCount per group. Either disagreement is the recorded
	// split-group shape, answered IntegrityMismatch with the whole import dropped.
	if g.r.Hash() != c.ContentHash || g.r.Count() != c.ChunkCount {
		delete(d.groups, key)
		return nil, false, nil, d.refuseLocked(id, "IntegrityMismatch",
			fmt.Sprintf("slice %d disagrees with the group it arrived on: the group opened with contentHash %s over %d slices, this frame declares %s over %d",
				c.ChunkSeq, g.r.Hash(), g.r.Count(), c.ContentHash, c.ChunkCount))
	}

	// Order: chunkSeq must name the next unrecorded slice, or a slice the group already holds. A jump is
	// the gap rule; a step backwards is a re-asked slice, answered from state and never recorded twice.
	arrived := g.r.Arrived()
	if c.ChunkSeq > arrived {
		delete(d.groups, key)
		return nil, false, nil, d.refuseLocked(id, "IntegrityMismatch",
			fmt.Sprintf("chunkSeq %d arrived before chunkSeq %d was recorded: chunks of one contentHash arrive in chunkSeq order, and a gap fails the whole import", c.ChunkSeq, arrived))
	}

	// Every answer that is not the group's last slice pays the catch-all floor — a newly recorded
	// intermediate slice and a re-asked one pay the same for the same answer. The last slice's floor goes
	// out on the reservation path, beside the handler it pays for.
	if c.ChunkSeq != c.ChunkCount-1 {
		if r := d.chargeLocked(id, m, true); r != nil {
			return nil, false, nil, r
		}
	}
	if c.ChunkSeq == arrived {
		// A fragment the group has not recorded yet: bounded first, recorded second, so an over-cap slice
		// never enters the buffer it would overflow.
		size, r := fragmentLen(f)
		if r != nil {
			delete(d.groups, key)
			return nil, false, nil, r
		}
		if g.bytes+size > reassembledCapBytes {
			delete(d.groups, key)
			return nil, false, nil, d.refuseLocked(id, "BadRequest",
				fmt.Sprintf("recording slice %d would reassemble more than the %d bytes subscription-source limits.maxBytes bounds a group to", c.ChunkSeq, reassembledCapBytes))
		}
		if r := addFragment(id, g.r, f, c.ChunkSeq); r != nil {
			delete(d.groups, key)
			return nil, false, nil, r
		}
		g.bytes += size
	}

	if c.ChunkSeq == c.ChunkCount-1 {
		return nil, true, g, nil
	}
	return &ChunkProgress{Stage: "reassembling", Done: g.r.Arrived(), Total: c.ChunkCount}, false, nil, nil
}

// reassemble verifies the completed group against the hash the group was opened with, before anything sees a
// handler; a mismatch is the whole import's failure.
func reassemble(g *openChunk) ([]byte, *Refusal) {
	all, err := g.r.Bytes()
	if err != nil {
		return nil, &Refusal{Code: "IntegrityMismatch", Message: "profile.import: " + err.Error()}
	}
	return all, nil
}

// reassembledArgs turns the verified bytes into the args the handler applies: a JSON object carrying none of
// the four members a chunked import must not carry — the payload description names the shape, and the fourth
// allOf clause is the reason credentials never ride a chunked frame (the staging buffer is where DPAPI does
// not reach).
func reassembledArgs(reassembled []byte) (json.RawMessage, *Refusal) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(reassembled, &doc); err != nil {
		return nil, &Refusal{Code: "BadRequest", Message: "profile.import: the reassembled document is not a JSON object"}
	}
	for _, k := range []string{"chunk", "payload", "contentHash", "credentials"} {
		if _, present := doc[k]; present {
			return nil, &Refusal{Code: "BadRequest",
				Message: fmt.Sprintf("profile.import: the reassembled document carries %q, and a chunked import carries none of chunk, payload, contentHash or credentials", k)}
		}
	}
	return json.RawMessage(reassembled), nil
}

// reassembledFrame is the request the handler answers: the group's own id, method and trace with the document
// as args, so the answer's inReplyTo names the group and the validate-then-apply path is the single-frame
// path — one schema, one handler, one idempotency record (ADR-0008).
func reassembledFrame(f *envelope.Frame, doc json.RawMessage) (*envelope.Frame, error) {
	sub := &envelope.Frame{
		V: f.V, ID: f.ID, Type: envelope.TypeRequest, Method: f.Method,
		Args: append(json.RawMessage(nil), doc...), TraceID: f.TraceID, SentAt: f.SentAt,
	}
	if err := sub.Validate(); err != nil {
		return nil, err
	}
	return sub, nil
}

// dropChunk removes a group by key. Groups end three ways — the last slice hands the document over, a slice
// disagreement drops the whole import, or the group outlives its command timeout — and all three end here.
func (d *Dispatcher) dropChunk(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.groups, key)
}

// expireChunkLocked drops groups that stopped receiving slices past the command's own timeout. The command
// whose slices these are has a deadline; a group that cannot meet it is not a transfer in progress but a
// stalled buffer, and no frame answers a silent expiry.
func (d *Dispatcher) expireChunkLocked(at time.Time) {
	m, _ := contract.LookupMethod("profile.import")
	for key, g := range d.groups {
		if at.Sub(g.opened) > time.Duration(m.TimeoutMs)*time.Millisecond {
			delete(d.groups, key)
		}
	}
}

// cachedReplay is the slice-path view of the response cache: the answer a completed group left under its id,
// returned without creating a reservation. An in-flight entry (a final slice whose handler is running) is
// invisible here — that retry is served by the reservation path's wait, not by a peek.
func (d *Dispatcher) cachedReplay(id Identity, requestID string, m contract.Method) (json.RawMessage, *Refusal) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cache := d.responses[id.SenderID]
	if cache == nil {
		return nil, nil
	}
	d.expireResponsesLocked(cache, d.now())
	entry := cache.entries[requestID]
	if entry == nil || entry.result == nil {
		return nil, nil
	}
	if entry.method != m.Name {
		return nil, d.refuseLocked(id, "BadRequest", "request id is already bound to another method")
	}
	return append(json.RawMessage(nil), entry.result...), nil
}

// fragmentLen reports the decoded size of a slice frame's args.payload. Base64 length maps to byte length
// deterministically, so the cap check runs before the group records anything; a payload that is not base64
// is caught by the recording that follows, never here.
func fragmentLen(f *envelope.Frame) (int, *Refusal) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &doc); err != nil {
		return 0, &Refusal{Code: "BadRequest", Message: "profile.import: slice args are not a JSON object"}
	}
	raw, ok := doc["payload"]
	if !ok || len(raw) < 2 {
		return 0, &Refusal{Code: "BadRequest", Message: "profile.import: a chunk slice carries its fragment in args.payload, and this frame carries none"}
	}
	var payload string
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, &Refusal{Code: "BadRequest", Message: "profile.import: args.payload is not a string"}
	}
	return base64.StdEncoding.DecodedLen(len(payload)), nil
}

// addFragment records one fragment, naming the group rules the recording can break: a fragment that does not
// decode is refused with the frame it came from still answerable by BadRequest.
func addFragment(id Identity, re *chunk.Reassembler, f *envelope.Frame, seq int) *Refusal {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &doc); err != nil {
		return &Refusal{Code: "BadRequest", Message: "profile.import: slice args are not a JSON object"}
	}
	var payload string
	if err := json.Unmarshal(doc["payload"], &payload); err != nil {
		return &Refusal{Code: "BadRequest", Message: "profile.import: args.payload is not a string"}
	}
	if err := re.Add(seq, payload); err != nil {
		return &Refusal{Code: "BadRequest", Message: "profile.import: " + err.Error()}
	}
	return nil
}
