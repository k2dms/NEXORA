# ADR-0017: `omega-locales/*.po` is the single translation source, compiled twice -- into `_locales` and into a UI catalog

Status: Accepted
Date: 2026-11-04

## Context

Translations already have a home and a community: `omega-locales/<locale>/LC_MESSAGES/omega-web.po`, 27
directories, one `.po` each, measured by counting the tree. The build compiles them into the browser's
`_locales/<lang>/messages.json` through the grunt `po2crx` task, whose entire input list is
`omega-target-chromium-extension/grunt/po2crx.coffee:4-17`.

That list has seven targets -- `en`, `zh`, `cs`, `fa`, `ru`, `zh_CN`, `zh_TW` -- produced from six `.po`
files (`zh` and `zh_CN` both come from `omega-locales/zh_CN/LC_MESSAGES/omega-web.po`, and `en` from
`en_US`). So the shipped set is 7 while the source set is 27. The gap is real and it is not uniform:
measured empty-`msgstr` counts per file put `zh_TW` and `zh_CN` at ~27 from ~350 entries, `ja`, `cs`, `de`,
`fr`, `es_AR`, `pt_BR`, `ru`, `fa` under ~95, and `he_IL`, `pl`, `si`, `lzh`, `it`, `sl`, `sk`, `nl`, `zh_Hant`
between ~255 and ~350. Some of the 20 unsupplied locales are nearly finished; some are placeholders.

`manifest.default_locale` is `en` (`omega-target-chromium-extension/overlay/manifest.json:14`), so the
browser can always resolve `_locales/en`. Nothing resolves the other 20, and nothing says so to the user.

The legacy UI does not read `chrome.i18n` directly. Its `tr` filter is
`omega-web/src/omega/filters.coffee:24`, which calls `omegaTarget.getMessage`, and that method is the
background's `chrome.i18n.getMessage` bound and re-exported over RPC at
`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:146`. Every translated label in the
popup is therefore a message to a service worker that may be asleep -- the exact condition the backoff at
`omega-target-chromium-extension/src/coffee/omega_target_web.coffee:41-78` exists to survive. A second UI
(ADR-0016) that inherited this pattern would translate its whole shell by RPC round trips.

## Decision

1. `omega-locales/*.po` is the **only** source of UI strings. Neither UI may introduce a second catalog as
   the place strings are *defined*: adding a `msgId` means adding it to the `.po` files, and the English
   source of truth stays `omega-locales/en_US/LC_MESSAGES/omega-web.po`.
2. The source MUST be compiled twice, by two mechanical targets, from the same `.po` input:
   - `_locales/<lang>/messages.json` through the existing `po2crx` task
     (`omega-target-chromium-extension/grunt/po2crx.coffee:4-17`) -- unchanged, because the browser reads
     only that directory, and manifest strings resolve there;
   - a typed catalog in `packages/i18n` consumed by the Svelte islands (ADR-0016), generated at build time
     from the same `.po` files, never hand-maintained, and carrying the same msgids so a key is never
     renamed to satisfy a bundler.
3. The new UI MUST NOT fetch a translation through background RPC. Catalog lookup is local and synchronous;
   `chrome.i18n` remains the path only for manifest-provided strings.
4. The 27-versus-7 gap MUST NOT be left silent. For Phase 1 it is **explicitly scoped**: the supported set
   is exactly the seven targets named in `po2crx.coffee`, and `packages/i18n` MUST publish a
   `localeSupport` table naming all 27 sources with their measured completion, so "unsupported" is a stated
   fact rather than an absent file. Closing the gap is a Phase-2+ contributor track, not a code task, and
   MUST NOT be promised in UI copy before it happens (constraint 11 of the brief).
5. Promotion of a locale from "source present" to "shipped" is gated and MUST require all three: measured
   completion above a stated threshold (empty `msgstr` excluding the header, plus no `#, fuzzy` on shipped
   entries), a new `po2crx` target entry, and its `localeSupport` row flipped. It MUST NOT be done by
   editing only one of the two compile paths.
6. Deleting `.po` files to make the numbers honest is MUST NOT be used. The 27 files are contributor work;
   the correct expression of "we do not ship this" is the support table.
7. Plural and placeholder handling stays within what `.po`/`po2crx` express. If the new UI needs richer
   formatting than the pipeline can carry, that is a new ADR superseding this one -- not a silent fork into
   a JS i18n library.

## Consequences

- One string pipeline serves the legacy UI, the new UI and (through the same catalog) Connector-facing
  messages that need no browser at all.
- The new UI's shell renders before the service worker answers anything, which removes an entire class of
  cold-start flicker that the RPC-filter design guarantees today.
- A third build step exists (`.po` to catalog), so `packages/i18n` becomes a workspace root of its own
  (ADR-0018) and its output is a generated artifact: generated files MUST NOT be hand-edited, and MUST be
  regenerable in CI.
- The support table becomes a reviewable document: an incomplete locale is visible as a percentage, which
  turns "restore 20 locales" from folklore into a queue with measured remaining work.
- Locale codes need care at the boundary: `en_US`, `zh_CN`, `zh_TW`, `zh_Hant`, `he_IL`, `nb_NO` do not all
  map 1:1 onto browser `lang` directory names, so the mapping table lives in `packages/i18n` and is the
  single place a mismatch can be fixed.
- Nothing in this ADR changes what today's users see: the shipped seven are untouched and the legacy
  `tr` filter keeps working.

## Alternatives considered

- **Adopt a JS i18n library with its own JSON/TS catalogs.** Rejected: it forks the source of truth, orphans
  20 partially translated communities, and still cannot replace `_locales` for manifest strings -- so the
  repo ends up with two sources and one of them wins only by accident.
- **Have the new UI call `chrome.i18n.getMessage` (or the background `tr` filter) directly.** Rejected:
  it re-introduces the RPC-per-label dependency at `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:146`
  inside the surface whose documented defect is worker wake-up latency, and it would leave Connector-side
  strings with no translation path at all.
- **Migrate to Fluent or ICU MessageFormat now.** Rejected for this phase: a translation-toolchain migration
  across 27 files with that completion spread destroys more translator work than it enables. Revisit with
  an ADR once the parity cut-over is settled.
- **Ship all 27 directories and let quality fall out.** Rejected: `he_IL`, `pl`, `si`, `lzh` would present a
  mostly-untranslated UI as if it were localised, which is worse for the user and for bug triage than an
  honest `en` fallback.
- **Drop the 20 unsupplied `.po` files.** Rejected on rule 6: it deletes contributor work to fix a
  documentation problem.

## Evidence

- VERIFIED -- `omega-target-chromium-extension/grunt/po2crx.coffee:4-17`: seven output `_locales` targets
  compiled from six `.po` inputs; that file is the complete shipped list.
- VERIFIED -- 27 locale directories under `omega-locales`, one `LC_MESSAGES/omega-web.po` each;
  `omega-locales/en_US/LC_MESSAGES/omega-web.po:3` carries the upstream `Project-Id-Version` header, i.e.
  the pipeline predates this fork.
- VERIFIED -- completion spread measured per file (`msgid` vs empty `msgstr` count): `en_US` 382/68,
  `zh_CN` 361/29, `ru` 382/90, `de` 351/82, `he_IL` 351/351, `pl` 351/344, `si` 351/349.
- VERIFIED -- `omega-web/src/omega/filters.coffee:24` (`tr` filter delegates to `omegaTarget.getMessage`)
  and `omega-target-chromium-extension/src/coffee/omega_target_web.coffee:146` (that method is the
  background-bound `chrome.i18n.getMessage`).
- VERIFIED -- `omega-target-chromium-extension/overlay/manifest.json:14`: `default_locale` is `en`.
- INFERRED -- that `zh` and `zh_CN` are byte-identical outputs of the same `.po`: read from the task config;
  not compared after a build in this iteration.
- UNVERIFIED -- how many of the empty `msgstr` entries are intentionally omitted vs unwritten, and whether
  any `.po` still carries stale `fuzzy` flags. Needs a po lint pass in Phase 2.

## Verification

- Existing check: `node tools/check-doc-evidence.mjs` resolves every citation above, including the `po2crx`
  list that defines the shipped set.
- Verified: NO -- deferred to Phase 2: the `.po`-to-catalog compiler, `localeSupport` table generation and
  its completion threshold, and "no translated string in the new UI reaches the background over RPC". The
  intended check is a CI job that regenerates the catalog and diffs it against the committed output, plus a
  lint that fails a shipped locale below threshold.
