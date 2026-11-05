package keystore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/contractgen"
)

// The store's grammar is the projected one, and the projection is the generator's reading of two files. These
// tests hold both ends: the consts against the data they were projected from, and the grammar against every
// reference the corpus actually carries. The second is doc 32 §5's assertion seen from the side that has to
// resolve a reference rather than validate a document — if a fixture spells something this package cannot parse,
// the compiler would emit it and apply would fail at the machine, which is the worst place to discover a
// vocabulary disagreement.
func TestProjectedVocabularyMatchesItsSources(t *testing.T) {
	root := repoRoot(t)

	var keys struct {
		Properties struct {
			SecretReferences struct {
				Properties struct {
					Scheme struct {
						Const string `json:"const"`
					} `json:"scheme"`
					Pattern struct {
						Const string `json:"const"`
					} `json:"pattern"`
					Shape struct {
						Const string `json:"const"`
					} `json:"shape"`
					Fingerprint struct {
						Properties struct {
							Pattern struct {
								Const string `json:"const"`
							} `json:"pattern"`
						} `json:"properties"`
					} `json:"fingerprint"`
				} `json:"properties"`
			} `json:"secretReferences"`
		} `json:"properties"`
	}
	readInto(t, filepath.Join(root, "packages", "schema", "envelope-keys.schema.json"), &keys)
	sr := keys.Properties.SecretReferences.Properties
	for _, pair := range []struct{ what, data, projected string }{
		{"scheme", sr.Scheme.Const, contract.SecretScheme},
		{"pattern", sr.Pattern.Const, contract.SecretReferencePattern},
		{"shape", sr.Shape.Const, contract.SecretReferenceShape},
		{"fingerprint", sr.Fingerprint.Properties.Pattern.Const, contract.SecretFingerprintPattern},
	} {
		if pair.data == "" {
			t.Fatalf("%s: envelope-keys.schema.json has no const for it", pair.what)
		}
		if pair.data != pair.projected {
			t.Errorf("the projected %s is %q while the registry const is %q", pair.what, pair.projected, pair.data)
		}
	}

	var ingest struct {
		Args struct {
			Properties struct {
				Credentials struct {
					Items struct {
						Properties struct {
							Entity struct {
								Enum []string `json:"enum"`
							} `json:"entity"`
							Field struct {
								Enum []string `json:"enum"`
							} `json:"field"`
							Value struct {
								MaxLength     int `json:"maxLength"`
								MaxProperties int `json:"maxProperties"`
							} `json:"value"`
						} `json:"properties"`
					} `json:"items"`
					MaxItems int `json:"maxItems"`
				} `json:"credentials"`
			} `json:"properties"`
		} `json:"args"`
	}
	readInto(t, filepath.Join(root, "packages", "schema", "protocol", "commands", "profile.import.json"), &ingest)
	cred := ingest.Args.Properties.Credentials
	if !sameStrings(cred.Items.Properties.Entity.Enum, contract.SecretEntities) {
		t.Errorf("profile.import accepts entities %v, the projection carries %v", cred.Items.Properties.Entity.Enum, contract.SecretEntities)
	}
	var projectedFields []string
	for _, f := range contract.SecretFields {
		projectedFields = append(projectedFields, f.Name)
	}
	if !sameStrings(cred.Items.Properties.Field.Enum, projectedFields) {
		t.Errorf("profile.import accepts fields %v, the projection carries %v", cred.Items.Properties.Field.Enum, projectedFields)
	}
	if cred.Items.Properties.Value.MaxLength != contract.KeystoreValueMaxChars ||
		cred.Items.Properties.Value.MaxProperties != contract.KeystoreValueMaxProperties ||
		cred.MaxItems != contract.KeystoreMaxCredentials {
		t.Errorf("the ingest bounds moved: schema says %d/%d/%d, the projection says %d/%d/%d",
			cred.Items.Properties.Value.MaxLength, cred.Items.Properties.Value.MaxProperties, cred.MaxItems,
			contract.KeystoreValueMaxChars, contract.KeystoreValueMaxProperties, contract.KeystoreMaxCredentials)
	}
}

func TestEveryReferenceInTheCorpusParses(t *testing.T) {
	root := repoRoot(t)
	var checked, skipped int
	regexy := regexp.MustCompile(`[\^\$\[\]\{\}\(\)\*\+\?\\|]`)
	err := filepath.WalkDir(filepath.Join(root, "packages", "schema"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		// examples/invalid/ exists to be refused; a broken reference in it is the point of the file, not a
		// vocabulary disagreement, so this check walks the corpus that is supposed to resolve.
		rel, _ := filepath.Rel(root, path)
		if strings.Contains(filepath.ToSlash(rel), "examples/invalid/") {
			skipped++
			return nil
		}
		var doc any
		readInto(t, path, &doc)
		for _, s := range stringsWithScheme(doc) {
			if regexy.MatchString(s) || s == contract.SecretScheme {
				continue
			}
			if _, err := Parse(s); err != nil {
				t.Errorf("%s carries %q, which this package cannot parse: %v", rel, s, err)
			}
			checked++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// A floor on the count, not a pinned one: this is the check that the corpus still has references for the
	// store to resolve, and it goes red if the walk silently stops finding any.
	if checked < 10 {
		t.Errorf("only %d references were checked across the schema corpus (%d files skipped); the walk is not finding what it claims to", checked, skipped)
	}
}

func stringsWithScheme(v any) []string {
	switch node := v.(type) {
	case string:
		if strings.HasPrefix(node, contract.SecretScheme) {
			return []string{node}
		}
	case map[string]any:
		var out []string
		for _, child := range node {
			out = append(out, stringsWithScheme(child)...)
		}
		return out
	case []any:
		var out []string
		for _, child := range node {
			out = append(out, stringsWithScheme(child)...)
		}
		return out
	}
	return nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := contractgen.FindRepoRoot(".")
	if err != nil {
		t.Fatalf("no schema directory above this test: %v", err)
	}
	return root
}

func readInto(t *testing.T, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}
