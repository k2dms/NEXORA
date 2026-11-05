# 02 — Toolchain decay and risk

> Purpose: date-stamp the legacy toolchain and runtime deps with line-level evidence, prove the
> reproducibility gaps (no `engines`, no tracked lockfile, empty test globs, POSIX `TZ=` prefix),
> assess whether the CI configurations in the tree can pass today (§7.1, §7.2 — and §7.3 for `verify.yml`,
> written after the audit), and give the NEXORA risk verdict: what must
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

Measured 2026-11-05, first while writing `.github/workflows/verify.yml` and then by probe-5 with the
`bower_components` directory moved aside (the risk above is now observed twice; the sentence before this
paragraph stays as the audit-time state):

- `git ls-remote git://github.com/zero-peak/zero-dependencies.git HEAD` → `fatal: unable to connect to
  github.com: … errno=timed out`. The `git://` transport is not reachable **from this host**. **VERIFIED** by
  execution. The inference drawn from this on the same day — that probe-3's bower success therefore came from
  the already-installed `omega-web/bower_components/` — is **false**, and the next two bullets are why.
- `git ls-remote --heads https://github.com/zero-peak/zero-dependencies.git main` →
  `4de5f14a046f74d6cbf830c01b1736d0270d7154`, and the same for `suziwen/angular-spectrum-colorpicker` →
  `e22ff9184a01d683c4d653b17c860ed25b987fa3`. Both repos are alive over https with the branch the manifest
  asks for. **VERIFIED** by execution.
- **A cold install works.** `mv bower_components aside && bower install` exits **0** and reinstalls all 26
  directories, with `zero-dependencies` resolving to `4de5f14a04` and `angular-spectrum-colorpicker` to
  `e22ff9184a` — the two commits the https `ls-remote` reports. The string `git://` appears **zero times** in
  the capture: bower rewrites the declared endpoint to `https://github.com/<owner>/<repo>.git` before it shells
  out to git. So the dead transport in `bower.json` is a cosmetic defect in a file bower reads leniently, not a
  build blocker. **VERIFIED** (`docs/audit/raw/probe-5-bower-cold-install.txt`).
- `git -c url."https://github.com/".insteadOf=git://github.com/ ls-remote
  git://github.com/zero-peak/zero-dependencies.git HEAD` → `4de5f14a…`, and with that config set for the
  process the cold `bower install` is byte-for-byte the same success it was without it. `verify.yml` still
  exports it on the runner (`git config --global`, so the repository keeps its own text) as defence in depth for
  any tool that hands the literal URL to git — **not** because the build needs it, which is what its step comment
  now says. **VERIFIED** by execution.
- `https://registry.bower.io/packages/angular` → HTTP 200. The deprecated registry still answers, so the
  components that name it resolve. What is still not observed is a machine with an empty Bower cache: 21 of the
  26 directories came from this host's cache during the cold install, the other five — including both `git://`
  endpoints — came off the network. **VERIFIED** for the five; **UNVERIFIED** for the twenty-one.

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
- Executed 2026-11-05, so the deduction above is now a measurement: `cmd.exe //c "npm test"` in `omega-pac`
  exits **1** with `"TZ" не является внутренней или внешней командой` ("`TZ` is not recognized as an internal
  or external command"), printed before any grunt output. The prefix alone is the failure — `npm run dev` and
  the `test` task itself are unaffected. **VERIFIED** by execution.
- **How CI resolves it, and what CI refuses to change.** `.github/workflows/verify.yml` runs the same task
  with the zone exported by the step rather than by the script: an `env`/`export` pair is shell-neutral, an
  npm-script prefix is not. Two alternatives were considered and rejected — editing
  `omega-pac/package.json:32` (a legacy file the layout rules keep untouched, and the prefix is correct on
  every POSIX host), and the `process.env.TZ ?= 'Europe/London'` wrapper proposed for
  `omega-pac/Gruntfile.coffee` (it would apply
  the test's zone to *every* grunt task in the project, including `build`, to satisfy one spec). For the dates
  the suite builds (`01 Feb 2016 …`) London is UTC+0, so the exported zone does not move the floor
  `verify.yml` pins; a clean runner reproduces exactly that floor — all three images of the 2026-11-05 runs print
  `142 passing, 1 failing` and `8 passing, 5 failing` (`docs/audit/99-baseline-report.md` §12), so §7.3's open
  question about the zone is answered and this one is not a hazard on a runner.
- **Correction to `docs/PROJECT-STATE.md`:** §6/#10 describes an *optional* fix "a `omega-pac/Gruntfile.coffee`
  wrapper `process.env.TZ ?= 'Europe/London'`". That wrapper does **not** exist — `omega-pac/Gruntfile.coffee:1`
  is only `module.exports = require('load-grunt-config')`. The TZ wrapper is a proposal, not current code.
  **VERIFIED** the wrapper is absent. Report-back item. The decision taken in the bullet above supersedes the
  proposal: the wrapper is **not** added, and no legacy file gains a `TZ` line in this iteration.
- The *reason* the test needs London time is `omega-pac/test/conditions.coffee:468` building a local-time
  `Date` for `TimeCondition`/`WeekdayCondition`; the honest fix (explicit UTC in the test) is deliberately
  not made because it changes test semantics (`docs/PROJECT-STATE.md` §6/#10). **VERIFIED** the citation
  line exists; **UNVERIFIED** that changing it would alter semantics (not exercised). The disposition is
  therefore explicit: the zone stays a runner concern in Phase 1, and the test-side rewrite is a Phase-2
  change to be made together with the NEXORA-owned suite that replaces this floor — not as a drive-by edit of
  a legacy spec.

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

## 7. Can the CI configurations pass today?

There were two CI configs in the tree at audit time, `.github/workflows/release.yml` and
`.circleci/config.yml`, and the plan's `verify.yml` did **not exist yet** — that sentence was true on 2026-11-04
and is what §7 was written to say. `verify.yml` was written on 2026-11-05 (§7.3), and no run of any of the three
has been observed here, so every verdict below is **INFERRED** from the YAML plus the code facts above.

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

### 7.3 `.github/workflows/verify.yml` — written 2026-11-05, run four times that day, green on the last two

The file exists and it has executed on GitHub runners: four runs of it on 2026-11-05, the
first two red on defects in the workflow itself, the third green across all six jobs and the fourth repeating that
green on the commit that records it. The measurements below are
therefore of two kinds — what was read off this host's shell before any runner existed, and what the runs printed,
which is recorded line by line in `docs/audit/99-baseline-report.md` §12.

- **Three jobs.** `contracts` runs the contract root, the citation checker, the two ADR record checks (the four
  `docs/adr/0001-adr-practice.md:99` promises — index parity, one `# ADR-NNNN:` heading, the six sections, no
  duplicate number — plus `Status:`, `Date:` and gap-free numbering) and the layout guards; `legacy-chain` (`needs: contracts`)
  installs the five legacy roots, links them, builds through the hub (`omega-build/package.json:14`), tests,
  packages and inspects the zips; `connector` is present but switched off with `if: ${{ false }}`, because Go,
  Docker and Python are all absent on the audited host (`docs/audit/99-baseline-report.md` §1) and there is no
  `connector/` tree yet. Deleting the job would hide the gap; an off job names it.
- **A matrix rather than one runner:** `ubuntu-latest`/Node 22, `windows-latest`/Node 22, `windows-latest`/Node 24,
  the last with `continue-on-error`. Windows is in the matrix because the failures this repository actually has
  are Windows failures — §4's `TZ=` prefix was measured failing here, and the audited host is Windows
  (`docs/audit/99-baseline-report.md` §1). Node 24 reports without blocking because it is the version carrying
  the `minifyify@4.4.0` `EBADENGINE` (§2, `docs/audit/99-baseline-report.md` §3).
- **§7.1(a)/(b) — the transport — is answered by rewriting the runner, and probe-5 says the rewrite is not what
  answers it.** The first step of `legacy-chain` sets
  `git config --global url."https://github.com/".insteadOf git://github.com/` (§1.1) before any install, which is
  how the runner is told to fetch while `omega-web/bower.json:37` and `:43` keep their text. Measured afterwards: a cold `bower install`
  succeeds **without** that config, because bower resolves those endpoints as `https://github.com/…\.git` on its
  own. The step therefore stays as belt-and-braces for a caller that passes the literal URL to git, and the
  honest claim about the runner is the smaller one — it will install, and it will fetch what this host served from
  cache. **Measured on the runner 2026-11-05:** all three images printed `Running "bower:install"` and
  `>> Installed bower packages` inside the hub build, and the zip carries 252 `lib/` entries there, on images that
  never held this host's cache (`docs/audit/99-baseline-report.md` §12). Worth naming precisely, because the
  workflow does not run the `bower install` of `omega-build/package.json:12` at all: the components arrive through
  grunt's own default sequence — `omega-web/grunt/aliases.coffee:9`, whose config is
  `omega-web/grunt/bower.coffee:5` with `targetDir: 'build/lib/'` — which is also why the extension's own `lib/`
  holds no tracked files.
- **§7.1(c) — zero test coverage — becomes a pinned floor.** The step `VER-11 -- test floor per project, parsed
  from the mocha summary` runs `grunt test` per project and demands `>= 142 passing / <= 1 failing` for
  `omega-pac` and `>= 8 / <= 5` for `omega-target`, both read from `docs/audit/99-baseline-report.md` §5; it
  exports the zone instead of using the npm-script prefix (§4), and prints `NOTE: <project> has 0 test files`
  for the two projects whose glob matches nothing (§5) rather than letting a vacuous green pass.
- **The 352-file lesson is the actual gate.** `VER-08 -- inspect the artifacts, never trust the exit code`
  asserts 353 entries, 252 `lib/` entries, 7 `_locales`, the exact top-level path set, and `js/omega_pac.min.js`
  present and ≥ 700 000 B. The uncompressed totals — 5 999 353 B (chromium) / 5 999 408 B (firefox) — and the
  bundle's recorded 742 272 B are printed as NOTEs beside the measured values, together with each sha256 beside the
  value recorded in `docs/audit/99-baseline-report.md` §6. Byte identity is a NOTE, not an assertion, and probe-4
  measured why: rebuilding the same tree on the same machine reproduced every content figure and moved both hashes,
  because the zip stores per-entry build timestamps (`docs/audit/99-baseline-report.md` §6). The totals were an
  assertion until the second run, which was red on ubuntu for exactly that reason: those figures are a property of
  a `core.autocrlf=true` checkout, and a clean LF checkout of the same commit is 24 964 B smaller — demoted in the
  same commit that fixed the Windows PATH defect, and green on the third run (§12).
- **What it deliberately does not answer:** §7.1(d) — `release.yml` remains the only thing that publishes and
  still ships `chromium-release.zip`/`firefox-release.zip`; the lockfile gap (§6) is unchanged because a
  lockfile policy is a human decision; and the 6 failing legacy tests stay the pinned regression floor rather
  than a green suite.
- **How it was validated before any runner existed.** Every `run:` block was extracted to a file and executed here
  under `set -eo pipefail` with runner-like working directories — including `check_zip` against the real
  `dist/chromium-release.zip` and `run_floor` against the real `omega-pac` suite (both printed their measured
  values, e.g. `142 passing, 1 failing`) — and each guard was then re-run against a synthetic violation (a root
  `package.json`, an `omega-*` import specifier, a side-effect-only `import 'omega-pac/…'`, a `"file:` link into
  the legacy tree, a `vless` literal in a `.svelte` file, a truncated zip) to prove it can fail. That was evidence
  about the shell logic only, and the four runs showed exactly how much that is worth: it caught nothing that the
  runner did not, and the runner found two defects no host shell could — a Windows `$GITHUB_WORKSPACE` that is not a
  POSIX path, and a byte total that is a property of one checkout. Both were reproduced here before being fixed
  (`docs/audit/raw/probe-7-grunt-path.txt`), and both were green on the next run. The runner output itself is kept as
  `docs/audit/raw/probe-8-runner-runs.txt`.

---

## 8. Risk verdict for NEXORA

### 8.1 Must NOT be touched (works today, Ladder A)

- **VERIFIED** The Grunt 0.4 + CoffeeScript + Bower chain builds green on Node 24 on this host
  (`docs/PROJECT-STATE.md` §1.1, §5). No migration to modern Grunt/TS/npm-workspaces is warranted in
  Phase 1.
- **VERIFIED** `bower install` produces the 252-entry `lib/` tree on this host (`docs/PROJECT-STATE.md` §1.2), so
  **vendoring ~25 components into git is not needed**; `tools/verify-vendor.mjs` is the guard instead. The
  *reason* it works took three readings: raw `git ls-remote git://…` times out here (§1.1), so the same day's
  first explanation was "the components were already installed" — which probe-5 falsified by moving
  `omega-web/bower_components/` aside and reinstalling it successfully, with no `git://` URL in the capture
  because bower normalises those endpoints to https itself. So "works today" now includes "works cold", and the
  remaining gap is cache-shaped, not transport-shaped: 21 of 26 directories came from this host's Bower store.
  `#main` (§1.1) is still a moving ref either way, so this bullet remains about not vendoring, not about pinning.
- The relative-path `file:` + `npm link` + `node_modules`-symlink wiring (see `docs/audit/01` §2) is the
  load-bearing reason the build works; changing it without a green rebuild is the highest-risk edit.

### 8.2 Real hazards for a clean runner / future maintainer

| Hazard | Evidence | Severity |
|---|---|---|
| No lockfile → non-deterministic resolution; §6 UNVERIFIED reproducibility | `.gitignore:3` | High — unchanged by the runs: three clean images resolved and built without a lockfile (§12 of `docs/audit/99-baseline-report.md`), which proves a set resolves today, not that it is the same set next month |
| `TZ=` prefix breaks `cmd.exe` npm scripts on Windows | `omega-pac/package.json:32` | High — measured (§4); CI exports the zone instead (§7.3), and the third run is green on both Windows cells at the pinned floors, which is the first time the workaround executed where the npm prefix cannot |
| No `engines` pin → a future Node major can silently break Grunt 0.4 | §2, `omega-build/package.json:8` | Medium |
| Bower deprecated registry + `git://` transport, moving `#main` refs | `omega-web/bower.json:37`, `:43` | Low-Medium — the transport is dead to *git* but bower normalises it to https and a cold install works (§1.1); `#main` is still a moving branch. The empty-cache half of this row is answered: the runner fetched every component with no cache behind it and the zip carries the same 252 `lib/` entries this host's warm cache produced (§7.3) |
| npm resolution never re-measured from an empty `node_modules` | `.gitignore:3`, probe-4 §1-deps (`up to date` five times`) | High — **narrowed by the runs**: `npm install` from an empty tree has now been observed on three clean images with no lockfile in git, and every floor and artifact figure reproduced there (§12 of `docs/audit/99-baseline-report.md`). What is still not measured is a *user* machine and whether the resolved set is identical across images — the ubuntu and windows builds of one commit differ by 12 B uncompressed, unexplained |
| Vacuous test pass hides zero coverage in web + chromium | `omega-web/grunt/mochaTest.coffee:6` | Medium — and the runner prints `NOTE: <project> has 0 test files` for both, on every cell, rather than passing them quietly |
| CI is either dormant (tag-gated) or dead (node:7.10 / `release.zip`) | `.github/workflows/release.yml:5`, `.circleci/config.yml:5` | Medium — and now three configs, only one of which has ever run: `verify.yml` has three runs and a green one (§7.3), while `release.yml` stays tag-gated and `.circleci/config.yml` stays a config nothing executes |
| `require.resolve('omega-pac/omega_pac.min.js')` at config load couples build order to file existence | `omega-target-chromium-extension/grunt/browserify.coffee:43` | Medium |

Forward-looking note: the Connector is never inside the extension zip; packaging that is a Phase-2 concern.
This audit only records that **today** the zip is `build/` + filtered manifest
(`omega-target-chromium-extension/grunt/compress.coffee:7-18`).

---

## UNVERIFIED registry

| Claim | Why it could not be checked here |
|---|---|
| §6 / §8.2 dependency-resolution reproducibility on a clean machine | No lockfile is tracked (§6 verified), and a clean-room `npm install` was not run; reproducibility is therefore assumed unknown, not proven broken. Both 2026-11-05 probes got closer without closing it: probe-4 re-ran `npm run deps` against this host's existing `node_modules` (five `up to date` lines, so the chain was measured and the resolution was not), and probe-5 did move `omega-web/bower_components/` aside, but 21 of the 26 directories then resolved from this host's Bower cache rather than the network (§1.1). **The runner then closed the clean-tree half** — three images installed all five roots from an empty `node_modules`, with no lockfile, and reproduced every floor and every artifact content figure (§12 of `docs/audit/99-baseline-report.md`). What the row still refuses to call closed is resolution *identity*: no lockfile is tracked, so nothing states which set a future host will get, and the ubuntu and windows builds of one commit already differ by 12 B uncompressed for a reason nobody has computed. |
| §4 that moving time-parsing to explicit UTC would change test semantics | `omega-pac/test/conditions.coffee:468` line verified present; the semantic consequence was not exercised. |
| §7.1 / §7.2 / §7.3 CI verdicts | `verify.yml` has now run: four times on 2026-11-05, red, red, green, green (§12 of `docs/audit/99-baseline-report.md`), so §7.3's verdict is no longer an unproven one. §7.1's and §7.2's stay exactly as written — INFERRED from YAML plus code facts — because neither of those two configs has ever executed: `release.yml` reacts only to a `v*` tag, and no tag was pushed in this iteration, and `.circleci/config.yml` is a config nothing runs. |
| §7.3 that the matrix rows, `needs` ordering and `continue-on-error` do what the YAML says | **Closed for the matrix and the `needs` gate; still open for `continue-on-error`.** The gate held when it mattered: in `42015170195` all three `contracts` cells were red and the `legacy-chain` job never executed — its log contains no line for it at all, and the API lists it as skipped with the matrix expression left unexpanded (`${{ matrix.os }} / ${{ matrix.node }}`), which is the shape GitHub gives a matrix job whose `needs` failed. `actions/checkout@v4`, `actions/setup-node@v4` and `actions/upload-artifact@v4` all executed. What no run measured is the non-blocking half: run `42017987418` had a red Node 24 cell, and the jobs API still called that cell `failure`, so `continue-on-error: ${{ matrix.node == '24' }}` is only observed in the case where it does not have to work — in `42020617681` and `42025558933` the cell is green
for the same reason the other two are. The suspicion behind this row was right about one thing, though: a `GITHUB_*` input did break, in a way no host shell could show. `$GITHUB_WORKSPACE` is `D:\a\NEXORA\NEXORA` on a Windows runner, and the `PATH` built from it reached grunt as `Error: Cannot find module 'A:\NEXORA\NEXORA\omega-build\node_modules\grunt-cli\bin\grunt'`; `docs/audit/raw/probe-7-grunt-path.txt` reproduces the same defect on this host, where the same `\a\` segment resolves against the Git install directory instead of a runner drive. |
| §1.1 that `#main` actually moves | Both branches were resolved once, over https (`4de5f14a…`, `e22ff918…`), so they are alive; no second observation exists to show the ref shifting, so the drift risk is reasoned, not measured. The transport itself, however, **is** measured now — the earlier "not probed" entry was closed by the 2026-11-05 probes recorded in §1.1, as was §4's `cmd.exe` deduction. |
| §8.1 "builds green on Node 24" | Carried from `docs/PROJECT-STATE.md` §5 measurements; not re-built during this audit (builds were forbidden in that iteration). The later probe-3 rebuild is in `docs/audit/99-baseline-report.md` §4.3, §10. |
