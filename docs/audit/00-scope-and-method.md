# 00 — Scope and method of this audit

> Purpose: define what was audited, on what machine, with what evidence standard, and what the reader is
> allowed to trust from the twelve documents in this directory. Nothing here is a design decision; decisions
> are ADRs (`docs/adr/`), designs are `docs/architecture/`, contracts are `packages/schema/`.

Phase 0, 2026-11-04, branch `nexora/architecture`, tree = ZeroOmega v3.5.2 plus an additive overlay.

---

## 1. What was audited

The repository **as it exists today** — the legacy ZeroOmega fork that NEXORA inherits — read as source and
as build output, not as documentation. Five CoffeeScript projects plus the build hub and the locales:
`omega-pac`, `omega-target`, `omega-web`, `omega-target-chromium-extension`, `omega-build`, `omega-locales`,
and the two CI definitions under `.github/workflows/` and `.circleci/`.

Explicitly **not** audited, because it does not exist yet:

| Subject | Status |
|---|---|
| Go Connector (`connector/`) | not written when this was audited; the module and its first three Go packages landed on 2026-11-05, and neither process still (`docs/PROJECT-STATE.md` §4.6) |
| Xray Core integration | not present |
| Native Messaging host / protocol implementation | not present — `grep -i nativeMessaging` over the whole tree (excluding `node_modules`, `bower_components`, `build/`) returns **0 files** |
| VLESS / REALITY / Vision code | not present — `grep -i` for `vless`, `xray`, `reality` over `omega-*` and `.github` returns **0 files** each |
| New UI (`nexora/extension`) | not written |
| `packages/{protocol,state,i18n,ui}` | not written |

The last two rows are the reason this audit exists: before Phase 1, the only defensible statement about
"what NEXORA will replace" is a measured description of what is there now.

## 2. Host toolchain (reproducibility anchor)

| Tool | Version | How established |
|---|---|---|
| Node.js | v24.22.1 | `node -v` |
| npm | 11.19.1 | `npm -v` |
| git | 2.56.0.windows.3 | `git --version` |
| Go | **absent** | `command -v go` → empty |
| Docker | **absent** | `command -v docker` → empty |
| Python | **absent** (only the Windows Store alias stub, which prints "Python was not found") | `python --version` |
| Bower (standalone CLI) | **absent** from PATH; available only as a project dependency (`omega-build/package.json:6` declares `"bower": "~1.8.4"`) and invoked via `grunt-bower-task` | `command -v bower` → empty |
| `npm config get script-shell` | `null` → npm scripts execute through `cmd.exe` | measured |
| OS | Windows, 10.0.26300, x64 | environment |

Consequences of that table are recorded where they bite: `npm test` in omega-pac cannot run because of the
POSIX `TZ=` prefix (`omega-pac/package.json:32`), and any future CI matrix must decide whether the legacy
chain runs on Node 22 (what `.github/workflows/release.yml:21` pins) or Node 24 (what this host has, and
what the developer will use).

## 3. Measured size of the inheritance

Counted on the tracked tree (`git ls-files` → **408 tracked files**), by extension:

| Ext | Files | Note |
|---|---|---|
| `.css` | 175 | almost all generated base16 theme sheets |
| `.coffee` | 106 | source, grunt config and tests |
| `.jade` | 29 | the whole AngularJS template layer |
| `.js` | 28 | overlays, polyfills, generated bundles checked in at project roots |
| `.po` | 27 | one per locale directory in `omega-locales` |
| `.json` | 9 | manifests and package metadata |
| `.html` | 6, `.less` 3, `.svg` 2, `.png` 8, `.md` 3, `.yml` 2, `.gitignore` 5, `.npmrc` — see §5 | |

CoffeeScript lines by project (`src` = product source, `grunt` = build config, `test` = tests):

| Project | src files | src LOC | grunt LOC | test files | test LOC |
|---|---|---|---|---|---|
| omega-pac | 6 | 1 751 | 72 | 6 | 1 533 |
| omega-target | 8 | 1 941 | 70 | 1 | 207 |
| omega-web | 19 | 2 394 | 152 | **0** | 0 |
| omega-target-chromium-extension | 27 | 3 524 | 196 | **0** | 0 |
| omega-build | 0 | 0 | 0 | 0 | 0 |
| **Total** | **60** | **9 610** | **490** | **7** | **1 740** |

Plus 532 LOC of hand-written JavaScript in `omega-target-chromium-extension/overlay/*.js` and
`src/js/*.js` (polyfills, popup bridge, the Firefox webext proxy script).

**The engineering brief's inventory is wrong and this audit corrects it.** The brief describes "omega-pac
10 089 LOC" and "~21k LOC total". Measured: omega-pac is 1 751 source lines (3 356 including its grunt
configs and tests), and the whole repository is ~11 840 CoffeeScript lines including tests and build config.
The brief's numbers are plausibly read off the generated bundles (`omega_pac.min.js` alone is 742 272 bytes).
Every effort estimate derived from the brief's LOC is therefore inflated by roughly 3×. **VERIFIED** by
`find`/`wc`; recorded here so Phase 2 planning uses the measured figure.

## 4. Evidence standard

Every claim in `docs/audit/**` is required to carry:

1. a citation of the form *repo-relative path, colon, line number* inside backticks — e.g.
   `` `omega-pac/src/profiles.coffee:111` ``, or a range like `` `omega-pac/src/profiles.coffee:111-118` ``; and
2. one of three tags:
   - **VERIFIED** — the auditor read the cited line and it states the claim;
   - **INFERRED** — the claim was derived from code that was read, but the mechanism is not directly
     observable in the tree (runtime behaviour, browser behaviour, timing);
   - **UNVERIFIED** — asserted but not checked. Every UNVERIFIED claim must also appear in that document's
     `## UNVERIFIED registry`.

`node tools/check-doc-evidence.mjs` enforces the citation half mechanically: it scans every Markdown file
under `docs/`, extracts each `` `path:line` `` reference and fails if the file is missing, if the cited line
is out of range, or if the cited line is blank. A drifting citation is worse than no citation, because the
reader cannot tell them apart. Target-layout paths (`connector/`, `nexora/`, `packages/protocol/…`) and
derived trees (`build/`, `tmp/`, `dist/`, `node_modules/`) are exempt from the "missing file" check only —
an out-of-range citation is never excused.

What the checker does **not** guarantee: that the cited line actually says what the prose claims. That is a
human review item, and it is why the tags exist.

## 5. Method

1. **Probe first, edit never** — the legacy chain was run three times before any file was touched, and the
   raw output is archived (`docs/audit/raw/`, described in `docs/audit/99-baseline-report.md`).
2. **Read the code, then write the claim** — the twelve documents below are ordered by layer, from build
   graph to security findings, each written against `Read`/`Grep` evidence rather than against upstream
   SwitchyOmega documentation, which describes a different codebase in several places.
3. **Turn each durable constraint into data** — where a finding is a rule NEXORA must keep, it was pushed
   into `packages/schema/` so it is machine-checked rather than remembered. The chain is:
   `docs/audit/NN` (fact) → `docs/adr/NNNN` (decision) → `docs/architecture/2N` (design) →
   `packages/schema/*.schema.json` + `protocol/commands/*.json` (contract) →
   `packages/schema/scripts/verify.mjs` (executable check) → `39-verification-matrix.md` (who owns the check).
   Example: the `+`/`-` envelope rule found in `omega-pac/src/profiles.coffee:111-118` became
   `envelope-keys.schema.json` and a literal assertion in the verifier.
4. **Count, don't estimate** — locale dirs, theme count, controller LOC, test files, zip entries: measured.

## 6. Document map

| File | Covers | Status |
|---|---|---|
| `00-scope-and-method.md` | this file | written |
| `01-repository-and-build-graph.md` | the 5 projects, grunt task tables, artifact chain, name-coupling | written |
| `02-toolchain-decay-and-risk.md` | dependency versions, missing lockfiles, CI verdict, `TZ=`, empty test globs | written |
| `03-runtime-layering.md` | the real seam (`Options` + injected `proxyImpl`), exports, module ownership | written |
| `04-data-model-and-options-envelope.md` | profile types and their contracts, the flat envelope, `+`/`-` keys | written |
| `05-storage-sync-and-migration.md` | storage classes, MV3 polyfills, `storage.session`, upgrades, the wipe trap | written |
| `06-proxy-application.md` | how a profile becomes `chrome.proxy` config, auth, PAC marker, external API | written |
| `07-routing-engine.md` | `Conditions`, `RuleList` parsers, `PacGenerator`, the Xray representability table | written |
| `08-ui-i18n-and-themes.md` | bootstrap mechanics, controllers, jade/LESS pipeline, locales, themes, two popups | written |
| `09-subscriptions-import-export-sync.md` | update path, import/export, `OptionsSync` and its backends | written |
| `10-manifest-packaging-distribution.md` | field-by-field manifests, listings, release workflow | written |
| `11-security-findings.md` | SEC-1 … SEC-7 with severity, evidence and reproduction description | written |
| `12-what-must-not-change.md` | the load-bearing behaviour list NEXORA is not allowed to break | written |
| `99-baseline-report.md` | probe output, lint violations, abort chain, artifact assertions, test floor, "not fixed" list | written |

The settled conclusions of this audit are summarised for continuation in
`docs/PROJECT-STATE.md` §2; that summary is a pointer, not a substitute.

## 7. Record of the removed working-tree edits

At the start of this session the tree contained three uncommitted, non-working edits: changes to product
sources plus a new `vless-test.coffee` that did not run. Per §21 of the engineering brief and the explicit
agreement with the user, they were **saved outside the repository and then removed**, not repaired silently:

- Preserved: `C:\Users\dms\.qoder\tmp\E--NEXORA\discarded-20261104T081418Z\` (patch plus the original files).
- Removed: the broken draft `vless-test.coffee` and the partial edits it depended on.
- Reason: a half-written VLESS implementation in JavaScript contradicts ADR-0003 (protocols belong to Xray in
  the Go connector), and keeping a non-building draft inside a branch that must stay green is worse than
  discarding it with a recoverable copy.
- No destructive operation was performed on any file that was not first copied outside the repo, and no
  git history was rewritten. See `docs/PROJECT-STATE.md` §7 for the git rules in force.

## 8. Aggregate UNVERIFIED register (Phase 0)

Individually listed per document; the ones that matter across the whole audit:

1. **Clean-machine reproducibility** of `npm install` + `bower install`: no lockfile is tracked
   (`.gitignore:3` ignores `package-lock.json`), Bower components are unpinned, and only one host was tested.
2. **Browser runtime behaviour** — `webRequest.onAuthRequired` with `blocking` under MV3, the actual
   `levelOfControl` transitions, and the blank-page consequence of a missing bundle are inferred from code
   and logs, not observed in Chrome or Firefox.
3. **CI history** — no run of either legacy configuration was executed (the third one this iteration added,
   `.github/workflows/verify.yml`, has: `docs/audit/99-baseline-report.md` §12 tabulates seven of its runs from
   2026-11-05 — two red, four green, one cancelled — and states the rule that keeps later green runs of the same shape
   out of the table); the verdict that both CI configurations fail today is reasoned
   from their contents (`docs/audit/02-toolchain-decay-and-risk.md`).
4. **The three `OptionsSync #requestPush` timeouts** — root cause not investigated.
5. **Upstream intent** — where behaviour differs from SwitchyOmega documentation, no attempt was made to
   decide which is "correct"; this audit describes this tree only.
6. **Every statement about the future connector** — by construction unverified here; they are designs, and
   each carries a row in `docs/architecture/39-verification-matrix.md` with `Verified: NO — deferred to Phase 2`
   until a check exists.

## 9. What Phase 2 may and may not assume from this directory

- May assume: every **VERIFIED** claim, provided the citation still resolves — run
  `node tools/check-doc-evidence.mjs` before trusting the audit after any refactor.
- May assume: the pinned regression floor in `99-baseline-report.md` §5 and the artifact assertions in §6.
- Must not assume: any **INFERRED** claim without re-checking it in a browser or against a running extension.
- Must not assume: that anything in §8 became verified by being repeated.
