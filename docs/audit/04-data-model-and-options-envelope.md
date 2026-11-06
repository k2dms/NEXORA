# 04 — Data model and the options envelope

> Purpose: state exactly what the persisted user data of ZeroOmega v3.5.2 *is* — the profile-type registry and
> each type's field contract, the flat key map that holds it, the identity/reference rules, and every writer and
> copier of that map — so that the NEXORA entity model can be laid on top of it without silently changing it.
> The normative key contract for the new entities lives in `packages/schema/envelope-keys.schema.json` and
> ADR-0012; this document is the measurement those documents were written against.

Tags: **VERIFIED** = the cited line was opened and says so. **INFERRED** = deduced from cited code.
**UNVERIFIED** = assumed; every such item is in the registry at the end.

---

## 0. Counts, corrected against the plan

| Thing | Plan said | Measured in this tree | Evidence | Tag |
|---|---|---|---|---|
| `profileType` values | 8 | **10 names**, of which **6 carry a handler object** and **4 are string aliases** | `omega-pac/src/profiles.coffee:253-521` | **VERIFIED** |
| `conditionType` values | 9 | **12** | `omega-pac/src/conditions.coffee:227-687` | **VERIFIED** |
| Builtin profiles synthesised by the engine | — | **2** (`+direct`, `+system`) | `omega-pac/src/profiles.coffee:34-44` | **VERIFIED** |
| Profiles shipped in the default envelope | 8 | **2** (`+proxy`, `+auto switch`) → 4 visible profiles on first run with the builtins | `omega-target/src/default_options.coffee:14-35`, `:37-57` | **VERIFIED** |
| `-`-prefixed setting keys read or written anywhere in `src` | — | **16**, plus the bare `schemaVersion` | §4 below, each cited | **VERIFIED** |
| `-` keys present in the shipped defaults | — | **11** (`schemaVersion` is `2` at `omega-target/src/default_options.coffee:2`) | `omega-target/src/default_options.coffee:2-13` | **VERIFIED** |

**False plan assumption, recorded.** The plan referred to a "`module/proxy` profile". There is no such thing:
`+proxy` is a *data item* — a `FixedProfile` named `proxy` inside the default options
(`omega-target/src/default_options.coffee:14-35`) — while `omega-target-chromium-extension/src/module/proxy/` is
the directory holding the `proxyImpl` capability layer (`omega-target-chromium-extension/src/module/proxy/index.coffee:1`).
Nothing reads one through the other; renaming or deleting the `+proxy` profile does not affect the proxy
implementation, and vice versa. **VERIFIED** (both files opened).

---

## 1. The shape: one flat map, three namespaces

`Options` holds "all the options, in a map from key to value" (`omega-target/src/options.coffee:58`), i.e. a
flat JSON object: top-level keys only, values arbitrary. It is not a document tree, not a collection, and has no
per-entity table. Three namespaces live in three *different* stores, which is the single most confusing fact for
anyone designing NEXORA persistence:

| Namespace | Key form | Backing store | Object holding it | Writers |
|---|---|---|---|---|
| **Envelope** (profiles + settings) | `+<name>`, `-<name>`, bare `schemaVersion` | `chrome.storage.local` | `ChromeStorage('local')` — `omega-target-chromium-extension/src/coffee/background.coffee:233` | §7 |
| **State** (derived/session-ish UI + sync status) | bare names, some dotted (`web.*`) | pseudo-`localStorage` under the `omega.local.` prefix | `BrowserStorage(zeroStorage, 'omega.local.')` — `omega-target-chromium-extension/src/coffee/background.coffee:234` | `omega-target/src/options.coffee:638-641`, `:665-670`, `:494-505` |
| **Session** (temp-profile only) | `tempProfileState` | `chrome.storage.session` | direct API calls — `omega-target-chromium-extension/src/module/options.coffee:106-107`, `:140` | `omega-target-chromium-extension/src/module/options.coffee:133-140` |

**INFERRED** consequence that matters for NEXORA: state keys are *not* in the envelope, so they are not exported,
not synced and not migrated by anything in `Options.upgrade`; yet the UI reads them as if they were settings
(`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:99-116`). A NEXORA entity placed in state
would be invisible to backup files.

---

## 2. `_profileTypes`: the registry that defines what a profile can be

Dispatch is one lookup plus an alias chain, and an unknown type is a hard error:
`_handler` `omega-pac/src/profiles.coffee:242-251`, resolving `while typeof handler == 'string'` at `:247-248`
and `throw new Error "Unknown profile type: …"` at `:250`.

| Registered name | Kind | Lines | `includable` | `inclusive` | Has `updateUrl`/`update` | Tag |
|---|---|---|---|---|---|---|
| `SystemProfile` | handler | `omega-pac/src/profiles.coffee:256-258` | no | no | no | **VERIFIED** |
| `DirectProfile` | handler | `omega-pac/src/profiles.coffee:259-262` | yes `:260` | no | no | **VERIFIED** |
| `FixedProfile` | handler | `omega-pac/src/profiles.coffee:263-349` | yes `:264` | no | no | **VERIFIED** |
| `PacProfile` | handler | `omega-pac/src/profiles.coffee:350-392` | conditional `:351` | no | yes `:378-392` | **VERIFIED** |
| `AutoDetectProfile` | alias → `PacProfile` | `omega-pac/src/profiles.coffee:393` | (inherits) | (inherits) | (inherits) | **VERIFIED** |
| `SwitchProfile` | handler | `omega-pac/src/profiles.coffee:394-444` | yes `:395` | yes `:396` | no | **VERIFIED** |
| `VirtualProfile` | alias → `SwitchProfile` | `omega-pac/src/profiles.coffee:445` | yes | yes | no | **VERIFIED** |
| `RuleListProfile` | handler | `omega-pac/src/profiles.coffee:446-519` | yes `:447` | yes `:448` | yes `:492-519` | **VERIFIED** |
| `SwitchyRuleListProfile` | alias → `RuleListProfile` | `omega-pac/src/profiles.coffee:520` | yes | yes | yes | **VERIFIED** |
| `AutoProxyRuleListProfile` | alias → `RuleListProfile` | `omega-pac/src/profiles.coffee:521` | yes | yes | yes | **VERIFIED** |

Two aliases are not cosmetic: `formatByType` maps them to a rule-list format
(`omega-pac/src/profiles.coffee:60-63`) which `create` consumes (`:451`), and the UI normalises them back to
`RuleListProfile` before saving (`omega-web/src/omega/controllers/profile.coffee:107-109`).
`SwitchyRuleListProfile`/`AutoProxyRuleListProfile` therefore never survive a round trip through the options page
as their own name. **INFERRED** from those two readings.

`SystemProfile` has no `compile` that works — it throws `"SystemProfile cannot be used in PAC scripts"`
(`omega-pac/src/profiles.coffee:257-258`). So "system" is representable **only** as a browser proxy config, never
inside a generated PAC. Any NEXORA export of routing to a `.pac` file must reproduce that restriction. **VERIFIED**

---

## 3. Field contract per profile type

Common fields (all types): `name` (identity), `profileType` (dispatch), `color`
(`omega-target/src/default_options.coffee:31`), `builtin` (`omega-pac/src/profiles.coffee:39`), `revision`
(`omega-pac/src/profiles.coffee:152-154`), plus `syncOptions`/`syncError` written by the quota fallback
(`omega-target/src/options_sync.coffee:161-165`).

| Type | Required / defaulted fields | Optional fields | Derived fields written by the engine |
|---|---|---|---|
| `FixedProfile` | `bypassList` default of 3 `BypassCondition` entries — `omega-pac/src/profiles.coffee:266-279` | `proxyForHttp`, `proxyForHttps`, `proxyForFtp`, `fallbackProxy` (scheme→prop table `omega-pac/src/profiles.coffee:46-51`), `auth` per-prop or `all` (`:290`, `:296`) | none |
| `PacProfile` | `pacScript` default stub — `omega-pac/src/profiles.coffee:353-357` | `pacUrl` (`:351`, `:378-382`), `headers` array of `{name,value}` (`omega-web/src/omega/controllers/pac_profile.coffee:53-54`) | none |
| `SwitchProfile` | `defaultProfileName` (`omega-pac/src/profiles.coffee:398`), `rules` array (`:399`) | per-rule `note`, `source` | none |
| `VirtualProfile` | same as `SwitchProfile` + `defaultProfileName` is the resolved target (`omega-pac/src/profiles.coffee:445`) | — | exposed in state only: `omega-target/src/options.coffee:620-621` |
| `RuleListProfile` | `format` (`omega-pac/src/profiles.coffee:451`), `defaultProfileName` `:452`, `matchProfileName` `:453`, `ruleList` `:454` | `sourceUrl` (`:492`), `headers` (`omega-web/src/omega/controllers/switch_profile.coffee:464-465`) | `pacScript` regenerated on every write (`omega-target/src/options.coffee:462-463`, generator `:29-45`), `lastUpdate` + `sha256` on fetch (`:805-807`), `isTempPacProfile` marker (`:35`) |

Field-level rules that are easy to break:

1. **Names are free-form strings.** Validation covers emptiness, collision and the `__` reservation only
   (`omega-web/src/omega/controllers/master.coffee:136-139`,
   `omega-web/src/partials/new_profile.jade:10`); the engine never parses a name.
2. **A stored profile whose name collides with a builtin is shadowed.** Lookup order is
   `exports.builtinProfiles[key] ? options[key]` (`omega-pac/src/profiles.coffee:101`, same at `:107`), and only
   `color` can be customised for a builtin (`omega-pac/src/profiles.coffee:24-31`). **VERIFIED**
3. **Derived data is stored next to source data.** `pacScript`, `sha256`, `lastUpdate` live inside the profile and
   are persisted; they are stripped only for sync (`omega-target/src/options.coffee:27`, `:92-98`) and are present
   in export files. **VERIFIED**
4. **Caches are attached to the profile object, not to the envelope.** `AttachedCache` writes a `_cache` property
   through `Object.defineProperty obj, prop, writable: true`
   (`omega-pac/src/utils.coffee:36-38`) — with `enumerable` left at its default `false`, so `JSON.stringify` and
   `for own` never see it. **INFERRED** from the cited call shape (spec semantics), not observed.

---

## 4. The flat envelope: every `-` key, and the bare one

Read from code, not from documentation. Eleven are shipped by default
(`omega-target/src/default_options.coffee:2-13`): `schemaVersion` (bare, `:2`), `-enableQuickSwitch` `:3`,
`-refreshOnProfileChange` `:4`, `-startupProfileName` `:5`, `-quickSwitchProfiles` `:6`, `-revertProxyChanges`
`:7`, `-confirmDeletion` `:8`, `-showInspectMenu` `:9`, `-addConditionsToBottom` `:10`,
`-showResultProfileOnActionBadgeText` `:11`, `-showExternalProfile` `:12`, `-downloadInterval` `:13`.
Five more are created lazily by the engine or the UI: `-customCss`
(`omega-target/src/options.coffee:496-498`), `-monitorWebRequests` (`omega-target/src/options.coffee:519-524`),
`-builtinProfiles` (`omega-pac/src/profiles.coffee:29`, `omega-web/src/omega/controllers/builtin.coffee:26-27`),
`-showConditionTypes` (`omega-web/src/omega/controllers/switch_profile.coffee:168-169`),
`-exportLegacyRuleList` (`omega-web/src/omega/controllers/switch_profile.coffee:154`).

Three invariants follow, all falsifiable:

* **The version stamp is a bare key.** `options['schemaVersion']`
  (`omega-target/src/options.coffee:329`, `:344`, `:231`; `omega-target-chromium-extension/src/module/options.coffee:327`;
  `omega-target-chromium-extension/src/module/sync_storage.coffee:207`). There is **no** `-schemaVersion` anywhere
  in `src` — grep over the five projects' sources returns nothing. **VERIFIED** (by absence).
* **`-` values may themselves contain `+`-shaped keys**, which the profile iterator cannot see:
  `-builtinProfiles` is a map keyed `+system`/`+direct`
  (`omega-web/src/omega/controllers/builtin.coffee:12-18`). So "`+` means profile" is true at depth 1 only.
  **VERIFIED**
* **References inside `-` values are bare names, not keys**: `-startupProfileName`
  (`omega-target/src/options.coffee:269-274`), `-quickSwitchProfiles` (`:549`), and inside profiles
  `defaultProfileName`/`profileName`/`matchProfileName` (`omega-target/src/default_options.coffee:57`). The
  `'+' +` conversion is applied at the use site (`omega-target/src/options.coffee:453`,
  `omega-pac/src/profiles.coffee:93-96`). **VERIFIED**

---

## 5. Identity and iteration rules (the hard constraint)

`Profiles.each` iterates a storage map and calls back **only** for keys whose first char code is `'+'`
(`omega-pac/src/profiles.coffee:111-114`), then re-emits the two builtins
(`omega-pac/src/profiles.coffee:115-118`). Every profile-facing feature is downstream of that one test:
`_setAvailableProfiles` (`omega-target/src/options.coffee:613-634`), `validResultProfilesFor`
(`omega-pac/src/profiles.coffee:224-226`), `referencedBySet` (`:211-214`), the quick-switch cleanup
(`omega-target/src/options.coffee:532-538`), profile updates (`:783`), rename/replace reference sweeps
(`omega-target/src/options.coffee:835-840`) and the legacy migration (`omega-target/src/options.coffee:332`,
`:345`). Key construction is `nameAsKey` = `'+' + name` (`omega-pac/src/profiles.coffee:93-96`).

`Profiles.updateProfile`/`_setOptions` also treat the prefix as a *change class*: a removed `+` key marks
`profilesChanged` and, if it is the current profile, forces a fallback apply (`omega-target/src/options.coffee:451-454`,
`:469-475`); a changed `-builtinProfiles` is the only scalar with that power (`:464-465`). **VERIFIED**

Hidden/reserved names are a UI convention, not an engine rule: `isProfileNameHidden` (leading `_`) and
`isProfileNameReserved` (leading `__`) live in `omega-web/src/omega/app.coffee:22-30`, the attachment convention
`__ruleListOf_<name>` in `omega-web/src/omega/app.coffee:13-20` (used at
`omega-web/src/omega/controllers/switch_profile.coffee:282-283` and created as a real profile at
`omega-web/src/omega/controllers/switch_profile.coffee:338-346`; produced by the legacy migrator at
`omega-target-chromium-extension/src/module/upgrade.coffee:30`, `:37`). Because the attachment is stored under
`+__ruleListOf_…`, `Profiles.each` **does** enumerate it — it is a real profile that the UI merely hides. **VERIFIED**

---

## 6. Rules, conditions and the reference graph

A `SwitchProfile` rule is `{condition: {conditionType, …}, profileName}`
(`omega-target/src/options.coffee:1026-1034`), optionally with `source` (`omega-pac/src/rule_list.coffee:50`),
`note` (`omega-pac/src/rule_list.coffee:98-99`) or `isTempRule` (`omega-target/src/options.coffee:957-962`).
Condition types (all 12, `omega-pac/src/conditions.coffee`): `TrueCondition` `:230`, `FalseCondition` `:238`,
`UrlRegexCondition` `:248`, `UrlWildcardCondition` `:256`, `HostRegexCondition` `:268`,
`HostWildcardCondition` `:276`, `BypassCondition` `:300`, `KeywordCondition` `:441`, `IpCondition` `:467`,
`HostLevelsCondition` `:579`, `WeekdayCondition` `:613`, `TimeCondition` `:661`; unknown type throws at
`omega-pac/src/conditions.coffee:223-224`. Each type carries abbreviation strings
(`omega-pac/src/conditions.coffee:76-84`) used for a text round trip (`:55-73`) — so condition values are
parseable in both directions, which is what makes rule-list text a data format. **VERIFIED**

Reference edges are computed, never stored: `directReferenceSet` exists only for `inclusive` profiles
(`omega-pac/src/profiles.coffee:168-173`), built from `defaultProfileName` + each rule's `profileName`
(`:400-406`) or from `matchProfileName`/`defaultProfileName` plus parsed rule-list result profiles
(`:455-462`, `omega-pac/src/rule_list.coffee:68-84` — the latter requires a `@with result` header, `:73`).
`allReferenceSet` walks transitively (`omega-pac/src/profiles.coffee:193-205`), `referencedBySet` scans the whole
envelope (`:206-216`), and `validResultProfilesFor` returns includable profiles that are not in the closure
(`:217-227`). A reference to a nonexistent name is **not** an error at storage time: it becomes a
`VirtualProfile` stand-in only when a compile asks for it (`omega-pac/src/profiles.coffee:175-191`,
`omega-target/src/options.coffee:585-591`). **VERIFIED**

The `'+'` prefix continues into the *output*: the generated PAC keys its profile table by envelope key
(`omega-pac/src/pac_generator.coffee:31-39`, with `+direct` elided at `:32`) and resolves chains until the string
no longer starts with `'+'` (`omega-pac/src/pac_generator.coffee:119-141`, comparison at `:138`). So `'+'` is
simultaneously a storage-key prefix, a compile-time table key and a runtime protocol marker inside user-visible
PAC text. **VERIFIED**

---

## 7. Who may write the envelope, and how

| Writer | Entry point | Mechanism | Tag |
|---|---|---|---|
| Options page (bulk edit) | `omega-web/src/omega/controllers/master.coffee:90-96` → RPC `patch` | jsondiffpatch diff of the *whole* copied envelope (`:63-66`) against `omega-target/src/options.coffee:426-440` | **VERIFIED** |
| Engine, single change | `omega-target/src/options.coffee:442-482` | key-by-key; `undefined` deletes; `+` keys trigger profile work; persist via `@_storage.set(changes)` then `remove(removed)` `:476-482` | **VERIFIED** |
| Typed mutators | `addProfile` `:1070-1078`, `addCondition` `:1003-1039`, `setDefaultProfile` `:1047-1063`, `renameProfile` `:884-909`, `replaceRef` `:860-876` | each recomputes `revision` and re-keys by `nameAsKey` | **VERIFIED** |
| Subscription refresh | `omega-target/src/options.coffee:780-818` | mutates the live profile object, then `@_setOptions({[key]: profile})` `:809-811` | **VERIFIED** |
| Remote sync pull | `omega-target/src/options_sync.coffee:200-227` → `Storage.apply` | writes straight into `@_storage`, bypassing `_setOptions`; the `Options._watch` handler re-absorbs it (`omega-target/src/options.coffee:484-527`, `:527`) | **VERIFIED** |
| Background RPC dispatcher | `omega-target-chromium-extension/src/coffee/background.coffee:458-481` | **any** `Options` method name is callable (`target[request.method]`, `:471-472`); only `resetAllOptions`/`getState`/`setState` are special-cased `:461-469` | **VERIFIED** |
| Reset / replace | `omega-target/src/options.coffee:379-395` (`remove` then `set`), `:113-120` | destructive, no backup | **VERIFIED** |
| External extension | `omega-target-chromium-extension/src/module/external_api.coffee:40-61` | read-only: `getOptions` returns `@options.getAll()` `:59-61` for one hard-coded extension id `:10` | **VERIFIED** |

Read-side copiers of the *same* map: `getAll` for every UI refresh
(`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:126-131`), the backup download
(`omega-web/src/omega/controllers/io.coffee:67-73`), the online restore that replaces the whole envelope from any
URL's response body (`omega-web/src/omega/controllers/io.coffee:104-118`), and the sync push filter
(`omega-target/src/options.coffee:89-99`, applied as `omega-target-chromium-extension/src/coffee/background.coffee:242`).

Three write-path defects are load-bearing for NEXORA and are recorded here (not fixed):

1. `renameProfile`'s duplicate-name rejection interpolates an undefined variable, so the user-visible message is
   "Target name undefined already taken!" (`omega-target/src/options.coffee:886-887`). **VERIFIED**
2. The sync push filter returns `undefined` for `-customCss`, and `requestPush` **skips** such keys entirely — a
   CSS deletion never propagates to the remote (`omega-target/src/options.coffee:89-92` +
   `omega-target/src/options_sync.coffee:101-105`). **INFERRED** from the two cited shapes.
3. The quota fallback mutates profile objects in place and only re-pushes to the remote
   (`omega-target/src/options_sync.coffee:157-170`); because `_setOptions` handed over the same reference
   (`omega-target/src/options.coffee:438`), the local in-memory profile gains `syncOptions: 'disabled'` without a
   local persist. **INFERRED** (aliasing read from the cited lines; not executed).

---

## 8. What the NEXORA model must not disturb

* **Stay out of `+`.** `packages/schema/envelope-keys.schema.json:20-26` reserves `+` for OmegaPac profiles and
  puts every NEXORA entity under `-`, one scalar key per entity;
  `packages/schema/scripts/verify.mjs` "namespace stays out of the profile prefix: ${ns.entity}" rejects any namespace whose `keyPattern` starts with `+`, citing
  `omega-pac/src/profiles.coffee:111-118` as the reason. A `+`-keyed NEXORA entity would appear in
  `availableProfiles`, quick-switch pickers, `validResultProfilesFor` and the generated PAC. ADR-0012. **VERIFIED**
* **Two version stamps will coexist.** NEXORA's `-nexora.schemaVersion`
  (`packages/schema/examples/envelope-keys.nexora.json:2`) cannot replace the legacy bare `schemaVersion`
  (`omega-target/src/options.coffee:329`), because `Options.upgrade` still rejects anything that is not `1` or `2`
  (`omega-target/src/options.coffee:349-353`). Both must be read by the migration runner. **VERIFIED**
* **Contract defect to fix in Phase 1.** The `legacyOwned` inventories
  (`packages/schema/envelope-keys.schema.json:78-81`,
  `packages/schema/examples/envelope-keys.nexora.json:76-79`) list `-schemaVersion`, `-proxySchema`, `-ruleLists`,
  `-tempRuleList` and `-optionsSync` — **none of which exists in the current tree** (§4; grep by absence). The
  verifier does not check `legacyOwned` against source, so the phantom keys pass silently. `+profile` is real but
  is an *example profile name* (`omega-target/src/default_options.coffee:14`), not a key namespace. This audit
  does not edit `packages/`; the correction belongs to the contract owner. **VERIFIED** (both readings)
* **`headers` is the SEC-1 field.** Subscription request headers are per-profile data
  (`omega-target/src/options.coffee:792-797`) that the sync strip-list omits
  (`omega-target/src/options.coffee:27`); the new contract closes it at the data layer
  (`packages/schema/envelope-keys.schema.json:105-116`, asserted by
  `packages/schema/scripts/verify.mjs` "syncExcludedValueKeys strips headers"). Severity and exploit path are owned by `docs/audit/11`. **VERIFIED**
* **Secrets have two resting places today**, and only one of them is the envelope: the gist/WebDAV token is a
  *state* key (`omega-target/src/options.coffee:1183-1188`), replicated into `chrome.storage.sync` by the
  built-in-sync blob (`omega-target/src/options_sync.coffee:236-243` with
  `omega-target-chromium-extension/src/coffee/background.coffee:259-271`). A keystore boundary must cover both or
  it covers neither. **VERIFIED**

---

## UNVERIFIED registry

| # | Claim | Why it could not be checked here |
|---|---|---|
| 1 | `_cache` is invisible to `JSON.stringify`/`for own` because `Object.defineProperty` leaves `enumerable: false` (`omega-pac/src/utils.coffee:36-38`) | JS-spec semantics read from the call shape; not exercised — running the Coffee tests is out of scope for this iteration |
| 2 | The four UI-createable profile types are exactly `FixedProfile`, `SwitchProfile`, `PacProfile`, `VirtualProfile` | radio buttons read from `omega-web/src/partials/new_profile.jade:24`, `:32`, `:40`, `:54`; no exhaustive search for other profile-creating dialogs (subscriptions and import paths create `RuleListProfile` separately) |
| 3 | Storing `"-builtinProfiles": {"+system": null}` throws when decorating colors | derived from the merge-then-index shape at `omega-pac/src/profiles.coffee:28-31`; not executed |
| 4 | `-quickSwitchProfiles` may legitimately contain `undefined` entries after a legacy migration | `omega-target-chromium-extension/src/module/upgrade.coffee:151-152` maps ids without a fallback, and `omega-target/src/options.coffee:532-538` filters falsy names — the combination is read, but the migrated data itself is not in this tree |
| 5 | No other module reads `chrome.storage.session` besides the temp-profile state | only `omega-target-chromium-extension/src/module/options.coffee:106-107`, `:140` were opened; a repo-wide `storage.session` sweep was not performed |
| 6 | The RPC dispatcher surface is exactly `Options`' public methods plus three specials | read from `omega-target-chromium-extension/src/coffee/background.coffee:458-481`; `ChromeOptions` inherits additional methods whose blast radius was not enumerated (owned by `docs/audit/11`) |
| 7 | Whether any real user data in the wild still carries `schemaVersion: 1` | requires a data corpus; `Options.upgrade`'s v1 branch (`omega-target/src/options.coffee:330-344`) was only read, never exercised |
| 8 | Behaviour of `Profiles.each` if an envelope key is not a string | cannot occur via JSON storage; the unguarded `key.charCodeAt(0)` at `omega-pac/src/profiles.coffee:113` was noted, not tested |
