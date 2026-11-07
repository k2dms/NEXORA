# 07 — Routing engine: the `Conditions` registry, rule-list grammar, the PAC AST, and representability in Xray routing

> Purpose: establish what ZeroOmega's routing layer can actually express today, at line-evidence
> fidelity, and then test that vocabulary against Xray's `routing` section — the deliverable being the
> representability table that ADR-0013 and `docs/architecture/28-routing-model.md` must be built on.
> Audit of ZeroOmega v3.5.2 as it sits in this tree; normative text lives in `docs/architecture`.

Tags: **VERIFIED** = read at the cited line; **INFERRED** = derived from cited code plus external
behaviour not observable in this repo; **UNVERIFIED** = listed in the registry at the end. Every claim
about Xray in §4 and §5 is **INFERRED**: no Xray source, binary or reachable upstream documentation
exists in this environment.

## 1. The `Conditions` registry

`omega-pac/src/conditions.coffee` exports one object (`module.exports = exports =`
`omega-pac/src/conditions.coffee:7`) whose private table `_conditionTypes`
`omega-pac/src/conditions.coffee:227-687` is the whole routing vocabulary. Dispatch is by string lookup
with a hard failure on unknown types `omega-pac/src/conditions.coffee:218-225`, specifically
`throw new Error "Unknown condition type: …"` `omega-pac/src/conditions.coffee:224`. Analysis is cached
per condition and invalidated by a tag built from the type plus the handler's own `tag`
`omega-pac/src/conditions.coffee:206-211`. Public entry points: `analyze`
`omega-pac/src/conditions.coffee:24-26`, `match` `omega-pac/src/conditions.coffee:28-31`,
`compile` `omega-pac/src/conditions.coffee:32-36`, `str` `omega-pac/src/conditions.coffee:37-52`,
`fromStr` `omega-pac/src/conditions.coffee:55-73`, `typeFromAbbr`
`omega-pac/src/conditions.coffee:76-84`.

**The request surface is three fields**: `requestFromUrl` yields `{url, host, scheme}` and nothing else
`omega-pac/src/conditions.coffee:8-14`. Every condition below is a predicate on those three strings.
**VERIFIED**

**True count: TWELVE.** Enumerating the keys of `_conditionTypes` in file order gives
`TrueCondition`, `FalseCondition`, `UrlRegexCondition`, `UrlWildcardCondition`, `HostRegexCondition`,
`HostWildcardCondition`, `BypassCondition`, `KeywordCondition`, `IpCondition`, `HostLevelsCondition`,
`WeekdayCondition`, `TimeCondition`. **VERIFIED** — this confirms PROJECT-STATE §2.7 and contradicts the
early nine-condition draft. The contract file states the same number
`packages/schema/routing-policy.schema.json:160-161`.

| # | conditionType (def) | abbrs | fields | `match` semantics | `compile` → PAC expression |
|---|---|---|---|---|---|
| 1 | `TrueCondition` `omega-pac/src/conditions.coffee:230` | `True` `omega-pac/src/conditions.coffee:231` | none | constant true `omega-pac/src/conditions.coffee:233` | `true` literal `omega-pac/src/conditions.coffee:234` |
| 2 | `FalseCondition` `omega-pac/src/conditions.coffee:238` | `False`, `Disabled` `omega-pac/src/conditions.coffee:239` | optional `pattern` `omega-pac/src/conditions.coffee:243-246` | constant false `omega-pac/src/conditions.coffee:241` | `false` literal `omega-pac/src/conditions.coffee:242` |
| 3 | `UrlRegexCondition` `omega-pac/src/conditions.coffee:248` | `UR`,`URegex`,`UrlR`,`UrlRegex` `omega-pac/src/conditions.coffee:249` | `pattern` | `/re/.test(request.url)` `omega-pac/src/conditions.coffee:251-252` | `/<escaped>/.test(url)` `omega-pac/src/conditions.coffee:253-254`, regex via `omega-pac/src/conditions.coffee:104-114` |
| 4 | `UrlWildcardCondition` `omega-pac/src/conditions.coffee:256` | `U`,`UW`,`Url`,`UrlW`,`UWild`,`UWildcard`,`UrlWild`,`UrlWildcard` `omega-pac/src/conditions.coffee:257-258` | `pattern`, `\|`-alternated | `|`-split shell-glob over the URL `omega-pac/src/conditions.coffee:259-264` | `/<joined>/.test(url)` `omega-pac/src/conditions.coffee:265-266` |
| 5 | `HostRegexCondition` `omega-pac/src/conditions.coffee:268` | `R`,`HR`,`Regex`,`HostR`,`HRegex`,`HostRegex` `omega-pac/src/conditions.coffee:269` | `pattern` | `/re/.test(request.host)` `omega-pac/src/conditions.coffee:271-272` | `/<escaped>/.test(host)` `omega-pac/src/conditions.coffee:273-274` |
| 6 | `HostWildcardCondition` `omega-pac/src/conditions.coffee:276` | `''`,`H`,`W`,`HW`,`Wild`,`Wildcard`,`Host`,`HostW`,`HWild`,`HWildcard`,`HostWild`,`HostWildcard` `omega-pac/src/conditions.coffee:277-278` | `pattern`, `\|`-alternated | leading `.`→`*`, `**.` = any depth, `*.` = "self or subtree" special case `omega-pac/src/conditions.coffee:279-294` | `/<joined>/.test(host)` `omega-pac/src/conditions.coffee:297-298` |
| 7 | `BypassCondition` `omega-pac/src/conditions.coffee:300` | `B`,`Bypass` `omega-pac/src/conditions.coffee:301` | `pattern` (Chrome bypass-list syntax) | scheme / CIDR / host / `host:port` / `<local>` union `omega-pac/src/conditions.coffee:366-370`, `<local>` = 127.0.0.1, ::1 or any dotless host `omega-pac/src/conditions.coffee:370-380` | url regex test `omega-pac/src/conditions.coffee:393-396`, `<local>` disjunction `omega-pac/src/conditions.coffee:398-420`, else `&&` of scheme + host-or-IP `omega-pac/src/conditions.coffee:423-439` |
| 8 | `KeywordCondition` `omega-pac/src/conditions.coffee:441` | `K`,`KW`,`Keyword` `omega-pac/src/conditions.coffee:442` | `pattern` | HTTP **only** + URL substring `omega-pac/src/conditions.coffee:444-445` | `scheme==='http' && url.indexOf(k)>=0` `omega-pac/src/conditions.coffee:446-463` |
| 9 | `IpCondition` `omega-pac/src/conditions.coffee:467` | `Ip` `omega-pac/src/conditions.coffee:468` | `ip`, `prefixLength` | literal-IP subnet test, v4/v6 families never cross `omega-pac/src/conditions.coffee:487-492` | guard-then-`isInNet`, `isInNetEx` preferred when defined `omega-pac/src/conditions.coffee:531-561`; /0 degrades to "looks like an IP" `omega-pac/src/conditions.coffee:526-530` |
| 10 | `HostLevelsCondition` `omega-pac/src/conditions.coffee:579` | `Lv`,`Level`,`Levels`,`HL`,`HLv`,`HLevel`,`HLevels`,`HostL`,`HostLv`,`HostLevel`,`HostLevels` `omega-pac/src/conditions.coffee:580-581` | `minValue`, `maxValue` | dot count over the host `omega-pac/src/conditions.coffee:583-590` | `host.split('.').length` in `[min+1,max+1]` `omega-pac/src/conditions.coffee:591-603`, via the range emitter `omega-pac/src/conditions.coffee:119-131` |
| 11 | `WeekdayCondition` `omega-pac/src/conditions.coffee:613` | `WD`,`Week`,`Day`,`Weekday` `omega-pac/src/conditions.coffee:614` | `days` (7 chars) **or** `startDay`/`endDay` | `new Date().getDay()` in the client's local week `omega-pac/src/conditions.coffee:616-619`; `days` = uppercase letter flags `omega-pac/src/conditions.coffee:200-204` | `new Date().getDay()` range or `"<flags>".charCodeAt(getDay)>64` `omega-pac/src/conditions.coffee:620-644` |
| 12 | `TimeCondition` `omega-pac/src/conditions.coffee:661` | `T`,`Time`,`Hour` `omega-pac/src/conditions.coffee:662` | `startHour`, `endHour` | `new Date().getHours()` in the client's local clock `omega-pac/src/conditions.coffee:664-666` | `new Date().getHours()` range `omega-pac/src/conditions.coffee:667-678` |

Three registry facts that matter to the compiler design:

1. **Two different abbreviations per type, and the emitter picks the last one.** Parsing accepts any
   entry of `abbrs`, case-insensitively, plus the type name itself
   `omega-pac/src/conditions.coffee:76-84`; serialisation with the default `abbr: -1` selects
   `abbrs[len-1]` `omega-pac/src/conditions.coffee:37`, `omega-pac/src/conditions.coffee:44-48`. So
   `FalseCondition` is written as `Disabled:`, `UrlRegexCondition` as `UrlRegex:`, and the round-trip is
   asserted by tests `omega-pac/test/conditions.coffee:517-520`.
2. **`HostWildcardCondition` owns the empty abbreviation**, which makes it the *default* type: a rule
   line with no `Type:` prefix resolves through `_abbrs['']`
   `omega-pac/src/conditions.coffee:79-82` + `omega-pac/src/conditions.coffee:59-63`. Serialisation
   correspondingly prints a bare pattern when the first abbr is empty and the pattern has no space and
   does not end in `:` `omega-pac/src/conditions.coffee:39-42`. Any compiler that guesses "host wildcard"
   for an unlabelled rule is inheriting this, not choosing it.
3. **Invalid regex does not fail**: `safeRegex` falls back to `/(?!)/`, a pattern that matches nothing
   `omega-pac/src/conditions.coffee:97-101`. A subscription with a broken regex silently becomes a
   never-matching rule rather than an error — the opposite of ADR-0013's "report, do not drop".

## 2. Rule-list grammar: three parsers and one `compose`

Two *formats* are registered — `formatByType` `omega-pac/src/profiles.coffee:60-62` and
`ruleListFormats: ['Switchy','AutoProxy']` `omega-pac/src/profiles.coffee:65-67` — but there are
**three parsers**, all in `omega-pac/src/rule_list.coffee`:

| Parser | Cite | Grammar |
|---|---|---|
| `AutoProxy.parse` | `omega-pac/src/rule_list.coffee:20-52` | AdBlock/AutoProxy line syntax; `@@` = exclusive, `/…/` regex, `\|\|` host, `\|` URL, bare = keyword |
| `Switchy.parseLegacy` | `omega-pac/src/rule_list.coffee:140-175` | Switchy! `#BEGIN`/`#END` blocks with `[WILDCARD]`/`[REGEXP]` sections, `!` = exclusive |
| `Switchy.parseOmega` | `omega-pac/src/rule_list.coffee:177-268` | SwitchyOmega `[SwitchyOmega Conditions]` header, `@with`/`@note` directives, `!` = exclusive, ` +profile` result suffix |

Selection: `Switchy.detect` matches the header prefix `omega-pac/src/rule_list.coffee:58-61` with
`omegaPrefix` `omega-pac/src/rule_list.coffee:55`; `getParser` downgrades to `parseLegacy` only when no
header is present and some line starts with `#` `omega-pac/src/rule_list.coffee:115-121`; `Switchy.parse`
is the two-line dispatcher `omega-pac/src/rule_list.coffee:63-66`. `AutoProxy` is detected by a
base-64 magic prefix `omega-pac/src/rule_list.coffee:9` (`W0F1dG9Qcm94` = `[AutoProxy`) or the literal
`omega-pac/src/rule_list.coffee:10-15`, and `preprocess` base-64-decodes the whole body
`omega-pac/src/rule_list.coffee:16-19` — i.e. a subscription may arrive entirely encoded, with no size or
scheme check at this layer (PROJECT-STATE §2.4).

Grammar detail worth inheriting exactly:

- AutoProxy maps `||domain` to `HostWildcardCondition` with a synthesised `*.` prefix
  `omega-pac/src/rule_list.coffee:38-41`, `|url` to `UrlWildcardCondition` with a trailing `*`
  `omega-pac/src/rule_list.coffee:42-43`, a `*`-free line to `KeywordCondition`
  `omega-pac/src/rule_list.coffee:44-46` (which is HTTP-only, §1 row 8), and anything else to
  `UrlWildcardCondition` wrapped as `http://*<line>*` `omega-pac/src/rule_list.coffee:47-49`. Regex lines
  strip the surrounding slashes `omega-pac/src/rule_list.coffee:34-36`. Exclusive `@@` rules are
  re-pointed at `defaultProfileName` and prepended `omega-pac/src/rule_list.coffee:29-32`,
  `omega-pac/src/rule_list.coffee:51-52`.
- Switchy! legacy honours section headers `omega-pac/src/rule_list.coffee:154-156`, ignores `;` comments
  `omega-pac/src/rule_list.coffee:147`, and converts wildcards through `conditionFromLegacyWildcard`
  `omega-pac/src/rule_list.coffee:123-138`, which uses `urlWildcard2HostWildcard`
  `omega-pac/src/conditions.coffee:16-22` to demote `*://host/*` to a host wildcard. Unknown sections
  yield `null` and are dropped `omega-pac/src/rule_list.coffee:164-172`.
- SwitchyOmega format: line classes header/comment/directive `omega-pac/src/rule_list.coffee:196-201`;
  `@with result` toggles per-rule destinations `omega-pac/src/rule_list.coffee:207-211`; `@note` attaches
  to the next rule only `omega-pac/src/rule_list.coffee:211-212`, `omega-pac/src/rule_list.coffee:251-253`;
  `!` marks a rule exclusive, which under `@with result` means "profile unresolved until the default is
  known" `omega-pac/src/rule_list.coffee:217-220`; the result name is taken from the **last** ` +` in the
  line `omega-pac/src/rule_list.coffee:221-233`; a `*` rule supplies the exclusive profile and missing
  defaults are back-filled from `defaultProfileName || 'direct'`
  `omega-pac/src/rule_list.coffee:258-267`; strict mode raises typed `missingResultProfile`,
  `invalidRule`, `noDefaultRule` errors with line numbers `omega-pac/src/rule_list.coffee:179-184`,
  `omega-pac/src/rule_list.coffee:224-228`, `omega-pac/src/rule_list.coffee:239-243`,
  `omega-pac/src/rule_list.coffee:261-263`. Non-strict mode silently `continue`s past every invalid line
  `omega-pac/src/rule_list.coffee:230`, `omega-pac/src/rule_list.coffee:245` — the default is drop, not
  report.

`compose` exists **only** on `Switchy` `omega-pac/src/rule_list.coffee:88-113` and is the shared
serialisation: CRLF `omega-pac/src/rule_list.coffee:89`, header
`omega-pac/src/rule_list.coffee:90`, `@with result` `omega-pac/src/rule_list.coffee:92-95`, `@note`
`omega-pac/src/rule_list.coffee:98-99`, condition text from `Conditions.str`
`omega-pac/src/rule_list.coffee:100`, `!` for rules equal to the default profile
`omega-pac/src/rule_list.coffee:101-102`, a `: ` escape when a condition would otherwise look like a
directive `omega-pac/src/rule_list.coffee:96`, `omega-pac/src/rule_list.coffee:104-105`, the ` +profile`
suffix `omega-pac/src/rule_list.coffee:106-108`, and the closing catch-all `* +default`
`omega-pac/src/rule_list.coffee:110-112`. Consequences, all **VERIFIED**:

- An AutoProxy or Switchy! list cannot be re-emitted: `RuleList['AutoProxy'].compose` is absent, so
  import ⇒ lossy one-way conversion at first save.
- Two open TODOs admit the escaping is incomplete for profile names containing ` +` or newlines
  `omega-pac/src/rule_list.coffee:107`, `omega-pac/src/rule_list.coffee:111`.
- `directReferenceSet` is also Switchy-only `omega-pac/src/rule_list.coffee:68-84` and only for
  `@with result` lists `omega-pac/src/rule_list.coffee:72-73`; the profile layer falls back to
  `matchProfileName`/`defaultProfileName` otherwise `omega-pac/src/profiles.coffee:455-462`, which is how
  a rule-list profile's reference set can under-report the profiles its own rules can reach.
- Format *detection* can never reject data: both `detect` implementations return `true` or `undefined`,
  never `false`, so the "wrong data for the current format" branch
  `omega-pac/src/profiles.coffee:504-506` is unreachable and a stored `format` always wins
  `omega-pac/src/profiles.coffee:499-519`.

## 3. `PacGenerator.script` — the AST walk

The module is a singleton with `ascii` `omega-pac/src/pac_generator.coffee:7-13` (escapes non-ASCII to
`\uXXXX`, because Chromium's PAC reader is not UTF-8 safe) and `compress`
`omega-pac/src/pac_generator.coffee:15-23` (`figure_out_scope`, `Compressor(warnings:false,
keep_fargs:true, if_return:false)`, `compute_char_frequency`, `mangle_names`).

**Range confirmed:** `script: (options, profile, args) ->` opens at
`omega-pac/src/pac_generator.coffee:25`; its last emitted expression is the `AST_Toplevel` built at
`omega-pac/src/pac_generator.coffee:146-153`; the CoffeeScript literal then closes on the bare bracket
line 157, and line 158 is the `coffeelint: enable` comment
`omega-pac/src/pac_generator.coffee:158`. So the brief's 25-157 span is right about the method, but the
last line carrying citable content is 153. **VERIFIED**

Structure of the emitted program:

1. Reference closure: name→profile resolution `omega-pac/src/pac_generator.coffee:26-27` and
   `Profiles.allReferenceSet` `omega-pac/src/pac_generator.coffee:28-29`, then a JS object literal of
   every referenced profile **except `+direct`** `omega-pac/src/pac_generator.coffee:31-39`, each value
   being the recursively compiled profile expression `omega-pac/src/profiles.coffee:233-238`.
2. A factory taking `(init, profiles)` and returning `function FindProxyForURL(url, host)`
   `omega-pac/src/pac_generator.coffee:41-49`, with `"use strict"`
   `omega-pac/src/pac_generator.coffee:52` and `scheme` derived as the prefix of `url` before the first
   `:` `omega-pac/src/pac_generator.coffee:53-69`.
3. The dispatcher: a `do { … } while (…)` loop `omega-pac/src/pac_generator.coffee:74-75` that (a)
   returns the current string if `profiles[result]` is missing
   `omega-pac/src/pac_generator.coffee:76-85`, (b) follows the chain `result = profiles[result]`
   `omega-pac/src/pac_generator.coffee:88-93`, (c) calls the value when it is a function, passing
   `(url, host, scheme)` `omega-pac/src/pac_generator.coffee:96-113`, and repeats while `result` is not
   a string **or** starts with `+` — the `+` test is `result.charCodeAt(0) === '+'.charCodeAt(0)`
   `omega-pac/src/pac_generator.coffee:119-138` — then returns
   `omega-pac/src/pac_generator.coffee:142`.
4. `var FindProxyForURL = factory(<profile key>, profiles)`
   `omega-pac/src/pac_generator.coffee:146-153`, where the initial value is the profile *key* from
   `Profiles.profileResult` `omega-pac/src/profiles.coffee:120-124` (which alone rewrites `+direct` into
   the PAC literal `DIRECT`).

AST nodes actually used by the generator: `AST_Toplevel`, `AST_Var`, `AST_VarDef`, `AST_SymbolVar`,
`AST_Function`, `AST_SymbolFunarg`, `AST_Object`, `AST_ObjectKeyVal`, `AST_Directive`, `AST_Return`,
`AST_Do`, `AST_BlockStatement`, `AST_If`, `AST_Assign`, `AST_Binary`, `AST_UnaryPrefix`, `AST_Sub`,
`AST_Call`, `AST_Dot`, `AST_Number`, `AST_String`
`omega-pac/src/pac_generator.coffee:31-54`, `omega-pac/src/pac_generator.coffee:74-149`. Condition
compilation adds `AST_True` `omega-pac/src/conditions.coffee:234`, `AST_False`
`omega-pac/src/conditions.coffee:242`, `AST_RegExp` `omega-pac/src/conditions.coffee:112-114`,
`AST_New` `omega-pac/src/conditions.coffee:625-627`, `AST_Conditional`
`omega-pac/src/conditions.coffee:551-561`; profile compilation adds `AST_Switch`, `AST_Case`,
`AST_Default` `omega-pac/src/profiles.coffee:328-337`.

**The raw-PAC splice and its comment-termination hack.** `PacProfile.compile`
`omega-pac/src/profiles.coffee:358-373` wraps the user's PAC text in a function scope and injects it as
raw source through `AST_Raw`, a subclass of `AST_SymbolRef` whose "name" *is* the code and which reports
`aborts = -> false` so the compressor will not prune it `omega-pac/src/profiles.coffee:8-12`. The injected
literal is `';\n' + profile.pacScript + '\n\n/* End of PAC */;'`
`omega-pac/src/profiles.coffee:373`, and the four-step rationale is documented in place — leading `;`
terminates an unclosed statement, the first `\n` kills a trailing `//` line comment, the second `\n`
survives a trailing backslash that escapes the newline, and `/* … */;` closes an unclosed block comment
`omega-pac/src/profiles.coffee:363-372`. A final `return FindProxyForURL`
`omega-pac/src/profiles.coffee:374-375` hands the wrapped function back to the caller, which the
generator then invokes as the profile value `omega-pac/src/profiles.coffee:359-360`. The same
`_comments_dumped` trick that lets UglifyJS print comments repeatedly lives in the condition layer
`omega-pac/src/conditions.coffee:86-95`.

Guarantees the generator does and does not provide:

| | |
|---|---|
| Provides | syntactically valid JS by construction (AST, never string concatenation) `omega-pac/src/pac_generator.coffee:146-153`; ASCII-only output `omega-pac/src/pac_generator.coffee:7-13`; a bounded name space — only `url`, `host`, `scheme` are in scope of the profile functions `omega-pac/src/pac_generator.coffee:47-49`; termination of the profile chain, because the loop exits on a string not starting with `+` `omega-pac/src/pac_generator.coffee:119-138` |
| Does not provide | validation of embedded PAC (raw splice, above); any error for a dangling profile reference — `Profiles.profileNotFound` throws only when no `profileNotFound` policy is given `omega-pac/src/profiles.coffee:175-191`, and in the generated script a missing key simply *returns the `+Name` string* `omega-pac/src/pac_generator.coffee:76-85`, which Chromium will read as a malformed proxy line (**INFERRED**); any cycle protection beyond that exit condition; any check that `SystemProfile` is never compiled — it throws instead `omega-pac/src/profiles.coffee:256-258` |

One more generation path exists outside the generator: subscription rule lists are pre-compiled into a
PAC *string* by `generateProfileTempPac` `omega-target/src/options.coffee:29-45` whenever a
`RuleListProfile` is stored `omega-target/src/options.coffee:462-463`, and that string is then re-embedded
by the raw splice above (`Profiles.compile` prefers `pacScript` for non-temp rule lists
`omega-pac/src/profiles.coffee:484-491`). So a `RuleListProfile` today traverses *two* generations
(PAC-from-rules, rules-PAC-spliced-into-PAC) — a NEXORA compiler must pick one.

## 4. THE REPRESENTABILITY TABLE

Read `Xray routing` as the `routing.rules[]` object vocabulary (per-connection matchers `domain`, `ip`,
`port`, `sourcePort`, `network`, `protocol`, `inboundTag`, `user`, plus `outboundTag`/`balancerTag`
targets and the top-level `domainStrategy`, `sniffing`, `balancers`, `reverse`). That vocabulary is
**INFERRED**: see the registry §4 of this document's UNVERIFIED list. The *left* side of every row is
**VERIFIED** in this repo.

| Omega condition / rule-list feature | How it is evaluated today | Expressible in Xray `routing`? | Lost / must be approximated | Verdict |
|---|---|---|---|---|
| `TrueCondition` `omega-pac/src/conditions.coffee:230` | compiled `true` in browser PAC `omega-pac/src/conditions.coffee:234`; in-process `match` on listener paths `omega-pac/src/conditions.coffee:233` | only as an always-matching rule; `coreRule` requires a selector (`minProperties: 2`, `packages/schema/routing-policy.schema.json:352-360`), so the idiom is `port: "0-65535"` (`packages/schema/examples/routing.two-plane.json:44-45`) | no "no selector" rule exists in the contract; the port idiom silently excludes future non-IP transports | APPROXIMATE |
| `FalseCondition` `omega-pac/src/conditions.coffee:238` | compiled `false` `omega-pac/src/conditions.coffee:242` — a *disabled* rule `omega-pac/src/rule_list.coffee:101-102` | trivially: emit nothing | "disabled but remembered" state is lost unless kept browser-side | EXACT (as deletion) |
| `UrlRegexCondition` `omega-pac/src/conditions.coffee:248` | `/re/.test(url)` in PAC `omega-pac/src/conditions.coffee:253-254` | **no** — Xray matches destination domain/IP/port/`protocol`, never a URL; only sniffed HTTP `Host` + SNI are visible | path/query/host-portion semantics disappear; HTTPS URLs are never recoverable | UNSUPPORTED |
| `UrlWildcardCondition` `omega-pac/src/conditions.coffee:256` | `|`-alternated glob over URL `omega-pac/src/conditions.coffee:259-266` | **no**, same reason | everything right of `host:port` disappears; `http://*x*`-style AutoProxy keywords become unmatchable | UNSUPPORTED |
| `HostRegexCondition` `omega-pac/src/conditions.coffee:268` | `/re/.test(host)` `omega-pac/src/conditions.coffee:273-274` | yes as a `regexp:` domain matcher (`domain` array `packages/schema/routing-policy.schema.json:360-367`) | JS regex vs Xray's matcher dialect; `\|` alternation must be rewritten; the contract has **no field** declaring which dialect a string is in | APPROXIMATE |
| `HostWildcardCondition` `omega-pac/src/conditions.coffee:276` | glob→regex with the `*.` subtree special case `omega-pac/src/conditions.coffee:279-294`, `omega-pac/src/shexp_utils.coffee:22-51` | yes: `*.x` ≡ Xray `domain:x` suffix rule; `x` ≡ `full:x` | `**.` (any-depth) and `?` single-char have no Xray equivalent; `trimAsterisk` makes a trailing `*` a no-op `omega-pac/src/shexp_utils.coffee:28-34` | APPROXIMATE |
| `BypassCondition` `omega-pac/src/conditions.coffee:300` | *three* sites: Omega's own `match` `omega-pac/src/conditions.coffee:366-385`, compiled PAC `omega-pac/src/conditions.coffee:393-439`, and Chromium's bypass list via `_formatBypassItem` `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:74-77` | CIDR → `ip` (`packages/schema/routing-policy.schema.json:370-377`), `host:port` → `domain` + `port` (`packages/schema/routing-policy.schema.json:380-383`) | `<local>` = "127.0.0.1, ::1, or **any dotless host**" `omega-pac/src/conditions.coffee:370-380` cannot be expressed by any Xray matcher; scheme-prefixed bypass (`http://…`) collapses | PARTIAL |
| `KeywordCondition` `omega-pac/src/conditions.coffee:441` | HTTP-only substring of the URL `omega-pac/src/conditions.coffee:444-445` | **no** — the Core never sees a substring of a URL; sniffed HTTP host is *nearest* and not equivalent | non-HTTP requests can never match today either, and the approximation changes behaviour | UNSUPPORTED |
| `IpCondition` `omega-pac/src/conditions.coffee:467` | `isInNet`/`isInNetEx` inside the browser PAC `omega-pac/src/conditions.coffee:531-561`, or `ip-address` subnet math in-process `omega-pac/src/conditions.coffee:487-492` | yes: CIDR in `ip` | the browser version **resolves the host to test an IP literal guard first** `omega-pac/src/conditions.coffee:493-530`; Xray's `domainStrategy` decides resolution *policy* instead (`packages/schema/routing-policy.schema.json:72-79`) — see defect §7.3 | EXACT (DNS side-effects differ) |
| `HostLevelsCondition` `omega-pac/src/conditions.coffee:579` | dot count `omega-pac/src/conditions.coffee:583-590` / `host.split('.').length` `omega-pac/src/conditions.coffee:591-603` | **no** label-count matcher exists; only an enumerated `regexp` expansion | unbounded label sets ⇒ cannot be generated for wide ranges | UNSUPPORTED (approximable per-value) |
| `WeekdayCondition` `omega-pac/src/conditions.coffee:613` | `new Date().getDay()` at PAC evaluation time, on the browser host `omega-pac/src/conditions.coffee:616-619` | **no** calendar predicate in `routing` | also: "browser-local week" ≠ "core-local week" if the connector runs in another TZ/timezone-locked container | UNSUPPORTED |
| `TimeCondition` `omega-pac/src/conditions.coffee:661` | `new Date().getHours()` `omega-pac/src/conditions.coffee:664-666` | **no** clock predicate in `routing` | same TZ hazard; hour granularity only | UNSUPPORTED |
| SwitchProfile ordered first-match `omega-pac/src/profiles.coffee:418-422` | PAC `if/else` chain in rule order `omega-pac/src/profiles.coffee:423-436`; Xray rules are likewise "first match wins" (INFERRED) | yes, order preserved as an array (`packages/schema/routing-policy.schema.json:108-112`) | nothing lost *if* every condition survives; a dropped condition reorders the rest | EXACT (mechanically) |
| `defaultProfileName` fallback `omega-pac/src/profiles.coffee:422` | last `return` of the chain `omega-pac/src/profiles.coffee:435-436` | yes, as a trailing catch-all rule | requires the `port: "0-65535"` idiom above | APPROXIMATE |
| Chain-of-profiles ("result") `omega-pac/src/pac_generator.coffee:74-142` | runtime `while` following `+Name` keys until a PAC string appears | **no** — Xray routes a connection to exactly one outbound; there is no "then ask the user's other profile" | the entire multi-profile intent hierarchy is invisible to the Core (see §4.2) | UNSUPPORTED |
| Rule-list `@with result` `omega-pac/src/rule_list.coffee:207-233` | per-rule destination profile | yes, if every destination maps to a compiled `ConnectorProfile` outbound | `unrepresentable` reporting is free text (`packages/schema/routing-policy.schema.json:115-123`), not per-condition | PARTIAL |
| Exclusive `!` rules `omega-pac/src/rule_list.coffee:217-220`, `omega-pac/src/rule_list.coffee:101-102` | re-pointed to the default profile | yes as normal rules | loses the "exclusive" marker; round-trip through `compose` re-derives it from `profileName == defaultProfileName` only | PARTIAL |
| `@note` `omega-pac/src/rule_list.coffee:211-212` | carried in `rule.note`, re-emitted | no Xray analogue | cosmetic; keep browser-side (`note` field exists in the contract, `packages/schema/routing-policy.schema.json:54-56`) | EXACT (browser plane only) |
| FixedProfile `bypassList` `omega-pac/src/profiles.coffee:265-277` | delegated to Chromium `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:68-72` | yes for host/CIDR items | identical `<local>` hole as row 7 | PARTIAL |
| Per-domain temp rules `omega-target/src/options.coffee:957-964` | host wildcard injected at apply time `omega-target/src/options.coffee:676-699` | yes (they are always `*.<domain>`) | only if the compiler reads the **temp** profile, not the user's | EXACT (with care) |

### 4.1 Why an exact mapping is impossible — reason 1: the browser decides first

On the Chromium path the whole routing decision *is* a PAC file handed to the browser
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:32-36`, generated inside the
extension `omega-target-chromium-extension/src/module/proxy/proxy_impl.coffee:62-65`. By the time a
connection reaches the Core, the browser has already chosen the loopback port
(`packages/schema/connector-profile.schema.json:5`). Xray routing can therefore only ever be a
*sub*-decision of an already-fixed choice; it cannot promote a request to a different profile.
**VERIFIED** for the browser side, **INFERRED** for the Core side.

### 4.2 Why it is impossible — reason 2: the result chain is invisible

`FindProxyForURL` returns a chain string such as `PROXY h:p; DIRECT` or a single `SOCKS5 h:p`
`omega-pac/src/profiles.coffee:82-89`, and the profile chain may pass through several profiles before a
terminal string appears `omega-pac/src/pac_generator.coffee:88-93`,
`omega-pac/src/profiles.coffee:280-290`. The *user intent* encoded there (fall back, then give up, with a
specific order, per-scheme `omega-pac/src/profiles.coffee:298-321`) is consumed by Chromium's proxy
resolution and never travels to the connector — `config.apply` args admit nothing else
(`packages/schema/protocol/commands/config.apply.json:11-14`, `additionalProperties: false`). Xray can
pick one outbound per connection; it cannot express "try, then fall back to direct", except by its own
`balancers`/failover machinery, which is a different semantic (**INFERRED**).

### 4.3 Why it is impossible — reason 3: time, weekday and wildcard semantics

`WeekdayCondition` and `TimeCondition` evaluate `new Date()` **in the browser**, compiled into the script
`omega-pac/src/conditions.coffee:620-644`, `omega-pac/src/conditions.coffee:667-678`; the same two types
are evaluated by *Omega's own JS* on listener paths `omega-pac/src/conditions.coffee:616-619`,
`omega-pac/src/conditions.coffee:664-666`. Moving them to the Core changes the clock (browser TZ vs
connector TZ) and changes the moment of decision (PAC evaluation vs connection setup). Host wildcards are
worse: the `*.` special case rewrites a prefix into `(?:^|\.)` and strips the trailing `.*`
`omega-pac/src/conditions.coffee:289-291`, `shexp2RegExp` with `trimAsterisk` drops leading *and* trailing
asterisks `omega-pac/src/shexp_utils.coffee:28-34`, and the anchors are only added at the string edges
`omega-pac/src/shexp_utils.coffee:36-49`. A Xray `domain:` suffix rule is *not* that function:
`*.a.b` in Omega matches `a.b` itself as well (INFERRED from the `(?:^|\.)` rewrite), which no naive
`domain:` translation reproduces.

### 4.4 Why it is impossible — reason 4: Core capabilities with no Omega analogue

| Xray capability | Omega analogue in this tree | Evidence status |
|---|---|---|
| `balancers` / `balancerTag` (load-balanced outbound groups, health-aware) | none: profile selection is a fixed chain; `selection.mode` is a *new* contract concept | INFERRED Xray side; repo side **VERIFIED** absent — `packages/schema/routing-policy.schema.json:155-157` allows only `direct`, `block`, `srv-<hex8>`, i.e. balancer targets are *forbidden by the contract* |
| `reverse` (bridge/portals, inbound-driven reverse proxying) | none, and out of scope for a browser proxy client | UNVERIFIED |
| `sniffing` (derive domain from HTTP Host / TLS SNI) | none — Omega already holds the full URL, so it never needed sniffing | contract models it: `packages/schema/routing-policy.schema.json:81-87`, `routeOnly` default `true` `packages/schema/routing-policy.schema.json:101-104` |
| `protocol` matchers (`http`,`tls`,`quic`,`fakedns`,`ws`,`bittorrent`) | none: only `scheme` (`http`/`https`/`ftp`) exists in the request `omega-pac/src/conditions.coffee:11-14`, and `KeywordCondition` hard-codes `scheme==='http'` `omega-pac/src/conditions.coffee:444-445` | contract allows six values: `packages/schema/routing-policy.schema.json:397-409` |
| `network` tcp/udp | none — PAC cannot see transport | `packages/schema/routing-policy.schema.json:385-393` |
| destination `port` / ranges | only `host:port` inside `BypassCondition` `omega-pac/src/conditions.coffee:332-337` | `packages/schema/routing-policy.schema.json:380-383` |
| `domainStrategy` DNS-disposition model | the inverse: Omega *avoids* DNS by guarding on host shape first `omega-pac/src/conditions.coffee:493-530` | `packages/schema/routing-policy.schema.json:72-79` |
| `inboundTag` / `user` / `sourceIp` / `userGeoip` / `inferenceOnly` / geo databases | none — no client identity, no geo concept anywhere in `omega-pac/src/conditions.coffee` | UNVERIFIED for Xray spelling; **VERIFIED** absent from the contract (`coreRule` lists exactly seven properties, `packages/schema/routing-policy.schema.json:359-418`) |
| Ordered first-match with `invert` | match order only, no negation primitive | `packages/schema/routing-policy.schema.json:413-415` |

## 5. The reverse direction, stated plainly

Omega's vocabulary is **URL-and-clock** based: `{url, host, scheme}` plus the browser clock
`omega-pac/src/conditions.coffee:8-14`, `omega-pac/src/conditions.coffee:616-619`. Xray's vocabulary is
**connection-and-sniffed-metadata** based: address, port, transport, inferred domain, inbound identity,
geo sets. Neither contains the other. The overlap is exactly: destination host (glob/regex), destination
IP/CIDR, destination port, and a constant-true fallback — which is why the table above yields
`APPROXIMATE`/`PARTIAL` more often than `EXACT`. Everything Omega has beyond that overlap (URL, keyword,
weekday, hour, host levels, profile chains) is browser-plane-only; everything Xray has beyond it
(sni/http sniffing, protocol, network, balancers, reverse, inbound identity) is core-plane-only and has no
Omega editor.

## 6. The two-plane conclusion (observation; becomes ADR-0013 / `28-routing-model.md`)

The code already separates the two decisions; the audit vocabulary for it is:

- **Browser plane** — *which profile*. `SwitchProfile` ordered first-match
  `omega-pac/src/profiles.coffee:418-422` → `applyProfile` hand-off
  `omega-target/src/options.coffee:699-701` → `chrome.proxy` or `proxy.onRequest`
  `omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:37-41`,
  `omega-target-chromium-extension/src/module/proxy/proxy_impl_listener.coffee:28-34`.
- **Core plane** — *which outbound inside the profile that traffic reached*. Expressed by
  `routing.rules[]` `packages/schema/routing-policy.schema.json:108-112` over compiled outbounds named by
  `packages/schema/routing-policy.schema.json:155-157`.

**Invariant (observation): exactly one plane decides the fate of a request.** Given §4.1, the only
consistent assignment is: the browser plane decides *profile*, the core plane decides *outbound*, and a
rule is compiled into the core plane only when the browser-plane outcome for that rule is already fixed to
that profile. The moment the same rule exists in both planes, two engines can disagree and the invariant
breaks — duplication, not absence, is the violation mode. A third plane is already latent in this tree and
must be counted: Chromium's own bypass matcher evaluates `bypassList`
`omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee:68-72`, and the
`<local>` semantics it inherits `omega-pac/src/conditions.coffee:370-380` are the clearest proof that some
Omega rules are only *approximately* portable. The contract's own wording already asserts the split
`packages/schema/routing-policy.schema.json:25-32`, `packages/schema/routing-policy.schema.json:63-70`.

## 7. Cross-reference against the already-written contracts

### 7.1 `packages/schema/routing-policy.schema.json`

`$defs.condition` accepts **all twelve** legacy types across eight `oneOf` branches:
`TrueCondition` `packages/schema/routing-policy.schema.json:168-169`,
`FalseCondition` `packages/schema/routing-policy.schema.json:176-177`,
`UrlRegexCondition`/`UrlWildcardCondition`
`packages/schema/routing-policy.schema.json:191-194`,
`HostRegexCondition`/`HostWildcardCondition`/`BypassCondition`/`KeywordCondition`
`packages/schema/routing-policy.schema.json:208-213`,
`IpCondition` `packages/schema/routing-policy.schema.json:228-229`,
`HostLevelsCondition` `packages/schema/routing-policy.schema.json:250-251`,
`WeekdayCondition` `packages/schema/routing-policy.schema.json:268-269` (with `days` XOR
`startDay/endDay` expressed as `anyOf` `packages/schema/routing-policy.schema.json:287-317`),
`TimeCondition` `packages/schema/routing-policy.schema.json:329-330`. Its own description states the
count and points at the registry `packages/schema/routing-policy.schema.json:160-161` — **consistent
with the twelve found in code**. Browser-plane rule shape is `{condition, profileName, note?}`
`packages/schema/routing-policy.schema.json:37-56` with `defaultProfileName`
`packages/schema/routing-policy.schema.json:34-35`, and profile names must not carry the envelope `+`
`packages/schema/routing-policy.schema.json:148-153` (matching `omega-pac/src/profiles.coffee:93-96`).
**VERIFIED**

Per-branch coverage of the *core* side (`$defs.coreRule`
`packages/schema/routing-policy.schema.json:352-418` offers exactly `domain`
`packages/schema/routing-policy.schema.json:360-367`, `ip`
`packages/schema/routing-policy.schema.json:370-377`, `port`
`packages/schema/routing-policy.schema.json:380-383`, `network`
`packages/schema/routing-policy.schema.json:385-393`, `protocol`
`packages/schema/routing-policy.schema.json:397-409`, `invert`
`packages/schema/routing-policy.schema.json:413-415`, `outbound`
`packages/schema/routing-policy.schema.json:417-418`):

| Condition | Has a core-plane target? | How |
|---|---|---|
| `TrueCondition` | yes | catch-all via `port` (see `packages/schema/examples/routing.two-plane.json:44-45`) |
| `FalseCondition` | n/a | dropped |
| `HostWildcardCondition` | yes | `domain[]` |
| `HostRegexCondition` | yes | `domain[]` in `regexp:` dialect — the dialect is **undeclared** in the schema |
| `IpCondition` | yes | `ip[]` CIDR |
| `BypassCondition` | partial | host/CIDR/port subsets only; `<local>` unrepresentable |
| `UrlWildcardCondition`, `UrlRegexCondition`, `KeywordCondition`, `HostLevelsCondition`, `WeekdayCondition`, `TimeCondition` | **no** | must land in `unrepresentable` |

`unrepresentable` `packages/schema/routing-policy.schema.json:115-123` is `{source, reason}` free text
(max 200 chars each) — so it can record a decision but cannot be machine-checked for *which* of the twelve
were dropped. The shipped example does exactly that for `WeekdayCondition` and `KeywordCondition`
`packages/schema/examples/routing.two-plane.json:49-56`, and the reason text there is the correct
argument: Xray has no calendar predicate, and the Core sees at most a sniffed HTTP host, never the URL.

### 7.2 `packages/schema/protocol/commands/config.apply.json`

It covers **zero** conditions directly and delegates by reference: `args` are `profileIds` plus the `intent`
documents the frame carries (`packages/schema/protocol/commands/config.apply.json:16-36` -- the `intent-down` movement's own carrier, and the
command's answer to its register question), `additionalProperties:
false` `packages/schema/protocol/commands/config.apply.json:13`. Consequences: no browser-plane rule, no
PAC text and no URL ever crosses the wire as *routing input* (which is *right* for the two-plane invariant, §6) --
the intent documents carry `keystore://` references, never values, and the routing the compiler reads is the
two-plane policy document the frame states; the compiler
runs in the Connector, and the arguments still do not carry a compiled config; the report channel is
`result.unrepresentable` `packages/schema/protocol/commands/config.apply.json:81-94` with `dryRun`
`packages/schema/protocol/commands/config.apply.json:37-41` as the only pre-flight
`packages/schema/protocol/commands/config.apply.json:5-8`, and the byte-stability invariant
`packages/schema/protocol/commands/config.apply.json:116` is what makes §4's approximations testable.
The same free-text `{source, reason}` shape repeats here, so the "which conditionType was dropped" gap is
protocol-wide, not schema-local.

### 7.3 Contract defects this audit can now name

1. `domainStrategy` enum is `["ASKING","IPIfNonMatch","IPAsOnly"]`
   `packages/schema/routing-policy.schema.json:72-79` and the shipped minimal example uses `"ASKING"`
   `packages/schema/examples/routing.minimal.json:19`. The documented Xray values are `AsIs`,
   `IPOnDemand`, `IPIfNonMatch` — `ASKING` and `IPAsOnly` are not among them, and the two-plane example uses
   the one valid value `packages/schema/examples/routing.two-plane.json:36`. **INFERRED** externally, so
   this is a *finding to confirm in Phase 2 against the pinned binary*, not a proven defect; if the
   external reading is right, a valid-per-schema config would be rejected or defaulted by Xray at parse
   time, and `verify.mjs` would still pass.
2. The contract's outbound vocabulary `packages/schema/routing-policy.schema.json:155-157` deliberately
   excludes `balancerTag`, so §4.4's `balancers` capability is unreachable by design. That is a decision to
   make explicit in ADR-0013, not leave as a pattern accident.
3. `coreRule` has no field for `userGeoip`, `inferenceOnly`, ruleset references, `sourceIp`, `inboundTag`
   or `user` `packages/schema/routing-policy.schema.json:359-418`, so a NEXORA UI cannot express geo-based
   routing at all even though Xray can; `geosite:` currently leaks in as a magic string inside `domain[]`
   `packages/schema/examples/routing.two-plane.json:40`.
4. Nothing in either contract says which *dialect* a `domain[]` string is in (`full` / `suffix` /
   `regexp` / `keyword` / `geosite`), while §1 shows Omega's own wildcard dialect is not the same function
   `omega-pac/src/conditions.coffee:289-291`, `omega-pac/src/shexp_utils.coffee:28-49`. An implicit dialect
   is how a compiler silently changes meaning.

## UNVERIFIED registry

1. **Xray's routing vocabulary and semantics as a whole.** No Xray source, binary or reachable upstream
   documentation in this session (`xtls.github.io/en/config/routing.html` fetch failed; only search
   snippets and a third-party mirror were reachable). Field spellings, `domainStrategy` casing
   (`IPIfNonMatch` vs `IpIfNonMatch`), and the existence of `userGeoip`, `inferenceOnly`, `reverse`,
   `rulesSet` and `proxyProtocolTag` are therefore **INFERRED/UNVERIFIED**. Pin context:
   `packages/schema/pins.schema.json:30-31`, example tag
   `packages/schema/examples/pins.xray.json:5`. Go is not installed (PROJECT-STATE §1), so nothing was
   executed.
2. Whether Xray's rule evaluation is strictly ordered first-match — asserted as INFERRED in §4; the table
   row depends on it.
3. Chromium's behaviour on a malformed PAC line produced by a dangling profile reference
   `omega-pac/src/pac_generator.coffee:76-85` — no browser available.
4. Chromium's bypass-list semantics the code deliberately mirrors
   `omega-pac/src/conditions.coffee:370-380` (the comment cites Chromium source, not this repo).
5. `isInNetEx` availability in real PAC engines
   `omega-pac/src/conditions.coffee:543-547` — the code already treats it as optional.
6. That the twelve-type count matches every *derived* document in the repo other than
   `packages/schema/routing-policy.schema.json:160-161`; sibling audit/architecture docs were being written
   concurrently and were not on disk at authoring time.
7. Test-suite status for `conditions`/`rule_list`/`pac_generator` was not re-run here; the pinned floor is
   142 passing / 1 failing (PROJECT-STATE §5).
8. Whether `parseOmega` non-strict dropping of invalid lines
   `omega-pac/src/rule_list.coffee:230`, `omega-pac/src/rule_list.coffee:245` is observable today through
   the UI error surface.
