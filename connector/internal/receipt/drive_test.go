package receipt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// driveAsset names the env var that points this test at a placed Core binary. The synthetic tests above prove
// the contract over bytes this repo controls; this one exists to run the same code over the bytes a release
// actually shipped — LIVE-21's runtime half in miniature, on a machine that has the asset. It writes the
// receipt it verifies against from the digest it measured itself, so the pass case proves the mechanism and
// not the fixture: the honest cross-check is that the digest it prints is the one the release asset is
// publicly known to hash to, which is a fact for the record to quote, not for a test to embed.
const driveAsset = "NEXORA_RECEIPT_ASSET"

func TestDriveOverTheAssetTheOperatorPointsAt(t *testing.T) {
	bin := os.Getenv(driveAsset)
	if bin == "" {
		t.Skipf("no asset to drive over: set %s to the placed Core binary; LIVE-21's runtime half is a machine with a real install, and this build has none", driveAsset)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("the asset the operator named is not there: %v", err)
	}

	started := time.Now()
	sum, err := HashFile(bin)
	if err != nil {
		t.Fatalf("hash the real asset: %v", err)
	}
	elapsed := time.Since(started)
	t.Logf("the asset hashes to %s in %s", sum, elapsed)

	dir := t.TempDir()
	r := Receipt{
		Tag:         "drive",
		PinsVersion: "drive",
		SHA256:      sum,
		VerifiedAt:  time.Now().UTC().Format(time.RFC3339),
		Path:        bin,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal the drive receipt: %v", err)
	}
	rp := filepath.Join(dir, "receipt.json")
	if err := os.WriteFile(rp, data, 0o600); err != nil {
		t.Fatalf("write the drive receipt: %v", err)
	}
	verify := NewVerifier(rp)
	if err := verify(bin); err != nil {
		t.Fatalf("the verifier refused the very file the receipt was written from: %v", err)
	}

	// The tamper case is the check's reason to exist (§6's "catches" column): one byte, mid-file, the kind of
	// rewrite an antivirus or a partial rollback produces. The refusal must carry both digests, because §12's
	// table needs to say which hash moved.
	raw, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read the asset back: %v", err)
	}
	mid := len(raw) / 2
	raw[mid] ^= 0xFF
	tampered := filepath.Join(dir, "tampered")
	if err := os.WriteFile(tampered, raw, 0o600); err != nil {
		t.Fatalf("write the tampered copy: %v", err)
	}
	err = verify(tampered)
	if err == nil {
		t.Fatalf("the verifier accepted a copy of the asset whose byte %d moved", mid)
	}
	t.Logf("the tampered copy was refused: %v", err)

	if err := NewVerifier(filepath.Join(dir, "absent.json"))(bin); err == nil {
		t.Fatal("with no receipt on disk the verifier answered a question nobody had verified")
	}
}
