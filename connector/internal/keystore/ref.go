package keystore

import (
	"regexp"
	"strings"

	"github.com/k2dms/nexora/connector/internal/contract"
)

// The two floors, projected from the envelope-key registry rather than copied (doc 32 §5: one vocabulary, five
// consumers). `floor` is what a schema property accepts; `shape` is what a concrete reference must be, and the
// store only ever works with the second — a string that passes the floor but not the shape names an entity, an
// identifier or a field the closed vocabulary does not hold.
var (
	floor = regexp.MustCompile(contract.SecretReferencePattern)
	shape = regexp.MustCompile(contract.SecretReferenceShape)
)

// Ref is a parsed `keystore://<entity>/<identifier>/<field>` handle.
type Ref struct {
	Entity string
	ID     string
	Field  string
}

// Parse turns the text a document carries into the three segments the store needs. A reference is minted by the
// Connector and never composed by a caller (registry `mintedBy`), so a string this refuses is either a value in
// the wrong place or a document that was edited by hand; the two answer with different codes because doc 32 §11
// says they send the user to different places.
func Parse(text string) (Ref, error) {
	if !strings.HasPrefix(text, contract.SecretScheme) {
		// No Ref on this one: the refused string is a candidate secret, and ADR-0011 rule 6 keeps values out of
		// every log line, including the error that explains why one was not read.
		return Ref{}, &Error{Op: "parse", Cause: ErrValueAtBoundary}
	}
	m := shape.FindStringSubmatch(text)
	if m == nil {
		// Echoing the text is safe only once the scheme prefix is proven, and even then the floor is what bounds
		// it: a value-shaped tail after `keystore://` is a malformed reference, not a credential.
		if !floor.MatchString(text) {
			return Ref{}, &Error{Op: "parse", Cause: ErrMalformedReference}
		}
		return Ref{}, &Error{Op: "parse", Ref: text, Cause: ErrMalformedReference}
	}
	// The registry's shape captures the entity and the field and leaves the identifier as a bare character class,
	// so the middle segment is taken from the text the whole regex just accepted rather than from a group that
	// does not exist.
	entity, field := m[1], m[2]
	id := strings.TrimSuffix(strings.TrimPrefix(text, contract.SecretScheme+entity+"/"), "/"+field)
	return Ref{Entity: entity, ID: id, Field: field}, nil
}

func (r Ref) String() string {
	return contract.SecretScheme + r.Entity + "/" + r.ID + "/" + r.Field
}

// Shape returns the field's recorded value shape — `string` for one value, `object` for the header map.
func (r Ref) Shape() string {
	f, ok := contract.SecretFieldByName(r.Field)
	if !ok {
		return ""
	}
	return f.ValueShape
}

// IsObject reports the one fact the compiler needs about a reference before it resolves it: whether the secret
// behind it is a map. ODEC-45: the compiler emits the handle as a keyed map; apply materializes it from the store.
func (r Ref) IsObject() bool { return r.Shape() == "object" }

// blobName is the file this reference's protected value lives in. The separator is `_` because the shape's
// grammar admits none: an entity is `[a-z0-9-]`, an identifier `[0-9a-f-]`, a field one of four recorded names,
// so a name built from a parsed reference cannot collide with a different reference — which is what lets the
// directory hold one file per secret with no index to keep in step with it.
func (r Ref) blobName() string {
	return r.Entity + "_" + r.ID + "_" + r.Field + ".blob"
}
