# ADR-0008: The request id is the idempotency key, replays converge, and probes are never replayed

Status: Accepted
Date: 2026-11-04

## Context

The transport of ADR-0004 and the process model of ADR-0005 guarantee duplicate delivery: the browser tears
the Native Messaging port down on service-worker eviction (`packages/schema/protocol/framing.json:62`), the
client reconnects and re-runs negotiation and reconciliation (`packages/schema/protocol/framing.json:68`), and
the MV3 cold-start precedent in this same repository shows a caller retrying the *same* logical call up to
fourteen times (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78`). Retried commands
in a proxy tool are how one user gesture becomes ten Core restarts.

The same mechanism cuts the other way for measurements. A latency number replayed from a cache is a lie: the
user reads "38 ms" for a server that stopped answering five minutes ago.

Both halves are already encoded as data rather than prose: `packages/schema/protocol/envelope.schema.json:26`
makes the request `id` do double duty as the idempotency key, and
`packages/schema/protocol/commands.schema.json:45` plus `:49` make idempotency and replayability two separate
declared properties of every command.

## Decision

1. The client-generated request `id` IS the idempotency key. The connector MUST treat a second request with
   a key it has already answered for an idempotent command as a replay: it MUST return the stored response,
   MUST NOT re-run the side effect, and MUST answer with `inReplyTo` set to that same id.
2. Keys and stored answers MUST be retained for a bounded window only, sized and aged in the connector's own
   configuration, and eviction MUST be observable. The window is deliberately bounded so a long-lived
   connector cannot accumulate answers forever and cannot make an old gesture look fresh. **This bound is not
   yet contract data**: no field in `packages/schema` states a window length or a key-count ceiling, so
   Phase 2 MUST add one (a `dedupeWindow` field on the framing document is the intended home) rather than
   hard-coding a constant in Go.
3. State-changing commands MUST converge to a desired state rather than perform an action blindly. The
   `class` enum makes this a checked property: `converge` means repeated calls are no-ops
   (`packages/schema/protocol/command-allowlist.json:18`), `replace` means declarative reconcile —
   `config.apply` restarts Core only when the compiled hash actually differs (`packages/schema/protocol/commands/config.apply.json:4`) — and a start on a Core already running
   with the same config MUST succeed with `alreadyRunning: true` and the same PID
   (`packages/schema/protocol/commands/core.start.json:30`).
4. A response whose result is inherently time-varying MUST declare `replayable: false` and MUST be answered
   exactly once per logical measurement. `server.test` is the probe that does this
   (`packages/schema/protocol/commands/server.test.json:7`), and the meta-schema enforces the implication:
   `class: probe` forces `replayable: false` (`packages/schema/protocol/commands.schema.json:130`).
5. A client that receives a replayed answer to a non-replayable command MUST present it as stale, never as a
   fresh measurement (`packages/schema/protocol/commands/server.test.json:83`).
6. A non-idempotent command MUST carry a `nonIdempotencyRationale`
   (`packages/schema/protocol/commands.schema.json:114`), checked by `packages/schema/scripts/verify.mjs` "non-idempotent command carries a rationale: ${doc.name}".
   All nineteen current commands are idempotent, so this rule is a guard on the future surface rather than a
   description of it.
7. Callers MUST supply a fresh key for a genuinely new intent: two deliberate `server.test` presses are two
   requests, not a replay of one.

## Consequences

Cheaper: a reconnect-driven retry becomes safe instead of destructive, which is what lets ADR-0005's
"reconnect and reconcile" rule be a loop rather than an alarm; the fixtures can prove convergence instead of
asserting it; and probe staleness is a per-command flag rather than a UI convention someone forgets.

Harder: the connector owns a response cache with real lifecycle rules (rule 2), including what happens when
an answer is larger than the cache can hold; `dryRun` calls must not poison a stored answer; and every new
command must decide and defend its `class`, `idempotent` and `replayable` triple, which the schema forces into
the open.

Forbidden for this iteration: adding a command whose `idempotent` and `class` disagree; caching probe results
behind a `replayable: true`; and "simplifying" by removing `inReplyTo` from a response, which is the field
that lets a late answer be discarded rather than applied.

## Alternatives considered

- **A separate `idempotencyKey` field alongside `id`.** Rejected: two identifiers for one concept invites a
  caller that sends the same `id` with different keys (or the reverse), and the connector then has to pick a
  winner. One key, supplied by the client, is the whole rule.
- **Server-assigned keys (the connector hands out a token per intent).** Rejected: it requires a round trip
  before the operation that needs deduplication, and it cannot deduplicate the case that matters — a request
  that timed out and was resent while the original was still executing.
- **Make everything non-idempotent and rely on the caller not retrying.** Rejected: it contradicts the
  measured retry ladder above and turns every port teardown into a Core-restart storm, which is the failure
  `docs/audit/12-what-must-not-change.md` §10 identifies as user-visible today.
- **Cache probe results briefly with a TTL.** Rejected on honesty grounds rather than cost: the value is
  presented as a measurement, and a measurement with an undisclosed age is worse than a stale-labelled one.
  A rate limit is the right tool for probe storms, and `server.test` already names it
  (`packages/schema/protocol/commands/server.test.json:84`).

## Evidence

- `packages/schema/protocol/envelope.schema.json:26` — VERIFIED — `id` "doubles as the idempotency key".
- `packages/schema/protocol/commands.schema.json:45` — VERIFIED — `idempotent`; `:49` — VERIFIED —
  `replayable` with `default: true` and the client-MUST-mark-stale clause at `:52`; `:130` — VERIFIED — the
  `allOf` that binds probe to non-replayable.
- `packages/schema/protocol/commands/server.test.json:6` and `:7` — VERIFIED — `idempotent: true` together
  with `replayable: false`, which is the whole distinction in two lines.
- `packages/schema/scripts/verify.mjs` "idempotent replay returns the same result: ${file}#${req.id}" — VERIFIED — replays must return byte-identical results after stripping
  fields ending `At`, `Ms`, `Seconds`, `seq`; `packages/schema/scripts/verify.mjs` "probe is answered exactly once: ${file}#${req.id}" — VERIFIED — a probe must be answered
  exactly once; `packages/schema/scripts/verify.mjs` "idempotent replay is exercised" — VERIFIED — at least four replay pairs must exist in the fixtures
  (the green run of this iteration reports five).
- `packages/schema/fixtures/protocol/core-lifecycle.jsonl:1` — VERIFIED — the `cl-req-0001` `core.start`
  request whose id is answered more than once, i.e. a replay recorded as data.
- Class distribution of the 19 methods (8 read, 3 converge, 2 replace, 5 mutate, 1 probe) — VERIFIED against
  `packages/schema/protocol/command-allowlist.json:13`.
- Rule 2's window bounds — UNVERIFIED: no contract field expresses them, stated in the decision rather than
  hidden in the consequences.

## Verification

Today: `npm --prefix packages/schema run verify` asserts the probe rule (`packages/schema/scripts/verify.mjs` "probe is non-replayable: ${doc.name}"),
the rationale rule, the replay-identity rule and the single-answer rule across
`packages/schema/fixtures/protocol/` — 109 frames, 5 idempotent replay pairs, all green.

Deferred to Phase 2: the connector's actual deduplication — a Go test that sends one request twice and asserts
one side effect plus two identical answers, a second `core.start` after a reconnect returning the same PID,
and the `dedupeWindow` contract field with its own invalid example. Specified in
`docs/architecture/26-command-surface.md` and `docs/architecture/29-subscriptions-and-servers.md`.

Verified: NO — deferred to Phase 2 (rule 2 has no check and no data yet).
