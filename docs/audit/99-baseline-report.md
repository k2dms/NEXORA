# 99 — Baseline report (green-build probe, ladder A)

> Purpose: record what the legacy ZeroOmega v3.5.2 toolchain in this repository actually does on this
> host, before and after the only two product-file edits made in this iteration, so that every later
> architectural claim ("the chain still builds", "the test floor is X") has a measured anchor instead of
> an assumption. Raw logs live next to this file in `docs/audit/raw/`.

Date of probes 1–3: **2026-11-04**; probes 4–5 ran on **2026-11-05** (§11). Branch: `nexora/architecture`. Tag
audited: ZeroOmega v3.5.2 (`release v3.5.2` is HEAD's closest release commit).

---

## 1. Host facts

| Item | Value | Tag |
|---|---|---|
| Node | v24.22.1 | **VERIFIED** (`node -v`) |
| npm | 11.19.1 | **VERIFIED** (`npm -v`) |
| git | 2.56.0.windows.3 | **VERIFIED** (`git --version`) |
| Go | absent | **VERIFIED** (`command -v go` → nothing) |
| Docker | absent | **VERIFIED** |
| Python | absent (only the Windows Store App-execution-alias stub, which errors out) | **VERIFIED** |
| Bower on PATH | absent — Bower is used only through the `grunt-bower-task` config, invoked by the `bower:install` grunt task | **VERIFIED** (probe 3 log) |
| `npm config get script-shell` | `null` → npm scripts execute through `cmd.exe`, so a POSIX env prefix in a script fails hard | **VERIFIED** |
| OS | Windows 10.0.26300 x64 | **VERIFIED** |

`omega-pac/package.json:32` is `"test": "TZ=Europe/London grunt test"` — a POSIX prefix of exactly that
kind. **VERIFIED**. It is why the probe ran `grunt test` directly instead of `npm test` for that project.

## 2. Method

Three probe rounds, executed **before** any edit, then once more after the minimal fix. Each round wrote a
per-step table (`exit=<code>|<elapsed>`) plus the full captured output. No `--force` was used except where
explicitly labelled as a diagnostic. Two more rounds ran on 2026-11-05, after every deliverable was on disk:
probe-4 re-ran the whole chain plus every gate as the iteration's end-to-end check (§11), and probe-5 settled
the transport question probe-3 left inferred (§7).

| Round | What it did | Raw log |
|---|---|---|
| probe-1 | `npm install --no-audit --no-fund` in `omega-build`, `omega-pac`, `omega-target`, `omega-web`, `omega-target-chromium-extension`; then `npm run build` and `npm run release` in `omega-build` | `docs/audit/raw/probe-1-legacy-toolchain.txt` (326 lines), `probe-1-table.txt` |
| probe-2 | `npm run dev` (the `npm link` chain), then grunt per project, per-project `test`, `--force` diagnostics, `grunt release`, `ls dist` | `docs/audit/raw/probe-2-legacy-toolchain.txt` (1036 lines), `probe-2-table.txt` |
| probe-3 | the same chain after the two lint demotions, ending in `npm run build` + `npm run release` from `omega-build` | `docs/audit/raw/probe-3-ladder-a-green.txt` (423 lines) |
| probe-4 | the whole Ladder A chain again on 2026-11-05 — `npm run deps`, `npm run dev`, `grunt`, `grunt release` — then both legacy suites at the pinned floors, all five gates, and the artifact assertions re-measured from the zips this run wrote | `docs/audit/raw/probe-4-e2e-re-verification.txt` |
| probe-5 | `omega-web/bower_components/` moved aside and reinstalled, with and without a `git:// → https://` rewrite, then the same with an attempted cold cache | `docs/audit/raw/probe-5-bower-cold-install.txt` |

Step table verbatim, probe-1 (`probe-1-table.txt`): all five installs `exit=0`; `build:omega-*` steps were
not attempted individually (`missing`); `hub-build|exit=3|1s`, `hub-release|exit=3|1s`.

## 3. probe-1 — installs are fine, the hub build is not

Every `npm install` succeeded (62 packages for `omega-build` in 12 s; `omega-pac` 24 s; `omega-target` 9 s;
`omega-web` 20 s; `omega-target-chromium-extension` 9 s). **VERIFIED**.

npm emitted a large volume of `deprecated` warnings (`glob@3.x/4.x/5.x`, `minimatch@0.2/0.3`,
`graceful-fs@1.2.3`/`2.0.3`, `mkdirp@0.3.x`, `jade@0.26.3`, `coffee-script@1.3.3/1.11.1/1.12.7`,
`source-map-resolve@0.5.3`, `urix@0.1.0`, `resolve-url@0.2.1`, `querystring@0.2.0`, `sprintf@0.1.5`,
`path-dirname@1.0.2`, `inflight@1.0.6`, `rimraf@2.2.8`) and one `EBADENGINE`:
`minifyify@4.4.0` declares `required: { node: '0.10.x' }` against `current: { node: 'v24.22.1' }`.
**VERIFIED** (probe-1 log lines 12–75). The engine warning is advisory only — npm installed it anyway and
the project then built. **VERIFIED** (probe-3).

`npm run build` → `npm run release` both **aborted with exit 3**. `omega-build/package.json:15` is
`"release": "cd ../omega-target-chromium-extension && grunt  release"` (note the double space, harmless).
**VERIFIED**.

## 4. probe-2 — the failure is lint, not the toolchain

Plan §4 predicted ladder B ("Grunt 0.4 dies at module load on Node 24, most likely via
`glob@3.2`/`graceful-fs@2`"). **That prediction is false.** Grunt 0.4 loads, runs tasks and writes bundles
on Node 24. **VERIFIED** (probe-3 log). The real blocker is 8 pre-existing coffeelint *errors*.

### 4.1 The eight violations (all `max_line_length`, all pre-existing)

| Project | File | Line | Length | Tag |
|---|---|---|---|---|
| omega-pac | `src/conditions.coffee` | 282 | 83 | **VERIFIED** (probe-2 log 146) |
| omega-pac | `src/conditions.coffee` | 372 | 129 | **VERIFIED** (probe-2 log 147) |
| omega-pac | `src/conditions.coffee` | 542 | 91 | **VERIFIED** (probe-2 log 148) |
| omega-pac | `src/rule_list.coffee` | 87 | 84 | **VERIFIED** (probe-2 log 154) |
| omega-target-chromium-extension | `src/module/proxy/proxy_impl_firefox.coffee` | 103 | 85 | **VERIFIED** (probe-3 log 133) |
| omega-target-chromium-extension | `src/module/proxy/proxy_impl_firefox.coffee` | 107 | 87 | **VERIFIED** (probe-3 log 134) |
| omega-target-chromium-extension | `src/module/proxy/proxy_impl_listener.coffee` | 67 | 85 | **VERIFIED** (probe-3 log 140) |
| omega-target-chromium-extension | `src/module/proxy/proxy_impl_listener.coffee` | 71 | 87 | **VERIFIED** (probe-3 log 141) |

omega-pac reports `3 errors` + `1 error`; the extension reports `2 errors` + `2 errors` — eight in total.
**VERIFIED**. In probe-3, with the level demoted, the identical lines are reported as `warnings` and the
tasks succeed. **VERIFIED**.

### 4.2 The abort chain, step by step

1. `coffeelint:src` fails in `omega-pac` → `Warning: Task "coffeelint:src" failed. Use --force to continue.`
   → `Aborted due to warnings.` **VERIFIED** (probe-2 log 160).
2. Because the default task aborts before `browserify:browser`, `omega-pac/omega_pac.min.js` is never
   written: `test -f omega-pac/omega_pac.min.js` → `exit=1`. **VERIFIED** (probe-2 table `artifact:omega_pac.min.js`).
3. `omega-target-chromium-extension` cannot even load its Gruntfile, because its browserify config requires
   that missing bundle **at config load time**: `omega-target-chromium-extension/grunt/browserify.coffee:36`
   maps `'omega-pac': 'omega-pac/omega_pac.min.js'`, and `omega-target-chromium-extension/grunt/browserify.coffee:43`
   does `noParse: [require.resolve('omega-pac/omega_pac.min.js')]`. **VERIFIED**.
   Observed output: `Loading "Gruntfile.coffee" tasks...ERROR` / `Error: Cannot find module
   'omega-pac/omega_pac.min.js'` with the require stack running through
   `load-grunt-config/lib/readfile.js` → `readconfigdir.js` → `gruntconfig.js` → `Gruntfile.coffee`
   (probe-2 log 269–280). **VERIFIED**.
4. Grunt then reports `Warning: Task "default" not found.` — the task list never got registered, so the
   abort message looks like a missing task rather than a missing module. This is the trap: the surface error
   points at the wrong project. **VERIFIED** (probe-2 log 281).
5. `grunt-hub` propagates the failure: `Warning: Task "hub:omega-pac:default" failed.` → `exit=3`
   (probe-1 log 286–289). **VERIFIED**.

`omega-target` and `omega-web` build successfully in probe-2 even while `omega-pac` is failing
(`build:omega-target|exit=0`, `build:omega-web|exit=0`), because `omega-target`'s own bundle does not need
the omega-pac file to *load*, only to be present for packaging. **VERIFIED** (probe-2 table).

### 4.3 `--force` diagnostics (probe-2)

`grunt --force` in omega-pac (`exit=0`), omega-target (`exit=0`) and the extension (`exit=0`) makes the
bundles appear (`artifact:after-force|exit=0`). **VERIFIED**. This proved the chain is healthy apart from
lint, which is what selected ladder A over ladder B.

## 5. Test floor (probe-2)

`npx grunt test` in `omega-pac` (run directly, not via `npm test`, because of the `TZ=` prefix):
**142 passing (94 ms) / 1 failing** → `Warning: Task "mochaTest:test" failed.` → `exit=3`. **VERIFIED**.

The single failure is `omega-pac/test/profiles.coffee:52`, `Profiles #pacResult should return special
compatible result for SOCKS5`: expected `SOCKS5 127.0.0.1:8888; SOCKS 127.0.0.1:8888`, actual
`SOCKS5 127.0.0.1:8888`. **VERIFIED**. This is load-bearing for NEXORA, not cosmetic: `ConnectorProfile.compile`
must emit a PAC result string through the same function, so the behaviour is a **Phase-2 blocking
requirement** (see `docs/audit/12-what-must-not-change.md` and the LIVE matrix).

`grunt test` in `omega-target`: **8 passing (6 s) / 5 failing** → `exit=3`. **VERIFIED**. All five are in
`OptionsSync`:

| # | Test | Failure | Tag |
|---|---|---|---|
| 1 | `#merge should use oldVal when sync is disabled in newVal` | assertion, `omega-target/test/options_sync.coffee:43` | **VERIFIED** |
| 2 | `#merge should use oldVal when sync is disabled in oldVal` | assertion, `omega-target/test/options_sync.coffee:47` | **VERIFIED** |
| 3 | `#requestPush should schedule storage write` | `Error: timeout of 2000ms exceeded` | **VERIFIED** |
| 4 | `#requestPush should combine multiple write operations` | timeout 2000 ms | **VERIFIED** |
| 5 | `#requestPush should disable syncing for the profiles if quota is exceeded` | timeout 2000 ms | **VERIFIED** |

Interpretation of 3–5: `requestPush` debounce/timer behaviour is not firing under `sinon@1.12` + fake timers
on Node 24. Cause not investigated — **UNVERIFIED**, recorded in §8.

`npm test` in `omega-web` and `omega-target-chromium-extension`: succeeds while matching **zero** test files
— those projects have no `test/` directory at all. Their `mochaTest` config globs `test/**/*.coffee`
(`omega-web/grunt/aliases.coffee:11` wires `test: ['mochaTest']`). **VERIFIED**. A green `npm test` in these
two projects means nothing; `verify.yml` (task #10) must print `NOTE: <project> has 0 test files`.

Real test inventory in the whole repo: **6 files in omega-pac, 1 file in omega-target, 0 elsewhere** = 7
test files. **VERIFIED** (`find` counts: omega-pac/test 6 files 1533 lines, omega-target/test 1 file 207
lines).

**Reproduced 2026-11-05 (probe-4), together with the two ways `npm test` dies before a test runs.** With
`omega-build/node_modules/.bin` on `PATH` and `TZ=Europe/London` exported, `grunt test` gives **142 passing
(95 ms) / 1 failing** in `omega-pac` — the same `omega-pac/test/profiles.coffee:52` SOCKS5 assertion, the same
`Warning: Task "mochaTest:test" failed.` — and **8 passing (6 s) / 5 failing** in `omega-target`, the same two
`OptionsSync` assertions and three 2 000 ms timeouts. Both exit 3. **VERIFIED** (`probe-4` §5c, §5d). The floor
holds, so it is a floor and not a coincidence of one afternoon.

Neither project's own `npm test` reaches the suite. In `omega-pac` the POSIX prefix is eaten by `cmd.exe`
(`docs/audit/02-toolchain-decay-and-risk.md` §4). In `omega-target`, `npm test` is plainly `grunt test` and it
still exits 1 with `"grunt" не является внутренней или внешней командой`: the project declares
`omega-target/package.json:11` `grunt ^0.4.5`, which ships no `bin`, and `grunt-cli` is a dependency of the hub
only — so there is no `omega-target/node_modules/.bin/grunt` shim for npm to put on `PATH`. **VERIFIED**
(`probe-4` §5b; `ls omega-target/node_modules/.bin` shows no grunt entry). This is the second reason the CI job
exports the hub's `.bin` directory instead of calling `npx grunt` in each leaf, and it means the sentence
"`npm test` is green in `omega-web`" from §5 above must be read as *vacuous*, never as *passing*.

## 6. Artifact finding — exit code 0 is not a correct artifact

With `--force` having produced the bundles, `grunt release` **exited 0** and wrote
`dist/chromium-release.zip` (1 710 687 B) + `dist/firefox-release.zip` (1 710 724 B). **VERIFIED** (probe-2
log 1000–1016). Those zips were **wrong**: 352 files, missing `js/omega_pac.min.js`.
**VERIFIED** at probe time. An extension built from that zip loads as a blank options page. **INFERRED** —
the blank page was reasoned from the missing bundle, not photographed in a browser.

After the lint fix, probe-3: `Created ../dist/chromium-release.zip (1903305 bytes)` and
`../dist/firefox-release.zip (1903342 bytes)`, `release:ext|exit=0`, `LADDER-A COMPLETE`. **VERIFIED**.

Re-measured 2026-11-04 for the record (all **VERIFIED**):

| Assertion | chromium | firefox |
|---|---|---|
| zip file entries | 353 | 353 |
| uncompressed bytes in zip | 5 999 353 | 5 999 408 |
| zip bytes on disk | 1 903 305 | 1 903 342 |
| `build/` tree files | 354 (6 001 108 B) | — |
| `build/lib/**` files | 252 | — |
| `build/_locales` dirs | 7 | — |
| `build/js/omega_pac.min.js` | 742 272 B | present |
| sha256 | `b93d5100e4c05ccf7af365dfc1742344360579f482df72e4e9ce00f5ba964ba8` | `b2309ce3bd0c2be246f0c228ffde1290fa1756c10ad93d90f17380249b4a728f` |

The 354-vs-353 and the 1 755-byte difference between the build tree and the zip is attributed to the
`manifest` task, which writes a filtered `tmp/{chromium,firefox}/manifest.json` (dropping `downloads`)
instead of packaging the build-tree manifest. **INFERRED** — the exact per-file diff was not computed.

Consequence recorded as a rule: **no build gate may trust an exit code.** `verify.yml` asserts file count,
`js/omega_pac.min.js` presence and size.

**Re-measured by probe-4 on 2026-11-05 — and the hash does not reproduce even on this machine.** A fresh
`grunt release` gave the same content: 353 entries in each zip, 1 903 305 and 1 903 342 bytes on disk, 252
`lib/` entries, 7 `_locales`, `js/omega_pac.min.js` at 742 272 B, and the same top-level path set. The sha256
values above did not: `73eee79d015ed404dc4bfdaa6d634ae0626a0612b2d603cb3412e6e47be68a4f` (chromium) and
`5ab2ca82a076ac83bbb1396630be6b7732a080fc3c25109b15a9caac92f9a887` (firefox). **VERIFIED** (`probe-4` §7a, §7b).
`unzip -l` on the new archive dates every entry `2026-11-05 14:44`, so the movement is attributed to the stored
build timestamps — **INFERRED**, because probe-4 overwrote the 2026-11-04 zips and no per-entry diff between the
two builds exists. What the difference proves on its own: a recorded artifact hash is an observation of one
build, never a floor. `verify.yml` therefore prints the hash it computed beside these values and does not fail
on a mismatch.

## 7. Bower

`bower install` **works on this host**: probe-3 shows `Running "bower:install" (bower) task` /
`Installed bower packages` / `Copied packages to E:\NEXORA\omega-web\build\lib`, and 252 `lib/` entries
land in the artifact. **VERIFIED**. Therefore the plan's vendoring step (ladder "put ~25 components into
`omega-web/lib/`") is **not executed** — it would add megabytes to git to solve a problem that does not
exist here. `omega-web/grunt/aliases.coffee:9` keeps `'bower'` in the `default` list, and
`omega-build/package.json:12` keeps `bower install` in `deps`. **VERIFIED**, unchanged.

The two `git://` dependencies in `omega-web/bower.json:37` (`angular-spectrum-colorpicker`) and
`omega-web/bower.json:43` (`zero-dependencies`) resolved anyway. **VERIFIED** — the probe observed the
success; the mechanism (system git still serving that protocol for these fetches) was **INFERRED**, and the
inference is **false**, twice over. Measured 2026-11-05 while writing `.github/workflows/verify.yml`: `git
ls-remote git://github.com/zero-peak/zero-dependencies.git HEAD` times out from this host
(`fatal: unable to connect to github.com: … errno=timed out`, exit 128). The deduction made next — that
probe-3 therefore succeeded only because `omega-web/bower_components/` was already installed — is what probe-5
falsified.

**probe-5: the cold install works, and no `git://` URL is ever fetched.** With `bower_components` moved aside
(kept, not deleted), `bower install` exits **0** and writes all 26 components, `angular-spectrum-colorpicker`
resolving to `e22ff9184a` and `zero-dependencies` to `4de5f14a04` — the same commits the https `ls-remote`
reports. The string `git://` occurs **zero times** in the capture: bower rewrites the declared endpoint to
`https://github.com/<owner>/<repo>.git` before it shells out to git, so the dead transport in `bower.json` is
never handed to a program that cannot use it. Adding
`url."https://github.com/".insteadOf git://github.com/` to the process's git config changes nothing about the
outcome (identical resolution lines), and `git ls-remote` of the raw URL still fails — the rewrite helps a
caller that passes the literal URL to git, which bower is not. **VERIFIED**
(`docs/audit/raw/probe-5-bower-cold-install.txt`).

Two things follow. The CI step that sets the rewrite stays, but its description moves from "the build needs it"
to "defence in depth, measured as unnecessary for Bower" — the step's own comment in
`.github/workflows/verify.yml` now says so, and §11 records the measurement. And the
reproducibility question narrows honestly rather than closing: 21 of the 26 components resolved from this
host's Bower cache at `C:\Users\dms\AppData\Local\bower` — the five that came off the network include both
`git://`-declared deps, which is the part that was in doubt — so an empty-cache runner still has 21 fetches
nobody has observed here (see the registry). `--config.storage.cache=<fresh dir>` did not relocate the store,
which is why probe-5 is a cold `bower_components` and not a cold runner.

This does **not** change the verdict of §7 — the task works here and vendoring is still not needed. Full
measurements: `docs/audit/02-toolchain-decay-and-risk.md` §1.1.

Reproducibility on *another* machine is **UNVERIFIED**, and probe-5 narrowed it rather than closing it: a cold
`bower_components` installs here, while `package-lock.json` stays gitignored (`.gitignore:3`), there is no root
lockfile, Bower is unpinned by the same logic, 21 of its components came off this host's cache, and no clean
machine or container was tested. `tools/verify-vendor.mjs` is the in-repo guard: it derives every `lib/...`
reference from the jade/html/coffee/overlay sources and fails if a path is missing under `lib/`. **VERIFIED**
(it runs; it does not prove a clean machine can *produce* `lib/`).

## 8. What this iteration changed in product files

Exactly two files, one line each, both in `grunt/` configuration (never in `src/`):

- `omega-pac/grunt/coffeelint.coffee:11` → `max_line_length: level: 'warn'` **VERIFIED**
- `omega-target-chromium-extension/grunt/coffeelint.coffee:11` → `max_line_length: level: 'warn'` **VERIFIED**

No source line was re-wrapped; no runtime behaviour changed; each is independently revertable. **VERIFIED**.

**Decision taken 2026-11-05** (task #10; this paragraph was the pending item): the `TZ=` fragility.
`omega-pac/test/conditions.coffee:468` builds `new Date("01 Feb 2016 #{time}")` deliberately in local time for
`TimeCondition`/`WeekdayCondition` under lolex, and the honest fix is an explicit UTC in the test — which changes
test semantics and is therefore still **not** done. The alternative floated below, a two-line wrapper in
`omega-pac/Gruntfile.coffee:1` (`process.env.TZ ?= 'Europe/London'` next to the existing `require`), was
considered and **rejected**: it would apply the test's zone to every task in the project, `build` included.
What exists instead is runner-side — `.github/workflows/verify.yml` exports `TZ` for the step that runs
`grunt test`, so no legacy file changes and no product behaviour moves (London is UTC+0 for the dates the suite
builds, so the pinned floor does not shift). The prefix is no longer a deduction either: `cmd.exe //c "npm test"`
in `omega-pac` exits **1** with `"TZ" не является внутренней или внешней командой`, measured 2026-11-05.
Reasoning and rejected alternatives: `docs/audit/02-toolchain-decay-and-risk.md` §4.

## 9. Explicit "not fixed" list

Reproduced from plan §4, verbatim intent. Each item was considered and deliberately left alone in this
iteration; several are Phase-2 requirements, some are permanent non-goals.

| Item | Status | Why |
|---|---|---|
| Grunt 0.4 → anything migration | not fixed | the chain works (ladder A); migration is a mass refactor the master prompt forbids |
| CoffeeScript → TypeScript in `omega-*` | not fixed | same |
| Bower → npm-runtime | not fixed | `bower install` works here; conversion = full toolchain migration, out of scope |
| Upgrades: `chai`, `sinon`, `mocha`, `browserify`, `minifyify`, `uglify-js`, `coffeeify`, `grunt-contrib-*`, `load-grunt-config` | not fixed | every upgrade risks changing build output; pinned `minifyify@4.4.0` `EBADENGINE` stays as measured noise |
| `npm link` → npm workspaces | not fixed | the `link` chain is how the bundles are consumed today (`omega-build/package.json:13`) |
| CSP hardening in the manifests | not fixed | product behaviour + listing implications; specified in `31-security-model.md`, done in Phase 2 |
| Permission reduction (`<all_urls>`, `downloads`, `unlimitedStorage`) | not fixed | same |
| Raising `minimum_chrome_version` | not fixed | interacts with the `chrome.storage.session` finding (`docs/audit/05`) |
| Adding `manifest.key` | not fixed | an ID-pinning decision with listing consequences → ADR-0015 |
| Rewriting `PacGenerator` / `Conditions` / `RuleList` | not fixed | ADR-0013 keeps them as the selection plane |
| New UI tests | not fixed | no UI exists yet in this iteration |
| Restoring the 20 locales that are not shipped | not fixed | content work, ADR-0017 scopes it |
| NEXORA branding in `release.yml`, renaming `dist/*-release.zip` | not fixed | cut-over-level change, specified only |
| Installing Go | not done | not needed for Phase 0/1; the `connector` CI job exists but is disabled (`if: ${{ false }}`) |
| Live tests in CI | not done | require the real server and secrets; env-gated matrix in `docs/verification/` |
| Repairing the three dirty files | deliberately dropped | broken uncommitted edits + `vless-test.coffee`; patch saved outside the repo (see `docs/PROJECT-STATE.md` §7) |
| Giving `omega-web` / `omega-target-chromium-extension` tests | not done | out of scope; instead `verify.yml` prints `NOTE: <project> has 0 test files` so the gap stays visible |
| **The `SEC-1` code fix** (`transformValueKeys` leaking subscription `headers` into sync) | **not fixed, on purpose** | changing `transformValueForSync` without a test changes product behaviour; it is a Phase-2 requirement with a written contract (`packages/schema/envelope-keys.schema.json`) |
| The 6 failing legacy tests | not fixed | they are the **pinned regression floor**, not a Phase-1 defect |
| Cause of the 3 `requestPush` timeouts | **UNVERIFIED** | not investigated; recorded so a future maintainer does not read "8 passing" as healthy sync |

## 10. Verdict

**Ladder A.** The legacy chain builds and packages correctly on Node 24 with two one-line lint demotions;
the contract layer (`npm --prefix packages/schema run verify`) is green independently; and the two known
failing areas (legacy tests, clean-machine reproducibility) are named rather than hidden. Ladder B (an
isolated Node LTS for legacy builds) and ladder C (contract-only baseline) are **not needed and not
applied**.

**Re-verified end to end on 2026-11-05 (§11).** Everything a shell on this host can check reproduced from a
clean rebuild — the four-step chain, both pinned test floors to the individual test, all five gates, and every
artifact content assertion. One thing did not reproduce: the recorded sha256 of the two zips, which moves on a
same-machine rebuild (§6). One thing that was inference became measurement: Bower resolves the declared `git://`
endpoints over https by itself, so a cold install works without any transport rewrite (§7). One thing this
iteration still has not done: run anything in a browser. The runner half it named alongside that is answered by
§12, which tabulates seven runs on 2026-11-05 — two red, four green, one cancelled — under a rule that keeps later
green runs of the same shape out of the table, and so keeps this sentence true.

## 11. probe-4 and probe-5 — the iteration's own end-to-end check, 2026-11-05

Task #11 of the work order is the last deliverable, and it is a re-verification rather than a new claim: rebuild
the legacy chain, re-measure the artifact, re-run every gate, and leave the tree clean. Raw capture:
`docs/audit/raw/probe-4-e2e-re-verification.txt` (chain, floors, gates, artifacts) and
`docs/audit/raw/probe-5-bower-cold-install.txt` (transport).

| What was re-checked | Expected, from this report | Measured 2026-11-05 |
|---|---|---|
| `npm run deps` → `dev` → `grunt` → `grunt release` from `omega-build` | exit 0 each (probe-3) | exit 0 each, **VERIFIED** |
| `omega-pac` floor | 142 passing / 1 failing | 142 passing (95 ms) / 1 failing, same `omega-pac/test/profiles.coffee:52` assertion, **VERIFIED** |
| `omega-target` floor | 8 passing / 5 failing | 8 passing (6 s) / 5 failing, same 2 assertions + 3 timeouts, **VERIFIED** |
| Contract layer | 1217 checks green | green, `13 schemas / 19 command docs / 20 examples / 24 rejected / 112 frames`, **VERIFIED** |
| Citation, secret, vendor and backup gates | green | green; the scanner now counts 409 tracked files because `verify.yml` is one of them, **VERIFIED** |
| Artifact content | 353 / 5 999 353 / 252 `lib/` / 7 `_locales` / 742 272 B bundle | identical on every count, **VERIFIED** |
| Artifact hash | recorded 2026-11-04 | **different** — `73eee79d…` / `5ab2ca82…`, **VERIFIED**; see §6 |
| Working tree after the build | no build output tracked | no build output tracked, `dist/` and `tmp/` ignored, **VERIFIED** |
| `npm test` in each leaf project | POSIX prefix fails in `omega-pac` | fails in `omega-target` too, for a second reason (`grunt` has no bin shim there) — §5 |
| Cold Bower install | unknown; probe-3 was warm | works, exit 0, 26 components, no `git://` fetch at all — §7 |

Four documentation consequences, all applied in the same sitting as this section, because each is a claim this
report made less accurately than the measurement now allows:

1. §7's explanation of *why* probe-3's Bower step succeeded was wrong (it credited the pre-installed tree, and
   before that "system git still serving the protocol"); the corrected mechanism is bower's own endpoint
   normalisation.
2. §6's two recorded sha256 values are labelled as one-build observations rather than a floor, matching what
   `verify.yml` already does with them.
3. `.github/workflows/verify.yml`'s transport step is described in its own comment as defence in depth, not as
   a prerequisite.
4. `docs/architecture/20-target-repository-layout.md` §6 and §9 cited this file by line number; a probe round
   appends lines, so those cites are now `§6`/`§11` references and `docs/audit/99-baseline-report.md` joined
   `MUTABLE_CITATION_TARGETS` in `tools/check-doc-evidence.mjs`, the same way `docs/PROJECT-STATE.md` did.

**What task #11 asked for and this section does not provide.** The plan's end-to-end item continues into a
browser: load `dist/chromium-release.zip` unpacked, read `chrome://extensions` for errors, walk the six
options states, import `docs/verification/backup-from-3.5.2.json`, switch profile and confirm through
`chrome.proxy.settings.get`, then export from the running extension and diff that export against the shipped
fixture. None of that happened — no browser was launched in this iteration, and the fixtures remain
exporter-shaped rather than genuine `.bak` files (`docs/verification/live-matrix.md` LIVE-14, and §4 of
`docs/verification/README.md` for what each row would need first). Under
`docs/verification/live-env-contract.md`, an unexecuted row is `SKIPPED`, never `PASSED`, so `LIVE-01…24` are
untouched by this section, and §12 opens no browser either. The other half this section could not close — a GitHub
runner — is answered by §12: `.github/workflows/verify.yml` has run there, and §12 tabulates seven of those runs on
2026-11-05 — twice red, four times green, once cancelled by its own `concurrency` block — the runs it leaves out
being green executions of a shape already in the table.

## 12. `.github/workflows/verify.yml` on a GitHub runner — seven runs tabulated, 2026-11-05

§11 ended with the one thing this iteration had not done: get the file onto a runner. It is now on one, and the
answer came as two red runs before it came as a green one, the green one repeated, and the sixth of the seven is
neither red nor green but `cancelled` — by this file's own `concurrency` block, which had never had the chance to
fire until two pushes arrived inside one build. Every line quoted below is copied out of `gh run view <id> --log`, not re-derived from the
workflow source, and the extraction itself is kept as `docs/audit/raw/probe-8-runner-runs.txt` — those seven runs' job
conclusions as the API reports them, every `##[error]` annotation verbatim, each line quoted here beside the number
of times the runner printed it, and the PATH forms counted per run so the fix is visible in the numbers. One
timestamp quoted below is not a log line but run metadata — the `createdAt` of `42029386377`, from
`gh run view <id> --json createdAt` — and it is what identifies the cancellation as caused by this repository's own
next push rather than by anything the tree did.

| Run | Commit | Result | What it found |
|---|---|---|---|
| `42015170195` | `4307ff6` | **red** — `VER-06`, all three `contracts` cells | the commit-label guard rejects a label it accepts nothing else of: `ci(verify): …`, which is the subject form this repository's own history uses. The vocabulary existed only in `.github/workflows/verify.yml`; `docs/architecture/20-target-repository-layout.md` §6, the document that step cites, never stated it. A guard enforcing a rule its source does not declare is the defect, and the commit that tripped it was already published — so the document was fixed first and the list second (`a7627ae`). All three cells printed the same line: `##[error]subject "ci(verify): add the PR-blocking workflow, written against measured values" carries no conventional label (doc 20 §6).` The `needs: contracts` gate then held: `legacy-chain` never executed, so its log has no line for it and the API lists the job with the matrix expression still unexpanded. |
| `42017987418` | `a7627ae` | **red** — all three `legacy-chain` cells: `VER-08` on ubuntu, `VER-11` on both windows images | (a) `$GITHUB_WORKSPACE` on a windows runner is `D:\a\NEXORA\NEXORA` — that value appears 40 times in the run's log — and it is not a POSIX path, so the `PATH` entry built from it reached grunt as a mangled module path and mocha never started: `##[error]omega-pac produced no mocha summary; the run died before reporting.` followed by `Error: Cannot find module 'A:\NEXORA\NEXORA\omega-build\node_modules\grunt-cli\bin\grunt'` in both windows cells; (b) `VER-08` compared the zip's uncompressed total against a number recorded from a `core.autocrlf=true` checkout, so a correct build on a different image cannot match it: `##[error]dist/chromium-release.zip is 5974389 B, expected 5999353 B.` Both were reproduced on this host before the file changed — `docs/audit/raw/probe-7-grunt-path.txt` — the first as the same `Cannot find module` error with the same `\a\` segment resolved against this machine's Git install directory instead of a runner drive, the second as a measured 24 964-byte CR delta between a CRLF and an LF checkout of the same tree. |
| `42020617681` | `ceedabf` | **green** — 6 jobs passed, 1 skipped | the whole file, on `ubuntu-latest`/Node 22, `windows-latest`/Node 22 and `windows-latest`/Node 24. |
| `42025558933` | `890fde6` | **green** — same 6 passed, 1 skipped, no `##[error]` line anywhere in the log | the green result reproduced on the commit that records it, which is also the commit that adds a tracked file: `no secret-shaped content in 419 files` and `no PE, ELF or Mach-O bytes in 589 tracked files` where `ceedabf` printed 418 and 588, and `checked 2260 path:line citations, 487 path#pointer citations and 1287 doc-name references` where it printed `2252 / 487 / 1253`. Every legacy figure is unchanged — both floors, 353 entries, `lib/=252`, `_locales=7`, the same four zip byte counts — and the two artifacts still printed `NOTE: … does not match the recorded checksum`: twelve distinct sha256 values across these two runs — three per artifact, one per image, so no two runner images agree even on the same commit — and none of the twelve is §6's recorded hash. |
| `42027072602` | `c81ae7e` | **green** — the same 6 passed, 1 skipped, again no `##[error]` line | the third green in a row, and the run this table stops at. `c81ae7e` adds no tracked file, so every counter it prints is `890fde6`'s: `2260 / 487 / 1287`, `419 files`, `589 tracked files`, `checks run: 1217`, both floors, the same four zip byte counts (`1 901 079` / `1 901 087` / `1 901 116` / `1 901 124`) and three fresh checksums per artifact. Every push re-executes this file, so the table could grow forever; it stopped at `42027072602` because a fourth green of the same shape adds no new fact, and the record of the question §11 left open was complete at the point where the answer stopped changing. That convention is what the next two rows broke, and they are in this table because they are not greens of the same shape: the sixth is a run this repository cancelled with its own next push, which is a third outcome none of the first five could produce. What reopens the table is any run whose *shape* differs — red, or cancelled — not merely a further green. |
| `42029092102` | `f15acb8` | **cancelled** — 3 `contracts` cells and the ubuntu `legacy-chain` cell `success`, both windows `legacy-chain` cells `cancelled`, no `##[error]` line anywhere in the log | the first run whose cause is this repository rather than the tree: `5dee596` was pushed at 12:42:12Z, ~2 min into a run created at 12:40:06Z, and `.github/workflows/verify.yml:31-33` declares `concurrency.cancel-in-progress: true` on a group keyed to the ref, so the runner cancelled the two windows cells then in flight. The cancellation is a verdict, not a missing build: both cells had already printed both floors, both `OK: dist/chromium-release.zip matches the shape of doc 20 §5` and `OK: dist/firefox-release.zip …`, their `NOTE: … does not match the recorded checksum` pair and their two computed hashes each, and all three cells — ubuntu included — printed `Artifact nexora-verify-<image>-node<n>.zip successfully finalized`. The Node 22 cell finished its upload at 12:43:15Z, three seconds before its `Post job cleanup`. Measured consequence: a run id is not the evidence; the log is, and `cancel-in-progress` can throw away a green verdict the artifacts and output already support. |
| `42029386377` | `5dee596` | **green** — 6 passed, 1 skipped, no `##[error]` line | the fourth green, and the push that caused the row above. It adds no tracked file and no citation, so it prints `f15acb8`'s corpus figures exactly — `2260 / 487 / 1289`, `419 files`, `589 tracked files`, `checks run: 1217` — both floors, the same four zip byte counts (`5 974 389` / `5 974 444` on ubuntu, `5 974 401` / `5 974 456` on both windows images) and six more checksum NOTEs. This is the shape the convention above predicts will repeat, so nothing after it is tabulated unless it differs. |

### What the green runs measured

`contracts`, identically on all three images and in all seven tabulated runs, with the one exception the red rows
explain —
`42015170195` never printed the commit-subject line, because printing it is the step that run failed on: `checks run: 1217`, `OK: 13 schemas compile,
19 command docs conform, 20 examples valid, 24 invalid examples rejected, 112 protocol frames replayed`,
`OK: the index matches 20 ADR files, exactly.`, `SELFTEST OK: 13 cases`, plus the `VER-02`, `VER-03`, `VER-04`,
`VER-08` (binary-path) and `VER-10` guards,
`OK: git diff --check clean and every tracked blob is LF or binary in the index.` and
`OK: every commit subject in the range carries its label; 0 cut-over commit(s).`

The two corpus counters that scan the tree are the only lines that moved among these runs: `ceedabf` printed
`checked 2252 path:line citations, 487 path#pointer citations and 1253 doc-name references in docs/`,
`no secret-shaped content in 418 files` and `no PE, ELF or Mach-O bytes in 588 tracked files`, `890fde6` — the
commit that adds the capture this section points at — and `c81ae7e` printed `2260 / 487 / 1287`, `419 files` and `589 tracked
files`, and `f15acb8`, `5dee596` and `75bff0f` printed `2260 / 487 / 1289` beside those same two file counts. All three counters read `git ls-files`
and the current contents of `docs/`, so they are a property of the tree at the sha under test, which is why they are
quoted as a set and never pinned as a floor.

`legacy-chain`, on each of the three images, in every run whose legacy job ran the chain — the four tabulated greens
and, with a `cancelled` verdict rather than a `success` one, the two windows cells of `42029092102`:

- `OK: omega-pac at or above its floor -- 142 passing, 1 failing (pinned >= 142, <= 1).` and
  `OK: omega-target at or above its floor -- 8 passing, 5 failing (pinned >= 8, <= 5).` — §5's floors reproduced on
  a machine that had never seen this repository, which is what §5 left as the open question.
- `NOTE: omega-web has 0 test files` and the same for `omega-target-chromium-extension`, printed rather than passed
  quietly, as designed.
- `Running "bower:install" (bower) task` → `>> Installed bower packages`, then in the artifact
  `dist/chromium-release.zip: 353 entries, 5974389 B uncompressed, lib/=252, _locales=7` (ubuntu; windows is
  `5974401` / `5974456`) and `OK: dist/chromium-release.zip matches the shape of doc 20 §5.`
- `Created ../dist/chromium-release.zip (1901079 bytes)` / `(1901116 bytes)` on ubuntu, `(1901087)` / `(1901124)` on
  windows; `OK: 2 shipped backups load and compile in the legacy engine`, `SELFTEST OK: 4/4 broken envelopes
  rejected.`, `OK: every statically referenced vendor asset is present.`, and both backup inventories reproduced
  (`profiles=4 compiled=3 refused-by-engine=1 rules=2 evaluations=15` and
  `profiles=7 compiled=6 refused-by-engine=1 rules=11 evaluations=30`).
- Three `NOTE` lines, all of them the deliberate demotions of §6 and of run `42017987418`: the totals are not the
  recorded `5 999 353` / `5 999 408`, `js/omega_pac.min.js is 735547 B, not the recorded 742 272 B`, and neither
  checksum matches. **Shape is the floor; bytes are an observation.** The two images differ from each other by
  exactly 12 B in total and not at all in the bundle size; that difference is recorded, not explained — nothing in
  this iteration computed a per-file diff between an ubuntu and a windows build.
- The checksum NOTEs aggregate into the sharpest form of the same finding: across the five runs §12 tabulates whose
  legacy chain reached that step — `42020617681`, `42025558933`, `42027072602`, `42029092102` and `42029386377` — there
  are fifteen builds and thirty computed `sha256` values, two per build, and all thirty are distinct from each other and
  from §6's two recorded hashes. No runner image has ever reproduced another image's bytes, or its own. The aggregate
  grows by six with every further run of this file, which is why the finding is stated as a rule rather than as a count:
  §6's recorded hashes have not been matched by any build any image has produced, including the eighth run's.
- `continue-on-error` on the Node 24 cell was not needed: it is green for the same reason the other two are. The
  attribute itself is still unmeasured — `42017987418` had a red Node 24 cell and the jobs API called that cell
  `failure` anyway, so what a red cell inside an otherwise green workflow would report has not been seen.
  Every job prints GitHub's own `##[warning]Node.js 20 is deprecated … forced to run on Node.js 24` for
  `actions/checkout@v4` and `actions/setup-node@v4`; that is the runner telling us the action pins, not this file.

### What run six measured about the file's own concurrency block

The first five runs each had the runner to themselves, because each push waited for its run. `42029092102` is the
first time that was not true, and it measures the block at `.github/workflows/verify.yml:31-33` — a
`concurrency.group` keyed to the ref with `cancel-in-progress: true` — which had therefore never been observed doing
anything. What it does is narrower and worse than it sounds. The two windows cells were cancelled at ~12:43Z, and by
then they had printed both floors, both `OK: … matches the shape of doc 20 §5`, both checksum NOTEs and their own two
computed hashes each, and all three of the run's legacy cells had printed `Artifact … successfully finalized`; the
Node 22 cell finalised at 12:43:15Z, three seconds before its `Post job cleanup`. So the knob discards a *verdict*
while leaving the *evidence* in the log, and the run-level conclusion — `cancelled`, with no `##[error]` line anywhere
— says nothing about the tree. Two consequences this iteration accepts rather than fixes: a run id is not the
evidence and its log is, which is why a cancelled run is tabulated here at all; and two pushes inside one build cost
one verdict, so a push waits for its run instead of stacking behind the previous one. The other workflow in
`.github/workflows/` declares no `concurrency` block at all, so interruptibility is a property of this gate, chosen
in this file, not of the repository's CI as a whole.

### The eighth run, and why the corpus stopped counting executions

`75bff0f` is the commit that carries the two rows above, and pushing it executed this file an eighth time —
`42033189223`, conclusion `success`, six jobs passed and one skipped, no `##[error]` line anywhere in its log. Every
guard line it printed matches `42029386377` character for character: `2260 path:line citations, 487 path#pointer
citations and 1289 doc-name references`, `no secret-shaped content in 419 files`, `no PE, ELF or Mach-O bytes in 589
tracked files`, `checks run: 1217`, `353 entries`, `lib/=252`, `_locales=7`, both pinned floors (`142 passing, 1
failing` and `8 passing, 5 failing`), the two `0 test files` NOTEs on each image, `js/omega_pac.min.js is 735547 B,
not the recorded 742 272 B` twice per build, the six `does not match the recorded checksum` NOTEs with the six computed
hashes printed beside them, and the zip byte counts already tabulated — ubuntu `5 974 389` and `5 974 444`, windows
`5 974 401` and `5 974 456`. The third of the corpus counters is a property of the tree at the sha under test, as the
note above says, and the references this section adds move it, so the next execution prints `2260 / 487 / 1290` where
the eighth printed `1289` — a number changing, not a shape differing. It is a green of exactly the shape §12's
convention describes, so it opens no row, and this section is the last place in the corpus that counts runs rather than
naming that rule.

The reason is structural, not fatigue: **every commit that records a run of this workflow is itself a commit that
runs it.** A sentence asserting a literal number of executions is therefore false at the moment the push carrying it
lands — that is how five became seven, and seven became eight, with the correction travelling in the commit it
describes. What stays true is a claim about the table plus a claim about the rule, which is what §12 now states: the
seven runs tabulated, their verdicts, and the condition under which any later run joins them. The eighth run measured
that condition holding. Its log is not added to the raw capture for the same reason the run is not added to the table:
nothing in it is new.

### Where the `lib/` files actually come from

The 252 `lib/` entries in both zips are Bower output, and `omega-target-chromium-extension/lib` has no tracked
files at all (`git ls-files` there prints nothing). `bower install` appears in `omega-build/package.json:12` only
inside the `deps` script, which this workflow does not run — the chain gets them from grunt:
`omega-web/Gruntfile.coffee:1` loads `load-grunt-config`, whose per-file config directory makes
`omega-web/grunt/aliases.coffee:9` the last task of the default sequence, and that task's config is
`omega-web/grunt/bower.coffee:5` — `targetDir: 'build/lib/'`. So `npm run build` is what fetches the components and
places them, and the runner log above is the proof: `Running "bower:install"` appears inside the hub build, and the
zip ends up carrying 252 of them. Two consequences: the "do not vendor" decision recorded in
`docs/PROJECT-STATE.md` §1 row 2 turns out to be sound in a way it was not measured to be before — the artifact does
depend on Bower, and a runner with no cache behind it produced the same 252 `lib/` entries this host did; and the
`Install the five legacy roots` step installs npm only, deliberately, because the `bower install` half of
`omega-build/package.json:12` is performed by the build itself.

### What the green runs do not do

They do not move a single `VER-*` row. Three reasons, all mechanical:

1. `packages/schema/verification.json` has no state meaning "closed" — `states.values` is `no-code`, `no-check`,
   `no-run`, `no-fixture`, `needs-human` — so closing a row means deleting the row **and** the labelled sentence it
   is anchored to, in `docs/architecture/20-target-repository-layout.md` §13, in docs 21/25/26/27/29, in the ADRs and
   in `docs/verification/live-matrix.md`, then re-deriving `marker.counted` (202) and re-asserting the state
   distribution in `packages/schema/scripts/verify.mjs`, which hard-codes 53/13/2/2/5 of 75. That is a commit of its
   own, with its own measured numbers, not a side effect of a status update.
2. For six of the rows, the run is evidence but not the named closing mechanism. `VER-04` and `VER-05` are queued
   against a `check-layout-guards` tool that has not been written; `VER-02` and `VER-03` scanned roots that are still
   empty of the code they will hold, so their green is a guard that executed, not a boundary that held under
   pressure; `VER-09` needs a store, and `VER-57` needs a CSP load test the workflow does not contain.
   `VER-06`, `VER-08`, `VER-10` and `VER-11` are the four whose stated condition — a green run of this file — is now
   met; `docs/architecture/39-verification-matrix.md` §8 is the order they close in, and closing them is the
   bookkeeping described in point 1 above.
3. Nothing in this run touched the browser. `LIVE-01…24` are still `SKIPPED`, `docs/verification/live-matrix.md` is
   unchanged, and the `connector` job is still `if: ${{ false }}`, so it reports SKIPPED on every run including this
   one.

## UNVERIFIED registry

- Whether the blank-options-page consequence of the 352-file zip was observed in a real browser (reasoned,
  not photographed).
- Root cause of the three `OptionsSync #requestPush` 2 000 ms timeouts.
- Whether the 354-vs-353 file difference is exactly the `manifest` task substitution (per-file diff not computed).
- Clean-machine reproducibility of `npm install` + `bower install`. **Narrowed twice 2026-11-05.** The Bower half is
  measured on a cold `bower_components` (§7) and works, and both halves are now measured on machines that had never
  held this tree: three GitHub runner images, in the four green runs §12 tabulates and a fifth whose two windows cells
  did the same work
  under a `cancelled` verdict, installed all five roots with no lockfile in
  git, built through the hub, and produced a zip carrying the same 252 `lib/` entries this host's warm cache did (§12). What remains unmeasured is
  narrower still — a *user* machine, whose image differs from GitHub's, and whether npm resolution there picks the
  same versions; the runner proves a set resolves and builds, not that it is the same set everywhere.
- ~~Whether `git://` Bower deps resolve on GitHub-protocol-restricted networks.~~ **Closed 2026-11-05, twice.**
  The first closure ("they do not resolve from this host; the https `insteadOf` rewrite is the mitigation") was
  itself wrong: raw `git ls-remote git://…` does time out here, but bower never uses that transport — it resolves
  the declared endpoint as `https://github.com/…\.git`, and a cold install of both deps succeeds with no rewrite
  set (§7). The question that survives is the narrower one immediately above.
- That the `minifyify@4.4.0` `EBADENGINE` warning is harmless on every Node ≥ 20 (it was harmless on 24.22.1, and
  probe-4 re-ran the whole chain under the same warning).
- ~~Determinism of the two zips across machines.~~ **Answered, and worse than expected: not on one machine.**
  A 2026-11-05 rebuild of the same tree reproduced every content assertion but produced different sha256 values
  (§6). The archive stores per-entry build timestamps, so no recorded hash is a floor; `verify.yml` prints the
  hash beside these values rather than asserting them. Whether the *uncompressed content* of the two builds is
  byte-identical is **UNVERIFIED** — probe-4 overwrote the 2026-11-04 zips before anyone thought to keep them,
  which is the lesson to carry into Phase 2: keep the previous artifact when re-measuring an artifact. **Partly
  answered by §12 from the other side:** two runner images of the same commit produced archives whose uncompressed
  totals differ by 12 B (`5 974 389`/`5 974 444` on ubuntu against `5 974 401`/`5 974 456` on windows, both with 353
  entries and the same 735 547 B bundle), so content is already known *not* to be byte-identical across images; the
  per-file diff that says which 12 bytes is still not computed. §12 aggregates it as a rule rather than a count: every
  runner build yet observed produced two `sha256` values distinct from every other build's and from the two recorded
  here, and each further run of that file adds six more to the set.
- Whether any of `LIVE-01…24` passes: no browser was launched in this iteration (§11). Under
  `docs/verification/live-env-contract.md` those rows are `SKIPPED`, which is not this report claiming success.
- That `.github/workflows/verify.yml` behaves as written on a GitHub runner. **Answered 2026-11-05: it does, after
  two failures that were defects in the file rather than in the tree (§12).** Every `run:` block had already been
  executed on this host, including `check_zip` against the real artifacts and `run_floor` against the real
  `omega-pac` suite, and each guard was re-run against a synthetic violation to prove it can fail; what this host
  could not exercise — the `actions/*` steps, the runner context, the `GITHUB_*` inputs — is what `42015170195` and
  `42017987418` then found, and `42020617681` then passed; `42025558933` repeated that green on `890fde6`, the commit
  that records it, which is the only proof in this iteration that a fix to the file was not a fluke. One part of the
  file still has no green run behind it: `continue-on-error` on the Node 24 cell. The `needs` gate, the matrix and
  `cancel-in-progress` are all now measured, each by a run that had to fail or be cancelled to show it. The consequence for the queue is recorded in §12 and is
  deliberately not a state change: `docs/architecture/39-verification-matrix.md` still carries every `VER-*` row at
  the state its data gives it, because closing one means deleting the row and the labelled line it is anchored to,
  and re-deriving the counts that check the deletion.
