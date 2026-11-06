# 29 — Subscriptions and Servers

> Purpose: decide who fetches a subscription and who parses it, and then be exact about the four input formats —
> what each one carries, what each one cannot carry, and what the parser MUST do with the difference. A
> subscription is the only place in this design where a remote party writes to the user's configuration, so the
> guards, the identity rules and the keep-last-good policy are part of the contract, not implementation detail.

Status: Accepted, with §5's field mappings marked unverified until a real feed is parsed (LIVE-06). This document
forced five contract amendments; they are listed in §12.

---

## 1. What a subscription is, and what it is not

A `SubscriptionSource` (`packages/schema/subscription-source.schema.json`) is a feed that yields **Servers**. It is
not a `RuleListProfile`, and it is not a profile at all: `Profiles.each` iterates keys whose first character is
`+` (`omega-pac/src/profiles.coffee:111-118`), so a subscription stored the legacy way would appear in the
quick-switch list and in generated PAC. NEXORA sources live under `-nexora.subscription.<id>` and their servers
under `-nexora.server.<id>` (`packages/schema/examples/envelope-keys.nexora.json:29`, ADR-0012,
`docs/architecture/24-options-envelope-compat.md` §3).

The legacy subscription mechanism is not removed. A ZeroOmega `RuleListProfile` with a `sourceUrl` keeps fetching
and compiling through the untouched CoffeeScript path (`docs/audit/09-subscriptions-import-export-sync.md` §1), and
that is the fallback for feeds whose behaviour NEXORA's Connector-side fetch cannot reproduce (§2, cost 3).

## 2. The ownership decision: the Connector fetches and parses

**Chosen:** the extension owns the source documents, the schedule, and the resulting entities. The Connector owns
the HTTP request, the guards, the format detection, the parse, and the keystore writes.

**Evidence, not taste.** The command surface has no `keystore.put`: `packages/schema/protocol/command-allowlist.json#/commands`
lists 19 methods, and the only keystore-touching pair is `keystore.purge` (delete) and the write inside
`subscription.refresh`/`profile.import` side effects. A feed body is a stream of user UUIDs and often a token. If
the extension fetched it, the only way those values could reach the keystore would be to send them back over the
pipe — and any pause in that transfer (MV3 tears a worker down after ~30 s idle, and a 20 s fetch plus a 2 MB parse
is longer than that) would have to park secret material in `chrome.storage`. `importStaging` exists for that
reason and expires in an hour precisely because it is a liability
(`packages/schema/import-staging.schema.json#/properties/expiresAt`). Having the Connector fetch removes the
transfer instead of scheduling it.

**What it costs**, stated so the reader can argue:

1. **No browser cookie jar and no challenge sessions.** A feed behind a JavaScript challenge or a login cookie
   that the browser already holds will return an interstitial page. The parser treats that as `SchemaMismatch`, not
   as content. Mitigation: `headersRef` carries a pasted `Cookie`/`Authorization` value as a keystore reference
   (`packages/schema/subscription-source.schema.json#/properties/headersRef`); a *session* is not reproducible.
2. **No automatic use of the system or browser proxy for the feed request.** The Connector's HTTP client goes out
   directly. This is deliberate for the common case — a feed must stay reachable when the Core is dead, which is
   exactly when a refresh is needed to fix it — but a feed only reachable *through* another proxy needs the
   `fetchProxy` field §13 records as open.
3. **Legacy `RuleListProfile` subscriptions stay the escape hatch**, and they keep working untouched (ADR-0002).
   For the feeds above, the user keeps the profile they already have; NEXORA does not delete the path that works.

**Consequence for the frame contract.** The feed body never crosses the pipe in the extension-to-host direction, so
chunking is not a subscription concern: `protocol/envelope.schema.json#/properties/chunk` serves `profile.import`,
whose args do exceed 1 MiB. The reverse direction is capped at 1 GiB
(`packages/schema/protocol/framing.json#/limits/hostToExtensionBytes`), and 4 096 Servers are far below it, so
`subscription.refresh` results are single-frame.

## 3. The guards, and where each one runs

| Guard | Value | Enforced by | Why it is here and not in the browser |
|---|---|---|---|
| Scheme | `^https://`, no userinfo | schema (`packages/schema/subscription-source.schema.json#/properties/url`), then re-checked after every redirect | `http://` lets anyone with a path rewrite the user's egress; the negative cases are `examples/invalid/subscription.http-url.json` and `examples/invalid/subscription.url-userinfo.json` |
| Credential in the query | forbidden in `url`; goes to `urlRef` | schema `#/allOf/0`, demonstrated by `examples/invalid/subscription.url-token.json` | §4 |
| Size | `limits.maxBytes` ≤ 8 MiB, default 2 MiB | Go: `io.LimitReader(maxBytes+1)`, abort at the limit | the legacy path has no cap at all — it calls `response.text()` (`omega-target-chromium-extension/src/module/fetch_url.coffee:11-13`), which is `docs/audit/09-subscriptions-import-export-sync.md` M3 |
| Redirects | `limits.maxRedirects` ≤ 5, default 3 | Go: counted, and the address guard below re-runs **for every hop** | a redirect is how an allowed host reaches `169.254.169.254` |
| Time | `limits.timeoutMs` ≤ 120 000, and the frame deadline is 120 000 | Go context | the legacy timeout is absent; the deadline rule is `docs/architecture/25-native-messaging-protocol.md` §8 |
| Address family | refused: `0.0.0.0`, `::`, `127.0.0.0/8`, `::1`, `169.254.0.0/16`, and the cloud metadata literals | Go, after resolution and **before** dialling, dialling the validated IP | SSRF-2 in `docs/audit/11-security-findings.md`; a browser `fetch()` resolves internally, so the check and the connection cannot be made about the same address |
| Content type | must not be `text/html` or `application/xhtml+xml` | Go, on the response header | the legacy adjudication reads `response.headers['content-type']` off a `Headers` object, which is `undefined`, and its hint list ends in the literal `'*'` that accepts anything (`omega-pac/src/profiles.coffee:493-498`, `omega-target-chromium-extension/src/module/fetch_url.coffee:26-33`) |
| Server count | `limits.maxServers` ≤ 4 096 | Go, at parse time, before anything is returned | a 40 MB "subscription" that parses is still a DoS of the UI |
| Body well-formedness | parse to ≥ 1 Server or fail | Go | an empty list is a **failure**, not a successful delete: see §9 |

Private ranges (`10/8`, `172.16/12`, `192.168/16`, `fc00::/7`) are **not** refused for a feed address. A home-run
panel is a legitimate feed host, and refusing it would break the users who self-host. The fail-closed boundary that
matters is the *inbound* side, which is loopback-only by ADR-0006 (`docs/architecture/27-config-compilation-pipeline.md` §4).

## 4. The feed address is a credential some of the time

`url` is required and sync-eligible, and subscription feeds routinely ship `?token=` or `?key=`. That is SEC-1
(`docs/audit/11-security-findings.md`) in a different field: the `Authorization` header got a schema field of its
own and the query string got none. So:

- `url` MUST NOT carry a credential-shaped parameter. It is a `not.pattern` in the schema
  (`packages/schema/subscription-source.schema.json#/allOf/0`) with the first-letter case classes spelled out,
  because JSON Schema patterns are case-sensitive; the Connector re-checks case-insensitively before the request
  leaves. The rejected case is `examples/invalid/subscription.url-token.json`.
- `urlRef` (`packages/schema/subscription-source.schema.json#/properties/urlRef`) holds the **full** address in the
  keystore, and `url` then holds the display form: scheme, host, path, query dropped. The valid example is
  `packages/schema/examples/subscription.tokenised.json`.
- `url` stays `required`, because the UI and sync need *something* to show and to carry, and a dropped query is a
  legible label rather than a hole.

**Trade-off that must not be glossed over:** `keystore://` references are inert on a second machine
(`packages/schema/examples/envelope-keys.nexora.json:12`), so a tokenised source syncs as a display URL and fails
`KeystoreUnavailable` on the new machine until the user re-pastes the address. That is the same behaviour
`headersRef` already has, it is the honest consequence of not syncing secrets, and it is a `PARTIAL` row rather
than a silent gap: the UI must name the missing reference rather than report a network error.

## 5. Formats and the loss tables

`format` is declared per source, not sniffed (`packages/schema/subscription-source.schema.json#/properties/format`).
`auto` is allowed only when **exactly one** parser's detector answers true. The legacy loader loops every format
and keeps the last detector that did not say `false` (`omega-pac/src/profiles.coffee:504-511`) — a guess, in code
that writes to the network. NEXORA's rule: two detectors true ⇒ `SchemaMismatch` naming both, and the UI asks the
user to declare the format. The fifth enum value, `auto`, never survives into a stored source, because the parser's
verdict has a home: every `refreshed[]` item carries `format`, the value the detector resolved
(`packages/schema/protocol/commands/subscription.refresh.json#/result/properties/refreshed/items/properties/format`,
whose enum is the source schema's minus `auto`), and the extension persists it into the source it owns
(`docs/architecture/34-data-split.md` §6) — the one field the result carries back, which its args' "not a mutated
source" description states as the exception it is.

Every table below uses the same four statuses.

| Status | Meaning | UI obligation |
|---|---|---|
| **mapped** | the field has a NEXORA home | none |
| **refused** | the field means something NEXORA forbids; the *entry* is rejected, not silently repaired | show the entry and the reason |
| **dropped** | no home exists; the entry imports and the field is lost | `lossyFields` names it, once per source per refresh |
| **unmodelled** | no home exists and the semantics differ by producer | refused rather than dropped, because guessing changes transport |

### 5.1 `vless-uri-list` — the URI is the unit

Grammar: `vless://<uuid>@<address>:<port>?<query>#<fragment>`; one endpoint per line, blank lines and `#`-only
comment lines ignored.

| Input token | Target | Status | Note |
|---|---|---|---|
| `<uuid>` | `userIdRef` (value → keystore) | mapped | the only secret in a VLESS URI |
| `<address>`, `<port>` | `address`, `port` | mapped | no scheme, no userinfo, no port inside `address` (`packages/schema/server.schema.json#/properties/address`) |
| `#fragment` | `displayName` | mapped | percent-decoded, truncated to 80; never an identifier (`#/properties/displayName`) |
| `type` | `network` | mapped | `tcp`/`ws`/`grpc`/`xhttp`/`h2` |
| `security` | `security` | mapped | `none`/`tls`/`reality`; any other value is refused |
| `flow` | `flow` | mapped | only `xtls-rprx-vision`; `xtls-rprx-direct` and friends are refused, since `#/properties/flow` is a two-value enum |
| `encryption` | — | dropped | VLESS uses `none`; `auto` is a legacy VMess habit and carries no meaning |
| `sni` | `tls.serverName` / `reality.serverName` | mapped | |
| `alpn` | `tls.alpn` | mapped | comma list → array of at most 3 (`#/$defs/tlsSettings/properties/alpn`) |
| `fp` | `tls.fingerprint` / `reality.fingerprint` | mapped | not a secret |
| `pbk` | `reality.publicKey` | mapped | |
| `sid` | `reality.shortId` | mapped | |
| `spx` | `reality.spiderX` | mapped | |
| `path` | `ws.path` / `xhttp.path` / `h2.path` | mapped | |
| `host` | `ws.host` / `xhttp.host` / `h2.host` | mapped | the HTTP `Host` header — **not** SNI; feeding it to `serverName` is the classic wrong map |
| `mode` | `xhttp.mode` | unmodelled when `type=grpc` | some producers write `mode=multi` meaning gRPC multi-mode, others write `multiMode=true`. Both present and disagreeing ⇒ refused |
| `serviceName`, `multiMode`, `authority` | `grpc.*` | mapped | |
| `headers` (JSON object) | `ws.headersRef` | mapped | inline header maps are the SEC-1 shape: `examples/invalid/server.inline-headers.json` |
| `allowInsecure`, `insecure` | — | **refused** | `tls.insecure` is `const false` (`#/$defs/tlsSettings/properties/insecure`); an entry asking for it is rejected whole rather than quietly corrected |
| `minVersion`/`maxVersion`, `cert`, `pinCert` | — | refused | certificate pinning has no field yet (§13) |
| `xver`, `idletimeout`, `pingtimeout`, `uponly`, `downloadSpeed`, `uploadSpeed` | — | dropped | server-side or display-only |
| `tfo`, `udp`, `mptcp`, `reconnect` | — | dropped | Xray per-outbound socket options NEXORA does not expose |
| — | `id`, `tag`, `source`, `subscriptionId`, `createdAt`, `updatedAt` | assigned | produced by the parser and the refresh, not by the feed (§6) |

### 5.2 `vless-base64` — same grammar, wrapped

`base64(text)` where the decoded text is a `vless-uri-list`. Everything in §5.1 applies unchanged; the wrapper adds
three failure modes and no fields.

| Concern | Rule |
|---|---|
| Padding and alphabet | accept standard and URL-safe alphabets, with or without `=` padding; strict-mode failure is a `SchemaMismatch`, never a fallthrough to "treat it as a URI list" |
| Double wrapping | one unwrap only. A body that is still base64 after unwrapping is reported as suspicious and refused, because chained encoders hide content-type from the guard in §3 |
| Decoded-but-empty | failure, not an empty success (§9) |

### 5.3 `v2ray-json` — two payloads share one name

Detected by shape, and the shape decides the mapping: a JSON **object** with `outbounds` is an Xray configuration;
a JSON **array** (or an object with `servers`) is a v2rayNG/v2rayN share document.

Share-array fields:

| Input field | Target | Status | Note |
|---|---|---|---|
| `address`, `port` | `address`, `port` | mapped | |
| `protocol` | — | gate | `vless` proceeds; `vmess`, `trojan`, `shadowsocks`, `socks` are refused per-entry with the reason `protocol out of scope (ADR-0003)`. A feed of 40 VMess nodes yields 40 refusals, not 40 servers |
| `psk` | `userIdRef` | mapped | v2rayNG reuses this field for the VLESS id — a name that lies, which is why the `protocol` gate runs first |
| `id`, `user` | `userIdRef` | mapped | alternate names for the same value across producers |
| `obfs` | `security` | mapped | `none`/`""` → `none`, `tls` → `tls`, `reality` → `reality`; anything else refused |
| `obfsParam` | `tls.serverName` **or** `reality.publicKey` | unmodelled | the field holds the SNI in some builds and the public key in others. Refused under `auto`; mapped only when the source declares `v2ray-json` **and** `security=reality` and the value matches the 43-character base64url pattern (`packages/schema/server.schema.json#/$defs/realitySettings/properties/publicKey`) |
| `path` | `ws.path` | mapped | |
| `remarks`, `title`, `name` | `displayName` | mapped | first present wins, in that order; all three present is reported |
| `method`, `security(vmess)`, `level`, `alterId` | — | dropped | VMess-era; no VLESS meaning |
| `header.type`, `seed`, `congestion` | — | dropped | mKCP/quic are not modelled transports |
| `tags`, `localAddr`, `connTest`, `editResult` | — | dropped | app-local bookkeeping |

Xray-config-object fields:

| Input | Target | Status | Note |
|---|---|---|---|
| `outbounds[].protocol` | gate | `vless` only | as above |
| `outbounds[].settings.id` | `userIdRef` | mapped | |
| `outbounds[].streamSettings.network` | `network` | mapped | `mkcp`, `quic`, `domainsocket` refused |
| `outbounds[].streamSettings.security` | `security` | mapped | |
| `...tlsSettings.serverName/alpn/fingerprint` | `tls.*` | mapped | |
| `...tlsSettings.insecure`, `...settings.masterKey`, `clusterSize` | — | refused | |
| `...realitySettings.publicKey/shortId/spiderX/fingerprint/serverName` | `reality.*` | mapped | |
| `...realitySettings.privateKey` | — | **refused** | a client config that contains the server's private key is not a client; `examples/invalid/server.reality-with-privatekey.json` |
| `...wsSettings.path/headers` | `ws.*` | mapped | `headers` → `headersRef` |
| `...grpcSettings.serviceName/multiMode/authority` | `grpc.*` | mapped | |
| `...xhttpSettings.mode/path/host/extra` | `xhttp.*` | mapped | |
| `...sockopt` (`tcpFastOpen`, `tcpMPTCP`, `domainStrategy`, `mark`, `tproxy`) | — | dropped | per-outbound socket options; `domainStrategy` belongs to routing, which is `docs/architecture/28-routing-model.md` |
| `...mux.enabled` | — | dropped | |
| `inbounds[]`, `log`, `policy`, `stats`, `transport`, `dns`, `reverse` | — | dropped | provider-side artefacts shipped by mistake; reported with their names |
| `routing` (rules + `domainStrategy`) | — | **refused** | a feed that ships routing is a second opinion about the routing plane (ADR-0013). It is never merged into the user's RoutingPolicy, and never silently dropped either: the count is reported |

### 5.4 `clash-yaml`

Only the `proxies` sequence is read, and only entries of `type: vless`.

| Input field | Target | Status | Note |
|---|---|---|---|
| `name` | `displayName` | mapped | |
| `server` | `address` | mapped | a `hosts:` alias resolved by the YAML loader is fine; a value that only exists inside Clash (`*provider-anchor`) is refused as unresolvable |
| `port` | `port` | mapped | |
| `type` | gate | `vless` only | `ss`, `vmess`, `trojan`, `tuic`, `hysteria2`, `direct`, `reject`, `selector`/`url-test` groups are refused per-entry |
| `uuid` | `userIdRef` | mapped | |
| `tls: true/false` | `security` | mapped | `true` → `tls`, `false` → `none`; with `reality-opts` present, `security` becomes `reality` |
| `servername` | `tls.serverName` / `reality.serverName` | mapped | |
| `skip-cert-verify` | — | **refused** | the entry, not the file: `insecure` is `const false` |
| `alpn` | `tls.alpn` | mapped | values outside `h3`/`h2`/`http/1.1` are dropped with a report |
| `client-fingerprint` | `*.fingerprint` | mapped | |
| `flow` | `flow` | mapped | `xtls-rprx-vision-reload` and similar are refused |
| `network` | `network` | mapped | `ws`/`grpc`/`h2`; `http` means XHTTP in mihomo and is mapped to `xhttp` **only** when `xhttp-opts` or a `mode` is present, otherwise refused — an unqualified `network: http` is ambiguous by producer |
| `ws-opts.path`, `ws-opts.headers` | `ws.path`, `ws.headersRef`, `ws.host` | mapped | a `Host` header inside `headers` moves to `ws.host`; a `headers-opts` list is unmodelled |
| `grpc-opts.grpc-service-name` | `grpc.serviceName` | mapped | `grpc-auth` refused (no field) |
| `h2-opts.host/path` | `h2.*` | mapped | |
| `reality-opts.public-key/short-id` | `reality.publicKey/shortId` | mapped | |
| `xhttp-opts` / `mode` / `path` | `xhttp.*` | mapped | |
| `udp`, `tfo`, `ip-version`, `mtu`, `tcp-port`, `network`-level `packet-encoding` | — | dropped | socket/transport options Xray per-outbound config in NEXORA does not expose |
| `client-id`, `ech`, `gRPC version`, `reality-opts.handshake` | — | refused | newer than the pinned core (`packages/schema/examples/pins.xray.json#/upstream/releaseTag`): refusing is honest, mapping to nothing is not |
| `dialer-proxy`, `interface`, `bind-address`, `marker`, `routing-mark` | — | refused | proxy chaining and OS-level binding do not exist in this design |
| `mux`/`smux`/`yacn`/`vless-mux` | — | dropped | |
| `hide-ip`, `global-client-fingerprint` (root-level) | — | dropped | |
| `proxy-providers` (and any `use:` inside `proxies`) | — | **refused** | following a provider URL is a second fetch, with its own size and address guards, at unbounded depth. §10 |
| `proxy-groups` | — | refused | that is a *selection plane*, and the plane question is settled by `docs/architecture/28-routing-model.md` |
| `rules`, `rule-providers`, `domain-suffix` lists | — | refused | routing plane, same treatment as the `routing` key in §5.3 |
| `dns`, `hosts`, `general`, `profile`, `experimental`, `tun`, `log-*`, `*port`, `allow-lan` | — | dropped | listener and host configuration; `allow-lan` in particular is what ADR-0006 forbids |

A YAML file that fails to load is a `SchemaMismatch` for the whole refresh. It is never read as "zero servers".

## 6. Identity, duplicates, and what a refresh may touch

A refresh that mints new identifiers on every run destroys three things at once: the per-server health history,
`core.select` (which names a server, `packages/schema/protocol/commands/core.select.json`), and the derived Xray tag
`srv-<id8>` (`packages/schema/server.schema.json#/properties/tag`). So:

1. **The identity key is non-secret and transport-only**: `protocol, address, port, network, security, flow` plus
   the transport discriminants that select the outbound (`sni`, `publicKey`, `shortId`, `path`, `host`,
   `serviceName`, `mode`). Never `displayName` (renaming must not fork an entity) and never a hash of the user id
   (a digest over secret material would be a second place the secret exists).
2. **Id reuse is scoped to the source that produced it.** A key match inside this source's existing `serverIds`
   reuses the `id`, the `userIdRef`, the `tag` and the user's `displayName` and `notes`. A match *outside* it is a
   duplicate, not a merge: it becomes a second entity and is counted in `duplicates`
   (`packages/schema/protocol/commands/subscription.refresh.json#/result`), because deleting one source must never
   delete another source's server.
3. **Two entries with the same key and different user ids are two servers.** The tie-break is a keystore-value
   comparison inside Go, and if that cannot distinguish them the parser keeps both, in body order, and reports the
   ambiguity rather than picking.
4. **The feed does not own membership.** A refresh may create, update and mark-gone servers; it may not reorder
   `ConnectorProfile.serverIds` (that order is the failover ranking,
   `packages/schema/connector-profile.schema.json#/properties/serverIds`) and may not add its own servers to a
   profile. Enlarging a profile from a feed is a user action in the UI.
5. **Gone servers are marked, not deleted.** A server absent from a successful parse loses its place in
   `serverIds` and is reported in `removed`, but the entity stays until either the user deletes it or the source is
   deleted, because a partial parse that auto-deletes is the master prompt's forbidden outcome. The reference check
   in `profile.remove` is what keeps a marked-gone server from being dropped out from under a profile.

## 7. Durable status and the schedule

The legacy path computes this information and throws it away: `updateProfile` writes `lastUpdate` and nothing else,
and the error object dies with the alert that displayed it
(`docs/audit/09-subscriptions-import-export-sync.md` §2 M1). NEXORA's `status` is persisted, in the extension's
envelope, written from the command result:

| Field | Written from | Read by |
|---|---|---|
| `lastAttemptAt` | every result, including `unchanged` and `skipped` | the list row "tried N min ago" |
| `lastSuccessAt` | `refreshed[]` only | "updated 3 days ago" |
| `lastSha256` | `refreshed[].sha256` | the next call's `unchanged` verdict (§2 of the result contract) |
| `serverCount` | `refreshed[].serverCount` | the list row |
| `format` | `refreshed[].format` — the resolved value, which replaces the source's `auto` declaration (§5) | the stored source, and the list row's "parsed as vless-uri-list" |
| `consecutiveFailures` | `failed[].consecutiveFailures` — the Connector counts, the extension stores | the backoff below, and the UI badge |
| `nextAttemptAt` | computed: `interval × min(multiplier^(failures-1), ·)` capped at `maxIntervalSeconds`, then ± `jitterPercent` | the alarm reschedule and `force` |
| `lastError.code` | `failed[].code`, drawn from a closed enum | the message; `detail` is masked like any other text (`docs/architecture/32-secrets-keystore-masking.md`) |

Mechanics that the MV3 lifecycle forces:

- **The alarm only signals.** `chrome.alarms` is the only timer that survives a worker teardown, so the schedule
  lives there and the state lives in the envelope; the handler is re-entrant and a concurrent call is answered
  `Busy`, not double-fetched. Legacy used one fixed `periodInMinutes` with no backoff and no randomness
  (`omega-target-chromium-extension/src/module/options.coffee:295-297`), which is M2 in the audit.
- **Floors are in the schema, not in the handler**: `intervalSeconds ≥ 300`
  (`packages/schema/subscription-source.schema.json#/properties/schedule`) means a mistyped source cannot become a
  polling loop; `jitterPercent` defaults to 20 so a thousand users do not hit the same panel on the same second.
- **`force` bypasses backoff, not the rate limit.** The Connector budgets refreshes per sender
  (`docs/architecture/31-security-model.md`), which is what makes `force` safe from a UI defect.
- **Reconcile on connect.** After a reconnect the extension re-sends any source whose `nextAttemptAt` has passed;
  there is no separate subscription sync.

## 8. The caps have to agree with each other

| Stage | Ceiling | Source |
|---|---|---|
| Servers a feed may produce | `maxServers` ≤ 4 096, default 1 024 | `packages/schema/subscription-source.schema.json#/properties/limits` |
| Servers one refresh may return | 4 096 per source | `packages/schema/protocol/commands/subscription.refresh.json#/result` |
| Servers one import may carry | 512 | `packages/schema/protocol/commands/profile.import.json#/args/properties/servers` |
| Servers one profile may reference | 512 | `packages/schema/connector-profile.schema.json#/properties/serverIds` |
| Inbounds one Core config may have | 64 | `packages/schema/protocol/commands/core.start.json#/result/properties/inboundPorts` |

The ladder is not monotone: a 300-server feed imports fine and then fails to **compile**, because one inbound per
server tag is a decision with an evidence trail (`docs/architecture/27-config-compilation-pipeline.md` §5) and the
ceiling it collides with is `QuotaExceeded`, never a partial config. Three consequences, all of which belong to the
UI rather than to the parser:

1. The options page must warn when a profile's `serverIds.length` exceeds 64 **before** the user applies, not
   after (`docs/architecture/37-ui-architecture.md` §4 and §5).
2. "Import all servers" is not a default action; a profile gets a subset by user choice or by health ranking.
3. Whether the ceiling should rise is an open decision, not an implementation detail: §13.

## 9. Failure outcomes, and the two that are not deletions

`docs/audit/09-subscriptions-import-export-sync.md` §2 established that keep-last-good is already safe by
construction in the legacy code, and lists seven things that are genuinely *missing* rather than broken. This
document closes them as follows: M1 §7, M2 §7, M3-M5 §3, M7 §4 and
`docs/architecture/32-secrets-keystore-masking.md`; M6 (the legacy `?_=<timestamp>` cache-buster,
`omega-target-chromium-extension/src/module/fetch_url.coffee:36-40`) is closed by not reproducing it — NEXORA adds
no cache-busting parameter at all, and a feed that requires one is served by the legacy path.

| Outcome | Effect on the last good set | Effect on `status` |
|---|---|---|
| HTTP 404 / 403 / 429 | none | `failed[]` with that code; the source survives; only an explicit user delete removes it |
| HTTP 5xx, transport failure, timeout | none | `failed[]`, `consecutiveFailures++`, `nextAttemptAt` grows |
| Body over `maxBytes` | none | `QuotaExceeded`, `retryable: false` — a bigger cap is the only retry |
| Unparseable body | none | `SchemaMismatch`, and `lossyFields` names what was understood |
| Parse to zero servers | none | `SchemaMismatch`, not success: an empty list is a feed outage or an attack, never a user intent |
| Valid, hash equals `status.lastSha256` | none | `unchanged`; `lastAttemptAt` only |
| Valid, hash differs | replaced **after** the new entities are accepted as a set | `refreshed`, `consecutiveFailures = 0` |
| Partially valid (`maxServers` or per-entry refusals) | replaced with the accepted entries, `partialServers` in `failed[]` shows what was dropped | `QuotaExceeded` or `SchemaMismatch`, and the UI must show the dropped list |

## 10. Non-goals

- No provider recursion, no `proxy-providers`, no `use:` chains (§5.4).
- No conversion of a subscription into rule lists or PAC text. A feed is servers; a rule list is a policy; the
  legacy path keeps both.
- No import of routing material from a feed under any name (§5.3, §5.4) — the plane decision belongs to ADR-0013.
- No health-driven membership: probes rank servers within a profile; they never add or remove one
  (`docs/architecture/22-component-contracts.md` §8).
- No cache of feed bodies anywhere except the keystore entries the parse produced.
- No auto-rename of a server the user renamed, and no auto-edit of `notes`.

## 11. Invariants this document owns

| Invariant | Checked by | Status |
|---|---|---|
| A failed refresh leaves the last good Servers byte-identical and usable | `packages/schema/subscription-source.schema.json#/properties/keepLastGood` (`const true`) + `examples/invalid/subscription.keep-last-good-false.json`; the runtime half is `LIVE-12` | PARTIAL |
| An HTTP 404 never deletes a subscription | `packages/schema/protocol/commands/subscription.refresh.json#/invariants`; the UI half is `LIVE-12` | Verified: NO — Phase 2 |
| No credential-shaped query in a synced `url` | schema `packages/schema/subscription-source.schema.json#/allOf/0`, rejected case `packages/schema/examples/invalid/subscription.url-token.json`, accepted display form `packages/schema/examples/subscription.tokenised.json` | **Verified** — runs in `npm --prefix packages/schema run verify` |
| Every Server a refresh returns validates against the stored schema, so extraction ran | `packages/schema/protocol/commands/subscription.refresh.json#/result/properties/refreshed/items/properties/servers` (`$ref` to `server.schema.json`); the fixture pair `packages/schema/fixtures/protocol/probes-and-ops.jsonl` carries three real servers through it | **Verified** (schema half) — the keystore write is `LIVE-18` |
| The engine never fetches, and no feed body enters the browser process | prose rule; testable as a build-graph assertion (no `fetch(`/`XMLHttpRequest` under `nexora/extension/src/subscriptions/**`) | Verified: NO — deferred to Phase 2 |
| Guards are applied to every redirect hop | `docs/verification/live-matrix.md` LIVE row for redirect-to-metadata | Verified: NO — Phase 2 |
| `format: auto` never resolves ambiguously | `packages/schema/subscription-source.schema.json#/properties/format` declares the enum; the two-true-detectors case has no fixture | Verified: NO — Phase 2 |
| Id reuse across refreshes: `id`, `tag`, `userIdRef` stable, feed name does not overwrite a user rename | §6 rules, no checker yet | Verified: NO — Phase 2 |
| The cap ladder warns before applying | §8; the compile half is `docs/architecture/27-config-compilation-pipeline.md` §10 | Verified: NO — Phase 2 |

## 12. Amendments this document forced

Each was found by reading one artefact against another, not by running anything.

1. `urlRef` added, plus the `not.pattern` on `url` and its invalid example (§4): the sync-eligible `url` could
   otherwise ship `?token=` to the browser account, the Gist and WebDAV — SEC-1 with the field changed.
2. `subscription.refresh` args carry the `SubscriptionSource` documents (§2). As written they carried identifiers,
   which the Connector cannot resolve because it owns no copy of user intent — the command was unexecutable.
3. `subscription.refresh` result gained `refreshed[].servers`, `duplicates` and `failed[].partialServers` (§6, §9):
   without them the extension could not store what the Connector had parsed.
4. `subscription.refresh` side effects lost `entity-write`/`entity-delete` and gained `keystore-delete` (§2): the
   Connector does not write envelope entities; it does delete keystore entries for servers that disappear.
5. `chunked.import` moved from `subscription.refresh` to `profile.import` (§2), and the wording in
   `protocol/envelope.schema.json#/properties/chunk`, `protocol/commands/profile.import.json#/result/properties/secretsExtracted`
   and `import-staging.schema.json#/description` was corrected to match: a feed body is no longer something this
   pipe chunks. `secretsExtracted` now names the ingest path that satisfies it -- `profile.import`'s
   `credentials` array (`docs/architecture/32-secrets-keystore-masking.md` §6) -- and bars that field from chunked frames.

## 13. Open items

| # | Item | Where it goes |
|---|---|---|
| 1 | A fetch-time proxy for feeds reachable only through another proxy | `docs/architecture/40-open-decisions.md` — needs a schema field, an address guard for the proxy itself, and a decision about whether it may be a loopback proxy |
| 2 | Certificate pinning (`pinCert`, `minVersion`) has no Server field; feeds that ship it are refused | `docs/architecture/38-compatibility-matrix.md` |
| 3 | Should the 64-inbound ceiling rise, or should profiles compile a subset? | `docs/architecture/27-config-compilation-pipeline.md` §5 owns the topology; the number belongs to `docs/architecture/40-open-decisions.md` |
| 4 | `KeystoreUnavailable` on a second machine for `urlRef`/`headersRef` sources: which UI state names it | `docs/architecture/37-ui-architecture.md` §4 |
| 5 | Whether a subscription may ever be created by the Connector (import of a legacy profile with `sourceUrl`) or only by the user | `docs/architecture/35-schema-versioning-migration.md` §6 |
| 6 | Whether the Connector owns subscriptions end to end, or the extension may receive feed-derived Servers through chunked events -- §2's single-frame answer holds only while that is true; `Verified: NO — Phase 2` until someone answers | `docs/architecture/40-open-decisions.md`; the human half is `docs/PROJECT-STATE.md` §9's fourth decision |
