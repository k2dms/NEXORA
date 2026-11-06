# 35 — Schema versioning and migration: how the envelope changes shape without changing the user's data

> Purpose: ADR-0014 decides that the envelope goes from version 2 to 3 through a backup-gated, resumable
> runner, and ADR-0015 decides that the extension's version numbers keep rising so that an inherited wipe
> cannot fire. Neither decision is a procedure. This document is the procedure: the two version stamps and who
> may write each; the step list and the order inside it; what a backup has to prove before a write is allowed;
> how a run resumes when the worker dies halfway; what happens when an *older* build meets a *newer*
> envelope; and the eleven things a migration MUST NOT do. It exists because the inherited engine does not
> refuse an unknown version — it deletes the data that carried it, and every line of that chain is in this
> tree already (`docs/audit/05-storage-sync-and-migration.md` §7.1).

Status: Accepted. Normative data: `packages/schema/migrations.json` (the ledger: stamps, steps, journal rules,
retention, prohibitions), `packages/schema/envelope-backup.schema.json#/required`,
`packages/schema/envelope-keys.schema.json#/properties/backupKeyPattern`,
`packages/schema/envelope-keys.schema.json#/properties/schemaVersionKey`,
`packages/schema/protocol/commands/profile.import.json#/args/properties/backupBeforeWrite`,
`packages/schema/import-staging.schema.json#/properties/expiresAt`.
Evidence base: `docs/audit/05-storage-sync-and-migration.md` (the eight stores, the ten destructive mechanisms,
the erase-on-unknown chain), `docs/audit/04-data-model-and-options-envelope.md`,
`docs/audit/11-security-findings.md` (SEC-1, SEC-5), `docs/audit/12-what-must-not-change.md`,
`docs/adr/0013-omega-conditions-compiles-into-xray-routing.md`, ADR-0014, ADR-0015, ADR-0012.

---

## 1. What this document owns

Owns:

1. **Ordering** — which step runs before which, and what each must prove before the next is allowed
   (§5, §6). This is the whole reason the document exists.
2. **The two version stamps** — the legacy bare `schemaVersion` and `-nexora.schemaVersion`, who may write
   either, and the pairing rule that binds them (§4).
3. **The backup contract as a gate** — not its shape, which is `packages/schema/envelope-backup.schema.json`,
   but the fact that nothing destructive happens until it has been *read back* (§7), and how long snapshots are
   kept (§7).
4. **Resumption** — what a run knows after a crash, and where that knowledge lives (§8).
5. **The version a build does not understand** — the read-only rule, and the downgrade cases around it (§10).
6. **What migration must not do** — the eleven prohibitions, each with the code that earned it (§14).

Refuses:

- **The secret vocabulary.** A reference's shape, masking, fingerprints and the ingest door are
  `docs/architecture/32-secrets-keystore-masking.md`. This document takes the ordering that document states
  (§11) and owns the sequence around it.
- **Which byte rests where.** The planes, the seven classes and the store-by-store catalogue are
  `docs/architecture/34-data-split.md`. This document assumes its rule that intent never moves to the
  Connector as its only copy, and adds the migration-shaped consequence: a version stamp is intent.
- **Whether a control works.** Every check named here resolves to a row of
  `docs/verification/live-matrix.md`; `LIVE-23` and `LIVE-24` are this document's rows.
- **The words the user sees.** `docs/architecture/37-ui-architecture.md` §4; `dryRun` is specified here as a mode,
  not as a screen.
- **Editing the legacy engine.** ADR-0002 and `docs/audit/12-what-must-not-change.md` keep `Options.upgrade`
  and `upgradeMigrateFn` as they are. This document's job is to make them unreachable from NEXORA's paths, not
  to repair them — which is why the erase chain in §3 is cited rather than patched.

## 2. The questions this document was cited to answer

Every row was a forward reference before this file existed. Each now has a section, and the harness knows about
most of them.

| Cited by | The question | Answered in |
|---|---|---|
| `docs/architecture/22-component-contracts.md:26` | MigrationRunner's specification file | §5, §6, §8 |
| `docs/architecture/22-component-contracts.md:338` | "the keystore must not be reachable from that path … (`35` §forbidden)" | §14 |
| `docs/architecture/24-options-envelope-compat.md:162` | "Migration itself is `35`" — the two-keys decision | §4 |
| `docs/architecture/29-subscriptions-and-servers.md:390` | "whether a subscription may ever be created by the Connector … or only by the user" | §6, step `subscription-sources` |
| `docs/architecture/30-failure-policy.md:286` | the fail-open confirmation is stripped by migration and import | §13 |
| `docs/architecture/32-secrets-keystore-masking.md:39`, `:446` | "the ordering owned by `35`" for extracting a credential | §11, and §6's `secret-census` |
| `docs/architecture/34-data-split.md:44` | "the ordering of any change to the envelope's shape, and the wipe trap" | §3, §5 |
| `docs/architecture/34-data-split.md:215` | "who may change either [stamp], in what order, and with what backup" | §4, §7 |
| `docs/architecture/34-data-split.md:274` | sync reset clearing the remote, "as a migration hazard" | §12 |
| `packages/schema/envelope-backup.schema.json#/description` | "the migration runner MUST write before its first destructive step" | §7 |
| `packages/schema/failure-policy.schema.json#/properties/version` | a document version "migrated independently of `-nexora.schemaVersion`" | §4 |
| `packages/schema/protocol/commands/profile.import.json#/args/properties/backupBeforeWrite` | "a destructive import mode must be preceded by a verified snapshot on both sides" | §11 |
| `packages/schema/protocol/data-ownership.json` rows `legacy-state`, `sync-remote-mirror` | "recorded, not fixed" and "the destructive-reset path" | §3, §12 |
| `docs/adr/0014-schema-version-3-and-backup-before-migrate.md` | the whole decision, in its rules 1–9 | §4–§8, §10 |
| `docs/adr/0015-continue-zeroomega-version-line.md:50` | `upgradeMigrateFn` is cited, not blessed | §3, §14 |

## 3. The trap, measured

The inherited engine has one migration mechanism and it is a branch inside the load path:
`omega-target/src/options.coffee:327-353`. It maps `1 → 2` (`omega-target/src/options.coffee:344`) and resolves
only when the version is `2` (`omega-target/src/options.coffee:349`); every other value rejects
(`omega-target/src/options.coffee:353`). What makes that fatal is not the rejection, it is the caller. The
sequence, each step cited from the shipped source:

| # | Step | Where |
|---|---|---|
| 1 | Every load runs `upgrade` | `omega-target/src/options.coffee:175-178` |
| 2 | A rejection lands in the load's catch | `omega-target/src/options.coffee:188-189` |
| 3 | The catch only re-raises when retries are exhausted; `retry` starts at 3 (`omega-target/src/options.coffee:128-129`) | `omega-target/src/options.coffee:189` |
| 4 | `Invalid schemaVersion` is not a `NoOptionsError`, so it takes the "serious error" branch: disable syncing, return null fallback | `omega-target/src/options.coffee:244-246` |
| 5 | The null fallback becomes **the shipped defaults**, and storage is removed and written over | `omega-target/src/options.coffee:250-256` |
| 6 | The engine reloads — now happily, on an empty configuration | `omega-target/src/options.coffee:258` |

So an unknown version does not mean "refuse to start". It means "the user's profiles are gone and the
extension works fine". There is no branch that preserves an unreadable envelope, and the audit measured this
chain end to end (`docs/audit/05-storage-sync-and-migration.md` §7.1). Three consequences follow, and they are
the spine of this document:

1. **The bare stamp is write-frozen.** NEXORA does not raise it while `Options.upgrade` is in the load path,
   which ADR-0002 says it is for as long as the legacy engine serves traffic (§4).
2. **A NEXORA build must never have a reason to write an unreadable value into a store the old engine reads.**
   Hence two stamps, one additive version, and the read-only rule for a future version (§10).
3. **"Fix the legacy behaviour first" is not available here.** Editing the gate to accept 3 would make every
   *older* build in the field erase data, because the older builds are the ones that still have the old gate.
   The fix is a version bump of the engine, which is a cut-over, which is ADR-0013's territory. Meanwhile the
   ledger's head is a NEXORA-only number.

The second trap is the one ADR-0015 exists for. `upgradeMigrateFn`
(`omega-target-chromium-extension/src/coffee/background.coffee:32-47`, registered at `:60-64`) clears
`chrome.storage.sync`, `chrome.storage.local` and the IndexedDB default store (`omega-target-chromium-extension/src/coffee/background.coffee:44-46`)
whenever the installed version compares greater than the previous one **and** `3.3.0` compares greater than the
installed version. Continuing the version line is what makes the second condition permanently false, and
ADR-0015 rule 5 (`docs/adr/0015-continue-zeroomega-version-line.md:50`) forbids editing the guard, because the
guard is load-bearing for installs that are still old. The consequence for this document is narrow but hard:

> **No migration path may be reachable from a version comparison.** The runner reads envelope state, never
> `chrome.runtime.getManifest().version`, and never `details.previousVersion`. That is what makes the
> keystore — and the envelope — unreachable from the wipe, which is invariant 3 of
> `docs/architecture/22-component-contracts.md` §9 stated as a procedure.

The dead mechanisms are worth naming, because they are the temptation. `omega-target-chromium-extension/src/module/upgrade.coffee`
is 181 lines of Switchy! v1 migration nothing loads (`:1` is a `require` of `omega-target`, and no module index
exports it); the update handler's own migration `switch` at
`omega-target-chromium-extension/src/coffee/background.coffee:49-58` is a `switch` on exact version strings with two
`# TODO check` branches
inside it. Both are evidence that "migrate on the update event, keyed by version string" was the inherited
strategy, and that it decayed into comments.

### 3.1 The destructive sites register

Nine paths in the shipped engine destroy user data. `LIVE-23` and `LIVE-24` are the rows that say a NEXORA run
cannot reach any of them.

| # | Path | Where | What it destroys | Reachable from a run? |
|---|---|---|---|---|
| 1 | `initWithOptions` — remove, then set | `omega-target/src/options.coffee:113-121`, removal at `:117` | the whole envelope, if the replacement is truncated | No: import is `profile.import`, gated by §11 |
| 2 | the fallback wipe | `omega-target/src/options.coffee:250-256` | the whole envelope | No: §10 makes a future version read-only |
| 3 | `reset` | `omega-target/src/options.coffee:380-395`, removal at `:389` | the whole envelope, deliberately | Only as a user action, with §7's backup |
| 4 | sync enable under conflict | `omega-target/src/options.coffee:1190-1199` | local storage, replaced by the remote | No: §12 forbids a run in that state |
| 5 | sync reset | `omega-target/src/options.coffee:1237-1256`, remote removal at `:1253` | the **remote** copy | No: §14 prohibition 5 |
| 6 | the update wipe | `omega-target-chromium-extension/src/coffee/background.coffee:44-46` | sync, local and IndexedDB state | No: unreachable by construction (§3) |
| 7 | `resetAllOptions` | `omega-target-chromium-extension/src/coffee/background.coffee:450-457` | sync and local, no backup gate | No: not a migration entry point (§14) |
| 8 | the debug reset | `omega-target-chromium-extension/src/coffee/omega_debug.coffee:36-56` | localStorage, the log store, the sync store, the default store, then reload | No, and recorded as a hazard (§14) |
| 9 | online restore | `omega-web/src/omega/controllers/io.coffee:104-118` | the whole envelope, from an unauthenticated URL | No: import replaces it (§11) |

## 4. Two stamps, one run

| Stamp | Key | Owner | Value | Who may write it |
|---|---|---|---|---|
| legacy | `schemaVersion` | the inherited engine | `2`, shipped at `omega-target/src/default_options.coffee:2` | nobody, from a NEXORA path |
| NEXORA | `-nexora.schemaVersion` | the runner | `3`, the ledger head | the runner, in step `stamp` |

The pairing rule, stated as the ledger states it: a run **reads** both, **writes** only the NEXORA one, and
after every step proves the bare stamp still reads `2`. A step that cannot prove that stops the run before it
writes anything. The assertion in `packages/schema/scripts/verify.mjs` is
*"no step writes a key the legacy engine owns, including the bare stamp it must preserve"*, and it is backed by
a second assertion that reads the legacy source itself — *"the erase-on-unknown chain the ledger is written
against is still the chain in the engine"* — because the pairing rule is only right while the shipped default
is 2 and the gate still rejects 3. If someone lifts that gate, this file's premise changes and the run must stop
being green rather than quietly becoming wrong.

Two stamps is not tidiness. It is the only way ADR-0012's one-scalar-per-entity rule, ADR-0013's "the legacy
engine keeps serving traffic", and a forward-only migration can all be true at once. A single stamp would have
to be either legible to the old engine (so stuck at 2, and NEXORA could not version anything) or legible to
NEXORA (so the old engine erased the user). `docs/architecture/24-options-envelope-compat.md` §7 owns the
compatibility half of the same fact; this document owns the ordering.

**Four other version spaces exist and a run must not touch any of them.** `packages/schema/migrations.json`
lists them and the harness reads each one out of its own schema: the settings document
(`packages/schema/connector-settings.schema.json#/properties/schemaVersion`, 1), the failure policy
(`packages/schema/failure-policy.schema.json#/properties/version`, 1), the pins document
(`packages/schema/pins.schema.json#/properties/pinsVersion`, deliberately unbounded), and the Native Messaging
protocol (`packages/schema/protocol/compatibility-rules.json#/protocolVersion`, negotiated at
`connector.hello`, never inferred from storage). The rule that makes these separate and not a fifth column of
confusion: **a document version is bumped by the code that reads the document; an envelope version is bumped
only by a step that rewrites storage.** A settings default change is a defaulting question and runs no step. A
pin change replaces a document from the network and runs no step. Conflating them would mean every settings
tweak demanded a backup, a verifier and a restore path — and a user watching a migration screen for a reason
that has none.

## 5. The ledger is data

`packages/schema/migrations.json` holds the stamps (§4), the run contract, the journal rules, the retention
number, the step list, the declared futures, and the prohibitions. It is data for one reason: two of its claims
are about files that can change underneath it — the legacy source and the key registry — and prose ages
silently where data cannot. The assertions, by their labels, each worth the check it runs:

- *"the ledger is one strictly forward chain over contiguous step numbers"* — no step is reachable twice, none
  goes backwards, none overshoots the head.
- *"the ledger spans exactly the version the envelope registry names, starting from the legacy stamp"* — the
  document, the registry and the ledger agree on `2 → 3`.
- *"every step is backup-gated, destructive or not, because rollback needs a before-image"* — ADR-0014 rule 3's
  asymmetry (a step that only adds keys is still backed up) is stated once, in data.
- *"version three adds keys and prunes none (ADR-0014 rule 7)"* — version 3 cannot be a cut-over wearing a
  version number.
- *"no step writes an envelope key the registry does not declare"* — a step inventing `-nexora.whatever` fails
  here instead of failing in the field as a key that sync, export and the strip set all ignore.
- *"every step declares an up, a verifier, an idempotence proof and a way back"* — the contract from ADR-0014
  rule 2, per step, not per runner.
- *"the ledger distinguishes what a step reads from what it writes"* — the read-only steps (`secret-census`)
  are honest about reading legacy data it will not modify.
- *"resume needs no undeclared key: the backup ledger is the journal and replay starts at step one"* — §8.
- *"the backup record already carries every field a resume has to read"* — §8's dependency on
  `packages/schema/envelope-backup.schema.json#/required` is checked, not assumed.
- *"a future envelope is held read-only, never migrated downward, and names the error that means it"* — §10.
- *"the destructive versions are declared as futures that need their own ADR, not as steps"* — §6's boundary.
- *"the other version spaces the runner must never bump are stated at their real values"* — §4's four spaces.
- *"pruning is limited to the backup namespace and never to zero snapshots"* — §7's retention number.
- *"every prohibition in the ledger cites the code or the contract that earns it"* — §14.
- *"the migration document names every step the ledger declares, so the two cannot disagree quietly"* — this
  file and that file are read against each other.

## 6. Version 3, step by step

Nine steps, all additive, all idempotent, all reversible by deleting what they wrote. The ledger is normative;
this table is the same information read by a person.

| # | Step | Writes | Reads (legacy) | What `verify` proves | How back |
|---|---|---|---|---|---|
| 1 | `stamp` | `-nexora.schemaVersion` | `schemaVersion` | the new stamp is 3 **and** the bare stamp is still 2 | delete the key |
| 2 | `settings` | `-nexora.settings` | `-revertProxyChanges`, `-downloadInterval` | validates, and its own `schemaVersion` is the value §4 declares | delete the key |
| 3 | `failure-policy` | `-nexora.failurePolicy` | — | validates, starts fail-closed, and no fail-open exception exists without a confirmation record (§13) | delete the key |
| 4 | `pins` | `-nexora.pins` | — | validates, keeps `verifySignature` true and `silentSelfUpdate` false | delete the key |
| 5 | `subscription-sources` | `-nexora.subscription.<id>` | `+<profile>`, `__ruleListOf_<profile>` | one source per source-bearing legacy profile, and the legacy profile plus its attachment are byte-identical | delete the created keys |
| 6 | `profiles` | `-nexora.profile.<id>` | `+<profile>` | every created profile resolves to exactly one legacy profile that still exists | delete the created keys |
| 7 | `routing` | `-nexora.routing.<id>` | `+<profile>` | validates and references only entities that exist after step 6 | delete the key |
| 8 | `secret-census` | — | `+<profile>` | the per-field counts are in the run report; nothing was written anywhere | nothing to reverse |
| 9 | `finalize` | — | `-nexora.schemaVersion` | all nine verifiers pass on a fresh read and the newest snapshot has `restoredAtLeastOnce` true | not reversible; writes nothing |

Three things this list does not do, each deliberate:

- **It creates no credential reference.** Step 8 counts values and leaves them in place. Minting a reference
  claims a keystore entry exists; writing the entry and rewriting the field is a *different* version (§11).
  This is also the answer `docs/architecture/29-subscriptions-and-servers.md` §13 asked for: the **runner**
  creates a SubscriptionSource from a legacy `sourceUrl`, the **user** creates one that carries a credential,
  and the **Connector** creates none — it has no reason to write intent, and ADR-0010 would be violated the
  moment it did.
- **It does not select anything.** The routing policy at step 7 is inert until chosen, the failure policy at
  step 3 defaults to fail-closed, and the pin at step 4 does not download. A version 3 install changes no
  traffic, which is what makes it safe to run without asking.
- **It does not prune.** Step 9 is the only step that could reach retention, and it writes nothing; pruning is
  a post-run action under §7's number, not a step, because a step that deletes belongs to a different version.

`docs/architecture/33-binary-integrity.md` §9's rule that a pin document is *replaced*, never migrated, is the
same boundary seen from the other side: the Connector's install state and the envelope's version are two
clocks, and only one of them is a runner.

## 7. The backup is a gate, not a file

ADR-0014 rule 3 makes it blocking; this section makes it concrete.

1. **Two media, always.** A snapshot goes to `chrome.storage.local` under
   `packages/schema/envelope-keys.schema.json#/properties/backupKeyPattern` **and** to the Connector's
   per-user directory (`packages/schema/envelope-backup.schema.json#/properties/connectorMirrorPath`). A
   snapshot stored only in the area being migrated proves nothing about quota, and a file the user must save
   by hand will not be saved before a forced upgrade.
2. **Read it back.** The runner re-reads the snapshot, recomputes
   `packages/schema/envelope-backup.schema.json#/properties/contentSha256` over the canonically serialised
   payload, compares, and only then sets `restoredAtLeastOnce`
   (`packages/schema/envelope-backup.schema.json#/properties/restoredAtLeastOnce`) in the record. A write
   promise resolving is evidence the call was accepted, not that the bytes survived: the state store rewrites
   its whole map as one blob and can bring back keys a step deleted
   (`omega-target/src/browser_storage.coffee:49-55`).
3. **Zero keys aborts.** `keyCount` exists in the schema so that "a wipe recorded as a success" is a detectable
   shape, and the runner refuses to write a step against a snapshot of nothing.
4. **Quota failure aborts.** The run stops before any destructive step. It MUST NOT continue "with the local
   copy only", and MUST NOT delete an older snapshot to make room — that trade converts a recovery plan into a
   single point of failure exactly when the storage is under pressure.
5. **No Connector, no destructive step.** With no mirror path the run may execute non-destructive steps only.
   Version 3 is entirely non-destructive, so NEXORA Lite still migrates (§8's dependency on the mirror is
   therefore not a dependency on the Connector being present, and `LIVE-22`'s Lite half stays true).
6. **Retention is a number, and it is 1.** `packages/schema/migrations.json#/retention`: after a completed run,
   one completed snapshot is kept, pruning may only touch the backup namespace, and the newest snapshot, any
   unfinished run's snapshot, and any key outside the backup namespace are never pruned. Storage roughly
   doubles during a run (ADR-0014's consequence); the rule that bounds it is here, not in an implementation note.
7. **A snapshot is a leak surface that only exists because backups do.** It copies everything, including the
   plaintext values a pre-keystore envelope holds. So it is owner-only, it is excluded from every export, sync
   payload, log and diagnostics bundle — `docs/adr/0014-schema-version-3-and-backup-before-migrate.md:81`
   states it as a consequence and `packages/schema/migrations.json#/forbidden` states it as a prohibition — and
   its payload excludes older snapshots (the rule this document added to
   `packages/schema/envelope-backup.schema.json#/properties/payload`: a snapshot that contains the older
   snapshots contains itself recursively, grows with the number of runs, and would resurrect its predecessors
   out of order if restored).
8. **Deleting the snapshot is a separate, later act** — after the user has run on the migrated envelope and
   nothing has needed restoring. A migration that cleans up after itself on success is a migration that removes
   its own evidence at the first optimistic signal.

## 8. Resuming, and why there is no cursor key

A run is interrupted by things the extension cannot prevent: the MV3 worker going away, the browser closing,
a crash inside a storage write. ADR-0014 rule 2 requires resumability and rule 8 requires that a resume cursor
"live in a namespace that is first declared in the key registry", while deliberately not inventing one. This
document decides the simpler thing:

> **There is no cursor key.** The envelope plus its backup records already state the truth, the steps are
> idempotent, and a cursor would be a second, authoritative copy of "what state is this envelope in" — the
> exact kind of fact that ends up disagreeing with the data it describes.

The recovery procedure, in order:

1. Read the envelope. Take `-nexora.schemaVersion` and the bare `schemaVersion`.
2. Enumerate keys matching the backup pattern. The suffix is a UTC ISO-8601 timestamp
   (`packages/schema/envelope-keys.schema.json#/properties/backupKeyPattern`), so lexical order **is**
   chronological order and "the newest snapshot" needs no index, no cursor and no second store.
3. Take the newest record whose `toSchemaVersion` is at least the envelope's claimed version, and whose
   `contentSha256` still matches its payload. A record that fails this is not a recovery plan: it is reported,
   kept, and not restored.
4. Run every step's `verify` in order. A verifier that reports "already applied" costs one read.
5. If a verifier fails after the run claims to have applied it, the envelope is half-applied: **restore step 3's
   snapshot**, then replay from step 1. The restore is not a repair of the migration; it is the reason the
   snapshot was made blocking.
6. If the restore itself fails, enter the visible "restore required" state and stop. The extension does not
   continue on a partial envelope, for the same reason ADR-0009 refuses to continue on a dead Core: a proxy that
   half-works routes traffic in a way the user did not ask for.

The field list a resume reads — `backupId`, `createdAt`, `fromSchemaVersion`, `toSchemaVersion`, `keyCount`,
`contentSha256`, `restoredAtLeastOnce` — is checked against
`packages/schema/envelope-backup.schema.json#/required` by *"the backup record already carries every field a
resume has to read"*. That assertion is the reason this design is safe to adopt now rather than at
implementation time: if the schema ever drops one of those fields into optionality, the journal stops being a
journal and the harness says so.

This is an amendment to ADR-0014 rule 8 (§17) rather than a silence about it.

## 9. How a run fails

| # | Failure | What the user has afterwards | What the runner does | Row |
|---|---|---|---|---|
| 1 | the snapshot write is refused for quota | the pre-run envelope, untouched | abort before any write; no "local copy only" path (§7 rule 4) | `LIVE-23` |
| 2 | the snapshot writes but the read-back differs | the pre-run envelope, untouched | abort; report the hash mismatch as a storage fault, not a migration failure | `LIVE-23` |
| 3 | the worker dies between backup and first step | a completed snapshot and an unmigrated envelope | §8's replay: verifiers all report "not applied", steps run | `LIVE-23` |
| 4 | the worker dies in the middle of a step | an envelope with some of that step's keys written | on resume, the step's `verify` fails → restore → replay from step 1 | `LIVE-23` |
| 5 | restore fails | nothing that works | visible "restore required"; the extension refuses to run on a partial envelope; export of the *remaining* storage is offered so the user can still recover by hand | `LIVE-23` |
| 6 | the options page is opened twice and both try to run | one run | the second tab observes a run in progress and waits; there is no lock file and no cross-process claim because the options page owns the envelope | `LIVE-23` |
| 7 | the Connector vanishes mid-run | intent intact, no mirror | non-destructive steps may continue; a destructive step is refused (§7 rule 5) — version 3 has none, so a Lite run completes | `LIVE-22`, `LIVE-23` |
| 8 | a sync push lands during the run | a half-migrated envelope plus a remote that moved | forbidden by precondition: `run.syncMustBeSettled`, because the conflict path wipes local storage itself (`omega-target/src/options.coffee:1190-1199`) | §12 |
| 9 | an older build opens the migrated envelope | read-only presentation | §10: `SchemaMismatch`, no write, no downward migration | `LIVE-24` |
| 10 | a pre-extraction snapshot is restored after version 4 | credentials back as values, references pointing at nothing | refused: the ledger's future version says extraction is its own decision, and restoring across it is a supported, warned, explicit action, not a recovery default | §11 |
| 11 | the legacy wipe fires on an old install while NEXORA data exists | everything, including the new keys | unreachable from a run; recorded as the reason ADR-0015's numbering is a data-persistence decision | §3 |

## 10. A build behind its own data

The read-only rule is the other half of §3, and it is the reason this document is worth having as data:

> An envelope whose `-nexora.schemaVersion` is above `packages/schema/migrations.json#/versionStamps/nexora/head`
> is **a build that is behind**, not data that is broken. The extension presents it, reports
> `packages/schema/protocol/error-taxonomy.json`'s `SchemaMismatch` — the code defined as a payload ahead of the
> receiver, borrowed deliberately so that storage and transit use one word for one condition — keeps every key
> it did not write, runs no migration, repairs nothing, and pushes nothing back to a sync backend.

The last clause is the one that stops an incident: a read-only client that writes nothing cannot "helpfully"
normalise the remote copy for every other machine. An old build that repairs a new envelope converts one
developer's version bump into every other machine's data loss, and it does so silently, because from its point
of view the data was wrong.

Three related cases, each with the same shape:

- **A key the build does not recognise** (added by a later version, inside a registered namespace): present it
  if it renders, ignore it if it does not, never delete it. Deletion of an unknown key is the one action that
  cannot be undone by upgrading.
- **The bare stamp above 2**: this build is so old it predates NEXORA versioning. §3's chain fires in the
  *legacy* engine, not in NEXORA code, which is why the mitigation is ADR-0015's numbering and not a code
  change in the gate.
- **A Connector newer than the extension** (or the reverse): not a migration question at all —
  `docs/architecture/25-native-messaging-protocol.md` §5, `packages/schema/protocol/compatibility-rules.json`,
  and `LIVE-01`. Keeping these two clocks apart is the point of §4's four spaces.

`LIVE-24` runs both halves: a v4 envelope on a v3 build, diffed before and after; and one reproduction of the
legacy erase chain as the control that shows what the row prevents.

## 11. Import, extraction, and the one-frame rule

Import is a migration by another name, so it inherits the same gate:

- `packages/schema/protocol/commands/profile.import.json#/args/properties/backupBeforeWrite` is `const true`.
  A destructive mode (`#/args/properties/mode` = `replace`) cannot be expressed without the field, and the
  field cannot be expressed as anything but true. Ajv refuses the request, so this is not a rule a call site
  can forget — the same technique `docs/architecture/32-secrets-keystore-masking.md` §6 uses for the ingest door.
- The snapshot it demands is §7's snapshot, on both media, verified. Reusing the migration's backup path rather
  than defining an import-shaped one is the whole point: two backup mechanisms means one of them is wrong.
- The legacy online restore (`omega-web/src/omega/controllers/io.coffee:104-118`) is not carried forward. It
  fetched a URL kept in the state store and called `reset` with the raw body — an unauthenticated whole-store
  replace, and one of the two paths in this tree that can destroy data with no credential and no confirmation.
- **Extraction is a version, not a step.** `docs/architecture/32-secrets-keystore-masking.md` §15 states the
  sequence: read the legacy document, hand the values and the intent to `profile.import` **in one frame**, and
  only then write the reference-shaped envelope. Splitting those is how a user gets a profile pointing at a
  credential that was never written. That sequence belongs to
  `packages/schema/migrations.json#/futureVersions` — version 4, `adrRequired: true`, not executable — because
  it rewrites a legacy field, so it is not additive, so ADR-0014 rule 7 says it needs its own decision. Version
  3's contribution is step `secret-census`: the runner can say how many values version 4 will move before it
  moves any.
- The one-way property that makes version 4 different in kind: extracting a credential removes it from the
  envelope, so restoring a *pre-extraction* snapshot onto a *post-extraction* install is the one downgrade that
  loses data. It is recorded as failure mode 10 and as an open item (§17), not solved by a rule.

## 12. Sync: the hazards a run must not step into

Sync is a second writer of the same envelope, on a timer, from a network. Four rules, in order of how much
damage each prevents:

1. **A run does not start while the sync state is unsettled** (`run.syncMustBeSettled`): under `conflict` the
   engine removes local storage and rewrites it from the remote
   (`omega-target/src/options.coffee:1190-1199`), and a run racing that is a restore test with the user's data
   as the fixture.
2. **A run does not clear or re-push the remote.** `resetOptionsSync`
   (`omega-target/src/options.coffee:1237-1256`) reaches the remote removal at `:1253`, which clears the local
   mirror and pushes an empty set (`omega-target-chromium-extension/src/module/sync_storage.coffee:174-178`).
   Destroying the other copy is not a local operation, and a remote wipe that arrives as a *side effect of a
   version bump* is a data-loss event nobody agreed to. This is the promise
   `docs/architecture/34-data-split.md` §8 and `packages/schema/protocol/data-ownership.json` (row
   `sync-remote-mirror`) asked this document to keep: the mechanism is recorded as a hazard, guarded by §14's
   prohibition 5 and by `LIVE-23`.
3. **Migration state is intent and syncs; migration *progress* does not.** The version stamp and every created
   entity are ordinary `-nexora.*` keys, sync-eligible per
   `packages/schema/protocol/data-ownership.json`; snapshots and import staging are not —
   `secretBearing: true` and `syncEligible: false` in
   `packages/schema/examples/envelope-keys.nexora.json` for both, which is how ADR-0014 rule 9 is enforced today.
   A machine that pulls a version 3 envelope mid-run resolves through §8, not through a cursor it never received.
4. **A sync restore is a migration boundary.** Restoring an older remote envelope over a newer local one is a
   downgrade, and the downgrade rule of §10 is the rule that applies, not by whichever component notices last. The strip set is what makes the
   restored envelope safe to hold (`docs/architecture/32-secrets-keystore-masking.md` §12); the version rule is
   what makes it safe to *write*.

## 13. What a run strips, and what it must not

A migration rewrites intent documents, so it also rewrites the evidence attached to them. The rule
`docs/architecture/30-failure-policy.md` §9 asked for, stated here:

- A **fail-open confirmation** is evidence that a named human agreed to a bypass **on this machine**. It is not
  intent about the network, so it does not survive a migration, an import, or a sync restore: each strips it,
  the exception stops being in force, and the UI re-asks. Carrying it across would let a version bump silently
  extend a security exception the user granted to a different configuration.
- A **health history**, an **applied-config hash**, and any `observed` byte are stripped for the opposite
  reason: they describe a state the run invalidated, and `docs/architecture/34-data-split.md` §10 rule 5 forbids
  a second copy of them anyway.
- A **staging buffer** is not stripped, it is expired: `packages/schema/import-staging.schema.json#/properties/expiresAt`,
  because a scratch buffer without a deadline becomes a credential store (ADR-0014 rule 9).
- What a run must **not** strip: the legacy keys it did not write (§6's additive property), the backup records
  (they are the journal), the `traceId`-shaped correlation history the user may still need to explain a past
  incident (`docs/architecture/36-observability.md`), or anything whose loss would make the envelope claim a
  version it no longer satisfies.

## 14. Forbidden

Eleven prohibitions, each with the artefact that earned it. They are in
`packages/schema/migrations.json#/forbidden` so the harness can require evidence for each
(*"every prohibition in the ledger cites the code or the contract that earns it"*); this is the same list read
as argument.

| # | Prohibition | Earned by |
|---|---|---|
| 1 | No NEXORA path may compare extension versions and clear storage as a consequence | `omega-target-chromium-extension/src/coffee/background.coffee:44-46`, and ADR-0015's table of how it detonates on the *second* release |
| 2 | No step may remove the envelope and then write its replacement | `omega-target/src/options.coffee:113-121` |
| 3 | No step may write the bare stamp above 2 while the legacy gate is in the load path | `omega-target/src/options.coffee:349-353`, plus §3's chain |
| 4 | No migration may start under a sync conflict or a pending push | `omega-target/src/options.coffee:1190-1199` |
| 5 | No migration step may clear, reset or re-push the sync remote | `omega-target/src/options.coffee:1237-1256` |
| 6 | No snapshot payload may appear in an export, a sync payload, a log line or a diagnostics bundle | `packages/schema/envelope-backup.schema.json#/properties/payload`, `docs/adr/0014-schema-version-3-and-backup-before-migrate.md:81` |
| 7 | A step may not write an unregistered key, and a runner may not keep private bookkeeping in storage | `packages/schema/envelope-keys.schema.json#/properties/namespaces`, ADR-0012 rule 6 |
| 8 | No destructive import may run without a verified snapshot on both media | `omega-web/src/omega/controllers/io.coffee:104-118`, and `profile.import`'s `backupBeforeWrite` |
| 9 | A step's success may not be judged by the write promise resolving | `omega-target/src/browser_storage.coffee:49-55` — the mirror that rewrites the whole map |
| 10 | The runner may not treat the Connector as the only copy of any intent it migrated | `docs/adr/0010-data-split-extension-intent-connector-truth.md:66-69`, `docs/architecture/34-data-split.md` §6 |
| 11 | The debug reset is not a migration and may not be reached from one | `omega-target-chromium-extension/src/coffee/omega_debug.coffee:36-56` |

Prohibition 9 deserves its prominence: it is the one that survives the others. A step that trusts a resolved
promise is a step whose "verified" means "we hope". Every verifier in §6 re-reads.

## 15. Who owns each check

| Claim | Check | Layer | Verified |
|---|---|---|---|
| The ledger is one forward chain over the version the registry names | assertions *"the ledger is one strictly forward chain over contiguous step numbers"*, *"the ledger spans exactly the version the envelope registry names, starting from the legacy stamp"* | Schema | **YES** (green) |
| The legacy premise still holds (default 2, gate rejects, fallback wipes) | *"the erase-on-unknown chain the ledger is written against is still the chain in the engine"* | Schema, reading legacy source | **YES** — static; the *behaviour* is measured in `docs/audit/05-storage-sync-and-migration.md` §7.1, never executed |
| Version 3 is additive and backup-gated | *"version three adds keys and prunes none (ADR-0014 rule 7)"*, *"every step is backup-gated, destructive or not, because rollback needs a before-image"* | Schema | **YES** |
| No step writes an unregistered or legacy-owned key | *"no step writes an envelope key the registry does not declare"*, *"no step writes a key the legacy engine owns, including the bare stamp it must preserve"* | Schema | **YES** |
| Resume needs no cursor and the snapshot carries the fields it reads | *"resume needs no undeclared key: the backup ledger is the journal and replay starts at step one"*, *"the backup record already carries every field a resume has to read"* | Schema | **YES** |
| Pruning is bounded to the backup namespace, never to zero | *"pruning is limited to the backup namespace and never to zero snapshots"* | Schema | **YES** |
| The four other version spaces are stated at their real values | *"the other version spaces the runner must never bump are stated at their real values"* | Schema | **YES** |
| Doc and ledger name the same steps | *"the migration document names every step the ledger declares, so the two cannot disagree quietly"* | Schema | **YES** — this file is read by the harness |
| A run orders, verifies, resumes, restores and aborts as specified | a runner test against a storage double, killed at each of the nine boundaries | Extension runtime | **NO — Phase 2**, `LIVE-23` |
| A future envelope is held read-only and the legacy chain does not fire | a v4-on-v3 load with a before/after store diff, plus the legacy control | Extension runtime | **NO — Phase 2**, `LIVE-24` |
| The snapshot survives quota pressure and MV3 teardown | a real browser, a real worker, a real quota | Extension runtime | **NO — Phase 2**, `LIVE-23` |

## 16. Threat, control, check

| Threat | Position | Control | Check |
|---|---|---|---|
| A version bump erases a user's configuration | the shipped engine's own fallback path | the bare stamp never moves; the ledger's head is NEXORA-only | *"the ledger spans exactly the version…"*, `LIVE-24` |
| An older build "repairs" a newer envelope | any machine that syncs | read-only presentation, `SchemaMismatch`, no writes, no re-push | *"a future envelope is held read-only…"*, `LIVE-24` |
| A crash leaves a half-migrated envelope serving traffic | worker lifecycle | blocking verified snapshot, restore-then-replay, refuse to run on partial state | `LIVE-23`; §8's procedure |
| A migration silently deletes the other machine's copy | sync | no step touches the remote; a run waits for a settled sync state | prohibitions 4 and 5; `docs/architecture/34-data-split.md` §8 |
| A snapshot becomes the leak a safety mechanism created | anyone with the export or the sync backend | snapshots are `syncEligible: false`, excluded from every artefact, payload excludes older snapshots | *"the strip set covers every credential key…"* (doc 34's harness), §7 rule 7 |
| A step is marked done because the call resolved | storage write path | per-step verifier that re-reads | prohibition 9; §6's `verify` column |
| A run invents a private key nobody strips, exports or backs up | the registry | writes validated against registered namespaces | *"no step writes an envelope key the registry does not declare"* |
| A security exception rides through a migration unnoticed | the failure policy | confirmations stripped, exception re-asks | §13; `LIVE-11` |

## 17. Invariants, amendments, open items

**Invariants.**

1. Version 3 is additive: it writes keys it owns and prunes nothing. Removal is a version of its own with an
   ADR.
2. The bare legacy stamp stays at 2 while `Options.upgrade` is in the load path, and the harness reads that
   source to keep this claim honest.
3. Nothing destructive happens before a snapshot that has been read back and hash-compared, on both media.
4. A step is verified by re-reading, never by a resolved promise.
5. A run resumes by replaying idempotent steps against the envelope and the backup ledger; there is no cursor.
6. A version this build does not understand is held read-only, never migrated downward, never re-pushed.
7. No migration path is reachable from a comparison of extension versions.
8. Snapshot payloads never leave the machine as an artefact, and a snapshot never contains another snapshot.

**Amendments made by this document** (all green in `npm --prefix packages/schema run verify`, 900 checks):

1. `packages/schema/migrations.json` is added, and the verifier gained 29 checks over it (§5's labels).
2. ADR-0014 rule 8 is amended: no cursor namespace is declared or needed. The resume procedure is §8, and the
   fields it reads are checked against `packages/schema/envelope-backup.schema.json#/required` instead.
3. `packages/schema/envelope-backup.schema.json#/properties/payload` now excludes the backup namespace from a
   snapshot — found while writing §7, because the previous wording ("keys verbatim") made a snapshot recursive
   in the snapshots it was meant to protect.
4. `docs/verification/live-matrix.md` gained `LIVE-23` (an interrupted migration is recoverable) and `LIVE-24`
   (a future envelope is held, not repaired); the matrix now runs to `LIVE-24`.
5. Forward references closed: `docs/architecture/29-subscriptions-and-servers.md` §13 item 5,
   `docs/architecture/34-data-split.md` §6 rule 4 and §8's sync-reset row,
   `docs/architecture/22-component-contracts.md` §9 invariant 3's `§forbidden`, and
   `docs/architecture/32-secrets-keystore-masking.md` §15's handed-over ordering.

**Open items.**

1. Whether mirrored legacy entities get ids derived from the profile name or minted randomly: derived ids make
   replay free and make a rename a data event. Recorded in
   `packages/schema/migrations.json#/openDecisions`, to be decided in `docs/architecture/40-open-decisions.md`.
2. Version 4 (credential extraction) needs its own ADR, and the restore-across-it rule (§11) needs a decision
   about whether a pre-extraction snapshot may be restored at all without an explicit warning and a confirm
   token in the `keystore.purge` shape.
3. Whether the state store must be inside the backup's scope once a version touches it — it does not for
   version 3, and `gistToken` lives there today, so version 4 inherits the question
   (`docs/architecture/34-data-split.md` §6, `docs/audit/05-storage-sync-and-migration.md` §2).
4. The UI wording of `dryRun`'s "what will change" screen and of the "restore required" state —
   `docs/architecture/37-ui-architecture.md` §4 and §5.
5. Whether the secret census counts are user-visible or diagnostics-only — `docs/architecture/36-observability.md`.
6. A runner test that asserts the negative fixtures ADR-0020 rule 5 demands: a decreasing version pair, a
   `keyCount: 0` snapshot, and a record with `restoredAtLeastOnce` still false after a destructive step.

---

Trace: ADR-0002, ADR-0009, ADR-0010, ADR-0012, ADR-0013, ADR-0014, ADR-0015, ADR-0019, ADR-0020 · Contract:
`packages/schema/migrations.json`, `packages/schema/envelope-backup.schema.json`,
`packages/schema/envelope-keys.schema.json`, `packages/schema/protocol/commands/profile.import.json`,
`packages/schema/import-staging.schema.json`, `packages/schema/protocol/error-taxonomy.json`,
`packages/schema/protocol/data-ownership.json` · See also:
`docs/audit/04-data-model-and-options-envelope.md`, `docs/audit/05-storage-sync-and-migration.md`,
`docs/audit/12-what-must-not-change.md`, `docs/architecture/22-component-contracts.md`,
`docs/architecture/24-options-envelope-compat.md`, `docs/architecture/29-subscriptions-and-servers.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/32-secrets-keystore-masking.md`,
`docs/architecture/33-binary-integrity.md`, `docs/architecture/34-data-split.md`,
`docs/verification/live-matrix.md`.
