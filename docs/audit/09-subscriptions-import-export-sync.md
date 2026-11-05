# 09 — Subscriptions, import/export and sync

> Purpose: establish, line by line, what ZeroOmega v3.5.2 actually does today for subscription refresh, options
> import/export and options sync — including the master prompt's §12 safety property, which is **already
> satisfied** by shipped code — and separate that from the list of things that are genuinely missing (durable
> per-profile status, backoff, guards, cache-buster determinism). Cross-references the two Phase-1 contracts
> that turn those gaps into enforceable obligations.

Every claim is tagged **VERIFIED** / **INFERRED** / **UNVERIFIED**. Nothing here was executed in a browser; all
readings are static. `docs/PROJECT-STATE.md` §1.6, §2.2 and §2.4 are the settled findings this document expands;
no contradiction of them was found, and one refinement is reported in §3.4.

---

## 1. The subscription update path, end to end

A subscription profile is a `RuleListProfile` whose `sourceUrl` is the feed
(`omega-pac/src/profiles.coffee:492`). Three triggers exist:

| Trigger | Code | Notes | Tag |
|---|---|---|---|
| User clicks "Download settings now" | `omega-web/src/partials/profile_rule_list.jade:27` → `omega-web/src/omega/controllers/master.coffee:246` | always with the literal `'bypass_cache'` | **VERIFIED** |
| Periodic alarm | `omega-target/src/options.coffee:511-512` (`@schedule 'updateProfile', …`) → `omega-target-chromium-extension/src/module/options.coffee:290-297` (`chrome.alarms.create`) → handler `:58-60` | no callback arg is honoured by the subclass; the alarm name is the only coupling | **VERIFIED** |
| On profile apply | `omega-target/src/options.coffee:706-712` — every watched profile of the applied profile is refreshed, gated on `-downloadInterval > 0` | default interval is 1440 minutes (`omega-target/src/default_options.coffee:13`) | **VERIFIED** |

Then, inside `Options.updateProfile` (`omega-target/src/options.coffee:780-818`), for each `+`-keyed profile that
has an `updateUrl` (`:783-790`):

1. **Headers are assembled from the profile.** `omega-target/src/options.coffee:792-796` builds a plain object
   from `profile.headers` (`{name, value}` pairs), skipping empty names. This is the field that later matters for
   SEC-1. **VERIFIED**
2. **Fetch.** `omega-target/src/options.coffee:797` calls `@fetchUrl(url, headers, opt_bypass_cache, type_hints)`;
   the implementation is the module-level assignment `omega-target-chromium-extension/src/module/options.coffee:16`
   pointing at `omega-target-chromium-extension/src/module/fetch_url.coffee:24-46`. The path in the brief is
   confirmed: the file exists exactly at `omega-target-chromium-extension/src/module/fetch_url.coffee`. **VERIFIED**
3. **The fetch is a bare `fetch()`** — `omega-target-chromium-extension/src/module/fetch_url.coffee:10` with the
   caller's headers attached at `:8-9`. There is no size cap, no scheme check, no redirect limit, no `response.ok`
   test: `:11-13` returns `[response, body]` for *any* status. **VERIFIED**
4. **Cache-buster.** `omega-target-chromium-extension/src/module/fetch_url.coffee:36-40`: only when `opt_bypass_cache` is set **and** the URL contains no
   `?`, the URL is re-formatted through Node's `url` module (`omega-target-chromium-extension/src/module/fetch_url.coffee:3`) with an added `_ = Date.now()`
   query. On failure it silently retries the *original* URL (`omega-target-chromium-extension/src/module/fetch_url.coffee:42-44`); URLs that already carry a
   query never get a buster at all (`:36`). **VERIFIED**
5. **Content-Type adjudication.** `omega-target-chromium-extension/src/module/fetch_url.coffee:26-33` iterates the profile's hint list. The hint list for
   subscriptions is `omega-pac/src/profiles.coffee:493-498` and its **last entry is the literal `'*'`**, whose
   handler at `omega-target-chromium-extension/src/module/fetch_url.coffee:56-58` returns the body unconditionally ("Allow all contents"). **VERIFIED**
   Reading note: at `omega-target-chromium-extension/src/module/fetch_url.coffee:27` the Content-Type is read as `response.headers['content-type']`, but a
   fetch `Headers` object is not string-indexable, so that expression evaluates to `undefined` and the two
   blacklist hints (`!text/html`, `!application/xhtml+xml`) can never fire; the `'*'` hint then accepts anything.
   **VERIFIED** code, **INFERRED** runtime consequence (not executed). The same bracket-access bug makes the PAC
   branch's `application/x-ns-proxy-autoconfig` hint resolve through its body-sniffing fallback (`omega-target-chromium-extension/src/module/fetch_url.coffee:77-85`),
   i.e. any response text containing `FindProxyForURL` is accepted as a PAC script. **INFERRED**
6. **Error classification is dead.** `omega-target-chromium-extension/src/module/fetch_url.coffee:14-22` maps `err.statusCode` / 404 / 5xx onto
   `HttpNotFoundError` / `HttpServerError` / `HttpError`, but it is guarded by `throw err unless err.isOperational`
   at `:15` — a flag of the abandoned `xhr` library (see the commented-out require at `:2`). `fetch()` rejects with a
   plain `TypeError`, so `:16-22` is unreachable and every transport failure leaves the caller with an untyped
   error. **VERIFIED** lines, **INFERRED** unreachability.
7. **sha256 + compare + write.** Back in `Options.updateProfile`: the body is hashed with WebCrypto
   (`omega-target/src/options.coffee:9-24`), the profile is re-read from live options at `:804`, stamped with
   `lastUpdate` at `:805`, and written **only** under the condition at `omega-target/src/options.coffee:806`.
   **VERIFIED**
8. **Parser-side truth.** `OmegaPac.Profiles.update` (`omega-pac/src/profiles.coffee:137-138`) dispatches to the
   `RuleListProfile` handler `omega-pac/src/profiles.coffee:499-519`: it trims, format-detects (`:504-511`),
   returns `false` if the trimmed data equals the existing `ruleList` (`:515`), and otherwise clears the derived
   `pacScript` (`:517`) and stores the new text (`:518-519`). The PAC branch is the two-line equivalent at
   `omega-pac/src/profiles.coffee:389-392`. **VERIFIED**

## 2. §12 verdict: the safe update is already implemented

The master prompt demands "a failed update must never delete the last good version". **This is true today.**
The exact lines that make it true:

* `omega-target/src/options.coffee:806` — `if OmegaPac.Profiles.update(profile, data) or not profile.sha256`.
  The write branch is reached only when the parser reports an actual change, or when the profile has never been
  hashed. A `false` return falls through to `omega-target/src/options.coffee:813` (`return profile`), which
  touches nothing. **VERIFIED**
* `omega-target/src/options.coffee:815-816` — the per-profile `.catch` converts the rejection into an
  `Error` **value inside the returned map**. The only caller is `Promise.props(results)`
  (`omega-target/src/options.coffee:818`), so one failing feed cannot reject the batch and cannot reach storage. **VERIFIED**
* `omega-target/src/options.coffee:476-482` — persistence happens in `_setOptions`, which is called at `:811`
  *only* from the success branch. No error path in `updateProfile` calls `_setOptions`, so no error path can
  write (or delete) a profile. **VERIFIED**
* The error is surfaced, not swallowed: the worker re-encodes each map value
  (`omega-target-chromium-extension/src/coffee/background.coffee:486-490`), the web client decodes it
  (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:140-145`) and the UI distinguishes
  success from error by `instanceof Error` (`omega-web/src/omega/controllers/master.coffee:249-264`), mapping
  `err.name` onto an `options_profileDownloadError_<Name>` message (`omega-web/src/omega/controllers/master.coffee:266-267`). **VERIFIED**

So the property is safe-by-construction. It is **not** safe-by-observation, and the following are genuinely
**missing** (each is a new capability, not a defect in the above):

| # | Missing today | Evidence that it is absent | Where it is required |
|---|---|---|---|
| M1 | Durable per-profile status `lastSuccessAt` / `lastError` / `consecutiveFailures` / `nextAttemptAt` | `updateProfile` writes `lastUpdate` (`omega-target/src/options.coffee:805`) and nothing else; the error object is discarded by the UI after one alert (`omega-web/src/omega/controllers/master.coffee:265-277`); `OmegaPac.Profiles.each` sees only what was stored | `packages/schema/subscription-source.schema.json:123-190` (`status`) |
| M2 | Retry / backoff / jitter | `chrome.alarms.create(name, {periodInMinutes})` is a plain fixed period (`omega-target-chromium-extension/src/module/options.coffee:295-297`); the sync poll is a hard-coded 5-minute alarm (`omega-target-chromium-extension/src/module/sync_storage.coffee:265-267`); no randomness anywhere in these paths | `packages/schema/subscription-source.schema.json:90-122` (`schedule.jitterPercent`, `backoffMultiplier`, `maxIntervalSeconds`) |
| M3 | Size guard | No `Content-Length`/stream limit anywhere: `omega-target-chromium-extension/src/module/fetch_url.coffee:11-13` takes `response.text()` | `packages/schema/subscription-source.schema.json:50-85` (`limits.maxBytes`, ≤ 8 MiB) |
| M4 | Scheme guard / SSRF defence | the URL is whatever `updateUrl` returned (`omega-pac/src/profiles.coffee:492`); `file:` is only special-cased for PAC (`omega-pac/src/profiles.coffee:378-381`); the online-restore field is a free-form `type='url'` input fetched straight from the page (`omega-web/src/omega/controllers/io.coffee:107-113`) | `packages/schema/subscription-source.schema.json:28-33` (`^https://`, no userinfo) |
| M5 | Status-code discrimination (404 ≠ delete, 5xx ≠ content) | no `response.ok` test in `omega-target-chromium-extension/src/module/fetch_url.coffee:10-13`; the classifiers that would have provided it are unreachable (`omega-target-chromium-extension/src/module/fetch_url.coffee:14-22`) | `packages/schema/protocol/commands/subscription.refresh.json#/invariants` (invariant "An HTTP 404 never deletes a subscription") |
| M6 | Cache-buster determinism for signed URLs | `omega-target-chromium-extension/src/module/fetch_url.coffee:36-40` injects a wall-clock `_` parameter, non-reproducibly, and only for query-less URLs | **not closed by either contract** — see §5 |
| M7 | Credential separation | `profile.headers` is used verbatim (`omega-target/src/options.coffee:792-796`) and is not stripped for sync (§4) | `packages/schema/subscription-source.schema.json:34-38` (`headersRef` keystore reference) |

The background-alarm path loses its user-visible signal entirely today: the badge/`setBadge` call that would
announce a failed periodic refresh is commented out
(`omega-target-chromium-extension/src/module/options.coffee:148-156`). **VERIFIED**

## 3. Import and export

| Path | Producer | Format / filename | Tag |
|---|---|---|---|
| Full backup export | `omega-web/src/omega/controllers/io.coffee:67-73` | `JSON.stringify` of the whole options object, `text/plain` blob, name `ZeroOmegaOptions-<ISO>.bak` (`:72`) | **VERIFIED** |
| Local import | `omega-web/src/partials/io.jade:24` → `omegaUpload` directive `omega-web/src/omega/directives.coffee:22-39` (`readAsText` at `:38`) → `omega-web/src/omega/controllers/io.coffee:82-87` → `omega-target/src/options.coffee:379-395` `reset` | accepts text or base64: `parseOptions` base64-decodes when the first char is not `{` (`omega-target/src/options.coffee:361-367`), then `JSON.parse`, then throws `Invalid options!` at `:370` | **VERIFIED** |
| Online restore | `omega-web/src/omega/controllers/io.coffee:104-118` — a page-side `$http` GET (10 s timeout, `responseType: 'text'`) of a user-entered URL, fed to the same `resetOptions` | unauthenticated, arbitrary scheme | **VERIFIED** |
| Per-profile PAC export | `omega-web/src/omega/controllers/master.coffee:33-55`, button at `omega-web/src/partials/profile.jade:10` | compiled through `OmegaPac.PacGenerator.script` (`omega-web/src/omega/controllers/master.coffee:49-52`), ASCII-folded, `OmegaProfile_<name>.pac`; Shift-key toggles a SOCKS export quirk (`:34`) | **VERIFIED** |
| Legacy rule-list export | `omega-web/src/omega/controllers/switch_profile.coffee:7-23` via `OmegaPac.RuleList.Switchy.compose` (`omega-pac/src/rule_list.coffee:88`) | emits the literal header `[SwitchyOmega Conditions]` (`omega-pac/src/rule_list.coffee:90`) plus a `; Require: ZeroOmega >= 2.3.2` line (`omega-web/src/omega/controllers/switch_profile.coffee:15`) into `OmegaRules_<name>.sorl` (`:23`) | **VERIFIED** |
| Source-view round-trip | `omega-web/src/omega/controllers/switch_profile.coffee:412-419` — `compose(args, {withResult: true})` renders the editable text pane, parsed back on collapse (`:421`) | the `@with result` / `@note` dialect (`omega-pac/src/rule_list.coffee:93`, `:99`) | **VERIFIED** |
| Legacy browser-import adapters | `omega-target-chromium-extension/src/module/upgrade.coffee:33` maps a Switchy/AutoProxy `ruleList*` config onto the `RuleListProfile` format names (`omega-pac/src/profiles.coffee:60-68`) | read by `Options.upgrade` (`omega-target/src/options.coffee:327-353`) | **VERIFIED** |
| `jszip` | loaded at `omega-web/src/options.jade:82`; referenced nowhere else in `omega-web/src/` (single grep hit) | **dead** — no zip container is produced by any export above; every artefact is a single flat file | **VERIFIED** |

Two import/export observations that bind the NEXORA migration: `reset` disables sync on the way in
(`omega-target/src/options.coffee:385-386`) and wipes local storage first (`:389-391`) — an import is destructive
by design; and `schemaVersion` is pinned at 2 with no v2→v3 path (`omega-target/src/options.coffee:349-353`), which
is the hole ADR-0014's `schemaVersion 3` has to fill.

## 4. Sync: `OptionsSync`, and SEC-1 as it actually reads

Mechanics (all **VERIFIED** by reading `omega-target/src/options_sync.coffee`):

| Mechanism | Line | Value |
|---|---|---|
| Debounce on push | `omega-target/src/options_sync.coffee:23`, applied at `:100` and `:107` | 1000 ms |
| Pull throttle | `omega-target/src/options_sync.coffee:29`, applied at `:227` | 1000 ms |
| Local push throttle to the backend | `omega-target-chromium-extension/src/module/sync_storage.coffee:121` | `setTimeout(_processPush, 600)` |
| Token bucket | `omega-target/src/options_sync.coffee:47` | `new TokenBucket(10, 10, 'minute')` — 10 ops / minute, from `limiter` (`:7`) |
| Extra token cost for `remove` | `omega-target/src/options_sync.coffee:132-139` | rejected as the string `'bucket'`, re-queued at `:149-150` |
| Rate-limit reaction | `omega-target/src/options_sync.coffee:151-156` | clears the bucket and re-schedules |
| Quota reaction | `omega-target/src/options_sync.coffee:157-170` | writes `value.syncOptions = 'disabled'` and `value.syncError = {reason: 'quotaPerItem'}` per `+`-key |
| Revision merge | `omega-target/src/options_sync.coffee:73-84` | keeps `oldVal` when `Revision.compare(oldVal.revision, newVal.revision) >= 0` (`:80-82`), else deep-equality via jsondiffpatch (`:83`) |
| Transform before merge | `omega-target/src/options_sync.coffee:103-105` | a `undefined` transform result drops the key from the push entirely |
| Remote → local | `omega-target/src/options_sync.coffee:200-227` | after applying, it collects profiles with an `updateUrl` (`:213-216`) and calls `updateProfile(names)` — sync can therefore trigger subscription fetches |

The wiring is in the worker: `omega-target-chromium-extension/src/coffee/background.coffee:233-246` constructs
`Storage('local')` (`:233`), `BrowserStorage(…, 'omega.local.')` (`:234`), the remote
`SyncStorage('sync', state)` (`:237`), the accelerator `Storage('sync')` (`:238`) and `OptionsSync` (`:239-241`),
then installs the transform at `omega-target-chromium-extension/src/coffee/background.coffee:242`. **VERIFIED**

### 4.1 `transformValueKeys` and the leak

`omega-target/src/options.coffee:89-99` is `Options.transformValueForSync`:

* `-customCss` is dropped wholesale (`omega-target/src/options.coffee:90-91`) — proof the exclusion mechanism
  exists and is reachable. **VERIFIED**
* For any `+` key that has an `updateUrl` (`:92-93`) it copies the profile **minus** the four names in
  `transformValueKeys = ['lastUpdate','ruleList','pacScript','sha256']` (`omega-target/src/options.coffee:27`,
  applied at `:94-98`). **VERIFIED**
* `headers` is **not** in that list, and §1 steps 1-2 show `headers` is a live, user-editable field
  (`omega-web/src/partials/profile_switch.jade:185-189` and `omega-web/src/partials/profile_pac.jade:27-29`
  render and mutate `profile.headers` through bound inputs). Therefore a subscription `Authorization` header is
  serialised verbatim into whatever the push writes. **VERIFIED** (chain read end to end); the classification
  **SEC-1** is settled in `docs/PROJECT-STATE.md` §2.2 and is not re-opened here. **UNVERIFIED**: whether the
  upstream store build differs; only this tree was inspected.

Refinement of the settled finding (§3.4 promised): the quota branch at
`omega-target/src/options_sync.coffee:157-170` is **unreachable in this fork's sync path**. The only producer of
`QuotaExceededError` / `RateLimitExceededError` is `ChromeStorage.parseStorageErrors`
(`omega-target-chromium-extension/src/module/storage.coffee:9-26`), which belongs to the *plain* `Storage`; the
object `OptionsSync` actually writes through is `ChromeSyncStorage`, whose own
`parseStorageErrors` is the identity re-throw
(`omega-target-chromium-extension/src/module/sync_storage.coffee:126-127`). So `syncOptions:'disabled'` can only
be produced by the local/accelerator storages, never by the Gist/WebDAV push. **VERIFIED** lines, **INFERRED**
unreachability.

### 4.2 Backends

* Dispatcher: `omega-target-chromium-extension/src/module/sync/sync_impl.coffee:29-35` — `webdav` selects
  `WebDavSyncImpl`, anything else falls back to Gist. Selected from the `syncBackendType` state
  (`omega-target/src/options.coffee:1177`) or auto-detected from the URI prefix in the UI
  (`omega-web/src/omega/controllers/io.coffee:20-25`). **VERIFIED**
* **Gist**: `omega-target-chromium-extension/src/module/sync/sync_impl_gist.coffee:30-46` reads commits and the
  options file; `:48-68` PATCHes the gist with `JSON.stringify(options, null, 4)` (`:53-55`); auth is
  `Authorization: Bearer <token>` (`:22-27`). The filename is assembled from a character array at
  `omega-target-chromium-extension/src/module/sync/sync_impl_gist.coffee:3-4` (→ `ZeroOmega.json`), duplicated at
  `omega-target-chromium-extension/src/module/sync_storage.coffee:14-15`. **VERIFIED**
* **WebDAV**: `omega-target-chromium-extension/src/module/sync/sync_impl_webdav.coffee:3-5` defines
  `zeroomega-commit.txt` and the `zeroomega-*.json` prefix, `:17` roots everything under a `zeroomega/` path,
  `:31-44` negotiates 401 into Basic or Bearer (`:66-69` builds `Authorization: Basic base64(user:token)`), and
  `:60-63` refuses Digest outright. **VERIFIED**
* **Built-in accelerator**: the `chrome.storage.sync` key is exactly **`zeroOmegaSync`**, declared twice
  (`omega-target/src/options_sync.coffee:10` and
  `omega-target-chromium-extension/src/coffee/background.coffee:9`). It is written by `toggleBuiltInSync`
  (`omega-target/src/options_sync.coffee:228-246`, the object built at `:240-241`) with
  `{gistId, gistToken, lastGistCommit, syncUsername, syncBackendType}`, read back at
  `omega-target/src/options_sync.coffee:247-250`, updated after each push
  (`omega-target-chromium-extension/src/module/sync_storage.coffee:101-103`) and watched at
  `omega-target-chromium-extension/src/coffee/background.coffee:259-262` so a second device auto-enables sync —
  including `state.set({gistId, gistToken})` at `:268-271`. Consequence: the Gist PAT travels through the browser
  account's sync storage. **VERIFIED**
* Sync state machine values written by the engine: `'unsupported'`
  (`omega-target/src/options.coffee:137` and `:170`), `'pristine'` (`:151`, `:187`, `:232`), `'conflict'`
  (`:185`, `:216`, `:1217`), `'sync'` (`:1184`, `:235`), `'disabled'`
  (`omega-target/src/options.coffee:1163`) — the last one from `setOptionsSync(false)`, distinct from the
  per-profile `'disabled'` of §4.1. **VERIFIED**
* Conflict semantics: enabling sync against a remote that already has `schemaVersion` refuses to overwrite and
  parks the state at `'conflict'` (`omega-target/src/options.coffee:1213-1219`); forcing is explicit
  (`omega-target/src/options.coffee:1170-1174`, UI button `omega-web/src/partials/io.jade:136`). **VERIFIED**
* The only durable sync status today is three *global* state keys — `lastGistCommit`, `lastGistState`,
  `lastGistSync` — written on success (`omega-target-chromium-extension/src/module/sync_storage.coffee:96-100`)
  and overwritten with `'fail: ' + e` on failure (`:107-110`, `:70-75`); a failed commit probe is treated as
  "probably changed" (`omega-target-chromium-extension/src/module/sync_storage.coffee:26-28`). Per-profile or
  per-key sync history does not exist. **VERIFIED**

## 5. What the two Phase-1 contracts already close

Read together, `packages/schema/subscription-source.schema.json` and
`packages/schema/protocol/commands/subscription.refresh.json` convert §2's M-list into data:

| Gap | Closed by | Mechanism |
|---|---|---|
| M1 durable status | `packages/schema/subscription-source.schema.json:123-190` | a persisted `status` object with `lastAttemptAt`, `lastSuccessAt`, `lastSha256`, `serverCount`, required `consecutiveFailures`, `nextAttemptAt`, and a `lastError.code` drawn from a fixed enum (`:166-177`) |
| M2 backoff/jitter | `packages/schema/subscription-source.schema.json:90-122` | `schedule.jitterPercent` (≤ 50, default 20), `backoffMultiplier` (1.5-10), `maxIntervalSeconds` cap, `intervalSeconds ≥ 300` |
| M3 size | `packages/schema/subscription-source.schema.json:58-84` | `limits.maxBytes` hard-capped at 8 388 608, plus `maxRedirects` and `timeoutMs`, all `required` (`:51-56`) |
| M4 scheme/SSRF | `packages/schema/subscription-source.schema.json:28-33` | `^https://[^\s@]+$` — https only, userinfo forbidden; the description states the loopback/metadata guard is Connector-side, not a schema property |
| M5 404 ≠ delete | `packages/schema/protocol/commands/subscription.refresh.json#/invariants` | invariant "An HTTP 404 never deletes a subscription; removal is an explicit user action", next to the keep-last-good invariant |
| M7 credentials | `packages/schema/subscription-source.schema.json:34-38` | `headersRef: keystore://…` replaces inline `headers`; the description names `omega-target/src/options.coffee:27` as the leak it closes |
| Result honesty | `packages/schema/protocol/commands/subscription.refresh.json#/result` | four disjoint outcome arrays (`refreshed` / `unchanged` / `failed` / `skipped`), `failed[]` carrying `retryable` + `consecutiveFailures` + `nextAttemptAt`, and an explicit note that `skipped` must not be read as success (`packages/schema/protocol/commands/subscription.refresh.json#/result/properties/skipped`) |
| Hammering protection | `packages/schema/protocol/commands/subscription.refresh.json#/args/properties/force` | `force` bypasses `nextAttemptAt` but is "rate-limited by the Connector so a UI defect cannot hammer a feed" |
| Loss reporting | `packages/schema/protocol/commands/subscription.refresh.json#/result/properties/refreshed/items/properties/lossyFields` | `lossyFields` per refreshed source, so the UI must ask instead of guessing a transport |

Still open after these contracts, stated plainly:

* **M6 cache-buster determinism.** Neither document carries a cache-policy field
  (`packages/schema/subscription-source.schema.json:7-16` lists the required keys and none is a fetch-policy), so
  the `?_=<timestamp>` behaviour of `omega-target-chromium-extension/src/module/fetch_url.coffee:39` — which breaks
  pre-signed feed URLs and makes every request non-reproducible — remains a Connector/ADR-level decision, not a
  schema obligation. **VERIFIED** (both files read in full).
* **Status ownership is undecided.** PROJECT-STATE §9 question 4 (extension-delivered vs Connector-owned
  subscriptions) is unresolved; the schema places `status` inside the *source* document
  (`packages/schema/subscription-source.schema.json:123`), which presumes one of the two answers. **UNVERIFIED**.
* **SEC-1 code fix is deliberately not made** (`docs/PROJECT-STATE.md` §10). Changing
  `omega-target/src/options.coffee:27` today has no test to catch a regression, and §5 of this document is the
  evidence for that: the exclusion list is applied inside a 10-line method with no unit coverage anywhere
  (`omega-pac/test` and `omega-target/test` hold the only 7 test files in the repo). **VERIFIED**.

## UNVERIFIED registry

| # | Item | Why it is not established here |
|---|---|---|
| 1 | Any runtime behaviour of the fetch/content-type/status paths | static reading only; no browser session was driven |
| 2 | That `response.headers['content-type']` evaluates to `undefined` in every target browser | based on the standard `Headers` interface, not on an executed probe |
| 3 | That the `'*'` hint accepting everything has caused a real bad-subscription incident | no issue/telemetry data consulted |
| 4 | Reachability of the `syncOptions:'disabled'` quota branch (§4.1 refinement) | proven for the code paths in this tree; a storage backend that throws `QuotaExceededError` directly would still reach it |
| 5 | Whether the store-published builds (Chrome/Edge/AMO) contain the same manifests and code as this tree | artifacts not downloaded; see `docs/audit/10` |
| 6 | Real-world existence of `Authorization`-bearing subscription feeds among users | plausible by construction, unmeasured |
| 7 | Behaviour of `Options.reset` + `upgrade` against a genuine 3.5.2 `.bak` file | `docs/verification/backup-from-3.5.2.json` exists but is exporter-shaped with example values — no browser ran in this iteration — so the genuine-`.bak` case is still open; `LIVE-14` closes it, and `tools/check-legacy-backup.mjs` already runs the compile half against the real engine |
| 8 | Whether an unhandled rejection results from a failed push (`omega-target/src/options_sync.coffee:171-172`) | no worker console was observed |
| 9 | `status`/`serverIds` ownership split between extension and Connector | open decision, PROJECT-STATE §9.4 |
