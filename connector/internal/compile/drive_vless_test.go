package compile

// The full compiled path with two VLESS servers, on loopback only: two local Xray *server* processes carry
// distinct UUIDs and distinct `sendThrough` source addresses, so the HTTP target can prove in-band which
// outbound the compiled routing actually selected — no access-log parsing, no remote server, no REALITY
// handshake. The outbounds are built by `buildOutbounds`, the routing by `Split` with the two `srv-` tags
// passed as the selected set, and the document emitted by `Emit`: the same functions stage 5 runs.
// What this does not prove, deliberately: a REALITY or TLS handshake against a remote server, browser
// integration, or production command wiring (docs/PROJECT-STATE.md §6, 2026-11-07 queue head).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDriveTwoVLESSServersSelectByRule(t *testing.T) {
	bin := driveXrayBinary(t)

	const uuidA = "6b1f6a1e-1111-4c2a-9d3e-0a0b0c0d0001"
	const uuidB = "6b1f6a1e-2222-4c2a-9d3e-0a0b0c0d0002"
	// Two VLESS servers, each identifiable at the target by the source address its `sendThrough` binds.
	srvA := driveStartVLESSServer(t, bin, uuidA, "127.0.0.11")
	srvB := driveStartVLESSServer(t, bin, uuidB, "127.0.0.12")

	idA := "0a0a0a0a-0000-4000-8000-00000000000a"
	idB := "0b0b0b0b-0000-4000-8000-00000000000b"
	tagA, tagB := ServerTag(idA), ServerTag(idB)
	if tagA == tagB {
		t.Fatal("the two server ids must mint distinct tags")
	}
	servers := []Server{
		{ID: idA, Protocol: "vless", Address: "127.0.0.1", Port: srvA.port, UserIDRef: uuidA, Network: "tcp", Security: "none"},
		{ID: idB, Protocol: "vless", Address: "127.0.0.1", Port: srvB.port, UserIDRef: uuidB, Network: "tcp", Security: "none"},
	}

	startClient := func(rules []CoreRule) *driveXray {
		t.Helper()
		outbounds, err := buildOutbounds(servers)
		if err != nil {
			t.Fatal(err)
		}
		routing, err := Split(&RoutingPolicy{CorePlane: CorePlane{DomainStrategy: "AsIs", Rules: rules}}, []string{tagA, tagB})
		if err != nil {
			t.Fatal(err)
		}
		if len(routing.Unrepresentable) != 0 {
			t.Fatalf("unrepresentable: %+v", routing.Unrepresentable)
		}
		guard := builtinOutbound("block")
		guard["tag"] = "drive-default"
		port := driveReserveLoopbackPort(t)
		doc := Document{
			"log":       Document{"loglevel": "error", "access": "none", "dnsLog": false},
			"inbounds":  []any{inboundDocument("drive", port, "127.0.0.1", nil)},
			"outbounds": append([]any{guard}, outbounds...),
			"routing":   routingSection(routing),
		}
		return driveStartDoc(t, bin, doc, port)
	}

	pickA := CoreRule{IP: []string{"127.0.0.1/32"}, Outbound: tagA}
	pickB := CoreRule{IP: []string{"127.0.0.2/32"}, Outbound: tagB}
	catchallBlock := CoreRule{IP: []string{"0.0.0.0/0"}, Outbound: "block"}

	t.Run("rules_pick_their_servers", func(t *testing.T) {
		targetA := driveTarget(t, "127.0.0.1", "served-by-A")
		targetB := driveTarget(t, "127.0.0.2", "served-by-B")
		p := startClient([]CoreRule{pickA, pickB, catchallBlock})
		driveGETVia(t, p.addr, targetA, "served-by-A", "127.0.0.11")
		driveGETVia(t, p.addr, targetB, "served-by-B", "127.0.0.12")
	})
	t.Run("swapped_rules_swap_the_servers", func(t *testing.T) {
		targetA := driveTarget(t, "127.0.0.1", "served-by-A")
		targetB := driveTarget(t, "127.0.0.2", "served-by-B")
		// Swapped *mapping*, not order: the destination now names the other server, so the same request
		// must exit through the other process.
		p := startClient([]CoreRule{{IP: []string{"127.0.0.1/32"}, Outbound: tagB}, {IP: []string{"127.0.0.2/32"}, Outbound: tagA}, catchallBlock})
		driveGETVia(t, p.addr, targetA, "served-by-A", "127.0.0.12")
		driveGETVia(t, p.addr, targetB, "served-by-B", "127.0.0.11")
	})
	t.Run("a_server_without_a_rule_is_not_a_fallback", func(t *testing.T) {
		targetA := driveTarget(t, "127.0.0.1", "served-by-A")
		targetB := driveTarget(t, "127.0.0.2", "served-by-B")
		p := startClient([]CoreRule{pickA, catchallBlock})
		driveGETVia(t, p.addr, targetA, "served-by-A", "127.0.0.11")
		driveGETViaBlocked(t, p.addr, targetB)
	})
}

// driveStartVLESSServer starts one local Xray process acting as a VLESS server: one vless inbound for the
// given UUID and a freedom outbound bound to `sendThrough`, so every connection it relays carries that
// source address at the target.
func driveStartVLESSServer(t *testing.T, bin, uuid, sendThrough string) *driveVLESSServer {
	t.Helper()
	port := driveReserveLoopbackPort(t)
	doc := Document{
		"log": Document{"loglevel": "error", "access": "none", "dnsLog": false},
		"inbounds": []any{Document{
			"tag":      "vless-in",
			"port":     port,
			"protocol": "vless",
			"listen":   "127.0.0.1",
			"settings": Document{
				"clients":    []any{Document{"id": uuid}},
				"decryption": "none",
			},
		}},
		"outbounds": []any{Document{"tag": "direct", "protocol": "freedom", "sendThrough": sendThrough}},
	}
	return &driveVLESSServer{p: driveStartDoc(t, bin, doc, port), port: port}
}

// driveVLESSServer pairs the running process with the port its inbound was reserved on.
type driveVLESSServer struct {
	p    *driveXray
	port int
}

// driveTarget binds an HTTP server on one loopback address and records the host part of the last request's
// remote address — the observable that proves which server carried the connection.
func driveTarget(t *testing.T, ip, body string) *driveTargetServer {
	t.Helper()
	target := &driveTargetServer{seen: &driveAtomicString{}}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			target.seen.set(host)
		}
		io.WriteString(w, body)
	}))
	listener, err := net.Listen("tcp4", ip+":0")
	if err != nil {
		t.Fatalf("bind target on %s: %v", ip, err)
	}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	target.url = u
	return target
}

// driveTargetServer pairs the running HTTP server with the recorder its handler fills.
type driveTargetServer struct {
	url  *url.URL
	seen *driveAtomicString
}

type driveAtomicString struct {
	mu  sync.Mutex
	val string
}

func (s *driveAtomicString) set(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.val = v
}

func (s *driveAtomicString) get() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.val
}

// driveGETVia performs one real HTTP request through the SOCKS5 inbound of a started client config and
// asserts the body, the status and the source address the target recorded.
func driveGETVia(t *testing.T, proxyAddr string, target *driveTargetServer, wantBody, wantSource string) {
	t.Helper()
	got := driveGET(t, proxyAddr, target.url)
	if got.err != nil || got.status != http.StatusOK || got.body != wantBody {
		t.Fatalf("GET %s through %s: status=%d body=%q error=%v", target.url, proxyAddr, got.status, got.body, got.err)
	}
	if source := target.seen.get(); source != wantSource {
		t.Fatalf("GET %s: target saw source %q, want %s — the compiled routing picked the wrong server", target.url, source, wantSource)
	}
}

// driveGETViaBlocked asserts the catchall: the request must fail with an explicit proxy error, never a
// timeout, and the target must have recorded no arrival.
func driveGETViaBlocked(t *testing.T, proxyAddr string, target *driveTargetServer) {
	t.Helper()
	got := driveGET(t, proxyAddr, target.url)
	if got.err == nil {
		t.Fatalf("GET %s through %s: blocked catchall returned status=%d body=%q", target.url, proxyAddr, got.status, got.body)
	}
	var timeout net.Error
	if errors.Is(got.err, context.DeadlineExceeded) || (errors.As(got.err, &timeout) && timeout.Timeout()) {
		t.Fatalf("blocked request timed out instead of failing at the proxy: %v", got.err)
	}
	if source := target.seen.get(); source != "" {
		t.Fatalf("blocked catchall connected to the target from %s", source)
	}
}

type driveGETResult struct {
	status int
	body   string
	err    error
}

func driveGET(t *testing.T, proxyAddr string, target *url.URL) driveGETResult {
	t.Helper()
	res := driveGETResult{}
	proxy := &url.URL{Scheme: "socks5", Host: proxyAddr}
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String()+"/drive", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		res.err = err
		return res
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		res.err = err
		return res
	}
	res.status = resp.StatusCode
	res.body = string(bytes.TrimRight(body, "\x00"))
	return res
}

// driveReserveLoopbackPort reserves an IPv4 loopback TCP port and releases it for the config to bind.
func driveReserveLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

// driveStartDoc starts one Xray process on an emitted document and waits until its inbound accepts
// connections. It is the transport half of driveStartXray, factored for documents this package composes
// directly; the process it starts is owned by the test and killed on cleanup.
func driveStartDoc(t *testing.T, bin string, doc Document, port int) *driveXray {
	t.Helper()
	data, err := Emit(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + itoa(port)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, bin, "run", "-config", path)
	cmd.WaitDelay = time.Second
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	p := &driveXray{addr: addr, done: make(chan struct{})}
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
			t.Fatalf("Xray not ready on port %s: %v\n%s", p.addr, ready.Err(), output.String())
		case <-ticker.C:
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [6]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
