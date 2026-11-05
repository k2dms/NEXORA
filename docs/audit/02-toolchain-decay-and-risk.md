# 02 — Toolchain decay and risk

> Purpose: date-stamp the legacy toolchain and runtime deps with line-level evidence, prove the
> reproducibility gaps (no `engines`, no tracked lockfile, empty test globs, POSIX `TZ=` prefix),
> assess whether the two CI configurations can pass today, and give the NEXORA risk verdict: what must
> not be touched versus what is a real hazard on a clean runner. Phase-0 audit only.

Tags: **VERIFIED** (file read, says so), **INFERRED** (deduced; no run/CI observed), **UNVERIFIED**
(assumed). Host facts (Node 24.22.1, npm 11.19.1, git 2.56.0; Go/Docker/Python absent; `npm config get
script-shell` → `null` so npm scripts run via `cmd.exe`; `bower install` works here) are taken from
`docs/PROJECT-STATE.md` §1 and not re-probed.

---

## 1. Toolchain and runtime dependency age

Every version string below was read from the file and line cited.

| Dependency | Pinned range | Where | Evidence | Note |
|---|---|---|---|---|
| Grunt core | `~0.4.1` | `omega-build` | `omega-build/package.json:8` | **VERIFIED** — 0.4.x line, 2013-era major |
| Grunt core | `^0.4.5` | every leaf project | `omega-pac/package.json:11` | **VERIFIED** (same in target/web/chromium) |
| grunt-cli | `~1.2.0` | `omega-build` | `omega-build/package.json:7` | **VERIFIED** |
| grunt-hub | `^0.7.0` | `omega-build` | `omega-build/package.json:9` | **VERIFIED** |
| Bower | `~1.8.4` | `omega-build` | `omega-build/package.json:6` | **VERIFIED** — registry deprecated |
| coffee-script | `^1.7.1` / `^1.8.0` | pac / target | `omega-pac/package.json:8`, `omega-target/package.json:8` | **VERIFIED** |
| coffeeify | `^0.7.0` | pac/target/chromium | `omega-pac/package.json:9` | **VERIFIED** |
| minifyify | `^4.1.1` | pac/target/chromium | `omega-pac/package.json:19` | **VERIFIED** — v4 major, unmaintained |
| uglify-js | `^2.4.15` | pac runtime dep | `omega-pac/package.json:24` | **VERIFIED** — v2, superseded by terser |
| chai | `~1.9.1` | pac/web/chromium | `omega-pac/package.json:7` | **VERIFIED** |
| chai | `^1.10.0` | target | `omega-target/package.json:7` | **VERIFIED** — target differs from the `~1.9` line elsewhere |
| sinon | `^1.12.2` | target | `omega-target/package.json:19` | **VERIFIED** |
| sinon-chai | `^2.6.0` | target | `omega-target/package.json:20` | **VERIFIED** |
| grunt-mocha-test | `~0.11.0` | all four | `omega-pac/package.json:16` | **VERIFIED** |
| grunt-contrib-jade | `^0.12.0` | web | `omega-web/package.json:15` | **VERIFIED** — `jade`→`pug` rename upstream |
| grunt-contrib-less | `^0.11.4` | web | `omega-web/package.json:16` | **VERIFIED** |
| grunt-autoprefixer | `^1.0.1` | web | `omega-web/package.json:9` | **VERIFIED** — v1, current major is far higher |
| grunt-contrib-compress | `^0.12.0` | chromium | `omega-target-chromium-extension/package.json:15` | **VERIFIED** |
| AngularJS (bower) | `~1.8.2` | web | `omega-web/bower.json:24` | **VERIFIED** — EOL framework |
| angular-ui-router | `~0.2.18` | web | `omega-web/bower.json:31` | **VERIFIED** — 0.2.x, ui-router 1.x exists |
| Bootstrap | `~3.3.2` | web | `omega-web/bower.json:32` | **VERIFIED** — Bootstrap 3 |

### 1.1 Bower `git://` dependencies (named, cited)

- **VERIFIED** `angular-spectrum-colorpicker`: `git://github.com/suziwen/angular-spectrum-colorpicker.git#main`
  at `omega-web/bower.json:37`.
- **VERIFIED** `zero-dependencies` (the fork): `git://github.com/zero-peak/zero-dependencies.git#main`
  at `omega-web/bower.json:43`.

Both use the `git://` (git protocol) transport, not `https://`. `git://` is commonly blocked by firewalls
and is unauthenticated; the `#main` ref is a moving branch, so these two are not content-pinned.
**INFERRED** risk (the URLs are read verbatim; the transport behaviour is not observed here).

### 1.2 Bower component count (correction to the plan)

The plan said "~18 bower components". Measured on this tree: `omega-web/bower.json` declares **20**
direct dependencies (`omega-web/bower.json:24-44`) and `omega-web/bower_components/` holds **26**
installed directories (the extra six — jquery, jquery-ui, ladda, spectrum, spin.js, tether — are pulled
transitively). **VERIFIED** by listing the directory and counting the dependency block. True count
reported to the orchestrator: **20 declared / 26 installed**.

---

## 2. `engines` is absent everywhere

**VERIFIED** No `package.json` in the repository declares an `engines` field, and none pins a Node version.
A search for `"engines"` and `"node"` across every `package.json` (all five projects + `packages/schema`)
returns no match. Consequently nothing stops Node 24 (or any future major) from being used — which is
exactly why the legacy chain runs here (Ladder A, `docs/PROJECT-STATE.md` §1.1) and equally why a
maintainer gets no warning when it eventually does not. None of the five manifests — for example
`omega-build/package.json:3` — carries an `engines` key or a Node pin.

---

## 3. `version: "0.0.1"` in every project

**VERIFIED** All five project manifests still carry the placeholder `0.0.1`, as does the bower manifest:

- `omega-build/package.json:3`, `omega-pac/package.json:3`, `omega-target/package.json:3`,
  `omega-web/package.json:3`, `omega-target-chromium-extension/package.json:3`, `omega-web/bower.json:3`.

The real extension version lives in `overlay/manifest.json` (read by the `manifest` task,
`omega-target-chromium-extension/Gruntfile.coffee:6`), not in any `package.json` — so npm versions are
decoys. **VERIFIED** for the reads; **INFERRED** for "decoy" (the packaging path uses the manifest).

---

## 4. The POSIX `TZ=` prefix problem

- **VERIFIED** The only POSIX env-prefix in an npm script is `omega-pac/package.json:32`
  `"test": "TZ=Europe/London grunt test"`. (A repo-wide search for `TZ=` in tracked config finds this one
  line plus prose in `docs/`.) No other project script, and no grunt `aliases.coffee`, uses a `VAR=value`
  prefix — **VERIFIED** by searching each `grunt/*.coffee`.
- **Why it fails here:** `npm config get script-shell` is `null` on this Windows host, so npm runs the
  script through `cmd.exe` (`docs/PROJECT-STATE.md` §1). `cmd.exe` has no `TZ=... command` prefix grammar;
  it treats `TZ=Europe/London` as a program token and errors before `grunt` starts. The command only works
  when invoked through a POSIX shell (`bash -c 'TZ=Europe/London grunt test'`) — which is how the raw probe
  ran it (`docs/audit/raw/probe-2-legacy-toolchain.txt:772`). **INFERRED** — the `cmd.exe` failure is
  deduced from the recorded script-shell fact, not re-executed here.
- **Correction to `docs/PROJECT-STATE.md`:** §6/#10 describes an *optional* fix "a `omega-pac/Gruntfile.coffee`
  wrapper `process.env.TZ ?= 'Europe/London'`". That wrapper does **not** exist — `omega-pac/Gruntfile.coffee:1`
  is only `module.exports = require('load-grunt-config')`. The TZ wrapper is a proposal, not current code.
  **VERIFIED** the wrapper is absent. Report-back item.
- The *reason* the test needs London time is `omega-pac/test/conditions.coffee:468` building a local-time
  `Date` for `TimeCondition`/`WeekdayCondition`; the honest fix (explicit UTC in the test) is deliberately
  not made because it changes test semantics (`docs/PROJECT-STATE.md` §6/#10). **VERIFIED** the citation
  line exists; **UNVERIFIED** that changing it would alter semantics (not exercised).

---

## 5. The empty-matching `mochaTest` glob

All four projects set the same target: `src: ['test/**/*.coffee']`
(`omega-pac/grunt/mochaTest.coffee:6`, `omega-target/grunt/mochaTest.coffee:6`,
`omega-web/grunt/mochaTest.coffee:6`, `omega-target-chromium-extension/grunt/mochaTest.coffee:6`).
**VERIFIED.** What actually matches:

| Project | `test/*.coffee` files present | glob outcome | Evidence of alias |
|---|---|---|---|
| `omega-pac` | 6 (conditions, pac_generator, profiles, rule_list, shexp_utils, utils) | runs real specs | `omega-pac/grunt/aliases.coffee:6` |
| `omega-target` | 1 (options_sync) | runs real spec | `omega-target/grunt/aliases.coffee:6` |
| `omega-web` | 0 — `test/` has no `.coffee` | **matches nothing**, `grunt test` exits 0 | `omega-web/grunt/aliases.coffee:11` |
| `omega-target-chromium-extension` | 0 | **matches nothing**, `grunt test` exits 0 | `omega-target-chromium-extension/grunt/aliases.coffee:9` |

**True count: 7 test files in the repo** (6 + 1 + 0 + 0), matching `docs/PROJECT-STATE.md` §1.5.
The green "test passes" signal for web and chromium is **vacuous** — the glob never matches a file, so
mocha runs zero specs and grunt reports success. **VERIFIED** (directory listing + config).

Nuance: `omega-web/package.json:23-25` and `omega-target-chromium-extension/package.json:33-35` define **no
`test` npm script** (only `dev`), so for those two the vacuous pass comes from `grunt test`, not `npm test`.
**This refines** `docs/PROJECT-STATE.md` §1.5 (which says "`npm test` … succeeds vacuously"). Report-back item.

---

## 6. No lockfile tracked in git

- **VERIFIED** `.gitignore:3` ignores `package-lock.json` globally.
- **VERIFIED** `git ls-files | grep -E 'package-lock|yarn.lock|npm-shrinkwrap'` returns nothing — no lock
  file is tracked. (Also `node_modules`/`bower_components` are ignored, `.gitignore:1-2`.)
- Lock files **do** exist on this disk (six: the five projects + `packages/schema/package-lock.json`) but
  they are untracked build residue, not shared state. **VERIFIED** by find + git ls-files.
- **Consequence:** `npm install` (`omega-build/package.json:12`) resolves fresh against `^`/`~` ranges every
  time, so a clean machine can pull different transitive versions than the ones that produced the
  353-file/742 272-byte baseline. **Dependency-resolution reproducibility on a clean machine is UNVERIFIED**
  — recorded here per `docs/PROJECT-STATE.md` §4.4 / §9.2. (The build-checker treats lock files as derived;
  `DERIVED_PREFIX` in `tools/check-doc-evidence.mjs`.)

---

## 7. Can the two CI configurations pass today? — No.

There are two CI configs in the tree: `.github/workflows/release.yml` and `.circleci/config.yml`. The plan's
`verify.yml` does **not exist yet** (only `release.yml` is in `.github/workflows/`); it is task #10. **VERIFIED.**
No CI run was observed for either config, so every verdict below is **INFERRED** from the YAML + the code facts
above.

### 7.1 GitHub — `.github/workflows/release.yml` (name `Build and Release`)

- **VERIFIED** Triggers only on tag pushes matching `v*` (`.github/workflows/release.yml:3-6`). It never fires on a branch
  push or PR, so on `nexora/architecture` it is dormant — it cannot be "green" because it does not run.
- **VERIFIED** Runs on `ubuntu-latest` with Node `'22'` (`.github/workflows/release.yml:10`,
  `.github/workflows/release.yml:21`); executes
  `npm run deps`/`dev`/`build`/`release` from `omega-build` (`.github/workflows/release.yml:23-29`).
- **INFERRED hazards even if a `v*` tag were cut:** (a) no lockfile (§6) so `npm run deps` re-resolves
  against a deprecated Bower registry and `git://` deps (§1.1); (b) `bower install` inside `deps`
  (`omega-build/package.json:12`) must succeed over `git://`; (c) the workflow does **not** run `npm test`
  or `grunt test` at all — zero test coverage in CI; (d) the branding is still ZeroOmega-era: the job
  publishes `chromium-release.zip`/`firefox-release.zip` (`.github/workflows/release.yml:33-43`), not NEXORA artifacts.

### 7.2 CircleCI — `.circleci/config.yml` (dead)

- **VERIFIED** Pins a retired image `circleci/node:7.10` (`.circleci/config.yml:5`); that Docker namespace
  has long been unsupported.
- **VERIFIED** Expects an artifact the current build no longer emits: it runs `grunt release` then
  `mv release.zip …` (`.circleci/config.yml:82-83`), but the `compress` task writes `../dist/*-release.zip`
  (`omega-target-chromium-extension/grunt/compress.coffee:6`), not a root `release.zip`. The `mv` therefore
  fails → the job can only ever be red. (`.gitignore:7` in the chromium project even still ignores the old
  `/release.zip`, confirming it is a vestigial name.)
- **VERIFIED** Uses `sudo npm install -g … bower web-ext json` and `web-ext lint`
  (`.circleci/config.yml:37`, `:86`) — extra moving parts, and the whole file's 6 cache keys are tied to the
  `omega-*` directory names (`.circleci/config.yml:14-34`, see `docs/audit/01` §7 row 21).
- **INFERRED** The project is not connected to a running CircleCI pipeline for this repo (no org context
  available here); treating it as live would be unfounded.

Both configs are pending a human decision to delete/replace (`docs/PROJECT-STATE.md` §9.3, §7).

---

## 8. Risk verdict for NEXORA

### 8.1 Must NOT be touched (works today, Ladder A)

- **VERIFIED** The Grunt 0.4 + CoffeeScript + Bower chain builds green on Node 24 on this host
  (`docs/PROJECT-STATE.md` §1.1, §5). No migration to modern Grunt/TS/npm-workspaces is warranted in
  Phase 1.
- **VERIFIED** `bower install` resolves both `git://` deps on this host (`docs/PROJECT-STATE.md` §1.2), so
  **vendoring ~25 components into git is not needed**; `tools/verify-vendor.mjs` is the guard instead.
- The relative-path `file:` + `npm link` + `node_modules`-symlink wiring (see `docs/audit/01` §2) is the
  load-bearing reason the build works; changing it without a green rebuild is the highest-risk edit.

### 8.2 Real hazards for a clean runner / future maintainer

| Hazard | Evidence | Severity |
|---|---|---|
| No lockfile → non-deterministic resolution; §6 UNVERIFIED reproducibility | `.gitignore:3` | High |
| `TZ=` prefix breaks `cmd.exe` npm scripts on Windows | `omega-pac/package.json:32` | High for any Windows CI |
| No `engines` pin → a future Node major can silently break Grunt 0.4 | §2, `omega-build/package.json:8` | Medium |
| Bower deprecated registry + `git://` transport, moving `#main` refs | `omega-web/bower.json:37`, `:43` | Medium |
| Vacuous test pass hides zero coverage in web + chromium | `omega-web/grunt/mochaTest.coffee:6` | Medium |
| CI is either dormant (tag-gated) or dead (node:7.10 / `release.zip`) | `.github/workflows/release.yml:5`, `.circleci/config.yml:5` | Medium |
| `require.resolve('omega-pac/omega_pac.min.js')` at config load couples build order to file existence | `omega-target-chromium-extension/grunt/browserify.coffee:43` | Medium |

Forward-looking note: the Connector is never inside the extension zip; packaging that is a Phase-2 concern.
This audit only records that **today** the zip is `build/` + filtered manifest
(`omega-target-chromium-extension/grunt/compress.coffee:7-18`).

---

## UNVERIFIED registry

| Claim | Why it could not be checked here |
|---|---|
| §6 / §8.2 dependency-resolution reproducibility on a clean machine | No lockfile is tracked (§6 verified), and a clean-room `npm install` was not run; reproducibility is therefore assumed unknown, not proven broken. |
| §4 `cmd.exe` failure of `TZ=` prefix | Deduced from the recorded `script-shell: null` host fact; the failing invocation was not executed this session. |
| §4 that moving time-parsing to explicit UTC would change test semantics | `omega-pac/test/conditions.coffee:468` line verified present; the semantic consequence was not exercised. |
| §7.1 / §7.2 CI verdicts ("dormant", "can only be red") | No CI run was observed; verdicts are INFERRED from the YAML plus code facts. The specific failure modes (Bower reachability, `git://` firewalling) were not executed. |
| §1.1 `git://` transport blocked / unpinned risk | URLs read verbatim; actual transport/registry behaviour not probed. |
| §8.1 "builds green on Node 24" | Carried from `docs/PROJECT-STATE.md` §5 measurements; not re-built here (builds forbidden this iteration). |
