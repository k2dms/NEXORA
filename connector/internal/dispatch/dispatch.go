// Package dispatch is the connector's front door — the total function
// `packages/schema/protocol/command-allowlist.json#/dispatchRule` promises, with exactly one failure case and no
// fallthrough. It decides whether a request may reach a handler and never what a handler does: connector.hello is
// answered by internal/hello, and every method that changes state is M3.
//
// The order is the order the contract gives its rules. The caller before the method, because ADR-0004 rule 6 answers
// a sender that is not on the allowlist with `UnauthorizedExtension` and nothing else, whatever it asked for. The
// method before the negotiation, because `docs/architecture/26-command-surface.md` §0 rule 1 resolves the name
// against a closed set and rule 3 gates everything but `connector.hello` behind a successful hello. The negotiation
// before the budget, because `packages/schema/protocol/framing.json#/senderPolicy/rateLimitFacts/countedWhere` puts
// the counting in this process -- "the Connector, before it hands the frame to a handler" -- and adds why it cannot be
// the shim's: a scoped budget names a method, the method name is inside the payload, and
// `docs/architecture/25-native-messaging-protocol.md` §2 forbids the shim to interpret payloads. A request refused by
// budget never reaches a handler, so it cannot half-apply.
// And all of them before state.
//
// What is deliberately not here: which ports a sender may hold open. `#/senderPolicy/portsPerSender` caps them at
// two and closes the oldest, which is a fact about connections and therefore the host's, not a decision a function
// that has never seen a connection can make.
package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/envelope"
)

// Identity is what the host resolved before it read a frame: who may speak, and how far the negotiation on this
// connection has come. None of it comes from the payload — a field asserting that the caller is the extension is on
// `framing.json#/senderPolicy/identity/notIdentity`, and ADR-0004 rule 3 requires the platform's answer instead.
type Identity struct {
	// SenderID is the extension id the OS handed the host when the port opened.
	SenderID string
	// Allowlisted is whether that id is in the host's on-disk startup configuration, which the Connector installer
	// writes and browser storage never does (`#/senderPolicy/identity/allowlistNeverIn`).
	Allowlisted bool
	// Negotiated reports that `connector.hello` has succeeded on this connection.
	Negotiated bool
	// Capabilities are the names that hello announced. A method whose `capabilities[]` is not covered by them was
	// sent before the peer was allowed to send it (§7 of `docs/architecture/25-native-messaging-protocol.md`).
	Capabilities []string
}

func (id Identity) knows(name string) bool {
	for _, c := range id.Capabilities {
		if c == name {
			return true
		}
	}
	return false
}

// Refusal is a decision taken before any handler runs, so that no side effect can half-happen.
type Refusal struct {
	// Code is a member of `packages/schema/protocol/error-taxonomy.json`. It is empty when Close is set, because
	// then the contract's answer is that there is no answer.
	Code string
	// Message names the rule that refused. It never quotes args: `#/senderPolicy/refusals/loggedFields` is
	// "sender id, method name, window remaining -- never args, which may carry a document the user typed".
	Message string
	// Close tells the host to end the stream instead of keeping up the exchange: the amplification guard in
	// `#/senderPolicy/refusals/amplificationGuard` fired. Closing the pipe is not a shutdown signal for the
	// connector or for Core (ADR-0005 rule 4).
	Close bool
}

func (r *Refusal) Error() string {
	if r.Close {
		return fmt.Sprintf("dispatch: %s, and the port closes", r.Message)
	}
	return fmt.Sprintf("dispatch: %s answered %s: %s", r.Message, r.Code, r.detail())
}

func (r *Refusal) detail() string {
	c, ok := contract.LookupCode(r.Code)
	if !ok {
		return "a code outside the taxonomy"
	}
	if c.Retryable {
		return "retryable"
	}
	return "not retryable"
}

// Handler answers a request that the front door already admitted. A nil Refusal means the result goes on the wire.
type Handler func(ctx context.Context, f *envelope.Frame) (any, *Refusal)

// Config is the dispatcher's wiring. The zero value has no handlers, which is a dispatcher that refuses everything
// with `Internal` — a build that forgot to register its methods is not a reason to run state.
type Config struct {
	// Handlers maps a contract method name to the function that answers it. Every name must be in the closed
	// allowlist; New refuses a dispatcher built around a method the contract does not publish.
	Handlers map[string]Handler
	// Now is the clock the budgets, refusal window and response dedupe read. Tests pass a fake one; production leaves it nil.
	Now func() time.Time
}

// Dispatcher holds the per-sender counters, which `#/senderPolicy/rateLimitFacts` defines as per sender id and per
// process and explicitly not persisted: "spent budget dies with the process".
//
// It is safe for concurrent use, because the host serves every sender's frames from one process.
type Dispatcher struct {
	handlers map[string]Handler
	now      func() time.Time

	mu       sync.Mutex
	spent    map[string]map[string][]time.Time
	refusals map[string][]time.Time

	responses map[string]*responseCache
	evictions DedupeStats
}

// DedupeStats exposes aggregate eviction counts without request ids, senders or payloads.
// Expiry is lazy: age evictions are counted when the sender next uses the cache.
type DedupeStats struct {
	AgeEvictions   uint64
	EntryEvictions uint64
	ByteEvictions  uint64
}

// responseCache is per sender. Entries include in-flight reservations, which cannot be evicted;
// order lists completed request ids in first-completion order, never replay order.
// Only routing keys and encoded successful answers are retained, never frames, args or digests.
type responseCache struct {
	entries map[string]*cachedResponse
	order   []string
	bytes   int
}

type cachedResponse struct {
	method    string
	done      chan struct{}
	result    json.RawMessage
	completed time.Time
}

// New builds the dispatcher over the generated surface.
func New(cfg Config) (*Dispatcher, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	for name := range cfg.Handlers {
		if _, ok := contract.LookupMethod(name); !ok {
			return nil, fmt.Errorf("dispatch: handler registered for %q, which the allowlist does not publish", name)
		}
	}
	handlers := make(map[string]Handler, len(cfg.Handlers))
	for name, h := range cfg.Handlers {
		handlers[name] = h
	}
	return &Dispatcher{
		handlers: handlers, now: cfg.Now,
		spent: map[string]map[string][]time.Time{}, refusals: map[string][]time.Time{},
		responses: map[string]*responseCache{},
	}, nil
}

// Methods returns the names this dispatcher can actually run, sorted. It exists so that a method the contract
// publishes and this build forgot to register is a visible gap rather than an assumed surface — what a hello
// announces is the capability registry, which internal/hello reads from the projection.
func (d *Dispatcher) Methods() []string {
	out := make([]string, 0, len(d.handlers))
	for name := range d.handlers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Dispatch resolves one inbound request frame and, if every gate admits it, runs its handler. Exactly one of the
// two results is non-nil.
func (d *Dispatcher) Dispatch(ctx context.Context, id Identity, f *envelope.Frame) (any, *Refusal) {
	if f.Type != envelope.TypeRequest {
		return nil, d.refuse(id, "BadRequest", fmt.Sprintf("a %s frame arrived where a request was expected", f.Type))
	}
	method := ""
	if f.Method != nil {
		method = *f.Method
	}
	// The caller first, always: rule 6 of ADR-0004 answers an unknown sender with the sender error and does not let
	// it learn which methods exist.
	if !id.Allowlisted {
		return nil, d.refuse(id, "UnauthorizedExtension",
			fmt.Sprintf("%s from %q is not on the host's allowlist", method, id.SenderID))
	}
	m, known := contract.LookupMethod(method)
	if !known {
		// No default handler, no closest match: deniedByDefault is what makes the set closed.
		return nil, d.refuse(id, "UnknownMethod", fmt.Sprintf("%q is not a published method", method))
	}
	if !m.AvailableBeforeHello && !id.Negotiated {
		// The state the gate reads is a boolean, and two histories produce it: no hello has ever crossed this
		// stream, or a hello was answered and a later `VersionMismatch` withdrew it (internal/hello's
		// Session.Record). A sentence naming the first history would be false during the second, which is the
		// case the recorded corpus reaches on purpose.
		return nil, d.refuse(id, "HandshakeFailed",
			fmt.Sprintf("%s needs a negotiation in force on %q's connection, and none is: no connector.hello answered, or one withdrawn",
				method, id.SenderID))
	}
	for _, need := range m.Capabilities {
		if !id.knows(need) {
			return nil, d.refuse(id, "HandshakeFailed",
				fmt.Sprintf("%s needs the %q capability, which %q's hello did not announce", method, need, id.SenderID))
		}
	}
	handler, ok := d.handlers[method]
	if !ok {
		return nil, d.refuse(id, "Internal", fmt.Sprintf("%s is published but this build registers no handler for it", method))
	}
	// Admission is always rechecked, even for a stored answer. Dedupe precedes budgets and state,
	// so a retry does not spend the mutation budget or repeat a protected write.
	entry, replay, r := d.beginResponse(ctx, id, f.ID, m)
	if r != nil {
		return nil, r
	}
	var encoded json.RawMessage
	if entry != nil {
		// Also releases reservations after budget refusals or a handler panic. No mutex is held
		// while invoking a handler, encoding its answer, or waiting for another invocation.
		defer func(requestID string) { d.completeResponse(id.SenderID, requestID, entry, encoded) }(f.ID)
	}
	if r := d.charge(id, m, replay != nil); r != nil {
		return nil, r
	}
	if replay != nil {
		return replay, nil
	}
	result, refusal := handler(ctx, f)
	// A handler's error is an answer, not a refusal: the amplification guard bounds the front door's volume of
	// saying no, and a failing method must not be able to close a sender's port. No errors are cached.
	if entry != nil && refusal == nil {
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, &Refusal{Code: "Internal", Message: "handler result could not be encoded"}
		}
		encoded = json.RawMessage(raw)
	}
	// Preserve the handler's typed first result (hello/session wiring also uses typed answers).
	// Encoding above detached the cached result from any mutable object the caller receives.
	return result, refusal
}

func cacheable(m contract.Method) bool {
	// Hello establishes per-connection session state and cannot be reduced to a stored wire answer.
	// Probes must always take a fresh measurement, even if their desired-state flag is idempotent.
	return m.Replayable && m.Idempotent && !m.AvailableBeforeHello && m.Name != "server.test"
}

// beginResponse returns either an owned reservation, an independent replay, or a refusal.
// A waiter retains only a completion channel, not the original request or a response outside the bound.
func (d *Dispatcher) beginResponse(ctx context.Context, id Identity, requestID string, m contract.Method) (*cachedResponse, json.RawMessage, *Refusal) {
	for {
		d.mu.Lock()
		cache := d.responses[id.SenderID]
		if cache != nil {
			d.expireResponsesLocked(cache, d.now())
			if entry := cache.entries[requestID]; entry != nil {
				if entry.method != m.Name {
					r := d.refuseLocked(id, "BadRequest", "request id is already bound to another method")
					d.mu.Unlock()
					return nil, nil, r
				}
				if entry.result != nil {
					replay := append(json.RawMessage(nil), entry.result...)
					d.mu.Unlock()
					return nil, replay, nil
				}
				done := entry.done
				d.mu.Unlock()
				select {
				case <-done:
					continue
				case <-ctx.Done():
					return nil, nil, d.refuse(id, "Busy", "request is still in flight")
				}
			}
		}
		if !cacheable(m) {
			d.mu.Unlock()
			return nil, nil, nil
		}
		if cache == nil {
			cache = &responseCache{entries: map[string]*cachedResponse{}}
			d.responses[id.SenderID] = cache
		}
		if len(cache.entries) >= contract.DedupeMaxEntries {
			if len(cache.order) == 0 {
				r := d.refuseLocked(id, "Busy", "response dedupe has no free in-flight slot")
				d.mu.Unlock()
				return nil, nil, r
			}
			evictResponse(cache)
			d.evictions.EntryEvictions++
		}
		entry := &cachedResponse{method: m.Name, done: make(chan struct{})}
		cache.entries[requestID] = entry
		d.mu.Unlock()
		return entry, nil, nil
	}
}

func (d *Dispatcher) completeResponse(senderID, requestID string, entry *cachedResponse, result json.RawMessage) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer close(entry.done)
	cache := d.responses[senderID]
	at := d.now()
	d.expireResponsesLocked(cache, at)
	if len(result) > contract.DedupeMaxBytes {
		// A single oversized answer cannot displace the whole cache or exceed its byte bound.
		d.evictions.ByteEvictions++
		result = nil
	}
	if result == nil {
		delete(cache.entries, requestID)
		if len(cache.entries) == 0 {
			delete(d.responses, senderID)
		}
		return
	}
	for cache.bytes > contract.DedupeMaxBytes-len(result) {
		evictResponse(cache)
		d.evictions.ByteEvictions++
	}
	entry.result, entry.completed = result, at
	cache.bytes += len(result)
	cache.order = append(cache.order, requestID)
}

func (d *Dispatcher) expireResponsesLocked(cache *responseCache, at time.Time) {
	edge := at.Add(-time.Duration(contract.DedupeWindowSeconds) * time.Second)
	for len(cache.order) > 0 && !cache.entries[cache.order[0]].completed.After(edge) {
		evictResponse(cache)
		d.evictions.AgeEvictions++
	}
}

// evictResponse removes the oldest completed answer. In-flight entries never appear in order.
func evictResponse(cache *responseCache) {
	requestID := cache.order[0]
	cache.bytes -= len(cache.entries[requestID].result)
	delete(cache.entries, requestID)
	cache.order[0] = ""
	cache.order = cache.order[1:]
}

// DedupeStats returns process-local counters only; it emits no protocol event.
func (d *Dispatcher) DedupeStats() DedupeStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.evictions
}

// charge takes the method budget for new work and the catch-all floor for every answer, including
// cached replays. It refuses with retryable Busy and records only what it admits.
func (d *Dispatcher) charge(id Identity, m contract.Method, replay bool) *Refusal {
	d.mu.Lock()
	defer d.mu.Unlock()
	at := d.now()

	named := []contract.RateLimit{contract.CatchAllRateLimit()}
	if rl, ok := contract.RateLimitFor(m.Name); ok && !replay {
		named = append([]contract.RateLimit{rl}, named...)
	}
	if d.spent[id.SenderID] == nil {
		d.spent[id.SenderID] = map[string][]time.Time{}
	}
	for _, rl := range named {
		// Keep the pruned length, not just its backing array: replay bursts in the next
		// window must not duplicate live timestamps or retain unbounded spent history.
		k := key(rl)
		d.spent[id.SenderID][k] = prune(d.spent[id.SenderID][k], at, rl.WindowSeconds)
		if len(d.spent[id.SenderID][k]) >= rl.MaxRequests {
			return d.refuseLocked(id, "Busy",
				fmt.Sprintf("%s: %d requests in %ds is the budget for %s",
					m.Name, rl.MaxRequests, rl.WindowSeconds, scope(rl)))
		}
	}
	for _, rl := range named {
		d.spent[id.SenderID][key(rl)] = append(d.spent[id.SenderID][key(rl)], at)
	}
	return nil
}

func (d *Dispatcher) refuse(id Identity, code, message string) *Refusal {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.refuseLocked(id, code, message)
}

// refuseLocked counts the refusal against the amplification guard and, once the sender has been refused
// RefusalsPerWindowMax times inside RefusalWindowSeconds, stops naming a code at all: the contract's answer to a
// retry loop is "close the port rather than keep answering". Every kind of refusal counts toward that window,
// including a `Busy` the sender provoked by hammering a budget, because the guard's stated reason is the cost of
// answering and not the identity of the caller.
func (d *Dispatcher) refuseLocked(id Identity, code, message string) *Refusal {
	r := &Refusal{Code: code, Message: message}
	at := d.now()
	d.refusals[id.SenderID] = append(prune(d.refusals[id.SenderID], at, contract.RefusalWindowSeconds), at)
	if len(d.refusals[id.SenderID]) > contract.RefusalsPerWindowMax {
		return &Refusal{Message: fmt.Sprintf("%d refusals in %ds from %q",
			contract.RefusalsPerWindowMax, contract.RefusalWindowSeconds, id.SenderID), Close: true}
	}
	return r
}

// Refused reports how many refusals a sender has left outstanding in the guard window — the number the host's log
// line carries beside the sender id and the method name.
func (d *Dispatcher) Refused(senderID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(prune(d.refusals[senderID], d.now(), contract.RefusalWindowSeconds))
}

func prune(in []time.Time, now time.Time, windowSeconds int) []time.Time {
	edge := now.Add(-time.Duration(windowSeconds) * time.Second)
	out := in[:0]
	for _, at := range in {
		if at.After(edge) {
			out = append(out, at)
		}
	}
	return out
}

func key(rl contract.RateLimit) string { return fmt.Sprintf("%s/%d", scope(rl), rl.WindowSeconds) }

func scope(rl contract.RateLimit) string {
	if len(rl.Methods) == 1 && rl.Methods[0] == contract.ScopeAllMethods {
		return "every method"
	}
	return fmt.Sprintf("%v", rl.Methods)
}
