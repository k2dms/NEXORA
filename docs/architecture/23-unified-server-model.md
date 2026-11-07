# 23 — Unified server model

> Purpose: fix the two entities that replace "a proxy profile is everything" — **Server** (a transport endpoint
> plus its credential reference) and **ConnectorProfile** (an ordered set of server references plus selection,
> failover and failure policy) — with a complete field table, the negative rules the compiler must refuse, and
> the Xray JSON each canonical shape compiles to.

Status: Accepted. `packages/schema/server.schema.json` and
`packages/schema/connector-profile.schema.json` are normative; this document explains them and is checked
against them by `npm --prefix packages/schema run verify`.

## 1. Reading rules

As in `docs/architecture/20-target-repository-layout.md` §1. Contract fields are cited as
`path#/json/pointer`, which the extended evidence checker resolves (the `POINTER_RE` rule in
`tools/check-doc-evidence.mjs`) — a
pointer that no longer exists fails the build the same way a stale line number does.

## 2. Why two entities and not one

Legacy OmegaPac has one unit of everything: a `+profile` carries its name, its proxy parameters, its rule list
and its update URL. That works for HTTP/SOCKS proxies, where "the proxy" is two scalars. It does not work here
for three concrete reasons:

1. **A server is not selectable.** The user selects a *profile* (a policy over servers), and `Profiles.each`
   iterating `+` keys (`omega-pac/src/profiles.coffee:111-118`) would put every individual VLESS endpoint into
   the quick-switch list and into generated PAC. That is why `Server` is **not** an OmegaPac profile and lives
   under `-nexora.server.<id>` (ADR-0012, `docs/architecture/24-options-envelope-compat.md`).
2. **A server is reused.** One VPS appears in "Work", "Fastest" and a subscription-derived set. Embedding it per
   profile means three copies of a credential and three places to rotate it.
3. **A credential must not travel.** Reference-not-value is the only design in which an envelope sync cannot
   leak a key (SEC-1, `docs/audit/11-security-findings.md`); a field that can only spell `keystore://…` cannot
   spell a secret by accident.

So: `Server` = transport truth, `ConnectorProfile` = user intent about *which* server, `RoutingPolicy` = user
intent about *when* (`docs/architecture/28-routing-model.md`).

## 3. `Server` — field by field

Required is exactly `["id","displayName","protocol","address","port","userIdRef","network","security"]`
(`packages/schema/server.schema.json#/required`); `additionalProperties: false` at the root means the table
below is complete — an unlisted field is a schema error, not a tolerated extra.

| Field | Type | Req | Default | Constraints | Why it is here |
|---|---|---|---|---|---|
| `id` | string (UUID v1–v5) | yes | — | `#/properties/id` | stable reference from `ConnectorProfile.serverIds`; also the seed for `tag` |
| `displayName` | string | yes | — | 1–80 | UI only; never used in a config |
| `protocol` | const `"vless"` | yes | — | `#/properties/protocol` | Phase 1 scope decision — see §7 |
| `address` | string | yes | — | 1–253, host/IPv6 literal pattern | the VPS; may be a name, resolved by Core per `domainStrategy` |
| `port` | integer | yes | — | 1–65535 | remote port, not the local inbound port |
| `userIdRef` | `keystore://…` | yes | — | `#/properties/userIdRef` | the VLESS user id **is** the credential; it never appears here |
| `network` | enum | yes | — | `tcp`, `ws`, `grpc`, `xhttp`, `h2` | transport container |
| `security` | enum | yes | — | `none`, `tls`, `reality` | TLS layer |
| `flow` | enum | no | `""` | `""`, `xtls-rprx-vision` | XTLS Vision; illegal combinations rejected (§4) |
| `tls` | `$defs/tlsSettings` | no | — | `serverName` required; `alpn`, `fingerprint`, `insecure` | present **iff** `security = tls` |
| `reality` | `$defs/realitySettings` | no | — | `publicKey`, `serverName` required; `shortId`, `spiderX`, `fingerprint` | present **iff** `security = reality`; note the absence of `privateKey` (§5) |
| `ws` | `$defs/wsSettings` | no | — | `path`, `host`, `headersRef` | required when `network = ws` |
| `grpc` | `$defs/grpcSettings` | no | — | `serviceName` required; `multiMode`, `authority`, `headersRef` | required when `network = grpc` |
| `xhttp` | `$defs/xhttpSettings` | no | — | `mode`, `path` required; `host`, `extra` | required when `network = xhttp` |
| `h2` | `$defs/h2Settings` | no | — | `path` required; `host` | required when `network = h2`, which also forces `security = tls` |
| `tag` | `^srv-[0-9a-f]{8}$` | no | derived | `#/properties/tag` | the Xray outbound tag; §6 |
| `notes` | string | no | — | ≤200 | user annotation |
| `source` | enum | no | `manual` | `manual`, `subscription`, `import` | provenance; drives whether the UI may edit it in place |
| `subscriptionId` | UUID | no | — | required when `source = subscription` | so a refresh can tell "my server disappeared" from "the user renamed it" |
| `createdAt` / `updatedAt` | date-time | no | — | `format: date-time` | bookkeeping; never part of a hash of intent |

## 4. The negative rules, and the bug each prevents

Nine `if/then` clauses at `packages/schema/server.schema.json#/allOf`. Each is listed with the failure it was
written for, because a rule without its reason will be "simplified away" in Phase 2.

| # | Rule | What it prevents |
|---|---|---|
| 1 | `flow = xtls-rprx-vision` ⇒ `network = tcp` **and** `security ∈ {tls, reality}` | a Vision-over-WS/XHTTP server whose config Xray accepts while silently ignoring `flow`: the user believes XTLS protects a connection that does not use it. Rejected outright (`packages/schema/examples/invalid/server.vision-over-ws.json`) |
| 2 | `security = tls` ⇒ `tls` present and `reality` absent | half-specified TLS; two security blocks claiming the same connection |
| 3 | `security = reality` ⇒ `reality` present, `tls` absent, `network ∈ {tcp, ws, xhttp, h2}` | REALITY over gRPC, which the pinned Core does not support — expressed as a schema rejection rather than a runtime surprise. Note this is the one place where the enum encodes Core capability, so it is re-checked on every Core bump (`docs/architecture/33-binary-integrity.md`) |
| 4 | `security = none` ⇒ no `tls`/`reality` block and `flow = ""` | "cleartext with a TLS block" — and `insecure: true` with `security: none` |
| 5–8 | `network = ws` ⇒ `ws`, `network = grpc` ⇒ `grpc`, `network = xhttp` ⇒ `xhttp`, `network = h2` ⇒ `h2` | a transport block that contradicts its container: today's silent behaviour is that the settings are ignored |
| 9 | `source = subscription` ⇒ `subscriptionId` present | an orphaned server a refresh cannot clean up, i.e. permanent accumulation |

`packages/schema/examples/invalid/` carries the matching must-reject cases — for instance
`packages/schema/examples/invalid/server.h2-without-tls.json`,
`packages/schema/examples/invalid/server.security-none-with-tls.json`,
`packages/schema/examples/invalid/server.inline-headers.json` — each paired with the keyword that must fire,
recorded in `packages/schema/examples/invalid/reasons.json`.

## 5. Where the secrets are, and where they are not

| Secret | Lives | Reference field | In any config byte written to disk? |
|---|---|---|---|
| VLESS user id | keystore | `packages/schema/server.schema.json#/properties/userIdRef` | yes, in the generated Core config, in-memory only for Xray's own reader — see the file-permission note below |
| gRPC / WS `:authorization` headers | keystore | `$defs/headersRef` — the two blocks that have the field; `xhttpSettings` and `h2Settings` have none | same |
| REALITY **private** key | never client-side | no field exists | not representable: `packages/schema/examples/invalid/server.reality-with-privatekey.json`. A client holding a server's private key is not a client |
| Subscription `Authorization` | keystore | `packages/schema/subscription-source.schema.json#/properties/headersRef` | never leaves the extension; this is exactly the SEC-1 field that legacy replicated into a Gist via `transformValueKeys` (`omega-target/src/options.coffee:27`) |

The compiled config necessarily contains the user id — that is what Xray requires. Therefore the config file is
written into the Connector's per-user directory with owner-only permissions, and `diagnostics.get` returns the
**masked** config only (`packages/schema/protocol/commands/config.snapshot.json#/result`). Asserted by the
`secretBearing ⇒ ¬syncEligible` rule in `packages/schema/scripts/verify.mjs`; the file-permission property
itself is `Verified: NO — deferred to Phase 2` (LIVE-18).

## 6. Tag derivation

`tag = "srv-" + id.slice(0,8)` of the UUID, i.e. `packages/schema/server.schema.json#/properties/tag` and its
`^srv-[0-9a-f]{8}$` pattern. Deterministic (ADR-0008's byte-stability), short enough for Core logs, and never
derived from `displayName` — a rename must not restart every tunnel. Uniqueness of the first eight hex
characters is asserted at compile time over the whole server set, and a collision is a compile error naming both
ids, not a silent suffix: `Verified: NO — deferred to Phase 2`.

The same pattern is reused for the compiler's output contract
(`packages/schema/protocol/commands/config.apply.json#/result/properties/serverTags`), so the extension can
verify that every tag it asked for exists in the applied config.

## 7. What `Server` deliberately is not

- **Not multi-protocol yet.** `protocol` is `const "vless"`
  (`packages/schema/server.schema.json#/properties/protocol`). Legacy subscriptions full of Shadowsocks,
  Trojan or WireGuard are therefore *lossy by construction*, and the compatibility matrix must say so out loud
  rather than let the parser quietly skip entries: `docs/architecture/29-subscriptions-and-servers.md` §loss
  table and `docs/architecture/38-compatibility-matrix.md`. Recorded as an open decision in
  `docs/architecture/40-open-decisions.md`.
- **Not an OmegaPac profile.** No `profileType` field, no `+` key, never in `Profiles.each`.
- **No per-server routing knobs** (`domainStrategy`, `bufferSize`, mux settings, `maxHops`): those belong to the
  policy or to Core's own outbound settings, and putting them here would make them per-endpoint configuration
  drift. Adding any of them requires an ADR.
- **No `enabled` flag**: disabling a server is a profile-level action (`serverIds` ordering), so the "why is this
  server not used" question has exactly one answer.

## 8. `ConnectorProfile` — field by field

Required is `["id","displayName","kind","serverIds","selection","failover","failurePolicy"]`
(`packages/schema/connector-profile.schema.json#/required`).

| Field | Type | Req | Default | Constraints | Notes |
|---|---|---|---|---|---|
| `id` | UUID | yes | — | — | envelope key `-nexora.profile.<id>` |
| `displayName` | string | yes | — | 1–80 | the *selectable* name shown to the user |
| `kind` | const `"connector"` | yes | — | `#/properties/kind` | discriminates from legacy profile types in tooling |
| `serverIds` | UUID[] | yes | — | 1–512, unique | ordering is meaningful for `mode: ordered` |
| `defaultServerId` | UUID | conditional | — | required when `selection.mode = fixed` (`#/allOf`) | matches legacy `defaultProfileName` semantics of "fallback must exist" |
| `selection` | object | yes | — | `mode ∈ {fixed, ordered, fastest, roundRobin}`, `stickySeconds` 0–86400 (default 300) | `stickySeconds` exists because a single slow probe must not flip the active server mid-session |
| `failover` | object | yes | — | `enabled`, `cooldownSeconds` 5–3600 (60), `maxConsecutiveFailures` 1–20 (3), `probeIntervalSeconds` 10–3600 (120) | the HealthProber's rate, not the user's patience |
| `failurePolicy` | `$ref` → `ProfilePolicy` | yes | — | `packages/schema/failure-policy.schema.json#/$defs/ProfilePolicy` | per-profile override of the global policy; `failOpen` requires `confirmToken` |
| `routingPolicyId` | UUID | no | — | — | pointer into `docs/architecture/28-routing-model.md`; the browser plane stays in the legacy `SwitchProfile` |
| `enabled` | boolean | no | `true` | — | soft off without deleting |
| `createdAt` / `updatedAt` | date-time | no | — | — | not part of intent hash |

The bridge to the legacy engine is a *registered handler*, not a new storage format: `Profiles.register` gains a
`ConnectorProfile` type whose `compile` emits `SOCKS5 127.0.0.1:<port>` / `PROXY 127.0.0.1:<port>` for the
allocated inbound of the selected server — the same string shape legacy produces today at
`omega-pac/src/profiles.coffee:82-89`. That single fact is what lets existing user rules keep working after the
cut-over (ADR-0013) and is specified in `docs/architecture/24-options-envelope-compat.md` §5.

## 9. The three canonical compiled shapes

Below: what the compiler must emit for `packages/schema/examples/server.vision-tcp-tls.json`,
`packages/schema/examples/server.reality-xhttp.json` and `packages/schema/examples/server.grpc-tls.json`.
These are illustrative here and **normative in `connector/testdata/config/*.golden.json` in Phase 2** —
`Verified: NO — deferred to Phase 2` for byte-exactness (ADR-0020's golden-config rule is the check). Marked
`⟨secret⟩` fields are materialized by the Connector from the keystore at write time and appear in no extension
transit.

Two of the three are pinned by a golden file today — `connector/testdata/config/shape-a-vision-tcp-tls.golden.json`
and `shape-b-reality-xhttp.golden.json` hold the emitted bytes, compared byte-for-byte by
`connector/internal/compile/golden_test.go` — and shape C is refused rather than pinned, because its `headersRef` is
a handle for an *object* materialized by the key list in `headersKeys` (`ODEC-45`'s answer). Four byte mappings are now
implemented: every per-server inbound is SOCKS5; `direct` and `block` declare `freedom` and `blackhole` outbounds;
`sniffing` is an inbound sibling with `protocols` renamed to `destOverride`; routing `network` and `protocol`
remain arrays. The materialized goldens pass Xray 24.11.11's config test, not a remote-server handshake.
Remaining routing evidence is tracked separately; `docs/architecture/40-open-decisions.md` keeps the list as `ODEC-37`.

**A. VLESS + TCP + TLS + Vision**

```jsonc
{
  "log": { "loglevel": "warning", "access": "none", "dnsLog": false },
  "inbounds": [
    { "tag": "in-srv-3f2a1c9e", "port": 20101, "protocol": "socks",
      "listen": "127.0.0.1",
      "settings": { "auth": "noauth", "udp": true } }
  ],
  "outbounds": [
    { "tag": "srv-3f2a1c9e", "protocol": "vless",
      "settings": { "vnext": [ {
        "address": "se1.example.net", "port": 443,
        "users": [{ "id": "⟨secret: userIdRef keystore://server/3f2a1c9e/user-id⟩",
                    "encryption": "none", "flow": "xtls-rprx-vision" }]
      } ] },
      "streamSettings": {
        "network": "tcp", "security": "tls",
        "tlsSettings": { "serverName": "se1.example.net",
                          "alpn": ["h2", "http/1.1"], "fingerprint": "chrome" } } }
  ],
  "policy": { "levels": {} },
  "routing": { "domainStrategy": "AsIs", "rules": [
    { "inboundTag": ["in-srv-3f2a1c9e"], "outboundTag": "srv-3f2a1c9e" }
  ] }
}
```

**B. VLESS + REALITY + XHTTP**

```jsonc
{
  "inbounds": [
    { "tag": "in-srv-8b1d4e70", "port": 20102, "protocol": "socks",
      "listen": "127.0.0.1", "settings": { "auth": "noauth", "udp": true } }
  ],
  "outbounds": [
    { "tag": "srv-8b1d4e70", "protocol": "vless",
      "settings": { "vnext": [ {
        "address": "nl2.example.net", "port": 443,
        "users": [{ "id": "⟨secret⟩", "encryption": "none", "flow": "" }]
      } ] },
      "streamSettings": {
        "network": "xhttp", "security": "reality",
        "xhttpSettings": { "mode": "auto", "path": "/websocket" },
        "realitySettings": { "fingerprint": "chrome", "serverName": "www.microsoft.com",
                              "publicKey": "hv2uW3Yx7Z9aBcDeFgHiJkLmNoPqRsTuVwXyZ012345",
                              "shortId": "1a2b3c", "spiderX": "/" } } }
  ],
  "routing": { "domainStrategy": "AsIs", "rules": [
    { "inboundTag": ["in-srv-8b1d4e70"], "outboundTag": "srv-8b1d4e70" }
  ] }
}
```

`publicKey`/`shortId` are **not** secrets — they are the server's public parameters, and masking them would
make a REALITY handshake failure undiagnosable. Their field-level masking rules are in
`docs/architecture/32-secrets-keystore-masking.md`.

**C. VLESS + gRPC (multi-mode) + TLS**

```jsonc
{
  "inbounds": [
    { "tag": "in-srv-c7e5a2d1", "port": 20103, "protocol": "socks",
      "listen": "127.0.0.1", "settings": { "auth": "noauth", "udp": true } }
  ],
  "outbounds": [
    { "tag": "srv-c7e5a2d1", "protocol": "vless",
      "settings": { "vnext": [ { "address": "de1.example.net", "port": 443,
        "users": [{ "id": "⟨secret⟩", "encryption": "none" }] } ] },
      "streamSettings": {
        "network": "grpc", "security": "tls",
        "tlsSettings": { "serverName": "de1.example.net",
                          "alpn": ["h3", "h2", "http/1.1"], "fingerprint": "firefox" },
        "grpcSettings": { "serviceName": "kernel-video", "multiMode": true,
                          "authority": "de1.example.net",
                          "headers": { ":authorization": "⟨secret: headersRef⟩" } } } }
  ],
  "routing": { "domainStrategy": "AsIs", "rules": [
    { "inboundTag": ["in-srv-c7e5a2d1"], "outboundTag": "srv-c7e5a2d1" }
  ] }
}
```

One inbound per server tag, ports from the fixed range described in
`docs/architecture/27-config-compilation-pipeline.md`, always on `127.0.0.1` (ADR-0006). The alternative —
one inbound with internal routing — is considered and rejected there, not here.

## 10. Invariants of this model and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| A `Server` cannot contain a credential value | `#/properties/userIdRef`, `$defs/headersRef`, `#/properties/reality` patterns + `packages/schema/examples/invalid/server.reality-with-privatekey.json` | **GREEN** |
| Vision over WS/XHTTP is unrepresentable | the first `allOf` clause + the must-reject example | **GREEN** |
| `additionalProperties: false` everywhere | declared on the schema and every `$defs` block; ajv compile is part of verify | **GREEN** |
| Every field has a description | the undocumented-field coverage check in `packages/schema/scripts/verify.mjs` | **GREEN** |
| `h2` implies `tls` | `allOf` clause 8 + `packages/schema/examples/invalid/server.h2-without-tls.json` | **GREEN** |
| A subscription server names its subscription | `allOf` clause 9 | **GREEN** |
| The three canonical configs compile byte-for-byte as shown | two of three: `connector/testdata/config/shape-a-vision-tcp-tls.golden.json` and `shape-b-reality-xhttp.golden.json`, compared by `connector/internal/compile/golden_test.go`; shape C is refused at its `headersRef` (`ODEC-45`) and `ODEC-37` tracks the remaining predicate semantics beyond the measured core-plane fixture | Verified: NO — deferred to Phase 2 (`connector/testdata/config/*.golden.json`) |
| Tag collision across the server set is a compile error | `connector/internal/compile/resolve.go` refuses a second server whose derived tag is taken, naming both ids — `TestATagCollisionNamesBothServers` | Verified: NO — deferred to Phase 2 |
| The generated config file is owner-only on disk | nothing | Verified: NO — LIVE-18 |
| A non-VLESS server in a legacy subscription is reported, not dropped | nothing (parser does not exist) | Verified: NO — `LIVE-14` carries the import; `docs/architecture/39-verification-matrix.md` will own the row |
| `protocol` enum expansion requires an ADR | `connector/internal/compile/outbound.go` refuses any protocol other than `vless` before it emits | Verified: NO — process rule, `docs/adr/README.md` |

Trace: ADR-0003, ADR-0006, ADR-0008, ADR-0010, ADR-0012, ADR-0013, ADR-0019 · Contract:
`packages/schema/server.schema.json`, `packages/schema/connector-profile.schema.json`,
`packages/schema/failure-policy.schema.json`, `packages/schema/subscription-source.schema.json`,
`packages/schema/examples/` · See also: `docs/architecture/24-options-envelope-compat.md`,
`docs/architecture/27-config-compilation-pipeline.md`, `docs/architecture/28-routing-model.md`,
`docs/architecture/29-subscriptions-and-servers.md`, `docs/architecture/32-secrets-keystore-masking.md`,
`docs/audit/04-data-model-and-options-envelope.md`.
