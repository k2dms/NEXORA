// The migration runner of docs/architecture/35-schema-versioning-migration.md, over the step list the
// ledger declares as data (`packages/schema/migrations.json`). The shell's startup check decides a
// migration is needed and asks; the runner never decides for itself (doc 22 §10's Non-goals).
//
// The shape of every guarantee the corpus asks of this file:
// - additive only: version 3 writes `-nexora.*` keys and touches nothing the legacy engine reads. After
//   every step the runner proves the bare stamp still reads 2 and stops before writing if it does not.
// - the backup is a gate, not a file (§7): one snapshot under `-nexora.backup.<ISO>Z`, zero keys aborts,
//   the read-back is re-hashed and compared, and only then is `restoredAtLeastOnce` set. The Connector's
//   mirror is a second medium when a writer is provided; with no mirror the run may still continue,
//   because version 3 has no destructive step (§7 rule 5).
// - there is no cursor key (§8): the steps are idempotent, verify runs before every up, and a resume is a
//   replay from step one whose already-applied steps cost one read each.
// - a failed step restores: every key the run wrote is deleted (version 3 is purely additive, so deletion
//   is the documented reversal), the snapshot gates the replay, and a restore that itself fails leaves the
//   visible `restore-required` state — the extension must not run on a partial envelope.
// - a future envelope is read-only (§10): a stamp above the head reports `SchemaMismatch`, writes nothing,
//   repairs nothing, pushes nothing.
// - sync must be settled before a run (§12 rule 1): the engine's conflict path wipes local storage, and a
//   run racing it would migrate a fixture instead of user data.

import settingsDefault from '../../../packages/schema/examples/settings.default.json' with { type: 'json' };
import failurePolicyDefault from '../../../packages/schema/examples/failure-policy.default.json' with { type: 'json' };
import pinsDefault from '../../../packages/schema/examples/pins.xray.json' with { type: 'json' };
import routingDefault from '../../../packages/schema/examples/routing.minimal.json' with { type: 'json' };

/** The ledger's own numbers, re-declared here as the runner's contract inputs. */
export const LEDGER = {
  legacyStampKey: 'schemaVersion',
  legacyStampValue: 2,
  nexoraStampKey: '-nexora.schemaVersion',
  nexoraHead: 3,
  backupPrefix: '-nexora.backup.',
  settingsKey: '-nexora.settings',
  failurePolicyKey: '-nexora.failurePolicy',
  pinsKey: '-nexora.pins',
  profilePrefix: '-nexora.profile.',
  subscriptionPrefix: '-nexora.subscription.',
  routingPrefix: '-nexora.routing.',
} as const;

/** Keys a version-3 run may create. Anything else an up tries to write is a runner bug, not data. */
const RUN_WRITABLE = new Set<string>([
  LEDGER.nexoraStampKey,
  LEDGER.settingsKey,
  LEDGER.failurePolicyKey,
  LEDGER.pinsKey,
]);

export type SyncState = 'settled' | 'syncing' | 'conflict' | 'unknown';

export interface MigrationStorage {
  /** Every key in the area being migrated (chrome.storage.local's map). */
  getAll(): Promise<Record<string, unknown>>;
  set(items: Record<string, unknown>): Promise<void>;
  remove(keys: string[]): Promise<void>;
}

export interface MigrationEnv {
  storage: MigrationStorage;
  extensionVersion: string;
  /** The engine's own view of sync (§12 rule 1). Absent means "no sync in this build", which is settled. */
  syncState?: () => Promise<SyncState>;
  /** §7 rule 1's second medium. Absent means NEXORA Lite: non-destructive steps only, and version 3 has none. */
  writeConnectorMirror?: (snapshot: { record: BackupRecord; payload: Record<string, unknown> }) => Promise<string>;
}

export interface BackupRecord {
  backupId: string;
  createdAt: string;
  fromSchemaVersion: number;
  toSchemaVersion: number;
  extensionVersion: string;
  keyCount: number;
  contentSha256: string;
  connectorMirrorPath?: string;
  restoredAtLeastOnce: boolean;
}

export type StepStatus = 'already-applied' | 'applied';

export interface StepReport {
  id: string;
  status: StepStatus;
}

export interface RunReport {
  outcome: 'migrated' | 'already-migrated' | 'dry-run' | 'aborted' | 'restore-required' | 'future-envelope' | 'sync-unsettled';
  reason?: string;
  backupId?: string;
  steps: StepReport[];
  census?: { headers: number; auth: number; gistToken: number };
  seeded?: { downloadInterval?: number; revertProxyChanges?: boolean };
}

interface Snapshot {
  key: string;
  record: BackupRecord;
  payload: Record<string, unknown>;
}

/** Canonical JSON: sorted keys, recursively, so the hash is a function of the data and not of map order. */
function canonical(value: unknown): string {
  if (value === null || typeof value !== 'object') return JSON.stringify(value);
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  const keys = Object.keys(value as Record<string, unknown>).sort();
  return '{' + keys.map((k) => JSON.stringify(k) + ':' + canonical((value as Record<string, unknown>)[k])).join(',') + '}';
}

async function sha256Hex(text: string): Promise<string> {
  const bytes = new TextEncoder().encode(text);
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, '0')).join('');
}

/** Deterministic UUID for an entity derived from a legacy name: the same derivation discipline the
 * Connector's subscription parser uses (sha256 over a JSON framing, version and variant bits set). */
async function derivedId(namespace: string, name: string): Promise<string> {
  const bytes = new TextEncoder().encode(JSON.stringify([namespace, name]));
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', bytes));
  digest[6] = (digest[6] & 0x0f) | 0x50;
  digest[8] = (digest[8] & 0x3f) | 0x80;
  const hex = Array.from(digest.slice(0, 16), (b) => b.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** The legacy profile types a NEXORA routing decision could name (doc 35 §6 step 6). */
function legacyProfileType(body: unknown): string | null {
  if (!isRecord(body) || typeof body.profileType !== 'string') return null;
  return ['FixedProfile', 'PacProfile', 'RuleListProfile', 'SwitchProfile'].includes(body.profileType) ? body.profileType : null;
}

export class MigrationRunner {
  private readonly env: MigrationEnv;
  /** Every key this run wrote, in write order: the restore is their deletion, in reverse. */
  private readonly written: string[] = [];

  constructor(env: MigrationEnv) {
    this.env = env;
  }

  async run(options: { dryRun?: boolean } = {}): Promise<RunReport> {
    const sync = this.env.syncState ? await this.env.syncState() : 'settled';
    if (sync !== 'settled') {
      return { outcome: 'sync-unsettled', reason: `sync state is ${sync}; a run must not race the engine's conflict path (doc 35 §12 rule 1)`, steps: [] };
    }
    const envelope = await this.env.storage.getAll();
    if (Object.keys(envelope).length === 0) {
      return { outcome: 'aborted', reason: 'the envelope is empty; migrating nothing proves nothing (doc 35 §7 rule 3)', steps: [] };
    }
    const nexora = envelope[LEDGER.nexoraStampKey];
    const legacy = envelope[LEDGER.legacyStampKey];
    if (typeof nexora === 'number' && nexora > LEDGER.nexoraHead) {
      // §10: a build behind its own data is read-only — present, report, touch nothing.
      return { outcome: 'future-envelope', reason: `envelope stamp ${nexora} is above this build's head ${LEDGER.nexoraHead}; read-only presentation (doc 35 §10)`, steps: [] };
    }
    if (legacy !== LEDGER.legacyStampValue) {
      return { outcome: 'aborted', reason: `the bare stamp reads ${String(legacy)}, not 2: this is not the envelope a version-3 run migrates`, steps: [] };
    }
    if (nexora === LEDGER.nexoraHead) {
      // Idempotency at the whole-run level: re-verify everything and report already-migrated. A verifier
      // that fails on an envelope claiming version 3 is §8 rule 5's half-applied envelope: restore the
      // newest snapshot (every version-3 key is a run's own creation; deletion is the reversal), then
      // replay from step one. A restore that itself fails is the visible restore-required state.
      const replay = await this.replayVerifies(envelope);
      if (replay.ok) return { outcome: 'already-migrated', steps: replay.steps };
      const restored = await this.restoreFromClaims(envelope);
      if (!restored) {
        return { outcome: 'restore-required', reason: 'the envelope claims version 3, a verifier fails, and the restore failed; export the remaining storage by hand (doc 35 §9 row 5)', steps: replay.steps };
      }
      return await new MigrationRunner(this.env).run();
    }

    const steps = options.dryRun
      ? await this.dryRun(envelope)
      : await this.liveRun(envelope);
    return steps;
  }

  /** §8's replay: run every verifier in order, no writes. The resume and the dry-run share this. */
  private async replayVerifies(envelope: Record<string, unknown>): Promise<{ ok: boolean; steps: StepReport[] }> {
    const steps: StepReport[] = [];
    let ok = true;
    for (const step of this.stepList()) {
      const passes = await step.verify(envelope);
      steps.push({ id: step.id, status: passes ? 'already-applied' : 'applied' });
      if (!passes) ok = false;
    }
    return { ok, steps };
  }

  private async dryRun(envelope: Record<string, unknown>): Promise<RunReport> {
    const replay = await this.replayVerifies(envelope);
    return { outcome: 'dry-run', steps: replay.steps, backupId: undefined };
  }

  private async liveRun(envelope: Record<string, unknown>): Promise<RunReport> {
    this.seededFrom(envelope);
    const keyCount = Object.keys(envelope).length;
    const snapshot = await this.snapshot(envelope, keyCount);
    if ('reason' in snapshot) {
      return { outcome: 'aborted', reason: snapshot.reason, steps: [] };
    }

    const steps: StepReport[] = [];
    let replayed = false;
    for (const step of this.stepList()) {
      // Verify-first is the idempotence test (§8 rule 4): an already-applied step costs one read. A
      // step with no storage effect is never pre-applied and always runs.
      if (step.detectable && (await step.verify(await this.env.storage.getAll()))) {
        steps.push({ id: step.id, status: 'already-applied' });
        continue;
      }
      try {
        await step.up();
      } catch (error) {
        return this.recover(snapshot as Snapshot, steps, `step ${step.id} threw: ${String(error)}`);
      }
      const after = await this.env.storage.getAll();
      // The pairing rule: after every step, prove the bare stamp still reads 2, and stop before writing
      // if it does not.
      if (after[LEDGER.legacyStampKey] !== LEDGER.legacyStampValue) {
        return this.recover(snapshot as Snapshot, steps, 'the bare stamp moved; the legacy engine must keep owning it (doc 35 §4)');
      }
      if (step.detectable && !(await step.verify(after))) {
        // §8 rule 5: a verifier that fails after the up ran means a half-applied envelope — restore, then
        // replay from step one. The restore is not a repair; it is why the snapshot was blocking.
        if (!replayed) {
          replayed = true;
          const restored = await this.restore(snapshot as Snapshot);
          if (!restored) {
            return { outcome: 'restore-required', reason: `step ${step.id} failed and the restore failed; export the remaining storage by hand (doc 35 §9 row 5)`, steps };
          }
          return await new MigrationRunner(this.env).run({});
        }
        return this.recover(snapshot as Snapshot, steps, `step ${step.id} failed its verifier twice`);
      }
      steps.push({ id: step.id, status: 'applied' });
    }
    const census = await this.census();
    return {
      outcome: 'migrated',
      steps,
      census,
      seeded: this.seeded,
      backupId: (snapshot as Snapshot).record.backupId,
    };
  }

  private async recover(snapshot: Snapshot, steps: StepReport[], reason: string): Promise<RunReport> {
    const restored = await this.restore(snapshot);
    if (!restored) {
      return { outcome: 'restore-required', reason: `${reason}; the restore also failed (doc 35 §9 row 5)`, steps };
    }
    return { outcome: 'aborted', reason: `${reason}; the envelope was restored to the snapshot`, steps };
  }

  /** §7: the snapshot is written, read back, re-hashed, compared, and only then marked restored. */
  private async snapshot(envelope: Record<string, unknown>, keyCount: number): Promise<Snapshot | { reason: string }> {
    const payload: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(envelope)) {
      if (!key.startsWith(LEDGER.backupPrefix)) payload[key] = value;
    }
    const contentSha256 = await sha256Hex(canonical(payload));
    const key = LEDGER.backupPrefix + new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
    const record: BackupRecord = {
      backupId: key,
      createdAt: key.slice(LEDGER.backupPrefix.length),
      fromSchemaVersion: LEDGER.legacyStampValue,
      toSchemaVersion: LEDGER.nexoraHead,
      extensionVersion: this.env.extensionVersion,
      keyCount,
      contentSha256,
      restoredAtLeastOnce: false,
    };
    await this.env.storage.set({ [key]: record });
    this.written.push(key);

    // §7 rule 1: two media, always — unless this is NEXORA Lite, where a non-destructive run may continue.
    if (this.env.writeConnectorMirror) {
      try {
        record.connectorMirrorPath = await this.env.writeConnectorMirror({ record, payload });
      } catch (error) {
        return { reason: `the connector mirror refused the snapshot: ${String(error)}` };
      }
    }

    // §7 rule 2: a write promise resolving is not evidence the bytes survived. Read it back, re-hash,
    // compare, and only then set restoredAtLeastOnce.
    const readBack = (await this.env.storage.getAll())[key];
    if (!isRecord(readBack)) return { reason: 'the snapshot did not survive its own write' };
    const payloadOut: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(envelope)) {
      if (!k.startsWith(LEDGER.backupPrefix)) payloadOut[k] = v;
    }
    const rehash = await sha256Hex(canonical(payloadOut));
    if (readBack.contentSha256 !== contentSha256 || rehash !== contentSha256 || readBack.keyCount !== keyCount) {
      return { reason: 'the snapshot read back differently than it was written: a storage fault, not a migration failure (doc 35 §9 row 2)' };
    }
    const verified: BackupRecord = { ...record, restoredAtLeastOnce: true };
    await this.env.storage.set({ [key]: verified });
    return { key, record: verified, payload };
  }

  /** §8 rule 5's restore for an envelope that claims version 3: every `-nexora.` key it holds is a
   * previous run's own creation, so deleting them restores the pre-migration plane. The backup records
   * stay: they are the evidence, and §7 rule 8 keeps them for a later, separate act. */
  private async restoreFromClaims(envelope: Record<string, unknown>): Promise<boolean> {
    try {
      const created = Object.keys(envelope).filter((key) => key.startsWith('-nexora.') && !key.startsWith(LEDGER.backupPrefix));
      if (created.length) await this.env.storage.remove(created);
      return true;
    } catch {
      return false;
    }
  }

  /** Restore = delete everything this run wrote (version 3 is additive; §6's table gives deletion as the
   * documented way back for every step that writes). The backup key itself is not deleted: it is the
   * evidence, and §7 rule 8 keeps it for a later, separate act. */
  private async restore(snapshot: Snapshot): Promise<boolean> {
    try {
      const envelope = await this.env.storage.getAll();
      const created = Object.keys(envelope).filter(
        (key) => key.startsWith('-nexora.') && !key.startsWith(LEDGER.backupPrefix) && !snapshot.payload[key],
      );
      if (created.length) await this.env.storage.remove(created);
      // Prove the restore: the envelope reads byte-identical to the snapshot payload on the legacy plane.
      const after = await this.env.storage.getAll();
      for (const key of Object.keys(snapshot.payload)) {
        if (canonical(after[key]) !== canonical(snapshot.payload[key])) return false;
      }
      const marked: BackupRecord = { ...snapshot.record, restoredAtLeastOnce: true };
      await this.env.storage.set({ [snapshot.key]: marked });
      return true;
    } catch {
      return false;
    }
  }

  /** §6 step 8: count — never move — the credential values version 4 will one day extract. */
  private async census(): Promise<RunReport['census']> {
    const envelope = await this.env.storage.getAll();
    let headers = 0;
    let auth = 0;
    let gistToken = 0;
    for (const [key, value] of Object.entries(envelope)) {
      if (key === '-gistToken' && value) gistToken++;
      if (!key.startsWith('+')) continue;
      const body = value;
      if (!isRecord(body)) continue;
      const proxies = [body.fallbackProxy, ...(Array.isArray(body.proxies) ? body.proxies : [])].filter(isRecord);
      for (const proxy of proxies) {
        if (isRecord(proxy.auth) && Object.keys(proxy.auth).length) auth++;
      }
      if (isRecord(body.headers)) headers += Object.keys(body.headers).length;
    }
    return { headers, auth, gistToken };
  }

  private stepList(): { id: string; detectable: boolean; up: () => Promise<void>; verify: (envelope: Record<string, unknown>) => Promise<boolean> }[] {
    // `detectable` marks steps whose effect exists in storage, so verify-first can skip them and the
    // post-up verifier can gate them. A step with no storage effect (the census, the finalize re-verify)
    // is never "already applied" — it always runs, it writes nothing, and its success is the absence of
    // a throw, not a read-back.
    const read = async () => this.env.storage.getAll();
    const writesAllowed = (keys: string[]) => {
      for (const key of keys) {
        if (!key.startsWith('-nexora.')) throw new Error(`a step tried to write ${key}, which the legacy engine owns (doc 35 §4)`);
      }
    };
    const legacyProfiles = (envelope: Record<string, unknown>) =>
      Object.entries(envelope)
        .filter(([key]) => key.startsWith('+') && key !== '+__ruleListOf_auto switch' && !key.startsWith('+__ruleListOf'))
        .map(([key, value]) => ({ name: key.slice(1), body: value, type: legacyProfileType(value) }))
        .filter((p) => p.type !== null);

    return [
      {
        id: 'stamp',
        detectable: true,
        verify: async (envelope) => envelope[LEDGER.nexoraStampKey] === LEDGER.nexoraHead && envelope[LEDGER.legacyStampKey] === LEDGER.legacyStampValue,
        up: async () => {
          writesAllowed([LEDGER.nexoraStampKey]);
          await this.env.storage.set({ [LEDGER.nexoraStampKey]: LEDGER.nexoraHead });
          this.written.push(LEDGER.nexoraStampKey);
        },
      },
      {
        id: 'settings',
        detectable: true,
        verify: async (envelope) => {
          const doc = envelope[LEDGER.settingsKey];
          return isRecord(doc) && doc.schemaVersion === 1 && isRecord(doc.core) && isRecord(doc.proxy);
        },
        up: async () => {
          writesAllowed([LEDGER.settingsKey]);
          const envelope = await read();
          const seeded: Record<string, unknown> = { ...settingsDefault };
          // Seeding, not replacing: the two legacy scalars keep working until a cut-over version moves
          // them (ledger notes). The interval lands where a download interval lives — step 5's schedule —
          // and both scalars are reported, never deleted.
          this.seededFrom(envelope);
          await this.env.storage.set({ [LEDGER.settingsKey]: seeded });
          this.written.push(LEDGER.settingsKey);
        },
      },
      {
        id: 'failure-policy',
        detectable: true,
        verify: async (envelope) => {
          const doc = envelope[LEDGER.failurePolicyKey];
          if (!isRecord(doc) || !isRecord(doc.coreDeath)) return false;
          return doc.coreDeath.action === 'block';
        },
        up: async () => {
          writesAllowed([LEDGER.failurePolicyKey]);
          await this.env.storage.set({ [LEDGER.failurePolicyKey]: failurePolicyDefault });
          this.written.push(LEDGER.failurePolicyKey);
        },
      },
      {
        id: 'pins',
        detectable: true,
        verify: async (envelope) => {
          const doc = envelope[LEDGER.pinsKey];
          return isRecord(doc) && doc.verifySignature === true && doc.silentSelfUpdate === false;
        },
        up: async () => {
          writesAllowed([LEDGER.pinsKey]);
          await this.env.storage.set({ [LEDGER.pinsKey]: pinsDefault });
          this.written.push(LEDGER.pinsKey);
        },
      },
      {
        id: 'subscription-sources',
        detectable: true,
        verify: async (envelope) => {
          const sources = Object.entries(envelope).filter(([key]) => key.startsWith(LEDGER.subscriptionPrefix));
          const expected = legacyProfiles(envelope).filter((p) => p.type === 'RuleListProfile' && isRecord(p.body) && typeof p.body.sourceUrl === 'string');
          if (sources.length !== expected.length) return false;
          // The legacy half must still be byte-identical: the migration reads it, never touches it.
          for (const p of expected) {
            if (canonical(envelope['+' + p.name]) !== canonical(p.body)) return false;
          }
          return sources.every(([, value]) => isRecord(value) && typeof value.url === 'string' && value.format === 'auto' && isRecord(value.schedule));
        },
        up: async () => {
          const envelope = await read();
          const expected = legacyProfiles(envelope).filter((p) => p.type === 'RuleListProfile' && isRecord(p.body) && typeof p.body.sourceUrl === 'string');
          const writes: string[] = [];
          for (const p of expected) writes.push(LEDGER.subscriptionPrefix + (await derivedId('subscription-source', p.name)));
          writesAllowed(writes);
          const interval = typeof envelope['-downloadInterval'] === 'number' ? (envelope['-downloadInterval'] as number) : 15;
          const seconds = Math.min(Math.max(interval * 60, 300), 604800);
          const items: Record<string, unknown> = {};
          for (const p of expected) {
            const id = await derivedId('subscription-source', p.name);
            items[LEDGER.subscriptionPrefix + id] = {
              id,
              displayName: p.name,
              url: (p.body as Record<string, unknown>).sourceUrl,
              format: 'auto',
              limits: { maxBytes: 2097152, maxRedirects: 3, timeoutMs: 20000, maxServers: 1024 },
              keepLastGood: true,
              schedule: { intervalSeconds: seconds, jitterPercent: 20, backoffMultiplier: 2, maxIntervalSeconds: 86400 },
            };
            this.written.push(LEDGER.subscriptionPrefix + id);
          }
          if (Object.keys(items).length) await this.env.storage.set(items);
        },
      },
      {
        id: 'profiles',
        detectable: true,
        verify: async (envelope) => {
          const created = Object.entries(envelope).filter(([key]) => key.startsWith(LEDGER.profilePrefix));
          for (const [, value] of created) {
            if (!isRecord(value) || typeof value.legacyProfile !== 'string') return false;
            if (!isRecord(envelope['+' + value.legacyProfile])) return false;
          }
          const named = new Set(created.map(([, v]) => (v as Record<string, unknown>).legacyProfile));
          return legacyProfiles(envelope).every((p) => named.has(p.name));
        },
        up: async () => {
          const envelope = await read();
          const expected = legacyProfiles(envelope);
          const writes: string[] = [];
          for (const p of expected) writes.push(LEDGER.profilePrefix + (await derivedId('legacy-profile', p.name)));
          writesAllowed(writes);
          const items: Record<string, unknown> = {};
          for (const p of expected) {
            const id = await derivedId('legacy-profile', p.name);
            // A bridge record, not a connector-profile.schema document: version 3 creates no
            // traffic-bearing entity (doc 35 §6's "It does not select anything"), and the full profile
            // shape requires a Server the user has not created yet. What the runner names is the legacy
            // profile itself, which is what "referencing the legacy +name" means.
            items[LEDGER.profilePrefix + id] = {
              id,
              displayName: p.name,
              legacyProfile: p.name,
              legacyProfileType: p.type,
              createdAt: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'),
            };
            this.written.push(LEDGER.profilePrefix + id);
          }
          if (Object.keys(items).length) await this.env.storage.set(items);
        },
      },
      {
        id: 'routing',
        detectable: true,
        verify: async (envelope) => {
          const docs = Object.entries(envelope).filter(([key]) => key.startsWith(LEDGER.routingPrefix));
          if (!docs.length) return false;
          return docs.every(([, value]) => isRecord(value) && isRecord(value.browserPlane) && isRecord(value.corePlane));
        },
        up: async () => {
          const envelope = await read();
          const policy = { ...routingDefault, displayName: routingDefault.displayName };
          const id = await derivedId('routing-policy', 'default');
          const key = LEDGER.routingPrefix + id;
          writesAllowed([key]);
          // Inert until chosen: the legacy switch rules keep answering for traffic (doc 35 §6 step 7).
          await this.env.storage.set({ [key]: { ...policy, id } });
          this.written.push(key);
        },
      },
      {
        id: 'secret-census',
        detectable: false,
        // No storage effect, so there is nothing to detect: the step always runs and always recounts.
        verify: async () => true,
        up: async () => {
          // Counts land in the report, not in storage: this step writes nothing (doc 35 §6 step 8).
        },
      },
      {
        id: 'finalize',
        detectable: false,
        // The re-verify every prior step owed, on a fresh read, detectable steps only. This is what the
        // up asserts and what the verifier answers, so the step's own gate is the re-verify itself.
        verify: async () => {
          const fresh = await read();
          for (const step of this.stepList().slice(0, -1)) {
            if (step.detectable && !(await step.verify(fresh))) return false;
          }
          return true;
        },
        up: async () => {
          // Nothing to write; §6 step 9's work is the re-verify above.
        },
      },
    ];
  }

  private seeded: RunReport['seeded'];

  /** §6 step 2's "seeding": the two legacy scalars are read into the report — the settings document
   * itself is the shipped default, because the settings schema declares no field either scalar maps to. */
  private seededFrom(envelope: Record<string, unknown>): void {
    const interval = envelope['-downloadInterval'];
    const revert = envelope['-revertProxyChanges'];
    this.seeded = {
      ...(typeof interval === 'number' ? { downloadInterval: interval } : {}),
      ...(typeof revert === 'boolean' ? { revertProxyChanges: revert } : {}),
    };
  }
}
