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
| `connector/internal/peer/`, `connector/internal/dispatch/`, `connector/internal/hello/` and the codec's boundary | written and measured on this host 2026-11-05 — `docs/PROJECT-STATE.md` §4.7, §4.8, §4.9 and §4.10: the heartbeat clause the peer declines, the queue row that stays open because of it, the three sender-policy rows a unit test cannot close, the first question two of this repository's own packages raised against each other, and the three pieces of contract data that answered it |
| `connector/internal/chunk/` | **not written — and not for want of code.** §4.11 read the chunk contract to start it and found the slice metadata complete, asserted green, and the slice body absent: no field in `packages/schema` carries a chunk's bytes, and the recorded fixture group takes no side between the readings that survive. Registered as `VER-76` and `ODEC-34`, asked as §9's ninth row, deferred from §5 of `docs/architecture/25-native-messaging-protocol.md` |
| `connector/internal/compile/` | the serializer and the hash, and nothing else — stages 5 and 6 of `docs/architecture/27-config-compilation-pipeline.md` §2, written and measured on this host 2026-11-05 (`docs/PROJECT-STATE.md` §4.15): keys sorted at every depth, arrays never reordered, an integer or a refusal naming the key, sha256 over the emitted bytes. Stages 1-4 have no code and stage 7 has no probe, so there is no compiled config and no golden file yet — `VER-28` and `VER-78` stay open, and which byte string `configHash` covers is `ODEC-36` rather than a choice this package made alone |
| `connector/cmd/nexora-connector/` | written, measured and **run** on this host 2026-11-05 — `docs/PROJECT-STATE.md` §4.13: one process, two transports, the negotiation and the two version strings answered, every other published method left unregistered so the front door answers it `Internal`, and no frame served to a sender nobody resolved. The line beside it from that run is §4.13's, not a fix smuggled into a slice about a binary |
| `connector/cmd/nexora-host/` | **not written — the queue's one item stopped by a decision rather than by a dependency.** §4.14 registers the crossing it must make as `ODEC-35` (an `adr`, three surviving shapes, one already excluded by ADR-0004 rule 3) and `VER-77`, because no file in `packages/schema` gives the local pipe the separately versioned message schema `docs/adr/0005-two-process-connector.md` rule 2 promises it |
| Anything in `nexora/`, `packages/{protocol,state,i18n,ui}` | **not in this iteration** |
