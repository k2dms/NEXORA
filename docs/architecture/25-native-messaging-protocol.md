# 25 — Native Messaging protocol

> Purpose: the wire contract between the extension and the Connector — framing, byte limits, the envelope,
> oversized-payload handling, negotiation, error taxonomy, deadlines, ordering, progress and liveness. Nothing
> here is a preference: each rule exists because the alternative produces a wrong proxy configuration or a
> silent leak.

Status: Accepted. Normative data: `packages/schema/protocol/framing.json`,
`packages/schema/protocol/envelope.schema.json`, `packages/schema/protocol/error-taxonomy.json`,
`packages/schema/protocol/compatibility-rules.json`, `packages/schema/protocol/commands/*.json`.
Replayed as frames by `packages/schema/fixtures/protocol/*.jsonl`.

## 1. Reading rules

As in `docs/architecture/20-target-repository-layout.md` §1.

## 2. Topology: two hops, one envelope

`extension ⇄ nexora-host ⇄ nexora-connector` (`docs/architecture/21-system-context.md` §5–§6). The same envelope
object crosses both hops: the shim does not translate, re-serialize, or interpret payloads — it enforces the
sender allowlist and the byte limits, then pumps. Consequence: a bug in the codec is a bug on both sides, which
is why the codec is one artefact in `packages/protocol/` compiled for TypeScript and mirrored by a Go test
reading the same fixtures (`docs/architecture/20-target-repository-layout.md` §3).

**The one thing §2 does not decide.** §2 is exact about the frame and silent about the sender. ADR-0005 rule 2
(`docs/adr/0005-two-process-connector.md:30-32`) requires the two processes to communicate over a local pipe "whose
message schema is versioned **separately** from the extension-facing protocol", carrying "its own version field" and
refusing a mismatch "rather than guess" — and no file in `packages/schema/` defines that schema or that field.
`packages/schema/protocol/framing.json` says as much about itself: its `#/description` scopes it to "the
extension-to-host pipe", and `#/senderPolicy` is written from that hop as well — its identity facts are an OS event on
the browser side, while its budgets are spent by a sender only that hop can name and are counted in the Connector
(`docs/architecture/31-security-model.md` §8). That is the sharper form of the same silence: the process that spends a
sender's budget is not the process that can see which sender it is spending.
The pipe's version *number* is declared now: `minPipeVersion` in `packages/schema/protocol/framing.json` closes `VER-18`
and answers `ODEC-02` (2026-11-06). A comparison is written too — `connector/internal/envelope`'s `Parse` refuses a
frame whose `v` is below `contract.MinPipeVersion`, and `TestThePipeFloorComparisonCompilesAndRuns` pins that the
branch runs — but the comparison cannot fire yet: `packages/schema/protocol/envelope.schema.json#/properties/v`
starts at the same 1 the floor carries, so every schema-valid frame is at or above the floor, and a frame below it is
already invalid on shape. What the front door still owes is the *shape* of that refusal, recorded as §13's floor row
and queued as `VER-89`:
`#/versionFloors/tooOld` names `VersionMismatch` for both floors, and §7 makes the mismatch a visible reason, while an
invalid envelope today ends the stream and answers nothing. The other half of the same silence
is still queued: **how the sender identity crosses hop 2, and what hop 2's message schema is at all.**

Four statements this corpus already makes close on each other there. (i) §2 above: the envelope crosses untranslated,
and the process that must know the sender is the connector — `connector/internal/dispatch` takes an
`Identity{SenderID, Allowlisted, Negotiated, Capabilities}` on every `Dispatch` call and its own comment states
"None of it comes from the payload", while `connector/internal/hello`'s `Session.Identity(senderID string,
allowlisted bool)` can only be called with a value someone resolved. (ii) The frame cannot carry it:
`packages/schema/protocol/envelope.schema.json#/additionalProperties` is `false` over fourteen named properties —
`v`, `id`, `type`, `method`, `args`, `result`, `error`, `inReplyTo`, `ok`, `traceId`, `sentAt`, `seq`, `progress`,
`chunk` — none of them a sender, and ADR-0004 rule 3
(`docs/adr/0004-native-messaging-only-no-loopback-http-control.md:32-35`) forbids a payload field from *becoming*
identity even if the object were opened. (iii) The shim may not add one either:
`docs/architecture/22-component-contracts.md` §2's Out is frames on the local pipe "verbatim (no translation, no
re-serialization of payloads)", and a field the shim injects is a translation. (iv)
`docs/architecture/31-security-model.md` §4 names this crossing's trust mechanism as "the inherited pipe and the
per-user lock file", and `docs/architecture/21-system-context.md` §11 records that lock's fields as
`{pid, pipeVersion, startedAt, configHash}` — no sender id among them — while
`packages/schema/protocol/framing.json#/senderPolicy/identity/resolvedFrom` resolves the id from "the Native
Messaging manifest registration the OS hands the host when the port opens": a hop-1 event, whose hop-2 counterpart is
the host process's own credentials, which name the shim and no extension. ADR-0004 priced exactly that shape when it
rejected a self-framed named pipe — there the sender identity becomes "whoever can open the pipe"
(`docs/adr/0004-native-messaging-only-no-loopback-http-control.md:74-76`).

The candidates are four, and the data removes one. (a) One pipe instance per sender, so identity is *which pipe* the
host opened: honest about who speaks, but it needs the pipe's naming and creation contract in `packages/schema`, and
it turns `#/senderPolicy/rateLimitFacts/scopeOfBudget`'s "per sender id" into a per-pipe count wherever one sender
holds more than one pipe. (b) An out-of-band startup handshake on the pipe, before the first envelope, carrying
`{senderId, allowlisted, pipeVersion}`: ADR-0005's "versioned separately" permits the shape, but it puts a second
protocol on one pipe — the objection §5 raises against the temp-file handoff, in its own words "gives the pipe two
protocols instead of one" — and it is a message no envelope contract covers. (c) OS peer credentials on hop 2
(`SO_PEERCRED`, the named-pipe client pid) mapped to the host process the connector was started by: authentic, but it
authenticates the *host*, so it still needs (a) or (b) for the sender id. (d) A sender field in the envelope:
**excluded, not open** — forbidden twice over, by ADR-0004 rule 3 and by the closed object above, and recorded here
the way `packages/schema/protocol/framing.json#/oversizedPayloads/alternativesRejected` records the handoff it also
refuses. (a), (b) and (c) survive and disagree about where identity lives, so nothing can drive the front door as a
real process until one is chosen. Answered 2026-11-07 by ADR-0022: one pipe instance per sender, the same envelope on both hops guarded by the pipe floor, no second protocol and no sender field.

**Hop 1's half of that answer is measured, not decided.**
`packages/schema/protocol/framing.json#/senderPolicy/identity/resolvedFrom` names "the Native Messaging manifest
registration the OS hands the host when the port opens", and what hands it over is the argv the browser spawns this
process with, per family: Chrome and the Chromium family pass the caller's origin (`chrome-extension://<id>`) as the
first argument — plus `--parent-window=<handle>` beside it on Windows — and Firefox passes the path of the app
manifest it launched us from, then the add-on id from `browser_specific_settings`. The extension chooses neither,
because it cannot start the host at all, and the argument is this process's own command line, not the *peer's* that
`#/senderPolicy/identity/notIdentity` excludes. `connector/cmd/nexora-host` resolves the sender that way and treats
anything it cannot read as one of those shapes as no sender at all, which is the refuse path rather than a guess. What
the argv cannot prove is who spawned this process: ADR-0022 rule 3's peer credentials are the layer that would, and
they are not wired.

### 2.1 What the registration is, and what each browser caps

The host's name is contract data, not one process's constant:
`packages/schema/protocol/framing.json#/senderPolicy/identity/hostName` carries `com.nexora.connector` and
`#/senderPolicy/identity/hostNameRule` states the shape both families accept — lowercase alphanumerics, underscores
and dots, no leading, trailing or doubled dot. The extension types that string into `runtime.connectNative`, the
installer writes the registration under it, and the manifest file is named after it, so a rename is one data change
with three places that must move together — which is why no place may hold it as a private constant.

Where the registration lives is per family and per OS; the installer writes the Windows column and refuses the rest (`ODEC-27`):

| Family | Windows | macOS | Linux |
|---|---|---|---|
| Chrome | key `HKCU\SOFTWARE\Google\Chrome\NativeMessagingHosts\<name>`, whose default value is the manifest path | `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/<name>.json` | `~/.config/google-chrome/NativeMessagingHosts/<name>.json` |
| Edge | key `HKCU\SOFTWARE\Microsoft\Edge\NativeMessagingHosts\<name>`; Edge also reads Chromium's key and then Chrome's | `~/Library/Application Support/Microsoft Edge/NativeMessagingHosts/<name>.json` (insider and beta channels keep their own directories) | `~/.config/microsoft-edge/NativeMessagingHosts/<name>.json` |
| Firefox | key `HKCU\SOFTWARE\Mozilla\NativeMessagingHosts\<name>` | `~/Library/Application Support/Mozilla/NativeMessagingHosts/<name>.json` | `~/.mozilla/native-messaging-hosts/<name>.json` |

One JSON object is what those keys and files point at: `name`, `description`, `path` to the host binary,
`type: "stdio"`, and both allow lists — `allowed_origins` for the Chromium family and `allowed_extensions` for
Firefox — always present as arrays, since each family reads its own and a null field is a manifest it rejects.
`connector/cmd/nexora-installer` writes it at `<root>\host\<name>.json`; nothing else is registered — no service, no
LaunchAgent, no startup item (`docs/architecture/33-binary-integrity.md` §14). The allowlist the host itself reads is
a separate file whose address is `#/senderPolicy/identity/allowlistFile`, so writer and reader never invent a path.

**The two byte caps are platform facts, not NEXORA's.** Chrome documents 64 MiB for what an extension may write into
the port and 1 MB for what the host may write back; Firefox gives the same 1 MB from the application and 4 GB toward
it, and Edge states that same pair. `#/limits` therefore carries 67108864 and 1048576, the binding number in each
direction being the smallest claim among the three. Until 2026-11-10 the data and §4 held 1 MiB out of the extension
and 1 GiB back into it, and neither figure is what those three pages state for the direction it was attached to, so the
host was written to refuse requests the browser would deliver and to emit answers the browser tears the port down for.
`docs/PROJECT-STATE.md` §4.54 records the measurement and the files it moved.

## 3. Framing

| Property | Value | Source |
|---|---|---|
| Length prefix | 4 bytes, unsigned, **little-endian**, length of the JSON payload only | `packages/schema/protocol/framing.json#/encoding/lengthPrefix` |
| Payload | one UTF-8 JSON object, no embedded newline, no array wrapper, no separator | `#/encoding/payload` |
| Frames per message | exactly one envelope object — Native Messaging is message-delimited, not a byte stream | `#/encoding/payload/oneFrameOneObject`, `#/nonGoals` |

Two things the framing deliberately does not do: newline-delimited JSON (a payload whose string values contain a
newline, or a frame that arrives split across a `readline` boundary, fragments the stream), and a
`Content-Length`-style text header (an off-by-one in a text protocol is a silent corruption; four
little-endian bytes are not).

## 4. Byte limits, and who enforces them

| Limit | Value | Enforced by | Why this side |
|---|---|---|---|
| extension → host | 64 MiB (`67108864`) | **sender, before write** | `#/limits/extensionToHostBytes/enforcement` — Chrome rejects the write; a receiver-side check would mean we already lost the frame |
| host → extension | 1 MiB (`1048576`) | **sender, before write** | `#/limits/hostToExtensionBytes/enforcement` — the cap all three families state, so anything larger is §5's problem, not the port's |
| effective cross-browser | 1 MiB | both | the smallest of the four documented caps, which is the host-to-extension one: a design that only works above it is a Chromium-only design |
| concurrent requests | 64 | sender stops issuing | `#/limits/maxConcurrentRequests`, `#/ordering/backpressure` |
| in-flight bytes | 8 MiB | sender | `#/limits/maxInFlightBytes` — the concurrency cap alone does not bound memory when frames are large |

`Verified: NO — deferred to Phase 2` for enforcement itself; the *values* are asserted today by the framing
sanity block in `packages/schema/scripts/verify.mjs`, including the ordering property that
`effectiveCrossBrowserBytes ≤ hostToExtensionBytes ≤ extensionToHostBytes`.

## 5. Oversized payloads: chunked envelope, and no `chunkId`

Decision recorded as data: `packages/schema/protocol/framing.json#/oversizedPayloads/decision` =
`chunked-envelope`. Mechanism (`#/oversizedPayloads/mechanism`): the slice fields live **nested** under
`envelope.chunk`, are `{contentHash, chunkSeq, chunkCount, encoding}` — all four required, `chunkCount ≥ 2` —
and `profile.import` mirrors that exact object under `args.chunk`
(`packages/schema/protocol/commands/profile.import.json#/args`). There is **no** top-level `chunkId` field and
**no** `chunk` frame type; the group is identified by `envelope.id` plus `contentHash`, and by
`args.contentHash` for the whole payload.

That is a correction of the earlier plan text, which listed `{contentHash, chunkId, chunkSeq}`. A separate
`chunkId` was rejected because it is a second identity for one thing: two ids that may disagree is the exact
shape of a reassembly bug. `packages/schema/protocol/envelope.schema.json#/required` keeps `id` mandatory, so
the group key is always present.

Group rules, asserted over the fixtures rather than described (the "chunk groups are complete" block in
`packages/schema/scripts/verify.mjs`):

1. every slice of one group declares the same `chunkCount`;
2. exactly `chunkCount` slices arrive and `chunkSeq` covers `0..chunkCount-1` exactly once — a gap fails the
   whole import with `IntegrityMismatch` rather than partially applying (`#/oversizedPayloads/mechanism/ordering`);
3. one `contentHash` per group;
4. within a frame, the envelope copy and the `args` copy of the slice are byte-identical;
5. the slice body rides in `args.payload` — a base64 fragment of the serialized import document, one fragment
   per slice frame, required on a chunked frame and rejected beside a frame without `chunk`
   (`protocol/commands/profile.import.json#/args/properties/payload`, `#/oversizedPayloads/mechanism/sliceBody`);
6. `chunk.contentHash` is sha256 over the concatenation of the decoded fragments of the group in `chunkSeq`
   order — the hash of the reassembled bytes, not of any single slice; `verify.mjs` reassembles the recorded
   group and hashes it (`#/oversizedPayloads/mechanism/sliceBody`);
7. reassembly completes, hashes, validates, and only then replaces the previous good set — validate-then-apply
   (`#/oversizedPayloads/mechanism/atomicity`);
8. total reassembled bytes are bounded by `packages/schema/subscription-source.schema.json#/properties/limits`,
   so chunking cannot be used to smuggle an arbitrarily large payload past the size guard.

Rejected alternative, with its reason kept in the data: **out-of-band file handoff** (extension writes a temp
file, host reads it) — `#/oversizedPayloads/alternativesRejected`. It leaks user data into a world-readable
temp path, needs a second cleanup mechanism, and gives the pipe two protocols instead of one. This closes the
plan's open question §7 п.9 in favour of chunking.

**The slice body — decided.** The paragraph this section used to end with asked where the bytes
ride, because `chunk` was metadata only and no field could hold a fragment (`chunk`'s
`additionalProperties` is `false`; the list fields are arrays of whole documents). It was the register's
`ODEC-34`, and it is answered as the register's second option, the **fragment model**: each slice carries
its fragment as base64 in the new `args.payload` field of `profile.import`
(`protocol/commands/profile.import.json#/args/properties/payload`), `encoding` keeps the one member that
names a real arrangement — `"base64"` — and `chunk.contentHash` is sha256 over the concatenation of the
decoded fragments of the group in `chunkSeq` order. The reassembled bytes are a UTF-8 JSON object shaped
like a single-frame `profile.import` args without `chunk`, `payload`, `contentHash` and `credentials`,
and each of its fields is validated against the same schema the command already references before anything is
written — rule 7's validate-then-apply is now written against a byte string the schema defines. The other two
options are excluded, recorded so the exclusion is not re-litigated: the announcement reading rested on a final
frame carrying the whole document structured, which is exactly the shape the recorded fixture used to show; and
the subset reading needed `encoding: "base64"` on a frame of JSON objects to mean something, plus a canonical
byte form this repository has never defined. The recorded group in
`packages/schema/fixtures/protocol/import-chunked.jsonl` now shows the decided shape: three slices, each with
`args.profiles: []` and a real base64 fragment in `args.payload`, and `verify.mjs` reassembles the
fragments, hashes the concatenation and refuses a group whose body does not hash to the declared
`contentHash` — which is also the assertion `VER-76` closed against. `connector/internal/chunk` is the
receiver half: `chunk.New` opens a group, `Add` takes one fragment, `Bytes` reassembles and verifies, and
a hash mismatch is `ErrIntegrityMismatch` — never a partial apply.

## 6. Envelope

Required: `["v","id","type","sentAt"]` — `packages/schema/protocol/envelope.schema.json#/required`.

| Field | Type / shape | Present when | Notes |
|---|---|---|---|
| `v` | integer 1–255 | always | the frame format version, distinct from the negotiated `protocolVersion` |
| `id` | `^[A-Za-z0-9_-]{8,36}$` | always | also the idempotency key (ADR-0008) |
| `type` | `request`, `response`, `event`, `progress` | always | the discriminator for the `allOf` clauses below |
| `method` | dotted lower-camel name | request | must be in the allowlist (`docs/architecture/26-command-surface.md`) |
| `args` | object or null | request | validated against the command's `#/args` |
| `result` | object or null | response, `ok: true` | validated against the command's `#/result` |
| `ok` | boolean | response | success/failure discriminator |
| `error` | `{code, message?, redacted?, retryable?, field?}` | response, `ok: false` | `code` required, from the closed taxonomy |
| `inReplyTo` | same shape as `id` | response, progress | request/event correlation |
| `traceId` | `^[A-Za-z0-9_-]{0,64}$` or null | optional | **no secrets, ever** — the pattern excludes `:`/`.`/spaces precisely so a `user@host:port` or a URL cannot be a trace id |
| `sentAt` | date-time | always | clocks are stripped before replay comparison (ADR-0008) |
| `seq` | integer ≥ 0 or null | per sender | gap detection only; no cross-sender ordering is assumed (`#/ordering/rule`) |
| `progress` | `{stage, done, total?, event?}` | progress frames | `stage` and `done` required |
| `chunk` | `{contentHash, chunkSeq, chunkCount, encoding}` | slice frames | §5 — the fragment rides in `args.payload` |

Type rules (`packages/schema/protocol/envelope.schema.json#/allOf`, three clauses): a `request` MUST carry
`method` and MUST NOT carry `result`, `ok` or `error`; a `response` MUST carry `ok` and `inReplyTo`, MUST NOT
carry `method`, and `ok: true` requires `result` while `ok: false` requires `error`; `event` and `progress`
MUST NOT carry `method` — an event with a method is a request wearing a different label, and it is rejected:
see `packages/schema/examples/invalid/envelope.event-with-method.json` and
`packages/schema/examples/invalid/envelope.ok-without-result.json`.

## 7. Negotiation

`connector.hello` is the only method dispatchable before negotiation
(`packages/schema/protocol/command-allowlist.json`, asserted by the "nothing is dispatchable before negotiation
except connector.hello" check). The exchange is
`packages/schema/protocol/compatibility-rules.json#/negotiation`:

- the extension announces `requiresProtocol` — its own max supported protocol version and its minimum
  acceptable connector version;
- the connector announces `protocolVersion`, `minCompatibleProtocolVersion`, `connectorVersion`, `coreVersion`,
  `capabilities`;
- compatible iff `extension.requiresProtocol.min ≤ connector.protocolVersion` **and**
  `connector.minCompatibleProtocolVersion ≤ extension.protocolVersion`; empty intersection ⇒ `VersionMismatch`;
- **the extension sends no command until hello succeeds.** This is the rule that makes the whole protocol
  safe to ship: a misread frame cannot apply a wrong proxy configuration, because nothing is sent to misread.

Matrix (`#/matrix`, three rows): extension `>=3.6 <4` × connector `protocolVersion 1` → compatible;
extension `>=3.6` × future `protocolVersion 2` → compatible-forward, where unknown event names are ignored and
unknown response fields tolerated; future extension × connector 1 → `VersionMismatch` → degraded, meaning the
Connector panel is disabled with a reason while **legacy profiles keep working** — a NEXORA entity that cannot
be applied is a visible error, never a silently direct connection.

Stability (`#/stability`): additive optional fields and new capability names are backward compatible; removing
a field, changing a type, or redefining a command's effect bumps `protocolVersion` and requires a matrix row;
**adding an error code is breaking** for clients that switch exhaustively, so new codes arrive only with a
documented fallback. `VersionMismatch` on the Connector panel is *not* a reason to stop applying legacy
profiles — the same reasoning as ADR-0015.

`commandGating` (`#/commandGating`): a command with a non-empty `capabilities[]` MUST NOT be sent until hello
reported every listed name. Today 13 capability names exist in the registry and the recorded hello announces
exactly that set — asserted in both directions by `packages/schema/scripts/verify.mjs`, so a name can neither be
added to the registry nor dropped from a hello without failing.

## 8. Errors: one closed taxonomy

`packages/schema/protocol/error-taxonomy.json` — 23 codes, each `{code, layer, meaning, retryable}`. The full
set: `BadRequest`, `UnknownMethod`, `SchemaMismatch`, `VersionMismatch`, `UnauthorizedExtension`,
`InvalidProfile`, `UnsupportedCombination`, `SecretRedacted`, `QuotaExceeded`, `CoreNotRunning`,
`CoreAlreadyRunning`, `CoreExited`, `ConfigApplyFailed`, `HealthCheckFailed`, `HandshakeFailed`, `AuthFailed`,
`NetworkUnreachable`, `Timeout`, `IntegrityMismatch`, `DiskFull`, `KeystoreUnavailable`, `Busy`, `Internal`.

Rules:

1. A code outside the taxonomy is a protocol violation, not a retryable error — the client cannot back off on
   an error it has never seen.
2. Every command doc cites only codes it can actually return; the "cites only taxonomy codes" and
   "fixture error code is in the taxonomy" assertions enforce that at both authoring time and replay time.
3. `retryable` drives client behaviour: `Busy` (dedupe window or CAS conflict) and `Timeout` yes;
   `UnauthorizedExtension`, `IntegrityMismatch`, `SecretRedacted` never.
4. `SecretRedacted` exists so that a *masked-on-purpose* rejection is distinguishable from a corrupt one
   (`docs/architecture/32-secrets-keystore-masking.md`).
5. A frame received before hello is answered `HandshakeFailed` if it carries a gated method, and
   `UnauthorizedExtension` if the sender is not allowlisted — the two are deliberately different because the
   operator's remediation differs (`docs/architecture/31-security-model.md`).

Which code answers a request whose args violate the call contract is declared by only six of the nineteen command docs, and the other thirteen — `server.test` among them — have no honest member of their own `#/errors` for it; the two answers already shipped are analogies (`connector.hello` answers `HandshakeFailed`, the `server.test` surface answers `HealthCheckFailed`), which is **Verified — ODEC-43 answered: the closest declared meaning, chosen per command** (the status quo, already tested and shipped)

## 9. Deadlines belong to the extension

`packages/schema/protocol/commands.schema.json#/properties/timeoutMs` gives each method a budget; the current
distribution is 2 000 ms (`connector.version`) to 120 000 ms (`subscription.refresh`). The rule that goes with
it is behavioural, not numerical:

- the **extension** owns the deadline and synthesises a local `Timeout` if the host stops answering, because a
  hung Connector must not hang the service worker (`docs/architecture/21-system-context.md` §12);
- the Connector's own internal timeouts are its business and MUST NOT be relied upon by the client;
- a late answer to an already-timed-out request is discarded by `id`, and MUST NOT re-apply its side effect —
  for a `replace`-class command that would be a config the user did not ask for;
- probes (`server.test`) are never retried on timeout: a retry is a second measurement, and the client wanted
  one. `Verified: NO — deferred to Phase 2` for the discard path.

One correction of the plan: it asked for `progress` frames for both `subscription.refresh` and `server.test`.
`subscription.refresh` has them (`subscription.updated`, `subscription.failed`), and `server.test` deliberately
does **not**: a probe's contract is exactly one answer (`packages/schema/fixtures/protocol/` is asserted so that
a probe answered twice fails the build), and progress frames would be a way to smuggle a second answer in.

Methods that do emit progress: `config.apply`, `core.start`, `core.restart`, `core.stop`, `keystore.purge`,
`profile.import`, `subscription.refresh` — each named in its own command doc's `progressEvents`, and every
event name is checked against `packages/schema/events.schema.json` by the "cites only registered events"
assertion.

## 10. Ordering, backpressure, idempotency interlude

`#/ordering`: one port delivers in order per sender; nothing is assumed across senders; `seq` is gap detection
only. `#/ordering/backpressure`: the sender stops issuing beyond `maxConcurrentRequests`; neither direction
holds an unbounded queue. Because the client's `id` is the idempotency key (ADR-0008), a retransmitted request
after a reconnect is either deduped to the original result or answered identically — which is why the fixtures
contain replay pairs whose results are byte-equal after clock fields are stripped, asserted by
`packages/schema/scripts/verify.mjs`.

## 11. Liveness and reconnect semiotics

`packages/schema/protocol/framing.json#/liveness`: the host sends a heartbeat every 25 s
(host-to-extension direction) and three missed heartbeats mean the peer is gone. The legacy keep-alive attempt
is visible in the code we replace (`omega-target-chromium-extension/overlay/x-background.js:34`); it is not a
heartbeat — it keeps a worker awake, which is a different problem.

Semiotics, stated because getting them backwards is the classic outage:

| Signal | Meaning | MUST NOT imply |
|---|---|---|
| port closed by the browser | the *client* is gone; the Connector keeps running | a shutdown request (ADR-0005) |
| heartbeat missed | the peer is unreachable; reconnect per `#/reconnect` | that Core died |
| `CoreExited` event | Core really exited | that the pipe is broken |
| reconnect succeeded | re-run hello, then reconcile by hash | that nothing changed while we were away |

Reconnect policy (`#/reconnect`): exponential backoff `[1,2,4,8,16,32,60]` s with 30 % jitter — jitter because
every browser window of one user reconnects at once otherwise — and the reconcile step compares hashes locally
(`VER-90`: the command's `#/args` carries no expectation field), so a drifted state is detected, not assumed
(`docs/architecture/30-failure-policy.md`). `Verified: NO — deferred to Phase 2`.

## 12. Non-goals of the wire protocol

`packages/schema/protocol/framing.json#/nonGoals`, restated: no streaming of large bodies beyond chunk
envelopes; no binary frames; no secrets in `traceId`; no out-of-band handoff. Additionally: no compression (a
Native Messaging frame is already bounded and compression of a secret-bearing payload is a CRIME-class risk),
no per-frame authentication token (the OS pipe and the sender allowlist are the authentication — ADR-0004), no
method-name versioning in the method itself (`config.apply.v2` is a protocol-version change, not a new name),
and no bidirectional streaming RPC semantics.

## 13. Invariants of this protocol and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| Frame limits are the documented ones and ordered | framing sanity block, `packages/schema/scripts/verify.mjs` "extension-to-host frame limit is 64 MiB", "the effective cross-browser limit is not larger than the smallest documented cap" and "host-to-extension cap is the binding 1 MB" | **GREEN** |
| Envelope `required` is exactly `v,id,type,sentAt` | `#/required` + ajv compile of every fixture frame | **GREEN** |
| `request`/`response`/`event` field discipline | `#/allOf` + the two must-reject envelope examples | **GREEN** |
| The sender identity crosses hop 2 by a named mechanism, and hop 2 has a contract of its own | the sender is which pipe the host opened, injected at spawn; the pipe carries the same envelope, guarded by `pipeVersion` | Verified as data — ADR-0022 states the boundary; the runtime half is the live rows, still `SKIPPED` |
| A frame whose `v` is below the pipe floor is refused before dispatch, by the code `framing.json` names | `connector/internal/envelope`'s `Parse` compares `v` to `contract.MinPipeVersion` and `TestThePipeFloorComparisonCompilesAndRuns` pins that the branch runs — but it cannot fire while `packages/schema/protocol/envelope.schema.json#/properties/v/minimum` is the same 1 the floor carries | Verified: NO — deferred to Phase 2 (`VER-89`: today such a frame is an invalid envelope and ends the stream; `#/versionFloors/tooOld` asks for a `VersionMismatch` answer in reply to the frame's own `id`, which is the assertion the front door owes the day the floor rises above 1) |
| Chunk shape is one object mirrored in `profile.import` | "the envelope chunk shape and profile.import args.chunk are one shape" assertion | **GREEN** |
| Chunk groups complete and non-vacuous | the chunk-group block with its absent-group, clean-group and split-hash guards | **GREEN** |
| Slice bodies reassemble to the document the sender hashed | the chunk-group block's "reassembled slice bodies hash to the declared contentHash" and "the reassembled bytes are a UTF-8 JSON document"; the receiver half is `connector/internal/chunk` (`Bytes` verifies before returning) | **GREEN** |
| Nothing dispatchable pre-hello but `connector.hello` | "nothing is dispatchable before negotiation except connector.hello" | **GREEN** |
| Capability registry and recorded hello are the same set | the capability equality assertions | **GREEN** |
| Every cited error code exists | "cites only taxonomy codes" + fixture-level taxonomy checks | **GREEN** |
| Every taxonomy entry declares `retryable` | "every error code declares retryability" | **GREEN** |
| Probes answered exactly once | "probe is answered exactly once" over the fixtures | **GREEN** |
| Replays byte-identical after clock-strip | "idempotent replay returns the same result" (5 pairs today) | **GREEN** |
| Every fixture frame validates against the envelope | the replay loop; it prints the frame count on every run | **GREEN** |
| Sender-side rejection before write | the `packages/protocol` codec refuses an oversized write and a peer declaring one; the `packages/state` client refuses a request over the ceiling before the port | Verified: NO — deferred to Phase 2 (the real crossing must still show a browser accepting one up to its own cap) |
| Heartbeat / missed-heartbeat behaviour | nothing | Verified: NO — deferred to Phase 2 (a liveness test drives the 25 s heartbeat and the three-miss deadline) |
| Late-answer discard by `id` | the Go request table and the `packages/state` client each drop an answer whose `id` is no longer in flight, and a duplicate one | Verified: NO — deferred to Phase 2 (no run shows a real host's late answer arriving after a timeout) |
| Jittered reconnect actually jitters | the `packages/state` harness walks the capped ladder on an injected clock; a live drive against `nexora-host` + `nexora-connector` measured 1009 ms and 1203 ms on the first rung, re-running hello both times | Verified: NO — LIVE-13 (a browser's own port teardown is still unmeasured) |

Trace: ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0015, ADR-0019 · Contract:
`packages/schema/protocol/framing.json`, `packages/schema/protocol/envelope.schema.json`,
`packages/schema/protocol/error-taxonomy.json`, `packages/schema/protocol/compatibility-rules.json`,
`packages/schema/protocol/commands.schema.json`, `packages/schema/fixtures/protocol/` · See also:
`docs/architecture/21-system-context.md`, `docs/architecture/22-component-contracts.md`,
`docs/architecture/26-command-surface.md`, `docs/architecture/30-failure-policy.md`,
`docs/architecture/31-security-model.md`.
