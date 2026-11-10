// The mirrors of the contract data the pipeline reads. Each struct below is one schema file's closed object: the
// field list is that file's `properties`, and Decode refuses a payload carrying a name the schema does not declare,
// because `additionalProperties: false` is stated by every one of them and the first thing two implementations of
// one contract disagree about is an invented field.
//
// Fields the pipeline never reads are still in these structs, and that is deliberate. `displayName`, `notes`,
// `source`, `subscriptionId`, `createdAt` and `updatedAt` arrive in the same payload and appear in no emitted byte
// (docs/architecture/23-unified-server-model.md §3 and §8; docs/architecture/27-config-compilation-pipeline.md §3.2,
// which makes the clock exclusion structural rather than a name match). A struct carrying only what the compiler
// emits could not tell a field it stripped from a field it was never given, and §3.2's claim is about the first.
package compile

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Decode unmarshals one of the closed objects this file declares, refusing any field the schema does not list.
// The error keeps the package's prefix because the message reaches `config.apply`'s caller: a schema-shaped
// refusal and a wire-shaped one have to be distinguishable in a log line.
func Decode[T any](data []byte) (T, error) {
	var out T
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		var zero T
		return zero, fmt.Errorf("compile: the payload does not match the schema it is read against: %w", err)
	}
	return out, nil
}

// Server mirrors `packages/schema/server.schema.json`. The Xray-shaped names that file avoids — `settings`,
// `streamSettings`, the outbound object itself — are the compiler's to synthesize (docs/architecture/23-unified-server-model.md
// §9); everything here is the model's own vocabulary.
type Server struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Protocol    string `json:"protocol"` // const "vless"; an expansion needs an ADR (doc 23 §10)
	Address     string `json:"address"`
	Port        int    `json:"port"`
	UserIDRef   string `json:"userIdRef"`
	Network     string `json:"network"`
	Security    string `json:"security"`
	// Flow is a pointer because the three canonical shapes of docs/architecture/23-unified-server-model.md §9 show
	// all three states a flow can be in: A emits `"flow": "xtls-rprx-vision"`, B emits `"flow": ""`, and C emits no
	// flow key at all. `server.schema.json` gives the field `"default": ""`, so absent and empty mean the same thing
	// to Core and differ only in the bytes — which is exactly what a golden file compares.
	Flow           *string      `json:"flow"`
	TLS            *TLSSettings `json:"tls"`
	Reality        *Reality     `json:"reality"`
	WS             *WSBlock     `json:"ws"`
	GRPC           *GRPCBlock   `json:"grpc"`
	XHTTP          *XHTTPBlock  `json:"xhttp"`
	H2             *H2Block     `json:"h2"`
	Tag            string       `json:"tag"`
	Notes          string       `json:"notes"`
	Source         string       `json:"source"`
	SubscriptionID string       `json:"subscriptionId"`
	CreatedAt      string       `json:"createdAt"`
	UpdatedAt      string       `json:"updatedAt"`
}

// TLSSettings mirrors `server.schema.json#/$defs/tlsSettings`. `insecure` is a pointer because the schema pins it
// to `const false`: absent is the normal case, `false` is the same normal case spelled out, and `true` is a value
// the model refuses to describe at all (docs/architecture/27-config-compilation-pipeline.md §6).
type TLSSettings struct {
	ServerName  string   `json:"serverName"`
	ALPN        []string `json:"alpn"`
	Fingerprint string   `json:"fingerprint"`
	Insecure    *bool    `json:"insecure"`
}

// Reality mirrors `#/$defs/realitySettings`. `PublicKey` and `ShortID` are public parameters and are never masked
// (docs/architecture/23-unified-server-model.md §9, docs/architecture/32-secrets-keystore-masking.md); there is no
// `PrivateKey` field here because the schema has none, and a payload carrying one is refused by Decode.
type Reality struct {
	PublicKey   string `json:"publicKey"`
	ShortID     string `json:"shortId"`
	SpiderX     string `json:"spiderX"`
	Fingerprint string `json:"fingerprint"`
	ServerName  string `json:"serverName"`
}

type WSBlock struct {
	Path        string   `json:"path"`
	Host        string   `json:"host"`
	HeadersRef  string   `json:"headersRef"`
	HeadersKeys []string `json:"headersKeys,omitempty"`
}

type GRPCBlock struct {
	ServiceName string   `json:"serviceName"`
	MultiMode   bool     `json:"multiMode"`
	Authority   string   `json:"authority"`
	HeadersRef  string   `json:"headersRef"`
	HeadersKeys []string `json:"headersKeys,omitempty"`
}

type XHTTPBlock struct {
	Mode  string `json:"mode"`
	Path  string `json:"path"`
	Host  string `json:"host"`
	Extra string `json:"extra"`
}

type H2Block struct {
	Path string `json:"path"`
	Host string `json:"host"`
}

// Profile mirrors `packages/schema/connector-profile.schema.json`. `Enabled` is a pointer for the same reason the
// schema's default matters: an absent field means true, and a struct field zero-value would mean the profile is off.
type Profile struct {
	ID              string        `json:"id"`
	DisplayName     string        `json:"displayName"`
	Kind            string        `json:"kind"`
	ServerIDs       []string      `json:"serverIds"`
	DefaultServerID string        `json:"defaultServerId"`
	Selection       Selection     `json:"selection"`
	Failover        Failover      `json:"failover"`
	FailurePolicy   ProfilePolicy `json:"failurePolicy"`
	RoutingPolicyID string        `json:"routingPolicyId"`
	Enabled         *bool         `json:"enabled"`
	CreatedAt       string        `json:"createdAt"`
	UpdatedAt       string        `json:"updatedAt"`
}

// Selection is the browser plane's own choice about servers; the compiled config carries one inbound per tag and
// says nothing about which is picked. `Mode` is read here only for the membership check on `defaultServerId`.
type Selection struct {
	Mode          string `json:"mode"`
	StickySeconds int    `json:"stickySeconds"`
}

// Failover belongs to the HealthProber, which does not exist yet. It is carried because the payload carries it.
type Failover struct {
	Enabled                bool `json:"enabled"`
	CooldownSeconds        int  `json:"cooldownSeconds"`
	MaxConsecutiveFailures int  `json:"maxConsecutiveFailures"`
	ProbeIntervalSeconds   int  `json:"probeIntervalSeconds"`
}

// ProfilePolicy mirrors `packages/schema/failure-policy.schema.json#/$defs/ProfilePolicy`.
type ProfilePolicy struct {
	CoreDeath struct {
		Action       string `json:"action"`
		ConfirmToken string `json:"confirmToken"`
	} `json:"coreDeath"`
}

// RoutingPolicy mirrors `packages/schema/routing-policy.schema.json`. The browser plane stays in the browser
// (docs/architecture/23-unified-server-model.md §8) and is read here only to refuse a policy whose rules the
// Connector would otherwise silently ignore; the core plane is what becomes `routing`.
type RoutingPolicy struct {
	ID           string       `json:"id"`
	DisplayName  string       `json:"displayName"`
	BrowserPlane BrowserPlane `json:"browserPlane"`
	CorePlane    CorePlane    `json:"corePlane"`
}

type BrowserPlane struct {
	DefaultProfileName string        `json:"defaultProfileName"`
	Rules              []BrowserRule `json:"rules"`
}

type BrowserRule struct {
	Condition   Condition `json:"condition"`
	ProfileName string    `json:"profileName"`
	Note        string    `json:"note"`
}

// Condition mirrors `#/$defs/condition`'s eight `oneOf` branches as one flat struct: the branches differ only by
// `conditionType` and by which of these names they allow, and `additionalProperties: false` on each branch means a
// condition carrying two branches' fields is not one of the twelve.
type Condition struct {
	ConditionType string `json:"conditionType"`
	Pattern       string `json:"pattern"`
	IP            string `json:"ip"`
	PrefixLength  int    `json:"prefixLength"`
	MinValue      int    `json:"minValue"`
	MaxValue      int    `json:"maxValue"`
	Days          string `json:"days"`
	StartDay      *int   `json:"startDay"`
	EndDay        *int   `json:"endDay"`
	StartHour     int    `json:"startHour"`
	EndHour       int    `json:"endHour"`
}

type CorePlane struct {
	DomainStrategy  string     `json:"domainStrategy"`
	Sniffing        Sniffing   `json:"sniffing"`
	Rules           []CoreRule `json:"rules"`
	Unrepresentable []Report   `json:"unrepresentable"`
}

// Sniffing's `RouteOnly` defaults to true and the one-plane rule requires it (docs/architecture/27-config-
// compilation-pipeline.md §6), so an absent field means true, which is why it is a pointer.
type Sniffing struct {
	Enabled   *bool    `json:"enabled"`
	Protocols []string `json:"protocols"`
	RouteOnly *bool    `json:"routeOnly"`
}

// CoreRule mirrors `#/$defs/coreRule`. Note the schema's field is `outbound` and the emitted Xray key is
// `outboundTag` (docs/architecture/28-routing-model.md §7's rank-5 row): the model's name and Core's name differ by
// design, and the compile step is where one becomes the other.
type CoreRule struct {
	Domain   []string `json:"domain"`
	IP       []string `json:"ip"`
	Port     string   `json:"port"`
	Network  []string `json:"network"`
	Protocol []string `json:"protocol"`
	Invert   *bool    `json:"invert"`
	Outbound string   `json:"outbound"`
}

// Report is the `{source, reason}` pair, declared once here and used twice: at the policy level
// (`routing-policy.schema.json#/properties/corePlane/properties/unrepresentable`) and at the apply level
// (`protocol/commands/config.apply.json#/result/properties/unrepresentable`). The bounds are the schema's and the
// compiler enforces them rather than truncating: a reason that would exceed 200 characters is a compiler message
// nobody asked for.
type Report struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// Settings mirrors `packages/schema/connector-settings.schema.json`: only the halves anything downstream reads —
// `ports` for allocation, `telemetry.logLevel` for the Core log, `core.installRoot` for the one field §3.3 says must
// never reach the bytes, and `core.egressProbe` for the endpoint the HealthProber fetches. The last two are carried
// without reaching the compiled bytes at all: they are read from the settings that were applied, not from what they
// compile to.
type Settings struct {
	SchemaVersion int `json:"schemaVersion"`
	Core          struct {
		AutoStart      bool         `json:"autoStart"`
		InstallChannel string       `json:"installChannel"`
		InstallRoot    *string      `json:"installRoot"`
		EgressProbe    *EgressProbe `json:"egressProbe"`
	} `json:"core"`
	Ports struct {
		Range struct {
			Min int `json:"min"`
			Max int `json:"max"`
		} `json:"range"`
		Host               string `json:"host"`
		PersistAssignments *bool  `json:"persistAssignments"`
	} `json:"ports"`
	Telemetry struct {
		LogLevel                    string   `json:"logLevel"`
		RetentionDays               int      `json:"retentionDays"`
		DiagnosticsIncludeByDefault []string `json:"diagnosticsIncludeByDefault"`
	} `json:"telemetry"`
	Proxy struct {
		FailClosedDefault string `json:"failClosedDefault"`
		NotificationLevel string `json:"notificationLevel"`
	} `json:"proxy"`
}

// EgressProbe mirrors `connector-settings.schema.json#/properties/core/properties/egressProbe`: the one machine-wide
// checkip URL ODEC-42 chose as the egress probe's endpoint. The expected answer is deliberately not a field here —
// the decision puts it in the server document's own `address`, read at probe time, and a copy in settings would be a
// second claim the probe could disagree with.
type EgressProbe struct {
	Endpoint string `json:"endpoint"`
}

// Failure mirrors `packages/schema/failure-policy.schema.json`. The `block` object is stage 4's named input, and the
// compiled document has no position for it or for the restart budget — see Compile's note on stage 4.
type Failure struct {
	Version   int `json:"version"`
	CoreDeath struct {
		Action                string `json:"action"`
		RestartAttempts       int    `json:"restartAttempts"`
		RestartBackoffSeconds []int  `json:"restartBackoffSeconds"`
		// StabilityWindowSeconds is ODEC-38's field: the denominator the ladder's budget had been missing. The
		// supervisor reads it as Death.Stability; this mirror carries it so an intent document the schema admits
		// is not refused as an invented field.
		StabilityWindowSeconds int  `json:"stabilityWindowSeconds"`
		Notify                 bool `json:"notify"`
	} `json:"coreDeath"`
	ServiceTeardown struct {
		GraceSeconds   int    `json:"graceSeconds"`
		Then           string `json:"then"`
		HardCapSeconds int    `json:"hardCapSeconds"`
	} `json:"serviceTeardown"`
	ExtensionLifecycle struct {
		OnDisabled     string `json:"onDisabled"`
		OnUpdated      string `json:"onUpdated"`
		OnBrowserStart string `json:"onBrowserStart"`
	} `json:"extensionLifecycle"`
	Block struct {
		Scheme string `json:"scheme"`
		Host   string `json:"host"`
		Range  struct {
			Min int `json:"min"`
			Max int `json:"max"`
		} `json:"portRange"`
		AssertClosed *bool `json:"assertClosed"`
	} `json:"block"`
	ForbiddenOutcomes []string `json:"forbiddenOutcomes"`
}
