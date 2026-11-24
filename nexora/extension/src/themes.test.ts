import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';
import {
  PALETTE_KEYS, SEMANTIC_TOKENS, TOKEN_COUNT,
  readThemeColors, readThemeCatalogue, semanticFrom, tokensFor,
} from './themes.ts';

const shippedCataloguePath = new URL('../../../omega-web/lib/themes/themes.json', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1');
const readShippedCatalogue = () => readThemeCatalogue(readFileSync(shippedCataloguePath, 'utf8'));

test('the token lists match the presentation data exactly: 16 + 15 = 31', () => {
  const pres = JSON.parse(readFileSync(new URL('../../../packages/schema/presentation.json', import.meta.url), 'utf8')).themes;
  assert.deepEqual([...PALETTE_KEYS], pres.paletteKeys);
  assert.deepEqual([...SEMANTIC_TOKENS], pres.semanticTokens);
  assert.equal(TOKEN_COUNT, 31);
});

test('the shipped catalogue measures as the presentation data recorded', () => {
  const pres = JSON.parse(readFileSync(new URL('../../../packages/schema/presentation.json', import.meta.url), 'utf8')).themes.catalogue;
  const entries = readShippedCatalogue();
  assert.equal(entries.length, pres.base16Count, 'the pipeline paints every palette the catalogue declares');
  assert.ok(entries.every((e) => Object.keys(e.colors).length === pres.colorsPerTheme));
  const dark = entries.filter((e) => e.dark).length;
  assert.equal(dark, pres.darkFlagged, 'the dark flag survives the read');
});

test('the measured defects are trimmed, not inherited: china-red-screen and tomorrow-night-blue', () => {
  const entries = readShippedCatalogue();
  const china = entries.find((e) => e.key === 'china-red-screen');
  const tomorrow = entries.find((e) => e.key === 'tomorrow-night-blue');
  assert.ok(china && tomorrow, 'the two defective palettes must survive the read');
  for (const value of Object.values(china!.colors)) {
    assert.match(value, /^#[0-9a-fA-F]{6}$/, 'no leading whitespace survives');
  }
  assert.equal(tomorrow!.colors.base08, '#ff9da4', 'no trailing whitespace survives');
});

test('a non-hex value is refused, not coerced: an invisible theme is a refusal', () => {
  const full = (base00: string): Record<string, string> => Object.fromEntries(PALETTE_KEYS.map((k, i) => [k, i === 0 ? base00 : '#0000' + i.toString(16).padStart(2, '0')]));
  assert.equal(readThemeColors(full('pink')), null);
  assert.equal(readThemeColors(full('#1111')), null);
  const trimmed = readThemeColors(full(' #222222 '));
  assert.equal(trimmed?.base00, '#222222');
});

test('the semantic tokens derive from the base16 palette the way variable.css maps them', () => {
  const base16 = Object.fromEntries(PALETTE_KEYS.map((k, i) => [k, '#00000' + i]));
  const semantic = semanticFrom(base16);
  const css = readFileSync(new URL('../../../omega-web/lib/themes/variable.css', import.meta.url), 'utf8');
  for (const token of SEMANTIC_TOKENS) {
    const declared = css.match(new RegExp(`--${token}:\\s*(?:var\\(--(base[0-9A-F]{2}),\\s*#[0-9a-fA-F]{6}\\)|(#[0-9a-fA-F]{3,6}));`));
    assert.ok(declared, token + ' is declared by variable.css');
    assert.equal(semantic[token], declared[1] ? base16[declared[1]] : declared[2], token);
  }
});

test('tokensFor produces the 31 properties the popup writes onto its host element', () => {
  const tokens = tokensFor(readShippedCatalogue()[0]);
  assert.equal(Object.keys(tokens).length, TOKEN_COUNT);
  for (const key of [...PALETTE_KEYS, ...SEMANTIC_TOKENS]) {
    assert.match(tokens[key], /^#[0-9a-fA-F]{3}(?:[0-9a-fA-F]{3})?$/, key + ' is not a valid hex colour');
  }
  const panel = readFileSync(new URL('./ui/Popup.svelte', import.meta.url), 'utf8');
  assert.match(panel, /\$host<HTMLElement>\(\)/);
  assert.match(panel, /Object\.entries\(tokensFor\(theme\)\)/);
  assert.match(panel, /hostEl\.style\.setProperty\('--' \+ token, value\)/);
});
