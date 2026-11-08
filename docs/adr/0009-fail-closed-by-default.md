# ADR-0009: Fail closed by default; fail-open is a confirmed per-profile exception that is always visible

Status: Accepted
Date: 2026-11-04

## Context

When Core dies, the browser is still configured to send everything to a port that no longer answers. What
happens next is the one behaviour in this product that cannot be abstracted away from the user: either traffic
stops, or it silently leaves the tunnel. A proxy tool that chooses the second option has not degraded — it
has lied.

Phase 0 measured that the primitives for the safe choice already exist and that the primitives for the unsafe
choice are the ones the legacy code reaches for. `chrome.proxy.settings` is the actuator
(`docs/audit/06-proxy-application.md` §11 lists it as the fail-closed actuator NEXORA inherits), and today's
two "hard" gestures in this fork both leak: a `DirectProfile` sets `mode: direct`
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:19`) and a `SystemProfile`
clears the setting back to the browser and the OS
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:14`). In PAC form, the built-in
`+direct` resolves to the empty-proxy result, which is the literal string `DIRECT`
(`omega-pac/src/profiles.coffee:122` falling through to `omega-pac/src/profiles.coffee:89`). There is no
concept of a blocked state anywhere in the inherited model.

## Decision

1. The global default MUST be fail-closed. It is expressed as an immutable constant, not a preference:
   `packages/schema/connector-settings.schema.json:156` makes `failClosedDefault` a required key
   (`:153`) whose value is `const: "block"` — a settings document cannot spell any other default, and the
   section's own description at `:170` states the default is not overridable there by design.
2. `failOpen` MUST exist only as a per-profile override (`packages/schema/failure-policy.schema.json:312`),
   MUST NOT be reachable from the global settings document, and MUST carry a `confirmToken` proving an
   explicit user action. That implication is written as `if/then` inside the schema
   (`packages/schema/failure-policy.schema.json:326` with the `then` at `:336`), so a validator in Go, in
   TypeScript or in a test rejects an unconfirmed fail-open identically. The global document can no longer
   express it at all: `packages/schema/failure-policy.schema.json#/properties/coreDeath/properties/action` is a
   single-value enum, which is the amendment `docs/architecture/30-failure-policy.md` §9 makes to close the gap
   this ADR used to carry in its Evidence section.
3. The confirmation MUST be re-asked, not remembered silently across an import or a sync restore, and the
   per-profile state MUST be surfaced in the UI wherever the profile is listed or edited. A hidden fail-open
   is a silent bypass with a checkbox behind it.
4. The blocking mechanism MUST be a FixedProfile aimed at a **closed** loopback port
   (`packages/schema/failure-policy.schema.json:275`): the browser is told to use a local endpoint and the
   connection is refused. `+direct` and `chrome.proxy.settings.clear()` MUST NOT be used as fail-closed
   mechanisms, because both *route* traffic rather than stop it — that is not a degradation, it is a leak, and
   the audit's rejection of both is recorded in `docs/audit/06-proxy-application.md` §11.
5. `direct`, `system` and `clear-proxy-settings` MUST be listed as forbidden outcomes for a
   connector-triggered resolution (`packages/schema/failure-policy.schema.json:291`), and no failure path MAY
   resolve to them.
6. The three failure planes MUST stay distinct, because their correct responses differ: `coreDeath` blocks
   (highest severity, traffic is still being routed), `serviceTeardown` MAY hold the current setting for a
   bounded grace window and then hard-caps to block — permitted only because the connector can come back and
   re-apply the same config — and `extensionLifecycle` restores or re-applies and MUST NOT depend on the
   connector being reachable (`packages/schema/failure-policy.schema.json:36`, `:53`).
7. A block MUST be visible: `notificationLevel: silent` is allowed for health changes and never for a blocked
   state (`packages/schema/connector-settings.schema.json:167`).
8. For this iteration the mechanism is specified and contracted; it is not executed. No product code MAY ship
   a fail-open path that rule 2's confirmation does not cover.

## Consequences

Cheaper: "is my traffic leaking?" has one answer instead of a per-platform guess; the confirmation requirement
becomes a schema rejection rather than a code review note; and because the block is a normal `chrome.proxy`
write, it reuses the funnel the audit identified as the single writer of proxy state
(`omega-target/src/options.coffee:654`) instead of adding a second one.

Harder: a Core crash now means the user sees a broken browser until the connector restarts, which is exactly
the pressure that will generate feature requests for a default fail-open — the counter-argument is recorded
below rather than deferred; the grace-window logic of rule 6 needs timers that survive a service-worker
eviction; and a closed-port block must be *provably* closed, which couples this ADR to ADR-0006's port
discipline.

Forbidden: making `failOpen` a global setting; using `+direct`, `mode: direct` or `clear()` to "unblock" the
user during a failure; restoring the proxy silently as a side effect of a diagnostics action; and shipping a
UI toggle for fail-open without the confirmation token round trip.

## Alternatives considered

- **Fail open with a notification.** Rejected: it is the behaviour the product exists to prevent. A user who
  chose a proxy for its exit IP gets an unproxied session and a toast; on a public network that is a privacy
  and safety failure, not a performance one. It is retained as a per-profile, confirmed exception precisely
  so a user who wants availability over confidentiality can say so explicitly.
- **`+direct` as the block mechanism.** Rejected as factually wrong, not merely risky: `+direct` compiles to
  `DIRECT` (`omega-pac/src/profiles.coffee:89`), i.e. full bypass. Recorded here because the instinct is
  strong enough to appear in review comments.
- **`chrome.proxy.settings.clear()` to "stop proxying".** Rejected for the same reason — clear returns control
  to the system resolver and any policy or OS proxy, which is the definition of leaking. It is the right actuator
  for a `SystemProfile`, which is what the legacy code uses it for at
  `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:14`.
- **Suspend the browser's network state (e.g. flip a firewall rule or revoke the tab).** Rejected: outside the
  extension's permissions, unrecoverable if the connector dies holding it, and a second failure mechanism to
  reason about.

## Evidence

- `packages/schema/connector-settings.schema.json:153` and `packages/schema/connector-settings.schema.json:156`
  — VERIFIED — `failClosedDefault` is required and const-"block".
- `packages/schema/failure-policy.schema.json:275` — VERIFIED — the closed-loopback FixedProfile is named as
  the fail-closed primitive, with the leak argument inline; `:88` — VERIFIED — `forbiddenOutcomes`;
  `:109`/`:336` — VERIFIED — per-profile `failOpen` and the required `confirmToken`.
- `packages/schema/examples/invalid/reasons.json:45` — VERIFIED — a fail-open without confirmation is a
  negative case the harness must reject.
- `omega-pac/src/profiles.coffee:122` — VERIFIED — `+direct` → `pacResult()`;
  `omega-pac/src/profiles.coffee:89` — VERIFIED — the no-proxy branch returns the literal `DIRECT`.
- `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:19` — VERIFIED — `mode` set to
  `direct`; `:14` — VERIFIED — `clear()` for the system profile.
- `packages/schema/fixtures/protocol/failure-block.jsonl:1` and `:4` — VERIFIED — the recorded sequence
  `core.exited` then `core.blocked`, i.e. the intended behaviour written as a replayable fixture.
- Rule 6's eviction-surviving timers, and the runtime behaviour of a block — UNVERIFIED by construction
  (no connector, no browser run in this iteration).
- The gap this decision recorded and the contract has now closed — VERIFIED:
  `packages/schema/failure-policy.schema.json#/properties/coreDeath/properties/action` is `["block"]`, so the
  **global** `coreDeath.action` cannot be `failOpen` by any document, only the per-profile override can, and
  `packages/schema/examples/invalid/failure-policy.global-failopen.json` is a rejected case that keeps the enum
  from being widened back. `serviceTeardown.then` was narrowed the same way for the same reason
  (`docs/architecture/30-failure-policy.md` §5). Both are listed as amendments in
  `docs/architecture/30-failure-policy.md` §15 rather than presented as the original design.

## Verification

Today: `npm --prefix packages/schema run verify` compiles `connector-settings.schema.json` and
`failure-policy.schema.json`, validates `packages/schema/examples/failure-policy.default.json` and rejects
`examples/invalid/profile.failopen-without-confirm.json` on the `required` keyword plus
`examples/invalid/failure-policy.global-failopen.json`, `examples/invalid/failure-policy.teardown-restore.json`
and `examples/invalid/failure-policy.narrow-forbidden.json` on the rules added by
`docs/architecture/30-failure-policy.md` §15, and replays the `failure-block.jsonl` frames against the envelope.

The claim that used to be false is now checked: `packages/schema/failure-policy.schema.json:293` said the verifier
asserts `forbiddenOutcomes`, and `packages/schema/scripts/verify.mjs` had no such assertion. It has one now — a
section that reads the resolution vocabulary out of the compiled schema and rejects any plane that can resolve to a
forbidden outcome, together with the loopback-only, no-bypass-list, `assertClosed`-default and
block-port/allocation-port disjointness assertions. The remaining half is runtime and owned by `LIVE-11`: that a
blocked state survives a killed Core, that no error path can write a direct or system profile, and that a fail-open
profile is visibly marked in the UI.

Verified: PARTIAL — the contract half is green today and runs in CI; the browser half is `NO — deferred to Phase 2`.
