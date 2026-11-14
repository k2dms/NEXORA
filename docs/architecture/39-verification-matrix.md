# 39 — The queue: every claim this repository makes that nothing checks, and the artefact that closes it

> Purpose: 196 lines across 44 documents say `Verified: NO — deferred to Phase 2`. `docs/adr/0001-adr-practice.md`
> rule 9 put the label there; `docs/adr/README.md` step 4 makes it the only honest alternative to naming a check;
> `docs/audit/00-scope-and-method.md` requires it of every `UNVERIFIED` finding it hands forward;
> `docs/architecture/20-target-repository-layout.md` §13 makes this document the owner of every one of them and
> states that no assertion there is complete until its row here is green; `docs/verification/live-matrix.md`
> promotion rule 4 requires that a `Verified: NO` row appear here so that no design document can invent a
> deferral nobody queued; `docs/adr/0020-test-strategy.md` requires a row per claim in itself;
> `docs/architecture/21-system-context.md`, `22-component-contracts.md`, `26-command-surface.md`,
> `27-config-compilation-pipeline.md`, `31-security-model.md`, `36-observability.md`, `37-ui-architecture.md` and
> `38-compatibility-matrix.md` each point here for the checks they do not have. A label scattered through 43
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
- **Designing anything.** A row is not a feature request and not an approval. 44 rows say the component does not
  exist; that is the queue reporting the phase boundary, not endorsing what comes after it.
- **Repeating the argument.** Why a claim is made lives in the document that carries the label. This document
  quotes the line and points at it; it does not re-litigate it.
- **Rounding a number into existence.** 4 rows are `needs-human`. Their value is that they stay unnumbered.

---

## 2. The questions that were deferred here

| # | Question a document could not answer itself | Answered by |
|---|---|---|
| 1 | Who owns the 180 deferrals the corpus says exist? | §3 and §10 — the queue, with `VER-NN` ids, partitioned by state |
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
| `no-code` | 44 | the component the claim is about does not exist | write the component, then its test — no checker can precede this |
| `no-check` | 5 | the thing exists (a contract, a document rule, an inherited file) and nothing asserts it | write a checker; no feature work is required |
| `no-run` | 10 | a check exists and is green where it ran, but has not run where the claim needs it | execute it — in CI or by hand, and record the run |
| `no-fixture` | 2 | the assertion is writable today; the input that would make it mean something is absent | add one file under `packages/schema/examples/` or `fixtures/` |
| `needs-human` | 4 | code cannot close it: a decision from `docs/PROJECT-STATE.md` §9, or a number only a browser yields | decide, or measure — then the row becomes one of the four above |

The distribution is the finding. **44 of 65 rows are `no-code`**: this iteration's claims are overwhelmingly
blocked on the Connector, the compiler and the UI existing, not on tests being clever. 5 `no-check` rows read as
the part closable before Phase 2 writes a line of product code, and four of them — `VER-20`, `VER-56`,
`VER-65`, `VER-71` — are named in an open decision's `blocks`, which makes them unclosable while that question is
open: deleting a row around its gate either leaves the gate blocking nothing or leaves it naming a row that no
longer exists, and `verify.mjs` refuses both. The fifth, `VER-58`, is a guard over files already on disk whose
injection half needs a `nexora/**` that is empty — the same emptiness §8 declines to count as evidence. The sixth,
`VER-85`, is the corpus files existing on both sides of the question — thirteen command docs without a
code for a malformed request, two shipped analogies recorded at their sites — and what is absent is the rule that
says which of the register's three options they follow. The column
was nine at the start of 2026-11-06 and lost five rows that day: `VER-01` to `tools/check-adr-shape.mjs`,
`VER-04`, `VER-05`, `VER-07` to `tools/check-layout-guards.mjs`, and `VER-18` to a number added in
`packages/schema/protocol/framing.json`. Two rows beside it — `VER-19` and `VER-66` — still need one example file
each, which is why they sit in another column: their blocker is an input, not a checker.

That ordering is not advice; it is what the states say.

---

## 5. Nine owners, eight closing mechanisms

`owners` — who can close the row: `connector` 36, `extension` 10, `contracts` 5, `ci` 2, `packages` 3,
`ui` 3, `external` 3, `tools` 2, `human` 1.

`closingKinds` — with what: `live-row` 41, `assertion` 13, `decision` 1, `gate` 2, `ci-step` 2,
`external-measurement` 3, `tool` 2, `new-live-row` 1.

Three consequences worth stating out loud:

- **41 of 65 rows close by executing an existing live row.** The queue is, numerically, mostly the live matrix seen
  from the other side. That is the point of promotion rule 4: the live rows are the executable half of the design,
  and a design claim that cannot name one of them is usually not a `no-code` row but a `no-check` one.
- **Exactly one row closes by a live row that does not exist yet** — `VER-65`, redirect-hop guards. It is the queue
  catching a citation in `docs/architecture/29-subscriptions-and-servers.md` that pointed at a row nobody wrote.
  Registering it as owed is the honest fix; deleting the citation would be the dishonest one. Two more citations
  pointed at rows that exist but do not test the claim (`LIVE-05` and `LIVE-06` on framing enforcement, `LIVE-05`
  on `core.select`, `LIVE-08` on containment, `LIVE-02` on determinism, `LIVE-11` on a 404, `LIVE-06` on fail-closed
  inside the live matrix itself); each now names its own closer or the right row, which is the same failure mode
  caught one tier down.
- **13 rows promise a named assertion** — in `packages/schema/scripts/verify.mjs`, in the Connector's test suite, or
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
| a row deleted so its obligation disappears with it | the queue gets shorter and nobody can say which claim it lost | `every VER id the queue ever issued is live or retired, no id is recycled, and a retired one says what it was and what closed it` |

Plus the exact-partition assertion: rows (130) + vocabulary (26) + inherited live rows (24) = 180 = the number the
corpus is re-read to, which `marker.counted` declares and the run compares. An unowned line is unowned work. A
stale anchor is finished work nobody deleted. A double claim is the queue lying about its own size — and 144 of
these claims come from 38 documents, 18 of which carry more than one, so the arithmetic cannot be done
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

1. **`VER-56`, `VER-58`, `VER-65`, `VER-71`, `VER-20`, `VER-66`, `VER-19`** are what this step was: everything the
   queue said could close before a line of product code exists. Four of the seven are gated — `VER-56` by `ODEC-11`,
   `VER-71` by `ODEC-08`, `VER-65` by `ODEC-07`, `VER-20` by `ODEC-03` and `ODEC-04` — and the harness makes a gated
   row unclosable while the question that owns it stays open, because deleting it either leaves that question
   blocking nothing, which doc 40's own assertions forbid, or leaves it naming a row no longer in the matrix, which
   is a phantom. What is actually left in this step is three rows, and each is smaller than it reads: `VER-66` needs
   a body detector the entity schemas have no field for — the register question closest to it in shape, `ODEC-40`,
   was answered on 2026-11-07 and left, while this one is still open — `VER-19`
   now needs a live cross-port replay/adoption fixture, not the implemented process-local bounds, and `VER-58`'s injection half is a
   scan over a `nexora/**` that is empty — the emptiness §8 refuses to count as evidence, which is exactly why
   `VER-02` and `VER-03` stayed open until the roots held real code: they closed 2026-11-14, the day the
   extension sources made the same greps non-vacuous (`docs/PROJECT-STATE.md` §4.71).
   The fourth was `VER-83` — the queue's only row of that shape: nothing absent from disk, no question gating it,
   a sixth pass in a guard that already ran. It closed 2026-11-14 by writing the pass (`docs/PROJECT-STATE.md` §4.70).
   `VER-18` closed on 2026-11-06, the first row this step deleted, and it closed by answering the decision that
   gated it rather than around it: `minPipeVersion` is in `packages/schema/protocol/framing.json` now, which is
   `ODEC-02`'s own first option. Eight rows that belonged to this step closed the same day:
   `VER-01`, `VER-06`, `VER-08`, `VER-10` in the queue's first deletions, `VER-04`, `VER-05`, `VER-07` in its
   second, and `VER-18` in its third. The `VER-06`, `VER-08`, `VER-10` three went with their steps green on the runner; `VER-01` went with its
   checks green on the runner as inline shell and, in the same commit, gathered into `tools/check-adr-shape.mjs`,
   and the three whose ids the workflow now carries as one step gathered into `tools/check-layout-guards.mjs` — two
   scripts no pipeline has executed yet, and the next run of this workflow is that. `VER-11` did not close, because
   its own record keeps the half a run cannot give it while no lockfile is tracked.
2. **`LIVE-04` is the gate.** Promotion rule 2 of the live matrix makes egress proof a precondition of
   `LIVE-05…LIVE-09`. `VER-27`, `VER-32` and `VER-68` close by executing `LIVE-04` itself, and `VER-50` hangs on
   `LIVE-05` — behind the gate twice. Until an egress IP equals the VPS IP, no transport row may be reported,
   because a TCP connect returning green while traffic leaks is ADR-0020's named worse-than-nothing test.
3. **One decision gates one row.** `VER-54` waits on `ODEC-10` (does any external API surface survive) —
   and `VER-88`, whose registration landed on 2026-11-07, beside it
   half of the seam `VER-80` carried. The fourth, `VER-61` on `ODEC-33`, closed on 2026-11-07 by
   the answer its question asked for: an `adr`-kind entry, so the answer is `docs/adr/0021-subscription-servers-travel-in-the-refresh-result.md`
   and the row left with the question it gated — the first closure by elimination, since the data already refused two
   of that entry's three options — and the run refuses a row that closes by a decision no entry
   gates. Four `no-check` rows — `VER-20`, `VER-56`, `VER-65`, `VER-71` — are named in a `blocks` as well, which is
   why §4 says they cannot be deleted around their gate. Behind them the inbound-per-tag question
   (`ODEC-06`) changes what `VER-30` even asserts. `VER-09`, `VER-37`, `VER-74` wait on browsers and stores, which is
   why they are `needs-human` and not quietly estimated.
4. **Everything else is `no-code` and waits on the Connector.** 28 of the 44 are owned by it. Four connector rows had
   left the column by 2026-11-07 — `VER-23`, whose keystore is now code with a green test over the real
   `crypt32` call, `VER-29`, whose writer and rollback are now Go tests over a temp directory
   (`connector/internal/apply`), `VER-14`, whose containment scope is now assigned, reported and proven against a
   child the supervisor spawned and killed (`connector/internal/supervisor`), and `VER-32`, whose report is now read
   out of the compiled result and into the answer frame by
   `connector/cmd/nexora-connector/config_apply_test.go` — and all four are in `no-run` rather
   than closed, because the live row each owes is a session, not a syscall, a rename or a `CloseHandle`. The column is
   back to 54 in the same slice because `VER-80` entered it: a command path with no production caller and no contract
   saying what it reads is a component that does not exist, which is what this state means. It left again on
   2026-11-07, by the decision `ODEC-39` was for — and the row's residual is `VER-88`, the registration, still in
   this column and still a component that does not exist. `VER-81` entered it in the
   next slice, and was the column's first row owned by `contracts` — the five contract-owned rows before it are
   `no-check` or `no-fixture`, which is the queue's own way of saying a contract obligation usually means a checker
   rather than a component — and the first row of the queue to close by a decision and not by the Connector's work:
   on 2026-11-07 its register question `ODEC-40` was answered by the field it asked for, and the row left with it.
   `VER-82` entered it in the slice after that, the sixth `contracts` row and the column's only `no-code` one then,
   and the only decision row the Connector did not own; it left on 2026-11-07, the pipe field its question asked for
   now in the event payload, and the column's `contracts` row count is back to five.
   The latest slice moved the column without closing anything: `VER-43` split, its pre-exec half leaving as `VER-87`
   for `no-run` the moment the receipt code existed (`connector/internal/receipt`), and what stayed is the
   archive-entry refusal of doc 33 §5 step 7, which has no extractor to be code for.
   What does not exist here is a place to put the answer, not the code that would compute it.
   On 2026-11-11 two more left for `no-run`, both owned by `connector` and both owed to `LIVE-16`: `VER-15`, because
   `connector/cmd/nexora-installer` writes one manifest, points three family keys at it and reads them back out of
   the real hive, and `VER-52`, because a real host process answered a foreign origin with one `UnauthorizedExtension`
   and closed the port on the eleventh refusal. Neither is closed: no browser has resolved the registration, and the
   per-sender port budget turns out to have no consumer at all — `contract.PortsPerSenderMaxConcurrent` is generated
   and read by nothing, which is a narrower gap than "unmeasured" and belongs to `ODEC-01`'s naming half.
   The next slice of the same day moved the column without changing its size: `VER-13` left for `no-run`, because
   `packages/state`'s client walks the capped jittered ladder and reconciles by hash on an injected clock and then
   did both against a real host that was killed twice, and `VER-90` entered it — the expectation
   `config.snapshot`'s result field promises has nowhere to ride in that command's closed `args`, and that method
   remains unregistered. The subsequent `core.status` slice does not close this snapshot obligation.

The queue does not pretend this is fast. It is the reason the queue exists: 72 named obligations with 41 of them
pointing at one executable matrix is a plan; the same 65 as 180 scattered phrases is a mood.

---

## 9. What is forbidden

| # | Rule | Why |
|---|---|---|
| 1 | No document may add a `Verified: NO` claim that this queue does not carry | the harness fails on the unowned line; promotion rule 4 as a machine check |
| 2 | No row may be closed by editing its `state` | closing means the artefact exists and the row is deleted in the same commit, and its id retires in `#/retired` |
| 3 | No row may cite a line number | lines move; an anchor that survives a re-flow is the only durable pointer in a living document |
| 4 | No row may report a live row as executed | §7 — inheritance means the status column is the live matrix's, and only its's |
| 5 | No row may be satisfied by a `note` in the harness | a `note` is a deferral of a *check*, which is the thing this queue is supposed to eliminate |
| 6 | No `needs-human` row may be given a number to make it green | `VER-09`, `VER-37`, `VER-74` are worth more unmeasured than invented (constraint 11) |
| 7 | The vocabulary may not absorb a real claim | a claim demoted to "vocabulary" hides work; each entry is a sentence about the rule, and the partition count would break |
| 8 | No `VER-NN` may be reissued, and since 2026-11-06 neither may an `ODEC-NN` | ADR-0001 rule 2 forbids it of ADR numbers for the same reason: these ids are cited across the corpus, in the step names of `.github/workflows/verify.yml`, and verbatim in the runner logs `docs/audit/99-baseline-report.md` §12 quotes — renumbering would silently change what every earlier citation means. The register's entries are cited the same way, by the `verification.json` rows that wait on them, so an answered entry retires in `open-decisions.json#/retired` instead of leaving a hole the ids were forbidden to have |

---

## 10. The queue

65 rows, grouped by state; the 25 ids this queue has closed are in `packages/schema/verification.json#/retired`,
not in these tables. **Docs · lines** is how many documents carry the claim and how many labelled lines the
row answers. Full text, `why`, and every anchor are in `packages/schema/verification.json#/matrix`.

### `no-code` — 50

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-12 | no test shows a second Native Messaging connection re-adopts the running Connector instead of starting a second one | `connector` · LIVE-13 | 1 · 2 |
| VER-16 | the runtime half of loopback-only: no compiled non-loopback inbound accepted, and `chrome.proxy` never pointing outside 127.0.0.1 | `connector` · LIVE-03 | 4 · 4 |
| VER-17 | nothing shows the Connector listens on nothing but its loopback inbounds — there is no HTTP control surface to grep | `connector` · LIVE-03 | 1 · 1 |
| VER-21 | fail-closed in the browser: no Core kill shows a closed block port, a badge, and no `DirectProfile` left applied | `extension` · LIVE-11 | 3 · 4 |
| VER-22 | no dump of `chrome.storage.local`, the state mirror and `chrome.storage.sync` after genuine use | `extension` · LIVE-22 | 3 · 3 |
| VER-24 | no secret shown absent from logs, diagnostics or an exported bundle by a real run | `connector` · LIVE-10 | 3 · 3 |
| VER-25 | no negative runtime test that a `-nexora.server.<id>` key yields no PAC entry, no `availableProfiles` row, no rule option | `extension` · assertion | 1 · 1 |
| VER-26 | two version stamps coexisting (`-schemaVersion` 2, `-nexora.schemaVersion` 3) never loaded by the legacy engine together | `extension` · LIVE-14 | 1 · 1 |
| VER-27 | core-plane compilation and explicit per-tag fallbacks exist; browser-rule lowering and the LIVE-04 session remain unproved | `connector` · LIVE-04 | 2 · 3 |
| VER-28 | five of doc 27 §3's six golden cases compare byte-for-byte; shape C's `headersRef` is materialized via `headersKeys` (`ODEC-45`), and fail-closed belongs in `chrome.proxy`, not a distinct Xray config | `connector` · assertion | 3 · 5 |
| VER-30 | `core.select` performing no config write is asserted by nothing (and §9.5 changes what the row means) | `connector` · assertion: the applied-config hash is unchanged | 1 · 1 |
| VER-33 | the migration runner does not exist: ordering, resume, `dryRun`, per-step verify, restore-then-compare | `extension` · LIVE-23 | 2 · 3 |
| VER-34 | a future envelope held read-only while the legacy chain does not fire — neither half executed | `extension` · LIVE-24 | 1 · 1 |
| VER-35 | an envelope restored where the keystore lacks the value degrades visibly with `InvalidProfile`; never demonstrated | `connector` · LIVE-19 | 3 · 3 |
| VER-36 | the parity matrix gating cut-over, and the popup startup budget, have never been measured | `ui` · assertion | 2 · 2 |
| VER-38 | no build-time scan proves a Svelte root is never mounted inside AngularJS DOM | `packages` · gate | 1 · 1 |
| VER-39 | no control is proven disabled-with-a-reason when its backend is Phase 2 (constraint 11) | `ui` · assertion | 1 · 1 |
| VER-40 | the `.po`-to-catalog compiler, the `localeSupport` table and the promotion threshold do not exist | `packages` · assertion | 2 · 2 |
| VER-41 | no download, sha256, minisign, refusal-on-mismatch, install ACL, rollback or `manual` channel exercised; `pins.json` absent | `connector` · LIVE-02 | 2 · 3 |
| VER-42 | none of the twelve first-run steps, or what the user sees when one fails, has been exercised | `connector` · LIVE-20 | 1 · 1 |
| VER-43 | the archive-entry refusal that runs before anything is written — a zip is an attacker-controlled directory listing, and the extractor does not exist | `connector` · LIVE-21 | 1 · 1 |
| VER-44 | the Core `log` section suppressing the access log — no started Core has been looked at | `connector` · LIVE-18 | 1 · 1 |
| VER-45 | the redaction pass never run against a live credential in any of the five sinks | `connector` · LIVE-10 | 2 · 2 |
| VER-46 | no two-plane fixture shows a trace id surviving the boundary; no migration run keeps the correlation history | `connector` · LIVE-23 | 1 · 1 |
| VER-47 | the diagnostics export uploading nothing — no review or grep of the options-page bundle has happened | `extension` · LIVE-10 | 1 · 1 |
| VER-48 | no run shows a record written, masked and rotated, with real `seq` gaps and a ring that evicts | `connector` · assertion | 1 · 1 |
| VER-49 | framing enforcement clause by clause: the oversized-write refusal and the late-answer discard are tested, the three-missed-heartbeat verdict is not | `connector` · assertion: codec, request-table and liveness tests | 1 · 5 |
| VER-50 | `server.test` results never replayed from cache — static half green, runtime half never happened | `connector` · LIVE-05 | 1 · 1 |
| VER-51 | the probe budget independent of the UI, the popup performing no side-effecting command on mount, `force` bypassing backoff only | `extension` · LIVE-17 | 1 · 1 |
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
| VER-72 | existing `SwitchProfile` rules selecting a NEXORA profile unchanged, on Firefox and on a real install | `extension` · LIVE-15 | 1 · 1 |
| VER-73 | the 64-inbound ceiling producing `QuotaExceeded` rather than a partially applied config | `connector` · LIVE-03 | 1 · 1 |
| VER-75 | `<local>` expansion shown in the UI — and there is no UI | `ui` · LIVE-15 | 1 · 1 |
| VER-76 | chunk reassembly has no slice body — the contract says what a slice is, never where its bytes ride | `connector` · assertion | 1 · 1 |
| VER-84 | the HealthProber and its `server.test` surface are built and tested, and unregistered: the prober refuses to build without the egress endpoint and expected address no schema field carries, and the surface reads a server store no document has placed | `connector` · decision (`ODEC-42`) | 1 · 1 |
| VER-86 | the `health-history` store row's `medium` of `disk` is kept nowhere: the built ring dies with the process and nothing writes it to a path | `connector` · assertion | 1 · 1 |

| VER-90 | the drift primitive the reconnect step leans on cannot answer: `config.snapshot`'s closed `args` carry no expectation, so `matchesExtensionHash` is null on every permitted answer, and it still has no handler — `core.status` is now registered, but does not provide the missing snapshot comparison | `connector` · LIVE-06 | 1 · 1 |

### `no-check` — 6

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-20 | `UnsupportedCombination` is absent from the taxonomy, so a user-supplied port overlap has no right code | `contracts` · assertion | 3 · 4 |
| VER-56 | no tool reads the shipped manifest: the permission target column is unmeasured | `tools` · tool | 1 · 2 |
| VER-58 | SEC-3's negatives are unguarded: no `web_accessible_resources`, no injected fetched text | `tools` · tool | 1 · 1 |
| VER-65 | no live row covers guards applied to every redirect hop — the omission is the finding | `contracts` · new-live-row | 1 · 1 |
| VER-71 | `protocol` enum expansion requires an ADR, and that process rule is asserted by nothing | `contracts` · assertion | 1 · 1 |
| VER-85 | which code answers a malformed request is decided per command by analogy — `HandshakeFailed` in `connector.hello`, `HealthCheckFailed` in `server.test` — and thirteen of nineteen command docs declare no `BadRequest` | `contracts` · decision (`ODEC-43`) | 1 · 1 |

### `no-run` — 10

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-11 | Ladder A ran by hand on one host and is green on three runner images at the pinned floors; what the row still owes is the unpinned dependency set, which no run can supply while `.gitignore:3` keeps the lockfile out | `ci` · ci-step | 1 · 2 |
| VER-13 | reconnect after a port teardown runs: `packages/state`'s client re-runs `connector.hello`, walks the capped jittered ladder and reconciles by hash, on an injected clock and against a real host killed twice (1009 ms and 1203 ms on the first rung); no browser has torn down a port on worker eviction | `connector` · LIVE-13 | 2 · 3 |
| VER-14 | the scope is built, assigned and proven for the child the Connector spawns — the job object, the assignment, and a test that ends its own child by closing the handle — while the chain above Core needs a host this repository has not written and a session that kills a real parent | `connector` · LIVE-13 | 2 · 4 |
| VER-15 | one manifest and three family keys are written and read back from HKCU by `connector/cmd/nexora-installer` and its tests, while no browser has resolved that registration | `connector` · LIVE-16 | 1 · 1 |
| VER-23 | no live run shows a secret surviving a round-trip and dying on command: the store, its DPAPI round-trip and its rotation are Go tests over the real `crypt32` call on this host, and the owner-only file the keystore feeds is now written and re-read by Go tests too, while `keystore.purge` and the masking sink remain unwritten | `connector` · LIVE-18 | 4 · 5 |
| VER-29 | no live apply has failed: the temp-file, sync, chmod and rename are Go tests over a temp directory, and a document whose secret is missing leaves the previous config byte-identical — the rollback half of doc 27 §8 is measured, and a command frame now drives it, while the Core half is not | `connector` · LIVE-03 | 2 · 3 |
| VER-32 | the compiled report reaches the `config.apply` answer and a test shows two entries arriving in one frame while the config stays applied; no browser has been shown the array yet | `connector` · LIVE-04 | 2 · 2 |
| VER-52 | a real host process answered a foreign origin with one `UnauthorizedExtension` and closed the port on the eleventh refusal, reading the allowlist the installer wrote; no browser has done it, and the per-sender port budget has no consumer in any process | `connector` · LIVE-16 | 1 · 4 |
| VER-57 | no CI build shows the packaged bundle survives the declared CSP | `ci` · ci-step | 1 · 2 |
| VER-87 | the pre-exec hash of doc 33 §6 exists as code — a receipt reader, a verifier and the supervisor's spawn path that runs it before every exec — but no live core.start or core.restart has executed it, and nothing in this build writes the receipt it reads | `connector` · LIVE-21 | 2 · 4 |

### `no-fixture` — 2

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-19 | Bounded process-local dedupe is tested; native-port recreation/adoption replay has no live fixture | `contracts` · assertion | 2 · 2 |
| VER-66 | `format: auto` never resolving ambiguously — no fixture where both detectors fire | `contracts` · assertion | 1 · 1 |

### `needs-human` — 4

| Row | What is unverified | Owner · mechanism | Docs · lines |
|---|---|---|---|
| VER-09 | extension identity and version-line continuity: the wipe trap is store behaviour, not repo behaviour | `external` · external-measurement | 2 · 3 |
| VER-37 | the UI budgets table is targets with no measurement behind any number | `external` · external-measurement | 1 · 1 |
| VER-54 | the inherited external surface still lets another extension read the whole options object | `human` · decision (`ODEC-10`) | 1 · 2 |
| VER-74 | `COMP-07`, `COMP-10`, `COMP-14`, `COMP-18` — browser floor, cohort count, envelope sizes, `unlimitedStorage` ceiling | `external` · external-measurement | 3 · 3 |

### Retired — 10

Closed in the five deletions this queue has performed. Four rows on 2026-11-06 in the first, three in the second —
`VER-04`, `VER-05`, `VER-07`, the last rows of the queue's own integrity that needed no product code —
and one in the third, `VER-18`, the first row closed by answering an open decision instead of waiting on one. The
fourth deleted `VER-81`, and the fifth deleted `VER-61` — the first `adr`-kind entry in the register, and the first
row this queue closed by elimination: two of the question's three options were already refused by data on disk, so
the answer was the reading that was left. The id stays retired: §9 rule 8.

| Row | What it claimed was unchecked | What closed it |
|---|---|---|
| VER-01 | no script checked the ADR index, the one-heading rule, the six sections or the 60-140 line bound | `tools/check-adr-shape.mjs` — ADR-0001 rules 1-4, 10 and 11 over `docs/adr`, with a `--selftest` that injects one defect per rule and refuses each — and the `VER-01` step of `.github/workflows/verify.yml` that runs it; the same checks ran on all three runner images as inline shell since 2026-11-05, and the shell became this script rather than stay a second copy of the rule |
| VER-04 | a root `package.json`, `go.work` or `.npmrc` could appear and nothing would notice | the `root-bare` rule of `tools/check-layout-guards.mjs` — five root paths refused — running in the workflow step that carried this id as a bash loop until the same commit gathered that loop into the script the row always named as its closer |
| VER-05 | nothing asserted that the coupled legacy files and the six legacy directories still sit at their cited paths | the `coupling`, `legacy-roots` and `locale-chain` rules of `tools/check-layout-guards.mjs`, which read doc 01 §7's 23-row enumeration rather than copying it and print the census the audit's headline does not make (13 distinct files, not 11) |
| VER-06 | the commit-shape rules were review-only | the `VER-06` step of `.github/workflows/verify.yml` — label, `CUT-OVER:` scope, one cut-over per range — green on the `contracts` cells of every recorded run since `a7627ae` stated the vocabulary that step enforces |
| VER-07 | `grunt watch` would pick up `packages/**` the moment a `Gruntfile.*` appeared outside the legacy roots | the `watcher-scope` rule of `tools/check-layout-guards.mjs`, over the five tracked Gruntfiles and both `watch.coffee` files, walking `git ls-files` so a scratch archive cannot trip it |
| VER-08 | no assertion inspected an artifact's contents, and nothing enforced 'no binary in a tracked path' | the magic-byte scan of every tracked file and the `check_zip` of the `legacy-chain` job, green on all three runner images since `ceedabf` — the uncompressed total is a printed NOTE, not a floor — plus the `.gitattributes` executable suffixes ADR-0019 rule 1 asked for |
| VER-10 | `.gitattributes` declared line endings and binary classes that nothing checked | the `VER-10` step of `.github/workflows/verify.yml` — `git diff --check` over the range, `git ls-files --eol` over the index — green on all three runner images |
| VER-18 | the design said the pipe and the browser protocol were versioned independently and one number said so | `packages/schema/protocol/framing.json#/minPipeVersion` and `#/versionFloors`: two windows, four numbers, one `tooOld` code (`VersionMismatch`) because adding a code is breaking for a client that switches exhaustively — the answer `ODEC-02` listed first, so the gate retired in the same commit, and the harness now reads both windows out of the file. The comparison itself lives in `connector/internal/envelope`'s `Parse` and `TestThePipeFloorComparisonCompilesAndRuns` pins that it runs; it cannot fire while `packages/schema/protocol/envelope.schema.json#/properties/v/minimum` is the same 1 the floor carries, and `VER-89` registers the answer shape a below-floor frame still owes |
| VER-81 | the format an `auto` parse resolves to had nowhere to be recorded, while §5 promised `auto` never survives into a stored source | `ODEC-40` answered by its own first option: a `format` field on each `refreshed` item of `packages/schema/protocol/commands/subscription.refresh.json#/result`, required and enumerated minus `auto`, with the §5 sentence it satisfies, the §7 status row, the `data-ownership.json` amendment and the fixture item in `packages/schema/fixtures/protocol/probes-and-ops.jsonl` |
| VER-61 | whether the Connector owns subscriptions end to end, so the delivery boundary that follows from the answer had nothing to assert against | `ODEC-33` answered by `docs/adr/0021-subscription-servers-travel-in-the-refresh-result.md` — the chunked hand-off excluded by `packages/schema/observability.json#/events/7/payload` and `packages/schema/protocol/envelope.schema.json#/properties/chunk`, the raw-body read excluded by doc 29 §11's invariant, and what survives written in `packages/schema/protocol/commands/subscription.refresh.json#/result/properties/refreshed/items/required`; doc 29 §13 item 6's labelled line left with it and the deferral moved to `VER-64` on the ADR's own `## Verification` |

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
| Row ids are issued once | "every VER id the queue ever issued is live or retired, no id is recycled, and a retired one says what it was and what closed it" | GREEN |
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
| Rows `VER-01`…`VER-90` beyond the contract layer — 65 live, 25 retired | the tools, CI steps, Go tests and live runs named in each row's `closesBy` | Verified: NO — deferred to Phase 2, **which is the entire contents of this document** |

---

## 12. Threat, control, check

| Threat to the verification itself | Control | Check |
|---|---|---|
| A deferral is written and never queued | the label is machine-recognised; membership is derived from the corpus | "every deferred claim in the corpus is answered by a row, a vocabulary entry, or an inherited live row" |
| The queue is padded so it looks managed | exact partition, ids issued once and never reissued, one owner and one artefact per row | "the queue partitions the corpus exactly: rows, plus the sentences that define the label, plus the inherited live rows" |
| The queue is shortened by deleting a row nobody closed | the retired ledger names every id that left, with what it claimed and what closed it | "every VER id the queue ever issued is live or retired, no id is recycled, and a retired one says what it was and what closed it" |
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
5. The count `180` is an output of the harness, not a contract: it grows when a document gains a label and shrinks
   when a row is honestly closed. Nothing outside `packages/schema/verification.json#/marker/counted` may depend on it.
6. This document's §11 table is checked by an assertion whose label is `"every mechanism doc 39 names is a check
   this run actually makes"` — it lives outside the table so it cannot certify itself.
7. A `VER-NN` is an address, not a tally: once issued it is never renumbered, reused or recycled, and a closed row
   leaves the matrix but not the ledger.

**Amendments.** 2026-11-06, with `connector/internal/supervisor`: `VER-14` moves `no-code` → `no-run`, `VER-79` is
added over the line doc 30 §13 now carries, and `marker.counted` moves 205 → 206 for that one new label. Adding a row
costs: one `matrix` entry with at least one resolvable anchor, and the distribution numbers in §4, §5 and `verify.mjs`
(three assertions exist to make a silent mismatch impossible). Same day, with `connector/cmd/nexora-connector`:
`VER-32` moves `no-code` → `no-run`, because the compiled report now crosses into the `config.apply` answer and a test
holds the frame open to show two entries arriving together; `VER-80` is added over the new row in doc 27 §9, and
`marker.counted` moves 206 → 207 — one label, because moving a row between states relabels nothing. doc 27 §8's
sentence lost the clause "no command surface calls either" for exactly that reason and kept its anchor, and doc 26 §2.1
gained the reading `expectedAppliedHash`'s own `[string, null]` type states: an unstated belief is no precondition.
A third amendment the same day, over `docs/architecture/29-subscriptions-and-servers.md` §5 rather than over Go:
`VER-81` is added, and `marker.counted` moves 207 → 208 for the one label §5 gained — the sentence promising that
`auto` never survives into a stored source is now a deferred claim, because the run found that nothing in the contract
carries the answer. Its state is `no-code` and its owner is `contracts`: a field that would have to exist before an
assertion could mean anything is a component that does not exist, which is what this state has always meant — said here
for the first time about a row the contract rather than the Connector owns. `ODEC-40` opens with it, so §8's third step
now names five decisions rather than four.
A fourth amendment the same day, and the first one that deletes rows rather than adding them: the run recorded in
`docs/audit/99-baseline-report.md` §12 met the stated condition of `VER-06`, `VER-08` and `VER-10`, so those three rows
are gone from `matrix`, their labelled sentences are gone from `docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md`
and `docs/architecture/20-target-repository-layout.md` §6, §9 and §11, `marker.counted` moves 208 → 203 for the five
labels those documents lost, and the distribution §4 and §5 read aloud moves with them. Deleting a row is the only
closure this queue has (§9 rule 2), and it broke invariant 7's other half: the ids `VER-06`, `VER-08`, `VER-10` are
cited by name in the step titles of `.github/workflows/verify.yml` and in runner logs, so an assertion that demanded
a dense `VER-01`…`VER-81` range would have had to renumber what those citations already named. The range is therefore
no longer required to be dense; it is required to be *issued once*, and `verification.json#/retired` now carries one
tombstone per closed row -- the date, what the row claimed, and the artefact that closed it. The assertion that replaced
the dense-id check is `"every VER id the queue ever issued is live or retired, no id is recycled, and a retired one
says what it was and what closed it"`, and it is the reason §9 rule 2 could be obeyed here at all: the row leaves, the
record stays, and a deletion becomes a named fact.
A fifth amendment the same day, and the first row closed by a tool rather than by a run: `VER-01` is gone from
`matrix` and is the first tombstone in `#/retired`, `marker.counted` moves 203 → 202 for the one labelled sentence
`docs/adr/0001-adr-practice.md` ## Verification lost, and the distribution §4 and §5 read aloud moves with it
(`no-check` 10 → 9, `tools` 6 → 5, `tool` 6 → 5, 78 rows → 77). What makes it different from the three above is the
evidence: the checks it names already ran, as two inline shell steps of `.github/workflows/verify.yml`, on every
runner image since 2026-11-05 — the row was not waiting for a pipeline, it was waiting for a script, and the bound
it named (rule 11's 60-140 lines) and the index's own `Status` and `Date` cells had no checker at all until
`tools/check-adr-shape.mjs`. The shell is gone instead of staying beside the script: two statements of one rule is
the failure ADR-0001 was written to prevent. What this closure does not claim is a runner execution of the script
itself; the next run of the workflow is that, and a push is confirmed each time.
A sixth amendment the same day, and the first closure that deleted three rows at once: `VER-04`, `VER-05` and
`VER-07` are gone from `matrix` and are the queue's fifth, sixth and seventh tombstones, `marker.counted` moves
202 → 196 for the six labelled sentences they carried — three, two and one — and the distribution §4 and §5 read
aloud moves with them (`no-check` 9 → 6, `tools` 5 → 2, `tool` 5 → 2, 77 rows → 74). Their closer is
`tools/check-layout-guards.mjs`: five refusals over the layout contract — the bare root, the coupled files read out
of doc 01 §7's own table rather than copied from it, the six legacy directories, the `po2crx` locale chain, and the
reach of `grunt watch` — each with a defect injected in its `--selftest`, so the CI step proves the guard bites
rather than merely executes. Two things this closure states rather than hides. The census the tool prints found 13
distinct files behind that section's 23 rows, where its headline counts 11; the difference is recorded in the tool,
in `VER-05`'s tombstone and in `docs/architecture/20-target-repository-layout.md` §4, and the audit's own sentence
was left alone, because a checker that silently adopted a count different from the document it cites is a second
wrong answer rather than a correction. And the bash loop that enforced the bare-root rule in
`.github/workflows/verify.yml` is gone instead of staying beside the script, for the reason the `VER-01` shell went:
two statements of one rule is the failure the corpus keeps being asked to avoid. What this closure does not claim is
a runner execution of either script — the step named for all three ids will provide that on the next run, and a push
is confirmed each time.
A seventh amendment the same day, and the first closure that began by answering a question rather than writing a
checker: `VER-18` is gone from `matrix` and is the queue's eighth tombstone. Its gate, `ODEC-02`, is gone from
`open-decisions.json` and is that register's first retirement, decided by the first option its own entry listed —
two floors in one file — so `framing.json` now carries `minPipeVersion` beside `frameFormatVersion` and
`minCompatibleProtocolVersion` beside `protocolVersion`, in a `versionFloors` block that says which window is
negotiated where and names one `tooOld` for both, because adding an error code is breaking for a client that
switches exhaustively. `marker.counted` moves 196 → 195 (the one labelled sentence in `docs/architecture/21-system-context.md` §12
that the row owned), and the distribution moves with it (`no-check` 6 → 5, `contracts` 7 → 6, `assertion` 18 → 17,
74 rows → 73). Two rules changed shape to let this happen. The register's ids were dense; now they are issued once,
which is doc 39 §9 rule 8 adopted for `ODEC-NN` for the same reason it exists for `VER-NN` — an id is cited by other
documents, and renumbering a citation is how a citation becomes a lie. And the harness's own label for that rule
changed, which is the second time this document's §11 table has had to follow a label rather than a line number.
What this closure does not claim is enforcement on the wire: `minPipeVersion` is a number a build carries and a test
compares to the version it floors, and nothing yet refuses a frame whose `v` is below it. That half is stated in
`docs/architecture/25-native-messaging-protocol.md` §1, not queued as if it were done.
An eighth amendment, 2026-11-07, and the first row that split rather than entered or closed: `connector/internal/receipt`
— the install receipt reader of doc 33 §5 step 10 and the producer of §6's pre-exec hash — gives the code half of
`VER-43`'s subject its implementation, so the row divides. `VER-43` keeps the one clause nothing has a seam for yet
(§5 step 7's archive-entry refusal) and stays `no-code` over its one remaining label; the pre-exec half leaves as
`VER-87`, takes the §6 label, the two §16 rows it now owns and doc 34 §14's row, and enters the queue in `no-run` —
a check exists and is green where it has run (the package's own tests, and a drive over the real release asset that
hashed the 27.4 MB binary in 18 ms and refused a one-byte tamper), but no registered command reaches the spawn path
and no installer exists to write the receipt, whose signature half is pinned open twice (`ODEC-22`, `ODEC-25`).
`marker.counted` does not move: a split re-owns labels rather than adding them, which is the property that made the
split honest. The distribution moves with it (`no-code` 57 unchanged — the row stayed in the column and narrowed,
`no-run` 6 → 7, 76 rows → 77, `connector` 41 → 42, `live-row` 39 → 40). Corrected in the same pass, because the run
measured it rather than trusted it: ¶4's `38 of the 57` has said 37 against the data since a previous slice added a
connector `no-code` row without touching a sentence no readNumbers regex covers.

**Compiler evidence continuation.** `VER-28` and `VER-78` now name five compared goldens, including
`two-plane-core-rules` and `shape-c-grpc-tls`. `connector/internal/apply/drive_test.go` recompiles each golden input and calls `apply.Write`
before Xray 24.11.11 `run -test -c`: all five materialized configs were accepted and the invalid-port control rejected.
The compiler now emits object-shaped `policy.levels`, VLESS credentials under `vnext[].users[]`, referenced
`direct`/`freedom` and `block`/`blackhole` outbounds, array `network`/`protocol` selectors, and per-tag fallback rules
after authored policy rules. All per-tag inbounds are SOCKS5; enabled sniffing is an inbound sibling of `settings`, with
`protocols` mapped to `destOverride`. Schema and compiler restrict sniffing to `http`, `tls`, `quic`, `fakedns` and
refuse `routeOnly: false`. Twenty loopback HTTP/UDP cases now exercise the measured routing subset; ignored
`invert: true` is reported whole. `TestDriveXrayTLSProtocolPredicate` further measures the `tls` predicate: Xray 24.11.11
matches `protocol:[tls]` only when the ClientHello carries an SNI, with sniff-off and plain-HTTP nonmatching controls. `TestDriveXrayBitTorrentAndWebSocketPredicates` adds fourteen paired cases: the TCP BitTorrent handshake is detected; plaintext WebSocket Upgrade matches `http`, not `ws`. The compiler reports any `ws` rule whole, including mixed protocol arrays, without changing server WebSocket transport. The `fakedns` predicate is answered from this compiler's own output rather than a fixture: Xray registers its sniffer only beside a FakeDNS engine and the pipeline emits none, so `TestFakednsRuleIsReportedWhole` reports such rules whole instead of shipping a predicate that can only miss; inversion has no lowering left to prove because the rule language has no negation and only a port complement is expressible. QUIC's positive traffic case and a live FakeDNS match with an engine remain unproved.
This narrows rather than retires `ODEC-37`. Shape C's `headersRef` is now materialized via `headersKeys` (ODEC-45); `VER-28` retains only the fail-closed case owned by
`chrome.proxy`; `VER-78` closed on 2026-11-07 by the ODEC-36 answer; the goldens' byte-stability half has its producer. No row, state, distribution or marker count
changes; doc 27 §3's existing anchor changes from two remaining cases to one in `verification.json` with the prose.

**Installer continuation.** 2026-11-11, with `connector/cmd/nexora-installer`: `VER-15` and `VER-52` move
`no-code` → `no-run`, both owed to `LIVE-16`. The installer writes the one manifest under the per-user root's `host`
directory, points the Chrome, Edge and Firefox keys at it, writes the allowlist at
`packages/schema/protocol/framing.json#/senderPolicy/identity/allowlistFile`, and removes all of it on uninstall; its tests read the directory,
the argv and the real hive back. The live half ran on this host: a host process with no `--allowlist` flag read that
file, served the registered sender through to `connector.hello`, answered a foreign origin with one
`UnauthorizedExtension` and no Connector started, and closed the port on the eleventh refusal in the window.
`VER-52`'s subject narrows as it moves — the per-sender port budget is not unmeasured behaviour but absent behaviour,
since `contract.PortsPerSenderMaxConcurrent` is generated and read by nothing. The distribution moves
`no-code` 52 → 50, `no-run` 7 → 9, `connector` `no-code` 34 → 31; 71 rows, the owner and closing-kind columns and
`marker.counted` are unchanged, because moving a row between states relabels nothing. Two defects the live run found
are fixed in the same slice and named in `docs/PROJECT-STATE.md` §4.55: `flag.Parse` stops at the verb, so
`install --origin <id>` parsed no flags, and a nil allow-list slice marshalled to `null` rather than `[]`.

**Extension client.** 2026-11-11, with `packages/state`: `VER-13` moves `no-code` → `no-run`, owed to `LIVE-13`, and
`VER-90` enters in `no-code`, owed to `LIVE-06`. The client is the extension half of the pipe — an injected port, one
in-flight request per id, the per-method deadline read from the generated projection, the two concurrency windows,
the jittered capped ladder and the reconcile-by-hash step — and its harness asserts each of them on a fake clock.
The live half ran against a real `nexora-host` spawning a real `nexora-connector`: hello negotiated, two host kills
produced 1009 ms and 1203 ms reconnects that re-ran hello, and the second reconcile reported a mismatch. `VER-90` is
what that run found rather than what it proved: `config.snapshot`'s closed `args` carry no expectation, so the
`matchesExtensionHash` its result promises is null on every answer the schema permits, and neither it nor
`core.status` is registered — both answered `Internal`. The distribution moves `no-run` 9 → 10, `connector` 38 → 39,
`live-row` 40 → 41, 71 rows → 72 and `marker.counted` 193 → 194; `no-code` stays 50, because one row left the column
as another entered it. `VER-49` does not move: its heartbeat clause still has no wire shape to test.

**Core status.** 2026-11-11: `core.status` now answers the supervisor's live process and retained applied hash and
last exit. Nine Go tests cover its command surface, a real child lifecycle and a reopened state record; three
real-host sessions validated six successful replies against the result schema, including a persisted signal and
an unreadable receipt. `VER-90` remains `no-code`: the missing snapshot handler and expectation field are untouched.
The distributions and marker count do not move; `docs/PROJECT-STATE.md` §4.57 records the evidence and limitations.

**Open items.**

1. 13 rows promise a named assertion. 14 of them can only be written once the code they test exists; `VER-20`
   and `VER-71` promise an assertion over a contract already on disk, and `VER-19` and `VER-66` need one
   fixture file each. Two more (`VER-56`, `VER-58`) promise a guard under `tools/` over files already on disk —
   `VER-01` was the sixth of that older list and `VER-04`, `VER-05`, `VER-07` its seventh, eighth and ninth, and all
   four closed on 2026-11-06, the last three with `tools/check-layout-guards.mjs`. `VER-18` was the tenth and closed
   the same day, by `packages/schema/protocol/framing.json` rather than by a checker. What is left of that list —
   three assertions, two fixtures, two guards — is smaller than it reads, and §8 step 1 says why: three of the six
   are named in an open decision's `blocks`, and a gated row cannot be deleted around its gate. This item counted
   them as nine, then ten, then eight; every row came off by closing, not by being re-counted.
2. `VER-65` needs a new live row, which means `docs/verification/live-matrix.md` gains `LIVE-25` — at which point
   §7's dense-id assertion must be amended in the same commit, deliberately, rather than allowed to drift.
3. `VER-09` and `VER-74` cannot be closed in-repo at all; they are the two places where this queue's answer is
   "someone must open a browser".
4. The 44 `no-code` rows are the honest size of Phase 2's verification debt. It is stated here, before Phase 2,
   because it is much harder to state afterwards.
5. `docs/adr/0020-test-strategy.md:86` calls this document "not yet written". ADR-0001 rule 5 forbids correcting
   history in place, and the same aside sits unsuperseded in `docs/adr/0016-svelte-5-vite-typescript.md:46`, so it
   stays: the ADR was true when written, and this file is the later fact. A superseding ADR is the only way to
   change the record, and nothing here reverses a decision.
6. Two lines are stale in exactly that sense, in the same direction: `docs/adr/0018-workspace-roots-and-no-root-package-json.md:55`
   and `docs/adr/0020-test-strategy.md:126` both describe `.github/workflows/verify.yml` as not yet written. It was
   written on 2026-11-05, so those two were true when authored and are now the earlier fact. The same day it ran, and
   `docs/audit/99-baseline-report.md` §12 tabulates
   ten of those runs — three red, six green, one cancelled — quoting each measured line there, and
   `docs/audit/02-toolchain-decay-and-risk.md` §7.3 says which
   half of each claim the runner measured. Nothing in this queue moved because of it for four days, and that was not
   timidity: `packages/schema/verification.json` has no state meaning closed, so a row closes by being deleted together
   with the labelled sentence in the document that anchors it, which changes `marker.counted` and the state distribution
   §4 reads aloud — both of which `packages/schema/scripts/verify.mjs` holds as hard-coded numbers, on purpose.
   `VER-06`, `VER-08` and `VER-10` are the three whose stated condition a green run meets, and the 2026-11-06 commit
   named in §13 deleted them; `VER-11` is the fourth whose run is green and stays, because its own subject asks for a
   lockfile that `.gitignore:3` refuses to track, and a run over an absent artefact is not that artefact.
   `VER-02` and `VER-03` ran over roots that hold no code yet, and still do; `VER-57` names a CSP load test the workflow
   does not contain. `VER-01` was the one row of that group whose run did not need to be waited for: its checks were
   already shell steps of the workflow and green on every image, and the row named a script rather than a
   pipeline, so it closed when the script arrived (§13's fifth amendment). `VER-04`, `VER-05` and `VER-07` are the same
   case one tier on: the workflow already refused the five root paths in bash, and the row named
   `tools/check-layout-guards.mjs`, which now exists and gathers that loop rather than staying beside it. One line of
   this record changed rather than only aged: `docs/adr/0018-...` ## Verification, which §13's earlier readings cited at
   `:121`, was rewritten by that closure into a `Verified: YES`, and the file keeps its 121 lines so the citation this
   section no longer needs still resolves. The same reading applies to the sentences in `docs/architecture/20-target-repository-layout.md` §7 and §8
   whose deferral explains that no checker or guard exists yet for the import boundary or the bare root: the scan is a
   step of that workflow and green on a runner, and the §8 sentence went `Verified: YES` with the guard; §7's stays
   deferred for the different reason the row gives — an empty `nexora/**`, which a scan over nothing cannot close.

Trace: ADR-0001 · ADR-0002 · ADR-0003 · ADR-0004 · ADR-0005 · ADR-0006 · ADR-0008 · ADR-0009 · ADR-0010 ·
ADR-0011 · ADR-0012 · ADR-0013 · ADR-0014 · ADR-0015 · ADR-0016 · ADR-0017 · ADR-0018 · ADR-0019 · ADR-0020 ·
docs/PROJECT-STATE.md §0 constraints 7, 8, 10, 11, 13 and §9 · Contract:
`packages/schema/verification.json`, `packages/schema/scripts/verify.mjs`,
`packages/schema/compatibility.json#/matrix`, `packages/schema/protocol/framing.json`,
`packages/schema/protocol/error-taxonomy.json`, `tools/check-no-secrets.mjs`, `tools/check-doc-evidence.mjs` ·
See also: `docs/verification/live-matrix.md`, `docs/verification/live-env-contract.md`,
`docs/architecture/20-target-repository-layout.md`, `docs/architecture/38-compatibility-matrix.md`,
`docs/architecture/40-open-decisions.md`, `docs/audit/00-scope-and-method.md`
