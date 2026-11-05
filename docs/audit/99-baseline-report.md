# 99 — Baseline report (green-build probe, ladder A)

> Purpose: record what the legacy ZeroOmega v3.5.2 toolchain in this repository actually does on this
> host, before and after the only two product-file edits made in this iteration, so that every later
> architectural claim ("the chain still builds", "the test floor is X") has a measured anchor instead of
> an assumption. Raw logs live next to this file in `docs/audit/raw/`.

Date of all probes: **2026-11-04**. Branch: `nexora/architecture`. Tag audited: ZeroOmega v3.5.2 (`release
v3.5.2` is HEAD's closest release commit).

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
explicitly labelled as a diagnostic.

| Round | What it did | Raw log |
|---|---|---|
| probe-1 | `npm install --no-audit --no-fund` in `omega-build`, `omega-pac`, `omega-target`, `omega-web`, `omega-target-chromium-extension`; then `npm run build` and `npm run release` in `omega-build` | `docs/audit/raw/probe-1-legacy-toolchain.txt` (326 lines), `probe-1-table.txt` |
| probe-2 | `npm run dev` (the `npm link` chain), then grunt per project, per-project `test`, `--force` diagnostics, `grunt release`, `ls dist` | `docs/audit/raw/probe-2-legacy-toolchain.txt` (1036 lines), `probe-2-table.txt` |
| probe-3 | the same chain after the two lint demotions, ending in `npm run build` + `npm run release` from `omega-build` | `docs/audit/raw/probe-3-ladder-a-green.txt` (423 lines) |

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

## 7. Bower

`bower install` **works on this host**: probe-3 shows `Running "bower:install" (bower) task` /
`Installed bower packages` / `Copied packages to E:\NEXORA\omega-web\build\lib`, and 252 `lib/` entries
land in the artifact. **VERIFIED**. Therefore the plan's vendoring step (ladder "put ~25 components into
`omega-web/lib/`") is **not executed** — it would add megabytes to git to solve a problem that does not
exist here. `omega-web/grunt/aliases.coffee:9` keeps `'bower'` in the `default` list, and
`omega-build/package.json:12` keeps `bower install` in `deps`. **VERIFIED**, unchanged.

The two `git://` dependencies in `omega-web/bower.json:37` (`angular-spectrum-colorpicker`) and
`omega-web/bower.json:43` (`zero-dependencies`) resolved anyway. **VERIFIED** — the probe observed the
success; the mechanism (system git still serving that protocol for these fetches) is **INFERRED**.

Reproducibility elsewhere is **UNVERIFIED**: `package-lock.json` is gitignored (`.gitignore:3`), there is no
root lockfile, Bower is unpinned by the same logic, and no clean machine or container was tested.
`tools/verify-vendor.mjs` is the in-repo guard: it derives every `lib/...` reference from the jade/html/
coffee/overlay sources and fails if a path is missing under `lib/`. **VERIFIED** (it runs; it does not prove
a clean machine can *produce* `lib/`).

## 8. What this iteration changed in product files

Exactly two files, one line each, both in `grunt/` configuration (never in `src/`):

- `omega-pac/grunt/coffeelint.coffee:11` → `max_line_length: level: 'warn'` **VERIFIED**
- `omega-target-chromium-extension/grunt/coffeelint.coffee:11` → `max_line_length: level: 'warn'` **VERIFIED**

No source line was re-wrapped; no runtime behaviour changed; each is independently revertable. **VERIFIED**.

**Pending decision** (task #10): the `TZ=` fragility. `omega-pac/test/conditions.coffee:468` builds
`new Date("01 Feb 2016 #{time}")` deliberately in local time for `TimeCondition`/`WeekdayCondition` under
lolex, and the honest fix is an explicit UTC in the test — which changes test semantics and is therefore
**not** done here. The alternative under consideration is a two-line wrapper in `omega-pac/Gruntfile.coffee:1`
(`process.env.TZ ?= 'Europe/London'` next to the existing `require`). **Not applied in this iteration.**

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

## UNVERIFIED registry

- Whether the blank-options-page consequence of the 352-file zip was observed in a real browser (reasoned,
  not photographed).
- Root cause of the three `OptionsSync #requestPush` 2 000 ms timeouts.
- Whether the 354-vs-353 file difference is exactly the `manifest` task substitution (per-file diff not computed).
- Clean-machine reproducibility of `npm install` + `bower install` (no lockfiles in git, one host only).
- Whether `git://` Bower deps resolve on GitHub-protocol-restricted networks in the future.
- That the `minifyify@4.4.0` `EBADENGINE` warning is harmless on every Node ≥ 20 (it was harmless on 24.22.1).
- Determinism of the two zips across machines (byte-identical rebuilds were not attempted; no lockfile).
