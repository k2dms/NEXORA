# 31 — Security model

> Purpose: who is allowed to touch the proxy decision, from where, how often, and what stays true when the
> answer to any of those is "no". This is not a template threat model — every row below names a position an
> adversary actually occupies in this architecture, the control that closes it, and, where there is no control,
> that fact is written down instead of smoothed over.

Status: Accepted. Normative data: `packages/schema/protocol/framing.json#/senderPolicy`,
`packages/schema/protocol/command-allowlist.json`, `packages/schema/protocol/error-taxonomy.json#/codes`,
`packages/schema/connector-settings.schema.json`, `packages/schema/envelope-keys.schema.json`,
`packages/schema/subscription-source.schema.json`, `packages/schema/pins.schema.json`,
`packages/schema/failure-policy.schema.json`.
Evidence base: `docs/audit/11-security-findings.md` (SEC-1…SEC-7 — the measured starting position),
`docs/audit/10-manifest-packaging-distribution.md` (what the manifest actually grants).

## 1. Reading rules, and what this document does not own

As in `docs/architecture/20-target-repository-layout.md` §1. Two additions specific to a security document:

- A control with no check behind it is labelled `Verified: NO — deferred to Phase 2` at the point of use, not in a
  summary at the end. A security claim that quietly became a hope is the most expensive kind of stale document.
- "Mitigated" here means *a named mechanism exists and is described in data or in code*, not "the risk is now
  low". Residual risk is §14, and it is deliberately long.

Not owned here, by design: the secret lifecycle and masking — `docs/architecture/32-secrets-keystore-masking.md`;
binary provenance and install — `docs/architecture/33-binary-integrity.md`; the failure resolutions themselves —
`docs/architecture/30-failure-policy.md`; which key owns which byte — `docs/architecture/34-data-split.md`;
browser/OS support claims — `docs/architecture/38-compatibility-matrix.md`; the execution status of every check
named below — `docs/architecture/39-verification-matrix.md`.

## 2. The five questions

1. **Who** may command the Connector? §6 — identity comes from the OS, not from the message.
2. **What** may they command? §7 — nineteen typed methods, denied by default.
3. **How much, how often?** §8 — per-sender budgets, enforced before dispatch.
4. **What happens when the answer is no?** §9 for a refusal, `docs/architecture/30-failure-policy.md` for a failure.
5. **What does none of this protect against?** §14. A model without that section is marketing.

## 3. What is being protected

The assets, in the order a user would grieve them:

| Asset | Where it rests | What losing it means |
|---|---|---|
| The proxy decision ("does this request go through the tunnel?") | `chrome.proxy.settings` written by the worker; the applied `chrome.proxy` value is the only actuator | Silent de-proxying: the user's traffic leaves unencrypted to whatever network they are on, believing otherwise. This is why a leak is a *security* failure, not an availability one (ADR-0009) |
| Credentials (VLESS uuid, REALITY key material, feed `Authorization`) | Connector keystore only (ADR-0011); in the browser only as `keystore://…` references — `packages/schema/server.schema.json#/properties/userIdRef` | A stolen uuid is a usable tunnel and a bill; a stolen feed token is the subscription itself |
| The last working configuration | Connector config store + the extension's own profile envelope | Integrity *and* availability: a refresh failure that overwrote good data is a self-inflicted outage |
| The machine | The Connector runs with the full rights of the interactive user | The Connector is the only component that spawns processes; see §14, row "same-user malware" |
| The user's existing setup | `docs/audit/04-data-model-and-options-envelope.md`, ADR-0013/0014/0015 | Losing years of rules is indistinguishable from malice to the user |
| The update path | Extension store; Core pins (`packages/schema/pins.schema.json#/properties/upstream`) | Where supply chain lives; ADR-0019 closes the Core half, nothing closes the store half |

## 4. Boundaries

| Boundary | What crosses it | What authenticates the crossing | What a breach buys |
|---|---|---|---|
| web content → extension | requests the decision applies to | nothing, by design: content cannot address the extension | only a *different choice* through a PAC evaluation; no message channel (see §10) |
| worker → host shim | one JSON envelope per frame | the OS pipe plus the sender id the registration resolved (`packages/schema/protocol/framing.json#/senderPolicy/identity`) | the nineteen methods, within budget |
| shim → Connector | the same envelope, untranslated | the inherited pipe and the per-user lock file | same |
| Connector → Core | a compiled config file and a spawn | the pin attestation (`docs/architecture/33-binary-integrity.md`) | arbitrary behaviour of the process that holds the credentials |
| Core ↔ internet | the user's tunneled traffic | the transport's own handshake (VLESS uuid, REALITY shortId) | nothing local: Core is the boundary, not a way back in |
| browser ⇄ sync endpoint | the options envelope | the user's own Gist/WebDAV credential | today it also buys the feed `Authorization` header — SEC-1 in `docs/audit/11-security-findings.md` |
| disk → memory | keystore blob | DPAPI, scoped to the user (ADR-0011) | nothing across users; everything across processes of the same user (§14) |

## 5. The starting position, measured

Seven findings, all VERIFIED in `docs/audit/11-security-findings.md`, none fixed by product code in this
iteration (that is PROJECT-STATE §2.2's rule: a behaviour change needs a test first). Read as a security model
rather than a list, they say three things:

1. **The legacy identity mechanism was a constant in source.** `knownExts: 'padekgcemlokbadohgkifijomclgjgif':
   32` at `omega-target-chromium-extension/src/module/external_api.coffee:9-10`, looked up against
   `port.sender.id` in `checkPerm` at `omega-target-chromium-extension/src/module/external_api.coffee:32-38`, on a
   listener that accepts *any* extension (`omega-target-chromium-extension/src/module/external_api.coffee:13-17`,
   wired live at `omega-target-chromium-extension/src/coffee/background.coffee:253`). Level 8 returns the whole
   options object — `omega-target-chromium-extension/src/module/external_api.coffee:59-61` — which contains every
   subscription header (SEC-1) and the sync credential (SEC-5).
2. **The same surface can command `system`.** `disable` at level 16 calls `applyProfile('system')` at
   `omega-target-chromium-extension/src/module/external_api.coffee:47-48`. In NEXORA `system` is a *forbidden
   outcome* (`packages/schema/failure-policy.schema.json#/properties/forbiddenOutcomes`), so the surface cannot
   survive into the new control model unchanged. That is the reason for §6.5, not a preference for tidiness.
3. **Secrets travelled wherever the code already travelled.** `omega-target/src/options.coffee:27` strips four
   keys before sync and `headers` is not among them;
   `omega-target-chromium-extension/overlay/log.js:45-58` masks a fixed word list that omits
   `headers`; `omega-target/src/options.coffee:237` logs whole option objects; `omega-target/src/options_sync.coffee:103`
   pushes whatever the transform returned. None of it is a broken cipher — there is no cipher, only lists that
   were never extended. That is why §11's rule is *structure* (references, not values) rather than a better list.

## 6. Who may command the Connector

Identity is resolved by the operating system when the port opens, from the Native Messaging manifest
registration. ADR-0004 rule 3; the data is
`packages/schema/protocol/framing.json#/senderPolicy/identity`. Consequences, all of them machine-checked:

- **These are not identity**: a payload field asserting the caller is the extension, the method name or its args,
  `envelope.traceId`, `envelope.seq`, the peer command line — the list is
  `packages/schema/protocol/framing.json#/senderPolicy/identity/notIdentity` and the assertion "the policy names
  what it refuses to treat as identity" fails if it shrinks. ADR-0004's
  rejected alternative "loopback HTTP plus a bearer token" died precisely because a token in a discoverable file
  turns identity back into a payload field.
- **The allowlist is not browser data.** It lives in the host's own startup configuration, written by the installer
  (`packages/schema/protocol/framing.json#/senderPolicy/identity/allowlistSource`). Storing it under
  `-nexora.settings` would let the caller configure
  its own authorization — the caller is exactly the thing rule 6 exists to constrain.
- **An unknown sender gets `UnauthorizedExtension` and nothing else** (`packages/schema/protocol/framing.json#/senderPolicy/identity/unknownSender`;
  ADR-0004 rule 6). `UnauthorizedExtension` declares `retryable: false` in
  `packages/schema/protocol/error-taxonomy.json#/codes`, checked by the assertion "UnauthorizedExtension is not
  retryable", because a refusal a client retries is a loop against the only process that holds the keystore.
- **Refusals are bounded too.** `packages/schema/protocol/framing.json#/senderPolicy/refusals/amplificationGuard`: ten refusals in ten seconds and the
  host closes the port instead of continuing to answer. An unallowlisted sender with a retry loop is a CPU-and-log
  attack, and the defence costs the attacker nothing to notice.
- **Ports per sender are capped** at two (`packages/schema/protocol/framing.json#/senderPolicy/portsPerSender`), which is the MV3 eviction overlap
  (worker dies, port tears, new worker opens one) and nothing legitimate needs a third.

### 6.1 Two error codes, two remediations

`docs/architecture/25-native-messaging-protocol.md` §8 rule 5 promised the distinction; here it is.

| Code | What actually happened | Who fixes it | What they do |
|---|---|---|---|
| `HandshakeFailed` | the sender is known but the *negotiation* did not happen or did not agree: a gated method arrived before `connector.hello`, or the version windows do not intersect | the installation | upgrade one side; the matrix rows are `packages/schema/protocol/compatibility-rules.json` and the legacy profiles keep working meanwhile (ADR-0015) |
| `UnauthorizedExtension` | the sender is not on the allowlist the host resolved at startup | the operator, deliberately | add the id to the host's install configuration, or understand why something else is talking to the Connector. Never automatic: an auto-retry would be a persistent attempt to use a pipe that has already said no |

### 6.2 Privileged methods

Eleven of the nineteen methods carry `privileged: true` in
`packages/schema/protocol/command-allowlist.json#/commands`: `core.start`, `core.stop`, `core.restart`,
`core.select`, `config.apply`, `profile.import`, `profile.remove`, `subscription.refresh`, `server.test`,
`diagnostics.get`, `keystore.purge`. The set is the one thing a Connector build MAY refuse to a sender that is
not the pinned extension id (ADR-0004, `docs/architecture/26-command-surface.md` §2), because every member either
starts a process, writes the keystore, deletes an entity, or emits traffic the user did not type.

The names are duplicated in `packages/schema/protocol/framing.json#/senderPolicy/privilegedMethods/names` on purpose: the assertion
"senderPolicy.privilegedMethods.names is exactly the allowlist's privileged flag" fails the build when the two
lists separate, and two more assertions hold the shape honest — the privileged set must be non-vacuous, and it
must be *smaller* than the whole surface, because a surface with no unprivileged read is a popup that cannot
render without being granted power. `Verified: NO — deferred to Phase 2` for the runtime refusal itself (LIVE-16).

### 6.3 Pinned id, and why Firefox is the hard case

The pinned id is the sender identity the installer writes down. On Chromium it is the key-derived extension id;
the manifests declare no `key` field (`docs/audit/10-manifest-packaging-distribution.md` §1), which means two
builds cannot accidentally share one — an accidental shared id would be an accidental shared allowlist entry.
On Firefox the identity is the literal `browser_specific_settings.gecko.id` at
`omega-target-chromium-extension/overlay/manifest-firefox.json:51`. It is an author's email address, and ADR-0015
forbids changing it, because changing it means a new install and a lost user. So the AMO id is *simultaneously*
the upgrade-continuity constraint and the security principal: it cannot rotate, and it must be reviewed like a
credential, which is a decision rather than an accident — recorded in `docs/architecture/40-open-decisions.md`.

### 6.4 What the extension cannot be made to do

`nativeMessaging` is absent from both shipped manifests (`omega-target-chromium-extension/overlay/manifest.json:39-48`,
`omega-target-chromium-extension/overlay/manifest-firefox.json:38-47`) — PROJECT-STATE §1.8 records zero Native
Messaging code in this tree. Adding it is the single largest change to the browser's reachable surface, and that
is the reason the surface it reaches is deliberately tiny: one shim, no business logic, no keystore access, no
config compiler (ADR-0005, `docs/architecture/21-system-context.md` §6).

### 6.5 The legacy external API

`ExternalApi` is not carried into the NEXORA control model. Reasons, in order of weight: its only authorization
factor is an id constant (§5.1); level 8 dumps every secret the extension holds; level 16 commands a `system`
resolution, which ADR-0009 forbids (§5.2); and it logs every inbound message whole, including its args
(`omega-target-chromium-extension/src/module/external_api.coffee:41`), which is the SEC-7 class with a new
source. `Verified: NO — deferred to Phase 2` — the honest statement today is that the surface still exists in the
inherited tree.

Whether *any* external surface survives is a compatibility question, not a security one: third-party tools that
drive SwitchyOmega today would stop working. That is a user-visible decision, so this document does not make it.
It constrains the answer: if the surface survives, `getOptions` does not — a whole-options read is incompatible
with per-command schemas and with secrets-never-values — and neither does the `disable` path that resolves to
`system`. Tracked in `docs/architecture/40-open-decisions.md`.

## 7. What a sender may command

Nineteen closed methods, `deniedByDefault: true`, one total dispatch function with exactly one failure case
(`packages/schema/protocol/command-allowlist.json#/deniedByDefault`, `#/dispatchRule`), and no fallthrough
(ADR-0007). The security-relevant property is what the surface *cannot* express:

- **No shell command.** Not a field, not a substring, not a "diagnostics" escape hatch. ADR-0007's allowlist is
  the reason the Connector can be reviewed as a state machine instead of as a command injection point.
- **No path chosen by the caller.** The install root is `packages/schema/connector-settings.schema.json#/properties/core/properties/installRoot`
  — an override of a pin, not an arbitrary file, and `installChannel` has exactly `pinned` and `manual`
  (`#/properties/core/properties/installChannel`), whose descriptions say why there is no "whatever is on disk".
- **No address chosen by the caller for a bind.** `#/properties/ports/properties/host` is the const `127.0.0.1`;
  ADR-0006 made it unconfigurable *in the schema*, which is a stronger statement than a runtime check because a
  config that cannot be written cannot be applied.
- **No method before negotiation.** `connector.hello` is the only pre-hello dispatch
  (`packages/schema/protocol/command-allowlist.json`, assertion "nothing is dispatchable before negotiation except
  connector.hello"), and `commandGating` in `packages/schema/protocol/compatibility-rules.json` requires the
  capability to have been announced. This is what makes a misread frame unable to apply a wrong configuration —
  there is no frame to misread yet.
- **No free-form URL inside a command.** Subscription URLs are validated by
  `packages/schema/subscription-source.schema.json#/properties/url` and the https/no-userinfo branch at `#/allOf/0/properties/url`.

Class semantics (`read` / `mutate` / `converge` / `replace` / `probe`) are not decoration: they decide whether a
replay may be served from cache (ADR-0008), which commands are idempotent, and which rate row applies (§8). The
allowlist and the per-command documents must agree on the class, checked by "command class agrees between allowlist
and command doc".

## 8. How often: per-sender budgets

The budget is data: `packages/schema/protocol/framing.json#/senderPolicy/rateLimits`. Per sender id, per Connector
process, counted by the Connector **before dispatch**, so a request refused by budget never reaches a handler and cannot
half-apply (`packages/schema/protocol/framing.json#/senderPolicy/rateLimitFacts/countedWhere`). The shim cannot count,
and this is not a preference: a scoped budget names a method, the method name is inside the payload, and
`docs/architecture/25-native-messaging-protocol.md` §2 forbids the shim to interpret payloads -- what it does enforce is
the sender allowlist, the byte limits and the ports per sender, all three answerable from an opened port and a frame
header without opening anything. The counters therefore live in the process that holds the keystore, which is the
process the amplification guard in `#/senderPolicy/refusals/amplificationGuard` is written to protect.

| Scope | Budget | On exceed | Why that number |
|---|---|---|---|
| `subscription.refresh` | 3 / 60 s | `Busy` | `args.force` bypasses the per-source backoff, so this row is the only thing between a stuck options tab and the user's feed provider. One call may carry forty sources: the row counts calls, not fetches |
| `server.test` | 8 / 10 s | `Busy` | enough for one panel of servers, too small for a poll loop to reach every VPS on the list each second. Probes are never retried, so a refusal is not a lost measurement |
| `config.apply`, `core.start`, `core.stop`, `core.restart`, `core.select` | 4 / 20 s | `Busy` | each may restart Core, and a restart drops every live tunnel. Converge-to-desired-state means waiting loses the user nothing |
| `profile.import`, `profile.remove`, `keystore.purge` | 2 / 60 s | `Busy` | replace-class and destructive: a second import inside a minute is a retry of the first (idempotency answers it) or a bug |
| `diagnostics.get` | 1 / 120 s | `Busy` | the most expensive read: it walks logs and writes a dump to disk |
| `*` (global floor) | 20 / 1 s | `Busy` | bounds the pipe against a spinning worker, so a mistake in any row above cannot become an unbounded queue |

Three rules hold the table together, each with an assertion that runs today:

- **A refusal must be retryable and must say "later", not "impossible".** Every row answers `Busy`, whose taxonomy
  entry declares `retryable: true`; `QuotaExceeded` is a *storage-layer* code and using it for a budget tells the
  client it has run out of disk. Checked by "rate limit refusal is Busy" and by the `Busy` meaning in
  `packages/schema/protocol/error-taxonomy.json#/codes`.
- **No class budget may exceed the floor** — checked arithmetically per row ("a class budget never exceeds the
  global floor"), so the table cannot rot into a set of numbers that contradict each other.
- **Every privileged method carries a budget** — checked by name, not by class ("every privileged method carries a
  rate budget: `<name>`"). That is the assertion that makes §6.2 and §8 one statement instead of two.

Budget spent is *not* persisted: a Connector restart clears it, which is right for a user who restarted the
browser and wrong for nobody, because the budget's whole purpose is to bound a caller that is stuck.

What the budgets are the enforcement half of (all three promises were written before this table existed, and are
now pointing at real numbers): opening the popup performs no side-effecting command
(`docs/architecture/22-component-contracts.md` §"popup must not" list); probe rate is bounded independently of the
UI (`docs/architecture/22-component-contracts.md` §8, whose fourth invariant is this budget); `force` bypasses backoff, not the
rate limit (`docs/architecture/29-subscriptions-and-servers.md` §7). `Verified: NO — deferred to Phase 2`
for enforcement (LIVE-17); GREEN for the numbers, the coverage and the arithmetic.

## 9. What a refusal costs the user

A security control that fails silently is a support ticket with a wrong answer in it. The refusal discipline:

| Refusal | User-visible surface | MUST NOT |
|---|---|---|
| `UnauthorizedExtension` | Connector-side log line: sender id, method name, window remaining — never args (`packages/schema/protocol/framing.json#/senderPolicy/refusals/loggedFields`), because args may carry a document the user typed | appear as a proxy failure; the tunnel is fine, the caller is not |
| Budget exhausted (`Busy`) | the UI disables the action it just refused, with the remaining window; the request is retried by the user, not by a loop | be retried silently by the client — a silent retry is how a budget becomes a queue |
| Capability absent after hello | the Connector panel is disabled *with a reason*; legacy profiles keep working | resolve to direct or system (ADR-0009, `packages/schema/failure-policy.schema.json#/properties/forbiddenOutcomes`) |
| `SecretRedacted` | the call was refused *because* a secret was in a place that syncs or logs | be reported as a corrupt document — `docs/architecture/32-secrets-keystore-masking.md` owns the distinction |
| Non-loopback bind in a compiled config | `core.start` refuses, and the previous config stays applied | be repaired by editing the config toward what the caller asked for |

## 10. The pipe is not a sandbox

Stated plainly because it is the most common way a model like this gets over-read: **the pipe authenticates the
sender, not the intent.** Once the sender id matches, every method in §7 is available within its budget, and the
Connector has no idea whether a human asked for it. Most of what is called "attack" in this product is therefore
not an outsider — it is our own code at rest: a UI defect, a bad compile, a subscription body that parses. The
depth against that is not "we trust the extension less", it is four structural facts:

1. **The extension cannot read a secret, so a compromised extension is not a credential theft.** Every secret
   field is a reference — `packages/schema/server.schema.json#/properties/userIdRef`,
   `#/$defs/headersRef`, `packages/schema/subscription-source.schema.json#/properties/headersRef`. This is also
   the structural close of SEC-1 and SEC-5: the sync exclusion list
   (`packages/schema/envelope-keys.schema.json#/properties/syncExcludedValueKeys`) stops being a list someone has
   to remember, because what would leak is inert on another machine.
2. **Args are validated against a per-command schema before any handler runs**, and the command documents are
   themselves validated against a meta-schema — the harness's `command doc conforms to meta-schema` loop, which is
   why "typed commands" is a checked property rather than an intention (ADR-0007).
3. **A rule that cannot be expressed is reported, not dropped:**
   `packages/schema/protocol/commands/config.apply.json#/result/properties/unrepresentable`. Silent dropping is how
   a routing decision becomes different from the one the user made.
4. **Fail-closed is a state, not a reaction** — `docs/architecture/30-failure-policy.md` §1 rule 2. The applied
   `chrome.proxy` value points at a loopback port only a live Core owns, so MV3 inaction (evicted worker plus dead
   Connector) is already safe. That is why the browser half of this model does not depend on the Connector being
   reachable.

ConfirmToken is the fifth: `packages/schema/failure-policy.schema.json#/$defs/ProfilePolicy` makes a per-profile
fail-open require an argument shaped like a confirmation the UI cannot invent. It converts an accident into a
decision. It does not make the decision safe, and §13 says so.

## 11. Secrets and logs — the security-model half

`docs/architecture/32-secrets-keystore-masking.md` owns the lifecycle. Three facts belong here because they are
boundary properties rather than storage details:

- **DPAPI is per-user, not per-process.** `docs/architecture/21-system-context.md` §3 pointed at this document for
  the residual: a blob protected with the user's entropy can be unprotected by *any* code running as that user.
  ADR-0011's choice is therefore explicitly against a narrower set than "malware": it defeats another user, a
  stolen disk image, and a roaming-profile copy. No self-written crypto is added on top to pretend otherwise —
  the master constraint and ADR-0011 both forbid inventing a cipher, and a cipher whose key is stored next to the
  ciphertext by the same process is theatre.
- **Diagnostics are a named, closed set of sections**
  (`packages/schema/connector-settings.schema.json#/properties/telemetry/properties/diagnosticsIncludeByDefault`),
  and the harness asserts every name exists in `diagnostics.get`'s own include enum. The legacy counterpart is the
  whole `localStorage['log']` shipped verbatim (SEC-7), plus a mask list
  (`omega-target-chromium-extension/overlay/log.js:45-58`) that omits the two fields that matter. A dump whose
  contents are a closed enum cannot accidentally include a new secret later; a dump that is "whatever we logged"
  can, which is why the legacy auth path's `ProxyAuth` log line at
  `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:80` is a design note (host:port of the
  user's proxy is destination metadata) and not a defect.
- **`diagnostics.get` is privileged and rate-limited** (§6.2, §8) — the most expensive read on the pipe is also the
  one most worth a stolen budget.

## 12. Inbounds, subscriptions, and the one place text becomes code

**Loopback is a network surface.** `packages/schema/connector-settings.schema.json#/properties/ports/properties/host`
fixes the bind to `127.0.0.1` and `#/properties/ports/properties/range` bounds the ports; ADR-0006 makes the
compile refuse anything else and `packages/schema/protocol/commands/core.start.json#/result/properties/inboundPorts`
report what actually got bound. The exposure that remains: TCP on loopback is reachable by *any local process*,
including another user's, because neither Windows nor Linux separates loopback per user. Controls, with their
limits stated:

- per-profile inbound credentials where the transport supports authentication (a NEXORA profile authenticates to
  *Core* the way it authenticates to the VPS — this is what makes an unauthenticated local proxy unnecessary);
- ports allocated from a bounded range rather than fixed well-known numbers, so a drive-by process cannot rely on
  one — explicitly *not* the control, only a cost;
- the block port range disjoint from the allocation range (`packages/schema/failure-policy.schema.json#/properties/block/properties/portRange`),
  checked by "the block range and the inbound allocation range are disjoint in the shipped documents", so a
  fail-closed target cannot silently turn into a live inbound when Core restarts into it;
- `assertClosed` defaults to true (`packages/schema/failure-policy.schema.json#/properties/block/properties/assertClosed`, asserted as "assertClosed defaults to true: a
  block that is not proven closed is an inbound").

**Sniffing is off by default** — `packages/schema/routing-policy.schema.json#/properties/corePlane/properties/sniffing`
— because payload inspection is a capability, not a performance feature, and enabling it later is a decision with a
section of its own in `docs/architecture/27-config-compilation-pipeline.md`. **`domainStrategy` default
`IPIfNonMatch`** (`#/properties/corePlane/properties/domainStrategy`) has a DNS consequence — a name that does not
match a domain rule may be resolved locally, which is a leak of a different shape; the analysis lives in
`docs/architecture/28-routing-model.md` and the open half in `docs/architecture/40-open-decisions.md`.

**A subscription is an untrusted input that the Connector fetches.** The limits are data:
`packages/schema/subscription-source.schema.json#/properties/limits` with `maxBytes` capped at
`#/properties/limits/properties/maxBytes`, redirect and timeout bounds, https-only URL, and the loopback/metadata
(SSRF) rejection assigned to the Connector rather than to a schema. The legacy path being replaced is a bare
`fetch(url, options)` with the body read whole at
`omega-target-chromium-extension/src/module/fetch_url.coffee:10-11`, no scheme or host check, and a Content-Type
accept list ending in a catch-all (SEC-2). Chunked reassembly cannot smuggle around the cap — the total is bounded
by the same `limits` (`docs/architecture/25-native-messaging-protocol.md` §5 rule 6).

**Rule text is compiled, not executed** (ADR-0013): a fetched rule list becomes `Conditions`, which become Xray
routing objects that must validate; the compiler's output is the only thing Core sees.

**The one place fetched text still becomes code** is the inherited PAC path: a downloaded script is spliced into
the generated function through an `AST_Raw` node at `omega-pac/src/profiles.coffee:373`, held together by a
four-step comment-termination hack documented at `omega-pac/src/profiles.coffee:363-372`. ADR-0002 keeps it, and
SEC-3 bounds it honestly: the splice executes in the proxy-resolution context, which is not the extension's
authority — it cannot call `chrome.proxy` or read storage. The model's requirement is the *negative* one: NEXORA
MUST NOT widen that context (no injection of fetched text into a page, no `web_accessible_resources`, which both
shipped manifests leave absent per `docs/audit/10-manifest-packaging-distribution.md` §1). `Verified: NO —
deferred to Phase 2` in the sense that the guarantee is today an argument about Chromium's PAC sandbox, not a test.

**A failed refresh must not destroy the last good set** (`docs/architecture/29-subscriptions-and-servers.md` §
failure table, LIVE-12): integrity and availability, stated in the master brief and enforced by the result shape
(`packages/schema/protocol/commands/subscription.refresh.json` splits `refreshed` / `unchanged` / `failed` /
`skipped`, and a `failed` entry carries no deletion).

## 13. Browser-side surface: grants and CSP

Today's grants, from `omega-target-chromium-extension/overlay/manifest.json:39-49` and
`omega-target-chromium-extension/overlay/manifest-firefox.json:38-48`:

| Grant | What it is load-bearing for today | NEXORA target | Gate |
|---|---|---|---|
| `proxy` | the only actuator (`docs/audit/06-proxy-application.md`) | keep | — |
| `storage` | the whole `+`/`-` envelope | keep | — |
| `alarms` | subscription polling (`omega-target-chromium-extension/src/module/options.coffee:293-295`) | keep | it is also how a block gets *declared* (`docs/architecture/30-failure-policy.md` §4) |
| `nativeMessaging` | nothing — absent today | **add** | it is the Connector's only door; ADR-0004 |
| `unlimitedStorage` | rule lists that exceed the sync quota | keep for now | removal is answered by `docs/architecture/34-data-split.md` §9: large bodies want the Connector's disk, not `chrome.storage.local` |
| `webRequest` + `webRequestAuthProvider` (Chromium) | proxy HTTP auth responses only: `onAuthRequired` registered with `{urls: ['<all_urls>']}` and `['blocking']` at `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:19-23`, gated at `:64` by `details.isProxy` | keep for legacy `FixedProfile` auth; NEXORA profiles authenticate inside Core and need none of it | becomes an optional grant when the legacy authenticated-proxy cohort is measured — `docs/architecture/38-compatibility-matrix.md` |
| `webRequestBlocking` (Firefox only, `omega-target-chromium-extension/overlay/manifest-firefox.json:45`) | the same auth path there | same | LIVE-15's parity half |
| `tabs` | per-tab badge, "open in a tab" helpers, and reload-after-switch (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:199-201`) | optional grant | it is UX, not a decision input: nothing about which proxy a request uses depends on it |
| `contextMenus` | the quick-switch menu (`omega-target-chromium-extension/src/coffee/background_preload.coffee:51`) | optional grant | same |
| `host_permissions: ["<all_urls>"]` | subscription/PAC fetching and the `['<all_urls>']` urlFilter above | **remove**, once fetching is Connector-side | this is the largest single reduction available and the riskiest to claim: it holds only while the Connector owns every outbound fetch, and the auth path above still asks for it. The honest order is: move the fetch, measure, then remove |

**CSP.** Neither manifest declares `content_security_policy` — VERIFIED for both templates and INFERRED
repository-wide in `docs/audit/11-security-findings.md` §SEC-3 and `docs/audit/10-manifest-packaging-distribution.md`
§1. Under MV3 the extension pages already get Chromium's isolated default, so "add a CSP" must be described
accurately, because its value differs per browser:

- **In Firefox the default is relaxable**, so an explicit declaration is a real control; declaring
  `script-src 'self'; object-src 'self'` there removes a way to weaken it later.
- **In Chromium MV3 the declaration cannot weaken the default**, so its value is review-visible: an update that
  adds `unsafe-eval` or a remote script source becomes a diff in a manifest rather than a line in a bundle.
- Both are worth doing for the reason that actually matters here: the Angular-era `ng-csp` stance at
  `omega-web/src/options.jade:2` is a UI-layer convention, not a manifest guarantee.
- **The build must not need `unsafe-eval`.** ADR-0016's Svelte 5 choice makes this a property rather than a hope —
  templates compile at build time — and the check that keeps it true is the release assertion in
  `.github/workflows/verify.yml` that the packaged bundle survives the declared CSP. `Verified: NO — deferred to
  Phase 2`.
- The iframe popup (`omega-target-chromium-extension/overlay/x-background.js:26-30`) is same-origin and needs no
  exception; adding `web_accessible_resources` would, and §12 forbids it.

**Permission reduction is a product behaviour change**, which puts it under PROJECT-STATE §2.2: it ships with a
test, not with this document. `Verified: NO — deferred to Phase 2` for every row in the target column above.

## 14. Residual risk, accepted and named

| Position | Why the controls above do not close it | What is true anyway |
|---|---|---|
| Malware running as the user | DPAPI is per-user (§11); the process can also drive the pinned extension through the browser | it cannot gain the *proxy decision* without the browser's own API, and the block state does not depend on the Connector being trusted |
| Another local process using a loopback inbound | TCP on `127.0.0.1` is not per-user (§12) | it sees the transport's own authentication where the profile has one; ports are unpredictable but that is cost, not control |
| The VPS operator | they are the destination: they see destinations, volumes and timings of what the routing sent them | the routing model, not the security model, decides what reaches them — `docs/architecture/28-routing-model.md` |
| A Core vulnerability | Core faces the internet and parses attacker-controlled bytes; a memory-safe language is not a sandbox | pinned, verified, user-initiated upgrade only (ADR-0019, `docs/architecture/33-binary-integrity.md`); no silent self-update |
| A malicious or compromised store update | nothing here is an anti-tamper system; the update path is the store's | the update cannot introduce a new secret-bearing field without failing the schema layer, and the version line is constrained by ADR-0015 |
| DNS and WebRTC around `chrome.proxy` | browser-stack behaviour, not ours; the inherited default bypass list literally contains `127.0.0.1`, `::1`, `localhost` (`omega-target/src/default_options.coffee:15-27`) and is copied into `fixed_servers` (`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:68-72`), and an *empty* FixedProfile degrades to `mode: 'direct'` at `:65-66` | NEXORA's `block` primitive has no bypass list at all (asserted: "the block primitive has no bypass list to leak through"); the browser half is open in `docs/architecture/40-open-decisions.md` |
| A coerced or tricked user | `confirmToken` makes fail-open deliberate, not silent; deliberate is not safe | the notification is mandatory (`packages/schema/failure-policy.schema.json#/properties/coreDeath/properties/notify`, and `silent` is permitted in `packages/schema/connector-settings.schema.json#/properties/proxy/properties/notificationLevel` only for health, never for a block), and the badge is the last thing the user saw |
| The user's own cloud account | sync replicates whatever the envelope holds | secrets are references, so what replicates is inert; SEC-1/SEC-5's structural half is this sentence |
| Physical access / unlocked session | out of scope for any software control | keystore age and retention (`#/properties/telemetry/properties/retentionDays`) limit how much is lying around |
| A bug in our own compiler or UI | §10's depth is bounding, not prevention | rate limits bound the blast rate, `unrepresentable` refuses to lie about routing, fail-closed bounds the failure, and the fixtures replay the protocol byte for byte |

## 15. Threat → control → check

| Threat | Control | Where the control lives | Check today |
|---|---|---|---|
| Any local process commands the Connector | no network control endpoint at all (ADR-0004) | `packages/schema/protocol/framing.json#/nonGoals` | "oversized payloads are chunked, not handed off out-of-band" and the framing sanity block — the *absence* of a control API is not schema-checkable; LIVE-01/LIVE-16 own the runtime half |
| Foreign extension drives the proxy | OS-resolved sender id + startup allowlist + `UnauthorizedExtension` | `packages/schema/protocol/framing.json#/senderPolicy/identity` | "an unknown sender is answered with the sender-id error", "the sender allowlist is declared to live outside browser storage" → `Verified: NO` for the refusal (LIVE-16) |
| Buggy UI hammers a feed / every VPS | per-sender budgets counted before dispatch | `packages/schema/protocol/framing.json#/senderPolicy/rateLimits` | "every privileged method carries a rate budget", "a class budget never exceeds the global floor", "rate limit refusal is Busy" → `Verified: NO` for enforcement (LIVE-17) |
| Sync or logs carry a credential | secrets are references; `secretBearing ⇒ ¬syncEligible` | `packages/schema/envelope-keys.schema.json#/properties/namespaces/items/properties/secretBearing`, `#/properties/syncExcludedValueKeys` | "namespace stays out of the profile prefix", "secret-bearing namespace is not sync-eligible", "syncExcludedValueKeys strips headers" → GREEN for the contract, LIVE-10 for the artefact |
| A fetched body is executed | compile-not-execute + https-only + byte caps + SSRF guard | `packages/schema/subscription-source.schema.json#/properties/limits` | schema validation + the invalid-example set; the fetch itself is `Verified: NO` (Connector) |
| A substituted Core binary | pinned sha256 + `verifySignature` and `silentSelfUpdate` declared as `const` in the schema, not as settings | `packages/schema/pins.schema.json#/properties/verifySignature`, `#/properties/silentSelfUpdate` | the schema compiles and `examples/pins.xray.json` validates against it, while `pins.mirror-url` — a pin pointing at a mirror instead of the upstream release host — is a rejected case; LIVE-02 for the runtime refusal |
| Non-loopback inbound becomes an open proxy | host is a const; `core.start` refuses; ports bounded and reported | `packages/schema/connector-settings.schema.json#/properties/ports/properties/host` | schema const + "the block target is loopback-only (ADR-0006)"; LIVE-03 for the runtime assert |
| A failure resolves to direct/system/clear | `forbiddenOutcomes` pinned element-by-element; block-only planes | `packages/schema/failure-policy.schema.json#/properties/forbiddenOutcomes` | the resolution-vocabulary assertions — GREEN for the contract half, LIVE-11 for the browser half |
| Whole-options read by another extension | the external surface is not carried (§6.5) | — | `Verified: NO` — the surface still exists in the inherited tree |

## 16. Invariants of this document and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| Identity comes from the OS, and the policy says which things are not identity | the sender-policy block in `packages/schema/scripts/verify.mjs` (assertions named in §6) | **GREEN** (contract) |
| The privileged set and the allowlist agree | "senderPolicy.privilegedMethods.names is exactly the allowlist's privileged flag" | **GREEN** |
| Privileged is a proper subset of the surface | "not every method is privileged: …" | **GREEN** |
| Every privileged method is budgeted; every budget names a real method | "every privileged method carries a rate budget", "rate limit names a real method" | **GREEN** |
| A budget refusal is retryable and never a storage error | "rate limit refusal is Busy", "UnauthorizedExtension is not retryable" | **GREEN** |
| No class budget exceeds the global floor | "a class budget never exceeds the global floor" (arithmetic, per row) | **GREEN** |
| Refusals and ports are bounded | "refusals have an amplification guard", "the policy bounds ports per sender" | **GREEN** (contract) / `Verified: NO` (runtime, LIVE-16) |
| Secrets never exist as values in a syncing or logging place | the envelope-keys namespace loop and the sync-strip-set assertion | **GREEN** (contract) / `Verified: NO — LIVE-10` (artefact) |
| The block primitive has nothing that can leak | "the block primitive has no bypass list to leak through", "the block target is loopback-only (ADR-0006)" | **GREEN** (contract) |
| No shell, path or bind address is caller-chosen | per-command schema validation + the `host` const + "command doc conforms to meta-schema" | **GREEN** (contract) / `Verified: NO` (Connector-side, LIVE-03) |
| The shipped manifest grants exactly what is in §13's "today" column | nothing — `check-doc-evidence.mjs` cites the lines but no tool reads the manifest | `Verified: NO — deferred to Phase 2`: a Phase-2 check should diff the packaged manifest against §13 |
| The packaged bundle survives the declared CSP | nothing | `Verified: NO — deferred to Phase 2` (`.github/workflows/verify.yml` step) |
| An unallowlisted sender cannot use a second registration to get around a budget | nothing | `Verified: NO — deferred to Phase 2` (LIVE-16) |

## 17. Amendments, and what this document asks for

Amendments made by this iteration:

1. `packages/schema/protocol/framing.json` gained `senderPolicy` — identity, privileged set, six budget rows,
   refusal amplification, port cap. Previously the promise "rate-limited per sender" existed only in a command
   description and in three documents pointing at this one.
2. `Busy` in `packages/schema/protocol/error-taxonomy.json#/codes` now names the budget case, so a rate refusal and
   a single-flight collision are the same *actionable* answer instead of two different mysteries.
3. `docs/architecture/22-component-contracts.md`, `docs/architecture/26-command-surface.md`,
   `docs/architecture/25-native-messaging-protocol.md` and `docs/architecture/29-subscriptions-and-servers.md`
   all referenced budgets or a pinned-sender refusal that did not exist as data. They now resolve to §6.2 and §8.
4. `docs/audit/04-data-model-and-options-envelope.md` and `docs/audit/05-storage-sync-and-migration.md` cited
   harness line numbers that had drifted; repaired, and the cite-kind rule that prevents the class is now in
   `docs/README.md`.
5. `docs/verification/live-matrix.md` gained `LIVE-16` (sender refusal, amplification guard, no budget reset via a
   second registration) and `LIVE-17` (budget enforcement, retryable `Busy`, no side effect, no command on mount),
   so §6.2, §8, §15 and §16 point at real rows instead of at a promise to add them.

Open, and deliberately not decided here (each goes to `docs/architecture/40-open-decisions.md`):

- whether any external-API surface survives for third-party compatibility (§6.5), under the constraint that
  `getOptions` and the `system`-resolving `disable` cannot;
- removing `<all_urls>`, which is gated on the Connector owning every fetch *and* on the legacy authenticated-proxy
  path no longer needing `webRequest` (§13);
- per-profile inbound credentials for loopback inbounds whose protocol can authenticate (§12);
- the Firefox AMO id as a security principal that cannot rotate (§6.3);
- DNS disposition and WebRTC while blocked, which the browser does not let us control (§14).

Trace: ADR-0002, ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0011, ADR-0012,
ADR-0013, ADR-0014, ADR-0015, ADR-0016, ADR-0019 · Contract:
`packages/schema/protocol/framing.json`, `packages/schema/protocol/command-allowlist.json`,
`packages/schema/protocol/error-taxonomy.json`, `packages/schema/connector-settings.schema.json`,
`packages/schema/envelope-keys.schema.json`, `packages/schema/subscription-source.schema.json`,
`packages/schema/server.schema.json`, `packages/schema/pins.schema.json`,
`packages/schema/failure-policy.schema.json`, `packages/schema/routing-policy.schema.json` · See also:
`docs/audit/11-security-findings.md`, `docs/audit/10-manifest-packaging-distribution.md`,
`docs/architecture/21-system-context.md`, `docs/architecture/25-native-messaging-protocol.md`,
`docs/architecture/26-command-surface.md`, `docs/architecture/29-subscriptions-and-servers.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/32-secrets-keystore-masking.md`,
`docs/architecture/33-binary-integrity.md`.
