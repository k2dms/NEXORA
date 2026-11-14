import type { SavedProfile } from '../model.ts';

// The extension-side registry load (docs/architecture/22-component-contracts.md §6, invariants 3 and 4):
// JSON Schema sees one envelope key at a time, so a profile whose `serverIds` name a server that no
// `-nexora.server.*` key holds is a valid document (`VER-31`). The registry performs the cross-entity
// referential check at load and refuses the profile with a named reason — a profile a sync merge restored
// ahead of its servers must not brick the whole registry, so a refusal removes one profile and names what
// was missing, and never throws.

export type RegistryRefusal = {
  profileId: string;
  reason: string;
};

export type RegistryLoad = {
  profiles: SavedProfile[];
  refused: RegistryRefusal[];
};

export type RegistryEntities = {
  profiles: SavedProfile[];
  /** The ids every `-nexora.server.*` key in the envelope holds. */
  serverIds: Iterable<string>;
};

export function loadRegistry(entities: RegistryEntities): RegistryLoad {
  const servers = new Set(entities.serverIds);
  const profiles: SavedProfile[] = [];
  const refused: RegistryRefusal[] = [];

  for (const profile of entities.profiles) {
    const reason = profileReason(profile, servers);
    if (reason === null) {
      profiles.push(profile);
    } else {
      refused.push({ profileId: profile.id, reason });
    }
  }
  return { profiles, refused };
}

function profileReason(profile: SavedProfile, servers: Set<string>): string | null {
  // Duplicates: the schema states uniqueItems, but the check here is the load-time backstop — a merge that
  // concatenated two copies of one profile must not survive it either.
  const seen = new Set<string>();
  for (const id of profile.serverIds) {
    if (seen.has(id)) {
      return `profile ${profile.id} lists server ${id} twice`;
    }
    seen.add(id);
    if (!servers.has(id)) {
      return `profile ${profile.id} references server ${id}, which no -nexora.server.* key holds`;
    }
  }
  if (!servers.has(profile.defaultServerId)) {
    return `profile ${profile.id} defaults to server ${profile.defaultServerId}, which no -nexora.server.* key holds`;
  }
  if (!seen.has(profile.defaultServerId)) {
    return `profile ${profile.id} defaults to server ${profile.defaultServerId}, which is not in its own serverIds`;
  }
  return null;
}
