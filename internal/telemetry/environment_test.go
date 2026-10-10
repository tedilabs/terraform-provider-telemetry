package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMachineMetadata(t *testing.T) {
	got := machine()
	osInfo := got["os"].(map[string]any)
	if osInfo["name"] == "" || got["arch"] != runtime.GOARCH || got["cpu_count"] != runtime.NumCPU() {
		t.Fatalf("invalid machine metadata: %v", got)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		if memory, ok := got["memory_size"].(uint64); !ok || memory == 0 {
			t.Fatalf("expected total memory in MiB: %v", got)
		}
	}
}

func TestParseOSRelease(t *testing.T) {
	for _, test := range []struct{ data, name, version string }{
		{"NAME=Ubuntu\nVERSION_ID=24.04\nPRETTY_NAME=ignored", "Ubuntu", "24.04"},
		{"NAME=\"Debian GNU/Linux\"\nVERSION_ID=\"12\"\n", "Debian GNU/Linux", "12"},
		{"# comment\nNAME='Alpine Linux'\nVERSION_ID='3.21'\n", "Alpine Linux", "3.21"},
		{"NAME=\"unfinished\nVERSION_ID=\"1\"", "", "1"},
		{"NAME=\"$(do-not-execute)\"\nSECRET=ignored", "$(do-not-execute)", ""},
	} {
		name, version := parseOSRelease([]byte(test.data))
		if name != test.name || version != test.version {
			t.Fatalf("got (%q, %q), want (%q, %q)", name, version, test.name, test.version)
		}
	}
}

func TestTerraformWorkspaceSources(t *testing.T) {
	for _, test := range []struct {
		env               map[string]string
		file              string
		err               error
		workspace, source string
	}{
		{env: map[string]string{"TF_WORKSPACE": "prod", "TF_IN_AUTOMATION": "false"}, workspace: "prod", source: "environment"},
		{env: map[string]string{"TF_DATA_DIR": "custom"}, file: "staging\n", workspace: "staging", source: "data_directory"},
		{file: "dev", workspace: "dev", source: "data_directory"},
		{err: os.ErrNotExist, workspace: "default", source: "default"},
		{err: os.ErrPermission},
		{file: ""},
	} {
		c := NewCollector()
		c.Getenv = func(key string) string { return test.env[key] }
		c.ReadFile = func(path string) ([]byte, error) {
			if test.env["TF_WORKSPACE"] != "" {
				t.Fatal("workspace override should avoid filesystem reads")
			}
			dir := test.env["TF_DATA_DIR"]
			if dir == "" {
				dir = ".terraform"
			}
			if path != filepath.Join(dir, "environment") {
				t.Fatalf("unexpected read: %s", path)
			}
			return []byte(test.file), test.err
		}
		got := c.terraform(context.Background())
		if test.workspace == "" {
			if _, ok := got["workspace"]; ok {
				t.Fatalf("inferred workspace after failed read: %v", got)
			}
		} else if got["workspace"] != test.workspace || got["workspace_source"] != test.source {
			t.Fatalf("wrong workspace: %v", got)
		}
		if _, ok := got["in_automation"]; ok {
			t.Fatal("unexpected automation flag")
		}
	}
}

func TestToolchainVersionsAndCache(t *testing.T) {
	c := NewCollector()
	var calls atomic.Int32
	c.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls.Add(1)
		switch name + " " + strings.Join(args, " ") {
		case "terraform version -json":
			return []byte(`{"terraform_version":"1.15.6","provider_selections":{"secret":"ignored"}}`), nil
		case "tofu version -json":
			return []byte(`{"terraform_version":"1.11.0"}`), nil
		case "git --version":
			return []byte("git version 2.50.1 (Apple Git-155)\n"), nil
		case "gh --version":
			return []byte("gh version 2.80.0 (2025-09-23)\nhttps://github.com/cli/cli/releases/tag/v2.80.0\n"), nil
		default:
			t.Errorf("unexpected command: %s %v", name, args)
			return nil, errors.New("unexpected")
		}
	}
	want := map[string]any{"terraform": "1.15.6", "opentofu": "1.11.0", "git": "2.50.1", "github_cli": "2.80.0"}
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			got := c.Collect(context.Background(), Options{Toolchain: true}, true)
			if !reflect.DeepEqual(got["toolchain"], want) {
				t.Errorf("unexpected versions: %v", got)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 4 {
		t.Fatalf("expected one probe per tool, got %d", calls.Load())
	}
	c.Collect(context.Background(), Options{Toolchain: true}, false)
	if calls.Load() != 8 {
		t.Fatal("cache bypass did not reprobe tools")
	}
}

func TestToolchainUnavailableAndMalformed(t *testing.T) {
	c := NewCollector()
	c.Run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "gh" {
			return []byte("gh version 2.80.0"), nil
		}
		if name == "terraform" {
			return nil, errors.New("missing")
		}
		return []byte("unrecognized output"), nil
	}
	if got := c.toolchain(context.Background()); !reflect.DeepEqual(got, map[string]any{"github_cli": "2.80.0"}) {
		t.Fatalf("bad tool suppressed valid version: %v", got)
	}
}

func TestTerraformCache(t *testing.T) {
	c := NewCollector()
	var reads atomic.Int32
	c.Getenv = func(string) string { return "" }
	c.ReadFile = func(string) ([]byte, error) { reads.Add(1); return []byte("prod"), nil }
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { c.Collect(context.Background(), Options{Terraform: true}, true) })
	}
	wg.Wait()
	if reads.Load() != 1 {
		t.Fatal("workspace repeatedly read")
	}
	c.Collect(context.Background(), Options{Terraform: true}, false)
	if reads.Load() != 2 {
		t.Fatal("workspace cache bypass failed")
	}
}
