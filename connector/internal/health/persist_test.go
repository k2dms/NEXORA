package health

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/compile"
)

// The medium the health-history row names is disk (VER-86): the ring a restart reads back is the same
// ring the dying process held. The closing assertion is this file — the test that notes probes through
// one prober, builds a second prober over the same state directory, and compares the rings.

func persistConfig(t *testing.T, dir string, tweak ...func(*Config)) Config {
	cfg := Config{
		Server: func(context.Context, string) (Server, error) { return Server{}, nil },
		Inbounds: func() []compile.Inbound {
			return []compile.Inbound{{Listen: "127.0.0.1", Port: 1, Tag: inboundTag(stateServerID)}}
		},
		Egress:   func() (string, error) { return "http://127.0.0.1:9/checkip", nil },
		StateDir: dir,
	}
	for _, f := range tweak {
		f(&cfg)
	}
	return cfg
}

func newDiskProber(t *testing.T, dir string, tweak ...func(*Config)) *Prober {
	t.Helper()
	p, err := New(persistConfig(t, dir, tweak...))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func noteAt(p *Prober, overall string, at time.Time) {
	p.note(&Answer{ServerID: stateServerID, Overall: overall}, at)
}

func TestTheRingSurvivesAProcessRestart(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

	// The dying process: three consecutive failures, the third of which condemns the server.
	first := newDiskProber(t, dir, func(c *Config) { c.Failover.MaxConsecutiveFailures = 3 })
	for i := 0; i < 3; i++ {
		noteAt(first, OverallFail, at.Add(time.Duration(i)*time.Second))
	}
	got, ok := first.Health(stateServerID)
	if !ok {
		t.Fatal("the dying process has no opinion about the server")
	}
	if got.Verdict != VerdictDown {
		t.Fatalf("verdict is %q, want down after three consecutive failures", got.Verdict)
	}
	if err := first.PersistError(); err != nil {
		t.Fatalf("the disk write failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, PersistFileName)); err != nil {
		t.Fatalf("the medium is not on disk: %v", err)
	}

	// The restarted process: a new Prober over the same directory, no notes of its own.
	second := newDiskProber(t, dir)
	got, ok = second.Health(stateServerID)
	if !ok {
		t.Fatal("the restarted prober lost the retained status entirely")
	}
	if got.Verdict != VerdictDown || got.ConsecutiveFailures != 3 {
		t.Fatalf("the restarted prober forgave the streak: verdict %q, %d failures", got.Verdict, got.ConsecutiveFailures)
	}
	before := first.History(stateServerID)
	after := second.History(stateServerID)
	if len(before) != len(after) || len(after) != 3 {
		t.Fatalf("history is %d entries after restart, want the %d the dying process held", len(after), len(before))
	}
	for i := range before {
		if before[i].At != after[i].At || before[i].Overall != after[i].Overall || before[i].Failures != after[i].Failures {
			t.Fatalf("entry %d differs across the restart: %+v vs %+v", i, before[i], after[i])
		}
	}
}

func TestAStreakContinuesAcrossARestart(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	first := newDiskProber(t, dir, func(c *Config) { c.Failover.MaxConsecutiveFailures = 3 })
	noteAt(first, OverallFail, at)
	noteAt(first, OverallFail, at.Add(time.Second))

	second := newDiskProber(t, dir, func(c *Config) { c.Failover.MaxConsecutiveFailures = 3 })
	// The third failure lands on the restarted process: the streak, not the new process, condemns.
	noteAt(second, OverallFail, at.Add(2*time.Second))
	got, _ := second.Health(stateServerID)
	if got.Verdict != VerdictDown || got.ConsecutiveFailures != 3 {
		t.Fatalf("a restart forgave the streak: verdict %q after %d", got.Verdict, got.ConsecutiveFailures)
	}
	if len(second.History(stateServerID)) != 3 {
		t.Fatalf("the restarted ring holds %d entries, want all three", len(second.History(stateServerID)))
	}
}

func TestAMissingOrCorruptDocumentStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PersistFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seeding a corrupt document: %v", err)
	}
	p := newDiskProber(t, dir)
	if _, ok := p.Health(stateServerID); ok {
		t.Fatal("a corrupt document produced retained state")
	}
	if len(p.Statuses()) != 0 {
		t.Fatal("a corrupt document produced statuses")
	}
	// And the next note rebuilds the document rather than dying on it.
	noteAt(p, OverallPass, time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC))
	if err := p.PersistError(); err != nil {
		t.Fatalf("the rebuild failed: %v", err)
	}
}

func TestAProberWithoutAStateDirKeepsMemoryOnly(t *testing.T) {
	dir := t.TempDir()
	p := newDiskProber(t, dir, func(c *Config) { c.StateDir = "" })
	noteAt(p, OverallPass, time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC))
	if _, err := os.Stat(filepath.Join(dir, PersistFileName)); !os.IsNotExist(err) {
		t.Fatalf("a memory-only prober wrote to disk: %v", err)
	}
	if p.PersistError() != nil {
		t.Fatalf("memory-only persistence reported an error: %v", p.PersistError())
	}
}
