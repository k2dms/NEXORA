# NEXORA documentation

NEXORA is the ongoing transformation of **ZeroOmega v3.5.2** (a SwitchyOmega fork) into a browser extension
plus a local **Go Connector** that runs **Xray Core** (VLESS / REALITY / TLS / Vision over tcp, ws, gRPC,
XHTTP). This tree contains the work of **Phase 0 (audit)** and **Phase 1 (architecture)** only: no product
code of the new system has been written, and the legacy CoffeeScript projects are deliberately left in place
(ADR-0002).

Read in this order:

| Order | Path | What it is |
|---|---|---|
| 1 | `PROJECT-STATE.md` | **Start here if you are continuing this work.** Assignment, measured facts that falsified the original plan, every decision already taken, full on-disk inventory, exact next steps, git rules |
| 2 | `audit/00-scope-and-method.md` | What was audited, on what host, and the evidence standard (`VERIFIED` / `INFERRED` / `UNVERIFIED` + `path:line` citations) |
| 3 | `audit/01…12` | The legacy system as it actually is: build graph, toolchain decay, runtime layering, data model, storage, proxy application, routing engine, UI/i18n, subscriptions & sync, manifests, security findings, load-bearing behaviour |
| 4 | `audit/99-baseline-report.md` | The green-build probe: what the legacy chain does on Node 24, the pinned test floor, artifact assertions, and the explicit "not fixed" list |
| 5 | `adr/0001…0020` | The decisions, one per file, append-only |
| 6 | `architecture/20…40` | The target design, normative language (`MUST` / `MUST NOT` / `SHOULD` / `MAY`), each document ending with `Trace:` and `Contract:` lines |
| 7 | `verification/` | How every claim above is or will be checked, including the env-gated live matrix `LIVE-01…24` |

Executable artefacts, outside this directory:

| Path | What it does |
|---|---|
| `packages/schema/` | The contracts as data — JSON Schema for entities, the Native Messaging envelope, the closed command allowlist with one document per command, error taxonomy, fixtures of recorded protocol frames. `npm --prefix packages/schema run verify` is the green test of this iteration |
| `tools/verify-vendor.mjs` | Fails if a `lib/…` reference in the legacy UI sources has no file under `omega-web/lib/` |
| `tools/check-no-secrets.mjs` | Scans the tree for secret-shaped strings; `--selftest` proves the scanner itself works |
| `tools/check-doc-evidence.mjs` | Fails if any `path:line` citation in `docs/` no longer points at a real, non-blank line |

## Conventions these documents follow

- **English** for everything in the repository; the user-facing discussion language is Russian.
- **Every factual claim about the legacy code cites `path:line`.** `node tools/check-doc-evidence.mjs`
  enforces that the citation still resolves; a stale citation is treated as a defect, not as trivia.
- **The cite kind follows the mutability of the target.** Frozen legacy code takes `path:line`; contract
  data takes `path#/json/pointer`, because a pointer survives every edit; a tool that is edited as the
  docs grow (`tools/check-doc-evidence.mjs`, `packages/schema/scripts/verify.mjs`) is cited by a *named*
  constant or assertion label, never by a line number — its line numbers move whenever a rule is added,
  and the checker rejects self-cites for exactly that reason. New sections in the verifier go above its
  report block, never between two existing sections: a mid-file insertion shifts every `verify.mjs:` cite
  below it, and two had already drifted that way before this rule was written. The same rule now covers
  `docs/PROJECT-STATE.md`, which is rewritten in place as the handoff log, and `docs/audit/99-baseline-report.md`,
  which every probe round appends to: cite them as `docs/PROJECT-STATE.md` §n and
  `docs/audit/99-baseline-report.md` §n, and the checker rejects a line cite into any of them.
- **Every design claim names its check.** If nothing checks it, it is recorded in
  `architecture/39-verification-matrix.md` with `Verified: NO — deferred to Phase 2` rather than asserted.
- **A question a check cannot close is a decision, not a deferral.** If the missing artefact is an answer a human
  owes — a product choice, a contract addition, a measurement, an upstream fact — it is registered in
  `architecture/40-open-decisions.md` and the row in 39 names the register entry that gates it.
- **Decisions are ADRs, not prose in design docs.** A design document may cite an ADR; it may not silently
  make one.
- **No secrets, no binaries, no invented protocol.** Credentials are keystore references, Xray is a pinned
  download, and browser↔connector traffic is Native Messaging only.
- **Version numbering is constrained** by the upgrade behaviour of the existing extension (ADR-0015); the
  NEXORA line continues ZeroOmega's numbers rather than restarting them.

## Status of this branch

| Deliverable | Status |
|---|---|
| Infrastructure (`.gitattributes`, `.env.example`, `tools/*`, `.npmrc` ×5) | done |
| `packages/schema` contracts + verifier | done, green |
| `docs/audit/00-12 + 99` | done (99-baseline-report written after the Step-0 probe) |
| `docs/adr/0001-0020` | done |
| `docs/architecture/20-40` | done — 39 is the queue of every claim nothing checks, 40 is the register of the questions 39 cannot close |
| `docs/verification/*` | done — live matrix, env contract, both 3.5.2 backups, and the README that indexes what runs |
| `verify.yml` + baseline fixes | done — written and executed step-by-step on this host, **and run on GitHub runners since 2026-11-05; the eight runs §12 tabulates are twice red, five times green, once cancelled** (`docs/audit/99-baseline-report.md` §12). The `VER-*` rows it names keep the states the queue gives them; a green run is evidence, and closing a row is a separate mechanical commit, for the reason §12 states |
| `probe-4` + `probe-5`, the end-to-end re-verification | done 2026-11-05 — the Ladder A chain, both test floors, all five gates, the artifact content and a cold Bower install, all re-measured and archived in `docs/audit/raw/` (`docs/audit/99-baseline-report.md` §11); the runner half is now recorded as run (§12), and the browser half as still not done, not as passed |
| `connector/` — M1: the contract projection, the frame codec, the envelope | done 2026-11-05 (`1004c4e`) — `connector/internal/{contract,contractgen,envelope,frames}`, and the workflow's `connector` job, previously pinned off, executes on both images |
| `connector/internal/peer/` and `connector/internal/dispatch/` — M2's framing endpoint and its front door | written and measured on this host 2026-11-05 — `docs/PROJECT-STATE.md` §4.7 and §4.8, including the heartbeat clause the peer declines, the queue row that stays open because of it, and the three sender-policy rows a unit test cannot close |
| Anything in `nexora/`, `packages/{protocol,state,i18n,ui}` | **not in this iteration** |
