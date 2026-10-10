package telemetry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// pseudonymizedProperties are the collected properties that name people, machines,
// repositories, or workspaces. TestPseudonymizedPropertiesAreClassified requires every
// collected property to be classified, so that new properties are not missed.
// network.public_ip is left out: a public IPv4 address has so few possible values
// that a pseudonym does not hide it from anyone who knows the project token.
var pseudonymizedProperties = []string{
	"network.hostname",
	"git.name", "git.remote",
	"github.login", "github.id", "github.name", "github.html_url",
	"github_actions.repository", "github_actions.repository_id", "github_actions.repository_owner",
	"github_actions.actor", "github_actions.actor_id", "github_actions.triggering_actor",
	"hcp_terraform.workspace_id", "hcp_terraform.workspace_name", "hcp_terraform.workspace_slug",
}

// PseudonymizedProperties returns the paths of the collected properties that
// enabled pseudonymization replaces with pseudonyms.
func PseudonymizedProperties() []string {
	return append([]string(nil), pseudonymizedProperties...)
}

// Pseudonymize returns a copy of properties in which the value at each path in
// keys is replaced with an HMAC-SHA256 pseudonym keyed by secret. Strings are
// hashed as is, and other values as their JSON encoding. Null values and
// missing paths are left unchanged. Each path is replaced once, even when it
// is repeated in keys. The input maps are never modified.
func Pseudonymize(properties map[string]any, keys []string, secret string) map[string]any {
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		if updated, ok := pseudonymizePath(properties, strings.Split(key, "."), secret); ok {
			properties = updated
		}
	}
	return properties
}

// pseudonymizePath copies only the maps along the path, so that maps shared
// with the input stay unchanged. It reports false when nothing is replaced.
func pseudonymizePath(object map[string]any, path []string, secret string) (map[string]any, bool) {
	value, ok := object[path[0]]
	if !ok || value == nil {
		return nil, false
	}
	var replaced any
	if len(path) == 1 {
		replaced = pseudonymOf(value, secret)
	} else {
		child, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		if replaced, ok = pseudonymizePath(child, path[1:], secret); !ok {
			return nil, false
		}
	}
	result := make(map[string]any, len(object))
	for k, v := range object {
		result[k] = v
	}
	result[path[0]] = replaced
	return result, true
}

func pseudonymOf(value any, secret string) string {
	data, ok := value.(string)
	if !ok {
		// Event properties always encode. If encoding failed, the value would still be hidden.
		encoded, _ := json.Marshal(value)
		data = string(encoded)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}
