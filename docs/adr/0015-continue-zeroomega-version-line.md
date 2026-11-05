# ADR-0015: NEXORA continues ZeroOmega's version line (3.6.x, then 4.x) under the same extension ID

Status: Accepted
Date: 2026-11-04

## Context

Rebranding a browser extension looks like a naming decision. In this codebase it is a data-persistence
decision, because the inherited update path destroys user data for any version number below `3.3.0`.

`omega-target-chromium-extension/src/coffee/background.coffee:32-47` is `upgradeMigrateFn`, registered on
`chrome.runtime.onInstalled` at `omega-target-chromium-extension/src/coffee/background.coffee:60-64`. Its
logic: on `reason === 'update'`, if the new version compares greater than the previous one
(`omega-target-chromium-extension/src/coffee/background.coffee:41`) **and** `3.3.0` compares greater than
the new version (`omega-target-chromium-extension/src/coffee/background.coffee:42`), then
`chrome.storage.sync.clear()`, `chrome.storage.local.clear()` and `idbKeyval.clear()` run at
`omega-target-chromium-extension/src/coffee/background.coffee:44-46`. The guard is a legacy one-off: it
exists to reset data written before the `3.3.0` format change, for users upgrading from very old builds.

The arithmetic, made explicit, for the tempting "start NEXORA at 1.0.0":

| Release event | `currentVersion` | `previousVersion` | line 41 `current > previous` | line 42 `3.3.0 > current` | Result |
|---|---|---|---|---|---|
| Fresh install 1.0.0 | `1.0.0` | -- | reason is `install`, not `update` | -- | no wipe |
| 3.5.2 -> 1.0.0 (store update) | `1.0.0` | `3.5.2` | false (a downgrade) | -- | no wipe |
| 1.0.0 -> 1.1.0 (second update) | `1.1.0` | `1.0.0` | true | true | **profiles, sync config and IndexedDB gone** |

So a restarted `1.x` line is not detected on the release that looks dangerous; it detonates on the *first
NEXORA-to-NEXORA update*, which is also usually the first release carrying the Connector -- the worst
possible moment to lose every profile.

Under the chosen scheme the same table never satisfies the second guard: `3.3.0 > 3.6.0` is false, and
`3.3.0 > 4.0.0` is false. Continuing the numbers is the fix that requires no code change.

## Decision

1. NEXORA MUST continue ZeroOmega's version line. Releases that ship the new UI alongside the legacy
   engine, and every pre-Connector release, are `3.6.x`. The cut-over release -- Connector required, new
   UI primary -- is `4.0.0`. Versions MUST NOT decrease, and MUST NOT restart at `0.x` or `1.x`.
2. NEXORA MUST NOT publish any version below `3.3.0`, and SHOULD treat `3.5.2` (the current manifest value
   at `omega-target-chromium-extension/overlay/manifest.json:4`) as the floor the next release must exceed.
3. The same extension identity MUST be kept: the existing Chrome Web Store and Edge listings, and the
   Firefox add-on identity `suziwen1@gmail.com` at
   `omega-target-chromium-extension/overlay/manifest-firefox.json:51`. Store auto-update, ratings, install
   base and -- decisively -- "one proxy engine per browser" all depend on it.
4. Pinning that identity is by `manifest.key`, which is the intended mechanism and is **currently absent**
   from both manifests. Adding it MUST be a single deliberate, one-time, one-way change: the key value MUST
   correspond to the already-published extension ID, and it MUST land before the first NEXORA-branded
   release, because after publication the ID cannot be re-pinned without becoming a different extension.
5. `upgradeMigrateFn` MUST NOT be edited as part of this decision. Its guard is cited, not blessed:
   changing it silently alters upgrade behaviour for existing users, and it is on the list of files
   `docs/audit/12-what-must-not-change.md` keeps untouched pending a test.
6. Version numbering is a contract, and MUST be recorded as such wherever releases are cut: renaming the
   artifacts from `dist/chromium-release.zip` to `nexora-chromium-<ver>.zip` is a cut-over-level change in
   the release workflow (its upload steps live in `.github/workflows/release.yml`), and is tracked
   separately from this ADR rather than done here.

## Consequences

- Existing users receive NEXORA as an ordinary update. No re-install instructions, no lost profiles, no
  orphaned listing, and no two proxy extensions fighting over `chrome.proxy.settings`.
- The `3.6.x` line is a genuine dual-track period: legacy UI plus NEXORA overlay (ADR-0002, ADR-0016) must
  be shippable and revertible independently.
- The `4.0.0` boundary now carries meaning and MUST NOT be used for cosmetics; a `4.x` release implies the
  Connector exists.
- Marketing pressure to re-launch at `1.0.0` is answered by a data-loss table, not by taste. Any future
  change to this scheme requires a new ADR that supersedes this one and MUST name the guard above plus a
  plan to make it inert with a test first.
- Because the version string is now load-bearing, a release process MUST refuse to publish a version below
  the floor; today nothing enforces that (see Verification).

## Alternatives considered

- **Restart at `1.0.0` as a fresh project.** Rejected: the table above. It wipes user data on the second
  update, and separately strands the listings.
- **Fix `upgradeMigrateFn` first, then restart at `1.0.0`.** Rejected for this iteration: the guard's
  condition is also the only mechanism that resets pre-`3.3.0` data for ancient upgraders, there is no test
  for either branch, and removing it does not recover the store identity, the rating history or the
  auto-update path. It may return as part of a `4.x` cleanup with tests.
- **Publish a second, separate NEXORA extension and deprecate ZeroOmega.** Rejected: two extensions both
  claiming `chrome.proxy.settings` and both holding `storage.session` state produce
  `levelOfControl` contention; users must export/import by hand; the profile format then forks in practice
  even though the schemas say otherwise.
- **Keep `3.5.x` and append build metadata.** Rejected: store version comparison ignores build metadata, so
  the update would not be offered at all.

## Evidence

- VERIFIED -- `omega-target-chromium-extension/src/coffee/background.coffee:32-47`: the reason/version
  guards and the three `clear()` calls; `:41` is `currentVersion > previousVersion`, `:42` is
  `'3.3.0' > currentVersion`.
- VERIFIED -- `omega-target-chromium-extension/src/coffee/background.coffee:60-64`: the same function is
  wired to `onInstalled`, so it runs on every store update.
- VERIFIED -- `omega-target-chromium-extension/overlay/manifest.json:4` is `"version": "3.5.2"`; neither
  manifest declares a `key` field, and `:51` of `omega-target-chromium-extension/overlay/manifest-firefox.json`
  is the Firefox add-on id.
- INFERRED -- "a `1.x` line wipes on the second update" follows the guards and the semver comparator
  reading; it was not reproduced in a browser, and reproduction requires two signed builds.
- INFERRED -- store auto-update continuity across the `3.6.x`/`4.0.0` boundary: standard behaviour for the
  same listing, not something this host can verify.
- UNVERIFIED -- the exact CWS rule for pinning `manifest.key` to an already-published ID. Must be confirmed
  against store documentation before rule 4 is executed.

## Verification

- Existing check: `node tools/check-doc-evidence.mjs` resolves the citations above; the wipe trap and this
  ADR are both on the record in `docs/audit/05-storage-sync-and-migration.md` and
  `docs/PROJECT-STATE.md` section 2.
- Verified: NO -- deferred to Phase 2: a release-gate assertion "published `manifest.version` > `3.5.2`,
  not `< 3.3.0`, and monotonic against the last release", to be added to `.github/workflows/verify.yml`;
  and the two-step `1.0.0 -> 1.1.0` browser reproduction of the wipe, which needs signed artifacts.
