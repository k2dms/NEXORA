# 24 — Options envelope compatibility

> Purpose: how NEXORA entities enter the legacy flat options envelope without disturbing anything that reads
> it — the key registry, why every entity is a `-` scalar key, the sync strip set, the registered
> `ConnectorProfile` handler that keeps existing rules working, and the exact amendment `transformValueForSync`
> needs (which is not the amendment the plan assumed).

Status: Accepted. The registry is data: `packages/schema/examples/envelope-keys.nexora.json`, shape enforced by
`packages/schema/envelope-keys.schema.json`.

## 1. Reading rules

As in `docs/architecture/20-target-repository-layout.md` §1.

## 2. What the envelope is, and why it is the boundary

The whole options state is one flat object mapping string keys to JSON values. Two prefixes carry all the
meaning: `+name` is a selectable OmegaPac profile, `-settingName` is a scalar setting
(`packages/schema/envelope-keys.schema.json#/properties/profilePrefix`,
`#/properties/scalarPrefix`). The engine iterates *only* `+` keys when it needs profiles:

```coffeescript
each: (options, callback) ->
  charCodePlus = '+'.charCodeAt(0)
  for key, profile of options when key.charCodeAt(0) == charCodePlus
    callback(key, profile)
```

(`omega-pac/src/profiles.coffee:111-114`, with builtins decorated at `:115-118`.)

That twelve-line function is the compatibility boundary of this entire project. Anything NEXORA writes under a
`+` key becomes a profile: it appears in the quick-switch list, in every rule picker, in `availableProfiles`, and
in the generated PAC. Anything written under `-` is invisible to the engine and must therefore be read by
NEXORA's own registry — which is exactly the trade this design chooses: **invisible to legacy, visible to us.**
The shipped default envelope is `omega-target/src/default_options.coffee:1-6`, with `schemaVersion: 2` at `:2`.

## 3. The key registry

| Key pattern | Entity | Value schema | Sync-eligible | Secret-bearing | Notes |
|---|---|---|---|---|---|
| `^-nexora\.server\.<uuid>$` | Server | `packages/schema/server.schema.json` | yes | no | references are inert on another machine |
| `^-nexora\.profile\.<uuid>$` | ConnectorProfile | `packages/schema/connector-profile.schema.json` | yes | no | the selectable intent |
| `^-nexora\.routing\.<uuid>$` | RoutingPolicy | `packages/schema/routing-policy.schema.json` | yes | no | browser-plane rule sets, §6 |
| `^-nexora\.subscription\.<uuid>$` | SubscriptionSource | `packages/schema/subscription-source.schema.json` | yes | no | bare https URL, no userinfo, plus `headersRef` |
| `^-nexora\.settings$` | ConnectorSettings | `packages/schema/connector-settings.schema.json` | yes | no | port range, telemetry opt-outs, proxy mode |
| `^-nexora\.failurePolicy$` | FailurePolicy | `packages/schema/failure-policy.schema.json` | yes | no | the global policy; per-profile override lives inside the profile |
| `^-nexora\.pins$` | CorePins | `packages/schema/pins.schema.json` | yes | no | hashes of the pinned Core assets |
| `^-nexora\.importStaging\.<uuid>$` | ImportStaging | `packages/schema/import-staging.schema.json` | **no** | **yes** | raw fetched payload; hard-expiring |
| `^-nexora\.backup\.<iso>$` | EnvelopeBackup | `packages/schema/envelope-backup.schema.json` | **no** | **yes** | pre-migration snapshot, local-only |
| `-nexora.schemaVersion` | scalar `3` | — | yes | no | `packages/schema/envelope-keys.schema.json#/properties/schemaVersionKey` |

Nine namespaces, of which exactly two are `secretBearing` and both of those are `syncEligible: false` — the
registry is generated and asserted so that the rule "`secretBearing ⇒ ¬syncEligible`" is never vacuous. Legacy
keys the extension keeps owning are listed in the registry's `legacyOwned` array — including `-optionsSync`,
`-schemaVersion` and `-customCss` — and the attachment convention `^__ruleListOf_` is declared there so that no
NEXORA key may claim it (`docs/audit/12-what-must-not-change.md`).

## 4. Why one scalar key per entity — and what the strip set may contain

**Servers are separate scalar keys** (`-nexora.server.<id>`), not one `-nexoraServers` object. Three reasons, all
measured against the legacy sync path:

1. **Per-entity quota.** `chrome.storage.sync` has a per-item cap, and the legacy layer already translates
   browser quota errors into its own `QuotaExceededError` (`omega-target/src/storage.coffee:18-20`, with the
   sibling `RateLimitExceededError` at `:10-12` and the fatal `StorageUnavailableError` at `:29-31`). One object
   holding 300 servers exceeds the per-item cap on the first subscription refresh; 300 keys do not. The numbers
   and the queue behaviour are `docs/audit/05-storage-sync-and-migration.md`.
2. **Churn.** A whole-object diff means every refresh rewrites every server: 600 ms push queue plus per-item
   write quotas (`omega-target-chromium-extension/src/module/sync_storage.coffee` — the module the Chromium
   target builds on) turns a routine subscription update into a burst that either rate-limits or loses the
   optimistic-concurrency token `lastGistCommit`.
3. **Sharing granularity.** A profile may reference servers from three subscriptions. With per-key entities the
   sync of one subscription cannot delete another's servers; with one object, a partial write destroys them.

**The strip set (`syncExcludedValueKeys`) is a confidentiality and size mechanism, not a correctness one, and it
has two hard bounds** — `packages/schema/envelope-keys.schema.json#/properties/syncExcludedValueKeys`:

1. **Strip values, never references.** `userIdRef`, `headersRef` and the like match `^keystore://…`: on another
   machine they point at nothing, so removing them buys no secrecy. What protects a reference that arrives
   without its secret is the receiving extension probing its own keystore and degrading the profile visibly
   (`docs/architecture/32-secrets-keystore-masking.md`), not amnesia in transit.
2. **The strip set must not intersect any sync-eligible schema's `required`.** An earlier draft of this registry
   stripped `userIdRef` (required in `packages/schema/server.schema.json#/required`), `headersRef`, and — worse —
   `publicKey`/`shortId`, which are REALITY's *public* parameters. The result was a synced Server that fails
   validation on the receiving machine and a handshake the user cannot fix. The assertion
   "sync strip set does not strip a required field" in `packages/schema/scripts/verify.mjs` now enforces both
   bounds over every registered namespace, with a non-vacuity guard on the number of namespaces compared.

What the list therefore contains: the legacy four (`lastUpdate`, `ruleList`, `pacScript`, `sha256`) plus
`headers` — closing SEC-1 — plus `body`, the raw staged payload
(`packages/schema/import-staging.schema.json#/properties/body`). `password` is *not* in the list yet, because
stripping it changes existing users' sync behaviour: recorded as an open decision in
`docs/architecture/40-open-decisions.md`, owned by `docs/architecture/38-compatibility-matrix.md`.

## 5. The bridge: a registered `ConnectorProfile` handler

ADR-0012 puts entities under `-`; ADR-0013 requires that the *user's existing rules* keep selecting things.
Those two are reconciled by one object: an OmegaPac profile type registered under the name `ConnectorProfile`
whose profile object lives at `+<displayName>` and whose **body holds only references**.

```
+Work            → { profileType: "ConnectorProfile", serverIdsKey: "-nexora.profile.<uuid>", … }
-nexora.profile.<uuid> → the ConnectorProfile entity (selection, failover, failurePolicy, serverIds)
-nexora.server.<id>    → each Server it references
```

Two shapes are possible and the difference matters:

- **(a) a thin `+` profile that points at the `-` entity** — chosen. The `+` object is small, stable, and its
  `profileType` is what the legacy dispatcher switches on; all mutable detail lives under `-`.
- **(b) the whole ConnectorProfile stored under a `+` key** — rejected: the envelope's one-key-one-purpose rule
  (`docs/audit/04-data-model-and-options-envelope.md`) means a `+`-keyed document is *parsed by legacy code*, so
  every new field in `ConnectorProfile` becomes a legacy parser's problem, and `Profiles.each` would show the raw
  entity to the user.

What the handler must implement is the legacy dispatcher contract, unchanged in meaning
(`docs/audit/04-data-model-and-options-envelope.md` §2):

| Hook | Behaviour of `ConnectorProfile` | Why |
|---|---|---|
| `create(profile)` | default `serverIds: []`, `selection.mode: 'ordered'`, `enabled: true` | a half-written profile must still be loadable |
| `includable` / `inclusive` | `true` / `true` | a SwitchProfile may select it; it contains no sub-profiles of its own |
| `directReferenceSet` | the `serverIds` under a `-nexora.server.` namespace **plus** the `routingPolicyId` | this is what makes "delete this server" ask about dependents |
| `analyze` | `[]` — no browser-plane rules live on the profile | routing rules stay on the `SwitchProfile` the user already has (§6) |
| `match` | not used for selection: a ConnectorProfile is a *target*, not a rule | one plane decides (ADR-0013) |
| `compile` | `SOCKS5 127.0.0.1:<port>` / `PROXY 127.0.0.1:<port>` for the selected server's inbound | the same string shape legacy produces today (`omega-pac/src/profiles.coffee:82-89`) |
| `replaceRef` | rewrites `serverIds` entries | rename/delete of a server must not orphan a profile |
| `referencedBySet` | derived from every `SwitchProfile`/`RuleListProfile` that names this profile | keeps the delete-guard honest |
| `update` / `updateUrl` | only on the subscription-derived ones | refresh semantics unchanged (`docs/audit/09-subscriptions-import-export-sync.md`) |

The `compile` row is the load-bearing one: because the handler emits a loopback proxy string in the shape the
PAC generator already understands, **existing user rules that name a NEXORA profile work with no migration**,
and the browser-side decision machinery is untouched (ADR-0013). The port it emits is not invented at compile
time — it is the allocated inbound reported back by `config.apply`
(`packages/schema/protocol/commands/config.apply.json#/result/properties/inboundPorts`) and stored in the
profile, which is why `docs/architecture/27-config-compilation-pipeline.md` requires port assignments to survive
a restart.

## 6. Rules, temp rules and attachments stay where they are

- Browser-plane rules continue to live in `SwitchProfile.conditionRules` under `+name` — unchanged, because
  `Conditions` remains the selection mechanism (ADR-0013, `docs/architecture/28-routing-model.md`).
- `RoutingPolicy` entities under `-nexora.routing.*` carry the *NEXORA-authored* policy sets (per-profile
  domain strategy, core-plane hints) that have no legacy representation; a policy is referenced by id, never
  inlined into a profile.
- Temp rules keep using `chrome.storage.session` and the `-tempRuleList`/`__tempZeroRuleListPac` mechanism
  (`omega-target/src/options.coffee:26`) — see the MV3 floor caveat in
  `docs/audit/05-storage-sync-and-migration.md`.
- `__ruleListOf_<name>` stays the attachment key for a `RuleListProfile`'s rule list, and NEXORA MUST NOT
  reuse it (`packages/schema/envelope-keys.schema.json#/properties/attachmentKeyPattern`).

## 7. Two version numbers in one envelope

`-schemaVersion` is legacy-owned and stays `2` for as long as legacy code reads it
(`omega-target/src/default_options.coffee:2`). NEXORA's own version lives at `-nexora.schemaVersion` and starts
at `3` (`packages/schema/envelope-keys.schema.json#/properties/schemaVersionKey`). The reason for two keys is
the wipe trap: the legacy startup comparison at
`omega-target-chromium-extension/src/coffee/background.coffee:32-46` deletes storage under a condition that
depends on the *packaged version*, and no NEXORA migration path may become reachable from
`initWithOptions()`'s remove-then-set (`omega-target/src/options.coffee:113-121`, the removal at `:117`). A
separate key means a NEXORA migration can be idempotent and resumable without ever feeding the legacy
comparator a value it will react to. Migration itself is `docs/architecture/35-schema-versioning-migration.md` §3–§6.

## 8. The amendment `transformValueForSync` actually needs

The plan said "add `secretRef`/`credential` to `transformValueKeys`". Reading the function changes the task
(`omega-target/src/options.coffee:89-99`):

```coffeescript
@transformValueForSync: (value, key) ->
  if key is '-customCss'
    return undefined
  if key[0] == '+'
    if OmegaPac.Profiles.updateUrl(value)
      profile = {}
      for k, v of value
        continue if transformValueKeys.indexOf(k) >= 0
        profile[k] = v
      value = profile
  return value
```

Three facts follow:

1. The strip list applies **only to `+` keys**, and only when `OmegaPac.Profiles.updateUrl(value)` is truthy
   (`:92-93`). Adding a name to `transformValueKeys` therefore does nothing for `-nexora.*` keys, which is where
   every NEXORA entity lives.
2. So the amendment is a **new branch**, not a list edit: for keys matching `^-nexora\.` the sync layer must
   apply `syncExcludedValueKeys` per value, and for legacy `+` subscription profiles the existing branch keeps
   running with `headers` added to the list — the latter is the SEC-1 closure.
3. `-customCss` is dropped wholesale (`:90-91`), which is the precedent for "some data simply does not travel":
   `-nexora.importStaging.*` and `-nexora.backup.*` are excluded by *namespace* (`syncEligible: false`), not by
   field stripping. That is the cleaner mechanism and the reason the registry has both knobs.

Where this lands in code is Phase 2, but the contract is stated now so it can be tested:
`packages/schema/envelope-keys.nexora.json` is the source of truth, `packages/state` reads it, and the legacy
sync module must not hard-code a second list (`docs/architecture/20-target-repository-layout.md` §3 forbids a
copy). `Verified: NO — deferred to Phase 2`; `LIVE-19` is the row that moves a REALITY server between two
profiles and asserts the reference travels while the value does not, and `LIVE-09` owns the handshake half.

## 9. Non-goals

No re-encoding of the envelope into a nested document tree. No renaming of legacy keys. No change to
`Profiles.each`. No new sync transport (Gist/WebDAV stay as they are, with SEC-1's fields stripped as data and
SEC-5 moved to the keystore — the built-in-sync write that bypasses the strip list stays open, see
`docs/architecture/32-secrets-keystore-masking.md` §14). No attempt to make `+`-keyed legacy profiles secret-free by editing them in place — the user's data
is migrated, not rewritten behind their back.

## 10. Invariants of this document and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| No NEXORA namespace pattern begins with `+` | "namespace stays out of the profile prefix" assertion in `packages/schema/scripts/verify.mjs` | **GREEN** |
| `secretBearing ⇒ ¬syncEligible` per namespace | the same loop's assertion, non-vacuous for `importStaging` and `backup` | **GREEN** |
| `headers` is stripped (SEC-1 closed as data) | "syncExcludedValueKeys strips headers" assertion | **GREEN** |
| Strip set never intersects a sync-eligible `required` | "sync strip set does not strip a required field" assertion + its two non-vacuity guards | **GREEN** |
| Every registered `valueSchema` file exists | the namespace loop's schema-existence check | **GREEN** |
| Backup and version keys are declared in the registry | `packages/schema/envelope-keys.schema.json#/properties/backupKeyPattern`, `#/properties/schemaVersionKey` | **GREEN** |
| `ConnectorProfile` compiles to a loopback proxy string | nothing (no handler yet) | Verified: NO — deferred to Phase 2, LIVE-03 |
| `transformValueForSync` honours the registry for `-nexora.*` | nothing | Verified: NO — deferred to Phase 2 |
| Existing `SwitchProfile` rules select a NEXORA profile unchanged | nothing | Verified: NO — LIVE-15 |
| `-schemaVersion` stays `2` while `-nexora.schemaVersion` is `3` | nothing | Verified: NO — deferred to Phase 2 |

Trace: ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0015 · Contract:
`packages/schema/envelope-keys.schema.json`, `packages/schema/examples/envelope-keys.nexora.json`, every
per-entity schema in §3 · See also: `docs/audit/04-data-model-and-options-envelope.md`,
`docs/audit/05-storage-sync-and-migration.md`, `docs/audit/11-security-findings.md`,
`docs/audit/12-what-must-not-change.md`, `docs/architecture/23-unified-server-model.md`,
`docs/architecture/34-data-split.md`, `docs/architecture/35-schema-versioning-migration.md`,
`docs/architecture/38-compatibility-matrix.md`.
