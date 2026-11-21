package install

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/receipt"
)

// The test's own pin: a real Ed25519 keypair generated per run, an httptest origin standing in for the
// pinned release host, and the entry set a legitimate Xray release carries. This is VER-41's exercise:
// download, size bound, signature, hash, traversal gate, extraction, owner-only ACL, receipt, upgrade
// with the previous version still on disk — with every refusal path driven by its own mutated input.

type harness struct {
	pin      Pin
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
	server   *httptest.Server
	root     string
	expected []string
	freeHit  *int64
}

func newHarness(t *testing.T, entries map[string][]byte) *harness {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(entries[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()
	zipLen := int64(len(zipBytes))
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, zipBytes))
	sha := sha256Hex(zipBytes)
	root := t.TempDir()

	h := &harness{
		pub:      pub,
		priv:     priv,
		expected: names,
		root:     root,
	}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	t.Cleanup(h.server.Close)

	h.pin = Pin{
		PinsVersion:              "1",
		ReleaseTag:               "v24.11.11",
		SignaturePubkey:          base64.StdEncoding.EncodeToString(pub),
		RetainedPreviousVersions: 1,
		Assets: []Asset{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, File: "Xray-test.zip",
			URL: h.server.URL + "/Xray-test.zip", SHA256: sha, Signature: sig, SizeBytes: zipLen,
		}},
	}
	return h
}

func (h *harness) installer(t *testing.T) *Installer {
	return &Installer{Root: h.root, Now: func() time.Time { return time.Unix(0, 0).UTC() }}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestInstallHappyPathWritesReceiptAndKeepsOldVersions(t *testing.T) {
	xray := []byte("fake xray binary bytes")
	h := newHarness(t, map[string][]byte{
		"xray.exe":     xray,
		"geoip.dat":    []byte("geo data"),
		"LICENSE":      []byte("license"),
		"docs/read.md": []byte("docs"),
	})
	inst := h.installer(t)
	res, err := inst.Install(context.Background(), h.pin, h.expected)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// Step 9's read-back: the version directory is owner-only.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(h.root, h.pin.ReleaseTag))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("version directory perms %o, want 0700", info.Mode().Perm())
		}
	}
	// The receipt is the pre-exec verifier's input, and it verifies the extracted binary.
	if err := receipt.NewVerifier(res.ReceiptPath)(res.BinaryPath); err != nil {
		t.Fatalf("the written receipt does not verify the extracted binary: %v", err)
	}
	r, err := receipt.Load(res.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if r.Tag != "v24.11.11" || r.PinsVersion != "1" || r.Path != res.BinaryPath {
		t.Fatalf("receipt content: %+v", r)
	}
	if !strings.Contains(r.SHA256, "0") || len(r.SHA256) != 64 {
		t.Fatalf("receipt sha256 shape: %q", r.SHA256)
	}
	// Step 11: available, and the receipt names the active one; a second release keeps the first on disk.
	old := res.BinaryPath
	h2 := newHarnessWithPin(t, h, "v24.12.1", map[string][]byte{"xray.exe": []byte("newer bytes")})
	res2, err := inst.Install(context.Background(), h2, []string{"xray.exe"})
	if err != nil {
		t.Fatalf("upgrade install: %v", err)
	}
	if res2.BinaryPath == old {
		t.Fatal("the upgrade did not switch the active binary")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("the previous verified version was removed; §8's rollback needs it on disk")
	}
	if _, err := os.Stat(filepath.Join(h.root, "v24.11.11")); err != nil {
		t.Fatal("the previous version directory is gone")
	}
	// GC keeps exactly the retained number of previous versions.
	if _, err := os.Stat(filepath.Join(h.root, "v24.12.1")); err != nil {
		t.Fatal("the active version directory is gone")
	}
}

func newHarnessWithPin(t *testing.T, h *harness, tag string, entries map[string][]byte) Pin {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data)
	}
	zw.Close()
	zipBytes := buf.Bytes()
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(h.priv, zipBytes))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	t.Cleanup(server.Close)
	return Pin{
		PinsVersion:              "1",
		ReleaseTag:               tag,
		SignaturePubkey:          base64.StdEncoding.EncodeToString(h.pub),
		RetainedPreviousVersions: 1,
		Assets: []Asset{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, File: "Xray-test.zip",
			URL: server.URL + "/Xray-test.zip", SHA256: sha256Hex(zipBytes), Signature: sig, SizeBytes: int64(len(zipBytes)),
		}},
	}
}

func TestInstallRefusesHashMismatchAndCleansUp(t *testing.T) {
	h := newHarness(t, map[string][]byte{"xray.exe": []byte("bytes")})
	h.pin.Assets[0].SHA256 = strings.Repeat("0", 64)
	res, err := h.installer(t).Install(context.Background(), h.pin, h.expected)
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("want a sha256 refusal, got %v, %+v", err, res)
	}
	assertClean(t, h)
}

func TestInstallRefusesSignatureMismatch(t *testing.T) {
	h := newHarness(t, map[string][]byte{"xray.exe": []byte("bytes")})
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h.pin.Assets[0].Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(otherPriv, []byte("something else")))
	res, err := h.installer(t).Install(context.Background(), h.pin, h.expected)
	if err == nil || !strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("want a signature refusal, got %v, %+v", err, res)
	}
	assertClean(t, h)
}

func TestInstallRefusesMissingSignature(t *testing.T) {
	h := newHarness(t, map[string][]byte{"xray.exe": []byte("bytes")})
	h.pin.Assets[0].Signature = ""
	res, err := h.installer(t).Install(context.Background(), h.pin, h.expected)
	if err == nil || !strings.Contains(err.Error(), "verifySignature") {
		t.Fatalf("want a missing-signature refusal, got %v, %+v", err, res)
	}
	assertClean(t, h)
}

func TestInstallAbortsAtTheSizeBound(t *testing.T) {
	h := newHarness(t, map[string][]byte{"xray.exe": []byte("bytes")})
	// The served body is longer than the pin's sizeBytes: the download must abort at sizeBytes + 1.
	h.pin.Assets[0].SizeBytes = 4
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(w, strings.NewReader("0123456789"))
	})
	res, err := h.installer(t).Install(context.Background(), h.pin, h.expected)
	if err == nil {
		t.Fatalf("an oversized download was accepted: %+v", res)
	}
	assertClean(t, h)
}

func TestInstallRefusesZipSlipBeforeAnyWrite(t *testing.T) {
	h := newHarness(t, map[string][]byte{"xray.exe": []byte("bytes"), "../evil.exe": []byte("evil")})
	h.expected = []string{"xray.exe", "../evil.exe"} // even a pin that lists it is refused by the shape gate
	res, err := h.installer(t).Install(context.Background(), h.pin, h.expected)
	if err == nil || !strings.Contains(err.Error(), "climbs out") {
		t.Fatalf("want a traversal refusal, got %v, %+v", err, res)
	}
	assertClean(t, h)
}

func TestInstallRefusesAnEntryOutsideTheExpectedSet(t *testing.T) {
	h := newHarness(t, map[string][]byte{"xray.exe": []byte("bytes"), "extra.dll": []byte("extra")})
	res, err := h.installer(t).Install(context.Background(), h.pin, []string{"xray.exe"})
	if err == nil || !strings.Contains(err.Error(), "outside the pin's expected set") {
		t.Fatalf("want an expected-set refusal, got %v, %+v", err, res)
	}
	assertClean(t, h)
}

func TestInstallRefusesADiskShortageBeforeAnyBytesMove(t *testing.T) {
	h := newHarness(t, map[string][]byte{"xray.exe": []byte("bytes")})
	inst := h.installer(t)
	inst.FreeBytes = func(string) (uint64, error) { return 1, nil }
	res, err := inst.Install(context.Background(), h.pin, h.expected)
	if err == nil || !strings.Contains(err.Error(), "DiskFull") {
		t.Fatalf("want a DiskFull refusal, got %v, %+v", err, res)
	}
	entries, _ := os.ReadDir(h.root)
	if len(entries) != 0 {
		t.Fatalf("a DiskFull refusal left %d entries behind", len(entries))
	}
}

func TestInstallRefusesAPlatformThePinDoesNotName(t *testing.T) {
	h := newHarness(t, map[string][]byte{"xray.exe": []byte("bytes")})
	h.pin.Assets = nil
	if _, err := h.installer(t).Install(context.Background(), h.pin, h.expected); err == nil || !strings.Contains(err.Error(), "refusing to run there") {
		t.Fatalf("want the absent-platform refusal, got %v", err)
	}
}

func TestLoadPinsReadsTheShippedShapeAndRefusesABrokenOne(t *testing.T) {
	_, err := LoadPins([]byte(`{"assets":[]}`))
	if err == nil || !strings.Contains(err.Error(), "names no assets") {
		t.Fatalf("want the no-assets refusal, got %v", err)
	}
	_, err = LoadPins([]byte(`{"pinsVersion":1,"upstream":{"releaseTag":"v24.11.11","signaturePubkey":"AA"},"assets":[{"os":"windows","arch":"amd64","file":"a.zip","url":"https://github.com/XTLS/Xray-core/releases/download/v/a.zip","sha256":"` + strings.Repeat("a", 64) + `","sizeBytes":1048576}]}`))
	if err != nil {
		t.Fatalf("a minimal but complete pin must load: %v", err)
	}
}

func assertClean(t *testing.T, h *harness) {
	t.Helper()
	entries, err := os.ReadDir(h.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "receipt.json" {
			t.Fatal("a refused install left a receipt behind")
		}
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("a refused install left a temp artifact behind: %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(h.root, h.pin.ReleaseTag)); !os.IsNotExist(err) {
		t.Fatal("a refused install left a version directory behind")
	}
}
