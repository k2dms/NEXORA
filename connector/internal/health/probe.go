package health

// This file is the measurement: the four requestable layers, the egress answer that rides on the last of them, and
// the merge of `repeat` runs into the rows the command answers with. The wiring that decides *whether* to probe is in
// state.go; the payload shape is `packages/schema/protocol/commands/server.test.json`.
//
// Two rules run through every layer here. The first is §8 invariant 1 of
// `docs/architecture/22-component-contracts.md`: a probe asserts egress through Core, so the two upper layers are
// asked of the Core's own loopback inbound rather than of the VPS, and a `dns`/`tcp` pass on its own is reported as
// `partial` and never as a working proxy. The second is that section's mode-of-failure row: an egress endpoint that
// is itself down is reported as `NetworkUnreachable` with the target named, not as a dead server — which is why the
// endpoint's failure lands in a layer row rather than a refusal.

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Probe runs one request and records what it learned. A refusal means the probe could not be performed at all; a
// layer that failed is the answer, not an error, because `state` has a member for every way a layer can fail and a
// client that has to catch an exception to learn the VPS is down is a client that cannot show it.
func (p *Prober) Probe(ctx context.Context, req Request) (*Answer, error) {
	layers, err := normalize(req)
	if err != nil {
		return nil, err
	}

	srv, err := p.cfg.Server(ctx, req.ServerID)
	if err != nil {
		return nil, refuse("server", req.ServerID, fmt.Errorf("%w: %v", ErrNoServer, err))
	}
	// Unconditional, including for a request naming only dns and tcp: `docs/architecture/30-failure-policy.md` §4's
	// first rule is "No probes", and the fixture's message for it — `probe would report success against a stale
	// inbound` — is about the lower layers most of all, since a VPS that answers TCP is exactly what a user would
	// read as "the server works" while nothing is carrying their traffic.
	inbound, ok := p.inboundFor(req.ServerID)
	if !ok {
		return nil, refuse("inbound", req.ServerID, ErrCoreNotRunning)
	}
	repeat := req.Repeat
	if repeat <= 0 {
		repeat = 1
	}

	rows := make([]row, 0, len(layers)+1)
	blocked := false
	var egress *egressOutcome

	for _, layer := range layers {
		if blocked {
			rows = append(rows, row{layer: layer, attempts: []Attempt{{State: StateSkipped}}})
			continue
		}
		switch layer {
		case LayerDNS:
			rows = append(rows, row{layer: layer, attempts: p.repeat(ctx, repeat, func(c context.Context) Attempt { return p.dns(c, srv.Host) })})
		case LayerTCP:
			rows = append(rows, row{layer: layer, attempts: p.repeat(ctx, repeat, func(c context.Context) Attempt { return p.tcp(c, srv) })})
		case LayerCoreOutbound:
			rows = append(rows, row{layer: layer, attempts: p.repeat(ctx, repeat, func(c context.Context) Attempt { return p.coreOutbound(c, inbound, srv) })})
		case LayerProxiedHTTP:
			// The endpoint is read here rather than at construction: ODEC-42 put it in a settings document, and the
			// document arrives with `config.apply`, which a probe may never have seen. A fetch layer with no endpoint
			// to fetch is a refusal naming that, and not a `NetworkUnreachable` row about a target nobody named.
			tgt, err := p.egressTarget()
			if err != nil {
				return nil, refuse(layer, req.ServerID, fmt.Errorf("%w: %v", ErrNoEgress, err))
			}
			// One fetch answers both this row and `egress-ip`: the request that carried the address is the request
			// whose status is reported here, and repeating it twice would be two egress probes for one row of each.
			if req.Egress {
				egress = p.fetchEgress(ctx, inbound, tgt, p.expectedAddresses(ctx, srv), repeat)
				rows = append(rows, row{layer: layer, attempts: egress.http})
			} else {
				rows = append(rows, row{layer: layer, attempts: p.repeat(ctx, repeat, func(c context.Context) Attempt { return p.proxiedGet(c, inbound, tgt) })})
			}
		}
		if rows[len(rows)-1].worst() != StatePass {
			blocked = true
		}
	}
	if egress != nil {
		rows = append(rows, row{layer: LayerEgressIP, attempts: egress.address})
	}

	at := p.cfg.Now()
	answer := &Answer{
		ServerID: req.ServerID,
		Layers:   measuresOf(rows),
		Overall:  overallOf(rows, egress),
		TestedAt: at.UTC().Format(time.RFC3339Nano),
	}
	if egress != nil {
		// `egressIp` is "only present when egressProbe was requested and proxied-http passed", and
		// `matchesExpectedEgress` is nullable because an endpoint that never answered has no opinion about the
		// address rather than an opinion that disagrees.
		if egress.ip != "" {
			ip := egress.ip
			answer.EgressIP = &ip
		}
		if egress.observed {
			matched := egress.matched
			answer.MatchesExpectedEgress = &matched
		}
	}
	p.note(answer, at)
	return answer, nil
}

// normalize applies the args contract this package's caller has already checked, so a direct caller of Probe cannot
// produce a result the schema would refuse: unique layer names from the requestable four, in the Connector's order,
// and a repeat the command bounds.
func normalize(req Request) ([]string, error) {
	if req.ServerID == "" {
		return nil, refuse("args", "", ErrNoServer)
	}
	if len(req.Layers) == 0 || len(req.Layers) > len(order) {
		return nil, refuse("args", req.ServerID, fmt.Errorf("layers must hold between 1 and %d names, and it holds %d", len(order), len(req.Layers)))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(req.Layers))
	for _, l := range req.Layers {
		if l == LayerEgressIP {
			return nil, refuse("args", req.ServerID, errors.New("layers names egress-ip, which is an answer rather than a request: it is what an egressProbe of proxied-http reports"))
		}
		if rank(l) == len(order) {
			return nil, refuse("args", req.ServerID, fmt.Errorf("layers carries a name outside the enum dns|tcp|core-outbound|proxied-http"))
		}
		if seen[l] {
			return nil, refuse("args", req.ServerID, errors.New("layers carries the same name twice, and the command's layers is uniqueItems"))
		}
		seen[l] = true
		out = append(out, l)
	}
	sortLayers(out)
	if req.Repeat < 1 || req.Repeat > 10 {
		return nil, refuse("args", req.ServerID, fmt.Errorf("repeat is %d, outside the command's 1 to 10", req.Repeat))
	}
	if req.Egress && !seen[LayerProxiedHTTP] {
		// The flag's own description says the fetch happens, and an address with no request behind it would be a row
		// the caller never asked for and cannot explain. Adding the layer is the reading that keeps both rows true.
		out = append(out, LayerProxiedHTTP)
	}
	return out, nil
}

func sortLayers(layers []string) {
	for i := 1; i < len(layers); i++ {
		for j := i; j > 0 && rank(layers[j]) < rank(layers[j-1]); j-- {
			layers[j], layers[j-1] = layers[j-1], layers[j]
		}
	}
}

// row is one layer's attempts, kept together so the merge and the short-circuit read the same data.
type row struct {
	layer    string
	attempts []Attempt
}

// worst is the row's state before the merge: `timeout` outranks `fail` because it is the more actionable of the two
// facts a mixed set of runs can report — "we never got an answer" tells a user something "the answer was no" does not,
// and both mean the same thing to the streak counter.
func (r row) worst() string {
	worst := StatePass
	for _, a := range r.attempts {
		if a.State == StateTimeout {
			return StateTimeout
		}
		if a.State != StatePass {
			worst = a.State
		}
	}
	return worst
}

func (r row) measure() Measure {
	state := r.worst()
	m := Measure{Layer: r.layer, State: state}
	if state == StateSkipped {
		m.Detail = clip("a lower layer did not pass, so this measurement would describe a path already known not to work")
		return m
	}
	// The row names the first attempt that failed, which is the one whose target and reply a reader can act on; a
	// mixed set of repeats has no single cause, and inventing a summary of several would be a claim about all of them.
	for _, a := range r.attempts {
		if a.State != StatePass {
			m.Code, m.Detail = a.Code, a.Detail
			break
		}
	}
	if m.Code == "" && state != StatePass {
		m.Code = "HealthCheckFailed"
	}

	var total, min, max time.Duration
	seen := 0
	for _, a := range r.attempts {
		total += a.RTT
		if seen == 0 || a.RTT < min {
			min = a.RTT
		}
		if seen == 0 || a.RTT > max {
			max = a.RTT
		}
		seen++
	}
	if seen == 0 {
		return m
	}
	rtt := round3(float64(total) / float64(time.Millisecond) / float64(seen))
	m.RTTMS = &rtt
	// Jitter of one sample is not a measurement of "no variation", so the field is absent rather than zero — the
	// schema leaves it optional for exactly that reason.
	if seen > 1 {
		jitter := round3(float64(max-min) / float64(time.Millisecond))
		m.JitterMS = &jitter
	}
	return m
}

func measuresOf(rows []row) []Measure {
	out := make([]Measure, len(rows))
	for i, r := range rows {
		out[i] = r.measure()
	}
	return out
}

// overallOf is `#/result/properties/overall`'s enum read as a decision table. `not-proxied` wins over `partial`
// whenever it is the only thing wrong, because the two are "the tunnel works and the address is someone else's" and
// "a layer did not work", and a user with a leak needs to hear the first of those.
func overallOf(rows []row, egress *egressOutcome) string {
	if egress != nil && egress.mismatched && passedExcept(rows, LayerEgressIP) {
		return OverallNotProxied
	}
	for i, r := range rows {
		if r.worst() == StatePass {
			continue
		}
		if i == 0 {
			return OverallFail
		}
		return OverallPartial
	}
	return OverallPass
}

func passedExcept(rows []row, layer string) bool {
	for _, r := range rows {
		if r.layer == layer {
			continue
		}
		if r.worst() != StatePass {
			return false
		}
	}
	return true
}

// repeat runs one layer n times and gives back the attempts. A timeout ends the repeats: four rows each stalling for
// the layer budget would spend the caller's 30 s on a server that is not answering, and the second measurement of a
// timeout has no information the first lacked.
func (p *Prober) repeat(ctx context.Context, n int, run func(context.Context) Attempt) []Attempt {
	out := make([]Attempt, 0, n)
	for i := 0; i < n; i++ {
		attemptCtx, cancel := context.WithTimeout(ctx, p.cfg.LayerTimeout)
		a := run(attemptCtx)
		cancel()
		out = append(out, a)
		if a.State == StateTimeout {
			break
		}
	}
	return out
}

// dns is the first layer: does this host resolve at all. `NetworkUnreachable` is its failure's registered meaning
// ("No route / DNS failure to the endpoint"), which is the only code in the taxonomy that names a resolver.
func (p *Prober) dns(ctx context.Context, host string) Attempt {
	start := time.Now()
	_, err := p.cfg.LookupHost(ctx, host)
	return attemptOf(start, err, "NetworkUnreachable", "the resolver gave no answer for "+host)
}

// tcp is the second layer: the VPS's port accepts a connection. It says nothing about the tunnel, which is the
// command's own summary for why the layer exists at all rather than being the answer.
func (p *Prober) tcp(ctx context.Context, srv Server) Attempt {
	start := time.Now()
	conn, err := p.dial(ctx, net.JoinHostPort(srv.Host, fmt.Sprint(srv.Port)))
	a := attemptOf(start, err, "NetworkUnreachable", "no TCP connection to "+net.JoinHostPort(srv.Host, fmt.Sprint(srv.Port)))
	if err == nil && conn != nil {
		_ = conn.Close()
	}
	return a
}

// coreOutbound is the third layer, and the first asked of Core rather than of the VPS: a SOCKS5 CONNECT from the
// server's own inbound to that server's address. A reply of 0x00 is the Core's statement that it accepted the request
// on the tag the compiler derived from this id and established the outbound — which is the VLESS/REALITY handshake
// having happened, since the socks request cannot be answered before the route to that tag is usable.
func (p *Prober) coreOutbound(ctx context.Context, inbound compileInbound, srv Server) Attempt {
	start := time.Now()
	conn, err := p.socksConnect(ctx, inbound, srv.Host, srv.Port)
	a := attemptOf(start, err, "HandshakeFailed", "Core refused the outbound for "+net.JoinHostPort(srv.Host, fmt.Sprint(srv.Port)))
	if err == nil && conn != nil {
		_ = conn.Close()
	}
	return a
}

// proxiedGet is the fourth layer without the egress comparison: a request that leaves through the tunnel and gets a
// 2xx back. It is the standalone answer for a caller that asked for `proxied-http` without `egressProbe`.
func (p *Prober) proxiedGet(ctx context.Context, inbound compileInbound, tgt *target) Attempt {
	start := time.Now()
	status, _, err := p.fetchThrough(ctx, inbound, tgt)
	return p.httpAttempt(start, tgt, status, err)
}

// egressTarget is the endpoint ODEC-42 placed in settings, read and parsed at the moment a layer needs to fetch.
func (p *Prober) egressTarget() (*target, error) {
	raw, err := p.cfg.Egress()
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, errors.New("no settings document has been applied, so `core.egressProbe.endpoint` has no value")
	}
	return parseTarget(raw)
}

// expectedAddresses is the claim an egress probe compares itself against: the server's own address, taken as the
// store wrote it when it is a literal and as the resolver answers it today when it is a name. No claim at all is not
// a failed comparison — the probe has no opinion, which `matchesExpectedEgress` carries as null.
func (p *Prober) expectedAddresses(ctx context.Context, srv Server) []string {
	if ip := net.ParseIP(srv.Host); ip != nil {
		return []string{ip.String()}
	}
	addrs, err := p.cfg.LookupHost(ctx, srv.Host)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		// A resolver that answers with names rather than addresses gives the comparison nothing, and guessing which
		// answer is the VPS's would be this package making the claim it exists to check.
		if ip := net.ParseIP(a); ip != nil {
			out = append(out, ip.String())
		}
	}
	return out
}

// egressOutcome is one egress probe's two rows plus the two facts the answer carries.
type egressOutcome struct {
	http    []Attempt
	address []Attempt
	ip      string
	// observed is whether an address can be judged at all — one read, and a claim to read it against;
	// matchesExpectedEgress is null until it is.
	observed bool
	matched  bool
	// mismatched is the leak: an address was read, the server's claim is known, and they disagree.
	mismatched bool
}

// fetchEgress asks the endpoint through the tunnel up to `repeat` times. Each fetch contributes one row of each kind
// — its status and the address read out of its body — so a repeat of 3 gives the latency spread of three fetches and
// three chances to read an address, rather than a second measurement of something already measured.
func (p *Prober) fetchEgress(ctx context.Context, inbound compileInbound, tgt *target, want []string, repeat int) *egressOutcome {
	out := &egressOutcome{}
	for i := 0; i < repeat; i++ {
		attemptCtx, cancel := context.WithTimeout(ctx, p.cfg.LayerTimeout)
		start := time.Now()
		status, body, err := p.fetchThrough(attemptCtx, inbound, tgt)
		cancel()
		rtt := time.Since(start)
		fetch := p.httpAttempt(start, tgt, status, err)
		out.http = append(out.http, fetch)

		switch {
		case fetch.State == StateTimeout:
			out.address = append(out.address, Attempt{State: StateTimeout, RTT: rtt, Code: "Timeout", Detail: clip("no answer within " + rtt.Round(time.Millisecond).String())})
			return out
		case fetch.State != StatePass:
			out.address = append(out.address, Attempt{State: StateSkipped, RTT: rtt, Detail: clip("the fetch did not succeed, so there was no answer to read an address from")})
			return out
		}
		ip, ok := firstAddress(body)
		if !ok {
			// The bytes are not quoted. §8 invariant 5 is about the result and the logs, and an endpoint's body is
			// neither the fact the row reports nor something a diagnostics section should carry.
			out.address = append(out.address, Attempt{State: StateFail, RTT: rtt, Code: "HealthCheckFailed", Detail: clip("the endpoint answered without an address in the body")})
			continue
		}
		out.ip = ip
		out.matched = slices.Contains(want, ip)
		out.mismatched = len(want) > 0 && !out.matched
		out.observed = len(want) > 0
		address := Attempt{State: StatePass, RTT: rtt, Detail: clip("the address behind the tunnel is the address this server claims")}
		switch {
		case !out.observed:
			address.State = StateSkipped
			address.Detail = clip("the server's address does not resolve, so there is no claim to compare the egress answer against")
		case out.mismatched:
			address.State = StateFail
			address.Detail = clip(fmt.Sprintf("egress was %s, expected %s", ip, strings.Join(want, " or ")))
			// A leak is a complete answer, not a partial one: repeating it cannot turn it into a match, and the
			// command's rate budget belongs to the user who is about to read this.
			out.address = append(out.address, address)
			return out
		}
		out.address = append(out.address, address)
	}
	if len(out.address) == 0 {
		out.address = []Attempt{{State: StateSkipped, Detail: clip("the fetch did not run")}}
	}
	return out
}

// httpAttempt turns one fetch into a row. A failure to reach the endpoint at all is `NetworkUnreachable` *with the
// target named*, which is doc 22 §8's mode-of-failure row refusing to let a dead checker be reported as a dead
// server; a non-2xx is the same class of fact about the checker.
func (p *Prober) httpAttempt(start time.Time, tgt *target, status int, err error) Attempt {
	if err != nil {
		return attemptOf(start, err, "NetworkUnreachable", "the egress endpoint "+tgt.raw+" did not answer through the tunnel")
	}
	rtt := time.Since(start)
	if status < 200 || status > 299 {
		return Attempt{State: StateFail, RTT: rtt, Code: "NetworkUnreachable", Detail: clip(fmt.Sprintf("%s answered %d", tgt.raw, status))}
	}
	return Attempt{State: StatePass, RTT: rtt}
}

// fetchThrough opens the egress endpoint through the server's inbound and performs the request. http.ReadResponse
// rather than http.Client because the connection is already a tunnel the Core agreed to open: a Client would resolve
// and dial the name again, which is the exact measurement this layer exists to avoid making.
func (p *Prober) fetchThrough(ctx context.Context, inbound compileInbound, tgt *target) (int, string, error) {
	conn, err := p.socksConnect(ctx, inbound, tgt.host, tgt.port)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()
	if err := deadline(conn, ctx); err != nil {
		return 0, "", err
	}
	if tgt.tls {
		tconn := tls.Client(conn, &tls.Config{ServerName: tgt.host})
		if err := tconn.HandshakeContext(ctx); err != nil {
			return 0, "", err
		}
		conn = tconn
	}
	req := &http.Request{
		Method:     http.MethodGet,
		URL:        tgt.parsed,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Host:       tgt.authority,
		Close:      true,
		Header:     http.Header{"User-Agent": []string{"nexora-connector/health"}},
	}
	if err := req.Write(conn); err != nil {
		return 0, "", err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	// The body is read to answer the address row and then dropped: §8 invariant 5 says a probe reveals no secret in
	// its result or logs, and an endpoint's plaintext is not something a diagnostics section should carry.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(body), nil
}

// firstAddress reads the first token that is an IP literal. Plain-text, JSON and HTML answers all work without a
// parser per endpoint, because `net.ParseIP` is the strictest possible reader of the one fact the row needs.
func firstAddress(body string) (string, bool) {
	for _, token := range strings.FieldsFunc(body, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == ',' || r == '{' || r == '}'
	}) {
		if ip := net.ParseIP(strings.TrimSpace(token)); ip != nil {
			return ip.String(), true
		}
	}
	return "", false
}

// parseTarget reads the egress URL into the four facts the fetch needs. Only origin-form http and https are
// accepted: a probe whose target is a proxy chain, a unix socket or a file is not measuring egress.
func parseTarget(raw string) (*target, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("scheme %q is not http or https", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, errors.New("the URL names no host to connect to")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	parsed, err := url.Parse(u.Scheme + "://" + u.Host + path)
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("port %q is not a number", port)
	}
	return &target{
		raw:       raw,
		host:      u.Hostname(),
		port:      n,
		authority: u.Host,
		tls:       u.Scheme == "https",
		parsed:    parsed,
	}, nil
}

type target struct {
	raw       string // as settings carried it, which is what a failure row names
	host      string
	port      int
	authority string
	tls       bool
	parsed    *url.URL
}

// socksConnect performs the two exchanges a SOCKS5 client makes against the inbound the compiler emitted with
// `auth: noauth`: the greeting that offers method 0 and the CONNECT that names the target. Anything else is refused
// rather than negotiated downward, because a Core that answers with an auth method this client did not offer is a
// Core whose inbound is not the one doc 27 §5 describes.
func (p *Prober) socksConnect(ctx context.Context, inbound compileInbound, host string, port int) (net.Conn, error) {
	conn, err := p.dial(ctx, net.JoinHostPort(inbound.listen, fmt.Sprint(inbound.port)))
	if err != nil {
		return nil, fmt.Errorf("the Core's inbound at %s did not answer: %w", net.JoinHostPort(inbound.listen, fmt.Sprint(inbound.port)), err)
	}
	if err := deadline(conn, ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	greet := make([]byte, 2)
	if _, err := io.ReadFull(conn, greet); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if greet[0] != 0x05 || greet[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("the inbound answered the no-auth greeting with %X; this compiler emits `auth: noauth` and negotiates down to nothing", greet)
	}

	request := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		request = append(request, 0x01)
		request = append(request, ip.To4()...)
	} else if ip != nil {
		request = append(request, 0x04)
		request = append(request, ip.To16()...)
	} else {
		b := []byte(host)
		if len(b) > 255 {
			return nil, fmt.Errorf("the target name is %d bytes, past the 255 a SOCKS5 domain address can carry", len(b))
		}
		request = append(request, 0x03, byte(len(b)))
		request = append(request, b...)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	if _, err := conn.Write(request); err != nil {
		_ = conn.Close()
		return nil, err
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if head[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("Core refused the outbound: SOCKS reply %s", socksReply(head[1]))
	}
	// The bound address the reply carries is read for nothing else than keeping the stream honest; a CONNECT's
	// reply is variable-length and the bytes after it are tunnel payload.
	switch head[3] {
	case 0x01:
		_, _ = io.ReadFull(conn, make([]byte, 6))
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			_ = conn.Close()
			return nil, err
		}
		_, _ = io.ReadFull(conn, make([]byte, int(length[0])+2))
	case 0x04:
		_, _ = io.ReadFull(conn, make([]byte, 18))
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("the inbound chose address type %d, which no SOCKS5 server should send in a reply", head[3])
	}
	return conn, nil
}

// socksReply names the failure the way the command's `detail` can say it. `AuthFailed` is declared by `server.test`
// and deliberately not used here: Xray's socks reply distinguishes "no route" from "the peer rejected the
// credential" no further than a reply code, and guessing which of the two a 0x01 was would be a claim about the
// user's UUID that nothing observed.
func socksReply(code byte) string {
	switch code {
	case 0x01:
		return "general failure to connect the requested outbound"
	case 0x02:
		return "connection not allowed by the routing rules in force"
	case 0x03:
		return "network unreachable from Core"
	case 0x04:
		return "host unreachable from Core"
	case 0x05:
		return "the connection refused"
	case 0x06:
		return "the time to live expired"
	case 0x07:
		return "the address type is not supported"
	default:
		return fmt.Sprintf("reply code %d", code)
	}
}

// compileInbound is one row of the table this package was handed, narrowed to the two fields a socks client dials.
type compileInbound struct {
	listen string
	port   int
}

func (p *Prober) inboundFor(serverID string) (compileInbound, bool) {
	tag := inboundTag(serverID)
	for _, in := range p.cfg.Inbounds() {
		if in.Tag == tag && in.Port > 0 {
			return compileInbound{listen: in.Listen, port: in.Port}, true
		}
	}
	return compileInbound{}, false
}

// dial is the platform seam, so a test can drive every layer over loopback without a route to the internet.
func (p *Prober) dial(ctx context.Context, addr string) (net.Conn, error) {
	return p.cfg.DialContext(ctx, "tcp", addr)
}

func deadline(conn net.Conn, ctx context.Context) error {
	d, ok := ctx.Deadline()
	if !ok {
		return nil
	}
	return conn.SetDeadline(d)
}

// attemptOf is the one place a Go error becomes a layer state. A context deadline is `timeout` and everything else
// is the code the caller named, because the taxonomy separates "we waited and nothing happened" from "the answer was
// no" and the two have different remediations.
//
// The Go error is appended to the detail rather than dropped: §8's mode-of-failure row asks for the target to be
// *named*, and the reply code, the resolver's complaint and the syscall's reason are the only parts of a probe that
// tell a user which of their three VPSes is the one with the firewall. No credential reaches this path — the probe
// reads no keystore value, sends no payload past a no-auth greeting, and never quotes a response body.
func attemptOf(start time.Time, err error, code, detail string) Attempt {
	rtt := time.Since(start)
	if err == nil {
		return Attempt{State: StatePass, RTT: rtt}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return Attempt{State: StateTimeout, RTT: rtt, Code: "Timeout", Detail: clip("no answer within " + rtt.Round(time.Millisecond).String())}
	}
	return Attempt{State: StateFail, RTT: rtt, Code: code, Detail: clip(detail + ": " + err.Error())}
}

// maxDetail is `#/result/properties/layers/items/properties/detail/maxLength`. The clip is here rather than at the
// command surface because every row is built in this package, and a layer that overflowed would be a frame the codec
// refuses — a closed port instead of a health answer.
//
// The bound counts code points, not bytes: JSON Schema's maxLength is "characters", and a detail that carries a
// non-ASCII server name or a resolver's UTF-8 error would otherwise be clipped at a length the receiver does not
// think it exceeded. The cut lands on a rune boundary for the same reason — half a rune is invalid UTF-8, and a
// frame the codec cannot parse is a worse answer than a short one.
const maxDetail = 160

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxDetail {
		return s
	}
	n := 0
	for i := range s {
		if n == maxDetail-1 {
			return s[:i] + "…"
		}
		n++
	}
	return s
}

func round3(f float64) float64 {
	return float64(int64(f*1000+0.5)) / 1000
}
