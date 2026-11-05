# 11 — Security findings (Phase-0 audit)

> Purpose: an evidence-backed register of the security-relevant behaviours that exist in the
> ZeroOmega v3.5.2 tree on this branch, each with a reproduction path, a pre-condition/attacker
> position, and the NEXORA contract or planned document that will own the mitigation. This is an
> audit of what the code already does, not a threat model of the unbuilt Connector. No product code
> was changed to produce it.

**Severity scale used here (deliberately not CVSS, deliberately not inflated).** This is a
user-configuration extension driven by URLs and profiles the *user* supplies; a bug the user triggers
against their own machine is not a remote code-execution hole, so the scale is bounded by
trust-boundary crossings, not worst-case imagination. **High** — a secret or the proxy decision leaves
the browser to an attacker who is not already the user, or (post-Connector) lets an untrusted byte be
executed. **Medium** — a secret/config crosses a boundary the user did not intend to open (a third
party's sync endpoint, another installed extension, an exported file). **Low** — impact confined to
the user's own machine, requiring the user to point a URL at it; a guard is worth adding but nothing
is exposed to a third party today. **Informational** — intended behaviour that becomes load-bearing
risk only once the Connector makes local execution possible.

Affected versions for every finding: **v3.5.2** (`omega-target-chromium-extension/overlay/manifest.json:4`), i.e. the
tree in this repository. Existing schema files are cited as real paths; where only a planned document
exists it is named, not cited.

---

## SEC-1 — Subscription `Authorization` header is copied verbatim into the synced envelope

**Severity: Medium** (a user secret crosses to a third-party sync endpoint and to every device on
that endpoint, without the user opting into that specific exposure).

**VERIFIED.** The value-transform list that decides what fields of a profile are stripped before a
profile is pushed to sync storage is
`omega-target/src/options.coffee:27` —
`transformValueKeys = ['lastUpdate', 'ruleList', 'pacScript', 'sha256']`. `headers` is not in it.

The chain:

1. **Where headers enter.** `headers` is a first-class profile field edited by the user in the
   subscription/PAC-profile UI: `omega-web/src/omega/controllers/pac_profile.coffee:53` and the
   name/value table `omega-web/src/partials/profile_pac.jade:27`. It is read back verbatim when the
   profile is fetched: `omega-target/src/options.coffee:793` (`if profile.headers`) → `:794`
   (`for header in profile.headers`) → `:797` passes the header dict into `@fetchUrl(url, headers, …)`.
2. **Where the profile is stored with headers.** After a successful fetch the profile object (still
   carrying `headers`) is written under its `+` key: `omega-target/src/options.coffee:810`
   (`changes[key] = profile`) → `:811` (`@_setOptions(changes)`).
3. **Where the value transform applies — and fails to strip.** `Options.transformValueForSync` runs
   only for `+` keys of profiles that have an update URL: `omega-target/src/options.coffee:92`
   (`if key[0] == '+'`), `:93` (`if OmegaPac.Profiles.updateUrl(value)`). It copies *every* field of
   the profile except those in the four-key list: `omega-target/src/options.coffee:96`
   (`continue if transformValueKeys.indexOf(k) >= 0`). Because `headers` is not in that list, an
   `Authorization` header survives into the returned value.
4. **Where that value goes.** `OptionsSync.requestPush` calls `@transformValue(value, key)` on each
   changed value before queuing it: `omega-target/src/options_sync.coffee:103`. That function is
   wired to the transform above at `omega-target-chromium-extension/src/coffee/background.coffee:242`
   (`sync.transformValue = OmegaTargetCurrent.Options.transformValueForSync`), and the resulting
   `_pending` map is written to the remote storage in `_doPush`
   (`omega-target/src/options_sync.coffee:120-125`). For a Gist backend the body is serialised whole
   to `api.github.com`: `omega-target-chromium-extension/src/module/sync/sync_impl_gist.coffee:53-54`.
5. **The exclusion mechanism exists and was simply never extended.** A whole-value exclusion is
   already implemented for one setting — `-customCss` returns `undefined` so it is dropped from sync
   entirely: `omega-target/src/options.coffee:90-91`. That proves the guard is allowlist-per-key on
   the value side (`transformValueKeys`) rather than a denylist of secret-bearing fields; nobody added
   `headers` to it.

**Reproduce (description only).** Create a subscription profile with an `Authorization` header; enable
Gist or WebDAV sync; force an update and let the debounced push run
(`omega-target/src/options_sync.coffee:99-107`); read the synced Gist file — the header value is
present next to `sourceUrl` while the four stripped keys are gone. **Preconditions:** user must use a
header-bearing subscription *and* have sync enabled; anyone with read access to that endpoint or a
stolen sync session then obtains the secret.

**Status today: NOT fixed in this iteration — stated plainly.** Changing `transformValueForSync`
narrows what is synced and therefore changes observable product behaviour for existing users; doing
it without a test is exactly the kind of silent behaviour change Phase 0 forbids. It is a **Phase-2
requirement gated on a test**, per PROJECT-STATE §2.2 and §9.1.

**Mitigation NEXORA already specifies.** Secret material is structurally forbidden in the envelope:
every secret field is a `keystore://…` reference, never a value, and the sync-exclusion list is
promoted from an implicit code list to data. See `packages/schema/envelope-keys.schema.json:97-117`
(`syncExcludedValueKeys`, which adds `headers` at `:110`), and for subscriptions the inline-header
path is replaced by a reference: `packages/schema/subscription-source.schema.json:34-37`
(`headersRef`, whose description names this exact leak). Keystore + masking are the durable control
(`docs/architecture/32-secrets-keystore-masking.md`).

---

## SEC-2 — Subscription fetch has no SSRF or body-size guard; RuleList accepts any Content-Type

**Severity: Low today** (the URL is user-supplied and the request runs in the user's own browser, so
the "target" is the user's machine; the exposure is self-inflicted, not third-party). It rises once
the Connector executes what a subscription delivers — a fetched body is no longer inert text.

**VERIFIED.** `fetch_url.coffee` is a bare `fetch()`: the whole request is `fetch(url, options)` at
`omega-target-chromium-extension/src/module/fetch_url.coffee:10`, with the body read in full by
`response.text()` at `:11` — no cap on the number of bytes, so a very large body is buffered without
limit. There is no scheme/host allowlist and no rejection of loopback, link-local or
cloud-metadata addresses anywhere on this path; the only URL munging is the cache-buster
`omega-target-chromium-extension/src/module/fetch_url.coffee:36-46` (adds `?_=<timestamp>`). The URL
reaching it is whatever the profile stores — `OmegaPac.Profiles.updateUrl(profile)` resolves to
`profile.sourceUrl` for a RuleList
(`omega-pac/src/profiles.coffee:492`) or `profile.pacUrl` for a PAC profile
(`omega-pac/src/profiles.coffee:378-382`), and `updateProfile` calls fetch with no validation
(`omega-target/src/options.coffee:789-791`). The extension holds `<all_urls>` host permission
(`omega-target-chromium-extension/overlay/manifest.json:49`), so the request is not origin-restricted.
On top of that, the RuleListProfile Content-Type accept list ends in a catch-all `*`
(`omega-pac/src/profiles.coffee:493-498`), handled by a hint that unconditionally returns the body
(`omega-target-chromium-extension/src/module/fetch_url.coffee:55-58`), so any MIME type is accepted.

**Reproduce (description only).** Point a subscription's source URL at `http://127.0.0.1:<port>/`
(probing a local service) or a provider metadata IP such as `http://169.254.169.254/`, trigger update,
and observe the response treated as a rule list; separately point a URL at a multi-hundred-MB endpoint
and observe the full body buffered. No PoC is provided. **Preconditions:** requires the user to add (or
be socially engineered into adding) a malicious URL; from inside the browser this is "the user reaches
their own loopback" — its NEXORA significance is that loopback is exactly where the Connector's
inbounds live, so an unvalidated fetch and an unvalidated config must not be conflated.

**Mitigation NEXORA already specifies.** Subscription sources are constrained declaratively and the
fetch limits become data, not absent: `packages/schema/subscription-source.schema.json:28-33`
(`url` `^https://…`, no userinfo) and `:50-85` (`limits.maxBytes ≤ 8 MiB`, `maxRedirects`,
`timeoutMs`; the description at `:84` states the legacy loader "has none"). SSRF loopback/metadata
rejection is explicitly assigned to the Connector, not the schema
(`packages/schema/subscription-source.schema.json:32`); the durable spec is
`docs/architecture/29-subscriptions-and-servers.md`, and config introspection goes through the typed
`packages/schema/protocol/commands/config.snapshot.json` rather than a raw fetch.

---

## SEC-3 — Broad permission surface, no manifest CSP, and PAC built by splicing raw user text through an AST escape

**Severity: Informational / Low.** The permissions are what the product's function requires; the
finding is that the safety of the whole feature rests on a hand-written escape sequence, with no
Content-Security-Policy to contain a mistake.

**VERIFIED.** The MV3 manifest requests `proxy`, `webRequest`, `unlimitedStorage` and `<all_urls>`:
`omega-target-chromium-extension/overlay/manifest.json:39-49` (`"proxy"` at `:40`,
`"webRequest"` at `:45`, `"unlimitedStorage"` at `:44`, `"host_permissions": ["<all_urls>"]` at `:49`).
The manifest object is lines 1-50 and declares no `content_security_policy` key — a repository-wide
search for that string returns nothing, so there is no CSP declared in this tree
(**INFERRED** from the absence across the manifest and the derived copies under `build/`/`tmp/`).

The PAC for a `PacProfile` is produced by injecting the raw downloaded script into a generated
function via a raw-AST node. `AST_Raw` is defined at `omega-pac/src/profiles.coffee:8-12`. The splice
is in the PacProfile `compile`: `omega-pac/src/profiles.coffee:358-373`, and the literal injection —
`new AST_Raw ';\n' + profile.pacScript + '\n\n/* End of PAC */;'` — is at `:373`. The four-step
comment-termination hack that makes this not break the surrounding generated code is documented in
the code comment block at `omega-pac/src/profiles.coffee:363-372` (terminate any open line comment,
then guard against a trailing backslash, then close any open block comment with `/* … */`, then a
semicolon). The compiled profile is embedded into the final script by `PacGenerator.script` calling
`Profiles.compile` (`omega-pac/src/pac_generator.coffee:39`), and the generated `FindProxyForURL`
resolution loop is itself keyed on the `+` marker
(`omega-pac/src/pac_generator.coffee:138`).

**Reproduce (description only).** A PAC/RuleList subscription whose fetched body contains an
unterminated comment or a `*/` sequence is spliced at `:373`; the surrounding hack is designed to
neutralise it, so the whole guarantee rests on the escape being correct for every input. Impact is
bounded by the PAC evaluation context, not by the extension's own privileges — an injected script does
not by itself escalate to `webRequest`/`proxy` authority.

**Mitigation NEXORA already specifies.** A documented "will not be rewritten here" item
(PROJECT-STATE §6 #10 lists the `PacGenerator`/`Conditions`/`RuleList` rewrite and CSP hardening as
*not fixed*). NEXORA keeps `Conditions` as the compiled-outbound mechanism rather than replacing the
PAC path (ADR-0013); CSP/permission reduction is deferred to Phase 2, and the loopback-only inbound
boundary that limits blast radius is asserted at `core.start`
(`docs/architecture/30-failure-policy.md`, `packages/schema/protocol/commands/config.snapshot.json`).

---

## SEC-4 — `ExternalApi` privileges are granted entirely on a hard-coded extension-ID list

**Severity: Low / Medium** (a foreign extension that happens to be on the list, or is the listed ID,
can read the full options set and disable the extension; no user interaction beyond installing it).

**VERIFIED.** The permission model is a single hard-coded map keyed on the connecting extension's ID:
`omega-target-chromium-extension/src/module/external_api.coffee:9-10`
(`knownExts: 'padekgcemlokbadohgkifijomclgjgif': 32`). It listens on `chrome.runtime.onConnectExternal`
(`omega-target-chromium-extension/src/module/external_api.coffee:12-17`) — i.e. it accepts connections
from *other extensions*. Authorization is nothing but a lookup of `port.sender.id` in that map with a
numeric level: `omega-target-chromium-extension/src/module/external_api.coffee:32-38` (`checkPerm`).
Reaching level 16 lets the caller fully disable proxy control, and level 32 marks it as an
"upgrade": `omega-target-chromium-extension/src/module/external_api.coffee:43-52`. Level 8 grants a
read of the entire options object: `omega-target-chromium-extension/src/module/external_api.coffee:59-61`
(`port.postMessage({action: 'options', options: @options.getAll()})`) — which includes subscription
`headers` (SEC-1) and any field held in `_options`.

**Reproduce (description only).** Any extension connecting over `connectExternal` presents its own ID;
if that ID equals the hard-coded one it obtains level 32 and can issue `getOptions`/`disable`. There is
no key, handshake, or capability check beyond the ID match. **Preconditions:** the attacker is another
installed extension — the risk is the *listed* extension being malicious or compromised, since Chrome
supplies `sender.id` and it cannot be forged, and the ID is the sole authorization factor.

**Mitigation NEXORA already specifies.** The external surface is not carried into NEXORA's control
model: browser↔Connector is Native Messaging only with typed, allowlisted commands and no arbitrary
pass-through (PROJECT-STATE §0 constraints 2-3; `packages/schema/protocol/command-allowlist.json`,
19 closed methods, and the envelope identity fields in
`packages/schema/protocol/envelope.schema.json`). The full options dump over an unauthenticated port
is replaced by per-command schemas such as
`packages/schema/protocol/commands/config.snapshot.json`.

---

## SEC-5 — Gist/WebDAV tokens live in synced/local state, with the allowlist exclusion as the only guard

**Severity: Medium.** The sync credential is placed in `chrome.storage.sync`, i.e. the Google-account
sync channel that replicates it to every signed-in device, not merely to local disk.

**VERIFIED.** The token is stored through the state layer: `omega-target/src/options.coffee:1187`
(`'gistToken': gistToken` in `setOptionsSync`) and read back on load
(`omega-target/src/options.coffee:143`, `:158`). For the built-in sync enhancement the whole config
object — `gistId`, `gistToken`, `syncUsername` — is written into the sync storage: the pull-and-store
runs `toggleBuiltInSync` → `omega-target/src/options_sync.coffee:240-242`
(`_obj[BUILTINSYNCKEY] = syncConfig`; `@builtInSyncStorage.set(_obj)`), and that storage is a
`Storage('sync')` bound to `chrome.storage.sync`:
`omega-target-chromium-extension/src/coffee/background.coffee:238`, then watched and replayed into
local state at `omega-target-chromium-extension/src/coffee/background.coffee:260-271`. WebDAV reuses
the same `gistToken` field for its bearer/basic credential
(`omega-target-chromium-extension/src/module/sync/sync_impl.coffee:9`) — the token is placed in an
`Authorization` header directly: `omega-target-chromium-extension/src/module/sync/sync_impl_webdav.coffee:18`
(reads `config.token`), `:68` (Basic `btoa(username + ':' + token)`), `:77` (Bearer). The only thing
stopping secrets from leaving via the *options envelope* is the value-allowlist of SEC-1
(`omega-target/src/options.coffee:27`, `:89-99`) — there is no secret denylist, no masking at rest,
and no encryption; the credential is stored in the clear in `chrome.storage.sync`.

**Reproduce (description only).** With built-in sync on, inspect `chrome.storage.sync` for the
`zeroOmegaSync` key — it holds `gistToken`; enabling sync replicates it to other devices on the same
account. **Preconditions:** anyone who can read the synced storage (a compromised Google-account
session, another process on a synced device) or the local extension storage obtains the token, scoped
to Gist (or the WebDAV host) — enough to read/overwrite the user's options and use the WebDAV account.

**Status today: NOT fixed** — same reasoning as SEC-1 (it is a behaviour/storage change needing a
test). **Mitigation.** ADR-0010/0011: Windows keystore is a DPAPI-protected file under
`%LOCALAPPDATA%\NEXORA\keystore/`, never `chrome.storage.sync`; secret-bearing envelope namespaces are
forced non-sync (`secretBearing ⇒ ¬syncEligible`) at
`packages/schema/envelope-keys.schema.json:54-61`, and the two secret-bearing namespaces are pinned
non-sync-eligible there. Durable spec `docs/architecture/32-secrets-keystore-masking.md`.

---

## SEC-6 — No concept of binary integrity anywhere in the tree (harmless today, fatal once a local process executes a config)

**Severity: Informational now / High post-Connector** (there is no downloaded, locally executed
binary today, so nothing to verify; the moment the Connector runs Xray Core, an unpinned-and-unverified
binary is a proxy that carries the user's traffic — the single worst supply-chain position).

**VERIFIED (as absence, the honest form of this finding).** There is no signature or integrity
verification of any executable in the current code. A repository-wide search over `**/src/**` for
`verifySignature|minisign|authenticode|integrity|digest(` returns only the *subscription-content* hash
path — `crypto.subtle.digest("SHA-256", data)` at `omega-target/src/options.coffee:13`, used to fill
`profile.sha256` at `omega-target/src/options.coffee:806-807`, which hashes rule text, not a binary,
and is consumed only as a change-detection token
(`omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:56-57`). There is no code that
fetches, checks or executes a Core binary — PROJECT-STATE §1.8 records zero Go, zero Xray, zero
Native Messaging in this tree. The *planned* test-data home `connector/testdata/pins.json` does **not
exist** (the entire `connector/` directory is absent), so it is named, not cited; the real, existing
home for the pin contract on disk is the schema itself.

**Reproduce (description only).** Not reproducible as an exploit today — there is nothing to attack; the
finding is a forward-looking gap. Under the NEXORA design it becomes reproducible the first time the
Connector is handed a Core binary whose provenance was never verified (attacker: anyone who controls
the first-run download without signature verification).

**Mitigation NEXORA already specifies.** Xray is an integrity-pinned downloadable asset (ADR-0019):
exact upstream repo, exact tag, `verifySignature` const true, `silentSelfUpdate` const false,
`sha256` `^[0-9a-f]{64}$`, `verifiedAt` timestamp, restricted to the upstream release host —
all enforced in `packages/schema/pins.schema.json`: `:31` (repository const), `:33-36` (exact
releaseTag), `:54-57` (`verifySignature` const true), `:131-134` (`sha256` pattern), `:126-129`
(upstream-only `url`), `:147-151` (`verifiedAt`). Mismatch = refuse to start. Durable spec
`docs/architecture/33-*` (Core install/verification).

---

## SEC-7 — Log download is exported with no masking (and the console hook's mask omits subscription secrets)

**Severity: Low / Medium** (the exported artifact can contain secrets; exposure is to whoever the user
hands the log file to — a GitHub issue, a support channel).

**VERIFIED.** Two export paths exist and neither masks subscription secrets.

1. **`localStorage['log']` path (legacy / Firefox), no masking at all.** The global error handler
   appends raw stack/message text with no redaction: `omega-web/src/coffee/log_error.coffee:4`
   (`log = localStorage['log'] || ''`) → `:6` (`log += err.stack`) → `:9` (store back). The About-page
   "download log" reads that exact key and ships it whole:
   `omega-web/src/omega/app.coffee:90-93` (`blob = new Blob [localStorage['log']], …` →
   `downloadFile(blob, "OmegaLog_…")`), surfaced by the button at
   `omega-web/src/partials/about.jade:22` and its controller
   `omega-web/src/omega/controllers/about.coffee:4-6`.
2. **IndexedDB console-hook path (chromium), masking that omits the fields that matter.** The overlay
   hooks `console.log`/`console.error` at `omega-target-chromium-extension/overlay/log.js:115-116` and
   persists them; its `replacerFn` masks only
   `username, password, host, port, token, gistToken, gistId` at
   `omega-target-chromium-extension/overlay/log.js:45-58` — note it does **not** mask `headers` or
   `syncUsername`, and the replacer runs only on non-string objects
   (`omega-target-chromium-extension/overlay/log.js:64-67`), so a bare string containing a secret is
   logged raw. The zip download exports those entries with no further redaction:
   `omega-target-chromium-extension/src/coffee/omega_debug.coffee:17-35`. Because the background logs
   whole options/profile objects on sync paths (e.g. `@log.log('Options#loadOptions::fromSync',
   options)` at `omega-target/src/options.coffee:237`), a profile's `headers` array lands in the log
   unmasked.
3. The background `Log.log`/`Log.error` are direct `console.*` bindings
   (`omega-target/src/log.coffee:45`, `:51`), so everything captured is what the hook sees; the
   masking `replacer` in that same file (`omega-target/src/log.coffee:4-11`) runs only through
   `Log.str`/`Log.method`, not plain `console.log`.

**Reproduce (description only).** Trigger a code path that logs a profile carrying an `Authorization`
header (subscription update / sync push), open About → download log, and grep the exported file for the
header value. **Preconditions:** exposure is to whoever the user hands the log to (a GitHub issue, a
support channel), or to an adversary with local file access to the log store — bounded to the user's
own artifacts.

**Status today: NOT fixed** (changing logging behaviour is a product change; deferred).
**Mitigation NEXORA already specifies.** "Never in logs, minimal storage, masking" is constraint 9
(PROJECT-STATE §0); the durable control is `docs/architecture/32-secrets-keystore-masking.md`, with
the invariant that secrets exist only as `keystore://…` references
(`packages/schema/envelope-keys.schema.json:58-61`) so there is nothing secret to mask in a log line.

---

## Summary table

| id | one-line description | severity (today) | status today | mechanism that owns it in NEXORA |
|----|----------------------|------------------|--------------|----------------------------------|
| SEC-1 | Subscription `Authorization` header is synced verbatim; `transformValueKeys` never lists `headers` | Medium | Not fixed (Phase-2, test-gated) | `packages/schema/envelope-keys.schema.json` (`syncExcludedValueKeys` += headers); `subscription-source.schema.json` `headersRef` |
| SEC-2 | Bare `fetch()`: no SSRF/host guard, unbounded body, RuleList Content-Type catch-all `*` | Low | Not fixed | `subscription-source.schema.json` (`https` only, `limits.maxBytes`); Connector SSRF guard; `docs/architecture/29-*` |
| SEC-3 | `<all_urls>`+`webRequest`+`proxy`+`unlimitedStorage`, no manifest CSP, PAC spliced via raw-AST 4-step hack | Informational/Low | Not fixed (deferred) | ADR-0013 keep `Conditions`; loopback-only assertion `docs/architecture/30-*`; CSP/permission reduction Phase-2 |
| SEC-4 | ExternalApi trust = hard-coded extension-ID map; `getOptions` dumps all secrets | Low/Medium | Not fixed (surface retired) | Native Messaging only; `protocol/command-allowlist.json`; `protocol/commands/config.snapshot.json` |
| SEC-5 | Gist/WebDAV token stored in `chrome.storage.sync`; allowlist exclusion is the only guard | Medium | Not fixed (Phase-2) | ADR-0010/0011 DPAPI keystore; `envelope-keys.schema.json` `secretBearing ⇒ ¬syncEligible`; `docs/architecture/32-*` |
| SEC-6 | No binary signature/hash verification anywhere (nothing to verify today) | Informational now → High post-Connector | Gap by design until Connector | `packages/schema/pins.schema.json` (`verifySignature`/`sha256`/`verifiedAt`); ADR-0019 |
| SEC-7 | Logs exported from `localStorage['log']`/IndexedDB with no (or partial, `headers`-missing) masking | Low/Medium | Not fixed | constraint 9 (no secrets in logs); `docs/architecture/32-*`; secrets as references only |

**No product code was changed to produce this document.** The only product-file edits on this branch
are the two `coffeelint.coffee` lint-level demotions needed to run the build (PROJECT-STATE §4.3). SEC-1,
SEC-2, SEC-3, SEC-5 and SEC-7 are **Phase-2 requirements gated on a test**; SEC-6 is a deliberate
forward-looking gap the Connector introduces.

## UNVERIFIED registry

- **No CSP declared (SEC-3).** The absence of `content_security_policy` in `overlay/manifest.json`
  is VERIFIED for that file; the claim that *no* manifest anywhere in the tree declares a CSP is
  **INFERRED** from a repository-wide string search and from `build/`/`tmp/` being derived copies
  (they are regenerated from the overlay, per PROJECT-STATE §3). Not independently proven for every
  historical build artifact.
- **Cloud-metadata SSRF reachability (SEC-2).** That the code performs no host/scheme validation is
  VERIFIED (`omega-target-chromium-extension/src/module/fetch_url.coffee:10`, `:36-46`). That a given deployment can actually reach
  `169.254.169.254` or a loopback service is environment-dependent and **UNVERIFIED** here (no network
  test was run, by design — no PoC).
- **`connector/testdata/pins.json` does not exist (SEC-6).** It was VERIFIED by directory listing when the
  whole `connector/` tree was absent, and is still true: the module landed on 2026-11-05 without that file
  (`docs/PROJECT-STATE.md` §4.6). The statement remains an existence claim about the current tree, not about
  the eventual Phase-2 layout.
- **Log contents actually contain a secret in normal operation (SEC-7).** That the export path applies
  no `headers`/`syncUsername` masking is VERIFIED. Whether a specific log file in the wild contains a
  live credential depends on which code paths fired; this is **INFERRED** from the `console.log` of
  whole options/profile objects at `omega-target/src/options.coffee:237` and was not observed in a
  captured artifact.
- **`chrome.storage.session` temp-rule lifetime** is treated in `docs/audit/12`, not here; it is
  reproduced there with its own tag.
