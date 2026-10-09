package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/config"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccCapturePostHogInputs(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run Terraform acceptance tests")
	}
	installAcceptanceProvider(t)

	for _, scenario := range []struct {
		name    string
		results []string
		unknown bool
		want    map[string]map[string]any
	}{
		{
			name: "null_inputs",
			results: []string{
				"null_connection", "null_host", "null_project_token", "null_options",
				"null_collector_option", "null_setting_option", "null_key", "misspelled_option", "null_extra_data", "known", "nested_nulls",
			},
			want: map[string]map[string]any{
				"":      {}, // Null extra_data is an empty object, so capture still runs.
				"known": {"test_case": "known", "value": "known"},
				"nested_nulls": {
					"test_case": "nested_nulls",
					"object":    map[string]any{"value": nil},
					"list":      []any{nil, "known"},
					"map":       map[string]any{"empty": nil, "known": "known"},
					"set":       []any{nil},
					"tuple":     []any{nil, json.Number("1"), true},
				},
			},
		},
		{
			name:    "unknown_inputs",
			results: []string{"connection", "host", "options", "option", "extra_data", "nested"},
			unknown: true,
			want: map[string]map[string]any{
				"connection": {"test_case": "connection", "value": "known"},
				"host":       {"test_case": "host", "value": "known"},
				"options":    {"test_case": "options", "value": "known"},
				"option":     {"test_case": "option", "value": "known"},
				"extra_data": {"test_case": "extra_data", "value": "known"},
				"nested": {
					"test_case": "nested", "object": map[string]any{"list": []any{"known", "known"}},
				},
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			capture := &captureCheck{}
			server := httptest.NewServer(capture)
			defer server.Close()
			results := make(map[string]knownvalue.Check)
			for _, name := range scenario.results {
				results[name] = knownvalue.Bool(true)
			}
			known := inputCaptureCheck{capture: capture, want: scenario.want}
			initial := known
			// A true result does not mean an event was sent. Verify both the known
			// result and the absence of HTTP requests while the input is unknown.
			preApply := []plancheck.PlanCheck{
				plancheck.ExpectKnownOutputValue("results", knownvalue.ObjectExact(results)),
			}
			if scenario.unknown {
				initial.want = nil
				preApply = append(preApply, plancheck.ExpectUnknownValue("terraform_data.inputs", tfjsonpath.New("output")))
			}
			preApply = append(preApply, initial)
			resource.Test(t, resource.TestCase{Steps: []resource.TestStep{
				{
					ConfigDirectory: config.StaticDirectory(filepath.Join("testdata", "acceptance", scenario.name)),
					ConfigVariables: config.Variables{"host": config.StringVariable(server.URL)},
					ConfigPlanChecks: resource.ConfigPlanChecks{
						PreApply:             preApply,
						PostApplyPreRefresh:  []plancheck.PlanCheck{known},
						PostApplyPostRefresh: []plancheck.PlanCheck{known},
					},
					ConfigStateChecks: []statecheck.StateCheck{
						statecheck.ExpectKnownOutputValue("results", knownvalue.ObjectExact(results)), known,
					},
				},
			}})
		})
	}
}

// Each check consumes captures from one Terraform command. This keeps the initial
// unknown plan separate from saved-plan apply and the subsequent no-change plans.
type inputCaptureCheck struct {
	capture *captureCheck
	want    map[string]map[string]any
}

func (c inputCaptureCheck) CheckPlan(_ context.Context, _ plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	resp.Error = c.check()
}

func (c inputCaptureCheck) CheckState(_ context.Context, _ statecheck.CheckStateRequest, resp *statecheck.CheckStateResponse) {
	resp.Error = c.check()
}

func (c inputCaptureCheck) check() error {
	c.capture.mu.Lock()
	defer c.capture.mu.Unlock()
	defer func() { c.capture.events = nil }()
	if c.capture.err != nil {
		return c.capture.err
	}
	if len(c.capture.events) != len(c.want) {
		return fmt.Errorf("expected %d captures, got %d: %#v", len(c.want), len(c.capture.events), c.capture.events)
	}
	seen := make(map[string]bool)
	for _, properties := range c.capture.events {
		name, _ := properties["test_case"].(string)
		extra, ok := c.want[name]
		if !ok || seen[name] {
			return fmt.Errorf("unexpected or duplicate capture %q: %#v", name, properties)
		}
		seen[name] = true
		want := map[string]any{"$process_person_profile": false, "$geoip_disable": true}
		for key, value := range extra {
			want[key] = value
		}
		if !reflect.DeepEqual(properties, want) {
			return fmt.Errorf("capture %q: got %#v, want %#v", name, properties, want)
		}
	}
	return nil
}
