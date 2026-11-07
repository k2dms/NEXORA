// Package health is the HealthProber — `docs/architecture/22-component-contracts.md` §8, the fifth of that
// document's five Connector components and the only one whose answer entitles a green mark.
//
// §8's first invariant is why four layers exist instead of one latency number: a TCP connect to the VPS succeeds
// whether or not the tunnel works, so nothing short of a request that left this machine through a Core inbound and
// came back with the address the user expects can be shown as proof. That is `docs/verification/live-matrix.md`
// LIVE-04, and it is why the egress endpoint and its expected answer are required wiring here rather than optional
// knobs: New refuses a prober that cannot answer "and did it actually leave?" for the same reason
// internal/supervisor refuses one that cannot prove its binary.
//
// What the corpus does not state is where those two facts live. `packages/schema/protocol/commands/server.test.json`
// asks for "the configured egress endpoint" in `#/args/properties/egressProbe` and compares against an expected
// address in `#/result/properties/matchesExpectedEgress`, and no field of any file under `packages/schema/` names
// either — `packages/schema/connector-settings.schema.json#/properties` has five keys and none of them is a probe
// target. The constructor is where that gap is felt, and `ODEC-42` is where it is recorded.
//
// Three more things belong to this package because §8's Out and `packages/schema/protocol/data-ownership.json`
// `#/stores` give them no other writer: the consecutive-failure streak and cooldown of
// `packages/schema/connector-profile.schema.json#/properties/failover` (invariant 3 — the count decides, never one
// timeout), the per-server retention ring behind the `health` diagnostics section, and the `server.health.changed`
// event, whose payload is a server id and nothing else.
//
// What it does not do is §8's non-goals: no selection order, no history past the ring, and nothing in a browser. It
// also does not schedule. `Due` is the answer a poller asks; running the poll is doc 30's reconciliation loop, and a
// prober that started its own goroutine would be probing behind the surface that owns the budget.
package health

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/k2dms/nexora/connector/internal/compile"
	"github.com/k2dms/nexora/connector/internal/contract"
)

// The five layer names are `packages/schema/protocol/commands/server.test.json#/result/properties/layers`
// `/items/properties/layer/enum`, and the four requestable ones are that array's `#/args/properties/layers`
// `/items/enum`. `egress-ip` is not requestable: it exists only as the answer of an `egressProbe` request, which is
// why the result allows five rows where the args allow four.
const (
	LayerDNS          = "dns"
	LayerTCP          = "tcp"
	LayerCoreOutbound = "core-outbound"
	LayerProxiedHTTP  = "proxied-http"
	LayerEgressIP     = "egress-ip"
)

// The four states of one layer row are that same item's `state` enum.
const (
	StatePass    = "pass"
	StateFail    = "fail"
	StateSkipped = "skipped"
	StateTimeout = "timeout"
)

// The four verdicts are `#/result/properties/overall/enum`, whose description separates `partial` from
// `not-proxied` because "a higher layer did not work" and "traffic leaked" are two different things to tell a user.
const (
	OverallPass       = "pass"
	OverallPartial    = "partial"
	OverallFail       = "fail"
	OverallNotProxied = "not-proxied"
)

// Verdicts are the retained status's vocabulary, not the wire's. `unknown` is a server this process has never
// probed, which is not the same fact as `down` and must never render the same way.
//
// The two named verdicts answer the failover question — has the profile declared this server out? — and not the
// question "did the last probe pass". A server with one failure under a policy that tolerates three is `healthy` in
// this field and `fail` in `Health.LastOverall`, and both numbers sit on one row because collapsing them would either
// evict a server the profile has not given up on or tell a support bundle that nothing is wrong.
const (
	VerdictUnknown = "unknown"
	VerdictHealthy = "healthy"
	VerdictDown    = "down"
)

// order is `#/args/properties/layers`' own instruction — "Ordered by the Connector, not by the caller: dns before
// tcp before core-outbound before proxied-http" — and the short-circuit order below: a row's measurement is only
// meaningful once every row before it has succeeded, which is what `skipped` is for.
var order = []string{LayerDNS, LayerTCP, LayerCoreOutbound, LayerProxiedHTTP}

func rank(layer string) int {
	for i, l := range order {
		if l == layer {
			return i
		}
	}
	return len(order)
}

// Server is the two facts a direct probe needs about a VPS. The store's record is `compile.Server` and carries
// credentials by reference, transports, and a display name; none of that reaches a probe, because §8 invariant 5 is
// that a probe never reveals a secret in its result or its logs and the narrowest possible read is what makes that
// checkable rather than merely intended.
type Server struct {
	ID   string
	Host string
	Port int
}

// Egress is what an egress probe fetches through the tunnel and what the answer has to be.
//
// `URL` is origin-form over http or https; https is verified with the platform roots, because a probe that accepted
// any certificate would be a man-in-the-middle test that always passes. `IP` is the public address the VPS presents
// — the thing `matchesExpectedEgress` compares against and the only number in this package that is a *claim* rather
// than a measurement.
type Egress struct {
	URL string
	IP  string
}

// Inbound is one row of the Core's live inbound table, in the compiler's own shape. The port for a server is found
// by the tag the compiler derived from its id (`compile.ServerTag`, prefixed with `in-` exactly as
// `inboundDocument` emits it), so a prober never guesses a port and a stale table is a refusal rather than a
// measurement of somebody else's inbound.
func inboundTag(serverID string) string { return "in-" + compile.ServerTag(serverID) }

// Request is one probe: `server.test#/args` in Go, after this package's command surface has refused a payload that
// does not match it. Layers arrive unordered and come out canonical; `Repeat` is clamped to the schema's 1 to 10 by
// the surface, not here.
type Request struct {
	ServerID string
	Layers   []string
	Egress   bool
	Repeat   int
}

// Attempt is one measurement of one layer by one run. The rows the command answers with are aggregates over
// `Repeat` attempts, so this is the intermediate the aggregation reads.
type Attempt struct {
	State  string
	RTT    time.Duration
	Code   string
	Detail string
}

// Measure is one row of the answer: `#/result/properties/layers/items`. `rttMs` is the mean over the repeat and
// `jitterMs` its spread, which is why jitter is absent for a single run — one sample has no spread, and a zero there
// would be a measurement of "no variation" rather than of "not measured".
type Measure struct {
	Layer    string   `json:"layer"`
	State    string   `json:"state"`
	RTTMS    *float64 `json:"rttMs,omitempty"`
	JitterMS *float64 `json:"jitterMs,omitempty"`
	Code     string   `json:"code,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

// Answer is `server.test#/result`. The three required fields are always present; the two egress fields are the
// contract's own conditional — `egressIp` is "only present when egressProbe was requested and proxied-http passed"
// and `matchesExpectedEgress` is nullable because an endpoint that refused to answer has no opinion about the IP.
type Answer struct {
	ServerID              string    `json:"serverId"`
	Layers                []Measure `json:"layers"`
	Overall               string    `json:"overall"`
	EgressIP              *string   `json:"egressIp,omitempty"`
	MatchesExpectedEgress *bool     `json:"matchesExpectedEgress,omitempty"`
	TestedAt              string    `json:"testedAt"`
}

// Health is §8's "retained status": what this process last believed about one server, and when it will next allow
// itself to believe otherwise. It is a status and not a verdict a UI may render as proof — LIVE-04 licenses only a
// `server.test` answer whose egress layer compared against the expected address, and `connector.health` is where a
// stale one has to be re-read rather than replayed (`docs/architecture/30-failure-policy.md` §4).
type Health struct {
	ServerID            string
	Verdict             string
	LastOverall         string
	ConsecutiveFailures int
	CoolingDown         bool
	CooldownUntil       time.Time
	LastProbeAt         time.Time
	NextProbeAt         time.Time
}

// Event is the one publication this package makes. Its payload is a server id and nothing else, which is
// `packages/schema/observability.json#/events/6`'s `payload` (closed, `additionalProperties: false`, `required:
// ["serverId"]`) and its `neverInPayload` ("latency numbers: a value in an event is a value the subscriber will show
// without re-reading, which is how a stale probe becomes a green mark").
type Event struct {
	ServerID string
}

// Config is the prober's wiring. New refuses the parts of it this package is forbidden to invent, and defaults the
// parts the corpus bounds somewhere else.
type Config struct {
	// Server resolves an id the frame named. §8 gives the prober no store: the ids come from a command surface that
	// reads one, and doc 20 §3 keeps the read in the surface rather than in each component that wants a fact.
	Server func(ctx context.Context, id string) (Server, error)

	// Inbounds is doc 22 §8's "Dependencies: CoreSupervisor (for the port to hit)" — the live inbound table, which
	// `internal/supervisor.Supervisor.Status().Inbounds` is the production answer for. An empty table or one without
	// this server's tag is `docs/architecture/30-failure-policy.md` §4's first rule ("No probes. `server.test`
	// answers `CoreNotRunning`"), and the fixture's message names the reason: a probe of a port the dead Core left
	// behind would report success against a stale inbound.
	Inbounds func() []compile.Inbound

	// Egress is the endpoint and the expected answer. Required; see the package doc for why the schema does not
	// provide a place for it.
	Egress Egress

	// Failover is the profile's policy — the streak length, the cooldown and the re-probe interval. It is
	// `compile.Failover` rather than a copy of those three numbers, and that type's own comment has said it "belongs
	// to the HealthProber, which does not exist yet" since internal/compile was written; this is the thing it belongs
	// to.
	Failover compile.Failover

	// LookupHost is the resolver the dns layer reads. nil is the platform's. The seam exists so that a test can
	// distinguish "the prober handled a refusal" from "this machine's resolver is slow", which the dns layer has to
	// be able to report as its own row.
	LookupHost func(ctx context.Context, host string) ([]string, error)

	// DialContext opens the TCP connections the layers ride on. nil is a plain net.Dialer.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	// Events receives `server.health.changed`, and only when the verdict moves: an event per probe would be the
	// cached answer arriving unasked, forty times a minute. nil is a prober that keeps records and publishes nothing,
	// which is what a build without an event pump has.
	Events func(Event)

	// Now is the clock the records, the cooldown and `testedAt` read. Tests pass a fake one; production leaves it
	// nil. RTT is not measured against it — a duration read from a faked clock would be a fake duration.
	Now func() time.Time

	// LayerTimeout bounds one attempt of one layer. Zero is defaultLayerTimeout.
	LayerTimeout time.Duration

	// RingBytes bounds the retained history per server. Zero is `DefaultRingBytes`.
	RingBytes int
}

// defaultLayerTimeout is the prober's own number, and the corpus states none: `server.test`'s deadline is 30000 ms
// (`contract.LookupMethod`'s `TimeoutMs`) and the command's four layers are bounded only by that. Five seconds per
// attempt lets the worst case the args allow — four layers at repeat 10 — exceed the command deadline, which is the
// caller's problem to bound and `Timeout`'s registered meaning ("the deadline owned by the caller expired"), and
// lets the ordinary case (four layers, one repeat, one dead VPS) answer in twenty seconds with every row named.
const defaultLayerTimeout = 5 * time.Second

// New builds the prober over its seams. A missing server lookup, a missing inbound table or an unset egress pair is
// refused rather than defaulted, because each of the three would turn §8's first invariant into a report that cannot
// mean anything.
func New(cfg Config) (*Prober, error) {
	switch {
	case cfg.Server == nil:
		return nil, errors.New("health: no server lookup; the probe is asked for by id and this package owns no store (doc 22 §8's In)")
	case cfg.Inbounds == nil:
		return nil, errors.New("health: no inbound table; doc 22 §8 gives the prober CoreSupervisor for exactly this fact")
	case cfg.Egress.URL == "":
		return nil, fmt.Errorf("health: no egress endpoint; `server.test`'s egressProbe asks for \"the configured egress endpoint\" and no schema field carries one — ODEC-42 is the register entry, not this call site")
	case cfg.Egress.IP == "":
		return nil, errors.New("health: no expected egress address, and `matchesExpectedEgress` would then compare against nothing while `not-proxied` would stay unreachable")
	}
	if _, err := parseTarget(cfg.Egress.URL); err != nil {
		return nil, fmt.Errorf("health: the egress endpoint %q is not a fetchable http or https URL: %w", cfg.Egress.URL, err)
	}
	if ip := net.ParseIP(cfg.Egress.IP); ip == nil {
		return nil, fmt.Errorf("health: the expected egress address %q is not an IP literal; `matchesExpectedEgress` compares literals, and a hostname there would be a second resolver question the prober cannot answer", cfg.Egress.IP)
	}
	if cfg.Failover.MaxConsecutiveFailures < 0 || cfg.Failover.MaxConsecutiveFailures > 20 {
		return nil, fmt.Errorf("health: maxConsecutiveFailures is %d, outside connector-profile.schema.json's 1 to 20", cfg.Failover.MaxConsecutiveFailures)
	}
	if cfg.Failover.CooldownSeconds < 0 || cfg.Failover.CooldownSeconds > 3600 {
		return nil, fmt.Errorf("health: cooldownSeconds is %d, outside that schema's 5 to 3600 (0 means no failover policy is configured)", cfg.Failover.CooldownSeconds)
	}
	if cfg.Failover.ProbeIntervalSeconds < 0 || cfg.Failover.ProbeIntervalSeconds > 3600 {
		return nil, fmt.Errorf("health: probeIntervalSeconds is %d, outside that schema's 10 to 3600", cfg.Failover.ProbeIntervalSeconds)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.LookupHost == nil {
		cfg.LookupHost = net.DefaultResolver.LookupHost
	}
	if cfg.DialContext == nil {
		cfg.DialContext = (&net.Dialer{}).DialContext
	}
	if cfg.LayerTimeout <= 0 {
		cfg.LayerTimeout = defaultLayerTimeout
	}
	if cfg.RingBytes <= 0 {
		cfg.RingBytes = DefaultRingBytes
	}
	return &Prober{cfg: cfg, records: map[string]*record{}}, nil
}

// Prober measures servers. It is safe for concurrent use: the command surface answers frames from one process while
// a poller may be probing the same table, and no probe holds the bookkeeping lock across its network I/O, because a
// dead VPS must not be able to stall a status read.
type Prober struct {
	cfg     Config
	mu      sync.Mutex
	records map[string]*record
}

// The four ways a probe can stop before it has measured anything. Each carries the taxonomy code the command answers
// with, so a refusal here cannot drift from the code `packages/schema/protocol/commands/server.test.json#/errors`
// already promises; the per-layer failures are not in this list because they are the answer rather than a refusal.
var (
	// ErrNoServer is an id the store does not have. Of the ten codes this command declares, `HealthCheckFailed` is
	// the only one that does not say something untrue: `InvalidProfile` would be the honest word and it is not in that
	// array, `Internal` is forbidden from being "the default for anything the caller could handle", and the caller can
	// handle this one — it can send a different id.
	ErrNoServer = errors.New("no server document is stored under that id")
	// ErrCoreNotRunning is doc 30 §4's first rule and `packages/schema/fixtures/protocol/failure-block.jsonl`'s own
	// message: with no live inbound for this server's tag, a probe would measure a port nobody owns.
	ErrCoreNotRunning = errors.New("Core is not running an inbound for this server")
	// ErrNoEgress is an `egressProbe` request this prober was not wired to answer. New refuses a prober built without
	// an endpoint, so reaching this means the wiring changed after construction — which is the only way the code stays
	// honest rather than dead.
	ErrNoEgress = errors.New("no egress endpoint is configured")
	// ErrBadProbe is a request that does not match `server.test#/args`: an unknown layer name, a duplicate, a
	// repeat outside the 1 to 10 the command bounds. It is `HealthCheckFailed` rather than `Internal` for the same
	// reason ErrNoServer is: the caller can fix it, and `Internal` is forbidden from being the default for anything
	// a caller could handle. The command surface decodes the same schema first, so from the wire these refusals are
	// normally unreachable — a second honest answer costs nothing and keeps a direct Go caller (the poller) from
	// reading `Internal` as "the probe itself broke".
	ErrBadProbe = errors.New("the probe request does not match the command's args schema")
	// ErrDeadline is the caller's own budget expiring between layers. The rows already measured still say what was
	// true at that moment, which is why this is a refusal rather than a short answer the schema would reject.
	ErrDeadline = errors.New("the probe deadline expired before every requested layer was attempted")
)

// codeFor maps each refusal to its error-taxonomy code, in the shape internal/keystore uses for the same reason: the
// codes are looked up rather than typed at the call site, so a taxonomy edit that renames one fails here at init
// instead of shipping a refusal no client can classify.
func codeFor(err error) string {
	switch {
	case errors.Is(err, ErrNoServer), errors.Is(err, ErrNoEgress), errors.Is(err, ErrBadProbe):
		return "HealthCheckFailed"
	case errors.Is(err, ErrCoreNotRunning):
		return "CoreNotRunning"
	case errors.Is(err, ErrDeadline):
		return "Timeout"
	}
	return "Internal"
}

// Error is a probe refusal. `Op` names the step that stopped — one of the layer names where a layer is the reason —
// and Cause is one of the sentinels above, so a caller can classify without reading a sentence.
type Error struct {
	Op       string
	ServerID string
	Cause    error
}

func (e *Error) Error() string {
	if e.ServerID == "" {
		return fmt.Sprintf("health: %s: %s", e.Op, e.Cause)
	}
	return fmt.Sprintf("health: %s: %s (%s)", e.Op, e.Cause, e.ServerID)
}

func (e *Error) Unwrap() error { return e.Cause }

// Code returns the taxonomy code name for this refusal.
func (e *Error) Code() string { return codeFor(e.Cause) }

// Taxonomy resolves the code against the projected contract so a caller can read its layer and retryability without a
// second table.
func (e *Error) Taxonomy() (contract.Code, bool) { return contract.LookupCode(e.Code()) }

func refuse(op, serverID string, cause error) *Error {
	return &Error{Op: op, ServerID: serverID, Cause: cause}
}

// serverTestErrors is `packages/schema/protocol/commands/server.test.json#/errors`, copied rather than read at
// runtime for the reason internal/contract copies its patterns: the Connector ships as one binary, and the check
// below is what makes such a copy fail loudly instead of quietly.
var serverTestErrors = []string{
	"CoreNotRunning", "NetworkUnreachable", "HandshakeFailed", "AuthFailed", "KeystoreUnavailable",
	"HealthCheckFailed", "IntegrityMismatch", "Timeout", "UnauthorizedExtension", "Internal",
}

func init() {
	for _, sentinel := range []error{ErrNoServer, ErrCoreNotRunning, ErrNoEgress, ErrBadProbe, ErrDeadline} {
		code, ok := contract.LookupCode(codeFor(sentinel))
		if !ok {
			panic("health: the projected taxonomy has no " + codeFor(sentinel) + " code")
		}
		if !slices.Contains(serverTestErrors, code.Name) {
			panic("health: " + code.Name + " is not in server.test.json#/errors, and this package's refusal table was written against a table that said otherwise")
		}
	}
}
