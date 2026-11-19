#!/usr/bin/env node
// ADR-0017's compile: the omega-locales .po files are the single translation source, and this script is the
// single producer of the two artifacts the new UI reads -- catalog/shipped.json (the typed catalog for the
// Svelte islands, keyed by the same msgids the po files carry) and localeSupport.json (rule 4's table naming
// every source with its measured completion, so "unsupported" is a stated fact rather than an absent file).
// Nothing here is hand-maintained: verify.mjs recompiles and byte-compares against the committed artifacts.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { findRepoRoot, measure, localesWithPo, LOCALES_DIR, PO_NAME, REFERENCE_LOCALE, SUPPORTED_THRESHOLD, CATALOG_FILE, SUPPORT_FILE } from './lib.mjs';

const root = findRepoRoot(dirname(fileURLToPath(import.meta.url)));

// po2crx.coffee is the shipped set's own declaration (ADR-0017 rule 4: "the supported set is exactly the
// seven targets named in po2crx.coffee"). The targets are read from the file, never copied here.
const po2crx = readFileSync(join(root, 'omega-target-chromium-extension', 'grunt', 'po2crx.coffee'), 'utf8');
const po2crxLocales = [...po2crx.matchAll(/omega-locales\/([^/]+)\/LC_MESSAGES\/omega-web\.po/g)].map((m) => m[1]);
const supported = [...new Set(po2crxLocales)].sort();

const referenceEntries = parseRef();
const referenceIds = new Set(referenceEntries.map((e) => e.id));

function parseRef() {
  const text = readFileSync(join(root, LOCALES_DIR, REFERENCE_LOCALE, PO_NAME), 'utf8');
  // Same reading as lib.parsePo, inlined to keep the reference load one file read.
  const entries = [];
  let cur = null;
  let mode = null;
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (line.startsWith('#')) continue;
    if (line.startsWith('msgid "')) {
      if (cur) entries.push(cur);
      cur = { id: '', str: '' };
      mode = 'id';
      cur.id = line.slice(7, -1);
      continue;
    }
    if (line.startsWith('msgstr "')) {
      mode = 'str';
      cur.str = line.slice(8, -1);
      continue;
    }
    if (line.startsWith('"') && cur) {
      cur[mode] += line.slice(1, -1);
      continue;
    }
  }
  if (cur) entries.push(cur);
  return entries.filter((e) => e.id && e.str);
}

const locales = localesWithPo(root);
if (!locales.includes(REFERENCE_LOCALE)) {
  throw new Error('the reference locale ' + REFERENCE_LOCALE + ' is not under ' + LOCALES_DIR);
}
if (!supported.every((l) => locales.includes(l))) {
  throw new Error('po2crx names a locale with no po source: ' + supported.join(', '));
}

const rows = [];
const catalog = {};
for (const locale of locales) {
  const m = measure(root, locale, referenceIds);
  const isSupported = supported.includes(locale);
  if (isSupported && m.completionPct < SUPPORTED_THRESHOLD) {
    throw new Error(`${locale} ships via po2crx but measures ${m.completionPct}%, below the stated threshold ${SUPPORTED_THRESHOLD} — promote or fix the source, never ship below the bar`);
  }
  rows.push({
    locale,
    entries: m.entries,
    translated: m.translated,
    fuzzy: m.fuzzy,
    covered: m.covered,
    completionPct: m.completionPct,
    supported: isSupported,
    // Rule 5's promotion checklist, stated per row: what a contributor must add before the flag flips.
    promotion: isSupported
      ? 'shipped'
      : `${m.completionPct >= SUPPORTED_THRESHOLD ? 'completion above the stated threshold' : `completion ${m.completionPct}% of ${SUPPORTED_THRESHOLD}%`}; a po2crx.coffee target entry; a localeSupport row flip`,
  });
  if (isSupported) {
    catalog[locale] = m.catalog;
  }
}

mkdirSync(join(root, 'packages', 'i18n', 'catalog'), { recursive: true });
const outCatalog = JSON.stringify(catalog, null, 2) + '\n';
const outSupport = JSON.stringify({
  generatedBy: 'packages/i18n/scripts/compile.mjs',
  source: LOCALES_DIR + '/*/' + PO_NAME,
  referenceLocale: REFERENCE_LOCALE,
  thresholdPct: SUPPORTED_THRESHOLD,
  shippedSet: supported,
  rows,
}, null, 2) + '\n';

function readIf(path) {
  try {
    return readFileSync(path, 'utf8');
  } catch {
    return null;
  }
}

const catalogPath = join(root, 'packages', 'i18n', CATALOG_FILE);
const supportPath = join(root, 'packages', 'i18n', SUPPORT_FILE);
const sameCatalog = readIf(catalogPath) === outCatalog;
const sameSupport = readIf(supportPath) === outSupport;
if (!sameCatalog) writeFileSync(catalogPath, outCatalog);
if (!sameSupport) writeFileSync(supportPath, outSupport);
console.log(
  `compiled ${locales.length} locales, shipped ${supported.length}: catalog ${sameCatalog ? 'unchanged' : 'rewritten'}, localeSupport ${sameSupport ? 'unchanged' : 'rewritten'}`,
);
