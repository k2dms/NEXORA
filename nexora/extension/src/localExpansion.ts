// The `<local>` expansion of doc/architecture/28-routing-model.md §5, as the browser plane produces it.
// The state vocabulary names this `localExpanded` (packages/schema/presentation.json#/states): "the CIDRs
// and suffixes the expansion actually produced, and that it is an approximation". The transport plane can
// only match IP CIDRs, so the mapping is a conservative expansion into RFC1918/loopback/link-local plus
// the `.local` suffix — and a user who sees the token believes the rule, while a user who sees the
// expansion can disagree with it. The UI contract is explicit: render the expansion, never the token.

export interface LocalExpansion {
  cidrs: string[];
  suffixes: string[];
  /** The approximation sentence the state's `shows` field requires next to the lists. */
  note: string;
}

const EXPANSION: LocalExpansion = {
  cidrs: ['127.0.0.0/8', '::1/128', '169.254.0.0/16', 'fe80::/10', '10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16'],
  suffixes: ['.local'],
  note: 'Это приблизительное раскрытие: браузер считает эти адреса достижимыми без прокси, а транспортная плоскость сопоставляет CIDR-список, а не токен <local>.',
};

export function expandLocal(): LocalExpansion {
  // The expansion is a constant of the mapping, not of the envelope: every `<local>` the browser plane
  // sees produces exactly this set (doc 28 §5's table row). Returned fresh so a caller cannot mutate the
  // contract through a shared reference.
  return { cidrs: [...EXPANSION.cidrs], suffixes: [...EXPANSION.suffixes], note: EXPANSION.note };
}

/** The token itself, for the one place the UI may name it: the sentence explaining what was expanded. */
export const LOCAL_TOKEN = '<local>';
