# 40 — The open questions: what Phase 1 could not answer, who is capable of answering, and what artefact closes each

> Purpose: 60 deferral sites in 15 documents point here as the place an answer lives, and six more lines name it only to
> list it. A deferral that names nothing is
> a way of not answering politely, so the register is data — `packages/schema/open-decisions.json` — re-derived from
> those lines on every run, and this document is the argument the data refuses to carry: why each question could not
> be closed here, what the choices actually cost, and which kind of artefact would end it. `docs/adr/0001-adr-practice.md`
> makes an ADR the only place a decision is recorded; `packages/schema/verification.json` queues what nothing checks;
> this file queues what nobody has chosen. The two are different obligations and one does not substitute for the
> other: a row that closes by a decision (`VER-54`) is waiting on an entry here, and an entry here that
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
- The grouping of the 36 entries into the kinds of thing that blocks them — so that "open" is not one undifferentiated
  pile, and so the 12 `contract` questions do not look as expensive as the 6 `adr` ones.
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
4. **An inventory line is not a deferral.** Six lines name this file only to list it among documents, to say where
   an answer belongs, or to record that an entry that is no longer here once lived in it: the two status lines in
   `docs/PROJECT-STATE.md` §4.4 and §4.5, the routing bullet in
   `docs/README.md`'s conventions list, `docs/architecture/20-target-repository-layout.md` §13's planned-files row,
   the document list in `docs/architecture/39-verification-matrix.md` §13, and the sentence in
   `docs/adr/0021-subscription-servers-travel-in-the-refresh-result.md`'s Context that says this register is where the
   premise it retires was recorded. They are declared in
   `packages/schema/open-decisions.json#/notADeferral` and the run checks that each still resolves to one line and
   still would otherwise have counted. That is the difference between excluding a case and forgetting it.

The result is 60 sites, and the partition is exact in both directions: no site without an entry, no entry without a
site, no site owned twice. The second direction is the one that matters most here — an entry with no citing line is
a question invented by the register, and the register is supposed to be a *report* of what the corpus deferred, not a
wishing well.

That constraint had a real cost. `VER-61` (subscription delivery) was pointed at two labelled lines that did not
carry its claim — `docs/architecture/27-config-compilation-pipeline.md` §5's ceiling refusal belongs to `VER-73`, and
`docs/architecture/33-binary-integrity.md` §11's door-back prohibition belongs to `VER-27` — because the delivery
question had **no line of its own anywhere**. It existed only as `docs/PROJECT-STATE.md` §9's fourth decision, which
is a note to a person rather than a claim in the architecture. `docs/architecture/29-subscriptions-and-servers.md`
§13 item 6 gained it on 2026-11-06, and §13 lost it on 2026-11-07 when the answer retired the question and the site
with it, and the count came back down by one. Read the two halves in order and they are the same sentence: a question
that cannot be cited is a question that will be answered by accident — and this one was almost answered by accident
twice, once by a document that assumed the answer and once by a note addressed to nobody.

---

## 3. The five kinds of answer

`packages/schema/open-decisions.json#/decidedBy` is a closed vocabulary of five values, each defined by what it
costs to answer:

| Value | Count | What closing it looks like | Why it is not interchangeable with the others |
|---|---|---|---|
| `product` | 14 | a screen, a sentence, or a refusal the user sees — written into `docs/architecture/37-ui-architecture.md` and the command docs | no measurement can decide what the user should be told; a schema field cannot make the choice less arbitrary |
| `contract` | 14 | a field, an enum member, an error code or a schema bound in `packages/schema`, plus its examples | choosing costs a commit and a verifier run; nothing about it needs a person or a browser |
| `adr` | 5 | a new numbered file in `docs/adr/` that supersedes or extends an accepted decision | answering changes a boundary, so `docs/adr/0001-adr-practice.md` requires the record; a field edit would hide it |
| `measurement` | 2 | a `COMP-NN` row in `packages/schema/compatibility.json` with a number, a browser, a version and a date | there is no opinion to argue with — the value is in a machine nobody has asked yet |
| `upstream` | 2 | a documented behaviour of the browser stack, Xray or AMO policy, cited outside this repository | the answer is not held here; the honest output is a stated residual, not a decision |

14 of the 37 are `product`. That is the honest shape of Phase 1: the architecture is settled about mechanisms
and unsettled about behaviour, because mechanisms can be derived from constraints and behaviour can only be chosen.
14 `contract` questions look different from the outside than they are: each is a field nobody wrote, and each
costs one commit once chosen — which is why they are the rows a Phase-2 commit can delete without waiting on a
browser, a store or a binary.

One question gates more rows than any other: `ODEC-14` (what `block` promises) stops five at once — `VER-21`,
`VER-74`, `COMP-07`, `COMP-10`, `COMP-14`. `ODEC-09` and `ODEC-27` gate three each, 22 entries gate exactly 2 and 12 gate 1; that is the honest shape of a design phase — most open questions are one field wide, and the few
that are not are the few §5 argues about.

Two questions are cited by four or more sites each — `ODEC-15` (the built-in sync secret, five) and `ODEC-01`
(Connector ownership across two browser profiles, four) — and five sit at three (`ODEC-05`, `ODEC-06`, `ODEC-09`,
`ODEC-14`, `ODEC-26`). Repetition is not agreement, and the register counting it is the point: five sites about a gist token in
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
   (encrypting the pre-migration snapshot), `ODEC-27` (Linux and macOS provenance), `ODEC-35` (what actually crosses
   the second pipe) and `ODEC-15` each change what a component is
   *for*. `docs/adr/0001-adr-practice.md` makes that an
   ADR, and an ADR needs the argument that a Phase-1 document cannot supply on the strength of a schema. A sixth that
   reads like this one — `ODEC-33`, who owns subscriptions — left the register on 2026-11-07 for exactly this reason:
   the answer was a boundary, so it became `docs/adr/0021-subscription-servers-travel-in-the-refresh-result.md`
   rather than a field, and it became that by elimination rather than by preference.
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
   the four formerly missing spellings are implemented, but the pinned binary silently ignores `invert` and
   config acceptance does not establish every protocol predicate. The remaining answer needs paired runtime
   evidence, not another successful parse. Both are `upstream`, and the only honest closing artefact is a measured residual plus a sentence the user
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
conclusion was sound and its premise was the open question: if the extension is ever to hold a subscription — chunked
or otherwise — then the frame contract, the §11 invariant that no feed body enters the browser, and the data split in
`docs/architecture/34-data-split.md` all move together. It was answered by accident for a whole phase because nobody
wrote the question down. It is written down now, in `docs/adr/0021-subscription-servers-travel-in-the-refresh-result.md`,
and the interesting part is how: nothing chose between its three options. Two were refused by data already shipped —
`packages/schema/observability.json#/events/7/payload` is closed at four count fields, so `subscription.updated` cannot
carry a Server, and `packages/schema/protocol/envelope.schema.json#/properties/chunk` names `profile.import` as its only
sender — and the third contradicted an invariant the corpus states as fact. What survived was what
`packages/schema/protocol/commands/subscription.refresh.json#/result/properties/refreshed/items/required` already
demands. The register's first closure by elimination, and the pattern `ODEC-34` was written to describe.

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
| `ODEC-34` | where a chunk slice carries its bytes, and which byte string its `contentHash` names | `contract` | `VER-76` |
| `ODEC-35` | how the sender identity crosses hop 2, and what the local pipe's message schema is | `adr` | `VER-77` |
| `ODEC-36` | which byte string `configHash` covers — the handle-bearing document or the materialized bytes | `contract` | `VER-78`, `VER-88` |
| `ODEC-37` | residual core-routing predicate semantics after measuring the four byte spellings; inversion and protocol-specific matches | `upstream` | `VER-28`, `VER-78` |
| `ODEC-42` | which artefact carries the egress probe's endpoint and the expected public address it must answer — a settings field, a per-server field, or a probe document of its own | `contract` | `VER-84` |
| `ODEC-43` | which code answers a request whose args violate the call contract, for the thirteen of nineteen commands that declare no `BadRequest` | `contract` | `VER-85` |

Each entry in the data also carries the options (§4's argument in three lines each), the reason it is open, and the
sites that deferred to it; the 40 rows above are the same 40 objects, not a summary of them.

One entry is not above, and never will be again. `ODEC-02` — what the two version floors are, and what each refuses
to speak — was answered on 2026-11-06 by its own first option, a field in `packages/schema/protocol/framing.json`,
and it left this file rather than acquiring a `decided` column, for the reason rule 1 forbids answering here: a
status field is a status field you will. Its id is retired in
`packages/schema/open-decisions.json#/retired`, which now exists because an answered entry is cited by the
`verification.json` rows that waited on it, and a register that renumbers to stay dense silently changes what those
citations mean — doc 39 §9 rule 8, adopted for `ODEC-NN` for exactly the reason ADR-0001 rule 2 states it for ADR
numbers. What leaves the register is the question. What stays in the corpus is the line that asked it, and it stops
deferring: `docs/architecture/21-system-context.md` §6 and §12 now state the two floors and the number that answers
them, which is why `citation.counted` moved 61 → 60 in the same commit rather than leaving a site that points at an
entry that is no longer here.

`ODEC-41` (which artefact carries the pipe half of the version negotiation) is `ODEC-02`'s
unfinished half: that question was answered by writing two floors into
`packages/schema/protocol/framing.json#/versionFloors`, and this one is what the corpus forgot to do afterwards. Doc 21
§6 justifies the two-process split with a risk — the extension is replaced on the browser's schedule and the connector
on the user's — and closes it with a promise: the negotiation result *must* be written down in the connector log and
surfaced in `diagnostics.get`. The overlap that sentence describes is the pipe's, the `v` field every frame carries, and
the contract says there are two negotiated windows, refused by one `VersionMismatch`. It is answered on 2026-11-07 by
its own first option: `connector.connected`'s payload gained the required `pipeVersion` beside `protocolVersion` —
the `v` of every frame the connector emits, the `pipe` floor the framing contract declares — and the `versions`
section's `contains` and the export footer name it beside the protocol number, so the recorded handshake fixture
carries the field and a support bundle can tell which side of the pipe refused. The id is retired in
`packages/schema/open-decisions.json#/retired`; what this entry deliberately never asked is whether any record gets
written at all, because nothing in `connector/**` imports a logging package and doc 36 §16's fifth open item already
says so — a decision here still cannot make that true, and the labelled row in doc 36 §14 now reads Verified as data
with the runtime half left where the fifth open item holds it.

`ODEC-42` (where the egress probe's endpoint and its expected answer live) and `ODEC-43` (which code answers a
malformed request to a command that declares no `BadRequest`) are the two newest, and both were found by writing
code rather than by reading the contract — the shape §1 calls the register working. `ODEC-42` is the gap the
HealthProber's constructor is where it bites: `server.test`'s `egressProbe` fetches "the configured egress endpoint"
and `matchesExpectedEgress` compares against an expected address, and no field under `packages/schema/` names
either, so `connector/internal/health` refuses to build without both and the component is tested and unregistrable
until the contract says where they live. `ODEC-43` is the gap every command surface eventually meets: only six of
nineteen command docs declare `BadRequest`, so the thirteen others answer a malformed payload with an analogy —
`connector.hello` answers `HandshakeFailed`, `server.test` answers `HealthCheckFailed` — and both analogies are
recorded at their sites while the rule that would justify either is still this entry's to write. Both are
`contract`: each surviving answer is a field, an array or a rule sentence in `packages/schema`, and neither needs a
browser, a store or a person.

`ODEC-40` (which artefact records the format an `auto` parse resolved to) is answered, and it was the first question
this repository raised by reading its own shipped contract — no code was written, and no code failed. It left this file
on 2026-11-07 by its own first option: `subscription.refresh`'s `refreshed[]` items now carry `format`, the value the
detector resolved, with an enum that is the source schema's minus `auto` — a resolved format is by definition not the
declaration that deferred the choice — and the `#/args` description that used to say the result is not a mutated source
now states the one exception in its own words, while `packages/schema/protocol/data-ownership.json`'s
`nexora-subscriptions` row names the extension as the hand that holds the pen and
`docs/architecture/29-subscriptions-and-servers.md` §5's storage claim is now a fact the contract keeps instead of a
promise nothing kept. The id is retired in `packages/schema/open-decisions.json#/retired`; what leaves the register is
the question, and what stays in the corpus is the answer.

`ODEC-33` (who owns subscriptions) is the third answered id, and the only one whose answer is an ADR rather than a
field: `docs/adr/0021-subscription-servers-travel-in-the-refresh-result.md`, on the same day as `ODEC-40`. Its shape is
what separates it from the other two. `ODEC-02` and `ODEC-40` closed because someone picked the first option and wrote
the field it named; this one closed because the corpus had already refused two of its three options — the chunked
hand-off by two shipped contracts, the raw-body read by an invariant doc 29 §11 states as fact — so the third was not a
choice but a remainder, and the ADR records the exclusions as well as the clause, which is why `ODEC-34`'s own option
list has an "excluded by the data, recorded so the exclusion is not re-litigated" line. Its site left with it: doc 29
§13's deferral row is gone, the deferred-claim label it carried moved onto that ADR's `## Verification` under
`VER-64`, and the counted total of deferred claims is unchanged at 195. The id is retired in
`packages/schema/open-decisions.json#/retired`; the gate it held, `VER-61`, is retired in
`packages/schema/verification.json#/retired` in the same commit, because §9 rule 6 forbids the row outliving the
question that gated it.

`ODEC-39` (what a real `config.apply` reads and where it puts what it writes) is answered, and it was the first
question this repository raised by *finishing* a path rather than designing one. It left this file on 2026-11-07 with
one answer per clause. The intent: the frame carries the documents — `packages/schema/protocol/commands/config.apply.json#/args/properties/intent` is
`required`, the `intent-down` movement's own wording made literal, and an ids-only call is the `BadRequest` its
description states, because the Connector keeps no copy of user intent (ADR-0010) and the seam that pretended
otherwise is gone from the Go half with it. The directories: doc 34 §7 spells the per-user layout —
%LOCALAPPDATA%\NEXORA\ with `config\` and `state\` beside the keystore — and the `compiled-config` row of
`packages/schema/protocol/data-ownership.json` carries it as data. The name: the stable `config.json` that doc 27 §8
renames over won, and the hash-derived wording left `compiled-config.location` in the same commit — a hash-derived
name would have needed the collector `ODEC-26` owns and would have handed Core a new path every apply. The id is
retired in `packages/schema/open-decisions.json#/retired`; what the answer leaves behind is the registration itself,
which still waits on `ODEC-36`'s span, and that residual is `VER-88`, the labelled row in doc 27 §9 this entry's
site used to share with the row that closed.

`ODEC-38` (over what span the attempts are spent) is answered, and it was found the way most of these are — by writing
the thing. It left this file on 2026-11-07 by its own second option: `coreDeath.stabilityWindowSeconds` is in the failure
policy now — default 300, floor 1, ceiling 86 400 — and the rule it states is the episode, not the death: a Core that
survived the window died into a fresh budget, one that died inside it resumes the ladder at the attempt it stopped on,
and a spent episode's next death re-publishes the block without spawning. `connector/internal/supervisor` enforces
exactly that, and the two tests the answer gained measure the crash loop that used to restart forever and the stable
Core that earns its re-arm. The hibernation reading the third option worried about is answered by the same field: a
machine that slept past the window already has a fresh budget waiting. The id is retired in
`packages/schema/open-decisions.json#/retired`; what leaves is the question, and what stays is a denominator the
schema declares and the code reads.

`ODEC-37` began with four missing byte spellings. They now have emitted forms and a fourth golden:
SOCKS5 inbounds, declared direct/block outbounds, inbound sniffing, and network/protocol arrays. Xray 24.11.11
accepts the four materialized fixtures; loopback HTTP and actual SOCKS5 UDP exercise the routing rules with
positive and negative controls. The runtime probe also shows why acceptance is not enough: `invert:true` is
ignored, and an unknown outbound falls back to the default. The compiler reports inverted rules whole and
never emits unknown tags. The question is narrowed, not retired: remaining protocol predicates need their own
matching/nonmatching evidence, and inversion has no proven equivalent lowering. Its sites remain
`docs/architecture/23-unified-server-model.md` §9 and `docs/architecture/27-config-compilation-pipeline.md` §9.
Shape C still needs `ODEC-36`; fail-closed still belongs in `chrome.proxy`, not a separate Core golden.

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
| 8 | An entry MUST NOT be deleted because the question is inconvenient or the phase ended. | Removing the register line while the corpus still defers to it fails the run, and correctly so. An entry that has been *answered* leaves in the same commit as its artefact, with a row in `#/retired` saying what it was and what answered it; its id is never reissued, because `verification.json` rows cite it. |

---

## 9. What this run asserts

| Obligation | Assertion that enforces it |
|---|---|
| the register is derived from the corpus, not a file list | `the register is derived from the whole documentation corpus, not from the files it was written against` |
| the site rule counts what the data declares | `the number of lines that defer to the open-decisions register is the number the register declares` |
| the five excluded lines are still one line each and still would count | `each line the register declares "not a deferral" is exactly one line and really would otherwise count as one` |
| every citing line still defers here | `every entry in the register lands on one line of one document, and that line still defers to it` |
| no deferral is unregistered | `no deferral in the corpus is left without a register entry, so no open question is unregistered work` |
| ids are issued once and every question was born from a site | `every ODEC id the register ever issued is live or retired, no id is recycled, a retired one says what it was and what answered it, and every live question was born from a line that deferred to it` |
| a question is a question, with options and a reason and a decider | `every open question is a question, names who can answer it, offers the choices a reader must weigh, and says why` |
| the who-can-decide vocabulary is closed and defined | `the register owns a closed vocabulary of who-can-decide, each member defined, and a boundary it states itself` |
| gates point at real rows, and decision rows have a written question | `every row a question gates exists, and every row the queue says waits on a decision is gated by a written question` |
| an entry must block something | `no register entry asks a question that blocks nothing, so an open question is work owed and not a worry noted` |
| this document names every entry and every vocabulary word | `the open-decisions document names every registered question and every word of the who-can-decide vocabulary` |
| this document is not a source of deferred claims | `the register document is a list of questions, not a source of deferred verification claims of its own` |
| this document's own tallies are the data's | `doc 40 reads aloud the register tallies the data has, and this check reads them out of the document` |

---

## 10. Invariants

1. 37 questions, 60 sites, one owner per site, at least one site per question. The numbers are outputs of the run,
   not contracts; `packages/schema/open-decisions.json#/citation/counted` states one, and the run compares it -- and
   since this document's §3 table read `contract` 16 against a data of 15 for one commit, the run now reads the
   document's own sentences too, the way doc 39 §11 has done since it caught three stale distributions there.
2. Every question gates at least one row. An entry that blocks nothing is a question the architecture does not need
   answered yet, and belongs in a note rather than the register.
3. One row closes by a decision — `VER-54` — and it is named
   here (`ODEC-10`); five more closed the same way on 2026-11-07 and left both
   lists — `VER-81` on `ODEC-40`, answered by the field its first option asked for; `VER-61` on `ODEC-33`,
   answered by the ADR its remaining option required; and `VER-80` on `ODEC-39`, answered by the frame
   carrying the intent and by the per-user layout that names the two directories and the file.
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
