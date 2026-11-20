package apply

import (
	"bufio"
	"github.com/k2dms/nexora/connector/internal/compile"
	"github.com/k2dms/nexora/connector/internal/contractgen"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// VER-44's runtime half, run against the pinned release the same way the golden drive is. The compiler
// writes `"access": "none"` into every config it emits (doc 36 §9: relying on an upstream default for a
// privacy control is not a control), and the config half is pinned by the goldens. What no test showed is
// a *started* Core: this test starts the pinned binary with a materialized golden, opens the socks
// inbound so a configured access log would have something to write, and asserts no log file appears in
// the Core's working directory — the destination address of a user connection is the browsing history the
// product exists to keep off the disk.
func TestTheStartedCoreWritesNoAccessLog(t *testing.T) {
	bin := pinnedXray(t)
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("locating the repository root: %v", err)
	}
	input := compile.Input{
		Settings: driveExample[compile.Settings](t, root, "settings.default.json"),
		Failure:  driveExample[compile.Failure](t, root, "failure-policy.default.json"),
	}
	input.Servers = []compile.Server{driveExample[compile.Server](t, root, "server.vision-tcp-tls.json")}
	profile := driveExample[compile.Profile](t, root, "profile.fixed.json")
	profile.ServerIDs = []string{input.Servers[0].ID}
	profile.DefaultServerID = profile.ServerIDs[0]
	input.Profiles = []compile.Profile{profile}
	compiled, err := compile.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	// Working directory is where a default-configured Xray would write its logs; the emitted section must
	// make that impossible. Write puts the materialized config in ConfigDir, and Core runs with its working
	// directory there.
	configDir := t.TempDir()
	out, err := Write(configDir, compiled.Document, fakeStore{
		"keystore://server/3f2a1c9e/user-id": driveSecret,
	})
	if err != nil {
		t.Fatalf("materializing the golden: %v", err)
	}

	ctx := exec.Command(bin, "run", "-c", out.Path)
	ctx.Dir = configDir
	stderr, err := ctx.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.Start(); err != nil {
		t.Fatalf("starting Core: %v", err)
	}
	running := true
	t.Cleanup(func() {
		if running {
			ctx.Process.Signal(syscall.SIGTERM)
			done := make(chan struct{})
			go func() { ctx.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				ctx.Process.Kill()
				<-done
			}
			running = false
		}
	})
	stderrText := make([]byte, 8192)

	var port int
	for _, inb := range compiled.Inbounds {
		port = inb.Port
		break
	}
	if port == 0 {
		t.Fatal("the compiled config names no inbound port")
	}
	deadline := time.Now().Add(15 * time.Second)
	var conn net.Conn
	startErr := ""
	for time.Now().Before(deadline) {
		c, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), 500*time.Millisecond)
		if dialErr == nil {
			conn = c
			break
		}
		if ctx.ProcessState != nil && ctx.ProcessState.Exited() {
			n, _ := stderr.Read(stderrText)
			startErr = string(stderrText[:n])
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("Core never accepted on the golden's inbound: %s", startErr)
	}
	// A socks handshake greeting: enough for a configured access log to record the connection.
	conn.Write([]byte{0x05, 0x01, 0x00})
	bufio.NewReader(conn).Read(make([]byte, 2))
	conn.Close()

	time.Sleep(1500 * time.Millisecond)
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".log" {
			logs = append(logs, e.Name())
		}
	}
	if len(logs) != 0 {
		t.Errorf("the started Core wrote %v in its working directory; the emitted log section did not suppress it", logs)
	}
	running = false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
