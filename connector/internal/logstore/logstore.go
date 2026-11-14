// Package logstore is the Connector's half of docs/architecture/36-observability.md: the record shape,
// the value-keyed masking pass that runs at the sink (docs/architecture/32-secrets-keystore-masking.md §8),
// and the two connector-plane sinks — the memory ring and the rotated log file — with the three caps
// (age, count, bytes) the retention contract states (§10). The record is one flat JSON object per line in
// every sink: a nested object is where a secret hides from a field-name policy, so there is none.
//
// What this package deliberately does not do: decide what is worth logging (components append records;
// sinks decide what leaves the process), read the Core's own log (that sink is a bounded tail owned by the
// supervisor, not by this package), or reach the browser plane (browser-ring is IndexedDB, extension-side).
package logstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Record is the flat shape of observability.json#/record/fields. Optional fields are pointers so a record
// never carries a zero that means nothing: seq is assigned per sink at append, and a record about no call
// carries no method.
type Record struct {
	Ts        string `json:"ts"`                // RFC 3339 with milliseconds, sender clock; never used for ordering
	Level     string `json:"level"`             // error | warn | info | debug
	Plane     string `json:"plane"`             // extension | connector | core
	Component string `json:"component"`         // one of doc 22 §0's components, ≤40 chars
	Seq       int64  `json:"seq,omitempty"`     // monotonic per sink per plane; the gap detector
	TraceID   string `json:"traceId,omitempty"` // events.schema.json#/$defs/traceId
	Event     string `json:"event,omitempty"`   // events[].name
	Method    string `json:"method,omitempty"`  // protocol/command-allowlist.json — never a free string
	Code      string `json:"code,omitempty"`    // protocol/error-taxonomy.json#/codes — never a rendered message
	DurMs     *int64 `json:"durMs,omitempty"`   // 0..600000
	Msg       string `json:"msg,omitempty"`     // one sentence
}

// Masker is the value-keyed masking pass. The table is built from the values the keystore currently holds,
// so it cannot go stale the way a name list does and needs no cooperation from the code producing the text.
// A value that equals or sits inside a rendered string is replaced whole with the mask — the legacy
// replacer's fatal gap was passing bare strings through, and there is no such gap here.
type Masker struct {
	mask   string
	values []string
}

// NewMasker builds the table. Empty and short values are refused: masking every occurrence of a one-byte
// value would redact the log into noise, and the keystore does not hold empty secrets.
func NewMasker(mask string, values []string) (*Masker, error) {
	if mask == "" {
		return nil, fmt.Errorf("logstore: the mask is empty")
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if len(v) < 8 {
			return nil, fmt.Errorf("logstore: refusing to mask a %d-byte value — too short to be a secret", len(v))
		}
		out = append(out, v)
	}
	// Longest first, so overlapping values mask deterministically.
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return &Masker{mask: mask, values: out}, nil
}

// Apply replaces every occurrence of every tabled value with the mask.
func (m *Masker) Apply(text string) string {
	for _, v := range m.values {
		text = strings.ReplaceAll(text, v, m.mask)
	}
	return text
}

// Sink is one place a record leaves the process. Append receives the record and its masked rendering;
// the sink assigns its own seq, which is what makes seq monotonic per sink per plane.
type Sink interface {
	Name() string
	Append(r Record, line string) error
}

// Store is one plane's log: one render, every sink. A record is written to each sink from the same
// object, so two sinks can never disagree about what happened (§8 rule 1).
type Store struct {
	mu       sync.Mutex
	sinks    []Sink
	plane    string
	sequence int64
}

// NewStore opens one plane's store over the given sinks. The plane names the record's `plane` field and
// is also the seq scope.
func NewStore(plane string, sinks ...Sink) *Store {
	return &Store{sinks: sinks, plane: plane}
}

// Append renders once, masks once, and hands the same masked line to every sink. The per-sink seq is
// stamped from the store's monotonic counter before rendering: seq is monotonic per sink per plane, and
// every sink of one plane sees the same sequence, so a gap in one sink is a gap in all of them.
func (s *Store) Append(rec Record) error {
	if rec.Ts == "" {
		rec.Ts = time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	rec.Plane = s.plane
	s.sequence++
	rec.Seq = s.sequence
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("logstore: record %d does not render: %w", rec.Seq, err)
	}
	for _, sink := range s.sinks {
		if err := sink.Append(rec, string(line)); err != nil {
			return fmt.Errorf("logstore: sink %s refused record %d: %w", sink.Name(), rec.Seq, err)
		}
	}
	return nil
}

// LastSeq is the seq the next record will carry. A reader comparing a sink's newest seq against this
// knows whether it is missing the tail; comparing its oldest against 1 knows whether the head evicted.
func (s *Store) LastSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sequence
}

// Ring is the connector-ring sink: memory, capped by count and bytes, the first cap reached wins
// (docs/architecture/36-observability.md §10). The shipped caps are 2000 records and 1 MiB; tests
// configure smaller ones through the same fields rather than through a second code path.
type Ring struct {
	mu         sync.Mutex
	name       string
	mask       *Masker
	maxRecords int
	maxBytes   int
	records    []ringEntry
	bytes      int
}

type ringEntry struct {
	rec   Record
	bytes int
}

// NewRing opens a ring sink. Count and byte caps both apply; a record that alone exceeds the byte cap is
// still kept — one record is one event and evicting the only witness to evict the bound is the legacy
// mistake — but it evicts everything behind it.
func NewRing(name string, mask *Masker, maxRecords, maxBytes int) *Ring {
	return &Ring{name: name, mask: mask, maxRecords: maxRecords, maxBytes: maxBytes}
}

func (r *Ring) Name() string { return r.name }

func (r *Ring) Append(rec Record, line string) error {
	masked := line
	stored := rec
	if r.mask != nil {
		masked = r.mask.Apply(line)
		// Masking at append means the ring never holds the raw value anywhere — not in its byte
		// accounting and not in the record it keeps. The masked line is the truth every sink of this
		// store rendered, so the record is re-read from it rather than masked field by field.
		var from Record
		if err := json.Unmarshal([]byte(masked), &from); err == nil {
			stored = from
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(masked)
	r.records = append(r.records, ringEntry{rec: stored, bytes: n})
	r.bytes += n
	for len(r.records) > 1 && (len(r.records) > r.maxRecords || r.bytes > r.maxBytes) {
		r.bytes -= r.records[0].bytes
		r.records = r.records[1:]
	}
	return nil
}

// Records returns a copy of the surviving records, oldest first. The first one's seq is the gap
// detector: a reader that saw seq 1..k earlier and now reads a ring starting above k knows records
// evicted; a reader whose high-water mark is below the ring's newest seq minus its length knows
// records were lost some other way.
func (r *Ring) Records() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, len(r.records))
	for i, e := range r.records {
		out[i] = e.rec
	}
	return out
}

// FileSink is the connector-log-file sink: disk, durable, 5 MiB × 5 files rotated oldest-first, masked
// before the write in the same render pass that produced the ring copy. RetentionDays 0 means the
// current session only: truncated at start rather than appended (docs/architecture/36 §10).
type FileSink struct {
	mu            sync.Mutex
	name          string
	mask          *Masker
	dir           string
	base          string
	bytesPerFile  int
	maxFiles      int
	retentionDays int
	file          *os.File
	written       int64
	started       time.Time
}

// NewFileSink opens the file sink. Files are named <base>.0 .. <base>.<maxFiles-1>, oldest first;
// rotation renames the set and starts a fresh .0. retentionDays 0 truncates any existing set at start.
func NewFileSink(name, dir, base string, mask *Masker, bytesPerFile, maxFiles, retentionDays int) (*FileSink, error) {
	if bytesPerFile <= 0 || maxFiles <= 0 {
		return nil, fmt.Errorf("logstore: file sink caps must be positive")
	}
	f := &FileSink{
		name: name, mask: mask, dir: dir, base: base,
		bytesPerFile: bytesPerFile, maxFiles: maxFiles, retentionDays: retentionDays,
		started: time.Now(),
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("logstore: %w", err)
	}
	if retentionDays == 0 {
		// The current session only: truncate at start, never append. A zero that silently meant the
		// default would be the second worst reading of a privacy control whose purpose is no history.
		for i := 0; i < maxFiles; i++ {
			if err := os.Truncate(f.path(i), 0); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("logstore: %w", err)
			}
		}
	}
	if err := f.open(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *FileSink) path(i int) string { return filepath.Join(f.dir, fmt.Sprintf("%s.%d", f.base, i)) }

func (f *FileSink) open() error {
	file, err := os.OpenFile(f.path(0), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("logstore: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("logstore: %w", err)
	}
	f.file, f.written = file, info.Size()
	return nil
}

func (f *FileSink) Name() string { return f.name }

func (f *FileSink) Append(rec Record, line string) error {
	masked := line
	if f.mask != nil {
		masked = f.mask.Apply(line)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Age is the third cap, and the first reached wins. Ageing is checked on append, not on a timer:
	// a quiet session rotates nothing, and a rotation with nothing to write is not a rotation.
	if f.retentionDays > 0 && time.Since(f.started) > time.Duration(f.retentionDays)*24*time.Hour {
		if err := f.rotateLocked(); err != nil {
			return err
		}
	}
	if f.written+int64(len(masked))+1 > int64(f.bytesPerFile) {
		if err := f.rotateLocked(); err != nil {
			return err
		}
	}
	if _, err := f.file.WriteString(masked + "\n"); err != nil {
		return fmt.Errorf("logstore: sink %s: %w", f.name, err)
	}
	f.written += int64(len(masked)) + 1
	return nil
}

// rotateLocked shifts <base>.N-1 → <base>.N and starts a fresh <base>.0; the oldest file falls off the
// end and is gone. Rotation runs before the write and the evicted set is simply removed — nothing is
// summarised or rolled up, and what was removed is reported by the files that remain.
func (f *FileSink) rotateLocked() error {
	if err := f.file.Close(); err != nil {
		return fmt.Errorf("logstore: sink %s: %w", f.name, err)
	}
	f.file = nil
	for i := f.maxFiles - 1; i > 0; i-- {
		if _, err := os.Stat(f.path(i - 1)); err == nil {
			_ = os.Remove(f.path(i))
			if err := os.Rename(f.path(i-1), f.path(i)); err != nil {
				return fmt.Errorf("logstore: sink %s: %w", f.name, err)
			}
		} else {
			_ = os.Remove(f.path(i))
		}
	}
	f.started = time.Now()
	return f.open()
}

// Close flushes nothing (writes are unbuffered) and releases the file handle.
func (f *FileSink) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}

// The shipped connector-ring caps: observability.json#/sinks' connector-ring row (2000 records, 1 MiB).
// Tests configure smaller caps through NewRing's own arguments, never through these.
const (
	DefaultRingRecords = 2000
	DefaultRingBytes   = 1048576
)
