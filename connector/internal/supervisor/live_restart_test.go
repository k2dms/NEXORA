package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/receipt"
)

// This is the live half of the ladder the fake-Core tests measure arithmetically: a real Xray 24.11.11 spawned
// through the production receipt.Load / receipt.NewVerifier path, killed from outside, and watched recovering,
// exhausting and re-arming. It skips unless NEXORA_XRAY_BIN names the pinned binary, so the default `go test`
// run stays the SKIPPED live row and ADR-0019 rule 4 keeps the binary out of the repository.
func TestLiveCoreRestart(t *testing.T) {
	bin := os.Getenv("NEXORA_XRAY_BIN")
	if bin == "" {
		t.Skip("set NEXORA_XRAY_BIN=E:/NEXORA/tmp/xray-extract/xray.exe to run the live Core restart")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("NEXORA_XRAY_BIN must be an absolute path to Xray 24.11.11")
	}
	vctx, vcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer vcancel()
	vcmd := exec.CommandContext(vctx, bin, "version")
	vcmd.WaitDelay = time.Second
	out, err := vcmd.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "Xray 24.11.11 ") {
		t.Fatalf("expected pinned Xray 24.11.11: %v\n%s", err, out)
	}

	t.Run("recovery", func(t *testing.T) {
		s, ev, addr, configPath := liveSupervisor(t, bin, Death{Attempts: 3, Backoff: []time.Duration{time.Second, time.Second, time.Second}, Stability: time.Hour, Notify: true})
		first := liveStart(t, s, addr, configPath)
		liveDial(t, addr)
		if !first.Contained {
			t.Fatalf("Core was not contained: %s", first.ContainmentNote)
		}
		liveKill(t, first.PID)
		after := liveWait(t, s, func(st Status) bool { return st.Running && st.PID != first.PID }, 8*time.Second)
		if after.AttemptsMade != 1 || after.Blocked {
			t.Fatalf("recovery should spend one attempt and lift the block: %+v", after)
		}
		if ev.count(KindExited) < 1 || ev.count(KindBlocked) < 1 || ev.count(KindRestart) != 1 {
			t.Fatalf("expected exited + blocked-before-retry + one restart, saw %v", ev.kinds())
		}
		if e := ev.last(t, KindStarted); e.Attempt != 1 {
			t.Fatalf("the recovered spawn is attempt 1, got %d", e.Attempt)
		}
		liveDial(t, addr)
		liveStop(t, s)
	})

	t.Run("exhaustion", func(t *testing.T) {
		s, ev, addr, configPath := liveSupervisor(t, bin, Death{Attempts: 3, Backoff: []time.Duration{time.Second, time.Second, time.Second}, Stability: time.Hour, Notify: true})
		cur := liveStart(t, s, addr, configPath)
		// Three deaths inside the stability window resume the same episode: attempts 1, 2, 3 each recover.
		for want := 1; want <= 3; want++ {
			liveKill(t, cur.PID)
			cur = liveWait(t, s, func(st Status) bool { return st.Running && st.AttemptsMade == want }, 8*time.Second)
		}
		starts := ev.count(KindStarted)
		// The fourth death finds the budget spent: the block is terminal and no attempt runs.
		liveKill(t, cur.PID)
		dead := liveWait(t, s, func(st Status) bool { return st.Blocked && !st.Running && st.Restarting == false }, 8*time.Second)
		if dead.AttemptsMade != 3 {
			t.Fatalf("terminal block keeps the three spent attempts: %+v", dead)
		}
		time.Sleep(1300 * time.Millisecond) // longer than one backoff step: a respawn would have happened
		if ev.count(KindStarted) != starts {
			t.Fatalf("a spent episode must not spawn again: starts %d -> %d", starts, ev.count(KindStarted))
		}
		if st := s.Status(); !st.Blocked || st.Running {
			t.Fatalf("block must stay in force until the user acts: %+v", st)
		}
		liveStop(t, s)
	})

	t.Run("rearm", func(t *testing.T) {
		s, _, addr, configPath := liveSupervisor(t, bin, Death{Attempts: 2, Backoff: []time.Duration{time.Second, time.Second}, Stability: time.Second, Notify: true})
		cur := liveStart(t, s, addr, configPath)
		// A death inside the window spends attempt 1.
		liveKill(t, cur.PID)
		cur = liveWait(t, s, func(st Status) bool { return st.Running && st.AttemptsMade == 1 }, 8*time.Second)
		// Surviving past stabilityWindowSeconds earns a fresh budget, so the next death resumes at 1, not 2.
		time.Sleep(1100 * time.Millisecond)
		liveKill(t, cur.PID)
		rearmed := liveWait(t, s, func(st Status) bool { return st.Running && st.PID != cur.PID }, 8*time.Second)
		if rearmed.AttemptsMade != 1 {
			t.Fatalf("a Core that outlived the stability window re-arms to a fresh budget, got attempts=%d", rearmed.AttemptsMade)
		}
		liveStop(t, s)
	})
}

// liveSupervisor wires the production seams over a real receipt naming bin, a free loopback port and a minimal
// config Xray accepts. The Launch captures diagnostics behind a lock (production sets neither stream) so a
// startup failure can be read on the cleanup path.
func liveSupervisor(t *testing.T, bin string, death Death) (*Supervisor, *recorder, string, string) {
	t.Helper()
	dir := t.TempDir()
	coreDir := filepath.Join(dir, "core")
	if err := os.MkdirAll(coreDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sum, err := receipt.HashFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	r := receipt.Receipt{Tag: "v24.11.11", PinsVersion: "1", SHA256: sum, VerifiedAt: time.Now().UTC().Format(time.RFC3339), Path: bin}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(coreDir, "receipt.json")
	if err := os.WriteFile(receiptPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	port := liveFreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cfg := fmt.Sprintf(`{"log":{"loglevel":"error"},"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth","udp":false},"tag":"live-in"}],"outbounds":[{"protocol":"freedom","tag":"direct"}]}`, port)
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	diag := new(safeBuffer)
	launch := func(configPath string) (*exec.Cmd, error) {
		loaded, err := receipt.Load(receiptPath)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(loaded.Path, "run", "-config", configPath)
		cmd.Stdout, cmd.Stderr = diag, diag
		return cmd, nil
	}
	ev := new(recorder)
	s, err := New(Config{Dir: filepath.Join(dir, "state"), Launch: launch, Verify: receipt.NewVerifier(receiptPath), Death: death, Events: ev.publish})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		liveStop(t, s)
		if t.Failed() {
			t.Logf("Xray diagnostics:\n%s", diag.String())
		}
	})
	return s, ev, addr, configPath
}

func liveStart(t *testing.T, s *Supervisor, addr, configPath string) Status {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := s.Start(ctx, StartParams{ConfigPath: configPath, ConfigHash: "live-restart-hash", ProfileID: "live", Inbounds: []Inbound{{Listen: host, Port: port, Tag: "live-in"}}})
	if err != nil {
		t.Fatalf("live start: %v", err)
	}
	if !st.Running || st.PID == 0 {
		t.Fatalf("Core did not start: %+v", st)
	}
	return *st
}

func liveDial(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Core never bound the loopback inbound at %s", addr)
}

func liveKill(t *testing.T, pid int) {
	t.Helper()
	p, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatalf("kill Core %d: %v", pid, err)
	}
}

func liveWait(t *testing.T, s *Supervisor, pred func(Status) bool, timeout time.Duration) Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if st := s.Status(); pred(st) {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s; last status %+v", timeout, s.Status())
	return Status{}
}

func liveStop(t *testing.T, s *Supervisor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Stop(ctx, false); err != nil {
		t.Logf("live stop: %v", err)
	}
}

func liveFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// safeBuffer is the diagnostics sink: Xray's own startup/shutdown lines from successive spawns land here, and the
// supervisor publishes events under its lock while a child may still be writing, so the buffer needs its own.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
