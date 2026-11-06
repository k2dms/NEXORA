package contract

import (
	"os"
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

func TestSurfaceIsTheClosedNineteen(t *testing.T) {
	if len(Methods) != 19 {
		t.Errorf("the allowlist publishes %d methods, and 19 is what docs/architecture/26-command-surface.md names", len(Methods))
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
	if privileged != 11 {
		t.Errorf("%d methods are privileged, want the 11 ADR-0004 names", privileged)
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
	if !(EffectiveCrossBrowserBytes <= ExtensionToHostBytes && ExtensionToHostBytes <= HostToExtensionBytes) {
		t.Errorf("limits out of order: %d %d %d", EffectiveCrossBrowserBytes, ExtensionToHostBytes, HostToExtensionBytes)
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
