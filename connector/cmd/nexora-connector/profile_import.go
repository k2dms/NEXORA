package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/keystore"
)

// profileImport is the single-frame merge ingest door. Intent stays with the
// caller: only protected secrets are persisted, never requests or documents.
// Replace needs a two-sided verified snapshot API this path does not have.
type profileImport struct{ store *keystore.Store }

func newProfileImport(store *keystore.Store) (*profileImport, error) {
	if store == nil {
		return nil, errors.New("profile.import: a keystore is required")
	}
	return &profileImport{store: store}, nil
}

func refuseImport(code, rule string) *dispatch.Refusal {
	return &dispatch.Refusal{Code: code, Message: "profile.import: " + rule}
}

var importCredentialShape = importObject(map[string]importRule{
	"entity":   importEnum("server", "subscription", "profile", "sync", "host"),
	"entityId": importString(0, -1, `^[0-9a-f][0-9a-f-]{6,61}[0-9a-f]$`),
	"field":    importEnum("user-id", "headers", "url", "token"),
	"value": func(v any) bool {
		if _, ok := v.(string); ok {
			return importString(0, 4096, "")(v)
		}
		obj, ok := v.(map[string]any)
		return ok && len(obj) <= 32
	},
}, "entity", "entityId", "field", "value")

var importArgsShape = importObject(map[string]importRule{
	"profiles":          importArray(0, 256, func(any) bool { return true }, false),
	"subscriptions":     importArray(0, 64, func(v any) bool { return subscriptionSourceShape(v) }, false),
	"servers":           importArray(0, 512, func(any) bool { return true }, false),
	"credentials":       importArray(0, 512, importCredentialShape, false),
	"mode":              importEnum("merge", "replace"),
	"backupBeforeWrite": importTrue,
	"contentHash":       func(v any) bool { return v == nil || importString(0, -1, `^([0-9a-f]{64})?$`)(v) },
	// Presence is refused before validation: no chunk staging/reassembly exists.
	"chunk": func(any) bool { return true },
}, "profiles")

type importAnswer struct {
	Imported struct {
		ProfileIDs []string `json:"profileIds"`
		ServerIDs  []string `json:"serverIds"`
	} `json:"imported"`
	Rejected         []importRejection `json:"rejected"`
	SecretsExtracted int               `json:"secretsExtracted"`
}

type importRejection struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

type importSecretRef struct {
	text  string
	field string
	keys  []any
}

// resolveImportOwner accepts an exact declared UUID or one unambiguous declared
// prefix. Store contents never supply owners for an import's credentials.
func resolveImportOwner(prefix string, servers map[string]map[string]any) (string, bool) {
	if _, ok := servers[prefix]; ok {
		return prefix, true
	}
	found := ""
	for id := range servers {
		if strings.HasPrefix(id, prefix) {
			if found != "" {
				return "", false
			}
			found = id
		}
	}
	return found, found != ""
}

func (h *profileImport) Handle(ctx context.Context, f *envelope.Frame) (any, *dispatch.Refusal) {
	if f == nil {
		return nil, refuseImport("BadRequest", "args must be a closed JSON object")
	}
	v, ok := decodeImportJSON(f.Args)
	args, object := v.(map[string]any)
	if !ok || !object {
		return nil, refuseImport("BadRequest", "args must be a closed JSON object")
	}
	if _, chunk := args["chunk"]; chunk || f.Chunk != nil {
		return nil, refuseImport("BadRequest", "chunked import is unavailable; no staging or reassembly is performed")
	}
	if !importArgsShape(args) {
		return nil, refuseImport("BadRequest", "args violate the closed single-frame import contract")
	}
	subscriptionDocs, _ := args["subscriptions"].([]any)
	if len(args["profiles"].([]any)) == 0 && len(subscriptionDocs) == 0 {
		return nil, refuseImport("BadRequest", "an import requires a profile or subscription source")
	}
	if args["mode"] == "replace" {
		return nil, refuseImport("Busy", "replace requires verified restorable snapshots on both sides; that API is unavailable and nothing was written")
	}
	if hash, _ := args["contentHash"].(string); hash != "" {
		return nil, refuseImport("BadRequest", "contentHash verification is unavailable until canonical payload hashing is specified")
	}
	if ctx.Err() != nil {
		return nil, refuseImport("Timeout", "the import was canceled before writes")
	}

	answer := &importAnswer{Rejected: []importRejection{}}
	answer.Imported.ProfileIDs = []string{}
	answer.Imported.ServerIDs = []string{}
	servers := map[string]map[string]any{}
	tags := map[string]bool{}
	serverDocs, _ := args["servers"].([]any)
	for _, doc := range serverDocs {
		if !validateImportServer(doc) {
			return nil, refuseImport("InvalidProfile", "a server violates the Server schema")
		}
		s := doc.(map[string]any)
		id := s["id"].(string)
		if _, duplicate := servers[id]; duplicate {
			return nil, refuseImport("InvalidProfile", "duplicate server identities are not allowed")
		}
		// Match the compiler's derived tag, not a free-form optional tag.
		tag := "srv-" + id[:8]
		if tags[tag] || (s["tag"] != nil && s["tag"] != tag) {
			return nil, refuseImport("InvalidProfile", "server tags collide or disagree with their derived identity")
		}
		tags[tag] = true
		servers[id] = s
		answer.Imported.ServerIDs = append(answer.Imported.ServerIDs, id)
	}
	profileIDs := map[string]bool{}
	for _, doc := range args["profiles"].([]any) {
		if !validateImportProfile(doc) {
			return nil, refuseImport("InvalidProfile", "a profile violates the ConnectorProfile schema")
		}
		p := doc.(map[string]any)
		id := p["id"].(string)
		if profileIDs[id] {
			return nil, refuseImport("InvalidProfile", "duplicate profile identities are not allowed")
		}
		profileIDs[id] = true
		member := map[string]bool{}
		for _, value := range p["serverIds"].([]any) {
			sid := value.(string)
			if _, exists := servers[sid]; !exists {
				return nil, refuseImport("InvalidProfile", "a profile references a server absent from this payload")
			}
			member[sid] = true
		}
		if def, present := p["defaultServerId"]; present && !member[def.(string)] {
			return nil, refuseImport("InvalidProfile", "defaultServerId must belong to serverIds")
		}
		answer.Imported.ProfileIDs = append(answer.Imported.ProfileIDs, id)
	}

	refs := map[string]importSecretRef{}
	byOwnerField := map[string]importSecretRef{}
	addRef := func(text, owner, field string, keys []any) bool {
		r, err := keystore.Parse(text)
		if err != nil || r.Entity != "server" || r.Field != field {
			return false
		}
		resolved, ok := resolveImportOwner(r.ID, servers)
		if !ok || resolved != owner {
			return false
		}
		if _, duplicate := refs[text]; duplicate {
			return false
		}
		pair := owner + "/" + field
		if _, collision := byOwnerField[pair]; collision {
			return false
		}
		descriptor := importSecretRef{text: r.String(), field: field, keys: keys}
		refs[text], byOwnerField[pair] = descriptor, descriptor
		return true
	}
	// Inspect all blocks, not just active transports or enabled profiles.
	for _, doc := range serverDocs {
		s := doc.(map[string]any)
		owner := s["id"].(string)
		if !addRef(s["userIdRef"].(string), owner, "user-id", nil) {
			return nil, refuseImport("InvalidProfile", "a user-id reference has an invalid owner, field or collision")
		}
		for _, name := range []string{"ws", "grpc"} {
			block, exists := s[name].(map[string]any)
			if !exists {
				continue
			}
			text, hasRef := block["headersRef"].(string)
			keys, hasKeys := block["headersKeys"].([]any)
			if hasRef != hasKeys || (hasRef && !addRef(text, owner, "headers", keys)) {
				return nil, refuseImport("InvalidProfile", "headers require a matching reference and exact declared keys without collisions")
			}
		}
	}

	subscriptions := map[string]map[string]any{}
	for _, doc := range subscriptionDocs {
		source := doc.(map[string]any)
		owner := source["id"].(string)
		if _, duplicate := subscriptions[owner]; duplicate {
			return nil, refuseImport("InvalidProfile", "duplicate subscription identities are not allowed")
		}
		subscriptions[owner] = source
	}
	for owner, source := range subscriptions {
		text, declared := source["urlRef"].(string)
		if !declared {
			continue
		}
		r, err := keystore.Parse(text)
		resolved, found := resolveImportOwner(r.ID, subscriptions)
		if err != nil || r.Entity != "subscription" || r.Field != "url" || !found || resolved != owner {
			return nil, refuseImport("InvalidProfile", "a subscription URL reference has an invalid owner or field")
		}
		if _, collision := refs[text]; collision {
			return nil, refuseImport("InvalidProfile", "subscription URL references collide")
		}
		descriptor := importSecretRef{text: r.String(), field: "url"}
		refs[text], byOwnerField[owner+"/url"] = descriptor, descriptor
	}

	values := map[string][]byte{}
	credentials, _ := args["credentials"].([]any)
	for _, entry := range credentials {
		c := entry.(map[string]any)
		field := c["field"].(string)
		owners := servers
		allowed := c["entity"] == "server" && (field == "user-id" || field == "headers")
		if c["entity"] == "subscription" {
			owners = subscriptions
			allowed = field == "url"
		}
		owner, found := resolveImportOwner(c["entityId"].(string), owners)
		if !allowed || !found {
			return nil, refuseImport("InvalidProfile", "a credential has no supported owner and field in this payload")
		}
		r, referenced := byOwnerField[owner+"/"+field]
		if !referenced {
			return nil, refuseImport("InvalidProfile", "a credential has no reference in its server document")
		}
		if _, duplicate := values[r.text]; duplicate {
			return nil, refuseImport("InvalidProfile", "duplicate credentials or reference collisions are not allowed")
		}
		value := c["value"]
		if !validateImportSecret(r, value) {
			return nil, refuseImport("InvalidProfile", "a credential violates its field shape or declared header keys")
		}
		if field != "headers" {
			values[r.text] = []byte(value.(string))
		} else {
			// JSON encoding a validated string map cannot fail.
			values[r.text], _ = json.Marshal(value)
		}
	}
	for text, r := range refs {
		if _, supplied := values[text]; supplied {
			continue
		}
		secret, err := h.store.Resolve(text)
		if err != nil {
			return nil, importStoreRefusal(err)
		}
		var value any = string(secret.Value)
		if r.field == "headers" {
			var valid bool
			value, valid = decodeImportJSON(secret.Value)
			if !valid {
				return nil, refuseImport("InvalidProfile", "stored headers violate their field shape")
			}
		}
		if !validateImportSecret(r, value) {
			return nil, refuseImport("InvalidProfile", "a stored secret violates its field shape or declared header keys")
		}
	}
	if ctx.Err() != nil {
		return nil, refuseImport("Timeout", "the import was canceled before writes")
	}
	// Every document and credential has passed before the first write. The
	// store serializes staging, installation and error rollback with readers.
	if err := h.store.PutBatch(values); err != nil {
		return nil, importStoreRefusal(err)
	}
	answer.SecretsExtracted = len(values)
	return answer, nil
}

func validateImportSecret(ref importSecretRef, value any) bool {
	if ref.field == "url" {
		text, ok := value.(string)
		if !ok || !importString(1, 4096, "")(text) {
			return false
		}
		address, err := url.Parse(text)
		return err == nil && address.Scheme == "https" && address.Hostname() != "" && address.User == nil && address.Fragment == "" && address.Opaque == ""
	}
	if ref.field == "user-id" {
		return importUserID(value)
	}
	if !validateImportHeaders(value) {
		return false
	}
	headers := value.(map[string]any)
	if len(headers) != len(ref.keys) {
		return false
	}
	for _, key := range ref.keys {
		if _, present := headers[key.(string)]; !present {
			return false
		}
	}
	return true
}

func importStoreRefusal(err error) *dispatch.Refusal {
	code := "KeystoreUnavailable"
	if errors.Is(err, keystore.ErrNotFound) {
		code = "InvalidProfile"
	}
	// Never quote errors from a protector, filesystem or decoder: even an
	// injected protector can include secret bytes in its failure message.
	return refuseImport(code, "secret resolution or batch storage failed; the import did not complete")
}
