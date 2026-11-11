// Package main is nexora-installer — the process that makes the browser able to start nexora-host at all. Its
// whole job is the registration matrix of `docs/architecture/25-native-messaging-protocol.md` §2.1: write the one
// manifest named after `framing.json#/senderPolicy/identity/hostName`, point every browser family's per-user
// registration at it, and write the startup allowlist the host reads at
// `framing.json#/senderPolicy/identity/allowlistFile`. A shim no browser can start is a tested binary and nothing
// more, which is the gap this command closes.
//
// What it deliberately is not: doc 33 §5's twelve first-run steps. Those download, verify and install Core, and
// they need `connector/testdata/pins.json`, which does not exist (`VER-41`). Nothing here fetches a byte, starts a
// process or registers a service — doc 33 §14 and doc 21 §12 forbid the service, and doc 25 §2.1 says the
// registration is the only thing written.
//
// The sender ids are an input, not a discovery. Chromium derives an extension id from its signing key, and no
// manifest in this repository declares a `key` field, so no process here can compute the origin the browser will
// hand the host; Firefox's id is an author's address (`ODEC-13` owns its custody). The operator names them with
// --origin and this command validates their shape, because a registration whose allow list can never match is a
// silent refusal rather than an install.
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
	"sort"
	"strings"

	"github.com/k2dms/nexora/connector/internal/contract"
)

// runner is the one effect this command has on the machine outside its own tree: a registry edit. Tests pass a
// recorder and read the argv; production passes reg.exe. Nothing else here needs an injection — the file writes
// are ordinary ones into a root the caller names.
type runner func(name string, args ...string) ([]byte, error)

func runReg(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

type config struct {
	root    string
	hostBin string
	origins []string
	run     runner
}

// manifest is the one JSON object every registration points at (doc 25 §2.1). Both allow-list fields are always
// present: the Chromium family reads allowed_origins and Gecko reads allowed_extensions, each ignores the other's,
// and an empty array is that family refusing every sender — the fail-closed direction, never a permissive default.
type manifest struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Path              string   `json:"path"`
	Type              string   `json:"type"`
	AllowedOrigins    []string `json:"allowed_origins"`
	AllowedExtensions []string `json:"allowed_extensions"`
}

// registration is one family's per-user entry point. On Windows the manifest is a file the key names; the key
// itself is the registration, and its default value is the manifest's absolute path.
type registration struct {
	family string
	key    string
}

// registrations is doc 25 §2.1's Windows column, in that table's order. Edge reads Chromium's key and then
// Chrome's as well, so the Chrome row already covers it; its own key is written because the table names it and a
// browser that changes its fallback order must not lose the registration.
func registrations() []registration {
	return []registration{
		{"Chrome", `HKCU\SOFTWARE\Google\Chrome\NativeMessagingHosts\` + contract.HostName},
		{"Edge", `HKCU\SOFTWARE\Microsoft\Edge\NativeMessagingHosts\` + contract.HostName},
		{"Firefox", `HKCU\SOFTWARE\Mozilla\NativeMessagingHosts\` + contract.HostName},
	}
}

// The two shapes are the platforms' own rules, not NEXORA's: a Chromium origin is chrome-extension:// followed by
// the 32 characters a key hash produces (a-p, the hex alphabet shifted), and a Gecko add-on id is a GUID or an
// email-like string. Validating here is what keeps a typo from becoming an install that refuses every frame.
var (
	reChromiumOrigin = regexp.MustCompile(`^chrome-extension://[a-p]{32}/?$`)
	reGeckoID        = regexp.MustCompile(`^(?:\{[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\}|[^@\s{}]+@[^@\s{}]+)$`)
)

// splitOrigins sorts the operator's ids into the two allow lists and normalises the Chromium ones to the trailing
// slash Chrome documents. The host trims that slash on both sides of its comparison
// (`connector/cmd/nexora-host` loadAllowlist and run), so one spelling serves the manifest and the allowlist file.
// Sorted and deduplicated, so installing twice writes identical bytes.
func splitOrigins(origins []string) (chromium, gecko []string, err error) {
	// Both are arrays in the file even when empty: a nil slice marshals to null, and a family that reads its own
	// allow list as a list rejects the manifest outright instead of refusing the sender.
	chromium, gecko = []string{}, []string{}
	seen := map[string]bool{}
	for _, o := range origins {
		var chromiumOrigin bool
		switch {
		case reChromiumOrigin.MatchString(o):
			o = strings.TrimRight(o, "/") + "/"
			chromiumOrigin = true
		case reGeckoID.MatchString(o):
		default:
			return nil, nil, fmt.Errorf("%q is neither a chrome-extension:// origin of 32 a-p characters nor a Gecko add-on id (a GUID or an email-like string); the browser would never match it, so it is refused rather than written", o)
		}
		if seen[o] {
			continue
		}
		seen[o] = true
		if chromiumOrigin {
			chromium = append(chromium, o)
		} else {
			gecko = append(gecko, o)
		}
	}
	sort.Strings(chromium)
	sort.Strings(gecko)
	return chromium, gecko, nil
}

// nexoraRoot is the per-user tree of doc 34 §7, the same root nexora-host and nexora-connector resolve for their
// own directories. The installer writes into it and invents no root of its own.
func nexoraRoot() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("%LOCALAPPDATA% is empty, and doc 34 §7 names no other per-user root")
	}
	return filepath.Join(base, "NEXORA"), nil
}

// defaultHostBin is doc 33 §7's install tree read the way nexora-host reads its own Connector: beside this binary.
func defaultHostBin() string {
	exe, err := os.Executable()
	if err != nil {
		return "nexora-host" + exeSuffix()
	}
	return filepath.Join(filepath.Dir(exe), "nexora-host"+exeSuffix())
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func manifestPath(root string) string {
	return filepath.Join(root, "host", contract.HostName+".json")
}

func allowlistPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(contract.HostAllowlistFile))
}

// supported is doc 33 §10's refusal rather than a fallback. The matrix in doc 25 §2.1 names macOS and Linux
// directories, but no document names the per-user root there — doc 34 §7's tree is a Windows path — and ODEC-27
// is the ADR that would answer it. Writing a guessed root is the thing that rule exists to prevent, and
// internal/keystore's platform_other.go already refuses for the same reason.
func supported() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("the installer registers Native Messaging on Windows only: doc 34 §7 names %%LOCALAPPDATA%%\\NEXORA and no per-user root for %s, and ODEC-27 is the decision that would name one (doc 33 §10 makes an unsupported platform a refusal, not a fallback)", runtime.GOOS)
	}
	return nil
}

// writeAtomic is the temp-and-rename discipline internal/apply and internal/keystore already use, so a failed
// install leaves the previous manifest byte-identical rather than half-written. 0600 is the portable half of
// doc 33 §7's owner-only rule; on Windows the inherited user-profile descriptor governs.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("installer: %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("installer: %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("installer: %s: %w", path, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("installer: %s could not be flushed: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("installer: %s could not be closed: %w", path, err)
	}
	if err = os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("installer: %s could not be made owner-only: %w", path, err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("installer: %s could not be installed: %w", path, err)
	}
	return nil
}

// install writes the two files first and registers second. A key naming a manifest that is not there is a
// registration the browser cannot use; a manifest no key names is inert. Every family is attempted even when one
// fails, so the operator sees the whole answer instead of the first third of it.
func install(cfg config) error {
	if err := supported(); err != nil {
		return err
	}
	if len(cfg.origins) == 0 {
		return errors.New("installer: no --origin was given; a manifest whose allow lists are both empty is a host no extension may call, which is not an install")
	}
	host, err := filepath.Abs(cfg.hostBin)
	if err != nil {
		return fmt.Errorf("installer: %s: %w", cfg.hostBin, err)
	}
	if fi, err := os.Stat(host); err != nil || fi.IsDir() {
		return fmt.Errorf("installer: the host binary %s is not a file (%v); doc 25 §2.1's manifest names the executable the browser starts, and a registration pointing at nothing is a refusal the user cannot diagnose", host, err)
	}
	chromium, gecko, err := splitOrigins(cfg.origins)
	if err != nil {
		return err
	}

	m := manifest{
		Name:              contract.HostName,
		Description:       "NEXORA Local Connector — the browser's pipe to the per-user proxy",
		Path:              host,
		Type:              "stdio",
		AllowedOrigins:    chromium,
		AllowedExtensions: gecko,
	}
	mBytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("installer: the manifest could not be encoded: %w", err)
	}
	mPath := manifestPath(cfg.root)
	if err := writeAtomic(mPath, append(mBytes, '\n')); err != nil {
		return err
	}

	ids := append(append([]string{}, chromium...), gecko...)
	aBytes, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return fmt.Errorf("installer: the allowlist could not be encoded: %w", err)
	}
	aPath := allowlistPath(cfg.root)
	if err := writeAtomic(aPath, append(aBytes, '\n')); err != nil {
		return err
	}

	var failed []string
	for _, r := range registrations() {
		if out, err := cfg.run("reg.exe", "add", r.key, "/ve", "/t", "REG_SZ", "/d", mPath, "/f"); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v (%s)", r.family, err, out))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("installer: the manifest is at %s and the allowlist at %s, but these registrations failed: %v", mPath, aPath, failed)
	}
	fmt.Printf("nexora-installer: %s registered for Chrome, Edge and Firefox; allowlist %s carries %d sender(s)\n", mPath, aPath, len(ids))
	return nil
}

// uninstall removes the registrations and the two files it wrote, and nothing else: the keystore, the compiled
// config, the applied state and Core are the user's data and doc 33 §7's cleanup owns them, not this command.
// Removing the allowlist is what makes the host fail closed afterwards rather than keep trusting a sender whose
// registration is gone.
func uninstall(cfg config) error {
	if err := supported(); err != nil {
		return err
	}
	var failed []string
	for _, r := range registrations() {
		// reg.exe delete answers non-zero for a key that is already absent, so the proof of removal is the query
		// afterwards, not the delete's exit code. That is what makes uninstall idempotent.
		_, _ = cfg.run("reg.exe", "delete", r.key, "/f")
		if _, err := cfg.run("reg.exe", "query", r.key); err == nil {
			failed = append(failed, r.family+": the key is still present after delete")
		}
	}
	for _, p := range []string{manifestPath(cfg.root), allowlistPath(cfg.root)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			failed = append(failed, fmt.Sprintf("%s: %v", p, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("installer: uninstall left these behind: %v", failed)
	}
	fmt.Printf("nexora-installer: removed the Chrome, Edge and Firefox registrations and the files under %s\n", filepath.Join(cfg.root, "host"))
	return nil
}

const usage = "usage: nexora-installer install --origin <id> [--origin <id>...] [--host <path>] | nexora-installer uninstall"

// parse reads the verb and then the flags. The verb comes first because flag.Parse stops at the first argument
// that is not a flag: `install --origin <id>` parsed as a verb alone would leave the allow lists empty, and the
// install would refuse itself for a reason the operator did not cause.
func parse(args []string) (string, config, error) {
	if len(args) == 0 {
		return "", config{}, errors.New(usage)
	}
	verb := args[0]
	fs := flag.NewFlagSet("nexora-installer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	hostBin := fs.String("host", defaultHostBin(), "the nexora-host binary the manifest names; default is beside this executable (doc 33 §7's install tree)")
	var origins []string
	fs.Func("origin", "a sender the browser will hand the host: chrome-extension://<32 a-p characters> or a Gecko add-on id. Repeatable; at least one is required to install", func(s string) error {
		origins = append(origins, s)
		return nil
	})
	if err := fs.Parse(args[1:]); err != nil {
		return verb, config{}, err
	}
	return verb, config{hostBin: *hostBin, origins: origins, run: runReg}, nil
}

func main() {
	verb, cfg, err := parse(os.Args[1:])
	if err == nil {
		cfg.root, err = nexoraRoot()
	}
	if err == nil {
		switch verb {
		case "install":
			err = install(cfg)
		case "uninstall":
			err = uninstall(cfg)
		default:
			err = fmt.Errorf("%s (got %q)", usage, verb)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexora-installer: %v\n", err)
		os.Exit(1)
	}
}