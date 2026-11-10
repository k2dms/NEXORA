package compile

import (
	"strings"
	"testing"
)

// `additionalProperties: false` is stated by every schema in `packages/schema/`, and doc 23 §10 marks it GREEN —
// `packages/schema/examples/invalid/server.reality-with-privatekey.json` is the corpus's own example of an invented
// field. Decode is where that contract has a Go counterpart: a struct that silently ignored the extra name would put
// the Connector and the extension on either side of a field neither document declares.
func TestDecodeRefusesAFieldTheSchemaDoesNotDeclare(t *testing.T) {
	_, err := Decode[Server]([]byte(`{"id":"` + idA + `","displayName":"x","protocol":"vless","address":"a.example.net",` +
		`"port":443,"network":"tcp","security":"none","userIdRef":"keystore://server/3f2a1c9e/user-id",` +
		`"privateKey":"leak"}`))
	if err == nil {
		t.Fatal("a reality privateKey decoded into a Server; the schema has no such field")
	}
	for _, want := range []string{"compile:", "privateKey", "schema"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// The other side of the same rule: a name the schema DOES declare has to be in the mirror, or the strict decoder
// refuses the very payload the contract allows. `core.egressProbe.endpoint` is ODEC-42's home for the checkip URL, so
// a settings document that carries it is the document `server.test` reads its endpoint from — and the expected answer
// is not a field of it, because the decision put that claim in the server document's own address.
func TestTheSettingsMirrorCarriesTheEgressProbeEndpoint(t *testing.T) {
	with, err := Decode[Settings]([]byte(`{"core":{"egressProbe":{"endpoint":"https://ipinfo.io/ip"}}}`))
	if err != nil {
		t.Fatalf("settings carrying the field the schema declares: %v", err)
	}
	if with.Core.EgressProbe == nil || with.Core.EgressProbe.Endpoint != "https://ipinfo.io/ip" {
		t.Errorf("core.egressProbe decoded as %+v", with.Core.EgressProbe)
	}
	if body, err := Decode[Settings]([]byte(`{"core":{"egressProbe":null}}`)); err != nil {
		t.Fatalf("the schema's own default of null: %v", err)
	} else if body.Core.EgressProbe != nil {
		t.Errorf("a null egress probe decoded as %+v, want absent: no endpoint is not the empty endpoint", *body.Core.EgressProbe)
	}
	if _, err := Decode[Settings]([]byte(`{"core":{"egressProbe":{"expectedAddress":"203.0.113.7"}}}`)); err == nil {
		t.Error("an egress probe carrying an expected address decoded; the schema declares only `endpoint`")
	}
}

// The three canonical servers differ in exactly the fields the compiler has to tell apart: `flow` present with a
// value, `flow` present and empty, and `flow` absent. A plain string would collapse the last two.
func TestTheFlowFieldKeepsAbsentAndEmptyApart(t *testing.T) {
	vision := example[Server](t, "server.vision-tcp-tls.json")
	if vision.Flow == nil || *vision.Flow != "xtls-rprx-vision" {
		t.Errorf("vision server's flow = %v", vision.Flow)
	}
	reality := example[Server](t, "server.reality-xhttp.json")
	if reality.Flow == nil || *reality.Flow != "" {
		t.Errorf("reality server's flow = %v, want a stated empty string", reality.Flow)
	}
	grpcServer := example[Server](t, "server.grpc-tls.json")
	if grpcServer.Flow != nil {
		t.Errorf("grpc server's flow = %q, want absent: doc 23 §9 emits no flow key for shape C", *grpcServer.Flow)
	}
}

// The whole shipped example set is read by these tests, so one loop proves the Go structs cover the contract files —
// an example that no longer decodes is a schema change the compiler has not caught up with.
func TestEveryContractExampleDecodesIntoTheModel(t *testing.T) {
	servers := []string{
		"server.vision-tcp-tls.json", "server.reality-xhttp.json", "server.grpc-tls.json",
		"server.plain-tcp.json", "server.ws-tls.json",
	}
	for _, name := range servers {
		s := example[Server](t, name)
		if s.ID == "" || s.Protocol != "vless" || s.UserIDRef == "" {
			t.Errorf("%s decoded into an unusable server: %+v", name, s)
		}
	}
	for _, name := range []string{"profile.fixed.json", "profile.ordered.json", "profile.fastest.json"} {
		p := example[Profile](t, name)
		if p.Kind != "connector" || len(p.ServerIDs) == 0 {
			t.Errorf("%s decoded into %+v", name, p)
		}
	}
	for _, name := range []string{"routing.minimal.json", "routing.two-plane.json"} {
		p := example[RoutingPolicy](t, name)
		if p.CorePlane.DomainStrategy == "" {
			t.Errorf("%s lost its domainStrategy", name)
		}
	}
	settings := example[Settings](t, "settings.default.json")
	if settings.Ports.Range.Min != 20100 || settings.Ports.Range.Max != 20899 || settings.Ports.Host != loopbackHost {
		t.Errorf("settings.default.json ports decoded as %+v", settings.Ports)
	}
	failure := example[Failure](t, "failure-policy.default.json")
	if failure.Block.Range.Min != 20900 || failure.Block.Range.Max != 20999 {
		t.Errorf("the shipped block range decoded as %+v", failure.Block.Range)
	}
	// doc 30 §7: `assertClosed` defaults to true, and the payload states it — the pointer keeps the default readable.
	if failure.Block.AssertClosed == nil || !*failure.Block.AssertClosed {
		t.Error("failure-policy.default.json no longer states assertClosed true, which doc 30 §7 calls a precondition")
	}
}

// The shipped defaults are disjoint (doc 30 §8, verified by `verify.mjs`); decoding them here and pairing them there
// is what keeps ports_test.go's ranges from becoming fixtures of their own.
func TestTheShippedRangesAreTheOnesTheTestsUse(t *testing.T) {
	settings := example[Settings](t, "settings.default.json")
	failure := example[Failure](t, "failure-policy.default.json")
	if got := (Range{Min: settings.Ports.Range.Min, Max: settings.Ports.Range.Max}); got != allocRange {
		t.Errorf("the allocation range under test is %v, the contract says %v", got, allocRange)
	}
	if got := (Range{Min: failure.Block.Range.Min, Max: failure.Block.Range.Max}); got != blockRange {
		t.Errorf("the block range under test is %v, the contract says %v", got, blockRange)
	}
	if allocRange.overlaps(blockRange) {
		t.Error("the shipped ranges overlap; verify.mjs asserts they do not")
	}
}
