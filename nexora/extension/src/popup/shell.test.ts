import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';
import { legacyProfileFacts, quickSwitchOrder, isLegacyProfileBody } from './shell.ts';

// The parity row's obligation: "the order a user made is intent data and survives the cut-over unchanged
// (ADR-0013)". The render order IS the storage order — the popup reorders nothing — and the facts the
// legacy decoration drew icons with (the profile type and the colour) travel as data.

const fixture = {
  schemaVersion: 2,
  '+proxy': { profileType: 'FixedProfile', color: '#99ccee', fallbackProxy: { scheme: 'http', host: 'p', port: 8080 } },
  '+company-pac': { profileType: 'PacProfile', color: '#ff8888', pacUrl: 'https://p/c.pac' },
  '+subscriptions': { profileType: 'RuleListProfile', color: '#dd6633', sourceUrl: 'https://r/l.txt' },
  '+auto switch': { profileType: 'SwitchProfile', color: '#99dd99', ruleList: [] },
  '+__ruleListOf_auto switch': { profileType: 'RuleListProfile', color: '#dd6633' },
  '-quickSwitchProfiles': ['auto switch', 'proxy'],
  '-revertProxyChanges': true,
};

test('legacyProfileFacts reads every profile in storage order with its own type and colour', () => {
  const facts = legacyProfileFacts(fixture);
  assert.deepEqual(facts.map((f) => f.displayName), ['proxy', 'company-pac', 'subscriptions', 'auto switch']);
  assert.deepEqual(facts.map((f) => f.profileType), ['FixedProfile', 'PacProfile', 'RuleListProfile', 'SwitchProfile']);
  assert.equal(facts[0].color, '#99ccee');
});

test('the rule-list attachments are not profiles: the +__ruleListOf prefix is excluded', () => {
  const facts = legacyProfileFacts(fixture);
  assert.ok(!facts.some((f) => f.displayName.startsWith('__ruleListOf')), 'an attachment rendered as a profile');
});

test('quickSwitchOrder reads the user ranking verbatim', () => {
  assert.deepEqual(quickSwitchOrder(fixture), ['auto switch', 'proxy']);
  assert.deepEqual(quickSwitchOrder({}), [], 'no ranking is not invented');
  assert.deepEqual(quickSwitchOrder({ '-quickSwitchProfiles': ['ghost', 'proxy'] }), ['ghost', 'proxy'], 'a missing profile is kept, not fixed');
});

test('a body that is not a legacy profile is skipped, not guessed', () => {
  assert.equal(isLegacyProfileBody({ profileType: 'SomethingElse' }), false);
  assert.equal(isLegacyProfileBody('FixedProfile'), false);
  assert.equal(legacyProfileFacts({ '+weird': { profileType: 'SomethingElse' } }).length, 0);
});

test('the render order is the snapshot order: the panel sorts nothing', () => {
  // The panel renders view.profiles in the given order and adds no sort of its own.
  const panel = readFileSync(new URL('../ui/Popup.svelte', import.meta.url), 'utf8');
  assert.match(panel, /\{#each view\.profiles as profile, index/);
  assert.doesNotMatch(panel, /\.sort\(|\.toSorted\(/, 'the popup must not reorder intent data');
});

test('the icon carries the profile type and the user colour as data, not a name-based guess', () => {
  const panel = readFileSync(new URL('../ui/Popup.svelte', import.meta.url), 'utf8');
  assert.match(panel, /ICON_GLYPH\[profile\.profileType/);
  assert.match(panel, /profile\.color \? 'outline-color: ' \+ profile\.color/);
  // The `<local>` expansion stays rendered on the options panel — VER-75's narrowing is not regressed.
  const optionsPanel = readFileSync(new URL('../ConnectionPanel.svelte', import.meta.url), 'utf8');
  assert.ok(optionsPanel.includes('localExpansion.cidrs'));
});
