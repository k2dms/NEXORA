#!/usr/bin/env node
// Asserts that every vendor asset referenced by the extension sources is present
// on disk before packaging. Without this, a missing file under lib/ surfaces only
// as a blank options page in a browser, and a broken Bower fetch surfaces as a
// build that silently ships an incomplete page.
//
// References are emitted by the page sources as `lib/<component>/...` and are
// resolved against either committed seed assets (omega-web/lib/) or the Bower
// output that grunt's `copy.lib` task writes (omega-web/build/lib/). The
// Chromium package ships the resolved path unchanged, so a hit in either root
// means the extension can load the asset.
//
// Dynamic references (runtime string concatenation, e.g. the angular-i18n locale
// prefix) cannot be checked by path and are reported separately, not as errors.

import { readFile, readdir, stat } from 'node:fs/promises';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const VENDOR_ROOTS = [
  join(REPO, 'omega-web', 'lib'),
  join(REPO, 'omega-web', 'build', 'lib'),
];

// `omega-web` also contains build/, bower_components/ and lib/ themselves; those
// are generated or are the assets under test, so they are pruned, not scanned.
const SCAN_ROOTS = ['omega-web', 'omega-target-chromium-extension'];
const SKIP_DIRS = new Set([
  'node_modules',
  'bower_components',
  'build',
  'dist',
  'tmp',
  'lib',
  'images',
  'themes',
]);
const SCAN_EXT = new Set(['.jade', '.html', '.coffee', '.js']);
const REF_RE = /(?:\.\/)?lib\/[A-Za-z0-9._@/-]+/g;

async function* walk(dir) {
  let entries;
  try {
    entries = await readdir(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    if (entry.name.startsWith('.') || SKIP_DIRS.has(entry.name)) continue;
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      yield* walk(path);
    } else if (SCAN_EXT.has(extOf(entry.name))) {
      yield path;
    }
  }
}

function extOf(name) {
  const dot = name.lastIndexOf('.');
  return dot === -1 ? '' : name.slice(dot);
}

// A reference is checkable only when it names a complete file.
function isCheckable(ref) {
  if (/[{*]$/.test(ref) || ref.endsWith('_')) return false;
  return /\.[a-z0-9]{2,5}$/i.test(ref);
}

async function exists(path) {
  try {
    await stat(path);
    return true;
  } catch {
    return false;
  }
}

const refs = new Map();
let filesScanned = 0;

for (const root of SCAN_ROOTS) {
  for await (const file of walk(join(REPO, root))) {
    filesScanned++;
    const text = await readFile(file, 'utf8');
    for (const match of text.match(REF_RE) ?? []) {
      const ref = match.replace(/^\.\//, '');
      if (!refs.has(ref)) refs.set(ref, []);
      refs.get(ref).push(relative(REPO, file));
    }
  }
}

const missing = [];
const dynamic = [];
const resolved = [];

async function resolveRef(ref) {
  const rel = ref.slice('lib/'.length);
  for (const root of VENDOR_ROOTS) {
    if (await exists(join(root, rel))) return relative(REPO, join(root, rel));
  }
  return null;
}

for (const [ref, sources] of [...refs].sort()) {
  if (!isCheckable(ref)) {
    dynamic.push([ref, sources]);
    continue;
  }
  const found = await resolveRef(ref);
  if (found) {
    resolved.push([ref, found]);
  } else {
    missing.push([ref, sources]);
  }
}

console.log(`scanned ${filesScanned} files, ${refs.size} distinct lib/ references`);
console.log(`  resolved: ${resolved.length}`);
for (const [ref, found] of resolved) {
  console.log(`    + ${ref} -> ${found}`);
}
console.log(`  dynamic (not path-checkable): ${dynamic.length}`);
for (const [ref, sources] of dynamic) {
  console.log(`    ~ ${ref}  <- ${sources.join(', ')}`);
}

if (missing.length > 0) {
  console.error(`\nMISSING ${missing.length} vendored reference(s):`);
  for (const [ref, sources] of missing) {
    console.error(`  X ${ref}`);
    console.error(`      referenced by: ${sources.join(', ')}`);
    console.error(
      `      expected under: ${VENDOR_ROOTS.map((r) => relative(REPO, r)).join(' or ')}`,
    );
  }
  console.error(
    '\nRun `npm run deps` in omega-build (it invokes `grunt bower` for omega-web) ' +
      'or populate omega-web/lib/ from a machine where Bower still works. See ' +
      'docs/audit/02-toolchain-decay-and-risk.md.',
  );
  process.exit(1);
}

console.log('\nOK: every statically referenced vendor asset is present.');
