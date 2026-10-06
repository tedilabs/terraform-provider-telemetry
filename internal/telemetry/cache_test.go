package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCollectionCacheConcurrentAndLazy(t *testing.T) {
	var gitCalls, githubCalls, envCalls atomic.Int32
	c := NewCollector()
	c.Run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "git" {
			gitCalls.Add(1)
			return []byte("/repo"), nil
		}
		githubCalls.Add(1)
		return []byte(`{"login":"first"}`), nil
	}
	c.Getenv = func(string) string { envCalls.Add(1); return "" }
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			got := c.Collect(context.Background(), Options{Machine: true, Git: true, GitHub: true}, true)
			if len(got) != 3 || got["github"].(map[string]any)["login"] != "first" {
				t.Errorf("unexpected metadata: %v", got)
			}
			got["github"].(map[string]any)["login"] = "caller mutation"
		})
	}
	wg.Wait()
	if gitCalls.Load() != 4 || githubCalls.Load() != 1 || envCalls.Load() != 0 {
		t.Fatalf("unexpected collection counts: git=%d github=%d env=%d", gitCalls.Load(), githubCalls.Load(), envCalls.Load())
	}
	// A group disabled on earlier calls remains eligible for lazy collection.
	for range 2 {
		got := c.Collect(context.Background(), Options{GitHubActions: true}, true)
		if len(got) != 0 {
			t.Fatalf("disabled groups leaked from cache: %v", got)
		}
	}
	if envCalls.Load() != 1 {
		t.Fatal("unavailable Actions metadata was not cached")
	}
}

func TestCollectionCacheBypassDoesNotReadOrWrite(t *testing.T) {
	var calls int
	c := NewCollector()
	c.Run = func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return []byte(fmt.Sprintf(`{"login":"user-%d"}`, calls)), nil
	}
	for _, step := range []struct {
		cache bool
		want  string
	}{
		{false, "user-1"}, // Does not populate the cache.
		{true, "user-2"},
		{false, "user-3"}, // Does not read or replace the cached snapshot.
		{true, "user-2"},
	} {
		got := c.Collect(context.Background(), Options{GitHub: true}, step.cache)
		if got["github"].(map[string]any)["login"] != step.want {
			t.Fatalf("cache=%v: got %v, want %s", step.cache, got, step.want)
		}
	}
}

func TestCollectionCacheUnavailableAndIndependentCollectors(t *testing.T) {
	var calls int
	newCollector := func() Collector {
		c := NewCollector()
		c.Run = func(context.Context, string, ...string) ([]byte, error) {
			calls++
			return nil, errors.New("unavailable")
		}
		return c
	}
	c := newCollector()
	for range 2 {
		got := c.Collect(context.Background(), Options{Machine: true, Git: true, GitHub: true}, true)
		if len(got) != 1 || got["machine"] == nil {
			t.Fatalf("failure affected other groups: %v", got)
		}
	}
	if calls != 2 {
		t.Fatalf("expected one attempt per unavailable group, got %d", calls)
	}
	newCollector().Collect(context.Background(), Options{Git: true, GitHub: true}, true)
	if calls != 4 {
		t.Fatal("independent collector unexpectedly reused cache")
	}
}

func TestCachedMetadataCopiesNestedValues(t *testing.T) {
	entry := cachedCollection{}
	read := func() map[string]any {
		return map[string]any{"ips": []string{"127.0.0.1"}, "nested": []any{map[string]any{"name": "original"}}}
	}
	first := entry.get(read)
	first["ips"].([]string)[0] = "changed"
	first["nested"].([]any)[0].(map[string]any)["name"] = "changed"
	second := entry.get(read)
	if second["ips"].([]string)[0] != "127.0.0.1" || second["nested"].([]any)[0].(map[string]any)["name"] != "original" {
		t.Fatal("caller mutated cached metadata")
	}
}
