// Package compile is the ConfigCompiler: ADR-0013's "compiled into Xray routing where representable" and the
// seven-stage pipeline of docs/architecture/27-config-compilation-pipeline.md §2. This file is stage 5 — emit — and
// stage 6 — hash, because stage 6 is one call over stage 5's bytes and separating them would only invite a second,
// different hashing rule. The other stages are resolve.go, split.go, ports.go, compile.go and validate.go.
//
// What is deliberately NOT in this package: the keystore read behind a `userIdRef` or `headersRef` (the compiler
// emits the handle; apply materializes it — ODEC-45), the writer that renames the file
// into place (doc 27 §8), and the probe that ends stage 7, which needs a Core to run.
//
// The contract this file implements is doc 27 §3, in its order:
//
//  1. key order is lexicographic at every depth, "emitted by the serializer, not inherited from map iteration" — so
//     this file sorts, and does not hand `encoding/json` a `map[string]any` and hope;
//  2. no clocks, paths or PIDs reach the bytes (structural here: the compiler builds every key it writes, so a
//     time-shaped field cannot arrive by accident — the suffix stripper in `packages/schema/scripts/verify.mjs` is a
//     fixture-comparison device and this package has no use for it, see doc 27 §3.2);
//  3. UTF-8, LF, no trailing whitespace, and arrays keep input order where order is meaningful — `serverIds` for
//     `selection.mode: ordered` (docs/architecture/23-unified-server-model.md §8) is the case the contract names;
//  4. numeric formatting is fixed: integers are integers, and a float is refused rather than formatted, because
//     doc 27 §3.4 forbids exponent form and `1.0` while defining no decimal rule, and the pipeline's own emitted
//     shapes (docs/architecture/23-unified-server-model.md §9) contain no non-integer number to invent one for.
package compile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"unicode/utf8"
)

// HashInputNote records ODEC-36's answer: configHash is the handle-bearing document's digest, not the materialized
// file's bytes. Emit and Hash are two calls so the emitter is not the thing that decided which bytes to hash.
const HashInputNote = "ODEC-36: configHash covers the handle-bearing document"

// maxDepth bounds the recursion. Xray's own shape (docs/architecture/23-unified-server-model.md §9) nests about five
// levels deep, so the limit is not a schema rule — it is the guard against a cyclic document, which a Go map can hold
// and a recursive writer cannot notice. A document that reaches it is a bug in the caller, said as loudly as a stack
// overflow would have been.
const maxDepth = 32

// Document is what the pipeline emits. A map, not a struct, because Go marshals struct fields in declaration order and
// doc 27 §3.1 makes key order a property of the emitted bytes rather than of the code that wrote them.
type Document = map[string]any

// Emit renders doc as canonical JSON: sorted keys at every depth, arrays in order, no trailing whitespace.
//
// HTML escaping is off, and the contract leaves it open, so it is stated rather than implied: an Xray config carries
// URLs and query strings, and a bare ampersand keeps them readable for the same byte count. Nothing outside this
// process recomputes these bytes — configHash is compared only against another hash this Connector produced
// (docs/architecture/25-native-messaging-protocol.md §11's drift check) — so a different escaping rule is not a
// compatibility risk, only a different hash.
func Emit(doc Document) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, doc, "config", 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Hash is the configHash of emitted bytes: lowercase hex sha256, the one pattern every `*Hash` field in the corpus
// uses (doc 27 §3.6, and `packages/schema/protocol/commands/config.apply.json#/result/properties/configHash`).
// It takes bytes and not a document on purpose: the hash is over "the exact bytes", so a caller that hashed a document
// it had re-encoded elsewhere would be hashing something else.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// The types the emitter accepts, and nothing else. An unhandled type is an error naming the key path, because the
// alternative — a fmt.Sprintf fallback — is a byte string no schema describes.
func writeValue(buf *bytes.Buffer, v any, at string, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("compile: %s nests past %d levels; a config that deep is either a cycle or a document the "+
			"compiler should have refused before it reached the emitter", at, maxDepth)
	}
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		return writeString(buf, t, at)
	case int:
		buf.WriteString(strconv.Itoa(t))
	case int64:
		buf.WriteString(strconv.FormatInt(t, 10))
	case float64:
		return fmt.Errorf("compile: %s holds the float %v; doc 27 §3.4 fixes numeric formatting and the emitted shapes "+
			"of doc 23 §9 contain no non-integer number, so the compiler must hand over an int", at, t)
	case []any:
		return writeArray(buf, t, at, depth)
	case []string:
		return writeStrings(buf, t, at, depth)
	case map[string]any:
		return writeObject(buf, t, at, depth)
	default:
		return fmt.Errorf("compile: %s carries a %T, which the emitter has no canonical form for", at, t)
	}
	return nil
}

// writeObject sorts the keys — the byte order Go's own encoder would not have given, because map iteration is
// randomised by design and an unsorted encoder changes the hash on every call (doc 27 §3.1).
func writeObject(buf *bytes.Buffer, obj map[string]any, at string, depth int) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeString(buf, k, at); err != nil {
			return err
		}
		buf.WriteByte(':')
		if err := writeValue(buf, obj[k], at+"."+k, depth+1); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

func writeArray(buf *bytes.Buffer, arr []any, at string, depth int) error {
	buf.WriteByte('[')
	for i, elem := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		// Never reordered: doc 27 §3.5 says arrays keep input order where order is meaningful, and a writer cannot
		// know which of the caller's arrays that is, so it treats all of them as meaningful.
		if err := writeValue(buf, elem, elemPath(at, i), depth+1); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

func writeStrings(buf *bytes.Buffer, arr []string, at string, depth int) error {
	buf.WriteByte('[')
	for i, elem := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeString(buf, elem, elemPath(at, i)); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

// writeString is the escaping rule, written here rather than borrowed from encoding/json so that HTML escaping stays
// off (see Emit) and so that invalid UTF-8 is refused instead of silently replaced — a replacement character would
// reach the hash, and two machines would then disagree about a config neither of them had.
func writeString(buf *bytes.Buffer, s string, at string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("compile: %s is not valid UTF-8, and doc 27 §3.5 makes UTF-8 part of the determinism "+
			"contract rather than a terminal setting", at)
	}
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c < 0x20:
			if esc, ok := controlEscapes[c]; ok {
				buf.WriteString(esc)
			} else {
				fmt.Fprintf(buf, "\\u%04x", c)
			}
			i++
		case c == '"' || c == '\\':
			buf.WriteByte('\\')
			buf.WriteByte(c)
			i++
		case c < utf8.RuneSelf:
			buf.WriteByte(c)
			i++
		default:
			// Validity was checked above, so the multi-byte sequence can be copied whole.
			_, width := utf8.DecodeRuneInString(s[i:])
			buf.WriteString(s[i : i+width])
			i += width
		}
	}
	buf.WriteByte('"')
	return nil
}

func elemPath(at string, i int) string {
	return fmt.Sprintf("%s[%d]", at, i)
}

var controlEscapes = map[byte]string{
	'\b': `\b`,
	'\f': `\f`,
	'\n': `\n`,
	'\r': `\r`,
	'\t': `\t`,
}
