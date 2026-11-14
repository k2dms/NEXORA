# ADR-0020: Testing is schema validity plus golden compiled configs plus an env-gated live matrix, and no test may claim success without proving egress

Status: Accepted
Date: 2026-11-04

## Context

The inherited test base is small and partly red, measured rather than estimated: seven `.coffee` test files
exist, `omega-pac` runs 142 passing / 1 failing and `omega-target` 8 passing / 5 failing, and `omega-web`
and `omega-target-chromium-extension` have **no `test/` directory at all** -- `grunt test` there succeeds
because the mocha glob matches nothing. The single failing `omega-pac` case is the SOCKS5 PAC result at
`omega-pac/test/profiles.coffee:49-52`, and it is not trivia: `ConnectorProfile.compile` depends on that code
path, so it is a Phase-2 blocking requirement, not a defect to hide. One legacy test is also
timezone-fragile by construction: `omega-pac/test/conditions.coffee:468` builds a local-time `Date` string
for the time/weekday conditions, and the honest fix changes test semantics, so it is recorded, not patched.

What that base cannot test is everything NEXORA adds: a Go Connector, a foreign Core binary, a Native
Messaging protocol, a compiler from intent to config, a migration over user data, and a proxy whose failure
mode is *silently not proxying*. For that failure mode, an ordinary unit test is worse than none: a
"connection succeeded" assertion built on a TCP connect returns green while the user's traffic leaks. The
brief's constraint 8 states the layers separately -- DNS, TCP, Core-outbound, proxied HTTP(S) -- and the
contract already models them (`packages/schema/protocol/commands/server.test.json:52`).

The green contracts give the starting point: `npm --prefix packages/schema run verify`
(`packages/schema/package.json:8`) runs the whole contract layer and ends with the summary line printed by the
report block at the end of `packages/schema/scripts/verify.mjs` — the line the CI step asserts on. The check count
it prints is an output, not a contract: it grows every time a contract gains an assertion, and nothing may be
pinned to it.

## Decision

1. Three tiers, each with a distinct question, all mandatory:
   - **Schema validity** -- `npm --prefix packages/schema run verify` is a blocking CI step. It compiles
     every schema, validates every example, replays every recorded frame, and rejects each case in
     `examples/invalid/` *for the keyword recorded against it*, so a fixture cannot pass by tripping an
     unrelated rule.
   - **Golden compiled configs** -- the envelope-to-Xray compiler is tested by byte-stable snapshots: the
     canonical server shapes, profile forms and routing policies under `packages/schema/examples/` each get
     a committed expected config, and the same input twice MUST produce the same bytes (sorted keys, no
     timestamps, no map order). A diff in a golden file is a contract change requiring review, never a
     test update.
   - **Live matrix `LIVE-01` … `LIVE-24`** -- end-to-end against the real test VPS, gated entirely by
     environment variables. A missing variable MUST yield `SKIPPED`, never `PASSED`, and secrets MUST come
     from the environment only, never committed and never echoed into logs.
2. **Hard rule (egress proof).** No test, assertion, fixture or UI state may report "connection succeeded"
   unless it has proven that egress left through the Core. Operationally: a pass requires `egressProbe`
   requested (`packages/schema/protocol/commands/server.test.json:25`), the `egress-ip` and `proxied-http`
   layers present in `layers`, and `matchesExpectedEgress` true
   (`packages/schema/protocol/commands/server.test.json:66`). A TCP-only success MUST be reported `partial`,
   and an answer whose egress IP is not the expected one MUST be `not-proxied`
   (`packages/schema/protocol/commands/server.test.json:40`) -- those are different user-visible facts and a
   test MUST NOT collapse them.
3. Probe results MUST NOT be served from an idempotency cache: `server.test` is
   `replayable: false` (`packages/schema/protocol/commands/server.test.json:83`), and the replay fixtures in
   `packages/schema/fixtures/protocol/probes-and-ops.jsonl` assert that a probe has exactly one answer. A
   cached latency number in an assertion is a lie.
4. Fail-closed tests MUST assert the absence of traffic, not the absence of an error: the expected outcome
   of a dead Core is a blocked request through the fail-closed mechanism of ADR-0009, and a test that
   asserts "no exception thrown" is rejected as a non-test.
5. Migration tests MUST include a restore assertion (ADR-0014): snapshot, migrate, restore, compare hashes.
   A migration test that never restores has not shown anything about recoverability.
6. The legacy floor stays pinned: CI parses the mocha summaries and fails below 142/1 (`omega-pac`) and
   8/5 (`omega-target`), and MUST print `NOTE: <project> has 0 test files` where that is true. Existing
   legacy tests MUST NOT be weakened or deleted to reach green.
7. Protocol-level behaviour is tested by replaying recorded frames before any live host exists:
   109 frames across six files under `packages/schema/fixtures/protocol/`, covering handshake, Core
   lifecycle, config apply, chunked import, failure-block and probes.
8. Cadence rule: tiers 1 and 2 run on every push; tier 3 runs only where the environment provides the
   server, and its absence MUST be visible in the report rather than presented as success. In this iteration
   tier 3 is specification only -- no browser and no live run exist on this host (the Go toolchain arrived
   later the same day, `docs/PROJECT-STATE.md` §4.6), and the CI matrix parity for the legacy chain follows the Node version pinned in
   `.github/workflows/release.yml:21`.

## Consequences

- The definition of "tested" for NEXORA is "the contract is valid, the compiler is deterministic, and the
  egress was proven" -- not "coverage is above a number".
- Golden files become reviewable artifacts; every transport combination worth supporting needs an example,
  which is why the five canonical server shapes live in `packages/schema/examples/`.
- A live run is now a documented, reproducible procedure with named environment variables, which is what
  makes `LIVE-01…24` executable by someone other than the person who wrote it.
- Two known legacy reds stay red and are stated in the report instead of being silenced, so a future
  "green" is credible rather than manufactured.
- `SKIPPED` must be a first-class CI outcome, which needs the workflow to distinguish it -- more YAML than a
  plain pass/fail, and the price of honesty about environments without a VPS.
- The verification matrix (`docs/architecture/39-verification-matrix.md`, not yet written) MUST carry a row
  per claim in this ADR, including the ones that are currently `Verified: NO`.

## Alternatives considered

- **Coverage percentage as the gate.** Rejected: a number over 29 Jade templates and a CoffeeScript engine
  measures line visits, not proxy correctness, and would be gamed by exactly the assertions rule 2 forbids.
- **Live-server tests as the primary tier.** Rejected as primary: nondeterministic, secret-dependent and
  impossible on most CI runners, so a green build would silently mean "the live tier did not run". Kept as
  tier 3 with `SKIPPED` semantics.
- **Mock Xray for everything.** Rejected for transport claims and accepted for everything else: a mock
  proves the compiler and the framing, and can prove nothing about REALITY, Vision or egress. This is why
  tiers 2 and 3 are not interchangeable.
- **UI snapshot/e2e testing first.** Rejected as the primary investment: the highest-consequence defects are
  in the protocol, the compiler and the migration, not in rendering; UI assertions still ride on rule 2 for
  anything that displays "connected".
- **Fix the six legacy failures first, then test.** Rejected for this iteration: changing `pacResult`'s
  SOCKS5 output or the timezone parsing changes product/test semantics without a contract, so they are
  pinned as a floor with the SOCKS5 case promoted to a Phase-2 blocking requirement.

## Evidence

- VERIFIED -- the report block at the end of `packages/schema/scripts/verify.mjs`: the summary line the CI step
  asserts on, and the green run it printed for this iteration (900 checks on 2026-11-04, cited as a reading of the
  harness rather than as a number anything depends on).
- VERIFIED -- `packages/schema/protocol/commands/server.test.json:25` (`egressProbe`), `:40` (the
  `pass`/`partial`/`fail`/`not-proxied` enum), `:52` (the five layers), `:66`
  (`matchesExpectedEgress`), `:83` (results never replayed from cache).
- VERIFIED -- 109 protocol frames across the six `packages/schema/fixtures/protocol/*.jsonl` files, counted
  on disk; `packages/schema/fixtures/protocol/probes-and-ops.jsonl` is the probe single-answer fixture set.
- VERIFIED -- `omega-pac/test/profiles.coffee:49-52`: the failing SOCKS5 `pacResult` expectation;
  `omega-pac/test/conditions.coffee:468`: the local-time `Date` construction behind the TZ fragility, with
  its own explanatory comment at `omega-pac/test/conditions.coffee:467`.
- VERIFIED -- `.github/workflows/release.yml:21` pins `node-version: '22'`, the parity anchor for the new CI
  matrix; the measured floor (142/1, 8/5, two projects with zero test files) is recorded in
  `docs/audit/99-baseline-report.md`.
- INFERRED -- that golden-file byte stability holds across machines and Go map iterations: it is a property
  of the compiler design (`packages/schema/protocol/commands/config.apply.json:120`), and there is no
  compiler yet to test.
- UNVERIFIED -- every `LIVE` row, and the two "0 test files" projects' behaviour under the new workflow,
  which is not written in this iteration.

## Verification

- Existing check: `npm --prefix packages/schema run verify` -- green, and it is the executable half of
  tier 1 plus the frame replay of rule 7.
- Existing check: `node tools/check-no-secrets.mjs --selftest` (13/13) guards the "no secrets in tests or
  logs" half of rule 1; `node tools/check-doc-evidence.mjs` resolves the citations above.
- Verified 2026-11-14 for the golden-config half, superseding the deferred verdict this item carried: the
  snapshots exist under `connector/testdata/config/` — five of doc 27 §3's six fixtures, byte-compared and
  Xray-driven by `connector/internal/apply/drive_test.go`; the sixth (the fail-closed variant) has no distinct
  Xray bytes by doc 30 §7's own decision, a stated exclusion, not a missing golden (`VER-28` retired).
  The rest -- the live matrix `LIVE-01…24`, the egress-proof rule on every "connected" display, the
  fail-closed traffic assertion, the migration test, and `.github/workflows/verify.yml`'s pinned legacy floor --
  stays deferred to Phase 2. History in git.
