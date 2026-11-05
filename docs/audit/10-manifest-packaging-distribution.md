# 10 — Manifests, packaging and distribution

> Purpose: document, field by field, the two manifests that actually ship (Chromium MV3 vs Firefox MV3), how the
> build turns those templates into the packaged `manifest.json`, what the release workflow publishes, why the
> CircleCI config is dead, and which packaging facts constrain how NEXORA may be distributed and upgraded.
> Everything is read statically from the repo; nothing was loaded in a browser.

Every claim is tagged **VERIFIED** / **INFERRED** / **UNVERIFIED**. `docs/PROJECT-STATE.md` §1 (version/ID
continuity), §2.6 (artifact baseline) and the "not fixed" list at `docs/PROJECT-STATE.md:282-289` are the settled
findings this document expands; the refinements found are reported in §2 and §3 and do not contradict them.

---

## 1. The two shipped manifests, field by field

There is exactly one pair of hand-authored manifests, both in the overlay directory of the Chromium target —
`omega-target-chromium-extension/overlay/manifest.json` (Chromium) and
`omega-target-chromium-extension/overlay/manifest-firefox.json` (Firefox). There is **no** `src/manifest.json`,
no `firefox/manifest.json`, and no template elsewhere (Glob for `**/manifest*.json` under the target returns only
`overlay/`, `build/` and `tmp/` copies). **VERIFIED**

| Field | `omega-target-chromium-extension/overlay/manifest.json` | `omega-target-chromium-extension/overlay/manifest-firefox.json` | Consequence | Tag |
|---|---|---|---|---|
| `manifest_version` | `3` `:2` | `3` `:2` | both are MV3 | **VERIFIED** |
| `name` | `"Proxy SwitchyOmega 3 (ZeroOmega)"` `:3` | identical `:3` | legacy brand lives *here*, not in the CI file (§4) | **VERIFIED** |
| `version` | `"3.5.2"` `:4` | `"3.5.2"` `:4` | the single version source; CircleCI reads it (`§5`) | **VERIFIED** |
| `description` | `__MSG_manifest_app_description__` `:5` | identical `:5` | resolved from `_locales` (see doc 08) | **VERIFIED** |
| `icons` | 16/24/32/48/64/128 `:6-12` | identical `:6-12` | same icon set | **VERIFIED** |
| `default_locale` | `"en"` `:14` | `"en"` `:14` | fallback locale folder must ship | **VERIFIED** |
| `action` | present `:15` (with `default_icon` `:16-20`) | present `:15` | MV3 renamed `browser_action`→`action`; **no `browser_action` key remains** (grep) | **VERIFIED** |
| `action.default_popup` | `"popup-iframe.html"` `:23` | `"popup/index.html"` `:23` | the popup divergence documented in doc 08; the service worker overrides Chromium's value at runtime (`omega-target-chromium-extension/overlay/x-background.js:26-30`) | **VERIFIED** |
| `background` | `"service_worker": "x-background.js"` `:26` | `"scripts": ["x-background.js"]` `:26` | Chromium uses an MV3 service worker; Firefox uses an event page — different lifetimes, same entry script | **VERIFIED** |
| `background.type` | `"module"` `:27` | `"module"` `:27` | ES-module import graph in `x-background.js` | **VERIFIED** |
| `minimum_chrome_version` | `"88"` `:29` | `"88"` `:29` | ignored by Firefox; on Chromium it predates `chrome.storage.session` (see PROJECT-STATE §2, `omega-target-chromium-extension/src/module/options.coffee:106-140`) — **not fixed**, `docs/PROJECT-STATE.md:286` | **VERIFIED** |
| `options_page` | `"options.html"` `:30` | **absent** — `minimum_chrome_version` (`:29`) is immediately followed by `options_ui` (`:30`) | Firefox relies on `options_ui` only | **VERIFIED** |
| `options_ui` | `page/browser_style/open_in_tab` `:31-34` | same keys `:30-33` | opens in its own tab | **VERIFIED** |
| `commands` | `_execute_action` `Alt+Shift+O` `:37` | `_execute_action` `:36` | keyboard command uses the MV3 `_execute_action` id, consistent with `action` | **VERIFIED** |
| `permissions` | proxy/tabs/alarms/storage/unlimitedStorage/webRequest/**webRequestAuthProvider**/contextMenus `:40-47` | proxy/tabs/alarms/storage/unlimitedStorage/webRequest/**webRequestBlocking**/contextMenus `:39-46` | Firefox keeps blocking webRequest (`:45`); Chromium keeps the auth-provider response capability (`:46`) | **VERIFIED** |
| `host_permissions` | `["<all_urls>"]` `:49` | `["<all_urls>"]` `:48` | broadest possible host grant — an NEXORA permission-reduction target (`docs/PROJECT-STATE.md:286`) | **VERIFIED** |
| `browser_specific_settings.gecko` | **absent** | `id "suziwen1@gmail.com"`, `strict_min_version "112.0"` `:51-52` | the AMO identity lives here; see §3 | **VERIFIED** |

Absent from **both** manifests (grep across every non-`node_modules` JSON in the repo returns no match for any of
these keys): `content_security_policy`, `web_accessible_resources`, `key`, `update_url`. This is confirmed by
reading the whole bodies `omega-target-chromium-extension/overlay/manifest.json:2-49` and
`omega-target-chromium-extension/overlay/manifest-firefox.json:2-52`. **VERIFIED**

* **No extension CSP.** With MV3 the extension pages get Chromium's default isolated CSP; the repo adds none, so
  the Angular `ng-csp` stance in `omega-web/src/options.jade:2` is a UI-layer convention, not a manifest
  guarantee. Adding a stricter CSP is explicitly on the not-fixed list (`docs/PROJECT-STATE.md:286`). **VERIFIED**
* **No `web_accessible_resources`.** The iframe popup (`overlay/popup-iframe.html`) loads same-origin, so nothing
  needs to be exposed cross-origin; a NEXORA overlay that reaches into page contexts would first have to add this
  key. **INFERRED**
* **No `key` field.** Consequence is treated in §6; do not add it casually — `docs/PROJECT-STATE.md:286` lists
  "adding `key`" as deliberately *not* done, and `docs/PROJECT-STATE.md:414` points at the upgrade path it
  interacts with. **VERIFIED**

---

## 2. How a template becomes the packaged manifest

The `manifest` grunt task is registered inline in the target's Gruntfile, `omega-target-chromium-extension/Gruntfile.coffee:5-11`:

1. read `overlay/manifest.json` (`:6`),
2. `manifest.permissions = manifest.permissions.filter (p) -> p != 'downloads'` (`:7`),
3. `grunt.file.write('tmp/chromium/manifest.json', JSON.stringify(manifest))` (`:8`) — note `JSON.stringify` with
   no indent, so the generated file is a single compact line (measured 1067 B vs the 1470 B pretty template),
4. the same three steps for `overlay/manifest-firefox.json` → `tmp/firefox/manifest.json` (`:9-11`, measured 1122 B).

**Refinement to the brief.** The brief describes `downloads` as "added then stripped by the `manifest` grunt task".
The filter does exist (`:7`, `:10`), but **neither shipped template declares `downloads`** — a repo-wide grep for
`"downloads"` in JSON returns zero hits. The filter is therefore a no-op on today's manifests, a vestige of an
earlier SwitchyOmega grant. **VERIFIED** (mechanism present, effect null)

Packaging is `grunt release` = `['manifest', 'compress']` (`omega-target-chromium-extension/grunt/aliases.coffee:10`,
default task `:2-7` does *not* run it). `compress` (`omega-target-chromium-extension/grunt/compress.coffee`) is the
only zip producer:

| Zip | Archive | Contents rules | Tag |
|---|---|---|---|
| Chromium | `../dist/chromium-release.zip` `:6` | `cwd: build`, `src: ['**', '!manifest.json', '!manifest-firefox.json']` (`:9-12`) **plus** `cwd: tmp/chromium/`, `src: 'manifest.json'` (`:15-16`) | **VERIFIED** |
| Firefox | `../dist/firefox-release.zip` `:22` | same exclusion `:25-28` **plus** `cwd: tmp/firefox/`, `src: 'manifest.json'` (`:31-32`) | **VERIFIED** |

The `!manifest.json` / `!manifest-firefox.json` exclusions matter because the overlay copy task
(`omega-target-chromium-extension/grunt/copy.coffee:19-23`) blindly copies `overlay/**/*` into `build/`, so both
*templates* land in `build/`. Without the exclusion the zip would ship the wrong/second manifest; the generated
`tmp/<browser>/manifest.json` is what actually gets injected (`mode: 'zip'` at `:2-3`). **VERIFIED**

Build chain that reaches this: `omega-build/package.json:14` `build` = `grunt` (hub over `../*/Gruntfile.*`), and
`:15` `release` = `cd ../omega-target-chromium-extension && grunt  release`. **VERIFIED**

*Secondary packaging observation:* the browserify minifier only engages under `process.env.BUILD == 'release'`
(`omega-target-chromium-extension/grunt/browserify.coffee:23`, and the same gate in
`omega-pac/grunt/browserify.coffee:22`). A grep across `.github/workflows` and every `package.json` shows **nothing
sets `BUILD=release`**, so the `.min.js` bundles that ship in the zip are CoffeeScript output that is *not*
minified (measured: `omega_target_chromium_extension.min.js` 5694 lines, indented source). This inflates the zip
and is a NEXORA build-hardening candidate, not a manifest bug. **VERIFIED**

---

## 3. Extension identity and listings

`README.md` is the only place the three store identities are declared:

| Store | Citation | Identifier used | Tag |
|---|---|---|---|
| Chrome Web Store | `README.md:4` | `pfnededegaaopdmhkdmcofjmoldfiped` (path segment) | **VERIFIED** |
| Microsoft Edge Add-ons | `README.md:6` | `dmaldhchmoafliphkijbfhaomcgglmgd` | **VERIFIED** |
| Firefox Add-ons (AMO) | `README.md:8` | slug `zeroomega` only — no numeric add-on id in the URL | **VERIFIED** |

The AMO slug is cosmetic; the *authoritative* Firefox identity is
`omega-target-chromium-extension/overlay/manifest-firefox.json:51` (`browser_specific_settings.gecko.id = "suziwen1@gmail.com"`). For Chromium the
identity is derived by the store from the packed-CRX signature because no `key` is present in
`omega-target-chromium-extension/overlay/manifest.json:2-49` — see §6. **INFERRED**

Offline-install artifacts named in `README.md:18-19` (CRX, Releases page) and `README.md:28-29` (XPI) live in the
upstream ZeroOmega GitHub release area, distinct from `dist/*-release.zip` produced locally
(`README.md:109`). **VERIFIED**

---

## 4. The release workflow

`.github/workflows/release.yml` is the live publisher:

* Trigger: `on: push: tags: - 'v*'` `:3-6` — the release is **tag-driven**, not manual. **VERIFIED**
* Runner / scope: `runs-on: ubuntu-latest` `:10`, `permissions: contents: write` `:11-12` (needed to publish). **VERIFIED**
* Toolchain: `actions/checkout@v3` `:16`, `actions/setup-node@v3` with `node-version: '22'` `:19-21`. **VERIFIED**
* Build: `working-directory: 'omega-build'` `:24` running `npm run deps` / `dev` / `build` / `release` `:26-29`. **VERIFIED**
* Checksums: `sha256sum dist/chromium-release.zip` and `…firefox-release.zip` written to `.sha256` sidecars `:33-34`. **VERIFIED**
* Publish: `softprops/action-gh-release@v1` `:37` uploading the four files listed at `:40-43` (two zips + two
  checksums), authenticated with `GITHUB_TOKEN` `:45`. **VERIFIED**

**Refinement to the brief.** The brief says release.yml "still carries ZeroOmega branding". Reading the whole file,
`release.yml` contains **no literal `ZeroOmega` or `SwitchyOmega` string** — it only references `dist/*-release.zip`
and creates a GitHub Release on whichever repo the tag is pushed to. The surviving legacy brand lives instead in
`omega-target-chromium-extension/overlay/manifest.json:3` (the `name`), the fixed basenames in `omega-target-chromium-extension/grunt/compress.coffee:6,22`, and the CircleCI filenames
(`§5`). `docs/PROJECT-STATE.md:287-288` lists "NEXORA branding in `release.yml`" as a not-fixed *action item*
(add branding / rename artifacts there), which is consistent — it is work not yet done, not text present now. **VERIFIED**

---

## 5. The dead CircleCI config

`.circleci/config.yml` is retained but cannot pass:

* Base image `circleci/node:7.10` `:5` — that tag is far below the repo's own floor (`README.md:91` says node 20.x
  required; CI uses 22, `.github/workflows/release.yml:21`). Modern CircleCI no longer offers it. **VERIFIED**
* It restores/saves **six** dependency caches keyed on each package manifest `:12-35` — the same fragility
  PROJECT-STATE §2 flags (`docs/PROJECT-STATE.md:120`). **VERIFIED**
* Global tool install `sudo npm install -g grunt-cli@1.2.0 bower web-ext json` `:37`; bare `grunt` (not `npx`) `:64`. **VERIFIED**
* Version string built from the manifest: `base_ver=$(json -f "overlay/manifest.json" version)` `:71`,
  `FULL_VER="${base_ver}-${CIRCLE_BUILD_NUM}.ci.${commit_rev}"` `:73`. **VERIFIED**
* Fatal break: after `grunt release` `:81` it does `mv release.zip "$dest_file"` `:83`, but `grunt release` now
  writes `../dist/chromium-release.zip` (`omega-target-chromium-extension/grunt/compress.coffee:6`), not a CWD `release.zip`. The `mv` can only fail, so
  the Chromium package step is permanently red; the Firefox step `:88-95` (`web-ext build` then
  `mv web-ext-artifacts/*.zip` `:94`) depends on the same dead branch. **VERIFIED**

Consequence for NEXORA: this file is not a working distribution path and is pending deletion
(`docs/PROJECT-STATE.md:402`, "delete in a separate commit or keep and mark dead"). Do not treat it as evidence of
current release behaviour. **VERIFIED**

---

## 6. Distribution constraints that bind NEXORA

1. **No arbitrary local binary inside the extension zip.** The zip is built solely from `build/**` minus the
   manifest templates plus the generated manifest (`omega-target-chromium-extension/grunt/compress.coffee:9-16`). PROJECT-STATE §2 is explicit that
   "The Connector is **never** inside the extension zip" (`docs/PROJECT-STATE.md:139`): the Go+Xray connector is
   a separate downloadable/installed component (ADR-0019, referenced at `docs/PROJECT-STATE.md:385`), not an
   extension payload. Any NEXORA feature needing a local executable must ship it out-of-band. **VERIFIED**
2. **Adding `manifest.key` is a decision, not a cleanup.** Neither manifest has a `key` (§1). Pinning one would
   fix the Chromium extension ID to a self-derived value, changing how the store and unpacked-load identity are
   computed. PROJECT-STATE lists "adding `key`" as intentionally *not* done (`docs/PROJECT-STATE.md:286`) and ties
   ID continuity to version continuity (`docs/PROJECT-STATE.md:46`); the ADR is the authority, so this document
   only records the constraint and does not restate it. **VERIFIED**
3. **Update continuity is the hard constraint.** Because NEXORA "continue[s] the ZeroOmega line (3.6.x → 4.x),
   same extension ID, existing CWS/Edge/AMO listings" (`docs/PROJECT-STATE.md:46`), the packaged `manifest.json`
   must keep producing the *same* identity the stores already know. For Firefox that identity is
   `omega-target-chromium-extension/overlay/manifest-firefox.json:51` (gecko.id) and must not change. **VERIFIED**
4. **Version string must remain a single dotted value.** CircleCI parsed it (`:71`), the GitHub Release trigger is
   the tag (`.github/workflows/release.yml:6`), and the in-product upgrade logic compares it numerically
   (`omega-target-chromium-extension/src/coffee/background.coffee:41`). Any NEXORA version scheme change must keep
   `omega-target-chromium-extension/overlay/manifest.json:4` comparable. **VERIFIED**

**Update-continuity requirements, as observations:**

* The upgrade handler reads `chrome.runtime.getManifest().version` on `reason 'update'` and only wipes storage when
  the version transition straddles `3.3.0` (`omega-target-chromium-extension/src/coffee/background.coffee:37-46`). Changing the version line to `4.x` keeps
  `compare '3.3.0', currentVersion, '>'` false, so the wipe trap does not fire for NEXORA-era updates
  (`docs/PROJECT-STATE.md:414`). **INFERRED** (arithmetic, not executed)
* Both manifests must bump together. The build writes them from two separate templates
  (`Gruntfile.coffee:6,9`); a NEXORA PR that edits only one would desynchronise the shipped versions. There is no
  single-source version file to guard against this. **VERIFIED**
* Artifact renaming (`dist/*-release.zip` → `nexora-…`) is a cut-over-level change touching `compress.coffee` *and*
  `.github/workflows/release.yml:29-39` together (`docs/PROJECT-STATE.md:140`); until then the zip basenames are load-bearing for
  `.github/workflows/release.yml:33-34` / `:40-43` and the CircleCI legacy (`.circleci/config.yml:82`). **VERIFIED**
* Store-side publishing is not automated here: `release.yml` only creates a GitHub Release
  (`:37,40-43`). Uploading `dist/chromium-release.zip`/`firefox-release.zip` to CWS/Edge/AMO is a manual step no
  file in the repo performs, so NEXORA continuity depends on the *same* listing credentials and IDs being reused
  (`docs/PROJECT-STATE.md:46`). **INFERRED**

---

## UNVERIFIED registry

| # | Claim | Why unverified | Status |
|---|---|---|---|
| 1 | `web_accessible_resources` truly unnecessary at runtime | only proved absent from the manifest, not load-tested in Chromium | UNVERIFIED |
| 2 | No-extension-CSP is safe under MV3 isolated world | depends on live Chromium policy, not observable statically | UNVERIFIED |
| 3 | Firefox event page vs Chromium service-worker lifetime effects on subscription alarms | requires running both browsers | UNVERIFIED |
| 4 | AMO accepts the zip because gecko.id alone defines identity | publishing behaviour is external to the repo | UNVERIFIED |
| 5 | `omega-target-chromium-extension/src/coffee/background.coffee:41` version comparison never wipes on 3.6.x→4.x updates | arithmetic reasoned, not executed | UNVERIFIED |
| 6 | Unminified `.min.js` is the reason the zips are ~1.9 MB | sizes measured (`dist/` ~1 903 305 / 1 903 342 B) but cause not bisected | UNVERIFIED |
| 7 | CircleCI `mv release.zip` is the *only* fatal break in the file | other steps (`node:7.10`, bare `grunt`) are also stale; full run not possible | UNVERIFIED |
| 8 | Store update continuity requires identical zip manifest fields beyond the ID | store policies unverifiable from source | UNVERIFIED |
