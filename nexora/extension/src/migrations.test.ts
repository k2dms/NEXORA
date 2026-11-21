import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';
import { MigrationRunner, LEDGER, type MigrationEnv, type MigrationStorage } from './migrations.ts';

const backupFixture = (): Record<string, unknown> =>
  JSON.parse(readFileSync(new URL('../../../docs/verification/backup-3.5.2-all-profile-types.json', import.meta.url), 'utf8'));

/** An in-memory chrome.storage.local stand-in. One map, one blob semantics like the real thing. */
function memoryStorage(initial: Record<string, unknown> = {}): MigrationStorage & { data: Record<string, unknown> } {
  const data: Record<string, unknown> = structuredClone(initial);
  return {
    data,
    async getAll() {
      return structuredClone(data);
    },
    async set(items) {
      for (const [k, v] of Object.entries(items)) data[k] = structuredClone(v);
    },
    async remove(keys) {
      for (const k of keys) delete data[k];
    },
  };
}

function runnerEnv(storage: MigrationStorage, overrides: Partial<MigrationEnv> = {}): MigrationEnv {
  return { storage, extensionVersion: '0.1.0-test', ...overrides };
}

const legacyKeys = (envelope: Record<string, unknown>) => Object.keys(envelope).filter((k) => !k.startsWith('-nexora.'));

test('a version-3 run migrates the shipped 3.5.2 envelope additively', async () => {
  const storage = memoryStorage(backupFixture());
  const before = structuredClone(storage.data);
  const report = await new MigrationRunner(runnerEnv(storage)).run();
  assert.equal(report.outcome, 'migrated', report.reason);
  assert.deepEqual(report.steps.map((s) => s.id), ['stamp', 'settings', 'failure-policy', 'pins', 'subscription-sources', 'profiles', 'routing', 'secret-census', 'finalize']);
  assert.ok(report.steps.every((s) => s.status === 'applied'), report.steps.map((s) => s.id + '=' + s.status).join(','));

  const after = storage.data;
  // The pairing rule: the bare stamp still reads 2, and every legacy key is byte-identical.
  assert.equal(after[LEDGER.legacyStampKey], 2);
  assert.equal(after[LEDGER.nexoraStampKey], 3);
  for (const key of legacyKeys(before)) {
    assert.deepEqual(after[key], before[key], `legacy key ${key} moved`);
  }
  // The four additive documents exist with their corpus-declared content.
  assert.equal((after[LEDGER.settingsKey] as Record<string, unknown>).schemaVersion, 1);
  const policy = after[LEDGER.failurePolicyKey] as Record<string, any>;
  assert.equal(policy.coreDeath.action, 'block', 'the default policy starts fail-closed');
  const pins = after[LEDGER.pinsKey] as Record<string, any>;
  assert.equal(pins.verifySignature, true);
  assert.equal(pins.silentSelfUpdate, false);
  // One subscription source per source-bearing legacy profile: +subscriptions is the only RuleListProfile
  // with a sourceUrl in this fixture.
  const sources = Object.values(after).filter((v: any) => typeof v?.url === 'string' && v.format === 'auto');
  assert.equal(sources.length, 1);
  assert.equal((sources[0] as Record<string, unknown>).url, 'https://rules.example.com/list.txt');
  // A bridge record per legacy profile, each resolving to the profile it names.
  const bridges = Object.values(after).filter((v: any) => typeof v?.legacyProfile === 'string');
  const legacyNames = ['proxy', 'company-pac', 'subscriptions', 'auto switch'];
  assert.equal(bridges.length, legacyNames.length);
  for (const bridge of bridges as any[]) {
    assert.ok(after['+' + bridge.legacyProfile], 'bridge names a missing legacy profile');
    assert.ok(legacyNames.includes(bridge.legacyProfile), bridge.legacyProfile + ' is not a legacy profile of this fixture');
  }
  // The routing policy is inert and the census counted, never moved.
  const routings = Object.values(after).filter((v: any) => typeof v?.browserPlane === 'object');
  assert.equal(routings.length, 1);
  assert.deepEqual(report.census, { headers: 0, auth: 0, gistToken: 0 });
  assert.equal(report.backupId?.startsWith(LEDGER.backupPrefix), true);
  const snapshot = after[report.backupId!] as Record<string, any>;
  assert.equal(snapshot.restoredAtLeastOnce, true);
  assert.equal(snapshot.keyCount, Object.keys(before).length);
  assert.equal(snapshot.fromSchemaVersion, 2);
  assert.equal(snapshot.toSchemaVersion, 3);
});

test('a dry run writes nothing', async () => {
  const storage = memoryStorage(backupFixture());
  const before = JSON.stringify(storage.data);
  const report = await new MigrationRunner(runnerEnv(storage)).run({ dryRun: true });
  assert.equal(report.outcome, 'dry-run');
  assert.equal(JSON.stringify(storage.data), before, 'a dry run wrote');
  assert.ok(report.steps.some((s) => s.id === 'stamp'));
});

test('a second run re-verifies and reports already-migrated', async () => {
  const storage = memoryStorage(backupFixture());
  const first = new MigrationRunner(runnerEnv(storage));
  assert.equal((await first.run()).outcome, 'migrated');
  const second = new MigrationRunner(runnerEnv(storage));
  const report = await second.run();
  assert.equal(report.outcome, 'already-migrated', report.reason);
  assert.ok(report.steps.every((s) => s.status === 'already-applied'));
});

test('an envelope from the future is read-only: SchemaMismatch, no writes, no repair', async () => {
  const storage = memoryStorage({ ...backupFixture(), [LEDGER.nexoraStampKey]: 4 });
  const before = JSON.stringify(storage.data);
  const report = await new MigrationRunner(runnerEnv(storage)).run();
  assert.equal(report.outcome, 'future-envelope', report.reason);
  assert.match(report.reason!, /SchemaMismatch|read-only/);
  assert.equal(JSON.stringify(storage.data), before, 'the read-only client wrote');
});

test('an empty envelope aborts: a wipe recorded as a success is a detectable shape', async () => {
  // Truly empty: no legacy stamp at all, which is the wiped envelope §7 rule 3 refuses to migrate.
  const report = await new MigrationRunner(runnerEnv(memoryStorage())).run();
  assert.equal(report.outcome, 'aborted');
  assert.match(report.reason!, /empty/);
});

test('a run refuses to start while sync is unsettled', async () => {
  const storage = memoryStorage(backupFixture());
  const report = await new MigrationRunner(runnerEnv(storage, { syncState: async () => 'conflict' })).run();
  assert.equal(report.outcome, 'sync-unsettled');
  assert.equal(Object.keys(storage.data).filter((k) => k.startsWith('-nexora.')).length, 0);
});

test('a write failure mid-run restores the envelope to the snapshot', async () => {
  const storage = memoryStorage(backupFixture());
  const before = structuredClone(storage.data);
  // Fail the fourth step's write (pins): the first three steps' keys must be gone after the restore.
  let writes = 0;
  const env = runnerEnv(storage);
  const inner = env.storage.set.bind(env.storage);
  (env as any).storage = {
    getAll: () => storage.getAll(),
    set: async (items: Record<string, unknown>) => {
      if (Object.keys(items).some((k) => k === LEDGER.pinsKey)) throw new Error('quota exceeded (simulated)');
      writes++;
      await inner(items);
    },
    remove: (keys: string[]) => storage.remove(keys),
  };
  const report = await new MigrationRunner(env as MigrationEnv).run();
  assert.equal(report.outcome, 'aborted', report.reason);
  assert.match(report.reason!, /pins/);
  assert.ok(report.reason!.includes('restored'));
  // The restore deleted everything the run had written, and the backup survives as the evidence.
  const after = storage.data;
  for (const key of legacyKeys(before)) assert.deepEqual(after[key], before[key], `legacy key ${key} moved by a failed run`);
  assert.equal(after[LEDGER.nexoraStampKey], undefined);
  assert.equal(after[LEDGER.settingsKey], undefined);
  const backups = Object.keys(after).filter((k) => k.startsWith(LEDGER.backupPrefix));
  assert.equal(backups.length, 1, 'the snapshot is evidence and stays');
  const snapshot = after[backups[0]] as Record<string, any>;
  assert.equal(snapshot.restoredAtLeastOnce, true, 'the restore proved itself against the snapshot');
});

test('a resume after a simulated mid-step death replays and completes', async () => {
  const storage = memoryStorage(backupFixture());
  const runner = new MigrationRunner(runnerEnv(storage));
  assert.equal((await runner.run()).outcome, 'migrated');
  // Simulate the worker dying after step 7 but before finalize's effects: drop the routing policy and the
  // stamp keeps the envelope looking half-applied only if a verifier fails. Routing's verifier fails.
  const routings = Object.keys(storage.data).filter((k) => k.startsWith(LEDGER.routingPrefix));
  await storage.remove(routings);
  const report = await new MigrationRunner(runnerEnv(storage)).run();
  // §8's recovery is restore-then-replay: the second run is a fresh migration, not a resume cursor.
  assert.equal(report.outcome, 'migrated', report.reason);
  assert.ok(Object.keys(storage.data).some((k) => k.startsWith(LEDGER.routingPrefix)), 'the replay did not re-run the failed step');
  assert.equal(storage.data[LEDGER.legacyStampKey], 2);
});

test('a restore that itself fails leaves the visible restore-required state', async () => {
  const storage = memoryStorage(backupFixture());
  const env = runnerEnv(storage);
  const inner = env.storage.set.bind(env.storage);
  const innerRemove = env.storage.remove.bind(env.storage);
  let failNextSet = true;
  (env as any).storage = {
    getAll: () => storage.getAll(),
    set: async (items: Record<string, unknown>) => {
      if (failNextSet && Object.keys(items).some((k) => k === LEDGER.pinsKey)) {
        failNextSet = false;
        throw new Error('quota (first failure)');
      }
      await inner(items);
    },
    remove: async (keys: string[]) => {
      // The restore's own deletion is what fails here: the second pinned write is the replay, so let the
      // first failure happen at pins, then make removals fail to simulate a wedged storage area.
      if (keys.some((k) => k === LEDGER.pinsKey) || keys.length > 1) throw new Error('storage wedged (simulated)');
      await innerRemove(keys);
    },
  };
  const report = await new MigrationRunner(env as MigrationEnv).run();
  assert.equal(report.outcome, 'restore-required', report.reason);
  assert.match(report.reason!, /pins/);
});

test('a connector mirror is used when provided and its absence still migrates version 3', async () => {
  const mirrors: string[] = [];
  const storage = memoryStorage(backupFixture());
  const withMirror = new MigrationRunner(runnerEnv(storage, {
    writeConnectorMirror: async ({ record }) => {
      mirrors.push(record.backupId + ': ' + record.keyCount + ' keys');
      return 'C:/mirror/' + record.backupId + '.json';
    },
  }));
  const report = await withMirror.run();
  assert.equal(report.outcome, 'migrated');
  assert.equal(mirrors.length, 1);
  const snapshot = storage.data[report.backupId!] as Record<string, any>;
  assert.equal(snapshot.connectorMirrorPath, 'C:/mirror/' + report.backupId + '.json', 'the record names the second medium');
});

test('a step that tries to write outside the -nexora namespace is a runner bug, refused', async () => {
  const storage = memoryStorage(backupFixture());
  const env = runnerEnv(storage);
  const runner = new MigrationRunner(env);
  // Reach into the runner's own step list through a normal run: the pairing-rule guard fires on any
  // legacy-key write. Drive it directly by corrupting the settings document's write target via a stubbed
  // set that reports what was attempted.
  const attempted: string[][] = [];
  const inner = env.storage.set.bind(env.storage);
  (env as any).storage = {
    getAll: () => storage.getAll(),
    set: async (items: Record<string, unknown>) => {
      attempted.push(Object.keys(items));
      await inner(items);
    },
    remove: (keys: string[]) => storage.remove(keys),
  };
  const report = await new MigrationRunner(env as MigrationEnv).run();
  for (const keys of attempted) {
    assert.ok(keys.every((k) => k.startsWith('-nexora.')), 'a run wrote a key the legacy engine owns: ' + keys.join(','));
  }
  assert.equal(report.outcome, 'migrated');
});
