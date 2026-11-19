#!/usr/bin/env node
// `VER-56` said no tool reads the shipped manifest: `check-doc-evidence.mjs` proves the cited lines exist, but a
// permission added to or removed from a manifest is a behaviour change with no witness. Its named closer is this
// script, which diffs both overlay manifests against the "today" column of docs/architecture/31-security-model.md
// §13 -- the column that describes what is shipped, not the target column that describes what NEXORA intends to
// change. The obligation has one owner now and can be run before a commit instead of only after a push.
//
// What this refuses
//   1. a permission present in a manifest but absent from §13's today column (a grant nobody reviewed);
//   2. a permission in today's column missing from a manifest (a grant silently removed);
//   3. a browser-scoped grant in the wrong manifest -- `webRequestAuthProvider` is Chromium,
//      `webRequestBlocking` is Firefox, `webRequest` itself is both;
//   4. a change to `host_permissions`, today's other shipped grant;
//   5. a rewrite of the §13 table that destroys its shape or the "absent today" marker on `nativeMessaging`.
//
// The expected sets are derived from the document on every run, never copied into this file: a grant added to the
// column changes the check without a code edit, which is the point of reading the document rather than quoting it.
// The selftest injects one defect per rule and proves the guard bites before it is trusted to be quiet -- the
// standard `tools/check-adr-shape.mjs` and `tools/check-layout-guards.mjs` set.

import { readFile, mkdir, writeFile, rm } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const SEC_DOC = 'docs/architecture/31-security-model.md';
const MANIFESTS = {
  chromium: 'omega-target-chromium-extension/overlay/manifest.json',
  firefox: 'omega-target-chromium-extension/overlay/manifest-firefox.json',
};

// A backticked span in the Grant column is a grant unless it names a source file (`path.json:line`). The trailing
// text after a grant carries its browser scope: "(Chromium)" or "Firefox only" applies to the grant just named,
// which is how the combined `webRequest` + `webRequestAuthProvider` (Chromium) row scopes only the second token.
const evidenceToken = (s) => /[\\/]|\.json\b|:\d/.test(s);

function parseGrants(secText) {
  const lines = secText.split(/\r?\n/);
  const start = lines.findIndex((l) => /^## 13\./.test(l));
  if (start < 0) throw new Error('§13 is not in the security model');
  const next = lines.findIndex((l, i) => i > start && /^## \d+\./.test(l));
  const body = lines.slice(start, next < 0 ? lines.length : next);
  const table = body.filter((l) => /^\|/.test(l));
  if (table.length < 4) throw new Error(`§13's grant table has ${table.length} rows`);
  const header = table[0].split('|').map((s) => s.trim()).filter(Boolean);
  if (header[0] !== 'Grant' || !/^What it is load-bearing for today/.test(header[1] ?? '')) {
    throw new Error(`§13's grant table lost its header: ${header.join(' / ')}`);
  }
  const expected = { chromium: new Set(), firefox: new Set() };
  const host = {};
  let rows = 0;
  for (const row of table.slice(2)) {
    const cells = row.split('|').map((s) => s.trim());
    const grantCell = cells[1] ?? '';
    const todayCell = cells[2] ?? '';
    rows += 1;
    if (/absent today/.test(todayCell)) continue;
    const tokens = [...grantCell.matchAll(/`([^`]+)`/g)]
      .map((m) => ({ text: m[1], start: m.index, end: m.index + m[0].length }))
      .filter((t) => !evidenceToken(t.text));
    for (let i = 0; i < tokens.length; i += 1) {
      const t = tokens[i];
      const tail = grantCell.slice(t.end, i + 1 < tokens.length ? tokens[i + 1].start : grantCell.length);
      const scope = /\(Chromium/.test(tail) ? 'chromium' : /\(Firefox/.test(tail) ? 'firefox' : 'both';
      if (/^host_permissions\s*:/.test(t.text)) {
        const value = t.text.slice(t.text.indexOf(':') + 1).trim();
        if (scope === 'both') {
          host.chromium = value;
          host.firefox = value;
        } else {
          host[scope] = value;
        }
        continue;
      }
      if (scope === 'both' || scope === 'chromium') expected.chromium.add(t.text);
      if (scope === 'both' || scope === 'firefox') expected.firefox.add(t.text);
    }
  }
  if (rows < 8 || !host.chromium || expected.chromium.size < 6 || expected.firefox.size < 6) {
    throw new Error(`§13's grant table no longer describes both manifests (${rows} rows, ${expected.chromium.size}/${expected.firefox.size} grants)`);
  }
  return { expected, host, rows };
}

function compare(grants, manifests) {
  const problems = [];
  for (const [browser, rel] of Object.entries(MANIFESTS)) {
    const m = manifests[rel];
    const actual = new Set(Array.isArray(m.permissions) ? m.permissions : []);
    const want = grants.expected[browser];
    for (const p of want) {
      if (!actual.has(p)) problems.push(`${browser}: ${p} is in §13's today column but absent from its manifest`);
    }
    for (const p of actual) {
      if (!want.has(p)) problems.push(`${browser}: ${p} is granted by the manifest but not in §13's today column`);
    }
    const actualHost = JSON.stringify(Object.prototype.hasOwnProperty.call(m, 'host_permissions') ? m.host_permissions : null);
    const wantHost = grants.host[browser] === undefined ? 'null' : grants.host[browser];
    let wantCanonical;
    try {
      wantCanonical = JSON.stringify(JSON.parse(wantHost));
    } catch {
      problems.push(`${browser}: §13's host_permissions cell is not JSON: ${wantHost}`);
      continue;
    }
    if (actualHost !== wantCanonical) {
      problems.push(`${browser}: host_permissions is ${actualHost} but §13's today column says ${wantCanonical}`);
    }
  }
  return problems;
}

async function run(root) {
  let grants;
  try {
    grants = parseGrants(await readFile(join(root, SEC_DOC), 'utf8'));
  } catch (err) {
    return { problems: [`§13: ${err.message}`], notes: [] };
  }
  const manifests = {};
  for (const [browser, rel] of Object.entries(MANIFESTS)) {
    try {
      manifests[rel] = JSON.parse(await readFile(join(root, rel), 'utf8'));
    } catch (err) {
      return { problems: [`${rel}: unreadable or not JSON (${err.message})`], notes: [] };
    }
  }
  const notes = [
    `§13 today column: chromium=[${[...grants.expected.chromium].sort().join(', ')}]`,
    `§13 today column: firefox=[${[...grants.expected.firefox].sort().join(', ')}]`,
    `host_permissions from §13: ${grants.host.chromium} (chromium), ${grants.host.firefox} (firefox)`,
    `${grants.rows} grant rows read from the document, 2 manifests compared`,
  ];
  return { problems: compare(grants, manifests), notes };
}

// ---- selftest --------------------------------------------------------------------------------------------

const FIX_SEC = `# 31 — Security model

## 13. Browser-side surface: grants and CSP

Today's grants:

| Grant | What it is load-bearing for today | NEXORA target | Gate |
|---|---|---|---|
| \`proxy\` | the only actuator | keep | — |
| \`storage\` | the envelope | keep | — |
| \`alarms\` | subscription polling | keep | — |
| \`nativeMessaging\` | nothing — absent today | **add** | ADR-0004 |
| \`unlimitedStorage\` | rule lists over the sync quota | keep for now | — |
| \`webRequest\` + \`webRequestAuthProvider\` (Chromium) | proxy HTTP auth responses only | keep | optional grant |
| \`webRequestBlocking\` (Firefox only, \`omega-target-chromium-extension/overlay/manifest-firefox.json:45\`) | the same auth path there | same | parity |
| \`tabs\` | per-tab badge | optional grant | — |
| \`contextMenus\` | quick-switch menu | optional grant | — |
| \`host_permissions: ["<all_urls>"]\` | subscription/PAC fetching | **remove** | measure first |

## 14. Residual risk

nothing here
`;

const manifest = (perms, host = ['<all_urls>']) => JSON.stringify({ manifest_version: 3, permissions: perms, host_permissions: host }, null, 2) + '\n';
const FIX_CHROMIUM = manifest(['proxy', 'tabs', 'alarms', 'storage', 'unlimitedStorage', 'webRequest', 'webRequestAuthProvider', 'contextMenus']);
const FIX_FIREFOX = manifest(['proxy', 'tabs', 'alarms', 'storage', 'unlimitedStorage', 'webRequest', 'webRequestBlocking', 'contextMenus']);
const BASE = [
  [SEC_DOC, FIX_SEC],
  [MANIFESTS.chromium, FIX_CHROMIUM],
  [MANIFESTS.firefox, FIX_FIREFOX],
];
const sub = (files, want, text) => files.map(([f, t]) => (f === want ? [f, text] : [f, t]));

async function selftest() {
  const { mkdtemp } = await import('node:fs/promises');
  const { tmpdir } = await import('node:os');
  const tmpRoot = await mkdtemp(join(tmpdir(), 'manifest-delta-'));
  const cases = [
    ['clean fixture', BASE, 0],
    ['a permission is removed', sub(BASE, MANIFESTS.chromium, manifest(['proxy', 'tabs', 'alarms', 'storage', 'unlimitedStorage', 'webRequest', 'contextMenus'])), 1],
    ['nativeMessaging is granted early', sub(BASE, MANIFESTS.chromium, manifest(['proxy', 'tabs', 'alarms', 'storage', 'unlimitedStorage', 'webRequest', 'webRequestAuthProvider', 'contextMenus', 'nativeMessaging'])), 1],
    ['a Firefox-only grant crosses to Chromium', sub(BASE, MANIFESTS.chromium, manifest(['proxy', 'tabs', 'alarms', 'storage', 'unlimitedStorage', 'webRequest', 'webRequestAuthProvider', 'webRequestBlocking', 'contextMenus'])), 1],
    ['host_permissions is widened', sub(BASE, MANIFESTS.firefox, manifest(['proxy', 'tabs', 'alarms', 'storage', 'unlimitedStorage', 'webRequest', 'webRequestBlocking', 'contextMenus'], ['*://*/*'])), 1],
    ['the document drops the absent-today marker', sub(BASE, SEC_DOC, FIX_SEC.replace('nothing — absent today', 'the Connector door')), 2],
    ['the document loses its table header', sub(BASE, SEC_DOC, FIX_SEC.replace('| Grant |', '| Something else |')), 1],
  ];
  let n = 0;
  let failed = 0;
  for (const [name, files, want] of cases) {
    const root = join(tmpRoot, `case-${(n += 1)}`);
    for (const [rel, text] of files) {
      const abs = join(root, ...rel.split('/'));
      await mkdir(dirname(abs), { recursive: true });
      await writeFile(abs, text);
    }
    const { problems } = await run(root);
    const ok = want === 0 ? problems.length === 0 : problems.length >= want;
    if (!ok) {
      console.log(`SELFTEST FAIL ${name}: expected ${want === 0 ? 'a clean run' : `at least ${want} problem(s)`}, got ${problems.length}: ${problems.join(' | ')}`);
      failed += 1;
    }
  }
  await rm(tmpRoot, { recursive: true, force: true });
  if (failed > 0) {
    console.log(`SELFTEST FAILED: ${failed} of ${cases.length} cases`);
    process.exitCode = 1;
    return;
  }
  console.log(`SELFTEST OK: ${cases.length - 1} injected defects refused and the clean fixture passed — one per rule.`);
}

// ---- main ------------------------------------------------------------------------------------------------

if (process.argv[2] === '--selftest') {
  await selftest();
} else {
  const { problems, notes } = await run(REPO);
  for (const note of notes) console.log(`  note: ${note}`);
  if (problems.length > 0) {
    for (const p of problems) console.log(`  FAIL ${p}`);
    console.log(`\nFAILED: ${problems.length} manifest/§13 delta(s) (VER-56).`);
    process.exitCode = 1;
  } else {
    console.log('OK: both overlay manifests grant exactly what §13\'s today column declares, browser scope and host_permissions included.');
  }
}
