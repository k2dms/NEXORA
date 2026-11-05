package compile

import (
	"strings"
	"testing"
)

// The identifiers this file invents. A VLESS user id is the credential the keystore holds, so
// `tools/check-no-secrets.mjs` treats its *shape* as a secret in every path that is not a fixture directory — and a
// Go test file is not one of them. None of these is a credential: no server, profile or routing policy in any
// deployment answers to them, and they exist so a test can check the one property doc 23 §6 makes of an id, namely
// that the first eight hex characters of it become the outbound tag.
const (
	idA       = "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01" // nexora-secret-allow: synthetic test identity, nothing behind it
	idB       = "8b1d4e70-2a3c-4f51-8b62-7e9d0c1a3f55" // nexora-secret-allow: synthetic test identity, nothing behind it
	idC       = "c7e5a2d1-9f30-4b12-a4e6-1c8d3b5f7a90" // nexora-secret-allow: synthetic test identity, nothing behind it
	idTwin    = "3f2a1c9e-9999-4000-8000-000000000001" // nexora-secret-allow: synthetic, shares idA's prefix on purpose
	idUpper   = "ABCDEF01-2A3C-4F51-8B62-7E9D0C1A3F55" // nexora-secret-allow: synthetic, exists to be uppercase
	idZero    = "00000000-0000-4000-8000-000000000000" // nexora-secret-allow: synthetic, all-zero is not a live id
	idPolicy  = "f1e2d3c4-b5a6-4978-8b9c-0d1e2f3a4b55" // nexora-secret-allow: a policy id, not a credential in this model
	idPolicy2 = "00000000-0000-4000-8000-000000000001" // nexora-secret-allow: a policy id, not a credential in this model
)

func server(id string) Server {
	return Server{ID: id, Protocol: "vless", Address: "a.example.net", Port: 443,
		UserIDRef: "keystore://server/" + id[:8] + "/user-id", Network: "tcp", Security: "none"}
}

func profile(id string, enabled bool, serverIDs ...string) Profile {
	on := enabled
	return Profile{ID: id, DisplayName: "p", Kind: "connector", ServerIDs: serverIDs,
		Selection: Selection{Mode: "ordered"}, Enabled: &on}
}

// doc 23 §6: the tag is derived from the id and from nothing else — not a `displayName` (user-editable, appears in no
// config) and not a random string (a re-import would change it).
func TestTheTagDerivesFromTheIDOnly(t *testing.T) {
	if got := ServerTag(idA); got != "srv-3f2a1c9e" {
		t.Errorf("ServerTag(%s) = %s", idA, got)
	}
	if got := ServerTag(idUpper); got != "srv-abcdef01" {
		t.Errorf("an uppercase id derived %q; #/$defs/coreOutbound admits lowercase hex only", got)
	}
	if got := ServerTag("short"); got != "srv-" {
		t.Errorf("an id shorter than the tag prefix derived %q, which Resolve then refuses", got)
	}
}

// doc 27 §4's Reservation row: "two profiles referencing the same server must not allocate two ports for one tag",
// and §4's Persistence row needs the union of the *enabled* profiles — a soft-off profile must not hold a port.
func TestTheSetIsTheUnionOverEnabledProfiles(t *testing.T) {
	servers := []Server{server(idA), server(idB), server(idC)}
	profiles := []Profile{
		profile("p1", true, idA, idB),
		profile("p2", true, idB, idC),
		profile("p3", false, idZero),
	}
	got, err := Resolve(profiles, servers, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Servers) != 3 || len(got.Tags) != 3 {
		t.Fatalf("got %d servers and %d tags, want the three selected once each", len(got.Servers), len(got.Tags))
	}
	// Sorted by tag, because the arrays this feeds are serialized into hashed bytes (doc 27 §3.1).
	want := []string{"srv-3f2a1c9e", "srv-8b1d4e70", "srv-c7e5a2d1"}
	for i, tag := range want {
		if got.Tags[i] != tag {
			t.Errorf("tag #%d = %s, want %s (sorted by tag, not by id or arrival)", i, got.Tags[i], tag)
		}
	}
	// The disabled profile's server is not in the set; its id would have derived srv-00000000.
	for _, tag := range got.Tags {
		if tag == "srv-00000000" {
			t.Error("a soft-off profile still holds a port")
		}
	}
}

// An `enabled` field that is absent means true (`connector-profile.schema.json` gives it that default), so a payload
// that never mentions it must not drop the profile's servers out of the config.
func TestAnAbsentEnabledFieldMeansTheProfileIsOn(t *testing.T) {
	p := profile("p1", false, idA)
	p.Enabled = nil
	got, err := Resolve([]Profile{p}, []Server{server(idA)}, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Tags) != 1 {
		t.Errorf("a profile with no enabled field selected %d servers", len(got.Tags))
	}
}

// doc 27 §2 stage 1: "every `-nexora.server.<id>` exists; dangling ⇒ InvalidProfile". JSON Schema sees one envelope
// key at a time, so this is the check the contract cannot state (doc 28 §9's LIVE-15 row names the same gap).
func TestADanglingReferenceNamesBothIDs(t *testing.T) {
	_, err := Resolve([]Profile{profile("p1", true, idB)}, []Server{server(idA)}, nil)
	if err == nil {
		t.Fatal("a profile naming a server the registry does not have compiled")
	}
	for _, want := range []string{"InvalidProfile", "p1", idB} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// VER-70's claim: a tag collision is a compile error, and it is checked across the whole server set rather than per
// profile, because two UUIDs sharing a prefix would otherwise put two outbounds under one tag in one file.
func TestATagCollisionNamesBothServers(t *testing.T) {
	twin := server(idTwin)
	_, err := Resolve([]Profile{profile("p1", true, idA, twin.ID)}, []Server{server(idA), twin}, nil)
	if err == nil {
		t.Fatal("two servers derived the same tag and both were emitted")
	}
	for _, want := range []string{"tag collision", idA, twin.ID, "srv-3f2a1c9e"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// Every one of these is a document the schema validates and the pipeline still has to refuse, because the mistake is
// between documents or inside one document's own meaning.
func TestTheStageOneRefusals(t *testing.T) {
	misTagged := server(idA)
	misTagged.Tag = "srv-deadbeef"
	shortID := Server{ID: "abc", Protocol: "vless", Address: "a.example.net", Port: 443, UserIDRef: "keystore://server/abc/user-id", Network: "tcp", Security: "none"}

	fixedNoDefault := profile("p1", true, idA, idB)
	fixedNoDefault.Selection.Mode = "fixed"

	fixedAlien := profile("p1", true, idA)
	fixedAlien.Selection.Mode = "fixed"
	fixedAlien.DefaultServerID = idB

	namesPolicy := profile("p1", true, idA)
	namesPolicy.RoutingPolicyID = idPolicy

	policy := policyWith()
	policy.ID = idPolicy2

	cases := []struct {
		name     string
		profiles []Profile
		servers  []Server
		routing  *RoutingPolicy
		want     string
	}{
		{"two servers one id", []Profile{profile("p1", true, idA)}, []Server{server(idA), server(idA)}, nil, "same id"},
		{"legacy kind", []Profile{{ID: "p1", Kind: "legacy", ServerIDs: []string{idA}}}, []Server{server(idA)}, nil, "connector"},
		{"no servers named", []Profile{{ID: "p1", Kind: "connector"}}, []Server{server(idA)}, nil, "minItems 1"},
		{"id too short to derive a tag", []Profile{profile("p1", true, "abc")}, []Server{shortID}, nil, "no `srv-<8 hex>` tag"},
		{"stored tag disagrees with the derived one", []Profile{profile("p1", true, idA)}, []Server{misTagged}, nil, "derived, not stored"},
		{"fixed with no default", []Profile{fixedNoDefault}, []Server{server(idA), server(idB)}, nil, "names no defaultServerId"},
		{"fixed default outside its own profile", []Profile{fixedAlien}, []Server{server(idA), server(idB)}, nil, "not in its own serverIds"},
		{"policy referenced but not supplied", []Profile{namesPolicy}, []Server{server(idA)}, nil, "gave the pipeline none"},
		{"a different policy supplied", []Profile{namesPolicy}, []Server{server(idA)}, policy, "one compiled config carries one routing section"},
	}
	for _, tc := range cases {
		_, err := Resolve(tc.profiles, tc.servers, tc.routing)
		if err == nil {
			t.Errorf("%s: resolved without a refusal", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: refusal does not say %q: %v", tc.name, tc.want, err)
		}
	}
}

// The matching-policy case is the one that must NOT refuse: `profile.fastest.json` names
// `routing.two-plane.json` and the two ids agree.
func TestAnAgreeingPolicyReferenceResolves(t *testing.T) {
	fastest := example[Profile](t, "profile.fastest.json")
	twoPlane := example[RoutingPolicy](t, "routing.two-plane.json")
	servers := []Server{
		example[Server](t, "server.vision-tcp-tls.json"),
		example[Server](t, "server.reality-xhttp.json"),
		example[Server](t, "server.ws-tls.json"),
	}
	got, err := Resolve([]Profile{fastest}, servers, &twoPlane)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Tags) != 3 {
		t.Errorf("got %v, want all three tags of the profile that is on", got.Tags)
	}
}
