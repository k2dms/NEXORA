# 26 — Command surface: the eighteen methods the Connector will answer

Status: Accepted
Depends on: ADR-0004, ADR-0005, ADR-0007, ADR-0008, ADR-0009, ADR-0013, ADR-0019, ADR-0020

> This document is the human-readable half of a contract whose machine-readable half is
> `packages/schema/protocol/command-allowlist.json` plus the eighteen files in
> `packages/schema/protocol/commands/`. It MUST name exactly those eighteen and nothing else as dispatchable.
> That equality is not editorial: `packages/schema/scripts/verify.mjs` "docs/architecture/26-command-surface.md names the same set" parses this file for backticked
> dotted names, intersects them with the allowlist, and fails the build unless the two sets are identical.

## 0. Rules that govern every method

1. The set is **closed**. `deniedByDefault: true` and `unknownMethodBehavior: "UnknownMethod"`
   (`packages/schema/protocol/command-allowlist.json:7-8`). An unknown method MUST be rejected before any state
   is touched, with no fallthrough, no default handler, and no "closest match"
   (`packages/schema/protocol/command-allowlist.json:9`). **Verified**: `packages/schema/scripts/verify.mjs` "command class agrees between allowlist and command doc" asserts each command
   doc's `class` equals the allowlist `class`, and `packages/schema/scripts/verify.mjs` "hello gating agrees between allowlist and command doc" asserts its hello gating is the exact
   complement of `availableBeforeHello`.
2. The caller MUST NOT be able to name a path, a URL, a shell token or a binary. Arguments are entity
   identifiers (`^srv-[0-9a-f]{8}$` tags, profile ids, subscription ids) and enumerated policies. This is the
   master prompt's "strictly typed commands, allowlist" constraint as data; the enumeration lives in
   `packages/schema/protocol/command-allowlist.json` and the shapes live in each command's `args` subschema.
3. Negotiation precedes everything. Only `connector.hello` is dispatchable beforehand
   (`packages/schema/protocol/command-allowlist.json:13`). A frame for any other method before a successful
   hello is `HandshakeFailed`, not "best effort".
4. `privileged` is a routing decision, not a hint. Ten of the eighteen are privileged
   (`packages/schema/protocol/command-allowlist.json:18-28`): every method that can start, stop or reconfigure
   Core, write the keystore, delete entities, or emit egress. A connector build MAY refuse privileged methods
   to a sender that is not the pinned extension id (ADR-0004, `docs/architecture/31-security-model.md`).
5. Every method declares `timeoutMs`, and the deadline belongs to the caller
   (`packages/schema/protocol/commands/schema` — `timeoutMs` is "Owned by the caller. A hung host must never
   hang the service worker"). The Connector MAY finish later and report; the extension MUST NOT assume a
   response means a completed effect after the deadline.
6. Only `server.test` is non-replayable. Everything else declares `replayable: true`, and `packages/schema/scripts/verify.mjs` "probe is non-replayable: ${doc.name}"
   enforces that a `probe` is exactly the opposite, because a cached timing is a lie about the network
   (ADR-0008).
7. Errors come from one closed taxonomy (`packages/schema/protocol/error-taxonomy.json`, 23 codes, each with
   `retryable`); `packages/schema/scripts/verify.mjs` "command ${doc.name} cites only taxonomy codes" rejects a command doc that cites a code the taxonomy does not define.

## 1. Surface at a glance

`class` is the idempotency semantics of §2; `priv` = privileged; `t` = `timeoutMs`.

| Method | class | priv | t (ms) | Errors | Side effects | Progress events |
|---|---|---|---|---|---|---|
| `connector.hello` | read | no | 5 000 | 5 | none | — |
| `connector.version` | read | no | 2 000 | 3 | none | — |
| `connector.status` | read | no | 3 000 | 3 | none | — |
| `connector.health` | read | no | 5 000 | 5 | none | — |
| `connector.events.subscribe` | mutate | no | 5 000 | 5 | none | — |
| `core.start` | converge | **yes** | 15 000 | 10 | core-process, disk-write, log-write | `core.started`, `integrity.mismatch` |
| `core.stop` | converge | **yes** | 10 000 | 4 | core-process, log-write | `core.exited` |
| `core.restart` | converge | **yes** | 20 000 | 10 | core-process, disk-write, log-write | `core.restart`, `core.started`, `core.exited` |
| `core.status` | read | no | 3 000 | 4 | none | — |
| `config.apply` | replace | **yes** | 30 000 | 14 | core-config, core-process, disk-write, log-write | `config.applied`, `core.restart`, `core.started` |
| `config.snapshot` | read | no | 5 000 | 5 | none | — |
| `profile.import` | replace | **yes** | 60 000 | 12 | entity-write, entity-delete, keystore-write, disk-write, log-write | `config.applied` |
| `profile.remove` | mutate | **yes** | 10 000 | 9 | entity-delete, entity-write, keystore-delete, disk-write, log-write | — |
| `profile.validate` | read | no | 10 000 | 7 | none | — |
| `subscription.refresh` | mutate | **yes** | 120 000 | 13 | keystore-write, keystore-delete, network-egress, disk-write, log-write | `subscription.updated`, `subscription.failed` |
| `server.test` | probe | **yes** | 30 000 | 9 | network-egress, log-write | — |
| `diagnostics.get` | read | **yes** | 15 000 | 6 | none | — |
| `keystore.purge` | mutate | **yes** | 10 000 | 8 | keystore-delete, disk-write, log-write | `keystore.unavailable` |

Counts and both class/priv columns are read from `packages/schema/protocol/commands/*.json` and
`packages/schema/protocol/command-allowlist.json`; a divergence fails `npm --prefix packages/schema run verify`.
**Verified.**

## 2. The four behaviour classes, and why `class` is data

| class | Contract | Failure mode if misread |
|---|---|---|
| `read` | No state mutation. Replaying is free. MUST NOT be used to infer that an effect happened — that is what `config.snapshot` and `core.status` are for. | A UI that treats `connector.status` as a health verdict will display "healthy" while Core is dead but the connector alive. |
| `converge` | `core.start`, `core.stop`, `core.restart`: the caller states the desired state; the Connector makes it true or reports why it cannot. `core.start` on an already-running Core with the same config hash is a **successful no-op returning the same PID** (`packages/schema/protocol/commands/core.start.json:4`). | A caller that treats converge as imperative will double-spawn Core or interpret a no-op as failure. |
| `replace` | `config.apply`, `profile.import`: declarative replace-and-reconcile of a whole set, validated before anything is written, with a compare-and-swap field. | A partial write on failure — forbidden by the invariants below. |
| `mutate` | `profile.remove`, `subscription.refresh`, `keystore.purge`, `connector.events.subscribe`: one directed change, idempotent by client `id` (ADR-0008), with a declared durable effect. | Replayed after a crash: safe only because the client `id` is the idempotency key. |
| `probe` | `server.test`: measures the network now. `replayable: false` is enforced (`packages/schema/scripts/verify.mjs` "probe is non-replayable: ${doc.name}"). | A cached probe result is a false statement about connectivity; the client MUST treat a replay as stale. |

### 2.1 `config.apply` and the switch that is one

This distinction is the most commonly broken one in designs of this shape, so it is stated as a rule with the
field that pins it:

- `config.apply` **replaces the desired profile set**, recompiles, and restarts Core **only if** the compiled
  hash differs — `result.unchangedBecauseHashMatched` and `result.restarted` are separate fields in
  `packages/schema/protocol/commands/config.apply.json:59-60`, and `expectedAppliedHash`
  (`packages/schema/protocol/commands/config.apply.json:42-45`) is a compare-and-swap: a mismatch is `Busy`,
  never a silent overwrite, because two options tabs must not race a config. That field's own type is
  `[string, null]` over `^([0-9a-f]{64})?# 26 — Command surface: the eighteen methods the Connector will answer

Status: Accepted
Depends on: ADR-0004, ADR-0005, ADR-0007, ADR-0008, ADR-0009, ADR-0013, ADR-0019, ADR-0020

> This document is the human-readable half of a contract whose machine-readable half is
> `packages/schema/protocol/command-allowlist.json` plus the eighteen files in
> `packages/schema/protocol/commands/`. It MUST name exactly those eighteen and nothing else as dispatchable.
> That equality is not editorial: `packages/schema/scripts/verify.mjs` "docs/architecture/26-command-surface.md names the same set" parses this file for backticked
> dotted names, intersects them with the allowlist, and fails the build unless the two sets are identical.

## 0. Rules that govern every method

1. The set is **closed**. `deniedByDefault: true` and `unknownMethodBehavior: "UnknownMethod"`
   (`packages/schema/protocol/command-allowlist.json:7-8`). An unknown method MUST be rejected before any state
   is touched, with no fallthrough, no default handler, and no "closest match"
   (`packages/schema/protocol/command-allowlist.json:9`). **Verified**: `packages/schema/scripts/verify.mjs` "command class agrees between allowlist and command doc" asserts each command
   doc's `class` equals the allowlist `class`, and `packages/schema/scripts/verify.mjs` "hello gating agrees between allowlist and command doc" asserts its hello gating is the exact
   complement of `availableBeforeHello`.
2. The caller MUST NOT be able to name a path, a URL, a shell token or a binary. Arguments are entity
   identifiers (`^srv-[0-9a-f]{8}$` tags, profile ids, subscription ids) and enumerated policies. This is the
   master prompt's "strictly typed commands, allowlist" constraint as data; the enumeration lives in
   `packages/schema/protocol/command-allowlist.json` and the shapes live in each command's `args` subschema.
3. Negotiation precedes everything. Only `connector.hello` is dispatchable beforehand
   (`packages/schema/protocol/command-allowlist.json:13`). A frame for any other method before a successful
   hello is `HandshakeFailed`, not "best effort".
4. `privileged` is a routing decision, not a hint. Ten of the eighteen are privileged
   (`packages/schema/protocol/command-allowlist.json:18-28`): every method that can start, stop or reconfigure
   Core, write the keystore, delete entities, or emit egress. A connector build MAY refuse privileged methods
   to a sender that is not the pinned extension id (ADR-0004, `docs/architecture/31-security-model.md`).
5. Every method declares `timeoutMs`, and the deadline belongs to the caller
   (`packages/schema/protocol/commands/schema` — `timeoutMs` is "Owned by the caller. A hung host must never
   hang the service worker"). The Connector MAY finish later and report; the extension MUST NOT assume a
   response means a completed effect after the deadline.
6. Only `server.test` is non-replayable. Everything else declares `replayable: true`, and `packages/schema/scripts/verify.mjs` "probe is non-replayable: ${doc.name}"
   enforces that a `probe` is exactly the opposite, because a cached timing is a lie about the network
   (ADR-0008).
7. Errors come from one closed taxonomy (`packages/schema/protocol/error-taxonomy.json`, 23 codes, each with
   `retryable`); `packages/schema/scripts/verify.mjs` "command ${doc.name} cites only taxonomy codes" rejects a command doc that cites a code the taxonomy does not define.

## 1. Surface at a glance

`class` is the idempotency semantics of §2; `priv` = privileged; `t` = `timeoutMs`.

| Method | class | priv | t (ms) | Errors | Side effects | Progress events |
|---|---|---|---|---|---|---|
| `connector.hello` | read | no | 5 000 | 5 | none | — |
| `connector.version` | read | no | 2 000 | 3 | none | — |
| `connector.status` | read | no | 3 000 | 3 | none | — |
| `connector.health` | read | no | 5 000 | 5 | none | — |
| `connector.events.subscribe` | mutate | no | 5 000 | 5 | none | — |
| `core.start` | converge | **yes** | 15 000 | 10 | core-process, disk-write, log-write | `core.started`, `integrity.mismatch` |
| `core.stop` | converge | **yes** | 10 000 | 4 | core-process, log-write | `core.exited` |
| `core.restart` | converge | **yes** | 20 000 | 10 | core-process, disk-write, log-write | `core.restart`, `core.started`, `core.exited` |
| `core.status` | read | no | 3 000 | 4 | none | — |
| `core.select` | mutate | **yes** | 3 000 | 7 | core-config | — |
| `config.apply` | replace | **yes** | 30 000 | 14 | core-config, core-process, disk-write, log-write | `config.applied`, `core.restart`, `core.started` |
| `config.snapshot` | read | no | 5 000 | 5 | none | — |
| `profile.import` | replace | **yes** | 60 000 | 12 | entity-write, entity-delete, keystore-write, disk-write, log-write | `config.applied` |
| `profile.remove` | mutate | **yes** | 10 000 | 9 | entity-delete, entity-write, keystore-delete, disk-write, log-write | — |
| `profile.validate` | read | no | 10 000 | 7 | none | — |
| `subscription.refresh` | mutate | **yes** | 120 000 | 13 | keystore-write, keystore-delete, network-egress, disk-write, log-write | `subscription.updated`, `subscription.failed` |
| `server.test` | probe | **yes** | 30 000 | 9 | network-egress, log-write | — |
| `diagnostics.get` | read | **yes** | 15 000 | 6 | none | — |
| `keystore.purge` | mutate | **yes** | 10 000 | 8 | keystore-delete, disk-write, log-write | `keystore.unavailable` |

Counts and both class/priv columns are read from `packages/schema/protocol/commands/*.json` and
`packages/schema/protocol/command-allowlist.json`; a divergence fails `npm --prefix packages/schema run verify`.
**Verified.**

## 2. The four behaviour classes, and why `class` is data

| class | Contract | Failure mode if misread |
|---|---|---|
| `read` | No state mutation. Replaying is free. MUST NOT be used to infer that an effect happened — that is what `config.snapshot` and `core.status` are for. | A UI that treats `connector.status` as a health verdict will display "healthy" while Core is dead but the connector alive. |
| `converge` | `core.start`, `core.stop`, `core.restart`: the caller states the desired state; the Connector makes it true or reports why it cannot. `core.start` on an already-running Core with the same config hash is a **successful no-op returning the same PID** (`packages/schema/protocol/commands/core.start.json:4`). | A caller that treats converge as imperative will double-spawn Core or interpret a no-op as failure. |
| `replace` | `config.apply`, `profile.import`: declarative replace-and-reconcile of a whole set, validated before anything is written, with a compare-and-swap field. | A partial write on failure — forbidden by the invariants below. |
| `mutate` | `core.select`, `profile.remove`, `subscription.refresh`, `keystore.purge`, `connector.events.subscribe`: one directed change, idempotent by client `id` (ADR-0008), with a declared durable effect. | Replayed after a crash: safe only because the client `id` is the idempotency key. |
| `probe` | `server.test`: measures the network now. `replayable: false` is enforced (`packages/schema/scripts/verify.mjs` "probe is non-replayable: ${doc.name}"). | A cached probe result is a false statement about connectivity; the client MUST treat a replay as stale. |


  (`packages/schema/protocol/commands/config.apply.json#/args/properties/expectedAppliedHash`), which is the
  contract saying a caller may state no belief at all: an absent value is not a claim that the record is empty, it
  is the absence of a precondition — otherwise every second apply from the same tab would have to read
  `core.status` first just to be allowed to speak.
- **A fast server switch is a `config.apply` whose compiled hash is unchanged** — the pipeline compiles to the
  same bytes, `unchangedBecauseHashMatched` comes back true, and Core is not restarted. This is §9.5's answer
  (2026-11-14, inbound-per-tag): the imperative fast path `core.select` promised — a switch with no config
  write — had no mechanism under one-inbound-with-routing, because the per-tag inbound layout the compiler ships
  pins the server into the config bytes, so the command was removed from the surface (18 methods now) rather
  than left published and unwired. The quick switch costs one compile, not a restart; the UI note in
  `packages/schema/presentation.json` states exactly that.

### 2.2 `profile.import` and the chunked envelope

`profile.import` (60 000 ms) is the only method that can arrive split across frames. The slice shape is a
nested `args.chunk` object — `{contentHash, chunkSeq, chunkCount, encoding}` — and it MUST be structurally
identical to `protocol/envelope.schema.json#/properties/chunk`; `packages/schema/scripts/verify.mjs` "the envelope chunk shape and profile.import args.chunk are one shape" compares the two field by
field so the mirror cannot drift. `args.profiles` is `minItems: 0` precisely so a slice frame can carry no
profiles (`packages/schema/protocol/commands/profile.import.json:51-55`), `contentHash` is mandatory whenever
`chunk` is present, and the group rules — same `chunkCount` across a group, `chunkSeq` covering
`0..chunkCount-1` exactly once, one hash per group — are asserted over
`packages/schema/fixtures/protocol/*.jsonl` by `packages/schema/scripts/verify.mjs` "a chunked profile.import slice exists in the fixtures", "a complete chunked import reassembles in the fixtures" and "the chunked IntegrityMismatch path is exercised by a real group", which fails if a chunked group is absent
from the fixtures rather than passing vacuously.

## 3. Per-method notes a reader cannot get from the schema

**`connector.hello`** — the only pre-negotiation method
(`packages/schema/protocol/command-allowlist.json:13`). Args: `extensionVersion`, `protocolVersion`,
`minCompatibleProtocolVersion`. Result: `protocolVersion`, `minCompatibleProtocolVersion`, `connectorVersion`,
`capabilities`, plus the keystore backend and Job-Object answer (`packages/schema/protocol/commands/connector.hello.json:35-36`)
that only make sense in the two-process model of ADR-0005. An extension whose `minCompatibleProtocolVersion`
the Connector cannot satisfy MUST refuse to proceed; the compatibility table is
`packages/schema/protocol/compatibility-rules.json`, whose capability names are asserted in both directions
against `connector.hello` (`packages/schema/scripts/verify.mjs` "command ${doc.name} cites only declared capabilities").

**`connector.version`** — subsumed by `connector.hello` for negotiation, kept because an already-negotiated
client needs a cheap re-read after a connector restart without re-declaring capabilities.

**`connector.status`** — the connector process, not Core. `core.status` is Core. They disagree routinely:
connector up, Core down, is precisely the `coreDeath` fail-closed plane of ADR-0009.

**`connector.health`** — the aggregate of the browser-side and connector-side views; requires capability
`health.probes`. Its `state` vocabulary is the same one `server.test` reports per layer, so a UI can render
one colour language (`packages/schema/protocol/commands/connector.health.json`).

**`connector.events.subscribe`** — args `names`, `replayRetained` (required), `sinceSeq`, `maxPerSecond`. It
is `class: mutate` because it allocates a `subscriptionId` and starts delivery; it is not privileged because
it grants no capability, only visibility. `names` MUST come from `packages/schema/events.schema.json`, and the
catalogue parity between that schema and `docs/architecture/36-observability.md` is checked by
`packages/schema/scripts/verify.mjs` (see `packages/schema/README.md`).

**`core.start` / `core.stop` / `core.restart`** — converge class. `core.start` requires capability
`integrity.pinned-core` (ADR-0019): a Core binary that has not passed hash-and-signature verification cannot
be started, and `integrity.mismatch` is a progress event on this method so the UI can show the reason rather
than a dead proxy. `core.start` result carries `inboundPorts`, which is where the loopback-only assertion of
ADR-0006 is reported — a non-loopback bind is a start failure, not a warning. `core.restart` requires a
`confirm` argument (`packages/schema/protocol/commands/core.restart.json`) because it drops tunnels.

**`core.status`** — running, PID, and the applied config hash; the cheap probe a keepalive watchdog uses.
Four errors: the smallest surface that can still report a containment problem.

**`core.select`** — removed 2026-11-14 by §9.5's answer (inbound-per-tag pins the server into the config bytes; see §2.1). The row below records what it once promised.
`CoreNotRunning`; it MUST NOT be able to introduce a server that `config.apply` did not load.

**`config.apply`** — fourteen errors, the widest surface, because it is where compilation, keystore access,
disk write and process spawn meet. `dryRun: true` (`packages/schema/protocol/commands/config.apply.json:42-45`)
is the only safe way for the UI to preview a change or surface unrepresentable rules. The `unrepresentable[]`
result field (`packages/schema/protocol/commands/config.apply.json:81-94`) is the machine-readable form of
ADR-0013's "report instead of drop": a browser-plane rule that Xray routing cannot express is returned as
`{source, reason}`, never silently discarded. **Verified as data; the behaviour is `Verified: NO — deferred to
Phase 2`** because nothing has answered a `config.apply` with a report *in a session*: the Compiler fills the field
and its bounds (`connector/internal/compile/split.go`), the command surface carries it into the answer
(`connector/cmd/nexora-connector/config_apply.go`), and `config_apply_test.go` shows two entries arriving in one
frame with the config still applied. What no test in this repository can show is a browser that reports rather than
drops one.

**`config.snapshot`** — returns `appliedConfigHash`, `matchesExtensionHash`, `serverTags`, and optionally a
masked config (`includeMaskedConfig`, `maxBytes`). This is the drift-detection primitive: the extension holds
the hash it last applied and asks whether reality matches. Masking is `SecretRedacted`-safe by construction —
`maskedConfig` MUST NOT contain any value the keystore holds, which is the invariant of
`docs/architecture/32-secrets-keystore-masking.md` and `docs/adr/0010-data-split-extension-intent-connector-truth.md:45-48`.

**`profile.import`** — see §2.2. `mode` distinguishes merge from replace; `backupBeforeWrite` exists because a
user-data rewrite is not a debug operation. Note the honest gap: `serverIds` in this method's args are **not**
validated against the stored Server set — JSON Schema cannot express cross-entity referential integrity, so
that check is a `ProfileRegistry` invariant owned by `docs/architecture/22-component-contracts.md`, and it is
now implemented at load: `nexora/extension/src/profiles/registry.ts` refuses a profile whose `serverIds` name
no `-nexora.server.*` key, naming profile and server. The Connector half of the same refusal is
`profile.import`'s own payload check.

**`profile.remove`** — `backupBeforeDelete` is required and `blockingReferences` is a result field: a removal
that would orphan a routing rule returns the blockers instead of performing a partial delete
(`packages/schema/protocol/commands/profile.remove.json`).

**`profile.validate`** — pure, no side effects, ten seconds, `compilePreview` asks the compiler what Xray JSON
would result without applying it. This is the method the routing editor calls on every keystroke, which is why
it is unprivileged and non-mutating.

**`subscription.refresh`** — 120 000 ms, the longest deadline, because it is a fetch plus a parse per source. Its
args carry the whole `SubscriptionSource` documents rather than identifiers (`packages/schema/protocol/commands/subscription.refresh.json#/args/properties/sources`):
the Connector owns no user intent, so it cannot look a source up, and passing the entity is the only shape that
does not create a second copy of the contract. It declares `network-egress`, which is why it is rate-limited and
why it is privileged: the master prompt forbids a UI bug from hammering a VPS, and
`docs/architecture/31-security-model.md` turns that into a concrete per-sender budget. Its result splits
`refreshed` / `unchanged` / `failed` / `skipped` — a `failed` entry MUST NOT have removed the last good data, which
is the durable-status rule of `docs/audit/09-subscriptions-import-export-sync.md`, and a `refreshed` entry carries
the parsed Servers as keystore references, never as values
(`docs/architecture/29-subscriptions-and-servers.md` §2).

**`server.test`** — `class: probe`, `replayable: false`, required args `serverId` and `layers`. The layer list
is the reason this method exists: DNS, TCP, Core-outbound and proxied-HTTP are four different claims, and a
"connection succeeded" that only proved TCP is the exact error the master prompt calls out. `egressProbe` is an
arg so the assertion "traffic left through the VPS" is part of the request, not an afterthought — and the
`matchesExpectedEgress` field in its result is what `LIVE-04` of `docs/verification/live-matrix.md` requires
before any row may be marked passed (ADR-0020).

**`diagnostics.get`** — privileged because it returns a dump. `format`, `include`, `maxBytes`, `sinceSeq` bound
it; `truncated` and `masked` are result fields, so a caller can tell a short answer from a cut answer. It
MUST NOT be able to emit an unmasked secret (`docs/adr/0010-data-split-extension-intent-connector-truth.md`).

**`keystore.purge`** — `scope` + `confirmToken` required; `dryRun` available; result `purged` / `retained`.
The `confirmToken` is not decoration: it is the same mechanism as ADR-0009's `failOpen` confirmation, so an
accidental "delete all credentials" is structurally impossible from a UI bug.

## 4. What this surface deliberately does not contain

- No generic `invoke`, no `eval`, no path argument, no URL chosen by the caller for local execution
  (ADR-0007). A method that took a shell string would make the allowlist decorative.
- No `server.add` / `server.remove` / `profile.save`. Entities move through `profile.import` (validated
  whole-set replace) and `profile.remove` (reference-checked delete); a per-field save would create a second,
  unvalidated write path around `packages/schema/server.schema.json`.
- No method that takes a URL and fetches it. `subscription.refresh` fetches only what a `SubscriptionSource`
  document says, and that document had to be created by the user in the options UI — so the Connector is never a
  general HTTP forwarder, which is what would make the SSRF guards decorative
  (`docs/architecture/29-subscriptions-and-servers.md` §2). Who owns the *result* of a refresh is PROJECT-STATE §9
  question 4; who owns the *fetch* is settled here: the Connector, because it is the only process that can write
  the keystore.
- No log-streaming method. Events go through `connector.events.subscribe`; `diagnostics.get` is a snapshot.
  A streaming log endpoint would be an unbounded, unmasked egress of state.
- No `chrome.proxy`-shaped method. The browser-plane decision stays in the extension
  (`docs/audit/06-proxy-application.md`), and the Connector never learns which tab is active
  (`docs/architecture/34-data-split.md` §10 rule 7).

## 5. Verification status of this document

| Claim in this file | Checked by | Status |
|---|---|---|
| The document names exactly the allowlist's eighteen methods | `packages/schema/scripts/verify.mjs` "docs/architecture/26-command-surface.md names the same set" (parses this file) | **Verified** — the DEFERRED note became an assertion when this file landed |
| Method class / hello-gating agree between allowlist and command doc | `packages/schema/scripts/verify.mjs` "hello gating agrees between allowlist and command doc", `packages/schema/scripts/verify.mjs` "command class agrees between allowlist and command doc" | **Verified** |
| Every cited error code exists in the taxonomy | `packages/schema/scripts/verify.mjs` "command ${doc.name} cites only taxonomy codes" | **Verified** |
| Every cited capability exists in the compatibility rules | `packages/schema/scripts/verify.mjs` "command ${doc.name} cites only declared capabilities" | **Verified** |
| `probe` is non-replayable | `packages/schema/scripts/verify.mjs` "probe is non-replayable: ${doc.name}" | **Verified** |
| Chunk mirror equals envelope chunk shape | `packages/schema/scripts/verify.mjs` "the envelope chunk shape and profile.import args.chunk are one shape" | **Verified** |
| Chunk groups in the fixtures are complete | `packages/schema/scripts/verify.mjs` "a complete chunked import reassembles in the fixtures" | **Verified** |
| `config.apply` restarts only on hash change | no code exists to run | **Verified: NO — deferred to Phase 2** (golden config test, `docs/architecture/27-config-compilation-pipeline.md`) |
| A fast switch never restarts Core | `config.apply`'s own `unchangedBecauseHashMatched`/`restarted` fields; the golden pair proves the compiled hash is byte-stable across a re-select | **Verified** (the switch is one, per §2.1; `VER-30` retired with the decision) |
| `profile.import` rejects a dangling `serverId` | the payload check in the Connector's handler; the load-time check is `nexora/extension/src/profiles/registry.ts`'s `loadRegistry`, tests naming profile and server | **Verified** (registry load) / Connector-side on `profile.import` |
| `diagnostics.get` output contains no secret | fixtures cannot prove absence over a real keystore | **Verified: NO — deferred to Phase 2** (`LIVE-10`) |
| `server.test` layer results are not replayed | static half green: assertions "probe is non-replayable: server.test" and "probe is answered exactly once" over `packages/schema/fixtures/protocol/` | **Verified: NO — deferred to Phase 2** for the runtime half (rides on `LIVE-05`) |
| `config.snapshot` can say whether reality matches the hash the extension holds | `packages/schema/protocol/commands/config.snapshot.json#/args` declares two properties and `additionalProperties: false`, so the expectation `matchesExtensionHash` is computed against cannot be sent; this method still has no handler, while `core.status` now reports the supervisor's process and persisted state | **Verified: NO — deferred to Phase 2** (`VER-90`: the field, then the handler, then a run that detects a drift — `LIVE-06` names that check) |

Trace: ADR-0004, ADR-0005, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0013, ADR-0019, ADR-0020 ·
Contract: `packages/schema/protocol/command-allowlist.json`, `packages/schema/protocol/commands/`,
`packages/schema/protocol/envelope.schema.json`, `packages/schema/protocol/error-taxonomy.json`,
`packages/schema/protocol/framing.json`, `packages/schema/protocol/compatibility-rules.json` ·
See also: `docs/architecture/25-native-messaging-protocol.md`,
`docs/architecture/22-component-contracts.md`, `docs/architecture/39-verification-matrix.md`
