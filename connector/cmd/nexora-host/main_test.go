package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go/parser"
	"go/token"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/frames"
)

// doc 22 §2 gives this process four jobs and one prohibition, and each is measured for the way it could be getting
// it wrong: the import graph that must hold no domain knowledge, the sender the browser's argv resolves, the
// allowlist that decides whether a Connector is started at all, the pump that must not understand what it moves,
// and the byte ceiling that refuses before a frame is handed on.

const (
	pinnedOrigin = "chrome-extension://abcdefghijklmnopabcdefghijklmnop"

	// childEnv marks the process TestThePipeIsARealProcess runs this very binary as. A Go test binary cannot be
	// handed the Connector's flags — its own framework would reject them — so the child names an entry point
	// instead and the marker tells it that it is the child.
	childEnv = "NEXORA_HOST_TEST_CHILD"
)

func requestPayload(t *testing.T, id, method, args string) []byte {
	t.Helper()
	f, err := envelope.NewRequest(id, method, json.RawMessage(args))
	if err != nil {
		t.Fatalf("building a %s request: %v", method, err)
	}
	data, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFrame(t *testing.T, w io.Writer, payload []byte) {
	t.Helper()
	if err := frames.Write(w, payload, contract.HostToExtensionBytes); err != nil {
		t.Fatalf("writing a frame: %v", err)
	}
}

func readFrame(t *testing.T, r io.Reader) []byte {
	t.Helper()
	payload, err := frames.Read(r, contract.HostToExtensionBytes)
	if err != nil {
		t.Fatalf("reading a frame: %v", err)
	}
	return payload
}

// allowlistFile is the host's own startup configuration, in the one shape framing.json describes without naming.
func allowlistFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "allowlist.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---- doc 22 §2 rule 3: no domain knowledge ---------------------------------------------------------------

// TestNoDomainKnowledgeReachesThisProcess is the import-graph assertion the invariant says is testable. It reads
// this package's own non-test import declarations — the graph a build compiles into the shim — and refuses by name
// every package that would hand it something to decide: the compiler, the applier, the keystore, the dispatcher,
// the negotiation, the supervisor, the prober, the receipt verifier.
//
// The closure below those declarations is `go list -deps`'s job and needs the toolchain at test time, which no
// test in this module does today. What is asserted here is the half the rule states: that the shim never reaches
// for domain knowledge itself.
func TestNoDomainKnowledgeReachesThisProcess(t *testing.T) {
	forbidden := []string{
		"internal/compile", "internal/apply", "internal/keystore", "internal/dispatch",
		"internal/hello", "internal/supervisor", "internal/health", "internal/receipt",
	}
	files, err := parser.ParseDir(token.NewFileSet(), ".", func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing this package: %v", err)
	}
	var got []string
	for _, pkg := range files {
		for _, f := range pkg.Files {
			for _, imp := range f.Imports {
				got = append(got, strings.Trim(imp.Path.Value, `"`))
			}
		}
	}
	if len(got) == 0 {
		t.Fatal("the parse found no imports at all, which asserts nothing about a graph")
	}
	for _, want := range []string{"internal/contract", "internal/envelope", "internal/frames"} {
		if !slices.ContainsFunc(got, func(p string) bool { return strings.HasSuffix(p, want) }) {
			t.Errorf("framing, the envelope and its numbers are this process's own job; %s is missing from %v", want, got)
		}
	}
	for _, imp := range got {
		for _, bad := range forbidden {
			if strings.HasSuffix(imp, bad) {
				t.Errorf("doc 22 §2 rule 3: nexora-host must not import %s", bad)
			}
		}
	}
}

// ---- the sender, from what the browser handed this process -----------------------------------------------

func TestSenderComesFromTheBrowsersArgv(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"chrome on linux and mac passes only the origin", []string{pinnedOrigin}, pinnedOrigin},
		{"chrome on windows adds a window handle beside it", []string{pinnedOrigin, "--parent-window=12345"}, pinnedOrigin},
		{
			"firefox passes the manifest path and then the add-on id",
			[]string{filepath.Join("C:", "Users", "dms", "Roaming", "nexora-host.json"), "nexora@example.com"},
			"nexora@example.com",
		},
		{"a trailing slash is the same origin", []string{"moz-extension://abc-def-ghi/"}, "moz-extension://abc-def-ghi/"},
		{"nothing recognisable is on the command line", []string{"--parent-pid=99"}, ""},
		{"a third argument is not the documented firefox shape", []string{"host.json", "nexora@example.com", "extra"}, ""},
		{"an empty argv is an unresolved caller", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstOrigin(tc.args); got != tc.want {
				t.Errorf("firstOrigin(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

func TestConnectorArgvCarriesTheResolvedIdOnce(t *testing.T) {
	got := connectorArgs(pinnedOrigin)
	want := []string{"--sender-id", pinnedOrigin, "--allowlisted=true"}
	if !slices.Equal(got, want) {
		t.Errorf("ADR-0022 rule 1's injection is the argv the child starts with:\n got %v\nwant %v", got, want)
	}
}

// ---- the allowlist, and what refusing costs --------------------------------------------------------------

func TestUnallowlistedSenderIsRefusedWithoutAConnector(t *testing.T) {
	path := allowlistFile(t, `["`+pinnedOrigin+`"]`)
	spawns := 0
	open := func(string) (io.ReadWriteCloser, error) {
		spawns++
		return nil, errors.New("the shim opened hop 2 for a sender it had refused")
	}
	cfg := config{senderID: "chrome-extension://otherotherotherotherother0", allowlistPath: path}

	browser, ext := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- run(cfg, browser, open, io.Discard) }()

	for i := 0; i < 3; i++ {
		writeFrame(t, ext, requestPayload(t, "abcdefgh", "config.apply", `{}`))
		f, err := envelope.Parse(readFrame(t, ext))
		if err != nil {
			t.Fatalf("refusal %d was not a frame the codec accepts: %v", i, err)
		}
		if f.Type != envelope.TypeResponse || f.InReplyTo == nil || *f.InReplyTo != "abcdefgh" {
			t.Errorf("refusal %d is not an answer in reply to its own request: type %s, inReplyTo %v", i, f.Type, f.InReplyTo)
		}
		if f.Error == nil || f.Error.Code != "UnauthorizedExtension" {
			t.Errorf("refusal %d carries %v, want UnauthorizedExtension", i, f.Error)
		} else if f.Error.Retryable == nil || *f.Error.Retryable {
			t.Error("the taxonomy declares UnauthorizedExtension not retryable, and a refusal a client retries is a loop")
		}
	}
	_ = ext.Close()
	if err := <-done; err != nil {
		t.Errorf("the run ended with %v; a port the extension closed is the end of a stream, not a failure", err)
	}
	if spawns != 0 {
		t.Errorf("ADR-0004 rule 6: %d Connector processes started for a sender that must be refused", spawns)
	}
}

func TestAMissingAllowlistRefusesRatherThanAdmits(t *testing.T) {
	var notes strings.Builder
	open := func(string) (io.ReadWriteCloser, error) {
		return nil, errors.New("a shim with no allowlist must not start a Connector")
	}
	cfg := config{senderID: pinnedOrigin, allowlistPath: filepath.Join(t.TempDir(), "absent.json")}

	browser, ext := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- run(cfg, browser, open, &notes) }()

	writeFrame(t, ext, requestPayload(t, "abcdefgh", "connector.hello", `{}`))
	f, err := envelope.Parse(readFrame(t, ext))
	if err != nil {
		t.Fatal(err)
	}
	if f.Error == nil || f.Error.Code != "UnauthorizedExtension" {
		t.Errorf("fail-open: a missing allowlist answered %v", f.Error)
	}
	if !strings.Contains(notes.String(), "absent.json") {
		t.Errorf("a broken install has to be legible; notes were %q", notes.String())
	}
	_ = ext.Close()
	<-done
}

func TestRefusalsPastTheGuardCloseThePort(t *testing.T) {
	// A clock that never moves puts every refusal in one window.
	still := time.Unix(1_700_000_000, 0)
	cfg := config{senderID: pinnedOrigin, allowlistPath: allowlistFile(t, `[]`), now: func() time.Time { return still }}

	browser, ext := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(cfg, browser, func(string) (io.ReadWriteCloser, error) {
			return nil, errors.New("the sender was never allowlisted, so hop 2 never opens")
		}, io.Discard)
	}()

	// The guard answers RefusalsPerWindowMax times and then stops answering at all, so the request after them is
	// read and the stream ends. Nothing deadlocks: the shim reads a frame before it counts the refusal it owes.
	replies := make(chan int, 1)
	go func() {
		n := 0
		for n < contract.RefusalsPerWindowMax {
			if _, err := frames.Read(ext, contract.HostToExtensionBytes); err != nil {
				break
			}
			n++
		}
		replies <- n
	}()
	for i := 0; i < contract.RefusalsPerWindowMax+1; i++ {
		writeFrame(t, ext, requestPayload(t, "abcdefgh", "connector.hello", `{}`))
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "refusals in") {
			t.Errorf("the amplification guard answered with %v, want the window closing the port", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the guard never fired: the port is still answering refusals")
	}
	if n := <-replies; n != contract.RefusalsPerWindowMax {
		t.Errorf("%d refusals answered in the window, want the guard's own %d", n, contract.RefusalsPerWindowMax)
	}
	_ = ext.Close()
}

// ---- the pump: verbatim, both directions, and the ceiling ------------------------------------------------

// echoPipe is a Connector stand-in: it answers every frame with the bytes it received and pushes one event of its
// own, understanding neither. The test asserts the bytes come back identical, which a shim that parsed and
// re-marshalled could not keep true — a re-encoded frame re-stamps sentAt and mints a new id.
func echoPipe(t *testing.T, conn net.Conn, pushed chan<- struct{}) {
	t.Helper()
	// The unsolicited event first: a leg that waited for a request before moving an answer would queue it, and
	// queueing is doc 22 §2's own non-goal.
	event := []byte(`{"v":1,"id":"event00001","type":"event","sentAt":"2026-11-10T00:00:00Z","progress":{"stage":"core","event":"core.state"}}`)
	if err := frames.Write(conn, event, contract.HostToExtensionBytes); err != nil {
		t.Errorf("pushing an event: %v", err)
		return
	}
	pushed <- struct{}{}
	for {
		payload, err := frames.Read(conn, contract.ExtensionToHostBytes)
		if err != nil {
			return
		}
		if err := frames.Write(conn, payload, contract.HostToExtensionBytes); err != nil {
			return
		}
	}
}

func TestFramesCrossBothPipesVerbatim(t *testing.T) {
	shimEnd, connectorEnd := net.Pipe()
	pushed := make(chan struct{}, 1)
	go echoPipe(t, connectorEnd, pushed)

	var gotID string
	open := func(senderID string) (io.ReadWriteCloser, error) {
		gotID = senderID
		return shimEnd, nil
	}
	cfg := config{senderID: pinnedOrigin, allowlistPath: allowlistFile(t, `["`+pinnedOrigin+`"]`)}

	browser, ext := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- run(cfg, browser, open, io.Discard) }()

	<-pushed
	// The event arrived with no request in flight, which is the second leg doing its job and the pump proving it
	// does not only answer questions.
	event := readFrame(t, ext)
	if !strings.Contains(string(event), `"type":"event"`) {
		t.Errorf("the event the Connector pushed arrived at the extension as %s", event)
	}

	// A method and an args object this process could not name, carried unchanged: doc 22 §2's Out in one assertion.
	payload := requestPayload(t, "servertest", "server.test", `{"serverId":"-nexora.server.0f0f0f0f-1111-2222-3333-444444444444"}`)
	writeFrame(t, ext, payload)
	back := readFrame(t, ext)
	if !slices.Equal(back, payload) {
		t.Errorf("the frame came back changed:\n got %s\nwant %s", back, payload)
	}
	if gotID != pinnedOrigin {
		t.Errorf("hop 2 opened for %q, want the sender the browser handed the shim: %q", gotID, pinnedOrigin)
	}

	_ = ext.Close()
	if err := <-done; err != nil {
		t.Errorf("a closed port ended the run with %v; the contract says a closed port is the end of a stream", err)
	}
}

func TestAnOversizedFrameNeverReachesHop2(t *testing.T) {
	shimEnd, connectorEnd := net.Pipe()
	forwarded := make(chan int, 1)
	go func() {
		payload, err := frames.Read(connectorEnd, contract.ExtensionToHostBytes)
		if err != nil {
			forwarded <- -1
			return
		}
		forwarded <- len(payload)
	}()
	open := func(string) (io.ReadWriteCloser, error) { return shimEnd, nil }
	cfg := config{senderID: pinnedOrigin, allowlistPath: allowlistFile(t, `["`+pinnedOrigin+`"]`)}

	browser, ext := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- run(cfg, browser, open, io.Discard) }()

	// The prefix declares a payload one byte over the extension-to-host ceiling and is not followed by it. The
	// codec refuses at the prefix, the only point where a frame this large can be refused without already having
	// been read; a receiver that truncated would desynchronise the stream.
	var hdr [frames.PrefixBytes]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(contract.ExtensionToHostBytes+1))
	if _, err := ext.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		var tooLarge *frames.TooLargeError
		if !errors.As(err, &tooLarge) {
			t.Errorf("the oversized frame ended the run with %v, want the limit error naming the ceiling", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("neither forwarded nor refused: the shim is sitting on an oversized frame")
	}
	select {
	case n := <-forwarded:
		if n >= 0 {
			t.Errorf("a frame over the ceiling reached hop 2: %d bytes", n)
		}
	default:
	}
	_ = ext.Close()
}

// ---- hop 2 across a real process boundary ----------------------------------------------------------------

// TestThePipeIsARealProcess is the half a net.Pipe cannot show: the child the shim spawns sees its own stdin and
// stdout as the pipe, answers the frame it is handed with the same bytes, and is reaped when the port closes
// rather than killed. The id travels by argv in production, which
// TestConnectorArgvCarriesTheResolvedIdOnce pins; what is measured here is that a real process on the far end of
// hop 2 completes the pump and ends on the EOF the shim sends it.
//
// The child leaves by os.Exit so the Go test framework never prints its verdict onto the stdout this pipe is made
// of — a PASS line would reach the shim as a length prefix, which is the desynchronisation doc 25 §3 refuses.
func TestThePipeIsARealProcess(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		payload, err := frames.Read(os.Stdin, contract.ExtensionToHostBytes)
		if err != nil {
			os.Exit(1)
		}
		if err := frames.Write(os.Stdout, payload, contract.HostToExtensionBytes); err != nil {
			os.Exit(1)
		}
		for {
			if _, err := frames.Read(os.Stdin, contract.ExtensionToHostBytes); err != nil {
				os.Exit(0)
			}
		}
	}

	cfg := config{senderID: pinnedOrigin, allowlistPath: allowlistFile(t, `["`+pinnedOrigin+`"]`)}
	open := spawn(os.Args[0], func(string) []string {
		return []string{"-test.run=TestThePipeIsARealProcess", "-test.count=1"}
	}, []string{childEnv + "=1"})

	browser, ext := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- run(cfg, browser, open, io.Discard) }()

	payload := requestPayload(t, "realproc01", "connector.hello", `{}`)
	writeFrame(t, ext, payload)
	back := readFrame(t, ext)
	if !slices.Equal(back, payload) {
		t.Errorf("across a real process boundary the frame came back changed:\n got %s\nwant %s", back, payload)
	}
	_ = ext.Close()
	if err := <-done; err != nil {
		t.Errorf("the run ended with %v; the child exits when the port closes and the shim reaps it", err)
	}
}
