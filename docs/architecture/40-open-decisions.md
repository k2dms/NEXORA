# 40 — The open questions: what Phase 1 could not answer, who is capable of answering, and what artefact closes each

> Purpose: 60 lines across 19 documents defer to this file as the place an answer lives, and five more name it only to
> list it. A deferral that names nothing is
> a way of not answering politely, so the register is data — `packages/schema/open-decisions.json` — re-derived from
> those lines on every run, and this document is the argument the data refuses to carry: why each question could not
> be closed here, what the choices actually cost, and which kind of artefact would end it. `docs/adr/0001-adr-practice.md`
> makes an ADR the only place a decision is recorded; `packages/schema/verification.json` queues what nothing checks;
> this file queues what nobody has chosen. The two are different obligations and one does not substitute for the
> other: a row that closes by a decision (`VER-54`, `VER-61`) is waiting on an entry here, and an entry here that
> gates no row is a question nobody is blocked by.

Status: Accepted. Normative data: `packages/schema/open-decisions.json`,
`packages/schema/open-decisions.json#/scope`, `packages/schema/open-decisions.json#/citation`,
`packages/schema/open-decisions.json#/notADeferral`, `packages/schema/open-decisions.json#/decidedBy`,
`packages/schema/open-decisions.json#/register`.
Machine-checked by: `packages/schema/scripts/verify.mjs` (§9 names the assertions).

---

## 1. What this document owns, and what it refuses

**Owns.**

- The argument for each question: why it is genuinely open rather than merely unfinished, and what a reader would be
  lied to about if Phase 1 had invented an answer.
- The grouping of the 40 entries into the kinds of thing that blocks them — so that "open" is not one undifferentiated
  pile, and so the sixteen `contract` questions do not look as expensive as the six `adr` ones.
- The tie backwards: which `VER-NN` rows and which `COMP-NN` rows stop moving until an entry here is answered.

**Refuses.**

- The decisions. Nothing in this file decides anything: `contract` answers appear as fields in
  `packages/schema`, `adr` answers as a new numbered file in `docs/adr/`, `measurement` answers as a number in
  `packages/schema/compatibility.json`, and `product` answers as screens and wording in
  `docs/architecture/37-ui-architecture.md`. A question that gets answered *in prose here* has not been answered.
- The queue of unchecked claims — `packages/schema/verification.json` and `docs/architecture/39-verification-matrix.md`
  own those. This document is not a source of deferred verification claims and MUST NOT become one: it carries no
  `Verified:` label, and the run says so ("the register document is a list of questions, not a source of deferred
  verification claims of its own").
- The human decision list and its wording — `docs/PROJECT-STATE.md` §9 stays the page a person reads when they are
  deciding. §7 here says how an answer reaches that page and comes back.
- The register itself as prose — `packages/schema/open-decisions.json#/register` is the list. §6 renders it; the data
  owns it.

The split is the same one every other document in this set uses: the JSON is the list, this file is the reason.

---

## 2. How a line becomes a question

The membership rule is the data's own (`packages/schema/open-decisions.json#/citation`) and it is executable, so it
is worth stating precisely.

1. **A deferral site is a line that names this file** — by basename or by path, which are the same mention. The
   corpus uses both: `docs/PROJECT-STATE.md` writes `` `40-open-decisions.md` `` where the architecture documents
   write the path, and a scan that required the path would silently lose a deferral for a formatting preference.
2. **A header that introduces a list defers its bullets, not itself.** Five lines in the corpus end in a colon and
   then a bulleted list — `docs/architecture/33-binary-integrity.md` §17's "Open, and deliberately not decided here"
   is the clearest. Those headers are the sentence that says "these are open"; the bullets are the deferrals.
   Counting both would inflate the register and, worse, would let one header claim six questions.
3. **A bullet may wrap.** A continuation line counts as part of its bullet's site, which is why the scan reads whole
   documents rather than grepping single lines: `docs/architecture/23-unified-server-model.md` §7 puts the filename
   alone on its own line at the end of a paragraph, and that line — not the paragraph's first — is the site.
4. **An inventory line is not a deferral.** Five lines name this file only to list it among documents or to say where
   an answer belongs: the two status lines in `docs/PROJECT-STATE.md` §4.4 and §4.5, the routing bullet in
   `docs/README.md`'s conventions list, `docs/architecture/20-target-repository-layout.md` §13's planned-files row, and
   the document list in `docs/architecture/39-verification-matrix.md` §13. They are declared in
   `packages/schema/open-decisions.json#/notADeferral` and the run checks that each still resolves to one line and
   still would otherwise have counted. That is the difference between excluding a case and forgetting it.

The result is 61 sites, and the partition is exact in both directions: no site without an entry, no entry without a
site, no site owned twice. The second direction is the one that matters most here — an entry with no citing line is
a question invented by the register, and the register is supposed to be a *report* of what the corpus deferred, not a
wishing well.

That constraint had a real cost. `VER-61` (subscription delivery) was pointed at two labelled lines that did not
carry its claim — `docs/architecture/27-config-compilation-pipeline.md` §5's ceiling refusal belongs to `VER-73`, and
`docs/architecture/33-binary-integrity.md` §11's door-back prohibition belongs to `VER-27` — because the delivery
question had **no line of its own anywhere**. It existed only as `docs/PROJECT-STATE.md` §9's fourth decision, which
is a note to a person rather than a claim in the architecture. `docs/architecture/29-subscriptions-and-servers.md`
§13 now carries it, and the corpus count went up by one. A question that cannot be cited is a question that will be
answered by accident.

---

## 3. The five kinds of answer

`packages/schema/open-decisions.json#/decidedBy` is a closed vocabulary of five values, each defined by what it
costs to answer:

| Value | Count | What closing it looks like | Why it is not interchangeable with the others |
|---|---|---|---|
| `product` | 14 | a screen, a sentence, or a refusal the user sees — written into `docs/architecture/37-ui-architecture.md` and the command docs | no measurement can decide what the user should be told; a schema field cannot make the choice less arbitrary |
| `contract` | 16 | a field, an enum member, an error code or a schema bound in `packages/schema`, plus its examples | choosing costs a commit and a verifier run; nothing about it needs a person or a browser |
| `adr` | 6 | a new numbered file in `docs/adr/` that supersedes or extends an accepted decision | answering changes a boundary, so `docs/adr/0001-adr-practice.md` requires the record; a field edit would hide it |
| `measurement` | 2 | a `COMP-NN` row in `packages/schema/compatibility.json` with a number, a browser, a version and a date | there is no opinion to argue with — the value is in a machine nobody has asked yet |
| `upstream` | 2 | a documented behaviour of the browser stack, Xray or AMO policy, cited outside this repository | the answer is not held here; the honest output is a stated residual, not a decision |

Fourteen of the 40 are `product`. That is the honest shape of Phase 1: the architecture is settled about mechanisms
and unsettled about behaviour, because mechanisms can be derived from constraints and behaviour can only be chosen.
Sixteen `contract` questions look different from the outside than they are: each is a field nobody wrote, and each
costs one commit once chosen — which is why they are the rows a Phase-2 commit can delete without waiting on a
browser, a store or a binary.

One question gates more rows than any other: `ODEC-14` (what `block` promises) stops five at once — `VER-21`,
`VER-74`, `COMP-07`, `COMP-10`, `COMP-14`. `ODEC-09` and `ODEC-27` gate three each, twenty-one entries gate exactly two
and sixteen gate one; that is the honest shape of a design phase — most open questions are one field wide, and the few
that are not are the few §5 argues about.

Two questions are cited by four or more sites each — `ODEC-15` (the built-in sync secret, five) and `ODEC-01`
(Connector ownership across two browser profiles, four) — and four sit at three (`ODEC-05`, `ODEC-09`, `ODEC-14`,
`ODEC-26`). Repetition is not agreement, and the register counting it is the point: five sites about a gist token in
`chrome.storage.sync` is one question nobody has answered five times.

---

## 4. Why none of these could be answered in Phase 1

There is a tempting failure mode for a phase like this one: invent the answer, because an open question reads as
incompetence. Every entry here resisted that, and the reasons are of six kinds.

1. **The answer is a number that lives in a browser.** `ODEC-31` asks what a `chrome.storage.sync` envelope actually
   holds without `unlimitedStorage`; `ODEC-11` asks when `<all_urls>` can leave the manifest. Writing a plausible
   figure would let `docs/architecture/34-data-split.md` §9's three conditions "pass" against a fiction. These are
   `measurement`, and `packages/schema/compatibility.json` already carries the discipline: a verdict of
   `deferred-needs-measurement` is not a verdict of failure — it is an admission that nobody has looked.
2. **The answer is what the user should be told.** `ODEC-16` (a legacy authenticated proxy in an imported backup) has
   three defensible shapes and no defensible derivation. The constraint that decides it — "a UI button must not imply
   a finished feature" — tells us what we may not say, not what to say. Picking one silently would make the screen
   the architecture's opinion rather than the product's.
3. **The answer crosses a boundary, so it is not a field edit.** `ODEC-08` (a Shadowsocks-heavy feed), `ODEC-17`
   (encrypting the pre-migration snapshot), `ODEC-27` (Linux and macOS provenance), `ODEC-33` (who owns
   subscriptions), `ODEC-35` (what actually crosses the second pipe) and `ODEC-15` each change what a component is
   *for*. `docs/adr/0001-adr-practice.md` makes that an
   ADR, and an ADR needs the argument that a Phase-1 document cannot supply on the strength of a schema.
4. **The answer depends on an artefact that does not exist yet.** `ODEC-03` (which error code means "cannot be
   compiled") has its call sites now — `connector/internal/compile/validate.go` refuses a bind, and
   `connector/cmd/nexora-connector/config_apply.go` classifies every pipeline refusal before it answers — and the
   artefact still missing is a taxonomy entry whose registered meaning is that sentence. Until one exists the surface
   can only follow the prose the corpus already has, which is what its marker table does and what its comments say it
   is doing. It is recorded rather than guessed because a guess made at a call site is a decision a client cannot
   re-ask: the code has to pick one string per refusal on every frame.
5. **The answer is held by someone else.** `ODEC-14` asks what `block` means against the browser stack: DNS
   disposition, WebRTC, and the `chrome.alarms` floor that every fail-closed timer in
   `docs/architecture/30-failure-policy.md` rests on. `ODEC-37` asks the same kind of question of the other process:
   four positions in an emitted Core config — an inbound's protocol, the protocol behind a `direct` or `block`
   outbound, where `sniffing` rides, and whether routing's `network` and `protocol` are arrays or one string — have no
   spelling anywhere in this repository, and the file that holds the answer is the pinned binary's own accepted
   config. Both are `upstream`, and the only honest closing artefact is a measured residual plus a sentence the user
   can read: an invention here would be a byte string some other project is free to stop accepting.
6. **The answer was hidden by a check that passes.** `ODEC-34` asks where a chunk's bytes ride. Nothing in
   `packages/schema` says, and `packages/schema/scripts/verify.mjs` prints a green chunk block anyway, because every
   clause it was given is about slice *metadata* — `contentHash`, `chunkSeq`, `chunkCount`, `encoding` — and a
   reassembly needs a body. The fixtures showed it before any Go did: the recorded group declares three slices, sends
   the first two with `args.profiles` empty and no `args.servers` at all, and puts the whole document in the third,
   while all three repeat one `encoding: "base64"` and one whole-document hash. That is not a wrong contract; it is a
   contract that asserts everything except the byte string its own atomicity rule is written against. A green check
   over an undefined body is how a design phase ends up leaving the next component a guess instead of a spec.

What Phase 1 did instead: every entry names at least three options and the cost of each (`…#/register/N/options`),
and every entry names the rows it gates. An answer can therefore be *audited against a Phase-2 diff* — which is what
makes this a register rather than a list of worries.

---

## 5. The seven that decide the shape of the product

Most entries are one field wide. Seven of them would move the product if answered the other way.

**`ODEC-01` — who owns the single Connector.** Adoption is described
(`docs/architecture/21-system-context.md` §11's launch model: a lock file with `{pid, pipeVersion, startedAt,
configHash}`, adopt rather than replace), but the lock's staleness rule, what a dead lock means, and — the part that
keeps returning — whose policy governs when a second profile arrives, are not.
`docs/architecture/21-system-context.md` §6 asks the same thing as a state, and
`docs/architecture/30-failure-policy.md` §15 asks it as a failure: two browser profiles, one Connector, one applied
config. Until it is chosen, no test can tell a correct adopt from a correct refusal, which is exactly why `VER-12`
and `VER-14` cannot be written.

**`ODEC-06` — the 64-inbound ceiling.** `docs/architecture/27-config-compilation-pipeline.md` §5 states the
consequence plainly: 200 servers need 200 inbounds, the schema says `maxItems: 64`, so the compiler refuses and
`profile.import` answers `QuotaExceeded` naming the count. What nobody has chosen is whether 64 is the number, or
merely the number in today's document. Option three — compile inbounds only for servers a reachable rule can select —
is the technically interesting one and the one that makes a user's subscription behave differently depending on their
routing, which is a `product` cost, not a memory saving.

**`ODEC-08` — the legacy feed.** `packages/schema/server.schema.json#/properties/protocol` is `const "vless"`, so a
Shadowsocks-heavy feed is lossy *by construction* and `docs/architecture/23-unified-server-model.md` §7 says so out
loud rather than letting a parser skip entries quietly. The three options differ in who carries the surprise: the
import report, a visible-but-inert legacy kind, or the refusal. Expanding the enum instead is an ADR and an Xray-side
matrix, and it is on the list so nobody mistakes "the schema says one value" for "the product decided one value".

**`ODEC-14` — what `block` blocks.** The failure policy's escalation ladder rests on a browser behaviour nobody here
controls: `docs/architecture/30-failure-policy.md` §15 marks the `chrome.alarms` floor and Chromium's DNS disposition
while blocked as `INFERRED` — no browser was run — and `docs/architecture/31-security-model.md` §14 records that the
inherited default bypass list literally contains `127.0.0.1`, `::1` and `localhost`, and §17's open list adds "DNS
disposition and WebRTC while blocked, which the browser does not let us control". NEXORA's `block` primitive has no
bypass list, and that half is asserted in data; the browser half is not ours to choose. `COMP-07`, `COMP-10` and `COMP-14` are where it becomes numbers. Narrowing the promise (option
two) is the cheapest answer and the one a user can be told about honestly; escalating (option three) means
per-platform controls whose differences the compatibility matrix has not yet measured.

**`ODEC-15` — the built-in sync secret.** `gistToken` in `chrome.storage.sync` is the one secret in the inherited
data path that never passes through `transformValueForSync`, so no strip rule can reach it
(`docs/architecture/32-secrets-keystore-masking.md` §14, `docs/architecture/34-data-split.md` §6). Five sites name
this document for it, which is the corpus saying the same uncomfortable thing five times: ADR-0010's rule that
secrets do not sync is true of NEXORA's own fields and false of the one it inherited. Every option admits a loss —
a device that never runs the migration, a second device that must re-enter a token, or an exception that stays
documented. That is why it is `adr` and not a field.

**`ODEC-33` — who owns subscriptions.** `docs/architecture/29-subscriptions-and-servers.md` §2 concludes that
`subscription.refresh` results are single-frame because a feed body never crosses the pipe toward the extension. The
conclusion is sound and its premise is the open question: if the extension is ever to hold a subscription — chunked
or otherwise — then the frame contract, the §11 invariant that no feed body enters the browser, and the data split in
`docs/architecture/34-data-split.md` all move together. It was answered by accident for a whole phase because nobody
wrote the question down.

**`ODEC-35` — what crosses the second hop.** ADR-0005 rule 2 (`docs/adr/0005-two-process-connector.md:30-32`) promises
a local pipe between the two processes whose message schema is versioned separately and carries its own version field,
and no file in `packages/schema` defines it — `packages/schema/protocol/framing.json` scopes itself, in its own
`#/description`, to "the extension-to-host pipe". `docs/architecture/25-native-messaging-protocol.md` §2 closes the
last escape: the same envelope crosses hop 2 verbatim,
`packages/schema/protocol/envelope.schema.json#/additionalProperties` is `false` over fourteen properties none of which
is a sender, and ADR-0004 rule 3 forbids a payload field from *becoming* identity even if the object were opened — yet
the connector's dispatch takes an `Identity` on every call. Three transport shapes survive, and they disagree about
where identity lives, so the answer moves ADR-0005's boundary and the reasoning ADR-0004 rests on: a new file in
`docs/adr/`, which is what `adr` costs. Chosen in Phase 1 it would have been a transport for a process nobody has
built, picked in the one document §8 forbids from picking anything. **That last clause went stale inside this
iteration, and the staleness is the entry's urgency.** `connector/cmd/nexora-connector` exists, serves the recorded
handshake, and takes its `Identity` from two flags an operator types, printing on stderr that the flags are a stand-in
for the answer this entry still owes (`docs/PROJECT-STATE.md` §4.13). So the question is no longer which transport is
architecturally nicer: it is which one a running process moves onto, and a wrong answer is now a migration rather than
a paragraph.

---

## 6. The register

The rows are `packages/schema/open-decisions.json#/register`; the columns are those, in the order the data gives
them, and "gates" is what stops moving without the answer.

| # | Question | Decided by | Gates |
|---|---|---|---|
| `ODEC-01` | who owns the single Connector when a second profile reaches it: adopt, refuse, replace | `contract` | `VER-12`, `VER-14` |
| `ODEC-02` | the two version floors — `protocolVersion` and `minPipeVersion` — and what each refuses | `contract` | `VER-18` |
| `ODEC-03` | which error code means "this intent cannot be compiled" | `contract` | `VER-16`, `VER-20` |
| `ODEC-04` | how the block port range and the inbound allocation range stay disjoint across two files | `contract` | `VER-20` |
| `ODEC-05` | what `localDNS` means, and whether `IPIfNonMatch` is the right default | `product` | `VER-27`, `VER-75` |
| `ODEC-06` | whether 64 inbounds is the ceiling, and what a 200-server feed gets | `product` | `VER-68`, `VER-73` |
| `ODEC-07` | whether the Connector may fetch a feed through another proxy, including a loopback one | `product` | `VER-59`, `VER-65` |
| `ODEC-08` | what a legacy Shadowsocks/Trojan/WireGuard feed becomes | `adr` | `VER-69`, `VER-71` |
| `ODEC-09` | whether `password` joins the sync strip set | `product` | `VER-22`, `VER-74`, `COMP-18` |
| `ODEC-10` | whether any external API surface survives for third-party compatibility | `product` | `VER-54` |
| `ODEC-11` | when `<all_urls>` can leave the manifest | `measurement` | `VER-56`, `COMP-07` |
| `ODEC-12` | whether loopback inbounds get per-profile credentials | `product` | `VER-23`, `VER-45` |
| `ODEC-13` | how the Firefox AMO id is managed as both continuity constraint and security principal | `product` | `VER-15`, `VER-52` |
| `ODEC-14` | what `block` actually promises against DNS, WebRTC and the alarm floor | `upstream` | `VER-21`, `VER-74`, `COMP-07`, `COMP-10`, `COMP-14` |
| `ODEC-15` | how the built-in sync config leaves `chrome.storage.sync` | `adr` | `VER-22`, `VER-24` |
| `ODEC-16` | what the UI says about a legacy authenticated proxy in an imported backup | `product` | `VER-31`, `COMP-07` |
| `ODEC-17` | whether the pre-migration snapshot is encrypted at rest | `adr` | `VER-33` |
| `ODEC-18` | whether the `host` fingerprint key rotates, and what the diagnostic history means after | `contract` | `VER-23`, `VER-45` |
| `ODEC-19` | what happens when `restartAttempts` and `restartBackoffSeconds` disagree | `contract` | `VER-29` |
| `ODEC-20` | who owns "Core is alive, unmanaged and unsupervised" | `contract` | `VER-14`, `VER-21` |
| `ODEC-21` | whether "blocked" needs its own field for a feed refresh to survive it | `contract` | `VER-21`, `VER-63` |
| `ODEC-22` | whether the pin ships inside the Connector or as a detached signed update | `product` | `VER-41`, `VER-42` |
| `ODEC-23` | whether `geosite`/`geoip` routing is a feature at all, and how data files get pinned | `product` | `VER-27`, `VER-41` |
| `ODEC-24` | what an air-gapped install does with a Core newer than the pin | `product` | `VER-41`, `VER-62` |
| `ODEC-25` | whether NEXORA signs the Core it rehosts | `product` | `VER-41`, `VER-43` |
| `ODEC-26` | what removes the compiled config after an unclean kill | `contract` | `VER-23`, `VER-24` |
| `ODEC-27` | the Linux and macOS equivalents of the Windows integrity story | `adr` | `VER-23`, `VER-43`, `COMP-12` |
| `ODEC-28` | how many pre-migration snapshots are kept, and who answers when the disk is full | `product` | `VER-33` |
| `ODEC-29` | whether legacy `derived` fields are ever evicted or frozen where ADR-0002 left them | `contract` | `VER-22` |
| `ODEC-30` | whether the unbounded `log-store` and state blob get a compaction policy | `product` | `VER-48` |
| `ODEC-31` | the real envelope sizes without `unlimitedStorage` | `measurement` | `VER-74`, `COMP-14` |
| `ODEC-32` | whether mirrored legacy profile keys are name-derived or minted | `contract` | `VER-33` |
| `ODEC-33` | whether the Connector owns subscriptions end to end, or the extension may receive Servers in chunks | `adr` | `VER-61` |
| `ODEC-34` | where a chunk slice carries its bytes, and which byte string its `contentHash` names | `contract` | `VER-76` |
| `ODEC-35` | how the sender identity crosses hop 2, and what the local pipe's message schema is | `adr` | `VER-77` |
| `ODEC-36` | which byte string `configHash` covers — the handle-bearing document or the materialized bytes | `contract` | `VER-78` |
| `ODEC-37` | what bytes the Compiler emits for the four positions the corpus names and never spells | `upstream` | `VER-28`, `VER-78` |
| `ODEC-38` | over what span the ladder's restart attempts are spent, and how long a restarted Core must stay up to earn a fresh budget | `contract` | `VER-79` |
| `ODEC-39` | where `config.apply` gets the intent its ids cannot carry, which two directories it is handed, and what the compiled config is called | `contract` | `VER-80` |
| `ODEC-40` | where the format an `auto` parse resolved to is recorded — a field in the refresh result, a value stored from something else, or nowhere | `contract` | `VER-81` |

Each entry in the data also carries the options (§4's argument in three lines each), the reason it is open, and the
sites that deferred to it; the 40 rows above are the same 40 objects, not a summary of them.

`ODEC-40` (which artefact records the format an `auto` parse resolved to) is the newest, and it is the first question
this repository raised by reading its own shipped contract — no code was written, and no code failed.
`docs/architecture/29-subscriptions-and-servers.md` §5 states a detection rule and then a storage claim: `auto` is
allowed only when exactly one parser's detector answers true, and the value therefore never survives into a stored
source because the parser records which detector won. The first half is a behaviour the Connector would implement. The
second half names a place a fact is kept, and no artefact in `packages/schema` keeps it:
`protocol/commands/subscription.refresh.json#/result` bounds each `refreshed[]` item with `additionalProperties: false`
over eight properties, none of them a format, and the same file's `#/args` states in its own description that the
Connector "writes none of them back — the result is not a mutated source". So the value that decides what a stored
`format` should say is produced in the Connector's plane, the source it would correct is owned by the browser plane
(`docs/architecture/34-data-split.md` §6), and the only channel between them carries eight named fields and refuses a
ninth. All three readings are fields and sentences in `packages/schema`, which is why this is `contract`, why the row it
gates is `no-code` even though the Connector owns no part of it, and why that row is the register's cheapest: the answer
costs a property and two sentences, and nothing in it waits for a person, a browser or a binary.

`ODEC-39` (what a real `config.apply` reads and where it puts what it writes) is the one before it, and it is the first
this repository raised by *finishing* a path rather than designing one.
`connector/cmd/nexora-connector/config_apply.go` compiles, writes, reserves and reconciles in one `Handle`, and to do
that it had to be handed three things the corpus does not name: the intent documents behind the profile ids, because
the command's own `#/args` is `additionalProperties: false` over ids while
`packages/schema/protocol/data-ownership.json#/movements` — its `intent-down` entry — says this command's payload *is* those
documents; a state directory and a config directory, whose locations no document states and a test can only pass
`t.TempDir()` for; and a name for the compiled config, where `compiled-config.location` says the name is derived from
the hash and §8 of `docs/architecture/27-config-compilation-pipeline.md` — plus `connector/internal/apply`, plus the
test that shows a replaced config is a restart and not a new file — says it is one stable file renamed over itself. The
three are one entry because they are one seam: whichever way the documents arrive decides what the other two are
called. It is `contract` for the same reason `ODEC-34` and `ODEC-35` are: the answer is strings and fields in
`packages/schema`, and writing it costs a commit, not a browser.

`ODEC-38` (over what span the attempts are spent) is the one before it: its site is the row `docs/architecture/30-failure-policy.md` §13
carries, and it was found the way most of these are — by writing the thing. `connector/internal/supervisor` re-arms the
ladder on every death because doc 30 §4 counts attempts inside one death and names no denominator for the next, so the
reading the code took is the one the corpus permits and the one a crash-looping Core lives inside indefinitely. The
answer is a field, which is why this is `contract` and not `adr`: `failure-policy.schema.json#/properties/coreDeath`
has one integer and no window, so none of the three readings is expressible in the shipped data, and no test can bound
consecutive deaths until one of them is.

`ODEC-37` (the four spellings a golden must pin) is the one before it: it was born from the golden pass that pinned three of
doc 27 §3's six fixtures and found the other three unpinnable for three different reasons — shape C needs the handle
answer in `ODEC-36`, the fail-closed fixture has no bytes to pin because `docs/architecture/30-failure-policy.md` §7
puts the block in `chrome.proxy` rather than in Core's config, and the core-plane fixture is this entry: the rules it
would emit have no spelled form in this repository. Its citing lines are
`docs/architecture/23-unified-server-model.md` §9, which now names the list rather than pretending the three canonical
configs all compile, and the §9 row in `docs/architecture/27-config-compilation-pipeline.md` that defers to this
register.

`ODEC-36` (which byte string `configHash` names) is the one before that: its citing line is the §9 invariant row in
`docs/architecture/27-config-compilation-pipeline.md` that defers to this register, and the claim it turns on is the
determinism statement at `docs/architecture/27-config-compilation-pipeline.md:65` ("The hash is over the exact bytes
written") — the run checks that this line still carries text, and that no other line names `40-open-decisions.md` at
the register row's site.

---

## 7. What closes a question

A question closes when its artefact exists, not when a document agrees it probably should:

| `decidedBy` | Closing artefact | What must happen in the same change |
|---|---|---|
| `contract` | a field, enum member, code or bound in `packages/schema`, with its valid and invalid examples | `…#/register` loses the entry, and the citing lines in §2 stop naming this file — the run fails until both have happened |
| `product` | wording and states in `docs/architecture/37-ui-architecture.md` plus the command doc that carries it | the `VER-NN` rows it gates gain their check, or a live row is named for them |
| `adr` | a new numbered file in `docs/adr/` that names what it supersedes | `docs/adr/README.md`'s index grows, and the superseded ADR keeps its text (`docs/adr/0001-adr-practice.md` rule 5: history is not corrected in place) |
| `measurement` | a `COMP-NN` row with a number, browser, version and date | the `VER-NN` row that waited on the number moves out of `needs-human` |
| `upstream` | a documented behaviour, cited outside this repository, and the residual stated | `docs/architecture/38-compatibility-matrix.md` carries the sentence the user reads |

The deletion is the mechanism. `packages/schema/open-decisions.json#/citation/counted` is compared to what the corpus
actually says on every run, so an answered question that is left in the register makes the register wrong — it claims
a deferral nobody makes any more. That is deliberate: a stale "open" entry hides a decision, and a hidden decision is
worse than an unanswered question, because at least the unanswered one tells the truth about itself.

`docs/PROJECT-STATE.md` §9 remains the reading list for the human decisions in here; when a person answers one, the
answer goes into the artefact in the table above and §9 loses its line, not this file gaining a paragraph.

---

## 8. What is forbidden

| # | Rule | Why |
|---|---|---|
| 1 | A question MUST NOT be answered in this document. | The artefact is the answer; prose here is a decision with no owner, no date and no diff. |
| 2 | An entry MUST NOT exist without a line in the corpus deferring to it. | The register reports deferrals. An invented question is a worry dressed as a contract. |
| 3 | A site MUST NOT be owned by two entries. | Two entries believing they answer one deferral is the register lying about how many questions there are. |
| 4 | `decidedBy` MUST come from the five-value vocabulary, and the value MUST be the kind of artefact the answer really is. | "Open" with no closing kind cannot be audited; it can only be re-asked forever. |
| 5 | An entry MUST name at least two options and the cost of choosing each. | A question with one option is a decision waiting for a signature, and those belong in an ADR. |
| 6 | A `VER-NN` row that closes by a decision MUST be gated by an entry here. | Otherwise the queue waits on a question nobody wrote down, which is how `VER-61` got mis-cited twice. |
| 7 | This document MUST NOT carry a `Verified:` label. | The queue in `packages/schema/verification.json` owns deferred claims; a second source of them would double-count work nobody scheduled. |
| 8 | An entry MUST NOT be deleted because the question is inconvenient or the phase ended. | Removing the register line while the corpus still defers to it fails the run, and correctly so. |

---

## 9. What this run asserts

| Obligation | Assertion that enforces it |
|---|---|
| the register is derived from the corpus, not a file list | `the register is derived from the whole documentation corpus, not from the files it was written against` |
| the site rule counts what the data declares | `the number of lines that defer to the open-decisions register is the number the register declares` |
| the five excluded lines are still one line each and still would count | `each line the register declares "not a deferral" is exactly one line and really would otherwise count as one` |
| every citing line still defers here | `every entry in the register lands on one line of one document, and that line still defers to it` |
| no deferral is unregistered | `no deferral in the corpus is left without a register entry, so no open question is unregistered work` |
| ids are dense and every question was born from a site | `the register is a closed, gapless set of questions and every question was born from a line that deferred to it` |
| a question is a question, with options and a reason and a decider | `every open question is a question, names who can answer it, offers the choices a reader must weigh, and says why` |
| the who-can-decide vocabulary is closed and defined | `the register owns a closed vocabulary of who-can-decide, each member defined, and a boundary it states itself` |
| gates point at real rows, and decision rows have a written question | `every row a question gates exists, and every row the queue says waits on a decision is gated by a written question` |
| an entry must block something | `no register entry asks a question that blocks nothing, so an open question is work owed and not a worry noted` |
| this document names every entry and every vocabulary word | `the open-decisions document names every registered question and every word of the who-can-decide vocabulary` |
| this document is not a source of deferred claims | `the register document is a list of questions, not a source of deferred verification claims of its own` |

---

## 10. Invariants

1. 40 questions, 61 sites, one owner per site, at least one site per question. The numbers are outputs of the run,
   not contracts; only `packages/schema/open-decisions.json#/citation/counted` states one, and the run compares it.
2. Every question gates at least one row. An entry that blocks nothing is a question the architecture does not need
   answered yet, and belongs in a note rather than the register.
3. Five rows close by a decision — `VER-54`, `VER-61`, `VER-79`, `VER-80` and `VER-81` — and each is named here
   (`ODEC-10`, `ODEC-33`, `ODEC-38`, `ODEC-39`, `ODEC-40`).
4. Nothing here asserts that a decision is coming, or that a phase ends with them answered. `docs/adr/0001-adr-practice.md`
   allows an ADR to be superseded and this register to be emptied; it does not allow either to be quietly redefined.
5. A `measurement` or `upstream` question cannot be closed by this repository alone. `packages/schema/compatibility.json`
   is where those become numbers, and `docs/verification/live-matrix.md` is where the browser gets asked.
6. The register is the only place a question is *registered*; the artefact is the only place it is *answered*.
   Confusing the two is how a design phase ends up with opinions nobody chose.

---

**Trace:** ADR-0001 (an ADR is where a decision is recorded; history is not corrected in place); ADR-0002 (no mass
rewrite — `ODEC-29` and `ODEC-15` are its residue); ADR-0010 and ADR-0011 (secrets, masking, keystore — `ODEC-09`,
`ODEC-12`, `ODEC-15`, `ODEC-17`, `ODEC-18`); ADR-0012 (no content-addressed secrets — `ODEC-32`); ADR-0013 (routing
representability — `ODEC-05`, `ODEC-06`, `ODEC-23`); ADR-0016 and ADR-0017 (UI and i18n surfaces — `ODEC-10`,
`ODEC-16`); ADR-0019 (no committed binaries — `ODEC-22`, `ODEC-24`, `ODEC-25`, `ODEC-27`); ADR-0020 (test tiers, and
the promotion rule that makes a deferred claim name its queue).
Contract: `packages/schema/open-decisions.json`, `packages/schema/verification.json#/closingKinds` (`decision`),
`packages/schema/compatibility.json#/matrix` (`deferred-needs-measurement`).
See also: `docs/architecture/39-verification-matrix.md` §8 and §9, `docs/PROJECT-STATE.md` §9.
