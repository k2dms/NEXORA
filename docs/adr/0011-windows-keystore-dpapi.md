# ADR-0011: On Windows the Connector stores secrets in a DPAPI-protected per-user file set

Status: Accepted
Date: 2026-11-04

## Context

The data split (ADR-0010) gives the extension user *intent* and the Connector *executable truth*. The
secret half of that truth -- the VLESS `userId`, REALITY key material, gRPC/ws header maps carrying
`Authorization`, subscription tokens in query strings -- therefore has to live somewhere the extension
cannot read, cannot sync and cannot log. Brief constraint 9 makes this non-optional: minimal storage,
masking, never in logs, no home-grown crypto.

Legacy ZeroOmega has no secret boundary at all. `omega-target/src/options.coffee:27` is the list of
profile fields stripped before a profile is copied toward `chrome.storage.sync`; it names four fields and
`headers` is not one of them, which is SEC-1 as recorded in `docs/audit/11-security-findings.md`: a
subscription header travels verbatim into GitHub Gist and WebDAV. The exclusion mechanism already exists,
so the bug is not architecture but an unextended allowlist -- and the same file at
`omega-target/src/options.coffee:89-99` shows the mechanism is per-key, not per-value-shape.

On Windows the plausible stores are: DPAPI (`CryptProtectData`, user scope), Windows Credential Manager,
a privileged daemon holding a master key, or a plain file with an ACL.

## Decision

1. On Windows the keystore MUST be a set of DPAPI-protected files under
   `%LOCALAPPDATA%\NEXORA\keystore\`, one blob per secret, protected with **user-scope** entropy and no
   application entropy. The directory MUST be created by the Connector process, inherit the user profile
   ACL only, and MUST NOT sit inside the extension directory or `Program Files` (no elevation).
2. Only the Connector reads or writes it. The extension MUST NOT receive plaintext and MUST NOT hold it:
   every secret-shaped field in an envelope document is a reference, patterned
   `^keystore://[A-Za-z0-9._:/-]{1,96}$` (`packages/schema/server.schema.json:49` for `userIdRef`,
   `packages/schema/server.schema.json:140` for header maps,
   `packages/schema/subscription-source.schema.json:36` for subscription `Authorization`). A schema that
   cannot *spell* a value cannot leak one.
3. A reference is inert outside the machine that minted it, which is why secret-bearing namespaces are
   allowed to be sync-eligible only when their payload contains references
   (`packages/schema/examples/envelope-keys.nexora.json:12`).
4. Deletion is part of the contract: every scope of `keystore.purge` (`server`, `subscription`,
   `profile`, `orphaned`, `all`) requires a `confirm://` token and returns the purged references as
   `^keystore://` strings (`packages/schema/protocol/commands/keystore.purge.json:44`), so secrets do not
   outlive the entity that used them.
5. A failed DPAPI call MUST surface as `KeystoreUnavailable`
   (`packages/schema/protocol/error-taxonomy.json:28`) and MUST be handled fail-closed (ADR-0009). The
   Connector MUST NOT silently fall back to a plaintext file.
6. Logs and `diagnostics.get` carry references only. The Connector MUST redact anything matching a
   secret shape before writing, and MUST NOT accept a `SecretRedacted`-shaped payload at a boundary
   instead of refusing it.
7. **Residual risk, stated as part of the decision.** DPAPI is *user*-scope, not *process*-scope: any
   process running as the same user can call `CryptUnprotectData` on these blobs. Therefore **the
   Connector is not a confidentiality boundary against the local user**, nor against malware running
   with the user's rights. The boundary it does provide is against: `chrome.storage.sync` and its
   backends, the options export file, logs, crash dumps handed to support, and another machine that
   obtains the envelope. The full threat model belongs to `docs/architecture/32-secrets-keystore-masking.md`
   and this ADR does not restate it.

## Consequences

- Secrets survive extension uninstall (deliberate: they are the user's, and `keystore.purge` scope `all`
  is the documented way to destroy them).
- A backup of `chrome.storage.local` no longer contains recoverable credentials. Restoring on another
  machine yields references that resolve to nothing until the user re-enters them -- which must be a
  visible UI state, not a silent failure.
- Cross-platform: Linux and macOS need their own ADRs when the Connector ships there; this ADR binds
  Windows only, and the envelope contract (references) stays identical across platforms.
- SEC-1 in legacy code is NOT fixed by this decision: the legacy sync path still copies `headers`. The
  keystore only becomes load-bearing once NEXORA entities stop using the legacy field. That is a Phase-2
  requirement with an existing data statement: `syncExcludedValueKeys` must contain `headers`.
- First-run ordering: keystore writes precede `config.apply`, because a compiled config referencing a
  missing secret must fail as `KeystoreUnavailable`, not start a Core with an empty credential.

## Alternatives considered

- **Windows Credential Manager / a cross-platform keyring crate.** Rejected for now, explicitly deferred.
  Three measured objections: a UI consent prompt on first read from a non-interactive Native Messaging
  child (the Connector has no console owner, so a prompt deadlocks the pipeline), per-entry size and
  quantity quotas that a subscription with hundreds of servers can hit, and materially different behaviour
  between Credential Manager, macOS Keychain and freedesktop Secret Service -- the last of which does not
  exist at all on many Linux hosts. Adopting one requires a new ADR, not an edit of this one.
- **A privileged daemon owning a master key.** Rejected: adds a second always-on process to keep alive,
  contradicts the containment chain of ADR-0005, and buys nothing against the same-user attacker DPAPI
  already concedes.
- **Plaintext file guarded only by an NTFS ACL.** Rejected: DPAPI costs one call and removes the class of
  ACL-inheritance mistakes a file-only guard invites.
- **Keep secrets in the envelope, masked in the UI.** Rejected outright by constraint 9 and disproved by
  SEC-1: masking is a display property, and `chrome.storage.sync` does not care what the display does.

## Evidence

- VERIFIED -- `omega-target/src/options.coffee:27`: the legacy strip list omits `headers`.
- VERIFIED -- `omega-target/src/options.coffee:89-99`: `transformValueForSync` drops only `-customCss`
  whole and the four listed value keys per profile.
- VERIFIED -- `packages/schema/server.schema.json:49` and `packages/schema/server.schema.json:140`:
  secret fields are `keystore://` patterns, never value types.
- VERIFIED -- `packages/schema/subscription-source.schema.json:36`: subscription header reference.
- VERIFIED -- `packages/schema/protocol/error-taxonomy.json:28`: `KeystoreUnavailable` exists and is
  not retryable, matching "wrong user session / DPAPI failure".
- VERIFIED -- `packages/schema/protocol/commands/keystore.purge.json:44`: purge results are references.
- VERIFIED -- `packages/schema/examples/envelope-keys.nexora.json:61` and
  `packages/schema/examples/envelope-keys.nexora.json:70`: the two namespaces declared
  `secretBearing: true`, and therefore not sync-eligible.
- INFERRED -- Windows DPAPI user-scope semantics (a same-user process can decrypt). OS behaviour, not
  observable in this tree; no keystore code exists yet (`docs/PROJECT-STATE.md` §4.6 lists what does).
- UNVERIFIED -- ACL inheritance of `%LOCALAPPDATA%\NEXORA\keystore` on a real host, and the Connector's
  redaction path. Both are Phase-2 observations.

## Verification

- Existing check: `npm --prefix packages/schema run verify` asserts the `keystore://` patterns against the
  five canonical Server examples and rejects inline secrets (`packages/schema/examples/invalid/`), and
  `node tools/check-no-secrets.mjs` scans the tree for literal credentials.
- Existing check: `node tools/check-doc-evidence.mjs` resolves every citation above.
- Verified: NO -- deferred to Phase 2: DPAPI round-trip, same-user decrypt demonstration, `keystore.purge`
  orphan collection, and redaction in logs. These become `LIVE` rows plus Go unit tests in the connector
  module; the toolchain arrived on 2026-11-05 and the module has three packages, none of them the keystore
  (`docs/PROJECT-STATE.md` §4.6).
