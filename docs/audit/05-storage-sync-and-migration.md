# 05 — Storage, sync and migration

> Purpose: inventory every store that holds user data in ZeroOmega v3.5.2, trace how a write reaches disk, how
> the sync pipeline actually works (it is **not** `chrome.storage.sync`), and what migration machinery exists —
> including the two paths that destroy data today. This is the measurement behind ADR-0014/0015,
> `packages/schema/envelope-backup.schema.json` and the future migration runner in `docs/architecture/35`.

Tags: **VERIFIED** = the cited line was opened and says so. **INFERRED** = deduced from cited code.
**UNVERIFIED** = assumed; every such item is in the registry at the end.

---

## 0. Four headline corrections

1. **Sync no longer uses `chrome.storage.sync` for options.** The sync remote is an IndexedDB key-value store
   (`omega-target-chromium-extension/src/module/sync_storage.coffee:131`) plus a network backend
   (`omega-target-chromium-extension/src/module/sync/sync_impl.coffee:29-35`). `chrome.storage.sync` survives only
   as the carrier of one configuration blob, `zeroOmegaSync`
   (`omega-target/src/options_sync.coffee:10`, instantiated as `Storage('sync')` at
   `omega-target-chromium-extension/src/coffee/background.coffee:238`). **VERIFIED**
2. **Unknown `schemaVersion` destroys the data**, it does not refuse to start — see §7 row 1 and §7.1. **VERIFIED**
3. **The Switchy! v1 migrator is orphan code.** `omega-target-chromium-extension/src/module/upgrade.coffee:1-181`
   is 181 lines of migration that nothing loads: it is not exported by the module index
   (`omega-target-chromium-extension/src/module/index.coffee:2-13`) and a repo-wide search for a `require` of it
   over tracked sources returns nothing. **VERIFIED** (by absence)
4. **State persistence in MV3 depends on a polyfill plus an IndexedDB mirror rewritten as one blob**
   (`omega-target-chromium-extension/overlay/localstorage-polyfill.js:5-45`,
   `omega-target/src/browser_storage.coffee:49-55`). It is the least durable part of the system. §3. **VERIFIED**

---

## 1. Storage class hierarchy

| Class | File | Backing | Notes that matter |
|---|---|---|---|
| `Storage` (base) | `omega-target/src/storage.coffee:5-147` | in-memory `@_items` | defines the error taxonomy `RateLimitExceededError` / `QuotaExceededError` / `StorageUnavailableError` (`omega-target/src/storage.coffee:10-31`), the change→operation reducer `operationsForChanges` (`:53-66`), `get` with default-injection for object keys (`:85-87`), `remove(null)` = wipe-all (`:110-111`), `watch` = **no-op returning a no-op** (`:130-132`), `apply` (`:142-145`) |
| `ChromeStorage extends Storage` | `omega-target-chromium-extension/src/module/storage.coffee:5-103` | `chrome.storage[areaName]` | real watch; error-string→class mapping at `:9-34`; `remove(null)` → `clear()` at `:57-59`; empty `set` short-circuit at `:52-54` |
| `BrowserStorage extends Storage` | `omega-target/src/browser_storage.coffee:6-96` | `localStorage`-shaped object + IndexedDB mirror | §3; inherits the no-op `watch` |
| `ChromeSyncStorage extends Storage` | `omega-target-chromium-extension/src/module/sync_storage.coffee:125-316` | IndexedDB `sync-store` + backend | §5 |
| `OptionsSync` | `omega-target/src/options_sync.coffee:12-275` | orchestrates two `Storage` objects | §5 |

Composition is by injection, and the wiring is six lines:
`omega-target-chromium-extension/src/coffee/background.coffee:233-247` builds `Storage('local')` (envelope),
`BrowserStorage(zeroStorage, 'omega.local.')` (state), `SyncStorage('sync', state)`, `Storage('sync')`
(built-in sync config), assigns the sync value filter at `:242`, and hands
`(storage, state, Log, sync, proxyImpl)` to `Options` at `:246-247`. **VERIFIED**

---

## 2. Store inventory (what a NEXORA migration has to touch)

| # | Store | Physical location | Contents | Written by |
|---|---|---|---|---|
| 1 | `chrome.storage.local` | browser profile dir | the whole options envelope (§4 of `docs/audit/04`) | `ChromeStorage('local')` — `omega-target-chromium-extension/src/coffee/background.coffee:233` |
| 2 | pseudo-`localStorage`, prefix `omega.local.` | MV3: in-memory map; Firefox: real `localStorage` | all *state* keys (`availableProfiles`, `syncOptions`, `customCss`, `gistToken`, …) | `omega-target-chromium-extension/src/coffee/background.coffee:234` |
| 3 | IndexedDB default store, key `localStorage` | IndexedDB | the **entire** pseudo-localStorage map as one blob | `omega-target/src/browser_storage.coffee:52`, `:90` |
| 4 | IndexedDB `sync-store` / store `sync` | IndexedDB | the local mirror of what is pushed to the remote | `omega-target-chromium-extension/src/module/sync_storage.coffee:131`, `:157` |
| 5 | `chrome.storage.sync` | browser account sync | one key: `zeroOmegaSync` (gist id, token, username, backend, last commit) | `omega-target/src/options_sync.coffee:240-242` |
| 6 | `chrome.storage.session` | SW lifetime | `tempProfileState` (`_tempProfile`, `_tempProfileActive`) | `omega-target-chromium-extension/src/module/options.coffee:136-140` |
| 7 | IndexedDB `log-store` | IndexedDB | diagnostic log entries and `lastError` | `omega-target-chromium-extension/src/coffee/omega_debug.coffee:2`, `omega-target-chromium-extension/overlay/log.js:2` |
| 8 | remote gist/WebDAV | network | full envelope JSON + commit id | `omega-target-chromium-extension/src/module/sync/sync_impl_gist.coffee:48-68`, `omega-target-chromium-extension/src/module/sync/sync_impl_webdav.coffee:112-147` |

Stores 1–3 and 4–7 are *all* user data: a migration that copies only `chrome.storage.local` silently drops state,
sync configuration and temp rules. **INFERRED** from the table above.

---

## 3. How a write becomes durable

Envelope path: `_setOptions` → `@_storage.set(changes)` → `ChromeStorage.set` → promisified
`chrome.storage.local.set` (`omega-target/src/options.coffee:476-482`,
`omega-target-chromium-extension/src/module/storage.coffee:52-55`, promisor
`omega-target-chromium-extension/src/module/chrome_api.coffee:4-18`). One key per changed entity; deletions are
batched through `remove(removed)` (`omega-target/src/options.coffee:478-481`). **VERIFIED**

State path, which is the unusual one:

1. The MV3 worker has no `localStorage`, so `zeroStorage` is a Proxy over an in-memory `Map`
   (`omega-target-chromium-extension/overlay/x-background.js:1`, `:62-63`;
   `omega-target-chromium-extension/overlay/localstorage-polyfill.js:5`, `:48-65`).
2. `BrowserStorage` speaks to that object **through the prototype**, bypassing the Proxy's own property
   semantics: `@proto = Object.getPrototypeOf(@storage)`
   (`omega-target/src/browser_storage.coffee:8`) then `@proto.getItem.call(@storage, @prefix + key)` (`:25`).
   **VERIFIED**
3. Durability is a side effect of the first access: `idbKeyval.get('localStorage')` seeds the map once per
   process, guarded by a module-level `_globalLocalStorageCache`
   (`omega-target/src/browser_storage.coffee:4`, `:11-14`), and every `set`/`remove` writes the **whole map** back
   under that single key (`omega-target/src/browser_storage.coffee:49-55`, `:87-93`). **VERIFIED**
4. Writes are made "really async" for Firefox with `setTimeout(…, 1)`
   (`omega-target/src/browser_storage.coffee:43-48`, `:80-86`) — the reason the debug reset has to wait two
   seconds before clearing IndexedDB (`omega-target-chromium-extension/src/coffee/omega_debug.coffee:42-48`).
   **VERIFIED**

Consequences, all **INFERRED** from the cited shapes (no browser run was performed):

* State durability is **all-or-nothing per write** and racy: if the worker is killed between a `setItem` and the
  IndexedDB write-back, the change is lost; if the write-back lands after a competing clear, deleted keys come
  back. The code itself admits the latter (`omega-target-chromium-extension/src/coffee/omega_debug.coffee:43-44`).
* `BrowserStorage.get` **mutates the caller's default map** (`map = keys` at
  `omega-target/src/browser_storage.coffee:21-22`) rather than copying, so the object you passed in as defaults is
  the object returned. **VERIFIED** for the assignment; aliasing effect **INFERRED**.
* `BrowserStorage.remove` with no argument runs the prefix-scan loop and then **falls through** into the
  string/array branches (`omega-target/src/browser_storage.coffee:63-78`), and inside that loop it deletes
  `@prefix + keys` (the whole parameter) instead of `@prefix + key` (the current entry) — `:72`. A prefixed wipe
  therefore cannot remove more than one key and then iterates an undefined `keys`. **VERIFIED** for the code
  shape; the runtime outcome (rejection) is **INFERRED**.
* The envelope is never watched through state: `BrowserStorage` inherits the base no-op `watch`
  (`omega-target/src/storage.coffee:130-132`), so the only live change channel is
  `chrome.storage.onChanged` on stores 1 and 5. **VERIFIED**

Session-store caveat: `chrome.storage.session` is used unguarded
(`omega-target-chromium-extension/src/module/options.coffee:106-107`, `:140`) while the manifest only demands
Chrome 88 (`omega-target-chromium-extension/overlay/manifest.json:29`); `chrome.storage.session` arrived later
than that floor, so the temp-profile restore throws on intermediate versions. Platform version history is
**UNVERIFIED** here; the absence of a guard is **VERIFIED**. NEXORA must not repeat it (PROJECT-STATE §2.5).

Keep-alive is `setInterval(chrome.runtime.getPlatformInfo, 25 * 1000)`
(`omega-target-chromium-extension/overlay/x-background.js:34`) — the worker is kept warm by polling, not by the
alarms that carry the real schedules (`omega-target-chromium-extension/src/module/options.coffee:290-298`).
**VERIFIED**

---

## 4. Change propagation inside the extension

| Channel | Installed by | Fires on | Handler |
|---|---|---|---|
| Options watcher | `omega-target/src/options.coffee:527` (`@_storage.watch null, handler`) | any `chrome.storage.local` change | `_setOptions(changes, {checkRevision: true, persist: false})` at `:485-487` |
| Fan-out | `omega-target-chromium-extension/src/module/storage.coffee:65-82` (single listener at `:80`) | one event per area | per-watcher dispatch `:84-98`, mapping `change.newValue` (`:96-97`) |
| Public watch API | `omega-target/src/options.coffee:583` | — | **no caller in tracked sources** (grep for `'watch'` as an RPC method returns nothing) — the UI polls instead (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:126-131`). **VERIFIED** (by absence) |
| Sync pull | `omega-target-chromium-extension/src/module/sync_storage.coffee:264-293` | alarm `omega.syncCheck`, every 5 minutes (`:265-267`) | `checkChange` → `onChangedListener` → `watchAndPull`'s callback |
| Built-in sync | `omega-target-chromium-extension/src/coffee/background.coffee:260-262` | `chrome.storage.sync` change on `zeroOmegaSync` | re-inits the backend and may force-enable sync (`:272-307`) |

Because the fan-out maps `newValue` verbatim (`omega-target-chromium-extension/src/module/storage.coffee:97`), a
storage *removal* arrives as an `undefined` value — which `_setOptions` interprets as a delete
(`omega-target/src/options.coffee:448-454`). Deletion and "absent" are therefore the same message on the wire.
**INFERRED** from the two cited lines.

---

## 5. The sync pipeline, end to end

**Push.** `_setOptions` calls `@sync?.requestPush(changes)` before persisting locally
(`omega-target/src/options.coffee:477`). `requestPush` transforms each value, **drops keys whose transformed value
is `undefined`** (`omega-target/src/options_sync.coffee:101-105`), debounces 1 s
(`omega-target/src/options_sync.coffee:23`, `:107`) and then `_doPush` spends a token from a
10-per-minute bucket (`omega-target/src/options_sync.coffee:47`, `:119`), reads the remote as `base` and reduces
changes to set/remove operations with the merge function (`:120-125`). **VERIFIED**

**Merge rules** (`omega-target/src/options_sync.coffee:73-84`): equal → keep old; both have `revision` and the old
one is not strictly newer (`Revision.compare` ≥ 0, `omega-pac/src/utils.coffee:5-13`) → keep old; a jsondiffpatch
diff finds no change → keep old; otherwise take new. `Revision` is a base-16 millisecond timestamp
(`omega-pac/src/utils.coffee:2-3`), compared by length then lexicographically (`:9-12`) — so a profile **without**
a revision always loses to one that has it (`:7`), and revisions from different clocks compare only by string
length. **VERIFIED**

**Pull.** `watchAndPull` throttles 1 s and, crucially, writes through `local.apply(operations)`
(`omega-target/src/options_sync.coffee:200-218`) — i.e. **straight into storage, bypassing `_setOptions`** — then
re-updates any pulled profile that has an `updateUrl` (`:211-217`). Consistency is restored only because the
`chrome.storage.onChanged` watcher (§4) re-absorbs the write. **VERIFIED**

**Remote mirror.** `ChromeSyncStorage` is a thin IndexedDB key-value façade: `get(null)` is
`idbKeyval.entries` over the whole store (`omega-target-chromium-extension/src/module/sync_storage.coffee:78-85`),
`set` validates "object only" and schedules a push (`:148-163`), `remove`/`clear` also schedule a push
(`:164-178`), and every push coalesces through a queue drained 600 ms later
(`:87-123`, timer at `:121`). While a push is in flight, `processPull` returns *no changes*
(`:37-39`) — an in-process loop guard, not a lock. **VERIFIED**

**Backends.** Gist: the options file name is assembled from a character array
(`omega-target-chromium-extension/src/module/sync/sync_impl_gist.coffee:3-4`), auth is
`Authorization: Bearer <token>` (`:22-27`), and `pushOptions` is a `PATCH` that **ignores its
`previousCommitId` argument** (`:48-60`, parameter declared at `:48` and never used in the request) — no
compare-and-swap, last writer wins. WebDAV: `MKCOL` on init
(`omega-target-chromium-extension/src/module/sync/sync_impl_webdav.coffee:15-22`, `:83-84`), commit pointer file
plus one snapshot file per commit, and the **previous snapshot is `DELETE`d after a successful push**
(`:112-147`, deletion at `:137-143`), leaving exactly one restore point; commit ids are 20 random hex chars, not
content hashes (`:149-156`); `Digest` auth is refused explicitly (`:60-63`) and Basic auth is `btoa(user:pass)`
(`:66-73`). **VERIFIED**

**Consequences for the contract** (design belongs to Phase 1, recorded here as measurement): no backend keeps
history, no backend does conditional write, the WebDAV path deletes the only prior copy, and the token lives in
state plus `chrome.storage.sync`. Therefore `envelope-backup.schema.json`'s required fields
(`packages/schema/envelope-backup.schema.json:7-16`) — including `contentSha256`
(`packages/schema/envelope-backup.schema.json:52-56`) and a non-zero `keyCount`
(`packages/schema/envelope-backup.schema.json:46-51`) — are not satisfiable by anything in this tree today.
**VERIFIED** (both sides read)

**State machine.** `syncOptions` takes `pristine` / `sync` / `disabled` / `conflict` / `unsupported`, written at
`omega-target/src/options.coffee:137`, `:150-152`, `:170`, `:185`, `:215-217`, `:232`, `:235`, `:1163`, `:1184`,
`:1217`, `:1245`, `:1255`, and branched on in the UI at `omega-web/src/partials/io.jade:85`, `:101`, `:125`,
`:145`. `loadOptions` decides the mode **before** reading the envelope
(`omega-target/src/options.coffee:140-173`), so a sync-enabled install reads the remote first and only then
upgrades whatever it got. **VERIFIED**

---

## 6. Failure handling

| Failure | Where | Response today | Data effect | Tag |
|---|---|---|---|---|
| Any error loading options | `omega-target/src/options.coffee:188-249` | 3 retries (`:129`), then fall back | see row 7 | **VERIFIED** |
| `NoOptionsError` (thrown when nothing has `schemaVersion`, `omega-target/src/options.coffee:327-328`) | `omega-target/src/options.coffee:191-241` | first-run markers, try remote, else defaults | none | **VERIFIED** |
| Other serious load error | `omega-target/src/options.coffee:242-247` | `@_state.remove(['syncOptions'])`, defaults | sync silently disabled | **VERIFIED** |
| Fallback applied | `omega-target/src/options.coffee:249-258` | `@_storage.remove()` **then** `set(defaults)` then reload | envelope replaced | **VERIFIED** |
| Sync storage unavailable | `omega-target/src/options.coffee:164-170` | `@sync = null`, `syncOptions: 'unsupported'` | none | **VERIFIED** |
| Remote quota exceeded | `omega-target/src/options_sync.coffee:157-170` | mark offending profiles `syncOptions: 'disabled'` + `syncError` | remote silently partial | **VERIFIED** |
| Remote rate limit | `omega-target/src/options_sync.coffee:149-156` | clear bucket, `requestPush({})` retry | none | **VERIFIED** |
| Push error after the queue was drained | `omega-target/src/options_sync.coffee:132-147` | re-submit loop iterates a `set` **reset to `{}`** at `:133` | pending set-changes dropped on retry | **INFERRED** (read, not executed) |
| `chrome.storage` quota/permission strings | `omega-target-chromium-extension/src/module/storage.coffee:9-34` | mapped to the three error classes | — | **VERIFIED** |

---

## 7. Migration surface, and where data dies

| # | Mechanism | Trigger | What it does | Verdict |
|---|---|---|---|---|
| 1 | `Options.upgrade` | every `loadOptions` (`omega-target/src/options.coffee:175-178`) and every `reset` (`:383`) | `1 → 2` (`:330-344`), strips `syncOptions`/`syncError` from profiles set to `disabled` (`:345-348`), **rejects every other version** (`:349-353`) | see §7.1 |
| 2 | `ChromeOptions.upgrade` | same | converts "no `schemaVersion`" into `NoOptionsError` (`omega-target-chromium-extension/src/module/options.coffee:325-328`) | benign |
| 3 | `onInstalled` wipe | install/update (`omega-target-chromium-extension/src/coffee/background.coffee:60-64`) | clears `chrome.storage.sync` + `chrome.storage.local` + the default IndexedDB store when `current > previous` **and** `3.3.0 > current` (`:41-47`) | **destructive**, ADR-0015 |
| 4 | Switchy! v1 reader | **never** (`omega-target-chromium-extension/src/module/upgrade.coffee:1-181`, orphan) | would rebuild an envelope from `config`/`profiles`/`rules` JSON strings | dead code |
| 5 | SwitchySharp import | hard-disabled: `… != SwitchySharp.extId and false` (`omega-target-chromium-extension/src/coffee/background.coffee:256-258`) | nothing | dead code |
| 6 | Reset paths | UI `reset`, `resetAllOptions`, debug reset | `remove` + `set` (`omega-target/src/options.coffee:379-395`); `Promise.all([sync.clear(), local.clear()])` (`omega-target-chromium-extension/src/coffee/background.coffee:450-457`); `localStorage.clear()` + store clears + reload (`omega-target-chromium-extension/src/coffee/omega_debug.coffee:36-56`) | destructive, no backup |
| 7 | Online restore | `omega-web/src/omega/controllers/io.coffee:104-118` | GETs an arbitrary URL kept in state and calls `reset` with the raw body | destructive, unauthenticated, no backup |
| 8 | Sync enable under conflict | `omega-target/src/options.coffee:1190-1199` | local `remove()`, then `flush({data: remote})` overwrites the IndexedDB mirror | destructive to local |
| 9 | Sync reset | `omega-target/src/options.coffee:1237-1256` | `@sync.storage.remove()` → `clear()` on the remote mirror → push of an empty set (`omega-target-chromium-extension/src/module/sync_storage.coffee:174-178`) | **destroys the remote**, guarded by nothing but the UI dialog |
| 10 | Cache-busting fetch | `omega-target-chromium-extension/src/module/fetch_url.coffee:36-46` | appends `?_=<ms>` when the URL has no query | not migration, but it is the only "refresh" mechanism subscriptions have |

### 7.1 The one that must be fixed first

An envelope whose `schemaVersion` is anything other than `1` or `2` follows this chain: `upgrade` rejects with
`Invalid schemaVersion` (`omega-target/src/options.coffee:353`) → `loadOptions`' catch does not recognise it as
`NoOptionsError`, so it logs and returns `null` fallback options (`:188-190`, `:242-247`) → the fallback
**removes every key and writes defaults** (`:254-256`) → `@loadOptions({retry: retry - 1})` reloads them
(`:258`). There is no branch that preserves an unreadable envelope. So the day NEXORA writes a `3`, a legacy
engine — or a rollback to one — erases the user's data. **VERIFIED** (every step cited)

This is the concrete justification for three decisions already taken: keep the legacy stamp at `2` and version
NEXORA separately (`packages/schema/envelope-keys.schema.json:16-19`), require a pre-migration backup whose
`toSchemaVersion` must not go backwards (`packages/schema/envelope-backup.schema.json:35-40`), and never let the
migration runner write a step against a zero-key snapshot
(`packages/schema/envelope-backup.schema.json:46-51`, decision text in ADR-0014).

Two wording defects in the existing contract, recorded for its owner (not edited here):
`packages/schema/envelope-keys.schema.json:104` describes the strip-list as applying "before a profile goes to
`chrome.storage.sync`", which is how §5 shows it actually works only for the `zeroOmegaSync` blob — options never
travel through `chrome.storage.sync` in this fork; and `legacyOwned`
(`packages/schema/envelope-keys.schema.json:78-81`) names keys the engine does not use (`docs/audit/04` §8). The
verifier checks namespace prefixes and the `secretBearing`/`syncEligible` cross-rule
(`packages/schema/scripts/verify.mjs` "namespace stays out of the profile prefix: ${ns.entity}" and "secret-bearing namespace is not sync-eligible: ${ns.entity}") but does not check `legacyOwned` against source at all. **VERIFIED**

---

## UNVERIFIED registry

| # | Claim | Why it could not be checked here |
|---|---|---|
| 1 | `idbKeyval.createStore('sync-store', 'sync')` means database `sync-store`, object store `sync` (`omega-target-chromium-extension/src/module/sync_storage.coffee:131`) | `idb-keyval` is vendored into a derived `build/` tree and is not a tracked source file; the call-shape reading is the best available evidence |
| 2 | `idbKeyval.clear()` with no store argument wipes the **default** store (`omega-target-chromium-extension/src/coffee/background.coffee:46`) | same vendored-library reason; the wipe's *conditions* are verified, its exact target scope is not |
| 3 | `compare-versions(current, previous, '>')` semantics at `omega-target-chromium-extension/src/coffee/background.coffee:41-42` | library is vendored, not tracked; the arithmetic consequence is taken from ADR-0015's table, not re-derived |
| 4 | `BrowserStorage.remove` with no argument rejects (TypeError over `undefined`) | CoffeeScript's compiled loop shape for `for key in keys` (`omega-target/src/browser_storage.coffee:77`) was read, not executed |
| 5 | Whether the state mirror can resurrect deleted keys in practice | depends on IndexedDB write-back timing (`omega-target/src/browser_storage.coffee:49-55`) and a real worker lifecycle; the code comment at `omega-target-chromium-extension/src/coffee/omega_debug.coffee:43-44` suggests the authors saw it, but no run was possible |
| 6 | `chrome.storage.session` availability starts above Chrome 88 | platform version history is external knowledge; only the missing guard is verified |
| 7 | `processCheckCommit`'s boolean never reaches `checkChange`'s `isChanged` (`omega-target-chromium-extension/src/module/sync_storage.coffee:17-28`, used at `:249-250`) | the promise chain is readable and the `return` at `:23` sits in an inner callback, but the resolved value depends on what `state.set` yields (`omega-target/src/browser_storage.coffee:33-56`); conclusion — periodic pulls without `force` never detect remote change — needs a run |
| 8 | Whether Gist `PATCH` without an etag/if-match can clobber a concurrent write | network behaviour; only the unused `previousCommitId` argument is verified |
| 9 | Real-world distribution of `schemaVersion` values among existing users | no data corpus; drives how urgent §7.1 is, not whether it is true |
| 10 | That stores 1–7 are exhaustive for user data | derived from the composition root `omega-target-chromium-extension/src/coffee/background.coffee:233-251` plus `omega-target-chromium-extension/overlay/x-background.js:62-63`; the popup pages' own `localStorage` writes (e.g. `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:120`) were not exhaustively enumerated |
