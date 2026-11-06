// Code generated from packages/schema/protocol and the envelope-key registry by internal/contractgen. DO NOT EDIT.
//
// Regenerate with `go generate ./...` from the connector module root. The drift test in
// contract_test.go fails the build when this file and the schema directory disagree, which is what keeps
// docs/architecture/20-target-repository-layout.md §3's "no second copy of any allowlist" honest: this is a
// projection of the contract, not a second source of it.

package contract

var Methods = []Method{
	{Name: "connector.hello", Class: "read", AvailableBeforeHello: true, Privileged: false,
		Replayable: true, TimeoutMs: 5000, Capabilities: nil},
	{Name: "connector.version", Class: "read", AvailableBeforeHello: false, Privileged: false,
		Replayable: true, TimeoutMs: 2000, Capabilities: nil},
	{Name: "connector.status", Class: "read", AvailableBeforeHello: false, Privileged: false,
		Replayable: true, TimeoutMs: 3000, Capabilities: nil},
	{Name: "connector.health", Class: "read", AvailableBeforeHello: false, Privileged: false,
		Replayable: true, TimeoutMs: 5000, Capabilities: []string{"health.probes"}},
	{Name: "connector.events.subscribe", Class: "mutate", AvailableBeforeHello: false, Privileged: false,
		Replayable: true, TimeoutMs: 5000, Capabilities: nil},
	{Name: "core.start", Class: "converge", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 15000, Capabilities: []string{"integrity.pinned-core"}},
	{Name: "core.stop", Class: "converge", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 10000, Capabilities: nil},
	{Name: "core.restart", Class: "converge", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 20000, Capabilities: []string{"integrity.pinned-core"}},
	{Name: "core.status", Class: "read", AvailableBeforeHello: false, Privileged: false,
		Replayable: true, TimeoutMs: 3000, Capabilities: nil},
	{Name: "core.select", Class: "mutate", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 3000, Capabilities: nil},
	{Name: "config.apply", Class: "replace", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 30000, Capabilities: nil},
	{Name: "config.snapshot", Class: "read", AvailableBeforeHello: false, Privileged: false,
		Replayable: true, TimeoutMs: 5000, Capabilities: []string{"config.snapshot"}},
	{Name: "profile.import", Class: "replace", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 60000, Capabilities: []string{"chunked.import"}},
	{Name: "profile.remove", Class: "mutate", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 10000, Capabilities: nil},
	{Name: "profile.validate", Class: "read", AvailableBeforeHello: false, Privileged: false,
		Replayable: true, TimeoutMs: 10000, Capabilities: nil},
	{Name: "subscription.refresh", Class: "mutate", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 120000, Capabilities: []string{"chunked.import"}},
	{Name: "server.test", Class: "probe", AvailableBeforeHello: false, Privileged: true,
		Replayable: false, TimeoutMs: 30000, Capabilities: []string{"health.probes"}},
	{Name: "diagnostics.get", Class: "read", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 15000, Capabilities: []string{"diagnostics.redacted"}},
	{Name: "keystore.purge", Class: "mutate", AvailableBeforeHello: false, Privileged: true,
		Replayable: true, TimeoutMs: 10000, Capabilities: nil},
}

var ErrorCodes = []Code{
	{Name: "BadRequest", Layer: "transport", Retryable: false},
	{Name: "UnknownMethod", Layer: "transport", Retryable: false},
	{Name: "SchemaMismatch", Layer: "validation", Retryable: false},
	{Name: "VersionMismatch", Layer: "validation", Retryable: false},
	{Name: "UnauthorizedExtension", Layer: "security", Retryable: false},
	{Name: "InvalidProfile", Layer: "validation", Retryable: false},
	{Name: "UnsupportedCombination", Layer: "validation", Retryable: false},
	{Name: "SecretRedacted", Layer: "security", Retryable: false},
	{Name: "QuotaExceeded", Layer: "storage", Retryable: false},
	{Name: "CoreNotRunning", Layer: "state", Retryable: false},
	{Name: "CoreAlreadyRunning", Layer: "state", Retryable: false},
	{Name: "CoreExited", Layer: "state", Retryable: false},
	{Name: "ConfigApplyFailed", Layer: "state", Retryable: false},
	{Name: "HealthCheckFailed", Layer: "network", Retryable: true},
	{Name: "HandshakeFailed", Layer: "network", Retryable: true},
	{Name: "AuthFailed", Layer: "network", Retryable: false},
	{Name: "NetworkUnreachable", Layer: "network", Retryable: true},
	{Name: "Timeout", Layer: "transport", Retryable: true},
	{Name: "IntegrityMismatch", Layer: "security", Retryable: false},
	{Name: "DiskFull", Layer: "storage", Retryable: false},
	{Name: "KeystoreUnavailable", Layer: "security", Retryable: false},
	{Name: "Busy", Layer: "state", Retryable: true},
	{Name: "Internal", Layer: "state", Retryable: false},
}

var Capabilities = []string{
	"vless.tcp.tls.vision",
	"vless.tcp.tls",
	"vless.ws.tls",
	"vless.grpc.tls",
	"vless.xhttp.tls",
	"vless.reality",
	"chunked.import",
	"health.probes",
	"failure.block-loopback",
	"config.snapshot",
	"keystore.dpapi",
	"diagnostics.redacted",
	"integrity.pinned-core",
}

var RateLimits = []RateLimit{
	{Methods: []string{"subscription.refresh"}, WindowSeconds: 60, MaxRequests: 3},
	{Methods: []string{"server.test"}, WindowSeconds: 10, MaxRequests: 8},
	{Methods: []string{"config.apply", "core.start", "core.stop", "core.restart", "core.select"}, WindowSeconds: 20, MaxRequests: 4},
	{Methods: []string{"profile.import", "profile.remove", "keystore.purge"}, WindowSeconds: 60, MaxRequests: 2},
	{Methods: []string{"diagnostics.get"}, WindowSeconds: 120, MaxRequests: 1},
	{Methods: []string{ScopeAllMethods}, WindowSeconds: 1, MaxRequests: 20},
}

const (
	ProtocolVersion                  = 1
	MinCompatibleProtocolVersion     = 1
	FrameFormatVersion               = 1
	MinPipeVersion                   = 1
	ExtensionToHostBytes             = 1048576
	HostToExtensionBytes             = 1073741824
	EffectiveCrossBrowserBytes       = 1048576
	MaxConcurrentRequests            = 64
	MaxInFlightBytes                 = 8388608
	HeartbeatSeconds                 = 25
	MissedHeartbeatsBeforeDisconnect = 3
	ReconnectJitterPercent           = 30
	PortsPerSenderMaxConcurrent      = 2
	RefusalWindowSeconds             = 10
	RefusalsPerWindowMax             = 10
)

var backoffSeconds = []int{
	1,
	2,
	4,
	8,
	16,
	32,
	60,
}

// BackoffSeconds returns the reconnect ladder 1, 2, 4, 8, 16, 32, 60.
func BackoffSeconds() []int {
	out := make([]int, len(backoffSeconds))
	copy(out, backoffSeconds)
	return out
}

const (
	SecretScheme               = "keystore://"
	SecretReferencePattern     = "^keystore://[A-Za-z0-9._:/-]{1,96}$"
	SecretReferenceShape       = "^keystore://(server|subscription|profile|sync|host)/[0-9a-f][0-9a-f-]{6,61}[0-9a-f]/(user-id|headers|url|token)$"
	SecretFingerprintPattern   = "^fp-[0-9a-f]{12}$"
	KeystoreValueMaxChars      = 4096
	KeystoreValueMaxProperties = 32
	KeystoreMaxCredentials     = 512
)

// SecretField is one member of the closed field vocabulary: which field of an entity a reference
// names, and whether the secret behind it is one value or a map.
type SecretField struct {
	Name       string
	ValueShape string
}

var SecretEntities = []string{
	"server",
	"subscription",
	"profile",
	"sync",
	"host",
}

var SecretFields = []SecretField{
	{Name: "user-id", ValueShape: "string"},
	{Name: "headers", ValueShape: "object"},
	{Name: "url", ValueShape: "string"},
	{Name: "token", ValueShape: "string"},
}
