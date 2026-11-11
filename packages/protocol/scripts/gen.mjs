// Generates packages/protocol/src/contract.ts from the normative contract data in
// packages/schema/protocol/. Run with `npm --prefix packages/protocol run gen`;
// `scripts/verify.mjs` regenerates and byte-compares, so a drift fails a run instead of living in the file.
//
// Two properties of the envelope schema name their closed set in another contract file rather than carrying an
// `enum`: `method` says "must be a member of protocol/command-allowlist.json" and `error.code` says "the
// authoritative list is protocol/error-taxonomy.json". The projection follows those sentences — that is what
// `substitutions` below is, and it is why this package has no method list of its own.
//
// shortcut: the `allOf` branches add per-frame-type required and forbidden fields, which are projected here, but
// the nested rule for an `event` frame's `progress` object (its `event` name becomes required) is left where the
// schema can enforce it. Upgrade this file when the extension builds frames in TypeScript rather than only reads
// them.
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '..', '..', '..');
const protocolDir = join(repoRoot, 'packages', 'schema', 'protocol');
const outFile = resolve(here, '..', 'src', 'contract.ts');

const read = (name) => JSON.parse(readFileSync(join(protocolDir, name), 'utf8'));
const framing = read('framing.json');
const envelopeSchema = read('envelope.schema.json');
const taxonomy = read('error-taxonomy.json');
const allowlist = read('command-allowlist.json');
// The per-method deadline and capability list live in each command document, not in the allowlist. The extension
// owns the deadline (docs/architecture/25-native-messaging-protocol.md §9), so a client that cannot read them
// would have to invent them.
const commandDocs = allowlist.commands.map((entry) => read(join('commands', `${entry.name}.json`)));

const lit = (value) => JSON.stringify(value);

const substitutions = {
  'method': 'MethodName',
  'error.code': 'ErrorCode',
  'type': 'FrameType',
  'chunk.encoding': 'ChunkEncoding',
};

// A JSON Schema node to TypeScript, keyed by the property path so the substitutions above can fire.
function tsType(schema, path) {
  if (substitutions[path]) return substitutions[path];
  if (Array.isArray(schema.enum)) return schema.enum.map(lit).join(' | ');
  if (schema.type === undefined) return 'Json';
  const kinds = Array.isArray(schema.type) ? schema.type : [schema.type];
  return [...new Set(kinds.map(baseType))].join(' | ');
}

function baseType(kind) {
  switch (kind) {
    case 'integer':
    case 'number':
      return 'number';
    case 'string':
      return 'string';
    case 'boolean':
      return 'boolean';
    case 'null':
      return 'null';
    case 'object':
      return 'Json';
    case 'array':
      return 'Json[]';
    default:
      throw new Error(`envelope schema carries a property type this generator was not told about: ${kind}`);
  }
}

// A property whose schema is an inline object with its own `properties` is projected structurally, so `args`,
// `result` and `error` keep the shape the schema declares instead of collapsing to `Json`.
function propertyType(name, schema) {
  if (schema.properties && schema.type !== 'array') {
    const required = new Set(schema.required ?? []);
    const fields = Object.entries(schema.properties).map(
      ([child, childSchema]) => `${child}${required.has(child) ? '' : '?'}: ${propertyType(`${name}.${child}`, childSchema)}`,
    );
    return `{ ${fields.join('; ')} }`;
  }
  return tsType(schema, name);
}

const frameRules = new Map();
for (const branch of envelopeSchema.allOf ?? []) {
  if (!branch.if || !branch.then) {
    throw new Error(`an allOf branch is not an if/then rule: ${JSON.stringify(branch).slice(0, 80)}`);
  }
  const discriminator = branch.if.properties?.type;
  if (!discriminator) throw new Error('an allOf branch does not discriminate on the frame type');
  const names = discriminator.const ? [discriminator.const] : discriminator.enum;
  if (!names) throw new Error('a frame discriminator is neither a const nor an enum');
  const forbidden = forbiddenFields(branch.then.not);
  for (const name of names) {
    const rule = frameRules.get(name) ?? { required: new Set(), forbidden: new Set() };
    for (const field of branch.then.required ?? []) rule.required.add(field);
    for (const field of forbidden) rule.forbidden.add(field);
    frameRules.set(name, rule);
  }
}

function forbiddenFields(negation) {
  if (!negation) return [];
  if (negation.anyOf) return negation.anyOf.flatMap((part) => part.required ?? []);
  return negation.required ?? [];
}

const baseRequired = new Set(envelopeSchema.required);
for (const field of baseRequired) {
  for (const rule of frameRules.values()) rule.required.add(field);
}

const lines = [];
lines.push('// Code generated by packages/protocol/scripts/gen.mjs from packages/schema/protocol/. DO NOT EDIT.');
lines.push('// Regenerate with `npm --prefix packages/protocol run gen`; the drift test in this package');
lines.push('// byte-compares this file against a fresh generation.');
lines.push('');
lines.push(`export const frameFormatVersion = ${framing.frameFormatVersion} as const;`);
lines.push(`export const minPipeVersion = ${framing.minPipeVersion} as const;`);
lines.push(`export const protocolVersion = ${framing.protocolVersion} as const;`);
lines.push(`export const minCompatibleProtocolVersion = ${framing.minCompatibleProtocolVersion} as const;`);
lines.push('');
lines.push(`export const hostName = ${lit(framing.senderPolicy.identity.hostName)} as const;`);
lines.push(`export const reconnectBackoffSeconds = ${lit(framing.reconnect.backoffSeconds)} as const;`);
lines.push(`export const reconnectJitterPercent = ${framing.reconnect.jitterPercent} as const;`);
lines.push('');
lines.push(`export const prefixBytes = ${framing.encoding.lengthPrefix.bytes} as const;`);
lines.push(`export const prefixByteOrder = ${lit(framing.encoding.lengthPrefix.byteOrder)} as const;`);
lines.push(`export const prefixIsUnsigned = ${framing.encoding.lengthPrefix.unsigned} as const;`);
lines.push('');
for (const [name, entry] of Object.entries(framing.limits)) {
  lines.push(`export const ${name} = ${entry.value} as const;`);
}
lines.push('');
lines.push('export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };');
lines.push('');
lines.push(`export type FrameType = ${(envelopeSchema.properties.type.enum).map(lit).join(' | ')};`);
lines.push(`export type ChunkEncoding = ${(envelopeSchema.properties.chunk.properties.encoding.enum).map(lit).join(' | ')};`);
lines.push('');
lines.push('export const errorCodes = [');
lines.push(taxonomy.codes.map((entry) => `  ${lit(entry.code)},`).join('\n'));
lines.push('] as const;');
lines.push('export type ErrorCode = (typeof errorCodes)[number];');
lines.push('');
lines.push('export const retryableErrors: Readonly<Record<ErrorCode, boolean>> = {');
lines.push(taxonomy.codes.map((entry) => `  ${lit(entry.code)}: ${entry.retryable},`).join('\n'));
lines.push('};');
lines.push('');
lines.push('export const methods = [');
lines.push(allowlist.commands.map((entry) => `  ${lit(entry.name)},`).join('\n'));
lines.push('] as const;');
lines.push('export type MethodName = (typeof methods)[number];');
lines.push('');
lines.push(`export const deniedByDefault = ${allowlist.deniedByDefault} as const;`);
lines.push(`export const unknownMethodBehavior = ${lit(allowlist.unknownMethodBehavior)} as const;`);
lines.push('export const availableBeforeHello: readonly MethodName[] = [');
lines.push(
  allowlist.commands
    .filter((entry) => entry.availableBeforeHello)
    .map((entry) => `  ${lit(entry.name)},`)
    .join('\n'),
);
lines.push('];');
lines.push('');
lines.push('// The deadline and the capability gate of every method, from its own command document. A client holds');
lines.push('// neither list of its own: docs/architecture/20-target-repository-layout.md §3 forbids it.');
lines.push('export const methodTimeoutMs: Readonly<Record<MethodName, number>> = {');
lines.push(commandDocs.map((doc) => `  ${lit(doc.name)}: ${doc.timeoutMs},`).join('\n'));
lines.push('};');
lines.push('');
lines.push('export const methodCapabilities: Readonly<Record<MethodName, readonly string[]>> = {');
lines.push(commandDocs.map((doc) => `  ${lit(doc.name)}: ${lit(doc.capabilities ?? [])},`).join('\n'));
lines.push('};');
lines.push('');
lines.push('// The dispatch skeleton: a host table is not well-typed until every allowlisted method has a handler,');
lines.push('// which is the type-level form of `deniedByDefault` in protocol/command-allowlist.json.');
lines.push('export type Handler = (frame: Extract<Envelope, { type: \'request\' }>) => Promise<Json>;');
lines.push('export type DispatchTable = { [M in MethodName]: Handler };');
lines.push('');
lines.push('// A frame whose `type` is not yet known, as it arrives off the pipe.');
lines.push('export interface RawEnvelope {');
for (const [name, schema] of Object.entries(envelopeSchema.properties)) {
  lines.push(`  ${name}${baseRequired.has(name) ? '' : '?'}: ${name === 'type' ? 'FrameType' : propertyType(name, schema)};`);
}
lines.push('}');
lines.push('');
for (const [name, rule] of frameRules) {
  const Pascal = name.charAt(0).toUpperCase() + name.slice(1);
  lines.push(`export interface Envelope${Pascal} {`);
  for (const [field, schema] of Object.entries(envelopeSchema.properties)) {
    if (rule.forbidden.has(field)) continue;
    const required = rule.required.has(field);
    const type = field === 'type' ? lit(name) : propertyType(field, schema);
    lines.push(`  ${field}${required ? '' : '?'}: ${type};`);
  }
  lines.push('}');
  lines.push('');
}
lines.push('export type Envelope =');
lines.push([...frameRules.keys()].map((name) => `  | Envelope${name.charAt(0).toUpperCase()}${name.slice(1)}`).join('\n'));
lines.push('');

const text = lines.join('\n');
if (process.argv.includes('--stdout')) {
  // The drift check reads the projection without letting it touch the committed file.
  process.stdout.write(text);
} else {
  mkdirSync(dirname(outFile), { recursive: true });
  writeFileSync(outFile, text, 'utf8');
  console.log(`generated ${outFile}`);
}
