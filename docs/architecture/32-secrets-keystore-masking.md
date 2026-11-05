# 32 — Secrets, the keystore, and masking

> Purpose: what counts as secret material in this product, how a value gets into the Connector and back out
> again without ever being stored where a value must not be, what "masked" means concretely enough to test, and
> what a keystore failure means for each of the nineteen commands. Fourteen earlier documents cited this one
> for an answer; §2 lists each citation and where its answer is, because a forward reference that no section
> closes is how a contract silently becomes a hope.

Status: Accepted. Normative data:
`packages/schema/envelope-keys.schema.json#/properties/secretReferences`,
`packages/schema/envelope-keys.schema.json#/properties/syncExcludedValueKeys`,
`packages/schema/server.schema.json#/properties/userIdRef`, `packages/schema/server.schema.json#/$defs/headersRef`,
`packages/schema/subscription-source.schema.json#/properties/headersRef`,
`packages/schema/subscription-source.schema.json#/properties/urlRef`,
`packages/schema/protocol/commands/profile.import.json#/args/properties/credentials`,
`packages/schema/protocol/commands/keystore.purge.json#/args/properties/scope`,
`packages/schema/protocol/commands/diagnostics.get.json#/result/properties/masked`,
`packages/schema/protocol/error-taxonomy.json#/codes`.
Evidence base: `docs/audit/11-security-findings.md` (SEC-1, SEC-5, SEC-7 — the measured starting position),
`docs/adr/0010-data-split-extension-intent-connector-truth.md`, `docs/adr/0011-windows-keystore-dpapi.md`.

---

## 1. What this document owns

Four things, and they are the four that every other document in this tree refused to decide:

1. **The boundary of "secret"** — which values must never be written where a reference could be written (§3),
   and which look like secrets but must stay legible (§10).
2. **The vocabulary** — what a `keystore://` string is allowed to say, who mints it, and what it must never be
   computed from (§5).
3. **The two doors** — the single legal path by which a value enters the system (§6) and the single legal path
   by which one leaves it, which is: none (§8).
4. **The failure semantics** — which error code means which remediation, per command (§11, §12).

Not owned here, by design: who may call the Connector and how often (`docs/architecture/31-security-model.md`);
what happens to the proxy decision when a component dies (`docs/architecture/30-failure-policy.md`); which
storage key holds which byte (`docs/architecture/34-data-split.md` §5–§7); migration ordering (§15 states the secret
consequences and hands the ordering to `docs/architecture/35-schema-versioning-migration.md` §11); the log format
and event catalogue (`docs/architecture/36-observability.md`); the wording a user sees
(`docs/architecture/37-ui-architecture.md` §4); and whether a control has actually been executed —
`docs/verification/live-matrix.md`, cited row by row below.

One reading rule, inherited from `docs/architecture/20-target-repository-layout.md` §1 and sharpened here: **a
secret-control claim with no check behind it is labelled `Verified: NO — Phase 2` at the point of use.** In this
document that is almost every row, because there is no keystore code yet. That is the honest state, not an
oversight, and §16 keeps the count visible instead of hiding it.

## 2. The fourteen questions this document was cited to answer

Each row is a real forward reference found by grepping the tree for this file's name.

| Cited by | What it deferred | Answered in |
|---|---|---|
| `docs/architecture/22-component-contracts.md:111` | "never returns an unmasked secret in any frame, including `diagnostics.get`" | §8, §9 |
| `docs/architecture/22-component-contracts.md:311` | a probe must not reveal a secret in result or logs | §8 |
| `docs/architecture/22-component-contracts.md:388` | masking happens at the sink, not the call site | §8 |
| `docs/architecture/23-unified-server-model.md:217` | field-level rules for `publicKey`/`shortId` | §10 |
| `docs/architecture/24-options-envelope-compat.md:81` | a reference arriving without its secret degrades visibly | §14 |
| `docs/architecture/25-native-messaging-protocol.md:184` | `SecretRedacted` vs a corrupt document | §11 |
| `docs/architecture/26-command-surface.md:162` | `maskedConfig` must contain no keystore value | §13 |
| `docs/architecture/27-config-compilation-pipeline.md:116` | mask and label rules for unrepresentable fields | §9, §10 |
| `docs/architecture/29-subscriptions-and-servers.md:278` | `detail` masked like any other text | §8 |
| `docs/architecture/29-subscriptions-and-servers.md:319` | M3–M5: credentials inside a subscription | §6, §14 |
| `docs/architecture/29-subscriptions-and-servers.md:375` | the write primitive "that does not exist yet" | §6 — it exists now |
| `docs/architecture/30-failure-policy.md:135` | a Core holding credentials with no controller | §7, §12 |
| `docs/architecture/30-failure-policy.md:370` | what a keystore failure means for a command | §12 |
| `docs/architecture/31-security-model.md:281` | the DPAPI residual and the full threat model | §16 here; the residual stays owned by `docs/architecture/31-security-model.md` §14 |
| `docs/adr/0011-windows-keystore-dpapi.md:54` | "the full threat model belongs to" this document | §16 |

## 3. What counts as secret material

The test is not "looks sensitive". It is: **can a party who recovers this value act as the user, read the user's
feed, or write the user's sync backend?** Four field classes pass that test, and they are exactly the four in
`packages/schema/envelope-keys.schema.json#/properties/secretReferences/properties/fields`:

| Field | Value | Why it is secret | Entropy class | Visible chars |
|---|---|---|---|---|
| `user-id` | a VLESS UUID | anyone holding it connects through that server as that user | high | 8 |
| `headers` | a header map | carries `Authorization`/`Cookie`/API keys for a transport or a feed | high | 0 |
| `url` | a fetch address with a credential in query or path | the tail *is* the token for feeds that ship `?token=…` | high | 0 |
| `token` | one human- or service-typed credential | a sync-backend token (Gist, WebDAV — SEC-5) or an upstream proxy passphrase | low | 0 |

Two consequences worth stating because they are load-bearing:

- **A reference is not a secret.** `userIdRef`, `headersRef`, `urlRef` are `required` in their own schemas
  (`packages/schema/server.schema.json#/required`) and they are what makes an envelope *useful* after sync.
  They must travel. They are inert on another machine, which is why they buy no confidentiality by being
  stripped — the reasoning that produced the first bound of
  `packages/schema/envelope-keys.schema.json#/properties/syncExcludedValueKeys`.
- **"Secret" is a property of a value, not of a field name.** §8 builds the redaction table from the values the
  keystore holds, precisely so that a credential appearing under an unexpected name — inside a `detail` string,
  inside a Core log line, inside an error message a VPS returned — is still destroyed.

Everything else is configuration, and configuration is meant to be readable: §10 lists the fields that are
deliberately *not* masked and why masking them would cost the user a diagnosis.

## 4. Where a secret can be, and what holds it there

| Position | Holder | Form | Control | Check |
|---|---|---|---|---|
| In memory | Connector process | the value, resolved per operation | only the Connector ever resolves a reference (`docs/adr/0011-windows-keystore-dpapi.md:30-35`) | `LIVE-18` `Verified: NO — Phase 2` |
| At rest, per secret | one DPAPI blob per secret, `%LOCALAPPDATA%\NEXORA\keystore\` | ciphertext bound to the user | ADR-0011 rule 1; `keystore://host/<id>/token` for the mint-time key | `LIVE-18` |
| At rest, compiled | the Xray config file on disk | **plaintext values** — §13 | owner-only ACL, bounded lifetime, never exported | `packages/schema/envelope-keys.schema.json#/properties/secretReferences/properties/atRest`, asserted by 'the plaintext exception is declared, not implied' |
| In transit | the Native Messaging pipe | references, plus one command's `credentials[]` | §6, §14 | 'a chunked import may not carry a credential' |
| In the envelope | `chrome.storage.local` / `.sync` | references only | `secretBearing ⇒ ¬syncEligible` (`packages/schema/envelope-keys.schema.json#/properties/namespaces/items/properties/secretBearing`) | the strip-set assertions |
| In logs, diagnostics, exports | the Connector's ring buffer, `diagnostics.get`, a user-chosen file | masked text | §8, §9 | `LIVE-10` |
| In the idempotency cache | Connector memory/window | the *response* only | ADR-0008 stores responses, never requests | §6 |

The row that most documents get wrong is the third: the keystore design does **not** mean "no plaintext secret is
ever written to disk". It cannot, while Xray is the proxy engine (ADR-0003). §13 treats that as a named,
bounded exception with its own controls rather than as a detail to glide over.

## 5. The reference vocabulary, and why it is data

Before this iteration the tree contained, in prose and in fixtures, the segments `user`, `userId`, `user-id`,
`authorization`, `ws-headers` and `headers` — six spellings of four ideas, in five files, with no rule about
which was canonical. That is not a style problem: a masking policy that five schemas and two clients each spell
their own way is five policies, and the one that is looser is the one that leaks.

So the vocabulary is one object in the envelope-key registry
(`packages/schema/envelope-keys.schema.json#/properties/secretReferences`), and `scripts/verify.mjs` reads it:

- **The floor** — `#/properties/secretReferences/properties/pattern`, the type-level regex verbatim from
  ADR-0011 rule 2. Assertion: *every keystore-typed property in the package uses the one reference floor from
  secretReferences*. It failed the first time it ran, because two schemas had written their own looser copies.
- **The shape** — `#/properties/secretReferences/properties/shape`: `<entity>/<identifier>/<field>`, with both
  lists closed (`properties/entities`, `properties/fields`). Assertion: *every recorded reference follows the
  entity/id/field shape* — over every example and every recorded protocol frame in the package.
- **Minting** — `#/properties/secretReferences/properties/mintedBy`: the Connector, with a random identifier.
  Nothing in the extension, the envelope, or a feed composes a reference.
- **Never derived** — `#/properties/secretReferences/properties/neverDerivedFrom`. A content-addressed reference
  is the worst possible design: the reference syncs, exports and logs freely while the value does not, so every
  envelope becomes an offline dictionary — anyone who guesses the value checks the guess by comparing strings.
  The shape's identifier is 8–63 characters, which cannot hold a SHA-256 hex digest; assertion *no reference is
  content-addressed* enforces that structurally, not as a comment.

The id segment admits lowercase hex **with dashes**, which is a decision, not a looseness: a Server's identity is
a UUID (`packages/schema/server.schema.json#/properties/id`), and `profile.import`'s `entityId` accepts a UUID or
a shorter hex prefix of it. The check found the mismatch — fixtures carried dashed UUIDs while the shape claimed
hex-only — which is exactly what a machine check is for.

## 6. How a value gets in

Everything in the tree specified how a secret is stored, resolved, masked and deleted. Nothing specified how a
value arrives. The gap was not cosmetic: a user typing a VLESS UUID, and a legacy ZeroOmega backup holding
`auth` and `headers` as values
(`omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:38`), both need a legal door, and without
one the first implementation would invent one.

**Decision: the door is `profile.import`'s `credentials` array**
(`packages/schema/protocol/commands/profile.import.json#/args/properties/credentials`) — not a twentieth command.

Three reasons, in order of weight:

1. **The contract already promised it.** `profile.import` declares the `keystore-write` side effect and reports
   `result.secretsExtracted` (`packages/schema/protocol/commands/profile.import.json#/result/properties/secretsExtracted`). A promise with no
   path to satisfy it is a bug waiting for someone to notice the count is always zero.
2. **Ingest is never standalone.** A typed credential arrives *with* the profile that uses it; a backup's
   credentials arrive *with* the profiles they belong to. Splitting them into a separate command creates a
   state where the intent exists but the secret does not — and a partial import is precisely the failure
   ADR-0008's idempotency record is supposed to make impossible.
3. **One door is checkable.** The assertion *exactly one field in the whole command surface may receive a secret
   value* walks every command's `args` for a `writeOnly` declaration and fails if a second one appears. A new
   ingest path is now a schema change that has to defeat that check.

The rules attached to that one door:

- `value` is declared `writeOnly: true` (assertion *the ingest value is declared writeOnly*) and the closed
  entity/field enums are checked against the canonical lists — *the ingest entity enum is the canonical entity
  set*, *the ingest field enum is the canonical field vocabulary*.
- It is bounded: `maxItems` 512, a 4096-character string or a 32-key map, and every entry must name an owning
  entity present in the same payload. **An orphaned credential is a refusal, not a stash** — a store entry
  nothing references is an entry nothing will ever delete through the normal path.
- **It is forbidden on a chunked frame.** Reassembly lives in the extension's import-staging buffer
  (`packages/schema/import-staging.schema.json`), which is `chrome.storage.local` — and DPAPI does not reach
  there. A credential that arrived in slices would sit in plaintext in browser storage while it reassembled,
  which is SEC-1 in a new costume. Assertions: *a single-frame import may carry a credential*, *a chunked import
  may not carry a credential*, and *the credential-carrying slice is rejected by the combination rule, not by a
  typo* — the last one pins the rejection to the `not` keyword, so the ban cannot silently degrade into an
  unrelated validation failure.
- Re-sending the same entity and field with a new value overwrites. That is rotation, and it needs no separate
  command either.
- The value appears in nothing that leaves: not the result (only the count), not a log line (§8), not the
  idempotency record. ADR-0008 caches responses, and this is why that asymmetry is a security property.
- No `confirm://` token is required. Writing a credential is correctable by writing it again; deleting one is
  not, which is why every `keystore.purge` scope demands the token
  (`packages/schema/protocol/commands/keystore.purge.json#/args/properties/confirmToken`). An asymmetry in
  ceremony is a sign the two operations were actually thought about.

`subscription.refresh` also declares `keystore-write`, and no value crosses the pipe for it: it fetches and
parses inside the Connector (`docs/architecture/29-subscriptions-and-servers.md` §2), so the secret material is
born on the correct side of the boundary. The command's keystore *deletion* half (§7) is what closes M3–M5.

## 7. Rotation, deletion, orphans, and what survives

`keystore.purge` is the only destruction path (`packages/schema/protocol/commands/keystore.purge.json`) and its
scopes are the entity classes plus two: `orphaned` and `all`.

- **`still-referenced` beats the request.** A purge that would strand an applied config is refused per entry and
  reported in `result.retained` with `referencedBy`
  (`packages/schema/protocol/commands/keystore.purge.json#/result/properties/retained/items/properties/reason`). The alternative — honouring the
  delete — produces a config referencing a value that no longer exists, which at next compile becomes either a
  broken tunnel or, worse, an empty credential silently substituted. `docs/architecture/30-failure-policy.md`
  forbids the second; this refuses the first.
- **`orphaned` is the UI-safe scope.** A button labelled "clean up unused credentials" can offer it without
  sounding destructive, and it is the one a subscription re-fetch implicitly needs: feeds replace servers
  constantly, and `subscription.refresh` carries `keystore-delete` precisely so the replaced servers'
  credentials do not accumulate.
- **Secrets outlive extension uninstall** — deliberate, per
  `docs/adr/0011-windows-keystore-dpapi.md:59-60`. They are the user's credentials, not the extension's
  scratch state; scope `all` is the documented destruction path. This is also the answer to
  `docs/architecture/30-failure-policy.md:135`: a Core holding credentials with no controller is forbidden, but
  a *store* holding credentials with no installed extension is the intended state, because the user's server
  list may well come back.
- **The `host` entity** holds installation-scoped material the Connector mints for itself — the fingerprint key
  (§9). It is purgeable only under `all`, and rotating it alone would silently break fingerprint comparison
  across an installation's history. That is acceptable because fingerprints are scoped to one installation by
  design; it is stated here because a future "rotate the key" feature would otherwise look free.

## 8. Masking happens at the sink, and it is keyed by value

Two designs were available.

**Name-keyed (what legacy does).** A redaction table of field names applied by a JSON `replacer`:
`omega-target-chromium-extension/overlay/log.js:45-58` masks `username`, `password`, `host`, `port`, `token`,
`gistToken`, `gistId`. Measured defects, all three present in that one switch:

1. It masks `host` and `port` — which are not secrets (§10) — so an operator reading a legacy log cannot tell
   which server failed, and the mask *costs* diagnosis to buy nothing.
2. It omits `headers` and `auth`: the two fields SEC-1 is actually about
   (`omega-target/src/options.coffee:27` — the strip list `['lastUpdate','ruleList','pacScript','sha256']`,
   applied one field at a time at `:96`). The highest-value credential in the product has no entry in the table.
3. It never applies to a bare string: `omega-target-chromium-extension/overlay/log.js:64-65` passes `typeof obj
   == 'string'` straight through, and `:67` applies the replacer only to objects. A token interpolated into a
   message is unmasked, and every interesting secret in a log is in a message.

**Value-keyed, at the sink (the NEXORA rule).** Each sink renders a record, then replaces any text equal to a
value the keystore currently holds — and any value inside a map, and any value inside a quoted string within the
text — with the declared mask. The table is built from the store, so it cannot go stale the way a name list
does, and it needs no cooperation from the code producing the text.

- **At the sink, not the call site**, because a call-site policy has one failure mode and it is the interesting
  one: a new `log.debug` written by someone who forgot the helper leaks. A sink cannot be forgotten — there are
  a fixed number of them; `packages/schema/observability.json#/sinks` closes the list. This is the mechanism
  `docs/architecture/22-component-contracts.md:388` promised.
- **The sinks**: the log ring buffer, `diagnostics.get`'s payload
  (`packages/schema/protocol/commands/diagnostics.get.json#/result/properties/masked`), a structured error's
  `detail` (`docs/architecture/29-subscriptions-and-servers.md:278`), `config.snapshot`'s `maskedConfig` (§13),
  an exported file, and a UI text node. The Connector owns the first five — it is the only process that has the
  values, so it is the only process that *can* build the table. The extension cannot mask what it never received,
  which is a second reason ADR-0010's split is a security decision and not only a cleanliness one.
- **Record-unit, not line-unit**: a Core log line is a string, and a string has no field names to key on. This is
  why §8's table is built from values.
- **A value the store does not hold is a refusal, not a strip.** If a document contains credential-shaped
  material that the keystore cannot confirm — a legacy import that failed mid-write, a hand-edited envelope —
  the Connector answers `SecretRedacted` rather than silently replacing it. ADR-0011 rule 6 says the same thing
  from the other direction: a `SecretRedacted`-shaped payload is refused at a boundary, not accepted and
  laundered. The reasoning is in §11.

`packages/schema/envelope-keys.schema.json#/properties/secretReferences/properties/display` carries `appliesAt:
"sink"` so the location is data, not prose. `Verified: NO — Phase 2` for the redaction pass itself; the data
half is green (`LIVE-10` asserts the greppable property against a fixture, `LIVE-18` covers the rest).

## 9. What a mask looks like

Four rules, all of them in `secretReferences`, all of them asserted:

1. **Not length-preserving** (`properties/display/properties/lengthPreserving`, assertion *the mask does not
   preserve the length of what it hides*). A mask that keeps the length publishes a fact about the secret — and
   for the `token` class, which is assumed low-entropy
   (`packages/schema/envelope-keys.schema.json#/properties/secretReferences/properties/fields`), narrowing the
   length is a material part of a brute force. `•`-run masks in other products are an aesthetic choice with a
   cost nobody measured.
2. **At most one class may show characters**, and no more than eight of them (assertions *exactly one credential
   class is allowed a partial display, so widening it is a decision*, *a low-entropy credential shows no
   character of itself*, and per-field `maxVisibleChars`). `user-id` gets 8 head-then-tail: a VLESS UUID is
   high-entropy, eight characters of it narrow nothing, and support needs *some* handle to say "the server you
   mean". `headers`, `url` and `token` show zero — for `url` specifically, a head-then-tail rule would be
   actively wrong, because the tail of a token-carrying feed address *is* the credential; the UI shows `url`'s
   display sibling instead (the query-stripped address,
   `packages/schema/subscription-source.schema.json#/properties/url`).
3. **One sentinel literal** (`properties/display/properties/maskLiteral`), asserted to actually appear in a
   recorded fixture (*a recorded fixture actually uses the declared mask literal*). Without that third check,
   LIVE-10 would grep for a string no implementation emits — a self-consistent contract that tests nothing.
4. **Fingerprints are keyed digests**: `^fp-[0-9a-f]{12}$`
   (`packages/schema/envelope-keys.schema.json#/properties/secretReferences/properties/fingerprint`), a
   truncated HMAC-SHA-256, never a bare hash (assertions *the comparison digest is keyed, never a bare hash of
   the value*, and *diagnostics.get fingerprints and secretReferences declare the same fingerprint shape*). The
   argument is §5's again: an unkeyed digest of a low-entropy token is searchable by anyone who can guess
   candidates, and diagnostics files are shared. The HMAC key is itself a keystore entry under the `host` entity
   (*the fingerprint key is itself a reference of the documented shape*), so it is not in the binary and not in
   the envelope. 48 bits is far more than "are these two values the same?" needs and far less than what would be
   needed to invert anything. Comparability is scoped to one installation — across machines the key differs, so
   the fingerprints do not match, and that is the correct behaviour rather than a leak.

What a fingerprint buys is in the command's own words: support can say "the UUID you sent is not the one on
file" without either value appearing.

## 10. What is deliberately not masked

The other half of the policy, and the half that protects the user from the engineer.

| Field | Class | Why it stays legible |
|---|---|---|
| `publicKey`, `shortId` | REALITY parameters | public by construction; masking makes a REALITY handshake undiagnosable — the case `docs/architecture/23-unified-server-model.md:215-217` names |
| `address`, `port` | server location | the user must be able to say which server failed; they identify a VPS, they do not authorise anything |
| `serverName`, `alpn`, TLS `fingerprint` | transport parameters | SNI is on the wire in clear anyway; pinning a CA-style fingerprint is integrity, not confidentiality |
| entity ids, tags, `displayName` | identity | they are how a UI refers to a thing; the identifier inside a reference is *derived from* the value's owner, never from the value (§5) |
| header **names** | `headers` map keys | names are displayable, values are not — so masking survives the keys and destroys the values |
| subscription content `sha256` | integrity | change detection; SEC-7 shows legacy already treats it as non-secret |
| `url` (display form) | feed address | the query-dropped address is what the user recognises; the credential-bearing whole address lives at `urlRef` |

This table is where `docs/architecture/27-config-compilation-pipeline.md:116` and
`docs/architecture/31-security-model.md`'s masking hand-off land: an unrepresentable field is reported by name
with its value intact, because the field that cannot be compiled is configuration, not a credential.

The `url`/`urlRef` pair is the pattern to copy whenever a value looks like it needs to be both secret and
visible: split it into two fields, and let the schema be the thing that keeps them from being confused. A single
field cannot be "shown without the token" and "fetched with the token" at the same time.

## 11. Which refusal is which

Three codes cover "a secret was involved", and they mean three different things to three different readers.
This is the answer to `docs/architecture/25-native-messaging-protocol.md:183-184`.

| Code | Meaning | Whose problem | What they do | Retryable? |
|---|---|---|---|---|
| `SecretRedacted` | a *value* sat where a reference belongs, in a place that syncs, logs or applies | the producer of that document | re-import through a path that extracts credentials; never "edit and retry" | no |
| `InvalidProfile` | the document is well-formed but names a reference the store does not have | the user, on this machine | re-enter the credential for the named field | no |
| `KeystoreUnavailable` | the store itself is unreachable (DPAPI failed, wrong session, blob directory gone) | nobody who types | fix the session/store; nothing typed helps | no |

- **`SecretRedacted` is never a stripped-and-continued.** Stripping is what makes a leak look like success; the
  taxonomy's meaning text says the call was refused instead. It is also the code that distinguishes "your
  document is corrupt" from "your document is *dangerous*", which is the difference between a repair and a
  warning.
- **A dangling reference is `InvalidProfile`, not `KeystoreUnavailable`.** The distinction matters operationally:
  one sends the user to retype, the other tells them their machine is broken. This is a decision taken here, and
  it needs no schema change because `config.apply` — the command that resolves references at compile time —
  already declares all three (assertion *the compiling command names every way a secret can be the reason
  nothing applied*).
- **`AuthFailed` implies the store was readable.** A credential was present, resolved, and rejected upstream.
  `server.test` therefore names `KeystoreUnavailable` alongside `AuthFailed`
  (`packages/schema/protocol/commands/server.test.json#/errors`), asserted by *a command that can be refused by a
  credential can be stopped by the store holding it* — without it, a client could read "auth failed" as a
  credential problem when the value never arrived.

## 12. Per-command keystore availability

`docs/architecture/30-failure-policy.md:369-370` asked this document for exactly one thing: what a keystore
failure means for each command. Nineteen commands, four groups.

| Group | Commands | Touches the store | On failure |
|---|---|---|---|
| ingest | `profile.import`, `subscription.refresh` | write (+ delete for refresh) | the whole call fails; **no partial intent** — the profiles and their credentials apply together or not at all |
| resolve/compile | `config.apply`, `core.start`, `core.restart`, `server.test`, `connector.health` | read | `KeystoreUnavailable`; nothing applied, nothing started; ADR-0011 forbids a plaintext fallback |
| destroy | `keystore.purge`, `profile.remove`, `subscription.refresh` | delete | refused, with entries reported `retained` rather than half-deleted (§7) |
| report | `diagnostics.get`, `config.snapshot` | read (masked) | the section degrades to `state: "unavailable"` rather than the call returning secrets or failing the whole dump |
| neither | `connector.hello`, `connector.status`, `connector.version`, `connector.events.subscribe`, `core.status`, `core.stop`, `profile.validate` | not at all | unaffected by the store |

Two decisions fall out of the table:

- **First-run ordering is a security property.** Keystore writes precede `config.apply`
  (`docs/adr/0011-windows-keystore-dpapi.md:69-70`), so a missing secret surfaces as `KeystoreUnavailable`
  before Core starts, and never as a Core running with an empty credential. This is the ordering §6's
  ingest-with-the-profile design exists to make natural.
- **`profile.validate` is deliberately store-free.** It checks shapes and combinations, so the options UI can
  still answer questions while the keystore is down. The cost is stated rather than hidden: `Validated` does
  not mean `applicable`, and only `config.apply` can promise that. A credential-bearing reference with a
  well-formed prefix validates; the resolution failure arrives at apply time as `InvalidProfile` (§11).

Read as a coverage rule: every command that writes the store names both `KeystoreUnavailable` and
`SecretRedacted`, and every command that deletes from it names `KeystoreUnavailable`
(`docs/architecture/31-security-model.md` §17 records the amendment). `Verified: NO — Phase 2` for every row of
behaviour in this table; the *declarations* are green.

## 13. The one place a value is plaintext: the compiled config

Xray reads its own config format and cannot be handed `keystore://` references (ADR-0003). So the file the
Connector writes for Core contains real UUIDs and real header values, on disk, in clear, and any document that
implied otherwise would be false. The registry says so in data —
`compiledConfigContainsValues: true` in
`packages/schema/envelope-keys.schema.json#/properties/secretReferences/properties/atRest`, with its own
assertion (*the plaintext exception is declared, not implied*) so a future editor cannot quietly turn it off to
make a sentence in a doc true.

The controls that make the exception bounded:

| Property | Requirement | Data |
|---|---|---|
| Owner | the file inherits the user-profile ACL only, no `Program Files`, no elevation | *the compiled config is owner-only on disk* |
| Lifetime | from the rename that installs it until `core.stop` or Connector shutdown; always re-derivable from intent + keystore, so never backed up | `properties/atRest/properties/compiledConfigLifetime` |
| Portability | never an export artefact; `maskedConfig` is the only version that leaves | *the compiled config never leaves as an artefact* |
| Verifiability | grepping `maskedConfig` for any pinned secret value finds nothing | `packages/schema/protocol/commands/config.snapshot.json#/invariants`, `LIVE-10` |

Who the exception costs: an adversary with the user's own privileges. That adversary can already call
`CryptUnprotectData` (`docs/adr/0011-windows-keystore-dpapi.md:49-54`), so a live config file introduces no new
adversary class — it is the same residual, written where it is easier to find. What it does add is a *timing*
risk: an abrupt kill or a power loss can leave the file past its lifetime, and cleanup is a shutdown step, not a
guarantee. `docs/architecture/33-binary-integrity.md` and `docs/architecture/30-failure-policy.md` own the
process-death half; §17 lists "orphaned plaintext config after an unclean kill" as a live residual rather than a
solved item.

## 14. In transit, in sync, on another machine

**The pipe.** Native Messaging carries references everywhere and values in exactly one field of one command
(§6), and that field is barred from chunked frames for the storage reason given there. MV3's message-size bound is
why `profile.import` is chunkable at all (`docs/architecture/25-native-messaging-protocol.md`), which makes the
credential ban a real constraint rather than an inconvenience: the natural implementation would have been to
chunk everything.

**Sync, and the honest scope of the strip list.**
`packages/schema/envelope-keys.schema.json#/properties/syncExcludedValueKeys` now carries
`lastUpdate`, `ruleList`, `pacScript`, `sha256`, `headers`, `auth`, `body`, `gistToken`, `token`. Against the
legacy four (`omega-target/src/options.coffee:27`) this closes the value-shaped leaks a migrated envelope could
still carry:

- **SEC-1 is two fields wide.** `headers` — the subscription header map — and `auth`, the per-proxy map read at
  `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:38-40` (`profile.auth[scheme.prop]`), which
  the audit named once and the data must name twice. Both are asserted per key by *the sync strip set covers the
  legacy credential field "headers"* and its sibling for `"auth"`.
- **The strip list is not the durable control.** References are. The strip list only matters for documents that
  still have legacy-shaped values; a NEXORA Server cannot hold an inline header map at all
  (`packages/schema/server.schema.json#/$defs/headersRef`). Do not read the list as the mitigation for SEC-1 —
  read it as the compatibility floor while legacy documents are still in the wild.
- **SEC-5 is NOT closed by the strip list, and this document says so instead of implying it.** The built-in sync
  config — `gistId`, `gistToken`, `syncUsername` — is written whole into `chrome.storage.sync` by
  `omega-target/src/options_sync.coffee:240-242` via `builtInSyncStorage`, a path that never passes through
  `transformValueForSync` (`omega-target/src/options.coffee:89-98`), so no entry in any strip list can reach it.
  Adding `gistToken` to the list is necessary and not sufficient; the sufficient half is a migration decision
  about that storage key, and it goes to `docs/architecture/40-open-decisions.md` (§17).

**The other machine.** A synced or restored envelope arrives with references that resolve to nothing, because a
reference is inert off the machine that minted it (ADR-0011 rule 3). The required behaviour, answering
`docs/architecture/24-options-envelope-compat.md:78-81`:

1. the profile is kept and shown, not discarded — it is the user's intent, and intent is what sync exists to
   carry;
2. it is marked as needing a credential, and the attempt to apply it answers `InvalidProfile` naming the
   reference (§11) — a reference is safe to display (§10, §5), which is what makes this diagnosable;
3. nothing degrades *silently*: an unresolvable server must not be skipped in a way that routes traffic
   directly, per ADR-0009.

The one-way property belongs here rather than in §15: extracting a credential into the keystore removes it from
the envelope, so restoring a *pre-extraction* backup on a post-extraction installation is the one downgrade that
loses data. Stated plainly, with the ordering owned by `docs/architecture/35-schema-versioning-migration.md` §11.

## 15. Legacy position, and migration's secret consequences

What legacy holds today, and what NEXORA does with each:

| Legacy position | Evidence | NEXORA outcome |
|---|---|---|
| inline `headers` on a subscription profile | `omega-target/src/options.coffee:94-98`, chain in SEC-1 | value → `credentials[]` ingest (§6), envelope keeps `headersRef` |
| per-proxy `auth` map | `omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:38-40` | same; `token`/`headers` field classes |
| `gistToken` in extension storage | `omega-target/src/options.coffee:1187` | `sync`-entity keystore entry; **not** closed for the built-in-sync write (§14) |
| credential in a feed URL's query | `packages/schema/subscription-source.schema.json#/properties/urlRef` | `url` (display) + `urlRef` (value) split, chosen per source, not sniffed |
| a credential-bearing URL accepted in `sourceUrl` | `omega-target-chromium-extension/src/module/fetch_url.coffee:36-40` path has no validation at all | `^https://`, no userinfo, no query shown (§10) |
| name-keyed log masking | `omega-target-chromium-extension/overlay/log.js:45-58` | replaced by value-keyed sink redaction (§8) |

Migration MUST NOT lose a working configuration (ADR-0013/0014/0015), and for secrets that means a specific
sequence: read the legacy document, hand the values and the intent to `profile.import` **in one frame** (§6),
and only then write the reference-shaped envelope. Splitting those steps is how a user ends up with a profile
pointing at a credential that was never written — which is a `still-referenced`-shaped problem in reverse.

The pre-migration snapshot (`packages/schema/envelope-keys.schema.json#/properties/backupKeyPattern`) is the
part of this document that cannot be made elegant: a snapshot of a legacy envelope contains legacy plaintext
values, by definition, and it must, because a snapshot you cannot restore is theatre. So the snapshot is
owner-only, it is written before any destructive step, and it is deleted when the migration is confirmed — the
same shape of control as §13's compiled config, and the same honest limitation: for the duration of a migration
this product has plaintext on disk in two places, and §16's threat model is written with that in mind rather
than around it.

## 16. Threat, control, check

| Threat | Position | Control | Check |
|---|---|---|---|
| Reader of a sync backend (Gist/WebDAV) | third party with the endpoint's read access | references only; values never sync; strip set covers legacy-shaped values | *the sync strip set covers the legacy credential field "…"*, `secretBearing ⇒ ¬syncEligible`; **SEC-5's built-in-sync path: `docs/architecture/40-open-decisions.md`** |
| Recipient of an exported options file | whoever the user hands the file to | no value in any export; `compiledConfigInExports: false` | *the compiled config never leaves as an artefact*, `LIVE-10` |
| Support reading a log/diagnostics bundle | a named human, by design | value-keyed sink redaction + `fp-` fingerprints (§8, §9) | *a recorded fixture actually uses the declared mask literal*, `LIVE-10` |
| Another machine holding the envelope | a stolen account/roaming profile | references inert there; visible degradation, `InvalidProfile` | `LIVE-19` `Verified: NO — Phase 2` |
| Guessing a token from a synced string | offline dictionary | references are minted, never derived; digests barred; fingerprints keyed | *no reference is content-addressed*, *the comparison digest is keyed, never a bare hash of the value* |
| A second extension reading options | SEC-4's position | no unauthenticated options dump; the external surface is not carried forward | `LIVE-16`, `docs/architecture/31-security-model.md` §6.5 |
| Same-user malware | any process as the user | none that this document can offer — DPAPI is per-user (ADR-0011 rule 7), and the config is plaintext (§13) | **no control; stated as residual** |
| A forgotten call site | new logging code written later | masking is at the sink, so a call site cannot be forgotten | *exactly one field in the whole command surface may receive a secret value* (the ingest door stays one) |
| A credential that outlives its entity | accumulated store entries | orphan refusal at ingest (§6); `orphaned` scope; `keystore-delete` on refresh (§7) | *a command that deletes secrets names the store failure*, `LIVE-18` |
| An unclean kill leaving the config | disk, after the fact | bounded lifetime, re-derivable, never backed up | §17 residual; `LIVE-18` |

## 17. Invariants, amendments, open items

**Invariants.**

1. A `keystore://` reference is the only form a credential field may take in an intent document; the floor is
   one string in one registry.
2. A reference is minted from randomness and never derived from the value, a digest of it, or a public attribute
   of its owner.
3. Exactly one field in the command surface may receive a value; it cannot appear in a chunked frame, cannot be
   echoed by any result, and is not stored in the idempotency record.
4. Masking is value-keyed and applied at a named sink; no call site is trusted, and no sink is trusted to be
   complete without a check.
5. A mask shows nothing about length; at most one credential class shows any character at all.
6. A comparison digest is keyed, and the key is itself in the store.
7. Purging a still-referenced secret is refused; a keystore failure stops a write, a compile, or a start rather
   than substituting an empty credential.
8. The compiled config contains values, is owner-only, is bounded in lifetime, and never leaves. That is a
   declared exception, not a contradiction of rule 1.

**Amendments made by this document** (all green in `npm --prefix packages/schema run verify`, 900 checks):

1. `packages/schema/envelope-keys.schema.json` gained `#/properties/secretReferences` — the whole vocabulary of
   §5, §9 and §13 — and `syncExcludedValueKeys` gained `auth`, `gistToken` and `token` alongside `headers` (§14).
2. `packages/schema/protocol/commands/profile.import.json` gained
   `#/args/properties/credentials` (§6), the `chunk`-exclusion clause, and a rewritten `secretsExtracted` that
   names its own ingest path instead of hedging.
3. `server.test`, `subscription.refresh` gained the error codes §11 and §12 require; the coverage rules are the
   assertions in §12.
4. `scripts/verify.mjs` gained the section that reads the vocabulary: the floor, the shape, the entity/field
   enums, the digest ban, the entropy/display rules, the plaintext honesty fields, the one-door rule, and the
   error-coverage loops.
5. Every fixture reference literal was normalised to the shape — three spellings of the same header class, three
   of the same user class — and `config-apply.jsonl`'s `maskedConfig` clients, which previously carried a bogus
   `keystore://` value *inside a compiled config*, now carry the mask literal. That fixture was the bug §13's
   honesty field exists to prevent.
6. `docs/verification/live-matrix.md` gained `LIVE-18` (at rest: keystore round-trip, purge and orphan
   behaviour, owner-only compiled config) and `LIVE-19` (a synced or restored envelope with a dangling reference
   degrades visibly, never silently direct), and the rows in `docs/architecture/22`, `23`, `24`, `26` and `29`
   that pointed at the wrong row now point at these.
7. `docs/adr/0011-windows-keystore-dpapi.md:68`'s requirement that the strip set contain `headers` is now
   satisfied in data, together with the sibling fields §14 names.

**Open, and deliberately not decided here** (each goes to `docs/architecture/40-open-decisions.md`):

- the built-in-sync `gistToken` write, which bypasses every strip rule (§14) — the one known-open half of SEC-5;
- per-profile inbound credentials for loopback inbounds, which would add a fifth field class and a fifth mask
  rule (`docs/architecture/31-security-model.md` §12);
- legacy authenticated HTTP/SOCKS proxies imported from a backup: NEXORA's model has no home for them, so §15's
  ingest must report them rather than stash them, and what "report" means is a UI decision for
  `docs/architecture/37-ui-architecture.md` §4 and §5;
- whether the pre-migration snapshot should be encrypted at rest (ADR-0011 forbids self-written crypto; DPAPI on
  a file the user might move is a different question from DPAPI on a store blob);
- rotation of the `host`-entity fingerprint key, and what an installation's diagnostic history means after it.

Trace: ADR-0002, ADR-0003, ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0011, ADR-0013, ADR-0014,
ADR-0015, ADR-0019 · Contract: `packages/schema/envelope-keys.schema.json`,
`packages/schema/server.schema.json`, `packages/schema/subscription-source.schema.json`,
`packages/schema/protocol/commands/profile.import.json`, `packages/schema/protocol/commands/keystore.purge.json`,
`packages/schema/protocol/commands/diagnostics.get.json`, `packages/schema/protocol/commands/config.snapshot.json`,
`packages/schema/protocol/error-taxonomy.json`, `packages/schema/import-staging.schema.json` · See also:
`docs/audit/11-security-findings.md` (SEC-1, SEC-4, SEC-5, SEC-7),
`docs/adr/0010-data-split-extension-intent-connector-truth.md`, `docs/adr/0011-windows-keystore-dpapi.md`,
`docs/architecture/22-component-contracts.md`, `docs/architecture/24-options-envelope-compat.md`,
`docs/architecture/25-native-messaging-protocol.md`, `docs/architecture/26-command-surface.md`,
`docs/architecture/27-config-compilation-pipeline.md`, `docs/architecture/29-subscriptions-and-servers.md`,
`docs/architecture/30-failure-policy.md`, `docs/architecture/31-security-model.md`,
`docs/architecture/34-data-split.md`, `docs/architecture/36-observability.md`.
