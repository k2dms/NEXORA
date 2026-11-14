# 20 — Target repository layout

> Purpose: fix the directory contract of NEXORA — which roots exist, who owns each, what each MUST contain, why
> the five legacy `omega-*` projects are not moved, the artifact chain the layout must not break, what a
> cut-over commit is, which names are frozen, and which machine check enforces each invariant. Everything here is
> either enforced by a command that runs today or is demoted to a `Verified: NO` row of
> `docs/architecture/39-verification-matrix.md`.

Status: Accepted — inherits the two open decisions of `docs/PROJECT-STATE.md` §9 (lockfile policy,
`.circleci/config.yml`, deleted 2026-11-14 by PROJECT-STATE §9.3); neither changes this layout, both are flagged where they touch it.

## 1. Reading rules

A `path:line` citation means the file and the line were opened and read in this iteration, and
`node tools/check-doc-evidence.mjs` enforces it — a missing file, an out-of-range line, or a range whose first
line carries no characters all fail the check (the `outOfRange`, `unresolved` and `blankTarget` reports in
`tools/check-doc-evidence.mjs`). Paths that exist in no
form yet — `nexora/**`, `packages/i18n/**`, `packages/ui/**` — are
cited WITHOUT a line number, and are pre-registered as future paths inside the checker itself (`FUTURE_PREFIX`
in `tools/check-doc-evidence.mjs`); `connector/**` left that list on 2026-11-05, `packages/protocol/**` on
2026-11-10 and `packages/state/**` on 2026-11-11 (`docs/PROJECT-STATE.md` §4.53, §4.56). Every
behavioural assertion with no automated or live check carries `Verified: NO — deferred to Phase 2` inline and belongs to
`docs/architecture/39-verification-matrix.md`.

## 2. The tree

```
E:/NEXORA
├─ .env.example, .gitattributes, .gitignore              EXISTS   root dotfiles added by this iteration (§11)
├─ .npmrc ×5                                             EXISTS   one per legacy project, none at root (§8)
├─ .github/workflows/release.yml                         EXISTS   legacy tag-driven publisher (§10)
├─ .github/workflows/verify.yml                          EXISTS   PR-blocking, non-tagging counterpart; written and first run 2026-11-05, green (§11)
├─ .circleci/config.yml                                  DELETED  dead, cannot pass; removed 2026-11-14 by PROJECT-STATE §9.3
├─ docs/{README,PROJECT-STATE}.md, audit/, adr/, verification/  EXISTS   Phase 0 + Phase 1 output
├─ docs/architecture/                                    PARTIAL  20-40; this file is 20
├─ packages/schema/                                      EXISTS   the only executable artefact of Phase 1
├─ packages/{protocol,state,i18n,ui}/                    PARTIAL  protocol + state EXIST; i18n + ui PLANNED (§3)
├─ connector/                                            EXISTS   own Go module; nothing Go-shaped outside it
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
| `packages/protocol/` | yes — `docs/PROJECT-STATE.md` §4.53, 2026-11-10 | TypeScript projection of `packages/schema/protocol/` for both sides of the pipe | the frame codec, generated envelope/error types, the dispatch table skeleton the Go host is tested against (`docs/architecture/25-native-messaging-protocol.md`, `docs/architecture/26-command-surface.md`) | its own method list — `packages/schema/protocol/command-allowlist.json:12` stays normative |
| `packages/state/` | partial — `docs/PROJECT-STATE.md` §4.56, 2026-11-11 | framework-agnostic typed client of the *extension-side* RPC contract, incl. the MV3 cold-start backoff ported verbatim and a form-state layer (ADR-0016) | the ConnectorClient is written (injected port, per-method deadlines, concurrency windows, jittered reconnect, reconcile by hash); `subscribe(fn)` compatible with `svelte/store` and the form-state layer are not | any import from `omega-*/src/**` |
| `packages/i18n/` | no | `.po` → typed UI catalog compiler plus the `locale → browser lang` mapping table (ADR-0017) | its own `package.json` (a root of its own) | a runtime dependency on `po2crx` |
| `packages/ui/` | no | design tokens and presentational primitives below the framework (ADR-0016 reversal clause) | the 164 base16 + 3 default theme contract (`docs/audit/08-ui-i18n-and-themes.md:192-203`) | theme CSS copied out of `omega-web/lib/themes/` |
| `connector/` | yes | the Go module: `cmd/nexora-host` (framing, auth, spawn only), `cmd/nexora-connector`, `cmd/nexora-installer` (manifest, registration, allowlist), `internal/**` for the Core supervisor, config compiler, keystore, prober, and `testdata/pins.json` (ADR-0019) | one `go.mod`; loopback-only bind assertion inside `core.start` (ADR-0006) | a compiled binary in git; the Xray executable; any file the extension zip reads |
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
(`docs/audit/99-baseline-report.md` §6). Normative: the six legacy roots and their package names MUST NOT
be moved, renamed, split or merged, and a tidy monorepo requires a superseding ADR naming all 23 sites as
migration work plus an artifact assertion proving the move
(`docs/adr/0002-keep-omega-directories-additive-layout.md:30-44`).

Verified: YES — `node tools/check-layout-guards.mjs` reads the enumeration above out of
`docs/audit/01-repository-and-build-graph.md` §7 and refuses a cited file that has left the tree, a cited line that has
drifted past its file, and a coupling literal edited out of the line that carried it; it counts 22 rows over 12 distinct (23 and 13 before §9.3 deleted `.circleci/config.yml`, whose row left the table with its file)
files where §7's own headline counts 11, and the guard prints what it counts rather than the headline. It also checks
that the six roots above are still directories, that the five of them which are npm packages still answer to their
directory name in `package.json` — ADR-0002 makes the *name*, not the folder, the thing the symlinks resolve through —
and that every `../omega-locales/**.po` path the `po2crx` task reads still resolves. `VER-05` closed on 2026-11-06 and is
in `packages/schema/verification.json#/retired`; the indirect tripwire in `node tools/check-doc-evidence.mjs` still
exists, and is no longer the only one.

## 5. Artifact chain (recorded verbatim; the layout must not alter it)

`npm run deps` → `npm run dev` → `npm run build` (grunt-hub: omega-pac → omega-target → omega-web →
omega-target-chromium-extension) → `npm run release` (the `manifest` task filters `downloads` and writes
`tmp/{chromium,firefox}/manifest.json`; `compress` writes `dist/chromium-release.zip` +
`dist/firefox-release.zip`) — the chain as agreed in `docs/PROJECT-STATE.md` §3.

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
`_locales`**, with `js/omega_pac.min.js` present (`docs/audit/99-baseline-report.md` §6) — and reproducible:
probe-4 rebuilt the whole chain on 2026-11-05 and every one of those assertions held again (§11). The zips'
sha256 did **not** move because the content changed but because the archive records each entry's build
timestamp, so a rebuild cannot repeat a hash; that is why `verify.yml` prints it beside the recorded value
instead of asserting it. `bower install` also succeeds with `omega-web/bower_components/` **removed**: bower
normalises the declared `git://github.com/…` endpoints to `https://github.com/….git` before it shells out to
git, so the transport `omega-web/bower.json:37` and `:43` name is never handed to git at all
(`docs/audit/raw/probe-5-bower-cold-install.txt`, `docs/audit/02-toolchain-decay-and-risk.md` §1.1). Raw
`git ls-remote git://…` still times out here, which is why `.github/workflows/verify.yml` keeps its
`insteadOf` step — defence in depth for a caller that does pass the literal URL to git, not the thing that
makes the build possible. The contract roots track their lockfiles since §9.2 answered
(`packages/**/package-lock.json`, un-ignored 2026-11-14); the legacy roots' lockfiles stay ignored — their
reproducibility is the audit's §6 question, not this rule's (`docs/audit/02-toolchain-decay-and-risk.md` §6). The empty-cache question that measurement closed: 21 of the 26
components came from this host's Bower cache in the 2026-11-05 rebuild, and the runner build of the same commit, on
an image with no cache, produced a zip carrying the same 252 `lib/` entries (§12).

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
class (ADR-0016, popup-last). Verified: YES — the `VER-06` step of `.github/workflows/verify.yml` reads every
subject in the range for its label and scopes a `CUT-OVER:`; granularity and the ADR-before-flip stay review-only.

The label vocabulary the guard accepts is `feat`, `fix`, `docs`, `build`, `test`, `refactor`, `chore`, `perf` and
`ci` — each optionally scoped, optionally `!`-marked — plus the `CUT-OVER:` prefix above and git's own `Merge `
and `Revert ` subjects. `ci` is the label for `.github/**`. This list was written into
`.github/workflows/verify.yml` as the VER-06 step before it was stated in the document it cites, so the first run
of that workflow on a real runner rejected this repository's own `ci(verify): …` commit — the defect the run found
was the guard enforcing a rule its source document did not state. The rule is stated now; the vocabulary is
declared here and the step reads it from here, and every execution of that workflow since has passed it — runs
`42020617681`, `42025558933`, `42027072602`, `42029092102` (whose `contracts` cells all succeeded before the run was
cancelled downstream), `42029386377`, `42033189223` and `42046629162`, each on `ubuntu-latest` and two
`windows-latest` cells (`docs/audit/99-baseline-report.md` §12).

## 7. Import boundary for `nexora/**`

Nothing under `nexora/**` MAY import from `omega-*`, except types *derived from* `packages/schema`
(`docs/PROJECT-STATE.md` §3, `docs/adr/0002-keep-omega-directories-additive-layout.md:37`,
`docs/adr/0018-workspace-roots-and-no-root-package-json.md:43-45`). Derived means generated from or written
against a schema — never a re-export of a legacy module: `omega-pac` exports are not a stable contract, the
schemas are. The legacy engine MUST NOT gain a dependency in the reverse direction either; both sides point at
`packages/schema`. Practical form: the Vite config of `nexora/extension` MUST declare no `resolve.alias` and no
`file:` dependency aimed at an `omega-*` directory, and `nexora/extension` MUST NOT enter the hub `submodules`
array (`omega-build/Gruntfile.coffee:2`), because the catch-all glob at `omega-build/Gruntfile.coffee:7` would
sweep it into `grunt watch` the moment it grew a `Gruntfile.*`. Verified: YES for the watcher's reach —
`node tools/check-layout-guards.mjs` refuses a `Gruntfile.*` anywhere outside the six legacy roots, new roots included,
and refuses any glob in the two `watch.coffee` files that leaves its own project except the declared
`../omega-locales/` or names a new root; `VER-07` closed on 2026-11-06 and is in
`packages/schema/verification.json#/retired`. The import-specifier scan of `nexora/**` and `packages/**` is the
`VER-02` step of `.github/workflows/verify.yml`, green on three runner images — and green now over a `nexora/**`
that holds real code: the vacuousness that kept the row open is gone, and `VER-02` closed 2026-11-14
(`docs/PROJECT-STATE.md` §4.71).

## 8. Roots: why there is deliberately no root `package.json`

Rules 1-7 of `docs/adr/0018-workspace-roots-and-no-root-package-json.md` are normative; cited below as `:nn`.

- **No root `package.json`, `go.work` or runner** (`:33`). Not tidiness: with a root manifest any tool that
  defaults to `npm test` / `npm run build` at the root can reach the legacy grunt graph, and npm hoisting
  rewrites the `node_modules` layout the prebuilt-bundle reads of §4 depend on. The root is verifiably bare —
  `find . -maxdepth 1 -name package.json` returns nothing, and the only manifests outside vendored and build
  trees are the five `omega-*/package.json` plus one per new root (`packages/schema`, `packages/protocol`, `packages/state`).
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

§9.2 answered (2026-11-14): the contract roots track their lockfiles, so each installs on pinned versions;
what a clean machine still cannot prove is that the resolution matches somewhere else — that is the CI run's
job (`docs/PROJECT-STATE.md` §9.2).
Verified: YES — the root-bare rule of `node tools/check-layout-guards.mjs` refuses any of the five paths named above
(`package.json`, `package-lock.json`, `go.work`, `go.mod`, `.npmrc`) at the repository root. Until 2026-11-06 the same
five were a shell loop in `.github/workflows/verify.yml`, green on three runner images; the tool is where that check
lives now, so `VER-04` closed on 2026-11-06 and is in `packages/schema/verification.json#/retired`.
`connector/` builds, vets and tests on this host since 2026-11-05 (Go 1.27.3) and in the `connector` job.

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
ELF magic byte — **exists**, and has run green on a runner: the `VER-08` step of
`.github/workflows/verify.yml` reads the first four bytes of every tracked file, and the `check_zip` of its
`legacy-chain` job lists each artifact the build just produced before it is uploaded. The 353-file and
`omega_pac.min.js` figures were measured by hand (`docs/audit/99-baseline-report.md` §6, reproduced by probe-4 in
§11) and are asserted there now; `.github/workflows/release.yml:31-34` still checksums the zip without inspecting it.

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
`.github/workflows/verify.yml` is the addition of the same class, landed on 2026-11-05 and run there the same day:
`docs/audit/99-baseline-report.md` §12 tabulates ten of its runs — the first two red on defects in the file itself,
six green, one cancelled by the
file's own `concurrency` block, and a third red whose only failing job is the connector's, on one test that polled a
counter for ~1 s of wall clock instead of waiting for the bytes the peer had already issued. It counts the table's
rows and not the file's executions because a record of this
workflow is itself a commit that runs it; §12 states the rule that decides when a further run is a finding. It is a
**new** workflow, PR-blocking and
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
| The 18 command method names | `packages/schema/protocol/command-allowlist.json:12`, asserted at `packages/schema/scripts/verify.mjs` "the command surface is exactly eighteen methods", cross-checked into `docs/architecture/26-command-surface.md` by `packages/schema/scripts/verify.mjs` "docs/architecture/26-command-surface.md names the same set" | both implementations switch on them; a rename is a protocol-version change |

## 13. File-level invariants this layout protects, and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| No credential-shaped literal in a tracked file; no committed `.env`; no PEM | `node tools/check-no-secrets.mjs` (the four rules in its header comment, the exemptions in `FIXTURE_PATH_RE` and `VENDORED_PATH_RE`, the shape bar in `credentialShaped` and `isLiteralSecret`, the self-test dispatch in `runSelftest`) | **GREEN** — 13/13 self-test cases |
| Every `lib/...` vendor reference resolves against `omega-web/lib/` or `build/lib/` | `node tools/verify-vendor.mjs` (`VENDOR_ROOTS` names both roots; a non-empty `missing` exits non-zero) | **GREEN** — and it is what replaced the plan's vendoring step |
| Every citation in `docs/` still means what it says | `node tools/check-doc-evidence.mjs` (`CITATION_RE` is the pattern, `FUTURE_PREFIX` exempts target paths, `selfCite` and `mutableCite` refuse line cites into files that move, `POINTER_RE` resolves every `path#/pointer`, `DOCNAME_RE` every named document, `staleLabel` every quoted harness assertion, and the claims mode reads each `\| Cited by \|` table by the law its own rows state — the cited line must name the document whose table it is, with `--selftest` proving the check can still say NO) | **GREEN for this file** — 52 of the 57 line cites in those tables (the other 5 are into `docs/adr/`, which the rule exempts), and `--selftest` reports the one drifted row of its four fixtures |
| Contract layer: schemas compile, examples valid, invalid rejected, every fixture frame replays against the envelope, allowlist is exactly 18 with one pre-hello method | `npm --prefix packages/schema run verify` (`packages/schema/package.json:8`; 19-method assertion `packages/schema/scripts/verify.mjs` "the command surface is exactly eighteen methods", pre-hello `packages/schema/scripts/verify.mjs` "nothing is dispatchable before negotiation except connector.hello") | **GREEN** — the run prints its own counts, and no assertion here depends on what it prints |
| NEXORA keys never start with `+`; `secretBearing ⇒ ¬syncEligible`; `headers` stripped from sync | the prefix rejection at `packages/schema/scripts/verify.mjs` "namespace stays out of the profile prefix: ${ns.entity}", the cross-rule and schema-existence checks at `packages/schema/scripts/verify.mjs` "secret-bearing namespace is not sync-eligible: ${ns.entity}" and "namespace value schema exists: ${ns.entity}" and the `headers` assertion at `packages/schema/scripts/verify.mjs` "syncExcludedValueKeys strips headers" | **GREEN** — this is SEC-1 expressed as data |
| Legacy build graph still reachable from `omega-build` | Ladder A (`omega-build/package.json:12-15`) run by hand, recorded in `docs/audit/99-baseline-report.md`; the `legacy-chain` job of `.github/workflows/verify.yml` automates install → link → hub build → tests → release → artifact inspection, and did all of it on three runner images on 2026-11-05, green, at the pinned floors (`docs/audit/99-baseline-report.md` §12) | Verified: NO — deferred to Phase 2 |
| The six legacy directories have not moved | the `legacy-roots` and `locale-chain` rules of `node tools/check-layout-guards.mjs` (each of the six is a directory, the five that are npm packages answer to their own directory name, and every `../omega-locales/**.po` the `po2crx` task reads resolves) | **GREEN** (§4) |
| `nexora/**` and `packages/**` import nothing from `omega-*` | the `VER-02` step of `.github/workflows/verify.yml` — import-specifier, side-effect-import and `file:` greps over both roots; green on three runner images and green over the real extension sources on this host (`docs/PROJECT-STATE.md` §4.71) | **GREEN** (§7) |
| No `package.json` / `go.work` / `.npmrc` at the repository root | the `root-bare` rule of `node tools/check-layout-guards.mjs` — five root paths including `package-lock.json` and `go.mod`; the same five were the `VER-04` shell loop of `.github/workflows/verify.yml` until 2026-11-06, green on three runner images, and the step now runs the script (`docs/audit/99-baseline-report.md` §12) | **GREEN** (§8) |
| No connector, host shim, or Xray bytes inside `dist/*.zip` | the `VER-08` step's `check_zip`, which asserts the exact top-level path set, the 353-entry count and the bundle's presence, and ran against the artifacts each of the three runner builds produced, green (`docs/audit/99-baseline-report.md` §12) | **GREEN** (§9) |
| Extension identity and version-line continuity | nothing in-repo; store behaviour is external | Verified: NO — deferred to Phase 2, `docs/verification/live-matrix.md` LIVE-14 |
| Line endings and binary classification stay as declared | `.gitattributes` itself, plus the `VER-10` step (`git diff --check` over the range and `git ls-files --eol` over the index); `git add --renormalize` is forbidden by the session rules | **GREEN** |

Every `Verified: NO` row above is owned by `docs/architecture/39-verification-matrix.md`; no assertion here is
complete until its row there is green; a row whose check has gone green is deleted here and there, not re-labelled.

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
