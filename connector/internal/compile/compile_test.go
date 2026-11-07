package compile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contractgen"
)

// The tests below read `packages/schema/examples/`, not a copy of it in this directory. That is the whole point of
// ADR-0020's golden rule as doc 27 §3 applies it: an example that the contract suite validates with ajv and the
// compiler reads with Go is one artefact tested twice, and a fixture maintained here would be a third thing allowed
// to drift.

// corpus loads one contract example, decoded through the same strict path the Connector will use.
func corpus(t *testing.T, name string) []byte {
	t.Helper()
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("locating the repository root: %v", err)
	}
	path := filepath.Join(root, "packages", "schema", "examples", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return data
}

func example[T any](t *testing.T, name string) T {
	t.Helper()
	value, err := Decode[T](corpus(t, name))
	if err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	return value
}

// settingsAndPolicy is the pair every compile needs: the shipped defaults, and their block range (20900-20999),
// which doc 30 §8 says must stay disjoint from the allocation range (20100-20899).
func shippedInput(t *testing.T, servers []Server, profiles []Profile, policy *RoutingPolicy) Input {
	t.Helper()
	return Input{
		Servers:  servers,
		Profiles: profiles,
		Routing:  policy,
		Settings: example[Settings](t, "settings.default.json"),
		Failure:  example[Failure](t, "failure-policy.default.json"),
	}
}

func profileWith(t *testing.T, name string, serverIDs ...string) Profile {
	t.Helper()
	p := example[Profile](t, name)
	p.ServerIDs = serverIDs
	return p
}

// shapeABytes is doc 23 §9's "A. VLESS + TCP + TLS + Vision" as this compiler emits it: keys sorted, the `log` and
// `policy` sections doc 36 §9 requires in every config, the handle at `vnext[0].users[0].id` rather than the secret (doc 22 §9:
// every layer above the keystore carries a handle), and 20100 instead of the illustration's 20101 — the first free
// port of the shipped range, which is what an allocator with no memory hands out.
const shapeABytes = `{"inbounds":[{"listen":"127.0.0.1","port":20100,"protocol":"socks","settings":{"auth":"noauth","udp":true},"tag":"in-srv-3f2a1c9e"}],` +
	`"log":{"access":"none","dnsLog":false,"loglevel":"info"},` +
	`"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"se1.example.net","port":443,"users":[{"encryption":"none","flow":"xtls-rprx-vision","id":"keystore://server/3f2a1c9e/user-id"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"alpn":["h2","http/1.1"],"fingerprint":"chrome","serverName":"se1.example.net"}},"tag":"srv-3f2a1c9e"}],` +
	`"policy":{"levels":{}},` +
	`"routing":{"domainStrategy":"AsIs","rules":[{"inboundTag":["in-srv-3f2a1c9e"],"outboundTag":"srv-3f2a1c9e"}]}}`

func TestShapeACompilesFromTheContractExample(t *testing.T) {
	server := example[Server](t, "server.vision-tcp-tls.json")
	profile := profileWith(t, "profile.fixed.json", server.ID)
	result, err := Compile(shippedInput(t, []Server{server}, []Profile{profile}, nil))
	if err != nil {
		t.Fatalf("Compile(shape A): %v", err)
	}
	if got := string(result.Bytes); got != shapeABytes {
		t.Errorf("shape A is not what doc 23 §9 requires\n got: %s\nwant: %s", got, shapeABytes)
	}
	if result.Hash != Hash(result.Bytes) {
		t.Error("Result.Hash is not the hash of Result.Bytes")
	}
	if len(result.Hash) != 64 || strings.ToLower(result.Hash) != result.Hash {
		t.Errorf("configHash %q is not lowercase hex sha256", result.Hash)
	}
	if got := result.ServerTags; len(got) != 1 || got[0] != "srv-3f2a1c9e" {
		t.Errorf("serverTags = %v, want the one derived tag", got)
	}
	if got := result.Inbounds; len(got) != 1 || got[0].Tag != "in-srv-3f2a1c9e" || got[0].Port != 20100 || got[0].Listen != "127.0.0.1" {
		t.Errorf("inboundPorts = %+v, want one loopback socks inbound on the allocated port", got)
	}
	if result.Unrepresentable != nil {
		t.Errorf("a config with no routing policy reported %v; the apply result must stay empty", result.Unrepresentable)
	}
	// doc 27 §4's carry-through: stage 4 embeds the block target and the restart budget for the caller, and neither
	// has a position in the Core config file — doc 30 §7 puts the block in chrome.proxy, not in Xray.
	if result.Block.PortRange != (Range{Min: 20900, Max: 20999}) || !result.Block.AssertClosed || result.Block.Scheme != "socks5" {
		t.Errorf("block target = %+v, want the shipped failure policy's socks5 127.0.0.1 20900-20999 with assertClosed default-true", result.Block)
	}
	if result.Restart.Attempts != 3 || len(result.Restart.BackoffSeconds) != 3 || !result.Restart.Notify {
		t.Errorf("restart budget = %+v, want the shipped 3 attempts, [1 5 30], notify", result.Restart)
	}
}

func TestShapeBCompilesAndKeepsTheEmptyFlow(t *testing.T) {
	server := example[Server](t, "server.reality-xhttp.json")
	profile := profileWith(t, "profile.fixed.json", server.ID)
	profile.DefaultServerID = server.ID
	result, err := Compile(shippedInput(t, []Server{server}, []Profile{profile}, nil))
	if err != nil {
		t.Fatalf("Compile(shape B): %v", err)
	}
	// Shape B is the illustration of the `"flow": ""` case: the field is present in the payload, so it is present in
	// the bytes; doc 23 §9's shape C shows the opposite (no key at all), which the pointer in Server.Flow preserves.
	for _, want := range []string{
		`"flow":""`,
		`"network":"xhttp"`,
		`"security":"reality"`,
		`"xhttpSettings":{"mode":"auto","path":"/websocket"}`,
		`"realitySettings":{"fingerprint":"chrome","publicKey":"hv2uW3Yx7Z9aBcDeFgHiJkLmNoPqRsTuVwXyZ012345","serverName":"www.microsoft.com","shortId":"1a2b3c","spiderX":"/"}`,
	} {
		if !bytes.Contains(result.Bytes, []byte(want)) {
			t.Errorf("the emitted config lacks %s\n%s", want, result.Bytes)
		}
	}
}

// doc 23 §9's shape C shows `"headers": {":authorization": "⟨secret: headersRef⟩"}` — an object at a position where
// the model holds one `keystore://` string, whose key names the schema never lists. Refusing is the honest answer
// until ODEC-36 says how an object handle is substituted.
func TestShapeCIsRefusedAtTheHeadersRef(t *testing.T) {
	server := example[Server](t, "server.grpc-tls.json")
	profile := profileWith(t, "profile.ordered.json", server.ID)
	_, err := Compile(shippedInput(t, []Server{server}, []Profile{profile}, nil))
	if err == nil {
		t.Fatal("shape C compiled; the header map behind headersRef has no emitted form yet")
	}
	for _, want := range []string{"headersRef", "ODEC-36"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// A server with no transport security is a shape the schema allows and doc 23 §9 does not illustrate; the emitted
// streamSettings then carries only the two required fields, and no empty security block.
func TestAnUnencryptedServerEmitsNoSecurityBlock(t *testing.T) {
	server := example[Server](t, "server.plain-tcp.json")
	profile := profileWith(t, "profile.fixed.json", server.ID)
	profile.DefaultServerID = server.ID
	result, err := Compile(shippedInput(t, []Server{server}, []Profile{profile}, nil))
	if err != nil {
		t.Fatalf("Compile(plain tcp): %v", err)
	}
	if want := `"streamSettings":{"network":"tcp","security":"none"}`; !bytes.Contains(result.Bytes, []byte(want)) {
		t.Errorf("got %s\nwant it to contain %s", result.Bytes, want)
	}
}

// doc 27 §3.2 makes the clock exclusion structural: `createdAt` and `updatedAt` arrive in the same payload the
// compiler reads and appear in no emitted byte. `installRoot` is §3.3's case — a path that would make the hash
// machine-specific — so this test sets both and looks for neither.
func TestNoClockAndNoPathReachesTheEmittedBytes(t *testing.T) {
	server := example[Server](t, "server.vision-tcp-tls.json")
	profile := profileWith(t, "profile.fixed.json", server.ID)
	in := shippedInput(t, []Server{server}, []Profile{profile}, nil)
	root := "/home/user/.local/share/nexora/core"
	in.Settings.Core.InstallRoot = &root
	in.Settings.Core.InstallChannel = "pinned"

	result, err := Compile(in)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, banned := range []string{"createdAt", "updatedAt", "2026-11-04", root, "installRoot", "displayName", "Stockholm"} {
		if bytes.Contains(result.Bytes, []byte(banned)) {
			t.Errorf("the emitted bytes contain %q; doc 27 §3.2 and §3.3 keep clocks, paths and labels out of them", banned)
		}
	}
	// The profile's own bookkeeping is present in the input, so the test would be worthless if it were not.
	if !strings.Contains(corpusString(t, "profile.fixed.json"), "createdAt") {
		t.Fatal("profile.fixed.json no longer carries a createdAt for this test to strip")
	}
}

func corpusString(t *testing.T, name string) string {
	t.Helper()
	return string(corpus(t, name))
}

// Two compiles of the same input produce the same bytes, and — the half that a single machine cannot show — a
// different *map iteration order* does not: Go randomises ranging, so the only proof is many rounds.
func TestCompilingTwiceProducesTheSameBytesAndHash(t *testing.T) {
	server := example[Server](t, "server.vision-tcp-tls.json")
	ws := example[Server](t, "server.plain-tcp.json")
	profile := profileWith(t, "profile.ordered.json", server.ID, ws.ID)
	in := shippedInput(t, []Server{ws, server}, []Profile{profile}, nil)

	first, err := Compile(in)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// The registry arrives in the other order: stage 1 sorts by derived tag, so the bytes must not move.
	reversed, err := Compile(shippedInput(t, []Server{server, ws}, []Profile{profile}, nil))
	if err != nil {
		t.Fatalf("Compile(reversed registry): %v", err)
	}
	if !bytes.Equal(first.Bytes, reversed.Bytes) {
		t.Errorf("registry order leaked into the compiled bytes\n      first: %s\nreversed: %s", first.Bytes, reversed.Bytes)
	}
	if first.Hash != reversed.Hash {
		t.Errorf("two orders of one registry produced %s and %s", first.Hash, reversed.Hash)
	}
	for i := 0; i < 50; i++ {
		again, err := Compile(in)
		if err != nil {
			t.Fatalf("Compile round %d: %v", i, err)
		}
		if !bytes.Equal(again.Bytes, first.Bytes) {
			t.Fatalf("round %d differs; the compiler emitted a map it did not sort", i)
		}
	}
}

// doc 36 §4: "The NEXORA vocabulary is 'warn' and Xray's is 'warning'. The Compiler maps; it does not pass through."
// An unmapped level would produce a config invalid at the one field nothing else reads.
func TestTheLogLevelIsMappedNotPassedThrough(t *testing.T) {
	server := example[Server](t, "server.vision-tcp-tls.json")
	profile := profileWith(t, "profile.fixed.json", server.ID)
	for _, tc := range []struct{ in, want string }{{"warn", "warning"}, {"info", "info"}, {"error", "error"}, {"debug", "debug"}} {
		in := shippedInput(t, []Server{server}, []Profile{profile}, nil)
		in.Settings.Telemetry.LogLevel = tc.in
		result, err := Compile(in)
		if err != nil {
			t.Fatalf("Compile(logLevel %s): %v", tc.in, err)
		}
		want := `"loglevel":"` + tc.want + `"`
		if !bytes.Contains(result.Bytes, []byte(want)) {
			t.Errorf("logLevel %q emitted %s; want %s", tc.in, result.Bytes, want)
		}
	}
	in := shippedInput(t, []Server{server}, []Profile{profile}, nil)
	in.Settings.Telemetry.LogLevel = "trace"
	if _, err := Compile(in); err == nil {
		t.Error("a level outside observability.json#/levels/names compiled; doc 36 §4 says there is no trace level")
	} else if !strings.Contains(err.Error(), "maps to Xray") {
		t.Errorf("the refusal does not name the mapping rule: %v", err)
	}
}

// coreLevels is a copy of contract data, and a copy is a second answer unless something checks it.
func TestTheLevelTableEqualsTheContractFile(t *testing.T) {
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Levels struct {
			Names  []string          `json:"names"`
			ToCore map[string]string `json:"toCore"`
		} `json:"levels"`
	}
	raw, err := os.ReadFile(filepath.Join(root, "packages", "schema", "observability.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Levels.ToCore) != len(coreLevels) {
		t.Fatalf("observability.json#/levels/toCore has %d entries, coreLevels has %d", len(doc.Levels.ToCore), len(coreLevels))
	}
	for name, mapped := range doc.Levels.ToCore {
		if coreLevels[name] != mapped {
			t.Errorf("the Go table maps %q to %q, the contract to %q", name, coreLevels[name], mapped)
		}
	}
	if len(doc.Levels.Names) != len(coreLevels) {
		t.Errorf("the vocabulary has %d names and the table has %d keys", len(doc.Levels.Names), len(coreLevels))
	}
}

// The 64 is not this package's number: doc 27 §4 takes it from the two `maxItems` in the command schemas.
func TestTheInboundCeilingEqualsTheCommandSchemasMaxItems(t *testing.T) {
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"core.start", "config.apply"} {
		var cmd struct {
			Result struct {
				Properties struct {
					InboundPorts struct {
						MaxItems int `json:"maxItems"`
					} `json:"inboundPorts"`
				} `json:"properties"`
			} `json:"result"`
		}
		raw, err := os.ReadFile(filepath.Join(root, "packages", "schema", "protocol", "commands", name+".json"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := json.Unmarshal(raw, &cmd); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cmd.Result.Properties.InboundPorts.MaxItems != maxInbounds {
			t.Errorf("%s declares inboundPorts maxItems %d, the allocator uses %d", name, cmd.Result.Properties.InboundPorts.MaxItems, maxInbounds)
		}
	}
}

// ADR-0006's enforcement point: the emitted document is inspected before Core sees it.
func TestANonLoopbackBindCannotSurviveValidation(t *testing.T) {
	doc := Document{"inbounds": []any{Document{"listen": "0.0.0.0", "port": 20100, "tag": "in-srv-00000000"}}}
	err := validate(doc, []Inbound{{Listen: "0.0.0.0", Port: 20100, Tag: "in-srv-00000000"}})
	if err == nil {
		t.Fatal("an inbound on 0.0.0.0 passed stage 7")
	}
	if !strings.Contains(err.Error(), "ADR-0006") {
		t.Errorf("the refusal does not name the ADR it enforces: %v", err)
	}
	if err := validate(Document{"inbounds": []any{}}, []Inbound{}); err == nil {
		t.Error("a config with no inbound compiled; nothing could reach its outbounds")
	}
	// The two lists are built by different functions, and stage 7 is where their agreement is caught.
	if err := validate(doc2inboundDoc(), []Inbound{}); err == nil {
		t.Error("the document and the result list disagreed and no one noticed")
	}
}

func doc2inboundDoc() Document {
	return Document{"inbounds": []any{Document{"listen": loopbackHost, "port": 20100}}}
}
