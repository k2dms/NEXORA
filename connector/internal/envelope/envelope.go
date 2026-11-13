// Package envelope is the Go half of the frame contract declared in
// packages/schema/protocol/envelope.schema.json: one JSON object per frame, with the required fields, the
// per-field shapes and the four type rules that decide which fields may exist at all.
//
// Parse is strict in the same direction the schema is strict: a field the schema does not declare is a
// violation, not something to ignore, because an unknown field on this pipe means one side was built from a
// different contract. A frame that fails validation is never dispatched and never applied.
//
// The boundary is the schema's own: this package answers "is that a frame?", and only internal/dispatch answers
// "is that a method?". Everything else in the process reads one of those two answers.
package envelope

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
)

// Type is the frame direction, and the discriminator for the type rules.
type Type string

const (
	TypeRequest  Type = "request"
	TypeResponse Type = "response"
	TypeEvent    Type = "event"
	TypeProgress Type = "progress"
)

var (
	reID     = regexp.MustCompile(`^[A-Za-z0-9_-]{8,36}$`)
	reMethod = regexp.MustCompile(`^[a-z]+\.[a-z][A-Za-z0-9]*(\.[a-z][A-Za-z0-9]*)*$`)
	reTrace  = regexp.MustCompile(`^[A-Za-z0-9_-]*$`)
	reHash   = regexp.MustCompile(`^[0-9a-f]{64}$`)

	known = map[string]bool{
		"v": true, "id": true, "type": true, "method": true, "args": true, "result": true,
		"error": true, "inReplyTo": true, "ok": true, "traceId": true, "sentAt": true,
		"seq": true, "progress": true, "chunk": true,
	}

	encodings = map[string]bool{"base64": true, "json": true}
)

// Error is a frame's failure detail. Only code is required, and it must come from the closed taxonomy.
type Error struct {
	Code      string  `json:"code"`
	Message   *string `json:"message,omitempty"`
	Redacted  *bool   `json:"redacted,omitempty"`
	Retryable *bool   `json:"retryable,omitempty"`
	Field     *string `json:"field,omitempty"`
}

// Progress carries completion state on a progress frame and the event name on an event frame.
type Progress struct {
	Stage string  `json:"stage"`
	Done  int     `json:"done"`
	Total *int    `json:"total,omitempty"`
	Event *string `json:"event,omitempty"`
}

// Chunk is one slice of an oversized payload (docs/architecture/25-native-messaging-protocol.md §5). There is
// no chunk id: the group is envelope.id plus contentHash.
type Chunk struct {
	ContentHash string `json:"contentHash"`
	ChunkSeq    int    `json:"chunkSeq"`
	ChunkCount  int    `json:"chunkCount"`
	Encoding    string `json:"encoding"`
}

// Frame is one parsed envelope.
type Frame struct {
	V         int
	ID        string
	Type      Type
	Method    *string
	Args      json.RawMessage
	Result    json.RawMessage
	Error     *Error
	InReplyTo *string
	OK        *bool
	TraceID   *string
	SentAt    string
	Seq       *int
	Progress  *Progress
	Chunk     *Chunk

	// present records which keys the peer actually sent. The type rules are about presence — a response that
	// carries "method": null is still a response wearing a request field — so nil alone cannot answer it.
	present map[string]bool
}

// Violation is one rule the frame broke.
type Violation struct {
	Field  string
	Reason string
}

func (v Violation) String() string { return v.Field + ": " + v.Reason }

// Invalid is the error Parse and Validate return: the complete list of broken rules, never just the first.
// BelowPipeFloor is non-zero when one of the violations is the pipe-floor comparison of
// protocol/framing.json#/versionFloors — a frame whose v carries a parseable id and an
// older framing than this build accepts. That frame is answerable: framing.json#/versionFloors/tooOld
// names VersionMismatch as its answer, in reply to the frame's own id (VER-89).
type Invalid struct {
	Violations     []Violation
	BelowPipeFloor int
	// RejectedID is the id the frame carried, when it parsed — a below-floor frame is answerable in reply
	// to its own id, and Parse returns no frame on error, so the id travels on the error instead.
	RejectedID    string
	RejectedTrace *string
}

func (e *Invalid) Error() string {
	parts := make([]string, len(e.Violations))
	for i, v := range e.Violations {
		parts[i] = v.String()
	}
	return "envelope invalid: " + strings.Join(parts, "; ")
}

// Parse decodes one frame payload and validates it.
func Parse(data []byte) (*Frame, error) {
	return ParseWithFloor(data, contract.MinPipeVersion)
}

// ParseWithFloor runs the same rule set at an explicit pipe floor. Parse is this function at the build's
// contract floor; the explicit form exists so a test can prove the below-floor branch the day's floor of 1
// otherwise subsumes behind the 1..255 bound (VER-89).
func ParseWithFloor(data []byte, floor int) (*Frame, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, &Invalid{Violations: []Violation{{Field: "(frame)", Reason: "a frame is one JSON object, never an array, a scalar or a newline-delimited stream"}}}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, &Invalid{Violations: []Violation{{Field: "(frame)", Reason: "malformed JSON: " + err.Error()}}}
	}

	var v []Violation
	add := func(field, reason string) { v = append(v, Violation{Field: field, Reason: reason}) }
	belowFloor := 0

	f := &Frame{present: map[string]bool{}}
	for k := range raw {
		f.present[k] = true
		if !known[k] {
			add(k, "field is not declared by the envelope schema")
		}
	}

	if r, ok := raw["v"]; !ok {
		add("(frame)", "v is required")
	} else if err := json.Unmarshal(r, &f.V); err != nil {
		add("v", "must be an integer")
	} else if f.V < 1 || f.V > 255 {
		add("v", fmt.Sprintf("%d is outside 1..255", f.V))
	} else if f.V < floor {
		belowFloor = f.V
		add("v", fmt.Sprintf("%d is below the pipe floor %d (VersionMismatch; framing.json#/versionFloors/pipe) — the sender speaks an older framing than this build accepts", f.V, floor))
	}

	if r, ok := raw["id"]; !ok {
		add("(frame)", "id is required")
	} else {
		var s string
		if err := json.Unmarshal(r, &s); err != nil {
			add("id", "must be a string")
		} else if !reID.MatchString(s) {
			add("id", "must match ^[A-Za-z0-9_-]{8,36}$ — it is also the idempotency key")
		} else {
			f.ID = s
		}
	}

	if r, ok := raw["type"]; !ok {
		add("(frame)", "type is required")
	} else {
		var s string
		if err := json.Unmarshal(r, &s); err != nil {
			add("type", "must be a string")
		} else {
			f.Type = Type(s)
			switch f.Type {
			case TypeRequest, TypeResponse, TypeEvent, TypeProgress:
			default:
				add("type", fmt.Sprintf("%q is not a frame type", s))
			}
		}
	}

	if r, ok := raw["method"]; ok {
		var s string
		if err := json.Unmarshal(r, &s); err != nil {
			add("method", "must be a string")
		} else {
			f.Method = &s
			// The shape only. Membership in the closed allowlist is deliberately not checked here:
			// `envelope.schema.json#/properties/method` declares a pattern and nothing else, and ADR-0007 rule 2
			// puts the refusal in *resolution* — "an unknown method MUST produce UnknownMethod and MUST be
			// answered". A codec that refuses first turns a published method name's absence into a transport
			// teardown, because internal/peer ends the stream on any frame Parse rejects; internal/dispatch is
			// the package that owns that verdict, and the fixtures record the exchange it produces.
			if !reMethod.MatchString(s) {
				add("method", "must be a dotted lower-camel name such as connector.hello")
			}
		}
	}

	for _, name := range []string{"args", "result"} {
		r, ok := raw[name]
		if !ok {
			continue
		}
		t := bytes.TrimSpace(r)
		if len(t) == 0 || (t[0] != '{' && string(t) != "null") {
			add(name, "must be an object or null")
			continue
		}
		if name == "args" {
			f.Args = append(json.RawMessage(nil), t...)
		} else {
			f.Result = append(json.RawMessage(nil), t...)
		}
	}

	if r, ok := raw["error"]; ok {
		f.Error = parseError(r, add)
	}
	if r, ok := raw["inReplyTo"]; ok {
		f.InReplyTo = parseNullableString(r, "inReplyTo", add)
		if f.InReplyTo != nil && !reID.MatchString(*f.InReplyTo) {
			add("inReplyTo", "must match ^[A-Za-z0-9_-]{8,36}$")
		}
	}
	if r, ok := raw["ok"]; ok {
		var b bool
		if err := json.Unmarshal(r, &b); err != nil {
			add("ok", "must be a boolean")
		} else {
			f.OK = &b
		}
	}
	if r, ok := raw["traceId"]; ok {
		f.TraceID = parseNullableString(r, "traceId", add)
		if f.TraceID != nil {
			if !reTrace.MatchString(*f.TraceID) {
				add("traceId", "may contain only [A-Za-z0-9_-] — a host, a URL or a secret must not be loggable as a correlation id")
			} else if len(*f.TraceID) > 64 {
				add("traceId", "must be at most 64 characters")
			}
		}
	}

	if r, ok := raw["sentAt"]; !ok {
		add("(frame)", "sentAt is required")
	} else {
		var s string
		if err := json.Unmarshal(r, &s); err != nil {
			add("sentAt", "must be a date-time string")
		} else if _, err := time.Parse(time.RFC3339, s); err != nil {
			add("sentAt", "must be an RFC 3339 date-time")
		} else {
			f.SentAt = s
		}
	}

	if r, ok := raw["seq"]; ok {
		n, ok := parseNullableInt(r, "seq", add)
		if ok {
			if n < 0 {
				add("seq", "must be >= 0")
			} else {
				f.Seq = &n
			}
		}
	}

	if r, ok := raw["progress"]; ok {
		f.Progress = parseProgress(r, add)
	}
	if r, ok := raw["chunk"]; ok {
		f.Chunk = parseChunk(r, add)
	}

	v = append(v, typeRules(f)...)
	v = append(v, chunkMirror(f)...)

	if len(v) > 0 {
		return nil, &Invalid{Violations: v, BelowPipeFloor: belowFloor, RejectedID: f.ID, RejectedTrace: f.TraceID}
	}
	return f, nil
}

// Validate checks a frame the caller built by hand. It is the same rule set Parse runs, so a response the
// Connector constructs cannot leave the process in a shape the extension would have to reject.
func (f *Frame) Validate() error {
	var v []Violation
	if f.V < 1 || f.V > 255 {
		v = append(v, Violation{"v", fmt.Sprintf("%d is outside 1..255", f.V)})
	}
	if !reID.MatchString(f.ID) {
		v = append(v, Violation{"id", "must match ^[A-Za-z0-9_-]{8,36}$"})
	}
	switch f.Type {
	case TypeRequest, TypeResponse, TypeEvent, TypeProgress:
	default:
		v = append(v, Violation{"type", fmt.Sprintf("%q is not a frame type", string(f.Type))})
	}
	if _, err := time.Parse(time.RFC3339, f.SentAt); err != nil {
		v = append(v, Violation{"sentAt", "must be an RFC 3339 date-time"})
	}
	if f.Error != nil {
		if _, found := contract.LookupCode(f.Error.Code); !found {
			v = append(v, Violation{"error.code", fmt.Sprintf("%q is outside the closed taxonomy", f.Error.Code)})
		}
	}
	v = append(v, typeRules(f)...)
	v = append(v, chunkMirror(f)...)
	if len(v) > 0 {
		return &Invalid{Violations: v}
	}
	return nil
}

func typeRules(f *Frame) []Violation {
	present := f.has
	var v []Violation
	switch f.Type {
	case TypeRequest:
		if !present("method") {
			v = append(v, Violation{"(frame)", "a request carries method"})
		}
		for _, k := range []string{"result", "ok", "error"} {
			if present(k) {
				v = append(v, Violation{k, "a request must not carry it"})
			}
		}
	case TypeResponse:
		for _, k := range []string{"ok", "inReplyTo"} {
			if !present(k) {
				v = append(v, Violation{"(frame)", "a response carries " + k})
			}
		}
		if present("method") {
			v = append(v, Violation{"method", "a response must not carry it"})
		}
		if f.OK != nil {
			if *f.OK && !present("result") {
				v = append(v, Violation{"result", "an ok response carries the result"})
			}
			if !*f.OK && !present("error") {
				v = append(v, Violation{"error", "a failed response carries the error"})
			}
		}
	case TypeEvent, TypeProgress:
		if !present("progress") {
			v = append(v, Violation{"progress", "an event or progress frame carries it"})
		}
		for _, k := range []string{"method", "ok", "error"} {
			if present(k) {
				v = append(v, Violation{k, "an event or progress frame must not carry it"})
			}
		}
		if f.Type == TypeEvent {
			if !present("result") {
				v = append(v, Violation{"result", "an event carries its payload as result"})
			}
			if f.Progress != nil && f.Progress.Event == nil {
				v = append(v, Violation{"progress.event", "an event names the event it is"})
			}
		}
	}
	return v
}

// has reports key presence. A parsed frame knows from the payload; a constructed frame infers it from the
// fields that were set.
func (f *Frame) has(key string) bool {
	if f.present != nil {
		return f.present[key]
	}
	switch key {
	case "v":
		return f.V != 0
	case "id":
		return f.ID != ""
	case "type":
		return f.Type != ""
	case "method":
		return f.Method != nil
	case "args":
		return f.Args != nil
	case "result":
		return f.Result != nil
	case "error":
		return f.Error != nil
	case "inReplyTo":
		return f.InReplyTo != nil
	case "ok":
		return f.OK != nil
	case "traceId":
		return f.TraceID != nil
	case "sentAt":
		return f.SentAt != ""
	case "seq":
		return f.Seq != nil
	case "progress":
		return f.Progress != nil
	case "chunk":
		return f.Chunk != nil
	}
	return false
}

// chunkMirror enforces the fourth group rule of docs/architecture/25-native-messaging-protocol.md §5: inside one
// frame the envelope copy and the args copy of a slice are the same object, not two values that may disagree.
func chunkMirror(f *Frame) []Violation {
	if f.Chunk == nil || f.Args == nil {
		return nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &args); err != nil {
		return nil
	}
	inner, ok := args["chunk"]
	if !ok {
		return nil
	}
	a, err := json.Marshal(f.Chunk)
	if err != nil {
		return nil
	}
	var mirror Chunk
	if err := json.Unmarshal(inner, &mirror); err != nil {
		return []Violation{{Field: "args.chunk", Reason: "must be a chunk object"}}
	}
	b, err := json.Marshal(&mirror)
	if err != nil {
		return nil
	}
	if string(a) != string(b) {
		return []Violation{{Field: "args.chunk", Reason: "differs from the envelope copy in the same frame"}}
	}
	return nil
}

func parseError(r json.RawMessage, add func(field, reason string)) *Error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(r, &obj); err != nil {
		add("error", "must be an object or null")
		return nil
	}
	e := &Error{}
	for k := range obj {
		switch k {
		case "code", "message", "redacted", "retryable", "field":
		default:
			add("error."+k, "field is not declared by the error shape")
		}
	}
	if code, ok := obj["code"]; !ok {
		add("error", "code is required")
	} else if err := json.Unmarshal(code, &e.Code); err != nil {
		add("error.code", "must be a string")
	} else if _, found := contract.LookupCode(e.Code); !found {
		add("error.code", fmt.Sprintf("%q is outside the closed taxonomy", e.Code))
	}
	if m, ok := obj["message"]; ok {
		var s string
		if err := json.Unmarshal(m, &s); err != nil {
			add("error.message", "must be a string")
		} else if len(s) > 200 {
			add("error.message", "must be at most 200 characters")
		} else {
			e.Message = &s
		}
	}
	if fl, ok := obj["field"]; ok {
		var s string
		if err := json.Unmarshal(fl, &s); err != nil {
			add("error.field", "must be a string")
		} else if len(s) > 64 {
			add("error.field", "must be at most 64 characters")
		} else {
			e.Field = &s
		}
	}
	for name, dst := range map[string]**bool{"redacted": &e.Redacted, "retryable": &e.Retryable} {
		r, ok := obj[name]
		if !ok {
			continue
		}
		var b bool
		if err := json.Unmarshal(r, &b); err != nil {
			add("error."+name, "must be a boolean")
			continue
		}
		value := b
		*dst = &value
	}
	return e
}

func parseNullableString(r json.RawMessage, name string, add func(field, reason string)) *string {
	if bytes.Equal(bytes.TrimSpace(r), []byte("null")) {
		return nil
	}
	var s string
	if err := json.Unmarshal(r, &s); err != nil {
		add(name, "must be a string or null")
		return nil
	}
	return &s
}

func parseNullableInt(r json.RawMessage, name string, add func(field, reason string)) (int, bool) {
	if bytes.Equal(bytes.TrimSpace(r), []byte("null")) {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(r, &n); err != nil {
		add(name, "must be an integer or null")
		return 0, false
	}
	return n, true
}

func parseProgress(r json.RawMessage, add func(field, reason string)) *Progress {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(r, &obj); err != nil {
		add("progress", "must be an object or null")
		return nil
	}
	p := &Progress{}
	for k := range obj {
		switch k {
		case "stage", "done", "total", "event":
		default:
			add("progress."+k, "field is not declared by the progress shape")
		}
	}
	if s, ok := obj["stage"]; !ok {
		add("progress", "stage is required")
	} else {
		var str string
		if err := json.Unmarshal(s, &str); err != nil {
			add("progress.stage", "must be a string")
		} else if len(str) < 1 || len(str) > 40 {
			add("progress.stage", "must be 1..40 characters")
		} else {
			p.Stage = str
		}
	}
	if d, ok := obj["done"]; !ok {
		add("progress", "done is required")
	} else {
		var n int
		if err := json.Unmarshal(d, &n); err != nil {
			add("progress.done", "must be an integer")
		} else if n < 0 {
			add("progress.done", "must be >= 0")
		} else {
			p.Done = n
		}
	}
	if t, ok := obj["total"]; ok {
		n, ok := parseNullableInt(t, "progress.total", add)
		if ok {
			if n < 0 {
				add("progress.total", "must be >= 0")
			} else {
				p.Total = &n
			}
		}
	}
	if e, ok := obj["event"]; ok {
		p.Event = parseNullableString(e, "progress.event", add)
	}
	return p
}

func parseChunk(r json.RawMessage, add func(field, reason string)) *Chunk {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(r, &obj); err != nil {
		add("chunk", "must be an object or null")
		return nil
	}
	c := &Chunk{}
	for k := range obj {
		switch k {
		case "contentHash", "chunkSeq", "chunkCount", "encoding":
		default:
			add("chunk."+k, "field is not declared by the chunk shape — there is no chunkId")
		}
	}
	if h, ok := obj["contentHash"]; !ok {
		add("chunk", "contentHash is required")
	} else if err := json.Unmarshal(h, &c.ContentHash); err != nil {
		add("chunk.contentHash", "must be a string")
	} else if !reHash.MatchString(c.ContentHash) {
		add("chunk.contentHash", "must be 64 lowercase hex characters")
	}
	if s, ok := obj["encoding"]; !ok {
		add("chunk", "encoding is required")
	} else if err := json.Unmarshal(s, &c.Encoding); err != nil {
		add("chunk.encoding", "must be a string")
	} else if !encodings[c.Encoding] {
		add("chunk.encoding", fmt.Sprintf("%q is not base64 or json", c.Encoding))
	}
	if r, ok := obj["chunkSeq"]; !ok {
		add("chunk", "chunkSeq is required")
	} else {
		n, ok := parseNullableInt(r, "chunk.chunkSeq", add)
		if ok {
			if n < 0 {
				add("chunk.chunkSeq", "must be >= 0")
			} else {
				c.ChunkSeq = n
			}
		}
	}
	if r, ok := obj["chunkCount"]; !ok {
		add("chunk", "chunkCount is required")
	} else {
		n, ok := parseNullableInt(r, "chunk.chunkCount", add)
		if ok {
			if n < 2 {
				add("chunk.chunkCount", fmt.Sprintf("%d is below 2 — a single-slice payload is not a chunked transfer", n))
			} else {
				c.ChunkCount = n
			}
		}
	}
	return c
}

// NewRequest builds a request frame. The id is caller-chosen because it is also the idempotency key: a retry
// of the same intent must reuse it (ADR-0008).
func NewRequest(id, method string, args any) (*Frame, error) {
	raw, err := jsonArg(args)
	if err != nil {
		return nil, err
	}
	m := method
	f := &Frame{V: contract.FrameFormatVersion, ID: id, Type: TypeRequest, Method: &m, Args: raw, SentAt: Now()}
	return f, f.finalize()
}

// Respond answers this request with a result.
func (f *Frame) Respond(result any) (*Frame, error) {
	raw, err := jsonArg(result)
	if err != nil {
		return nil, err
	}
	reply := f.ID
	yes := true
	out := &Frame{V: f.V, ID: newID(), Type: TypeResponse, OK: &yes, InReplyTo: &reply, Result: raw, TraceID: f.TraceID, SentAt: Now()}
	return out, out.finalize()
}

// Fail answers this request with a taxonomy code. The retryable flag is taken from the taxonomy, not supplied
// by the caller, so a handler cannot claim a permanent failure is worth retrying.
func (f *Frame) Fail(code, message string) (*Frame, error) {
	c, found := contract.LookupCode(code)
	if !found {
		return nil, fmt.Errorf("envelope: %q is outside the closed error taxonomy", code)
	}
	reply := f.ID
	no := false
	retry := c.Retryable
	msg := message
	out := &Frame{V: f.V, ID: newID(), Type: TypeResponse, OK: &no, InReplyTo: &reply,
		Error: &Error{Code: code, Message: &msg, Retryable: &retry}, TraceID: f.TraceID, SentAt: Now()}
	return out, out.finalize()
}

// finalize keeps a constructed frame's presence map in step with its fields, then validates it.
func (f *Frame) finalize() error {
	f.present = nil
	return f.Validate()
}

func jsonArg(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	return b, nil
}

// Now returns the timestamp format the contract declares.
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Marshal encodes the frame. The key set it emits is exactly the set the validator saw, so a frame that
// passed Validate cannot come back from Parse as invalid.
func (f *Frame) Marshal() ([]byte, error) {
	type wire struct {
		V         int             `json:"v"`
		ID        string          `json:"id"`
		Type      Type            `json:"type"`
		Method    *string         `json:"method,omitempty"`
		Args      json.RawMessage `json:"args,omitempty"`
		Result    json.RawMessage `json:"result,omitempty"`
		Error     *Error          `json:"error,omitempty"`
		InReplyTo *string         `json:"inReplyTo,omitempty"`
		OK        *bool           `json:"ok,omitempty"`
		TraceID   *string         `json:"traceId,omitempty"`
		SentAt    string          `json:"sentAt"`
		Seq       *int            `json:"seq,omitempty"`
		Progress  *Progress       `json:"progress,omitempty"`
		Chunk     *Chunk          `json:"chunk,omitempty"`
	}
	out, err := json.Marshal(&wire{
		V: f.V, ID: f.ID, Type: f.Type, Method: f.Method, Args: f.Args, Result: f.Result, Error: f.Error,
		InReplyTo: f.InReplyTo, OK: f.OK, TraceID: f.TraceID, SentAt: f.SentAt, Seq: f.Seq,
		Progress: f.Progress, Chunk: f.Chunk,
	})
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	return out, nil
}

// newID makes a response id. Request ids come from the caller because they are the idempotency key.
func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		panic("envelope: no source of randomness: " + err.Error())
	}
	return "resp-" + hex.EncodeToString(buf)
}
