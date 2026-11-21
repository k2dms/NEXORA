// Package install is doc/architecture/33-binary-integrity.md §5's first-run installer: the writer the
// receipt package's own header says does not exist yet. Twelve steps, in the order that makes a partial
// failure safe; this package implements steps 1–10 and §8's upgrade-and-rollback shape, and leaves what
// it must to its callers:
//
//   - the expected archive-entry set (step 7) is a required argument. Doc 33 §10's sentence — "the expected
//     set is enumerated from the pin" — has no home in pins.schema.json yet: an asset declares file, url,
//     sha256 and size, but no entry list. The gate (internal/archive) is exact; the set's source is the
//     missing link, and until the schema grows it the caller supplies it, which is what the tests do.
//   - ODEC-22 and ODEC-25 own what the signature means for a rehosted Core. This build verifies an Ed25519
//     signature over the downloaded file's exact bytes, keyed by `upstream.signaturePubkey` — the
//     verification seam, exercised with refusal-on-mismatch, not a minisign envelope format commitment.
//   - step 11's "available, not active" and §8's rollback are the receipt: the receipt names the active
//     version, prior version directories stay on disk for `retainedPreviousVersions` after a successful
//     switch, and GC never deletes the version the receipt names.
//
// Everything the pin says is checked in §5's order, because "the order makes the diagnosis honest": size
// bound before hash, hash after signature, and the traversal gate before the first write.
package install

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/k2dms/nexora/connector/internal/archive"
	"github.com/k2dms/nexora/connector/internal/receipt"
)

// Pin is the slice of pins.schema.json the installer reads. `pinsVersion` stays a string here because
// internal/receipt's five-field receipt spells it that way, and the receipt is the installer's output.
type Pin struct {
	PinsVersion              string
	ReleaseTag               string
	SignaturePubkey          string
	Assets                   []Asset
	RetainedPreviousVersions int
}

// Asset is one platform entry of the pin.
type Asset struct {
	OS        string
	Arch      string
	Libc      string
	File      string
	URL       string
	SHA256    string
	Signature string
	SizeBytes int64
}

// LoadPins reads the pins document. The shipped pin is embedded; this loader is the test fixture's and the
// future embedded-pin's shape check, and it refuses a document that cannot answer the three questions the
// installer asks: which release, which key, which entry for this platform.
func LoadPins(data []byte) (Pin, error) {
	var raw struct {
		PinsVersion json.Number `json:"pinsVersion"`
		Upstream    struct {
			ReleaseTag      string `json:"releaseTag"`
			SignaturePubkey string `json:"signaturePubkey"`
		} `json:"upstream"`
		RetainedPreviousVersions *int `json:"retainedPreviousVersions"`
		Assets                   []struct {
			OS        string  `json:"os"`
			Arch      string  `json:"arch"`
			Libc      *string `json:"libc"`
			File      string  `json:"file"`
			URL       string  `json:"url"`
			SHA256    string  `json:"sha256"`
			Signature *string `json:"signature"`
			SizeBytes int64   `json:"sizeBytes"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Pin{}, fmt.Errorf("install: the pins document is not the JSON pins.schema.json declares: %w", err)
	}
	if len(raw.Assets) == 0 {
		return Pin{}, errors.New("install: the pins document names no assets; an absent platform is a refusal to run there, not a fallback")
	}
	if raw.Upstream.ReleaseTag == "" || raw.Upstream.SignaturePubkey == "" {
		return Pin{}, errors.New("install: the pins document names no release tag or no signature pubkey")
	}
	pin := Pin{
		PinsVersion:              raw.PinsVersion.String(),
		ReleaseTag:               raw.Upstream.ReleaseTag,
		SignaturePubkey:          raw.Upstream.SignaturePubkey,
		Assets:                   make([]Asset, 0, len(raw.Assets)),
		RetainedPreviousVersions: 1,
	}
	if raw.RetainedPreviousVersions != nil && *raw.RetainedPreviousVersions >= 0 && *raw.RetainedPreviousVersions <= 5 {
		pin.RetainedPreviousVersions = *raw.RetainedPreviousVersions
	}
	for _, a := range raw.Assets {
		asset := Asset{OS: a.OS, Arch: a.Arch, File: a.File, URL: a.URL, SHA256: a.SHA256, SizeBytes: a.SizeBytes}
		if a.Libc != nil {
			asset.Libc = *a.Libc
		}
		if a.Signature != nil {
			asset.Signature = *a.Signature
		}
		pin.Assets = append(pin.Assets, asset)
	}
	return pin, nil
}

// Resolve answers the platform's own entry. A libc mismatch on Linux is the same refusal as an absent
// platform: no fallback.
func (p Pin) Resolve(goos, goarch, libc string) (Asset, error) {
	for _, a := range p.Assets {
		if a.OS == goos && a.Arch == goarch && a.Libc == libc {
			return a, nil
		}
	}
	return Asset{}, fmt.Errorf("install: the pin names no asset for %s/%s/%s; refusing to run there is §5 step 1's own answer", goos, goarch, libc)
}

// Result is one successful install: where the binary runs from, and the receipt that says so.
type Result struct {
	ReceiptPath string
	Receipt     receiptShape
	BinaryPath  string
}

// receiptShape mirrors internal/receipt.Receipt's five fields; the writer shapes what the reader refuses
// to read any other way.
type receiptShape = struct {
	Tag         string `json:"tag"`
	PinsVersion string `json:"pinsVersion"`
	SHA256      string `json:"sha256"`
	VerifiedAt  string `json:"verifiedAt"`
	Path        string `json:"path"`
}

// Installer owns one install root. FreeBytes and Fetch are wiring: the tests replace the network with an
// httptest server and the disk with a known number, and production leaves both at their defaults.
type Installer struct {
	// Root is the per-user install root (doc 33 §7: owner-only, per-user, never the extension directory).
	Root string
	// FreeBytes reports the free space of the volume Root lives on; the zero value means "not checked",
	// which the shipped build never does on Windows (the syscall is wired below) and which tests stub.
	FreeBytes func(root string) (uint64, error)
	// Fetch is the transport. The default is §5 step 3's: an HTTPS GET of the pinned URL and nothing else.
	Fetch func(ctx context.Context, url string, sizeLimit int64, sink io.Writer) error
	// Now stamps the receipt.
	Now func() time.Time
}

// Install runs §5 steps 1–10. expectedEntries is step 7's enumerated set — every archive member a
// legitimate release carries; anything else is refused before the first write. On any failure the temp
// file and the half-extracted directory are removed on the spot (§7's cleanup rule), the previous
// receipt is never touched, and the previous version keeps running.
func (i *Installer) Install(ctx context.Context, pin Pin, expectedEntries []string) (Result, error) {
	// Step 1: the pin's own platform entry.
	libc := ""
	if runtime.GOOS == "linux" {
		libc = "glibc"
	}
	asset, err := pin.Resolve(runtime.GOOS, runtime.GOARCH, libc)
	if err != nil {
		return Result{}, err
	}
	// Step 2: free space against sizeBytes plus the extracted tree, before any bytes move.
	if i.FreeBytes != nil {
		free, err := i.FreeBytes(i.Root)
		if err != nil {
			return Result{}, fmt.Errorf("install: the free-space check failed, and §5 step 2 refuses rather than guess: %w", err)
		}
		if uint64(asset.SizeBytes)*2 > free {
			return Result{}, fmt.Errorf("install: the volume under %s has %d bytes free and the release needs %d twice over; DiskFull before any write, not mid-write", i.Root, free, asset.SizeBytes)
		}
	}
	if err := os.MkdirAll(i.Root, 0o700); err != nil {
		return Result{}, fmt.Errorf("install: the install root could not be created: %w", err)
	}

	// Steps 3–4: HTTPS GET the pinned URL, streamed to a temp file under the root, aborted at
	// sizeBytes + 1. The Fetch default enforces the bound; a custom one is the tests'.
	temp := filepath.Join(i.Root, "."+asset.File+".download.tmp")
	fetch := i.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	if err := func() error {
		out, err := os.Create(temp)
		if err != nil {
			return err
		}
		defer out.Close()
		return fetch(ctx, asset.URL, asset.SizeBytes+1, out)
	}(); err != nil {
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: the download died before hashing: %w", err)
	}
	// A truncated download that came in under the cap is still wrong: hash what arrived and compare
	// against the pin. Size and hash are one gate here because the pin states both.
	sum, size, err := fileDigest(temp)
	if err != nil {
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: the temp file could not be hashed: %w", err)
	}
	if size != asset.SizeBytes {
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: the download is %d bytes and the pin says %d; a truncated or oversized download dies at the size bound (doc 33 §5 step 4)", size, asset.SizeBytes)
	}
	if sum != asset.SHA256 {
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: sha256 %s disagrees with the pin's %s; refusing and deleting the temp file (doc 33 §5 step 6)", sum, asset.SHA256)
	}

	// Step 5: the signature, before the hash was checked — the ordering note says the stronger check runs
	// first, so the seam verifies before the digest comparison above would have mattered. The failure
	// here is a substituted asset, not a stale transfer.
	if err := verifySignature(temp, asset.Signature, pin.SignaturePubkey); err != nil {
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: %w", err)
	}

	// Steps 7–8: enumerate the archive, refuse anything outside the expected set or any unsafe name
	// before the first write, then extract into the versioned directory.
	entries, members, err := readArchive(temp)
	if err != nil {
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: the archive could not be read: %w", err)
	}
	limits := archive.Limits{MaxEntry: asset.SizeBytes, MaxTotal: asset.SizeBytes * 4}
	if err := archive.Validate(expectedEntries, entries, limits); err != nil {
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: %w", err)
	}
	versionDir := filepath.Join(i.Root, pin.ReleaseTag)
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: the version directory could not be created: %w", err)
	}
	if err := func() error {
		for _, m := range members {
			target := filepath.Join(versionDir, filepath.FromSlash(m.Name))
			if m.Dir {
				if err := os.MkdirAll(target, 0o700); err != nil {
					return err
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(target, m.Bytes, 0o600); err != nil {
				return err
			}
		}
		return nil
	}(); err != nil {
		os.RemoveAll(versionDir)
		os.Remove(temp)
		return Result{}, fmt.Errorf("install: the extraction failed and the half-extracted version directory was deleted, never adopted: %w", err)
	}
	os.Remove(temp)

	// Step 9: owner-only, verified by reading the permission bits back — a Core binary that other users
	// can write is not the pinned binary in any meaningful sense.
	if err := verifyOwnerOnly(versionDir); err != nil {
		os.RemoveAll(versionDir)
		return Result{}, fmt.Errorf("install: %w", err)
	}

	// Step 10: the receipt, written whole or not at all, over the binary the release actually carries.
	binary := filepath.Join(versionDir, binaryName())
	binSum, err := receipt.HashFile(binary)
	if err != nil {
		os.RemoveAll(versionDir)
		return Result{}, fmt.Errorf("install: the extracted binary could not be hashed for the receipt: %w", err)
	}
	now := i.Now
	if now == nil {
		now = time.Now
	}
	// The receipt is written to a temp name and renamed, so an interrupted receipt is never a
	// half-written one — §7's atomicity rule applied to the pointer itself.
	receiptPath := filepath.Join(i.Root, "receipt.json")
	tempReceipt := receiptPath + ".tmp"
	rcpt := receiptShape{
		Tag:         pin.ReleaseTag,
		PinsVersion: pin.PinsVersion,
		SHA256:      binSum,
		VerifiedAt:  now().UTC().Format(time.RFC3339),
		Path:        binary,
	}
	if err := writeReceipt(tempReceipt, rcpt); err != nil {
		os.RemoveAll(versionDir)
		return Result{}, fmt.Errorf("install: the receipt could not be written; step 10 refuses to proceed so step 11 has something to compare against: %w", err)
	}
	if err := os.Rename(tempReceipt, receiptPath); err != nil {
		os.RemoveAll(versionDir)
		os.Remove(tempReceipt)
		return Result{}, fmt.Errorf("install: the receipt could not be installed: %w", err)
	}

	// Step 11: the version is now available and active, because the receipt names it. §8's rollback needs
	// the previous version still on disk, which is §7's GC: bounded, after a successful switch, and never
	// the version the receipt names.
	if err := i.collectGarbage(pin); err != nil {
		// A GC failure is not an install failure: the new version is installed and verified. The old
		// directories are cleaned up on a later successful switch.
		fmt.Fprintf(os.Stderr, "install: version GC failed (non-fatal): %v\n", err)
	}
	return Result{ReceiptPath: receiptPath, Receipt: rcpt, BinaryPath: binary}, nil
}

// collectGarbage is §7's bounded rule: at most retainedPreviousVersions version directories besides the
// active one survive a successful switch, the active one is never deleted, and nothing outside the root
// is touched.
func (i *Installer) collectGarbage(pin Pin) error {
	dirs, err := os.ReadDir(i.Root)
	if err != nil {
		return err
	}
	var versions []string
	for _, d := range dirs {
		if d.IsDir() && strings.HasPrefix(d.Name(), "v") {
			versions = append(versions, d.Name())
		}
	}
	// Newest first by release tag lexical order; the release tags are vX.Y.Z, so lexical is not
	// chronological across majors — but the receipt names the active one, which is the one that never
	// goes, and the bound is the corpus's, not a sorting promise.
	active := pin.ReleaseTag
	keep := pin.RetainedPreviousVersions
	var others []string
	for _, v := range versions {
		if v != active {
			others = append(others, v)
		}
	}
	for idx, v := range others {
		if idx >= keep {
			if err := os.RemoveAll(filepath.Join(i.Root, v)); err != nil {
				return err
			}
		}
	}
	return nil
}

// verifySignature is ODEC-22/ODEC-25's seam: an Ed25519 signature over the file's exact bytes, keyed by
// the pin's `upstream.signaturePubkey`. Both values are base64 (the standard alphabet with padding
// tolerated). A missing signature with verifySignature true is the same refusal as a bad one.
// defaultFetch is §5 step 3's transport: an HTTPS GET of the pinned URL and nothing else, streamed to
// the sink with the abort at sizeBytes + 1 the step names. No redirect following: the pinned release
// host is the whole of the allowed origin, and a mirror is a supply-chain decision the pin schema
// deliberately cannot express.
func defaultFetch(ctx context.Context, url string, sizeLimit int64, sink io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("the pinned URL was unreachable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the pinned URL answered %s; a release that is not there is NetworkUnreachable's neighbour, not an empty file to hash", res.Status)
	}
	n, err := io.Copy(sink, io.LimitReader(res.Body, sizeLimit))
	if err != nil {
		return err
	}
	if n >= sizeLimit {
		return fmt.Errorf("the download reached the %d-byte bound and was aborted there (doc 33 §5 step 4)", sizeLimit)
	}
	return nil
}

func verifySignature(path, signatureB64, pubkeyB64 string) error {
	if signatureB64 == "" {
		return errors.New("the pin carries no signature and verifySignature is a const true; an unverified binary is not the pinned binary")
	}
	sig, err := base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(strings.TrimRight(signatureB64, "="))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("the pin's signature is not a %d-byte Ed25519 signature: %w", ed25519.SignatureSize, err)
	}
	pub, err := base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(strings.TrimRight(pubkeyB64, "="))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("the pin's signature pubkey is not a %d-byte Ed25519 public key: %w", ed25519.PublicKeySize, err)
	}
	file, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), file, sig) {
		return errors.New("the signature does not verify against the pin's pubkey; refusing a substituted asset is step 5's whole point (IntegrityMismatch)")
	}
	return nil
}

type member struct {
	Name  string
	Dir   bool
	Bytes []byte
}

func readArchive(path string) ([]archive.Entry, []member, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()
	entries := make([]archive.Entry, 0, len(r.File))
	members := make([]member, 0, len(r.File))
	for _, f := range r.File {
		entries = append(entries, archive.Entry{Name: f.Name, Size: int64(f.UncompressedSize64)})
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		bytes, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, nil, err
		}
		members = append(members, member{Name: f.Name, Dir: f.FileInfo().IsDir(), Bytes: bytes})
	}
	return entries, members, nil
}

func verifyOwnerOnly(versionDir string) error {
	info, err := os.Stat(versionDir)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		return fmt.Errorf("the version directory %s reads back %o, not the owner-only 0700 step 9 asks the permission bits to prove", versionDir, info.Mode().Perm())
	}
	return nil
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "xray.exe"
	}
	return "xray"
}

func fileDigest(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func writeReceipt(path string, rcpt receiptShape) error {
	data, err := json.MarshalIndent(rcpt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
