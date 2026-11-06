# 36 — Observability: what a log line is, where it may rest, and what may never be in one

> Purpose: `docs/architecture/22-component-contracts.md` §11 gives one component a normative contract that is
> neither a command document nor a data catalogue — DiagnosticsCollector points here — and the audit found the
> inherited logging system failing in four independent ways: a mask keyed on field *names* that omits the two
> fields that carry credentials (`docs/audit/11-security-findings.md` SEC-7), a buffer with no bound at all, a
> console hook that never applies to a bare string, and two export paths that ship whatever they hold to a public
> bug tracker. Meanwhile six documents deferred eleven specific facts to this file: which events exist and what
> each one carries, where a record may rest, which block port is in force, what the Core's own log section says,
> how long anything is kept, and what a migration must not strip. This document states them, and
> `packages/schema/observability.json` states them again as data, cross-checked against the six contracts the
> facts come from.

Status: Accepted. Normative data: `packages/schema/observability.json`,
`packages/schema/events.schema.json#/$defs/name`, `packages/schema/events.schema.json#/$defs/catalog`,
`packages/schema/protocol/envelope.schema.json#/properties/result/description`,
`packages/schema/protocol/commands/diagnostics.get.json#/args/properties/include/items/enum`,
`packages/schema/protocol/commands/connector.events.subscribe.json#/args/properties/maxPerSecond`,
`packages/schema/connector-settings.schema.json#/properties/telemetry/properties/logLevel`,
`packages/schema/connector-settings.schema.json#/properties/telemetry/properties/retentionDays`,
`packages/schema/protocol/data-ownership.json#/stores`,
`packages/schema/examples/settings.default.json#/telemetry/diagnosticsIncludeByDefault`,
`packages/schema/examples/failure-policy.default.json#/block/portRange`.
Evidence base: `docs/audit/11-security-findings.md` (SEC-1, SEC-7),
`docs/architecture/22-component-contracts.md` §11, `docs/architecture/26-command-surface.md`,
`docs/architecture/27-config-compilation-pipeline.md` §7, `docs/architecture/28-routing-model.md` §7,
`docs/architecture/30-failure-policy.md` §4 and §8, `docs/architecture/32-secrets-keystore-masking.md` §8–§10,
`docs/architecture/34-data-split.md` §10, `docs/architecture/35-schema-versioning-migration.md` §13,
ADR-0009, ADR-0010, ADR-0011.

---

## 1. What this document owns

Owns:

1. **The record** — the shape every NEXORA log line is rendered from, its eleven fields and each of their bounds
   (§3). This is the reason the rest is possible: a policy that cannot name a field cannot police it.
2. **The sink list** — the closed set of places a record leaves the process, and the stage at which masking
   applies to each (§5). `docs/architecture/32-secrets-keystore-masking.md` §8 argues *why* masking lives at a
   sink; this names *which sinks*.
3. **The event catalogue** — one entry per registered event name, with its channel, retention, sampling, delivery
   mode, follow-up command and payload schema (§6).
4. **What `diagnostics.get` returns** — the ten sections, which are on by default, which store each reads and what
   none of them may contain (§8), including the answer doc 30 §8 asked for about the block port.
5. **The Core's own `log` section** — what the Compiler writes there, and the one field it must refuse to leave to
   an upstream default (§9).
6. **Correlation** — who mints a trace id, what may live inside one, what crosses the plane boundary unchanged,
   and what a migration must not strip (§7).
7. **Retention, rotation and export** — three caps per sink, what `retentionDays: 0` actually means for a file,
   the three things that never expire (§10), and the one sink whose audience is not this machine (§11).

Refuses:

- **The masking algorithm.** Value-keyed redaction, the mask literal, the fingerprint scheme and the
  `SecretRedacted` semantics are `docs/architecture/32-secrets-keystore-masking.md` §8–§11. This document only
  says at which of its own sinks the pass runs.
- **The frame.** An event rides on `packages/schema/protocol/envelope.schema.json`; that schema owns the field
  rules and the `progress` shape. This document owns what `result` carries when `type` is `event`, which is why
  the envelope's own `result` description points here instead of repeating it.
- **The decision.** Which outcome a failure takes is `docs/architecture/30-failure-policy.md`. A log reports the
  outcome that was taken; it never owns it. That distinction is why `core.blocked` carries a `policy` field and
  no rule engine.
- **The wording.** A user-facing sentence, including every label on a diagnostics panel, is
  `docs/architecture/37-ui-architecture.md` §4.
- **Whether any of it was executed.** `docs/verification/live-matrix.md` and
  `docs/architecture/39-verification-matrix.md` own that, and this file marks its own unmeasured claims the same
  way.

## 2. The questions this document was cited to answer

Every row below was a forward reference found by grepping the tree for this file's name. Each now has a section.

| Cited by | The question | Answered in |
|---|---|---|
| `docs/architecture/22-component-contracts.md:27` | DiagnosticsCollector's contract column names this file — what is the contract? | §3–§11; `packages/schema/observability.json` is its data half |
| `docs/architecture/26-command-surface.md:139` | "the catalogue parity between that schema and `docs/architecture/36-observability.md` is checked by `packages/schema/scripts/verify.mjs`" | §6, asserted as *"the observability catalogue is exactly the registered event set"* |
| `docs/architecture/27-config-compilation-pipeline.md:155` | `config.apply` results are logged, so `source` must never be a raw condition | §6, `config.applied` |
| `docs/architecture/28-routing-model.md:109` | rank 5 should be unreachable; "if it is reached, the diagnostics event says so" | §6, `routing.decision` |
| `docs/architecture/30-failure-policy.md:262` | "which port is in force, and why, belongs in the `failure-policy` section" | §8's `inForceBlockPort` |
| `docs/architecture/32-secrets-keystore-masking.md:40` | "the log format and event catalogue" | §3, §5, §6 |
| `docs/architecture/32-secrets-keystore-masking.md:555` | the contract list doc 32's Trace line hands here | §5, §8, §11 |
| `docs/architecture/34-data-split.md:46` | where the log rests, and the legacy `+`-key behaviour that must not change | §5 (`browser-ring`), §10 |
| `docs/architecture/35-schema-versioning-migration.md:442` | the "`traceId`-shaped correlation history the user may still need to explain a past incident" | §7 |
| `docs/architecture/35-schema-versioning-migration.md:541` | whether the secret census counts are user-visible or diagnostics-only | §8, and §16's open item 1 |
| `packages/schema/protocol/data-ownership.json#/stores` | why a memory ring cannot answer why the Connector exited; why the log is capped three ways | §5, §9, §10 |
| `packages/schema/protocol/envelope.schema.json#/properties/result/description` | where an event frame's payload shape is declared | §6 |
| `packages/schema/migrations.json#/openDecisions/2` | the census question, asked again from the ledger | §16 |

## 3. A log line is a record, or it cannot be policed

Legacy logs objects. `omega-target/src/log.coffee:4-11` defines a `replacer` keyed on field names and applies it
at `omega-target/src/log.coffee:32`, and `omega-target-chromium-extension/overlay/log.js:45-58` does the same for
the browser's console hook. Both fail the same way, through three distinct mechanisms, each recorded in
`docs/audit/11-security-findings.md` SEC-7:

1. The name list has no entry for `headers` or `auth` — the two fields that actually carry credentials — while it
   does mask `host` and `port`, which are not secrets and which the operator then cannot read.
2. The replacer never runs on a string: `omega-target-chromium-extension/overlay/log.js:64-65` passes
   `typeof obj == 'string'` straight through, and only objects reach `JSON.stringify`. Every interesting secret
   that has ever appeared in a log appeared inside a sentence.
3. Both paths are reached through `console.log`, which the overlay rebinds at
   `omega-target-chromium-extension/overlay/log.js:115-116`; the background singleton is a direct
   `console.log` binding (`omega-target/src/log.coffee:45`), and the global error handler appends a raw stack
   (`omega-web/src/coffee/log_error.coffee:6`). Text written by any of those three is filtered by nobody.

A record fixes all three at once, because it changes what a log *is*: not prose with values in it, but a flat
object with eleven declared fields, of which exactly one — `msg` — is free text, and that one is capped at 500
characters. The vocabulary is `packages/schema/observability.json#/record/fields`; the argument for five
particular choices in it:

- **Flat, with no undeclared field.** A nested object is where a secret hides from a field-name policy, and
  value-keyed masking on a shape nobody declared is guesswork. A renderer that receives a key outside the list
  drops it and counts the drop in
  `packages/schema/protocol/commands/diagnostics.get.json#/result/properties/masked` — not because the key is a
  secret, but because a field nobody declared has nobody owning its content.
- **`seq`, not `ts`, orders a log.** `ts` is the sender's clock and the two planes have two clocks; the frame
  already states the same rule at `packages/schema/protocol/envelope.schema.json#/properties/sentAt/description`.
  `seq` is monotonic per sink per plane and is also the gap detector: a ring that evicted and a ring that lost
  records look identical unless `seq` says otherwise.
- **`code` from the taxonomy, never a rendered message.** `protocol/error-taxonomy.json#/codes` is the closed set,
  and the reason is defect three above: the text of a fetch failure is built from the URL that failed, and a URL
  in this product is frequently a credential (`docs/architecture/29-subscriptions-and-servers.md` treats
  tokenised feed URLs as secret-bearing).
- **`method` from the allowlist.** `protocol/command-allowlist.json#/commands` is closed, so a log line cannot
  describe a call that does not exist — the failure mode of a free-string method field.
- **`plane` says `extension` where the catalogue says `browser`.** `packages/schema/protocol/data-ownership.json#/planes`
  spells its third value `browser` because there it disambiguates a storage medium. In a log line "browser" is
  ambiguous between the browser *process* and the browser *plane*, and a reader of a line wants the process. The
  difference is deliberate and asserted nowhere, so it is recorded here rather than "fixed" in Phase 2.

Four rules sit on top of the shape, and the last of them is what makes the other three cheap:

1. One render, every sink. A record is written to each sink in its plane from the same object; two renders of one
   fact can disagree, and a log that disagrees with itself is worse than a short log.
2. A record about a decision is written after the decision commits. A record of a rolled-back `config.apply`
   states something that did not happen, and `docs/architecture/35-schema-versioning-migration.md` §8 is a
   recovery procedure that *reads the log to decide what already happened*.
3. Nothing is logged at a call site by hand. Components append records; sinks decide what leaves the process. This
   is `docs/architecture/32-secrets-keystore-masking.md` §8's "a sink cannot be forgotten — there are a fixed
   number of them", and §5 of this document is that fixed number.
4. `component` is one of the fourteen names in `docs/architecture/22-component-contracts.md` §0, and its cap of 40
   characters is `packages/schema/protocol/envelope.schema.json#/properties/progress/properties/stage/maxLength`,
   so a component that fits the log always fits the frame that reports it.

## 4. Levels, and the one word the two vocabularies disagree on

The level vocabulary is not a NEXORA invention: it is
`packages/schema/connector-settings.schema.json#/properties/telemetry/properties/logLevel` — `error`, `warn`,
`info`, `debug` — and `packages/schema/observability.json#/levels/names` restates it so the verifier can compare
the two. Xray's vocabulary is the same four words except for `warning`.

That single mismatch deserves its own section because of *how* it fails. A config that passes `warn` through into
`log.loglevel` is not rejected loudly; Core starts, and the level the operator asked for is not the level in
force. That is the worst class of defect this product can have: the apply succeeds, and the thing nobody reads
silently reverts to a default. So the Compiler maps — `packages/schema/observability.json#/levels/toCore`, with
`warn` becoming `warning` — and the assertion is *"the one word NEXORA and Xray disagree on is mapped by the
Compiler, never passed through"*.

Two refusals follow from the same instinct:

- **No `trace` level.** A level with no bound on what it emits cannot have a bound on its size, and size is what
  §5 and §10 are about. `debug` already carries the widest licence the design grants: per
  `packages/schema/connector-settings.schema.json#/properties/telemetry/properties/logLevel/description` it
  additionally logs the compiled config structure with every secret field replaced — which is precisely why the
  mask runs at the sink rather than at the code that builds that structure.
- **No per-component verbosity.** It would be a second configuration surface with no owner, no schema and no UI
  row, and its absence costs nothing: `component` is already a field on every record, so a reader who wants one
  component filters instead of reconfiguring.

## 5. The five sinks

`packages/schema/observability.json#/sinks` is a closed list of five, and the verifier refuses a sixth by
asserting *"the sink list is closed and every sink has one name"*. This section is the argument for each row and
for the shape of the list.

| Sink | Plane | Medium | Durable | Bound | Masking stage |
|---|---|---|---|---|---|
| `connector-ring` | connector | memory | no | 2000 records, 1 MiB | at append |
| `connector-log-file` | connector | disk | yes | 5 MiB × 5 files, aged | before the write |
| `core-log` | core | disk | yes | 1 MiB × 2 files | capture into the ring |
| `browser-ring` | browser | IndexedDB | yes | 2000 records, 2 MiB | at append |
| `export-file` | browser | download | yes | 4 MiB | final pass over the artefact |

**Why a memory ring is not enough, and why it stays.** The ring is the answer to "right now", and its byte cap is
exactly `packages/schema/protocol/framing.json#/limits/effectiveCrossBrowserBytes` — one chunk, so the answer to a
question about the immediate past needs no reassembly. But a ring cannot answer the one question that matters
after the fact: *why did the Connector exit?* A crash-loop is invisible without a record written by the run that
died. That is the whole of `connector-log-file`, and it is also the row that makes `retentionDays` mean
something: an age in days cannot apply to a buffer that dies with its process, so before this sink existed
`retentionDays: 30` and `retentionDays: 0` described identical behaviour. The durable half of
`docs/architecture/34-data-split.md` §10 rule 5 is respected by keeping the file a *log* rather than a state copy:
nothing reads it to make a decision.

**Why `core-log` is read-only to NEXORA.** Xray writes a file whose format the design does not control and
rotates it on an interval the design does not configure. Making that sink owned would mean parsing it; making it
a bounded tail read, masked on its way into the ring, means a Core-side format change cannot become a NEXORA leak.
The redaction pass runs over the text and looks for every value the keystore holds
(`docs/architecture/32-secrets-keystore-masking.md` §8, "record-unit, not line-unit"), so a line it cannot
interpret is still scanned. §9 owns what the Compiler writes into that file in the first place.

**Why `browser-ring` is IndexedDB and not `chrome.storage.local`.** A security decision wearing the costume of a
storage preference. Keys in `chrome.storage.local` are envelope keys, so a diagnostics ring kept there would be:
synced (SEC-1's home), counted by the key count a migration backs up
(`packages/schema/migrations.json#/run/abortOnZeroKeyCount`), exported by a profile export, and migrated. A
diagnostic buffer must be none of those four things. The medium is inherited from the part of the legacy design
that was right — `omega-target-chromium-extension/overlay/log.js:2` creates its own `log-store` — and the
unboundedness of that store is what §10 fixes: its rotation is by ISO day of week
(`omega-target-chromium-extension/overlay/log.js:16`), which bounds days and not bytes, and whose day-rollover
path (`omega-target-chromium-extension/overlay/log.js:28-30`) discards the previous day's contents by
overwriting them rather than by rotating a file.

**Why the export gets the last pass.** It is the only sink whose audience is not this machine. Legacy got exactly
this wrong twice, in two directions: the About page ships `localStorage['log']` whole
(`omega-web/src/omega/app.coffee:90-93`) — a key the global error handler had already filled with raw stack text
(`omega-web/src/coffee/log_error.coffee:4`) — and the debug page zips the IndexedDB entries with no further
redaction (`omega-target-chromium-extension/src/coffee/omega_debug.coffee:17-35`). Both are user actions, which is
the one part of that design worth keeping; both lacked the pass this row adds.

**How this relates to doc 32's list.** `docs/architecture/32-secrets-keystore-masking.md` §8 enumerates six
surfaces: the ring, `diagnostics.get`'s payload, a structured error's `detail`, `config.snapshot`'s
`maskedConfig`, an exported file, and a UI text node. That list is about *where a secret could leave*; this one is
about *where a record rests and is handed over*. Four of doc 32's six are renderings of records rather than sinks
— a diagnostics response, an error detail, a masked config and a text node are all views of §3's object — and two
of this list, `connector-log-file` and `browser-ring`, are stores doc 32 had no reason to name. The pointer that
closes the list is now `packages/schema/observability.json#/sinks`, and the assertion *"every log-backed store has
a sink that masks it, and the only store-less sink is the export"* keeps the two documents from meaning different
things by the word "sink". A UI text node stays outside this document's ownership: it is the one surface where
masking is a rendering concern, and `docs/architecture/37-ui-architecture.md` §4 and doc 32 §9 own it.

## 6. The event catalogue

`packages/schema/events.schema.json#/$defs/name` registers fourteen names;
`packages/schema/observability.json#/events` describes all fourteen and nothing else, and the verifier checks the
equality in both directions. `packages/schema/events.schema.json#/$defs/catalog` declares what an entry must look
like — `name`, `channel`, `retained`, optional `sampled`, `payload` — and each entry here, projected onto that
shape, validates against it; the extra fields in this document (`delivery`, `detailCommand`, `why`) are commentary
on the entry, never a second authority. That projection is asserted as *"every catalogue entry is a valid instance
of the catalog shape the event schema declares"*, every payload is a closed object, and every `required` key is
checked against its own `properties`.

Three decisions structure the catalogue.

**An event is a hint, never an answer.** Twelve of the fourteen are `push`: they state what happened, and the
state itself is read from the command named in `detailCommand`, whose existence the verifier confirms (*"every
event names a real command for its follow-up"*). Two — `server.health.changed` and `subscription.updated` — are
`push-then-pull`, and their payloads are deliberately smaller than they could be: `server.health.changed` carries
a `serverId` and nothing else, because `docs/architecture/30-failure-policy.md` §4 forbids a cached health answer
while a block is in force, and an event carrying a state value *is* a cached answer arriving unasked.
`packages/schema/protocol/data-ownership.json#/movements` says the same from the other side: a missed event is
never a state change. The general rule is machine-checked as *"no event payload carries a state, a destination or
a rendered message that a read command also reports"*.

**`routing.decision` is the only sampled event, and its `rank` is bounded to the two rows this process can see.**
`docs/architecture/28-routing-model.md` §7 numbers six priority ranks, of which 0–3 are decided inside the
extension; a Connector-owned event registry cannot announce a decision the Connector never witnessed, so those
ranks appear as records in `browser-ring` and 4 and 5 are the only values the event may carry. Rank 5 is the
interesting one: the Compiler emits an explicit `outboundTag` for every policy, so reaching 5 means one did not
exist — a routing policy the compiler could not fully place. That is the fact
`docs/architecture/28-routing-model.md:109` deferred here, and a `null` `ruleIndex` alongside `rank: 5` is the
pair worth noticing. The event is `sampled: true` and `retained: false` because it is the only chatty event in the
system; `packages/schema/protocol/commands/connector.events.subscribe.json#/args/properties/maxPerSecond` exists
solely for it (default 10, ceiling 50, asserted as *"one sampled event, one rate ceiling, and the ceiling exists
for it"*), and a chatty event that is also retained is a replay storm for a service worker that has just
restarted. What it must never carry — destination host, port, URL, SNI, client IP — is §12.

**The payload proves a requirement, in two places.** `subscription.failed` carries `keptLastGood` as a `const
true`. The requirement is that a failed refresh must not delete the last working version; a payload without that
field would leave the requirement visible only to whoever read the source. `core.blocked` carries `inForcePort`
for the same reason: ADR-0009's visibility rule cannot be met by a badge alone, and a user who can see which
closed loopback port the browser was pointed at can verify it themselves. Both are exercised by recorded frames —
`packages/schema/fixtures/protocol/failure-block.jsonl` for the block,
`packages/schema/fixtures/protocol/subscriptions.jsonl` for the failure — and every event frame in the corpus is
validated against the payload its own entry declares, which is what makes this catalogue more than a list.

Two entries have no recorded frame, and the verifier asserts that the set of unrecorded events is exactly those
two (*"the only events with no recorded frame are the two that cannot have one on this pipe"*), because each
absence is a fact about the design rather than an accidental gap: `connector.disconnected` is emitted by the
browser plane because a process cannot report its own exit through a channel it no longer owns, and
`keystore.unavailable` needs a locked, corrupt or session-changed store, which is a live-OS condition
(`docs/architecture/32-secrets-keystore-masking.md` §11), not a fixture.

The fourteen, as a list a reader can check against
`packages/schema/examples/events.catalog.json#/events`: `core.started`, `core.exited`, `core.restart`,
`core.blocked`, `config.applied`, `config.drift`, `server.health.changed`, `subscription.updated`,
`subscription.failed`, `routing.decision`, `connector.connected`, `connector.disconnected`,
`keystore.unavailable`, `integrity.mismatch`.

## 7. Correlation: one id per episode, across two planes

A trace id exists so that "I clicked apply and nothing happened" is answerable with one string rather than a time
window. `packages/schema/events.schema.json#/$defs/traceId` owns the pattern;
`packages/schema/observability.json#/correlation` owns the rules, four of which are worth arguing:

- **Who mints.** The extension mints for anything that begins as a user action; the Connector mints for anything
  that begins on its own — a scheduled refresh, a probe, a Core exit. A run with no browser involved still has to
  be findable in a log.
- **It is not the idempotency key.** `packages/schema/protocol/envelope.schema.json#/properties/id` serves
  ADR-0008's replay rule and is per request; `traceId` is per *episode* and shared by every request inside it.
  Conflating them would either break replay or make a trace cover exactly one call, which is the useless case.
- **The character class excludes `.`, `:`, `/`, `@` and every character that appears in a hostname, URL, email or
  token.** That is not hygiene. A free-text correlation field is the one place a developer will write an email
  address and a port into a log line, and a pattern is what makes that impossible rather than merely discouraged.
- **Nothing may be derived from user data.** A trace id is random or counter-based, never a hash of a server name
  or a URL: a hash of a secret is a secret the log holds forever, and the redaction table is built from live
  keystore values, not from their digests (`docs/architecture/32-secrets-keystore-masking.md` §9 forbids the
  digest form for the same reason).

The trace *index* — `traceId` to the ordered set of records carrying it, across both planes — is rebuilt on read
and never stored, because a stored index is a second copy of state (`docs/architecture/34-data-split.md` §10 rule
5) and would need a migration of its own. It ages out with its records: a trace whose every record has rotated
away is not kept as a stub, because a stub that cannot be read is a promise nobody can keep.

This is the item `docs/architecture/35-schema-versioning-migration.md` §13 names as what a run must **not** strip:
a user explaining a past incident needs the correlation history, and stripping it would let the envelope claim a
recovery it cannot provide. Cross-plane, the string is not re-minted — the Connector does not prefix it, because
re-minting at the boundary is how a support conversation ends with two logs that cannot be joined, and
`docs/architecture/25-native-messaging-protocol.md` §2 says the shim is a pump, not a translator. The trace id's
character rules are the wire protocol's non-goal as well, in
`docs/architecture/25-native-messaging-protocol.md` §12.

## 8. What `diagnostics.get` returns

`packages/schema/protocol/commands/diagnostics.get.json#/args/properties/include/items/enum` lists ten section
names. `packages/schema/observability.json#/diagnosticsSections` has exactly ten entries, the sets are asserted
equal in both directions, and each entry names one store from
`packages/schema/protocol/data-ownership.json#/stores` — one vocabulary for "where the bytes rest", never two.

| Section | Plane | Store | Default | Cap |
|---|---|---|---|---|
| `core-log` | core | `core-log-file` | yes | 256 KiB |
| `connector-log` | connector | `diagnostics-ring` | yes | 512 KiB |
| `events` | connector | `diagnostics-ring` | no | 128 KiB |
| `config-snapshot` | connector | `compiled-config` | yes | 128 KiB |
| `health` | connector | `health-history` | yes | 64 KiB |
| `inbounds` | core | `applied-state` | no | 32 KiB |
| `versions` | connector | `applied-state` | yes | 8 KiB |
| `pins` | connector | `connector-pin-mirror` | no | 32 KiB |
| `routing-decisions` | connector | `diagnostics-ring` | no | 64 KiB |
| `failure-policy` | browser | `extension-log` | no | 32 KiB |

**The default set, and why four sections are not in it.** The five defaults are exactly
`packages/schema/examples/settings.default.json#/telemetry/diagnosticsIncludeByDefault`, and the assertion *"the
sections the catalogue marks default-included are exactly the sections the shipped settings ask for"* is what
stops a contract change in one file from quietly altering what a support bundle contains. Each exclusion has a
reason: `events` is history the `connector-log` records already cover; `pins` is only interesting when something
is refusing to start; `routing-decisions` is the sampled, chatty one; and `inbounds` is the section most likely to
surprise a user — a loopback bind is the rule of ADR-0006, and seeing the list is how you check it. `inbounds` is
therefore on the *diagnostics* path and not on the *default* path, the same judgement
`docs/architecture/31-security-model.md` applies to every other confirm-before-reveal control.

**`empty` and `unavailable` are different sentences.**
`packages/schema/protocol/commands/diagnostics.get.json#/result/properties/sections/items/properties/state/enum`
has four values, and each section here carries an `emptyMeans`, because "empty" is only interpretable against what
would have made it non-empty. `versions` empty is impossible — a Connector that cannot answer it has nothing to
say. `health` empty means no probe has run, which is why a UI that renders green from an empty health section is
the fake success `docs/verification/live-matrix.md` LIVE-04 exists to forbid. `pins` empty means Core cannot start
(`docs/architecture/33-binary-integrity.md`), which is a fact about the present, not an absence of history.

**Which block port is in force.** `docs/architecture/30-failure-policy.md` §8 asked this document: within the
block range, one port at a time, chosen deterministically, not round-robin. The rule is *the lowest port in
`packages/schema/examples/failure-policy.default.json#/block/portRange` that the `assertClosed` probe found
closed, evaluated once when a block is written and never re-rolled while that block is in force*.

The two halves of that rule buy two different things. *Lowest and once* is what makes reconnect reconciliation
work: the port in force is part of what the comparison reads (doc 30 §8 again), so a port chosen from anything
other than a fixed rule would differ between two runs of the same inputs and would look like drift on every
reconnect, turning a working block into a reported fault. *The range, rather than one port*, is what keeps
`assertClosed` honest: a block aimed at a port some other local process owns turns that process into the user's
proxy, receiving every hostname, so an occupied port must be skipped rather than adopted. Ascending order buys
both at once — the answer is computable from the range alone, and no listener is ever inherited. If every port in
the range is occupied the block is **refused** and the refusal is the reported state: there is no "blocked
anyway" and no silent widening, because a range the design does not control is a port the design cannot name.

The answer is reported in three places so no reader has to ask in the right one: as `core.blocked`'s
`inForcePort` (the event a subscriber gets without asking), as the `failure-policy` section's
`{inForcePort, portRange, probed, refusal}` from `packages/schema/observability.json#/inForceBlockPort`, and as
`connector.health`'s `blocked` answer, which is the state `chrome.proxy.settings` actually holds. `probed` lists
each port examined with its result, so a refusal reads as "another process holds 20900" rather than as "the range
is misconfigured". Disjointness from the inbound range is asserted, not assumed: *"the in-force block port comes
from a range disjoint from every inbound range"*.

**The masked counters are part of the answer.**
`packages/schema/protocol/commands/diagnostics.get.json#/result/properties/masked/properties/fingerprints` and its
`fieldsReplaced` appear in every response, because a bundle that has been redacted is a bundle that must say so;
§11 carries the same two into the exported file for the same reason.

## 9. The Core's own log, and the access log that must not exist

Xray has its own `log` section — `loglevel`, `access`, `error`, `dnsLog`, `maskAddress` — and the Compiler writes
it as part of every config it emits. Shape A of `docs/architecture/23-unified-server-model.md` §9 shows it with
`loglevel`, `access` and `dnsLog` all stated; shapes B and C abbreviate the section away because they are
illustrations of the outbound, and the rule is that no emitted config omits it.

`access` is the load-bearing field and it is **not a user setting**.

An access log is one line per connection: the destination, the inbound it arrived on, the outbound chosen. Xray
writes one by default, and `none` is the documented way to disable it. On a home user's disk, unbounded,
describing everywhere they browsed, that artefact is the precise opposite of what this product is for — and it
would be produced by the *default*, meaning a build that never thought about it would ship it anyway. NEXORA's
answer to "why did this connection go direct" is §6's `routing.decision` plus the `routing-decisions` section:
masked, bounded, sampled, and never naming a destination.

The reason this is stated rather than inherited is the pin document.
`packages/schema/examples/pins.xray.json` lets the Core version move, so any control left to an upstream default
moves with it — the next release's default is not this release's default, and nobody would notice. That is why
`packages/schema/observability.json#/coreLog/accessLogDecision` marks `isUserSetting: false`, why the verifier
asserts *"Xray access logging is stated off by the Compiler and is not a user setting"*, and why it also asserts
that doc 23's shapes say so out loud. If a user ever wants Xray's own file, that is an ADR about a new privacy
surface, not a settings boolean.

**Honesty about this claim.** The exact upstream wording could not be fetched from the host this document was
written on — the same limitation `docs/audit/07-routing-engine.md:425` records for the routing page — so the
`verification` field carries it as UNVERIFIED, and the rule is written so that it holds whichever side the default
falls on: *state it explicitly*. An assertion that a field is present in the emitted config does not depend on
knowing what happens when it is absent.

`maskAddress` is left unset, deliberately. It masks IP addresses in Xray's own logs; with access logging off and
the error log carrying no destination, it would mask a file that has nothing to mask, and an address the user's
own provider already holds is in `docs/architecture/32-secrets-keystore-masking.md` §10's category of things
deliberately not masked. It is §16's open item 2 — to revisit once a live capture shows whether the error log
names peers.

The `error` path points inside the Connector's state directory, next to the Connector's own log rather than next
to the binary, so that one directory is the whole of what has to be cleaned up; and because Xray rotates that file
on an interval NEXORA does not configure, `core-log` is a bounded tail read rather than an owned file (§5).
`dnsLog` is off: a DNS log is a domain history, which is the same artefact §12 refuses under a different name.

## 10. Retention: three caps, one rotation per sink

`packages/schema/connector-settings.schema.json#/properties/telemetry/properties/retentionDays` allows 0–90 with a
default of 7, and `packages/schema/observability.json#/retention/days` restates all three numbers so the verifier
can compare them (*"the retention window is the settings window, restated and equal"*). The default inherits the
legacy intent — `omega-target-chromium-extension/overlay/log.js:16` rotates by ISO day of week and comments "keep
logs max 7 day" — and not the legacy mechanism, which bounds days and not size.

**Age, count and bytes are three caps and the first reached wins.** A single cap is not a cap: records-per-day
varies by four orders of magnitude between an idle notebook and a busy desktop, and a count-only bound on a buffer
whose records can each carry a 500-character sentence is a bound on nothing. That is why `browser-ring` states
2000 records *and* 2 MiB, and why the legacy row in `packages/schema/protocol/data-ownership.json#/stores` now
says "unbounded today" with the finding behind it rather than inventing a number it never had.

**`retentionDays: 0` means the current session only, on every sink.** The setting's own description already says
"0 keeps the current session only", and for a file that can only mean one thing: truncated at start rather than
appended. The ring keeps its count cap either way. A zero that silently meant "the default" would be the second
worst possible reading of a privacy control whose entire purpose is to have no history.

**Nothing is summarised, rolled up, or rewritten.**
`packages/schema/connector-settings.schema.json#/properties/telemetry/properties/retentionDays/description` calls
it a ring-buffer age, not an archive, and the difference matters: when a record falls out it is gone, and a design
that quietly kept a daily digest would have built the archive `docs/architecture/34-data-split.md` §10 forbids.

**Rotation runs before deletion-for-space, and reports what it removed.** "Old and the disk is full at the same
time" is the case where a log disappears with no explanation; ordering the two caps makes a missing log have an
entry in the log.

Three things never expire, because they are state rather than history and something reads them to answer a
question: the applied config hash and the last start/exit records
(`packages/schema/protocol/data-ownership.json#/stores`, the `applied-state` row, which `core.status` answers
from), the backup ledger (`packages/schema/migrations.json#/journal`, whose resume procedure *is* the journal —
ageing it out is the same mistake as not writing it), and the idempotency cache's window
(`packages/schema/protocol/data-ownership.json#/stores`, the `idempotency-cache` row, bounded by ADR-0008's replay
rules and not by a user setting).

## 11. The export, which is the only sink with a second audience

One trigger: a user action on the options page. `docs/architecture/22-component-contracts.md` §11 invariant 4 is
"nothing is uploaded anywhere; export is a user action", and no code path may create another.

The artefact is JSONL — §3's records, one per line, ordered sink → section → seq — capped at 4194304 bytes, which
is *equal to* `packages/schema/protocol/commands/diagnostics.get.json#/args/properties/maxBytes/maximum` rather
than merely large, so an export can never be bigger than what one call was allowed to produce. A truncation writes
`truncated: true` into the footer and shows it in the UI; a stated limitation is a limitation and a silent one is a
lie about the incident.

The footer repeats `packages/schema/protocol/commands/diagnostics.get.json#/result`: the three versions plus
`protocolVersion` and `appliedConfigHash`, the masked counters and fingerprints, and each section with its state.
Without those facts the file cannot say which build produced it, whether anything was masked, or whether a section
was missing rather than empty — and the response the file is a rendering of already says them. The fingerprints are
the part a support conversation runs on: they let someone say "the UUID you sent is not the one on file" without
either value appearing, which is the only way to check a user's paste without asking for the secret.

The name is `NexoraDiagnostics_<iso8601>.jsonl`: no username, no machine name, no extension id, because the file's
destination is a bug report on a public tracker — the same audience that made
`omega-web/src/omega/app.coffee:90-93` a finding.

## 12. Never logged

`packages/schema/observability.json#/neverLogged` is seven rows, each with the ADR, section or finding that places
it. The list is short and every row is load-bearing:

1. **Any value the keystore holds.** ADR-0010, ADR-0011, `docs/architecture/32-secrets-keystore-masking.md` §3.
    The legacy counter-example is one line: `omega-target/src/options.coffee:237` logs a whole options object on
    the sync path, and the overlay's replacer has no entry for `headers`.
2. **The destination of a user connection — host, port, URL, SNI.** This is the user's browsing history; the
    product exists to keep it off the network, not to write it to their disk. §9 is the same rule applied to Core.
3. **A subscription source URL that carries a token.** `docs/architecture/29-subscriptions-and-servers.md` treats
    tokenised feed URLs as credentials, and a URL in a log is a credential in a log.
4. **A sync token, a Gist id, or the user's GitHub login.** SEC-5's built-in sync is the row
    `docs/verification/live-matrix.md` LIVE-22 expects to *fail* a storage grep; it must not also fail a log grep.
5. **A path under the user's home directory beyond the Connector's own state directory.**
    `packages/schema/connector-settings.schema.json#/properties/core/properties/installRoot/description` already
    says paths are not secrets and are not logged either; a custom install root is a directory the user did not
    name in a bug report.
6. **The argv or environment of any process.** The constraint is that secrets arrive through the environment and
    never reach a log; capturing the environment defeats that constraint in one line (ADR-0020, `.env.example`).
7. **A stack trace that contains a request URL.** `omega-web/src/coffee/log_error.coffee:6` appends `err.stack` to
    a key the About page then ships whole, and the frames of a failed fetch contain the URL that failed — so NEXORA
    logs the taxonomy code (§3) and, where a stack is genuinely needed, the stack with §5's final pass applied at
    the sink.

## 13. Forbidden

`packages/schema/observability.json#/forbidden` is ten rules. Each is a shape error rather than a style preference,
and each names the mechanism it prevents:

1. A log line that is not a record — a free-text line has no field names, so a name-keyed policy cannot see it,
   and `omega-target-chromium-extension/overlay/log.js:64-65` is the proof that this is how legacy leaked.
2. A sink that is not in §5 — masking-at-the-sink works only because the sinks are enumerable; a sixth sink is an
   unmasked one with a plausible name.
3. An unbounded buffer of any kind, in any plane.
4. Xray's access log enabled by omission (§9).
5. A per-connection log in either plane, at any level, **including `debug`** — debug is a verbosity setting, not a
   licence to record destinations. A user who turns it on to diagnose a transport must not be creating a browsing
   history file.
6. Anything uploaded, staged for upload, or sent to a third-party collector. There is no telemetry backend in this
   design, so "anonymous usage statistics" would be a second data path with no owner and no contract.
7. A diagnostics failure that breaks proxying. Diagnostics is a reader of state and never a dependency of a
   decision; a ring whose append throws must not reach the code that writes `chrome.proxy.settings`
   (`docs/architecture/22-component-contracts.md` §11's "mode of failure").
8. An event whose payload carries a state a read command also reports (§6).
9. A trace id built from anything the user typed or connected to (§7).
10. A record written before the effect it describes is committed (§3), which is the rule that keeps
    `docs/architecture/35-schema-versioning-migration.md` §8's recovery procedure able to trust what it reads.

## 14. Who owns each check

| Claim | Check | Layer | Verified |
|---|---|---|---|
| Fourteen events, no more, no fewer | *"the observability catalogue is exactly the registered event set"*, *"every catalogue entry is a valid instance of the catalog shape the event schema declares"* | Schema | **YES** (green) |
| Every payload is a closed object and every recorded frame matches it | *"every event payload is a closed object, so no event can carry a field nobody declared"*, *"recorded event frame matches its declared payload"* | Schema, replaying fixtures | **YES** |
| No event answers a question a command answers | *"no event payload carries a state, a destination or a rendered message that a read command also reports"*, *"a push-then-pull event names its subject and points at a command that actually holds the state"* | Schema | **YES** |
| The two unrecorded events are unrecordable here, not untested by accident | *"the only events with no recorded frame are the two that cannot have one on this pipe"* | Schema | **YES** |
| Ten sections, defaulted as the shipped settings say | *"the diagnostics catalogue is exactly the sections the command can produce"*, *"the sections the catalogue marks default-included are exactly the sections the shipped settings ask for"* | Schema | **YES** |
| Every section reads a declared store, in a declared plane, within the command's byte ceiling | *"every diagnostics section reads a store the ownership catalogue declares"*, *"no section can ask for more bytes than one call may return"* | Schema | **YES** |
| Five sinks, each on a declared store, each with a masking stage; the ring is one frame | *"the sink list is closed and every sink has one name"*, *"every log-backed store has a sink that masks it, and the only store-less sink is the export"*, *"a full Connector ring is exactly one frame, so the answer to a question needs no reassembly"* | Schema | **YES** |
| Levels, and the mapping into Core's vocabulary | *"the log level vocabulary is the settings vocabulary, not a second one"*, *"the one word NEXORA and Xray disagree on is mapped by the Compiler, never passed through"* | Schema | **YES** |
| The record is flat, declared and bounded; one free-text field; component names fit the wire | *"exactly one log field is free text, and it is capped in characters rather than in prose"*, *"a component name that fits the log always fits the frame it is reported on"* | Schema | **YES** |
| Retention window, export cap and block range all equal their sources | *"the retention window is the settings window, restated and equal"*, *"an export can never be larger than what one diagnostics call was allowed to produce"*, *"the in-force block port comes from a range disjoint from every inbound range"* | Schema | **YES** |
| Access log off, and not a user setting | *"Xray access logging is stated off by the Compiler and is not a user setting"*, *"the illustrative compiled shapes do not leave the access log to an upstream default"* | Schema | **YES** — declarative; no config has been compiled yet |
| Doc and data name the same sinks, events and sections | *"the observability document names every sink, event and section the data declares"*, *"the component vocabulary the log copies is a readable index and every name fits the cap"* | Schema | **YES** — this file is read by the harness |
| Every prohibition has evidence behind it | *"every prohibition and every never-logged class cites the code, the contract or the finding behind it"* | Schema | **YES** |
| A record is actually written, masked and rotated at runtime; `seq` gaps are real; the ring evicts | a Go test over a fake sink set, plus a grep of a real exported bundle | Connector + Extension | **NO — Phase 2**, `LIVE-10`, `LIVE-11` |
| The value-keyed pass destroys a live credential in all five sinks | a run against a REALITY server and a tokenised subscription, then a grep of the log file, the ring, the Core tail and the export | Connector | **NO — Phase 2**, `LIVE-10`; the algorithm is doc 32 §8's, `Verified: NO` there too |
| `inForcePort` is the port `chrome.proxy.settings` holds, and a refusal is reported as a refusal | `Get-NetTCPConnection` / `ss -ltnp` against the block port after a Core kill, plus a listener planted on 20900 | Extension + Connector | **NO — Phase 2**, `LIVE-11` |
| A trace id survives the boundary unchanged and a migration keeps the history | a two-plane fixture assertion, then the migration runner's before/after store diff | Extension | **NO — Phase 2**, `LIVE-23` |
| Xray's `log` section as emitted really suppresses the access log | one started Core and a directory listing of its state dir | Connector | **NO — Phase 2**; the config half is green, the runtime half needs the binary (ADR-0019) |
| The export never uploads anything | no network call in the export path, plus a review of the options page bundle | Extension | **NO — Phase 2**; `docs/architecture/31-security-model.md` owns the network surface |
| The negotiated versions reach the connector log and the answer to `diagnostics.get` | nothing in this document's own vocabulary can say it: the pipe floor has no field, only the protocol number has a name | Connector + Schema | **NO — Phase 2**, deferred to §16's sixth open item |

## 15. Threat, control, check

| Threat | Position | Control | Check |
|---|---|---|---|
| A secret reaches a log, then a public bug report | the export path, twice in legacy | value-keyed masking at the sink, a final pass over the artefact, no free text but a capped one | *"every sink says at which stage the mask runs"*, `LIVE-10`, §12 |
| A per-connection destination history accumulates on the user's disk | Core's own default, or a debug level taken seriously | `access` stated `none`, rank-and-rule routing decisions, §13 rule 5 | *"Xray access logging is stated off by the Compiler and is not a user setting"*, §9 |
| Diagnostics becomes a disk problem | an unbounded buffer | three caps per sink, rotation that reports what it removed | *"the retention window is the settings window, restated and equal"*, §10 |
| A ring that evicted is mistaken for a ring that lost records | MV3 teardown, crash loops | `seq` monotonic per sink per plane, gaps stated not silenced | §3's rule on `seq`, `LIVE-23` for the restart half |
| A stale cached state arrives as an event and is shown as current | a subscriber that missed the read | push-then-pull: a hint plus `detailCommand`, never a value | *"no event payload carries a state, a destination or a rendered message that a read command also reports"* |
| The support bundle cannot be trusted to be complete | a truncated or partial export | `truncated`, the masked counters, fingerprints, per-section state | *"an export can never be larger than what one diagnostics call was allowed to produce"*, §11 |
| A block is reported as a fault because the port moved | reconnect reconciliation | lowest closed port, chosen once, never re-rolled | *"the in-force block port comes from a range disjoint from every inbound range"*, `LIVE-11` |
| A trace id smuggles a hostname | a developer filling a free-text field | the pattern in `packages/schema/events.schema.json#/$defs/traceId`, no derivation from user data | §7, `docs/architecture/25-native-messaging-protocol.md` §12 |
| A forgotten call site | new logging code written later | records are the only call-site API, and sinks are enumerable | §5, §13 rule 1, doc 32 §8 |
| A log write breaks proxying | a corrupt buffer | diagnostics is a reader, never a dependency | §13 rule 7, `docs/architecture/22-component-contracts.md` §11 |
| An upstream default changes under a moved pin | `packages/schema/examples/pins.xray.json` letting Core move | every privacy-relevant field stated explicitly in the emitted config | §9's `verification` note, `LIVE-02` |

## 16. Invariants, amendments, open items

**Invariants.**

1. A NEXORA log line is a record built from the declared eleven fields and nothing else, and there are exactly
   five sinks.
2. Masking runs at the sink, keyed on live keystore values, and the export gets a final pass over the whole
   artefact.
3. No event carries a state, a destination or a rendered message that a read command also reports.
4. No sink is unbounded, in either plane, and no buffer's bound is a single dimension.
5. Nothing in this document is uploaded anywhere; the export exists only because a user pressed something.
6. A destination — host, port, URL, SNI — appears in no record, at no level, in no plane, ever.
7. Every privacy-relevant field of the Core's `log` section is written by the Compiler, never inherited.
8. A trace id is minted, not derived, and is never re-minted at the plane boundary.

**Amendments made by this document** (all green in `npm --prefix packages/schema run verify`, 973 checks):

1. `packages/schema/observability.json` is added, and the verifier gained 62 checks over it and its cross-reads
   (§14's labels).
2. `packages/schema/protocol/envelope.schema.json` was tightened while §6 was written. Its single rule for `event`
   and `progress` frames forbade only `method`, so an event frame could legally carry `ok` or `error`, and eleven
   recorded event frames had no `result` at all — which meant an event's payload had no home and the envelope's
   own `result` description contradicted the rules below it. There are now two rules: `progress` is required on
   both `event` and `progress` frames with `method`, `ok` and `error` forbidden, and `result` is additionally
   required on an `event` frame. The eleven recorded frames were back-filled with the payloads their matching
   responses carry, and three frames added (`connector.connected`, `server.health.changed`, `routing.decision`) so
   that the hint and sampled classes are exercised too.
3. `docs/architecture/23-unified-server-model.md` §9's illustrative compiled shape now states `access` and `dnsLog`
   explicitly; found while writing §9, because the shape as it stood left Xray's own access-log default in force.
   The recorded `config.snapshot` frame's `maskedConfig.log` was corrected the same way.
4. `packages/schema/protocol/data-ownership.json#/stores` gained the `connector-log-file` and `core-log-file` rows
   (§5), and the `extension-log` row's `bound` was corrected to say what is true today rather than what will be
   true in Phase 2.
5. `docs/architecture/32-secrets-keystore-masking.md` §8's sink-list self-reference now points at
   `packages/schema/observability.json#/sinks` instead of a section that never enumerated it, and
   `docs/architecture/22-component-contracts.md` §11 invariant 2's reference to doc 32 was corrected to §8.

**Open items.**

1. Whether the browser ring is included in a profile export. It is not part of the envelope and not sync-eligible,
   so including it is a deliberate size-and-privacy decision rather than an oversight —
   `docs/architecture/34-data-split.md` §12. The same row answers the census question in
   `packages/schema/migrations.json#/openDecisions/2`: the secret census counts belong to diagnostics, not to the
   UI, until `docs/architecture/37-ui-architecture.md` §13 says otherwise with a reason.
2. Whether `maskAddress` should be set once a live capture shows the error log naming peers —
   `docs/verification/live-matrix.md`.
3. Whether `routing.decision` sampling should be user-configurable or fixed at the subscribe default of 10 per
   second. A user-tunable rate is a user-tunable amount of their own traffic history in a buffer, which is a
   different conversation from a verbosity slider, and is ADR territory rather than a settings row.
4. Whether the export's fingerprints should be keyed per artefact. They are keyed on the live keystore today, so
   two exports from one machine at different times can disagree — safe, but worth naming before a support workflow
   depends on comparing them.
5. The runtime half of every row in §14 marked **NO — Phase 2**, which is most of them: there is no Connector to
   write a record yet. `Verified: NO — deferred to Phase 2`.
6. Where the pipe half of the negotiation is written down. `docs/architecture/21-system-context.md` §6 gives its reason
   for promising that the negotiation result reaches the connector log and `diagnostics.get` in the pipe, not the
   browser: a shim from NEXORA 3.7 talking to a connector from 3.6. Since 2026-11-06 the framing contract says there
   are two floors — `packages/schema/protocol/framing.json#/versionFloors` declares four numbers over `protocol` and
   `pipe`, and `packages/schema/protocol/error-taxonomy.json#/codes` refuses both with one `VersionMismatch` while its
   own `meaning` states that "today the window that is negotiated is the protocol one". Every artefact this document
   owns names that one number: the `versions` entry of `packages/schema/observability.json#/diagnosticsSections`, the
   `protocolVersion` of `packages/schema/observability.json#/export/footer/fields`, and the closed payload of
   `connector.connected` in `packages/schema/observability.json#/events`. Which of them carries the pipe number is for
   `docs/architecture/40-open-decisions.md` (`ODEC-41`) to say, and not a sentence this document may settle.

---

Trace: ADR-0006, ADR-0008, ADR-0009, ADR-0010, ADR-0011, ADR-0013, ADR-0014, ADR-0019, ADR-0020 · Contract:
`packages/schema/observability.json`, `packages/schema/events.schema.json`,
`packages/schema/protocol/envelope.schema.json`, `packages/schema/protocol/commands/diagnostics.get.json`,
`packages/schema/protocol/commands/connector.events.subscribe.json`,
`packages/schema/connector-settings.schema.json`, `packages/schema/protocol/data-ownership.json`,
`packages/schema/protocol/framing.json`, `packages/schema/protocol/error-taxonomy.json`,
`packages/schema/protocol/command-allowlist.json`, `packages/schema/examples/settings.default.json`,
`packages/schema/examples/failure-policy.default.json`, `packages/schema/examples/events.catalog.json`,
`packages/schema/examples/pins.xray.json`, `packages/schema/migrations.json`,
`packages/schema/fixtures/protocol/failure-block.jsonl`,
`packages/schema/fixtures/protocol/subscriptions.jsonl`, `packages/schema/fixtures/protocol/handshake.jsonl`,
`packages/schema/fixtures/protocol/probes-and-ops.jsonl` · See also:
`docs/audit/11-security-findings.md`, `docs/audit/07-routing-engine.md`,
`docs/architecture/21-system-context.md`, `docs/architecture/22-component-contracts.md`,
`docs/architecture/23-unified-server-model.md`, `docs/architecture/25-native-messaging-protocol.md`,
`docs/architecture/26-command-surface.md`, `docs/architecture/27-config-compilation-pipeline.md`,
`docs/architecture/28-routing-model.md`, `docs/architecture/29-subscriptions-and-servers.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/31-security-model.md`,
`docs/architecture/32-secrets-keystore-masking.md`, `docs/architecture/33-binary-integrity.md`,
`docs/architecture/34-data-split.md`, `docs/architecture/35-schema-versioning-migration.md`,
`docs/verification/live-matrix.md`.
