# 39 — The queue: every claim this repository makes that nothing checks, and the artefact that closes it

> Purpose: 202 lines across 44 documents say `Verified: NO — deferred to Phase 2`. `docs/adr/0001-adr-practice.md`
> rule 9 put the label there; `docs/adr/README.md` step 4 makes it the only honest alternative to naming a check;
> `docs/audit/00-scope-and-method.md` requires it of every `UNVERIFIED` finding it hands forward;
> `docs/architecture/20-target-repository-layout.md` §13 makes this document the owner of every one of them and
> states that no assertion there is complete until its row here is green; `docs/verification/live-matrix.md`
> promotion rule 4 requires that a `Verified: NO` row appear here so that no design document can invent a
> deferral nobody queued; `docs/adr/0020-test-strategy.md` requires a row per claim in itself;
> `docs/architecture/21-system-context.md`, `22-component-contracts.md`, `26-command-surface.md`,
> `27-config-compilation-pipeline.md`, `31-security-model.md`, `36-observability.md`, `37-ui-architecture.md` and
> `38-compatibility-matrix.md` each point here for the checks they do not have. A label scattered through 44
> documents is a rumour. **This is the queue.** Its value is not the list — the list is in
> `packages/schema/verification.json` — it is that the list is *exact*: every labelled line is answered by exactly
> one row, every row answers a line that still carries the label, and the two directions are asserted on every run.

Status: Accepted. Normative data: `packages/schema/verification.json`,
`packages/schema/verification.json#/marker`, `packages/schema/verification.json#/states`,
`packages/schema/verification.json#/owners`, `packages/schema/verification.json#/closingKinds`,
`packages/schema/verification.json#/live`, `packages/schema/verification.json#/vocabulary`,
`packages/schema/verification.json#/matrix`.
Machine-checked by: `packages/schema/scripts/verify.mjs` (§11 names the assertions).

---

## 1. What this document owns, and what it refuses

**Owns.**

- The queue itself: one row per obligation, with what is unverified, why it is not already checkable, who closes
  it, and the artefact that closes it.
- The state vocabulary — `no-code`, `no-check`, `no-run`, `no-fixture`, `needs-human` — because those are five
  different jobs and a queue that says only "not verified" puts "write the Go compiler" in the same column as
  "add one invalid example file".
- The owner vocabulary and the closing mechanisms, so that a row cannot be closed by "later".
- The inheritance of `docs/verification/live-matrix.md`: which rows exist and what their status column says.
- The corpus vocabulary: the sentences that *define* the label rather than carry a claim.

**Refuses.**

- **Execution.** A row here never says a check ran. It says what would have to run. `no-run` is the state of a
  check that exists and has not fired where the claim needs it; that is a statement about this iteration, not a
  result.
- **Copying the live matrix.** Its 24 rows are inherited by id (§7). A second copy of 24 rows is a second thing
  that can be wrong, and it would be wrong in the direction that matters: a stale status column reads as progress.
- **Designing anything.** A row is not a feature request and not an approval. 53 rows say the component does not
  exist; that is the queue reporting the phase boundary, not endorsing what comes after it.
- **Repeating the argument.** Why a claim is made lives in the document that carries the label. This document
  quotes the line and points at it; it does not re-litigate it.
- **Rounding a number into existence.** Six rows are `needs-human`. Their value is that they stay unnumbered.

---

## 2. The questions that were deferred here

| # | Question a document could not answer itself | Answered by |
|---|---|---|
| 1 | Who owns the 205-odd deferrals the corpus says exist? | §3 and §10 — the queue, with `VER-NN` ids, partitioned by state |
| 2 | Is a deferral the same thing as unfinished work? | No: §4 splits it five ways; the split *is* the useful content |
| 3 | Can a document claim a check exists when it does not? | §6 — anchors resolve or the run fails |
| 4 | Can one obligation be counted twice, so the queue looks shorter than it is? | §6 — the same partition check refuses it |
| 5 | Can the queue go stale, so that a closed claim still occupies a row? | §6 — an anchor must land on a line that still carries the label |
| 6 | What may this document say about the live matrix? | §7 — only what its own status column says: 24 specified, 1 partially executed, 0 executed |
| 7 | In what order can the queue be closed at all? | §8 — the ordering is forced by `LIVE-04` and by the two decisions |
| 8 | Do the compatibility promises that owe a browser measurement appear here? | §10 `VER-74`, and the run checks the four `COMP-07`, `COMP-10`, `COMP-14`, `COMP-18` are named |

---

## 3. The label is the contract

The corpus uses two spellings: `Verified: NO — deferred to Phase 2` (prose, and the form `docs/adr/0001-adr-practice.md`
mandates) and the table-column `**NO — Phase 2**` (`docs/architecture/35-schema-versioning-migration.md` onward).
Both are deferred claims, so the membership rule is one alternation over both, and it is written in
`packages/schema/verification.json#/marker` rather than hard-coded in the harness — the queue states its own
membership rule, and changing it is a visible edit to the data.

Two details are load-bearing and were learned the expensive way:

1. **One alternation, not two.** The first draft matched `Verified: NO` and `NO — deferred to Phase 2` as separate
   branches, and every line containing the full phrase counted twice. 205 was wrong; the phrase counts once.
2. **The leading `\b`.** Without it, `UNVERIFIED: no lockfile is tracked` matches `Verified: NO` case-insensitively
   and the queue inherits rows nobody wrote — a phantom obligation that looks like a real one because it has a
   document and a line behind it. The word boundary is the only thing separating "we have not verified this" from
   "we have not verified this, and it is a different statement".

The label is placed *at the point of use*, never collected into a caveats section — that rule is
`docs/adr/0001-adr-practice.md` rule 9 and `docs/architecture/31-security-model.md` §1, and it is what makes this
document possible at all: a deferral that lives in prose can be anchored. A deferral that lived only in a summary
would have to be re-derived from the design, which is how a verification matrix becomes fiction.

---

## 4. Five states, five jobs

| State | Rows | What it means | The job it implies |
|---|---|---|---|
| `no-code` | 53 | the component the claim is about does not exist | write the component, then its test — no checker can precede this |
| `no-check` | 13 | the thing exists (a contract, a document rule, an inherited file) and nothing asserts it | write a checker; no feature work is required |
| `no-run` | 2 | a check exists and is green where it ran, but has not run where the claim needs it | execute it — in CI or by hand, and record the run |
| `no-fixture` | 2 | the assertion is writable today; the input that would make it mean something is absent | add one file under `packages/schema/examples/` or `fixtures/` |
| `needs-human` | 5 | code cannot close it: a decision from `docs/PROJECT-STATE.md` §9, or a number only a browser yields | decide, or measure — then the row becomes one of the four above |

The distribution is the finding. **53 of 75 rows are `no-code`**: this iteration's claims are overwhelmingly
blocked on the Connector, the compiler and the UI existing, not on tests being clever. 13 `no-check` rows are the
part that could be closed *before* Phase 2 writes a line of product code — `VER-01`, `VER-04`, `VER-05`,
`VER-07`, `VER-08`, `VER-10`, `VER-56` and `VER-58` are all guards over files that already exist. `VER-19` and
`VER-66` are closeable with one example file each; `VER-18` and `VER-20` with one contract field each.

That ordering is not advice; it is what the states say.

---

## 5. Nine owners, eight closing mechanisms

`owners` — who can close the row: `connector` 34, `extension` 11, `ci` 7, `contracts` 6, `tools` 6, `packages` 3,
`ui` 3, `external` 3, `human` 2.

`closingKinds` — with what: `live-row` 39, `assertion` 15, `tool` 6, `ci-step` 6, `gate` 3,
`external-measurement` 3, `decision` 2, `new-live-row` 1.

Three consequences worth stating out loud:

- **39 of 75 rows close by executing an existing live row.** The queue is, numerically, mostly the live matrix seen
  from the other side. That is the point of promotion rule 4: the live rows are the executable half of the design,
  and a design claim that cannot name one of them is usually not a `no-code` row but a `no-check` one.
- **Exactly one row closes by a live row that does not exist yet** — `VER-65`, redirect-hop guards. It is the queue
  catching a citation in `docs/architecture/29-subscriptions-and-servers.md` that pointed at a row nobody wrote.
  Registering it as owed is the honest fix; deleting the citation would be the dishonest one. Two more citations
  pointed at rows that exist but do not test the claim (`LIVE-05` and `LIVE-06` on framing enforcement, `LIVE-05`
  on `core.select`, `LIVE-08` on containment, `LIVE-02` on determinism, `LIVE-11` on a 404, `LIVE-06` on fail-closed
  inside the live matrix itself); each now names its own closer or the right row, which is the same failure mode
  caught one tier down.
- **15 rows promise a named assertion** — in `packages/schema/scripts/verify.mjs`, in the Connector's test suite, or
  in the client's — **and every one of them promises an assertion this run does not yet make.** The harness checks
  that (`every row that promises an assertion promises one this run does not yet make`). It is what keeps the queue
  from becoming a graveyard: adding the assertion and leaving the row pointing at it is a build failure, so closing
  a row means deleting it in the same commit.

A row is closed when its artefact exists and passes — not when the row is re-labelled. `docs/adr/0001-adr-practice.md`
is append-only for the same reason a closed row is deleted rather than marked `done`: a status column that can be
edited in place is a status column that will be.

---

## 6. Anchors, not summaries

Every row lists `at`: pairs of `{doc, anchor}`, where `anchor` is a verbatim fragment of the line carrying the
label. Not a line number — line numbers in these documents move every time a paragraph is added, and
`docs/README.md` forbids citing a mutating file by line for exactly that reason. An anchor survives a re-flow; it
dies only if the sentence dies, which is precisely when the row should die too.

The harness then refuses three failures no prose review can see:

| Failure | Why a reader cannot see it | The assertion that refuses it |
|---|---|---|
| a labelled line no row answers | the corpus grows by paragraphs; nobody re-counts | `every deferred claim in the corpus is answered by a row, a vocabulary entry, or an inherited live row` |
| an anchor that no longer lands on a labelled line | the row looks fine; the document it came from was edited | `every anchor in the queue lands on one line of one document, and that line still carries the label` |
| one line claimed by two rows | both rows read correctly, and the queue is a lie about its length | `no deferred claim is counted as owned by two rows, or by a row and the vocabulary` |

Plus the exact-partition assertion: rows (152) + vocabulary (26) + inherited live rows (24) = 202 = the number the
corpus is re-read to, which `marker.counted` declares and the run compares. An unowned line is unowned work. A
stale anchor is finished work nobody deleted. A double claim is the queue lying about its own size — and 152 of
these claims come from 38 documents, 30 of them spanning more than one document, so the arithmetic cannot be done
by eye.

The **vocabulary** list is the fourth thing: 26 lines carry the label because they *define* it — the rule in
`docs/adr/README.md`, the purpose blocks at the head of docs 20, 21, 22, 31, 32, 33, the legend and promotion rules
of the live matrix, the `docs/README.md` convention line. They are not obligations. Listing them is what makes the
partition exact rather than approximately right; a check that allowed "a few unlabelled lines" would allow one,
and then allow ten.

---

## 7. The live matrix is inherited, never copied

`packages/schema/verification.json#/live` names `docs/verification/live-matrix.md` as the owner of the 24
`LIVE-NN` rows. The harness reads the file and asserts:

- the ids are exactly `LIVE-01` … `LIVE-24`, in order, no gap and no extra (`the live rows this queue inherits are
  exactly LIVE-01 to LIVE-24, in order`);
- the status column of every row is one of the three known words, **exactly one row is `PARTIAL` (`LIVE-14`, whose
  backup-import fixtures were validated against the real `omega-pac` engine on 2026-11-04) and no row is executed**
  (`in this iteration exactly one live row is partially executed and none is executed -- the queue may not read
  ahead`);
- every row here that closes by `live-row` names an id that exists, and the one row that closes by `new-live-row`
  does not claim its row already exists.

Inheritance has one rule attached, in `#/live/rule`: a row that closes by a live row **inherits that row's
status**. When `LIVE-04` is executed, `VER-27`, `VER-32` and `VER-68` move with it in the same commit; no document
may report a live row as done on the strength of its specification, which is the exact failure mode ADR-0020 rule 8
was written to prevent (`SKIPPED`, never `PASSED`).

The queue adds nothing to the live matrix and takes nothing from it. Environment variables, procedure and
`SKIPPED` semantics belong to `docs/verification/live-env-contract.md`.

---

## 8. The order the queue can be closed in

Not a schedule — a dependency graph the states impose:

1. **`VER-01`, `VER-04`, `VER-05`, `VER-07`, `VER-08`, `VER-10`, `VER-56`, `VER-58`, `VER-71`, `VER-18`,
   `VER-66`, `VER-19`** close before any product code exists. They are guards over files already on disk and two
   contract additions. They are the queue's own integrity, and they are why the rest of this list can be trusted
   later.
2. **`LIVE-04` is the gate.** Promotion rule 2 of the live matrix makes egress proof a precondition of
   `LIVE-05…LIVE-09`. `VER-27`, `VER-32` and `VER-68` close by executing `LIVE-04` itself, and `VER-50` hangs on
   `LIVE-05` — behind the gate twice. Until an egress IP equals the VPS IP, no transport row may be reported,
   because a TCP connect returning green while traffic leaks is ADR-0020's named worse-than-nothing test.
3. **Two decisions gate five rows.** `VER-54` waits on `ODEC-10` (does any external API surface survive) and
   `VER-61` on `ODEC-33` (whether the Connector owns subscriptions end to end) — both in doc 40's register; the
   inbound-per-tag question (`ODEC-06`) changes what `VER-30` even
   asserts. `VER-09`, `VER-37`, `VER-74`
   wait on browsers and stores, which is why they are `needs-human` and not quietly estimated.
4. **Everything else is `no-code` and waits on the Connector.** 34 of the 53 are owned by it.

The queue does not pretend this is fast. It is the reason the queue exists: 75 named obligations with 39 of them
pointing at one executable matrix is a plan; the same 75 as 202 scattered phrases is a mood.

---

## 9. What is forbidden

| # | Rule | Why |
|---|---|---|
| 1 | No document may add a `Verified: NO` claim that this queue does not carry | the harness fails on the unowned line; promotion rule 4 as a machine check |
| 2 | No row may be closed by editing its `state` | closing means the artefact exists and the row is deleted in the same commit |
| 3 | No row may cite a line number | lines move; an anchor that survives a re-flow is the only durable pointer in a living document |
| 4 | No row may report a live row as executed | §7 — inheritance means the status column is the live matrix's, and only its's |
| 5 | No row may be satisfied by a `note` in the harness | a `note` is a deferral of a *check*, which is the thing this queue is supposed to eliminate |
| 6 | No `needs-human` row may be given a number to make it green | `VER-09`, `VER-37`, `VER-74` are worth more unmeasured than invented (constraint 11) |
| 7 | The vocabulary may not absorb a real claim | a claim demoted to "vocabulary" hides work; each entry is a sentence about the rule, and the partition count would break |

---

## 10. The queue

75 rows, grouped by state. **Docs · lines** is how many documents carry the claim and how many labelled lines the
row answers. Full text, `why`, and every anchor are in `packages/schema/verification.json#/matrix`.

### `no-code` — 53

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-02 | the import boundary — nothing under `nexora/**` or `packages/**` may import from `omega-*` — is asserted by nothing | `ci` · gate | 4 · 4 |
| VER-03 | no CI grep gate fails when `vless`/`reality`/`xtls` appears as executable code under `packages/` or `nexora/` | `ci` · ci-step | 2 · 2 |
| VER-12 | no test shows a second Native Messaging connection re-adopts the running Connector instead of starting a second one | `connector` · LIVE-13 | 1 · 2 |
| VER-13 | reconnect after a port teardown (re-run `connector.hello`, reconcile by `config.snapshot` hash, jittered backoff) has never been executed | `connector` · LIVE-13 | 2 · 3 |
| VER-14 | nothing proves `Xray ⊆ connector ⊆ host` survives a killed parent, or that the Connector runs in the interactive session | `connector` · LIVE-13 | 2 · 4 |
| VER-15 | no test shows the host registers exactly one Native Messaging entry point per installation | `connector` · LIVE-16 | 1 · 1 |
| VER-16 | the runtime half of loopback-only: no compiled non-loopback inbound accepted, and `chrome.proxy` never pointing outside 127.0.0.1 | `connector` · LIVE-03 | 4 · 4 |
| VER-17 | nothing shows the Connector listens on nothing but its loopback inbounds — there is no HTTP control surface to grep | `connector` · LIVE-03 | 1 · 1 |
| VER-21 | fail-closed in the browser: no Core kill shows a closed block port, a badge, and no `DirectProfile` left applied | `extension` · LIVE-11 | 3 · 4 |
| VER-22 | no dump of `chrome.storage.local`, the state mirror and `chrome.storage.sync` after genuine use | `extension` · LIVE-22 | 3 · 3 |
| VER-23 | no DPAPI round-trip, rotation, purge, or owner-only compiled config on disk | `connector` · LIVE-18 | 4 · 5 |
| VER-24 | no secret shown absent from logs, diagnostics or an exported bundle by a real run | `connector` · LIVE-10 | 3 · 3 |
| VER-25 | no negative runtime test that a `-nexora.server.<id>` key yields no PAC entry, no `availableProfiles` row, no rule option | `extension` · assertion | 1 · 1 |
| VER-26 | two version stamps coexisting (`-schemaVersion` 2, `-nexora.schemaVersion` 3) never loaded by the legacy engine together | `extension` · LIVE-14 | 1 · 1 |
| VER-27 | the envelope→Xray compiler does not exist: representability, lowering, and the rank-5 bound are unread | `connector` · LIVE-04 | 2 · 3 |
| VER-28 | golden compiled configs do not exist, so byte-determinism and the three canonical shapes are unproven | `connector` · assertion | 3 · 5 |
| VER-29 | no run shows a failed `config.apply` leaves the previous config applied and reports `previousConfigHash` | `connector` · LIVE-03 | 2 · 3 |
| VER-30 | `core.select` performing no config write is asserted by nothing (and §9.5 changes what the row means) | `connector` · assertion: the applied-config hash is unchanged | 1 · 1 |
| VER-31 | cross-entity referential integrity — a dangling `serverId` — checked by nothing, because JSON Schema cannot see a sibling key | `extension` · assertion | 3 · 4 |
| VER-32 | the `unrepresentable[]` shape is validated; the behaviour of reporting rather than dropping is not shown | `connector` · LIVE-04 | 2 · 2 |
| VER-33 | the migration runner does not exist: ordering, resume, `dryRun`, per-step verify, restore-then-compare | `extension` · LIVE-23 | 2 · 3 |
| VER-34 | a future envelope held read-only while the legacy chain does not fire — neither half executed | `extension` · LIVE-24 | 1 · 1 |
| VER-35 | an envelope restored where the keystore lacks the value degrades visibly with `InvalidProfile`; never demonstrated | `connector` · LIVE-19 | 3 · 3 |
| VER-36 | the parity matrix gating cut-over, and the popup startup budget, have never been measured | `ui` · assertion | 2 · 2 |
| VER-38 | no build-time scan proves a Svelte root is never mounted inside AngularJS DOM | `packages` · gate | 1 · 1 |
| VER-39 | no control is proven disabled-with-a-reason when its backend is Phase 2 (constraint 11) | `ui` · assertion | 1 · 1 |
| VER-40 | the `.po`-to-catalog compiler, the `localeSupport` table and the promotion threshold do not exist | `packages` · assertion | 2 · 2 |
| VER-41 | no download, sha256, minisign, refusal-on-mismatch, install ACL, rollback or `manual` channel exercised; `pins.json` absent | `connector` · LIVE-02 | 2 · 3 |
| VER-42 | none of the twelve first-run steps, or what the user sees when one fails, has been exercised | `connector` · LIVE-20 | 1 · 1 |
| VER-43 | no pre-execution re-hash against a receipt, no archive-entry refusal, no lower-privilege replacement test | `connector` · LIVE-21 | 2 · 5 |
| VER-44 | the Core `log` section suppressing the access log — no started Core has been looked at | `connector` · LIVE-18 | 1 · 1 |
| VER-45 | the redaction pass never run against a live credential in any of the five sinks | `connector` · LIVE-10 | 2 · 2 |
| VER-46 | no two-plane fixture shows a trace id surviving the boundary; no migration run keeps the correlation history | `connector` · LIVE-23 | 1 · 1 |
| VER-47 | the diagnostics export uploading nothing — no review or grep of the options-page bundle has happened | `extension` · LIVE-10 | 1 · 1 |
| VER-48 | no run shows a record written, masked and rotated, with real `seq` gaps and a ring that evicts | `connector` · assertion | 1 · 1 |
| VER-49 | framing limits enforced by nothing: sender-side rejection before write, the oversize discard path, heartbeat | `connector` · assertion: codec, request-table and liveness tests | 1 · 5 |
| VER-50 | `server.test` results never replayed from cache — static half green, runtime half never happened | `connector` · LIVE-05 | 1 · 1 |
| VER-51 | the probe budget independent of the UI, the popup performing no side-effecting command on mount, `force` bypassing backoff only | `extension` · LIVE-17 | 1 · 1 |
| VER-52 | the sender policy's refusal, its amplification guard, and a second registration not doubling a budget | `connector` · LIVE-16 | 1 · 4 |
| VER-53 | no handler exists to prove no shell, path or bind address is caller-chosen at run time | `connector` · LIVE-03 | 1 · 1 |
| VER-55 | no run shows a per-sender budget counted *before* dispatch, answered `Busy`, with no side effect | `connector` · LIVE-17 | 1 · 1 |
| VER-59 | a fetched body never executed (compile-not-execute, https-only, byte caps, SSRF guard) — no run has a body to try | `connector` · LIVE-12 | 1 · 1 |
| VER-60 | commands that write the keystore naming `KeystoreUnavailable` and `SecretRedacted` — neither produced | `connector` · LIVE-18 | 1 · 1 |
| VER-62 | doc 33's detection controls are not code, so even detection is unproven | `connector` · LIVE-02 | 1 · 1 |
| VER-63 | a failed refresh keeping the last good version and recording durable status (constraint 7) | `extension` · LIVE-12 | 1 · 1 |
| VER-64 | nothing prevents the engine from fetching; no feed body entering the browser process is unproven | `packages` · gate | 2 · 2 |
| VER-67 | `id`, `tag`, `userIdRef` stable across refreshes; a feed name never overwriting a user rename | `connector` · LIVE-12 | 2 · 2 |
| VER-68 | the cap ladder warning before it applies — neither the warning nor the application has run | `connector` · LIVE-04 | 1 · 1 |
| VER-69 | a non-VLESS server in a legacy subscription reported, not dropped — there is no parser | `connector` · LIVE-14 | 1 · 1 |
| VER-70 | tag collision over the whole server set a compile error naming both ids — there is no compile step | `connector` · assertion | 1 · 1 |
| VER-72 | existing `SwitchProfile` rules selecting a NEXORA profile unchanged, on Firefox and on a real install | `extension` · LIVE-15 | 1 · 1 |
| VER-73 | the 64-inbound ceiling producing `QuotaExceeded` rather than a partially applied config | `connector` · LIVE-03 | 1 · 1 |
| VER-75 | `<local>` expansion shown in the UI — and there is no UI | `ui` · LIVE-15 | 1 · 1 |

### `no-check` — 13

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-01 | no script checks the ADR index, one heading per file, the six sections, or the 60-140 line bound | `tools` · tool | 1 · 1 |
| VER-04 | nothing prevents a root `package.json`, `go.work` or `.npmrc` from appearing | `tools` · tool | 2 · 3 |
| VER-05 | nothing asserts the eleven coupled legacy files and six legacy directories still sit at their cited paths | `tools` · tool | 1 · 2 |
| VER-06 | the commit-shape rules are review-only | `ci` · ci-step | 1 · 1 |
| VER-07 | `grunt watch` scope over the new roots is unguarded | `tools` · tool | 1 · 1 |
| VER-08 | no assertion inspects the *contents* of `dist/*.zip`; nothing enforces no-binary-in-a-tracked-path | `ci` · ci-step | 2 · 3 |
| VER-10 | line endings and binary classification are declared in `.gitattributes` and checked by nothing | `ci` · ci-step | 1 · 1 |
| VER-18 | the pipe floor and the browser protocol floor are declared independent; `minPipeVersion` does not exist | `contracts` · assertion | 1 · 1 |
| VER-20 | `UnsupportedCombination` is absent from the taxonomy, so a user-supplied port overlap has no right code | `contracts` · assertion | 3 · 4 |
| VER-56 | no tool reads the shipped manifest: the permission target column is unmeasured | `tools` · tool | 1 · 2 |
| VER-58 | SEC-3's negatives are unguarded: no `web_accessible_resources`, no injected fetched text | `tools` · tool | 1 · 1 |
| VER-65 | no live row covers guards applied to every redirect hop — the omission is the finding | `contracts` · new-live-row | 1 · 1 |
| VER-71 | `protocol` enum expansion requires an ADR, and that process rule is asserted by nothing | `contracts` · assertion | 1 · 1 |

### `no-run` — 2

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-11 | Ladder A ran by hand on one host and is green on three runner images at the pinned floors; what the row still owes is the unpinned dependency set, which no run can supply while `.gitignore:3` keeps the lockfile out | `ci` · ci-step | 1 · 2 |
| VER-57 | no CI build shows the packaged bundle survives the declared CSP | `ci` · ci-step | 1 · 2 |

### `no-fixture` — 2

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-19 | `dedupeWindow` bounds are neither data nor tested (ADR-0008 rule 2) | `contracts` · assertion | 2 · 2 |
| VER-66 | `format: auto` never resolving ambiguously — no fixture where both detectors fire | `contracts` · assertion | 1 · 1 |

### `needs-human` — 5

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-09 | extension identity and version-line continuity: the wipe trap is store behaviour, not repo behaviour | `external` · external-measurement | 2 · 3 |
| VER-37 | the UI budgets table is targets with no measurement behind any number | `external` · external-measurement | 1 · 1 |
| VER-54 | the inherited external surface still lets another extension read the whole options object | `human` · decision (`ODEC-10`) | 1 · 2 |
| VER-61 | whether the Connector owns subscriptions end to end is undecided, so the boundary that follows from the answer cannot be asserted | `human` · decision (`ODEC-33`) | 1 · 1 |
| VER-74 | `COMP-07`, `COMP-10`, `COMP-14`, `COMP-18` — browser floor, cohort count, envelope sizes, `unlimitedStorage` ceiling | `external` · external-measurement | 3 · 3 |

---

## 11. Who owns each check

Every mechanism named here is an assertion `packages/schema/scripts/verify.mjs` makes on this run; the run checks
that (§13, invariant 6), so this table cannot cite a check that stopped existing. The assertion that enforces this
table is deliberately absent from the table: a row quoting its own label is a check that validates itself, and the
run would pass even if the table had been deleted. Labels are quoted, never line numbers — a verifier section moves
whenever a rule is added above it, and `docs/README.md` forbids the alternative.

| Mechanism | Assertion label that enforces it | State |
|---|---|---|
| The corpus is re-read, not remembered | "the queue is derived from the whole documentation corpus, not from a list of files it was written against" | GREEN |
| The declared count matches the derived one | "the number of deferred claims the queue says the corpus carries is what the corpus carries" | GREEN |
| Row ids are closed and dense | "the queue is a closed set of rows with no duplicate and no hole in its ids" | GREEN |
| The three vocabularies are closed | "every row names one state, one owner and one closing mechanism, and all three vocabularies are closed" | GREEN |
| A row is not a placeholder | "every row says what is unverified and why the state is what it is, rather than only that it is owed" | GREEN |
| Anchors resolve | "every anchor in the queue lands on one line of one document, and that line still carries the label" | GREEN |
| Nothing is counted twice | "no deferred claim is counted as owned by two rows, or by a row and the vocabulary" | GREEN |
| Nothing is unowned | "every deferred claim in the corpus is answered by a row, a vocabulary entry, or an inherited live row" | GREEN |
| The partition is exact | "the queue partitions the corpus exactly: rows, plus the sentences that define the label, plus the inherited live rows" | GREEN |
| The live ids are the ones we have | "the live rows this queue inherits are exactly LIVE-01 to LIVE-24, in order" | GREEN |
| No reading ahead | "in this iteration exactly one live row is partially executed and none is executed -- the queue may not read ahead" | GREEN |
| `closesBy` points at a real row | "a row that closes by a live row names a row the live matrix has, and a row that needs a new one does not claim it exists" | GREEN |
| Promised assertions are still owed | "every row that promises an assertion promises one this run does not yet make, so a closed row is deleted rather than re-labelled" | GREEN |
| The COMP debts are queued | "the four compatibility promises that owe a browser measurement are queued as the measurement they owe" | GREEN |
| The distributions read aloud | "the state distribution doc 39 reads aloud is what the data says" · "the owner distribution doc 39 reads aloud is what the data says" · "the closing-mechanism distribution doc 39 reads aloud is what the data says" | GREEN |
| Rows `VER-01`…`VER-75` beyond the contract layer | the tools, CI steps, Go tests and live runs named in each row's `closesBy` | Verified: NO — deferred to Phase 2, **which is the entire contents of this document** |

---

## 12. Threat, control, check

| Threat to the verification itself | Control | Check |
|---|---|---|
| A deferral is written and never queued | the label is machine-recognised; membership is derived from the corpus | "every deferred claim in the corpus is answered by a row, a vocabulary entry, or an inherited live row" |
| The queue is padded so it looks managed | exact partition, dense ids, one owner and one artefact per row | "the queue partitions the corpus exactly: rows, plus the sentences that define the label, plus the inherited live rows" |
| Work is reported closed because a row was re-worded | a promised assertion must still be absent | "every row that promises an assertion promises one this run does not yet make, so a closed row is deleted rather than re-labelled" |
| The live matrix is read ahead of itself | status read from its owner, on every run | "in this iteration exactly one live row is partially executed and none is executed -- the queue may not read ahead" |
| The harness quietly shrinks what it scans | a floor on the corpus size and on §11's label count | "the queue is derived from the whole documentation corpus, not from a list of files it was written against" |
| A real claim is demoted to vocabulary to make the numbers work | the vocabulary list is explicit, counted and anchored | "every anchor in the queue lands on one line of one document, and that line still carries the label" |

---

## 13. Invariants, amendments, and the open items

**Invariants.**

1. Every deferred claim in `docs/**` has exactly one home: a row, the vocabulary, or an inherited live row.
2. A row is closed by its artefact, and closing deletes the row in the same commit.
3. Nothing in this document or its data asserts that a check ran. `no-run` is a state, not a result.
4. The live matrix owns the live rows; this document owns what the rest of the corpus owes them.
5. The count `201` is an output of the harness, not a contract: it grows when a document gains a label and shrinks
   when a row is honestly closed. Nothing outside `packages/schema/verification.json#/marker/counted` may depend on it.
6. This document's §11 table is checked by an assertion whose label is `"every mechanism doc 39 names is a check
   this run actually makes"` — it lives outside the table so it cannot certify itself.

**Amendments.** None. Adding a row costs: one `matrix` entry with at least one resolvable anchor, and the
distribution numbers in §4, §5 and `verify.mjs` (three assertions exist to make a silent mismatch impossible).

**Open items.**

1. 15 rows promise a named assertion. 10 of them can only be written once the code they test exists; `VER-18`,
   `VER-20` and `VER-71` promise an assertion over a contract already on disk. Six more (`VER-01`, `VER-04`,
   `VER-05`, `VER-07`, `VER-56`, `VER-58`) promise a guard under `tools/` over files already on disk. Those nine are
   the rows a Phase-2 commit can delete without writing a line of product code.
2. `VER-65` needs a new live row, which means `docs/verification/live-matrix.md` gains `LIVE-25` — at which point
   §7's dense-id assertion must be amended in the same commit, deliberately, rather than allowed to drift.
3. `VER-09` and `VER-74` cannot be closed in-repo at all; they are the two places where this queue's answer is
   "someone must open a browser".
4. The 53 `no-code` rows are the honest size of Phase 2's verification debt. It is stated here, before Phase 2,
   because it is much harder to state afterwards.
5. `docs/adr/0020-test-strategy.md:86` calls this document "not yet written". ADR-0001 rule 5 forbids correcting
   history in place, and the same aside sits unsuperseded in `docs/adr/0016-svelte-5-vite-typescript.md:46`, so it
   stays: the ADR was true when written, and this file is the later fact. A superseding ADR is the only way to
   change the record, and nothing here reverses a decision.
6. Three lines are stale in exactly that sense, in the same direction: `docs/adr/0018-workspace-roots-and-no-root-package-json.md:55`,
   the same record at `:121`, and `docs/adr/0020-test-strategy.md:126` all describe
   `.github/workflows/verify.yml` as not yet written. It was written on 2026-11-05, so those three were true when
   authored and are now the earlier fact. The same day it ran, and `docs/audit/99-baseline-report.md` §12 tabulates
   eight of those runs — two red, five green, one cancelled — quoting each measured line there, and
   `docs/audit/02-toolchain-decay-and-risk.md` §7.3 says which
   half of each claim the runner measured. Nothing in this queue moved because of it, and that is not timidity —
   `packages/schema/verification.json` has no state meaning closed, so a row closes by being deleted together with
   the labelled sentence in the document that anchors it, which changes `marker.counted` and the state distribution
   §4 reads aloud — both of which `packages/schema/scripts/verify.mjs` holds as hard-coded numbers, on purpose. `VER-06`, `VER-08`, `VER-10` and `VER-11` are the four whose stated condition a green run now
   meets; `VER-02` and `VER-03` ran over roots that hold no code yet; `VER-04` and `VER-05` are queued against a
   `check-layout-guards` tool that has not been written; `VER-57` names a CSP load test the workflow does not
   contain. The same reading applies to the sentences in `docs/architecture/20-target-repository-layout.md` §7 and §8
   whose deferral explains that no checker or guard exists yet for the import boundary or the bare root: the scan and
   the guard are steps of that workflow and they are green on a runner, and the deferral is still honest for the
   different reason each row now gives — an empty `nexora/**`, and an unwritten tool.

Trace: ADR-0001 · ADR-0002 · ADR-0003 · ADR-0004 · ADR-0005 · ADR-0006 · ADR-0008 · ADR-0009 · ADR-0010 ·
ADR-0011 · ADR-0012 · ADR-0013 · ADR-0014 · ADR-0015 · ADR-0016 · ADR-0017 · ADR-0018 · ADR-0019 · ADR-0020 ·
docs/PROJECT-STATE.md §0 constraints 7, 8, 10, 11, 13 and §9 · Contract:
`packages/schema/verification.json`, `packages/schema/scripts/verify.mjs`,
`packages/schema/compatibility.json#/matrix`, `packages/schema/protocol/framing.json`,
`packages/schema/protocol/error-taxonomy.json`, `tools/check-no-secrets.mjs`, `tools/check-doc-evidence.mjs` ·
See also: `docs/verification/live-matrix.md`, `docs/verification/live-env-contract.md`,
`docs/architecture/20-target-repository-layout.md`, `docs/architecture/38-compatibility-matrix.md`,
`docs/architecture/40-open-decisions.md`, `docs/audit/00-scope-and-method.md`
