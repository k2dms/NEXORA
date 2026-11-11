package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/keystore"
)

const subscriptionOutputLimit = 1 << 20

func subscriptionNumber(min, max float64) importRule {
	return func(v any) bool {
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		f, err := n.Float64()
		return err == nil && f >= min && f <= max
	}
}

// This is the complete closed Source schema, including blocks the fetch does not
// consume. Public URLs never carry a credential query; only urlRef may do so.
var subscriptionSourceShape = importObject(map[string]importRule{
	"id": importUUID, "displayName": importString(1, 80, ""),
	"url": func(v any) bool {
		if !importString(0, -1, `^https://[^@\s]+$`)(v) {
			return false
		}
		_, err := subscriptionURL(v.(string), false)
		return err == nil
	},
	"urlRef": importRef, "headersRef": importRef,
	"format": importEnum("auto", "vless-base64", "vless-uri-list", "v2ray-json", "clash-yaml"),
	"limits": importObject(map[string]importRule{
		"maxBytes": importInteger(1024, 8388608), "maxRedirects": importInteger(0, 5),
		"timeoutMs": importInteger(1000, 120000), "maxServers": importInteger(1, 4096),
	}, "maxBytes", "maxRedirects", "timeoutMs"),
	"keepLastGood": importTrue,
	"schedule": importObject(map[string]importRule{
		"intervalSeconds": importInteger(300, 604800), "jitterPercent": importInteger(0, 50),
		"backoffMultiplier": subscriptionNumber(1.5, 10), "maxIntervalSeconds": importInteger(3600, 2592000),
	}, "intervalSeconds"),
	"status": importObject(map[string]importRule{
		"lastAttemptAt": importDateTime, "lastSuccessAt": importDateTime,
		"lastSha256": importString(0, -1, `^[0-9a-f]{64}$`), "serverCount": importInteger(0, 4096),
		"consecutiveFailures": importInteger(0, 1000), "nextAttemptAt": importDateTime,
		"lastError": func(v any) bool {
			return v == nil || importObject(map[string]importRule{
				"code": importEnum("NetworkUnreachable", "Timeout", "AuthFailed", "SchemaMismatch", "QuotaExceeded", "IntegrityMismatch", "DiskFull", "Internal"),
				"at":   importDateTime, "detail": importString(0, 200, ""),
			}, "code")(v)
		},
	}, "consecutiveFailures"),
	"serverIds": importArray(0, 4096, importUUID, true), "createdAt": importDateTime, "updatedAt": importDateTime,
}, "id", "displayName", "url", "format", "limits", "keepLastGood", "schedule")

var subscriptionArgsShape = importObject(map[string]importRule{
	"sources": importArray(1, 64, subscriptionSourceShape, false), "force": importBool,
}, "sources")

type subscriptionRefresh struct {
	store    *keystore.Store
	mu       sync.Mutex
	inflight map[string]bool
	// Unexported test seams; production construction always uses OS DNS, pinned
	// IP dialing and the system TLS roots, with no environment proxy.
	testTransport http.RoundTripper
	testLookupIP  func(context.Context, string, string) ([]net.IP, error)
}

func newSubscriptionRefresh(store *keystore.Store) (*subscriptionRefresh, error) {
	if store == nil {
		return nil, errors.New("subscription.refresh: a keystore is required")
	}
	return &subscriptionRefresh{store: store, inflight: map[string]bool{}}, nil
}

func refuseSubscription(code string) *dispatch.Refusal {
	return &dispatch.Refusal{Code: code, Message: "subscription.refresh: validation, fetch or batch storage did not complete"}
}

type subscriptionRefreshAnswer struct {
	Refreshed []subscriptionRefreshed `json:"refreshed"`
	Unchanged []string                `json:"unchanged"`
	Failed    []subscriptionFailed    `json:"failed"`
	Skipped   []string                `json:"skipped"`
}

type subscriptionRefreshed struct {
	SubscriptionID string           `json:"subscriptionId"`
	SHA256         string           `json:"sha256"`
	Format         string           `json:"format"`
	ServerCount    int              `json:"serverCount"`
	Servers        []map[string]any `json:"servers"`
}

type subscriptionFailed struct {
	SubscriptionID      string           `json:"subscriptionId"`
	Code                string           `json:"code"`
	Retryable           bool             `json:"retryable"`
	ConsecutiveFailures int              `json:"consecutiveFailures"`
	NextAttemptAt       string           `json:"nextAttemptAt"`
	PartialServers      []map[string]any `json:"partialServers,omitempty"`
	Format              string           `json:"format,omitempty"`
	RejectedCount       int              `json:"rejectedCount,omitempty"`
}

type subscriptionFetchSource struct {
	doc     map[string]any
	address *url.URL
	headers http.Header
	code    string
}

func subscriptionValue(obj map[string]any, key string, fallback float64) float64 {
	if n, ok := obj[key].(json.Number); ok {
		f, _ := n.Float64()
		return f
	}
	return fallback
}

func subscriptionTime(s string) time.Time {
	s = strings.ToUpper(s)
	leap := len(s) > 18 && s[17:19] == "60"
	if leap {
		s = s[:17] + "59" + s[19:]
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	if leap {
		t = t.Add(time.Second)
	}
	return t
}

func subscriptionFailure(source map[string]any, code string) subscriptionFailed {
	status, _ := source["status"].(map[string]any)
	failures := min(1000, int(subscriptionValue(status, "consecutiveFailures", 0))+1)
	schedule := source["schedule"].(map[string]any)
	ceiling := subscriptionValue(schedule, "maxIntervalSeconds", 86400)
	seconds := subscriptionValue(schedule, "intervalSeconds", 300)
	multiplier := subscriptionValue(schedule, "backoffMultiplier", 2)
	// Saturate before exponentiation can overflow on the 1000-failure boundary.
	for i := 1; i < failures && seconds < ceiling; i++ {
		seconds = math.Min(ceiling, seconds*multiplier)
	}
	seconds = math.Min(seconds, ceiling)
	jitter := subscriptionValue(schedule, "jitterPercent", 20) / 100
	seconds *= 1 + (rand.Float64()*2-1)*jitter
	return subscriptionFailed{
		SubscriptionID: source["id"].(string), Code: code,
		Retryable:           code == "NetworkUnreachable" || code == "Timeout" || code == "KeystoreUnavailable",
		ConsecutiveFailures: failures,
		NextAttemptAt:       time.Now().Add(time.Duration(seconds * float64(time.Second))).UTC().Format(time.RFC3339Nano),
	}
}

func subscriptionURL(text string, secret bool) (*url.URL, error) {
	bad := errors.New("invalid subscription address")
	if strings.IndexFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return nil, bad
	}
	u, err := url.Parse(text)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.Hostname() == "" || u.User != nil || strings.Contains(u.Host, "%") {
		return nil, bad
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, bad
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, bad
	}
	if !secret {
		for key := range query {
			switch strings.ToLower(key) {
			case "token", "key", "secret", "password", "passwd", "api_key", "apikey", "access_token", "accesstoken", "auth":
				return nil, bad
			}
		}
	}
	return u, nil
}

func subscriptionOwnedRef(text, id, field string) bool {
	r, err := keystore.Parse(text)
	return err == nil && r.Entity == "subscription" && r.ID == id && r.Field == field
}

func subscriptionHeaders(value any) (http.Header, bool) {
	if !validateImportHeaders(value) {
		return nil, false
	}
	out := http.Header{}
	for key, value := range value.(map[string]any) {
		// Header names are RFC tokens. Reject routing/hop-by-hop overrides and
		// case aliases instead of letting Header.Set silently replace one.
		for _, c := range key {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
				return nil, false
			}
		}
		name := http.CanonicalHeaderKey(key)
		switch name {
		case "Host", "Connection", "Proxy-Authorization", "Proxy-Connection", "Transfer-Encoding", "Content-Length", "Trailer", "Upgrade", "Te", "Accept-Encoding":
			return nil, false
		}
		if _, duplicate := out[name]; duplicate {
			return nil, false
		}
		text := value.(string)
		for _, c := range text {
			if c == 127 || c < 32 && c != '\t' {
				return nil, false
			}
		}
		out.Set(name, text)
	}
	return out, true
}

func (h *subscriptionRefresh) prepare(doc map[string]any) subscriptionFetchSource {
	out := subscriptionFetchSource{doc: doc}
	address := doc["url"].(string)
	secretURL := false
	if ref, ok := doc["urlRef"].(string); ok {
		secret, err := h.store.Resolve(ref)
		if err != nil {
			out.code = subscriptionStoreCode(err)
			return out
		}
		address, secretURL = string(secret.Value), true
	}
	var err error
	out.address, err = subscriptionURL(address, secretURL)
	if err != nil || len(address) > 4096 {
		out.code = "InvalidProfile"
		return out
	}
	out.headers = http.Header{}
	if ref, ok := doc["headersRef"].(string); ok {
		secret, err := h.store.Resolve(ref)
		if err != nil {
			out.code = subscriptionStoreCode(err)
			return out
		}
		value, valid := decodeImportJSON(secret.Value)
		if !valid {
			out.code = "InvalidProfile"
			return out
		}
		out.headers, valid = subscriptionHeaders(value)
		if !valid {
			out.code = "InvalidProfile"
		}
	}
	return out
}

func subscriptionStoreCode(err error) string {
	if errors.Is(err, keystore.ErrNotFound) {
		return "InvalidProfile"
	}
	return "KeystoreUnavailable"
}

func (h *subscriptionRefresh) Handle(ctx context.Context, f *envelope.Frame) (any, *dispatch.Refusal) {
	if f == nil || f.Chunk != nil || len(f.Args) > subscriptionOutputLimit {
		return nil, refuseSubscription("BadRequest")
	}
	value, valid := decodeImportJSON(f.Args)
	if !valid || !subscriptionArgsShape(value) {
		return nil, refuseSubscription("BadRequest")
	}
	args := value.(map[string]any)
	docs := args["sources"].([]any)
	seen := map[string]bool{}
	// Validate every source and exact reference ownership before any fetch/write.
	for _, value := range docs {
		doc := value.(map[string]any)
		id := doc["id"].(string)
		if seen[id] {
			return nil, refuseSubscription("BadRequest")
		}
		seen[id] = true
		for key, field := range map[string]string{"urlRef": "url", "headersRef": "headers"} {
			if text, present := doc[key].(string); present && !subscriptionOwnedRef(text, id, field) {
				return nil, refuseSubscription("BadRequest")
			}
		}
	}
	if ctx.Err() != nil {
		return nil, refuseSubscription("Timeout")
	}
	answer := &subscriptionRefreshAnswer{
		Refreshed: []subscriptionRefreshed{}, Unchanged: []string{}, Failed: []subscriptionFailed{}, Skipped: []string{},
	}
	force, _ := args["force"].(bool)
	claimed := []string{}
	defer func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, id := range claimed {
			delete(h.inflight, id)
		}
	}()
	pending := []subscriptionFetchSource{}
	for _, value := range docs {
		doc := value.(map[string]any)
		id := doc["id"].(string)
		status, _ := doc["status"].(map[string]any)
		if next, ok := status["nextAttemptAt"].(string); !force && ok && subscriptionTime(next).After(time.Now()) {
			answer.Skipped = append(answer.Skipped, id)
			continue
		}
		h.mu.Lock()
		busy := h.inflight[id]
		if !busy {
			h.inflight[id] = true
			claimed = append(claimed, id)
		}
		h.mu.Unlock()
		if busy {
			answer.Skipped = append(answer.Skipped, id)
			continue
		}
		pending = append(pending, h.prepare(doc))
	}
	credentials := map[string][]byte{}
	serverIDs := map[string]bool{}
	serverTags := map[string]bool{}
	for _, source := range pending {
		doc := source.doc
		id := doc["id"].(string)
		declared := doc["format"].(string)
		code := source.code
		if code == "" && declared != "auto" && declared != "vless-base64" && declared != "vless-uri-list" {
			code = "SchemaMismatch"
		}
		if code != "" {
			answer.Failed = append(answer.Failed, subscriptionFailure(doc, code))
			continue
		}
		body, code := h.fetch(ctx, source)
		if code != "" {
			answer.Failed = append(answer.Failed, subscriptionFailure(doc, code))
			continue
		}
		digest := sha256.Sum256(body)
		hash := hex.EncodeToString(digest[:])
		status, _ := doc["status"].(map[string]any)
		if !force && status["lastSha256"] == hash {
			answer.Unchanged = append(answer.Unchanged, id)
			continue
		}
		limits := doc["limits"].(map[string]any)
		maxServers := int(subscriptionValue(limits, "maxServers", 1024))
		servers, values, format, dropped, err := parseSubscription(body, declared, id, maxServers)
		code = ""
		if errors.Is(err, errSubscriptionQuota) || len(servers) > maxServers {
			code = "QuotaExceeded"
		} else if err != nil || dropped > 0 || len(servers) == 0 {
			code = "SchemaMismatch"
		}
		if code == "" && (format != "vless-base64" && format != "vless-uri-list" || declared != "auto" && format != declared) {
			code = "SchemaMismatch"
		}
		members, _ := doc["serverIds"].([]any)
		// A partial update must never rotate the credentials supporting last-good.
		// For an initial source only, accepted entries can be made usable and shown
		// as partialServers, still under failed (never a full-success verdict).
		if code != "" && (len(members) != 0 || len(servers) == 0 || len(servers) > maxServers) {
			failure := subscriptionFailure(doc, code)
			if err == nil && len(servers) > 0 && dropped > 0 {
				failure.Format, failure.RejectedCount = format, dropped
			}
			answer.Failed = append(answer.Failed, failure)
			continue
		}
		servers, valid = validateSubscriptionServers(servers, values, id)
		if !valid {
			answer.Failed = append(answer.Failed, subscriptionFailure(doc, "SchemaMismatch"))
			continue
		}
		for _, server := range servers {
			sid := server["id"].(string)
			if serverIDs[sid] || serverTags[sid[:8]] {
				return nil, refuseSubscription("SchemaMismatch")
			}
			serverIDs[sid], serverTags[sid[:8]] = true, true
		}
		for ref, secret := range values {
			if _, collision := credentials[ref]; collision {
				return nil, refuseSubscription("SchemaMismatch")
			}
			credentials[ref] = secret
		}
		if code != "" {
			failure := subscriptionFailure(doc, code)
			failure.PartialServers = servers
			failure.Format, failure.RejectedCount = format, dropped
			answer.Failed = append(answer.Failed, failure)
		} else {
			answer.Refreshed = append(answer.Refreshed, subscriptionRefreshed{
				SubscriptionID: id, SHA256: hash, Format: format, ServerCount: len(servers), Servers: servers,
			})
		}
	}
	// Preflight the entire response before the single transactional secret write.
	encoded, err := json.Marshal(answer)
	if err != nil || len(encoded) > subscriptionOutputLimit {
		return nil, refuseSubscription("QuotaExceeded")
	}
	if ctx.Err() != nil {
		return nil, refuseSubscription("Timeout")
	}
	if len(credentials) > 0 {
		if err := h.store.PutBatch(credentials); err != nil {
			return nil, refuseSubscription("KeystoreUnavailable")
		}
	}
	return answer, nil
}

func validateSubscriptionServers(servers []map[string]any, values map[string][]byte, sourceID string) ([]map[string]any, bool) {
	normalized := make([]map[string]any, 0, len(servers))
	owners := map[string]map[string]any{}
	tags := map[string]bool{}
	for _, server := range servers {
		data, err := json.Marshal(server)
		if err != nil {
			return nil, false
		}
		value, valid := decodeImportJSON(data)
		if !valid || !validateImportServer(value) {
			return nil, false
		}
		s := value.(map[string]any)
		id := s["id"].(string)
		if s["source"] != "subscription" || s["subscriptionId"] != sourceID || owners[id] != nil || tags[id[:8]] || s["tag"] != nil && s["tag"] != "srv-"+id[:8] {
			return nil, false
		}
		owners[id], tags[id[:8]] = s, true
		normalized = append(normalized, s)
	}
	refs := map[string]bool{}
	pairs := map[string]bool{}
	check := func(text, owner, field string, keys []any) bool {
		r, err := keystore.Parse(text)
		if err != nil || r.Entity != "server" || r.Field != field || refs[text] || pairs[owner+"/"+field] {
			return false
		}
		resolved, ok := resolveImportOwner(r.ID, owners)
		if !ok || resolved != owner {
			return false
		}
		secret, found := values[text]
		if !found {
			return false
		}
		var value any = string(secret)
		if field == "headers" {
			value, ok = decodeImportJSON(secret)
			if !ok {
				return false
			}
		}
		if !validateImportSecret(importSecretRef{field: field, keys: keys}, value) {
			return false
		}
		refs[text], pairs[owner+"/"+field] = true, true
		return true
	}
	for _, server := range normalized {
		owner := server["id"].(string)
		if !check(server["userIdRef"].(string), owner, "user-id", nil) {
			return nil, false
		}
		for _, name := range []string{"ws", "grpc"} {
			block, _ := server[name].(map[string]any)
			text, hasRef := block["headersRef"].(string)
			keys, hasKeys := block["headersKeys"].([]any)
			if hasRef != hasKeys || hasRef && !check(text, owner, "headers", keys) {
				return nil, false
			}
		}
	}
	return normalized, len(refs) == len(values)
}

func subscriptionSafeIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Metadata/wire-server literals not all covered by link-local rejection.
	for _, text := range []string{"100.100.100.200", "168.63.129.16", "192.0.0.192", "fd00:ec2::254"} {
		if ip.Equal(net.ParseIP(text)) {
			return false
		}
	}
	return true
}

func (h *subscriptionRefresh) guardedIPs(ctx context.Context, host string) ([]net.IP, error) {
	var ips []net.IP
	var err error
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else if h.testLookupIP != nil {
		ips, err = h.testLookupIP(ctx, "ip", host)
	} else {
		resolver := &net.Resolver{PreferGo: false}
		ips, err = resolver.LookupIP(ctx, "ip", host)
	}
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errors.New("empty address resolution")
	}
	for _, ip := range ips {
		if !subscriptionSafeIP(ip) {
			return nil, errors.New("prohibited resolved address")
		}
	}
	return ips, nil
}

func subscriptionTransport(u *url.URL, ips []net.IP) *http.Transport {
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return &http.Transport{
		Proxy: nil, DisableCompression: true, DisableKeepAlives: true,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()},
		MaxResponseHeaderBytes: 64 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, requestedPort, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(host, u.Hostname()) || requestedPort != port {
				return nil, errors.New("unexpected connection target")
			}
			var last error
			for _, ip := range ips {
				if !subscriptionSafeIP(ip) {
					return nil, errors.New("prohibited connection address")
				}
				conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		},
	}
}

func subscriptionOrigin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return strings.ToLower(u.Scheme + "://" + net.JoinHostPort(u.Hostname(), port))
}

func subscriptionNetworkCode(ctx context.Context, err error) string {
	var timeout net.Error
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return "Timeout"
	}
	return "NetworkUnreachable"
}

func (h *subscriptionRefresh) fetch(parent context.Context, source subscriptionFetchSource) ([]byte, string) {
	limits := source.doc["limits"].(map[string]any)
	ctx, cancel := context.WithTimeout(parent, time.Duration(subscriptionValue(limits, "timeoutMs", 20000))*time.Millisecond)
	defer cancel()
	maxBytes := int64(subscriptionValue(limits, "maxBytes", 2097152))
	maxRedirects := int(subscriptionValue(limits, "maxRedirects", 3))
	address := source.address
	headers := source.headers.Clone()
	for redirects := 0; ; redirects++ {
		ips, err := h.guardedIPs(ctx, address.Hostname())
		if err != nil {
			return nil, subscriptionNetworkCode(ctx, err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
		if err != nil {
			return nil, "InvalidProfile"
		}
		request.Header = headers.Clone()
		request.Header.Set("Accept-Encoding", "identity")
		var transport http.RoundTripper = h.testTransport
		var production *http.Transport
		if transport == nil {
			production = subscriptionTransport(address, ips)
			transport = production
		}
		response, err := transport.RoundTrip(request)
		if production != nil {
			defer production.CloseIdleConnections()
		}
		if err != nil {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, subscriptionNetworkCode(ctx, err)
		}
		if response == nil || response.Body == nil {
			return nil, "NetworkUnreachable"
		}
		switch response.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			_ = response.Body.Close()
			if redirects >= maxRedirects {
				return nil, "NetworkUnreachable"
			}
			location, err := response.Location()
			if err != nil {
				return nil, "NetworkUnreachable"
			}
			location = address.ResolveReference(location)
			// Resolved secret addresses and their redirects may carry a query, but
			// never userinfo or a non-HTTPS hop. Every hop gets a fresh IP guard.
			next, err := subscriptionURL(location.String(), true)
			if err != nil {
				return nil, "NetworkUnreachable"
			}
			if subscriptionOrigin(next) != subscriptionOrigin(address) {
				headers = http.Header{} // no custom secret header crosses an origin
			}
			address = next
			continue
		case http.StatusUnauthorized, http.StatusForbidden:
			_ = response.Body.Close()
			return nil, "AuthFailed"
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			return nil, "NetworkUnreachable"
		}
		contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if strings.EqualFold(contentType, "text/html") || strings.EqualFold(contentType, "application/xhtml+xml") {
			_ = response.Body.Close()
			return nil, "SchemaMismatch"
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
		_ = response.Body.Close()
		if err != nil || ctx.Err() != nil {
			return nil, subscriptionNetworkCode(ctx, err)
		}
		if int64(len(body)) > maxBytes {
			return nil, "QuotaExceeded"
		}
		prefix := bytes.ToLower(bytes.TrimSpace(body))
		if bytes.HasPrefix(prefix, []byte("<!doctype html")) || bytes.HasPrefix(prefix, []byte("<html")) {
			return nil, "SchemaMismatch"
		}
		return body, ""
	}
}
