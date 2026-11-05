// Package main is nexora-connector — the process that owns the front door. It reads one Native Messaging stream,
// hands every frame to internal/dispatch, and answers it with internal/hello.
//
// What it registers is what an unwired build can honestly say, which is `docs/PROJECT-STATE.md` §4.9's inventory:
// the negotiation, and the version strings beside it. `connector.version` is here because both fields its result
// requires are data this process holds — its own release and `contract.ProtocolVersion` — and its args carry nothing
// the answer depends on. Every other published method is a fact about Xray Core, the keystore, an applied config or
// an event pump this build does not have, so it stays unregistered and internal/dispatch refuses it with the code the
// contract gives rather than this build guessing an answer.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/hello"
	"github.com/k2dms/nexora/connector/internal/peer"
)

// config is what this process is told and cannot measure for itself: the host's platform-resolved answer injected by
// an operator, how far that sender is trusted, which release this binary is, and which transport the caller wants.
// The transport that would carry the resolved id in production is not in the contract — see ODEC-35 — and
// `framing.json#/senderPolicy/identity/notIdentity` rules out the *peer's* command line, which is not this one.
type config struct {
	senderID    string
	allowlisted bool
	jsonl       bool
	version     string
}

// duplex joins the connector's two descriptors into the single io.ReadWriteCloser internal/peer takes: Native
// Messaging hands a host stdin for reads and stdout for writes, and nothing joins them for it.
type duplex struct {
	in  io.ReadCloser
	out io.WriteCloser
}

func (d duplex) Read(p []byte) (int, error)  { return d.in.Read(p) }
func (d duplex) Write(p []byte) (int, error) { return d.out.Write(p) }

func (d duplex) Close() error {
	if err := d.in.Close(); err != nil {
		return err
	}
	return d.out.Close()
}

// service is one connection's front door: the process-wide dispatcher, this pipe's negotiation, and the identity the
// host resolved before it read a byte of payload.
type service struct {
	d           *dispatch.Dispatcher
	sess        *hello.Session
	senderID    string
	allowlisted bool

	// p and stop are set only by the framed mode: the peer reads its own endpoint, and a refusal that says the
	// stream ends instead of an answer is carried out of Serve as the error the caller prints.
	p    *peer.Peer
	stop error
}

func newService(cfg config) (*service, error) {
	responder, err := hello.New(hello.Facts{ConnectorVersion: cfg.version})
	if err != nil {
		return nil, err
	}
	d, err := dispatch.New(dispatch.Config{Handlers: map[string]dispatch.Handler{
		"connector.hello":   responder.Handle,
		"connector.version": versionResponder(cfg.version),
	}})
	if err != nil {
		return nil, err
	}
	return &service{d: d, sess: new(hello.Session), senderID: cfg.senderID, allowlisted: cfg.allowlisted}, nil
}

// respond is the one dispatch path the two transports share, so --jsonl cannot diverge from the contract's own:
// exactly one of the answer frame and the reason the stream ends is non-nil.
func (s *service) respond(f *envelope.Frame) (*envelope.Frame, error) {
	result, refusal := s.d.Dispatch(context.Background(), s.sess.Identity(s.senderID, s.allowlisted), f)
	s.sess.Record(result, refusal)
	if refusal == nil {
		res, err := f.Respond(result)
		if err != nil {
			return nil, err
		}
		return res, nil
	}
	if refusal.Close {
		// The amplification guard's answer is a closed port and no code at all, so there is nothing to send.
		return nil, errors.New(refusal.Message)
	}
	res, err := f.Fail(refusal.Code, refusal.Message)
	if err != nil {
		// A refusal whose code is outside the taxonomy has no answer a peer could act on (ADR-0007 rule 6).
		return nil, err
	}
	return res, nil
}

// serveFramed is the contract's transport. The direction pair is the host endpoint's in either hop, because the
// frame arriving on this pipe is the extension's own (`packages/schema/protocol/framing.json`).
func (s *service) serveFramed(pipe io.ReadWriteCloser) error {
	cfg := peer.ToExtension()
	cfg.Handler = s.answerOnPipe
	s.p = peer.New(pipe, cfg)
	err := s.p.Serve()
	if s.stop != nil {
		return s.stop
	}
	return err
}

func (s *service) answerOnPipe(f *envelope.Frame) {
	res, stop := s.respond(f)
	if stop != nil {
		s.stop = stop
		_ = s.p.Close()
		return
	}
	if err := s.p.Send(res); err != nil {
		// The peer stopped listening; its read loop reports why, so there is no second error to raise here.
		_ = s.p.Close()
	}
}

// serveLines is the terminal affordance — one frame per stdin line, one response per stdout line, exit at EOF —
// justified because the length-prefixed mode above is the contract's transport and a human at a shell cannot type
// four length bytes. It shares service.respond, so it makes no decision of its own.
func (s *service) serveLines(pipe io.ReadWriteCloser) error {
	sc := bufio.NewScanner(pipe)
	// The framed mode's own read ceiling, so a frame the host may not receive cannot be typed at it either.
	sc.Buffer(make([]byte, 64*1024), contract.ExtensionToHostBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		f, err := envelope.Parse(line)
		if err != nil {
			// internal/peer ends the stream on a frame the codec rejects, and this mode follows it rather than
			// inventing a second answer to a desynchronised pipe.
			return fmt.Errorf("inbound frame rejected: %w", err)
		}
		res, stop := s.respond(f)
		if stop != nil {
			return stop
		}
		data, err := res.Marshal()
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(pipe, "%s\n", data); err != nil {
			return err
		}
	}
	return sc.Err()
}

func run(cfg config, pipe io.ReadWriteCloser, notes io.Writer) error {
	// Nothing here may default to a permissive caller: the identity is the platform's answer, and a build that
	// guessed it would be the fallthrough ADR-0004 rule 3 forbids. --allowlisted therefore defaults false.
	if cfg.senderID == "" {
		return errors.New("no --sender-id: this process serves no frame whose sender the host has not resolved, and the transport that would carry that answer is not in the contract (ODEC-35)")
	}
	if cfg.version == "" {
		return errors.New("no --connector-version, and this binary's build info carries no version: connector.hello answers a product version it actually has")
	}
	fmt.Fprint(notes, "nexora-connector: demo affordance — --sender-id and --allowlisted inject the answer the host's platform will resolve in production; the transport that carries it is undecided (ODEC-35)\n")

	s, err := newService(cfg)
	if err != nil {
		return err
	}
	if cfg.jsonl {
		return s.serveLines(pipe)
	}
	return s.serveFramed(pipe)
}

// versionAnswer is connector.version's result: the two fields its schema requires, both of which this process holds.
// coreVersion, schemaVersion, pinsVersion and buildId are optional and belong to the Core supervisor, the pins
// reader and the release tooling this build does not have, so they are absent rather than guessed — internal/hello's
// stance for the same fields.
type versionAnswer struct {
	ConnectorVersion string `json:"connectorVersion"`
	ProtocolVersion  int    `json:"protocolVersion"`
}

// versionResponder answers connector.version, which `packages/schema/protocol/commands/connector.version.json`
// describes as the same strings hello carries minus the compatibility intersection. Its args are declared over no
// properties, and nothing in them changes the answer, so there is nothing here to misread.
func versionResponder(version string) dispatch.Handler {
	return func(context.Context, *envelope.Frame) (any, *dispatch.Refusal) {
		return &versionAnswer{ConnectorVersion: version, ProtocolVersion: contract.ProtocolVersion}, nil
	}
}

// buildVersion is the version Go stamps into a binary built at a tagged module version. A local build reports
// "(devel)", which is not a product version, so --connector-version is how the operator names the release this
// process answers for instead of the process inventing one; internal/hello checks the shape either way.
func buildVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	v := strings.TrimPrefix(bi.Main.Version, "v")
	if !strings.Contains(v, ".") {
		return ""
	}
	return v
}

func main() {
	fs := flag.NewFlagSet("nexora-connector", flag.ExitOnError)
	senderID := fs.String("sender-id", "", "the sender id the host's platform resolved for this port, injected by the operator; see ODEC-35")
	allowlisted := fs.Bool("allowlisted", false, "whether that id is in the host's on-disk allowlist; false refuses every frame, so it is the default")
	jsonl := fs.Bool("jsonl", false, "one frame per stdin line and one response per stdout line; a shell affordance, not the contract's transport")
	version := fs.String("connector-version", buildVersion(), "product version connector.hello and connector.version answer with")
	_ = fs.Parse(os.Args[1:])

	cfg := config{senderID: *senderID, allowlisted: *allowlisted, jsonl: *jsonl, version: *version}
	if err := run(cfg, duplex{os.Stdin, os.Stdout}, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "nexora-connector: %v\n", err)
		os.Exit(1)
	}
}
