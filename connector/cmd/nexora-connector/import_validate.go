package main

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/k2dms/nexora/connector/internal/compile"
)

// These boundary rules mirror server.schema.json, connector-profile.schema.json,
// and failure-policy.schema.json#/$defs/ProfilePolicy. Typed decoding alone cannot
// distinguish absent, null and false in zero-valued structs. Rules examine the
// original JSON types and presence, including unused blocks.
type importRule func(any) bool

func importObject(fields map[string]importRule, required ...string) importRule {
	return func(v any) bool {
		obj, ok := v.(map[string]any)
		if !ok {
			return false
		}
		for _, key := range required {
			if _, ok := obj[key]; !ok {
				return false
			}
		}
		for key, value := range obj {
			rule, known := fields[key]
			if !known || !rule(value) {
				return false
			}
		}
		return true
	}
}

func importString(min, max int, pattern string) importRule {
	var re *regexp.Regexp
	if pattern != "" {
		re = regexp.MustCompile(pattern)
	}
	return func(v any) bool {
		s, ok := v.(string)
		if !ok {
			return false
		}
		n := utf8.RuneCountInString(s)
		return n >= min && (max < 0 || n <= max) && (re == nil || re.MatchString(s))
	}
}

func importEnum(allowed ...string) importRule {
	return func(v any) bool {
		s, ok := v.(string)
		if !ok {
			return false
		}
		for _, value := range allowed {
			if s == value {
				return true
			}
		}
		return false
	}
}

func importInteger(min, max int64) importRule {
	return func(v any) bool {
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		// Normalize decimal digits without floating-point rounding or a big
		// integer allocation driven by an untrusted exponent (e.g. 1e999999999).
		s := string(n)
		negative := strings.HasPrefix(s, "-")
		s = strings.TrimPrefix(s, "-")
		mantissa, exponent, _ := strings.Cut(strings.ToLower(s), "e")
		fraction := 0
		if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
			fraction = len(mantissa) - dot - 1
			mantissa = mantissa[:dot] + mantissa[dot+1:]
		}
		digits := strings.TrimLeft(mantissa, "0")
		if digits == "" {
			return min <= 0 && max >= 0
		}
		exp := int64(0)
		if exponent != "" {
			var err error
			exp, err = strconv.ParseInt(exponent, 10, 64)
			if err != nil || exp < -int64(len(mantissa)) || exp > int64(len(mantissa))+19 {
				return false
			}
		}
		trimmed := strings.TrimRight(digits, "0")
		scale := exp - int64(fraction) + int64(len(digits)-len(trimmed))
		if scale < 0 || scale > 19 || int64(len(trimmed))+scale > 19 {
			return false
		}
		digits = trimmed + strings.Repeat("0", int(scale))
		if negative {
			digits = "-" + digits
		}
		value, err := strconv.ParseInt(digits, 10, 64)
		return err == nil && value >= min && value <= max
	}
}

func importArray(min, max int, item importRule, unique bool) importRule {
	return func(v any) bool {
		list, ok := v.([]any)
		if !ok || len(list) < min || len(list) > max {
			return false
		}
		seen := map[string]bool{}
		for _, value := range list {
			if !item(value) {
				return false
			}
			if unique {
				// All unique arrays in these schemas have string items.
				s, ok := value.(string)
				if !ok || seen[s] {
					return false
				}
				seen[s] = true
			}
		}
		return true
	}
}

func importBool(v any) bool  { _, ok := v.(bool); return ok }
func importTrue(v any) bool  { b, ok := v.(bool); return ok && b }
func importFalse(v any) bool { b, ok := v.(bool); return ok && !b }

var (
	importUUID = importString(0, -1, `^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	// Credential UUIDs are not entity IDs: the command does not restrict
	// their version or hex case as the Server/Profile identity schemas do.
	importUserID      = importString(36, 36, `^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	importRef         = importString(0, -1, `^keystore://[A-Za-z0-9._:/-]{1,96}$`)
	importHost        = importString(1, 253, `^[A-Za-z0-9.-]+$`)
	importPath        = importString(1, 255, `^/[A-Za-z0-9._~%!$&'()*+,;=:@/?-]*$`)
	importHeaderKeys  = importArray(1, 32, importString(1, 128, ""), true)
	importDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt]([01][0-9]|2[0-3]):[0-5][0-9]:([0-5][0-9]|60)(\.[0-9]+)?([Zz]|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)
)

func importDateTime(v any) bool {
	s, ok := v.(string)
	if !ok || !importDatePattern.MatchString(s) {
		return false
	}
	s = strings.ToUpper(s)
	leap := s[17:19] == "60"
	if leap {
		s = s[:17] + "59" + s[19:]
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return false
	}
	// RFC3339 permits leap seconds at the end of a UTC day, including
	// their equivalent local times when a numeric offset is used.
	return !leap || (t.UTC().Hour() == 23 && t.UTC().Minute() == 59)
}

var importServerShape = importObject(map[string]importRule{
	"id":          importUUID,
	"displayName": importString(1, 80, ""),
	"protocol":    importEnum("vless"),
	"address":     importString(1, 253, `^[A-Za-z0-9.\-:\[\]a-f0-9]+$`),
	"port":        importInteger(1, 65535),
	"userIdRef":   importRef,
	"network":     importEnum("tcp", "ws", "grpc", "xhttp", "h2"),
	"security":    importEnum("none", "tls", "reality"),
	"flow":        importEnum("", "xtls-rprx-vision"),
	"tls": importObject(map[string]importRule{
		"serverName":  importHost,
		"alpn":        importArray(1, 3, importEnum("h3", "h2", "http/1.1"), true),
		"fingerprint": importString(1, 32, ""),
		"insecure":    importFalse,
	}, "serverName"),
	"reality": importObject(map[string]importRule{
		"publicKey":   importString(0, -1, `^[A-Za-z0-9_-]{43}$`),
		"serverName":  importHost,
		"shortId":     importString(0, -1, `^([0-9a-f]{2}){0,4}$`),
		"spiderX":     importString(0, 64, ""),
		"fingerprint": importString(1, 32, ""),
	}, "publicKey", "serverName"),
	"ws": importObject(map[string]importRule{
		"path": importPath, "host": importHost, "headersRef": importRef, "headersKeys": importHeaderKeys,
	}),
	"grpc": importObject(map[string]importRule{
		"serviceName": importString(1, 128, `^[A-Za-z0-9._/-]+$`),
		"multiMode":   importBool, "authority": importHost, "headersRef": importRef, "headersKeys": importHeaderKeys,
	}, "serviceName"),
	"xhttp": importObject(map[string]importRule{
		"mode": importEnum("auto", "str", "packet-up"), "path": importPath,
		"host": importHost, "extra": importString(0, 256, ""),
	}, "mode", "path"),
	"h2":             importObject(map[string]importRule{"path": importPath, "host": importHost}, "path"),
	"tag":            importString(0, -1, `^srv-[0-9a-f]{8}$`),
	"notes":          importString(0, 200, ""),
	"source":         importEnum("manual", "subscription", "import"),
	"subscriptionId": importUUID,
	"createdAt":      importDateTime, "updatedAt": importDateTime,
}, "id", "displayName", "protocol", "address", "port", "network", "security", "userIdRef")

func validateImportServer(v any) bool {
	if !importServerShape(v) {
		return false
	}
	s := v.(map[string]any)
	network, security := s["network"].(string), s["security"].(string)
	_, tls := s["tls"]
	_, reality := s["reality"]
	if (security == "tls" && (!tls || reality)) ||
		(security == "reality" && (!reality || tls || network == "grpc")) ||
		(security == "none" && (tls || reality)) {
		return false
	}
	if s["flow"] == "xtls-rprx-vision" && (network != "tcp" || security == "none") {
		return false
	}
	if network != "tcp" {
		if _, ok := s[network]; !ok {
			return false
		}
	}
	if network == "h2" && security != "tls" {
		return false
	}
	if s["source"] == "subscription" {
		if _, ok := s["subscriptionId"]; !ok {
			return false
		}
	}
	// Preserve numeric spellings: import supports only the compiler's typed decoding subset.
	data, err := json.Marshal(s)
	if err != nil {
		return false
	}
	server, err := compile.Decode[compile.Server](data)
	// Match compile.transportBlock: TCP cannot carry even inactive transport blocks.
	return err == nil && (server.Network != "tcp" ||
		(server.WS == nil && server.GRPC == nil && server.XHTTP == nil && server.H2 == nil))
}

var importProfileShape = importObject(map[string]importRule{
	"id": importUUID, "displayName": importString(1, 80, ""), "kind": importEnum("connector"),
	"serverIds": importArray(1, 512, importUUID, true), "defaultServerId": importUUID,
	"selection": importObject(map[string]importRule{
		"mode":          importEnum("fixed", "ordered", "fastest", "roundRobin"),
		"stickySeconds": importInteger(0, 86400),
	}, "mode"),
	"failover": importObject(map[string]importRule{
		"enabled": importBool, "cooldownSeconds": importInteger(5, 3600),
		"maxConsecutiveFailures": importInteger(1, 20), "probeIntervalSeconds": importInteger(10, 3600),
	}, "enabled"),
	"failurePolicy": importObject(map[string]importRule{
		"coreDeath": importObject(map[string]importRule{
			"action":       importEnum("block", "failOpen"),
			"confirmToken": importString(0, -1, `^confirm://[A-Za-z0-9._:/-]{1,64}$`),
		}, "action"),
	}, "coreDeath"),
	"routingPolicyId": importUUID, "enabled": importBool,
	"createdAt": importDateTime, "updatedAt": importDateTime,
}, "id", "displayName", "kind", "serverIds", "selection", "failover", "failurePolicy")

func validateImportProfile(v any) bool {
	if !importProfileShape(v) {
		return false
	}
	p := v.(map[string]any)
	if p["selection"].(map[string]any)["mode"] == "fixed" {
		if _, ok := p["defaultServerId"]; !ok {
			return false
		}
	}
	death := p["failurePolicy"].(map[string]any)["coreDeath"].(map[string]any)
	if death["action"] == "failOpen" {
		if _, ok := death["confirmToken"]; !ok {
			return false
		}
	}
	// Schema-valid integer values may still have encodings the compiler refuses.
	data, err := json.Marshal(p)
	if err != nil {
		return false
	}
	_, err = compile.Decode[compile.Profile](data)
	return err == nil
}

func validateImportHeaders(v any) bool {
	headers, ok := v.(map[string]any)
	if !ok || len(headers) == 0 || len(headers) > 32 {
		return false
	}
	for key, value := range headers {
		if !importString(1, 128, "")(key) || !importString(1, 4096, "")(value) {
			return false
		}
	}
	return true
}

// Token decoding rejects duplicate keys, case aliases and trailing JSON rather
// than silently picking one secret/owner. No decoder error or input key leaves
// this file. The depth bound exceeds every declared shape in this command.
func decodeImportJSON(data []byte) (any, bool) {
	if !utf8.Valid(data) {
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, ok := readImportJSON(d, 0)
	if !ok {
		return nil, false
	}
	_, err := d.Token()
	return v, err == io.EOF
}

func readImportJSON(d *json.Decoder, depth int) (any, bool) {
	if depth > 64 {
		return nil, false
	}
	t, err := d.Token()
	if err != nil {
		return nil, false
	}
	delim, container := t.(json.Delim)
	if !container {
		return t, true
	}
	switch delim {
	case '{':
		out := map[string]any{}
		for d.More() {
			key, err := d.Token()
			s, ok := key.(string)
			if err != nil || !ok {
				return nil, false
			}
			if _, exists := out[s]; exists {
				return nil, false
			}
			v, ok := readImportJSON(d, depth+1)
			if !ok {
				return nil, false
			}
			out[s] = v
		}
		end, err := d.Token()
		return out, err == nil && end == json.Delim('}')
	case '[':
		out := []any{}
		for d.More() {
			v, ok := readImportJSON(d, depth+1)
			if !ok {
				return nil, false
			}
			out = append(out, v)
		}
		end, err := d.Token()
		return out, err == nil && end == json.Delim(']')
	default:
		return nil, false
	}
}
