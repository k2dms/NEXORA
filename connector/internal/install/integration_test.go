package install

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"crypto/ed25519"
	"crypto/rand"
	"github.com/k2dms/nexora/connector/internal/receipt"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

// VER-87's two halves, closed in-repo. The installer writes the receipt the pre-exec verifier reads
// (TestInstallHappyPath...), and this test runs the supervisor's own spawn path against that receipt:
// the pinned release is downloaded from an httptest origin, installed, and then *started* through
// supervisor.New with Verify = receipt.NewVerifier — the exact wiring main.go builds. A started Core is
// the "live core.start" half; a tampered binary is the refusal half. Env-gated on the pinned binary the
// same way the golden drive is.
func TestTheSpawnPathConsumesTheInstallerReceipt(t *testing.T) {
	bin := pinnedXrayEnv(t)
	root := t.TempDir()

	// A minimal config the pinned release accepts: one loopback socks inbound, nothing else.
	freePort := freePort(t)
	configPath := filepath.Join(root, "config.json")
	minimal := `{"log":{"loglevel":"warning"},"inbounds":[{"listen":"127.0.0.1","port":` +
		itoaPort(freePort) + `,"protocol":"socks","settings":{"auth":"noauth","udp":false}}],"outbounds":[{"protocol":"freedom"}]}`
	if err := os.WriteFile(configPath, []byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}

	// The release asset: the pinned binary itself, zipped under the name the installer extracts.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entryName := "xray.exe"
	if runtime.GOOS != "windows" {
		entryName = "xray"
	}
	fh := &zip.FileHeader{Name: entryName, Method: zip.Store}
	fw, err := zw.CreateHeader(fh)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, zipBytes))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	defer server.Close()

	pin := Pin{
		PinsVersion:              "1",
		ReleaseTag:               "v24.11.11",
		SignaturePubkey:          base64.StdEncoding.EncodeToString(pub),
		RetainedPreviousVersions: 1,
		Assets: []Asset{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, File: "Xray-test.zip",
			URL: server.URL + "/Xray-test.zip", SHA256: sha256Hex(zipBytes), Signature: sig, SizeBytes: int64(len(zipBytes)),
		}},
	}
	inst := &Installer{Root: root, Now: func() time.Time { return time.Unix(0, 0).UTC() }}
	res, err := inst.Install(context.Background(), pin, []string{entryName})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	// The spawn path main.go wires: the supervisor's Verify is receipt.NewVerifier over the receipt the
	// installer wrote, and Launch spawns the receipt's own path.
	verifier := receipt.NewVerifier(res.ReceiptPath)
	if err := verifier(res.BinaryPath); err != nil {
		t.Fatalf("the pre-exec verifier refused the installer's receipt: %v", err)
	}
	s, err := supervisor.New(supervisor.Config{
		Dir: root,
		Launch: func(configPath string) (*exec.Cmd, error) {
			return exec.Command(res.BinaryPath, "run", "-c", configPath), nil
		},
		Verify: verifier,
		Death:  supervisor.Death{},
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Start(context.Background(), supervisor.StartParams{ConfigPath: configPath, ConfigHash: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatalf("core.start through the installer receipt: %v", err)
	}
	// A running Core is the proof the receipt survived its own producer: the supervisor hashed the
	// installer's binary, compared it to the installer's receipt, and only then exec'd.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s.Status().Running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !s.Status().Running {
		t.Fatalf("the started Core is not running: %+v", started)
	}
	if _, err := s.Stop(context.Background(), false); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// The refusal half: one rewritten byte and the same spawn path refuses before exec.
	if err := os.WriteFile(res.BinaryPath, append(binary, 'x'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifier(res.BinaryPath); err == nil {
		t.Fatal("the pre-exec verifier accepted a tampered binary")
	}
}

func pinnedXrayEnv(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("NEXORA_XRAY_BIN")
	if bin == "" {
		t.Skipf("NEXORA_XRAY_BIN is not set; the started-Core proof needs the pinned release (the golden drive skips the same way)")
	}
	return bin
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func itoaPort(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
