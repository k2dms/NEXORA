package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/k2dms/nexora/connector/internal/keystore"
)

const (
	subscriptionParseTestSource = "11111111-2222-5333-8444-555555555555" // nexora-secret-allow: synthetic source identity
	subscriptionParseTestOther  = "22222222-3333-5444-8555-666666666666" // nexora-secret-allow: synthetic source identity
	subscriptionParseTestUser   = "abcdef01-2345-4678-9abc-def012345678" // nexora-secret-allow: synthetic credential, no real endpoint
	subscriptionParseTestRotate = "bcdef012-3456-4789-abcd-ef0123456789" // nexora-secret-allow: synthetic rotated credential
	subscriptionParseTestThird  = "cdef0123-4567-489a-bcde-f0123456789a" // nexora-secret-allow: synthetic third credential
)

func subscriptionParseTestURI(address, credential, fragment string, query url.Values) string {
	u := url.URL{Scheme: "vless", User: url.User(credential), Host: address, RawQuery: query.Encode(), Fragment: fragment}
	return u.String()
}

func subscriptionParseTestReality(network string) url.Values {
	query := url.Values{
		"security": {"reality"}, "flow": {"xtls-rprx-vision"}, "encryption": {"none"},
		"sni": {"www.example.test"}, "fp": {"chrome"}, "pbk": {strings.Repeat("a", 43)},
		"sid": {"0011aabb"}, "spx": {"/"}, "headerType": {"none"},
	}
	if network != "" {
		query.Set("type", network)
	}
	return query
}

func subscriptionParseTestPublic(t *testing.T, servers []map[string]any, values map[string][]byte) {
	t.Helper()
	if len(servers) != len(values) {
		t.Fatal("public documents and credential batch disagree")
	}
	for _, server := range servers {
		data, err := json.Marshal(server)
		if err != nil {
			t.Fatal("public document did not marshal")
		}
		decoded, ok := decodeImportJSON(data)
		if !ok || !validateImportServer(decoded) {
			t.Fatal("public document violates import boundary")
		}
		for _, credential := range []string{subscriptionParseTestUser, subscriptionParseTestRotate, subscriptionParseTestThird} {
			if strings.Contains(strings.ToLower(string(data)), credential) {
				t.Fatal("public document contains a credential")
			}
		}
		if server["source"] != "subscription" || server["subscriptionId"] != subscriptionParseTestSource || server["protocol"] != "vless" || server["network"] != "tcp" {
			t.Fatal("server has incorrect provenance or transport")
		}
		if _, ok := server["flow"].(string); !ok {
			t.Fatal("flow must always be a string")
		}
		id := server["id"].(string)
		if !importUUID(id) || id[14] != '5' || !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatal("derived identity is not UUID-v5-shaped")
		}
		ref := server["userIdRef"].(string)
		owner, err := keystore.Parse(ref)
		if err != nil || owner.Entity != "server" || owner.ID != id || owner.Field != "user-id" {
			t.Fatal("credential reference has wrong owner or field")
		}
		if !importUserID(string(values[ref])) {
			t.Fatal("credential batch must contain only UUID bytes")
		}
		if name := server["displayName"].(string); name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || subscriptionSecretText.MatchString(name) {
			t.Fatal("unsafe display name")
		}
	}
}

func TestSubscriptionActualFeedShapesAndFormats(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		network := "tcp"
		if i >= 4 && i < 12 {
			network = "raw"
		} else if i >= 12 && i < 18 {
			network = ""
		} else if i >= 18 {
			network = "grpc"
		}
		lines = append(lines, subscriptionParseTestURI(fmt.Sprintf("node-%d.example.test:443", i), subscriptionParseTestUser, fmt.Sprintf("Node %d", i), subscriptionParseTestReality(network)))
	}
	plain := "\n# synthetic subscription\r\n\n" + strings.Join(lines, "\r\n") + "\n"
	cases := []struct {
		name string
		body string
		want string
	}{
		{"plain", plain, "vless-uri-list"},
		{"standard padded", base64.StdEncoding.EncodeToString([]byte(plain)), "vless-base64"},
		{"standard raw", base64.RawStdEncoding.EncodeToString([]byte(plain)), "vless-base64"},
		{"url padded", base64.URLEncoding.EncodeToString([]byte(plain)), "vless-base64"},
		{"url raw", base64.RawURLEncoding.EncodeToString([]byte(plain)), "vless-base64"},
	}
	for _, tc := range cases {
		for _, format := range []string{"auto", tc.want} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				servers, values, resolved, dropped, err := parseSubscription([]byte("\n"+tc.body+"\n"), format, subscriptionParseTestSource, 20)
				if err != nil || resolved != tc.want || dropped != 2 || len(servers) != 18 {
					t.Fatalf("format=%s accepted=%d dropped=%d error=%v", resolved, len(servers), dropped, err)
				}
				subscriptionParseTestPublic(t, servers, values)
				for _, server := range servers {
					if server["flow"] != "xtls-rprx-vision" || server["security"] != "reality" {
						t.Fatal("REALITY/Vision fields lost")
					}
				}
			})
		}
	}
}

func TestSubscriptionBase64Strictness(t *testing.T) {
	for _, encoded := range []string{"+/8=", "+/8", "-_8=", "-_8"} {
		decoded, ok := decodeSubscriptionBase64(encoded)
		if !ok || string(decoded) != string([]byte{0xfb, 0xff}) {
			t.Fatal("valid alphabet or padding variant refused")
		}
	}
	for _, encoded := range []string{"", "Z", "Zg=", "Zg===", "Zh==", "Zh", "+_8=", "Zg==junk", "Zg\n==", "Zg\r==", "Z g==", "Zg\t==", "Zg$=", "Zg==\u00a0"} {
		if _, ok := decodeSubscriptionBase64(encoded); ok {
			t.Fatal("malformed strict base64 accepted")
		}
	}
	plain := subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "😀", nil)
	// Unicode forces the wrappers to exercise alphabet characters too; the
	// synthetic comment is ignored after decoding, not stripped before it.
	plain += "\n#\u00ff\u00ff\u00ff"
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		servers, values, format, dropped, err := parseSubscription([]byte(encoding.EncodeToString([]byte(plain))), "auto", subscriptionParseTestSource, 1)
		if err != nil || format != "vless-base64" || dropped != 0 || len(servers) != 1 {
			t.Fatal("wrapped unicode URI refused")
		}
		subscriptionParseTestPublic(t, servers, values)
	}
}

func TestSubscriptionEmptyMalformedAndSingleUnwrap(t *testing.T) {
	plain := subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "Node", nil)
	wrapped := base64.StdEncoding.EncodeToString([]byte(plain))
	for _, tc := range []struct {
		name, body, format string
	}{
		{"empty auto", "", "auto"}, {"empty plain", " \n\t", "vless-uri-list"},
		{"comments only", "# no nodes\n\n# another", "vless-uri-list"},
		{"empty decoded", base64.StdEncoding.EncodeToString([]byte(" \n")), "vless-base64"},
		{"bad padding", "Zg=", "auto"}, {"bad trailing bits", "Zh==", "vless-base64"},
		{"bad alphabet", "Zg!", "auto"}, {"internal whitespace", "Zg\n==", "vless-base64"},
		{"double wrapped", base64.StdEncoding.EncodeToString([]byte(wrapped)), "auto"},
		{"double raw url wrapped", base64.RawURLEncoding.EncodeToString([]byte(wrapped)), "vless-base64"},
		{"decoded junk", base64.StdEncoding.EncodeToString([]byte("not a URI")), "auto"},
		{"declared wrapper mismatch", plain, "vless-base64"},
		{"declared plain mismatch", wrapped, "vless-uri-list"},
		{"unsupported format", plain, "v2ray-json"},
		{"invalid UTF8", string([]byte{0xff}), "vless-uri-list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			servers, values, _, _, err := parseSubscription([]byte(tc.body), tc.format, subscriptionParseTestSource, 10)
			if err == nil || servers != nil || values != nil || strings.Contains(err.Error(), subscriptionParseTestUser) || strings.Contains(err.Error(), "vless://") {
				t.Fatal("invalid payload succeeded or returned unsanitized data")
			}
		})
	}
	servers, values, _, _, err := parseSubscription([]byte(plain), "auto", "source-secret-invalid", 1)
	if err == nil || servers != nil || values != nil || strings.Contains(err.Error(), "source-secret-invalid") {
		t.Fatal("invalid source identity not safely refused")
	}
}

func TestSubscriptionSecurityFieldsIPv6AndNames(t *testing.T) {
	tls := url.Values{"type": {"tcp"}, "security": {"tls"}, "sni": {"WWW.Example.Test."}, "fp": {"firefox"}, "alpn": {"h2,http/1.1"}, "flow": {"xtls-rprx-vision"}}
	for _, tc := range []struct {
		name, address, fragment, wantName, security string
		query                                       url.Values
	}{
		{"plain default", "Example.Test.:00443", "Plain", "Plain", "none", nil},
		{"TLS", "example.test:443", "TLS node", "TLS node", "tls", tls},
		{"IPv6", "[2001:0db8:0:0::1]:443", "東京 + Europe / node", "東京 + Europe / node", "reality", subscriptionParseTestReality("raw")},
		{"truncate runes", "example.test:443", strings.Repeat("界", 100), strings.Repeat("界", 80), "none", nil},
		{"empty name", "example.test:443", "", "example.test:443", "none", nil},
		{"UUID name", "example.test:443", "Node " + subscriptionParseTestUser, "example.test:443", "none", nil},
		{"upper UUID name", "example.test:443", strings.ToUpper(subscriptionParseTestRotate), "example.test:443", "none", nil},
		{"compact UUID name", "example.test:443", strings.ReplaceAll(subscriptionParseTestUser, "-", ""), "example.test:443", "none", nil},
		{"URI name", "example.test:443", "vless://" + subscriptionParseTestUser + "@example.test:443", "example.test:443", "none", nil},
		{"URL token name", "example.test:443", "https://example.test/private-token", "example.test:443", "none", nil},
		{"double encoded name", "example.test:443", strings.ReplaceAll(subscriptionParseTestUser, "-", "%2D"), "example.test:443", "none", nil},
		{"encoded link name", "example.test:443", "vless%3A%2F%2Fhidden", "example.test:443", "none", nil},
		{"control name", "example.test:443", "Node\nSecret", "example.test:443", "none", nil},
		{"secret after truncation", "example.test:443", strings.Repeat("界", 81) + subscriptionParseTestUser, "example.test:443", "none", nil},
		{"IPv6 fallback", "[2001:db8::1]:443", "", "[2001:db8::1]:443", "none", nil},
		{"long address fallback", strings.Repeat("a", 180) + ".test:443", "", strings.Repeat("a", 80), "none", nil},
		{"decoded invalid UTF8 name", "example.test:443", string([]byte{0xff}), "example.test:443", "none", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := subscriptionParseTestURI(tc.address, subscriptionParseTestUser, tc.fragment, tc.query)
			servers, values, _, dropped, err := parseSubscription([]byte(body), "auto", subscriptionParseTestSource, 1)
			if err != nil || dropped != 0 || len(servers) != 1 {
				t.Fatalf("supported URI refused: %v", err)
			}
			subscriptionParseTestPublic(t, servers, values)
			s := servers[0]
			if s["displayName"] != tc.wantName || s["security"] != tc.security || s["port"] != 443 {
				t.Fatal("name, security or port mapped incorrectly")
			}
			if strings.HasPrefix(tc.address, "[") && s["address"] != "2001:db8::1" {
				t.Fatal("IPv6 address not normalized")
			}
			if tc.security == "tls" {
				block := s["tls"].(map[string]any)
				if block["serverName"] != "www.example.test" || block["fingerprint"] != "firefox" || strings.Join(block["alpn"].([]string), ",") != "h2,http/1.1" {
					t.Fatal("TLS fields not preserved")
				}
			}
			if tc.security == "reality" {
				block := s["reality"].(map[string]any)
				if block["shortId"] != "0011aabb" || block["spiderX"] != "/" || block["fingerprint"] != "chrome" || block["publicKey"] != strings.Repeat("a", 43) {
					t.Fatal("REALITY fields not preserved")
				}
			}
		})
	}
}

func TestSubscriptionRefusesUnsupportedOrAmbiguousParameters(t *testing.T) {
	good := subscriptionParseTestURI("good.example.test:443", subscriptionParseTestUser, "Good", nil)
	cases := []struct{ name, key, value string }{
		{"grpc REALITY", "type", "grpc"}, {"websocket", "type", "ws"}, {"xhttp", "type", "xhttp"}, {"h2", "type", "h2"}, {"empty type", "type", ""},
		{"unsupported security", "security", "xtls"}, {"empty security", "security", ""},
		{"invalid flow", "flow", "xtls-rprx-direct"}, {"invalid encryption", "encryption", "auto"}, {"empty encryption", "encryption", ""},
		{"HTTP header", "headerType", "http"}, {"empty header", "headerType", ""},
		{"insecure true", "insecure", "true"}, {"insecure false", "insecure", "false"}, {"allow insecure", "allowInsecure", "1"},
		{"certificate", "cert", "private-token"}, {"pin", "pinCert", "private-token"}, {"pin SHA", "pinnedPeerCertificateChainSha256", "private-token"},
		{"minimum TLS", "minVersion", "1.2"}, {"maximum TLS", "maxVersion", "1.3"}, {"unknown socket", "tfo", "1"},
		{"unknown secret", "token", subscriptionParseTestUser}, {"private key", "privateKey", subscriptionParseTestUser},
		{"unsupported postquantum", "support-x25519mlkem768", "1"},
		{"unsupported path", "path", "/"}, {"unsupported host", "host", "example.test"}, {"service", "serviceName", "service"},
		{"missing SNI", "sni", ""}, {"bad SNI", "sni", "bad_name"}, {"SNI credential", "sni", subscriptionParseTestUser},
		{"bad fingerprint", "fp", ""}, {"long fingerprint", "fp", strings.Repeat("a", 33)}, {"fingerprint credential", "fp", subscriptionParseTestUser},
		{"missing public key", "pbk", ""}, {"bad public key", "pbk", strings.Repeat("a", 42)},
		{"odd short ID", "sid", "a"}, {"long short ID", "sid", "001122334455667788"}, {"bad short ID", "sid", "gg"},
		{"long spider", "spx", strings.Repeat("a", 65)}, {"spider credential", "spx", subscriptionParseTestUser},
		{"REALITY ALPN unsupported", "alpn", "h2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := subscriptionParseTestReality("tcp")
			q.Set(tc.key, tc.value)
			bad := subscriptionParseTestURI("bad.example.test:443", subscriptionParseTestRotate, "Bad", q)
			// The copied synthetic credential uses either credential, not merely
			// the one in this URI's userinfo.
			if strings.Contains(tc.name, "credential") {
				q.Set(tc.key, subscriptionParseTestRotate)
				bad = subscriptionParseTestURI("bad.example.test:443", subscriptionParseTestRotate, "Bad", q)
			}
			servers, values, format, dropped, err := parseSubscription([]byte(bad+"\n"+good), "auto", subscriptionParseTestSource, 2)
			if err != nil || format != "vless-uri-list" || len(servers) != 1 || dropped != 1 {
				t.Fatalf("mixed feed did not refuse bad entry: accepted=%d dropped=%d error=%v", len(servers), dropped, err)
			}
			subscriptionParseTestPublic(t, servers, values)
			servers, values, _, dropped, err = parseSubscription([]byte(bad), "vless-uri-list", subscriptionParseTestSource, 2)
			if err == nil || servers != nil || values != nil || dropped != 1 || strings.Contains(err.Error(), subscriptionParseTestRotate) || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "vless://") {
				t.Fatal("zero-accepted feed not safely refused")
			}
		})
	}
	for _, q := range []url.Values{
		{"security": {"none"}, "flow": {"xtls-rprx-vision"}},
		{"security": {"none"}, "sni": {"example.test"}},
		{"security": {"none"}, "fp": {"chrome"}},
		{"security": {"none"}, "pbk": {strings.Repeat("a", 43)}},
		{"security": {"tls"}, "sni": {"example.test"}, "pbk": {strings.Repeat("a", 43)}},
		{"security": {"tls"}, "sni": {"example.test"}, "sid": {""}},
		{"security": {"tls"}, "sni": {"example.test"}, "spx": {"/"}},
		{"security": {"tls"}, "sni": {"example.test"}, "alpn": {"h2,h2"}},
		{"security": {"tls"}, "sni": {"example.test"}, "alpn": {"ftp"}},
		{"security": {"tls"}, "sni": {"example.test"}, "alpn": {""}},
	} {
		bad := subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "Node", q)
		if _, _, _, _, err := parseSubscription([]byte(bad), "auto", subscriptionParseTestSource, 1); err == nil {
			t.Fatal("inactive security parameter or invalid ALPN accepted")
		}
	}
}

func TestSubscriptionMalformedURIsCredentialsAndRepeatedQuery(t *testing.T) {
	prefix := "vless://" + subscriptionParseTestUser + "@example.test:443"
	cases := []string{
		"vless://example.test:443", "vless://bad-credential@example.test:443",
		"vless://" + subscriptionParseTestUser + ":password@example.test:443",
		"vmess://" + subscriptionParseTestUser + "@example.test:443",
		"vless://" + subscriptionParseTestUser + "@example.test", "vless://" + subscriptionParseTestUser + "@:443",
		"vless://" + subscriptionParseTestUser + "@2001:db8::1:443",
		"vless://" + subscriptionParseTestUser + "@[example.test]:443", "vless://" + subscriptionParseTestUser + "@[127.0.0.1]:443",
		"vless://" + subscriptionParseTestUser + "@example.test:0", "vless://" + subscriptionParseTestUser + "@example.test:65536",
		"vless://" + subscriptionParseTestUser + "@example.test:+443", "vless://" + subscriptionParseTestUser + "@example.test:1.5",
		"vless://" + subscriptionParseTestUser + "@bad_host:443", "vless://" + subscriptionParseTestUser + "@" + subscriptionParseTestUser + ":443",
		prefix + "/path", prefix + "/", prefix + "?sni=%zz", prefix + "#%zz",
		prefix + "?type=tcp&type=tcp", prefix + "?type=tcp&type=grpc", prefix + "?type=tcp&%74ype=raw",
		prefix + "?security=none&security=tls", prefix + "?flow=&flow=", prefix + "?headerType=none&headerType=none",
		prefix + "?type=tcp;security=none", prefix + "?type=tcp&Type=tcp", prefix + "?type=%ff",
		prefix + "?security=tls&sni=example.test&sni=example.test",
		prefix + string([]byte{0xff}),
	}
	for i, bad := range cases {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			servers, values, _, dropped, err := parseSubscription([]byte(bad), "vless-uri-list", subscriptionParseTestSource, 1)
			if err == nil || servers != nil || values != nil || dropped != 1 || strings.Contains(err.Error(), subscriptionParseTestUser) || strings.Contains(err.Error(), "vless://") || strings.Contains(err.Error(), "password") {
				t.Fatal("malformed URI succeeded or leaked input")
			}
		})
	}
	upper := subscriptionParseTestURI("example.test:443", strings.ToUpper(subscriptionParseTestUser), "Uppercase credential", nil)
	servers, values, _, dropped, err := parseSubscription([]byte(upper), "auto", subscriptionParseTestSource, 1)
	if err != nil || dropped != 0 || string(values[servers[0]["userIdRef"].(string)]) != strings.ToUpper(subscriptionParseTestUser) {
		t.Fatal("valid uppercase UUID bytes changed or refused")
	}
}

func TestSubscriptionStableIdentityAndDuplicates(t *testing.T) {
	parse := func(body, source string) ([]map[string]any, map[string][]byte, int) {
		t.Helper()
		servers, values, _, dropped, err := parseSubscription([]byte(body), "auto", source, 10)
		if err != nil {
			t.Fatalf("valid identity fixture refused: %v", err)
		}
		return servers, values, dropped
	}
	original := subscriptionParseTestURI("Example.Test.:00443", subscriptionParseTestUser, "Old name", subscriptionParseTestReality("tcp"))
	q := subscriptionParseTestReality("raw")
	q.Set("sni", "WWW.EXAMPLE.TEST.")
	q.Set("sid", "0011AABB")
	q.Del("encryption")
	q.Del("headerType")
	rotated := subscriptionParseTestURI("example.test:443", subscriptionParseTestRotate, "New name", q)
	a, _, _ := parse(original, subscriptionParseTestSource)
	renamedURI := subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "Provider rename", subscriptionParseTestReality("raw"))
	renamed, _, _ := parse(renamedURI, subscriptionParseTestSource)
	if a[0]["id"] != renamed[0]["id"] || renamed[0]["displayName"] != "Provider rename" {
		t.Fatal("provider rename alone changed identity")
	}
	duplicates, _, duplicateCount := parse(original+"\n"+renamedURI, subscriptionParseTestSource)
	if len(duplicates) != 1 || duplicateCount != 1 || duplicates[0]["displayName"] != "Old name" {
		t.Fatal("same credential with renamed or aliased URI was not collapsed")
	}
	b, values, _ := parse(rotated, subscriptionParseTestSource)
	if a[0]["id"] != b[0]["id"] || a[0]["userIdRef"] != b[0]["userIdRef"] || b[0]["displayName"] != "New name" || string(values[b[0]["userIdRef"].(string)]) != subscriptionParseTestRotate {
		t.Fatal("rename, normalization or credential rotation changed identity")
	}
	q.Del("type")
	c, _, _ := parse(subscriptionParseTestURI("example.test:443", subscriptionParseTestThird, "Third name", q), subscriptionParseTestSource)
	if c[0]["id"] != a[0]["id"] {
		t.Fatal("absent TCP default changed identity")
	}
	other, _, _ := parse(original, subscriptionParseTestOther)
	if other[0]["id"] == a[0]["id"] {
		t.Fatal("source identity missing from derivation")
	}
	for _, change := range []struct{ key, value string }{{"sni", "other.example.test"}, {"fp", "firefox"}, {"sid", "00112233"}, {"spx", "/changed"}, {"flow", ""}, {"pbk", strings.Repeat("b", 43)}} {
		query := subscriptionParseTestReality("tcp")
		query.Set(change.key, change.value)
		changed, _, _ := parse(subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "Old name", query), subscriptionParseTestSource)
		if changed[0]["id"] == a[0]["id"] {
			t.Fatalf("connection field %s missing from identity", change.key)
		}
	}
	third := subscriptionParseTestURI("example.test:443", subscriptionParseTestThird, "Third", q)
	mixed, values, dropped := parse(strings.Join([]string{original, original, rotated, original, third, rotated}, "\n"), subscriptionParseTestSource)
	if len(mixed) != 3 || len(values) != 3 || dropped != 3 || mixed[0]["id"] != a[0]["id"] || mixed[0]["id"] == mixed[1]["id"] || mixed[1]["id"] == mixed[2]["id"] {
		t.Fatal("duplicates did not collapse or distinct credentials lost ordinals")
	}
	subscriptionParseTestPublic(t, mixed, values)
	rotationOnly, _, _ := parse(strings.Join([]string{rotated, original, third}, "\n"), subscriptionParseTestSource)
	for i := range mixed {
		if mixed[i]["id"] != rotationOnly[i]["id"] {
			t.Fatal("credential values or their digest entered ordinal identities")
		}
	}
	for _, endpoint := range []string{"other.example.test:443", "example.test:444"} {
		changed, _, _ := parse(subscriptionParseTestURI(endpoint, subscriptionParseTestUser, "Node", subscriptionParseTestReality("tcp")), subscriptionParseTestSource)
		if changed[0]["id"] == a[0]["id"] {
			t.Fatal("address or port missing from identity")
		}
	}
	ipv6a, _, _ := parse(subscriptionParseTestURI("[2001:0db8:0:0::1]:443", subscriptionParseTestUser, "Node", nil), subscriptionParseTestSource)
	ipv6b, _, _ := parse(subscriptionParseTestURI("[2001:db8::1]:443", subscriptionParseTestRotate, "Renamed", url.Values{"type": {"raw"}, "security": {"none"}}), subscriptionParseTestSource)
	if ipv6a[0]["id"] != ipv6b[0]["id"] {
		t.Fatal("equivalent IPv6 identity not normalized")
	}
	tlsQuery := url.Values{"security": {"tls"}, "sni": {"example.test"}, "alpn": {"h2,http/1.1"}}
	tlsA, _, _ := parse(subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "TLS", tlsQuery), subscriptionParseTestSource)
	tlsQuery.Set("alpn", "http/1.1,h2")
	tlsB, _, _ := parse(subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "TLS", tlsQuery), subscriptionParseTestSource)
	plain, _, _ := parse(subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "Plain", nil), subscriptionParseTestSource)
	if tlsA[0]["id"] == tlsB[0]["id"] || tlsA[0]["id"] == plain[0]["id"] || tlsA[0]["id"] == a[0]["id"] {
		t.Fatal("ALPN order or security mode missing from identity")
	}
	// Empty REALITY short ID/spider path and their absence compile identically.
	query := subscriptionParseTestReality("tcp")
	query.Del("sid")
	query.Del("spx")
	absent, _, _ := parse(subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "Node", query), subscriptionParseTestSource)
	query.Set("sid", "")
	query.Set("spx", "")
	empty, _, _ := parse(subscriptionParseTestURI("example.test:443", subscriptionParseTestUser, "Renamed", query), subscriptionParseTestSource)
	if absent[0]["id"] != empty[0]["id"] {
		t.Fatal("empty optional defaults changed identity")
	}
}

func TestSubscriptionTagCollisionRefused(t *testing.T) {
	// These nonsecret transport addresses have an actual SHA-256 first-eight
	// collision under the fixed source and ordinal, not an injected hash stub.
	first := subscriptionParseTestURI("collision-3764.example.test:443", subscriptionParseTestUser, "One", nil)
	second := subscriptionParseTestURI("collision-78128.example.test:443", subscriptionParseTestRotate, "Two", nil)
	servers, values, format, dropped, err := parseSubscription([]byte(first+"\n"+second), "auto", subscriptionParseTestSource, 2)
	if err == nil || errors.Is(err, errSubscriptionQuota) || servers != nil || values != nil || format != "vless-uri-list" || dropped != 0 || strings.Contains(err.Error(), subscriptionParseTestUser) || strings.Contains(err.Error(), subscriptionParseTestRotate) {
		t.Fatal("derived tag collision not atomically refused")
	}
}

func TestSubscriptionQuotaBeforeAnyReturnedBatch(t *testing.T) {
	one := subscriptionParseTestURI("one.example.test:443", subscriptionParseTestUser, "One", nil)
	two := subscriptionParseTestURI("two.example.test:443", subscriptionParseTestRotate, "Two", nil)
	for _, max := range []int{-1, 0, 1} {
		servers, values, _, _, err := parseSubscription([]byte(one+"\n"+two), "auto", subscriptionParseTestSource, max)
		if !errors.Is(err, errSubscriptionQuota) || servers != nil || values != nil {
			t.Fatal("over-limit feed returned a partial batch or wrong error")
		}
	}
	servers, values, _, dropped, err := parseSubscription([]byte("invalid\n"+one+"\n"+one+"\n"+one), "auto", subscriptionParseTestSource, 1)
	if err != nil || len(servers) != 1 || len(values) != 1 || dropped != 3 {
		t.Fatal("duplicates or invalid entries consumed accepted-server quota")
	}
	var lines []string
	for i := 0; i < 513; i++ {
		lines = append(lines, subscriptionParseTestURI(fmt.Sprintf("limit-%d.example.test:443", i), subscriptionParseTestUser, "Node", nil))
	}
	servers, values, _, _, err = parseSubscription([]byte(strings.Join(lines[:512], "\n")), "auto", subscriptionParseTestSource, 512)
	if err != nil || len(servers) != 512 || len(values) != 512 {
		t.Fatal("exact quota refused or truncated")
	}
	servers, values, _, _, err = parseSubscription([]byte(strings.Join(lines, "\n")), "auto", subscriptionParseTestSource, 512)
	if !errors.Is(err, errSubscriptionQuota) || servers != nil || values != nil {
		t.Fatal("quota overflow was not atomically refused")
	}
}
