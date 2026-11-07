# 27 — Config compilation pipeline

> Purpose: how intent becomes bytes that Xray can load — the determinism contract, the port allocation
> strategy, inbound topology, the legacy-adjacent settings that must be mapped rather than guessed, and the
> rule that a rule which cannot be compiled is *reported*, never dropped.

Status: Accepted. The compiler itself is Phase 2 Go code (`connector/internal/compile/**`); everything here is
expressed as data or as a golden-config obligation that the Phase-2 tests inherit.

## 1. Reading rules

As in `docs/architecture/20-target-repository-layout.md` §1.

## 2. The pipeline in seven stages

```
desired state (ProfileRegistry)  ─┐
Server[] + references            ─┤ 1 resolve     every -nexora.server.<id> exists; dangling ⇒ InvalidProfile
RoutingPolicy                    ─┤ 2 split       each rule → browser plane | core plane | unrepresentable
ConnectorSettings (ports)        ─┤ 3 allocate    per selected server a loopback inbound port
FailurePolicy (global+profile)   ─┤ 4 embed       block target, restart budget
                                 ─┤ 5 emit        Xray JSON: sorted keys, no clocks, UTF-8, no trailing WS
                                 ─┤ 6 hash        sha256 over the emitted bytes ⇒ configHash
                                 ─┤ 7 validate    schema + "every inbound listens on 127.0.0.1" + probe
```

Stage 7 is what makes ADR-0006 a fact instead of an intention: the compiled document is inspected before Core
ever sees it, so a non-loopback bind is a build-time rejection, not a runtime exposure
(`docs/architecture/21-system-context.md` §12).

## 3. Determinism

Byte-stability is not a nicety — three mechanisms depend on it: `config.apply` restarting only on a hash change
(`packages/schema/protocol/commands/config.apply.json#/result/properties/unchangedBecauseHashMatched`), the
drift check on reconnect (`docs/architecture/25-native-messaging-protocol.md` §11), and the idempotent-replay
assertions in `packages/schema/fixtures/protocol/`.

The contract:

1. **Key order is lexicographic at every object depth**, emitted by the serializer, not inherited from map
   iteration (Go map order is randomised by design — an unsorted encoder would produce a different hash on
   every call). The engine being replaced never sorted either: the PAC it ships is built by iterating the profile
   table in whatever order the object has (`omega-pac/src/pac_generator.coffee:32`), so byte-determinism is an
   obligation NEXORA takes on, not one it inherits. Four golden files now check it; the remaining fixture obligations
   below keep this contract's row in §9 open.
2. **No clocks, no paths, no PIDs** in the config. `createdAt`/`updatedAt` are envelope bookkeeping
   (`docs/architecture/23-unified-server-model.md` §8) and MUST NOT reach the compiled bytes. The exclusion is
   structural, not a name match: the compiler emits only keys it built from the model, so a time-shaped field has
   no path into the document, and it is checked by the golden files rather than by a filter. A suffix rule would
   misfire in both directions — `At`/`Ms`/`Seconds`/`seq` would strip `stickySeconds`, `cooldownSeconds` and
   `probeIntervalSeconds`, which are semantics with declared ranges and defaults
   (`packages/schema/connector-profile.schema.json`, and `docs/architecture/23-unified-server-model.md` §8 explains
   why each exists), while missing the two fields that actually carry a clock in the engine being replaced:
   `revision`, a timestamp in hex (`omega-pac/src/utils.coffee:4`, stamped by `updateRevision` at
   `omega-pac/src/profiles.coffee:153`), and `lastUpdate`, an ISO string (`omega-target/src/options.coffee:805`).
   The suffix list is kept where it is honest — `packages/schema/scripts/verify.mjs` "idempotent replay returns the same result: ${file}#${req.id}" uses it to compare two
   replayed protocol *results*, which is a fixture device, not a compile rule.
3. **`installRoot` is not inlined** into the config (`packages/schema/connector-settings.schema.json#/properties/core/properties/installRoot`):
   an absolute path would make the hash machine-specific, so a relocated Core changes no hash; the install state it depends on is what `docs/architecture/33-binary-integrity.md` §7 and §11 answer.
4. **Numeric formatting is fixed** — no exponent form, no `1.0`; the port is an integer
   (`packages/schema/protocol/commands/core.start.json#/result/properties/inboundPorts/items/properties/port`).
5. **Encoding is UTF-8 with LF**, and arrays keep input order where order is meaningful — `serverIds` ordering
   for `selection.mode: ordered` (`docs/architecture/23-unified-server-model.md` §8) is semantics, so it is
   *not* sorted even though object keys are.
6. The hash is over the exact bytes written, lowercase hex sha256, matching the one pattern used everywhere
   (`packages/schema/protocol/commands/config.apply.json#/result/properties/configHash`).

**Golden fixtures** are the test: `connector/testdata/config/*.golden.json`, one per canonical shape — the
three in `docs/architecture/23-unified-server-model.md` §9 plus one multi-server profile, one with core-plane
rules, one fail-closed variant. A golden file is compared byte-for-byte, and `docs/architecture/39-verification-matrix.md`
owns the rule that a change to a golden file requires a named reason in the same commit. Four of the six are
pinned: `shape-a-vision-tcp-tls`, `shape-b-reality-xhttp`, `multi-server-ordered` and `two-plane-core-rules`.
`connector/internal/compile/golden_test.go` compiles the contract examples and compares all four byte-for-byte;
it writes snapshots only with `-golden.update`. The remaining two cases have different reasons: shape C stops at
its `headersRef`, whose object materialization is still undecided -- how a `keystore://` handle for an *object*
reaches the compiled config is decided (ODEC-45): the server document gains `headersKeys`, the key list the stored object holds, and the materializer fills their values from the store; the
fail-closed variant has no bytes of
its own because `docs/architecture/30-failure-policy.md` §7 puts the block in `chrome.proxy`, not in Xray.
`Verified: NO — deferred to Phase 2` (two of the six, for two reasons).

All four materialized configs were also accepted by Xray 24.11.11 through `connector/internal/apply/drive_test.go`; §9 distinguishes this from runtime evidence.

## 4. Port allocation

| Rule | Value / mechanism | Reason |
|---|---|---|
| Host | `127.0.0.1`, `const` in `packages/schema/connector-settings.schema.json#/properties/ports/properties/host` | ADR-0006 — the schema refuses to *describe* a LAN-visible inbound, and `core.start` refuses to run one |
| Range | `ports.range.min`/`max`, 1024–65535 bounds (`#/properties/ports/properties/range`), sanity asserted by `connector-settings.schema.json#/allOf` | a narrow range fails often on shared machines; a wide one collides with software the user already runs — the description in that file is the reason, kept with the data |
| One port per selected server tag | inbound tag `in-srv-<8 hex>` paired to outbound `srv-<8 hex>` | the browser-side decision names a *server*, so the port must be addressable per server |
| Reservation | the Connector holds the assignment table in its own store, keyed by `(profileId, serverId)` | two profiles referencing the same server must not allocate two ports for one tag |
| Conflict | if the OS refuses a reserved port, the Connector re-allocates **and re-applies** the browser config; it never silently uses a different port | a stale `chrome.proxy` setting pointing at a closed port is a fail-closed event with the wrong cause |
| Persistence | `ports.persistAssignments` default `true` | a Connector restart must not invalidate the proxy configuration the browser already has — otherwise every host reboot looks like an outage |
| Bound | inbounds ≤ 64 (`maxItems` on `packages/schema/protocol/commands/core.start.json#/result/properties/inboundPorts` and on `packages/schema/protocol/commands/config.apply.json#/result/properties/inboundPorts`) | matches `maxConcurrentRequests` order of magnitude and keeps one config file reviewable |
| Fail-closed port | never drawn from the allocation range | `docs/architecture/30-failure-policy.md` — a blocked state must not be reachable by an open inbound |

The last row is a real trap: `packages/schema/failure-policy.schema.json#/properties/block/properties/portRange`
chooses a port to *leave closed*. If that range overlaps the allocation range, a fail-closed write could point
at a live inbound and silently proxy instead of blocking. The rule is that the two ranges are disjoint, and the
disjointness is an open item for Phase 2 data (`docs/architecture/40-open-decisions.md`) — it cannot be
expressed as a JSON Schema constraint across two files today, so it is asserted where both are loaded:
`Verified: NO — deferred to Phase 2`.

## 5. Inbound topology: per tag, not one inbound with routing

**Chosen:** one loopback SOCKS5 inbound per server tag (`protocol: socks`, `auth: noauth`, `udp: true`), independent of outbound transport.
This matches the existing PAC and `connector/internal/health/probe.go`'s SOCKS5 client; an HTTP-based outbound transport does not imply an HTTP inbound.

**Rejected:** a single inbound plus internal routing. Rejected on evidence, not taste:

1. The browser can only express "use this proxy for this request" as a host:port pair. With one inbound, the
   *selection* decision would have to be smuggled into the request (a fake username, an HTTP `Host` trick, or
   PROXY-protocol-like framing), which means the browser plane's decision is no longer the one that decides —
   violating the one-plane rule of `docs/architecture/28-routing-model.md`.
2. Legacy PAC output is exactly `"SOCKS5 host:port; DIRECT"` (`omega-pac/src/profiles.coffee:82-89`) — the
   per-port shape is what the existing engine already produces, so per-tag inbounds are the *additive* choice
   and single-inbound is the rewrite. ADR-0002 and ADR-0013 both say don't rewrite what works.
3. Health probing becomes ambiguous: `server.test` against a shared port measures the routing table, not the
   server (`docs/architecture/22-component-contracts.md` §8, ADR-0020's egress rule).
4. Cost: 64 SOCKS5 inbounds in one Xray process is memory we can measure; the alternative saves it and
   spends the four problems above.

**Consequence to state plainly:** a NEXORA profile with 200 servers needs 200 inbounds, which exceeds the
`maxItems: 64` bound. That is a real product limit, so the honest behaviour is: the compiler refuses the
config, `profile.import` answers `QuotaExceeded` naming the count, and the UI asks the user to split the
subscription into profiles. Silently allocating ports until the range is exhausted is not acceptable.
`Verified: NO — deferred to Phase 2`; the ceiling value itself is an open decision
(`docs/architecture/40-open-decisions.md`).

## 6. Mapping the knobs that have no legacy equivalent

| Setting | Where it lives | Mapping rule |
|---|---|---|
| `domainStrategy` | `packages/schema/routing-policy.schema.json#/properties/corePlane/properties/domainStrategy`, default `IPIfNonMatch` | exactly Xray's `AsIs` / `IPIfNonMatch` / `IPOnDemand` — any other spelling makes Core reject the *whole* config, surfacing as `ConfigApplyFailed` rather than as a routing miss. Pinned to the release named in `packages/schema/examples/pins.xray.json` (v24.11.11) and re-checked against that tag before any Core bump |
| `sniffing` | `#/properties/corePlane/properties/sniffing` (`enabled` default `false`, `protocols`, `routeOnly` `const true`, default `true`) | when enabled, emitted as an inbound sibling of `settings`, with `protocols` mapped to `destOverride` and `routeOnly: true` to prevent destination rewriting. The schema admits only `http`, `tls`, `quic`, `fakedns` for sniffing; `split.go` also refuses any other token or explicit `routeOnly: false`, even when sniffing is disabled |
| `localDNS` / resolver choice | not modelled yet | deliberately absent: a resolver setting changes what "the same rule" means, and Phase 1 has no evidence for which default the user expects. Open item, `docs/architecture/40-open-decisions.md` |
| `insecure` TLS | `packages/schema/server.schema.json#/$defs/tlsSettings/properties/insecure` | **never** — the field is `const false`, so a client that accepts any certificate cannot be spelled in this model at all; a link or legacy profile asking for it is refused whole (`docs/architecture/29-subscriptions-and-servers.md` §5's loss table) and reported in §7's unrepresentable output, not downgraded. The mask/label rules for what a TLS failure may reveal are `docs/architecture/32-secrets-keystore-masking.md` |
| `alpn` / `fingerprint` | `#/$defs/tlsSettings/properties/alpn`, `#/properties/fingerprint` | passed through verbatim — these are the observable shape of the connection, so the compiler MUST NOT normalise them |
| per-server `mux`/`maxHops` | absent from the model | rejected in `docs/architecture/23-unified-server-model.md` §7; adding them requires an ADR |
| Empty policy levels | `connector/internal/compile/compile.go` | `policy.levels` is `{}`, not `[]`; Xray expects an object |
| VLESS credentials | `connector/internal/compile/outbound.go` | `settings.vnext[]` holds `address`, `port` and `users[]`; `id`, `encryption: none` and optional `flow` belong inside each user. `id` remains a keystore handle until `apply.Write` materializes it |
| Built-in rule targets | `connector/internal/compile/split.go` | referenced `direct` and `block` targets emit `freedom` and `blackhole` outbounds respectively, after server outbounds in fixed `direct`, `block` order; unreferenced built-ins are not emitted |
| Routing selectors and priority | `connector/internal/compile/split.go`, `compile.go` | `network` and `protocol` retain array form; authored rules keep first-match priority ahead of last-priority `inboundTag: [in-srv-<8 hex>]` → `outboundTag: srv-<8 hex>` fallbacks for each selected tag. These explicitly pair otherwise-unmatched ports to their servers instead of relying on Xray's first outbound; the multi-server golden pins both pairings. Routing's `protocol` selector is separate from the strict sniffing allowlist |

These are implemented, tested interim forms. `connector/internal/apply/drive_test.go` recompiles the four golden
inputs, compares the snapshots, then calls `apply.Write` and Xray 24.11.11 `run -test -c`: all four written configs
were accepted and the invalid-port control rejected. `connector/internal/compile/drive_test.go` separately checks loopback HTTP and SOCKS5 UDP; `ODEC-37` retains the unproven predicate semantics.

## 7. The unrepresentable report

Two fields intentionally carry the same shape so both the *policy author* and the *operator* see it:

- at the policy level — `packages/schema/routing-policy.schema.json#/properties/corePlane/properties/unrepresentable` (what this policy could never express), and
- at the apply level — `packages/schema/protocol/commands/config.apply.json#/result/properties/unrepresentable` (what this compile could not place); each entry is `{source, reason}`, both strings bounded to 200 characters.

1. **Report, never drop** (ADR-0013). A silently dropped rule is a security claim the machine does not honour: "youtube via proxy" quietly became "youtube direct".
2. `source` is the user-visible rule name, never the raw condition object, which can contain a private hostname; `config.apply` results are logged
   (`docs/architecture/36-observability.md`).
3. The report is *not* an error: applying succeeds, the outcome is a visible notice in the UI and a
   `routing.decision`-adjacent diagnostic entry. A warning that blocks the apply would make the feature
   unusable for every legacy `<local>`/CIDR combination Xray cannot express.
4. Every entry in the examples is bounded (`maxItems: 256`), so a pathological ruleset cannot turn the response
   into a large frame: chunking is for imports, not for diagnostics
   (`docs/architecture/25-native-messaging-protocol.md` §5).
5. `docs/architecture/28-routing-model.md` owns which conditions are representable; this document only owns
   that the report exists, is bounded, and is shown.

## 8. Atomicity and rollback

`config.apply` writes to a temp file, fsyncs, renames over the previous compiled config, and only then signals
Core. A failure at any stage leaves the previous config *applied and unchanged*, and the response is
`ConfigApplyFailed` with `previousConfigHash`
(`packages/schema/protocol/commands/config.apply.json#/result/properties/previousConfigHash`) so the extension
can detect that its belief about the applied state was wrong. The extension-side mirror of this rule — "a
failed refresh keeps the last good version" — is `docs/architecture/29-subscriptions-and-servers.md`; the two
are the same principle at two layers, which is why neither is allowed to be "best effort".
The fsync above is the file's: a directory-level flush has no Windows equivalent, so a power loss between the rename
and the metadata flush can lose the rename — and the config is re-derivable from intent plus keystore, so the answer
is a re-apply, not a recovery. Who hands the supervisor its `Launch` in production is decided: `settings.installRoot` overrides
the pin, null resolves to the pin's own per-user core directory, and the supervisor's `Launch` reads the install
receipt at that root per spawn — the rule doc 33 §7 states beside its receipts bullet, and what let `main.go` register
the method.
`Verified: NO — deferred to Phase 2` (the writer and its rollback are Go tests in `connector/internal/apply`, and
`connector/cmd/nexora-connector/config_apply.go` now calls them from inside one command frame; what no test can show
is an apply that failed while a real Core was loading the previous config).

## 9. Invariants of this pipeline and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| `ports.host` is a `const`, not an enum with two values | `#/properties/ports/properties/host` | **GREEN** |
| Port range is bounded and sane | `connector-settings.schema.json#/allOf` | **GREEN** |
| `domainStrategy` accepts only Xray's three values | `packages/schema/scripts/verify.mjs` "corePlane.domainStrategy is exactly the set Xray documents", "the domainStrategy default is a value Xray accepts", "the domainStrategy description cites the source the enum was taken from" and "routing example uses a documented domainStrategy" | **GREEN** |
| `unrepresentable` shape is identical in policy and command | shared `{source, reason}` shape in both files, each `additionalProperties: false` and `maxItems: 256`; `connector/internal/compile/split.go` fills it and its bounds are tested, and `connector/cmd/nexora-connector/config_apply.go` carries what it fills into the command answer | **GREEN** for the shape; the *reporting behaviour* is Verified: NO — no session shows a browser reporting rather than dropping it |
| Every `*Hash` field is one canonical lowercase sha256 pattern | the coverage check in `packages/schema/scripts/verify.mjs` "${name}.${part}.${field} uses the canonical sha256 pattern" | **GREEN** |
| Compilation is byte-deterministic | the serializer and four goldens — `emit_test.go` re-emits one hand-built document two hundred times and compares an independent encoder, and `golden_test.go` compiles the contract examples and compares the result byte-for-byte with `connector/testdata/config/*.golden.json`; registry order and map order are both covered | Verified: NO — golden configs (VER-28's tier; no live row tests the determinism of a *compiled* config) |
| Non-loopback bind is impossible to emit | `connector/internal/compile/validate.go` refuses any inbound whose `listen` is not `127.0.0.1` before stage 7 returns, and `compile_test.go` shows an `0.0.0.0` inbound cannot survive it | Verified: NO — deferred to Phase 2 |
| Block port range and allocation range are disjoint | `connector/internal/compile/ports.go` refuses a range that overlaps the block range rather than drawing around it, and `model_test.go` checks the two shipped documents | Verified: NO — deferred to Phase 2, `docs/architecture/40-open-decisions.md` |
| The 64-inbound ceiling produces `QuotaExceeded`, not a partial config | `ports.go` refuses the whole set naming the count, so no partial document is ever emitted; whether `QuotaExceeded` is that count's code is `ODEC-03`'s question | Verified: NO — deferred to Phase 2 |
| The core-rule and per-tag inbound spellings a golden must pin | §5–§6 record the implemented interim forms; `split_test.go` and `golden_test.go` pin their emitted bytes, and `connector/internal/apply/drive_test.go` measures acceptance of four materialized configs by Xray 24.11.11. Loopback HTTP/UDP tests additionally verify the measured routing subset; `invert: true` is reported whole because Core ignores it. `ODEC-37` retains the remaining predicate semantics | open decision, `docs/architecture/40-open-decisions.md` |
| A failed apply leaves the previous config applied | `connector/internal/apply/write_test.go` writes a document whose secret is missing over a good config and re-reads the file, byte-for-byte, plus the hash the contract would report | Verified: NO — deferred to Phase 2 |
| §2's seven stages and §8's write are reachable from one command frame | `connector/cmd/nexora-connector/config_apply.go` — compare-and-swap against the applied-state record, the intent documents the frame itself carries, compile, atomic write, port reservation into the applied-state record, stop-then-start — with `config_apply_test.go` showing a compiled file on disk, a child handed that path, and each refusal leaving everything after it untouched | Verified — the intent documents ride the frame, the directories are doc 34 §7's per-user layout, and `main.go` registers `config.apply` in the dispatcher; the runtime half of a real frame from an extension is the live rows, which are still `SKIPPED` |
| `localDNS` semantics decided before Phase 2 ships | — | open decision, `docs/architecture/40-open-decisions.md` |
| Which byte string `configHash` covers — the handle-bearing document or the materialized bytes | answered 2026-11-07 by ODEC-36's first option: the handle-bearing document, whose digest `emit_test.go` pins for shape A — machine-independent, and a digest whose input holds no secret material, which is why the written bytes are not the subject (`packages/schema/protocol/commands/config.apply.json#/result/properties/configHash`'s description states the rotation cost beside it) | Verified as data |

Trace: ADR-0002, ADR-0003, ADR-0006, ADR-0008, ADR-0010, ADR-0012, ADR-0013, ADR-0019, ADR-0020 · Contract:
`packages/schema/connector-settings.schema.json`, `packages/schema/routing-policy.schema.json`,
`packages/schema/server.schema.json`, `packages/schema/protocol/commands/config.apply.json`,
`packages/schema/protocol/commands/core.start.json`, `packages/schema/examples/pins.xray.json` · See also:
`docs/architecture/23-unified-server-model.md`, `docs/architecture/28-routing-model.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/33-binary-integrity.md`,
`docs/audit/07-routing-engine.md`.
