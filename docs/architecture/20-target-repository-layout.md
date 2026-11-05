# 20 — Target repository layout

> Purpose: fix the directory contract of NEXORA — which roots exist, who owns each, what each MUST contain, why
> the five legacy `omega-*` projects are not moved, the artifact chain the layout must not break, what a
> cut-over commit is, which names are frozen, and which machine check enforces each invariant. Everything here is
> either enforced by a command that runs today or is demoted to a `Verified: NO` row of
> `docs/architecture/39-verification-matrix.md`.

Status: Accepted — inherits the two open decisions of `docs/PROJECT-STATE.md` §9 (lockfile policy,
`.circleci/config.yml`); neither changes this layout, both are flagged where they touch it.

## 1. Reading rules

A `path:line` citation means the file and the line were opened and read in this iteration, and
`node tools/check-doc-evidence.mjs` enforces it — a missing file, an out-of-range line, or a range whose first
line carries no characters all fail the check (the `outOfRange`, `unresolved` and `blankTarget` reports in
`tools/check-doc-evidence.mjs`). Paths that exist in no
form yet — `connector/**`, `nexora/**`, `packages/protocol/**`, `packages/state/**`, `packages/i18n/**`,
`packages/ui/**` — are cited WITHOUT a line number, and are pre-registered as future paths inside the checker
itself (`FUTURE_PREFIX` in `tools/check-doc-evidence.mjs`): that is this layout existing as code. Every behavioural
assertion with no automated or live check carries `Verified: NO — deferred to Phase 2` inline and belongs to
`docs/architecture/39-verification-matrix.md`.

## 2. The tree

```
E:/NEXORA
├─ .env.example, .gitattributes, .gitignore              EXISTS   root dotfiles added by this iteration (§11)
├─ .npmrc ×5                                             EXISTS   one per legacy project, none at root (§8)
├─ .github/workflows/release.yml                         EXISTS   legacy tag-driven publisher (§10)
├─ .github/workflows/verify.yml                          PLANNED  PR-blocking, non-tagging counterpart
├─ .circleci/config.yml                                  EXISTS   dead, cannot pass; pending PROJECT-STATE §9.3
├─ docs/{README,PROJECT-STATE}.md, audit/, adr/, verification/  EXISTS   Phase 0 + Phase 1 output
├─ docs/architecture/                                    PARTIAL  20-40; this file is 20
├─ packages/schema/                                      EXISTS   the only executable artefact of Phase 1
├─ packages/{protocol,state,i18n,ui}/                    PLANNED  four more independent npm roots (§3)
├─ connector/                                            PLANNED  own Go module; nothing Go-shaped outside it
├─ nexora/extension/                                     PLANNED  own npm root: Vite + Svelte 5 + TypeScript
├─ tools/*.mjs                                           EXISTS   three dependency-free guards (§13)
├─ dist/                                                 GENERATED  never tracked (`.gitignore:4`)
└─ omega-build/ omega-pac/ omega-target/ omega-web/
   omega-target-chromium-extension/ omega-locales/       FROZEN   positions and names (§4)
```

## 3. Directory ownership and required contents

| Root | Exists | Owner / role | MUST contain | MUST NOT contain |
|---|---|---|---|---|
| `omega-build/` | yes | the *only* driver of the legacy chain | the hub config (`omega-build/Gruntfile.coffee:2`) and the four npm scripts (`omega-build/package.json:12-15`) | any NEXORA source; any wrapper invoked by NEXORA tooling |
| `omega-pac/`, `omega-target/` | yes | legacy engine and storage/sync layer, read-only for this iteration | unchanged; the two demoted lint rules are the only edits (`omega-pac/grunt/coffeelint.coffee:11`) | new NEXORA profile types; new sync-eligible keys |
| `omega-web/` | yes | legacy AngularJS options + popup | `src/coffee/options.coffee` (the real bootstrap page; there is **no** `omega-web/src/options.coffee`), `src/omega/app.coffee`, `src/partials/**`, `src/popup/**`, `lib/themes/**` | NEXORA components mounted inside AngularJS DOM |
| `omega-target-chromium-extension/` | yes | legacy packaging; the only zip producer | `overlay/manifest.json`, `overlay/manifest-firefox.json`, `grunt/compress.coffee`, the inline `manifest` task (`omega-target-chromium-extension/Gruntfile.coffee:5-11`) | anything compiled from `connector/` |
| `omega-locales/` | yes | the single translation source (ADR-0017) | 27 `*.po` trees, 7 of which reach `_locales` (`omega-target-chromium-extension/grunt/po2crx.coffee:4-17`) | a second UI catalog as source of truth |
| `docs/` | yes | the deliverable of this iteration | `audit/`, `adr/`, `architecture/`, `verification/` | assertions with no named check |
| `packages/schema/` | yes | normative data contracts; the executable Phase-1 gate | 13 schemas, `protocol/command-allowlist.json` (19 methods), 19 command docs, `examples/` + `examples/invalid/`, `fixtures/protocol/*.jsonl`, `scripts/verify.mjs` | a second copy of any allowlist or entity shape |
| `packages/protocol/` | no | TypeScript projection of `packages/schema/protocol/` for both sides of the pipe | the frame codec, generated envelope/error types, the dispatch table skeleton the Go host is tested against (`docs/architecture/25-native-messaging-protocol.md`, `docs/architecture/26-command-surface.md`) | its own method list — `packages/schema/protocol/command-allowlist.json:12` stays normative |
| `packages/state/` | no | framework-agnostic typed client of the *extension-side* RPC contract, incl. the MV3 cold-start backoff ported verbatim and a form-state layer (ADR-0016) | `subscribe(fn)` compatible with `svelte/store` but not depending on Svelte | any import from `omega-*/src/**` |
| `packages/i18n/` | no | `.po` → typed UI catalog compiler plus the `locale → browser lang` mapping table (ADR-0017) | its own `package.json` (a root of its own) | a runtime dependency on `po2crx` |
| `packages/ui/` | no | design tokens and presentational primitives below the framework (ADR-0016 reversal clause) | the 164 base16 + 3 default theme contract (`docs/audit/08-ui-i18n-and-themes.md:192-203`) | theme CSS copied out of `omega-web/lib/themes/` |
| `connector/` | no | the Go module: `cmd/nexora-host` (framing, auth, spawn only), `cmd/nexora-connector`, `internal/**` for the Core supervisor, config compiler, keystore, prober, and `testdata/pins.json` (ADR-0019) | one `go.mod`; loopback-only bind assertion inside `core.start` (ADR-0006) | a compiled binary in git; the Xray executable; any file the extension zip reads |
| `nexora/extension/` | no | the new extension tree and new UI: `src/ui/**`, the `nexora-options.html` / `nexora-popup.html` entry documents, the Vite config that builds them | only JSON-serialisable boundaries with legacy code (ADR-0016) | any `import` of `omega-*` except types derived from `packages/schema` (§6) |
| `tools/` | yes | repository-level guards usable before any install | zero-dependency `.mjs` scripts (ADR-0018 rule 6) | an npm dependency; a `package.json` |

Verified: NO — deferred to Phase 2: none of the cells above for a root that does not exist yet, nor the
ownership split itself, has a checker; the only layout facts code enforces today are the future-path allowlist
(`FUTURE_PREFIX` in `tools/check-doc-evidence.mjs`) and the contract layer (`packages/schema/package.json:8`).

## 4. Why `omega-*` does not move

The plan assumed "at least 15" name-coupling sites; Phase 0 measured **23 distinct sites across 11 files**
(`docs/audit/01-repository-and-build-graph.md:199-226`), each a literal `omega-*` path inside build
configuration with no test underneath it — `omega-web` and `omega-target-chromium-extension` hold zero test
files:

| Coupling | Cited sites | What a move breaks |
|---|---|---|
| Hub build graph | `omega-build/Gruntfile.coffee:2`, `:7`, `:10`, `:22` | a renamed directory silently stops being a build-graph node; the catch-all `../*/Gruntfile.*` silently *adds* any new sibling that grows a Gruntfile |
| npm script chains | `omega-build/package.json:12`, `:13`, `:15` | `cd ../omega-pac` etc. fail as "file not found", not as a build error |
| Relative `file:` deps | `omega-target/package.json:26`, `omega-web/package.json:21`, `omega-target-chromium-extension/package.json:25-27` | install fails; these are also why `npm link` semantics must stay untouched |
| `npm link` chains | `omega-pac/package.json:31`, `omega-target/package.json:32`, `omega-web/package.json:24`, `omega-target-chromium-extension/package.json:34` | the `node_modules/omega-*` symlinks below stop existing |
| **Prebuilt bundle at the upstream project root** | `omega-web/grunt/copy.coffee:4`, `omega-target-chromium-extension/grunt/copy.coffee:9-10`, alias + load-time resolution at `omega-target-chromium-extension/grunt/browserify.coffee:36`, `:43` | the subtle one: the *package name*, not the folder, resolves the symlink, and `require.resolve` runs while the Gruntfile loads — so a missing bundle aborts the whole hub build |
| Cross-project copy | `omega-target-chromium-extension/grunt/copy.coffee:4`, `:26-27` | `cwd: '../omega-web/build'` and the repo-root `COPYING`/`AUTHORS` read |
| Watch graphs and locales | `omega-web/grunt/watch.coffee:10`, `omega-target-chromium-extension/grunt/watch.coffee:9`, `:12`, `:15`, `omega-target-chromium-extension/grunt/po2crx.coffee:4-17` | dev loops stop rebuilding and the seven `../omega-locales/...` paths break, silently |
| Dead CI cache keys | `.circleci/config.yml:14` through `:34`, plus `:83` | six checksums of `omega-*/package.json`; the file already cannot pass |

Getting it wrong is measured, not theoretical: before the lint demotion `grunt release` exited **0** while
producing a 352-file zip with no `js/omega_pac.min.js`, which loads as a blank options page
(`docs/audit/99-baseline-report.md:151-155`). Normative: the six legacy roots and their package names MUST NOT
be moved, renamed, split or merged, and a tidy monorepo requires a superseding ADR naming all 23 sites as
migration work plus an artifact assertion proving the move
(`docs/adr/0002-keep-omega-directories-additive-layout.md:30-44`).

Verified: NO — deferred to Phase 2: nothing asserts the eleven coupled files still sit at their cited paths;
the tripwire is indirect — deleting one breaks a citation in `node tools/check-doc-evidence.mjs`.

## 5. Artifact chain (recorded verbatim; the layout must not alter it)

`npm run deps` → `npm run dev` → `npm run build` (grunt-hub: omega-pac → omega-target → omega-web →
omega-target-chromium-extension) → `npm run release` (the `manifest` task filters `downloads` and writes
`tmp/{chromium,firefox}/manifest.json`; `compress` writes `dist/chromium-release.zip` +
`dist/firefox-release.zip`) — the chain as agreed at `docs/PROJECT-STATE.md:135-140`.

Line-level anchors, all read: `deps` and `dev` are `omega-build/package.json:12`, `:13`; `build` is bare `grunt`
(`:14`) driven by grunt-hub (`:9`); `release` is `cd ../omega-target-chromium-extension && grunt release` (`:15`)
and its alias is `['manifest', 'compress']` (`omega-target-chromium-extension/grunt/aliases.coffee:10`).

Two corrections this document carries forward, because the chain sentence above is imprecise: (a) the
`manifest` task is **not** in `omega-build` and no `manifestFilter` exists there — it is registered inline in
the extension project (`omega-target-chromium-extension/Gruntfile.coffee:5-11`); (b) its `downloads` filter
(`omega-target-chromium-extension/Gruntfile.coffee:7`, `:10`) is a **no-op on today's manifests** — neither
`omega-target-chromium-extension/overlay/manifest.json:39-48` nor
`omega-target-chromium-extension/overlay/manifest-firefox.json:38-47` declares `downloads`
(`docs/audit/10-manifest-packaging-distribution.md:69-72`), so it MUST NOT be cited as evidence that
permissions are reduced at build time.

Green baseline (Ladder A) that any layout change MUST reproduce: `dist/chromium-release.zip` +
`dist/firefox-release.zip` at **353 file entries / 5 999 353 B uncompressed / 252 `lib/` entries / 7
`_locales`**, with `js/omega_pac.min.js` present (`docs/audit/99-baseline-report.md:164-170`). `bower install`
works on this host, clean-runner reproducibility is UNVERIFIED: no lockfile is tracked (`.gitignore:3`,
`docs/audit/02-toolchain-decay-and-risk.md:135`) and two Bower deps use the dead `git://` transport
(`omega-web/bower.json:37`, `:43`).

## 6. Cut-over commit: definition

A **cut-over commit** is exactly one commit whose subject begins `CUT-OVER:` and which flips a user-visible
default from legacy to NEXORA. It MUST: (1) carry no other logical change and be revertible as that one commit
with Ladder A still green; (2) touch only `nexora/**`, `tools/**`, `docs/**` and the frozen surface of §12 —
for the UI flip that is precisely `omega-target-chromium-extension/overlay/manifest.json:23`
(`action.default_popup`) with `:30` (`options_page`) and `:32` (`options_ui.page`), plus the Firefox
equivalents `omega-target-chromium-extension/overlay/manifest-firefox.json:23` and `:32`; (3) name the parity
rows it closes in `docs/architecture/38-compatibility-matrix.md`, all green; (4) re-run and re-record the
artifact-content assertion of §5 — an exit code is never the gate; (5) be preceded by an ADR, because a design
document MAY describe a cut-over and MUST NOT perform one. Removing legacy source is a later, separate commit
class (ADR-0016, popup-last). Verified: NO — deferred to Phase 2: nothing checks commit granularity, the label,
or the ADR-before-flip.

## 7. Import boundary for `nexora/**`

Nothing under `nexora/**` MAY import from `omega-*`, except types *derived from* `packages/schema`
(`docs/PROJECT-STATE.md:142`, `docs/adr/0002-keep-omega-directories-additive-layout.md:37`,
`docs/adr/0018-workspace-roots-and-no-root-package-json.md:43-45`). Derived means generated from or written
against a schema — never a re-export of a legacy module: `omega-pac` exports are not a stable contract, the
schemas are. The legacy engine MUST NOT gain a dependency in the reverse direction either; both sides point at
`packages/schema`. Practical form: the Vite config of `nexora/extension` MUST declare no `resolve.alias` and no
`file:` dependency aimed at an `omega-*` directory, and `nexora/extension` MUST NOT enter the hub `submodules`
array (`omega-build/Gruntfile.coffee:2`), because the catch-all glob at `omega-build/Gruntfile.coffee:7` would
sweep it into `grunt watch` the moment it grew a `Gruntfile.*`. Verified: NO — deferred to Phase 2: no checker
exists; the planned one is an import-specifier scan of `nexora/**` and `packages/**` run from
`.github/workflows/verify.yml`.

## 8. Roots: why there is deliberately no root `package.json`

Rules 1-7 of `docs/adr/0018-workspace-roots-and-no-root-package-json.md` are normative; cited below as `:nn`.

- **No root `package.json`, `go.work` or runner** (`:33`). Not tidiness: with a root manifest any tool that
  defaults to `npm test` / `npm run build` at the root can reach the legacy grunt graph, and npm hoisting
  rewrites the `node_modules` layout the prebuilt-bundle reads of §4 depend on. The root is verifiably bare —
  `find . -maxdepth 1 -name package.json` returns nothing, and the only manifests outside vendored and build
  trees are the five `omega-*/package.json` plus `packages/schema/package.json`.
- **Each root is independent and self-describing** (`:35-38`): `npm --prefix packages/schema run verify`
  (`packages/schema/package.json:8` — two `ajv` dependencies, no workspace parent), `connector/go.mod` alone,
  `nexora/extension` its own npm root. A root MUST declare its own dependencies and MUST NOT rely on hoisting
  from a sibling.
- **Legacy keeps one driver** (`:39-42`): `cd omega-build && npm run deps|dev|build|release`
  (`omega-build/package.json:12-15`); CI MUST invoke per-root commands explicitly in that style (`:53-56`),
  which is already the shape of `.github/workflows/release.yml:24` (`working-directory: 'omega-build'`).
- **`.npmrc` is per-root policy** (`:46`): five `legacy-peer-deps=true` files, one inside each `omega-*`
  project, none at the root — a root `.npmrc` would change resolution for every future npm root at once, the
  exact accident ADR-0018 exists to prevent. **`tools/*.mjs` stay dependency-free** (`:50`) so they run before
  any install exists.

Inherited open decision: with no lockfile tracked (`.gitignore:3`) each root answers reproducibility for
itself, and `packages/schema` currently answers UNVERIFIED on a clean machine (`docs/PROJECT-STATE.md` §9.2).
Verified: NO — deferred to Phase 2: no guard fails when a root `package.json`, `go.work` or `.npmrc` appears,
and `connector/` cannot be built or vetted on this host because Go, Docker and Python are absent.

## 9. The Connector is never inside the extension archive

ADR-0019 rule 1 (`docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md:32-34`) states it; what makes it
true is structure, not a blocklist. `compress` is the only zip producer
(`omega-target-chromium-extension/grunt/aliases.coffee:10`): the Chromium target archives `cwd: 'build'`,
`src: ['**', '!manifest.json', '!manifest-firefox.json']`
(`omega-target-chromium-extension/grunt/compress.coffee:9-13`) plus the generated `cwd: 'tmp/chromium/'`,
`src: 'manifest.json'` (`:15-17`) into `../dist/chromium-release.zip` (`:6`), and Firefox mirrors that
(`:22`, `:25-28`, `:31-32`). `build/` is filled only by `omega-target-chromium-extension/grunt/copy.coffee` —
the `omega-web` build tree (`:4-6`), the two upstream bundles (`:9-10`), `overlay/**` (`:19-23`) and the root
`COPYING`/`AUTHORS` (`:26-27`). No task reads `connector/**`, and none could start to without editing the
`submodules` array, which is ADR-0002 territory. The `!manifest.json` / `!manifest-firefox.json` exclusions at
`:10` and `:26` exist *because* `copy:overlay` drops both raw templates into `build/`: the packaged manifest is
the generated one, never the template.

The check that would catch a violation — list the archive, fail on any path outside the known set or on a PE or
ELF magic byte — **does not exist**. The 353-file and `omega_pac.min.js` figures were obtained by re-measuring
by hand (`docs/audit/99-baseline-report.md:164-170`), and `.github/workflows/release.yml:31-34` checksums the
zip it just built without inspecting it. **Verified: NO — deferred to Phase 2**, where it becomes a step of
`.github/workflows/verify.yml` plus a row of `docs/architecture/39-verification-matrix.md`. `.gitignore:4`
covers the zip and `tools/check-no-secrets.mjs` covers credentials; neither recognises an executable.

## 10. Renaming artifacts: specified now, performed at cut-over

`dist/nexora-chromium-<ver>.zip` is a cut-over-level change (§6) touching two places **together**, never
piecemeal: the archive paths `omega-target-chromium-extension/grunt/compress.coffee:6` and `:22`, and the
publisher that hard-codes the basenames — `.github/workflows/release.yml:33-34` (checksum inputs) with `:40-43`
(upload list). Because `.gitignore:4` matches `*-release.zip`, the replacement ignore pattern MUST land in the
same commit class or the renamed zips become committable. Store continuity does not depend on the filename
(`docs/audit/10-manifest-packaging-distribution.md:194-196`); it depends on manifest identity and the version
line (ADR-0015), and the in-product comparison at
`omega-target-chromium-extension/src/coffee/background.coffee:41` is why the line continues. Verified: NO —
deferred to Phase 2: nothing pins these names today.

## 11. Additive overlay: what may be added, and where

A NEXORA change MUST add files in a new root, in `docs/`, `tools/`, `.github/workflows/`, or the root dotfiles,
and MUST NOT edit a legacy file unless a build break demands it — the only legacy edits of this iteration are
the two lint demotions (`omega-pac/grunt/coffeelint.coffee:11` and the chromium twin). Existing proof the
pattern works: `tools/verify-vendor.mjs`, `tools/check-no-secrets.mjs`, `tools/check-doc-evidence.mjs`,
`.gitattributes:23-25` (Go line rules declared before any Go exists), `.env.example:11` (empty value whose
comment names the LIVE row it gates), `.gitignore:8-10` (`.env`, `.env.*`, `!.env.example`).
`.github/workflows/verify.yml` is the planned addition of the same class: a **new** workflow, PR-blocking and
non-tagging, leaving `.github/workflows/release.yml` untouched — that one fires only on a `v*` tag
(`.github/workflows/release.yml:6`) and MUST NOT be reused as a verification gate.

## 12. What may NOT move or be renamed without an ADR

| Frozen name | Where it lives | Why it is frozen |
|---|---|---|
| `omega-pac`, `omega-target`, `omega-web`, `omega-target-chromium-extension`, `omega-build`, `omega-locales` (dirs **and** package names) | §4 | 23 build-config couplings; the symlink reads resolve by package name (ADR-0002 rule 7) |
| `+` / `-` envelope prefixes | `omega-pac/src/profiles.coffee:111-118` (the `+`-only iteration), `omega-target/src/default_options.coffee:3` | `+` means "selectable profile"; a `+`-keyed NEXORA entity leaks into `availableProfiles`, rule pickers and the generated PAC (ADR-0012) |
| `__ruleListOf_<name>` | `omega-web/src/omega/app.coffee:13`, `omega-target-chromium-extension/src/module/upgrade.coffee:30`, as data at `packages/schema/envelope-keys.schema.json:90` and `packages/schema/examples/envelope-keys.nexora.json:86` | the attachment convention; renaming it orphans every existing switch-profile rule body |
| `-nexora.*` namespace patterns | `packages/schema/examples/envelope-keys.nexora.json:7` | append-only public contract; a rename is a schema migration (ADR-0014), not a refactor |
| Extension identity | `omega-target-chromium-extension/overlay/manifest-firefox.json:51` (`gecko.id`), the Chromium store identity derived without a `key` field | update continuity for existing installs (ADR-0015) |
| Packaged `version` format | `omega-target-chromium-extension/overlay/manifest.json:4`, compared at `omega-target-chromium-extension/src/coffee/background.coffee:41` | the wipe trap; a `1.x` restart deletes user data on the second update |
| `dist/chromium-release.zip`, `dist/firefox-release.zip` | `omega-target-chromium-extension/grunt/compress.coffee:6`, `:22` + `.github/workflows/release.yml:33-34`, `:40-43` | §10; renaming is a cut-over |
| UI route state names (`ui`, `general`, `io`, `builtin`, `theme`, `profile`, `about`) | `omega-web/src/omega/app.coffee:48-76` — **seven** states, not the six quoted in the plan and in `docs/PROJECT-STATE.md` §6 | bookmarks and the parity matrix key off them |
| The 19 command method names | `packages/schema/protocol/command-allowlist.json:12`, asserted at `packages/schema/scripts/verify.mjs:186`, cross-checked into `docs/architecture/26-command-surface.md` at `packages/schema/scripts/verify.mjs:196-210` | both implementations switch on them; a rename is a protocol-version change |

## 13. File-level invariants this layout protects, and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| No credential-shaped literal in a tracked file; no committed `.env`; no PEM | `node tools/check-no-secrets.mjs` (rules at `:6-18`, fixture/doc path exemptions at `:33`, entropy gate at `:55-60`, self-test dispatch at `:128`) | **GREEN** — 13/13 self-test cases |
| Every `lib/...` vendor reference resolves against `omega-web/lib/` or `build/lib/` | `node tools/verify-vendor.mjs` (roots at `:21-24`, non-zero exit on a miss at `:130-145`) | **GREEN** — and it is what replaced the plan's vendoring step |
| Every `path:line` citation in `docs/` still resolves | `node tools/check-doc-evidence.mjs` (pattern `:33`, future-path exemptions `:19-28`) | **GREEN for this file** |
| Contract layer: schemas compile, examples valid, invalid rejected, 109 frames replay, allowlist is exactly 19 with one pre-hello method | `npm --prefix packages/schema run verify` (`packages/schema/package.json:8`; 19-method assertion `packages/schema/scripts/verify.mjs:186`, pre-hello `:187-192`) | **GREEN** — 707 checks |
| NEXORA keys never start with `+`; `secretBearing ⇒ ¬syncEligible`; `headers` stripped from sync | the prefix rejection at `packages/schema/scripts/verify.mjs:360-364`, the namespace loop at `packages/schema/scripts/verify.mjs:359-372` and the `headers` assertion at `packages/schema/scripts/verify.mjs:373-377` | **GREEN** — this is SEC-1 expressed as data |
| Legacy build graph still reachable from `omega-build` | Ladder A (`omega-build/package.json:12-15`) run by hand, recorded in `docs/audit/99-baseline-report.md` | Verified: NO — deferred to Phase 2 (no automated chain run in this iteration; `.github/workflows/verify.yml` is the intended owner) |
| The six legacy directories have not moved | nothing | Verified: NO — deferred to Phase 2 |
| `nexora/**` and `packages/**` import nothing from `omega-*` | nothing | Verified: NO — deferred to Phase 2 (§7) |
| No `package.json` / `go.work` / `.npmrc` at the repository root | nothing | Verified: NO — deferred to Phase 2 (§8) |
| No connector, host shim, or Xray bytes inside `dist/*.zip` | nothing (manual measurement only) | Verified: NO — deferred to Phase 2 (§9) |
| Extension identity and version-line continuity | nothing in-repo; store behaviour is external | Verified: NO — deferred to Phase 2, `docs/verification/live-matrix.md` LIVE-14 |
| Line endings and binary classification stay as declared | `.gitattributes` itself; `git add --renormalize` is forbidden by the session rules | Verified: NO — deferred to Phase 2 |

Every `Verified: NO` row above is owned by `docs/architecture/39-verification-matrix.md`; no assertion here is
complete until its row there is green.

Trace: ADR-0002, ADR-0012, ADR-0015, ADR-0016, ADR-0017, ADR-0018, ADR-0019, ADR-0020 · Contract:
`packages/schema/protocol/command-allowlist.json`, `packages/schema/examples/envelope-keys.nexora.json`,
`packages/schema/envelope-keys.schema.json`, `packages/schema/scripts/verify.mjs`,
`packages/schema/package.json` · See also: `docs/PROJECT-STATE.md`,
`docs/audit/01-repository-and-build-graph.md`, `docs/audit/02-toolchain-decay-and-risk.md`,
`docs/audit/05-storage-sync-and-migration.md`, `docs/audit/08-ui-i18n-and-themes.md`,
`docs/audit/10-manifest-packaging-distribution.md`, `docs/audit/99-baseline-report.md`,
`docs/architecture/21-system-context.md`, `docs/architecture/26-command-surface.md`,
`docs/architecture/37-ui-architecture.md`, `docs/architecture/38-compatibility-matrix.md`,
`docs/architecture/39-verification-matrix.md`, `docs/architecture/40-open-decisions.md`
