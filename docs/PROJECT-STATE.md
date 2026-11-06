# NEXORA — project state and continuation log

> **Purpose of this file.** It is the handoff record for an interrupted engineering session. If you
> (a human, or an agent in another IDE such as Cursor) pick this repo up cold, read this file first:
> it contains the agreed scope, every decision already taken and *why*, the measured facts that
> falsified the original plan, the exact inventory of what exists on disk, and the precise next steps.
> Written 2026-11-04. English because it is the repository language.

Status of this branch: **Phase 0 (audit) + Phase 1 (architecture) in progress, committed and pushed. No
product code is written yet, and none should be in this iteration.** §7 records the commits and the ref.

---

## 0. The assignment (unchanged, still the whole job)

Source: `NEXORA — Engineering Master Prompt` (~1000 lines, Russian), preserved at
`C:\Users\dms\.qoder\tmp\E--NEXORA\attachments\bfca081c-72fb-458b-9463-7d98141ef826\9e13fce7-098f-4514-b006-1c7e151a5d20.txt`.

Turn **ZeroOmega v3.5.2** (a SwitchyOmega fork: 5 CoffeeScript projects, ~21k LOC) into **NEXORA**:
a browser extension plus a local **Go Connector** that runs **Xray Core** (VLESS / REALITY / TLS /
Vision over TCP, ws, gRPC, XHTTP), with subscriptions, per-server health, advanced routing and a new
UI. Production-grade, not a prototype.

Hard constraints from the prompt that keep biting (violating any of these is a rework):

| # | Constraint | Where it lands |
|---|---|---|
| 1 | Do **not** implement VLESS in JavaScript — protocols live in Xray behind the Go connector | ADR-0003; `22-component-contracts.md` |
| 2 | Do not invent a network protocol — browser↔connector is Native Messaging only | ADR-0004, ADR-0005 |
| 3 | No arbitrary shell/rpc pass-through: typed, allowlisted commands | `protocol/command-allowlist.json` (19 methods, closed) |
| 4 | Core inbounds bind **loopback only**, not configurable, asserted at `core.start` | ADR-0006 |
| 5 | No mass refactor of working ZeroOmega; do not rewrite what works | ADR-0002, `20-target-repository-layout.md` |
| 6 | No committed binaries | ADR-0019 |
| 7 | A failed subscription update must never delete the last good version | already true today (`docs/audit/09`), see §4.6 |
| 8 | A TCP check is not a proxy check — DNS / TCP / Core-outbound / proxied HTTP(S) are distinct layers | `server.test.layers[]` in `protocol/commands/server.test.json` |
| 9 | Secrets: minimal storage, masking, never in logs, no home-grown crypto | ADR-0010/0011, `32-secrets-keystore-masking.md` |
| 10 | Silent proxy bypass on Core death is forbidden (fail-closed default) | ADR-0009, `30-failure-policy.md` |
| 11 | A UI button must not imply a finished feature | `37-ui-architecture.md` parity matrix |
| 12 | No destructive operations without explicit agreement; never overwrite user changes | see §7 git rules |
| 13 | Backward compatibility of user data (ZeroOmega profiles survive) | ADR-0013/0014/0015, `38-compatibility-matrix.md` |

Agreed scope for **this** iteration (confirmed by the user in two pre-planning rounds):

- **Phase 0 audit + Phase 1 architecture only** (plus a green-build baseline to make architecture claims checkable).
- New UI layer: **Svelte 5 + Vite + TypeScript**, coexisting with legacy AngularJS until a parity-driven cut-over (ADR-0016).
- Versions: **continue the ZeroOmega line (3.6.x → 4.x)**, same extension ID, existing CWS/Edge/AMO listings (ADR-0015).
- Baseline strategy: **probe → ladder A/B/C**, no Grunt/Bower/CoffeeScript migration.
- Git: `origin` untouched (upstream ZeroOmega), new remote **`nexora` → git@github.com:k2dms/NEXORA.git**, work on branch **`nexora/architecture`**.
- The broken uncommitted edits and `vless-test.coffee` were **deleted** after saving a patch outside the repo (§7).
- A **real VLESS/REALITY test server** exists; access is via environment variables only, secrets never committed.

The approved plan is the authoritative work order:
`C:\Users\dms\.qoder\plans\dawn-delta-snipe.md` (sections §1 layout, §2 audit files, §3 architecture
files + ADRs + contracts, §4 ladder A/B/C, §5 UI, §6 verification, §7 risks, §8 git sequence).

---

## 1. Measured facts that changed the plan

These were probes, not assumptions. Each one removed work or added a requirement.

| # | Plan said | Reality (measured) | Consequence |
|---|---|---|---|
| 1 | Ladder B or C likely — Grunt 0.4 will die on Node 24 at module load (`glob@3.2`/`graceful-fs@2`) | **Grunt 0.4 runs on Node 24.** Full chain green | **Ladder A**. No side Node LTS install, no `engines.node` pin. Raw logs: `docs/audit/raw/probe-*.txt` |
| 2 | Bower is dead; must vendor ~25 components into `omega-web/lib/` (multi-MB into git) | **`bower install` works on this host** (both `git://` deps resolve), and the components turn out to arrive through **grunt**, not through that script | **Do not vendor.** `deps` and `grunt/aliases.coffee` stay untouched; `tools/verify-vendor.mjs` is the guard instead. Clean-runner reproducibility was UNVERIFIED when this row was written; the 2026-11-05 runs installed and built on three images with no cache behind them and produced the same 252 `lib/` entries (`docs/audit/99-baseline-report.md` §12). The lockfile gap is still open, and §12 states what that leaves unproven |
| 3 | The build blocker is the toolchain | The blocker is **8 pre-existing `max_line_length` coffeelint *errors*** in `omega-pac`; `grunt test`-less default task aborts → `omega-pac/omega_pac.min.js` is never produced → `omega-target-chromium-extension/grunt/browserify.coffee` requires that file **at config load** → `Loading "Gruntfile.coffee" tasks...ERROR` → grunt-hub aborts | Fixed by demoting `max_line_length` from `error` to `warn` in exactly two files (see §5). No line was re-wrapped, no product behaviour changed |
| 4 | `grunt release` exit code 0 ⇒ correct artifact | False. Pre-fix zip: 352 files / 1 710 687 B, **missing `js/omega_pac.min.js` (742 272 B)** — the extension loaded as a blank page | Good artifact baseline: **353 files, 5 999 353 B uncompressed, 252 `lib/` entries, 7 `_locales`** |
| 5 | Tests are a green baseline | There are **7 real test files** in the repo. `omega-web` and `omega-target-chromium-extension` have **no `test/` directory at all**, yet `grunt test` there succeeds because `mochaTest.src` globs `test/**/*.coffee` and matches nothing; neither package.json even declares an npm `test` script. Real floor today: **omega-pac 142 passing / 1 failing**, **omega-target 8 passing / 5 failing** | The 6 pre-existing failures are the **pinned regression floor**, not a defect to fix in Phase 1. The failing `pacResult` SOCKS5 case (`omega-pac/test/profiles.coffee:49-52`) is a **Phase-2 blocking requirement** because `ConnectorProfile.compile` depends on that code path |
| 6 | master-prompt §12 (safe subscription update) needs building | Already correct today: `Options.updateProfile` writes only when `Profiles.update(...)` returns true or `sha256` is absent, and `.catch` puts the error in the *returned map*, never into `@_options` | What is **missing** is durable per-profile update status (`lastSuccessAt/lastError/consecutiveFailures/nextAttemptAt`), retry/backoff/jitter, size+scheme guards, and cache-buster determinism for signed URLs → `docs/audit/09`, `29-subscriptions-and-servers.md` |
| 7 | The seam is an abstract `Target`/`TargetCfg` (SwitchyOmega model) | Not in this fork. The seam is `Options` + an injected `proxyImpl` (`omega-target/src/options.coffee:101`), implementation chosen in `omega-target-chromium-extension/src/module/proxy/index.coffee` from `[Firefox, Listener, Script, Settings]` | `docs/audit/03-runtime-layering.md` must describe the *actual* seam |
| 8 | — | The repo has **zero VLESS code, zero Go, zero Native Messaging**. Searching `vless|xray|reality|nativeMessaging` yields one broken draft (`vless-test.coffee`, deleted) | Phase 1 documents are the only thing standing between this repo and a greenfield claim |

Host toolchain at probe time (for reproducing): **Node 24.22.1, npm 11.19.1, git 2.56.0. Go, Docker and Python are absent.** `npm config get script-shell` → `null`, i.e. npm scripts run through `cmd.exe`, so any POSIX `TZ=` prefix in an npm script fails hard on this box.

---

## 2. What the audit already proved (the load-bearing findings)

Full documents for these are the pending task #6; the conclusions are settled and must not be re-litigated.

1. **Wipe trap.** `omega-target-chromium-extension/src/coffee/background.coffee:32-47` — on
   `runtime.onInstalled{reason:'update'}` it calls `storage.sync.clear()` + `storage.local.clear()` +
   `idbKeyval.clear()` when the new version is greater than the previous one **and** `3.3.0 > currentVersion`.
   A `1.x` numbering scheme therefore wipes user data on the *second* update inside the NEXORA line
   (`1.0.0 → 1.1.0`). This is why version numbering is an architectural constraint → **ADR-0015**.
2. **SEC-1, secret leak into sync.** `omega-target/src/options.coffee:27`:
   `transformValueKeys = ['lastUpdate','ruleList','pacScript','sha256']` — `headers` is not stripped, and
   `Options.updateProfile` really does use subscription `headers`. So `Authorization` from a subscription
   profile is copied verbatim into GitHub Gist / WebDAV. The exclusion mechanism exists (`-customCss` is
   excluded) and was simply never extended. Primary justification for the keystore boundary
   (`32-secrets-keystore-masking.md`) and for `envelope-keys.schema.json`. **Not fixed in code in this
   iteration** — changing `transformValueForSync` without a test changes product behaviour. It is a
   Phase-2 requirement.
3. **Envelope contract.** `omega-pac/src/profiles.coffee:111-118` — `Profiles.each` iterates only keys whose
   `charCodeAt(0) == '+'`. Any NEXORA entity stored under `+…` would leak into `availableProfiles`,
   `validResultProfilesFor`, `allReferenceSet`, quick-switch lists and the generated PAC. Hence every NEXORA
   key lives under `-` → ADR-0012, `24-options-envelope-compat.md`, and the machine check
   `keyPattern.startsWith('+') === false` in `scripts/verify.mjs`.
4. **No SSRF/size guard on subscription fetch.** `src/module/fetch_url.coffee` is a bare `fetch()` with a
   `?_=<ts>` cache-buster; `RuleListProfile`'s Content-Type hint list ends in `*`, so anything is accepted.
   A subscription URL may point at `http://127.0.0.1:*`, a cloud metadata address, or a 200 MB body.
   → `subscription-source.schema.json` (`https://` only, no userinfo, `maxBytes ≤ 8 MiB`).
5. **`chrome.storage.session` vs `minimum_chrome_version: "88"`** (`omega-target-chromium-extension/src/module/options.coffee:106-140`)
   — latent bug on Chrome 88–101. Connector session state must not repeat this design.
6. **i18n**: 27 locale directories, only 7 ship (`grunt/po2crx.coffee`). `jszip` is loaded in
   `omega-web/src/options.jade:82` and used nowhere in `src/` — dead code.
7. **Twelve** legacy `conditionType` registry values exist in `omega-pac/src/conditions.coffee:227-687`
   (not nine as an earlier draft assumed). SwitchProfile rules are `{condition:{conditionType,…}, profileName}`
   plus `defaultProfileName`.
8. **MV3 cold-start RPC backoff is a fixed user-facing bug** (#276, `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78`):
   5 retries at `100*attempt` ms then 9 at `2000*2^(attempt-6)` ms on `"Receiving end does not exist"`.
   The new UI client must port this **verbatim** or it reintroduces the popup hang.

---

## 3. Repository layout as agreed (ADR-0002)

Legacy `omega-*` directories **do not move**. Name-coupling exists in ≥15 places: the `submodules` array and
hub glob `../*/Gruntfile.*` in `omega-build/Gruntfile.coffee`, `cd ../omega-pac` chains in `omega-build/package.json`,
relative `file:` deps, the copy glob `'../omega-web/build'`, 6 cache keys in `.circleci/config.yml`, and — the
subtle one — downstream projects read **prebuilt bundles at the root of the upstream project** via a
`node_modules` symlink (`node_modules/omega-pac/omega_pac.min.js` in `omega-web/grunt/copy.coffee`,
`node_modules/omega-target/omega_target.min.js` in `omega-target-chromium-extension/grunt/copy.coffee`).

Additive overlay, no root `package.json` (ADR-0018) so the legacy grunt graph cannot be hit by accident:

```
docs/README.md  docs/audit/00-12 + 99 + raw/  docs/architecture/20-40  docs/adr/0001-0020  docs/verification/
packages/{schema,protocol,state,i18n,ui}/     connector/                nexora/extension/
tools/{verify-vendor.mjs,check-no-secrets.mjs,check-doc-evidence.mjs}
.github/workflows/verify.yml   .gitattributes   .env.example   *.npmrc
omega-build/ omega-pac/ omega-target/ omega-web/ omega-target-chromium-extension/ omega-locales/   # untouched positions
```

Artifact chain (recorded verbatim in `docs/architecture/20-…`):
`npm run deps` → `npm run dev` → `npm run build` (grunt-hub: omega-pac → omega-target → omega-web →
omega-target-chromium-extension) → `npm run release` (the `manifest` task filters `downloads` and writes
`tmp/{chromium,firefox}/manifest.json`; `compress` writes `dist/chromium-release.zip` + `dist/firefox-release.zip`).
The Connector is **never** inside the extension zip. Renaming artifacts to `nexora-chromium-<ver>.zip` is a
cut-over-level change (`grunt/compress.coffee` + `.github/workflows/release.yml:29-39`) — specified now, not done now.

Rule: nothing under `nexora/**` may import from `omega-*`, except types derived from `packages/schema`.

---

## 4. On disk right now (inventory, all committed and pushed)

### 4.1 Infrastructure — done

- `.gitattributes` (new): binary asset declarations, `eol=lf` for `*.sh *.ps1 Gruntfile.coffee **/*.coffee **/*.jade **/*.less **/*.json **/*.md` and future `connector/**`.
- `.env.example` (new): every live-matrix variable, empty values, each line comments which `LIVE-nn` row it gates.
- `.npmrc` × 5 (new, one line each: `legacy-peer-deps=true`) in `omega-build`, `omega-pac`, `omega-target`, `omega-web`, `omega-target-chromium-extension`.
- `.gitignore`: appended `.env`, `.env.*`, `!.env.example`, and `dist/` (the release directory — the two zips were already caught by `*-release.zip`, the checksum file `verify.yml` writes beside them was not).
- `tools/verify-vendor.mjs` — collects every `lib/...` reference from `omega-web/src/**/*.jade`, `src/popup/**/*.html`, `src/coffee/*.coffee` and `omega-target-chromium-extension/overlay/*.js`, resolves each against `lib/` **or** `build/lib/`, exits non-zero on a miss.
- `tools/check-no-secrets.mjs` — secret scanner with vendored-path skip, identifier-vs-literal discrimination and a `credentialShaped()` entropy gate; `--selftest` runs 13 cases and is green.
- `tools/check-doc-evidence.mjs` — asserts every citation in `docs/**` resolves: a `path:line` lands on a non-blank line
  of a file in the tree, a `path#/json/pointer` lands inside a JSON document that parses, a document or gate-script
  name that a document tells the reader to run or read exists (or is in `PLANNED_DOCS`, which is empty), and a quoted
  harness label is a check `packages/schema/scripts/verify.mjs` actually has. A line cite into a file rewritten in
  place — `MUTABLE_CITATION_TARGETS`, today `docs/PROJECT-STATE.md`, `docs/audit/99-baseline-report.md` and the
  harness itself — is rejected.
- `tools/check-legacy-backup.mjs` — hands the two shipped 3.5.2 envelopes to the real browserified `omega-pac` engine,
  compiles every profile the engine yields and evaluates each against sample requests; fails on a dangling profile
  reference, a key the `+`-prefix rule hides, an unknown profile type or a missing `schemaVersion`. `--selftest`
  rejects 4 broken envelopes. This is `LIVE-14`'s compile half and the command `docs/verification/README.md` §3 names.

### 4.2 `docs/audit/raw/` — done

`probe-1-legacy-toolchain.txt` (32 826 B) + `probe-1-table.txt`, `probe-2-legacy-toolchain.txt` (42 154 B) + `probe-2-table.txt`, `probe-3-ladder-a-green.txt` (14 711 B — the green chain: `hub:build exit=0`, `release exit=0`).

### 4.3 Two modified legacy files — the only product-file edits in this iteration

- `omega-pac/grunt/coffeelint.coffee`: `max_line_length` `level: error` → `level: warn`.
- `omega-target-chromium-extension/grunt/coffeelint.coffee`: same.

Both are needed for the build to run at all (§1.3). Reverting either restores the red build.

### 4.4 `packages/schema/` — the executable Phase-1 gate (task #5, done)

11 JSON Schemas: `server`, `connector-profile`, `routing-policy`, `subscription-source`, `envelope-keys`,
`failure-policy`, `events`, `pins`, `connector-settings`, `import-staging`, `envelope-backup`
(+ `protocol/envelope.schema.json`, `protocol/commands.schema.json`).
4 protocol data documents: `protocol/error-taxonomy.json` (≥20 codes, each with `retryable`),
`protocol/framing.json` (1 MB extension→host, larger host→extension, `oversizedPayloads.decision = "chunked-envelope"`),
`protocol/compatibility-rules.json` (capability names), `protocol/command-allowlist.json` (**exactly 19 methods**,
only `connector.hello` is `availableBeforeHello`).
19 command documents `protocol/commands/*.json`, each validated by `commands.schema.json` and cross-checked for
taxonomy/capability/event membership, idempotency rationale and probe non-replayability.
20 valid examples in `examples/` (5 server forms incl. the three canonical Xray shapes, 3 profiles, 2 routing,
3 subscription, envelope-keys, failure-policy, events catalog, pins, settings, import-staging, envelope-backup).
24 negative cases in `examples/invalid/` + `reasons.json` (`{schema, keyword, mustReject}` per case) —
the harness asserts rejection **and** that the documented keyword is what rejected it.
`fixtures/protocol/*.jsonl` — **6 files, 112 frames** (`handshake`, `core-lifecycle`, `config-apply`,
`import-chunked`, `failure-block`, `probes-and-ops`), 5 idempotent replay pairs and one probe asserted
single-answer. `packages/schema/README.md` explains the package and the rules it encodes.

Seven of these files are *lists* — the repo's "data home" rule puts the enumeration in JSON and the argument in the
architecture document: `protocol/data-ownership.json` (34), `migrations.json` (35), `observability.json` (36),
`presentation.json` (37), `compatibility.json` (38), **`verification.json` (39)** and **`open-decisions.json` (40)**.
The queue is `202` labelled lines over `44` documents partitioned into `77` live rows — the issued range is still
`VER-01…VER-81`, and `4` of those ids now sit in `verification.json#/retired` — `26` vocabulary
sentences and `24` live rows inherited by id from `docs/verification/live-matrix.md`. The register is `61` deferral
sites over `14` documents partitioned into `40` entries `ODEC-01…ODEC-40`, each naming who can answer it
(`product` 14, `contract` 16, `adr` 6, `measurement` 2, `upstream` 2), three options — thirty-seven of the forty,
since `ODEC-35` carries a fourth that records the reading ADR-0004 rule 3 already excludes rather than leaving it
unwritten, `ODEC-37` carries a fourth that needs a Core binary `docs/adr/0013-omega-conditions-compiles-into-xray-routing.md:98`
records as absent on this host, and `ODEC-39` carries a fourth that answers only the second half of its own question —
which two directories and which file name — because the seam that carries an intent and the layout that holds it have
different owners — and the `VER`/`COMP` rows it
blocks. Anchors are verbatim substrings, never line numbers; the harness re-reads `docs/**` on every run and fails if
a claim or a deferral is unowned, owned twice, or if a document reads a distribution the data does not have.

`npm --prefix packages/schema run verify` → **green: 1220 checks, 13 schemas, 19 command docs,
20 valid examples, 24 invalid rejected, 112 frames replayed**. The only note printed is `compiled 13 schemas`;
every `DEFERRED:` note the earlier drafts printed is gone: `26-command-surface.md`,
`37-ui-architecture.md`, `38-compatibility-matrix.md`, `39-verification-matrix.md` and
`40-open-decisions.md` exist now, so their cross-checks are real assertions.
`node tools/check-doc-evidence.mjs` → **green: 2262 `path:line` citations, 493 `path#pointer` citations,
1313 doc-and-script names** (they read `2260 / 487 / 1290` before §4.6's increment; the `path:line` figure fell by 21
when every cite into this file became a section
reference, and by 3 more when probe-4 turned the same rule on for `docs/audit/99-baseline-report.md`, which every
probe round appends to; it rose by 8 again when the runner record cited the legacy build configs it reads, and the
third figure by 34 for the same reason, and by two more in this sitting — its VER-11 row in
`docs/architecture/21-system-context.md` now names that runner record, and this file's sentence about the count now
names that document. It rose by one more in the pass that recorded the seventh and eighth runner runs: that pass
leaves one more named reference to the baseline report in `docs/` than it removes. The `connector/` increment §4.6
records moved the set again — **2262 / 487 / 1306**: two more `path:line` citations, sixteen more names, no new
pointer — and it is the first increment in this repository whose citations point at executable code in a language
other than CoffeeScript or JavaScript. Both figures moved for two reasons: §4.6 and §6 cite code paths that exist
now, and ten documents besides this one had to stop asserting that this host had no Go toolchain, no Go code or
no `connector/` tree — each of them now names §4.6 as the reason that sentence stopped being true.
The peer slice §4.7 moves the set a second time, and in a different shape — **2262 / 491 / 1312**: no new `path:line`
citation, four new pointers, and all four of them JSON locations inside
`packages/schema/protocol/framing.json` or `packages/schema/protocol/envelope.schema.json`, because what those two
sections had to establish is not where a line of code sits but what the data says about the direction of a heartbeat
and how many values the envelope's `type` can take. The six new names are the documents those sections cite for the
same reason. Nothing above or in §5 is a runner's number for this tree: `42046629162` measured `1004c4e`, and no
execution has happened since, so the figures the runner printed stay in the run record and the figures this paragraph
prints are read on this host. The front door §4.8 moves it a third time — **2262 / 493 / 1313** — and again with no
new `path:line` citation, because what those two sections cite is a rule in the contract data and a section of doc 31,
not a place in a file. The negotiation §4.9 moves it a fourth time — **2266 / 496 / 1317** — and all three figures rise
this time, because it is the first section of this file whose `path:line` citations are not about where a rule lives
but about where two of this repository's own packages read the same data differently: all four of them are inside §9's
ninth row. The three new pointers are locations inside `packages/schema/protocol/`, and the four new names are the
documents and the live matrix those two sections read. §4.10 answers that row and moves the set a fifth time —
**2262 / 500 / 1323** — and its shape is the finding rather than an accident: the four `path:line` citations leave,
every one of them having been the ninth row's address for the disagreement, and four `path#pointer` citations take
their place, because what settled the question is a rule in the contract data and in
`docs/adr/0007-typed-versioned-command-allowlist.md`, not a place in a file. A disagreement between two implementations
gets cited by line; the rule that ends it gets cited by pointer. The six new names are ADR-0007 and the schema harness,
each of them restated by every section that carries the answer — §4.9's addendum, §4.10's table and §9's note.
§4.11 moves the set a sixth time — **2262 / 507 / 1335** — with §4.10's shape and the opposite cause: the
`path:line` figure does not move, because the finding is that a `path:line` citation is precisely what went stale
under the paragraph. Two citations in `docs/architecture/32-secrets-keystore-masking.md` into doc 25 §8 had their
numbers re-read and moved by twenty-one lines, and a re-numbered citation changes none of the three figures. What the
slice adds is seven pointers and twelve names — four pointers and three names in the paragraph the protocol document now
carries, one name in the register's new argument for why a green check hid the answer, two in the index row that says
the package is not written, and the rest in this file's own record of the finding.
§4.12 moves the set a seventh time — **2262 / 508 / 1340** — and it is the smallest move in the series in one figure
and the loosest in another: no `path:line` citation is added or lost, one citation by pointer arrives because the record
names the field it repaired, and five names arrive because a slice that settles a disagreement between two files has to
cite both of them, twice in the finding and once in §4.4's own sentence. It re-numbers one citation instead, which is
the §4.11 shape arriving one slice later than predicted: four added lines in doc 31 §8 moved doc 34's cite into that
document's `unlimitedStorage` row.
§4.13 and §4.14 move the set an eighth time — **2267 / 514 / 1356** — together, and in the shape of a code slice plus
the registration it forces. Read per file against a clean worktree at `4997081` — which is the only way to attribute a
move this size, because two of this slice's authors were editing the same tree — the protocol document carries the
evidence half of it, `+3 / +3 / +6`: a deferral that claims four contract statements close on each other has to cite
all four, and the
invariant row the new queue entry anchors on cites the register beside them. This file moves `+1 / +2 / +6`, the
register's document `+1 / +1 / +2`, and the docs index `+2` names — one row for the package that now runs, one for the
one that is stopped. Nothing was re-numbered by accident, and doc 32 moves `+0 / +0 / +0` while two of its citations
shifted by fifty lines, which is the third arrival of §4.11's lesson and the reason a re-read is not a delta: the slice
that registers a gap in a document also makes that document longer. What no guard here can see is the count word §7 had
been carrying since the seventh slice's own commit — "the five slices §4.7 to §4.12 record", for six of them, above a
breakdown that adds to a different number again: a tally written beside a list drifts from that list silently, and
`tools/check-doc-evidence.mjs` checks that citations resolve, never that prose counts its own rows — and the same
limit is why a clause orphaned by the seventh slice's own edit had been sitting in this paragraph for a day with an
unmatched parenthesis and no subject. Repairing it restates what it had been recording before the edit swallowed it:
`DOCNAME_RE` checks that a `tools/*.mjs` name a document tells the reader to run is a file that exists, and
`PLANNED_DOCS` is empty — no document is cited anywhere in `docs/` that does not exist.
§4.15 moves the set a ninth time — **2281 / 516 / 1367** — and it is the first move in this series whose headline is a
figure that barely changed: two new `path#pointer`s, the two JSON locations the record and its own harness read, against
fourteen new `path:line` cites. Read per file against a clean worktree at the commit before: doc 27 `+5 / +0 / +3`, because a slice
that repairs a clause by naming the fields it would have eaten has to cite the schema that declares them and the legacy
code that carries the clock; this file `+8 / +2 / +4`; the register's document `+1 / +0 / +2`; the docs index
`+0 / +0 / +2`; and docs 32, 33, 36 and 39 `+0 / +0 / +0` each — four documents whose citations into doc 27 were re-read
and re-numbered, which is §4.11's lesson arriving a fourth time and the reason a re-read is never a delta. What this move
has that the eight before it did not: one of those re-read citations had *never* been right. The row in doc 33's index
that cites "the compiler's dependency on install state" pointed at the footer of doc 27 — a `See also` line, non-blank,
so green for two slices, and not the claim. The guard checks that a number lands on text; it cannot check what the text
says, so §8's decision 19 keeps the finding instead.
§4.16 moves the set a tenth time — **2282 / 517 / 1380** — and the move is almost entirely the doc-name kind: measured
per file against the commit before, this file is `+1 / +1 / +7`, doc 23 `+0 / +0 / +1`, doc 27 `+0 / +0 / +2`, doc 40
`+0 / +0 / +3`, and the two documents this slice rewords hardest — `docs/README.md` and
`docs/architecture/26-command-surface.md` — move `+0 / +0 / +0`, because a sentence corrected in place to stop claiming
that no compiler exists neither adds nor drops an address. The one `path:line` and the one pointer are the two the
prose pass needed to cite rather than assert: the taxonomy row that proves `ODEC-03`'s old premise was wrong, and the
`$defs` entry whose description contradicts the values the same definition admits.
The runner ledger below moves the set an eleventh time — **2282 / 517 / 1381** — one doc-name reference and nothing
else: measured against HEAD file by file, §12 of the baseline report gained the one and no document lost one, which is
the cheapest kind of move a bookkeeping pass can make.
§4.17 moves the set a twelfth time — **2314 / 517 / 1389** — and it is the first move of this series whose headline is
the first number: 32 new `path:line` addresses, no new pointer, eight new document names. Measured per file against the
commit before: this file `+27 / +0 / +5` — five, because this sentence names two of the documents it is counting —
`docs/adr/0011-windows-keystore-dpapi.md` `+5 / +0 / -1`, and `docs/README.md` and doc 32 `+0 / +0 / +2` each. The ADR's
minus one is the sentence that said no keystore code exists yet; a claim about the absence of a file is not an address
once the file arrives. Doc 39 moves `+0 / +0 / +0` while its `no-code` table loses a row and its distribution is
restated four times: §4.11's lesson a fifth time, an edit that re-numbers a count without naming a new place is
invisible to this guard, and what makes the change real is the harness asserting the distribution the data gives it —
55 / 13 / 3 / 2 / 5 at that commit, and the next line is the second time the middle numbers moved.
§4.18 moves the set a thirteenth time — **2331 / 518 / 1394** — seventeen `path:line` addresses, one pointer and five
names, measured per file against the commit before: this file `+17 / +1 / +3` and `docs/README.md` `+0 / +0 / +2`. The
one pointer is `previousConfigHash`'s pattern, cited because the writer reports "nothing was applied" as an empty
string and that is only honest if the schema admits one. Doc 27, doc 32 and doc 39 move `+0 / +0 / +0` between them
while all three change what they assert about the same file — §8's parenthetical rewritten, §1's reading rule and §13's
cost paragraph re-typed inside their own line counts, doc 39's row deleted from one table and added to another — which
is §4.11's lesson a sixth time, and the reason the guard's set is a floor on the work rather than a measure of it.
§4.19 moves the set a fourteenth time — **2347 / 520 / 1402** — in the shape §4.13 first used, a code slice plus the two
registry rows it forces, and read per file against the commit before: this file `+16 / +1 / +4` — sixteen, because a slice
that measures a syscall table cites the lines it measured, and the one pointer is `coreDeath`'s integer, named because it
is the field with no denominator; the docs index `+0 / +0 / +2`, one row for the package that now runs and one for what it
refuses; the failure-policy document `+0 / +0 / +1`, which is the single new labelled line both registries claim; and the
register's document `+0 / +1 / +1`, the pointer being `ODEC-38`'s own anchor. Six documents besides this one change what
they assert and move `+0 / +0 / +0`: docs 21, 22 and 39, ADR-0005, the live matrix, and doc 32 — and doc 32 is the finding
this paragraph exists to record, because three of its citations into the failure-policy document had gone *wrong* rather
than stale. Six lines inserted above them landed each address on a blank line, and `tools/check-doc-evidence.mjs` printed
three `BLANK TARGET` findings; the four addresses moved are `:135` and `:370` forward by six and eight, and the range
`:369-370` with them. §4.11's lesson was that a re-read citation is invisible to this ledger; this is the case that lesson
did not cover — a citation nobody re-read, plausible because the sentence under it was untouched, caught only by the
guard's floor that a number must land on text.
The push that carried §4.17 through §4.19 moves the set a fifteenth time and changes none of the three figures —
**2347 / 520 / 1402** — which is the first entry in this ledger whose headline is that nothing moved: a commit sha, a
refspec and a line of remote output are prose about the remote, not addresses into this tree, and the three numbers count
only the second kind.
§4.20 moves the set a sixteenth time — **2362 / 523 / 1408** — in §4.17's shape, a code slice whose new addresses are all
inside the code it just wrote, and measured per file against a clean worktree at the commit before. Reading that worktree
is not only the attribution method: it printed exactly the three figures §4.19 recorded, so the delta is a difference
between two runs of the same guard rather than a claim about prose. This file moves all fifteen `path:line` addresses,
plus one pointer and one name; doc 26 and doc 40 gain one pointer each — the same `[string, null]` type, cited once in
the contract sentence that now states what an unstated belief is and once in the register entry that turns the intent
seam into a decision — doc 27 one name, doc 40 one name, and doc 39 `+0 / +0 / +0` for the seventh slice running, while
its `no-code` table loses a row, gains one and has six of its distributions re-typed. The sixth name belongs to `README.md`:
three of its `connector/` rows ended with "no command surface calls it", and a row that says that while §4.20 exists is
not a slow claim but a false one, so each now points at this file instead of at nothing. What this ledger cannot see is
the half of the slice that was a repair: two addresses in this file into
`connector/internal/supervisor/supervisor_test.go` had gone wrong when the fake child gained fifteen lines above them, and
fixing them moves nothing. §4.11's case was a citation somebody re-read and did not update; §4.19's was a citation nobody
re-read, green because the sentence under it was untouched; this is the third way a number can be wrong while the ledger
stays silent — the *file the number points into grew for a reason belonging to the next slice*, and both figures and prose
read fine until a test starts needing a process that stays alive.
§4.21 moves the set a seventeenth time — **2364 / 524 / 1410** — by **+2 / +1 / +2**, every one of them inside this file,
which is what a slice whose subject is the harness looks like: two new addresses for the gate message and its test, one
pointer to the `countedWhere` field the new assertion now reads, and two names — doc 39, whose tallies the harness now
opens, and the harness file itself, whose growth is the paragraph's other finding.
The two documents the assertions read move by nothing, and that is the point of them: a check that *reads* a document
leaves no citation behind, because reading is not pointing. The one prose edit outside this file is the exception that
proves the same rule — doc 39's "Five rows are `needs-human`" became "5 rows are" so the number could be checked at all,
which is the first time in this ledger a sentence changed its *format* because a guard asked to parse it. §4.20's third
failure mode arrived on schedule one file over: this slice grew `packages/schema/scripts/verify.mjs` by 88 lines, 22 of
them above the §4.18 address into its anchor rule, and that address moved by exactly the 22 that preceded it — silently
and correctly, because a citation that lands on a different assertion than the one it names is caught only when it
lands on a blank line.
§4.22 moves the set an eighteenth time — **2312 / 524 / 1463** — and it is the first entry in this series whose first
number *falls*: −52 `path:line`, +53 document names, the pointer count untouched by a slice that touched nothing but
addresses. All 52 were line numbers into the one file four earlier paragraphs of this ledger explain cannot be addressed
by line number, and §6 #8(b) had deferred them as a job to do "in one commit, not in passing" so that the file could
join `MUTABLE_CITATION_TARGETS` in the same breath. Per file against a clean worktree at the commit before: doc 26
`−17 / +17`, doc 20 `−6 / +5`, ADR-0007 `−7 / +4`, ADR-0008, ADR-0010 and ADR-0012 `−3` each (`+4 / +3 / +3` names),
ADR-0004 `−2 / +2`, `docs/audit/04` `−2 / +1`, this file `−2 / +8`, and ADR-0002, ADR-0014, doc 25, doc 27 and
`docs/audit/05` `−1 / +1` each — plus doc 21 at `−0 / +1`, the odd one, because its frame-limits cell carried a bare
range that had never been a citation the guard could see. The two counts do not move one for one, for two reasons the ledger should own
separately. `DOCNAME_RE`'s optional suffix is `[:#]?\d*`, so a *range* address — file, colon, number, dash, number —
was never a name the census could see, and 41 of the 52 were ranges; the converted form registers the file every time it
registers a check. The other nine are this bookkeeping: writing down the conversion names the harness eight times in
this file and once more in doc 21, so the act of recording the move is one of the things that moves the figures it
records. The guard now prints a fourth number beside the three, **68 harness assertion labels**, and that is what the
names resolve to.
The failure mode §4.20 named as the third one recurred *inside* the slice that removes its cause: ADR-0010 grew a line
while its own cites were being converted, two addresses in doc 34 moved onto the neighbouring real lines, and the guard
saw only the third — the one that landed blank. The other two exist only because someone diffed every cite's target
between HEAD and the worktree, which no citation checker can do: `:111` and `:112` are both legitimate lines, and only
the sentence knows which one it means. That is the argument for −52 rather than for tidiness.
A nineteenth entry, and the second whose headline is a remote rather than a tree, because the query that answers it
changes none of the three figures: `git ls-remote nexora master` prints `7544baa`, and `git rev-list --count
nexora/master..HEAD` prints **3** — §4.20's `293cb73`, §4.21's `d1d22bb` and §4.22's `31008a5` are committed and not
pushed, since §7's rule asks for a confirmation per push and none has been given for these three. §7's opening bullet
still named `cf7e56d` as the remote's head, which was true when it was written and is two commits behind the present;
the bullet now states what the remote prints instead, for the reason the fifteenth entry gave — a sha and the output of a
query against the remote are prose about the remote, and the remote is the only witness.
§4.23 moves the set a twentieth time — **2312 / 528 / 1471**, 68 labels — and it is the first entry in this series whose
subject is a sentence rather than a file: no Go was written, no contract changed, no assertion added, and the first
number does not move because a slice that adds no address cannot add a `path:line`. Per file against a clean tree at the
commit before: doc 29 `+0 / +0 / +1`, the one name its new §13 row points at the register; doc 39 `+0 / +0 / +1`, the
amendments paragraph naming the document whose §5 gained the label; doc 40 `+0 / +1 / +2`, the refresh contract's result
pointer plus doc 29 and doc 34 in the new entry's argument; and this file `+0 / +3 / +4` — the three pointers §4.23
measures into the refresh contract, the source schema and the register's own scope, its heading and its body naming doc
29 twice and doc 34 once, and the harness file named once for the four constants it grew. That last clause is also the
slice's one lesson in the guard's own grammar: the first draft named the register by path in a sentence *about* naming
the register, and the run counted that line as a deferral this file owes — `derived 62, declared 61`, with
`unowned: docs/PROJECT-STATE.md:1773` pointing at the sentence. A description of a deferral is not a deferral, and the
membership rule cannot tell them apart; the prose now says "names the register". What the corpus counts moved for is the
queue and the register rather than the
citations: one labelled line in doc 29 §5 and one deferral site in its §13, so `marker.counted` goes
207 → 208, `citation.counted` 60 → 61, and doc 39's partition sentence reads rows (158) + vocabulary (26) + live (24) =
208. §4.20's third failure mode — the file a number points into grew for a reason belonging to the next slice — did not
recur, because nothing in this slice points into a file that grew; what did recur is its cousin, and this ledger has not
seen it before: **a citation that collides with the anchor it describes**. The first draft of §13's seventh row quoted
§5's own words to say what §5 says, and those words are `VER-81`'s anchor, so one anchor landed on two lines of one
document. No prose review can see that, and the registration would have looked *complete* — a row, an entry, a site, a
label — while the queue silently lost §5's claim. It was found by a throwaway census that printed 158 anchors landing on
159 lines, and proven to be caught rather than merely noted by restoring the collision and running the harness, which
fails three checks and names the document, the line and the arithmetic. The 68 labels are the unchanged fourth figure: a
slice that changes four constants in the harness quotes no new label.
A twenty-first entry, the third whose headline is a remote and the third that moves no figure at all — **2312 / 528 /
1471**, 68 labels, and every one of them unchanged, measured against a clean archive of the commit before rather than
asserted from the shape of the edit. §4.24 re-types a sha, a query's output and the five commits it counts, and none of
those tokens is an address: no backtick in the whole edit holds a path, a line number or a pointer, so the census walks
this file and finds the same three counts it found an hour ago. The fifteenth and the nineteenth did the same arithmetic
about the same bullet; what is different here is only that the interval between the bullet being written and the bullet
being stale is now one slice rather than three, which is what a sentence about a remote costs in a repository that
commits a slice a sitting. It is also the first entry whose *own* text moves nothing it describes: §4.23's paragraph
still reports 1471 because nothing after it added an address, and §4.24's paragraph reports the same number twice — once
as the delta of the edit and once as the delta of the recording — which is the ledger saying out loud that this file is
the only artefact the slice touched.
§4.25 moves the set a twenty-second time, and it is the first entry in this series whose census has a minus sign:
**2312 / 534 / 1478**, 68 labels. Per file against a clean archive of the commit before — doc 20 `56 → 54` names, the
two it stops citing are the queue document and the secrets tool, both of which appear only in the sentences deleted with
the rows; doc 39 `+0 / +2 / +4`, two pointers into the ledger and four names, the baseline report cited twice for the
runs that closed these rows, and the ADR and doc 20 each once by section; and this file `+0 / +4 / +5`, the ledger
pointer in §4.4's queue sentence, in §4.25's own text and in §5's, plus the baseline report, doc 20 twice and the ADR
twice. `check-doc-evidence.mjs`'s first figure is the one that refuses to move, and it is the most interesting number in
the set: a commit that deletes three rows, five labelled sentences and a whole assertion adds **no** `path:line`
citation, because nothing here points at a line of code — every mechanism this slice cites is a CI step named in a YAML
file the corpus addresses by section, and the ledger is a JSON pointer. What the 68 labels do prove is the substitution:
the harness still runs exactly 1220 checks, so the dense-range assertion left and the issued-once assertion arrived in
the same edit, and no label in the corpus now names a check that stopped existing — the guard that reads that claim
aloud is the one §4.21 added.
§4.26 moves the set a twenty-third time, and the minus sign is in the first figure again — **2311 / 537 / 1491**, 68
labels, measured after this paragraph was written, so the paragraph reports the set the tool prints rather than the set
its own sentences produced. Per file against a clean `git archive` of the commit before: this file `-1 / +2 / +8` — the
two names this paragraph adds are inside that `+8` — `docs/adr/0001-adr-practice.md`
`0 / +1 / 0` for the ledger pointer its new `Verified: YES` carries, and doc 39 `0 / 0 / +5`, four of them the new script
cited by name. The single subtraction is the line number §6 #7 used to carry into that ADR — the file is rewritten in
place, the citation rule says cite it by section, and this slice is the one that finally took its own advice. What the
`+13` names measure is the shape of a closure by tool rather than by run: a row that closes because a file now exists is
a slice whose evidence is the names of files, not addresses inside them, and the one address it does add is a JSON
pointer into the ledger. The fourth figure did not move, and the reason is the arithmetic §4.25 gives for the same
silence — the harness gained no check and lost none, it re-listed four distributions.
`node tools/check-legacy-backup.mjs` → **green**: both shipped 3.5.2 envelopes load and compile in the real
legacy engine (`11` profiles, `9` PAC bodies, `45` request evaluations), and `--selftest` rejects `4` deliberately
broken envelopes so that the green run cannot be a check that stopped checking.

**The harness caught eight real contract defects while the fixtures were being written.** This is the
evidence that it is load-bearing rather than decorative, and the pattern to watch for again:

| Defect | Symptom | Fix |
|---|---|---|
| `$defs.host` pattern was `^[A-Za-z0.\-]+$` | every hostname containing `1`–`9` was rejected (`se1.example.net`) | `^[A-Za-z0-9.\-]+$` |
| method/name pattern had exactly two segments | `connector.events.subscribe` (one of the 19) was itself invalid | allow 2+ segments in `protocol/envelope.schema.json` and `protocol/commands.schema.json` |
| `$defs.condition` had `additionalProperties:false` **outside** its `oneOf` | every legacy condition was rejected: branches do not contribute property names to the parent | `additionalProperties:false` moved into each branch; `WeekdayCondition`'s fields hoisted from `anyOf` onto the branch |
| `profile.import` required `profiles` with `minItems:1` | a chunked import could not exist: slice frames legitimately carry no profiles | `minItems:0` + `if chunk present then contentHash required`, and the chunk object gets its own shape |
| keystore/purge and failure-policy `confirm://` and `keystore://` patterns forbade `/` | reference URIs I had written everywhere (`keystore://server/<id>/user-id`) were invalid | one grammar for both schemes, `/` allowed |
| `connector-settings` invented diagnostics section names | it claimed to be "limited to the command's own enum" while listing different names | enum aligned to `diagnostics.get` + a new catalogue-parity check in `verify.mjs` |
| `failOpen ⇒ confirmToken` existed only as prose in a description | a Go or TS implementation reading the schema alone would not reject it | expressed as `if/then` inside `$defs.ProfilePolicy` |
| `core.select` summary exceeded the meta-schema `maxLength:400` | meta-schema violation found by its own conformance check | shortened |


What the contracts encode that a reader must not accidentally relax:

- `server.schema.json`: `protocol` const `vless`; `tag` `^srv-[0-9a-f]{8}$`; every secret field is a
  `keystore://[A-Za-z0-9._:/-]{1,96}` reference, never a value; `reality.privateKey` is **structurally
  impossible** (`additionalProperties:false`); `tls.insecure` const false; nine `allOf` combination rules, the
  important one being `flow: xtls-rprx-vision` legal **only** with `network: tcp` + `security: tls|reality`
  (so Vision-over-WS/XHTTP is rejected), and `h2` requires `tls`.
- `connector-profile.schema.json`: ordered `serverIds`, `selection`, `failover`, and — because
  `failOpen` needs durable evidence of user consent — `coreDeath.action = failOpen` requires `confirmToken`.
  That rule was **moved into the schema** (`failure-policy.schema.json` `$defs.ProfilePolicy` `if/then`) so any
  validator in any language rejects it; `verify.mjs` no longer owns it.
- `routing-policy.schema.json`: two planes (`browserPlane`, `corePlane`), `$defs.profileName` pattern
  `^(?!\+).+$` — writing `'+direct'` into a rule produces a rule that can never match, because
  `Profiles.nameAsKey` adds the `+` itself.
- `envelope.schema.json`: request ⇒ `method` + `not anyOf [result, ok, error]`; response ⇒ `ok`+`inReplyTo` and
  `anyOf(ok true → result | ok false → error)`; event/progress ⇒ `not required method`; `chunk` requires
  `contentHash/chunkSeq/chunkCount`; `traceId` charset excludes anything that can look like a URL or secret.
- `pins.schema.json`: `upstream.repository` const `https://github.com/XTLS/Xray-core`, exact `releaseTag`
  (no ranges, no `latest`), `verifySignature` const true, `silentSelfUpdate` const false, asset URL restricted
  to the upstream release host, `sha256 ^[0-9a-f]{64}$`, `sizeBytes ≥ 1 MiB`.
- `envelope-keys.nexora.json`: 9 namespaces; `server/profile/routing/subscription/settings/failurePolicy/pins`
  are `syncEligible: true, secretBearing: false`; **`ImportStaging` and `EnvelopeBackup` are
  `syncEligible: false, secretBearing: true`** — those two are the only non-vacuous instances of the
  `secretBearing ⇒ ¬syncEligible` assertion in `verify.mjs`, so don't "tidy" them into the other group.
  `syncExcludedValueKeys` (11 entries) must contain `headers` — that single line is SEC-1 as data.

`package-lock.json` for `packages/schema` exists locally but **is not tracked** (repo `.gitignore:3` ignores
`package-lock.json` globally, and the agreed plan explicitly rejects adding a negation). Consequence to state
honestly in `docs/audit/02`: contract-layer reproducibility on a clean runner is UNVERIFIED. Minimal fix if the
user later changes policy: `!packages/**/package-lock.json`. Pending user decision — risk §7.5 of the plan.

### 4.5 The plan's deliverables are all on disk — and the one that had not executed has now executed

Nothing the Phase 0/1 plan asks for on disk is absent any more. `.github/workflows/verify.yml` landed on
2026-11-05 — three jobs (`contracts`, `legacy-chain`, and `connector` present but switched off with
`if: ${{ false }}` — it is switched on now, §4.6), a three-row matrix, and the
`VER-02`/`03`/`04`/`06`/`08`/`10`/`11` guards as executable steps —
and it ran there the same day: `docs/audit/99-baseline-report.md` §12 tabulates ten of those runs — three red, six
times green and once cancelled by the file's own
`concurrency` block — under the rule that a further run joins the table only when its shape differs, a rule this
iteration had to write because every commit that records a run of that workflow is itself a commit that runs it. The
first two failures were defects in the workflow
itself rather than in the tree; the third was one Go test in `connector/internal/peer`, waiting on ~1 s of wall clock
for a reservation the peer had already made, which `75c9d97` changed into a wait on the bytes the pipe carries — all
of them recorded line by line in `docs/audit/99-baseline-report.md` §12. Before
those runs every `run:` block had been executed on this host and proved able to fail against a synthetic violation,
which is a claim about the shell logic; the runs are what made it a claim about CI
(`docs/audit/02-toolchain-decay-and-risk.md` §7.3). `tools/check-legacy-backup.mjs` — the command `LIVE-14`'s compile half cites — is on disk beside the other
three gate scripts.
`docs/architecture/39-verification-matrix.md`, `docs/architecture/40-open-decisions.md`, `packages/schema/verification.json` and
`packages/schema/open-decisions.json` are **on disk** now, with the harness
cross-checks that used to print `DEFERRED:` notes running as real assertions, and `docs/verification/README.md`
landed with them so `PLANNED_DOCS` in `tools/check-doc-evidence.mjs` is empty.
No `packages/{protocol,state,i18n,ui}` and no `nexora/extension` — all Phase 2+. `connector/` is no longer in that
list; §4.6.

---

### 4.6 The first executable code: `connector/` (2026-11-05)

The user lifted the Phase-1 hold on writing the two connector processes ("Go — начинай с bookkeeping-коммита,
дальше `connector/` на Go"), which is what `docs/adr/0005-two-process-connector.md:64` had reserved for Phase 2.

**Toolchain.** Go was not on this host — `go: command not found` — which is exactly the reason that line gives for
the prohibition. Offered three ways to fix that (user-local zip, CI-only, machine-wide), the user chose
`winget install GoLang.Go`: **Go 1.27.3** at `C:\Program Files\Go`. Shells that were open before the install do not
see it; they need `export PATH="$PATH:/c/Program Files/Go/bin"`. `connector/go.mod` declares `go 1.24`, the version
`setup-go` pins in CI, so the module's floor is the version the workflow proves rather than the one this host
happens to have.

**What landed** — four packages, all under `internal/`, and all of them derived rather than hand-copied:

| Package | What it is |
|---|---|
| `connector/internal/contractgen/` | a generator that reads `packages/schema/protocol/**` and refuses to emit on drift — it re-checks the `protocolVersion` agreement across the four files that carry it, that `availableBeforeHello` and `requiresHello` are complements, that the capability registry covers the surface, that the privileged set equals `senderPolicy.privilegedMethods.names`, that the byte limits are ordered, that every `onExceed` is `Busy`, and that there is exactly one `"*"` catch-all |
| `connector/internal/contract/` | `contract_gen.go` (committed, 19 methods / 23 codes / 13 capabilities / 6 budgets / the transport numbers) plus a drift test that regenerates it and compares the **bytes**, so the method list in Go cannot diverge from `packages/schema/protocol/command-allowlist.json:12` unnoticed — the same rule §3 of `docs/architecture/20-target-repository-layout.md` writes for `packages/protocol/` |
| `connector/internal/frames/` | the 4-byte little-endian prefix codec; `Write` checks the limit **before** touching the writer, which is what `docs/architecture/25-native-messaging-protocol.md` requires of the sender |
| `connector/internal/envelope/` | the envelope validator: required keys, closed object, id/method/trace/hash patterns, the four type rules, taxonomy membership for `error.code`, and the chunk mirror; reports **every** violation, not the first. It does not decide whether a method is published — see §4.10, which is where that rule moved from and to |

**Measured on this host** (`go -C connector …`, the invocation form ADR-0018 rule 7 mandates): `build ./...`,
`vet ./...` and `test ./...` all exit 0, and the replay test printed `replayed 112 protocol frames (110 accepted,
2 refused)` — the two refusals being the corpus's two requests for names the allowlist does not publish, with the
named invalid examples asserted by their own test. §4.10 moved that refusal out of the codec, so the same command now
prints `replayed 112 protocol frames, every one of them accepted as a frame`; §4.10 carries that measurement and this
one stands as what was true when M1 landed. `node
tools/check-doc-evidence.mjs`, `npm --prefix packages/schema run verify`, `node tools/check-no-secrets.mjs` and
`node tools/check-legacy-backup.mjs` all stay green on the tree with `connector/` in it; §5 carries the counts.

**CI.** The `connector` job that §4.5 recorded as switched off is switched on: two OS cells, `setup-go@v5` with
`go-version: '1.24'`, and the same three commands. That changed the *shape* of `.github/workflows/verify.yml`, so its
next execution became a row `docs/audit/99-baseline-report.md` §12's own rule says to tabulate: `42046629162`, green,
eight jobs passed and none skipped, printing `go version go1.24.13` on both images and, per cell, the three `ok` lines
and two `[no test files]` lines this host's own `go -C connector test ./...` prints. Its verbatim output, including the
two `Restore cache failed … go.sum` warnings a dependency-free module produces, is in
`docs/audit/raw/probe-8-runner-runs.txt`. It is not a `VER-NN` row moving, for the reason §5 already gives.

**Not yet.** No `cmd/nexora-host`, no `cmd/nexora-connector`, no ADR-0005 local pipe, no `connector.hello`
negotiation, no dispatcher, no per-sender budgets enforced at dispatch, no chunk reassembly, no Core supervisor, no
keystore, no config compiler, no prober — a snapshot of eleven items at M1, of which four are since written (§4.8,
§4.9, §4.13) and two more are since registered as questions with names (§4.11, §4.14). The live inventory is §6's queue,
not this line. `VER-49`/`13`/`52`/`55` keep their rows in
`packages/schema/verification.json` — none of them is met by M1, each asks about the behaviour of those missing
parts, and the queue has no state meaning closed (§5).

---

### 4.7 M2's first slice: the framing peer (`connector/internal/peer/`, 2026-11-05)

What §4.6's `frames` and `envelope` packages describe, `connector/internal/peer/` now *applies*: one pipe endpoint
that owns the transport rules a codec cannot own — the two directional byte ceilings and the backpressure budget in
`packages/schema/protocol/framing.json`, the late-answer rule and the "an illegal frame ends the stream" rule of
`docs/architecture/25-native-messaging-protocol.md`, and the closed-port semiotics of
`docs/adr/0005-two-process-connector.md`. The rules, and the test that measures each of them on this host:

| Rule | What the peer does | Test in `connector/internal/peer/peer_test.go` |
|---|---|---|
| the sender refuses before the write | `Request` compares the marshalled frame with `Config.WriteLimit` and returns `*frames.TooLargeError` without touching the writer, so the caller keeps an unsent request and the budget stays whole | `TestAnOversizedRequestIsRefusedBeforeThePipe` |
| backpressure has two ceilings, not one | the concurrency cap and the in-flight byte budget, reserved and released on the same path; the byte budget is shown to bind on its own by holding the concurrency cap out of the way | `TestTheConcurrencyCapStopsIssuing`, `TestTheInFlightByteBudgetStopsIssuing`, `TestTheBudgetsAreTheContractsAndCrossTheTwoDirections` |
| a late answer is discarded by `id` | a reply whose request already timed out has nowhere to go, so it is counted by `Discarded()` and never reaches the handler — the rule whose stated reason is that a late `replace`-class answer would be a configuration the user did not ask for | `TestALateAnswerIsDiscardedAndItsEffectNotReapplied` |
| an oversized or illegal inbound frame ends the stream | `Serve` returns `*frames.TooLargeError`, never `frames.ErrTruncated` (a message-delimited pipe that skips a frame is read out of step from there on), and a rejection that names the frame; neither is handed to the handler | `TestAnOversizedInboundFrameIsATransportErrorNotATruncation`, `TestAnInvalidInboundFrameEndsTheStream` |
| a closed port is the client going away | `Serve` returns `nil` for `frames.ErrClosed`, `Close()` unblocks outstanding requests with `ErrPeerClosed`, and neither path declares the peer lost — rule 4 of `docs/adr/0005-two-process-connector.md`: it is not a shutdown signal | the tail of `TestSilenceIsCountedInTheContractsOwnUnits` |
| the liveness arithmetic and the verdict | `Missed()` counts whole intervals of silence against `Config.Now`; the watchdog turns the third miss into `ErrPeerLost` and tears the pipe down | `TestSilenceIsCountedInTheContractsOwnUnits`, `TestThreeMissedHeartbeatsTearTheTransportDown` |

**Measured on this host**, Go 1.27.3: `gofmt -l connector` prints nothing, `go -C connector vet ./...` exits 0, and
`go -C connector test -count=1 ./...` adds `ok github.com/k2dms/nexora/connector/internal/peer` to §4.6's three `ok`
lines. Nine tests, all of them in `peer_test.go`, every one named in the table above and each of them passing here.
No runner has executed since `1004c4e`, so nothing in this section quotes one; §5 carries the corpus counts this host
reads.

**The rule neither constructor arms, and the reason.** `ToExtension()` and `ToHost()` both leave
`HeartbeatInterval` and `MissedHeartbeats` at zero, so neither starts the watchdog. The reason is the direction the
data declares: `packages/schema/protocol/framing.json#/liveness/heartbeatDirection` is `host-to-extension`, so on the
hop that has a heartbeat at all the Go peer is the one *sending* the beats and the extension is the one counting
misses. A deadline on inbound silence in `nexora-host` would tear the transport down under a browser that is merely
idle — and on that hop the contract's own "the client is gone" signal is the port closing, not a missed beat.
`ToHost()` arms nothing either, because `packages/schema/protocol/framing.json#/liveness` speaks of the browser hop
and ADR-0005's local pipe has no liveness number of its own — its rule 2 is a version rule. The machinery is written and measured anyway: the
two liveness tests set the interval by hand, which is exactly how a peer that turns out to *receive* the beats arms
it, and `TestTheBudgetsAreTheContractsAndCrossTheTwoDirections` fails if a constructor starts arming it on its own.

**Not yet**, and unchanged from §4.6's list: no `cmd/nexora-host`, no `cmd/nexora-connector`, no ADR-0005 local
pipe, no `connector.hello` negotiation, no sender identity, no dispatcher, no per-sender budgets enforced *at
dispatch*, no chunk reassembly, no Core supervisor, no keystore, no config compiler, no prober. (Twelve here; four of
them written by §4.8, §4.9 and §4.13, two registered by §4.11 and §4.14, and §6 is the live list.)

**`VER-49` does not close on this slice**, and the reason is worth the words because it is the first time the queue
and the contract disagree rather than the queue and the code. Its `closesBy.ref` asks that the framing peer
"refuse an oversized write, discard a late answer by id, **and** treat three missed heartbeats as a lost peer".
Two of those three clauses now have a test that names them; the third cannot be claimed honestly yet, because the
contract gives that heartbeat to the other endpoint and gives it no frame. That gap is §9's eighth question — the
first question in that list that writing code raised rather than reading the plan — and
until it is answered the row, its five labelled anchors in `docs/architecture/25-native-messaging-protocol.md`, and
the distribution counts §5 and `docs/architecture/39-verification-matrix.md` read from those rows all stay as they
are. Writing a third-clause test that passes by arming the watchdog by hand, in a package whose own constructors
decline to arm it, would close a row by testing a configuration no process uses.

---

### 4.8 M2's second slice: the front door (`connector/internal/dispatch/`, 2026-11-05)

`connector/internal/dispatch/` is the total function
`packages/schema/protocol/command-allowlist.json#/dispatchRule` promises — "resolves the method string against this
list before touching any state; the resolution is a total function with exactly one failure case" — with the gates
that function has to pass in the order the contract puts them. It decides *whether* a request reaches a handler and
never what a handler does, so it is the last connector piece that can be written before the methods that change
state arrive in M3.

| Gate, in order | What it answers | Test in `connector/internal/dispatch/dispatch_test.go` |
|---|---|---|
| the frame is a request | a non-`request` envelope is `BadRequest`, before anything is read out of it | `TestANonRequestFrameIsNotDispatched` |
| the caller is on the allowlist | `UnauthorizedExtension`, not retryable, and the sender does not learn whether the method it asked for exists — ADR-0004 rule 6 answered "only" with the sender error | `TestTheCallerIsResolvedBeforeTheMethodIsNamed` |
| the method is published | `UnknownMethod` for a name one character away from a real one, for a capitalised one, for a bare word and for the empty string; no default handler and no closest match | `TestAnUnknownMethodIsRefusedWithNoFallthrough` |
| the negotiation happened | every method but `connector.hello` before a successful hello is `HandshakeFailed`, walked over all 19 published names, with the handlers counting that none of them ran | `TestNothingIsDispatchableBeforeHelloExceptHello` |
| the hello announced the capability | a method whose `capabilities[]` the peer did not report is `HandshakeFailed`, because doc 31's two-code split puts "the negotiation did not agree" on that side and `docs/architecture/25-native-messaging-protocol.md` §7 states the MUST NOT | `TestAMethodNeedsTheCapabilityItsHelloAnnounced` |
| the build has a handler | a published method with no registered handler answers `Internal` rather than silently succeeding, and `New` refuses to build a dispatcher around a name the allowlist does not publish — that would be the fallthrough `deniedByDefault` forbids, wearing a Go map | `TestAPublishedMethodWithNoHandlerSaysInternal` |
| the sender's budget | its method's named window and the `"*"` floor, both counted before dispatch, answered with retryable `Busy`, and the handler's counter proves the refused request never started work | `TestABudgetRefusalIsBusyRetryableAndReachesNoHandler`, `TestTheCatchAllFloorIsUnderEveryNamedBudget`, `TestABudgetIsPerSenderAndNotPerProcess` |
| the refusal guard | ten refusals in ten seconds from one sender and the answer stops being a code: `Refusal.Close` tells the host to end the stream, which is not a shutdown signal for the connector or Core | `TestTenRefusalsInTenSecondsClosesThePort`, `TestAHandlerErrorIsAnAnswerNotARefusal` |

**Three readings this package had to make, because the data stops short of them.** A budget counts *admitted*
requests: `#/senderPolicy/rateLimitFacts/countedWhere` says a request refused by budget never reaches the handler,
and charging a refusal would let a sender that is already being refused spend the budget of the sender behind it.
The amplification guard counts every front-door refusal regardless of code — its stated reason in
`#/senderPolicy/refusals/amplificationGuard/why` is the cost of answering, not the identity of the caller — but it
does **not** count a handler's own error, because a method that fails is an answer the user needs and a failing M3
handler must not be able to tear down the pipe it is reporting through. And a capability the hello did not announce
is `HandshakeFailed` rather than a new code, because the taxonomy is closed (doc 25 §8 rule 1: a code outside it is a
protocol violation) and doc 31 defines `HandshakeFailed` as "the negotiation did not happen **or did not agree**".

**What this package does not do, and where that belongs.** `#/senderPolicy/portsPerSender` caps a sender at two open
ports and closes the oldest — a fact about connections, so the host owns it and this function has never seen a
connection. `privilegedMethods.rule` is a MAY that a *build* exercises by what it writes into the host's on-disk
allowlist, so there is no second knob here: a sender that is allowlisted is the pinned id by construction, and a
dispatcher flag invented without a configuration source would be a policy no process can set. And this package
answers no method itself: the negotiation it gates on arrives as `Identity.Negotiated`, and the package that
produces that value is §4.9.

**Measured on this host**, Go 1.27.3: `gofmt -l connector` prints nothing, `go -C connector vet ./...` exits 0, and
`go -C connector test -count=1 ./...` adds `ok github.com/k2dms/nexora/connector/internal/dispatch` — eleven tests,
every one named in the table above. Still no runner has measured this tree.

**No queue row moves on this slice**, and it is worth naming which ones it touches: `VER-51`, `VER-52` and `VER-55`
are the rows about the budget, the unknown sender and the counted-before-dispatch refusal, and all three close by a
live row (`LIVE-17`, `LIVE-16`, `LIVE-17`), because what those rows ask is that the refusal happen in a running host
with a real sender on the other end. A unit test here proves the front door's logic; it does not prove that a
process opens, resolves a sender id from the platform and answers on a pipe. `VER-49` is unaffected — this package
never touches the framing rules that row's closer names.

---

### 4.9 M2's third slice: the negotiation (`connector/internal/hello/`, 2026-11-05)

`connector/internal/hello/` is the frame every other frame waits for. `connector.hello` is the only method
`packages/schema/protocol/command-allowlist.json` lets through before negotiation, and the reason §7 of
`docs/architecture/25-native-messaging-protocol.md` gives for that gate is the reason this slice comes before
anything that changes state: "a misread frame cannot apply a wrong proxy configuration, because nothing is sent to
misread". The package is two types because the exchange has two lifetimes. A `Responder` is stateless and per
process — an answer depends only on the frame's args and on the build facts the process injected — and a `Session` is
one connection's memory of that answer, which is the value §4.8's `Identity.Negotiated` reads. That split is also
what makes the handler registerable in a shared dispatcher at all: the budgets are per sender and the negotiation is
per port, so the function the dispatcher holds cannot be the thing that remembers.

| Rule, from the contract | What the Go half enforces | Test in `connector/internal/hello/hello_test.go` |
|---|---|---|
| the recorded exchange | the Go answer to the fixture's compatible hello is that fixture's `result` **byte for byte**, in field order, with the capability list equal to the projected registry | `TestTheGoAnswerIsTheRecordedHandshakeByteForByte` |
| `packages/schema/protocol/compatibility-rules.json#/negotiation/rule`, in both directions | `Window.Intersects` is the rule as the arithmetic it states — the peer's minimum against this build's version, this build's minimum against the peer's version — tested on a connector window protocol 1 cannot produce, because the args floor is 1 and `minCompatibleProtocolVersion` is 1, so no peer can be too old for this build | `TestTheWindowRuleIsCheckedInBothDirections` |
| the recorded refusal | the stale hello in the fixture is answered in the fixture's own words, `no overlap between 7..7 and 1..1`, with the code it records and the taxonomy's non-retryable flag beside it | `TestTheVersionMismatchIsTheRecordedWords` |
| the gate §4.8 reads | a dispatcher holding this handler refuses `core.status` before the hello and admits it after; the admitted identity carries the 13 registry names; and a second connection from the same sender is not negotiated by the first connection's hello | `TestNothingIsDispatchableUntilTheHelloThisSessionRecorded` |
| `replayable: true` | the same id and the same args, answered twice, produce the same bytes, and the session does not churn on the replay | `TestARetatedHelloIsAnsweredIdentically` |
| a retracted window | a peer that agrees to `1..1` and then claims `7..7` is refused and the negotiation is withdrawn, because continuing to dispatch under a window the peer has just disowned is the misread-frame risk the exchange exists to close | `TestAHelloThatRetractsItsWindowWithdrawsTheNegotiation` |
| args the command cannot describe | thirteen malformed hellos — a missing field, a string wearing a number, a fraction, `null`, `0`, `256`, a four-part product version, an undeclared field, a 41-character build id, no args at all — each answered `HandshakeFailed`, each naming the rule it broke and none quoting the value | `TestAnUnreadableHelloIsHandshakeFailedBecauseHelloDoesNotDeclareBadRequest` |
| a window that contradicts itself | `minCompatibleProtocolVersion` above `protocolVersion` is refused as the empty window it is, rather than run through the intersection arithmetic, which would print `9..1` and blame the wrong endpoint | `TestAPeerWindowThatContradictsItselfIsRefusedInItsOwnWords` |
| a product version is not a protocol version | a 3.5.2 extension that speaks protocol 1 is admitted: the rule cites protocol versions only, and the recorded stale hello fails on its window and not on its build number | `TestTheProductVersionIsNotPartOfTheWindow` |
| facts the process has not earned | `New` refuses a `connectorVersion`, `os`, `keystoreBackend` or `schemaVersion` that the command's own result schema rejects, and a build that has not wired Core, pins or a keystore reports nothing about them instead of guessing | `TestTheResponderRefusesToBuildOnFactsItsOwnResultRejects` |

**Three readings this package had to make.** `packages/schema/protocol/commands/connector.hello.json#/errors` names
five codes, and `BadRequest` — whose taxonomy meaning is literally "missing args, wrong shape" — is not one of them,
so an unreadable hello is answered `HandshakeFailed`, which is also what actually happened and which §8's second rule
of `docs/architecture/25-native-messaging-protocol.md` keeps inside the set the command declares it can return. The
two numbers a compatible answer carries are this connector's own `protocolVersion` and
`minCompatibleProtocolVersion`, not the intersection: the peer applies the same rule to reach its own verdict, and the
recorded handshake echoes the connector's numbers back unchanged, so the Go answer does too. And a refusal message
names the schema key at fault and never its value, because
`packages/schema/protocol/framing.json#/senderPolicy/refusals/loggedFields` rules args out of a log line — which is
why the undeclared-field message names no field: past that boundary the key itself is peer-chosen text.

**One method, one connection, no lock.** `Session` carries no mutex because `internal/peer` calls its handler inline
in the read loop, so a Session has exactly one caller; two connections from one sender are two Sessions, which is the
same per-port/per-sender split §4.8 had to make in the other direction.

**What this package does not do.** It resolves nothing about the sender — that is the platform's answer, ADR-0004
rule 3, and the host's to make. It writes nothing to the wire, which is §4.7's job. It produces no `coreVersion`,
`pinnedCoreVersion`, `schemaVersion` or `keystoreBackend` of its own: those are injected, and until
`cmd/nexora-connector` reads pins and opens a keystore the honest answer omits them — the last test in the table is
written to keep that honest rather than to let a half-wired build claim a Core it has never found. And it does not
reconnect: `VER-13`'s closer is a running process re-running this frame after a teardown.

**The disagreement this slice found, in the corpus rather than in the plan.** Reading
`packages/schema/fixtures/protocol/handshake.jsonl` frame by frame made the connector's two halves meet, and they do
not agree. The fixture records a request for `core.snooze` whose answer is `UnknownMethod`; `internal/envelope`
asserts that its validator refuses exactly that frame, as one of the corpus's two named negatives; and a request the
codec refuses never reaches the dispatcher whose one documented failure case is answering it. Both positions cite the
same data and both are on record — the codec's strictness is what makes the replay mean something, and the recorded
answer is what makes `unknownMethodBehavior` an answer rather than a teardown. Which one the composed process keeps is
§9's ninth question, the first question in that list that two of this repository's own packages raised against each
other. Until it is answered, the reader in these tests tolerates that one refusal by id and fails on any other, so the
day the answer changes the test says so.

**The day the answer changed was the next slice.** §9's ninth question turned out to have three pieces of data older
than itself answering it — `docs/adr/0007-typed-versioned-command-allowlist.md` rule 2 says an unknown method MUST be
*answered*, `packages/schema/protocol/envelope.schema.json#/properties/method` declares a pattern and no membership
rule, and the Node harness sends those two frames on and requires the recorded answer — so the codec was the half out
of step with the contract it mirrors. §4.10 records the change, the test that replaced the one it removed, and the
clause in `recordedHellos` that this paragraph describes becoming unnecessary.

**Measured on this host**, Go 1.27.3: `gofmt -l connector` prints nothing, `go -C connector vet ./...` exits 0, and
`go -C connector test -count=1 ./...` adds `ok github.com/k2dms/nexora/connector/internal/hello` — ten tests, every one
named in the table above — so that invocation now prints six `ok` package lines. Still no runner has measured this
tree.

**No queue row moves on this slice either, and the reason is not §4.8's reason.** The row whose mechanism this is
lives in `docs/verification/live-matrix.md` as `LIVE-01`: "`connector.hello` negotiates: compatible version → session
with the capability set; incompatible → `VersionMismatch` in **both** directions", and the mechanism that row names is
"Go table test **+ TS client test**". The Go half exists and runs on this host; the TS half is a `nexora/` client,
which §6 keeps out of this iteration, and a `YES` in that legend means executed and recorded — for a negotiation, two
endpoints agreeing. No `VER-NN` row covers the window rule at all: `VER-13` is the reconnect that re-runs this frame,
`VER-18` is the pipe version that does not exist, and `VER-49` is the framing row §4.7 left open on a heartbeat this
package never sees. So there is nothing here to close, and nothing here to reopen.

---

### 4.10 M2's fourth slice: the boundary §9's ninth question turned out to have an answer for (`connector/internal/envelope/`, 2026-11-05)

**The question, and the three pieces of data that answer it.** §4.9 recorded the two halves of `connector/` refusing
each other: the codec rejected a request for a name the allowlist does not publish, `connector/internal/peer/peer.go`
ends the stream on anything the codec rejects, and so the exchange the corpus records —
`packages/schema/fixtures/protocol/handshake.jsonl` asking for `core.snooze` and being answered `UnknownMethod` — could
not happen in one process. The answer was not a preference between two defensible readings; it was already written
down, three times, in data older than the question.

| Data | What it says |
|---|---|
| `packages/schema/protocol/envelope.schema.json#/properties/method` | a `pattern` and nothing else. Membership lives in the *description*, which is prose; the validator that owns this file does not enforce it, so a codec refusing unpublished names was enforcing a rule the schema it mirrors does not carry |
| `docs/adr/0007-typed-versioned-command-allowlist.md` | rule 2: an unknown method MUST produce `UnknownMethod` and MUST be answered before any state is read or written. The verb is *answered*, and a teardown is not an answer. The ADR was accepted before either Go package existed |
| `packages/schema/scripts/verify.mjs` | the Node half reads the same two frames, sends them on, and asks only that a name outside the allowlist produce `UnknownMethod` and nothing else. Under doc 25 §2's "one contract, two implementations", the Go codec was the implementation out of step |

**The change.** `Parse` keeps the dotted-name pattern — `core:snooze` with a colon is still a rejected frame, and
`TestTargetedViolations` still says so — and no longer consults the projection for membership. The package comment now
states the boundary in one sentence: this package answers "is that a frame", and only `internal/dispatch` answers "is
that a method".

**What the replay test lost, and what it has instead.** The exception list is gone from
`TestReplayProtocolFixtures`: the assertion is now that **all 112 corpus frames parse**, with no id excused, and the
two named negatives are still named — by id *and* by the unpublished method the corpus pairs them with — and are
required to still be in the corpus, so a fixture that quietly stopped recording them fails this test instead of
weakening it. The refusal itself moved to `TestTheUnpublishedNamesInTheCorpusAreAnsweredHereNotInTheCodec` in
`connector/internal/dispatch/`, which reads the same files, parses them with the real codec, dispatches them through
the real front door, and compares the verdict with the answer the corpus records: same code, and the taxonomy's
`retryable` equal to the flag on the recorded error. That is the stronger of the two assertions, and the first test in
this module where two of its packages and the corpus meet in one sentence.

**Why this is not a loosening.** Removing a check in front of a dispatcher deserves the question "what gate went
away?". None did. `internal/dispatch` resolves the name through `contract.LookupMethod` — the generated closed set,
no prefix match, no default handler, tested against a name one character from a real one, a capitalised one, a bare
word and the empty string — and `New` refuses to build a dispatcher holding a handler for an unpublished name, which
§4.8 records as "the fallthrough `deniedByDefault` forbids, wearing a Go map". There is no path from a frame the codec
accepts to a handler the allowlist does not publish. What moved was a duplicate gate standing in front of the gate,
whose only observable effect was the teardown.

**What §4.9's fixture reader no longer tolerates.** `recordedHellos` had a clause excusing one refusal by id and
failing on any other. It is gone, and the reader now fails on any rejection at all — which is the assertion that would
have caught this in M1, sitting in a test that was already reading the file frame by frame.

**Measured on this host**, Go 1.27.3: `gofmt -l connector` prints nothing, `go -C connector vet ./...` exits 0, and
`go -C connector test -count=1 ./...` prints the same six `ok` package lines §4.9 printed, with
`replayed 112 protocol frames, every one of them accepted as a frame` where §4.6 quotes `(110 accepted, 2 refused)`.
No runner has measured any of §4.7 to §4.10 yet — §7 records that the three slices before this one are on this branch
and not on `nexora/master`, and this one joins them.

**No queue row moves, for a new reason.** Nothing in `packages/schema/verification.json` asserts *where* the
`UnknownMethod` verdict is produced: `VER-13` closes by `LIVE-13`, a reconnect no running process has performed, and
no row asks which package refuses. What this slice changes is the queue in §6 — the two items it gated on §9's ninth
question, `internal/chunk` and `cmd/nexora-connector`, are unblocked, and §6 now says so. `VER-49`'s heartbeat clause
stays exactly where §4.7 left it: §9's eighth question is still open, and this answer says nothing about which frame
carries a beat.

### 4.11 The gate behind the gate: what `connector/internal/chunk` found before any of it was written (2026-11-05)

§4.10 unblocked two items, and the first thing done with the unblocked one was to read the contract it would
implement. `connector/internal/chunk` is still not written, because the contract does not say what the package has to
do. Nothing in this section is a guess about the design; every line below is a field, a fixture, or a check that either
exists or does not.

**What the chunk contract does say, and it says well.** Slice metadata: four required fields, `chunkCount ≥ 2`, no
`chunkId`, one `contentHash` per group, sequence order enforced, a gap failing the whole import with
`IntegrityMismatch`, validate-then-apply, total bytes bounded, and the envelope copy mirrored byte-identically in
`args.chunk`. All of it is asserted in `packages/schema/scripts/verify.mjs` and every clause of it is GREEN. That is
exactly the problem: a green block over the part that was specified made the part that was not invisible.

**What no file states: the slice body.**

| Data | What it holds | What it does not hold |
|---|---|---|
| `packages/schema/protocol/envelope.schema.json#/properties/chunk` | four metadata fields, `additionalProperties: false` | any payload field, and no way to add one without editing the closed object |
| `packages/schema/protocol/commands/profile.import.json#/args/properties` | `profiles`, `servers`, `mode`, `contentHash`, `chunk`, `backupBeforeWrite`, `credentials` | a byte carrier: the two list fields are arrays whose `items` is a `$ref` to a whole-document schema, so a fragment of a serialized document fails validation rather than passing through |
| `packages/schema/fixtures/protocol/import-chunked.jsonl` | `chunkCount: 3`; slices 0 and 1 with `args.profiles: []` and no `args.servers`; slice 2 with the whole document; `encoding: "base64"` on all three; one whole-document `args.contentHash` repeated by every frame | a decision. Two empty frames declaring a *bytes* encoding of bytes no field holds is the corpus declining to take a side |
| `packages/schema/protocol/framing.json#/oversizedPayloads/mechanism` | `ordering`, `atomicity`, `cap` — all three written as if a group contributes bytes | the byte string those three are written against |

**The reading the data does remove.** "The earlier slices are announcements and only the last frame carries the
document" is what the fixtures literally show, and it cannot be the contract: `#/properties/chunk/description` in the
envelope appears "when a payload exceeds the 1 MB extension-to-host frame limit", and under that reading the frame that
must carry the whole document exceeds §4's sender cap and would be refused by §4.7's own peer before it reached a
writer. So one of the three is decided and two survive — the fragment model (`contentHash` over the concatenation of
decoded bytes, needing a field that does not exist) and the subset model (each slice a valid subset, `contentHash` over
the canonical serialization of a union, needing a canonical form that does not exist). The two disagree about which
byte string `chunk.contentHash` names, and both the gap rule and validate-then-apply are written against that string.

**What this slice therefore is.** Not the package, and not a guess at the field. It is the gap put where the corpus keeps
gaps, so that it cannot be inherited silently by Phase 2: a row in §13 of `docs/architecture/25-native-messaging-protocol.md`
carrying that document's deferred claim in the state column, a paragraph in its §5 that names the gap and defers, a row
`VER-76` in the queue, an entry `ODEC-34` in the register with the two survivors and the excluded reading written out,
and the argument in doc 40 §4 as a sixth kind of "why Phase 1 could not answer": the answer was hidden by a check that
passes. The chain of coupled counts that had to move together — `marker.counted`
202→203, the queue 75→76 rows with `no-code` 53→54, owner `connector` 34→35 and closing kind `assertion` 15→16, anchor
entries 152→153, the register 33→34 entries with `contract` 11→12, and `citation.counted` 53→54 across 14 documents —
is asserted by `verify.mjs` on every run, which is the only reason appending one row to two JSON files is safe.

**A second thing this slice fixed, unrelated to chunking.** Inserting the §5 paragraph moved doc 25 down by 21 lines,
and two citations in `docs/architecture/32-secrets-keystore-masking.md` — §10's coverage row and §11's opening
sentence, both into doc 25 §8's rule 4 — went stale without the guard noticing, because `path:line` citations only fail
when the number exceeds the file. The numbers were re-read and repaired. This is the failure mode
`tools/check-doc-evidence.mjs` documents for itself at the top of its own source; the corpus's answer for a mutable
target is a named constant or a pointer, and doc 25 §8 is neither, so the repair is the mechanism here.

**Measured on this host.** `node packages/schema/scripts/verify.mjs` → green, **1217 checks** — the same number as
before the row was appended, because a queue row is data and adds no assertion; `node tools/check-doc-evidence.mjs` →
green; `node tools/check-no-secrets.mjs` and `node tools/check-legacy-backup.mjs --selftest` → green; `go -C connector
test -count=1 ./...` → the same six `ok` lines §4.10 printed, unchanged, because no Go file moved in this slice. The
print of the evidence guard is §4.4's sentence and §5's bullet.

**One row appears; none closes.** The queue went up, which is the opposite of what the last four entries in §4 did. It
is the honest direction for this find: `VER-76` is `no-code` and gated by `ODEC-34`, and until the body field exists
there is no test to write and no package to name — so the correct output of reading the contract was a row, not a
green tick.

### 4.12 M2's sixth slice: the contract sentence that named the wrong process (2026-11-05)

**Found by reading `framing.json` end to end in order to say precisely what the second hop does not have.**
`packages/schema/protocol/framing.json#/senderPolicy/rateLimitFacts/countedWhere` said the budgets are counted by
"**the host**, before dispatch", `docs/architecture/31-security-model.md` §8 repeated the phrase as though it were its
own reasoning, and the code counts in the other process — `Dispatcher.charge` in
`connector/internal/dispatch/dispatch.go`. The package comment had already been walked around the contradiction rather
than through it: it quoted the field's entire second clause, "before dispatch: a request refused by budget never
reaches the handler, so it cannot half-apply", and dropped the two words that came before them.

**The wrong reading is not inaccurate, it is unimplementable.** A scoped budget names a method — every row of
`#/senderPolicy/rateLimits` is keyed by one — and the method name is
inside the envelope, which `docs/architecture/25-native-messaging-protocol.md` §2 forbids the shim to open: it
"does not translate, re-serialize, or interpret payloads — it enforces the sender allowlist and the byte limits, then
pumps". For the shim to know that this frame is the ninth `server.test` of its window it would have to parse the
payload, and to parse the payload on the second hop it would have to be told who sent it, which is the field
ADR-0004 rule 3 puts outside the envelope. The contract asked for a control that can only exist by breaking two others.

**Three signs name the Connector, and none of them is the code.** (1) The field sitting one line above in the same
object, `#/senderPolicy/rateLimitFacts/scopeOfBudget`, scopes every budget "per sender id, per **Connector** process";
a budget one process spends and another expires is not a budget. (2) The reason attached to the guard below it,
`#/senderPolicy/refusals/amplificationGuard/why`, is the cost of answering "on the only process that holds the
keystore" — the refusal loop it defends is one the Connector suffers, not one the shim observes from outside. (3)
`#/senderPolicy/portsPerSender` is the one row of that whole policy the shim really does own, precisely because closing
the oldest port is answerable from an opened connection without reading a byte of payload — which is the same test the
three rows above fail. So the answer was in the file all along; it was in the fields nobody quoted.

**What the repair is.** Data first: `countedWhere` now names the Connector, keeps the half-apply clause, and carries
the shim's impossibility as its own second sentence, so the next reader gets the reason and not just the verdict.
Then the document that had been repeating the wrong word: §8 of `docs/architecture/31-security-model.md` states the
impossibility rather than a preference, and names the three things the shim actually enforces. Then the comment that
half-quoted the contract, which now quotes it whole — a comment that cites half a sentence is the mechanism by which a
contradiction like this survives a green test suite.

**Measured on this host.** `node packages/schema/scripts/verify.mjs` → green, **1217 checks**, the same count as before
this slice: it adds no assertion and removes none, because it changes what one field says.
`node tools/check-doc-evidence.mjs` → green at **2262 / 508 / 1340**, §4.4's seventh move — no `path:line` figure, one
citation by pointer, five names. `node tools/check-no-secrets.mjs` → green, 435 files.
`go -C connector vet ./...` → 0, `gofmt -l connector` → empty, `go -C connector test -count=1 ./...` → the same six
`ok` lines §4.11 printed, because the Go delta is a comment. The figures are this slice's own commit's; §9's next
entry and the row it gates will move the same three numbers when the slice that registers the second hop lands.

**Two things deliberately not done.** No `ODEC` entry and no `VER` row: nothing remains undecided once the field names
the process that has the information, and a register entry for a question the data already answers is the defect doc 40
calls an entry the register invented. And no new assertion, although the cheap honest one is obvious — the harness
reading §8 and `countedWhere` for the same process name, so that the two cannot drift apart again. It is queued rather
than written here, because `packages/schema/scripts/verify.mjs` is the file the slice that registers the second hop is
editing its distribution assertions in right now, and a third writer in that array produces a check count no run of
this repository will reproduce.

### 4.13 M2's seventh slice: the process, and the identity it has to be told (`connector/cmd/nexora-connector/`, 2026-11-05)

**What exists now.** A Connector that runs. `connector/cmd/nexora-connector` opens one Native Messaging stream on
stdin and stdout — a ~15-line `duplex` joins the two descriptors because nothing else does it and `internal/peer` takes
one `io.ReadWriteCloser` — puts `internal/peer`'s host-side limits on it (`peer.ToExtension()`: reads capped at the
extension's 1 MiB, writes at the host's 1 GiB), and hands every frame that arrives to `internal/dispatch` with
`internal/hello` behind it. Two transports, one function: the length-prefixed mode is the contract's, and `--jsonl` —
one frame per line, one answer per line, exit at EOF — exists because a human at a shell cannot type four length bytes,
and shares `service.respond` rather than reimplementing it, so the affordance cannot drift from the transport.

**What it registers, and why that is the honest list.** `connector.hello`, and `connector.version` — because both
fields that command's result requires are data this process holds: its own release and
`packages/schema/protocol/framing.json#/protocolVersion`. `coreVersion`, `schemaVersion`, `pinsVersion` and `buildId`
are optional and belong to a supervisor, a pins reader and a release tool this build does not have, so they are absent,
which is `internal/hello`'s stance for the same fields. Everything else published — `core.*`, `config.*`, `profile.*`,
`keystore.purge`, `diagnostics.get`, `connector.health`, `server.test`, `connector.events.subscribe` — is left
unregistered, so the front door answers it `Internal` for what it actually is: a method this build has no handler for.
`New` refuses a dispatcher built around an unpublished name, and an unpublished name is answered
`UnknownMethod` rather than torn down (ADR-0007 rule 2), which the recorded corpus exercises at exactly that frame.

**It refuses to serve before it is told who it is talking to.** `--sender-id` and `--allowlisted` inject the answer
the host's platform will resolve in production; `--allowlisted` defaults to false, so an omitted flag allows nothing
rather than allowing everything; the process prints one line to stderr naming the injection as a stand-in and the
undecided transport as its reason, and exits 1 with no sender id at all. That line is the point, not decoration: the
front door's `Identity` has no producer, §4.14 registers why, and a binary that silently assumed an id would have
buried the question inside a working demo.

**Measured, not asserted.** `go -C connector test -count=1 ./...` → **seven** `ok` lines where §4.7 to §4.12 printed
six; `gofmt -l connector` empty, `vet` and `build` exit 0; `check-no-secrets.mjs` → green, and its file count moves
with this commit because `git ls-files` is how it enumerates. The nine tests in `main_test.go` are of two kinds: the
front door's table (hello answered, `core.snooze` answered not killed, `HandshakeFailed` before the negotiation,
`Internal` for a published method with no handler, a non-allowlisted sender answered only `UnauthorizedExtension` with
its `password` asserted absent from the bytes, and two malformed lines ending the stream exactly as `peer.Serve` does),
and the transport's (two `peer.Peer` over `net.Pipe`, where the gate opens only when a hello has crossed it; the
recorded `handshake.jsonl` replayed through this dispatcher).

**Then the binary was run, on purpose, against the corpus's own frames.** Feeding the six `type":"request"` lines of
`packages/schema/fixtures/protocol/handshake.jsonl` into `--jsonl --sender-id demo.extension --allowlisted
--connector-version 1.0.0` answers, in order: `ok` with 13 capabilities, `ok` with `1.0.0`/`1`, `Internal` for
`core.status`, `UnknownMethod` for `core.snooze`, `VersionMismatch` for the fixture's too-old second hello, and
`HandshakeFailed` for `connector.health` behind it. The last two are the finding. The negotiation this process
established was *withdrawn* by a mismatching hello — `Session.Record`'s stated rule, "a `VersionMismatch` on an
already-negotiated connection is the only thing that can withdraw it" — so the frame that follows is refused by a gate
whose message says it arrived before the hello. The message is the gate's generic wording and the state is correct;
naming that wording is now a line in §6, not a fix smuggled into a slice about a binary.

### 4.14 M2's eighth slice: the second hop registered as data, and the one entry that turned out not to be a question

**What landed.** `docs/architecture/25-native-messaging-protocol.md` §2 now carries three paragraphs titled "The one
thing §2 does not decide" — the only deferral site for the new entry — and its invariant table gained the labelled row
`VER-77` anchors on. The register holds `ODEC-35` (`adr`, four candidates of which one, a sender field in the envelope,
is recorded as **excluded, not open**), and the queue holds `VER-77` (`no-code`, `connector`, closing by an assertion
nothing prints yet). The coupled figures move together, and these are the eighth of the series: 204 labelled lines, 77
rows, 55 sites, 35 entries, with `no-code` 55, `connector` 36 and `assertion` 17 in the distribution the harness
insists the data has — and the register's `adr` group at six, one more than it has held since the register was written,
because this is the first entry no field edit can answer. Doc 32's two citations into doc 25 §11 were re-read and moved by fifty lines (`:184`→`:234`,
`:183-184`→`:234-235`) — the same failure §4.11 named, arriving on schedule for the third time, because the slice that
registers a gap in a document also makes that document longer. Re-tallying the queue to write that row is also what
found a number no guard reads: doc 39's §2 had been saying six rows are `needs-human` where the data has held five
through every slice that cited it — the sibling of §4.4's count word, and corrected here rather than silently, because a
stale tally in an inventory document is a claim about the corpus, and the corpus is the product.

**Three of doc 39's own tallies were one behind the data, and no build ever failed for it.** Reading §5 aloud to write
the new row's numbers found `connector` listed as 34 where the check has asserted 35 at `4997081`,
`assertion` listed as 15 where it asserted 16, and §2's prose saying six rows are `needs-human` where the data has held
five across every slice that cited it. The reason is in the check's own wording: the assertion whose message reads
"the owner distribution doc 39 reads aloud is what the data says" compares the data against a constant and never opens
`docs/architecture/39-verification-matrix.md`. It is a sound check and a false name — §4.12's shape one level down, a
sentence describing more than the mechanism behind it does, green for as long as nobody read the sentence. The three
figures above are corrected from the data in this commit; the assertion that would keep them corrected is queued in §6.

**The argument for why this is `adr` and not a field.** Four statements in this corpus close on each other and leave
nothing between them: the connector's `Identity` must be resolved by someone and its own comment says it is not in the
payload; the envelope is closed at fourteen properties, none a sender, and ADR-0004 rule 3 forbids a payload field from
*becoming* identity; the shim may not stamp one on, because an injected field is the translation doc 22 §2 forbids;
and the mechanism doc 31 §4 names for this crossing — "the inherited pipe and the per-user lock file" — is a lock whose
recorded fields are `{pid, pipeVersion, startedAt, configHash}`, with no sender among them. Three shapes survive
(which pipe the host opened; a handshake before the first envelope; OS peer credentials that authenticate the *host*
and still not the extension) and they disagree about where identity lives, which moves ADR-0005's boundary. A field
edit would hide that; a new ADR carries it.

**One candidate entry was checked and refused, and the check is the part worth keeping.** The same reading surfaced a
second question — who counts the rate budgets, and is a budget per sender — phrased as though nothing had a test. It
does. `Dispatcher.charge` indexes `d.spent[id.SenderID][key(rl)]`, and
`TestABudgetIsPerSenderAndNotPerProcess` in `connector/internal/dispatch/dispatch_test.go` spends one sender's
`diagnostics.get` budget, refuses that sender's second call with retryable `Busy`, admits a second sender's first call,
and asserts the handler ran exactly twice — quoting `#/senderPolicy/rateLimitFacts/scopeOfBudget` in its own comment
while it does it. So no `ODEC-36` and no `VER-78` were added: §4.12 had already answered which process counts, and the
per-sender half was already answered by code and by a named green test. A register entry for a question the tree has
answered is the defect doc 40 §3 calls a question the register invented, and the harness cannot catch it — the count
would have agreed. The only durable form of this is §8's decision 17.

**Where the two slices touched.** §4.12 repaired `countedWhere` while the hop-2 reading was in progress, so the
sentence "framing.json's `senderPolicy` describes that one hop" — written from its `#/description` — was true about
identity and false about budgets by the time it landed. The document and both `why` fields now say the sharper thing:
the policy's identity facts resolve from an OS event on hop 1, its budgets are spent in the Connector, and that
mismatch *is* the gap — the process that spends a sender's budget is not the process that can see which sender it is.
Doc 40 §5 gained its seventh paragraph for `ODEC-35`, and that paragraph's own clause ("a transport for a process
nobody has built") went stale inside the iteration §4.13 describes, which the paragraph now says out loud instead of
quietly rewriting.

---

### 4.15 M3's first slice: the serializer, and the determinism clause that would have eaten three fields (`connector/internal/compile/`, 2026-11-05)

**What landed.** `connector/internal/compile/emit.go` and `emit_test.go` — stage 5 (emit) and stage 6 (hash) of
`docs/architecture/27-config-compilation-pipeline.md` §2, and no other stage. The package serializes a document, sorts
keys at every depth, keeps arrays in input order, formats integers only, and hashes the bytes it wrote. It resolves no
profile reference, splits no rule, allocates no port, embeds no block target, and does not look at what it emitted for a
loopback bind: stages 1-4 and stage 7 have no code, so this slice creates no `connector/testdata/config/` and no golden
file — `VER-28` keeps its row, and `VER-78` arrives beside it precisely because a byte-stable serializer is not a
compiler. This is the eighth `ok` package line of the series and the first Go code in the repository whose subject is a
config rather than a frame.

**Two decisions the emitter refuses to make, and how it says so.** `configHash` is documented as the sha256 of "the
exact bytes written", and by the time a config is on disk two different byte strings exist: the document the compiler
builds, whose credential positions are `keystore://<ref>` handles, and the materialized file, which holds the
credential. Which one the hash covers decides whether a rotated secret changes the hash, whether the hash identifies
intent or what Core loaded, and whether a logged digest hashes a secret-bearing document — and the corpus answers none
of it: `packages/schema/protocol/commands/config.apply.json#/result/properties/configHash` is a pattern with no
description, and `docs/architecture/23-unified-server-model.md` §8's own "not part of intent hash" uses a term defined
nowhere in the repository. So `Emit` and `Hash` are two calls, the const `HashInputNote` names the open decision where
the code that would have made it stands, and the register carries `ODEC-36` (`contract`, three candidates) gated to the
queue by `VER-78`, both anchored on the new labelled invariant row in doc 27 §9. The second refusal is smaller: the
contract does not say whether HTML characters are escaped, so the package says out loud that they are not, and the test
suite then cross-checks the *ordering* claim against `encoding/json` with escaping disabled — two independent encoders
agreeing on the canonical bytes of the shape doc 23 §9 calls A.

**What the eleven tests buy.** Re-emitting shape A two hundred times and requiring identical bytes is the test for
§3.1, because Go randomises map iteration and an unsorted encoder would change the hash on every call — the failure this
contract exists to prevent, not a style preference. A float at `config.inbounds[0].port` is refused with that path in
the message rather than formatted, because §3.4 forbids exponent form and `1.0` while defining no decimal rule, and no
emitted shape in doc 23 §9 contains a non-integer to invent one for; an unsupported type is refused the same way.
Invalid UTF-8 is refused rather than replaced, because a replacement character would reach the hash and two machines
would then disagree about a config neither of them had. A `[]string` of `serverIds` comes out in the order it went in.
A self-referential document is refused at thirty-two levels instead of overflowing the process that is supposed to keep
Core running. The hash is pinned to `8e15609e…` for shape A, checked against the `^[0-9a-f]{64}$` pattern every `*Hash`
field in the corpus uses, and shown to move when one port moves.

**The contract defect this slice found — and it is the same shape as §4.11's and §4.12's.** Doc 27 §3.2 said the
compiler would apply "the same stripping rule the fixtures use": any field named `…At`, `…Ms`, `…Seconds` or `…seq` is
time-shaped and excluded from hash-relevant comparison. Writing the emitter made that sentence checkable, and it fails
in both directions. It would strip `stickySeconds`, `cooldownSeconds` and `probeIntervalSeconds` — three fields with
declared ranges, defaults and reasons in the data (`packages/schema/connector-profile.schema.json:59`,
`packages/schema/connector-profile.schema.json:75`, `packages/schema/connector-profile.schema.json:77`), each of which
changes what the Connector does. It would keep the two legacy fields that actually carry a clock, because neither name
ends in those suffixes: `revision`, a timestamp rendered in hex (`omega-pac/src/utils.coffee:4`, stamped by
`updateRevision` at `omega-pac/src/profiles.coffee:153`), and `lastUpdate`, an ISO string
(`omega-target/src/options.coffee:805`). §3.2 now states the exclusion structurally — the compiler emits only keys it
built from the model, and a golden file is what checks it — and names where the suffix list does belong:
`packages/schema/scripts/verify.mjs` "idempotent replay returns the same result: ${file}#${req.id}", which compares two replayed protocol *results*, a fixture device and not a
compile rule. Doc 27 §3.1 gained the other half of the same reading: the legacy engine never sorted anything either, it
iterates its profile table (`omega-pac/src/pac_generator.coffee:32`), so byte-determinism is an obligation NEXORA takes
on rather than inherits — which is why the NO on that row is honest today and not a slip.

**A citation defect, found while the guards were being re-read.** Five documents cite doc 27 by line number, and every
number in that set moved when this slice made doc 27 longer — the ordinary §4.11 failure, arriving for the fourth time.
Re-reading them found one that had never been right: doc 33's index row cites "the compiler's dependency on install
state", and the number it carried pointed at the *footer* of doc 27 — a `See also` line, non-blank, and not the claim.
`tools/check-doc-evidence.mjs` asserts that a cited line exists and has text on it; it cannot assert that the line holds
the sentence the prose describes, so a wrong number that lands on any non-blank line is green here indefinitely. The
durable form is §8's decision 19: re-read the sentence beside a citation, not only its number.

**And a third defect, of a kind no guard was written for: a sentence that quotes the label becomes a claim in the
queue.** The two paragraphs above each reproduced the label in prose — one in doc 27 §3.1 explaining why the row reads
that way, one here — and `packages/schema/verification.json#/marker/pattern` reads a line, not a role, so any line
carrying that spelling is a deferred claim owed a row. The partition came up with 207 claims against the 205 the queue
declares and named both new lines unowned. No row was added: the obligation those sentences quote is owned already, by
the §9 line they point at, and re-anchoring one obligation in two places would inflate what Phase 2 is billed for. Both
now say that the row *reads NO* instead of reproducing the label, which keeps the sentence and drops the double count.
What is worth carrying forward is that the corpus's deferral total is a property of its prose style, so a slice that
explains a deferral has to state it rather than quote it — and that the harness caught this on its own, in the same run
that was already checking the arithmetic of the queue. The finding re-litigated itself while being written: the sentence above, which says that a quoted label
becomes a claim, carried a quoted label, and §8's decision 20 tripped the same counter on its first draft. A rule about
prose style is hard to state without performing it.

**Gates.** `gofmt -l connector` prints nothing, `go -C connector vet ./...` is clean, and
`go -C connector test -count=1 ./...` now prints eight `ok` lines where §5 recorded seven. The schema harness is at
1217 checks with its distributions moved to `no-code` 56, 78 rows, `connector` 37 and `assertion` 18; the register is
at 36 entries over 56 sites with `contract` at thirteen. `node tools/check-no-secrets.mjs` enumerates **439** files
with this package staged — two more than §4.13 recorded, which is §5's standing note about that guard reading
`git ls-files` come true in the ordinary way.

### 4.16 M3's second slice: the five stages the serializer was waiting for, and the three goldens that pin them (`connector/internal/compile/`, 2026-11-06)

**What landed.** `model.go`, `resolve.go`, `split.go`, `ports.go`, `outbound.go`, `compile.go`, `validate.go`, six test
files beside them, `golden_test.go`, and `connector/testdata/config/` with three files in it. That is stage 1 (resolve),
stage 2 (split), stage 3 (allocate), stage 4 (embed) and the document-building half of stage 5 of
`docs/architecture/27-config-compilation-pipeline.md` §2, plus the assembler that runs them in one call and the part of
stage 7 that needs no Core. §4.15's package was an encoder with nothing to encode; this is the first code in the
repository that turns a desired state into a config byte string and a `configHash` for it. The `ok` line count stays at
eight — no new package is born here — but the package's test functions go from eleven to forty-nine, and the first
line of that shift is a decision about fixtures: the tests read `packages/schema/examples/`, not a copy of it under
this directory, which is ADR-0020's golden rule as doc 27 §3 applies it. One artefact, two readers — ajv in the
contract suite, Go here — and no third thing allowed to drift. `corpus(t, name)` is the whole mechanism, and it is why
`connector/testdata/config/` holds three files instead of three hundred.

**What the goldens pin, and what they cannot.** Three of doc 27 §3's six fixtures. `shape-a-vision-tcp-tls` and
`shape-b-reality-xhttp` are doc 23 §9's first two canonical configs as this compiler emits them;
`multi-server-ordered` is the third thing a single-machine test can actually prove about determinism — two servers
handed to the pipeline in one order and the profile naming them in the other, pinned to the tag-sorted bytes, so the
golden is the assertion that arrival order does not reach the hash. `TestCompilingTwiceProducesTheSameBytesAndHash`
already re-emits shape A two hundred times; what it cannot do is catch a change of *mind*, and a golden file can, which
is why `-golden.update` exists as an explicit, named act of re-blessing rather than a silent rewrite. The other three
fixtures are not pinned, and each is unpinnable for a different reason, recorded rather than worked around: shape C
refuses at `headersRef` (`ODEC-36`), the core-plane fixture has no spelled form for the rules it would emit
(`ODEC-37`), and the fail-closed fixture has no bytes in a Core config at all because
`docs/architecture/30-failure-policy.md` §7 puts the block in `chrome.proxy`.

**Two refusals the corpus forced, and where they live.** The first is the `headersRef` case: the model holds one
`keystore://` string where doc 23 §9's shape C prints an *object* whose key — `:authorization` — no schema in this
repository lists, so `outbound.go` refuses and names the register entry rather than inventing a map. The second is
bigger and is the reason `split.go` is a reporter and not a lowering engine: four positions in the emitted document —
an inbound's protocol, the protocol behind a `direct` or `block` outbound, where `sniffing` rides, and whether
routing's `network` and `protocol` are arrays or one string — are named by the prose and spelled by nothing. `freedom`
and `blackhole`, Xray's usual words for a direct and a null route, appear in no document, schema or example here, and
`routing-policy.schema.json#/$defs/coreOutbound` admits `direct` and `block` while its own description says only
derived tags are allowed. So a rule that needs one of those arrives in `unrepresentable[]`, which is doc 27 §7 rule 1
("report, never drop") implemented as bytes instead of as an intention — and the report is what `VER-28` and `VER-78`
are now described against, rather than the four fixtures a reader might have expected to find in the directory.

**The prose this slice had to correct, because the code made it checkable.** Six claims in the corpus were true when
written and are not true now, and one was never true. `ODEC-03` said `UnsupportedCombination` is absent from the
taxonomy; it is at `packages/schema/protocol/error-taxonomy.json:14`, with a transport-combination meaning — so the
question is whether that meaning stretches to a non-loopback bind, not whether the code exists.
`docs/architecture/23-unified-server-model.md` §3's transport tables and §5's secrets row both over-named fields:
`xhttpSettings` is `mode`, `path`, `host`, `extra` and `h2Settings` is `path`, `host`, neither of which is what the
tables said, and only `wsSettings` and `grpcSettings` carry a `headersRef`, so §5's promise of a `:authorization`
header for "gRPC / WS / XHTTP" was three transports' worth of a two transports' field. §9 of the same document claimed
the three canonical configs compile byte-for-byte. `docs/architecture/26-command-surface.md` explained an empty
`unrepresentable` by saying no compiler exists; a compiler now exists and it is the command surface that does not read
the report. doc 27 §9's rows for range disjointness and the inbound ceiling said *nothing* checks them, which was the
honest state in §4.15 and is `ports.go` today. Six queue rows were re-worded to match what their anchors now assert
(`VER-16`, `VER-20`, `VER-27`, `VER-32`, `VER-70`, `VER-73`) and the two golden rows describe three fixtures instead of
zero (`VER-28`, `VER-78`). No row was added and none deleted: the register and the queue have no state meaning closed,
and a row whose subject is half-met stays where it is, which is what §4.7's `VER-49` established and this slice follows.

**The register gained one entry, and the harness proved it gained one.** `ODEC-37` is `upstream`, gates `VER-28` and
`VER-78`, and was born from exactly two new deferral lines: doc 23 §9's sentence about the four spellings, and the §9
invariant row in doc 27 written by this slice to defer the same question. The first harness run after the pass failed five checks, all
of them arithmetic between prose and data rather than Go: an anchor rewritten underneath the row that cites it, a new
deferral line no entry claimed, the derived deferral count ahead of the declared one, the deferred-claim partition one
short, and the register document not yet naming the entry it had just acquired. The second run is green at 1217
checks — the same figure §4.15 recorded, because this slice writes no new assertion; what moved is inside existing
ones. The register is now 37 entries over 58 sites: `product` 14, `contract` 13, `adr` 6, `measurement` 2, `upstream` 2.

**And the secrets guard met Go's test convention.** `node tools/check-no-secrets.mjs` reported nine `uuid-literal`
findings in this package's test files, and it is right to: a VLESS user id *is* the credential the keystore holds, so
the guard treats its shape as a secret in every path that is not a fixture directory — and its exempt list
(`docs/**`, `packages/schema/**`, `connector/testdata/**`, `**/test/**`) has no slot for a Go `_test.go`, which by
convention sits beside the package it tests rather than under a `test/` directory. The ids were hoisted into one
documented `const` block at the top of `resolve_test.go` and annotated with the guard's own escape hatch, which no
file in this repository had needed before this slice; `--selftest` is still 13/13, so the gate demonstrably fires on
the shape it is being asked to forgive. The plain run now enumerates 455 files, sixteen more than §4.15's 439, which is
that slice's note about `git ls-files` repeating itself in the expected direction.

**Gates.** `gofmt -l connector` prints nothing, `go -C connector vet ./...` is clean, and
`go -C connector test -count=1 ./...` prints the same eight `ok` lines §4.15 recorded — the ninth will arrive with a
package that is not this one. `npm --prefix packages/schema run verify` → 1217 checks, green.
`node tools/check-doc-evidence.mjs` → 2282 `path:line`, 517 pointers, 1381 doc names.
What is still not here, and is the shape of the next slice: nothing reads a handle out of the keystore, so a compiled
config is a document with `keystore://` strings in its credential positions and has never been written to disk; stage
7's probe needs a running Core; and no command surface calls `Compile`, so `config.apply` still has no answer to give.

---

### 4.17 The keystore: a projected grammar, one file per credential, and a syscall pair for the protection (`connector/internal/keystore/`, 2026-11-06)

**What landed.** `ref.go`, `store.go`, `keystore.go`, `platform_windows.go`, `platform_other.go`, four test files beside
them, and a regenerated `connector/internal/contract/contract_gen.go`. Five sources, four tests, 24 test functions —
and the ninth `ok` line: §4.16 left its Gates paragraph saying "the ninth will arrive with a package that is not this
one", and this is a package that is not that one, so the eight became nine on the same command. It is the first Go code
in the repository that holds a credential, which is why most of what it asserts is absence: nothing on disk is the value
(`connector/internal/keystore/keystore_test.go:84`), no error string quotes one (`connector/internal/keystore/keystore_test.go:246`),
and a resolved secret prints as its reference and nothing else (`connector/internal/keystore/store.go:189`). ADR-0011
rule 6, turned into three test names.

**The grammar is projected, so it cannot be copied twice.** doc 32 §5 says the reference vocabulary is data, and
`packages/schema/envelope-keys.schema.json:143` is the line holding it: scheme, floor and shape as three `const`s, read
by ajv, read by `packages/schema/scripts/verify.mjs`, and now read by the Go generator into
`connector/internal/contract/contract_gen.go:139`. `connector/internal/contractgen/contractgen.go:283` is where the
projection refuses instead of copies: both floors must be built on the scheme they are anchored to, the entity and field
lists must agree with the registry's own, the identifier segment of the shape must be the ingest document's pattern
byte-for-byte (`packages/schema/protocol/commands/profile.import.json:85`), and
`connector/internal/contractgen/contractgen.go:304` will not emit a vocabulary with more — or fewer — than one
object-shaped field, because `headers` is the one ODEC-36 is about. One artefact, three readers, a build that stops when
they drift: §4.16's golden rule applied to a grammar rather than a fixture. The other half of that claim is a test, not
a generator: `connector/internal/keystore/vocabulary_test.go:108` walks every JSON file under `packages/schema/`,
parses each string that begins with the scheme, and keeps a floor on how many it found (`:144`) so the walk cannot go
quietly blind.

**Three refusals, and a fourth the store had to keep for itself.** doc 32 §11's table names `SecretRedacted`,
`InvalidProfile` and `KeystoreUnavailable` and says each sends the user somewhere different;
`connector/internal/keystore/keystore.go:48` is the map from this package's sentinels onto those projected names, and
`:98` is the check that the map still means what the table says — a code that went missing, or went retryable, stops the
package at init rather than at the point where a user would be told to retry something no retry can fix.
`connector/internal/keystore/ref.go:30` decides which one applies, and the order of its tests is the design: no scheme
prefix means a value in a reference position, and a value is never echoed
(`connector/internal/keystore/ref_test.go:82`); a proven prefix with an unproven shape is a malformed reference, echoed
only once the floor has accepted it. The fourth refusal is not in §11
and could not be: an empty value. The contract bounds a credential at the door
(`packages/schema/protocol/commands/profile.import.json:95`) and says nothing about a zero-length one, so the store keeps
a floor of its own and answers it with `SchemaMismatch`, recorded here rather than smoothed over.

**DPAPI is two syscalls and no dependency.** `connector/internal/keystore/platform_windows.go:21` reaches `crypt32.dll`
through `syscall.NewLazyDLL`, because the module has zero external requirements and ADR-0011 rule 1 names exactly one
API: `CryptProtectData` with no descriptor, no application entropy, `CRYPTPROTECT_UI_FORBIDDEN`, and a `LocalFree` of the
buffer the OS allocated. The `golang.org/x/sys` import a DPAPI binding usually arrives with would have been this
module's first; the hand-written `DATA_BLOB` is the cheaper thing to audit. What the call buys is checked at
`connector/internal/keystore/store.go:105`: the store refuses a protector that answers with the bytes it was handed,
which is rule 5's forbidden plaintext fallback made unrepresentable at write time, and
`connector/internal/keystore/keystore_test.go:100` proves the refusal with an `identityProtector` that is exactly the
plausible placeholder rule 5 fears.

**The seam is why the ubuntu runner has keystore tests at all.** `Protector` is an interface of two methods
(`connector/internal/keystore/store.go:17`), `New` takes one and `Open` does not; on anything that is not Windows
`connector/internal/keystore/platform_other.go:11` answers an error so `Open` fails closed, because ADR-0011 binds
Windows and rule 5 forbids filling the gap with a cipher nobody designed. That split lets 21 of the 24 tests run on
Linux against a base64 protector that is explicitly *not* protection: one blob per reference, temp-file-and-rename at
`0600`, rotation leaving nothing behind (`connector/internal/keystore/keystore_test.go:165`), a dangling handle answering
`InvalidProfile` and not `KeystoreUnavailable` (`connector/internal/keystore/keystore_test.go:136`) are properties of the
store, not of crypt32. The three that are crypt32's (`connector/internal/keystore/dpapi_windows_test.go:17`) are
build-tagged and ran on this host against the real DLL, in a temp directory: `TestDefaultDirIsADRsPath` checks the path
`Open` would have used without creating it, because leaving secret-shaped files in a user profile from a test suite is a
finding of its own.

**What this package does not do.** It mints nothing: `mintedBy` says the Connector generates the identifier when it
stores a value (`packages/schema/examples/envelope-keys.nexora.json:103`), and minting is a command handler's job, so
every reference in these tests was typed by a test. It masks nothing — doc 32 §8 puts masking at the sink, keyed by
value, and a store is not a sink. It purges nothing by scope: `keystore.purge`'s three scopes need the reference index
doc 32 §7 describes, and what exists is a directory of blobs whose names encode their reference, which is enough to
delete one handle and not enough to answer "which orphans does nothing reference". It enforces no ingest bound: 512
entries, 4096 characters and 32 keys are projected so the door that must apply them has a number to read, and that door
is `profile.import`'s argument validation, which is not written. And nothing opens it: `compile.Compile` takes no
`Store`, so a compiled document still carries handles in its credential positions and has still never been written out.

**Which is why ODEC-36's blocker is narrower and no smaller.** §4.16 recorded shape C refusing at its `headersRef`
because "no schema in this repository lists the keys". The mechanism half of that reason is gone: a
`keystore://…/headers` reference parses, resolves and reads out as a `map[string]string`
(`connector/internal/keystore/store.go:205`), and the key names come from the object a user imported rather than from a
schema that never enumerated them. What remains is the decision, and `connector/internal/compile/outbound.go:229` still
refuses for the right reason: whether that map is inside the hashed bytes or only in the written file is a question about
what `configHash` *means*, and no amount of code answers it. The comment above the refusal was rewritten to say so,
because a refusal that cites a missing mechanism once the mechanism has arrived is a map edited and not re-walked.

**The prose this slice had to correct, because the code made it checkable.** Five sentences said there is no keystore.
ADR-0011's Evidence line said "no keystore code exists yet" and its Verification line queued four deferred things under
one label; the round trip is now measured, so the ADR gained three VERIFIED bullets — the syscall pair, the fallback
refusal, the owner-only install — and its INFERRED row now names precisely what is still OS behaviour: that a same-user
process can decrypt, which is rule 7's residual risk and cannot be a test of ours. doc 32 §1's reading rule said
"almost every row, because there is no keystore code yet"; it now says which half became code and which four doors did
not. doc 32 §12's closing verdict keeps its Phase-2 label, because a table of commands still has no command behind
it, and adds that the store is code and nothing has run. LIVE-18's parenthetical said "no keystore code exists to
round-trip" — the sentence this slice makes false — and its verdict stays in the same Phase-2 column for the reason §1's
rule gives: a live row is a session, not a syscall. And `VER-23`, whose subject was that the backend does not exist,
moved state:
`no-code` to `no-run`, the queue's word for a check that is green where it ran and has not run where the claim needs it.
That is the first subtraction from the `no-code` column this queue has made — every earlier move of that number added a
row beside it — and it takes doc 39's distribution to 55 / 13 / 3 / 2 / 5, which the harness asserts rather than admires.
§4.16's rule that no row is added and none deleted still holds: a state is not a closure, and `VER-23` is queued at
LIVE-18 exactly as it was.

**And the secrets guard met the grammar it cannot exempt.** The ids this package's tests are written from are
uuid-shaped, and a VLESS user id is the credential the keystore holds, so the gate reads every one of them as a secret:
they are now two documented consts (`connector/internal/keystore/keystore_test.go:55`,
`connector/internal/keystore/ref_test.go:16`) and one deletion — the DPAPI round trip does not need its own identifier to
prove that crypt32 gives the bytes back, and the second it had was a fourth literal for no fourth claim. The remaining
finding is `connector/internal/keystore/ref_test.go:83`, whose whole purpose is to be a value in a reference position:
the shape the gate looks for, in a test that exists to be refused, so it is annotated rather than renamed. §4.16
established the form — hoist, document, annotate with `nexora-secret-allow`, keep `--selftest` at 13/13 so the gate
demonstrably still fires on the shape it is asked to forgive. The plain run enumerates 464 files, nine more than
§4.16's 455: the same `git ls-files` behaviour, this time counting the nine files of a package that did not exist.

**Gates.** `gofmt -l connector` prints nothing; `go -C connector vet ./...` is clean, and clean again under
`GOOS=linux`; `go -C connector test -count=1 ./...` prints nine `ok` lines, the ninth being this package. On this host
the keystore's 24 tests pass with the three DPAPI ones executing against `crypt32.dll`; on the ubuntu cell of the
`connector` job they will pass with those three not built at all, which is what the seam is for.
`npm --prefix packages/schema run verify` → 1217 checks, the same count as §4.16: this slice changes a state in the
queue's data and the distribution assertion that reads it, and adds no check. `node tools/check-doc-evidence.mjs` →
2314 path:line citations, 517 pointers and 1389 doc-name references. `node tools/check-no-secrets.mjs` → 464 files, no finding.
What is next is unchanged in kind and narrower in size: the materializer that reads a handle at the moment of writing and
the owner-only config file it would write into (`docs/architecture/32-secrets-keystore-masking.md` §13), then
`config.apply`'s command surface calling `Compile`, then the supervisor — §4.18 landed the first of those two and §4.19
the third, so what this sentence still owes is the surface — then stage 7's probe, the last of these still
needing a Core binary this host does not have.

---

### 4.18 The one place a value is plaintext: the materializer, and a write that leaves the previous config intact (`connector/internal/apply/`, 2026-11-06)

**What landed.** Two files, six exported names, thirteen tests: `Materialize`
(`connector/internal/apply/apply.go:39`) over a `Resolver` seam (`connector/internal/apply/apply.go:28`) and `Write`
(`connector/internal/apply/write.go:41`) with `Applied` (`:22`), `AppliedHash` (`:114`) and `FileName` (`:17`). The
compiler's own package doc named the writer as one of the things deliberately not in it
(`connector/internal/compile/emit.go:6`), so this is the package that half of M3 was waiting on rather than a
second answer to a question `compile` already answers.

**It walks for the grammar, not for the key names.** `docs/architecture/22-component-contracts.md` §9 says every layer
above the keystore carries a handle and `docs/architecture/23-unified-server-model.md` §9 marks those positions
⟨secret⟩, but neither lists a field name — and a substitution keyed on `id` would ship a handle to Core the first time
a field is renamed or a new credential position appears. So `substitute` recurses the whole document and asks one
question of each string: does `keystore.Parse` accept it (`connector/internal/apply/apply.go:50`). Depth is bounded by
compile's own 32 (`:34`), because a document deeper than the emitter that wrote it is a cycle nobody should be hashing.
The refusal keeps the path it failed at (`:88`), which is why a missing secret says `outbounds[0].settings.vnext[0]…`
and never says what it could not read.

**Two documents, two hashes, and no decision taken in the middle of a writer.** ODEC-36 asks which byte string
`configHash` covers — the handle-bearing document or the materialized file — and `compile.HashInputNote` exists so the
emitter is not the thing that decided. `Write` therefore emits twice and reports both
(`connector/internal/apply/write.go:103` and `connector/internal/apply/write.go:104`);
`connector/internal/apply/write_test.go:71` asserts the two differ, because two equal hashes
would mean one of them is not what it claims. The file on disk is re-read and re-hashed separately (`AppliedHash`,
`:114`) so doc 25 §11's drift check compares a writer's claim against bytes that survived the rename. And "nothing was
applied yet" is the empty string rather than a sentinel, because
`packages/schema/protocol/commands/config.apply.json#/result/properties/previousConfigHash` already admits `^([0-9a-f]{64})?$`
— a pattern the contract accepts beats a value invented to mean absence.

**The rollback is now a test, and it is still only half the row.** `VER-29`'s sentence is "a failed `config.apply`
leaves the previous config applied and reports `previousConfigHash`". The measurable half is
`connector/internal/apply/write_test.go:85`: a document naming a secret the store does not have fails after the
handle-bearing emit and before a byte is written, the previous file comes back byte-identical, the directory holds one
entry — no temp left behind — and the hash the caller would report has not moved. The half nothing in this repository
can measure is the Core that was already signalled and the extension that believed the old hash. `VER-29` therefore
moved `no-code` → `no-run`, the same move §4.17 made for `VER-23`, and for the same reason: the live row it owes is a
session.

**Owner-only, in the one sense this host can show.** The sequence is `keystore.Store.Put`'s — create in the target
directory, write, `Sync` (`connector/internal/apply/write.go:85`), close, `Chmod 0600` (`:93`), rename (`:96`) — so the
module has one temp-and-rename discipline to audit rather than two, and the deferred cleanup at `:75` is the reason the
failure path is expressible at all. The mode assertions skip on Windows (`connector/internal/apply/write_test.go:44`),
where Go reports the inherited profile descriptor instead of the bits that were asked for; that is ADR-0011 rule 3's
residual stated in the test rather than smoothed over, and the ubuntu cell of the connector job is the one that will
assert it. What the `Sync` does
not buy is also now written down: there is no directory-level flush on Windows, so a power loss between the rename and
the metadata flush can lose the rename — and doc 32 §13's answer is that the file is re-derivable, so the recovery is a
re-apply. doc 27 §8 carries that sentence.

**What it refuses to invent.** An object-shaped handle is refused by name (`connector/internal/apply/apply.go:93`),
because ODEC-36 blocks shape C and a writer that guessed would make the decision a bug report later; the compiler's own
refusal at `connector/internal/compile/outbound.go:229` stays, since `compile` still takes no store. And the corpus
names four properties of this file — owner, lifetime, portability, verifiability — and never a path, so `Write` takes
the directory from its caller and refuses an empty one (`connector/internal/apply/write.go:43`): naming
`%LOCALAPPDATA%\NEXORA\config` would be this slice inventing an ADR-shaped fact. That choice belongs to the slice that
wires `config.apply`.

**The prose this slice had to correct, and the anchor that made it move together.** doc 27 §8's closing parenthetical
read "(no writer exists)" and was `VER-29`'s `at[]` anchor, so the sentence and the row could not be edited apart: the
harness requires an anchor to land on exactly one line and that line to still carry a label
(`packages/schema/scripts/verify.mjs` "every anchor in the queue lands on one line of one document, and that line still carries the label"). Rewriting one without the other is a red run, which is the cheapest form
of the law "a claim and its check are one change". doc 32 §1's reading rule and §13's closing paragraph, doc 34's
row that `VER-23` cites, `VER-23`'s own subject — which had named "the config writer" as unwritten one slice after
§4.17 — and doc 39's distribution all moved with it.

**Queue arithmetic, stated so the next slice can check it.** 78 rows unchanged; `no-code` 55 → 54 and `no-run` 3 → 4;
owner counts unchanged, because `VER-29` was already the Connector's; `closesBy` kinds unchanged, so the 39 live-row
rows and the 205-claim partition did not move — no label was added or removed anywhere, only three labels had their
reasons rewritten. Two subtractions from the `no-code` column, in two slices: the queue's own size is starting to
respond to code, which is what it was written to measure.

**Gates.** `gofmt -l connector` prints nothing; `go -C connector vet ./...` is clean here and under `GOOS=linux`;
`go -C connector test -count=1 ./...` prints ten `ok` lines, the tenth being this package.
`npm --prefix packages/schema run verify` → 1217 checks, unchanged: this slice moves one row's state and rewrites three
labels' reasons, and adds no assertion to the harness. `node tools/check-no-secrets.mjs` → 468 files, no finding — four
more than §4.17's 464, the same `git ls-files` behaviour counting a package that did not exist, and its one uuid-shaped
literal annotated rather than renamed (`connector/internal/apply/apply_test.go:17`).
`node tools/check-doc-evidence.mjs` → 2331 path:line citations, 518 pointers and 1394 doc-name references.
What is next is unchanged in kind: `config.apply`'s command surface
calling `Compile` and this package, then the supervisor, then stage 7's probe — the last still needing a Core binary
this host does not have.

### 4.19 M3's fifth slice: the Core process, the scope it is asked to live in, and the ladder in doc 30's order (`connector/internal/supervisor/`, 2026-11-06)

**What landed.** Eight files, 1 777 lines, nineteen test functions. The type is `Supervisor` over three seams —
`Launch` (`connector/internal/supervisor/supervisor.go:143`), `Verify` (`:148`) and the policy's `Death` (`:52`) —
with `New` (`:218`), `Start` (`:285`), `Stop` (`:514`), `Restart` (`:606`) and `Status` (`:263`); the applied-state
record is `State` (`connector/internal/supervisor/state.go:20`) with `ReadState` (`:31`) and `WriteState` (`:56`);
and one `scope` type is answered by three files, one per platform family. doc 22 §4 gave this package its address
(`connector/internal/supervisor/**`) before it existed, which is the only reason the row's four invariants read as a
specification this slice had to satisfy rather than a description it wrote afterwards.

**One of the nineteen tests is not a test.** `TestFakeCore` (`connector/internal/supervisor/supervisor_test.go:34`)
is the Core: every supervisor test re-executes the test binary with a mode in its environment, because `docs/adr/0019`
keeps a real Xray out of this repository and ADR-0011 keeps this host from being the judge of what a real one does. The
fake's whole behaviour is four lines of `select {}` or an `os.Exit`, and what it proves is the part that needs no binary:
that the config *path* reached the child (the child writes back what it was handed), that a second `core.start` with the
same hash returns the same pid, that a death the supervisor did not ask for produces a restart, and that closing the
containment handle ends a process. Nothing in this slice says a word about whether Xray loads the file.

**`Launch` exists because argv is not this package's fact.** The corpus names the config file, its hash, and the moment
it is handed over — and never the command line that delivers it. So the supervisor takes a function that builds the
child and refuses to invent `xray run -c` (`connector/internal/supervisor/supervisor.go:327`), in the same way §4.18's
writer takes a directory rather than naming `%LOCALAPPDATA%`: an invention in a seam's place is a fact about a binary
this host has never run. `Verify` is the same discipline pointed the other way — doc 33 §6's table row says the receipt
comparison runs "every `core.start` / `core.restart`, before exec", so it is a function this package calls before every
spawn, a ladder retry included, and `TestVerifyRunsBeforeTheSpawnAndADenialLeavesNothingRunning` fails if a refused
binary ever produces a process.

**Containment is asked for and then reported, because rule 5 says the result, not the mechanism.** ADR-0005's Windows
half is a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, and the first run of these tests was red eight times over
it — which is how the two facts now written into `connector/internal/supervisor/contain_windows.go` got measured instead
of assumed. (1) `SetInformationJobObject` refuses class 1 here at every structure length tried — 20, 24, 40, 44, 52, 56,
60, 64, 68, 72, 80, 96, 120, 144, 160 all answer `ERROR_BAD_LENGTH` (24) and 48 answers `ERROR_INVALID_PARAMETER` (87) —
while `QueryInformationJobObject` accepts 48 for the same class, and class 9
(`jobObjectExtendedLimits`, `connector/internal/supervisor/contain_windows.go:36`) accepts 144 and reads the limit back
at offset 16. The seam therefore uses class 9, and `TestJobObjectLayoutMatchesWin32` asserts the layout against the
architecture it is running on rather than against the 64-bit one it was written on, so the 32-bit build is checked and
not excused. (2) `AssignProcessToJobObject` answers `ERROR_ACCESS_DENIED` for a handle opened with anything short of
`PROCESS_SET_QUOTA | PROCESS_TERMINATE`, and the reason was mine rather than Windows': the constant had been transposed
to `0x0010`, which is `PROCESS_VM_READ`; `PROCESS_TERMINATE` is `0x0001`
(`connector/internal/supervisor/contain_windows.go:43`). `PROCESS_ALL_ACCESS` "worked" only because it contains that bit.
The proof is end to end, not API by API: `TestParentDeathEndsTheContainedChild`
(`connector/internal/supervisor/supervisor_test.go:571`) has a holder process put a child in a job, watches it alive,
exits, and then polls `processAlive` until the child is gone — the child is orphaned by nothing, because the only
process it could outlive is the one that held the handle. `IsProcessInJob` on this host answers zero for the Connector
and its children, so the inheritance branch `attach` also implements (`:103`) — the case where the host's own job
already claims the child and rule 6's chain holds by inheritance — is code with a reason and no witness here.

**What the standard library will not give, stated in the contract rather than in a comment.** doc 22 §4's invariant 1
asks for containment "from the instant of spawn — never adopted afterwards". Go's `syscall.SysProcAttr` has no job-list
attribute and `os/exec` exposes only the pid, so a dependency-free Connector can only reach `CreateProcessW` with
`STARTUPINFOEX`/`PROC_THREAD_ATTRIBUTE_JOB_LIST` by leaving `os/exec`, and `exec_windows.go` never adds an attribute
besides the handle list. The assignment therefore happens after the pid exists — a window — and doc 22 §4 now says so,
names the closing of the window as ADR-0011's platform work, and does not pretend the invariant moved. What the code
does about it is the part rule 5 can require today: `spawnLocked` creates the scope before the spawn
(`connector/internal/supervisor/supervisor.go:343`), attaches after, and reports the answer — `contained` beside a note
in the platform's own words — and a scope that cannot be made is *not* a refused start, because `core.start`'s own gloss
says `false` "is a reported condition, not a silent one". `TestStartReportsContainmentAsThePlatformAnswersIt`
(`connector/internal/supervisor/supervisor_test.go:552`) fails if the flag and the note ever disagree, on any platform,
including the two that answer false.

**The ladder runs in doc 30 §4's order, and the order was the finding.** Read verbatim, that section does not describe
"restart, and if it fails, block": the recorded scene is exit, one status read, `core.blocked` ninety milliseconds later —
before any restart was attempted (`packages/schema/fixtures/protocol/failure-block.jsonl`, `seq` 1 to 4) — and only then
the attempts. The first implementation had it backwards, and `TestUnexpectedExitIsRestartedByTheLadderAndRecoveryIsPublished`
caught that by asserting the verdict arrives before the recovery does. `ladderRun`
(`connector/internal/supervisor/supervisor.go:448`) now sets the blocked flag inside the same critical section that
registers the episode and publishes it (`:461`) before waiting a millisecond, then makes up to `restartAttempts` spawns
at the policy's own backoffs, and publishes the recovery (`:480`) *only when `notify` is on* — while exhaustion publishes
nothing further, because the block has been in force since the death. Worst case is the section's arithmetic, 3 × 15 s +
(1 + 5 + 30) s = 81 s, and no probe and no config rewrite happens inside it.

**A gap the code found, registered rather than papered over.** Counting attempts per death is what the section says, and
it is what `watch` does (`connector/internal/supervisor/supervisor.go:381`): every observed unexpected exit starts its
own ladder with its own budget. A Core that dies again straight after a successful restart is therefore handed the budget
again, forever, with the block lifted and re-imposed each cycle and `server.test` answering normally in between — and no
file in this corpus names a window that would make the second death a new episode, or a count that would make the block
permanent. `failure-policy.schema.json#/properties/coreDeath` holds one integer and no denominator, so the three readings
are not distinguishable from the shipped data. That is `ODEC-38` (`contract`, three options) and `VER-79` (`no-code`,
closing by that decision), anchored on the row doc 30 §13 now carries; the exhaustion test does not depend on the answer,
because it makes the *attempts themselves* fail through the `Verify` seam rather than relying on a death-after-recovery.

**The record, and the field that had to stop being two.** `data-ownership.json`'s `applied-state` row names four
properties and doc 34 §7 repeats them, so `State` has exactly those four and no fifth — including no containment answer,
which is a fact about a process that is gone by the time the file is read. One bug was hiding there: `Status.LastExit`
was served from a supervisor field that `WriteState` never populated, so a reloaded record could not answer the question
LIVE-13's restart half asks. `statusLocked` now reads `s.st.LastExit` (`connector/internal/supervisor/supervisor.go:263`)
and `spawnLocked` carries it forward (`:360`), which is what makes `TestAppliedStateOutlivesTheSupervisorThatWroteIt` a
demonstration of the row rather than of a process's memory. `WriteState` reuses §4.18's sequence — temp, write, `Sync`,
close, `Chmod 0600`, rename, cleanup on failure — so this module has one atomic-replacement discipline to audit, and the
mode is owner-only for a different reason than the config's: the row's own `holdsValue` answer is "no", so what 0600
protects here is a server list and a pid. A directory with no record is the first apply and not an error
(`connector/internal/supervisor/state.go:31`), which is why `previousConfigHash` stays an empty string rather than a
sentinel; a record that will not parse is an error to `ReadState` and a fresh start to `New`, and `TestCorruptStateIsReadAsFreshByNew`
keeps those two readings from being confused by a later caller.

**What it refuses to invent, item by item.** An `Exit` has a code and a signal and no message
(`connector/internal/supervisor/supervisor.go:93`): doc 30 §3 says the code is reported and interpreted nowhere, only two
of doc 22 §4's four classes have data behind them, and no file states Xray's numbers — so "config rejected" and "port
conflict" are absent rather than guessed. The child's stderr is drained and dropped, because the file it was handed is
plaintext credential material and a tail of it in a state file would be a secret outside the keystore and outside doc 32's
masking. `StopResult.TerminatedBy` admits `request` and `already-stopped`
(`connector/internal/supervisor/supervisor.go:122`) and not the schema's other two: `parent-death` is unreachable from
inside the thing that died, and `crash` would be a stop request claiming a death it did not cause. `askGraceful` is false
on Windows with no channel named — nothing in `packages/schema` or `docs/` states a polite stop Xray would take, and an
invented one is the same invention argv would be. `New` refuses an empty `Dir`, a nil `Launch`, a nil `Verify`, attempts
outside the schema's 0–10, and a backoff list that cannot answer the attempts it is paired with — four refusals that each
replace an invention with an error.

**The prose this slice had to move, and the guard that found the part I had not.** doc 30 §4 gained the sentence naming
what it does not say, doc 30 §13 gained the labelled row `VER-79` anchors, doc 21 §12's containment cell stopped saying
"nothing", ADR-0005's Verification section gained the half that now has a producer, doc 22 §4's invariant 1 gained the
platform's cost, and LIVE-13's mechanism cell says what the supervisor test is not. Inserting those lines moved doc 30's
numbering, and `tools/check-doc-evidence.mjs` answered with three `BLANK TARGET` findings — doc 32 had cited doc 30 at
`:135` and `:369-370` by line number, and line numbers are the one form of citation this corpus cannot make in prose
alone. All four citations moved to `:141` and `:377-378`. That is the third slice running into this: §4.18's anchor could
not be edited apart from its row, and here a *document's own paragraph* could not be edited apart from another document's
citations. The citation set moves with the prose or the run is red.

**Queue arithmetic, stated so the next slice can check it.** 78 rows → 79: `VER-14` moves `no-code` → `no-run` (the
scope is code, assigned, reported and proven against a child this package spawned and killed — the third connector row
§4.17 and §4.18 started moving that way, and for the same reason: the live row it owes is a session), and `VER-79` enters
`no-code` over the budget the corpus does not have a field for. `no-code` is therefore 54 → 53 → 54, `no-run` 4 → 5,
`connector` 37 → 38, and `decision` 2 → 3 — doc 39 §8 now reads "three decisions gate six rows". `marker.counted` 205 →
206 for the one new label in doc 30 §13 (the partition is 156 + 26 + 24), and the register 37 → 38 entries over 58 → 59
sites, `contract` 13 → 14, with doc 40's newest-entry paragraph passed from `ODEC-36` to `ODEC-37` to `ODEC-38` as it
moves. `closesBy` kinds other than `decision` are unchanged, so the 39 live-row count and the executable matrix did not
move.

**Gates.** `gofmt -l connector` prints nothing; `go -C connector vet ./...` is silent for windows, linux, darwin and
freebsd and for `GOOS=windows GOARCH=386`; `go -C connector test -count=1 ./...` prints eleven `ok` lines, the eleventh
being this package at 3.2 s — the first package whose tests are slower than the rest of the module put together, because
they spawn real processes and wait for them to die. `-race` is not among them and will not be claimed: the toolchain here
needs cgo for it and gcc is absent. `npm --prefix packages/schema run verify` → 1217 checks, unchanged — this slice adds
no assertion, it re-types three distributions and two counters, and the run is green on the first attempt after the
doc 39 and doc 40 numbers moved. `node tools/check-no-secrets.mjs` → 476 files, no finding: eight more than §4.18's 468,
the same `git ls-files` behaviour counting a package that did not exist when the guard last ran, and no annotation added,
because nothing in this package mints an identifier-shaped literal. `node tools/check-doc-evidence.mjs` → 2347 path:line
citations, 520 pointers and 1402 doc-name references: +16 / +2 / +8 against §4.18's **2331 / 518 / 1394**, every one of
them in this file's own section except `docs/README.md`'s two names, the register document's one new pointer and name, and
the labelled line doc 30 §13 carries — and doc 32, the file whose four citations had to move, contributes `+0 / +0 / +0`,
because a moved address is not a new one. That file is the finding, and §4.4's fourteenth paragraph records it: three
addresses in it had gone wrong, not stale, and only the guard's floor under a citation caught that. That is the third
slice in a row whose real signal is in the files the guard says nothing about.

What is next is unchanged in kind and shorter by one item: `config.apply`'s command surface — which is also what would
make `compile`, `apply` and this package one path rather than three tested neighbours — then `connector/cmd/nexora-host`,
which is the row `ODEC-35` still stops and the process `VER-14`'s chain needs above Core, then stage 7's probe, whose
binary this host still does not have.

### 4.20 M3's sixth slice: the command surface that joins compile, apply and supervisor (`connector/cmd/nexora-connector/`, 2026-11-06)

**What landed.** Two files, 1 761 lines, sixteen test functions, and no fourth package. `config_apply.go` (523 lines) is
`Handle` (`connector/cmd/nexora-connector/config_apply.go:288`) over the three things §4.15 to §4.19 built: the seams
`intentSource` (`:58`) and `coreProcess` (`:66`), the wiring `applyConfig` (`:101`), the hand-decoded args
(`readApplyArgs`:172), the answer struct (`applyAnswer`:271), the reconciliation (`reconcile`:360) and the classification
table (`codeMarkers`:478, `codeForApply`:499). `config_apply_test.go` (1 238 lines) is the slice's claim. `go test
-count=1 ./...` still prints eleven `ok` lines — this is the eleventh package's second file, not a twelfth package — and
the `cmd` line moved from hundredths of a second to 0.265 s, because two of the sixteen tests spawn real children.

**The order is the contract, and the refusal tests are the proof of the order.** Args, then the record, then
compare-and-swap, then the intent, then the compile, then the file, then the reservation table, then the process —
"compare-and-swap first … a refusal at any of those points has touched nothing after it" is doc 27 §8's atomicity rule
restated as a sequence rather than as a rename, and `Handle` keeps the shape of that sentence.
`TestCompareAndSwapMismatchIsBusy` (`connector/cmd/nexora-connector/config_apply_test.go:671`) is the one that makes it
observable: after the Busy it asserts the intent source was never asked (`asks != 0 || records != 0`), Core was never
touched (`starts != 0 || stops != 0`) and no config file exists — three negative assertions against the packages behind
the surface, not against a mock's call log. The same shape recurs in `TestDryRunTouchesNothing` (`:624`) and in
`TestRestartPolicyNeverLeavesTheProcessAlone` (`:695`), whose last assertion is the subtle one: with
`restartPolicy: never` the file *is* applied, and the applied-state record stays empty (`:715`) because that record is
the supervisor's to write and no process was ever spawned. A surface that wrote the hash itself would be a surface that
could make the next apply believe a config is in force that nothing loads.

**`expectedAppliedHash` is `[string, null]`, and that type is the sentence the CAS needed.** The arg's own schema
(`packages/schema/protocol/commands/config.apply.json#/args/properties/expectedAppliedHash`) is a pattern
`^([0-9a-f]{64})?$` over a type that admits absence, so an empty value is not "I believe nothing is applied" — it is no
belief at all. `Handle` therefore compares only when the caller stated something
(`connector/cmd/nexora-connector/config_apply.go:302`), and the reason is not tidiness: were an unstated belief read as
a claim about an empty record, every second apply from the same tab would have to call `core.status` first just to be
allowed to speak, and doc 26 §2.1's CAS would have become a lock. `docs/architecture/26-command-surface.md` §2.1 now
says that out loud beside the rule it qualifies, because the code and the sentence had to move together or the next
reader would "fix" the `!= ""` guard into a bug.

**ODEC-36 is a knob, and the knob is required.** `hashSpan` (`:74`) has two live values and a zero value that
`newConfigApply` refuses (`:126`), so a build that has not read the register cannot answer `config.apply` at all —
`TestNewConfigApplyRefusesUndecidedWiring` (`connector/cmd/nexora-connector/config_apply_test.go:1003`) walks all seven
wiring cases including the four nil seams, and each refusal's message names the decision or the directory it is missing.
The two spans are then measured against each other rather than argued: the same intent gives two different
lowercase-sha256 strings (`TestHashesDifferBetweenTheTwoSpans`:600), because `internal/apply` reports both digests
(`connector/internal/apply/write.go:103`) and `span` (`connector/cmd/nexora-connector/config_apply.go:404`) drops
exactly one — "which is exactly what ODEC-36 says is undecided" is what that function's comment claims, and the test is
what makes it a claim about code. The slice also paid ODEC-36's consequence forward into the register's `why`: under
`hashValues` a *preview* has to materialize to know what the file would hash to (`previewHash`:387), so a dryRun can
refuse for keystore reasons. That asymmetry is now pinned both ways — `TestValuesSpanPreviewReadsTheStore` (`:653`) and
`TestDryRunTouchesNothing` (`:624`) — because a decision argued only in prose is a decision that gets reversed by a
tidy-up.

**The classification table is the slice's real output, and fourteen cases drive it.** `codeForApply`'s order is the
argument: `errors.As` on `*keystore.Error` first (`:500`), because a keystore refusal already carries its registered
code; then `errors.Is(err, supervisor.ErrIntegrity)` (`:505`), because doc 22 §4's "an unbootable Core is surfaced as
`IntegrityMismatch`" is a security answer rather than a config one; then the six markers (`:478`), then the `compile: `
prefix as `InvalidProfile` (`:517`), then `ConfigApplyFailed` (`:522`). `TestRefusalCodesDrivesTheRealRefusals`
(`connector/cmd/nexora-connector/config_apply_test.go:771`) makes each of the three packages refuse *for real* — a stage
of the pipeline, the keystore, the writer, the reservation store, the process owner — and asserts the code plus the
cause that survived into the message, so a reworded refusal fails a row there instead of quietly changing an answer a
client acts on. Two rows of that table are worth the sentence: doc 32 §11's missing secret answers `InvalidProfile` (the
user's problem, and retyping helps) while a store the platform will not open answers `KeystoreUnavailable` (retyping
does not), and the grammar-rejected reference answers `SchemaMismatch` from the keystore's own table, which is a code
this command declares and doc 32 §11 does not name. `DiskFull` is declared by `config.apply` and unreachable from here
(`:519-522`) — stated, because an unclaimed code in a schema is either a gap or a lie and a reader cannot tell which
without the sentence.

**`ErrIntegrity` had to be added to the supervisor, and it is a sentinel for a reason.** doc 33 §6's pre-exec proof is
the verifier's own error, and reading `IntegrityMismatch` out of its *words* would be a string match on somebody else's
message — the same failure §4.11 records a green check hiding. So the supervisor wraps it beside the cause
(`connector/internal/supervisor/supervisor.go:343`, `%w: %w` around `:52`) and the surface matches the sentinel, which
keeps the verifier's reason available in the message and the taxonomy code out of anybody's prose.

**`clip` keeps the tail, which is a decision with a consequence attached.** `maxApplyMessage` (`:438`) is the envelope
schema's own `maxLength` for an error message, and a refusal that quoted a whole pipeline error would be a frame the codec
refuses — the worse of the two answers. The cut walks forward to a rune start (`:463`) because a message that is not
valid UTF-8 is that same refused frame; and it keeps the end because these errors are
`surface: context: cause: cause`, where the end is the one part a caller cannot reconstruct — which document, field or
stage refused — while the head is the surface naming itself, which the code already says.

**One of the sixteen tests is not a test, and writing it found a bug in the slice before it.** The fake Core is this
test binary re-executed (`TestFakeCoreAgent`:56), the device §4.19 uses for the same two ADRs. Its first body was the
honest expression of forever — `select {}` — and every test that needed a live child died. The cause is Go's runtime,
not this repository: in a re-execed test binary whose only goroutine parks with nothing the runtime owes it, the
deadlock detector fires, and the child exits 2 with `fatal error: all goroutines are asleep - deadlock!`. Measured
against the *committed* tree, in a clean worktree at `7544baa`: that tree's own compiled test binary run as its `wait`
mode child exits 2 and prints nothing else, and a 4-second-budget probe of both bodies gives `select {}` → exit 2 with
the fatal error and `for { time.Sleep(time.Hour) }` → still alive at four seconds. The fix belongs to both files
(`connector/cmd/nexora-connector/config_apply_test.go:75`, and `stayUp` in
`connector/internal/supervisor/supervisor_test.go:75`) — but the finding is the other half: `go test
./connector/internal/supervisor` was green, and stays green, with the dying child, because nothing in that package
waited for a child to *stay* alive. `Stop`'s `WasRunning` reads the supervisor's own bookkeeping, and
`TestParentDeathEndsTheContainedChild`'s ten-second poll was waiting for a death the child had already caused itself. A
command surface that stops a running Core cannot be tested that way, which is the ordinary fact that §4.19's device
becomes this slice's bug: a test of *ordering* is the first test of *still being alive*.

**Three assertions had to move before the reconciliation test meant anything.** (1) The launch closure picks a record
slot by spawn count and clamps it (`connector/cmd/nexora-connector/config_apply_test.go:496-507`), because the ladder is
entitled to spawn a third child and a closure that indexed by call count would have handed the third one the second
record — a proof of a handover that never happened. (2) The test waits for the *first* child's record before issuing the
second apply (`:530`, 20 s): the second apply ends that process, so a child that had not finished booting would leave no
record at all and the assertion would become a race instead of a proof. (3) The new server had to join the profile's own
list (`intent.Profiles[0].ServerIDs`, `:538`) as well as the registry, because membership is what the compiler reads —
an intent that only gained a registry entry compiles the identical config, and `:547`'s `t.Fatal` on an unmoved hash
exists to say that this test then proves nothing. Beside them, `secondServer` (`:570`) sets `s.Tag = ""` because doc 23
§6 derives the tag; a test that pinned a tag would be pinning a value the compiler computes.

**The promise ADR-0013 made at the layer it was made for.** `split.go` producing an `unrepresentable[]` entry is half of
"report, do not drop"; the other half is that the entry leaves the Connector in the frame the extension reads.
`TestAReportedRuleTravelsInTheAnswer` (`:1079`) asserts both in one answer: `applied: true`, two entries whose sources
are the shipped policy's built-in-tag rule and the control enabled on top of it, and a config on disk that contains no
`sniffing` block. The test was written expecting one entry and found two — the shipped `routing.minimal.json` already
reports a rule — which is the stronger version of the assertion and the reason it is worth stating: the answer's list is
the *compile's* list, not a test's fixture.

**The prose this slice had to move.** doc 27 §8's tail stopped describing an unwired writer and says now that
`config_apply.go` calls it from inside one command frame, with the honest remainder (an apply that failed while a *real*
Core held the previous config); doc 27 §9 gained its fourteenth row, and that row is `VER-80`'s anchor and `ODEC-39`'s
site at once; doc 26 §2.1 gained the `[string, null]` sentence; doc 39 moved `VER-32` between its tables, added `VER-80`,
and restated every distribution in it; doc 40 gained `ODEC-39`, its counts, and the newest-entry paragraph that passed
from `ODEC-38` — and this file's own two citations into `supervisor_test.go` moved (`:537`→`:571`, `:556`→`:552`),
because inserting `stayUp` shifted that file by fifteen lines. §4.11's lesson a seventh time, and a new twist on it: the
stale addresses were not in a document citing a document, they were in the record of the slice that inserted the lines.

**Queue arithmetic, stated so the next slice can check it.** 79 rows → 80: `VER-32` moves `no-code` → `no-run` (the
envelope→Xray compiler now has an answer for ADR-0013's promise — `Unrepresentable`
(`connector/cmd/nexora-connector/config_apply.go:421`) carries the compile's report into the result, proven at
`connector/cmd/nexora-connector/config_apply_test.go:1079` — and what no test here can show is a real Core holding the
previous config), and `VER-80` enters `no-code` over the surface `main.go` does not register (it wires `connector.hello`
and `connector.version` only, `connector/cmd/nexora-connector/main.go:79-80`, and `intentSource` has no production
implementation). `no-code` is therefore 54 → 53 → 54 and `no-run` 5 → 6, `connector` 37+1 → 39 and `decision` 3 → 4.
`marker.counted` 206 → 207 for doc 27 §9's one new label (the partition is 157 + 26 + 24), and the register 38 → 39
entries over 59 → 60 sites with `contract` 14 → 15. `verify.mjs`'s literals moved to those numbers; the check count did
not, because a slice that adds a row and a site adds no assertion.

**Gates.** `gofmt -l connector` prints nothing; `go -C connector vet ./...` is silent for windows and linux; `go -C
connector test -count=1 ./...` prints eleven `ok` lines, the supervisor still at 3.2 s and still slower than the other
ten put together — 0.75 s and 0.91 s in the two runs this slice measured, the difference being the command package's own
child processes rather than any assertion — which is what §4.19's claim now has to survive a second process-spawning
package. `npm --prefix packages/schema run verify` is green on the first attempt after the doc 39 and doc 40 numbers
moved. `node tools/check-no-secrets.mjs` counts the two new files once they are staged — 478 files after §4.19's 476 —
and found four `uuid-literal` lines in `config_apply_test.go` on the first run: the synthetic credential value, the
profile id, the server id and the second server's id. Each carries a `nexora-secret-allow` line now, the corpus's
thirteenth through sixteenth annotation, because a command-surface test cannot put its fixtures under
`connector/testdata/` and stay a test of the compiler's real examples; the reference URI at
`connector/cmd/nexora-connector/config_apply_test.go:49` needed nothing, the guard not reading a `keystore://` handle as
an identifier. `node tools/check-doc-evidence.mjs` → the figures in §4.4's sixteenth paragraph.

What is next is §6's queue, and the first item on it is unchanged: `connector/cmd/nexora-host`, the row `ODEC-35` stops —
the process that reads the pipe, owns the dispatcher and would hand a frame to the surface this slice wrote, which is now
the second-to-last thing standing between `config.apply` and a running Core. Then stage 7's probe, whose binary this host
still does not have, and `internal/chunk`, which `ODEC-34` still stops.

---

### 4.21 The three cheap items §6 queued, and the harness that now reads its own documents aloud (2026-11-06)

**What landed.** §6's M2 tail queues three items beside the milestone and says of all three that they are "a string and a
check in a file the next real slice already opens". §4.20 opened that file, so this is the three of them, in the order
they were written: the front door's gate message (`connector/internal/dispatch/dispatch.go:169`), an assertion joining
doc 31 §8 to `packages/schema/protocol/framing.json#/senderPolicy/rateLimitFacts/countedWhere` (§4.12's repair, which
left the data, the document and a comment agree and left nothing to keep them agreeing), and an assertion that opens
`docs/architecture/39-verification-matrix.md` and reads its tallies out of the document instead of comparing the data to
a constant (§4.14's finding, which was that three of those tallies had rotted while every run stayed green). The harness
goes 1217 → **1220 checks**: two new assertions for the counting process and one for the tallies. No `VER` row moves, and
none is invented — these three were §6's lines, not doc 39's queue, and a register entry for work the tree already
finished is the defect doc 40 §3 calls a question the register invented.

**One boolean, two histories, one sentence.** `Identity.Negotiated` is false because no hello has ever crossed the stream
and because a hello was answered and a later `VersionMismatch` withdrew it; §4.13 ran the second case on purpose through
the corpus's own `handshake.jsonl` and found the refusal claiming the frame "arrived before connector.hello". The state
was right and the wording was a history, so the message now names the state — `TestTheGateMessageNamesTheStateRatherThanOneHistory`
(`connector/internal/dispatch/dispatch_test.go:148`) drives both halves, requires the two messages to be byte-identical,
requires the words "in force" and "withdrawn" to be present and the word "before" to be absent, and then walks every
published gated method with a 32-character sender id to hold each message under the envelope's
`#/properties/error/properties/message/maxLength` of 200, since nothing on that path clips.

**The counting process, cross-read rather than cross-constants.** §4.12's lesson was that a field and a document can
agree with each other and disagree with the code, so the new check extracts a process name from each side's own text —
the leading clause of `countedWhere`, and the sentence in §8 that says `counted by the X **before dispatch**` — and fails
if they differ or if either becomes unreadable. Its second half refuses a §8 that hands the counting to a *different*
process in a different sentence, which is how the original contradiction was phrased. Proven load-bearing, not assumed:
writing `host` into §8 turns the run red with `framing.json says "Connector", doc 31 §8 says "host"`, and reverting it
turns it green again.

**Doc 39's numbers, read from doc 39.** The new tally check parses §4's five-row state table, §5's owner list and
closing-kind list, and the prose sentences that restate the same counts — nine of them, matched by eight patterns: the
`N of M rows are \`no-code\`` sentence, the `N of M rows close by executing an existing live row` one, the two `N rows
promise a named assertion` occurrences, the `N \`no-check\` rows` and `The N \`no-code\` rows` sentences, the
`N rows say the component does not exist` bullet, the `N rows are \`needs-human\`` bullet and the
`the N deferrals the corpus says exist` question — and compares each to the distribution the data has. One of doc 39's
own tallies had to change to be readable at all: §1 said "Five rows are `needs-human`", and a check cannot read an
English numeral, so it says 5. The sentence's point was that those rows stay unnumbered as *questions*, which is still
what it says. Proven the same way: setting §4's table to 53 fails the run with `§4's table says 53 no-code rows, the
data says 54`.

**Gates.** `npm --prefix packages/schema run verify` → 1220 checks, green, and green again after both negative tests were
reverted. `go -C connector test -count=1 ./...` → the same eleven `ok` lines §4.20 printed, one of them now carrying a
test it did not have. `gofmt -l connector` prints nothing and `go -C connector vet ./...` is silent.
`node tools/check-no-secrets.mjs` → 478 files, unchanged and unsurprising: the guard enumerates `git ls-files`, and this
slice adds no file. `node tools/check-doc-evidence.mjs` caught, on its first run, the one citation this slice broke by
growing the harness — §4.18's address into the anchor rule, 3518 → 3540 — and after that re-point prints the figures in
§4.4's seventeenth paragraph.

What is next is §6's queue with its first paragraph closed: the three cheap items are done, and what remains at the front
is `connector/cmd/nexora-host`, which `ODEC-35` stops — not a cheap item, and not work an agent may finish by itself.

---

### 4.22 The 52 line numbers into the harness, replaced by the assertions they were naming (2026-11-06)

**What landed.** §6 #8(b) has carried this job since the convention it repairs was written, and it asked for the work in
one commit rather than in passing so that `packages/schema/scripts/verify.mjs` could join
`MUTABLE_CITATION_TARGETS` in the same breath. The item said `49`; measured against a clean worktree at the commit
before, the corpus carried **52** visible `path:line` addresses into that file across **14 documents**, and three more in
a shape the checker is blind to — the bare `` `:437` `` and `` `:452` `` in
`docs/adr/0008-idempotency-and-replay.md`, which are not `path:line` spans because they name no path, and the
parenthetical `` (`:242-263`) `` in doc 21's frame-limits row, which sits outside the file token it refers to. All 55
are now citations of the assertion: the file token, then one or more double-quoted labels exactly as the harness carries
them, template placeholders included. No `VER` or `ODEC` row moves, for the reason §4.21 gives for its three: this is a
claim about how the corpus points, not about the system, and the register's own rule calls a row for either of the other
two kinds.

**Converted by meaning, not by number.** The rule for each of the 55 was to read the sentence, find the block of the
harness it describes, and quote the label that block asserts — never to follow the address, which is the quantity in
question. That choice is what found the two that had already rotted: ADR-0008's `:437` lands on the line that works out
whether any answer in a replay group succeeded, and `:452` on the closing brace of the fixture-event-name check, while
the probe-answered-once check sits forty-one lines below the first and the replay-pairs check forty-one below the
second. Both sentences were accurate; both numbers were not; and because the spans were bare, a green run had been
agreeing with them for months. Had the conversion been mechanical, the corpus would have kept two citations that resolve
and mean nothing — which is the whole defect class, in the two places it had already happened.

**The half of the rule that had no checker.** A label citation is stronger than a line number only if something reads
it, so `tools/check-doc-evidence.mjs` gained a fourth pass: `HARNESS_TOKEN_RE` finds the token, `LABEL_RUN_RE` walks the
quoted labels that follow it, and `staleLabel` refuses any quote that is not a substring of the harness. Writing it
produced two findings of its own. First a false positive: doc 36 §2's questions table has a cell whose sentence ends
*at* the token, and the first parser read the next cell's opening quote as a label — which is why the separator must be
real whitespace,
a comma or `and`/`—`, why a label may not contain `|`, and why the run stops at a blank line. Then the opposite failure,
the silent one: two of the converted cites wrapped a label across a line break — ADR-0004's four framing rules and doc
27's `domainStrategy` row, which also lost its table shape — and a parser that refuses newlines inside a label checks
fewer labels and reports green. A throwaway that re-parses the corpus with a grammar that *allows* the split, and
compares the two counts, is what proved the set closed: it prints each escaped quote instead of trusting the guard's
total, and today both counts are 68.

**What the shift cost while the conversion was in progress.** §4.20's third failure mode — the file a number points into
grew for a reason belonging to the next slice — arrived inside the slice that removes its cause, and in the one place it
still can: converting ADR-0010's own bullet put three lines where two had been, two addresses in doc 34 into that ADR
moved onto the neighbouring real lines, and the guard saw only the third of the three, because it landed blank. The
other two are not catchable by any citation checker, since `:111` and `:112` are both legitimate lines and only the
sentence knows which it means; they were found by diffing every cited target between HEAD and the worktree, and
re-pointed by reading. Line cites into a document a slice edits are still this fragile. What this slice buys is that the
55 into the file that moves *most* are no longer addresses at all.

**Gates.** `node tools/check-doc-evidence.mjs` → green at the three figures §4.4's eighteenth paragraph carries, plus
**68 harness assertion labels**; the bucket is load-bearing, proven by writing "extension-to-host frame limit is 2 MB"
into ADR-0004, which fails with `STALE LABEL (1)` naming the file and the quote, and is green again on revert.
`npm --prefix packages/schema run verify`
→ the same **1220 checks** as §4.21: this slice changes no contract and no assertion, so the harness that reads doc 39's
tallies reads the same doc 39. `go -C connector test -count=1 ./...` → the same eleven `ok` lines; `gofmt -l connector`
prints nothing and `go -C connector vet ./...` is silent. `node tools/check-no-secrets.mjs` → 478 files, unchanged —
it enumerates `git ls-files` and this slice adds no file. `docs/README.md`'s citation-kind bullet, doc 20's citation row,
doc 21's dispatch and frame-limits rows and this file's §4.1 inventory line now state the label grammar, so the
convention and the checker describe one thing.

---

### 4.23 The format an `auto` parse resolves to, and the contract field that has nowhere to put it (`docs/architecture/29-subscriptions-and-servers.md`, 2026-11-06)

**What landed.** No Go file, no contract change, and no decision. §6's M3 item names five components; four are packages
on disk and the fifth — subscriptions — is the one no slice had opened, which is what §4.14 and §4.20 did with the
other gaps: read the document against the data until a sentence turns out to have no artefact. This one is in
`docs/architecture/29-subscriptions-and-servers.md` §5, and it is two claims in one sentence. The first — `auto` is
legal only while exactly one parser's detector answers true — is a behaviour the Connector would implement, and doc 29 §2
already assigns it there. The second — that `auto` therefore never survives into a stored source, because the parser
records which detector won — is a promise about *where a value rests*, and nothing in this repository is that place. The
registration is the deliverable: §5's sentence now carries the label, §13 gains a seventh row that names the register as
where the answer belongs, `VER-81` enters `packages/schema/verification.json` (`no-code`, `contracts`, closes by
decision), `ODEC-40` enters
`packages/schema/open-decisions.json` (`contract`, three options, gates `VER-81`), `marker.counted` moves 207 → 208 and
`citation.counted` 60 → 61, doc 39 and doc 40 are re-typed to match, and four constants in
`packages/schema/scripts/verify.mjs` move: `54 → 55` and `80 → 81` in the state assertion, `contracts 6 → 7` in the
owner one, `decision 4 → 5` in the closing-mechanism one. That is the whole of the code this slice changes.

**What the data says, measured rather than remembered.** Four artefacts, and the gap is between them, not inside any
one. `packages/schema/protocol/commands/subscription.refresh.json#/result` bounds a `refreshed[]` item with
`additionalProperties: false` over exactly eight properties — `subscriptionId`, `sha256`, `serverCount`, `added`,
`removed`, `duplicates`, `lossyFields`, `servers` — and none of them is a format. The same file's `#/args` description
states the opposite duty in its own words: the Connector reads `format` and "writes none of them back — the result is
not a mutated source". `packages/schema/subscription-source.schema.json#/properties/format` carries `auto` in its enum
with `default: auto`, so the ambiguous value is the *default*. And the two planes are separated by design: doc 29 §2
gives the Connector the format detection and the parse, while `docs/architecture/34-data-split.md` §6's table puts every
intent store — the source document included — in the browser plane. So the value that would decide what a stored
`format` should say is computed one plane away from the document it would correct, and the only channel between the
planes is those eight fields and refuses a ninth.

**Why this is a row and not a fix.** Three readings survive the gap, and all three are a property plus the sentences
that read it: `refreshed[]` gains `format` and the args description gains the exception it currently denies; the
verdict travels beside the servers instead of beside the source, which makes it a fact about the feed rather than about
the row the user sees; or `auto` is kept, §5 loses its storage claim, and an ambiguous feed fails once per refresh
rather than once per source. Writing any of them into a schema in this slice would be the thing
`packages/schema/open-decisions.json#/scope` refuses — an answer invented by the report. What is owed is a choice, and
the queue now names which artefact has to change once it is made.

**What the harness caught while the registration was being written.** A registration is a set of pointers, and pointers
collide. The first draft of §13's seventh row described §5 with §5's own phrase, "records which detector won", and that
phrase is `VER-81`'s anchor — so the anchor matched two lines of one document. A throwaway census printed it before any
run did: 158 anchors landing on 159 lines, one anchor with two hits, no line claimed by two rows. The row was reworded,
and then the collision was deliberately restored to check that the guard, not the census, owns the rule. `npm --prefix
packages/schema run verify` fails three checks on it: `VER-81 -> anchor matches 2 lines in
docs/architecture/29-subscriptions-and-servers.md`, then `unowned: docs/architecture/29-subscriptions-and-servers.md:107`
because the dropped anchor leaves §5's label unanswered, then `rows=157 + vocabulary=26 + live=24 = 207, derived=208`.
One failure is the collision; the other two are what a collision costs the arithmetic, and they are the reason §6 #8's
"exact partition" sentence is worth its machinery. Earlier in the same slice the run had printed the six the
registration is *supposed* to fail before it is complete — derived 208 against a declared 207, the three distribution
assertions, `absent from doc 39: VER-81`, `unowned: …29-subscriptions-and-servers.md:390` and `unregistered=VER-81` — so
doc 39, doc 40 and the four harness constants were written against its messages rather than against a guess.

**Gates.** `npm --prefix packages/schema run verify` → **1220 checks**, green: the count is unchanged because this slice
adds no assertion, and the three checks that read doc 39 and doc 40 aloud now read the re-typed documents.
`node tools/check-doc-evidence.mjs` → green at the four figures §4.4's twentieth paragraph carries.
`node tools/check-no-secrets.mjs` → the same **478 files** — it enumerates `git ls-files`, and this slice stages no
file. `go -C connector test -count=1 ./...` → the same eleven `ok` lines, `gofmt -l connector` prints nothing and
`go -C connector vet ./...` is silent, because none of this slice is Go.

---

### 4.24 The bullet that asks the remote, asking it again (section 7, 2026-11-06)

**What landed.** One bullet and this section. §7's opening line now names `f4cac24` as the branch head instead of
`31008a5`, the count that query returns is 5 instead of 3, and the five commits the count covers are spelled out —
§4.20's surface, §4.21's tail, §4.22's conversion, the ledger line that recorded those three, and §4.23's
registration. No push has been confirmed for any of them, and §4.23 is the reason a bullet written yesterday was stale
today: a sentence about a remote ages at the rate this repository commits, which is the whole case for asking the remote
instead of reading the file. The bullet now also states its own limit: a commit cannot name the sha it lands under, so
the number the bullet carries is the count at the sha beside it and widens by one with every commit after it.

**The convention, now that it has bitten twice.** §4.22's entry — the nineteenth in this series — refreshed this same
bullet for this same reason, and left behind the residue every such refresh leaves: its own paragraph still reads
`prints 3`, and it is true, of the run that measured it. Both stay true because of a division this file has never stated
in one place, so it is stated here: **the bullet carries the present, the ledger carries the past**. §7 is undated and
re-typed whenever the remote is asked; §4.4's paragraphs are dated and never re-typed, because a measurement of a run is
evidence about that run, and rewriting it would destroy the only thing that makes a ledger usable — the ability to see
what was known when. The rule the nineteenth entry drew licenses both halves: a sha and the output of a query against a
remote are prose about the remote, and the remote is the only witness; a paragraph in this file's own history is prose
about a run, and the run is the only witness. What is *not* licensed is a third place that reads the remote aloud, and
there is none: the bullet is the only line in the corpus that states a present fact about `nexora/master`.

**Gates.** `npm --prefix packages/schema run verify` → the same **1220 checks**, green: no contract, no queue row and no
register entry moved, so nothing in the harness has a new thing to say. `node tools/check-doc-evidence.mjs` → green at
the four figures §4.4's twenty-first paragraph carries, every one of them unchanged, and the per-file census over the
61 Markdown documents under `docs/` prints no line — the edit adds no address, no pointer and no name, because a sha, a
refspec and the output
of a query are prose about a remote. `node tools/check-no-secrets.mjs` → the same **478 files**, no file added.
`go -C connector test -count=1 ./...`, `gofmt -l connector` and `go -C connector vet ./...` → the eleven `ok` lines, the
empty list and the silence they were in §4.23, because this section is neither Go nor contract.

---

### 4.25 Three rows deleted, and the assertion that made deleting them legal (2026-11-06)

**What landed.** `VER-06`, `VER-08` and `VER-10` are gone from `packages/schema/verification.json`, and the five
labelled sentences they were anchored to are gone with them: one in
`docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md`, one each in
`docs/architecture/20-target-repository-layout.md` §6 and §9, and two in that document's §11 table. Each of those
sentences now reads as a `Verified: YES` with the mechanism that earned it named beside it — the commit-label step, the
magic-byte scan and the `check_zip` of the `legacy-chain` job, and the `git diff --check` / `git ls-files --eol` pair —
each green on the three runner images `docs/audit/99-baseline-report.md` §12 tabulates. Two of the three got there by
failing first, and in both cases the thing that was wrong was the corpus rather than the tree: `VER-06` rejected a
label its own source document had never stated, and `VER-08` compared a total against a number recorded from a
CRLF checkout. `VER-10` has never been recorded red. `marker.counted` moves
208 → 203, the queue is 78 rows instead of 81, and the three distributions doc 39 §4 and §5 read aloud move with them:
`no-check` 13 → 10, the `ci` owner 7 → 4, the `ci-step` closing mechanism 6 → 3. `VER-11` is the fourth row whose run is
green and it stayed, and the reason is in its own subject: the half no run can supply is the dependency set a later host
resolves, and `package-lock.json` is untracked by design (§9.2). One guard the closed rows describe had a hole worth
filling while they were being closed — ADR-0019 rule 1 forbids an executable in a tracked path, and `.gitattributes`
declared only PDFs as binary — so that file gained the four executable suffixes as a declared classification, beside the
`connector/bin/` and `dist/` patterns of `.gitignore`. It costs nothing today: no such file is tracked, which is exactly
what the scan proves on every run.

**The collision, and what it cost to resolve honestly.** §9 rule 2 of doc 39 says a row closes by deletion, because this
queue has no state meaning closed. It did not say what happens to the id. The harness had an assertion that the range is
dense — `VER-01`…`VER-NN`, no hole, no duplicate — and three honest deletions leave three holes and a length that no
longer reaches its own highest number. The alternative was to renumber: shift `VER-07`…`VER-81` down and keep the
arithmetic pretty. That was rejected on measurement rather than on taste. These ids appear in the step titles of
`.github/workflows/verify.yml`, which GitHub has already printed in the logs of ten runs, and the baseline report quotes
those log lines verbatim as its evidence; they appear in the `ref` fields of closing mechanisms, in doc 39's own tables,
and in several hundred places across the corpus. Renumbering would not fail a check — it would silently change the
address every one of those citations already points at, which is the failure this corpus has spent forty slices
avoiding. ADR-0001 rule 2 states the rule for the neighbouring namespace — a number, once issued, MUST NOT be reused,
renumbered or recycled — and the queue has taken it over: ids are issued once, a closed row leaves the matrix and enters
a new `verification.json#/retired` ledger carrying the date, what it claimed and the artefact that closed it, and the
dense-range assertion was replaced by one that reads the two halves together. The check-count is unchanged at 1220,
because an assertion was replaced rather than added; what changed is what a deletion is allowed to look like.

**The precedent, which the corpus wrote before it needed it.** doc 39 §13's second open item has said since `VER-65`
was registered that closing it will add `LIVE-25`, "at which point §7's dense-id assertion must be amended in the same
commit, deliberately, rather than allowed to drift". That sentence is about the live range, not this one, and it has not
come due. What it did establish is the discipline this slice used: an id-range assertion is amended in the same commit as
the row that forces the amendment, by name, in the amendment list — rather than left to drift, and rather than repaired
by moving the rows to fit the arithmetic. The range over `VER` ids came due first.

**Gates.** `npm --prefix packages/schema run verify` → **green, 1220 checks**, the same count §4.24 reports, with one
assertion in it that did not exist an hour ago and three rows that do not. `node tools/check-doc-evidence.mjs` → green,
every citation resolved and no label cited that the harness no longer makes. `node tools/check-no-secrets.mjs` → the same
**478 files**, no file added: `.gitattributes` grew eleven lines and no bytes of any other kind.
`go -C connector test -count=1 ./...`, `gofmt -l connector` and `go -C connector vet ./...` → the eleven `ok` lines,
the empty list and the silence §4.24 reported, because this slice is corpus, not Go.

---

### 4.26 The first row closed by a script rather than by a run — `VER-01`, and `tools/check-adr-shape.mjs` (2026-11-06)

**What the row claimed, measured against the tree before anything was written.** `VER-01` said no script checks the ADR
index, the one-heading-per-file rule, the six section headings or the 60-140 line bound. Three of those four were already
checked — by two inline shell steps of `.github/workflows/verify.yml`, green on all three runner images since 2026-11-05,
which §6's own record of task #7 says in the same breath as it says the ADR still calls them planned. What did not exist
was the artefact the row names as its closer, a script under `tools/`; the 60-140 bound, which nothing read; the index's
`Status` and `Date` cells, which the shell compared by filename only; and any way to check any of it without a runner.
So this row was not waiting for a pipeline. It was waiting for a file — which is why doc 39 §8's first step could be
worked through in this order at all.

**What landed.** `tools/check-adr-shape.mjs`, 316 lines, over ADR-0001 rules 1, 2, 3, 4, 10 and 11: the filename grammar
and a numbering that must be exactly `0001`…`00NN` with no hole and no repeat; one `# ` heading per record and that
heading carrying its own file's number; the six `## ` sections in the one legal order, extras refused as firmly as
omissions; `Status:` and `Date:` above the first section, inside rule 4's vocabulary, with a `Superseded by ADR-NNNN`
that has to land on a record that exists; every record between 60 and 140 lines; and the index in `docs/adr/README.md`
naming exactly the records on disk, with `Status` and `Date` agreeing with the files themselves. It ends with a
`--selftest`: 10 record cases and 5 index cases, each a clean synthetic record with one defect injected, so the gate
proves it bites before it is trusted to be quiet — the standard `tools/check-no-secrets.mjs` sets and
`tools/check-legacy-backup.mjs` follows. Both halves run in CI and both are green here on the 20 records on disk, the
shortest 102 lines and the longest 140, exactly at the bound rule 11 states.

**The shell went rather than stayed beside the script.** 64 lines of workflow became 11, one rule now stated once, in
one language, in a file that can be run before a commit rather than only after a push. Keeping both would have given the
same obligation two owners and one of them a copy nobody edits, which is the failure mode ADR-0001 §Context lists as its
third reason for existing. The step that replaced them is named for the row it closes — `VER-01 -- the ADR set has the
shape ADR-0001 defines (rules 1-3, 10, 11)` — because §9 rule 8 of doc 39 says an issued id is an address, and the
address now points at the script instead of at the two lines that used to be its only executor.

**What closed, and what this closure does not claim.** `marker.counted` moves 203 → 202 for the one labelled sentence
`docs/adr/0001-adr-practice.md` ## Verification lost — it reads `Verified: YES` now, and its "(504 checks)" figure, last
true in Phase 1, reads 1 220; that record grew 100 → 105 lines, inside the bound the new script enforces on it for the
first time. The queue is 77 rows, `no-check` 10 → 9, the `tools` owner 6 → 5, the `tool` mechanism 6 → 5, and doc 39 §4,
§5, §6, §8, §10, §11 and §13 moved with them, `#/retired` gaining its fourth tombstone and its Retired table its fourth
row. What is *not* claimed anywhere in this paragraph is a runner execution of the script: the checks have run on
GitHub, as shell, and the next run of the workflow is the first that executes the file. That distinction is in the
tombstone, in doc 39 §8 and §13, and in this sentence, because a closure whose evidence is a local green run has to say
so or it is the thing §9 rule 5 was written against.

**The number a bullet miscounts.** doc 39 §13's first open item listed the rows closable without product code as
"those nine" while naming eleven of them — three contract assertions, two fixtures, six tool guards, one of which is now
closed, leaving ten and a sentence that reads correctly for the first time as "those ten". Registered rather than
silently fixed, because the corpus's own rule is that an arithmetic claim states what it counts: the list was ten, the
bullet said nine, and neither number moved when the row did.

**Gates.** `npm --prefix packages/schema run verify` → **green, 1220 checks**, four of them distributions this slice
moved. `node tools/check-adr-shape.mjs --selftest` and `node tools/check-adr-shape.mjs` → both green, described above.
`node tools/check-doc-evidence.mjs` → green; the workflow's `:31-33` citation still lands on the concurrency block the
53-line shrink of that file sits below, which is the reason the header comment was re-flowed to eight lines rather than
rewritten freely. `node tools/check-no-secrets.mjs` → **479 files**, one more than §4.25 reported: the new script is the
only content added to the tree and it is source, not bytes. `go -C connector test -count=1 ./...`, `gofmt -l connector`
and `go -C connector vet ./...` → the eleven `ok` lines, the empty list and the silence, unchanged: this slice touches no
Go file.

---

## 5. Verification state to the letter


- `cd omega-build && npm run deps && npm run dev && npx grunt && npx grunt release` → **exit 0** for the whole chain (Ladder A).
  Re-run end to end on 2026-11-05 by probe-4 — same four steps, same order, all exit 0, and its `npm run deps`
  reused this host's `node_modules` (five `up to date` lines), so it re-verified the chain rather than resolution.
- Artifact assertions: 353 files / 5 999 353 B / 252 `lib/` entries / 7 `_locales`, and `js/omega_pac.min.js` present at 742 272 B —
  every one of them reproduced from the 2026-11-05 rebuild, top-level path set included. On a clean image the *shape*
  figures reproduce and the *byte* figures differ from this host's, then agree with each other: thirty-six runner
  builds — the three images of each of the twelve executions of that file whose legacy chain got that far, eight of them
  rows §12 tabulates and four of them the runs its shape rule leaves out — gave 353 entries, 252 `lib/`,
  7 `_locales` and the bundle, at 5 974 389 B / 5 974 444 B (ubuntu) and 5 974 401 B / 5 974 456 B (windows) with a
  735 547 B bundle, the same four totals in every one of those runs — so the totals and the bundle size are properties of a CRLF
  checkout against this host, and of the runner image against a different image, which is why the workflow
  prints them as NOTEs (`docs/audit/99-baseline-report.md` §12). The hashes are the sharper half: seventy-two computed
  sha256 values across those thirty-six builds, seventy of them distinct. The one repeat is a pair, and §12 explains
  it — `96d1613` reached two refs, and the windows/Node-24 cell of one run and the windows/Node-22 cell of the other
  printed the same two hashes 1.3 s apart, so "no two builds ever match" was a count that had not yet reached a
  collision rather than a law. The ledger listing the values is the final block of
  `docs/audit/raw/probe-8-runner-runs.txt`; none of the seventy is either of the two recorded below, and every further execution of
  that file adds three builds and six hashes to a set that has never once matched a recorded value, which is the form
  this finding keeps rather than the count it had.
- `sha256 dist/chromium-release.zip` = `b93d5100e4c05ccf7af365dfc1742344360579f482df72e4e9ce00f5ba964ba8`
- `sha256 dist/firefox-release.zip` = `b2309ce3bd0c2be246f0c228ffde1290fa1756c10ad93d90f17380249b4a728f`
  (2026-11-05 values: `73eee79d015ed404dc4bfdaa6d634ae0626a0612b2d603cb3412e6e47be68a4f` and
  `5ab2ca82a076ac83bbb1396630be6b7732a080fc3c25109b15a9caac92f9a887`. **These four hashes are observations of
  four builds, not a floor**: the 2026-11-05 rebuild of the *same tree on the same machine* moved both values,
  because the zip stores each entry's build timestamp. That is the strongest available evidence for the rule
  "the artifact *content* assertion is the gate, the exit code and the hash are not" — and why `verify.yml`
  prints a hash beside these instead of asserting it.)
- Test floor: `omega-pac` **142 passing / 1 failing**; `omega-target` **8 passing / 5 failing**; `omega-web` and
  `omega-target-chromium-extension` **0 test files** (there `grunt test` succeeds vacuously — no `test/` directory
  exists, and no npm `test` script is declared; the `VER-11` step of `.github/workflows/verify.yml` prints
  `NOTE: <project> has 0 test files` instead of counting that as coverage).
  probe-4 reproduced both floors to the individual test on 2026-11-05 (`142 passing (95 ms) / 1 failing`, the same
  `omega-pac/test/profiles.coffee:52` assertion; `8 passing (6 s) / 5 failing`, the same two `OptionsSync`
  assertions and three timeouts), and measured that **neither project's `npm test` reaches the suite**: `omega-pac`
  on the `TZ=` prefix, `omega-target` because `grunt` ships no `bin` and `grunt-cli` is a dependency of the hub only,
  so its `node_modules/.bin` has no `grunt` shim. The working invocation, here and in CI, is the hub's `grunt-cli`
  with `TZ` exported.
- `node tools/check-no-secrets.mjs --selftest` → 13/13 green. Its plain run enumerates **478** files — §4.19's 476 plus
  the two `connector/cmd/nexora-connector` files §4.20 stages, because the guard walks `git ls-files` and so counts a file
  when it is staged, not when it is thought of. It carries sixteen `nexora-secret-allow` annotations now: the eight
  documented constants in `connector/internal/compile/resolve_test.go` that §4.16 lists, §4.17's three in
  `keystore_test.go` and `ref_test.go` twice, §4.18's one in `connector/internal/apply/apply_test.go:17`, and §4.20's four
  in `config_apply_test.go` — the synthetic credential value, profile id and server id in its constant block and the second
  server's id in its fixture — because Go's test convention puts a `_test.go` beside the package it tests and therefore
  outside every path the guard exempts.
- `npm --prefix packages/schema run verify` → **green** (1220 checks: §4.21 adds three, and what they add is the
  harness opening two documents it used to quote; see §4.4).
- `go -C connector build ./... && go -C connector vet ./... && go -C connector test ./...` → **exit 0**, all on
  2026-11-05 on this host with Go 1.27.3 (§4.6), and in the `connector` job of `.github/workflows/verify.yml`, which
  is enabled for `ubuntu-latest` and `windows-latest` on Go 1.24. Since §4.7, §4.8 and §4.9 the same invocation printed
  six `ok` package lines where §4.6 recorded three — `contract`, `dispatch`, `envelope`, `frames`, `hello` and `peer`
  — §4.13 makes it seven with `github.com/k2dms/nexora/connector/cmd/nexora-connector`, §4.15 eight with
  `github.com/k2dms/nexora/connector/internal/compile` and §4.17 nine with
  `github.com/k2dms/nexora/connector/internal/keystore`; §4.18 ten with
  `github.com/k2dms/nexora/connector/internal/apply` and §4.19 eleven with
  `github.com/k2dms/nexora/connector/internal/supervisor`; `gofmt -l connector` still
  prints nothing. No `VER-NN` row moves on any of the increments up to §4.16: §4.6's "Not yet"
  list is the list of what those rows ask about, §4.7 gives the reason `VER-49` keeps its row now that two of the three
  clauses its closer names have a test, §4.8 names the three rows its own tests touch and why all three close by a live
  row rather than by one more Go test, and §4.9 names the live row whose mechanism is half in this repository and the
  question two of this repository's own packages raised against each other; §4.10 names the three pieces of contract
  data that answered it and the boundary the answer drew; §4.11 names the four that stopped the next package before its
  first line; §4.12 names the contract sentence that had been naming the wrong process for as long as the tests had
  been green; §4.13 is the first slice since §4.7 to move the `ok` count rather than the prose beside it, §4.14
  moves no Go code at all — it registers what the running binary proved the contract does not say — and §4.15 moves the
  count again with a package whose subject is a config rather than a frame, which is the first Go code in this
  repository able to produce a `configHash` and therefore the reason `VER-78` exists to name which byte string it is a
  hash of. §4.16 fills that package in — stages 1 to 4, the document half of 5 and stage 7's inspection — and moves no
  `ok` line, because the eight are packages and §4.16 is thirty-eight more test functions inside the eighth. §4.17 is the
  ninth package, and the first slice to move a row's *state* rather than its count: `VER-23` leaves `no-code` for
  `no-run`, which is the one subtraction the queue's `no-code` column has had, and why doc 39's distribution and the
  harness assertion that reads it both move by one. §4.18 is the tenth package and the second such subtraction — the
  materializer and the owner-only write, with `VER-29` following `VER-23` out of the same column — and §4.19 the eleventh
  and the third, with `VER-14`. §4.20 moves no `ok` line, because it is the seventh package's second file rather than a
  twelfth package: sixteen test functions inside `cmd/nexora-connector`, its compile-and-apply-and-supervisor path, and
  the slice that turned a *runtime* fact about this repository's own test device into a code change two packages away —
  the `select {}` child that Go's deadlock detector kills, which §4.20 measures and §4.19's tests never could see.
  §4.21 moves no `ok` line either — one more test function inside `internal/dispatch`, and the only change a
  client can see is a sentence.
- `node tools/check-doc-evidence.mjs` → **green** (2311 `path:line`, 537 pointers, 1491 doc-and-script
  names, 68 harness assertion labels — the fourth figure is new with §4.22 and no GitHub runner has printed it yet;
  `PLANNED_DOCS` empty; §4.4 has the same figures and the same provenance for each change in them. These are not
  local to this host: `42025558933` and `42027072602` printed exactly `2260 / 487 / 1287`, `42029092102`,
  `42029386377` and `42033189223` printed `2260 / 487 / 1289`, `42035782395` printed `2260 / 487 / 1290`,
  `42046629162` printed `2262 / 487 / 1306`, and the three earliest executions printed `2252 / 487 / 1253` for the corpus
  as it stood at `ceedabf` — `docs/audit/99-baseline-report.md` §12. Any commit that moves a citation moves all three, so they are read as a
  set and never pinned. No runner set has been *read* since: the two guard runs this host printed after that execution
  are records of this machine, and §4.4 says which slice moved which of the three figures and in what shape.)
  `node tools/check-legacy-backup.mjs` → **green** (`--selftest` 4/4).
  `node tools/verify-vendor.mjs` runs against the
  working tree.
- `node tools/check-adr-shape.mjs --selftest` and `node tools/check-adr-shape.mjs` → **green here**: 15 injected
  defects refused (10 record cases, 5 index cases) and 20 records plus one index passing, the shortest record 102 lines
  and the longest exactly at rule 11's 140. No runner has printed these two lines yet — the file arrived on 2026-11-06
  and the push that would show it asks for its confirmation each time — while the checks themselves have run there since
  2026-11-05 as the two shell steps §4.26 replaces. It is the `VER-01` step of the workflow bullet below, and `VER-01` is
  in `verification.json#/retired`.
- `.github/workflows/verify.yml` → **written, step-verified here, and run on GitHub runners since 2026-11-05 —
  §12 tabulates ten of those runs: three red, six times green, once cancelled** (`docs/audit/99-baseline-report.md` §12). What the green runs answered is the question
  §4.5 and `docs/audit/02-toolchain-decay-and-risk.md` §7.3 used to leave open — a clean image can install and build
  this tree: all five roots from an empty `node_modules`, no lockfile in git, both pinned test floors reproduced,
  every artifact content figure reproduced, and the 252 `lib/` entries produced without this host's Bower cache
  (probe-5 had already removed the transport half of that question: a cold `bower install` of the two `git://` deps
  succeeds **without** any rewrite, because bower normalises them to `https://github.com/…\.git` before git is
  involved, while `git ls-remote git://…` from a shell still times out). The sixth run's cancellation is a fact about
  this file's `concurrency` block, not about the tree: it cancelled two cells that had already printed every guard and
  uploaded both artifacts, and it is why a push here now waits for its run instead of stacking behind the last one.
  What no run of it did, by itself, is move a `VER-NN` row: `packages/schema/verification.json` has no state meaning
  closed, so a met row is deleted together with the labelled line that anchors it, and that is a commit of its own, not
  a status edit in this file. §4.25 is that commit, and the three rows whose condition these runs met are gone from the
  queue — `VER-06`, `VER-08` and `VER-10`, retired in `packages/schema/verification.json#/retired` rather than
  renumbered, because a `VER-NN` is an address the corpus, the CI step names and the runner logs have already used.
  §4.26 is the second such commit and the first whose closer is a file rather than a run: `VER-01` is the fourth id in
  that ledger, retired on the day `tools/check-adr-shape.mjs` arrived, with the runner half of its evidence still owed
  to the next execution of this workflow and said so in the tombstone.

---

## 6. The exact next steps (task #6 → #11)

**#5 `packages/schema` — done.** It is now the green, re-runnable test of this iteration:
`npm --prefix packages/schema run verify` → `OK: 13 schemas compile, 19 command docs conform, 20 examples
valid, 24 invalid examples rejected, 112 protocol frames replayed.` (1220 checks; details and the 8 contract
defects the fixtures exposed are in §4.4). Every step the earlier draft of this block prescribed is
implemented — `envelope` is in `SCHEMAS_BY_PREFIX`, the six `fixtures/protocol/*.jsonl` clear `frameCount`
and `replayPairs`, `verify.mjs` validates each frame's `args`/`result` against the compiled per-command
sub-schemas, and the import-staging `expiresAt` lifetime plus events-catalogue parity are machine checked.
The self-deactivating hold-backs named in the earlier draft (`26-command-surface.md`, then
`35`, `36`, `37` and `38`) have all fired: those documents exist, so their cross-checks run as assertions.

**#6 `docs/audit/00-12 + 99` — done.** Every claim carries `path:line` evidence and a `VERIFIED` / `INFERRED` /
`UNVERIFIED` tag; `99-baseline-report.md` carries the raw probe output, the 8 lint violations, the abort chain,
the 352-vs-353-file / 742 272-byte finding, the test floor, "Bower works here but is not reproducible", and the
literal **"not fixed" list** from plan §4 (Grunt migration, CoffeeScript→TS, Bower→npm-runtime, dependency
upgrades, `npm link`→workspaces, CSP hardening, permission reduction, `minimum_chrome_version`, adding `key`,
rewriting `PacGenerator`/`Conditions`/`RuleList`, new UI tests, restoring 20 locales, NEXORA branding in
`release.yml`, installing Go, live tests in CI, the three dirty files, adding tests to omega-web/chromium-ext,
and the SEC-1 code fix).

**#7 `docs/adr/0001-0020` — done.** Format fixed by 0001 (`Status/Date/Context/Decision/Consequences/Alternatives
considered/Evidence/Verification`); numbering append-only; supersession only via a new ADR, never by editing.
Titles are in plan §3. `docs/adr/README.md` index matches the files, and the machine checks of that match plus of the
record shape ADR-0001 rules 1-3 require live in one script, `tools/check-adr-shape.mjs`, which the `VER-01` step of
`.github/workflows/verify.yml` runs after its own `--selftest`; they lived in that workflow as two inline shell steps —
"The ADR index lists exactly the ADR files on disk" and "Each ADR has the shape ADR-0001 defines" — which printed
`OK: the index matches 20 ADR files, exactly.` and `OK: 20 ADR records each carry their number, …` on each of the three
runner images on 2026-11-05 (`docs/audit/99-baseline-report.md` §12) and are the reason §4.26 could close the row on
checks that had already run rather than on a check that had only just been written. The ADR's own `## Verification`
section no longer calls them planned: it names the script, and `VER-01` is in `packages/schema/verification.json#/retired`.

**#8 `docs/architecture/20-40` — done: 20 to 40 are on disk.** `26-command-surface.md` names **exactly** the
19 allowlist methods and the cross-check is a live assertion, not the `DEFERRED:` note the earlier draft
described. Document rules: normative `MUST/MUST NOT/
SHOULD/MAY`, `Status:` header, `Trace: ADR-…; Contract: packages/schema/…` footer, and the iron rule that no
document asserts behaviour nothing checks — otherwise it gets a `39-verification-matrix.md` row with
`Verified: NO — deferred to Phase 2`.

`39-verification-matrix.md` + `packages/schema/verification.json` are that rule's endgame: the corpus's `202`
deferral labels over `44` documents are partitioned, on every run, into the `152` lines `77` rows own, `26` sentences
that define the
label and `24` live rows the matrix owns — so a new deferral with no row, a row whose anchor moved, a row re-labelled
instead of closed, or a document reading a distribution the data does not have, all fail the build. Writing it also
caught seven citations that pointed at a live row which does not test the claim: `LIVE-08`→`LIVE-13` for the
containment/reconnect family (two places in `21-…`), `LIVE-05`/`LIVE-06`→named assertions for framing enforcement
(`25-…`), `LIVE-05`→the applied-config-hash assertion for `core.select` (`26-…`), `LIVE-02`→golden configs for
byte-determinism (`27-…`), `LIVE-11`→`LIVE-12` for the 404 rule (`29-…`) and `LIVE-06`→`LIVE-11` for the fail-closed
observation inside `docs/verification/live-matrix.md` itself. The stale `109 fixture frames` / `707 checks` numbers in
the architecture documents are gone; the run prints its own counts and nothing depends on them.

The open-decisions register — doc 40 and `packages/schema/open-decisions.json` — does the same for the questions
rather than the checks: `53` sites across `13` documents defer to that file (five more lines name it only to list it,
and are declared), and the run partitions them into
`33` entries `ODEC-01…ODEC-33` —
each with the kind of artefact that would close it (`product` 14, `contract` 11, `adr` 5, `measurement` 2,
`upstream` 1), three options with their costs, and the `VER-NN`/`COMP-NN` rows it blocks. The site rule is the data's
and is executable: a line naming the file by basename **or** path is a site, a header ending in a colon defers its
bullets and not itself, and the five lines that merely list the document or say where an answer belongs are declared in
`#/notADeferral` rather than silently subtracted. Two properties are worth the machinery: an entry with no citing
line is a question the register invented, and an answered question the register still claims fails the count — so the
register cannot outlive its own questions. Writing it exposed a third class of defect: `VER-61` (subscription
delivery) had cited `27-…` §5's deferral line, which is the 64-inbound ceiling and now belongs to `VER-73`, and
`33-…` §11's, which is the compiler's door-back prohibition and now belongs to `VER-27` — because the delivery
question existed nowhere in the corpus, only as `docs/PROJECT-STATE.md` §9's fourth note to a person.
`docs/architecture/29-subscriptions-and-servers.md` §13 carries it now, the labelled corpus is `202` rather than
`201`, and `ODEC-33` gates `VER-61`.

The two figures in the paragraphs above are the ones those slices measured, and they have moved since: §5 and §4.4's
final ledger paragraph carry today's partition of the labelled corpus and today's size of the register. This block is
the record of how the machinery was written, not a second place to read its counts.

**Still owed in the corpus, deliberately not done here:** (a) `109 frames` / `504 checks` / `707 checks` remain in
five ADR `## Verification` sections — ADR-0001 rule 5 forbids correcting history in place, and
`docs/adr/0012-nexora-keys-under-dash-prefix.md:106` shows the intended form ("green at the time of writing");
`docs/adr/0020-test-strategy.md:86` likewise calls doc 39
"not yet written", which is recorded as stale-by-design in doc 39 §13. (b) — **done, §4.22.** The item said `49`
citations across `13` documents; measured against the commit before it was `52` across `14`, plus three more the
checker could not see at all. All 55 now name the assertion they describe, `packages/schema/scripts/verify.mjs` joined
`MUTABLE_CITATION_TARGETS`, and the rule the `docs/README.md` convention had been recommending since it was written is
enforced by `staleLabel` in `tools/check-doc-evidence.mjs`.

(c) The same defect already happened once here, at scale: `21` citations pointed at *this* file by line number, and the
paragraphs written above them moved eight of them onto the wrong decisions while they still resolved to non-blank
lines. All `21` are now `docs/PROJECT-STATE.md` §n references, `docs/README.md` states the rule, and
`tools/check-doc-evidence.mjs` rejects a line cite into this file the way it already rejected a line cite into itself.
Two of the eight had also been citing the wrong section even when the numbers were fresh, which is the part a line
number cannot express: `:414` was read as the version-continuity decision and is §2's wipe trap, `:402` was read as the
CircleCI decision and is §9's third question. One ADR file moved with them — `docs/adr/0001-adr-practice.md`, whose
Context and Evidence each carried a line cite into this file and were converted to `§6` references. That is not
rule 5 breaking its own rule: rule 5 protects a *decision* from being rewritten, and what changed here is the address
of a citation the corpus's own checker now rejects. The decision, the section sequence and the deferral line are
byte-identical; `git diff docs/adr/` shows two hunks and nothing else.

**#9 `docs/verification/*` — done, five of five.** `live-env-contract.md` (missing env var ⇒ `SKIPPED`, **never** `PASSED`),
the `LIVE-01…24` matrix, `backup-from-3.5.2.json`, `backup-3.5.2-all-profile-types.json` and now `README.md`, which
indexes what runs, in which order, and what would have to exist before a row could move. `PLANNED_DOCS` in
`tools/check-doc-evidence.mjs` is empty: no document in the corpus is named without existing.

The README's §3 needed a command that did not exist, because `LIVE-14` claimed "the legacy engine compiles both
today" and nothing compiled them. `tools/check-legacy-backup.mjs` is that command: it loads the built browserified
engine (`omega-pac/index.js`, a build output, so its absence is reported with the build step rather than passed over),
hands both envelopes to `Profiles.each`, compiles every profile it yields, evaluates every body against five sample
requests, and fails if an answer names a profile the envelope does not contain. It measures 11 profiles, 9 PAC bodies
and 45 request evaluations, and `--selftest` rejects four broken envelopes — a dangling profile reference, a key the
`+`-prefix rule makes invisible, an unknown profile type, a missing `schemaVersion` — so the green run cannot be an
empty check. Writing it also corrected two things: the engine compiles a condition-free profile to a bare result
*string* rather than a function (the first version treated that as a crash), and `+system` is refused by the engine
itself, which is behaviour to expect rather than a fixture to fix.

**The provenance claim in the earlier draft of this section was false and is corrected here.** The two envelopes were
described as "the real `.bak` files produced by today's `io.jade` export"; no browser was run in this iteration, so
they are exporter-*shaped* files written from the audit's reading of `omega-web/src/partials/io.jade` and
`omega-target/src/options.coffee`, carrying example hosts and no credential. `LIVE-14`'s status cell now says that in
its own words: the compile half runs against the real engine, and a genuine `.bak` from a real 3.5.2 install is part of
what closes the browser half.

**#10 Baseline fixes — done: `.github/workflows/verify.yml` is on disk, has run green there, and the `TZ=` question
is decided (in `docs/audit/02-toolchain-decay-and-risk.md` §4, not in a legacy file).** Three jobs: `contracts` (install
`packages/schema`, `npm run verify`, `check-doc-evidence.mjs`, the two ADR record checks below, `check-no-secrets.mjs`
with its `--selftest` first, then the guards that only a machine can hold — root-bare `VER-04`, import boundary
`VER-02`, no-protocol-in-JS `VER-03`, no-executable-bytes `VER-08`, line endings `VER-10`, commit shape `VER-06`),
`legacy-chain` (`needs: contracts`; transport rewrite → install×5 → `npm run dev`×4 → hub `npm run build` → `VER-11`
test floors → `grunt release` → zip **content** → checksum beside the recorded baseline → `verify-vendor.mjs` →
`check-legacy-backup.mjs` with `--selftest` → `upload-artifact`, never a release), and `connector` present with
`if: ${{ false }}` so the absent Go job stays visible. Matrix: ubuntu-latest/22 (parity with
`.github/workflows/release.yml:21`), windows-latest/22, windows-latest/24 with `continue-on-error`. It parses
`N passing` per project and fails below `142/1` and `8/5`, and prints `NOTE: <project> has 0 test files` for the two
projects whose glob matches nothing. Deliberately absent, as planned: `web-ext lint`, the live matrix, any Go-shaped
root file — plus `engines`, because adding it is a human decision (§7).

Three deviations from the text of the plan, each on purpose. (1) Triggers are `push` to `master` and `nexora/**`
plus `pull_request` to `master`; the plan said "push/PR on `master` and `docs/**`", and this repository's
documentation branches live under `nexora/` (`nexora/architecture` is the working branch), so `docs/**` would have
been dead config. (2) The schema verify, secret scan and doc-evidence gate run in the *first* job rather than after
the legacy chain, so a contract break fails in ninety seconds instead of waiting for five `npm install`s. (3) It
also does the ADR-index check the plan assigned to it (§6 #7) **plus the other three checks ADR-0001's own
`## Verification` promised** — one `# ADR-NNNN:` heading matching the file's own number, `Status:` in one of the
three legal states with a dated `Date:` line, the six sections in the one legal order and no others, no number issued
twice and no gap in the sequence — the `git://` transport rewrite, which the plan did
not foresee but a clean runner needs. Both ADR steps were run here: the index step prints
`OK: the index matches 20 ADR files, exactly.` and the shape step
`OK: 20 ADR records each carry their number, their status and date, and the six sections in order.`; each was then
re-run against six mutations of a copy of `docs/adr` (a cloned number, a renumbered file, swapped sections, a stray
`## Notes`, a second H1, a `Date: last week`) and failed on all six, while a legal `Status: Superseded by ADR-0021`
passed.

The `TZ=` half: the `omega-pac/Gruntfile.coffee` wrapper was **rejected** (it would give the test's zone to `build`
too) and the POSIX prefix in `omega-pac/package.json:32` stays; CI exports the zone for the one step that needs it.
`docs/audit/02-toolchain-decay-and-risk.md` §4 has the reasoning, and the honest test-side fix (explicit UTC in
`omega-pac/test/conditions.coffee:468`) is a Phase-2 change made together with the suite that replaces this floor.

Writing and testing the workflow, and then re-running the whole chain for #11, produced four measurements that
corrected the audit — and one of them corrected a correction made a day earlier. (1) The `git://` transport is
dead **from this host** as far as git is concerned, but probe-3's Bower success was **not** explained by the
already-installed components: probe-5 moved `omega-web/bower_components/` aside and the cold install resolved both
`git://` deps over https anyway, with `git://` appearing zero times in the log, because bower normalises the
endpoint before git ever sees it — which is why `verify.yml`'s `insteadOf` step is now described as defence in
depth, not as the thing that makes the build possible (`docs/audit/02-toolchain-decay-and-risk.md` §1.1,
`docs/audit/99-baseline-report.md` §7). (2) `cmd.exe //c "npm test"` in `omega-pac` exits 1 on the `TZ=` prefix, so
a deduction became a fact (`docs/audit/02-toolchain-decay-and-risk.md` §4). (3) `grunt 0.4.5` declares no `bin`, so
`npx grunt` in a leaf project cannot work and `omega-target`'s own `npm test` dies the same way `omega-pac`'s does,
for a second reason; CI therefore invokes the hub's `grunt-cli` from `omega-build/node_modules/.bin`, which is also
what keeps ADR-0018 rule 3's single driver intact. (4) A rebuild on the same machine of the same tree reproduces
every artifact content assertion and moves both sha256 values, because the zip stores per-entry timestamps (§5).
What is still true: its `run:` blocks were validated by extracting and executing them here first, then re-running
each guard against a synthetic violation so the guard is proved able to fail (§7.3 of
`docs/audit/02-toolchain-decay-and-risk.md`) — and the runs that followed showed how much that was worth,
because they found two defects no host shell could: a Windows `$GITHUB_WORKSPACE` that is not a POSIX path, and a
byte total that belonged to one checkout rather than to the build. Both were reproduced here before being fixed, and
both are green on every run since. A third thing the runs found is not a defect at all: `concurrency.cancel-in-progress`
discarded a verdict in run six while the two cancelled cells had already printed every guard, which is recorded in §12
and is why a push here waits for its own run. What no run has done is open a browser; `LIVE-01…24` are still `SKIPPED`, and the
rows in `docs/architecture/39-verification-matrix.md` keep the states the data gives them for the reason §12 states.

**#11 End-to-end verification and report — done for everything a shell on this host can run, and not further.**
Two probe rounds, both archived: `docs/audit/raw/probe-4-e2e-re-verification.txt` (the Ladder A chain → both
legacy suites → all five gates → artifact content, hash and top-level set → `git status` after the build) and
`docs/audit/raw/probe-5-bower-cold-install.txt` (the transport question probe-3 had left inferred). The results,
and the four sentences in this corpus they corrected, are `docs/audit/99-baseline-report.md` §10 and §11. In one
line each: the chain is green; both pinned floors reproduce to the individual test; all five gates are green;
every artifact **content** assertion reproduces; the artifact **hash** does not, on the same machine from the
same tree (§5); `npm test` reaches no suite in either leaf project, for two different reasons (§5); a cold
`bower install` of the two `git://` deps works *without* the rewrite CI sets, because bower normalises them to
https before git is involved; and the build leaves no git noise, which is what the `dist/` line in `.gitignore`
bought.

**The plan's browser half was not done and cannot be done from here.** Loading `dist/chromium-release.zip`
unpacked, reading `chrome://extensions` for errors, walking the six options states, importing
`docs/verification/backup-from-3.5.2.json`, switching profile and confirming through `chrome.proxy.settings.get`,
then exporting from the running extension and diffing that export against the shipped fixture — the export being
also what would replace the fixtures' exporter-shaped values with a genuine 3.5.2 `.bak`, the half `LIVE-14`
still owes — all need a person and a real Chrome. Under `docs/verification/live-env-contract.md` an unexecuted
row is `SKIPPED`, never `PASSED`, so `LIVE-01…24` and `docs/verification/live-matrix.md` are untouched by #11.
That is a boundary, not an oversight.

**The item that belonged here and nowhere else — the first execution of `.github/workflows/verify.yml` on a runner —
is done.** It needed a push, so it was gated on the confirmation rule in §7; it happened on 2026-11-05 and has
happened since, and `docs/audit/99-baseline-report.md` §12 carries the measured output of the ten runs it
tabulates, with the raw capture of all fourteen executions — job
conclusions, every `##[error]` annotation, and each quoted line beside the number of times the runner printed it — at
`docs/audit/raw/probe-8-runner-runs.txt`. That is the raw record this file's own convention asks for. #10 is therefore
no longer "written and locally step-verified"; it is written, run, three times failing, six times green and once cancelled —
and §12 states the rule that keeps that sentence true under the runs still to come. The third failure is the one that
did not refuse a contract: a Go test in `connector/internal/peer/` that polled an in-flight counter for ~1 s instead of
waiting for the frame the peer had already issued, and lost to the runner's scheduling. Two items take its
place, and neither is a status edit:

1. **The row bookkeeping — executed, and §4.25 is its record.** `VER-06`, `VER-08` and `VER-10` met the condition
   their records state — a green run of that file — and the queue has no state meaning closed, so they closed by
   deletion: the rows out of `packages/schema/verification.json`, their five labelled sentences out of
   `docs/architecture/20-target-repository-layout.md` and `docs/adr/0019-xray-as-integrity-pinned-downloadable-asset.md`,
   `marker.counted` re-derived 208 → 203, and the hard-coded state distribution in `packages/schema/scripts/verify.mjs`
   amended in the same commit. Deleting them cost more than the arithmetic: the dense-id assertion in the harness demanded
   an unbroken `VER-01…VER-NN` range, and a range with holes is what three honest deletions leave behind. Rather than
   renumber ids that CI step names and runner logs already cite, the queue now records them in a new
   `verification.json#/retired` ledger, and the assertion reads `"every VER id the queue ever issued is live or retired,
   no id is recycled, and a retired one says what it was and what closed it"`. `VER-11` reached the same green and stayed,
   because the half its subject still owes is a lockfile and `package-lock.json` is untracked by design (§9.2).
   `VER-02`/`03` stay (they scanned roots that hold no code yet), and `VER-04`/`05` stay queued against a
   `check-layout-guards` tool that has not been written.
   The same bookkeeping ran a second time the next sitting, one row at a time and with a different kind of evidence:
   **§4.26 closes `VER-01`**, whose closer was a file rather than a run — `tools/check-adr-shape.mjs`, the artefact the
   row's own `closesBy` names. The ledger has four ids, the queue is 77 rows, `marker.counted` moved 203 → 202, and doc
   39 §8's first step is nine rows wide instead of the ten it listed while `VER-01` was one of them.
2. **The browser half.** `LIVE-01…24`, unchanged and `SKIPPED`, need a person with a real Chrome, and `LIVE-14`
   needs the export that would replace the fixtures' exporter-shaped values.

**Connector, in the order the user set on 2026-11-05.** M1 is §4.6's four packages — the last thing that could be
written before any behaviour existed. What each next milestone has to make true:

1. **M2 — both processes as far as they are testable without Xray.** Four in-process packages are written, and §4.13
   joins them into a process that runs: §4.7's `connector/internal/peer/` is the endpoint that refuses an oversized write before it reaches the writer,
   bounds the outstanding-request and in-flight-byte budgets, discards a late answer by `id`, ends the stream on an
   illegal inbound frame, and treats a closed port as the client going away rather than as a shutdown signal;
   §4.8's `connector/internal/dispatch/` is the front door that resolves a method against the closed allowlist, gates
   it on the sender, the negotiation and the announced capabilities, and counts the per-sender budgets before any
   handler can half-apply; §4.9's `connector/internal/hello/` is the frame that produces the value the third of those
   gates reads; and §4.10's `connector/internal/envelope/` is the codec that stops at "is that a frame" and leaves
   "is that a method" to the front door, which is what lets the corpus's own `UnknownMethod` exchanges happen inside
   one process. What §4.8's package does *not* have an honest way to do is the heartbeat clause, for the reason §4.7
   gives and §9 asks. Remaining, in the order they build on each other: `internal/chunk`, the reassembly that
   `packages/schema/protocol/framing.json#/oversizedPayloads` requires to fail a whole import with
   `IntegrityMismatch` rather than apply part of one — the gate this list put in front of it, §9's ninth question, was
   answered in §4.10, so the codec no longer pre-empts the front door's verdict and a chunk arrives as a frame; the
   package then met a second gate, found by reading the contract rather than by writing code, and §4.11 records what
   the contract does not say. `VER-76` and `ODEC-34` are that gap as data, §9's ninth row is it as a question, and this
   list will not put a third item in front of `internal/chunk` until one of the two surviving readings is chosen;
   then `cmd/nexora-connector`, the process that reads a pipe and owns a dispatcher — which is exactly where
   `internal/envelope` and `internal/dispatch` met and disagreed, and now meet in the test §4.10 names — is written,
   measured and was run against the corpus's own frames, and §4.13 records what it answers, what it refuses, and the one
   sentence of its output that is imprecise; then `cmd/nexora-host`, which is the only item on this list stopped by a
   decision rather than by a dependency: §4.14 registers `ODEC-35` and `VER-77` because no file in this repository says
   how the identity the platform resolves for a port reaches the process on the far side of the local pipe — ADR-0005
   rule 2 promises that pipe a message schema versioned separately and none is defined, the envelope is closed at
   fourteen properties none of them a sender, and ADR-0004 rule 3 forbids a payload field from *becoming* identity.
   Until an ADR chooses among the three shapes that survive, the honest host is one that injects the same demo identity
   the connector is already told, which is a second binary repeating the first rather than a second hop.
   `VER-49`, `VER-13`, `VER-51`, `VER-52`, `VER-55` and `VER-77` all ask about that behaviour, and
   `docs/verification/live-matrix.md`'s `LIVE-01` asks about the negotiation. §4.7 records why `VER-49` in particular does not close on the peer alone; §4.8 records why the
   three sender-policy rows need a running host and cannot close on a unit test; §4.9 records why `LIVE-01` names two
   test halves and only one of them is in this repository; §4.10 records why a boundary that only decides *which*
   package speaks moves no row either; and §4.11 records why reading the first item on this list, rather than writing
   it, moved the queue up instead of down. §4.12 records the mechanism by which a contract sentence naming the wrong
   process stayed green, §4.13 that the sentence was testable both ways at once, and §4.14 that this list's last item is
   a question with three surviving answers rather than work nobody has done. Three cheap items queued beside this
   milestone, none of them a slice: `connector/internal/hello`'s gate answers a frame that arrived *after* a
   `VersionMismatch` withdrew a negotiation with the wording that it arrived before one — the state is right and the
   sentence is wrong (§4.13); §4.12 leaves an assertion un-written rather than race a concurrent editor of
   `packages/schema/scripts/verify.mjs`, the harness reading §8 of `docs/architecture/31-security-model.md` and
   `packages/schema/protocol/framing.json#/senderPolicy/rateLimitFacts/countedWhere` for the same process name so the
   two cannot drift apart again; and §4.14 finds the same shape one level down — an assertion whose message says it
   checks what the queue document reads aloud compares the data to a constant and never opens that document. The check
   that would make doc 39's tallies un-rottable is the third item here, not a variant of the second: one reads a
   document against one field, the other reads a document against the whole distribution. All three are a
   string and a check in a file the next real slice already opens; none is small enough to be done in a commit that
   would then have to be found again. **§4.21 does all three**, in the order above, in the file §4.20 had already
   opened; the paragraph stays as written because it is the record of why they were queued rather than folded into a
   slice, and what changes here is only that nothing on this list is owed any more.
2. **M3 — the Core supervisor, config compiler, keystore, prober, subscriptions**, plus the parts of it that §9 still
   leaves to a human. This read "one of the five is begun" when §4.15 landed doc 27 §2's stages 5 and 6 — the serializer
   and the hash — and stopped there, because stages 1 to 4 are what decide what the serializer would be claiming to have
   compiled; §4.16 wrote those stages, §4.17 the keystore and §4.19 the supervisor, so three of the five are packages on
   disk, the compiler is begun, and what is left is the prober — whose last stage needs the Core binary §4.13 records as
   absent — and subscriptions, which no command surface has reached yet and whose one registered gap is now `ODEC-40`:
   where the format an `auto` parse resolved to is recorded. §4.23 is that change, so the fifth component is stopped by a
   decision rather than by a document nobody had read. The order the rest builds in is §10's closing
   note.

---

## 7. Git rules of this session (do not improvise)

- Branch: **`nexora/architecture`** at `4250893`. The remote is not where this bullet last left it: `git ls-remote nexora
  master` prints `7544baa`, and `git rev-list --count nexora/master..HEAD` prints 9 — the config.apply surface
  (`293cb73`), M2's cheap tail (`d1d22bb`), the citation conversion (`31008a5`), the ledger line that recorded those
  three (`18f1e1b`), `VER-81`'s registration (`f4cac24`), the three ledger lines after it (`e1020b5`, `8730c7a`,
  `6d222ae`) and the commit that deleted `VER-06`, `VER-08` and `VER-10` (`4250893`) are committed here and unpushed,
  because the rule further
  down this list asks for its own confirmation each time and none was given for these nine. A bullet written by the slice
  it describes cannot name the sha it lands under, so the number beside it is the count at `4250893` rather than the
  count at the commit carrying the sentence, and it widens by one with every commit after — which is why the query, and
  not the number, is the fact this bullet keeps. When this bullet was written
  the remote was at `cf7e56d`, and the push after it carried `3b7ffb2` and `7544baa` themselves; the remote's own answer,
  not this file's memory, is what says where a ref is. What the `5e59e17..cf7e56d` fast-forward carried is §4.17's
  keystore (`bdd7f1d`),
  §4.18's materializer
  and owner-only write (`be6ce05`) and §4.19's supervisor with its record and its two registry rows (`cf7e56d`) — three
  slices that each waited on this file rather than on a ref of their own, because a push needs its own confirmation each
  time and this file states that rather than assuming it was given. `5e59e17` itself was reached in three pushes after
  `96d1613` — `96d1613..f2a5826` (§4.15's serializer and its
  self-quoting label), `f2a5826..ef9792e` (§4.16's stages 1 to 4 and three goldens, with the bookkeeping that moved the
  queue's tallies for them) and `ef9792e..5e59e17` (the byte-budget test that waits on the wire rather than on the
  scheduler, and the ledger's two new rows and the collision that killed a law) — and `96d1613` was the twelfth of the
  pushes enumerated below, the runner-record commit `f1a3829` and the eight slices
  §4.7 to §4.14 record — five of them code, three found by reading: two before a line of Go was written and one while
  the seventh was still running — among them. Local `master`
  still points at `f38f27b`; every push since has been a fast-forward of `nexora/architecture` onto `nexora/master`,
  so the local branch pointer was never moved.
  Main branch for PRs upstream: `master`.
- Remotes: `origin` = upstream ZeroOmega (**never push**), `nexora` = `git@github.com:k2dms/NEXORA.git`.
- **Committed and pushed** (2026-11-05: `c367db2..431ea0a`, a clean fast-forward to `nexora/master`, no MR needed),
  in the planned groups — (a) infrastructure: `.gitattributes`, `.env.example`, `.gitignore`, `.npmrc`×5, `tools/*`;
  (b) `packages/schema/*` plus the `check-no-secrets.mjs` fixture-path scoping its own negative example required;
  (c) `docs/audit/*`; (d) `docs/adr/*` + `docs/architecture/*` + `docs/README.md`; (e) `docs/verification/*` and
  this file; (f) the two coffeelint demotions, separate so they can be reverted without touching the documents.
- **Twelve more pushes to `nexora/master` the same day, all fast-forwards, each separately confirmed:**
  `431ea0a..f38f27b` (the state record above), `f38f27b..4307ff6` (the workflow, and the queue and the register as
  data, and the end-to-end re-verification), `4307ff6..a7627ae` and `a7627ae..ceedabf` (the two CI fixes),
  `ceedabf..890fde6` and `890fde6..c81ae7e` (the §12 record, with `docs/audit/raw/probe-8-runner-runs.txt`),
  `c81ae7e..f15acb8` (the same record carried to the fifth run: §12's table, the regenerated capture, the run-count
  wording in the docs index, audit docs 00, 02 and 99, architecture docs 20, 21 and 39, the verification README and
  the workflow's own header comment — doc 21's no-protocol-code row moving here, while the queue data's VER-11 subject
  and architecture doc 39's VER-11 row had moved in the push before it), `f15acb8..5dee596` (one clause: which row
  moved in which push, correcting the attribution the message of `f15acb8` made), and `5dee596..75bff0f` (the §12 table
  carried to the seven runs it tabulates: the cancelled `42029092102` and the green `42029386377` as rows, the
  regenerated capture, the finding about this file's own `concurrency` block, and — across audit docs 00, 02 and 99,
  architecture docs 20 and 39, the verification README, the docs index, this file and the workflow's header comment —
  the run-count wording replaced by the shape rule, for the reason in the next bullet), `75bff0f..fe7a3e6` (that rule
  standing in place of the counts, across audit docs 00, 02 and 99, architecture docs 20 and 39, the verification
  README, the docs index, this file and the workflow's header comment, plus the untabulated run recorded in the
  capture), and `fe7a3e6..1004c4e` (§4.6's M1: `connector/internal/{contract,contractgen,envelope,frames}` as a Go
  module, the `connector` job no longer pinned off, and the documents that had to stop asserting this host had no
  Go toolchain, no Go code and no `connector/` tree), and `1004c4e..96d1613` (§4.7 through §4.14: `connector/internal/`
  `{peer,dispatch,hello,envelope}`, then `connector/cmd/nexora-connector`, with the three slices found by reading — the
  `internal/chunk` gate, the contract sentence that named the wrong process, and `ODEC-35` — carried in the same
  push, because a registration found while a slice is in flight has no separate fast-forward of its own). Twelve executions of
  `.github/workflows/verify.yml` came from those pushes — `42015170195` and `42017987418` red, `42020617681`,
  `42025558933`, `42027072602`, `42029386377`, `42033189223`, `42035782395` and `42046629162` green, and `42029092102`
  `cancelled` because it was
  still in flight when the push of `5dee596` landed two minutes into it, and `42077447021` and `42077576648` — the same
  tree judged twice, ~50 s apart, both green, because the second of them came from the ref in the bullet below. §12 tabulates the eight whose shapes it
  records line by line and leaves `42033189223` and `42035782395` out, because each printed a green of the shape §12
  already shows.
  No `--force`, no `--no-verify`, no amend of anything already published: the
  commit that first tripped the label guard stayed on `master` with its label, and the document moved to match the
  guard instead.
- **The ref on the remote this session did not mean to make is deleted.** The first attempt at the
  `1004c4e..96d1613` push used the refspec `nexora/architecture:nexora/master` — the local branch name pasted after a
  colon — so GitHub created `refs/heads/nexora/master` rather than moving `refs/heads/master`. The corrected
  `nexora/architecture:master` fast-forward followed it inside a minute, both refs carried `96d1613`, and neither held a
  commit the other lacked. What it did cost was the duplicate execution named above:
  `.github/workflows/verify.yml` reacts to a push on any ref, so a branch whose name repeats its remote's is a second
  copy of the build. The safety check was the remote's own list, not this file's memory of it: `git ls-remote` printed
  two refs before the delete, `refs/heads/master` at `5e59e17` and the stray at `96d1613` — an ancestor
  of that tip, so nothing hung on it and nothing needed merging. The delete then ran as
  `git push nexora :refs/heads/nexora/master`, the fully spelled ref rather than the `--delete nexora/master` form this
  bullet used to recommend, because a bare `nexora/master` is precisely the ambiguous name that made the stray ref, and
  removing published state should not depend on which ref the resolver happens to pick. `git ls-remote --heads nexora`
  after it prints one ref, and the local tracking ref was pruned rather than left to argue with it.
- **The enumerated list above is authoritative only up to the sha it names, and the reason is worth stating once:**
  every commit that records a run of this workflow is itself a commit that runs it, so no sentence in this repository
  can state a final count of executions and survive its own push. This file's next push is therefore one more
  fast-forward and one more execution, and what decides whether it is a finding is §12's rule — a run enters the table
  when its shape differs — not any count in this file.
- **Every push requires separate explicit confirmation.** No `--force`, no `--no-verify`, no amend of published commits.
  Pushes go one at a time and wait for their own run: `concurrency.cancel-in-progress` in `.github/workflows/verify.yml`
  makes a second push inside one build cancel the first run's verdict, which is how `42029092102` ended.
- Never `git add --renormalize .` (the repo has `core.autocrlf=true` on this host; mass re-normalization is exactly
  the forbidden mass diff). If line endings must change, isolated commit, endings only.
- The deleted dirty work is preserved **outside** the repo at
  `C:\Users\dms\.qoder\tmp\E--NEXORA\discarded-20261104T081418Z\` (patch + `vless-test.coffee`), per the user's
  decision to delete those edits. Affected files were `omega-web/src/omega/app.coffee`,
  `omega-web/src/omega/controllers/profile.coffee`, `omega-web/src/partials/profile.jade`, `omega-web/src/test/`.
- Scratch file `E:\tmp-cmds.txt` (command args/result dump) is mine to delete during cleanup; nothing depends on it.
- Pending user decisions that must not be taken unilaterally: remove `.circleci/config.yml` (references the
  unsupported `circleci/node:7.10` and a `release.zip` the build no longer produces — it can only ever be red);
  add `engines.node`; change the lockfile policy; fix SEC-1 now vs. document it as a Phase-2 requirement.

---

## 8. Decisions already made so they don't get re-argued

1. **Two-process connector** (ADR-0005): thin `nexora-host` (framing + auth + spawn) over a local pipe to
   `nexora-connector`. Reason: the host is killed by the browser when the port closes; Core must not die because an
   options tab reloaded. Containment chain `Xray ⊆ Connector ⊆ host shim ⊆ Service Worker`. Single-process was
   rejected — restarting the host would drop Xray. On Windows, **Job Objects are a requirement, not a nicety**
   (win32 has no parent-death cleanup by default); Linux/macOS `prctl(PR_SET_PDEATHSIG)` / `kqueue`.
2. **Native Messaging only, no loopback HTTP/WS control API** (ADR-0004). A local HTTP control endpoint is reachable
   by any local process and any web page via DNS rebinding/simple CSRF, needs port discovery, and does not authenticate
   the extension. NM gives OS pipes + sender identity for free.
3. **Fail-closed primitive = `chrome.proxy.settings` FixedProfile aimed at a closed loopback port** (ADR-0009).
   `+direct` and `chrome.proxy.settings.clear()` are explicitly rejected as fail-closed mechanisms because both
   *leak* traffic instead of stopping it. Three distinct failure planes: `coreDeath` → block, `serviceTeardown` →
   bounded grace window *only because the connector may come back and re-apply the same config* then hard-cap → block,
   `extensionLifecycle` → restore/reapply, must never depend on the connector being reachable.
4. **`config.apply` vs `core.select` are not the same thing** and the docs must keep them apart:
   `config.apply` is declarative replace-and-reconcile and restarts Core **only** if the compiled config hash changed;
   `core.select` is the imperative fast path that changes the active outbound tag among already-loaded servers, with
   `configHashUnchanged: true` required by the schema, no restart, no config write. `core.start/stop/restart` are
   converge-to-desired-state (`start` on a running Core with the same config is a successful no-op returning the same PID).
5. **Chunked envelope for >1 MB payloads**, not out-of-band file hand-off (`framing.json.oversizedPayloads.decision`),
   because a temp file is not an authenticated channel.
6. **Keystore on Windows = DPAPI-protected file** under `%LOCALAPPDATA%\NEXORA\keystore/` (ADR-0011). No UI prompt,
   no daemon dependency; residual risk stated: DPAPI is user-scope, not process-scope. OS credential store deferred.
7. **Omega `Conditions` stays the outbound-selection mechanism and is *compiled* into Xray routing where
   representable — not replaced** (ADR-0013). The compiler must report unrepresentable rules instead of dropping them.
8. **UI: Svelte 5 islands/custom elements, no SvelteKit** (ADR-0016). Decided on CSS-scope grounds (legacy page drags
   Bootstrap 3 + 166 base16 themes + arbitrary `-customCss`), MV3 popup cold-start direction (popup already carries
   jQuery+Angular through `$script`), and reversibility (framework sits above `packages/schema` + `packages/state`,
   so a switch touches only `nexora/extension/src/ui/**`). React recorded as the considered alternative.
   Hard rule: **never mount a Svelte root inside AngularJS DOM** — separate documents, shared state, not shared runtime.
   Cut-over is defined by the parity matrix, not a date; popup flips last.
9. **i18n single source = `omega-locales/*.po` compiled twice** (ADR-0017): `_locales` through the existing
   `po2crx`, UI catalog through `packages/i18n`. Whether the 20 unsupplied locales get closed is open.
10. **Xray is an integrity-pinned downloadable asset** (ADR-0019): fetched at first run, sha256 **and** signature
    verified, mismatch = refuse to start. No silent self-update; user-initiated re-verify with the previous version
    retained for rollback. Never bundled in the extension archive (store policy + size + independent Core upgrades).
11. **Test strategy** (ADR-0020): schema validity + golden configs + env-gated live matrix, and no test may assert
    "connection succeeded" without proving egress went through Core (`LIVE-04`: egress IP must equal the VPS IP).
12. **Three of the six §14 UI sections ship without the Connector** — Subscriptions, Routing (browser plane),
    Settings (existing keys). Dashboard, Servers and the connector half of Diagnostics are meaningless before
    Phase 2. Do not promise all six in one UI iteration; that promise is stubs.
13. **The codec answers "is that a frame", and only the front door answers "is that a method"** (§4.10, which is
    §9's former ninth question resolved by data rather than by a person): `connector/internal/envelope/` does not
    consult the projection for allowlist membership, because
    `packages/schema/protocol/envelope.schema.json#/properties/method` declares a pattern, ADR-0007 rule 2 puts the
    refusal in *resolution*, and a codec that refuses first turns an unpublished name into a transport teardown
    through `connector/internal/peer/`. Do not re-add the check "for safety": the gate is
    `connector/internal/dispatch/`, `New` already refuses a handler for a name the allowlist does not publish, and
    §4.10 names the composed test that shows the corpus's own two negatives still get answered.
14. **A green check over an unspecified body is not a spec** (§4.11). `connector/internal/chunk` stays unwritable until
    §9's ninth row is answered: do not invent a fragment field, a subset rule, or a canonical serialization to make the
    chunk block "work end to end". `packages/schema/scripts/verify.mjs` asserts everything the contract says about
    slices, and the value of that green run is precisely that it does not assert where a slice's bytes ride. When an
    answer is chosen, `VER-76` and `ODEC-34` are deleted in the same commit that adds the field, its examples, and the
    assertion named in the row's `closesBy` — not marked closed.
15. **A contract sentence and a green test can disagree in plain sight, and the test is not automatically the one that
    is wrong** (§4.12). When one field of `packages/schema` attributes an act to a process that another field of the
    same file, another section of the document that quotes it, and the code all place elsewhere, the prose is the
    defect: repair the field, then the quotation, then the comment that had been quoting it in halves. Do not add a
    `VER-NN` row for it — the register has no question left once the field names the process that holds the
    information — and do not let a comment survive by quoting the half of a contract that suits it.
16. **A process answers with the data it holds and refuses to start without the data it does not** (§4.13).
    `connector/cmd/nexora-connector/` registers the two published methods whose required result fields it actually
    possesses — the negotiation and the two version strings — and leaves every other published name unregistered so the
    front door answers it `Internal`, which is the code for "this build has no handler", not a placeholder for a guess.
    The same rule governs the two inputs it cannot measure for itself: `--sender-id` has no default and the process exits
    rather than serving a frame whose sender nobody resolved, and `--connector-version` falls back to Go's stamped
    module version but treats a local `(devel)` build as no version at all, because a product version a binary invented
    for itself is the field a user would later trust. A build-info string is not a release. When the real producer of
    either value lands — the host for the identity, the release tooling for the four optional version fields — the flag
    disappears rather than becoming a default.
17. **A claim that nothing tests a rule is a claim about the tree, and the tree answers it** (§4.14). A reading of the
    rate-budget contract arrived phrased as though no test existed for who counts a budget or whether it is per sender,
    and proposed a register entry and a queue row for the question. `TestABudgetIsPerSenderAndNotPerProcess` in
    `connector/internal/dispatch/dispatch_test.go:245` spends one sender's budget, refuses that sender's second call,
    admits a second sender's first, and asserts the handler ran exactly twice — while quoting the contract field that
    scopes the budget in its own comment. The entry was refused. This is not a courtesy check on a draft: every
    assertion in `packages/schema/scripts/verify.mjs` counts register entries and would have agreed with a register that
    holds a question this repository has already answered, which is the defect doc 40 §3 names a question the register
    invented. The check that catches it is the one that reads the named test before the row is written.
18. **A check's message describes its intent, not its behaviour; read the code before trusting the sentence** (§4.14).
    The assertion that pins the queue's owner distribution says in its own message that it is checking what the
    inventory document reads aloud, and it never opens that document — it compares the data to a constant. Three of
    `docs/architecture/39-verification-matrix.md`'s numbers sat one behind the data at `4997081` under a green build
    that had been printing that message the whole time. So: when a stale figure survives a check whose label claims to
    catch stale figures, read the check itself before concluding the harness is wrong or the document is; both can be
    right about different things. And when a §4.12-style repair of a *sentence* is queued as an assertion, name which
    file the assertion will read, because an assertion that reads the data cannot protect a document from itself.
19. **A citation that resolves is not a citation that points at the claim** (§4.15). The row in doc 33's index that
    cites "the compiler's dependency on install state" carried, for two slices, a line number into doc 27 that landed on
    that document's `See also` footer — a real file, a non-blank line, and not the sentence. The guard that owns
    citations asserts resolution, not relevance, and a wrong number that hits any text is indistinguishable from a right
    one. So: when a document grows, re-read the *description beside each number* in the documents that cite it, not only
    the numbers, because a re-numbered citation moves no counter and no build output. The residual this leaves is honest
    and worth naming: a guard could compare a phrase from the citing description against the cited line, and nothing here
    does, which means the corpus has a class of defect that only the person editing catches — §4.11's finding one level
    deeper, and the reason a re-read is recorded as work rather than skipped as a formality.
20. **A deferral is stated, not quoted** (§4.15). Two sentences that explained why an existing row reads NO spelled the
    label inline, and the marker the queue declares in its data matches a line rather than a role, so
    the partition billed the corpus two extra deferred claims and named them unowned. The two answers were a second anchor on
    the same row, or prose that says *the row reads NO*. The first was refused: one obligation owned twice inflates what
    Phase 2 is billed for, which is the number this whole mechanism exists to keep honest. So the rule is the second —
    quote a label only on a line a row already owns — and the general shape behind it is worth the sentence: the corpus's
    deferral total is a property of its prose style, not only of its claims.

---

## 9. Open questions that genuinely need a human

| # | Question | Blocks |
|---|---|---|
| 1 | Fix SEC-1 (`transformValueKeys` + `headers`) immediately after baseline, or hold it as a documented Phase-2 requirement until a test exists? | Phase 2 first PR |
| 2 | Lockfile policy: keep `package-lock.json` ignored or add `!packages/**/package-lock.json`? What the 2026-11-05 runs changed is the framing: a lockfile-free install now *measurably* builds, on three clean images, and what stays unverified is that it resolves the same set somewhere else (`docs/audit/99-baseline-report.md` §12) | CI as a trust anchor |
| 3 | `.circleci/config.yml`: delete in a separate commit or keep and mark dead? | cosmetic, but CI-red |
| 4 | Chunking vs out-of-band for >1 MB is decided (chunking) — but does `subscription.refresh` deliver servers to the extension through chunked events, or does the Connector own subscriptions entirely? | `25-…`, `29-…`, data split |
| 5 | inbound-per-tag vs single-inbound-with-routing (one Xray inbound per profile vs one with `routing` and `core.select`) | `27-…`, `core.select` semantics |
| 6 | Do we close the 20 unsupplied locales, or scope to 7 and say so publicly? | ADR-0017 |
| 7 | Offline installs: pinned-Xray means a first-run download, which breaks air-gapped setup. Accept as a product limitation or ship a documented manual-pin path (`installChannel: manual` exists in `connector-settings.schema.json`)? | `33-…` |
| 8 | What carries the heartbeat? `packages/schema/protocol/framing.json#/liveness` gives the interval (25 s), the miss count (3) and a **direction** (`host-to-extension`), and `packages/schema/protocol/envelope.schema.json#/properties/type` has four values — `request`, `response`, `event`, `progress` — none of them a beat. So either the beat is an envelope frame the data has not named (which one, given the closed object and the four type rules), or `nexora-host` keeps the port open without writing and only the extension counts, or the beat belongs to the local pipe instead and the direction value is wrong. Until one of those is chosen, no Go peer can claim the third clause of `VER-49`'s closer, and the peer in §4.7 counts *any* inbound frame as a beat arrived — a proxy, not the contract | `VER-49`, §11 of `docs/architecture/25-native-messaging-protocol.md`, the watchdog in `connector/internal/peer/` |
| 9 | What are the bytes of a chunk slice, and which byte string does `chunk.contentHash` hash? §4.11 read the contract to write `connector/internal/chunk` and found the metadata complete and the body absent: the envelope's `chunk` object is closed at four fields, `profile.import`'s only list fields are arrays of whole documents, and the recorded group declares `encoding: "base64"` on two frames that hold no bytes. The data does exclude one reading — announcements-only, because that leaves the whole document in one frame over §4's sender cap — and does not choose between the two survivors: a fragment field plus a hash over the concatenation, or per-slice valid subsets plus a hash over a canonical serialization of the union that nothing in this repository defines. Whichever is chosen, `#/oversizedPayloads/mechanism/atomicity` and the gap rule start meaning a specific byte string instead of an intended one | `VER-76`, `ODEC-34`, `connector/internal/chunk`, §5 of `docs/architecture/25-native-messaging-protocol.md` |
**This list ran to nine rows, dropped to eight, and is at nine again.** It dropped when §4.10 deleted the ninth it had
— whether a name the allowlist does not publish is refused by the codec or by the front door was raised here as a
choice between two defensible readings and turned out not to be one: `docs/adr/0007-typed-versioned-command-allowlist.md`
rule 2 says an unknown method MUST be *answered*, `packages/schema/protocol/envelope.schema.json#/properties/method`
declares a pattern and no membership rule, and `packages/schema/scripts/verify.mjs` replays the two frames in question
and requires the answer the corpus records. The codec was the half out of step with the contract it mirrors. A question
the corpus already answers does not belong in a list of questions that need a human, so the row is deleted rather than
marked answered, for the same reason §5 deletes a queue row that closed; §4.10 carries the change and §8's thirteenth
entry carries the position. The ninth row that replaced it came from the opposite situation: §4.11 read the chunk
contract, found an answer that was *not* in it, and refused to supply one. It is a `contract` question — the two
survivors cost a field and its examples, not a person — and it is in this list because the next agent to touch
`connector/internal/chunk` would otherwise meet a green chunk block and an unwritable package, and the cheapest answer
available to them would be a guess in a commit message. What remains is nine questions, and two of them have Go waiting:
the eighth, `VER-49`'s heartbeat clause, which §4.7 declined to fake, and the ninth, which stopped §4.11 before a line
of §6's queue was written.

---

## 10. Files that must stay as they are (cited, never edited)

| File | Why untouched |
|---|---|
| `omega-target-chromium-extension/src/coffee/background.coffee:32-47` | the wipe trap — cited by ADR-0015; "fixing" it silently changes upgrade behaviour for existing users |
| `omega-target/src/options.coffee:27` | SEC-1 — a Phase-2 change, needs a test first |
| `omega-pac/src/profiles.coffee:111-118` | the `+`-prefix filter every NEXORA key design depends on |
| `omega-pac/test/conditions.coffee:468` | local-time parse behind the TZ fragility; changing it changes test semantics |
| `.circleci/config.yml` | dead, pending decision §9.3 |

---

*End of handoff log. The remaining work order is §6's, and §4.20 closes the item §4.19 left first in it: the command
surface that makes doc 27 §2's stages, `connector/internal/apply`'s write and `connector/internal/supervisor`'s process
one path rather than three tested neighbours is written, sixteen tests deep, and answers a frame with a config file, a
hash and a different Core. All four halves the compiled config was waiting for — the store, the writer, the supervisor
that runs what it writes, and the surface that calls all three — now exist and are tested. The Connector queue now runs:
`connector/cmd/nexora-host`, the process `VER-14`'s containment chain needs above
Core — then stage 7's probe, which needs a binary
`docs/adr/0013-omega-conditions-compiles-into-xray-routing.md` §Evidence records as absent from this host. Stages 1 to 4,
the document half of 5, the serializer of §4.15, three of doc 27 §3's six golden configs (§4.16), the store that holds
every credential a config points at (§4.17), the materialize-and-rename that turns one into a file Core can read
(§4.18), the supervisor that spawns that file's process (§4.19) and the `config.apply` frame that drives them in order
(§4.20) are written; `VER-28` and `VER-78` describe three fixtures rather than none and
stay open for the two reasons §4.16 names; `VER-23`, `VER-29`, `VER-14` and now `VER-32` have moved from `no-code` to
`no-run` because the round trip, the rollback, the parent-death window and the unrepresentable-rule report are measured
while the session around them is not — and `VER-80` is the row that replaced one: the surface exists, and no `main.go`
registers it, because the intent seam and the two directories are `ODEC-39`. `internal/chunk` and
`cmd/nexora-host` stay where they were: the first is stopped by `ODEC-34`, and the second by `ODEC-35` — a decision with
three surviving answers, which is the first time this queue has been blocked by an ADR rather than by a gap in the data.
`ODEC-36` is the third stopped thing and the cheapest, with one third of its reason now gone: the mechanism that reads an
object handle out of the store exists, and what is owed is the sentence that says whether the map it returns belongs to
the hashed bytes or only to the file — a sentence §4.20's `hashSpan` refuses to write for it, and instead measures: under
the written-bytes span even a dryRun has to open the keystore. A fourth sits beside it — `ODEC-37`, the four byte
positions the corpus names and never spells, which `§4.16`'s goldens found by refusing to guess and which only Xray's own
accepted config can settle. A fifth is `ODEC-38`, over what span the ladder's restart attempts are spent, which §4.19's
own tests found by being written — a Core that dies again straight after a successful restart is handed the budget again,
forever, and no file in this corpus names a window or a count that would make the second death a new episode. A sixth is
new: `ODEC-39`, where the intent documents an ids-only `config.apply` cannot carry come from, which two directories the
surface is handed, and what the compiled config file is called — found the same way, by writing a seam that three of the
register's own rows needed named. The queue's cheap tail is closed: §4.21 writes the three items §6 had queued beside
M2, so nothing that remains is a string and a check — every surviving item is stopped by a decision above, by the Xray
binary this host does not have, or by a browser. §4.22 closes the queue's other tail, the one it had carried since the
citation convention was written: `docs/` no longer addresses `packages/schema/scripts/verify.mjs` by line number at all,
so a rule added in the middle of the harness can move a sentence but not a citation. A seventh stopped thing is
`ODEC-40`, and it is the first that arrived without any code being written at all: doc 29 §5 promises that the format an
`auto` parse resolved to is recorded, and the refresh contract carries eight result fields, none of them a format, plus
an args description that refuses to write any of them back. That entry is what makes the claim two sentences above true
rather than nearly true — until §4.23, subscriptions sat in this queue as the one component nobody had read, and the
sentence "nothing that remains is a string and a check" was spoken about a part of the system it did not cover.
Plus the decisions in §7 and the nine questions in §9, which are not work an agent may finish; the note under that table
says why one ninth stopped being a question and what took its place.
Everything in §1, §2, §5 was measured, not inferred — re-run the
probes before contradicting it.*
