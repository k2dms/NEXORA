#!/usr/bin/env node
// The executable part of Phase 1. Architecture documents are prose; this script is
// what makes them checkable. It compiles every contract, validates every example,
// asserts every invalid example is rejected, and enforces the rules that JSON Schema
// cannot express on its own.
//
// Exit codes: 0 all checks pass, 1 a check failed, 2 the harness itself broke.

import { execFileSync } from 'node:child_process';
import { readFile, readdir } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { basename, dirname, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

import { createHash } from 'node:crypto';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';

const PKG = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const REPO = resolve(PKG, '..', '..');

const failures = [];
const notes = []; let labels = new Set();
let checks = 0;

function check(ok, label, detail) {
  checks++; labels.add(label);
  if (!ok) failures.push({ label, detail });
  return ok;
}

function note(text) {
  notes.push(text);
}

async function readJson(path) {
  return JSON.parse(await readFile(path, 'utf8'));
}

async function* walk(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) yield* walk(path);
    else yield path;
  }
}

// ---------------------------------------------------------------- load + compile

const ajv = new Ajv2020({ strict: false, allErrors: true });
addFormats(ajv);

const schemaFiles = [];
for await (const file of walk(PKG)) {
  if (file.endsWith('.schema.json')) schemaFiles.push(file);
}
schemaFiles.sort();

for (const file of schemaFiles) {
  const doc = await readJson(file);
  try {
    ajv.addSchema(doc);
  } catch (err) {
    console.error(`cannot compile ${relative(REPO, file)}: ${err.message}`);
    process.exit(2);
  }
}
note(`compiled ${schemaFiles.length} schemas`);

// The non-schema data documents are referenced by $id from schemas, so they must be
// added to ajv as documents too -- errors, framing and compatibility rules are data
// with a shape, and a typo in them should fail here rather than in a browser.
const dataDocs = {};
for (const name of [
  'protocol/error-taxonomy.json',
  'protocol/framing.json',
  'protocol/compatibility-rules.json',
  'protocol/command-allowlist.json',
]) {
  dataDocs[name] = await readJson(join(PKG, name));
}

// ---------------------------------------------------------------- command docs

const commandDir = join(PKG, 'protocol', 'commands');
const commandFiles = (await readdir(commandDir)).filter((f) => f.endsWith('.json')).sort();
const commandDocs = new Map();
const commandParts = new Map();
const commandMeta = ajv.getSchema('https://schema.nexora.local/protocol/commands.schema.json');
const eventNames = new Set(
  (await readJson(join(PKG, 'events.schema.json'))).$defs.name.enum,
);

for (const file of commandFiles) {
  const doc = await readJson(join(commandDir, file));
  commandDocs.set(doc.name, { file, doc });
  const valid = commandMeta(doc);
  check(
    valid,
    `command doc conforms to meta-schema: ${doc.name}`,
    valid ? '' : JSON.stringify(commandMeta.errors?.slice(0, 4)),
  );

  // args/result must themselves be compilable JSON Schema -- a hand-written
  // fragment that ajv rejects is not a contract anyone can implement against.
  // The compiled validators are kept, because the protocol fixtures below are
  // checked against them: a fixture frame that does not match its own command
  // document is a documentation bug, not a passing test.
  const parts = commandParts.get(doc.name) ?? {};
  for (const part of ['args', 'result']) {
    try {
      parts[part] = ajv.compile({ ...doc[part], $id: `https://schema.nexora.local/tmp/${doc.name}.${part}` });
      checks++;
    } catch (err) {
      check(false, `command ${doc.name} ${part} is not valid JSON Schema`, err.message);
    }
  }
  commandParts.set(doc.name, parts);

  const allowEntries = new Map(
  dataDocs['protocol/command-allowlist.json'].commands.map((c) => [c.name, c]),
);
  const taxonomy = new Set(dataDocs['protocol/error-taxonomy.json'].codes.map((c) => c.code));
  const unknownCodes = doc.errors.filter((c) => !taxonomy.has(c));
  check(
    unknownCodes.length === 0,
    `command ${doc.name} cites only taxonomy codes`,
    `unknown: ${unknownCodes.join(', ')}`,
  );

  const caps = new Set(
    dataDocs['protocol/compatibility-rules.json'].capabilities.map((c) => c.name),
  );
  const unknownCaps = doc.capabilities.filter((c) => !caps.has(c));
  check(
    unknownCaps.length === 0,
    `command ${doc.name} cites only declared capabilities`,
    `unknown: ${unknownCaps.join(', ')}`,
  );

  const unknownEvents = (doc.progressEvents ?? []).filter((e) => !eventNames.has(e));
  check(
    unknownEvents.length === 0,
    `command ${doc.name} cites only registered events`,
    `unknown: ${unknownEvents.join(', ')}`,
  );

  if (doc.idempotent === false) {
    check(
      typeof doc.nonIdempotencyRationale === 'string' && doc.nonIdempotencyRationale.length > 20,
      `non-idempotent command carries a rationale: ${doc.name}`,
      'idempotent=false requires nonIdempotencyRationale',
    );
  }
  if (doc.class === 'probe') {
    check(doc.replayable === false, `probe is non-replayable: ${doc.name}`, 'timings must not be cached');
  }
  // The allowlist is normative for dispatch, the command doc is normative for the
  // handler. If they disagree about hello, one of the two implementations deadlocks:
  // the host answers a pre-hello frame the handler refuses, or vice versa.
  const entry = allowEntries.get(doc.name);
  if (entry) {
    check(
      doc.requiresHello === !entry.availableBeforeHello,
      `hello gating agrees between allowlist and command doc: ${doc.name}`,
      `allowlist availableBeforeHello=${entry.availableBeforeHello} but requiresHello=${doc.requiresHello}`,
    );
    check(
      doc.class === entry.class,
      `command class agrees between allowlist and command doc: ${doc.name}`,
      `allowlist says ${entry.class}, command doc says ${doc.class}`,
    );
  }
}

// ---------------------------------------------------------------- allowlist parity

const allowlist = dataDocs['protocol/command-allowlist.json'];
const allowNames = allowlist.commands.map((c) => c.name).sort();
const docNames = [...commandDocs.keys()].sort();
check(
  JSON.stringify(allowNames) === JSON.stringify(docNames),
  'command-allowlist.json and protocol/commands/*.json name the same set',
  `only-in-allowlist: ${allowNames.filter((n) => !docNames.includes(n)).join(', ')}; ` +
    `only-as-file: ${docNames.filter((n) => !allowNames.includes(n)).join(', ')}`,
);
check(allowNames.length === 19, 'the command surface is exactly nineteen methods', `found ${allowNames.length}`);
check(
  allowlist.commands.filter((c) => c.availableBeforeHello).map((c) => c.name).join() ===
    'connector.hello',
  'nothing is dispatchable before negotiation except connector.hello',
  'a command callable pre-hello can be misread by an incompatible Connector',
);

// The command-surface document, once written, must agree. Deferred rather than
// silently skipped, so the gap is visible in the output.
const surfaceDoc = join(REPO, 'docs', 'architecture', '26-command-surface.md');
if (existsSync(surfaceDoc)) {
  const text = await readFile(surfaceDoc, 'utf8');
  const cited = [...new Set(text.match(/`[a-z]+\.[a-z][A-Za-z0-9]*(?:\.[a-z][A-Za-z0-9]*)*`/g) ?? [])]
    .map((s) => s.replace(/[`]/g, ''))
    .filter((n) => allowNames.includes(n))
    .sort();
  check(
    JSON.stringify(cited) === JSON.stringify(allowNames),
    'docs/architecture/26-command-surface.md names the same set',
    `missing from doc: ${allowNames.filter((n) => !cited.includes(n)).join(', ')}`,
  );
} else {
  note('DEFERRED: 26-command-surface.md cross-check (file not present yet)');
}

// ---------------------------------------------------------------- taxonomy sanity

const taxonomy = dataDocs['protocol/error-taxonomy.json'];
{
  const codes = taxonomy.codes.map((c) => c.code);
  // An entry must be a described object, not a bare string: `codes.every(c =>
  // typeof c.retryable === 'boolean')` below is vacuously true for an array of
  // strings, and that is the exact way this table could rot into a list of names.
  check(
    taxonomy.codes.every(
      (c) =>
        c !== null &&
        typeof c === 'object' &&
        !Array.isArray(c) &&
        typeof c.code === 'string' &&
        typeof c.meaning === 'string' &&
        typeof c.layer === 'string',
    ),
    'every taxonomy entry is a code/layer/meaning object',
    `non-objects: ${taxonomy.codes.filter((c) => typeof c !== 'object' || c === null || typeof c.code !== 'string').map((c) => JSON.stringify(c)).join(', ')}`,
  );
  check(new Set(codes).size === codes.length, 'error codes are unique', `dupes in ${codes}`);
  check(codes.length >= 20, 'the taxonomy covers the documented surface', `${codes.length} codes`);
  check(
    taxonomy.codes.every((c) => typeof c.retryable === 'boolean'),
    'every error code declares retryability',
    'a client cannot back off on an undeclared code',
  );
}

// ---------------------------------------------------------------- framing sanity

const framing = await readJson(join(PKG, 'protocol', 'framing.json'));
for (const name of ['windowSeconds', 'maxEntries', 'maxBytes']) {
  const value = framing.dedupeWindow?.[name];
  check(
    Number.isSafeInteger(value) && value > 0,
    `dedupe window has a positive integer bound: ${name}`,
    `${name}=${value}; response dedupe must be bounded by contract data`,
  );
}
{
  check(
    framing.limits.extensionToHostBytes.value === 67108864,
    'extension-to-host frame limit is 64 MiB',
    'Chrome documents 64 MiB in this direction while Firefox and Edge document 4 GB, so Chrome is the binding claim (docs/architecture/25 §2.1)',
  );
  check(
    framing.limits.effectiveCrossBrowserBytes.value <= framing.limits.hostToExtensionBytes.value,
    'the effective cross-browser limit is not larger than the smallest documented cap',
    'every browser family caps what the host writes back at 1 MB, so that is the number a design may rely on',
  );
  check(
    framing.limits.hostToExtensionBytes.value === 1048576,
    'host-to-extension cap is the binding 1 MB',
    'the corpus held the two directions swapped, in the pre-Chrome-44 pair, until 2026-11-10 -- docs/PROJECT-STATE.md §4.54',
  );
  check(
    framing.oversizedPayloads.decision === 'chunked-envelope',
    'oversized payloads are chunked, not handed off out-of-band',
    'ADR-0004: a temp file is not an authenticated channel',
  );
  // Two floors rather than one, because the two ends of the port are replaced on different schedules
  // (docs/architecture/22-component-contracts.md §2 rule 4): the browser-side floor is negotiated in hello, the pipe
  // floor is the `v` field every frame carries. This is the reading that makes `VER-18`'s closure a check instead of a
  // sentence, so it checks the shape rather than the values: four distinct numbers, each floor at or below the version
  // it floors, one refusal code and that code in the taxonomy.
  const floors = framing.versionFloors;
  const floorPairs = ['protocol', 'pipe'].map((k) => [floors?.[k]?.own, floors?.[k]?.floor]);
  check(
    !!floors &&
      floorPairs.every(([own, floor]) => typeof framing[own] === 'number' && typeof framing[floor] === 'number') &&
      new Set(floorPairs.flat()).size === 4 &&
      floorPairs.every(([own, floor]) => framing[floor] <= framing[own]) &&
      taxonomy.codes.some((c) => c.code === floors.tooOld),
    'the pipe floor and the protocol floor are separate numbers in the framing contract',
    `protocol ${framing.protocolVersion}..${framing.minCompatibleProtocolVersion}, ` +
      `pipe ${framing.frameFormatVersion}..${framing.minPipeVersion}, tooOld=${floors?.tooOld ?? '(absent)'}`,
  );
}

// ---------------------------------------------------------------- entity examples

const SCHEMAS_BY_PREFIX = {
  server: 'https://schema.nexora.local/server.schema.json',
  profile: 'https://schema.nexora.local/connector-profile.schema.json',
  routing: 'https://schema.nexora.local/routing-policy.schema.json',
  subscription: 'https://schema.nexora.local/subscription-source.schema.json',
  'envelope-keys': 'https://schema.nexora.local/envelope-keys.schema.json',
  'failure-policy': 'https://schema.nexora.local/failure-policy.schema.json',
  events: 'https://schema.nexora.local/events.schema.json',
  pins: 'https://schema.nexora.local/pins.schema.json',
  settings: 'https://schema.nexora.local/connector-settings.schema.json',
  'import-staging': 'https://schema.nexora.local/import-staging.schema.json',
  'envelope-backup': 'https://schema.nexora.local/envelope-backup.schema.json',
  envelope: 'https://schema.nexora.local/protocol/envelope.schema.json',
};

function schemaIdFor(file) {
  const stem = basename(file).split('.')[0];
  const id = SCHEMAS_BY_PREFIX[stem];
  if (!id) throw new Error(`no schema mapped for example prefix "${stem}" in ${file}`);
  return id;
}

const examplesDir = join(PKG, 'examples');
const validExamples = [];
const invalidDir = join(examplesDir, 'invalid');

for (const file of (await readdir(examplesDir)).filter((f) => f.endsWith('.json')).sort()) {
  validExamples.push(join(examplesDir, file));
}

let validCount = 0;
for (const file of validExamples) {
  const doc = await readJson(file);
  const id = schemaIdFor(file);
  const validate = ajv.getSchema(id);
  const ok = validate(doc);
  check(ok, `valid example accepted: ${basename(file)}`, ok ? '' : JSON.stringify(validate.errors?.slice(0, 3)));
  if (ok) validCount++;
}

// Every invalid case declares why it must be rejected. Without that, a typo in the
// fixture could "pass" by violating some unrelated rule and the intended trap would
// go untested.
const reasons = await readJson(join(invalidDir, 'reasons.json'));
let rejectedCount = 0;

for (const [caseName, reason] of Object.entries(reasons.cases)) {
  const path = join(invalidDir, `${caseName}.json`);
  if (!existsSync(path)) {
    check(false, `invalid case file exists: ${caseName}`, path);
    continue;
  }
  const doc = await readJson(path);
  const id = SCHEMAS_BY_PREFIX[reason.schema];
  const validate = ajv.getSchema(id);
  const rejected = !validate(doc);
  check(rejected, `invalid case rejected: ${caseName}`, reason.mustReject);
  if (rejected) {
    rejectedCount++;
    const keywords = (validate.errors ?? []).map((e) => e.keyword);
    check(
      keywords.includes(reason.keyword),
      `rejected for the documented reason: ${caseName}`,
      `expected "${reason.keyword}" among ${[...new Set(keywords)].join(', ')}`,
    );
  }
}

check(validCount >= 15, 'at least fifteen valid examples', `found ${validCount}`);
check(rejectedCount >= 10, 'at least ten invalid examples', `found ${rejectedCount}`);

// ---------------------------------------------------------------- cross-field rules

for (const file of validExamples) {
  const doc = await readJson(file);
  if (doc.serverIds && doc.selection) {
    check(
      !doc.defaultServerId || doc.serverIds.includes(doc.defaultServerId),
      `defaultServerId is one of serverIds: ${basename(file)}`,
      `${doc.defaultServerId} not in [${doc.serverIds}]`,
    );
  }
  if (doc.serverIds && doc.id) {
    const refs = doc.serverIds ?? [];
    check(new Set(refs).size === refs.length, `serverIds unique: ${basename(file)}`, 'dupes');
  }
}

const envelopeKeys = await readJson(join(examplesDir, 'envelope-keys.nexora.json'));
for (const ns of envelopeKeys.namespaces) {
  check(
    !ns.keyPattern.startsWith('\\+') && !ns.keyPattern.startsWith('+'),
    `namespace stays out of the profile prefix: ${ns.entity}`,
    "keys starting with '+' would be iterated by Profiles.each (omega-pac/src/profiles.coffee:111-118)",
  );
  check(
    !(ns.secretBearing && ns.syncEligible),
    `secret-bearing namespace is not sync-eligible: ${ns.entity}`,
    'this is SEC-1 expressed as data',
  );
  const schemaPath = join(REPO, 'packages', 'schema', ns.valueSchema);
  check(existsSync(schemaPath), `namespace value schema exists: ${ns.entity}`, ns.valueSchema);
}
check(
  envelopeKeys.syncExcludedValueKeys.includes('headers'),
  'syncExcludedValueKeys strips headers',
  'the legacy list omits it (omega-target/src/options.coffee:27)',
);

// Description coverage: an undocumented field is a field no component owns (plan 6).
for (const [name, id] of Object.entries(SCHEMAS_BY_PREFIX)) {
  if (name === 'events' || name === 'envelope-keys') continue;
  const schema = ajv.getSchema(id);
  const props = schema?.schema?.properties ?? {};
  const missing = Object.entries(props)
    .filter(([, v]) => typeof v?.description !== 'string')
    .map(([k]) => k);
  check(
    missing.length === 0,
    `every top-level field is described in ${name}.schema.json`,
    `undocumented: ${missing.join(', ')}`,
  );
}

// ---------------------------------------------------------------- protocol fixtures

const fixturesDir = join(PKG, 'fixtures', 'protocol');
const envelopeSchema = ajv.getSchema('https://schema.nexora.local/protocol/envelope.schema.json');
let frameCount = 0;
let replayPairs = 0;
// Kept aside so the cross-frame rules below (chunk reassembly, capability
// negotiation, hash identity, retryability) can reason over whole scenes rather
// than one frame at a time. A chunk group that never arrives is invisible to a
// per-frame check, which is exactly how a vacuous test is born.
const framesByFile = new Map();

for (const file of (await readdir(fixturesDir)).filter((f) => f.endsWith('.jsonl')).sort()) {
  const text = await readFile(join(fixturesDir, file), 'utf8');
  const frames = text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => JSON.parse(l));
  framesByFile.set(file, frames);

  const requests = frames.filter((f) => f.type === 'request');
  const responses = frames.filter((f) => f.type === 'response');
  const methodOf = (frame) =>
    frame.method ??
    requests.find((r) => r.id === frame.inReplyTo)?.method ??
    undefined;

  for (const frame of frames) {
    const ok = envelopeSchema(frame);
    check(ok, `fixture frame conforms to envelope: ${file}#${frame.id}`, ok ? '' : JSON.stringify(envelopeSchema.errors?.slice(0, 3)));
    frameCount++;

    const method = methodOf(frame);
    const parts = method ? commandParts.get(method) : undefined;

    // A frame is only worth recording if it would actually compile against the
    // command it claims to exercise. This is what stops a fixture from silently
    // describing an invented field, which is the failure mode a prose spec has.
    if (frame.type === 'request' && parts?.args && frame.args !== undefined && frame.args !== null) {
      const answers = responses.filter((r) => r.inReplyTo === frame.id);
      // A request that ended in BadRequest/SchemaMismatch is supposed to carry args
      // the command rejects; validating them would assert the opposite of the scene.
      const succeeded = answers.some((r) => r.ok === true);
      if (succeeded || answers.length === 0) {
        const a = parts.args(frame.args);
        check(a, `fixture args match ${method}: ${file}#${frame.id}`, a ? '' : JSON.stringify(parts.args.errors?.slice(0, 3)));
      }
    }
    if (frame.type === 'response' && frame.ok === true && parts?.result) {
      const r = parts.result(frame.result);
      check(r, `fixture result matches ${method}: ${file}#${frame.id}`, r ? '' : JSON.stringify(parts.result.errors?.slice(0, 3)));
    }
    if (frame.type === 'event') {
      check(
        eventNames.has(frame.progress?.event),
        `fixture event name is registered: ${file}#${frame.id}`,
        `progress.event = ${frame.progress?.event}`,
      );
    }
    if (frame.type === 'response' && frame.ok === false) {
      check(
        taxonomy.codes.some((c) => c.code === frame.error.code),
        `fixture error code is in the taxonomy: ${file}#${frame.id}`,
        frame.error.code,
      );
      // UnknownMethod is the one error a method outside the allowlist may produce;
      // anything else must be a code the command itself declares it can return.
      if (parts && frame.error.code !== 'UnknownMethod') {
        const declared = commandDocs.get(method)?.doc.errors ?? [];
        check(
          declared.includes(frame.error.code),
          `fixture error code is declared by ${method}: ${file}#${frame.id}`,
          `${frame.error.code} not in [${declared.join(', ')}]`,
        );
      }
    }
  }

  for (const req of requests) {
    const answers = responses.filter((r) => r.inReplyTo === req.id);
    check(answers.length >= 1, `every request is answered: ${file}#${req.id}`, 'no response frame');
    const isProbe = commandDocs.get(req.method)?.doc.class === 'probe';
    if (isProbe) {
      check(answers.length === 1, `probe is answered exactly once: ${file}#${req.id}`, 'a cached probe is a stale probe');
    }
    if (answers.length >= 2) {
      replayPairs++;
      const [a, b] = answers;
      const strip = (r) => JSON.stringify(r.result, (k, v) => (/(At|Ms|Seconds|seq)$/i.test(k) ? undefined : v));
      check(
        strip(a) === strip(b),
        `idempotent replay returns the same result: ${file}#${req.id}`,
        'a replay that changes the answer is not idempotent',
      );
    }
  }
}
check(frameCount >= 20, 'protocol fixtures cover the surface', `${frameCount} frames`);
check(replayPairs >= 4, 'idempotent replay is exercised', `${replayPairs} replay pairs`);

// ---------------------------------------------------------------- protocol enum
//
// Adding a transport or a protocol is a new Core dependency and a new compatibility surface, so the process
// rule (docs/architecture/23-unified-server-model.md §10, ADR-0003) is that the enums move by ADR only. This
// assertion pins the shipped literals: a schema edit that widens them without the ADR -- or an ADR whose
// member list disagrees with this literal -- is a red run, which is what makes 'requires an ADR' mean
// something (VER-71).
{
  const server = await readJson(join(PKG, 'server.schema.json'));
  check(
    server.properties.protocol.const === 'vless' &&
      JSON.stringify(server.properties.network.enum) === JSON.stringify(['tcp', 'ws', 'grpc', 'xhttp', 'h2']) &&
      /ADR/.test(server.properties.network.description) &&
      /ADR/.test(server.properties.protocol.description),
    'the protocol enum equals the five shipped transports, and only an ADR changes it',
    `protocol=${JSON.stringify(server.properties.protocol)} network=${JSON.stringify(server.properties.network.enum)}`,
  );
}

// ---------------------------------------------------------------- chunked import
//
// The authoritative slice shape is protocol/envelope.schema.json#/properties/chunk:
// contentHash / chunkSeq / chunkCount / encoding live NESTED under `chunk` on an
// ordinary request frame. There is no top-level chunkId field and no `type: "chunk"`
// frame, and protocol/commands/profile.import.json mirrors the nested object inside
// its args. Everything below turns that from a description into a test: the group
// rules fail loudly when a group is absent, short, duplicated or split across two
// hashes, which is precisely what a per-frame check cannot see.

{
  const envChunk = envelopeSchema.schema.properties.chunk;
  const importDoc = commandDocs.get('profile.import').doc;
  const mirror = (importDoc.args.allOf ?? [])
    .map((branch) => branch?.then?.properties?.chunk)
    .find((c) => c && typeof c === 'object');
  check(
    mirror !== undefined,
    'profile.import declares a nested args.chunk mirror',
    'no chunk object under args -- the command doc no longer describes a slice',
  );

  const shapeOf = (c) =>
    JSON.stringify({
      required: [...(c?.required ?? [])].sort(),
      additionalProperties: c?.additionalProperties,
      fields: Object.fromEntries(
        Object.entries(c?.properties ?? {})
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([k, v]) => [k, { type: v.type, pattern: v.pattern, minimum: v.minimum, enum: v.enum }]),
      ),
    });
  if (mirror) {
    check(
      shapeOf(envChunk) === shapeOf(mirror),
      'the envelope chunk shape and profile.import args.chunk are one shape',
      `envelope=${shapeOf(envChunk)}\n      args=${shapeOf(mirror)}`,
    );
  }
  check(
    envChunk.properties.chunkCount.minimum >= 2,
    'a chunked frame declares at least two slices',
    `envelope chunkCount minimum is ${envChunk.properties.chunkCount.minimum}; one slice is not a chunked transfer`,
  );
  check(
    (envChunk.required ?? []).includes('encoding'),
    'every slice states its encoding instead of inheriting a default',
    `chunk required: ${(envChunk.required ?? []).join(', ')}`,
  );

  // Count the slices straight off the bytes on disk, independently of the parsed
  // frames, so the group analysis below is tied to the fixture files.
  let rawSliceCount = 0;
  for (const file of (await readdir(fixturesDir)).filter((f) => f.endsWith('.jsonl')).sort()) {
    const raw = await readFile(join(fixturesDir, file), 'utf8');
    for (const line of raw.split('\n').map((l) => l.trim()).filter(Boolean)) {
      if (/"type":"request","method":"profile\.import"/.test(line) && /"chunk":\{/.test(line)) {
        rawSliceCount++;
      }
    }
  }

  const slices = [];
  for (const [file, frames] of framesByFile) {
    for (const frame of frames) {
      if (frame.type === 'request' && frame.method === 'profile.import') {
        const onEnvelope = frame.chunk !== null && typeof frame.chunk === 'object';
        const onArgs = frame.args?.chunk !== null && typeof frame.args?.chunk === 'object';
        check(
          onEnvelope === onArgs,
          `an import slice declares the chunk in both places or neither: ${file}#${frame.id}`,
          `envelope.chunk=${onEnvelope} args.chunk=${onArgs}`,
        );
        if (onEnvelope) slices.push({ file, frame, frames });
      }
    }
  }
  check(slices.length > 0, 'a chunked profile.import slice exists in the fixtures', 'no frame carried envelope.chunk, so the chunked contract was never exercised');
  check(
    slices.length === rawSliceCount,
    `the ${rawSliceCount} chunked import frames on disk are all in the group analysis`,
    `analysis counted ${slices.length}`,
  );

  const groups = new Map();
  for (const slice of slices) {
    const key = `${slice.file}:${slice.frame.id}`;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(slice);
  }
  check(groups.size > 0, 'at least one chunked import group is present', `${groups.size} groups`);

  let cleanGroups = 0;
  let splitGroups = 0;
  for (const [key, members] of groups) {
    const counts = [...new Set(members.map((m) => m.frame.chunk.chunkCount))];
    check(
      counts.length === 1,
      `one chunk group cannot disagree about its own size: ${key}`,
      `chunkCount values ${counts.join(', ')}`,
    );
    const declared = counts[0];
    check(
      members.length === declared,
      `chunk group delivered every slice it declared: ${key}`,
      `${members.length} of ${declared} frames arrived -- a missing slice is a failure, not a shorter group`,
    );
    const seqs = members.map((m) => m.frame.chunk.chunkSeq);
    check(
      new Set(seqs).size === seqs.length,
      `chunk group has no duplicate chunkSeq: ${key}`,
      `seqs ${seqs.join(', ')}`,
    );
    const covered = Array.from({ length: declared }, (_, i) => i);
    check(
      JSON.stringify([...seqs].sort((a, b) => a - b)) === JSON.stringify(covered),
      `chunkSeq covers 0..chunkCount-1 exactly once: ${key}`,
      `declared ${declared} slices, seqs ${[...seqs].sort((a, b) => a - b).join(', ')}`,
    );

    const hashes = new Set();
    for (const m of members) {
      const { chunk } = m.frame;
      check(
        typeof chunk.contentHash === 'string' && /^[0-9a-f]{64}$/.test(chunk.contentHash),
        `chunk contentHash is sha256 hex: ${key}#${m.frame.seq}`,
        String(chunk.contentHash),
      );
      hashes.add(chunk.contentHash);
      check(
        JSON.stringify(m.frame.args?.chunk) === JSON.stringify(chunk),
        `the envelope copy and the args copy of the slice are byte-identical: ${key}#${m.frame.seq}`,
        `envelope=${JSON.stringify(chunk)} args=${JSON.stringify(m.frame.args?.chunk)}`,
      );
      check(
        m.frame.args?.contentHash === chunk.contentHash,
        `args.contentHash is the hash of the whole this slice belongs to: ${key}#${m.frame.seq}`,
        `args=${m.frame.args?.contentHash} chunk=${chunk.contentHash}`,
      );
      check(
        typeof m.frame.args?.payload === 'string' && /^[A-Za-z0-9+/]*={0,2}$/.test(m.frame.args.payload),
        `a chunk slice carries its fragment in args.payload: ${key}#${m.frame.seq}`,
        `payload=${typeof m.frame.args?.payload} len=${m.frame.args?.payload?.length ?? 'n/a'}`,
      );
    }

    if (hashes.size === 1) {
      cleanGroups++;
      check(true, `chunk group carries one contentHash throughout: ${key}`, '');
      // The VER-76 assertion, now written: reassembled slice bodies hash to the
      // document the sender declared. The fragments are the byte string the
      // atomicity rule is about, so the group-level check hashes the
      // concatenation, not any single slice.
      const ordered = [...members].sort((a, b) => a.frame.chunk.chunkSeq - b.frame.chunk.chunkSeq);
      let reassembled;
      try {
        reassembled = Buffer.concat(ordered.map((m) => Buffer.from(m.frame.args.payload, 'base64')));
      } catch {
        reassembled = null;
      }
      check(
        reassembled !== null,
        `every slice fragment decodes as base64: ${key}`,
        'a fragment Buffer.from() refused',
      );
      const reassembledHash = reassembled === null ? '' : createHash('sha256').update(reassembled).digest('hex');
      check(
        reassembledHash === members[0].frame.chunk.contentHash,
        `reassembled slice bodies hash to the declared contentHash: ${key}`,
        `sha256(${reassembled?.length ?? 0} bytes)=${reassembledHash.slice(0, 16)}… declared ${members[0].frame.chunk.contentHash.slice(0, 16)}…`,
      );
      check(
        (() => {
          try {
            JSON.parse(reassembled.toString('utf8'));
            return true;
          } catch {
            return false;
          }
        })(),
        `the reassembled bytes are a UTF-8 JSON document: ${key}`,
        'reassembly produced bytes JSON.parse refuses',
      );
    } else {
      splitGroups++;
      const answers = members[0].frames.filter(
        (f) => f.type === 'response' && f.inReplyTo === members[0].frame.id,
      );
      check(
        answers.some((f) => f.ok === false && f.error?.code === 'IntegrityMismatch'),
        `a chunk group that splits its contentHash is rejected, not reassembled: ${key}`,
        `${hashes.size} distinct hashes, answers ${answers.map((a) => `${a.ok}/${a.error?.code ?? 'ok'}`).join(', ') || 'none'}`,
      );
    }
  }
  check(cleanGroups >= 1, 'a complete chunked import reassembles in the fixtures', `${cleanGroups} clean groups`);
  check(splitGroups >= 1, 'the chunked IntegrityMismatch path is exercised by a real group', `${splitGroups} split groups`);
}

// ---------------------------------------------------------------- capability registry
//
// protocol/compatibility-rules.json is the normative registry of capability names.
// connector.hello announces the set a real Connector honours. Two lists, one truth:
// unless the sets are compared, a name can be added to the registry that no
// Connector ever reports (so a UI gates on a capability nobody has) or announced by
// a Connector the registry never declared (so the client cannot gate at all).

{
  const registry = dataDocs['protocol/compatibility-rules.json'].capabilities;
  const names = registry.map((c) => c.name);
  check(
    registry.every((c) => c && typeof c === 'object' && typeof c.name === 'string' && typeof c.addedIn === 'string' && typeof c.description === 'string'),
    'every capability is a named, versioned, described object',
    `offenders: ${registry.filter((c) => !c || typeof c.name !== 'string' || typeof c.addedIn !== 'string' || typeof c.description !== 'string').map((c) => JSON.stringify(c)).join(', ')}`,
  );
  check(new Set(names).size === names.length, 'capability names are unique', `dupes in ${names}`);
  check(names.length >= 13, 'the registry still covers the documented capability surface', `${names.length} names`);

  const announced = new Set();
  let helloFrames = 0;
  for (const [, frames] of framesByFile) {
    const requests = frames.filter((f) => f.type === 'request');
    for (const f of frames) {
      if (f.type !== 'response' || f.ok !== true) continue;
      const req = requests.find((r) => r.id === f.inReplyTo);
      if (req?.method !== 'connector.hello') continue;
      helloFrames++;
      for (const name of f.result?.capabilities ?? []) announced.add(name);
    }
  }
  check(helloFrames > 0, 'a recorded connector.hello succeeds in the fixtures', 'no ok hello response to read capabilities from');
  check(announced.size > 0, 'the recorded hello announces at least one capability', `${announced.size}`);

  const registryOnly = names.filter((n) => !announced.has(n));
  const helloOnly = [...announced].filter((n) => !names.includes(n));
  check(
    registryOnly.length === 0 && helloOnly.length === 0,
    'the recorded hello capability set equals protocol/compatibility-rules.json',
    `registry-only: ${registryOnly.join(', ')}; hello-only: ${helloOnly.join(', ')} (registry ${names.length}, hello ${announced.size})`,
  );
  for (const [name, entry] of commandDocs) {
    for (const cap of entry.doc.capabilities) {
      check(
        announced.has(cap),
        `command ${name} gates on a capability the recorded Connector actually announces`,
        `${cap} is declared in the registry but never announced`,
      );
    }
  }
}

// ---------------------------------------------------------------- retryability agrees
//
// The taxonomy owns `retryable`; a frame may echo it as a convenience, and an echo
// that contradicts the table is a client that backs off where it should stop.

{
  const byCode = new Map(taxonomy.codes.map((c) => [c.code, c]));
  let compared = 0;
  for (const [file, frames] of framesByFile) {
    for (const f of frames) {
      if (f.type !== 'response' || f.ok !== false || !('retryable' in f.error)) continue;
      compared++;
      check(
        byCode.get(f.error.code)?.retryable === f.error.retryable,
        `fixture error.retryable matches the taxonomy: ${file}#${f.id}`,
        `${f.error.code}: frame says ${f.error.retryable}, the taxonomy says ${byCode.get(f.error.code)?.retryable}`,
      );
    }
  }
  check(compared > 0, 'error frames exist to compare against the taxonomy', `${compared} compared`);
}

// ---------------------------------------------------------------- one hash, one shape
//
// appliedConfigHash (connector.status, config.snapshot), configHash (config.apply,
// core.start, core.restart, core.status) and compiledConfigHash (profile.validate)
// name the same quantity from different commands. Divergent encodings would let two
// commands both claim "in sync" about different numbers, so the pattern is asserted
// and the values are traced back to the command that produced them.

{
  const HASH = '^[0-9a-f]{64}$';
  const HASH_NULLABLE = '^([0-9a-f]{64})?$';
  let hashFields = 0;
  for (const [name, entry] of commandDocs) {
    for (const part of ['args', 'result']) {
      for (const [field, spec] of Object.entries(entry.doc[part]?.properties ?? {})) {
        if (!/Hash$/.test(field)) continue;
        const types = Array.isArray(spec.type) ? spec.type : [spec.type];
        if (!types.includes('string')) continue; // a comparison flag, not a hash
        hashFields++;
        check(
          spec.pattern === HASH || spec.pattern === HASH_NULLABLE,
          `${name}.${part}.${field} uses the canonical sha256 pattern`,
          `pattern ${String(spec.pattern)} is neither ${HASH} nor ${HASH_NULLABLE}`,
        );
        check(
          (spec.pattern === HASH_NULLABLE) === types.includes('null'),
          `${name}.${part}.${field} is nullable in its type exactly when its pattern says so`,
          `type ${JSON.stringify(spec.type)} with pattern ${String(spec.pattern)}`,
        );
      }
    }
  }
  check(hashFields >= 8, 'the hash identity check has fields to check', `${hashFields} hash fields`);

  const PRODUCED_BY = new Set(['config.apply', 'core.start', 'core.restart']);
  const READ_BY = new Set(['config.snapshot', 'connector.status', 'core.status']);
  const produced = new Set();
  const reads = [];
  for (const [file, frames] of framesByFile) {
    const requests = frames.filter((f) => f.type === 'request');
    for (const f of frames) {
      if (f.type !== 'response' || f.ok !== true) continue;
      const req = requests.find((r) => r.id === f.inReplyTo);
      if (!req) continue;
      for (const [field, value] of Object.entries(f.result ?? {})) {
        if (typeof value !== 'string' || !/Hash$/.test(field)) continue;
        if (PRODUCED_BY.has(req.method)) produced.add(value);
        else if (READ_BY.has(req.method)) {
          reads.push({ file, id: f.id, method: req.method, field, value, drift: f.result?.drift === true });
        }
      }
    }
  }
  check(produced.size > 0, 'a mutating command produced a config hash to compare against', `${produced.size} hashes`);
  check(reads.length >= 3, 'status commands report the applied config hash in the fixtures', `${reads.length} reports`);
  for (const read of reads) {
    check(
      produced.has(read.value) || read.drift,
      `${read.method}#${read.field} reports a hash some mutating command produced: ${read.file}#${read.id}`,
      `${read.value} was never applied by config.apply, core.start or core.restart, and the frame does not flag drift`,
    );
  }
}

// ---------------------------------------------------------------- catalogue parity
//
// Three documents describe the same closed set from different sides. If they drift,
// a UI can offer a section the Connector cannot produce or a build can ship an event
// nothing subscribes to -- both invisible until a user hits it.

{
  const diagnosticsInclude = new Set(
    commandDocs.get('diagnostics.get').doc.args.properties.include.items.enum,
  );
  const settingsSchema = ajv
    .getSchema('https://schema.nexora.local/connector-settings.schema.json')
    .schema.properties.telemetry.properties.diagnosticsIncludeByDefault;
  const invented = (settingsSchema.items.enum ?? []).filter((n) => !diagnosticsInclude.has(n));
  check(
    invented.length === 0,
    'connector-settings names no diagnostics section the command cannot produce',
    `invented: ${invented.join(', ')}`,
  );
  const exampleDefault = (await readJson(join(examplesDir, 'settings.default.json'))).telemetry
    .diagnosticsIncludeByDefault;
  check(
    exampleDefault.every((n) => diagnosticsInclude.has(n)),
    'the shipped default diagnostics set is producible',
    `offenders: ${exampleDefault.filter((n) => !diagnosticsInclude.has(n)).join(', ')}`,
  );

  const catalog = await readJson(join(examplesDir, 'events.catalog.json'));
  const missingFromCatalog = [...eventNames].filter((n) => !catalog.events.includes(n));
  const extraInCatalog = catalog.events.filter((n) => !eventNames.has(n));
  check(
    missingFromCatalog.length === 0 && extraInCatalog.length === 0,
    'events.catalog.json is exactly the registered event set',
    `missing: ${missingFromCatalog.join(', ')}; extra: ${extraInCatalog.join(', ')}`,
  );

  const staging = await readJson(join(examplesDir, 'import-staging.partial.json'));
  const life =
    (new Date(staging.expiresAt).getTime() - new Date(staging.createdAt).getTime()) / 3600000;
  check(
    life > 0 && life <= 1,
    'import staging expires within one hour of creation',
    `example lifetime is ${life} h`,
  );
  check(
    staging.receivedChunkSeqs.length < staging.chunkCount,
    'a partial staging record really is partial',
    'the record exists to describe an interrupted transfer',
  );
}

// ---------------------------------------------------------------- enum honesty
//
// An enum is only a contract if the values exist in the engine that reads them.
// Xray's routing section accepts exactly three domainStrategy values; the shipped
// examples must speak that language or the compiled config is rejected at apply
// time as ConfigApplyFailed. Re-check against the release tag in pins before a
// Core bump (examples/pins.xray.json pins v24.11.11).

const XRAY_DOMAIN_STRATEGY = ['AsIs', 'IPIfNonMatch', 'IPOnDemand']; // https://xtls.github.io/en/config/routing.html

{
  const routing = ajv
    .getSchema('https://schema.nexora.local/routing-policy.schema.json')
    .schema.properties.corePlane.properties.domainStrategy;
  check(
    JSON.stringify([...routing.enum].sort()) === JSON.stringify([...XRAY_DOMAIN_STRATEGY].sort()),
    'corePlane.domainStrategy is exactly the set Xray documents',
    `schema ${JSON.stringify(routing.enum)} vs Xray ${JSON.stringify(XRAY_DOMAIN_STRATEGY)}`,
  );
  check(
    routing.enum.includes(routing.default),
    'the domainStrategy default is a value Xray accepts',
    `default ${routing.default}`,
  );
  check(
    /xtls\.github\.io/i.test(routing.description ?? ''),
    'the domainStrategy description cites the source the enum was taken from',
    'an enum copied from a document must name the document',
  );

  const routingFiles = [
    ...validExamples.filter((f) => basename(f).startsWith('routing.')),
    ...(await readdir(invalidDir)).filter((f) => f.startsWith('routing.') && f.endsWith('.json')).map((f) => join(invalidDir, f)),
  ];
  let checked = 0;
  for (const file of routingFiles) {
    const doc = await readJson(file);
    const value = doc.corePlane?.domainStrategy;
    if (value === undefined) continue;
    checked++;
    check(
      routing.enum.includes(value),
      `routing example uses a documented domainStrategy: ${basename(file)}`,
      `${value} is not one of ${routing.enum.join(', ')}`,
    );
  }
  check(checked >= 4, 'the domainStrategy cross-check saw the routing documents', `${checked} files`);
}

{
  const validate = ajv.getSchema('https://schema.nexora.local/routing-policy.schema.json');
  const example = await readJson(join(PKG, 'examples', 'routing.two-plane.json'));
  const allowed = structuredClone(example);
  allowed.corePlane.sniffing.protocols = ['http', 'tls', 'quic', 'fakedns'];
  check(
    validate(allowed),
    'corePlane.sniffing accepts the protocols supported by Xray v24.11.11',
    JSON.stringify(validate.errors),
  );
  for (const protocol of ['mp4', 'mkv', 'ps', 'www']) {
    const unsupported = structuredClone(example);
    unsupported.corePlane.sniffing.protocols = [protocol];
    check(
      !validate(unsupported) && (validate.errors ?? []).some((e) =>
        e.keyword === 'enum' && e.instancePath === '/corePlane/sniffing/protocols/0'),
      `corePlane.sniffing rejects unsupported protocol by enum: ${protocol}`,
      JSON.stringify(validate.errors),
    );
  }
  const rewriting = structuredClone(example);
  rewriting.corePlane.sniffing.routeOnly = false;
  check(
    !validate(rewriting) && (validate.errors ?? []).some((e) =>
      e.keyword === 'const' && e.instancePath === '/corePlane/sniffing/routeOnly'),
    'corePlane.sniffing rejects routeOnly false by const',
    JSON.stringify(validate.errors),
  );
  const omitted = structuredClone(example);
  delete omitted.corePlane.sniffing.routeOnly;
  check(
    validate(omitted),
    'corePlane.sniffing permits omission of routeOnly',
    JSON.stringify(validate.errors),
  );
}

// ---------------------------------------------------------------- required sets are real
//
// A name in `required` that is not in `properties` silently rejects every instance
// under additionalProperties: false, and a property nobody declared optional in the
// prose is a field whose optionality has no owner. So: required is a subset of the
// declared properties everywhere, and for the staging buffer the optional subset is
// both documented and demonstrated by removing one field at a time.

{
  for (const file of schemaFiles) {
    const doc = await readJson(file);
    const orphans = (doc.required ?? []).filter((r) => !(doc.properties && r in doc.properties));
    check(
      orphans.length === 0,
      `every required name is a declared property: ${relative(PKG, file)}`,
      `not in properties: ${orphans.join(', ')}`,
    );
  }

  const stagingId = 'https://schema.nexora.local/import-staging.schema.json';
  const stagingSchema = ajv.getSchema(stagingId).schema;
  const stagingValidate = ajv.getSchema(stagingId);
  const stagingExample = await readJson(join(examplesDir, 'import-staging.partial.json'));
  const requiredNames = stagingSchema.required ?? [];
  const optionalNames = Object.keys(stagingSchema.properties).filter((p) => !requiredNames.includes(p));
  check(optionalNames.length > 0, 'the staging contract has an optional subset to describe', `${optionalNames.length}`);
  // The prose carries the whole split, so any change to the required array that is
  // not also a change to the description fails here.
  for (const name of Object.keys(stagingSchema.properties)) {
    check(
      new RegExp(`\\b${name}\\b`).test(stagingSchema.description ?? ''),
      `import-staging documents the status of "${name}" in its description`,
      `${name} is neither named as required nor explained as optional`,
    );
  }
  const proseRequired = /is exactly ([A-Za-z0-9, ]+?) --/.exec(stagingSchema.description ?? '');
  check(
    proseRequired !== null &&
      JSON.stringify(proseRequired[1].split(',').map((s) => s.trim()).sort()) ===
        JSON.stringify([...requiredNames].sort()),
    'the description states exactly the required array',
    `prose="${proseRequired?.[1] ?? '(unparsed)'}" array=${requiredNames.join(', ')}`,
  );
  for (const name of optionalNames) {
    const probe = { ...stagingExample };
    delete probe[name];
    const ok = stagingValidate(probe);
    check(ok, `staging survives without the optional ${name}`, ok ? '' : JSON.stringify(stagingValidate.errors?.slice(0, 2)));
  }
  for (const name of requiredNames) {
    const probe = { ...stagingExample };
    delete probe[name];
    const rejected = !stagingValidate(probe);
    check(rejected, `staging refuses a record without the required ${name}`, 'required is not actually enforced');
  }
}

// ---------------------------------------------------------------- backup version rule
//
// JSON Schema cannot compare two sibling integers, so the floor lives in the schema
// and the ordering lives here: a backup whose toSchemaVersion is below the version
// it read describes a runner that downgraded a user's envelope.

{
  const backupId = 'https://schema.nexora.local/envelope-backup.schema.json';
  const backupSchema = ajv.getSchema(backupId).schema;
  const floor = (backupSchema.allOf ?? [])
    .map((branch) => branch?.then?.properties?.toSchemaVersion?.minimum)
    .find((m) => typeof m === 'number');
  check(floor === 2, 'the backup allOf enforces the floor its prose states (toSchemaVersion >= 2)', `found ${String(floor)}`);
  check(
    />= 2/.test(backupSchema.properties.toSchemaVersion.description ?? ''),
    'the toSchemaVersion prose states the machine-checkable floor',
    'the description must not claim an ordering the schema does not express',
  );

  const backupExamples = validExamples.filter((f) => basename(f).startsWith('envelope-backup.'));
  check(backupExamples.length >= 1, 'a backup record exists to check the version rule against', `${backupExamples.length}`);
  for (const file of backupExamples) {
    const doc = await readJson(file);
    check(
      doc.toSchemaVersion >= doc.fromSchemaVersion,
      `migration only moves forward: ${basename(file)}`,
      `${doc.fromSchemaVersion} -> ${doc.toSchemaVersion}`,
    );
  }
}

// ---------------------------------------------------------------- citations resolve
//
// Every legacy path this package cites is a claim about code that has to still be
// there. `omega-pac/src/default_options.coffee` was one such citation pointing at a
// file that does not exist (the shipped default is omega-target/src/default_options.coffee:2),
// so the rule is now checked: the file resolves and the line range fits inside it.

{
  const jsonFiles = [];
  for await (const file of walk(PKG)) {
    if (!file.endsWith('.json') && !file.endsWith('.jsonl')) continue;
    if (file.includes(`${sep}node_modules${sep}`) || basename(file) === 'package-lock.json') continue;
    jsonFiles.push(file);
  }
  const citations = new Map();
  for (const file of jsonFiles) {
    const text = await readFile(file, 'utf8');
    for (const match of text.matchAll(/omega-[A-Za-z0-9_-]+\/[A-Za-z0-9_.\/-]+\.coffee(?::\d+(?:-\d+)?)?/g)) {
      if (!citations.has(match[0])) citations.set(match[0], file);
    }
  }
  check(citations.size >= 8, 'the citation check saw the legacy references', `${citations.size} citations`);
  for (const [citation, where] of citations) {
    const [rel, range] = citation.split(':');
    const abs = join(REPO, rel);
    if (!existsSync(abs)) {
      check(false, `cited legacy path exists: ${citation}`, `cited in ${relative(REPO, where)}`);
      continue;
    }
    const lineCount = (await readFile(abs, 'utf8')).split('\n').length;
    const numbers = range ? range.split('-').map(Number) : [];
    check(
      numbers.every((n) => Number.isInteger(n) && n >= 1 && n <= lineCount),
      `cited line range is inside the file: ${citation}`,
      `${rel} has ${lineCount} lines; cited ${range} (in ${relative(REPO, where)})`,
    );
  }
}

// ------------------------------------------------- sync strip set vs required fields
//
// A field that `required` in a sync-eligible schema must never be in
// syncExcludedValueKeys: stripping it would produce an entity that fails validation on the
// receiving machine. This is not hypothetical -- an earlier draft of the registry stripped
// `userIdRef`, `headersRef`, `publicKey` and `shortId`, which are either inert keystore
// references (no confidentiality gained) or public REALITY parameters (a server that cannot
// handshake). docs/architecture/24-options-envelope-compat.md §4 owns the reasoning.

{
  const strip = new Set(envelopeKeys.syncExcludedValueKeys);
  const eligible = envelopeKeys.namespaces.filter((ns) => ns.syncEligible);
  check(eligible.length >= 5, 'the strip-set check has namespaces to look at', `${eligible.length} sync-eligible namespaces`);
  let compared = 0;
  for (const ns of eligible) {
    const abs = join(REPO, 'packages', 'schema', ns.valueSchema);
    if (!existsSync(abs)) continue; // already reported by the namespace loop
    const valueSchema = JSON.parse(await readFile(abs, 'utf8'));
    const required = valueSchema.required ?? [];
    compared++;
    const collide = required.filter((name) => strip.has(name));
    check(
      collide.length === 0,
      `sync strip set does not strip a required field: ${ns.entity}`,
      `${collide.join(', ')} is required in ${ns.valueSchema} and listed in syncExcludedValueKeys`,
    );
  }
  check(compared >= 5, 'the strip set was compared against real value schemas', `${compared} schemas compared`);
}

// ------------------------------------------------- the block is a resolution, not a slogan
//
// docs/adr/0009-fail-closed-by-default.md used to record, in its own Verification
// section, that failure-policy.schema.json "#/properties/forbiddenOutcomes claims the
// verifier asserts these, and it does not". It does now. The rule is one sentence: no
// Connector-triggered plane may resolve to an outcome the document itself calls forbidden.
// JSON Schema cannot compare two fields, so the comparison lives here: the resolution
// vocabulary of every plane is read out of the compiled schema and matched against the
// forbidden set. Widening any of those enums fails this build rather than a review.

{
  const policy = ajv.getSchema('https://schema.nexora.local/failure-policy.schema.json').schema;
  const forbidden = new Set(policy.properties.forbiddenOutcomes.items.enum);
  check(forbidden.size === 3, 'the forbidden-outcome vocabulary is the documented three', [...forbidden].join(', '));

  // What each resolution word does to chrome.proxy. 'restore' is the SystemProfile
  // gesture (clear(), docs/audit/06-proxy-application.md section 4) and 'failOpen' the
  // DirectProfile gesture (mode: 'direct'); both were measured in Phase 0, which is why
  // they appear here as outcomes rather than as opinions about intent.
  const OUTCOME_OF = { restore: 'system', failOpen: 'direct', clear: 'clear-proxy-settings' };
  const wordOutcome = (word) => (word in OUTCOME_OF ? OUTCOME_OF[word] : word);

  const planes = {
    coreDeath: policy.properties.coreDeath.properties.action.enum,
    serviceTeardown: policy.properties.serviceTeardown.properties.then.enum,
    onDisabled: policy.properties.extensionLifecycle.properties.onDisabled.enum,
    onUpdated: policy.properties.extensionLifecycle.properties.onUpdated.enum,
    onBrowserStart: policy.properties.extensionLifecycle.properties.onBrowserStart.enum,
    profileOverride: policy.$defs.ProfilePolicy.properties.coreDeath.properties.action.enum,
  };

  for (const name of ['coreDeath', 'serviceTeardown']) {
    const leaks = planes[name].filter((word) => forbidden.has(wordOutcome(word)));
    check(leaks.length === 0, `connector-triggered plane resolves to no forbidden outcome: ${name}`, `${leaks.join(', ')} in ${planes[name].join(', ')}`);
  }
  check(
    planes.coreDeath.length === 1 && planes.coreDeath[0] === 'block',
    'global coreDeath.action is block-only, so fail-open is unreachable except per profile (ADR-0009 rule 2)',
    planes.coreDeath.join(', '),
  );
  check(
    planes.serviceTeardown.length === 1 && planes.serviceTeardown[0] === 'block',
    'serviceTeardown.then is block-only: the grace window may delay the verdict, not reverse it',
    planes.serviceTeardown.join(', '),
  );

  // The exemption is written down as data too: on the browser-triggered plane 'restore'
  // is not a leak, it is the user taking the proxy back. Narrowing these two enums to
  // 'block' would hold a blocked browser hostage after the extension is disabled.
  for (const name of ['onDisabled', 'onUpdated']) {
    check(planes[name].includes('restore'), `user-triggered plane may hand the proxy back: ${name}`, planes[name].join(', '));
  }
  check(
    planes.onBrowserStart.every((word) => ['restore', 'reapply'].includes(word)),
    'onBrowserStart resolves only to restore or reapply',
    planes.onBrowserStart.join(', '),
  );

  check(
    planes.profileOverride.includes('failOpen'),
    'the confirmed per-profile fail-open exception still exists',
    planes.profileOverride.join(', ')
  );
  const gated = policy.$defs.ProfilePolicy.allOf[0]?.then?.properties?.coreDeath?.required ?? [];
  check(gated.includes('confirmToken'), 'failOpen is still gated by a required confirmToken in the same subschema', `required: ${gated.join(', ') || 'none'}`);

  // The pin at the bottom of the schema must cover the whole vocabulary: an outcome word
  // added to items.enum without a matching contains-branch is a forbidden outcome a
  // document may drop, which is the exact weakening this block exists to prevent.
  const pinned = new Set((policy.allOf ?? []).map((branch) => branch?.properties?.forbiddenOutcomes?.contains?.const));
  check(
    pinned.size === forbidden.size && [...forbidden].every((word) => pinned.has(word)),
    'every forbidden outcome is pinned element-by-element in the schema',
    `vocabulary ${[...forbidden].join(', ')}; pinned ${[...pinned].join(', ') || 'nothing'}`,
  );

  const block = policy.properties.block.properties;
  for (const host of block.host.enum) {
    check(['127.0.0.1', '::1'].includes(host), 'the block target is loopback-only (ADR-0006)', host);
  }
  check(block.assertClosed.default === true, 'assertClosed defaults to true: a block that is not proven closed is an inbound', String(block.assertClosed.default));
  // Absence is the contract: a block that can carry exemptions is not a block, and a
  // socks4-shaped block keyword makes the browser resolve the name before it fails.
  check(!('bypassList' in block), 'the block primitive has no bypass list to leak through', Object.keys(block).join(', '));
  check(
    block.scheme.enum.join(',') === 'http,socks5',
    'the block scheme is http or socks5 only: the SOCKS4 keyword form resolves locally (omega-pac/src/profiles.coffee:53-58)',
    block.scheme.enum.join(', '),
  );

  // Shipped documents, not just vocabulary: a block port the Compiler may allocate is a
  // block that silently becomes a live inbound the moment Core restarts into that port.
  const policyExample = await readJson(join(examplesDir, 'failure-policy.default.json'));
  const settingsExample = await readJson(join(examplesDir, 'settings.default.json'));
  const { portRange } = policyExample.block;
  check(portRange.min <= portRange.max, 'the example block range is ordered', `${portRange.min}-${portRange.max}`);
  const alloc = settingsExample.ports.range;
  check(
    portRange.max < alloc.min || portRange.min > alloc.max,
    'the block range and the inbound allocation range are disjoint in the shipped documents',
    `block ${portRange.min}-${portRange.max} vs allocation ${alloc.min}-${alloc.max}`
  );
  check(
    portRange.min >= 1024 && portRange.max <= 65535,
    'the example block range sits in 1024..65535, the same non-privileged space the allocation range is bounded to',
    `${portRange.min}-${portRange.max} vs allocation ${alloc.min}-${alloc.max}`
  );

  // ODEC-19: the ladder's length equals the attempt count in data, so the disagreement cannot be authored.
  {
    const validate = ajv.getSchema('https://schema.nexora.local/failure-policy.schema.json');
    const agree = structuredClone(policyExample);
    agree.coreDeath.restartAttempts = 1;
    agree.coreDeath.restartBackoffSeconds = [5];
    check(validate(agree), 'coreDeath accepts a one-attempt ladder of one delay', JSON.stringify(validate.errors));
    for (const [attempts, ladder, label] of [
      [10, [1], 'the canonical ODEC-19 disagreement: ten attempts, one delay'],
      [3, [1, 5], 'a short ladder: three attempts, two delays'],
      [2, [1, 5, 30], 'an over-long ladder: two attempts, three delays'],
    ]) {
      const bad = structuredClone(policyExample);
      bad.coreDeath.restartAttempts = attempts;
      bad.coreDeath.restartBackoffSeconds = ladder;
      check(
        !validate(bad) && (validate.errors ?? []).some((e) =>
          e.instancePath === '/coreDeath/restartBackoffSeconds' && (e.keyword === 'minItems' || e.keyword === 'maxItems')),
        `coreDeath refuses ${label}`,
        JSON.stringify(validate.errors),
      );
    }
  }

  // The scene that proves the resolution order, read from the recorded frames rather than
  // restated in prose: exited first, blocked second, and nothing in between resolving to
  // a direct profile.
  const scene = framesByFile.get('failure-block.jsonl') ?? [];
  const events = scene.filter((f) => f.type === 'event').map((f) => f.progress?.event);
  const exited = events.indexOf('core.exited');
  const blocked = events.indexOf('core.blocked');
  check(exited >= 0 && blocked > exited, 'the recorded failure scene blocks after Core exits', `events: ${events.join(', ') || 'none'}`);
  check(
    !scene.some((f) => /direct|"system"|clear/.test(JSON.stringify(f.progress ?? ''))),
    'no frame of the failure scene resolves to a direct or system gesture',
    JSON.stringify(scene.filter((f) => f.type === 'event').map((f) => f.progress)).slice(0, 120)
  );
}

// ------------------------------------------------- who may speak on the pipe
//
// docs/architecture/31-security-model.md describes the sender model; the budgets it leans on
// are data in framing.json#/senderPolicy, because a rate limit the Go host and the TypeScript
// client each invent is two rate limits. These checks are what stop the prose and the numbers
// from separating -- ADR-0004 rules 3 and 6 are only testable through them.
{
  const sp = framing.senderPolicy;
  check(sp !== undefined, 'framing.json declares a sender policy', 'ADR-0004 has no numeric home without it');
  if (sp) {
    const privilegedDeclared = allowlist.commands.filter((c) => c.privileged).map((c) => c.name).sort();
    check(
      JSON.stringify([...(sp.privilegedMethods?.names ?? [])].sort()) === JSON.stringify(privilegedDeclared),
      'senderPolicy.privilegedMethods.names is exactly the allowlist\'s privileged flag',
      `allowlist: ${privilegedDeclared.join(', ')}; policy: ${(sp.privilegedMethods?.names ?? []).join(', ')}`,
    );
    check(privilegedDeclared.length >= 10, 'the privileged set is non-vacuous', `${privilegedDeclared.length} methods`);
    check(
      privilegedDeclared.length < allowNames.length,
      'not every method is privileged: a surface with no unprivileged read leaves the popup unable to render without granting power',
      `${privilegedDeclared.length} of ${allowNames.length}`,
    );

    check(
      /UnauthorizedExtension/.test(sp.identity?.unknownSender ?? ''),
      'an unknown sender is answered with the sender-id error',
      sp.identity?.unknownSender ?? 'absent',
    );
    const unauth = taxonomy.codes.find((c) => c.code === 'UnauthorizedExtension');
    check(
      unauth?.retryable === false,
      'UnauthorizedExtension is not retryable: a refusal a client retries becomes a loop against the pipe',
      `retryable=${unauth?.retryable}`,
    );
    check(
      /browser storage/.test(sp.identity?.allowlistNeverIn ?? ''),
      'the sender allowlist is declared to live outside browser storage',
      'an allowlist the caller can write is the caller authorising itself (ADR-0004 rule 3)',
    );
    check(
      (sp.identity?.notIdentity ?? []).length >= 4,
      'the policy names what it refuses to treat as identity',
      'a payload field claiming to be the extension is the attack this rule exists for',
    );

    const rows = sp.rateLimits ?? [];
    const methodNames = new Set(allowNames);
    for (const row of rows) {
      for (const name of row.scope ?? []) {
        check(
          name === '*' || methodNames.has(name),
          `rate limit names a real method: ${name}`,
          'a typo in a scope string is a command nobody ever limits',
        );
      }
      check(
        row.windowSeconds >= 1 && row.maxRequests >= 1,
        `rate limit row is a positive budget: ${row.scope.join(',')}`,
        `${row.windowSeconds}/${row.maxRequests}`,
      );
      check(
        row.onExceed === 'Busy',
        `rate limit refusal is Busy: ${row.scope.join(',')}`,
        `found ${row.onExceed}; QuotaExceeded is a storage-layer code and tells the client the wrong thing`,
      );
    }
    const floors = rows.filter((r) => (r.scope ?? []).includes('*'));
    check(floors.length === 1, 'exactly one global rate floor exists', `${floors.length} rows carry "*"`);
    for (const row of rows) {
      if (floors.length !== 1 || (row.scope ?? []).includes('*')) continue;
      const perSecond = row.maxRequests / row.windowSeconds;
      const floorPerSecond = floors[0].maxRequests / floors[0].windowSeconds;
      check(perSecond <= floorPerSecond, `a class budget never exceeds the global floor: ${row.scope.join(',')}`, `${perSecond}/s vs ${floorPerSecond}/s`);
    }
    const limited = new Set(rows.filter((r) => !(r.scope ?? []).includes('*')).flatMap((r) => r.scope));
    for (const name of privilegedDeclared) {
      check(limited.has(name), `every privileged method carries a rate budget: ${name}`, 'docs/architecture/31 promises a budget per side-effecting sender action, not per class that happens to be listed');
    }

    const guard = sp.refusals?.amplificationGuard;
    check(
      guard && guard.windowSeconds >= 1 && guard.maxRefusals >= 1,
      'refusals have an amplification guard',
      'an unallowlisted sender with a retry loop must not be allowed to make the host answer forever',
    );
    check(
      (sp.portsPerSender?.maxConcurrent ?? 0) >= 1,
      'the policy bounds ports per sender',
      'MV3 eviction overlaps exactly one reconnect; an unbounded count is a leak or a second registration',
    );

    // The drift §4.12 found, pinned so it cannot come back: doc 31 §8 and
    // framing.json#/senderPolicy/rateLimitFacts/countedWhere must name the same process for the same budget. The
    // field used to say "the host, before dispatch" and the document repeated the field -- two sentences agreeing
    // with each other while the code counted in the other process, which is how a contradiction of this kind
    // survives a green suite. Each side is read out of its own text rather than compared with a constant, so
    // renaming the process in the data alone fails here.
    const countedWhere = sp.rateLimitFacts?.countedWhere ?? '';
    const dataProcess = /^the ([A-Za-z][A-Za-z0-9]*),/.exec(countedWhere)?.[1];
    const sec8Text = (await readFile(join(REPO, 'docs', 'architecture', '31-security-model.md'), 'utf8'))
      .split(/^## 8\. /m)[1]?.split(/^## /m)[0] ?? '';
    const docProcess = /counted by the ([A-Za-z][A-Za-z0-9]*) \*\*before dispatch\*\*/.exec(sec8Text)?.[1];
    check(
      !!dataProcess && !!docProcess && dataProcess === docProcess,
      'the budget-counting process is one name in the data and in doc 31 §8',
      `framing.json says "${dataProcess ?? '(unreadable)'}", doc 31 §8 says "${docProcess ?? '(no counted-by sentence)'}"; countedWhere=${countedWhere.slice(0, 60)}`,
    );
    check(
      !!dataProcess && !/\b(shim|host)\b[^.]{0,40}counted by/i.test(sec8Text),
      'doc 31 §8 does not hand the counting to a second process',
      'the shim cannot open the payload, so a sentence that makes it count budgets describes a control no code can implement',
    );
  }
}

// ------------------------------------------- secrets, references and the mask
//
// docs/architecture/32-secrets-keystore-masking.md is the argument; the vocabulary it argues
// about is data in envelope-keys.schema.json#/properties/secretReferences, because a masking
// rule that five schemas and two clients each spell their own way is five rules. These checks
// keep the prose and the strings together: one reference floor, one field vocabulary, one
// fingerprint shape, one ingest path, and the error every command that touches the keystore is
// obliged to name.
{
  const sr = envelopeKeys.secretReferences;
  check(sr !== undefined, 'the envelope registry declares the secret-reference vocabulary', 'docs/architecture/32 has no numeric home without it');

  const jsonTrees = [];
  async function collectJson(dirPath) {
    for (const entry of await readdir(dirPath, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        if (entry.name !== 'node_modules') await collectJson(join(dirPath, entry.name));
        continue;
      }
      if (!/\.jsonl?$/.test(entry.name)) continue;
      const full = join(dirPath, entry.name);
      try {
        const text = await readFile(full, 'utf8');
        const docs = /\.jsonl$/.test(entry.name)
          ? text.split(/\r?\n/).filter(Boolean).map((line) => JSON.parse(line))
          : [JSON.parse(text)];
        jsonTrees.push({ file: relative(PKG, full).split(sep).join('/'), docs });
      } catch {
        check(false, `secret-vocabulary walk parsed ${entry.name}`, 'unparseable JSON under packages/schema');
      }
    }
  }
  await collectJson(PKG);

  const patternValues = new Set();
  const refLiterals = [];
  function scan(node, where, path) {
    if (Array.isArray(node)) {
      for (const item of node) scan(item, where, path);
      return;
    }
    if (!node || typeof node !== 'object') return;
    for (const [key, value] of Object.entries(node)) {
      // A `description` is prose about references and `scheme` is the literal prefix itself --
      // neither is an instance of a reference, so neither is held to the shape.
      if (key === 'description' || key === 'scheme') continue;
      if (typeof value === 'string' && key === 'pattern' && /^\^(keystore|confirm):\/\//.test(value)) {
        // Constraints *inside* the vocabulary (which entity owns the fingerprint key) name a
        // position, not a floor; only the schemas that consume the vocabulary must repeat it.
        if (!path.includes('/secretReferences')) patternValues.add(value);
      } else if (typeof value === 'string' && value.startsWith('keystore://')) {
        refLiterals.push({ value, where });
      } else {
        scan(value, where, `${path}/${key}`);
      }
    }
  }
  for (const tree of jsonTrees) scan(tree.docs, tree.file, '');

  const keystoreFloors = [...patternValues].filter((p) => p.startsWith('^keystore://'));
  check(
    keystoreFloors.length === 1 && keystoreFloors[0] === sr.pattern,
    'every keystore-typed property in the package uses the one reference floor from secretReferences',
    `found: ${keystoreFloors.join(' | ') || 'none'}`,
  );
  const confirmFloors = [...patternValues].filter((p) => p.startsWith('^confirm://'));
  check(
    confirmFloors.length <= 1,
    'the confirmation token has one shape, not one per command that uses it',
    `found: ${confirmFloors.join(' | ') || 'none'}`,
  );

  const shapeRe = new RegExp(sr.shape);
  const offShape = refLiterals.filter((r) => !shapeRe.test(r.value));
  check(offShape.length === 0, 'every recorded reference follows the entity/id/field shape', offShape.map((r) => `${r.where}: ${r.value}`).join(' | ') || `${refLiterals.length} references match`);
  check(refLiterals.length >= 8, 'the shape check is not vacuous', `${refLiterals.length} concrete references scanned`);
  check(
    !refLiterals.some((r) => /\/[0-9a-f]{64}(\/|$)/.test(r.value)),
    'no reference is content-addressed',
    'a digest-shaped segment would make every synced envelope an offline dictionary against the value',
  );

  for (const entity of sr.entities ?? []) {
    check(sr.shape.includes(entity), `the shape admits the declared entity class "${entity}"`, sr.shape);
  }
  for (const field of sr.fields ?? []) {
    check(sr.shape.includes(field.name), `the shape admits the declared field "${field.name}"`, sr.shape);
  }
  check(
    (sr.fields ?? []).every((f) => ['high', 'low'].includes(f.entropyClass) && (f.description ?? '').length >= 30),
    'every credential field class declares its entropy and says why',
    (sr.fields ?? []).map((f) => `${f.name}=${f.entropyClass}`).join(', '),
  );
  check(
    (sr.fields ?? []).filter((f) => f.entropyClass === 'low').every((f) => f.maxVisibleChars === 0),
    'a low-entropy credential shows no character of itself',
    (sr.fields ?? []).filter((f) => f.entropyClass === 'low').map((f) => `${f.name}:${f.maxVisibleChars}`).join(', '),
  );
  check(
    (sr.fields ?? []).filter((f) => f.maxVisibleChars > 0).length <= 1,
    'exactly one credential class is allowed a partial display, so widening it is a decision',
    (sr.fields ?? []).filter((f) => f.maxVisibleChars > 0).map((f) => f.name).join(', ') || 'none',
  );
  check(sr.display?.lengthPreserving === false, 'the mask does not preserve the length of what it hides', JSON.stringify(sr.display));

  const fp = sr.fingerprint ?? {};
  const diag = commandDocs.get('diagnostics.get')?.doc;
  const fpPattern = diag?.result?.properties?.masked?.properties?.fingerprints?.items?.pattern;
  check(
    fpPattern === fp.pattern,
    'diagnostics.get fingerprints and secretReferences declare the same fingerprint shape',
    `command: ${fpPattern ?? 'absent'}; vocabulary: ${fp.pattern ?? 'absent'}`,
  );
  check(
    fp.keyed === true && /hmac/.test(fp.algorithm ?? '') && fp.algorithm !== 'sha256',
    'the comparison digest is keyed, never a bare hash of the value',
    `${fp.algorithm ?? 'absent'}, keyed=${fp.keyed}`,
  );
  check(shapeRe.test(fp.keyOwner ?? ''), 'the fingerprint key is itself a reference of the documented shape', fp.keyOwner ?? 'absent');
  check(
    jsonTrees.some((tree) => tree.docs.some((doc) => JSON.stringify(doc).includes(sr.display?.maskLiteral ?? '\u0000'))),
    'a recorded fixture actually uses the declared mask literal',
    'the sentinel that stands for a destroyed value must be the one the artefacts contain, or LIVE-10 greps for a string nobody emits',
  );

  check(sr.atRest?.compiledConfigContainsValues === true, 'the plaintext exception is declared, not implied', 'Xray reads its own config format (ADR-0003); a document that later claims otherwise would contradict this field');
  check(sr.atRest?.compiledConfigInExports === false, 'the compiled config never leaves as an artefact', JSON.stringify(sr.atRest));
  check(sr.atRest?.compiledConfigOwnerOnly === true, 'the compiled config is owner-only on disk', JSON.stringify(sr.atRest));

  const pi = commandDocs.get('profile.import')?.doc;
  const creds = pi?.args?.properties?.credentials;
  check(creds !== undefined, 'profile.import declares the keystore write primitive', 'values otherwise have no legal way in (docs/architecture/32 §6)');
  check(creds?.items?.properties?.value?.writeOnly === true, 'the ingest value is declared writeOnly', 'a field that may arrive must be declared unable to leave');
  check(
    JSON.stringify([...(creds?.items?.properties?.entity?.enum ?? [])].sort()) === JSON.stringify([...(sr.entities ?? [])].sort()),
    'the ingest entity enum is the canonical entity set',
    `enum: ${(creds?.items?.properties?.entity?.enum ?? []).join(', ')}`
  );
  const declaredFields = (sr.fields ?? []).map((f) => f.name).sort();
  check(
    JSON.stringify([...(creds?.items?.properties?.field?.enum ?? [])].sort()) === JSON.stringify(declaredFields),
    'the ingest field enum is the canonical field vocabulary',
    `enum: ${(creds?.items?.properties?.field?.enum ?? [])?.join(', ')}; vocabulary: ${declaredFields.join(', ')}`
  );
  // A result may *describe* the ingest path (operators need to know a count exists); what it may
  // never do is carry a field that could hold the value. So the claim is about property names,
  // not about every string in the document.
  const resultFieldNames = (node, out) => {
    if (Array.isArray(node)) {
      for (const item of node) resultFieldNames(item, out);
      return;
    }
    if (!node || typeof node !== 'object') return;
    for (const [key, value] of Object.entries(node)) {
      if (key === 'properties' && value && typeof value === 'object') {
        for (const name of Object.keys(value)) out.add(name);
      } else if (key === 'required' && Array.isArray(value)) {
        for (const name of value) if (typeof name === 'string') out.add(name);
      } else {
        resultFieldNames(value, out);
      }
    }
    return out;
  };
  const allDocs = [...commandDocs.values()].map((entry) => entry.doc);
  const noEcho = allDocs.filter((doc) => [...resultFieldNames(doc.result ?? {}, new Set())]
    .some((name) => /^(credentials?|secretValue|value)$/i.test(name)));
  check(noEcho.length === 0, 'no command result can spell a credential field', noEcho.map((d) => d.name).join(', '));

  if (pi) {
    const validateArgs = ajv.compile(pi.args);
    const profileForTest = await readJson(join(examplesDir, 'profile.fastest.json'));
    const oneCredential = { entity: 'server', entityId: '3f2a1c9e', field: 'user-id', value: 'b8f2e6c4-3a1d-4f52-9e70-6b5c4d3e2f10' };
    const whole = { profiles: [profileForTest], credentials: [oneCredential] };
    check(
      validateArgs(whole),
      'a single-frame import may carry a credential',
      JSON.stringify(validateArgs.errors?.slice(0, 2)),
    );
    const sliced = {
      profiles: [profileForTest],
      contentHash: '0'.repeat(64),
      chunk: { contentHash: '0'.repeat(64), chunkSeq: 0, chunkCount: 2, encoding: 'json' },
      credentials: [oneCredential],
    };
    check(!validateArgs(sliced), 'a chunked import may not carry a credential', 'a reassembly buffer lives where DPAPI does not reach (docs/architecture/32 §6)');
    check(
      (validateArgs.errors ?? []).some((e) => e.keyword === 'not'),
      'the credential-carrying slice is rejected by the combination rule, not by a typo',
      [...new Set((validateArgs.errors ?? []).map((e) => e.keyword))].join(', '),
    );
  }

  const writeCmds = allDocs.filter((d) => (d.sideEffects ?? []).includes('keystore-write'));
  check(writeCmds.length >= 2, 'the write path is exercised by more than one command', writeCmds.map((d) => d.name).join(', '));
  for (const doc of writeCmds) {
    check(
      doc.errors.includes('KeystoreUnavailable') && doc.errors.includes('SecretRedacted'),
      `a command that writes the keystore names both ways it can fail: ${doc.name}`,
      doc.errors.join(', '),
    );
  }
  for (const doc of allDocs) {
    if ((doc.sideEffects ?? []).includes('keystore-delete')) {
      check(doc.errors.includes('KeystoreUnavailable'), `a command that deletes secrets names the store failure: ${doc.name}`, doc.errors.join(', '));
    }
    if (doc.errors.includes('AuthFailed')) {
      check(doc.errors.includes('KeystoreUnavailable'), `a command that can be refused by a credential can be stopped by the store holding it: ${doc.name}`, doc.errors.join(', '));
    }
  }

  for (const key of ['headers', 'auth', 'body', 'gistToken', 'token']) {
    check(
      (envelopeKeys.syncExcludedValueKeys ?? []).includes(key),
      `the sync strip set covers the legacy credential field "${key}"`,
      'SEC-1 is two fields wide -- headers and the per-proxy auth map -- and SEC-5 is the sync backend token',
    );
  }

  // docs/architecture/32 §6: ingest is one door, not one per command that finds it convenient.
  const writeOnlyPaths = [];
  const findWriteOnly = (node, p) => {
    if (Array.isArray(node)) {
      for (const item of node) findWriteOnly(item, p);
      return;
    }
    if (!node || typeof node !== 'object') return;
    if (node.writeOnly === true) writeOnlyPaths.push(p);
    for (const [key, value] of Object.entries(node)) {
      if (key !== 'writeOnly') findWriteOnly(value, `${p}/${key}`);
    }
  };
  for (const doc of allDocs) findWriteOnly(doc.args ?? {}, doc.name);
  check(
    writeOnlyPaths.length === 1 && writeOnlyPaths[0].startsWith('profile.import'),
    'exactly one field in the whole command surface may receive a secret value',
    writeOnlyPaths.join(' | ') || 'none',
  );

  // docs/architecture/32 §11: three different remediations, so three different codes, and the
  // command that resolves references at compile time is the one that has to be able to name all three.
  const apply = commandDocs.get('config.apply')?.doc;
  check(
    ['InvalidProfile', 'KeystoreUnavailable', 'SecretRedacted'].every((c) => (apply?.errors ?? []).includes(c)),
    'the compiling command names every way a secret can be the reason nothing applied',
    (apply?.errors ?? []).join(', '),
  );
}

// ------------------------------------------------- the pin document and Core's own reach
//
// ADR-0019 decides what a permitted binary is; docs/architecture/33-binary-integrity.md argues how the
// Connector reaches that answer and what must not be reachable from the process afterwards. Two of its rules
// are cheap to keep honest mechanically. A pin document that half-updated -- the tag moved and the asset URLs
// did not -- is a document that fails at first run on a user's machine, which is the worst place to discover a
// typo. And a compiled config carrying `browserForwarder` hands Core a door back into the browser: ADR-0004
// arriving from the other side, inside a file the Connector wrote.
{
  const pins = await readJson(join(examplesDir, 'pins.xray.json'));
  const tag = pins?.upstream?.releaseTag;
  const assetFiles = (pins?.assets ?? []).map((a) => a.file);
  check(typeof tag === 'string' && assetFiles.length >= 1, 'the pins instance names a tag and at least one asset', `tag=${tag}, assets=${assetFiles.length}`);
  for (const asset of pins?.assets ?? []) {
    check(
      asset.url === `https://github.com/XTLS/Xray-core/releases/download/${tag}/${asset.file}`,
      'every asset URL is the pinned tag with the pinned file name',
      `${asset.os}/${asset.arch}: ${asset.url}`,
    );
  }
  check(
    assetFiles.includes(pins?.upstream?.assetNaming),
    'the declared asset naming is a file this document actually pins',
    `assetNaming=${pins?.upstream?.assetNaming}; pinned files: ${assetFiles.join(', ')}`,
  );
  check(
    /%LOCALAPPDATA%|\/home\/|\$\{?XDG_DATA_HOME/.test(pins?.installRoot ?? ''),
    'the install root is a per-user path, never a machine path',
    pins?.installRoot ?? 'absent',
  );
  check(
    (pins?.retainedPreviousVersions ?? 0) >= 1,
    'the shipped pin keeps a rollback target',
    `retainedPreviousVersions=${pins?.retainedPreviousVersions ?? 'absent'}`,
  );
}

{
  const FORBIDDEN_CORE_KEYS = ['browserForwarder', 'api', 'stats'];
  const offenders = [];
  let configs = 0;
  const walkConfig = (node, where) => {
    if (Array.isArray(node)) {
      for (const item of node) walkConfig(item, where);
      return;
    }
    if (!node || typeof node !== 'object') return;
    for (const [key, value] of Object.entries(node)) {
      if (FORBIDDEN_CORE_KEYS.includes(key)) offenders.push(`${where}: ${key}`);
      walkConfig(value, where);
    }
  };
  const walkFrame = (node, where) => {
    if (Array.isArray(node)) {
      for (const item of node) walkFrame(item, where);
      return;
    }
    if (!node || typeof node !== 'object') return;
    for (const [key, value] of Object.entries(node)) {
      // A `stats` field in a command *result* is the Connector reporting counters; the claim here is
      // about the config the Connector hands Core, so only that subtree is in scope.
      if (key === 'maskedConfig') {
        configs += 1;
        walkConfig(value, where);
        continue;
      }
      walkFrame(value, where);
    }
  };
  for (const [file, frames] of framesByFile) walkFrame(frames, file);
  check(offenders.length === 0, 'no recorded config gives Core a door back into the browser or its own control API', offenders.join(', ') || 'none');
  check(configs >= 1, 'a recorded maskedConfig exists for that scan to read', `${configs} occurrences`);
}

// ------------------------------------------------------ which layer owns which byte
//
// ADR-0010 rule 6 closes with the thing that makes it testable: the boundary "needs a
// catalogue -- which key is intent, which is observation, which is secret -- that must be
// maintained as data rather than remembered". protocol/data-ownership.json is that catalogue and
// docs/architecture/34-data-split.md is the argument for it. Putting it in data only pays if the
// catalogue can be cross-checked against the three things it claims to describe: the envelope-key
// registry, the command allowlist and the sync strip set. A prose table cannot drift from those in a
// way anyone notices; these assertions make every way it could drift fail the run.
{
  const own = await readJson(join(PKG, 'protocol', 'data-ownership.json'));
  const planes = new Set((own.planes ?? []).map((p) => p.id));
  const classes = new Map((own.classes ?? []).map((c) => [c.id, c]));
  const stores = own.stores ?? [];
  const movements = own.movements ?? [];

  check(
    stores.length >= 20 && movements.length >= 8 && planes.has('core'),
    'the ownership catalogue spans both planes and every movement',
    `stores=${stores.length}, movements=${movements.length}, planes=${[...planes].join('/')}`,
  );

  const badRefs = stores
    .filter((s) => !planes.has(s.plane) || !classes.has(s.class))
    .map((s) => `${s.id}: ${s.plane}/${s.class}`);
  check(badRefs.length === 0, 'every store row names a plane and a class this catalogue defines', badRefs.join(', ') || 'none');

  const badChecks = stores.filter((s) => typeof s.check !== 'string' || s.check.length < 8).map((s) => s.id);
  check(badChecks.length === 0, 'every store row names the check that owns it', badChecks.join(', ') || 'none');

  // The registry and the catalogue describe the same keys from two sides: what may travel, and where
  // it rests. Either one being updated alone is the drift SEC-1 was -- a mechanism that existed and was
  // never extended -- so the two sets must be equal and their booleans must agree.
  const rowsByPattern = new Map(stores.filter((s) => s.keyPattern).map((s) => [s.keyPattern, s]));
  const missing = envelopeKeys.namespaces.filter((ns) => !rowsByPattern.has(ns.keyPattern)).map((ns) => ns.entity);
  const invented = [...rowsByPattern.keys()].filter((k) => !envelopeKeys.namespaces.some((ns) => ns.keyPattern === k));
  check(missing.length === 0, 'every registered envelope namespace has an ownership row', missing.join(', ') || 'none');
  check(invented.length === 0, 'no ownership row invents a namespace the registry does not register', invented.join(', ') || 'none');

  const disagreed = envelopeKeys.namespaces
    .filter((ns) => {
      const row = rowsByPattern.get(ns.keyPattern);
      if (!row) return false;
      return row.secretBearing !== ns.secretBearing || row.syncEligible !== ns.syncEligible;
    })
    .map((ns) => `${ns.entity} sync=${rowsByPattern.get(ns.keyPattern)?.syncEligible}/${ns.syncEligible} secret=${rowsByPattern.get(ns.keyPattern)?.secretBearing}/${ns.secretBearing}`);
  check(disagreed.length === 0, 'the catalogue agrees with the registry about sync and secrets', disagreed.join(', ') || 'none');

  const syncableAgainstClass = envelopeKeys.namespaces
    .filter((ns) => {
      const row = rowsByPattern.get(ns.keyPattern);
      return ns.syncEligible && row && classes.get(row.class)?.maySync !== true;
    })
    .map((ns) => `${ns.entity} is class ${rowsByPattern.get(ns.keyPattern)?.class}`);
  check(
    syncableAgainstClass.length === 0,
    'a class that may not travel never describes a sync-eligible namespace',
    syncableAgainstClass.join(', ') || 'none',
  );

  const intentInConnector = stores.filter((s) => s.class === 'intent' && s.plane !== 'browser').map((s) => s.id);
  check(intentInConnector.length === 0, 'intent never rests in the Connector plane', intentInConnector.join(', ') || 'none');

  const connectorSyncs = stores.filter((s) => s.plane === 'connector' && s.syncEligible).map((s) => s.id);
  check(connectorSyncs.length === 0, 'nothing the Connector holds leaves the machine', connectorSyncs.join(', ') || 'none');

  const travelsWithValue = stores
    .filter((s) => s.secretBearing && s.syncEligible)
    .filter((s) => !/^syncExcludedValueKeys|^none \(open item/.test(s.mitigation ?? ''))
    .map((s) => s.id);
  check(
    travelsWithValue.length === 0,
    'a store that both travels and can hold a value names either the strip set or the open decision',
    travelsWithValue.join(', ') || 'none',
  );

  const secretRows = stores.filter((s) => s.class === 'secret');
  check(
    secretRows.length === 1 && secretRows[0].plane === 'connector',
    'credential material has exactly one home, and it is not in the browser',
    secretRows.map((s) => `${s.id} in ${s.plane}`).join(', ') || 'none',
  );

  const unexplained = stores
    .filter((s) => (s.secretValueKeys ?? []).length > 0 && !(typeof s.mitigation === 'string' && s.mitigation.length >= 8))
    .map((s) => `${s.id}: ${s.secretValueKeys.join('+')}`);
  check(
    unexplained.length === 0,
    'every row that can hold a credential value says how it is kept out of a transit',
    unexplained.join(', ') || 'none',
  );

  const stripRows = stores.filter((s) => (s.mitigation ?? '').startsWith('syncExcludedValueKeys'));
  const notStripped = stripRows.flatMap((s) => (s.secretValueKeys ?? []).filter((k) => !envelopeKeys.syncExcludedValueKeys.includes(k)).map((k) => `${s.id}.${k}`));
  check(
    stripRows.length >= 1 && notStripped.length === 0,
    'the strip set covers every credential key the catalogue says the synced envelope can hold',
    notStripped.join(', ') || `${stripRows.length} rows covered`,
  );

  const openRows = stores.filter((s) => (s.mitigation ?? '').startsWith('none (open item'));
  check(
    openRows.length === 1 && /40-open-decisions\.md/.test(openRows[0].mitigation),
    'exactly one credential is admitted to be reachable by a transit today, and it names the open decision',
    openRows.map((s) => `${s.id}: ${s.mitigation}`).join(' | ') || 'none',
  );

  const upWithValues = movements.filter((m) => m.direction === 'up' && m.carriesSecretValues).map((m) => m.id);
  check(upWithValues.length === 0, 'nothing that carries a credential value moves toward the browser', upWithValues.join(', ') || 'none');

  const downWithValues = movements.filter((m) => m.direction === 'down' && m.carriesSecretValues);
  check(
    downWithValues.length === 1 && downWithValues[0].command === 'profile.import',
    'exactly one movement carries a credential value into the Connector, and it is the ingest door',
    downWithValues.map((m) => `${m.id} via ${m.command}`).join(', ') || 'none',
  );

  const inventedCommands = movements.filter((m) => m.command && !commandDocs.has(m.command)).map((m) => `${m.id}: ${m.command}`);
  check(inventedCommands.length === 0, 'no movement names a command the surface does not have', inventedCommands.join(', ') || 'none');

  const badDirections = movements.filter((m) => !['down', 'up', 'aside'].includes(m.direction)).map((m) => `${m.id}: ${m.direction}`);
  const badEndpoints = movements
    .filter((m) => !(planes.has(m.from) || m.from === 'network') || !(planes.has(m.to) || m.to === 'network'))
    .map((m) => `${m.id}: ${m.from}->${m.to}`);
  check(
    badDirections.length === 0 && badEndpoints.length === 0,
    'every movement names a direction and endpoints this catalogue defines',
    [...badDirections, ...badEndpoints].join(', ') || 'none',
  );

  const liteBroken = stores.filter((s) => s.class === 'intent' && s.survivesConnectorRemoval !== true).map((s) => s.id);
  check(
    liteBroken.length === 0,
    'removing the Connector leaves every intent row readable, which is what "NEXORA Lite" means',
    liteBroken.join(', ') || 'none',
  );

  const immortal = stores.filter((s) => s.plane === 'connector' && s.survivesConnectorRemoval !== false).map((s) => s.id);
  check(immortal.length === 0, 'no Connector-plane row claims to outlive the Connector', immortal.join(', ') || 'none');

  // The catalogue's `check` column is only worth having if the rows it points at exist. This reads the
  // live matrix for the first time here, and deliberately so: a claim about where something is verified
  // must be checked against the document that holds the verifications, not against a comment.
  const matrixText = await readFile(join(REPO, 'docs', 'verification', 'live-matrix.md'), 'utf8');
  const citedRows = new Set();
  for (const m of [...stores, ...movements]) {
    for (const match of String(m.check ?? '').matchAll(/LIVE-\d{2}/g)) citedRows.add(match[0]);
  }
  const matrixRowIds = new Set([...matrixText.matchAll(/^\| (LIVE-\d{2}) \|/gm)].map((m) => m[1]));
  const dangling = [...citedRows].filter((id) => !matrixRowIds.has(id));
  check(
    dangling.length === 0 && citedRows.size >= 8,
    'every live row the ownership catalogue cites exists in the matrix',
    dangling.length ? dangling.join(', ') : `${citedRows.size} distinct rows cited`,
  );
}

// ------------------------------------------------------------ the migration ledger
//
// ADR-0014 makes an envelope migration ordered, idempotent, resumable, dry-run-able and
// backup-gated; docs/architecture/35-schema-versioning-migration.md is the argument for it;
// migrations.json is the step list. The list is data rather than a table in that document because
// the two facts that decide whether a run is safe are claims about things that can change under the
// design: that version 3 writes no key the registry has not declared, and that the bare legacy stamp
// the old engine reads still defaults to the one value that engine accepts. A prose table would age
// silently. These assertions read the legacy source, the key registry and the backup schema.

{
  const mig = await readJson(join(PKG, 'migrations.json'));
  const steps = [...(mig.steps ?? [])].sort((a, b) => a.order - b.order);
  const legacy = mig.versionStamps?.legacy ?? {};
  const nexora = mig.versionStamps?.nexora ?? {};

  const orderGaps = steps.filter((s, i) => s.order !== i + 1).map((s) => s.id);
  const backward = steps.filter((s) => s.to < s.from).map((s) => `${s.id}: ${s.from}->${s.to}`);
  const aboveHead = steps.filter((s) => s.to > (nexora.head ?? 0)).map((s) => s.id);
  check(
    steps.length >= 8 && orderGaps.length === 0 && backward.length === 0 && aboveHead.length === 0,
    'the ledger is one strictly forward chain over contiguous step numbers',
    `steps=${steps.length}, gaps=${orderGaps.join('/') || 'none'}, backward=${backward.join('/') || 'none'}, aboveHead=${aboveHead.join('/') || 'none'}`,
  );

  const maxTo = steps.reduce((m, s) => Math.max(m, s.to), 0);
  const firstFrom = steps.length ? Math.min(...steps.map((s) => s.from)) : 0;
  check(
    maxTo === nexora.head && firstFrom === legacy.mustRemain && nexora.key === envelopeKeys.schemaVersionKey,
    'the ledger spans exactly the version the envelope registry names, starting from the legacy stamp',
    `from=${firstFrom}, head=${maxTo}, declared=${nexora.head}, registryKey=${envelopeKeys.schemaVersionKey}`,
  );

  // The premise of the whole ledger, read from the engine rather than remembered: the shipped default
  // still says 2, the gate that rejects every other value is still in place, and the caller that turns a
  // rejection into a wipe still removes storage before writing defaults. If any of those three move,
  // "keep the bare stamp at 2" stops being the right rule and this run has to say so out loud.
  const legacyDefault = await readFile(join(REPO, 'omega-target', 'src', 'default_options.coffee'), 'utf8');
  const legacyOptions = await readFile(join(REPO, 'omega-target', 'src', 'options.coffee'), 'utf8').then((t) => t.split('\n'));
  const stampLineOk = /schemaVersion:\s*2\b/.test(legacyDefault.split('\n')[1] ?? '');
  const gateKeepsTwo = /\bif version == 2\b/.test(legacyOptions[348] ?? '');
  const gateRejects = /Invalid schemaVersion/.test(legacyOptions[352] ?? '');
  const fallbackWipes = /@_storage\.remove\(\)/.test(legacyOptions[253] ?? '');
  check(
    stampLineOk && gateKeepsTwo && gateRejects && fallbackWipes,
    'the erase-on-unknown chain the ledger is written against is still the chain in the engine',
    `defaultIs2=${stampLineOk}, gate=${gateKeepsTwo}, rejects=${gateRejects}, fallbackRemoves=${fallbackWipes} (docs/architecture/35-schema-versioning-migration.md section 3)`,
  );

  const ungated = steps.filter((s) => s.requiresBackup !== true).map((s) => s.id);
  check(
    ungated.length === 0,
    'every step is backup-gated, destructive or not, because rollback needs a before-image',
    ungated.join(', ') || 'none',
  );

  const pruning = steps.filter((s) => (s.removes ?? []).length > 0).map((s) => `${s.id}: ${s.removes.join('|')}`);
  check(pruning.length === 0, 'version three adds keys and prunes none (ADR-0014 rule 7)', pruning.join(', ') || 'none');

  // A write is legal only if a registered namespace accepts it. `<id>` is instantiated with a UUID,
  // which is what the registry's own patterns ask for, so a step that invents `-nexora.whatever` fails here
  // instead of failing in the field as a key sync, export and the strip set all ignore.
  const UUID = '0f8e2a6c-1b3d-4e5f-8a9b-0c1d2e3f4a5b';
  const keyIsRegistered = (key) => {
    const probe = key.replace('<id>', UUID);
    if (probe === envelopeKeys.schemaVersionKey) return true;
    return (envelopeKeys.namespaces ?? []).some((ns) => {
      try {
        return new RegExp(ns.keyPattern).test(probe);
      } catch {
        return false;
      }
    });
  };
  const unregistered = [];
  for (const s of steps) {
    for (const w of s.writes ?? []) if (!keyIsRegistered(w)) unregistered.push(`${s.id}: ${w}`);
  }
  check(
    unregistered.length === 0,
    'no step writes an envelope key the registry does not declare',
    unregistered.join(', ') || 'none',
  );

  // The mirror of the rule above, and the one ADR-0013 depends on: a step may read the legacy engine's
  // keys, and may not answer to them. Writing a `+` key, an attachment key or a legacy-owned scalar is
  // the cut-over arriving early inside a migration nobody approved.
  const legacyOwnedSet = new Set(envelopeKeys.legacyOwned ?? []);
  const overwriting = [];
  for (const s of steps) {
    for (const w of s.writes ?? []) {
      if (w.startsWith('+') || w.startsWith('__ruleListOf_') || legacyOwnedSet.has(w) || w === legacy.key) {
        overwriting.push(`${s.id}: ${w}`);
      }
    }
  }
  check(
    overwriting.length === 0,
    'no step writes a key the legacy engine owns, including the bare stamp it must preserve',
    overwriting.join(', ') || 'none',
  );

  const thin = steps
    .filter((s) => (s.verify ?? '').length < 30 || (s.idempotence ?? '').length < 10 || (s.reversibleBy ?? '').length < 6 || (s.up ?? '').length < 30)
    .map((s) => s.id);
  check(
    thin.length === 0,
    'every step declares an up, a verifier, an idempotence proof and a way back',
    thin.join(', ') || 'none',
  );

  const readsLegacy = steps.filter((s) => (s.reads ?? []).length > 0).map((s) => s.id);
  check(
    readsLegacy.length >= 4 && steps.every((s) => Array.isArray(s.reads) && Array.isArray(s.writes) && Array.isArray(s.removes)),
    'the ledger distinguishes what a step reads from what it writes',
    `${readsLegacy.length} steps read legacy keys; ${steps.length} declare all three lists`,
  );

  const journal = mig.journal ?? {};
  const cursorWrites = steps.filter((s) => (s.writes ?? []).some((w) => /migrate|journal|cursor|state/i.test(w))).map((s) => s.id);
  check(
    journal.usesBackupLedger === true &&
      cursorWrites.length === 0 &&
      (mig.run?.resumableBy ?? '').includes('step one') &&
      /cursor/.test(journal.whyNoCursorKey ?? ''),
    'resume needs no undeclared key: the backup ledger is the journal and replay starts at step one',
    `journal=${journal.usesBackupLedger}, cursorWrites=${cursorWrites.join('/') || 'none'}`,
  );

  const backupRequired = (await readJson(join(PKG, 'envelope-backup.schema.json'))).required ?? [];
  const resumeFieldsMissing = (journal.fieldsReadOnResume ?? []).filter((f) => !backupRequired.includes(f));
  check(
    (journal.fieldsReadOnResume ?? []).length >= 5 && resumeFieldsMissing.length === 0,
    'the backup record already carries every field a resume has to read',
    resumeFieldsMissing.join(', ') || `${journal.fieldsReadOnResume?.length} fields, all required`,
  );

  const codes = new Set(taxonomy.codes.map((c) => c.code));
  const newer = mig.onNewerEnvelope ?? {};
  check(
    newer.writesAllowed === false &&
      codes.has(newer.error) &&
      (newer.legacyCounterexample ?? '').includes('options.coffee') &&
      /read-only/.test(newer.policy ?? ''),
    'a future envelope is held read-only, never migrated downward, and names the error that means it',
    `policy=${newer.policy}, error=${newer.error} in taxonomy=${codes.has(newer.error)}`,
  );

  const future = mig.futureVersions ?? [];
  const premature = future.filter((v) => v.executed !== false || v.adrRequired !== true || v.to <= (nexora.head ?? 0)).map((v) => v.to);
  check(
    future.length >= 2 && premature.length === 0,
    'the destructive versions are declared as futures that need their own ADR, not as steps',
    premature.length ? premature.join(', ') : future.map((v) => `${v.to}:${v.name}`).join(', '),
  );

  const resolvePointer = (doc, pointer) =>
    pointer
      .split('/')
      .filter(Boolean)
      .reduce((cur, seg) => (cur == null ? cur : cur[seg.replace(/~1/g, '/').replace(/~0/g, '~')]), doc);
  const spaceProblems = [];
  for (const space of mig.otherVersionSpaces ?? []) {
    const [file, pointer] = String(space.carriedBy ?? '').split('#');
    let doc;
    try {
      doc = await readJson(join(PKG, file.replace(/^packages\/schema\//, '')));
    } catch {
      spaceProblems.push(`${space.name}: ${file} unreadable`);
      continue;
    }
    const value = resolvePointer(doc, pointer);
    const actual = value && typeof value === 'object' ? (value.const ?? value.default ?? value.type) : value;
    if (space.value === null) {
      if (actual !== 'integer') spaceProblems.push(`${space.name}: declared unbounded, schema says ${JSON.stringify(actual)}`);
    } else if (JSON.stringify(actual) !== JSON.stringify(space.value)) {
      spaceProblems.push(`${space.name}: ledger says ${space.value}, schema says ${JSON.stringify(actual)}`);
    }
  }
  check(
    (mig.otherVersionSpaces ?? []).length >= 4 && spaceProblems.length === 0,
    'the other version spaces the runner must never bump are stated at their real values',
    spaceProblems.join('; ') || `${mig.otherVersionSpaces?.length} spaces: settings, failure policy, pins, protocol`,
  );

  const retention = mig.retention ?? {};
  const sampleBackupKey = '-nexora.backup.2026-11-04T07:55:00Z';
  const backupNamespace = (envelopeKeys.namespaces ?? []).find((ns) => {
    try {
      return new RegExp(ns.keyPattern).test(sampleBackupKey);
    } catch {
      return false;
    }
  });
  check(
    retention.keepCompleted >= 1 &&
      String(retention.mayPruneOnlyKeysMatching ?? '').startsWith('-nexora.backup.') &&
      backupNamespace?.entity === 'EnvelopeBackup' &&
      (retention.neverPrune ?? []).length >= 3,
    'pruning is limited to the backup namespace and never to zero snapshots',
    `keep=${retention.keepCompleted}, scope=${retention.mayPruneOnlyKeysMatching}, registered as=${backupNamespace?.entity ?? 'none'}`,
  );

  const weakEvidence = (mig.forbidden ?? []).filter(
    (f) => (f.evidence ?? '').length < 20 || !/(omega-|packages\/schema|docs\/)/.test(f.evidence ?? '') || (f.why ?? '').length < 40,
  );
  check(
    (mig.forbidden ?? []).length >= 8 && weakEvidence.length === 0,
    'every prohibition in the ledger cites the code or the contract that earns it',
    weakEvidence.map((f) => f.rule.slice(0, 40)).join('; ') || `${mig.forbidden.length} prohibitions`,
  );

  const migrationDoc = join(REPO, 'docs', 'architecture', '35-schema-versioning-migration.md');
  if (existsSync(migrationDoc)) {
    const text = await readFile(migrationDoc, 'utf8');
    const stepIds = steps.map((s) => s.id);
    const unnamed = stepIds.filter((id) => !text.includes(`\`${id}\``));
    check(
      unnamed.length === 0 && text.includes('migrations.json'),
      'the migration document names every step the ledger declares, so the two cannot disagree quietly',
      unnamed.length ? `steps absent from doc 35: ${unnamed.join(', ')}` : `${stepIds.length} steps named`,
    );
  } else {
    note('DEFERRED: 35-schema-versioning-migration.md cross-check (file not present yet)');
  }
}

// ---------------------------------------------------- the log as a contract
//
// docs/architecture/36-observability.md is the argument; observability.json is the list. It exists as
// data because six contracts describe the same vocabulary from six sides and a log implementation
// discovers a disagreement at runtime, in the shape of a diagnostics section the Connector cannot
// produce or an event frame no subscriber can validate. docs/architecture/26-command-surface.md
// promises that the catalogue parity is checked here, so this is where it happens: the event names and
// channels against events.schema.json, the section names against diagnostics.get, the default section
// set against settings.default.json, the level and age bounds against connector-settings.schema.json,
// the stores against protocol/data-ownership.json, the byte caps against framing.json, and every
// recorded event frame in fixtures/protocol against the payload its own catalogue entry declares.

{
  const obs = await readJson(join(PKG, 'observability.json'));
  const own = await readJson(join(PKG, 'protocol', 'data-ownership.json'));
  const storeIds = new Set(own.stores.map((s) => s.id));
  const planeIds = new Set(own.planes.map((p) => p.id));
  const evDefs = (await readJson(join(PKG, 'events.schema.json'))).$defs;
  const channels = new Set(evDefs.catalog.properties.channel.enum);
  const diagCmd = commandDocs.get('diagnostics.get').doc;
  const sectionNames = new Set(diagCmd.args.properties.include.items.enum);
  const maxBytesCap = diagCmd.args.properties.maxBytes.maximum;
  const telemetry = ajv
    .getSchema('https://schema.nexora.local/connector-settings.schema.json')
    .schema.properties.telemetry.properties;

  // ---- sinks: the enumerable list masking applies at

  const sinks = obs.sinks ?? [];
  check(
    sinks.length >= 5 && new Set(sinks.map((s) => s.id)).size === sinks.length,
    'the sink list is closed and every sink has one name',
    `${sinks.length} sinks: ${sinks.map((s) => s.id).join(', ')}`,
  );
  const strayStore = sinks.filter((s) => s.store !== null && !storeIds.has(s.store)).map((s) => s.id);
  check(
    strayStore.length === 0,
    'every sink names a store the ownership catalogue declares',
    `not a store: ${strayStore.join(', ') || 'none'}`,
  );
  const noStage = sinks.filter((s) => !(s.maskingStage ?? '').length).map((s) => s.id);
  check(noStage.length === 0, 'every sink says at which stage the mask runs', noStage.join(', ') || 'none');

  const LOG_STORES = ['diagnostics-ring', 'connector-log-file', 'core-log-file', 'extension-log'];
  const sinkStores = new Set(sinks.map((s) => s.store).filter(Boolean));
  const unmasked = LOG_STORES.filter((id) => !sinkStores.has(id));
  check(
    unmasked.length === 0 && sinks.filter((s) => s.store === null).length === 1,
    'every log-backed store has a sink that masks it, and the only store-less sink is the export',
    `unmasked=${unmasked.join('/') || 'none'}`,
  );
  const ring = sinks.find((s) => s.id === 'connector-ring');
  check(
    ring?.bounds?.bytes === framing.limits.effectiveCrossBrowserBytes.value,
    'a full Connector ring is exactly one frame, so the answer to a question needs no reassembly',
    `ring=${ring?.bounds?.bytes}, chunk ceiling=${framing.limits.effectiveCrossBrowserBytes.value}`,
  );

  // ---- the record: one flat declared shape, every field bounded

  const fields = obs.record?.fields ?? [];
  const names = fields.map((f) => f.name);
  check(
    names.length >= 11 && new Set(names).size === names.length,
    'the record declares a flat set of fields with no repeats',
    `${names.length} fields`,
  );
  const unbounded = fields.filter((f) => !(f.bound ?? '').length).map((f) => f.name);
  check(unbounded.length === 0, 'every log field states its own bound', unbounded.join(', ') || 'none');
  const component = fields.find((f) => f.name === 'component');
  const wireStage = envelopeSchema.schema.properties.progress.properties.stage.maxLength;
  check(
    component?.maxLength === wireStage,
    'a component name that fits the log always fits the frame it is reported on',
    `component=${component?.maxLength}, envelope progress.stage=${wireStage}`,
  );
  const freeText = fields.filter((f) => f.type === 'string' && f.required && f.name === 'msg');
  check(
    freeText.length === 1 && freeText[0].maxLength > 0 && freeText[0].maxLength <= 500,
    'exactly one log field is free text, and it is capped in characters rather than in prose',
    `msg maxLength=${freeText[0]?.maxLength}`,
  );

  // ---- levels: two vocabularies, one mapping

  check(
    JSON.stringify(obs.levels?.names) === JSON.stringify(telemetry.logLevel.enum),
    'the log level vocabulary is the settings vocabulary, not a second one',
    `log: ${(obs.levels?.names ?? []).join('/')} vs settings: ${telemetry.logLevel.enum.join('/')}`,
  );
  // https://xtls.github.io/en/config/log.html -- the words Xray accepts in log.loglevel.
  const XRAY_LOG_LEVELS = ['error', 'warning', 'info', 'debug'];
  const coreWords = obs.levels?.toCore ?? {};
  const badCore = Object.values(coreWords).filter((v) => !XRAY_LOG_LEVELS.includes(v));
  check(
    Object.keys(coreWords).sort().join() === [...(obs.levels?.names ?? [])].sort().join() &&
      badCore.length === 0 &&
      coreWords.warn === 'warning',
    'the one word NEXORA and Xray disagree on is mapped by the Compiler, never passed through',
    `bad=${badCore.join('/') || 'none'}, warn -> ${coreWords.warn ?? 'absent'}`,
  );

  // ---- events: the catalogue, its payloads, and the frames that prove them

  const events = obs.events ?? [];
  const declared = new Set(events.map((e) => e.name));
  const missingFromObs = [...eventNames].filter((n) => !declared.has(n));
  const inventedByObs = [...declared].filter((n) => !eventNames.has(n));
  check(
    missingFromObs.length === 0 && inventedByObs.length === 0,
    'the observability catalogue is exactly the registered event set',
    `absent=${missingFromObs.join('/')} invented=${inventedByObs.join('/')}`,
  );
  const badChannel = events.filter((e) => !channels.has(e.channel)).map((e) => `${e.name}:${e.channel}`);
  check(badChannel.length === 0, 'every event names a channel the event schema declares', badChannel.join(', ') || 'none');

  // events.schema.json#/$defs/catalog declares what a catalogue entry must look like; this document is
  // the only catalogue. A projection of each entry onto that shape is the check that both are true --
  // the entry is valid *and* the extra fields here are commentary, never a second authority.
  const catalogEntry = ajv.compile({ $ref: 'https://schema.nexora.local/events.schema.json#/$defs/catalog' });
  const badEntries = events
    .map((e) => ({ name: e.name, e: catalogEntry({ name: e.name, channel: e.channel, retained: e.retained, sampled: e.sampled, payload: e.payload }) }))
    .filter((r) => !r.e)
    .map((r) => r.name);
  check(
    badEntries.length === 0,
    'every catalogue entry is a valid instance of the catalog shape the event schema declares',
    badEntries.join(', ') || `${events.length} entries`,
  );

  const loosePayloads = events
    .filter((e) => e.payload?.type !== 'object' || e.payload.additionalProperties !== false)
    .map((e) => e.name);
  check(
    loosePayloads.length === 0,
    'every event payload is a closed object, so no event can carry a field nobody declared',
    loosePayloads.join(', ') || 'none',
  );
  const payloadValidators = new Map();
  for (const e of events) {
    if (e.payload?.type !== 'object') continue;
    const unknown = (e.payload.required ?? []).filter((k) => !(k in (e.payload.properties ?? {})));
    check(unknown.length === 0, `event payload requires only what it declares: ${e.name}`, unknown.join(', '));
    try {
      payloadValidators.set(e.name, ajv.compile(e.payload));
    } catch (err) {
      check(false, `event payload compiles: ${e.name}`, err.message);
    }
  }

  const STATE_WORDS = /^(state|status|healthy|latency|latencyMs|error|message|url|host|port)$/i;
  const stateCarriers = events
    .filter((e) => Object.keys(e.payload?.properties ?? {}).some((k) => STATE_WORDS.test(k)))
    .map((e) => e.name);
  check(
    stateCarriers.length === 0,
    'no event payload carries a state, a destination or a rendered message that a read command also reports',
    `${stateCarriers.join(', ') || 'none'} -- an event is a hint; docs/architecture/30-failure-policy.md section 4 forbids the cached answer`,
  );

  const recordedEvents = new Set();
  for (const [file, frames] of framesByFile) {
    for (const frame of frames.filter((f) => f.type === 'event')) {
      const name = frame.progress?.event;
      const v = payloadValidators.get(name);
      if (!v) continue;
      const ok = v(frame.result);
      check(
        ok,
        `recorded event frame matches its declared payload: ${file}#${frame.id}`,
        ok ? '' : JSON.stringify(v.errors?.slice(0, 3)),
      );
      recordedEvents.add(name);
    }
  }
  check(recordedEvents.size >= 10, 'the fixture corpus exercises most of the catalogue', `${recordedEvents.size} of ${declared.size} events carry a recorded payload`);
  const unrecorded = [...declared].filter((n) => !recordedEvents.has(n)).sort();
  check(
    unrecorded.join() === 'connector.disconnected,keystore.unavailable',
    'the only events with no recorded frame are the two that cannot have one on this pipe',
    `unrecorded=${unrecorded.join(', ')} -- a process cannot announce its own exit through the channel it no longer owns, and a locked keystore is not a fixture`,
  );

  const sampled = events.filter((e) => e.sampled);
  const rate = commandDocs.get('connector.events.subscribe').doc.args.properties.maxPerSecond;
  check(
    sampled.length === 1 && sampled[0].name === 'routing.decision' && rate.maximum === 50 && rate.default === 10,
    'one sampled event, one rate ceiling, and the ceiling exists for it',
    `sampled=${sampled.map((e) => e.name).join('/')} default=${rate.default} max=${rate.maximum}`,
  );
  // Every event tells the reader where to ask the follow-up question, and that place has to exist.
  const detailMissing = events
    .filter((e) => !commandDocs.has((e.detailCommand ?? '').split(' ')[0]))
    .map((e) => `${e.name} -> ${e.detailCommand ?? 'absent'}`);
  check(detailMissing.length === 0, 'every event names a real command for its follow-up', detailMissing.join('; ') || 'none');

  const pullEvents = events.filter((e) => e.delivery === 'push-then-pull');
  const noSubject = pullEvents
    .filter((e) => !(e.payload?.required ?? []).some((k) => /Id$/.test(k)))
    .map((e) => e.name);
  const noPull = pullEvents.filter((e) => !commandDocs.has((e.detailCommand ?? '').split(' ')[0])).map((e) => e.name);
  check(
    pullEvents.length >= 2 && noSubject.length === 0 && noPull.length === 0,
    'a push-then-pull event names its subject and points at a command that actually holds the state',
    `count=${pullEvents.length} no subject=${noSubject.join('/')} no pull=${noPull.join('/')}`,
  );

  // ---- diagnostics sections: named by the command, defaulted by the settings

  const sections = obs.diagnosticsSections ?? [];
  const secNames = new Set(sections.map((s) => s.name));
  const secMissing = [...sectionNames].filter((n) => !secNames.has(n));
  const secInvented = [...secNames].filter((n) => !sectionNames.has(n));
  check(
    secMissing.length === 0 && secInvented.length === 0,
    'the diagnostics catalogue is exactly the sections the command can produce',
    `absent=${secMissing.join('/')} invented=${secInvented.join('/')}`,
  );
  const settingsExample = await readJson(join(examplesDir, 'settings.default.json'));
  const shippedDefault = new Set(settingsExample.telemetry.diagnosticsIncludeByDefault);
  const defaultSet = new Set(sections.filter((s) => s.defaultIncluded).map((s) => s.name));
  const defaultDrift = [...new Set([...shippedDefault, ...defaultSet])].filter((n) => !shippedDefault.has(n) || !defaultSet.has(n));
  check(
    defaultDrift.length === 0,
    'the sections the catalogue marks default-included are exactly the sections the shipped settings ask for',
    `drift=${defaultDrift.join('/') || 'none'}`,
  );
  const badSectionStore = sections.filter((s) => !storeIds.has(s.store)).map((s) => `${s.name}:${s.store}`);
  check(
    badSectionStore.length === 0,
    'every diagnostics section reads a store the ownership catalogue declares',
    badSectionStore.join(', ') || 'none',
  );
  const badSectionPlane = sections.filter((s) => !planeIds.has(s.plane)).map((s) => s.name);
  check(badSectionPlane.length === 0, 'every diagnostics section names a plane', badSectionPlane.join(', ') || 'none');
  const oversized = sections.filter((s) => s.maxBytes > maxBytesCap || s.maxBytes < 4096).map((s) => `${s.name}=${s.maxBytes}`);
  check(
    oversized.length === 0 && sections.every((s) => !(s.neverIn ?? '').length || s.name === 'routing-decisions'),
    'no section can ask for more bytes than one call may return',
    oversized.join(', ') || `${sections.length} sections within ${maxBytesCap}`,
  );

  // ---- bounds that live in another file, and are repeated here only to be compared

  const days = obs.retention?.days ?? {};
  const rd = telemetry.retentionDays;
  check(
    days.default === rd.default && days.minimum === rd.minimum && days.maximum === rd.maximum,
    'the retention window is the settings window, restated and equal',
    `here=${JSON.stringify(days)} settings={default:${rd.default},min:${rd.minimum},max:${rd.maximum}}`,
  );
  check(
    obs.export?.maxBytes === maxBytesCap,
    'an export can never be larger than what one diagnostics call was allowed to produce',
    `export=${obs.export?.maxBytes} command ceiling=${maxBytesCap}`,
  );

  const fpExample = await readJson(join(examplesDir, 'failure-policy.default.json'));
  const { min: blockLo, max: blockHi } = fpExample.block.portRange;
  const { min: inboundLo, max: inboundHi } = settingsExample.ports.range;
  const inForce = obs.inForceBlockPort ?? {};
  check(
    blockLo > inboundHi && inForce.range?.endsWith('#/block/portRange') && Number.isInteger(blockLo) && blockHi - blockLo >= 1,
    'the in-force block port comes from a range disjoint from every inbound range',
    `inbounds=${inboundLo}-${inboundHi}, block=${blockLo}-${blockHi}, range citation=${inForce.range ?? 'absent'}`,
  );

  check(
    obs.coreLog?.section?.access === 'none' && obs.coreLog?.accessLogDecision?.isUserSetting === false,
    'Xray access logging is stated off by the Compiler and is not a user setting',
    `access=${obs.coreLog?.section?.access}, isUserSetting=${obs.coreLog?.accessLogDecision?.isUserSetting}`,
  );

  // ---- prohibitions carry the evidence that earned them

  const prohibitions = [
    ...(obs.forbidden ?? []).map((f) => ({ ...f, kind: 'forbidden' })),
    ...(obs.neverLogged ?? []).map((f) => ({ ...f, kind: 'neverLogged' })),
  ];
  const weak = prohibitions
    .filter((p) => (p.evidence ?? '').length < 8 || (p.reason ?? p.why ?? '').length < 40)
    .map((p) => `${p.kind}: ${(p.rule ?? p.thing ?? '').slice(0, 40)}`);
  check(
    (obs.forbidden ?? []).length >= 10 && (obs.neverLogged ?? []).length >= 7 && weak.length === 0,
    'every prohibition and every never-logged class cites the code, the contract or the finding behind it',
    weak.join('; ') || `${obs.forbidden.length} prohibitions, ${obs.neverLogged.length} never-logged classes`,
  );

  // ---- doc 36 names the data it argues about

  const observabilityDoc = join(REPO, 'docs', 'architecture', '36-observability.md');
  const doc23 = join(REPO, 'docs', 'architecture', '23-unified-server-model.md');
  if (existsSync(observabilityDoc)) {
    const text = await readFile(observabilityDoc, 'utf8');
    const unnamedSinks = sinks.filter((s) => !text.includes(`\`${s.id}\``)).map((s) => s.id);
    const unnamedEvents = events.filter((e) => !text.includes(`\`${e.name}\``)).map((e) => e.name);
    const unnamedSections = sections.filter((s) => !text.includes(`\`${s.name}\``)).map((s) => s.name);
    const absent = [...unnamedSinks, ...unnamedEvents, ...unnamedSections];
    check(
      absent.length === 0 && text.includes('observability.json'),
      'the observability document names every sink, event and section the data declares',
      absent.length ? `absent from doc 36: ${absent.join(', ')}` : `${sinks.length} sinks, ${events.length} events, ${sections.length} sections named`,
    );
    const compiled = await readFile(doc23, 'utf8');
    check(
      compiled.includes('"access": "none"'),
      'the illustrative compiled shapes do not leave the access log to an upstream default',
      'docs/architecture/23-unified-server-model.md section 9',
    );

    // The log's `component` field copies a vocabulary owned elsewhere: doc 22's index. If a component
    // is added there with a name longer than the cap, or the index stops being an index, the log field
    // silently stops describing anything.
    const contractsDoc = await readFile(join(REPO, 'docs', 'architecture', '22-component-contracts.md'), 'utf8');
    const index = contractsDoc.split('## 0. Index')[1]?.split('## 1. ExtensionShell')[0] ?? '';
    const components = [...index.matchAll(/^\| ([A-Z][A-Za-z0-9]+) \|/gm)].map((m) => m[1]);
    const tooLong = components.filter((c) => c.length > (component?.maxLength ?? 0));
    check(
      components.length >= 12 && tooLong.length === 0,
      'the component vocabulary the log copies is a readable index and every name fits the cap',
      `components=${components.length} too long=${tooLong.join('/') || 'none'}`,
    );
  } else {
    note('DEFERRED: 36-observability.md cross-check (file not present yet)');
  }
}

// ---------------------------------------------------------------- the screen as a contract
//
// docs/architecture/37-ui-architecture.md is the argument; presentation.json is the list. The
// surface layer is the one contract in this repository that was cited by eight other documents
// before it had a home, and a screen cannot be validated the way a frame can -- so it is data, and
// the data is cross-read against the contracts it borrows. What is checked here is the disagreement
// a build would otherwise discover at runtime: a surface naming a command the allowlist does not
// carry, an event no surface renders, a state anchored to a code nobody defined, a confirmation set
// remembered instead of derived from the command documents, a theme count that drifted from the
// palettes on disk, a locale number taken from a comment instead of from the build task, and a
// parity row whose legacy evidence is a file that does not exist. Every legacy number below is
// re-measured from the inherited tree on each run, because a parity promise is a claim about that
// tree and the tree is still in this repository.

{
  const pres = await readJson(join(PKG, 'presentation.json'));
  const docIds = pres.documents.map((d) => d.id);
  const surfaceIds = pres.surfaces.map((s) => s.id);
  const stateIds = pres.states.map((s) => s.id);

  // ---- evidence: a cite is a path, optionally with a line suffix, a section or a pointer

  const citedFile = (text) => {
    const head = String(text).trim().split(/\s+/)[0];
    const file = head.split('#')[0].replace(/:\d+(?:-\d+)?$/, '');
    if (!file.includes('/')) return null;
    return existsSync(join(REPO, file)) || existsSync(join(PKG, file)) ? file : null;
  };
  const at = (doc, pointer) =>
    String(pointer ?? '')
      .split('/')
      .filter(Boolean)
      .reduce((v, seg) => (v == null ? undefined : v[seg.replace(/~1/g, '/').replace(/~0/g, '~')]), doc);
  // A pointer inside an evidence string has to resolve in the file it names, because the claim is
  // about a field, not about a file existing.
  const evidenceOk = async (text) => {
    const head = String(text).trim().split(/\s+/)[0];
    const file = citedFile(text);
    if (!file) return false;
    const [, pointer] = head.split('#');
    if (pointer === undefined) return true;
    const abs = existsSync(join(REPO, file)) ? join(REPO, file) : join(PKG, file);
    if (!file.endsWith('.json')) return true;
    return at(await readJson(abs), pointer) !== undefined;
  };

  // ---- where the UI lives

  check(
    pres.documents.length >= 3 && new Set(docIds).size === docIds.length,
    'the document list is closed and every document has one name',
    `${docIds.length} documents: ${docIds.join(', ')}`,
  );
  check(
    pres.surfaces.length >= 10 && new Set(surfaceIds).size === surfaceIds.length,
    'the surface list is closed and every surface has one name',
    `${surfaceIds.length} surfaces: ${surfaceIds.join(', ')}`,
  );
  check(
    pres.surfaces.every((s) => docIds.includes(s.document)),
    'every surface lives in a declared document',
    `unknown documents: ${[...new Set(pres.surfaces.map((s) => s.document))].filter((d) => !docIds.includes(d)).join(', ') || 'none'}`,
  );
  check(
    docIds.every((d) => pres.surfaces.some((s) => s.document === d)),
    'every declared document hosts at least one surface',
    `empty documents: ${docIds.filter((d) => !pres.surfaces.some((s) => s.document === d)).join(', ') || 'none'}`,
  );
  check(
    docIds.includes('nexora-popup.html') &&
      pres.surfaces.filter((s) => s.document === 'nexora-popup.html').length === 1,
    'the popup is exactly one surface, so the two-second screen has one owner',
    `${pres.surfaces.filter((s) => s.document === 'nexora-popup.html').length} surfaces in the popup document`,
  );
  const slotted = pres.documents.filter((d) => (d.slots ?? []).length > 0);
  const mounted = pres.surfaces.filter((s) => s.document === 'legacy-mount');
  check(
    slotted.length === 1 &&
      slotted[0].slots.length > 0 &&
      slotted[0].slots.length <= 3 &&
      slotted[0].slots.every((slot) => slot.startsWith('nexora-')) &&
      [...slotted[0].slots].sort().join(',') === mounted.map((s) => `nexora-${s.id}`).sort().join(','),
    'the legacy page hands over a bounded set of nexora-prefixed slots and every slot is one declared surface',
    `slots=${slotted.map((d) => d.slots.join('/')).join(' ') || 'none'} mounted surfaces=${mounted.map((s) => s.id).join(', ')}`,
  );
  check(
    pres.surfaces.every((s) => (s.purpose ?? '').length >= 20 && (s.note ?? '').length >= 40),
    'every surface says what it is for and what its boundary means',
    `thin surfaces: ${pres.surfaces.filter((s) => (s.purpose ?? '').length < 20 || (s.note ?? '').length < 40).map((s) => s.id).join(', ') || 'none'}`,
  );

  // ---- what the UI may say over the pipe

  const namedCommands = new Set(pres.surfaces.flatMap((s) => s.commands));
  const strayCommands = [...namedCommands].filter((c) => !allowNames.includes(c));
  check(
    strayCommands.length === 0,
    'no surface names a command the allowlist does not carry',
    `not allowed: ${strayCommands.join(', ') || 'none'}`,
  );
  const namedEvents = new Set(pres.surfaces.flatMap((s) => s.events));
  const strayEvents = [...namedEvents].filter((e) => !eventNames.has(e));
  const unrouted = [...eventNames].filter((e) => !namedEvents.has(e));
  check(
    strayEvents.length === 0,
    'no surface renders an event the catalogue does not declare',
    `unknown events: ${strayEvents.join(', ') || 'none'}`,
  );
  check(
    unrouted.length === 0,
    'every declared event has a surface that renders it, so no subscriber pays for a frame nobody shows',
    `unrouted: ${unrouted.join(', ') || 'none'}`,
  );
  const notNamed = allowNames.filter((c) => !namedCommands.has(c)).sort();
  check(
    pres.sessionCommands.length === 2 && notNamed.join(',') === [...pres.sessionCommands].sort().join(','),
    'the commands no surface names are exactly the two the shell owns, and the split is declared',
    `not named=${notNamed.join(', ')} session=${[...pres.sessionCommands].sort().join(', ')}`,
  );
  check(
    pres.surfaces.every((s) => s.commands.length > 0),
    'no surface is a screen with no command it may issue',
    `commandless: ${pres.surfaces.filter((s) => !s.commands.length).map((s) => s.id).join(', ') || 'none'}`,
  );

  // ---- the state vocabulary

  const ANCHORS = ['command', 'error', 'event', 'origin', 'policyPointer'];
  const REQUIRED_STATES = [
    'loading', 'empty', 'unknown', 'stale', 'error', 'blocked', 'failOpenInForce', 'degradedSecret',
    'keystoreUnavailable', 'restoreRequired', 'dryRunPlan', 'unrepresentable', 'localExpanded',
    'partialImport', 'disabledPhase2', 'truncatedExport', 'integrityRejected', 'busy',
  ];
  check(
    pres.states.length >= 15 && new Set(stateIds).size === stateIds.length,
    'the state vocabulary is closed and every condition has one name',
    `${stateIds.length} states: ${stateIds.join(', ')}`,
  );
  const missingStates = REQUIRED_STATES.filter((id) => !stateIds.includes(id));
  check(
    missingStates.length === 0 && stateIds.length === REQUIRED_STATES.length,
    'the state vocabulary is exactly the conditions the other contracts hand to a screen',
    `missing=${missingStates.join(', ') || 'none'} extra=${stateIds.filter((id) => !REQUIRED_STATES.includes(id)).join(', ') || 'none'}`,
  );
  const unanchored = pres.states.filter((s) => ANCHORS.filter((k) => s[k] !== undefined).length !== 1).map((s) => s.id);
  check(
    unanchored.length === 0,
    'every state names exactly one anchor: a command, an error code, an event, a policy pointer or an origin',
    `wrong anchor count: ${unanchored.join(', ') || 'none'}`,
  );
  const vague = pres.states.filter((s) => (s.mustNot ?? '').trim().length < 20).map((s) => s.id);
  check(
    vague.length === 0,
    'every state says in a sentence what it must never claim',
    `too short: ${vague.join(', ') || 'none'}`,
  );
  const dangling = [];
  for (const s of pres.states) {
    if (s.command !== undefined && !commandDocs.has(s.command)) dangling.push(`${s.id} -> command ${s.command}`);
    if (s.error !== undefined && !taxonomy.codes.some((c) => c.code === s.error))
      dangling.push(`${s.id} -> error ${s.error}`);
    if (s.event !== undefined && !eventNames.has(s.event)) dangling.push(`${s.id} -> event ${s.event}`);
    if (s.policyPointer !== undefined) {
      const [file, pointer] = String(s.policyPointer).split('#');
      const value = existsSync(join(REPO, file)) ? at(await readJson(join(REPO, file)), pointer) : undefined;
      if (value === undefined) dangling.push(`${s.id} -> ${s.policyPointer}`);
    }
  }
  check(
    dangling.length === 0,
    'every state anchor resolves: no condition is anchored to a command, code, event or schema location that does not exist',
    `dangling: ${dangling.join(', ') || 'none'}`,
  );
  const stateUse = new Set(pres.surfaces.flatMap((s) => s.states));
  check(
    stateIds.every((id) => stateUse.has(id)) && [...stateUse].every((id) => stateIds.includes(id)),
    'no state is declared but rendered, and no surface renders an undeclared state',
    `unrendered=${stateIds.filter((id) => !stateUse.has(id)).join(', ') || 'none'} undeclared=${[...stateUse].filter((id) => !stateIds.includes(id)).join(', ') || 'none'}`,
  );

  // ---- the placement vocabulary the error taxonomy borrowed

  const placements = pres.errorPlacements.values;
  const oddPlacement = taxonomy.codes.filter((c) => !placements.includes(c.surface)).map((c) => `${c.code}=${c.surface}`);
  const unusedPlacement = placements.filter((p) => !taxonomy.codes.some((c) => c.surface === p));
  check(
    placements.length >= 5 && new Set(placements).size === placements.length && oddPlacement.length === 0,
    'every error code names a placement the screen implements, so no code has nowhere to appear',
    `outside the list: ${oddPlacement.join(', ') || 'none'}`,
  );
  check(
    unusedPlacement.length === 0,
    'every placement the UI declares is the placement of at least one code',
    `unused: ${unusedPlacement.join(', ') || 'none'}`,
  );

  // ---- themes, measured against the inherited catalogue

  const th = await readJson(join(REPO, 'omega-web', 'lib', 'themes', 'themes.json'));
  const themeKeys = Object.keys(th);
  const pal = pres.themes.paletteKeys;
  const sem = pres.themes.semanticTokens;
  const cat = pres.themes.catalogue;
  check(
    pal.length === 16 && sem.length === 15 && pal.filter((k) => sem.includes(k)).length === 0,
    'the token vocabulary is sixteen base16 keys and fifteen semantic properties, with no key in both',
    `palette=${pal.length} semantic=${sem.length} overlap=${pal.filter((k) => sem.includes(k)).join(', ') || 'none'}`,
  );
  const badPalette = themeKeys.filter((k) => {
    const colors = th[k]?.colors;
    if (!colors || typeof colors !== 'object') return true;
    const keys = Object.keys(colors);
    return keys.length !== pal.length || keys.some((c) => !pal.includes(c));
  });
  check(
    badPalette.length === 0 && themeKeys.length === cat.base16Count,
    'every theme in the inherited catalogue carries exactly the sixteen keys the pipeline reads',
    `${themeKeys.length} themes, wrong shape: ${badPalette.join(', ') || 'none'}`,
  );
  const missingCss = themeKeys.filter(
    (k) => !existsSync(join(REPO, 'omega-web', 'lib', 'themes', 'others', `base16-${k}.css`)),
  );
  check(
    missingCss.length === 0 && cat.stylesheetPath.includes('master.coffee:331'),
    'a catalogue key becomes a legacy stylesheet path exactly the way the controller says it does',
    `no stylesheet: ${missingCss.join(', ') || 'none'}`,
  );
  const darkFlagged = themeKeys.filter((k) => th[k]?.dark === true).length;
  const namesDiffer = themeKeys.filter((k) => th[k]?.name !== k).length;
  const orderPresent = themeKeys.filter((k) => th[k]?.order !== undefined).length;
  const measuredWhitespace = themeKeys
    .map((k) => [k, Object.values(th[k]?.colors ?? {}).filter((v) => v !== String(v).trim()).length])
    .filter(([, n]) => n > 0);
  const declaredWhitespace = pres.themes.measuredDefects.whitespaceInHex.map((d) => [d.theme, d.fields]);
  const whitespaceTotal = measuredWhitespace.reduce((sum, [, n]) => sum + n, 0);
  check(
    darkFlagged === cat.darkFlagged &&
      namesDiffer === pres.themes.measuredDefects.namesDifferFromKey &&
      orderPresent === pres.themes.measuredDefects.orderFieldPresentOn &&
      JSON.stringify(measuredWhitespace) === JSON.stringify(declaredWhitespace) &&
      whitespaceTotal === pres.themes.measuredDefects.totalValues,
    'the measured facts about the catalogue are the numbers on disk, re-measured rather than remembered',
    `dark=${darkFlagged}/${cat.darkFlagged} named=${namesDiffer}/${pres.themes.measuredDefects.namesDifferFromKey} ordered=${orderPresent}/${pres.themes.measuredDefects.orderFieldPresentOn} whitespace=${JSON.stringify(measuredWhitespace)} vs ${JSON.stringify(declaredWhitespace)} (${whitespaceTotal} vs ${pres.themes.measuredDefects.totalValues})`,
  );
  check(
    cat.selectableTotal === themeKeys.length + cat.defaults.length + 1 && cat.defaultsSameAsPalettes === false,
    'the selectable set is the catalogue plus the three named buttons plus the empty selection, and the buttons are not the dropdown rows they share a name with',
    `declared=${cat.selectableTotal} counted=${themeKeys.length}+${cat.defaults.length}+1`,
  );
  const buttonThemes = cat.defaults.filter(
    (name) => !existsSync(join(REPO, 'omega-web', 'lib', 'themes', `${name}.css`)),
  );
  check(
    buttonThemes.length === 0,
    'each of the three default themes is a stylesheet at the path the legacy buttons fetch',
    `missing: ${buttonThemes.join(', ') || 'none'}`,
  );
  const varCss = await readFile(join(REPO, 'omega-web', 'lib', 'themes', 'variable.css'), 'utf8');
  const sheetTokens = [...new Set([...varCss.matchAll(/--([A-Za-z0-9-]+)\s*:/g)].map((m) => m[1]))];
  const allTokens = [...pal, ...sem];
  check(
    allTokens.length === 31 &&
      new Set(allTokens).size === 31 &&
      sheetTokens.length === 31 &&
      allTokens.every((t) => sheetTokens.includes(t)),
    'the inherited variable sheet is the token list: the same thirty-one custom properties, none extra and none missing',
    `sheet=${sheetTokens.length} declared=${allTokens.length} absent from sheet=${allTokens.filter((t) => !sheetTokens.includes(t)).join(', ') || 'none'}`,
  );
  const autoCss = await readFile(join(REPO, 'omega-web', 'lib', 'themes', 'default-auto.css'), 'utf8');
  check(
    autoCss.includes('prefers-color-scheme') && pres.themes.pipeline.auto.includes('prefers-color-scheme'),
    'Auto is a media query over the same sixteen properties, so light and dark are parity rather than invention',
    `sheet has the query=${autoCss.includes('prefers-color-scheme')}`,
  );
  check(
    pres.themes.pipeline.mustNot.length >= 4 &&
      pres.themes.legacyCoexistence.length >= 40 &&
      pres.themes.pipeline.reads.includes('themes.json'),
    'the pipeline reads the catalogue and says what it must not fetch',
    `mustNot=${pres.themes.pipeline.mustNot.length}`,
  );

  // ---- i18n, measured against the inherited .po tree and the build task

  const poTask = await readFile(
    join(REPO, 'omega-target-chromium-extension', 'grunt', 'po2crx.coffee'),
    'utf8',
  );
  const compiled = [...poTask.matchAll(/_locales\/([A-Za-z0-9_]+)\/messages\.json':\s*'\.\.\/(omega-locales\/[^']+\.po)'/g)]
    .map((m) => [m[1], m[2]]);
  const poInputs = [...new Set(compiled.map(([, input]) => input))];
  check(
    compiled.length === pres.i18n.shippedTargetCount &&
      compiled.map(([target]) => target).join(',') === pres.i18n.shippedTargets.join(',') &&
      poInputs.length === pres.i18n.distinctPoInputs,
    'the shipped locale set is the build task list, read from the task rather than from a document',
    `targets=${compiled.map(([t]) => t).join(', ') || 'none'} declared=${pres.i18n.shippedTargets.join(', ')} inputs=${poInputs.length}/${pres.i18n.distinctPoInputs}`,
  );
  const localeDirs = (await readdir(join(REPO, 'omega-locales'))).filter((d) =>
    existsSync(join(REPO, 'omega-locales', d, 'LC_MESSAGES', 'omega-web.po')),
  );
  check(
    localeDirs.length === pres.i18n.localeSources &&
      localeDirs.length - poInputs.length === pres.i18n.unsupplied &&
      pres.i18n.unsuppliedBasis.includes('27'),
    'the locale source count and the unsupplied remainder are directory counts, and the basis names the arithmetic',
    `dirs=${localeDirs.length} declared=${pres.i18n.localeSources} inputs=${poInputs.length} unsupplied=${pres.i18n.unsupplied}`,
  );
  const enPo = await readFile(
    join(REPO, 'omega-locales', 'en_US', 'LC_MESSAGES', 'omega-web.po'),
    'utf8',
  );
  const msgids = (enPo.match(/^msgid /gm) || []).length;
  check(
    msgids === pres.i18n.enMsgids,
    'the English source carries the number of msgids a compiled catalog has to cover',
    `msgid lines=${msgids} declared=${pres.i18n.enMsgids}`,
  );
  const jadeFiles = [];
  for await (const file of walk(join(REPO, 'omega-web', 'src'))) if (file.endsWith('.jade')) jadeFiles.push(file);
  let trUses = 0;
  let literalLines = 0;
  const templateText = new Map();
  for (const file of jadeFiles) {
    const text = await readFile(file, 'utf8');
    templateText.set(relative(REPO, file).split(sep).join('/'), text);
    trUses += (text.match(/\|\s*tr\}\}/g) || []).length;
    literalLines += text.split('\n').filter((l) => /\{\{'/.test(l) && !/\|\s*tr\b/.test(l)).length;
  }
  check(
    trUses === pres.i18n.literalDebt.trUsesInTemplates &&
      literalLines === pres.i18n.literalDebt.hardcodedAngularLiterals,
    'the translated-literal debt is a recount over the inherited templates, so parity work carries a number',
    `tr=${trUses}/${pres.i18n.literalDebt.trUsesInTemplates} hardcoded=${literalLines}/${pres.i18n.literalDebt.hardcodedAngularLiterals}`,
  );
  const badExample = [];
  for (const cite of pres.i18n.literalDebt.namedExamples) {
    const [file, line] = String(cite).split(':');
    const text = templateText.get(file);
    if (text === undefined) badExample.push(`${cite}: no such template`);
    else {
      const l = text.split('\n')[Number(line) - 1] ?? '';
      if (!/\{\{'/.test(l) || /\|\s*tr\b/.test(l)) badExample.push(`${cite}: line does not carry a hardcoded literal`);
    }
  }
  check(
    badExample.length === 0,
    'every named example of hardcoded copy is a line that actually carries one',
    `bad: ${badExample.join(', ') || 'none'}`,
  );
  const po2crxImpl = await readFile(join(REPO, 'omega-target-chromium-extension', 'grunt-po2crx.coffee'), 'utf8');
  const implLines = po2crxImpl.split('\n');
  const matchLine = implLines[12] ?? '';
  const emitLine = implLines[25] ?? '';
  check(
    matchLine.includes('message.replace') &&
      matchLine.includes('(\\d+:)') &&
      matchLine.includes('(\\w+)') &&
      emitLine.includes("'$' + ref + '$'") &&
      /\$\d+:[A-Z_]+\$/.test(enPo) &&
      pres.i18n.placeholders.poSyntax.startsWith('$') &&
      pres.i18n.placeholders.crxSyntax.startsWith('$name') &&
      pres.i18n.placeholders.rewrittenBy.endsWith(':13') &&
      pres.i18n.placeholders.emittedOn.endsWith(':26'),
    'the po placeholder pattern is read on the line the contract names and the crx form is emitted on the line it names',
    `13: ${matchLine.trim()} | 26: ${emitLine.trim()}`,
  );
  check(
    pres.i18n.lookup.mustNot.length >= 3 && pres.i18n.namesAreData.length >= 40 && pres.i18n.supportTable.includes('localeSupport'),
    'the catalog is local, profile names stay data, and the support table is the place an unfinished locale is stated',
    `lookup mustNot=${pres.i18n.lookup.mustNot.length}`,
  );

  // ---- the popup, measured against the legacy popup and the retry ladder

  const keyboard = await readFile(
    join(REPO, 'omega-web', 'src', 'popup', 'js', 'keyboard.js'),
    'utf8',
  );
  const missingActions = pres.popup.keyboardMap.namedActions.filter((a) => !keyboard.includes(`'${a}'`));
  check(
    missingActions.length === 0 && pres.popup.keyboardMap.namedActions.length >= 6,
    'the keyboard map the contract inherits is in the legacy popup today, action by action',
    `absent: ${missingActions.join(', ') || 'none'}`,
  );
  check(
    new RegExp(`for \\(i = 1; i <= ${pres.popup.keyboardMap.digitSlots}; i\\+\\+\\)`).test(keyboard) &&
      keyboard.includes("'js-profile-'"),
    'the digit slots are a loop over that many keys, not a number in a document',
    `slots=${pres.popup.keyboardMap.digitSlots}`,
  );
  const rpcSource = await readFile(
    join(REPO, 'omega-target-chromium-extension', 'src', 'coffee', 'omega_target_web.coffee'),
    'utf8',
  );
  const constOf = (name) => Number(rpcSource.match(new RegExp(`${name} = (\\d+)`))?.[1]);
  const maxAttempts = constOf('maxAttempts');
  const maxWorkerDown = constOf('maxWorkerDownAttempts');
  let ladderMs = 0;
  for (let attempt = 1; attempt < maxWorkerDown; attempt++)
    ladderMs += attempt > maxAttempts ? 2000 * 2 ** (attempt - maxAttempts - 1) : 100 * attempt;
  check(
    maxAttempts === pres.popup.coldStart.maxAttempts &&
      maxWorkerDown === pres.popup.coldStart.maxWorkerDownAttempts &&
      ladderMs === pres.popup.coldStart.ladderTotalMs &&
      rpcSource.includes(pres.popup.coldStart.errorMatch) &&
      pres.budgets.coldWorkerCeilingMs.value === ladderMs,
    'the cold-start ladder is recomputed from the legacy constants, and the budget ceiling is that sum',
    `recomputed=${ladderMs}ms declared=${pres.popup.coldStart.ladderTotalMs}ms ceiling=${pres.budgets.coldWorkerCeilingMs.value}ms`,
  );
  check(
    pres.popup.coldStart.rule.includes('verbatim') && pres.popup.coldStart.why.length >= 40,
    'the ladder is ported verbatim and the reason it is a contract is stated',
    pres.popup.coldStart.rule.slice(0, 60),
  );
  const statePages = pres.popup.statesPages.legacy;
  check(
    statePages.length === 2 && statePages.every((f) => existsSync(join(REPO, f))),
    'the two whole-document popup states are documents that exist, because they must render without a Connector',
    `missing: ${statePages.filter((f) => !existsSync(join(REPO, f))).join(', ') || 'none'}`,
  );
  const iframeJs = await readFile(
    join(REPO, 'omega-target-chromium-extension', 'overlay', 'popup-iframe.js'),
    'utf8',
  );
  check(
    pres.popup.geometry.includes('popup-iframe.js') && /height/.test(iframeJs),
    'the popup auto-resize obligation points at the wrapper that actually sets a height',
    `geometry cites the wrapper=${pres.popup.geometry.includes('popup-iframe.js')}`,
  );

  // ---- controls, derived from the command documents rather than remembered

  const argKeys = (doc) => {
    const out = new Set();
    const visit = (node) => {
      if (node === null || typeof node !== 'object') return;
      if (!Array.isArray(node) && node.properties && typeof node.properties === 'object')
        Object.keys(node.properties).forEach((k) => out.add(k));
      Object.values(node).forEach(visit);
    };
    visit(doc);
    return out;
  };
  const derivedConfirmation = allowNames
    .filter((name) => {
      const keys = argKeys(commandDocs.get(name).doc.args ?? {});
      return keys.has('confirm') || keys.has('confirmToken');
    })
    .sort();
  const derivedDryRun = allowNames
    .filter((name) => argKeys(commandDocs.get(name).doc.args ?? {}).has('dryRun'))
    .sort();
  check(
    [...pres.controls.confirmationRequired].sort().join(',') === derivedConfirmation.join(','),
    'the confirmation set is derived: exactly the commands that declare a confirm argument',
    `derived=${derivedConfirmation.join(', ')} declared=${[...pres.controls.confirmationRequired].sort().join(', ')}`,
  );
  check(
    [...pres.controls.dryRunAvailable].sort().join(',') === derivedDryRun.join(','),
    'the dryRun set is derived: exactly the commands that declare a dryRun argument',
    `derived=${derivedDryRun.join(', ')} declared=${[...pres.controls.dryRunAvailable].sort().join(', ')}`,
  );
  const badNotShipable = [];
  for (const item of pres.controls.notShipable)
    if (!(await evidenceOk(item.evidence)) || (item.reason ?? '').length < 40) badNotShipable.push(item.control);
  check(
    pres.controls.notShipable.length >= 6 && badNotShipable.length === 0,
    'every control that will not ship names evidence that resolves and a reason long enough to review',
    `weak rows: ${badNotShipable.join(', ') || 'none'}`,
  );
  const pins = await readJson(join(PKG, 'pins.schema.json'));
  check(
    pins.properties.silentSelfUpdate.const === false,
    'the silent-update entry on the not-shipable list is a schema const, not an opinion',
    `const=${pins.properties.silentSelfUpdate.const}`,
  );
  check(
    pres.controls.rule.includes('disabled') && pres.controls.destructiveCopyRule.includes('held'),
    'a control with no backend renders disabled with a reason, and a hold is never copy-ed as a loss',
    `${pres.controls.rule.slice(0, 40)} / ${pres.controls.destructiveCopyRule.slice(0, 40)}`,
  );

  // ---- budgets, parity rows and prohibitions

  const budgetKeys = Object.keys(pres.budgets).filter((k) => typeof pres.budgets[k] === 'object');
  check(
    budgetKeys.length >= 4 && budgetKeys.every((k) => (pres.budgets[k].basis ?? '').length >= 20) &&
      budgetKeys.every((k) => Number(pres.budgets[k].value) > 0),
    'every budget is a number with a stated basis, and none of them is a promise in UI copy',
    `${budgetKeys.map((k) => `${k}=${pres.budgets[k].value}`).join(', ')}`,
  );
  const rows = pres.parity.rows;
  const rowKeys = rows.map((r) => `${r.legacy}|${r.surface}|${r.obligation}`);
  const badRows = rows.filter(
    (r) => !surfaceIds.includes(r.surface) && !docIds.includes(r.surface),
  );
  const badRowEvidence = [];
  for (const r of rows) if (!(await evidenceOk(r.evidence))) badRowEvidence.push(r.evidence);
  check(
    rows.length >= 15 && new Set(rowKeys).size === rowKeys.length,
    'the parity matrix is a closed list of obligations, each named once',
    `${rows.length} rows, ${new Set(rowKeys).size} distinct`,
  );
  check(
    badRows.length === 0,
    'every parity row names a surface or a document this contract declares',
    `unknown: ${badRows.map((r) => r.surface).join(', ') || 'none'}`,
  );
  check(
    badRowEvidence.length === 0,
    'every parity row points at a legacy file that is still in the repository',
    `missing: ${badRowEvidence.join(', ') || 'none'}`,
  );
  check(
    rows.every((r) => (r.obligation ?? '').length >= 30 && pres.parity.statusValues.includes(r.status) && r.status !== 'done'),
    'no parity row is closed in this iteration, and every row states an obligation long enough to test',
    `statuses=${[...new Set(rows.map((r) => r.status))].join(', ')} short=${rows.filter((r) => (r.obligation ?? '').length < 30).length}`,
  );
  check(
    pres.parity.gate.includes('popup') && pres.parity.gate.includes('ADR-0016') && pres.parity.statusValues.length === 3,
    'the gate says which surface flips last and which ADR says so, with a three-value status vocabulary',
    pres.parity.gate.slice(0, 60),
  );
  const badForbidden = [];
  for (const f of pres.forbidden) if (!(await evidenceOk(f.evidence)) || (f.reason ?? '').length < 40) badForbidden.push(f.rule);
  check(
    pres.forbidden.length >= 12 && badForbidden.length === 0,
    'every UI prohibition cites a contract that exists and says why it is a prohibition',
    `weak rows: ${badForbidden.join(', ') || 'none'}`,
  );
  const forbiddenText = pres.forbidden.map((f) => f.rule).join(' | ');
  const uncovered = [
    ['Happ', 'the brief forbids copying the reference interface'],
    ['backend', 'constraint 11: a control implies a feature'],
    ['locale', 'ADR-0017 rule 4: no promise of an unreleased language'],
    ['RPC', 'ADR-0017 rule 3: no label fetched from the background'],
    ['shadow', 'ADR-0016 rules 4 and 5: encapsulation is structural'],
  ].filter(([needle]) => !forbiddenText.includes(needle));
  check(
    uncovered.length === 0,
    'the five prohibitions that come from the brief and the UI ADRs are all in the list',
    `missing: ${uncovered.map(([, what]) => what).join(', ') || 'none'}`,
  );
  check(
    pres.scope.owns.length >= 8 &&
      pres.scope.doesNotOwn.length >= 8 &&
      pres.scope.owns.every((o) => o.length >= 20) &&
      pres.scope.doesNotOwn.every((o) => o.length >= 20),
    'the UI contract says what it owns and what it refuses, in sentences that name the owner instead',
    `owns=${pres.scope.owns.length} doesNotOwn=${pres.scope.doesNotOwn.length}`,
  );
  check(
    pres.openItems.length >= 3 && pres.openItems.every((o) => String(o).length >= 60) && pres.description.length >= 400,
    'the description carries the cross-read contracts and the open items stay open instead of being decided by a control',
    `openItems=${pres.openItems.length} description=${pres.description.length} chars`,
  );
  // The data-document family: presentation.json and compatibility.json are lists whose argument lives in docs/architecture.
  // A list that is not named in the package README is a list a contributor will duplicate.
  const dataDocsHere = [
    'migrations.json',
    'protocol/data-ownership.json',
    'observability.json',
    'presentation.json',
    'compatibility.json',
  ];
  const schemaReadme = await readFile(join(PKG, 'README.md'), 'utf8');
  const unlisted = dataDocsHere.filter((f) => !schemaReadme.includes(f));
  check(
    dataDocsHere.every((f) => existsSync(join(PKG, f))) && unlisted.length === 0,
    'every data document the package holds is a real file and is named in the package README',
    `unlisted: ${unlisted.join(', ') || 'none'}`,
  );
  const thinDescription = [];
  for (const f of dataDocsHere) {
    const doc = f === 'presentation.json' ? pres : await readJson(join(PKG, f));
    if (typeof doc.description !== 'string' || doc.description.length < 80) thinDescription.push(f);
  }
  check(
    thinDescription.length === 0,
    'every data document says in its own description why it exists as data',
    `thin or missing: ${thinDescription.join(', ') || 'none'}`,
  );

  // ---- the argument, when it exists

  const uiDoc = join(REPO, 'docs', 'architecture', '37-ui-architecture.md');
  if (existsSync(uiDoc)) {
    const text = await readFile(uiDoc, 'utf8');
    const unnamed = [
      ...surfaceIds.filter((id) => !text.includes(id)),
      ...stateIds.filter((id) => !text.includes(id)),
      ...docIds.filter((id) => !text.includes(id)),
      ...pres.errorPlacements.values.filter((v) => !text.includes(v)),
      ...pres.themes.paletteKeys.filter((k) => !text.includes(k)),
      ...pres.i18n.shippedTargets.filter((t) => !text.includes(t)),
    ];
    check(
      unnamed.length === 0 && text.includes('presentation.json'),
      'the user-interface document names every surface, state, document, placement, token and locale the data declares',
      `absent from doc 37: ${unnamed.join(', ') || 'none'}`,
    );
    const contractsDoc = await readFile(join(REPO, 'docs', 'architecture', '22-component-contracts.md'), 'utf8');
    const cite3 = contractsDoc.includes('37-ui-architecture.md` §3');
    const cite7 = contractsDoc.includes('37-ui-architecture.md` §7');
    const staleI18nCite = contractsDoc.includes('§i18n');
    check(
      cite3 && cite7 && !staleI18nCite,
      'the component contracts cite the numbered sections that own the UI message boundary and the compiled catalog',
      'doc 22 must point at §3 for the typed UI message and §7 for the catalog',
    );
    const layoutDoc = await readFile(join(REPO, 'docs', 'architecture', '20-target-repository-layout.md'), 'utf8');
    const htmlDocs = docIds.filter((d) => d.endsWith('.html'));
    check(
      htmlDocs.length === 2 && htmlDocs.every((d) => layoutDoc.includes(d)),
      'the repository layout names the same NEXORA documents the UI contract says the surfaces live in',
      `not named in doc 20: ${htmlDocs.filter((d) => !layoutDoc.includes(d)).join(', ') || 'none'}`,
    );
  } else {
    note('DEFERRED: 37-ui-architecture.md cross-check (file not present yet)');
  }
}

// ---------------------------------------------------------------- the matrix as a contract
//
// Nine documents send a compatibility promise here, and every one of them asked for a number the document
// itself could not produce: a browser floor, a cohort count, an envelope size. `packages/schema/compatibility.json`
// answers by refusing to guess. Each row names the line in the inherited tree that makes the promise about
// something real, and each number below is re-read from that tree on every run -- the two manifests, the
// ten-name profile-type registry, the eleven `-` option keys, the browser-API surface including the APIs the
// legacy code never calls, the two sync backends and the two destructive legacy paths. The half this repository
// cannot measure is stated as owed (COMP-07, COMP-10, COMP-14, COMP-18), because a compatibility claim whose number lives
// in a browser is not a claim yet, and docs/PROJECT-STATE.md §0 constraint 11 forbids shipping a control that
// depends on it.

{
  const comp = await readJson(join(PKG, 'compatibility.json'));
  const manifestC = await readJson(join(REPO, 'omega-target-chromium-extension', 'overlay', 'manifest.json'));
  const manifestF = await readJson(join(REPO, 'omega-target-chromium-extension', 'overlay', 'manifest-firefox.json'));

  const legacyLines = {};
  const linesOf = async (rel) => {
    if (legacyLines[rel] === undefined) {
      legacyLines[rel] = existsSync(join(REPO, rel)) ? (await readFile(join(REPO, rel), 'utf8')).split(/\r?\n/) : [];
    }
    return legacyLines[rel];
  };
  const lineSays = async (cite, needle) => {
    const [rel, range] = String(cite).split(':');
    const [from, to] = range.split('-').map(Number);
    const text = (await linesOf(rel)).slice(from - 1, to ?? from).join('\n');
    return text.includes(needle);
  };
  // A matrix citation is either a line in the inherited tree, a pointer into a JSON contract, a
  // doc section, or a bare file name. Each of the four has a different way to go stale, so each is
  // resolved differently; none of them is allowed to simply name something that is not there.
  const at = (doc, pointer) =>
    String(pointer ?? '')
      .split('/')
      .filter(Boolean)
      .reduce((v, seg) => (v == null ? undefined : v[seg.replace(/~1/g, '/').replace(/~0/g, '~')]), doc);
  const jsonCache = {};
  const evidenceResolves = async (text) => {
    const head = String(text).trim().split(/\s+/)[0];
    const [loc, pointer] = head.split('#');
    const range = loc.match(/^(.+?):(\d+)(?:-(\d+))?$/);
    const file = range ? range[1] : loc;
    const abs = existsSync(join(REPO, file)) ? join(REPO, file) : existsSync(join(PKG, file)) ? join(PKG, file) : null;
    if (abs === null) return false;
    if (pointer !== undefined) {
      if (!file.endsWith('.json')) return true;
      jsonCache[abs] ??= JSON.parse(await readFile(abs, 'utf8'));
      return at(jsonCache[abs], pointer) !== undefined;
    }
    if (range === null) return true;
    const lines = await linesOf(file);
    const from = Number(range[2]);
    const to = Number(range[3] ?? range[2]);
    return lines.slice(from - 1, to).some((l) => l.trim() !== '');
  };

  // ---- the corpus the support claims are measured against

  const corpusDirs = comp.apiSurface.corpus;
  const missingCorpus = corpusDirs.filter((d) => !existsSync(join(REPO, d)));
  check(
    corpusDirs.length === 5 && missingCorpus.length === 0,
    'the corpus directories are the inherited source trees and all of them exist',
    `missing: ${missingCorpus.join(', ') || 'none'}`,
  );
  const corpusText = [];
  for (const dir of corpusDirs) {
    if (!existsSync(join(REPO, dir))) continue;
    for await (const file of walk(join(REPO, dir))) {
      if (/node_modules[\\/]/.test(file)) continue;
      if (/\.(min\.js|map|lock)$/i.test(file)) continue;
      if (!/\.(coffee|js|html|jade)$/i.test(file)) continue;
      corpusText.push(await readFile(file, 'utf8'));
    }
  }
  const corpus = corpusText.join('\n');
  check(
    corpus.length > 200000,
    'the corpus is a real read of the inherited source, not an empty string that proves nothing',
    `${corpus.length} bytes from ${corpusDirs.length} trees`,
  );
  const stillThere = comp.apiSurface.absent.filter((api) => corpus.includes(api));
  check(
    comp.apiSurface.absent.length >= 15 && stillThere.length === 0,
    'no API the file lists as absent appears in the inherited corpus',
    `now present: ${stillThere.join(', ') || 'none'}`,
  );
  const goneApi = [];
  for (const u of comp.apiSurface.used) {
    const [rel, line] = u.cite.split(':');
    const text = (await linesOf(rel))[Number(line) - 1] ?? '';
    const leaf = u.api.split('.').pop();
    if (!text.includes(leaf)) goneApi.push(`${u.api} -> ${u.cite} reads "${text.trim()}"`);
  }
  check(
    goneApi.length === 0,
    'every API the file says the legacy code calls is on the line it names',
    goneApi.join(' | ') || `${comp.apiSurface.used.length} calls, each cited`,
  );
  const unguarded = comp.apiSurface.unguarded ?? [];
  const unguardedLive = [];
  for (const item of unguarded) {
    for (const cite of item.cites) if (!(await lineSays(cite, item.api.split('.').pop()))) unguardedLive.push(cite);
  }
  check(
    unguarded.length === 1 && unguarded[0].floor === null && unguardedLive.length === 0,
    'an unmeasured floor is named as unmeasured, and the lines it hangs on still call the API',
    `unguarded=${unguarded.map((u) => u.api).join(', ')} stale=${unguardedLive.join(', ') || 'none'}`,
  );
  const modesWritten = [];
  for (const mode of comp.apiSurface.proxyModesUsed) {
    if (!(await linesOf('omega-target-chromium-extension/src/module/proxy/proxy_impl_settings.coffee'))
      .some((l) => l.includes(`config['mode'] = '${mode}'`))) modesWritten.push(mode);
  }
  check(
    comp.apiSurface.proxyModesUsed.length === 3 && modesWritten.length === 0 &&
      !corpus.includes("mode'] = 'system'"),
    'the proxy modes the file lists are the strings the settings backend actually writes',
    `absent from the backend: ${modesWritten.join(', ') || 'none'}`,
  );
  check(
    (await lineSays('omega-target-chromium-extension/src/module/proxy/proxy_auth.coffee:16', 'if not chrome.webRequest.onAuthRequired')) &&
      (await lineSays('omega-target-chromium-extension/src/module/storage.coffee:39', 'browser?.storage?[@areaName]')),
    'a guard for one optional API exists in the same tree that lacks one for another',
    'proxy_auth.coffee:16 and storage.coffee:39 are the two feature-detects',
  );

  // ---- the two manifests

  check(
    manifestC.minimum_chrome_version === comp.browserFloors.minimumChromeVersion &&
      manifestF.minimum_chrome_version === comp.browserFloors.minimumChromeVersion &&
      manifestF.browser_specific_settings?.gecko?.strict_min_version === comp.browserFloors.geckoStrictMinVersion &&
      manifestF.browser_specific_settings?.gecko?.id === comp.browserFloors.geckoId &&
      manifestC.manifest_version === comp.browserFloors.manifestVersion &&
      manifestC.version === comp.browserFloors.extensionVersion &&
      manifestC.default_locale === 'en' && manifestF.default_locale === 'en',
    'the two inherited manifests carry the floor and permission lists this file claims',
    `chromium=${manifestC.minimum_chrome_version} firefox=${manifestF.minimum_chrome_version}/${manifestF.browser_specific_settings?.gecko?.strict_min_version}`,
  );
  const permOk = (file, list) => JSON.stringify(file.permissions) === JSON.stringify(list);
  const onlyC = manifestC.permissions.filter((p) => !manifestF.permissions.includes(p));
  const onlyF = manifestF.permissions.filter((p) => !manifestC.permissions.includes(p));
  check(
    permOk(manifestC, comp.manifestDeltas.permissions.chromium) &&
      permOk(manifestF, comp.manifestDeltas.permissions.firefox) &&
      JSON.stringify(onlyC) === JSON.stringify(comp.manifestDeltas.permissions.onlyChromium) &&
      JSON.stringify(onlyF) === JSON.stringify(comp.manifestDeltas.permissions.onlyFirefox) &&
      JSON.stringify(manifestC.host_permissions) === JSON.stringify(comp.manifestDeltas.permissions.hostPermissions),
    'the permission lists are the manifests, in order, including the one-sided pair',
    `only-chromium=${onlyC.join(',')} only-firefox=${onlyF.join(',')}`,
  );
  check(
    !manifestC.permissions.includes('nativeMessaging') && !manifestF.permissions.includes('nativeMessaging') &&
      comp.manifestDeltas.nexoraAdds.length === 1 && comp.manifestDeltas.nexoraAdds[0].permission === 'nativeMessaging',
    'nativeMessaging is absent from both inherited manifests, which is what makes ADR-0004 a change and not a description',
    `chromium=${manifestC.permissions.length} firefox=${manifestF.permissions.length} added=${comp.manifestDeltas.nexoraAdds.map((a) => a.permission).join(',')}`,
  );
  const fieldOf = (file, dotted) => dotted.split('.').reduce((v, k) => (v == null ? undefined : v[k]), file);
  const divergentBad = [];
  for (const d of comp.manifestDeltas.divergentFields) {
    const asString = (x) => (x == null ? null : typeof x === 'object' ? Object.keys(x)[0] ?? 'object' : String(x));
    const side = (value, actual) => {
      if (value === null) return actual === undefined;
      const want = String(value).split(' ')[0];
      return asString(actual) === want || (Array.isArray(actual) && actual.length > 0);
    };
    if (!side(d.chromium, fieldOf(manifestC, d.field)) || !side(d.firefox, fieldOf(manifestF, d.field))) {
      divergentBad.push(`${d.field}: chromium=${asString(fieldOf(manifestC, d.field)) ?? 'absent'} firefox=${asString(fieldOf(manifestF, d.field)) ?? 'absent'}`);
    }
  }
  check(
    comp.manifestDeltas.divergentFields.length === 4 && divergentBad.length === 0,
    'the divergent fields are divergent in the files and not only in this list',
    divergentBad.join(' | ') || 'popup path, background spelling, options_page, browser_specific_settings',
  );

  // ---- the shape of the user's data

  const profilesRel = 'omega-pac/src/profiles.coffee';
  const profilesLines = await linesOf(profilesRel);
  const registryStart = profilesLines.findIndex((l) => l.startsWith('  _profileTypes:'));
  const entries = [];
  for (let i = registryStart + 1; i < profilesLines.length && registryStart >= 0; i++) {
    if (/^  \S/.test(profilesLines[i])) break;
    const m = profilesLines[i].match(/^    '([A-Za-z]+)':\s*(.*)$/);
    if (m) entries.push({ name: m[1], rest: m[2].trim().replace(/,$/, ''), line: i + 1 });
  }
  const measuredTypes = entries.filter((e) => !/^'[A-Za-z]+'$/.test(e.rest)).map((e) => e.name);
  const measuredAliases = Object.fromEntries(
    entries.filter((e) => /^'[A-Za-z]+'$/.test(e.rest)).map((e) => [e.name, e.rest.replace(/'/g, '')]),
  );
  const dataTypes = comp.profileTypes.types.map((t) => t.name);
  const dataAliases = Object.fromEntries(comp.profileTypes.aliases.map((a) => [a.name, a.target]));
  check(
    JSON.stringify(measuredTypes.slice().sort()) === JSON.stringify(dataTypes.slice().sort()) &&
      measuredTypes.length === 6,
    'the profile-type registry has six types and four aliases, and this file names them',
    `measured=${measuredTypes.join(',')} declared=${dataTypes.join(',')}`,
  );
  check(
    JSON.stringify(Object.keys(measuredAliases).sort()) === JSON.stringify(Object.keys(dataAliases).sort()) &&
      Object.entries(dataAliases).every(([k, v]) => measuredAliases[k] === v),
    'every alias resolves to the type this file says it resolves to',
    `measured=${JSON.stringify(measuredAliases)} declared=${JSON.stringify(dataAliases)}`,
  );
  const aliasTargetsMissing = comp.profileTypes.aliases.filter((a) => !dataTypes.includes(a.target)).map((a) => a.name);
  check(
    aliasTargetsMissing.length === 0,
    'no alias points at a type this file does not declare, so the walk in _handler cannot land on a name NEXORA never reads',
    `dangling: ${aliasTargetsMissing.join(', ') || 'none'}`,
  );
  check(
    (await lineSays(`${profilesRel}:250`, 'Unknown profile type')) &&
      (await lineSays(`${profilesRel}:247`, "typeof handler == 'string'")),
    'an unknown profile type throws, so a reader that ignores the aliases is wrong loudly rather than silently',
    'profiles.coffee:247 walks and :250 throws',
  );
  const builtinKeys = Object.keys(
    (() => {
      const out = {};
      const start = profilesLines.findIndex((l) => l.startsWith('  builtinProfiles:'));
      for (let i = start + 1; i < profilesLines.length; i++) {
        if (/^  \S/.test(profilesLines[i])) break;
        const m = profilesLines[i].match(/^    '(\+?[A-Za-z_]+)':/);
        if (m) out[m[1]] = true;
      }
      return out;
    })(),
  );
  check(
    JSON.stringify(builtinKeys.sort()) === JSON.stringify(comp.profileTypes.builtinProfiles.slice().sort()),
    'the built-in profiles are the two the file names, and they come out of the code rather than out of storage',
    `measured=${builtinKeys.join(',')} declared=${comp.profileTypes.builtinProfiles.join(',')}`,
  );
  check(
    (await lineSays(`${profilesRel}:96`, "'+' + profileName")) &&
      (await lineSays(`${profilesRel}:113`, "key.charCodeAt(0) == charCodePlus")) &&
      (await lineSays('omega-target/src/options.coffee:92', "key[0] == '+'")) &&
      comp.profileTypes.keyConvention.prefix === '+' &&
      comp.profileTypes.builtinProfiles.every((k) => k.startsWith('+')),
    'the profile namespace is a first-character test, and NEXORA adds no prefix',
    'profiles.coffee:96 builds the key, :113 iterates it, options.coffee:92 dispatches on it',
  );

  const defaultRel = 'omega-target/src/default_options.coffee';
  const defaultLines = await linesOf(defaultRel);
  const measuredScalars = [];
  let measuredVersion = null;
  for (const line of defaultLines.slice(0, 30)) {
    const key = line.match(/^  "?(-[A-Za-z]+)"?:/);
    if (key) measuredScalars.push(key[1]);
    const version = line.match(/^  schemaVersion: (\d+)/);
    if (version) measuredVersion = Number(version[1]);
  }
  check(
    JSON.stringify(measuredScalars.sort()) === JSON.stringify(comp.optionKeys.scalarKeys.slice().sort()) &&
      measuredScalars.length === 11,
    'the scalar option keys are the ones the default set writes',
    `measured=${measuredScalars.length} declared=${comp.optionKeys.scalarKeys.length} diff=${[...measuredScalars, ...comp.optionKeys.scalarKeys].filter((k, i, a) => a.indexOf(k) === a.lastIndexOf(k)).join(', ') || 'none'}`,
  );
  check(
    measuredVersion === comp.optionKeys.schemaVersionValue &&
      comp.optionKeys.acceptedVersions.length === 2 && comp.optionKeys.acceptedVersions.includes(1) &&
      comp.optionKeys.acceptedVersions.includes(2) &&
      (await lineSays('omega-target/src/options.coffee:344', "'schemaVersion'] = options['schemaVersion'] = 2")) &&
      (await lineSays('omega-target/src/options.coffee:349', 'if version == 2')) &&
      (await lineSays('omega-target/src/options.coffee:353', 'Invalid schemaVersion')),
    'the schema version the legacy engine writes is the value this file freezes, and nothing else is accepted',
    `default=${measuredVersion} accepted=${comp.optionKeys.acceptedVersions.join(',')} reject at options.coffee:353`,
  );
  check(
    comp.optionKeys.namespaces.map((n) => n.prefix).join(',') === '+,-,omega.local.,bare' &&
      (await lineSays('omega-target/src/options.coffee:90', "key is '-customCss'")) &&
      (await lineSays('omega-target/src/options.coffee:464', "key is '-builtinProfiles'")) &&
      (await lineSays('omega-target-chromium-extension/src/coffee/background.coffee:234', "'omega.local.'")),
    'the four namespaces a top-level key may live in are named, and the two bare special cases are where the file says',
    `namespaces=${comp.optionKeys.namespaces.map((n) => n.prefix).join(', ')}`,
  );

  // ---- the portable copy of it

  check(
    (await lineSays('omega-web/src/omega/controllers/io.coffee:69', '$rootScope.options')) &&
      (await lineSays('omega-web/src/omega/controllers/io.coffee:72', 'ZeroOmegaOptions-')) &&
      (await lineSays('omega-target/src/options.coffee:360', 'parseOptions: (options)')) &&
      (await lineSays('omega-target/src/options.coffee:362', "options[0] != '{'")) &&
      (await lineSays('omega-target/src/options.coffee:365', "'base64'")) &&
      (await lineSays('omega-target/src/options.coffee:370', "Invalid options!")),
    'a backup has no wrapper: the exported object is the options map, in JSON or in base64',
    'io.coffee:69 and :72 write it, options.coffee:360-370 reads it',
  );
  check(
    (await lineSays('omega-target-chromium-extension/src/coffee/background.coffee:42', "compareVersions.compare '3.3.0'")) &&
      (await lineSays('omega-target-chromium-extension/src/coffee/background.coffee:44', 'chrome.storage.sync.clear()')) &&
      (await lineSays('omega-target-chromium-extension/src/coffee/background.coffee:45', 'chrome.storage.local.clear()')) &&
      (await lineSays('omega-target-chromium-extension/src/coffee/background.coffee:46', 'idbKeyval.clear()')),
    'the legacy update path clears three stores, and this file names it so no cut-over inherits it',
    'background.coffee:42-46 is the version wipe ADR-0014 and constraint 12 forbid',
  );
  check(
    (await lineSays('omega-web/src/omega/controllers/io.coffee:108', 'method: \'GET\'')) &&
      (await lineSays('omega-web/src/omega/controllers/io.coffee:114', 'resetOptions(result.data)')),
    'the online restore path resets the whole store from a fetched body, which is why NEXORA import is not that path',
    'io.coffee:108 and :114',
  );

  // ---- sync

  const remote = comp.syncBackends.filter((b) => b.name !== 'chrome-storage-sync');
  const impl = await linesOf('omega-target-chromium-extension/src/module/sync/sync_impl.coffee');
  check(
    remote.length === 2 &&
      impl[0].includes('sync_impl_gist') && impl[1].includes('sync_impl_webdav') &&
      (await lineSays('omega-target-chromium-extension/src/module/sync/sync_impl.coffee:30', "'webdav'")) &&
      (await lineSays('omega-target-chromium-extension/src/module/sync/sync_impl.coffee:31', 'WebDavSyncImpl')) &&
      (await lineSays('omega-target-chromium-extension/src/module/sync/sync_impl.coffee:33', 'GistSyncImpl')),
    'the sync module requires exactly the backends this file names',
    `declared remote=${remote.map((b) => b.name).join(',')} requires=${impl[0]?.trim()} / ${impl[1]?.trim()}`,
  );
  const extraBackends = comp.syncBackendsAbsent.filter((name) => corpus.toLowerCase().includes(name.toLowerCase()));
  check(
    comp.syncBackendsAbsent.length === 4 && extraBackends.length === 0,
    'no third sync backend exists in the inherited tree, including the ones a help page might claim',
    `found: ${extraBackends.join(', ') || 'none'}`,
  );
  check(
    (await lineSays('omega-target/src/options_sync.coffee:10', "BUILTINSYNCKEY = 'zeroOmegaSync'")) &&
      (await lineSays('omega-target-chromium-extension/src/coffee/background.coffee:237', "SyncStorage('sync'")) &&
      (await lineSays('omega-target-chromium-extension/src/module/sync/sync_impl_gist.coffee:10', 'https://api.github.com')) &&
      (await lineSays('omega-target-chromium-extension/src/module/sync/sync_impl_gist.coffee:25', '"Bearer "')) &&
      (await lineSays('omega-target-chromium-extension/src/module/sync/sync_impl_webdav.coffee:17', "'/zeroomega/'")) &&
      (await lineSays('omega-target-chromium-extension/src/module/sync/sync_impl_webdav.coffee:3', "COMMIT_FILENAME = 'zeroomega-commit.txt'")),
    'the built-in sync key that holds a token and the two remote file names are the ones the data lists',
    'options_sync.coffee:10, background.coffee:237, the gist and webdav name constants',
  );

  // ---- the matrix itself

  const ids = comp.matrix.map((r) => r.id);
  const dupes = ids.filter((id, i) => ids.indexOf(id) !== i);
  const gapless = ids.every((id, i) => id === `COMP-${String(i + 1).padStart(2, '0')}`);
  check(
    comp.matrix.length >= 15 && dupes.length === 0 && gapless,
    'every matrix id is a row, and the run has no gap in it',
    `${ids.length} rows, duplicates=${dupes.join(',') || 'none'}, gapless=${gapless}`,
  );
  const verdictValues = comp.verdicts.values;
  const badVerdict = comp.matrix.filter((r) => !verdictValues.includes(r.verdict)).map((r) => r.id);
  check(
    verdictValues.length === 4 && new Set(verdictValues).size === 4 && badVerdict.length === 0,
    'the verdict vocabulary is closed and every row carries exactly one of its four members',
    `off-vocabulary: ${badVerdict.join(', ') || 'none'}`,
  );
  const unresolved = [];
  for (const row of comp.matrix) {
    if (!Array.isArray(row.evidence) || row.evidence.length < 2) unresolved.push(`${row.id} has fewer than two citations`);
    for (const e of row.evidence ?? []) {
      if (!(await evidenceResolves(e))) unresolved.push(`${row.id} -> ${e}`);
    }
    if (!(row.subject ?? '').length || !(row.owed ?? '').length) unresolved.push(`${row.id} has an empty subject or owed`);
  }
  check(
    unresolved.length === 0,
    'every matrix row names evidence that resolves, in two citations or more, and says what it owes',
    `unresolved: ${unresolved.join(' | ') || 'none'}`,
  );
  const lineCites = [];
  for (const row of comp.matrix) {
    for (const e of row.evidence ?? []) {
      const m = String(e).match(/^([^#]+?):(\d+)(?:-\d+)?$/);
      if (!m) continue;
      const [file, from] = [m[1], Number(m[2])];
      const text = (await linesOf(file)) ?? [];
      if (!text[from - 1]?.trim()) lineCites.push(`${row.id} -> ${e}`);
    }
  }
  check(
    lineCites.length === 0,
    'every citation in the matrix lands on a line that has something on it',
    `blank or out of range: ${lineCites.join(', ') || 'none'}`,
  );
  const liveText = await readFile(join(REPO, 'docs', 'verification', 'live-matrix.md'), 'utf8');
  const liveIds = [...new Set([...liveText.matchAll(/^\| (LIVE-\d+) \|/gm)].map((m) => m[1]))];
  const namedLive = [...new Set(comp.matrix.flatMap((r) => [...String(r.owed).matchAll(/LIVE-\d+/g)].map((m) => m[0])))];
  const ghostLive = namedLive.filter((id) => !liveIds.includes(id));
  check(
    liveIds.length >= 20 && ghostLive.length === 0 && namedLive.length >= 2,
    'every LIVE row the matrix names exists in the live matrix',
    `not a row: ${ghostLive.join(', ') || 'none'}; ${namedLive.length} of ${liveIds.length} referenced`,
  );
  const deferred = comp.matrix.filter((r) => r.verdict === 'deferred-needs-measurement');
  const silent = deferred.filter((r) => /^none\b/i.test(String(r.owed))).map((r) => r.id);
  check(
    deferred.length === 4 && silent.length === 0,
    'a row awaiting a measurement names the measurement, and four rows do',
    `silent: ${silent.join(', ') || 'none'}; deferred=${deferred.map((r) => r.id).join(', ')}`,
  );
  const byVerdict = (v) => comp.matrix.filter((r) => r.verdict === v).length;
  check(
    byVerdict('survives-unchanged') === 6 && byVerdict('imports-with-report') === 5 &&
      byVerdict('refused-with-reason') === 5 && byVerdict('deferred-needs-measurement') === 4 &&
      comp.matrix.length === 20,
    'the verdict column has the distribution doc 38 reads aloud',
    `survives=${byVerdict('survives-unchanged')} imports=${byVerdict('imports-with-report')} ` +
      `refused=${byVerdict('refused-with-reason')} deferred=${byVerdict('deferred-needs-measurement')} of ${comp.matrix.length}`,
  );

  // ---- the argument, when it exists

  const compatDoc = join(REPO, 'docs', 'architecture', '38-compatibility-matrix.md');
  if (existsSync(compatDoc)) {
    const text = await readFile(compatDoc, 'utf8');
    const openItems = text.split('**Open items**')[1] ?? '';
    const missing = [
      ...ids.filter((id) => !text.includes(id)),
      ...verdictValues.filter((v) => !text.includes(v)),
      ...dataTypes.filter((t) => !text.includes(t)),
      ...Object.keys(dataAliases).filter((a) => !text.includes(a)),
      ...comp.syncBackends.map((b) => b.name).filter((n) => !text.includes(n)),
      ...comp.optionKeys.scalarKeys.filter((k) => !text.includes(k)),
      ...comp.apiSurface.absent.filter((a) => !text.includes(a)),
      ...comp.syncBackendsAbsent.filter((n) => !text.includes(n)),
      ...comp.apiSurface.proxyModesUsed.filter((m) => !text.includes(`\`${m}\``)),
      ...deferred.map((r) => r.id).filter((id) => !openItems.includes(id)),
    ];
    check(
      missing.length === 0 && text.includes('compatibility.json'),
      'the compatibility document names every row, verdict, type, alias, backend, option key, absent API, mode and open item the data declares',
      `absent from doc 38: ${missing.join(', ') || 'none'}`,
    );
    const closes = /LIVE-\d+|verify\.mjs|39-verification-matrix\.md|20-target-repository-layout\.md/;
    const rowClaims = comp.matrix.filter((r) => r.verdict === 'survives-unchanged' &&
      !/^none\b/i.test(String(r.owed)) && !closes.test(String(r.owed)));
    check(
      rowClaims.length === 0,
      'no row that claims survival owes an obligation no named check closes',
      `unowned obligation: ${rowClaims.map((r) => r.id).join(', ') || 'none'}`,
    );
    const ctxDoc = await readFile(join(REPO, 'docs', 'architecture', '21-system-context.md'), 'utf8');
    const splitDoc = await readFile(join(REPO, 'docs', 'architecture', '34-data-split.md'), 'utf8');
    check(
      ctxDoc.includes('38-compatibility-matrix.md` §3') || ctxDoc.includes('38-compatibility-matrix.md, Firefox'),
      'the system context asks this document for the browser floor by name, as the floor is still unmeasured',
      'doc 21 §3 points at doc 38 for the floor',
    );
    check(
      splitDoc.includes('38-compatibility-matrix.md` §5') &&
        deferred.every((r) => text.includes(r.id)),
      'the data split cites the section that owns the `+` key, and the four deferred rows are disclosed in doc 38',
      'doc 34 must point at §5 for the legacy key behaviour',
    );
    const checkTables = text.split('## 11. Who owns each check')[1]?.split('## 13.')[0] ?? '';
    const quoted = [...checkTables.matchAll(/"([^"]+)"/g)].map((m) => m[1]);
    const staleLabels = quoted.filter((q) => !labels.has(q));
    check(
      quoted.length >= 12 && staleLabels.length === 0,
      'every mechanism doc 38 names is a check this run actually makes',
      `not an assertion label: ${staleLabels.join(' | ') || 'none'}`,
    );
  } else {
    note('DEFERRED: 38-compatibility-matrix.md cross-check (file not present yet)');
  }
}

// ---------------------------------------------------------------- the queue as a contract
//
// Every line in the corpus that says `Verified: NO — deferred to Phase 2` is a deferred claim. ADR-0001 rule 9 put
// the label there, docs/architecture/20-target-repository-layout.md §13 made this file the owner of every one of
// them, and docs/verification/live-matrix.md promotion rule 4 made the same demand, so that no design document
// could invent a deferral nobody queued. A label scattered through a corpus is a rumour; `packages/schema/verification.json` is
// the queue, and the queue is only a queue if it is *exact*. Three failures are invisible to a reader and to any
// prose review, and each is a check below: a labelled claim no row answers (silently unowned work), an anchor that
// no longer lands on a labelled line (a row that has been quietly closed, or moved, or deleted from the document
// that carried it), and a line counted twice (two rows believing they own one obligation). The corpus is re-read
// on every run rather than cached, because the whole point is that the documents keep being edited.
//
// The label pattern lives in the data, not here, so that the queue states its own membership rule; the leading
// `\b` in it is load-bearing -- without it `UNVERIFIED: no lockfile is tracked` reads as a queued claim, which
// would add a row nobody wrote and hide the fact that the two vocabularies in the corpus are one alternation.

{
  const verif = await readJson(join(PKG, 'verification.json'));
  const LIVE_DOC = 'docs/verification/live-matrix.md';

  const mdFiles = [];
  for await (const f of walk(join(REPO, 'docs'))) {
    if (!f.endsWith('.md')) continue;
    const rel = f.slice(REPO.length + 1).replace(/\\/g, '/');
    if (rel === 'docs/architecture/39-verification-matrix.md') continue;
    mdFiles.push({ rel, abs: f });
  }
  check(
    mdFiles.length >= 40,
    'the queue is derived from the whole documentation corpus, not from a list of files it was written against',
    `${mdFiles.length} markdown documents under docs/`,
  );

  const MARKER = new RegExp(verif.marker.pattern, verif.marker.flags);
  const labelled = [];
  for (const { rel, abs } of mdFiles) {
    const lines = (await readFile(abs, 'utf8')).split(/\r?\n/);
    lines.forEach((line, i) => {
      MARKER.lastIndex = 0;
      if (MARKER.test(line)) labelled.push({ doc: rel, n: i + 1, line });
    });
  }
  const derived = labelled.length;
  check(
    derived === verif.marker.counted,
    'the number of deferred claims the queue says the corpus carries is what the corpus carries',
    `derived ${derived}, declared ${verif.marker.counted} -- a document gained or lost a label, or the count was typed`,
  );

  const ids = verif.matrix.map((r) => r.id);
  const retiredIds = verif.retired.rows.map((r) => r.id);
  const dupes = ids.filter((id, i) => ids.indexOf(id) !== i);
  const retiredDupes = retiredIds.filter((id, i) => retiredIds.indexOf(id) !== i);
  const recycled = ids.filter((id) => retiredIds.includes(id));
  const issued = ids.length + retiredIds.length;
  const holes = Array.from({ length: issued }, (_, i) => `VER-${String(i + 1).padStart(2, '0')}`)
    .filter((id) => !ids.includes(id) && !retiredIds.includes(id));
  const ascending = ids.every((id, i) => i === 0 || Number(id.slice(4)) > Number(ids[i - 1].slice(4)));
  const thinTomb = verif.retired.rows.filter((r) =>
    !/^\d{4}-\d{2}-\d{2}$/.test(r.closed ?? '') || (r.was ?? '').length < 40 || (r.artefact ?? '').length < 40).map((r) => r.id);
  check(
    ids.length >= 60 && dupes.length === 0 && retiredDupes.length === 0 && recycled.length === 0 &&
      holes.length === 0 && ascending && thinTomb.length === 0,
    'every VER id the queue ever issued is live or retired, no id is recycled, and a retired one says what it was and what closed it',
    `${ids.length} rows, ${retiredIds.length} retired, duplicates=${[...dupes, ...retiredDupes].join(',') || 'none'}, ` +
      `recycled=${recycled.join(',') || 'none'}, holes=${holes.join(',') || 'none'}, ascending=${ascending}, ` +
      `thin tombstones=${thinTomb.join(',') || 'none'}`,
  );
  const offVocab = (key, values) => verif.matrix.filter((r) => !values.includes(r[key])).map((r) => r.id);
  const badState = offVocab('state', verif.states.values);
  const badOwner = offVocab('owner', verif.owners.values);
  const badKind = verif.matrix.filter((r) => !verif.closingKinds.values.includes(r.closesBy.kind)).map((r) => r.id);
  check(
    badState.length === 0 && badOwner.length === 0 && badKind.length === 0 &&
      new Set(verif.states.values).size === 5 && new Set(verif.owners.values).size === 9 &&
      new Set(verif.closingKinds.values).size === 8,
    'every row names one state, one owner and one closing mechanism, and all three vocabularies are closed',
    `state=${badState.join(',')} owner=${badOwner.join(',')} kind=${badKind.join(',')}`,
  );
  const unexplained = verif.matrix
    .filter((r) => (r.why ?? '').length < 40 || !(r.subject ?? '').length || !Array.isArray(r.at) || r.at.length === 0)
    .map((r) => r.id);
  check(
    unexplained.length === 0,
    'every row says what is unverified and why the state is what it is, rather than only that it is owed',
    `thin or unanchored: ${unexplained.join(', ') || 'none'}`,
  );

  // ---- the partition: each labelled line is answered exactly once

  const linesOf = async (rel) => {
    const abs = rel.startsWith('docs/') ? join(REPO, rel) : join(PKG, rel);
    return existsSync(abs) ? (await readFile(abs, 'utf8')).split(/\r?\n/) : null;
  };
  const ownersOf = new Map();
  const broken = [];
  const record = (key, what) => {
    ownersOf.set(key, (ownersOf.get(key) ?? []).concat(what));
  };
  const claimKey = (d, n) => `${d}:${n}`;
  for (const r of verif.matrix) {
    for (const a of r.at) {
      const lines = await linesOf(a.doc);
      if (lines === null) {
        broken.push(`${r.id} -> ${a.doc} (no such document)`);
        continue;
      }
      const hits = [];
      lines.forEach((l, i) => {
        if (l.includes(a.anchor)) hits.push(i + 1);
      });
      if (hits.length === 0) broken.push(`${r.id} -> anchor not found in ${a.doc}`);
      else if (hits.length > 1) broken.push(`${r.id} -> anchor matches ${hits.length} lines in ${a.doc}`);
      else {
        MARKER.lastIndex = 0;
        if (!MARKER.test(lines[hits[0] - 1])) broken.push(`${r.id} -> ${a.doc}:${hits[0]} no longer carries a label`);
        record(claimKey(a.doc, hits[0]), r.id);
      }
    }
  }
  for (const v of verif.vocabulary) {
    const lines = await linesOf(v.doc);
    if (lines === null) {
      broken.push(`vocabulary -> ${v.doc} (no such document)`);
      continue;
    }
    const hits = [];
    lines.forEach((l, i) => {
      if (l.includes(v.anchor)) hits.push(i + 1);
    });
    if (hits.length !== 1) broken.push(`vocabulary -> ${v.doc} anchor matches ${hits.length} lines`);
    else record(claimKey(v.doc, hits[0]), 'vocabulary');
  }
  for (const l of labelled) {
    if (l.doc === LIVE_DOC && /^\| (LIVE-\d\d) \|/.test(l.line)) record(claimKey(l.doc, l.n), 'live-row');
  }
  const twice = [...ownersOf].filter(([, who]) => new Set(who).size > 1);
  check(
    broken.length === 0,
    'every anchor in the queue lands on one line of one document, and that line still carries the label',
    `broken: ${broken.slice(0, 6).join(' | ') || 'none'}${broken.length > 6 ? ` | +${broken.length - 6} more` : ''}`,
  );
  check(
    twice.length === 0,
    'no deferred claim is counted as owned by two rows, or by a row and the vocabulary',
    `claimed twice: ${twice.map(([k, who]) => `${k} by ${[...new Set(who)].join('+')}`).join(' | ') || 'none'}`,
  );
  const unowned = labelled.filter((l) => !ownersOf.has(claimKey(l.doc, l.n)));
  check(
    unowned.length === 0,
    'every deferred claim in the corpus is answered by a row, a vocabulary entry, or an inherited live row',
    `unowned: ${unowned.slice(0, 8).map((l) => `${l.doc}:${l.n}`).join(' | ') || 'none'}` +
      `${unowned.length > 8 ? ` | +${unowned.length - 8} more` : ''}`,
  );
  const rowKeys = [...ownersOf].filter(([, who]) => who.some((x) => x !== 'vocabulary' && x !== 'live-row'));
  const vocabKeys = [...ownersOf].filter(([, who]) => who.includes('vocabulary') && who.length === 1);
  const liveKeys = [...ownersOf].filter(([, who]) => who.length === 1 && who[0] === 'live-row');
  check(
    vocabKeys.length === verif.vocabulary.length && liveKeys.length === 25 &&
      rowKeys.length + vocabKeys.length + liveKeys.length === derived,
    'the queue partitions the corpus exactly: rows, plus the sentences that define the label, plus the inherited live rows',
    `rows=${rowKeys.length} + vocabulary=${vocabKeys.length} + live=${liveKeys.length} = ` +
      `${rowKeys.length + vocabKeys.length + liveKeys.length}, derived=${derived}`,
  );

  // ---- the live half is read from its owner, never copied

  const liveText = await readFile(join(REPO, LIVE_DOC), 'utf8');
  const liveRows = [...liveText.matchAll(/^\| (LIVE-\d\d) \|(.*)$/gm)].map((m) => ({ id: m[1], rest: m[2] }));
  const liveIds = liveRows.map((r) => r.id);
  const denseLive = liveIds.every((id, i) => id === `LIVE-${String(i + 1).padStart(2, '0')}`);
  const statusOf = (r) => r.rest.replace(/\|$/, '').split('|').pop().trim();
  const partial = liveRows.filter((r) => /^`?PARTIAL/.test(statusOf(r))).map((r) => r.id);
  const executed = liveRows.filter((r) => /^`?YES/.test(statusOf(r))).map((r) => r.id);
  const unknownStatus = liveRows.filter((r) => !/^`?(NO|PARTIAL|YES)/.test(statusOf(r))).map((r) => r.id);
  check(
    liveIds.length === 25 && denseLive,
    'the live rows this queue inherits are exactly LIVE-01 to LIVE-25, in order',
    `derived ${liveIds.length}: ${liveIds.join(',')}`,
  );
  check(
    partial.length === 1 && partial[0] === 'LIVE-14' && executed.length === 0 && unknownStatus.length === 0,
    'in this iteration exactly one live row is partially executed and none is executed -- the queue may not read ahead',
    `partial=${partial.join(',') || 'none'} executed=${executed.join(',') || 'none'} unreadable=${unknownStatus.join(',') || 'none'}`,
  );
  const ghostRefs = verif.matrix
    .filter((r) => r.closesBy.kind === 'live-row' && !liveIds.includes(r.closesBy.ref))
    .map((r) => `${r.id}->${r.closesBy.ref}`);
  const prematureNew = verif.matrix
    .filter((r) => r.closesBy.kind === 'new-live-row' && liveIds.some((id) => r.closesBy.ref.includes(id)))
    .map((r) => r.id);
  check(
    ghostRefs.length === 0 && prematureNew.length === 0,
    'a row that closes by a live row names a row the live matrix has, and a row that needs a new one does not claim it exists',
    `not a row: ${ghostRefs.join(', ') || 'none'}; already present: ${prematureNew.join(', ') || 'none'}`,
  );

  // ---- promises the queue makes to itself: a named assertion must not already be green

  const promisedAssertions = verif.matrix.filter((r) => r.closesBy.kind === 'assertion');
  const alreadyGreen = promisedAssertions.filter((r) => labels.has(r.closesBy.ref)).map((r) => r.id);
  check(
    promisedAssertions.length >= 8 && alreadyGreen.length === 0,
    'every row that promises an assertion promises one this run does not yet make, so a closed row is deleted rather than re-labelled',
    `already green and still queued: ${alreadyGreen.join(', ') || 'none'}`,
  );

  // ---- the compatibility rows that owe a measurement are queued here too

  const comp = await readJson(join(PKG, 'compatibility.json'));
  const deferredComp = comp.matrix.filter((r) => r.verdict === 'deferred-needs-measurement').map((r) => r.id);
  const queueText = JSON.stringify(verif.matrix);
  const missingComp = deferredComp.filter((id) => !queueText.includes(id));
  check(
    deferredComp.length === 4 && missingComp.length === 0,
    'the four compatibility promises that owe a browser measurement are queued as the measurement they owe',
    `not in the queue: ${missingComp.join(', ') || 'none'}`,
  );

  // ---- the distribution the document reads aloud

  const count = (key, v) => verif.matrix.filter((r) => r[key] === v).length;
  const kindCount = (k) => verif.matrix.filter((r) => r.closesBy.kind === k).length;
  check(
    count('state', 'no-code') === 40 && count('state', 'no-check') === 2 && count('state', 'no-run') === 11 &&
      count('state', 'no-fixture') === 3 && count('state', 'needs-human') === 4 && verif.matrix.length === 60,
    'the state distribution doc 39 reads aloud is what the data says',
    `no-code=${count('state', 'no-code')} no-check=${count('state', 'no-check')} no-run=${count('state', 'no-run')} ` +
      `no-fixture=${count('state', 'no-fixture')} needs-human=${count('state', 'needs-human')} of ${verif.matrix.length}`,
  );
  check(
    count('owner', 'connector') === 34 && count('owner', 'extension') === 9 && count('owner', 'packages') === 3 &&
      count('owner', 'ui') === 3 && count('owner', 'contracts') === 4 && count('owner', 'tools') === 1 &&
      count('owner', 'ci') === 2 && count('owner', 'human') === 1 && count('owner', 'external') === 3,
    'the owner distribution doc 39 reads aloud is what the data says',
    `connector=${count('owner', 'connector')} extension=${count('owner', 'extension')} ` +
      `packages=${count('owner', 'packages')} ui=${count('owner', 'ui')} contracts=${count('owner', 'contracts')} ` +
      `tools=${count('owner', 'tools')} ci=${count('owner', 'ci')} human=${count('owner', 'human')} external=${count('owner', 'external')}`,
  );
  check(
    kindCount('live-row') === 42 && kindCount('new-live-row') === 0 && kindCount('assertion') === 8 &&
      kindCount('gate') === 2 && kindCount('tool') === 1 && kindCount('ci-step') === 2 &&
      kindCount('decision') === 1 && kindCount('external-measurement') === 4,
    'the closing-mechanism distribution doc 39 reads aloud is what the data says',
    `live=${kindCount('live-row')} new-live=${kindCount('new-live-row')} assertion=${kindCount('assertion')} ` +
      `gate=${kindCount('gate')} tool=${kindCount('tool')} ci=${kindCount('ci-step')} ` +
      `decision=${kindCount('decision')} external=${kindCount('external-measurement')}`,
  );

  // ---- the argument, when it exists

  const verifDoc = join(REPO, 'docs', 'architecture', '39-verification-matrix.md');
  if (existsSync(verifDoc)) {
    const text = await readFile(verifDoc, 'utf8');
    const missing = [
      ...ids.filter((id) => !text.includes(id)),
      ...verif.states.values.filter((s) => !text.includes(`\`${s}\``)),
      ...verif.owners.values.filter((o) => !text.includes(`\`${o}\``)),
      ...verif.closingKinds.values.filter((k) => !text.includes(`\`${k}\``)),
      ...deferredComp.filter((id) => !text.includes(id)),
    ];
    check(
      missing.length === 0 && text.includes('verification.json'),
      'the verification document names every row, state, owner, closing kind and owed compatibility promise the data declares',
      `absent from doc 39: ${missing.join(', ') || 'none'}`,
    );
    const checkTables = text.split('## 11. Who owns each check')[1]?.split('## 13.')[0] ?? '';
    const quoted = [...checkTables.matchAll(/"([^"]+)"/g)].map((m) => m[1]);
    const staleLabels = quoted.filter((q) => !labels.has(q));
    check(
      quoted.length >= 12 && staleLabels.length === 0,
      'every mechanism doc 39 names is a check this run actually makes',
      `not an assertion label: ${staleLabels.join(' | ') || 'none'}`,
    );
    // §4.14's third finding, turned into a check. The three distribution assertions above name this document in
    // their labels and compare the data against a constant, so a tally here could rot while every run stayed green
    // -- and three of them had. This one opens the document and reads its own numbers: the state table, the owner
    // and closing-kind lists, and every prose sentence that states one of the same counts.
    const sec4 = text.split(/^## 4\. /m)[1]?.split(/^## 5\./m)[0] ?? '';
    const sec5 = text.split(/^## 5\. /m)[1]?.split(/^## 6\./m)[0] ?? '';
    const total = verif.matrix.length;
    const tallyGaps = [];
    let proseHits = 0;
    const readNumbers = (label, re, expected) => {
      const found = [...text.matchAll(re)];
      proseHits += found.length;
      if (!found.length) tallyGaps.push(`${label}: no line in doc 39 states it in numbers`);
      for (const m of found) {
        m.slice(1).forEach((g, i) => {
          if (Number(g) !== expected[i]) tallyGaps.push(`${label}: doc 39 says ${g}, the data says ${expected[i]}`);
        });
      }
    };
    readNumbers('the deferral count', /the (\d+) deferrals the corpus says exist/g, [verif.marker.counted]);
    readNumbers('the invariant-5 count', /The count `(\d+)` is an output of the harness/g, [derived]);
    readNumbers('the `N of M rows are no-code` sentence', /(\d+) of (\d+) rows are `no-code`/g,
      [count('state', 'no-code'), total]);
    readNumbers('the `N of M rows close by executing an existing live row` sentence',
      /(\d+) of (\d+) rows close by executing an existing live row/g, [kindCount('live-row'), total]);
    readNumbers('the `N rows promise a named assertion` sentence', /(\d+) rows promise a named assertion/g,
      [kindCount('assertion')]);
    readNumbers('the `N no-check rows` sentence', /(\d+) `no-check` rows/g, [count('state', 'no-check')]);
    readNumbers('the `N no-code rows` sentence', /The (\d+) `no-code` rows/g, [count('state', 'no-code')]);
    readNumbers('the `N rows say the component does not exist` sentence', /(\d+) rows say the component does not\n?\s*exist/g,
      [count('state', 'no-code')]);
    readNumbers('the `N rows are needs-human` sentence', /(\d+) rows are `needs-human`/g,
      [count('state', 'needs-human')]);
    // §4.31's two additions. The queue's own size and its retirement ledger are stated twice in prose — §10's
    // headline and §11's last row — and neither sentence was among the counts read above, so the slice that
    // retired `VER-81` left both saying 74 rows and eight ids behind a data set that had moved to 73 and nine.
    readNumbers('the §10 headline', /(\d+) rows, grouped by state/g, [total]);
    readNumbers('the §10 retired-ids count', /the (\d+) ids this queue has closed/g, [verif.retired.rows.length]);
    readNumbers('the §11 `N live, M retired` row', /(\d+) live, (\d+) retired/g,
      [total, verif.retired.rows.length]);
    const stateRows = [...sec4.matchAll(/^\| `(no-code|no-check|no-run|no-fixture|needs-human)` \| (\d+) \|/gm)];
    if (stateRows.length !== verif.states.values.length) {
      tallyGaps.push(`§4's state table names ${stateRows.length} states; the data declares ${verif.states.values.length}`);
    }
    for (const [, state, n] of stateRows) {
      if (Number(n) !== count('state', state)) {
        tallyGaps.push(`§4's table says ${n} ${state} rows, the data says ${count('state', state)}`);
      }
    }
    const pairsIn = (src) => new Map([...src.matchAll(/`([a-z][a-z-]+)` (\d+)/g)].map((m) => [m[1], Number(m[2])]));
    const ownersInDoc = pairsIn(sec5.split('`closingKinds`')[0] ?? '');
    const kindsInDoc = pairsIn((sec5.split('`closingKinds`')[1] ?? '').split('Three consequences')[0] ?? '');
    if (ownersInDoc.size !== verif.owners.values.length) {
      tallyGaps.push(`§5 lists ${ownersInDoc.size} owners; the vocabulary declares ${verif.owners.values.length}`);
    }
    if (kindsInDoc.size !== verif.closingKinds.values.length) {
      tallyGaps.push(`§5 lists ${kindsInDoc.size} closing kinds; the vocabulary declares ${verif.closingKinds.values.length}`);
    }
    for (const owner of verif.owners.values) {
      if (ownersInDoc.get(owner) !== count('owner', owner)) {
        tallyGaps.push(`§5 says ${ownersInDoc.get(owner) ?? 'absent'} rows are owned by ${owner}; the data says ${count('owner', owner)}`);
      }
    }
    for (const kind of verif.closingKinds.values) {
      if (kindsInDoc.get(kind) !== kindCount(kind)) {
        tallyGaps.push(`§5 says ${kindsInDoc.get(kind) ?? 'absent'} rows close by ${kind}; the data says ${kindCount(kind)}`);
      }
    }
    check(
      tallyGaps.length === 0,
      'doc 39 reads aloud the tallies the data has, and this check reads them out of the document',
      tallyGaps.join('; ') ||
        `${stateRows.length} states, ${ownersInDoc.size} owners, ${kindsInDoc.size} kinds and ${proseHits} prose counts agree with ${total} rows`,
    );
    const layoutDoc = await readFile(join(REPO, 'docs', 'architecture', '20-target-repository-layout.md'), 'utf8');
    check(
      layoutDoc.includes('39-verification-matrix.md') &&
        liveText.includes('39-verification-matrix.md') &&
        text.includes('docs/verification/live-matrix.md'),
      'the two documents that made this queue mandatory are named by it, and it names the live matrix it inherits from',
      'doc 20 and the live matrix each point here; this document must point back at both',
    );
  } else {
    note('DEFERRED: 39-verification-matrix.md cross-check (file not present yet)');
  }
}


// ---------------------------------------------------------------- the open questions as a contract
//
// A document that cannot answer something says so by naming docs/architecture/40-open-decisions.md. A deferral is
// worth nothing while nothing counts it: the register in `packages/schema/open-decisions.json` is re-derived from
// the corpus on every run, so a question nobody queued, a queued question whose citing line was rewritten under it,
// and one line claimed by two entries are each a build failure. The membership rule is the data's own
// (`citation.rule`): a line that names the file, plus every bullet of a list whose header line names it -- the
// header is the sentence that says "these are open", not a deferral in itself, and a line that merely lists the
// file among documents is declared in `notADeferral` rather than silently dropped from the arithmetic.
//
// The register also ties each question to the rows it gates, which is what makes "open" auditable rather than a
// mood: a `verification.json` row that closes by a decision must be named by at least one entry, or the queue
// waits on a question nobody wrote down.

{
  const odec = await readJson(join(PKG, 'open-decisions.json'));
  const verifForOdec = await readJson(join(PKG, 'verification.json'));
  const compatForOdec = await readJson(join(PKG, 'compatibility.json'));
  const OPEN_DOC = 'docs/architecture/40-open-decisions.md';
  // A document may name the register by basename or by path; either is a deferral, so the scan matches the
  // basename and `citation.where` says which tree it reads.
  const OPEN_NAME = '40-open-decisions.md';

  const openLines = new Map();
  for await (const f of walk(join(REPO, 'docs'))) {
    if (!f.endsWith('.md')) continue;
    const rel = f.slice(REPO.length + 1).replace(/\\/g, '/');
    if (rel.endsWith(OPEN_NAME)) continue;
    openLines.set(rel, (await readFile(f, 'utf8')).split(/\r?\n/));
  }
  check(
    openLines.size >= 40,
    'the register is derived from the whole documentation corpus, not from the files it was written against',
    `${openLines.size} markdown documents under docs/`,
  );

  const deferralSites = new Map();
  const capturingHeaders = [];
  for (const [rel, lines] of openLines) {
    lines.forEach((line, i) => {
      if (!line.includes(OPEN_NAME)) return;
      const trimmed = line.trim();
      if (!/[:]\*{0,2}\s*$/.test(trimmed)) {
        deferralSites.set(`${rel}:${i + 1}`, trimmed);
        return;
      }
      let j = i + 1;
      let captured = 0;
      while (j < lines.length) {
        while (j < lines.length && lines[j].trim() === '') j += 1;
        if (j >= lines.length || !/^\s*-\s/.test(lines[j])) break;
        let k = j + 1;
        while (k < lines.length && lines[k].trim() !== '' && !/^\s*-\s/.test(lines[k])) k += 1;
        deferralSites.set(`${rel}:${j + 1}`, lines[j].trim());
        captured += 1;
        j = k;
      }
      if (captured === 0) deferralSites.set(`${rel}:${i + 1}`, trimmed);
      else capturingHeaders.push(`${rel}:${i + 1}`);
    });
  }
  for (const key of capturingHeaders) deferralSites.delete(key);
  const notSites = [];
  for (const nd of odec.notADeferral) {
    const lines = openLines.get(nd.doc);
    const hits = lines === undefined ? [] : lines.map((l, i) => [l, i + 1]).filter(([l]) => l.includes(nd.anchor));
    if (hits.length !== 1) notSites.push(`${nd.doc} anchor matches ${hits.length} lines`);
    else if (!deferralSites.delete(`${nd.doc}:${hits[0][1]}`)) notSites.push(`${nd.doc}:${hits[0][1]} is not a site`);
  }
  check(
    notSites.length === 0,
    'each line the register declares "not a deferral" is exactly one line and really would otherwise count as one',
    `wrong: ${notSites.join(' | ') || 'none'}`,
  );
  check(
    deferralSites.size === odec.citation.counted,
    'the number of lines that defer to the open-decisions register is the number the register declares',
    `derived ${deferralSites.size}, declared ${odec.citation.counted} -- a document gained or lost a deferral, ` +
      'or the count was typed rather than measured',
  );

  const siteOwner = new Map();
  const unresolved = [];
  for (const o of odec.register) {
    for (const a of o.at) {
      const lines = openLines.get(a.doc);
      if (lines === undefined) {
        unresolved.push(`${o.id} -> ${a.doc} (no such document)`);
        continue;
      }
      const hits = lines.map((l, i) => [l, i + 1]).filter(([l]) => l.includes(a.anchor));
      if (hits.length !== 1) {
        unresolved.push(`${o.id} -> anchor matches ${hits.length} lines in ${a.doc}`);
        continue;
      }
      const key = `${a.doc}:${hits[0][1]}`;
      if (!deferralSites.has(key)) unresolved.push(`${o.id} -> ${key} is no longer a deferral site`);
      else if (siteOwner.has(key)) unresolved.push(`${key} claimed by ${siteOwner.get(key)} and ${o.id}`);
      else siteOwner.set(key, o.id);
    }
  }
  const unowned = [...deferralSites.keys()].filter((k) => !siteOwner.has(k));
  check(
    unresolved.length === 0,
    'every entry in the register lands on one line of one document, and that line still defers to it',
    `broken: ${unresolved.slice(0, 6).join(' | ') || 'none'}${unresolved.length > 6 ? ` | +${unresolved.length - 6} more` : ''}`,
  );
  check(
    unowned.length === 0,
    'no deferral in the corpus is left without a register entry, so no open question is unregistered work',
    `unowned: ${unowned.slice(0, 6).join(' | ') || 'none'}${unowned.length > 6 ? ` | +${unowned.length - 6} more` : ''}`,
  );

  const oIds = odec.register.map((o) => o.id);
  // The register's ids used to be required dense, which made the deletion its own description prescribes for an
  // answered entry a renumbering of every question after it -- and an `ODEC-NN` is cited by `verification.json`
  // rows, by doc 39 and by this repository's ledger. Doc 39 §9 rule 8 settled that argument for `VER-NN`; the
  // same reasoning applies here, so an answered id retires in `#/retired` and the numbering is gap-allowed.
  const oRetired = (odec.retired?.rows ?? []).map((r) => r.id);
  const oDupes = [...oIds.filter((id, i) => oIds.indexOf(id) !== i), ...oRetired.filter((id, i) => oRetired.indexOf(id) !== i)];
  const oRecycled = oIds.filter((id) => oRetired.includes(id));
  const oHoles = Array.from({ length: oIds.length + oRetired.length }, (_, i) => `ODEC-${String(i + 1).padStart(2, '0')}`)
    .filter((id) => !oIds.includes(id) && !oRetired.includes(id));
  const thinRetired = (odec.retired?.rows ?? []).filter((r) =>
    !/^ODEC-\d\d$/.test(r.id ?? '') || !r.decided || (r.was ?? '').length < 20 || (r.artefact ?? '').length < 40);
  check(
    oIds.every((id) => /^ODEC-\d\d$/.test(id)) && oIds.length >= 30 && oDupes.length === 0 &&
      oRecycled.length === 0 && oHoles.length === 0 && thinRetired.length === 0 &&
      odec.register.every((o) => o.at.length >= 1),
    'every ODEC id the register ever issued is live or retired, no id is recycled, a retired one says what it was and what answered it, and every live question was born from a line that deferred to it',
    `${oIds.length} live, ${oRetired.length} retired, duplicates=${oDupes.join(',') || 'none'}, ` +
      `recycled=${oRecycled.join(',') || 'none'}, unaccounted=${oHoles.join(',') || 'none'}, ` +
      `thin tombstones=${thinRetired.map((r) => r.id).join(',') || 'none'}`,
  );
  const thin = odec.register.filter((o) =>
    !/\?\s*$/.test(o.question) || o.options.length < 2 || (o.why ?? '').length < 40 ||
      !odec.decidedBy.values.includes(o.decidedBy)).map((o) => o.id);
  check(
    thin.length === 0,
    'every open question is a question, names who can answer it, offers the choices a reader must weigh, and says why',
    `thin or off-vocabulary: ${thin.join(', ') || 'none'}`,
  );
  check(
    new Set(odec.decidedBy.values).size === odec.decidedBy.values.length && odec.decidedBy.values.length >= 5 &&
      Object.keys(odec.decidedBy.meaning).length === odec.decidedBy.values.length &&
      odec.scope.owns.length >= 3 && odec.scope.doesNotOwn.length >= 5,
    'the register owns a closed vocabulary of who-can-decide, each member defined, and a boundary it states itself',
    `values=${odec.decidedBy.values.length} defined=${Object.keys(odec.decidedBy.meaning).length} ` +
      `owns=${odec.scope.owns.length} doesNotOwn=${odec.scope.doesNotOwn.length}`,
  );

  const verifIds = new Set(verifForOdec.matrix.map((r) => r.id));
  const compatIds = new Set(compatForOdec.matrix.map((r) => r.id));
  const phantom = [];
  for (const o of odec.register) {
    for (const v of o.blocks.verification) if (!verifIds.has(v)) phantom.push(`${o.id} -> ${v}`);
    for (const c of o.blocks.compatibility) if (!compatIds.has(c)) phantom.push(`${o.id} -> ${c}`);
  }
  const gated = new Set(odec.register.flatMap((o) => o.blocks.verification));
  const decisionRows = verifForOdec.matrix.filter((r) => r.closesBy.kind === 'decision').map((r) => r.id);
  check(
    phantom.length === 0 && decisionRows.every((id) => gated.has(id)) &&
      gated.size >= decisionRows.length,
    'every row a question gates exists, and every row the queue says waits on a decision is gated by a written question',
    `phantom=${phantom.join(', ') || 'none'} unregistered=${decisionRows.filter((id) => !gated.has(id)).join(', ') || 'none'}`,
  );
  const nonBlocking = odec.register.filter((o) => o.blocks.verification.length + o.blocks.compatibility.length === 0);
  check(
    nonBlocking.length === 0,
    'no register entry asks a question that blocks nothing, so an open question is work owed and not a worry noted',
    `blocks nothing: ${nonBlocking.map((o) => o.id).join(', ') || 'none'}`,
  );

  const odecDoc = join(REPO, 'docs', 'architecture', '40-open-decisions.md');
  if (existsSync(odecDoc)) {
    const text = await readFile(odecDoc, 'utf8');
    const missing = [
      ...oIds.filter((id) => !text.includes(id)),
      ...odec.decidedBy.values.filter((v) => !text.includes(`\`${v}\``)),
    ];
    check(
      missing.length === 0 && text.includes('open-decisions.json'),
      'the open-decisions document names every registered question and every word of the who-can-decide vocabulary',
      `absent from doc 40: ${missing.join(', ') || 'none'}`,
    );
    check(
      !/Verified: *(?:NO|PARTIAL)/i.test(text),
      'the register document is a list of questions, not a source of deferred verification claims of its own',
      'doc 40 carries a `Verified:` label; the queue in verification.json owns those, and this document is not in it',
    );
    // The register's own tallies, read out of the document that states them. doc 39 §11 has had this since it
    // caught three stale distributions; doc 40 had not, and its §3 table said `contract` 16 against a data of 15
    // from the commit that retired `ODEC-02` until the next one happened to make the number right again.
    const sec3 = text.split(/^## 3\. /m)[1]?.split(/^## 4\./m)[0] ?? '';
    const gaps = [];
    const byKind = (k) => odec.register.filter((e) => e.decidedBy === k).length;
    const kindsIn = [...sec3.matchAll(/^\| `([a-z-]+)` \| (\d+) \|/gm)];
    if (kindsIn.length !== odec.decidedBy.values.length) {
      gaps.push(`§3's table names ${kindsIn.length} kinds; the vocabulary declares ${odec.decidedBy.values.length}`);
    }
    for (const [, kind, n] of kindsIn) {
      if (!odec.decidedBy.values.includes(kind)) gaps.push(`§3's table names ${kind}, the vocabulary does not`);
      else if (Number(n) !== byKind(kind)) gaps.push(`§3's table says ${n} ${kind} questions; the data says ${byKind(kind)}`);
    }
    // §6's table is the register read aloud, and nothing compared it with the data: four ids sat in that "open"
    // table after retiring, and one row still gated a queue row that had left with its own question. Membership,
    // kind and gates are all data the table copies, so all three are read back out of it here.
    const sec6 = text.split(/^## 6\. /m)[1]?.split(/^## 7\./m)[0] ?? '';
    const tableRows = sec6
      .split('\n')
      .filter((l) => /^\|\s*`ODEC-\d\d`\s*\|/.test(l))
      .map((l) => l.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map((c) => c.trim()));
    if (tableRows.length !== odec.register.length) {
      gaps.push(`§6's table lists ${tableRows.length} rows; the register has ${odec.register.length} live questions`);
    }
    const bare = (s) => s.replace(/`/g, '').trim();
    const tableIds = tableRows.map((r) => bare(r[0]));
    for (const [cellId, , cellKind, cellGates] of tableRows) {
      const id = bare(cellId);
      const entry = odec.register.find((e) => e.id === id);
      if (!entry) {
        gaps.push(`§6's table lists ${id}, which is retired in #/retired`);
        continue;
      }
      if (bare(cellKind) !== entry.decidedBy) gaps.push(`§6's table says ${id} is ${bare(cellKind)}; the data says ${entry.decidedBy}`);
      const listed = cellGates.split(',').map(bare).filter(Boolean).sort().join(' ');
      const gatesData = [...entry.blocks.verification, ...entry.blocks.compatibility].sort().join(' ');
      if (listed !== gatesData) gaps.push(`§6's table gates ${id} with [${listed}]; the data says [${gatesData}]`);
    }
    for (const id of oIds) if (!tableIds.includes(id)) gaps.push(`${id} is live in the register and absent from §6's table`);
    const siteDocs = new Set([...deferralSites.keys()].map((k) => k.slice(0, k.lastIndexOf(':'))));
    const gatedBy = (m) => odec.register.filter((e) => e.blocks.verification.length + e.blocks.compatibility.length === m).length;
    const readTally = (label, re, expected) => {
      const found = [...text.matchAll(re)];
      if (!found.length) gaps.push(`${label}: no line in doc 40 states it in numbers`);
      for (const f of found) {
        f.slice(1).forEach((g, i) => {
          if (Number(g) !== expected[i]) gaps.push(`${label}: doc 40 says ${g}, the data says ${expected[i]}`);
        });
      }
    };
    readTally('the Purpose tally', /Purpose: (\d+) deferral sites in (\d+) documents/g, [deferralSites.size, siteDocs.size]);
    readTally('the site-rule tally', /The result is (\d+) sites/g, [deferralSites.size]);
    readTally('the invariant tally', /(\d+) questions, (\d+) sites, one owner per site/g, [odec.register.length, deferralSites.size]);
    readTally('the `product` tally', /(\d+) of the (\d+) are `product`/g, [byKind('product'), odec.register.length]);
    readTally('the `contract` tally', /(\d+) `contract` questions look different/g, [byKind('contract')]);
    readTally('the gating tally', /(\d+) entries gate exactly (\d+) and (\d+) gate (\d+)/g,
      [gatedBy(2), 2, gatedBy(1), 1]);
    check(
      gaps.length === 0,
      'doc 40 reads aloud the register tallies the data has, and this check reads them out of the document',
      gaps.join('; ') ||
        `${kindsIn.length} kinds, ${odec.register.length} questions, ${deferralSites.size} sites in ${siteDocs.size} documents agree`,
    );

    // §9 of doc 40 cites its labels in backticks, where tools/check-doc-evidence.mjs cannot see them, so a
    // question answered in the register would otherwise leave this run pointing at an assertion that stopped
    // existing -- the same rot doc 39 §11 refuses, one document down.
    const sec9 = text.split(/^## 9\. /m)[1]?.split(/^## 10\./m)[0] ?? '';
    const citedLabels = [...sec9.matchAll(/`([^`]{20,})`/g)].map((m) => m[1]);
    const staleCites = citedLabels.filter((c) => !labels.has(c));
    check(
      citedLabels.length >= 12 && staleCites.length === 0,
      'every mechanism doc 40 names is a check this run actually makes',
      `cited=${citedLabels.length} not an assertion label: ${staleCites.join(' | ') || 'none'}`,
    );
  } else {
    note('DEFERRED: 40-open-decisions.md cross-check (file not present yet)');
  }
}


// ---------------------------------------------------------------- report

console.log(`checks run: ${checks}`);
for (const n of notes) console.log(`  note: ${n}`);

if (failures.length > 0) {
  console.error(`\nFAILED ${failures.length} check(s):`);
  for (const f of failures) console.error(`  X ${f.label}${f.detail ? `\n      ${f.detail}` : ''}`);
  process.exit(1);
}

console.log(
  `OK: ${schemaFiles.length} schemas compile, ${commandFiles.length} command docs conform, ` +
    `${validCount} examples valid, ${rejectedCount} invalid examples rejected, ` +
    `${frameCount} protocol frames replayed.`,
);
