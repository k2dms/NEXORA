package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

var goodSet = []string{"xray/xray.exe", "xray/geoip.dat", "xray/geosite/"}

func entries(names ...string) []Entry {
	out := make([]Entry, 0, len(names))
	for _, n := range names {
		out = append(out, Entry{Name: n, Size: 10})
	}
	return out
}

func TestValidateAcceptsAPinShapedSet(t *testing.T) {
	if err := Validate(goodSet, entries("xray/xray.exe", "xray/geoip.dat", "xray/geosite/"), Limits{MaxEntry: 100, MaxTotal: 1000}); err != nil {
		t.Fatalf("a set the pin enumerates must pass: %v", err)
	}
}

func TestValidateRefusesNamesOutsideTheSet(t *testing.T) {
	err := Validate(goodSet, entries("xray/xray.exe", "xray/extra.dll"), Limits{})
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Rule != ErrOutsideExpectedSet.Error() || !strings.Contains(ref.Error(), "xray/extra.dll") {
		t.Fatalf("want a set refusal naming the entry, got %v", err)
	}
}

func TestValidateRefusesZipSlipShapes(t *testing.T) {
	cases := []struct{ name, want string }{
		{"../evil.exe", "climbs out"},
		{"xray/../../evil.exe", "climbs out"},
		{"xray/../evil.exe", "climbs out"},
		{"/etc/passwd", "absolute path"},
		{"~/.evil", "absolute path"},
		{"C:evil.exe", "drive letter"},
		{"xray\\evil.exe", "backslash"},
		{"..\\evil.exe", "backslash"},
		{"./xray/xray.exe", "clean relative path"},
		{"xray//xray.exe", "clean relative path"},
		{"", "no name"},
	}
	for _, c := range cases {
		err := Validate(goodSet, entries(c.name), Limits{})
		var ref *Refusal
		if !errors.As(err, &ref) || !strings.Contains(ref.Rule, c.want) {
			t.Errorf("%q: want refusal naming %q, got %v", c.name, c.want, err)
		}
	}
}

func TestValidateRefusesDirectoryClimbsEvenWithTrailingSlash(t *testing.T) {
	for _, name := range []string{"../", "xray/../", "xray/geosite/../"} {
		err := Validate(goodSet, entries(name), Limits{})
		var ref *Refusal
		if !errors.As(err, &ref) || !strings.Contains(ref.Rule, "climbs out") {
			t.Errorf("%q: want a climb refusal, got %v", name, err)
		}
	}
}

func TestValidateRefusesDuplicates(t *testing.T) {
	err := Validate(goodSet, entries("xray/xray.exe", "xray/xray.exe"), Limits{})
	var ref *Refusal
	if !errors.As(err, &ref) || !strings.Contains(ref.Rule, "twice") {
		t.Fatalf("want a duplicate refusal, got %v", err)
	}
}

func TestValidateRefusesSizeBounds(t *testing.T) {
	entries := []Entry{
		{Name: "xray/xray.exe", Size: 300},
		{Name: "xray/geoip.dat", Size: 900},
	}
	err := Validate(goodSet, entries, Limits{MaxEntry: 200})
	var ref *Refusal
	if !errors.As(err, &ref) || !strings.Contains(ref.Rule, "one member") {
		t.Fatalf("want a per-entry refusal, got %v", err)
	}
	err = Validate(goodSet, entries, Limits{MaxTotal: 1000})
	if !errors.As(err, &ref) || !strings.Contains(ref.Rule, "in total") {
		t.Fatalf("want a total-bound refusal, got %v", err)
	}
}

// The guard reads what a real zip reader reports, not what a caller typed: this test writes a zip with
// attacker-shaped entry names, walks its central directory and hands exactly those declarations to Validate.
func TestValidateOverARealZipCentralDirectory(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{"xray/xray.exe", "../evil.exe", "xray\\evil.exe"} {
		fh := &zip.FileHeader{Name: name}
		fh.SetMode(0o600)
		f, err := w.CreateHeader(fh)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("payload")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]Entry, 0, len(r.File))
	for _, f := range r.File {
		entries = append(entries, Entry{Name: f.Name, Size: int64(f.UncompressedSize64)})
	}
	err = Validate(goodSet, entries, Limits{})
	var ref *Refusal
	// The first refused entry is xray/xray.exe's neighbour in central-directory order; whichever shape the
	// reader hands back first, the guard must refuse before any write and name the bytes it saw.
	if !errors.As(err, &ref) {
		t.Fatalf("a real zip with a Zip-Slip member must be refused whole, got %v", err)
	}
	t.Logf("first refusal: %v", ref)
}

func TestValidateAcceptsNothingFromAnEmptySet(t *testing.T) {
	err := Validate(nil, entries("xray/xray.exe"), Limits{})
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Rule != ErrOutsideExpectedSet.Error() {
		t.Fatalf("an empty expected set must refuse everything, got %v", err)
	}
	if err := Validate(goodSet, nil, Limits{}); err != nil {
		t.Fatalf("an archive with no members is not an error here: step 8 refuses an empty extraction, not step 7: %v", err)
	}
}
