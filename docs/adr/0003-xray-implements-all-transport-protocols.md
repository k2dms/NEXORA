# ADR-0003: Xray inside the connector implements every transport protocol; JavaScript implements none

Status: Accepted
Date: 2026-11-04

## Context

The product asks for VLESS with REALITY, TLS and the Vision flow over TCP, WebSocket, gRPC and XHTTP.
Phase 0 measured two things that decide where that can live.

First, the browser has no vocabulary for any of it. The whole outbound surface a profile can express is a
PAC result string built from a five-entry scheme table (`omega-pac/src/profiles.coffee:53`) interpolated as
`SCHEME host:port` (`omega-pac/src/profiles.coffee:87`), compiled into a generated `FindProxyForURL`
(`omega-pac/src/pac_generator.coffee:148`) or into a `chrome.proxy` `fixed_servers` rule
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:45`). `PROXY`, `HTTPS`,
`SOCKS`, `SOCKS5`, `DIRECT` — there is no token for a handshake, a flow, a public key or a mux.

Second, there is no protocol code to build on: a case-insensitive search for `vless`, `xtls`, `reality`,
`xray` across the sources of all five projects returns **0 files** (recorded in
`docs/audit/00-scope-and-method.md` §1 and re-run for this ADR). The one artifact of trying anyway is
already in this branch's history: a half-written `vless-test.coffee` that did not run, saved outside the
repository and deleted (`docs/audit/00-scope-and-method.md` §7).

## Decision

1. Every transport and every protocol — VLESS, REALITY, TLS, the Vision flow, and the TCP, WebSocket, gRPC
   and XHTTP transports — MUST be implemented by Xray Core running inside the Go connector. NEXORA MUST NOT
   implement, re-implement or shim any part of a handshake, framing, mux or TLS stack in JavaScript.
2. The browser-side configuration model MUST therefore degrade to exactly one choice: which loopback
   `HTTP` or `SOCKS5` endpoint a PAC result or a `fixed_servers` rule names. The extension selects a profile;
   Core carries the traffic. No browser-side field MAY describe a protocol, a flow or a certificate.
3. JavaScript MAY validate *shape* — the JSON Schemas in `packages/schema` describe server fields, transport
   combinations and secret references — and MUST NOT infer or assert protocol behaviour from that shape.
   `packages/schema/server.schema.json:31` naming `vless` is a declaration of intent, not an implementation.
4. Unsupported or impossible combinations MUST be rejected at the boundary by validation, before a config
   reaches Core: `xtls-rprx-vision` is legal only over `tcp`
   (`packages/schema/server.schema.json:327`), `h2` requires TLS, and `reality` MUST NOT contain a private
   key. The error is `UnsupportedCombination`
   (`packages/schema/protocol/error-taxonomy.json:14`), never a silently dropped field.
5. Transport capability MUST be advertised by negotiation (`packages/schema/protocol/compatibility-rules.json:21`
   through `:46`), so an extension can refuse to offer a transport its connector cannot run, rather than
   emitting a config that Core accepts and ignores.
6. Routing rules that cannot be compiled into Core form MUST be reported to the user as unrepresentable
   (ADR-0013); the compiler MUST NOT drop them quietly.

## Consequences

Cheaper: one protocol implementation, upgraded by upstream rather than by us; the whole `pins` mechanism
reduces to verifying a binary hash and signature instead of auditing a crypto port; the extension's attack
surface stays where the audit found it; and the deleted JavaScript draft cannot come back as "partly done".

Harder: every request now traverses an extra loopback hop, so latency and port-allocation policy become real
design subjects (`docs/architecture/27-config-compilation-pipeline.md`); a Core bug cannot be fixed in place — it
becomes a pinned-version decision; the extension's feature set is bounded by what a config file can express;
and a first-run download is required, which is the air-gapped-installation open question recorded in
`docs/PROJECT-STATE.md` §9.7.

Forbidden for this iteration: any protocol code under `packages/**`, `nexora/**` or `connector/**` other
than the Go process boundary itself; adding a transport to the UI that is not expressible in Xray's config;
and describing VLESS in an architecture document as something the extension "handles".

## Alternatives considered

- **A pure-JavaScript VLESS client (the master prompt's first instinct).** Rejected on substance, not taste:
  REALITY depends on a forked Go TLS stack with controlled ClientHello manipulation, and no maintained
  browser-side port provides it; even where community ports exist, we would own a second routing engine and a
  second crypto review. The measured 0-file baseline means this is greenfield, not a port.
- **sing-box instead of Xray Core.** Rejected for this iteration: it is a different configuration grammar and
  a different capability set, and the transport names, the flow names and the pin upstream
  (`packages/schema/pins.schema.json:5`) are all written against Xray. Choosing sing-box is a legitimate
  future ADR, not a variation of this one.
- **Drive an externally installed, user-managed proxy (v2rayN, Clash) through its local API.** Rejected:
  the user-managed process is outside our integrity pin, its port is not ours to assert closed, and the
  fail-closed path of ADR-0009 depends on owning the process lifecycle.
- **Compile VLESS into WebSocket frames the browser can send.** Rejected: it is the same JavaScript protocol
  implementation under a different name.

## Evidence

- `omega-pac/src/profiles.coffee:53` — VERIFIED — the complete browser-side scheme vocabulary.
- `omega-pac/src/profiles.coffee:87` — VERIFIED — a PAC result is `SCHEME host:port`, nothing more.
- `omega-pac/src/pac_generator.coffee:148` — VERIFIED — the generated decision function is `FindProxyForURL`.
- `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:45` — VERIFIED — the fixed-profile
  path emits `mode: fixed_servers`.
- `packages/schema/server.schema.json:31` and `packages/schema/server.schema.json:327` — VERIFIED — protocol
  is a const and the Vision-over-TCP rule; both are validation, not transport code.
- `packages/schema/protocol/compatibility-rules.json:21` — VERIFIED — transports appear as capability names.
- Zero protocol code in the legacy tree — VERIFIED by re-running the `grep -i` over `omega-pac/src`,
  `omega-target/src`, `omega-web/src`, `omega-target-chromium-extension/src` (0 matches).
- "The extension cannot speak these protocols" — INFERRED from the citations above plus the documented
  capability of `chrome.proxy`; not observed against a live Core process in this iteration.

## Verification

Today: `npm --prefix packages/schema run verify` compiles `server.schema.json`, validates the five server
examples and asserts the nine transport-combination rules reject — including
`packages/schema/examples/invalid/server.vision-over-ws.json` and `examples/invalid/server.h2-without-tls.json`,
each checked to fail on the documented keyword. That proves the *shape* half of rule 4.

Nothing proves rule 1 or rule 2 yet: there is no Core process, no loopback inbound and no egress measurement
in this tree. The runtime assertion is Phase 2 row `LIVE-04` of the live matrix (egress IP must equal the test
server's IP, see `docs/verification/live-env-contract.md`), and the negative assertion — a CI grep that fails
if `vless|reality|xtls` appears as executable code under `packages/` or `nexora/` — is deferred with it.

Verified: NO — deferred to Phase 2.
