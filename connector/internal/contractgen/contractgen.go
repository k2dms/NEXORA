// Package contractgen projects the normative protocol data in packages/schema/protocol into Go source.
//
// The schema directory stays the single source of truth (docs/architecture/20-target-repository-layout.md
// §3: `packages/schema/` MUST NOT have a second copy of any allowlist or entity shape). The Go module needs
// the same numbers at run time, so the projection is generated, committed, and re-checked by a test that
// regenerates it and compares bytes — silent drift between the contract and the binary fails the build.
package contractgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// hostNameShape is the native-messaging host name rule both browser families accept: lowercase alphanumerics
// and underscores in dot-separated groups, so no leading, trailing or doubled dot. The generator refuses a
// contract name outside it because the failure it prevents is an install no browser can resolve.
var hostNameShape = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)

const header = `// Code generated from packages/schema/protocol and the envelope-key registry by internal/contractgen. DO NOT EDIT.
//
// Regenerate with ` + "`go generate ./...`" + ` from the connector module root. The drift test in
// contract_test.go fails the build when this file and the schema directory disagree, which is what keeps
// docs/architecture/20-target-repository-layout.md §3's "no second copy of any allowlist" honest: this is a
// projection of the contract, not a second source of it.

package contract

var Methods = []Method{
`

type limitValue struct {
	Value int `json:"value"`
}

type framingDoc struct {
	ProtocolVersion              int `json:"protocolVersion"`
	MinCompatibleProtocolVersion int `json:"minCompatibleProtocolVersion"`
	FrameFormatVersion           int `json:"frameFormatVersion"`
	MinPipeVersion               int `json:"minPipeVersion"`
	Limits                       struct {
		ExtensionToHostBytes       limitValue `json:"extensionToHostBytes"`
		HostToExtensionBytes       limitValue `json:"hostToExtensionBytes"`
		EffectiveCrossBrowserBytes limitValue `json:"effectiveCrossBrowserBytes"`
		MaxConcurrentRequests      limitValue `json:"maxConcurrentRequests"`
		MaxInFlightBytes           limitValue `json:"maxInFlightBytes"`
	} `json:"limits"`
	Liveness struct {
		HeartbeatSeconds                 int `json:"heartbeatSeconds"`
		MissedHeartbeatsBeforeDisconnect int `json:"missedHeartbeatsBeforeDisconnect"`
	} `json:"liveness"`
	Reconnect struct {
		BackoffSeconds []int `json:"backoffSeconds"`
		JitterPercent  int   `json:"jitterPercent"`
	} `json:"reconnect"`
	DedupeWindow struct {
		WindowSeconds int `json:"windowSeconds"`
		MaxEntries    int `json:"maxEntries"`
		MaxBytes      int `json:"maxBytes"`
	} `json:"dedupeWindow"`
	SenderPolicy struct {
		Identity struct {
			HostName      string `json:"hostName"`
			AllowlistFile string `json:"allowlistFile"`
		} `json:"identity"`
		PrivilegedMethods struct {
			Names []string `json:"names"`
		} `json:"privilegedMethods"`
		RateLimits []struct {
			Scope         []string `json:"scope"`
			WindowSeconds int      `json:"windowSeconds"`
			MaxRequests   int      `json:"maxRequests"`
			OnExceed      string   `json:"onExceed"`
		} `json:"rateLimits"`
		Refusals struct {
			AmplificationGuard struct {
				WindowSeconds int `json:"windowSeconds"`
				MaxRefusals   int `json:"maxRefusals"`
			} `json:"amplificationGuard"`
		} `json:"refusals"`
		PortsPerSender struct {
			MaxConcurrent int `json:"maxConcurrent"`
		} `json:"portsPerSender"`
	} `json:"senderPolicy"`
}

type allowlistDoc struct {
	ProtocolVersion int `json:"protocolVersion"`
	Commands        []struct {
		Name                 string `json:"name"`
		Class                string `json:"class"`
		AvailableBeforeHello bool   `json:"availableBeforeHello"`
		Privileged           bool   `json:"privileged"`
	} `json:"commands"`
}

type taxonomyDoc struct {
	ProtocolVersion int `json:"protocolVersion"`
	Codes           []struct {
		Code      string `json:"code"`
		Layer     string `json:"layer"`
		Retryable bool   `json:"retryable"`
	} `json:"codes"`
}

type compatDoc struct {
	ProtocolVersion int `json:"protocolVersion"`
	Capabilities    []struct {
		Name string `json:"name"`
	} `json:"capabilities"`
}

type commandDoc struct {
	Name          string   `json:"name"`
	Class         string   `json:"class"`
	Replayable    bool     `json:"replayable"`
	Idempotent    bool     `json:"idempotent"`
	RequiresHello bool     `json:"requiresHello"`
	TimeoutMs     int      `json:"timeoutMs"`
	Capabilities  []string `json:"capabilities"`
}

// ingestDoc is the half of profile.import.json that is not a method flag: the one door through which a secret
// value may arrive (docs/architecture/32-secrets-keystore-masking.md §6), whose closed enums are the entity and
// field lists the keystore reference vocabulary must agree with.
type ingestDoc struct {
	Args struct {
		Properties struct {
			Credentials struct {
				MaxItems int `json:"maxItems"`
				Items    struct {
					Properties struct {
						Entity struct {
							Enum []string `json:"enum"`
						} `json:"entity"`
						EntityID struct {
							Pattern string `json:"pattern"`
						} `json:"entityId"`
						Field struct {
							Enum []string `json:"enum"`
						} `json:"field"`
						Value struct {
							MaxLength     int `json:"maxLength"`
							MaxProperties int `json:"maxProperties"`
						} `json:"value"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"credentials"`
		} `json:"properties"`
	} `json:"args"`
}

// secretRefsDoc is the shape of the const floor strings in
// packages/schema/envelope-keys.schema.json#/properties/secretReferences.
type secretRefsDoc struct {
	Properties struct {
		SecretReferences struct {
			Properties struct {
				Scheme struct {
					Const string `json:"const"`
				} `json:"scheme"`
				Pattern struct {
					Const string `json:"const"`
				} `json:"pattern"`
				Shape struct {
					Const string `json:"const"`
				} `json:"shape"`
				Fingerprint struct {
					Properties struct {
						Pattern struct {
							Const string `json:"const"`
						} `json:"pattern"`
					} `json:"properties"`
				} `json:"fingerprint"`
			} `json:"properties"`
		} `json:"secretReferences"`
	} `json:"properties"`
}

// secretRegistry is the recorded instance of the same block — the entity list and the field vocabulary with
// their value shapes. The schema declares the floor, the example carries the lists, and both are contract data:
// scripts/verify.mjs already fails when the two separate, and this generator refuses to project them apart.
type secretRegistry struct {
	SecretReferences struct {
		Entities []string `json:"entities"`
		Fields   []struct {
			Name       string `json:"name"`
			ValueShape string `json:"valueShape"`
		} `json:"fields"`
	} `json:"secretReferences"`
}

// Source reads the protocol data under repoRoot and returns the generated Go source for package contract.
func Source(repoRoot string) ([]byte, error) {
	protocol := filepath.Join(repoRoot, "packages", "schema", "protocol")

	var fr framingDoc
	if err := readJSON(filepath.Join(protocol, "framing.json"), &fr); err != nil {
		return nil, err
	}
	var al allowlistDoc
	if err := readJSON(filepath.Join(protocol, "command-allowlist.json"), &al); err != nil {
		return nil, err
	}
	var tx taxonomyDoc
	if err := readJSON(filepath.Join(protocol, "error-taxonomy.json"), &tx); err != nil {
		return nil, err
	}
	var cr compatDoc
	if err := readJSON(filepath.Join(protocol, "compatibility-rules.json"), &cr); err != nil {
		return nil, err
	}
	var ing ingestDoc
	if err := readJSON(filepath.Join(protocol, "commands", "profile.import.json"), &ing); err != nil {
		return nil, err
	}
	schemaDir := filepath.Join(repoRoot, "packages", "schema")
	var sr secretRefsDoc
	if err := readJSON(filepath.Join(schemaDir, "envelope-keys.schema.json"), &sr); err != nil {
		return nil, err
	}
	var reg secretRegistry
	if err := readJSON(filepath.Join(schemaDir, "examples", "envelope-keys.nexora.json"), &reg); err != nil {
		return nil, err
	}

	docs := map[string]commandDoc{}
	dir := filepath.Join(protocol, "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var c commandDoc
		if err := readJSON(filepath.Join(dir, e.Name()), &c); err != nil {
			return nil, err
		}
		docs[c.Name] = c
	}

	registry := map[string]bool{}
	for _, k := range cr.Capabilities {
		registry[k.Name] = true
	}
	codes := map[string]bool{}
	for _, c := range tx.Codes {
		codes[c.Code] = true
	}

	// Generation refuses to project a contract that disagrees with itself. These are the same cross-file
	// facts packages/schema/scripts/verify.mjs asserts on the Node side, re-asserted here so the Go binary
	// cannot be built from a half-edited schema directory.
	for _, pair := range []struct {
		what string
		a, b int
	}{
		{"framing.json and command-allowlist.json", fr.ProtocolVersion, al.ProtocolVersion},
		{"command-allowlist.json and error-taxonomy.json", al.ProtocolVersion, tx.ProtocolVersion},
		{"error-taxonomy.json and compatibility-rules.json", tx.ProtocolVersion, cr.ProtocolVersion},
	} {
		if pair.a != pair.b {
			return nil, fmt.Errorf("protocolVersion disagrees between %s: %d vs %d", pair.what, pair.a, pair.b)
		}
	}
	for name, value := range map[string]int{
		"windowSeconds": fr.DedupeWindow.WindowSeconds,
		"maxEntries":    fr.DedupeWindow.MaxEntries,
		"maxBytes":      fr.DedupeWindow.MaxBytes,
	} {
		if value <= 0 {
			return nil, fmt.Errorf("framing.json dedupeWindow.%s must be a positive integer, got %d", name, value)
		}
	}
	if !codes["Busy"] {
		return nil, fmt.Errorf("the taxonomy has no Busy code, which every rate limit answers with")
	}
	// The directional caps are the platforms' numbers, and the binding direction is host-to-extension: all three
	// browser families cap what the host writes back at 1 MB, while only Chrome caps what the extension writes, at
	// 64 MiB (docs/architecture/25-native-messaging-protocol.md §2.1). So the effective ceiling is the smallest of
	// the three and the cross-browser design must live under it in both directions.
	if fr.Limits.EffectiveCrossBrowserBytes.Value > fr.Limits.HostToExtensionBytes.Value ||
		fr.Limits.HostToExtensionBytes.Value > fr.Limits.ExtensionToHostBytes.Value {
		return nil, fmt.Errorf("byte limits are out of order, want effective %d <= host-to-extension %d <= extension-to-host %d",
			fr.Limits.EffectiveCrossBrowserBytes.Value, fr.Limits.HostToExtensionBytes.Value, fr.Limits.ExtensionToHostBytes.Value)
	}

	// The two senderPolicy.identity strings the installer and the host both read. They are checked here rather
	// than at run time because a name no browser accepts is a broken install discovered by a user, and the
	// shape rule is the platforms': lowercase alphanumerics, underscores and dots, no empty or doubled dot.
	if !hostNameShape.MatchString(fr.SenderPolicy.Identity.HostName) {
		return nil, fmt.Errorf("senderPolicy.identity.hostName %q is not a name the browser families accept (lowercase alphanumerics, underscores and dots, no leading, trailing or doubled dot)", fr.SenderPolicy.Identity.HostName)
	}
	if filepath.IsAbs(fr.SenderPolicy.Identity.AllowlistFile) || strings.Contains(fr.SenderPolicy.Identity.AllowlistFile, "..") {
		return nil, fmt.Errorf("senderPolicy.identity.allowlistFile %q must be a relative path under the per-user root", fr.SenderPolicy.Identity.AllowlistFile)
	}

	// The secret-reference vocabulary in four files, re-joined here. docs/architecture/32 §5 makes it one
	// vocabulary on purpose and scripts/verify.mjs fails when the schemas drift apart; the keystore is the first
	// Go consumer, so the same agreement is checked before it is projected rather than after a binary ships with
	// a reference grammar only the Node side enforces.
	srScheme := sr.Properties.SecretReferences.Properties.Scheme.Const
	srPattern := sr.Properties.SecretReferences.Properties.Pattern.Const
	srShape := sr.Properties.SecretReferences.Properties.Shape.Const
	srFingerprint := sr.Properties.SecretReferences.Properties.Fingerprint.Properties.Pattern.Const
	entities := ing.Args.Properties.Credentials.Items.Properties.Entity.Enum
	fields := ing.Args.Properties.Credentials.Items.Properties.Field.Enum
	idPattern := strings.Trim(ing.Args.Properties.Credentials.Items.Properties.EntityID.Pattern, "^$")
	if srScheme == "" || srPattern == "" || srShape == "" || srFingerprint == "" {
		return nil, fmt.Errorf("envelope-keys.schema.json lost one of its secretReferences const strings")
	}
	for name, s := range map[string]string{"pattern": srPattern, "shape": srShape} {
		// Both floors are anchored regexes, so the scheme is what follows the anchor rather than the first bytes.
		if !strings.HasPrefix(strings.TrimPrefix(s, "^"), srScheme) {
			return nil, fmt.Errorf("the reference %s does not begin with the scheme const %q", name, srScheme)
		}
	}
	if len(entities) == 0 || len(fields) == 0 {
		return nil, fmt.Errorf("profile.import.json declares no entity or field enum; the keystore vocabulary is empty")
	}
	if !setEqual(entities, reg.SecretReferences.Entities) {
		return nil, fmt.Errorf("the entity set differs: profile.import accepts %v, the envelope-key registry records %v", entities, reg.SecretReferences.Entities)
	}
	var fieldNames []string
	objectFields := 0
	for _, f := range reg.SecretReferences.Fields {
		fieldNames = append(fieldNames, f.Name)
		if f.ValueShape == "object" {
			objectFields++
		}
	}
	if !setEqual(fields, fieldNames) {
		return nil, fmt.Errorf("the field vocabulary differs: profile.import accepts %v, the envelope-key registry records %v", fields, fieldNames)
	}
	if objectFields != 1 {
		return nil, fmt.Errorf("the field vocabulary records %d object-shaped fields; ODEC-36 is open because of exactly one of them", objectFields)
	}
	if !alternationsIn(srShape, entities) || !alternationsIn(srShape, fields) ||
		!strings.Contains(srShape, idPattern) {
		return nil, fmt.Errorf("the reference shape %q does not carry the closed entity list, the closed field list and the ingest id pattern it is supposed to be built from", srShape)
	}

	var priv []string
	for _, c := range al.Commands {
		if c.Privileged {
			priv = append(priv, c.Name)
		}
	}
	senderPriv := append([]string(nil), fr.SenderPolicy.PrivilegedMethods.Names...)
	sort.Strings(senderPriv)
	sort.Strings(priv)
	if strings.Join(priv, ",") != strings.Join(senderPriv, ",") {
		return nil, fmt.Errorf("the privileged set differs: allowlist flags %v, senderPolicy names %v", priv, senderPriv)
	}

	var b bytes.Buffer
	fmt.Fprint(&b, header)
	published := map[string]bool{}
	for _, c := range al.Commands {
		doc, ok := docs[c.Name]
		if !ok {
			return nil, fmt.Errorf("the allowlist names %s but protocol/commands/ has no such document", c.Name)
		}
		published[c.Name] = true
		if doc.Class != c.Class {
			return nil, fmt.Errorf("%s: class is %q in the allowlist and %q in its command document", c.Name, c.Class, doc.Class)
		}
		if doc.RequiresHello == c.AvailableBeforeHello {
			return nil, fmt.Errorf("%s: availableBeforeHello and requiresHello are not complements", c.Name)
		}
		for _, name := range doc.Capabilities {
			if !registry[name] {
				return nil, fmt.Errorf("%s cites capability %q, which the registry does not declare", c.Name, name)
			}
		}
		fmt.Fprintf(&b, "\t{Name: %s, Class: %s, AvailableBeforeHello: %s, Privileged: %s,\n",
			quote(c.Name), quote(c.Class), strconv.FormatBool(c.AvailableBeforeHello), strconv.FormatBool(c.Privileged))
		fmt.Fprintf(&b, "\t\tReplayable: %s, Idempotent: %s, TimeoutMs: %d, Capabilities: %s},\n",
			strconv.FormatBool(doc.Replayable), strconv.FormatBool(doc.Idempotent), doc.TimeoutMs, stringSliceLit(doc.Capabilities))
	}
	for name := range docs {
		if !published[name] {
			return nil, fmt.Errorf("protocol/commands/%s.json documents a method the allowlist does not publish", name)
		}
	}
	fmt.Fprint(&b, "}\n\nvar ErrorCodes = []Code{\n")
	for _, c := range tx.Codes {
		fmt.Fprintf(&b, "\t{Name: %s, Layer: %s, Retryable: %s},\n", quote(c.Code), quote(c.Layer), strconv.FormatBool(c.Retryable))
	}
	fmt.Fprint(&b, "}\n\nvar Capabilities = []string{\n")
	for _, c := range cr.Capabilities {
		fmt.Fprintf(&b, "\t%s,\n", quote(c.Name))
	}
	fmt.Fprint(&b, "}\n\nvar RateLimits = []RateLimit{\n")
	starred := 0
	for _, r := range fr.SenderPolicy.RateLimits {
		if r.OnExceed != "Busy" {
			return nil, fmt.Errorf("a rate limit answers %q; only retryable Busy is legal", r.OnExceed)
		}
		if len(r.Scope) == 1 && r.Scope[0] == "*" {
			starred++
			fmt.Fprintf(&b, "\t{Methods: []string{ScopeAllMethods}, WindowSeconds: %d, MaxRequests: %d},\n",
				r.WindowSeconds, r.MaxRequests)
			continue
		}
		for _, s := range r.Scope {
			if s == "*" {
				return nil, fmt.Errorf(`"*" scopes the whole surface and cannot appear beside other names`)
			}
			if !published[s] {
				return nil, fmt.Errorf("a rate limit scopes %q, which is not a published command", s)
			}
		}
		fmt.Fprintf(&b, "\t{Methods: %s, WindowSeconds: %d, MaxRequests: %d},\n",
			stringSliceLit(r.Scope), r.WindowSeconds, r.MaxRequests)
	}
	if starred != 1 {
		return nil, fmt.Errorf(`framing.json must declare exactly one "*" catch-all budget; found %d`, starred)
	}
	fmt.Fprint(&b, "}\n\nconst (\n")
	for _, c := range []struct{ name, value string }{
		{"ProtocolVersion", strconv.Itoa(al.ProtocolVersion)},
		{"MinCompatibleProtocolVersion", strconv.Itoa(fr.MinCompatibleProtocolVersion)},
		{"FrameFormatVersion", strconv.Itoa(fr.FrameFormatVersion)},
		{"MinPipeVersion", strconv.Itoa(fr.MinPipeVersion)},
		{"ExtensionToHostBytes", strconv.Itoa(fr.Limits.ExtensionToHostBytes.Value)},
		{"HostToExtensionBytes", strconv.Itoa(fr.Limits.HostToExtensionBytes.Value)},
		{"EffectiveCrossBrowserBytes", strconv.Itoa(fr.Limits.EffectiveCrossBrowserBytes.Value)},
		{"MaxConcurrentRequests", strconv.Itoa(fr.Limits.MaxConcurrentRequests.Value)},
		{"MaxInFlightBytes", strconv.Itoa(fr.Limits.MaxInFlightBytes.Value)},
		{"DedupeWindowSeconds", strconv.Itoa(fr.DedupeWindow.WindowSeconds)},
		{"DedupeMaxEntries", strconv.Itoa(fr.DedupeWindow.MaxEntries)},
		{"DedupeMaxBytes", strconv.Itoa(fr.DedupeWindow.MaxBytes)},
		{"HeartbeatSeconds", strconv.Itoa(fr.Liveness.HeartbeatSeconds)},
		{"MissedHeartbeatsBeforeDisconnect", strconv.Itoa(fr.Liveness.MissedHeartbeatsBeforeDisconnect)},
		{"ReconnectJitterPercent", strconv.Itoa(fr.Reconnect.JitterPercent)},
		{"PortsPerSenderMaxConcurrent", strconv.Itoa(fr.SenderPolicy.PortsPerSender.MaxConcurrent)},
		{"RefusalWindowSeconds", strconv.Itoa(fr.SenderPolicy.Refusals.AmplificationGuard.WindowSeconds)},
		{"RefusalsPerWindowMax", strconv.Itoa(fr.SenderPolicy.Refusals.AmplificationGuard.MaxRefusals)},
		{"HostName", quote(fr.SenderPolicy.Identity.HostName)},
		{"HostAllowlistFile", quote(fr.SenderPolicy.Identity.AllowlistFile)},
	} {
		fmt.Fprintf(&b, "\t%s = %s\n", c.name, c.value)
	}
	fmt.Fprint(&b, ")\n\nvar backoffSeconds = []int{\n")
	for _, s := range fr.Reconnect.BackoffSeconds {
		fmt.Fprintf(&b, "\t%d,\n", s)
	}
	fmt.Fprintf(&b, "}\n\n// BackoffSeconds returns the reconnect ladder %s.\nfunc BackoffSeconds() []int {\n\tout := make([]int, len(backoffSeconds))\n\tcopy(out, backoffSeconds)\n\treturn out\n}\n",
		joinInts(fr.Reconnect.BackoffSeconds))

	fmt.Fprintf(&b, "\nconst (\n\tSecretScheme = %s\n\tSecretReferencePattern = %s\n\tSecretReferenceShape = %s\n\tSecretFingerprintPattern = %s\n",
		quote(srScheme), quote(srPattern), quote(srShape), quote(srFingerprint))
	fmt.Fprintf(&b, "\tKeystoreValueMaxChars = %d\n\tKeystoreValueMaxProperties = %d\n\tKeystoreMaxCredentials = %d\n)\n",
		ing.Args.Properties.Credentials.Items.Properties.Value.MaxLength,
		ing.Args.Properties.Credentials.Items.Properties.Value.MaxProperties,
		ing.Args.Properties.Credentials.MaxItems)
	fmt.Fprint(&b, "\n// SecretField is one member of the closed field vocabulary: which field of an entity a reference\n"+
		"// names, and whether the secret behind it is one value or a map.\n"+
		"type SecretField struct {\n\tName       string\n\tValueShape string\n}\n\n"+
		"var SecretEntities = []string{\n")
	for _, e := range entities {
		fmt.Fprintf(&b, "\t%s,\n", quote(e))
	}
	fmt.Fprint(&b, "}\n\nvar SecretFields = []SecretField{\n")
	for _, f := range reg.SecretReferences.Fields {
		fmt.Fprintf(&b, "\t{Name: %s, ValueShape: %s},\n", quote(f.Name), quote(f.ValueShape))
	}
	fmt.Fprint(&b, "}\n")

	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("the generated source does not parse: %w", err)
	}
	return src, nil
}

// FindRepoRoot walks up from start until it sees packages/schema/protocol/framing.json, so the generator can
// be run from any directory inside the repository.
func FindRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "packages", "schema", "protocol", "framing.json")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no packages/schema/protocol/framing.json above %s — run the generator inside the repository", start)
		}
		dir = parent
	}
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func quote(s string) string { return strconv.Quote(s) }

func setEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

// alternationsIn reports whether every name occurs in the regex as a complete alternative — delimited by `(`, `|`
// or `)` — so a shape that lists the same words in a different order still projects.
func alternationsIn(regex string, names []string) bool {
	for _, n := range names {
		found := false
		for _, delim := range []string{"(" + n + ")", "(" + n + "|", "|" + n + ")", "|" + n + "|"} {
			if strings.Contains(regex, delim) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func stringSliceLit(items []string) string {
	if len(items) == 0 {
		return "nil"
	}
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = quote(s)
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}

func joinInts(items []int) string {
	parts := make([]string, len(items))
	for i, n := range items {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}
