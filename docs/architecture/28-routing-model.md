# 28 — Two-plane routing model

> Purpose: which routing rules are evaluated where, why that split is not negotiable, how the twelve legacy
> condition types map into it, and what happens to a rule that fits neither plane cleanly.

Status: Accepted. Data: `packages/schema/routing-policy.schema.json` (`browserPlane` and `corePlane`),
`packages/schema/examples/routing.minimal.json`, `packages/schema/examples/routing.two-plane.json`.

## 1. Reading rules

As in `docs/architecture/20-target-repository-layout.md` §1.

## 2. The invariant

**Exactly one plane decides the fate of a request.** Not "usually", not "the more specific one wins": a design
in which two evaluators can each redirect traffic produces the class of bug where the user believes a rule is
active and neither plane is honouring it. The rule is mechanical:

- the **selection plane** decides *which Omega profile a request maps to* — and therefore which loopback host:port
  the browser uses. It runs in the browser, on a URL, before any byte leaves the machine.
- the **transport plane** decides *which already-loaded outbound inside Core carries a connection* — and it can
  only choose among what the compiler emitted. It runs in Core, on socket-level facts.

A rule therefore has exactly one home. `packages/schema/routing-policy.schema.json#/properties/browserPlane/description`
says this in the file itself, and `#/properties/corePlane/description` states the other half: the core plane
"cannot send traffic outside the Core, and it never sees a URL."

## 3. Plane capabilities and limits

| | Selection plane (browser) | Transport plane (Core) |
|---|---|---|
| Evaluated by | `OmegaPac.Conditions` (`docs/audit/07-routing-engine.md`), via PAC on Chromium and via the request listener where PAC cannot express it | Xray `routing.rules` |
| Sees | full URL, scheme, host, port, protocol-family keywords, time of day, weekday, request initiator context where the browser exposes it | host (or sniffed host), resolved IP, port, network tcp/udp, sniffed protocol |
| Does not see | resolved IP for a *hostname* rule at PAC time, connection timing, TLS SNI beyond the URL | the URL path, the user's profile names, anything not on the wire |
| Output | a profile name → a PAC/`chrome.proxy` result | an outbound tag |
| Latency | per request, in-process | per connection, post-handshake |
| Can it be bypassed by the browser? | no — it is the browser | yes: if the selection plane said DIRECT, Core never sees the connection |

The last row is the one that decides most placements: **a rule whose purpose is "this must not go direct" belongs
in the selection plane**, because only there is its evaluation guaranteed. Putting it in `corePlane` and hoping
the browser also routed the traffic to Core is a rule that fails open.

## 4. The twelve condition types and their home

`packages/schema/routing-policy.schema.json#/properties/browserPlane` carries conditions verbatim from the
legacy registry — all twelve `conditionType` names, in the eight `oneOf` branches of
`packages/schema/routing-policy.schema.json#/$defs/condition`, with field names taken from each handler's own
code so that a NEXORA document round-trips through `Conditions.fromStr`/`toStr` unchanged (ADR-0013).

| Condition | Plane | Compiled into core plane too? | Notes |
|---|---|---|---|
| `TrueCondition` | selection | n/a | the default branch; equivalent to `defaultProfileName` |
| `FalseCondition` | selection | no | exists for round-trip fidelity, never authored by us |
| `UrlWildcardCondition`, `UrlRegexCondition` | selection | **sometimes** | a URL pattern with no path component can be lowered to a `domain` rule; anything with a path/query stays browser-only, and lowering is reported not silent |
| `HostWildcardCondition`, `HostRegexCondition` | selection | **yes** | → `coreRule.domain` (`#/$defs/coreRule/properties/domain`) as a `domain:` list entry; the wildcard/regex syntax differs, so lowering is *only* for the forms Xray's `domain` field accepts verbatim (`full::`, `prefix:`, `regexp:`) |
| `KeywordCondition` | selection | no | matches host-or-URL in legacy semantics; Xray has no equivalent ambiguity |
| `BypassCondition` | selection | **partially** | see §5 |
| `IpCondition` (`ip` + `prefixLength`, 0–128) | selection | **yes** | → `coreRule.ip` CIDR; note it also applies in the browser plane for IP-literal URLs |
| `HostLevelsCondition` (`minValue`/`maxValue` 1–127) | selection | no | label-count matching has no Xray equivalent |
| `WeekdayCondition` (7-letter `days`) | selection | no | Core has no calendar |
| `TimeCondition` (`minValue`/`maxValue` minutes) | selection | no | same reason; also a timezone question the Core would answer differently from the browser |

Anything not lowered to the core plane is evaluated in the browser only, and if the compiler *drops* it, the
drop is reported (`docs/architecture/27-config-compilation-pipeline.md` §7). The count of representable types
today: 4 of 12 can be lowered at all, 2 partially.

## 5. `BypassCondition`, and what `<local>` means twice

Legacy `BypassCondition` is a Switchy-style bypass list: `<local>`, `<pac>`, `<proxy>`, hostname wildcards, CIDR
literals and `host:port` forms — see `docs/audit/07-routing-engine.md`. Each piece has a different home:

| Bypass form | Selection plane | Transport plane | Why |
|---|---|---|---|
| `<local>` | yes — the PAC result is DIRECT | partially | "local" in the browser means "resolvable without a proxy / in the private ranges"; Core can only match IP CIDRs, so the mapping is a *conservative expansion* into RFC1918/loopback/link-local + `.local` suffix — an approximation the user must be told about |
| hostname wildcard | yes | yes, as `domain` | |
| bare CIDR | yes | yes, as `ip` | |
| `host:port` | yes | as `domain` + `port` (`#/$defs/coreRule/properties/port`, pattern `^\d+(-\d+)?$`) | |
| `<pac>` / `<proxy>` | selection only | no | these name *browser* concepts; a core rule cannot express "as configured by PAC" |

This is the single most misunderstood rule in proxy extensions, so the UI contract is explicit:
`docs/architecture/37-ui-architecture.md` §4 renders the expansion `<local>` actually produced, not the token.

## 6. The three builtins, and the fourth that is not a profile

| Name | Selection plane | Transport plane | Fail-closed meaning |
|---|---|---|---|
| `+direct` | a real OmegaPac builtin: PAC returns `DIRECT` | the `direct` outbound tag (`#/$defs/coreOutbound` allows `direct`) | **must never be the result of a Connector error** (ADR-0009) |
| `+system` | builtin: fall through to system proxy settings | no equivalent | also forbidden as an error outcome, for the same reason: it is an unmonitored egress path |
| `+block` | **not inherited** — a NEXORA builtin: this tree ships exactly `+direct` and `+system` (`omega-pac/src/profiles.coffee:34-44`) | the `block` outbound tag | the intended error outcome; in the browser plane it is a closed loopback port (`docs/architecture/30-failure-policy.md` §7) |
| `-nexora.server.<id>` | **not a profile** (`docs/architecture/24-options-envelope-compat.md` §3) | becomes `srv-<8 hex>` | a rule that named a server directly is a bug: rules name profiles, profiles name servers |

`#/$defs/coreOutbound` is `^(direct|block|srv-[0-9a-f]{8})$` by design: a policy cannot name an outbound the
compiler did not emit, which closes the "hand-edited config references a tag that does not exist" class of bug.
The browser-plane counterpart is `#/$defs/profileName` — pattern `^(?!\+).+$` — because a rule that stores
`'+direct'` instead of `'direct'` resolves to a profile literally named `+direct`, silently stops matching, and
turns a routing rule into a no-op; the legacy field it mirrors is `omega-pac/src/profiles.coffee:405`. This is
asserted by the key-pattern check over the routing examples in `packages/schema/scripts/verify.mjs`
(`packages/schema/examples/invalid/routing.prefixed-profilename.json`).

## 7. Priority, stated as a table rather than as prose

| Rank | Rule | Effect |
|---|---|---|
| 0 | Connector absent / Core not running and policy says block | the block target is written; nothing else is evaluated |
| 1 | temp rules (`-tempRuleList`, `chrome.storage.session`) | override stored rules until cleared (`docs/audit/05-storage-sync-and-migration.md`) |
| 2 | the selected profile's own rules, first match wins (`SwitchProfile.match` over `cache.analyzed`) | the classic legacy behaviour, unchanged |
| 3 | `defaultProfileName` of that profile | the fallback branch |
| 4 | core-plane rules inside the chosen server's inbound | only reached because ranks 0–3 sent the connection to Core |
| 5 | Core's own default outbound | the compiler emits an explicit `outboundTag` for every policy, so rank 5 should be unreachable; if it is reached, the diagnostics event says so (`docs/architecture/36-observability.md`) |

The rank-4/5 ordering is the practical consequence of §2: a core rule can only refine a decision the browser
already made, never reverse it. A core rule that *would* reverse it — e.g. "traffic to 1.2.3.4 must never use
srv-abcd1234" — is expressible in the model (`invert: true`, `#/$defs/coreRule/properties/invert`) but cannot
be passed through: Xray 24.11.11 ignores that field. The compiler reports the entire inverted rule as
unrepresentable; it does not emit the opposite match. The reason is now stated from the pinned implementation
rather than a fixture: Xray 24.11.11's rule parser accepts eleven condition keys and no negation among them, drops
an unknown key while decoding, and ANDs every condition it builds — so there is no equivalent lowering to derive,
and only a port-only inversion has a complement the language can spell, because the `port` string splits on commas
into ranges (`NOT 8000-9000` becomes `"port": "0-7999,9001-65535"` in one rule); a domain or CIDR complement has no
such spelling. A rule containing `protocol: [ws]`, including a mixed protocol array, is likewise reported whole: Xray classifies plaintext WebSocket Upgrade as HTTP, so replacing `ws` with `http` would broaden the match to ordinary HTTP. This restriction is on routing predicates, not the server's WebSocket transport.
A rule containing `protocol: [fakedns]` is reported whole for a reason inside this compiler's own output: Xray adds the fakedns sniffer only when a FakeDNS engine is registered, and the compilation pipeline emits no `fakedns` section, so that predicate could only ever miss the config it is handed.
Cross-plane contradiction reporting in the browser is still separate work, not something that Core's config parser certifies.

## 8. What the compiler may and may not do

| May | May not |
|---|---|
| lower a host wildcard to `domain` | invent an outbound tag |
| expand `<local>` into explicit CIDRs | expand `<local>` silently — the expansion is shown to the user |
| emit `routing.domainStrategy` from the policy | decide a URL-level rule inside Core |
| report `unrepresentable[]` | drop a rule without reporting |
| reorder core rules for matching efficiency | reorder browser rules (first-match semantics are the user's mental model) |

## 9. Invariants and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| Browser rules carry bare profile names, never `+`-prefixed | `#/$defs/profileName` pattern + `packages/schema/examples/invalid/routing.prefixed-profilename.json` | **GREEN** |
| Core rules may name only `direct`, `block` or a derived `srv-` tag | `#/$defs/coreOutbound` pattern + `packages/schema/examples/invalid/routing.invented-outbound.json` | **GREEN** |
| All twelve legacy condition types are representable in the document | the eight `oneOf` branches of `#/$defs/condition` + `packages/schema/examples/invalid/routing.unknown-condition.json` | **GREEN** |
| A core rule must say something to match on | `#/$defs/coreRule/minProperties: 2` with `outbound` required — a rule with only an outbound is a default, not a rule | **GREEN** |
| `protocol` values are ones Xray accepts | the enum in `#/$defs/coreRule/properties/protocol` (`http`, `tls`, `bittorrent`, `quic`, `fakedns`, `ws`) | **GREEN** |
| `network` ∈ {tcp, udp}, ≤ 2 items | same object | **GREEN** |
| Nothing invented a 13th condition type | `packages/schema/scripts/verify.mjs` checks the registry count against `docs/audit/07-routing-engine.md`'s twelve | **GREEN** |
| Every browser rule maps to a profile that exists | JSON Schema cannot express cross-key referential integrity: the `+`-keyed profile is a sibling envelope key, invisible to this schema. Owned by ProfileRegistry (`docs/architecture/22-component-contracts.md` §6), implemented at load: `nexora/extension/src/profiles/registry.ts` refuses a profile whose serverIds name no `-nexora.server.*` key (`VER-31` retired with it) | the Firefox half stays `LIVE-15` |
| Lowering a rule never changes its meaning | nothing | Verified: NO — deferred to Phase 2 (golden configs compare the *un*lowered browser rules too) |
| `<local>` expansion is shown in the UI | nothing | Verified: NO — deferred to Phase 2 |
| Rank 5 is unreachable in a compiled config | nothing | Verified: NO — deferred to Phase 2 |

Trace: ADR-0006, ADR-0009, ADR-0012, ADR-0013, ADR-0020 · Contract:
`packages/schema/routing-policy.schema.json`, `packages/schema/examples/routing.minimal.json`,
`packages/schema/examples/routing.two-plane.json`, `packages/schema/examples/invalid/` (three routing cases) ·
See also: `docs/audit/07-routing-engine.md`, `docs/architecture/27-config-compilation-pipeline.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/37-ui-architecture.md`,
`docs/architecture/38-compatibility-matrix.md`.
