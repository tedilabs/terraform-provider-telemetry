package telemetry

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestIdentityID(t *testing.T) {
	props := map[string]any{
		"git":        map[string]any{"remote": "https://github.com/acme/infra"},
		"module":     "https://github.com/acme/infra",
		"extra_data": map[string]any{"count": json.Number("1"), "text": "1", "nullable": nil},
	}
	id := IdentityID(props, []string{"git.remote"})
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
		t.Fatalf("malformed identity: %q", id)
	}
	if IdentityID(props, []string{"git.remote"}) != id {
		t.Fatal("unstable identity")
	}
	if IdentityID(props, []string{"module"}) == id {
		t.Fatal("the same value under a different path has the same identity")
	}
	pair := IdentityID(props, []string{"git.remote", "module"})
	if IdentityID(props, []string{"module", "git.remote", "module"}) != pair || pair == id {
		t.Fatal("key order or duplicate keys changed the identity")
	}
	if IdentityID(props, []string{"extra_data.count"}) == IdentityID(props, []string{"extra_data.text"}) {
		t.Fatal("values of different types share an identity")
	}
	other := map[string]any{"git": map[string]any{"remote": "https://github.com/acme/other"}}
	if IdentityID(other, []string{"git.remote"}) == id {
		t.Fatal("different values share an identity")
	}
	if IdentityID(props, []string{"extra_data.nullable"}) == "" {
		t.Fatal("an explicit null should be a usable value")
	}
	for _, keys := range [][]string{nil, {}, {"git.branch"}, {"git.remote", "git.branch"}, {"module.name"}} {
		if got := IdentityID(props, keys); got != "" {
			t.Errorf("%v: expected no identity, got %q", keys, got)
		}
	}
}
