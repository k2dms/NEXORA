#!/usr/bin/env node
// The assertion VER-40 closed with: the committed artifacts are byte-identical to a fresh compile (nothing is
// hand-maintained), the localeSupport table names every .po source in the tree, the shipped set is exactly what
// po2crx.coffee compiles, and every shipped locale clears the stated threshold without a fuzzy flag — rule 5's
// three-part promotion gate, enforced rather than described.
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { findRepoRoot, measure, localesWithPo, SUPPORTED_THRESHOLD, CATALOG_FILE, SUPPORT_FILE } from './lib.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const root = findRepoRoot(here);
const pkg = join(root, 'packages', 'i18n');
let failures = 0;
const check = (ok, label, detail = '') => {
  if (ok) {
    console.log(`ok: ${label}${detail ? ` — ${detail}` : ''}`);
  } else {
    console.log(`FAILED: ${label}${detail ? ` — ${detail}` : ''}`);
    failures += 1;
  }
};

// 1. A fresh compile must byte-match the committed artifacts. The committed bytes are read before the compile
// runs, because a drifted compile rewrites the files in place; on a mismatch the committed bytes are restored
// so the failure a runner sees is the diff the guard printed, not a silently updated artifact.
const committedCatalog = readFileSync(join(pkg, CATALOG_FILE), 'utf8');
const committedSupport = readFileSync(join(pkg, SUPPORT_FILE), 'utf8');
execFileSync(process.execPath, [join(here, 'compile.mjs')], { cwd: root, stdio: 'ignore' });
const freshCatalog = readFileSync(join(pkg, CATALOG_FILE), 'utf8');
const freshSupport = readFileSync(join(pkg, SUPPORT_FILE), 'utf8');
check(freshCatalog === committedCatalog, 'the committed catalog is byte-identical to a fresh compile');
check(freshSupport === committedSupport, 'the committed localeSupport is byte-identical to a fresh compile');
if (freshCatalog !== committedCatalog) writeFileSync(join(pkg, CATALOG_FILE), committedCatalog);
if (freshSupport !== committedSupport) writeFileSync(join(pkg, SUPPORT_FILE), committedSupport);

// 2. The support table names every .po source in the tree, and no extras.
const support = JSON.parse(committedSupport);
const treeLocales = localesWithPo(root);
const named = support.rows.map((r) => r.locale);
check(
  JSON.stringify(named) === JSON.stringify(treeLocales),
  'localeSupport names every .po source in the tree, in order',
  `tree=${treeLocales.length} named=${named.length}`,
);

// 3. The shipped set is exactly what po2crx.coffee compiles.
const po2crx = readFileSync(join(root, 'omega-target-chromium-extension', 'grunt', 'po2crx.coffee'), 'utf8');
const po2crxLocales = [...new Set([...po2crx.matchAll(/omega-locales\/([^/]+)\/LC_MESSAGES\/omega-web\.po/g)].map((m) => m[1]))].sort();
const shipped = [...support.shippedSet].sort();
check(
  JSON.stringify(shipped) === JSON.stringify(po2crxLocales),
  'the shipped set is exactly the locales po2crx.coffee compiles',
  `po2crx=[${po2crxLocales.join(', ')}] shipped=[${shipped.join(', ')}]`,
);

// 4. Every shipped locale clears the stated threshold with no fuzzy flag.
for (const row of support.rows) {
  if (!row.supported) continue;
  check(
    row.completionPct >= SUPPORTED_THRESHOLD && row.fuzzy === 0,
    `shipped locale ${row.locale} clears the ${SUPPORTED_THRESHOLD}% threshold with no fuzzy`,
    `${row.completionPct}%, ${row.fuzzy} fuzzy`,
  );
}

// 5. Rule 5's promotion gate is honest: an unshipped locale states exactly what a contributor must add (the
// po2crx entry and the row flip), never a promise of shipping. Promotion-eligible locales are printed for the
// contributor track the ADR names.
for (const row of support.rows) {
  if (row.supported) continue;
  check(/po2crx\.coffee target entry/.test(row.promotion), `unshipped locale ${row.locale} states the promotion work, not a promise`, row.promotion.slice(0, 60));
  if (row.completionPct >= SUPPORTED_THRESHOLD) console.log(`   promotion-eligible today: ${row.locale} at ${row.completionPct}%`);
}

// 6. Every shipped catalog entry traces to its po: the msgid exists there and the msgstr matches, so a key is
// never renamed to satisfy the bundler (ADR-0017 rule 2).
for (const [locale, entries] of Object.entries(JSON.parse(committedCatalog))) {
  const po = measure(root, locale, new Set(Object.keys(entries)));
  const bad = Object.entries(entries).filter(([id, str]) => po.catalog[id] !== str);
  check(bad.length === 0, `shipped catalog ${locale}: every msgstr matches its po`, bad.length ? `first mismatch: ${bad[0][0]}` : `${Object.keys(entries).length} entries`);
}

if (failures > 0) {
  console.log(`FAILED: ${failures} check(s)`);
  process.exit(1);
}
console.log('i18n: all checks pass');
