// The PopupShell's snapshot (doc 22 §13): profiles, applied state, health — the shapes cross the
// custom-element boundary as attributes, so they stay JSON-serialisable and derived from the same
// contract data the rest of the extension reads. Invariant 1: the snapshot exists with no Connector —
// the legacy list is present and the NEXORA half explains why it is unavailable.

export interface PopupProfile {
  /** The profile's own display name; the legacy list orders by the user's ordering. */
  displayName: string;
  /** NEXORA profile id, present only when the legacy profile has a migrated counterpart. */
  nexoraId?: string;
  /** The source of truth this row is read from: legacy (the legacy engine keeps serving, ADR-0013) or nexora. */
  plane: 'legacy' | 'nexora';
  selected: boolean;
}

export interface PopupSnapshot {
  profiles: PopupProfile[];
  /** The applied session's evidence, present only when the Connector proved one. */
  applied?: { serverName: string; configHash: string };
  /** Health dot: the two sources doc 37 §2 names — connector.health state and core.status containment. */
  health: 'unknown' | 'healthy' | 'degraded' | 'down';
  /** Why the NEXORA half is unavailable, shown verbatim when the Connector is absent. */
  nexoraUnavailableReason?: string;
  phase: 'disconnected' | 'connecting' | 'connected' | 'blocked' | 'disconnecting';
}

/** Build the snapshot with no Connector at all: invariant 1's answer. The legacy list is whatever the
 * legacy engine serves; the NEXORA half is unavailable for a stated reason, never silently absent. */
export function snapshotWithoutConnector(legacyProfiles: string[], reason: string): PopupSnapshot {
  return {
    profiles: legacyProfiles.map((name, index) => ({
      displayName: name,
      plane: 'legacy' as const,
      selected: index === 0,
    })),
    health: 'unknown',
    nexoraUnavailableReason: reason,
    phase: 'disconnected',
  };
}
