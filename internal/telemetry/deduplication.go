package telemetry

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"sync"
)

// Deduplicator remembers attempts for one provider process. Its zero value is
// ready to use; it retains only hashes, not credentials or event properties.
type Deduplicator struct {
	mu   sync.Mutex
	seen map[[sha256.Size]byte]struct{}
}

// Allow atomically reserves an event before delivery. Scope distinguishes the
// destination, connection, and event type. Empty keys compare all properties.
// Missing paths bypass deduplication rather than suppressing unrelated events.
func (d *Deduplicator) Allow(scope []string, properties map[string]any, keys []string) bool {
	selected := properties
	if len(keys) > 0 {
		var ok bool
		if selected, ok = selectPaths(properties, keys); !ok {
			return true
		}
	}
	// JSON sorts map keys, preserving value types and avoiding delimiter collisions.
	// Keep full-property comparisons separate from explicitly selected paths.
	encoded, err := json.Marshal(struct {
		Scope    []string
		Filtered bool
		Values   map[string]any
	}{scope, len(keys) > 0, selected})
	if err != nil {
		return true
	}
	key := sha256.Sum256(encoded)
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.seen[key]; exists {
		return false
	}
	if d.seen == nil {
		d.seen = make(map[[sha256.Size]byte]struct{})
	}
	d.seen[key] = struct{}{}
	return true
}

// selectPaths returns the values of dot-separated property paths, keyed by path.
// It returns false if a path is missing.
func selectPaths(properties map[string]any, paths []string) (map[string]any, bool) {
	selected := make(map[string]any, len(paths))
	for _, path := range paths {
		var value any = properties
		for _, part := range strings.Split(path, ".") {
			object, ok := value.(map[string]any)
			if !ok {
				return nil, false
			}
			if value, ok = object[part]; !ok {
				return nil, false
			}
		}
		selected[path] = value
	}
	return selected, true
}
