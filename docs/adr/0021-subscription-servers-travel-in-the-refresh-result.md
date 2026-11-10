# ADR-0021: A subscription's Servers travel in the refresh result, and an event carries counts rather than names

Status: Accepted
Date: 2026-11-07

## Context

`docs/architecture/29-subscriptions-and-servers.md` §2 ends with a frame contract: the feed body never crosses
the pipe toward the extension, chunking therefore serves `profile.import` alone, and the reverse direction is
capped at 1 GiB, so a `subscription.refresh` result is single-frame. That sentence has been load-bearing for
three other documents since it was written — and it rested on a premise nothing in the corpus had decided:
that the Connector keeps subscriptions whole. `docs/architecture/40-open-decisions.md` recorded the premise as
`ODEC-33`, `docs/PROJECT-STATE.md` §9's fourth question put it to a person, and `docs/architecture/39-verification-matrix.md`
carried `VER-61` as the queue's `needs-human` row: a boundary that cannot be asserted against an undecided
answer.

**Corrected 2026-11-10, and the correction is arithmetic, not taste.** The 1 GiB figure above is what this record
believed, and the platforms say otherwise: Chrome, Firefox and Edge each cap what a native host writes back at
1 MB, and the 64 MiB belongs to the other direction (`docs/architecture/25-native-messaging-protocol.md` §2.1,
measured in `docs/PROJECT-STATE.md` §4.54). Clause 3's single-frame sentence therefore holds only up to what 1 MiB
carries — the corpus's own Server documents compact at 448–664 B, so a refresh near `maxServers` is about 2 MiB and
does not fit. What this record decides is unchanged: the Servers travel in the result, and never in an event. What
it assumed — that the result always fits one frame — is the half of `ODEC-06` the ceiling question now carries.

What the sitting that tried to answer it found is that the two alternatives were never open. They are excluded
by data this repository already carries and already checks, and the exclusion is not a matter of taste:

- An event cannot carry a Server. `packages/schema/observability.json#/events/7` is `subscription.updated`, and
  its payload is `additionalProperties: false` over `sourceId`, `added`, `removed` and `serverIdsReused`. Its
  own `why` states the reason: feed server names are the provider's marketing strings and often carry the
  user's account identity, which is why ids are reused across refreshes and counts are safe to log.
- A chunked subscription has no sender. `packages/schema/protocol/envelope.schema.json#/properties/chunk`
  names `profile.import` as the only sender today and says so in its own description, because a subscription
  body is fetched inside the Connector and never crosses that frame boundary at all.
- The raw-body reading contradicts an invariant, not a preference. `docs/architecture/29-subscriptions-and-servers.md`
  §11 states that no feed body enters the browser process, and `packages/schema/protocol/data-ownership.json`
  gives the browser plane only the `SubscriptionSource` — fetch policy, display URL, resolved format, two
  keystore references — never a body and never a parsed server list.
- What is left is not merely permitted, it is required. `servers` is in
  `packages/schema/protocol/commands/subscription.refresh.json#/result/properties/refreshed/items/required`,
  with `maxItems` 4096 and every item a `server.schema.json` document. The parsed Servers already have a named
  place on the pipe, and the extension is the only component that can write the intent documents that refer
  to them (ADR-0010).

So the decision was already the shape of the data. Writing it down is the cheapest way to stop three documents
resting on a premise, and to take `VER-61` out of a queue that cannot close a `needs-human` row by itself.

## Decision

1. The Connector owns the whole subscription path that touches a feed: the fetch, every guard in
   `docs/architecture/29-subscriptions-and-servers.md` §3, the parse, the credential extraction into the
   keystore, and the hash. The feed body MUST NOT cross the pipe in either direction, in either frame type, for
   any reason.
2. Parsed `Server` documents reach the extension in the result of the command the extension itself calls:
   `subscription.refresh`'s `refreshed[].servers`. A subscription event MUST NOT carry a Server, a Server name,
   an address or any other field a payload of that shape would need; `subscription.updated` stays a counts-only
   notification, and the pull that follows it is the command in clause 2.
3. `subscription.refresh` results stay single-frame within one frame's worth of Servers. The ceiling is
   `packages/schema/protocol/framing.json#/limits/hostToExtensionBytes`, now 1 048 576 B, and the same ceiling is
   what `packages/schema/subscription-source.schema.json#/properties/limits/properties/maxServers` refuses above
   — an arithmetic this record got wrong when it was written, and the question it leaves open is
   `docs/architecture/40-open-decisions.md` `ODEC-06`: lower the bound, or let a large result cross in pieces.
   A second sender for `chunk` MUST NOT be introduced to serve subscriptions while that question stands; whichever
   way it is answered supersedes this clause rather than amending it.
4. The extension persists what the result returns into the source documents it owns, and the one field the
   Connector may correct in them is the resolved `refreshed[].format` (ADR-0010's split, with the exception
   `docs/architecture/34-data-split.md` §6 states and `packages/schema/protocol/data-ownership.json`'s
   `nexora-subscriptions` writer clause repeats).

## Consequences

Cheaper: the delivery boundary stops being a question, so `docs/architecture/29` §2's frame contract now rests
on a decided clause instead of a premise, `VER-61` leaves the queue, and the four documents that deferred to
`ODEC-33` have an answer to cite. It also takes subscriptions off the critical path of `connector/internal/chunk`:
the reassembly that `docs/architecture/40-open-decisions.md` `ODEC-34` still holds is a `profile.import` problem
and nothing in this decision waits on it.

Harder: a refresh is one frame containing every parsed server, so the whole list is paid for at once — no
streaming, no partial page, and a 4 096-item ceiling that is now a delivery constraint as well as a data one.
The `servers` array is the payload, which means the result size grows with the feed while the frame shape
stays fixed. And clause 1's prohibition now carries weight: `docs/architecture/39-verification-matrix.md`
`VER-64` was already the row that nothing prevents the engine from fetching, but until this decision it was a
tidy rule; now it is the guard a security property depends on, and it stays open.

Forbidden: adding a Server-shaped field to any subscription event; widening `chunk` to a second sender without
superseding this record; letting the Connector store or create a `SubscriptionSource` (ADR-0010, and
`packages/schema/protocol/data-ownership.json`'s writer clause); and reading a feed body into the browser
process through any bounded command.

## Alternatives considered

- **Chunked hand-off of parsed Servers through `subscription.updated`**, the second option `ODEC-33` listed.
  Rejected on data, not on cost: the payload is closed at four fields and would need a fifth of unknown shape,
  `chunk` names one sender and says why the second does not exist, and the catalogue's own `why` argues the
  names should not be in the event at all because they carry the account identity. It also leaves the extension
  holding a partial subscription between chunks, which is a state no document describes how to display.
- **A bounded raw-body read**, the third option. Rejected: it is the one alternative whose price is an invariant,
  `docs/architecture/29-subscriptions-and-servers.md` §11's "no feed body enters the browser process" — and it
  would buy, with that, a capability the extension has no use for: it does not parse feeds.
- **The Connector owns subscriptions end to end and the extension never sees Servers.** This is the reading
  "end to end" invites, and it is the one the data forbids hardest: `servers` is required in the result, the
  profiles that reference servers are intent documents the extension owns (ADR-0010), and a Server the browser
  cannot name is a Server no profile can select.

## Evidence

- `packages/schema/observability.json#/events/7/payload` — the closed, counts-only `subscription.updated`
  payload, and `#/events/7/why` for the identity reason behind it.
- `packages/schema/protocol/commands/subscription.refresh.json#/result/properties/refreshed/items/required` —
  `servers` is required; `#/result/properties/refreshed/items/properties/servers` — `maxItems` 4096, items
  `$ref` `server.schema.json`.
- `packages/schema/protocol/envelope.schema.json#/properties/chunk/description` — `profile.import` is the only
  sender, and a subscription body never crosses the extension-to-host boundary.
- `packages/schema/protocol/framing.json#/limits/hostToExtensionBytes/value` — 1 048 576, and
  `packages/schema/protocol/framing.json#/limits/extensionToHostBytes/value` — 67 108 864; and
  `packages/schema/subscription-source.schema.json#/properties/limits/properties/maxServers/maximum` — 4096,
  the arithmetic §2's single-frame sentence rests on.
- `packages/schema/protocol/data-ownership.json` — the `nexora-subscriptions` entry: plane `browser`, class
  `intent`, and a `writer` clause that says `subscription.refresh` NEVER writes the source key.
- `docs/architecture/29-subscriptions-and-servers.md` §2, §3 and §11 — the frame contract, the guards that run
  on the Connector's side of it, and the invariant this decision now depends on.
- `docs/architecture/34-data-split.md` §6 — the one field the refresh corrects in a source the extension owns.

## Verification

Today: `npm --prefix packages/schema run verify` compiles the schemas that carry every pointer above, replays
the protocol fixtures whose `subscription.refresh` result frame is validated against the `servers` clause this
record relies on, and asserts the register and the queue as data — including that a row which closes by a
decision is gated by an entry that exists. `node tools/check-doc-evidence.mjs` resolves each pointer and each
document name cited here. `docs/PROJECT-STATE.md` §9's fourth question, `ODEC-33` and `VER-61` are retired by
the same commit, and `docs/architecture/39-verification-matrix.md` §8's third step is four rows instead of five.

Verified: NO — deferred to Phase 2 for the guard this decision makes load-bearing: nothing in this repository
yet proves that the engine never fetches or that no feed body reaches the browser process, which is
`docs/architecture/39-verification-matrix.md` `VER-64` and its build-graph assertion over
`nexora/extension/src/subscriptions/**`.
