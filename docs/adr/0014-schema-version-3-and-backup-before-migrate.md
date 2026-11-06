# ADR-0014: Envelope `schemaVersion` moves 2 -> 3 through a backup-gated, resumable migration runner

Status: Accepted
Date: 2026-11-04

## Context

User data in this product is one flat options envelope with a version number in it. The current value is 2
(`omega-target/src/default_options.coffee:2`), and the entire migration mechanism is `Options.upgrade`
(`omega-target/src/options.coffee:327-353`): if the stored version is 1 it performs one in-place edit and
assigns 2 (`omega-target/src/options.coffee:344`); any other value is rejected outright at
`omega-target/src/options.coffee:353`. There is no ordering, no per-step verification, no resumability, and
above all no backup -- it mutates the object the user depends on and hopes.

Adjacent paths are worse. `omega-target/src/options.coffee:113-121` is `initWithOptions`: when it is handed
an options object it calls `@_storage.remove()` (line 117) and only then `@_storage.set(options)`. A
truncated or malformed import therefore deletes the previous envelope before anything validates the
replacement. That is the pattern NEXORA MUST NOT repeat, and it is the reason "backup first" has to be a
blocking precondition rather than advice.

NEXORA adds entities to that envelope -- Servers, ConnectorProfiles, RoutingPolicies, subscription sources,
settings, failure policy, pins -- under `-nexora.*` keys (ADR-0012), including its own version scalar
`-nexora.schemaVersion` (`packages/schema/envelope-keys.schema.json:17`). Old extension versions will
therefore see data they did not write, and the Connector needs to know whether it is being handed a
pre-3.5.2 profile set or a migrated one. That is a schema-version event.

## Decision

1. The envelope schema version goes **2 -> 3**. The NEXORA value is held in `-nexora.schemaVersion`
   (`packages/schema/examples/envelope-keys.nexora.json:2`), and the legacy bare `schemaVersion` MUST stay
   `2` for as long as legacy `Options.upgrade` is in the load path, because that function rejects anything
   other than 1 or 2 (`omega-target/src/options.coffee:353`): bumping the legacy key without editing the
   engine makes the extension refuse to load its own data. A step MUST therefore write both, and the runner
   MUST be reviewed as a pair.
2. Migration runs through a **runner with a fixed contract**, not through an `if version == 2` branch:
   - **ordered** -- a declared step list; a step runs only after its predecessors are verified;
   - **idempotent** -- re-running a step against already-migrated state is a provable no-op;
   - **resumable** -- after a crash or an MV3 worker teardown, the run continues from the last verified
     step, never from the beginning and never past a step it did not verify;
   - **`dryRun`** -- the plan and the per-key diffs are reported without touching storage, the same idea the
     command surface already exposes for config (`packages/schema/protocol/commands/config.apply.json:24`);
   - **per-step `up` and `verify`** -- every step ships a verifier that re-reads storage and asserts the
     post-condition; a step whose `verify` fails is a failed run.
3. **Backup is blocking.** Before the first destructive step -- anything that removes, renames or
   irreversibly rewrites an envelope key -- the runner MUST write a full envelope snapshot to
   `chrome.storage.local` under a key matching the pattern
   `packages/schema/envelope-keys.schema.json:94` (`-nexora.backup.<iso>`, instance pattern at
   `packages/schema/examples/envelope-keys.nexora.json:66`) **and** a Connector-side file mirror
   (`packages/schema/envelope-backup.schema.json:57`). A step that only adds keys is non-destructive and
   MUST still be backed up, because rollback needs the before-image.
4. The backup record MUST satisfy `packages/schema/envelope-backup.schema.json`: `backupId` so a resume can
   tell its own snapshot from somebody else's, `fromSchemaVersion` / `toSchemaVersion`, `extensionVersion`
   (recorded precisely because the update path compares versions), `keyCount`, `contentSha256` over the
   canonically serialised payload, and `restoredAtLeastOnce`.
5. **Verified restore is part of the precondition, not a nice-to-have.** The runner MUST read the snapshot
   back, recompute its hash, compare, and only then set `restoredAtLeastOnce` true. "Write succeeded" is not
   a backup: a zero-key or unreadable snapshot is how a wipe gets recorded as a success, which is why
   `keyCount` exists at `packages/schema/envelope-backup.schema.json:46` and why a `keyCount` of 0 MUST abort
   the run. A quota failure on the backup write MUST abort before any destructive step, MUST NOT proceed
   "with the local copy only", and MUST NOT delete an older backup to make room.
6. On any failed step the runner MUST restore the backup, and MUST NOT half-apply. If restore itself fails,
   the extension MUST enter a visible "restore required" state and MUST NOT continue running on a partial
   envelope -- continuing is how a migration bug becomes a silent proxy failure (ADR-0009).
7. Legacy keys are preserved, never pruned, by this step: `+` profiles and legacy `-` settings keep their
   meaning until the cut-over (ADR-0013), so version 3 is *additive*. A removal step needs its own version
   and its own ADR.
8. The runner's resume cursor MUST live in a namespace that is first declared in the key registry
   (`packages/schema/envelope-keys.schema.json:28`), with a schema on disk, so ADR-0012 rule 6 accepts it.
   An undeclared ad-hoc key is a contract violation, and this ADR deliberately does not invent one.
9. Backups are `secretBearing: true` and `syncEligible: false`
   (`packages/schema/examples/envelope-keys.nexora.json:70`), so they never reach OptionsSync. Import staging
   follows the same discipline with a hard expiry (`packages/schema/import-staging.schema.json:50`), because
   a scratch buffer without a deadline becomes a credential store.

## Consequences

- Version 3 is cheap for existing users (additive) and the machinery now exists for the expensive step later.
- Storage usage roughly doubles during a migration; the runner MUST prune old snapshots *after* success, and
  the retention rule is a reviewable number.
- Because a snapshot is a copy of everything, it inherits the pre-keystore secrets still sitting in legacy
  profile fields. So the export path MUST NOT embed `-nexora.backup.*` payloads by default, and MUST scrub
  them if the user explicitly includes them. This is a new leak surface that only exists because backups do.
- The migration MUST be testable as restore-and-compare, which is exactly the assertion ADR-0020 rule 5 makes
  mandatory.
- `dryRun` gives the UI an honest "what will change" screen, so a destructive action can be shown as one
  (brief constraint 12).
- The schema is not yet as strong as the prose: `packages/schema/envelope-backup.schema.json:78` enforces
  `toSchemaVersion >= 2` but not "never decreases relative to `fromSchemaVersion`", which its own
  description at `packages/schema/envelope-backup.schema.json:39` claims. That asymmetry is a contract gap to
  close in Phase 2, and the runner MUST enforce the stronger rule meanwhile.

## Alternatives considered

- **Extend `Options.upgrade` with another `if` branch.** Rejected: the function has no ordering, no
  per-step verification, no resume and no backup, and its contract is "reject unknown versions", which means
  every older build in the field refuses to load a version 3 envelope. It also cannot be tested for
  recoverability, only for shape.
- **Lazy, versionless migration -- recognise NEXORA keys on read.** Rejected: the presence of a key is not a
  version, so a half-migrated envelope and a freshly added entity become indistinguishable, the Connector
  cannot know what it was handed, and there is no point at which to demand a backup.
- **Keep `schemaVersion` at 2 and put the version inside `-nexora.settings`.** Rejected: it hides the fact
  that the *envelope* changed, which is the one thing the sync/export/rollback paths need to see, and it
  duplicates version state that already exists as a scalar by convention.
- **Back up on the Connector side only (or as a file download only).** Rejected: rule 3 requires two media
  because a snapshot stored in the area being migrated proves nothing about quota or teardown, and a file the
  user must save manually will not be saved before a forced upgrade.
- **Full copy-on-write envelope (migrate into a new storage area, then flip).** Rejected for this step:
  doubles the sync and export surface permanently and breaks the "one envelope is the user's data" property
  ADR-0012 depends on.

## Evidence

- VERIFIED -- `omega-target/src/default_options.coffee:2`: the shipped default is `schemaVersion: 2`.
- VERIFIED -- `omega-target/src/options.coffee:327-353`: the whole legacy migration; `:344` assigns 2 from 1
  and `:353` rejects any other version with "Invalid schemaVersion".
- VERIFIED -- `omega-target/src/options.coffee:113-121`: `initWithOptions` removes storage at
  `omega-target/src/options.coffee:117` before setting the imported options.
- VERIFIED -- `packages/schema/envelope-backup.schema.json:46`, `:52`, `:57`, `:63`: `keyCount`,
  `contentSha256`, `connectorMirrorPath` and `restoredAtLeastOnce` all exist as required-or-documented
  fields, with the zero-key and unverified-backup hazards named in their descriptions.
- VERIFIED -- `packages/schema/examples/envelope-keys.nexora.json:2` and
  `packages/schema/envelope-keys.schema.json:94`: the version key and the backup key pattern; and
  `packages/schema/examples/envelope-backup.pre-migration.json:9` shows a mirror path in a valid instance.
- VERIFIED -- `packages/schema/import-staging.schema.json:50`: the hard-expiry field whose description states
  the deadline is machine-checked.
- INFERRED -- the legacy `schemaVersion` rejection breaking a bumped key: follows from reading `upgrade` and
  the load path; not exercised in a browser.
- UNVERIFIED -- storage quota behaviour during a snapshot write, MV3 teardown mid-migration, and restore
  success rates. All three need a running extension.

## Verification

- Existing check: `npm --prefix packages/schema run verify` compiles `envelope-backup.schema.json` and
  validates the pre-migration instance as one of its 19 valid examples, and the namespace loop
  (`packages/schema/scripts/verify.mjs` "namespace stays out of the profile prefix: ${ns.entity}" and "secret-bearing namespace is not sync-eligible: ${ns.entity}") keeps the backup and version keys declared, non-`+`, and
  non-sync-eligible while secret-bearing.
- Verified: NO -- deferred to Phase 2: the runner itself (ordering, idempotency, resume, `dryRun`, per-step
  `verify`), the blocking backup/restore precondition, the abort-on-zero-`keyCount` rule, and the negative
  fixtures the current suite lacks -- a decreasing version pair, a `keyCount: 0` snapshot, and a backup
  record with `restoredAtLeastOnce` left false after a destructive step.
