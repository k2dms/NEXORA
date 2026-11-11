// The verification harness of packages/protocol: it byte-compares the generated projection against a fresh
// generation, plays the frame codec over the corpus's own protocol fixtures in both directions, refuses the frames
// the contract says must be refused, and writes the fixture the Go mirror test reads.
// Run with `npm --prefix packages/protocol run verify`. Zero dependencies, as `docs/architecture/20-target-repository-layout.md` §3 asks of tooling.
import { execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  FrameFailure,
  createFrameDecoder,
  decodeEnvelope,
  encodeEnvelope,
  encodeFrame,
  limitFor,
} from '../src/frames.ts';
import {
  errorCodes,
  extensionToHostBytes,
  frameFormatVersion,
  hostName,
  hostToExtensionBytes,
  methodCapabilities,
  methodTimeoutMs,
  methods,
  prefixBytes,
  reconnectBackoffSeconds,
  reconnectJitterPercent,
  retryableErrors,
} from '../src/contract.ts';

const here = dirname(fileURLToPath(import.meta.url));
const packageRoot = resolve(here, '..');
const repoRoot = resolve(packageRoot, '..', '..');
const fixtureDir = join(repoRoot, 'packages', 'schema', 'fixtures', 'protocol');
const protocolDir = join(repoRoot, 'packages', 'schema', 'protocol');
const goldenFile = join(packageRoot, 'testdata', 'framed.jsonl');
const goProjection = join(repoRoot, 'connector', 'internal', 'contract', 'contract_gen.go');

let checks = 0;
const failures = [];

function check(name, run) {
  checks += 1;
  try {
    run();
  } catch (error) {
    failures.push(`${name} -- ${error.message}`);
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function assertEqual(actual, expected, message) {
  if (actual !== expected) throw new Error(`${message}: got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)}`);
}

const equalBytes = (a, b) => a.length === b.length && a.every((byte, i) => byte === b[i]);

function expectFailure(kind, run, message) {
  try {
    run();
  } catch (error) {
    assert(error instanceof FrameFailure, `${message}: threw ${error}`);
    assertEqual(error.kind, kind, `${message}: wrong failure kind`);
    return;
  }
  throw new Error(`${message}: nothing threw`);
}

const b64 = (bytes) => Buffer.from(bytes).toString('base64');
const unB64 = (text) => new Uint8Array(Buffer.from(text, 'base64'));

// The fixtures are the same 112 frames the schema harness replays as objects; here they are frames on a pipe.
const fixtures = readdirSync(fixtureDir)
  .filter((name) => name.endsWith('.jsonl'))
  .sort()
  .map((name) => ({
    name,
    envelopes: readFileSync(join(fixtureDir, name), 'utf8')
      .split('\n')
      .filter((line) => line.trim() !== '')
      .map((line) => JSON.parse(line)),
  }));

const frameCount = fixtures.reduce((total, file) => total + file.envelopes.length, 0);

check('the generated projection is byte-identical to a fresh generation', () => {
  const fresh = execFileSync(process.execPath, [join(here, 'gen.mjs'), '--stdout'], { encoding: 'utf8' });
  const committed = readFileSync(join(packageRoot, 'src', 'contract.ts'), 'utf8');
  assertEqual(fresh, committed, 'src/contract.ts has drifted from packages/schema/protocol; run `npm --prefix packages/protocol run gen`');
});

check('the projection carries the fixture frame count it is measured against', () => {
  assertEqual(frameCount, 112, 'the protocol fixtures changed size without the harness noticing');
});

check('every fixture frame crosses both directions and comes back the same object', () => {
  for (const direction of ['extension-to-host', 'host-to-extension']) {
    for (const file of fixtures) {
      const decoder = createFrameDecoder(limitFor(direction));
      let seen = 0;
      for (const envelope of file.envelopes) {
        for (const payload of decoder.push(encodeEnvelope(envelope, direction))) {
          assertEqual(JSON.stringify(decodeEnvelope(payload)), JSON.stringify(envelope), `${file.name} frame ${seen} did not round-trip`);
          seen += 1;
        }
      }
      decoder.end();
      assertEqual(seen, file.envelopes.length, `${file.name} lost a frame in the ${direction} direction`);
    }
  }
});

check('one byte at a time gives the same frames as one whole frame at a time', () => {
  const bytes = fixtures[0].envelopes.map((envelope) => encodeEnvelope(envelope, 'host-to-extension'));
  const stream = new Uint8Array(bytes.reduce((total, frame) => total + frame.byteLength, 0));
  let offset = 0;
  for (const frame of bytes) {
    stream.set(frame, offset);
    offset += frame.byteLength;
  }
  const decoder = createFrameDecoder(hostToExtensionBytes);
  const payloads = [];
  for (const byte of stream) payloads.push(...decoder.push(new Uint8Array([byte])));
  decoder.end();
  assertEqual(payloads.length, bytes.length, 'byte-at-a-time decoding lost a frame');
  for (let i = 0; i < payloads.length; i += 1) {
    assert(equalBytes(payloads[i], bytes[i].slice(prefixBytes)), `frame ${i} differs when the pipe hands one byte at a time`);
  }
});

check('the prefix is four unsigned little-endian bytes of the payload length, excluding itself', () => {
  const payload = new TextEncoder().encode('{"v":1}');
  const frame = encodeFrame(payload, 'extension-to-host');
  assertEqual(frame.byteLength, payload.byteLength + prefixBytes, 'the frame carries its prefix');
  assertEqual(new DataView(frame.buffer, frame.byteOffset, prefixBytes).getUint32(0, true), payload.byteLength, 'the prefix is the payload length');
  assertEqual([...frame.slice(0, prefixBytes)].join(','), '7,0,0,0', 'the length is little-endian, not network order');
});

check('a frame over the extension-to-host ceiling is refused before any byte is written', () => {
  const oversized = new Uint8Array(extensionToHostBytes + 1);
  oversized.fill(0x20);
  let written = null;
  expectFailure('too-large', () => {
    try {
      written = encodeFrame(oversized, 'extension-to-host');
    } finally {
      assertEqual(written, null, 'bytes came out of a refused write');
    }
  }, '64 MiB + 1 out of the extension');
  expectFailure('too-large', () => createFrameDecoder(extensionToHostBytes).push(Uint8Array.from([1, 0, 0, 4, 0x7b])), 'a peer declaring a frame over the ceiling');
});

check('the same payload one byte under the ceiling is legal, and the other direction is the 1 MB the browsers state', () => {
  const atLimit = new Uint8Array(extensionToHostBytes).fill(0x20);
  assertEqual(encodeFrame(atLimit, 'extension-to-host').byteLength, extensionToHostBytes + prefixBytes, 'a payload exactly at the ceiling writes');
  assertEqual(limitFor('host-to-extension'), hostToExtensionBytes, 'the ceiling back into the browser is the 1 MB every family documents');
  assertEqual(limitFor('extension-to-host'), extensionToHostBytes, 'the ceiling out of the browser is Chrome 64 MiB, the smallest claim in that direction');
});

check('an empty payload is not a frame, in either direction', () => {
  expectFailure('empty', () => encodeFrame(new Uint8Array(0), 'extension-to-host'), 'writing an empty payload');
  expectFailure('empty', () => createFrameDecoder(64).push(Uint8Array.from([0, 0, 0, 0])), 'a peer declaring an empty payload');
});

check('a pipe that ends inside a frame is a truncation, and one that never carried a frame is a closed peer', () => {
  const half = createFrameDecoder(64);
  half.push(Uint8Array.from([3, 0, 0, 0, 0x7b]));
  expectFailure('truncated', () => half.end(), 'EOF three bytes into a declared frame');
  expectFailure('closed', () => createFrameDecoder(64).end(), 'EOF before any byte');
});

check('a broken pipe stays broken rather than decoding what follows the refusal', () => {
  const decoder = createFrameDecoder(4);
  expectFailure('too-large', () => decoder.push(Uint8Array.from([9, 0, 0, 0])), 'a peer declaring nine bytes to a four-byte limit');
  expectFailure('closed', () => decoder.push(Uint8Array.from([0x7b, 0x7d])), 'a push after the refusal');
});

check('a frame is one object: arrays and null are refused on both sides of the pipe', () => {
  expectFailure('not-an-object', () => encodeEnvelope([1, 2], 'extension-to-host'), 'an array out');
  expectFailure('not-an-object', () => encodeEnvelope(null, 'extension-to-host'), 'a null out');
  expectFailure('not-an-object', () => decodeEnvelope(new TextEncoder().encode('[1,2]')), 'an array in');
});

check('the projection closes the method and error lists and declares retryability as data', () => {
  assertEqual(methods.length, 19, 'the command allowlist is nineteen methods');
  assertEqual(errorCodes.length, 23, 'the error taxonomy is twenty-three closed codes');
  assertEqual(Object.keys(retryableErrors).length, errorCodes.length, 'every code declares whether it is retryable');
  assertEqual(typeof frameFormatVersion, 'number', 'the frame version is a number, not a comment');
});

// The deadline and the gate are the extension's to enforce, so the projection has to carry them verbatim rather
// than let a client type them again.
check('the projection carries every method deadline and capability gate from its own command document', () => {
  assertEqual(Object.keys(methodTimeoutMs).length, methods.length, 'a method has no deadline projected');
  assertEqual(Object.keys(methodCapabilities).length, methods.length, 'a method has no capability gate projected');
  for (const name of methods) {
    const doc = JSON.parse(readFileSync(join(protocolDir, 'commands', `${name}.json`), 'utf8'));
    assertEqual(methodTimeoutMs[name], doc.timeoutMs, `${name} carries a deadline its command document does not`);
    assertEqual(JSON.stringify(methodCapabilities[name]), JSON.stringify(doc.capabilities ?? []), `${name} carries a gate its command document does not`);
  }
});

check('the projection names the host and the reconnect policy the framing contract declares', () => {
  const framing = JSON.parse(readFileSync(join(protocolDir, 'framing.json'), 'utf8'));
  assertEqual(hostName, framing.senderPolicy.identity.hostName, 'the client would connectNative a name the installer did not register');
  assertEqual(JSON.stringify(reconnectBackoffSeconds), JSON.stringify(framing.reconnect.backoffSeconds), 'the backoff ladder drifted');
  assertEqual(reconnectJitterPercent, framing.reconnect.jitterPercent, 'the jitter drifted');
});

// The cross-language half: the Go projection and this one read the same JSON, so a divergence between the two
// projections is a bug in a generator, not a difference of opinion.
check('the Go projection and this one list the same methods and codes', () => {
  const go = readFileSync(goProjection, 'utf8');
  const codesStart = go.indexOf('var ErrorCodes');
  assert(codesStart > 0, 'the Go projection does not carry an ErrorCodes block');
  const goMethods = [...go.slice(0, codesStart).matchAll(/\t\{Name: "([^"]+)"/g)].map((match) => match[1]);
  const goCodes = [...go.slice(codesStart).matchAll(/\t\{Name: "([^"]+)"/g)].map((match) => match[1]);
  assertEqual(goMethods.length, methods.length, 'the method lists differ in length');
  for (let i = 0; i < methods.length; i += 1) assertEqual(goMethods[i], methods[i], `method ${i} differs from connector/internal/contract`);
  for (let i = 0; i < errorCodes.length; i += 1) assertEqual(goCodes[i], errorCodes[i], `error code ${i} differs from connector/internal/contract`);
});

// The fixture the Go mirror test reads: the exact payload bytes this codec put on the pipe and the exact prefix it
// wrote in front of them, so `connector/internal/frames` is compared against this implementation's answer rather
// than against a re-derivation of its own.
const golden = fixtures.flatMap((file) =>
  file.envelopes.map((envelope, index) => {
    const payload = new TextEncoder().encode(JSON.stringify(envelope));
    const frame = encodeFrame(payload, 'extension-to-host');
    return {
      file: file.name,
      frame: index + 1,
      direction: 'extension-to-host',
      payloadBytes: payload.byteLength,
      prefixHex: Buffer.from(frame.slice(0, prefixBytes)).toString('hex'),
      payloadB64: b64(payload),
    };
  }),
);

check('the mirror fixture covers every fixture frame and is byte-stable', () => {
  assertEqual(golden.length, frameCount, 'the mirror fixture dropped a frame');
  mkdirSync(dirname(goldenFile), { recursive: true });
  const text = `${golden.map((record) => JSON.stringify(record)).join('\n')}\n`;
  if (process.argv.includes('--write') || !exists(goldenFile)) writeFileSync(goldenFile, text, 'utf8');
  assertEqual(readFileSync(goldenFile, 'utf8'), text, 'testdata/framed.jsonl has drifted from this codec; run the harness with --write');
});

function exists(path) {
  try {
    readFileSync(path);
    return true;
  } catch {
    return false;
  }
}

console.log(`checks run: ${checks}`);
console.log(`  note: ${frameCount} protocol frames from ${fixtures.length} fixture files, ${golden.length} mirror records`);
if (failures.length > 0) {
  for (const failure of failures) console.error(`FAIL: ${failure}`);
  process.exitCode = 1;
} else {
  console.log('OK: the projection matches the contract data, the codec frames and refuses what the framing contract says, and the Go projection agrees on the method and code lists.');
}
