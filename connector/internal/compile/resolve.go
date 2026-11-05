package compile

import (
	"fmt"
	"sort"
	"strings"
)

// Resolved is stage 1's output: the servers the compiled config must carry, with the tag each one is emitted under.
type Resolved struct {
	Servers []Server
	Tags    []string
}

// ServerTag derives the outbound tag from the server id: the first eight characters of the UUID
// (docs/architecture/23-unified-server-model.md §6). It is derived and never read, because §6 names the two things
// that cannot be allowed to produce a tag — a `displayName`, which is user-editable and appears in no config, and a
// random string, which a re-import would change.
func ServerTag(id string) string { return "srv-" + shortHex(id) }

func shortHex(id string) string {
	if len(id) < 8 {
		return ""
	}
	return strings.ToLower(id[:8])
}

// Resolve is stage 1 of doc 27 §2 — "every `-nexora.server.<id>` exists; dangling ⇒ `InvalidProfile`" — and the
// place the cross-key integrity the schemas cannot state is finally checked. JSON Schema sees one envelope key at a
// time, so a profile naming a server that does not exist is a valid document (`VER-31`, `docs/architecture/28-routing-model.md`
// §9's LIVE-15 row), and the only honest answer is a refusal naming both ids.
//
// The set is the union over the *enabled* profiles: `enabled` defaults to true and a soft-off profile must not hold
// a port (`docs/architecture/23-unified-server-model.md` §8). A server referenced by two profiles appears once — §4's
// reservation row says two profiles must not allocate two ports for one tag, and that starts here, at one tag.
func Resolve(profiles []Profile, servers []Server, routing *RoutingPolicy) (*Resolved, error) {
	byID := make(map[string]Server, len(servers))
	for _, s := range servers {
		if _, dup := byID[s.ID]; dup {
			return nil, fmt.Errorf("compile: two servers are stored under the same id %s", s.ID)
		}
		byID[s.ID] = s
	}

	byTag := make(map[string]string, len(servers))
	selected := make(map[string]Server)
	for _, p := range profiles {
		if p.Enabled != nil && !*p.Enabled {
			continue
		}
		if p.Kind != "connector" {
			return nil, fmt.Errorf("compile: profile %s has kind %q; the pipeline compiles `connector` profiles only, and a legacy `+`-keyed profile is not one (doc 23 §2)", p.ID, p.Kind)
		}
		if len(p.ServerIDs) == 0 {
			return nil, fmt.Errorf("compile: profile %s names no server, and `serverIds` has minItems 1", p.ID)
		}
		for _, id := range p.ServerIDs {
			s, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("compile: InvalidProfile — profile %s references server %s, which is not in the registry", p.ID, id)
			}
			tag := ServerTag(id)
			if shortHex(id) == "" {
				return nil, fmt.Errorf("compile: profile %s references the server id %s, from which no `srv-<8 hex>` tag derives", p.ID, id)
			}
			if s.Tag != "" && s.Tag != tag {
				return nil, fmt.Errorf("compile: server %s is stored with tag %q but its id derives %q; the tag is derived, not stored (doc 23 §6)", id, s.Tag, tag)
			}
			if other, clash := byTag[tag]; clash && other != id {
				// VER-70's claim, and the reason the check is on the whole set rather than per profile: two UUIDs
				// sharing a prefix would otherwise put two outbounds under one tag in the same file and Core would
				// route to whichever it happened to find.
				return nil, fmt.Errorf("compile: tag collision — servers %s and %s both derive %s; both ids are named because neither is the mistake", other, id, tag)
			}
			byTag[tag] = id
			selected[id] = s
		}
		if p.Selection.Mode == "fixed" {
			if p.DefaultServerID == "" {
				return nil, fmt.Errorf("compile: profile %s selects with mode fixed and names no defaultServerId", p.ID)
			}
			if !contains(p.ServerIDs, p.DefaultServerID) {
				return nil, fmt.Errorf("compile: profile %s defaults to server %s, which is not in its own serverIds", p.ID, p.DefaultServerID)
			}
		}
		if p.RoutingPolicyID != "" {
			if routing == nil {
				return nil, fmt.Errorf("compile: profile %s references routing policy %s, and the caller gave the pipeline none", p.ID, p.RoutingPolicyID)
			}
			if p.RoutingPolicyID != routing.ID {
				return nil, fmt.Errorf("compile: profile %s references routing policy %s while the caller supplied %s; one compiled config carries one routing section, so two policies cannot both apply", p.ID, p.RoutingPolicyID, routing.ID)
			}
		}
	}

	out := make([]Server, 0, len(selected))
	tags := make([]string, 0, len(selected))
	for _, s := range selected {
		out = append(out, s)
		tags = append(tags, ServerTag(s.ID))
	}
	// Sorted by tag, not by id or by the order the registry arrived in: the arrays this feeds are serialized into
	// the bytes the hash covers, and doc 27 §3.1 makes ordering a property of the emitted file. `serverIds` order is
	// meaningful for `mode: ordered` (doc 27 §3.5) and stays where it belongs — in the profile, on the browser side.
	sort.Slice(out, func(i, j int) bool { return ServerTag(out[i].ID) < ServerTag(out[j].ID) })
	sort.Strings(tags)
	return &Resolved{Servers: out, Tags: tags}, nil
}

func contains(hay []string, needle string) bool {
	for _, v := range hay {
		if v == needle {
			return true
		}
	}
	return false
}
