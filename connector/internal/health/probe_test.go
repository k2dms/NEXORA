package health

// The tests here drive every layer over loopback against a process that speaks the real SOCKS5 wire, rather than
// against a seam that would let this package grade its own client. The reason is §8 invariant 1: the two upper layers
// are the whole claim of this component, and a fake that accepted anything would pass a client that sent nothing.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/compile"
)

// egressAddress is TEST-NET-3 (`RFC 5737`), the documentation range: since ODEC-42 the expected address is the
// server's own, so this is the value that is *not* the harness server's — someone else's traffic, in a test whose
// subject is whether an address is the expected one.
const egressAddress = "203.0.113.7"

// fakeCore is the stand-in for the one thing this package may not assume: a socks inbound that opens the outbound the
// request names. It answers the greeting and the CONNECT the way the compiler's `auth: noauth` inbound does, then
// pipes bytes, so a client that mis-encoded either exchange fails here for the reason it would fail in production.
type fakeCore struct {
	ln    net.Listener
	reply byte
	hang  bool

	connects chan string
}

func startFakeCore(t *testing.T, core *fakeCore) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("loopback listener: %v", err)
	}
	core.ln = ln
	core.connects = make(chan string, 16)
	t.Cleanup(func() { _ = ln.Close() })
	go core.serve(t)
}

func (f *fakeCore) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeCore) serve(t *testing.T) {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return // the listener closed with the test
		}
		go func() {
			defer conn.Close()
			if err := f.handle(conn); err != nil {
				t.Logf("fake core: %v", err)
			}
		}()
	}
}

func (f *fakeCore) handle(conn net.Conn) error {
	greet := make([]byte, 3)
	if _, err := io.ReadFull(conn, greet); err != nil {
		return err
	}
	if greet[0] != 0x05 || greet[1] != 0x01 || greet[2] != 0x00 {
		return fmt.Errorf("the client greeted with %X, not a single no-auth method", greet)
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return err
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 0x05 || head[1] != 0x01 {
		return fmt.Errorf("the client asked for version %d command %d; a probe only ever CONNECTs", head[0], head[1])
	}
	host, err := readSocksAddress(conn, head[3])
	if err != nil {
		return err
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return err
	}
	target := net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(port)))
	f.connects <- target

	if f.hang {
		time.Sleep(30 * time.Second)
		return nil
	}
	if f.reply != 0 {
		_, err := conn.Write([]byte{0x05, f.reply, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return err
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		_, werr := conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return werr
	}
	defer upstream.Close()
	bind := upstream.LocalAddr().(*net.TCPAddr)
	reply := append([]byte{0x05, 0x00, 0x00, 0x01}, bind.IP.To4()...)
	conn.Write(binary.BigEndian.AppendUint16(reply, uint16(bind.Port)))

	go func() { io.Copy(upstream, conn); upstream.Close() }()
	io.Copy(conn, upstream)
	return nil
}

func readSocksAddress(conn net.Conn, atyp byte) (string, error) {
	switch atyp {
	case 0x01:
		b := make([]byte, 4)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		return net.IP(b).String(), nil
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", err
		}
		b := make([]byte, length[0])
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		return string(b), nil
	case 0x04:
		b := make([]byte, 16)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		return net.IP(b).String(), nil
	default:
		return "", fmt.Errorf("address type %d", atyp)
	}
}

// clock is the calendar the records read. RTT never comes from it, and no test here asserts a duration the wall
// clock did not measure.
type clock struct{ at time.Time }

func (c *clock) Now() time.Time { return c.at }

func (c *clock) Advance(d time.Duration) { c.at = c.at.Add(d) }

// harness is one server, one Core inbound and one egress endpoint, all real listeners on loopback.
type harness struct {
	t        *testing.T
	core     *fakeCore
	vps      net.Listener
	origin   *httptest.Server
	serverID string
	clock    *clock
	events   []Event
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, serverID: "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01", clock: &clock{at: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)}} // nexora-secret-allow: fixture id invented for this test
	h.core = &fakeCore{}
	startFakeCore(t, h.core)

	vps, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("vps listener: %v", err)
	}
	h.vps = vps
	t.Cleanup(func() { _ = vps.Close() })
	go func() {
		for {
			c, err := vps.Accept()
			if err != nil {
				return
			}
			_ = c.Close() // a port that answers is all the tcp layer asks
		}
	}()

	// The endpoint echoes the address behind it, which for a probe that reached this origin through the harness's
	// own loopback tunnel is the harness server's address: the matched case, because ODEC-42 compares the answer with
	// the server record rather than with a number the wiring declares.
	h.origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s\n", h.server().Host)
	}))
	t.Cleanup(h.origin.Close)
	return h
}

func (h *harness) inbounds() []compile.Inbound {
	return []compile.Inbound{{Listen: "127.0.0.1", Port: h.core.port(), Tag: inboundTag(h.serverID)}}
}

func (h *harness) server() Server {
	return Server{ID: h.serverID, Host: "127.0.0.1", Port: h.vps.Addr().(*net.TCPAddr).Port}
}

// prober builds the package under its production shape: every required seam present, the failover policy at the
// schema's documented defaults, and a layer budget short enough that a hung test fails rather than hangs.
func (h *harness) prober(tweak ...func(*Config)) *Prober {
	h.t.Helper()
	cfg := Config{
		Server: func(ctx context.Context, id string) (Server, error) {
			if id != h.serverID {
				return Server{}, fmt.Errorf("no server %s", id)
			}
			return h.server(), nil
		},
		Inbounds:     h.inbounds,
		Egress:       func() (string, error) { return h.origin.URL + "/checkip", nil },
		Failover:     compile.Failover{Enabled: true, CooldownSeconds: 60, MaxConsecutiveFailures: 3, ProbeIntervalSeconds: 120},
		LayerTimeout: 2 * time.Second,
		Now:          h.clock.Now,
		Events:       func(e Event) { h.events = append(h.events, e) },
		LookupHost:   func(ctx context.Context, host string) ([]string, error) { return []string{"127.0.0.1"}, nil },
	}
	for _, f := range tweak {
		f(&cfg)
	}
	p, err := New(cfg)
	if err != nil {
		h.t.Fatalf("New: %v", err)
	}
	return p
}

func request(layers ...string) Request {
	return Request{ServerID: "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01", Layers: layers, Repeat: 1} // nexora-secret-allow: fixture id invented for this test
}

// egressRequest is the same probe with the flag that asks for the address set: the fifth row and the two answer
// fields belong to `egressProbe`, and a caller that did not ask for a fetch does not get one.
func egressRequest(layers ...string) Request {
	req := request(layers...)
	req.Egress = true
	return req
}

func rowOf(t *testing.T, a *Answer, layer string) Measure {
	t.Helper()
	for _, m := range a.Layers {
		if m.Layer == layer {
			return m
		}
	}
	t.Fatalf("the answer carries no %q row; it carries %v", layer, a.Layers)
	return Measure{}
}

func TestEveryLayerPassingIsTheOnlyAnswerThatMeansTheTunnelWorks(t *testing.T) {
	h := newHarness(t)
	a, err := h.prober().Probe(context.Background(), egressRequest(LayerDNS, LayerTCP, LayerCoreOutbound, LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if a.Overall != OverallPass {
		t.Errorf("overall is %q with every layer measured, want pass: %+v", a.Overall, a.Layers)
	}
	if len(a.Layers) != 5 {
		t.Errorf("the answer carries %d rows, want the four layers plus the egress address", len(a.Layers))
	}
	for _, m := range a.Layers {
		if m.State != StatePass {
			t.Errorf("%s is %q (%s %s), want pass", m.Layer, m.State, m.Code, m.Detail)
		}
	}
	if a.EgressIP == nil || *a.EgressIP != h.server().Host {
		t.Errorf("egressIp is %v, want the address the endpoint returned", a.EgressIP)
	}
	if a.MatchesExpectedEgress == nil || !*a.MatchesExpectedEgress {
		t.Error("matchesExpectedEgress must be true when the observed address is the one the server's own record carries")
	}
	if a.TestedAt == "" {
		t.Error("testedAt is required by the result schema and empty")
	}
	// The Core was asked for two outbounds: the VPS's address and the endpoint's. A client that dialed the endpoint
	// directly would leave the first connect missing, which is the leak invariant 1 forbids.
	if got := len(h.core.connects); got != 2 {
		t.Errorf("the fake Core opened %d outbounds, want one per upper layer", got)
	}
}

func TestACoreThatRefusesTheOutboundIsPartialAndNeverGreen(t *testing.T) {
	h := newHarness(t)
	h.core.reply = 0x02 // "connection not allowed by ruleset": the routing table in force has no route for this tag
	a, err := h.prober().Probe(context.Background(), request(LayerDNS, LayerTCP, LayerCoreOutbound, LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if a.Overall != OverallPartial {
		t.Errorf("overall is %q, want partial: the lower layers passed and the tunnel did not", a.Overall)
	}
	core := rowOf(t, a, LayerCoreOutbound)
	if core.State != StateFail || core.Code != "HandshakeFailed" {
		t.Errorf("core-outbound is %q/%q, want fail/HandshakeFailed (%s)", core.State, core.Code, core.Detail)
	}
	if !strings.Contains(core.Detail, "routing rules") {
		t.Errorf("the detail does not carry the reply the Core actually gave: %q", core.Detail)
	}
	if got := rowOf(t, a, LayerProxiedHTTP).State; got != StateSkipped {
		t.Errorf("proxied-http is %q, want skipped: no tunnel was open to fetch through", got)
	}
	if a.EgressIP != nil || a.MatchesExpectedEgress != nil {
		t.Error("an answer that never fetched must not carry an address or an opinion about one")
	}
}

func TestADeadVPSFailsTheLayerAskedAboutAndSkipsEveryRowAboveIt(t *testing.T) {
	h := newHarness(t)
	_ = h.vps.Close()
	a, err := h.prober().Probe(context.Background(), request(LayerDNS, LayerTCP, LayerCoreOutbound))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := rowOf(t, a, LayerDNS).State; got != StatePass {
		t.Errorf("dns is %q, want pass: the name still resolves with nothing listening", got)
	}
	tcp := rowOf(t, a, LayerTCP)
	if tcp.State != StateFail || tcp.Code != "NetworkUnreachable" {
		t.Errorf("tcp is %q/%q, want fail/NetworkUnreachable", tcp.State, tcp.Code)
	}
	if got := rowOf(t, a, LayerCoreOutbound).State; got != StateSkipped {
		t.Errorf("core-outbound is %q, want skipped", got)
	}
	// `partial` is the enum's word for exactly this shape — "lower layers passed, a higher layer did not" — and it
	// is why a name that resolves cannot make a dead port read as a working server.
	if a.Overall != OverallPartial {
		t.Errorf("overall is %q, want partial", a.Overall)
	}
}

func TestANameThatDoesNotResolveFailsTheProbeAtTheFirstRow(t *testing.T) {
	h := newHarness(t)
	p := h.prober(func(cfg *Config) {
		cfg.LookupHost = func(ctx context.Context, host string) ([]string, error) {
			return nil, fmt.Errorf("no such host: %s", host)
		}
	})
	a, err := p.Probe(context.Background(), request(LayerDNS, LayerTCP, LayerCoreOutbound))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if a.Overall != OverallFail {
		t.Errorf("overall is %q, want fail: nothing below the resolver was measured, so no layer can be credited", a.Overall)
	}
	if dns := rowOf(t, a, LayerDNS); dns.State != StateFail || dns.Code != "NetworkUnreachable" {
		t.Errorf("dns is %q/%q, want fail/NetworkUnreachable — that is the code whose registered meaning names a DNS failure", dns.State, dns.Code)
	}
	for _, l := range []string{LayerTCP, LayerCoreOutbound} {
		if got := rowOf(t, a, l).State; got != StateSkipped {
			t.Errorf("%s is %q, want skipped", l, got)
		}
	}
}

func TestAnUnexpectedEgressAddressIsALeakAndNotAPartial(t *testing.T) {
	h := newHarness(t)
	h.origin.Close()
	h.origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "203.0.113.99\n") // someone else's address: the traffic left this machine unproxied
	}))
	p := h.prober()
	a, err := p.Probe(context.Background(), egressRequest(LayerDNS, LayerTCP, LayerCoreOutbound, LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if a.Overall != OverallNotProxied {
		t.Errorf("overall is %q, want not-proxied: every layer answered and the address was the wrong one", a.Overall)
	}
	if a.MatchesExpectedEgress == nil || *a.MatchesExpectedEgress {
		t.Error("matchesExpectedEgress must be false, and that field is the only one that licenses a green mark")
	}
	if got := rowOf(t, a, LayerEgressIP).State; got != StateFail {
		t.Errorf("egress-ip is %q, want fail", got)
	}
	if got := rowOf(t, a, LayerProxiedHTTP).State; got != StatePass {
		t.Errorf("proxied-http is %q, want pass: the leak is not that layer's failure", got)
	}
}

// The runtime half of ODEC-42, in four tests: the claim an egress probe compares itself against is the server
// record's own address read at probe time, a name there is resolved at probe time, an unresolvable name is the
// absence of a claim rather than a leak, and the endpoint is read when the probe runs because the settings document
// that carries it arrives with `config.apply` and not with the process.
func TestTheExpectedAddressIsTheServersOwnAndNotAWiringConstant(t *testing.T) {
	h := newHarness(t)
	p := h.prober(func(cfg *Config) {
		cfg.Server = func(context.Context, string) (Server, error) {
			s := h.server()
			// The record now claims a different machine while the endpoint still echoes this one. A wiring-time
			// expectation would have gone on reporting a match, which is the failure this row exists to prevent.
			s.Host = egressAddress
			return s, nil
		}
	})
	a, err := p.Probe(context.Background(), egressRequest(LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if a.Overall != OverallNotProxied {
		t.Errorf("overall is %q, want not-proxied: every layer answered and the address is not the server's", a.Overall)
	}
	if a.MatchesExpectedEgress == nil || *a.MatchesExpectedEgress {
		t.Error("matchesExpectedEgress still says true against a server record that claims another address")
	}
	ip := rowOf(t, a, LayerEgressIP)
	if ip.State != StateFail || !strings.Contains(ip.Detail, egressAddress) {
		t.Errorf("egress-ip is %q (%s), want a fail naming the address the server claims", ip.State, ip.Detail)
	}
}

func TestAServerAddressThatIsANameIsResolvedForTheComparison(t *testing.T) {
	h := newHarness(t)
	p := h.prober(func(cfg *Config) {
		cfg.Server = func(context.Context, string) (Server, error) {
			s := h.server()
			s.Host = "vps.example"
			return s, nil
		}
		cfg.LookupHost = func(_ context.Context, host string) ([]string, error) {
			if host != "vps.example" {
				return nil, fmt.Errorf("unexpected resolver question: %s", host)
			}
			return []string{"127.0.0.1"}, nil
		}
	})
	a, err := p.Probe(context.Background(), egressRequest(LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if a.MatchesExpectedEgress == nil || !*a.MatchesExpectedEgress {
		t.Errorf("matchesExpectedEgress is %v, want true: the name resolved to the address behind the tunnel", a.MatchesExpectedEgress)
	}
}

func TestAnUnresolvableServerAddressIsNoOpinionAndNotALeak(t *testing.T) {
	h := newHarness(t)
	p := h.prober(func(cfg *Config) {
		cfg.Server = func(context.Context, string) (Server, error) {
			s := h.server()
			s.Host = "vps.example"
			return s, nil
		}
		cfg.LookupHost = func(_ context.Context, host string) ([]string, error) {
			return nil, fmt.Errorf("no such host: %s", host)
		}
	})
	a, err := p.Probe(context.Background(), egressRequest(LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if a.EgressIP == nil {
		t.Error("the endpoint answered with an address and the answer dropped it: a missing claim is not a missing measurement")
	}
	if a.MatchesExpectedEgress != nil {
		t.Errorf("matchesExpectedEgress is %v, want absent: nothing resolves to compare against, so the probe has no opinion", *a.MatchesExpectedEgress)
	}
	eg := rowOf(t, a, LayerEgressIP)
	if eg.State != StateSkipped {
		t.Errorf("egress-ip is %q (%s), want skipped and not a fail: the claim is unknown, the egress is not someone else's", eg.State, eg.Detail)
	}
	if got := rowOf(t, a, LayerProxiedHTTP).State; got != StatePass {
		t.Errorf("proxied-http is %q, want pass: the fetch worked, which is all that row reports", got)
	}
	if a.Overall != OverallPartial {
		t.Errorf("overall is %q, want partial: an unmeasured row cannot be credited as a green mark", a.Overall)
	}
}

func TestTheEgressEndpointIsReadWhenTheProbeRuns(t *testing.T) {
	h := newHarness(t)
	var endpoint string
	p := h.prober(func(cfg *Config) {
		cfg.Egress = func() (string, error) {
			if endpoint == "" {
				return "", errors.New("no settings document has been applied")
			}
			return endpoint, nil
		}
	})

	// Nothing has been applied, so a probe that asks for a fetch is the refusal that says so — and a probe that asks
	// for none is answered, which is the difference between refusing a wiring and reporting a missing document.
	_, err := p.Probe(context.Background(), egressRequest(LayerProxiedHTTP))
	var he *Error
	if !errors.As(err, &he) || !errors.Is(err, ErrNoEgress) {
		t.Fatalf("the fetch layer without an endpoint gave %v, want a refusal that unwraps to ErrNoEgress", err)
	}
	if he.Code() != "HealthCheckFailed" {
		t.Errorf("the missing endpoint answers %q, want HealthCheckFailed", he.Code())
	}
	if _, err := p.Probe(context.Background(), request(LayerTCP)); err != nil {
		t.Fatalf("a probe that fetches nothing was refused for want of an endpoint: %v", err)
	}

	endpoint = h.origin.URL + "/checkip"
	a, err := p.Probe(context.Background(), egressRequest(LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("the same prober refused the endpoint that appeared after its first apply: %v", err)
	}
	if a.MatchesExpectedEgress == nil || !*a.MatchesExpectedEgress {
		t.Error("a late endpoint that matches the server's address is still reported as no opinion")
	}

	endpoint = "ftp://api.ipify.org"
	if _, err := p.Probe(context.Background(), egressRequest(LayerProxiedHTTP)); !errors.Is(err, ErrNoEgress) {
		t.Errorf("an endpoint that cannot be fetched gave %v, want ErrNoEgress", err)
	}
}

func TestADeadEgressEndpointBlamesTheEndpointAndNotTheServer(t *testing.T) {
	h := newHarness(t)
	url := h.origin.URL
	h.origin.Close()
	p := h.prober()
	a, err := p.Probe(context.Background(), egressRequest(LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	httpRow := rowOf(t, a, LayerProxiedHTTP)
	if httpRow.Code != "NetworkUnreachable" {
		t.Errorf("the fetch failed with code %q, want NetworkUnreachable: doc 22 §8 refuses to let a dead checker be a dead server", httpRow.Code)
	}
	if !strings.Contains(httpRow.Detail, strings.TrimPrefix(url, "http://")) {
		t.Errorf("the detail %q does not name the target, which is that same row's instruction", httpRow.Detail)
	}
	if got := rowOf(t, a, LayerEgressIP).State; got != StateSkipped {
		t.Errorf("egress-ip is %q, want skipped: nothing answered, so there was no address to compare", got)
	}
}

func TestNoInboundForTheTagsIsCoreNotRunningAndNoSocketOpens(t *testing.T) {
	h := newHarness(t)
	p := h.prober(func(cfg *Config) { cfg.Inbounds = func() []compile.Inbound { return nil } })
	_, err := p.Probe(context.Background(), request(LayerDNS, LayerTCP))
	var he *Error
	if !errors.As(err, &he) {
		t.Fatalf("Probe returned %v, want a *health.Error", err)
	}
	if he.Code() != "CoreNotRunning" {
		t.Errorf("the refusal answers %q, want CoreNotRunning", he.Code())
	}
	if len(h.core.connects) != 0 {
		t.Error("a refused probe opened an outbound anyway")
	}
}

func TestAnInboundOwnedByAnotherServerIsNotMeasured(t *testing.T) {
	h := newHarness(t)
	p := h.prober(func(cfg *Config) {
		cfg.Inbounds = func() []compile.Inbound {
			return []compile.Inbound{{Listen: "127.0.0.1", Port: h.core.port(), Tag: "in-srv-deadbeef"}}
		}
	})
	if _, err := p.Probe(context.Background(), request(LayerTCP)); err == nil {
		t.Fatal("a probe of a tag the table does not carry was answered: it measured somebody else's inbound")
	}
}

func TestAStalledInboundIsATimeoutRowNotAFailure(t *testing.T) {
	h := newHarness(t)
	h.core.hang = true
	a, err := h.prober(func(cfg *Config) { cfg.LayerTimeout = 80 * time.Millisecond }).
		Probe(context.Background(), request(LayerDNS, LayerTCP, LayerCoreOutbound))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	core := rowOf(t, a, LayerCoreOutbound)
	if core.State != StateTimeout || core.Code != "Timeout" {
		t.Errorf("core-outbound is %q/%q, want timeout/Timeout", core.State, core.Code)
	}
	if got := rowOf(t, a, LayerTCP).State; got != StatePass {
		t.Errorf("tcp is %q, want pass: the timeout is the tunnel's, not the VPS's", got)
	}
	if a.Overall != OverallPartial {
		t.Errorf("overall is %q, want partial", a.Overall)
	}
}

func TestRepeatReportsAMeanAndASpreadAndASingleRunReportsNeitherSpread(t *testing.T) {
	h := newHarness(t)
	p := h.prober()
	thrice := request(LayerTCP)
	thrice.Repeat = 3
	a, err := p.Probe(context.Background(), thrice)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	tcp := rowOf(t, a, LayerTCP)
	if tcp.RTTMS == nil {
		t.Fatal("rttMs is absent where a measurement was made")
	}
	if tcp.JitterMS == nil {
		t.Error("jitterMs is absent after three runs, which is the only case it means anything in")
	}
	once, err := p.Probe(context.Background(), request(LayerTCP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if rowOf(t, once, LayerTCP).JitterMS != nil {
		t.Error("jitterMs is present after one run: a single sample has no spread, and zero would claim it did")
	}
}

func TestTheEndpointBodyIsNeverQuotedInAnAnswer(t *testing.T) {
	h := newHarness(t)
	h.origin.Close()
	h.origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "token=do-not-log-this\n")
	}))
	p := h.prober()
	a, err := p.Probe(context.Background(), egressRequest(LayerProxiedHTTP))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if strings.Contains(fmt.Sprint(a.Layers), "do-not-log-this") {
		t.Errorf("a layer detail quoted the endpoint's body: %+v", a.Layers)
	}
	if got := rowOf(t, a, LayerEgressIP).Code; got != "HealthCheckFailed" {
		t.Errorf("an answer with no address in it is coded %q, want HealthCheckFailed", got)
	}
}

func TestALayerOrderTheCallerChoseIsNotTheOrderMeasured(t *testing.T) {
	h := newHarness(t)
	a, err := h.prober().Probe(context.Background(), request(LayerProxiedHTTP, LayerTCP, LayerDNS))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	want := []string{LayerDNS, LayerTCP, LayerProxiedHTTP}
	if len(a.Layers) != len(want) {
		t.Fatalf("%d rows, want %d: no egress address without the flag", len(a.Layers), len(want))
	}
	for i, l := range want {
		if a.Layers[i].Layer != l {
			t.Errorf("row %d is %q, want %q: the args say the Connector orders them, not the caller", i, a.Layers[i].Layer, l)
		}
	}
}

func TestAnEgressProbeFetchesEvenWhenTheCallerNamedNoFetchLayer(t *testing.T) {
	h := newHarness(t)
	req := request(LayerTCP)
	req.Egress = true
	a, err := h.prober().Probe(context.Background(), req)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := rowOf(t, a, LayerProxiedHTTP).State; got != StatePass {
		t.Errorf("proxied-http is %q, want pass: egressProbe's own description says the fetch happens", got)
	}
	if a.EgressIP == nil {
		t.Error("an egress probe that never reported an address cannot answer matchesExpectedEgress")
	}
}

func TestARequestTheCommandRefusesOpensNoSockets(t *testing.T) {
	h := newHarness(t)
	p := h.prober()
	cases := []struct {
		name string
		req  Request
	}{
		{"egress-ip is an answer not a request", request(LayerEgressIP)},
		{"a layer outside the enum", request(LayerDNS, "tls")},
		{"the same layer twice", request(LayerTCP, LayerTCP)},
		{"no layers", request()},
		{"five layers", request(LayerDNS, LayerTCP, LayerCoreOutbound, LayerProxiedHTTP, LayerDNS)},
		{"a repeat past the command's bound", func() Request { r := request(LayerTCP); r.Repeat = 11; return r }()},
		{"an id the store has never heard of", func() Request { r := request(LayerTCP); r.ServerID = "00000000-0000-4000-8000-000000000000"; return r }()}, // nexora-secret-allow: fixture id invented for this test
	}
	for _, c := range cases {
		if _, err := p.Probe(context.Background(), c.req); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
	if n := len(h.core.connects); n != 0 {
		t.Errorf("%d outbounds were opened by requests that should have been refused", n)
	}
}
