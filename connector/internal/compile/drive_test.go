package compile

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDriveXrayRouting(t *testing.T) {
	bin := driveXrayBinary(t)
	catchall := func(outbound string) CoreRule {
		return CoreRule{IP: []string{"0.0.0.0/0"}, Outbound: outbound}
	}
	for _, tc := range []struct {
		name     string
		rules    []CoreRule
		sniff    bool
		omit     string
		fallback string
		want     bool
	}{
		{name: "direct_catchall", rules: []CoreRule{catchall("direct")}, want: true},
		{name: "block_catchall", rules: []CoreRule{catchall("block")}},
		{name: "tcp_array_matches_http", rules: []CoreRule{{Network: []string{"tcp"}, Outbound: "direct"}, catchall("block")}, want: true},
		{name: "udp_array_does_not_match_http", rules: []CoreRule{{Network: []string{"udp"}, Outbound: "direct"}, catchall("block")}},
		{name: "tcp_udp_array_matches_http", rules: []CoreRule{{Network: []string{"tcp", "udp"}, Outbound: "direct"}, catchall("block")}, want: true},
		{name: "tcp_array_blocks_http", rules: []CoreRule{{Network: []string{"tcp"}, Outbound: "block"}, catchall("direct")}},
		{name: "udp_array_falls_through_to_direct", rules: []CoreRule{{Network: []string{"udp"}, Outbound: "block"}, catchall("direct")}, want: true},
		{name: "http_array_sniff_on_direct", rules: []CoreRule{{Protocol: []string{"http"}, Outbound: "direct"}, catchall("block")}, sniff: true, want: true},
		{name: "http_array_sniff_off_block", rules: []CoreRule{{Protocol: []string{"http"}, Outbound: "direct"}, catchall("block")}},
		{name: "http_array_sniff_on_block", rules: []CoreRule{{Protocol: []string{"http"}, Outbound: "block"}, catchall("direct")}, sniff: true},
		{name: "http_array_sniff_off_direct", rules: []CoreRule{{Protocol: []string{"http"}, Outbound: "block"}, catchall("direct")}, want: true},
		{name: "missing_direct_with_block_default_cannot_arrive", rules: []CoreRule{catchall("direct")}, omit: "direct", fallback: "block"},
		{name: "missing_direct_with_direct_default_reaches_target", rules: []CoreRule{catchall("direct")}, omit: "direct", fallback: "direct", want: true},
		{name: "invert_false_matching_ip", rules: []CoreRule{{IP: []string{"127.0.0.1/32"}, Invert: boolPtr(false), Outbound: "direct"}, catchall("block")}, want: true},
		{name: "invert_true_ignored_matching_ip", rules: []CoreRule{{IP: []string{"127.0.0.1/32"}, Invert: boolPtr(true), Outbound: "direct"}, catchall("block")}, want: true},
		{name: "invert_false_nonmatching_ip", rules: []CoreRule{{IP: []string{"127.0.0.2/32"}, Invert: boolPtr(false), Outbound: "direct"}, catchall("block")}},
		{name: "invert_true_ignored_nonmatching_ip", rules: []CoreRule{{IP: []string{"127.0.0.2/32"}, Invert: boolPtr(true), Outbound: "direct"}, catchall("block")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := &RoutingPolicy{CorePlane: CorePlane{
				DomainStrategy: "AsIs", Rules: tc.rules,
				Sniffing: Sniffing{Enabled: &tc.sniff, Protocols: []string{"http"}},
			}}
			fallback := "direct"
			if tc.want {
				fallback = "block"
			}
			if tc.fallback != "" {
				fallback = tc.fallback
			}
			p := driveStartXray(t, bin, policy, fallback, tc.omit)
			driveHTTP(t, p, tc.want)
			if tc.omit != "" {
				t.Logf("Xray 24.11.11: missing direct tag uses %s default; target reached=%t", fallback, tc.want)
			}
			if invert := tc.rules[0].Invert; invert != nil && *invert {
				t.Logf("Xray 24.11.11 ignores emitted invert:true for IP %v; target reached=%t", tc.rules[0].IP, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		network []string
		want    bool
	}{
		{"udp_array_round_trip", []string{"udp"}, true},
		{"tcp_array_rejects_udp", []string{"tcp"}, false},
		{"tcp_udp_array_both_transports", []string{"tcp", "udp"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := &RoutingPolicy{CorePlane: CorePlane{DomainStrategy: "AsIs", Rules: []CoreRule{
				{Network: tc.network, Outbound: "direct"}, catchall("block"),
			}}}
			fallback := "block"
			if !tc.want {
				fallback = "direct"
			}
			p := driveStartXray(t, bin, policy, fallback, "")
			if tc.network[0] == "tcp" {
				driveHTTP(t, p, true)
			}
			driveUDP(t, p, tc.want)
			if !tc.want {
				driveHTTP(t, p, true)
			}
		})
	}
}

// Opposite catchalls expose missing matches, including TLS without the SNI required by Xray's sniffer.
func TestDriveXrayTLSProtocolPredicate(t *testing.T) {
	bin := driveXrayBinary(t)
	catchall := func(outbound string) CoreRule {
		return CoreRule{IP: []string{"0.0.0.0/0"}, Outbound: outbound}
	}
	for _, tc := range []struct {
		name  string
		rules []CoreRule
		sniff bool
		https bool
		sni   string
		want  bool
	}{
		{name: "https_catchall_direct_reaches", rules: []CoreRule{catchall("direct")}, sniff: true, https: true, sni: "localhost", want: true},
		{name: "tls_sni_sniff_on_direct", rules: []CoreRule{{Protocol: []string{"tls"}, Outbound: "direct"}, catchall("block")}, sniff: true, https: true, sni: "localhost", want: true},
		{name: "tls_sni_sniff_off_block", rules: []CoreRule{{Protocol: []string{"tls"}, Outbound: "direct"}, catchall("block")}, sniff: false, https: true, sni: "localhost"},
		{name: "tls_sni_sniff_on_block", rules: []CoreRule{{Protocol: []string{"tls"}, Outbound: "block"}, catchall("direct")}, sniff: true, https: true, sni: "localhost"},
		{name: "http_does_not_match_tls_rule", rules: []CoreRule{{Protocol: []string{"tls"}, Outbound: "direct"}, catchall("block")}, sniff: true, https: false},
		{name: "tls_no_sni_not_detected", rules: []CoreRule{{Protocol: []string{"tls"}, Outbound: "direct"}, catchall("block")}, sniff: true, https: true, sni: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := &RoutingPolicy{CorePlane: CorePlane{
				DomainStrategy: "AsIs", Rules: tc.rules,
				Sniffing: Sniffing{Enabled: &tc.sniff, Protocols: []string{"http", "tls"}},
			}}
			fallback := "direct"
			if tc.want {
				fallback = "block"
			}
			p := driveStartXray(t, bin, policy, fallback, "")
			if tc.https {
				driveHTTPS(t, p, tc.want, tc.sni)
			} else {
				driveHTTP(t, p, tc.want)
			}
			t.Logf("Xray 24.11.11 protocol:[tls] sniff=%t trafficTLS=%t sni=%q: target reached=%t", tc.sniff, tc.https, tc.sni, tc.want)
		})
	}
}

func TestDriveXrayBitTorrentAndWebSocketPredicates(t *testing.T) {
	bin := driveXrayBinary(t)
	bittorrent := append([]byte("\x13BitTorrent protocol"), make([]byte, 48)...)
	malformed := bytes.Clone(bittorrent)
	malformed[1] = 'X'
	httpRequest := []byte("GET /drive HTTP/1.1\r\nHost: localhost\r\n\r\n")
	httpResponse := []byte("HTTP/1.1 204 No Content\r\n\r\n")
	wsRequest := []byte("GET /drive HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	wsResponse := []byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
	for _, tc := range []struct {
		name     string
		request  []byte
		response []byte
		protocol string
		outbound string
		sniff    bool
		want     bool
	}{
		{"bt_catchall_direct", bittorrent, bittorrent, "", "direct", true, true},
		{"bt_sniff_on_direct", bittorrent, bittorrent, "bittorrent", "direct", true, true},
		{"bt_sniff_on_block", bittorrent, bittorrent, "bittorrent", "block", true, false},
		{"bt_sniff_off_block", bittorrent, bittorrent, "bittorrent", "direct", false, false},
		{"bt_sniff_off_direct", bittorrent, bittorrent, "bittorrent", "block", false, true},
		{"http_not_bittorrent", httpRequest, httpResponse, "bittorrent", "direct", true, false},
		{"bt_bad_signature_not_detected", malformed, malformed, "bittorrent", "direct", true, false},
		{"ws_catchall_direct", wsRequest, wsResponse, "", "direct", true, true},
		{"ws_not_detected_direct_rule", wsRequest, wsResponse, "ws", "direct", true, false},
		{"ws_not_detected_block_rule", wsRequest, wsResponse, "ws", "block", true, true},
		{"ws_matches_http_direct", wsRequest, wsResponse, "http", "direct", true, true},
		{"ws_matches_http_block", wsRequest, wsResponse, "http", "block", true, false},
		{"ws_http_sniff_off_block", wsRequest, wsResponse, "http", "direct", false, false},
		{"plain_http_not_ws", httpRequest, httpResponse, "ws", "direct", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opposite := "direct"
			if tc.outbound == "direct" {
				opposite = "block"
			}
			rule := CoreRule{Protocol: []string{tc.protocol}, Outbound: tc.outbound}
			if tc.protocol == "" {
				rule = CoreRule{IP: []string{"0.0.0.0/0"}, Outbound: tc.outbound}
			}
			policy := &RoutingPolicy{CorePlane: CorePlane{
				DomainStrategy: "AsIs",
				Rules:          []CoreRule{rule, {IP: []string{"0.0.0.0/0"}, Outbound: opposite}},
				Sniffing:       Sniffing{Enabled: &tc.sniff, Protocols: []string{"http", "tls"}},
			}}
			fallback := "direct"
			if tc.want {
				fallback = "block"
			}
			p := driveStartXray(t, bin, policy, fallback, "")
			driveTCPExchange(t, p, tc.request, tc.response, tc.want)
		})
	}
}

func driveTCPExchange(t *testing.T, p *driveXray, request, response []byte, want bool) {
	t.Helper()
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var arrivals atomic.Int32
	served := make(chan error, 1)
	go func() {
		conn, err := target.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				err = nil
			}
			served <- err
			return
		}
		defer conn.Close()
		arrivals.Add(1)
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			served <- err
			return
		}
		got := make([]byte, len(request))
		if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, request) {
			served <- fmt.Errorf("target request=%x, want=%x: %v", got, request, err)
			return
		}
		_, err = io.Copy(conn, bytes.NewReader(response))
		served <- err
	}()
	t.Cleanup(func() {
		target.Close()
		if err := <-served; err != nil {
			t.Errorf("TCP responder: %v", err)
		}
		if !want && arrivals.Load() != 0 {
			t.Errorf("blocked TCP target saw %d connections", arrivals.Load())
		}
	})
	conn, err := net.DialTimeout("tcp4", p.addr, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	var method [2]byte
	if _, err := io.ReadFull(conn, method[:]); err != nil || method != [2]byte{5, 0} {
		t.Fatalf("SOCKS5 no-auth greeting: %x, %v", method, err)
	}
	connect := []byte{5, 1, 0, 1, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(connect[8:], uint16(target.Addr().(*net.TCPAddr).Port))
	if _, err := conn.Write(connect); err != nil {
		t.Fatal(err)
	}
	var reply [10]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil || !bytes.Equal(reply[:4], []byte{5, 0, 0, 1}) {
		t.Fatalf("SOCKS5 CONNECT IPv4 reply: %x, %v", reply, err)
	}
	p.alive(t)
	if _, err := io.Copy(conn, bytes.NewReader(request)); err != nil {
		t.Fatalf("write tunneled payload: %v", err)
	}
	got := make([]byte, len(response))
	_, err = io.ReadFull(conn, got)
	if want {
		if err != nil || !bytes.Equal(got, response) || arrivals.Load() != 1 {
			t.Fatalf("TCP response=%x, arrivals=%d, error=%v", got, arrivals.Load(), err)
		}
	} else {
		var timeout net.Error
		if err == nil || (errors.As(err, &timeout) && timeout.Timeout()) {
			t.Fatalf("want explicit connection failure, not delivery or timeout: %v", err)
		}
		select {
		case <-time.After(150 * time.Millisecond):
		case <-p.done:
			p.alive(t)
		}
		if arrivals.Load() != 0 {
			t.Fatal("blocked payload connected to target")
		}
	}
	p.alive(t)
}

func driveXrayBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("NEXORA_XRAY_BIN")
	if bin == "" {
		t.Skip("set NEXORA_XRAY_BIN=E:/NEXORA/tmp/xray-extract/xray.exe to run loopback probes")
	}
	if !filepath.IsAbs(bin) {
		t.Fatal("NEXORA_XRAY_BIN must be an absolute path to Xray 24.11.11")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "version")
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "Xray 24.11.11 ") {
		t.Fatalf("expected pinned Xray 24.11.11: %v\n%s", err, out)
	}
	return bin
}

type driveXray struct {
	addr    string
	done    chan struct{}
	waitErr error
}

func (p *driveXray) alive(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
		t.Fatalf("Xray exited unexpectedly: %v", p.waitErr)
	default:
	}
}

func driveStartXray(t *testing.T, bin string, policy *RoutingPolicy, fallback, omit string) *driveXray {
	t.Helper()
	probePolicy := *policy
	probePolicy.CorePlane.Rules = append([]CoreRule(nil), policy.CorePlane.Rules...)
	for i := range probePolicy.CorePlane.Rules {
		probePolicy.CorePlane.Rules[i].Invert = nil
		if contains(probePolicy.CorePlane.Rules[i].Protocol, "ws") {
			probePolicy.CorePlane.Rules[i].Protocol = []string{"http"}
		}
	}
	routing, err := Split(&probePolicy, nil)
	if err != nil || len(routing.Unrepresentable) != 0 {
		t.Fatalf("Split: routing=%+v, error=%v", routing, err)
	}
	// Inject unsupported predicates only for upstream controls; Compile reports them instead of emitting them.
	for i, rule := range policy.CorePlane.Rules {
		if rule.Invert != nil {
			routing.Rules[i]["invert"] = *rule.Invert
		}
		if contains(rule.Protocol, "ws") {
			routing.Rules[i]["protocol"] = rule.Protocol
		}
	}
	var reserved net.Listener
	var udp *net.UDPConn
	// UDP-first reservation avoids TCP-selected ports excluded by Windows for UDP.
	for attempt := 0; attempt < 8; attempt++ {
		udp, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatalf("reserve inbound UDP port: %v", err)
		}
		reserved, err = net.Listen("tcp4", udp.LocalAddr().String())
		if err == nil {
			break
		}
		udp.Close()
	}
	if err != nil {
		t.Fatalf("reserve shared TCP/UDP inbound port after 8 attempts: %v", err)
	}
	t.Cleanup(func() { reserved.Close(); udp.Close() })
	port := reserved.Addr().(*net.TCPAddr).Port
	// An opposite default makes a silently dropped rule observable.
	guard := builtinOutbound(fallback)
	guard["tag"] = "drive-default"
	outbounds := []any{guard}
	for _, tag := range routing.BuiltIns {
		if tag != omit {
			outbounds = append(outbounds, builtinOutbound(tag))
		}
	}
	doc := Document{
		"log":       Document{"loglevel": "error", "access": "none", "dnsLog": false},
		"inbounds":  []any{inboundDocument("drive", port, "127.0.0.1", routing.Sniffing)},
		"outbounds": outbounds,
		"routing":   routingSection(routing),
	}
	data, err := Emit(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, bin, "run", "-config", path)
	cmd.WaitDelay = time.Second
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	p := &driveXray{addr: reserved.Addr().String(), done: make(chan struct{})}
	if err := udp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Xray: %v", err)
	}
	t.Cleanup(func() {
		select {
		case <-p.done:
			t.Errorf("Xray exited before cleanup: %v", p.waitErr)
		default:
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Errorf("kill owned Xray: %v", err)
			}
		}
		cancel()
		<-p.done
		if t.Failed() {
			t.Logf("Xray diagnostics:\n%s", output.String())
		}
	})
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	ready, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		p.alive(t)
		conn, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(ready, "tcp4", p.addr)
		if err == nil {
			conn.Close()
			p.alive(t)
			return p
		}
		select {
		case <-p.done:
			p.alive(t)
		case <-ready.Done():
			t.Fatalf("Xray not ready on reserved port %s (including a possible reservation race): %v", p.addr, ready.Err())
		case <-ticker.C:
		}
	}
}

func driveHTTP(t *testing.T, p *driveXray, want bool) {
	t.Helper()
	driveHTTPTarget(t, p, want, false, "")
}

func driveHTTPS(t *testing.T, p *driveXray, want bool, sni string) {
	t.Helper()
	driveHTTPTarget(t, p, want, true, sni)
}

func driveHTTPTarget(t *testing.T, p *driveXray, want bool, useTLS bool, sni string) {
	t.Helper()
	const body = "nexora loopback HTTP response"
	var requests, connections atomic.Int32
	arrived := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		io.WriteString(w, body)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
			select {
			case arrived <- struct{}{}:
			default:
			}
		}
	}
	t.Cleanup(func() {
		server.Close()
		if !want && (requests.Load() != 0 || connections.Load() != 0) {
			t.Errorf("blocked target saw %d connections and %d requests", connections.Load(), requests.Load())
		}
	})
	if server.Listener.Addr().(*net.TCPAddr).IP.String() != "127.0.0.1" {
		t.Fatal("HTTP target must bind IPv4 loopback")
	}
	if useTLS {
		server.StartTLS()
	} else {
		server.Start()
	}
	proxy := &url.URL{Scheme: "socks5", Host: p.addr}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
	if useTLS {
		// The httptest certificate is self-signed for loopback; skipping verification is the fixture, not a trust choice.
		// An empty sni leaves the IP-literal dial without an SNI — the no-SNI half of the tls measurement.
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, ServerName: sni}
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/drive", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.alive(t)
	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if !want {
		if err == nil {
			t.Fatal("blocked request returned an HTTP response")
		}
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			t.Fatalf("HTTP timeout is inconclusive, want explicit proxy/connection failure: %v", err)
		}
		select {
		case <-arrived:
			t.Fatal("blocked request connected to target")
		case <-time.After(150 * time.Millisecond):
		}
	} else {
		if err != nil {
			t.Fatalf("proxied HTTP request: %v", err)
		}
		got, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(body)+1)))
		if err != nil || resp.StatusCode != http.StatusOK || string(got) != body || requests.Load() != 1 || connections.Load() != 1 {
			t.Fatalf("HTTP status=%d body=%q requests=%d connections=%d error=%v", resp.StatusCode, got, requests.Load(), connections.Load(), err)
		}
	}
	p.alive(t)
}

func driveUDP(t *testing.T, p *driveXray, want bool) {
	t.Helper()
	target, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	var arrivals atomic.Int32
	served := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, peer, err := target.ReadFromUDP(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					err = nil
				}
				served <- err
				return
			}
			arrivals.Add(1)
			if _, err := target.WriteToUDP(append([]byte("reply:"), buf[:n]...), peer); err != nil {
				served <- err
				return
			}
		}
	}()
	t.Cleanup(func() {
		target.Close()
		if err := <-served; err != nil {
			t.Errorf("UDP responder: %v", err)
		}
		if !want && arrivals.Load() != 0 {
			t.Errorf("blocked UDP target received %d datagrams", arrivals.Load())
		}
	})
	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	control, err := (&net.Dialer{}).DialContext(ctx, "tcp4", p.addr)
	if err != nil {
		t.Fatalf("SOCKS5 control dial: %v", err)
	}
	defer control.Close()
	deadline, _ := ctx.Deadline()
	if err := control.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	var method [2]byte
	if _, err := io.ReadFull(control, method[:]); err != nil || method != [2]byte{5, 0} {
		t.Fatalf("SOCKS5 no-auth greeting: %x, %v", method, err)
	}
	associate := []byte{5, 3, 0, 1, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(associate[8:], uint16(client.LocalAddr().(*net.UDPAddr).Port))
	if _, err := control.Write(associate); err != nil {
		t.Fatal(err)
	}
	var reply [10]byte
	if _, err := io.ReadFull(control, reply[:]); err != nil || !bytes.Equal(reply[:4], []byte{5, 0, 0, 1}) {
		t.Fatalf("SOCKS5 UDP associate IPv4 reply: %x, %v", reply, err)
	}
	relay := &net.UDPAddr{IP: net.IP(reply[4:8]), Port: int(binary.BigEndian.Uint16(reply[8:]))}
	if !relay.IP.IsLoopback() || relay.Port == 0 {
		t.Fatalf("SOCKS5 returned non-loopback or unusable relay: %s", relay)
	}
	const payload = "nexora loopback UDP request"
	frame := []byte{0, 0, 0, 1, 127, 0, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(frame[8:], uint16(target.LocalAddr().(*net.UDPAddr).Port))
	frame = append(frame, payload...)
	if err := client.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if n, err := client.WriteToUDP(frame, relay); err != nil || n != len(frame) {
		t.Fatalf("send SOCKS5 UDP datagram: n=%d, error=%v", n, err)
	}
	buf := make([]byte, 1024)
	n, peer, err := client.ReadFromUDP(buf)
	if want {
		if err != nil {
			t.Fatalf("SOCKS5 UDP response: %v", err)
		}
		if !peer.IP.Equal(relay.IP) || peer.Port != relay.Port || n < 10 || !bytes.Equal(buf[:10], frame[:10]) || string(buf[10:n]) != "reply:"+payload || arrivals.Load() != 1 {
			t.Fatalf("UDP peer=%s packet=%x arrivals=%d", peer, buf[:n], arrivals.Load())
		}
	} else {
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() || arrivals.Load() != 0 {
			t.Fatalf("want no UDP delivery after successful association: packet=%x arrivals=%d error=%v", buf[:n], arrivals.Load(), err)
		}
	}
	p.alive(t)
}
