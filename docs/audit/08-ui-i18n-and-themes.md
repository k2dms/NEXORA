# 08 — UI, i18n and themes

> Purpose: audit the presentation layer of ZeroOmega v3.5.2 exactly as it ships — the `$script` bootstrap, the
> registered UI states, the controller inventory, the grunt pipeline that produces the bundle, the `tr` →
> `chrome.i18n` chain and its 27-vs-7 locale gap, the theme system and `-customCss`, and the two divergent
> popup branches — so that the parity obligations of the NEXORA Svelte layer are observations taken from code,
> not assumptions. No product code is changed by this document.

Reading rules: every claim is tagged **VERIFIED** (I opened the cited line), **INFERRED** (derived from code I
read but not stated by it), or **UNVERIFIED** (not established here). LOC figures were produced by `wc -l` on
the working tree during this audit; the accompanying citation anchors the declaration line I actually opened.

---

## 1. Bootstrap mechanics: `$script`, then `themes.json`, then `angular.bootstrap`

The options page is not a self-contained document. `omega-web/src/options.jade:2` binds the whole `<html>`
element to `MasterCtrl` and `omega-web/src/options.jade:15` keeps `<body>` at `display: none` until `options`
exists on the scope — so a page that never finishes bootstrapping is a *blank page*, not an error page. **VERIFIED**

Load order, all of it hand-written in the template and in `src/coffee/options.coffee`:

| Step | Evidence | What it does | Tag |
|---|---|---|---|
| 1. Static `<script>` tags | `omega-web/src/options.jade:80-86` | moment, idb-keyval, **jszip**, `omega_debug.js`, `log_error.js`, `script.js` (the loader) and `js/options.js` are the only scripts the page itself pulls in | **VERIFIED** |
| 2. `$script` graph | `omega-web/src/coffee/options.coffee:2-5` | loader → jquery → `js/omega_pac.min.js` (the PAC engine bundle) → FileSaver/Blob; ladda/spin are nested callbacks | **VERIFIED** |
| 3. Module declaration before Angular | `omega-web/src/coffee/options.coffee:13-19` | after `angular-loader` is ready it declares module `omega` with 11 dependency modules, then loads `js/omega.js` (the compiled UI bundle) once `omega-pac` is ready | **VERIFIED** |
| 4. Angular + plugins | `omega-web/src/coffee/options.coffee:39-44` | `angular.min.js` after loader+jquery; `angular-ui-router` and `angular-sanitize` after angular | **VERIFIED** |
| 5. ngLocale pick | `omega-web/src/coffee/options.coffee:46-61` | an 8-entry map from `navigator.language` to an `angular-locale_*.js` file, defaulting to `en-us` | **VERIFIED** |
| 6. Theme catalogue fetch, then bootstrap | `omega-web/src/coffee/options.coffee:63-70` | *all 12 named bundles* must be `$script.ready` before one `$http.get('./lib/themes/themes.json')`; the response is frozen in as the `themes` constant and only then `angular.bootstrap document, ['omega']` | **VERIFIED** |

Two consequences the new UI inherits whether it likes them or not:

* **Bootstrap is gated on a static asset fetch.** `omega-web/src/coffee/options.coffee:68-70` makes the whole Angular app depend on
  one XHR against the extension's own `lib/`. That request is the single point of failure for the options
  page, and its failure mode is the blank page of §1 (`omega-web/src/options.jade:15`). **INFERRED**
* **`jszip` is dead weight.** `omega-web/src/options.jade:82` loads `lib/zero-dependencies/jszip/jszip.min.js`;
  a repo-wide search for `jszip|JSZip` over `omega-web/src/` returns exactly that one line and nothing else, and
  `jszip` appears in no `.coffee` file under `src/`. **VERIFIED** (grep over `omega-web/src`, 1 hit)

## 2. The startup states (the `:48-76` range)

The range `:48-76` in the brief does **not** belong to `omega-web/src/options.coffee`: no such file exists (the
entry script is `omega-web/src/coffee/options.coffee`, 71 lines, so `:76` is out of range), and the state
registry lives in `omega-web/src/omega/app.coffee:48-75`. **VERIFIED**

`omega-web/src/omega/app.coffee:48-75` registers **seven** states, not six:

| State | URL | Template | Controller | Lines |
|---|---|---|---|---|
| `ui` | `/ui` | `partials/ui.html` | *(commented out)* | `omega-web/src/omega/app.coffee:49-52` |
| `general` | `/general` | `partials/general.html` | *(commented out)* | `omega-web/src/omega/app.coffee:53-56` |
| `io` | `/io` | `partials/io.html` | `IoCtrl` | `omega-web/src/omega/app.coffee:57-60` |
| `builtin` | `/builtin` | `partials/builtin.html` | `BuiltinCtrl` | `omega-web/src/omega/app.coffee:61-64` |
| `theme` | `/theme` | `partials/theme.html` | *(none — `MasterCtrl` scope)* | `omega-web/src/omega/app.coffee:65-67` |
| `profile` | `/profile/*name` | `partials/profile.html` | `ProfileCtrl` | `omega-web/src/omega/app.coffee:68-71` |
| `about` | `/about` | `partials/about.html` | `AboutCtrl` | `omega-web/src/omega/app.coffee:72-75` |

`ui` and `general` are deliberately controller-less (`omega-web/src/omega/app.coffee:52` and `omega-web/src/omega/app.coffee:56` are commented-out
`controller:` lines): they are pure form bindings onto `options["-…"]` keys held by `MasterCtrl`. **VERIFIED**

Six of the seven are reachable from the sidebar; `profile` is reached per profile
(`omega-web/src/options.jade:46-49`), and `about` is the document title link
(`omega-web/src/options.jade:19`). `omega-web/src/omega/app.coffee:40-46` makes `/about` the fallback and, on an empty path,
restores `omegaTarget.lastUrl()` — the last visited URL, persisted in `localStorage`
(`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:117-123`). **VERIFIED**

> **Contradiction reported:** `docs/PROJECT-STATE.md` §6 step #11 and the plan both say "walk all 6 states"
> while pointing at `omega-web/src/omega/app.coffee:48-76`. The code registers 7 states in that block. The defensible reading is
> "6 sidebar sections + 1 detail state (`profile/*name`)". The number 6 should not be repeated without that
> qualification, and the path should be `app.coffee`, not `options.coffee`. **VERIFIED**

## 3. Controller inventory

`wc -l` over the UI sources. Outliers (> 200 LOC) are flagged; they are the parity surface.

| File | LOC | Purpose | Tag |
|---|---|---|---|
| `omega-web/src/omega/controllers/switch_profile.coffee:1` | **468** | Switch/Virtual profile editor: rule list source view, drag-sort, rule import, header editor for the attached subscription | **VERIFIED** |
| `omega-web/src/omega/controllers/master.coffee:1` | **405** | Root scope of the page: options copy/dirty/apply, PAC + rule-list export, rename/replace/new profile modals, `updateProfile`, theme switching, first-run guide | **VERIFIED** |
| `omega-web/src/coffee/popup.coffee:1` | **393** | The *AngularJS* popup (`omegaPopup` module, `popup.html`), not the shipped `popup/index.html` | **VERIFIED** |
| `omega-web/src/omega/controllers/io.coffee:1` | **233** | Import/export screen plus the whole sync-backend form (Gist/WebDAV detection, enable/disable/reset) | **VERIFIED** |
| `omega-web/src/omega/controllers/profile.coffee:1` | 116 | Per-profile shell: delete/rename guards, `exportRuleList` handler slot (`omega-web/src/omega/controllers/profile.coffee:88-92`), `scriptable` flag | **VERIFIED** |
| `omega-web/src/omega/controllers/fixed_profile.coffee:1` | 111 | Fixed profile: per-scheme host/port, bypass list editor, proxy auth | **VERIFIED** |
| `omega-web/src/omega/controllers/pac_profile.coffee:1` | 57 | PAC profile incl. its own request-headers editor (`omega-web/src/omega/controllers/pac_profile.coffee:53-57`) | **VERIFIED** |
| `omega-web/src/omega/controllers/builtin.coffee:1` | 40 | Built-in profile list + the `-proxy` global bypass settings | **VERIFIED** |
| `omega-web/src/omega/controllers/about.coffee:1` | 27 | About/log/debug section | **VERIFIED** |
| `omega-web/src/omega/controllers/quick_switch.coffee:1` | 15 | Quick-switch profile ordering (nested `ng-controller`, `omega-web/src/partials/ui.jade:54`) | **VERIFIED** |
| `omega-web/src/omega/controllers/rule_list_profile.coffee:1` | 2 | Two lines: publishes `ruleListFormats` into `omega-web/src/partials/profile_rule_list.jade:1` | **VERIFIED** |
| `omega-web/src/omega/app.coffee:1` | 108 | Module constants + state registry + `$exceptionHandler` + `downloadFile` | **VERIFIED** |
| `omega-web/src/omega/directives.coffee:1` | 54 | `inputGroupClear`, `omegaUpload`, `omegaIp2str` | **VERIFIED** |
| `omega-web/src/omega/filters.coffee:1` | 29 | `profiles`, **`tr`**, `dispName` | **VERIFIED** |
| `omega-web/src/coffee/omega_decoration.coffee:1` | 125 | Separate `omegaDecoration` module: profile icons/order + 4 directives | **VERIFIED** |
| `omega-web/src/coffee/options.coffee:1` | 71 | §1 bootstrap | **VERIFIED** |
| `omega-web/src/coffee/options_guide.coffee:1` / `omega-web/src/coffee/switch_profile_guide.coffee:1` / `omega-web/src/coffee/log_error.coffee:1` | 53 / 77 / 10 | Shepherd tour for the switch-profile form; console error hook | **VERIFIED** |

Controllers total 1274 LOC; the four outliers hold 57 % of it. `ui.coffee` and `general.coffee` do not exist —
those two screens are driven entirely from `MasterCtrl`'s scope. **VERIFIED** (`wc -l` totals over the table)

Per-profile-type sub-forms are separate partials with their own `ng-controller`:
`omega-web/src/partials/profile_fixed.jade:1`, `omega-web/src/partials/profile_pac.jade:1`,
`omega-web/src/partials/profile_rule_list.jade:1`, `omega-web/src/partials/profile_switch.jade:1`. **VERIFIED**
27 jade partials exist under `omega-web/src/partials/` (counted with `ls | wc -l`). **VERIFIED**

## 4. Build pipeline for the UI

`omega-web/grunt/aliases.coffee:2-10` is the whole default task: `copy, jade, less, autoprefixer, coffeelint,
coffee, bower`. There is no browserify/concat step for the UI; the page is assembled from loose files at
runtime (§1). **VERIFIED**

| Stage | Config | Input → output | Tag |
|---|---|---|---|
| jade → HTML | `omega-web/grunt/jade.coffee:8-11` | `src/*.jade` → `build/*.html` (`options.jade` → `options.html`) | **VERIFIED** |
| jade → partials | `omega-web/grunt/jade.coffee:15-18` | `src/partials/*.jade` → `build/partials/*.html`; these are `templateUrl` targets, e.g. `omega-web/src/omega/app.coffee:59` | **VERIFIED** |
| LESS → CSS (tmp) | `omega-web/grunt/less.coffee:4` | `src/less/options.less` → `tmp/css/options.css` (and `:7` for the popup) | **VERIFIED** |
| autoprefixer | `omega-web/grunt/autoprefixer.coffee:6-8` | `tmp/css/**.css` → `build/css/` — hence `omega-web/src/options.jade:11` links `css/options.css` | **VERIFIED** |
| coffee → per-file JS | `omega-web/grunt/coffee.coffee:4-7` | `src/coffee/**/*.coffee` → `build/js/<name>.js` (one file each: `options.js`, `popup.js`, …) | **VERIFIED** |
| coffee → **single UI bundle** | `omega-web/grunt/coffee.coffee:10` | `src/omega/**/*.coffee` → one `build/js/omega.js`, loaded at `omega-web/src/coffee/options.coffee:19` | **VERIFIED** |
| bower vendor | `omega-web/grunt/bower.coffee:5` | components installed straight into `build/lib/` (25 vendored dirs measured in `build/lib`) | **VERIFIED** |
| PAC engine | `omega-web/grunt/copy.coffee:4` | `node_modules/omega-pac/omega_pac.min.js` → `build/js/omega_pac.min.js` — a **prebuilt** artifact read through the `npm link` symlink | **VERIFIED** |
| themes/img/popup | `omega-web/grunt/copy.coffee:6-9`, `:15-18` | `lib/**` → `build/lib/`, `img/**`, `src/popup/**` → `build/popup/` | **VERIFIED** |
| ngAnnotate | `omega-web/grunt/ngAnnotate.coffee:6` | configured to emit `build/js/omega.ngmin.js`, **but `ngAnnotate` is absent from `omega-web/grunt/aliases.coffee:2-10`** and no file references `ngmin` anywhere in the repo: dead task, dead output | **VERIFIED** |
| assembly | `omega-target-chromium-extension/grunt/copy.coffee:4-6` | the entire `omega-web/build` tree is copied into the extension's `build/`, and `:20-23` layers `overlay/**` (manifests, `x-background.js`, `popup-iframe.*`) on top | **VERIFIED** |

`omega-web/grunt/mochaTest.coffee:6` globs `test/**/*.coffee` and `omega-web/test/` does not exist (`ls` fails),
so the UI project's `npm test` succeeds against zero test files. **VERIFIED**

## 5. i18n: template `tr` → `chrome.i18n.getMessage`, and the 27-vs-7 gap

The chain, both ends:

1. Template: `{{'options_theme' | tr}}` (e.g. `omega-web/src/options.jade:39`). **VERIFIED**
2. Filter: `omega-web/src/omega/filters.coffee:24` binds `tr` to `omegaTarget.getMessage` with no other logic;
   the popup module repeats the identical one-liner at `omega-web/src/coffee/popup.coffee:4`. **VERIFIED**
3. Implementation: `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:146` is literally
   `getMessage: chrome.i18n.getMessage.bind(chrome.i18n)`. **VERIFIED**
4. Same endpoint reached from the framework-free popup
   (`omega-target-chromium-extension/src/js/omega_target_popup.js:92`) and from background code calling
   `chrome.i18n.getMessage` directly (`omega-target-chromium-extension/src/coffee/background.coffee:66`,
   `omega-target-chromium-extension/src/module/options.coffee:323`). **VERIFIED**
5. Profile names go through a second key namespace: `omega-web/src/omega/filters.coffee:25-29` looks up
   `profile_<name>`, falling back to the raw name. **VERIFIED**

`.po` → `_locales`, the only path that produces translatable strings:

* `omega-target-chromium-extension/grunt/po2crx.coffee:4-5` maps
  `../omega-locales/en_US/LC_MESSAGES/omega-web.po` → `build/_locales/en/messages.json`, and `:14-17` maps
  zh_CN and zh_TW. **VERIFIED**
* `omega-target-chromium-extension/grunt-po2crx.coffee:8` parses with `po2json`, `:13` rewrites gettext
  `$1:ref$`-style placeholders into Chromium `$ref$` placeholders, `:41` writes the JSON. **VERIFIED**
* The task is part of the default chain (`omega-target-chromium-extension/grunt/aliases.coffee:2-7`) — i.e.
  `_locales` is regenerated on every build, not committed. **VERIFIED**

Measured gap:

| Measurement | Value | How |
|---|---|---|
| Locale directories in `omega-locales/` | **27** | `ls` count |
| `omega-web.po` files | **27** (one per dir, none under 2 KB) | `find … -name '*.po' \| wc -l` |
| Destinations declared in the build | **7**: `en`, `zh`, `cs`, `fa`, `ru`, `zh_CN`, `zh_TW` | `omega-target-chromium-extension/grunt/po2crx.coffee:4-17` |
| `build/_locales/` directories | **7** — matches | `ls` |
| Keys shipped for `en` | **381** (`"message":` occurrences) | grep count over `build/_locales/en/messages.json` |
| msgids in `omega-locales/en_US/LC_MESSAGES/omega-web.po` | **382** | grep count |
| 20 locales that never reach a user | de, es, es_AR, fr, he_IL, is, it, ja, lzh, nb_NO, nl, pl, pt, pt_BR, si, sk, sl, tr, uk, zh_Hant | set difference |

Two extra findings inside the gap:

* `zh` and `zh_CN` are generated from the *same* source file (`omega-target-chromium-extension/grunt/po2crx.coffee:6-7` and `:14-15` both point at
  `omega-locales/zh_CN/…`), so one locale is shipped twice under two codes. **VERIFIED**
* Of the 20 unsupplied locales, several are near-empty anyway: measured non-empty single-line `msgstr` counts
  include he_IL 0, si 2, pl 7, lzh 14, nl 30, sk 30, it 42, is 38, sl 41, pt 63 out of ~351 msgids; the strong
  ones are ja 278, es_AR 270, ach 270, cs 272, pt_BR 269, de 269, fr 269, es 244, tr 223, uk 253, nb_NO 198.
  (Method: `grep -c '^msgstr "[^"]'`; multi-line msgstr blocks are **not** counted, so these are lower bounds.)
  **VERIFIED** measurement, **INFERRED** completeness conclusion.

What NEXORA loses if the gap stays open (§9 of PROJECT-STATE asks this):

* **Discoverability is by browser UI locale only.** Chromium's `chrome.i18n.getMessage` resolves against
  `default_locale` (`omega-target-chromium-extension/overlay/manifest.json:14`) plus whatever is in `_locales`;
  a missing directory silently falls back to English. Twenty locales of *existing translated work* are therefore
  invisible to users and unmaintainable from the UI. **INFERRED** from the two mechanisms above.
* **The UI catalog and the manifest catalog drift apart.** `en_US` carries 382 msgids while 12 other locale
  files still show 351 — 31 keys are already missing from the snapshots even in the locales that *do* ship
  (measured by `grep -c '^msgid '`). A second compiler (`packages/i18n`, ADR-0017) that reads the same `.po`
  files inherits exactly this skew. **VERIFIED** counts, **INFERRED** inheritance.
* **Some UI text never enters i18n at all.** `omega-web/src/omega/app.coffee` aside, the theme screen is
  hardcoded English: `omega-web/src/partials/theme.jade:2` and `:6` use `{{'Theme'}}` (a bare Angular
  expression, no `tr`), and `omega-web/src/partials/io.jade:59` uses `{{'Create a secret Gist. '}}`. Measured:
  **307** `| tr}}` uses across `omega-web/src/**/*.jade` versus **22** lines containing `{{'` with no `tr` on
  the same line. **VERIFIED** measurement.

## 6. Themes: 164 base16 themes, three defaults, and `-customCss` through csso

* The catalogue is a data file: `omega-web/lib/themes/themes.json` has **164** top-level keys (measured with
  `grep -cE '^  "[^"]+": \{'`; first entry `omega-web/lib/themes/themes.json:2` is `"3024"`), each carrying
  `name`, 16 `base00…base0F` colours, `order` and a `dark` boolean. **VERIFIED**
* The CSS on disk matches one-to-one: `ls omega-web/lib/themes/others/base16-*.css | wc -l` → **164**; 169 CSS
  files exist in total under `lib/themes` (164 base16 + `base.css` + `variable.css` + `default-{light,dark,auto}.css`). **VERIFIED**
* `omega-web/src/omega/controllers/master.coffee:329-335` builds the dropdown from the injected `themes` constant, prefixing every key with
  `'others/base16-'` (`omega-web/src/omega/controllers/master.coffee:331`) and labelling it with a moon/sun
  glyph from `val.dark` (`:332-333`). The constant is the `themes.json` fetch of §1 step 6. **VERIFIED**
* Therefore the honest count is **164 base16 themes + 3 "default-\*" themes + Clear** (`omega-web/src/partials/theme.jade:6-21`).
  The figure "166 base16 themes" recorded in `docs/PROJECT-STATE.md` §8.8 is **not supported** by the tree; the
  same 166 appears in the plan's §1 note about vendoring. **Contradiction reported.**
* Applying a theme is not a stylesheet switch: `omega-web/src/omega/controllers/master.coffee:338-366` fetches `lib/themes/variable.css`, then
  `lib/themes/<key>.css`, then `lib/themes/base.css` and stores the concatenation into the options key
  `-customCss` (`omega-web/src/omega/controllers/master.coffee:360`), with an empty-string write on both the
  "no theme" path (`:339-341`) and the fetch-failure path (`:362-365`). **VERIFIED**
* `-customCss` then leaves the UI: the options watcher minifies it with **csso** and publishes it as the
  `customCss` state value (`omega-target/src/options.coffee:496-498`), and the *page* reads that state back
  (`omega-web/src/omega/controllers/master.coffee:15-16`) into `omega-web/src/options.jade:13-14`, an inline
  `<style class="om-style">`. csso itself is loaded only in the service worker
  (`omega-target-chromium-extension/overlay/x-background.js:8`). **VERIFIED**
* Because the value is a `-`-prefixed options key it is also **excluded from sync** by
  `omega-target/src/options.coffee:90-91`, and it is user-editable raw CSS in a 30-row textarea
  (`omega-web/src/partials/theme.jade:27`). The theme system and the arbitrary-CSS escape hatch are the same
  mechanism. **VERIFIED**

## 7. Two divergent popup branches, plus a third stack nobody names

* **Chromium** declares `action.default_popup = popup-iframe.html`
  (`omega-target-chromium-extension/overlay/manifest.json:23`); **Firefox** declares
  `popup/index.html` (`omega-target-chromium-extension/overlay/manifest-firefox.json:23`). **VERIFIED**
* `omega-target-chromium-extension/overlay/popup-iframe.html:6` pulls iframeResizer and `:18` embeds `./popup/index.html` in an iframe sized by
  `omega-target-chromium-extension/overlay/popup-iframe.js:7-13`. But `omega-target-chromium-extension/overlay/popup-iframe.js:2` first assigns
  `document.location.href = './popup/index.html'` unconditionally (comment at `:3-4` cites issue #4), so in the
  normal case the iframe wrapper navigates away and the rendered document is the same one Firefox loads
  directly. **VERIFIED** file contents; **INFERRED** runtime outcome — I did not execute the popup.
* The runtime override is real: `omega-target-chromium-extension/overlay/x-background.js:26` sets
  `globalThis.POPUPHTMLURL = './popup-iframe.html'` and `:29-30` replaces it with `'./popup/index.html'` when
  `navigator.userAgent` matches `/Android/i` (comment at `:27-28` cites ZeroOmega issue #93). The single
  consumer is `chrome.action.setPopup` in `omega-target-chromium-extension/src/module/options.coffee:201-203`:
  quick-switch on ⇒ popup cleared (`:201`), quick-switch off ⇒ `POPUPHTMLURL` (`:203`). **VERIFIED**
* A fourth path exists: when quick-switch is enabled the action opens `popup/index.html` **as a tab**
  (`omega-target-chromium-extension/src/module/options.coffee:85`), and `ui`/`general`-style flows link into
  `../popup.html#!requestInfo` (`omega-web/src/popup/index.html:24`), which is the *AngularJS* popup built from
  `omega-web/src/popup.jade` (§3, `popup.coffee` 393 LOC). So three popup renderings coexist: `popup.html`
  (Angular), `popup/index.html` (plain JS), `popup-iframe.html` (wrapper). **VERIFIED**
* The plain-JS popup stack is the one both branches converge on and is framework-free:
  `omega-web/src/popup/js/` + `omega-target-chromium-extension/src/js/omega_target_popup.js` total 867 LOC
  including the network and temp-rules sub-pages, and it does its own i18n at
  `omega-web/src/popup/js/i18n.js:3-9`. **VERIFIED**

## 8. Implications for the Svelte layer (observations only)

Parity is defined by what a user can already do, so each row below is an obligation taken from the code above,
not a design choice. No row is a claim that a NEXORA feature exists.

1. **The i18n key set is a contract of ~381 keys** (`build/_locales/en/messages.json` measured), of which 31 are
   already absent from 12 locale snapshots. A Svelte catalog compiled from the same `.po` files (ADR-0017) must
   either ship all 382 `en_US` msgids or the UI shows raw key names; and the 22 untranslated `{{'…'}}` literals
   of §5 must be re-tagged during port, because they are currently invisible to translators. **VERIFIED** inputs.
2. **Theme injection must survive two ways at once.** The legacy path writes `variable.css + theme.css +
   base.css` into `-customCss` (`omega-web/src/omega/controllers/master.coffee:350-361`), csso-minifies it in the worker
   (`omega-target/src/options.coffee:498`) and the options page inlines it (`omega-web/src/options.jade:13-14`). A new UI in
   the same document inherits that stylesheet; a new UI in a **shadow root** does not — and user-authored
   `customCss` will keep targeting the legacy DOM, including the 164 base16 palettes and Bootstrap 3 classes
   (`omega-web/src/options.jade:7`). This is the CSS-scope argument ADR-0016 rests on, and §6 shows the mechanism it must
   reproduce. **INFERRED** from the mechanism; the decision itself is not re-opened here.
3. **164 theme entries must be enumerable without a 165th fetch.** `omega-web/src/coffee/options.coffee:68-70` already makes the
   bootstrap depend on `themes.json`; the per-theme CSS is fetched only on user selection
   (`omega-web/src/omega/controllers/master.coffee:343-356`). Any new UI that preloads the catalogue would add 164 requests where today there
   is 1 + 1. **VERIFIED** counts.
4. **Both popup branches must stay addressable.** `POPUPHTMLURL` is a runtime value
   (`omega-target-chromium-extension/overlay/x-background.js:26-30`) consumed at `omega-target-chromium-extension/src/module/options.coffee:203`, so the *branch* is data, not build-time
   config; the Android override and the `setPopup('')` quick-switch mode (`:201`) are user-visible behaviours
   the parity matrix must carry. The iframe wrapper additionally exists to make the popup auto-resize
   (`omega-target-chromium-extension/overlay/popup-iframe.js:7-13`) — losing it changes popup geometry on Chromium, which is a regression even if the
   DOM content is identical. **VERIFIED** mechanism, **UNVERIFIED** rendering outcome (not executed).
5. **The seven registered states, not six, are the navigation contract** — including the two controller-less
   ones (`omega-web/src/omega/app.coffee:52`, `omega-web/src/omega/app.coffee:56`), which bind forms straight onto `-`-prefixed option keys, and the
   `lastUrl` restore that decides which page a user lands on after a browser restart (`omega-web/src/omega/app.coffee:42-45`).
   Cutting over one state at a time is only safe because each is a separate `templateUrl`; a Svelte island per
   state is the shape the legacy router already implies. **INFERRED** from `omega-web/src/omega/app.coffee:48-75`.
6. **The RPC surface behind the UI is one dispatcher with an error map, and its backoff is a fixed bug**
   (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:50-77`, documented at `:37-49`). Any replacement UI that talks to the worker must
   port that retry ladder verbatim; §5 of PROJECT-STATE #8 states the same obligation from the other side.
   **VERIFIED** lines, **INFERRED** necessity.
7. **Nothing in `omega-web` is tested** (§4: no `test/` directory, vacuous `npm test`), so the parity gate for
   this layer cannot be "tests pass"; the only machine-checkable statements available today are the counts in
   this file (states, keys, themes, files, LOC) plus `node tools/verify-vendor.mjs`, which validates that every
   `lib/...` reference in `§1`'s scripts still resolves. **VERIFIED**.

## UNVERIFIED registry

| # | Claim not established here | Why | Who can close it |
|---|---|---|---|
| 1 | That `omega-target-chromium-extension/overlay/popup-iframe.js:2` really navigates away before the iframe renders (i.e. that the Chromium popup and the Firefox popup are the same document at runtime) | Static reading only; nothing was executed in a browser this iteration | Phase-2 smoke test in Chrome + Firefox |
| 2 | Translation completeness per locale | Measured with a single-line `msgstr` grep; multi-line msgstr blocks are undercounted, so all 27 percentages are lower bounds | `msgfmt --statistics` in Phase 2 |
| 3 | Whether any of the 20 unsupplied locales ship in an upstream ZeroOmega store package | Store packages are not in this repo; only the local build was inspected | Compare against the store listings cited by `docs/audit/10` |
| 4 | The size of the `build/lib` bower output ("25 vendored dirs") | Counted from `ls build/lib` in the working tree, which is a *derived* directory and is not reproducible on a clean runner (PROJECT-STATE §1.2) | Re-run `npm run deps` on a clean runner |
| 5 | `ngAnnotate` being "dead" | Proven only for this repo (`ngmin` referenced nowhere, task absent from `aliases.coffee`). A consumer outside the repo could load it | grep upstream |
| 6 | That `-customCss` minification by csso preserves all user CSS semantics | csso minify behaviour was not tested; only its call site was read | Phase-2 unit test |
| 7 | Whether `themes.json` fetch failure is user-recoverable | `omega-web/src/options.jade:15` implies a blank page; the actual failure UX was not observed | Manual smoke step in `docs/verification/*` |
| 8 | The 6-vs-7 state count | The code is unambiguous (`omega-web/src/omega/app.coffee:48-75`, seven `.state(` calls); the *wording* "6 states" in PROJECT-STATE/plan is what needs correction | Update PROJECT-STATE §6 #11 in a docs-only commit |
