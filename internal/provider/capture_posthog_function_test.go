package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

var connectionTypes = map[string]attr.Type{"host": types.StringType, "project_token": types.StringType}

var optionTypes = map[string]attr.Type{
	"machine": types.BoolType, "network": types.BoolType, "git": types.BoolType,
	"github": types.BoolType, "github_actions": types.BoolType, "hcp_terraform": types.BoolType,
	"terraform": types.BoolType, "toolchain": types.BoolType,
}

func optionsWithCache(options types.Object, flag attr.Value) types.Dynamic {
	attributes := options.Attributes()
	attributeTypes := options.AttributeTypes(context.Background())
	if flag != nil {
		attributes["cache_enabled"] = flag
		attributeTypes["cache_enabled"] = flag.Type(context.Background())
	}
	return types.DynamicValue(types.ObjectValueMust(attributeTypes, attributes))
}

func connectionValue(host string) types.Object {
	return types.ObjectValueMust(connectionTypes, map[string]attr.Value{
		"host": types.StringValue(host), "project_token": types.StringValue("test-token"),
	})
}

func disabledOptions() types.Object {
	attributes := map[string]attr.Value{}
	for key := range optionTypes {
		attributes[key] = types.BoolValue(false)
	}
	return types.ObjectValueMust(optionTypes, attributes)
}

func runCapture(t *testing.T, ctx context.Context, args ...attr.Value) {
	t.Helper()
	if _, ok := args[1].(types.Dynamic); !ok {
		args[1] = types.DynamicValue(args[1])
	}
	resp := function.RunResponse{Result: function.NewResultData(types.BoolUnknown())}
	NewCapturePostHogFunction("dev").Run(ctx, function.RunRequest{Arguments: function.NewArgumentsData(args)}, &resp)
	if resp.Error != nil || !resp.Result.Value().Equal(types.BoolValue(true)) {
		t.Fatalf("expected true without errors, got %+v", resp)
	}
}

func TestCaptureFlattensExtraDataAndPreservesTypes(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		var payload map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		properties := payload["properties"].(map[string]any)
		if len(properties) != 11 {
			t.Errorf("unexpected property count: %v", properties)
		}
		if _, wrapped := properties["extra_data"]; wrapped {
			t.Error("extra_data wrapper remains")
		}
		extra := properties
		if extra["count"] != json.Number("9007199254740993") || extra["enabled"] != true || extra["nullable"] != nil {
			t.Errorf("extra_data lost types or precision: %v", extra)
		}
		if extra["machine"] != "user-defined" || extra["$custom"] != "preserved" {
			t.Error("extra_data keys were overwritten")
		}
		if extra["nested"].(map[string]any)["name"] != "demo" {
			t.Error("nested object lost")
		}
		if len(extra["items"].([]any)) != 2 {
			t.Error("tuple lost")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	nested := types.ObjectValueMust(map[string]attr.Type{"name": types.StringType}, map[string]attr.Value{"name": types.StringValue("demo")})
	items := types.TupleValueMust([]attr.Type{types.StringType, types.BoolType}, []attr.Value{types.StringValue("a"), types.BoolValue(false)})
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{
		"count": types.Int64Type, "enabled": types.BoolType, "nullable": types.StringType,
		"machine": types.StringType, "$custom": types.StringType, "nested": nested.Type(context.Background()), "items": items.Type(context.Background()),
	}, map[string]attr.Value{
		"count": types.Int64Value(9007199254740993), "enabled": types.BoolValue(true), "nullable": types.StringNull(),
		"machine": types.StringValue("user-defined"), "$custom": types.StringValue("preserved"), "nested": nested, "items": items,
	}))
	runCapture(t, context.Background(), connectionValue(server.URL), disabledOptions(), extra)
	if count.Load() != 1 {
		t.Fatalf("expected event, got %d", count.Load())
	}
}

func TestCaptureAlwaysTrueOnDeliveryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	empty := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{}, map[string]attr.Value{}))
	runCapture(t, context.Background(), connectionValue(server.URL), disabledOptions(), empty)
	server.Close()
	runCapture(t, context.Background(), connectionValue(server.URL), disabledOptions(), empty)
	runCapture(t, context.Background(), connectionValue(":invalid"), disabledOptions(), empty)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runCapture(t, ctx, connectionValue(server.URL), disabledOptions(), empty)
}

func TestCaptureSkipsIncompleteInputsAndReturnsTrue(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1) }))
	defer server.Close()
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{}, map[string]attr.Value{}))
	nestedUnknown := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{"value": types.StringType}, map[string]attr.Value{"value": types.StringUnknown()}))
	for _, args := range [][]attr.Value{
		{types.ObjectUnknown(connectionTypes), disabledOptions(), extra},
		{types.ObjectNull(connectionTypes), disabledOptions(), extra},
		{connectionValue(server.URL), types.ObjectUnknown(optionTypes), extra},
		{connectionValue(server.URL), types.ObjectNull(optionTypes), extra},
		{connectionValue(server.URL), disabledOptions(), types.DynamicUnknown()},
		{connectionValue(server.URL), disabledOptions(), nestedUnknown},
		{connectionValue(server.URL), disabledOptions(), types.DynamicValue(types.StringValue("not a map"))},
	} {
		runCapture(t, context.Background(), args...)
	}
	if count.Load() != 0 {
		t.Fatal("sent incomplete event")
	}
}

func TestFunctionOptsIntoNullAndUnknownArguments(t *testing.T) {
	var response function.DefinitionResponse
	NewCapturePostHogFunction("dev").Definition(context.Background(), function.DefinitionRequest{}, &response)
	if len(response.Definition.Parameters) != 3 || response.Definition.VariadicParameter != nil {
		t.Fatal("capture_posthog must accept exactly three arguments")
	}
	for _, param := range response.Definition.Parameters {
		if !param.GetAllowNullValue() || !param.GetAllowUnknownValues() {
			t.Errorf("%s may bypass always-true behavior", param.GetName())
		}
	}
}

func TestCaptureCachesOnlyPredefinedMetadata(t *testing.T) {
	original := processCollector
	processCollector = telemetry.NewCollector()
	t.Cleanup(func() { processCollector = original })
	var reads int
	processCollector.Run = func(context.Context, string, ...string) ([]byte, error) {
		reads++
		return []byte(fmt.Sprintf(`{"login":"user-%d"}`, reads)), nil
	}
	events := make(chan map[string]any, 4)
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = testTransport(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		events <- payload["properties"].(map[string]any)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	attributes := disabledOptions().Attributes()
	attributes["github"] = types.BoolValue(true)
	opts := types.ObjectValueMust(optionTypes, attributes)
	for i, flag := range []attr.Value{nil, types.BoolValue(true), types.BoolValue(false), types.BoolValue(true)} {
		github := types.ObjectValueMust(map[string]attr.Type{"sample": types.Int64Type}, map[string]attr.Value{"sample": types.Int64Value(int64(i))})
		extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{"github": github.Type(context.Background())}, map[string]attr.Value{"github": github}))
		args := []attr.Value{connectionValue("https://example.invalid"), optionsWithCache(opts, flag), extra}
		// Each call constructs a new function instance, as the framework may do.
		runCapture(t, context.Background(), args...)
		select {
		case props := <-events:
			wantUser := "user-1"
			if i == 2 {
				wantUser = "user-2"
			}
			if props["github"].(map[string]any)["login"] != wantUser || props["github"].(map[string]any)["sample"] != float64(i) {
				t.Fatalf("stale extra_data or wrong cache behavior: %v", props)
			}
		default:
			t.Fatal("collection caching suppressed an event")
		}
	}
	if reads != 2 {
		t.Fatalf("expected default cached read and bypass read, got %d", reads)
	}
}

func TestCaptureSkipsInvalidCacheArguments(t *testing.T) {
	var count atomic.Int32
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = testTransport(func(*http.Request) (*http.Response, error) {
		count.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{}, map[string]attr.Value{}))
	for _, flag := range []attr.Value{types.BoolNull(), types.BoolUnknown(), types.StringValue("false")} {
		args := []attr.Value{connectionValue("https://example.invalid"), optionsWithCache(disabledOptions(), flag), extra}
		runCapture(t, context.Background(), args...)
	}
	if count.Load() != 0 {
		t.Fatal("invalid cache arguments caused capture")
	}
}

// These cache tests exercise HTTP payload construction without opening sockets.
type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestCaptureCacheArgumentsThroughProtocol(t *testing.T) {
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	var sends int
	http.DefaultTransport = testTransport(func(*http.Request) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	server := providerserver.NewProtocol6(New("test")())()
	ctx := context.Background()
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{}, map[string]attr.Value{}))
	for _, flag := range []attr.Value{nil, types.BoolValue(true), types.BoolValue(false)} {
		options := optionsWithCache(disabledOptions(), flag).UnderlyingValue().(types.Object)
		args := []attr.Value{connectionValue("https://example.invalid"), optionsWithAttributes(options, map[string]attr.Value{"deduplication_enabled": types.BoolValue(false)}), extra}
		req := &tfprotov6.CallFunctionRequest{Name: "capture_posthog"}
		for _, arg := range args {
			value, err := arg.ToTerraformValue(ctx)
			if err != nil {
				t.Fatal(err)
			}
			dynamic, err := tfprotov6.NewDynamicValue(arg.Type(ctx).TerraformType(ctx), value)
			if err != nil {
				t.Fatal(err)
			}
			req.Arguments = append(req.Arguments, &dynamic)
		}
		resp, err := server.CallFunction(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Error != nil || resp.Result == nil {
			t.Fatalf("protocol call failed: %+v", resp)
		}
		value, err := resp.Result.Unmarshal(tftypes.Bool)
		if err != nil {
			t.Fatal(err)
		}
		var result bool
		if err := value.As(&result); err != nil || !result {
			t.Fatalf("expected true: %v, %v", value, err)
		}
	}
	if sends != 3 {
		t.Fatalf("expected captures with omitted/true/false cache, got %d", sends)
	}
}

func TestCollectionOptionsValidation(t *testing.T) {
	for _, asMap := range []bool{false, true} {
		attributes := disabledOptions().Attributes()
		attributes["git"] = types.BoolValue(true)
		var value types.Dynamic
		if asMap {
			attributes["cache_enabled"] = types.BoolValue(false)
			value = types.DynamicValue(types.MapValueMust(types.BoolType, attributes))
		} else {
			value = optionsWithCache(types.ObjectValueMust(optionTypes, attributes), types.BoolValue(false))
		}
		opts, err := collectionOptions(value)
		if err != nil || opts.cacheEnabled || opts.collect != (telemetry.Options{Git: true}) {
			t.Fatalf("invalid parsed options: %+v, cache=%v, err=%v", opts, opts.cacheEnabled, err)
		}
	}
	for name := range optionTypes {
		for _, invalid := range []attr.Value{types.BoolNull(), types.BoolUnknown(), types.StringValue("true")} {
			if _, err := collectionOptions(optionsWithAttributes(disabledOptions(), map[string]attr.Value{name: invalid})); err == nil {
				t.Fatalf("invalid %s accepted", name)
			}
		}
	}
	for _, value := range []types.Dynamic{
		optionsWithAttributes(disabledOptions(), map[string]attr.Value{"netwrok": types.BoolValue(false)}),
		types.DynamicValue(types.MapValueMust(types.BoolType, map[string]attr.Value{"netwrok": types.BoolValue(false)})),
	} {
		if _, err := collectionOptions(value); err == nil || !strings.Contains(err.Error(), `"netwrok"`) {
			t.Fatalf("misspelled option was not rejected: %v", err)
		}
	}
	for _, value := range []types.Dynamic{
		types.DynamicNull(),
		types.DynamicUnknown(),
		types.DynamicValue(types.StringValue("invalid")),
		types.DynamicValue(types.ObjectNull(optionTypes)),
		types.DynamicValue(types.ObjectUnknown(optionTypes)),
		types.DynamicValue(types.MapNull(types.BoolType)),
		types.DynamicValue(types.MapUnknown(types.BoolType)),
		types.DynamicValue(types.MapValueMust(types.StringType, map[string]attr.Value{"machine": types.StringValue("true")})),
	} {
		if _, err := collectionOptions(value); err == nil {
			t.Fatalf("accepted invalid options: %v", value)
		}
	}
}

func TestCollectionOptionDefaults(t *testing.T) {
	want := telemetry.Options{Machine: true, Network: true, Git: true, GitHubActions: true, HCPTerraform: true, Terraform: true, Toolchain: true}
	for _, value := range []types.Dynamic{
		types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{}, map[string]attr.Value{})),
		types.DynamicValue(types.MapValueMust(types.BoolType, map[string]attr.Value{})),
	} {
		opts, err := collectionOptions(value)
		if err != nil || opts.collect != want || !opts.cacheEnabled || !opts.deduplicationEnabled || len(opts.deduplicationKeys) != 0 || len(opts.identityKeys) != 0 {
			t.Fatalf("unexpected defaults for %v: %+v, err=%v", value, opts, err)
		}
	}
	// Each attribute overrides only its own default.
	opts, err := collectionOptions(types.DynamicValue(types.ObjectValueMust(
		map[string]attr.Type{"network": types.BoolType, "github": types.BoolType},
		map[string]attr.Value{"network": types.BoolValue(false), "github": types.BoolValue(true)},
	)))
	want.Network, want.GitHub = false, true
	if err != nil || opts.collect != want {
		t.Fatalf("unexpected overridden defaults: %+v, err=%v", opts, err)
	}
}

func TestTerraformAndToolchainCategories(t *testing.T) {
	original := processCollector
	processCollector = telemetry.NewCollector()
	t.Cleanup(func() { processCollector = original })
	processCollector.Getenv = func(key string) string {
		if key == "TF_WORKSPACE" {
			return "prod"
		}
		return ""
	}
	processCollector.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, fmt.Errorf("not installed") }
	options := optionsWithAttributes(disabledOptions(), map[string]attr.Value{
		"terraform": types.BoolValue(true), "toolchain": types.BoolValue(true), "deduplication_enabled": types.BoolValue(false),
	})
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	var sends int
	http.DefaultTransport = testTransport(func(req *http.Request) (*http.Response, error) {
		sends++
		var payload map[string]any
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		props := payload["properties"].(map[string]any)
		if props["terraform"].(map[string]any)["workspace"] != "prod" {
			t.Error("missing workspace")
		}
		if props["toolchain"].(map[string]any)["telemetry_provider"] != "test-version" {
			t.Error("missing build version")
		}
		if props["$lib"] != "terraform-provider-telemetry" || props["$lib_version"] != "test-version" {
			t.Errorf("missing library properties: %v, %v", props["$lib"], props["$lib_version"])
		}
		if req.Header.Get("User-Agent") != "terraform-provider-telemetry/test-version" {
			t.Errorf("unexpected User-Agent: %q", req.Header.Get("User-Agent"))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	p := New("test-version")().(*TelemetryProvider)
	f := p.Functions(context.Background())[0]()
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{}, map[string]attr.Value{}))
	resp := function.RunResponse{}
	f.Run(context.Background(), function.RunRequest{Arguments: function.NewArgumentsData([]attr.Value{connectionValue("https://example.invalid"), options, extra})}, &resp)
	if resp.Error != nil || !resp.Result.Value().Equal(types.BoolValue(true)) || sends != 1 {
		t.Fatalf("capture failed: %+v", resp)
	}
}

func TestCaptureLogsWithoutSecrets(t *testing.T) {
	resetDeduplication(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	var output bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &output)
	connection := types.ObjectValueMust(connectionTypes, map[string]attr.Value{
		"host": types.StringValue(server.URL), "project_token": types.StringValue("secret-token"),
	})
	extra := types.DynamicValue(types.ObjectValueMust(
		map[string]attr.Type{"value": types.StringType}, map[string]attr.Value{"value": types.StringValue("private-value")},
	))
	runCapture(t, ctx, connection, optionsWithAttributes(disabledOptions(), map[string]attr.Value{"netwrok": types.BoolValue(false)}), extra)
	runCapture(t, ctx, connection, disabledOptions(), extra)
	runCapture(t, ctx, connection, disabledOptions(), extra)
	raw := output.String()
	if strings.Contains(raw, "secret-token") || strings.Contains(raw, "private-value") {
		t.Fatalf("logs contain the project token or event properties: %s", raw)
	}
	entries, err := tflogtest.MultilineJSONDecode(&output)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ message, field, contains string }{
		{"Skipped telemetry capture", "reason", `"netwrok"`},
		{"Failed to send telemetry event", "error", "500"},
		{"Skipped telemetry capture", "reason", "identical event"},
	}
	if len(entries) != len(want) {
		t.Fatalf("expected %d log entries, got %d: %s", len(want), len(entries), raw)
	}
	for i, entry := range entries {
		value, _ := entry[want[i].field].(string)
		if entry["@level"] != "debug" || entry["@message"] != want[i].message || !strings.Contains(value, want[i].contains) {
			t.Errorf("unexpected log entry %d: %v", i, entry)
		}
	}
}
