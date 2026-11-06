# ADR-0007: A closed, typed, versioned command allowlist is the only thing the connector dispatches

Status: Accepted
Date: 2026-11-04

## Context

The command surface is where a proxy tool becomes dangerous: an interface that can be asked to run anything
turns a local process into a shell. The master prompt's constraint is explicit — no arbitrary shell or RPC
pass-through, typed allowlisted commands only (`docs/PROJECT-STATE.md` §0, constraint 3) — and the same
fork already shows what happens when a privileged decision is made from an unstructured string: an
extension-ID allowlist hard-coded in source, maintained by hand and consulted per request
(`docs/audit/11-security-findings.md`, SEC-4).

The other half of the problem is versioning. `packages/schema/protocol/envelope.schema.json:19` separates the
frame format version from the protocol version precisely because the shim can be replaced independently of
the connector (ADR-0005); that asymmetry only pays off if the set of dispatchable names is known to both
sides before a message is trusted. Phase 0's measured reality is that none of this exists yet: zero Go, zero
Native Messaging, zero protocol code (`docs/audit/00-scope-and-method.md` §1). So the allowlist is authored
first and implemented second, and it is the artifact both implementations bind to.

## Decision

1. The connector MUST dispatch only methods listed in
   `packages/schema/protocol/command-allowlist.json`, which is the single normative artifact for both the Go
   switch and the TypeScript client. It currently lists **exactly 19 methods**
   (`packages/schema/protocol/command-allowlist.json:13` through
   `packages/schema/protocol/command-allowlist.json:31`), each with a `class`, a
   `availableBeforeHello` flag and a `privileged` flag.
2. Resolution MUST be a total function with one failure case: an unknown method MUST produce
   `UnknownMethod` (`packages/schema/protocol/error-taxonomy.json:9`) and MUST be answered before any state is
   read or written — no fallthrough, no prefix matching, no "convenient" generic call, and no proxying of an
   unrecognised method to Core, the shell or the filesystem. The list is deny-by-default by construction
   (`packages/schema/protocol/command-allowlist.json:9`).
3. Every method MUST have a command document under `packages/schema/protocol/commands/` validated by
   `packages/schema/protocol/commands.schema.json:7`, whose eleven required fields make a command describe
   its args, result, errors, timeouts, class, idempotency and side effects. A method without a document is a
   build failure, not a TODO.
4. The three names — the allowlist, the command documents and `docs/architecture/26-command-surface.md` —
   MUST denote the same set. The parity assertion in `packages/schema/scripts/verify.mjs` "hello gating agrees between allowlist and command doc" and "command class agrees between allowlist and command doc" and the count
   assertion in `packages/schema/scripts/verify.mjs` "the command surface is exactly nineteen methods" enforce the first two today; the document cross-check
   degrades to a visible `DEFERRED` note (`packages/schema/scripts/verify.mjs` "DEFERRED: 26-command-surface.md cross-check (file not present yet)") and becomes a real
   assertion the moment the file exists.
5. Commands MUST be versioned with the protocol: the frame carries `v`, `connector.hello` negotiates
   `protocolVersion`, and a command whose `capabilities[]` is non-empty MUST NOT be sent before `hello`
   reports them (`packages/schema/protocol/compatibility-rules.json:16`). Nothing is dispatchable before
   negotiation except `connector.hello` (`packages/schema/scripts/verify.mjs` "nothing is dispatchable before negotiation except connector.hello").
6. Error reporting MUST use the closed taxonomy — 23 codes, each declaring `retryable` — and a code outside
   it MUST be treated as a protocol violation, because a client cannot decide to retry an error it has never
   seen (`packages/schema/protocol/error-taxonomy.json:5`).
7. No field of any command MAY accept a shell command, an arbitrary path or a caller-chosen URL: the
   allowlist says so at `packages/schema/protocol/command-allowlist.json:5`, and the schemas express it as
   keystore references and bounded enums.
8. Adding a method MUST be a change to the allowlist, a new command document, a row in the command-surface
   document and a fixture, in one change; removing one MUST be an ADR that supersedes this one's count.

## Consequences

Cheaper: the security property "this process cannot be told to do something unlisted" is a grep-able data
file rather than a code review note; one artifact generates both the Go switch table and the TypeScript
client union, so the two cannot drift silently; `retryable` being data means the client's backoff policy is
auditable; and every new capability has an obvious place to be refused.

Harder: 19 methods is a real ceiling on what the UI can do this iteration — dashboard and health views are
served by `connector.status`, `connector.health`, `config.snapshot` and `diagnostics.get` and nothing else;
adding anything is a four-artifact change; and the enum discipline means a Phase-2 feature cannot ship as a
stringly-typed `rpc.call` "temporarily".

Forbidden for this iteration: implementing a method that is not in the list; inventing an error code; and
documenting a command surface that names a method the allowlist does not contain.

## Alternatives considered

- **A generic `execute(method, json)` passthrough with runtime validation.** Rejected: it moves the allowlist
  into the implementation, where the parity check cannot see it, and it makes an unlisted method a matter of
  someone's `switch` default branch.
- **Capability-based dynamic method discovery (the connector publishes its own method list).** Rejected:
  discovery is not authorisation. It reintroduces "whatever the connector answers is allowed", and it makes
  the extension unable to refuse a method it was never designed to call safely.
- **gRPC or JSON-RPC with a schema file.** Rejected on the transport argument (ADR-0004 forbids a network
  protocol) and because both generate a client we would have to keep honest against our own envelopes; the
  hand-written allowlist costs more to write once and less to audit forever.
- **One wide `settings.update` method instead of typed commands.** Rejected: it hides side effects —
  `sideEffects` per command (`packages/schema/protocol/commands.schema.json:87`) is what lets ADR-0009's
  blocking path and ADR-0010's keystore boundary be reasoned about per call.

## Evidence

- `packages/schema/protocol/command-allowlist.json:9` — VERIFIED — `"deniedByDefault": true`;
  `:10` — VERIFIED — `"unknownMethodBehavior": "UnknownMethod"`; `:11` — VERIFIED — the total-function
  dispatch rule.
- `packages/schema/protocol/error-taxonomy.json:9` — VERIFIED — `UnknownMethod` is non-retryable, surface
  `diagnostics`.
- `packages/schema/protocol/commands.schema.json:7` — VERIFIED — the eleven required fields; `:35` —
  VERIFIED — the closed `class` enum `read | converge | replace | mutate | probe`.
- `packages/schema/scripts/verify.mjs` "the command surface is exactly nineteen methods" — VERIFIED — the literal assertion that the surface is nineteen methods;
  `packages/schema/scripts/verify.mjs` "every taxonomy entry is a code/layer/meaning object" — VERIFIED — the taxonomy shape assertion (which counts 23 today).
- `packages/schema/protocol/commands/connector.hello.json:5` — VERIFIED — `class: read` for the pre-hello
  negotiation call, whose `requiresHello` field is itself pinned by
  `packages/schema/protocol/commands.schema.json:60`, so "available before hello" is data, not a special case.
- Command counts (19 documents on disk, 8 read / 3 converge / 2 replace / 5 mutate / 1 probe, 11 privileged,
  1 available pre-hello) — VERIFIED by reading the allowlist and the per-command files.

## Verification

Today: `npm --prefix packages/schema run verify` runs the parity check (rule 3 and 4), the count check (rule
1), the pre-hello check (rule 5), the taxonomy checks (rules 2 and 6) and the non-replayable-probe rule, and
replays `packages/schema/fixtures/protocol/*.jsonl` — 109 frames — where an out-of-allowlist method may only
ever produce `UnknownMethod` (`packages/schema/scripts/verify.mjs` "fixture error code is declared by ${method}: ${file}#${frame.id}").

Deferred to Phase 2: the Go switch and the TypeScript client being generated from or tested against these
documents, so that "unknown method" is verified against a live binary rather than a fixture. Specified in
`docs/architecture/26-command-surface.md`; no check exists for rule 7's caller-chosen-URL prohibition beyond
the per-command schemas.
