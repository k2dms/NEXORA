# ADR-0012: NEXORA entities are stored under `-nexora.*` keys, never as `+`-prefixed profiles

Status: Accepted
Date: 2026-11-04

## Context

The options envelope is one flat dictionary. Its key prefix is load-bearing semantics, not style:
`+something` means "this is an OmegaPac profile", `-something` means "this is a scalar setting". The
engine enforces that by construction. `omega-pac/src/profiles.coffee:111-118` is `Profiles.each`, and the
loop at `omega-pac/src/profiles.coffee:113` iterates only keys whose first character code equals the code
for `+`; `omega-pac/src/profiles.coffee:96` shows the inverse direction -- `nameAsKey` prepends `+` to a
bare profile name. Legacy settings already live on the other side, e.g.
`omega-target/src/default_options.coffee:3` (`-enableQuickSwitch`).

Everything downstream is built by iterating `Profiles.each`: the `availableProfiles` map is assembled in
`omega-target/src/options.coffee:613` and written to state at `omega-target/src/options.coffee:639`, the
quick-switch and rule-editor pickers read it, `validResultProfilesFor`
(`omega-pac/src/profiles.coffee:217`) and `allReferenceSet` (`omega-pac/src/profiles.coffee:193`) collect
reference sets the same way, and the generated PAC embeds the collected keys -- `pac_generator.coffee`
filters them at `omega-pac/src/pac_generator.coffee:32` and re-tests the `+` marker at runtime at
`omega-pac/src/pac_generator.coffee:138`.

So a NEXORA Server stored under `+nexora.server.<uuid>` is not "an odd profile": it is *advertised* as a
selectable profile in the quick-switch list, appears as a legal rule target, and is compiled into the PAC
script, where it resolves to a proxy config that does not exist. The failure mode is a silently broken
proxy for the user who never touched that key.

## Decision

1. Every NEXORA entity MUST live in the same options envelope, under a `-nexora.*` scalar key, one key per
   entity: `-nexora.server.<id>`, `-nexora.profile.<id>`, `-nexora.routing.<id>`,
   `-nexora.subscription.<id>`, `-nexora.settings`, `-nexora.failurePolicy`, `-nexora.pins`,
   `-nexora.importStaging.<id>`, `-nexora.backup.<iso>`, and the version scalar `-nexora.schemaVersion`.
   The authoritative list is the data registry `packages/schema/examples/envelope-keys.nexora.json`, whose
   patterns are of the form `packages/schema/examples/envelope-keys.nexora.json:7` for a Server.
2. NEXORA entities MUST NOT be `+`-prefixed profiles, and MUST NOT redefine a legacy-owned key. The
   contract states `profilePrefix` as `+`, reserved, with the MUST NOT written into the schema description
   `packages/schema/envelope-keys.schema.json:22`, and `scalarPrefix` as the NEXORA home
   `packages/schema/envelope-keys.schema.json:24`.
3. A NEXORA entity MAY be *represented* to the legacy UI as a profile when, and only when, it is genuinely
   a profile the user can select -- e.g. a ConnectorProfile surfaced as a `VirtualProfile`. That
   `+`-keyed object is legacy-owned surface data; the authoritative entity remains under `-nexora.*`. The
   derived object MUST be reconstructible from the `-` key, never the other way round.
4. Within values, bare profile names MUST NOT carry the `+`: `packages/schema/routing-policy.schema.json:155`
   enforces `^(?!\+).+$`, because `nameAsKey` adds the prefix itself and a stored `+direct` produces a rule
   that can never match.
5. Two further prefixes are reserved by the legacy engine and MUST NOT be reused: the attachment convention
   `packages/schema/envelope-keys.schema.json:90` (rule-list bodies) and the `-schemaVersion` legacy scalar.
   NEXORA keeps its own version key so the two migration histories never touch.
6. Adding a namespace means editing the registry and nothing else; `packages/schema/scripts/verify.mjs:359-372`
   rejects a pattern that starts with `+`, rejects `secretBearing && syncEligible`, and requires the
   referenced value schema to exist on disk.

## Consequences

- NEXORA entities are invisible to `Profiles.each`, therefore absent from the quick-switch list, the rule
  pickers, the reference sets and the generated PAC. Legacy behaviour is preserved without editing legacy
  code (constraint 5 of the brief).
- Per-entity keys keep sync diffs, quota accounting and deletion at entity granularity: writing one server
  rewrites one key. A single `-nexora` JSON blob would have made every subscription refresh a full-object
  rewrite against `chrome.storage` quotas.
- The namespace registry is the single place to audit "what may sync". The `secretBearing => !syncEligible`
  rule is a data assertion, so a future namespace cannot be added with credentials and sync turned on at
  the same time without the verifier failing.
- Export/import, backup and OptionsSync all keep working, because the entities ride in the envelope that
  those mechanisms already handle.
- Names become a public, append-only contract: renaming `-nexora.server.` to something else is a schema
  migration (ADR-0014), not a refactor.

## Alternatives considered

- **Store NEXORA entities as `+` profiles and filter them out by `profileType`.** Rejected: it requires
  changing `Profiles.each` and every consumer that assumes "`+` means selectable", which is the mass
  refactor of working code the brief forbids, and it leaves the PAC generator compiling objects it cannot
  resolve.
- **One nested document under a single `-nexora` key.** Rejected for envelope reasons: quota and sync-diff
  granularity, and a torn write to the whole blob destroys every entity at once instead of one.
- **A separate storage area outside the envelope (`chrome.storage.local` under our own namespace).**
  Rejected: it silently falls outside export, import, backup and OptionsSync, which is exactly the user
  data continuity ADR-0014/0015 exist to guarantee.
- **Firefox `browser.storage`-specific layout.** Rejected: the envelope is browser-agnostic already, and a
  second shape would fork every contract in `packages/schema`.

## Evidence

- VERIFIED -- `omega-pac/src/profiles.coffee:111-118`: `Profiles.each` iterates `+` keys only; line 113 is
  the guarded loop over `options`.
- VERIFIED -- `omega-pac/src/profiles.coffee:96`: `nameAsKey` returns `'+' + profileName`.
- VERIFIED -- `omega-target/src/options.coffee:613` and `omega-target/src/options.coffee:639`: the leak
  path -- an iteration over `+` keys becomes the `availableProfiles` state the UI reads.
- VERIFIED -- `omega-pac/src/pac_generator.coffee:32` and `omega-pac/src/pac_generator.coffee:138`: `+`
  keys reach the compiled PAC and the runtime result marker.
- VERIFIED -- `packages/schema/envelope-keys.schema.json:22` and `packages/schema/envelope-keys.schema.json:24`:
  reserved `+` prefix and the `-` home, in schema text.
- VERIFIED -- `packages/schema/scripts/verify.mjs:359-372`: the machine check on the key pattern, with the
  `profiles.coffee` citation carried in its failure message.
- VERIFIED -- `omega-target/src/default_options.coffee:3`: `-` scalars are the existing convention.
- INFERRED -- the concrete UI leak (an entity showing in the quick-switch list) follows from the code paths
  above but was not observed in a running browser; no NEXORA key has ever been written.

## Verification

- Existing check: `npm --prefix packages/schema run verify` -- the namespace loop
  (`packages/schema/scripts/verify.mjs:359-372`) fails the build on a `+` pattern, on a secret-bearing
  sync-eligible namespace, and on a missing value schema. Green at the time of writing (707 checks; the harness has grown since).
- Existing check: `node tools/check-doc-evidence.mjs` resolves every citation in this file.
- Verified: NO -- deferred to Phase 2: the negative runtime test "a `-nexora.server.<id>` key produces no
  entry in `availableProfiles` and no PAC reference". That needs a loaded extension and belongs to
  `docs/verification/`.
