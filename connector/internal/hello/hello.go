// Package hello is the connector's half of `connector.hello` — the only frame a peer may send before it has
// negotiated (`packages/schema/protocol/command-allowlist.json`, `docs/architecture/25-native-messaging-protocol.md` §7).
// That gate is what makes the rest of the protocol safe to ship: "a misread frame cannot apply a wrong proxy
// configuration, because nothing is sent to misread". So this package answers exactly two questions — which
// protocol versions this build speaks, and which capability names it honours — and holds the answer for one
// connection at a time.
//
// The Responder is stateless and shareable: an answer depends only on the frame's args and on the build facts the
// process injected, so one process registers one Responder in the front door and every connection gets its own
// Session. The Session is what `dispatch.Identity.Negotiated` reads, which is the same fact as the gate that
// refuses every other method: only a hello on this connection can move it.
//
// Three readings live here because the contract data does not spell them out. First, the two numbers a hello
// result carries are this connector's own `protocolVersion` and `minCompatibleProtocolVersion` — not the
// intersection: the peer applies the same rule to reach its own verdict, and the recorded handshake
// (`packages/schema/fixtures/protocol/handshake.jsonl`) echoes the connector's numbers back unchanged. Second,
// `packages/schema/protocol/commands/connector.hello.json#/errors` declares five codes and `BadRequest` is not one
// of them, so a hello whose args this build cannot read is `HandshakeFailed` — which is also literally what
// happened. Third, a peer that retracts its window after agreeing to one is answered truthfully and the
// negotiation is withdrawn: dispatching under a window the peer has just disowned is the misread-frame risk this
// exchange exists to close.
//
// Refusal messages name the schema key at fault and never its value.
// `packages/schema/protocol/framing.json#/senderPolicy/refusals/loggedFields` forbids quoting args on this pipe,
// and an undeclared key is attacker-chosen text, so that one message says only that an undeclared key arrived.
package hello

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
)

// Window is one endpoint's claim about the protocol versions it will speak. For the peer, `min` is
// minCompatibleProtocolVersion — the oldest connector it will talk to — and `max` is its own protocolVersion. For
// this build both numbers come from the projection of
// `packages/schema/protocol/compatibility-rules.json`.
type Window struct {
	Min, Max int
}

// Intersects is `packages/schema/protocol/compatibility-rules.json#/negotiation/rule` as arithmetic: the peer's
// minimum against this build's own version and this build's minimum against the peer's own version, which is one
// symmetric pair of interval comparisons. An empty intersection is `VersionMismatch`.
func (w Window) Intersects(o Window) bool { return o.Min <= w.Max && w.Min <= o.Max }

// String is the shape the recorded VersionMismatch message uses for a window: "7..7".
func (w Window) String() string { return fmt.Sprintf("%d..%d", w.Min, w.Max) }

var (
	reConnectorVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?(-[0-9A-Za-z.-]+)?$`)
	reExtensionVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)

	platforms = map[string]bool{"windows": true, "linux": true, "darwin": true}
	keystores = map[string]bool{"dpapi": true, "keyring": true, "file-acl": true, "unavailable": true}
)

// Facts are the answers about this build that a hello reports. Only ConnectorVersion is required; the rest are
// optional fields of the result schema and zero (or nil) means "not reported", which is how a build whose Core,
// pins or keystore are not yet wired answers honestly instead of inventing them. `OS` and `KeystoreBackend` are the
// process's own readings of its platform, each checked here against the values the result schema enumerates: a
// name outside those lists is a build that should not start.
type Facts struct {
	ConnectorVersion   string
	CoreVersion        string
	PinnedCoreVersion  string
	SchemaVersion      int
	OS                 string
	KeystoreBackend    string
	JobObjectAvailable *bool
}

func (f Facts) validate() error {
	if !reConnectorVersion.MatchString(f.ConnectorVersion) {
		return fmt.Errorf("hello: connectorVersion %q does not match the pattern connector.hello's own result declares", f.ConnectorVersion)
	}
	if f.OS != "" && !platforms[f.OS] {
		return fmt.Errorf("hello: os %q is not one of the three values the result schema enumerates", f.OS)
	}
	if f.KeystoreBackend != "" && !keystores[f.KeystoreBackend] {
		return fmt.Errorf("hello: keystoreBackend %q is not one of the four values the result schema enumerates", f.KeystoreBackend)
	}
	if f.SchemaVersion < 0 {
		return fmt.Errorf("hello: schemaVersion %d is below the floor of 1; zero means this build does not know", f.SchemaVersion)
	}
	return nil
}

// Answer is the `connector.hello` result object: the required four fields plus the optional ones this build was
// given. Field order is the order the recorded handshake writes them, so a Go answer and the fixture's frame can
// be compared as bytes.
//
// The capability list is the projected registry, and nothing else —
// `packages/schema/scripts/verify.mjs` asserts in both directions that the recorded hello and
// `packages/schema/protocol/compatibility-rules.json#/capabilities` are the same set, and the drift test in
// internal/contract asserts that the projection is the current contract, so naming the registry here needs no
// second check.
type Answer struct {
	ProtocolVersion              int      `json:"protocolVersion"`
	MinCompatibleProtocolVersion int      `json:"minCompatibleProtocolVersion"`
	ConnectorVersion             string   `json:"connectorVersion"`
	CoreVersion                  string   `json:"coreVersion,omitempty"`
	PinnedCoreVersion            string   `json:"pinnedCoreVersion,omitempty"`
	SchemaVersion                int      `json:"schemaVersion,omitempty"`
	Capabilities                 []string `json:"capabilities"`
	OS                           string   `json:"os,omitempty"`
	KeystoreBackend              string   `json:"keystoreBackend,omitempty"`
	JobObjectAvailable           *bool    `json:"jobObjectAvailable,omitempty"`
}

// Responder answers hellos for a process. Build it once, register it for `connector.hello`, and give every
// connection a Session.
type Responder struct {
	facts  Facts
	window Window
}

// New checks the injected facts against the schema the answers are written from — a build that reports a version
// its own result cannot carry is a build that should fail at startup rather than negotiate.
func New(facts Facts) (*Responder, error) {
	if err := facts.validate(); err != nil {
		return nil, err
	}
	return &Responder{
		facts:  facts,
		window: Window{Min: contract.MinCompatibleProtocolVersion, Max: contract.ProtocolVersion},
	}, nil
}

// Window reports the versions this responder will speak, which is the contract's own pair of numbers.
func (r *Responder) Window() Window { return r.window }

// Handle is a dispatch.Handler: the front door's gates run in front of it like every other method, and a hello
// that reaches it and cannot be answered returns a Refusal rather than an Error value.
func (r *Responder) Handle(_ context.Context, f *envelope.Frame) (any, *dispatch.Refusal) {
	a, refusal := r.Answer(f)
	if refusal != nil {
		return nil, refusal
	}
	return a, nil
}

// Answer is the stateless half: read the peer's window, apply the rule, and assemble the result. It is exported
// because a connection that wants to answer a hello without recording it — a probe, a fixture comparison — needs
// the same arithmetic, not a second copy of it.
func (r *Responder) Answer(f *envelope.Frame) (*Answer, *dispatch.Refusal) {
	peer, bad := readArgs(f)
	if bad != nil {
		return nil, bad
	}
	if !peer.Intersects(r.window) {
		return nil, &dispatch.Refusal{
			Code:    "VersionMismatch",
			Message: fmt.Sprintf("no overlap between %s and %s", peer, r.window),
		}
	}
	return &Answer{
		ProtocolVersion:              r.window.Max,
		MinCompatibleProtocolVersion: r.window.Min,
		ConnectorVersion:             r.facts.ConnectorVersion,
		CoreVersion:                  r.facts.CoreVersion,
		PinnedCoreVersion:            r.facts.PinnedCoreVersion,
		SchemaVersion:                r.facts.SchemaVersion,
		Capabilities:                 contract.CapabilityNames(),
		OS:                           r.facts.OS,
		KeystoreBackend:              r.facts.KeystoreBackend,
		JobObjectAvailable:           r.facts.JobObjectAvailable,
	}, nil
}

// Session is one connection's memory of the negotiation. The peer calls a handler inline in its read loop, so one
// Session has exactly one caller and needs no lock; two connections from one sender are two Sessions, which is
// why the negotiation is per port and the budgets are per sender.
type Session struct {
	agreed *Answer
}

// Record folds everything Dispatch returned for one frame into the negotiation, and reports whether it moved. A
// hello that produced an Answer is the only thing that can negotiate; a VersionMismatch on an already-negotiated
// connection is the only thing that can withdraw it. Any other answer — including a `HandshakeFailed` for
// unreadable args — says nothing about the peer's window, so the agreement stands.
func (s *Session) Record(result any, refusal *dispatch.Refusal) bool {
	if a, ok := result.(*Answer); ok {
		s.agreed = a
		return true
	}
	if s.agreed != nil && refusal != nil && refusal.Code == "VersionMismatch" {
		s.agreed = nil
		return true
	}
	return false
}

// Negotiated reports whether this connection may send anything but hello.
func (s *Session) Negotiated() bool { return s.agreed != nil }

// Identity is the front door's view of this connection: the sender id and allowlist decision the host resolved
// from the OS, plus the negotiation this package holds. The capabilities a gate checks against are the names this
// build announced to this peer, which is `packages/schema/protocol/compatibility-rules.json#/commandGating` read
// from the connector's side.
func (s *Session) Identity(senderID string, allowlisted bool) dispatch.Identity {
	id := dispatch.Identity{SenderID: senderID, Allowlisted: allowlisted}
	if s.agreed != nil {
		id.Negotiated = true
		id.Capabilities = s.agreed.Capabilities
	}
	return id
}

// readArgs is the peer's side of the exchange, exactly as
// `packages/schema/protocol/commands/connector.hello.json#/args` declares it: four fields, three of them required,
// `additionalProperties: false`, version numbers in 1..255.
func readArgs(f *envelope.Frame) (Window, *dispatch.Refusal) {
	if len(f.Args) == 0 {
		return Window{}, badArgs("args are required: extensionVersion, protocolVersion, minCompatibleProtocolVersion")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &raw); err != nil {
		return Window{}, badArgs("args are not a JSON object")
	}
	for k := range raw {
		switch k {
		case "extensionVersion", "protocolVersion", "minCompatibleProtocolVersion", "uiBuild":
		default:
			return Window{}, badArgs("args carry a field connector.hello does not declare")
		}
	}

	ext, why := requiredText(raw, "extensionVersion")
	if why != "" {
		return Window{}, badArgs(why)
	}
	if !reExtensionVersion.MatchString(ext) {
		return Window{}, badArgs("extensionVersion does not match the pattern the command declares")
	}
	max, why := requiredNumber(raw, "protocolVersion")
	if why != "" {
		return Window{}, badArgs(why)
	}
	min, why := requiredNumber(raw, "minCompatibleProtocolVersion")
	if why != "" {
		return Window{}, badArgs(why)
	}
	if build, ok := raw["uiBuild"]; ok {
		value, why := decodeText(build)
		if why != "" {
			return Window{}, badArgs("uiBuild " + why)
		}
		if utf8.RuneCountInString(value) > 40 {
			return Window{}, badArgs("uiBuild is longer than the 40 characters the command declares")
		}
	}

	if min > max {
		return Window{}, badArgs("minCompatibleProtocolVersion is above protocolVersion, so the window it describes is empty")
	}
	return Window{Min: min, Max: max}, nil
}

// requiredText and requiredNumber return the rule a field broke, naming only a key the command declares — never
// its value, which is what `#/senderPolicy/refusals/loggedFields` rules out of a log line.
func requiredText(raw map[string]json.RawMessage, name string) (string, string) {
	v, ok := raw[name]
	if !ok {
		return "", name + " is required"
	}
	s, why := decodeText(v)
	if why != "" {
		return "", name + " " + why
	}
	return s, ""
}

func requiredNumber(raw map[string]json.RawMessage, name string) (int, string) {
	v, ok := raw[name]
	if !ok {
		return 0, name + " is required"
	}
	n, why := decodeNumber(v)
	if why != "" {
		return 0, name + " " + why
	}
	return n, ""
}

// decodeNumber accepts a JSON integer in 1..255 — the range the two version fields declare — and nothing that
// merely looks like one: a string wearing a number, a fraction, a boolean and null are all refused.
func decodeNumber(raw json.RawMessage) (int, string) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var token any
	if err := dec.Decode(&token); err != nil {
		return 0, "is not readable JSON"
	}
	n, ok := token.(json.Number)
	if !ok {
		return 0, "must be an integer"
	}
	i, err := n.Int64()
	if err != nil {
		return 0, "must be an integer, not a fraction"
	}
	if i < 1 || i > 255 {
		return 0, "is outside 1..255"
	}
	return int(i), ""
}

func decodeText(v json.RawMessage) (string, string) {
	dec := json.NewDecoder(bytes.NewReader(v))
	var s any
	if err := dec.Decode(&s); err != nil {
		return "", "is not readable JSON"
	}
	t, ok := s.(string)
	if !ok {
		return "", "must be a string"
	}
	return t, ""
}

func badArgs(rule string) *dispatch.Refusal {
	return &dispatch.Refusal{Code: "HandshakeFailed", Message: "connector.hello " + rule}
}
