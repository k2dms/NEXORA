# @nexora/schema

Machine-readable contracts for NEXORA. This package is the executable half of Phase 1: every
architectural claim in `docs/architecture/**` that can be expressed as a shape lives here, so that
"agreed" means "checkable".

```
npm install
npm run verify
```

`verify` compiles every schema, validates every example, asserts every negative case is rejected
*for the reason recorded in it*, and enforces the rules JSON Schema cannot express alone. It exits
non-zero on any failure and prints the count of checks run.

## Layout

| Path | What it is |
|---|---|
| `*.schema.json` | Entity contracts: Server, ConnectorProfile, RoutingPolicy, SubscriptionSource, FailurePolicy, Pins, connector settings, import staging, envelope backup, the event catalogue, the `-nexora.*` key registry |
| `protocol/envelope.schema.json` | One Native Messaging frame: request, response, event, progress |
| `protocol/framing.json` | Byte limits and the oversized-payload decision |
| `protocol/error-taxonomy.json` | The closed error enum with `retryable` flags |
| `protocol/command-allowlist.json` | The 19 dispatchable methods and their privilege class |
| `protocol/compatibility-rules.json` | Protocol versions and capability names |
| `protocol/commands/*.json` | Args schema, result schema, errors, timeouts, idempotency class and side effects per command |
| `examples/` | One valid instance per entity, including the canonical Xray shapes |
| `examples/invalid/` | Cases that MUST be rejected, each with `reasons.json` naming the schema and the keyword responsible |
| `fixtures/protocol/*.jsonl` | Recorded frame exchanges: negotiation, core lifecycle, config apply and snapshot, chunked import, fail-closed behaviour, probes and operations |

## Rules this package encodes and why

- **Secrets are references, never values.** Every credential field matches `^keystore://…`. A
  subscription's `Authorization` header reaching `chrome.storage.sync` is the historical bug
  (SEC-1, `docs/audit/11-security-findings.md`); a schema that cannot spell a value cannot leak one.
- **NEXORA entities live under `-` keys.** `Profiles.each` iterates keys whose first char is `+`
  (`omega-pac/src/profiles.coffee:111-118`), so a `+`-prefixed entity would appear in the quick-switch
  list and in generated PAC. `examples/envelope-keys.nexora.json` is checked for this.
- **`failOpen` requires evidence.** A per-profile fail-open must carry a `confirmToken`; the schema
  says so with `if/then`, so any validator in any language rejects an unconfirmed fail-open.
- **Impossible transport combinations are unrepresentable.** `flow: xtls-rprx-vision` is legal only
  with `network: tcp`; `reality` cannot contain a `privateKey`, because a client that holds a server's
  private key is not a client.
- **Replays are byte-identical apart from clocks.** Each idempotent command has a replay pair in
  `fixtures/protocol`, compared after stripping fields ending in `At`, `Ms`, `Seconds`, `seq`.
  Probes (`server.test`) are asserted to have exactly one answer: a cached latency number is a lie.
- **A chunked import is a group, not a frame.** The slice fields live nested under `chunk` —
  `contentHash`, `chunkSeq`, `chunkCount` (>= 2), `encoding` (mandatory) — in
  `protocol/envelope.schema.json#/properties/chunk`, and `protocol/commands/profile.import.json`
  mirrors that exact object under `args.chunk`; there is no top-level `chunkId` and no `chunk` frame
  type. `verify` asserts the two shapes are identical, that within one frame the envelope copy and
  the `args` copy are byte-equal, and per group (keyed by `envelope.id`) that every slice declares the
  same `chunkCount`, that exactly `chunkCount` slices arrived, that `chunkSeq` covers `0..chunkCount-1`
  once each, that `args.contentHash` is the hash of the whole — and that a group which splits its hash
  is answered by `IntegrityMismatch` instead of being reassembled. A corpus with no chunk group at all,
  or with a clean group or a rejected group missing, fails rather than passing vacuously.
- **Capability names have one owner.** `protocol/compatibility-rules.json` is the registry; the
  recorded `connector.hello` in `fixtures/protocol/handshake.jsonl` must announce exactly that set, and
  every capability a command gates on must be in both.
- **One hash, one encoding, one value.** Every `*Hash` string field in `protocol/commands/*.json` uses
  the canonical lowercase sha256 pattern (nullable only where its `type` says so), and every applied
  config hash a status command reports in the fixtures was produced by `config.apply`, `core.start` or
  `core.restart` somewhere in the corpus — unless that frame flags `drift`.
- **Enums must name values the engine accepts.** `corePlane.domainStrategy` is exactly Xray's
  documented `AsIs` | `IPIfNonMatch` | `IPOnDemand`
  ([xtls.github.io/en/config/routing.html](https://xtls.github.io/en/config/routing.html)), pinned to
  the release in `examples/pins.xray.json` (v24.11.11) and re-checked on any Core bump; every routing
  example is asserted against that enum.
- **Cited legacy code still exists.** Every `omega-*/**/*.coffee` path in this package resolves, and
  any cited line range fits inside the file — the check that would have caught
  `omega-pac/src/default_options.coffee`, which never existed; the shipped default is
  `omega-target/src/default_options.coffee:2`.
- **Optional fields say why.** A `required` array is a subset of the declared `properties`, and the
  `import-staging` description spells out the required six and the two optional fields (`body`,
  `attempt`) — asserted against the schema, then demonstrated by removing one field at a time.

## Adding to it

New command → add `protocol/commands/<name>.json`, register it in
`protocol/command-allowlist.json`, and cite it in `docs/architecture/26-command-surface.md`;
`verify` fails if the three disagree. New entity field → it must carry a `description`: the coverage
check in `scripts/verify.mjs` rejects an undocumented top-level field, because an undocumented field
has no owner in `docs/architecture/22-component-contracts.md`.

## Data documents (not schemas)

Five files here are lists rather than shapes: no `$schema`, no validation target, and each one exists because
an architecture document argues a fact that belongs in data. They are cross-read against the schemas and
command documents they name, so a disagreement fails here rather than at runtime.

- `migrations.json` — the schema-version ledger: the stamps, the ordered steps, the journal, the retention
  rule and the read-only policy for a future envelope. Argued in
  `docs/architecture/35-schema-versioning-migration.md`.
- `protocol/data-ownership.json` — the plane and store catalogue: which key pattern lives where, what may
  cross the transit, and which store holds credential material. Argued in
  `docs/architecture/34-data-split.md`.
- `observability.json` — the log contract: sinks and their masking stage, the event list, the diagnostics
  sections, the bounds and the retention. Argued in `docs/architecture/36-observability.md`.
- `presentation.json` — the screen contract: surfaces and the commands and events each may name, the closed
  state vocabulary and error placements, the derived confirmation and dryRun sets, the theme token pipeline
  measured against the inherited catalogue, the locale counts measured against the `.po` tree, the popup's
  keyboard and cold-start obligations, and the parity rows that gate the cut-over. Argued in
  `docs/architecture/37-ui-architecture.md`.
- `compatibility.json` — the compatibility matrix: the floors and permission lists the inherited manifests
  actually carry, the browser-API surface including the APIs the legacy code never calls, the six profile types
  and four aliases, the eleven `-` option keys, the backup shape and the two destructive legacy paths, the two
  sync backends, and one row per promise with a verdict and the measurement still owed. Argued in
  `docs/architecture/38-compatibility-matrix.md`.

A new list of this kind follows the same rule as a new command: the document that owns it must exist, and the
assertions that read it must cross-check it against the contracts it borrows names from — never against itself.
