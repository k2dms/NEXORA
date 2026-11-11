// Package main is nexora-connector — the process that owns the front door. It reads one Native Messaging stream,
// hands every frame to internal/dispatch, and answers it with internal/hello.
//
// What it registers is what a build can answer without inventing a fact: `docs/PROJECT-STATE.md` §4.9's inventory of
// the negotiation and the version strings beside it, plus the two paths that join the components — `config.apply`,
// which compiles the intent the frame carries and hands the file to Core, and `server.test`, which probes the
// servers that apply put in force. Every other published method is a fact about an event pump, a settings store the
// Connector does not own (ADR-0010) or a diagnostics section nothing has written, so it stays unregistered and
// internal/dispatch refuses it with the code the contract gives rather than this build guessing an answer.
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
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/hello"
	"github.com/k2dms/nexora/connector/internal/keystore"
	"github.com/k2dms/nexora/connector/internal/peer"
	"github.com/k2dms/nexora/connector/internal/receipt"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

// config is what this process is told and cannot measure for itself: the sender id the host resolved on hop 1, how
// far that sender is trusted, which release this binary is, and which transport the caller wants. ADR-0022 rule 1
// makes the first two the decision's own mechanism rather than a testing convenience: the host opens one pipe per
// sender, so the id it injects when it spawns this process for that pipe is the same fact as "which pipe the frame
// arrived on". `framing.json#/senderPolicy/identity/notIdentity` rules out the *peer's* command line, which is not this one.
type config struct {
	senderID    string
	allowlisted bool
	jsonl       bool
	version     string

	// stateDir, configDir, keystoreDir and coreDir are the per-user layout of doc 34 §7, overridable one flag
	// each because a test or a sandboxed run must be able to point them at a temp tree. The empty string means
	// the default: %LOCALAPPDATA%\NEXORA\ and the subdirectory of the same name, which is what production uses.
	stateDir    string
	configDir   string
	keystoreDir string
	coreDir     string
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

// nexoraRoot is the per-user tree of doc 34 §7: %LOCALAPPDATA%\NEXORA. The same root the keystore's
// own platform helper resolves, stated here for the three directories beside it.
func nexoraRoot() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("%LOCALAPPDATA% is empty, and doc 34 §7 names no other per-user root for the Connector's state, config and keystore")
	}
	return filepath.Join(base, "NEXORA"), nil
}

// defaultDirs is the layout one flag-less run uses. Every directory is a subdirectory of one root, and the
// keystore's own helper must agree with the keystore entry, so the root is computed once and shared.
func defaultDirs() (state, config, keystore, core string, err error) {
	root, err := nexoraRoot()
	if err != nil {
		return "", "", "", "", err
	}
	return filepath.Join(root, "state"), filepath.Join(root, "config"), filepath.Join(root, "keystore"), filepath.Join(root, "core"), nil
}

// coreLaunch is ODEC-44's answer as a function: the supervisor's Launch reads the install receipt at the core
// directory per spawn, and the receipt names the active binary. A receipt that does not exist is Core not being
// installed yet (the first-run download of doc 33 §5 has not run), and the error travels into the start's
// refusal instead of a directory listing being trusted.
func coreLaunch(coreDir string) supervisor.Launch {
	return func(configPath string) (*exec.Cmd, error) {
		r, err := receipt.Load(filepath.Join(coreDir, "receipt.json"))
		if err != nil {
			return nil, fmt.Errorf("Core is not installed: the install receipt could not be read (%v); the first-run download has not run or the install is broken (doc 33 §5, VER-41)", err)
		}
		bin := r.Path
		if _, err := os.Stat(bin); err != nil {
			return nil, fmt.Errorf("Core is not installed: the receipt names %s and the file is not there (doc 33 §7: receipts, not directory listings)", bin)
		}
		return exec.Command(bin, "run", "-config", configPath), nil
	}
}

// binaryName is the platform spelling of the Xray executable inside the install root.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "xray.exe"
	}
	return "xray"
}

func newService(cfg config) (*service, error) {
	responder, err := hello.New(hello.Facts{ConnectorVersion: cfg.version})
	if err != nil {
		return nil, err
	}
	handlers := map[string]dispatch.Handler{
		"connector.hello":   responder.Handle,
		"connector.version": versionResponder(cfg.version),
	}
	// The config.apply surface: the directories of doc 34 §7 (flags first, per-user defaults under them), the
	// platform's own keystore protector, and the supervisor whose Launch resolves the pinned binary per spawn.
	// A wiring that cannot be built — no per-user root, no protector — is a process that refuses to start rather
	// than one that answers the method with guesses.
	stateDir, configDir, keystoreDir, coreDir := cfg.stateDir, cfg.configDir, cfg.keystoreDir, cfg.coreDir
	if stateDir == "" || configDir == "" || keystoreDir == "" || coreDir == "" {
		state, conf, keys, core, err := defaultDirs()
		if err != nil {
			return nil, err
		}
		if stateDir == "" {
			stateDir = state
		}
		if configDir == "" {
			configDir = conf
		}
		if keystoreDir == "" {
			keystoreDir = keys
		}
		if coreDir == "" {
			coreDir = core
		}
	}
	protector, err := keystore.NewSystemProtector()
	if err != nil {
		return nil, fmt.Errorf("no keystore protector on this platform: %w", err)
	}
	store, err := keystore.New(keystoreDir, protector)
	if err != nil {
		return nil, fmt.Errorf("the keystore could not be opened at %s: %w", keystoreDir, err)
	}
	sup, err := supervisor.New(supervisor.Config{
		Dir:    stateDir,
		Launch: coreLaunch(coreDir),
		Verify: receipt.NewVerifier(filepath.Join(coreDir, "receipt.json")),
		Death: supervisor.Death{
			Attempts:  3,
			Backoff:   []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
			Stability: 300 * time.Second,
			Notify:    true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("the Core supervisor could not be built: %w", err)
	}
	ca, err := newConfigApply(applyConfig{StateDir: stateDir, ConfigDir: configDir},
		fileStateRecord{dir: stateDir}, store, sup)
	if err != nil {
		return nil, fmt.Errorf("the config.apply surface could not be built: %w", err)
	}
	handlers["config.apply"] = ca.Handle
	st, err := newHealthSurface(ca)
	if err != nil {
		return nil, fmt.Errorf("the server.test surface could not be built: %w", err)
	}
	handlers["server.test"] = st.Handle
	cs, err := newCoreStatus(sup, coreDir)
	if err != nil {
		return nil, fmt.Errorf("the core.status surface could not be built: %w", err)
	}
	handlers["core.status"] = cs.Handle
	d, err := dispatch.New(dispatch.Config{Handlers: handlers})
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
		return errors.New("no --sender-id: this process serves no frame whose sender the host has not resolved, and ADR-0022 rule 1 makes that resolution the host's spawn-time injection rather than anything readable off the pipe")
	}
	if cfg.version == "" {
		return errors.New("no --connector-version, and this binary's build info carries no version: connector.hello answers a product version it actually has")
	}
	fmt.Fprint(notes, "nexora-connector: --sender-id and --allowlisted are ADR-0022 rule 1's spawn-time injection — the host names the sender it resolved for the pipe it opened; run by hand you are supplying that answer yourself\n")

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

// releaseVersion is the product version the release passes in:
// `go build -ldflags "-X main.releaseVersion=0.1.0" ./cmd/nexora-connector`. It exists because the host starts this
// process with no arguments of its own (ADR-0022 rule 1), so the binary has to know what it answers for; a build
// from a checkout leaves it empty and the process refuses to start rather than invent a version — Go reports
// "(devel)" for a main module whatever tags exist, so nothing stamps it here by itself.
var releaseVersion string

// buildVersion is the version this binary was released with, or the empty string when it was not. --connector-version
// is the operator's way of naming the release the same way; internal/hello checks the shape either way.
func buildVersion() string {
	if releaseVersion != "" {
		return strings.TrimPrefix(releaseVersion, "v")
	}
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
	senderID := fs.String("sender-id", "", "the sender id the host resolved on hop 1, injected by the process that opened this pipe (ADR-0022 rule 1); an operator running this binary by hand passes the answer themselves")
	allowlisted := fs.Bool("allowlisted", false, "whether that id is in the host's on-disk allowlist; false refuses every frame, so it is the default")
	jsonl := fs.Bool("jsonl", false, "one frame per stdin line and one response per stdout line; a shell affordance, not the contract's transport")
	version := fs.String("connector-version", buildVersion(), "product version connector.hello and connector.version answer with")
	stateDir := fs.String("state-dir", "", "override of the per-user state directory (doc 34 §7); tests and sandboxes only, production uses the default")
	configDir := fs.String("config-dir", "", "override of the per-user compiled-config directory (doc 34 §7)")
	keystoreDir := fs.String("keystore-dir", "", "override of the per-user keystore directory (ADR-0011)")
	coreDir := fs.String("core-dir", "", "override of the pinned Core install root (doc 33 §7)")
	_ = fs.Parse(os.Args[1:])

	cfg := config{senderID: *senderID, allowlisted: *allowlisted, jsonl: *jsonl, version: *version,
		stateDir: *stateDir, configDir: *configDir, keystoreDir: *keystoreDir, coreDir: *coreDir}
	if err := run(cfg, duplex{os.Stdin, os.Stdout}, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "nexora-connector: %v\n", err)
		os.Exit(1)
	}
}
