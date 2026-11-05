# verification — how a NEXORA claim stops being a claim

> Purpose: this directory is where the rest of the corpus is paid. `docs/architecture/39-verification-matrix.md`
> queues what nothing checks; `docs/verification/live-matrix.md` turns those into rows a person can execute;
> `docs/verification/live-env-contract.md` says what the runner is allowed to read. Without this README those three
> are documents nobody can act on: the reader has no list of what already runs, no command for it, and no rule for
> what counts as an answer. `docs/adr/0020-test-strategy.md` requires the tiers this file indexes;
> `docs/verification/live-matrix.md` row LIVE-14 and `docs/verification/live-env-contract.md` §4 both name §3 below as
> the command that makes their claim executable; `docs/architecture/39-verification-matrix.md` inherits its `LIVE-NN`
> rows from here and refuses to invent them.

---

## 1. What is in this directory

| File | What it is | Who treats it as binding |
|---|---|---|
| `live-matrix.md` | 24 rows, each an assertion, the mechanism that must exist to check it, whether it is automatable, its owner, and its status. | `docs/adr/0020-test-strategy.md`; `packages/schema/verification.json#/live` inherits all 24 by id and refuses a row that is not there |
| `live-env-contract.md` | The 15 variables a live run may read, which row each gates, and the rule that a missing variable means `SKIPPED` and never `PASSED`. | every `LIVE-02…LIVE-09` row; `.env.example` is the tracked template |
| `backup-from-3.5.2.json` | An envelope in the shape the 3.5.2 exporter writes — a fixed proxy with its bypass list and a two-rule switch profile — carrying example hosts and no credential. | LIVE-14; `docs/audit/12-what-must-not-change.md` §5; the migration contract in `packages/schema/envelope-backup.schema.json` |
| `backup-3.5.2-all-profile-types.json` | The same envelope shape with one profile of every type the legacy registry defines, 11 rules covering each condition family, and the implicit `__ruleListOf_` list a switch profile creates. | LIVE-14; `packages/schema/server.schema.json`'s negative cases; `docs/audit/09-subscriptions-import-export-sync.md` |
| `logs/` | Created by the first live run, not before: promotion rule 1 of the matrix requires raw output archived there. | `live-matrix.md` promotion rules |

Both `.json` files are load-bearing in a way a fixture usually is not: they are the input the migration in
`docs/architecture/35-schema-versioning-migration.md` must reproduce byte-for-byte, so their value is the *shape*, and
any edit that makes them tidier makes the migration test weaker. Their provenance is stated once, plainly: no browser
was run in this iteration, so they were written to the exporter's documented shape (`omega-web/src/partials/io.jade`
and `omega-target/src/options.coffee`'s `reset` path) with example hosts and no credential. A genuine `.bak` from a real
3.5.2 install replaces them at `LIVE-14`'s browser half — the shape is what command 2 below proves, not the export.

---

## 2. Two vocabularies, deliberately not merged

- **In this directory**: `SKIPPED`, `NO`, `PARTIAL`, `YES` — the state of an *execution*. `SKIPPED` means the
  environment was incomplete, `PARTIAL` means a Phase-1 artefact checks part of the row statically, `YES` means an
  executed check with its evidence attached. Definitions: `live-matrix.md` legend and `live-env-contract.md` §1.
- **In the queue**: `no-code`, `no-check`, `no-run`, `no-fixture`, `needs-human` — the state of an *obligation*, i.e.
  which kind of work is missing. Definitions: `packages/schema/verification.json#/states`.

They are not synonyms and a row cannot be promoted by changing its word. The bridge is one sentence of the queue's
own contract: `live-matrix.md` promotion rule 4 requires that a deferred row appear in
`docs/architecture/39-verification-matrix.md`, so a `NO` here is always an obligation there, never a rumour.

---

## 3. What runs today

Seven commands, all offline, all re-runnable, all of which print their own counts. Nothing here asks for a browser,
a server or a connector; the counts below are what the current tree prints, and the printouts are the point — the
corpus cites labels, not numbers.

| # | Command | What it proves | Which rows it serves |
|---|---|---|---|
| 1 | `npm --prefix packages/schema run verify` | The contract layer is self-consistent: every schema compiles, every command document conforms, valid examples validate and invalid ones are refused, and every recorded protocol frame replays through the framing rules. It also runs the assertions that §11 of `docs/architecture/39-verification-matrix.md` and §9 of the open-decisions register name. | the static half of `LIVE-01`, `LIVE-10`, `LIVE-16`…`LIVE-24`; every `VER-NN` row whose mechanism is a schema or a fixture |
| 2 | `node tools/check-legacy-backup.mjs` | Both shipped 3.5.2 envelopes load in the **real** legacy engine (`omega-pac/index.js`), every profile the engine finds compiles to a PAC body — `+system` excepted, because the engine itself refuses it — every body is valid JavaScript that answers each sample request, and every answer that names a profile names one present in the same envelope. | `LIVE-14`, the compile half |
| 3 | `node tools/check-legacy-backup.mjs --selftest` | Command 2 is not vacuous: four deliberately broken envelopes (a dangling profile reference, a key the loader cannot see, an unknown profile type, a missing `schemaVersion`) each have to be rejected. | the credibility of `LIVE-14` |
| 4 | `node tools/check-no-secrets.mjs` | No secret-shaped content is committed anywhere in the tracked tree; `--selftest` proves the scanner still fires on positives and stays quiet on references. | `LIVE-10`, offline half |
| 5 | `node tools/check-doc-evidence.mjs` | Every citation in `docs/` resolves: `path:line` lands on a non-blank line of a file that exists, `path#/pointer` lands inside a JSON document that parses, a document name is a document that exists. A citation into a file that is rewritten in place —
`docs/PROJECT-STATE.md` — must be a section reference. | the honesty of every claim in this directory that cites evidence |
| 6 | `node tools/verify-vendor.mjs` | The legacy options page's referenced vendor assets resolve on disk, so a green build is not shipping a page that renders blank. | the baseline, not a NEXORA claim |
| 7 | `npm --prefix omega-pac test`, `npm --prefix omega-target test` | The legacy test floor, pinned as a regression baseline: `omega-pac` 142 passing / 1 failing, `omega-target` 8 passing / 5 failing. The 6 failures predate this branch and are recorded in `docs/audit/99-baseline-report.md`; the failing SOCKS5 `pacResult` case is a Phase-2 blocking requirement because `ConnectorProfile.compile` depends on that path. | the promise that migration does not silently break the engine that reads these fixtures |

Command 2 needs `omega-pac/index.js`, the browserified engine the `omega-pac` build produces; it is a build output, so
it is not committed, and the command says so and exits non-zero with the build step instead of passing quietly. That
is the difference between "not applicable" and "not run".

All seven also run unattended: `.github/workflows/verify.yml` executes them on every push to `master` or `nexora/**`
and on every pull request targeting `master`, and adds the guards only a machine can hold — the `VER-NN` rows it names
in its step titles, plus the artifact inspection that `docs/audit/99-baseline-report.md` §6 turned into a rule (a build
counts as green only when the zip is shown to carry 353 entries, 252 `lib/` files and `js/omega_pac.min.js` inside it,
never from an exit code). It has now run there: three executions of it on 2026-11-05, the first two red on defects
in the workflow — a commit label its own source document had never declared, a Windows `$GITHUB_WORKSPACE` that is
not a POSIX path, and a byte total that was one checkout's property rather than a floor — and the third green across
all six jobs, recorded line by line in `docs/audit/99-baseline-report.md` §12. Before those runs, each `run:` block
had been executed on the development host and re-run against a synthetic violation to prove the guard can fail —
`docs/audit/02-toolchain-decay-and-risk.md` §7.3 — which was a claim about shell logic, and the runs are what turned
it into a claim about CI. What the green run is still not: a `YES` for any live row, because no browser was opened,
and not a state change in the queue, because `packages/schema/verification.json` has no state meaning closed and
closing a row means deleting the labelled line it is anchored to. That bookkeeping is `docs/architecture/39-verification-matrix.md` §8's
first task in Phase 2, not a status edit in a document.

---

## 4. What does not run, and what would have to exist first

The honest list, in the order it unblocks:

1. **`connector/`** — Go, not installed on the development host (`docs/audit/00-scope-and-method.md` §2). Until it
   exists, `LIVE-01`…`LIVE-09`, `LIVE-13`, `LIVE-16`…`LIVE-22` have no mechanism to execute against.
2. **`nexora/extension/`** — the MV3 client. `LIVE-11`, `LIVE-12`, `LIVE-14`'s import half, `LIVE-15`, `LIVE-19`'s
   visible-degradation half need a page that can be loaded.
3. **A real Xray binary under the pin** — `LIVE-02`, `LIVE-20`, `LIVE-21`. `packages/schema/pins.schema.json` already
   refuses a pin that would allow a silent self-update; refusing a *downloaded* one is a runtime act.
4. **A VLESS/REALITY test server and its environment** — `live-env-contract.md` §2. The server exists; the variables
   are unset in this iteration, which under rule 1 means `SKIPPED`.
5. **Browsers and stores** — `LIVE-15` and anything that touches CWS/Edge/AMO behaviour. No amount of static checking
   substitutes for them; those rows are `needs-human` in the queue for that reason.

`LIVE-04` is the gate in the middle of that list: the matrix's promotion rule 2 makes egress proof a precondition of
reporting any transport row, because a TCP connect that turns green while traffic leaks is worse than no test at all.

---

## 5. Running a live row when its artefacts exist

In this order, because each step can invalidate the previous answer:

1. Copy `.env.example` to `.env`, fill the variables the row names in `live-env-contract.md` §2, and never commit it.
2. Run `LIVE-04` first. If the observed egress IP is not `NEXORA_EXPECTED_EGRESS_IP`, no transport row may be
   reported at all — not as `NO`, not as `PARTIAL`.
3. Execute the row's mechanism, exactly as the row describes it, and capture the raw output.
4. Append a line to the row's `Status` cell: date, host, connector and Core versions, the command, and the path of the
   archived output under `logs/`. The status changes only with that evidence present.
5. If the row was deferred in `docs/architecture/39-verification-matrix.md`, close the row there in the same change —
   the queue's partition assertion fails a corpus that gained a `YES` while still claiming the obligation.
6. A row that later regresses goes back to `NO` with the failing log attached. It is never re-labelled `SKIPPED`,
   which is the word for "we did not ask", not for "it broke".

---

## 6. Rules for this directory

- A row is an assertion about the world, not a task. "Implement the keystore" is `PROJECT-STATE` work-order material;
  "a secret survives a round-trip and dies on command" is `LIVE-18`.
- No row may claim a feature works because a document says it should. `PARTIAL` is available and honest; `YES` is not
  negotiable on prose.
- Nothing in here stores a credential. `.env` is ignored, fixtures carry example hosts, and `tools/check-no-secrets.mjs`
  runs over this directory like everywhere else.
- This directory adds no architecture. If a row turns out to require a decision, that is an entry in the
  open-decisions register and the row says which `ODEC-NN`; if it requires a contract change, that is a schema in
  `packages/schema` and the row cites its pointer.
- A file here that nothing cites is a note, not a verification artefact. Every document above is named by at least one
  obligation elsewhere in the corpus, and its Purpose blockquote lists who.

See also: `docs/README.md`, `docs/PROJECT-STATE.md` §5 and §6, `docs/adr/0020-test-strategy.md`,
`docs/architecture/39-verification-matrix.md`.
