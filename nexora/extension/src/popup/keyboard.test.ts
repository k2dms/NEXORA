import { test } from 'node:test';
import { strict as assert } from 'node:assert';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { handleKey, tabbables, type KeyboardState } from './keyboard.ts';
import { snapshotWithoutConnector, type PopupProfile } from './model.ts';

// The keyboard map is a behaviour, not an implementation detail
// (packages/schema/presentation.json#/popup/keyboardMap): a user who switches profiles with 3 has no
// migration note that helps them. These tests pin the behaviour, not the key codes' spelling.

const profiles: PopupProfile[] = [
  { displayName: 'Work', plane: 'nexora', nexoraId: '11111111-1111-4111-8111-111111111111', selected: true },
  { displayName: 'Home', plane: 'nexora', nexoraId: '22222222-2222-4222-8222-222222222222', selected: false },
  { displayName: 'Travel', plane: 'legacy', selected: false },
];

const noHandlers = {
  selectBySlot: () => {},
  openOptions: () => {},
  announceSelection: () => {},
  toggleHelp: () => {},
  moveFocus: () => {},
  activateFocused: () => {},
};

const initial: KeyboardState = { helpOpen: false, focusedIndex: 0, announce: '' };

test('digit slots 1–9 select by position and refuse slots past the list', () => {
  let picked: number | undefined;
  const state = handleKey('2', initial, profiles, { ...noHandlers, selectBySlot: (slot) => { picked = slot; } });
  assert.equal(picked, 2);
  assert.equal(state.focusedIndex, 1);
  assert.match(state.announce, /2: Home/);
  // Slot 4 names no profile: the state is untouched, not wrapped.
  const untouched = handleKey('4', initial, profiles, noHandlers);
  assert.deepEqual(untouched, initial);
});

test('motions move the focus one entry and clamp at both ends', () => {
  const down = handleKey('ArrowDown', initial, profiles, noHandlers);
  assert.equal(down.focusedIndex, 1);
  const vim = handleKey('j', down, profiles, noHandlers);
  assert.equal(vim.focusedIndex, 2);
  const past = handleKey('j', vim, profiles, noHandlers);
  assert.equal(past.focusedIndex, 2, 'the walker clamps at the last entry');
  const up = handleKey('ArrowUp', past, profiles, noHandlers);
  assert.equal(up.focusedIndex, 1);
  const h = handleKey('h', up, profiles, noHandlers);
  assert.equal(h.focusedIndex, 0);
  const clamped = handleKey('k', h, profiles, noHandlers);
  assert.equal(clamped.focusedIndex, 0, 'the walker clamps at the first entry');
});

test('slash and question mark toggle the help overlay, Escape closes it, and the overlay is modal', () => {
  const open = handleKey('?', initial, profiles, noHandlers);
  assert.equal(open.helpOpen, true);
  const closed = handleKey('/', open, profiles, noHandlers);
  assert.equal(closed.helpOpen, false);
  const open2 = handleKey('/', closed, profiles, noHandlers);
  const escaped = handleKey('Escape', open2, profiles, noHandlers);
  assert.equal(escaped.helpOpen, false);
  // While the overlay is up, digits do not select: the legacy overlay is modal too.
  const modal = handleKey('2', open2, profiles, noHandlers);
  assert.equal(modal.helpOpen, true);
  assert.equal(modal.focusedIndex, open2.focusedIndex);
});

test('invariant 1: a snapshot with no Connector renders the legacy list and a stated reason', () => {
  const snap = snapshotWithoutConnector(['proxy', 'auto switch'], 'Connector не отвечает: legacy-профили переключаются, NEXORA-профили недоступны.');
  assert.equal(snap.profiles.length, 2);
  assert.ok(snap.profiles.every((p) => p.plane === 'legacy'));
  assert.match(snap.nexoraUnavailableReason!, /NEXORA-профили недоступны/);
  assert.equal(snap.health, 'unknown');
});

test('invariant 2: the popup entry starts nothing on mount', () => {
  const entry = readFileSync(new URL('../ui/popup-entry.ts', import.meta.url), 'utf8');
  assert.doesNotMatch(entry, /request\(|sendMessage|connectNative|server\.test|subscription\.refresh/, 'the popup entry fires no command');
  assert.match(entry, /Invariant 2: this entry starts nothing/);
});

test('the built popup document is inside the popupBundleKiB budget', () => {
  // The budget lives at packages/schema/presentation.json#/budgets: 64 KiB measured on the built
  // document. The measurement is the popup entry plus every chunk it imports, the bytes the document
  // actually loads.
  const budgetMatch = JSON.parse(readFileSync(new URL('../../../../packages/schema/presentation.json', import.meta.url), 'utf8'));
  const budget = budgetMatch.budgets.popupBundleKiB;
  const root = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..', 'dist', 'chromium');
  const seen = new Set<string>();
  let total = 0;
  const walk = (file: string): void => {
    if (seen.has(file)) return;
    seen.add(file);
    total += statSync(file).size;
    const src = readFileSync(file, 'utf8');
    for (const m of src.matchAll(/from\s*"(\.[^"]+)"|import\s*"(\.[^"]+)"/g)) {
      const resolved = resolve(dirname(file), m[1] ?? m[2]);
      try {
        if (resolved.endsWith('.js') && statSync(resolved).isFile()) walk(resolved);
      } catch { /* a missing chunk is the build's problem, not the budget's */ }
    }
  };
  const entry = join(root, 'nexora-popup.js');
  walk(entry);
  const kib = total / 1024;
  assert.ok(kib <= budget.value, `the popup document loads ${kib.toFixed(1)} KiB against a ${budget.value} KiB budget`);
});
