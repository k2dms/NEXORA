package receipt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goodReceipt is a receipt the installer of doc 33 §5 step 10 could have written: the five fields, a digest in
// the pins charset, a stamp in the pin's own format. The digest is sha256 of the five bytes "nonce", so the
// tests below can write a file whose hash is known without hashing anything twice.
const goodDigest = "78377b525757b494427f89014f97d79928f3938d14eb51e20fb5dec9834eb304"

func goodReceipt() Receipt {
	return Receipt{
		Tag:         "v25.8.29",
		PinsVersion: "2026-11-01",
		SHA256:      goodDigest,
		VerifiedAt:  "2026-11-01T12:00:00Z",
		Path:        "core/v25.8.29/xray.exe",
	}
}

func writeReceipt(t *testing.T, dir string, r Receipt) string {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal the fixture receipt: %v", err)
	}
	p := filepath.Join(dir, "receipt.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write the fixture receipt: %v", err)
	}
	return p
}

func writeBytes(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestLoadRejectsAReceiptMissingAnyOfTheFiveFields(t *testing.T) {
	for _, dropped := range receiptFields {
		t.Run(dropped, func(t *testing.T) {
			r := goodReceipt()
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			delete(raw, dropped)
			p := writeBytes(t, t.TempDir(), "receipt.json", mustJSON(t, raw))
			_, err = Load(p)
			if err == nil {
				t.Fatalf("a receipt without %s loaded", dropped)
			}
			if !strings.Contains(err.Error(), "names no "+dropped) {
				t.Fatalf("the refusal does not name the field that is missing: %v", err)
			}
		})
	}
}

func TestLoadRejectsAReceiptCarryingAFieldStepTenDoesNotWrite(t *testing.T) {
	r := goodReceipt()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw["sizeBytes"] = json.RawMessage(`1048576`)
	p := writeBytes(t, t.TempDir(), "receipt.json", mustJSON(t, raw))
	if _, err := Load(p); err == nil {
		t.Fatal("a receipt with a sixth field loaded")
	}
}

func TestLoadRefusesAnEmptyFactInAnyOfTheFiveFields(t *testing.T) {
	for _, emptied := range receiptFields {
		t.Run(emptied, func(t *testing.T) {
			r := goodReceipt()
			switch emptied {
			case "tag":
				r.Tag = ""
			case "pinsVersion":
				r.PinsVersion = ""
			case "sha256":
				r.SHA256 = ""
			case "verifiedAt":
				r.VerifiedAt = ""
			case "path":
				r.Path = ""
			}
			p := writeReceipt(t, t.TempDir(), r)
			_, err := Load(p)
			if err == nil {
				t.Fatalf("a receipt whose %s is empty loaded", emptied)
			}
		})
	}
}

func TestLoadRefusesADigestOutsideThePinsVocabulary(t *testing.T) {
	for name, digest := range map[string]string{
		"sixty-three digits": goodDigest[:63],
		"sixty-five digits":  goodDigest + "a",
		"uppercase":          strings.ToUpper(goodDigest),
		"not hex":            strings.Repeat("g", 64),
	} {
		t.Run(name, func(t *testing.T) {
			r := goodReceipt()
			r.SHA256 = digest
			p := writeReceipt(t, t.TempDir(), r)
			if _, err := Load(p); err == nil {
				t.Fatalf("a digest %q loaded", digest)
			}
		})
	}
}

func TestLoadRefusesAStampThatIsNotTheDateTimeThePinUses(t *testing.T) {
	for name, stamp := range map[string]string{
		"a date with no time": "2026-11-01",
		"no zone":             "2026-11-01T12:00:00",
		"prose":               "yesterday, after lunch",
	} {
		t.Run(name, func(t *testing.T) {
			r := goodReceipt()
			r.VerifiedAt = stamp
			p := writeReceipt(t, t.TempDir(), r)
			if _, err := Load(p); err == nil {
				t.Fatalf("a verifiedAt of %q loaded", stamp)
			}
		})
	}
	if _, err := time.Parse(time.RFC3339, goodReceipt().VerifiedAt); err != nil {
		t.Fatalf("the fixture's own stamp stopped being a date-time: %v", err)
	}
}

func TestHashFileDigestsWithTheAlgorithmTheReceiptNames(t *testing.T) {
	// sha256 of "abc", the test vector the algorithm's own specification publishes.
	p := writeBytes(t, t.TempDir(), "abc", []byte("abc"))
	sum, err := HashFile(p)
	if err != nil {
		t.Fatalf("hash a three-byte file: %v", err)
	}
	if sum != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("the digest is not sha256: %s", sum)
	}
}

func TestHashFileRefusesAFileItCannotOpen(t *testing.T) {
	if _, err := HashFile(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing file hashed")
	}
}

func TestTheVerifierAcceptsTheFileTheReceiptWasWrittenFrom(t *testing.T) {
	dir := t.TempDir()
	body := []byte("nonce")
	p := writeBytes(t, dir, "core", body)
	r := goodReceipt()
	r.SHA256 = goodDigest
	r.Path = p
	rp := writeReceipt(t, dir, r)
	if err := NewVerifier(rp)(p); err != nil {
		t.Fatalf("the verifier refused the very file the receipt was written from: %v", err)
	}
}

func TestTheVerifierRefusesAFileWhoseBytesMoved(t *testing.T) {
	dir := t.TempDir()
	p := writeBytes(t, dir, "core", []byte("nonce"))
	r := goodReceipt()
	r.Path = p
	rp := writeReceipt(t, dir, r)
	other := writeBytes(t, dir, "other", []byte("once"))
	err := NewVerifier(rp)(other)
	if err == nil {
		t.Fatal("the verifier accepted a file whose bytes moved")
	}
	if !strings.Contains(err.Error(), "refuses the exec") {
		t.Fatalf("the refusal does not name the row that refuses: %v", err)
	}
}

func TestTheVerifierRefusesASpawnWhenNoReceiptIsOnDisk(t *testing.T) {
	dir := t.TempDir()
	p := writeBytes(t, dir, "core", []byte("nonce"))
	err := NewVerifier(filepath.Join(dir, "absent.json"))(p)
	if err == nil {
		t.Fatal("a spawn with no receipt anywhere passed the pre-exec check")
	}
}

func TestTheVerifierRefusesAReceiptItCannotFullyRead(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":             "the installer was killed mid-write",
		"not an object":        `["tag"]`,
		"a digest not in hex":  `{"tag":"v","pinsVersion":"p","sha256":"zz","verifiedAt":"2026-11-01T12:00:00Z","path":"x"}`,
		"a stamp with no zone": `{"tag":"v","pinsVersion":"p","sha256":"` + goodDigest + `","verifiedAt":"2026-11-01T12:00:00","path":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := writeBytes(t, dir, "core", []byte("nonce"))
			rp := writeBytes(t, dir, "receipt.json", []byte(content))
			if err := NewVerifier(rp)(p); err == nil {
				t.Fatalf("a receipt the reader cannot fully read passed the pre-exec check: %s", name)
			}
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
