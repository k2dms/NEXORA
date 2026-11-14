# 01 — Repository and build graph

> Purpose: record, with line-level evidence, how the five CoffeeScript projects are wired into one
> Grunt-Hub build, what each project's grunt task table contains, the end-to-end artifact chain, every
> `.gitignore`, the missing root manifests, the `BUILD=release` gate, and the concrete name-coupling
> sites that make the `omega-*` directories expensive to move. Phase-0 audit only; no product code.

Tags: **VERIFIED** = read the file and it says so. **INFERRED** = deduced from code read here, the
mechanism is not directly observable without a run. **UNVERIFIED** = assumed, not checked. See the
registry at the end. Host facts are taken from `docs/PROJECT-STATE.md` §1 and are not re-probed.

---

## 1. The five projects and their roles

| Project | Role | Leaf bundle it emits | Evidence |
|---|---|---|---|
| `omega-pac` | PAC / profile / condition engine | `omega_pac.min.js`, `index.js` | `omega-pac/grunt/browserify.coffee:15`, `omega-pac/grunt/browserify.coffee:4` **VERIFIED** |
| `omega-target` | Storage/sync abstraction over `omega-pac` | `omega_target.min.js`, `index.js` | `omega-target/grunt/browserify.coffee:15`, `omega-target/grunt/browserify.coffee:4` **VERIFIED** |
| `omega-web` | AngularJS options + popup UI | `build/` tree (jade→html, less→css, coffee→js, bower→lib) | `omega-web/grunt/aliases.coffee:2-10` **VERIFIED** |
| `omega-target-chromium-extension` | MV2/MV3 packaging, zip release | `build/` + `dist/*-release.zip` | `omega-target-chromium-extension/grunt/aliases.coffee:10`, `omega-target-chromium-extension/grunt/compress.coffee:6` **VERIFIED** |
| `omega-build` | Hub orchestrator (no source of its own) | nothing — drives the other four | `omega-build/Gruntfile.coffee:14-22` **VERIFIED** |

`omega-locales` is a data directory (27 `*.po` locale trees), not a Grunt project; `po2crx` compiles
7 of them into `_locales`. **VERIFIED** (`omega-target-chromium-extension/grunt/po2crx.coffee:4-17`;
directory count observed 27).

---

## 2. Build graph: hubs, `file:` dependencies, `npm link`

### 2.1 The hub fan-out (in `omega-build`)

The hub is the only place that knows the sibling names. It declares a module list and one glob:

- **VERIFIED** `submodules = ['omega-pac', 'omega-target', 'omega-web', 'omega-target-*']` at
  `omega-build/Gruntfile.coffee:2`. The last entry is a literal glob token, not a directory name.
- **VERIFIED** per-module hub target `src: "../#{module}/Gruntfile.*"` at
  `omega-build/Gruntfile.coffee:10`.
- **VERIFIED** a broader catch-all `src: "../*/Gruntfile.*"` at `omega-build/Gruntfile.coffee:7`
  (used only by `watch`, `omega-build/Gruntfile.coffee:22`). Any sibling directory that ever grows a
  `Gruntfile.*` is silently swept into `grunt watch`. Forward hazard, not a current defect. **INFERRED**
- **VERIFIED** `hubAll` builds `hub:<module>:<task>` for each module at
  `omega-build/Gruntfile.coffee:12`, and `default`/`test` are registered from it at
  `omega-build/Gruntfile.coffee:20-21`.
- **VERIFIED** `grunt-hub` is the engine: `omega-build/package.json:9` and loaded at
  `omega-build/Gruntfile.coffee:18`.

Because Grunt runs a registered task list in order, `grunt` (default) fans out strictly:
`omega-pac → omega-target → omega-web → omega-target-chromium-extension`, matching the array order at
`omega-build/Gruntfile.coffee:2`. **INFERRED** from array order + grunt task-list semantics (no run
observed here).

### 2.2 `file:` (relative-path) inter-project dependencies

| Dependent | Depends on | Path | Evidence |
|---|---|---|---|
| `omega-target` | `omega-pac` | `../omega-pac` | `omega-target/package.json:26` **VERIFIED** |
| `omega-web` | `omega-pac` | `../omega-pac` (devDependency) | `omega-web/package.json:21` **VERIFIED** |
| `omega-target-chromium-extension` | `omega-target` | `../omega-target` | `omega-target-chromium-extension/package.json:25` **VERIFIED** |
| `omega-target-chromium-extension` | `omega-web` | `../omega-web` | `omega-target-chromium-extension/package.json:26` **VERIFIED** |
| `omega-target-chromium-extension` | `omega-pac` | `../omega-pac` | `omega-target-chromium-extension/package.json:27` **VERIFIED** |

`omega-build` itself has no `file:` dependency (`omega-build/package.json:5-10`) — it reaches the others
only through the hub glob and shell `cd`. **VERIFIED**

### 2.3 Where `npm link` is used (the `dev` scripts)

Each project's `dev` script self-links and links its upstreams, so a bundle built in one project is
resolvable by the next at its *own* project root:

- **VERIFIED** `omega-pac/package.json:31` `"dev": "npm link"`
- **VERIFIED** `omega-target/package.json:32` `"dev": "npm link omega-pac && npm link"`
- **VERIFIED** `omega-web/package.json:24` `"dev": "npm link omega-pac && npm link"`
- **VERIFIED** `omega-target-chromium-extension/package.json:34`
  `"dev": "npm link omega-target && npm link omega-web && npm link omega-pac"`

`npm run dev` in `omega-build` simply re-runs those four in graph order:
`omega-build/package.json:13`. **VERIFIED**

### 2.4 How the bundle is consumed at the root through a `node_modules` symlink

This is the subtle coupling. The downstream grunt config reads the **prebuilt** bundle from the
**upstream project root**, reached through the `node_modules/omega-*` symlink that `npm link` created:

- **VERIFIED** `omega-web/grunt/copy.coffee:4` copies
  `node_modules/omega-pac/omega_pac.min.js` → `build/js/omega_pac.min.js`.
- **VERIFIED** `omega-target-chromium-extension/grunt/copy.coffee:9-10` copies
  `node_modules/omega-target/omega_target.min.js` → `build/js/`.
- **VERIFIED** `omega-target-chromium-extension/grunt/browserify.coffee:36` aliases
  `'omega-pac': 'omega-pac/omega_pac.min.js'` and `:43` does
  `noParse: [require.resolve('omega-pac/omega_pac.min.js')]` — resolved **at config load**, so if
  `omega-pac/omega_pac.min.js` is absent the whole chromium Gruntfile fails to load (this is the
  abort chain recorded in `docs/PROJECT-STATE.md` §1.3). **VERIFIED** for the `require.resolve` call;
  **INFERRED** for the abort behaviour (not re-run here).
- **VERIFIED** the `browser` field redirects the *Node* resolution to a shim so `npm` never tries to
  run CoffeeScript server-side: `omega-target/package.json:29`,
  `omega-target-chromium-extension/package.json:31`, `omega-pac/package.json:26-29`.

---

## 3. Grunt task table per project

Each project's `Gruntfile.coffee` is only `module.exports = require('load-grunt-config')`
(`omega-pac/Gruntfile.coffee:1`, `omega-target/Gruntfile.coffee:1`, `omega-web/Gruntfile.coffee:1`),
so the real config lives in `grunt/*.coffee`. The chromium project's Gruntfile additionally loads
`grunt-po2crx` and registers `manifest` inline (`omega-target-chromium-extension/Gruntfile.coffee:2-11`).

| Project | `grunt/` config files | `default` | `test` | `release` |
|---|---|---|---|---|
| `omega-pac` | aliases, browserify, coffeelint, mochaTest, watch | `coffeelint` + `browserify` (`omega-pac/grunt/aliases.coffee:2-5`) | `mochaTest` (`omega-pac/grunt/aliases.coffee:6`) | not defined |
| `omega-target` | aliases, browserify, coffeelint, mochaTest, watch | `coffeelint` + `browserify` (`omega-target/grunt/aliases.coffee:2-5`) | `mochaTest` (`omega-target/grunt/aliases.coffee:6`) | not defined |
| `omega-web` | aliases, autoprefixer, bower, coffee, coffeelint, copy, jade, less, mochaTest, ngAnnotate, watch | `copy jade less autoprefixer coffeelint coffee bower` (`omega-web/grunt/aliases.coffee:2-10`) | `mochaTest` (`omega-web/grunt/aliases.coffee:11`) | not defined |
| `omega-target-chromium-extension` | aliases, browserify, coffee, coffeelint, compress, copy, mochaTest, po2crx, watch + inline `manifest` | `coffeelint browserify coffee copy po2crx` (`omega-target-chromium-extension/grunt/aliases.coffee:2-8`) | `mochaTest` (`omega-target-chromium-extension/grunt/aliases.coffee:9`) | `manifest` + `compress` (`omega-target-chromium-extension/grunt/aliases.coffee:10`) |

All **VERIFIED** from the `aliases.coffee` reads and the `grunt/` directory listings.

Note (not in `PROJECT-STATE`): `omega-web/grunt/ngAnnotate.coffee` defines a target
`'build/js/omega.ngmin.js': 'build/js/omega.js'` (`omega-web/grunt/ngAnnotate.coffee:6`) but **no alias
references `ngAnnotate`** — it is not in `default` (`omega-web/grunt/aliases.coffee:2-10`) nor in
`watch` (`omega-web/grunt/watch.coffee`). So `omega.ngmin.js` is not produced by `grunt` here.
**VERIFIED** that no alias lists it; **INFERRED** the file is therefore dead under the default chain.
**This does not contradict `docs/PROJECT-STATE.md`; it adds to it.** Report-back item.

---

## 4. Artifact chain, end to end

Sequence (all `omega-build/package.json`): `deps` at `:12`, `dev` at `:13`, `build` at `:14`
(= `grunt`), `release` at `:15` (= `cd ../omega-target-chromium-extension && grunt release`). **VERIFIED**

1. **`npm run deps`** — installs every project, plus `bower install` for `omega-web`; the whole thing
   is a `cd ../omega-* && npm install` shell chain (`omega-build/package.json:12`). **VERIFIED**
2. **`npm run dev`** — runs each project's `npm link` chain in graph order
   (`omega-build/package.json:13`), creating the `node_modules/omega-*` symlinks §2.4 relies on. **VERIFIED**
3. **`npm run build`** = `grunt` → hub `default` fan-out in the order of §2.1. `omega-pac`/`omega-target`
   emit `*.min.js` bundles (`browserify:browser`), `omega-web` assembles `build/`, the chromium project
   copies `omega-web/build` (`omega-target-chromium-extension/grunt/copy.coffee:4`) and the upstream
   bundles into its own `build/`. **VERIFIED** for each copy/alias; **INFERRED** for global ordering.
4. **`npm run release`** = `grunt release` in the chromium project → `manifest` then `compress`
   (`omega-target-chromium-extension/grunt/aliases.coffee:10`). **VERIFIED**
   - `manifest` reads `overlay/manifest.json` and `overlay/manifest-firefox.json`, filters `downloads`
     out of `permissions` (`omega-target-chromium-extension/Gruntfile.coffee:7` and `:10`), and writes
     `tmp/chromium/manifest.json` (`:8`) and `tmp/firefox/manifest.json` (`:11`). **VERIFIED**
   - `compress` writes `../dist/chromium-release.zip` (`omega-target-chromium-extension/grunt/compress.coffee:6`)
     and `../dist/firefox-release.zip` (`:22`), each from `build/**` excluding the two raw manifests
     (`!manifest.json`, `!manifest-firefox.json`) plus the filtered `tmp/<browser>/manifest.json`.
     **VERIFIED** (`omega-target-chromium-extension/grunt/compress.coffee:10-18` and `:26-34`).

### 4.1 The `BUILD=release` gate for `minifyify`

Minification is not on by default. `browserify` adds the `minifyify` plugin **only** when the
environment equals `release`:

- **VERIFIED** `omega-pac/grunt/browserify.coffee:22` `if process.env.BUILD == 'release'`.
- **VERIFIED** `omega-target/grunt/browserify.coffee:22` same test.
- **VERIFIED** `omega-target-chromium-extension/grunt/browserify.coffee:23` and `:38` same test for the
  two chromium bundles.

**What happens without it:** the `else` branch disables the plugin (`omega-pac/grunt/browserify.coffee:22-24`), so the
bundle is emitted **un-minified** (a `debug: true` browserify build, `omega-pac/grunt/browserify.coffee:12`).
The 742 272 B `js/omega_pac.min.js` baseline in `docs/PROJECT-STATE.md` §1.4 is therefore only produced
when `BUILD=release` is exported. **INFERRED** — `npm run build` (`omega-build/package.json:14`) does not
set `BUILD`, so the plain `grunt` path yields unminified bundles; only `grunt release` combined with an
exported `BUILD=release` yields the shipped minified artifact. Neither the env export nor a live build is
observed here. **UNVERIFIED** for the byte-for-byte size outcome.

---

## 5. Every `.gitignore` and what it excludes

| File | Excludes | Evidence |
|---|---|---|
| repo `.gitignore` | `node_modules`, `bower_components`, `package-lock.json`, `*-release.zip`, `*.swp`, `.env`, `.env.*` but keep `.env.example` | `.gitignore:1-10` **VERIFIED** |
| `omega-pac/.gitignore` | `/index.js`, `/omega_pac.min.js` (build outputs at project root) | `omega-pac/.gitignore:1-2` **VERIFIED** |
| `omega-target/.gitignore` | `/index.js`, `/omega_target.min.js` | `omega-target/.gitignore:1-2` **VERIFIED** |
| `omega-web/.gitignore` | `/build`, `/tmp` | `omega-web/.gitignore:1-2` **VERIFIED** |
| `omega-target-chromium-extension/.gitignore` | `/index.js`, `/omega_target_*.min.js`, `/tmp`, `/build`, `/build*`, `/release.zip`, `/web-ext-artifacts` | `omega-target-chromium-extension/.gitignore:1-8` **VERIFIED** |

Five project `.gitignore` files + the repo root one = six tracked ignore files. (The extra `.gitignore`
files found under `omega-web/bower_components/*` are vendored, derived trees, not project config —
`node_modules`/`bower_components` are themselves gitignored at `.gitignore:1-2`.) **VERIFIED**

---

## 6. Missing root manifests

- **VERIFIED** No root `package.json` and no root `Gruntfile`. A directory listing of `E:\NEXORA` returns
  neither file (`ls package.json Gruntfile*` → none). The only root build entry point is the `omega-build`
  project. This is deliberate: ADR-0018 keeps the legacy grunt graph unreachable from the repo root so new
  NEXORA tooling cannot trip it by accident (`docs/PROJECT-STATE.md` §3).

---

## 7. Name-coupling sites that make moving `omega-*` expensive

Plan claimed "≥15". Concrete enumeration below, each verified against a line I read. Row 21 (the CircleCI
cache keys, `.circleci/config.yml:14-34`) left the table on 2026-11-14: PROJECT-STATE §9.3 deleted the file it
cited, and a coupling to a file that no longer exists is no coupling. The census this section once counted
(23 rows, 13 files) is 22 rows over 12 files since.

| # | Coupling | Evidence |
|---|---|---|
| 1 | `submodules` array literal | `omega-build/Gruntfile.coffee:2` |
| 2 | hub catch-all glob `../*/Gruntfile.*` | `omega-build/Gruntfile.coffee:7` |
| 3 | per-module glob `../#{module}/Gruntfile.*` | `omega-build/Gruntfile.coffee:10` |
| 4 | `deps` `cd ../omega-* && npm install` chain | `omega-build/package.json:12` |
| 5 | `dev` `cd ../omega-* && npm run dev` chain | `omega-build/package.json:13` |
| 6 | `release` `cd ../omega-target-chromium-extension` | `omega-build/package.json:15` |
| 7 | `file:` dep `omega-pac` in target | `omega-target/package.json:26` |
| 8 | `file:` dep `omega-pac` in web | `omega-web/package.json:21` |
| 9 | `file:` deps target/web/pac in chromium | `omega-target-chromium-extension/package.json:25-27` |
| 10 | `npm link` self+upstream in pac | `omega-pac/package.json:31` |
| 11 | `npm link` in target | `omega-target/package.json:32` |
| 12 | `npm link` in web | `omega-web/package.json:24` |
| 13 | `npm link` in chromium | `omega-target-chromium-extension/package.json:34` |
| 14 | copy reads `node_modules/omega-pac/omega_pac.min.js` | `omega-web/grunt/copy.coffee:4` |
| 15 | copy cwd `../omega-web/build` | `omega-target-chromium-extension/grunt/copy.coffee:4` |
| 16 | copy reads `node_modules/omega-target/omega_target.min.js` | `omega-target-chromium-extension/grunt/copy.coffee:9-10` |
| 17 | browserify alias `'omega-pac': 'omega-pac/omega_pac.min.js'` | `omega-target-chromium-extension/grunt/browserify.coffee:36` |
| 18 | browserify `require.resolve('omega-pac/omega_pac.min.js')` at load | `omega-target-chromium-extension/grunt/browserify.coffee:43` |
| 19 | watch globs `node_modules/omega-web/build` + `node_modules/omega-target` | `omega-target-chromium-extension/grunt/watch.coffee:12-15` |
| 20 | watch reads `node_modules/omega-pac/omega_pac.min.js` | `omega-web/grunt/watch.coffee:10` |
| 22 | `po2crx` reads `../omega-locales/**` for 7 locales | `omega-target-chromium-extension/grunt/po2crx.coffee:5-17` |
| 23 | copy pulls `COPYING`/`AUTHORS` from repo root `..` | `omega-target-chromium-extension/grunt/copy.coffee:26-27` |

All **VERIFIED** (each line read). **True count: 23 distinct coupling sites across 11 files** — exceeds
the "≥15" claim. Rows 9, 21, 22, 23 each bundle several literal occurrences on the cited line/range.

---

## 8. Packaging boundary (current facts only)

The connector living outside the extension zip is a **forward-looking requirement**, not a current fact,
and is recorded as such elsewhere. What is true **today**:

- **VERIFIED** `compress` archives exactly the chromium/firefox `build/` tree plus the filtered manifest
  (`omega-target-chromium-extension/grunt/compress.coffee:7-18`). There is no Go binary, no connector, no
  native-messaging host anywhere in that tree (consistent with `docs/PROJECT-STATE.md` §1.8).
- **VERIFIED** The release names are still `chromium-release.zip` / `firefox-release.zip`
  (`omega-target-chromium-extension/grunt/compress.coffee:6` and `:22`); a `nexora-chromium-<ver>.zip`
  rename is a cut-over change, not made here.

---

## UNVERIFIED registry

| Claim | Why it could not be checked here |
|---|---|
| §2.1 global grunt fan-out order `pac → target → web → chromium` | Inferred from the `submodules` array order and grunt task-list semantics; no `grunt` run executed (builds are forbidden this iteration). |
| §3 `omega.ngmin.js` is not produced | Verified no alias references `ngAnnotate`; "not produced" is an inference from the absent wiring, not an observed build output. |
| §4.1 unminified-vs-minified byte outcome | The `BUILD=release` code gate is verified, but the resulting artifact sizes were not re-measured (no build run); the 742 272 B figure is quoted from `docs/PROJECT-STATE.md`, not re-derived. |
| §4 `dist/*.zip` contents (353-file / 252-lib baseline) | Carried from `docs/PROJECT-STATE.md` §5; the working tree was not rebuilt or unzipped here. |
