package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contractgen"
)

// TestProjectionMatchesTheContractData is the row this package exists for: the numbers the Go binary enforces
// are regenerated from packages/schema/protocol and compared to the committed file. A schema edit that was
// never projected fails `go test ./...`, and `node packages/schema/scripts/verify.mjs` keeps failing the
// contract layer beside it.
func TestProjectionMatchesTheContractData(t *testing.T) {
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	want, err := contractgen.Source(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("contract_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("contract_gen.go is not the current projection of packages/schema/protocol — run `go generate ./...` in connector/")
	}
}

func TestDedupeBoundsAndIdempotencyAreProjectedFromContractData(t *testing.T) {
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	var framing struct {
		DedupeWindow struct {
			WindowSeconds int `json:"windowSeconds"`
			MaxEntries    int `json:"maxEntries"`
			MaxBytes      int `json:"maxBytes"`
		} `json:"dedupeWindow"`
	}
	readContractJSON(t, filepath.Join(root, "packages", "schema", "protocol", "framing.json"), &framing)
	bounds := framing.DedupeWindow
	if bounds.WindowSeconds <= 0 || bounds.MaxEntries <= 0 || bounds.MaxBytes <= 0 {
		t.Fatalf("dedupe window is not positively bounded: %+v", bounds)
	}
	if DedupeWindowSeconds != bounds.WindowSeconds || DedupeMaxEntries != bounds.MaxEntries || DedupeMaxBytes != bounds.MaxBytes {
		t.Fatalf("dedupe bounds differ from contract data: %+v", bounds)
	}
	for _, m := range Methods {
		var doc struct {
			Idempotent bool `json:"idempotent"`
			Replayable bool `json:"replayable"`
		}
		readContractJSON(t, filepath.Join(root, "packages", "schema", "protocol", "commands", m.Name+".json"), &doc)
		if m.Idempotent != doc.Idempotent || m.Replayable != doc.Replayable {
			t.Errorf("%s projection lost its execution flags", m.Name)
		}
	}
}

// Mutations are made only in temporary fixtures, never in the normative source directory.
func TestProjectionRejectsInvalidDedupeBounds(t *testing.T) {
	fixture := contractFixture(t)
	file := filepath.Join(fixture, "packages", "schema", "protocol", "framing.json")
	var original map[string]json.RawMessage
	readContractJSON(t, file, &original)
	var bounds map[string]json.RawMessage
	if err := json.Unmarshal(original["dedupeWindow"], &bounds); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"windowSeconds", "maxEntries", "maxBytes"} {
		for _, invalid := range []string{"0", "-1", "1.5", `"300"`, "null", ""} {
			t.Run(name+"/"+invalid, func(t *testing.T) {
				mutated := make(map[string]json.RawMessage, len(bounds))
				for k, v := range bounds {
					mutated[k] = v
				}
				if invalid == "" {
					delete(mutated, name)
				} else {
					mutated[name] = json.RawMessage(invalid)
				}
				original["dedupeWindow"] = contractJSON(t, mutated)
				writeContractJSON(t, file, original)
				if _, err := contractgen.Source(fixture); err == nil || !strings.Contains(err.Error(), "dedupeWindow") {
					t.Fatalf("invalid dedupeWindow.%s=%s projected: %v", name, invalid, err)
				}
			})
		}
	}
	delete(original, "dedupeWindow")
	writeContractJSON(t, file, original)
	if _, err := contractgen.Source(fixture); err == nil {
		t.Fatal("missing dedupeWindow projected as an unbounded/disabled cache")
	}
}

func TestValidDedupeAndIdempotencyMutationsChangeTheProjection(t *testing.T) {
	fixture := contractFixture(t)
	before, err := contractgen.Source(fixture)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(fixture, "packages", "schema", "protocol", "framing.json")
	var doc map[string]json.RawMessage
	readContractJSON(t, file, &doc)
	doc["dedupeWindow"] = contractJSON(t, map[string]int{"windowSeconds": 301, "maxEntries": 257, "maxBytes": 8388609})
	writeContractJSON(t, file, doc)
	command := filepath.Join(fixture, "packages", "schema", "protocol", "commands", "profile.import.json")
	var method map[string]json.RawMessage
	readContractJSON(t, command, &method)
	method["idempotent"] = json.RawMessage("false")
	writeContractJSON(t, command, method)
	after, err := contractgen.Source(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Fatal("positive bounds/idempotency mutation left the projection unchanged")
	}
	for _, want := range []string{"DedupeWindowSeconds", "301", "DedupeMaxEntries", "257", "DedupeMaxBytes", "8388609", "Idempotent: false"} {
		if !bytes.Contains(after, []byte(want)) {
			t.Errorf("mutated projection lost %q", want)
		}
	}
}

func contractFixture(t *testing.T) string {
	t.Helper()
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	source := filepath.Join(root, "packages", "schema")
	target := filepath.Join(fixture, "packages", "schema")
	if err := os.CopyFS(filepath.Join(target, "protocol"), os.DirFS(filepath.Join(source, "protocol"))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "examples"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"envelope-keys.schema.json", filepath.Join("examples", "envelope-keys.nexora.json")} {
		raw, err := os.ReadFile(filepath.Join(source, rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, rel), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func readContractJSON(t *testing.T, file string, dst any) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatal(err)
	}
}

func contractJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeContractJSON(t *testing.T, file string, value any) {
	t.Helper()
	if err := os.WriteFile(file, contractJSON(t, value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSurfaceIsTheClosedEighteen(t *testing.T) {
	if len(Methods) != 18 {
		t.Errorf("the allowlist publishes %d methods, and 18 is what docs/architecture/26-command-surface.md names", len(Methods))
	}
	var beforeHello, privileged, nonReplayable int
	for _, m := range Methods {
		if m.AvailableBeforeHello {
			beforeHello++
			if m.Name != "connector.hello" {
				t.Errorf("%s is dispatchable before negotiation", m.Name)
			}
		}
		if m.Privileged {
			privileged++
		}
		if !m.Replayable {
			nonReplayable++
			if m.Name != "server.test" {
				t.Errorf("%s is declared non-replayable; only a probe may be", m.Name)
			}
		}
		if m.TimeoutMs <= 0 {
			t.Errorf("%s declares no caller deadline", m.Name)
		}
		for _, c := range m.Capabilities {
			if !IsCapability(c) {
				t.Errorf("%s cites capability %s, which the registry does not declare", m.Name, c)
			}
		}
	}
	if beforeHello != 1 {
		t.Errorf("%d methods are dispatchable before hello, want exactly 1", beforeHello)
	}
	if privileged != 10 {
		t.Errorf("%d methods are privileged, want the 10 ADR-0004 names after core.select left the surface", privileged)
	}
	if nonReplayable != 1 {
		t.Errorf("%d methods are non-replayable, want exactly 1", nonReplayable)
	}
	if got := len(PrivilegedMethodNames()); got != privileged {
		t.Errorf("PrivilegedMethodNames returned %d entries, inconsistent with the flags", got)
	}
}

func TestNothingIsDispatchableBeforeNegotiationExceptHello(t *testing.T) {
	for _, name := range []string{"core.start", "config.apply", "server.test", "keystore.purge", "diagnostics.get"} {
		m, ok := LookupMethod(name)
		if !ok {
			t.Fatalf("%s is not in the projection", name)
		}
		if m.AvailableBeforeHello {
			t.Errorf("%s claims to be dispatchable before hello", name)
		}
	}
	if _, ok := LookupMethod("core.snooze"); ok {
		t.Error("core.snooze — the fixture corpus's named negative — is in the projection")
	}
}

func TestTaxonomyIsClosedAndBusyIsTheOnlyRetryableRefusal(t *testing.T) {
	if len(ErrorCodes) != 23 {
		t.Errorf("the taxonomy has %d codes; docs/architecture/25 names 23", len(ErrorCodes))
	}
	busy, ok := LookupCode("Busy")
	if !ok {
		t.Fatal("the taxonomy has no Busy, which every rate limit answers with")
	}
	if !busy.Retryable {
		t.Error("Busy must be retryable — a budget that is not worth retrying is a storage failure")
	}
	quota, ok := LookupCode("QuotaExceeded")
	if !ok {
		t.Fatal("the taxonomy has no QuotaExceeded")
	}
	if quota.Retryable {
		t.Error("QuotaExceeded is a storage-layer code; it must not look like a back-off")
	}
}

func TestBudgetsCoverTheSurfaceTheyClaim(t *testing.T) {
	catch := CatchAllRateLimit()
	if catch.MaxRequests <= 0 || catch.WindowSeconds <= 0 {
		t.Fatalf("the catch-all budget is empty: %+v", catch)
	}
	for _, name := range []string{"subscription.refresh", "server.test", "config.apply", "diagnostics.get"} {
		r, ok := RateLimitFor(name)
		if !ok {
			t.Errorf("%s has no named budget", name)
			continue
		}
		m, _ := LookupMethod(name)
		if m.TimeoutMs > r.WindowSeconds*1000 && r.MaxRequests == 1 {
			t.Errorf("%s: a %d ms budget window cannot bound a %d ms call", name, r.WindowSeconds*1000, m.TimeoutMs)
		}
	}
	if _, ok := RateLimitFor("core.snooze"); ok {
		t.Error("an unpublished method has a budget; the dispatcher refuses it before any counter is touched")
	}
}

func TestTransportNumbersAreTheOnesTheDocumentPrints(t *testing.T) {
	if FrameFormatVersion != 1 || MinPipeVersion != 1 || ProtocolVersion != 1 || MinCompatibleProtocolVersion != 1 {
		t.Errorf("versions are %d/%d/%d/%d", FrameFormatVersion, MinPipeVersion, ProtocolVersion, MinCompatibleProtocolVersion)
	}
	if MinPipeVersion > FrameFormatVersion || MinCompatibleProtocolVersion > ProtocolVersion {
		t.Error("a floor above the version it floors refuses every build that ships it")
	}
	if !(EffectiveCrossBrowserBytes <= HostToExtensionBytes && HostToExtensionBytes <= ExtensionToHostBytes) {
		t.Errorf("limits out of order: %d %d %d", EffectiveCrossBrowserBytes, HostToExtensionBytes, ExtensionToHostBytes)
	}
	if ExtensionToHostBytes != 67108864 || HostToExtensionBytes != 1048576 {
		t.Errorf("the caps are not the browsers': extension-to-host %d want 64 MiB, host-to-extension %d want 1 MiB",
			ExtensionToHostBytes, HostToExtensionBytes)
	}
	if HeartbeatSeconds != 25 || MissedHeartbeatsBeforeDisconnect != 3 {
		t.Errorf("liveness is %ds with %d misses, want 25s with 3", HeartbeatSeconds, MissedHeartbeatsBeforeDisconnect)
	}
	want := []int{1, 2, 4, 8, 16, 32, 60}
	got := BackoffSeconds()
	if len(got) != len(want) {
		t.Fatalf("the ladder is %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the ladder is %v, want %v", got, want)
		}
	}
	got[0] = 999
	if BackoffSeconds()[0] != 1 {
		t.Fatal("BackoffSeconds hands out the internal slice; a caller could rewrite the contract")
	}
}
