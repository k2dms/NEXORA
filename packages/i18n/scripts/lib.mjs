// Shared parsing for ADR-0017's compile: one .po reader, one completion metric, one shipped-set rule.
// The metric (rule 5's "measured completion"): a msgid is covered when the locale's po carries a non-empty
// msgstr without a fuzzy flag, and the msgid exists in the en_US reference (the largest entry set); the ratio
// against the reference's msgid count is the completion. The shipped set (rule 4 + §9.6): exactly the locales
// po2crx.coffee compiles, each at or above the stated threshold.
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

export const LOCALES_DIR = 'omega-locales';
export const PO_NAME = 'LC_MESSAGES/omega-web.po';
export const REFERENCE_LOCALE = 'en_US';
// The stated threshold of ADR-0017 rule 5, in percent of the reference's msgids.
export const SUPPORTED_THRESHOLD = 90;
export const CATALOG_FILE = 'catalog/shipped.json';
export const SUPPORT_FILE = 'localeSupport.json';

export function findRepoRoot(start) {
  let dir = start;
  for (;;) {
    if (readdirSync(dir).includes('omega-locales') && readdirSync(dir).includes('packages')) return dir;
    const parent = join(dir, '..');
    if (parent === dir) throw new Error('repository root not found from ' + start);
    dir = parent;
  }
}

// One .po reader: msgid/msgstr pairs plus the fuzzy flag, longest-value concatenation for wrapped lines.
export function parsePo(text) {
  const entries = [];
  let cur = null;
  let mode = null;
  let fuzzy = false;
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (line.startsWith('#,')) {
      if (line.includes('fuzzy')) fuzzy = true;
      continue;
    }
    if (line.startsWith('#')) continue;
    if (line.startsWith('msgid "')) {
      if (cur) entries.push(cur);
      cur = { id: '', str: '', fuzzy };
      fuzzy = false;
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
  return entries;
}

export function localesWithPo(root) {
  return readdirSync(join(root, LOCALES_DIR))
    .filter((d) => {
      try {
        return readdirSync(join(root, LOCALES_DIR, d)).includes('LC_MESSAGES');
      } catch {
        return false;
      }
    })
    .sort();
}

export function measure(root, locale, referenceIds) {
  const po = join(root, LOCALES_DIR, locale, PO_NAME);
  const entries = parsePo(readFileSync(po, 'utf8')).filter((e) => e.id);
  const translated = entries.filter((e) => e.str && !e.fuzzy);
  const covered = translated.filter((e) => referenceIds.has(e.id)).map((e) => e.id);
  return {
    locale,
    entries: entries.length,
    translated: translated.length,
    fuzzy: entries.filter((e) => e.fuzzy).length,
    covered: covered.length,
    completionPct: Math.round((covered.length / referenceIds.size) * 100),
    coveredIds: new Set(covered),
    catalog: Object.fromEntries(translated.map((e) => [e.id, e.str])),
  };
}
