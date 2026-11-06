# ADR-0018: Separate workspace roots (`packages/*`, `nexora/extension`, `connector/`) and no root `package.json`

Status: Accepted
Date: 2026-11-04

## Context

The legacy chain is already a monorepo with its own orchestrator, and its coupling is by *relative path*,
not by package registry. `omega-build/package.json:11-16` is the whole developer surface: `deps` walks
`../omega-pac`, `../omega-target`, `../omega-web` (plus `bower install`) and `../omega-target-chromium-extension`
with `npm install` in each; `dev` runs `npm run dev` in each of them; `build` is bare `grunt`; `release`
cds into the extension project and runs `grunt release`. The hub discovers those projects by glob --
`omega-build/Gruntfile.coffee:2` lists the submodules and `omega-build/Gruntfile.coffee:7` matches
`../*/Gruntfile.*` -- and the projects are wired to each other by `npm link` and by relative `file:`-style
entries: `omega-web/package.json:21` declares `"omega-pac": "../omega-pac"` and
`omega-web/package.json:24` is `npm link omega-pac && npm link`, while
`omega-target-chromium-extension/package.json:34` links three projects at once. Downstream projects then
read *prebuilt bundles at the root of the upstream project* through a `node_modules` symlink.

That is a graph where the position of a directory and the identity of its `node_modules` are both semantic.
Hoisting a dependency into a shared root, or letting a runner install "everything" from above, is not a
cosmetic change to it -- it is a rewrite of it, and ADR-0002 puts that out of scope.

The new code needs its own roots: `packages/*` holds the machine-checked contracts -- today
`packages/schema/package.json:8` defines its `verify` as `node scripts/verify.mjs` and installs only `ajv` --
the UI will
live under `nexora/extension`, and the Connector is Go under `connector/`. There is currently exactly one
`package.json` per legacy project and none at the repository root: `git ls-files` shows five, all inside
`omega-*`.

## Decision

1. The repository MUST have **no root `package.json`**, and no root `go.work`, and no root build runner
   config. There is no command that installs or builds "the repository" as one unit.
2. Each root is independent and self-describing: every package under `packages/` is its own npm root
   installed and run with `npm --prefix packages/<name>`; `nexora/extension` is its own npm root; `connector/`
   is its own Go module with `connector/go.mod` and nothing Go-shaped outside it. A root MUST declare its
   own dependencies and MUST NOT rely on hoisting from a sibling root.
3. The legacy chain keeps exactly one driver: the `omega-build` hub. Legacy commands stay
   `cd omega-build && npm run deps|dev|build|release` (`omega-build/package.json:11-16`). NEXORA tooling MUST
   NOT add a second path that reaches the grunt graph, and MUST NOT wrap it in a root script whose name
   makes it look like part of the new build.
4. Cross-root imports are forbidden in one direction and permitted in one: nothing under `packages/**` or
   `nexora/**` may import from `omega-*`, except types *derived from* `packages/schema`. Contracts are data
   and the UI consumes generated types; the legacy engine consumes nothing new.
5. `.npmrc` is per-root policy, not global. The five `legacy-peer-deps=true` files exist only inside the
   `omega-*` projects (measured: `omega-build/.npmrc`, `omega-pac/.npmrc`, `omega-target/.npmrc`,
   `omega-web/.npmrc`, `omega-target-chromium-extension/.npmrc`) and MUST NOT be moved to a root, because a
   root `.npmrc` would change resolution for every future npm root at once.
6. `tools/*.mjs` stay dependency-free single-file Node scripts run as `node tools/<name>.mjs`, so they work
   before any install exists. They MUST NOT gain an npm dependency; if one is genuinely needed, that is a
   new root with its own `package.json`.
7. CI MUST invoke the per-root commands explicitly (`npm --prefix packages/schema run verify`,
   `go -C connector test ./...`, `cd omega-build && npm run build`), and MUST NOT create a root
   `package.json` to shorten a workflow. This is a rule about `verify.yml`, which does not exist yet; the
   existing release workflow already runs from `working-directory: 'omega-build'`, which is the pattern to
   follow.

## Consequences

- There is no single `npm install`. A contributor learns four commands per area instead of one per repo.
  That is the intended cost: it makes "I ran install at the root and the legacy build changed" impossible.
- Any tool or agent that defaults to root-level `npm test` / `npm run build` gets a hard, immediate failure
  rather than a surprise mutation of the legacy graph. This is the primary reason for the rule.
- Lockfiles, `node_modules` and dependency policies are per root, so `packages/schema` can be strict and
  `omega-web` can stay on its 2016-era resolution without either contaminating the other. It also means the
  reproducibility question is answered per root -- `packages/schema`'s lockfile is currently untracked, so
  that root's clean-machine install remains UNVERIFIED.
- Adding a shared helper means adding a root (and its `package.json`), not dropping a file in a `common/`
  directory. Deliberate friction.
- Go and npm never share a directory, so `gofmt`/`go vet` and lint/format tooling cannot trip over each
  other's files, and `connector/testdata` is not scanned by JS tooling.
- Artifact naming and release packaging stay in the legacy hub; the NEXORA release path is a separate
  workflow, per `docs/architecture/20-target-repository-layout.md`.

## Alternatives considered

- **Root `package.json` with npm `workspaces` covering `omega-*` too.** Rejected: npm hoisting rewrites the
  `node_modules` layout the `npm link` chain and the prebuilt-bundle-at-project-root reads depend on
  (`omega-web/package.json:24`), and it converts ADR-0002's "additive overlay" into a migration of working
  code. This is also the mechanism by which an unrelated tool could drive the legacy graph by accident.
- **One npm root at `packages/package.json` with workspaces for the contract packages.** Considered, not
  taken now: it keeps `connector/` and `nexora/` out but couples contract packages to one install step, and
  today only one package exists. MAY be revisited by a new ADR once three or more `packages/*` need a shared
  install; it would still not add a repository root.
- **Nx / Turbo as a repo-wide runner.** Rejected: a runner becomes the de-facto root that schedules
  everything, including grunt-hub tasks it does not understand, and buys caching nobody has measured a need
  for. The repo already has one orchestrator, and it is not going to have two owners.
- **pnpm or Yarn workspaces at the root.** Rejected for the same hoisting/layout reason plus a third
  toolchain the host does not have and the CI matrix does not need.
- **A root `Makefile` / `justfile` as the single entry point.** Rejected in Phase 1: it re-creates the
  "one command drives the legacy graph" hazard in a different syntax while adding a tool nothing else here
  uses. A documented command list in `docs/README.md` is the substitute.

## Evidence

- VERIFIED -- `omega-build/package.json:11-16`: `deps`/`dev`/`build`/`release`, all relative `cd ..` chains;
  `omega-build/package.json:6-9` declares `bower`, `grunt-cli`, `grunt ~0.4.1` and `grunt-hub` as its own
  devDependencies, i.e. the hub owns the legacy toolchain.
- VERIFIED -- `omega-build/Gruntfile.coffee:2` (submodules list) and `omega-build/Gruntfile.coffee:7`
  (hub `src: "../*/Gruntfile.*"`), which is how a directory name becomes a build-graph node.
- VERIFIED -- `omega-web/package.json:21` (`"omega-pac": "../omega-pac"`), `omega-web/package.json:24`,
  `omega-target-chromium-extension/package.json:34` for the `npm link` chain.
- VERIFIED -- no root `package.json`: `find . -maxdepth 1 -name package.json` is empty and `git ls-files`
  returns five, all under `omega-*`; five `.npmrc` files, all under `omega-*`.
- VERIFIED -- `packages/schema/package.json:8`: the contract root's `verify` script, self-contained with the
  two `ajv` dependencies at `packages/schema/package.json:10-13`.
- INFERRED -- npm hoisting would break the `node_modules`-symlink bundle reads: standard package-manager
  behaviour applied to the measured layout; no experiment performed.
- UNVERIFIED -- that a clean machine can still install the legacy chain (no tracked lockfiles; see
  `docs/audit/02-toolchain-decay-and-risk.md`).

## Verification

- Existing check: `npm --prefix packages/schema run verify` proves a per-root install-and-verify cycle works
  with no root manifest present (green: 1 220 checks).
- Existing check: `node tools/check-doc-evidence.mjs` is itself a rootless script (`node tools/...`), which
  is rule 6 demonstrated.
- Verified: YES -- a root `package.json`, `go.work` or `.npmrc` now fails a guard: the `root-bare` rule of
  `tools/check-layout-guards.mjs`, the `VER-04` step of `.github/workflows/verify.yml` -- a file this
  iteration did not write, run by CI since 2026-11-05 -- and rules 2 and 7's per-root calls.
