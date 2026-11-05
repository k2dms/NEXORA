package apply

import (
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/compile"
	"github.com/k2dms/nexora/connector/internal/keystore"
)

// The two references and one value these tests are written from, in the same shape keystore's tests use: a VLESS
// user id is the credential the store exists to hold, and tools/check-no-secrets.mjs reads any uuid-shaped string as
// one — including in a `_test.go`, which sits beside its package and so outside the guard's fixture exemptions.
const (
	userIDRef = "keystore://server/3f2a1c9e/user-id"
	headerRef = "keystore://server/c7e5a2d1/headers"
	userID    = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d" // nexora-secret-allow: synthetic test identity, nothing behind it
)

// fakeStore is the Resolver a test uses: the substitution is about which position a value reaches, and a runner
// with no DPAPI still has to be able to prove that.
type fakeStore map[string]string

func (f fakeStore) Resolve(ref string) (keystore.Secret, error) {
	parsed, err := keystore.Parse(ref)
	if err != nil {
		return keystore.Secret{}, err
	}
	value, ok := f[ref]
	if !ok {
		return keystore.Secret{}, keystore.ErrNotFound
	}
	return keystore.Secret{Ref: parsed, Value: []byte(value)}, nil
}

func serverDoc() compile.Document {
	return compile.Document{
		"inbounds": []any{map[string]any{"port": 10808, "listen": "127.0.0.1"}},
		"outbounds": []any{map[string]any{
			"tag": "srv-a",
			"settings": map[string]any{"vnext": []any{map[string]any{
				"users":   []any{map[string]any{"id": userIDRef, "level": 1}},
				"address": "example.invalid",
				"port":    443,
			}}},
		}},
		"log": map[string]any{"loglevel": "warning"},
	}
}

// atUserID walks the position serverDoc puts its handle in, so the assertions below read as claims about that one
// field rather than as five type assertions.
func atUserID(t *testing.T, doc compile.Document) any {
	t.Helper()
	outbound := doc["outbounds"].([]any)[0].(map[string]any)
	vnext := outbound["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)
	return vnext["users"].([]any)[0].(map[string]any)["id"]
}

func TestMaterializeReplacesEveryHandlePosition(t *testing.T) {
	in := serverDoc()
	out, err := Materialize(in, fakeStore{userIDRef: userID})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got := atUserID(t, out); got != userID {
		t.Errorf("materialized id = %v, want the stored value", got)
	}
	if got := atUserID(t, in); got != userIDRef {
		t.Errorf("the document handed in now carries %v; the handle-bearing one is what the goldens and ODEC-36's other half need intact", got)
	}
}

func TestMaterializedEmitIsDeterministicAndCarriesNoHandle(t *testing.T) {
	out, err := Materialize(serverDoc(), fakeStore{userIDRef: userID})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	first, err := compile.Emit(out)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	again, err := Materialize(serverDoc(), fakeStore{userIDRef: userID})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	second, err := compile.Emit(again)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if string(first) != string(second) {
		t.Error("the materialized document is not byte-stable, which makes configHash over these bytes meaningless")
	}
	if strings.Contains(string(first), "keystore://") {
		t.Errorf("the written bytes still carry a handle: %s", first)
	}
	if !strings.Contains(string(first), userID) {
		t.Error("the written bytes do not carry the value, so Core would be handed a reference it cannot read")
	}
}

func TestMaterializeRefusesAnObjectHandle(t *testing.T) {
	doc := compile.Document{"outbounds": []any{map[string]any{
		"streamSettings": map[string]any{"wsSettings": map[string]any{"headers": headerRef}},
	}}}
	// The resolver holds a map for this reference, so the refusal below cannot be an unavailable store.
	_, err := Materialize(doc, fakeStore{headerRef: `{"Authorization":"Bearer x"}`})
	if err == nil {
		t.Fatal("Materialize wrote a header map into the config; ODEC-36 is open and a writer is not a decision")
	}
	if !strings.Contains(err.Error(), "ODEC-36") {
		t.Errorf("refusal names nothing useful: %v", err)
	}
	if strings.Contains(err.Error(), "Bearer") {
		t.Errorf("the refusal echoed a stored value: %v", err)
	}
}

func TestMaterializeKeepsTheStoresAnswerForABrokenReference(t *testing.T) {
	doc := compile.Document{"outbounds": []any{map[string]any{"id": "keystore://server/ZZZZ/user-id"}}}
	_, err := Materialize(doc, fakeStore{})
	if err == nil {
		t.Fatal("a reference the grammar rejects was accepted")
	}
	if !strings.Contains(err.Error(), "outbounds[0].id") {
		t.Errorf("the refusal does not say which position failed: %v", err)
	}
}

func TestMaterializeReportsAMissingSecret(t *testing.T) {
	_, err := Materialize(serverDoc(), fakeStore{})
	if err == nil {
		t.Fatal("a handle with nothing behind it materialized")
	}
	if !strings.Contains(err.Error(), "outbounds[0].settings.vnext[0].users[0].id") {
		t.Errorf("the refusal does not say which position failed: %v", err)
	}
	if strings.Contains(err.Error(), userID) {
		t.Errorf("the refusal echoed the credential it could not read: %v", err)
	}
}

func TestMaterializeRefusesNoResolver(t *testing.T) {
	if _, err := Materialize(serverDoc(), nil); err == nil {
		t.Fatal("Materialize ran with no resolver, which is a config of handle strings by accident")
	}
}
