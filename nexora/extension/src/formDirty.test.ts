import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';
import { formOfServer, isDirty } from './formDirty.ts';
import type { SavedServer } from './model.ts';
import type { SavedServer, ServerForm } from './model.ts';

const saved: SavedServer = {
  id: '3f2a1c9e-7b4d-4e60-9a11-0d2c4e6f8a01',
  displayName: 'Stockholm',
  protocol: 'vless',
  address: 'se1.example.net',
  port: 443,
  userIdRef: '11111111-1111-4111-8111-111111111111',
  network: 'tcp',
  security: 'reality',
  flow: 'xtls-rprx-vision',
  reality: { serverName: 'se1.example.net', publicKey: 'A'.repeat(43), shortId: '0011223' },
  source: 'manual',
};

const clean: ServerForm = {
  displayName: 'Stockholm',
  address: 'se1.example.net',
  port: 443,
  userId: '11111111-1111-4111-8111-111111111111',
  serverName: 'se1.example.net',
  publicKey: 'A'.repeat(43),
  shortId: '0011223',
  flow: true,
};

test('a form equal to the applied server is not dirty', () => {
  assert.equal(isDirty(clean, formOfServer(saved)), false);
});

test('editing any field makes the buffer dirty', () => {
  for (const field of ['displayName', 'address', 'serverName', 'publicKey'] as const) {
    assert.equal(isDirty({ ...clean, [field]: clean[field] + 'x' }, saved), true, field);
  }
  assert.equal(isDirty({ ...clean, port: 8443 }, saved), true);
  assert.equal(isDirty({ ...clean, userId: '22222222-2222-4222-8222-222222222222' }, saved), true);
  assert.equal(isDirty({ ...clean, shortId: '' }, saved), true);
  assert.equal(isDirty({ ...clean, flow: false }, saved), true);
});

test('the applied state with no form is never dirty: editing is not applying', () => {
  assert.equal(isDirty(clean, formOfServer(saved)), false);
  assert.equal(isDirty(clean, formOfServer(saved)), false);
});

test('the port compares numerically: "0443" is 443, not a different server', () => {
  assert.equal(isDirty({ ...clean, port: '0443' as unknown as number }, formOfServer(saved)), false);
  assert.equal(isDirty({ ...clean, port: 444 as unknown as number }, formOfServer(saved)), true);
});

test('the panel renders the dirty fact and the applied state side by side, with discard', () => {
  const panel = readFileSync(new URL('./ConnectionPanel.svelte', import.meta.url), 'utf8');
  assert.match(panel, /let dirty = \$state/);
  assert.match(panel, /discardEdits/);
  assert.match(panel, /role="status"/);
  assert.match(panel, /discard/i);
  void formOfServer;
});
