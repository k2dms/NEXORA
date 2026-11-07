package health

// The constructor is where this component's contract is enforced, so it gets its own file: doc 22 §8 says the
// prober answers "did it actually leave this machine?", and a prober wired without an egress endpoint, an inbound
// table or a server lookup cannot answer that at all. Each refusal below is checked for the two things a build log
// has to carry to be useful — what is missing and which document forbids this package from filling it in.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/k2dms/nexora/connector/internal/compile"
)

// validConfig is the smallest wiring New accepts. The endpoint is loopback because nothing here sends a request; the
// address is a TEST-NET-3 literal for the reason probe_test.go gives.
func validConfig() Config {
	return Config{
		Server:   func(context.Context, string) (Server, error) { return Server{}, nil },
		Inbounds: func() []compile.Inbound { return nil },
		Egress:   Egress{URL: "http://127.0.0.1:9/checkip", IP: egressAddress},
	}
}

func TestNewRefusesTheWiringThisPackageIsForbiddenToInvent(t *testing.T) {
	cases := []struct {
		name   string
		tweak  func(*Config)
		wantIn []string
	}{
		{
			name:   "no server lookup",
			tweak:  func(c *Config) { c.Server = nil },
			wantIn: []string{"no server lookup", "doc 22 §8's In"},
		},
		{
			name:   "no inbound table",
			tweak:  func(c *Config) { c.Inbounds = nil },
			wantIn: []string{"no inbound table", "CoreSupervisor"},
		},
		{
			name:   "no egress endpoint",
			tweak:  func(c *Config) { c.Egress.URL = "" },
			wantIn: []string{"no egress endpoint", "ODEC-42"},
		},
		{
			name:   "no expected egress address",
			tweak:  func(c *Config) { c.Egress.IP = "" },
			wantIn: []string{"no expected egress address", "matchesExpectedEgress"},
		},
		{
			name:   "endpoint that is not a fetchable URL",
			tweak:  func(c *Config) { c.Egress.URL = "ftp://api.ipify.org" },
			wantIn: []string{"not a fetchable http or https URL"},
		},
		{
			name:   "endpoint with no host",
			tweak:  func(c *Config) { c.Egress.URL = "http://" },
			wantIn: []string{"not a fetchable http or https URL"},
		},
		{
			name:   "expected address named as a hostname",
			tweak:  func(c *Config) { c.Egress.IP = "api.ipify.org" },
			wantIn: []string{"not an IP literal"},
		},
		{
			// connector-profile.schema.json#/properties/failover bounds 1 to 20; the schema's own floor is not
			// re-stated here, so an out-of-range value has to be the caller's bug and not a silent clamp.
			name:   "streak above the schema ceiling",
			tweak:  func(c *Config) { c.Failover.MaxConsecutiveFailures = 21 },
			wantIn: []string{"maxConsecutiveFailures is 21", "1 to 20"},
		},
		{
			name:   "streak negative",
			tweak:  func(c *Config) { c.Failover.MaxConsecutiveFailures = -1 },
			wantIn: []string{"maxConsecutiveFailures is -1"},
		},
		{
			name:   "cooldown above the schema ceiling",
			tweak:  func(c *Config) { c.Failover.CooldownSeconds = 3601 },
			wantIn: []string{"cooldownSeconds is 3601", "5 to 3600"},
		},
		{
			name:   "interval above the schema ceiling",
			tweak:  func(c *Config) { c.Failover.ProbeIntervalSeconds = 4000 },
			wantIn: []string{"probeIntervalSeconds is 4000", "10 to 3600"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.tweak(&cfg)
			_, err := New(cfg)
			if err == nil {
				t.Fatalf("New accepted %s; the zero value is the answer this package must not invent", tc.name)
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not say %q", err, want)
				}
			}
		})
	}
}

func TestANoFailoverProfileIsWiredAndNotRefused(t *testing.T) {
	// The schema makes the whole `failover` object optional, so the all-zero policy is the common case rather than a
	// misconfiguration — and the constructor has to build it, because refusing it would push the "no policy"
	// question onto every caller that reads a profile without one.
	cfg := validConfig()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New with no failover policy: %v", err)
	}
	if _, ok := p.Health(stateServerID); ok {
		t.Error("a prober that has probed nothing has no status, not an unverified healthy one")
	}
}

func TestNewDefaultsWhatTheCorpusBoundsSomewhereElse(t *testing.T) {
	p, err := New(validConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.cfg.LayerTimeout; got != defaultLayerTimeout {
		t.Errorf("LayerTimeout = %v, want the prober's own %v (the 30000 ms deadline is the command's, not a per-layer number)", got, defaultLayerTimeout)
	}
	if got := p.cfg.RingBytes; got != DefaultRingBytes {
		t.Errorf("RingBytes = %d, want doc 36 §8's health cap %d", got, DefaultRingBytes)
	}
	if p.cfg.Now == nil || p.cfg.LookupHost == nil || p.cfg.DialContext == nil {
		t.Error("New left a seam nil, so the first Probe would panic instead of answering")
	}
}

func TestInboundTagIsTheCompilersOwnDerivationAndNothingElse(t *testing.T) {
	// The prober must not invent a port or a tag: the tag is `compile.ServerTag` prefixed exactly as
	// `inboundDocument` emits it, so a table produced by a different compile of the same server is found here.
	want := "in-" + compile.ServerTag(stateServerID)
	if got := inboundTag(stateServerID); got != want {
		t.Errorf("inboundTag = %q, want the compiler's %q", got, want)
	}
	if !strings.HasPrefix(inboundTag(stateServerID), "in-srv-") {
		t.Errorf("inboundTag = %q, which does not look like the compiler's inbound tag for a server", inboundTag(stateServerID))
	}
}

func TestDetailIsClippedToTheCommandsOwnLengthBound(t *testing.T) {
	long := strings.Repeat("a", 500)
	if got := clip(long); utf8.RuneCountInString(got) != maxDetail {
		t.Errorf("clip left %d characters, want exactly the schema's %d", utf8.RuneCountInString(got), maxDetail)
	}
	if got := clip("short"); got != "short" {
		t.Errorf("clip rewrote a detail that fits: %q", got)
	}

	// A resolver or a TLS error can carry non-ASCII, and a server display name can too. `maxLength` counts
	// characters, and a slice that lands inside a multi-byte rune is not a string the codec can put in a frame.
	mixed := strings.Repeat("ünïcode‑сервер ", 40)
	got := clip(mixed)
	if !utf8.ValidString(got) {
		t.Errorf("clip split a rune, which is a frame the receiver cannot parse: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > maxDetail {
		t.Errorf("clip left %d characters of a non-ASCII detail, over the %d the schema allows", n, maxDetail)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a clipped detail should say it was clipped, got %q", got)
	}
}

func TestEveryRefusalMapsToACodeTheCommandDeclares(t *testing.T) {
	cases := []struct {
		cause error
		code  string
	}{
		{ErrNoServer, "HealthCheckFailed"},
		{ErrNoEgress, "HealthCheckFailed"},
		{ErrBadProbe, "HealthCheckFailed"},
		{ErrCoreNotRunning, "CoreNotRunning"},
		{ErrDeadline, "Timeout"},
	}
	for _, tc := range cases {
		err := refuse("tcp", stateServerID, tc.cause)
		if got := err.Code(); got != tc.code {
			t.Errorf("%v answers %s, want %s", tc.cause, got, tc.code)
		}
		code, ok := err.Taxonomy()
		if !ok {
			t.Errorf("%s is not in the projected taxonomy", tc.code)
			continue
		}
		if code.Name != tc.code {
			t.Errorf("Taxonomy resolved %s, want %s", code.Name, tc.code)
		}
		if !strings.Contains(err.Error(), stateServerID) {
			t.Errorf("refusal %q does not name the server doc 30 §4 requires the answer to name", err)
		}
	}
	if got := codeFor(errors.New("something with no mapping")); got != "Internal" {
		t.Errorf("an unmapped cause answers %s, want Internal, which is the only honest word and the one the init check above cannot bless", got)
	}
}

func TestARefusalNamesTheStepThatStopped(t *testing.T) {
	// `Op` is one of the layer names where a layer is the reason and the command surface's `message` is built from
	// it, so a user reading the frame can tell which of the four questions failed without a second call.
	err := refuse(LayerCoreOutbound, stateServerID, ErrCoreNotRunning)
	if !strings.Contains(err.Error(), LayerCoreOutbound) {
		t.Errorf("refusal %q does not name the layer", err)
	}
	if !errors.Is(err, ErrCoreNotRunning) {
		t.Error("the cause is not unwrappable, so a caller would have to read a sentence to classify it")
	}
	if !errors.As(err, new(*Error)) {
		t.Error("the typed error does not survive errors.As through a wrap")
	}
	wrapped := error(err)
	for i := 0; i < 3; i++ {
		wrapped = fmt.Errorf("probe surface: %w", wrapped)
		if !errors.Is(wrapped, ErrCoreNotRunning) {
			t.Fatalf("a second wrap lost the sentinel at depth %d", i)
		}
	}
	if a := new(Error); !errors.As(err, &a) || a.Op != LayerCoreOutbound {
		t.Errorf("errors.As gave %+v, want the refusal with its Op intact", a)
	}
}
