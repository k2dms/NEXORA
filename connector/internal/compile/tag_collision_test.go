package compile

import (
	"strings"
	"testing"
)

// The five canonical server documents. They travel as one batch the way a subscription import delivers them:
// whole set, arbitrary order, every document schema-valid on its own — which is why the tag check runs over the
// set and not per profile (VER-70, docs/architecture/23-unified-server-model.md §10).
var canonicalServerExamples = []string{
	"server.plain-tcp.json",
	"server.vision-tcp-tls.json",
	"server.ws-tls.json",
	"server.grpc-tls.json",
	"server.reality-xhttp.json",
}

func canonicalServers(t *testing.T) []Server {
	t.Helper()
	out := make([]Server, 0, len(canonicalServerExamples))
	for _, name := range canonicalServerExamples {
		out = append(out, example[Server](t, name))
	}
	return out
}

// TestTheCanonicalSetCarriesNoCollisionAndStoredTagsAreTheDerivedOnes proves the shipped set compiles as one
// batch and that doc 23 §6's derived-never-stored promise holds on every canonical document: the stored tag, when
// one exists, is exactly what the id derives.
func TestTheCanonicalSetCarriesNoCollisionAndStoredTagsAreTheDerivedOnes(t *testing.T) {
	servers := canonicalServers(t)
	ids := make([]string, 0, len(servers))
	for _, s := range servers {
		ids = append(ids, s.ID)
		if s.Tag != "" && s.Tag != ServerTag(s.ID) {
			t.Errorf("%s is stored with tag %q but derives %q", s.ID, s.Tag, ServerTag(s.ID))
		}
	}
	res, err := Resolve([]Profile{profileWith(t, "profile.fixed.json", ids...)}, servers, nil)
	if err != nil {
		t.Fatalf("the five canonical servers collided in one batch: %v", err)
	}
	if len(res.Servers) != len(ids) {
		t.Fatalf("resolved %d servers, want %d", len(res.Servers), len(ids))
	}
}

// TestACollisionArrivingUnsortedFromAFeedIsRefusedInEitherOrder is the registry that arrives unsorted from a
// subscription import: the twin of the canonical vision server shares its first eight hex characters — the
// collision a generated-uuid feed can genuinely deliver — and the batch arrives in a different order than the
// examples are listed in. The refusal must name both ids and the shared tag whichever way the feed sorted them.
func TestACollisionArrivingUnsortedFromAFeedIsRefusedInEitherOrder(t *testing.T) {
	// The twin is built from the canonical document itself, not from a private fixture: the feed delivers
	// schema-valid documents, and the mistake is between them.
	raw := corpus(t, "server.vision-tcp-tls.json")
	twinJSON := strings.Replace(string(raw), "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01",
		"3f2a1c9e-9999-4000-8000-000000000001", 1)
	if !strings.Contains(twinJSON, "3f2a1c9e-9999") {
		t.Fatal("the canonical vision server id was not found to twin")
	}
	twin, err := Decode[Server]([]byte(twinJSON))
	if err != nil {
		t.Fatalf("the twinned document no longer decodes: %v", err)
	}

	ids := make([]string, 0, len(canonicalServerExamples)+1)
	for _, s := range canonicalServers(t) {
		ids = append(ids, s.ID)
	}
	ids = append(ids, twin.ID)
	profile := profileWith(t, "profile.fixed.json", ids...)

	// Two arrivals of the same feed: canonical order with the twin last, and reversed — the feed's sort is
	// nobody's contract, so the refusal may not depend on it.
	for name, batch := range map[string][]Server{
		"canonical-order-with-twin-last": append(canonicalServers(t), twin),
		"reversed-with-twin-first":       prepend(twin, reverse(canonicalServers(t))),
	} {
		_, err := Resolve([]Profile{profile}, batch, nil)
		if err == nil {
			t.Fatalf("%s: a feed carrying an eight-hex tag twin was accepted", name)
		}
		for _, want := range []string{"tag collision", "3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01", twin.ID, "srv-3f2a1c9e"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the refusal does not name %q: %v", name, want, err)
			}
		}
	}
}

func reverse(s []Server) []Server {
	out := make([]Server, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

func prepend(v Server, s []Server) []Server {
	return append([]Server{v}, s...)
}
