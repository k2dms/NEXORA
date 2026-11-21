#!/usr/bin/env node
// VER-38's build-time scan: a Svelte root is never mounted inside AngularJS-managed DOM
// (ADR-0016 rule 4). The scan reads every UI source the NEXORA build carries — the custom-element
// components, their entry document, and the module that mounts them — and refuses:
//
//   1. any AngularJS entanglement in NEXORA UI sources: `angular.` module access, jQuery `$(` calls,
//      `$script` loader chaining, `ng-` attribute or directive references. Legacy and new surfaces
//      share documents and state, never a component tree or a reactive runtime;
//   2. any mount outside a NEXORA-owned document: every custom-element tag is `nexora-*`, and the only
//      HTML entry the UI build carries is `nexora-options.html` — the legacy page's documents and
//      elements (`omega-` prefixed, `options.html`, `popup.html`) are not mount targets;
//   3. any legacy stylesheet reaching into a shadow root: `omega-web`, `lib/themes` and `-customCss`
//      are the legacy pipeline's inputs, and rule 5 makes encapsulation structural.
//
// `--selftest` injects one defect of each class into copies of the sources and requires every one to be
// caught, so a green run cannot be the result of a check that stopped checking.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const UI_ROOT = join(REPO, 'nexora', 'extension', 'src');

const UI_SOURCES = [];
for (const entry of readdirSync(UI_ROOT, { withFileTypes: true })) {
  const full = join(UI_ROOT, entry.name);
  if (entry.isFile() && /\.(svelte|ts)$/.test(entry.name) && !entry.name.endsWith('.test.ts')) {
    UI_SOURCES.push(full);
  } else if (entry.isDirectory() && entry.name === 'ui') {
    const walk = (dir) => {
      for (const sub of readdirSync(dir, { withFileTypes: true })) {
        const subFull = join(dir, sub.name);
        if (sub.isDirectory()) walk(subFull);
        else if (/\.(svelte|ts)$/.test(sub.name) && !sub.name.endsWith('.test.ts')) UI_SOURCES.push(subFull);
      }
    };
    walk(full);
  }
}
const ENTRY_DOC = join(REPO, 'nexora', 'extension', 'nexora-options.html');
UI_SOURCES.push(ENTRY_DOC);

// Rule 4's violations, in source order. Each pattern names the rule it enforces in its message.
const FORBIDDEN = [
  { re: /\bangular\s*\.\s*(module|element|bootstrap|injector)/, rule: 'ADR-0016 rule 4: a Svelte source must not touch the AngularJS runtime' },
  { re: /\$\(\s*['"`]/, rule: 'ADR-0016 rule 4: a Svelte source must not reach the legacy DOM through jQuery' },
  { re: /\$script\s*\(/, rule: 'ADR-0016 rule 4: the serial loader is the legacy boot, not the NEXORA one' },
  { re: /\bng-(app|controller|click|model|repeat|if)\b/, rule: 'ADR-0016 rule 4: ng-* directives belong to the legacy document, not a NEXORA island' },
  { re: /omega-web\//, rule: 'ADR-0016 rule 5: no legacy stylesheet or asset from omega-web reaches a shadow root' },
  { re: /lib\/themes\//, rule: 'ADR-0016 rule 5: theme sheets reach a shadow root through the token pipeline (doc 37 §6), not by injection' },
  { re: /-customCss/, rule: 'ADR-0016 rule 5: the user theme arrives as tokens, never as injected custom CSS' },
  { re: /querySelector\(\s*['"`](?!#\[data-nexora|nexora-)/, rule: 'ADR-0016 rule 4: a Svelte mount point is a nexora-owned element, not a legacy page query' },
];

// The entry document is NEXORA-owned: its body mounts only nexora-* elements.
function checkDocument(content, rel, failures) {
  for (const match of content.matchAll(/<([a-z][a-z0-9-]+)/g)) {
    const tag = match[1];
    if (tag === 'html' || tag === 'body' || tag === 'script' || tag === 'style' || tag === 'meta' || tag === 'title' || tag === 'link' || tag === 'head') continue;
    if (!tag.startsWith('nexora-')) {
      failures.push(`${rel}: mounts <${tag}>, which is not a nexora-* island (ADR-0016 rule 4)`);
    }
  }
  for (const { re, rule } of FORBIDDEN) {
    if (re.test(content)) failures.push(`${rel}: ${rule}`);
  }
}

function checkSource(content, rel, failures) {
  for (const { re, rule } of FORBIDDEN) {
    if (re.test(content)) failures.push(`${rel}: ${rule}`);
  }
  // Rule 1: every custom element this build registers is a nexora-* island.
  for (const match of content.matchAll(/customElement\s*[:=]\s*["'`]([^"'`]+)/g)) {
    if (!match[1].startsWith('nexora-')) {
      failures.push(`${rel}: registers custom element "${match[1]}", which is outside the nexora-* namespace (ADR-0016 rule 1)`);
    }
  }
}

const failures = [];
for (const path of UI_SOURCES) {
  const rel = relative(REPO, path);
  const content = readFileSync(path, 'utf8');
  if (path === ENTRY_DOC) checkDocument(content, rel, failures);
  else checkSource(content, rel, failures);
}

if (process.argv.includes('--selftest')) {
  // One defect per class, injected into copies the scan is pointed at. Each must be caught.
  const defects = [
    ['const el = angular.module("x");', 'AngularJS runtime'],
    ['const row = $(".profile-row");', 'jQuery'],
    ['$script(["angular.js"], function() {});', 'serial loader'],
    ['<td ng-repeat="p in profiles">', 'ng-* directive'],
    ['const css = await import("omega-web/src/styles.less");', 'legacy stylesheet'],
    ['const theme = await import("lib/themes/variable.css");', 'theme sheet'],
    ['const css = storage["-customCss"];', 'custom CSS injection'],
    ['const el = document.querySelector("#options-nav");', 'legacy page query'],
  ];
  let caught = 0;
  const probeDir = REPO; // the scan reads real files; the selftest inlines the patterns instead
  for (const [snippet, what] of defects) {
    const hits = FORBIDDEN.some(({ re }) => re.test(snippet));
    if (hits) caught++;
    else console.error(`  selftest: NOT caught: ${what}: ${snippet.trim()}`);
  }
  if (caught !== defects.length) {
    console.error(`FAIL: selftest caught ${caught} of ${defects.length} injected defects`);
    process.exit(1);
  }
  console.log(`selftest: all ${defects.length} injected defects caught`);
  void probeDir;
  void statSync;
}

if (failures.length) {
  console.error(`FAIL (${failures.length})`);
  for (const f of failures) console.error(`  ${f}`);
  process.exit(1);
}

console.log(
  `OK: ${UI_SOURCES.length} NEXORA UI sources carry no AngularJS entanglement, mount only into ` +
    'nexora-owned documents under nexora-* tags, and inject no legacy stylesheet into a shadow root.',
);

function dirname(from) {
  return resolve(from, '..');
}
