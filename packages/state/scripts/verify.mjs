// The verification harness of packages/state: it drives the ConnectorClient over a fake port and a fake clock, so
// the deadline, the request table, the byte windows and the reconnect ladder are measured rather than described.
// Run with `npm --prefix packages/state run verify`. Zero dependencies, as
// docs/architecture/20-target-repository-layout.md §3 asks of tooling.
import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { ClientError, ConnectorClient } from '../src/client.ts';
import {
  extensionToHostBytes,
  hostName,
  maxConcurrentRequests,
  maxInFlightBytes,
  methodTimeoutMs,
  protocolVersion,
  reconnectBackoffSeconds,
  reconnectJitterPercent,
} from '../../protocol/src/contract.ts';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '..', '..', '..');
const protocolDir = join(repoRoot, 'packages', 'schema', 'protocol');
const registry = JSON.parse(readFileSync(join(protocolDir, 'compatibility-rules.json'), 'utf8'));
const allCapabilities = registry.capabilities.map((entry) => entry.name);

let checks = 0;
const failures = [];

function check(name, run) {
  checks += 1;
  return Promise.resolve()
    .then(run)
    .catch((error) => failures.push(`${name} -- ${error.message}`));
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function assertEqual(actual, expected, message) {
  if (actual !== expected) throw new Error(`${message}: got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)}`);
}

const tick = () => new Promise((done) => setImmediate(done));

function fakeClock() {
  let now = 0;
  let handle = 0;
  const timers = new Map();
  return {
    now: () => now,
    setTimeout(fn, ms) {
      handle += 1;
      timers.set(handle, { at: now + ms, fn });
      return handle;
    },
    clearTimeout(h) {
      timers.delete(h);
    },
    advance(ms) {
      const target = now + ms;
      for (;;) {
        const due = [...timers.entries()].filter(([, t]) => t.at <= target).sort((a, b) => a[1].at - b[1].at || a[0] - b[0]);
        if (due.length === 0) break;
        const [h, timer] = due[0];
        timers.delete(h);
        now = timer.at;
        timer.fn();
      }
      now = target;
    },
    armed: () => [...timers.values()].map((t) => t.at - now).sort((a, b) => a - b),
  };
}

function fakePort() {
  const sent = [];
  let message = null;
  let disconnect = null;
  let closed = false;
  return {
    sent,
    postMessage(frame) {
      if (closed) throw new Error('postMessage on a closed port');
      sent.push(frame);
    },
    onMessage: { addListener(fn) { message = fn; } },
    onDisconnect: { addListener(fn) { disconnect = fn; } },
    disconnect() { closed = true; },
    deliver(frame) { message?.(frame); },
    drop() { closed = true; disconnect?.(); },
    isClosed: () => closed,
  };
}

const answer = (request, result) => ({
  v: 1,
  id: `ans-${request.id}`,
  type: 'response',
  inReplyTo: request.id,
  ok: true,
  result,
  sentAt: new Date(0).toISOString(),
});

const refusal = (request, code) => ({
  v: 1,
  id: `ans-${request.id}`,
  type: 'response',
  inReplyTo: request.id,
  ok: false,
  error: { code, message: `refused with ${code}` },
  sentAt: new Date(0).toISOString(),
});

const helloResult = (over = {}) => ({
  protocolVersion,
  minCompatibleProtocolVersion: 1,
  connectorVersion: '0.1.0',
  coreVersion: null,
  capabilities: allCapabilities,
  ...over,
});

function rig(options = {}) {
  const clock = fakeClock();
  const ports = [];
  const events = [];
  const states = [];
  const reconciles = [];
  const client = new ConnectorClient({
    connect: () => {
      const port = fakePort();
      ports.push(port);
      return port;
    },
    extensionVersion: '3.6.0',
    clock,
    random: options.random ?? (() => 0.5),
    onEvent: (frame) => events.push(frame),
    onState: (state, reason) => states.push({ state, reason }),
    onReconcile: (result) => reconciles.push(result),
  });
  return { client, clock, ports, events, states, reconciles };
}

// start() posts connector.hello; answering it is what makes the client ready.
async function ready(rig, over = {}) {
  rig.client.start();
  await tick();
  const port = rig.ports[rig.ports.length - 1];
  port.deliver(answer(port.sent[0], helloResult(over)));
  await tick();
  return port;
}

async function expectClientError(code, run, message) {
  try {
    await run();
  } catch (error) {
    assert(error instanceof ClientError, `${message}: threw ${error}`);
    assertEqual(error.code, code, `${message}: wrong code`);
    return error;
  }
  throw new Error(`${message}: nothing threw`);
}

const run = async () => {
  await check('the client posts envelope objects, never a length-prefixed frame', async () => {
    const r = rig();
    const port = await ready(r);
    const hello = port.sent[0];
    assertEqual(typeof hello, 'object', 'the browser frames the message itself; a Uint8Array here would be payload');
    assertEqual(hello.type, 'request', 'the first frame is a request');
    assertEqual(hello.method, 'connector.hello', 'the first method is the negotiation');
    assertEqual(hello.v, 1, 'the frame format version travels');
    assert(/^[A-Za-z0-9_-]{8,36}$/.test(hello.id), `the id ${hello.id} is not the envelope pattern`);
    assertEqual(hello.args.protocolVersion, protocolVersion, 'the extension announces its own protocol version');
    assertEqual(hostName, 'com.nexora.connector', 'the name the worker passes to connectNative is the registered one');
  });

  await check('no command is sent before connector.hello succeeds', async () => {
    const r = rig();
    r.client.start();
    await tick();
    const port = r.ports[0];
    await expectClientError('HandshakeFailed', () => r.client.request('core.status'), 'a command during negotiation');
    assertEqual(port.sent.length, 1, 'only hello was written');
    port.deliver(answer(port.sent[0], helloResult()));
    await tick();
    assertEqual(r.client.state, 'ready', 'the client is ready once hello answered');
    const pending = r.client.request('core.status');
    await tick();
    assertEqual(port.sent.length, 2, 'the command travels after the negotiation');
    port.deliver(answer(port.sent[1], { running: false }));
    await pending;
  });

  await check('every request carries its own id and one answer resolves exactly one request', async () => {
    const r = rig();
    const port = await ready(r);
    const first = r.client.request('core.status');
    const second = r.client.request('connector.version');
    await tick();
    const a = port.sent[1];
    const b = port.sent[2];
    assert(a.id !== b.id, 'two in-flight requests share an id');
    assertEqual(a.seq + 1, b.seq, 'seq is assigned per sender and increases');
    port.deliver(answer(b, { version: '0.1.0' }));
    assertEqual(JSON.stringify(await second), JSON.stringify({ version: '0.1.0' }), 'the second answer resolved the first request');
    port.deliver(answer(a, { running: true }));
    assertEqual(JSON.stringify(await first), JSON.stringify({ running: true }), 'the first request never resolved');
  });

  await check('the deadline is the method own timeoutMs, so a hung host cannot hang the worker', async () => {
    const r = rig();
    const port = await ready(r);
    const quick = r.client.request('connector.version');
    const slow = r.client.request('config.apply', {});
    await tick();
    r.clock.advance(methodTimeoutMs['connector.version']);
    const quickError = await expectClientError('Timeout', () => quick, 'connector.version past its deadline');
    assertEqual(quickError.retryable, true, 'Timeout is retryable by the taxonomy');
    let settled = false;
    void slow.then(() => { settled = true; }, () => { settled = true; });
    r.clock.advance(1);
    await tick();
    assertEqual(settled, false, 'config.apply expired on another method deadline');
    r.clock.advance(methodTimeoutMs['config.apply']);
    await expectClientError('Timeout', () => slow, 'config.apply past its own deadline');
    assertEqual(port.sent.length, 3, 'a timeout writes nothing');
  });

  await check('a late answer to a timed-out request is discarded and re-applies nothing', async () => {
    const r = rig();
    const port = await ready(r);
    const request = r.client.request('config.apply', {});
    await tick();
    const sent = port.sent[1];
    r.clock.advance(methodTimeoutMs['config.apply']);
    await expectClientError('Timeout', () => request, 'the deadline');
    port.deliver(answer(sent, { appliedConfigHash: 'a'.repeat(64) }));
    await tick();
    assertEqual(r.events.length, 0, 'a late answer was fanned out as an event');
    assertEqual(r.client.state, 'ready', 'a late answer disturbed the session');
    const next = r.client.request('core.status');
    await tick();
    port.deliver(answer(port.sent[2], { running: true }));
    assertEqual(JSON.stringify(await next), JSON.stringify({ running: true }), 'the table did not survive the discard');
  });

  await check('a second answer to the same id is discarded', async () => {
    const r = rig();
    const port = await ready(r);
    const request = r.client.request('core.status');
    await tick();
    const sent = port.sent[1];
    port.deliver(answer(sent, { running: true }));
    port.deliver(answer(sent, { running: false }));
    assertEqual(JSON.stringify(await request), JSON.stringify({ running: true }), 'the second answer replaced the first');
  });

  await check('a command whose capability hello did not announce is refused before it is sent', async () => {
    const r = rig();
    const port = await ready(r, { capabilities: [] });
    await expectClientError('HandshakeFailed', () => r.client.request('config.snapshot', {}), 'a gated command with no capability');
    await expectClientError('HandshakeFailed', () => r.client.request('server.test', {}), 'a probe with no capability');
    assertEqual(port.sent.length, 1, 'a gated command was written');
    const ungated = r.client.request('core.status');
    await tick();
    port.deliver(answer(port.sent[1], { running: false }));
    await ungated;
  });

  await check('the concurrency window refuses the next request with Busy and writes nothing', async () => {
    const r = rig();
    const port = await ready(r);
    const held = [];
    for (let i = 0; i < maxConcurrentRequests; i += 1) held.push(r.client.request('core.status'));
    await tick();
    assertEqual(port.sent.length, maxConcurrentRequests + 1, 'the window did not fill');
    await expectClientError('Busy', () => r.client.request('core.status'), 'the request past the window');
    assertEqual(port.sent.length, maxConcurrentRequests + 1, 'a refused request was written');
    for (let i = 0; i < maxConcurrentRequests; i += 1) port.deliver(answer(port.sent[i + 1], { running: false }));
    await Promise.all(held);
    const after = r.client.request('core.status');
    await tick();
    port.deliver(answer(port.sent[port.sent.length - 1], { running: false }));
    await after;
  });

  await check('the in-flight byte window refuses before the write', async () => {
    const r = rig();
    const port = await ready(r);
    const body = 'x'.repeat(maxInFlightBytes - 4096);
    const first = r.client.request('profile.import', { body });
    await tick();
    assertEqual(port.sent.length, 2, 'the first large request was refused');
    await expectClientError('Busy', () => r.client.request('profile.import', { body }), 'a second request over the byte window');
    assertEqual(port.sent.length, 2, 'the refused request was written');
    port.deliver(answer(port.sent[1], { imported: true }));
    await first;
  });

  await check('a single request over the extension-to-host ceiling is refused before the write', async () => {
    const r = rig();
    const port = await ready(r);
    const body = 'x'.repeat(extensionToHostBytes);
    const error = await expectClientError('BadRequest', () => r.client.request('profile.import', { body }), 'a body over 64 MiB');
    assertEqual(error.retryable, false, 'an oversized body is not something asking again fixes');
    assertEqual(port.sent.length, 1, 'the oversized frame was written');
  });

  await check('a closed port is a reconnect, not a shutdown, and what was in flight fails retryably', async () => {
    const r = rig();
    const port = await ready(r);
    const request = r.client.request('core.status');
    await tick();
    port.drop();
    const error = await expectClientError('NetworkUnreachable', () => request, 'the request the drop took');
    assertEqual(error.retryable, true, 'a dropped port is retryable');
    assertEqual(r.client.state, 'waiting', 'the client did not schedule a reconnect');
    assertEqual(r.clock.armed().length, 1, 'more than one reconnect was scheduled');
  });

  await check('the reconnect ladder is the contract ladder, jittered within its percent, and capped', async () => {
    const r = rig();
    let port = await ready(r);
    const seen = [];
    for (let i = 0; i < reconnectBackoffSeconds.length + 2; i += 1) {
      port.drop();
      const delay = r.clock.armed()[0];
      seen.push(delay);
      r.clock.advance(delay);
      await tick();
      port = r.ports[r.ports.length - 1];
      assertEqual(port.sent[0].method, 'connector.hello', 'a reconnect did not re-run hello');
    }
    const expected = [...reconnectBackoffSeconds, reconnectBackoffSeconds.at(-1), reconnectBackoffSeconds.at(-1)].map((s) => s * 1000);
    assertEqual(JSON.stringify(seen), JSON.stringify(expected), 'the ladder is not the contract ladder at the jitter midpoint');
    for (const percent of [0, 1]) {
      const j = rig({ random: () => percent });
      await ready(j);
      j.ports[0].drop();
      const bound = reconnectBackoffSeconds[0] * 1000 * (1 + (percent * 2 - 1) * (reconnectJitterPercent / 100));
      assertEqual(j.clock.armed()[0], Math.round(bound), `jitter at random()=${percent} left the ${reconnectJitterPercent} percent band`);
    }
  });

  await check('a successful hello resets the ladder rather than resuming where it stopped', async () => {
    const r = rig();
    const port = await ready(r);
    port.drop();
    r.clock.advance(r.clock.armed()[0]);
    await tick();
    const second = r.ports[1];
    second.drop();
    assertEqual(r.clock.armed()[0], reconnectBackoffSeconds[1] * 1000, 'the second failure did not climb the ladder');
    r.clock.advance(r.clock.armed()[0]);
    await tick();
    const third = r.ports[2];
    third.deliver(answer(third.sent[0], helloResult()));
    await tick();
    assertEqual(r.client.state, 'ready', 'the third attempt did not negotiate');
    third.drop();
    assertEqual(r.clock.armed()[0], reconnectBackoffSeconds[0] * 1000, 'a good session did not reset the ladder');
  });

  await check('VersionMismatch is terminal: a reason, no reconnect, and nothing in flight survives', async () => {
    const r = rig();
    r.client.start();
    await tick();
    const port = r.ports[0];
    port.deliver(answer(port.sent[0], helloResult({ minCompatibleProtocolVersion: protocolVersion + 1 })));
    await tick();
    assertEqual(r.client.state, 'incompatible', 'an empty version intersection was not terminal');
    assert(/VersionMismatch/.test(r.client.stateReason), 'the panel has no reason to show');
    assertEqual(r.clock.armed().length, 0, 'a reconnect was scheduled after a version refusal');
    assert(port.isClosed(), 'the port was left open');
    await expectClientError('HandshakeFailed', () => r.client.request('core.status'), 'a command after a version refusal');
  });

  await check('after a reconnect the client reconciles by hash instead of reconfiguring', async () => {
    const r = rig();
    const hash = 'b'.repeat(64);
    r.client.recordAppliedConfigHash(hash);
    const port = await ready(r);
    await tick();
    assertEqual(port.sent[1].method, 'config.snapshot', 'the first session did not reconcile');
    port.deliver(answer(port.sent[1], { appliedConfigHash: hash, matchesExtensionHash: null, serverTags: [] }));
    await tick();
    assertEqual(JSON.stringify(r.reconciles), JSON.stringify([{ expected: hash, applied: hash, matches: true }]), 'the matching hash was not reported as matching');
    assertEqual(port.sent.length, 2, 'a reconcile issued a config.apply');

    port.drop();
    r.clock.advance(r.clock.armed()[0]);
    await tick();
    const second = r.ports[1];
    second.deliver(answer(second.sent[0], helloResult()));
    await tick();
    assertEqual(second.sent[1].method, 'config.snapshot', 'the reconnect did not reconcile');
    second.deliver(answer(second.sent[1], { appliedConfigHash: 'c'.repeat(64), matchesExtensionHash: null, serverTags: [] }));
    await tick();
    assertEqual(r.reconciles.length, 2, 'the second reconcile was not reported');
    assertEqual(r.reconciles[1].matches, false, 'a drifted hash was reported as matching');
    assertEqual(second.sent.length, 2, 'the drift caused a reconfigure the shell did not ask for');
  });

  await check('a snapshot that cannot be read is a mismatch, never an assumption that nothing changed', async () => {
    const r = rig();
    r.client.recordAppliedConfigHash('d'.repeat(64));
    const port = await ready(r);
    await tick();
    port.deliver(refusal(port.sent[1], 'CoreNotRunning'));
    await tick();
    assertEqual(r.reconciles.length, 1, 'a refused snapshot reported nothing');
    assertEqual(r.reconciles[0].applied, null, 'a refused snapshot invented a hash');
    assertEqual(r.reconciles[0].matches, false, 'a refused snapshot was reported as in sync');
  });

  await check('event and progress frames reach the listener, and an unknown event name is not refused', async () => {
    const r = rig();
    const port = await ready(r);
    port.deliver({ v: 1, id: 'evt-00000001', type: 'event', result: null, progress: { stage: 'refresh', done: 1, event: 'subscription.updated' }, sentAt: new Date(0).toISOString() });
    port.deliver({ v: 1, id: 'evt-00000002', type: 'event', result: null, progress: { stage: 'refresh', done: 2, event: 'an.event.from.the.future' }, sentAt: new Date(0).toISOString() });
    port.deliver({ v: 1, id: 'prg-00000001', type: 'progress', inReplyTo: 'req-00000000-0000000', progress: { stage: 'apply', done: 3 }, sentAt: new Date(0).toISOString() });
    await tick();
    assertEqual(r.events.length, 3, 'an unsolicited frame did not reach the listener');
    assertEqual(r.client.state, 'ready', 'an unknown event name disturbed the session');
  });

  await check('a connector refusal reaches the caller with its own code and retryability', async () => {
    const r = rig();
    const port = await ready(r);
    const request = r.client.request('config.apply', {});
    await tick();
    port.deliver(refusal(port.sent[1], 'InvalidProfile'));
    const error = await expectClientError('InvalidProfile', () => request, 'the refusal');
    assertEqual(error.retryable, false, 'InvalidProfile became retryable');
  });

  await check('a hello answered in the same task the port closed does not make a dead session ready', async () => {
    const r = rig();
    r.client.start();
    await tick();
    const port = r.ports[0];
    port.deliver(answer(port.sent[0], helloResult()));
    port.drop();
    await tick();
    assertEqual(r.client.state, 'waiting', 'a hello whose port closed in the same task was taken as a live session');
    r.clock.advance(r.clock.armed()[0]);
    await tick();
    const second = r.ports[1];
    second.deliver(answer(second.sent[0], helloResult()));
    await tick();
    assertEqual(r.client.state, 'ready', 'the reconnect did not negotiate');
    assertEqual(r.ports.length, 2, 'the stale hello opened a third port');
  });

  await check('stop cancels the reconnect and refuses what was in flight', async () => {
    const r = rig();
    const port = await ready(r);
    const request = r.client.request('core.status');
    await tick();
    r.client.stop();
    await expectClientError('NetworkUnreachable', () => request, 'the request stop took');
    assertEqual(r.client.state, 'stopped', 'stop did not settle the state');
    port.drop();
    assertEqual(r.clock.armed().length, 0, 'a stopped client scheduled a reconnect');
    assertEqual(r.ports.length, 1, 'a stopped client opened a port');
  });
};

await run();

console.log(`checks run: ${checks}`);
console.log(`  note: ${allCapabilities.length} capability names from the registry, ${Object.keys(methodTimeoutMs).length} method deadlines`);
if (failures.length > 0) {
  for (const failure of failures) console.error(`FAIL: ${failure}`);
  process.exitCode = 1;
} else {
  console.log('OK: the client negotiates before it commands, owns every deadline, discards a late answer by id, refuses past its windows before writing, and reconnects on the contract ladder to reconcile rather than reconfigure.');
}
