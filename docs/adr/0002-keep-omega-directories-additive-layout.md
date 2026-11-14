# ADR-0002: Keep the omega-* directories where they are and add NEXORA as an overlay

Status: Accepted
Date: 2026-11-04

## Context

The intuitive first move when turning a fork into a product is to restructure: `legacy/`, `apps/`,
`packages/omega-pac`, a tidy monorepo. Phase 0 measured what that move actually costs in this tree and the
answer is large and load-bearing. `docs/audit/01-repository-and-build-graph.md` §7 enumerates **23 distinct
name-coupling sites across 11 files** — more than the "at least 15" the plan assumed — and every one of them
is a literal `omega-*` path inside build configuration that has no test underneath it: `omega-web` and
`omega-target-chromium-extension` contain **zero test files** (`docs/audit/99-baseline-report.md` §5).

The coupling is not only cosmetic. The hub build is driven by an array literal and a parent-directory glob
(`omega-build/Gruntfile.coffee:2`, `omega-build/Gruntfile.coffee:7`); the npm scripts are `cd ../omega-*`
chains (`omega-build/package.json:12`); downstream projects read the **prebuilt bundle at the root of the
upstream project through a `node_modules` symlink** (`omega-web/grunt/copy.coffee:4`,
`omega-target-chromium-extension/grunt/copy.coffee:10`), which is the subtle one — it survives any rename of
the *directory* only because the *package name* is unchanged; and the dead CI caches on the same names
(`.circleci/config.yml:14`).

Meanwhile the chain is green: `npm run deps`, `npm run dev`, `grunt`, `grunt release` all exit 0 on this
host, and the artifact assertion (353 files, `js/omega_pac.min.js` present) passes
(`docs/audit/99-baseline-report.md` §6). A restructure would put a working build at risk for no functional
gain, in an iteration whose deliverable is documents.

## Decision

1. The directories `omega-pac`, `omega-target`, `omega-web`, `omega-target-chromium-extension`,
   `omega-build` and `omega-locales` MUST NOT be moved, renamed, split or merged by any NEXORA change.
2. Their internal layout MUST be treated as frozen for this iteration: adding a file inside them is allowed
   only when a build break demands it; reorganising them is not.
3. The new system MUST be an additive overlay in new top-level roots: `packages/` (contracts and shared
   TypeScript), `connector/` (the Go connector, Phase 2), `nexora/` (the extension sources and new UI),
   `docs/` (this tree). None of these MAY exist inside an `omega-*` directory.
4. Nothing under `nexora/**` MAY import from `omega-*`, except types derived from `packages/schema`.
5. No root `package.json` MUST be added while the legacy grunt graph exists (ADR-0018), so that new NEXORA
   tooling cannot reach the legacy chain by accident.
6. If a future iteration wants the tidy layout, it MUST write a new ADR that supersedes this one and name
   all 23 coupling sites as migration work with an artifact assertion that proves the move; it MAY NOT do so
   as a side effect of another feature.
7. Package *names* (`omega-pac`, `omega-target`, …) MUST keep their current spelling even if the folders
   are ever relocated, because the `node_modules` symlink reads quoted above resolve by package name.

## Consequences

Cheaper: the green build stays green with zero new work; every NEXORA diff is confined to roots that have
no legacy coupling, so review is scoped by directory; the citation checker (`tools/check-doc-evidence.mjs`)
becomes an accidental guard — deleting or relocating a coupled legacy file breaks a doc citation loudly.

Harder: the repository looks inconsistent (lower-case `omega-*` beside `nexora/`), paths in the new UI are
long, and the artifact chain remains five separate npm installs with no lockfile, so the clean-runner
reproducibility problem recorded in `docs/audit/02-toolchain-decay-and-risk.md` is inherited, not solved.

Forbidden for this iteration: directory moves, `git mv` of any `omega-*` subtree, mass renames, adding a
root `package.json` or npm workspaces, and converting the relative `file:` dependencies
(`omega-target/package.json:26`, `omega-web/package.json:21`,
`omega-target-chromium-extension/package.json:25`) — that last one changes `npm link` behaviour in the `dev`
scripts and is a product-behaviour change disguised as tidiness.

## Alternatives considered

- **Relocate the fork under `legacy/omega-*` and update the 23 sites.** Rejected: the update surface is
  grunt configuration with no tests below it, and the failure mode measured in Phase 0 is silent — a missing
  `omega_pac.min.js` produced exit code 0 and a blank options page, not a build error.
- **Move the connector to its own repository.** Rejected: the command allowlist, the envelope and the framing
  limits are one contract shared by both sides; splitting them means a version-skew class of bug that the
  in-tree cross-check in `packages/schema/scripts/verify.mjs` "hello gating agrees between allowlist and command doc" and "command class agrees between allowlist and command doc" exists to prevent.
- **npm workspaces replacing `file:` deps and `npm link`.** Rejected for this iteration only, not on design
  grounds: it changes the install chain that is the sole reason the baseline is green, and no NEXORA feature
  depends on it. Recorded here so it is not mistaken for an oversight.
- **Vendor the legacy code as a git submodule pinned to upstream ZeroOmega.** Rejected: user data
  compatibility (ADR-0013) requires editing this fork's profiles, and a submodule makes the required changes
  impossible to land in the same commit as their contract.

## Evidence

- `omega-build/Gruntfile.coffee:2`, `omega-build/Gruntfile.coffee:7`, `omega-build/Gruntfile.coffee:10` —
  VERIFIED — submodule array, hub catch-all glob, per-module glob.
- `omega-build/package.json:12`, `omega-build/package.json:15` — VERIFIED — `cd ../omega-*` chains.
- `omega-web/grunt/copy.coffee:4` — VERIFIED — reads `node_modules/omega-pac/omega_pac.min.js`.
- `omega-target-chromium-extension/grunt/copy.coffee:4` — VERIFIED — cwd `../omega-web/build`.
- `omega-target-chromium-extension/grunt/copy.coffee:10` — VERIFIED — reads
  `node_modules/omega-target/omega_target.min.js`.
- `omega-target/package.json:26`, `omega-web/package.json:21`,
  `omega-target-chromium-extension/package.json:25` — VERIFIED — relative `file:` dependencies.
- `.circleci/config.yml:14` — VERIFIED — cache keys checksumming `omega-*/package.json`.
- The full 23-site enumeration, the task tables and the artifact chain — VERIFIED in
  `docs/audit/01-repository-and-build-graph.md`; the zero-test floor in `docs/audit/99-baseline-report.md`.
- "The chain is green on this host" — VERIFIED by the probes archived under `docs/audit/raw/`; clean-runner
  reproducibility — UNVERIFIED (no tracked lockfile).

## Verification

Today: `node tools/check-doc-evidence.mjs` fails if any cited coupling file disappears or its cited line
goes blank, which is a weak but real tripwire for rules 1 and 7; `npm --prefix packages/schema run verify`
does not look at the legacy tree at all. A layout assertion (the eleven coupled files still exist at the
cited paths, and `nexora/**` contains no import of `omega-*`) is specified for Phase 2 in
`docs/architecture/20-target-repository-layout.md` and is not implemented in this iteration.

Verified 2026-11-14, superseding the deferred verdict this line carried: rule 4's import boundary has a checker — the `VER-02` step of `.github/workflows/verify.yml`, green on three runner images and over the real extension sources since they landed (`docs/PROJECT-STATE.md` §4.71; history in git).
