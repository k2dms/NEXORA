package compile

import (
	"fmt"
	"strings"
	"testing"
)

// The ranges of the two shipped documents: doc 30 §8's table names both, and the tests below use them rather than
// invented numbers so that a change to the contract is a change here too.
var (
	allocRange = Range{Min: 20100, Max: 20899}
	blockRange = Range{Min: 20900, Max: 20999}
)

func tags(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("srv-%08x", i))
	}
	return out
}

// doc 27 §3.1's determinism rule applied to the allocator: the same set in any arrival order is the same table. The
// map is not serialized into the config, but a port that depended on ranging order would put an unstable value into
// stable-looking bytes.
func TestAllocationIsIndependentOfArrivalOrder(t *testing.T) {
	ordered := tags(6)
	reversed := append([]string(nil), ordered...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	first, err := Allocate(ordered, allocRange, blockRange, nil)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	second, err := Allocate(reversed, allocRange, blockRange, nil)
	if err != nil {
		t.Fatalf("Allocate(reversed): %v", err)
	}
	for tag, port := range first {
		if second[tag] != port {
			t.Errorf("%s got %d and %d from the same range in a different order", tag, port, second[tag])
		}
	}
	// Lexicographic tags take the range from its start, lowest tag lowest port.
	for i, tag := range ordered {
		if first[tag] != allocRange.Min+i {
			t.Errorf("%s took port %d, want the %dth port of the range", tag, first[tag], i)
		}
	}
}

// §4's Persistence row: "a Connector restart must not invalidate the proxy configuration the browser already has".
// The held table is the caller's memory, and a tag keeps its port across compiles.
func TestHeldPortsAreReusedInRange(t *testing.T) {
	held := map[string]int{"srv-00000000": 20500, "srv-00000001": 20501}
	got, err := Allocate(tags(3), allocRange, blockRange, held)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got["srv-00000000"] != 20500 || got["srv-00000001"] != 20501 {
		t.Errorf("held ports were not reused: %v", got)
	}
	if got["srv-00000002"] != 20100 {
		t.Errorf("the new tag took %d; a fresh tag takes the lowest free port of the range", got["srv-00000002"])
	}

	// A held port outside the range is not a reservation any more — the user narrowed the range — and a held port two
	// tags claim is one the second must not take.
	moved, err := Allocate(tags(2), allocRange, blockRange, map[string]int{"srv-00000000": 1024, "srv-00000001": 20100})
	if err != nil {
		t.Fatalf("Allocate(out of range): %v", err)
	}
	if moved["srv-00000000"] != 20100 || moved["srv-00000001"] != 20101 {
		t.Errorf("out-of-range and contended held ports resolved to %v", moved)
	}
	if moved["srv-00000000"] == moved["srv-00000001"] {
		t.Error("one port given to two inbounds")
	}
}

// A held entry for a tag that is no longer selected must not block a port: §4's conflict row says the Connector
// re-allocates, and a table that kept ghosts would walk the range forward on every recompile.
func TestHeldPortsForDroppedTagsAreIgnored(t *testing.T) {
	got, err := Allocate(tags(1), allocRange, blockRange, map[string]int{"srv-deadbeef": 20100})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if got["srv-00000000"] != 20100 {
		t.Errorf("a port held by a tag no longer selected was skipped: %v", got)
	}
}

// doc 30 §8's specific sequence: if the block range were allocatable, then after a restart the Compiler could bind
// exactly the port the browser believes is closed, and "blocked" becomes a live unmanaged tunnel. The refusal is the
// rule; §4's "Fail-closed port never drawn from the allocation range" row holds because of it.
func TestAnOverlappingBlockRangeIsRefused(t *testing.T) {
	for _, block := range []Range{{Min: 20899, Max: 20999}, {Min: 20100, Max: 20101}, {Min: 1, Max: 65535}, {Min: 20500, Max: 20500}} {
		_, err := Allocate(tags(2), allocRange, block, nil)
		if err == nil {
			t.Errorf("the block range %d-%d was accepted beside the allocation range", block.Min, block.Max)
			continue
		}
		if !strings.Contains(err.Error(), "ODEC-04") || !strings.Contains(err.Error(), "overlaps") {
			t.Errorf("the overlap refusal names neither the decision nor the property: %v", err)
		}
	}
	// Adjacent is not overlapping: the shipped pair is the boundary case.
	if _, err := Allocate(tags(2), allocRange, blockRange, nil); err != nil {
		t.Errorf("the shipped ranges were refused: %v", err)
	}
}

// doc 27 §4's empty and inverted ranges: `connector-settings.schema.json#/allOf` asserts sanity against the contract,
// and this is the same claim read from whatever the store handed back.
func TestAnEmptyOrInvertedAllocationRangeIsRefused(t *testing.T) {
	for _, ports := range []Range{{Min: 20100, Max: 20099}, {Min: 20899, Max: 20100}} {
		if _, err := Allocate(tags(1), ports, blockRange, nil); err == nil {
			t.Errorf("the allocation range %d-%d was accepted", ports.Min, ports.Max)
		}
	}
	if allocRange.Size() != 800 || blockRange.Size() != 100 {
		t.Errorf("Size() on the shipped ranges gave %d and %d, want 800 and 100", allocRange.Size(), blockRange.Size())
	}
}

// §5's consequence row: "the compiler refuses the config and names the count" — one past the ceiling, at the ceiling
// still working. An empty set is a refusal too, because a config with no inbound is not an empty config.
func TestTheCeilingAndTheEmptySetAreRefusals(t *testing.T) {
	if _, err := Allocate(tags(maxInbounds), allocRange, blockRange, nil); err != nil {
		t.Errorf("exactly %d tags were refused: %v", maxInbounds, err)
	}
	_, err := Allocate(tags(maxInbounds+1), allocRange, blockRange, nil)
	if err == nil {
		t.Fatal("65 inbounds compiled; doc 27 §5 says the whole config is refused, not a subset")
	}
	if !strings.Contains(err.Error(), fmt.Sprint(maxInbounds+1)) || !strings.Contains(err.Error(), "QuotaExceeded") {
		t.Errorf("the refusal does not name the count and the code doc 27 §5 promises: %v", err)
	}
	if _, err := Allocate(nil, allocRange, blockRange, nil); err == nil {
		t.Error("an empty selection allocated nothing instead of refusing")
	}
}

// The range can be legal and still too small — a user who narrowed it to two ports with three servers. Naming the tag
// and how many were already placed is what makes the refusal actionable.
func TestAnExhaustedRangeNamesTheTagItCouldNotPlace(t *testing.T) {
	_, err := Allocate(tags(3), Range{Min: 30000, Max: 30001}, blockRange, nil)
	if err == nil {
		t.Fatal("three inbounds fit in a two-port range")
	}
	for _, want := range []string{"srv-00000002", "no free port", "30000-30001"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}
