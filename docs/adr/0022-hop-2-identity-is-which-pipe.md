# ADR-0022: Hop 2's sender identity is which pipe the host opened, and the local pipe carries the extension envelope

Status: Accepted
Date: 2026-11-07

## Context

The two processes of ADR-0005 communicate over a local pipe whose message schema is versioned separately from the
extension-facing protocol — but no document said what that schema *is* or how the sender identity the host resolved
on hop 1 reaches the Connector on hop 2. The corpus states the requirement and no field to read an answer out of:

- `docs/adr/0005-two-process-connector.md` rule 2 makes the pipe's schema a contract "versioned separately" that
  "MUST carry its own version field" — and leaves the schema itself undefined.
- `packages/schema/protocol/framing.json` describes itself as the contract for "the extension-to-host pipe". The
  half of its `senderPolicy` that reaches the Connector is written from the other side: identity facts resolve
  from an OS event on hop 1, while budgets are counted in the Connector (`docs/architecture/31-security-model.md`
  §8) — the process that spends a sender's budget cannot see which sender it is.
- `docs/architecture/25-native-messaging-protocol.md` §2 closes the last escape: the same envelope crosses both
  hops untranslated, `packages/schema/protocol/envelope.schema.json` is closed at fourteen properties none of
  which is a sender, and ADR-0004 rule 3 forbids a payload field from becoming identity.
- Until this is chosen, `connector/internal/dispatch`'s `Identity` has no producer on hop 2, and no `cmd/` process
  can be written against a transport the contracts do not describe. `ODEC-35` carried the question and `VER-77`
  waited on it.

The `decidedBy` of `ODEC-35` is `adr` because choosing between the surviving options moves ADR-0005's boundary and
the reasoning ADR-0004 rests on: a field edit would hide it.

## Decision

**One pipe instance per sender, and the local pipe carries the same envelope the extension-facing protocol
already defines — with the pipe's own `pipeVersion` floor guarding it.**

1. **The host opens one pipe per extension port, and the sender is which pipe it opened.** The host already
   resolved the sender's platform identity on hop 1 (ADR-0004 rule 3); on hop 2 it spawns or names the Connector's
   pipe for that sender, so the Connector learns the sender from *which pipe the frame arrived on* — never from
   anything inside the frame. The host passes the resolved identity once at process startup as a spawn-time
   injection (`--sender-id`), because the pipe the host opened *for that sender* is the same fact by construction:
   one sender, one pipe. `senderPolicy`'s budgets stay per sender id, per Connector process; `portsPerSender`'
   "close the oldest port" is the host refusing a second pipe for one sender, which is the same rule read from the
   other side.
2. **The local pipe carries the extension envelope, guarded by `pipeVersion`.** There is no second message schema
   and no out-of-band handshake: `packages/schema/protocol/framing.json`'s `pipe` floor (`minPipeVersion`,
   `frameFormatVersion`) is the version field ADR-0005 rule 2 demands, and it is carried in the envelope's own `v`
   field, which every frame already bears. A frame whose `v` is below the Connector's `minPipeVersion` is refused
   at the framing layer before dispatch, the same refusal `VER-18`'s tombstone describes — declared, not compared
   yet.
3. **OS peer credentials are defence in depth, not identity.** The named-pipe client pid (Windows) or
   `SO_PEERCRED` (POSIX) authenticates the *host* process the Connector was started by — a useful containment
   check, but not the sender's identity, because it says nothing about which extension port the host was serving.
   It MAY be added later as a hardening layer without touching this decision.
4. **A sender field in the envelope is rejected.** ADR-0004 rule 3 forbids a payload field from becoming identity,
   and the envelope's fourteen-property closed object has no sender among them. Opening the object to add one
   would let every extension-frame consumer see a sender field that means something different on hop 1 (where it
   is forbidden) and hop 2 (where it would be injected) — the misread the closed object exists to prevent.

## Consequences

- `nexora-host`'s job is now stated rather than implied: frame hop 1, resolve the sender, spawn or name the
  Connector's pipe for that sender, and inject nothing into the envelope. The "versioned separately" of ADR-0005
  rule 2 is answered by "the same envelope, versioned by `pipeVersion`" — one message format across both hops,
  two version floors.
- The Connector's `Identity` gains its producer: the spawn-time injection the host performs, which is the same
  fact as "which pipe" because the host opens one pipe per sender. `cmd/nexora-connector`'s `--sender-id` flag is
  no longer a demo affordance; it is the decision's own mechanism, and its comment stops calling it one.
- A sender holding two browser ports concurrently is refused by `portsPerSender` exactly as before — the count is
  per sender id, and the host is the enforcer.
- `VER-77`'s pipe-schema claim is answered by the same envelope the extension protocol already ships, and the
  framing layer's `minPipeVersion` comparison is the one runtime check still owed (`VER-18`'s tombstone states
  that; this ADR does not change it).

## Alternatives considered

- **An out-of-band startup handshake** carrying `{senderId, allowlisted, pipeVersion}` before the first envelope.
  Rejected for the reason doc 25 §5 raises against the temp-file handoff: it gives the pipe two protocols instead
  of one, and the handshake message is exactly the kind of second schema the closed envelope exists to prevent.
- **OS peer credentials as identity.** Authenticates the host, not the extension; still needs option 1 or option
  2 for the sender id. Kept as a future hardening layer, not an identity source.
- **A sender field in the envelope.** Excluded by ADR-0004 rule 3 and the closed object; recorded here so the
  exclusion is not re-litigated, the way `packages/schema/protocol/framing.json#/oversizedPayloads/alternativesRejected` records the
  handoff it also refuses.

## Evidence

- `docs/adr/0005-two-process-connector.md` rules 2–4: the pipe's schema is a separate contract; the host owns no
  state; the Connector outlives the host.
- `docs/adr/0004-native-messaging-only-no-loopback-http-control.md` rule 3 and the self-framed-pipe refusal.
- `packages/schema/protocol/framing.json#/senderPolicy` — budgets per sender id per Connector process;
  `portsPerSender` — the host's own refusal of a second pipe.
- `docs/architecture/25-native-messaging-protocol.md` §2 — the same envelope crosses both hops untranslated.
- `docs/architecture/31-security-model.md` §8 — the process that counts a budget is not the process that resolved
  the identity.
- `connector/internal/dispatch/dispatch.go` — `Identity` carries `SenderID` and `Allowlisted`, resolved from the
  spawn-time injection; its comment states that none of it comes from the payload.

## Verification

The wiring this decision describes is measured: `cmd/nexora-connector`'s tests run the framed and jsonl transports
through the same dispatcher with the injected identity, and the registration of `config.apply` in the dispatcher is
the proof that the process answers a real method through the same `Identity` the host's injection feeds. The
`minPipeVersion` comparison at the framing layer is `VER-18`'s tombstone's declared residual, not a new row.

`Verified` — green at the time of writing on the 73-row queue; the live rows that measure a real host on a real
pipe are `SKIPPED` and stay that way.
