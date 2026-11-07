# Architectural Decision Records

This directory is the decision log for NEXORA -- the transformation of ZeroOmega v3.5.2 into a browser
extension plus a local Go Connector running Xray Core. Everything here is Phase 1: the decisions bind the
implementation, and no product code of the new system has been written yet.

**Read `0001-adr-practice.md` first.** It defines the format every record in this directory follows
(`Status` / `Date` / `Context` / `Decision` / `Consequences` / `Alternatives considered` / `Evidence` /
`Verification`), the normative vocabulary (`MUST`, `MUST NOT`, `SHOULD`, `MAY`), and the rule that a decision
names its own check or is recorded as `Verified: NO -- deferred to Phase 2`.

**Numbering is append-only.** A number is never reused, re-ordered or renamed to fit a new topic. If a
decision changes, a new ADR is written that supersedes the old one and the old file is marked
`Superseded by ADR-NNNN` in its `Status:` line -- supersession happens only through a new ADR, never by
rewriting history in place. All twenty records numbered up to `0020` were authored in the same Phase 1
iteration, so they share one date; that is not a claim that they were taken blindly. `0021` is the first record
written after that iteration, and its date is the sitting that found the answer in data the earlier records had
already committed.

## Index

| ID | Title | Status | Date | One-line hook |
|---|---|---|---|---|
| 0001 | [ADR practice](0001-adr-practice.md) | Accepted | 2026-11-04 | Every decision is an append-only record that names the check proving it |
| 0002 | [Keep `omega-*` directories, additive layout](0002-keep-omega-directories-additive-layout.md) | Accepted | 2026-11-04 | Legacy projects do not move; NEXORA is an overlay, because the build graph is path-coupled |
| 0003 | [Xray implements all transport protocols](0003-xray-implements-all-transport-protocols.md) | Accepted | 2026-11-04 | VLESS/REALITY/Vision live in Core behind Go; JavaScript implements none of them |
| 0004 | [Native Messaging only, no loopback HTTP control API](0004-native-messaging-only-no-loopback-http-control.md) | Accepted | 2026-11-04 | OS pipes plus sender identity instead of a local port any process or page can reach |
| 0005 | [Two-process connector](0005-two-process-connector.md) | Accepted | 2026-11-04 | A thin NM host over a local pipe to a long-lived service, so a tab reload cannot kill Core |
| 0006 | [Loopback-only inbounds](0006-loopback-only-inbounds.md) | Accepted | 2026-11-04 | Core binds `127.0.0.1`/`::1` only, non-configurable, asserted at `core.start` |
| 0007 | [Typed versioned command allowlist](0007-typed-versioned-command-allowlist.md) | Accepted | 2026-11-04 | Nineteen closed, typed methods -- no shell, no RPC pass-through |
| 0008 | [Idempotency and replay](0008-idempotency-and-replay.md) | Accepted | 2026-11-04 | The request id is the idempotency key; replays converge, probes are never replayed |
| 0009 | [Fail closed by default](0009-fail-closed-by-default.md) | Accepted | 2026-11-04 | A dead Core blocks traffic; fail-open is a confirmed, always-visible per-profile exception |
| 0010 | [Data split: extension owns intent, connector owns truth](0010-data-split-extension-intent-connector-truth.md) | Accepted | 2026-11-04 | Intent lives in the envelope, executable config lives in the Connector, secrets cross neither |
| 0011 | [Windows keystore is DPAPI-protected files](0011-windows-keystore-dpapi.md) | Accepted | 2026-11-04 | `%LOCALAPPDATA%\NEXORA\keystore\`, per-user, honestly not a boundary against the local user |
| 0012 | [NEXORA keys under the dash prefix](0012-nexora-keys-under-dash-prefix.md) | Accepted | 2026-11-04 | `-nexora.*` never `+…`, because `Profiles.each` would turn an entity into a selectable profile |
| 0013 | [Omega conditions compile into Xray routing](0013-omega-conditions-compiles-into-xray-routing.md) | Accepted | 2026-11-04 | Conditions still choose the profile; representable ones are compiled, the rest are reported |
| 0014 | [Schema version 3 with backup-before-migrate](0014-schema-version-3-and-backup-before-migrate.md) | Accepted | 2026-11-04 | An ordered, resumable runner whose destructive steps are gated on a verified restore |
| 0015 | [Continue the ZeroOmega version line](0015-continue-zeroomega-version-line.md) | Accepted | 2026-11-04 | `3.6.x` then `4.x`, same ID -- restarting at `1.0.0` would trip the `3.3.0` wipe guard |
| 0016 | [Svelte 5 + Vite + TypeScript](0016-svelte-5-vite-typescript.md) | Accepted | 2026-11-04 | Custom-element islands for CSS scope and cold start; reversible by design |
| 0017 | [Single i18n source, compiled twice](0017-single-i18n-source-compiled-twice.md) | Accepted | 2026-11-04 | `.po` files feed `_locales` and a UI catalog; the 27-vs-7 gap is scoped out loud |
| 0018 | [Workspace roots, no root `package.json`](0018-workspace-roots-and-no-root-package-json.md) | Accepted | 2026-11-04 | Per-root installs so no tool can accidentally drive the legacy grunt graph |
| 0019 | [Xray is a pinned downloadable asset](0019-xray-as-integrity-pinned-downloadable-asset.md) | Accepted | 2026-11-04 | Exact tag, sha256 and signature verified, never in git, never in the zip, no self-update |
| 0020 | [Test strategy](0020-test-strategy.md) | Accepted | 2026-11-04 | Schema validity, golden configs, env-gated live matrix -- and egress must be proven |
| 0021 | [Subscription Servers travel in the refresh result](0021-subscription-servers-travel-in-the-refresh-result.md) | Accepted | 2026-11-07 | The feed body never crosses the pipe; parsed Servers arrive in one result frame; an event carries counts |

## How the set fits together

| Plane | Records |
|---|---|
| Practice and layout | 0001, 0002, 0018 |
| Connector and Core boundary | 0003, 0004, 0005, 0006, 0019 |
| Protocol semantics | 0007, 0008 |
| Failure and data ownership | 0009, 0010, 0011, 0021 |
| Legacy compatibility | 0012, 0013, 0014, 0015, 0017 |
| UI | 0016 |
| Assurance | 0001, 0020 |

Dependencies run mostly one-way: 0010 is what makes 0011 and 0019 necessary; 0012 is what makes 0013, 0014
and 0015 survivable; 0009 constrains what 0016 and 0020 are allowed to display as success.

## Adding a record

1. Take the next number. Do not edit an existing decision in place to change its outcome.
2. Follow the section order of 0001, write `Date: 2026-11-04` style dates, and use
   `MUST` / `MUST NOT` / `SHOULD` / `MAY` normatively.
3. Put a `path:line` citation behind every factual claim about the legacy code, and tag each evidence item
   `VERIFIED` / `INFERRED` / `UNVERIFIED`. `node tools/check-doc-evidence.mjs` fails on a citation that no
   longer resolves, so a stale reference is a build break, not a typo.
4. Name the check in `## Verification`, or state `Verified: NO -- deferred to Phase 2` and give that claim a
   row in `docs/architecture/39-verification-matrix.md`.
5. Add a row to the index above -- the index is expected to match the files in this directory exactly.
6. If the new record replaces one, add `Superseded by ADR-NNNN` to the old record's `Status:` line and link
   forward from it.

## Honesty notes

- Titles in 0001-0010 were taken from those files as written in this same iteration; if a title changes, the
  index row changes with it. A number never changes meaning.
- Most of these decisions are contract-backed, not behaviour-backed: `packages/schema` is green
  (`npm --prefix packages/schema run verify`) and asserts the data rules, while the Connector, the compiler,
  the migration runner and the UI do not exist yet. Read each `## Verification` section before treating a
  decision as implemented.
- Two decisions were recorded with a caveat rather than a clean conscience: 0011 states that DPAPI is not a
  boundary against the local user, and 0017 states that twenty locales are present but not shipped. Both are
  the agreed position, not an oversight.
