package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/keystore"
)

const (
	importServerID      = "3f2a1c9e-0b12-4c56-8a90-123456789abc" // nexora-secret-allow: synthetic entity identity
	importOtherServerID = "c7e5a2d1-1111-4222-8333-444444444444" // nexora-secret-allow: synthetic entity identity
	importProfileID     = "aaaa1111-1111-4222-8333-444444444444" // nexora-secret-allow: synthetic entity identity
	importUser          = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" // nexora-secret-allow: synthetic credential, nothing behind it
	importRotatedUser   = "b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e" // nexora-secret-allow: synthetic credential, nothing behind it
	importUserRef       = "keystore://server/3f2a1c9e/user-id"
	importHeadersRef    = "keystore://server/3f2a1c9e/headers"
)

type importTestProtector struct {
	calls  int
	failAt int
}

func (p *importTestProtector) Protect(value []byte) ([]byte, error) {
	p.calls++
	if p.calls == p.failAt {
		return nil, errors.New("untrusted protector error containing " + string(value))
	}
	return []byte(base64.StdEncoding.EncodeToString(value)), nil
}
func (*importTestProtector) Unprotect(blob []byte) ([]byte, error) {
	return base64.StdEncoding.DecodeString(string(blob))
}

func testImportStore(t *testing.T) (*keystore.Store, *importTestProtector, *profileImport) {
	t.Helper()
	p := &importTestProtector{}
	s, err := keystore.New(t.TempDir(), p)
	if err != nil {
		t.Fatal(err)
	}
	h, err := newProfileImport(s)
	if err != nil {
		t.Fatal(err)
	}
	return s, p, h
}

func testImportServer(id string) map[string]any {
	return map[string]any{"id": id, "displayName": "Manual server", "protocol": "vless", "address": "example.test", "port": 443, "network": "tcp", "security": "none", "userIdRef": "keystore://server/" + id[:8] + "/user-id"}
}
func testImportProfile() map[string]any {
	return map[string]any{"id": importProfileID, "displayName": "Manual profile", "kind": "connector", "serverIds": []any{importServerID}, "defaultServerId": importServerID, "selection": map[string]any{"mode": "fixed"}, "failover": map[string]any{"enabled": false}, "failurePolicy": map[string]any{"coreDeath": map[string]any{"action": "block"}}}
}
func testImportCredential(owner, field string, value any) map[string]any {
	return map[string]any{"entity": "server", "entityId": owner, "field": field, "value": value}
}
func testImportArgs(headers bool) map[string]any {
	s := testImportServer(importServerID)
	credentials := []any{testImportCredential(importServerID, "user-id", importUser)}
	if headers {
		s["network"] = "ws"
		s["ws"] = map[string]any{"path": "/", "headersRef": importHeadersRef, "headersKeys": []any{"Authorization", "Host"}}
		credentials = append(credentials, testImportCredential(importServerID[:8], "headers", map[string]any{"Authorization": "secret-header-marker", "Host": "example.test"}))
	}
	return map[string]any{"profiles": []any{testImportProfile()}, "servers": []any{s}, "credentials": credentials}
}
func testImportFrame(args any) *envelope.Frame {
	method := "profile.import"
	return &envelope.Frame{ID: "import-test-0001", Type: envelope.TypeRequest, Method: &method, Args: json.RawMessage(stringMustJSON(args))}
}
func testImportDisk(t *testing.T, s *keystore.Store) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, entry := range entries {
		blob, err := os.ReadFile(s.Dir() + "/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = blob
	}
	return out
}

func TestProfileImportSuccessReplayCountShapeAndRotation(t *testing.T) {
	s, p, h := testImportStore(t)
	args := testImportArgs(true)
	var prior []byte
	for i := 0; i < 2; i++ {
		answer, bad := h.Handle(context.Background(), testImportFrame(args))
		if bad != nil {
			t.Fatal(bad)
		}
		data, err := json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if len(result) != 3 || result["secretsExtracted"] != float64(2) {
			t.Fatalf("unexpected count-only result: %s", data)
		}
		imported := result["imported"].(map[string]any)
		if len(imported) != 2 || !reflect.DeepEqual(imported["profileIds"], []any{importProfileID}) || !reflect.DeepEqual(imported["serverIds"], []any{importServerID}) {
			t.Fatal("incorrect imported identities")
		}
		if rejected, ok := result["rejected"].([]any); !ok || len(rejected) != 0 {
			t.Fatal("rejected must be an empty array, never null")
		}
		if bytes.Contains(data, []byte(importUser)) || bytes.Contains(data, []byte("secret-header-marker")) || bytes.Contains(data, []byte("Manual server")) {
			t.Fatal("result echoed credentials or documents")
		}
		if prior != nil && !bytes.Equal(prior, data) {
			t.Fatal("replay changed the safe response")
		}
		prior = data
		if files := testImportDisk(t, s); len(files) != 2 {
			t.Fatal("request, document or scratch file persisted")
		}
	}
	// The handler itself retains only its Store, not the replay request/results.
	if reflect.TypeOf(*h).NumField() != 1 {
		t.Fatal("handler retained state other than its store")
	}
	if p.calls != 4 {
		t.Fatalf("protector calls = %d", p.calls)
	}
	args["credentials"].([]any)[0].(map[string]any)["value"] = importRotatedUser
	args["mode"] = "merge"
	answer, bad := h.Handle(context.Background(), testImportFrame(args))
	if bad != nil || answer.(*importAnswer).SecretsExtracted != 2 {
		t.Fatalf("rotation refused: %v", bad)
	}
	secret, err := s.Resolve(importUserRef)
	if err != nil || string(secret.Value) != importRotatedUser {
		t.Fatal("merge did not rotate the existing reference")
	}
	for _, blob := range testImportDisk(t, s) {
		if bytes.Contains(blob, []byte(importRotatedUser)) || bytes.Contains(blob, []byte("secret-header-marker")) {
			t.Fatal("plaintext persisted")
		}
	}
}

func TestProfileImportAlreadyStoredRefsAndDisabledProfiles(t *testing.T) {
	s, p, h := testImportStore(t)
	args := testImportArgs(true)
	if _, bad := h.Handle(context.Background(), testImportFrame(args)); bad != nil {
		t.Fatal(bad)
	}
	before := testImportDisk(t, s)
	calls := p.calls
	delete(args, "credentials")
	args["profiles"].([]any)[0].(map[string]any)["enabled"] = false
	args["contentHash"] = nil
	args["backupBeforeWrite"] = true
	answer, bad := h.Handle(context.Background(), testImportFrame(args))
	if bad != nil {
		t.Fatal(bad)
	}
	if answer.(*importAnswer).SecretsExtracted != 0 || p.calls != calls {
		t.Fatal("already stored refs counted as extracted or rewritten")
	}
	if !reflect.DeepEqual(before, testImportDisk(t, s)) {
		t.Fatal("refs-only import changed the store")
	}
}

func TestProfileImportCredentialExactAndDeclaredPrefixOwners(t *testing.T) {
	for _, owner := range []string{importServerID, importServerID[:8], importServerID[:13]} {
		for _, refID := range []string{importServerID, importServerID[:8], importServerID[:18]} {
			t.Run(owner+"/"+refID, func(t *testing.T) {
				s, _, h := testImportStore(t)
				args := testImportArgs(false)
				ref := "keystore://server/" + refID + "/user-id"
				args["servers"].([]any)[0].(map[string]any)["userIdRef"] = ref
				args["credentials"].([]any)[0].(map[string]any)["entityId"] = owner
				if _, bad := h.Handle(context.Background(), testImportFrame(args)); bad != nil {
					t.Fatal(bad)
				}
				if ok, err := s.Has(ref); err != nil || !ok {
					t.Fatal("did not confirm document's existing reference")
				}
				if len(testImportDisk(t, s)) != 1 {
					t.Fatal("minted a second unreferenced blob")
				}
			})
		}
	}
	servers := map[string]map[string]any{importServerID: {}, importServerID[:8] + "-1111-4222-8333-444444444444": {}} // nexora-secret-allow: synthetic identity
	if _, ok := resolveImportOwner(importServerID[:8], servers); ok {
		t.Fatal("ambiguous declared prefix resolved")
	}
	if _, ok := resolveImportOwner("", map[string]map[string]any{}); ok {
		t.Fatal("undeclared prefix resolved")
	}
}

func TestProfileImportInvalidLastDocumentOrCredentialNeverWrites(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"last server", func(a map[string]any) {
			last := testImportServer(importOtherServerID)
			last["port"] = 0
			a["servers"] = append(a["servers"].([]any), last)
		}},
		{"last disabled profile", func(a map[string]any) {
			last := testImportProfile()
			last["id"] = importOtherServerID
			last["enabled"] = false
			last["failover"].(map[string]any)["probeIntervalSeconds"] = 0
			a["profiles"] = append(a["profiles"].([]any), last)
		}},
		{"last credential orphan", func(a map[string]any) {
			a["credentials"] = append(a["credentials"].([]any), testImportCredential(importOtherServerID, "user-id", importUser))
		}},
		{"last credential wrong shape", func(a map[string]any) {
			a["credentials"].([]any)[1].(map[string]any)["value"] = map[string]any{"Authorization": 7, "Host": "example.test"}
		}},
		{"last credential undeclared key", func(a map[string]any) {
			a["credentials"].([]any)[1].(map[string]any)["secret-key-marker"] = "secret-value-marker"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, p, h := testImportStore(t)
			if err := s.Put(importUserRef, []byte(importRotatedUser)); err != nil {
				t.Fatal(err)
			}
			before, calls := testImportDisk(t, s), p.calls
			args := testImportArgs(true)
			tc.mutate(args)
			answer, bad := h.Handle(context.Background(), testImportFrame(args))
			if bad == nil || answer != nil {
				t.Fatal("invalid final entry accepted")
			}
			if p.calls != calls || !reflect.DeepEqual(before, testImportDisk(t, s)) {
				t.Fatal("invalid final entry wrote or staged earlier credentials")
			}
			if strings.Contains(bad.Error(), "secret-key-marker") || strings.Contains(bad.Error(), "secret-value-marker") {
				t.Fatal("refusal echoed input")
			}
		})
	}
}

func TestProfileImportUnsupportedCompilerInputsNeverWrites(t *testing.T) {
	for _, tc := range []struct {
		document, path string
		values         []any
	}{
		{"server", "ws", []any{map[string]any{}}},
		{"server", "grpc", []any{map[string]any{"serviceName": "service"}}},
		{"server", "xhttp", []any{map[string]any{"mode": "auto", "path": "/"}}},
		{"server", "h2", []any{map[string]any{"path": "/"}}},
		{"server", "port", []any{json.Number("443.0"), json.Number("443e0")}},
		{"profile", "selection.stickySeconds", []any{json.Number("10.0"), json.Number("10e0")}},
		{"profile", "failover.cooldownSeconds", []any{json.Number("10.0"), json.Number("10e0")}},
		{"profile", "failover.maxConsecutiveFailures", []any{json.Number("10.0"), json.Number("10e0")}},
		{"profile", "failover.probeIntervalSeconds", []any{json.Number("10.0"), json.Number("10e0")}},
	} {
		for _, value := range tc.values {
			for _, existing := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%s/existing=%t", tc.document, tc.path, stringMustJSON(value), existing), func(t *testing.T) {
					s, p, h := testImportStore(t)
					if existing {
						if err := s.Put(importUserRef, []byte(importRotatedUser)); err != nil {
							t.Fatal(err)
						}
					}
					before, calls := testImportDisk(t, s), p.calls
					a := testImportArgs(true)
					last := testImportServer(importOtherServerID)
					shape := importServerShape
					if tc.document == "profile" {
						last = testImportProfile()
						last["id"], last["enabled"] = importOtherServerID, false
						shape = importProfileShape
					}
					last["displayName"] = "secret-value-marker"
					importSet(last, tc.path, value)
					if !shape(importParsed(t, last)) {
						t.Fatal("regression input is not structurally schema-legal")
					}
					a[tc.document+"s"] = append(a[tc.document+"s"].([]any), last)
					if tc.document == "server" {
						a["credentials"] = append(a["credentials"].([]any), testImportCredential(importOtherServerID, "user-id", importUser))
					}
					answer, bad := h.Handle(context.Background(), testImportFrame(a))
					if answer != nil || bad == nil || bad.Code != "InvalidProfile" {
						t.Fatalf("unsupported compiler input refusal = %v", bad)
					}
					if p.calls != calls || !reflect.DeepEqual(before, testImportDisk(t, s)) {
						t.Fatal("unsupported compiler input protected or wrote credentials")
					}
					for _, secret := range []string{importUser, importRotatedUser, "secret-header-marker", "secret-value-marker"} {
						if strings.Contains(bad.Error(), secret) {
							t.Fatal("refusal echoed input")
						}
					}
				})
			}
		}
	}
}

func TestProfileImportPlainTCPAndIntegerFieldsRemainValid(t *testing.T) {
	s, p, h := testImportStore(t)
	a := testImportArgs(false)
	a["profiles"] = []any{fullImportProfile()}
	answer, bad := h.Handle(context.Background(), testImportFrame(a))
	if bad != nil {
		t.Fatal(bad)
	}
	if answer.(*importAnswer).SecretsExtracted != 1 || p.calls != 1 || len(testImportDisk(t, s)) != 1 {
		t.Fatal("normal TCP and integer fields did not import")
	}
}

func TestProfileImportCredentialOrphansMismatchesAndCollisions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"orphan", func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["entityId"] = importOtherServerID }},
		{"owner exists only in store", func(a map[string]any) {
			a["credentials"] = append(a["credentials"].([]any), testImportCredential(importOtherServerID, "user-id", importUser))
		}},
		{"undeclared prefix", func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["entityId"] = "12345678" }},
		{"missing referenced field", func(a map[string]any) {
			a["credentials"] = append(a["credentials"].([]any), testImportCredential(importServerID, "headers", map[string]any{"Host": "secret"}))
		}},
		{"reference owner mismatch", func(a map[string]any) {
			a["servers"].([]any)[0].(map[string]any)["userIdRef"] = "keystore://server/" + importOtherServerID + "/user-id"
		}},
		{"reference field mismatch", func(a map[string]any) { a["servers"].([]any)[0].(map[string]any)["userIdRef"] = importHeadersRef }},
		{"malformed reference", func(a map[string]any) {
			a["servers"].([]any)[0].(map[string]any)["userIdRef"] = "keystore://server/not-an-id/user-id"
		}},
		{"duplicate credential alias", func(a map[string]any) {
			a["credentials"] = append(a["credentials"].([]any), testImportCredential(importServerID[:8], "user-id", importRotatedUser))
		}},
		{"UUID empty", func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["value"] = "" }},
		{"UUID malformed", func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["value"] = "secret-not-a-UUID" }},
		{"UUID object", func(a map[string]any) {
			a["credentials"].([]any)[0].(map[string]any)["value"] = map[string]any{"token": "secret"}
		}},
	}
	for _, entity := range []string{"subscription", "profile", "sync", "host"} {
		cases = append(cases, struct {
			name   string
			mutate func(map[string]any)
		}{entity, func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["entity"] = entity }})
	}
	for _, field := range []string{"url", "token"} {
		cases = append(cases, struct {
			name   string
			mutate func(map[string]any)
		}{field, func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["field"] = field }})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, p, h := testImportStore(t)
			otherRef := "keystore://server/" + importOtherServerID[:8] + "/user-id"
			if err := s.Put(otherRef, []byte(importUser)); err != nil {
				t.Fatal(err)
			}
			before, calls := testImportDisk(t, s), p.calls
			args := testImportArgs(false)
			tc.mutate(args)
			answer, bad := h.Handle(context.Background(), testImportFrame(args))
			if answer != nil || bad == nil || bad.Code != "InvalidProfile" {
				t.Fatalf("refusal = %v", bad)
			}
			if p.calls != calls || !reflect.DeepEqual(before, testImportDisk(t, s)) {
				t.Fatal("invalid credential wrote to store")
			}
		})
	}
}

func TestProfileImportHeaderShapesKeysAndReferenceCollisions(t *testing.T) {
	for _, network := range []string{"ws", "grpc"} {
		for _, tc := range []struct {
			name   string
			mutate func(map[string]any, map[string]any)
		}{
			{"empty map", func(c, b map[string]any) { c["value"] = map[string]any{} }},
			{"empty value", func(c, b map[string]any) { c["value"] = map[string]any{"Authorization": "", "Host": "example.test"} }},
			{"long value", func(c, b map[string]any) {
				c["value"] = map[string]any{"Authorization": strings.Repeat("x", 4097), "Host": "example.test"}
			}},
			{"nested secret object", func(c, b map[string]any) {
				c["value"] = map[string]any{"Authorization": map[string]any{"secret-key-marker": "secret-value-marker"}, "Host": "example.test"}
			}},
			{"unexpected key", func(c, b map[string]any) { c["value"] = map[string]any{"Cookie": "secret", "Host": "example.test"} }},
			{"missing key", func(c, b map[string]any) { c["value"] = map[string]any{"Host": "example.test"} }},
			{"not object", func(c, b map[string]any) { c["value"] = "secret" }},
			{"missing keys", func(c, b map[string]any) { delete(b, "headersKeys") }},
			{"missing ref", func(c, b map[string]any) { delete(b, "headersRef") }},
			{"ref wrong field", func(c, b map[string]any) { b["headersRef"] = importUserRef }},
			{"ref wrong entity", func(c, b map[string]any) { b["headersRef"] = "keystore://profile/3f2a1c9e/headers" }},
		} {
			t.Run(network+"/"+tc.name, func(t *testing.T) {
				s, _, h := testImportStore(t)
				a := testImportArgs(true)
				server := a["servers"].([]any)[0].(map[string]any)
				block := server["ws"].(map[string]any)
				if network == "grpc" {
					delete(server, "ws")
					delete(block, "path")
					block["serviceName"] = "service"
					server["grpc"], server["network"] = block, network
				}
				tc.mutate(a["credentials"].([]any)[1].(map[string]any), block)
				if _, bad := h.Handle(context.Background(), testImportFrame(a)); bad == nil || bad.Code != "InvalidProfile" {
					t.Fatalf("refusal = %v", bad)
				}
				if len(testImportDisk(t, s)) != 0 {
					t.Fatal("invalid headers wrote earlier user credential")
				}
			})
		}
	}
	for _, differentPrefix := range []bool{false, true} {
		s, _, h := testImportStore(t)
		a := testImportArgs(true)
		server := a["servers"].([]any)[0].(map[string]any)
		ref := importHeadersRef
		if differentPrefix {
			ref = "keystore://server/" + importServerID + "/headers"
		}
		server["grpc"] = map[string]any{"serviceName": "service", "headersRef": ref, "headersKeys": []any{"Authorization", "Host"}}
		if _, bad := h.Handle(context.Background(), testImportFrame(a)); bad == nil || bad.Code != "InvalidProfile" {
			t.Fatalf("colliding headers accepted: %v", bad)
		}
		if len(testImportDisk(t, s)) != 0 {
			t.Fatal("reference collision wrote secrets")
		}
	}
	// Boundary maximums, including case-sensitive exact header name matching.
	s, _, h := testImportStore(t)
	a := testImportArgs(true)
	server := a["servers"].([]any)[0].(map[string]any)
	keys := []any{}
	headers := map[string]any{}
	for i := 0; i < 32; i++ {
		key := fmt.Sprintf("%03d", i) + strings.Repeat("k", 125)
		keys = append(keys, key)
		headers[key] = strings.Repeat("界", 4096)
	}
	server["ws"].(map[string]any)["headersKeys"] = keys
	a["credentials"].([]any)[1].(map[string]any)["value"] = headers
	if _, bad := h.Handle(context.Background(), testImportFrame(a)); bad != nil {
		t.Fatal(bad)
	}
	if len(testImportDisk(t, s)) != 2 {
		t.Fatal("maximum sized headers did not import")
	}
}

func TestProfileImportStoredSecretValidationAndMissingRefs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		user    []byte
		headers []byte
	}{
		{"missing", nil, nil}, {"bad UUID", []byte("not UUID"), nil},
		{"headers missing", []byte(importUser), nil}, {"headers wrong keys", []byte(importUser), []byte(`{"Cookie":"secret","Host":"example.test"}`)},
		{"headers empty value", []byte(importUser), []byte(`{"Authorization":"","Host":"example.test"}`)},
		{"headers duplicate keys", []byte(importUser), []byte(`{"Authorization":"first","Authorization":"last","Host":"example.test"}`)},
		{"headers nested", []byte(importUser), []byte(`{"Authorization":{"token":"secret"},"Host":"example.test"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, h := testImportStore(t)
			if tc.user != nil {
				if err := s.Put(importUserRef, tc.user); err != nil {
					t.Fatal(err)
				}
			}
			if tc.headers != nil {
				if err := s.Put(importHeadersRef, tc.headers); err != nil {
					t.Fatal(err)
				}
			}
			before, calls := testImportDisk(t, s), p.calls
			a := testImportArgs(true)
			delete(a, "credentials")
			if _, bad := h.Handle(context.Background(), testImportFrame(a)); bad == nil || bad.Code != "InvalidProfile" {
				t.Fatalf("refusal = %v", bad)
			}
			if p.calls != calls || !reflect.DeepEqual(before, testImportDisk(t, s)) {
				t.Fatal("invalid stored ref changed the store")
			}
		})
	}
}

func TestProfileImportIdentityMembershipAndTagValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"duplicate server", func(a map[string]any) { a["servers"] = append(a["servers"].([]any), testImportServer(importServerID)) }},
		{"duplicate profile", func(a map[string]any) { a["profiles"] = append(a["profiles"].([]any), testImportProfile()) }},
		{"missing server reference", func(a map[string]any) {
			a["profiles"].([]any)[0].(map[string]any)["serverIds"] = []any{importOtherServerID}
		}},
		{"default outside members", func(a map[string]any) {
			a["profiles"].([]any)[0].(map[string]any)["defaultServerId"] = importOtherServerID
		}},
		{"derived tag collision", func(a map[string]any) {
			id := importServerID[:8] + "-1111-4222-8333-444444444444"
			a["servers"] = append(a["servers"].([]any), testImportServer(id))
		}}, // nexora-secret-allow: synthetic identity
		{"tag mismatch", func(a map[string]any) { a["servers"].([]any)[0].(map[string]any)["tag"] = "srv-12345678" }},
		{"unused server missing secret", func(a map[string]any) {
			a["servers"] = append(a["servers"].([]any), testImportServer(importOtherServerID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, h := testImportStore(t)
			a := testImportArgs(false)
			tc.mutate(a)
			a["profiles"].([]any)[0].(map[string]any)["enabled"] = false
			if _, bad := h.Handle(context.Background(), testImportFrame(a)); bad == nil || bad.Code != "InvalidProfile" {
				t.Fatalf("refusal = %v", bad)
			}
			if len(testImportDisk(t, s)) != 0 {
				t.Fatal("invalid disabled or unused document wrote secrets")
			}
		})
	}
}

func TestProfileImportUnsupportedModesAndArgumentBounds(t *testing.T) {
	cases := []struct {
		name, code string
		mutate     func(map[string]any)
	}{
		{"replace", "Busy", func(a map[string]any) { a["mode"] = "replace"; a["backupBeforeWrite"] = true }},
		{"replace without backup claim", "Busy", func(a map[string]any) { a["mode"] = "replace" }},
		{"chunk", "BadRequest", func(a map[string]any) {
			a["chunk"] = map[string]any{"contentHash": strings.Repeat("a", 64), "chunkSeq": 0, "chunkCount": 2, "encoding": "json"}
			a["contentHash"] = strings.Repeat("a", 64)
		}},
		{"chunk null", "BadRequest", func(a map[string]any) { a["chunk"] = nil }},
		{"hash", "BadRequest", func(a map[string]any) { a["contentHash"] = strings.Repeat("a", 64) }},
		{"bad hash", "BadRequest", func(a map[string]any) { a["contentHash"] = "secret-marker" }},
		{"hash number", "BadRequest", func(a map[string]any) { a["contentHash"] = 123 }},
		{"mode null", "BadRequest", func(a map[string]any) { a["mode"] = nil }},
		{"backup false", "BadRequest", func(a map[string]any) { a["backupBeforeWrite"] = false }},
		{"backup null", "BadRequest", func(a map[string]any) { a["backupBeforeWrite"] = nil }},
		{"unknown args", "BadRequest", func(a map[string]any) { a["secret-key-marker"] = "secret-value-marker" }},
		{"case alias", "BadRequest", func(a map[string]any) { a["Profiles"] = a["profiles"] }},
		{"profiles missing", "BadRequest", func(a map[string]any) { delete(a, "profiles") }},
		{"profiles empty", "BadRequest", func(a map[string]any) { a["profiles"] = []any{} }},
		{"profiles null", "BadRequest", func(a map[string]any) { a["profiles"] = nil }},
		{"profiles object", "BadRequest", func(a map[string]any) { a["profiles"] = map[string]any{} }},
		{"profiles too many", "BadRequest", func(a map[string]any) {
			list := []any{}
			for i := 0; i < 257; i++ {
				list = append(list, testImportProfile())
			}
			a["profiles"] = list
		}},
		{"servers null", "BadRequest", func(a map[string]any) { a["servers"] = nil }},
		{"servers too many", "BadRequest", func(a map[string]any) {
			list := []any{}
			for i := 0; i < 513; i++ {
				list = append(list, testImportServer(importServerID))
			}
			a["servers"] = list
		}},
		{"credentials null", "BadRequest", func(a map[string]any) { a["credentials"] = nil }},
		{"credentials too many", "BadRequest", func(a map[string]any) {
			list := []any{}
			for i := 0; i < 513; i++ {
				list = append(list, testImportCredential(importServerID, "user-id", importUser))
			}
			a["credentials"] = list
		}},
		{"credentials unknown property", "BadRequest", func(a map[string]any) {
			a["credentials"].([]any)[0].(map[string]any)["secret-key-marker"] = "secret-value-marker"
		}},
		{"credential value null", "BadRequest", func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["value"] = nil }},
		{"credential string oversized", "BadRequest", func(a map[string]any) {
			a["credentials"].([]any)[0].(map[string]any)["value"] = strings.Repeat("a", 4097)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, p, h := testImportStore(t)
			a := testImportArgs(false)
			tc.mutate(a)
			answer, bad := h.Handle(context.Background(), testImportFrame(a))
			if answer != nil || bad == nil || bad.Code != tc.code {
				t.Fatalf("refusal = %v, want %s", bad, tc.code)
			}
			if p.calls != 0 || len(testImportDisk(t, s)) != 0 {
				t.Fatal("unsupported or malformed args wrote secrets")
			}
			if strings.Contains(bad.Error(), "secret-key-marker") || strings.Contains(bad.Error(), "secret-value-marker") {
				t.Fatal("refusal echoed args")
			}
		})
	}
	s, _, h := testImportStore(t)
	f := testImportFrame(testImportArgs(false))
	f.Chunk = &envelope.Chunk{}
	if _, bad := h.Handle(context.Background(), f); bad == nil || bad.Code != "BadRequest" {
		t.Fatal("envelope-only chunk accepted")
	}
	if len(testImportDisk(t, s)) != 0 {
		t.Fatal("chunk wrote secrets")
	}
}

func TestProfileImportMalformedAndUnknownSecretFieldsDoNotEcho(t *testing.T) {
	_, _, h := testImportStore(t)
	for _, raw := range []string{"", `null`, `[]`, `{"secret-key-marker":"secret-value-marker"}`, `{"profiles":`, `{"profiles":[],"profiles":[]}`, `{} {"secret-key-marker":"secret-value-marker"}`} {
		_, bad := h.Handle(context.Background(), &envelope.Frame{Args: json.RawMessage(raw)})
		if bad == nil || bad.Code != "BadRequest" {
			t.Fatalf("malformed args refusal = %v", bad)
		}
		if strings.Contains(bad.Error(), "secret-key-marker") || strings.Contains(bad.Error(), "secret-value-marker") {
			t.Fatal("decoder error echoed payload")
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"server inline user ID", func(a map[string]any) {
			a["servers"].([]any)[0].(map[string]any)["user-id-secret-key-marker"] = "secret-value-marker"
		}},
		{"profile nested", func(a map[string]any) {
			a["profiles"].([]any)[0].(map[string]any)["failurePolicy"].(map[string]any)["coreDeath"].(map[string]any)["secret-key-marker"] = "secret-value-marker"
		}},
		{"WS inline headers", func(a map[string]any) {
			a["servers"].([]any)[0].(map[string]any)["ws"].(map[string]any)["headers"] = map[string]any{"secret-key-marker": "secret-value-marker"}
		}},
		{"malformed reference tail", func(a map[string]any) {
			a["servers"].([]any)[0].(map[string]any)["userIdRef"] = "keystore://secret-value-marker"
		}},
	} {
		a := testImportArgs(true)
		tc.mutate(a)
		_, bad := h.Handle(context.Background(), testImportFrame(a))
		if bad == nil || strings.Contains(bad.Error(), "secret-key-marker") || strings.Contains(bad.Error(), "secret-value-marker") {
			t.Fatalf("unsafe %s refusal: %v", tc.name, bad)
		}
	}
}

func TestProfileImportProtectorSecondSecretFailureIsSafeAndByteIdentical(t *testing.T) {
	s, p, h := testImportStore(t)
	if err := s.Put(importUserRef, []byte(importRotatedUser)); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(importHeadersRef, []byte(`{"Host":"old"}`)); err != nil {
		t.Fatal(err)
	}
	before := testImportDisk(t, s)
	p.calls, p.failAt = 0, 2
	answer, bad := h.Handle(context.Background(), testImportFrame(testImportArgs(true)))
	if answer != nil || bad == nil || bad.Code != "KeystoreUnavailable" {
		t.Fatalf("refusal = %v", bad)
	}
	if strings.Contains(bad.Error(), importUser) || strings.Contains(bad.Error(), "secret-header-marker") {
		t.Fatal("protector failure echoed secret")
	}
	if !reflect.DeepEqual(before, testImportDisk(t, s)) {
		t.Fatal("second protection failure changed any stored byte")
	}
}

func TestProfileImportMaximumSingleFrameCounts(t *testing.T) {
	s, p, h := testImportStore(t)
	servers, credentials, profiles, ids := []any{}, []any{}, []any{}, []any{}
	for i := 0; i < 512; i++ {
		id := fmt.Sprintf("%08x-1111-4222-8333-444444444444", i+1) // nexora-secret-allow: synthetic entity identity
		servers = append(servers, testImportServer(id))
		credentials = append(credentials, testImportCredential(id, "user-id", importUser))
		ids = append(ids, id)
	}
	for i := 0; i < 256; i++ {
		profile := testImportProfile()
		profile["id"] = fmt.Sprintf("%08x-1111-4222-8333-444444444444", i+1024) // nexora-secret-allow: synthetic entity identity
		profile["serverIds"], profile["defaultServerId"] = []any{ids[i]}, ids[i]
		profiles = append(profiles, profile)
	}
	// Also exercise the profile schema's 512-member serverIds ceiling.
	profiles[0].(map[string]any)["serverIds"] = ids
	args := map[string]any{"profiles": profiles, "servers": servers, "credentials": credentials}
	answer, bad := h.Handle(context.Background(), testImportFrame(args))
	if bad != nil {
		t.Fatal(bad)
	}
	result := answer.(*importAnswer)
	if len(result.Imported.ProfileIDs) != 256 || len(result.Imported.ServerIDs) != 512 || result.SecretsExtracted != 512 || p.calls != 512 {
		t.Fatal("single-frame count ceilings were not supported")
	}
	if len(testImportDisk(t, s)) != 512 {
		t.Fatal("unexpected persisted state at the count ceilings")
	}
}

func TestProfileImportConstructorCancellationAndEmptyHash(t *testing.T) {
	if _, err := newProfileImport(nil); err == nil {
		t.Fatal("nil store accepted")
	}
	s, p, h := testImportStore(t)
	if _, bad := h.Handle(context.Background(), nil); bad == nil || bad.Code != "BadRequest" {
		t.Fatal("nil frame accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, bad := h.Handle(ctx, testImportFrame(testImportArgs(false))); bad == nil || bad.Code != "Timeout" {
		t.Fatal("canceled call wrote")
	}
	if p.calls != 0 || len(testImportDisk(t, s)) != 0 {
		t.Fatal("canceled call had side effects")
	}
	a := testImportArgs(false)
	a["contentHash"] = ""
	a["credentials"].([]any)[0].(map[string]any)["value"] = strings.ToUpper(importUser)
	if _, bad := h.Handle(context.Background(), testImportFrame(a)); bad != nil {
		t.Fatal(bad)
	}
}

func testImportSubscriptionArgs() map[string]any {
	source := map[string]any{
		"id": importProfileID, "displayName": "Subscription", "url": "https://example.test/",
		"urlRef": "keystore://subscription/" + importProfileID + "/url", "format": "auto",
		"limits":       map[string]any{"maxBytes": 2097152, "maxRedirects": 3, "timeoutMs": 20000, "maxServers": 512},
		"keepLastGood": true, "schedule": map[string]any{"intervalSeconds": 3600},
		"status": map[string]any{"consecutiveFailures": 0}, "serverIds": []any{},
	}
	return map[string]any{
		"profiles": []any{}, "subscriptions": []any{source},
		"credentials": []any{map[string]any{"entity": "subscription", "entityId": importProfileID, "field": "url", "value": "https://example.test/private-feed?token=synthetic-marker"}},
	}
}

func TestProfileImportSubscriptionURLStorageRotationAndSafeResult(t *testing.T) {
	s, p, h := testImportStore(t)
	args := testImportSubscriptionArgs()
	for _, address := range []string{"https://example.test/private-feed?token=synthetic-marker", "https://example.test/replacement?key=rotated-marker"} {
		args["credentials"].([]any)[0].(map[string]any)["value"] = address
		answer, bad := h.Handle(context.Background(), testImportFrame(args))
		if bad != nil {
			t.Fatal(bad)
		}
		result := answer.(*importAnswer)
		if result.SecretsExtracted != 1 || len(result.Imported.ProfileIDs) != 0 || len(result.Imported.ServerIDs) != 0 {
			t.Fatal("source-only import did not return its count-only result")
		}
		data, _ := json.Marshal(answer)
		if bytes.Contains(data, []byte(address)) || bytes.Contains(data, []byte("Subscription")) {
			t.Fatal("subscription credential or document escaped in the result")
		}
		secret, err := s.Resolve("keystore://subscription/" + importProfileID + "/url")
		if err != nil || string(secret.Value) != address {
			t.Fatal("URL was not stored or rotated under its declared owner")
		}
		for _, blob := range testImportDisk(t, s) {
			if bytes.Contains(blob, []byte(address)) {
				t.Fatal("plaintext URL persisted")
			}
		}
	}
	before, calls := testImportDisk(t, s), p.calls
	delete(args, "credentials")
	if _, bad := h.Handle(context.Background(), testImportFrame(args)); bad != nil {
		t.Fatal(bad)
	}
	if p.calls != calls || !reflect.DeepEqual(before, testImportDisk(t, s)) {
		t.Fatal("reference-only import rewrote an existing URL")
	}
}

func TestProfileImportSubscriptionFailuresPreserveStoredURL(t *testing.T) {
	cases := map[string]func(map[string]any){
		"http": func(a map[string]any) {
			a["credentials"].([]any)[0].(map[string]any)["value"] = "http://example.test/private-feed"
		},
		"userinfo": func(a map[string]any) {
			a["credentials"].([]any)[0].(map[string]any)["value"] = "https://user:password@example.test/"
		},
		"fragment": func(a map[string]any) {
			a["credentials"].([]any)[0].(map[string]any)["value"] = "https://example.test/#secret-marker"
		},
		"wrong owner":       func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["entityId"] = importOtherServerID },
		"wrong field":       func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["field"] = "token" },
		"wrong entity":      func(a map[string]any) { a["credentials"].([]any)[0].(map[string]any)["entity"] = "server" },
		"orphan credential": func(a map[string]any) { delete(a["subscriptions"].([]any)[0].(map[string]any), "urlRef") },
		"wrong ref owner": func(a map[string]any) {
			a["subscriptions"].([]any)[0].(map[string]any)["urlRef"] = "keystore://subscription/" + importOtherServerID + "/url"
		},
		"wrong ref entity": func(a map[string]any) {
			a["subscriptions"].([]any)[0].(map[string]any)["urlRef"] = "keystore://server/" + importProfileID + "/url"
		},
		"duplicate credential": func(a map[string]any) {
			a["credentials"] = append(a["credentials"].([]any), a["credentials"].([]any)[0])
		},
		"duplicate source": func(a map[string]any) {
			a["subscriptions"] = append(a["subscriptions"].([]any), a["subscriptions"].([]any)[0])
		},
		"public token query": func(a map[string]any) {
			a["subscriptions"].([]any)[0].(map[string]any)["url"] = "https://example.test/?token=synthetic-marker"
		},
		"invalid server before URL": func(a map[string]any) { a["servers"] = []any{map[string]any{"id": importServerID}} },
		"empty import":              func(a map[string]any) { delete(a, "subscriptions"); delete(a, "credentials") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, p, h := testImportStore(t)
			if _, bad := h.Handle(context.Background(), testImportFrame(testImportSubscriptionArgs())); bad != nil {
				t.Fatal(bad)
			}
			before, calls := testImportDisk(t, s), p.calls
			args := testImportSubscriptionArgs()
			mutate(args)
			if answer, bad := h.Handle(context.Background(), testImportFrame(args)); bad == nil || answer != nil {
				t.Fatal("invalid subscription import accepted")
			}
			if p.calls != calls || !reflect.DeepEqual(before, testImportDisk(t, s)) {
				t.Fatal("failed replacement changed the protected last-good URL")
			}
		})
	}
}
