package main

// This file is `config.apply` as one path. docs/architecture/22-component-contracts.md §0 puts the
// ConnectorService's command surface in this package, and the three things it joins are already tested as
// neighbours rather than as a route: internal/compile (doc 27 §2's stages 1 to 6), internal/apply (§8's
// materialize-then-rename) and internal/supervisor (the process that loads the file). A frame arrives carrying
// the intent documents and naming the profiles to activate; a compiled document, a config file and, if the hash
// moved, a different Core are what leaves.
//
// Two decisions the corpus has settled are stated here rather than re-argued:
//
//   - the intent documents arrive in the frame (`config.apply.json#/args/properties/intent`, the `intent-down`
//     movement's own carrier), and this file never opens a store of them: the Connector keeps no copy of user
//     intent (ADR-0010), so an ids-only call is a `BadRequest` and not a lookup;
//   - the two directories and the stable file name are doc 34 §7's per-user layout, and the only byte-string
//     question left is which hash `configHash` reports — ODEC-36, so the span is a required wiring knob and
//     newConfigApply refuses to build a surface that has not been told.
//
// What it decides itself, with the evidence next to each row: the order of the checks, the taxonomy code every
// refusal answers with, and the fact that a failure after the rename is reported rather than rolled back.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/k2dms/nexora/connector/internal/apply"
	"github.com/k2dms/nexora/connector/internal/compile"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/keystore"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

// Intent is the desired state the frame carries (`config.apply.json#/args/properties/intent`). The five fields
// are the documents stage 1 resolves, decoded by hand against the closed field lists the schemas declare.
//
// Held is deliberately absent: doc 27 §4's "Persistence" row puts the reservation table in the Connector, and the
// Connector's own copy of it is the applied-state record this command reads and rewrites — not a document the
// caller could carry, and not a store this package opens anywhere else.
type Intent struct {
	Profiles []compile.Profile      `json:"profiles"`
	Servers  []compile.Server       `json:"servers"`
	Routing  *compile.RoutingPolicy `json:"routingPolicy,omitempty"`
	Settings compile.Settings       `json:"settings"`
	Failure  compile.Failure        `json:"failurePolicy"`
}

// stateRecord is the applied-state record as this command needs it: read for the compare-and-swap and the port
// table the last apply took, written once per apply that took ports. It is a seam because it is file I/O — the
// same reason coreProcess is one — and its one production implementation is the StateDir of doc 34 §7's layout.
type stateRecord interface {
	Read() (supervisor.State, error)
	Write(supervisor.State) error
}

// fileStateRecord is that one production implementation: the applied-state file in the state directory, the same
// temp-and-rename discipline internal/supervisor writes it with.
type fileStateRecord struct{ dir string }

func (r fileStateRecord) Read() (supervisor.State, error) { return supervisor.ReadState(r.dir) }

func (r fileStateRecord) Write(st supervisor.State) error { return supervisor.WriteState(r.dir, st) }

// coreProcess is internal/supervisor.Supervisor's surface as this command needs it. *supervisor.Supervisor
// satisfies it; the seam exists so the path can be driven with a process that is not a process, the way the
// supervisor's own tests drive it with one that is.
type coreProcess interface {
	Start(ctx context.Context, p supervisor.StartParams) (*supervisor.Status, error)
	Stop(ctx context.Context, graceful bool) (*supervisor.StopResult, error)
	Status() supervisor.Status
}

// hashSpan is ODEC-36 held as a switch instead of being settled by a default. The zero value is refused by
// newConfigApply, so a build that has not read the register cannot answer `config.apply` at all.
type hashSpan int

const (
	hashUnknown hashSpan = iota

	// hashHandles reports the compiler's own output — the document with `keystore://…` in every credential
	// position, the identity the drift check and `unchangedBecauseHashMatched` want, and a hash of bytes that
	// never reach disk.
	hashHandles

	// hashValues reports the materialized file Core loads, so a credential rotation moves the hash and the
	// preview of a change has to read the keystore to say what the hash would be.
	hashValues
)

func (h hashSpan) String() string {
	switch h {
	case hashHandles:
		return "handles"
	case hashValues:
		return "values"
	default:
		return "unset"
	}
}

// applyConfig is the wiring this process cannot measure for itself: the two directories of doc 34 §7's per-user
// layout, and the span ODEC-36 owns.
type applyConfig struct {
	// StateDir holds the applied-state record internal/supervisor keeps. Its `appliedConfigHash` is the only
	// answer to "what is applied" that survives in either span, because the file on disk can only ever give a
	// hash of materialized bytes.
	StateDir string
	// ConfigDir holds the compiled config internal/apply writes: doc 34 §7's per-user config directory, whose
	// one stable file name is what makes doc 27 §8's replacement a rename and not a create.
	ConfigDir string
	Span      hashSpan
}

// configApply is the handler: the seams, the directories, and the span.
type configApply struct {
	record   stateRecord
	resolver apply.Resolver
	core     coreProcess
	cfg      applyConfig
}

func newConfigApply(cfg applyConfig, record stateRecord, resolver apply.Resolver, core coreProcess) (*configApply, error) {
	switch {
	case cfg.StateDir == "":
		return nil, errors.New("config.apply: no state directory; the applied hash this command compares against is a record, not a memory")
	case cfg.ConfigDir == "":
		return nil, errors.New("config.apply: no config directory; internal/apply refuses to guess where the file Core loads goes")
	case cfg.Span == hashUnknown:
		return nil, fmt.Errorf("config.apply: no configHash byte span; ODEC-36 is still open, and a surface that picked one would be picking it in the wrong place — the register is docs/architecture/40-open-decisions.md")
	case record == nil:
		return nil, errors.New("config.apply: no applied-state record; the compare-and-swap and the port table have nowhere to rest")
	case resolver == nil:
		return nil, errors.New("config.apply: no keystore resolver; a credential position would be written as a handle string")
	case core == nil:
		return nil, errors.New("config.apply: no Core process owner; restartPolicy auto has nothing to restart")
	}
	return &configApply{record: record, resolver: resolver, core: core, cfg: cfg}, nil
}

// reApplyID is the UUID floor both referenced schemas state: `connector-profile.schema.json#/properties/id` for
// profileIds and `routing-policy.schema.json#/$defs/uuid` for routingPolicyId. Copied rather than read at runtime
// because the Connector ships as one binary, the same reason internal/hello copies its version patterns.
var reApplyID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// reAppliedHash is `config.apply.json#/args/properties/expectedAppliedHash`'s pattern, which allows the empty
// string as well as a digest: empty is "I believe nothing is applied", and a caller that had to invent a sentinel
// for that would be inventing a value the pattern rejects.
var reAppliedHash = regexp.MustCompile(`^([0-9a-f]{64})?$`)

// applyArgs is the decoded form of `packages/schema/protocol/commands/config.apply.json#/args`, whose fields are
// the selection (`profileIds`), the documents (`intent`) and the switches. restartPolicy keeps the wire's own
// spelling because it reaches the answer's `restarted` field and a reader comparing the two should see the same
// word.
type applyArgs struct {
	profileIDs          []string
	intent              Intent
	dryRun              bool
	expectedAppliedHash string
	restartPolicy       string
}

const (
	restartAuto  = "auto"
	restartNever = "never"
)

// readApplyArgs is this command's side of internal/hello's convention: decode by hand against the closed field
// list, because `additionalProperties:false` is stated by the command doc and a payload carrying an undeclared
// name is a caller that believes it asked for something.
//
// The code is `BadRequest`, whose registered meaning is "Frame is well-formed JSON but violates the call contract
// (missing args, wrong shape)". `SchemaMismatch` is the other code the command declares and is not used here: its
// meaning is a *version* the receiver does not implement, which connector.hello settled before dispatch reached
// this handler.
func readApplyArgs(f *envelope.Frame) (applyArgs, *dispatch.Refusal) {
	if len(f.Args) == 0 {
		return applyArgs{}, badApplyArgs("args are required: profileIds")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &raw); err != nil {
		return applyArgs{}, badApplyArgs("args are not a JSON object")
	}
	for k := range raw {
		switch k {
		case "profileIds", "intent", "dryRun", "expectedAppliedHash", "restartPolicy":
		default:
			return applyArgs{}, badApplyArgs("args carry a field config.apply does not declare")
		}
	}
	args := applyArgs{restartPolicy: restartAuto}

	ids, why := requiredIDs(raw, "profileIds")
	if why != "" {
		return applyArgs{}, badApplyArgs(why)
	}
	args.profileIDs = ids

	if v, ok := raw["intent"]; !ok {
		return applyArgs{}, badApplyArgs("intent is required: the documents this apply compiles ride the frame, because the Connector keeps no copy of user intent (ADR-0010)")
	} else {
		intent, bad := readIntent(v)
		if bad != nil {
			return applyArgs{}, bad
		}
		args.intent = *intent
	}
	if d, ok := raw["dryRun"]; ok {
		if err := json.Unmarshal(d, &args.dryRun); err != nil {
			return applyArgs{}, badApplyArgs("dryRun is not a boolean")
		}
	}
	if h, ok := raw["expectedAppliedHash"]; ok {
		var s *string
		if err := json.Unmarshal(h, &s); err != nil {
			return applyArgs{}, badApplyArgs("expectedAppliedHash is not a string or null")
		}
		if s != nil {
			if !reAppliedHash.MatchString(*s) {
				return applyArgs{}, badApplyArgs("expectedAppliedHash is neither a lowercase sha256 nor the empty string the pattern allows")
			}
			args.expectedAppliedHash = *s
		}
	}
	if p, ok := raw["restartPolicy"]; ok {
		var s string
		if err := json.Unmarshal(p, &s); err != nil {
			return applyArgs{}, badApplyArgs("restartPolicy is not a string")
		}
		if s != restartAuto && s != restartNever {
			return applyArgs{}, badApplyArgs("restartPolicy is outside the enum auto|never")
		}
		args.restartPolicy = s
	}
	return args, nil
}

// requiredIDs reads an array of the command's UUID ids: minItems 1, maxItems 64, uniqueItems true. The 64 is the
// same number doc 27 §4's inbound ceiling carries, so an args refusal here is the cheap version of the refusal
// the compiler would otherwise raise as QuotaExceeded after building every outbound.
func requiredIDs(raw map[string]json.RawMessage, key string) ([]string, string) {
	v, ok := raw[key]
	if !ok {
		return nil, key + " is required"
	}
	var list []string
	if err := json.Unmarshal(v, &list); err != nil {
		return nil, key + " is not an array of strings"
	}
	if len(list) == 0 || len(list) > 64 {
		return nil, fmt.Sprintf("%s must hold between 1 and 64 ids, and it holds %d", key, len(list))
	}
	seen := make(map[string]bool, len(list))
	for _, id := range list {
		if !reApplyID.MatchString(id) {
			return nil, key + " carries an id that is not the lowercase UUID v4 the schema declares"
		}
		if seen[id] {
			return nil, key + " carries the same id twice"
		}
		seen[id] = true
	}
	return list, ""
}

func badApplyArgs(rule string) *dispatch.Refusal {
	return &dispatch.Refusal{Code: "BadRequest", Message: "config.apply " + rule}
}

// The bounds `config.apply.json#/args/properties/intent` declares, mirrored as constants because the Connector
// ships as one binary: 64 profiles is the same ceiling requiredIDs reads, 4096 servers is `maxServers` (doc 29
// §8's caps table), and both are refusals here that the compiler would otherwise raise as `QuotaExceeded` after
// building every outbound.
const (
	maxIntentProfiles = 64
	maxIntentServers  = 4096
)

// readIntent decodes `args.intent` document by document with the same closed-object rule every entity schema
// states: a payload carrying a name the schema does not declare is a caller that believes it asked for something.
// The bounds are checked here so a frame that is two documents over the ceiling is refused before the pipeline
// builds anything.
func readIntent(raw json.RawMessage) (*Intent, *dispatch.Refusal) {
	var doc struct {
		Profiles      []json.RawMessage          `json:"profiles"`
		Servers       []json.RawMessage          `json:"servers"`
		RoutingPolicy *json.RawMessage           `json:"routingPolicy"`
		Settings      *json.RawMessage           `json:"settings"`
		FailurePolicy *json.RawMessage           `json:"failurePolicy"`
		Extra         map[string]json.RawMessage `json:"-"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, badApplyArgs("intent is not the object the args schema declares: " + err.Error())
	}
	out := &Intent{}
	if len(doc.Profiles) == 0 || len(doc.Profiles) > maxIntentProfiles {
		return nil, badApplyArgs(fmt.Sprintf("intent.profiles must hold between 1 and %d documents, and it holds %d", maxIntentProfiles, len(doc.Profiles)))
	}
	out.Profiles = make([]compile.Profile, 0, len(doc.Profiles))
	for i, p := range doc.Profiles {
		profile, err := compile.Decode[compile.Profile](p)
		if err != nil {
			return nil, badApplyArgs(fmt.Sprintf("intent.profiles[%d] is not a profile the schema declares: %v", i, trimCause(err.Error())))
		}
		out.Profiles = append(out.Profiles, profile)
	}
	if len(doc.Servers) > maxIntentServers {
		return nil, badApplyArgs(fmt.Sprintf("intent.servers must hold at most %d documents, and it holds %d", maxIntentServers, len(doc.Servers)))
	}
	out.Servers = make([]compile.Server, 0, len(doc.Servers))
	for i, s := range doc.Servers {
		server, err := compile.Decode[compile.Server](s)
		if err != nil {
			return nil, badApplyArgs(fmt.Sprintf("intent.servers[%d] is not a server the schema declares: %v", i, trimCause(err.Error())))
		}
		out.Servers = append(out.Servers, server)
	}
	if doc.RoutingPolicy != nil {
		policy, err := compile.Decode[compile.RoutingPolicy](*doc.RoutingPolicy)
		if err != nil {
			return nil, badApplyArgs("intent.routingPolicy is not a routing policy the schema declares: " + trimCause(err.Error()))
		}
		out.Routing = &policy
	}
	if doc.Settings == nil {
		return nil, badApplyArgs("intent.settings is required: the ports and telemetry the compiler reads are the caller's documents, not this build's defaults")
	}
	settings, err := compile.Decode[compile.Settings](*doc.Settings)
	if err != nil {
		return nil, badApplyArgs("intent.settings is not a settings document the schema declares: " + trimCause(err.Error()))
	}
	out.Settings = settings
	if doc.FailurePolicy == nil {
		return nil, badApplyArgs("intent.failurePolicy is required: what happens when Core dies is the caller's document, not this build's default")
	}
	failure, err := compile.Decode[compile.Failure](*doc.FailurePolicy)
	if err != nil {
		return nil, badApplyArgs("intent.failurePolicy is not a failure policy the schema declares: " + trimCause(err.Error()))
	}
	out.Failure = failure
	return out, nil
}

// trimCause clips a Decode error to the json package's own last clause, which names the field at fault; the
// wrapper's boilerplate is the package prefix a refusal message does not need twice.
func trimCause(s string) string {
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}

// selectProfiles applies the frame's own selection over the documents it carried: the profiles named by
// `profileIds`, in that order. Both directions of the rule the args description states are refusals here — a
// named id with no document, and a document no id names — because either one means the frame's selection and the
// frame's documents disagree about what is being applied.
func selectProfiles(docs []compile.Profile, ids []string) ([]compile.Profile, *dispatch.Refusal) {
	byID := make(map[string]compile.Profile, len(docs))
	for _, p := range docs {
		if _, dup := byID[p.ID]; dup {
			return nil, badApplyArgs("intent carries two profile documents under the id " + p.ID)
		}
		byID[p.ID] = p
	}
	for _, id := range ids {
		if _, ok := byID[id]; !ok {
			return nil, badApplyArgs("profileIds names " + id + ", which the intent documents do not carry: an id without a document is not a dangling reference the Connector resolves")
		}
	}
	named := make(map[string]bool, len(ids))
	for _, id := range ids {
		named[id] = true
	}
	for _, p := range docs {
		if !named[p.ID] {
			return nil, badApplyArgs("intent carries the profile document " + p.ID + ", which no profileIds entry names: the selection and the documents must agree about what is applied")
		}
	}
	out := make([]compile.Profile, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, nil
}

// applyAnswer is `config.apply.json#/result` in Go. The three required fields are always present, and the six
// optional ones are omitted rather than emitted empty: a dryRun has no restart to report, an apply with nothing
// recorded has no previous hash, and an empty `unrepresentable` array would say "there were rules we could not
// compile, and here is the list of them" when the list is the absence.
type applyAnswer struct {
	Applied                     bool              `json:"applied"`
	DryRun                      bool              `json:"dryRun"`
	ConfigHash                  string            `json:"configHash"`
	PreviousConfigHash          string            `json:"previousConfigHash,omitempty"`
	Restarted                   bool              `json:"restarted"`
	UnchangedBecauseHashMatched bool              `json:"unchangedBecauseHashMatched,omitempty"`
	InboundPorts                []compile.Inbound `json:"inboundPorts,omitempty"`
	ServerTags                  []string          `json:"serverTags,omitempty"`
	Unrepresentable             []compile.Report  `json:"unrepresentable,omitempty"`
}

// Handle is the whole path, in the order the contract gives it.
//
// Compare-and-swap first (doc 26 §2.1: a mismatch is Busy, not a silent overwrite), then the intent, then the
// compile, then — and only then — the file, then the process. A refusal at any of those points has touched
// nothing after it, which is the property doc 27 §8's atomicity rule is really about.
func (a *configApply) Handle(ctx context.Context, f *envelope.Frame) (any, *dispatch.Refusal) {
	args, bad := readApplyArgs(f)
	if bad != nil {
		return nil, bad
	}

	recorded, err := a.record.Read()
	if err != nil {
		return nil, refuseApply(err, "config.apply: the applied-state record could not be read, so the hash this call would compare and report is unknown")
	}
	// The field's own schema (`config.apply.json#/args/properties/expectedAppliedHash`) is `type: [string, null]`,
	// which is the contract saying a caller may state no belief at all. A stated belief that disagrees with the
	// record is Busy; an unstated one is not a claim about an empty record, it is the absence of a precondition —
	// otherwise every second apply would have to read `core.status` first just to be allowed to speak.
	if args.expectedAppliedHash != "" && args.expectedAppliedHash != recorded.AppliedHash {
		return nil, &dispatch.Refusal{
			Code:    "Busy",
			Message: "config.apply: the config on record is not the one the caller believes is applied; ask again or read core.status, because two callers must not race a config",
		}
	}

	selected, bad := selectProfiles(args.intent.Profiles, args.profileIDs)
	if bad != nil {
		return nil, bad
	}
	res, err := compile.Compile(compile.Input{
		Profiles: selected,
		Servers:  args.intent.Servers,
		Routing:  args.intent.Routing,
		Settings: args.intent.Settings,
		Failure:  args.intent.Failure,
		Held:     recorded.Held,
	})
	if err != nil {
		return nil, refuseApply(err, "config.apply: the pipeline refused the compiled config")
	}

	if args.dryRun {
		hash, refusal := a.previewHash(res)
		if refusal != nil {
			return nil, refusal
		}
		return a.answer(res, hash, recorded.AppliedHash, true, false, hash == recorded.AppliedHash), nil
	}

	applied, err := apply.Write(a.cfg.ConfigDir, res.Document, a.resolver)
	if err != nil {
		// Nothing was renamed, so the config Core loads is still the one on record. That is the invariant this
		// command states, and it is why a write refusal carries no rollback.
		return nil, refuseApply(err, "config.apply: the compiled config was not written, so the previous one is still applied")
	}
	if err := a.record.Write(supervisor.State{
		AppliedHash:     recorded.AppliedHash,
		SelectedProfile: recorded.SelectedProfile,
		LastStartPID:    recorded.LastStartPID,
		LastExit:        recorded.LastExit,
		Held:            res.Assigned,
	}); err != nil {
		// The file is in place and the port table is not. The next compile without this table can re-allocate a
		// tag's port from scratch, which is a churn question rather than a safety one, so the apply reports the
		// failure it actually has rather than pretending the rename did not happen.
		return nil, refuseApply(err, "config.apply: the config was written but the port reservation table was not stored")
	}

	hash := a.span(applied.HandleHash, applied.ValueHash)
	restarted, refusal := a.reconcile(ctx, args, applied.Path, hash, res, recorded.AppliedHash)
	if refusal != nil {
		return nil, refusal
	}
	return a.answer(res, hash, recorded.AppliedHash, false, restarted, hash == recorded.AppliedHash), nil
}

// reconcile is doc 22 §3 invariant 4 — restart on a hash difference and on nothing else — and the reason the
// comparison is against the applied-state record rather than against `Status().Hash` is that the record survives a
// Connector restart and a dead Core, while a status does not.
//
// Stop-then-start rather than `supervisor.Restart`, because Restart re-spawns the config path already on record
// and this call has just installed a new one.
func (a *configApply) reconcile(ctx context.Context, args applyArgs, path, hash string, res *compile.Result, recorded string) (bool, *dispatch.Refusal) {
	if args.restartPolicy == restartNever || hash == recorded {
		return false, nil
	}
	if live := a.core.Status(); live.Running {
		if _, err := a.core.Stop(ctx, true); err != nil {
			return false, refuseApply(err, "config.apply: the running Core did not stop, so the new config was never handed to it")
		}
	}
	st, err := a.core.Start(ctx, supervisor.StartParams{
		ConfigPath: path,
		ConfigHash: hash,
		// The selection as this frame stated it. The applied-state record calls the field `selectedProfile` and
		// pins no spelling for it; doc 22 §4 forbids the supervisor from knowing what a profile is, so the string
		// is the caller's own list carried through, not a resolved name.
		ProfileID: strings.Join(args.profileIDs, ","),
		Inbounds:  supervisorInbounds(res.Inbounds),
	})
	if err != nil {
		return false, refuseApply(err, "config.apply: the compiled config is on disk and Core was not started with it")
	}
	return st.Running && st.Hash == hash, nil
}

// previewHash is a dryRun's `configHash`. Under hashHandles the compiler's own hash is the answer and the store is
// never read; under hashValues the preview has to materialize to know what the file would hash to, which is the
// consequence of that option ODEC-36 should be argued with: a preview can then refuse for keystore reasons.
func (a *configApply) previewHash(res *compile.Result) (string, *dispatch.Refusal) {
	if a.cfg.Span == hashHandles {
		return res.Hash, nil
	}
	valueDoc, err := apply.Materialize(res.Document, a.resolver)
	if err != nil {
		return "", refuseApply(err, "config.apply: the dry run could not materialize the credential positions the compiled config carries")
	}
	data, err := compile.Emit(valueDoc)
	if err != nil {
		return "", refuseApply(err, "config.apply: the dry run could not emit the document it materialized")
	}
	return compile.Hash(data), nil
}

// span picks which of the two hashes internal/apply reported is the command's `configHash`. The other one is
// dropped here and nowhere else, which is exactly what ODEC-36 says is undecided.
func (a *configApply) span(handleHash, valueHash string) string {
	if a.cfg.Span == hashHandles {
		return handleHash
	}
	return valueHash
}

func (a *configApply) answer(res *compile.Result, hash, previous string, dryRun, restarted, unchanged bool) *applyAnswer {
	return &applyAnswer{
		Applied:                     !dryRun,
		DryRun:                      dryRun,
		ConfigHash:                  hash,
		PreviousConfigHash:          previous,
		Restarted:                   restarted,
		UnchangedBecauseHashMatched: unchanged,
		InboundPorts:                res.Inbounds,
		ServerTags:                  res.ServerTags,
		Unrepresentable:             res.Unrepresentable,
	}
}

func supervisorInbounds(list []compile.Inbound) []supervisor.Inbound {
	if len(list) == 0 {
		return nil
	}
	out := make([]supervisor.Inbound, len(list))
	for i, in := range list {
		out[i] = supervisor.Inbound{Listen: in.Listen, Port: in.Port, Tag: in.Tag}
	}
	return out
}

// maxApplyMessage is `envelope.schema.json#/properties/error/properties/message/maxLength`. A refusal that quoted
// a whole pipeline message would be a frame the codec refuses, which is the worse of the two answers.
const maxApplyMessage = 200

// refuseApply turns one error from the apply path into the answer the contract says it has. The cause is carried
// into the message because every code in the table below names a remediation the caller cannot derive without
// knowing which document, field or stage refused; the clip keeps the frame legal, and no caller's payload is ever
// quoted (framing.json's refusals row logs "never args").
func refuseApply(err error, why string) *dispatch.Refusal {
	code := codeForApply(err)
	msg := clip(fmt.Sprintf("%s: %v", why, err), maxApplyMessage)
	return &dispatch.Refusal{Code: code, Message: msg}
}

// clip cuts a message at a byte budget without cutting a rune in half, because a frame whose message is invalid
// UTF-8 is a frame the codec refuses and a closed port instead of an answer.
//
// It keeps the tail, not the head: these messages are "context: cause: cause: cause", and the end is the one part
// a caller cannot reconstruct — which document, field or stage refused. The head is the surface naming itself, and
// the code already says that. Cutting the head is also what makes the two assertions in config_apply_test.go that
// quote a nested cause survive on a full-length pipeline error.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const marker = "…"
	start := len(s) - (n - len(marker))
	for start > 0 && start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return marker + s[start:]
}

// codeMarkers maps the refusals whose code the corpus states somewhere other than the taxonomy's own meaning to
// that code. Each row is a phrase one of the three packages actually emits — a test in config_apply_test.go drives
// the real refusal for every row, so a reworded message fails that test rather than silently changing an answer a
// client acts on.
//
// ODEC-03 keeps two of these uses open — the bind refusal and the port-range overlap are uses of
// `UnsupportedCombination` that its registered meaning does not cover, and the 64-inbound ceiling is
// `QuotaExceeded` at the storage layer — and the register's `why` names the three Go sites that already behave
// this way. This table follows the prose the corpus has, not a prose the register has not written yet.
var codeMarkers = []struct {
	marker string
	code   string
}{
	// doc 22 §3 invariant 6, ADR-0006's non-loopback answer.
	{"the Connector only ever binds", "UnsupportedCombination"},
	// doc 30 §8, through ODEC-03's own list: an overlap is not a malformed document.
	{"overlaps the block range", "UnsupportedCombination"},
	// doc 27 §9's invariant row and doc 29 §306: the 64-inbound ceiling, in the compiler's own words.
	{"which is what QuotaExceeded names", "QuotaExceeded"},
	// ports.go's allocator running out inside the range the settings declared.
	{"no free port left in", "QuotaExceeded"},
	// doc 23 §10's transport clause, which is what `UnsupportedCombination` means literally.
	{"the schema admits that flow only over", "UnsupportedCombination"},
	// A headersRef is a field the config needs and no decision says how to build; ODEC-03's first option is the
	// reading of `UnsupportedCombination` that says "an intent that cannot be compiled without inventing a field".
	{"ODEC-36 blocks", "UnsupportedCombination"},
}

// codeForApply is the classification table. Order matters: a keystore refusal already carries its code, and the
// three cases below it are wrapped causes the seams raise rather than pipeline refusals.
func codeForApply(err error) string {
	var ke *keystore.Error
	if errors.As(err, &ke) {
		return ke.Code()
	}
	// doc 22 §4: an unbootable Core is surfaced as IntegrityMismatch, not as a config failure.
	if errors.Is(err, supervisor.ErrIntegrity) {
		return "IntegrityMismatch"
	}
	text := err.Error()
	for _, m := range codeMarkers {
		if strings.Contains(text, m.marker) {
			return m.code
		}
	}
	if strings.HasPrefix(text, "compile: ") {
		// Everything else the pipeline refuses is a profile or server document that failed entity validation,
		// which is `InvalidProfile`'s registered meaning and doc 27 §2's stage-1 answer verbatim.
		return "InvalidProfile"
	}
	// A write, a record or a spawn failure that no row above claims. `DiskFull` is declared by this command and
	// unreachable from here: internal/apply reports the OS's failure without saying whether it was space, and
	// telling those two apart is an errno question with no document attached to it yet.
	return "ConfigApplyFailed"
}
