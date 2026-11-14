import { strict as assert } from 'node:assert';
import { test } from 'node:test';
import { loadRegistry } from './registry.ts';
import type { SavedProfile } from '../model.ts';

const serverIds = ['3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01', 'e2b7c4a9-6d15-4f83-9a27-3c5e7b9d1f42']; // // nexora-secret-allow: synthetic canonical ids, nothing real behind any of them — the same identities the connector-side tests use

function profile(overrides: Partial<SavedProfile> = {}): SavedProfile {
  return {
    id: 'b9d6f2c4-1a85-4e37-9c50-6d3b8f2a4e11', // // nexora-secret-allow: synthetic canonical ids, nothing real behind any of them — the same identities the connector-side tests use
    displayName: 'Работа',
    kind: 'connector',
    serverIds: [serverIds[0]],
    defaultServerId: serverIds[0],
    selection: { mode: 'fixed' },
    failover: { enabled: false },
    failurePolicy: { coreDeath: { action: 'block' } },
    ...overrides,
  };
}

function load(profiles: SavedProfile[], ids: string[] = serverIds) {
  return loadRegistry({ profiles, serverIds: ids });
}

// The one class of invalid data the contract layer provably cannot catch: JSON Schema sees one envelope key
// at a time, so a profile naming a server that does not exist is a valid document, and the registry's load
// is the check (VER-31, docs/architecture/22-component-contracts.md §6 invariant 4).
test('a profile whose serverId no server key holds is refused with a named reason', () => {
  const dangling = profile({ id: 'c1d1f2c4-1a85-4e37-9c50-6d3b8f2a4e12', serverIds: ['0d0d0d0d-1a85-4e37-9c50-6d3b8f2a4e33'], defaultServerId: '0d0d0d0d-1a85-4e37-9c50-6d3b8f2a4e33' }); // // nexora-secret-allow: synthetic canonical ids, nothing real behind any of them — the same identities the connector-side tests use
  const result = load([profile(), dangling]);
  assert.deepEqual(result.profiles.map((p) => p.id), [profile().id]);
  assert.equal(result.refused.length, 1);
  assert.equal(result.refused[0].profileId, dangling.id);
  assert.match(result.refused[0].reason, /references server 0d0d0d0d/);
  assert.match(result.refused[0].reason, /no -nexora\.server\.\* key holds/);
});

test('a server deleted after a profile was created is the same refusal at load', () => {
  // The restored-backup case: the profile survived a sync merge, its server did not.
  const result = load([profile()], [serverIds[1]]);
  assert.equal(result.profiles.length, 0);
  assert.match(result.refused[0].reason, new RegExp(`references server ${serverIds[0]}`));
});

test('duplicate serverIds are refused, not silently deduplicated', () => {
  const doubled = profile({ serverIds: [serverIds[0], serverIds[0]] });
  const result = load([doubled]);
  assert.equal(result.profiles.length, 0);
  assert.match(result.refused[0].reason, new RegExp(`lists server ${serverIds[0]} twice`));
});

test('a defaultServerId outside the envelope is refused, and so is one outside the profile itself', () => {
  const alien = profile({ defaultServerId: serverIds[1] });
  const result = load([alien]);
  assert.equal(result.profiles.length, 0);
  assert.match(result.refused[0].reason, /which is not in its own serverIds/);

  const absent = profile({ defaultServerId: '9f9f9f9f-1a85-4e37-9c50-6d3b8f2a4e44' }); // nexora-secret-allow: synthetic canonical id, nothing real behind it
  const result2 = load([absent]);
  assert.equal(result2.profiles.length, 0);
  assert.match(result2.refused[0].reason, /defaults to server 9f9f9f9f/);
});

test('a refusal removes one profile and never the others', () => {
  const good = profile();
  const bad = profile({ id: 'c1d1f2c4-1a85-4e37-9c50-6d3b8f2a4e12', serverIds: ['0d0d0d0d-1a85-4e37-9c50-6d3b8f2a4e33'], defaultServerId: '0d0d0d0d-1a85-4e37-9c50-6d3b8f2a4e33' }); // // nexora-secret-allow: synthetic canonical ids, nothing real behind any of them — the same identities the connector-side tests use
  const result = load([bad, good]);
  assert.deepEqual(result.profiles.map((p) => p.id), [good.id]);
  assert.deepEqual(result.refused.map((r) => r.profileId), [bad.id]);
});

test('the whole canonical batch loads', () => {
  const result = load([profile(), profile({ id: 'c1d1f2c4-1a85-4e37-9c50-6d3b8f2a4e12', serverIds: [...serverIds], defaultServerId: serverIds[1] })]); // nexora-secret-allow: synthetic canonical id, nothing real behind it
  assert.equal(result.profiles.length, 2);
  assert.deepEqual(result.refused, []);
});
