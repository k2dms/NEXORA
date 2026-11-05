// Package contract is the Go projection of packages/schema/protocol: the nineteen published methods, the
// closed error taxonomy, the capability registry, the transport numbers and the per-sender budgets.
//
// Everything in contract_gen.go is generated — see internal/contractgen — and the drift test in
// contract_test.go regenerates it from the schema directory and compares bytes, so a schema edit that was
// never projected cannot ship. The hand-written half below only reads the projection.
package contract

import "sort"

//go:generate go run github.com/k2dms/nexora/connector/internal/contractgen/gen -out .

// Method is one entry of the closed command allowlist: the flags from command-allowlist.json plus the
// execution facts from that method's document in protocol/commands/.
type Method struct {
	Name                 string
	Class                string
	AvailableBeforeHello bool
	Privileged           bool
	Replayable           bool
	TimeoutMs            int
	Capabilities         []string
}

// Code is one member of the closed error taxonomy (protocol/error-taxonomy.json).
type Code struct {
	Name      string
	Layer     string
	Retryable bool
}

// RateLimit is one per-sender budget from framing.json#/senderPolicy/rateLimits. Exhausting it answers
// retryable Busy before dispatch, so a refused request never reaches a handler. Methods holds either
// command names or the single element ScopeAllMethods, which is the catch-all budget.
type RateLimit struct {
	Methods       []string
	WindowSeconds int
	MaxRequests   int
}

// ScopeAllMethods is the "*" scope in the contract data: a budget that counts every request a sender makes.
const ScopeAllMethods = "*"

var (
	methods  = indexMethods(Methods)
	codes    = indexCodes(ErrorCodes)
	limits   = indexLimits(RateLimits)
	global   = catchAll(RateLimits)
	caps     = indexSet(Capabilities)
	names    = sortedNames(Methods)
	privName = sortedPrivileged(Methods)
)

func indexMethods(in []Method) map[string]Method {
	out := make(map[string]Method, len(in))
	for _, m := range in {
		out[m.Name] = m
	}
	return out
}

func indexCodes(in []Code) map[string]Code {
	out := make(map[string]Code, len(in))
	for _, c := range in {
		out[c.Name] = c
	}
	return out
}

func indexLimits(in []RateLimit) map[string]RateLimit {
	out := make(map[string]RateLimit, len(in))
	for _, r := range in {
		if len(r.Methods) == 1 && r.Methods[0] == ScopeAllMethods {
			continue
		}
		for _, m := range r.Methods {
			out[m] = r
		}
	}
	return out
}

// catchAll returns the "*" budget. The generator refuses to project a contract without exactly one, so it is
// always found; returning the zero RateLimit would silently disable the per-sender request budget.
func catchAll(in []RateLimit) RateLimit {
	for _, r := range in {
		if len(r.Methods) == 1 && r.Methods[0] == ScopeAllMethods {
			return r
		}
	}
	panic("contract: the projection has no catch-all rate budget")
}

func indexSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}

func sortedNames(in []Method) []string {
	out := make([]string, len(in))
	for i, m := range in {
		out[i] = m.Name
	}
	sort.Strings(out)
	return out
}

func sortedPrivileged(in []Method) []string {
	var out []string
	for _, m := range in {
		if m.Privileged {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	return out
}

// MethodNames returns every dispatchable method, sorted.
func MethodNames() []string { return append([]string(nil), names...) }

// PrivilegedMethodNames returns the methods that may be refused to a sender other than the pinned extension.
func PrivilegedMethodNames() []string { return append([]string(nil), privName...) }

// LookupMethod finds a method by its dotted name. An unknown name is UnknownMethod, never a fallthrough.
func LookupMethod(name string) (Method, bool) {
	m, ok := methods[name]
	return m, ok
}

// LookupCode finds an error code by name.
func LookupCode(name string) (Code, bool) {
	c, ok := codes[name]
	return c, ok
}

// RateLimitFor returns the named per-sender budget of a method. An unpublished method has no budget: the
// dispatcher refuses it before a counter is touched.
func RateLimitFor(name string) (RateLimit, bool) {
	r, ok := limits[name]
	return r, ok
}

// CatchAllRateLimit is the "*" budget every request counts against in addition to its named one.
func CatchAllRateLimit() RateLimit { return global }

// IsCapability reports whether a capability name is in the registry.
func IsCapability(name string) bool { return caps[name] }

// CapabilityNames returns the registry in declaration order.
func CapabilityNames() []string { return append([]string(nil), Capabilities...) }
