package telemetry

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
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

// Every property that a collector can send must be either pseudonymized or
// explicitly left as is, and every pseudonymized path must exist.
func TestPseudonymizedPropertiesAreClassified(t *testing.T) {
	notPseudonymized := []string{
		"machine.os.name", "machine.os.version", "machine.arch", "machine.cpu_count", "machine.memory_size",
		"network.public_ip",
		"git.branch", "git.commit",
		"github.account_type",
		"github_actions.workflow", "github_actions.job", "github_actions.run_number", "github_actions.run_attempt",
		"github_actions.event_name", "github_actions.ref", "github_actions.sha", "github_actions.head_ref", "github_actions.base_ref",
		"github_actions.server_url", "github_actions.runner.os", "github_actions.runner.arch", "github_actions.runner.environment",
		"hcp_terraform.run_id", "hcp_terraform.project_id", "hcp_terraform.project_name",
		"hcp_terraform.git_branch", "hcp_terraform.git_commit", "hcp_terraform.git_tag",
		"terraform.command_id", "terraform.cli", "terraform.cli_version", "terraform.workspace", "terraform.workspace_source",
		"toolchain.terraform", "toolchain.opentofu", "toolchain.git", "toolchain.github_cli",
	}
	// Every collector reports every property it can.
	c := NewCollector()
	c.Getenv = func(key string) string {
		if key == "GITHUB_ACTIONS" {
			return "true"
		}
		if strings.HasPrefix(key, "GITHUB_") || strings.HasPrefix(key, "RUNNER_") || strings.HasPrefix(key, "TFC_") || key == "TF_WORKSPACE" {
			return "value"
		}
		return ""
	}
	c.LookupPublicIP = func(context.Context) string { return "203.0.113.7" }
	c.Parent = func() (Process, bool) {
		return Process{ID: 4242, Start: "start", Executable: "/usr/bin/terraform"}, true
	}
	c.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch command := strings.Join(append([]string{name}, args...), " "); {
		case strings.HasSuffix(command, "version -json"):
			return []byte(`{"terraform_version":"1.16.5"}`), nil
		case command == "git --version":
			return []byte("git version 2.50.0"), nil
		case command == "gh --version":
			return []byte("gh version 2.80.0"), nil
		case name == "gh":
			return []byte(`{"login":"user","id":1,"name":"User","html_url":"https://github.com/user","account_type":"User"}`), nil
		case command == "git config --get remote.origin.url":
			return []byte("https://github.com/example/repository.git"), nil
		default:
			return []byte("value"), nil
		}
	}
	properties := c.Collect(context.Background(), Options{
		Machine: true, Network: true, Git: true, GitHub: true, GitHubActions: true, HCPTerraform: true, Terraform: true, Toolchain: true,
	}, false)
	collected := map[string]bool{}
	var walk func(prefix string, object map[string]any)
	walk = func(prefix string, object map[string]any) {
		for key, value := range object {
			if child, ok := value.(map[string]any); ok {
				walk(prefix+key+".", child)
			} else {
				collected[prefix+key] = true
			}
		}
	}
	walk("", properties)
	classified := map[string]bool{}
	for _, path := range append(PseudonymizedProperties(), notPseudonymized...) {
		if classified[path] {
			t.Errorf("%s is classified twice", path)
		}
		classified[path] = true
	}
	var unclassified []string
	for path := range collected {
		if !classified[path] {
			unclassified = append(unclassified, path)
		}
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Errorf("classify these properties in pseudonymizedProperties or notPseudonymized: %v", unclassified)
	}
	for _, path := range PseudonymizedProperties() {
		if !collected[path] {
			t.Errorf("pseudonymized property %s is never collected", path)
		}
	}
}
