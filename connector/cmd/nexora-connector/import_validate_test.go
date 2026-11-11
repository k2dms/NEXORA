package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func importParsed(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := decodeImportJSON(data)
	if !ok {
		t.Fatal("test JSON could not be decoded")
	}
	return out
}

func importSet(doc map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		doc = doc[part].(map[string]any)
	}
	doc[parts[len(parts)-1]] = value
}

func importDelete(doc map[string]any, path string) {
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		doc = doc[part].(map[string]any)
	}
	delete(doc, parts[len(parts)-1])
}

func fullImportServer() map[string]any {
	s := testImportServer(importServerID)
	s["network"], s["security"] = "ws", "tls"
	s["flow"] = ""
	s["tls"] = map[string]any{"serverName": "example.test", "alpn": []any{"h3", "h2", "http/1.1"}, "fingerprint": "chrome", "insecure": false}
	s["ws"] = map[string]any{"path": "/path?x=1", "host": "example.test", "headersRef": importHeadersRef, "headersKeys": []any{"Host"}}
	s["grpc"] = map[string]any{"serviceName": "service/v1", "multiMode": true, "authority": "example.test", "headersRef": importHeadersRef, "headersKeys": []any{"Host"}}
	s["xhttp"] = map[string]any{"mode": "packet-up", "path": "/", "host": "example.test", "extra": ""}
	s["h2"] = map[string]any{"path": "/", "host": "example.test"}
	s["tag"], s["notes"], s["source"], s["subscriptionId"] = "srv-"+importServerID[:8], "", "subscription", importOtherServerID
	s["createdAt"], s["updatedAt"] = "2026-11-07T00:00:00Z", "2026-11-07t12:34:56.123+02:30"
	return s
}

func fullImportProfile() map[string]any {
	p := testImportProfile()
	p["selection"].(map[string]any)["stickySeconds"] = 86400
	p["failover"] = map[string]any{"enabled": true, "cooldownSeconds": 5, "maxConsecutiveFailures": 20, "probeIntervalSeconds": 3600}
	p["failurePolicy"] = map[string]any{"coreDeath": map[string]any{"action": "failOpen", "confirmToken": "confirm://accepted"}}
	p["routingPolicyId"], p["enabled"] = importOtherServerID, false
	p["createdAt"], p["updatedAt"] = "2026-11-07T00:00:00Z", "2026-11-07T12:34:56Z"
	return p
}

func TestImportValidationRealSchemaExamples(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no source path")
	}
	root := filepath.Join(filepath.Dir(file), "../../../packages/schema/examples")
	for _, invalid := range []bool{false, true} {
		dir := root
		if invalid {
			dir = filepath.Join(root, "invalid")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			server, profile := strings.HasPrefix(name, "server."), strings.HasPrefix(name, "profile.")
			if !server && !profile {
				continue
			}
			t.Run(name, func(t *testing.T) {
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				v, ok := decodeImportJSON(data)
				if !ok {
					t.Fatal("fixture is not JSON")
				}
				valid := validateImportServer(v)
				if profile {
					valid = validateImportProfile(v)
				}
				if valid == invalid {
					t.Fatalf("valid = %v, want %v", valid, !invalid)
				}
			})
		}
	}
}

func TestImportValidationRequiredAndNullFields(t *testing.T) {
	for _, group := range []struct {
		name     string
		makeDoc  func() map[string]any
		validate importRule
		required []string
	}{
		{"server", fullImportServer, validateImportServer, []string{"id", "displayName", "protocol", "address", "port", "network", "security", "userIdRef", "tls.serverName", "grpc.serviceName", "xhttp.mode", "xhttp.path", "h2.path", "subscriptionId"}},
		{"profile", fullImportProfile, validateImportProfile, []string{"id", "displayName", "kind", "serverIds", "selection", "selection.mode", "failover", "failover.enabled", "failurePolicy", "failurePolicy.coreDeath", "failurePolicy.coreDeath.action", "failurePolicy.coreDeath.confirmToken", "defaultServerId"}},
	} {
		t.Run(group.name, func(t *testing.T) {
			if !group.validate(importParsed(t, group.makeDoc())) {
				t.Fatal("complete schema document refused")
			}
			for _, path := range group.required {
				t.Run("missing "+path, func(t *testing.T) {
					doc := group.makeDoc()
					importDelete(doc, path)
					if group.validate(importParsed(t, doc)) {
						t.Fatal("missing required field accepted")
					}
				})
			}
			var nulls func(map[string]any, string)
			nulls = func(doc map[string]any, prefix string) {
				for key, value := range doc {
					path := prefix + key
					t.Run("null "+path, func(t *testing.T) {
						copy := group.makeDoc()
						importSet(copy, path, nil)
						if group.validate(importParsed(t, copy)) {
							t.Fatal("null schema field accepted")
						}
					})
					if nested, ok := value.(map[string]any); ok {
						nulls(nested, path+".")
					}
				}
			}
			nulls(group.makeDoc(), "")
		})
	}
}

func TestImportValidationClosedNestedShapes(t *testing.T) {
	for _, path := range []string{"", "tls", "ws", "grpc", "xhttp", "h2"} {
		s := fullImportServer()
		key := "secret-shaped-unknown"
		if path != "" {
			key = path + "." + key
		}
		importSet(s, key, "never echo this")
		if validateImportServer(importParsed(t, s)) {
			t.Fatalf("open server shape: %s", path)
		}
	}
	for _, path := range []string{"", "selection", "failover", "failurePolicy", "failurePolicy.coreDeath"} {
		p := fullImportProfile()
		key := "secret-shaped-unknown"
		if path != "" {
			key = path + "." + key
		}
		importSet(p, key, "never echo this")
		if validateImportProfile(importParsed(t, p)) {
			t.Fatalf("open profile shape: %s", path)
		}
	}
	s := testImportServer(importServerID)
	s["security"], s["reality"] = "reality", map[string]any{"publicKey": strings.Repeat("a", 43), "serverName": "example.test", "privateKey": "never echo"}
	if validateImportServer(importParsed(t, s)) {
		t.Fatal("REALITY private key accepted")
	}
}

func TestImportValidationServerEdges(t *testing.T) {
	cases := []struct {
		path  string
		value any
	}{
		{"id", strings.ToUpper(importServerID)}, {"id", strings.Replace(importServerID, "-4", "-7", 1)},
		{"displayName", ""}, {"displayName", strings.Repeat("界", 81)}, {"displayName", false},
		{"protocol", "vmess"}, {"address", "https://example.test"}, {"address", strings.Repeat("a", 254)},
		{"port", 0}, {"port", 65536}, {"port", 1.5}, {"port", "443"},
		{"port", json.Number("1.000000000000000000000001")}, {"port", json.Number("1e999999999")},
		{"network", "udp"}, {"security", "ssl"}, {"flow", "other"}, {"userIdRef", "not-a-reference"},
		{"tls.serverName", "bad_name"}, {"tls.alpn", []any{}}, {"tls.alpn", []any{"h2", "h2"}},
		{"tls.alpn", []any{"h2", "ftp"}}, {"tls.fingerprint", ""}, {"tls.fingerprint", strings.Repeat("a", 33)},
		{"tls.insecure", true}, {"tls.insecure", 0},
		{"ws.path", "relative"}, {"ws.path", "/has space"}, {"ws.path", "/" + strings.Repeat("a", 255)},
		{"ws.host", "user@example.test"}, {"ws.headersRef", strings.Repeat("a", 97)},
		{"ws.headersKeys", []any{}}, {"ws.headersKeys", []any{"Host", "Host"}},
		{"ws.headersKeys", []any{strings.Repeat("a", 129)}}, {"ws.headersKeys", []any{true}},
		{"grpc.serviceName", "bad service"}, {"grpc.serviceName", strings.Repeat("a", 129)},
		{"grpc.multiMode", "true"}, {"grpc.authority", strings.Repeat("a", 254)},
		{"xhttp.mode", "stream"}, {"xhttp.extra", strings.Repeat("a", 257)}, {"h2.path", ""},
		{"tag", "srv-deadbeeF"}, {"notes", strings.Repeat("a", 201)}, {"source", "unknown"},
		{"subscriptionId", "prefixonly"}, {"createdAt", "2026-02-30T00:00:00Z"},
		{"updatedAt", "2026-11-07"}, {"updatedAt", "2026-11-07T24:00:00Z"},
		{"updatedAt", "2026-11-07T00:00:00+24:00"}, {"updatedAt", "2026-11-07T00:00:00"},
	}
	for _, tc := range cases {
		t.Run(tc.path+"/"+stringMustJSON(tc.value), func(t *testing.T) {
			s := fullImportServer()
			importSet(s, tc.path, tc.value)
			if validateImportServer(importParsed(t, s)) {
				t.Fatal("schema edge accepted")
			}
		})
	}
	for _, value := range []json.Number{"1", "443", "65535", "1.0", "443.0", "443e0", "65535e0", "6553500e-2", "0.01e2"} {
		s := fullImportServer()
		s["port"] = value
		parsed := importParsed(t, s)
		if !importServerShape(parsed) {
			t.Fatalf("schema integer %s refused structurally", value)
		}
		_, err := value.Int64()
		if validateImportServer(parsed) != (err == nil) {
			t.Fatalf("port %s did not match the supported integer encoding subset", value)
		}
	}
}

func stringMustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestImportValidationServerConditionalsAndReality(t *testing.T) {
	for _, network := range []string{"ws", "grpc", "xhttp", "h2"} {
		s := testImportServer(importServerID)
		s["network"] = network
		if validateImportServer(importParsed(t, s)) {
			t.Fatalf("missing %s block accepted", network)
		}
		s = testImportServer(importServerID)
		s[network] = fullImportServer()[network]
		parsed := importParsed(t, s)
		if !importServerShape(parsed) {
			t.Fatalf("inactive %s block refused structurally", network)
		}
		if validateImportServer(parsed) {
			t.Fatalf("TCP with inactive %s block accepted", network)
		}
	}
	if !validateImportServer(importParsed(t, testImportServer(importServerID))) {
		t.Fatal("plain TCP refused")
	}
	s := fullImportServer()
	s["network"], s["flow"] = "tcp", "xtls-rprx-vision"
	for _, block := range []string{"ws", "grpc", "xhttp", "h2"} {
		delete(s, block)
	}
	if !validateImportServer(importParsed(t, s)) {
		t.Fatal("TCP with TLS and Vision refused")
	}
	for _, security := range []string{"tls", "reality"} {
		s := testImportServer(importServerID)
		s["security"] = security
		if validateImportServer(importParsed(t, s)) {
			t.Fatal("missing security block accepted")
		}
	}
	for _, mutate := range []func(map[string]any){
		func(s map[string]any) { s["security"] = "none" },
		func(s map[string]any) {
			s["reality"] = map[string]any{"publicKey": strings.Repeat("a", 43), "serverName": "example.test"}
		},
		func(s map[string]any) { s["flow"] = "xtls-rprx-vision" },
		func(s map[string]any) { s["network"], s["flow"] = "h2", ""; s["security"] = "none"; delete(s, "tls") },
	} {
		s := fullImportServer()
		mutate(s)
		if validateImportServer(importParsed(t, s)) {
			t.Fatal("invalid conditional accepted")
		}
	}
	base := func() map[string]any {
		s := testImportServer(importServerID)
		s["security"], s["reality"] = "reality", map[string]any{"publicKey": strings.Repeat("a", 43), "serverName": "example.test", "shortId": "", "spiderX": "", "fingerprint": "chrome"}
		return s
	}
	for _, shortID := range []string{"", "0011223344", "0011223344556677"} {
		s := base()
		s["reality"].(map[string]any)["shortId"] = shortID
		if !validateImportServer(importParsed(t, s)) {
			t.Fatal("valid REALITY short ID rejected")
		}
	}
	for _, tc := range []struct {
		path  string
		value any
	}{
		{"reality.publicKey", strings.Repeat("a", 42)}, {"reality.publicKey", strings.Repeat("+", 43)},
		{"reality.shortId", "a"}, {"reality.shortId", "ABCDEF"}, {"reality.shortId", "001122334455667788"},
		{"reality.serverName", ""}, {"reality.spiderX", strings.Repeat("a", 65)}, {"reality.fingerprint", ""},
	} {
		s := base()
		importSet(s, tc.path, tc.value)
		if validateImportServer(importParsed(t, s)) {
			t.Fatalf("REALITY edge accepted: %s", tc.path)
		}
	}
	s = base()
	s["network"], s["grpc"] = "grpc", map[string]any{"serviceName": "service"}
	if validateImportServer(importParsed(t, s)) {
		t.Fatal("REALITY over gRPC accepted")
	}
}

func TestImportValidationProfileEdges(t *testing.T) {
	for _, tc := range []struct {
		path  string
		value any
	}{
		{"kind", "fixed"}, {"displayName", ""}, {"displayName", strings.Repeat("a", 81)},
		{"id", strings.ToUpper(importProfileID)}, {"serverIds", []any{}}, {"serverIds", []any{importServerID, importServerID}},
		{"serverIds", []any{"bad"}}, {"serverIds", strings.Repeat("a", 10)},
		{"selection.mode", "random"}, {"selection.stickySeconds", -1}, {"selection.stickySeconds", 86401},
		{"selection.stickySeconds", 1.5}, {"failover.enabled", "false"}, {"enabled", 0},
		{"failover.cooldownSeconds", 4}, {"failover.cooldownSeconds", 3601},
		{"failover.maxConsecutiveFailures", 0}, {"failover.maxConsecutiveFailures", 21},
		{"failover.probeIntervalSeconds", 9}, {"failover.probeIntervalSeconds", 3601},
		{"failurePolicy.coreDeath.action", "direct"}, {"failurePolicy.coreDeath.confirmToken", "confirm://"},
		{"failurePolicy.coreDeath.confirmToken", "confirm://" + strings.Repeat("a", 65)},
		{"defaultServerId", "bad"}, {"routingPolicyId", "bad"}, {"updatedAt", "yesterday"},
	} {
		p := fullImportProfile()
		importSet(p, tc.path, tc.value)
		if validateImportProfile(importParsed(t, p)) {
			t.Fatalf("profile edge accepted: %s", tc.path)
		}
	}
	for _, mode := range []string{"ordered", "fastest", "roundRobin"} {
		p := testImportProfile()
		p["selection"].(map[string]any)["mode"] = mode
		delete(p, "defaultServerId")
		if !validateImportProfile(importParsed(t, p)) {
			t.Fatalf("mode %s without default refused", mode)
		}
	}
}

func TestImportValidationProfileIntegerEncodings(t *testing.T) {
	for _, path := range []string{"selection.stickySeconds", "failover.cooldownSeconds", "failover.maxConsecutiveFailures", "failover.probeIntervalSeconds"} {
		for _, value := range []json.Number{"10", "10.0", "10e0"} {
			t.Run(path+"/"+string(value), func(t *testing.T) {
				p := fullImportProfile()
				importSet(p, path, value)
				parsed := importParsed(t, p)
				if !importProfileShape(parsed) {
					t.Fatal("schema integer refused structurally")
				}
				if validateImportProfile(parsed) != (value == "10") {
					t.Fatal("profile did not match the supported integer encoding subset")
				}
			})
		}
	}
}

func TestImportValidationStrictJSONAndDates(t *testing.T) {
	for _, raw := range []string{`{"secret":1,"secret":2}`, `{} {}`, `{"x":`, `{"x":[1,]}`} {
		if _, ok := decodeImportJSON([]byte(raw)); ok {
			t.Fatalf("invalid JSON accepted: %s", raw)
		}
	}
	if _, ok := decodeImportJSON([]byte{'"', 0xff, '"'}); ok {
		t.Fatal("invalid UTF-8 accepted")
	}
	for _, raw := range []string{"2024-02-29T12:00:00Z", "2026-11-07t00:00:00z", "2016-12-31T23:59:60Z", "2017-01-01T00:59:60+01:00"} {
		if !importDateTime(raw) {
			t.Fatalf("valid RFC3339 timestamp refused: %s", raw)
		}
	}
	for _, raw := range []string{"2025-02-29T12:00:00Z", "2026-1-01T12:00:00Z", "2026-01-01T12:00:60Z", "2026-01-01T12:60:00Z"} {
		if importDateTime(raw) {
			t.Fatalf("invalid RFC3339 timestamp accepted: %s", raw)
		}
	}
}
