// Package apply is the half of doc 27 §2 that the compiler refuses to be: the keystore read behind a handle and
// the file Core actually loads. docs/architecture/32-secrets-keystore-masking.md §13 calls that file the one place
// a secret value is plaintext on disk, and docs/architecture/27-config-compilation-pipeline.md §8 gives the write
// its shape — temp file, fsync, rename, and a failure that leaves the previous config applied.
//
// Two rules decide the whole package:
//
//  1. a handle is replaced, never interpreted. The positions a document carries as `keystore://…` are the only
//     places a value may appear (`docs/architecture/22-component-contracts.md` §9), so this file walks for the
//     grammar rather than for key names — a renamed field cannot smuggle a handle past the substitution;
//  2. the document handed in is not the document written. compile.Emit runs twice over two documents and the
//     handle-bearing hash keeps its own bytes: ODEC-36 makes that document, not the materialized file, the configHash subject.
package apply

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/k2dms/nexora/connector/internal/compile"
	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/keystore"
)

// Resolver is the store's read half, stated here rather than imported as a concrete type so that the substitution
// is testable on a runner with no DPAPI — the same reason keystore.Protector exists.
type Resolver interface {
	Resolve(ref string) (keystore.Secret, error)
}

// maxDepth is compile's bound (emit.go's maxDepth, doc 27 §3's cycle guard) restated as this package's own, because
// a walk that is deeper than the writer that produced it is a bug in whoever called it first.
const maxDepth = 32

// Materialize returns a copy of doc in which every string the reference grammar accepts has been replaced by the
// value behind it. The input is never modified: the handle-bearing document is what the goldens, the drift check
// and ODEC-36's other half all need to stay intact.
func Materialize(doc compile.Document, r Resolver) (compile.Document, error) {
	if r == nil {
		return nil, errors.New("apply: Materialize was given no resolver; a config written without reading the keystore is a config of handle strings")
	}
	out, err := substitute(doc, "config", r, 0)
	if err != nil {
		return nil, err
	}
	return out.(compile.Document), nil
}

func substitute(v any, at string, r Resolver, depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("apply: %s nests past %d levels, which compile.Emit would have refused to write", at, maxDepth)
	}
	switch node := v.(type) {
	case map[string]any:
		if ref, ok := objectHandle(node); ok {
			return resolveHeaders(node, ref, at, r)
		}
		out := make(map[string]any, len(node))
		for key, child := range node {
			replaced, err := substitute(child, at+"."+key, r, depth+1)
			if err != nil {
				return nil, err
			}
			out[key] = replaced
		}
		return out, nil
	case []any:
		out := make([]any, len(node))
		for i, child := range node {
			replaced, err := substitute(child, fmt.Sprintf("%s[%d]", at, i), r, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = replaced
		}
		return out, nil
	case string:
		if !strings.HasPrefix(node, contract.SecretScheme) {
			return node, nil
		}
		return resolve(node, at, r)
	default:
		return v, nil
	}
}

// resolve is where a scalar handle stops being a handle. Object handles are resolved as keyed maps above.
func resolve(ref, at string, r Resolver) (any, error) {
	parsed, err := keystore.Parse(ref)
	if err != nil {
		return nil, fmt.Errorf("apply: %s holds %w", at, err)
	}
	if parsed.IsObject() {
		return nil, fmt.Errorf("apply: %s holds %s as a bare value; an object reference must appear as a keyed map whose values all name that handle", at, parsed)
	}
	secret, err := r.Resolve(ref)
	if err != nil {
		return nil, fmt.Errorf("apply: %s could not read %s: %w", at, parsed, err)
	}
	value, err := secret.Text()
	if err != nil {
		return nil, fmt.Errorf("apply: %s could not read %s as a string: %w", at, parsed, err)
	}
	return value, nil
}

func objectHandle(node map[string]any) (string, bool) {
	var ref string
	for _, child := range node {
		value, ok := child.(string)
		if !ok {
			return "", false
		}
		if ref == "" {
			parsed, err := keystore.Parse(value)
			if err != nil || !parsed.IsObject() {
				return "", false
			}
			ref = value
		} else if value != ref {
			return "", false
		}
	}
	return ref, ref != ""
}

func resolveHeaders(node map[string]any, ref, at string, r Resolver) (map[string]any, error) {
	secret, err := r.Resolve(ref)
	if err != nil {
		return nil, fmt.Errorf("apply: %s could not read %s: %w", at, ref, err)
	}
	stored, err := secret.Headers()
	if err != nil {
		return nil, fmt.Errorf("apply: %s could not read %s as a header map: %w", at, ref, err)
	}
	keys := make([]string, 0, len(node))
	for key := range node {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		value, ok := stored[key]
		if !ok {
			return nil, fmt.Errorf("apply: %s names header %q that %s does not hold", at, key, ref)
		}
		out[key] = value
	}
	return out, nil
}
