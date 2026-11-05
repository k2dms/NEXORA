# 12 — What must not change (load-bearing behaviours)

> Purpose: an observational list of legacy behaviours in the ZeroOmega v3.5.2 tree that NEXORA must
> preserve. Each entry: the invariant, the code that proves it, what depends on it, how a NEXORA change
> could silently break it, and the Phase-2 machine check that will guard it (named only — the checks
> land later under `docs/verification/`). Not a to-do; a list of things the new architecture may not
> touch.

Affected version: **v3.5.2** (`omega-target-chromium-extension/overlay/manifest.json:4`). Every invariant is
**VERIFIED** against source unless a sub-claim is tagged otherwise. Machine-check names use the
project's existing vocabulary (grep assertions in `tools/`, JSON-Schema rules in `packages/schema/`,
`LIVE-nn` rows in the live matrix — PROJECT-STATE §6 #9).

---

## 1. The `+` / `-` envelope split, and the `charCodeAt(0) == '+'` profile filter

**Invariant.** In the options envelope a key whose first character is `+` is a *profile*; a key whose
first character is `-` is a *scalar setting*; nothing else is iterated as a profile. This split is
enforced by a one-character test, not by a type or a schema.

**Evidence.** `Profiles.each` iterates only `+` keys of the user options and only `+` keys of the
built-ins: `omega-pac/src/profiles.coffee:111-118` — the filter is `key.charCodeAt(0) == charCodePlus`
at `:113` (`charCodePlus` set at `:112`). The `+` is *added* by the canonical key builder
`nameAsKey`: `omega-pac/src/profiles.coffee:93-96` (`'+' + profileName` at `:96`). The generated PAC
resolution loop then relies on the same marker to know a value is still a profile reference rather
than a terminal proxy string: `omega-pac/src/pac_generator.coffee:130-138` (`right: '+'.charCodeAt(0)`
at `:138`).

**What depends on it.** `Options._setAvailableProfiles` (`omega-target/src/options.coffee:613`),
`_setOptions` change tracking (`omega-target/src/options.coffee:451`, `:456` `key[0] == '+'`),
`updateProfile` iteration (`omega-target/src/options.coffee:783` via `Profiles.each`),
`transformValueForSync` (`omega-target/src/options.coffee:92`), `allReferenceSet`,
`validResultProfilesFor`, quick-switch lists and the PAC build all flow through this filter.

**How a NEXORA change could break it.** Storing any NEXORA entity under a `+…` key would make it
appear in `availableProfiles`, the quick-switch list, the reference sets and the generated PAC as if
it were a switchable proxy profile — silent, and visible only as phantom entries in the UI. This is
why every NEXORA key is mandated to live under `-`: `packages/schema/envelope-keys.schema.json:20-22`
(`profilePrefix` const `+`, "NEXORA entities MUST NOT use it") and `:24-26` (`scalarPrefix` const `-`).
The description at `packages/schema/envelope-keys.schema.json:5` records that the constraint is
enforced by the legacy `charCodeAt` filter, "not by convention".

**Machine check (Phase 2).** Schema rule `keyPattern.startsWith('+') === false` over
`envelope-keys.schema.json` namespaces in `packages/schema` `verify.mjs`; grep assertion
`no-plus-namespace-in-nexora-keys`.

---

## 2. `proxyImpl` precedence and how a profile is turned into a proxy config

**Invariant.** Exactly one `proxyImpl` is chosen at startup, by a fixed candidate order — Firefox,
Listener, Script, Settings — and the first one whose `isSupported()` returns truth wins. Profile →
`chrome.proxy` config mapping is then done by type-specific branches inside that single impl.

**Evidence.** Candidate array and selection loop: `omega-target-chromium-extension/src/module/proxy/index.coffee:6-16`
(order at `:6-11`; `for Impl … if Impl.isSupported() return new Impl` at `:13-15`; hard failure
"Your browser does not support proxy settings!" at `:16`). Support predicates are distinct and
overlapping-sensitive: Firefox `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:10-16`,
Listener `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:8`,
Script `omega-target-chromium-extension/src/module/proxy/proxy_impl_script.coffee:6-7`,
Settings `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:8`. The chosen
impl is instantiated once and its capability set is published to state:
`omega-target-chromium-extension/src/coffee/background.coffee:244-245`. `Options` holds the impl as an
injected collaborator (`omega-target/src/options.coffee:101`) and delegates the actual apply:
`omega-target/src/options.coffee:699` and `:701` (`@proxyImpl.applyProfile(...)`). The Settings impl's
type dispatch (SystemProfile clears, DirectProfile → `direct`, PacProfile → `pac_script`,
FixedProfile → `fixed_servers`, everything else → a generated PAC) is
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:10-36`.

**What depends on it.** The `proxyImplFeatures` value in state (what the UI believes a profile can
express), the fail-closed primitive (ADR-0009 aims `chrome.proxy.settings` at a closed loopback port —
it reuses this same Settings impl), and `parseExternalProfile`
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:91-196`).

**How a NEXORA change could break it.** Reordering the candidate list, or adding an impl whose
`isSupported()` reports truthy too early, silently changes which code path every user gets and can drop
a capability (`watchProxyChange`, `pacScript`) downstream logic assumes. Mapping the NEXORA
`ConnectorProfile` to the wrong `mode` in the type dispatch changes proxy behaviour for real traffic.

**Machine check (Phase 2).** grep assertion `proxy-impl-order-immutable` pinning
`[Firefox, Listener, Script, Settings]`; `LIVE-xx` row asserting `chrome.proxy.settings.get`
round-trips the applied mode for each profileType (see PROJECT-STATE §6 #11).

---

## 3. `levelOfControl` handling — never fight an externally managed policy

**Invariant.** The extension reads `details.levelOfControl` on every proxy change and, when the proxy
is `not_controllable` (managed by enterprise policy) or owned by another extension, it backs off — it
records the state as "not controllable" and does **not** try to overwrite it.

**Evidence.** The switch is `omega-target-chromium-extension/src/coffee/background.coffee:346-356`:
`when "controlled_by_other_extensions", "not_controllable"` sets `reason = if …'policy'…'app'` and
`noRevert = true` (`:347-354`); the else branch clears the flag (`:355-356`). The self-change case
`controlled_by_this_extension` is short-circuited at `:358-360`. The flag is stored and badged, not
used to force a write: `omega-target-chromium-extension/src/module/options.coffee:161-168`
(`setProxyNotControllable` writes `proxyNotControllable` to state at `:164`, otherwise removes it at
`:167`). Note the guard `return if options.externalApi.disabled` at
`omega-target-chromium-extension/src/coffee/background.coffee:341`.

**What depends on it.** `setExternalProfile` (`omega-target/src/options.coffee:1118-1126`) and the
whole revert decision (item 4) read the not-controllable state; the popup/UI reads `proxyNotControllable`
to show "managed by policy".

**How a NEXORA change could break it.** If the fail-closed Connector path writes proxy settings
without first checking `levelOfControl`, it will fight an externally managed policy — which either
loses (Chromium keeps the policy) or, worse, repeatedly clobbers another extension. ADR-0009 already
forbids the silent-bypass failure; the corollary here is a silent *fight* with policy.

**Machine check (Phase 2).** grep assertion `reads-levelOfControl-before-set` on every
`chrome.proxy.settings.set` call site; `LIVE-xx` row under a forced enterprise policy asserting the
extension sets `not_controllable` and performs no `set`.

---

## 4. `-revertProxyChanges` — re-applying the profile after an external change

**Invariant.** When an external proxy change is detected and the user has
`-revertProxyChanges` on (default `true`), and the extension is not in system mode, it re-applies the
previous profile rather than adopting the external one, remembering the revert target.

**Evidence.** `omega-target/src/options.coffee:1118-1126`: gate `if @_options['-revertProxyChanges']
and not @_isSystem` (`:1119`); when the external profile differs from the current one and reverts are
allowed it calls `@applyProfile(@_revertToProfileName)` and clears the target (`:1121-1123`); when
`args.noRevert` it instead remembers `@_revertToProfileName ?= @_currentProfileName` (`:1124-1126`).
The default is set in `omega-target/src/default_options.coffee:7` (`"-revertProxyChanges": true`). The
legacy key name is preserved across upgrades from the older `preventProxyChanges` key:
`omega-target-chromium-extension/src/module/upgrade.coffee:13`.

**What depends on it.** `background.coffee` feeds `noRevert` from the `levelOfControl` result
(`omega-target-chromium-extension/src/coffee/background.coffee:345`, `:373-374`), so policy-owned
changes are *not* reverted (see item 3) while genuine external hijacks are.

**How a NEXORA change could break it.** Treating a Connector teardown or a fail-closed block as an
"external change" and reverting it would undo the deliberate block; conversely disabling revert would
silently let a foreign proxy stick. The distinction (revert only when `levelOfControl` says this
extension should own the setting) is the whole safety property.

**Machine check (Phase 2).** grep assertion `noRevert-set-from-levelOfControl`; `LIVE-xx` row
installing an external fixed proxy and asserting the profile is re-applied within the debounce window,
and a second row asserting a policy-owned change is NOT reverted.

---

## 5. Temporary rules live in `chrome.storage.session`; their lifetime is the browser session

**Invariant.** Quick temporary rules are held in an in-memory temp `SwitchProfile` and persisted only
to `chrome.storage.session` — so they survive an MV3 service-worker teardown within a browser session
but are gone after a browser restart, and never travel through sync.

**Evidence.** On init the temp profile is restored from session storage:
`omega-target-chromium-extension/src/module/options.coffee:106-130` (`chrome.storage.session.get(
TEMPPROFILEKEY)` at `:106-107`; per-rule re-indexing, `domain = condition.pattern.substring(2)` to
strip the leading `*.`, at `:114-122`; `@_tempProfileActive` restored at `:126`). On every add the
state is written back: `omega-target-chromium-extension/src/module/options.coffee:133-140`
(`addTempRule` → `chrome.storage.session.set(_zeroState)` at `:140`). The base-class rule shape —
a `HostWildcardCondition` of `'*.' + domain` tagged `isTempRule: true` — is
`omega-target/src/options.coffee:957-964`; lazy removal of a rule whose target profile has
disappeared happens on lookup: `omega-target/src/options.coffee:988-995` (`delete
@_tempProfileRules[domain]` at `:994`). The active temp profile is folded into the applied proxy at
`omega-target/src/options.coffee:676-701` (`@_tempProfileActive` branch).

**What depends on it.** `applyProfile` choosing the temp profile over the named one, `matchProfile`
(`omega-target/src/options.coffee:1090-1094`), and the popup's temp-rule badge via `getPageInfo`
(`omega-target-chromium-extension/src/module/options.coffee:367`).

**How a NEXORA change could break it.** (a) Persisting temp rules to `chrome.storage.local` (or sync)
would leak a per-session decision across restarts/devices. (b) The session-storage use sits on a latent
compatibility bug: `chrome.storage.session` needs Chrome ≥102 while the manifest pins
`minimum_chrome_version: "88"` (`omega-target-chromium-extension/overlay/manifest.json:29`) —
PROJECT-STATE §2.5 records this as a known Chrome 88–101 defect, which NEXORA must not repeat for
Connector session state. Expiry is session-scoped, not a TTL (**INFERRED** from the standard
`chrome.storage.session` lifetime, not from code in this tree).

**Machine check (Phase 2).** grep assertion `temp-rules-use-session-storage` and a companion
`session-state-min-chrome-version` (assert `minimum_chrome_version` ≥ 102 wherever
`chrome.storage.session` is used); `LIVE-xx` row: add temp rule → restart service worker (rule
persists) → restart browser (rule gone).

---

## 6. The `__ruleListOf_<name>` attachment convention

**Invariant.** A subscription (RuleList/PAC) profile's parsed rules are attached to a `SwitchProfile`
through a hidden companion profile whose *name* is `__ruleListOf_<parentName>` (stored under the key
`+__ruleListOf_<parentName>`). The prefix is a load-bearing protocol string shared between the UI and
the engine.

**Evidence.** The prefix and its two accessors: `omega-web/src/omega/app.coffee:13-20`
(`attachedPrefix = '__ruleListOf_'` at `:13`; `getAttachedName` at `:14-15`; `getParentName` at
`:16-20`). The companion key is built by feeding the attached name through `nameAsKey`, so it carries
the `+` like any profile: `omega-web/src/omega/controllers/switch_profile.coffee:282-283`
(`$scope.attachedKey = OmegaPac.Profiles.nameAsKey($scope.attachedName)`). The engine writes the same
name during upgrades: `omega-target-chromium-extension/src/module/upgrade.coffee:30`
(`name: '__ruleListOf_' + auto.name`). Because the name begins with `_`, UI filters hide it
(`omega-web/src/omega/app.coffee:24-26` `isProfileNameHidden`) and reserve the double-underscore form
(`:27-30`).

**What depends on it.** The default-profile wiring when a subscription is "in use"
(`omega-web/src/omega/controllers/switch_profile.coffee:307-316`), rename/replace reference rewriting
(`omega-web/src/omega/controllers/master.coffee:207-209` via `getAttachedName`), and the `isHidden`
detection in `background.coffee` (`omega-target-chromium-extension/src/coffee/background.coffee:120-122`).

**How a NEXORA change could break it.** Reusing `__` as a NEXORA namespace, or changing the prefix,
would orphan every existing attachment on upgrade and re-surface hidden companion profiles in pickers.
`packages/schema/envelope-keys.schema.json:89-91` (`attachmentKeyPattern` const `^__ruleListOf_`)
explicitly reserves it: "NEXORA MUST NOT reuse this prefix."

**Machine check (Phase 2).** Schema rule rejecting any NEXORA `keyPattern` matching `^__` except the
reserved `attachmentKeyPattern`; grep assertion `ruleListOf-prefix-unchanged`.

---

## 7. The PAC revision marker comment `/*OmegaProfile*<name>*<revision>*/`

**Invariant.** Generated PAC for a non-builtin profile is prefixed with a marker carrying the profile
name and its revision; the same marker is parsed back to recognise the extension's own proxy writes
as *internal* rather than as external hijacks.

**Evidence.** Written on the way out: `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:66-69`
(the `*`/`\` are neutralised at `:67-68`, and `prefix = "/*OmegaProfile*#{profileName}*#{meta.revision}*/"`
at `:69`, prepended at `:70`). Parsed back:
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:124-140` — the magic
literal `/*OmegaProfile*` at `:126`, token split on `*` at `:131-132`, JSON-decode of the name at
`:133-136`, and the decisive equality `OmegaPac.Revision.compare(profile.revision, revision) == 0` at
`:139`.

**What depends on it.** `parseExternalProfile` returning the *named* profile (not a synthetic
PacProfile) only when the revision matches; that in turn decides whether `setExternalProfile` treats
the change as internal (`omega-target/src/options.coffee:1129-1130`) versus external-and-revertible.
Revision source: `OmegaPac.Profiles.updateRevision` (`omega-pac/src/profiles.coffee:152-154`).

**How a NEXORA change could break it.** Changing the marker format, the separator, the escaping, or
the revision semantics means the extension no longer recognises its own PAC on `proxy.settings.onChange`
— every self-apply then looks like an external change, triggering spurious revert (item 4) and
"externally managed" badges. Conversely a change that *loosens* the revision match could let a stale
marker be mistaken for the current profile.

**Machine check (Phase 2).** grep assertion pinning the literal `/*OmegaProfile*` on both the write
(`proxy_impl.coffee`) and read (`proxy_impl_settings.coffee`) sides; `LIVE-xx` round-trip: apply a
profile, read `chrome.proxy.settings`, re-parse, assert the resolved profile name and revision equal
what was applied.

---

## 8. The `transformValueForSync` exclusion list

**Invariant.** Only four per-profile value keys are stripped before a profile is synced, and one whole
key (`-customCss`) is dropped; the list is allowlist-by-name and its correctness is a product behaviour
other logic relies on.

**Evidence.** The value list: `omega-target/src/options.coffee:27`
(`['lastUpdate', 'ruleList', 'pacScript', 'sha256']`). The transform that skips those for `+` profiles
and drops `-customCss`: `omega-target/src/options.coffee:89-99` (`-customCss` → `undefined` at `:90-91`;
`+`-key gate at `:92`; `continue if transformValueKeys.indexOf(k) >= 0` at `:96`). Applied to the push
queue at `omega-target/src/options_sync.coffee:103`, wired in
`omega-target-chromium-extension/src/coffee/background.coffee:242`.

**What depends on it.** Sync size/quota handling (dropping `ruleList`/`pacScript` keeps large bodies out
of the small sync quota) and the `QuotaExceededError` downgrade path
(`omega-target/src/options_sync.coffee:157-170`).

**How a NEXORA change could break it — and the one deliberate override.** Adding to or reordering the
list changes exactly which profile bytes replicate; removing one (e.g. `pacScript`) would blow the
quota and start disabling profiles. **This is the single "must not change" item that NEXORA
deliberately overrides for a security requirement: SEC-1 requires adding `headers` to the exclusion
list.** That is not a contradiction of this invariant but a controlled extension of it, and it is the
reason SEC-1 is deferred to a test-gated Phase-2 change rather than made ad hoc. The override is
already encoded as data — `packages/schema/envelope-keys.schema.json:97-116`
(`syncExcludedValueKeys` adds `headers` at `:110`; description at `:104` names SEC-1 and cites the
legacy list `omega-target/src/options.coffee:27`).

**Machine check (Phase 2).** Schema rule `headers IN syncExcludedValueKeys` in
`envelope-keys.schema.json` (checked by `packages/schema` `verify.mjs`); grep assertion that the four
legacy keys remain present (superset rule, not replacement).

---

## 9. `schemaVersion: 2` and the upgrade gate

**Invariant.** The engine accepts only `schemaVersion` 1 (upgraded) or 2 (current); anything else is
rejected, and options without a `schemaVersion` are treated as "not ours to load".

**Evidence.** `omega-target/src/options.coffee:327-353`: `version = options?['schemaVersion']`
(`:329`); the v1 upgrade branch rewrites and bumps to 2 (`:330`, `version = changes['schemaVersion'] =
options['schemaVersion'] = 2` at `:344`); `if version == 2` resolves (`:349-351`); else hard reject
`"Invalid schemaVersion #{version}!"` (`:353`). The current value is `schemaVersion: 2` in
`omega-target/src/default_options.coffee:2`. The Chromium subclass tightens it: a null-version load is
turned into `NoOptionsError` rather than upgraded
(`omega-target-chromium-extension/src/module/options.coffee:325-328`). Sync uses the same marker to
decide pristine-vs-sync: `omega-target/src/options.coffee:186-187` (`@sync.storage.get('schemaVersion')`,
`pristine` when absent).

**What depends on it.** `loadOptions` fallback selection and the wipe-trap version comparison
(`omega-target-chromium-extension/src/coffee/background.coffee:41-47`) both key off a sane version
story; the schemaVersion is the contract that lets an old ZeroOmega envelope open in NEXORA at all
(ADR-0013/0014/0015 backward compatibility).

**How a NEXORA change could break it.** Bumping `schemaVersion` past 2 without a matching branch in
`upgrade` makes every existing user's options fail to load (`Invalid schemaVersion`). Conversely, a
NEXORA-only field guarded by a *new* scalar must not reuse this legacy `schemaVersion`
(`packages/schema/envelope-keys.schema.json:16-18` puts the NEXORA version on a separate key,
`-nexora.schemaVersion`, precisely to leave `schemaVersion: 2` untouched).

**Machine check (Phase 2).** grep assertion `legacy-schemaVersion-const-2`; `LIVE-14`'s import of a genuine
3.5.2 `.bak` (the shipped `docs/verification/backup-from-3.5.2.json` is exporter-shaped, so the real file is
still owed) asserting a clean load with `schemaVersion`
still 2 and zero data loss.

---

## 10. MV3 cold-start RPC retry/backoff — port it verbatim or reintroduce the popup hang (#276)

**Invariant.** The UI-to-background RPC must survive the MV3 service-worker cold start with the exact
retry/backoff schedule: five quick retries at `100 * attempt` ms, then — only on "Receiving end does
not exist" — nine more at `2000 * 2^(attempt-6)` ms (≈15 s total) before giving up.

**Evidence.** `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:50-77`:
`maxAttempts = 5` (`:50`), `maxWorkerDownAttempts = 9` (`:51`); error test
`err.message?.indexOf('Receiving end does not exist') >= 0` (`:59-60`); cap
`max = if workerDown then maxWorkerDownAttempts else maxAttempts` (`:61`); delay
`100 * attempt`, or `2000 * Math.pow(2, attempt - maxAttempts - 1)` when `workerDown and attempt >
maxAttempts` (`:63-66`, i.e. `attempt-6`); re-schedule `setTimeout((-> send(attempt + 1)), delay)`
(`:67`); reject past the cap (`:68-69`); first call `send(1)` (`:77`). The rationale is the file's own
comment block (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:37-49`) and
PROJECT-STATE §2.8.

**What depends on it.** Every `callBackground(...)`-based UI action (state read, `refresh`,
`applyProfile`, `updateProfile`, sync toggles) is built on this helper
(`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:99-213`).

**How a NEXORA change could break it.** The Svelte/TS UI client (ADR-0016) will reimplement this RPC
layer. Simplifying the schedule — a fixed retry count, a uniform delay, or dropping the
`workerDown`-specific exponential branch — reintroduces exactly the popup hang the fix removed: the
popup renders only the toolbar icon while the worker refuses to wake for ~15 s. The mandate is to port
the constants and both formulas verbatim.

**Machine check (Phase 2).** grep/AST assertion `coldstart-backoff-ladder-verbatim` pinning
`maxAttempts=5`, `maxWorkerDownAttempts=9`, `100*attempt`, and `2000*2^(attempt-6)` in the new client;
`LIVE-xx` row killing the service worker and opening the popup, asserting it resolves within the ≈15 s
ladder and never hangs (issue #276 regression guard).

---

## UNVERIFIED registry

- **Temp-rule expiry (item 5).** Restored from `chrome.storage.session` on `ready` is VERIFIED
  (`omega-target-chromium-extension/src/module/options.coffee:106-140`); that the platform clears that
  store at session end (the expiry boundary) is **INFERRED** from documented Chrome behaviour, not
  asserted by any line here.
- **Machine-check names.** Every `grep assertion`/`LIVE-xx`/schema-rule name is a *planned* identifier
  landing in Phase 2 under `docs/verification/`; none exists on disk. Forward references, not claims
  about current code. **UNVERIFIED** by nature.
- **Firefox/Listener/Script `isSupported()` overlap.** Settings predicate
  (`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:8`) and order
  (`omega-target-chromium-extension/src/module/proxy/index.coffee:6-16`) are VERIFIED; whether any real
  shipped browser satisfies two impls at once is **UNVERIFIED** (Firefox test read at
  `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:10-16`).
- **Wipe-trap interaction (item 9).** Cited (`omega-target-chromium-extension/src/coffee/background.coffee:41-47`)
  and settled in PROJECT-STATE §2.1, but its exact trigger under a `3.6.x → 4.x` re-version was not
  executed here — **UNVERIFIED**, tracked by ADR-0015.
