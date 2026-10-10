package telemetry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
)

func TestPseudonymize(t *testing.T) {
	network := map[string]any{"hostname": "build-01", "public_ip": "203.0.113.7"}
	properties := map[string]any{
		"network": network,
		"github":  map[string]any{"id": json.Number("42"), "login": "octocat"},
		"tags":    []any{"a", "b"},
		"empty":   nil,
		"module":  "vpc",
	}
	before := cloneMetadata(properties)
	keys := []string{"network.hostname", "github.id", "tags", "empty", "missing", "module.name", "network.hostname"}
	got := Pseudonymize(properties, keys, "token")

	if !reflect.DeepEqual(properties, before) {
		t.Fatalf("input was modified: %v", properties)
	}
	if want := mustPseudonym(t, "build-01", "token"); got["network"].(map[string]any)["hostname"] != want {
		t.Fatalf("a repeated path is not hashed once: got %v, want %s", got["network"], want)
	}
	if id := got["github"].(map[string]any)["id"]; id != mustPseudonym(t, "42", "token") {
		t.Errorf("a number is not hashed as its JSON encoding: %v", id)
	}
	if tags := got["tags"]; tags != mustPseudonym(t, `["a","b"]`, "token") {
		t.Errorf("a list is not hashed as its JSON encoding: %v", tags)
	}
	format := regexp.MustCompile(`^[0-9a-f]{32}$`)
	for _, value := range []any{got["network"].(map[string]any)["hostname"], got["github"].(map[string]any)["id"], got["tags"]} {
		if s, ok := value.(string); !ok || !format.MatchString(s) {
			t.Errorf("malformed pseudonym: %v", value)
		}
	}
	// Unselected values, null values, missing paths, and paths through non-objects are unchanged.
	if got["network"].(map[string]any)["public_ip"] != "203.0.113.7" || got["github"].(map[string]any)["login"] != "octocat" {
		t.Errorf("unselected values changed: %v", got)
	}
	if v, ok := got["empty"]; !ok || v != nil || got["module"] != "vpc" {
		t.Errorf("null value or non-object path changed: %v", got)
	}
	if _, ok := got["missing"]; ok {
		t.Error("a missing path was added")
	}
	// Unchanged maps are not copied.
	if unchanged := Pseudonymize(properties, []string{"missing.path"}, "token"); reflect.ValueOf(unchanged).Pointer() != reflect.ValueOf(properties).Pointer() {
		t.Error("a missing path copied the properties")
	}
}

func TestPseudonymizeIsDeterministicPerSecret(t *testing.T) {
	properties := map[string]any{"git": map[string]any{"remote": "https://github.com/example/repository.git"}}
	remote := func(secret string) any {
		return Pseudonymize(properties, []string{"git.remote"}, secret)["git"].(map[string]any)["remote"]
	}
	if remote("first") != remote("first") {
		t.Fatal("the same value and secret produced different pseudonyms")
	}
	if remote("first") == remote("second") {
		t.Fatal("different secrets produced the same pseudonym")
	}
	whole := Pseudonymize(properties, []string{"git"}, "first")["git"]
	if whole != mustPseudonym(t, `{"remote":"https://github.com/example/repository.git"}`, "first") {
		t.Fatalf("an object is not hashed as its JSON encoding: %v", whole)
	}
}

func mustPseudonym(t *testing.T, data, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}
