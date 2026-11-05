# 21 — System context and process model

> Purpose: name every process that exists at runtime, who starts it, who may kill it, and what the system
> promises when each one dies. This is the document ADR-0005 points at for the two-process connector, and the
> place where the containment chain (`Xray ⊆ Connector ⊆ host ⊆ service worker`) is stated as a rule rather than
> drawn.

Status: Accepted.

## 1. Reading rules

As in `docs/architecture/20-target-repository-layout.md` §1: `path:line` means the line was read this iteration
and `node tools/check-doc-evidence.mjs` enforces it; `path#/json/pointer` cites contract data, which does not
drift when a file grows; `connector/**` and `nexora/**` paths are cited without line numbers because they are
target layout; every behavioural claim with no check today carries `Verified: NO — deferred to Phase 2` inline
and owns a row in `docs/architecture/39-verification-matrix.md`.

## 2. The system in one paragraph

NEXORA is a browser extension that decides, per request, which proxy should carry it, and a per-user local
program that is the only thing able to speak the transport protocols. The extension keeps the user's *intent* —
profiles, rules, subscriptions, UI state — and writes a browser proxy configuration pointing at loopback. The
Connector owns the *executable truth* — a compiled Core configuration, live inbound ports, credentials, health
history. Nothing in the browser implements VLESS, TLS or REALITY; the browser only ever chooses
`HTTP/SOCKS5 127.0.0.1:<port>` (ADR-0003, ADR-0006). The two halves talk over one Native Messaging port and
nothing else (ADR-0004).

## 3. Actors and external systems

| Actor / system | Kind | What it does | What it can force on us |
|---|---|---|---|
| The user | human | creates profiles, picks a server, refreshes subscriptions, approves fail-open | any command in `docs/architecture/26-command-surface.md` |
| Browser (Chrome/Edge ≥ the floor in `docs/architecture/38-compatibility-matrix.md` §3, Firefox ≥ 112 per `omega-target-chromium-extension/overlay/manifest-firefox.json:52`) | platform | hosts the service worker, enforces the proxy API, opens/tears the Native Messaging port, evicts the worker | kills the worker at any time; silently ignores `chrome.proxy` writes it considers invalid |
| Web content (any page) | untrusted | makes requests that our decision applies to | nothing directly — it cannot reach the Connector, but it can make us *choose* differently through a PAC evaluation |
| Xray Core | forked child | terminates the transport, holds the only copy of a live credential in memory for the duration of a connection | exits non-zero, binds a non-loopback port, refuses a config we compiled |
| VPS / subscription endpoint | remote | serves rule lists, server lists, and the actual egress | returns 404/403/HTML/an 80 MB body; is itself the MITM for REALITY handshakes |
| OS keystore / DPAPI | local trust store | holds credentials at rest (ADR-0011) | is per-user, not per-process — see `docs/architecture/31-security-model.md` residual risk |
| Extension store (CWS/AMO) | distributor | ships updates | an automatic update restarts the browser and the worker mid-session — the reason `SwTeardown` needs a grace window (`docs/architecture/30-failure-policy.md`) |

## 4. Context diagram

```
        ┌────────────────────────────── browser process ──────────────────────────────┐
        │  web content      popup (Svelte island)     options page (tabs + routes)     │
        │      │                    │                          │                       │
        │      │        request decisions (PAC / onRequest)    │ chrome.runtime messages│
        │      ▼                    ▼                          ▼                       │
        │  ┌──────────────── service worker (MV3) ────────────────────────────────┐    │
        │  │ ProfileRegistry · Conditions · Storage(+/- envelope) · FailurePolicy │    │
        │  │ chrome.proxy.settings writer · diagnostics ring · event subscriber   │    │
        │  └───────────────┬───────────────────────────────────┬─────────────────┘    │
        │                  │ chrome.runtime.connectNative      │ chrome.proxy.*       │
        └──────────────────┼──────────────────────────────────┼──────────────────────┘
                           │ 4-byte LE length + one JSON frame │
                           ▼                                   ▼
              ┌────────────────────────┐            loopback TCP 127.0.0.1:<port>
              │ nexora-host (shim)     │            ┌────────────────────────────┐
              │ framing · sender allow │            │ browser network stack      │
              │ list · spawn-and-adopt │            │ (FixedProfile → port/tag)  │
              └───────┬────────────────┘            └────────────┬───────────────┘
                      │ local pipe (verbatim envelope, versioned  │
                      │                      independently)       ▼
                      ▼                                  ┌──────────────┐   remote
        ┌───────────────────────────┐                    │  Xray Core   │──────────▶ VPS
        │ nexora-connector (Go)     │──── config, ──────▶│ inbounds per │   (VLESS/
        │ keystore · config compiler│     spawn/adopt    │ server tag,  │    REALITY/
        │ health probes · events    │                    │ routing plane│    TLS/Vision)
        └───────────────────────────┘                    └──────────────┘
```

The right-hand `chrome.proxy` path and the left-hand Native Messaging path are **independent**: the first is
how traffic reaches the Core, the second is how the Core is told what to do with it. A failure of the second
must not silently change the first — that single sentence is why `docs/architecture/30-failure-policy.md`
exists as a named policy rather than as error handling.

## 5. Process inventory and lifetimes

| Process | Started by | Lives as long as | Killed by | Must survive its death |
|---|---|---|---|---|
| Service worker | browser, on event | minutes; evicted freely (`omega-target-chromium-extension/overlay/x-background.js:34` is the legacy keep-alive attempt) | browser | Connector, Core, applied proxy config, every user profile |
| Popup / options UI | user click / `options_page` (`omega-target-chromium-extension/overlay/manifest.json:23`, `:31-34`) | tab or popup lifetime | user, navigation | nothing (it is a view, not a state owner) |
| `nexora-host` shim | browser, via `connectNative` | the Native Messaging port | browser (close of port), OS | Connector and Core must keep running (ADR-0005) |
| `nexora-connector` | the shim (spawn + adopt) | until `core.stop`/`keystore.purge`, or crash | the shim, OS containment, the user | Core must be contained by it, never orphaned (ADR-0005, `docs/architecture/30-failure-policy.md`) |
| Xray Core | the connector, with the config it compiled | until `config.apply` restarts it, or crash | the connector, OS containment | nothing — restart is converge-to-desired-state, so `core.start` again returns the same logical state |

Containment chain, stated as a rule: **`Xray ⊆ connector ⊆ host ⊆ service worker ⊆ browser`.** Containment
means: if the outer process dies, the inner one is destroyed by the OS, not by cooperation. On Windows this
does not happen by default and therefore has to be *requested* — Job Objects with
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, because a bare child outlives its parent there. On Linux/macOS the
equivalents are `prctl(PR_SET_PDEATHSIG)` and `kqueue`/`EVFILT_PROC`. Each level is adopted by the level above
at spawn time and re-adopted on reconnect. `Verified: NO — deferred to Phase 2` (LIVE-13 is the row that kills
a parent and asserts no orphan remains).

The inversion this creates is deliberate: the *browser* is the least durable process in the chain, so nothing
that must persist — credentials, applied config hash, health history, the running Core — may live in it.

## 6. Why the connector is two processes (ADR-0005 restated operationally)

One process would mean the shim's lifetime *is* the Core's lifetime. Chrome closes the Native Messaging port
when the worker is evicted — routinely, and always at browser restart — and Firefox closes it when the last
window closes. A single process would then take Xray down with it, dropping every live tunnel, which is
indistinguishable from an outage from the user's side. Splitting gives:

- **shim** — framing, sender allowlist check, spawn-or-adopt, byte pump. No business logic, no keystore access,
  no config compiler. Small enough to be reviewed as the only attack surface the browser can reach.
- **connector** — everything stateful. Survives port teardown; the next `connectNative` re-attaches to the
  running instance instead of starting a second one (`CoreAlreadyRunning` is the error that says "another
  browser profile already owns the instance" — see `docs/architecture/40-open-decisions.md`).
- **the pipe between them** carries the same envelope as the browser side, so no translation layer exists, but
  it is versioned *separately* (`packages/schema/protocol/framing.json#/protocolVersion` for the browser side;
  a distinct `minPipeVersion` for the shim↔connector hop) because the shim is replaced by an extension update
  while the connector is upgraded by user action on a different schedule (ADR-0019). Consequence worth stating:
  a shim from NEXORA 3.7 may legally talk to a connector from 3.6 when both sides' declared floors overlap, so
  the negotiation result must be *written down* in the connector log and surfaced in `diagnostics.get`.

Reconnect semantics after a port teardown are in `packages/schema/protocol/framing.json#/reconnect`: re-run
`connector.hello`, then reconcile by comparing the extension's `config.snapshot` hash against the applied hash.
`Verified: NO — deferred to Phase 2`.

## 7. Which process decides what (the two planes, in one row each)

| Plane | Deciding process | Mechanism | Document |
|---|---|---|---|
| Selection ("which outbound tag for this request") | browser (worker + PAC) | Omega `Conditions`, compiled into PAC on Chromium, evaluated in the request listener where PAC cannot express it | `docs/architecture/28-routing-model.md`, `docs/audit/07-routing-engine.md` |
| Transport ("how the bytes reach the VPS") | connector → Core | Xray `routing` + outbound objects, compiled from intent | `docs/architecture/27-config-compilation-pipeline.md` |

The invariant that keeps the two from fighting: exactly one plane decides a request's fate, and the browser
plane's output is a *tag*, never a transport parameter. ADR-0013; enforcement is `config.apply` returning
`result.unrepresentable[]` instead of dropping rules
(`packages/schema/protocol/commands/config.apply.json#/result/properties/unrepresentable`).

## 8. Where data rests

Deliberately compressed here; the ownership table is `docs/architecture/34-data-split.md` §5–§7.

- Browser: `chrome.storage.local` (and `session` for temp rules) under the flat `+`/`-` envelope —
  `docs/audit/04-data-model-and-options-envelope.md`, `docs/audit/05-storage-sync-and-migration.md`.
- Connector: per-user directory — keystore (ADR-0011), compiled config + its hash, health history, subscriptions
  cache, diagnostics ring, `pins.json` mirror.
- Never in either: a plaintext credential in a sync transit (SEC-1, `docs/audit/11-security-findings.md`), a
  secret in a `traceId` (`packages/schema/protocol/framing.json#/nonGoals`).

## 9. Explicit non-goals of the deployed system

Each is an ADR, restated as "this does not exist":

1. No transport protocol implemented in JavaScript (ADR-0003).
2. No loopback HTTP/WebSocket control API, no port discovery, no pairing code (ADR-0004). A local HTTP control
   endpoint is reachable by every local process and by any web page through DNS rebinding; Native Messaging
   gives the OS pipe and the sender identity for free.
3. No server-side component, no NEXORA cloud, no account system. Sync, if used, is the legacy Gist/WebDAV
   mechanism with SEC-1 closed (`docs/audit/09-subscriptions-import-export-sync.md`).
4. No bundled Xray binary in the extension archive (ADR-0019; `docs/architecture/20-target-repository-layout.md`
   §9 and `docs/architecture/33-binary-integrity.md`).
5. No silent self-update of Core or connector (ADR-0019; `docs/architecture/33-binary-integrity.md` §upgrade).
6. No arbitrary shell command reachable from the browser: nineteen typed methods, closed enum of errors
   (ADR-0007; `docs/architecture/26-command-surface.md`).

## 10. What each process death does to the user

| Death | User-visible effect | Recovery | Policy owner |
|---|---|---|---|
| Worker eviction | nothing while the applied `chrome.proxy` config stands; in-flight commands are answered by the deadline rule in `packages/schema/protocol/framing.json#/liveness` | next event wakes the worker; `onStartup`/`startupCheck` reconcile | `docs/architecture/30-failure-policy.md` §5 (`serviceTeardown`) |
| Shim teardown (port closed) | nothing; Core keeps serving existing tunnels | reconnect per §6 | same |
| Connector crash | existing tunnels die with it (it is the parent); no new commands; badge goes to the error state | `core.start` converges; the applied config is re-verified by hash before anything is restarted | `docs/architecture/30-failure-policy.md` §5, and §4 for what the extension does meanwhile |
| Core crash/exits | every proxied request fails: with the applied config pointing at loopback ports, nothing silently goes direct | the block is written first, then the restart ladder runs; lifting it requires a verified restart | `docs/architecture/30-failure-policy.md` §4, §12 |
| Browser exit | everything is torn down by containment | `onStartup` restores the last applied state | `docs/audit/06-proxy-application.md` |

The row that matters most is the last-but-one: a dead Core must **not** produce direct egress. Fail-open is a
per-profile, explicitly confirmed opt-in (`packages/schema/failure-policy.schema.json#/$defs/ProfilePolicy`), never a
default, never silent (ADR-0009).

## 11. Launch model per OS

The shim is started by the browser (Native Messaging is always browser-spawned — the extension cannot choose
the PID, only the manifest). Everything else follows from *who adopts the Connector*, and the answer is: nobody
but the shim, and the Connector adopts Core.

| OS | How the Connector runs | Why not the alternative | Containment mechanism |
|---|---|---|---|
| Windows | user-session process, spawned and adopted by the shim, with a Job Object handle | a Windows **service** (SCM) runs in session 0 as `LocalSystem` or a service account: it cannot see the interactive user's DPAPI entropy (ADR-0011), cannot be installed without elevation, and its lifetime is decoupled from the user in a way that leaves a Core holding credentials after logout. A scheduled task adds a second identity and a latency floor. Both are rejected for the same reason: the keystore is per-user, so the process that reads it must be that user's | `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` on a job the shim holds; the Connector puts Core in the same job at spawn (`docs/architecture/30-failure-policy.md`) |
| Linux | plain forked child of the shim, no daemon, no `systemd --user` unit required | a systemd user unit is a hard dependency on a desktop that a headless or non-systemd user may not have; it also survives browser exit, which is the opposite of what containment asks for | `prctl(PR_SET_PDEATHSIG, SIGKILL)` in the child, plus the shim's own `waitpid` bookkeeping |
| macOS | plain forked child of the shim | a LaunchAgent would keep the Connector alive across logout and would need a plist installed with user consent; no benefit for a single-user proxy | `kqueue`/`EVFILT_PROC` note on the parent, or `libproc` polling where the note is unavailable |

Consequence of the shim being the only spawner: **reconnect is not start**. When the port drops and a new shim
process appears, the existing Connector is *adopted* (a lock file with `{pid, pipeVersion, startedAt, configHash}`
in the per-user directory) rather than replaced — `docs/architecture/26-command-surface.md` §`core.start`
converge-to-desired-state. If the lock names a live process the shim cannot reach, the answer is
`CoreAlreadyRunning`, never a second Core. `Verified: NO — deferred to Phase 2` (the lock file's format is
recorded in `docs/architecture/40-open-decisions.md`).

## 12. Invariants of this document and their checks

| Invariant | Enforcing check today | State |
|---|---|---|
| Exactly one Native Messaging entry point per extension installation | nothing (no host exists yet) | Verified: NO — deferred to Phase 2 |
| `Xray ⊆ connector ⊆ host` containment holds after a SIGKILL of the parent | nothing | Verified: NO — LIVE-13 |
| The Connector runs in the interactive user's session and never as a service/agent | nothing | Verified: NO — deferred to Phase 2 (§11); the installer must not register an SCM service or a LaunchAgent |
| Reconnect adopts the running Connector instead of starting a second one | nothing | Verified: NO — deferred to Phase 2 (lock file format open, `docs/architecture/40-open-decisions.md`) |
| No inbound binds non-loopback | asserted at `core.start` per ADR-0006; the shape is `packages/schema/protocol/commands/core.start.json#/result/properties/inboundPorts` and the compile-time rule is `packages/schema/connector-profile.schema.json#/properties/serverIds` | Verified: NO — deferred to Phase 2 (a Go test, not schema-checkable) |
| No protocol code in `nexora/**` | grep gate planned in `.github/workflows/verify.yml` | Verified: NO — deferred to Phase 2 |
| The pipe version and the browser protocol version are independent numbers | `packages/schema/protocol/framing.json#/protocolVersion` exists; `minPipeVersion` does not yet | Verified: NO — recorded as a Phase-2 contract addition, `docs/architecture/40-open-decisions.md` |
| The command set the worker may send is the allowlist | `packages/schema/protocol/command-allowlist.json` vs `packages/schema/protocol/commands/` — the "command-allowlist.json and protocol/commands/*.json name the same set" assertion in `packages/schema/scripts/verify.mjs` | **GREEN** (run `npm --prefix packages/schema run verify`) |
| Nothing is dispatchable before negotiation | the "nothing is dispatchable before negotiation except connector.hello" assertion in the same file | **GREEN** |
| Frame limits are the documented ones | the framing sanity block in `packages/schema/scripts/verify.mjs` (`:242-263`) | **GREEN** |

Trace: ADR-0003, ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0009, ADR-0011, ADR-0013, ADR-0019 · Contract:
`packages/schema/protocol/framing.json`, `packages/schema/protocol/command-allowlist.json`,
`packages/schema/connector-profile.schema.json`, `packages/schema/failure-policy.schema.json` ·
See also: `docs/architecture/20-target-repository-layout.md`,
`docs/architecture/22-component-contracts.md`, `docs/architecture/25-native-messaging-protocol.md`,
`docs/architecture/26-command-surface.md`, `docs/architecture/28-routing-model.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/31-security-model.md`,
`docs/architecture/34-data-split.md`, `docs/audit/03-runtime-layering.md`,
`docs/audit/06-proxy-application.md`.
