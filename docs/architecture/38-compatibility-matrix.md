# 38 — The compatibility matrix: what an existing ZeroOmega user keeps, what they lose, and what nobody may claim until it is measured

> Purpose: nine documents sent promises here that they could not make themselves.
> `docs/architecture/21-system-context.md` describes the browser as a platform and points at "the floor in
> `docs/architecture/38-compatibility-matrix.md`" for the number; `docs/architecture/22-component-contracts.md`
> makes a registry check and a no-Connector popup behaviour its two `Verified: NO` items and names this file as
> their owner; `docs/architecture/23-unified-server-model.md` says a Trojan subscription is lossy *by
> construction* and that the matrix must "say so out loud"; `docs/architecture/24-options-envelope-compat.md`
> leaves the `password`-in-sync strip as an open decision owned here; `docs/architecture/28-routing-model.md`
> hands over the question of what a user loses when a condition cannot compile; `docs/architecture/31-security-model.md`
> refuses to state any browser or OS support claim and sends them here; `docs/architecture/34-data-split.md`
> owes the `chrome.storage.session` floor and an envelope-size measurement to this file before its own quota
> rule can be checked; `docs/architecture/20-target-repository-layout.md` requires a manifest change to name the
> parity rows it closes; and `docs/verification/live-matrix.md` excludes the Firefox parity half of LIVE-15 to
> here. Constraint 13 of the brief is the promise behind all of it: **ZeroOmega profiles survive**. A promise of
> that size is either a list with a verdict per row or it is marketing.

Status: Accepted. Normative data: `packages/schema/compatibility.json`,
`packages/schema/compatibility.json#/browserFloors`, `packages/schema/compatibility.json#/manifestDeltas`,
`packages/schema/compatibility.json#/apiSurface/absent`, `packages/schema/compatibility.json#/profileTypes`,
`packages/schema/compatibility.json#/optionKeys/scalarKeys`, `packages/schema/compatibility.json#/backups`,
`packages/schema/compatibility.json#/syncBackends`,
`packages/schema/compatibility.json#/verdicts/values`,
`packages/schema/compatibility.json#/matrix`.
Evidence base: `omega-target-chromium-extension/overlay/manifest.json`,
`omega-target-chromium-extension/overlay/manifest-firefox.json`, `omega-pac/src/profiles.coffee`,
`omega-pac/src/conditions.coffee`, `omega-pac/src/rule_list.coffee`, `omega-target/src/options.coffee`,
`omega-target/src/default_options.coffee`, `omega-target/src/options_sync.coffee`,
`omega-target-chromium-extension/src/module/`, `omega-web/src/omega/controllers/io.coffee`,
`docs/adr/0002-keep-omega-directories-additive-layout.md`, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0015,
`docs/audit/12-what-must-not-change.md`, `docs/verification/live-matrix.md`, `docs/PROJECT-STATE.md` §0.

---

## 1. What this document owns, and what it refuses

Owns (`packages/schema/compatibility.json#/scope/owns`): the browser and OS floor; the permission delta between
what the inherited manifests ask and what NEXORA asks; the legacy data shapes that must survive cut-over —
profile types, the `+` key, the `-` option keys, the `.bak` file and the two sync backends; the four verdicts a
row may carry; and the rows themselves.

Refuses, in this order:

- **The wire negotiation.** `packages/schema/protocol/compatibility-rules.json` owns protocol versions and
  capability names. Its filename collides with this document's subject and its content does not: that file says
  a Connector and an extension may speak at all, this one says what a *user* experiences when either side is
  older than the other. Neither reads the other's list.
- **The condition vocabulary.** `packages/schema/routing-policy.schema.json#/$defs/condition` lists the twelve
  `conditionType` names found in `omega-pac/src/conditions.coffee:227`, and
  `docs/architecture/28-routing-model.md` decides which of them run in the browser plane and which reach Core.
  A row here says what the user keeps either way.
- **The feed formats.** `docs/architecture/29-subscriptions-and-servers.md` §5 owns the loss tables. This
  document borrows its verdict rather than restating it as a second list.
- **The keystore per operating system.** ADR-0011 binds Windows; a row here says only which OS is in scope (§3).
- **Whether a check ran.** `docs/verification/live-matrix.md` and `docs/architecture/39-verification-matrix.md`
  own execution. The `owed` column below never claims a measurement happened; it names who owes it.

One refusal deserves its own sentence, because it is the one a reviewer will be tempted to break: **this file
does not state a browser version it cannot evidence.** The inherited manifests state one, and they are wrong in
a way worth reading (§3).

## 2. The questions that were deferred here

| Cited by | What it asked for | Where it lands |
|---|---|---|
| `docs/architecture/21-system-context.md` §3 | the Chrome/Edge floor | §3 |
| `docs/architecture/22-component-contracts.md` §6 | the dangling-`serverId` deferral | §10, COMP-16 |
| `docs/architecture/22-component-contracts.md` §13 | "works when the Connector is absent" | §10, COMP-15 |
| `docs/architecture/23-unified-server-model.md` §7 | Trojan/WireGuard are lossy; say it out loud | §10, COMP-08 |
| `docs/architecture/24-options-envelope-compat.md` §4 | the `password` strip decision | §10, COMP-18 |
| `docs/architecture/28-routing-model.md` §9 | what a user loses when a condition cannot compile | §10, COMP-05 |
| `docs/architecture/31-security-model.md` §1 | every browser and OS support claim | §3, §4 |
| `docs/architecture/31-security-model.md` §13 | when `webRequest*` becomes an optional grant | §10, COMP-07 |
| `docs/architecture/34-data-split.md` §1 | the legacy `+`-key behaviour that must not change | §5 |
| `docs/architecture/34-data-split.md` §8 | the `chrome.storage.session` floor | §3, COMP-10 |
| `docs/architecture/34-data-split.md` §9 and §13 | envelope sizes without `unlimitedStorage` | §10, COMP-14 |
| `docs/architecture/20-target-repository-layout.md` §6 | the parity rows a manifest edit must close | §4, COMP-20 |
| ADR-0013 consequences | a registry change is a compatibility event | §5 |
| `docs/verification/live-matrix.md` LIVE-15 | the Firefox half of parity | §10, COMP-11 |

Fourteen deferrals, and the shape of them is consistent: each document knew the *kind* of answer it needed and
not the *number*, because the number lives in the inherited tree or in a browser. So this document's first duty
is to go and read the tree, which is what §3–§8 do, and its second is to say plainly which answers the tree
cannot give (§10).

## 3. The floor is a promise, and the inherited one is already broken

Both manifests say `minimum_chrome_version: "88"` — `omega-target-chromium-extension/overlay/manifest.json:29`
and, absurdly, `omega-target-chromium-extension/overlay/manifest-firefox.json:29`, a key no Gecko reads. The
Gecko floor is written once, at `omega-target-chromium-extension/overlay/manifest-firefox.json:52`, as
`strict_min_version: "112.0"`.

Against those two numbers stands this: the background calls `chrome.storage.session` at
`omega-target-chromium-extension/src/module/options.coffee:106` and writes it at
`omega-target-chromium-extension/src/module/options.coffee:140`, with no guard — and `chrome.storage.session` is
a newer API than 88. The same tree knows how to guard an optional API:
`omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:16` checks
`if not chrome.webRequest.onAuthRequired` and degrades with a log line, and
`omega-target-chromium-extension/src/module/storage.coffee:39` probes `browser?.storage?[@areaName]` before
touching an area. So the omission is not ignorance of the pattern; it is one API that arrived after the floor
was written and was never reconciled with it.

What NEXORA therefore promises is not a number but a rule
(`packages/schema/compatibility.json#/browserFloors/rule`): **the shipped floor equals the highest API floor among
the calls the extension actually makes.** The rule cannot be closed today, and this document does not pretend
otherwise — the real availability floor of `browser.storage.session` per engine is a platform fact, and
`docs/architecture/34-data-split.md` §8 makes the same concession about the `chrome.storage.local` quota ceiling:
a platform fact is not a repository fact. That is COMP-10, verdict `deferred-needs-measurement`, owed by LIVE-15
and its Chromium counterpart. Until that row runs, no NEXORA document — including this one — may print a browser
version as settled, and the sentence in `docs/architecture/21-system-context.md` that points here must keep
pointing here rather than at a number.

Two corollaries follow, and both are cheap to violate:

1. **A floor is per engine.** The two manifests differ in fourteen keys and agree in the one that matters least.
   A single "Chrome 88 / Firefox 112 and up" sentence in a store listing would be a guess dressed as a fact.
2. **The OS floor is narrower than the browser floor.** The Connector runs where a keystore and a pinned binary
   exist. Today that is Windows: ADR-0011's rule 1 names a DPAPI-protected store, and
   `docs/architecture/33-binary-integrity.md` §17 lists Linux and macOS provenance as owed by their own ADRs.
   COMP-12 carries that as `refused-with-reason`, not as silence.

## 4. The permission surface, and the API list that licenses a removal

The inherited permission sets are eight items each, and they differ by exactly one (`packages/schema/compatibility.json#/manifestDeltas/permissions`):

| | Chromium (`omega-target-chromium-extension/overlay/manifest.json:39`) | Firefox (`omega-target-chromium-extension/overlay/manifest-firefox.json:38`) |
|---|---|---|
| shared | `proxy`, `tabs`, `alarms`, `storage`, `unlimitedStorage`, `webRequest`, `contextMenus` | same seven, in the same relative order |
| one-sided | `webRequestAuthProvider` (`omega-target-chromium-extension/overlay/manifest.json:46`) | `webRequestBlocking` (`omega-target-chromium-extension/overlay/manifest-firefox.json:45`) |

Both ask `host_permissions: ["<all_urls>"]` (`omega-target-chromium-extension/overlay/manifest.json:49`,
`omega-target-chromium-extension/overlay/manifest-firefox.json:48`), and
neither asks `nativeMessaging` — which is not an opinion about NEXORA, it is what the shipped files say, and it is
the measurement that makes ADR-0004's door a *change* to the permission list rather than a line in a document
(`packages/schema/compatibility.json#/manifestDeltas/nexoraAdds`).

The two one-sided permissions gate the same behaviour: proxy HTTP authentication for legacy `FixedProfile`
credentials, registered at `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:19` with
`['blocking']` at `:22`, gated on `details.isProxy` at `:64`. That is COMP-06 and COMP-07 together: the behaviour
survives, the permission stays, and the question of when it becomes an *optional* grant is answered by a cohort
count this repository cannot produce.

What makes a removal defensible is the absence list. `packages/schema/compatibility.json#/apiSurface/absent`
names eighteen APIs — `chrome.notifications`, `chrome.management`, `chrome.idle`, `chrome.bookmarks`,
`chrome.history`, `chrome.downloads`, `chrome.system`, `chrome.storage.managed`, `chrome.browsingData`,
`chrome.scripting`, `chrome.sidePanel`, `chrome.commands`, `chrome.devtools`, `chrome.cookies`,
`chrome.debugger`, `chrome.tts`, `chrome.offscreen`, `chrome.identity` — and every run re-greps each of them over
the five inherited source directories
(`packages/schema/compatibility.json#/apiSurface/corpus`). An API the code never calls is a permission nobody may
ask for and a support sentence nobody may write. `cookies` is the one worth naming: it is the intuitive answer to
"how does a proxy extension know about authentication", and the legacy tree answers it through `onAuthRequired`
instead, which is why `webRequestAuthProvider` exists at all.

The same list contains what the proxy API is actually asked to do: the strings `direct`, `pac_script` and
`fixed_servers` (`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:19`, `:21`, `:45`)
plus `clear()` for a system profile (`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:14`). No
`mode: "system"` and no `advance-mode` is ever written, so a document claiming support for them would be
describing code that does not exist.

`unlimitedStorage` stays granted (COMP-14) because `docs/architecture/34-data-split.md` §9 sets three conditions
for removing it and the first is a measurement of real envelope sizes that no file here contains.

## 5. The shape of the user's data, part one: profiles

There is one registry, at `omega-pac/src/profiles.coffee:253`, and it has ten names: six real types —
`SystemProfile` (`:256`), `DirectProfile` (`:259`), `FixedProfile` (`:263`), `PacProfile` (`:350`),
`SwitchProfile` (`:394`), `RuleListProfile` (`:446`) — and four aliases resolved by a while-loop at
`omega-pac/src/profiles.coffee:247`: `AutoDetectProfile` → `PacProfile` (`:393`), `VirtualProfile` →
`SwitchProfile` (`:445`), `SwitchyRuleListProfile` → `RuleListProfile` (`:520`) and `AutoProxyRuleListProfile` →
`RuleListProfile` (`:521`). An unknown name throws at `:250`.

The aliases are the part a NEXORA reader gets wrong. They exist to read data written by SwitchyOmega-era tools,
which means they appear in exactly the backups whose survival constraint 13 promises (ADR-0015 continues the
version line; ADR-0013 makes a registry change a compatibility event). A reader that resolves only the six
canonical types does not fail loudly — it fails by showing fewer profiles than the user has. COMP-03 therefore
promises all ten, and its `owed` is LIVE-14 run once per name, not once per file.

Two more facts about this shape are load-bearing:

- **The key is `'+' + name`.** Built at `omega-pac/src/profiles.coffee:96`, iterated by character code at
  `omega-pac/src/profiles.coffee:113`, and dispatched the same way in the storage watcher at
  `omega-target/src/options.coffee:92`. `docs/architecture/34-data-split.md` §1 cites this document for exactly
  that behaviour. A NEXORA entity that needed a second namespace would be invisible to `each` — which is why the
  NEXORA namespace is a `-` key under ADR-0012 and not a new prefix.
- **`+direct` and `+system` are built-ins, not stored profiles** (`omega-pac/src/profiles.coffee:34`). They come
  out of `builtinProfiles` (`:35`, `:40`) on every read, so a store that lost them is not a store that lost two
  user objects; and a NEXORA import that *wrote* them would create a shadow that `byName` prefers
  (`omega-pac/src/profiles.coffee:101`).

`RuleListProfile` parses two source formats, AutoProxy (`omega-pac/src/rule_list.coffee:7`) and Switchy
(`:54`), and both must keep parsing, because a feed and a hand-written rule list are the same field to the
storage layer.

## 6. The shape of the user's data, part two: eleven scalars and one version stamp

`omega-target/src/default_options.coffee:2` writes `schemaVersion: 2`, and lines 3 through 13 write the eleven
`-`-prefixed options: `-enableQuickSwitch`, `-refreshOnProfileChange`, `-startupProfileName`,
`-quickSwitchProfiles`, `-revertProxyChanges`, `-confirmDeletion`, `-showInspectMenu`, `-addConditionsToBottom`,
`-showResultProfileOnActionBadgeText`, `-showExternalProfile`, `-downloadInterval`. Every one is a promise,
because every one is a key a user's existing store already holds, and `-downloadInterval` and
`-revertProxyChanges` change network behaviour when defaulted wrongly.

`packages/schema/compatibility.json#/optionKeys/namespaces` names the four prefixes in use: `+` for profiles,
`-` for options, `omega.local.` for state (`omega-target-chromium-extension/src/coffee/background.coffee:234`)
and **no bare key for NEXORA**, because the dispatch is a first-character test — `omega-target/src/options.coffee:90`
special-cases `-customCss` and `:464` special-cases `-builtinProfiles`. ADR-0012's choice of a dash-prefixed
namespace is the reason this row can be a promise instead of a hazard.

The version space is two numbers wide: `omega-target/src/options.coffee:330` handles version 1 by rewriting
`+auto_detect`, `:344` assigns 2, and `:349`/`:353` accept 2 and reject everything else. That ledger is
`packages/schema/migrations.json`'s subject, not this one; what belongs here is COMP-02 — the value 2 stays the
value the legacy engine writes — because ADR-0002 forbids touching the engine and ADR-0014 forbids letting a
NEXORA migration bump it.

## 7. Backups, and the two destructive paths NEXORA does not inherit

A `.bak` is the whole options map, stringified: `omega-web/src/omega/controllers/io.coffee:69` builds it from
`$rootScope.options` and `:72` names it `ZeroOmegaOptions-<ISO>.bak`. There is no envelope, no per-item `__type`,
no digest. `omega-target/src/options.coffee:360` parses it, `:362` treats a body not starting with `{` as base64
and decodes at `:365`, and both spellings converge on the same `Invalid options!` at `:370`. COMP-01 promises
both spellings survive; LIVE-14 is the run that proves it.

Two legacy paths are worth naming here rather than in a security document, because they are compatibility
traps, not just defects:

- `omega-web/src/omega/controllers/io.coffee:107` fetches an arbitrary URL and `:114` resets the entire store
  with the response. NEXORA does not inherit that path: import is validated before apply with a pre-migration
  snapshot first (ADR-0014). Recorded as `packages/schema/compatibility.json#/backups/onlineRestoreRule`.
- `omega-target-chromium-extension/src/coffee/background.coffee:42` compares versions on an `update` install
  event and, for anything below 3.3.0, clears `chrome.storage.sync` (`:44`), `chrome.storage.local` (`:45`) and
  IndexedDB (`:46`). This is COMP-19, and its verdict is `refused-with-reason`: an upgrade path that deletes is
  not a migration and constraint 12 forbids it. The reason it belongs in a compatibility document is that this is
  how the *previous* project answered "what happens to incompatible old data", and the strongest argument against
  copying it is that it is still in the tree, still reachable, and still the shape a hurried NEXORA cut-over
  commit would take.

## 8. Sync backends: two, not five

`omega-target-chromium-extension/src/module/sync/sync_impl.coffee:1` requires exactly two implementations, and
`loadSyncImpl` chooses between them on the single string `webdav`. So the list of remote backends is **Gist**
(`omega-target-chromium-extension/src/module/sync/sync_impl_gist.coffee:6`, host `https://api.github.com` at `:10`, filename assembled at `:4` from the letters
of `ZeroOmega` at `:3` plus `.json`, bearer token sent at `:25`) and **WebDAV** (`omega-target-chromium-extension/src/module/sync/sync_impl_webdav.coffee:7`, base path
`uri + '/zeroomega/'` at `:17`, commit marker `zeroomega-commit.txt` at `:3`, file prefix `zeroomega-` at `:4`).
Dropbox, OneDrive, box.com and Google Drive are absent — all four re-grepped over the corpus to zero hits
(`packages/schema/compatibility.json#/syncBackendsAbsent`) because a documentation page listing them would otherwise never be contradicted.

Built-in browser sync is a third thing and is not a backend: the data calls it `chrome-storage-sync`, it is
`chrome.storage.sync` wrapped at
`omega-target-chromium-extension/src/coffee/background.coffee:237`, with its configuration held under
`zeroOmegaSync` (`omega-target/src/options_sync.coffee:10`) — and that configuration blob contains the Gist
token, which is the substance of COMP-18 and of `docs/verification/live-matrix.md` LIVE-10.
One near-miss is recorded so it is not repeated: `FileSaver` is present in the tree (`omega-web/src/coffee/options.coffee:6`)
and is a save-to-disk library, not a sync backend, so it is not in the absent list — that list admits only names with zero hits.

## 9. Four verdicts, and why the vocabulary is closed

`packages/schema/compatibility.json#/verdicts/values` admits exactly:

| Verdict | What it commits NEXORA to | What it forbids |
|---|---|---|
| `survives-unchanged` | the behaviour is inherited as it is, the cited line still says what the row claims, and nothing is outstanding | writing the row without a run that closes it |
| `imports-with-report` | the data arrives and whatever cannot be expressed is named at import time — the shape ADR-0013 requires for conditions and doc 29 §5 for feeds | dropping a field quietly |
| `refused-with-reason` | NEXORA will not do it, a named UI state says so, and the refusal is a consequence | a refusal with nowhere to appear (`docs/architecture/37-ui-architecture.md` §4) |
| `deferred-needs-measurement` | the number is missing and not in this repository; the row names who measures it | shipping a control that depends on the answer (constraint 11) |

Four, because each has a different obligation attached. Two would be too few: "supported" and "unsupported" is
the vocabulary that gets products into trouble, because it makes a promise awaiting a measurement look like a
promise kept. `packages/schema/compatibility.json#/verdicts/rule` closes the set and the run asserts that every
row carries exactly one member of it.

## 10. The matrix

Twenty rows, one promise each (`packages/schema/compatibility.json#/matrix`). Every `evidence` citation is
re-checked on every run — a `path:line` into the inherited tree must exist and be non-blank, a `#/pointer` must
resolve, and a `§n` must be a real section heading. Every `LIVE-nn` named must exist in
`docs/verification/live-matrix.md`. A row whose evidence moves is a broken promise found here rather than in a
browser.

| ID | Subject | Verdict | Owed by |
|---|---|---|---|
| COMP-01 | a v3.5.2 backup re-opens unchanged | imports-with-report | LIVE-14, both JSON and base64 |
| COMP-02 | `schemaVersion: 2` stays the legacy value | survives-unchanged | nothing — asserted every run |
| COMP-03 | all six types and four aliases load | survives-unchanged | LIVE-14, once per name |
| COMP-04 | `+` remains the only profile namespace | survives-unchanged | nothing — asserted every run |
| COMP-05 | user switch rules survive as browser-plane rules | imports-with-report | a compile test per condition type |
| COMP-06 | legacy `FixedProfile` credentials keep working | imports-with-report | COMP-07's cohort count |
| COMP-07 | `webRequest*` stays granted | deferred-needs-measurement | an installed-base cohort count |
| COMP-08 | vmess/trojan/ss/WireGuard are refused, not dropped | refused-with-reason | a feed carrying one, imported |
| COMP-09 | `pinCert`/`minVersion` are reported | refused-with-reason | doc 23's field set, doc 39's row |
| COMP-10 | `chrome.storage.session` guarded or floor raised | deferred-needs-measurement | LIVE-15 and its Chromium half |
| COMP-11 | Firefox keeps its own popup, backend and permission | imports-with-report | LIVE-15 in full |
| COMP-12 | the Connector is Windows-only this phase | refused-with-reason | a Linux ADR and a macOS ADR |
| COMP-13 | no third sync backend appears | survives-unchanged | both backends against a real remote |
| COMP-14 | `unlimitedStorage` stays granted | deferred-needs-measurement | envelope-size measurements |
| COMP-15 | the extension works with no Connector | survives-unchanged | a run with nothing registered |
| COMP-16 | a dangling `serverId` is refused by name | imports-with-report | the Phase-2 registry check |
| COMP-17 | profile names are not translation keys | refused-with-reason | nothing — the disclosure is the fix |
| COMP-18 | `password` is stripped from the synced blob | deferred-needs-measurement | `docs/architecture/40-open-decisions.md` |
| COMP-19 | no NEXORA upgrade clears a store | refused-with-reason | nothing — a prohibition |
| COMP-20 | both popup entry paths keep resolving | survives-unchanged | doc 20 §5's artifact assertion |

Read the verdict column as a whole, and note that the run asserts the numbers in this sentence: eleven rows are
`survives-unchanged` and `imports-with-report` and carry no outstanding measurement, or owe one that is already
named elsewhere; five are `refused-with-reason`, which is the
honest shape of a product that knows its edges; four are `deferred-needs-measurement`, and **none of the four may
be shipped as a control** (constraint 11). The distribution is not a mood — it is the argument of §3, that a
compatibility claim whose number lives outside the repository is not a claim yet.

## 11. Who owns each check

| Check | Owner | Mechanism |
|---|---|---|
| manifest floors, permission sets, divergent fields | `nexora/extension` | "the two inherited manifests carry the floor and permission lists this file claims", "the permission lists are the manifests, in order, including the one-sided pair", "the divergent fields are divergent in the files and not only in this list" |
| absent APIs are still absent, used APIs still on their lines | `nexora/extension` + Connector | "no API the file lists as absent appears in the inherited corpus", "every API the file says the legacy code calls is on the line it names" |
| profile types and aliases equal the registry | `nexora/extension` | "the profile-type registry has six types and four aliases, and this file names them", "every alias resolves to the type this file says it resolves to" |
| the `+`/`-` namespaces and the eleven scalars | `nexora/extension` | "the scalar option keys are the ones the default set writes", "the four namespaces a top-level key may live in are named, and the two bare special cases are where the file says" |
| backup shape and the two destructive paths | `packages/connector` + `nexora/extension` | "every citation in the matrix lands on a line that has something on it", "the legacy update path clears three stores, and this file names it so no cut-over inherits it" |
| sync backends are exactly two, and the four claimed absences are absent | `nexora/extension` | "the sync module requires exactly the backends this file names", "no third sync backend exists in the inherited tree, including the ones a help page might claim" |
| verdicts closed, one per row, and the column has the distribution the prose reads | `nexora/extension` | "the verdict vocabulary is closed and every row carries exactly one of its four members", "the verdict column has the distribution doc 38 reads aloud" |
| rows real, ids unique, evidence resolving, LIVE ids exist | `nexora/extension` | "every matrix id is a row, and the run has no gap in it", "every matrix row names evidence that resolves, in two citations or more, and says what it owes", "every LIVE row the matrix names exists in the live matrix" |
| the document names the data | `nexora/extension` | "the compatibility document names every row, verdict, type, alias, backend, option key, absent API, mode and open item the data declares" |
| browser floors, cohort counts, envelope sizes | a human with a browser | none yet — `Verified: NO`, COMP-07/10/14/18, owed by LIVE-15 and by doc 39 |

## 12. Threat, control, check

| What could go wrong | Control | Check |
|---|---|---|
| a support page prints a browser floor nobody measured | §3's rule and COMP-10's verdict | "an unmeasured floor is named as unmeasured, and the lines it hangs on still call the API", "a row awaiting a measurement names the measurement, and four rows do" |
| the alias set is forgotten by a new reader | §5's alias rule | "every alias resolves to the type this file says it resolves to", "no alias points at a type this file does not declare, so the walk in _handler cannot land on a name NEXORA never reads" |
| a NEXORA key lands in the profile namespace | ADR-0012 plus §6's dispatch evidence | "the profile namespace is a first-character test, and NEXORA adds no prefix" |
| a permission is dropped because a document said it was unused | §4's absence list | "no API the file lists as absent appears in the inherited corpus" |
| a `deferred` row quietly becomes a shipped button | §9's fourth verdict | "no row that claims survival owes an obligation no named check closes" |
| evidence rots as the tree changes | `matrixRules.evidenceResolves` | "every matrix row names evidence that resolves, in two citations or more, and says what it owes" |
| an old-format backup is rejected as invalid | §7's no-envelope rule | "a backup has no wrapper: the exported object is the options map, in JSON or in base64" |
| a cut-over reuses the legacy wipe | §7's destructive-path rule | "the legacy update path clears three stores, and this file names it so no cut-over inherits it" |

## 13. Invariants, amendments, and the open items

**Invariants.** (1) A row exists only with a verdict and evidence. (2) Evidence is a line in the inherited tree,
a pointer in a contract, or a section of a document — never a memory. (3) The `+` namespace stays the only
profile namespace. (4) `schemaVersion` stays 2 for the legacy engine, and NEXORA writes only its own key. (5) No
NEXORA upgrade path deletes user data. (6) A platform fact never stated as measured blocks the row that carries
it from reading `survives-unchanged`. Each is asserted in `packages/schema/scripts/verify.mjs` except the
browser-side halves, which are `Verified: NO` and named in §11.

**Amendments.** Adding a row means adding evidence and a verdict in the same change; removing one means saying
which promise stopped existing. Raising or lowering a browser floor requires a measurement attached to COMP-10
first, and the ADR that records it, because a floor is a statement about users who have not upgraded
(ADR-0015 keeps this extension on a real version line, so the floor is a support promise, not a build flag). A
new sync backend, a new profile type or a new top-level namespace each require a superseding ADR, not an edit
here — this document has no authority to widen the format of the user's data by itself.

**Open items** (`packages/schema/compatibility.json#/matrixRules/owedIsNamed`, the four `deferred-needs-measurement`
rows): the Chromium and
Gecko availability floors of `chrome.storage.session` (COMP-10); the authenticated-legacy-proxy cohort count that
decides whether `webRequest*` becomes an optional grant (COMP-07); envelope sizes without `unlimitedStorage`
(COMP-14, owed back to `docs/architecture/34-data-split.md` §9); the `password`-in-sync strip, which is a
cross-device behaviour change and therefore lives in `docs/architecture/40-open-decisions.md` (COMP-18). Held open
beside them, though carrying a different verdict, the Linux and macOS platform ADRs that would let COMP-12 stop
being a refusal.

Trace: ADR-0002, ADR-0004, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0015, ADR-0019 · Contract:
`packages/schema/compatibility.json`, `packages/schema/protocol/compatibility-rules.json`,
`packages/schema/migrations.json`, `packages/schema/routing-policy.schema.json`,
`packages/schema/server.schema.json`, `packages/schema/subscription-source.schema.json`,
`packages/schema/connector-profile.schema.json`, `packages/schema/envelope-keys.schema.json`,
`packages/schema/examples/pins.xray.json`, `packages/schema/presentation.json` · See also:
`docs/audit/12-what-must-not-change.md`, `docs/architecture/20-target-repository-layout.md`,
`docs/architecture/21-system-context.md`, `docs/architecture/22-component-contracts.md`,
`docs/architecture/23-unified-server-model.md`, `docs/architecture/24-options-envelope-compat.md`,
`docs/architecture/28-routing-model.md`, `docs/architecture/29-subscriptions-and-servers.md`,
`docs/architecture/31-security-model.md`, `docs/architecture/32-secrets-keystore-masking.md`,
`docs/architecture/33-binary-integrity.md`, `docs/architecture/34-data-split.md`,
`docs/architecture/35-schema-versioning-migration.md`, `docs/architecture/37-ui-architecture.md`,
`docs/architecture/39-verification-matrix.md`, `docs/verification/live-matrix.md`.
