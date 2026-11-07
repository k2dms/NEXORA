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
and answers `ODEC-02` (2026-11-06), and no code compares a frame's `v` to it yet. The other half of the same silence
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
real process until one is chosen. This defers to `docs/architecture/40-open-decisions.md`; its entry names the four.

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
| extension → host | 1 MiB (`1048576`) | **sender, before write** | `#/limits/extensionToHostBytes/enforcement` — Chrome rejects the write; a receiver-side check would mean we already lost the frame |
| host → extension | 1 GiB | receiver treats an oversized frame as a transport error and reconnects, never truncates | `#/limits/hostToExtensionBytes` |
| effective cross-browser | 1 MiB | both | Firefox's historical cap is the binding constraint, so a design that only works at 1 GiB is a Chromium-only design |
| concurrent requests | 64 | sender stops issuing | `#/limits/maxConcurrentRequests`, `#/ordering/backpressure` |
| in-flight bytes | 8 MiB | sender | `#/limits/maxInFlightBytes` — the concurrency cap alone does not bound memory when frames are large |

`Verified: NO — deferred to Phase 2` for enforcement itself; the *values* are asserted today by the framing
sanity block in `packages/schema/scripts/verify.mjs`, including the ordering property that
`effectiveCrossBrowserBytes ≤ extensionToHostBytes ≤ hostToExtensionBytes`.

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
5. reassembly completes, hashes, validates, and only then replaces the previous good set — validate-then-apply
   (`#/oversizedPayloads/mechanism/atomicity`);
6. total reassembled bytes are bounded by `packages/schema/subscription-source.schema.json#/properties/limits`,
   so chunking cannot be used to smuggle an arbitrarily large payload past the size guard.

Rejected alternative, with its reason kept in the data: **out-of-band file handoff** (extension writes a temp
file, host reads it) — `#/oversizedPayloads/alternativesRejected`. It leaks user data into a world-readable
temp path, needs a second cleanup mechanism, and gives the pipe two protocols instead of one. This closes the
plan's open question §7 п.9 in favour of chunking.

**The one thing §5 does not decide.** Everything above is slice *metadata*, and the fixtures and
`packages/schema/scripts/verify.mjs` assert all of it. What no file in `packages/schema/` states is the slice
**body** — where the bytes ride. `packages/schema/protocol/envelope.schema.json#/properties/chunk/additionalProperties`
is `false`, so nothing travels inside `chunk`; `packages/schema/protocol/commands/profile.import.json#/args/properties`
offers `profiles`, `servers`, `mode`, `contentHash`, `chunk`, `backupBeforeWrite`, `credentials`, and the only two
list fields are typed as arrays of whole documents (`#/args/properties/profiles/items/$ref` points at the profile
schema), so a byte fragment cannot be put in them and still validate. The recorded group does not choose between
them. `packages/schema/fixtures/protocol/import-chunked.jsonl` sends `chunkCount: 3` whose first two slices carry
`args.profiles: []`, no `args.servers` at all, and `encoding: "base64"` — a string encoding of bytes no field
holds — while the third carries the entire document as structured `profiles` and `servers`, and all three repeat
one whole-document `args.contentHash`. That is consistent with three shapes of exchange, and the data removes one
of them: (a) the earlier slices are announcements and only the last frame travels, except that
`packages/schema/protocol/framing.json#/oversizedPayloads/mechanism/cap` counts "total reassembled bytes" and
`packages/schema/protocol/envelope.schema.json#/properties/chunk/description` says the field appears "when a
payload exceeds the 1 MB extension-to-host frame limit" — under (a) the frame that must carry the whole document
exceeds §4's sender cap and cannot be written at all, so this reading is excluded, and what the corpus does not
say is which of the two survivors replaces it. (b) each slice carries a fragment of the serialized document and
`contentHash` hashes the concatenation of the decoded bytes — what rules 2, 5 and 6 above assume, and the one
reading with no field to write a fragment into. (c) each slice carries a valid *subset* of the profile and server
arrays and `contentHash` hashes the canonical serialization of the union — every frame then stays individually
schema-valid, but `encoding: "base64"` on a frame of JSON objects becomes meaningless, `json` is the other member
of that enum for a reason, and "canonical" is a byte form no file in this repository defines. (b) and (c)
disagree about which byte string `chunk.contentHash` names, and the atomicity rule is written against exactly that
string, so reassembly cannot be written from this document without guessing. This defers to `docs/architecture/40-open-decisions.md`,
whose entry for it is the question `connector/internal/chunk` is waiting on; the human wording of the same question
is `docs/PROJECT-STATE.md` §9.

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
| `chunk` | `{contentHash, chunkSeq, chunkCount, encoding}` | slice frames | §5 |

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

Which code answers a request whose args violate the call contract is declared by only six of the nineteen command docs, and the other thirteen — `server.test` among them — have no honest member of their own `#/errors` for it; the two answers already shipped are analogies (`connector.hello` answers `HandshakeFailed`, the `server.test` surface answers `HealthCheckFailed`), which is **Verified: NO — deferred to Phase 2** until a rule or a contract pass decides it, and `docs/architecture/40-open-decisions.md` owns the question.

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
every browser window of one user reconnects at once otherwise — and the reconcile step sends the extension's
recorded applied hash via `config.snapshot` so a drifted state is detected rather than assumed
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
| Frame limits are the documented ones and ordered | framing sanity block, `packages/schema/scripts/verify.mjs` "extension-to-host frame limit is 1 MB", "the effective cross-browser limit is not larger than the smallest documented cap" and "host-to-extension cap is at least the reverse direction" | **GREEN** |
| Envelope `required` is exactly `v,id,type,sentAt` | `#/required` + ajv compile of every fixture frame | **GREEN** |
| `request`/`response`/`event` field discipline | `#/allOf` + the two must-reject envelope examples | **GREEN** |
| The sender identity crosses hop 2 by a named mechanism, and hop 2 has a contract of its own | nothing | Verified: NO — deferred to Phase 2 (the envelope is closed at fourteen fields and none of them is a sender, and no file in `packages/schema/` states the local pipe's message schema or its version field — see §2's last three paragraphs; naming that contract is the assertion this row promises) |
| Chunk shape is one object mirrored in `profile.import` | "the envelope chunk shape and profile.import args.chunk are one shape" assertion | **GREEN** |
| Chunk groups complete and non-vacuous | the chunk-group block with its absent-group, clean-group and split-hash guards | **GREEN** |
| Slice bodies reassemble to the document the sender hashed | nothing | Verified: NO — deferred to Phase 2 (the contract says what a slice *is* and never says where its bytes ride — see §5's last paragraph; the reassembly test is the assertion this row promises) |
| Nothing dispatchable pre-hello but `connector.hello` | "nothing is dispatchable before negotiation except connector.hello" | **GREEN** |
| Capability registry and recorded hello are the same set | the capability equality assertions | **GREEN** |
| Every cited error code exists | "cites only taxonomy codes" + fixture-level taxonomy checks | **GREEN** |
| Every taxonomy entry declares `retryable` | "every error code declares retryability" | **GREEN** |
| Probes answered exactly once | "probe is answered exactly once" over the fixtures | **GREEN** |
| Replays byte-identical after clock-strip | "idempotent replay returns the same result" (5 pairs today) | **GREEN** |
| Every fixture frame validates against the envelope | the replay loop; it prints the frame count on every run | **GREEN** |
| Sender-side rejection before write | nothing | Verified: NO — deferred to Phase 2 (the client must refuse a >1 MiB write before the port) |
| Heartbeat / missed-heartbeat behaviour | nothing | Verified: NO — deferred to Phase 2 (a liveness test drives the 25 s heartbeat and the three-miss deadline) |
| Late-answer discard by `id` | nothing | Verified: NO — deferred to Phase 2 (a request-table test drops an answer whose `id` is no longer in flight) |
| Jittered reconnect actually jitters | nothing | Verified: NO — LIVE-13 |

Trace: ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0015, ADR-0019 · Contract:
`packages/schema/protocol/framing.json`, `packages/schema/protocol/envelope.schema.json`,
`packages/schema/protocol/error-taxonomy.json`, `packages/schema/protocol/compatibility-rules.json`,
`packages/schema/protocol/commands.schema.json`, `packages/schema/fixtures/protocol/` · See also:
`docs/architecture/21-system-context.md`, `docs/architecture/22-component-contracts.md`,
`docs/architecture/26-command-surface.md`, `docs/architecture/30-failure-policy.md`,
`docs/architecture/31-security-model.md`.
