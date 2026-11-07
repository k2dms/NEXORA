package compile

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// shapeACanonical is docs/architecture/23-unified-server-model.md §9's "A. VLESS + TCP + TLS + Vision" as the emitted
// bytes must be: keys sorted at every depth, arrays in the order the model gives them, no whitespace between tokens.
const shapeACanonical = `{"inbounds":[{"listen":"127.0.0.1","port":20101,"protocol":"socks","settings":{"auth":"noauth","udp":true},"tag":"in-srv-3f2a1c9e"}],"log":{"access":"none","dnsLog":false,"loglevel":"warning"},"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"se1.example.net","port":443,"users":[{"encryption":"none","flow":"xtls-rprx-vision","id":"⟨secret: userIdRef keystore://server/3f2a1c9e/user-id⟩"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"alpn":["h2","http/1.1"],"fingerprint":"chrome","serverName":"se1.example.net"}},"tag":"srv-3f2a1c9e"}],"policy":{"levels":{}},"routing":{"domainStrategy":"AsIs","rules":[{"inboundTag":["in-srv-3f2a1c9e"],"outboundTag":"srv-3f2a1c9e"}]}}`

// Computed independently with Node crypto over the UTF-8 fixture bytes.
const shapeAHash = "7ed985e92cc166af6c70f8f9c7ab2a4e46001222834b909a73e20842d2f68511"

// shapeA is the same document, written with keys in the order doc 23 happens to list them — deliberately not sorted —
// so that a passing comparison is evidence about the encoder rather than about the ordering of this test's literals.
func shapeA() Document {
	return Document{
		"routing": Document{"rules": []any{Document{"outboundTag": "srv-3f2a1c9e", "inboundTag": []string{"in-srv-3f2a1c9e"}}}, "domainStrategy": "AsIs"},
		"policy":  Document{"levels": Document{}},
		"outbounds": []any{
			Document{
				"tag":      "srv-3f2a1c9e",
				"protocol": "vless",
				"settings": Document{
					"vnext": []any{
						Document{
							"port":    443,
							"address": "se1.example.net",
							"users": []any{Document{
								"id":         "⟨secret: userIdRef keystore://server/3f2a1c9e/user-id⟩",
								"encryption": "none",
								"flow":       "xtls-rprx-vision",
							}},
						},
					},
				},
				"streamSettings": Document{
					"network":  "tcp",
					"security": "tls",
					"tlsSettings": Document{
						"fingerprint": "chrome",
						"alpn":        []string{"h2", "http/1.1"},
						"serverName":  "se1.example.net",
					},
				},
			},
		},
		"inbounds": []any{
			Document{
				"settings": Document{"udp": true, "auth": "noauth"},
				"protocol": "socks",
				"port":     20101,
				"tag":      "in-srv-3f2a1c9e",
				"listen":   "127.0.0.1",
			},
		},
		"log": Document{"loglevel": "warning", "dnsLog": false, "access": "none"},
	}
}

// Go randomises map iteration by design, so an encoder that inherited order from ranging would produce a different
// hash on a different run (doc 27 §3.1). Re-ranging is the only way to test that claim without a second Go runtime.
func TestTheEmitterSortsKeysAtEveryDepthNotFromMapIteration(t *testing.T) {
	first, err := Emit(shapeA())
	if err != nil {
		t.Fatalf("Emit(shapeA()): %v", err)
	}
	if got := string(first); got != shapeACanonical {
		t.Errorf("emitted bytes are not the canonical form\n got: %s\nwant: %s", got, shapeACanonical)
	}
	for i := 0; i < 200; i++ {
		again, err := Emit(shapeA())
		if err != nil {
			t.Fatalf("Emit round %d: %v", i, err)
		}
		if !bytes.Equal(again, first) {
			t.Fatalf("round %d emitted different bytes; the key order came from map iteration, not from sorting:\n %s",
				i, again)
		}
	}
}

// encoding/json sorts map keys too, so agreement between two independent encoders is the cheap proof that the order
// this package emits is the order anyone re-marshalling the same document would find. HTML escaping is disabled to
// match, and the trailing newline Encoder.Encode appends is what this package's contract forbids.
func TestTheCanonicalBytesAgreeWithAnIndependentEncoder(t *testing.T) {
	got, err := Emit(shapeA())
	if err != nil {
		t.Fatalf("Emit(shapeA()): %v", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(shapeA()); err != nil {
		t.Fatalf("encoding/json on the same document: %v", err)
	}
	want := strings.TrimSuffix(buf.String(), "\n")
	if string(got) != want {
		t.Errorf("the two encoders disagree about canonical order\n ours: %s\ntheirs: %s", got, want)
	}
}

// Order in an array is semantics, not formatting: serverIds for selection.mode ordered is the case doc 27 §3.5 names,
// and docs/architecture/23-unified-server-model.md §8 gives the array its meaning by its order.
func TestArraysAreEmittedInInputOrderNeverReordered(t *testing.T) {
	got, err := Emit(Document{
		"selection": Document{"mode": "ordered", "serverIds": []string{"srv-b", "srv-a", "srv-c"}},
		"rules":     []any{"z", 1, nil, true, "a"},
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	const want = `{"rules":["z",1,null,true,"a"],"selection":{"mode":"ordered","serverIds":["srv-b","srv-a","srv-c"]}}`
	if string(got) != want {
		t.Errorf("reordering or reshaping an array\n got: %s\nwant: %s", got, want)
	}
}

// doc 27 §3.4: "the port is an integer". A float reaching the emitter means the compiler above it produced a
// non-integer for a field Xray reads as a port, so this is a refusal with the path named, not a formatting choice.
func TestAFloatIsRefusedAndNamesWhereItWasFound(t *testing.T) {
	_, err := Emit(Document{"inbounds": []any{Document{"port": float64(20101)}}})
	if err == nil {
		t.Fatal("a float emitted silently; doc 27 §3.4 fixes numeric formatting and defines no decimal rule")
	}
	for _, want := range []string{"config.inbounds[0].port", "float"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// The alternative to a typed refusal is a fmt.Sprintf fallback, which is a byte string no schema describes.
func TestATypeWithNoCanonicalFormIsRefusedWithItsPath(t *testing.T) {
	_, err := Emit(Document{"outbounds": []any{Document{"settings": Document{"port": uint16(443)}}}})
	if err == nil {
		t.Fatal("a uint16 emitted without a documented canonical form")
	}
	for _, want := range []string{"config.outbounds[0].settings.port", "uint16"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// Invalid UTF-8 would otherwise be replaced, and two machines would then hash a config neither of them had
// (doc 27 §3.5 makes UTF-8 part of the determinism contract).
func TestInvalidUTF8IsRefusedRatherThanReplaced(t *testing.T) {
	_, err := Emit(Document{"log": Document{"access": "\xff not a rune"}})
	if err == nil {
		t.Fatal("invalid UTF-8 reached the emitted bytes")
	}
	if !strings.Contains(err.Error(), "config.log.access") {
		t.Errorf("the refusal does not name the key holding it: %v", err)
	}
}

// U+2028 and U+2029 are left unescaped on purpose: encoding/json escapes them because a JSON-P consumer would
// evaluate them, and Core parses this document as JSON rather than as JavaScript.
func TestEscapingKeepsTheConfigReadableWithoutChangingItsMeaning(t *testing.T) {
	got, err := Emit(Document{
		"url":       "https://example.net/path?a=1&b=2<3>",
		"quoted":    `say "hi"`,
		"backslash": `C:\x`,
		"control":   "\x01\u001f\t\n",
		"label":     "Прокси",
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	const want = `{"backslash":"C:\\x","control":"\u0001\u001f\t\n","label":"Прокси",` +
		`"quoted":"say \"hi\"","url":"https://example.net/path?a=1&b=2<3>"}`
	if string(got) != want {
		t.Errorf(" escaping differs\n got: %s\nwant: %s", got, want)
	}
}

// No raw line break can survive the escaping rule, so the emitted document is one line with nothing around it: doc 27
// §3.5's "UTF-8 with LF" and "no trailing WS" in the direction the file is actually written.
func TestNothingOutsideTheDocumentReachesTheBytes(t *testing.T) {
	got, err := Emit(shapeA())
	if err != nil {
		t.Fatalf("Emit(shapeA()): %v", err)
	}
	if bytes.ContainsAny(got, "\n\r") {
		t.Errorf("a raw line break is in the emitted bytes: %q", got)
	}
	if !bytes.Equal(got, bytes.TrimSpace(got)) {
		t.Errorf("the document is padded with whitespace: %q", got)
	}
}

func TestIntegersAreFormattedAsIntegers(t *testing.T) {
	got, err := Emit(Document{
		"port":  20101,
		"count": 1000000000,
		"big":   int64(9007199254740993),
		"off":   -7,
		"zero":  0,
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	const want = `{"big":9007199254740993,"count":1000000000,"off":-7,"port":20101,"zero":0}`
	if string(got) != want {
		t.Errorf("exponent or decimal form crept in\n got: %s\nwant: %s", got, want)
	}
}

// configHash is compared against `^[0-9a-f]{64}$`, the pattern `packages/schema/protocol/commands/config.apply.json`
// declares and the one every `*Hash` field in the corpus uses, so the emitter's own hash has to satisfy it.
func TestTheHashIsTheCanonicalLowercaseSha256OfTheEmittedBytes(t *testing.T) {
	got, err := Emit(shapeA())
	if err != nil {
		t.Fatalf("Emit(shapeA()): %v", err)
	}
	hashed := Hash(got)
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hashed) {
		t.Errorf("configHash %q is outside the pattern the command schema declares", hashed)
	}
	if hashed != shapeAHash {
		t.Errorf("shape A no longer hashes to the golden: got %s, want %s", hashed, shapeAHash)
	}
	if Hash([]byte(shapeACanonical)) != shapeAHash {
		t.Error("the golden hash is not the hash of the golden bytes — one of the two drifted")
	}

	// One port moved is a different config, and the drift check depends on noticing it.
	doc := shapeA()
	doc["inbounds"].([]any)[0].(Document)["port"] = 20102
	changed, err := Emit(doc)
	if err != nil {
		t.Fatalf("Emit(the moved port): %v", err)
	}
	if Hash(changed) == hashed {
		t.Error("a changed port produced the same configHash")
	}
}

// A Go map can hold a cycle and a recursive writer cannot notice; the limit is the difference between an error and a
// stack overflow inside the process that is supposed to keep Core running.
func TestACyclicDocumentIsRefusedAtTheDepthLimit(t *testing.T) {
	doc := Document{"name": "loop"}
	doc["self"] = doc
	_, err := Emit(doc)
	if err == nil {
		t.Fatal("a self-referential document emitted")
	}
	if !strings.Contains(err.Error(), "nests past") {
		t.Errorf("the cycle was refused for some other reason: %v", err)
	}
}
