// Stage 7 of docs/architecture/27-config-compilation-pipeline.md §2, the half that needs no Core. The stage is
// described as "schema + 'every inbound listens on 127.0.0.1' + probe", and §2 calls it "what makes ADR-0006 a fact
// instead of an intention: the compiled document is inspected before Core ever sees it, so a non-loopback bind is a
// build-time rejection, not a runtime exposure".
//
// The schema half is not here: `packages/schema/scripts/verify.mjs` validates the contract files and the examples,
// and a Go-side validator of the same schemas would be a second implementation to disagree with. What is here is the
// inspection of the *emitted document* — not of the settings it was built from — because the claim is about the bytes
// Core loads. The probe needs a running Core and is owed to Phase 2 with the golden files.
package compile

import (
	"fmt"
)

// validate walks the emitted inbounds and refuses any that is not a loopback bind, plus an inbound list that is empty
// or longer than the bound `packages/schema/protocol/commands/config.apply.json#/result/properties/inboundPorts`
// declares — the same 64 stage 3 refuses at, checked again on the way out because the two lists are built by
// different functions.
func validate(doc Document, inbounds []Inbound) error {
	if len(inbounds) == 0 {
		return fmt.Errorf("compile: the emitted document has no inbound, so nothing could reach the outbounds it declares")
	}
	if len(inbounds) > maxInbounds {
		return fmt.Errorf("compile: the emitted document has %d inbounds, past the %d the apply result may report", len(inbounds), maxInbounds)
	}
	list, ok := doc["inbounds"].([]any)
	if !ok || len(list) != len(inbounds) {
		return fmt.Errorf("compile: internal — the document carries %d inbounds and the result carries %d; the two are built from one table and must agree", len(list), len(inbounds))
	}
	for i, entry := range list {
		inbound, ok := entry.(Document)
		if !ok {
			return fmt.Errorf("compile: internal — inbound #%d is not an object", i)
		}
		switch listen := inbound["listen"].(type) {
		case string:
			if listen != loopbackHost {
				return fmt.Errorf("compile: inbound #%d would listen on %q; ADR-0006 admits %s, and doc 27 §2 says this is caught before Core sees the file", i, listen, loopbackHost)
			}
		default:
			return fmt.Errorf("compile: inbound #%d has no listen address it names, which is the field ADR-0006 is enforced on", i)
		}
		if port, ok := inbound["port"].(int); !ok || port < 1 || port > 65535 {
			return fmt.Errorf("compile: inbound #%d carries the port %v, outside 1-65535", i, inbound["port"])
		}
	}
	return nil
}
