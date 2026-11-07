# 22 — Component contracts

> Purpose: one section per component that will exist in Phase 2 — its responsibility, its typed inputs and
> outputs, three to seven **testable** invariants, its characteristic mode of failure and who owns it, its
> dependencies, and its non-goals. The non-goals are not filler: in this design the most common way to build
> this wrong is for a component to make a decision that belongs to another plane or another process.

Status: Accepted. Every invariant below is either satisfied by data that `npm --prefix packages/schema run
verify` checks today, or carries `Verified: NO — deferred to Phase 2` and owns a row in
`docs/architecture/39-verification-matrix.md`.

## 0. Index

| Component | Root (target) | Process | Normative contract |
|---|---|---|---|
| ExtensionShell | `nexora/extension/src/shell/**` | service worker | `docs/architecture/24-options-envelope-compat.md` |
| NmHostShim | `connector/cmd/nexora-host/**` | host shim | `docs/architecture/25-native-messaging-protocol.md` |
| ConnectorService | `connector/cmd/nexora-connector/**` | connector | `docs/architecture/26-command-surface.md` |
| CoreSupervisor | `connector/internal/supervisor/**` | connector (goroutine) | `docs/architecture/30-failure-policy.md` |
| ConfigCompiler | `connector/internal/compile/**` | connector | `docs/architecture/27-config-compilation-pipeline.md` |
| ProfileRegistry | `nexora/extension/src/profiles/**` + legacy `omega-pac` | service worker | `docs/audit/04-data-model-and-options-envelope.md` |
| SubscriptionEngine | `nexora/extension/src/subscriptions/**` | service worker | `docs/architecture/29-subscriptions-and-servers.md` |
| FeedFetcher | `connector/internal/feed/**` | connector | `docs/architecture/29-subscriptions-and-servers.md` §2 |
| HealthProber | `connector/internal/health/**` | connector | `docs/architecture/26-command-surface.md` §3 |
| Keystore | `connector/internal/keystore/**` | connector | `docs/architecture/32-secrets-keystore-masking.md` |
| MigrationRunner | `nexora/extension/src/migrate/**` | service worker (options page) | `docs/architecture/35-schema-versioning-migration.md` |
| DiagnosticsCollector | `connector/internal/diag/**` + `nexora/extension/src/diag/**` | both | `docs/architecture/36-observability.md` |
| NewUiShell | `nexora/extension/src/ui/options/**` | options page | `docs/architecture/37-ui-architecture.md` |
| PopupShell | `nexora/extension/src/ui/popup/**` | popup | `docs/architecture/37-ui-architecture.md` |

Cross-cutting rule for the table: a component may only be reached by another through the artefact named in
its contract column. There is no NEXORA component that reaches into another's data structure.

## 1. ExtensionShell

**Responsibility** — own everything the browser can do and nothing the browser cannot: the options envelope,
the profile registry façade, the `chrome.proxy.settings` writer, event fan-out to the UI, and the single
ConnectorClient connection with its reconnect loop.

**In** — user actions arriving as typed messages from the UI (`docs/architecture/37-ui-architecture.md` §3);
browser events (`chrome.proxy`, `chrome.runtime.onStartup`, `chrome.tabs`, the request listener); frames from
the Connector (events, responses).
**Out** — `chrome.proxy.settings.set/clear` calls; Native Messaging frames; the storage write queue; UI state
snapshots.

**Invariants**
1. Only ExtensionShell writes `chrome.proxy.settings`, and every write carries a reason tag recorded in the
   diagnostics ring. No other component may call that API — this is what makes `LIVE-11` greppable.
2. A frame is never awaited longer than the deadline for its method
   (`packages/schema/protocol/commands.schema.json#/properties/timeoutMs`): the deadline belongs to the
   extension, so a hung host cannot hang the worker (`docs/architecture/25-native-messaging-protocol.md` §6).
3. On worker restart the shell reconciles rather than reconfigures: it compares the applied config hash from
   `config.snapshot` with its own record before issuing any `config.apply`
   (`packages/schema/protocol/commands/config.snapshot.json#/result`).
4. It never stores a secret. Secret fields are `keystore://` references at every layer
   (`packages/schema/server.schema.json#/properties`), so a leak of `chrome.storage` cannot produce a credential.
5. It does not implement transport protocols. Its proxy vocabulary is `http`/`socks5`/`direct`/`system` only
   (ADR-0003).

**Mode of failure** — the worker is evicted mid-command; the answer is the port-dropped-is-not-shutdown rule of
`packages/schema/protocol/framing.json#/liveness`. **Owner**: the extension.
**Dependencies**: ProfileRegistry, SubscriptionEngine, MigrationRunner, DiagnosticsCollector, `packages/state`.
**Non-goals**: no routing decision outside `Conditions`; no health probing; no config compilation.

## 2. NmHostShim (`nexora-host`)

**Responsibility** — be the only thing the browser can reach: frame codec, sender allowlist enforcement,
spawn-or-adopt of the Connector, byte pump between the two pipes.

**In** — Native Messaging port from the browser; the OS-level sender identity.
**Out** — frames on the local pipe to the Connector, verbatim (no translation, no re-serialization of payloads).

**Invariants**
1. It rejects a frame larger than `packages/schema/protocol/framing.json#/limits/extensionToHostBytes`
   **before** writing, not after reading — the enforcement field says so, and a receiver-side truncation is a
   silent corruption.
2. A sender not on the allowlist produces exactly one frame back: `UnauthorizedExtension`, and no state
   mutation (ADR-0004 rule 6).
3. It holds no domain knowledge: `connector/cmd/nexora-host` MUST NOT import the config compiler, the keystore
   or the command handlers. Testable as an import-graph assertion; `Verified: NO — deferred to Phase 2`.
4. It is replaceable by an extension update while the Connector keeps running — hence the pipe is versioned
   separately from the browser protocol (`docs/architecture/21-system-context.md` §6).
5. Its pinnable identity is the manifest it writes (`docs/architecture/33-binary-integrity.md`), not a build
   artefact inside the extension zip.

**Mode of failure** — killed by the browser when the port closes; the Connector must not notice. **Owner**: the
Connector installer owns its registration; the extension owns which ID it may talk to.
**Dependencies**: `packages/protocol` (frame codec, shared with the extension side), the OS pipes.
**Non-goals**: no authentication logic beyond sender identity, no retries, no queueing, no logging of payloads.

## 3. ConnectorService

**Responsibility** — the nineteen-method dispatcher: validate against the command doc, enforce hello gating and
privilege class, execute, emit events, and be the only process that talks to Core and the keystore.

**In** — envelope frames from the shim; `core.start`/`config.apply`/… typed args.
**Out** — response/error/event/progress frames; the compiled config handed to Core; keystore reads.

**Invariants**
1. An unknown method is `UnknownMethod`, with no fallthrough and no partial execution — the allowlist is a
   closed set (`packages/schema/protocol/command-allowlist.json`).
2. Every method except `connector.hello` refuses to run before a successful hello; this is checked in both
   directions between the allowlist and the command docs (the "hello gating agrees" assertion in
   `packages/schema/scripts/verify.mjs`).
3. The client's frame `id` is the idempotency key inside a dedupe window: same id ⇒ same side effect, two
   identical answers (ADR-0008). `Verified: NO — deferred to Phase 2` for the window itself; the replay pairs in
   `packages/schema/fixtures/protocol/` prove the observable half.
4. `config.apply` restarts Core only when the compiled hash changes
   (`packages/schema/protocol/commands/config.apply.json#/result/properties/unchangedBecauseHashMatched`).
5. It never returns an unmasked secret in any frame, including `diagnostics.get`
   (`docs/architecture/32-secrets-keystore-masking.md` §sink rule).
6. It refuses to start Core if any inbound would bind non-loopback (ADR-0006), answering
   `UnsupportedCombination` — `Verified: NO — deferred to Phase 2`, and flagged as a missing error-code
   decision in `docs/architecture/40-open-decisions.md`.

**Mode of failure** — crash ⇒ Core dies with it by containment; the extension's fail-closed policy takes over
(`docs/architecture/30-failure-policy.md`). **Owner**: the Go module.
**Dependencies**: CoreSupervisor, ConfigCompiler, Keystore, HealthProber, DiagnosticsCollector.
**Non-goals**: no UI state, no user-visible source of truth except the applied config hash and the keystore
(ADR-0010), no browser API knowledge.

## 4. CoreSupervisor

**Responsibility** — own the Xray child process: spawn with the compiled config, containment handle, health of
the process itself (not of the servers), restart with backoff, and orderly stop.

**In** — `core.start`/`core.stop`/`core.restart` intents, a config file path and its hash, the containment
primitive for the OS.
**Out** — `core.started` / `core.exited` events, PID + `inboundPorts` in `core.start`'s result, restart
accounting.

**Invariants**
1. Core is in the same containment scope as the Connector from the instant of spawn — never adopted afterwards.
   This is the one invariant the Go standard library will not hand over on Windows: `os/exec` carries no
   job-list attribute in `SysProcAttr`, so a dependency-free Connector can only assign the child after its pid
   exists, which is a window rather than a guarantee. `connector/internal/supervisor` takes that window, reports
   the assignment's result instead of assuming it, and names the closing of that window (`STARTUPINFOEX` with
   `PROC_THREAD_ATTRIBUTE_JOB_LIST`, or a launcher this module does not have) as platform work under ADR-0011
   rather than something this invariant is allowed to accept.
2. `core.start` on a running Core with the same config hash is a successful no-op returning the same PID
   (`packages/schema/protocol/commands/core.start.json#/result/properties/alreadyRunning`).
3. Exits are classified (`exit code`, `signal`, `config rejected`, `port conflict`) and the classification is
   what selects the failure policy, not a generic "process died".
4. Restart attempts obey `packages/schema/failure-policy.schema.json#/properties/coreDeath`; when they are
   exhausted, the outcome is the block action, never a silent give-up.
5. It never writes the config file into the extension's directory, and never reads user data besides the
   Connector's own store.

**Mode of failure** — an unbootable Core (bad binary, AV quarantine) is an install problem, surfaced as
`IntegrityMismatch` or a health-check failure and routed to `docs/architecture/33-binary-integrity.md`.
**Owner**: the Go module.
**Dependencies**: the pinned Xray asset, the OS containment API, ConfigCompiler for the file it hands over.
**Non-goals**: does not know what a profile is; does not decide routing; does not report per-server health.

## 5. ConfigCompiler

**Responsibility** — deterministic translation of intent (profiles + servers + policies) into an Xray JSON
config, plus the honest report of what could not be translated.

**In** — the extension's desired state: `ConnectorProfile[]`, `Server[]`, `RoutingPolicy`, per-profile failure
policy, port allocation plan.
**Out** — a config document (bytes), its sha256, the `inboundPorts` table, and `unrepresentable[]`.

**Invariants**
1. Byte-for-byte determinism: same inputs ⇒ identical bytes (sorted keys, no timestamps, no map iteration
   order). Golden fixtures are the test (`docs/architecture/27-config-compilation-pipeline.md` §5).
2. Every emitted inbound listens on `127.0.0.1` or `::1` and nothing else; the check is at compile time, so
   `core.start` never has to discover it later.
3. It rejects illegal transport combinations rather than emitting them: `flow: xtls-rprx-vision` only with
   `network: tcp` + `security: tls`; `reality` never with a `privateKey`
   (`packages/schema/examples/invalid/server.vision-over-ws.json`,
   `packages/schema/examples/invalid/server.reality-with-privatekey.json`).
4. A rule it cannot express is reported in `unrepresentable[]` with `source` and `reason`; dropping silently is
   forbidden by ADR-0013.
5. It never invents an outbound tag: server tags come from `Server.id` derivation
   (`packages/schema/server.schema.json#/properties/id`), so a compiled config cannot reference a server that
   was not supplied.
6. It does not evaluate a request. See non-goals.

**Mode of failure** — a config Core refuses is `ConfigApplyFailed` with the previous good config still applied;
the compiler must be unable to half-write (`docs/architecture/27-config-compilation-pipeline.md` §atomicity).
**Owner**: the Go module.
**Dependencies**: `packages/schema` entity shapes, the port allocator.
**Non-goals**: **not** the place where a request's route is decided; not a policy engine for the browser plane;
not a validator of user credentials; not allowed to reformat a config it received intact.

## 6. ProfileRegistry

**Responsibility** — the compatibility boundary of the whole project: hold OmegaPac profiles under the `+`
prefix, hold NEXORA entities under `-nexora.*`, and never let the two namespaces be confused.

**In** — the flat options envelope; profile CRUD from the UI; `Profiles.each` iteration from legacy code
(`omega-pac/src/profiles.coffee:111-118`).
**Out** — profile objects for `Conditions`/PAC compilation; the desired-state document handed to the compiler.

**Invariants**
1. A `-nexora.*` key is never iterated as a profile, therefore never appears in the quick-switch list, rule
   pickers, or generated PAC — asserted for every registered namespace by the "namespace stays out of the
   profile prefix" assertion in `packages/schema/scripts/verify.mjs`, reading
   `packages/schema/examples/envelope-keys.nexora.json`.
2. `ConnectorProfile` is registered as a real OmegaPac profile type whose `compile` emits
   `PROXY/SOCKS5 127.0.0.1:<port>` — that is the bridge that keeps existing `SwitchProfile` rules working
   (ADR-0013, `docs/architecture/24-options-envelope-compat.md` §5).
3. Servers are referenced by id, never embedded: a profile's `serverIds` must all resolve, and duplicates are
   rejected (`packages/schema/connector-profile.schema.json#/properties/serverIds`,
   `packages/schema/examples/invalid/profile.no-servers.json`).
4. JSON Schema cannot express the cross-entity referential check (a dangling `serverId`), so the registry
   performs it at load and refuses the profile with a named reason; `Verified: NO — deferred to Phase 2` and
   owned by `docs/architecture/38-compatibility-matrix.md` plus a live row.
5. Deleting a profile that a rule still references is either refused or rewired, never silent —
   `Profiles.replaceRef`/`referencedBySet` semantics stay as legacy implemented them.
6. It does not touch storage directly; it is given a `Storage` (`docs/audit/05-storage-sync-and-migration.md`).

**Mode of failure** — envelope drift (a key written by an older build) ⇒ the migration runner, not the
registry, decides. **Owner**: the extension.
**Dependencies**: legacy `omega-pac` dispatcher (read-only), `packages/state` for typed views.
**Non-goals**: no proxy writes, no network, no secret handling.

## 7. SubscriptionEngine

**Responsibility** — own the *intent* side of subscriptions: the `SubscriptionSource` documents in the envelope,
the alarm that decides when a refresh is due, the write-back of the Servers the Connector produced, the durable
status the legacy code never stored, and the events the UI reads. The *egress* side — fetching the URL, guarding
the response, detecting and parsing the format, and putting the feed's credentials into the keystore — belongs to
`connector/internal/feed/**`, and the split is not a preference: the allowlist has no `keystore.put`, so the only
process that can write a secret is the one that has to do the parsing
(`docs/architecture/29-subscriptions-and-servers.md` §2).

**In** — `SubscriptionSource` documents and their `status`, the Connector's `subscription.refresh` result
(`packages/schema/protocol/commands/subscription.refresh.json#/result`), alarm fires.
**Out** — `subscription.refresh` frames carrying the source documents; `Server` entities written under
`-nexora.server.<id>`; the updated `-nexora.subscription.<id>` status; `subscription.updated` /
`subscription.failed` rendering for the UI with `{lastSuccessAt, lastError, consecutiveFailures, nextAttemptAt}`.

**Invariants**
1. A failed refresh never deletes the last good version — not on non-2xx, not on a parse error, not on a
   format change (`packages/schema/subscription-source.schema.json#/properties/keepLastGood`, and the
   `examples/invalid/subscription.keep-last-good-false.json` case that forbids configuring it away).
2. Guards run before parsing — max bytes, content-type, redirect count, timeout, address without userinfo — and
   they run in the Connector, where the bytes arrive: `limits` travels with the source document
   (`packages/schema/subscription-source.schema.json#/properties/limits`,
   `packages/schema/examples/invalid/subscription.oversized-limit.json`,
   `packages/schema/examples/invalid/subscription.http-url.json`,
   `packages/schema/examples/invalid/subscription.url-userinfo.json`).
3. The engine never performs the fetch and never sees a feed body: a body full of user UUIDs that entered the
   service worker would have to be re-sent over the pipe to reach the keystore, and the only place it could wait
   is `chrome.storage` (`docs/architecture/29-subscriptions-and-servers.md` §2, ADR-0010).
4. Content-addressed delivery: the Connector reports the sha256 of the body it received, and the extension stores
   it as `status.lastSha256` — which is what makes the next call's `unchanged` verdict answerable without the
   Connector remembering anything (`packages/schema/protocol/commands/subscription.refresh.json#/result`).
5. Lossy-format handling is explicit: every field a source format cannot carry arrives in `lossyFields` and is
   reported or asked of the user (`docs/architecture/29-subscriptions-and-servers.md` §5). A silent default for
   `flow` or `security` is a connection that mysteriously fails.
6. `404 ≠ delete`: removal of a subscription is a user action, never a fetch outcome.
7. Identity is reused, not regenerated: a server this source already produced keeps its `id`, so health history,
   `core.select` and the derived `srv-<id8>` tag survive a refresh (§identity of doc 29).

**Mode of failure** — a hostile endpoint returning 80 MB or a Clash YAML where a VLESS list was expected ⇒ size
guard or format detect rejects before any parsing, and the previous state is untouched.
**Owner**: the extension; the fetch half is owned by the Go module.
**Dependencies**: ProfileRegistry, ConnectorClient, `packages/schema` formats, the alarm scheduler.
**Non-goals**: no HTTP, no transport parsing, no health (HealthProber owns that), no credential storage.

## 7.1 FeedFetcher (`connector/internal/feed`)

**Responsibility** — be the only thing in NEXORA that fetches a subscription URL: resolve, guard, read under a
byte ceiling, detect the format, parse it into Servers, move every credential into the keystore, and return
reference-shaped entities.

**In** — the `SubscriptionSource` documents inside `subscription.refresh` args, the resolved keystore entries
behind `urlRef` and `headersRef`.
**Out** — the `subscription.refresh` result: `{sha256, serverCount, added, removed, duplicates, lossyFields,
servers[]}` or a `failed[]` entry with `{code, retryable, consecutiveFailures, nextAttemptAt}`.

**Invariants**
1. It resolves the address, checks every returned IP against the loopback / link-local / metadata ranges and then
   dials **that** address, so a DNS reply cannot change between the check and the connection
   (`docs/architecture/29-subscriptions-and-servers.md` §3, SEC-2).
2. It stops reading at `limits.maxBytes` rather than buffering first, because a size guard applied after
   `response.text()` is not a guard (`docs/audit/09-subscriptions-import-export-sync.md` M3).
3. It counts redirects against `limits.maxRedirects` and re-runs guard 1 for each hop — a redirect is how an
   allowed host reaches `169.254.169.254`.
4. It parses into the shipped `Server` schema and no other shape: an entry that will not validate is reported as
   a rejection and never kept in a private intermediate form, because a second in-memory server shape is how the
   two copies start to disagree (`packages/schema/server.schema.json`).
5. It emits Servers whose every credential field is a `keystore://` reference — `server.schema.json` cannot spell
   a value, so an un-extracted secret fails validation instead of shipping (`LIVE-10` for the grep, `LIVE-18` for the write that makes it resolve).
6. It keeps no subscription state: nothing is written to disk except keystore entries and the log.

**Mode of failure** — `NetworkUnreachable`, `Timeout`, `AuthFailed`, `SchemaMismatch`, `QuotaExceeded` per source,
each carrying `retryable` and the new `consecutiveFailures`.
**Owner**: the Go module.
**Dependencies**: Keystore, `packages/schema` formats, the OS resolver.
**Non-goals**: no envelope writes, no scheduling, no config compilation, no health probing.

## 8. HealthProber

**Responsibility** — measure, per server tag, whether egress through Core actually works, and publish that as
an event plus a retained status.

**In** — server tag, probe definition (`packages/schema/protocol/commands/server.test.json#/args`), interval
and cooldown from `packages/schema/connector-profile.schema.json#/properties/failover`.
**Out** — `server.health.changed` events, `server.test` responses, failover decisions fed back to Core via
`core.select` or the Core's own load-balance policy.

**Invariants**
1. A probe asserts **egress through Core**, not reachability of the VPS: a probe that would also succeed with
   the system proxy off is measuring the wrong thing (ADR-0020; `LIVE-03`).
2. Probe results are non-replayable and non-cached: enforced by the "probe is non-replayable" and
   "probe is answered exactly once" assertions over `packages/schema/fixtures/protocol/`, and by
   `packages/schema/protocol/commands/server.test.json#/class` being `probe`.
3. Consecutive failures are counted and the count, not a single timeout, drives failover
   (`packages/schema/connector-profile.schema.json#/properties/failover/properties/maxConsecutiveFailures`).
4. Probe rate is bounded independently of the UI: `docs/architecture/31-security-model.md` rate-limit row, so a
   stuck options tab cannot hammer every VPS.
5. A probe never reveals a secret in its result or logs — masked at the sink
   (`docs/architecture/32-secrets-keystore-masking.md`).

**Mode of failure** — a probe endpoint that is itself down (the "http://connectivity check" problem) is reported
as `NetworkUnreachable` with the target named, not as "server is dead". **Owner**: the Go module.
**Dependencies**: CoreSupervisor (for the port to hit), DiagnosticsCollector.
**Non-goals**: does not decide selection order for the user; does not persist history beyond the retention ring;
does not run in the browser.
**Status (2026-11-07)** — the prober and its command surface are built and tested (`connector/internal/health`, 38 tests over a loopback SOCKS5 core; the `server.test` surface beside `config.apply` in `connector/cmd/nexora-connector`), and registration in the front door is **Verified: NO — deferred to Phase 2**: the prober refuses to build without the egress endpoint and expected address that no schema field carries, and the surface reads a server store no document has placed — `docs/architecture/40-open-decisions.md` owns both questions.

One more gap sits beside registration: the store row names the retained status `health-history` and gives it `medium` `disk` — its bound is the sentence about a user who cannot fill a disk by probing more — while the ring this build keeps lives behind the prober's mutex and dies with the process. **Verified: NO — deferred to Phase 2**, queued as `VER-86` and closed by the restart test that re-reads the ring from whatever path the placement decisions land on.

## 9. Keystore

**Responsibility** — be the only place a credential value exists at rest, and the only thing allowed to
materialize one for Core.

**In** — `{id, kind, secret}` writes from the options UI over `profile.import`/config apply; reads by
CoreSupervisor and ConfigCompiler at the moment of use.
**Out** — `keystore://<ref>` handles in every other layer; masked renderings for diagnostics.

**Invariants**
1. Windows backend is DPAPI-protected per-user files (`docs/architecture/21-system-context.md` §11 explains
   why not a service); Linux/macOS backends are declared as deferred, not silently implemented with a
   self-invented cipher (ADR-0011).
2. A secret never appears in `chrome.storage.sync`, never in a Gist/WebDAV transit, never in a log line,
   never in a `traceId` — the `secretBearing ⇒ ¬syncEligible` assertion in
   `packages/schema/scripts/verify.mjs` and the `syncExcludedValueKeys` list that strips `headers`
   (SEC-1 closure, `docs/audit/11-security-findings.md`).
3. `initWithOptions()` in legacy code deletes storage (`omega-target/src/options.coffee:113-121`) — the keystore
   must not be reachable from that path, so a version-compare wipe cannot orphan credentials
   (`docs/architecture/35-schema-versioning-migration.md` §14).
4. Reinstalling the extension does not lose the keystore; removing the Connector leaves the profiles intact and
   degrades to "NEXORA Lite" (`docs/architecture/34-data-split.md` §11).
5. `keystore.purge` is privileged, requires a confirm token, and is the only way a secret is destroyed
   (`packages/schema/protocol/commands/keystore.purge.json`).

**Mode of failure** — `KeystoreUnavailable` (locked profile, missing entropy) ⇒ Core does not start; the failure
policy applies. **Owner**: the Go module.
**Dependencies**: OS APIs only.
**Non-goals**: no crypto of our own, no cloud sync of secrets, no password to unlock (a future ADR if ever), no
secret storage for the browser plane.

## 10. MigrationRunner

**Responsibility** — move the envelope from `schemaVersion 2` to `3` (and any later step) without ever being
the thing that destroys user data.

**In** — the whole options envelope, the backup snapshot, an ordered list of steps.
**Out** — a migrated envelope, a verified backup reference, a per-step report.

**Invariants**
1. Backup-before-migrate is blocking: a full snapshot exists in `chrome.storage.local` under
   `-nexora.backup.<iso>` **and** as a Connector-side file before any destructive step, and the restore path is
   executed and compared, not assumed
   (`packages/schema/envelope-backup.schema.json`, `packages/schema/examples/envelope-backup.pre-migration.json`).
2. Steps are ordered, idempotent, resumable, `dryRun`-able, with per-step `up` and `verify`; no step deletes
   before its replacement is written.
3. Wipe-by-version-compare is forbidden in NEXORA code: the legacy trap at
   `omega-target-chromium-extension/src/coffee/background.coffee:32-46` stays as legacy behaviour for legacy
   installs and is unreachable from any migration path (ADR-0014, ADR-0015).
4. Firefox and Chrome run the same migration with the same acceptance, and the parity assertion is per step.
5. Failure at step *n* leaves the envelope at *n-1* plus a visible diagnostic — never a half-migrated state.

**Mode of failure** — an aborted migration mid-step, i.e. a worker eviction during the run: the runner must be
resumable and idempotent, which is why `dryRun` is a first-class mode. **Owner**: the extension (options page).
**Dependencies**: ProfileRegistry, Storage, Keystore (to prove the keystore still matches the envelope).
**Non-goals**: does not repair a corrupt backup, does not import legacy SwitchyOmega formats (that is the
compatibility matrix), does not decide that a migration is needed (the shell's startup check does).

## 11. DiagnosticsCollector

**Responsibility** — make "why is my traffic not going through Xray right now?" answerable with one dump, with
no secrets in it.

**In** — events from both sides, ring buffers, the applied config hash, the failure-policy decisions taken.
**Out** — `diagnostics.get` responses (bounded), UI panels, export-to-file via the options page.

**Invariants**
1. Size-bounded and ring-buffered: a user cannot fill a disk with a log, and a stale buffer cannot be mistaken
   for current state (`packages/schema/protocol/commands/diagnostics.get.json#/result`).
2. Masking happens at the sink, not at the call site (`docs/architecture/32-secrets-keystore-masking.md` §8),
   so a forgotten call site cannot leak.
3. Every `chrome.proxy.settings` write appears with its reason tag — the audit trail for the fail-closed rule.
4. Nothing is uploaded anywhere; export is a user action.
5. The catalogue of event names is one registry (`packages/schema/events.schema.json`), and both sides must use
   names from it — asserted by the "cites only registered events" assertion in
   `packages/schema/scripts/verify.mjs`.

**Mode of failure** — diagnostics unavailable (buffer corrupt) must never be fatal to the proxy path.
**Owner**: both sides, one schema.
**Dependencies**: the event catalogue, Keystore for masking.
**Non-goals**: not a log shipper, not a metrics pipeline, not a profiler.

## 12. NewUiShell (options)

**Responsibility** — Svelte 5 islands on the options page: profile editing, server/subscription management,
routing rule editor, health display, the Connector/keystore status panel, and the migration review screen.

**In** — typed stores from `packages/state`; user input.
**Out** — commands (never raw frames) through ExtensionShell; route changes; i18n keys.

**Invariants**
1. No button implies a feature that does not exist: any control whose backend is Phase 2 or later is rendered
   disabled with a reason string. This is a master-prompt constraint, so it is stated as an invariant and is
   reviewable per screen. `Verified: NO — deferred to Phase 2`.
2. It cannot reach the Connector directly: UI → shell → one ConnectorClient. Testable by the import boundary
   (`docs/architecture/20-target-repository-layout.md` §7).
3. All copy comes from the compiled catalog (`docs/architecture/37-ui-architecture.md` §7), and all colour
   from the theme tokens in `packages/ui`, including the 164 base16 + 3 default contract.
4. Secret entry fields never round-trip through a store: they go to `profile.import`/keystore and come back as a
   masked reference.
5. Destructive actions require explicit confirmation carrying a token that the command itself validates
   (`keystore.purge`, per-profile `failOpen`).

**Mode of failure** — the shell is disconnected: the UI shows the last known state with an explicit staleness
marker, never a silently stale "connected". **Owner**: `nexora/extension`.
**Dependencies**: `packages/state`, `packages/ui`, `packages/i18n`.
**Non-goals**: no routing decisions, no polling of the Connector, no direct storage access, no Happ-style UI
(master-prompt constraint).

## 13. PopupShell

**Responsibility** — the two-second surface: current profile, quick switch, health dot, one tap to options.

**In** — the shell's snapshot (profiles, applied state, health), keyboard commands
(`omega-target-chromium-extension/overlay/manifest.json:36-38`).
**Out** — profile selection, `core.select`, navigation to options.

**Invariants**
1. It works when the Connector is absent: with no Core, the popup still switches legacy profiles, and shows
   why NEXORA profiles are unavailable (`docs/architecture/38-compatibility-matrix.md`).
2. Opening the popup must not start a probe or a refresh: no command with a side effect on mount
   (`docs/architecture/31-security-model.md` rate-limit row).
3. Selection changes are converge-to-desired-state and idempotent — a double tap is not two switches.
4. The popup is the legacy `popup-iframe.html` boundary (`omega-target-chromium-extension/overlay/x-background.js:26`):
   whichever document it points at, the shell owns the state, the popup is a view.

**Mode of failure** — the popup closes mid-command: the shell completes it, because the popup is not the
executor. **Owner**: `nexora/extension`.
**Dependencies**: `packages/ui`, `packages/state`.
**Non-goals**: no editing, no diagnostics, no subscription management, no secret entry.

## 14. What is deliberately absent from this document

No component here owns a transport protocol (ADR-0003). No component owns "the routing decision at request
time" except `Conditions` in the browser plane and Xray `routing` in the transport plane
(`docs/architecture/28-routing-model.md`). There is no "ConnectorClientLibrary used by the UI", no plugin system,
no generic RPC bridge, and no component that both stores user intent and executes it — the split of ADR-0010 is
the reason those are absent, and each absence is a boundary this document protects.

Trace: ADR-0003, ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0008, ADR-0010, ADR-0011, ADR-0012, ADR-0013,
ADR-0014, ADR-0016, ADR-0020 · Contract: every artefact in the §0 table's contract column ·
See also: `docs/architecture/21-system-context.md`, `docs/architecture/26-command-surface.md`,
`docs/architecture/34-data-split.md`, `docs/audit/03-runtime-layering.md`,
`docs/audit/04-data-model-and-options-envelope.md`.
