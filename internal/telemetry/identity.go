package telemetry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// IdentityID derives a stable distinct ID from the values of the selected
// property paths, keyed by secret. It returns an empty string when no paths are
// selected or a path is missing.
func IdentityID(properties map[string]any, keys []string, secret string) string {
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
	// Key the hash like a pseudonym. The original values of pseudonymized properties
	// are then no easier to recover from the ID than from their pseudonyms, and the
	// ID differs between PostHog projects.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("terraform-provider-telemetry/identity:"))
	mac.Write(encoded)
	return hex.EncodeToString(mac.Sum(nil)[:16])
}
