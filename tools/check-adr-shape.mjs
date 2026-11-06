#!/usr/bin/env node
// ADR-0001 says the decision log is checkable: rules 1, 2 and 3 fix how a record is named, numbered and
// sectioned, rule 4 fixes the status vocabulary, rule 11 bounds the length, and rule 10 says the index in
// docs/adr/README.md "MUST be treated as a checkable artifact, not a convenience". Nothing checked any of it
// (docs/architecture/39-verification-matrix.md `VER-01`), so a decision could be indexed under a number that
// was not its own, or shortened until its alternatives disappeared, and the only way to notice was to read
// twenty files.
//
// What this refuses
//   1. a file in `docs/adr/` that is not `NNNN-<kebab-slug>.md`, a number used twice, or a hole in the
//      sequence -- rule 2 is append-only, so the numbers present must be exactly 0001..N (README.md is the
//      index, not a record, and is the only other name allowed in the directory).
//   2. a record without exactly one `# ` heading, or one whose heading is not `# ADR-NNNN:` with NNNN equal to
//      the number in its own filename -- two headings means two decisions, and a heading that disagrees with
//      the filename means the id has changed meaning.
//   3. a section sequence that is not exactly `## Context`, `## Decision`, `## Consequences`,
//      `## Alternatives considered`, `## Evidence`, `## Verification` -- in that order, and no other `## `
//      sections (rule 3 forbids the extras as firmly as it forbids the missing ones).
//   4. a `Status:` or `Date:` line that is absent, after the first section, malformed, or outside rule 4's
//      vocabulary; and a `Superseded by ADR-NNNN` that points at a record this directory does not hold.
//   5. a record outside 60..140 lines (rule 11).
//   6. an index whose rows are not exactly the records present: same numbers, same filenames, same Status and
//      Date cells as the files themselves (rule 10).
//
// The bounds in rule 5 are the ones a checker can enforce and a writer can satisfy honestly: a 141-line record
// is one that needs splitting, not one that gets truncated to pass.
//
// `--selftest` runs each rule against a real record with one defect injected, so CI proves the guard bites
// rather than merely executing -- the same standard `tools/check-no-secrets.mjs` is held to.

import { readFile, readdir } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const ADR_DIR = join(REPO, 'docs', 'adr');
const INDEX_FILE = 'docs/adr/README.md';

const RECORD_RE = /^(\d{4})-[a-z0-9]+(-[a-z0-9]+)*\.md$/;
const SECTIONS = ['Context', 'Decision', 'Consequences', 'Alternatives considered', 'Evidence', 'Verification'];
const STATUS_RE = /^(Accepted|Proposed|Superseded by ADR-(\d{4}))$/;
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
const LINE_MIN = 60;
const LINE_MAX = 140;

function recordShape(name, text) {
  const bad = [];
  const num = name.slice(0, 4);

  const h1 = text.split('\n').filter((l) => l.startsWith('# '));
  if (h1.length !== 1) {
    bad.push({ rule: 'one-heading', detail: `${h1.length} \`# \` headings; rule 3 has room for exactly one` });
  } else if (!h1[0].startsWith(`# ADR-${num}:`)) {
    bad.push({ rule: 'heading-number', detail: `heading says "${h1[0].slice(2, 12)}", filename says ADR-${num}` });
  }

  const got = text.split('\n').filter((l) => l.startsWith('## ')).map((l) => l.slice(3));
  const extra = got.filter((s) => !SECTIONS.includes(s));
  const missing = SECTIONS.filter((s) => !got.includes(s));
  if (extra.length > 0 || missing.length > 0) {
    bad.push({
      rule: 'sections',
      detail:
        (missing.length > 0 ? `missing ${missing.join(', ')}; ` : '') +
        (extra.length > 0 ? `not in rule 3's sequence: ${extra.join(', ')}` : ''),
    });
  } else if (got.join('|') !== SECTIONS.join('|')) {
    bad.push({ rule: 'section-order', detail: `present but ordered ${got.join(', ')}` });
  }

  const lines = text.split('\n');
  const firstSection = lines.findIndex((l) => l.startsWith('## '));
  const head = firstSection < 0 ? lines : lines.slice(0, firstSection);
  const status = head.map((l) => (l.match(/^Status:\s*(.*?)\s*$/) || [])[1]).find((s) => s !== undefined);
  const date = head.map((l) => (l.match(/^Date:\s*(.*?)\s*$/) || [])[1]).find((s) => s !== undefined);
  if (status === undefined) bad.push({ rule: 'status', detail: 'no `Status:` line above the first `## ` section' });
  else if (!STATUS_RE.test(status)) bad.push({ rule: 'status', detail: `"${status}" is outside rule 4's vocabulary` });
  if (date === undefined) bad.push({ rule: 'date', detail: 'no `Date:` line above the first `## ` section' });
  else if (!DATE_RE.test(date)) bad.push({ rule: 'date', detail: `"${date}" is not YYYY-MM-DD` });

  const count = lines[lines.length - 1] === '' ? lines.length - 1 : lines.length;
  if (count < LINE_MIN || count > LINE_MAX) {
    bad.push({ rule: 'length', detail: `${count} lines; rule 11 allows ${LINE_MIN}-${LINE_MAX}` });
  }

  return { num, status, date, count, bad };
}

function checkIndex(rows, records) {
  const bad = [];
  const listed = new Set(rows.map((r) => r.num));
  const held = new Set(records.map((r) => r.num));

  for (const r of records) {
    if (!listed.has(r.num)) bad.push({ rule: 'index-row', detail: `${r.name} is present but not indexed` });
  }
  for (const row of rows) {
    const rec = records.find((r) => r.num === row.num);
    if (!rec) {
      bad.push({ rule: 'index-row', detail: `index row ${row.num} names no record in this directory` });
      continue;
    }
    if (row.file !== rec.name) {
      bad.push({ rule: 'index-link', detail: `row ${row.num} links ${row.file}, the file is ${rec.name}` });
    }
    if (row.status !== rec.status) {
      bad.push({ rule: 'index-status', detail: `row ${row.num} says "${row.status}", ${rec.name} says "${rec.status}"` });
    }
    if (row.date !== rec.date) {
      bad.push({ rule: 'index-date', detail: `row ${row.num} says ${row.date}, ${rec.name} says ${rec.date}` });
    }
  }
  return bad;
}

const parseIndex = (text) => {
  const rows = [];
  let inSection = false;
  for (const line of text.split('\n')) {
    if (/^## /.test(line)) {
      inSection = /^## Index\s*$/.test(line);
      continue;
    }
    if (!inSection) continue;
    const m = line.match(
      /^\|\s*(\d{4})\s*\|\s*\[[^\]]*\]\(([^)]+\.md)\)\s*\|\s*([^|]*?)\s*\|\s*([^|]*?)\s*\|/,
    );
    if (m) rows.push({ num: m[1], file: m[2], status: m[3], date: m[4] });
  }
  return rows;
};

// A record that satisfies every rule, plus one injected defect: the positives the gate must refuse. Padded to
// 70 lines so the length rule has room on both sides of the bound.
const GOOD_BODY = [
  '# ADR-0001: A title long enough to be a real decision',
  '',
  'Status: Accepted',
  'Date: 2026-11-04',
  '',
  '## Context',
  '',
  'Measurement.',
  '',
  '## Decision',
  '',
  '1. The rule, stated normatively.',
  '',
  '## Consequences',
  '',
  'Consequence.',
  '',
  '## Alternatives considered',
  '',
  '- A real alternative, and why it was rejected.',
  '',
  '## Evidence',
  '',
  '- `docs/adr/README.md` -- VERIFIED -- the index this record must appear in.',
  '',
  '## Verification',
  '',
  'Verified: NO -- deferred to Phase 2.',
  '',
];
const GOOD_RECORD = GOOD_BODY.concat(
  Array.from({ length: 70 - GOOD_BODY.length }, (_, i) => `Filler line ${i}.`),
  [''],
);

function runSelftest() {
  const cases = [
    { name: '0001-good.md', text: GOOD_RECORD.join('\n'), want: null },
    { name: '0001-good.md', text: GOOD_RECORD.join('\n').replace('Status: Accepted', 'Status: Superseded by ADR-0002'), want: null },
    { name: '0001-good.md', text: GOOD_RECORD.join('\n').replace('Status: Accepted', 'Status: Retired'), want: 'status' },
    {
      name: '0001-good.md',
      text: GOOD_RECORD.join('\n').replace('# ADR-0001:', '# ADR-0007:'),
      want: 'heading-number',
    },
    { name: '0001-good.md', text: GOOD_RECORD.join('\n').replace('## Evidence', '# Evidence'), want: 'one-heading' },
    {
      name: '0001-good.md',
      text: GOOD_RECORD.join('\n').replace('## Consequences', '## Fallout'),
      want: 'sections',
    },
    {
      name: '0001-good.md',
      text: GOOD_RECORD.join('\n')
        .replace('## Decision', '@@')
        .replace('## Context', '## Decision')
        .replace('@@', '## Context'),
      want: 'section-order',
    },
    { name: '0001-good.md', text: GOOD_RECORD.join('\n').replace('Date: 2026-11-04', 'Date: 4 Nov 2026'), want: 'date' },
    { name: '0001-good.md', text: GOOD_RECORD.slice(0, 40).join('\n'), want: 'length' },
    { name: '0001-good.md', text: GOOD_RECORD.concat(['extra line']).join('\n'), want: null },
  ];

  const failures = [];
  for (const { name, text, want } of cases) {
    const got = recordShape(name, text).bad.map((b) => b.rule).find((r) => r === want) ?? null;
    if (want === null) {
      if (got !== null || recordShape(name, text).bad.length > 0) {
        failures.push(`  want clean              got ${recordShape(name, text).bad.map((b) => b.rule).join(',')}  ${name}`);
      }
    } else if (!recordShape(name, text).bad.some((b) => b.rule === want)) {
      failures.push(`  want ${String(want).padEnd(18)}  got ${recordShape(name, text).bad.map((b) => b.rule).join(',') || 'clean'}  ${name}`);
    }
  }

  const records = [
    { num: '0001', name: '0001-a.md', status: 'Accepted', date: '2026-11-04' },
    { num: '0002', name: '0002-b.md', status: 'Proposed', date: '2026-11-05' },
  ];
  const indexCases = [
    { rows: parseIndex(indexText('| 0001 | [A](0001-a.md) | Accepted | 2026-11-04 | h |\n| 0002 | [B](0002-b.md) | Proposed | 2026-11-05 | h |')), want: 0 },
    { rows: parseIndex(indexText('| 0001 | [A](0001-a.md) | Accepted | 2026-11-04 | h |')), want: 1 },
    { rows: parseIndex(indexText('| 0001 | [A](0001-c.md) | Accepted | 2026-11-04 | h |\n| 0002 | [B](0002-b.md) | Proposed | 2026-11-05 | h |')), want: 1 },
    { rows: parseIndex(indexText('| 0001 | [A](0001-a.md) | Proposed | 2026-11-04 | h |\n| 0002 | [B](0002-b.md) | Proposed | 2026-11-05 | h |')), want: 1 },
    { rows: parseIndex(indexText('| 0001 | [A](0001-a.md) | Accepted | 2026-11-04 | h |\n| 0002 | [B](0002-b.md) | Proposed | 2026-11-05 | h |\n| 0003 | [C](0003-c.md) | Accepted | 2026-11-06 | h |')), want: 1 },
  ];
  for (const [i, c] of indexCases.entries()) {
    const n = checkIndex(c.rows, records).length;
    if (n !== c.want) failures.push(`  index case ${i}: want ${c.want} finding(s), got ${n}`);
  }

  if (failures.length > 0) {
    console.error(`SELFTEST FAILED ${failures.length} case(s):`);
    console.error(failures.join('\n'));
    process.exit(1);
  }
  console.log(
    `SELFTEST OK: ${cases.length} record cases and ${indexCases.length} index cases ` +
      `(clean passes, every injected defect refused).`,
  );
  process.exit(0);
}

const indexText = (rows) => `# Index fixture\n\n## Index\n\n| ID | Title | Status | Date | Hook |\n|---|---|---|---|---|\n${rows}\n`;

if (process.argv.includes('--selftest')) runSelftest();

let entries;
try {
  entries = await readdir(ADR_DIR);
} catch (err) {
  console.error(`cannot read docs/adr/: ${err.message}`);
  process.exit(2);
}

const findings = [];
const records = [];

for (const name of entries.filter((n) => n.endsWith('.md')).sort()) {
  if (name === 'README.md') continue;
  if (!RECORD_RE.test(name)) {
    findings.push({ file: `docs/adr/${name}`, rule: 'filename', detail: 'not NNNN-<kebab-slug>.md (rule 1)' });
    continue;
  }
  const text = await readFile(join(ADR_DIR, name), 'utf8');
  const { num, status, date, count, bad } = recordShape(name, text);
  for (const b of bad) findings.push({ file: `docs/adr/${name}`, rule: b.rule, detail: b.detail });
  records.push({ name, num, status, date, count });
}

// Rule 2: append-only numbering means the numbers on disk are exactly 0001 through N, in a sequence with no
// hole and no repeat. A hole is a renumbering or a deletion; a repeat is a recycled id.
const nums = records.map((r) => r.num).sort();
const dupes = nums.filter((n, i) => n === nums[i - 1]);
for (const d of dupes) findings.push({ file: 'docs/adr/', rule: 'duplicate-number', detail: `ADR-${d} is held by two files (rule 2)` });
for (let i = 0; i < nums.length; i++) {
  if (Number(nums[i]) !== i + 1) {
    findings.push({
      file: 'docs/adr/',
      rule: 'number-sequence',
      detail: `position ${i + 1} holds ADR-${nums[i]}; rule 2 makes the set exactly 0001..${String(nums.length).padStart(4, '0')}`,
    });
    break;
  }
}

// A supersession that points at nothing is a dangling forward reference -- the only edit rule 5 allows a
// superseded file, so it is the one worth proving lands somewhere.
for (const r of records) {
  const m = (r.status || '').match(/^Superseded by ADR-(\d{4})$/);
  if (m && !nums.includes(m[1])) {
    findings.push({ file: `docs/adr/${r.name}`, rule: 'supersession-target', detail: `${r.status}, and no such record exists` });
  }
}

let indexRows;
try {
  indexRows = parseIndex(await readFile(resolve(REPO, INDEX_FILE), 'utf8'));
} catch (err) {
  console.error(`cannot read ${INDEX_FILE}: ${err.message}`);
  process.exit(2);
}
for (const b of checkIndex(indexRows, records)) {
  findings.push({ file: INDEX_FILE, rule: b.rule, detail: b.detail });
}

if (findings.length === 0) {
  const lengths = records.map((r) => r.count);
  console.log(
    `OK: ${records.length} ADRs -- one heading each, rule 3's six sections in order, Status and Date in ` +
      `vocabulary, ${Math.min(...lengths)}-${Math.max(...lengths)} lines (bound ${LINE_MIN}-${LINE_MAX}), ` +
      `numbered 0001..${nums[nums.length - 1]} with no hole, and indexed exactly.`,
  );
  process.exit(0);
}

console.error(`ADR SHAPE: ${findings.length} problem(s)`);
for (const f of findings) console.error(`  ${f.file}  [${f.rule}]  ${f.detail}`);
console.error('\nADR-0001 is append-only: fix the record, do not relax this gate to fit it.');
process.exit(1);
