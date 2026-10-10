package provider_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
)

func TestAccCapturePostHog(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run Terraform acceptance tests")
	}
	installAcceptanceProvider(t)

	t.Run("metadata_and_http_failure", func(t *testing.T) {
		capture := &captureCheck{want: 1}
		server := httptest.NewServer(capture)
		defer server.Close()
		work := t.TempDir()
		capture.gitName = filepath.Base(work)
		for _, args := range [][]string{
			{"-c", "init.defaultBranch=main", "init", "--quiet"},
			{"-c", "user.name=Acceptance Test", "-c", "user.email=acceptance@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "--quiet", "--allow-empty", "-m", "Acceptance test"},
			{"remote", "add", "origin", "https://test:fake-token@example.invalid/demo/infra.git?token=fake"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = work
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git: %s: %v", out, err)
			}
		}
		var steps []resource.TestStep
		for _, scenario := range []struct {
			status int
			cache  bool
		}{
			{http.StatusNoContent, true},
			{http.StatusInternalServerError, true}, // Same configuration: no-change apply still attempts delivery.
			{http.StatusInternalServerError, false},
		} {
			step := acceptanceStep(capture, "capture", config.Variables{
				"host":          config.StringVariable(server.URL),
				"cache_enabled": config.BoolVariable(scenario.cache),
			})
			step.PreConfig = func() { capture.reset(scenario.status) }
			steps = append(steps, step)
		}
		resource.Test(t, resource.TestCase{WorkingDir: work, Steps: steps})
	})

	t.Run("module_deduplication", func(t *testing.T) {
		for _, scenario := range []struct {
			name    string
			enabled bool
			keys    []config.Variable
			want    int
		}{
			{"module_keys", true, []config.Variable{config.StringVariable("module"), config.StringVariable("workspace")}, 2},
			{"disabled", false, []config.Variable{config.StringVariable("module"), config.StringVariable("workspace")}, 200},
			{"instance_keys", true, []config.Variable{config.StringVariable("module"), config.StringVariable("instance")}, 200},
		} {
			t.Run(scenario.name, func(t *testing.T) {
				capture := &captureCheck{want: scenario.want}
				server := httptest.NewServer(capture)
				defer server.Close()
				step := acceptanceStep(capture, "deduplication", config.Variables{
					"host":                  config.StringVariable(server.URL),
					"deduplication_enabled": config.BoolVariable(scenario.enabled),
					"deduplication_keys":    config.ListVariable(scenario.keys...),
				})
				resource.Test(t, resource.TestCase{Steps: []resource.TestStep{step}})
			})
		}
	})
}

// Use real provider processes: in-process factories would share the process-wide
// metadata cache and deduplicator across plan and saved-plan apply.
func installAcceptanceProvider(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	mirror := filepath.Join(root, "mirror")
	binDir := filepath.Join(mirror, "registry.terraform.io", "tedilabs", "telemetry", "0.1.0", runtime.GOOS+"_"+runtime.GOARCH)
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "terraform-provider-telemetry_v0.1.0")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binary, "../..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build provider: %s: %v", out, err)
	}
	cliConfig := filepath.Join(root, "terraform.rc")
	// No direct fallback: the tests cannot fetch a published provider by mistake.
	content := fmt.Sprintf("provider_installation {\n  filesystem_mirror {\n    path = %q\n  }\n}\n", filepath.ToSlash(mirror))
	if err := os.WriteFile(cliConfig, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TF_CLI_CONFIG_FILE", cliConfig)
	t.Setenv("CHECKPOINT_DISABLE", "1")
	if os.Getenv("TF_ACC_TERRAFORM_PATH") == "" {
		path, err := exec.LookPath("terraform")
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("TF_ACC_TERRAFORM_PATH", path)
	}
}

func acceptanceStep(capture *captureCheck, fixture string, variables config.Variables) resource.TestStep {
	return resource.TestStep{
		ConfigDirectory: config.StaticDirectory(filepath.Join("testdata", "acceptance", fixture)),
		ConfigVariables: variables,
		ConfigPlanChecks: resource.ConfigPlanChecks{
			PreApply:             []plancheck.PlanCheck{capture},
			PostApplyPreRefresh:  []plancheck.PlanCheck{capture},
			PostApplyPostRefresh: []plancheck.PlanCheck{capture},
		},
		ConfigStateChecks: []statecheck.StateCheck{capture},
	}
}

type captureCheck struct {
	mu      sync.Mutex
	events  []map[string]any
	err     error
	status  int
	want    int
	gitName string
}

func (c *captureCheck) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var event struct {
		Properties map[string]any `json:"properties"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&event); err != nil {
		c.err = err
	}
	if r.Method != http.MethodPost || r.URL.Path != "/i/v0/e/" {
		c.err = fmt.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}
	c.events = append(c.events, event.Properties)
	status := c.status
	if status == 0 {
		status = http.StatusNoContent
	}
	w.WriteHeader(status)
}

func (c *captureCheck) reset(status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events, c.err, c.status = nil, nil, status
}

func (c *captureCheck) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	resp.Error = c.check(req.Plan.Checks)
}

func (c *captureCheck) CheckState(_ context.Context, req statecheck.CheckStateRequest, resp *statecheck.CheckStateResponse) {
	if req.State.Values != nil {
		var checkResources func(*tfjson.StateModule) bool
		checkResources = func(m *tfjson.StateModule) bool {
			if m == nil {
				return false
			}
			if len(m.Resources) != 0 {
				return true
			}
			for _, child := range m.ChildModules {
				if checkResources(child) {
					return true
				}
			}
			return false
		}
		if checkResources(req.State.Values.RootModule) {
			resp.Error = fmt.Errorf("function-only provider created resource state")
			return
		}
	}
	resp.Error = c.check(req.State.Checks)
}

func (c *captureCheck) check(checks []tfjson.CheckResultStatic) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { c.events = nil }()
	if c.err != nil {
		return c.err
	}
	if len(c.events) != c.want {
		return fmt.Errorf("expected %d captures per Terraform command, got %d", c.want, len(c.events))
	}
	if len(checks) == 0 {
		return fmt.Errorf("missing check block results")
	}
	for _, check := range checks {
		if check.Status != tfjson.CheckStatusPass {
			return fmt.Errorf("check did not pass: %+v", check)
		}
	}
	groups := map[string]int{}
	for _, props := range c.events {
		for _, key := range []string{"network", "github", "github_actions", "extra_data"} {
			if _, ok := props[key]; ok {
				return fmt.Errorf("unexpected property %q", key)
			}
		}
		if c.gitName == "" {
			groups[fmt.Sprint(props["module"])]++
			continue
		}
		property := func(path string) any {
			var value any = props
			for _, key := range strings.Split(path, ".") {
				object, _ := value.(map[string]any)
				value = object[key]
			}
			return value
		}
		for path, want := range map[string]any{
			"count": json.Number("9007199254740993"), "nested.enabled": true,
			"items": []any{"a", json.Number("2")}, "workspace": "default",
			"machine.os.version": "acceptance-override", "terraform.workspace": "default",
			"terraform.in_automation": nil, "terraform.cli": "terraform", "toolchain.telemetry_provider": "dev",
			"git.name": c.gitName, "git.remote": "https://example.invalid/demo/infra.git", "git.branch": pseudonym("acceptance-only", "main"),
		} {
			if got := property(path); !reflect.DeepEqual(got, want) {
				return fmt.Errorf("%s: got %#v, want %#v", path, got, want)
			}
		}
		for _, path := range []string{"machine.os.name", "git.commit", "terraform.command_id", "terraform.cli_version", "toolchain.terraform", "toolchain.git"} {
			if value, ok := property(path).(string); !ok || value == "" {
				return fmt.Errorf("missing %s", path)
			}
		}
		memory, ok := property("machine.memory_size").(json.Number)
		if !ok {
			return fmt.Errorf("missing machine.memory_size")
		}
		if value, err := memory.Int64(); err != nil || value <= 0 {
			return fmt.Errorf("invalid memory size: %s", memory)
		}
	}
	if c.gitName == "" && (groups["counted"] != c.want/2 || groups["each"] != c.want/2) {
		return fmt.Errorf("unexpected module captures: %v", groups)
	}
	return nil
}

func pseudonym(token, value string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}
