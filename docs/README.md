# NEXORA documentation

NEXORA is the ongoing transformation of **ZeroOmega v3.5.2** (a SwitchyOmega fork) into a browser extension
plus a local **Go Connector** that runs **Xray Core** (VLESS / REALITY / TLS / Vision over tcp, ws, gRPC,
XHTTP). This tree contains the work of **Phase 0 (audit)**, **Phase 1 (architecture)** and the initial
**Phase 2 Connector implementation** indexed below. The legacy CoffeeScript projects are deliberately left in place
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
| `tools/check-doc-evidence.mjs` | Fails if a citation in `docs/` no longer means what it says: a `path:line` that is out of range or lands on a blank line, a `path#/pointer` that does not resolve, a named document that does not exist, a line cite into a file that is rewritten in place, or a quoted harness label that is not a check the harness has |

## Conventions these documents follow

- **English** for everything in the repository; the user-facing discussion language is Russian.
- **Every factual claim about the legacy code cites `path:line`.** `node tools/check-doc-evidence.mjs`
  enforces that the citation still resolves; a stale citation is treated as a defect, not as trivia.
- **The cite kind follows the mutability of the target.** Frozen legacy code takes `path:line`; contract
  data takes `path#/json/pointer`, because a pointer survives every edit; a file that is rewritten as the
  docs grow takes neither. `tools/check-doc-evidence.mjs` is cited by a *named constant* (`CITATION_RE`,
  `FUTURE_PREFIX`, `PLANNED_DOCS`, `blankTarget`), `docs/PROJECT-STATE.md` and
  `docs/audit/99-baseline-report.md` by *section* (`docs/PROJECT-STATE.md` §n), and
  `packages/schema/scripts/verify.mjs` by *assertion label*. A harness citation is the file token and then
  one or more double-quoted labels exactly as the harness carries them — template placeholders included,
  `packages/schema/scripts/verify.mjs` "the command surface is exactly nineteen methods",
  "secret-bearing namespace is not sync-eligible: ${ns.entity}" — one label per line, never split across a
  line break or a table cell. `STALE LABEL` in the checker reads the harness and refuses a citation that
  names no check, which is why a rule added in the middle of the verifier can no longer break the docs that
  depend on it: 52 such citations across 14 documents were converted this way on 2026-11-06, and the file
  joined `MUTABLE_CITATION_TARGETS` in the same commit so a new line cite into it is a red run rather than
  a recommendation.
- **A section cite names a section that exists.** `tools/check-doc-evidence.mjs` resolves four citation forms, and
  no pass opens a section number against the headings of the file it names: `Verified: NO — deferred to Phase 2`
  until `VER-83`'s pass lands. 568 cites of this form were counted on 2026-11-07, 8 of them named a section their
  document does not have, and all 8 were re-aimed by hand — `docs/PROJECT-STATE.md` §4.33 is the record.
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
| `docs/adr/0001-0021` | done |
| `docs/architecture/20-40` | done — 39 is the queue of every claim nothing checks, 40 is the register of the questions 39 cannot close |
| `docs/verification/*` | done — live matrix, env contract, both 3.5.2 backups, and the README that indexes what runs |
| `verify.yml` + baseline fixes | done — written and executed step-by-step on this host, **and run on GitHub runners since 2026-11-05; the ten runs §12 tabulates are three red, six times green, once cancelled** (`docs/audit/99-baseline-report.md` §12). The `VER-*` rows it names keep the states the queue gives them; a green run is evidence, and closing a row is a separate mechanical commit, for the reason §12 states |
| `probe-4` + `probe-5`, the end-to-end re-verification | done 2026-11-05 — the Ladder A chain, both test floors, all five gates, the artifact content and a cold Bower install, all re-measured and archived in `docs/audit/raw/` (`docs/audit/99-baseline-report.md` §11); the runner half is now recorded as run (§12), and the browser half as still not done, not as passed |
| `connector/` — M1: the contract projection, the frame codec, the envelope | done 2026-11-05 (`1004c4e`) — `connector/internal/{contract,contractgen,envelope,frames}`, and the workflow's `connector` job, previously pinned off, executes on both images |
| `connector/internal/peer/`, `connector/internal/dispatch/`, `connector/internal/hello/` and the codec's boundary | written and measured on this host 2026-11-05 — `docs/PROJECT-STATE.md` §4.7, §4.8, §4.9 and §4.10: the heartbeat clause the peer declines, the queue row that stays open because of it, the three sender-policy rows a unit test cannot close, the first question two of this repository's own packages raised against each other, and the three pieces of contract data that answered it |
| `connector/internal/chunk/` | **not written — and not for want of code.** §4.11 read the chunk contract to start it and found the slice metadata complete, asserted green, and the slice body absent: no field in `packages/schema` carries a chunk's bytes, and the recorded fixture group takes no side between the readings that survive. Registered as `VER-76` and `ODEC-34`, asked as §9's ninth row, deferred from §5 of `docs/architecture/25-native-messaging-protocol.md` |
| `connector/internal/compile/` | stages 1–6 plus stage 7's schema-and-bindout checks, with **four** compared goldens (`docs/architecture/27-config-compilation-pipeline.md` §3). The evidence continuation in `docs/architecture/39-verification-matrix.md` §13 records corrected `policy.levels: {}` and VLESS `vnext[].users[]`, referenced `direct`/`freedom` and `block`/`blackhole` outbounds, array routing selectors, SOCKS5 per-tag inbounds and last-priority per-tag fallbacks after authored rules. Enabled sniffing is an inbound sibling with `protocols` → `destOverride`; only `http`, `tls`, `quic`, `fakedns` and `routeOnly: true` are admitted. The four actual compiled → `apply.Write` configs were accepted by Xray 24.11.11; this is config acceptance, not runtime routing evidence, and `ODEC-37` remains open. Shape C's `headersRef` and the hash subject remain gated by `ODEC-36`; fail-closed belongs in `chrome.proxy`, not a separate Xray golden |
| `connector/internal/apply/` | written and measured on this host 2026-11-06 — `docs/PROJECT-STATE.md` §4.18: the keystore read the compiler refuses to do, then the one place a secret is plaintext on disk (`docs/architecture/32-secrets-keystore-masking.md` §13). It walks for the reference grammar rather than for key names, re-emits through `compile.Emit` so the determinism rule stays in one place, reports both hashes ODEC-36 chooses between instead of choosing, and writes temp-sync-chmod-rename so a document with a missing secret leaves the previous config byte-identical (`VER-29`'s measurable half). The command surface that calls it is `docs/PROJECT-STATE.md` §4.20; `connector/internal/apply/drive_test.go` now checks all four freshly compiled, materialized goldens with Xray 24.11.11 `run -test -c`, accepting each and rejecting the invalid-port control. This does not establish runtime routing or rollback under a running Core, and shape C's header map still stops at ODEC-36 |
| `connector/internal/keystore/` | written and measured on this host 2026-11-06 — `docs/PROJECT-STATE.md` §4.17: the `keystore://` grammar projected from `packages/schema/envelope-keys.schema.json` rather than copied, the store over one `Protector` seam, DPAPI reached through `crypt32.dll` with no dependency added, and the three refusals of `docs/architecture/32-secrets-keystore-masking.md` §11 mapped onto the projected taxonomy. It mints no reference, masks nothing, purges nothing by scope; the command surface that opens it is `docs/PROJECT-STATE.md` §4.20 |
| `connector/internal/supervisor/` | written and measured on this host 2026-11-06 — `docs/PROJECT-STATE.md` §4.19: the Xray child process itself — spawned with the config path on its command line, placed in the containment scope the platform is *asked* for and then reports rather than assumes (`docs/adr/0005-two-process-connector.md` rule 5, proven by a test that kills the scope and watches the child die), restarted by doc 30 §4's ladder in doc 30 §4's order (block first, attempts after), and recorded in the applied-state file whose four fields outlive the process that wrote them. What it refuses is in the same register: an exit is classified by code and signal only, because no file states Xray's numbers; a `contained: false` is a report, not a refusal to start; and the budget across consecutive deaths is `ODEC-38`, not a guess. The command surface that calls it is `docs/PROJECT-STATE.md` §4.20, no real Core has been spawned, and the host that would hold the job is still unwritten |
| `connector/cmd/nexora-connector/` | written, measured and **run** on this host 2026-11-05 — `docs/PROJECT-STATE.md` §4.13: one process, two transports, the negotiation and the two version strings answered, every other published method left unregistered so the front door answers it `Internal`, and no frame served to a sender nobody resolved. The line beside it from that run is §4.13's, not a fix smuggled into a slice about a binary. §4.20 registers the second half of the same directory: `config.apply` is a command now, so compile, keystore read, materialize-and-rename and the supervisor's spawn are one path instead of three tested neighbours |
| `connector/cmd/nexora-host/` | **not written — the queue's one item stopped by a decision rather than by a dependency.** §4.14 registers the crossing it must make as `ODEC-35` (an `adr`, three surviving shapes, one already excluded by ADR-0004 rule 3) and `VER-77`, because no file in `packages/schema` gives the local pipe the separately versioned message schema `docs/adr/0005-two-process-connector.md` rule 2 promises it |
| Anything in `nexora/`, `packages/{protocol,state,i18n,ui}` | **not in this iteration** |
