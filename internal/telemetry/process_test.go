package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestParentProcess(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("parent process start time is not supported on " + runtime.GOOS)
	}
	first, ok := parentProcess()
	if !ok || first.ID != os.Getppid() || first.Start == "" || !filepath.IsAbs(first.Executable) {
		t.Fatalf("parent process not found: %+v, %v", first, ok)
	}
	if second, ok := parentProcess(); !ok || second != first {
		t.Fatalf("parent process changed: %+v, %+v", first, second)
	}
}

func TestCommandID(t *testing.T) {
	parent := Process{ID: 4242, Start: "1700000000.000001"}
	id := commandID(parent)
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) || id != commandID(parent) {
		t.Fatalf("unstable or malformed command ID: %s", id)
	}
	if strings.Contains(id, strconv.Itoa(parent.ID)) {
		t.Fatal("command ID exposes the process ID")
	}
	for _, other := range []Process{{ID: 4243, Start: parent.Start}, {ID: parent.ID, Start: "1700000000.000002"}} {
		if commandID(other) == id {
			t.Fatalf("different processes share a command ID: %+v", other)
		}
	}
}

func TestTerraformCommandID(t *testing.T) {
	c := NewCollector()
	c.Getenv = func(key string) string { return map[string]string{"TF_WORKSPACE": "prod"}[key] }
	c.Parent = func() (Process, bool) { return Process{ID: 4242, Start: "start"}, true }
	if got := c.terraform(context.Background()); got["command_id"] != commandID(Process{ID: 4242, Start: "start"}) || got["workspace"] != "prod" {
		t.Fatalf("missing command ID: %v", got)
	}
	c.Parent = func() (Process, bool) { return Process{}, false }
	if _, ok := c.terraform(context.Background())["command_id"]; ok {
		t.Fatal("command ID without a parent process")
	}
}

func TestCLIName(t *testing.T) {
	for _, test := range []struct{ executable, cli string }{
		{"/usr/local/bin/terraform", "terraform"},
		{"/home/user/.local/share/mise/installs/opentofu/1.10.0/tofu", "opentofu"},
		{`C:\Program Files\Terraform\TERRAFORM.EXE`, "terraform"},
		{`C:\tools\tofu.exe`, "opentofu"},
		{"/usr/local/bin/terraform-ls", ""},
		{"/usr/local/bin/terragrunt", ""},
		{"/usr/bin/go", ""},
		{"", ""},
	} {
		if cli, ok := cliName(test.executable); cli != test.cli || ok != (test.cli != "") {
			t.Errorf("%q: got (%q, %v), want %q", test.executable, cli, ok, test.cli)
		}
	}
}

func TestTerraformCLI(t *testing.T) {
	for _, test := range []struct{ executable, cli, version string }{
		{"/opt/terraform/terraform", "terraform", "1.16.5"},
		{"/opt/opentofu/tofu", "opentofu", "1.10.0"},
		{"/usr/bin/python3", "", ""},
	} {
		c := NewCollector()
		c.Getenv = func(string) string { return "" }
		c.Parent = func() (Process, bool) { return Process{ID: 4242, Executable: test.executable}, true }
		var commands []string
		c.Run = func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return []byte(`{"terraform_version":"` + test.version + `","platform":"ignored"}`), nil
		}
		got := c.terraform(context.Background())
		if test.cli == "" {
			if len(commands) != 0 || got["cli"] != nil || got["cli_version"] != nil {
				t.Errorf("%s: ran or reported an unknown executable: %v, %v", test.executable, commands, got)
			}
			continue
		}
		if len(commands) != 1 || commands[0] != test.executable+" version -json" {
			t.Errorf("%s: unexpected commands %v", test.executable, commands)
		}
		if got["cli"] != test.cli || got["cli_version"] != test.version {
			t.Errorf("%s: got %v", test.executable, got)
		}
		if _, ok := got["command_id"]; ok {
			t.Errorf("%s: command ID without a start time", test.executable)
		}
	}
}
