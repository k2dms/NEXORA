# ADR-0006: Core inbounds bind loopback only and the connector refuses to start otherwise

Status: Accepted
Date: 2026-11-04

## Context

Once ADR-0003 puts every protocol behind a local endpoint, the browser's only job becomes pointing at a
port on this machine. That port carries all of the user's traffic in cleartext to a process the user started,
and it is a functioning HTTP or SOCKS proxy: if it is reachable from anywhere but the local host, it is an
open proxy for the network it is reachable from, with no authentication behind it.

Nothing in the inherited code constrains a bind address, because nothing in it ever bound one. The legacy
path interpolates whatever host and port the profile stored (`omega-pac/src/profiles.coffee:87`) and the only
`127.0.0.1` constants in the profile model are *destination* bypasses — a default `FixedProfile` arrives with
`127.0.0.1` in its bypassList (`omega-pac/src/profiles.coffee:269`), which means "never proxy the loopback
host", the opposite of a bind constraint. The absence of a listener is confirmed independently:
`docs/audit/00-scope-and-method.md` §1 records zero Go and zero connector code, and
`docs/audit/11-security-findings.md` SEC-6 records that the tree has no concept of binary integrity, i.e. no
precedent for "this process may not do X".

So loopback-only cannot be inherited as a property of the code. It has to be an assertion made at the one
moment where it can be enforced — before the process exists.

## Decision

1. Every inbound the compiler produces MUST bind to a loopback address only: `127.0.0.1`, or `::1` where an
   IPv6-only host requires it. No other listen address is representable: the settings schema fixes the host
   with a `const` (`packages/schema/connector-settings.schema.json:83`), and the fail-closed block primitive
   uses the same two values (`packages/schema/failure-policy.schema.json:75`).
2. The bind address MUST NOT be a user setting, MUST NOT be readable from a subscription, and MUST NOT be
   overridable by a command argument. A caller MAY choose the port within the bounded range, and MAY NOT
   choose the host.
3. The connector MUST validate the compiled configuration before spawning Core: if any inbound's `listen`
   value is outside the loopback set, `core.start` MUST fail without starting the process, and MUST NOT
   partially apply. The compiled contract states the same invariant as a runtime assertion
   (`packages/schema/protocol/commands/core.start.json:72`), and every reported inbound MUST answer to the
   loopback enum (`packages/schema/protocol/commands/core.start.json:43`).
4. The connector MUST report the actual bound ports and addresses in the `core.start` result
   (`packages/schema/protocol/commands/core.start.json:34`) so the extension can compare intent with
   reality instead of trusting the file it sent.
5. After a successful start, the connector SHOULD re-check that it holds no listener outside loopback, and
   the verification matrix MUST assert the same thing from outside the process (row `LIVE-03`, via
   `Get-NetTCPConnection`).
6. A port conflict on a loopback port MUST be resolved by moving the allocation within the range, not by
   binding another address.

## Consequences

Cheaper: the inbound needs no authentication layer, because reachability is the authentication; there is no
LAN-exposure story to test, no firewall prompt, no "bind address" field to migrate, and the fail-closed
mechanism of ADR-0009 is the same primitive pointed at a port that is closed instead of one that is open.

Harder: no other application or device can be routed through NEXORA, which will read as a missing feature to
some users; local debugging from another machine (a remote test harness, a phone on the same LAN) is
impossible by design; and the compiler now owns a refusal path it did not ask for, which needs its own error
surface — the taxonomy names `UnsupportedCombination` for semantically invalid configs
(`packages/schema/protocol/error-taxonomy.json:14`), but `core.start` does not currently declare it, which is
a gap Phase 2 must close rather than paper over with `Internal`.

Forbidden for this iteration and beyond: adding an `inboundHost` setting; a `0.0.0.0` or LAN-interface bind
"in temporarily" for debugging; and documenting remote use as a roadmap item without a superseding ADR.

## Alternatives considered

- **Configurable bind address with a token or IP allowlist.** Rejected: it turns a personal proxy into a
  network service the moment a user copies a plausible-looking value out of a tutorial, and the mitigation
  (per-connection auth) is a second mechanism to get right. The stated threat — a neighbour or a compromised
  LAN host relaying traffic — is not one the product claims to serve.
- **A Unix socket / named pipe instead of a loopback TCP port.** Rejected on platform grounds rather than
  security ones: `chrome.proxy` accepts host and port only
  (`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:45`), so a socket would
  still need a TCP or HTTP shim in front of it.
- **One fixed port for every profile.** Rejected: a fixed well-known port is trivially fingerprintable by
  any local process and collides with other tools; the bounded-range allocation with
  `persistAssignments` keeps a reconnect from invalidating the `chrome.proxy` setting the browser already
  holds.
- **Trust the compiler and assert nothing at start.** Rejected: the compiler is the component that could be
  changed by a subscription-driven config, so the check has to live where the process is spawned.

## Evidence

- `packages/schema/connector-settings.schema.json:83` — VERIFIED — `"host": { "const": "127.0.0.1" }`, and
  `:84` states the refusal rationale.
- `packages/schema/protocol/commands/core.start.json:43` — VERIFIED — `"listen": { "enum": ["127.0.0.1",
  "::1"] }`; `packages/schema/protocol/commands/core.start.json:72` — VERIFIED — the invariant naming
  `LIVE-03`.
- `packages/schema/failure-policy.schema.json:75` — VERIFIED — the block primitive is loopback as well, so
  one address set covers both directions of the design.
- `omega-pac/src/profiles.coffee:87` — VERIFIED — legacy emits `SCHEME host:port` from caller data with no
  address constraint; `omega-pac/src/profiles.coffee:269` — VERIFIED — the `127.0.0.1` in the profile model is
  a bypass destination, not a proxy host.
- "Today's FixedProfile results already use 127.0.0.1" — INFERRED and partly falsified: what is verified is
  that a FixedProfile PAC result is a loopback-capable `host:port` string the *user* typed; nothing in the
  tree pins it to loopback, which is exactly why rule 3 exists.
- No connector or Core process exists to bind anything — VERIFIED by `docs/audit/00-scope-and-method.md` §1;
  rules 3, 4, 5 are therefore UNVERIFIED at runtime until Phase 2.

## Verification

Today: `npm --prefix packages/schema run verify` compiles `connector-settings.schema.json` and
`failure-policy.schema.json`, so rule 1 is enforced *declaratively* — a settings document that names another
host cannot validate, and `packages/schema/examples/settings.default.json:25` is the valid counterpart that
proves the schema still accepts a real configuration. Rule 3's enum is part of the compiled
`core.start` sub-schema that the `packages/schema/fixtures/protocol/core-lifecycle.jsonl` frames are
validated against.

Not checked today: that a running connector actually refuses a non-loopback bind, and that it holds no
listener outside loopback after starting. Those are Phase 2 rows `LIVE-03` and the Go unit test named in
`docs/architecture/27-config-compilation-pipeline.md`. Adding `UnsupportedCombination` to `core.start`'s declared
errors is a Phase 2 contract change with a fixture of its own.

Verified: NO — deferred to Phase 2 (rule 1 only, and only declaratively, is checked now).
