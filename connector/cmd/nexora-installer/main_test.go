package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contract"
)

// The installer's claims are the ones a browser would otherwise discover at the worst moment: that the manifest
// is one object both families can read, that there is exactly one of it, that the allowlist is the file the host
// opens, and that a registration which cannot match is refused before it is written. The registry itself is
// faked for those, and the last test drives the real one on a Windows host.

const (
	pinnedOrigin = "chrome-extension://abcdefghijklmnopabcdefghijklmnop"
	geckoID      = "nexora@example.org"
)

// fakeReg is the registry as the three verbs this command uses see it. It answers query with an error for a key
// that is not there, which is what reg.exe does and what makes uninstall's idempotency a property of the code
// rather than of the exit status it happens to ignore.
type fakeReg struct {
	keys    map[string]string
	calls   [][]string
	failAdd string
}

func newFakeReg() *fakeReg { return &fakeReg{keys: map[string]string{}} }

func (f *fakeReg) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if name != "reg.exe" {
		return nil, errors.New("the installer must reach the registry through reg.exe and nothing else")
	}
	key := args[1]
	switch args[0] {
	case "add":
		if key == f.failAdd {
			return []byte("ERROR: access is denied."), errors.New("exit status 1")
		}
		f.keys[key] = args[6]
	case "delete":
		delete(f.keys, key)
	case "query":
		v, ok := f.keys[key]
		if !ok {
			return nil, errors.New("exit status 1")
		}
		return []byte(key + "\n    (Default)    REG_SZ    " + v + "\n"), nil
	}
	return nil, nil
}

func (f *fakeReg) verbs(verb string) [][]string {
	var out [][]string
	for _, c := range f.calls {
		if c[1] == verb {
			out = append(out, c)
		}
	}
	return out
}

// installFixture is one install into a root the test owns, with a host binary that exists — the manifest names an
// executable, and a path that is not there is a refusal this command is supposed to make.
func installFixture(t *testing.T, origins ...string) (config, *fakeReg) {
	t.Helper()
	root := t.TempDir()
	host := filepath.Join(root, "nexora-host"+exeSuffix())
	if err := os.WriteFile(host, []byte("not really a binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	reg := newFakeReg()
	return config{root: root, hostBin: host, origins: origins, run: reg.run}, reg
}

func readManifest(t *testing.T, root string) manifest {
	t.Helper()
	data, err := os.ReadFile(manifestPath(root))
	if err != nil {
		t.Fatalf("the manifest is not where doc 25 §2.1 says the installer writes it: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("the manifest is not one JSON object: %v", err)
	}
	return m
}

func TestTheManifestIsOneObjectBothFamiliesRead(t *testing.T) {
	cfg, reg := installFixture(t, pinnedOrigin, geckoID)
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, cfg.root)
	if m.Name != contract.HostName {
		t.Errorf("name = %q, want the contract's %q — the extension types this string into connectNative", m.Name, contract.HostName)
	}
	if m.Type != "stdio" {
		t.Errorf("type = %q, want stdio", m.Type)
	}
	if m.Path != cfg.hostBin {
		t.Errorf("path = %q, want the host binary %q", m.Path, cfg.hostBin)
	}
	if !slices.Equal(m.AllowedOrigins, []string{pinnedOrigin + "/"}) {
		t.Errorf("allowed_origins = %v, want the Chromium origin with the slash Chrome documents", m.AllowedOrigins)
	}
	if !slices.Equal(m.AllowedExtensions, []string{geckoID}) {
		t.Errorf("allowed_extensions = %v, want the Gecko add-on id", m.AllowedExtensions)
	}
	if len(reg.verbs("add")) != 3 {
		t.Errorf("registrations = %d, want one per family of doc 25 §2.1's matrix", len(reg.verbs("add")))
	}
}

func TestBothAllowListsArePresentEvenWhenOneIsEmpty(t *testing.T) {
	cfg, _ := installFixture(t, pinnedOrigin)
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifestPath(cfg.root))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"allowed_origins", "allowed_extensions"} {
		v, ok := raw[field]
		if !ok {
			t.Errorf("%s is absent; the family that reads it would reject the manifest rather than refuse the sender", field)
			continue
		}
		// null is not an empty allow list: it is a field of the wrong type, and the family rejects the manifest.
		var list []string
		if err := json.Unmarshal(v, &list); err != nil {
			t.Errorf("%s is %s, which is not a JSON array: %v", field, v, err)
		}
	}
	m := readManifest(t, cfg.root)
	if len(m.AllowedExtensions) != 0 {
		t.Errorf("allowed_extensions = %v, want empty: no Gecko id was named, so Gecko must refuse every sender", m.AllowedExtensions)
	}
}

func TestExactlyOneEntryPointPerInstallation(t *testing.T) {
	cfg, reg := installFixture(t, pinnedOrigin, geckoID)
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(manifestPath(cfg.root))
	if err != nil {
		t.Fatal(err)
	}
	// A second install with the ids in the other order is the same installation, not a second one.
	cfg.origins = []string{geckoID, pinnedOrigin, pinnedOrigin}
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(manifestPath(cfg.root))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("installing twice changed the manifest's bytes; the allow lists are sorted and deduplicated so a re-install is a rename over identical content")
	}
	entries, err := os.ReadDir(filepath.Join(cfg.root, "host"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	want := []string{"allowlist.json", contract.HostName + ".json"}
	if !slices.Equal(names, want) {
		t.Errorf("the host directory holds %v, want exactly %v — two manifests for one host name are two paths into one Connector", names, want)
	}
	for _, c := range reg.verbs("add") {
		if c[7] != manifestPath(cfg.root) {
			t.Errorf("%s names %q, not the one manifest at %q", c[2], c[7], manifestPath(cfg.root))
		}
	}
}

func TestTheRegistrationMatrixIsDoc25sWindowsColumn(t *testing.T) {
	cfg, reg := installFixture(t, pinnedOrigin)
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`HKCU\SOFTWARE\Google\Chrome\NativeMessagingHosts\` + contract.HostName,
		`HKCU\SOFTWARE\Microsoft\Edge\NativeMessagingHosts\` + contract.HostName,
		`HKCU\SOFTWARE\Mozilla\NativeMessagingHosts\` + contract.HostName,
	}
	adds := reg.verbs("add")
	if len(adds) != len(want) {
		t.Fatalf("registrations = %d, want %d", len(adds), len(want))
	}
	for i, c := range adds {
		if c[0] != "reg.exe" || c[1] != "add" || c[2] != want[i] {
			t.Errorf("argv = %v, want reg.exe add %s", c, want[i])
		}
		if !slices.Equal(c[3:], []string{"/ve", "/t", "REG_SZ", "/d", manifestPath(cfg.root), "/f"}) {
			t.Errorf("argv tail = %v, want the default value set to the manifest path as REG_SZ", c[3:])
		}
	}
	// doc 33 §14: the registration is the only thing written. No service, no run key, no startup item.
	for _, c := range reg.calls {
		if strings.Contains(strings.ToLower(strings.Join(c, " ")), "currentversion\\run") || strings.Contains(c[2], "Services") {
			t.Errorf("argv %v registers something other than a Native Messaging host", c)
		}
	}
}

func TestTheAllowlistIsTheFileTheHostOpens(t *testing.T) {
	cfg, _ := installFixture(t, pinnedOrigin, geckoID)
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.root, filepath.FromSlash(contract.HostAllowlistFile)))
	if err != nil {
		t.Fatalf("the allowlist is not at the contract's address: %v", err)
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		t.Fatalf("the host reads a JSON array of sender ids and nothing else: %v", err)
	}
	if !slices.Equal(ids, []string{pinnedOrigin + "/", geckoID}) {
		t.Errorf("allowlist = %v, want both senders", ids)
	}
	m := readManifest(t, cfg.root)
	if !slices.Equal(ids, append(append([]string{}, m.AllowedOrigins...), m.AllowedExtensions...)) {
		t.Error("the allowlist and the manifest disagree; the browser would admit a sender the host then refuses")
	}
	// The host trims the trailing slash on both sides of its comparison, so the argv Chrome hands it matches.
	if strings.TrimRight(ids[0], "/") != pinnedOrigin {
		t.Errorf("the host would compare %q against the argv origin %q and refuse", strings.TrimRight(ids[0], "/"), pinnedOrigin)
	}
}

func TestAnOriginThatCannotMatchIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	for _, bad := range []string{
		"",
		"chrome-extension://ABCDEFGHIJKLMNOPABCDEFGHIJKLMNOP",
		"chrome-extension://abcdefghijklmnopabcdefghijklmnopabc",
		"chrome-extension://abcdefghijklmnopqrstuvwxy/abcdefghijklmnop",
		"moz-extension://12345678-1234-1234-1234-123456789abc",
		"chrome-extension://abcdefghijklmnopabcdefghijklmnop/extra",
		"nexora",
		"nexora@example.org@elsewhere",
	} {
		cfg, reg := installFixture(t, bad)
		err := install(cfg)
		if err == nil {
			t.Errorf("%q was accepted; no browser would ever hand the host that id", bad)
			continue
		}
		if len(reg.calls) != 0 {
			t.Errorf("%q: the registry was touched before the origin was validated: %v", bad, reg.calls)
		}
		if _, err := os.Stat(filepath.Join(cfg.root, "host")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%q: a file was written for a refused install (stat err = %v)", bad, err)
		}
	}
}

func TestAGeckoGuidIsAnAddOnId(t *testing.T) {
	id := "{12345678-1234-1234-1234-123456789abc}"
	cfg, _ := installFixture(t, id)
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, cfg.root)
	if !slices.Equal(m.AllowedExtensions, []string{id}) {
		t.Errorf("allowed_extensions = %v, want the GUID form MDN documents", m.AllowedExtensions)
	}
	if len(m.AllowedOrigins) != 0 {
		t.Errorf("allowed_origins = %v, want empty", m.AllowedOrigins)
	}
}

func TestInstallRefusesAnEmptyOrMissingHost(t *testing.T) {
	cfg, _ := installFixture(t)
	if err := install(cfg); err == nil {
		t.Error("an install with no --origin was accepted; both allow lists would be empty and every frame refused")
	}
	cfg, reg := installFixture(t, pinnedOrigin)
	cfg.hostBin = filepath.Join(cfg.root, "absent"+exeSuffix())
	if err := install(cfg); err == nil {
		t.Error("a manifest naming a binary that is not there was accepted")
	}
	if len(reg.calls) != 0 {
		t.Errorf("the registry was touched for a host that cannot be started: %v", reg.calls)
	}
}

func TestARegistrationFailureNamesTheFamilyAndTheFilesItLeft(t *testing.T) {
	cfg, reg := installFixture(t, pinnedOrigin)
	reg.failAdd = registrations()[1].key
	err := install(cfg)
	if err == nil {
		t.Fatal("a failed registration was reported as success")
	}
	if !strings.Contains(err.Error(), "Edge") {
		t.Errorf("error %q does not name the family that failed", err)
	}
	for _, r := range registrations() {
		if r.family == "Edge" {
			continue
		}
		if _, ok := reg.keys[r.key]; !ok {
			t.Errorf("%s was not attempted after Edge failed; the operator sees a third of the answer", r.family)
		}
	}
	if _, err := os.Stat(manifestPath(cfg.root)); err != nil {
		t.Errorf("the manifest was removed after a registration failure: %v", err)
	}
}

func TestUninstallRemovesTheRegistrationsAndTheTwoFilesOnly(t *testing.T) {
	cfg, reg := installFixture(t, pinnedOrigin, geckoID)
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	// The user's data shares this root and is not the installer's to delete.
	keep := []string{filepath.Join(cfg.root, "keystore", "blob"), filepath.Join(cfg.root, "state", "state.json")}
	for _, p := range keep {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := uninstall(cfg); err != nil {
		t.Fatal(err)
	}
	if len(reg.keys) != 0 {
		t.Errorf("keys left behind: %v", reg.keys)
	}
	for _, p := range []string{manifestPath(cfg.root), allowlistPath(cfg.root)} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived uninstall (err = %v)", p, err)
		}
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("uninstall deleted %s, which is the user's data: %v", p, err)
		}
	}
	for _, c := range reg.verbs("delete") {
		if !slices.Equal(c, []string{"reg.exe", "delete", c[2], "/f"}) {
			t.Errorf("delete argv = %v, want the key and /f only", c)
		}
	}
}

func TestUninstallIsIdempotent(t *testing.T) {
	cfg, _ := installFixture(t, pinnedOrigin)
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := uninstall(cfg); err != nil {
			t.Fatalf("uninstall %d: %v", i+1, err)
		}
	}
}

func TestAnUnsupportedPlatformIsARefusal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this host is the supported platform; the refusal is the other platforms' test")
	}
	cfg, reg := installFixture(t, pinnedOrigin)
	for _, op := range []func(config) error{install, uninstall} {
		err := op(cfg)
		if err == nil {
			t.Fatal("a platform with no named per-user root was accepted")
		}
		if !strings.Contains(err.Error(), "ODEC-27") {
			t.Errorf("refusal %q does not name the decision that would answer it", err)
		}
	}
	if len(reg.calls) != 0 {
		t.Errorf("the registry was touched on an unsupported platform: %v", reg.calls)
	}
}

// TestTheVerbIsParsedBeforeTheFlags is the defect a live run found: flag.Parse stops at the first non-flag
// argument, so `install --origin <id>` read as flags-after-verb parses no flags at all and the install refuses
// itself for an empty allow list the operator did type.
func TestTheVerbIsParsedBeforeTheFlags(t *testing.T) {
	verb, cfg, err := parse([]string{"install", "--origin", pinnedOrigin, "--origin", geckoID, "--host", "C:\\bin\\nexora-host.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if verb != "install" {
		t.Errorf("verb = %q, want install", verb)
	}
	if !slices.Equal(cfg.origins, []string{pinnedOrigin, geckoID}) {
		t.Errorf("origins = %v, want both ids the command line named", cfg.origins)
	}
	if cfg.hostBin != "C:\\bin\\nexora-host.exe" {
		t.Errorf("host = %q, want the path the command line named", cfg.hostBin)
	}
	if cfg.run == nil {
		t.Error("parse left the registry runner nil; production would panic on the first registration")
	}
	if verb, _, err := parse([]string{"uninstall"}); err != nil || verb != "uninstall" {
		t.Errorf("uninstall parse = %q, %v", verb, err)
	}
	if _, _, err := parse(nil); err == nil {
		t.Error("no verb at all was accepted")
	}
	if _, _, err := parse([]string{"install", "--origins", pinnedOrigin}); err == nil {
		t.Error("an unknown flag was accepted")
	}
}

// TestTheRegistrationIsRealOnThisHost drives reg.exe itself: the fake above proves the argv, and only the real
// registry proves Windows accepts it and hands the value back. It writes the contract's own host name under HKCU
// and removes it again, so a run leaves the hive as it found it.
func TestTheRegistrationIsRealOnThisHost(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("reg.exe and HKCU are Windows facts")
	}
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{root: root, hostBin: exe, origins: []string{pinnedOrigin}, run: runReg}
	t.Cleanup(func() {
		if err := uninstall(cfg); err != nil {
			t.Errorf("cleanup left a registration behind: %v", err)
		}
	})
	if err := install(cfg); err != nil {
		t.Fatal(err)
	}
	for _, r := range registrations() {
		out, err := runReg("reg.exe", "query", r.key)
		if err != nil {
			t.Fatalf("%s: the key the installer wrote cannot be read back: %v (%s)", r.family, err, out)
		}
		if !strings.Contains(string(out), manifestPath(root)) {
			t.Errorf("%s: query answers %s, which does not name the manifest at %s", r.family, out, manifestPath(root))
		}
	}
	if err := uninstall(cfg); err != nil {
		t.Fatal(err)
	}
	for _, r := range registrations() {
		if out, err := runReg("reg.exe", "query", r.key); err == nil {
			t.Errorf("%s: the key survived uninstall: %s", r.family, out)
		}
	}
}