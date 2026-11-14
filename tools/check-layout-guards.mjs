#!/usr/bin/env node
// Three rows of docs/architecture/39-verification-matrix.md said the same thing about this repository's own
// shape: the rules that keep the legacy tree buildable are written in documents and enforced by nothing.
// `VER-04` (no root manifest may appear), `VER-05` (the coupled legacy files and the six legacy roots may not
// move) and `VER-07` (the hub watcher must not reach the new roots) are those three. The first was already a
// step of .github/workflows/verify.yml; it is here now, with the two that had no executor at all, so the
// obligation has one owner and can be run before a commit instead of only after a push.
//
// What this refuses
//   1. root-bare — `package.json`, `package-lock.json`, `go.work`, `go.mod` or `.npmrc` at the repository root
//      (ADR-0018 rule 1, doc 20 §8). A root manifest changes which `node_modules` the prebuilt bundles resolve
//      against; a `go.work` silently re-scopes the Connector's module graph. Invisible in a diff of one file.
//   2. coupling — every `path:line` row of the name-coupling table in
//      `docs/audit/01-repository-and-build-graph.md` §7 (ADR-0002's evidence for why `omega-*` does not move)
//      must still name a file that exists, with those lines inside it, and those lines must still carry the
//      coupling: the literal `omega`, or for the four rows whose coupling is a glob, a link or a root-level file,
//      the token declared beside them. A directory renamed to `omega-web2` leaves every citation resolvable and
//      breaks the build graph -- that is the hole this closes, and `check-doc-evidence.mjs` does not.
//   3. legacy-roots — the six legacy directories exist, and the five that are npm packages still answer to their
//      own name in `package.json`. ADR-0002 makes the package name, not the folder, the thing the symlinks
//      resolve through, so a name change moves the coupling without moving a file.
//   4. locale-chain — the `../omega-locales/**.po` paths that
//      `omega-target-chromium-extension/grunt/po2crx.coffee` reads must exist: that file is the only thing in the
//      legacy build that reaches a sibling directory by relative path.
//   5. watcher-scope — `omega-build/Gruntfile.coffee` registers `watch` as `hub:all:watch` over the catch-all
//      `../*/Gruntfile.*`, so the watcher runs in every child of this repository that has a Gruntfile. Refused: a
//      `Gruntfile.*` anywhere outside the six legacy roots, and any glob in the two `watch.coffee` files that
//      leaves its own project except the declared `../omega-locales/` or mentions a new root.
//
// The selftest builds a synthetic tree in a temp directory and injects one defect per rule, so the guard proves
// it bites before it is trusted to be quiet -- the standard `tools/check-no-secrets.mjs` sets.
//
// Rule 5 walks the tracked file list (`git ls-files` here, the fixture's own listing in the selftest): derived
// trees like the scratch archives under `tmp/` hold copies of the legacy Gruntfiles and are not this repository.

import { readFile, readdir, mkdir, writeFile, rm, stat } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), '..');

const ROOT_FORBIDDEN = ['package.json', 'package-lock.json', 'go.work', 'go.mod', '.npmrc'];
const COUPLING_DOC = 'docs/audit/01-repository-and-build-graph.md';
const LEGACY_DIRS = [
  'omega-build',
  'omega-locales',
  'omega-pac',
  'omega-target',
  'omega-target-chromium-extension',
  'omega-web',
];
const NPM_DIRS = LEGACY_DIRS.filter((d) => d !== 'omega-locales');
const PO2CRX = 'omega-target-chromium-extension/grunt/po2crx.coffee';
const WATCH_FILES = ['omega-web/grunt/watch.coffee', 'omega-target-chromium-extension/grunt/watch.coffee'];
const NEW_ROOTS = ['packages', 'nexora', 'connector'];
const ALLOWED_ESCAPE = '../omega-locales/';

// The four coupling rows whose cited line does not contain the literal `omega`, and the token that makes each
// of them a coupling instead. Printed on every run: a row that stops needing its exception has to be noticed,
// the way PLANNED_DOCS is printed by tools/check-doc-evidence.mjs.
const NAME_FREE = { 2: 'Gruntfile', 3: 'Gruntfile', 10: 'npm link', 23: 'COPYING' };

const CITE_RE = /^([^`:\s]+):(\d+)(?:-(\d+))?$/;

const exists = async (p) => {
  try {
    return await stat(p);
  } catch {
    return null;
  }
};

const readLines = async (abs) => (await readFile(abs, 'utf8')).split(/\r?\n/);

// The coupling rows are read from the document, not copied into this file: the table is the record of the
// enumeration, so the guard reads the record and a deleted row becomes a refusal rather than a silent exemption.
async function readCouplingTable(root) {
  const lines = await readLines(join(root, COUPLING_DOC));
  const start = lines.findIndex((l) => l.startsWith('## 7.'));
  if (start === -1) throw new Error(`${COUPLING_DOC} has no "## 7." section to read the coupling table from`);
  const rows = [];
  for (let i = start + 1; i < lines.length; i += 1) {
    if (/^\*\*/.test(lines[i])) break;
    const m = lines[i].match(/^\|\s*(\d+)\s*\|[^|]*\|([^|]*)\|\s*$/);
    if (!m) continue;
    const cites = [];
    for (const cell of m[2].matchAll(/`([^`]+)`/g)) {
      const c = cell[1].match(CITE_RE);
      if (c) cites.push({ path: c[1], from: Number(c[2]), to: Number(c[3] ?? c[2]) });
    }
    rows.push({ num: Number(m[1]), cites });
  }
  return rows;
}

async function checkRootBare(root) {
  const bad = [];
  for (const f of ROOT_FORBIDDEN) {
    if (await exists(join(root, f))) bad.push(`${f} exists at the repository root`);
  }
  return bad;
}

async function checkCoupling(root, rows) {
  const bad = [];
  const files = new Set();
  for (const row of rows) {
    if (row.cites.length === 0) {
      bad.push(`row ${row.num} cites no \`path:line\`, so the table no longer describes evidence`);
      continue;
    }
    for (const cite of row.cites) {
      files.add(cite.path);
      const abs = join(root, ...cite.path.split('/'));
      if (!(await exists(abs))) {
        bad.push(`row ${row.num}: ${cite.path} is not in the tree (moved, renamed or deleted)`);
        continue;
      }
      const lines = await readLines(abs);
      if (cite.to > lines.length) {
        bad.push(`row ${row.num}: ${cite.path}:${cite.to} is past the end of a ${lines.length}-line file`);
        continue;
      }
      const seg = lines.slice(cite.from - 1, cite.to).join(' ');
      const token = /omega/.test(seg) ? 'omega' : NAME_FREE[row.num];
      if (!seg.trim() || !token || !seg.includes(token)) {
        bad.push(
          `row ${row.num}: ${cite.path}:${cite.from}-${cite.to} no longer carries the coupling ` +
            `(${NAME_FREE[row.num] ? `declared token "${NAME_FREE[row.num]}"` : 'expected the literal "omega"'}): ${seg.trim().slice(0, 64)}`,
        );
      }
    }
  }
  return { bad, fileCount: files.size };
}

async function checkLegacyRoots(root) {
  const bad = [];
  for (const d of LEGACY_DIRS) {
    if (!(await exists(join(root, d)))?.isDirectory?.()) bad.push(`${d}/ is not a directory`);
  }
  for (const d of NPM_DIRS) {
    let pkg;
    try {
      pkg = JSON.parse(await readFile(join(root, d, 'package.json'), 'utf8'));
    } catch {
      bad.push(`${d}/package.json is missing or unreadable`);
      continue;
    }
    if (pkg.name !== d) {
      bad.push(`${d}/package.json calls itself "${pkg.name}" (ADR-0002: the name resolves the symlink)`);
    }
  }
  return bad;
}

async function checkLocaleChain(root) {
  const text = await readFile(join(root, ...PO2CRX.split('/')), 'utf8');
  const paths = [...text.matchAll(/'\.\.\/omega-locales\/([^']+)'/g)].map((m) => `omega-locales/${m[1]}`);
  const bad = [];
  for (const p of new Set(paths)) {
    if (!(await exists(join(root, ...p.split('/'))))) bad.push(`${p} is read by ${PO2CRX} and is not in the tree`);
  }
  return { bad, occurrences: paths.length, distinct: new Set(paths).size };
}

async function listTracked(root) {
  try {
    return execFileSync('git', ['ls-files'], { cwd: root, encoding: 'utf8' })
      .split(/\r?\n/)
      .filter(Boolean);
  } catch {
    const out = [];
    const walk = async (rel) => {
      for (const e of await readdir(join(root, rel), { withFileTypes: true })) {
        const p = rel ? `${rel}/${e.name}` : e.name;
        if (e.isDirectory()) await walk(p);
        else out.push(p);
      }
    };
    await walk('');
    return out;
  }
}

async function checkWatcherScope(root, tracked) {
  const bad = [];
  for (const f of tracked) {
    const top = f.split('/')[0];
    if (/^Gruntfile\./.test(f.split('/').pop()) && !LEGACY_DIRS.includes(top)) {
      bad.push(
        `${f} is outside the six legacy roots, and omega-build's \`watch\` is \`hub:all:watch\` over the ` +
          `catch-all \`../*/Gruntfile.*\` -- the legacy dev loop would load it`,
      );
    }
  }
  for (const f of WATCH_FILES) {
    const text = await readFile(join(root, ...f.split('/')), 'utf8');
    for (const glob of text.matchAll(/'([^']*[*][^']*)'/g)) {
      const p = glob[1];
      if (p.startsWith('../') && !p.startsWith(ALLOWED_ESCAPE)) {
        bad.push(`${f} watches "${p}", which leaves the project and is not the declared ${ALLOWED_ESCAPE}`);
      } else if (NEW_ROOTS.some((r) => p.startsWith(r) || p.includes(`/${r}/`))) {
        bad.push(`${f} watches "${p}", which reaches the new root "${NEW_ROOTS.find((r) => p.startsWith(r) || p.includes(`/${r}/`))}"`);
      }
    }
  }
  return bad;
}

// SEC-3 (docs/architecture/31-security-model.md §12, VER-58): NEXORA must not widen the
// proxy-resolution context, and the cheapest negative to enforce is a diff against the shipped
// manifests: web_accessible_resources is the one key whose absence the audit records as load-bearing
// (docs/audit/10-manifest-packaging-distribution.md §1), so any tracked manifest that names it — as a
// key or as text — is refused.
async function checkManifests(root, tracked) {
  const bad = [];
  let scanned = 0;
  for (const f of tracked) {
    if (!/manifest[^/]*\.json$/.test(f)) continue;
    const text = await readFile(join(root, f), 'utf8').catch(() => null);
    if (text === null) continue;
    scanned += 1;
    if (text.includes('web_accessible_resources')) {
      bad.push(`${f} names web_accessible_resources — the shipped manifests leave it absent, and adding it widens the proxy-resolution context to every page (docs/architecture/31-security-model.md §12)`);
    }
  }
  return { bad, scanned };
}

async function run(root, lister) {
  const rows = await readCouplingTable(root);
  const coupling = await checkCoupling(root, rows);
  const locales = await checkLocaleChain(root);
  const tracked = await lister(root);
  const manifests = await checkManifests(root, tracked);
  const problems = [
    ...(await checkRootBare(root)).map((b) => `root-bare: ${b}`),
    ...coupling.bad.map((b) => `coupling: ${b}`),
    ...(await checkLegacyRoots(root)).map((b) => `legacy-roots: ${b}`),
    ...locales.bad.map((b) => `locale-chain: ${b}`),
    ...(await checkWatcherScope(root, tracked)).map((b) => `watcher-scope: ${b}`),
    ...manifests.bad.map((b) => `sec3-manifest: ${b}`),
  ];
  const notes = [
    `${rows.length} coupling rows over ${coupling.fileCount} distinct files (the audit's headline counts 11; this census counts ${coupling.fileCount})`,
    `rows ${Object.keys(NAME_FREE).join(', ')} are name-free and checked by their declared token instead of "omega"`,
    `${locales.occurrences} po paths read by po2crx.coffee, ${locales.distinct} distinct, all present`,
    `${tracked.filter((f) => /^Gruntfile\./.test(f.split('/').pop())).length} Gruntfiles tracked, all inside the six legacy roots`,
    `${manifests.scanned} manifest files scanned; web_accessible_resources absent from all`,
  ];
  return { problems, notes };
}

// ---- selftest --------------------------------------------------------------------------------------------

const FIX_AUDIT = `# fixture audit

## 7. Name-coupling sites that make moving \`omega-*\` expensive

| # | Coupling | Evidence |
|---|---|---|
| 2 | hub catch-all glob | \`omega-build/Gruntfile.coffee:1\` |
| 10 | npm link in pac | \`omega-pac/package.json:1\` |
| 22 | po2crx reads locales | \`omega-target-chromium-extension/grunt/po2crx.coffee:5\` |

All **VERIFIED** (each line read). **True count: 3 sites across 3 files**
`;

const FIX_WATCH = `module.exports =
  grunt:
    files:
      'grunt/*'
  po2crx_locales:
    files: ['../omega-locales/**/*']
    tasks: ['po2crx:locales']
`;

const FIX_PO2CRX = `module.exports =
  locales:
    files:
      'build/_locales/en/messages.json':
        '../omega-locales/en_US/LC_MESSAGES/omega-web.po'
`;

const FIX_PKG = (name, link = false) =>
  `{"name": "${name}"${link ? ', "scripts": { "dev": "npm link" }' : ''}}\n`;

const BASE = [
  [COUPLING_DOC, FIX_AUDIT],
  ['omega-build/Gruntfile.coffee', 'src: "../*/Gruntfile.*"\n'],
  ['omega-build/package.json', FIX_PKG('omega-build')],
  ['omega-locales/README.md', 'locale sources\n'],
  ['omega-locales/en_US/LC_MESSAGES/omega-web.po', 'msgid ""\n'],
  ['omega-pac/package.json', FIX_PKG('omega-pac', true)],
  ['omega-target/package.json', FIX_PKG('omega-target')],
  ['omega-target-chromium-extension/package.json', FIX_PKG('omega-target-chromium-extension')],
  ['omega-target-chromium-extension/grunt/po2crx.coffee', FIX_PO2CRX],
  ['omega-target-chromium-extension/grunt/watch.coffee', FIX_WATCH],
  ['omega-web/package.json', FIX_PKG('omega-web')],
  ['omega-web/grunt/watch.coffee', FIX_WATCH],
  ['packages/schema/package.json', '{"name": "@nexora/schema"}\n'],
  ['omega-target-chromium-extension/overlay/manifest.json', '{"manifest_version": 2, "name": "x"}\n'],
  ['omega-target-chromium-extension/overlay/manifest-firefox.json', '{"manifest_version": 2, "name": "x-fx"}\n'],
];

const sub = (files, want, text) => files.map(([f, t]) => (f === want ? [f, text] : [f, t]));

async function selftest() {
  const { mkdtemp } = await import('node:fs/promises');
  const { tmpdir } = await import('node:os');
  const tmpRoot = await mkdtemp(join(tmpdir(), 'layout-guards-'));
  const cases = [];
  let n = 0;
  const fixture = async (files) => {
    const root = join(tmpRoot, `case-${(n += 1)}`);
    for (const [rel, text] of files) {
      const abs = join(root, ...rel.split('/'));
      await mkdir(dirname(abs), { recursive: true });
      await writeFile(abs, text);
    }
    return root;
  };
  const fsLister = async (root) => {
    const out = [];
    const walk = async (rel) => {
      for (const e of await readdir(join(root, rel), { withFileTypes: true })) {
        const p = rel ? `${rel}/${e.name}` : e.name;
        if (e.isDirectory()) await walk(p);
        else out.push(p);
      }
    };
    await walk('');
    return out;
  };

  const add = (name, files, want) => cases.push({ name, files, want });
  add('clean fixture', BASE, 0);
  add('a root package.json appears', [...BASE, ['package.json', '{}\n']], 1);
  add('a coupled file moves', sub(BASE, COUPLING_DOC, FIX_AUDIT.replace('omega-pac/package.json:1', 'omega-pac/package.moved.json:1')), 1);
  add('a cited line drifts past the file', sub(BASE, COUPLING_DOC, FIX_AUDIT.replace('Gruntfile.coffee:1', 'Gruntfile.coffee:9')), 1);
  add('a coupling literal is edited out of the cited line', sub(BASE, PO2CRX, FIX_PO2CRX.replace('omega-locales/en_US/LC_MESSAGES/omega-web.po', 'locales/en_US/web.po')), 1);
  add('a package renames itself', sub(BASE, 'omega-web/package.json', FIX_PKG('zeroomega-web')), 1);
  add('a locale file disappears', BASE.filter(([f]) => f !== 'omega-locales/en_US/LC_MESSAGES/omega-web.po'), 1);
  add('a new root grows a Gruntfile', [...BASE, ['packages/ui/Gruntfile.coffee', 'module.exports = () ->\n']], 1);
  add('a watcher glob reaches a new root', sub(BASE, 'omega-web/grunt/watch.coffee', "module.exports =\n  nexora:\n    files: ['packages/ui/**/*']\n"), 1);
  add('a watcher glob escapes to an undeclared sibling', sub(BASE, 'omega-web/grunt/watch.coffee', "module.exports =\n  other:\n    files: ['../omega-other/**/*']\n"), 1);
  add('a manifest grows web_accessible_resources', [...BASE, ['omega-target-chromium-extension/overlay/manifest.json', '{"manifest_version": 2, "web_accessible_resources": ["img/*"]}\n']], 1);

  let failed = 0;
  for (const c of cases) {
    const root = await fixture(c.files);
    let problems;
    try {
      ({ problems } = await run(root, fsLister));
    } catch (err) {
      console.log(`SELFTEST FAIL ${c.name}: threw ${err.message}`);
      failed += 1;
      continue;
    }
    if (problems.length !== c.want) {
      console.log(`SELFTEST FAIL ${c.name}: expected ${c.want} problem(s), got ${problems.length}: ${problems.join(' | ')}`);
      failed += 1;
    }
  }
  await rm(tmpRoot, { recursive: true, force: true });
  if (failed > 0) {
    console.log(`SELFTEST FAILED: ${failed} of ${cases.length} cases`);
    process.exitCode = 1;
    return;
  }
  console.log(
    `SELFTEST OK: ${cases.length - 1} injected defects refused and the clean fixture passed — one per rule: ` +
      'a root manifest, a moved file, a drifted line, an edited-out coupling, a renamed package, a missing locale, ' +
      'a Gruntfile in a new root, two watcher globs out of scope and a widened manifest.',
  );
}

// ---- main ------------------------------------------------------------------------------------------------

if (process.argv[2] === '--selftest') {
  await selftest();
} else {
  const { problems, notes } = await run(REPO, listTracked);
  for (const note of notes) console.log(`  note: ${note}`);
  if (problems.length > 0) {
    for (const p of problems) console.log(`  FAIL ${p}`);
    console.log(`\nFAILED: ${problems.length} layout guard problem(s) (VER-04, VER-05, VER-07).`);
    process.exitCode = 1;
  } else {
    console.log(
      'OK: the root is bare of a manifest, every coupling row of the audit still points at the literal that ' +
        'makes it a coupling, the six legacy roots answer to their own names, the locale chain is intact, and ' +
        'nothing the hub watcher can reach belongs to the new roots.',
    );
  }
}
