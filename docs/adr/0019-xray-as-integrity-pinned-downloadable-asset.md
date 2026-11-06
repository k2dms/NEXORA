# ADR-0019: Xray Core is an integrity-pinned downloadable asset, never vendored and never inside the extension archive

Status: Accepted
Date: 2026-11-04

## Context

The system needs a foreign executable at runtime. ADR-0003 puts VLESS/REALITY/TLS/Vision inside Xray Core
behind the Go Connector, so the question is not whether to run it but how its bytes reach the machine and
what proves they are the bytes we meant.

Three properties of this project make the obvious answers wrong.

It is a **browser extension**. Chrome Web Store and Edge reviews treat a packaged third-party native
binary as a red flag, and the current archive is already a large static bundle: the release zip is asserted
at 353 files / 5,999,353 bytes uncompressed (`docs/PROJECT-STATE.md` section 5), while a single Xray release
asset is on the order of 9-11 MB (`packages/schema/examples/pins.xray.json:21`). Bundling would multiply the
download for every user to carry a binary most users' browser never executes.

It has an **independent upgrade cadence**. Upstream Core ships on its own schedule; a bundled binary makes
every Core fix an extension store release with review latency, and makes per-user rollback impossible. The
same argument cuts the other way for the extension: an extension release MUST NOT silently swap the binary
that carries user traffic.

And a wrong binary is not a crash, it is a **compromise that carries traffic**. A supply-chain substitution
in a proxy Core is indistinguishable from working correctly to the user. That is why pinning is expressed
as data, not as prose: the brief's constraint 6 says no committed binaries, and
`packages/schema/pins.schema.json` says what an acceptable binary is.

## Decision

1. Xray Core MUST NOT be committed to git, MUST NOT be vendored as a submodule build artifact, and MUST NOT
   appear inside the extension archive for any browser. The Connector is never in the zip either
   (`docs/architecture/20-target-repository-layout.md`).
2. The Core MUST be obtained at first run as a **downloadable asset described by a pin document**, whose
   shape is `packages/schema/pins.schema.json` and whose instance lives in the Connector tree at
   `connector/testdata/pins.json`. The pin document MUST name exactly one upstream repository --
   `packages/schema/pins.schema.json:31` makes any other value invalid -- and an **exact release tag**,
   because `packages/schema/pins.schema.json:36` admits neither ranges nor `latest`.
3. Every asset entry MUST carry `sha256` (`packages/schema/pins.schema.json:132`), a `sizeBytes` within
   bounds (`packages/schema/pins.schema.json:144`), a `verifiedAt` stamp (`packages/schema/pins.schema.json:150`)
   recording that a human or CI actually checked the pin against upstream, and a URL restricted to the
   upstream release host (`packages/schema/pins.schema.json:128`). A mirror is a supply-chain decision and
   is structurally inexpressible -- the negative case
   `packages/schema/examples/invalid/pins.mirror-url.json` is rejected for that reason
   (`packages/schema/examples/invalid/reasons.json:90`).
4. Verification is **both** mechanisms, not one. `verifySignature` is `const true`
   (`packages/schema/pins.schema.json:56`): a hash list is worthless to an attacker who controls both the
   asset and the list, so the minisign signature is checked too, against the pinned
   `upstream.signaturePubkey`. A hash or signature mismatch is `IntegrityMismatch`
   (`packages/schema/protocol/error-taxonomy.json:26`) and MUST mean refusing to start, not "start and warn".
5. Installation target is a **per-user path** resolved by the Connector, e.g.
   `%LOCALAPPDATA%\NEXORA\core` (`packages/schema/pins.schema.json:67`, instance at
   `packages/schema/examples/pins.xray.json:11`): never the extension directory, never `Program Files`, so
   no elevation is requested and the ACL stays user-owned.
6. **No silent self-update.** `silentSelfUpdate` is `const false`
   (`packages/schema/pins.schema.json:60`). A Core upgrade happens only on explicit user action, MUST
   re-verify the new pins before switching, and MUST retain the previous verified version for rollback --
   `retainedPreviousVersions` defaults to 1 (`packages/schema/pins.schema.json:73`) and a value of 0 is
   allowed only where the product knowingly gives up rollback.
7. There is no path that runs "whatever binary is on disk". `installChannel` is
   `pinned` or `manual` only (`packages/schema/connector-settings.schema.json:34`); `manual` is the
   documented air-gapped route and still requires a pin document to be satisfied, because a binary chosen by
   path is a binary chosen by whoever wrote that path.
8. An absent platform entry MUST be a refusal to run there, not a fallback
   (`packages/schema/pins.schema.json:83`). Adding Windows arm64, or musl Linux, means adding a verified pin
   row in a reviewed change.

## Consequences

- First run needs network, or a documented `manual` installation. Air-gapped setups are a real product
  limitation, tracked as an open question rather than pretended away.
- Pin maintenance becomes recurring work with a named owner: every Core upgrade is a reviewed change to the
  pin document including a fresh `verifiedAt`, and CI MUST validate the document against its schema before
  the Connector is built.
- Antivirus and SmartScreen friction moves from "unsigned binary in the extension" to "unsigned binary we
  downloaded": the pin carries `authenticodeSubject` precisely so the observation is recorded rather than
  discovered in a support ticket (`packages/schema/pins.schema.json:158`).
- Core upgrades become user-visible and reversible, which is what makes it acceptable to depend on upstream
  at all.
- Disk usage per user grows by the retained versions; bounded by `retainedPreviousVersions`.
- The extension archive stays a pure web bundle, which keeps the store story simple and the zip assertions
  in the baseline report meaningful.
- Nothing in this iteration could prove any of it: no Core, no installer, no network probe, and the Go toolchain
  only arrived later the same day (`docs/PROJECT-STATE.md` §4.6). Data, as above; behaviour, deferred.

## Alternatives considered

- **Bundle the Core binary in the extension archive.** Rejected on all three Context properties: store
  review risk, archive size, and coupling Core upgrades to store release latency; also violates the brief's
  no-committed-binaries constraint once the same file is in git.
- **Vendor the binary in git or Git LFS.** Rejected: turns the repository into a release mirror of upstream,
  makes every Core bump a multi-megabyte commit, and still provides no signature story.
- **Build Xray from source and ship our own signed binaries.** Rejected as a phase-1 non-option: it makes us
  a software publisher with a signing infrastructure and a rebuild pipeline for a component whose source we
  do not intend to fork. It MAY return as a superseding ADR if pinning upstream assets becomes untenable.
- **Resolve "latest release" at install time.** Rejected outright: `packages/schema/pins.schema.json:36`
  exists to make that unrepresentable.
- **A loopback download page, or letting the extension fetch the binary.** Rejected: the extension is not a
  package manager, ADR-0004 forbids extra local control surfaces, and installing an executable is the
  Connector's job under Connector ACLs.
- **System package manager (`winget`/`apt`/`brew`).** Rejected: no version control, elevation on some hosts,
  absent on others, and it silently auto-updates the binary carrying user traffic.

## Evidence

- VERIFIED -- `packages/schema/pins.schema.json:31` (single permitted repository), `:36` (exact tag),
  `:56` (`verifySignature` const true), `:60` (`silentSelfUpdate` const false), `:128` (upstream release
  host only), `:132` (sha256 charset), `:144` (minimum 1 MiB), `:158` (recorded Authenticode subject).
- VERIFIED -- `packages/schema/examples/pins.xray.json:11` and `:21`: a conforming instance with the
  per-user install root and a realistic asset size.
- VERIFIED -- `packages/schema/protocol/error-taxonomy.json:26`: `IntegrityMismatch` is a non-retryable
  security-layer error whose meaning is "Core refuses to start".
- VERIFIED -- `packages/schema/connector-settings.schema.json:34`: the install channel enum is exactly
  `pinned` and `manual`.
- INFERRED -- store-review sensitivity to a bundled native binary, and SmartScreen/antivirus behaviour on a
  downloaded unsigned executable. Policy and product behaviour, not observable in this tree.
- UNVERIFIED -- that the pinned tag and hashes in the current example correspond to any real upstream
  release: the example values are placeholders for shape, and no network fetch was performed here. Closing
  this is the first Connector task.

## Verification

- Existing check: `npm --prefix packages/schema run verify` compiles `pins.schema.json`, validates the
  conforming instance, and rejects the mirror-URL case for the recorded keyword -- so rules 2, 3 and 7 are
  machine-checked at the contract layer today.
- Verified: NO -- deferred to Phase 2: download, sha256 and minisign verification, refusal on mismatch,
  per-user install ACL, upgrade-with-rollback, and `manual` channel behaviour. These need the Go module and
  a live host, and are `LIVE` rows (ADR-0020).
- Verified: YES -- rule 1 has its repository guard. The `VER-08` step of `.github/workflows/verify.yml` reads
  the first bytes of every tracked file and fails on a PE, ELF or Mach-O magic number, and `.gitattributes`
  now declares the executable suffixes beside the `connector/bin/` and `dist/` patterns of `.gitignore`.
