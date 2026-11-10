package health

// These tests drive the bookkeeping directly: they hand the prober the answers `Probe` would have produced, because
// the streak, the cooldown, the ring and the event are the subject and a socket would only make them slower to
// explain. The wire itself is tested in probe_test.go, over loopback, against a process that speaks SOCKS5.

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/compile"
)

const stateServerID = "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01" // nexora-secret-allow: fixture id invented for this test

// stateProber is a wired prober that will never be asked to measure: the clock and the event sink are captured so a
// test can read what the component decided rather than what it cost.
type stateProber struct {
	p      *Prober
	at     time.Time
	events []Event
}

func newStateProber(t *testing.T, tweak ...func(*Config)) *stateProber {
	t.Helper()
	sp := &stateProber{at: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)}
	cfg := Config{
		Server: func(context.Context, string) (Server, error) { return Server{}, nil },
		// A table with a row is what `note` reads nothing from and `Due` cares nothing about; Probe is not called in
		// this file, so the row exists only to satisfy the constructor that refuses a prober without one.
		Inbounds: func() []compile.Inbound {
			return []compile.Inbound{{Listen: "127.0.0.1", Port: 1, Tag: inboundTag(stateServerID)}}
		},
		Egress:   func() (string, error) { return "http://127.0.0.1:9/checkip", nil },
		Failover: compile.Failover{Enabled: true, CooldownSeconds: 60, MaxConsecutiveFailures: 3, ProbeIntervalSeconds: 120},
		Now:      func() time.Time { return sp.at },
		Events:   func(e Event) { sp.events = append(sp.events, e) },
	}
	for _, f := range tweak {
		f(&cfg)
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sp.p = p
	return sp
}

func (sp *stateProber) note(overall string, advance time.Duration) {
	sp.at = sp.at.Add(advance)
	sp.p.note(&Answer{ServerID: stateServerID, Overall: overall}, sp.at)
}

func (sp *stateProber) mustHealth(t *testing.T) Health {
	t.Helper()
	h, ok := sp.p.Health(stateServerID)
	if !ok {
		t.Fatal("the prober has recorded this server and cannot report it")
	}
	return h
}

func TestOneFailureDoesNotDeclareAServerDown(t *testing.T) {
	sp := newStateProber(t)
	sp.note(OverallFail, 0)
	h := sp.mustHealth(t)
	if h.ConsecutiveFailures != 1 {
		t.Errorf("the streak is %d, want 1", h.ConsecutiveFailures)
	}
	// §8 invariant 3 is that "the count, not a single timeout, drives failover", and the policy this prober was wired
	// with says three. A server evicted after one dropped packet is the failure mode that field exists to prevent.
	if h.Verdict != VerdictHealthy {
		t.Errorf("verdict is %q, want healthy: the policy has not declared this server out", h.Verdict)
	}
	if h.LastOverall != OverallFail {
		t.Errorf("LastOverall is %q, want fail: the status must not hide which way the last measurement went", h.LastOverall)
	}
	if h.CoolingDown {
		t.Error("a server inside its tolerance is not in cooldown")
	}
}

func TestTheThirdFailureIsTheOneThatClosesTheStreak(t *testing.T) {
	sp := newStateProber(t)
	sp.note(OverallFail, 0)
	sp.note(OverallFail, time.Second)
	if len(sp.events) != 1 {
		t.Fatalf("%d events after two failures, want the one that moved the verdict from unknown to healthy", len(sp.events))
	}
	sp.note(OverallFail, time.Second)
	h := sp.mustHealth(t)
	if h.Verdict != VerdictDown {
		t.Errorf("verdict is %q after the streak reached the policy's 3, want down", h.Verdict)
	}
	if !h.CoolingDown || !h.CooldownUntil.Equal(sp.at.Add(60*time.Second)) {
		t.Errorf("cooldown is %v until %v, want in force for the policy's 60 seconds", h.CoolingDown, h.CooldownUntil)
	}
	if len(sp.events) != 2 {
		t.Errorf("%d events, want two: healthy and then down", len(sp.events))
	}
}

func TestAFourthFailureOfTheSameStreakSaysNothingNew(t *testing.T) {
	sp := newStateProber(t)
	for i := 0; i < 3; i++ {
		sp.note(OverallFail, time.Second)
	}
	before := len(sp.events)
	sp.note(OverallFail, time.Second)
	if len(sp.events) != before {
		t.Errorf("the verdict moved twice and the prober published %d times: an event per probe is the cached answer arriving unasked", len(sp.events)-before+1)
	}
	if got := sp.mustHealth(t).ConsecutiveFailures; got != 4 {
		t.Errorf("the streak is %d, want 4: a longer failure is still the same verdict", got)
	}
}

func TestASuccessClearsTheStreakAndLiftsTheCooldown(t *testing.T) {
	sp := newStateProber(t)
	for i := 0; i < 3; i++ {
		sp.note(OverallFail, time.Second)
	}
	sp.note(OverallPass, time.Minute)
	h := sp.mustHealth(t)
	if h.Verdict != VerdictHealthy || h.ConsecutiveFailures != 0 || h.CoolingDown {
		t.Errorf("after a pass the record is %+v, want healthy, streak 0, no cooldown", h)
	}
	if len(sp.events) != 3 {
		t.Errorf("%d events, want three: the verdict moved three times", len(sp.events))
	}
}

func TestLeakedTrafficCountsAgainstTheStreak(t *testing.T) {
	sp := newStateProber(t)
	for i := 0; i < 3; i++ {
		sp.note(OverallNotProxied, time.Second)
	}
	if got := sp.mustHealth(t).Verdict; got != VerdictDown {
		t.Errorf("verdict is %q, want down: traffic leaving through someone else's address fails the question §8 asks", got)
	}
}

func TestAPartialProbeCountsAgainstTheStreak(t *testing.T) {
	sp := newStateProber(t)
	for i := 0; i < 3; i++ {
		sp.note(OverallPartial, time.Second)
	}
	if got := sp.mustHealth(t).Verdict; got != VerdictDown {
		t.Errorf("verdict is %q, want down: a server whose tunnel does not work is out whatever its TCP port says", got)
	}
}

func TestADownServerIsNotAskedAgainUntilItsCooldownEnds(t *testing.T) {
	sp := newStateProber(t)
	for i := 0; i < 3; i++ {
		sp.note(OverallFail, time.Second)
	}
	closed := sp.at
	sp.at = closed.Add(59 * time.Second)
	if sp.p.Due(stateServerID, sp.at) {
		t.Error("a server one second short of its cooldown was declared due")
	}
	sp.at = closed.Add(60 * time.Second)
	if !sp.p.Due(stateServerID, sp.at) {
		t.Error("the cooldown expired and the poller is still told to wait")
	}
}

func TestAHealthyServerIsAskedAgainOneIntervalAfterTheLastAnswer(t *testing.T) {
	sp := newStateProber(t)
	sp.note(OverallPass, 0)
	probed := sp.at
	sp.at = probed.Add(119 * time.Second)
	if sp.p.Due(stateServerID, sp.at) {
		t.Error("due before probeIntervalSeconds had passed")
	}
	sp.at = probed.Add(120 * time.Second)
	if !sp.p.Due(stateServerID, sp.at) {
		t.Error("not due one interval after the last probe")
	}
}

func TestAProberWithoutAnIntervalVolunteersNothing(t *testing.T) {
	sp := newStateProber(t, func(cfg *Config) {
		cfg.Failover = compile.Failover{Enabled: false}
	})
	sp.note(OverallPass, 0)
	sp.at = sp.at.Add(365 * 24 * time.Hour)
	if sp.p.Due(stateServerID, sp.at) {
		t.Error("an unset interval read as \"now\": a poll loop with that answer probes as fast as it can turn")
	}
}

func TestAServerNobodyHasProbedIsDueAndHasNoStatus(t *testing.T) {
	sp := newStateProber(t)
	if !sp.p.Due(stateServerID, sp.at) {
		t.Error("a server this process has never measured was reported as not needing a measurement")
	}
	if _, ok := sp.p.Health(stateServerID); ok {
		t.Error("Health answered for a server nobody probed: unknown and down are different facts")
	}
	if len(sp.p.History(stateServerID)) != 0 {
		t.Error("a server with no probes has a history")
	}
}

func TestTheRingKeepsTheNewestAndCannotGrowPastItsCap(t *testing.T) {
	sp := newStateProber(t, func(cfg *Config) { cfg.RingBytes = 320 })
	for i := 0; i < 500; i++ {
		sp.note(OverallPass, time.Second)
	}
	history := sp.p.History(stateServerID)
	if len(history) >= 500 {
		t.Fatalf("the ring holds %d entries after 500 probes with a 320 byte cap", len(history))
	}
	if len(history) == 0 {
		t.Fatal("the ring evicted everything, including the probe that just happened")
	}
	if history[len(history)-1].At != sp.at.UTC().Format(time.RFC3339Nano) {
		t.Error("the last entry is not the newest probe: eviction took the answer and kept the noise")
	}
	if got := sp.p.records[stateServerID].bytes; got > 320 {
		t.Errorf("the ring counts %d bytes against a cap of 320", got)
	}
}

func TestTheRingIsPerServer(t *testing.T) {
	sp := newStateProber(t)
	sp.p.note(&Answer{ServerID: "00000000-0000-4000-8000-000000000001", Overall: OverallPass}, sp.at) // nexora-secret-allow: fixture id invented for this test
	if len(sp.p.History(stateServerID)) != 0 {
		t.Error("one server's probe appeared in another server's history")
	}
	if len(sp.p.History("00000000-0000-4000-8000-000000000001")) != 1 { // nexora-secret-allow: fixture id invented for this test
		t.Error("the probe was not retained for the server it measured")
	}
}

func TestStatusesAreOrderedAndComplete(t *testing.T) {
	sp := newStateProber(t)
	for _, id := range []string{"00000000-0000-4000-8000-000000000009", stateServerID, "00000000-0000-4000-8000-000000000001"} { // nexora-secret-allow: fixture ids invented for this test
		sp.p.note(&Answer{ServerID: id, Overall: OverallPass}, sp.at)
	}
	got := sp.p.Statuses()
	if len(got) != 3 {
		t.Fatalf("%d statuses, want one per probed server", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].ServerID >= got[i].ServerID {
			t.Errorf("statuses are not ordered by id: %v", got)
		}
	}
}

func TestTheEventCarriesAServerIDAndNothingElse(t *testing.T) {
	sp := newStateProber(t)
	sp.note(OverallFail, 0)
	if len(sp.events) != 1 || sp.events[0].ServerID != stateServerID {
		t.Fatalf("published %+v, want one event naming this server", sp.events)
	}
	// observability.json's `neverInPayload` for this event is "latency numbers", and its `why` is that a value in an
	// event is a value a subscriber will show without re-reading. A second field is how that rule gets broken by
	// somebody adding a useful-looking one, so the shape is what is asserted.
	typ := reflect.TypeOf(Event{})
	if typ.NumField() != 1 || typ.Field(0).Name != "ServerID" {
		t.Errorf("Event has fields %v, want exactly ServerID", fieldNames(typ))
	}
}

func fieldNames(typ reflect.Type) []string {
	out := make([]string, typ.NumField())
	for i := range out {
		out[i] = typ.Field(i).Name
	}
	return out
}

func TestTheCooldownStartsAtTheProbeThatClosedTheStreak(t *testing.T) {
	sp := newStateProber(t)
	sp.note(OverallFail, 0)
	first := sp.at
	sp.note(OverallFail, 10*time.Second)
	sp.note(OverallFail, 10*time.Second)
	h := sp.mustHealth(t)
	// cooldownSeconds is what happens "when a Server stops working", and it stops working at the third failure — a
	// cooldown dated from the first would be 40 seconds shorter than the policy says, on every profile that probes
	// more slowly than its own grace period.
	if !h.CooldownUntil.Equal(first.Add(20*time.Second + 60*time.Second)) {
		t.Errorf("cooldown ends at %v, want 60 seconds after the probe that reached the threshold", h.CooldownUntil)
	}
}

func TestAProberWithoutFailoverConfiguredDecidesOnTheFirstFailure(t *testing.T) {
	sp := newStateProber(t, func(cfg *Config) {
		cfg.Failover = compile.Failover{Enabled: false, ProbeIntervalSeconds: 120}
	})
	sp.note(OverallFail, 0)
	if got := sp.mustHealth(t).Verdict; got != VerdictDown {
		t.Errorf("verdict is %q, want down: nothing was configured to tolerate more than one failure", got)
	}
}

func TestNoRefusalCodeIsOneTheCommandDoesNotDeclare(t *testing.T) {
	for _, sentinel := range []error{ErrNoServer, ErrCoreNotRunning, ErrNoEgress, ErrDeadline} {
		e := refuse("probe", stateServerID, sentinel)
		if !contains(serverTestErrors, e.Code()) {
			t.Errorf("%v answers with %q, which server.test.json#/errors does not declare", sentinel, e.Code())
		}
		code, ok := e.Taxonomy()
		if !ok {
			t.Errorf("%q is not in the projected taxonomy", e.Code())
			continue
		}
		if code.Layer == "" {
			t.Errorf("%q resolved without a layer, so a client cannot route it", code.Name)
		}
	}
	if got := fmt.Sprint(refuse("probe", stateServerID, fmt.Errorf("wrapped: %w", ErrCoreNotRunning))); !strings.Contains(got, stateServerID) {
		t.Errorf("the refusal %q does not name the server it refused", got)
	}
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
