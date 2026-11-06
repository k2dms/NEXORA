# ADR-0005: The connector is two processes, a thin host and a long-lived service

Status: Accepted
Date: 2026-11-04

## Context

Native Messaging has a lifecycle the extension does not control. The browser starts the host when a port is
opened and terminates it when the last port closes — including when a service worker is evicted, which on
MV3 is routine and idle-driven, and when an options tab reloads. The contract data already records this:
`packages/schema/protocol/framing.json:62` states that Chrome tears the port out on eviction and that "the
Connector and Core MUST survive that — a dropped port is a client reconnect event, never a shutdown signal".

The legacy tree contains the same asymmetry in miniature, and it cost a user-visible bug: a popup that calls
into an MV3 service worker that is asleep gets "Receiving end does not exist" and hangs unless the caller
retries with a long backoff, which is what `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78`
implements (five quick attempts, then nine exponentially longer ones, roughly fifteen seconds). If the
in-process worker's disappearance is that awkward, a *proxy process* disappearing when a tab reloads is
unacceptable: every tunnel in the browser breaks.

Neither of those processes exists yet — `connector/` has had Go *libraries* since 2026-11-05 and still has no `cmd/`,
no host and no pipe (`docs/PROJECT-STATE.md` §4.6) — so the shape is a decision, not a refactor.

## Decision

1. The connector system MUST be two processes: `nexora-host`, a thin shim whose only jobs are Native
   Messaging framing, sender-identity checking per ADR-0004 and making sure `nexora-connector` is running;
   and `nexora-connector`, the service that owns configuration compilation, the keystore, health history and
   the Xray Core child process.
2. The two MUST communicate over a local pipe whose message schema is versioned **separately** from the
   extension-facing protocol, so that the host can be replaced while the connector keeps running. The
   negotiation rule in `packages/schema/protocol/compatibility-rules.json:12` governs the extension-facing
   version; the internal pipe MUST carry its own version field and MUST refuse a mismatch rather than guess.
3. The host MUST NOT own Core, MUST NOT hold decrypted secrets, and MUST NOT decide proxy state. It MAY be
   killed at any moment without changing the user's effective routing.
4. The connector MUST start on host request if absent, MUST keep running with no connected host, and MUST
   exit only on its own shutdown policy — a user action, an idle timeout that preserves the fail-closed
   setting of ADR-0009, or a `core.stop`/teardown decision. It MUST NOT treat a disconnected port as a kill
   signal.
5. Core containment is the connector's job: the connector MUST place Core in a Job Object (Windows) or set
   `PR_SET_PDEATHSIG` / equivalent parent-death notification (Linux, macOS), and MUST report the result in
   `core.start`'s `contained` field (`packages/schema/protocol/commands/core.start.json:52`) rather than
   assuming success. On Windows containment is a requirement, not a nicety: the platform performs no
   parent-death cleanup by default.
6. The containment chain MUST be `Xray ⊆ connector ⊆ host ⊆ service worker`, and every reconnection MUST
   re-run `connector.hello` and then reconcile by hash, per the `afterReconnect` rule in
   `packages/schema/protocol/framing.json:68`.
7. The keystore backend MUST be resolved by the connector and reported to the extension
   (`packages/schema/protocol/commands/connector.hello.json:35`), never by the host.

## Consequences

Cheaper: an options-page reload or a service-worker eviction stops being a connectivity event; the host
binary can be reinstalled or replaced during an upgrade without dropping tunnels; the connector can hold a
warm Core, a health history and a subscription cache that survive UI churn; and the failure planes stay
distinct, which is what lets `packages/schema/failure-policy.schema.json:40` give `serviceTeardown` a bounded
grace window while `coreDeath` blocks immediately.

Harder: two artifacts to build, pin, install and version; one more crash surface (the connector dying is now
a first-class event with its own policy); an orphans problem if the connector outlives a browser session —
bounded by rule 4's idle timeout and by containment; and a local pipe with its own schema is a second
contract nobody can copy from the extension protocol.

Forbidden for the iteration this was written in: writing either process — there was no Go toolchain on this host
when it was accepted, and Phase 1 was documents and contracts only. The user lifted that hold on 2026-11-05
(`docs/PROJECT-STATE.md` §4.6); the libraries landed, the two `cmd/` processes have not. Still forbidden, and the
reason this ADR exists: folding host duties into the connector "temporarily".

## Alternatives considered

- **Single process: the host is the connector.** Rejected, and the reason is measured rather than
  stylistic: the browser terminates the host when the port closes, so every options-tab reload, every
  service-worker eviction and every host upgrade would drop Core and re-establish every tunnel. It also
  makes host replacement impossible without a proxy outage, which is a routine install-time event.
- **A system service or scheduled task launching the connector.** Rejected: it needs elevation or a service
  installer, changes the per-user trust model of the keystore, and makes the extension install require
  administrator rights on Windows — unacceptable for a store-distributed tool that today installs by loading
  a zip.
- **The extension starting Core directly.** Rejected: an MV3 service worker has no process-spawning API, and
  even with the host as spawner, Core would be parented to a process with the short lifetime of rule 3.
- **Two processes plus a supervising daemon (three total).** Rejected: the supervisor becomes a third binary
  with the same lifetime question the ADR answers, and nothing in the failure model needs it — the connector
  is already the supervisor.

## Evidence

- `packages/schema/protocol/framing.json:62` — VERIFIED — the port-teardown and survive-it requirement, and
  `packages/schema/protocol/framing.json:68` — VERIFIED — the reconnect-and-reconcile rule.
- `packages/schema/protocol/commands/core.start.json:52` — VERIFIED — `contained` exists so a missing Job
  Object is a reported condition, with `packages/schema/protocol/commands/connector.hello.json:36` —
  VERIFIED — carrying `jobObjectAvailable`.
- `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78` — VERIFIED — the retry ladder
  that exists because an in-browser counterpart goes away; the precedent for lifetime asymmetry, not for the
  mechanism.
- `packages/schema/failure-policy.schema.json:40` — VERIFIED — `serviceTeardown` is described as "the
  Connector process itself is gone (host shim disconnected…)", i.e. the contract already assumes two
  processes.
- `packages/schema/protocol/commands/core.start.json:4` — VERIFIED — converge-to-running semantics that make
  a reconnect-driven `core.start` safe rather than a port-conflict storm.
- Neither process exists, and never did when this was accepted — `connector/` gained three Go libraries on
  2026-11-05 and no `cmd/` (`docs/PROJECT-STATE.md` §4.6); the runtime behaviour of the two-process design is
  therefore UNVERIFIED by construction and belongs to Phase 2.

## Verification

Today: the *shape* of the split is checked, because `npm --prefix packages/schema run verify` validates
`failure-policy.schema.json`, the 19 command documents and the 109 replayed frames, and the
`connector.hello` result is required to name a keystore backend and a Job-Object answer — fields that only
make sense if the service and the UI are separate processes.

The behaviour itself is not checkable without the binaries. Phase 2 must assert, as named in
`docs/architecture/21-system-context.md`: closing every Native Messaging port for sixty seconds
leaves the Core PID unchanged; killing the host leaves the connector and Core serving the already-applied
config; `contained = false` surfaces as a warning, never as a silent start.

The third of those three has a producer and a check now. `connector/internal/supervisor` asks the platform
for the containment answer after every spawn, puts it in `core.start`'s `contained` beside the reason in a
note, and a test fails if the flag and the note ever disagree — rule 5's "report rather than assume", made
mechanical. Rule 1's stronger reading is not satisfied by that code and is stated as the cost in doc 22 §4:
Go's `os/exec` offers no job-list attribute, so on Windows the child is assigned after its pid exists rather
than created into the scope. The first two assertions still need a host process this repository has not
written and a browser session this host is not.

Verified: NO — deferred to Phase 2.
