// The popup keyboard map, ported from the legacy behaviour the parity matrix names
// (omega-web/src/popup/js/keyboard.js:2-28, nine digit slots, motions, the help overlay of
// keyboard_help.js:1). packages/schema/presentation.json#/popup/keyboardMap states the contract: the map
// is a behaviour, not an implementation detail — a user who switches profiles with 3 has no migration note
// that helps them. The NEXORA mapping keeps every binding the legacy popup had and gives the named
// actions their PopupShell meanings (doc 22 §13): quick switch, options, help. The focus model stays a
// tree walker with an announced selection.

import type { PopupProfile } from './model.ts';

export interface KeyboardHandlers {
  /** js-direct / js-system / js-external: the legacy builtins, selected in the browser plane. */
  selectBuiltin(which: 'direct' | 'system' | 'external'): void;
  /** js-addrule: add a rule for the current site in the browser plane (works with no Connector). */
  addRule(): void;
  /** js-temprule: the temp rule for the current site, browser-plane only. */
  tempRule(): void;
  /** Digit slots 1–9: select the profile at that position; digits beyond the list do nothing. */
  selectBySlot(slot: number): void;
  /** js-option: navigate to the NEXORA options page. */
  openOptions(): void;
  /** js-reqinfo's descendant: announce the current selection (screen-reader contract). */
  announceSelection(): void;
  /** The help overlay of keyboard_help.js: slash and question mark toggle it. */
  toggleHelp(): void;
  /** Motions move the focus one entry up or down the list. */
  moveFocus(delta: number): void;
  /** Enter/Space activate the focused entry — the walker's tabbable-only contract. */
  activateFocused(): void;
}

export interface KeyboardState {
  helpOpen: boolean;
  focusedIndex: number;
  announce: string;
}

/**
 * Handle one keydown against the popup's state. Returns the next state; a key the map does not name
 * leaves the state untouched. The digit slots are 1–9 over the profile list order; motions are the
 * arrow keys plus the vim letters the legacy map answers to; slash and question mark toggle the help
 * overlay; Escape closes it.
 */
export function handleKey(
  key: string,
  state: KeyboardState,
  profiles: PopupProfile[],
  handlers: KeyboardHandlers,
): KeyboardState {
  if (key === '/' || key === '?') {
    return { ...state, helpOpen: !state.helpOpen };
  }
  if (key === 'Escape' && state.helpOpen) {
    return { ...state, helpOpen: false };
  }
  if (state.helpOpen) {
    // While the overlay is up the list is not navigable: the legacy overlay is modal too.
    return state;
  }
  const slot = Number(key);
  if (Number.isInteger(slot) && slot >= 1 && slot <= 9) {
    if (slot <= profiles.length) {
      handlers.selectBySlot(slot);
      const profile = profiles[slot - 1];
      return { ...state, focusedIndex: slot - 1, announce: `${slot}: ${profile.displayName}` };
    }
    return state;
  }
  switch (key) {
    case 'ArrowDown':
    case 'j':
      return move(state, profiles, handlers, +1);
    case 'ArrowUp':
    case 'k':
      return move(state, profiles, handlers, -1);
    case 'ArrowLeft':
    case 'h':
    case 'ArrowRight':
    case 'l':
      // The legacy left/right motion pages the list; with one column it is the same step.
      return move(state, profiles, handlers, key === 'ArrowRight' || key === 'l' ? +1 : -1);
    case 'Enter':
    case ' ':
      handlers.activateFocused();
      return state;
    case '0':
      handlers.selectBuiltin('direct');
      return { ...state, announce: 'direct' };
    case 's':
      handlers.selectBuiltin('system');
      return { ...state, announce: 'system' };
    case 'e':
      handlers.selectBuiltin('external');
      return { ...state, announce: 'external' };
    case 'a':
    case '+':
      handlers.addRule();
      return state;
    case 't':
      handlers.tempRule();
      return state;
    case 'o':
      handlers.openOptions();
      return state;
    case 'r':
      handlers.announceSelection();
      return state;
    default:
      return state;
  }
}

function move(state: KeyboardState, profiles: PopupProfile[], handlers: KeyboardHandlers, delta: number): KeyboardState {
  if (profiles.length === 0) return state;
  const next = Math.min(Math.max(state.focusedIndex + delta, 0), profiles.length - 1);
  handlers.moveFocus(next - state.focusedIndex);
  return { ...state, focusedIndex: next, announce: profiles[next].displayName };
}

/** The tree walker of omega-web/src/popup/js/keyboard.js:34-38, kept as the focus contract: the popup's
 * tabbables are the entries the walker yields, and focus lands on the walker's answer, not on a
 * hand-maintained index. */
export function tabbables(root: ParentNode): HTMLElement[] {
  const walker = (root.ownerDocument ?? document).createTreeWalker(root, NodeFilter.SHOW_ELEMENT, {
    acceptNode(node) {
      const el = node as HTMLElement;
      if (el.tagName === 'BUTTON' || el.tagName === 'A' || el.tagName === 'SELECT' || el.tagName === 'INPUT') {
        if ((el as HTMLButtonElement).disabled) return NodeFilter.FILTER_REJECT;
        return NodeFilter.FILTER_ACCEPT;
      }
      if (el.tabIndex >= 0) return NodeFilter.FILTER_ACCEPT;
      return NodeFilter.FILTER_SKIP;
    },
  });
  const out: HTMLElement[] = [];
  let current = walker.nextNode();
  while (current) {
    out.push(current as HTMLElement);
    current = walker.nextNode();
  }
  return out;
}
