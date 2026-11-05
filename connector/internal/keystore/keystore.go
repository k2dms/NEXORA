// Package keystore is the Connector's half of ADR-0011: the one place a `keystore://` reference becomes the
// bytes behind it again.
//
// Everything above this package carries references (docs/architecture/32-secrets-keystore-masking.md §4), and
// the schema floor that makes that enforceable is projected into internal/contract rather than restated here —
// docs/architecture/20-target-repository-layout.md §3 forbids a second copy of any allowlist or entity shape.
// What this package adds is only the two things a document cannot do: read a file and call the OS protector.
//
// It resolves, writes and deletes. It does not mask (doc 32 §8 puts masking at the sink, in the extension and in
// diagnostics.get), does not mint (a reference is minted by the ingest door, doc 32 §6, and the minting rule is
// in the registry), and does not fall back. A failed protector is KeystoreUnavailable and the value stays
// unwritten: ADR-0011 rule 5 forbids the plaintext file that would otherwise make a green run here look like a
// working credential store on a platform this ADR does not bind.
package keystore

import (
	"errors"
	"fmt"

	"github.com/k2dms/nexora/connector/internal/contract"
)

// The four ways a secret can be the reason an operation stopped. doc 32 §11 names three of them and the schema
// floor supplies the fourth; each carries the taxonomy code the dispatcher answers with, so a refusal here cannot
// drift from the code the command documents already promise.
var (
	// ErrNotFound is a reference the store does not have. doc 32 §11 makes it InvalidProfile rather than
	// KeystoreUnavailable on purpose: one sends the user to retype, the other tells them their machine broke.
	ErrNotFound = fmt.Errorf("no secret is stored under that reference")
	// ErrUnavailable is the store itself being unreachable — a failed DPAPI call, the wrong user session, the
	// blob directory gone, or a platform no ADR has bound yet (ADR-0011 rule 5).
	ErrUnavailable = fmt.Errorf("the keystore is unavailable")
	// ErrValueAtBoundary is a value sitting where a reference belongs. It is refused, never stripped: doc 32 §11
	// says stripping is what makes a leak look like success.
	ErrValueAtBoundary = fmt.Errorf("a value sits where a keystore:// reference belongs")
	// ErrMalformedReference is the scheme present and the shape not: a reference that names nothing the
	// vocabulary can hold.
	ErrMalformedReference = fmt.Errorf("the reference does not match the recorded shape")
	// ErrEmptyValue is the store's own floor. The ingest schema bounds a credential's size but declares no
	// minLength, and an empty credential would resolve to something while meaning nothing — which is the one
	// state a user could not distinguish from a store that answered wrongly.
	ErrEmptyValue = fmt.Errorf("an empty secret cannot be stored; it would resolve to a value that means nothing")
)

// codeFor maps each refusal to its error-taxonomy code. The codes are looked up rather than typed at the call
// site so a taxonomy edit that renames one of them fails here at init instead of shipping a refusal no client
// can classify.
func codeFor(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "InvalidProfile"
	case errors.Is(err, ErrUnavailable):
		return "KeystoreUnavailable"
	case errors.Is(err, ErrValueAtBoundary):
		return "SecretRedacted"
	case errors.Is(err, ErrMalformedReference), errors.Is(err, ErrEmptyValue):
		return "SchemaMismatch"
	}
	return "Internal"
}

// Error is a keystore refusal. It carries the reference only where a reference is safe to carry: ADR-0011 rule 6
// says logs and diagnostics hold references and nothing else, so the value-shaped case has no Ref and its
// message never quotes the string it refused.
type Error struct {
	Op  string
	Ref string
	// Cause is one of the sentinel errors above; the code the dispatcher answers with derives from it.
	Cause error
}

func (e *Error) Error() string {
	if e.Ref == "" {
		return fmt.Sprintf("keystore: %s: %s", e.Op, e.Cause)
	}
	return fmt.Sprintf("keystore: %s: %s (%s)", e.Op, e.Cause, e.Ref)
}

func (e *Error) Unwrap() error { return e.Cause }

// Code returns the taxonomy code name for this refusal.
func (e *Error) Code() string { return codeFor(e.Cause) }

// Taxonomy resolves the code against the projected contract so a caller can read its layer and retryability
// without a second table.
func (e *Error) Taxonomy() (contract.Code, bool) { return contract.LookupCode(e.Code()) }

func refuse(op, ref string, cause error) *Error {
	return &Error{Op: op, Ref: ref, Cause: cause}
}

// unavailable is the one constructor a caller uses when the OS itself refused: the reference may be legal or not,
// and either way the answer is KeystoreUnavailable rather than a guess about the document.
func unavailable(op, ref string, cause error) *Error {
	return &Error{Op: op, Ref: ref, Cause: fmt.Errorf("%w: %v", ErrUnavailable, cause)}
}

func init() {
	for _, sentinel := range []error{ErrNotFound, ErrUnavailable, ErrValueAtBoundary, ErrMalformedReference, ErrEmptyValue} {
		code, ok := contract.LookupCode(codeFor(sentinel))
		if !ok {
			panic("keystore: the projected taxonomy has no " + codeFor(sentinel) + " code")
		}
		// doc 32 §11: none of these is a "type it again and retry" answer. A retryable code here would mean the
		// taxonomy changed under this package's assumption.
		if code.Retryable {
			panic("keystore: " + code.Name + " became retryable, and the keystore refusals were written against a table that said otherwise")
		}
	}
}
