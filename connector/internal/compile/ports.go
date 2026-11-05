package compile

import (
	"fmt"
	"slices"
	"sort"
)

// maxInbounds is docs/architecture/27-config-compilation-pipeline.md §4's bound, taken from the two places the
// ceiling is stated as data — `maxItems` on `packages/schema/protocol/commands/core.start.json#/result/properties/inboundPorts`
// and the same on `packages/schema/protocol/commands/config.apply.json#/result/properties/inboundPorts` — and the
// value itself is `ODEC-06`'s open question. What is not open is the behaviour at the ceiling: §5's consequence row
// says the compiler refuses the config and names the count, and this file is the thing that refuses it.
const maxInbounds = 64

// Range is an inclusive pair of ports. Two of these reach stage 3: the allocation range from
// `connector-settings.schema.json#/properties/ports/properties/range`, and the block range from
// `failure-policy.schema.json#/properties/block/properties/portRange`.
type Range struct {
	Min int
	Max int
}

// Size is how many ports the range offers; a zero or negative size is a malformed document, not an empty choice.
func (r Range) Size() int { return r.Max - r.Min + 1 }

// overlaps is the property `ODEC-04` exists to decide — the two ranges in different files that JSON Schema cannot
// span. This is the "asserted where both are loaded" half of doc 27 §4's last row, and it is a refusal rather than a
// workaround: the disjointness rule says a fail-closed write must never land on a live inbound, and drawing from an
// overlapping range would make every allocation from the overlap a coin flip.
func (r Range) overlaps(o Range) bool {
	return r.Min <= o.Max && o.Min <= r.Max
}

// Allocate is stage 3 of doc 27 §2: one loopback port per selected server tag.
//
// It is a pure function over the tag set, the two ranges and the previously held table, and that shape is the
// contract: §4's reservation row puts the table in the Connector's store, its conflict row promises a re-allocation
// followed by a re-apply, and its persistence row says a restart must not invalidate a `chrome.proxy` setting the
// browser already holds. All three are about a *caller* holding a table and handing it back in, so the allocator
// must be the part with no I/O.
//
// Determinism, in the specific sense the golden files need: tags are visited in lexicographic order regardless of
// the order they arrive in, so the same server set and the same held table produce the same port map on any machine.
// That is not §3.1's rule — this map is not serialized into the document, the document carries the ports it produced
// — but a table that depended on iteration order would put an unstable port into stable-looking bytes.
func Allocate(tags []string, ports, block Range, held map[string]int) (map[string]int, error) {
	if len(tags) == 0 {
		// docs/architecture/28-routing-model.md §6: removing the last profile is a `BadRequest`, not an empty
		// config, so an empty set never reaches a writer.
		return nil, fmt.Errorf("compile: the selection names no server, so there is nothing to allocate; an empty set is a refusal, not an empty config")
	}
	if len(tags) > maxInbounds {
		return nil, fmt.Errorf("compile: the selection needs %d inbounds, past the %d this config may carry; the whole config is refused rather than a subset, which is what QuotaExceeded names",
			len(tags), maxInbounds)
	}
	if ports.Size() <= 0 {
		return nil, fmt.Errorf("compile: the allocation range %d-%d is empty or inverted", ports.Min, ports.Max)
	}
	if block.Size() > 0 && ports.overlaps(block) {
		return nil, fmt.Errorf("compile: the allocation range %d-%d overlaps the block range %d-%d, which would let a fail-closed write point at a live inbound; ODEC-04 decides how the two are kept disjoint",
			ports.Min, ports.Max, block.Min, block.Max)
	}

	ordered := slices.Clone(tags)
	sort.Strings(ordered)

	assigned := make(map[string]int, len(ordered))
	taken := make(map[int]string, len(ordered))
	claim := func(tag string, port int) {
		assigned[tag] = port
		taken[port] = tag
	}

	for _, tag := range ordered {
		if prev, ok := held[tag]; ok && prev >= ports.Min && prev <= ports.Max && taken[prev] == "" {
			claim(tag, prev)
			continue
		}
		// The lowest free port of the range. Nothing here filters the block range out of the candidates, because the
		// overlap check above already refused the only state in which a block port could be a candidate: doc 30 §8
		// says a user-supplied overlap is refused by `config.apply` with `UnsupportedCombination`, and §4's
		// "Fail-closed port never drawn from the allocation range" row holds by that refusal rather than by drawing
		// around it.
		port := 0
		for candidate := ports.Min; candidate <= ports.Max; candidate++ {
			if taken[candidate] != "" {
				continue
			}
			port = candidate
			break
		}
		if port == 0 {
			return nil, fmt.Errorf("compile: no free port left in %d-%d for %s after %d of %d tags were placed",
				ports.Min, ports.Max, tag, len(assigned), len(ordered))
		}
		claim(tag, port)
	}
	return assigned, nil
}
