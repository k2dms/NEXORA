# 06 — Proxy application: how a profile becomes a browser proxy configuration

> Purpose: trace, layer by layer and with line evidence, the path from "a user picks a profile" to
> "the browser has a proxy configuration", so NEXORA's loopback/Xray model can say precisely which
> layers it inherits and which it replaces. Audit of ZeroOmega v3.5.2 as it sits in this tree;
> normative text lives in `docs/architecture`.

Tags: **VERIFIED** = read at the cited line; **INFERRED** = derived from cited code plus platform
behaviour not observable here; **UNVERIFIED** = listed in the registry at the end.

## 1. The funnel: `Options#applyProfile`

The method is `applyProfile` — not `setProfile` or `useProfile` — in the base class
`omega-target/src/options.coffee:654`. It is the *only* place a profile reaches the browser: it resolves
the name via `OmegaPac.Profiles.byName` `omega-target/src/options.coffee:656`, rejects
`ProfileNotExistError` if absent `omega-target/src/options.coffee:657-658`, and records
`@_currentProfileName`, `@_isSystem`, `@_watchingProfiles` `omega-target/src/options.coffee:660-663`. **VERIFIED**

| Fact | Evidence | Consequence for NEXORA |
|---|---|---|
| `options.proxy == false` returns before touching the browser | `omega-target/src/options.coffee:674-675` | profile state can change without re-applying (used by the external path) |
| system mode is a flag as well as a profile type | `omega-target/src/options.coffee:661` | failure policy must model "system" as its own plane |
| after applying, referenced profiles are scheduled for update | `omega-target/src/options.coffee:705-712` | applying a profile triggers subscription fetches today |
| startup chain: `-startupProfileName` → `currentProfileName` → `fallbackProfileName`, default `'system'` | `omega-target/src/options.coffee:264-291`, `omega-target/src/options.coffee:69` | ADR-0009 must displace this `system` fallback |
| any watched profile edit re-applies the current profile | `omega-target/src/options.coffee:467-473` | already the replace-and-reconcile shape `config.apply` claims |

## 2. The temp-SwitchProfile wrapper

Declared `omega-target/src/options.coffee:67-68`, created lazily in `addTempRule`
`omega-target/src/options.coffee:930-934` as an anonymous `SwitchProfile` whose `defaultProfileName` is
the *currently applied* profile. Every temp rule is
`{condition:{conditionType:'HostWildcardCondition', pattern:'*.'+domain}, profileName, isTempRule:true}`
`omega-target/src/options.coffee:957-964`; a change bumps the revision and re-applies
`omega-target/src/options.coffee:976-978`.

The wrapper takes over at `omega-target/src/options.coffee:676-699`: it is armed when the profile
`isIncludable` `omega-target/src/options.coffee:677-678`, prunes rules of deleted profiles
`omega-target/src/options.coffee:684-694`, recomputes the watch set from the *temp* profile
`omega-target/src/options.coffee:696-697`, and hands the implementation the temp profile as operative
with the user's profile as metadata only `omega-target/src/options.coffee:699`; the plain branch is
`omega-target/src/options.coffee:701`. **VERIFIED**

Load-bearing for ADR-0013: **the object given to the proxy layer is not the object the user selected**.
A connector compiler that reads "the current profile" without the temp profile's rules silently loses
per-domain temp rules; `isCurrentProfileStatic` already treats it as non-static
`omega-target/src/options.coffee:764-766`.

## 3. Which `proxyImpl` is selected, and how the platform is detected

The seam is the fifth constructor argument `@proxyImpl` `omega-target/src/options.coffee:101`. There is
no `Target`/`TargetCfg` abstraction in this fork. The instance is built once in the service worker
`omega-target-chromium-extension/src/coffee/background.coffee:244-247` and its feature string is
published to state `omega-target-chromium-extension/src/coffee/background.coffee:245`.

Selection is a first-match loop over a fixed priority list
`omega-target-chromium-extension/src/module/proxy/index.coffee:6-10` — order
`[Firefox, Listener, Script, Settings]` — instantiating the first `Impl.isSupported()`
`omega-target-chromium-extension/src/module/proxy/index.coffee:12-16`, else throwing
`'Your browser does not support proxy settings!'` `omega-target-chromium-extension/src/module/proxy/index.coffee:16`.
Detection is pure capability probing of globals, no UA sniffing:

| Impl | `isSupported` test | Cite | Wins when |
|---|---|---|---|
| `FirefoxProxyImpl` | `!!chrome.contextMenus and Promise? and browser?.proxy?.onRequest? and browser?.proxy?.settings` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:10-16` | Firefox, where both proxy APIs exist |
| `ListenerProxyImpl` | `Promise? and browser?.proxy?.onRequest?` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:8` | `proxy.onRequest` without `proxy.settings` |
| `ScriptProxyImpl` | `browser?.proxy?.register? or browser?.proxy?.registerProxyScript?` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_script.coffee:6-7` | legacy Firefox proxy-script API |
| `SettingsProxyImpl` | `chrome?.proxy?.settings?` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:8` | Chromium — the normal NEXORA target |

Feature strings diverge and the UI reads them: `['fullUrlHttp','pacScript','watchProxyChange']`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:9` vs
`['fullUrl','socks5Auth']` `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:9`.
Which impl wins on a given shipping browser is **INFERRED** (no browser here).

Decay: `ScriptProxyImpl.applyProfile` opens by logging `'Your browser is outdated!'`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_script.coffee:15-17`, and the SwitchySharp
bridge is switched off by a literal `and false`
`omega-target-chromium-extension/src/coffee/background.coffee:256-258` (its ID constant still ships,
`omega-target-chromium-extension/src/module/switchysharp.coffee:7`). Dead branches are inherited surface
area.

## 4. Mode mapping in `SettingsProxyImpl`

`applyProfile` `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:10`:

| Profile type | `chrome.proxy` output | Cite |
|---|---|---|
| `SystemProfile` | no mode at all: `chrome.proxy.settings.clear({})`, then a manual `get` to re-sync the watcher | `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:12-16` |
| `DirectProfile` | `mode: 'direct'` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:18-19` |
| `PacProfile` | `mode: 'pac_script'`; `pacScript.url` when the script is empty or a `file:` URL, else inline `pacScript.data` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:20-29` |
| `FixedProfile` | `mode: 'fixed_servers'` from `_fixedProfileConfig` | `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:30-31` |
| everything else (`SwitchProfile`, `RuleListProfile`, `VirtualProfile`) | `mode: 'pac_script'`, `data:` = generated PAC | `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:32-36` |

`mandatory: true` is set on both PAC branches `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:24-29`:
a PAC that cannot be fetched is *intended* to break connectivity rather than fall back to direct — the
only pre-existing fail-closed gesture in this code. **VERIFIED** for the flag, **INFERRED** for the
consequence. Nothing ever writes `mode: 'system'`; system means `clear()`. External parsing still
recognises a `system` value `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:94-95`
and invents `http://wpad/wpad.dat` for `auto_detect`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:99-103`.

`_fixedProfileConfig` copies `proxyForHttp`/`proxyForHttps`/`proxyForFtp`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:47-51` and degrades to
`mode: 'direct'` when neither a fallback nor any protocol proxy exists
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:65-66`. `bypassList`
entries are serialised by stripping the condition-type prefix
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:74-77` using
`OmegaPac.Conditions.str` — so on this path **Chromium's own bypass matcher evaluates them**, not
Omega's.

## 5. The `singleProxy` workaround for HTTP `fallbackProxy`

`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:53-64`. Verbatim comment:
`# Chromium does not allow HTTP proxies in 'fallbackProxy'.` `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:55`.
With no per-protocol proxy configured the HTTP fallback is emitted as `rules['singleProxy']`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:56-58`; otherwise the same
proxy object is *replicated* into all three protocol slots
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:60-62`; a non-HTTP fallback
goes straight to `rules['fallbackProxy']`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:63-64`.

The limitation is Chrome's `fixed_servers` intake rejecting `scheme: "http"` in the `fallbackProxy`
slot (**INFERRED** — asserted by the comment above, upstream IDL unreachable). Both re-expressions change
semantics: `singleProxy` also captures protocols the user never configured, and replication turns a
*fallback* into a *mandatory* per-protocol entry. The reverse mapping admits the fudge by folding
`singleProxy` back into `fallbackProxy` when reconstructing a profile
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:150-155` and
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:188-192`, and again when
comparing candidate profiles `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:172-175`.

## 6. External changes: `levelOfControl`, the 500 ms debounce, `parseExternalProfile`

`watchProxyChange` subscribes to `chrome.proxy.settings.onChange`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:83-90`; the listener impls
return `null` instead `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:21`.

- `levelOfControl` `omega-target-chromium-extension/src/coffee/background.coffee:346-356`:
  `controlled_by_other_extensions`/`not_controllable` become reasons `'app'`/`'policy'` and set
  `noRevert` `omega-target-chromium-extension/src/coffee/background.coffee:347-354`; anything else clears
  the state. `setProxyNotControllable` only badges and persists
  `omega-target-chromium-extension/src/module/options.coffee:161-168` — it does not stop `applyProfile`.
  Regained control is flagged `internal` and short-circuited
  `omega-target-chromium-extension/src/coffee/background.coffee:358-360`.
- The 500 ms debounce has a written rationale: Chromium fires `onChange` at unload, after this extension
  already lost control, which would corrupt `currentProfileName`
  `omega-target-chromium-extension/src/coffee/background.coffee:363-368`. Mechanically
  `clearTimeout` + `setTimeout(…, 500)` `omega-target-chromium-extension/src/coffee/background.coffee:369-375`,
  with the parse issued *after* the timer is armed
  `omega-target-chromium-extension/src/coffee/background.coffee:377`, so the closure applies whatever the
  latest parse produced and a burst costs one apply.
- `parseExternalProfile` is a `null` stub on the base class
  `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:15` and is implemented only by the
  Settings impl `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:91-98`:
  re-identify by PAC URL, then by exact PAC text, then by the marker comment (§7), then by fixed-server
  rules; otherwise synthesise an anonymous profile
  `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:141-144`. Bypass
  reconstruction normalises `<local>` against `OmegaPac.Conditions.localHosts`
  `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:162-165`,
  `omega-pac/src/conditions.coffee:198`. The result goes to `Options#setExternalProfile`
  `omega-target/src/options.coffee:1118-1145`, whose `-revertProxyChanges` branch
  `omega-target/src/options.coffee:1119-1126` is the pre-existing "someone else moved my proxy" defence.

## 7. The PAC marker comment `/*OmegaProfile*<name>*<revision>*/`

Generated in `ProxyImpl#getProfilePacScript`
`omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:36-74`: AST from
`OmegaPac.PacGenerator.script` `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:62-63`,
compressed `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:64`, printed and
ASCII-escaped `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:65`; the name is
JSON-stringified with `*`→`\u002a` and `\`→`\u002f` so it cannot terminate the comment
`omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:66-68`; prefix built at
`omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:69`. The cache key folds each
referenced profile's `revision`, or its `sha256` for updated profiles, into a string
`omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:52-59`, and the module cache carries
an open TODO about temp profiles `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:6-7`.

Parsed in `parseExternalProfile`: `magic = '/*OmegaProfile*'`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:126`, prefix test and `*/`
search `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:127-128`, `split('*')`
into `[profileName, revision]` `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:130-132`,
guarded `JSON.parse` `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:133-136`,
and comparison `OmegaPac.Revision.compare(profile.revision, revision) == 0`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:137-139` — revisions are hex
timestamps `omega-pac/src/utils.coffee:2-4` ordered by length then lexicographically
`omega-pac/src/utils.coffee:5-13`, so equality is the only reliable relation. **VERIFIED**

Two honest defects here: `profile` at
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:138` is not null-checked
before `profile.revision` on the next line — a marker naming a deleted profile dereferences `undefined`
(missing check **VERIFIED**, resulting `TypeError` **INFERRED**) — and a marker that compares unequal
falls through silently to an anonymous PAC profile
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:141-144`.

## 8. `ProxyAuth` on `webRequest.onAuthRequired` with `['blocking']`

`ProxyAuth#listen` registers the handler with the third argument literally `['blocking']`
`omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:19-22`, behind capability guards that
log `'Proxy auth disabled!'` when `chrome.webRequest` or `…onAuthRequired` is missing
`omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:13-18`, plus `onCompleted`/
`onErrorOccurred` cleaners `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:24-30`.
Candidates are keyed `host:port` `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:34`
from every referenced profile's `auth` map `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:38-44`;
the handler replays them per `requestId` using an `authTries` counter, then falls through to the
`auth.all` fallbacks `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:63-84`. `listen()`
is reached from `setProxyAuth` `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:23-31`,
which each impl awaits while applying
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:37`,
`omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:27`. **VERIFIED**

MV3 availability, stated honestly: the Chromium manifest requests `webRequestAuthProvider`
`omega-target-chromium-extension/overlay/manifest.json:45-47` and pins
`minimum_chrome_version: "88"` `omega-target-chromium-extension/overlay/manifest.json:29`; the Firefox
manifest instead requests `webRequestBlocking`
`omega-target-chromium-extension/overlay/manifest-firefox.json:44-46` with
`strict_min_version: "112.0"` `omega-target-chromium-extension/overlay/manifest-firefox.json:52`. So the
registration is *not* dead code — in MV3 `['blocking']` on `onAuthRequired` is honoured for extensions
holding `webRequestAuthProvider` — but that Chrome special-case post-dates the declared minimum version,
and the guards at `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:16-18` test only for
the *presence* of the API, never for blocking capability. **INFERRED** (registry §12.1). Design
consequence: NEXORA must not build HTTP proxy Basic auth on a `blocking` webRequest hook; it moves to the
connector/keystore (ADR-0010/0011).

## 9. `proxyDNS`

Read from the profile and passed into `proxyInfo`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:45-48`, defaulted to `true`
in the signature `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:60`, and
applied only for `socks` `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:75-77`
with an explicit TODO about SOCKS4 `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:79-81`.
The Firefox impl mirrors it `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:84`
and additionally sets the top-level `proxyDNS` in `browser.proxy.settings.set`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:49-53`. The legacy registered
script repeats it `omega-target-chromium-extension/src/js/omega_webext_proxy_script.js:51-52`, and the UI
binds the checkbox to the profile `omega-web/src/omega/controllers/fixed_profile.coffee:100-104`,
`omega-web/src/partials/profile_fixed.jade:43-44`. **VERIFIED**

Correction to the brief: `proxyDNS` is not *Firefox*-only — it exists on both `proxy.onRequest` impls —
but it is genuinely absent from `SettingsProxyImpl` and from the PAC path, whose only DNS signal is the
`SOCKS5` keyword `omega-pac/src/profiles.coffee:53-57`, `omega-pac/src/profiles.coffee:82-89`.
Consequence on Chromium: a `FixedProfile` with `proxyDNS: false` has no consumer for the flag and behaves
as if DNS were remote, and there is no way to ask the PAC engine for local resolution (**INFERRED**). In
the loopback model DNS disposition belongs to the Core (`domainStrategy`, `sniffing`), not the browser —
see `packages/schema/routing-policy.schema.json:72-79`.

## 10. `ExternalApi` allowlist keyed on extension ID

`knownExts` is a hard-coded object literal with exactly one entry, ID `padekgcemlokbadohgkifijomclgjgif`
at level `32` `omega-target-chromium-extension/src/module/external_api.coffee:9-10`. Not configurable: no
storage key, manifest field or UI path writes it. Transport is `chrome.runtime.onConnectExternal`
`omega-target-chromium-extension/src/module/external_api.coffee:12-17`, wired at
`omega-target-chromium-extension/src/coffee/background.coffee:253-254`. Authorisation is a numeric level
compare against `port.sender.id` `omega-target-chromium-extension/src/module/external_api.coffee:32-38`:
`disable` needs 16 `omega-target-chromium-extension/src/module/external_api.coffee:43-44`, `getOptions`
needs 8 `omega-target-chromium-extension/src/module/external_api.coffee:59-61`, and level 32 only changes
the reported reason from `disabled` to `upgrade`
`omega-target-chromium-extension/src/module/external_api.coffee:50-52`. `enable` is unguarded
`omega-target-chromium-extension/src/module/external_api.coffee:56-58` — any external extension can
re-enable. The disabled state also suppresses external-proxy processing
`omega-target-chromium-extension/src/coffee/background.coffee:340-341` and badge clearing
`omega-target-chromium-extension/src/module/options.coffee:188`. **VERIFIED**

This is the closest thing in the tree to a trust boundary, and it is weak: an ID list in source, an
unauthenticated `enable`, and a level-8 `getOptions` that returns `@options.getAll()` — the whole
envelope, including subscription `headers` (SEC-1, PROJECT-STATE §2.2).

## 11. What NEXORA's loopback/Xray model inherits and what it replaces

Observations only; no normative claim.

**Inherits.** (1) The single funnel `omega-target/src/options.coffee:654` as the one writer of browser
proxy state, whose replace-and-reconcile shape `packages/schema/protocol/commands/config.apply.json:4-8`
already mirrors. (2) The injected-`proxyImpl` seam `omega-target/src/options.coffee:101` and first-match
capability selection `omega-target-chromium-extension/src/module/proxy/index.coffee:12-16`: a
connector-backed impl is an additional list entry, not a refactor. (3) The temp-profile hand-off
semantics `omega-target/src/options.coffee:676-701` — a ConnectorProfile compiled to a loopback
`FixedProfile` is still just another object passed down that path
(`packages/schema/connector-profile.schema.json:5`). (4) The `SwitchProfile` rule shape plus
`defaultProfileName`, which `packages/schema/routing-policy.schema.json:5` promises to preserve verbatim,
and the `+`-key envelope `omega-pac/src/profiles.coffee:111-118`. (5) `chrome.proxy.settings` as the
fail-closed actuator: today's only hard gestures are `mandatory: true`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:24-29` and `clear()` for
system `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:14`; ADR-0009's
closed-loopback `FixedProfile` is the same actuator with opposite intent.

**Replaces.** (1) The PAC *evaluation site* — on Chromium the decision is made by the browser's PAC
engine over a generated script `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:32-36`;
in NEXORA the browser decides only *which profile* (see `docs/audit/07`). (2) `ProxyAuth`'s `blocking`
hook (§8) → connector-supplied credentials from the keystore. (3) Bypass semantics delegated to
Chromium's matcher (§4) → compiled Core rules, except that `<local>`'s "127.0.0.1, ::1 or any host
without dots" rule `omega-pac/src/conditions.coffee:370-380` has no Core analogue and must stay
browser-side. (4) `proxyDNS` (§9) → Core DNS policy. (5) The `singleProxy`/`fallbackProxy` gymnastics (§5)
→ moot once the browser only ever receives one loopback destination. (6) Subscription fetch — a bare
`fetch()` carrying caller-supplied headers `omega-target-chromium-extension/src/module/fetch_url.coffee:6-10`
plus a `?_=<ts>` cache-buster `omega-target-chromium-extension/src/module/fetch_url.coffee:36-40` — and
the RuleList→PAC pre-compile `omega-target/src/options.coffee:29-45`,
`omega-target/src/options.coffee:462-463` → connector `subscription.refresh` with routing compiled to
Core form. (7) The hard-coded ID allowlist (§10) → Native Messaging sender identity (ADR-0004/0005).

## UNVERIFIED registry

1. Runtime behaviour of `['blocking']` on `chrome.webRequest.onAuthRequired` under MV3 per Chrome version:
   only the registration site and the two manifests are citable here.
2. Chrome's rule that HTTP is not allowed in `fallbackProxy` — asserted only by the code comment
   `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:55`; upstream IDL
   unreachable this session.
3. Which impl actually wins on each shipping browser — probes read, never executed.
4. Firefox precedence between the `proxy.settings` autoconfig blob
   `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:44-53` and the competing
   `proxy.onRequest` listener registered in the same constructor
   `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:24-28`: double evaluation is
   visible, which one Firefox consults is not answerable from this tree.
5. Whether the incognito gate `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:33`
   skipping `browser.proxy.settings.set` while still resolving
   `omega-target-chromium-extension/src/module/proxy/proxy_impl_firefox.coffee:56-60` is a user-visible
   failure.
6. The `TypeError` from the unguarded `profile.revision` at
   `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:137-139`.
7. Whether 500 ms is actually sufficient to absorb Chromium's unload event.
8. Xray-side equivalences referenced in §9 and §11 (`domainStrategy`, `sniffing`) — treated in
   `docs/audit/07` §7, where the upstream docs also could not be fetched.
