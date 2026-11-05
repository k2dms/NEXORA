# ADR-0016: The new UI is Svelte 5 islands with `customElement: true`, built by Vite and TypeScript, without SvelteKit

Status: Accepted
Date: 2026-11-04

## Context

NEXORA ships a new UI on top of a live AngularJS UI that must keep working through the whole transition
(ADR-0002). So the framework question is not "what is nicest to write" but "what can share a browser
extension with Bootstrap 3, a jade/LESS pipeline and Bower components without either side breaking".

Three properties of the inherited code drive the choice.

**Cold start is this repo's documented defect.** The MV3 service worker may be asleep when the popup opens,
so the shipped UI client retries RPC with a hand-tuned backoff --
`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78`: `maxAttempts = 5` and
`maxWorkerDownAttempts = 9` at `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:50-51`,
with `100 * attempt` ms normally and `2000 * 2^(attempt - 6)` ms once the error is "Receiving end does not
exist" (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:63-67`). Startup cost is not a
micro-optimisation here, it is the bug class.

**Coexistence is a CSS problem before it is a JS problem.** A user theme is three stylesheets concatenated
into the `chrome.storage` key `-customCss`
(`omega-web/src/omega/controllers/master.coffee:359-360`), fetched from
`lib/themes/variable.css`, the selected `lib/themes/<key>.css` and `lib/themes/base.css`; there are 164
base16 theme entries in `omega-web/lib/themes/themes.json` plus arbitrary user CSS on the options page.
Bootstrap 3 styles bare element selectors, and the popup applies the same `customCss`
(`omega-web/src/coffee/popup.coffee:89-90`). Any new UI injected into that document inherits all of it.
Encapsulation has to be structural, not a naming convention.

**The legacy page boots through a serial script loader.** `omega-web/src/coffee/options.coffee:2-10` chains
`$script` calls for angular-loader, jQuery, `omega_pac.min.js`, FileSaver, spin/ladda. Anything mounted
inside that DOM competes with jQuery for the document.

## Decision

1. The new UI MUST use **Svelte 5 + Vite + TypeScript**, in an islands architecture: each NEXORA surface is
   a component compiled to a custom element with `customElement: true`, registered under `nexora-*` tag
   names, and mounted only into explicit mount points the legacy page owns.
2. **SvelteKit MUST NOT be used.** There is no server, no URL routing surface and no SSR target in an MV3
   package; its router and runtime would be dead weight against a manifest-declared page list.
3. TypeScript MUST be `strict`, and UI code MUST NOT redefine contract types: entity and command types are
   derived from `packages/schema` (the one permitted exception to "nothing under `nexora/**` may import
   from `omega-*`", which is `docs/architecture/20-target-repository-layout.md`).
4. A Svelte root MUST NOT be mounted inside AngularJS-managed DOM. Legacy and new surfaces share *state*
   (via `packages/state`, not yet written) and *documents* (an explicit mount point, or a separate document
   as the popup already does at `omega-target-chromium-extension/overlay/popup-iframe.html:18`), and MUST
   NOT share a component tree or a reactive runtime.
5. Shadow DOM from the custom elements is the encapsulation mechanism. The UI MUST NOT inject legacy
   stylesheets into a shadow root, and MUST NOT depend on legacy CSS reaching into one. Theme support for
   NEXORA surfaces is therefore an explicit token pipeline, specified in
   `docs/architecture/37-ui-architecture.md` §6, not an accident of inheritance.
6. The UI's background-RPC client MUST port the cold-start backoff at
   `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78` **verbatim**, including the
   "Receiving end does not exist" branch. It MUST NOT be "simplified" to a fixed retry count; that is
   regression #276 in disguise.
7. **Reversal clause.** The framework sits strictly above `packages/schema`, `packages/state` and
   `packages/ui`. Switching UI framework MUST be expressible as a change confined to
   `nexora/extension/src/ui/**`, plus the Vite config that builds it. If a framework switch would require
   touching a contract, a command, or the state layer, that is a defect in this layering and MUST be fixed
   as such rather than by widening the switch. A framework change itself still requires a superseding ADR
   (ADR-0001).
8. Cut-over is defined by the parity matrix, not by a date, and the popup flips last (ADR-0017 and
   `docs/architecture/37-ui-architecture.md` §9). A NEXORA section MUST NOT be added to the legacy UI as a
   placeholder button -- constraint 11 of the brief.

## Consequences

- Two toolchains in one repository: grunt for `omega-*`, Vite for `nexora/extension`. They are separated by
  workspace-root boundaries (ADR-0018) so that nothing can accidentally drive the legacy graph from the new
  one, and the legacy chain keeps its own `omega-build` hub.
- i18n now has two consumers and therefore one source compiled twice (ADR-0017).
- The islands model makes per-section rollout possible: a NEXORA surface appears when it is finished, and
  the legacy section stays visible until then. That is what makes the parity matrix enforceable.
- `customElement: true` constrains authoring: props cross a DOM boundary as attributes/properties, so
  component APIs at the boundary MUST stay JSON-serialisable -- the same shapes `packages/schema` already
  defines.
- Vite dev-server/HMR is a development affordance only; the MV3 package MUST contain built static assets and
  MUST NOT contain remote code (CSP).
- Bundle-size and startup budgets become reviewable numbers for the popup, since the popup is where the
  documented cold-start pain lives.

## Alternatives considered

- **React 18/19 + Vite + TypeScript** -- the considered alternative, and a good one. Not chosen, on the
  three Context properties: (a) it adds `react-dom` plus its scheduler to a surface whose shipped defect is
  cold-start latency (`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78`); (b) it
  has no built-in style encapsulation, so against Bootstrap 3 + 164 theme sheets + arbitrary `-customCss` it
  needs a hand-rolled shadow wrapper or a CSS-in-JS runtime -- extra bytes at the exact place bytes are the
  problem, and a runtime that fights the legacy document; (c) islands in React mean one `createRoot` per
  island plus effect-driven lifecycle plumbing, where Svelte's `customElement: true` yields the same shape
  with less ceremony. It is *not* forbidden: rule 7 exists precisely so this can be reversed cheaply.
- **Stay on AngularJS and extend it** -- rejected: no reactivity model, no type system, and its
  jade/LESS/Bower pipeline is exactly what ADR-0002 refuses to mass-refactor. Extending it means deepening
  the dependency rather than isolating it.
- **Svelte + SvelteKit** -- rejected: no server to run, no URL space to route, and an adapter story that
  does not fit `chrome-extension://` pages.
- **Lit, or plain TypeScript DOM** -- closest runner-up on the encapsulation axis (native custom elements),
  rejected mainly on authoring ergonomics: Svelte's runes give the reactive state model `packages/state`
  needs without a class-per-component structure. Kept as the fallback if rule 7 is exercised.

## Evidence

- VERIFIED -- `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78`: the retry
  constants at `:50-51` and the two-branch backoff at `:63-67`, with the comment naming the worker-wake
  race as the reason.
- VERIFIED -- `omega-web/src/omega/controllers/master.coffee:359-360`: `customCss` is the concatenation of
  variable, theme and base sheets, written back to `-customCss`.
- VERIFIED -- `omega-web/src/coffee/options.coffee:2-10`: the serial `$script` boot chain, including
  `omega-target-chromium-extension/overlay/popup-iframe.html:18` for the popup's iframe-based isolation.
- VERIFIED -- 164 theme entries in `omega-web/lib/themes/themes.json` (counted on disk, tracked in git).
  Note: `docs/PROJECT-STATE.md` records "166 base16 themes"; the measured value is 164.
- INFERRED -- React bundle/encapsulation objections: standard framework properties, applied to measured
  conditions in this repo; no React build was produced or timed.
- UNVERIFIED -- Svelte 5 custom-element CSS scoping behaviour inside an MV3 options page, and popup
  cold-start numbers with either framework. Both need the extension loaded in a browser.

## Verification

- Existing check: `npm --prefix packages/schema run verify` keeps the contract layer UI-independent --
  schemas and command documents reference no UI type, and the description-coverage rule there rejects an
  undocumented contract field, which is what makes rule 3 (derive types, do not redefine them) checkable.
- Verified: NO -- deferred to Phase 2: the parity matrix gating cut-over; a popup startup budget assertion;
  the verbatim-backoff port of rule 6; and "no Svelte root inside AngularJS DOM", for which the intended
  check is a build-time scan of `nexora/extension/src/ui/**` mount points.
