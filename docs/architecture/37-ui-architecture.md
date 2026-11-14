# 37 — The screen as a contract: surfaces, states, tokens, and what a button is allowed to promise

> Purpose: `docs/architecture/22-component-contracts.md` §12 and §13 give two UI components normative
> contracts that point here for everything they refuse to own, and eight documents deferred concrete facts
> to a file that did not exist yet: which surfaces exist and in which document, what a screen may name as a
> command, what conditions it must be able to show, where an error is allowed to appear, how a theme reaches
> a shadow root, which locales exist and at what completion, what the popup owes a keyboard and a sleeping
> service worker, and what gates the cut-over. Meanwhile the brief forbids the most likely failure of all —
> `docs/PROJECT-STATE.md` §0 constraint 11, "a button in the interface does not mean a finished feature".
> This document states those facts, and `packages/schema/presentation.json` states them again as data,
> cross-read against the allowlist, the event catalogue, the error taxonomy, the command documents, the
> failure-policy schema, the inherited theme catalogue and the inherited `.po` tree.

Status: Accepted. Normative data: `packages/schema/presentation.json`,
`packages/schema/presentation.json#/surfaces`, `packages/schema/presentation.json#/states`,
`packages/schema/presentation.json#/errorPlacements/values`,
`packages/schema/presentation.json#/controls/notShipable`,
`packages/schema/presentation.json#/themes/catalogue`,
`packages/schema/presentation.json#/i18n/literalDebt`,
`packages/schema/presentation.json#/popup/coldStart/ladderTotalMs`,
`packages/schema/presentation.json#/parity/rows`, `packages/schema/presentation.json#/budgets`,
`packages/schema/presentation.json#/forbidden`, `packages/schema/presentation.json#/openItems`,
`packages/schema/protocol/command-allowlist.json#/commands`,
`packages/schema/events.schema.json#/$defs/name`,
`packages/schema/protocol/error-taxonomy.json#/codes`,
`packages/schema/failure-policy.schema.json#/$defs/ProfilePolicy/properties/coreDeath/properties/action`,
`packages/schema/protocol/commands/diagnostics.get.json#/args/properties/include/items/enum`,
`packages/schema/protocol/commands/connector.health.json#/result/properties/state`,
`packages/schema/protocol/commands/server.test.json#/result/properties/overall`.
Evidence base: `docs/audit/08-ui-i18n-and-themes.md`,
`docs/architecture/22-component-contracts.md` §12 and §13, `docs/architecture/20-target-repository-layout.md`
§7, ADR-0016, ADR-0017, `docs/verification/live-matrix.md` (LIVE-04, LIVE-11, LIVE-14, LIVE-17),
`docs/PROJECT-STATE.md` §0.

---

## 1. What this document owns, and what it refuses

Owns, and nothing else does (`packages/schema/presentation.json#/scope/owns`):

1. **The surface list** — which NEXORA screens exist, in which document each one lives, which commands it may
   name and which events it renders. `docs/architecture/22-component-contracts.md` §12 and §13 give the
   components; this gives the screens. The distinction matters because a component can be split without moving
   a pixel, while a surface is what a user can point at.
2. **The state vocabulary** — the closed set of conditions a NEXORA control must be able to show, and what
   each one must never claim (§4).
3. **The control rules** — which commands need a confirmation, which can answer before acting, and which
   controls will not ship at all (§5).
4. **The token pipeline** — how 164 inherited palettes become CSS custom properties inside a shadow root, and
   the defects in the catalogue that a pipeline would otherwise inherit silently (§6).
5. **The i18n contract at the UI boundary** — the numbers, the no-RPC rule, and the literal debt (§7).
6. **The popup's obligations** — keyboard map, cold-start ladder, whole-document states, geometry (§8).
7. **The parity matrix and the cut-over gate** (§9).
8. **The budget numbers** the parity gate carries (§9).
9. **The UI prohibitions** — the list a reviewer checks a screen against (§10).

Refuses (`packages/schema/presentation.json#/scope/doesNotOwn`), because each of these already has a home and
a second home is how a contract dies: the frame and the method list
(`docs/architecture/25-native-messaging-protocol.md`, `packages/schema/protocol/command-allowlist.json`), a
command's argument shape (`packages/schema/protocol/commands/`), the wording of a message (the `.po` tree,
ADR-0017), the routing decision (`docs/architecture/28-routing-model.md`), the store layout
(`docs/architecture/34-data-split.md`, `packages/schema/protocol/data-ownership.json#/stores`), the mask and
the sinks (`docs/architecture/36-observability.md`), the migration steps
(`packages/schema/migrations.json`), the pin (`packages/schema/pins.schema.json`), and the live verification
rows (`docs/verification/live-matrix.md`).

## 2. The eight questions that were deferred here

| Asked by | Deferred fact | Answered |
|---|---|---|
| `docs/architecture/22-component-contracts.md` §12 | the typed message boundary between a UI surface and the shell | §3 |
| `docs/architecture/22-component-contracts.md` §12 invariant 3 | that all copy comes from the compiled catalog | §7 |
| `docs/architecture/30-failure-policy.md` §9 | whether a fail-open notice on screen is a contract or cosmetics | §4, `failOpenInForce` |
| `docs/architecture/28-routing-model.md` §5 | that `<local>` must be rendered as an expansion, not as a token | §4, `localExpanded` |
| `docs/architecture/32-secrets-keystore-masking.md` §14 and §16 | the wording of every secret refusal, and where a legacy authenticated proxy goes | §4, §5 |
| `docs/architecture/34-data-split.md` §11 and `docs/architecture/35-schema-versioning-migration.md` §10 | the words "restore required" and "what will change" | §4, `restoreRequired` |
| `docs/architecture/36-observability.md` §11 | the wording a diagnostics panel shows when an export was cut | §4, `truncatedExport` |
| `docs/verification/live-matrix.md` | "Popup opens in under N ms", excluded from the LIVE rows | §9 |

Every row in that table is a fact some other document needed and could not own. `Verified: NO — deferred to
Phase 2` for all of them, in the sense that the screen that would show them does not exist yet; the
declarations below are what makes "does not exist yet" checkable rather than a matter of opinion.

## 3. Surfaces, documents, and the typed message boundary

Three documents (`packages/schema/presentation.json#/documents`):

- **`nexora-options.html`** — a new MV3 options page hosting the editing surfaces. It is a separate document
  from the legacy options page rather than a section inside it, because ADR-0016 rule 4 forbids mounting a
  Svelte root inside AngularJS-managed DOM and the legacy page boots through a serial script loader
  (`omega-web/src/coffee/options.coffee:2-10`) that would compete with any island for the document.
- **`nexora-popup.html`** — the two-second surface, exactly one surface, for the same reason the legacy popup
  is its own document (`omega-web/src/popup/index.html:1`).
- **`legacy-mount`** — not a page. A named slot inside the legacy options page: at most three custom elements,
  each registered with a `nexora-` prefix. The first two are `connector-status` and `core-health`, and the
  count is capped because every slot is a second reactive runtime inside a document owned by AngularJS.

Surfaces (`packages/schema/presentation.json#/surfaces`) and what each may say over the pipe:

| Surface | Document | Commands it may name | Events it renders |
|---|---|---|---|
| `dashboard` | `nexora-options.html` | `connector.status`, `connector.health`, `core.status`, `config.snapshot`, `profile.validate` | `core.started`, `core.exited`, `core.blocked`, `connector.connected`, `connector.disconnected`, `config.applied` |
| `servers` | `nexora-options.html` | `server.test`, `config.apply`, `profile.validate`, `connector.health` | `server.health.changed` |
| `subscriptions` | `nexora-options.html` | `subscription.refresh`, `profile.import`, `config.apply`, `connector.health` | `subscription.updated`, `subscription.failed` |
| `routing` | `nexora-options.html` | `profile.validate`, `config.apply`, `diagnostics.get` | `routing.decision` |
| `settings` | `nexora-options.html` | `core.start`, `core.stop`, `core.restart`, `config.apply`, `connector.version`, `keystore.purge`, `profile.import`, `profile.remove` | `core.restart`, `integrity.mismatch` |
| `diagnostics` | `nexora-options.html` | `diagnostics.get`, `connector.health`, `core.status`, `config.snapshot` | `config.drift`, `keystore.unavailable` |
| `migration-review` | `nexora-options.html` | `profile.import`, `config.apply`, `diagnostics.get` | — |
| `secret-entry` | `nexora-options.html` | `profile.import`, `keystore.purge` | `keystore.unavailable` |
| `popup` | `nexora-popup.html` | `config.apply`, `connector.status`, `core.status`, `config.snapshot` | `core.blocked`, `connector.connected`, `connector.disconnected`, `server.health.changed` |
| `connector-status` | `legacy-mount` | `connector.status`, `connector.version` | `connector.connected`, `connector.disconnected` |
| `core-health` | `legacy-mount` | `core.status`, `connector.health` | `core.started`, `core.exited`, `core.blocked` |

**The boundary.** A surface issues commands, never frames
(`docs/architecture/25-native-messaging-protocol.md`), and it issues them through ExtensionShell: UI → shell →
one `ConnectorClient` (`docs/architecture/22-component-contracts.md` §12 invariant 2). Two allowlisted commands
appear nowhere in the table above and that is the point —
`packages/schema/presentation.json#/sessionCommands` names `connector.hello` and
`connector.events.subscribe` as the shell's own, because a surface that could open a session would own a
second reader of the same pipe and a second place to leak a payload. The harness asserts this as
"the commands no surface names are exactly the two the shell owns, and the split is declared", which is the
only way the absence stays visible: an unlisted command is invisible, a listed absence is not.

**The two read-only islands.** `connector-status` and `core-health` exist so that a user who never opens the
NEXORA pages can still see whether a Connector is present at all. Without them the honest answer to "is the
tunnel working" requires opening a second options page, which is how a new UI becomes a rumour. They are
read-only by construction: the commands they name are reads, and the events they render are the ones the shell
already holds. The legacy page's experimental badge is the row they inherit
(`omega-web/src/options.jade:20-21`).

**Import boundary.** Nothing under `nexora/extension/src/ui/**` may import from `omega-*`, and the one
permitted exception is type derivation from `packages/schema`
(`docs/architecture/20-target-repository-layout.md` §7). The check is the `VER-02` step of
`.github/workflows/verify.yml`, and it is green over the real UI sources — the UI package exists, and the scan is
no longer vacuous (`VER-02` closed 2026-11-14, `docs/PROJECT-STATE.md` §4.71).

## 4. The state vocabulary

Eighteen conditions, closed (`packages/schema/presentation.json#/states`). A state is not a colour or a
component; it is a thing that can be true about the system which the screen is obliged to distinguish. Each
one is anchored — to a command result, an error code, an event, a policy pointer, or a named origin — so that
no screen can render a condition nothing produces, and each one carries a sentence saying what it must never
claim, because the dangerous failure of a UI is not a missing state but a misleading one.

| State | Anchored to | What it must never claim |
|---|---|---|
| `loading` | origin: client | that it is waiting for something a read will answer |
| `empty` | origin: client | that nothing exists, when nothing is filtered in |
| `unknown` | event `connector.disconnected` | that absence of an answer is a healthy answer |
| `stale` | event `connector.disconnected` | that the last known state is the current one |
| `error` | the taxonomy in `packages/schema/protocol/error-taxonomy.json#/codes` | an upstream message rendered verbatim |
| `blocked` | event `core.blocked` | failure: `connector.health` declares `blocked` as fail-closed working as designed (`packages/schema/protocol/commands/connector.health.json#/result/properties/state`) |
| `failOpenInForce` | `packages/schema/failure-policy.schema.json#/$defs/ProfilePolicy/properties/coreDeath/properties/action` | quiet: ADR-0009 makes an unmarked fail-open a defect, so a badge is not enough |
| `degradedSecret` | error `SecretRedacted` | that the profile is usable, or that the value is recoverable from the screen |
| `keystoreUnavailable` | error `KeystoreUnavailable` | that a retry will help on a machine where the keystore is the problem |
| `restoreRequired` | error `SchemaMismatch` | deletion: `docs/architecture/35-schema-versioning-migration.md` §10 holds the envelope read-only |
| `dryRunPlan` | `config.apply` and its `dryRun` answer | past tense — see §10 |
| `unrepresentable` | `config.apply#/result/properties/unrepresentable` | that an intent was applied when the compiler refused it |
| `localExpanded` | `profile.validate#/result/properties/inboundPlan` | the token `<local>` as if Core understood it (`docs/architecture/28-routing-model.md` §5) |
| `partialImport` | `subscription.refresh#/result/properties/refreshed` | that a feed updated cleanly, when part of it was refused (`docs/architecture/29-subscriptions-and-servers.md` §5) |
| `disabledPhase2` | the absent backend named in §5 | a promise; this is constraint 11 made reviewable |
| `truncatedExport` | `diagnostics.get#/result/properties/truncated` | completeness (`docs/architecture/36-observability.md` §11) |
| `integrityRejected` | error `IntegrityMismatch` | a retry, an ignore, or "use anyway" (`docs/architecture/33-binary-integrity.md` §12) |
| `busy` | error `Busy` | that the action failed, when nothing was dispatched at all (`docs/verification/live-matrix.md` LIVE-17) |

Two of these deserve the argument rather than the row.

**`unknown` and `stale` are separate states** because the shell has two different answers. No session yet is
`unknown`; a session that was established and then lost is `stale`, and the second one carries a value the
user may act on. `docs/architecture/22-component-contracts.md` §12's mode of failure says the UI shows "the
last known state with an explicit staleness marker, never a silently stale connected", and ADR-0010's plane
split is why a read of the intent stores can be perfectly correct while the applied state behind it is a
minute out of date. Collapsing the two into one "disconnected" label would let a stale applied-config hash
masquerade as a current one.

**`blocked` is not an error.** `connector.health` returns five states, and the schema's own description says
`'blocked'` means the fail-closed policy is actively rejecting traffic — working as designed, not healthy
(`packages/schema/protocol/commands/connector.health.json#/result/properties/state`). A screen that colours it
red teaches the user that the safety mechanism is a fault, which is exactly the behaviour the next incident
needs them not to have. The paired obligation is `failOpenInForce`, whose whole purpose is that the *opposite*
configuration must not be quiet.

**Placements.** `packages/schema/protocol/error-taxonomy.json#/codes` gives every code a `surface` field
naming where it is shown. That field's vocabulary was undeclared until this document, which is a defect of
exactly the kind the data home exists to catch: a new code could have been added with a placement no screen
implements. The closed list is `packages/schema/presentation.json#/errorPlacements/values` — `banner`,
`status`, `error`, `diagnostics`, `none` — and the harness asserts both directions: no code names a placement
the UI does not have, and no declared placement belongs to zero codes. `none` is a legitimate placement, not a
hole: `Busy` belongs to the refused action alone and gets no persistent place.

**Health is not proof.** The `servers` and `popup` surfaces render `server.health.changed`, and
`packages/schema/protocol/commands/server.test.json#/result/properties/overall` distinguishes `pass`,
`partial`, `fail` and `not-proxied` — the last one meaning something answered but the egress address was not
the expected one. A health dot that renders `not-proxied` as `fail` hides a leak inside a failure, which is
`docs/verification/live-matrix.md` LIVE-04's anti-fake-success frame pair and constraint 8 of the brief.

## 5. Controls: confirmation, dryRun, and the backend that does not exist

**Every control declares the command it issues.** A control whose command is absent, whose backend is Phase 2,
or whose answer cannot be had because there is no session renders disabled with a reason string in the language
of §4's `disabledPhase2` — never enabled, never silent (`packages/schema/presentation.json#/controls/rule`).

**The derived sets.** Two lists in this contract are not remembered; they are derived from the command
documents on every run, and the harness asserts equality in both directions ("the confirmation set is derived:
exactly the commands that declare a confirm argument", "the dryRun set is derived: exactly the commands that
declare a dryRun argument"):

- `packages/schema/presentation.json#/controls/confirmationRequired` — `core.restart`
  (`packages/schema/protocol/commands/core.restart.json#/args/properties/confirm`, whose description says a
  restart drops live connections) and `keystore.purge`
  (`packages/schema/protocol/commands/keystore.purge.json#/args/properties/confirmToken`).
- `packages/schema/presentation.json#/controls/dryRunAvailable` — `config.apply`
  (`packages/schema/protocol/commands/config.apply.json#/args/properties/dryRun`), `profile.remove` and
  `keystore.purge` (`packages/schema/protocol/commands/keystore.purge.json#/args/properties/dryRun`).

The reason these are derived rather than written down is the same as everywhere else in this repository: a
command document that grows a `dryRun` argument silently invalidates a prose list, and the UI is the place
where the silent invalidation is felt. Where the command *can* answer before acting, the UI must show the
answer before it lets the user ask for the act; where it cannot, the confirmation dialog is the only gate and
its copy carries the consequence. `keystore.purge`'s `dryRun` answer is the interesting case, because
`docs/architecture/32-secrets-keystore-masking.md` §7 requires it to report `retained` with a reason rather
than quietly holding an entry a profile still references.

**The not-shipable list** (`packages/schema/presentation.json#/controls/notShipable`) is the part of
constraint 11 that can be reviewed. Seven controls that a reasonable contributor would add and that this
project refuses, each with the evidence that makes it a refusal rather than a preference:

| Refused control | Refuses because |
|---|---|
| a silent Core update | `packages/schema/pins.schema.json#/properties/silentSelfUpdate` is a `const false`; a toggle that cannot do what it says is not a control |
| fetching a feed through another proxy | no schema field and no address guard (`docs/architecture/29-subscriptions-and-servers.md` §13) |
| per-server certificate pinning and `minVersion` | `Server` has no such field, so a feed carrying `pinCert` is refused today by the compiler |
| the twenty-one unsupplied locales as menu entries | ADR-0017 rule 4 forbids promising an unreleased language in UI copy |
| removing `all-urls` from the manifest | the grant holds only while the Connector owns every outbound fetch (`docs/architecture/31-security-model.md` §13) |
| a raw-CSS editor for NEXORA surfaces | the inherited escape hatch writes arbitrary CSS into `-customCss` (`omega-target/src/options.coffee:90-91`) and the popup applies it with `innerHTML` (`omega-web/src/popup/js/style.js:10`); a NEXORA equivalent needs an ADR, not a textarea |
| importing a legacy authenticated HTTP/SOCKS proxy as a NEXORA server | the server model has no home for a browser-plane credential, so §4's `error` state names it and leaves it in the legacy profile |

A confirmation never re-uses the word "delete" for a hold
(`packages/schema/presentation.json#/controls/destructiveCopyRule`): §10 of
`docs/architecture/35-schema-versioning-migration.md` holds an unparseable envelope read-only and writes
nothing back, and a user whose next action after "deleted" is the same as after "held" has been told the wrong
thing.

## 6. Themes as a token pipeline

The requirement is "светлая и тёмная темы" — light and dark. The inherited system already is a light/dark
token system, and measuring it is what turns that requirement from a feature into a parity obligation.

`omega-web/lib/themes/variable.css:1` opens a `:root` block declaring exactly **thirty-one** custom
properties: the sixteen base16 palette keys — `base00`, `base01`, `base02`, `base03`, `base04`, `base05`,
`base06`, `base07`, `base08`, `base09`, `base0A`, `base0B`, `base0C`, `base0D`, `base0E`, `base0F` — and
fifteen semantic ones: `defaultBackground`, `lighterBackground`, `selectionBackground`,
`highlightBackground`, `darkForeground`, `defaultForeground`, `lightForeground`, `lightBackground`,
`primaryColor`, `secondaryColor`, `positiveColor`, `negativeColor`, `infoColor`, `warningColor`,
`whiteColor`. `packages/schema/presentation.json#/themes/paletteKeys` and
`packages/schema/presentation.json#/themes/semanticTokens` carry the same two lists, and the harness asserts
they are the same set as the sheet — no extra token in the data, none missing from the CSS.

**The pipeline.** `packages/schema/presentation.json#/themes/pipeline` says it reads the `colors` object in
`omega-web/lib/themes/themes.json` and writes the thirty-one properties onto the host element of each surface.
It does not parse CSS, does not fetch a stylesheet, and does not copy one into `packages/ui`. The reason this
is possible at all is that CSS custom properties inherit into shadow roots, which is the one mechanism that
crosses the encapsulation ADR-0016 rule 5 requires. The four prohibitions are in
`packages/schema/presentation.json#/themes/pipeline/mustNot`: fetch one of the 164 stylesheets to paint a
surface, copy a theme's CSS into `packages/ui`, inject a legacy stylesheet into a shadow root, depend on
legacy CSS reaching into one.

**The catalogue, measured.** 164 entries in `themes.json`, each carrying exactly the sixteen base16 keys; 113
flagged dark and 51 not; 9 whose `name` differs from their key; 6 that carry an `order` field, which is why a
sorted dropdown is not a contract
(`omega-web/src/omega/controllers/master.coffee:329-335` builds the list from the JSON, and
`omega-web/src/omega/controllers/master.coffee:331` is where a key becomes the path `others/base16-<key>` —
the token pipeline reads the JSON and never needs the path). The selectable total is 168: the 164 dropdown
rows, three buttons, one empty selection (`omega-web/src/partials/theme.jade:5-21`).

That total needs a note because the measurement caught a real ambiguity: `default-light` and `default-dark`
are *both* dropdown rows and buttons, and the two files behind one name differ byte for byte — the dropdown
loads `omega-web/lib/themes/others/base16-default-light.css` while the button loads
`omega-web/lib/themes/default-light.css`. So a button is not a shortcut to the row that shares its name, and
`packages/schema/presentation.json#/themes/catalogue/defaultsSameAsPalettes` records that as `false` so nobody
"deduplicates" 168 into 166 by accident. `default-auto` is the third button and has no catalogue entry at all:
its mechanism is `@media (prefers-color-scheme: dark)` (`omega-web/lib/themes/default-auto.css:19`), which is
why Auto is a parity obligation rather than a new feature.

**The defects.** `packages/schema/presentation.json#/themes/measuredDefects` records seventeen hex values with
stray whitespace in exactly two palettes — `china-red-screen` (sixteen, a leading space before every hash) and
`tomorrow-night-blue` (one, a trailing space on `base08`). A pipeline that reads the JSON instead of the CSS
inherits these silently and produces invalid property values that fail as an invisible theme, in two of 164
palettes, which is a bug report nobody can reproduce. The reader trims and refuses a value that is not a
six-digit sextet after trimming; it does not repair the catalogue, because the catalogue is inherited content
and constraint 5 says the working parts stay.

**Coexistence.** The legacy theme screen keeps writing `-customCss`
(`omega-web/src/omega/controllers/master.coffee:359-360`) and the popup keeps applying it verbatim
(`omega-web/src/coffee/popup.coffee:89-90`). A NEXORA surface must not read that key, must not write it, and
must not be affected by what is in it — which is only expressible because the surface does not inherit the
legacy document's styles.

## 7. i18n: one source, two compile paths, no round trip

ADR-0017 decided the shape; this section carries the numbers, because the parity obligation is a count and a
count has to be re-measured.

- **Source of truth**: `omega-locales/en_US/LC_MESSAGES/omega-web.po`, 382 `msgid` lines. That is the file a
  new `msgId` is added to, and the harness recounts it ("the English source carries the number of msgids a
  compiled catalog has to cover").
- **Sources**: 27 locale directories, each with one `LC_MESSAGES/omega-web.po`
  (`packages/schema/presentation.json#/i18n/localeSources`).
- **Shipped**: seven `_locales` targets — `en`, `zh`, `cs`, `fa`, `ru`, `zh_CN`, `zh_TW` — compiled from six
  distinct `.po` inputs, because `zh` and `zh_CN` read the same file
  (`omega-target-chromium-extension/grunt/po2crx.coffee:6-7` and
  `omega-target-chromium-extension/grunt/po2crx.coffee:14-15`). The list is read out of the build task, not
  out of a document, by the assertion "the shipped locale set is the build task list, read from the task
  rather than from a document".
- **Unsupplied**: twenty-one directories that no shipped target reads. ADR-0017's prose says twenty because it
  counts 27 sources against 7 targets, and the difference is `zh_CN`, which supplies two targets. The
  contributor queue is measured in directories, because a directory is what a translator maintains;
  `packages/schema/presentation.json#/i18n/unsuppliedBasis` states the arithmetic so the two numbers stop
  contradicting each other.
- **The literal debt**: 307 `| tr}}` uses and 22 hardcoded `{{'…'}}` lines across the templates under
  `omega-web/src`, both recounted on every run. Every label a port touches becomes a msgid in the `.po` tree; a
  `{{'…'}}` without `tr` is invisible to translators, and examples of it are on the theme screen itself
  (`omega-web/src/partials/theme.jade:9`, `omega-web/src/partials/theme.jade:13`,
  `omega-web/src/partials/theme.jade:17`) and on the sync form (`omega-web/src/partials/io.jade:59`).
- **No RPC**: the inherited `tr` filter is a call to the background
  (`omega-web/src/omega/filters.coffee:24`, resolving to
  `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:146`), so every label in the shipped
  popup is one round trip subject to the worker wake-up race §8 exists to survive. A NEXORA surface reads a
  compiled catalog locally and synchronously (ADR-0017 rules 2 and 3). Catalog lookup is never awaited before
  first paint, and the pipeline never forks into a JS i18n library with its own catalog.
- **Placeholders**: the `.po` form is `$1:name$` and the `_locales` form is `$name$`; the rewrite happens on
  `omega-target-chromium-extension/grunt-po2crx.coffee:13` and is emitted on
  `omega-target-chromium-extension/grunt-po2crx.coffee:26`. One placeholder vocabulary on both compile paths,
  so a key that takes an argument in the catalog takes the same named argument in `_locales`. A mismatch is a
  build error, never a runtime fallback.
- **Names are data**: the inherited UI looks up `profile_<name>` and falls back to the raw name
  (`omega-web/src/omega/filters.coffee:25-29`), so a user who names a profile `options_apply` gets a label they
  did not write. NEXORA renders user-chosen names verbatim and translates only its own vocabulary.
- **The support table** is `packages/i18n`'s `localeSupport`, not this file (ADR-0017 rule 4): a completion
  percentage is a build measurement, and a contract that stores one goes stale on the next translation commit.

## 8. The popup

The popup is where every UI constraint in this project collides: it is the surface users touch daily, it is the
one with a documented cold-start defect, and it is the one that must keep working when the Connector is absent.

**Keyboard.** The inherited map is a behaviour, not an implementation detail: a user who switches profiles with
`3` has no migration note that explains a renamed binding
(`packages/schema/presentation.json#/popup/keyboardMap/rule`). The seven named actions — `js-direct`,
`js-system`, `js-external`, `js-addrule`, `js-temprule`, `js-option`, `js-reqinfo` — are each asserted to
exist in `omega-web/src/popup/js/keyboard.js:2-28` today, the nine digit slots are asserted to be the loop
that binds them, and the motions (`up`/`down`, `left`/`right`, `h`/`j`/`k`/`l`) and the help key are carried
from `omega-web/src/popup/js/keyboard.js:2-28` and `omega-web/src/popup/js/keyboard_help.js:1`. The focus model
is a tree walker over `.om-nav` with a tabbable-only filter
(`omega-web/src/popup/js/keyboard.js:34-38`, the list itself in `omega-web/src/popup/index.html:10`), which is
the correct model for a surface whose whole content is a list of one-word targets. A NEXORA popup that renames
or drops a binding is a parity row in §9 with status `pending`.

**Cold start.** `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:50-51` sets
`maxAttempts = 5` and `maxWorkerDownAttempts = 9`; `:63-67` waits `100 * attempt` ms normally and
`2000 * Math.pow(2, attempt - maxAttempts - 1)` once the error is "Receiving end does not exist". Summing that
ladder gives **15500 ms**, and the harness recomputes it from the CoffeeScript constants rather than trusting
the number ("the cold-start ladder is recomputed from the legacy constants, and the budget ceiling is that
sum"). ADR-0016 rule 6 requires the port to be verbatim, including the worker-down branch, and
`docs/audit/12-what-must-not-change.md` §10 lists it as inherited behaviour that must not be "simplified";
simplification here is regression #276 in disguise.

What the ceiling means for a budget is in §9: fast is the *local* render. The ≈15 s ladder is the outer bound
of an RPC, not a target anyone should measure a screen against.

**Two whole-document states.** The legacy popup leaves the document entirely for two conditions —
`location.href = 'grant_permissions.html'` (`omega-web/src/popup/js/loader.js:45`) and the
`proxyNotControllable` branch (`omega-web/src/popup/js/profiles.js:33-35`), each a complete page
(`omega-web/src/popup/grant_permissions.html:1`, `omega-web/src/popup/proxy_not_controllable.html:1`). NEXORA
renders them as §4 states instead of as documents, and the obligation goes the other way round: a permission
problem or an uncontrolled proxy is a fact about the browser, not about the Connector, so it must render when
there is no session at all and must not be phrased as a tunnel failure.

**Geometry.** The Chromium action points at an iframe wrapper (`omega-target-chromium-extension/overlay/popup-iframe.html:18`)
whose script first navigates to the document inside it (`omega-target-chromium-extension/overlay/popup-iframe.js:2`)
and then resizes it (`omega-target-chromium-extension/overlay/popup-iframe.js:7-13`), while the content side
loads the matching resizer (`omega-web/src/popup/index.html:54`) and the background holds the popup URL
(`omega-target-chromium-extension/overlay/x-background.js:26`). Whether the navigate or the iframe wins at
runtime is UNVERIFIED (`docs/audit/08-ui-i18n-and-themes.md`, registry item 1) — which is precisely why
auto-resize is a parity row: losing it is a user-visible regression even when the DOM is identical.

**No side effects on mount.** Opening the popup starts no probe, no refresh, no subscription fetch
(`docs/architecture/22-component-contracts.md` §13 invariant 2, `docs/verification/live-matrix.md` LIVE-17),
and selection is converge-to-desired-state so a double tap is not two switches.

## 9. Parity, cut-over, and the budgets that gate it

ADR-0016 rule 8 says cut-over is defined by the parity matrix, not by a date. A matrix that is prose cannot
gate anything, so the rows are data (`packages/schema/presentation.json#/parity/rows`), each naming a legacy
behaviour, the file that proves it exists, the NEXORA surface responsible for it, the obligation, and a status
out of `pending`, `specified`, `done`.

`packages/schema/presentation.json#/parity/gate`: a surface replaces a legacy behaviour only when every row
naming it is `done`, and the popup flips last. A row cannot be closed by design work alone — `done` means
executed, with the check named in `docs/architecture/39-verification-matrix.md`, and `docs/adr/0020-*` is the
rule that an unexecuted row is never reported as passed. The harness asserts the discipline in this iteration
("no parity row is closed in this iteration"), which is the difference between a plan and a claim.

The eighteen rows cover: the seven router states and the restore-by-`lastUrl` behaviour
(`omega-web/src/omega/app.coffee:48-75`, `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:117-123`),
apply-dirty-discard as one model (`omega-web/src/options.jade:57-67`), the theme picker
(`omega-web/src/omega/controllers/master.coffee:329-335`), the first-run and switch-profile guides
(`omega-web/src/coffee/options_guide.coffee:1`), profile icons and ordering
(`omega-web/src/coffee/omega_decoration.coffee:1`), backup upload and download
(`omega-web/src/omega/directives.coffee:1`), the plain-JS popup with its temp rule and quick switch
(`omega-web/src/popup/js/profiles.js:1`), the keyboard map, the two whole-document states, the auto-resize, the
retry ladder, the switch-profile rule editor (`omega-web/src/omega/controllers/switch_profile.coffee:1`), the
per-scheme server form (`omega-web/src/omega/controllers/fixed_profile.coffee:1`), PAC request headers
(`omega-web/src/omega/controllers/pac_profile.coffee:53-57`), the sync-backend form
(`omega-web/src/omega/controllers/io.coffee:1`), the About page's log and export
(`omega-web/src/omega/app.coffee:90-93`), and the translation surface itself.

Budgets live in this section because they are what the gate is measured against
(`packages/schema/presentation.json#/budgets`), and they are targets, not correctness assertions — which is why
`docs/verification/live-matrix.md` excludes "Popup opens in under N ms" from the LIVE rows.

| Budget | Value | Basis |
|---|---|---|
| `popupLocalPaintMs` | 100 | catalog lookup is local (§7) and the shell snapshot is one read, so first paint needs no round trip |
| `popupOptionsCommandMs` | 2500 | half the read-class deadline `docs/architecture/25-native-messaging-protocol.md` §9 gives a read |
| `popupBundleKiB` | 64 | ADR-0016's consequence that popup startup budgets become reviewable numbers; measured on the built document |
| `coldWorkerCeilingMs` | 15500 | the inherited ladder in §8, which is the number a hung popup was fixed at |

Every budget is `Verified: NO — deferred to Phase 2`: a number with no measurement behind it is a target, and
stating it as data is what makes the missing measurement a list item rather than a rumour.

## 10. What is forbidden

`packages/schema/presentation.json#/forbidden` is the list a reviewer checks a screen against. Fifteen rows,
each with a citation and a reason; the five that come from the brief and the UI ADRs are asserted to be present
by name ("the five prohibitions that come from the brief and the UI ADRs are all in the list"):

- a control enabled while its backend does not exist — constraint 11 of `docs/PROJECT-STATE.md` §0 and
  ADR-0016;
- a promise of an unreleased locale, transport or Core feature in UI copy — ADR-0017 rule 4;
- copying the Happ interface — the brief's UI section, verbatim;
- a translated string reached over background RPC — ADR-0017 rule 3, and the mechanism at
  `omega-web/src/omega/filters.coffee:24`;
- a legacy stylesheet injected into a shadow root, or a NEXORA surface depending on one — ADR-0016 rules 4
  and 5.

The rest are the specific lies a screen can tell: a silently stale "connected" indicator, a badge as the only
evidence of a block or a fail-open, a health or latency figure presented as proof that traffic is proxied, a
probe started by opening the popup, an upstream message / stack / path / URL rendered verbatim as an explanation
(the inherited log does exactly that at `omega-web/src/coffee/log_error.coffee:6`), a secret value in a store, a
URL, a component property, a DOM attribute or a `title`, a second catalog as the place strings are defined, a
remote script or an `eval`, a `dryRun` plan in a past tense, and one screen re-declaring a bound a schema
already owns.

## 11. Who owns each check

| Check | Owner | Mechanism |
|---|---|---|
| surface/command/event consistency | `nexora/extension` | the assertions "no surface names a command the allowlist does not carry" and "every declared event has a surface that renders it" in `packages/schema/scripts/verify.mjs` |
| state anchors resolve, no orphan state | `nexora/extension` | "every state anchor resolves" and "no state is declared but rendered" |
| derived confirmation and dryRun sets | Connector + `nexora/extension` | "the confirmation set is derived", "the dryRun set is derived" |
| placement vocabulary | Extension + Connector | "every error code names a placement the screen implements" |
| token list equals the inherited sheet, catalogue counts | `nexora/extension` | "the inherited variable sheet is the token list", "the measured facts about the catalogue are the numbers on disk" |
| locale counts equal the build task and the `.po` tree | `packages/i18n` | "the shipped locale set is the build task list", "the English source carries the number of msgids" |
| keyboard map and ladder constants | `nexora/extension` | "the keyboard map the contract inherits is in the legacy popup today", "the cold-start ladder is recomputed" |
| parity rows are real and none is closed | `nexora/extension` | "every parity row points at a legacy file that is still in the repository", "no parity row is closed in this iteration" |
| doc 37 names what the data declares | `nexora/extension` | "the user-interface document names every surface, state, document, placement, token and locale the data declares" |
| every budget measured, popup startup | `nexora/extension` | none yet — `Verified: NO`, deferred to Phase 2 with a CI measurement |
| locale completion threshold and promotion | contributors + `packages/i18n` | none yet — `Verified: NO`, ADR-0017 rule 5 |
| no Svelte root inside AngularJS DOM | `nexora/extension` | none yet — `Verified: NO`, a build-time scan of mount points (ADR-0016 verification) |

## 12. Threat, control, check

| What could go wrong | Control | Check |
|---|---|---|
| a screen shows a feature that has no backend | §5's disabled-with-reason rule and the not-shipable list | "every control that will not ship names evidence that resolves and a reason long enough to review" |
| a stale indicator reads as live | `unknown` and `stale` are separate states | "the state vocabulary is exactly the conditions the other contracts hand to a screen" |
| a credential reaches the DOM | §10, ADR-0010's plane split, `secret-entry` writes to `profile.import#/args/properties/credentials` only | "no surface is a screen with no command it may issue" plus `docs/verification/live-matrix.md` LIVE-10 and LIVE-22 |
| a fail-open is invisible | `failOpenInForce` anchored to the policy pointer | "every state names exactly one anchor" |
| a leak is reported as a failure | `server.test`'s `not-proxied` distinct from `fail` | §4's health-is-not-proof paragraph, LIVE-04 |
| an error has nowhere to appear | the placement vocabulary | "no declared placement belongs to zero codes" |
| a theme breaks two of 164 palettes invisibly | the reader trims and refuses a bad sextet | "the measured facts about the catalogue are the numbers on disk" |
| a shell abstraction erodes | two session commands no surface may name | "the commands no surface names are exactly the two the shell owns" |

## 13. Invariants, amendments, and the open items

**Invariants.** (1) A surface never opens a session. (2) A control never exists before its backend does.
(3) A state never claims more than its anchor allows. (4) A screen never reads `-customCss` and never writes a
secret into a store, a URL, a property, an attribute or a title. (5) All copy is a msgid; all colour is a
token. (6) The popup starts nothing on mount. (7) Cut-over is gated by the parity matrix, and the popup flips
last. Each is asserted in `packages/schema/scripts/verify.mjs` except the browser-side halves, which are
`Verified: NO` and named in §11.

**Amendments.** A new surface means a document, a command set and a parity row in the same change; a new state
means an anchor and a `mustNot` sentence; a new locale target means the build task, the support table and
ADR-0017's promotion gate, in that order and never one of the three alone. Reversing the framework is
governed by ADR-0016 rule 7 and must be expressible inside `nexora/extension/src/ui/**` — if it needs a
contract change, the layering is the defect.

**Open items** (`packages/schema/presentation.json#/openItems`, five): the sentences themselves, since every
state in §4 names what it must never claim and the wording is msgids still to be written; whether the 64-inbound
ceiling rises, which decides whether `routing` warns before or after the user asks; whether the secret census
counts are user-visible or diagnostics-only; whether a NEXORA surface ever accepts raw CSS, which needs an ADR
before it needs a control; and the two popup branches' runtime geometry, UNVERIFIED until a browser is loaded.

Trace: ADR-0002, ADR-0007, ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0016, ADR-0017,
ADR-0019, ADR-0020 · Contract: `packages/schema/presentation.json`,
`packages/schema/protocol/command-allowlist.json`, `packages/schema/events.schema.json`,
`packages/schema/protocol/error-taxonomy.json`, `packages/schema/protocol/commands/core.restart.json`,
`packages/schema/protocol/commands/keystore.purge.json`, `packages/schema/protocol/commands/config.apply.json`,
`packages/schema/protocol/commands/profile.remove.json`,
`packages/schema/protocol/commands/profile.validate.json`,
`packages/schema/protocol/commands/subscription.refresh.json`,
`packages/schema/protocol/commands/profile.import.json`,
`packages/schema/protocol/commands/diagnostics.get.json`,
`packages/schema/protocol/commands/connector.health.json`,
`packages/schema/protocol/commands/core.status.json`,
`packages/schema/protocol/commands/server.test.json`,
`packages/schema/failure-policy.schema.json`, `packages/schema/pins.schema.json`,
`packages/schema/protocol/data-ownership.json`, `packages/schema/migrations.json`,
`packages/schema/observability.json` · See also:
`docs/audit/08-ui-i18n-and-themes.md`, `docs/architecture/20-target-repository-layout.md`,
`docs/architecture/21-system-context.md`, `docs/architecture/22-component-contracts.md`,
`docs/architecture/25-native-messaging-protocol.md`, `docs/architecture/26-command-surface.md`,
`docs/architecture/28-routing-model.md`, `docs/architecture/29-subscriptions-and-servers.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/31-security-model.md`,
`docs/architecture/32-secrets-keystore-masking.md`, `docs/architecture/33-binary-integrity.md`,
`docs/architecture/34-data-split.md`, `docs/architecture/35-schema-versioning-migration.md`,
`docs/architecture/36-observability.md`, `docs/architecture/38-compatibility-matrix.md`,
`docs/architecture/39-verification-matrix.md`, `docs/verification/live-matrix.md`.
