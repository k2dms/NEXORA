import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';
import { expandLocal, LOCAL_TOKEN } from './localExpansion.ts';
import { PHASE2_CONTROLS } from './phase2.ts';

// VER-75's helper: the expansion the browser plane produces for `<local>` is exactly doc 28 §5's
// conservative mapping — RFC1918/loopback/link-local plus the `.local` suffix — and the state
// vocabulary's `shows` field requires the approximation note beside it.
test('expandLocal produces the conservative RFC1918/loopback/link-local set with the suffix', () => {
  const e = expandLocal();
  assert.deepEqual(e.cidrs.sort(), ['10.0.0.0/8', '127.0.0.0/8', '169.254.0.0/16', '172.16.0.0/12', '192.168.0.0/16', '::1/128', 'fe80::/10'].sort());
  assert.deepEqual(e.suffixes, ['.local']);
  assert.match(e.note, /приблизительное раскрытие/);
  assert.match(e.note, /CIDR/);
  // The expansion is the mapping's constant: two calls give equal, independent copies.
  const again = expandLocal();
  assert.deepEqual([...e.cidrs].sort(), [...again.cidrs].sort());
  e.cidrs.push('mutated');
  assert.notEqual(e.cidrs.length, expandLocal().cidrs.length);
});

// The panel renders the expansion and the disabled-with-a-reason Phase-2 controls. The component itself
// compiles in the build; what a unit test can pin is that the sources carry the contract's own words.
test('the panel renders the expansion with the approximation note, never a bare token alone', () => {
  const panel = readFileSync(new URL('./ConnectionPanel.svelte', import.meta.url), 'utf8');
  assert.ok(panel.includes('localExpansion.cidrs'), 'the expansion list is rendered');
  assert.ok(panel.includes('localExpansion.suffixes'), 'the suffix list is rendered');
  assert.ok(panel.includes('localExpansion.note'), 'the approximation note is rendered beside the lists');
  // The token appears only through the LOCAL_TOKEN constant naming what was expanded, not as a standalone
  // claim about what the Core matches.
  assert.ok(panel.includes('{LOCAL_TOKEN}'), 'the token names what was expanded');
});

test('every Phase-2 control is disabled-with-a-reason sourced from the corpus', () => {
  assert.ok(PHASE2_CONTROLS.length >= 2);
  for (const control of PHASE2_CONTROLS) {
    assert.ok(control.reason.length > 40, control.id + ': a reason shorter than a sentence explains nothing');
    assert.match(control.reason, /(doc|docs|packages\/schema|presentation\.json)/, control.id + ': the reason names its corpus source');
  }
  const panel = readFileSync(new URL('./ConnectionPanel.svelte', import.meta.url), 'utf8');
  assert.ok(panel.includes('PHASE2_CONTROLS'), 'the panel renders the registry');
  // The rendered button is disabled: constraint 11's compliant pattern.
  assert.match(panel, /button type="button" disabled aria-describedby=\{"phase2-"/);
});
