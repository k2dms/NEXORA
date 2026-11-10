// Package main is nexora-host — ADR-0005's first process and the only thing the browser can reach. Its jobs are
// the four doc 22 §2 lists: frame hop 1, resolve the sender, open the Connector's pipe for that sender, pump the
// bytes between the two pipes verbatim.
//
// Two of those are refusals to know something. It does not read a payload:
// `packages/schema/protocol/framing.json#/senderPolicy/rateLimitFacts/countedWhere` puts the method-shaped budgets
// in the Connector precisely because a method name lives inside the payload and `docs/architecture/25-native-
// messaging-protocol.md` §2 forbids this process to interpret one; what the shim enforces is what an opened port
// and a frame header answer without interpretation. And it owns no state (ADR-0005 rule 2), so the most it can do
// for a sender it does not know is answer `UnauthorizedExtension` and nothing else (ADR-0004 rule 6) and, past the
// amplification guard, hang up.
//
// Hop 2 is `docs/adr/0022-hop-2-identity-is-which-pipe.md`: one pipe per sender, and the sender is which pipe the
// host opened. This build reaches that fact by spawning the Connector with the resolved id injected once at
// startup, so `--sender-id` in the child is the decision's own mechanism rather than an operator's guess. Serving a
// *second* port for one sender is the naming half of the same decision — one Connector process, several pipes — and
// with it the adopt-or-refuse question `docs/PROJECT-STATE.md`'s `ODEC-01` still holds; until that is answered this
// process serves the one port the browser gave it.
//
// What doc 22 §2 rule 3 forbids is asserted, not promised: main_test.go reads this package's import declarations
// and refuses the config compiler, the keystore and the command handlers by name.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/frames"
)

// config is what this process is told and cannot measure for itself: the sender the browser handed it, where the
// Connector binary and the startup allowlist live, and the clock the refusal window reads.
type config struct {
	senderID      string
	connectorBin  string
	allowlistPath string

	// now is the amplification guard's clock. Tests pass a fake one; production leaves it nil and runs on time.
	now func() time.Time
}

// duplex joins the two descriptors the browser gave this process into the single endpoint internal/frames reads
// and writes. Native Messaging hands stdin for reads and stdout for writes, and nothing joins them for it.
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

// hop2 opens the Connector's side of the pipe for one sender. The signature is ADR-0022 rule 1 read as a
// function: the input is the resolved id, the output is a pipe, and the id travels into the child at startup
// because the pipe *is* the child — which pipe this host opened is the same fact as which child it spawned.
type hop2 func(senderID string) (io.ReadWriteCloser, error)

// connectorArgs is the injection itself. `--allowlisted` travels beside the id because the flag's whole point is
// the answer the host's platform produced on hop 1: the Connector cannot consult the allowlist (it is the host's
// file, and doc 22 §2 gives the shim the port) and must not guess it either.
func connectorArgs(senderID string) []string {
	return []string{"--sender-id", senderID, "--allowlisted=true"}
}

// spawn is the process boundary: start a child with the argv the caller resolves for this sender and hand back
// the two descriptors that make its pipe. Production passes connectorArgs and no extra environment; the test that
// covers a real process across hop 2 names its own child entry point instead, because a Go test binary cannot be
// handed a flag its framework does not know.
func spawn(bin string, argv func(senderID string) []string, env []string) hop2 {
	return func(senderID string) (io.ReadWriteCloser, error) {
		cmd := exec.Command(bin, argv(senderID)...)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		cmd.Stderr = os.Stderr
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("the Connector could not be started at %s: %w (doc 33 §7 installs it beside this binary, and ADR-0022 rule 1 makes it the process this host opens a pipe for)", bin, err)
		}
		return &child{in: in, out: out, cmd: cmd}, nil
	}
}

// child is the Connector process seen as one pipe. Close ends hop 2 the way the contract wants a port closed: the
// child's own read loop sees EOF at a frame boundary and exits, and this process reaps it rather than killing it —
// a closed port is not a shutdown signal for anything below the Connector (ADR-0005 rule 4).
//
// CloseWrite is the direction the pump uses first: it is what lets the child finish answering the last frame while
// already knowing there will not be another, and Go's `Wait` must not run while a read of the child's stdout is
// still in flight.
//
// shortcut: `Wait` blocks until the child exits, which is what the spawn half of ADR-0022 assumes of a Connector
// that ends its serve loop at EOF. A Connector that outlives its pipe is the naming-and-adopting half of the same
// decision, which `ODEC-01` still holds; when that is answered, this type is where the adoption refusal belongs.
type child struct {
	in   io.WriteCloser
	out  io.ReadCloser
	cmd  *exec.Cmd
	once sync.Once
	err  error
}

func (c *child) Read(p []byte) (int, error)  { return c.out.Read(p) }
func (c *child) Write(p []byte) (int, error) { return c.in.Write(p) }

// CloseWrite ends only the host-to-Connector direction; the read side belongs to the child's own exit.
func (c *child) CloseWrite() error { return c.in.Close() }

func (c *child) Close() error {
	c.once.Do(func() {
		_ = c.in.Close()
		c.err = c.cmd.Wait()
	})
	return c.err
}

// reOrigin is the shape both browser families hand as an argument: `chrome-extension://<id>`, with or without the
// trailing slash, and `moz-extension://<id>` for a Gecko one. It is a shape, not a whitelist — nothing here
// decides who is allowed; it only recognises the argument that is the caller's identity.
var reOrigin = regexp.MustCompile(`^[a-z][a-z0-9+.-]*-extension://[^/]+/?$`)

// firstOrigin is ADR-0004 rule 3's platform fact as a function. What a browser hands this process at startup is
// documented per family: Chrome and the Chromium family pass the caller's origin as the first argument (and, on
// Windows only, a handle to the calling window beside it); Firefox passes the path of the manifest it launched us
// from and then the add-on id from `browser_specific_settings`. Neither is chosen by the extension and neither is
// read from a payload, which is the whole of why the rule exists.
//
// What this does NOT prove is who wrote that argv: the process that spawned us. ADR-0022 rule 3's peer credentials
// are the layer that would, and they are not wired — see this file's record in docs/PROJECT-STATE.md.
func firstOrigin(args []string) string {
	for _, a := range args {
		if reOrigin.MatchString(a) {
			return a
		}
	}
	// Firefox's pair: a manifest path is never an id, so the second argument is the caller. Exactly two
	// arguments, because a third means something other than the documented shape is on this command line.
	if len(args) == 2 && strings.HasSuffix(args[0], ".json") && args[1] != "" {
		return args[1]
	}
	return ""
}

// loadAllowlist reads the host's own startup configuration on disk — a JSON array of sender ids, the one shape
// `framing.json#/senderPolicy/identity/allowlistSource` describes without naming. A file that cannot be read or
// parsed is an empty allowlist, never a permissive default: the unknown sender ADR-0004 rule 6 refuses is the
// same unknown whether the installer never wrote the file or the caller is not in it. The reason goes out on
// notes, because a broken install has to be legible rather than silent.
func loadAllowlist(path string, notes io.Writer) map[string]bool {
	allow := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(notes, "nexora-host: no startup allowlist at %s (%v), so every sender is unknown and every frame is refused\n", path, err)
		return allow
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		fmt.Fprintf(notes, "nexora-host: the allowlist at %s is not a JSON array of sender ids (%v), so every sender is unknown and every frame is refused\n", path, err)
		return allow
	}
	for _, id := range ids {
		allow[strings.TrimRight(id, "/")] = true
	}
	return allow
}

// run is the whole shim: resolve, admit or refuse, and then either answer every frame alone or move bytes.
func run(cfg config, browser io.ReadWriteCloser, open hop2, notes io.Writer) error {
	if cfg.now == nil {
		cfg.now = time.Now
	}
	allow := loadAllowlist(cfg.allowlistPath, notes)
	id := strings.TrimRight(cfg.senderID, "/")
	if id == "" {
		fmt.Fprintf(notes, "nexora-host: no browser handed this process a caller — the argv it documents (an origin, or a manifest path and an id) is absent, so there is no sender to check and every frame is refused\n")
	}
	if !allow[id] {
		fmt.Fprintf(notes, "nexora-host: sender %q is not on the allowlist at %s; one UnauthorizedExtension per frame, no Connector started\n", id, cfg.allowlistPath)
		return refuseOnly(browser, id, cfg.now)
	}
	pipe, err := open(cfg.senderID)
	if err != nil {
		return err
	}
	fmt.Fprintf(notes, "nexora-host: serving sender %q over the pipe opened at %s\n", id, cfg.connectorBin)
	return pump(browser, pipe)
}

// refuseOnly is ADR-0004 rule 6 as a loop: exactly one frame back per request, no pipe to the Connector opened,
// no state touched — this process has none to touch. `inReplyTo` is the only thing read out of the payload, and
// only because a refusal an extension could act on has to name the request it refuses; the method half of
// `#/senderPolicy/refusals/loggedFields` is deliberately absent here, because naming a method means interpreting
// a payload and that is the Connector's half of the split.
//
// Past the guard's window the port closes rather than keeps answering
// (`#/senderPolicy/refusals/amplificationGuard`), which is the response a retry loop cannot cost anything to
// notice and the only one available to a process that holds no keystore.
func refuseOnly(browser io.ReadWriteCloser, senderID string, now func() time.Time) error {
	g := newGuard(now)
	for {
		payload, err := frames.Read(browser, contract.ExtensionToHostBytes)
		if err != nil {
			if errors.Is(err, frames.ErrClosed) {
				return nil
			}
			return err
		}
		if g.spend() {
			return fmt.Errorf("%d refusals in %ds from %q: the port closes instead of answering (framing.json#/senderPolicy/refusals/amplificationGuard)",
				contract.RefusalsPerWindowMax, contract.RefusalWindowSeconds, senderID)
		}
		f, err := envelope.Parse(payload)
		if err != nil {
			// A frame the codec rejects has no id to reply to, and a pipe that is desynchronised is not read on
			// (docs/architecture/25-native-messaging-protocol.md §8 rule 1).
			return fmt.Errorf("refusing sender %q: %w", senderID, err)
		}
		res, err := f.Fail("UnauthorizedExtension",
			// The id is clipped because the envelope caps an error message and a refusal that cannot be
			// marshalled is a closed port instead of an answer — which is the outcome the guard is meant to
			// reserve for a retry loop, not for a long command line.
			fmt.Sprintf("the sender %.64q is not on this host's startup allowlist (ADR-0004 rule 6)", senderID))
		if err != nil {
			return err
		}
		data, err := res.Marshal()
		if err != nil {
			return err
		}
		// The write side refuses before a byte reaches the pipe (doc 22 §2 rule 1), and the answer's own frame is
		// the host-to-extension class.
		if err := frames.Write(browser, data, contract.HostToExtensionBytes); err != nil {
			return err
		}
	}
}

// pump is the byte pump doc 22 §2's Out names: frames on the local pipe verbatim, no translation, no
// re-serialization. The payload read from one end is the payload written to the other, which is the only way this
// process can move a frame it has not understood — and a frame it had understood would be a frame the contract
// forbids it to open.
//
// Two legs, because a Connector emits events nobody requested and a leg that waited for an answer before reading
// the next frame would queue them; queueing is the same document's non-goal.
func pump(browser, connector io.ReadWriteCloser) error {
	legs := make(chan error, 2)
	go func() {
		legs <- forward(connector, browser, contract.ExtensionToHostBytes, contract.ExtensionToHostBytes)
	}()
	go func() {
		legs <- forward(browser, connector, contract.HostToExtensionBytes, contract.HostToExtensionBytes)
	}()
	first := <-legs
	// Whichever hop ended first, the other has nothing left to serve. Ending the write side of hop 2 is the EOF the
	// Connector's read loop treats as the end of a stream and not a shutdown (ADR-0005 rule 4), and the child
	// closing its own stdout is what ends the second leg; the extension owns the reconnect
	// (docs/architecture/25-native-messaging-protocol.md §11), so this process restarts nothing.
	endWriteSide(connector)
	_ = browser.Close()
	second := <-legs
	// Only now that both legs have stopped reading is the child reaped.
	_ = connector.Close()
	for _, err := range []error{first, second} {
		if !quiet(err) {
			return err
		}
	}
	return nil
}

// endWriteSide tells hop 2 that its stream is over. A child process can end one direction and keep its answer
// flowing; a test's net.Pipe cannot, so it is closed outright and its own leg ends on the closed pipe.
func endWriteSide(pipe io.ReadWriteCloser) {
	if wc, ok := pipe.(interface{ CloseWrite() error }); ok {
		_ = wc.CloseWrite()
		return
	}
	_ = pipe.Close()
}

// forward copies frame payloads until one end stops. The read limit is the inbound class of that direction and
// the write limit refuses an outbound frame before a byte is written, so an oversized answer never half-reaches
// the browser.
func forward(dst io.Writer, src io.Reader, readLimit, writeLimit int) error {
	for {
		payload, err := frames.Read(src, readLimit)
		if err != nil {
			return err
		}
		if err := frames.Write(dst, payload, writeLimit); err != nil {
			return err
		}
	}
}

// quiet reports the errors that end a run without a message: a peer that closed at a frame boundary, and a pipe
// this process tore down itself after the other hop had already gone.
func quiet(err error) bool {
	return err == nil || errors.Is(err, frames.ErrClosed) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed)
}

// guard is `#/senderPolicy/refusals/amplificationGuard` counted where the refusals are produced. The Connector
// keeps its own copy of the same window for the refusals it produces; this one bounds the volume of
// UnauthorizedExtension, which is a number only this process can see.
type guard struct {
	mu   sync.Mutex
	at   []time.Time
	now  func() time.Time
	last time.Time
}

func newGuard(now func() time.Time) *guard { return &guard{now: now} }

// spend counts one refusal and reports whether the guard says close the port.
func (g *guard) spend() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	at := g.now()
	edge := at.Add(-time.Duration(contract.RefusalWindowSeconds) * time.Second)
	kept := g.at[:0]
	for _, t := range g.at {
		if t.After(edge) {
			kept = append(kept, t)
		}
	}
	g.at = append(kept, at)
	return len(g.at) > contract.RefusalsPerWindowMax
}

// nexoraRoot is the per-user tree of doc 34 §7, the same root the Connector resolves for its own directories.
func nexoraRoot() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("%LOCALAPPDATA% is empty, and doc 34 §7 names no other per-user root")
	}
	return filepath.Join(base, "NEXORA"), nil
}

// defaultConnectorBin is where doc 33 §7 puts the Connector: beside this binary, in the install tree the installer
// owns. A path the operator names with --connector wins over it.
func defaultConnectorBin() string {
	exe, err := os.Executable()
	if err != nil {
		return "nexora-connector" + exeSuffix()
	}
	return filepath.Join(filepath.Dir(exe), "nexora-connector"+exeSuffix())
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// defaultAllowlistPath is the host's own startup configuration under the per-user root. No document names the file
// — `framing.json#/senderPolicy/identity/allowlistSource` names only who writes it, the installer — so the path
// here is this process's choice and doc 33's installer has to agree with it, which is the kind of agreement the
// unwritten installer is recorded as owing.
func defaultAllowlistPath() string {
	root, err := nexoraRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "host", "allowlist.json")
}

func main() {
	fs := flag.NewFlagSet("nexora-host", flag.ExitOnError)
	connectorBin := fs.String("connector", defaultConnectorBin(), "the nexora-connector binary this host opens a pipe for")
	allowlistPath := fs.String("allowlist", defaultAllowlistPath(), "the host's startup allowlist: a JSON array of sender ids the installer wrote (framing.json#/senderPolicy/identity/allowlistSource)")
	senderOrigin := fs.String("sender-origin", "", "developer affordance: the sender to serve when no browser started this process. Production never reads it — ADR-0004 rule 3 wants the browser's argv, and a hand-typed id is the caller choosing its own authorization")
	_ = fs.Parse(os.Args[1:])

	// The browser's answer outranks the flag: whatever a human typed, the argv the port opened with is the
	// platform's fact and the only one the contract accepts as identity.
	origin := firstOrigin(fs.Args())
	if origin == "" {
		origin = *senderOrigin
	}

	cfg := config{senderID: origin, connectorBin: *connectorBin, allowlistPath: *allowlistPath}
	open := spawn(cfg.connectorBin, connectorArgs, nil)
	if err := run(cfg, duplex{os.Stdin, os.Stdout}, open, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "nexora-host: %v\n", err)
		os.Exit(1)
	}
}
