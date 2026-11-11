import { strict as assert } from 'node:assert';
import { test } from 'node:test';
import { Connection, createSelection, siteURL, type Environment, type ProxyValue, type RuntimeRecord, type Selection } from './connection.ts';
import type { ServerForm } from './model.ts';

const form = (): ServerForm => ({ displayName: 'Test', address: '127.0.0.1', port: 443, userId: crypto.randomUUID(), serverName: 'localhost', publicKey: 'A'.repeat(43), shortId: '', flow: true });

function fixture() {
  const events: string[] = [];
  let selection: Selection | null = null;
  let record: RuntimeRecord | null = null;
  const previous: ProxyValue = { mode: 'pac_script', pacScript: { data: 'function FindProxyForURL(){return "DIRECT";}', mandatory: true } };
  let proxy = previous;
  let running = false;
  let denied: string | null = null;
  const messages: { method: string; args: any }[] = [];
  const env: Environment = {
    version: '0.1.0',
    connectNative() {
      let listener: (message: unknown) => void = () => {};
      return {
        onMessage: { addListener(fn) { listener = fn; } }, onDisconnect: { addListener() {} }, disconnect() {},
        postMessage(frame: any) {
          messages.push(frame);
          events.push(frame.method);
          let result: unknown;
          let code: string | null = null;
          switch (frame.method) {
            case 'connector.hello': result = { protocolVersion: 1, minCompatibleProtocolVersion: 1, capabilities: ['chunked.import', 'keystore.ref', 'multi.server'] }; break;
            case 'profile.import': result = { imported: { profileIds: [], serverIds: [] }, rejected: [], secretsExtracted: 1 }; break;
            case 'config.apply':
              if (denied) { code = denied; break; }
              running = true;
              result = { applied: true, configHash: 'a'.repeat(64), inboundPorts: [{ listen: '127.0.0.1', port: 20100, tag: 'in-srv-' + selection!.server.id.replaceAll('-', '').slice(0, 8) }] };
              break;
            case 'core.status': result = { running, configHash: 'a'.repeat(64), coreVersion: '24.11.11' }; break;
            case 'core.stop':
              if (denied) { code = denied; break; }
              running = false; result = { running: false }; break;
          }
          queueMicrotask(() => listener({ type: 'response', inReplyTo: frame.id, ok: !code, result, error: code ? { code, message: 'private cause' } : undefined }));
        },
      };
    },
    async load() { return selection; },
    async save(value) { events.push('save-public'); selection = structuredClone(value); },
    async readRuntime() { return record; },
    async writeRuntime(value) { record = value && structuredClone(value); },
    async getProxy() { return { value: proxy, levelOfControl: 'controllable_by_this_extension' }; },
    async setProxy(value) { events.push('set-proxy'); proxy = value; },
    async restoreProxy(value) { events.push('restore-proxy'); proxy = value; },
    async openSite() { events.push('open-site'); },
  };
  const connection = new Connection(env);
  return { connection, env, events, messages, previous, proxy: () => proxy, stored: () => selection, record: () => record, deny: (code: string | null) => { denied = code; }, kill: () => { running = false; } };
}

test('manual input becomes public intent, never a persisted UUID', () => {
  const f = form();
  const { selection, userId } = createSelection(f);
  assert.equal(userId, f.userId);
  assert.ok(!JSON.stringify(selection).includes(f.userId));
  assert.match(selection.server.userIdRef, /^keystore:\/\/server\/[0-9a-f-]{36}\/user-id$/);
  for (const field of ['port', 'publicKey', 'shortId', 'userId', 'serverName'] as const) {
    assert.throws(() => createSelection({ ...f, [field]: field === 'port' ? 0 : '<bad>' }));
  }
});

test('save, connect, open HTTPS, disconnect, connect again', async () => {
  const f = fixture();
  const input = form();
  assert.equal((await f.connection.action('save', input)).phase, 'disconnected');
  assert.ok(!JSON.stringify(f.stored()).includes(input.userId));
  assert.ok(f.events.indexOf('profile.import') < f.events.indexOf('save-public'));
  assert.equal((await f.connection.action('connect')).phase, 'connected');
  assert.ok(f.events.indexOf('config.apply') < f.events.indexOf('set-proxy'));
  assert.equal(f.proxy().rules?.singleProxy?.port, 20100);
  assert.deepEqual(f.proxy().rules?.bypassList, ['<-loopback>']);
  assert.equal((await f.connection.action('open-site', { url: 'https://localhost/' })).phase, 'connected');
  assert.ok(f.events.includes('open-site'));
  assert.equal((await f.connection.action('disconnect')).phase, 'disconnected');
  assert.deepEqual(f.proxy(), f.previous);
  assert.equal(f.record(), null);
  assert.ok(f.events.indexOf('core.stop') < f.events.indexOf('restore-proxy'));
  assert.equal((await f.connection.action('connect')).phase, 'connected');
  f.connection.client.stop();
});

test('missing trusted Core does not modify browser proxy', async () => {
  const f = fixture();
  await f.connection.action('save', form());
  f.deny('IntegrityMismatch');
  const view = await f.connection.action('connect');
  assert.equal(view.phase, 'disconnected');
  assert.match(view.error!, /receipt/);
  assert.deepEqual(f.proxy(), f.previous);
  assert.ok(!f.events.includes('set-proxy'));
  f.connection.client.stop();
});

test('failed session persistence stops the newly applied Core without changing proxy', async () => {
  const f = fixture();
  await f.connection.action('save', form());
  f.env.writeRuntime = async () => { throw new Error('session store failed'); };
  const view = await f.connection.action('connect');
  assert.equal(view.phase, 'disconnected');
  assert.ok(f.events.includes('core.stop'));
  assert.ok(!f.events.includes('set-proxy'));
  assert.deepEqual(f.proxy(), f.previous);
  assert.equal(f.record(), null);
  f.connection.client.stop();
});

test('Core death and refused stop keep the proxy, no direct fallback', async () => {
  const f = fixture();
  await f.connection.action('save', form());
  await f.connection.action('connect');
  f.kill();
  assert.equal((await f.connection.action('status')).phase, 'blocked');
  const opens = f.events.filter((x) => x === 'open-site').length;
  await f.connection.action('open-site', { url: 'https://localhost/' });
  assert.equal(f.events.filter((x) => x === 'open-site').length, opens);
  f.deny('Timeout');
  assert.equal((await f.connection.action('disconnect')).phase, 'blocked');
  assert.equal(f.proxy().mode, 'fixed_servers');
  assert.ok(!f.events.includes('restore-proxy'));
  f.connection.client.stop();
});

test('unexpected proxy changes are reasserted and lost control is reported', async () => {
  const f = fixture();
  await f.connection.action('save', form());
  await f.connection.action('connect');
  await f.connection.preserveProxy({ mode: 'direct' });
  assert.equal(f.events.filter((event) => event === 'set-proxy').length, 2);
  f.env.setProxy = async () => { throw new Error('policy owns proxy'); };
  await f.connection.preserveProxy({ mode: 'direct' });
  assert.equal(f.connection.snapshot().phase, 'blocked');
  assert.match(f.connection.snapshot().error!, /не подтверждена/);
  const opens = f.events.filter((event) => event === 'open-site').length;
  await f.connection.action('open-site', { url: 'https://localhost/' });
  assert.equal(f.events.filter((event) => event === 'open-site').length, opens);
  f.connection.client.stop();
});

test('failed proxy restoration retains the blocking proxy and active record', async () => {
  const f = fixture();
  await f.connection.action('save', form());
  await f.connection.action('connect');
  f.env.restoreProxy = async () => { throw new Error('restore failed'); };
  assert.equal((await f.connection.action('disconnect')).phase, 'blocked');
  assert.equal(f.proxy().mode, 'fixed_servers');
  assert.equal(f.record()?.active, true);
  f.connection.client.stop();
});

test('failed session cleanup after restoration never records an active tunnel', async () => {
  const f = fixture();
  await f.connection.action('save', form());
  await f.connection.action('connect');
  const write = f.env.writeRuntime;
  f.env.writeRuntime = async (record) => {
    if (!record) throw new Error('cleanup failed');
    await write(record);
  };
  const view = await f.connection.action('disconnect');
  assert.equal(view.phase, 'disconnected');
  assert.match(view.error!, /cleanup/);
  assert.deepEqual(f.proxy(), f.previous);
  assert.equal(f.record()?.active, false);
  f.connection.client.stop();
});

test('open-site refuses non-HTTPS, credentials and malformed input', () => {
  for (const url of ['http://localhost/', 'javascript:alert(1)', 'file:///etc/passwd', 'https://user:pass@localhost/', 'invalid']) assert.throws(() => siteURL({ url }));
  assert.equal(siteURL({ url: 'https://localhost' }), 'https://localhost/');
});
