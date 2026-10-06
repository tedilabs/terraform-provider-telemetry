package telemetry

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeduplicationConcurrentSelectedKeys(t *testing.T) {
	var d Deduplicator
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			properties := map[string]any{"extra_data": map[string]any{"module": "vpc", "version": "1", "instance": i}}
			if d.Allow([]string{"posthog", "host", "token"}, properties, []string{"extra_data.module", "extra_data.version"}) {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 1 {
		t.Fatalf("expected one attempt for 100 instances, got %d", allowed.Load())
	}
}

func TestDeduplicationScopeAndKeyIdentity(t *testing.T) {
	var d Deduplicator
	scope := []string{"posthog", "host", "token"}
	props := map[string]any{"extra_data": map[string]any{"module": "vpc", "version": "1", "workspace": "prod"}}
	keys := []string{"extra_data.module", "extra_data.version"}
	if !d.Allow(scope, props, keys) || d.Allow(scope, props, []string{keys[1], keys[0], keys[0]}) {
		t.Fatal("key order or duplicate keys changed event identity")
	}
	for _, other := range [][]string{{"other", "host", "token"}, {"posthog", "other", "token"}, {"posthog", "host", "other"}} {
		if !d.Allow(other, props, keys) {
			t.Fatalf("different destination was suppressed: %v", other)
		}
	}
	if !d.Allow(scope, props, []string{"extra_data.workspace"}) || !d.Allow(scope, props, nil) {
		t.Fatal("different comparison keys were suppressed")
	}
	props["extra_data"].(map[string]any)["version"] = "2"
	if !d.Allow(scope, props, keys) {
		t.Fatal("different version was suppressed")
	}
	var fresh Deduplicator
	if !fresh.Allow(scope, props, keys) {
		t.Fatal("independent deduplicator unexpectedly reused state")
	}
}

func TestDeduplicationFullPropertiesAndTypes(t *testing.T) {
	var d Deduplicator
	for _, value := range []any{nil, "1", json.Number("1"), true, []any{"a", "b"}, map[string]any{"a": "b"}} {
		props := map[string]any{"extra_data": map[string]any{"value": value}}
		if !d.Allow(nil, props, nil) || d.Allow(nil, props, []string{}) {
			t.Fatalf("incorrect full-property comparison for %v", value)
		}
	}
	first := map[string]any{"a": 1, "b": map[string]any{"x": "y", "z": nil}}
	second := map[string]any{"b": map[string]any{"z": nil, "x": "y"}, "a": 1}
	if !d.Allow(nil, first, nil) || d.Allow(nil, second, nil) {
		t.Fatal("map order changed event identity")
	}
}

func TestDeduplicationMissingPathsBypassWithoutReserving(t *testing.T) {
	var d Deduplicator
	props := map[string]any{"extra_data": map[string]any{"module": "vpc"}}
	for range 2 {
		if !d.Allow(nil, props, []string{"extra_data.version"}) || !d.Allow(nil, props, []string{"extra_data.module.name"}) {
			t.Fatal("missing paths suppressed an event")
		}
	}
	props["extra_data"].(map[string]any)["version"] = nil
	if !d.Allow(nil, props, []string{"extra_data.version"}) || d.Allow(nil, props, []string{"extra_data.version"}) {
		t.Fatal("explicit null should be a usable value, distinct from missing")
	}
}
