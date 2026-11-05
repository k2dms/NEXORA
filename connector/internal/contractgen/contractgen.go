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
	"sort"
	"strconv"
	"strings"
)

const header = `// Code generated from packages/schema/protocol by internal/contractgen. DO NOT EDIT.
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
	SenderPolicy struct {
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
	RequiresHello bool     `json:"requiresHello"`
	TimeoutMs     int      `json:"timeoutMs"`
	Capabilities  []string `json:"capabilities"`
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
	if !codes["Busy"] {
		return nil, fmt.Errorf("the taxonomy has no Busy code, which every rate limit answers with")
	}
	if fr.Limits.EffectiveCrossBrowserBytes.Value > fr.Limits.ExtensionToHostBytes.Value ||
		fr.Limits.ExtensionToHostBytes.Value > fr.Limits.HostToExtensionBytes.Value {
		return nil, fmt.Errorf("byte limits are out of order: effective %d > extension-to-host %d > host-to-extension %d",
			fr.Limits.EffectiveCrossBrowserBytes.Value, fr.Limits.ExtensionToHostBytes.Value, fr.Limits.HostToExtensionBytes.Value)
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
		fmt.Fprintf(&b, "\t\tReplayable: %s, TimeoutMs: %d, Capabilities: %s},\n",
			strconv.FormatBool(doc.Replayable), doc.TimeoutMs, stringSliceLit(doc.Capabilities))
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
		{"ExtensionToHostBytes", strconv.Itoa(fr.Limits.ExtensionToHostBytes.Value)},
		{"HostToExtensionBytes", strconv.Itoa(fr.Limits.HostToExtensionBytes.Value)},
		{"EffectiveCrossBrowserBytes", strconv.Itoa(fr.Limits.EffectiveCrossBrowserBytes.Value)},
		{"MaxConcurrentRequests", strconv.Itoa(fr.Limits.MaxConcurrentRequests.Value)},
		{"MaxInFlightBytes", strconv.Itoa(fr.Limits.MaxInFlightBytes.Value)},
		{"HeartbeatSeconds", strconv.Itoa(fr.Liveness.HeartbeatSeconds)},
		{"MissedHeartbeatsBeforeDisconnect", strconv.Itoa(fr.Liveness.MissedHeartbeatsBeforeDisconnect)},
		{"ReconnectJitterPercent", strconv.Itoa(fr.Reconnect.JitterPercent)},
		{"PortsPerSenderMaxConcurrent", strconv.Itoa(fr.SenderPolicy.PortsPerSender.MaxConcurrent)},
		{"RefusalWindowSeconds", strconv.Itoa(fr.SenderPolicy.Refusals.AmplificationGuard.WindowSeconds)},
		{"RefusalsPerWindowMax", strconv.Itoa(fr.SenderPolicy.Refusals.AmplificationGuard.MaxRefusals)},
	} {
		fmt.Fprintf(&b, "\t%s = %s\n", c.name, c.value)
	}
	fmt.Fprint(&b, ")\n\nvar backoffSeconds = []int{\n")
	for _, s := range fr.Reconnect.BackoffSeconds {
		fmt.Fprintf(&b, "\t%d,\n", s)
	}
	fmt.Fprintf(&b, "}\n\n// BackoffSeconds returns the reconnect ladder %s.\nfunc BackoffSeconds() []int {\n\tout := make([]int, len(backoffSeconds))\n\tcopy(out, backoffSeconds)\n\treturn out\n}\n",
		joinInts(fr.Reconnect.BackoffSeconds))

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
