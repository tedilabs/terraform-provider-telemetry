package telemetry

import "sync"

// Only these predefined groups may be cached. Each is collected lazily once,
// including an unavailable result, to avoid repeated failing commands.
type collectionCache struct {
	machine, network, git, github, githubActions, terraform, toolchain cachedCollection
}

type cachedCollection struct {
	once sync.Once
	data map[string]any
}

func (c *cachedCollection) get(read func() map[string]any) map[string]any {
	c.once.Do(func() { c.data = read() })
	if c.data == nil {
		return nil
	}
	// Callers may add or modify event fields without changing the shared snapshot.
	return cloneMetadata(c.data).(map[string]any)
}

func cloneMetadata(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = cloneMetadata(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = cloneMetadata(item)
		}
		return result
	case []string:
		return append([]string{}, v...)
	default:
		return value
	}
}
