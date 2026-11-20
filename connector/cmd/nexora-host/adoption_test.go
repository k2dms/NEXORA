package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/localpipe"
)

// TestAdoptionAcrossPortRecreation is the live fixture for VER-19 and the artefact that retires ODEC-01: a shared
// Connector survives the loss of a native port, a second host adopts it instead of spawning a second one, the
// instance record names the same process both times, and a repeated frame id is answered from the cache.
func TestAdoptionAcrossPortRecreation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the adoption fixture drives Windows named pipes, DPAPI and a real Connector process; the Linux runner has no pipe to adopt (the golden test asserts win32 the same way)")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Fatalf("module root not found: %v", err)
	}

	connectorBin := filepath.Join(t.TempDir(), "nexora-connector.test.exe")
	build := exec.Command("go", "test", "-c", "-o", connectorBin, "./cmd/nexora-connector")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the connector test binary: %v\n%s", err, out)
	}

	root := t.TempDir()
	base := root
	root = filepath.Join(base, "NEXORA")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	os.Setenv("LOCALAPPDATA", base)
	t.Cleanup(func() { os.Unsetenv("LOCALAPPDATA") })

	stateDir := filepath.Join(root, "state")
	configDir := filepath.Join(root, "config")
	keystoreDir := filepath.Join(root, "keystore")
	coreDir := filepath.Join(root, "core")
	for _, d := range []string{stateDir, configDir, keystoreDir, coreDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	senderIDs := []string{pinnedOrigin}
	senderIDsJSON, _ := json.Marshal(senderIDs)

	owner := exec.Command(connectorBin, "-test.run=TestAdoptionSharedOwner", "-test.count=1")
	owner.Env = append(os.Environ(),
		"NEXORA_ADOPTION_OWNER=1",
		"NEXORA_ADOPTION_STATE_DIR="+stateDir,
		"NEXORA_ADOPTION_CONFIG_DIR="+configDir,
		"NEXORA_ADOPTION_KEYSTORE_DIR="+keystoreDir,
		"NEXORA_ADOPTION_CORE_DIR="+coreDir,
		"NEXORA_ADOPTION_INSTANCE_ROOT="+root,
		"NEXORA_ADOPTION_HOST_EXE="+os.Args[0],
		"NEXORA_ADOPTION_VERSION=1.0.0",
		"NEXORA_ADOPTION_SENDER_IDS="+string(senderIDsJSON),
	)
	owner.Stderr = os.Stderr
	if err := owner.Start(); err != nil {
		t.Fatalf("starting the shared owner: %v", err)
	}
	defer func() {
		_ = owner.Process.Kill()
		_ = owner.Wait()
	}()

	// Wait for the owner to have written its instance record (which it does only after its listeners are up).
	deadline := time.Now().Add(15 * time.Second)
	var firstRecord localpipe.InstanceRecord
	for time.Now().Before(deadline) {
		rec, err := localpipe.ReadInstanceRecord(root)
		if err == nil {
			firstRecord = rec
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if firstRecord.PID == 0 {
		t.Fatal("the shared owner did not publish an instance record")
	}

	allowlist := allowlistFile(t, `["`+pinnedOrigin+`"]`)
	cfg := config{senderID: pinnedOrigin, allowlistPath: allowlist, connectorBin: connectorBin}

	helloArgs := `{"extensionVersion":"1.0.0","protocolVersion":1,"minCompatibleProtocolVersion":1}`

	// First port: establish the hello session, then issue the frame whose answer will be cached.
	browser1, ext1 := net.Pipe()
	done1 := make(chan error, 1)
	open1 := sharedOpen(connectorBin, allowlist)
	go func() { done1 <- run(cfg, browser1, open1, os.Stderr) }()

	writeFrame(t, ext1, requestPayload(t, "adopt-hello-01", "connector.hello", helloArgs))
	helloBack1 := readFrame(t, ext1)
	_ = helloBack1

	payload := requestPayload(t, "adopt-replay-01", "connector.version", `{}`)
	writeFrame(t, ext1, payload)
	back1 := readFrame(t, ext1)
	_ = ext1.Close()
	if err := <-done1; err != nil {
		t.Fatalf("first host run ended with %v", err)
	}

	// Second port: adopt again, re-establish hello, then replay the cached answer for the same id.
	browser2, ext2 := net.Pipe()
	done2 := make(chan error, 1)
	open2 := sharedOpen(connectorBin, allowlist)
	go func() { done2 <- run(cfg, browser2, open2, os.Stderr) }()

	writeFrame(t, ext2, requestPayload(t, "adopt-hello-02", "connector.hello", helloArgs))
	_ = readFrame(t, ext2)

	writeFrame(t, ext2, payload)
	back2 := readFrame(t, ext2)
	_ = ext2.Close()
	if err := <-done2; err != nil {
		t.Fatalf("second host run ended with %v", err)
	}

	f1, err := envelope.Parse(back1)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := envelope.Parse(back2)
	if err != nil {
		t.Fatal(err)
	}
	if f1.Result == nil || f2.Result == nil || string(f1.Result) != string(f2.Result) {
		t.Fatalf("replay across port recreation returned a different result:\n first %s\nsecond %s", back1, back2)
	}
	if f1.Error != nil || f2.Error != nil {
		t.Fatalf("replay returned an error: first=%v second=%v", f1.Error, f2.Error)
	}

	secondRecord, err := localpipe.ReadInstanceRecord(root)
	if err != nil {
		t.Fatal(err)
	}
	if secondRecord.PID != firstRecord.PID {
		t.Fatalf("the owner changed across port recreation: %d -> %d (a second Connector was spawned)", firstRecord.PID, secondRecord.PID)
	}
	if secondRecord.StartedAt != firstRecord.StartedAt {
		t.Fatalf("the owner restarted across port recreation: %s -> %s", firstRecord.StartedAt, secondRecord.StartedAt)
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
