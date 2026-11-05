package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/contractgen"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/frames"
	"github.com/k2dms/nexora/connector/internal/peer"
)

// The corpus is the referee for this process too: `packages/schema/fixtures/protocol/handshake.jsonl` records the
// hello this build must answer the same way, the version strings it must agree with, and the one exchange an unwired
// connector can reproduce exactly — a request for a name the allowlist does not publish, answered rather than torn
// down.

const (
	pinned = "ext-nexora-pinned"

	recordedHelloArgs = `{"extensionVersion":"3.6.0","protocolVersion":1,` +
		`"minCompatibleProtocolVersion":1,"uiBuild":"nexora-options-12"}`
)

// nopWriteCloser is one half of a duplex made from buffers, so a test can drive the joined descriptors run() takes
// without a real pipe.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func jsonlConfig(allowlisted bool) config {
	return config{senderID: pinned, allowlisted: allowlisted, jsonl: true, version: "1.0.0"}
}

func framedConfig(allowlisted bool) config {
	cfg := jsonlConfig(allowlisted)
	cfg.jsonl = false
	return cfg
}

func requestLine(t *testing.T, id, method, args string) string {
	t.Helper()
	f, err := envelope.NewRequest(id, method, json.RawMessage(args))
	if err != nil {
		t.Fatalf("building the %s request: %v", method, err)
	}
	data, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustParse(t *testing.T, line string) *envelope.Frame {
	t.Helper()
	f, err := envelope.Parse([]byte(line))
	if err != nil {
		t.Fatalf("building a frame the codec rejects: %v", err)
	}
	return f
}

func bufferPipe(t *testing.T, in string, out *bytes.Buffer) duplex {
	t.Helper()
	return duplex{io.NopCloser(strings.NewReader(in)), nopWriteCloser{out}}
}

// runJSONL is the whole line mode: frames in, the response lines it wrote, and the error that ended the run.
func runJSONL(t *testing.T, cfg config, lines ...string) ([]string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(cfg, bufferPipe(t, strings.Join(lines, "\n")+"\n", &out), io.Discard)
	var got []string
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			got = append(got, strings.TrimSpace(l))
		}
	}
	return got, err
}

// answered parses the one response a case must have produced and checks it replies to the right request.
func answered(t *testing.T, lines []string, wantID string) *envelope.Frame {
	t.Helper()
	if len(lines) != 1 {
		t.Fatalf("%d response lines, want exactly one: %v", len(lines), lines)
	}
	res, err := envelope.Parse([]byte(lines[0]))
	if err != nil {
		t.Fatalf("this build answered a frame its own codec rejects: %v", err)
	}
	if res.Type != envelope.TypeResponse {
		t.Fatalf("answered a %s frame", res.Type)
	}
	if res.InReplyTo == nil || *res.InReplyTo != wantID {
		t.Fatalf("answered %v, want inReplyTo %s", res.InReplyTo, wantID)
	}
	return res
}

// checkRefusal asserts the code and the retryable verdict, which envelope.Fail reads from the taxonomy rather than
// from this package — a refusal this build invented could not pass it.
func checkRefusal(t *testing.T, res *envelope.Frame, wantID, code string) {
	t.Helper()
	if res.OK == nil || *res.OK {
		t.Fatalf("%s was answered ok:%v", code, res.OK)
	}
	if res.Error == nil || res.Error.Code != code {
		t.Fatalf("answered %+v, want %s", res.Error, code)
	}
	tax, found := contract.LookupCode(code)
	if !found {
		t.Fatalf("%s is outside the taxonomy this build reads its refusals from", code)
	}
	if res.Error.Retryable == nil || *res.Error.Retryable != tax.Retryable {
		t.Errorf("%s answered retryable=%v, want the taxonomy's %v", code, res.Error.Retryable, tax.Retryable)
	}
	if wantID != "" && (res.InReplyTo == nil || *res.InReplyTo != wantID) {
		t.Errorf("answered %v, want inReplyTo %s", res.InReplyTo, wantID)
	}
}

func TestJSONLModeAnswersTheFrontDoorCases(t *testing.T) {
	hello := requestLine(t, "req-jsonl-hello0", "connector.hello", recordedHelloArgs)
	cases := []struct {
		name          string
		allowlisted   bool
		warm          []string
		id            string
		request       string
		wantCode      string
		wantQuiet     string
		wantStreamEnd bool
	}{
		{
			name: "the negotiation frame is answered", allowlisted: true,
			id: "req-jsonl-hello0", request: hello,
		},
		{
			name: "an unpublished name is answered, not torn down (ADR-0007 rule 2)", allowlisted: true,
			id: "req-jsonl-snooze", request: requestLine(t, "req-jsonl-snooze", "core.snooze", `{}`),
			wantCode: "UnknownMethod",
		},
		{
			name: "everything but hello waits for the negotiation", allowlisted: true,
			id: "req-jsonl-versn0", request: requestLine(t, "req-jsonl-versn0", "connector.version", `{}`),
			wantCode: "HandshakeFailed",
		},
		{
			name: "a published method this build registers no handler for says Internal", allowlisted: true,
			warm: []string{hello},
			id:   "req-jsonl-select", request: requestLine(t, "req-jsonl-select", "core.select", `{}`),
			wantCode: "Internal",
		},
		{
			name: "a sender the host did not allowlist gets the sender error and nothing of its args", allowlisted: false,
			id:       "req-jsonl-forign",
			request:  requestLine(t, "req-jsonl-forign", "connector.hello", strings.Replace(recordedHelloArgs, `,"uiBuild"`, `,"password":"hunter2","uiBuild"`, 1)),
			wantCode: "UnauthorizedExtension", wantQuiet: "hunter2",
		},
		{
			name: "a frame missing the fields the envelope requires ends the stream", allowlisted: true,
			id: "req-jsonl-shape0", request: `{"v":1,"id":"req-jsonl-shape0","type":"request","method":"connector.hello"}`,
			wantStreamEnd: true,
		},
		{
			name: "a line that is not one JSON object ends the stream", allowlisted: true,
			id: "req-jsonl-garbag", request: `connector.hello {}`,
			wantStreamEnd: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := runJSONL(t, jsonlConfig(c.allowlisted), append(append([]string{}, c.warm...), c.request)...)
			if c.wantStreamEnd {
				if err == nil {
					t.Fatal("a frame the codec rejects was treated as something to read on past")
				}
				if !strings.Contains(err.Error(), "inbound frame rejected") {
					t.Errorf("ended the run with %v, want the codec's rejection", err)
				}
				if len(got) != 0 {
					t.Errorf("the rejected frame was answered: %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(got) != len(c.warm)+1 {
				t.Fatalf("%d response lines, want %d", len(got), len(c.warm)+1)
			}
			res := answered(t, got[len(got)-1:], c.id)
			if c.wantCode == "" {
				if res.OK == nil || !*res.OK || res.Error != nil {
					t.Fatalf("hello was refused: %+v", res.Error)
				}
				if len(res.Result) == 0 {
					t.Fatal("an ok answer with no result object")
				}
				return
			}
			checkRefusal(t, res, c.id, c.wantCode)
			if c.wantQuiet != "" && strings.Contains(got[len(got)-1], c.wantQuiet) {
				t.Errorf("the answer quotes the peer's args: %s", got[len(got)-1])
			}
		})
	}
}

// TestTheNegotiationGateOpensOnlyAfterHelloCrossesThePipe is the two peers meeting over an in-memory pipe: the far
// end is a real internal/peer endpoint sending length-prefixed frames, so the gate this process reads is moved only
// by a hello that crossed the transport.
func TestTheNegotiationGateOpensOnlyAfterHelloCrossesThePipe(t *testing.T) {
	hostEnd, extEnd := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- run(framedConfig(true), hostEnd, io.Discard) }()

	driver := peer.New(extEnd, peer.ToHost())
	go func() { _ = driver.Serve() }()
	t.Cleanup(func() { _ = driver.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := driver.Request(ctx, "req-pipe-before0", "connector.version", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("connector.version before the negotiation: %v", err)
	}
	checkRefusal(t, res, "req-pipe-before0", "HandshakeFailed")

	res, err = driver.Request(ctx, "req-pipe-hello000", "connector.hello", json.RawMessage(recordedHelloArgs))
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	if res.OK == nil || !*res.OK {
		t.Fatalf("the recorded compatible hello was refused: %+v", res.Error)
	}
	var negotiated struct {
		ProtocolVersion              int      `json:"protocolVersion"`
		MinCompatibleProtocolVersion int      `json:"minCompatibleProtocolVersion"`
		ConnectorVersion             string   `json:"connectorVersion"`
		Capabilities                 []string `json:"capabilities"`
	}
	if err := json.Unmarshal(res.Result, &negotiated); err != nil {
		t.Fatalf("hello answered %s: %v", res.Result, err)
	}
	if negotiated.ProtocolVersion != contract.ProtocolVersion ||
		negotiated.MinCompatibleProtocolVersion != contract.MinCompatibleProtocolVersion {
		t.Errorf("hello announced %d..%d, want the contract's %d..%d", negotiated.MinCompatibleProtocolVersion,
			negotiated.ProtocolVersion, contract.MinCompatibleProtocolVersion, contract.ProtocolVersion)
	}
	if got, want := strings.Join(negotiated.Capabilities, ","), strings.Join(contract.CapabilityNames(), ","); got != want {
		t.Errorf("the gate was handed %q, want the whole registry %q", got, want)
	}

	// A name the allowlist does not publish is answered on a live pipe, so the request after it still gets here.
	res, err = driver.Request(ctx, "req-pipe-snooze0", "core.snooze", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("the port died on an unknown method: %v", err)
	}
	checkRefusal(t, res, "req-pipe-snooze0", "UnknownMethod")

	res, err = driver.Request(ctx, "req-pipe-after00", "connector.version", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("connector.version after the negotiation: %v", err)
	}
	if res.OK == nil || !*res.OK {
		t.Fatalf("connector.version after the negotiation was refused: %+v", res.Error)
	}
	// The two fields its schema requires, and none of the four that belong to a Core supervisor, a pins reader and a
	// release tool this build does not have.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(res.Result, &fields); err != nil {
		t.Fatalf("connector.version answered %s: %v", res.Result, err)
	}
	if len(fields) != 2 {
		t.Errorf("answered %d fields (%s), want the two it holds", len(fields), res.Result)
	}
	for _, name := range []string{"coreVersion", "schemaVersion", "pinsVersion", "buildId"} {
		if _, ok := fields[name]; ok {
			t.Errorf("connector.version claimed %s it has never measured", name)
		}
	}
	if got, want := string(fields["connectorVersion"]), `"1.0.0"`; got != want {
		t.Errorf("connectorVersion = %s, want %s", got, want)
	}
	if got, want := string(fields["protocolVersion"]), "1"; got != want {
		t.Errorf("protocolVersion = %s, want the contract's %s", got, want)
	}

	_ = driver.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the host loop ended with %v, want a clean close when the port hangs up", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host loop never noticed the port closing")
	}
}

// TestTheFramedModeUsesThePrefixAndTheHostsOwnReadCeiling writes the length prefix the contract declares by hand, so
// the mode is checked against the framing rather than against itself, and hands it one oversized header: the
// extension-to-host number is the ceiling this endpoint reads at, and a frame above it ends the stream instead of
// being skipped.
func TestTheFramedModeUsesThePrefixAndTheHostsOwnReadCeiling(t *testing.T) {
	var in bytes.Buffer
	payload := []byte(requestLine(t, "req-frame-hello0", "connector.hello", recordedHelloArgs))
	var hdr [frames.PrefixBytes]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(payload)))
	in.Write(hdr[:])
	in.Write(payload)
	binary.LittleEndian.PutUint32(hdr[:], uint32(contract.ExtensionToHostBytes)+1)
	in.Write(hdr[:])

	var out bytes.Buffer
	err := run(framedConfig(true), duplex{io.NopCloser(bytes.NewReader(in.Bytes())), nopWriteCloser{&out}}, io.Discard)

	var tooLarge *frames.TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("an oversized inbound frame ended the run with %v, want the codec's size verdict", err)
	}
	if tooLarge.Limit != contract.ExtensionToHostBytes {
		t.Errorf("read at the %d B ceiling, want the extension-to-host %d B", tooLarge.Limit, contract.ExtensionToHostBytes)
	}

	data, rerr := frames.Read(bytes.NewReader(out.Bytes()), contract.HostToExtensionBytes)
	if rerr != nil {
		t.Fatalf("the hello answer never came back as a frame: %v", rerr)
	}
	res, perr := envelope.Parse(data)
	if perr != nil {
		t.Fatalf("answered a frame its own codec rejects: %v", perr)
	}
	checkRefusalOrOK(t, res, "req-frame-hello0")
}

func checkRefusalOrOK(t *testing.T, res *envelope.Frame, wantID string) {
	t.Helper()
	if res.InReplyTo == nil || *res.InReplyTo != wantID {
		t.Fatalf("answered %v, want inReplyTo %s", res.InReplyTo, wantID)
	}
	if res.OK == nil || !*res.OK || res.Error != nil {
		t.Fatalf("the recorded hello was refused on the framed transport: %+v", res.Error)
	}
}

type corpusPair struct {
	req    *envelope.Frame
	line   []byte
	answer *envelope.Frame
}

// replayable are the exchanges the fixture records that an unwired connector answers identically: the two hellos,
// and the request for a name the allowlist does not publish. Its connector.version, core.status and connector.health
// answers record a build with a Core, pins and probes wired, which this one is not, so they are not replayed here.
func replayable(t *testing.T) []corpusPair {
	t.Helper()
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "packages", "schema", "fixtures", "protocol", "handshake.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{"connector.hello": true, "core.snooze": true}

	answers := map[string]*envelope.Frame{}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	for i, line := range lines {
		f, err := envelope.Parse(line)
		if err != nil {
			t.Fatalf("line %d is a corpus frame this build's codec rejects: %v", i+1, err)
		}
		if f.Type == envelope.TypeResponse && f.InReplyTo != nil {
			answers[*f.InReplyTo] = f
		}
	}

	var out []corpusPair
	for i, line := range lines {
		f, err := envelope.Parse(line)
		if err != nil {
			t.Fatal(err)
		}
		if f.Type != envelope.TypeRequest || f.Method == nil || !names[*f.Method] {
			continue
		}
		answer, ok := answers[f.ID]
		if !ok {
			t.Fatalf("line %d records %s with no answer", i+1, *f.Method)
		}
		out = append(out, corpusPair{req: f, line: line, answer: answer})
	}
	if len(out) != 3 {
		t.Fatalf("the corpus replays as %d exchanges, want the two hellos and the unknown name", len(out))
	}
	return out
}

func TestTheRecordedHandshakeReplaysThroughThisDispatcher(t *testing.T) {
	// Every exchange replayable() returns goes through this build's own front door and is compared with the answer
	// the corpus recorded: the ok flag, the code, the retryable verdict, and the four hello fields both sides hold.
	for _, c := range replayable(t) {
		t.Run(*c.req.Method+"/"+c.req.ID, func(t *testing.T) {
			got, err := runJSONL(t, jsonlConfig(true), string(c.line))
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			res := answered(t, got, c.req.ID)
			if res.OK == nil || c.answer.OK == nil || *res.OK != *c.answer.OK {
				t.Fatalf("answered ok=%v, the corpus records ok=%v", res.OK, c.answer.OK)
			}
			if c.answer.Error == nil {
				// The four fields an unwired build does hold, read out of the recorded result as a check on the
				// projection rather than on this package's arithmetic.
				var mine, recorded struct {
					ProtocolVersion              int      `json:"protocolVersion"`
					MinCompatibleProtocolVersion int      `json:"minCompatibleProtocolVersion"`
					ConnectorVersion             string   `json:"connectorVersion"`
					Capabilities                 []string `json:"capabilities"`
				}
				if err := json.Unmarshal(res.Result, &mine); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(c.answer.Result, &recorded); err != nil {
					t.Fatal(err)
				}
				if mine.ProtocolVersion != recorded.ProtocolVersion ||
					mine.MinCompatibleProtocolVersion != recorded.MinCompatibleProtocolVersion ||
					mine.ConnectorVersion != recorded.ConnectorVersion ||
					strings.Join(mine.Capabilities, ",") != strings.Join(recorded.Capabilities, ",") {
					t.Errorf("answered %+v, the corpus records %+v", mine, recorded)
				}
				return
			}
			checkRefusal(t, res, c.req.ID, c.answer.Error.Code)
			tax, found := contract.LookupCode(c.answer.Error.Code)
			if !found || c.answer.Error.Retryable == nil || tax.Retryable != *c.answer.Error.Retryable {
				t.Errorf("the corpus records %s retryable=%v, the taxonomy says %v",
					c.answer.Error.Code, c.answer.Error.Retryable, tax.Retryable)
			}
		})
	}
}

func TestTheProcessRefusesToServeWithoutTheFlagsThatNameItsCaller(t *testing.T) {
	cases := []struct {
		name     string
		cfg      config
		wantFlag string
	}{
		{"no sender id", config{jsonl: true, version: "1.0.0"}, "--sender-id"},
		{"no product version", config{senderID: pinned, jsonl: true}, "--connector-version"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, notes bytes.Buffer
			in := requestLine(t, "req-nodefault-1", "connector.hello", recordedHelloArgs) + "\n"
			err := run(c.cfg, bufferPipe(t, in, &out), &notes)
			if err == nil || !strings.Contains(err.Error(), c.wantFlag) {
				t.Fatalf("err = %v, want it to name %s", err, c.wantFlag)
			}
			if out.Len() != 0 {
				t.Errorf("it served a frame anyway: %s", out.String())
			}
			if notes.Len() != 0 {
				t.Errorf("it printed %q before refusing to serve", notes.String())
			}
		})
	}
}

func TestAnAllowlistedFlagAbsentMeansNothingIsAllowed(t *testing.T) {
	cfg := config{senderID: pinned, jsonl: true, version: "1.0.0"}
	got, err := runJSONL(t, cfg, requestLine(t, "req-failclosed-1", "connector.hello", recordedHelloArgs))
	if err != nil {
		t.Fatal(err)
	}
	checkRefusal(t, answered(t, got, "req-failclosed-1"), "req-failclosed-1", "UnauthorizedExtension")
}

func TestTheStartupLineNamesTheIdentityInjectionAsAnAffordance(t *testing.T) {
	var out, notes bytes.Buffer
	in := requestLine(t, "req-affordance", "connector.hello", recordedHelloArgs) + "\n"
	if err := run(jsonlConfig(true), bufferPipe(t, in, &out), &notes); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(notes.String())
	for _, want := range []string{"--sender-id", "--allowlisted", "ODEC-35"} {
		if !strings.Contains(line, want) {
			t.Errorf("the startup line %q does not name %s", line, want)
		}
	}
	if strings.Count(notes.String(), "\n") != 1 {
		t.Errorf("the startup note is %d lines, want one: %q", strings.Count(notes.String(), "\n"), notes.String())
	}
}

func TestThisBuildRegistersOnlyTheMethodsItCanAnswer(t *testing.T) {
	s, err := newService(jsonlConfig(true))
	if err != nil {
		t.Fatal(err)
	}
	got := s.d.Methods()
	if len(got) != 2 || got[0] != "connector.hello" || got[1] != "connector.version" {
		t.Errorf("this build registers %v, want exactly connector.hello and connector.version", got)
	}
	for _, name := range got {
		if _, published := contract.LookupMethod(name); !published {
			t.Errorf("%s is registered but the allowlist does not publish it", name)
		}
	}
}

func TestTheVersionAnswerIgnoresWhatThePeerClaimsAboutItself(t *testing.T) {
	s, err := newService(jsonlConfig(true))
	if err != nil {
		t.Fatal(err)
	}
	f := mustParse(t, requestLine(t, "req-version-const", "connector.version", `{"connectorVersion":"9.9.9"}`))

	res, stop := s.respond(f)
	if stop != nil {
		t.Fatalf("connector.version before the negotiation ended the stream: %v", stop)
	}
	checkRefusal(t, res, "req-version-const", "HandshakeFailed")

	helloFrame := mustParse(t, requestLine(t, "req-version-hel0", "connector.hello", recordedHelloArgs))
	if _, stop := s.respond(helloFrame); stop != nil {
		t.Fatalf("the negotiation frame ended the stream: %v", stop)
	}
	if !s.sess.Negotiated() {
		t.Fatal("the hello this process answered did not open the gate")
	}

	res, stop = s.respond(f)
	if stop != nil {
		t.Fatalf("connector.version after the negotiation: %v", stop)
	}
	if res.OK == nil || !*res.OK {
		t.Fatalf("answered %+v, want ok", res.Error)
	}
	if want := `"connectorVersion":"1.0.0"`; !bytes.Contains(res.Result, []byte(want)) {
		t.Errorf("result %s does not answer this build's own version %s", res.Result, want)
	}
	if bytes.Contains(res.Result, []byte("9.9.9")) {
		t.Errorf("the answer quoted the peer's own version claim: %s", res.Result)
	}
}
