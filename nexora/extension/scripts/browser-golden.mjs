import { spawn, spawnSync } from 'node:child_process';
import { createServer as httpsServer } from 'node:https';
import { createServer as tcpServer } from 'node:net';
import { generateKeyPairSync, createHash, randomUUID } from 'node:crypto';
import { mkdtemp, mkdir, readFile, writeFile, readdir, rm } from 'node:fs/promises';
import { strict as assert } from 'node:assert';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
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
  inbounds: [{ listen: '127.0.0.1', port: serverPort, protocol: 'vless', settings: { clients: [{ id: userId, flow: 'xtls-rprx-vision' }], decryption: 'none' }, streamSettings: { network: 'tcp', security: 'reality', realitySettings: { show: false, dest: '127.0.0.1:' + sitePort, xver: 0, serverNames: ['localhost'], privateKey, shortIds: ['abcd'] } } }],
  outbounds: [{ protocol: 'freedom', sendThrough: '127.0.0.2' }],
};
await writeFile(root + '/server.json', JSON.stringify(serverConfig));
await writeFile(core + '/receipt.json', JSON.stringify({ tag: 'v24.11.11', pinsVersion: '1', sha256: createHash('sha256').update(await readFile(xray)).digest('hex'), verifiedAt: new Date().toISOString(), path: xray }));
let socket;
let chrome;
let server;
let installed = false;
let measurement;
let nextId = 0;
const pending = new Map();
const delay = (ms) => new Promise((done) => setTimeout(done, ms));
const call = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
  const id = ++nextId;
  const timer = setTimeout(() => { pending.delete(id); reject(new Error('CDP timeout: ' + method)); }, 20000);
  pending.set(id, { resolve, reject, timer });
  socket.send(JSON.stringify({ id, method, params, sessionId }));
});
try {
  server = spawn(xray, ['run', '-c', root + '/server.json'], { stdio: 'ignore' });
  await delay(300);
  assert.equal(server.exitCode, null, 'REALITY fixture did not start');
  chrome = spawn(process.env.NEXORA_CHROME ?? join(process.env.PROGRAMFILES ?? 'C:/Program Files', 'Google/Chrome/Application/chrome.exe'), [
    '--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--ignore-certificate-errors',
    '--enable-unsafe-extension-debugging', '--remote-debugging-port=' + debugPort, '--user-data-dir=' + root + '/browser', 'about:blank',
  ], { stdio: 'ignore', env });
  let version;
  for (let attempt = 0; attempt < 50; attempt++) {
    try { version = await (await fetch('http://127.0.0.1:' + debugPort + '/json/version')).json(); break; } catch { await delay(100); }
  }
  assert.ok(version);
  socket = new WebSocket(version.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject; });
  socket.onmessage = ({ data }) => {
    const message = JSON.parse(data);
    const request = pending.get(message.id);
    if (!request) return;
    pending.delete(message.id);
    clearTimeout(request.timer);
    if (message.error) request.reject(new Error(JSON.stringify(message.error)));
    else request.resolve(message.result);
  };
  const { id } = await call('Extensions.loadUnpacked', { path: resolve(project, 'dist/chromium') });
  installed = true;
  command(bins + 'nexora-installer.exe', ['install', '--host', bins + 'nexora-host.exe', '--origin', 'chrome-extension://' + id + '/']);
  const { targetId } = await call('Target.createTarget', { url: 'chrome-extension://' + id + '/nexora-options.html' });
  const { sessionId } = await call('Target.attachToTarget', { targetId, flatten: true });
  await call('Runtime.enable', {}, sessionId);
  const evaluate = async (expression) => {
    const response = await call('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true }, sessionId);
    if (response.exceptionDetails) throw new Error(response.exceptionDetails.text + ': ' + (response.exceptionDetails.exception?.description ?? ''));
    return response.result.value;
  };
  await delay(1500);
  const rpc = async (action, payload) => evaluate(`new Promise(resolve=>chrome.runtime.sendMessage(${JSON.stringify({ channel: 'nexora.connection.v1', id: randomUUID(), action, payload })},resolve))`);
  let status;
  for (let attempt = 0; attempt < 40; attempt++) {
    status = await rpc('status');
    if (status?.view?.connector === 'ready') break;
    await delay(250);
  }
  assert.equal(status?.view?.connector, 'ready', JSON.stringify(status));
  const before = await evaluate('chrome.proxy.settings.get({incognito:false})');
  const input = { displayName: 'Loopback REALITY', address: '127.0.0.1', port: serverPort, userId, serverName: 'localhost', publicKey, shortId: 'abcd', flow: true };
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
  await delay(2100);
  const url = 'https://127.0.0.1:' + sitePort + '/golden';
  await evaluate(`(()=>{const root=document.querySelector('nexora-connection-panel').shadowRoot;const el=root.querySelector('#site-url');el.value=${JSON.stringify(url)};el.dispatchEvent(new Event('input',{bubbles:true}));el.form.requestSubmit();})()`);
  for (let attempt = 0; attempt < 80 && !arrivals.includes('127.0.0.2'); attempt++) await delay(100);
  assert.ok(arrivals.includes('127.0.0.2'), 'HTTPS request never passed through REALITY server: ' + JSON.stringify(arrivals));
  const targets = await call('Target.getTargets');
  const site = targets.targetInfos.find((target) => target.url === url);
  assert.ok(site);
  const siteSession = (await call('Target.attachToTarget', { targetId: site.targetId, flatten: true })).sessionId;
  const body = await call('Runtime.evaluate', { expression: 'document.body.textContent', returnByValue: true }, siteSession);
  assert.equal(body.result.value, 'nexora-local-https-ok');
  await delay(2100);
  await evaluate("document.querySelector('nexora-connection-panel').shadowRoot.querySelector('button.secondary[type=button]').click()");
  for (let attempt = 0; attempt < 60; attempt++) {
    status = await rpc('status');
    if (status.view.phase === 'disconnected') break;
    await delay(100);
  }
  assert.equal(status.view.phase, 'disconnected', JSON.stringify(status));
  await assert.rejects(readFile(local + '/NEXORA/config/config.json'), { code: 'ENOENT' });
  const after = await evaluate('chrome.proxy.settings.get({incognito:false})');
  assert.deepEqual(after.value, before.value);
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
  measurement = { browser: version.Browser, extensionId: id, realNativeMessaging: true, realXrayReality: true, httpsTunneledSource: '127.0.0.2', uuidAbsentFromLocalSyncAndBlob: true, previousProxyRestored: true, materializedConfigRemovedOnStop: true, reconnectAfterStop: true, width320WithoutOverflow: true, screenshot: root + '-connected.png', scope: 'loopback fixture; mechanically prepared test receipt, not trusted production installation; test-only HTTPS certificate exception' };
} finally {
  if (socket?.readyState === WebSocket.OPEN) {
    try { await call('Browser.close'); } catch {}
    socket.close();
  }
  if (chrome?.exitCode === null) chrome.kill();
  if (server?.exitCode === null) server.kill();
  web.closeAllConnections();
  await new Promise((done) => web.close(done));
  if (installed) command(bins + 'nexora-installer.exe', ['uninstall']);
  for (const key of keys) assert.notEqual(spawnSync('reg.exe', ['query', key], { stdio: 'ignore' }).status, 0, 'temporary registration not removed');
  await rm(root, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
}
console.log(JSON.stringify({ ...measurement, sandboxDisposed: true }));
