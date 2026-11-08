# 30 — Failure policy

> Purpose: the three ways this system stops working, who notices each one, what the browser is told to do about
> it, and the single rule that holds across all three — a Connector-triggered failure never resolves to direct or
> system egress. ADR-0009 decides the *principle* and explicitly defers its runtime shape here
> (`docs/adr/0009-fail-closed-by-default.md` rule 6 and rule 8).

Status: Accepted. Every resolution word the policy document admits is machine-checked today by the
`forbiddenOutcomes` section of `packages/schema/scripts/verify.mjs`; the behaviour of those words in a real
browser is `Verified: NO — deferred to Phase 2` and owned by row `LIVE-11` of
`docs/verification/live-matrix.md`.

## 1. A plane is a question about who vanished

The policy document has exactly three top-level sections because exactly three actors can disappear:
`packages/schema/failure-policy.schema.json#/required` names them. A fourth kind of bad thing — a command that
failed — is not a plane. Failed commands have answers (`packages/schema/protocol/error-taxonomy.json#/codes`),
the process being gone does
not, and the difference decides who is allowed to act.

| Plane | Who is gone | Who can still act | What the browser must end up with |
|---|---|---|---|
| `coreDeath` | Xray Core | the Connector, the extension | a declared block, then recovery |
| `serviceTeardown` | the Connector (and, by containment, Core) | the extension alone | a declared block after a bounded grace |
| `extensionLifecycle` | the extension, by the browser's decision | nothing inside NEXORA | the user's own proxy state back, or the last applied config |

Two rules span the table:

1. **No resolution reachable from a Connector-triggered plane may be `direct`, `system` or
   `clear-proxy-settings`** (`packages/schema/failure-policy.schema.json#/properties/forbiddenOutcomes`).
   §9 defines the one exception the ADR grants, and why it is not a hole in this rule.
2. **Fail-closed is a state, not an action.** The applied `chrome.proxy` setting always points at a loopback port
   that only a live Core owns, so *doing nothing* is already fail-closed. That is what makes the design survivable
   under MV3 teardown (§10.3, §11); the timers in §11 exist to make a safe state *declared*, not to create safety.

Rule 2 is the reason `+direct` and `chrome.proxy.settings.clear()` were rejected in ADR-0009 rather than merely
disliked: both *route* traffic, so using them to "stop proxying" replaces a closed port with an open path. In this
tree the leak is visible in the actuator itself — `mode: 'direct'`
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:18-19`), `clear()`
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:12-16`), and the PAC literal `DIRECT`
(`omega-pac/src/profiles.coffee:88-89`).

## 2. Why `serviceTeardown` is a separate plane at all

Because Connector death is not Core death (ADR-0005, restated operationally in
`docs/architecture/21-system-context.md` §6): the Connector survives the Native Messaging port closing, so a
closed port usually means "the browser left", not "the proxy died". The two have different recoveries — the first
re-attaches to a still-running instance, the second spawns one — and the policy needs a place to say which. That
is the whole content of `packages/schema/failure-policy.schema.json#/properties/serviceTeardown/description`.

The distinction is load-bearing in one direction only: **`serviceTeardown` ⊇ `coreDeath` in severity.** When the
Connector is gone the extension cannot tell whether Core went with it, so it must resolve at least as strictly as
coreDeath does. §5 draws the consequence.

## 3. Detection: which signal means which plane

The wire-level signals are owned by `docs/architecture/25-native-messaging-protocol.md` §11; the mapping to planes
is owned here.

| Observed | Plane it opens | It must NOT imply |
|---|---|---|
| `core.exited` event (`events.schema.json#/$defs/name`) | `coreDeath` | that the pipe is broken |
| `core.status` answers with `running: false` and a `lastExit` (`packages/schema/protocol/commands/core.status.json#/result/properties/lastExit`) | `coreDeath`, confirmed rather than guessed | that the config was bad — `exitCode` is reported, interpreted nowhere |
| Native Messaging port closed | `serviceTeardown` | a shutdown request (ADR-0005) |
| request deadline exceeded (`packages/schema/protocol/framing.json#/liveness`) | `serviceTeardown` suspect, then `coreDeath` if `connector.hello` still answers | that Core died |
| `contained: false` (`packages/schema/protocol/commands/core.status.json#/result/properties/contained`) | `coreDeath` **plus** an integrity alarm (`docs/architecture/33-binary-integrity.md`) | that nothing is running — an uncontained Core may be serving traffic with no controller |
| browser `runtime.onStartup` / `onInstalled` | `extensionLifecycle` | anything about the Connector |

Three rules about the mapping:

- **One plane per episode**, chosen by the most durable actor still reachable. Handling an episode in two planes
  would let `graceSeconds` apply to a `coreDeath` and delay the block for no reason.
- **A missing `CoreExited` is not evidence of life.** The Connector can only report the exit it observed; if it
  died with Core, the extension reaches `serviceTeardown` through the deadline row above, never through an event.
- **A clock jump is not a death.** After hibernation, `uptimeSeconds` and the `clock-skew` check of
  `packages/schema/protocol/commands/connector.health.json#/result/properties/checks` decide, not wall-clock
  arithmetic in the extension.

## 4. `coreDeath`: block first, then restart

The ordering is the decision this document adds, and the recorded scene already shows it: exit, one status read,
and `core.blocked` 90 ms later — before any restart was attempted
(`packages/schema/fixtures/protocol/failure-block.jsonl`, frames `seq` 1 to 4). Restart attempts follow, and when
they run out the answer the user gets is that the block is still in force (same file, frames `seq` 7 to 8:
`ConfigApplyFailed`, *"restart attempt 3 of 3 failed; block profile left in force"*).

**Why block before retrying.** With Core dead, the port the browser points at is already closed, so writing a
block changes no packet. What it changes is the *status of the state*: an accidental closure becomes a declared,
dated, visible one that `connector.health` can answer `blocked` for
(`packages/schema/protocol/commands/connector.health.json#/result/properties/state`) instead of `unknown`.
The alternative ordering — retry, then block — would leave the user inside an undeclared failure for the whole
ladder: up to `restartAttempts` × `core.start`'s deadline plus the backoff sum
(`packages/schema/protocol/commands/core.start.json#/timeoutMs`,
`packages/schema/failure-policy.schema.json#/properties/coreDeath/properties/restartAttempts`,
`packages/schema/failure-policy.schema.json#/properties/coreDeath/properties/restartBackoffSeconds`), which with the shipped defaults is 3 × 15 s + (1 + 5 + 30) s = **81 seconds** of
"the browser is broken and the product does not say so".

The ladder itself belongs to CoreSupervisor (`docs/architecture/22-component-contracts.md` §4), and it is
converge-to-desired-state, so an extra `core.start` from a user who pressed something is legal rather than a
second spawn (`docs/architecture/26-command-surface.md` §2). No jitter in this ladder, unlike the subscription one
in `docs/architecture/29-subscriptions-and-servers.md` §7: jitter exists to spread a herd, and there is one
Connector per machine.

What bounds the deaths is `coreDeath.stabilityWindowSeconds`
(`packages/schema/failure-policy.schema.json#/properties/coreDeath/properties/stabilityWindowSeconds`, default 300,
floor 1, ceiling 86 400): a Core that stayed up at least that long died into a *new episode*, and the ladder runs
again from attempt one; a Core that died inside the window left the current episode running, the ladder resumes at
its next attempt, and when the episode's `restartAttempts` are spent the block is permanent until the user acts — a
`config.apply`, a `core.start`, or lifting the block per §12. The two readings answer two different questions and the
field is the second one's: a hibernating machine that wakes with a Core that has been dead for hours still earned
its fresh budget the moment it died past the window, while a Core that crash-loops once a minute spends one budget
and then sits in the visible block. `connector/internal/supervisor` enforces exactly this: `watch` re-arms the
budget only for a child that survived the window, hands the ladder the attempts already spent otherwise, and a
spent episode's next death re-publishes the verdict without spawning.

Visibility splits in two places, and the split is deliberate:

- `notify` gates the *ladder*. A death that recovers on attempt two must not raise a system notification on a
  machine whose user saw nothing but a slow page.
- The terminal block is surfaced whatever `notify` says — ADR-0009 rule 7, and
  `packages/schema/connector-settings.schema.json#/properties/proxy/properties/notificationLevel` refuses `silent`
  for a blocked state.

Two things must not happen while the ladder runs:

1. **No probes.** `server.test` answers `CoreNotRunning` — *"probe would report success against a stale inbound"*
   (`packages/schema/fixtures/protocol/failure-block.jsonl`, frames `seq` 9 to 10). A cached or best-effort health
   result during a block is a false statement about connectivity, and ADR-0008's replay rule is exactly why the
   answer must be the error rather than the last good number.
2. **No silent config rewrite.** A restart uses the last applied hash; if the file and the compiled intent
   disagree that is `config.drift` (`packages/schema/events.schema.json#/$defs/name`) and it resolves by
   recompiling from intent, never by trusting the on-disk bytes
   (`docs/architecture/27-config-compilation-pipeline.md` §3).

## 5. `serviceTeardown`: a bounded decision delay, not a fail-open

The Connector is gone, so nothing arrives from it. The trigger is the extension's own liveness rule
(`packages/schema/protocol/framing.json#/liveness`) or the closed port, and the resolution is `block` — after a
window in which the current setting is left alone.

Why holding is not the leak it looks like: containment says Core died with its parent
(`docs/architecture/21-system-context.md` §5), so the held setting points at a closed port, which *is* a block.
The grace window therefore delays only the *declaration*. The state that genuinely needs it is the one containment
was supposed to make impossible: `contained: false`, an unmanaged Core still serving. For that window the design
accepts "working, nobody driving" because severing on a detection artefact is worse than a few seconds of
unmanaged tunnel — and then severs on schedule, because a Core holding credentials with no controller is the state
ADR-0011 and `docs/architecture/32-secrets-keystore-masking.md` forbid.

The two timers are not redundant, and the reading that makes them coherent is the one
`packages/schema/failure-policy.schema.json#/properties/serviceTeardown/properties/graceSeconds/description`
states: `graceSeconds` (0–900, default 60) is **per episode**, `hardCapSeconds` (1–86 400, default 3600) is
**cumulative**. Without the cap, a Connector that starts and dies every 50 seconds buys itself a fresh grace
window forever and the machine never reaches a declared state.

`then` admits one word. `restore` was in that enum and is removed by this document: `serviceTeardown` is a
Connector-triggered plane, so a `restore` there is a route from "the Connector failed" to the browser's
pre-NEXORA setting, which is `direct` or `system` — the forbidden outcome, reached through a field named after
recovery. The rejected case lives in `packages/schema/examples/invalid/failure-policy.teardown-restore.json` so
the enum cannot be widened back without failing a build. ADR-0009 rule 6 already said "hard-caps to block"; the
enum simply had not been told.

Upgrade and hibernation are both this plane, and both must keep the applied config rather than drop it: an
extension update closes the port and restarts the worker (`docs/architecture/21-system-context.md` §5), and after
sleep the first honest signal is a hash comparison, not a conclusion
(`packages/schema/protocol/framing.json#/reconnect`).

## 6. `extensionLifecycle`: the one plane whose trigger is the user

| Field | Values | Why |
|---|---|---|
| `onDisabled`, `onUpdated` | `restore` only | the user is removing the proxy by choice; a block held by a disabled extension can never be lifted, because the only writer of proxy state is off |
| `onBrowserStart` | `restore` \| `reapply`, default `reapply` | re-applying the last profile at startup is the behaviour the inherited code has today, and a proxy that silently stops working after a reboot is indistinguishable from a proxy that died |

The inherited startup path is `omega-target/src/options.coffee:264-274`, gated by
`omega-target-chromium-extension/src/coffee/background.coffee:12-16` and the `onStartup` listener two lines later
(`omega-target-chromium-extension/src/coffee/background.coffee:19-20`). NEXORA keeps the *outcome* and drops the
*input*: a 2-second `setTimeout` race that guesses whether this is a browser restart is not a decision input, so
`onBrowserStart` keys on the browser's own events instead.

This plane is also where the inherited code's two accidental fail-opens live, and both are forbidden here rather
than merely discouraged:

1. Any error while loading options applies `fallbackProfileName`, and that is `'system'`
   (`omega-target/src/options.coffee:69`, used at `omega-target/src/options.coffee:285-288`) — a corrupt or
   unreadable store therefore *unproxies the user* through `clear()`.
2. Deleting the profile currently in use applies the same fallback
   (`omega-target/src/options.coffee:469-471`) — a profile vanishes, traffic starts going out unmanaged.

NEXORA's rules: an error path may select `+block`, never `+direct` or `+system`; and a removal cannot strand the
browser, because a delete that is referenced elsewhere is refused with the references listed
(`packages/schema/protocol/commands/profile.remove.json#/invariants`) while a removal of the active profile
resolves through an explicit `config.apply` over the remaining set — an empty remaining set is `BadRequest`, not a
fallback.

`onUpdated` has one more constraint from ADR-0014: an update that runs a migration must restore/re-apply *after*
the migration commits. A failed migration rolls the envelope back and leaves the proxy exactly as it was, because
a half-migrated profile set and a live proxy setting are two truths about one session.

## 7. The block primitive

`packages/schema/failure-policy.schema.json#/properties/block` is a FixedProfile aimed at a loopback port nothing
listens on. On Chromium it becomes `mode: 'fixed_servers'`
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:43-45`) with the target in
`rules.fallbackProxy` for a SOCKS scheme (`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:63-64`);
on the PAC path the same profile compiles to a `SOCKS5 127.0.0.1:20900` line
(`omega-pac/src/profiles.coffee:82-87`).

Four properties of it are contracts, not implementation notes.

**There is no bypass list, and that absence is the point.** `_fixedProfileConfig` copies a profile's bypass
conditions into `rules.bypassList`
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:68-72`), and the shipped default
profile carries `127.0.0.1`, `::1` and `localhost` there (`omega-target/src/default_options.coffee:15-27`). A
block that inherits those conditions blocks everything except localhost — which reads like a bug report no one
will file because it looks like the proxy "partly working". So `block` has no `bypassList` field, and
`verify.mjs` asserts the absence of the key rather than trusting the shape of the default.

**The scheme decides whether a name is resolved while blocked.** `PROXY`, `HTTPS` and the `SOCKS` (v4) keyword
forms mean the browser resolves first and hands over an address; `SOCKS5` means it hands over the hostname to an
endpoint that will not answer (`omega-pac/src/profiles.coffee:53-58`). A block via `http` therefore performs a
DNS lookup for every page the user opens, on a network where the lookup itself is the thing being hidden. That is
why the shipped default is `socks5` (`packages/schema/examples/failure-policy.default.json#/block/scheme`), why
`scheme` admits only `http` and `socks5` (`packages/schema/failure-policy.schema.json#/properties/block/properties/scheme` — the v4 form would resolve locally by
definition), and why the legacy compatibility fallback that emits `SOCKS5 …; SOCKS …`
(`omega-pac/src/profiles.coffee:84-85`, disabled in this tree at `omega-pac/src/profiles.coffee:14-22`) must not be
re-enabled for a block: its second keyword quietly reintroduces local resolution. Chromium's exact DNS disposition
in `fixed_servers` mode is **INFERRED** here — no browser was run — and is a `LIVE` assertion, not a claim.

**`assertClosed` is a precondition, not a hope.** Probing first, then writing the setting, is what prevents the
pathological case: a block aimed at a port some other local process owns turns that process into the user's proxy,
receiving every hostname. If the probe finds a listener, the block is refused and the state reported is the
refusal, not a "block". `packages/schema/failure-policy.schema.json#/properties/block/properties/assertClosed`
defaults to `true`, and an implementation that skips the probe has changed the security model, not a detail.

**An empty FixedProfile is a leak, not a broken block.** If neither a fallback nor any per-protocol proxy exists,
`_fixedProfileConfig` sets `mode: 'direct'`
(`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:65-66`). So a block whose host or
port failed to render does not fail closed — it fails *open*, quietly, from a "harmless" empty object. The three
fields `block` requires
(`packages/schema/failure-policy.schema.json#/properties/block/required`) are required for that reason, and the
block profile is always synthesised from this document rather than from a template with optional fields.

**`+block` is a NEXORA builtin, not an inherited one.** This tree has exactly two builtins, `+direct` and
`+system` (`omega-pac/src/profiles.coffee:34-44`). `+block` joins them so that a SwitchProfile rule, a
`-startupProfileName` or a popup action can *name* a deliberate block instead of approximating one with a profile
the user made. `docs/architecture/28-routing-model.md` §6 listed it as inherited; that row is corrected by this
document (§15).

## 8. Port discipline

| Range | Owner | Shipped value |
|---|---|---|
| inbound allocation | the Compiler, from `packages/schema/examples/settings.default.json#/ports/range` | 20100–20899 |
| block | `packages/schema/examples/failure-policy.default.json#/block/portRange` | 20900–20999 |

They must be disjoint, and `docs/architecture/27-config-compilation-pipeline.md` §4 already points
here for the rule. The reason is a specific sequence, not hygiene: if the block range were allocatable, then after
a restart the Compiler could bind exactly the port the browser believes is closed, and "blocked" becomes a live,
unmanaged tunnel that the user has been told is severed. The two shipped documents are checked for disjointness by
`verify.mjs` today; a user-supplied overlap is a semantic conflict and must be refused by `config.apply` with
`UnsupportedCombination` rather than `BadRequest`, because nothing about either document is malformed.
`Verified: NO — deferred to Phase 2`.

Within the block range, one port is used at a time and it is chosen deterministically, not round-robin: the port
in force is part of what reconnect reconciliation compares, so a rotating block port would look like drift on every
reconnect. Which port is in force, and why, belongs in the `failure-policy` section of
`docs/architecture/36-observability.md`.

## 9. Fail-open: one exception, and what it does not weaken

`failOpen` exists only as a per-profile override
(`packages/schema/failure-policy.schema.json#/$defs/ProfilePolicy`), and the schema makes it unwriteable without
evidence: the `if/then` in that subschema's `allOf` requires a `confirmToken` whenever `action` is `failOpen`, so a
validator in Go, in TypeScript or in a harness rejects an unconfirmed fail-open identically
(`packages/schema/examples/invalid/profile.failopen-without-confirm.json` is the case that proves it).

**The global enum is now `["block"]`, and this document is the amendment.** ADR-0009 recorded the gap in its own
Evidence section: rule 1 and rule 2 forbid a global fail-open while the schema admitted one. The ADR left the
choice between "narrow the global enum" and "require a token globally" to Phase 2, on the grounds that either is a
change to an agreed contract. It is narrowed here, and the reason is that a token in a *global* document proves
nothing: any code path that can write the document can write the token, and there is no user action attached to the
other profiles' traffic. A per-profile token at least names the profile the exception belongs to. The rejected
case `packages/schema/examples/invalid/failure-policy.global-failopen.json` keeps the enum from widening back.

Scope of `forbiddenOutcomes`, stated precisely, because the schema cannot express it: the three words are forbidden
as *unchosen* resolutions — any default, any fallback, any accidental path, any global path. A confirmed,
per-profile, permanently visible `failOpen` is the exception ADR-0009 rule 2 grants, and the assertion in
`verify.mjs` is scoped to the global planes for exactly that reason. Reading the prohibition as absolute would
make `$defs/ProfilePolicy` unsatisfiable; reading it as a preference would delete the product's reason to exist.

Three consequences of the exception being per-profile rather than global:

- Two profiles on one machine can disagree, and the *active* one wins. The badge and the popup must therefore say
  "fail-open in force for this profile" whenever it is — ADR-0009 rule 3, and the reason the UI row in
  `docs/architecture/37-ui-architecture.md` §4 is a contract rather than cosmetics.
- The confirmation is not remembered across an import or a sync restore: the token is evidence about a user action
  on *this* machine, so migration and import strip it and the exception re-asks
  (`docs/architecture/35-schema-versioning-migration.md` §13).
- `LIVE-11` runs both halves: kill Core and assert no direct/system profile is applied and the block is visible;
  run the same test with a confirmed `failOpen` and assert traffic does leave — and that the user was told.

## 10. Who acts: detection in two processes, actuation in one

1. **The Connector** owns the Core ladder (§4) and the events `core.exited`, `core.blocked`
   (`packages/schema/events.schema.json#/$defs/name`). It does not own the browser's proxy setting and has no
   method that touches it — the nineteen-method surface in `docs/architecture/26-command-surface.md` §4 contains
   none, by ADR-0004.
2. **The extension** is the only writer of `chrome.proxy`. It inherits the single-funnel property the audit
   identified in the legacy code (`omega-target/src/options.coffee:654`, `docs/audit/06-proxy-application.md` §11)
   rather than adding a second writer, which is also why a block is an ordinary profile application and can be
   reconciled by the same machinery.
3. **Neither is awake**: the worker is evicted and the Connector is gone. Nothing is written, the applied setting
   keeps pointing at a loopback port nobody owns, and traffic stops. This is the case that would be fatal in a
   fail-open design and is merely ugly in this one — rule 2 of §1 again: the safe state is the *default* state, so
   a missed timer cannot leak.
4. **`core.blocked` is a verdict, not an act.** The Connector emits it because it is the process that knows Core is
   gone; the extension performs it. If the extension cannot be reached the event is retained and replayed on
   reconnect — `packages/schema/events.schema.json#/$defs/catalog/properties/retained` exists for exactly this
   window, because a service worker can die between the failure and the UI that must show it.

## 11. Timers that survive eviction

`setTimeout` is not a timer in MV3; the only browser-persisted one is an alarm. The design does not need a
persisted countdown, and that is the interesting part:

- The *deadline* is arithmetic on data the Connector already persists: `lastExit.at`
  (`packages/schema/protocol/commands/core.status.json#/result/properties/lastExit`) plus `graceSeconds` /
  `hardCapSeconds` from the policy document. Every worker wake — `onStartup`, an alarm, a port open, a UI message —
  recomputes it. No new envelope key, no second copy of the truth, nothing to migrate
  (`docs/architecture/34-data-split.md` §10 row 5 — ADR-0010: intent in the extension, observed state on the Connector).
- The *alarm* is the wake-up for the case where nothing else wakes it. Its granularity is coarser than the schema's
  floor: `graceSeconds` admits 0 while the browser's minimum alarm period is tens of seconds
  (`docs/architecture/38-compatibility-matrix.md`, `Verified: NO`). The imprecision costs only the deliberate
  severing of an unmanaged-but-serving Core (§5) — never the safety of the blocked state itself, because §10.3
  already covers inaction.
- Cumulative accounting for flapping Connectors (§5) is likewise derived: the Connector records each start and
  exit, so the extension asks rather than remembers.

## 12. Lifting a block

A block is lifted by exactly three things, and none of them is "some time passed":

1. a **verified** restart: Core answers `running: true` with the `configHash` the extension last applied
   (`packages/schema/protocol/commands/core.status.json#/result/properties/configHash`), the loopback inbound
   accepts, and `connector.health` reports `ok` or `degraded` — never `unknown`, never `blocked`
   (`packages/schema/protocol/commands/connector.health.json#/result/properties/state`);
2. an **explicit user action** — a profile selection or `core.restart` with its `confirm` argument, which is a
   converge, so pressing it twice is not a bug (`docs/architecture/26-command-surface.md` §2);
3. `extensionLifecycle` handing the browser back to the user (§6).

Forbidden lifts: a diagnostics read, a subscription refresh, a reconnect that did not compare hashes, a
`config.apply` that reported `unchangedBecauseHashMatched`
(`packages/schema/protocol/commands/config.apply.json#/result/properties/unchangedBecauseHashMatched`) while the
state machine was still in `coreDeath`. The lift is also ordered *after* the inbound is proven listening, because
re-applying the proxy setting to a Core that accepts but does not tunnel is the anti-fake-success case
`docs/verification/live-matrix.md` `LIVE-04` exists to catch, and the recorded `overall: "not-proxied"` probe frame
is the shape of the answer that must keep a block in force.

## 13. Invariants, and what proves each one today

| Invariant | Checked now | Phase-2 row |
|---|---|---|
| No global resolution is direct, system or clear | `verify.mjs`: *connector-triggered plane resolves to no forbidden outcome*, *global coreDeath.action is block-only*, *serviceTeardown.then is block-only* | `LIVE-11` |
| `forbiddenOutcomes` cannot be narrowed by a document | the `contains` branches in `packages/schema/failure-policy.schema.json#/allOf` plus *every forbidden outcome is pinned element-by-element* | — |
| The per-profile exception still exists and is still gated | *failOpen is still gated by a required confirmToken* , *the confirmed per-profile fail-open exception still exists* | `LIVE-11` second half, UI parity row |
| A block is loopback-only | *the block target is loopback-only (ADR-0006)*, from `packages/schema/failure-policy.schema.json#/properties/block/properties/host` | `LIVE-03` |
| A block has no bypass list | *the block primitive has no bypass list to leak through* | `LIVE-11` |
| A block port is never an inbound | *the block range and the inbound allocation range are disjoint in the shipped documents* | refusal on user-supplied overlap (§8) |
| The block is proven closed before it is written | `packages/schema/failure-policy.schema.json#/properties/block/properties/assertClosed` default `true`, asserted as *assertClosed defaults to true* | runtime probe, `Verified: NO` |
| The failure scene in the fixtures ends in a block, not a direct profile | *the recorded failure scene blocks after Core exits*, *no frame of the failure scene resolves to a direct or system gesture* | — |
| Inaction is fail-closed | not expressible as a schema rule; rests on §1 rule 2 and §10.3 | `LIVE-11`, `LIVE-13` |
| A grace window cannot outlive its hard cap | prose + `graceSeconds`/`hardCapSeconds` bounds | `LIVE-13` |
| A death inside the stability window continues the episode; a death past it earns a fresh budget | `connector/internal/supervisor` re-arms the budget only for a child that survived `coreDeath.stabilityWindowSeconds`, resumes the ladder mid-episode otherwise, and a spent episode's next death re-publishes the block without spawning; the two supervisor tests measure both halves | Verified |
| Health never says `ok` while blocked | `packages/schema/protocol/commands/connector.health.json#/result/properties/state` is a closed enum and its description says `blocked` is working-as-designed | `LIVE-04` |

The first eight rows are the ones ADR-0009's Verification section said did not exist. They exist now, they run in
the same command as everything else (`npm --prefix packages/schema run verify`, 900 checks), and the ADR is updated
to stop claiming otherwise (§15).

## 14. Non-goals

Not this document's to decide: how Core is fetched and verified (`docs/architecture/33-binary-integrity.md`); what
a keystore failure means for a command (`docs/architecture/32-secrets-keystore-masking.md`); per-server health and
failover among *living* servers (`docs/architecture/22-component-contracts.md` §8); the UI's wording
(`docs/architecture/37-ui-architecture.md` §4); the wire protocol's reconnect policy itself
(`docs/architecture/25-native-messaging-protocol.md` §11). Explicitly out of the model, per ADR-0009's rejected
alternatives: no firewall or OS-network manipulation, no tab suspension, no "safe mode" profile, no auto-restart of
the browser, and no browser-plane retry logic that would make a block look like a slow page.

## 15. Amendments this document makes, and what it leaves open

Amendments — each is a change to a contract written before the runtime shape existed:

1. `packages/schema/failure-policy.schema.json#/properties/coreDeath/properties/action`: enum
   `["block", "failOpen"]` → `["block"]`. Closes the gap ADR-0009 recorded rather than resolved silently.
2. `packages/schema/failure-policy.schema.json#/properties/serviceTeardown/properties/then`: enum
   `["block", "restore"]` → `["block"]` (§5).
3. The top-level `allOf`, which used to say "a global `failOpen` must notify", became unreachable with (1) and is
   replaced by three `contains` branches pinning `forbiddenOutcomes` element by element. `minItems: 2` alone let a
   document drop two of the three.
4. Three new rejected cases: `failure-policy.global-failopen.json`, `failure-policy.teardown-restore.json`,
   `failure-policy.narrow-forbidden.json`.
5. A new `verify.mjs` section implementing the assertion ADR-0009 said was missing, plus the disjointness and
   bypass-absence checks.
6. `docs/architecture/21-system-context.md` §10's Core-crash row is corrected: it described auto-restart first and
   fail-closed "if the restart fails", which contradicts the recorded scene and §4 above.
7. `docs/architecture/28-routing-model.md` §6 is corrected: `+block` is a NEXORA builtin, not an inherited one
   (§7).
8. `docs/adr/0009-fail-closed-by-default.md` Evidence and Verification sections are updated: the gap it recorded is
   closed, and the `forbiddenOutcomes` assertion it said did not exist now does.
9. `packages/schema/failure-policy.schema.json#/properties/coreDeath/allOf`: ten `if/then` branches tie
   `restartBackoffSeconds`' length to `restartAttempts` exactly (ODEC-19, answered by its third option — the two
   fields agree in data, so the disagreement cannot be authored). `connector/internal/supervisor` mirrors the
   refusal for a wiring that bypassed the schema. The repeat-the-last and stop-early readings are rejected.

Open items, carried to `docs/architecture/40-open-decisions.md`:

- **`contained: false` without an exit.** Core alive, unmanaged, and unsupervised is reported by two different
  mechanisms (health check and integrity event) and no plane owns it. §5 handles the episode; the *classification*
  is open.
- **Two browser profiles, one Connector.** Whose policy document decides the block is unresolved, and it is the same
  ownership question `CoreAlreadyRunning` raises in `docs/architecture/21-system-context.md` §6.
- **The alarm floor** and Chromium's DNS disposition while blocked are both `INFERRED` here and belong to the
  compatibility matrix.
- **Feed egress while blocked.** A `subscription.refresh` must still work when traffic is blocked — the Connector
  fetches, not the browser plane (`docs/architecture/29-subscriptions-and-servers.md` §2) — so "blocked" is defined
  as *the user's browsing traffic*, never as a network-wide policy. Whether that boundary needs its own field to
  survive an enterprise reading of the document is open.
