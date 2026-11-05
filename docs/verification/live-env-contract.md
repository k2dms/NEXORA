# Live-environment contract for VLESS / REALITY verification

> Purpose: define exactly which environment variables the live matrix (`docs/verification/live-matrix.md`)
> reads, what each one gates, and the rule that makes the matrix trustworthy: **a missing variable produces
> `SKIPPED`, never `PASSED`.**

The tracked template is `.env.example`; `.env` itself is ignored (`.gitignore:8`). Copy the template, fill it,
and never commit it. Nothing in this repository may write a real credential into a tracked file —
`tools/check-no-secrets.mjs` is the mechanical guard and `LIVE-10` is its runtime counterpart.

---

## 1. Governing rules

1. **`SKIPPED` is not `PASSED`.** Each row of the matrix declares the variables it needs; if any is absent or
   empty, the runner reports `SKIPPED` and the row keeps `Verified: NO`. A row becomes `PASSED` only after an
   executed check with the variable present. **VERIFIED** — stated in `.env.example:5-7`.
2. **No success claim without an egress assertion.** A row that says "the tunnel works" is meaningless unless
   the observed public IP equals the test server's (`NEXORA_EXPECTED_EGRESS_IP`); this is ADR-0020's rule and
   the reason `server.test` carries `layers[]` and `matchesExpectedEgress`
   (`packages/schema/protocol/commands/server.test.json`). **VERIFIED**.
3. **Secrets stay in the environment.** The runner reads them; it never persists them into options, logs,
   diagnostics or fixtures. `packages/schema/envelope-keys.schema.json` and
   `packages/schema/protocol/commands/config.snapshot.json` are the contracts that enforce this on the data side.
4. **The server-side private key is not needed.** A REALITY client requires `publicKey`, `shortId`,
   `serverName` and `fingerprint`; the private key belongs to the operator. **VERIFIED** — recorded so nobody
   asks for it later (`.env.example:34-35`).
5. **One transport per run.** `NEXORA_VLESS_NETWORK` selects the transport under test; the matrix walks the
   values rather than inferring coverage from a single run.

## 2. Variables

| Variable | Required for | Meaning / allowed values | Example line |
|---|---|---|---|
| `NEXORA_VLESS_ADDR` | LIVE-05…LIVE-09 | server hostname or IP | `.env.example:11` |
| `NEXORA_VLESS_PORT` | LIVE-05…LIVE-09 | server port | `.env.example:12` |
| `NEXORA_VLESS_UUID` | LIVE-05…LIVE-11 | the VLESS user id the server expects (a secret — `LIVE-10` greps for it) | `.env.example:15` |
| `NEXORA_VLESS_FLOW` | LIVE-05 | `""` or `xtls-rprx-vision`; legal **only** with `network=tcp` + `security=tls` | `.env.example:20` |
| `NEXORA_VLESS_NETWORK` | LIVE-05…LIVE-08 | `tcp` \| `ws` \| `grpc` \| `xhttp` \| `h2` | `.env.example:24` |
| `NEXORA_VLESS_SECURITY` | LIVE-05…LIVE-09 | `none` \| `tls` \| `reality` | `.env.example:27` |
| `NEXORA_VLESS_SNI` | LIVE-05…LIVE-09 | TLS/REALITY `serverName` | `.env.example:30` |
| `NEXORA_VLESS_ALPN` | LIVE-05, LIVE-06 | ALPN list, e.g. `h2,http/1.1` | `.env.example:31` |
| `NEXORA_VLESS_FINGERPRINT` | LIVE-09 | REALITY `fingerprint` (e.g. a Chrome hello id) | `.env.example:32` |
| `NEXORA_VLESS_PUBLIC_KEY` | LIVE-09 | REALITY `publicKey` | `.env.example:36` |
| `NEXORA_VLESS_SHORT_ID` | LIVE-09 | REALITY `shortId` (hex, may be empty) | `.env.example:37` |
| `NEXORA_VLESS_SPIDER_X` | LIVE-09 | REALITY `spiderX` query prefix | `.env.example:38` |
| `NEXORA_EGRESS_PROBE` | LIVE-04, LIVE-05…LIVE-09 | HTTPS endpoint returning the caller's public IP | `.env.example:44` |
| `NEXORA_EXPECTED_EGRESS_IP` | LIVE-04 | the IP the probe must report when traffic really transits the server | `.env.example:49` |
| `NEXORA_XRAY_BIN` | LIVE-02 | path to a pre-installed Xray binary; when unset, acquisition runs against the pinned release | `.env.example:54` |

Every value above is validated structurally by `packages/schema/server.schema.json` before it reaches the
connector — an unusable combination (for example `flow=xtls-rprx-vision` with `network=ws`) must be rejected
by the schema/compiler, which is itself a live assertion (`LIVE-05`, negative branch).

## 3. Combining the variables into the five transport shapes under test

| Row | `NETWORK` | `SECURITY` | `FLOW` | Extra requirement |
|---|---|---|---|---|
| LIVE-05 | `tcp` | `tls` | `xtls-rprx-vision` | — |
| LIVE-06 | `ws` | `tls` | `""` | path + host headers |
| LIVE-07 | `grpc` | `tls` | `""` | `mode=multi` |
| LIVE-08 | `xhttp` | `tls` | `""` | `mode=auto` |
| LIVE-09 | `tcp`/`xhttp` | `reality` | `""` or vision on tcp | public key + short id + fingerprint |

Any combination not in this table is out of scope for the iteration; the schema allows it to be *expressed*,
so `LIVE-05`'s negative branch is what prevents it from being *silently believed*.

## 4. Execution status in this iteration

**None of LIVE-01…LIVE-13 or LIVE-15…LIVE-24 can be executed here**: the connector, the host shim and the Xray
binary do not exist yet, and the Go toolchain is not installed on the development host
(`docs/audit/00-scope-and-method.md` §2). The matrix is therefore published as a **specification**, with every
row's `Verified:` column set to `NO — deferred to Phase 2` except where a Phase-1 artefact already checks the
contract statically. `LIVE-14` (import of a real 3.5.2 backup) is partially executable now — see
`docs/verification/README.md` §3 for the command that compiles the shipped fixtures through the legacy
`omega-pac` engine.
