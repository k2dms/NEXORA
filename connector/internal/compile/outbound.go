// The model→Core translation of one server: `streamSettings` and the blocks inside it. The names here are the
// schema's own `$defs` names — `tlsSettings`, `realitySettings`, `wsSettings`, `grpcSettings`, `xhttpSettings`,
// `h2Settings` — which is the corpus's statement of Xray's spellings, and four of the six are shown at those exact
// positions in docs/architecture/23-unified-server-model.md §9.
package compile

import (
	"fmt"
)

// visionFlow is the only non-empty value `server.schema.json#/properties/flow` admits, and the one whose legality is
// constrained by transport (doc 23 §10's first row after the credential rows: "Vision over WS/XHTTP is unrepresentable").
const visionFlow = "xtls-rprx-vision"

// buildOutbounds is stage 5's outbound half; the servers arrive already sorted by tag from stage 1.
func buildOutbounds(servers []Server) ([]any, error) {
	list := make([]any, 0, len(servers))
	for _, s := range servers {
		doc, err := outboundDocument(s, ServerTag(s.ID))
		if err != nil {
			return nil, err
		}
		list = append(list, doc)
	}
	return list, nil
}

// outboundDocument is the one place the model's vocabulary becomes Xray's. `settings.vnext[0].id` is the value of
// `userIdRef` — a `keystore://` handle, which is what doc 23 §9's shape A prints at that position
// (`⟨secret: userIdRef keystore://server/3f2a1c9e/user-id⟩`) and what docs/architecture/22-component-contracts.md §9
// requires of every layer above the keystore. `encryption: "none"` is stated because all three shapes state it; VLESS
// has no other value in this model.
func outboundDocument(s Server, tag string) (Document, error) {
	if s.Protocol != "vless" {
		return nil, fmt.Errorf("compile: server %s has protocol %q; `server.schema.json` pins the field to the const \"vless\" and an expansion needs an ADR (doc 23 §10)", s.ID, s.Protocol)
	}
	stream, err := streamSettings(s)
	if err != nil {
		return nil, err
	}
	vnext := Document{
		"address":    s.Address,
		"port":       s.Port,
		"id":         s.UserIDRef,
		"encryption": "none",
	}
	if s.Flow != nil {
		vnext["flow"] = *s.Flow
	}
	return Document{
		"tag":            tag,
		"protocol":       s.Protocol,
		"settings":       Document{"vnext": []any{vnext}},
		"streamSettings": stream,
	}, nil
}

// streamSettings is `{"network": …, "security": …}` plus at most one transport block and at most one security block,
// which is what the three shapes show. Both `network` and `security` are required in the schema, so both are always
// emitted; the blocks are emitted only when the model states them.
func streamSettings(s Server) (Document, error) {
	if err := checkFlow(s); err != nil {
		return nil, err
	}
	stream := Document{"network": s.Network, "security": s.Security}

	securityName, security, err := securityBlock(s)
	if err != nil {
		return nil, err
	}
	if security != nil {
		stream[securityName] = security
	}

	network, block, err := transportBlock(s)
	if err != nil {
		return nil, err
	}
	if block != nil {
		stream[network] = block
	}
	return stream, nil
}

// checkFlow is the Go-side mirror of the `allOf` clause doc 23 §10 marks GREEN ("Vision over WS/XHTTP is
// unrepresentable — the first allOf clause + the must-reject example"). It is re-checked here because stage 7's
// "schema" is a claim about the emitted document, and a payload that reached this package from something other than
// ajv would otherwise put a flow in the config that Core ignores or misbehaves over.
func checkFlow(s Server) error {
	if s.Flow == nil || *s.Flow != visionFlow {
		return nil
	}
	if s.Network == "tcp" && (s.Security == "tls" || s.Security == "reality") {
		return nil
	}
	return fmt.Errorf("compile: server %s asks for %s over %s/%s; the schema admits that flow only over tcp with tls or reality (doc 23 §10)",
		s.ID, visionFlow, s.Network, s.Security)
}

// securityBlock returns the Xray block name and its content for `security`, or ("", nil) when the transport carries
// no security at all — the `security: "none"` of `server.plain-tcp.json`, which the shapes of doc 23 §9 do not
// illustrate and which emits nothing beyond the two required fields.
func securityBlock(s Server) (string, Document, error) {
	switch s.Security {
	case "none":
		if s.TLS != nil || s.Reality != nil {
			return "", nil, fmt.Errorf("compile: server %s says security none but carries a tls or reality block", s.ID)
		}
		return "", nil, nil
	case "tls":
		if s.TLS == nil {
			return "", nil, fmt.Errorf("compile: server %s says security tls and carries no tls block; `server.schema.json` requires it", s.ID)
		}
		if s.Reality != nil {
			return "", nil, fmt.Errorf("compile: server %s says security tls and carries a reality block", s.ID)
		}
		if s.TLS.Insecure != nil && *s.TLS.Insecure {
			// `#/$defs/tlsSettings/properties/insecure` is `const false`, so this is a payload that broke the schema
			// rather than a shape it describes. Doc 27 §6: a client that accepts any certificate cannot be spelled in
			// this model, and the field is not silently dropped on the way past.
			return "", nil, fmt.Errorf("compile: server %s asks for tls.insecure true; the schema pins the field to false and the compiler does not drop a request it cannot honour (doc 27 §6)", s.ID)
		}
		return "tlsSettings", tlsSettings(s.TLS), nil
	case "reality":
		if s.Reality == nil {
			return "", nil, fmt.Errorf("compile: server %s says security reality and carries no reality block; REALITY requires publicKey", s.ID)
		}
		if s.TLS != nil {
			return "", nil, fmt.Errorf("compile: server %s says security reality and carries a tls block", s.ID)
		}
		if s.Reality.PublicKey == "" {
			return "", nil, fmt.Errorf("compile: server %s has a reality block with no publicKey, which `#/$defs/realitySettings` requires", s.ID)
		}
		return "realitySettings", realitySettings(s.Reality), nil
	default:
		return "", nil, fmt.Errorf("compile: server %s has security %q, outside the enum none|tls|reality", s.ID, s.Security)
	}
}

// tlsSettings emits `serverName` (required), then the three optional fields when the model states them.
//
// `fingerprint` is uTLS's, and doc 27 §6 says alpn and fingerprint are "the observable shape of the connection, so
// the compiler MUST NOT normalise them" — passed through verbatim, including a fingerprint Core would reject.
func tlsSettings(t *TLSSettings) Document {
	out := Document{"serverName": t.ServerName}
	if len(t.ALPN) > 0 {
		out["alpn"] = t.ALPN
	}
	if t.Fingerprint != "" {
		out["fingerprint"] = t.Fingerprint
	}
	return out
}

// realitySettings carries only public parameters. `publicKey` and `shortId` are not secrets and are not masked —
// doc 23 §9's note under shape B says masking them would make a REALITY handshake failure undiagnosable — and there
// is no `privateKey` field in this model at all, so `packages/schema/examples/invalid/server.reality-with-privatekey.json`
// is refused by Decode before this function is reached.
func realitySettings(r *Reality) Document {
	out := Document{"publicKey": r.PublicKey, "serverName": r.ServerName}
	if r.ShortID != "" {
		out["shortId"] = r.ShortID
	}
	if r.SpiderX != "" {
		out["spiderX"] = r.SpiderX
	}
	if r.Fingerprint != "" {
		out["fingerprint"] = r.Fingerprint
	}
	return out
}

// transportBlock returns the Xray block name and its content for `network`. TCP has no block: doc 23 §9's shape A is
// a tcp outbound whose streamSettings holds `network`, `security` and `tlsSettings` and nothing else.
func transportBlock(s Server) (string, Document, error) {
	switch s.Network {
	case "tcp":
		if s.WS != nil || s.GRPC != nil || s.XHTTP != nil || s.H2 != nil {
			return "", nil, fmt.Errorf("compile: server %s uses network tcp and carries a transport block for another network", s.ID)
		}
		return "", nil, nil
	case "ws":
		if s.WS == nil {
			return "", nil, fmt.Errorf("compile: server %s uses network ws and carries no ws block; `server.schema.json` requires it when network is ws", s.ID)
		}
		if s.WS.HeadersRef != "" {
			return "", nil, unsupportedHeaders(s, "ws")
		}
		block := Document{}
		put(block, "path", s.WS.Path)
		put(block, "host", s.WS.Host)
		return "wsSettings", block, nil
	case "grpc":
		if s.GRPC == nil {
			return "", nil, fmt.Errorf("compile: server %s uses network grpc and carries no grpc block", s.ID)
		}
		if s.GRPC.HeadersRef != "" {
			return "", nil, unsupportedHeaders(s, "grpc")
		}
		block := Document{"serviceName": s.GRPC.ServiceName, "multiMode": s.GRPC.MultiMode}
		put(block, "authority", s.GRPC.Authority)
		return "grpcSettings", block, nil
	case "xhttp":
		if s.XHTTP == nil {
			return "", nil, fmt.Errorf("compile: server %s uses network xhttp and carries no xhttp block", s.ID)
		}
		block := Document{"mode": s.XHTTP.Mode, "path": s.XHTTP.Path}
		put(block, "host", s.XHTTP.Host)
		put(block, "extra", s.XHTTP.Extra)
		return "xhttpSettings", block, nil
	case "h2":
		if s.H2 == nil {
			return "", nil, fmt.Errorf("compile: server %s uses network h2 and carries no h2 block; doc 23 §10 also forces security tls for h2", s.ID)
		}
		block := Document{"path": s.H2.Path}
		put(block, "host", s.H2.Host)
		return "h2Settings", block, nil
	default:
		return "", nil, fmt.Errorf("compile: server %s has network %q, outside the enum tcp|ws|grpc|xhttp|h2", s.ID, s.Network)
	}
}

// unsupportedHeaders is the refusal doc 23 §9's shape C would otherwise paper over. `headersRef` points at an *object*
// — `#/$defs/headersRef` is a `keystore://` string, and the map behind it has key names the schema never lists — so
// the compiler cannot emit the `"headers": {"…": "⟨secret: headersRef⟩"}` that §9 shows: it has the handle, and the
// handle is a string where Core wants an object. Whether the substituted map is inside the hashed bytes or only in
// the written file is ODEC-36, and either answer still needs someone to say where the key names come from.
func unsupportedHeaders(s Server, network string) error {
	return fmt.Errorf("compile: server %s (%s) carries a headersRef for %s, and no decision in this repository says how a keystore:// handle for an object reaches the config; ODEC-36 blocks shape C of doc 23 §9 until it does",
		s.ID, network, s.refOf(network))
}

func (s Server) refOf(network string) string {
	switch network {
	case "grpc":
		return s.GRPC.HeadersRef
	default:
		return s.WS.HeadersRef
	}
}

// put writes a string field only when the model states it, which is the rule the shapes support: every optional field
// that appears in doc 23 §9 has a value in the example that produced it.
func put(doc Document, key, value string) {
	if value != "" {
		doc[key] = value
	}
}
