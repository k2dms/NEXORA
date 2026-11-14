package health

// This file is `health-history`'s medium (packages/schema/protocol/data-ownership.json#/stores): the row
// says `disk`, and what existed before it was a map behind the prober's mutex that died with the process
// (VER-86). The ring per server id is written to one JSON document under the state directory after every
// retained probe — atomic temp-plus-rename, the same discipline `internal/apply` uses — and is read back
// when a new Prober opens the same directory, so a Connector restart keeps the streak that drove a verdict
// and the history a support bundle reads.
//
// What survives is the retained half: the entries, the streak counter, the verdict they produced and the
// cooldown that is still running. What does not is in-flight probe state, which is `Due`'s business and
// re-derives from the clock on the next probe.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// timeJSONFormat is the RFC 3339 form the entry timestamps already use, stated once.
const timeJSONFormat = "2006-01-02T15:04:05.000000000Z07:00"

// PersistFileName is the document the medium names. It lives in the state directory the Connector already
// owns (doc 34 §7's per-user layout), beside the applied-state record it sits next to in
// data-ownership.json#/stores.
const PersistFileName = "health-history.json"

// persistedRecord is one server's ring and the streak state the verdict was computed from.
type persistedRecord struct {
	Entries     []Entry `json:"entries"`
	Verdict     string  `json:"verdict"`
	LastOverall string  `json:"lastOverall"`
	LastProbe   string  `json:"lastProbe,omitempty"`
	NextProbe   string  `json:"nextProbe,omitempty"`
	Cooling     bool    `json:"cooling,omitempty"`
	CooldownEnd string  `json:"cooldownEnd,omitempty"`
}

// persistedState is the whole file. One document, not one file per server: the row's "a ring per server
// id" is the shape of the state, not of the disk layout, and a single atomic write keeps the rings and
// the streaks mutually consistent.
type persistedState struct {
	Version  int                         `json:"version"`
	Records  map[string]*persistedRecord `json:"records"`
	Sequence int64                       `json:"sequence"`
}

// persistLocked writes the current state under mu. A write failure is remembered, not raised: a probe
// result that cannot reach the disk is a lost hint, not a failed probe, and a caller that cares reads it
// back through PersistError.
func (p *Prober) persistLocked() {
	if p.cfg.StateDir == "" {
		return
	}
	state := persistedState{Version: 1, Records: map[string]*persistedRecord{}, Sequence: p.sequence}
	for id, r := range p.records {
		pr := &persistedRecord{
			Entries:     r.entries,
			Verdict:     r.verdict,
			LastOverall: r.lastOverall,
			LastProbe:   timeJSON(r.lastProbe),
			NextProbe:   timeJSON(r.nextProbe),
			Cooling:     r.cooling,
			CooldownEnd: timeJSON(r.cooldownEnd),
		}
		state.Records[id] = pr
	}
	data, err := json.Marshal(&state)
	if err != nil {
		p.persistErr = fmt.Errorf("health-history does not marshal: %w", err)
		return
	}
	final := filepath.Join(p.cfg.StateDir, PersistFileName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		p.persistErr = fmt.Errorf("health-history does not write: %w", err)
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		p.persistErr = fmt.Errorf("health-history does not replace: %w", err)
		return
	}
	p.persistErr = nil
}

// loadLocked reads the state file under mu. A missing file is a fresh install, not an error; a corrupt
// one is the same thing by the strict reading — the ring is a hint for a support bundle, and refusing to
// start over it would be the tail wagging the prober.
func (p *Prober) loadLocked() {
	if p.cfg.StateDir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(p.cfg.StateDir, PersistFileName))
	if err != nil {
		return // missing or unreadable: start empty
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	for id, pr := range state.Records {
		if pr == nil {
			continue
		}
		r := &record{
			verdict:     pr.Verdict,
			lastOverall: pr.LastOverall,
			consecutive: streakFrom(pr),
			lastProbe:   timeOf(pr.LastProbe),
			nextProbe:   timeOf(pr.NextProbe),
			cooling:     pr.Cooling,
			cooldownEnd: timeOf(pr.CooldownEnd),
			entries:     pr.Entries,
		}
		// The byte cap is re-applied on the way in: a ring written by a build with a larger cap is
		// trimmed, not refused.
		for _, e := range pr.Entries {
			if data, err := json.Marshal(e); err == nil {
				r.bytes += len(data)
			}
		}
		for r.bytes > p.cfg.RingBytes && len(r.entries) > 1 {
			if dropped, err := json.Marshal(r.entries[0]); err == nil {
				r.bytes -= len(dropped)
			}
			r.entries = r.entries[1:]
		}
		p.records[id] = r
	}
	p.sequence = state.Sequence
}

// streakFrom recomputes the consecutive-failure counter from what was persisted. A `healthy` verdict
// means the streak was zero at write time; anything else carries the stored count, because the count, not
// a single timeout, drives failover (doc 22 §8 invariant 3) and a restart must not forgive a server the
// policy had already condemned.
func streakFrom(pr *persistedRecord) int {
	if pr.Verdict == VerdictHealthy {
		return 0
	}
	if pr.Entries == nil {
		return 0
	}
	last := pr.Entries[len(pr.Entries)-1]
	if last.Failures > 0 {
		return last.Failures
	}
	return 0
}

func timeJSON(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(timeJSONFormat)
}

// timeOf reads back what timeJSON wrote; the zero time is the absent value every field uses.
func timeOf(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(timeJSONFormat, s)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
