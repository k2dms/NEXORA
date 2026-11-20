package main

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"

	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

// configSnapshot is doc 26 §6's drift-detection primitive: the extension holds the hash it last applied and
// asks whether reality matches. The hash that survives is the applied-state record's `appliedConfigHash` —
// the same quantity `core.status` reports when Core runs and the same one a restart reads back — because the
// file on disk can only give a hash of materialized bytes and ODEC-36 makes the handle-bearing document the
// configHash subject.
type configSnapshot struct {
	stateDir string
}

func newConfigSnapshot(stateDir string) (*configSnapshot, error) {
	if stateDir == "" {
		return nil, errors.New("config.snapshot: no state directory; the applied-state record is the only answer to \"what is applied\" that survives a restart, and doc 34 §7 puts it there")
	}
	return &configSnapshot{stateDir: stateDir}, nil
}

type configSnapshotAnswer struct {
	AppliedConfigHash    string   `json:"appliedConfigHash"`
	CompiledAt           *string  `json:"compiledAt"`
	MatchesExtensionHash *bool    `json:"matchesExtensionHash"`
	Drift                bool     `json:"drift"`
	ServerTags           []string `json:"serverTags"`
	MaskedConfig         *any     `json:"maskedConfig"`
	Truncated            bool     `json:"truncated"`
}

func (s *configSnapshot) Handle(_ context.Context, f *envelope.Frame) (any, *dispatch.Refusal) {
	args, bad := readSnapshotArgs(f)
	if bad != nil {
		return nil, bad
	}
	// Masked-config rendering is the sink half of doc 32: masking every value the keystore holds needs the
	// store's value enumeration, which the store does not expose. Refusing the whole answer keeps the command
	// atomic — a caller asking for the masked view gets no hash either, rather than a hash it may act on
	// alongside a view that was never rendered.
	if args.includeMaskedConfig {
		return nil, &dispatch.Refusal{Code: "SecretRedacted", Message: clip("config.snapshot: includeMaskedConfig is refused until the keystore exposes the value enumeration the masking sink needs (doc 32's sink half, VER-45's register row); ask without it for the drift answer", maxApplyMessage)}
	}
	st, err := supervisor.ReadState(s.stateDir)
	if err != nil {
		return nil, &dispatch.Refusal{Code: "Internal", Message: clip("config.snapshot: "+err.Error(), maxApplyMessage)}
	}
	if st.AppliedHash == "" {
		// A record with no hash is the first apply's empty reading, not a corrupt file: nothing is applied, so
		// a drift answer would be an answer about a config that does not exist.
		return nil, &dispatch.Refusal{Code: "CoreNotRunning", Message: clip("config.snapshot: no applied config is on record, so there is no reality to compare an expectation against; apply one first", maxApplyMessage)}
	}
	ans := configSnapshotAnswer{
		AppliedConfigHash: st.AppliedHash,
		ServerTags:        []string{},
	}
	// Held is doc 27 §4's reservation table keyed by tag, and the tags of the applied config are its keys.
	tags := make([]string, 0, len(st.Held))
	for tag := range st.Held {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	ans.ServerTags = tags
	if args.expectation != "" {
		matches := st.AppliedHash == args.expectation
		ans.MatchesExtensionHash = &matches
		ans.Drift = !matches
	}
	return ans, nil
}

type snapshotArgs struct {
	expectation         string
	includeMaskedConfig bool
}

func readSnapshotArgs(f *envelope.Frame) (*snapshotArgs, *dispatch.Refusal) {
	args := &snapshotArgs{}
	if len(f.Args) == 0 {
		return args, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &raw); err != nil {
		return nil, badSnapshotArgs("args are not a JSON object")
	}
	for k, v := range raw {
		switch k {
		case "expectation":
			if err := json.Unmarshal(v, &args.expectation); err != nil {
				return nil, badSnapshotArgs("expectation is not a string")
			}
			if !hash64.MatchString(args.expectation) {
				return nil, badSnapshotArgs("expectation is not a lowercase sha256 the answer's own appliedConfigHash pattern admits")
			}
		case "includeMaskedConfig":
			if err := json.Unmarshal(v, &args.includeMaskedConfig); err != nil {
				return nil, badSnapshotArgs("includeMaskedConfig is not a boolean")
			}
		case "maxBytes":
			// Accepted and unenforced: nothing this handler renders is sized by it while maskedConfig is
			// refused, and the range the schema declares is checked by every caller's own validation.
			var n int
			if err := json.Unmarshal(v, &n); err != nil {
				return nil, badSnapshotArgs("maxBytes is not an integer")
			}
		default:
			return nil, badSnapshotArgs("args carry a field config.snapshot does not declare; its args object is closed")
		}
	}
	return args, nil
}

func badSnapshotArgs(rule string) *dispatch.Refusal {
	return &dispatch.Refusal{Code: "Internal", Message: clip("config.snapshot: "+rule, maxApplyMessage)}
}

// hash64 is the lowercase-sha256 shape appliedConfigHash and expectation both carry; one pattern here so a
// widened answer cannot widen the expectation silently.
var hash64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
