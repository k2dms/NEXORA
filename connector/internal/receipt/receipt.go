// Package receipt reads the install receipt — doc 33 §5 step 10's "(tag, `pinsVersion`, file hash, `verifiedAt`,
// path)" — and is the producer of doc 33 §6's pre-exec check: "every `core.start` / `core.restart`, before exec,
// `sha256` of the file being spawned, compared to the install receipt". data-ownership's `core-binary` row says
// what the file is for in one clause: the install root holds "the executable that carries the user's traffic,
// plus the receipts that say it was verified".
//
// §7's sentence is the reason this package is a reader and only a reader: "Receipts, not directory listings" —
// which version is verified is answered by a receipt the installer wrote, so a stray directory in the install
// root is not a candidate for execution, and neither is a path whose bytes no receipt names. The verifier below
// therefore refuses on three shapes of nothing: no receipt to read, a receipt that is not the five fields, and a
// file whose digest disagrees with the one the receipt recorded. All three are the same answer at the point of
// use — doc 22 §4's unbootable Core, which internal/supervisor wraps in ErrIntegrity and the command surface
// answers `IntegrityMismatch` — because §12's table gives an integrity refusal no retry.
//
// What this package deliberately does not have is a writer. data-ownership's row gives that job to "the
// installer, only after signature then hash then size then traversal checks", and the first of those four is
// pinned open twice: pins.schema.json carries `verifySignature` as a `const`, and ODEC-22/ODEC-25 own what that
// signature means for a rehosted Core. §5's own ordering note is why the other three steps do not excuse it —
// "the order makes the diagnosis honest" — so a receipt written without the signature check would be a lie about
// a verification that did not happen, written by the one component whose whole job is not to lie about that.
// Nothing under connector/ writes a receipt in this build, which is why a missing one is the expected answer
// until the installer exists, and why the refusal below is fail-closed rather than provisional.
//
// Three refusals to invent, next to the code that wanted them:
//
//   - No document names the receipt's file name, so the path comes in from the wiring — the same seam
//     internal/supervisor's Dir and internal/apply's write directory are, for the same reason.
//   - `verifiedAt` is required and parsed and then not interpreted. No document states a freshness rule for a
//     receipt, because §6's control is the hash and not the stamp: a receipt whose verification is a year old is
//     still the only receipt there is about these bytes.
//   - The five fields are the whole of what decodes. A receipt carrying a sixth name claims an authority doc 33
//     §5 never wrote, which is what a closed field list exists to refuse.
package receipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"
)

// Receipt is the install receipt of doc 33 §5 step 10. The JSON names follow the pins vocabulary where the same
// fact has a name there — `pinsVersion`, `sha256` and `verifiedAt` are pins.schema.json's own spellings — and
// `tag` and `path` are step 10's own words for the release and the file it verified.
type Receipt struct {
	Tag         string `json:"tag"`
	PinsVersion string `json:"pinsVersion"`
	SHA256      string `json:"sha256"`
	VerifiedAt  string `json:"verifiedAt"`
	Path        string `json:"path"`
}

// receiptFields is step 10's receipt, in the order the sentence names it. The order is load-bearing in one
// place only: a receipt missing several fields is refused for the first one it lacks, and a reader comparing
// two refusals of the same broken file should see the same words both times.
var receiptFields = []string{"tag", "pinsVersion", "sha256", "verifiedAt", "path"}

// reSHA256 is pins.schema.json's own charset for the digest — `^[0-9a-f]{64}$` — copied rather than parsed at
// runtime because the Connector ships as one binary, the same reason internal/hello copies its version patterns.
var reSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Load reads and shapes one receipt. Every failure is the refusal the verifier turns into a stopped exec: a
// receipt this process cannot fully read answers "which version is verified" with nothing, and §7's rule gives
// that answer no fallback — there is no directory listing to consult instead.
func Load(path string) (Receipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Receipt{}, fmt.Errorf("receipt: the install receipt could not be read, and receipts-not-listings gives nothing to fall back to: %w", err)
	}
	return decode(data)
}

func decode(data []byte) (Receipt, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Receipt{}, fmt.Errorf("receipt: the install receipt is not a JSON object: %w", err)
	}
	for k := range raw {
		known := false
		for _, name := range receiptFields {
			if k == name {
				known = true
				break
			}
		}
		if !known {
			return Receipt{}, fmt.Errorf("receipt: the install receipt carries %q, and step 10's receipt is five fields", k)
		}
	}
	values := make(map[string]string, len(receiptFields))
	for _, name := range receiptFields {
		v, ok := raw[name]
		if !ok {
			return Receipt{}, fmt.Errorf("receipt: the install receipt names no %s, and a receipt is written whole or not at all", name)
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return Receipt{}, fmt.Errorf("receipt: the receipt's %s is not a string", name)
		}
		if s == "" {
			return Receipt{}, fmt.Errorf("receipt: the receipt's %s is empty, and a receipt with an empty fact is not a receipt about anything", name)
		}
		values[name] = s
	}
	r := Receipt{
		Tag:         values["tag"],
		PinsVersion: values["pinsVersion"],
		SHA256:      values["sha256"],
		VerifiedAt:  values["verifiedAt"],
		Path:        values["path"],
	}
	if !reSHA256.MatchString(r.SHA256) {
		return Receipt{}, errors.New("receipt: the receipt's sha256 is not the 64 lowercase hex digits pins.schema.json digests with")
	}
	if _, err := time.Parse(time.RFC3339, r.VerifiedAt); err != nil {
		return Receipt{}, fmt.Errorf("receipt: the receipt's verifiedAt is not the date-time the pin's own stamp is written as: %w", err)
	}
	return r, nil
}

// HashFile is the digest §6's row names: `sha256` of the file being spawned. It streams, because §6 prices the
// check at "~10 MB hashed, tens of milliseconds" and reading a Core whole into memory to save a buffer would
// price it differently.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("receipt: the file being spawned could not be opened to hash it: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 64*1024)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if readErr == io.EOF {
			return hex.EncodeToString(h.Sum(nil)), nil
		}
		if readErr != nil {
			return "", fmt.Errorf("receipt: the file being spawned could not be read to hash it: %w", readErr)
		}
	}
}

// NewVerifier is the producer internal/supervisor's Config.Verify takes: §6's row as a func, run before every
// exec the supervisor attempts — a ladder retry included, because that row names the calls rather than the
// first one. The receipt path is the wiring's fact; the three refusals inside are fail-closed, because §12
// gives `IntegrityMismatch` no retry and a missing receipt is the expected state of a machine where nothing
// verified anything yet.
//
// The mismatch error names both digests and neither path: §12's table needs the diagnosis ("which hash moved")
// and the supervisor's own wrap already names what was about to run, while the digests are facts about a
// public asset and not about the user.
func NewVerifier(receiptPath string) func(binaryPath string) error {
	return func(binaryPath string) error {
		r, err := Load(receiptPath)
		if err != nil {
			return err
		}
		sum, err := HashFile(binaryPath)
		if err != nil {
			return err
		}
		if sum != r.SHA256 {
			return fmt.Errorf("receipt: the file being spawned hashes to %s and the receipt written for it names %s; doc 33 §6's pre-exec row refuses the exec", sum, r.SHA256)
		}
		return nil
	}
}
