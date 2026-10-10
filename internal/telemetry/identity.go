package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// IdentityID derives a stable distinct ID from the values of the selected
// property paths. It returns an empty string when no paths are selected or a
// path is missing.
func IdentityID(properties map[string]any, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	selected, ok := selectPaths(properties, keys)
	if !ok {
		return ""
	}
	// JSON sorts map keys and keeps value types. Paths are part of the input,
	// so the same value under different paths yields a different ID.
	encoded, err := json.Marshal(selected)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("terraform-provider-telemetry/identity:"), encoded...))
	return hex.EncodeToString(sum[:16])
}
