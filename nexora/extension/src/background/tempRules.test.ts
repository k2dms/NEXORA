import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { TempRules } from './tempRules.ts';

// The toggle semantics are the legacy Options#addTempRule's own (omega-target/src/options.coffee:920-960):
// one rule per domain, 1 forces on, -1 forces off, 0/null toggles, a domain already routed elsewhere is
// moved to the new profile, and every no-op reports so rather than pretending a change.

function memoryStorage(): { get: (k?: string[] | null) => Promise<Record<string, unknown>>; set: (i: Record<string, unknown>) => Promise<void>; data: Record<string, unknown> } {
  const data: Record<string, unknown> = {};
  return {
    data,
    async get(keys) {
      if (!keys) return structuredClone(data);
      const out: Record<string, unknown> = {};
      for (const k of keys) if (k in data) out[k] = structuredClone(data[k]);
      return out;
    },
    async set(items) {
      for (const [k, v] of Object.entries(items)) data[k] = structuredClone(v);
    },
  };
}

test('a temp rule toggles on and off for the same domain', async () => {
  const store = new TempRules(memoryStorage());
  assert.equal(await store.toggle('example.com', 'Work'), 'added');
  let profile = await store.asProfile('direct');
  assert.equal(profile.rules.length, 1);
  assert.deepEqual(profile.rules[0], {
    condition: { conditionType: 'HostWildcardCondition', pattern: '*.example.com' },
    profileName: 'Work',
  });
  assert.equal(await store.toggle('example.com', 'Work'), 'removed');
  profile = await store.asProfile('direct');
  assert.equal(profile.rules.length, 0);
});

test('toggle 1 is idempotent and -1 is a no-op on a missing rule', async () => {
  const store = new TempRules(memoryStorage());
  assert.equal(await store.toggle('a.test', 'Work', 1), 'added');
  assert.equal(await store.toggle('a.test', 'Work', 1), 'unchanged');
  assert.equal(await store.toggle('missing.test', 'Work', -1), 'unchanged');
  const profile = await store.asProfile('direct');
  assert.equal(profile.rules.length, 1);
});

test('a domain routed to another profile is moved, not duplicated', async () => {
  const store = new TempRules(memoryStorage());
  await store.toggle('x.test', 'Work');
  assert.equal(await store.toggle('x.test', 'Home'), 'moved');
  const profile = await store.asProfile('direct');
  assert.equal(profile.rules.length, 1);
  assert.equal(profile.rules[0].condition.pattern, '*.x.test');
  assert.equal(profile.rules[0].profileName, 'Home');
});

test('the built document is the legacy _tempProfile shape', async () => {
  const store = new TempRules(memoryStorage());
  await store.toggle('b.test', 'Home');
  await store.toggle('a.test', 'Work');
  const profile = await store.asProfile('direct', '#99dd99');
  assert.equal(profile.profileType, 'SwitchProfile');
  assert.equal(profile.defaultProfileName, 'direct');
  assert.equal(profile.color, '#99dd99');
  // Rules are in domain order, so the document is byte-stable across reads.
  assert.deepEqual(profile.rules.map((r) => r.condition.pattern), ['*.a.test', '*.b.test']);
});

test('a domain that is not a hostname is refused before any write', async () => {
  const store = new TempRules(memoryStorage());
  await assert.rejects(
    store.toggle('not a host;el', 'Work'),
    /not a hostname/,
  );
  await assert.rejects(store.toggle('', 'Work'), /not a hostname/);
});
