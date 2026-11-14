import { strict as assert } from 'node:assert';
import { test } from 'node:test';
import { Connection, createSelection, createSubscription, siteURL, type Environment, type ProxyValue, type RuntimeRecord, type Selection } from './connection.ts';
import type { SavedServer, SavedSubscription, ServerForm, SubscriptionResult } from './model.ts';

const form = (): ServerForm => ({ displayName: 'Test', address: '127.0.0.1', port: 443, userId: crypto.randomUUID(), serverName: 'localhost', publicKey: 'A'.repeat(43), shortId: '', flow: true });

function subscriptionServers(id: string, count = 3): SavedServer[] {
  return Array.from({ length: count }, (_, index) => {
    const server = createSelection({ ...form(), displayName: `Сервер ${index + 1}`, address: `tcp${index + 1}.example.test` }).selection.server;
    return { ...server, source: 'subscription', subscriptionId: id };
  });
}

const subscriptionURL = 'https://feed.example.test/private-path-secret?token=query-secret#fragment-secret';

function fixture() {
  const events: string[] = [];
  let selection: Selection | null = null;
  let subscription: SavedSubscription | null = null;
  let servers: SavedServer[] = [];
  let refreshResponse: (source: SavedSubscription) => SubscriptionResult | Promise<SubscriptionResult> = (source) => {
    const list = subscriptionServers(source.id);
    return { refreshed: [{ subscriptionId: source.id, format: 'vless-base64', sha256: 'b'.repeat(64), serverCount: list.length, servers: list }], unchanged: [], failed: [], skipped: [] };
  };
  let probeResponse: (serverId: string) => unknown | Promise<unknown> = (serverId) => ({
    serverId, overall: 'pass', layers: [
      { layer: 'dns', state: 'pass', rttMs: 1.25 },
      { layer: 'tcp', state: 'pass', rttMs: 2 },
      { layer: 'core-outbound', state: 'pass', rttMs: 3 },
      { layer: 'proxied-http', state: 'pass', rttMs: 4 },
    ], egressIp: null, matchesExpectedEgress: null,
  });
  let record: RuntimeRecord | null = null;
  const previous: ProxyValue = { mode: 'pac_script', pacScript: { data: 'function FindProxyForURL(){return "DIRECT";}', mandatory: true } };
  let proxy = previous;
  let running = false;
  let coreProfile: string | null = null;
  let coreInbounds: { listen: string; port: number; tag: string }[] = [];
  let statusOverride: Record<string, unknown> = {};
  let controlled = false;
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
            case 'connector.hello': result = { protocolVersion: 1, minCompatibleProtocolVersion: 1, capabilities: ['chunked.import', 'keystore.ref', 'multi.server', 'health.probes'] }; break;
            case 'profile.import':
              if (denied) { code = denied; break; }
              result = { imported: { profileIds: [], serverIds: [] }, rejected: [], secretsExtracted: 1 }; break;
            case 'subscription.refresh':
              void Promise.resolve(refreshResponse(frame.args.sources[0])).then((result) => listener({ type: 'response', inReplyTo: frame.id, ok: true, result }));
              return;
            case 'config.apply':
              if (denied) { code = denied; break; }
              running = true;
              coreProfile = frame.args.profileIds[0];
              coreInbounds = [{ listen: '127.0.0.1', port: 20100, tag: 'in-srv-' + frame.args.intent.servers[0].id.replaceAll('-', '').slice(0, 8) }];
              result = { applied: true, configHash: 'a'.repeat(64), inboundPorts: coreInbounds };
              break;
            case 'server.test':
              if (denied) { code = denied; break; }
              void Promise.resolve(probeResponse(frame.args.serverId)).then((result) => listener({ type: 'response', inReplyTo: frame.id, ok: true, result }));
              return;
            case 'core.status': result = { running, configHash: 'a'.repeat(64), coreVersion: '24.11.11', selectedProfileId: running ? coreProfile : null, inboundPorts: running ? coreInbounds : [], ...statusOverride }; break;
            case 'core.stop':
              if (denied) { code = denied; break; }
              if (running && frame.args.expectedConfigHash && frame.args.expectedConfigHash !== (statusOverride.configHash ?? 'a'.repeat(64))) { code = 'Busy'; break; }
              running = false; result = { running: false }; break;
          }
          queueMicrotask(() => listener({ type: 'response', inReplyTo: frame.id, ok: !code, result, error: code ? { code, message: 'private cause' } : undefined }));
        },
      };
    },
    async load() { return selection; },
    async save(value) { events.push('save-public'); selection = structuredClone(value); },
    async loadSubscription() { return structuredClone(subscription); },
    async loadServers(ids) { return structuredClone(servers.filter((server) => ids.includes(server.id))); },
    async saveSubscription(source, list, selected) {
      assert.notEqual(source.format, 'auto');
      events.push('save-subscription-public');
      subscription = structuredClone(source);
      servers = structuredClone(list);
      selection = structuredClone(selected);
    },
    async readRuntime() { return record; },
    async writeRuntime(value) { record = value && structuredClone(value); },
    async getProxy() { return { value: proxy, levelOfControl: controlled ? 'controlled_by_this_extension' : 'controllable_by_this_extension' }; },
    async setProxy(value) { events.push('set-proxy'); proxy = value; controlled = true; },
    async restoreProxy(value) { events.push('restore-proxy'); proxy = value; },
    async openSite() { events.push('open-site'); },
  };
  const connection = new Connection(env);
  return {
    connection, env, events, messages, previous, proxy: () => proxy, stored: () => selection, record: () => record,
    subscription: () => subscription, servers: () => servers,
    refreshWith: (response: typeof refreshResponse) => { refreshResponse = response; },
    probeWith: (response: typeof probeResponse) => { probeResponse = response; },
    statusWith: (value: Record<string, unknown>) => { statusOverride = value; },
    deny: (code: string | null) => { denied = code; }, kill: () => { running = false; },
  };
}

test('manual input becomes public intent, never a persisted UUID', () => {
  const f = form();
  const { selection, userId } = createSelection(f);
  assert.equal(userId, f.userId);
  assert.ok(!JSON.stringify(selection).includes(f.userId));
  assert.match(selection.server.userIdRef, /^keystore:\/\/server\/[0-9a-f-]{36}\/user-id$/);
  assert.equal(createSelection({ ...f, shortId: '0011223344556677' }).selection.server.reality?.shortId, '0011223344556677');
  assert.throws(() => createSelection({ ...f, shortId: '001122334455667788' }));
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

test('an applied response without a valid hash cannot stop an unknown Core', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  const request = f.connection.client.request.bind(f.connection.client);
  f.connection.client.request = async (method, args) => {
    const result = await request(method, args);
    return method === 'config.apply' ? { ...(result as object), configHash: 'not-a-hash' } : result;
  };
  assert.equal((await f.connection.action('connect')).phase, 'blocked');
  assert.ok(!f.events.includes('core.stop'));
  assert.ok(!f.events.includes('set-proxy'));
  assert.deepEqual(f.proxy(), f.previous);
});

test('open-site refuses non-HTTPS, credentials and malformed input', () => {
  for (const url of ['http://localhost/', 'javascript:alert(1)', 'file:///etc/passwd', 'https://user:pass@localhost/', 'invalid']) assert.throws(() => siteURL({ url }));
  assert.equal(siteURL({ url: 'https://localhost' }), 'https://localhost/');
});

test('subscription URL is transient; public source contains only the origin', () => {
  const { source, url } = createSubscription({ url: subscriptionURL });
  assert.equal(url, subscriptionURL);
  assert.equal(source.displayName, 'feed.example.test');
  assert.equal(source.url, 'https://feed.example.test/');
  assert.equal(source.urlRef, `keystore://subscription/${source.id}/url`);
  assert.equal(source.format, 'auto');
  assert.deepEqual(source.limits, { maxBytes: 2097152, maxRedirects: 3, timeoutMs: 20000, maxServers: 512 });
  assert.deepEqual(source.schedule, { intervalSeconds: 3600 });
  assert.deepEqual(source.status, { consecutiveFailures: 0 });
  assert.deepEqual(source.serverIds, []);
  assert.equal(source.keepLastGood, true);
  for (const secret of ['private-path-secret', 'query-secret', 'fragment-secret']) assert.ok(!JSON.stringify(source).includes(secret));
  for (const url of ['http://feed.example.test/', 'https://user:password@feed.example.test/', 'bad']) assert.throws(() => createSubscription({ url }));
});

test('subscribe, explicitly select, connect only the chosen server, open HTTPS and disconnect', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  const view = await f.connection.action('subscribe', { url: subscriptionURL });
  assert.equal(view.phase, 'disconnected');
  assert.equal(view.pending, false);
  assert.equal(view.servers.length, 3);
  assert.equal(view.server?.id, view.servers[0]!.id);
  assert.equal(view.subscription?.temporary, false);
  assert.equal(view.subscription?.serverCount, 3);
  assert.ok(!f.events.includes('config.apply'));
  const imported = f.messages.find((message) => message.method === 'profile.import')!.args;
  assert.deepEqual(imported.profiles, []);
  assert.equal(imported.subscriptions[0].format, 'auto');
  assert.deepEqual(imported.credentials, [{ entity: 'subscription', entityId: view.subscription!.id, field: 'url', value: subscriptionURL }]);
  const refresh = f.messages.find((message) => message.method === 'subscription.refresh')!.args;
  assert.equal(refresh.sources[0].url, 'https://feed.example.test/');
  assert.equal(refresh.force, true);
  assert.ok(f.events.indexOf('profile.import') < f.events.indexOf('subscription.refresh'));
  assert.ok(f.events.indexOf('subscription.refresh') < f.events.indexOf('save-subscription-public'));
  assert.equal(f.subscription()?.format, 'vless-base64');
  assert.deepEqual(f.subscription()?.serverIds, view.servers.map((server) => server.id));
  assert.equal(f.subscription()?.status.lastSha256, 'b'.repeat(64));
  assert.equal(f.subscription()?.status.serverCount, 3);
  assert.ok(f.subscription()?.status.lastSuccessAt);
  const chosen = view.servers[2]!;
  assert.equal((await f.connection.action('select', { serverId: chosen.id })).server?.id, chosen.id);
  assert.deepEqual(f.stored()?.profile.serverIds, [chosen.id]);
  assert.equal(f.stored()?.profile.defaultServerId, chosen.id);
  assert.ok(!f.events.includes('config.apply'));
  assert.equal((await f.connection.action('connect')).phase, 'connected');
  const intent = f.messages.find((message) => message.method === 'config.apply')!.args.intent;
  assert.deepEqual(intent.servers, [chosen]);
  assert.deepEqual(intent.profiles[0].serverIds, [chosen.id]);
  assert.equal(intent.profiles[0].failover.enabled, false);
  for (const secret of ['private-path-secret', 'query-secret', 'fragment-secret']) {
    assert.ok(!JSON.stringify([view, f.connection.snapshot(), f.subscription(), f.servers(), f.stored(), intent, refresh]).includes(secret));
  }
  assert.equal((await f.connection.action('open-site', { url: 'https://example.test/' })).error, null);
  assert.ok(f.events.includes('open-site'));
  assert.equal((await f.connection.action('disconnect')).phase, 'disconnected');
  assert.deepEqual(f.proxy(), f.previous);
});

test('HTTP 404 refresh and partial refresh preserve last-good list and selection byte for byte', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  const added = await f.connection.action('subscribe', { url: subscriptionURL });
  await f.connection.action('select', { serverId: added.servers[1]!.id });
  const stored = JSON.stringify(f.stored());
  const servers = JSON.stringify(f.servers());
  const oldStatus = structuredClone(f.subscription()!.status);
  for (const [code, partial] of [['AuthFailed', false], ['SchemaMismatch', true]] as const) {
    f.refreshWith((source) => ({ refreshed: [], unchanged: [], failed: [{
      subscriptionId: source.id, code, retryable: false, consecutiveFailures: source.status.consecutiveFailures + 1,
      nextAttemptAt: '2030-01-01T00:00:00Z', ...(partial ? { partialServers: subscriptionServers(source.id, 2) } : {}),
    }], skipped: [] }));
    const view = await f.connection.action('refresh');
    assert.equal(JSON.stringify(f.stored()), stored);
    assert.equal(JSON.stringify(f.servers()), servers);
    assert.equal(JSON.stringify(view.servers), servers);
    assert.equal(view.server?.id, added.servers[1]!.id);
    assert.match(view.error!, /Прежний список/);
    assert.match(view.warning!, partial ? /не поддерживается/ : /404/);
    assert.equal(f.subscription()?.status.lastSha256, oldStatus.lastSha256);
    assert.equal(f.subscription()?.status.lastSuccessAt, oldStatus.lastSuccessAt);
    assert.deepEqual(f.subscription()?.serverIds, added.servers.map((server) => server.id));
    assert.equal(f.subscription()?.status.nextAttemptAt, '2030-01-01T00:00:00Z');
    assert.equal(f.subscription()?.status.lastError?.code, code);
  }
  assert.equal(f.subscription()?.status.consecutiveFailures, 2);
  assert.equal((await f.connection.action('connect')).phase, 'connected');
  assert.deepEqual(f.messages.find((message) => message.method === 'config.apply')!.args.intent.servers, [added.servers[1]]);
});

test('initial partial import exposes supported TCP servers without inventing a stored format', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  f.refreshWith((source) => ({ refreshed: [], unchanged: [], failed: [{
    subscriptionId: source.id, code: 'SchemaMismatch', retryable: false, consecutiveFailures: 1, partialServers: subscriptionServers(source.id, 18),
  }], skipped: [] }));
  const view = await f.connection.action('subscribe', { url: subscriptionURL });
  assert.equal(view.error, null);
  assert.equal(view.servers.length, 18);
  assert.ok(view.servers.every((server) => server.network === 'tcp'));
  assert.match(view.warning!, /Часть записей.*не поддерживается/);
  assert.match(view.warning!, /временные/);
  assert.equal(view.subscription?.temporary, true);
  assert.equal(f.subscription(), null);
  assert.equal(f.stored(), null);
  assert.ok(!f.events.includes('save-subscription-public'));
  const chosen = view.servers[17]!;
  await f.connection.action('select', { serverId: chosen.id });
  assert.equal(f.connection.snapshot().server?.id, chosen.id);
  assert.equal((await f.connection.action('connect')).phase, 'connected');
  assert.deepEqual(f.messages.find((message) => message.method === 'config.apply')!.args.intent.servers, [chosen]);
  assert.equal((await f.connection.action('disconnect')).phase, 'disconnected');
  const unchangedList = JSON.stringify(f.connection.snapshot().servers);
  f.refreshWith((source) => ({ refreshed: [], unchanged: [], failed: [{
    subscriptionId: source.id, code: 'SchemaMismatch', retryable: false, consecutiveFailures: 2, partialServers: subscriptionServers(source.id, 1),
  }] }));
  const failed = await f.connection.action('refresh');
  assert.equal(failed.server?.id, chosen.id);
  assert.equal(JSON.stringify(failed.servers), unchangedList);
  f.refreshWith((source) => ({ refreshed: [{ subscriptionId: source.id, format: 'v2ray-json', sha256: 'c'.repeat(64), serverCount: 18, servers: view.servers }], unchanged: [], failed: [] }));
  const confirmed = await f.connection.action('refresh');
  assert.equal(confirmed.server?.id, chosen.id);
  assert.equal(confirmed.subscription?.temporary, false);
  assert.equal(confirmed.warning, null);
  assert.equal(f.subscription()?.format, 'v2ray-json');
  assert.equal(f.subscription()?.status.consecutiveFailures, 0);
  assert.equal(f.subscription()?.status.lastError, null);
  assert.equal(f.subscription()?.status.nextAttemptAt, undefined);
  assert.equal((await f.connection.action('connect')).phase, 'connected');
  assert.deepEqual(f.messages.find((message) => message.method === 'config.apply')!.args.intent.servers, [chosen]);
});

test('partial import with confirmed format persists supported list, selection and unsupported warning', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  f.refreshWith((source) => ({ refreshed: [], unchanged: [], failed: [{
    subscriptionId: source.id, code: 'SchemaMismatch', retryable: false, consecutiveFailures: 1,
    format: 'v2ray-json', rejectedCount: 2, partialServers: subscriptionServers(source.id, 18),
  }] }));
  const view = await f.connection.action('subscribe', { url: subscriptionURL });
  assert.equal(view.error, null);
  assert.equal(view.subscription?.temporary, false);
  assert.equal(view.servers.length, 18);
  assert.match(view.warning!, /Неподдерживаемых записей: 2/);
  assert.ok(!view.warning!.includes('временные'));
  assert.equal(f.subscription()?.format, 'v2ray-json');
  assert.equal(f.subscription()?.status.consecutiveFailures, 1);
  assert.equal(f.subscription()?.status.lastError?.code, 'SchemaMismatch');
  assert.equal(f.subscription()?.status.lastSha256, undefined);
  assert.equal(f.subscription()?.status.lastSuccessAt, undefined);
  assert.deepEqual(f.subscription()?.serverIds, view.servers.map((server) => server.id));
  const chosen = view.servers[17]!;
  await f.connection.action('select', { serverId: chosen.id });
  const selected = JSON.stringify(f.stored());
  const list = JSON.stringify(f.servers());
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  const loaded = await restarted.action('status');
  assert.equal(loaded.server?.id, chosen.id);
  assert.equal(JSON.stringify(loaded.servers), list);
  assert.match(loaded.warning!, /неподдерживаемые/);
  f.refreshWith((source) => ({ refreshed: [], unchanged: [], failed: [{
    subscriptionId: source.id, code: 'SchemaMismatch', retryable: false, consecutiveFailures: 2,
    format: 'clash-yaml', rejectedCount: 1, partialServers: subscriptionServers(source.id, 1),
  }] }));
  await restarted.action('refresh');
  assert.equal(JSON.stringify(f.stored()), selected);
  assert.equal(JSON.stringify(f.servers()), list);
  assert.equal(f.subscription()?.format, 'v2ray-json');
  assert.equal((await restarted.action('connect')).phase, 'connected');
  assert.deepEqual(f.messages.find((message) => message.method === 'config.apply')!.args.intent.servers, [chosen]);
});

test('a new partial subscription replaces the active list without mixing source owners', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  const first = await f.connection.action('subscribe', { url: subscriptionURL });
  await f.connection.action('select', { serverId: first.servers[1]!.id });
  f.refreshWith((source) => ({ refreshed: [], unchanged: [], failed: [{
    subscriptionId: source.id, code: 'SchemaMismatch', retryable: false, consecutiveFailures: 1,
    format: 'vless-base64', rejectedCount: 1, partialServers: subscriptionServers(source.id, 2),
  }] }));
  const second = await f.connection.action('subscribe', { url: 'https://second.example.test/private-source' });
  assert.equal(second.error, null);
  assert.notEqual(second.subscription?.id, first.subscription?.id);
  assert.equal(second.servers.length, 2);
  assert.ok(second.servers.every((server) => server.subscriptionId === second.subscription!.id));
  assert.deepEqual(f.subscription()?.serverIds, second.servers.map((server) => server.id));
  const chosen = second.servers[1]!;
  assert.equal((await f.connection.action('select', { serverId: chosen.id })).error, null);
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  const loaded = await restarted.action('status');
  assert.equal(loaded.subscription?.id, second.subscription?.id);
  assert.equal(loaded.server?.id, chosen.id);
  assert.deepEqual(loaded.servers, second.servers);
  const refreshed = await restarted.action('refresh');
  assert.equal(refreshed.server?.id, chosen.id);
  assert.deepEqual(refreshed.servers, second.servers);
  assert.equal((await restarted.action('connect')).phase, 'connected');
  assert.deepEqual(f.messages.find((message) => message.method === 'config.apply')!.args.intent.servers, [chosen]);
});

test('unchanged refresh updates only attempt time, preserving source status, servers and selection', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('subscribe', { url: subscriptionURL });
  const source = structuredClone(f.subscription()!);
  const servers = JSON.stringify(f.servers());
  const selected = JSON.stringify(f.stored());
  f.refreshWith((source) => ({ refreshed: [], unchanged: [source.id], failed: [], skipped: [] }));
  assert.equal((await f.connection.action('refresh')).error, null);
  const after = structuredClone(f.subscription()!);
  assert.ok(after.status.lastAttemptAt);
  after.status.lastAttemptAt = source.status.lastAttemptAt;
  assert.deepEqual(after, source);
  assert.equal(JSON.stringify(f.servers()), servers);
  assert.equal(JSON.stringify(f.stored()), selected);
});

test('unknown server selection and malformed payload cannot change saved selection', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('subscribe', { url: subscriptionURL });
  const stored = JSON.stringify(f.stored());
  const saves = f.events.filter((event) => event === 'save-subscription-public').length;
  for (const payload of [undefined, {}, { serverId: crypto.randomUUID() }, { serverId: 0 }, { serverId: subscriptionURL }]) {
    const view = await f.connection.action('select', payload);
    assert.match(view.error!, /доступного списка/);
    assert.ok(!JSON.stringify(view).includes('query-secret'));
    assert.equal(JSON.stringify(f.stored()), stored);
  }
  assert.equal(f.events.filter((event) => event === 'save-subscription-public').length, saves);
});

test('subscribe, refresh and select are refused while connected or blocked', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  const added = await f.connection.action('subscribe', { url: subscriptionURL });
  await f.connection.action('connect');
  const selected = JSON.stringify(f.stored());
  const serverList = JSON.stringify(f.servers());
  const requests = f.messages.filter((message) => ['profile.import', 'subscription.refresh'].includes(message.method)).length;
  for (const phase of ['connected', 'blocked']) {
    if (phase === 'blocked') { f.kill(); await f.connection.action('status'); }
    for (const action of ['subscribe', 'refresh', 'select'] as const) {
      const view = await f.connection.action(action, action === 'subscribe' ? { url: subscriptionURL } : { serverId: added.servers[2]!.id });
      assert.equal(view.phase, phase);
      assert.match(view.error!, /Сначала отключитесь/);
    }
    assert.equal(JSON.stringify(f.stored()), selected);
    assert.equal(JSON.stringify(f.servers()), serverList);
    assert.equal(f.messages.filter((message) => ['profile.import', 'subscription.refresh'].includes(message.method)).length, requests);
  }
  await f.connection.action('disconnect');
  assert.equal((await f.connection.action('select', { serverId: added.servers[2]!.id })).error, null);
});

test('pending refresh blocks overlapping subscription edits and reports pending in status', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  const added = await f.connection.action('subscribe', { url: subscriptionURL });
  let finish!: (result: SubscriptionResult) => void;
  let started!: () => void;
  const began = new Promise<void>((resolve) => { started = resolve; });
  f.refreshWith(() => new Promise<SubscriptionResult>((resolve) => { finish = resolve; started(); }));
  const refreshing = f.connection.action('refresh');
  await began;
  assert.equal((await f.connection.action('status')).pending, true);
  for (const action of ['subscribe', 'refresh', 'select'] as const) {
    const view = await f.connection.action(action, action === 'subscribe' ? { url: subscriptionURL } : { serverId: added.servers[1]!.id });
    assert.match(view.error!, /Дождитесь/);
    assert.equal(view.pending, true);
    assert.equal(view.server?.id, added.server?.id);
  }
  finish({ refreshed: [], unchanged: [added.subscription!.id], failed: [] });
  assert.equal((await refreshing).pending, false);
});

test('manual save clears controller subscription association without deleting the old library', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  const added = await f.connection.action('subscribe', { url: subscriptionURL });
  const library = JSON.stringify([f.subscription(), f.servers()]);
  const manual = await f.connection.action('save', form());
  assert.equal(manual.subscription, null);
  assert.equal(manual.warning, null);
  assert.equal(manual.servers.length, 1);
  assert.equal(manual.servers[0]!.source, 'manual');
  assert.equal(JSON.stringify([f.subscription(), f.servers()]), library);
  assert.match((await f.connection.action('refresh')).error!, /добавьте подписку/);
  assert.match((await f.connection.action('select', { serverId: added.servers[0]!.id })).error!, /доступного списка/);
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  assert.equal((await restarted.action('status')).subscription, null);
});

test('public subscription library and selected server survive controller restart', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  const added = await f.connection.action('subscribe', { url: subscriptionURL });
  await f.connection.action('select', { serverId: added.servers[2]!.id });
  f.refreshWith((source) => ({ refreshed: [], unchanged: [], failed: [{ subscriptionId: source.id, code: 'AuthFailed', retryable: false, consecutiveFailures: 1 }] }));
  await f.connection.action('refresh');
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  const view = await restarted.action('status');
  assert.equal(view.server?.id, added.servers[2]!.id);
  assert.deepEqual(view.servers, added.servers);
  assert.equal(view.subscription?.consecutiveFailures, 1);
  assert.match(view.warning!, /404/);
});

test('subscription TCP servers keep none, TLS and REALITY settings in chosen-only intent', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  f.refreshWith((source) => {
    const list = subscriptionServers(source.id);
    delete list[0]!.reality;
    list[0]!.security = 'none';
    list[0]!.flow = '';
    delete list[1]!.reality;
    list[1]!.security = 'tls';
    list[1]!.tls = { serverName: 'tls.example.test', alpn: ['h2', 'http/1.1'], fingerprint: 'chrome', insecure: false };
    return { refreshed: [{ subscriptionId: source.id, format: 'vless-uri-list', sha256: 'e'.repeat(64), serverCount: list.length, servers: list }], unchanged: [], failed: [] };
  });
  const added = await f.connection.action('subscribe', { url: subscriptionURL });
  assert.equal(f.subscription()?.format, 'vless-uri-list');
  assert.deepEqual(added.servers.map((server) => server.security), ['none', 'tls', 'reality']);
  for (const server of added.servers) {
    await f.connection.action('select', { serverId: server.id });
    assert.equal((await f.connection.action('connect')).phase, 'connected');
    assert.deepEqual(f.messages.filter((message) => message.method === 'config.apply').at(-1)!.args.intent.servers, [server]);
    assert.equal((await f.connection.action('disconnect')).phase, 'disconnected');
  }
});

test('subscription WebSocket keeps public path, host and TLS SNI through selection and restart', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  f.refreshWith((source) => {
    const list = subscriptionServers(source.id, 2);
    for (const server of list) {
      delete server.reality;
      server.network = 'ws';
      server.flow = '';
      server.security = 'none';
      server.ws = { path: '/socket%2Fcase?route=one', host: 'ws.example.test' };
    }
    list[1]!.security = 'tls';
    list[1]!.tls = { serverName: 'tls.example.test', alpn: ['http/1.1'], insecure: false };
    Object.assign(list[1]!.ws!, { userId: 'private-access-secret', raw: subscriptionURL });
    return { refreshed: [{ subscriptionId: source.id, format: 'vless-base64', sha256: 'e'.repeat(64), serverCount: list.length, servers: list }], unchanged: [], failed: [] };
  });
  const added = await f.connection.action('subscribe', { url: subscriptionURL });
  assert.equal(added.error, null);
  assert.deepEqual(added.servers[1]!.ws, { path: '/socket%2Fcase?route=one', host: 'ws.example.test' });
  assert.ok(!JSON.stringify([added, f.stored(), f.servers()]).includes('private-access-secret'));
  assert.ok(!JSON.stringify(added).includes('query-secret'));
  for (const server of added.servers) {
    await f.connection.action('select', { serverId: server.id });
    assert.equal((await f.connection.action('connect')).phase, 'connected');
    assert.deepEqual(f.messages.filter((message) => message.method === 'config.apply').at(-1)!.args.intent.servers, [server]);
    assert.equal((await f.connection.action('disconnect')).phase, 'disconnected');
  }
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  const loaded = await restarted.action('status');
  assert.equal(loaded.server?.id, added.servers[1]!.id);
  assert.deepEqual(loaded.servers, added.servers);
  assert.equal(loaded.servers[1]!.tls!.serverName, 'tls.example.test');
});

test('unsupported WebSocket combinations leave the subscription library unchanged', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('subscribe', { url: subscriptionURL });
  const stored = JSON.stringify([f.subscription(), f.servers(), f.stored()]);
  for (const change of [
    { security: 'reality', flow: '' }, { security: 'none', flow: 'xtls-rprx-vision' },
    { security: 'none', flow: '', ws: undefined },
    { security: 'none', flow: '', ws: { headersRef: 'keystore://server/headers', headersKeys: ['Authorization'] } },
  ]) {
    f.refreshWith((source) => {
      const server = { ...subscriptionServers(source.id, 1)[0]!, network: 'ws', ws: {}, ...change } as SavedServer;
      return { refreshed: [{ subscriptionId: source.id, format: 'vless-uri-list', sha256: 'e'.repeat(64), serverCount: 1, servers: [server] }], unchanged: [], failed: [] };
    });
    assert.match((await f.connection.action('refresh')).error!, /неподдерживаемый сервер/);
    assert.equal(JSON.stringify([f.subscription(), f.servers(), f.stored()]), stored);
  }
});

test('request-level refresh errors persist valid status without leaking private details or replacing data', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('subscribe', { url: subscriptionURL });
  const selected = JSON.stringify(f.stored());
  const list = JSON.stringify(f.servers());
  const request = f.connection.client.request.bind(f.connection.client);
  for (const code of ['Timeout', 'Busy', 'HandshakeFailed']) {
    f.connection.client.request = async (method, args) => {
      if (method === 'subscription.refresh') throw Object.assign(new Error(subscriptionURL), { code });
      return request(method, args);
    };
    const view = await f.connection.action('refresh');
    assert.ok(view.error);
    assert.ok(!JSON.stringify([view, f.subscription()]).includes('query-secret'));
    assert.equal(JSON.stringify(f.stored()), selected);
    assert.equal(JSON.stringify(f.servers()), list);
    assert.equal(f.subscription()?.status.lastError?.code, code === 'Busy' ? 'Internal' : code === 'HandshakeFailed' ? 'NetworkUnreachable' : code);
  }
});

test('subscription errors and unknown result fields never expose raw URL secrets', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  f.deny('AuthFailed');
  const failed = await f.connection.action('subscribe', { url: subscriptionURL });
  assert.ok(failed.error);
  assert.equal(f.subscription(), null);
  assert.ok(!JSON.stringify(failed).includes('query-secret'));
  f.deny(null);
  f.refreshWith((source) => ({ refreshed: [{ subscriptionId: source.id, sha256: 'd'.repeat(64), format: 'clash-yaml', serverCount: 1,
    servers: subscriptionServers(source.id, 1).map((server) => ({ ...server, url: subscriptionURL, userId: 'private-access-secret', reality: { ...server.reality!, raw: subscriptionURL } })),
  }], unchanged: [], failed: [] }));
  const view = await f.connection.action('subscribe', { url: subscriptionURL });
  assert.equal(view.error, null);
  assert.equal(f.subscription()?.format, 'clash-yaml');
  for (const secret of ['private-path-secret', 'query-secret', 'fragment-secret', 'private-access-secret']) {
    assert.ok(!JSON.stringify([view, f.subscription(), f.servers(), f.stored()]).includes(secret));
  }
  const mutated = f.connection.snapshot();
  mutated.servers[0]!.displayName = 'Changed outside';
  assert.notEqual(f.connection.snapshot().servers[0]!.displayName, 'Changed outside');
});

async function readyStatus(connection: Connection) {
  const deadline = Date.now() + 1000;
  let view = await connection.action('status');
  while (view.connector !== 'ready' && Date.now() < deadline) {
    await new Promise((done) => setTimeout(done, 5));
    view = await connection.action('status');
  }
  assert.equal(view.connector, 'ready');
  return connection.action('status');
}

test('controller restart adopts only the proven live session and restores the original proxy', async (t) => {
  const f = fixture();
  await f.connection.action('save', form());
  assert.equal((await f.connection.action('connect')).phase, 'connected');
  f.connection.client.stop();
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  assert.equal((await readyStatus(restarted)).phase, 'connected');
  assert.equal(f.messages.filter((m) => m.method === 'config.apply').length, 1);
  assert.equal((await restarted.action('disconnect')).phase, 'disconnected');
  assert.deepEqual(f.proxy(), f.previous);
  assert.equal(f.record(), null);
  assert.equal(f.messages.find((m) => m.method === 'core.stop')!.args.expectedConfigHash, 'a'.repeat(64));
});

test('restart refuses stopped, foreign, missing and ambiguous Core proofs without reapplying', async (t) => {
  for (const override of [
    { running: false }, { configHash: 'b'.repeat(64) }, { selectedProfileId: crypto.randomUUID() },
    { selectedProfileId: null }, { inboundPorts: [] },
    { inboundPorts: [{ listen: '0.0.0.0', port: 20100, tag: 'wrong' }] },
  ]) {
    const f = fixture();
    await f.connection.action('save', form());
    await f.connection.action('connect');
    f.connection.client.stop();
    f.statusWith(override);
    const restarted = new Connection(f.env);
    t.after(() => restarted.client.stop());
    assert.equal((await readyStatus(restarted)).phase, 'blocked');
    assert.equal(f.proxy().mode, 'fixed_servers');
    assert.equal(f.messages.filter((m) => m.method === 'config.apply').length, 1);
    assert.ok(!f.events.includes('restore-proxy'));
    assert.equal((await restarted.action('open-site', { url: 'https://example.test/' })).phase, 'blocked');
    assert.ok(!f.events.includes('open-site'));
  }
});

test('old or lost runtime proof cannot enable direct traffic or stop an unknown session', async (t) => {
  for (const lost of [false, true]) {
    const f = fixture();
    await f.connection.action('save', form());
    await f.connection.action('connect');
    f.connection.client.stop();
    if (lost) f.env.readRuntime = async () => null;
    else delete (f.record() as Partial<RuntimeRecord>).configHash;
    const restarted = new Connection(f.env);
    t.after(() => restarted.client.stop());
    assert.equal((await readyStatus(restarted)).phase, 'blocked');
    assert.equal((await restarted.action('disconnect')).phase, 'blocked');
    assert.equal(f.proxy().mode, 'fixed_servers');
    assert.ok(!f.events.includes('core.stop'));
    assert.ok(!f.events.includes('restore-proxy'));
  }
});

test('lost proxy control keeps recovery blocked', async (t) => {
  const f = fixture();
  await f.connection.action('save', form());
  await f.connection.action('connect');
  f.connection.client.stop();
  f.env.setProxy = async () => { throw new Error('policy owns proxy'); };
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  assert.equal((await readyStatus(restarted)).phase, 'blocked');
  assert.ok(!f.events.includes('restore-proxy'));
});

test('a changed live config blocks opening sites and stale disconnect cannot stop it', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  await f.connection.action('connect');
  f.statusWith({ configHash: 'b'.repeat(64) });
  assert.equal((await f.connection.action('status')).phase, 'blocked');
  assert.equal((await f.connection.action('open-site', { url: 'https://example.test/' })).phase, 'blocked');
  assert.equal((await f.connection.action('disconnect')).phase, 'blocked');
  assert.ok(!f.events.includes('restore-proxy'));
  assert.ok(!f.events.includes('open-site'));
});

test('a late status answer cannot revive an explicitly disconnected session', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  await f.connection.action('connect');
  const applied = f.messages.find((m) => m.method === 'config.apply')!;
  const request = f.connection.client.request.bind(f.connection.client);
  let answer!: (status: any) => void;
  f.connection.client.request = async (method, args) => method === 'core.status' ? new Promise((resolve) => { answer = resolve; }) : request(method, args);
  const pending = f.connection.action('status');
  await Promise.resolve();
  assert.equal((await f.connection.action('disconnect')).phase, 'disconnected');
  answer({ running: true, configHash: 'a'.repeat(64), selectedProfileId: applied.args.profileIds[0], inboundPorts: [{ listen: '127.0.0.1', port: 20100, tag: 'in-srv-' + applied.args.intent.servers[0].id.replaceAll('-', '').slice(0, 8) }] });
  await pending;
  assert.equal(f.connection.snapshot().phase, 'disconnected');
  assert.deepEqual(f.proxy(), f.previous);
});

test('an interrupted disconnect retains its previous proxy until explicit completion', async (t) => {
  const f = fixture();
  await f.connection.action('save', form());
  await f.connection.action('connect');
  f.connection.client.stop();
  f.record()!.active = false;
  f.kill();
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  assert.equal((await readyStatus(restarted)).phase, 'blocked');
  assert.equal((await restarted.action('disconnect')).phase, 'disconnected');
  assert.deepEqual(f.proxy(), f.previous);
});

test('test-server probes only the selected subscription server without changing the live session', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  const added = await f.connection.action('subscribe', { url: subscriptionURL });
  const selected = added.servers[2]!;
  await f.connection.action('select', { serverId: selected.id });
  assert.equal((await f.connection.action('connect')).phase, 'connected');
  const stored = structuredClone([f.stored(), f.subscription(), f.servers(), f.record(), f.proxy()]);
  const messages = f.messages.length;
  const events = f.events.length;
  let writes = 0;
  const write = f.env.writeRuntime;
  f.env.writeRuntime = async (value) => { writes++; await write(value); };
  const view = await f.connection.action('test-server');
  const check = { serverId: selected.id, layers: [
    { layer: 'dns', state: 'pass', rttMs: 1.25 },
    { layer: 'tcp', state: 'pass', rttMs: 2 },
    { layer: 'core-outbound', state: 'pass', rttMs: 3 },
    { layer: 'proxied-http', state: 'pass', rttMs: 4 },
  ], egressIp: null, matchesExpectedEgress: null };
  assert.equal(view.phase, 'connected');
  assert.equal(view.pending, false);
  assert.equal(view.error, null);
  assert.deepEqual(view.check, check);
  assert.deepEqual([f.stored(), f.subscription(), f.servers(), f.record(), f.proxy()], stored);
  assert.equal(writes, 0);
  const requests = f.messages.slice(messages);
  assert.deepEqual(requests.filter((message) => message.method === 'server.test').map((message) => message.args), [
    { serverId: selected.id, layers: ['dns', 'tcp', 'core-outbound', 'proxied-http'], repeat: 1 },
  ]);
  assert.ok(requests[0]?.method === 'core.status');
  assert.equal(requests.at(-1)?.method, 'core.status');
  assert.ok(f.events.slice(events).every((event) => ['core.status', 'server.test'].includes(event)));
  assert.deepEqual((await f.connection.action('status')).check, check);
  view.check!.serverId = crypto.randomUUID();
  view.check!.layers[0]!.rttMs = 999;
  view.check!.layers.push({ layer: 'tcp', state: 'fail', rttMs: 0 });
  const snapshot = f.connection.snapshot();
  assert.deepEqual(snapshot.check, check);
  snapshot.check!.layers[0]!.state = 'fail';
  snapshot.check!.layers.pop();
  assert.deepEqual(f.connection.snapshot().check, check);
});

test('pending test-server clears the old check and serializes all non-status actions', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  await f.connection.action('connect');
  const previous = (await f.connection.action('test-server')).check;
  assert.ok(previous);
  let finish!: (result: unknown) => void;
  let started!: () => void;
  const began = new Promise<void>((resolve) => { started = resolve; });
  f.probeWith(() => new Promise<unknown>((resolve) => { finish = resolve; started(); }));
  const checking = f.connection.action('test-server');
  await Promise.race([began, checking.then(() => assert.fail('server.test must remain pending'))]);
  const stored = structuredClone([f.stored(), f.record(), f.proxy()]);
  const messages = f.messages.length;
  const events = f.events.length;
  const status = await f.connection.action('status');
  assert.equal(status.pending, true);
  assert.equal(status.phase, 'connected');
  assert.equal(status.check, null);
  for (const action of ['test-server', 'save', 'subscribe', 'refresh', 'select', 'connect', 'disconnect', 'open-site'] as const) {
    const view = await f.connection.action(action, action === 'save' ? form() : { serverId: crypto.randomUUID(), url: 'https://example.test/' });
    assert.match(view.error!, /Дождитесь/);
    assert.equal(view.pending, true);
    assert.equal(view.phase, 'connected');
    assert.equal(view.check, null);
  }
  assert.equal(f.messages.length, messages);
  assert.equal(f.events.length, events);
  assert.deepEqual([f.stored(), f.record(), f.proxy()], stored);
  finish({ ...previous, overall: 'pass' });
  const view = await checking;
  assert.equal(view.pending, false);
  assert.equal(view.error, null);
  assert.deepEqual(view.check, previous);
});

test('test-server refuses disconnected, blocked, foreign Core and unowned proxy proofs before probing', async (t) => {
  for (const cause of ['disconnected', 'blocked', 'stopped', 'config', 'profile', 'missing-profile', 'missing-inbound', 'foreign-inbound', 'ambiguous-inbound', 'proxy-control', 'proxy-value'] as const) {
    await t.test(cause, async (t) => {
      const f = fixture();
      t.after(() => f.connection.client.stop());
      await f.connection.action('save', form());
      if (cause !== 'disconnected') await f.connection.action('connect');
      const proxy = structuredClone(f.proxy());
      if (cause === 'blocked') { f.kill(); await f.connection.action('status'); }
      if (cause === 'stopped') f.kill();
      if (cause === 'config') f.statusWith({ configHash: 'b'.repeat(64) });
      if (cause === 'profile') f.statusWith({ selectedProfileId: crypto.randomUUID() });
      if (cause === 'missing-profile') f.statusWith({ selectedProfileId: null });
      if (cause === 'missing-inbound') f.statusWith({ inboundPorts: [] });
      if (cause === 'foreign-inbound') f.statusWith({ inboundPorts: [{ listen: '0.0.0.0', port: 20100, tag: 'foreign' }] });
      if (cause === 'ambiguous-inbound') {
        const tag = 'in-srv-' + f.stored()!.server.id.replaceAll('-', '').slice(0, 8);
        f.statusWith({ inboundPorts: Array.from({ length: 2 }, () => ({ listen: '127.0.0.1', port: 20100, tag })) });
      }
      if (cause === 'proxy-control') f.env.getProxy = async () => ({ value: f.proxy(), levelOfControl: 'controlled_by_other_extensions' });
      if (cause === 'proxy-value') f.env.getProxy = async () => ({ value: { mode: 'direct' }, levelOfControl: 'controlled_by_this_extension' });
      const events = f.events.length;
      const view = await f.connection.action('test-server');
      assert.ok(view.error);
      assert.equal(view.check, null);
      assert.equal(view.phase, cause === 'disconnected' ? 'disconnected' : 'blocked');
      assert.ok(!f.events.includes('server.test'));
      assert.ok(f.events.slice(events).every((event) => event === 'core.status'));
      assert.deepEqual(f.proxy(), proxy);
    });
  }
});

test('test-server preserves failed, timeout and skipped rows while whitelisting public measurements', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  await f.connection.action('connect');
  const serverId = f.stored()!.server.id;
  const result = { serverId, overall: 'fail', raw: subscriptionURL, egressIp: '203.0.113.7', matchesExpectedEgress: true, layers: [
    { layer: 'dns', state: 'fail', rttMs: 0, code: 'private-access-secret', detail: subscriptionURL, raw: subscriptionURL },
    { layer: 'tcp', state: 'timeout', rttMs: null, jitterMs: 100, detail: 'private-access-secret' },
    { layer: 'core-outbound', state: 'skipped', detail: subscriptionURL },
    { layer: 'proxied-http', state: 'skipped', detail: subscriptionURL },
  ] };
  f.probeWith(() => result);
  const stored = structuredClone([f.stored(), f.record(), f.proxy()]);
  const view = await f.connection.action('test-server');
  const check = { serverId, layers: [
    { layer: 'dns', state: 'fail', rttMs: 0 },
    { layer: 'tcp', state: 'timeout', rttMs: null },
    { layer: 'core-outbound', state: 'skipped', rttMs: null },
    { layer: 'proxied-http', state: 'skipped', rttMs: null },
  ], egressIp: '203.0.113.7', matchesExpectedEgress: true };
  assert.equal(view.error, null);
  assert.equal(view.phase, 'connected');
  assert.deepEqual(view.check, check);
  assert.deepEqual([f.stored(), f.record(), f.proxy()], stored);
  // The egress pair is an observed answer the UI renders, so 203.0.113.7 is public now; what must never
  // travel is the request context the probe ran against and the native error detail.
  for (const secret of ['private-path-secret', 'query-secret', 'fragment-secret', 'private-access-secret']) {
    assert.ok(!JSON.stringify([view, f.connection.snapshot()]).includes(secret));
  }
  result.layers[0]!.rttMs = 999;
  result.serverId = crypto.randomUUID();
  assert.deepEqual(f.connection.snapshot().check, check);
});

test('refused test-server requests clear a previous check without leaking native error details', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  await f.connection.action('connect');
  for (const code of ['CoreNotRunning', 'AuthFailed', 'Timeout', 'Busy', 'Internal']) {
    f.deny(null);
    assert.ok((await f.connection.action('test-server')).check);
    const stored = structuredClone([f.stored(), f.record(), f.proxy()]);
    f.deny(code);
    const view = await f.connection.action('test-server');
    assert.ok(view.error);
    assert.equal(view.check, null);
    assert.equal(view.phase, 'connected');
    assert.equal(view.pending, false);
    assert.ok(!JSON.stringify([view, f.connection.snapshot()]).includes('private cause'));
    assert.deepEqual([f.stored(), f.record(), f.proxy()], stored);
  }
});

test('malformed, foreign, duplicate, unknown and missing probe results clear the check safely', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  await f.connection.action('connect');
  const good = { serverId: f.stored()!.server.id, overall: 'pass', layers: [
    { layer: 'dns', state: 'pass', rttMs: 1.25 },
    { layer: 'tcp', state: 'pass', rttMs: 2 },
    { layer: 'core-outbound', state: 'pass', rttMs: 3 },
    { layer: 'proxied-http', state: 'pass', rttMs: 4 },
  ], egressIp: null, matchesExpectedEgress: null };
  const malformed: unknown[] = [
    undefined, null, subscriptionURL, [], {}, { layers: good.layers },
    { ...good, serverId: crypto.randomUUID() }, { ...good, serverId: subscriptionURL },
    { ...good, layers: undefined }, { ...good, layers: {} }, { ...good, layers: [] },
    { ...good, layers: good.layers.slice(0, 2) },
    { ...good, layers: [good.layers[0], good.layers[0], good.layers[2]] },
    ...['unknown', 'proxied-http', 'egress-ip'].map((layer) => ({ ...good, layers: [...good.layers.slice(0, 2), { layer, state: 'pass', rttMs: 1 }] })),
    ...[undefined, 'unknown', true].map((state) => ({ ...good, layers: [{ ...good.layers[0], state }, ...good.layers.slice(1)] })),
    { ...good, layers: [null, ...good.layers.slice(1)] },
    ...[-1, NaN, Infinity, -Infinity, '1', true].map((rttMs) => ({ ...good, layers: [{ ...good.layers[0], rttMs }, ...good.layers.slice(1)] })),
  ];
  const stored = structuredClone([f.stored(), f.record(), f.proxy()]);
  for (const [index, result] of malformed.entries()) {
    f.probeWith(() => good);
    assert.ok((await f.connection.action('test-server')).check);
    f.probeWith(() => result && typeof result === 'object' && !Array.isArray(result) ? { ...result, raw: subscriptionURL, detail: 'private-access-secret' } : result);
    const view = await f.connection.action('test-server');
    assert.ok(view.error, `malformed result ${index} must report a safe error`);
    assert.equal(view.check, null);
    assert.equal(f.connection.snapshot().check, null);
    assert.equal(view.phase, 'connected');
    assert.equal(view.pending, false);
    for (const secret of ['private-path-secret', 'query-secret', 'fragment-secret', 'private-access-secret']) assert.ok(!JSON.stringify([view, f.connection.snapshot()]).includes(secret));
    assert.deepEqual([f.stored(), f.record(), f.proxy()], stored);
  }
});

test('late probe results are discarded after Core, config, proxy control or revision changes', async (t) => {
  for (const cause of ['stopped', 'config', 'proxy-control', 'revision'] as const) {
    await t.test(cause, async (t) => {
      const f = fixture();
      t.after(() => f.connection.client.stop());
      await f.connection.action('save', form());
      await f.connection.action('connect');
      const previous = (await f.connection.action('test-server')).check;
      assert.ok(previous);
      let finish!: (result: unknown) => void;
      let started!: () => void;
      const began = new Promise<void>((resolve) => { started = resolve; });
      f.probeWith(() => new Promise<unknown>((resolve) => { finish = resolve; started(); }));
      const checking = f.connection.action('test-server');
      await Promise.race([began, checking.then(() => assert.fail('server.test must remain pending'))]);
      const proxy = structuredClone(f.proxy());
      const events = f.events.length;
      if (cause === 'stopped') f.kill();
      if (cause === 'config') f.statusWith({ configHash: 'b'.repeat(64) });
      if (cause === 'proxy-control') f.env.getProxy = async () => ({ value: f.proxy(), levelOfControl: 'controlled_by_other_extensions' });
      if (cause === 'revision') await f.connection.preserveProxy({ mode: 'direct' });
      finish({ ...previous, overall: 'pass', raw: subscriptionURL });
      const view = await checking;
      assert.ok(view.error);
      assert.equal(view.check, null);
      assert.equal(f.connection.snapshot().check, null);
      assert.equal(view.pending, false);
      if (cause !== 'revision') assert.equal(view.phase, 'blocked');
      assert.deepEqual(f.proxy(), proxy);
      assert.ok(f.events.slice(events).every((event) => ['core.status', ...(cause === 'revision' ? ['set-proxy'] : [])].includes(event)));
      assert.ok(!JSON.stringify(view).includes('query-secret'));
    });
  }
});

test('non-status actions and a newly blocked session clear the previous probe check', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  await f.connection.action('connect');
  for (const action of ['save', 'subscribe', 'refresh', 'select', 'connect', 'open-site'] as const) {
    assert.ok((await f.connection.action('test-server')).check);
    const view = await f.connection.action(action, action === 'save' ? form() : { serverId: crypto.randomUUID(), url: 'https://example.test/' });
    assert.equal(view.check, null);
    assert.equal(view.phase, 'connected');
  }
  assert.ok((await f.connection.action('test-server')).check);
  f.kill();
  const blocked = await f.connection.action('status');
  assert.equal(blocked.phase, 'blocked');
  assert.equal(blocked.check, null);
  assert.equal(f.proxy().mode, 'fixed_servers');
});

test('checks are transient across disconnect, reconnect and controller restart', async (t) => {
  const f = fixture();
  t.after(() => f.connection.client.stop());
  await f.connection.action('save', form());
  assert.equal(f.connection.snapshot().check, null);
  await f.connection.action('connect');
  assert.ok((await f.connection.action('test-server')).check);
  assert.equal((await f.connection.action('disconnect')).check, null);
  const reconnected = await f.connection.action('connect');
  assert.equal(reconnected.phase, 'connected');
  assert.equal(reconnected.check, null);
  assert.ok((await f.connection.action('test-server')).check);
  f.connection.client.stop();
  assert.equal(f.connection.snapshot().check, null);
  const restarted = new Connection(f.env);
  t.after(() => restarted.client.stop());
  const loaded = await readyStatus(restarted);
  assert.equal(loaded.phase, 'connected');
  assert.equal(loaded.check, null);
  assert.ok((await restarted.action('test-server')).check);
  restarted.client.stop();
  assert.equal(restarted.snapshot().check, null);
  restarted.client.start();
  const recovered = await readyStatus(restarted);
  assert.equal(recovered.phase, 'connected');
  assert.equal(recovered.check, null);
  assert.equal(f.messages.filter((message) => message.method === 'server.test').length, 3);
  assert.equal(f.messages.filter((message) => message.method === 'config.apply').length, 2);
});
