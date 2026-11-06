# ADR-0001: Record every decision as an append-only ADR that names its own check

Status: Accepted
Date: 2026-11-04

## Context

The decisions that shape NEXORA were agreed in two pre-planning rounds and in a ~1000-line master prompt.
Where they live today is a prose list in `docs/PROJECT-STATE.md` §8 and the work order in
`docs/PROJECT-STATE.md` §6. A list of twelve bullets has no address, no date, no status and no owner, so
it cannot be cited by a design document, a contract or a test.

Phase 0 fixed an evidence standard for the *audit* half of this branch (see
`docs/audit/00-scope-and-method.md` §4): every claim carries a repo-relative `path:line` citation and one of
`VERIFIED` / `INFERRED` / `UNVERIFIED`. It also built the mechanical half of that standard —
`tools/check-doc-evidence.mjs` extracts every citation from `docs/**` (`CITATION_RE`) and fails on any citation
landing on a blank line (`blankTarget`), because "a citation that drifts is worse than no citation". No equivalent exists
for decisions, and three concrete failure modes follow: a decision that nothing checks; a decision silently
reversed by editing the document that recorded it; and a decision restated differently in the architecture
document, the JSON contract and the code that reads it.

This ADR exists before the other nine because their format is not negotiable afterwards.

## Decision

1. Every architectural decision MUST be recorded as one file in `docs/adr/` named `NNNN-<kebab-slug>.md`,
   holding exactly one decision.
2. Numbering MUST be four-digit, sequential and append-only. A number, once issued, MUST NOT be reused,
   renumbered or recycled after a rejection.
3. Each file MUST use this section sequence and no other sections:
   `## Context`, `## Decision`, `## Consequences`, `## Alternatives considered`, `## Evidence`,
   `## Verification`, preceded by a `Status:` line and a `Date:` line.
4. `Status:` MUST be one of `Accepted`, `Proposed`, `Superseded by ADR-NNNN`.
5. A decision MUST be superseded only by writing a NEW ADR that names the old one. The old file's Context,
   Decision, Consequences, Alternatives and Evidence MUST NOT be edited — history is not corrected in place;
   the only permitted edit to a superseded file is its `Status:` line.
6. Normative words `MUST`, `MUST NOT`, `SHOULD`, `MAY` MUST be used with their RFC 2119 meaning, MUST be
   written in all caps when they carry that meaning, and MUST appear only inside `## Decision`. `## Context` is
   measurement, `## Consequences` is consequence. The single exception is a verbatim quotation from a contract
   or another ADR, which MUST be shown inside backticks or quotation marks as the quotation it is.
7. `## Evidence` MUST tag each citation `VERIFIED`, `INFERRED` or `UNVERIFIED`. A `path:line` citation MUST
   be repo-relative and MUST have been opened by the author. Files that do not exist yet
   (`docs/architecture/**`, `connector/**`, `nexora/**`, `packages/protocol/**`) MAY be referenced by name
   without a colon and line number, and MUST NOT be cited with a line number.
8. `## Alternatives considered` MUST list the real alternatives with the reason each was rejected. An
   alternative that was never plausible MUST NOT appear there as a strawman.
9. Every ADR MUST name in `## Verification` the script, contract or assertion that checks it today. Where
   nothing checks it, the file MUST contain the literal line `Verified: NO — deferred to Phase 2`.
10. `docs/adr/README.md` (authored separately, later) MUST index every file in this directory, and that
    index MUST be treated as a checkable artifact, not a convenience.
11. Each file MUST be between 60 and 140 lines. Longer means two decisions; shorter means the alternatives
    were not recorded honestly.

## Consequences

Cheaper: a design document or a contract can cite `ADR-0006` and inherit its evidence; a reviewer can tell
whether a change reopens a settled question without re-reading the chat log; the `path:line` discipline makes
the existing citation checker cover decisions as well as findings.

Harder: writing an ADR now costs a verification section, which forces the author to either point at a real
check or admit `Verified: NO` in public; append-only numbering means near-duplicate decisions must be
explicitly linked rather than quietly merged.

Forbidden for this iteration: editing an existing ADR to change its decision; citing a not-yet-written file
with a line number; recording a decision anywhere except `docs/adr/` (`docs/PROJECT-STATE.md` remains a
continuation log and a pointer, not the source of a decision).

## Alternatives considered

- **One `decisions.md` shared by every decision, a table row each.** Rejected: a row cannot carry six sections, and
  any edit to the shared file destroys the diff that shows which decision moved and when.
- **Edit decisions in place as living documents (RFC style).** Rejected: it is exactly the silent-reversal
  failure mode this ADR exists to prevent, and it makes "what did we agree in November?" unanswerable.
- **An ADR tool (adr-log, log4brains, MADR CLI).** Rejected: it adds a Node dependency to a repository that
  deliberately has no root `package.json` (ADR-0002, ADR-0018), and the only check we already run against
  prose — citation resolution — is 100 lines of plain script in `tools/`.
- **Decisions recorded as issues or in the master prompt.** Rejected: neither is in the tree a reviewer
  clones, and neither survives a merge conflict.

## Evidence

- `tools/check-doc-evidence.mjs` — VERIFIED — `CITATION_RE` is the extractor whose rules section 7 must respect,
  and `blankTarget` is the rejection this practice inherits. Neither is cited by line number on purpose: this
  file changes every time a citation kind is added, and a line cite into it is the drift this ADR forbids.
- `docs/PROJECT-STATE.md` §6 — VERIFIED — the work order that fixes this format before the other nine.
- `packages/schema/README.md:51` — VERIFIED — precedent that a decision, a contract and a document must name
  the same set or the build fails; rule 9 generalises that precedent to prose.
- `docs/audit/00-scope-and-method.md` — VERIFIED (read in full) — the evidence standard adopted in rule 7.
- The twelve prose decisions of `docs/PROJECT-STATE.md` §8 being the current, unaddressable record —
  VERIFIED by reading that section.

## Verification

Checks running today: `node tools/check-doc-evidence.mjs` validates rules 7's citation half across
`docs/**`; `npm --prefix packages/schema run verify` (1 222 checks) is the model for rule 9's "name the check".
`node tools/check-adr-shape.mjs` checks this rule set over the directory: rule 1's filename shape, rule 2's
sequential append-only numbering with no repeat, rule 3's one H1 and the six-section sequence, rule 4's status
vocabulary, rule 10's index parity including the Status and Date cells, and rule 11's 60-140 line bound. The
workflow runs it as its `VER-01` step, after that script's own `--selftest` injects one defect per rule and
refuses each — the standard `tools/check-no-secrets.mjs` is held to.

Verified: YES — rules 1-4, 10 and 11 have a checker; `VER-01` closed on 2026-11-06 and is in
`packages/schema/verification.json#/retired`. Two facts this record cannot itself prove stay with the runner: the
same checks ran there as inline shell on every image since 2026-11-05, and the script that now carries them has
not yet been executed by a pipeline, because a push to this repository is confirmed each time and none was given.
