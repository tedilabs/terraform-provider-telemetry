package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

func TestMergeProperties(t *testing.T) {
	base := map[string]any{
		"machine": map[string]any{"os": map[string]any{"name": "Linux", "version": "old"}, "cpu_count": 4},
		"items":   []any{1, 2}, "nullable": map[string]any{"key": "value"}, "scalar": "old", "keep": true,
	}
	overrides := map[string]any{
		"machine": map[string]any{"os": map[string]any{"version": "new"}},
		"items":   []any{3}, "nullable": nil, "scalar": map[string]any{"key": "new"}, "added": json.Number("9007199254740993"),
	}
	beforeBase, _ := json.Marshal(base)
	beforeOverrides, _ := json.Marshal(overrides)
	got := mergeProperties(base, overrides)
	want := map[string]any{
		"machine": map[string]any{"os": map[string]any{"name": "Linux", "version": "new"}, "cpu_count": 4},
		"items":   []any{3}, "nullable": nil, "scalar": map[string]any{"key": "new"}, "keep": true, "added": json.Number("9007199254740993"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected merge: %v", got)
	}
	afterBase, _ := json.Marshal(base)
	afterOverrides, _ := json.Marshal(overrides)
	if string(beforeBase) != string(afterBase) || string(beforeOverrides) != string(afterOverrides) {
		t.Fatal("merge modified an input")
	}
	if got := mergeProperties(base, map[string]any{"machine": "replacement"}); got["machine"] != "replacement" {
		t.Fatal("scalar did not replace object")
	}
}

func TestCaptureExtraDataOverridesBuiltinsWithoutChangingCache(t *testing.T) {
	original := processCollector
	processCollector = telemetry.NewCollector()
	processCollector.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("missing tool") }
	t.Cleanup(func() { processCollector = original })
	events := make(chan map[string]any, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload["api_key"] != "test-token" || payload["event"] != "terraform_capture" {
			t.Error("event properties overwrote transport fields")
		}
		events <- payload["properties"].(map[string]any)
	}))
	defer server.Close()
	opts := optionsWithAttributes(disabledOptions(), map[string]attr.Value{
		"machine": types.BoolValue(true), "toolchain": types.BoolValue(true), "deduplication_enabled": types.BoolValue(false),
	})
	ctx := context.Background()
	osOverride := types.ObjectValueMust(map[string]attr.Type{"version": types.StringType}, map[string]attr.Value{"version": types.StringValue("caller-version")})
	machineOverride := types.ObjectValueMust(map[string]attr.Type{"os": osOverride.Type(ctx)}, map[string]attr.Value{"os": osOverride})
	toolchainOverride := types.ObjectValueMust(map[string]attr.Type{"telemetry_provider": types.StringType}, map[string]attr.Value{"telemetry_provider": types.StringValue("caller-provider")})
	for _, value := range []attr.Value{toolchainOverride, types.StringNull(), types.StringValue("custom")} {
		extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{
			"machine": machineOverride.Type(ctx), "toolchain": value.Type(ctx),
			"$geoip_disable": types.BoolType, "$process_person_profile": types.BoolType, "api_key": types.StringType,
		}, map[string]attr.Value{
			"machine": machineOverride, "toolchain": value,
			"$geoip_disable": types.BoolValue(false), "$process_person_profile": types.BoolValue(true), "api_key": types.StringValue("property-only"),
		}))
		runCapture(t, ctx, connectionValue(server.URL), opts, extra)
		props := <-events
		machine := props["machine"].(map[string]any)
		osInfo := machine["os"].(map[string]any)
		if osInfo["version"] != "caller-version" || osInfo["name"] == nil || machine["arch"] == nil {
			t.Fatalf("incorrect deep merge: %v", props)
		}
		if props["$geoip_disable"] != false || props["$process_person_profile"] != true || props["api_key"] != "property-only" {
			t.Fatalf("user properties lost precedence: %v", props)
		}
		if value.IsNull() {
			if props["toolchain"] != nil {
				t.Error("null did not replace toolchain")
			}
		} else if value.Equal(toolchainOverride) {
			if props["toolchain"].(map[string]any)["telemetry_provider"] != "caller-provider" {
				t.Error("build version overwrote caller value")
			}
		} else if props["toolchain"] != "custom" {
			t.Error("scalar did not replace toolchain")
		}
	}
	empty := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{}, map[string]attr.Value{}))
	runCapture(t, ctx, connectionValue(server.URL), opts, empty)
	props := <-events
	if props["machine"].(map[string]any)["os"].(map[string]any)["version"] == "caller-version" || props["toolchain"].(map[string]any)["telemetry_provider"] != "dev" {
		t.Fatal("extra_data contaminated cached metadata")
	}
	if props["$geoip_disable"] != true || props["$process_person_profile"] != false {
		t.Fatal("defaults were not restored for the next call")
	}
}

func TestDeduplicationUsesMergedProperties(t *testing.T) {
	resetDeduplication(t)
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1) }))
	defer server.Close()
	for _, machineEnabled := range []bool{true, false} {
		opts := optionsWithAttributes(disabledOptions(), map[string]attr.Value{"machine": types.BoolValue(machineEnabled)})
		extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{"machine": types.StringType}, map[string]attr.Value{"machine": types.StringValue("same-final-value")}))
		runCapture(t, context.Background(), connectionValue(server.URL), opts, extra)
	}
	if sends.Load() != 1 {
		t.Fatalf("same final properties should deduplicate, got %d", sends.Load())
	}
}
