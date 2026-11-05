package compile

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contractgen"
)

// -golden.update rewrites a golden from what the compiler emits today. It exists because docs/architecture/39 and
// ADR-0020 make a golden diff the signal of a contract change: the change is reviewed in the diff, and the flag is
// what lets the person making it produce that diff rather than hand-writing bytes. A run without the flag never
// writes.
var updateGoldens = flag.Bool("golden.update", false, "rewrite the golden files from the compiler's current output")

// The shapes doc 27 §3 names as the fixture set, and which of them a file can pin today:
//
//   - shape A (vision over tcp+tls) and shape B (reality over xhttp): pinnable.
//   - one multi-server profile: pinnable, and it is the case where the allocator's output is visible — two inbounds,
//     two outbounds, and the tags pairing them.
//   - shape C (grpc): refused, not pinnable. `server.grpc-tls.json` carries a `headersRef`, and no decision says how a
//     handle for an *object* is materialized — `TestShapeCIsRefusedAtTheHeadersRef` pins the refusal, and ODEC-36
//     owns the answer.
//   - one with core-plane rules: refused by the reports, not pinnable. Neither shipped policy emits a routing rule:
//     `routing.minimal.json`'s only rule names `network`, whose emitted form (array or comma-joined string) the corpus
//     never states, and `routing.two-plane.json`'s two rules name the built-in tags `block` and `direct`, whose
//     emitted outbounds no document shows — `TestTheShippedRoutingExamplesCompileIntoReports` pins both, and ODEC-37
//     owns the spellings.
//   - one fail-closed variant: no such file. docs/architecture/30-failure-policy.md §7 puts the fail-closed proxy in
//     `chrome.proxy`, not in the Xray document, so a fail-closed profile compiles to the same bytes with a different
//     `Block` carry-through, which the golden for shape A already shows.
func TestTheCompiledShapesMatchTheirGoldens(t *testing.T) {
	for _, tc := range []struct {
		file  string
		build func(t *testing.T) []byte
	}{
		{"shape-a-vision-tcp-tls.golden.json", goldenShapeA},
		{"shape-b-reality-xhttp.golden.json", goldenShapeB},
		{"multi-server-ordered.golden.json", goldenMultiServer},
	} {
		got := tc.build(t)
		root, err := contractgen.FindRepoRoot(".")
		if err != nil {
			t.Fatalf("locating the repository root: %v", err)
		}
		// The path the corpus names — `docs/architecture/27-config-compilation-pipeline.md` §3 and
		// `docs/architecture/23-unified-server-model.md` §9 both say the golden files are normative at
		// `connector/testdata/config/`, so they live at the connector's own testdata directory rather than in this
		// package's, and a second path would be a second answer to where a contract fixture is kept.
		dir := filepath.Join(root, "connector", "testdata", "config")
		path := filepath.Join(dir, tc.file)
		want, err := os.ReadFile(path)
		if *updateGoldens {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("%s: %v", tc.file, err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatalf("%s: %v", tc.file, err)
			}
			t.Logf("rewrote %s (%d bytes)", tc.file, len(got))
			continue
		}
		if err != nil {
			t.Fatalf("reading %s: %v (run `go test ./internal/compile -golden.update` to create it)", path, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s is not what the compiler emits\n in golden: %s\n   compiled: %s", tc.file, want, got)
		}
		// doc 27 §3.5 makes the byte form of the document part of the determinism contract, and a golden that quietly
		// gained a trailing newline from an editor would compare equal to nothing.
		if bytes.ContainsAny(want, "\r") || bytes.HasSuffix(want, []byte("\n")) {
			t.Errorf("%s is not LF-clean: a golden holds exactly the emitted bytes", tc.file)
		}
	}
}

func goldenShapeA(t *testing.T) []byte {
	t.Helper()
	server := example[Server](t, "server.vision-tcp-tls.json")
	result, err := Compile(shippedInput(t, []Server{server}, []Profile{profileWith(t, "profile.fixed.json", server.ID)}, nil))
	if err != nil {
		t.Fatalf("Compile(shape A): %v", err)
	}
	return result.Bytes
}

func goldenShapeB(t *testing.T) []byte {
	t.Helper()
	server := example[Server](t, "server.reality-xhttp.json")
	profile := profileWith(t, "profile.fixed.json", server.ID)
	profile.DefaultServerID = server.ID
	result, err := Compile(shippedInput(t, []Server{server}, []Profile{profile}, nil))
	if err != nil {
		t.Fatalf("Compile(shape B): %v", err)
	}
	return result.Bytes
}

func goldenMultiServer(t *testing.T) []byte {
	t.Helper()
	vision := example[Server](t, "server.vision-tcp-tls.json")
	reality := example[Server](t, "server.reality-xhttp.json")
	// The registry arrives in the order a refresh would produce it, not the order the profile names: stage 1 sorts by
	// derived tag, so the golden records the sorted pairing of `in-srv-<8 hex>` to `srv-<8 hex>`.
	profile := profileWith(t, "profile.ordered.json", reality.ID, vision.ID)
	profile.DefaultServerID = reality.ID
	result, err := Compile(shippedInput(t, []Server{vision, reality}, []Profile{profile}, nil))
	if err != nil {
		t.Fatalf("Compile(multi-server): %v", err)
	}
	if len(result.Inbounds) != 2 || len(result.ServerTags) != 2 {
		t.Fatalf("a two-server profile compiled to %d inbounds and %d tags", len(result.Inbounds), len(result.ServerTags))
	}
	return result.Bytes
}
