import { spawn, spawnSync } from 'node:child_process';
import { createServer as httpsServer } from 'node:https';
import { createServer as tcpServer } from 'node:net';
import { generateKeyPairSync, createHash, randomUUID } from 'node:crypto';
import { mkdtemp, mkdir, readFile, writeFile, readdir, rm } from 'node:fs/promises';
import { strict as assert } from 'node:assert';
import { tmpdir } from 'node:os';
import { basename, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

assert.equal(process.platform, 'win32', 'this live test requires Windows Native Messaging and DPAPI');
const project = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const root = await mkdtemp(join(tmpdir(), 'nexora-golden-'));
const local = root + '/local';
const core = local + '/NEXORA/core';
await mkdir(core, { recursive: true });
const env = { ...process.env, LOCALAPPDATA: local };
const bins = resolve(project, '../../tmp/bin') + '/';
const xray = process.env.NEXORA_XRAY;
const subscriptionURL = process.env.NEXORA_SUBSCRIPTION_URL;
assert.ok(xray, 'set NEXORA_XRAY to the local Xray 24.11.11 test executable; no download or install is performed');
const keys = [
  'HKCU\\Software\\Google\\Chrome\\NativeMessagingHosts\\com.nexora.connector',
  'HKCU\\Software\\Microsoft\\Edge\\NativeMessagingHosts\\com.nexora.connector',
  'HKCU\\Software\\Mozilla\\NativeMessagingHosts\\com.nexora.connector',
];
for (const key of keys) assert.notEqual(spawnSync('reg.exe', ['query', key], { stdio: 'ignore' }).status, 0, 'refusing to overwrite existing registration');
const command = (exe, args, environment = env) => {
  const result = spawnSync(exe, args, { env: environment, encoding: 'utf8' });
  if (result.status !== 0) throw new Error(exe + ' failed: ' + result.stderr);
  return result.stdout;
};
command('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', root + '/tls.key', '-out', root + '/tls.crt', '-days', '1', '-subj', '/CN=localhost']);
const arrivals = [];
const web = httpsServer({ key: await readFile(root + '/tls.key'), cert: await readFile(root + '/tls.crt'), minVersion: 'TLSv1.3', maxVersion: 'TLSv1.3' }, (req, res) => {
  arrivals.push(req.socket.remoteAddress);
  res.end('nexora-local-https-ok');
});
await new Promise((done) => web.listen(0, '127.0.0.1', done));
const sitePort = web.address().port;
const reserve = tcpServer();
await new Promise((done) => reserve.listen(0, '127.0.0.1', done));
const serverPort = reserve.address().port;
await new Promise((done) => reserve.close(done));
await new Promise((done) => reserve.listen(0, '127.0.0.1', done));
const debugPort = reserve.address().port;
await new Promise((done) => reserve.close(done));
const pair = generateKeyPairSync('x25519');
const privateKey = pair.privateKey.export({ format: 'der', type: 'pkcs8' }).subarray(-32).toString('base64url');
const publicKey = pair.publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64url');
const userId = randomUUID();
const serverConfig = {
  log: { loglevel: 'warning' },
  inbounds: [{ listen: '127.0.0.1', port: serverPort, protocol: 'vless', settings: { clients: [{ id: userId, flow: 'xtls-rprx-vision' }], decryption: 'none' }, streamSettings: { network: 'tcp', security: 'reality', realitySettings: { show: false, dest: '127.0.0.1:' + sitePort, xver: 0, serverNames: ['localhost'], privateKey, shortIds: ['0011223344556677'] } } }],
  outbounds: [{ protocol: 'freedom', sendThrough: '127.0.0.2' }],
};
await writeFile(root + '/server.json', JSON.stringify(serverConfig));
await writeFile(core + '/receipt.json', JSON.stringify({ tag: 'v24.11.11', pinsVersion: '1', sha256: createHash('sha256').update(await readFile(xray)).digest('hex'), verifiedAt: new Date().toISOString(), path: xray }));
let socket;
let chrome;
let server;
let version;
let id;
let sessionId;
let installed = false;
let measurement;
let nextId = 0;
const pending = new Map();
const workerVersions = new Map();
const delay = (ms) => new Promise((done) => setTimeout(done, ms));
const connectorExe = bins + 'nexora-connector.exe';
const hostExe = bins + 'nexora-host.exe';
const instancePath = local + '/NEXORA/state/connector-instance.json';
const appliedPath = local + '/NEXORA/state/state.json';
const runtimeKey = '-nexora.runtimeConnection';
const imageKey = (path) => resolve(path).replaceAll('\\', '/').toLowerCase();
const processes = (filter) => JSON.parse(command('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command',
  `$ErrorActionPreference='Stop';$items=@(Get-CimInstance Win32_Process -Filter '${filter}' | Select-Object ProcessId,ParentProcessId,ExecutablePath,CommandLine,CreationDate);ConvertTo-Json -InputObject $items -Compress`,
]).trim());
const processAt = (pid) => {
  assert.ok(Number.isSafeInteger(pid) && pid > 0, 'invalid process PID');
  return processes('ProcessId=' + pid)[0];
};
const waitProcessExit = async (owned) => {
  const deadline = performance.now() + 20000;
  while (performance.now() < deadline) {
    const current = processAt(owned.ProcessId);
    if (!current || current.CreationDate !== owned.CreationDate) return;
    await delay(100);
  }
  throw new Error('process did not exit: ' + owned.ProcessId);
};
const waitChildExit = (child) => new Promise((resolve, reject) => {
  if (!child || child.exitCode !== null || child.signalCode !== null) { resolve(); return; }
  const done = () => { clearTimeout(timer); child.off('error', failed); resolve(); };
  const failed = (error) => { clearTimeout(timer); child.off('exit', done); reject(error); };
  const timer = setTimeout(() => { child.off('exit', done); child.off('error', failed); reject(new Error('child did not exit: ' + child.pid)); }, 20000);
  child.once('exit', done);
  child.once('error', failed);
});
const stopChild = async (child) => {
  if (child?.exitCode === null && child.signalCode === null) child.kill();
  await waitChildExit(child);
};
const call = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
  const id = ++nextId;
  const timer = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); }, 20000);
  pending.set(id, { resolve, reject, timer });
  try { socket.send(JSON.stringify({ id, method, params, sessionId })); }
  catch (error) { pending.delete(id); clearTimeout(timer); reject(error); }
});
const launchChrome = async () => {
  chrome = spawn(process.env.NEXORA_CHROME ?? join(process.env.PROGRAMFILES ?? 'C:/Program Files', 'Google/Chrome/Application/chrome.exe'), [
    '--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-background-networking',
    ...subscriptionURL ? [] : ['--ignore-certificate-errors'],
    '--enable-unsafe-extension-debugging', '--remote-debugging-port=' + debugPort, '--user-data-dir=' + root + '/browser', 'about:blank',
  ], { stdio: 'ignore', env });
  version = undefined;
  for (let attempt = 0; attempt < 100; attempt++) {
    assert.equal(chrome.exitCode, null, 'Chrome exited before CDP was ready');
    try { version = await (await fetch('http://127.0.0.1:' + debugPort + '/json/version', { signal: AbortSignal.timeout(1000) })).json(); break; }
    catch { await delay(100); }
  }
  assert.ok(version, 'Chrome did not expose CDP');
  const ws = socket = new WebSocket(version.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject; });
  workerVersions.clear();
  ws.onmessage = ({ data }) => {
    const message = JSON.parse(data);
    if (message.method === 'ServiceWorker.workerVersionUpdated') {
      for (const worker of message.params.versions) workerVersions.set(worker.versionId, worker);
    }
    const request = pending.get(message.id);
    if (!request) return;
    pending.delete(message.id);
    clearTimeout(request.timer);
    if (message.error) request.reject(new Error(JSON.stringify(message.error)));
    else request.resolve(message.result);
  };
  ws.onclose = () => {
    if (socket !== ws) return;
    for (const request of pending.values()) { clearTimeout(request.timer); request.reject(new Error('CDP browser closed')); }
    pending.clear();
  };
};
const closeChrome = async () => {
  if (socket?.readyState === WebSocket.OPEN) {
    try { await call('Browser.close'); } catch {} // Chrome may close CDP before acknowledging Browser.close.
  }
  await waitChildExit(chrome);
  socket?.close();
};
const evaluate = async (expression) => {
  const response = await call('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true }, sessionId);
  if (response.exceptionDetails) throw new Error(response.exceptionDetails.text + ': ' + (response.exceptionDetails.exception?.description ?? ''));
  return response.result.value;
};
const attachOptions = async () => {
  const { targetId } = await call('Target.createTarget', { url: 'chrome-extension://' + id + '/nexora-options.html' });
  sessionId = (await call('Target.attachToTarget', { targetId, flatten: true })).sessionId;
  await call('Runtime.enable', {}, sessionId);
  for (let attempt = 0; attempt < 30; attempt++) {
    const actualId = await evaluate('globalThis.chrome?.runtime?.id');
    if (actualId) { assert.equal(actualId, id, 'options extension ID changed'); return targetId; }
    await delay(100);
  }
  await call('Target.closeTarget', { targetId });
  const error = new Error('options page did not load the expected extension');
  error.code = 'EXTENSION_NOT_LOADED';
  throw error;
};
const rpc = async (action, payload) => evaluate(`new Promise(resolve=>chrome.runtime.sendMessage(${JSON.stringify({ channel: 'nexora.connection.v1', id: randomUUID(), action, payload })},resolve))`);
const awaitConnected = async () => {
  let answer;
  for (let attempt = 0; attempt < 120; attempt++) {
    answer = await rpc('status');
    if (answer?.view?.phase === 'connected' && answer.view.connector === 'ready') return answer;
    await delay(250);
  }
  assert.fail('connection did not recover: ' + JSON.stringify(answer));
};
const workers = async () => (await call('Target.getTargets')).targetInfos.filter((target) => target.type === 'service_worker' && target.url.startsWith('chrome-extension://' + id + '/'));
const nativeHosts = () => processes("Name=\"" + basename(hostExe) + "\"").filter((item) => {
  assert.ok(item.ExecutablePath, 'cannot verify native-host process image');
  return imageKey(item.ExecutablePath) === imageKey(hostExe);
});
const cleanupConnector = async () => {
  let record;
  try { record = JSON.parse(await readFile(instancePath, 'utf8')); }
  catch (error) { if (error.code === 'ENOENT') return; throw error; }
  const owned = processAt(record.pid);
  if (!owned) return;
  assert.ok(owned.ExecutablePath && imageKey(owned.ExecutablePath) === imageKey(connectorExe), 'refusing to terminate a PID whose image is not the test Connector');
  const children = processes('ParentProcessId=' + record.pid);
  for (const child of children) assert.ok(child.ExecutablePath && imageKey(child.ExecutablePath) === imageKey(xray), 'unexpected shared Connector child');
  command('taskkill.exe', ['/PID', String(record.pid), '/F']); // Session record + verified image; never /IM or /T.
  await waitProcessExit(owned);
  for (const child of children) await waitProcessExit(child);
};
try {
  server = spawn(xray, ['run', '-c', root + '/server.json'], { stdio: 'ignore' });
  await delay(300);
  assert.equal(server.exitCode, null, 'REALITY fixture did not start');
  await launchChrome();
  ({ id } = await call('Extensions.loadUnpacked', { path: resolve(project, 'dist/chromium') }));
  installed = true;
  command(bins + 'nexora-installer.exe', ['install', '--host', hostExe, '--origin', 'chrome-extension://' + id + '/']);
  await attachOptions();
  await delay(1500);
  let status;
  for (let attempt = 0; attempt < 40; attempt++) {
    status = await rpc('status');
    if (status?.view?.connector === 'ready') break;
    await delay(250);
  }
  assert.equal(status?.view?.connector, 'ready', JSON.stringify(status));
  const before = await evaluate('chrome.proxy.settings.get({incognito:false})');
  if (subscriptionURL) {
    const probe = process.env.NEXORA_EGRESS_PROBE ?? 'https://api.ipify.org?format=json';
    const direct = await (await fetch(probe, { signal: AbortSignal.timeout(20000) })).json();
    assert.ok(direct.ip, 'egress probe must return a JSON ip field');
    const privateBody = Buffer.from(await (await fetch(subscriptionURL, { signal: AbortSignal.timeout(20000) })).text(), 'base64').toString();
    const privateIds = [...privateBody.matchAll(/vless:\/\/([0-9a-f-]+)@/gi)].map((match) => match[1]);
    assert.ok(privateIds.length >= 3, 'live subscription has too few entries');
    const privateEntries = privateBody.trim().split(/\r?\n/).filter((line) => line.startsWith('vless://')).map((line) => new URL(line));
    const tcpEntries = privateEntries.filter((entry) => ['tcp', 'raw', null].includes(entry.searchParams.get('type')) && !entry.searchParams.has('support-x25519mlkem768'));
    const unsupportedCount = privateEntries.length - tcpEntries.length;
    await evaluate(`(()=>{const input=document.querySelector('nexora-connection-panel').shadowRoot.querySelector('#subscription-url');input.value=${JSON.stringify(subscriptionURL)};input.dispatchEvent(new Event('input',{bubbles:true}));input.form.requestSubmit();})()`);
    for (let attempt = 0; attempt < 240; attempt++) {
      status = await rpc('status');
      if (status.view.subscription || status.view.error) break;
      await delay(250);
    }
    assert.ok(status.view.subscription, 'live subscription import failed: ' + (status.view.error?.code ?? 'no answer'));
    assert.equal(status.view.servers.length, tcpEntries.length, 'TCP subscription entry count mismatch');
    assert.ok(status.view.warning, 'unsupported entries must be visible');
    assert.equal(await evaluate("document.querySelector('nexora-connection-panel').shadowRoot.querySelector('#subscription-url').value"), '');
    const firstSource = status.view.subscription.id;
    await delay(2100);
    await evaluate(`(()=>{const input=document.querySelector('nexora-connection-panel').shadowRoot.querySelector('#subscription-url');input.value=${JSON.stringify(subscriptionURL)};input.dispatchEvent(new Event('input',{bubbles:true}));input.form.requestSubmit();})()`);
    for (let attempt = 0; attempt < 240; attempt++) {
      status = await rpc('status');
      if (!status.view.pending && (status.view.subscription?.id !== firstSource || status.view.error)) break;
      await delay(250);
    }
    assert.notEqual(status.view.subscription?.id, firstSource, 'new partial source kept the old active subscription');
    assert.equal(status.view.servers.length, tcpEntries.length, 'new source lost its supported server list');
    assert.ok(status.view.servers.every((server) => server.subscriptionId === status.view.subscription.id), 'new source mixed old server owners');
    const localStorage = await evaluate('chrome.storage.local.get(null)');
    const syncStorage = await evaluate('chrome.storage.sync.get(null)');
    const publicData = JSON.stringify([localStorage, syncStorage]);
    for (const secret of [subscriptionURL, new URL(subscriptionURL).pathname, ...privateIds]) {
      assert.ok(!publicData.includes(secret), 'private subscription data escaped into browser storage');
      for (const file of await readdir(local + '/NEXORA/keystore')) {
        assert.ok(!(await readFile(local + '/NEXORA/keystore/' + file)).includes(Buffer.from(secret)), 'subscription secret is plaintext at rest');
      }
    }
    assert.equal(Object.values(localStorage).find((item) => item?.urlRef?.startsWith('keystore://subscription/'))?.format, 'vless-base64', 'partial import did not persist its resolved format');
    const protectedBefore = new Map();
    for (const file of await readdir(local + '/NEXORA/keystore')) protectedBefore.set(file, await readFile(local + '/NEXORA/keystore/' + file));
    const selected = status.view.server.id;
    const refresh = await rpc('refresh');
    assert.ok(refresh.view.warning, 'partial refresh did not report unsupported entries');
    assert.equal(refresh.view.server.id, selected, 'refresh lost the selected server');
    assert.deepEqual(refresh.view.servers, status.view.servers, 'refresh changed the last-good server list');
    for (const [file, value] of protectedBefore) assert.deepEqual(await readFile(local + '/NEXORA/keystore/' + file), value, 'partial refresh changed a last-good credential');
    const representatives = ['tcp', 'raw', null].map((network) => status.view.servers.find((server) => tcpEntries.some((entry) => entry.searchParams.get('type') === network && entry.hostname === server.address && Number(entry.port) === server.port))).filter(Boolean);
    const candidates = [...new Map([...representatives, ...status.view.servers].map((server) => [server.id, server])).values()];
    const successes = [];
    let failures = 0;
    for (const candidate of candidates.slice(0, 8)) {
      await delay(6000); // Core mutations share a four-per-20-second budget.
      await evaluate(`(()=>{const select=document.querySelector('nexora-connection-panel').shadowRoot.querySelector('#subscription-server');select.value=${JSON.stringify(candidate.id)};select.dispatchEvent(new Event('change',{bubbles:true}));})()`);
      for (let attempt = 0; attempt < 40; attempt++) {
        status = await rpc('status');
        if (status.view.server.id === candidate.id) break;
        await delay(100);
      }
      assert.equal(status.view.server.id, candidate.id, 'UI server selection failed');
      await delay(2100);
      await evaluate("[...document.querySelector('nexora-connection-panel').shadowRoot.querySelectorAll('button')].find(button=>button.textContent.trim()==='Подключить').click()");
      for (let attempt = 0; attempt < 100; attempt++) {
        status = await rpc('status');
        if (status.view.phase === 'connected' || status.view.error) break;
        await delay(100);
      }
      assert.equal(status.view.phase, 'connected', 'selected server Core did not start');
      assert.equal(await evaluate("document.querySelector('nexora-connection-panel').shadowRoot.querySelector('#subscription-server').disabled"), true, 'selection remained enabled while connected');
      const config = JSON.parse(await readFile(local + '/NEXORA/config/config.json', 'utf8'));
      const proxied = config.outbounds.filter((outbound) => outbound.protocol === 'vless');
      assert.equal(proxied.length, 1, 'Core received more than the selected subscription server');
      assert.equal(proxied[0].settings.vnext[0].address, candidate.address, 'Core received the wrong server');
      await delay(2100);
      const url = probe + (probe.includes('?') ? '&' : '?') + 'nexora=' + randomUUID();
      const previousTargets = new Set((await call('Target.getTargets')).targetInfos.map((target) => target.targetId));
      await evaluate(`(()=>{const input=document.querySelector('nexora-connection-panel').shadowRoot.querySelector('#site-url');input.value=${JSON.stringify(url)};input.dispatchEvent(new Event('input',{bubbles:true}));input.form.requestSubmit();})()`);
      let site;
      for (let attempt = 0; attempt < 60; attempt++) {
        site = (await call('Target.getTargets')).targetInfos.find((target) => target.type === 'page' && !previousTargets.has(target.targetId));
        if (site) break;
        await delay(100);
      }
      assert.ok(site, 'open-site UI did not create a tab');
      const siteSession = (await call('Target.attachToTarget', { targetId: site.targetId, flatten: true })).sessionId;
      let egress;
      for (let attempt = 0; attempt < 120; attempt++) {
        let body;
        try { body = await call('Runtime.evaluate', { expression: 'document.body?.textContent', returnByValue: true }, siteSession); }
        catch { break; }
        try { egress = JSON.parse(body.result.value); } catch {}
        if (egress?.ip) break;
        await delay(250);
      }
      if (egress?.ip && egress.ip !== direct.ip) successes.push(candidate.id);
      else failures++;
      await call('Target.closeTarget', { targetId: site.targetId });
      await delay(2100);
      status = await rpc('disconnect');
      assert.equal(status.view.phase, 'disconnected', 'live disconnect failed: ' + status.view.error + '; connector=' + status.view.connector + '; provenSelections=' + successes.length);
      await assert.rejects(readFile(local + '/NEXORA/config/config.json'), { code: 'ENOENT' });
      assert.deepEqual((await evaluate('chrome.proxy.settings.get({incognito:false})')).value, before.value);
      if (successes.length === 3) break;
    }
    assert.equal(new Set(successes).size, 3, 'fewer than three distinct selected servers proved external HTTPS egress');
    await call('Emulation.setDeviceMetricsOverride', { width: 320, height: 1000, deviceScaleFactor: 1, mobile: false }, sessionId);
    assert.equal(await evaluate('document.documentElement.scrollWidth <= 320'), true, 'subscription UI overflows at 320px');
    measurement = { browser: version.Browser, realNativeMessaging: true, realDPAPI: true, subscriptionEntries: privateEntries.length, selectableServers: status.view.servers.length, unsupportedEntries: unsupportedCount, externalRealityHTTPSSelections: successes.length, unsuccessfulSelections: failures, selectionDisabledWhileConnected: true, onlySelectedServerInCore: true, subscriptionSecretsAbsentFromLocalSyncAndBlob: true, previousProxyRestored: true, materializedConfigRemovedOnStop: true, width320WithoutOverflow: true, scope: 'user-authorized live subscription; valid HTTPS certificates; mechanically prepared test receipt, not trusted production installation' };
  } else {
  const input = { displayName: 'Loopback REALITY', address: '127.0.0.1', port: serverPort, userId, serverName: 'localhost', publicKey, shortId: '0011223344556677', flow: true };
  await evaluate(`(()=>{const root=document.querySelector('nexora-connection-panel').shadowRoot;for(const [name,value] of Object.entries(${JSON.stringify(input)})){if(name==='flow')continue;const element=root.querySelector('[name="'+name+'"]');element.value=String(value);element.dispatchEvent(new Event('input',{bubbles:true}));}root.querySelector('#server-form').requestSubmit();})()`);
  for (let attempt = 0; attempt < 40; attempt++) {
    status = await rpc('status');
    if (status.view.server) break;
    await delay(100);
  }
  assert.ok(status.view.server, JSON.stringify(status));
  assert.equal(await evaluate("document.querySelector('nexora-connection-panel').shadowRoot.querySelector('#user-id')?.value ?? ''"), '');
  const stored = await evaluate('chrome.storage.local.get(null)');
  assert.ok(!JSON.stringify(stored).includes(userId), 'credential leaked to browser local store');
  const sync = await evaluate('chrome.storage.sync.get(null)');
  assert.ok(!JSON.stringify(sync).includes(userId), 'credential leaked to browser sync store');
  const blobs = await readdir(local + '/NEXORA/keystore');
  assert.ok(blobs.length > 0);
  for (const file of blobs) assert.ok(!(await readFile(local + '/NEXORA/keystore/' + file)).includes(Buffer.from(userId)), 'credential is plaintext at rest');
  await delay(2100);
  await evaluate("document.querySelector('nexora-connection-panel').shadowRoot.querySelector('button.primary[type=button]').click()");
  for (let attempt = 0; attempt < 60; attempt++) {
    status = await rpc('status');
    if (status.view.phase === 'connected') break;
    if (status.view.error) throw new Error(JSON.stringify(status));
    await delay(100);
  }
  assert.equal(status.view.phase, 'connected', JSON.stringify(status));
  const selectedServer = status.view.server.id;
  const runtimeBefore = await evaluate(`chrome.storage.local.get(${JSON.stringify(runtimeKey)}).then(value=>value[${JSON.stringify(runtimeKey)}])`);
  assert.equal(runtimeBefore?.active, true, 'active connection was not persisted durably');
  assert.deepEqual(runtimeBefore.previous, before.value, 'durable runtime lost the exact previous proxy');
  const connectorBefore = JSON.parse(await readFile(instancePath, 'utf8'));
  const appliedBefore = JSON.parse(await readFile(appliedPath, 'utf8'));
  assert.equal(appliedBefore.selectedProfile, runtimeBefore.profileId);
  assert.equal(appliedBefore.appliedConfigHash, runtimeBefore.configHash);
  const connectorProcess = processAt(connectorBefore.pid);
  const coreProcess = processAt(appliedBefore.lastStartPid);
  assert.ok(connectorProcess?.ExecutablePath && imageKey(connectorProcess.ExecutablePath) === imageKey(connectorExe), 'not the session test Connector');
  assert.ok(coreProcess?.ExecutablePath && imageKey(coreProcess.ExecutablePath) === imageKey(xray), 'applied-state PID is not the test Core');
  const assertSameProcesses = async () => {
    const connectorNow = JSON.parse(await readFile(instancePath, 'utf8'));
    const appliedNow = JSON.parse(await readFile(appliedPath, 'utf8'));
    assert.equal(connectorNow.pid, connectorBefore.pid, 'shared Connector PID changed');
    assert.equal(connectorNow.startedAt, connectorBefore.startedAt, 'shared Connector instance changed');
    assert.equal(appliedNow.lastStartPid, appliedBefore.lastStartPid, 'Core PID changed');
    assert.equal(appliedNow.selectedProfile, appliedBefore.selectedProfile, 'Core selection changed');
    assert.equal(appliedNow.appliedConfigHash, appliedBefore.appliedConfigHash, 'Core config changed');
    assert.equal(processAt(connectorBefore.pid)?.CreationDate, connectorProcess.CreationDate, 'Connector process exited or PID was reused');
    const candidates = processes('ParentProcessId=' + connectorBefore.pid);
    assert.deepEqual(candidates.map((item) => item.ProcessId), [appliedBefore.lastStartPid], 'shared Connector has no Core or duplicate children');
    assert.equal(candidates[0].CreationDate, coreProcess.CreationDate, 'Core process exited or PID was reused');
    assert.ok(candidates[0].ExecutablePath && imageKey(candidates[0].ExecutablePath) === imageKey(xray));
  };
  const proveHTTPS = async () => {
    await delay(2100);
    const start = arrivals.length;
    const url = 'https://127.0.0.1:' + sitePort + '/golden?' + randomUUID();
    await evaluate(`(()=>{const root=document.querySelector('nexora-connection-panel').shadowRoot;const el=root.querySelector('#site-url');el.value=${JSON.stringify(url)};el.dispatchEvent(new Event('input',{bubbles:true}));el.form.requestSubmit();})()`);
    for (let attempt = 0; attempt < 80 && !arrivals.slice(start).includes('127.0.0.2'); attempt++) await delay(100);
    assert.ok(arrivals.slice(start).includes('127.0.0.2'), 'fresh HTTPS request never passed through REALITY server');
    const site = (await call('Target.getTargets')).targetInfos.find((target) => target.url === url);
    assert.ok(site, 'loopback HTTPS tab missing');
    const siteSession = (await call('Target.attachToTarget', { targetId: site.targetId, flatten: true })).sessionId;
    let text;
    for (let attempt = 0; attempt < 80; attempt++) {
      text = (await call('Runtime.evaluate', { expression: 'document.body?.textContent', returnByValue: true }, siteSession)).result.value;
      if (text === 'nexora-local-https-ok') break;
      await delay(100);
    }
    assert.equal(text, 'nexora-local-https-ok');
    await call('Target.closeTarget', { targetId: site.targetId });
  };
  await proveHTTPS();
  const workerRestart = { stopped: false, recovered: false, method: null, reason: null };
  const oldWorker = (await workers())[0];
  if (oldWorker) {
    try { await call('ServiceWorker.enable', {}, sessionId); } catch {}
    for (let attempt = 0; attempt < 10 && ![...workerVersions.values()].some((worker) => worker.targetId === oldWorker.targetId); attempt++) await delay(100);
    const workerVersion = [...workerVersions.values()].find((worker) => worker.targetId === oldWorker.targetId);
    if (workerVersion) {
      try { await call('ServiceWorker.stopWorker', { versionId: workerVersion.versionId }, sessionId); workerRestart.method = 'ServiceWorker.stopWorker'; } catch {}
    }
    if (!workerRestart.method) {
      try {
        if ((await call('Target.closeTarget', { targetId: oldWorker.targetId })).success) workerRestart.method = 'Target.closeTarget';
      } catch {}
    }
    const confirmWorkerStopped = async () => {
      for (let attempt = 0; attempt < 50; attempt++) {
        if (!(await workers()).some((worker) => worker.targetId === oldWorker.targetId)) return true;
        await delay(100);
      }
      return false;
    };
    if (workerRestart.method) workerRestart.stopped = await confirmWorkerStopped();
    if (!workerRestart.stopped && workerRestart.method === 'ServiceWorker.stopWorker') {
      try {
        if ((await call('Target.closeTarget', { targetId: oldWorker.targetId })).success) {
          workerRestart.method = 'Target.closeTarget';
          workerRestart.stopped = await confirmWorkerStopped();
        }
      } catch {}
    }
    if (workerRestart.stopped) {
      status = await awaitConnected();
      assert.ok((await workers()).some((worker) => worker.targetId !== oldWorker.targetId), 'MV3 worker did not restart');
      assert.equal(status.view.server.id, selectedServer, 'worker restart lost selected server');
      assert.deepEqual(await evaluate(`chrome.storage.local.get(${JSON.stringify(runtimeKey)}).then(value=>value[${JSON.stringify(runtimeKey)}])`), runtimeBefore, 'worker restart changed durable runtime');
      await assertSameProcesses();
      workerRestart.recovered = true;
    } else workerRestart.reason = 'CDP could not prove the original worker target stopped; no worker restart claimed';
  } else workerRestart.reason = 'MV3 worker target unavailable; no worker restart claimed';
  assert.ok(nativeHosts().length > 0, 'no real native-host process before browser shutdown');
  const oldChromePID = chrome.pid;
  await closeChrome();
  for (let attempt = 0; attempt < 100 && nativeHosts().length; attempt++) await delay(100);
  assert.equal(nativeHosts().length, 0, 'native hosts survived Browser.close');
  const quietStart = performance.now();
  let zeroPortSamples = 0;
  do {
    assert.equal(nativeHosts().length, 0, 'native host appeared during the zero-native-port interval');
    await assertSameProcesses();
    zeroPortSamples++;
    if (performance.now() - quietStart < 60000) await delay(1000);
  } while (performance.now() - quietStart < 60000);
  assert.equal(nativeHosts().length, 0, 'native host present at end of ADR5 survival interval');
  await assertSameProcesses();
  const zeroNativePortsMilliseconds = Math.round(performance.now() - quietStart);
  await launchChrome();
  assert.notEqual(chrome.pid, oldChromePID, 'Chrome was not relaunched');
  let automaticWorker;
  for (let attempt = 0; attempt < 50; attempt++) {
    automaticWorker = (await workers())[0];
    if (automaticWorker) break;
    await delay(100);
  }
  let unpackedReloadedOnRelaunch = false;
  if (!automaticWorker) {
    // Probe the old ID before reloading: a loaded extension must initialize without an options-page RPC.
    let extensionStillLoaded = false;
    try { await attachOptions(); extensionStillLoaded = true; }
    catch (error) { if (error.code !== 'EXTENSION_NOT_LOADED') throw error; }
    assert.equal(extensionStillLoaded, false, 'persisted extension did not initialize its background automatically');
    const loaded = await call('Extensions.loadUnpacked', { path: resolve(project, 'dist/chromium') });
    assert.equal(loaded.id, id, 'unpacked extension ID changed on relaunch');
    unpackedReloadedOnRelaunch = true;
    for (let attempt = 0; attempt < 50; attempt++) {
      automaticWorker = (await workers())[0];
      if (automaticWorker) break;
      await delay(100);
    }
  }
  assert.ok(automaticWorker, 'background did not initialize automatically before attaching new options');
  for (let attempt = 0; attempt < 50 && !nativeHosts().length; attempt++) await delay(100);
  assert.ok(nativeHosts().length > 0, 'background did not reconnect native messaging automatically before options opened');
  await attachOptions();
  assert.equal(await evaluate('chrome.runtime.id'), id, 'extension ID changed after browser restart');
  assert.deepEqual(await evaluate(`chrome.storage.local.get(${JSON.stringify(runtimeKey)}).then(value=>value[${JSON.stringify(runtimeKey)}])`), runtimeBefore, 'browser restart lost durable local runtime');
  status = await awaitConnected();
  assert.equal(status.view.server.id, selectedServer, 'browser restart lost selected server');
  await assertSameProcesses();
  await proveHTTPS();
  const recovery = { workerRestart, browserRestart: true, sameUserDataDirAndDebugPort: true, extensionIdStable: true, unpackedReloadedOnRelaunch, backgroundAutomaticallyInitialized: true, durableLocalRuntimeSurvived: true, connectorPID: connectorBefore.pid, corePID: appliedBefore.lastStartPid, connectorAndCorePIDsUnchanged: true, selectedServerRestored: true, noDuplicateCore: true, adr5Survived: true, zeroNativePorts: true, zeroNativePortsMilliseconds, zeroPortSamples, zeroNativePortsEvidence: 'no process with the expected test native-host executable during the closed-browser interval', httpsProvenAfterBrowserRestart: true };
  await delay(2100);
  await evaluate("[...document.querySelector('nexora-connection-panel').shadowRoot.querySelectorAll('button')].find(button=>button.textContent.trim()==='Отключить').click()");
  for (let attempt = 0; attempt < 60; attempt++) {
    status = await rpc('status');
    if (status.view.phase === 'disconnected') break;
    await delay(100);
  }
  assert.equal(status.view.phase, 'disconnected', JSON.stringify(status));
  await assert.rejects(readFile(local + '/NEXORA/config/config.json'), { code: 'ENOENT' });
  const after = await evaluate('chrome.proxy.settings.get({incognito:false})');
  assert.deepEqual(after.value, before.value);
  assert.equal(await evaluate(`chrome.storage.local.get(${JSON.stringify(runtimeKey)}).then(value=>Object.hasOwn(value,${JSON.stringify(runtimeKey)}))`), false, 'disconnect did not clear durable local runtime');
  await delay(2100);
  await evaluate("document.querySelector('nexora-connection-panel').shadowRoot.querySelector('button.primary[type=button]').click()");
  for (let attempt = 0; attempt < 60; attempt++) {
    status = await rpc('status');
    if (status.view.phase === 'connected') break;
    if (status.view.error) throw new Error(JSON.stringify(status));
    await delay(100);
  }
  assert.equal(status.view.phase, 'connected', 'connect-after-stop failed: ' + JSON.stringify(status));
  await call('Emulation.setDeviceMetricsOverride', { width: 320, height: 900, deviceScaleFactor: 1, mobile: false }, sessionId);
  assert.equal(await evaluate('document.documentElement.scrollWidth <= 320'), true, 'connection UI overflows at 320px');
  const shot = await call('Page.captureScreenshot', { captureBeyondViewport: true }, sessionId);
  await writeFile(root + '-connected.png', Buffer.from(shot.data, 'base64'));
  await delay(2100);
  status = await rpc('disconnect');
  assert.equal(status.view.phase, 'disconnected', JSON.stringify(status));
  await assert.rejects(readFile(local + '/NEXORA/config/config.json'), { code: 'ENOENT' });
  assert.deepEqual((await evaluate('chrome.proxy.settings.get({incognito:false})')).value, before.value);
  assert.equal(await evaluate(`chrome.storage.local.get(${JSON.stringify(runtimeKey)}).then(value=>Object.hasOwn(value,${JSON.stringify(runtimeKey)}))`), false, 'final disconnect did not clear durable local runtime');
  measurement = { browser: version.Browser, extensionId: id, realNativeMessaging: true, realXrayReality: true, httpsTunneledSource: '127.0.0.2', recovery, durableRuntimeClearedOnDisconnect: true, uuidAbsentFromLocalSyncAndBlob: true, previousProxyRestored: true, materializedConfigRemovedOnStop: true, reconnectAfterStop: true, width320WithoutOverflow: true, screenshot: root + '-connected.png', scope: 'loopback fixture; mechanically prepared test receipt, not trusted production installation; test-only HTTPS certificate exception' };
  }
} finally {
  const cleanupErrors = [];
  const cleanup = async (action) => { try { await action(); } catch (error) { cleanupErrors.push(error); } };
  await cleanup(async () => {
    try { await closeChrome(); } catch { await stopChild(chrome); socket?.close(); }
    for (let attempt = 0; attempt < 100 && nativeHosts().length; attempt++) await delay(100);
    assert.equal(nativeHosts().length, 0, 'native hosts still running during cleanup');
  });
  await cleanup(() => stopChild(server));
  await cleanup(async () => {
    web.closeAllConnections();
    await new Promise((done) => web.close(done));
  });
  // Never kill the shared service while a browser/host could still reconnect and recreate it.
  if (!cleanupErrors.length) await cleanup(cleanupConnector);
  await cleanup(async () => {
    if (installed) command(bins + 'nexora-installer.exe', ['uninstall']);
    for (const key of keys) assert.notEqual(spawnSync('reg.exe', ['query', key], { stdio: 'ignore' }).status, 0, 'temporary registration not removed');
  });
  if (cleanupErrors.length) throw new AggregateError(cleanupErrors, 'cleanup failed; sandbox retained: ' + root);
  await rm(root, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
}
console.log(JSON.stringify({ ...measurement, sandboxDisposed: true }));
