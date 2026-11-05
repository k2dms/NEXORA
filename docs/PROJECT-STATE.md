# NEXORA — project state and continuation log

> **Purpose of this file.** It is the handoff record for an interrupted engineering session. If you
> (a human, or an agent in another IDE such as Cursor) pick this repo up cold, read this file first:
> it contains the agreed scope, every decision already taken and *why*, the measured facts that
> falsified the original plan, the exact inventory of what exists on disk, and the precise next steps.
> Written 2026-11-04. English because it is the repository language.

Status of this branch: **Phase 0 (audit) + Phase 1 (architecture) in progress. No product code is
written yet, and none should be in this iteration.** Nothing on this branch is committed yet.

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
| 2 | Bower is dead; must vendor ~25 components into `omega-web/lib/` (multi-MB into git) | **`bower install` works on this host** (both `git://` deps resolve) | **Do not vendor.** `deps` and `grunt/aliases.coffee` stay untouched; `tools/verify-vendor.mjs` is the guard instead. Reproducibility on a clean runner stays **UNVERIFIED** and is recorded as such in `docs/audit/02` |
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

## 4. On disk right now (inventory, all uncommitted)

### 4.1 Infrastructure — done

- `.gitattributes` (new): binary asset declarations, `eol=lf` for `*.sh *.ps1 Gruntfile.coffee **/*.coffee **/*.jade **/*.less **/*.json **/*.md` and future `connector/**`.
- `.env.example` (new): every live-matrix variable, empty values, each line comments which `LIVE-nn` row it gates.
- `.npmrc` × 5 (new, one line each: `legacy-peer-deps=true`) in `omega-build`, `omega-pac`, `omega-target`, `omega-web`, `omega-target-chromium-extension`.
- `.gitignore`: appended `.env`, `.env.*`, `!.env.example`.
- `tools/verify-vendor.mjs` — collects every `lib/...` reference from `omega-web/src/**/*.jade`, `src/popup/**/*.html`, `src/coffee/*.coffee` and `omega-target-chromium-extension/overlay/*.js`, resolves each against `lib/` **or** `build/lib/`, exits non-zero on a miss.
- `tools/check-no-secrets.mjs` — secret scanner with vendored-path skip, identifier-vs-literal discrimination and a `credentialShaped()` entropy gate; `--selftest` runs 13 cases and is green.
- `tools/check-doc-evidence.mjs` — asserts every `path:line` citation in `docs/**` exists in HEAD and contains the quoted token.

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

`npm --prefix packages/schema run verify` → **green: 1184 checks, 13 schemas, 19 command docs,
20 valid examples, 24 invalid rejected, 112 frames replayed**. The only note printed is `compiled 13 schemas`;
the `DEFERRED:` notes the earlier drafts printed are gone: `26-command-surface.md`,
`37-ui-architecture.md` and `38-compatibility-matrix.md`
exist now, so their cross-checks are real assertions. `node tools/check-doc-evidence.mjs` → **green: 2261
`path:line` citations, 458 `path#pointer` citations, 1003 doc-name references**, with three still-planned
documents listed on every run (`39-verification-matrix.md`, `40-open-decisions.md`, `docs/verification/README.md`).

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

### 4.5 What is NOT on disk yet (the remaining plan deliverables)

`docs/architecture/39-verification-matrix.md`, `docs/architecture/40-open-decisions.md`,
`docs/verification/README.md` (these three are the exact list `tools/check-doc-evidence.mjs` prints as
"planned, not yet written" on every run), and `.github/workflows/verify.yml`.
No `packages/{protocol,state,i18n,ui}`, no `connector/` content, no `nexora/extension` — all Phase 2+.

---

## 5. Verification state to the letter

- `cd omega-build && npm run deps && npm run dev && npx grunt && npx grunt release` → **exit 0** for the whole chain (Ladder A).
- Artifact assertions: 353 files / 5 999 353 B / 252 `lib/` entries / 7 `_locales`, and `js/omega_pac.min.js` present at 742 272 B.
- `sha256 dist/chromium-release.zip` = `b93d5100e4c05ccf7af365dfc1742344360579f482df72e4e9ce00f5ba964ba8`
- `sha256 dist/firefox-release.zip` = `b2309ce3bd0c2be246f0c228ffde1290fa1756c10ad93d90f17380249b4a728f`
  (Both are machine-local build hashes — they will differ on another machine because there is no lockfile.
  They are recorded as evidence that the artifact *content* assertion, not the exit code, is the gate.)
- Test floor: `omega-pac` **142 passing / 1 failing**; `omega-target` **8 passing / 5 failing**; `omega-web` and
  `omega-target-chromium-extension` **0 test files** (there `grunt test` succeeds vacuously — no `test/` directory
  exists, and no npm `test` script is declared; `verify.yml` must print `NOTE: <project> has 0 test files`).
- `node tools/check-no-secrets.mjs --selftest` → 13/13 green.
- `npm --prefix packages/schema run verify` → **green** (1184 checks; see §4.4).
- `node tools/check-doc-evidence.mjs` → **green** (2261 `path:line`, 458 `path#pointer`, 1003 doc-name
  references; three planned documents named on every run). `node tools/verify-vendor.mjs` runs against the
  working tree.

---

## 6. The exact next steps (task #6 → #11)

**#5 `packages/schema` — done.** It is now the green, re-runnable test of this iteration:
`npm --prefix packages/schema run verify` → `OK: 13 schemas compile, 19 command docs conform, 20 examples
valid, 24 invalid examples rejected, 112 protocol frames replayed.` (1184 checks; details and the 8 contract
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
Titles are in plan §3. `docs/adr/README.md` index matches the files; the machine check of that match belongs to
`verify.yml` (#10), which is still unwritten.

**#8 `docs/architecture/20-40` — 20 to 38 done; 39 and 40 remain.** `26-command-surface.md` names **exactly** the
19 allowlist methods and the cross-check is a live assertion, not the `DEFERRED:` note the earlier draft
described. Document rules: normative `MUST/MUST NOT/
SHOULD/MAY`, `Status:` header, `Trace: ADR-…; Contract: packages/schema/…` footer, and the iron rule that no
document asserts behaviour nothing checks — otherwise it gets a `39-verification-matrix.md` row with
`Verified: NO — deferred to Phase 2`.

**#9 `docs/verification/*` — four of five on disk.** `live-env-contract.md` (missing env var ⇒ `SKIPPED`, **never** `PASSED`),
the `LIVE-01…24` matrix, `backup-from-3.5.2.json` and `backup-3.5.2-all-profile-types.json` — the two real `.bak`
files produced by today's `io.jade` export, committed after secret scrubbing — exist.
Only `docs/verification/README.md` is missing, and it is named as planned by `tools/check-doc-evidence.mjs`.

**#10 Baseline fixes** — `.github/workflows/verify.yml`: push/PR on `master` and `docs/**`; matrix
ubuntu-latest (Node 22, parity with `.github/workflows/release.yml:21`) + windows-latest (Node 22 and Node 24, the latter
`continue-on-error: true`); steps install×5 → `npm run dev` → `grunt` → `npm test` per project → `grunt release`
→ assert `dist/*.zip` **content** (353-file/`omega_pac.min.js`-present style checks, not just existence) →
checksum → `verify-vendor.mjs` → `npm --prefix packages/schema run verify` → `check-no-secrets.mjs` → upload
artifacts (never a release). Parses `passing N` and fails below the pinned floor; prints
`NOTE: <project> has 0 test files`. Deliberately absent: `web-ext lint`, Go build/vet (`if: ${{ false }}` with a
"enable in Phase 2" note), the live matrix. Also decide whether the `omega-pac` `TZ=` fix is still needed —
`omega-pac/Gruntfile.coffee` wrapper `process.env.TZ ?= 'Europe/London'` + dropping the POSIX prefix from the
`test` script; the underlying fragility is `omega-pac/test/conditions.coffee:468` building
`new Date("01 Feb 2016 #{time}")` for `TimeCondition`/`WeekdayCondition` under lolex, and the honest fix (explicit
UTC in the test) is deliberately **not** done because it changes test semantics — record it in `02-…` instead.

**#11 End-to-end verification and report** — see plan §6 last paragraph: rebuild, load `dist/chromium-release.zip`
unpacked in Chrome, no errors on `chrome://extensions`, open options and walk all 6 states, import
`docs/verification/backup-from-3.5.2.json`, switch profile and confirm via `chrome.proxy.settings.get`, export and
diff against the original `.bak`, run both test suites, run the schema verify, run `verify-vendor.mjs`.

---

## 7. Git rules of this session (do not improvise)

- Branch: **`nexora/architecture`**. `master` untouched. Main branch for PRs upstream: `master`.
- Remotes: `origin` = upstream ZeroOmega (**never push**), `nexora` = `git@github.com:k2dms/NEXORA.git`.
- **Nothing is committed yet.** Planned commits, in order, each one logical piece (plan §8.5):
  (a) infrastructure — `.gitattributes`, `.env.example`, `.gitignore`, `.npmrc`×5, `tools/*`;
  (b) `docs/audit/*`; (c) `packages/schema/*` with green `npm run verify`; (d) `docs/adr/*` + `docs/architecture/*`;
  (e) `docs/verification/*`; (f) baseline fixes (coffeelint demotion, `verify.yml`, optional TZ change) — kept
  separate so it can be reverted without losing documents.
- **Every push requires separate explicit confirmation.** No `--force`, no `--no-verify`, no amend of published commits.
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
| 2 | Lockfile policy: keep `package-lock.json` ignored (build reproducibility stays UNVERIFIED) or add `!packages/**/package-lock.json`? | CI as a trust anchor |
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

*End of handoff log. Sections §4.5, §6 and §7 together are the complete remaining work order; §8 and §9 are the
reasons. Everything in §1, §2, §5 was measured, not inferred — re-run the probes before contradicting it.*
