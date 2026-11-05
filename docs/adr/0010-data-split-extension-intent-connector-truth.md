# ADR-0010: The extension owns user intent, the connector owns executable truth, and secrets cross neither

Status: Accepted
Date: 2026-11-04

## Context

Two components will hold overlapping data: the extension, which inherits Omega's flat options envelope and
its sync/export/backends, and the connector, which compiles a runnable Core configuration, stores
credentials and measures health. If the split is left implicit, both sides end up owning everything, and the
first evidence of that is already in this tree.

`docs/audit/11-security-findings.md` SEC-1 documents a real leak produced by an unstated ownership rule:
`omega-target/src/options.coffee:27` lists the value keys stripped before a profile is copied into the synced
envelope as `['lastUpdate','ruleList','pacScript','sha256']` — `headers` is not among them, and subscription
headers really do carry `Authorization`. The exclusion mechanism exists (`-customCss` is excluded, per the
same audit) and was simply never extended, because nothing in the design said which fields may never travel.
The envelope has a second trap: `omega-pac/src/profiles.coffee:111-118` iterates only keys whose first
character is `+`, so any entity stored under `+…` appears in `availableProfiles`, in the quick-switch lists and
in generated PAC — the mechanism that decides what the extension may store at all
(`docs/audit/12-what-must-not-change.md` §1).

## Decision

1. The extension owns **user intent**: profiles and their order, which servers a profile uses, routing
   rules in their legacy `SwitchProfile` shape, subscriptions as sources, connector settings, and everything
   a user can edit, export or sync. These are the entities registered under `-nexora.*` keys
   (`packages/schema/examples/envelope-keys.nexora.json:5`), never under `+` keys, and each namespace declares
   whether it is sync-eligible (`packages/schema/envelope-keys.schema.json:54`).
2. The connector owns **executable truth**: the compiled Xray configuration and its hash, the keystore, the
   applied-config state, per-server health history, probe results and the pinned Core binary
   (`packages/schema/pins.schema.json:5`). The extension MUST NOT store, cache or reconstruct any of these as
   its own source of truth; it MAY hold a copy for display only, obtained through `config.snapshot` or
   `connector.health`, and MUST treat it as an observation.
3. A secret MUST NOT leave the keystore. Every credential field in an intent document is a
   `keystore://` reference (`packages/schema/server.schema.json:49`), never a value, and the rule is
   enforced from the sink as well as the source: `diagnostics.get` is required to return a masked section
   (`packages/schema/protocol/commands/diagnostics.get.json:43`), and `config.snapshot`'s `maskedConfig`
   preserves field names and destroys values
   (`packages/schema/protocol/commands/config.snapshot.json:39`). `SecretRedacted`
   (`packages/schema/protocol/error-taxonomy.json:15`) exists so a call that would have carried a secret is
   refused rather than stripped.
4. That invariant MUST be testable, not aspirational: the snapshot contract states that grepping
   `maskedConfig` for any pinned secret value finds nothing
   (`packages/schema/protocol/commands/config.snapshot.json:51`), and the namespace rule
   `secretBearing ⇒ ¬syncEligible` is asserted over the registered keys
   (`packages/schema/scripts/verify.mjs:365-368`).
5. Cleanup is split the same way: `keystore.purge` deletes secrets by scope — including `orphaned` — without
   deleting intent, and refuses to strand an applied config
   (`packages/schema/protocol/commands/keystore.purge.json:17` and its `still-referenced` invariant at
   `:77`). A keystore that can only be emptied by reinstalling the connector is not a keystore.
6. The two halves MUST be reconcilable without either side owning the other: the extension sends intent, the
   connector compiles and reports drift (`packages/schema/protocol/commands/config.snapshot.json:30`), and
   the extension re-applies after a reconnect. Import MUST be an intent document arriving through
   `profile.import` and never a pre-compiled config.
7. For this iteration the split exists as schemas, command contracts and fixtures only. No product code MAY
   put an observation in the synced envelope or a value where a reference belongs.

## Consequences

Cheaper: the SEC-1 class of bug becomes a shape error instead of a review finding, because an intent document
cannot spell a credential; sync/export/backends keep working on user data without ever touching secrets;
health history and compiled config can grow, expire or be wiped without migrating anything the user sees; and
each side's storage quota problem stays on its own side of the pipe.

Harder: drift becomes a first-class state the UI must show ("the connector is running a config that is not
the one you last saved"), two documents must agree on every entity lifecycle, and the boundary needs a
catalogue — which key is intent, which is observation, which is secret — that must be maintained as data
(`packages/schema/examples/envelope-keys.nexora.json`) rather than remembered.

Forbidden: storing a token, a header map, a private key or a subscription body in the options envelope, in
`chrome.storage.sync`, in an export file, in a log, or in a diagnostics section; letting the connector create
or reorder user profiles on its own authority; and letting the extension write a compiled Core config directly.

## Alternatives considered

- **The connector owns everything, including profile ordering and rule editing.** Rejected: it would put the
  features that exist today (subscriptions, routing, settings) behind a process that does not exist yet, and
  the parity plan is explicit that three of the six UI sections ship before the connector. It also makes
  offline editing and the legacy sync backends depend on a local binary.
- **The extension owns the keystore and hands secrets to the connector per call.** Rejected: it recreates
  SEC-1 by design — a value that passes through the options envelope can be synced, exported and logged — and
  it makes "secrets never leave the keystore" untestable, because the keystore would live on the leaking side.
- **One shared store (a file both processes read).** Rejected: two writers, no ownership, no schema version
  story, and a secret-bearing file inside the extension's reach; the audit already shows how badly
  exclusion-list-only protection behaves (`omega-target/src/options.coffee:27`).
- **Mask only at display time, keep values in the synced envelope.** Rejected: the leak is at rest, not on
  screen — GitHub Gist and WebDAV receive the synced value verbatim, so a redacted UI over an unsanitised
  envelope protects nothing.

## Evidence

- `omega-target/src/options.coffee:27` — VERIFIED — the three-key exclusion list that omits `headers`; the
  full finding, including that the mechanism exists and was never extended, is in
  `docs/audit/11-security-findings.md` SEC-1.
- `omega-pac/src/profiles.coffee:111-118` — VERIFIED — the `charCodeAt(0) == '+'` filter that decides what the
  extension may keep private.
- `packages/schema/examples/envelope-keys.nexora.json:5` — VERIFIED — the `profilePrefix`/`scalarPrefix` split;
  `:61` — VERIFIED — `ImportStaging` as `syncEligible: false, secretBearing: true`; `:88` through `:93` —
  VERIFIED — `syncExcludedValueKeys` containing `headers`, i.e. SEC-1 expressed as data.
- `packages/schema/server.schema.json:49` — VERIFIED — `"pattern": "^keystore://[A-Za-z0-9._:/-]{1,96}$"`.
- `packages/schema/protocol/commands/config.snapshot.json:37` — VERIFIED — `maskedConfig`;
  `packages/schema/protocol/commands/diagnostics.get.json:4` — VERIFIED — "everything it returns is masked at
  the sink, size-bounded and ring-buffered".
- `packages/schema/scripts/verify.mjs:373-377` — VERIFIED — the harness asserts `headers` is stripped; `packages/schema/scripts/verify.mjs:365-368` —
  VERIFIED — the `secretBearing`/`syncEligible` assertion, non-vacuous only for the two namespaces above.
- `packages/schema/connector-profile.schema.json:5` — VERIFIED — an intent document that compiles to one
  loopback FixedProfile, which is the boundary in one sentence.
- The connector side of the split (keystore contents, compiled config, health history) — UNVERIFIED at
  runtime: `connector/` does not exist, so the durable specification is
  `docs/architecture/34-data-split.md` §6–§7 and the executable half is this package.

## Verification

Today: `npm --prefix packages/schema run verify` compiles all thirteen schemas, validates the 20 examples,
rejects the 24 negative cases — including `examples/invalid/server.inline-headers.json`, which fails because a
header map may not be a value — and runs the ownership, secret-vocabulary and error-coverage assertions the
sections above name, plus frame-level checks that no fixture places a secret-shaped value in an envelope field.

Deferred to Phase 2: the grep test that a real `maskedConfig` and a real `diagnostics.get` dump contain no
pinned secret (`LIVE-10`), the keystore round trip behind DPAPI, and a storage dump proving nothing
secret-shaped ever reaches `chrome.storage.sync`. `docs/architecture/34-data-split.md` §14 names the owner of
each check, and `LIVE-22` is that dump.

Verified: NO — deferred to Phase 2 for the runtime invariant; the structural half is checked now.
