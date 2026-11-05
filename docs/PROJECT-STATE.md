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
  of a file in the tree, a `path#/json/pointer` lands inside a JSON document that parses, and a document or gate-script
  name that a document tells the reader to run or read exists (or is in `PLANNED_DOCS`, which is empty). A line cite
  into a file rewritten in place — `MUTABLE_CITATION_TARGETS`, today `docs/PROJECT-STATE.md` — is rejected.
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
The queue is `202` labelled lines over `44` documents partitioned into `75` rows `VER-01…VER-75`, `26` vocabulary
sentences and `24` live rows inherited by id from `docs/verification/live-matrix.md`. The register is `53` deferral
sites over `13` documents partitioned into `33` entries `ODEC-01…ODEC-33`, each naming who can answer it
(`product` 14, `contract` 11, `adr` 5, `measurement` 2, `upstream` 1), three options, and the `VER`/`COMP` rows it
blocks. Anchors are verbatim substrings, never line numbers; the harness re-reads `docs/**` on every run and fails if
a claim or a deferral is unowned, owned twice, or if a document reads a distribution the data does not have.

`npm --prefix packages/schema run verify` → **green: 1217 checks, 13 schemas, 19 command docs,
20 valid examples, 24 invalid rejected, 112 frames replayed**. The only note printed is `compiled 13 schemas`;
every `DEFERRED:` note the earlier drafts printed is gone: `26-command-surface.md`,
`37-ui-architecture.md`, `38-compatibility-matrix.md`, `39-verification-matrix.md` and
`40-open-decisions.md` exist now, so their cross-checks are real assertions.
`node tools/check-doc-evidence.mjs` → **green: 2260 `path:line` citations, 487 `path#pointer` citations,
1290 doc-and-script names** (the `path:line` figure fell by 21 when every cite into this file became a section
reference, and by 3 more when probe-4 turned the same rule on for `docs/audit/99-baseline-report.md`, which every
probe round appends to; it rose by 8 again when the runner record cited the legacy build configs it reads, and the
third figure by 34 for the same reason, and by two more in this sitting — its VER-11 row in
`docs/architecture/21-system-context.md` now names that runner record, and this file's sentence about the count now
names that document. It rose by one more in the pass that recorded the seventh and eighth runner runs: that pass
leaves one more named reference to the baseline report in `docs/` than it removes.
It had also risen earlier, when `DOCNAME_RE` began checking that a `tools/*.mjs` name a document tells the
reader to run is a file that exists), and `PLANNED_DOCS` is empty — no document is cited anywhere in `docs/` that does
not exist.
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
`if: ${{ false }}`), a three-row matrix, and the `VER-02`/`03`/`04`/`06`/`08`/`10`/`11` guards as executable steps —
and it ran there the same day: `docs/audit/99-baseline-report.md` §12 tabulates seven of those runs — twice red, four
times green and once cancelled by the file's own
`concurrency` block — under the rule that a further run joins the table only when its shape differs, a rule this
iteration had to write because every commit that records a run of that workflow is itself a commit that runs it. Each
of the two failures was a defect in the workflow
itself rather than in the tree, all of them recorded line by line in `docs/audit/99-baseline-report.md` §12. Before
those runs every `run:` block had been executed on this host and proved able to fail against a synthetic violation,
which is a claim about the shell logic; the runs are what made it a claim about CI
(`docs/audit/02-toolchain-decay-and-risk.md` §7.3). `tools/check-legacy-backup.mjs` — the command `LIVE-14`'s compile half cites — is on disk beside the other
three gate scripts.
`docs/architecture/39-verification-matrix.md`, `docs/architecture/40-open-decisions.md`, `packages/schema/verification.json` and
`packages/schema/open-decisions.json` are **on disk** now, with the harness
cross-checks that used to print `DEFERRED:` notes running as real assertions, and `docs/verification/README.md`
landed with them so `PLANNED_DOCS` in `tools/check-doc-evidence.mjs` is empty.
No `packages/{protocol,state,i18n,ui}`, no `connector/` content, no `nexora/extension` — all Phase 2+.

---

## 5. Verification state to the letter

- `cd omega-build && npm run deps && npm run dev && npx grunt && npx grunt release` → **exit 0** for the whole chain (Ladder A).
  Re-run end to end on 2026-11-05 by probe-4 — same four steps, same order, all exit 0, and its `npm run deps`
  reused this host's `node_modules` (five `up to date` lines), so it re-verified the chain rather than resolution.
- Artifact assertions: 353 files / 5 999 353 B / 252 `lib/` entries / 7 `_locales`, and `js/omega_pac.min.js` present at 742 272 B —
  every one of them reproduced from the 2026-11-05 rebuild, top-level path set included. On a clean image the *shape*
  figures reproduce and the *byte* figures differ from this host's, then agree with each other: fifteen runner
  builds — the three images of each of the five runs §12 tabulates whose legacy chain got that far — gave 353 entries, 252 `lib/`,
  7 `_locales` and the bundle, at 5 974 389 B / 5 974 444 B (ubuntu) and 5 974 401 B / 5 974 456 B (windows) with a
  735 547 B bundle, the same four totals in every one of those runs — so the totals and the bundle size are properties of a CRLF
  checkout against this host, and of the runner image against a different image, which is why the workflow
  prints them as NOTEs (`docs/audit/99-baseline-report.md` §12). What no two builds ever shared is the hash: thirty
  distinct sha256 values across those fifteen builds, none of them the two recorded below — and every further run of
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
- `node tools/check-no-secrets.mjs --selftest` → 13/13 green.
- `npm --prefix packages/schema run verify` → **green** (1217 checks; see §4.4).
- `node tools/check-doc-evidence.mjs` → **green** (2260 `path:line`, 487 `path#pointer`, 1290 doc-and-script
  names; `PLANNED_DOCS` empty; §4.4 has the same figures and the same provenance for each change in them. These are not
  local to this host: `42025558933` and `42027072602` printed exactly `2260 / 487 / 1287`, `42029092102`,
  `42029386377` and `42033189223` printed `2260 / 487 / 1289`, and the three earliest executions printed `2252 / 487 / 1253` for the corpus
  as it stood at `ceedabf` — `docs/audit/99-baseline-report.md` §12. Any commit that moves a citation moves all three, so they are read as a
  set and never pinned.)
  `node tools/check-legacy-backup.mjs` → **green** (`--selftest` 4/4).
  `node tools/verify-vendor.mjs` runs against the
  working tree.
- `.github/workflows/verify.yml` → **written, step-verified here, and run on GitHub runners since 2026-11-05 —
  §12 tabulates seven of those runs: twice red, four times green, once cancelled** (`docs/audit/99-baseline-report.md` §12). What the green runs answered is the question
  §4.5 and `docs/audit/02-toolchain-decay-and-risk.md` §7.3 used to leave open — a clean image can install and build
  this tree: all five roots from an empty `node_modules`, no lockfile in git, both pinned test floors reproduced,
  every artifact content figure reproduced, and the 252 `lib/` entries produced without this host's Bower cache
  (probe-5 had already removed the transport half of that question: a cold `bower install` of the two `git://` deps
  succeeds **without** any rewrite, because bower normalises them to `https://github.com/…\.git` before git is
  involved, while `git ls-remote git://…` from a shell still times out). The sixth run's cancellation is a fact about
  this file's `concurrency` block, not about the tree: it cancelled two cells that had already printed every guard and
  uploaded both artifacts, and it is why a push here now waits for its run instead of stacking behind the last one.
  What it did not do is move a `VER-NN` row:
  `packages/schema/verification.json` has no state meaning closed, so a met row is deleted together with the labelled
  line that anchors it, and that is the first mechanical commit of Phase 2, not a status edit in this file.

---

## 6. The exact next steps (task #6 → #11)

**#5 `packages/schema` — done.** It is now the green, re-runnable test of this iteration:
`npm --prefix packages/schema run verify` → `OK: 13 schemas compile, 19 command docs conform, 20 examples
valid, 24 invalid examples rejected, 112 protocol frames replayed.` (1217 checks; details and the 8 contract
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
record shape ADR-0001 rules 1-3 require are two steps of `.github/workflows/verify.yml` —
"The ADR index lists exactly the ADR files on disk" and "Each ADR has the shape ADR-0001 defines" — which print
`OK: the index matches 20 ADR files, exactly.` and `OK: 20 ADR records each carry their number, …` when run here, and
printed the same two lines on each of the three runner images on 2026-11-05
(`docs/audit/99-baseline-report.md` §12). `docs/adr/0001-adr-practice.md:99` still calls them planned; that line is now
`docs/architecture/39-verification-matrix.md` §13's item 6, not a gap.

**#8 `docs/architecture/20-40` — done: 20 to 40 are on disk.** `26-command-surface.md` names **exactly** the
19 allowlist methods and the cross-check is a live assertion, not the `DEFERRED:` note the earlier draft
described. Document rules: normative `MUST/MUST NOT/
SHOULD/MAY`, `Status:` header, `Trace: ADR-…; Contract: packages/schema/…` footer, and the iron rule that no
document asserts behaviour nothing checks — otherwise it gets a `39-verification-matrix.md` row with
`Verified: NO — deferred to Phase 2`.

`39-verification-matrix.md` + `packages/schema/verification.json` are that rule's endgame: the corpus's `202`
deferral labels over `44` documents are partitioned, on every run, into `75` rows, `26` sentences that define the
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

**Still owed in the corpus, deliberately not done here:** (a) `109 frames` / `504 checks` / `707 checks` remain in
five ADR `## Verification` sections — ADR-0001 rule 5 forbids correcting history in place, and
`docs/adr/0012-nexora-keys-under-dash-prefix.md:106` shows the intended form ("green at the time of writing");
`docs/adr/0020-test-strategy.md:86` likewise calls doc 39
"not yet written", which is recorded as stale-by-design in doc 39 §13. (b) `49` citations across `13` documents still
point at `packages/schema/scripts/verify.mjs` *line numbers* although the "Conventions these documents follow" list in
`docs/README.md` says a mutable tool file is cited by its constant or assertion label; they resolve today because every
new block was appended at the end, and the first block added in the middle will break them — convert them in one commit,
not in passing, and add the file to `MUTABLE_CITATION_TARGETS` in the same commit so the rule is enforced rather than
recommended.

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
happened since, and `docs/audit/99-baseline-report.md` §12 carries the measured output of the seven runs it
tabulates, with the raw capture of those seven — job
conclusions, every `##[error]` annotation, and each quoted line beside the number of times the runner printed it — at
`docs/audit/raw/probe-8-runner-runs.txt`. That is the raw record this file's own convention asks for. #10 is therefore
no longer "written and locally step-verified"; it is written, run, twice failing, four times green and once cancelled —
and §12 states the rule that keeps that sentence true under the runs still to come. Two items take its
place, and neither is a status edit:

1. **The row bookkeeping.** `VER-06`, `VER-08`, `VER-10` and `VER-11` now meet the condition their records state — a
   green run of that file — and the queue has no state meaning closed, so they close by deletion: the row out of
   `packages/schema/verification.json`, its labelled sentence out of every document that carries it, `marker.counted`
   re-derived, and the hard-coded state distribution in `packages/schema/scripts/verify.mjs` amended in the same
   commit. `VER-02`/`03` stay (they scanned roots that hold no code yet), and `VER-04`/`05` stay queued against a
   `check-layout-guards` tool that has not been written.
2. **The browser half.** `LIVE-01…24`, unchanged and `SKIPPED`, need a person with a real Chrome, and `LIVE-14`
   needs the export that would replace the fixtures' exporter-shaped values.

---

## 7. Git rules of this session (do not improvise)

- Branch: **`nexora/architecture`**, now equal to local and remote `master`. Main branch for PRs upstream: `master`.
- Remotes: `origin` = upstream ZeroOmega (**never push**), `nexora` = `git@github.com:k2dms/NEXORA.git`.
- **Committed and pushed** (2026-11-05: `c367db2..431ea0a`, a clean fast-forward to `nexora/master`, no MR needed),
  in the planned groups — (a) infrastructure: `.gitattributes`, `.env.example`, `.gitignore`, `.npmrc`×5, `tools/*`;
  (b) `packages/schema/*` plus the `check-no-secrets.mjs` fixture-path scoping its own negative example required;
  (c) `docs/audit/*`; (d) `docs/adr/*` + `docs/architecture/*` + `docs/README.md`; (e) `docs/verification/*` and
  this file; (f) the two coffeelint demotions, separate so they can be reverted without touching the documents.
- **Nine more pushes to `nexora/master` the same day, all fast-forwards, each separately confirmed:**
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
  the run-count wording replaced by the shape rule, for the reason in the next bullet). Eight executions of
  `.github/workflows/verify.yml` came from those pushes — `42015170195` and `42017987418` red, `42020617681`,
  `42025558933`, `42027072602`, `42029386377` and `42033189223` green, and `42029092102` `cancelled` because it was
  still in flight when the push of `5dee596` landed two minutes into it. §12 tabulates the seven whose shapes it
  records line by line and leaves the eighth out, because `42033189223` printed a green of the shape §12 already shows.
  No `--force`, no `--no-verify`, no amend of anything already published: the
  commit that first tripped the label guard stayed on `master` with its label, and the document moved to match the
  guard instead.
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

*End of handoff log. The remaining work order is §6 task #11 — the end-to-end pass no document in this corpus can do
for itself — plus the decisions in §7 and the seven questions in §9, which are not work an agent may finish. §4.5 is
the inventory that says why nothing else is owed. Everything in §1, §2, §5 was measured, not inferred — re-run the
probes before contradicting it.*
