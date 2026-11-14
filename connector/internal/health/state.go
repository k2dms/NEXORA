package health

// This file is the retained half of `docs/architecture/22-component-contracts.md` §8's Out — "an event plus a
// retained status" — and of the `health-history` row of `packages/schema/protocol/data-ownership.json#/stores`, whose
// writer is named as this component and whose holds are "probe results, latencies, failure streaks".
//
// The streak counter is the reason the file exists at all. §8 invariant 3 is that "consecutive failures are counted
// and the count, not a single timeout, drives failover", and `compile.Failover` has carried those three numbers
// through the compiler since internal/compile was written while nothing on disk read them. One timeout is a fact
// about an afternoon; three in a row is a fact about a server.

import (
	"encoding/json"
	"sort"
	"time"
)

// DefaultRingBytes is the `health` row's cap in `docs/architecture/36-observability.md` §8's table of diagnostics
// sections — 64 KiB, the smallest in it, which is that table's way of saying a probe history is a hint for a support
// bundle and not a log. The data-ownership row's own bound is the sentence the cap exists for: "ring-buffered by
// design: a user cannot fill a disk by probing more".
const DefaultRingBytes = 64 * 1024

// Entry is one retained probe. Three fields, because those are the three the store's `holds` lists, and a fourth
// would be a fact the diagnostics section cannot get anywhere else: the per-layer rows live in the command's answer,
// and an answer that mattered was read when it arrived.
type Entry struct {
	ServerID string   `json:"serverId"`
	At       string   `json:"at"`
	Overall  string   `json:"overall"`
	RTTMS    *float64 `json:"rttMs,omitempty"`
	Failures int      `json:"failures"`
}

// record is one server's state between probes. It is not the answer to a probe and never travels on a frame: the
// event payload is a server id and the status is read back through Health, which is what
// `packages/schema/observability.json#/events/6/why` means by "a hint, not a state".
type record struct {
	verdict     string
	lastOverall string
	consecutive int
	lastProbe   time.Time
	nextProbe   time.Time
	cooling     bool
	cooldownEnd time.Time
	entries     []Entry
	bytes       int
}

// note folds one answer into the retained state and publishes if the verdict moved.
//
// Anything but `pass` is a failure for the counter, including `not-proxied`: the verdict §8 is about is "whether
// egress through Core actually works", and a server whose traffic leaves through someone else's address fails that
// question as certainly as one that refuses the connection. `partial` fails for the same reason, which is also why
// the lower layers cannot rescue it — a TCP connect is not what was being asked about.
func (p *Prober) note(a *Answer, at time.Time) {
	failed := a.Overall != OverallPass

	p.mu.Lock()
	defer p.mu.Unlock()

	r := p.records[a.ServerID]
	if r == nil {
		r = &record{verdict: VerdictUnknown}
		p.records[a.ServerID] = r
	}
	previous := r.verdict
	if failed {
		r.consecutive++
	} else {
		r.consecutive = 0
	}
	// The threshold is the policy's, and a prober whose wiring carried no failover policy still has to decide
	// something: one failure marks the server down when nothing was configured to tolerate more than that.
	threshold := 1
	if p.cfg.Failover.Enabled && p.cfg.Failover.MaxConsecutiveFailures > 0 {
		threshold = p.cfg.Failover.MaxConsecutiveFailures
	}
	if r.consecutive >= threshold {
		r.verdict = VerdictDown
	} else {
		r.verdict = VerdictHealthy
	}

	// The cooldown starts at the probe that closed the streak, not at the first failure: `cooldownSeconds`'s own
	// description in `packages/schema/connector-profile.schema.json` is about what happens "when a Server stops
	// working", and a server only stops working when the policy says it has.
	wasCooling := r.cooling
	r.cooling = r.verdict == VerdictDown && p.cfg.Failover.CooldownSeconds > 0
	if r.cooling {
		if !wasCooling || r.cooldownEnd.Before(at) {
			r.cooldownEnd = at.Add(time.Duration(p.cfg.Failover.CooldownSeconds) * time.Second)
		}
	} else {
		r.cooldownEnd = time.Time{}
	}

	r.lastProbe = at
	r.nextProbe = p.scheduled(r, at)
	if r.cooling {
		r.nextProbe = r.cooldownEnd
	}

	r.lastOverall = a.Overall
	p.retain(r, a, at)

	// The medium the store row names is disk (VER-86): the retained rings reach it on every note, while
	// the prober's lock is already held and the caller cannot observe a failed write.
	p.persistLocked()

	if previous != r.verdict && p.cfg.Events != nil {
		p.cfg.Events(Event{ServerID: a.ServerID})
	}
}

// scheduled is when the automatic prober may next ask, which is `probeIntervalSeconds` after the probe just recorded.
// A zero answer means "this prober will not volunteer": an unset interval has no default in Go's sense, because a
// poller that read a zero time as "now" would probe as fast as it could loop, and §8 invariant 4 exists to stop
// exactly that.
func (p *Prober) scheduled(r *record, at time.Time) time.Time {
	if p.cfg.Failover.ProbeIntervalSeconds <= 0 {
		return time.Time{}
	}
	return at.Add(time.Duration(p.cfg.Failover.ProbeIntervalSeconds) * time.Second)
}

// retain appends one entry and evicts from the front until the ring fits. The size is the marshalled length rather
// than a guess at it, because the bound the section carries is a byte bound and a per-record constant would drift
// the moment a field is added.
func (p *Prober) retain(r *record, a *Answer, at time.Time) {
	entry := Entry{
		ServerID: a.ServerID,
		At:       at.UTC().Format(time.RFC3339Nano),
		Overall:  a.Overall,
		Failures: r.consecutive,
	}
	for _, m := range a.Layers {
		if m.Layer == LayerDNS || m.RTTMS == nil {
			continue
		}
		// One latency per probe, from the first row that measured something: the `health` section is a support hint,
		// and the four-row breakdown is in the answer the caller already has.
		v := *m.RTTMS
		entry.RTTMS = &v
		break
	}
	data, err := json.Marshal(entry)
	size := len(data)
	if err != nil {
		// An entry this package cannot marshal is an entry it cannot count against a byte bound, so it is not kept:
		// the alternative is an unbounded ring wearing the name of a bounded one.
		return
	}
	r.entries = append(r.entries, entry)
	r.bytes += size
	for r.bytes > p.cfg.RingBytes && len(r.entries) > 1 {
		dropped, err := json.Marshal(r.entries[0])
		if err == nil {
			r.bytes -= len(dropped)
		}
		r.entries = append(r.entries[:0], r.entries[1:]...)
	}
}

// Health is the retained status §8's Out names. The second result is the difference between "never probed" and
// "probed and down", which is the difference between a grey mark and a red one.
func (p *Prober) Health(serverID string) (Health, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.records[serverID]
	if !ok {
		return Health{}, false
	}
	return r.status(serverID), true
}

// Statuses is every server this prober has an opinion about, ordered by id. It is the body of the `health`
// diagnostics section (`packages/schema/observability.json#/diagnosticsSections`), which names `health-history` as
// its store and this component as its writer.
func (p *Prober) Statuses() []Health {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Health, 0, len(p.records))
	for id, r := range p.records {
		out = append(out, r.status(id))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServerID < out[j].ServerID })
	return out
}

func (r *record) status(serverID string) Health {
	return Health{
		ServerID:            serverID,
		Verdict:             r.verdict,
		LastOverall:         r.lastOverall,
		ConsecutiveFailures: r.consecutive,
		CoolingDown:         r.cooling,
		CooldownUntil:       r.cooldownEnd,
		LastProbeAt:         r.lastProbe,
		NextProbeAt:         r.nextProbe,
	}
}

// Due reports whether the automatic prober may ask about a server at `at`. It is a question rather than a gate on
// Probe: a user who pressed "test" is entitled to a measurement now, and `packages/schema/protocol/framing.json`
// `#/senderPolicy/rateLimits` is what bounds them, not this interval — which exists so that a poll loop does not
// become the probe storm §8 invariant 4 was written against.
//
// A server in cooldown is not due until the cooldown has passed, and one with no interval configured is never due:
// the first is the policy's own grace and the second is the absence of a policy to schedule against.
func (p *Prober) Due(serverID string, at time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.records[serverID]
	if !ok {
		return true
	}
	if r.cooling {
		return !at.Before(r.cooldownEnd)
	}
	return !r.nextProbe.IsZero() && !at.Before(r.nextProbe)
}

// History is the ring, oldest first — the answer of the `health` diagnostics section and the only way a support
// bundle can say "it has been failing since Tuesday" rather than "it is failing".
func (p *Prober) History(serverID string) []Entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.records[serverID]
	if !ok {
		return nil
	}
	return append([]Entry(nil), r.entries...)
}
