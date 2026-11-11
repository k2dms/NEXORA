package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/keystore"
)

const subscriptionTestID = "bbbb2222-1111-4222-8333-444444444444"  // nexora-secret-allow: synthetic source identity
const subscriptionOtherID = "cccc3333-1111-4222-8333-444444444444" // nexora-secret-allow: synthetic source identity

type subscriptionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f subscriptionRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func subscriptionTestSource() map[string]any {
	return map[string]any{
		"id": subscriptionTestID, "displayName": "Test feed", "url": "https://feed.example.test/list",
		"format": "auto", "keepLastGood": true,
		"limits":   map[string]any{"maxBytes": 2097152, "maxRedirects": 3, "timeoutMs": 1000, "maxServers": 1024},
		"schedule": map[string]any{"intervalSeconds": 300, "jitterPercent": 0},
	}
}

func subscriptionTestFrame(sources ...map[string]any) *envelope.Frame {
	list := make([]any, len(sources))
	for i, source := range sources {
		list[i] = source
	}
	return subscriptionTestArgs(map[string]any{"sources": list})
}

func subscriptionTestArgs(args any) *envelope.Frame {
	data, _ := json.Marshal(args)
	method := "subscription.refresh"
	return &envelope.Frame{Method: &method, Args: data}
}

func subscriptionTestHandler(t *testing.T) (*subscriptionRefresh, *importTestProtector) {
	t.Helper()
	p := &importTestProtector{}
	store, err := keystore.New(t.TempDir(), p)
	if err != nil {
		t.Fatal(err)
	}
	h, err := newSubscriptionRefresh(store)
	if err != nil {
		t.Fatal(err)
	}
	h.testLookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP("2001:db8::10")}, nil
	}
	return h, p
}

func subscriptionTestBody(host string) string {
	return "vless://" + importUser + "@" + host + ":443?type=tcp&security=none#Test%20server"
}

func subscriptionTestResponse(r *http.Request, code int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = http.Header{"Content-Type": {"text/plain"}}
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: headers, Request: r}
}

func subscriptionTestFeed(h *subscriptionRefresh, body string) {
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return subscriptionTestResponse(r, 200, body, nil), nil
	})
}

func subscriptionTestAnswer(t *testing.T, h *subscriptionRefresh, frame *envelope.Frame) *subscriptionRefreshAnswer {
	t.Helper()
	value, refusal := h.Handle(context.Background(), frame)
	if refusal != nil {
		t.Fatalf("refresh refused: %v", refusal)
	}
	answer, ok := value.(*subscriptionRefreshAnswer)
	if !ok {
		t.Fatal("wrong answer type")
	}
	subscriptionAssertContract(t, answer)
	return answer
}

// Assert the actual command's closed property lists, required keys and enums,
// and every returned Server using the shared schema boundary.
func subscriptionAssertContract(t *testing.T, answer *subscriptionRefreshAnswer) {
	t.Helper()
	path, err := filepath.Abs("../../../packages/schema/protocol/commands/subscription.refresh.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var command map[string]any
	if err := json.Unmarshal(data, &command); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(answer)
	if err != nil || len(encoded) > subscriptionOutputLimit {
		t.Fatal("answer not bounded JSON")
	}
	value, valid := decodeImportJSON(encoded)
	if !valid {
		t.Fatal("answer invalid JSON")
	}
	var check func(any, map[string]any)
	check = func(value any, schema map[string]any) {
		if schema["$ref"] != nil {
			if !validateImportServer(value) {
				t.Fatal("returned server fails schema")
			}
			return
		}
		if choices, ok := schema["enum"].([]any); ok {
			found := false
			for _, choice := range choices {
				found = found || choice == value
			}
			if !found {
				t.Fatal("result enum mismatch")
			}
		}
		switch schema["type"] {
		case "object":
			obj, ok := value.(map[string]any)
			if !ok {
				t.Fatal("result object absent")
			}
			properties := schema["properties"].(map[string]any)
			for _, key := range schema["required"].([]any) {
				if _, present := obj[key.(string)]; !present {
					t.Fatal("result required property absent")
				}
			}
			for key, item := range obj {
				property, known := properties[key]
				if !known {
					t.Fatal("result contains undeclared property")
				}
				check(item, property.(map[string]any))
			}
		case "array":
			list, ok := value.([]any)
			if !ok || len(list) > int(schema["maxItems"].(float64)) {
				t.Fatal("result array missing or over quota")
			}
			seen := map[string]bool{}
			for _, item := range list {
				check(item, schema["items"].(map[string]any))
				if schema["uniqueItems"] == true {
					text := item.(string)
					if seen[text] {
						t.Fatal("result repeats a source identity")
					}
					seen[text] = true
				}
			}
		case "integer":
			if !importInteger(int64(schema["minimum"].(float64)), 4096)(value) {
				t.Fatal("result integer invalid")
			}
		case "boolean":
			if !importBool(value) {
				t.Fatal("result bool invalid")
			}
		case "string":
			if !importString(0, -1, "")(value) {
				t.Fatal("result string invalid")
			}
			if pattern, ok := schema["pattern"].(string); ok && !importString(0, -1, pattern)(value) {
				t.Fatal("result pattern invalid")
			}
		default: // nextAttemptAt is string|null in the contract.
			if value != nil && schema["format"] == "date-time" && !importDateTime(value) {
				t.Fatal("result timestamp invalid")
			}
		}
	}
	check(value, command["result"].(map[string]any))
}

func TestSubscriptionConstructorAndSingleBatchPreflight(t *testing.T) {
	if h, err := newSubscriptionRefresh(nil); h != nil || err == nil {
		t.Fatal("constructor accepted no keystore")
	}
	h, p := subscriptionTestHandler(t)
	production, err := newSubscriptionRefresh(h.store)
	if err != nil || production.testTransport != nil || production.testLookupIP != nil {
		t.Fatal("production constructor installed test hooks")
	}
	first, second := subscriptionTestSource(), subscriptionTestSource()
	second["id"] = subscriptionOtherID
	calls := 0
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if p.calls != 0 {
			t.Fatal("secrets were written before every source finished preflight")
		}
		return subscriptionTestResponse(r, 200, subscriptionTestBody("server.example.test"), nil), nil
	})
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(first, second))
	if calls != 2 || len(answer.Refreshed) != 2 || p.calls != 2 || len(testImportDisk(t, h.store)) != 2 {
		t.Fatal("multi-source batch did not finish once with two protected credentials")
	}
	if answer.Refreshed[0].Servers[0]["id"] == answer.Refreshed[1].Servers[0]["id"] {
		t.Fatal("two source identities collided")
	}
}

func TestSubscriptionSourceClosedShape(t *testing.T) {
	source := subscriptionTestSource()
	source["urlRef"] = "keystore://subscription/" + subscriptionTestID + "/url"
	source["headersRef"] = "keystore://subscription/" + subscriptionTestID + "/headers"
	source["serverIds"] = []any{importServerID}
	source["createdAt"], source["updatedAt"] = "2026-11-01T12:30:00Z", "2026-11-01t12:30:00z"
	source["schedule"].(map[string]any)["backoffMultiplier"] = 1.75
	source["schedule"].(map[string]any)["maxIntervalSeconds"] = 86400
	source["status"] = map[string]any{
		"lastAttemptAt": "2026-11-01T12:30:00Z", "lastSuccessAt": "2026-11-01T12:30:00Z",
		"lastSha256": strings.Repeat("a", 64), "serverCount": 1, "consecutiveFailures": 2,
		"nextAttemptAt": "2026-11-01T12:30:00Z",
		"lastError":     map[string]any{"code": "AuthFailed", "at": "2026-11-01T12:30:00Z", "detail": "public error"},
	}
	value, ok := decodeImportJSON(subscriptionTestFrame(source).Args)
	if !ok || !subscriptionArgsShape(value) {
		t.Fatal("full existing Source schema rejected")
	}
	for _, format := range []string{"auto", "vless-uri-list", "vless-base64", "v2ray-json", "clash-yaml"} {
		source["format"] = format
		source["status"].(map[string]any)["lastError"] = nil
		value, _ := decodeImportJSON(subscriptionTestFrame(source).Args)
		if !subscriptionArgsShape(value) {
			t.Fatal("existing format/null lastError rejected")
		}
	}
}

func TestSubscriptionInvalidArgsBeforeAnyFetchOrWrite(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"unknown source":   func(s map[string]any) { s["headers"] = map[string]any{"Authorization": "secret-marker"} },
		"unknown limits":   func(s map[string]any) { s["limits"].(map[string]any)["unused"] = true },
		"unknown schedule": func(s map[string]any) { s["schedule"].(map[string]any)["unused"] = true },
		"unknown status":   func(s map[string]any) { s["status"] = map[string]any{"consecutiveFailures": 0, "unused": true} },
		"unknown error": func(s map[string]any) {
			s["status"] = map[string]any{"consecutiveFailures": 0, "lastError": map[string]any{"code": "AuthFailed", "unused": true}}
		},
		"missing required":   func(s map[string]any) { delete(s, "keepLastGood") },
		"null required":      func(s map[string]any) { s["displayName"] = nil },
		"false lastgood":     func(s map[string]any) { s["keepLastGood"] = false },
		"bad id":             func(s map[string]any) { s["id"] = strings.ToUpper(subscriptionTestID) },
		"wrong number":       func(s map[string]any) { s["limits"].(map[string]any)["maxBytes"] = "1024" },
		"fraction":           func(s map[string]any) { s["limits"].(map[string]any)["maxServers"] = 1.5 },
		"too many redirects": func(s map[string]any) { s["limits"].(map[string]any)["maxRedirects"] = 6 },
		"bad multiplier":     func(s map[string]any) { s["schedule"].(map[string]any)["backoffMultiplier"] = 1.49 },
		"bad timestamp":      func(s map[string]any) { s["updatedAt"] = "2026-02-30T00:00:00Z" },
		"unknown error code": func(s map[string]any) {
			s["status"] = map[string]any{"consecutiveFailures": 0, "lastError": map[string]any{"code": "secret-marker"}}
		},
		"status count":         func(s map[string]any) { s["status"] = map[string]any{"consecutiveFailures": 1001} },
		"duplicate members":    func(s map[string]any) { s["serverIds"] = []any{importServerID, importServerID} },
		"public token":         func(s map[string]any) { s["url"] = "https://feed.example.test/?ToKeN=secret-marker" },
		"encoded public token": func(s map[string]any) { s["url"] = "https://feed.example.test/?%74oken=secret-marker" },
		"http":                 func(s map[string]any) { s["url"] = "http://feed.example.test/" },
		"userinfo":             func(s map[string]any) { s["url"] = "https://secret-marker@feed.example.test/" },
		"bad URI":              func(s map[string]any) { s["url"] = "https://[invalid/" },
		"null ref":             func(s map[string]any) { s["urlRef"] = nil },
		"prefix owner":         func(s map[string]any) { s["urlRef"] = "keystore://subscription/" + subscriptionTestID[:8] + "/url" },
		"wrong entity":         func(s map[string]any) { s["urlRef"] = "keystore://server/" + subscriptionTestID + "/url" },
		"wrong owner": func(s map[string]any) {
			s["headersRef"] = "keystore://subscription/" + subscriptionTestID + "/headers"
		},
		"wrong field": func(s map[string]any) { s["headersRef"] = "keystore://subscription/" + subscriptionTestID + "/url" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			h, protector := subscriptionTestHandler(t)
			h.testTransport = subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid request fetched")
				return nil, nil
			})
			badSource := subscriptionTestSource()
			badSource["id"] = subscriptionOtherID
			mutate(badSource)
			value, refusal := h.Handle(context.Background(), subscriptionTestFrame(subscriptionTestSource(), badSource))
			if value != nil || refusal == nil || refusal.Code != "BadRequest" || protector.calls != 0 {
				t.Fatalf("invalid args not rejected before work: %v", refusal)
			}
		})
	}
	for name, frame := range map[string]*envelope.Frame{
		"nil": nil, "empty": {}, "null": {Args: json.RawMessage("null")},
		"empty sources":     subscriptionTestArgs(map[string]any{"sources": []any{}}),
		"unknown args":      subscriptionTestArgs(map[string]any{"sources": []any{subscriptionTestSource()}, "unexpected": true}),
		"bad force":         subscriptionTestArgs(map[string]any{"sources": []any{subscriptionTestSource()}, "force": "true"}),
		"duplicate sources": subscriptionTestFrame(subscriptionTestSource(), subscriptionTestSource()),
		"duplicate key":     {Args: json.RawMessage(`{"sources":[],"sources":[]}`)},
		"trailing json":     {Args: json.RawMessage(`{"sources":[]} {}`)},
	} {
		t.Run(name, func(t *testing.T) {
			h, p := subscriptionTestHandler(t)
			_, refusal := h.Handle(context.Background(), frame)
			if refusal == nil || refusal.Code != "BadRequest" || p.calls != 0 {
				t.Fatal("malformed args accepted")
			}
		})
	}
	tooMany := make([]any, 65)
	for i := range tooMany {
		s := subscriptionTestSource()
		s["id"] = fmt.Sprintf("%08x-1111-4222-8333-444444444444", i)
		tooMany[i] = s
	}
	h, p := subscriptionTestHandler(t)
	if _, bad := h.Handle(context.Background(), subscriptionTestArgs(map[string]any{"sources": tooMany})); bad == nil || bad.Code != "BadRequest" || p.calls != 0 {
		t.Fatal("source count not bounded")
	}
}

func TestSubscriptionGuardIPsAndMixedDNS(t *testing.T) {
	for _, text := range []string{
		"0.0.0.0", "::", "127.0.0.1", "127.99.1.2", "::1", "::ffff:127.0.0.1",
		"169.254.169.254", "169.254.1.1", "fe80::1", "ff02::1", "224.0.0.1", "255.255.255.255",
		"100.100.100.200", "168.63.129.16", "192.0.0.192", "fd00:ec2::254",
	} {
		t.Run(text, func(t *testing.T) {
			if subscriptionSafeIP(net.ParseIP(text)) {
				t.Fatal("prohibited IP accepted")
			}
			h, p := subscriptionTestHandler(t)
			h.testLookupIP = func(context.Context, string, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("192.0.2.10"), net.ParseIP(text)}, nil
			}
			h.testTransport = subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("mixed public/prohibited DNS reached transport")
				return nil, nil
			})
			answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
			if len(answer.Failed) != 1 || answer.Failed[0].Code != "NetworkUnreachable" || p.calls != 0 {
				t.Fatal("guard did not fail without writes")
			}
		})
	}
	for _, text := range []string{"10.1.2.3", "172.16.1.2", "192.168.1.2", "fc00::1", "192.0.2.10", "2001:db8::10"} {
		if !subscriptionSafeIP(net.ParseIP(text)) {
			t.Fatal("legitimate self-hosted/private feed address rejected")
		}
	}
	if subscriptionSafeIP(nil) {
		t.Fatal("nil address accepted")
	}
	for name, ips := range map[string][]net.IP{"empty": {}, "nil item": {nil}} {
		t.Run(name, func(t *testing.T) {
			h, _ := subscriptionTestHandler(t)
			h.testLookupIP = func(context.Context, string, string) ([]net.IP, error) { return ips, nil }
			if _, err := h.guardedIPs(context.Background(), "feed.example.test"); err == nil {
				t.Fatal("empty/invalid resolution accepted")
			}
		})
	}
}

func TestSubscriptionRedirectGuardLimitsAndHeaderOrigins(t *testing.T) {
	for name, target := range map[string]string{
		"metadata": "https://169.254.169.254/feed", "loopback": "https://[::1]/feed",
		"http": "http://next.example.test/feed", "userinfo": "https://secret-marker@next.example.test/feed",
	} {
		t.Run(name, func(t *testing.T) {
			h, p := subscriptionTestHandler(t)
			calls := 0
			h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return subscriptionTestResponse(r, 302, "secret-body", http.Header{"Location": {target}}), nil
			})
			answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
			if calls != 1 || p.calls != 0 || answer.Failed[0].Code != "NetworkUnreachable" {
				t.Fatal("unsafe redirect followed")
			}
		})
	}
	for _, maximum := range []int{0, 1, 3, 5} {
		t.Run(fmt.Sprintf("limit%d", maximum), func(t *testing.T) {
			h, p := subscriptionTestHandler(t)
			calls, lookups := 0, 0
			h.testLookupIP = func(context.Context, string, string) ([]net.IP, error) {
				lookups++
				return []net.IP{net.ParseIP("192.0.2.10")}, nil
			}
			h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return subscriptionTestResponse(r, 307, "", http.Header{"Location": {"/again"}}), nil
			})
			source := subscriptionTestSource()
			source["limits"].(map[string]any)["maxRedirects"] = maximum
			answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
			if calls != maximum+1 || lookups != calls || p.calls != 0 || len(answer.Failed) != 1 {
				t.Fatal("redirect count or per-hop guard wrong")
			}
		})
	}
	h, _ := subscriptionTestHandler(t)
	calls := 0
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls <= 2 && (r.Header.Get("Authorization") != "secret-header-marker" || r.Header.Get("X-Api-Key") != "custom-secret-marker") {
			t.Fatal("same-origin header lost")
		}
		if calls >= 3 && (r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != "") {
			t.Fatal("secret header crossed an origin or was reattached later")
		}
		switch calls {
		case 1:
			return subscriptionTestResponse(r, 301, "", http.Header{"Location": {"/relative"}}), nil
		case 2:
			return subscriptionTestResponse(r, 303, "", http.Header{"Location": {"https://other.example.test/list"}}), nil
		case 3:
			return subscriptionTestResponse(r, 308, "", http.Header{"Location": {"https://feed.example.test/list"}}), nil
		default:
			return subscriptionTestResponse(r, 200, subscriptionTestBody("server.example.test"), nil), nil
		}
	})
	source := subscriptionTestSource()
	ref := "keystore://subscription/" + subscriptionTestID + "/headers"
	if err := h.store.Put(ref, []byte(`{"Authorization":"secret-header-marker","X-Api-Key":"custom-secret-marker"}`)); err != nil {
		t.Fatal(err)
	}
	source["headersRef"] = ref
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if len(answer.Refreshed) != 1 || calls != 4 {
		t.Fatal("valid bounded redirect chain failed")
	}
}

func TestSubscriptionResolvedURLAndHeaderBoundary(t *testing.T) {
	h, _ := subscriptionTestHandler(t)
	source := subscriptionTestSource()
	ref := "keystore://subscription/" + subscriptionTestID + "/url"
	secretURL := "https://feed.example.test/private-path-marker?token=secret-url-marker"
	if err := h.store.Put(ref, []byte(secretURL)); err != nil {
		t.Fatal(err)
	}
	source["urlRef"] = ref
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != secretURL {
			t.Fatal("resolved URL was not used")
		}
		return subscriptionTestResponse(r, 200, subscriptionTestBody("server.example.test"), nil), nil
	})
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	data, _ := json.Marshal(answer)
	for _, marker := range []string{secretURL, "secret-url-marker", "private-path-marker", importUser} {
		if bytes.Contains(data, []byte(marker)) {
			t.Fatal("secret URL or credential in output")
		}
	}
	for _, address := range []string{"http://feed.example.test/", "https://secret@feed.example.test/", "https://feed.example.test/\nsecret"} {
		if err := h.store.Put(ref, []byte(address)); err != nil {
			t.Fatal(err)
		}
		answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
		if len(answer.Failed) != 1 || answer.Failed[0].Code != "InvalidProfile" {
			t.Fatal("invalid resolved URL accepted")
		}
	}
	for name, headers := range map[string]string{
		"Host": `{"Host":"127.0.0.1"}`, "newline value": `{"Authorization":"secret\r\nInjected: yes"}`,
		"newline name": `{"X-Bad\r\nInjected":"secret"}`, "invalid name": `{"X Bad":"secret"}`,
		"case aliases": `{"Authorization":"one","authorization":"two"}`,
		"proxy auth":   `{"Proxy-Authorization":"secret"}`, "connection": `{"Connection":"upgrade"}`,
		"length": `{"Content-Length":"999"}`, "compression": `{"Accept-Encoding":"gzip"}`,
		"non-string": `{"Authorization":123}`, "empty": `{}`, "duplicate JSON": `{"Authorization":"one","Authorization":"two"}`,
	} {
		t.Run(name, func(t *testing.T) {
			h, p := subscriptionTestHandler(t)
			ref := "keystore://subscription/" + subscriptionTestID + "/headers"
			if err := h.store.Put(ref, []byte(headers)); err != nil {
				t.Fatal(err)
			}
			before := p.calls
			source := subscriptionTestSource()
			source["headersRef"] = ref
			h.testTransport = subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("unsafe stored headers reached transport")
				return nil, nil
			})
			answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
			if len(answer.Failed) != 1 || answer.Failed[0].Code != "InvalidProfile" || p.calls != before {
				t.Fatal("unsafe header not rejected without writes")
			}
		})
	}
	if _, ok := subscriptionHeaders(map[string]any{"X-Long": strings.Repeat("a", 4097)}); ok {
		t.Fatal("header value not bounded")
	}
	many := map[string]any{}
	for i := 0; i < 33; i++ {
		many[fmt.Sprintf("X-%d", i)] = "value"
	}
	if _, ok := subscriptionHeaders(many); ok {
		t.Fatal("header count not bounded")
	}
}

type subscriptionCountingBody struct {
	read, closed int
}

func (r *subscriptionCountingBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.read += len(p)
	return len(p), nil
}
func (r *subscriptionCountingBody) Close() error { r.closed++; return nil }

func TestSubscriptionDNSDeadlineAndExactStreamLimit(t *testing.T) {
	h, p := subscriptionTestHandler(t)
	h.testLookupIP = func(ctx context.Context, _, _ string) ([]net.IP, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	h.testTransport = subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("timed-out DNS reached transport")
		return nil, nil
	})
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
	if answer.Failed[0].Code != "Timeout" || p.calls != 0 {
		t.Fatal("DNS did not honor the source timeout")
	}
	h, p = subscriptionTestHandler(t)
	body := subscriptionTestBody("server.example.test")
	body += strings.Repeat(" ", 1024-len(body))
	subscriptionTestFeed(h, body)
	source := subscriptionTestSource()
	source["limits"].(map[string]any)["maxBytes"] = 1024
	answer = subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if len(answer.Refreshed) != 1 || p.calls != 1 {
		t.Fatal("body exactly at stream limit rejected")
	}
}

func TestSubscriptionStreamLimitAndHTML(t *testing.T) {
	h, p := subscriptionTestHandler(t)
	body := &subscriptionCountingBody{}
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: http.Header{}, Request: r}, nil
	})
	source := subscriptionTestSource()
	source["limits"].(map[string]any)["maxBytes"] = 1024
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if body.read != 1025 || body.closed != 1 || p.calls != 0 || answer.Failed[0].Code != "QuotaExceeded" || answer.Failed[0].Retryable {
		t.Fatal("stream limit not enforced at maxBytes+1")
	}
	for name, response := range map[string]struct{ contentType, body string }{
		"html mime":  {"text/html; charset=utf-8", subscriptionTestBody("server.example.test")},
		"xhtml mime": {"application/xhtml+xml", "secret-body-marker"},
		"sniff html": {"text/plain", "  <!DOCTYPE HTML><body>secret-body-marker"},
		"sniff tag":  {"", "\n<HTML>secret-body-marker"},
	} {
		t.Run(name, func(t *testing.T) {
			h, p := subscriptionTestHandler(t)
			h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				return subscriptionTestResponse(r, 200, response.body, http.Header{"Content-Type": {response.contentType}}), nil
			})
			answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
			if answer.Failed[0].Code != "SchemaMismatch" || p.calls != 0 {
				t.Fatal("HTML feed accepted")
			}
		})
	}
}

func TestSubscriptionProductionTransportPinnedTLSAndNoProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	u, _ := url.Parse("https://feed.example.test/list")
	transport := subscriptionTransport(u, []net.IP{net.ParseIP("192.0.2.10")})
	defer transport.CloseIdleConnections()
	if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.RootCAs != nil || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || transport.TLSClientConfig.ServerName != "feed.example.test" {
		t.Fatal("production transport can bypass proxy/TLS guards")
	}
	for _, target := range []string{"other.example.test:443", "feed.example.test:80", "127.0.0.1:443"} {
		if conn, err := transport.DialContext(context.Background(), "tcp", target); err == nil || conn != nil {
			t.Fatal("unpinned transport target accepted")
		}
	}
	// A canceled dial goes directly to the validated IP even for an unresolvable
	// hostname. A net.OpError carrying that IP proves no second DNS lookup.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := transport.DialContext(ctx, "tcp", "feed.example.test:443")
	var op *net.OpError
	if !errors.As(err, &op) || op.Addr == nil || op.Addr.String() != "192.0.2.10:443" {
		t.Fatalf("dial was not pinned to validated IP: %T", err)
	}
	// Exercise the production TLS config against a local synthetic server. Only
	// the test's dialer reaches loopback; production's guard is not weakened.
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted TLS request reached HTTP") }))
	defer server.Close()
	local, _ := url.Parse(server.URL)
	localTransport := subscriptionTransport(local, []net.IP{net.ParseIP("192.0.2.10")})
	defer localTransport.CloseIdleConnections()
	localTransport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, local.Host)
	}
	client := &http.Client{Transport: localTransport, Timeout: time.Second}
	_, err = client.Get(server.URL)
	var unknown x509.UnknownAuthorityError
	if err == nil || !errors.As(err, &unknown) {
		t.Fatal("self-signed TLS certificate accepted")
	}
}

func TestSubscriptionSuccessBatchRefsHashUnchangedAndForce(t *testing.T) {
	h, p := subscriptionTestHandler(t)
	raw := subscriptionTestBody("one.example.test") + "\n" + subscriptionTestBody("two.example.test") + "\n"
	body := base64.StdEncoding.EncodeToString([]byte(raw))
	subscriptionTestFeed(h, body)
	source := subscriptionTestSource()
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if len(answer.Refreshed) != 1 || len(answer.Failed) != 0 || answer.Refreshed[0].Format != "vless-base64" || answer.Refreshed[0].ServerCount != 2 || p.calls != 2 {
		t.Fatal("successful feed did not extract exactly two credentials")
	}
	hash := sha256.Sum256([]byte(body))
	if answer.Refreshed[0].SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatal("hash not computed from raw response bytes")
	}
	for _, server := range answer.Refreshed[0].Servers {
		ref := server["userIdRef"].(string)
		secret, err := h.store.Resolve(ref)
		if err != nil || string(secret.Value) != importUser {
			t.Fatal("returned reference is unusable")
		}
	}
	encoded, _ := json.Marshal(answer)
	if bytes.Contains(encoded, []byte(importUser)) || bytes.Contains(encoded, []byte(body)) || bytes.Contains(encoded, []byte("feed.example.test")) {
		t.Fatal("secret/raw feed/source URL leaked")
	}
	before := testImportDisk(t, h.store)
	if len(before) != 2 {
		t.Fatal("feed or state persisted alongside protected credentials")
	}
	source["status"] = map[string]any{"consecutiveFailures": 0, "lastSha256": answer.Refreshed[0].SHA256}
	answer = subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if !reflect.DeepEqual(answer.Unchanged, []string{subscriptionTestID}) || len(answer.Refreshed) != 0 || p.calls != 2 || !reflect.DeepEqual(before, testImportDisk(t, h.store)) {
		t.Fatal("unchanged feed wrote or resent servers")
	}
	frame := subscriptionTestArgs(map[string]any{"sources": []any{source}, "force": true})
	answer = subscriptionTestAnswer(t, h, frame)
	if len(answer.Refreshed) != 1 || p.calls != 4 {
		t.Fatal("force did not bypass hash comparison")
	}
}

func TestSubscriptionUnsupportedFormatsAndEmptyFeed(t *testing.T) {
	for _, format := range []string{"v2ray-json", "clash-yaml"} {
		t.Run(format, func(t *testing.T) {
			h, p := subscriptionTestHandler(t)
			h.testTransport = subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("unsupported declared format fetched")
				return nil, nil
			})
			source := subscriptionTestSource()
			source["format"] = format
			answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
			if answer.Failed[0].Code != "SchemaMismatch" || answer.Failed[0].Retryable || p.calls != 0 {
				t.Fatal("unsupported format not explicit failure")
			}
		})
	}
	for _, body := range []string{"", "\n# empty\n", "secret-body-marker", `{"outbounds":[]}`} {
		h, p := subscriptionTestHandler(t)
		subscriptionTestFeed(h, body)
		answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
		if answer.Failed[0].Code != "SchemaMismatch" || p.calls != 0 || len(answer.Refreshed) != 0 {
			t.Fatal("empty/unparseable feed became successful deletion")
		}
	}
}

func TestSubscriptionBackoffFailuresAuthAndTimeoutSanitized(t *testing.T) {
	h, p := subscriptionTestHandler(t)
	source := subscriptionTestSource()
	source["status"] = map[string]any{"consecutiveFailures": 999, "nextAttemptAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	calls := 0
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return subscriptionTestResponse(r, 403, "secret-body-marker", nil), nil
	})
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if !reflect.DeepEqual(answer.Skipped, []string{subscriptionTestID}) || calls != 0 || p.calls != 0 {
		t.Fatal("backoff source fetched or written")
	}
	answer = subscriptionTestAnswer(t, h, subscriptionTestArgs(map[string]any{"sources": []any{source}, "force": true}))
	if calls != 1 || answer.Failed[0].Code != "AuthFailed" || answer.Failed[0].ConsecutiveFailures != 1000 {
		t.Fatal("forced auth failure/backoff clamp wrong")
	}
	for _, code := range []int{401, 403, 404, 429, 500} {
		h, p := subscriptionTestHandler(t)
		h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			return subscriptionTestResponse(r, code, "secret-body-marker", nil), nil
		})
		answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
		want := "NetworkUnreachable"
		if code == 401 || code == 403 {
			want = "AuthFailed"
		}
		encoded, _ := json.Marshal(answer)
		if answer.Failed[0].Code != want || p.calls != 0 || bytes.Contains(encoded, []byte("secret-body-marker")) {
			t.Fatal("HTTP code wrong or body echoed")
		}
	}
	for name, fail := range map[string]error{
		"network":  errors.New("https://feed.example.test/?token=secret-network-marker"),
		"timeout":  &net.DNSError{Err: "secret-timeout-marker", IsTimeout: true},
		"deadline": context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			h, p := subscriptionTestHandler(t)
			h.testTransport = subscriptionRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fail })
			answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
			want := "Timeout"
			if name == "network" {
				want = "NetworkUnreachable"
			}
			encoded, _ := json.Marshal(answer)
			if answer.Failed[0].Code != want || !answer.Failed[0].Retryable || p.calls != 0 || bytes.Contains(encoded, []byte("secret-")) || bytes.Contains(encoded, []byte("https://")) {
				t.Fatal("network failure unsanitized or misclassified")
			}
		})
	}
	h, _ = subscriptionTestHandler(t)
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	answer = subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
	if answer.Failed[0].Code != "Timeout" {
		t.Fatal("source timeout not enforced")
	}
	// Declared schedule, exponent and cap, with deterministic zero jitter.
	value, _ := decodeImportJSON(subscriptionTestFrame(subscriptionTestSource()).Args)
	doc := value.(map[string]any)["sources"].([]any)[0].(map[string]any)
	doc["status"] = map[string]any{"consecutiveFailures": json.Number("2")}
	doc["schedule"] = map[string]any{"intervalSeconds": json.Number("300"), "backoffMultiplier": json.Number("1.5"), "jitterPercent": json.Number("0")}
	start := time.Now()
	failure := subscriptionFailure(doc, "NetworkUnreachable")
	delta := subscriptionTime(failure.NextAttemptAt).Sub(start)
	if failure.ConsecutiveFailures != 3 || delta < 675*time.Second || delta > 676*time.Second {
		t.Fatal("declared backoff formula wrong")
	}
	doc["status"].(map[string]any)["consecutiveFailures"] = json.Number("1000")
	delete(doc["schedule"].(map[string]any), "backoffMultiplier")
	delete(doc["schedule"].(map[string]any), "jitterPercent")
	failure = subscriptionFailure(doc, "Timeout")
	delta = subscriptionTime(failure.NextAttemptAt).Sub(time.Now())
	if failure.ConsecutiveFailures != 1000 || delta < time.Duration(86400*0.8)*time.Second-time.Second || delta > time.Duration(86400*1.2)*time.Second+time.Second {
		t.Fatal("default capped backoff/jitter outside range")
	}
}

func TestSubscriptionPartialInitialAndLastGoodPreserved(t *testing.T) {
	h, p := subscriptionTestHandler(t)
	good := subscriptionTestBody("server.example.test")
	source := subscriptionTestSource()
	subscriptionTestFeed(h, good+"\nunsupported-entry-secret-marker")
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if len(answer.Refreshed) != 0 || len(answer.Failed) != 1 || answer.Failed[0].Code != "SchemaMismatch" || len(answer.Failed[0].PartialServers) != 1 || p.calls != 1 {
		t.Fatal("initial partial feed not marked failed with usable accepted servers")
	}
	if answer.Failed[0].Format != "vless-uri-list" || answer.Failed[0].RejectedCount != 1 {
		t.Fatal("partial import omitted its resolved format or rejection count")
	}
	server := answer.Failed[0].PartialServers[0]
	ref := server["userIdRef"].(string)
	secret, err := h.store.Resolve(ref)
	if err != nil || string(secret.Value) != importUser {
		t.Fatal("initial partial reference not backed by stored credential")
	}
	source["serverIds"] = []any{server["id"]}
	before := testImportDisk(t, h.store)
	rotated := strings.ReplaceAll(good, importUser, importRotatedUser)
	subscriptionTestFeed(h, rotated+"\nunsupported-entry-secret-marker")
	answer = subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if len(answer.Refreshed) != 0 || len(answer.Failed) != 1 || len(answer.Failed[0].PartialServers) != 0 || p.calls != 1 || !reflect.DeepEqual(before, testImportDisk(t, h.store)) {
		t.Fatal("partial last-good refresh rotated credentials or returned new unusable refs")
	}
	if answer.Failed[0].Format != "vless-uri-list" || answer.Failed[0].RejectedCount != 1 {
		t.Fatal("last-good refusal omitted its safe parser verdict")
	}
	secret, err = h.store.Resolve(ref)
	if err != nil || string(secret.Value) != importUser {
		t.Fatal("last-good reference no longer usable")
	}
	// Fully valid rotation may replace the last-good credential.
	subscriptionTestFeed(h, rotated)
	answer = subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	secret, err = h.store.Resolve(ref)
	if len(answer.Refreshed) != 1 || err != nil || string(secret.Value) != importRotatedUser {
		t.Fatal("complete rotation did not install credential atomically")
	}
}

func TestSubscriptionQuotaAndOutputPreflightNoWrites(t *testing.T) {
	h, p := subscriptionTestHandler(t)
	source := subscriptionTestSource()
	source["limits"].(map[string]any)["maxServers"] = 1
	subscriptionTestFeed(h, subscriptionTestBody("one.example.test")+"\n"+subscriptionTestBody("two.example.test"))
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(source))
	if answer.Failed[0].Code != "QuotaExceeded" || p.calls != 0 || len(answer.Refreshed) != 0 {
		t.Fatal("parser quota not mapped without writes")
	}
	var body strings.Builder
	for i := 0; i < 4096; i++ {
		body.WriteString("vless://" + importUser + "@" + fmt.Sprintf("s%d.", i) + strings.Repeat("g", 60) + "." + strings.Repeat("h", 60) + "." + strings.Repeat("j", 60) + ".test:443?type=tcp&security=none#" + strings.Repeat("n", 80) + "\n")
	}
	source = subscriptionTestSource()
	source["limits"].(map[string]any)["maxServers"] = 4096
	source["limits"].(map[string]any)["maxBytes"] = 8388608
	servers, _, _, dropped, parseErr := parseSubscription([]byte(body.String()), "auto", subscriptionTestID, 4096)
	if parseErr != nil || dropped != 0 || len(servers) != 4096 {
		t.Fatalf("oversize fixture must parse fully: count=%d dropped=%d err=%v", len(servers), dropped, parseErr)
	}
	subscriptionTestFeed(h, body.String())
	value, refusal := h.Handle(context.Background(), subscriptionTestFrame(source))
	if value != nil || refusal == nil || refusal.Code != "QuotaExceeded" || p.calls != 0 || len(testImportDisk(t, h.store)) != 0 {
		t.Fatal("over-1MiB result committed credentials before response preflight")
	}
}

func TestSubscriptionBatchRollbackAndCancellation(t *testing.T) {
	h, p := subscriptionTestHandler(t)
	subscriptionTestFeed(h, subscriptionTestBody("one.example.test")+"\n"+subscriptionTestBody("two.example.test"))
	p.failAt = 2
	value, refusal := h.Handle(context.Background(), subscriptionTestFrame(subscriptionTestSource()))
	if value != nil || refusal == nil || refusal.Code != "KeystoreUnavailable" || len(testImportDisk(t, h.store)) != 0 || strings.Contains(refusal.Message, importUser) {
		t.Fatal("batch failure leaked secrets or installed a partial batch")
	}
	h, p = subscriptionTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, refusal := h.Handle(ctx, subscriptionTestFrame(subscriptionTestSource())); refusal == nil || refusal.Code != "Timeout" || p.calls != 0 {
		t.Fatal("pre-canceled request did work")
	}
	ctx, cancel = context.WithCancel(context.Background())
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		return subscriptionTestResponse(r, 200, subscriptionTestBody("one.example.test"), nil), nil
	})
	if _, refusal := h.Handle(ctx, subscriptionTestFrame(subscriptionTestSource())); refusal == nil || refusal.Code != "Timeout" || p.calls != 0 {
		t.Fatal("canceled refresh wrote credentials")
	}
}

func TestSubscriptionParallelRefreshSkippedAndReleased(t *testing.T) {
	h, p := subscriptionTestHandler(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.testTransport = subscriptionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return subscriptionTestResponse(r, 200, subscriptionTestBody("server.example.test"), nil), nil
	})
	done := make(chan *subscriptionRefreshAnswer, 1)
	go func() {
		value, refusal := h.Handle(context.Background(), subscriptionTestFrame(subscriptionTestSource()))
		if refusal != nil {
			done <- nil
			return
		}
		done <- value.(*subscriptionRefreshAnswer)
	}()
	<-entered
	answer := subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
	if !reflect.DeepEqual(answer.Skipped, []string{subscriptionTestID}) || len(answer.Failed) != 0 {
		t.Fatal("parallel refresh fetched or claimed success")
	}
	close(release)
	if answer = <-done; answer == nil || len(answer.Refreshed) != 1 || p.calls != 1 {
		t.Fatal("first refresh did not complete")
	}
	h.mu.Lock()
	remaining := len(h.inflight)
	h.mu.Unlock()
	if remaining != 0 {
		t.Fatal("handler retained source state after refresh")
	}
	subscriptionTestFeed(h, subscriptionTestBody("server.example.test"))
	answer = subscriptionTestAnswer(t, h, subscriptionTestFrame(subscriptionTestSource()))
	if len(answer.Refreshed) != 1 || p.calls != 2 {
		t.Fatal("inflight lock not released")
	}
}

func TestSubscriptionServerCredentialBoundary(t *testing.T) {
	server := testImportServer(importServerID)
	server["source"], server["subscriptionId"] = "subscription", subscriptionTestID
	server["network"] = "ws"
	server["ws"] = map[string]any{"path": "/", "headersRef": importHeadersRef, "headersKeys": []any{"Authorization"}}
	values := map[string][]byte{importUserRef: []byte(importUser), importHeadersRef: []byte(`{"Authorization":"synthetic-header-marker"}`)}
	if _, valid := validateSubscriptionServers([]map[string]any{server}, values, subscriptionTestID); !valid {
		t.Fatal("reference-shaped server with bounded credentials rejected")
	}
	for name, mutate := range map[string]func(map[string]any, map[string][]byte){
		"plaintext":    func(s map[string]any, _ map[string][]byte) { s["userId"] = importUser },
		"wrong source": func(s map[string]any, _ map[string][]byte) { s["subscriptionId"] = subscriptionOtherID },
		"wrong entity": func(s map[string]any, _ map[string][]byte) {
			s["userIdRef"] = "keystore://subscription/" + subscriptionTestID + "/user-id"
		},
		"missing credential": func(_ map[string]any, v map[string][]byte) { delete(v, importUserRef) },
		"extra credential": func(_ map[string]any, v map[string][]byte) {
			v["keystore://server/"+importOtherServerID+"/user-id"] = []byte(importUser)
		},
		"invalid UUID": func(_ map[string]any, v map[string][]byte) { v[importUserRef] = []byte("synthetic-secret-marker") },
		"wrong header keys": func(_ map[string]any, v map[string][]byte) {
			v[importHeadersRef] = []byte(`{"Unexpected":"synthetic-header-marker"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			copyJSON, _ := json.Marshal(server)
			var s map[string]any
			_ = json.Unmarshal(copyJSON, &s)
			v := map[string][]byte{}
			for key, value := range values {
				v[key] = value
			}
			mutate(s, v)
			if _, valid := validateSubscriptionServers([]map[string]any{s}, v, subscriptionTestID); valid {
				t.Fatal("invalid parser output/credential escaped boundary")
			}
		})
	}
}
