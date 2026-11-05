# 33 — Binary integrity: fetch, verify, install, upgrade

> Purpose: ADR-0019 decides *what* a permitted Core binary is. This document works out how the Connector gets
> to that answer on a user's machine and what has to stay true afterwards: the trust chain link by link, the
> first-run procedure with its failure at every step, when each verification runs and what it can actually
> catch, the upgrade and rollback mechanics, the air-gapped route, and the question ADR-0019 deliberately leaves
> open — who verifies the Connector itself. A proxy Core is the one component where a supply-chain substitution
> is indistinguishable from correct behaviour, so this is the document where "it started" is not evidence of
> anything.

Status: Accepted. Normative data: `packages/schema/pins.schema.json`, `packages/schema/examples/pins.xray.json`,
`packages/schema/connector-settings.schema.json#/properties/core/properties/installChannel`,
`packages/schema/protocol/error-taxonomy.json#/codes`,
`packages/schema/protocol/commands/core.start.json#/errors`,
`packages/schema/protocol/commands/connector.version.json#/result/properties`,
`packages/schema/protocol/commands/config.apply.json#/result/properties/configHash`.
Evidence base: `docs/audit/11-security-findings.md` (SEC-6 — the measured absence of any integrity concept in
the legacy tree), `docs/audit/10-manifest-packaging-distribution.md` (what the archive is and is not),
`docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md`.

---

## 1. What this document owns

`docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md` is the decision and its alternatives; this is the
procedure that makes the decision executable and the four places where following it literally required choosing
something the ADR did not state.

Owned here: the trust chain (§3), the pin document's operational life (§4), install (§5–§7), verification timing
(§6), upgrade and rollback (§8), the `manual` channel (§9), platform coverage (§10), what the compiled config
must not make reachable (§11), `IntegrityMismatch` as a remediation object (§12), the antivirus story (§13), the
Connector's own provenance and uninstall (§14), and the failure-mode table (§15).

Not owned here: where the file sits in the repository layout (`docs/architecture/20-target-repository-layout.md`
§9), how Core is supervised once running and what its death means
(`docs/architecture/22-component-contracts.md` §6, `docs/architecture/30-failure-policy.md`), which fields the
compiler emits (`docs/architecture/27-config-compilation-pipeline.md` — §11 states the prohibition, that document
owns the emit list), the identity model for who may ask for any of this (`docs/architecture/31-security-model.md`),
and whether any row below has actually been executed — `docs/verification/live-matrix.md`, cited per row.

`Verified: NO — deferred to Phase 2` appears at the point of use throughout, and in this document that is nearly
every behavioural claim: there is no Go toolchain on this host, no Core, no installer, and no network fetch was
performed in this iteration. What *is* green is the contract layer, and §4 names exactly which assertions are
green today.

## 2. The questions this document was cited to answer

| Cited by | What it deferred | Answered in |
|---|---|---|
| `docs/adr/0004-native-messaging-only-no-loopback-http-control.md:54` | install and uninstall as "a real design subject" | §5, §7, §14 |
| `docs/architecture/21-system-context.md:154` | no bundled binary in the archive | §3, §4 |
| `docs/architecture/21-system-context.md:155` | "no silent self-update of Core or connector" | §8, §11 |
| `docs/architecture/22-component-contracts.md:83` | the host's pinnable identity being the manifest it writes | §3, §14 |
| `docs/architecture/22-component-contracts.md:144` | an unbootable Core (bad binary, AV quarantine) routed here | §12, §13 |
| `docs/architecture/23-unified-server-model.md:76` | what the *pinned Core* supports (REALITY over gRPC) | §4.3 |
| `docs/architecture/27-config-compilation-pipeline.md:179` | the compiler's dependency on install state | §7, §11 |
| `docs/architecture/30-failure-policy.md:66` | `contained: false` plus an integrity alarm | §15 |
| `docs/architecture/30-failure-policy.md:369` | how Core is fetched and verified | §5, §6 |
| `docs/architecture/31-security-model.md:26` | binary provenance | §3, §14 |
| `docs/architecture/31-security-model.md:59` | the pin attestation as the Connector→Core trust boundary | §3, §6 |
| `docs/architecture/31-security-model.md:397` | a Core vulnerability: pinned, verified, user-initiated | §8, §11, §16 |
| `docs/architecture/32-secrets-keystore-masking.md:400` | cleanup of the compiled config after an unclean kill | §7, §15 |

## 3. The trust chain, link by link

Four links, each with a different mechanism and a different failure mode. A chain described as "signature
verification" without saying where the key came from is the most common way a document like this hides the real
weakness.

| # | Link | What it guarantees | Mechanism | What breaks it |
|---|---|---|---|---|
| 1 | The extension | the code asking for a proxy is the code that was reviewed | browser-store signing and update review (no NEXORA control) | a side-loaded build; a dev build pointed at a production Connector |
| 2 | The Native Messaging host + its manifest | which local process may receive the user's commands | the manifest is the pinnable identity (`docs/architecture/22-component-contracts.md:83`); the installer writes it under the user's ACL; the browser enforces the sender id | anything that can rewrite the manifest path or the binary it names — see §14 |
| 3 | The pin document | which bytes are permitted, from where, under which key | `packages/schema/pins.schema.json`, shipped inside the Connector's own build | a Connector build with placeholder pins (§4.4), or link 2 |
| 4 | The asset | the file on disk is the pinned file | minisign against `upstream.signaturePubkey`, then `sha256`, then exact `sizeBytes`, then the URL host | an attacker who controls both the asset and the list — which is why `verifySignature` is `const true` |

Links 3 and 4 are the ones NEXORA wrote; links 1 and 2 are inherited, and §14 states plainly what that means for
the threat model. The chain is deliberately **not** a web of trust: there is no third party between the user's
installer and upstream's signing key, and no NEXORA server is consulted, because §1 of
`docs/architecture/21-system-context.md` forbids a cloud component and a server that answers "is this binary
allowed?" is exactly such a component.

Two properties follow that are easy to get wrong:

- **The pin document is not fetched.** It is data that ships with the Connector, so its integrity is link 2's
  problem, not a fifth link. Consequence, stated in §8: a Core upgrade normally means a Connector release, and
  the escape hatch for a user who needs a Core fix sooner is §9's channel, not a silent fetch.
- **`sha256` and the signature are both required because they fail differently.** A hash mismatch catches a
  corrupted download, a truncated file, and a substituted asset *when the list is trustworthy*. A signature
  mismatch catches the case where the list itself was replaced. `packages/schema/pins.schema.json`'s
  `#/properties/verifySignature` says the second is not optional; the allOf guard at its tail makes per-asset
  signatures mandatory if no release key is pinned, so "we only have a hash" cannot be expressed by omission.

## 4. The pin document as an operational object

### 4.1 Shape versus instance

The shape is `packages/schema/pins.schema.json` and it exists in this repository today. The instance that ships —
`connector/testdata/pins.json` (`docs/architecture/20-target-repository-layout.md` §60) — does not: the whole
`connector/` tree is absent, and this is the document's largest `Verified: NO`. What does exist is the conforming
example `packages/schema/examples/pins.xray.json`, which `SEC-6` in
`docs/audit/11-security-findings.md` cites as the real home of the pin *contract* on disk.

### 4.2 What the schema already refuses

Each of these is structurally inexpressible rather than discouraged, which is the only kind of rule that survives
a deadline: one repository (`#/properties/upstream/properties/repository`, `const`); an exact tag with neither
ranges nor `latest` (`…/releaseTag`); URLs restricted to the upstream release host, so a mirror is not a
configuration choice (`#/$defs/asset/properties/url`, with the negative case
`packages/schema/examples/invalid/pins.mirror-url.json`); `sha256` as a 64-hex charset; `sizeBytes` bounded to
1 MiB–100 MiB, which turns "the download was cut off" into a validation failure before execution; `verifiedAt` as
a required stamp; `silentSelfUpdate` and `verifySignature` as constants; and per-asset `os`/`arch`/`libc`
enums with `additionalProperties: false`.

### 4.3 The pinned version bounds the feature set

`docs/architecture/23-unified-server-model.md:76` makes REALITY-over-gRPC unsupported *by the pinned Core*, not
by preference. That is a supply-chain statement with a product consequence: an `IntegrityMismatch`-free upgrade
to a newer tag is also the event that can newly *allow* a combination the compiler had been refusing. So the
compiler's capability decisions and the pin document are coupled, and `core.select`'s and `config.apply`'s
behaviour must never depend on a version the Connector did not verify — which is why `connector.version` reports
`coreVersion`, `pinsVersion` and `buildId` together
(`packages/schema/protocol/commands/connector.version.json#/result/properties`): a support bundle that cannot say
which pinned document produced which bytes cannot diagnose a drift.

### 4.4 Self-consistency, and the release gate

A pin document can be schema-valid and still be wrong, the classic case being a half-updated one: the tag moved,
some asset URLs did not. `scripts/verify.mjs` now refuses that — assertions *every asset URL is the pinned tag
with the pinned file name*, *the declared asset naming is a file this document actually pins*, *the install root
is a per-user path, never a machine path*, and *the shipped pin keeps a rollback target*. These are cheap and they
catch the failure mode that a human reviewing a five-field diff will miss.

What they cannot catch is the thing `docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md:118-120` marks
`UNVERIFIED`: that the tag and hashes in the current example correspond to any real upstream release. They do not
— the values are placeholders for shape, and no network fetch was performed in this iteration. **Release gate: no
Connector build ships a pin document whose `verifiedAt` was not produced by an actual verification run**, and that
run is the first Connector task, not a Phase-2 polish item. The gate belongs to CI
(`.github/workflows/verify.yml`), and §17 records it as still open because there is no CI job that can reach the
network here.

### 4.5 Pin maintenance is a named, recurring job

Every Core upgrade is a reviewed change containing a fresh `verifiedAt`, a new `pinsVersion`, and a retained
rollback entry (`retainedPreviousVersions`, `packages/schema/pins.schema.json#/properties/retainedPreviousVersions`).
`docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md:73-75` says CI must validate the document against the
schema before the Connector is built; today `npm --prefix packages/schema run verify` does that against the schema
and the §4.4 self-consistency rules, and `check-no-secrets.mjs` covers the artefact-shaped half.

## 5. First run

Twelve steps, in the order that makes a partial failure safe. Each row is the requirement; the failure column is
what the user sees, and every one of them is `Verified: NO — deferred to Phase 2`.

| # | Step | On failure |
|---|---|---|
| 1 | Read the pin document (embedded), resolve the entry for `os`/`arch`/`libc` | no entry → refuse to install and report the platform as unsupported (§10) |
| 2 | Check free space against `sizeBytes` plus the extracted tree | `DiskFull` before any bytes move, not mid-write |
| 3 | HTTPS GET the pinned `url`, nothing else — no redirect following off the release host | `NetworkUnreachable`; the extension keeps working without a Core, in whatever `docs/architecture/30-failure-policy.md` resolution applies |
| 4 | Stream to a temp file under `installRoot`, aborting at `sizeBytes + 1` | a truncated or oversized download dies at the size bound, before hashing |
| 5 | Verify minisign signature against `upstream.signaturePubkey` | `IntegrityMismatch`, refuse, delete the temp file |
| 6 | Verify `sha256` of the temp file | `IntegrityMismatch` — same treatment; step order puts the stronger check first because it is the one that distinguishes a substituted list from a corrupted transfer |
| 7 | Enumerate the archive; refuse entries with absolute paths, `..`, or names outside the expected set | `IntegrityMismatch`; this is the extraction guard, and it runs before anything is written, because a zip is an attacker-controlled directory listing |
| 8 | Extract into a versioned directory, e.g. `<installRoot>/<releaseTag>/` | `DiskFull` / `Internal`; a half-extracted version directory is deleted, never adopted |
| 9 | Apply the owner-only ACL, verify it by reading back the permission bits | refuse: a Core binary that other users can write is not the pinned binary in any meaningful sense |
| 10 | Write the install receipt (tag, `pinsVersion`, file hash, `verifiedAt`, path) | a receipt that cannot be written means step 11 has nothing to compare against; refuse to proceed |
| 11 | Record the version as available, *not* as active | nothing starts here: installing is not running |
| 12 | Let `config.apply` / `core.start` resolve the active version from the receipt (§6) | `KeystoreUnavailable` and `IntegrityMismatch` are the only reasons a start is refused, and both are fail-closed per `docs/architecture/30-failure-policy.md` |

Two ordering decisions in that list are load-bearing and were not obvious:

- **Signature before hash, not after.** Reversed, a substituted asset with a recomputed hash from a tampered list
  would pass step 6 and fail step 5 — the same observable outcome, but the log line says "corrupt download" for a
  compromise. The order makes the diagnosis honest.
- **Nothing starts at install time.** `autoStart` in `packages/schema/connector-settings.schema.json` decides
  whether Core runs when the *Connector* comes up; first-run install does not imply first-run execution, because
  the intent that justifies running is a profile the user selected, and there may not be one yet.

## 6. When verification runs, and what it can catch

| Moment | Check | Cost | Catches |
|---|---|---|---|
| Install (first run, upgrade) | signature, then hash, then size, then traversal | network + one RSA/ed25519 verify + one SHA-256 | every substitution and corruption class |
| Every `core.start` / `core.restart`, before exec | `sha256` of the file being spawned, compared to the install receipt | ~10 MB hashed, tens of milliseconds | post-install tampering by anything that could write the file but not the Connector, disk decay, a partial rollback |
| While running | containment and parent-pid checks (`docs/architecture/22-component-contracts.md` §6) | none | a Core nobody is supervising |
| Never | re-fetching the list, resolving `latest`, trusting "the file exists and is executable" | — | §10 and ADR-0019 rule 7 |

The pre-exec hash is the decision worth arguing for, because it looks like theatre: an attacker who runs as the
user can already rewrite the Connector, and the Connector is what does the checking
(`docs/adr/0011-windows-keystore-dpapi.md:49-54` makes the same concession about DPAPI). It is not theatre for
two reasons. First, it covers the non-adversarial failures — a truncated extraction, a disk fault, an antivirus
that rewrote the file in place, a rollback that half-completed — which are the *common* ways the bytes on disk stop
matching the bytes that were verified. Second, it separates "another process as this user replaced my Core" from
"another *user*, or a machine-wide updater, or a roaming profile copy did", and the second class is the one ADR-0011
chose to defend against. `Verified: NO — Phase 2`, and `LIVE-21` owns it.

What this does **not** buy: a claim that a same-user adversary is stopped. §16 lists it as uncontrolled, and
`docs/architecture/31-security-model.md` §14 keeps it in the residual section rather than in this one.

## 7. Install layout, atomicity, and cleanup

- **Versioned directories, one active pointer.** The switch is a rename of a pointer, not an overwrite of a binary
  in place — a running Windows process cannot have its executable replaced, and on POSIX an in-place write leaves a
  window where the file is neither version. A rename also makes §8's rollback a pointer change plus a receipt write.
- **Owner-only, per-user, never the extension directory.** `installRoot` is `%LOCALAPPDATA%\NEXORA\core` in the
  example (`packages/schema/pins.schema.json#/properties/installRoot` says why: no elevation, user-owned ACL). The
  compiled config lives in the same tree and inherits the same rules — the plaintext exception
  `docs/architecture/32-secrets-keystore-masking.md` §13 depends on that.
- **Extraction is a trust boundary, not a convenience.** Step 7 of §5 is the whole of Zip-Slip: the archive's own
  entry names are attacker-controlled bytes, so the expected set is enumerated from the pin and anything outside it
  is refused before the first write.
- **Receipts, not directory listings.** "Which version is verified" is answered by a receipt written at step 10, so
  a stray directory in the install root — left by an interrupted upgrade, or dropped there by something else — is
  not a candidate for execution. This is what makes "no path runs whatever binary is on disk" (ADR-0019 rule 7)
  operational rather than aspirational.
- **Garbage collection is bounded and explicit.** `retainedPreviousVersions` is the whole retention policy; GC runs
  after a successful switch, never mid-upgrade, and never deletes the version that `core.status` reports as running.
- **Cleanup on failure**: a temp file, a half-extracted version directory, or an unverified receipt is removed on
  the spot. `docs/architecture/32-secrets-keystore-masking.md` §13 flags the residual that survives this: an
  unclean kill can leave the compiled config past its lifetime, because removal is a shutdown step and not a
  guarantee. §17 sends that to `docs/architecture/40-open-decisions.md`.

## 8. Upgrade, and what "user-initiated" has to mean

`silentSelfUpdate` is `const false`
(`packages/schema/pins.schema.json#/properties/silentSelfUpdate`) and the reason is not conservatism: a Core
upgrade changes the bytes that carry the user's traffic, and an automatic change to those bytes is the exact
event ADR-0019 exists to make impossible. The mechanics that make the rule usable rather than merely strict:

1. **The trigger is a user action** in the options UI, not an alarm, not a page load, not a Connector start. The UI
   must not imply a capability it lacks — the brief's constraint, and the reason the upgrade control reports the
   pinned version it has rather than the newest version upstream has, since answering the second question would
   need the network resolution §6 refuses.
2. **The new bytes arrive with a new pin document**, and the pin document ships inside the Connector (§3). So the
   ordinary path is: Connector release → new pins → the user is offered the Core change. A user who needs a Core
   fix before a Connector release has §9, and §17 records that this coupling is a product cost, not a virtue.
3. **Verify-then-switch, never switch-then-verify.** The full §5 sequence runs for the new version while the old
   one remains the active pointer. A failed verification leaves the running Core exactly where it was and answers
   `IntegrityMismatch` — the upgrade attempt is not allowed to degrade a working installation.
4. **The switch is `core.restart`**, which is the only command that can change which verified version executes,
   and it names `IntegrityMismatch`
   (`packages/schema/protocol/commands/core.restart.json#/errors`) for the case where the pointer moved but the
   bytes no longer match the receipt. Restart is a visible interruption: connections drop
   (`docs/architecture/30-failure-policy.md` owns the grace window; ADR-0019's "MUST retain the previous version
   for rollback" is why that window has something to roll back to).
5. **Rollback is a pointer change plus a re-verification**, not a re-download, as long as the retained version is
   present — `retainedPreviousVersions` defaults to 1 and zero is allowed only where the product knowingly gives up
   rollback (`packages/schema/pins.schema.json#/properties/retainedPreviousVersions`).
6. **Interrupted upgrade** — killed between extract and switch: the new version directory exists, the receipt does
   not name it active, the old one still runs. Killed between switch and GC: either the old or the new is fully
   verified and the receipt says which. `LIVE-20` asserts both halves.

One consequence deserves saying rather than hiding: an upgrade is the event that can *unfix* nothing and *change*
behaviour, because the compiler's capability decisions are version-coupled (§4.3). A user who upgrades Core may
find a previously refused combination now accepted. That is the intended direction of travel, but it means "same
extension version, different routing outcomes" is possible, and the UI's version display is the only thing that
makes it diagnosable.

## 9. The `manual` channel, and the air-gapped story

`installChannel` is exactly `pinned` or `manual`
(`packages/schema/connector-settings.schema.json#/properties/core/properties/installChannel`), and its
description contains the sentence this section exists to unpack: *"a binary chosen by path is a binary chosen by
whoever wrote that path."* So `manual` is **not** "run what the user points at". It is the documented offline
route, with the same verification on the other side of the transfer:

1. The pin document travels with the Connector build (it is embedded — §3), so an air-gapped machine has the
   expected hashes without needing network access.
2. The user fetches the asset on a connected machine, from the pinned URL, and moves it by any medium.
3. The Connector runs §5 steps 5–10 on the placed file, in a directory it chose under `installRoot`. The user
   supplies bytes, never a decision about which bytes are permitted.
4. If the placed file does not satisfy the pin, the answer is `IntegrityMismatch` and nothing runs. There is no
   "install anyway".

That leaves the genuinely air-gapped-but-no-pinned-release case — a platform with no entry (§10), or a machine that
needs a Core newer than the embedded pin. The honest answer is that this product does not serve it in Phase 1:
`docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md:71-72` records air-gapped setups as a real
limitation tracked as an open question, and the alternative that would solve it — NEXORA publishing its own signed
builds — is the "build from source" option ADR-0019 rejected and says may return only as a superseding ADR. §17
keeps it open rather than inventing a fourth channel in prose.

## 10. Platform coverage is a refusal, not a fallback

`packages/schema/pins.schema.json#/properties/assets` says an absent platform "is a refusal to run there, not a
fallback", and the current example pins `windows/amd64` and `linux/amd64` with `maxItems` 12 of headroom. Adding
`windows/arm64`, `darwin/arm64`, or `musl` means adding a verified row in a reviewed change, with a real download,
a real signature check and a fresh `verifiedAt` — the schema's `libc` enum admits `musl` so the *shape* is already
expressible, which is exactly the kind of near-miss that tempts someone to fill in a guessed hash. The §4.4
self-consistency assertions catch a mistyped URL; nothing in this repository can catch a fabricated hash, so
`LIVE-02` and the CI gate in §4.4 are the controls, and the residual is a maintainer who lies.

The same rule applies to architecture detection: the Connector resolves the entry from the OS it is running on, and
a host that cannot identify its own `os`/`arch`/`libc` triple must refuse rather than pick "the closest" entry.
Choosing the nearest binary is how an arm64 machine ends up executing an `amd64` image through an emulation layer
that silently changes which code paths run.

## 11. The second self-update door: what the compiled config must not make reachable

`silentSelfUpdate: false` governs the *Connector's* behaviour about *itself*. Xray has its own capabilities, and a
config that enables them reopens the door ADR-0019 closed, from inside the thing being pinned. Three of them are
named here because each is a real Xray configuration feature and each would be reachable through a config the
Connector wrote:

- **Geo-data auto-update.** Xray can download `geoip`/`geosite` assets at runtime. Silent, unsigned, and driven by
  the binary we pinned — the definition of what rule 6 forbids. So the compiler MUST NOT emit a config that lets
  Core fetch bytes at runtime; if geo-based routing is ever enabled (`docs/audit/07-routing-engine.md:415` records
  that `geosite:` currently leaks in as a magic string the UI cannot express), the data files arrive as **pinned
  assets with their own `sha256`, `sizeBytes` and `verifiedAt`**, updated by the same user-initiated flow as §8.
  `#/$defs/asset`'s `os`/`arch` shape does not describe a data file, so that needs a schema change and a
  decision — §17.
- **`browserForwarder`.** A config field that makes Core call *back into the browser* to originate connections.
  That is ADR-0004's forbidden control surface arriving from the opposite direction: a local component the browser
  did not register, opening a channel the extension process is asked to service. The compiler must not emit it,
  and the recorded fixtures must not contain it — asserted by *no recorded config gives Core a door back into the
  browser or its own control API* over `packages/schema/fixtures/protocol/`.
- **`api` / `stats`.** Core's own gRPC control surface and stats server. Both are "a local control endpoint", which
  is the thing `docs/architecture/21-system-context.md` refuses for the Connector and refuses for Core for the same
  reason: reachability by every local process. Health and metrics come from `connector.health`, `core.status` and
  `diagnostics.get` instead, under the sender identity model of `docs/architecture/31-security-model.md`.

None of this is enforceable from `packages/schema` alone — the compiled config has no schema of its own
(`docs/architecture/27-config-compilation-pipeline.md` explains why: its shape is Xray's, not ours), so §11 is a
requirement on the compiler plus a fixture-level guard, and the guard's coverage is exactly as wide as the
fixtures. `Verified: NO — Phase 2` for a compiler that could violate it; green for the fixture scan.

## 12. `IntegrityMismatch`, and the other thing it means

One code, two distinct failures
(`packages/schema/protocol/error-taxonomy.json#/codes` — "Binary hash or chunk reassembly hash differs from the
pin. Core refuses to start"). That double duty is deliberate — both mean "the bytes you have are not the bytes you
were promised", and both are non-retryable security-layer refusals — but the remediation differs enough that a UI
which conflates them will mislead:

| Context | Meaning | Who acts | What they do |
|---|---|---|---|
| install / `core.start` / `core.restart` | the asset is not the pinned asset | maintainer, effectively | re-run the fetch from the pinned URL; if it still mismatches, the pin is wrong and that is a reviewed change |
| `profile.import` / chunked frames | the reassembled import does not hash to the declared `contentHash` | the user | re-select the file; nothing about their binary is in question |
| `config.apply` | the config hash presented does not match what the Connector compiled | the extension | re-compile and re-apply; see the drift path in `docs/architecture/26-command-surface.md` |
| `subscription.refresh`, `server.test` | a Core the Connector no longer believes in is what would answer the call | fail closed | refuse rather than probe with an unverified engine |

It is `retryable: false` in every one of those rows, and that is the property the user is protected by: an
integrity refusal must not become a retry loop that ends with a different binary being accepted. Commands that can
hit it all name it (`config.apply`, `core.start`, `core.restart`, `profile.import`, `server.test`,
`subscription.refresh` — six), which `scripts/verify.mjs` reads as part of the per-command error discipline in
`docs/architecture/26-command-surface.md`.

## 13. The antivirus and SmartScreen friction

`docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md:76-78` moves the unsigned-binary problem from "inside
the extension" to "downloaded at first run", and the pin records `authenticodeSubject` per asset precisely so the
observation is in data rather than in a support ticket
(`packages/schema/pins.schema.json#/$defs/asset/properties/authenticodeSubject`). What that means operationally:

- Upstream Xray assets are not Authenticode-signed, so Windows Defender SmartScreen and enterprise AV react to
  reputation, not to our verification. The minisign check is invisible to them.
- **Quarantine is a runtime integrity event.** AV removing or rewriting the file makes the pre-exec hash (§6) fail,
  which is the *correct* outcome: `IntegrityMismatch`, Core does not start, the proxy decision fails closed per
  `docs/architecture/30-failure-policy.md`. The wrong outcome — which an implementation that treats a missing file
  as "just start again" produces — is traffic going direct.
- `docs/architecture/22-component-contracts.md:143-144` routes "an unbootable Core (bad binary, AV quarantine)"
  here; the answer is that both are the same observable state at the point of use, and the *diagnosis* is what
  differs — which is why `diagnostics.get` carries a `versions` section and `connector.version` reports
  `pinsVersion` plus `coreVersion` (`packages/schema/connector-settings.schema.json#/properties/telemetry`
  governs which sections are included by default).
- A product decision this document states but does not make: whether NEXORA ever ships its own Authenticode-signed
  wrapper build of Core to remove the friction. That is the "become a software publisher" alternative ADR-0019
  rejected, and §17 leaves it open with the reason: it would need a code-signing certificate with a cost, a key
  custody story, and a rebuild pipeline for a component we do not fork.

## 14. What verifies the Connector

ADR-0019 pins Core and says nothing about the Connector's own bytes, so the question lands here. The honest
answer, in three parts.

**There is no fourth link.** The Connector's integrity is the installer's, and the installer's integrity is the
download page's plus the OS's. NEXORA does not control a store for native binaries, and the alternative — the
Connector verifying itself — is circular. `buildId` in `connector.version`
(`packages/schema/protocol/commands/connector.version.json#/result/properties`) exists so that a support exchange
can establish which build is running, which is *detection*, not *proof*.

**The manifest is the pinnable object.** `docs/architecture/22-component-contracts.md:83` states it, and the
consequence is that the install/uninstall story is a story about a registration under the user's ACL: the installer
writes the Native Messaging host manifest naming the binary path and the extension id it may talk to; uninstall
removes the registration and leaves the download cache and the keystore to be handled explicitly — the keystore
survives by design (`docs/adr/0011-windows-keystore-dpapi.md:59-60`,
`docs/architecture/32-secrets-keystore-masking.md` §7), the Core binaries do not need to survive because they are
re-derivable from a pin plus a fetch, and neither is silently deleted by an uninstall that the user did not ask to
be destructive. The installer must not register a service or LaunchAgent
(`docs/architecture/21-system-context.md:198`), which keeps the whole footprint inside the user's session and the
user's write permissions.

**The residual, stated.** Anything that can write the host binary or its manifest can answer every command in
`docs/architecture/26-command-surface.md` with fabricated data — including "the Core is the pinned Core" — and no
amount of pinning below that link helps, because the pin is checked by the process an attacker would replace. The
controls that remain are the ones `docs/architecture/31-security-model.md` already counts: sender identity enforced
by the browser rather than by the host, loopback-only inbounds, typed commands with no shell, and the extension
refusing to trust a Connector whose `protocolVersion`/`pinsVersion` it does not recognise. Detection, not proof, and
labelled as such — `Verified: NO — Phase 2` for even that much, since none of it exists as code yet.

## 15. Failure modes, and where each resolution lives

| Failure | Observable | Code | Resolution owner |
|---|---|---|---|
| No entry for this platform | install refuses before any network call | `UnsupportedCombination` at the boundary, refused locally otherwise | this document §10 |
| Download truncated / oversized | size bound fails before hashing | `NetworkUnreachable` → `IntegrityMismatch` | §5 |
| Signature mismatch | nothing extracted | `IntegrityMismatch`, non-retryable | §5, §12 |
| Hash mismatch at start | Core does not spawn | `IntegrityMismatch`; the proxy decision follows ADR-0009 | §6, `docs/architecture/30-failure-policy.md` |
| Archive contains a traversal path | refused at enumeration | `IntegrityMismatch` | §5, §7 |
| Disk full mid-install | temp file and partial tree removed | `DiskFull` | §5 step 2, §7 |
| AV quarantine | pre-exec hash fails, or file missing | `IntegrityMismatch` / `CoreNotRunning` | §13 |
| Interrupted upgrade | old version remains active | none — the state is correct by construction | §8.6 |
| `contained: false` with an unmanaged Core | the supervision check fails | surfaces as the integrity alarm `docs/architecture/30-failure-policy.md:66` combines with `coreDeath` | §6, §14 |
| Pin document half-updated in git | CI refuses the build | not a runtime code — a build failure | §4.4 |

The line that matters most is the last pair: an integrity problem is never resolved by *choosing a different
binary*, and it is never resolved by the failure policy's grace windows either. `docs/architecture/30-failure-policy.md`
holds traffic; §12 says the code is non-retryable. A reader who takes "grace" from one document and "retry" from
the other would conclude the Core can be replaced automatically, and it cannot.

## 16. Threat, control, check

| Threat | Control | Check | Status |
|---|---|---|---|
| Upstream release substituted (compromised host or asset) | minisign against a pinned key, checked before the hash | assertion *every asset URL is the pinned tag with the pinned file name* + a real verification run in CI | `LIVE-02` `Verified: NO — Phase 2` |
| Pin list replaced along with the asset | the pin ships inside the Connector, so replacing it means replacing the host — link 2, not link 4 | `buildId`/`pinsVersion` reporting | §14; detection only |
| Truncated or corrupted download | exact `sizeBytes` bound before hashing, `sha256` after | schema bounds asserted by `npm --prefix packages/schema run verify` | contract green, runtime `LIVE-02` |
| Malicious archive entries | enumeration and expected-set refusal before any write | `LIVE-21` | `Verified: NO — Phase 2` |
| Post-install file replacement by a lower-privilege or non-user actor | pre-exec hash against the receipt | `LIVE-21` | `Verified: NO — Phase 2` |
| Silent automatic upgrade | `silentSelfUpdate: false` as a `const`, plus §8's user-initiated flow | *the shipped pin keeps a rollback target* + schema `const` | contract green |
| Core reaching back into the browser, or opening its own control port | §11's compiler prohibitions | *no recorded config gives Core a door back into the browser or its own control API* | fixture scan green; **needs a compiler to be real** |
| Unsigned-data assets fetched by Core at runtime (geo data) | §11: no runtime fetch; pin data files if geo routing is ever enabled | none yet | §17 open item |
| Same-user malware replacing the host or its manifest | nothing | nothing | **uncontrolled, stated as residual** |
| A binary that is not on the pinned list running anyway | receipts, not directory listings (§7); `installChannel` has no "path" value | `LIVE-20`, `LIVE-21` | `Verified: NO — Phase 2` |

## 17. Invariants, amendments, open items

**Invariants.**

1. The Connector executes exactly one kind of binary: one whose hash and signature were verified against an
   embedded pin document, recorded in a receipt it wrote.
2. There is no code path in which "a binary exists at this path" is a reason to run it.
3. Verification order is signature, then hash, then size, then archive enumeration — so a substituted list is
   reported as a substitution.
4. Install never starts Core, and upgrade never switches before the new version is fully verified.
5. An absent platform entry is a refusal; a guessed hash is a reviewed change, not a runtime fallback.
6. The compiled config grants Core no capability to fetch bytes, dial the browser, or open a control surface.
7. `IntegrityMismatch` is non-retryable in every context, including the chunk-reassembly one.
8. The Connector's own integrity is not provable from inside the chain, and this document says so instead of
   inventing a link.

**Amendments made by this document:**

1. `scripts/verify.mjs` gained the pin self-consistency block — *every asset URL is the pinned tag with the pinned
   file name*, *the declared asset naming is a file this document actually pins*, *the install root is a per-user
   path, never a machine path*, *the shipped pin keeps a rollback target* — and the config-reach scan, *no recorded
   config gives Core a door back into the browser or its own control API* with its non-vacuity guard *a recorded
   maskedConfig exists for that scan to read*. The scan found nothing to fix and is here to keep §11 from becoming
   prose. `npm --prefix packages/schema run verify` is green at 900 checks.
2. `docs/verification/live-matrix.md` gained `LIVE-20` (upgrade is user-initiated, fully re-verified, reversible,
   and an interrupted upgrade leaves the previous Core runnable) and `LIVE-21` (install safety: signature-then-hash
   order, traversal refusal, receipt-gated execution, and the pre-exec hash at every start), so §6–§8 point at rows
   rather than at intentions; `docs/verification/live-matrix.md` is where they are executed.
3. §11's three prohibitions are recorded as requirements on `docs/architecture/27-config-compilation-pipeline.md`'s
   emit list, which is where they will be implemented; this document owns the rule and the reason.

**Open, and deliberately not decided here** (each goes to `docs/architecture/40-open-decisions.md`):

- the coupling of Core upgrades to Connector releases (§8.2) — the user-facing cost of putting the pin inside the
  host, and whether a signed detached pin update is worth its complexity;
- geo-data: whether `geosite`/`geoip` routing is a product feature at all
  (`docs/audit/07-routing-engine.md`), and if so, how data files are pinned when `#/$defs/asset` describes
  executables;
- air-gapped-and-newer-than-the-pin (§9), and the publisher alternative ADR-0019 rejected;
- Authenticode signing of a NEXORA-built Core (§13), with its certificate-custody and rebuild-pipeline cost;
- cleanup of the compiled config after an unclean kill, shared with
  `docs/architecture/32-secrets-keystore-masking.md` §13;
- Linux and macOS provenance, which need their own ADRs the way the keystore does
  (`docs/adr/0011-windows-keystore-dpapi.md:64-65`), since "owner-only ACL" means three different things.

Trace: ADR-0002, ADR-0003, ADR-0004, ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0011, ADR-0013,
ADR-0019 · Contract: `packages/schema/pins.schema.json`, `packages/schema/examples/pins.xray.json`,
`packages/schema/connector-settings.schema.json`, `packages/schema/protocol/error-taxonomy.json`,
`packages/schema/protocol/commands/core.start.json`, `packages/schema/protocol/commands/core.restart.json`,
`packages/schema/protocol/commands/config.apply.json`, `packages/schema/protocol/commands/connector.version.json`,
`packages/schema/protocol/commands/diagnostics.get.json` · See also:
`docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md`, `docs/audit/11-security-findings.md` (SEC-6),
`docs/audit/10-manifest-packaging-distribution.md`, `docs/architecture/20-target-repository-layout.md`,
`docs/architecture/21-system-context.md`, `docs/architecture/22-component-contracts.md`,
`docs/architecture/26-command-surface.md`, `docs/architecture/27-config-compilation-pipeline.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/31-security-model.md`,
`docs/architecture/32-secrets-keystore-masking.md`.
