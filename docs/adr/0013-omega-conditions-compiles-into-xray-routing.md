# ADR-0013: Omega `Conditions` stay the browser-plane selector and are compiled into Xray routing where representable

Status: Accepted
Date: 2026-11-04

## Context

Rule-based profile selection is the most-used feature of the inherited product, and it is a working,
ten-year-tested engine. A `SwitchProfile` (`omega-pac/src/profiles.coffee:394`) holds a list of rules of
shape `{ condition: { conditionType, … }, profileName }` plus a `defaultProfileName`, and each condition is
dispatched through a registry of exactly **twelve** `conditionType` handlers --
`omega-pac/src/conditions.coffee:227-687`, starting at `omega-pac/src/conditions.coffee:230`
(`TrueCondition`) and ending at `omega-pac/src/conditions.coffee:661` (`TimeCondition`). Each handler
implements `analyze`, `match` and `compile`; `omega-pac/src/conditions.coffee:32-36` is the compile entry,
and `PacGenerator.script` (`omega-pac/src/pac_generator.coffee:25`) turns the whole profile graph into a
PAC `FindProxyForURL`. The Firefox path evaluates `match` directly instead of compiling, e.g.
`omega-pac/src/conditions.coffee:665` reads the local hour at request time.

Xray Core has its own routing section, and it is a different plane of existence: it decides which
*already-accepted* outbound to use, it never sees a URL, and -- per ADR-0006 -- it only sees traffic that
reached a loopback inbound. Domain, keyword, IP/CIDR and port rules are expressible there; `UrlWildcard`,
`UrlRegex`, `TimeCondition`, `WeekdayCondition` and the browser `BypassCondition` semantics are not, at
least not without re-deriving information Xray was never given. The full per-type table is the design
document `docs/architecture/28-routing-model.md`, informed by `docs/audit/07-routing-engine.md`.

The tempting move -- "Xray has routing, so rewrite the rules there" -- would throw away the URL, time and
weekday semantics, and would silently stop honouring rules users wrote years ago.

## Decision

1. Omega `Conditions` remain the mechanism that decides **which profile a request uses**. That decision
   stays in the browser plane (compiled PAC on Chromium, the proxy listener on Firefox). NEXORA MUST NOT
   replace it, and MUST NOT re-author its registry.
2. Browser-plane rules that are representable in Xray MUST additionally be **compiled** into the Core's
   `routing` section so that a single loopback port can fan out correctly once traffic is inside. The
   compilation is one-directional and reproducible: same envelope in, byte-identical config out.
3. The `browserPlane` of a RoutingPolicy keeps the legacy shape verbatim --
   `{condition:{conditionType,…}, profileName}` with a bare (unprefixed) `profileName` -- so that an
   existing user rule round-trips through `Conditions.fromStr`/`Conditions.str`
   (`omega-pac/src/conditions.coffee:37-52`) unchanged. This is asserted in
   `packages/schema/routing-policy.schema.json:5` and by the `$defs.condition` branches mirroring the twelve
   registry types.
4. The compiler MUST NOT silently drop a rule it cannot express. It MUST report it: the `config.apply`
   result carries an `unrepresentable` array of `{source, reason}` entries
   (`packages/schema/protocol/commands/config.apply.json:81`), and a RoutingPolicy document carries the
   same field (`packages/schema/routing-policy.schema.json:115`) with the rule written into its description
   at `packages/schema/routing-policy.schema.json:138`.
5. Reporting is not optional at the UI either: an unrepresentable rule MUST be visible where the rule was
   authored, and a policy whose rules are all unrepresentable MUST be surfaced as such rather than applied
   quietly. `dryRun` (`packages/schema/protocol/commands/config.apply.json:37`) is the sanctioned way to
   obtain that report without touching Core.
6. A rule whose target profile no longer exists MUST be reported the same way, never resolved to a default.
7. Order is normative: browser plane first, core plane second. A core-plane rule MUST NOT be assumed to be
   able to send traffic outside the Core, and MUST NOT be documented as if it could
   (`packages/schema/routing-policy.schema.json:70`).

## Consequences

- User-authored rules survive the cut-over with no rewrite, which is what makes constraint 13 (backward
  compatibility of user data) achievable at all.
- A new component exists: the rules compiler, with two outputs (PAC/listener behaviour unchanged, plus an
  Xray `routing` fragment) and one report. It is the component the golden-config fixtures of ADR-0020 exist
  to pin.
- Two planes must be kept consistent in the UI: a rule may be honoured twice, once before Core and once
  inside it. That is safe for `direct`/`block`/profile-targeted outcomes but must be stated, not discovered.
- Some rules will only ever be browser-plane. That is a documented, user-visible fact, not a defect -- the
  report is the product surface for it.
- The compiler must not "improve" on the legacy engine: it consumes `Conditions` as data, so any future
  change to the registry is a compatibility event tracked in `docs/architecture/38-compatibility-matrix.md`.

## Alternatives considered

- **Replace `Conditions` with a new NEXORA rule DSL compiled only to Xray routing.** Rejected: it discards
  the URL/time/weekday/bypass semantics Xray cannot see, forces every existing user rule through a lossy
  migration, and re-implements a well-tested engine -- the mass refactor the brief forbids.
- **Point the browser at one loopback port always, and put all routing inside Core.** Rejected: strictly
  weaker than today, and it would make `UrlWildcardCondition` and friends unrepresentable *by design*,
  leaving the drop to happen silently -- precisely the failure rule 4 exists to prevent.
- **Evaluate legacy conditions per request over Native Messaging.** Rejected: no such Core hook exists, and
  a per-request round trip through the port would dominate the latency it is supposed to save.
- **Leave legacy rules inert until the cut-over and re-author them.** Rejected by constraint 13 and by the
  parity-driven cut-over rule of ADR-0016.

## Evidence

- VERIFIED -- `omega-pac/src/conditions.coffee:227-687`: the `_conditionTypes` registry; twelve handler
  keys, `TrueCondition` at `omega-pac/src/conditions.coffee:230` through `TimeCondition` at
  `omega-pac/src/conditions.coffee:661`.
- VERIFIED -- `omega-pac/src/conditions.coffee:32-36`: `Conditions.compile` dispatches per `conditionType`
  through `_handler`.
- VERIFIED -- `omega-pac/src/pac_generator.coffee:25`: `PacGenerator.script` compiles the profile graph,
  and `omega-pac/src/profiles.coffee:394` is the `SwitchProfile` handler whose rules it compiles.
- VERIFIED -- `packages/schema/routing-policy.schema.json:115` and
  `packages/schema/protocol/commands/config.apply.json:81`: the `unrepresentable` report field exists in
  both the policy document and the command result, with the "report rather than drop" rule in their
  descriptions.
- INFERRED -- which of the twelve types map onto Xray `routing` fields: derived from reading both rule
  shapes; no Core binary on the host and no compiler written yet. Table lives in
  `docs/architecture/28-routing-model.md`.
- UNVERIFIED -- that a compiled core-plane fragment actually behaves identically to the legacy PAC for a
  given rule set. Requires a running Core: `docs/verification/`.

## Verification

- Existing check: `npm --prefix packages/schema run verify` validates the RoutingPolicy schemas and the two
  policy examples (`packages/schema/examples/routing.minimal.json`,
  `packages/schema/examples/routing.two-plane.json`), and rejects conditions whose shape deviates from the
  registry. Green at the time of writing (504 checks).
- Verified: NO -- deferred to Phase 2: the compiler itself, the representability table, and the
  "unrepresentable rule is reported, never dropped" assertion. Golden compiled configs (ADR-0020) are the
  intended check; until they exist this ADR's decision is contract-backed but not behaviour-backed.
