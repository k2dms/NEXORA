#!/usr/bin/env node
// Proves that the two shipped ZeroOmega 3.5.2 backups still load and compile in the *real*
// legacy engine. `docs/verification/live-matrix.md` row LIVE-14 claims the compile half runs
// today, and a row in a matrix needs a command behind it, not a memory of one.
//
// Nothing here emulates the engine: it loads `omega-pac/index.js` — the browserified legacy
// code the build produces — and asks it to do the work. If that file is missing the run fails
// with the build command instead of passing vacuously.
//
// What every envelope must satisfy
//   1. the shape `omega-pac/src/profiles.coffee` reads: every top-level key is a `+profile`,
//      a `-setting`, or `schemaVersion`. Any other key is invisible to the legacy loader, so a
//      migration built on it would silently drop user data.
//   2. every profile the engine finds compiles to a PAC body — except the profile types the
//      engine itself refuses (`+system`), which is the engine's own documented behaviour and
//      is expected, not excused.
//   3. every compiled body is valid JavaScript and produces a non-empty result string for each
//      sample request.
//   4. every result that names a profile names one present in the same envelope. A rule
//      pointing at a missing profile is exactly the corruption the migration must not introduce.
//
// Limits, stated because documents cite this file: no browser, no `chrome.*`, no import
// through the extension UI. The browser half of LIVE-14 stays `NO — deferred to Phase 2`.
//
// `--selftest` runs deliberately broken envelopes through the same rules and requires each to
// fail, so a green run above cannot be the result of a check that stopped checking.

import { createRequire } from 'node:module';
import { readFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(import.meta.url);

const BUNDLE = join(REPO, 'omega-pac', 'index.js');

const FIXTURES = [
  'docs/verification/backup-from-3.5.2.json',
  'docs/verification/backup-3.5.2-all-profile-types.json',
];

// One request per condition family the fixtures contain: a host a wildcard rule names, a host
// an ip-addition covers, and hosts that reach the default.
const SAMPLE_URLS = [
  'http://www.example.com/',
  'https://mail.example.com/inbox',
  'http://internal.example.com/',
  'http://10.0.0.8/',
  'https://shop.example.com/cart',
];

// The PAC built-ins Chromium provides at evaluate time. Stubbed because what is being proven is
// the *shape* of an answer — a result string that names a real profile — not a routing decision,
// which is what the live rows measure.
const PAC_HELPERS = ['shExpMatch', 'dnsResolve', 'isInNet', 'myIP', 'dnsDomainIs', 'localHostOrDomainIs'];

let OmegaPac;
try {
  OmegaPac = require(BUNDLE);
} catch {
  console.error(
    `FAIL: ${BUNDLE} is not built. Run \`npm --prefix omega-pac install\` then \`npx --prefix omega-pac grunt\` ` +
      '(the default task; see docs/audit/99-baseline-report.md) and re-run this check.',
  );
  process.exit(1);
}

const Profiles = OmegaPac.Profiles;

const helperImpl = (name) => {
  if (name === 'dnsResolve') return (host) => host;
  if (name === 'myIP') return () => '127.0.0.1';
  return () => false;
};

// The engine compiles a profile to either a FindProxyForURL-shaped function or, when the
// profile has no conditions at all, the literal result string itself. Both are valid output;
// only the first takes a request.
const evaluate = (profile) => {
  const src = Profiles.compile(profile).print_to_string();
  const value = new Function(...PAC_HELPERS, `return (${src});`)(...PAC_HELPERS.map(helperImpl));
  return typeof value === 'function' ? value : () => value;
};

function checkEnvelope(rel, options) {
  const failures = [];
  const stats = { profiles: 0, compiled: 0, refused: 0, rules: 0, evaluations: 0 };

  if (!('schemaVersion' in options)) failures.push(`${rel}: no schemaVersion — the legacy export always writes one`);

  for (const key of Object.keys(options)) {
    if (key === 'schemaVersion') continue;
    if (key[0] !== '+' && key[0] !== '-') {
      failures.push(`${rel}: key ${JSON.stringify(key)} is invisible to Profiles.each (the prefix rule)`);
    }
  }

  const known = new Set([...Object.keys(options).filter((k) => k[0] === '+'), ...Object.keys(Profiles.builtinProfiles)]);
  const names = new Set([...known].map((k) => k.slice(1)));
  const declared = Object.keys(options).filter((k) => k[0] === '+');
  const visited = new Set();
  let ownCompiled = 0;
  let ownRefused = 0;

  Profiles.each(options, (key, profile) => {
    stats.profiles++;
    const own = Object.prototype.hasOwnProperty.call(options, key);
    if (own) visited.add(key);
    if (Array.isArray(profile.rules)) stats.rules += profile.rules.length;

    let fn;
    try {
      fn = evaluate(profile);
    } catch (err) {
      // The engine refuses to put a system profile in a PAC; that refusal is the contract.
      if (/cannot be used in PAC scripts/.test(String(err.message))) {
        stats.refused++;
        if (own) ownRefused++;
        return;
      }
      failures.push(`${rel}: ${key} (${profile.profileType}) did not compile: ${err.message}`);
      return;
    }

    if (own) ownCompiled++;
    stats.compiled++;
    for (const url of SAMPLE_URLS) {
      const request = OmegaPac.Conditions.requestFromUrl(url);
      let result;
      try {
        result = fn(request.url, request.host, request.scheme);
      } catch (err) {
        failures.push(`${rel}: ${key}: evaluating ${url} threw ${err.message}`);
        continue;
      }
      stats.evaluations++;
      if (typeof result !== 'string' || result.length === 0) {
        failures.push(`${rel}: ${key}: ${url} produced ${JSON.stringify(result)} — a PAC result must be a string`);
        continue;
      }
      for (const token of result.split(';').map((s) => s.trim()).filter(Boolean)) {
        const ref = token.split(' ')[0];
        if (ref.startsWith('+') && !known.has(ref) && !names.has(ref.slice(1))) {
          failures.push(`${rel}: ${key}: ${url} resolves to ${ref}, which is not in this envelope`);
        }
      }
    }
  });

  // Profiles.each yields the builtins too, so "found nothing" is not a reachable failure —
  // what matters is that every profile the envelope *declares* was actually seen, which is
  // the `+`-prefix filter every NEXORA key design depends on.
  for (const key of declared) {
    if (!visited.has(key)) failures.push(`${rel}: ${key} is declared but Profiles.each never yielded it`);
  }
  if (declared.length > 0 && ownCompiled === 0 && ownRefused < declared.length) {
    failures.push(`${rel}: none of the ${declared.length} user profiles compiled`);
  }

  return { failures, stats };
}

const printStats = (rel, s) =>
  console.log(
    `${rel}\n  profiles=${s.profiles} compiled=${s.compiled} refused-by-engine=${s.refused} rules=${s.rules} ` +
      `evaluations=${s.evaluations}`,
  );

if (process.argv.includes('--selftest')) {
  // Each case names the rule it exists to catch, and the run passes only if every one fails.
  const cases = [
    ['a rule that names a profile the envelope does not contain', {
      schemaVersion: 2,
      '+switch': { profileType: 'FixedProfile', proxy: { scheme: 'http', host: '127.0.0.1', port: 8888 } },
      '+auto switch': {
        profileType: 'SwitchProfile',
        defaultProfileName: 'gone',
        rules: [{ condition: { conditionType: 'HostWildcardCondition', pattern: '*' }, profileName: 'gone' }],
      },
    }],
    ['a key the legacy loader cannot see at all', { schemaVersion: 2, proxy: { profileType: 'DirectProfile' } }],
    ['an envelope with a profile type the engine does not know', {
      schemaVersion: 2,
      '+weird': { profileType: 'NotAProfileType' },
    }],
    ['an envelope with no schemaVersion', { '+direct': { profileType: 'DirectProfile' } }],
  ];
  let passed = 0;
  for (const [what, envelope] of cases) {
    const { failures } = checkEnvelope('<selftest>', envelope);
    if (failures.length > 0) {
      passed++;
      console.log(`  caught: ${what} (${failures[0].replace('<selftest>: ', '')})`);
    } else {
      console.error(`  MISSED: ${what} — the check would wave this through`);
    }
  }
  console.log(
    passed === cases.length
      ? `SELFTEST OK: ${passed}/${cases.length} broken envelopes rejected.`
      : `SELFTEST FAIL: ${cases.length - passed} of ${cases.length} accepted.`,
  );
  process.exit(passed === cases.length ? 0 : 1);
}

const all = [];
for (const rel of FIXTURES) {
  const options = JSON.parse(await readFile(join(REPO, rel), 'utf8'));
  const { failures, stats } = checkEnvelope(rel, options);
  printStats(rel, stats);
  all.push(...failures);
}

if (all.length) {
  console.error(`\nFAIL (${all.length})`);
  for (const f of all) console.error(`  ${f}`);
  process.exit(1);
}

console.log(
  `OK: ${FIXTURES.length} shipped backups load and compile in the legacy engine ` +
    '(`omega-pac/index.js`).',
);
