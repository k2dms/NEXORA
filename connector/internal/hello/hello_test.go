package hello

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/contractgen"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
)

// The fixture is the referee here, not an example. packages/schema/fixtures/protocol/handshake.jsonl is the frame
// pair the contract layer asserts against the capability registry and the command schemas, so a Go answer that
// reproduces it byte for byte is the projection and the exchange agreeing in the same sentence.

type pair struct{ req, res *envelope.Frame }

// recordedHellos reads the two hello exchanges the fixture records, and nothing else.
//
// That file also records `req-000000ff`, a request for `core.snooze` — a name the allowlist does not publish. It is
// a valid frame: the codec accepts it, internal/dispatch refuses it, and that refusal is what the fixture's
// `UnknownMethod` answer is the record of. So this reader skips it as what it is, not as something it has to make
// room for.
func recordedHellos(t *testing.T) []pair {
	t.Helper()
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "packages", "schema", "fixtures", "protocol", "handshake.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	responses := map[string]*envelope.Frame{}
	var hellos []*envelope.Frame
	for i, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		f, err := envelope.Parse(line)
		if err != nil {
			t.Fatalf("line %d is a fixture frame the codec rejected: %v", i+1, err)
		}
		switch {
		case f.Type == envelope.TypeResponse && f.InReplyTo != nil:
			responses[*f.InReplyTo] = f
		case f.Method != nil && *f.Method == "connector.hello":
			hellos = append(hellos, f)
		}
	}
	if len(hellos) != 2 {
		t.Fatalf("the fixture records %d hellos, and this package's tests are written against the two in it", len(hellos))
	}
	out := make([]pair, 0, len(hellos))
	for _, req := range hellos {
		res, ok := responses[req.ID]
		if !ok {
			t.Fatalf("the fixture records a hello %s with no answer", req.ID)
		}
		out = append(out, pair{req: req, res: res})
	}
	return out
}

func fullFacts() Facts {
	yes := true
	return Facts{
		ConnectorVersion:   "1.0.0",
		CoreVersion:        "24.11.11",
		PinnedCoreVersion:  "24.11.11",
		SchemaVersion:      3,
		OS:                 "windows",
		KeystoreBackend:    "dpapi",
		JobObjectAvailable: &yes,
	}
}

func TestTheGoAnswerIsTheRecordedHandshakeByteForByte(t *testing.T) {
	r, err := New(fullFacts())
	if err != nil {
		t.Fatal(err)
	}
	hello := recordedHellos(t)[0]

	answer, refusal := r.Answer(hello.req)
	if refusal != nil {
		t.Fatalf("the recorded compatible hello was refused: %s", refusal.Message)
	}
	got, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, hello.res.Result) {
		t.Errorf("the Go answer is not the recorded result\n got: %s\nwant: %s", got, hello.res.Result)
	}
}

func TestTheVersionMismatchIsTheRecordedWords(t *testing.T) {
	r, err := New(fullFacts())
	if err != nil {
		t.Fatal(err)
	}
	stale := recordedHellos(t)[1]

	_, refusal := r.Answer(stale.req)
	if refusal == nil {
		t.Fatal("a peer that speaks only protocol 7 was answered a result")
	}
	if refusal.Code != "VersionMismatch" {
		t.Errorf("code = %s, want VersionMismatch", refusal.Code)
	}
	want := *stale.res.Error.Message
	if refusal.Message != want {
		t.Errorf("message = %q, want the recorded %q", refusal.Message, want)
	}
	if c, _ := contract.LookupCode(refusal.Code); c.Retryable {
		t.Error("VersionMismatch is retryable, so a panel that cannot negotiate would retry forever")
	}
}

// The projected window is 1..1, which makes one of LIVE-01's two directions unreachable through the responder: no
// peer can be too old for a minimum of 1, because the args floor is 1. So the rule itself is tested as the
// arithmetic the contract states it in, and only the reachable direction runs through Answer.
func TestTheWindowRuleIsCheckedInBothDirections(t *testing.T) {
	cases := []struct {
		name       string
		peer, conn Window
		want       bool
	}{
		{"recorded boot", Window{1, 1}, Window{1, 1}, true},
		{"peer too new for this connector", Window{7, 7}, Window{1, 1}, false},
		{"peer too old for this connector", Window{1, 2}, Window{3, 9}, false},
		{"forward compatible", Window{1, 9}, Window{3, 9}, true},
		{"this connector behind the peer's floor", Window{3, 9}, Window{1, 2}, false},
		{"peer window ends where ours starts", Window{2, 5}, Window{1, 2}, true},
	}
	for _, c := range cases {
		if got := c.peer.Intersects(c.conn); got != c.want {
			t.Errorf("%s: %s against %s = %v, want %v", c.name, c.peer, c.conn, got, c.want)
		}
		if !c.conn.Intersects(c.peer) != !c.want {
			t.Errorf("%s: the rule is not symmetric, so the two endpoints would disagree about compatibility", c.name)
		}
	}
	if got := (Window{Min: 1, Max: 255}).String(); got != "1..255" {
		t.Errorf("window printed as %q, want the 1..255 shape the recorded message uses", got)
	}
}

func TestNothingIsDispatchableUntilTheHelloThisSessionRecorded(t *testing.T) {
	r, err := New(fullFacts())
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	d, err := dispatch.New(dispatch.Config{Handlers: map[string]dispatch.Handler{
		"connector.hello": r.Handle,
		"core.status":     func(context.Context, *envelope.Frame) (any, *dispatch.Refusal) { ran++; return "status", nil },
	}})
	if err != nil {
		t.Fatal(err)
	}

	sender := "ext-nexora-pinned"
	sess := new(Session)
	id := sess.Identity(sender, true)
	if id.Negotiated || len(id.Capabilities) != 0 || id.SenderID != sender || !id.Allowlisted {
		t.Fatalf("a fresh session handed the front door %+v", id)
	}

	if _, refusal := d.Dispatch(context.Background(), id, frame(t, "req-status-before", "core.status", "{}")); refusal == nil ||
		refusal.Code != "HandshakeFailed" {
		t.Fatalf("core.status before hello = %+v, want HandshakeFailed", refusal)
	}
	if ran != 0 {
		t.Fatal("a method reached its handler before negotiation")
	}

	result, refusal := d.Dispatch(context.Background(), id, recordedHellos(t)[0].req)
	if refusal != nil {
		t.Fatalf("hello was refused: %s", refusal.Message)
	}
	if !sess.Record(result, refusal) || !sess.Negotiated() {
		t.Fatal("the successful hello did not negotiate this connection")
	}

	after := sess.Identity(sender, true)
	if !after.Negotiated {
		t.Fatal("an answered hello left the gate closed")
	}
	if got, want := strings.Join(after.Capabilities, ","), strings.Join(contract.CapabilityNames(), ","); got != want {
		t.Errorf("the gate knows %q, want the whole registry %q", got, want)
	}
	if len(after.Capabilities) != 13 {
		t.Errorf("the registry announced %d capabilities, and 13 is what docs/architecture/25-native-messaging-protocol.md §7 counts", len(after.Capabilities))
	}
	if _, refusal := d.Dispatch(context.Background(), after, frame(t, "req-status-after", "core.status", "{}")); refusal != nil {
		t.Fatalf("core.status after hello = %s, want admitted", refusal.Message)
	}
	if ran != 1 {
		t.Errorf("the admitted call ran %d handlers, want 1", ran)
	}

	// A second connection from the same sender is not negotiated by the first one's hello: the gate is per port,
	// the budgets are per sender.
	if new(Session).Identity(sender, true).Negotiated {
		t.Error("negotiation leaked from one connection to another")
	}
}

func TestARetatedHelloIsAnsweredIdentically(t *testing.T) {
	r, err := New(fullFacts())
	if err != nil {
		t.Fatal(err)
	}
	recorded := recordedHellos(t)[0]

	first, refusal := r.Answer(recorded.req)
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	// The same intent, the same id, the same args — a retransmission after a reconnect is `replayable: true`, so
	// it is either deduped or answered identically. This is the answered-identically half.
	retry := frame(t, recorded.req.ID, "connector.hello", string(recorded.req.Args))
	second, refusal := r.Answer(retry)
	if refusal != nil {
		t.Fatalf("the retransmitted hello was refused: %s", refusal.Message)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if !bytes.Equal(a, b) {
		t.Errorf("a replay answered %s, want the recorded %s", b, a)
	}

	sess := new(Session)
	sess.Record(first, nil)
	if moved := sess.Record(second, nil); !moved || !sess.Negotiated() {
		t.Errorf("replaying the hello reported %v and left the session negotiated=%v", moved, sess.Negotiated())
	}
}

func TestAHelloThatRetractsItsWindowWithdrawsTheNegotiation(t *testing.T) {
	r, err := New(fullFacts())
	if err != nil {
		t.Fatal(err)
	}
	sess := new(Session)

	good, refusal := r.Answer(recordedHellos(t)[0].req)
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	sess.Record(good, nil)
	if !sess.Negotiated() {
		t.Fatal("the compatible hello did not negotiate")
	}

	_, refusal = r.Answer(recordedHellos(t)[1].req)
	if refusal == nil || refusal.Code != "VersionMismatch" {
		t.Fatalf("a peer that now claims only protocol 7 got %+v", refusal)
	}
	sess.Record(nil, refusal)
	if sess.Negotiated() {
		t.Fatal("the retraction left the gate open, which is where a misread frame applies a wrong proxy configuration")
	}
	if sess.Identity("ext-nexora-pinned", true).Capabilities != nil {
		t.Error("a withdrawn session still announces capabilities")
	}
}

func TestAnUnreadableHelloIsHandshakeFailedBecauseHelloDoesNotDeclareBadRequest(t *testing.T) {
	r, err := New(fullFacts())
	if err != nil {
		t.Fatal(err)
	}
	sess := new(Session)

	// The five codes packages/schema/protocol/commands/connector.hello.json#/errors declares are HandshakeFailed,
	// VersionMismatch, UnauthorizedExtension, Timeout, Internal. BadRequest — the taxonomy's answer to "missing
	// args, wrong shape" — is deliberately not among them, so an unreadable hello answers with the one code that
	// is true: the handshake failed.
	cases := []struct{ name, args, want string }{
		{"no args at all", "", "args are required"},
		{"empty args", "{}", "extensionVersion is required"},
		{"no version window", `{"extensionVersion":"3.6.0"}`, "protocolVersion is required"},
		{"no minimum", `{"extensionVersion":"3.6.0","protocolVersion":1}`, "minCompatibleProtocolVersion is required"},
		{"a string wearing a number", `{"extensionVersion":"3.6.0","protocolVersion":"1","minCompatibleProtocolVersion":1}`, "protocolVersion must be an integer"},
		{"a fraction", `{"extensionVersion":"3.6.0","protocolVersion":1.5,"minCompatibleProtocolVersion":1}`, "protocolVersion must be an integer, not a fraction"},
		{"above the byte", `{"extensionVersion":"3.6.0","protocolVersion":256,"minCompatibleProtocolVersion":1}`, "protocolVersion is outside 1..255"},
		{"below the floor", `{"extensionVersion":"3.6.0","protocolVersion":1,"minCompatibleProtocolVersion":0}`, "minCompatibleProtocolVersion is outside 1..255"},
		{"null instead of a version", `{"extensionVersion":"3.6.0","protocolVersion":null,"minCompatibleProtocolVersion":1}`, "protocolVersion must be an integer"},
		{"a four-part product version", `{"extensionVersion":"3.6.0.7","protocolVersion":1,"minCompatibleProtocolVersion":1}`, "extensionVersion does not match the pattern"},
		{"a null product version", `{"extensionVersion":null,"protocolVersion":1,"minCompatibleProtocolVersion":1}`, "extensionVersion must be a string"},
		{"an undeclared field", `{"extensionVersion":"3.6.0","protocolVersion":1,"minCompatibleProtocolVersion":1,"proxyPac":"http://evil.test"}`, "args carry a field connector.hello does not declare"},
		{"a build id too long", `{"extensionVersion":"3.6.0","protocolVersion":1,"minCompatibleProtocolVersion":1,"uiBuild":"` + strings.Repeat("b", 41) + `"}`, "uiBuild is longer than the 40 characters"},
	}
	for i, c := range cases {
		var args json.RawMessage
		if c.args != "" {
			args = json.RawMessage(c.args)
		}
		_, refusal := r.Answer(frameWithArgs(t, fmt.Sprintf("req-args-%03d", i), "connector.hello", args))
		if refusal == nil || refusal.Code != "HandshakeFailed" {
			t.Errorf("%s: got %+v, want HandshakeFailed", c.name, refusal)
			continue
		}
		if !strings.Contains(refusal.Message, c.want) {
			t.Errorf("%s: message %q does not name the rule %q", c.name, refusal.Message, c.want)
		}
		if sess.Record(nil, refusal) || sess.Negotiated() {
			t.Errorf("%s: an unreadable hello moved the negotiation", c.name)
		}
	}

	// The message names the key at fault and never the value: `#/senderPolicy/refusals/loggedFields` says args are
	// not loggable, and the undeclared-field case is the one where the whole key is attacker-chosen text.
	_, leak := r.Answer(frame(t, "req-args-leak", "connector.hello", `{"extensionVersion":"3.6.0","protocolVersion":1,"minCompatibleProtocolVersion":1,"proxyPac":"http://evil.test"}`))
	if strings.Contains(leak.Message, "evil") || strings.Contains(leak.Message, "proxyPac") {
		t.Errorf("the refusal quoted the peer's args: %q", leak.Message)
	}
}

func TestAPeerWindowThatContradictsItselfIsRefusedInItsOwnWords(t *testing.T) {
	r, err := New(fullFacts())
	if err != nil {
		t.Fatal(err)
	}
	// The schema bounds each number separately, so nothing but this check stops a peer from claiming "I speak at
	// most 1 and need at least 9". Answering that with the intersection arithmetic would print "9..1 and 1..1" —
	// a message that hides which of the two endpoints is incoherent.
	_, refusal := r.Answer(frame(t, "req-inverted", "connector.hello",
		`{"extensionVersion":"3.6.0","protocolVersion":1,"minCompatibleProtocolVersion":9}`))
	if refusal == nil || refusal.Code != "HandshakeFailed" {
		t.Fatalf("got %+v, want HandshakeFailed", refusal)
	}
	if !strings.Contains(refusal.Message, "the window it describes is empty") {
		t.Errorf("message %q does not say the peer's own window is empty", refusal.Message)
	}
}

func TestTheProductVersionIsNotPartOfTheWindow(t *testing.T) {
	r, err := New(fullFacts())
	if err != nil {
		t.Fatal(err)
	}
	// The legacy build number and a compatible protocol window: the rule
	// (`packages/schema/protocol/compatibility-rules.json#/negotiation/rule`) cites protocol versions only, so a
	// 3.5.2 extension that speaks protocol 1 is admitted, and the recorded stale hello fails on its window, not
	// on its product version.
	answer, refusal := r.Answer(frame(t, "req-legacy-ext", "connector.hello",
		`{"extensionVersion":"3.5.2","protocolVersion":1,"minCompatibleProtocolVersion":1}`))
	if refusal != nil {
		t.Fatalf("a legacy product version refused a compatible protocol window: %s", refusal.Message)
	}
	if answer.ConnectorVersion != "1.0.0" || answer.ProtocolVersion != 1 {
		t.Fatalf("answer %+v does not report this build's own numbers", answer)
	}
}

func TestTheResponderRefusesToBuildOnFactsItsOwnResultRejects(t *testing.T) {
	bad := []struct {
		name  string
		facts Facts
	}{
		{"a tagged connector version", Facts{ConnectorVersion: "v1.0.0"}},
		{"a one-part connector version", Facts{ConnectorVersion: "1"}},
		{"an unknown platform", Facts{ConnectorVersion: "1.0.0", OS: "win32"}},
		{"an unlisted keystore", Facts{ConnectorVersion: "1.0.0", KeystoreBackend: "vault"}},
		{"a schema version below the floor", Facts{ConnectorVersion: "1.0.0", SchemaVersion: -1}},
	}
	for _, c := range bad {
		if _, err := New(c.facts); err == nil {
			t.Errorf("%s: New accepted facts the result schema would reject", c.name)
		}
	}

	// A build that has not wired its Core, pins or keystore yet still negotiates, and says nothing about them:
	// every optional field of the schema is absent rather than guessed.
	r, err := New(Facts{ConnectorVersion: "1.0.0-rc.2"})
	if err != nil {
		t.Fatal(err)
	}
	answer, refusal := r.Answer(recordedHellos(t)[0].req)
	if refusal != nil {
		t.Fatal(refusal.Message)
	}
	got, _ := json.Marshal(answer)
	for _, name := range []string{"coreVersion", "pinnedCoreVersion", "schemaVersion", `"os"`, "keystoreBackend", "jobObjectAvailable"} {
		if bytes.Contains(got, []byte(name)) {
			t.Errorf("an unwired build reported %s anyway: %s", name, got)
		}
	}
	if !bytes.Contains(got, []byte(`"connectorVersion":"1.0.0-rc.2"`)) {
		t.Errorf("answer %s does not carry the pre-release version it was given", got)
	}
	if r.Window() != (Window{Min: contract.MinCompatibleProtocolVersion, Max: contract.ProtocolVersion}) {
		t.Error("the responder speaks a window that is not the projected one")
	}
}

func frame(t *testing.T, id, method, args string) *envelope.Frame {
	t.Helper()
	return frameWithArgs(t, id, method, json.RawMessage(args))
}

// frameWithArgs issues a real request frame, so a test that hands the responder something malformed has still
// handed it a frame the codec accepted: envelope validation stops at "args is an object", and what the object must
// contain is the command's own subschema, which is this package's to enforce.
func frameWithArgs(t *testing.T, id, method string, args json.RawMessage) *envelope.Frame {
	t.Helper()
	if len(id) < 8 {
		t.Fatalf("request id %q is below the envelope's eight-character floor", id)
	}
	f, err := envelope.NewRequest(id, method, args)
	if err != nil {
		t.Fatalf("building %s: %v", method, err)
	}
	return f
}
