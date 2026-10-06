package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

func optionsWithAttributes(base types.Object, values map[string]attr.Value) types.Dynamic {
	attributes := base.Attributes()
	attributeTypes := base.AttributeTypes(context.Background())
	for key, value := range values {
		attributes[key] = value
		attributeTypes[key] = value.Type(context.Background())
	}
	return types.DynamicValue(types.ObjectValueMust(attributeTypes, attributes))
}

func resetDeduplication(t *testing.T) {
	t.Helper()
	original := processDeduplicator
	processDeduplicator = &telemetry.Deduplicator{}
	t.Cleanup(func() { processDeduplicator = original })
}

func moduleExtra(module string, instance int64) types.Dynamic {
	return types.DynamicValue(types.ObjectValueMust(
		map[string]attr.Type{"module": types.StringType, "instance": types.Int64Type},
		map[string]attr.Value{"module": types.StringValue(module), "instance": types.Int64Value(instance)},
	))
}

func TestCaptureDeduplicationConcurrentAndFailure(t *testing.T) {
	resetDeduplication(t)
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	options := optionsWithAttributes(disabledOptions(), map[string]attr.Value{
		"deduplication_enabled": types.BoolValue(true),
		"deduplication_keys":    types.TupleValueMust([]attr.Type{types.StringType}, []attr.Value{types.StringValue("module")}),
	})
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			runCapture(t, context.Background(), connectionValue(server.URL), options, moduleExtra("vpc", int64(i)))
		})
	}
	wg.Wait()
	if sends.Load() != 1 {
		t.Fatalf("expected one attempt, even when delivery fails, got %d", sends.Load())
	}
	runCapture(t, context.Background(), connectionValue(server.URL), options, moduleExtra("subnet", 0))
	if sends.Load() != 2 {
		t.Fatal("different module did not capture")
	}
	// Disabling collection caching does not disable event deduplication.
	uncached := optionsWithAttributes(options.UnderlyingValue().(types.Object), map[string]attr.Value{"cache_enabled": types.BoolValue(false)})
	runCapture(t, context.Background(), connectionValue(server.URL), uncached, moduleExtra("vpc", 100))
	if sends.Load() != 2 {
		t.Fatal("cache_enabled=false bypassed event deduplication")
	}
}

func TestCaptureDeduplicationDefaultsAndBypass(t *testing.T) {
	resetDeduplication(t)
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1) }))
	defer server.Close()
	extra := moduleExtra("vpc", 0)
	noDedup := optionsWithAttributes(disabledOptions(), map[string]attr.Value{"deduplication_enabled": types.BoolValue(false)})
	// Disabled calls neither reserve an identity nor consult an existing identity.
	for _, options := range []attr.Value{noDedup, disabledOptions(), disabledOptions(), noDedup} {
		runCapture(t, context.Background(), connectionValue(server.URL), options, extra)
	}
	if sends.Load() != 3 {
		t.Fatalf("expected bypass, first default, bypass; got %d", sends.Load())
	}
	emptyKeys := optionsWithAttributes(disabledOptions(), map[string]attr.Value{"deduplication_keys": types.TupleValueMust([]attr.Type{}, []attr.Value{})})
	runCapture(t, context.Background(), connectionValue(server.URL), emptyKeys, extra)
	if sends.Load() != 3 {
		t.Fatal("empty keys differed from omitted keys")
	}
	runCapture(t, context.Background(), connectionValue(server.URL), disabledOptions(), moduleExtra("vpc", 1))
	if sends.Load() != 4 {
		t.Fatal("default deduplication lost changed extra_data")
	}
}

func TestCaptureDeduplicationMissingKeysAndConnections(t *testing.T) {
	resetDeduplication(t)
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1) }))
	defer server.Close()
	missing := optionsWithAttributes(disabledOptions(), map[string]attr.Value{
		"deduplication_keys": types.ListValueMust(types.StringType, []attr.Value{types.StringValue("version")}),
	})
	for range 2 {
		runCapture(t, context.Background(), connectionValue(server.URL), missing, moduleExtra("vpc", 0))
	}
	for _, token := range []string{"first", "second", "first"} {
		connection := types.ObjectValueMust(connectionTypes, map[string]attr.Value{
			"host": types.StringValue(server.URL), "project_token": types.StringValue(token),
		})
		runCapture(t, context.Background(), connection, disabledOptions(), moduleExtra("vpc", 0))
	}
	if sends.Load() != 4 {
		t.Fatalf("missing keys or distinct connections were suppressed: %d", sends.Load())
	}
}

func TestDeduplicationOptionsValidation(t *testing.T) {
	for name, values := range map[string][]attr.Value{
		"deduplication_enabled": {types.BoolNull(), types.BoolUnknown(), types.StringValue("true")},
		"deduplication_keys": {
			types.StringValue("module"), types.ListNull(types.StringType), types.ListUnknown(types.StringType),
			types.TupleValueMust([]attr.Type{types.BoolType}, []attr.Value{types.BoolValue(true)}),
			types.ListValueMust(types.StringType, []attr.Value{types.StringNull()}),
			types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()}),
			types.ListValueMust(types.StringType, []attr.Value{types.StringValue("")}),
			types.ListValueMust(types.StringType, []attr.Value{types.StringValue("terraform..workspace")}),
		},
	} {
		for _, value := range values {
			if _, ok := collectionOptions(optionsWithAttributes(disabledOptions(), map[string]attr.Value{name: value})); ok {
				t.Errorf("accepted invalid %s: %v", name, value)
			}
		}
	}
	for _, keys := range []attr.Value{
		types.TupleValueMust([]attr.Type{types.StringType}, []attr.Value{types.StringValue("module")}),
		types.ListValueMust(types.StringType, []attr.Value{types.StringValue("module")}),
	} {
		opts, ok := collectionOptions(optionsWithAttributes(disabledOptions(), map[string]attr.Value{"deduplication_keys": keys}))
		if !ok || !opts.deduplicationEnabled || len(opts.deduplicationKeys) != 1 || opts.deduplicationKeys[0] != "module" {
			t.Fatalf("valid keys were not decoded: %+v", opts)
		}
	}
}
