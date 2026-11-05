# ADR-0004: The browser talks to the connector over Native Messaging only

Status: Accepted
Date: 2026-11-04

## Context

The extension has to command a local process that owns a proxy. There are only two families of way to do
that: a channel the operating system provides (Native Messaging pipes, a named pipe or a Unix socket we
pair over) or a channel the network provides (a loopback HTTP, WebSocket or JSON-RPC endpoint).

The network family looks cheaper and is the default instinct in this kind of project — it is debuggable with
curl, and it needs no manifest registration. Phase 0 recorded that no such channel exists today
(`docs/audit/00-scope-and-method.md` §1: a search for `nativeMessaging` across the tree returns 0 files), and
it also recorded what this fork does when it has to decide *who* is allowed to drive the proxy: a hard-coded
list of extension IDs in source (`docs/audit/11-security-findings.md`, SEC-4) — a control that has to be
maintained by hand because the platform gives the code no sender identity on a network socket.

The connector's blast radius is not theoretical. It holds subscription credentials in a keystore
(`packages/schema/server.schema.json:49`) and it points the browser at a local port through which all of the
user's traffic will pass. A control endpoint for it is the highest-value local target in the product.

## Decision

1. The browser and the connector MUST communicate over Native Messaging only. The pipe is the whole control
   plane; NEXORA MUST NOT define a protocol that runs over the network — locally or otherwise
   (`packages/schema/protocol/framing.json:78`).
2. The connector MUST NOT open a listening TCP or UDP socket for control purposes, and MUST NOT expose an
   HTTP, WebSocket or JSON-RPC endpoint on `127.0.0.1`, `localhost` or any interface. This includes a
   development-only or diagnostics-only endpoint; a debug endpoint is the production endpoint with the
   documentation missing.
3. Sender identity MUST come from the platform: the host resolves the calling extension's ID from the
   Native Messaging manifest registration and compares it against the allowlist it was configured with at
   startup. A payload field asserting "I am the extension" MUST NOT be treated as identity
   (`packages/schema/protocol/error-taxonomy.json:12`).
4. There MUST be no port discovery, no pairing code, no shared secret in a file and no token rotation
   ceremony, because rules 1-3 make them unnecessary. Adding any of them is evidence that a second channel
   has crept in.
5. Frame limits, chunking and heartbeat MUST follow `packages/schema/protocol/framing.json` — the 1 MB
   extension-to-host cap at `packages/schema/protocol/framing.json:26`, the larger host-to-extension cap at
   `:31`, and the chunked envelope for anything bigger. A payload that does not fit MUST NOT be handed over
   out of band (`packages/schema/protocol/framing.json:44`).
6. A request from a sender not on the allowlist MUST be answered only with `UnauthorizedExtension` and MUST
   NOT touch state.

## Consequences

Cheaper: nothing to bind, discover, authenticate or firewall; the OS enforces the channel and the peer
identity; the connector can stay single-user with no credential to leak into a `Set-Cookie`; the manifest
registration is one small file installed by the connector, and its absence is a legible error rather than a
misrouted request.

Harder: Native Messaging must be registered per browser and per OS, which makes connector install and
uninstall a real design subject (`docs/architecture/33-binary-integrity.md`); the 1 MB per-message cap
forces chunking, which is why `profile.import` and the chunk fixtures exist at all; a Chrome service-worker
eviction tears the port down, which is what ADR-0005 exists to survive; and a developer cannot poke the
connector with curl — diagnostics go through the typed `diagnostics.get` command instead, which is a
deliberate reduction in developer convenience.

Forbidden for this iteration and the next: any loopback server in `connector/**`; a browser-side fetch to a
local port; and reusing `chrome.runtime` message passing (which is in-browser, not browser-to-process) as
the connector transport.

## Alternatives considered

- **Loopback HTTP plus a bearer token.** Rejected on three measured grounds. Any local process can connect —
  the token then has to be discoverable by the extension, which means it lives in a file or in synced
  storage, which is the exact class of leak SEC-1 documents (`docs/audit/11-security-findings.md`).
  A web page can reach it through DNS rebinding or a simple cross-origin POST with no preflight. And the
  server still cannot tell *which* extension is talking, so the identity allowlist has to be rebuilt in
  payload.
- **Loopback WebSocket.** Rejected for the same reasons with a different transport: the connection is
  still origin-blind and process-blind, and it adds a port to the system.
- **Named pipe or Unix socket with our own framing and a pairing step.** Rejected: it delivers roughly what
  Native Messaging delivers, but the sender identity becomes "whoever can open the pipe", so the pairing
  ceremony we rejected in HTTP returns, on three platforms, with our own tests.
- **File-based handoff (extension writes a request, connector polls).** Rejected: no delivery notification,
  an ordering problem on every field, a race the chunked-envelope note calls out at
  `packages/schema/protocol/framing.json:44` — and a world-writable staging directory is not an authenticated
  channel.

## Evidence

- `packages/schema/protocol/framing.json:78` — VERIFIED — non-goal: "no protocol defined by NEXORA over the
  network — the pipe is local and its only job is control".
- `packages/schema/protocol/framing.json:26` and `packages/schema/protocol/framing.json:31` — VERIFIED — the
  two byte caps the decision commits us to.
- `packages/schema/protocol/framing.json:44` — VERIFIED — rejection of out-of-band file handoff, with the
  trust argument stated in data.
- `packages/schema/protocol/error-taxonomy.json:12` — VERIFIED — `UnauthorizedExtension` is defined as the
  sender-ID failure, i.e. the host is expected to know the sender.
- `packages/schema/server.schema.json:49` — VERIFIED — secrets are keystore references; a control channel
  that can be opened by another process makes every reference retrievable.
- `docs/audit/00-scope-and-method.md` §1 — VERIFIED (as recorded by the audit) — zero Native Messaging code
  in the tree today.
- SEC-4's hard-coded extension-ID allowlist as the current identity mechanism — VERIFIED in
  `docs/audit/11-security-findings.md`; the claim that a loopback endpoint is reachable by other local
  processes and by web pages via DNS rebinding — INFERRED (standard platform behaviour, not measured on this
  host).

## Verification

Today, rule 5 is checked: `npm --prefix packages/schema run verify` compiles `protocol/framing.json`,
asserts its numeric limits and its `oversizedPayloads.decision`, and replays the 109 fixture frames in
`packages/schema/fixtures/protocol/` against `protocol/envelope.schema.json`
(`packages/schema/scripts/verify.mjs:247-263`). Rule 3's error code is checked against the taxonomy at
`packages/schema/scripts/verify.mjs:222-234`.

Rules 1, 2, 4 and 6 are properties of a process that does not exist yet. The Phase-2 checks are named in
`docs/architecture/25-native-messaging-protocol.md`: a Go test that starts the connector and asserts zero
listening sockets other than the loopback inbounds, and a browser test that a second extension ID is refused.

Verified: NO — deferred to Phase 2.
