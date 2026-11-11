package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var errSubscriptionQuota = errors.New("subscription: server quota exceeded")

// Display text is untrusted too: providers sometimes copy credentials or whole
// share links into the fragment. Examine it before truncation, not afterwards.
var subscriptionSecretText = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|(^|[^0-9a-f])[0-9a-f]{32}([^0-9a-f]|$)|[a-z][a-z0-9+.-]*://`)

// parseSubscription is an in-memory boundary. Only values contains credentials;
// callers must wait for successful validation, quota and collision checks before
// persisting either the public documents or the keystore batch.
func parseSubscription(body []byte, declaredFormat, sourceID string, maxServers int) (servers []map[string]any, values map[string][]byte, resolvedFormat string, dropped int, err error) {
	if !importUUID(sourceID) {
		return nil, nil, "", 0, errors.New("subscription: invalid source identity")
	}
	if maxServers < 1 {
		return nil, nil, "", 0, errSubscriptionQuota
	}
	text := strings.TrimSpace(string(body))
	resolvedFormat = declaredFormat
	if declaredFormat == "auto" {
		resolvedFormat = "vless-base64"
		// URI lists cannot be strict base64 (':' is outside either alphabet).
		// Comments and unsupported protocols must still be refused per entry.
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				if strings.Contains(line, "://") {
					resolvedFormat = "vless-uri-list"
					break
				}
			}
		}
	}
	switch resolvedFormat {
	case "vless-base64":
		decoded, ok := decodeSubscriptionBase64(text)
		if !ok {
			return nil, nil, resolvedFormat, 0, errors.New("subscription: invalid base64 payload")
		}
		text = strings.TrimSpace(string(decoded))
		// Never unwrap again, even if the first decoding succeeded.
		if _, wrapped := decodeSubscriptionBase64(text); wrapped {
			return nil, nil, resolvedFormat, 0, errors.New("subscription: nested base64 payload refused")
		}
	case "vless-uri-list":
	default:
		return nil, nil, "", 0, errors.New("subscription: unsupported format")
	}
	if text == "" {
		return nil, nil, resolvedFormat, 0, errors.New("subscription: empty payload")
	}

	servers = []map[string]any{}
	values = map[string][]byte{}
	// Raw UUID equality is used only in memory. Neither it nor a secret digest
	// enters the public ID: distinct credentials share an identity's ordinals.
	seen := map[string]map[string]bool{}
	tags := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		server, credential, ok := parseSubscriptionVLESS(line)
		if !ok {
			dropped++
			continue
		}
		// This map contains exclusively normalized public connection fields.
		identityJSON, _ := json.Marshal(server)
		identity := string(identityJSON)
		credentials := seen[identity]
		if credentials == nil {
			credentials = map[string]bool{}
			seen[identity] = credentials
		}
		if credentials[credential] {
			dropped++
			continue
		}
		id := subscriptionServerID(sourceID, identity, len(credentials))
		ref := "keystore://server/" + id + "/user-id"
		server["id"], server["userIdRef"] = id, ref
		server["source"], server["subscriptionId"] = "subscription", sourceID
		u, _ := url.Parse(line) // Already parsed successfully above.
		server["displayName"] = subscriptionDisplayName(u.Fragment, server["address"].(string), server["port"].(int))
		data, marshalErr := json.Marshal(server)
		decoded, decodedOK := decodeImportJSON(data)
		if marshalErr != nil || !decodedOK || !validateImportServer(decoded) {
			dropped++
			continue
		}
		if len(servers) >= maxServers {
			return nil, nil, resolvedFormat, dropped, errSubscriptionQuota
		}
		// The compiler uses the first eight hex digits as its outbound tag.
		if tags[id[:8]] {
			return nil, nil, resolvedFormat, dropped, errors.New("subscription: derived server tags collide")
		}
		tags[id[:8]] = true
		credentials[credential] = true
		servers = append(servers, server)
		values[ref] = []byte(credential)
	}
	if len(servers) == 0 {
		return nil, nil, resolvedFormat, dropped, errors.New("subscription: no supported servers")
	}
	return servers, values, resolvedFormat, dropped, nil
}

func decodeSubscriptionBase64(text string) ([]byte, bool) {
	if text == "" {
		return nil, false
	}
	// Go's decoder ignores CR/LF even in Strict mode. Reject internal whitespace
	// explicitly so a malformed wrapper cannot be silently repaired.
	for _, r := range text {
		if unicode.IsSpace(r) {
			return nil, false
		}
	}
	encoding := base64.StdEncoding
	if strings.ContainsAny(text, "-_") {
		encoding = base64.URLEncoding
	}
	if !strings.Contains(text, "=") {
		encoding = encoding.WithPadding(base64.NoPadding)
	}
	decoded, err := encoding.Strict().DecodeString(text)
	return decoded, err == nil
}

// parseSubscriptionVLESS returns only the connection identity, never a name or
// credential field. Unsupported/inactive parameters are refused, not discarded.
func parseSubscriptionVLESS(line string) (map[string]any, string, bool) {
	u, err := url.Parse(line)
	if err != nil || u.Scheme != "vless" || u.Opaque != "" || u.User == nil || u.Path != "" || !utf8.ValidString(line) {
		return nil, "", false
	}
	credential := u.User.Username()
	if _, password := u.User.Password(); password || !importUserID(credential) {
		return nil, "", false
	}
	address, portText, err := net.SplitHostPort(u.Host)
	if err != nil || address == "" {
		return nil, "", false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || strings.Trim(portText, "0123456789") != "" {
		return nil, "", false
	}
	if ip := net.ParseIP(address); ip != nil {
		if strings.HasPrefix(u.Host, "[") && !strings.Contains(address, ":") {
			return nil, "", false // Brackets are reserved for IPv6 literals.
		}
		address = ip.String()
	} else {
		if strings.HasPrefix(u.Host, "[") {
			return nil, "", false
		}
		if strings.Contains(address, ":") || !importHost(address) {
			return nil, "", false
		}
		address = strings.ToLower(strings.TrimSuffix(address, "."))
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, "", false
	}
	for key, list := range query {
		if len(list) != 1 || !utf8.ValidString(list[0]) {
			return nil, "", false
		}
		switch key {
		case "type", "security", "flow", "encryption", "headerType", "sni", "fp", "alpn", "pbk", "sid", "spx":
		default:
			// Includes insecure, certificate/pinning and unknown socket options.
			return nil, "", false
		}
	}
	network := query.Get("type")
	if _, present := query["type"]; !present {
		network = "tcp" // VLESS URI default; raw is Xray's TCP alias.
	}
	if network != "tcp" && network != "raw" {
		return nil, "", false
	}
	for _, key := range []string{"encryption", "headerType"} {
		if _, present := query[key]; present && query.Get(key) != "none" {
			return nil, "", false
		}
	}
	security := query.Get("security")
	if _, present := query["security"]; !present {
		security = "none"
	}
	server := map[string]any{"protocol": "vless", "address": address, "port": port, "network": "tcp", "security": security, "flow": query.Get("flow")}
	block := map[string]any{}
	switch security {
	case "none":
		for _, key := range []string{"sni", "fp", "alpn", "pbk", "sid", "spx"} {
			if _, present := query[key]; present {
				return nil, "", false
			}
		}
	case "tls", "reality":
		// Schema requires SNI. Do not guess it from an IP address or HTTP host.
		block["serverName"] = strings.ToLower(strings.TrimSuffix(query.Get("sni"), "."))
		if _, present := query["fp"]; present {
			block["fingerprint"] = query.Get("fp")
		}
		if security == "tls" {
			for _, key := range []string{"pbk", "sid", "spx"} {
				if _, present := query[key]; present {
					return nil, "", false
				}
			}
			if _, present := query["alpn"]; present {
				block["alpn"] = strings.Split(query.Get("alpn"), ",")
			}
		} else {
			if _, present := query["alpn"]; present {
				return nil, "", false // REALITY has no ALPN field in the current schema.
			}
			block["publicKey"] = query.Get("pbk")
			block["shortId"] = strings.ToLower(query.Get("sid"))
			block["spiderX"] = query.Get("spx")
		}
		server[security] = block
	default:
		return nil, "", false
	}
	// Prevent a credential copied into supposedly public connection fields from
	// leaking through documents or their identity hash. This is not a secret hash.
	publicJSON, _ := json.Marshal(server)
	publicText := strings.ToLower(string(publicJSON))
	if subscriptionSecretText.MatchString(address) || strings.Contains(publicText, strings.ToLower(credential)) || strings.Contains(publicText, strings.ToLower(strings.ReplaceAll(credential, "-", ""))) {
		return nil, "", false
	}
	return server, credential, true
}

func subscriptionServerID(sourceID, identity string, ordinal int) string {
	// JSON framing avoids delimiter ambiguities. Display names and UUID values
	// are deliberately absent so renames and credential rotation preserve IDs.
	data, _ := json.Marshal([]any{sourceID, identity, ordinal})
	sum := sha256.Sum256(data)
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func subscriptionDisplayName(fragment, address string, port int) string {
	name := strings.TrimSpace(fragment)
	if name == "" || !utf8.ValidString(name) || subscriptionSecretText.MatchString(name) || strings.Contains(name, "%") || strings.Contains(strings.ToLower(name), "vless:") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		name = net.JoinHostPort(address, strconv.Itoa(port))
	}
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	return name
}
