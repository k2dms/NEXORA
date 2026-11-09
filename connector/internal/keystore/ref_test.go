package keystore

import (
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contract"
)

// The grammar these tests exercise is the projected one (contract.SecretReferenceShape), so every case below is
// a claim about the registry's floor strings, not about a pattern this package wrote. The ids are keystore_test.go's
// documented consts; this one is the other shape the guard cares about — a VLESS user id *is* the credential the
// keystore holds, so `tools/check-no-secrets.mjs` reads its uuid form as a secret in any path that is not a fixture
// directory, and a Go `_test.go` sits beside its package and therefore outside that exemption. Nothing here is a
// credential; no server anywhere answers to it.
const valueLike = "3f2a1c9e-0000-4000-8000-000000000000" // nexora-secret-allow: the shape refused, not a credential

func TestParseAcceptsTheRecordedShapes(t *testing.T) {
	for _, tc := range []struct {
		text              string
		entity, id, field string
	}{
		{"keystore://server/3f2a1c9e/user-id", "server", "3f2a1c9e", "user-id"},
		{"keystore://server/c7e5a2d1/headers", "server", "c7e5a2d1", "headers"},
		{"keystore://subscription/2b3c4d5e/url", "subscription", "2b3c4d5e", "url"},
		{"keystore://host/4f1d2c8b/token", "host", "4f1d2c8b", "token"},
		{"keystore://profile/abcdef01/token", "profile", "abcdef01", "token"},
		{"keystore://sync/12345678/token", "sync", "12345678", "token"},
		{"keystore://server/" + userID + "/user-id", "server", userID, "user-id"},
	} {
		r, err := Parse(tc.text)
		if err != nil {
			t.Fatalf("Parse(%q) = %v, want the recorded shape to accept it", tc.text, err)
		}
		if r.Entity != tc.entity || r.ID != tc.id || r.Field != tc.field {
			t.Errorf("Parse(%q) = %+v, want entity=%s id=%s field=%s", tc.text, r, tc.entity, tc.id, tc.field)
		}
		if r.String() != tc.text {
			t.Errorf("round trip of %q printed %q; a reference must survive parse and print unchanged", tc.text, r.String())
		}
	}
}

// The refusals are where the two floors separate, and doc 32 §11 says the difference decides what the user does
// next: a value in a reference position is SecretRedacted, a broken reference is a schema problem, and neither is
// allowed to answer with the code that means "your machine is broken".
func TestParseRefusalsCarryTheRightCode(t *testing.T) {
	for _, tc := range []struct {
		text, wantCode string
	}{
		{valueLike, "SecretRedacted"},
		{"", "SecretRedacted"},
		{"keystore://server/3F2A1C9E/user-id", "SchemaMismatch"},
		{"keystore://server/3f2a1c9e/userId", "SchemaMismatch"},
		{"keystore://server/3f2a1c9e", "SchemaMismatch"},
		{"keystore://server/short/user-id", "SchemaMismatch"},
		{"keystore://server/too-short/user-id", "SchemaMismatch"},
		{"keystore://widget/3f2a1c9e/user-id", "SchemaMismatch"},
		{"keystore://server/3f2a1c9e/../../windows/system32", "SchemaMismatch"},
		{"keystore://server/" + strings.Repeat("a", 64) + "/user-id", "SchemaMismatch"},
	} {
		_, err := Parse(tc.text)
		if err == nil {
			t.Fatalf("Parse(%q) succeeded, want %s", tc.text, tc.wantCode)
		}
		var ke *Error
		if !asKeystoreError(err, &ke) {
			t.Fatalf("Parse(%q) returned %T, want *Error", tc.text, err)
		}
		if got := ke.Code(); got != tc.wantCode {
			t.Errorf("Parse(%q) refused with %s, want %s (%v)", tc.text, got, tc.wantCode, ke)
		}
		if _, ok := ke.Taxonomy(); !ok {
			t.Errorf("Parse(%q) answered %s, which the projected taxonomy does not have", tc.text, ke.Code())
		}
	}
}

// A refused value must not be echoed: the whole point of the reference position is that a plaintext has no
// business in a log line (ADR-0011 rule 6), and the error is the one thing this package cannot assume nobody
// prints.
func TestValueAtBoundaryIsNotEchoed(t *testing.T) {
	const secret = "hunter2-with-a-very-distinctive-shape" // nexora-secret-allow: the value under test is that it is refused
	_, err := Parse(secret)
	if err == nil {
		t.Fatal("Parse accepted a value at a reference position")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the refusal quotes the refused value: %v", err)
	}
}

func TestFieldShapesComeFromTheRegistry(t *testing.T) {
	for _, tc := range []struct{ field, shape string }{
		{"user-id", "string"},
		{"url", "string"},
		{"token", "string"},
		{"headers", "object"},
	} {
		r := Ref{Entity: "server", ID: "3f2a1c9e", Field: tc.field}
		if got := r.Shape(); got != tc.shape {
			t.Errorf("field %q has shape %q in the projection, want %q", tc.field, got, tc.shape)
		}
		if got, want := r.IsObject(), tc.shape == "object"; got != want {
			t.Errorf("IsObject for %q = %v, want %v", tc.field, got, want)
		}
	}
	// The one field whose value is a map is what ODEC-45's headersKeys names; if the registry ever records a second
	// object-shaped field, the compile path's pairing check needs re-reading, so this fails rather than adapts.
	object := 0
	for _, f := range contract.SecretFields {
		if f.ValueShape == "object" {
			object++
		}
	}
	if object != 1 {
		t.Errorf("the vocabulary records %d object-shaped fields, want the one `headers` ODEC-45 is about", object)
	}
}

// The blob name is the only index this store keeps, so two references must not be able to land on one file. The
// separator is a character the reference grammar does not admit anywhere, which is what makes the concatenation
// injective rather than merely unlikely to collide.
func TestBlobNamesAreInjective(t *testing.T) {
	seen := map[string]string{}
	for _, entity := range contract.SecretEntities {
		for _, f := range contract.SecretFields {
			for _, id := range []string{"3f2a1c9e", userID, "deadbeef"} {
				r := Ref{Entity: entity, ID: id, Field: f.Name}
				text := r.String()
				parsed, err := Parse(text)
				if err != nil {
					t.Fatalf("the vocabulary itself does not parse: %q: %v", text, err)
				}
				name := parsed.blobName()
				if stem := strings.TrimSuffix(name, ".blob"); strings.ContainsAny(stem, `/\.`) {
					t.Errorf("blob name %q has a path-meaningful character outside its extension", name)
				}
				if other, dup := seen[name]; dup {
					t.Errorf("%s and %s both name %q", other, text, name)
				}
				seen[name] = text
			}
		}
	}
}

func asKeystoreError(err error, out **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*out = e
	}
	return ok
}
