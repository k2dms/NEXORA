# 03 — Runtime layering

> Purpose: describe the five CoffeeScript projects as they actually run — what each one exports, who
> consumes it, and by what mechanism (prebuilt bundle at the project root, `node_modules` symlink,
> browserify `standalone` global, AngularJS module) — and to record the corrected extension seam
> (`Options` plus an injected `proxyImpl`) that NEXORA must plug into. This is an observation of the
> current tree (ZeroOmega v3.5.2), not a proposal; the normative target layering lives in
> `docs/architecture/20-target-repository-layout.md` and the proxy-application deep trace in `docs/audit/06`.

Tags: **VERIFIED** = the cited line was opened and says so. **INFERRED** = deduced from cited code.
**UNVERIFIED** = assumed; every such item is listed in the registry at the end.

---

## 0. The headline correction

The SwitchyOmega design literature describes a `Target` / `TargetCfg` abstraction as the extension point.
Neither exists in this fork: a search over `omega-pac/src`, `omega-target/src`, `omega-web/src` and
`omega-target-chromium-extension/src` returns **zero** occurrences of the strings `TargetCfg` and
`class Target` **VERIFIED** (by absence, grep over those trees). The real seam is two things:

1. a class — `Options`, `omega-target/src/options.coffee:50` **VERIFIED**, whose constructor takes the
   collaborator objects directly: `constructor: (@_storage, @_state, @log, @sync, @proxyImpl) ->`
   `omega-target/src/options.coffee:101` **VERIFIED**; and
2. a browser-capability object — `proxyImpl`, selected at runtime from an ordered list by
   `OmegaTargetChromium.proxy.getProxyImpl` `omega-target-chromium-extension/src/module/proxy/index.coffee:12`
   **VERIFIED**.

There is no registry of `Target` subclasses, no `TargetCfg` descriptor, and no per-browser "target"
module. Everything else that differs between browsers is a subclass override on `Options`
(`ChromeOptions extends OmegaTarget.Options`, `omega-target-chromium-extension/src/module/options.coffee:12`
**VERIFIED**) plus the four `proxyImpl` candidates.

---

## 1. Layer map

| Layer | Project | What it exports | Consumed by | Mechanism of consumption |
|---|---|---|---|---|
| Engine | `omega-pac` | `Conditions`, `PacGenerator`, `Profiles`, `RuleList`, `ShexpUtils` + utils spread — `omega-pac/index.coffee:2-9` | `omega-target`, `omega-web`, the webext proxy script | `require('omega-pac')` resolves to the **browser global** `OmegaPac` through the shim `omega-target/omega_pac_shim.js:1` (`module.exports = OmegaPac;`), wired by the `browser` field `omega-target/package.json:29`; the UI reads the global directly (`omega-web/src/omega/app.coffee:2`) |
| Storage/options abstraction | `omega-target` | `Log`, `Storage`, `BrowserStorage`, `Options`, `OptionsSync`, `OmegaPac` — `omega-target/index.coffee:2-7` | `omega-target-chromium-extension` | same trick: `omega-target-chromium-extension/omega_target_shim.js:1` (`module.exports = OmegaTarget`) reached through `omega-target-chromium-extension/package.json:31` |
| Platform glue + composition root | `omega-target-chromium-extension` | its own `Storage`, `SyncStorage`, `Options`, `ChromeTabs`, `SwitchySharp`, `ExternalApi`, `WebRequestMonitor`, `Inspect`, `Url`, `proxy` — `omega-target-chromium-extension/src/module/index.coffee:2-11`, then the whole `omega-target` surface copied in at `:13-14` | the extension's own service worker, as the global `OmegaTargetChromium` — `omega-target-chromium-extension/grunt/browserify.coffee:19`, `:29` | browserify `standalone` bundle at the project root, `omega-target-chromium-extension/grunt/browserify.coffee:16` |
| UI | `omega-web` | no JS API: a static `build/` tree (html/css/js/lib) | `omega-target-chromium-extension` copies the entire directory — `omega-target-chromium-extension/grunt/copy.coffee:2-6` | file copy, then AngularJS module names inside the page |
| Build hub | `omega-build` | nothing at runtime — grunt-hub task forwarding only | developers / CI | `omega-build/Gruntfile.coffee:2`, `:7`, `:18` |

`omega-locales` is data, not a layer: 27 `.po` trees feeding `po2crx` (see `docs/audit/01`).

---

## 2. Engine: `omega-pac`

* Own dependencies: `ip-address`, `tldjs`, `uglify-js` (`omega-pac/package.json:21-24`).
  For the browser, `omega-pac/package.json:26` maps `uglify-js` to `./uglifyjs-shim.js` (`:27`) and keeps
  the real one available as `uglify-js-real` (`:28`), so the bundle carries its own AST library
  **INFERRED** from `omega-pac/src/profiles.coffee:1` (`U2 = require 'uglify-js'`) plus that map.
* Leaf artifact: `omega_pac.min.js` emitted at the project root by
  `omega-pac/grunt/browserify.coffee:15`, aliased to the global name at `:18` and
  `standalone: 'OmegaPac'` at `:28` — all **VERIFIED**.
* Consumers never read `src/`. They read the prebuilt root bundle through a symlink:
  `omega-web/grunt/copy.coffee:4` copies `node_modules/omega-pac/omega_pac.min.js` into
  `build/js/omega_pac.min.js`, and `omega-target-chromium-extension/grunt/browserify.coffee:36`
  aliases `'omega-pac': 'omega-pac/omega_pac.min.js'` for the Firefox proxy script. The symlinks exist
  because `npm run dev` runs `npm link` (`omega-build/package.json:13`); on this host
  `omega-target-chromium-extension/node_modules/omega-pac -> /e/NEXORA/omega-pac` **VERIFIED** (ls -la).
* Content: the profile type registry (`omega-pac/src/profiles.coffee:253`), the condition registry
  (`omega-pac/src/conditions.coffee:227`), the PAC compiler (`omega-pac/src/pac_generator.coffee:25`) and
  the rule-list parsers (`omega-pac/src/rule_list.coffee`). It is pure data-in / AST-out: no browser API
  appears in any of these files **VERIFIED** (read of `profiles.coffee`, `pac_generator.coffee`,
  `conditions.coffee`).
* NEXORA inherits this layer wholesale (ADR-0013 keeps `Conditions` as the outbound-selection mechanism);
  the observation here is only that the layer is *reachable by name* from three other projects, so any
  NEXORA profile type must be added inside `_profileTypes` or it will break the engine for everyone
  (`omega-pac/src/profiles.coffee:250` throws `Unknown profile type`).

## 3. Abstraction: `omega-target`

* Exports four classes and one namespace (`omega-target/index.coffee:2-7`), plus `Promise` and helpers
  pulled in from `src/utils.coffee` at `:9-10` and the error taxonomy from `omega-target/src/errors.coffee:28-33`
  (copied into the namespace at `omega-target/index.coffee:12-13`).
* `Options` is the only mutable object in the layer; it holds `_options` (`omega-target/src/options.coffee:61`),
  the injected storage (`:101`), the sync adapter (`:101`) and the proxy implementation (`:101`).
  `Storage` (`omega-target/src/storage.coffee:5`) and `BrowserStorage` (`omega-target/src/browser_storage.coffee:6`)
  are the two storage shapes it can be handed. Details in `docs/audit/05`.
* Leaf artifact: `omega_target.min.js` at the project root, `omega-target/grunt/browserify.coffee:15`,
  `standalone: 'OmegaTarget'` at `:28`. Its own `require('omega-pac')` is answered by the shim
  (`omega-target/package.json:28-30`), i.e. **this layer expects its dependency to already be a global**.
* NEXORA inherits `Options` as the envelope owner and replaces nothing in it at Phase 1; the Connector
  adds a *new* injected collaborator alongside `proxyImpl`, which is precisely why `:101` is the seam
  rather than a method to patch.

## 4. Platform glue and composition root: `omega-target-chromium-extension`

* `index.coffee` (`omega-target-chromium-extension/index.coffee:1`) → `src/module/index.coffee`, which
  exports the chromium-specific classes and then folds `omega-target` into the same namespace
  (`omega-target-chromium-extension/src/module/index.coffee:13-14`, `module.exports[name] ?= value` — the `?=` means the local class wins;
  `ChromeStorage` is exported as `Storage` at `omega-target-chromium-extension/src/module/index.coffee:2`, shadowing `OmegaTarget.Storage`).
* The composition root is `zeroBackground` — `omega-target-chromium-extension/src/coffee/background.coffee:68` — exposed as
  `globalThis.zeroBackground` at `:498` and called by the MV3 service worker entry
  `omega-target-chromium-extension/overlay/x-background.js:63` **VERIFIED**. That entry's `import` order *is* the dependency order:
  polyfills first (`omega-target-chromium-extension/overlay/x-background.js:1-2`), globals `omega_pac.min.js` → `omega_target.min.js` →
  `omega_target_chromium_extension.min.js` (`:14-16`), then `background.js` (`:18`).
  Because the shims (`omega-target/omega_pac_shim.js:1`, `omega-target-chromium-extension/omega_target_shim.js:1`) read those globals at
  require time, reordering `x-background.js` breaks the bundle — **INFERRED** from the shim bodies plus
  the import list; no run was performed.
* `src/module/proxy/*` owns every call into a browser proxy API; `omega-target-chromium-extension/src/module/fetch_url.coffee:10` owns
  the subscription `fetch`; `src/module/storage.coffee` and `sync_storage.coffee` own persistence; the
  tab/badge/menu surface is exported as `ChromeTabs` (`omega-target-chromium-extension/src/module/index.coffee:5`),
  `WebRequestMonitor` (`:8`) and `Inspect` (`:9`), constructed in `omega-target-chromium-extension/src/coffee/background.coffee:308` and `:311`.
* NEXORA replaces the *implementation* chosen by `getProxyImpl` (a Connector-backed impl) and inherits the
  selection list mechanics — see §7.

## 5. UI: `omega-web`

* Emits no bundle. `omega-web/grunt/aliases.coffee:2-10` runs `copy → jade → less → autoprefixer →
  coffeelint → coffee → bower`; the result is `omega-web/build/`, which the extension project copies whole
  (`omega-target-chromium-extension/grunt/copy.coffee:2-6`).
* Its coffee compiles one file at a time (`omega-web/grunt/coffee.coffee:2-7` for `src/coffee`, `:8-10`
  for `build/js/omega.js` from `src/omega/**/*.coffee`), so there is no module boundary at runtime:
  the page reaches the engine through the *global* `OmegaPac` (`omega-web/src/omega/app.coffee:2`) and
  the background through `chrome.runtime.sendMessage` (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:53`).
* Bootstrap is manual, not declarative: `omega-web/src/options.jade:2` carries `ng-controller='MasterCtrl'` with no
  `ng-app`, and the app is started by `omega-web/src/coffee/options.coffee:70`
  (`angular.bootstrap document, ['omega']`). The popup is the only document with a declarative app:
  `omega-web/src/popup.jade:20` (`ng-app='omegaPopup'`).
* Vendor JS comes from Bower, including the `zero-dependencies` bundle
  (`omega-web/bower.json:43`), installed into `build/lib/` (`omega-web/grunt/bower.coffee:5`) and copied
  into the extension by the whole-directory copy. `lib/` therefore contains `idb-keyval`,
  `compare-versions`, `csso`, `moment`, `fake-indexeddb` — the runtime libraries the service worker
  imports at `omega-target-chromium-extension/overlay/x-background.js:4-8` **VERIFIED** for the import lines, **UNVERIFIED** for the
  file contents (vendored, untracked).

## 6. Build hub: `omega-build`

No runtime layer. `omega-build/Gruntfile.coffee:2` lists the submodules and `:7` additionally globs
`../*/Gruntfile.*`; `:12` builds the `hub:<module>:<task>` names and `:20-22` register `default`,
`test`, `watch`. Its `package.json` scripts are the only cross-project install order
(`omega-build/package.json:12` deps, `:13` dev = the `npm link` chain, `:14` build, `:15` release).
Consequences for the artifact chain are in `docs/audit/01`; layering adds nothing here.

---

## 7. The corrected seam in detail

**Selection.** `omega-target-chromium-extension/src/module/proxy/index.coffee` requires exactly four
implementations (`:1-4`) and publishes them in preference order in `exports.proxyImpls` (`:6-10`):
`FirefoxProxyImpl`, `ListenerProxyImpl`, `ScriptProxyImpl`, `SettingsProxyImpl`. `getProxyImpl(log)`
(`:12`) walks that list, returns the first `Impl.isSupported()` (`:14-15`) and otherwise throws
`'Your browser does not support proxy settings!'` (`:16`). This is the file the task brief called
`src/module/proxy/index.coffee`; in this repo its full path is
`omega-target-chromium-extension/src/module/proxy/index.coffee` — **VERIFIED**. (The brief's wording
"under `omega-target`" is wrong: `omega-target/src` contains no `module/` directory at all.)

Support predicates, one per implementation, all **VERIFIED**:

| Implementation | `isSupported` | line | `features` | line |
|---|---|---|---|---|
| `FirefoxProxyImpl` | `!!chrome.contextMenus and Promise? and browser?.proxy?.onRequest? and browser?.proxy?.settings` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:10-16` | `['fullUrl', 'socks5Auth']` | `:17` |
| `ListenerProxyImpl` | `Promise? and browser?.proxy?.onRequest?` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:8` | `['fullUrl', 'socks5Auth']` | `:9` |
| `ScriptProxyImpl` | `@isSupported:` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_script.coffee:6` | `['socks5Auth']` | `:8` |
| `SettingsProxyImpl` | `chrome?.proxy?.settings?` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:8` | `['fullUrlHttp', 'pacScript', 'watchProxyChange']` | `:9` |
| `ProxyImpl` (base) | `-> false` | `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:12` | — | — |

**Injection.** Exactly one call site: `proxyImpl = OmegaTargetCurrent.proxy.getProxyImpl(Log)` at
`omega-target-chromium-extension/src/coffee/background.coffee:244`, its capability set published to the UI
through state at `:245` (`state.set({proxyImplFeatures: proxyImpl.features})`), and the instance passed as
the **fifth** constructor argument at `:246-247`. That is the whole dependency-arrival path — there is no
setter, no provider, no second injection point **VERIFIED** (`Options#applyProfile` is the only reader of
`@proxyImpl`: `omega-target/src/options.coffee:699` and `:701`).

**Contract.** A `proxyImpl` must provide `applyProfile(profile, meta, options)`,
`watchProxyChange(callback)` and `parseExternalProfile(details, options)` — declared as stubs on the base
class at `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:13-15` and consumed at `omega-target/src/options.coffee:699` and
`omega-target-chromium-extension/src/coffee/background.coffee:340`, `:377`. Two shared services come from the base class rather than the subclass:
proxy-auth setup (`omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:23`) and PAC-text generation for inclusive profiles
(`omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:36`, which calls `OmegaPac.PacGenerator.script` at `:62`).

## 8. Where the AngularJS module `omegaTarget` really lives

It is **not** in `omega-web`. It is registered by the extension project:

```
omega-target-chromium-extension/src/coffee/omega_target_web.coffee:19
  angular.module('omegaTarget', []).factory 'omegaTarget', ($q) ->
```

**VERIFIED** at that line. The file compiles (via `omega-target-chromium-extension/grunt/coffee.coffee:2-6`,
`src/coffee` → `build/js/`) into `build/js/omega_target_web.js`, which the options page loads by name at
`omega-web/src/coffee/options.coffee:22` and the popup path loads as well; the module name is then declared
as a dependency of both apps: `omegaTarget` inside the `'omega'` module list
(`omega-web/src/coffee/options.coffee:16`) and inside `angular.module('omegaPopup', [...])`
(`omega-web/src/coffee/popup.coffee:1`).

So the UI layer depends on a file that lives in the platform layer. The factory itself contains no
AngularJS-specific logic beyond `$q`: it is a promise shim over `chrome.runtime.sendMessage`
(`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:35`, `:53`), a port helper (`:79`) and the MV3 service-worker wake-up retry
(`:50-51`, `:63-67`) documented as bug #276 in `docs/audit/06` and `PROJECT-STATE` §2.8. NEXORA inherits
this file verbatim as the bridge contract for the legacy UI; the new UI client must re-implement the same
method surface (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:97-214`) **INFERRED** — the list is what any consumer can call.

## 9. Responsibility ownership, and what NEXORA inherits versus replaces

| Responsibility | Owner today (evidence) | NEXORA: inherit or replace |
|---|---|---|
| Profile / condition / rule-list data model | `omega-pac/src/profiles.coffee:253`, `omega-pac/src/conditions.coffee:227` | Inherit; extend the type registry rather than bypass it |
| PAC text generation | `omega-pac/src/pac_generator.coffee:25` | Inherit for the browser plane only (`docs/audit/06`) |
| Envelope read/parse/upgrade/patch | `omega-target/src/options.coffee:327`, `:426`, `:442` | Inherit; NEXORA keys live beside the legacy ones |
| Persistence + quota/error taxonomy | `omega-target/src/storage.coffee:53`, `omega-target-chromium-extension/src/module/storage.coffee:6` | Inherit; details in `docs/audit/05` |
| Remote sync (Gist/WebDAV) | `omega-target/src/options_sync.coffee:12`, `omega-target-chromium-extension/src/module/sync_storage.coffee:125` | Inherit unchanged |
| Applying a proxy to the browser | the four `proxy_impl_*` files, selected at `omega-target-chromium-extension/src/module/proxy/index.coffee:12` | Replace by *adding* a Connector-backed impl to that list; do not edit `SettingsProxyImpl` behaviour |
| Subscription download | `omega-target/src/options.coffee:780`, `omega-target-chromium-extension/src/module/fetch_url.coffee:24` | Replaced for server lists by the Connector (`subscription.refresh`); legacy rule lists keep the old path |
| Browser-event glue (alarms, menus, badge, ports) | `omega-target-chromium-extension/src/module/options.coffee:56-100`, `omega-target-chromium-extension/src/coffee/background.coffee:308`, `:311` | Inherit |
| Options page behaviour | `omega-web/src/omega/**` (AngularJS) + `omega_target_web.coffee` | Coexists with the Svelte layer until the parity matrix flips (ADR-0016) |
| Packaging / release zip | `omega-target-chromium-extension/grunt/compress.coffee`, `overlay/manifest.json` | Inherit position, rename artifacts at cut-over (`PROJECT-STATE` §3) |

## 10. How a message travels

Hop-by-hop for "user clicks a profile in the options page", with the layer crossed at each step:

1. **UI → bridge.** `omegaTarget.applyProfile(name)` calls `callBackground('applyProfile', name)` —
   `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:166-167`. **VERIFIED**
2. **Bridge → service worker.** `chrome.runtime.sendMessage({method, args})` with a 5 + 9 attempt
   backoff if the worker is not yet awake — `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:53`, `:50-51`, `:63-67`.
   **VERIFIED**
3. **Worker router.** `chrome.runtime.onMessage.addListener` requires `request.method`
   (`omega-target-chromium-extension/src/coffee/background.coffee:458-459`), waits for `options.ready` (`:460`), and resolves the target by name:
   `getState`/`setState` route to `state` (`:464-469`), `resetAllOptions` to a local function (`:461-463`),
   everything else to `target = options; method = target[request.method]` (`:471-472`); a missing method
   answers `reason: 'noSuchMethod'` (`:473-478`). The call itself is `method.apply(target, request.args)`
   (`:481`) and the answer is `respond(result: result)` (`:490`) / `respond(error: encodeError(error))`
   (`:494`), with `return true` at `:497` keeping the port open. **VERIFIED**
   *Note: this router is a string-dispatched RPC onto a live object — NEXORA must not reuse it for the
   Connector, which is allowlisted and typed (constraint 3 in `PROJECT-STATE` §0).*
4. **`Options` domain step.** `applyProfile` (`omega-target/src/options.coffee:654`) resolves the profile
   with `OmegaPac.Profiles.byName(name, @_options)` (`:656`), records `_watchingProfiles` from
   `allReferenceSet` (`:662`), writes the derived state keys `currentProfileName` / `isSystemProfile` /
   `currentProfileCanAddRule` (`:665-670`) and refreshes `availableProfiles` via
   `_setAvailableProfiles()` (`:671`, body `:606-641`). **VERIFIED**
5. **Engine step (only for PAC-shaped profiles).** `@proxyImpl.applyProfile(...)` at `:699` / `:701`.
   `SettingsProxyImpl.applyProfile` (`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:10`) maps the profile type onto a
   `chrome.proxy` config: `direct` (`:19`), `pac_script` from `profile.pacScript` (`:21-29`),
   `fixed_servers` (`:31`, builder `:43-73`), or — for inclusive types such as Switch/RuleList —
   `mode: 'pac_script'` with a script generated by the engine (`:36` → `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:36` →
   `omega-pac/src/pac_generator.coffee:25`). **VERIFIED**
6. **Browser.** `chromeApiPromisify(chrome.proxy.settings, 'set')({value: config})` —
   `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:38`; promisify is `omega-target-chromium-extension/src/module/chrome_api.coffee:4-18`. Then
   `chrome.proxy.settings.get {}, @_proxyChangeListener` (`:40`) re-reads the value so the
   `levelOfControl` bookkeeping stays accurate. **VERIFIED**
7. **Storage.** Persistence happens on the write path, not the apply path: `_setOptions` pushes
   `@sync?.requestPush(changes)` (`:477`) and `@_storage.set(changes)` (`:480`) followed by
   `@_storage.remove(removed)` (`:481`). `ChromeStorage.set` is `omega-target-chromium-extension/src/module/storage.coffee:52-55`, i.e.
   `chrome.storage.local` (the area name is fixed at `omega-target-chromium-extension/src/coffee/background.coffee:233`). **VERIFIED**
8. **Back to the UI.** The UI never reads storage; it re-reads `Options` through
   `omegaTarget.refresh()` → `callBackground('getAll')` (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:126-127`) and the
   derived state through `getState` (`:99-116`), where `availableProfiles` values are indexed with an
   explicit `'+' + name` (`omega-web/src/coffee/popup.coffee:230`, `:243`, and the validator
   `availableProfiles["+" + $value]` at `:194`). **VERIFIED** — this is the round trip that makes the
   key-prefix rule of `docs/audit/04` observable in the UI.
9. **Watch loop.** `Options#_watch` registers `@_storage.watch null, handler`
   (`omega-target/src/options.coffee:527`; the public form is `:583`), `ChromeStorage.watch` installs one
   shared `chrome.storage.onChanged` listener and fans out to registered watchers
   (`omega-target-chromium-extension/src/module/storage.coffee:65-82`, listener `:80`, dispatch `:84-98`). **VERIFIED**

Crossing summary: page (UI) → bridge module (glue) → router (glue) → `Options` (abstraction) →
`Profiles`/`PacGenerator` (engine) → `chrome.proxy` (browser) → `ChromeStorage` (abstraction/glue) →
`onChanged` → `_watch` handler (abstraction). No hop goes the other way: the engine never touches a
browser API, and the UI never touches storage directly.

---

## UNVERIFIED registry

| # | Claim | Why it could not be checked here |
|---|---|---|
| 1 | The exact default `idb-keyval` store cleared by `idbKeyval.clear()` at `omega-target-chromium-extension/src/coffee/background.coffee:46` | the library is vendored into `build/lib/zero-dependencies/` by Bower and is not a tracked source file; cited in `docs/audit/05` §5 |
| 2 | `compare-versions` semantics (`compare(a, b, '>'`) used at `omega-target-chromium-extension/src/coffee/background.coffee:41-42` | same vendored-file reason; the arithmetic consequences are derived in `docs/audit/05` §9 from the call shape only |
| 3 | Reordering the `import` list in `overlay/x-background.js` breaks the bundle | INFERRED from the two shim files (`omega-target/omega_pac_shim.js:1`, `omega-target-chromium-extension/omega_target_shim.js:1`); would need a build+load run, and running Grunt is out of scope for this iteration |
| 4 | `omega-web/build/lib` contains `idb-keyval`, `csso`, `moment`, `compare-versions`, `fake-indexeddb` | observed on this host in an untracked `build/` tree only; the audit rule is not to cite derived output |
| 5 | `OmegaTargetPopup` (`src/js/omega_target_popup.js`) is the only non-Angular bridge used by the popup | read of `:1-40` plus `omega-target-chromium-extension/overlay/popup-iframe.js:5`; the remaining popup `src/popup/js/*.js` files were not audited |
| 6 | The four `proxyImpl` candidates are exhaustive in this fork | grep-based conclusion over `omega-target-chromium-extension/src/module/proxy/index.coffee:6-11` **VERIFIED**, but "no implementation lives elsewhere" was not separately searched |
| 7 | `getProxyImpl` ordering actually selects `FirefoxProxyImpl` on desktop Firefox | needs a browser run; only the predicate text (`omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:10-16`) was read |
