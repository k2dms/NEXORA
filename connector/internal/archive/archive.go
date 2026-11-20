// Package archive is doc/architecture/33-binary-integrity.md §5 step 7: the extraction guard that runs
// before anything is written, because a zip is an attacker-controlled directory listing.
//
// The row VER-43 asked for has a shape the corpus pins exactly: enumerate the archive, refuse entries with
// absolute paths, `..`, or names outside the expected set, and do all of it before the first write. The
// expected set comes from the pin — the same document steps 1–6 already trusted — so the caller hands it in
// and this package trusts nothing about the entries themselves, not even that their names are printable.
//
// The extractor itself is step 8's business and lives with the installer that has a pin and a download
// (VER-41's register row); this package is the gate that installer calls, kept pure so every rule below is
// testable over attacker-shaped bytes without a network or a real release.
package archive

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// Entry is one archive member's declared name and size, exactly what the zip central directory reports.
// The name is never cleaned up here — cleaning is how Zip-Slip wins.
type Entry struct {
	Name string
	Size int64
}

// Limits are the size bounds doc 33 §5 puts before any write. A caller with a pin has `sizeBytes` for the
// whole archive; the per-entry bound is the same idea applied to a member, and MaxTotal is the extracted
// tree's ceiling — step 2's "free space against sizeBytes plus the extracted tree", which a claim of
// uncompressed size inside the archive cannot be trusted to state honestly.
type Limits struct {
	MaxEntry int64
	MaxTotal int64
}

// Refusal is one rejected entry. The zip format forbids duplicate names, but a crafted archive can carry
// them, and the message names the archive's own bytes rather than a cleaned-up path.
type Refusal struct {
	Entry string
	Rule  string
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("archive: entry %q refused before any write: %s", r.Entry, r.Rule)
}

// Sentinel refusals the caller's error taxonomy distinguishes. Doc 33 §5 step 7's failure column says
// IntegrityMismatch for the whole step, so the caller maps any *Refusal there; the sentinels exist for the
// tests and the log line, not for a wider code surface.
var ErrOutsideExpectedSet = errors.New("outside the pin's expected set")

// Validate is step 7 whole: every entry is refused or accepted before the caller writes a single byte.
//
// The rules run in the order the failure column reads: shape first (a name that cannot be a safe relative
// path is refused regardless of what the pin says, because the pin's expected set is enumerated from the
// release listing and a pin entry matching `..\x.exe` is itself the compromise), then the expected set, then
// the bounds. A name the set does not contain is refused even when it looks harmless — the set is the whole
// truth about what a legitimate release carries.
func Validate(expected []string, entries []Entry, limits Limits) error {
	set := make(map[string]bool, len(expected))
	for _, name := range expected {
		set[name] = true
	}
	seen := make(map[string]bool, len(entries))
	var total int64
	for _, e := range entries {
		if rule := badShape(e.Name); rule != "" {
			return &Refusal{Entry: e.Name, Rule: rule}
		}
		if seen[e.Name] {
			return &Refusal{Entry: e.Name, Rule: "the archive declares this name twice; which of them would the extractor write?"}
		}
		seen[e.Name] = true
		if !set[e.Name] {
			return &Refusal{Entry: e.Name, Rule: ErrOutsideExpectedSet.Error()}
		}
		if limits.MaxEntry > 0 && e.Size > limits.MaxEntry {
			return &Refusal{Entry: e.Name, Rule: fmt.Sprintf("declares %d bytes, past the %d one member may carry", e.Size, limits.MaxEntry)}
		}
		total += e.Size
		if limits.MaxTotal > 0 && total > limits.MaxTotal {
			return &Refusal{Entry: e.Name, Rule: fmt.Sprintf("the archive declares %d bytes in total, past the %d bound", total, limits.MaxTotal)}
		}
	}
	return nil
}

// badShape answers the rules step 7 names before the set is consulted. Windows path separators are refused
// on every platform: an archive is built once and extracted wherever the pin says, and a name carrying `\`
// is never a legitimate Core release member. A trailing slash is a directory entry and stays legal — it is
// validated with the slash stripped, so "dir/../" is still a climb.
func badShape(name string) string {
	if name == "" {
		return "an entry with no name is an entry this guard cannot reason about"
	}
	if strings.ContainsRune(name, '\\') {
		return "carries a backslash; the archive is extracted as a relative path tree, and a Windows separator is not a name"
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "~") {
		return "is an absolute path"
	}
	if len(name) >= 2 && name[1] == ':' {
		return "carries a drive letter"
	}
	trimmed := strings.TrimSuffix(name, "/")
	if strings.Contains(trimmed, "../") || strings.Contains(trimmed, "/..") || trimmed == ".." {
		return "climbs out of the extraction directory"
	}
	if clean := path.Clean(trimmed); clean != trimmed {
		return "is not a clean relative path (" + clean + ")"
	}
	return ""
}
