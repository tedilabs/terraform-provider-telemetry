package telemetry

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestDisabledCollectorsDoNotReadEnvironmentOrRunCommands(t *testing.T) {
	c := Collector{
		Run: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("disabled collector ran a command")
			return nil, nil
		},
		Getenv: func(string) string { t.Fatal("disabled collector read environment"); return "" },
	}
	if got := c.Collect(context.Background(), Options{}, true); len(got) != 0 {
		t.Fatalf("unexpected metadata: %v", got)
	}
}

func TestCollectorsAreIndependentAndFilterSensitiveData(t *testing.T) {
	env := map[string]string{
		"GITHUB_ACTIONS": "true", "GITHUB_WORKFLOW": "Deploy", "GITHUB_JOB": "terraform",
		"GITHUB_RUN_ID": "42", "GITHUB_SERVER_URL": "https://github.com",
		"GITHUB_REPOSITORY": "example/infra", "GITHUB_TOKEN": "must-not-collect",
	}
	commands := map[string]string{
		"git rev-parse --show-toplevel":                "/work/infra\n",
		"git rev-parse --abbrev-ref HEAD":              "main\n",
		"git rev-parse HEAD":                           "abc123\n",
		"git config --get remote.origin.url":           "https://user:secret@github.com/example/infra.git?token=secret#secret\n",
		"gh api user --jq {login, id, name, html_url}": `{"login":"tester","id":123,"name":"Test User","html_url":"https://github.com/tester","email":"private@example.com","token":"secret"}`,
	}
	c := Collector{
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			key := name + " " + strings.Join(args, " ")
			value, ok := commands[key]
			if !ok {
				t.Fatalf("unexpected command: %s", key)
			}
			return []byte(value), nil
		},
		Getenv: func(key string) string { return env[key] },
	}
	got := c.Collect(context.Background(), Options{Machine: true, Git: true, GitHub: true, GitHubActions: true}, true)
	if len(got) != 4 {
		t.Fatalf("expected four enabled collectors, got %v", got)
	}
	if got["machine"].(map[string]any)["arch"] != runtime.GOARCH {
		t.Fatal("wrong architecture")
	}
	git := got["git"].(map[string]any)
	if git["remote"] != "https://github.com/example/infra.git" || git["name"] != "infra" {
		t.Fatalf("wrong git metadata: %v", git)
	}
	user := got["github"].(map[string]any)
	if len(user) != 4 || user["login"] != "tester" {
		t.Fatalf("unfiltered user: %v", user)
	}
	actions := got["github_actions"].(map[string]any)
	if actions["job"] != "terraform" || actions["run_url"] != "https://github.com/example/infra/actions/runs/42" {
		t.Fatalf("wrong action metadata: %v", actions)
	}
	if _, ok := actions["GITHUB_TOKEN"]; ok {
		t.Fatal("collected token")
	}
}

func TestUnavailableCollectorsAreOmitted(t *testing.T) {
	c := Collector{
		Run:    func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("unavailable") },
		Getenv: func(string) string { return "" },
	}
	got := c.Collect(context.Background(), Options{Machine: true, Git: true, GitHub: true, GitHubActions: true}, true)
	if len(got) != 1 || got["machine"] == nil {
		t.Fatalf("failed collector affected remaining metadata: %v", got)
	}
}

func TestNetworkMetadata(t *testing.T) {
	got := NewCollector().Collect(context.Background(), Options{Network: true}, true)
	if len(got) != 1 || got["network"] == nil {
		t.Fatal("network collector not enabled")
	}
	for key := range got["network"].(map[string]any) {
		if key != "hostname" && key != "ips" {
			t.Fatalf("unexpected network field: %s", key)
		}
	}
}

func TestSanitizeRemote(t *testing.T) {
	for input, want := range map[string]string{
		"https://user:token@github.com/a/b.git?secret=x#fragment": "https://github.com/a/b.git",
		"ssh://git@example.com/a/b.git":                           "ssh://example.com/a/b.git",
		"git@github.com:a/b.git":                                  "github.com:a/b.git",
		"/home/user/private/repo":                                 "",
		"file:///home/user/private/repo":                          "",
		"https://bad%host/repo":                                   "",
	} {
		if got := sanitizeRemote(input); !reflect.DeepEqual(got, want) {
			t.Errorf("sanitizeRemote(%q) = %q, want %q", input, got, want)
		}
	}
}
