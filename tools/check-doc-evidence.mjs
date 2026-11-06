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
//
// Four more passes have joined that first test as the corpus grew each other kind of
// citation, and each refuses what the passes above it cannot see:
//   POINTER   a `file.json#/a/b` pointer must resolve inside a file that parses
//   DOCNAME   a document or script named in prose must exist
//   LABEL     a harness assertion quoted by its label must be a check the harness makes
//   CLAIMS    a `| Cited by |` row must cite a line that names the document whose index it is

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
// Cite these by section (`docs/PROJECT-STATE.md` §6) or by named assertion, never by line.
// `packages/schema/scripts/verify.mjs` joined on 2026-11-06, when the last of the 52 line cites it carried
// were converted to the assertion labels PROJECT-STATE §6 #8(b) prescribed; the label half of the rule is
// checked below by STALE LABEL, which reads the harness and refuses a citation that names no check.
// `docs/audit/99-baseline-report.md` joins on 2026-11-05 evidence: it is the file every
// probe round appends to, and a row added to its §2 table moved three citations in
// `docs/architecture/20-target-repository-layout.md` that addressed §6 by line number.
const MUTABLE_CITATION_TARGETS = ['docs/PROJECT-STATE.md', 'docs/audit/99-baseline-report.md', 'packages/schema/scripts/verify.mjs'];

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

// A `| Cited by |` table is the corpus's own index of the questions other documents asked this
// one. Its first column is a line cite, and every check above proves about that cite is only
// that the line still has text on it -- so a paragraph inserted above a deferral leaves the row
// green while pointing at its neighbour. That is the class §4.15 named and §4.31 measured: the
// six tables hold 79 rows, 57 of them carry a `path:line` cite, and 21 of the 52 outside
// `docs/adr/` pointed at a neighbour on 2026-11-07. All 21 were green.
//
// The tables state their own law, and this mode reads them by it: a row claims that the line it
// cites asks this document a question, and a line that asks a question NAMES this document. So
// the cited line, or one of the two lines around it (a sentence wraps), must carry this file's
// basename. Targets under `docs/adr/` are exempt -- an accepted ADR is a record of a decision,
// and it is not edited to keep somebody else's index honest. Four of the five ADR rows name the
// host anyway; the exemption is load-bearing for one only, doc 35's `0015` row, which states a
// fact the ADR carries rather than a question the ADR asked, and says so in the table.
const CLAIMS_HEADER_RE = /^\|\s*Cited by\s*\|/;
const CLAIMS_ROW_RE = /^\|/;
// `docs/audit/**` is a baseline record read by section, never by row index, and
// `docs/PROJECT-STATE.md` is on the mutable-target list above: a line cite out of either is
// already refused, so a table there could not be claims-checked anyway.
const CLAIMS_HOST_EXEMPT = ['docs/PROJECT-STATE.md', 'docs/audit/'];
const CLAIMS_TARGET_EXEMPT = ['docs/adr/'];

const linesCache = new Map();
async function citedLines(rel) {
  if (linesCache.has(rel)) return linesCache.get(rel);
  let l = null;
  try {
    l = (await readFile(join(REPO, rel), 'utf8')).split('\n');
  } catch {
    l = null;
  }
  linesCache.set(rel, l);
  return l;
}

// Every `path:line` cite in a `Cited by` row, with the row's own claim cell for the message.
function claimsRows(src, startIdx) {
  // Returns [rowIndex, citePath, citeLine] for the table whose header is at startIdx.
  const out = [];
  for (let i = startIdx + 2; i < src.length; i++) {
    if (!CLAIMS_ROW_RE.test(src[i])) break;
    if (/^\|[-:\s|]+\|\s*$/.test(src[i])) continue;
    const cells = src[i].split('|');
    for (const m of cells[1].matchAll(CITATION_RE)) {
      if (!looksLikePath(m[1]) || !m[1].endsWith('.md')) continue;
      out.push([i, m[1], Number(m[2]), cells[2] ? cells[2].trim() : '']);
    }
  }
  return out;
}

// The claim itself, split from the filesystem so `--selftest` can drive it with fixtures and prove
// the check can say NO. Returns {checked, stale}.
async function claimsProblems(label, src, readTarget) {
  const host = label.split('/').pop();
  const stale = [];
  let checked = 0;
  for (let i = 0; i < src.length; i++) {
    if (!CLAIMS_HEADER_RE.test(src[i])) continue;
    for (const [row, citePath, citeLine, claim] of claimsRows(src, i)) {
      if (CLAIMS_TARGET_EXEMPT.some((p) => citePath.startsWith(p))) continue;
      const tl = await readTarget(citePath);
      if (!tl) continue; // already reported as UNRESOLVED by the citation scan
      checked++;
      const window = [tl[citeLine - 2], tl[citeLine - 1], tl[citeLine]].filter(Boolean);
      if (window.some((x) => x.includes(host))) continue;
      const nearby = tl
        .map((x, k) => (x.includes(host) ? k + 1 : 0))
        .filter(Boolean)
        .slice(0, 4)
        .join(', ');
      stale.push([label, row + 1, citePath, citeLine, claim, (tl[citeLine - 1] || '').trim().slice(0, 100), nearby]);
    }
  }
  return { checked, stale };
}

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

// The other half of the harness rule. A citation into `packages/schema/scripts/verify.mjs` names the assertion it
// depends on -- the file token, then one or more double-quoted labels exactly as the harness carries them, template
// placeholders included. Line numbers move when a rule is added; a label moves when the *claim* changes, which is the
// only case where the sentence around it is supposed to change too. So every quoted label that follows the token has
// to still be in the file, or the sentence is describing a check that no run makes.
const HARNESS = join(REPO, 'packages', 'schema', 'scripts', 'verify.mjs');
const harnessText = await readFile(HARNESS, 'utf8');
const HARNESS_TOKEN_RE = /`packages\/schema\/scripts\/verify\.mjs`(?:'s)?/g;
// A label is the quoted text that follows the token: whitespace, then a `,`/`and`/`—` between neighbours, and a label
// never crosses a line or a table cell of its own. The leading whitespace is what keeps a citation whose *sentence*
// ends at the token -- `... is checked by `packages/schema/scripts/verify.mjs`" | next cell | ...` -- from reading the
// cell's closing quote as a label.
const LABEL_RUN_RE = /^(?:[ \t\n]+|,[ \t\n]*|(?:and|—)[ \t\n]+)"([^"\n|]{6,200})"/;
const staleLabel = [];
let labelCitesChecked = 0;
const claimsStale = [];
let claimsChecked = 0;

// A green run must be able to say NO. Four fixtures, all of them the shape a real table has:
//   named    -- the cited line names the host
//   wrapped  -- the name is on the line above the cite, inside the one-line wrap allowance
//   drift    -- the cited line is a non-blank neighbour (so the resolution checks stay green)
//               while the naming line sits three lines below: the exact class §4.15 named
//   adr      -- the same drift, into docs/adr/, which the rule deliberately does not reach
const SELFTEST_HOST = [
  '# host',
  '',
  '| Cited by | What it deferred | Answered in |',
  '|---|---|---|',
  '| `docs/architecture/named.md:2` | names it | §1 |',
  '| `docs/architecture/wrapped.md:3` | names it one line up | §2 |',
  '| `docs/architecture/drift.md:2` | drifted to the neighbour | §3 |',
  '| `docs/adr/0001-x.md:2` | an ADR is exempt | §4 |',
];
const SELFTEST_TARGETS = {
  'docs/architecture/named.md': ['', 'the question `docs/architecture/host.md` §1 answers', ''],
  'docs/architecture/wrapped.md': ['', 'nothing here', 'asked of `docs/architecture/host.md`', 'and the sentence continues'],
  'docs/architecture/drift.md': ['', 'a different question entirely', 'nothing to see', 'and here too', 'asked of `docs/architecture/host.md`'],
  'docs/adr/0001-x.md': ['', 'a different question entirely', 'nothing to see', 'and here too', 'asked of `docs/architecture/host.md`'],
};
const selfLines = async (rel) => SELFTEST_TARGETS[rel] || null;

if (process.argv.includes('--selftest')) {
  const { checked, stale } = await claimsProblems('docs/architecture/host.md', SELFTEST_HOST, selfLines);
  const expect = (what, got, want) => {
    if (got !== want) {
      console.error(`SELFTEST FAILED: ${what} -- got ${got}, want ${want}`);
      process.exit(1);
    }
  };
  expect('rows read', checked, 3);
  expect('drift reported', stale.length, 1);
  expect('the drifted row is the one named', stale[0] && stale[0][2], 'docs/architecture/drift.md');
  console.log('SELFTEST OK: 3 rows evaluated and the exempt ADR row skipped, the named and wrapped rows pass, and the one drifted row is reported.');
  process.exit(0);
}

for await (const doc of mdFiles(join(REPO, 'docs'))) {
  const text = await readFile(doc, 'utf8');
  const label = relative(REPO, doc).replace(/\\/g, '/');
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
  for (const match of text.matchAll(HARNESS_TOKEN_RE)) {
    let rest = text.slice(match.index + match[0].length);
    for (;;) {
      const step = LABEL_RUN_RE.exec(rest);
      if (!step || /\n[ \t]*\n/.test(step[0])) break;
      labelCitesChecked++;
      if (!harnessText.includes(step[1])) staleLabel.push([label, step[1]]);
      rest = rest.slice(step.index + step[0].length);
    }
  }

  // CLAIMS: the `Cited by` tables. Walked line by line, because a table is a region of a file and
  // the regex scans above have no notion of the row a citation sits in.
  if (!CLAIMS_HOST_EXEMPT.some((p) => label.startsWith(p) || label === p)) {
    const found = await claimsProblems(label, text.split('\n'), citedLines);
    claimsChecked += found.checked;
    claimsStale.push(...found.stale);
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
  mutableCite.length +
  staleLabel.length +
  claimsStale.length;
console.log(
  `checked ${checked} path:line citations, ${pointerChecked} path#pointer citations, ${docNameChecked} doc-name references, ${labelCitesChecked} harness assertion labels and ${claimsChecked} \`Cited by\` rows in docs/`,
);
if (plannedSeen.size) {
  console.log(`planned, not yet written (${plannedSeen.size}): ${[...plannedSeen].sort().join(', ')}`);
}

if (problems === 0) {
  console.log('OK: every citation resolves to a real, non-blank line, every pointer resolves, every harness label cited is a check the harness has, and every `Cited by` row cites a line that names the document whose table it is.');
  process.exit(0);
}

if (claimsStale.length) {
  console.error(`\nSTALE CLAIM (${claimsStale.length}) -- a \`Cited by\` row cites a line that does not name this document:`);
  for (const [doc, row, citePath, citeLine, claim, at, nearby] of claimsStale) {
    console.error(`  ${doc}:${row}  claims "${claim.slice(0, 70)}"  but cites ${citePath}:${citeLine}`);
    console.error(`      that line says: ${JSON.stringify(at)}`);
    console.error(`      this document is named at: ${nearby || 'nowhere in the file'}`);
  }
}

if (staleLabel.length) {
  console.error(`\nSTALE LABEL (${staleLabel.length}) -- a citation names a harness check that is not in the file:`);
  for (const [doc, quote] of staleLabel) {
    console.error(`  ${doc}  cites "${quote}"  (no such string in packages/schema/scripts/verify.mjs)`);
  }
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
    console.error(`  ${doc}  cites \`${cite}\`  (say \`docs/PROJECT-STATE.md\` §n for the state document and the baseline report; for the harness say \`packages/schema/scripts/verify.mjs\` "the assertion label" instead)`);
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
