# 34 — Data split: which layer owns which byte

> Purpose: ADR-0010 decides *who owns what* in one sentence — intent in the extension, executable truth in the
> Connector, secrets in neither's transit. That sentence is not a storage plan. This document is the storage plan:
> the catalogue it demands, plane by plane and key by key; the seven classes of byte and the rule each one is
> under; what crosses the Native Messaging pipe in which direction and what happens when it does not fit; where
> quota actually bites; what "NEXORA Lite" promises and what it is forbidden to mean; and who owns each of the
> checks. It exists because an unstated ownership rule is not an absence — it is the shape of SEC-1, which this
> tree can measure.

Status: Accepted. Normative data: `packages/schema/protocol/data-ownership.json`,
`packages/schema/envelope-keys.schema.json#/properties/namespaces/items/properties/syncEligible`,
`packages/schema/envelope-keys.schema.json#/properties/syncExcludedValueKeys`,
`packages/schema/examples/envelope-keys.nexora.json`, `packages/schema/protocol/framing.json#/limits`,
`packages/schema/protocol/framing.json#/oversizedPayloads`,
`packages/schema/import-staging.schema.json#/properties/expiresAt`,
`packages/schema/subscription-source.schema.json#/properties/urlRef`,
`packages/schema/envelope-backup.schema.json#/properties/keyCount`.
Evidence base: `docs/audit/04-data-model-and-options-envelope.md` (what the data *is*, measured),
`docs/audit/05-storage-sync-and-migration.md` (where it rests, and the eight stores a migration has to touch),
`docs/audit/11-security-findings.md` (SEC-1, SEC-5, SEC-7 — what an unstated rule costs),
`docs/adr/0010-data-split-extension-intent-connector-truth.md`, ADR-0012, ADR-0014, ADR-0019.

---

## 1. What this document owns

Owned here:

1. **The catalogue** — every store, its plane, its class, what it may hold, how it dies, and which check owns it
   (`docs/adr/0010-data-split-extension-intent-connector-truth.md:66-69` asks for exactly this, "as data rather
   than remembered"). §5 makes it data; §6–§7 read it back as prose.
2. **The direction of every crossing** — what may move browser→Connector, Connector→browser, and what may never
   move at all (§3, §10).
3. **Size and quota truth** — where the limits actually bind, and the answer to the `unlimitedStorage` question
   `docs/architecture/31-security-model.md` §13 left to this document (§9).
4. **NEXORA Lite** — the precise, testable meaning of "the Connector is not there and the user's data still
   means what it meant" (§11).
5. **Identity and reference survival** — what survives a rename, a reinstall, a sync, or a restore on another
   machine (§12).

Not owned here, by design: the *secret vocabulary* — reference shape, masking, fingerprinting, the ingest door's
protocol — `docs/architecture/32-secrets-keystore-masking.md`; the *ordering* of any change to the envelope's
shape, and the wipe trap — `docs/architecture/35-schema-versioning-migration.md` §3–§5; the bytes of the Core binary
and its install — `docs/architecture/33-binary-integrity.md`; the log format and event catalogue —
`docs/architecture/36-observability.md`; the legacy `+`-key behaviour that must not change —
`docs/architecture/38-compatibility-matrix.md` §5; and the wording the user sees —
`docs/architecture/37-ui-architecture.md` §4.

## 2. The questions this document was cited to answer

Each of these was a forward reference in another document. Each now has a section.

| Cited by | The question | Answered in |
|---|---|---|
| `docs/adr/0010-data-split-extension-intent-connector-truth.md:112` | "the durable specification is `34-data-split.md`" — the connector side has never run, so where is it written down? | §7, and the catalogue rows carry it |
| `docs/adr/0010-data-split-extension-intent-connector-truth.md:123` | "`34-data-split.md` will name the owner of each check" | §14 |
| `docs/architecture/21-system-context.md:134` | "the ownership table is `34-data-split.md`" — §8 of that document is deliberately compressed | §6, §7 |
| `docs/architecture/22-component-contracts.md:352` | "removing the Connector leaves the profiles intact and degrades to NEXORA Lite" | §11 |
| `docs/architecture/26-command-surface.md:232` | "the Connector never learns which tab is active" — where is the browser-plane data kept out of reach? | §10 |
| `docs/architecture/30-failure-policy.md:330` | the grace deadline is "arithmetic on data the Connector already persists" — no second copy | §10, row 5 |
| `docs/architecture/31-security-model.md:367` | "`unlimitedStorage` … removal is a data-split consequence" | §9 |
| `docs/architecture/32-secrets-keystore-masking.md:38` | "which storage key holds which byte" | §5–§7 |
| `docs/architecture/24-options-envelope-compat.md:229` | the envelope's compatibility half points here for placement, not for shape | §6, §8 |

## 3. Three planes, two directions, one door

ADR-0010's two halves become three physical places once Core is counted, because Core holds a file:

| Plane | Process | What it owns | What it dies with |
|---|---|---|---|
| browser | service worker, options page, popup | user intent, and whatever copy of an observation the UI is showing right now | the browser profile — and a `chrome.storage.local` wipe does *not* take the IndexedDB mirrors with it (`docs/audit/05-storage-sync-and-migration.md` §3) |
| connector | `nexora-connector`, plus the files it writes under the user profile | executable truth: keystore values, compiled config and its hash, health history, the fetched-feed cache, the diagnostics ring, the binary and its receipts | its per-user directory, which outlives both the extension and a Core upgrade |
| core | the Xray child process | the config file it was handed, in plaintext, for as long as it may run | the process |

Between the two owned planes there are exactly two directions and one door.

- **Intent goes down.** Profiles, servers, routing, settings, failure policy, the chosen pin — as documents
  validated against their schemas, with `keystore://` references standing in for every credential
  (`packages/schema/server.schema.json#/properties/userIdRef`).
- **Observation comes up.** Masked config, hashes, state, exit records, probe results, events — each size-bounded,
  each masked at the sink (`packages/schema/protocol/commands/config.snapshot.json#/result/properties/maskedConfig`).
- **One door carries values upward-in-the-only-direction-that-exists**: `profile.import`'s `credentials` field
  (`packages/schema/protocol/commands/profile.import.json#/args/properties/credentials`) is the only field in the
  whole command surface that may receive a secret value, and it moves browser→Connector. That is
  `docs/architecture/32-secrets-keystore-masking.md` §6's decision, restated here as a placement fact, and it is
  asserted as *exactly one* by `packages/schema/scripts/verify.mjs`.

Everything else about a crossing is derived from those three sentences, and the catalogue states each crossing as
a `movements` row with a direction, a bound, and an overflow rule. Three of them are worth reading directly,
because each is where a naive design breaks:

| Movement | Direction | Bound | If it does not fit |
|---|---|---|---|
| `intent-down` | browser → connector | 64 MiB per frame (`packages/schema/protocol/framing.json#/limits/extensionToHostBytes`) | chunked envelope, reassembled and hash-checked before anything is replaced (`#/oversizedPayloads`) |
| `feed-fetch` | network → connector | `limits.maxBytes`, default 2 MiB, ceiling 8 MiB | the refresh fails and the last good set stays applied — `docs/architecture/29-subscriptions-and-servers.md` §9 |
| `sync-out` | browser → network | the remote's own quota | the legacy answer is to mark profiles `syncOptions: 'disabled'` (`omega-target/src/options_sync.coffee:157-170`); NEXORA's answer is durable status, not silent exclusion |

The `sync-out` row is the one that admits a hole, and §9 and §11 keep it visible rather than explaining it away.

## 4. Seven classes of byte, and the rule each is under

A plane says *where* a byte is. A class says *what may be done to it*. The distinction earns its keep in exactly
one place: the two words "delete" and "sync" mean opposite things for intent and for observation.

| Class | Rule | May travel | Examples |
|---|---|---|---|
| `intent` | only the extension writes it; the Connector MUST NOT create, reorder or invent it (ADR-0010 rule 6) | yes | `-nexora.server.*`, `-nexora.routing.*`, legacy `+` profiles |
| `staging` | transient, local-only, hard-expiring; the one browser-plane class allowed to hold a value | no | `-nexora.importStaging.*` |
| `snapshot` | a deliberate copy of everything, so it inherits what it copied; local-only | no | `-nexora.backup.*` |
| `secret` | exists only as a keystore blob; everywhere else it is a reference or a mask | no | the keystore |
| `observation` | the browser may show a copy and MUST treat it as a copy | no | health history, `applied-state`, UI display copies |
| `derived` | computed, re-derivable, deletable at any time — and MUST NOT be the only copy of anything the user made | no | `pacScript`, `compiled-config`, `availableProfiles` |
| `control` | program bytes and the records that authorise them; verified before executed, never exported | no | the installed binary, install receipts, the Connector's pin copy |

Three consequences follow from the table rather than from prose:

1. **`derived` is not `observation`.** A derived artifact is deletable because it can be recomputed from intent;
   an observation is deletable because it will be re-measured. The difference is what the user loses: a stale
   `pacScript` regenerates on the next write (`omega-target/src/options.coffee:462-463` regenerates it today),
   while a lost hour of probe history is simply gone. Collapsing them would let one GC policy serve both.
2. **`secret` and `snapshot` may never be the same row.** A pre-migration backup is a copy of everything
   (`packages/schema/envelope-backup.schema.json#/properties/keyCount` must be non-zero), which is precisely why
   it is `syncEligible: false, secretBearing: true` alongside `ImportStaging`
   (`packages/schema/examples/envelope-keys.nexora.json:58-72`).
3. **Only `intent` may travel.** That is the assertion a class table makes possible: `syncEligible ⇒ class.maySync`
   is checked over every registered namespace, so a future namespace cannot be made syncable while classified as
   staging, snapshot, observation, secret or control.

## 5. The catalogue as data — and the drift it found on its first run

`packages/schema/protocol/data-ownership.json` is the catalogue: three planes, seven classes, twenty-seven store
rows and nine movements. Each store row names its plane, class, medium, location, writer, purge path, bound,
whether it survives the Connector and whether it survives a reinstall, which of its value keys can hold a
credential, what keeps that credential out of a transit, and the check that owns the claim. Each movement names a
direction, endpoints, a command where one exists, whether it carries values, a bound, and an overflow rule.

The value of data over a table is that data can be cross-checked against the three things it describes. The
harness now asserts, among others:

- *every registered envelope namespace has an ownership row*, and its mirror, *no ownership row invents a
  namespace the registry does not register* — so adding a `-nexora.*` family without saying where its bytes rest
  fails the run;
- *the catalogue agrees with the registry about sync and secrets* — the two booleans are compared per namespace,
  not inferred;
- *a class that may not travel never describes a sync-eligible namespace*;
- *credential material has exactly one home, and it is not in the browser*;
- *nothing the Connector holds leaves the machine* — every connector-plane row has `syncEligible: false`;
- *removing the Connector leaves every intent row readable* — which is §11's promise, expressed as one boolean;
- *nothing that carries a credential value moves toward the browser*, and *exactly one movement carries a
  credential value into the Connector, and it is the ingest door*;
- *no movement names a command the surface does not have* — checked against the command documents themselves, so
  the catalogue cannot invent a method ADR-0007 would refuse;
- *the strip set covers every credential key the catalogue says the synced envelope can hold* — the SEC-1
  mechanism and the SEC-1 field are now joined by an assertion rather than by a reviewer's memory
  (`omega-target/src/options.coffee:27` is the list that forgot `headers`);
- *every live row the ownership catalogue cites exists in the matrix* — which is why `LIVE-22` had to be written
  before the catalogue could name it, and why §14's table cannot rot silently.

**What the first run caught.** The `-nexora.pins` row was written as class `control` on the reasoning that a pin
authorises a binary. *a class that may not travel never describes a sync-eligible namespace* refused it: the
registry marks `-nexora.pins` `syncEligible: true` (`packages/schema/examples/envelope-keys.nexora.json:51-55`),
and the refusal was correct — what travels is a public statement about a public release, which is intent the user
may want on a second machine, and what authorises a binary is the Connector's own copy, written atomically with
the bytes it authorises. The envelope row is now `intent`; the connector-plane row `connector-pin-mirror` is the
`control` object. The distinction was not in anybody's head before the assertion asked for it.

**What the catalogue does not claim.** It is a description of the *target* placement. Where a byte sits today and
where it is supposed to sit differ for exactly two rows, and both say so in a `legacy` or `mitigation` field
rather than being quietly drawn as if already moved: the credential-bearing state key and the built-in-sync blob
(§8, §9).

## 6. Where each byte rests — the browser plane

The extension's stores, in the order a migration has to touch them (the count and the physical locations are
measured in `docs/audit/05-storage-sync-and-migration.md` §2, and its conclusion holds: "a migration that copies
only `chrome.storage.local` silently drops state, sync configuration and temp rules").

| Store | Class | Syncs | Can hold a value | Bound | How it dies |
|---|---|---|---|---|---|
| `-nexora.server.*` … `-nexora.pins` (7 namespaces) | `intent` | yes | no — references only | one storage item per entity | `profile.remove`, which also schedules `keystore-delete` |
| `-nexora.importStaging.*` | `staging` | no | **yes** | body ≤ 8 MiB, `chunkCount` ≤ 1024, `expiresAt` | expiry, or the acknowledge of `profile.import` |
| `-nexora.backup.*` | `snapshot` | no | inherits | one item per snapshot, count open | explicit user action |
| `+` profiles and the bare `schemaVersion` | `intent` | yes | **yes, today** — `headers`, `auth` | none in practice (§9) | the engine's delete paths |
| legacy `-` scalars (sixteen keys) | `intent` | yes | no | one item each | reset paths |
| state store (`omega.local.*`, mirrored as one IndexedDB blob) | `derived` | no | **yes, today** — `gistToken` | none; every write rewrites the whole map (`omega-target/src/browser_storage.coffee:49-55`) | the debug reset and the install wipe |
| `chrome.storage.session` → `tempProfileState` | `intent` | no | no | two keys | the browser clears it with the session |
| `zeroOmegaSync` in `chrome.storage.sync` | `intent` | yes | **yes, and this one is open** | the account-sync quota | the install wipe (`docs/audit/05-storage-sync-and-migration.md` §7 row 3) |
| `sync-store` IndexedDB mirror | `observation` | no | only what the strip set let through | none; no compaction exists | sync reset, which also clears the remote |
| `log-store` and the `log` state key | `observation` | no | **yes** — an unmasked line today (SEC-7) | unbounded | the debug reset |
| Svelte stores holding the last snapshot and status | `observation` | no | no | one response per component per mount | page unload |

Four rules over that table, all of them asserted:

1. **Nothing credential-bearing travels except where the catalogue names a mitigation.** The mitigation vocabulary
   is deliberately small: `syncExcludedValueKeys` (strip on exit), `keystore` (never in this plane), `localOnly`
   (never syncs, never in diagnostics), `sink masking` (destroyed where it leaves), `lifetime` (owner-only for a
   bounded window), or *none, open item*. Exactly one row is allowed the last, and its text must name
   `docs/architecture/40-open-decisions.md` — *exactly one credential is admitted to be reachable by a transit
   today, and it names the open decision* is that sentence as a check. Today that row is `sync-account-blob`, and
   the reason it cannot be fixed by the strip list is measured: the built-in-sync config is written whole into
   `chrome.storage.sync` by `omega-target/src/options_sync.coffee:240-242`, bypassing
   `transformValueForSync` entirely (SEC-5, `docs/audit/11-security-findings.md`).
2. **State is not the envelope, and NEXORA treats that as a warning rather than a free choice.** A NEXORA entity
   placed in the state store is invisible to backups and to the sync pipeline
   (`docs/audit/04-data-model-and-options-envelope.md` §1). So: the state store may hold `derived` and nothing
   else. The catalogue puts no NEXORA row in it.
3. **A removal and an absence are the same message on the browser's own wire** — `newValue` is mapped verbatim, so
   deletion arrives as `undefined` (`docs/audit/05-storage-sync-and-migration.md` §4). Any "was it deleted or was
   it never there?" question in the UI is therefore unanswerable from a change event alone; the answer is the
   revision, not the event.
4. **Two version stamps coexist and both stay in the browser plane**: the bare `schemaVersion`, which the legacy
   engine reads and will reject anything above `2` for (`omega-target/src/options.coffee:349-353`), and
   `-nexora.schemaVersion` (`packages/schema/envelope-keys.schema.json#/properties/schemaVersionKey`). Who may
   change either, in what order, and with what backup is `docs/architecture/35-schema-versioning-migration.md` §4, §7;
   what is *owned* here is that both are intent, both are local, and neither may ever move to the Connector as
   its only copy.

## 7. Where each byte rests — the Connector plane and Core

| Store | Class | Value present? | Bound | How it dies | Why it is here and not in §6 |
|---|---|---|---|---|---|
| keystore (`%LOCALAPPDATA%\NEXORA\keystore`, one blob per secret) | `secret` | yes — the only one by class | one blob per entity field | `keystore.purge` by scope, `profile.remove`'s side effect | ADR-0011 rule 1; the plane is the only place a value exists at rest |
| compiled config (`%LOCALAPPDATA%NEXORAconfigconfig.json`) | `derived` | **yes, declared** | one file, renamed over in place | `core.stop`, shutdown | Xray cannot be handed references; see the exception below. A hash-derived name would need the GC `ODEC-26` owns; the stable name is what makes doc 27 §8's replacement a rename and not a create |
| applied state (hash, selection, PID, `lastExit`) | `observation` | no | a handful of fields | rewritten on every apply | it *is* the truth about now, and it must survive a browser restart (LIVE-13) |
| health history ring, per server id | `observation` | no | ring-buffered | eviction; ids are reused across refreshes | it grows with traffic, not with the user's vocabulary |
| feed cache, per subscription, keyed by content hash | `observation` | yes — user UUIDs and often a token-bearing URL | `maxBytes`, default 2 MiB | next successful refresh; source delete | the body never enters a browser process (`docs/architecture/29-subscriptions-and-servers.md` §2) |
| diagnostics ring | `observation` | no | size-bounded at the source | eviction; nothing durable by design | an unbounded dump is a crash and an unmasked one is a leak |
| idempotency cache | `observation` | no (a cached response is masked like any response) | bounded window | eviction — and a probe is never cached | ADR-0008 needs it; the user must not have to |
| Core binary and install receipts | `control` | no | `retainedPreviousVersions` | rollback GC | ADR-0019 keeps it out of git and out of the archive |
| the Connector's pin document | `control` | no | one per retained version | replaced atomically with the binary it authorises | the copy the installer trusts cannot be the one in a synced envelope |
| the config file Core reads | `derived` | yes | one file | the compiled-config lifetime | same bytes, different owner of the fd |

The uncomfortable row is the second one, and it is a rule, not a footnote:
`packages/schema/envelope-keys.schema.json#/properties/secretReferences/properties/atRest` states
`compiledConfigContainsValues: true`. Xray reads a config file of its own format
(ADR-0003, ADR-0019), so the file handed to it is plaintext credential material on disk. This document's
contribution is to keep that statement bounded rather than general: owner-only ACL, a lifetime that begins at the
rename that installs it and ends at `core.stop`, never in an export, never in a diagnostics section, always
re-derivable — which is what makes it a cache and not a secret store. The cleanup half after an unclean kill is
shared with `docs/architecture/32-secrets-keystore-masking.md` §13 and `docs/architecture/33-binary-integrity.md`
§17 and is listed as open in §17 below.

The per-user layout of those rows is one directory tree: `%LOCALAPPDATA%\NEXORA\` on Windows, with `keystore\` for the blobs above (ADR-0011), `config\` holding the one compiled `config.json` the rename discipline replaces, `state\` holding the applied-state record (`state.json`: applied hash, selection, last PID, last exit, and doc 27 §4's port-reservation table `config.apply` writes after each rename), and `core\` holding the pinned binary, its pin mirror and the install receipt the supervisor's Launch resolves the active version from (doc 33 §7: receipts, not directory listings), and `host\` holding the one Native Messaging manifest and the startup allowlist `connector/cmd/nexora-installer` writes (`packages/schema/protocol/framing.json#/senderPolicy/identity/allowlistFile`) (`state.json`: applied hash, selection, last PID, last exit, and doc 27 §4's port-reservation table `config.apply` writes after each rename). `packages/schema/protocol/data-ownership.json`'s `compiled-config` row states the same path and the same stable name as data, and `connector/internal/apply`'s `FileName` and `connector/internal/supervisor`'s `StateFileName` are the two constants the code keeps to it. No document in `packages/schema` derives these paths at run time: the layout is this section's, and the two directories `config.apply`'s surface is constructed with are its `state` and `config` entries.

Nothing in this plane syncs. There is no NEXORA cloud, no account, no server component
(`docs/architecture/21-system-context.md` §9), so `syncEligible: false` on every connector row is not a policy
choice but a statement that no path exists. That is what *nothing the Connector holds leaves the machine* checks,
and it is the assertion to re-run if anyone ever proposes a backup-to-cloud feature: the answer moves data across
the one boundary this design has teeth about.

## 8. What NEXORA inherits, and what it refuses to inherit

Inherited unchanged, because ADR-0002 says do not rewrite what works and ADR-0013/ADR-0014 say the user's data is
the user's:

- the flat envelope with three namespaces and the `+` iteration rule
  (`omega-pac/src/profiles.coffee:111-118`) — every NEXORA entity stays under `-`
  (`docs/adr/0012-nexora-keys-under-dash-prefix.md`);
- derived fields stored *inside* profiles (`pacScript`, `ruleList`, `sha256`, `lastUpdate`, per
  `docs/audit/04-data-model-and-options-envelope.md` §3 rule 3) — a shape the new model does not copy for NEXORA
  entities but does not repair in legacy ones either;
- the engine's write paths, including the sync pull that writes straight through `Storage.apply` and bypasses
  `_setOptions` (`docs/audit/05-storage-sync-and-migration.md` §5) — consistency survives only because the
  `chrome.storage.onChanged` watcher re-absorbs the write;
- Gist and WebDAV as sync backends, with SEC-1 closed at the strip set and SEC-5's second half still open (§9).

Refused, and refused specifically because the audit measured the cost:

| Legacy behaviour | Where it lives | Why NEXORA does not copy it |
|---|---|---|
| Exclusion lists as the only secret guard, forgotten at the fifth field | `omega-target/src/options.coffee:27` | ADR-0010's Alternatives section calls exclusion-only protection out; here the strip set is cross-checked against the catalogue (§5) |
| One blob holding the entire state map, rewritten per key | `omega-target/src/browser_storage.coffee:49-55` | all-or-nothing per write and racy (`docs/audit/05-storage-sync-and-migration.md` §3); no NEXORA data may depend on it |
| `onInstalled` wipe when `3.3.0 > current` | `omega-target-chromium-extension/src/coffee/background.coffee:41-47`, `:60-64` | ADR-0015's version continuity exists to stop this firing; the keystore is unreachable from that path (`docs/architecture/22-component-contracts.md` §9 invariant 3) |
| Online restore that `reset`s the envelope from any URL's body | `omega-web/src/omega/controllers/io.coffee:104-118` | an unauthenticated whole-store replace; import is `profile.import` with validation before apply, and a pre-migration snapshot first (ADR-0014) |
| Sync reset clearing the remote | `omega-target/src/options.coffee:1237-1256` | destroying the other copy is not a local operation; owned by `docs/architecture/35-schema-versioning-migration.md` §12 as a migration hazard |
| `chrome.storage.session` used with no guard, under a manifest floor older than the API | `docs/audit/05-storage-sync-and-migration.md` §3 | the absence is VERIFIED; a crash on an intermediate browser version is not a design. The floor belongs to `docs/architecture/38-compatibility-matrix.md` §3 |
| An unbounded log store exported by a button | `docs/audit/11-security-findings.md` SEC-7 | sink masking with a bounded ring (§7) replaces name-keyed masking at the exit |

## 9. Quota, size, and the `unlimitedStorage` question

`docs/architecture/31-security-model.md` §13 granted the permission's removal to this document. The measured
answer is: **keep it this iteration, and evict the right class when it goes.**

Where size actually binds today, in the browser plane:

- the browser's own quota does not bind: `unlimitedStorage` is granted in both manifests
  (`omega-target-chromium-extension/overlay/manifest.json:44`,
  `omega-target-chromium-extension/overlay/manifest-firefox.json:43`), which is why no legacy path has a cap on a
  rule list;
- the *remote* quota does bind, and the legacy response to it is the defect, not the limit: offending profiles are
  marked `syncOptions: 'disabled'` (`omega-target/src/options_sync.coffee:157-170`), and because `_setOptions`
  handed over the same object reference, the in-memory copy gains the flag without a local persist
  (`docs/audit/04-data-model-and-options-envelope.md` §7 defect 3);
- the frame limit binds the pipe, not the disk: 64 MiB per message toward the Connector and 1 MiB back
  (`packages/schema/protocol/framing.json#/limits/extensionToHostBytes` and `#/limits/hostToExtensionBytes`),
  with chunking as the declared answer for an import that exceeds it, and staging as its durable half
  (`packages/schema/import-staging.schema.json`).

Where size binds in the Connector plane: the feed body at `maxBytes`, the config at one file per hash, history and
diagnostics at ring bounds, disk at `retainedPreviousVersions`.

So the removal of `unlimitedStorage` is not a size project, it is a **class** project. Three conditions, in order:

1. Only `derived` and `observation` rows may be evicted toward the Connector's disk — never `intent`. A user's
   routing rules are intent; moving them behind a process that may be absent would break §11. This is the rule
   that makes "large bodies want the Connector's disk" (`docs/architecture/31-security-model.md` §13) safe: the
   large bodies in question are feed bodies (already Connector-side) and generated PAC text (already `derived`).
2. `chrome.storage.local` without `unlimitedStorage` has a browser-side ceiling that this tree cannot measure
   (it is a platform fact, not a repo fact), so the removal needs a measurement of real envelope sizes across the
   compatibility matrix — `docs/architecture/38-compatibility-matrix.md`, `Verified: NO`.
3. The state store must stop being a durability dependency first (§6 rule 2), or a quota error mid-rewrite
   destroys more than the key being written.

Until all three hold, keeping the permission is the honest position: it is what today's users' data assumes.

The other half of the quota story is what a *failure* to fit means. The master prompt's rule — a subscription
update failure must not delete the last working version — is a placement rule as much as an error rule: the last
good version is not a retry queue entry, it is the `sync-remote-mirror` and the feed cache rows, and both are
written only by a successful refresh (`docs/architecture/29-subscriptions-and-servers.md` §9, LIVE-12).

## 10. What may not be stored at all

Forbidden, each with the mechanism that makes it a shape error rather than a review finding:

1. **A credential value in an intent document.** Every credential field is a reference; a header map written as a
   value is rejected by the negative example `packages/schema/examples/invalid/server.inline-headers.json`.
2. **A value in a sync or export payload** — the strip set, cross-checked against the catalogue's
   `secretValueKeys` (§5), and `syncExcludedValueKeys` including `headers`, `auth`, `body`, `gistToken`, `token`
   (`packages/schema/examples/envelope-keys.nexora.json:88-98`).
3. **A compiled config anywhere the user's exports go** — `compiledConfigInExports: false`
   (`packages/schema/envelope-keys.schema.json#/properties/secretReferences/properties/atRest`).
4. **A feed body in the browser process** — the Connector fetches and parses for exactly this reason
   (`docs/architecture/29-subscriptions-and-servers.md` §2), and the chunked-frame ban on `credentials` keeps the
   reverse path from reintroducing it.
5. **A second copy of observed state.** The grace deadline is arithmetic on `lastExit` plus the policy numbers
   (`docs/architecture/30-failure-policy.md` §11, `packages/schema/protocol/commands/core.status.json#/result/properties/lastExit`)
   — no envelope key, no migration, nothing to fall out of agreement. This row is the one
   `docs/architecture/30-failure-policy.md:323` asked this document to own.
6. **A secret in a log line, a `traceId`, or a diagnostics section** — sink masking with a redaction table built
   from live keystore contents (`docs/architecture/32-secrets-keystore-masking.md` §8), and
   `packages/schema/protocol/framing.json#/nonGoals` for the trace case.
7. **Which tab is active, in the Connector's world.** `docs/architecture/26-command-surface.md` §4's non-goal is a
   placement statement: tab state is browser-plane, transient, and none of the nineteen commands can carry it. A
   Connector that knows the active tab is a Connector with a new failure mode and no new capability.
8. **A content-derived reference.** §12's id rule, because a content-addressed reference makes every synced
   envelope an offline dictionary against the user's credentials
   (`packages/schema/examples/envelope-keys.nexora.json:104-108`).

## 11. NEXORA Lite, stated precisely

`docs/architecture/22-component-contracts.md` §9 invariant 4 promises that removing the Connector leaves the
profiles intact and degrades to "NEXORA Lite". Promises of that shape are where designs go dishonest, so:

**What still works with no Connector at all.** Everything in the browser plane that is `intent`: every profile the
user made — including legacy `FixedProfile`, `PacProfile`, `SwitchProfile` and `RuleListProfile` behaviour, which
is the whole of ZeroOmega's feature set — the routing engine, quick switch, the options page, import and export,
sync, and the failure policy document itself. The proxy actuator is `chrome.proxy` plus `chrome.webRequest` auth,
and it is browser-plane (`docs/audit/06-proxy-application.md`); the Connector is not in that path for a legacy
profile.

**What is absent, and what the UI says.** Health, latency, per-server probes, REALITY/VLESS transports, applied
config hashes, drift, and the Core lifecycle. Each of those is an `observation` row, and each renders as **unknown**,
never as `ok` and never as `fail`. The catalogue asserts the boolean (`survivesConnectorRemoval`) so that a future
row cannot quietly make a user-visible object depend on a process that may not be there.

**What Lite must not be allowed to mean.** Not a fallback to Direct for a NEXORA profile. If the selected profile
resolves through the Connector and the Connector is gone, the answer is the failure policy: the proxy stays pointed
at a closed loopback port and the state is visible (ADR-0009, `docs/architecture/30-failure-policy.md` §4,
LIVE-11). "The binary is missing" is a reason to refuse with an explanation, not a reason to send traffic
unproxied — which is the difference between Lite and a silent downgrade, and it is the sentence that keeps §11
honest next to ADR-0019's first-run network requirement.

**One asymmetry to keep visible.** A machine with no Connector can hold a keystore that nothing can read, and a
machine with a Connector can hold references with no values (§6, LIVE-19). Both degrade visibly and neither is
Lite's problem to solve; the purge paths are what stop either from becoming a leak
(`packages/schema/protocol/commands/keystore.purge.json#/args/properties/scope`).

## 12. Identity, references, and what survives what

Two identity schemes live side by side, and the split is the reason both must be described in one place:

| | Legacy | NEXORA |
|---|---|---|
| form | `+<name>`, a free-form user string | `-nexora.<entity>.<uuid>` |
| identity test | name collision and the `__` reservation (`docs/audit/04-data-model-and-options-envelope.md` §5) | the UUID, never the display name |
| how others refer to it | a bare name inside a `-` value or inside a profile (`defaultProfileName`, `profileName`, `matchProfileName`) | an id field (`packages/schema/connector-profile.schema.json#/properties/serverIds`) |
| rename | sweeps every reference in the envelope | nothing to sweep: the name is not the key |
| collides with | a builtin, and is then shadowed (`omega-pac/src/profiles.coffee:101`) | nothing, by construction |

Consequences that are placement, not taste:

1. **Health history is keyed by the NEXORA id, so a refresh that mints new ids destroys it.** That is
   `docs/architecture/29-subscriptions-and-servers.md` §6's identity-reuse rule read as a data-split fact: the key
   that survives is the one both planes agree on.
2. **A reference is minted, never derived** (§10 rule 8), so an id segment is a random identifier, and a secret
   that nothing references is refused rather than stashed
   (`packages/schema/protocol/commands/profile.import.json`'s second invariant). An orphan with no owner has no
   purge path that can reach it except `orphaned`, and `orphaned` is defined against entities, not against
   digests.
3. **Reinstall and re-image differ per plane.** An extension reinstall keeps the Connector's directory and its
   keystore (that is why the keystore is not in the extension's reach at all); a browser-profile move keeps
   whatever the sync backends carried and loses everything else, including every value. Both are §14's checks, and
   neither is a surprise if the table above is read as the only identity contract that matters.

## 13. Reconciling the two halves without either owning the other

ADR-0010 rule 6 in one paragraph: the extension sends intent, the Connector compiles and reports drift, the
extension re-applies after a reconnect, and import is always an intent document rather than a pre-compiled config.

The mechanics, in this document's terms:

- **Drift is a reported field, not a repaired state.** `config.snapshot` returns the applied hash and the verdict;
  the extension compares and offers to re-apply. The Connector never "fixes" intent to match what is running —
  that would be rule 6 inverted, and it is the failure mode that makes a proxy tool feel haunted.
- **Authority is per class, not per store.** `intent` is writable only by the browser; `observation` only by the
  Connector; `secret` by the Connector's minting path only; `control` by the installer only. Where a UI action
  needs two classes — deleting a server destroys both intent and a keystore entry — it is one command with a
  declared side effect (`profile.remove`), not two writes racing.
- **Reconnect is re-read, not replayed.** After a session loss the extension asks for status and snapshot and
  decides; a replayed `config.apply` would be an effect the user may not have wanted twice, and ADR-0008's
  idempotency cache exists for the ambiguous case, not as a licence to resend.
- **A dangling reference is a state, not an error at rest.** The profile is kept, marked as needing a credential,
  and `config.apply` answers `InvalidProfile` naming the reference (LIVE-19). Storage-time validation of an
  `entityId` is refused instead — §5's ingest rule — because the reference can become dangling *after* it was
  valid, and a design that only checks at write time has no word for that.

## 14. Who owns each check

`docs/adr/0010-data-split-extension-intent-connector-truth.md:121-124` defers three runtime invariants to this
document and asks it to name an owner for each. Naming them, plus the ones §5–§13 introduced:

| Invariant | Check that owns it | Owner | Status |
|---|---|---|---|
| The catalogue covers every namespace and agrees with the registry | assertions in `packages/schema/scripts/verify.mjs` (§5) | Schema | **Verified** — green in the 900-check run |
| Nothing credential-bearing leaves this machine except where a mitigation is named | the strip-set coverage and open-item-count assertions; `tools/check-no-secrets.mjs` for committed content | Schema + CI | **Verified** for the declared half |
| No secret in transit, logs or diagnostics | `LIVE-10` | Connector + Extension | `Verified: NO — Phase 2` |
| The keystore round-trips, rotates, purges, and its compiled config is owner-only | `LIVE-18` | Connector | `Verified: NO — Phase 2` |
| A restored envelope with a dangling reference degrades visibly | `LIVE-19` | Extension + Connector | `Verified: NO — Phase 2` |
| Nothing secret-shaped ever reaches `chrome.storage.sync`, and Lite keeps intent readable | `LIVE-22` | Extension + Connector | `Verified: NO — Phase 2`, and expected to fail on `zeroOmegaSync` until the §17 open item closes |
| A migration that copies only `chrome.storage.local` is not a migration | `LIVE-14`, and the store inventory §6 | Extension | `PARTIAL` — fixtures compile; the browser half is Phase 2 |
| The feed body never enters the browser process | `docs/architecture/29-subscriptions-and-servers.md` §11's build-graph assertion, plus `LIVE-12` | Extension + Connector | `Verified: NO — Phase 2` |
| No Connector row syncs, and no `intent` row depends on the Connector | the two Lite and connector-plane assertions | Schema | **Verified** |
| The binary and its receipts are verified before execution | `LIVE-02`, `LIVE-21` | Connector | `Verified: NO — Phase 2` |

## 15. Failure modes of the split itself

| Mode | What the user sees | What the design says | Where |
|---|---|---|---|
| Orphan secret (value with no intent pointing at it) | nothing, until a disk audit | a failure, not a stash: refused at ingest if its owner is absent from the payload, otherwise collected by `orphaned` | §12, LIVE-18 |
| Orphan reference (intent pointing at no value) | a profile marked as needing a credential | `InvalidProfile` naming the reference, never silent direct | §13, LIVE-19 |
| Duplicate truth (two copies disagreeing) | "running config is not the one you saved" | drift is reported by `config.snapshot`; only intent is writable, so one side is always wrong in a knowable way | §13 |
| Frame too large | nothing, if chunking worked; a refusal if `credentials` was chunked | `#/oversizedPayloads`, staging with `expiresAt`, and a hash that distinguishes a missing chunk from a corrupted one | §9 |
| Store unavailable | `KeystoreUnavailable`, and Core does not start | the failure policy applies; the keystore is never reachable from a wipe path | §8, doc 32 §12 |
| Install wipe while the user has intent | ZeroOmega today loses everything; NEXORA must not | ADR-0015's version continuity, ADR-0014's backup-before-migrate, and a keystore in a directory the extension never touches | §8 |
| Sync quota exceeded | legacy disables profiles silently; NEXORA must say so | durable status, and the last good set stays | §9 |
| Worker torn down mid-import | an import that half-applied, or a resume | staging plus `contentSha256` plus `chunkCount`: resume, or drop — never half | §9, doc 32 §6 |
| Reinstall with a live keystore | today: nothing to lose; NEXORA: profiles re-resolve against existing references | §12's plane asymmetry, asserted by `LIVE-22` | §12 |
| State blob race | a key that came back after deletion | not inherited: no NEXORA `intent` row lives there | §6 rule 2 |

## 16. Threat, control, check

| Threat | Control | Check |
|---|---|---|
| A credential reaches a synced or exported payload | references by class rule, strip set cross-checked against the catalogue, one admitted open item named | the two strip assertions in `packages/schema/scripts/verify.mjs`; `LIVE-22` |
| An observation becomes a source of truth after a restart | `intent`/`observation` classes with different writers; status re-read on reconnect | §13, `LIVE-13` |
| Both sides own one entity | per-class authority, one command per cross-class action | `profile.remove`'s declared side effect, `docs/architecture/26-command-surface.md` §1 |
| A deleted profile strands a secret | ingest refuses ownerless values; `orphaned` scope; `dryRun` reports `retained` | `LIVE-18` |
| The Connector reads more browser state than it needs | nineteen typed commands, none of which carries tab or DOM state | `docs/architecture/26-command-surface.md` §4, §10 rule 7 |
| A quota error destroys intent | only `derived`/`observation` may be evicted, and `unlimitedStorage` stays until the three §9 conditions hold | §9, `docs/architecture/31-security-model.md` §13 |
| A restored backup puts a value where a reference belongs | `SecretRedacted` refuses rather than strips; snapshots are local-only by class | `LIVE-18`, doc 32 §12 |
| The compiled config outlives its purpose | declared lifetime, owner-only ACL, `compiledConfigInExports: false` | `LIVE-18`; the unclean-kill half is open (§17) |

## 17. Invariants, amendments, open items

**Invariants this document adds to the design:**

1. Every byte has exactly one owning plane, one class, one writer and one death, and all four are data in
   `packages/schema/protocol/data-ownership.json`.
2. `intent` never rests in the Connector plane, and no `intent` row needs the Connector to be interpretable.
3. Credential material has one home. Anywhere else it is a reference, a mask, or a bounded plaintext file with a
   declared lifetime.
4. Only `intent` may travel, and only what the strip set permits may leave this machine.
5. Nothing that carries a credential value moves toward the browser; exactly one field moves it inward.
6. A store may be deleted without hesitation only if its class is `derived` or `observation`.
7. Degradation is a state of the data model, not of the UI: absence of the Connector must be expressible from the
   envelope alone, and must never be expressible as "direct".
8. A reference can become dangling after it was valid; the design has a word for that and it is not a validation
   error at write time.

**Amendments made by this document:**

1. `packages/schema/protocol/data-ownership.json` is created — 3 planes, 7 classes, 27 store rows, 9 movements —
   and `packages/schema/scripts/verify.mjs` gained twenty-one checks over it, including the namespace-equality and
   sync/secret agreement cross-checks, the strip-set coverage, the one-home rule, the ingest-door uniqueness, the
   Lite boolean, and the rule that every cited live row must exist in `docs/verification/live-matrix.md`.
   `npm --prefix packages/schema run verify` is green at 900 checks.
2. `docs/verification/live-matrix.md` gained `LIVE-22` (a dump of the three browser stores after real use
   contains no credential value, and removing the Connector leaves intent readable), because §14 must name owners
   and §5 must cite rows that exist.
3. The `-nexora.pins` row's class is `intent` while the Connector's copy is `control`, found by the harness and
   argued in §5 — the registry was already saying the envelope copy syncs.
4. `docs/architecture/31-security-model.md` §13's `unlimitedStorage` row now has an answer and an order of
   operations (§9); `docs/architecture/22-component-contracts.md` §9 invariant 4's Lite promise now has a section
   (§11); `docs/architecture/30-failure-policy.md` §11's "no second copy" claim is now row 5 of §10.

**Open, and going to `docs/architecture/40-open-decisions.md`:**

- the `zeroOmegaSync` half of SEC-5 — the one credential the catalogue admits still travels, and what a keystore
  reference for a sync token costs in the legacy sync UX;
- how many `-nexora.backup.*` snapshots to retain, and who decides when the answer is "the disk is full";
- whether legacy `derived` fields inside profiles (`pacScript`, `ruleList`) are ever evicted toward the Connector,
  or stay frozen where ADR-0002 put them;
- the compiled config after an unclean kill, shared with `docs/architecture/32-secrets-keystore-masking.md` §13
  and `docs/architecture/33-binary-integrity.md` §17;
- envelope-size measurements without `unlimitedStorage`, owed by
  `docs/architecture/38-compatibility-matrix.md` before §9's three conditions can be checked.

Trace: ADR-0002, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0015,
ADR-0019 · Contract: `packages/schema/protocol/data-ownership.json`,
`packages/schema/envelope-keys.schema.json`, `packages/schema/examples/envelope-keys.nexora.json`,
`packages/schema/server.schema.json`, `packages/schema/connector-profile.schema.json`,
`packages/schema/subscription-source.schema.json`, `packages/schema/import-staging.schema.json`,
`packages/schema/envelope-backup.schema.json`, `packages/schema/pins.schema.json`,
`packages/schema/protocol/framing.json`, `packages/schema/protocol/commands/profile.import.json`,
`packages/schema/protocol/commands/config.snapshot.json`, `packages/schema/protocol/commands/core.status.json`,
`packages/schema/protocol/commands/keystore.purge.json` · See also:
`docs/adr/0010-data-split-extension-intent-connector-truth.md`, `docs/audit/04-data-model-and-options-envelope.md`,
`docs/audit/05-storage-sync-and-migration.md`, `docs/audit/11-security-findings.md`,
`docs/architecture/21-system-context.md`, `docs/architecture/22-component-contracts.md`,
`docs/architecture/24-options-envelope-compat.md`, `docs/architecture/26-command-surface.md`,
`docs/architecture/29-subscriptions-and-servers.md`, `docs/architecture/30-failure-policy.md`,
`docs/architecture/31-security-model.md`, `docs/architecture/32-secrets-keystore-masking.md`,
`docs/architecture/33-binary-integrity.md`, `docs/architecture/35-schema-versioning-migration.md`.
