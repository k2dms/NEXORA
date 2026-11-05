#!/usr/bin/env node
// Makes the audit falsifiable: every `path:line` citation in docs/ must still
// point at a real line in the current tree. A citation that drifts is worse than
// no citation, because the reader has no way to tell.
//
// Two failure kinds:
//   out-of-range  the file exists but the cited line does not (the claim went stale)
//   unresolved    the file does not exist at all
// Paths under FUTURE_PREFIX are exempt from "unresolved" only: they are target
// layout described in docs/architecture/20-target-repository-layout.md and does
// not exist yet. An out-of-range citation is never excused.

import { readFile, readdir, stat } from 'node:fs/promises';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), '..');

const FUTURE_PREFIX = [
  'nexora/',
  'packages/protocol/',
  'packages/state/',
  'packages/i18n/',
  'packages/ui/',
  'docs/audit/99-baseline-report.md',
  'docs/verification/backup-from-3.5.2.json',
];

// Build output and vendored trees are derived, not tracked.
const DERIVED_PREFIX = ['build/', 'tmp/', 'dist/', 'node_modules/', 'package-lock.json'];

// The architecture series is written across sessions. A doc that is planned and named
// in the layout spec is excused from "exists" — but only by exact filename, and the list
// shrinks as each document is written. It is printed on every run so a stale excuse
// cannot hide there. The series is now complete: this set is empty, and a doc added here
// is a document that has been named but not written.
const PLANNED_DOCS = new Set([]);

const CITATION_RE = /`([^`\s]+?):(\d+)(?:-(\d+))?`/g;

// Files that are rewritten in place, by hand, every session. A line number into one of
// them is correct only until the next paragraph is inserted above it, which is how eight
// citations into the state document came to point at the wrong decisions in one sitting.
// Cite these by section (`docs/PROJECT-STATE.md` §6), never by line. `packages/schema/
// scripts/verify.mjs` belongs here too and joins when its 44 remaining line cites are
// converted to assertion labels, in the one commit PROJECT-STATE §6 #8 already promises.
// `docs/audit/99-baseline-report.md` joins on 2026-11-05 evidence: it is the file every
// probe round appends to, and a row added to its §2 table moved three citations in
// `docs/architecture/20-target-repository-layout.md` that addressed §6 by line number.
const MUTABLE_CITATION_TARGETS = ['docs/PROJECT-STATE.md', 'docs/audit/99-baseline-report.md'];

// A JSON Pointer citation is the stable half of the contract layer: `x.schema.json#/a/b`
// survives every edit that would silently invalidate `x.schema.json:14`. It is therefore
// checked as strictly as a line number: the file must parse and the pointer must resolve.
const POINTER_RE = /`([^`\s#]+?)#([A-Za-z0-9_$\-~%.\/]*)`/g;

// Documents cite each other by name, with no line number to break. That is the one
// citation kind this tool did not check, and the first typo it missed was real:
// `docs/audit/05-storage-sync-migration.md` for a file named
// `05-storage-sync-and-migration.md`. A wrong doc name reads exactly like a right one.
// The same holds for the gate scripts the documents name as runnable commands -- a
// `tools/check-*.mjs` that does not exist is a README that cannot be followed -- so
// `.mjs` joins `.md` here.
// `tools/*.mjs` is a glob in a layout table, not a file, so `*` is outside the path class.
const DOCNAME_RE = /`((?:docs|tools|packages)\/[^`\s:#*][^`\s:#*]*?\.(?:md|mjs))(?:[#:]?\d*)`/g;

async function* mdFiles(dir) {
  let entries;
  try {
    entries = await readdir(dir, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      yield* mdFiles(path);
    } else if (entry.name.endsWith('.md')) {
      yield path;
    }
  }
}

const lineCountCache = new Map();
async function lineCount(abs) {
  if (lineCountCache.has(abs)) return lineCountCache.get(abs);
  let count = null;
  try {
    const text = await readFile(abs, 'utf8');
    count = text.split('\n').length;
  } catch {
    count = null;
  }
  lineCountCache.set(abs, count);
  return count;
}

function looksLikePath(raw) {
  if (/^[a-z]+:\/\//.test(raw)) return false; // http(s):, git@
  if (/[*<>|]/.test(raw)) return false; // globs and placeholders
  if (!/\./.test(raw)) return false; // extension-less is prose, not a file
  if (raw.startsWith('.')) return false; // relative-to-the-doc, not repo root
  return true;
}

const outOfRange = [];
const unresolved = [];
const blankTarget = [];
const selfCite = [];
const mutableCite = [];
let checked = 0;
const pointerUnresolved = [];
let pointerChecked = 0;
const missingDocs = [];
const plannedSeen = new Set();
let docNameChecked = 0;

for await (const doc of mdFiles(join(REPO, 'docs'))) {
  const text = await readFile(doc, 'utf8');
  const label = relative(REPO, doc);
  for (const match of text.matchAll(CITATION_RE)) {
    const [, raw, from, to] = match;
    if (!looksLikePath(raw)) continue;
    checked++;
    // This file is the one mutable target a citation can point at whose line numbers move
    // every time a citation kind is added, so a line cite into it is stale by construction.
    // Cite the named constant instead: FUTURE_PREFIX, DERIVED_PREFIX, PLANNED_DOCS,
    // CITATION_RE, POINTER_RE, blankTarget. Six such cites were converted on 2026-11-04
    // after `:38` had spent a whole session pointing at a planned-docs entry while claiming
    // to cite the pointer resolver.
    if (raw === 'tools/check-doc-evidence.mjs') {
      selfCite.push([label, match[0]]);
      continue;
    }
    if (MUTABLE_CITATION_TARGETS.includes(raw)) {
      mutableCite.push([label, match[0]]);
      continue;
    }
    const last = to ? Number(to) : Number(from);
    const abs = join(REPO, raw);
    const present = await stat(abs).then(
      () => true,
      () => false,
    );
    if (!present) {
      const excused =
        FUTURE_PREFIX.some((p) => raw.startsWith(p)) ||
        DERIVED_PREFIX.some((p) => raw.startsWith(p));
      if (!excused) unresolved.push([label, raw, match[0]]);
      continue;
    }
    const count = await lineCount(abs);
    if (count !== null && last > count) {
      outOfRange.push([label, raw, match[0], `file has ${count} lines`]);
      continue;
    }
    // A citation that lands on a blank line is the signature of a line number that
    // was guessed rather than read. Range checks alone cannot tell those apart.
    const text = await readFile(abs, 'utf8');
    const target = text.split('\n')[Number(from) - 1] ?? '';
    if (!/[A-Za-z0-9]/.test(target)) {
      blankTarget.push([label, raw, match[0]]);
    }
  }
  for (const match of text.matchAll(POINTER_RE)) {
    const [, raw, pointer] = match;
    if (!pointer || !looksLikePath(raw)) continue;
    if (!raw.endsWith('.json')) continue; // .md anchors are prose-level navigation
    pointerChecked++;
    const candidates = [join(REPO, raw), join(REPO, 'packages', 'schema', raw)];
    let abs = null;
    for (const c of candidates) {
      if (await stat(c).then(() => true, () => false)) { abs = c; break; }
    }
    if (!abs) {
      if (!FUTURE_PREFIX.some((p) => raw.startsWith(p))) {
        pointerUnresolved.push([label, raw, match[0], 'file not found']);
      }
      continue;
    }
    let doc2;
    try {
      doc2 = JSON.parse(await readFile(abs, 'utf8'));
    } catch (err) {
      pointerUnresolved.push([label, raw, match[0], `not parseable JSON: ${err.message}`]);
      continue;
    }
    const tokens = decodeURIComponent(pointer)
      .split('/')
      .filter((t) => t !== '')
      .map((t) => t.replace(/~1/g, '/').replace(/~0/g, '~'));
    let node = doc2;
    let walked = '';
    for (const token of tokens) {
      if (node !== null && typeof node === 'object' && Object.hasOwn(node, token)) {
        node = node[token];
        walked += `/${token}`;
      } else {
        pointerUnresolved.push([label, raw, match[0], `stops at "${walked}/${token}"`]);
        node = undefined;
        break;
      }
    }
  }
  for (const match of text.matchAll(DOCNAME_RE)) {
    const raw = match[1];
    docNameChecked++;
    const abs = join(REPO, raw);
    if (await stat(abs).then(() => true, () => false)) continue;
    if (PLANNED_DOCS.has(raw)) {
      plannedSeen.add(raw);
      continue;
    }
    missingDocs.push([label, raw]);
  }
}

const pointerProblems = pointerUnresolved.length;
const problems =
  outOfRange.length +
  unresolved.length +
  blankTarget.length +
  pointerProblems +
  missingDocs.length +
  selfCite.length +
  mutableCite.length;
console.log(
  `checked ${checked} path:line citations, ${pointerChecked} path#pointer citations and ${docNameChecked} doc-name references in docs/`,
);
if (plannedSeen.size) {
  console.log(`planned, not yet written (${plannedSeen.size}): ${[...plannedSeen].sort().join(', ')}`);
}

if (problems === 0) {
  console.log('OK: every citation resolves to a real, non-blank line and every pointer resolves.');
  process.exit(0);
}

if (missingDocs.length) {
  console.error(`\nMISSING DOC (${missingDocs.length}) -- a document is cited by a name that is not in the tree and not in PLANNED_DOCS:`);
  for (const [doc, cite] of missingDocs) {
    console.error(`  ${doc}  cites \`${cite}\``);
  }
}

if (selfCite.length) {
  console.error(`\nSELF CITATION (${selfCite.length}) -- this file's line numbers move whenever a rule is added:`);
  for (const [doc, cite] of selfCite) {
    console.error(`  ${doc}  cites \`${cite}\`  (say CITATION_RE, POINTER_RE, FUTURE_PREFIX, DERIVED_PREFIX, PLANNED_DOCS or blankTarget instead)`);
  }
}

if (mutableCite.length) {
  console.error(`\nMUTABLE TARGET (${mutableCite.length}) -- a line cite into a file rewritten in place every session:`);
  for (const [doc, cite] of mutableCite) {
    console.error(`  ${doc}  cites \`${cite}\`  (say \`docs/PROJECT-STATE.md\` §n instead)`);
  }
}

if (pointerUnresolved.length) {
  console.error(`\nBAD POINTER (${pointerUnresolved.length}) -- the cited JSON location does not exist:`);
  for (const [doc, file, cite, why] of pointerUnresolved) {
    console.error(`  ${doc}  cites \`${cite}\`  (${file} ${why})`);
  }
}

if (blankTarget.length) {
  console.error(`\nBLANK TARGET (${blankTarget.length}) -- the cited line has no code on it:`);
  for (const [doc, file, cite] of blankTarget) {
    console.error(`  ${doc}  cites \`${cite}\``);
  }
}

if (outOfRange.length) {
  console.error(`\nOUT OF RANGE (${outOfRange.length}) -- the cited line no longer exists:`);
  for (const [doc, file, cite, why] of outOfRange) {
    console.error(`  ${doc}  cites \`${cite}\`  (${file} ${why})`);
  }
}
if (unresolved.length) {
  console.error(`\nUNRESOLVED (${unresolved.length}) -- cited file is not in the tree:`);
  for (const [doc, file, cite] of unresolved) {
    console.error(`  ${doc}  cites \`${cite}\``);
  }
}
process.exit(1);
