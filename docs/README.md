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
- **A section cite names a section that exists.** `tools/check-doc-evidence.mjs` resolves six citation forms, and
  the sixth is the section pass `VER-83` closed with: a backticked `docs/` path followed by `§N` must name a
  numbered heading of the file it cites — on the exact sub-number, so a §4 cite cannot be certified by the §8 the
  sentence meant — with the two mutable targets checked on the integer part only, because their subsections are
  dated ledger entries and table slots rather than headings. 568 cites of this form were counted on 2026-11-07, 8 of
  them named a section their document does not have, and all 8 were re-aimed by hand —
  `docs/PROJECT-STATE.md` §4.33 is the record; the run now re-reads every one of them.
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
| `connector/internal/chunk/` | written 2026-11-14: the reassembly half of doc 25 §5. `chunk.New` opens a group (chunkCount ≥ 2, sha256-hex hash), `Add` takes one base64 fragment per slice refusing duplicates, out-of-range sequences and non-base64 payloads, and `Bytes` reassembles in chunkSeq order, hashes the concatenation and returns the document only if it matches the declared `contentHash` — a mismatch is `ErrIntegrityMismatch`, never a partial apply. The contract it reads is the fragment model `ODEC-34` decided; the fixture-level assertion is the chunk block's "reassembled slice bodies hash to the declared contentHash" |
| `connector/internal/logstore/` | written 2026-11-14: the Connector's half of doc 36. `Store` renders once and hands every sink the same masked line with the same per-plane seq (one render, every sink); `Masker` is the value-keyed pass built from the keystore's values, refusing short and empty table entries; `Ring` is the memory sink capped by count and bytes, first cap wins, keeping a single oversized record, re-reading the masked line so it never holds the raw value; `FileSink` rotates oldest-first at 5 MiB × 5 and truncates at start when `retentionDays` is 0 — the current session only. Nine fake-sink tests pin the render identity, the bare-string mask (the legacy replacer's fatal gap), the seq gap detector and both caps; `VER-48`'s connector half retired with it |
| `connector/internal/compile/` | stages 1–6 plus stage 7's schema-and-bindout checks, with **five** compared goldens (`docs/architecture/27-config-compilation-pipeline.md` §3). The evidence continuation in `docs/architecture/39-verification-matrix.md` §13 records corrected `policy.levels: {}` and VLESS `vnext[].users[]`, referenced `direct`/`freedom` and `block`/`blackhole` outbounds, array routing selectors, SOCKS5 per-tag inbounds and last-priority per-tag fallbacks after authored rules. Enabled sniffing is an inbound sibling with `protocols` → `destOverride`; only `http`, `tls`, `quic`, `fakedns` and `routeOnly: true` are admitted. The five actual compiled → `apply.Write` configs were accepted by Xray 24.11.11; this is config acceptance, not runtime routing evidence, and `ODEC-37` remains open. Shape C's `headersRef` is materialized via `headersKeys` (ODEC-45); fail-closed belongs in `chrome.proxy`, not a separate Xray golden |
| `connector/internal/apply/` | written and measured on this host 2026-11-06 — `docs/PROJECT-STATE.md` §4.18: the keystore read the compiler refuses to do, then the one place a secret is plaintext on disk (`docs/architecture/32-secrets-keystore-masking.md` §13). It walks for the reference grammar rather than for key names, re-emits through `compile.Emit` so the determinism rule stays in one place, reports both hashes (ODEC-36: configHash is the handle-bearing digest), and writes temp-sync-chmod-rename so a document with a missing secret leaves the previous config byte-identical (`VER-29`'s measurable half). The command surface that calls it is `docs/PROJECT-STATE.md` §4.20; `connector/internal/apply/drive_test.go` now checks all five freshly compiled, materialized goldens with Xray 24.11.11 `run -test -c`, accepting each and rejecting the invalid-port control. This does not establish runtime routing or rollback under a running Core |
| `connector/internal/keystore/` | written and measured on this host 2026-11-06 — `docs/PROJECT-STATE.md` §4.17: the `keystore://` grammar projected from `packages/schema/envelope-keys.schema.json` rather than copied, the store over one `Protector` seam, DPAPI reached through `crypt32.dll` with no dependency added, and the three refusals of `docs/architecture/32-secrets-keystore-masking.md` §11 mapped onto the projected taxonomy. It mints no reference, masks nothing, purges nothing by scope; the command surface that opens it is `docs/PROJECT-STATE.md` §4.20 |
| `connector/internal/supervisor/` | written and measured on this host 2026-11-06 — `docs/PROJECT-STATE.md` §4.19: the Xray child process itself — spawned with the config path on its command line, placed in the containment scope the platform is *asked* for and then reports rather than assumes (`docs/adr/0005-two-process-connector.md` rule 5, proven by a test that kills the scope and watches the child die), restarted by doc 30 §4's ladder in doc 30 §4's order (block first, attempts after), and recorded in the applied-state file whose four fields outlive the process that wrote them. What it refuses is in the same register: an exit is classified by code and signal only, because no file states Xray's numbers; a `contained: false` is a report, not a refusal to start; and the budget across consecutive deaths is `ODEC-38`, not a guess. The command surface that calls it is `docs/PROJECT-STATE.md` §4.20, no real Core has been spawned, and the host that would hold the job is still unwritten |
| `connector/cmd/nexora-connector/` | written, measured and **run** on this host 2026-11-05 — `docs/PROJECT-STATE.md` §4.13: one process, two transports, the negotiation and the two version strings answered, every other published method left unregistered so the front door answers it `Internal`, and no frame served to a sender nobody resolved. The line beside it from that run is §4.13's, not a fix smuggled into a slice about a binary. §4.20 registers the second half of the same directory: `config.apply` is a command now, so compile, keystore read, materialize-and-rename and the supervisor's spawn are one path instead of three tested neighbours. A binary built from a checkout carries no product version and refuses to start under the same rule — the release stamps it, `go build -ldflags "-X main.releaseVersion=0.1.0" ./cmd/nexora-connector`, because the host spawns this process with only `--sender-id` and `--allowlisted` and nothing else to tell it what it answers for |
| `connector/cmd/nexora-host/` | written and measured on this host 2026-11-10 — `docs/PROJECT-STATE.md` §4.52: the four jobs ADR-0005 rule 1 gives the shim, and nothing else. The sender is resolved from the argv the browser itself hands the process (the measured shapes: Chrome's `chrome-extension://<id>`, Firefox's manifest path then add-on id), checked against the host's own on-disk allowlist, and an unallowlisted sender gets exactly one `UnauthorizedExtension` per frame with no Connector spawned and no state touched (`docs/adr/0004-native-messaging-only-no-loopback-http-control.md` rule 6) until the refusal guard closes the port. The admitted path is a verbatim byte pump under the two directional limits, and the Connector is spawned once with `--sender-id` — ADR-0022 rule 1's own mechanism, not a test affordance. What it deliberately is not: the lock file that would let a second port *adopt* rather than spawn (`ODEC-01`, and `VER-12`), the installer that registers the manifest, a job object holding the outer containment level (`VER-14`'s LIVE-13 half), the `packages/protocol` TypeScript codec, and the OS peer-credential hardening layer ADR-0022 rule 3 leaves optional |
| `packages/protocol/` | written and measured on this host 2026-11-10 — `docs/PROJECT-STATE.md` §4.53: the TypeScript projection of `packages/schema/protocol/` the layout row asked for. `src/contract.ts` is generated from the four contract files and byte-compared against a fresh generation, so the nineteen methods and the twenty-three codes arrive by projection and this package holds no list of its own; `src/frames.ts` is the frame codec with the two directional ceilings, refusing an oversized write before a byte leaves and a broken pipe after it. `connector/internal/frames/protocol_mirror_test.go` reads `packages/protocol/testdata/framed.jsonl` — 112 frames this codec actually wrote — and requires the Go codec to write those bytes and read them back. What it is not: the extension's client, the MV3 service worker that would import it, or a type-checked build — there is no `tsc` in this repository, so Node's own type stripping runs the sources and the harness compares bytes |
| Anything in `nexora/`, `packages/{state,i18n,ui}` | **not in this iteration** |
