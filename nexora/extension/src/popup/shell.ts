// The shell's snapshot builder (doc 22 §13 invariant 4: the shell owns the state, the popup is a view).
// It reads the legacy envelope directly — reads are not side effects (doc 22 §13 invariant 2: opening the
// popup performs no side-effecting command on mount) — and shapes what the popup renders:
//
//   - the legacy profiles in the order the envelope stores them. The order a user made is intent data
//     and survives the cut-over unchanged (ADR-0013), which for the popup means: the render order IS the
//     storage order, and the runner of the list is the storage's, never the popup's;
//   - the quick-switch ordering (`-quickSwitchProfiles`) when the user has one, which is the legacy
//     quick-switch's own ranking and keeps its meaning;
//   - each profile's type and colour, the two facts `omega_decoration` drew icons with — as data, so the
//     popup's icon is the profile's own fact rather than a guess from the name;
//   - the NEXORA half, absent unless a caller proved it: with no Connector the snapshot carries the
//     stated reason (invariant 1) and the legacy list still renders.
//
// The envelope keys read here (`+<profile>`, `-quickSwitchProfiles`) are the legacy engine's own, read
// and never written: a migration strips nothing, a popup reorders nothing.

export interface LegacyProfileFact {
  displayName: string;
  profileType: 'FixedProfile' | 'PacProfile' | 'RuleListProfile' | 'SwitchProfile';
  color?: string;
}

export const LEGACY_PROFILE_TYPES = ['FixedProfile', 'PacProfile', 'RuleListProfile', 'SwitchProfile'] as const;

export function isLegacyProfileBody(value: unknown): value is Record<string, unknown> & { profileType: LegacyProfileFact['profileType'] } {
  return (
    typeof value === 'object' && value !== null &&
    LEGACY_PROFILE_TYPES.includes((value as Record<string, unknown>).profileType as LegacyProfileFact['profileType'])
  );
}

/** Read the legacy envelope's profile facts, in storage order. `storage` is the same read-only adapter
 * shape the popup entry already uses; nothing here writes. */
export function legacyProfileFacts(envelope: Record<string, unknown>): LegacyProfileFact[] {
  const facts: LegacyProfileFact[] = [];
  for (const [key, value] of Object.entries(envelope)) {
    if (!key.startsWith('+') || key.startsWith('+__ruleListOf')) continue;
    if (!isLegacyProfileBody(value)) continue;
    facts.push({
      displayName: key.slice(1),
      profileType: value.profileType,
      color: typeof value.color === 'string' ? value.color : undefined,
    });
  }
  return facts;
}

/** The quick-switch ranking, when the user made one: the envelope's own order, verbatim. A name the
 * legacy list does not carry is kept in the ranking (the popup renders what the user ordered, and a
 * missing profile is the legacy engine's problem to render, not the popup's to fix). */
export function quickSwitchOrder(envelope: Record<string, unknown>): string[] {
  const raw = envelope['-quickSwitchProfiles'];
  if (!Array.isArray(raw)) return [];
  return raw.filter((name): name is string => typeof name === 'string');
}
