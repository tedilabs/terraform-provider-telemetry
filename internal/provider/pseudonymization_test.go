package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

func pseudonymization(attributes map[string]attr.Value) types.Object {
	attributeTypes := map[string]attr.Type{}
	for key, value := range attributes {
		attributeTypes[key] = value.Type(context.Background())
	}
	return types.ObjectValueMust(attributeTypes, attributes)
}

func stringList(values ...string) types.List {
	elements := make([]attr.Value, len(values))
	for i, value := range values {
		elements[i] = types.StringValue(value)
	}
	return types.ListValueMust(types.StringType, elements)
}

func TestCapturePseudonymizesProperties(t *testing.T) {
	resetDeduplication(t)
	original := processCollector
	processCollector = telemetry.NewCollector()
	t.Cleanup(func() { processCollector = original })
	processCollector.LookupPublicIP = func(context.Context) string { return "203.0.113.7" }
	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("no host name: ", err)
	}
	events := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		events <- string(body)
	}))
	defer server.Close()
	options := func(settings map[string]attr.Value) types.Dynamic {
		attributes := map[string]attr.Value{"network": types.BoolValue(true)}
		if settings != nil {
			attributes["pseudonymization"] = pseudonymization(settings)
		}
		return optionsWithAttributes(disabledOptions(), attributes)
	}
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{"module": types.StringType}, map[string]attr.Value{"module": types.StringValue("vpc")}))
	runCapture(t, context.Background(), connectionValue(server.URL),
		options(map[string]attr.Value{"enabled": types.BoolValue(true), "additional_keys": stringList("module", "module.version")}), extra)
	runCapture(t, context.Background(), connectionValue(server.URL), options(nil), extra)
	// Listed keys without enabled = true skip the capture instead of sending the values.
	runCapture(t, context.Background(), connectionValue(server.URL), options(map[string]attr.Value{"additional_keys": stringList("module")}), extra)
	if len(events) != 2 {
		t.Fatalf("expected two events, got %d", len(events))
	}
	bodies := []string{<-events, <-events}
	properties := func(body string) map[string]any {
		var payload struct{ Properties map[string]any }
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Properties
	}
	pseudonym := func(value string) string {
		mac := hmac.New(sha256.New, []byte("test-token"))
		mac.Write([]byte(value))
		return hex.EncodeToString(mac.Sum(nil)[:16])
	}
	if strings.Contains(bodies[0], `"`+hostname+`"`) || strings.Contains(bodies[0], `"vpc"`) {
		t.Fatalf("the event contains a pseudonymized value: %s", bodies[0])
	}
	got := properties(bodies[0])
	network := got["network"].(map[string]any)
	if network["hostname"] != pseudonym(hostname) || got["module"] != pseudonym("vpc") {
		t.Fatalf("unexpected pseudonyms: %v", got)
	}
	// The public IP address is not a predefined pseudonymized property.
	if network["public_ip"] != "203.0.113.7" {
		t.Fatalf("public IP address changed: %v", network)
	}
	// Pseudonymization does not change the cached metadata of later calls.
	if got := properties(bodies[1]); got["network"].(map[string]any)["hostname"] != hostname || got["module"] != "vpc" {
		t.Fatalf("the cached metadata was pseudonymized: %v", got)
	}
}

func TestPseudonymizationOptionsValidation(t *testing.T) {
	for _, value := range []attr.Value{
		types.StringValue("enabled"),
		types.ObjectNull(map[string]attr.Type{"enabled": types.BoolType}),
		types.ObjectUnknown(map[string]attr.Type{"enabled": types.BoolType}),
		pseudonymization(map[string]attr.Value{"enabled": types.BoolNull()}),
		pseudonymization(map[string]attr.Value{"enabled": types.BoolUnknown()}),
		pseudonymization(map[string]attr.Value{"enabled": types.StringValue("true")}),
		pseudonymization(map[string]attr.Value{"enabeld": types.BoolValue(true)}),
		pseudonymization(map[string]attr.Value{"enabled": types.BoolValue(true), "additional_keys": types.StringValue("module")}),
		pseudonymization(map[string]attr.Value{"enabled": types.BoolValue(true), "additional_keys": types.ListNull(types.StringType)}),
		pseudonymization(map[string]attr.Value{"enabled": types.BoolValue(true), "additional_keys": stringList("network..hostname")}),
		pseudonymization(map[string]attr.Value{"enabled": types.BoolValue(false), "additional_keys": stringList("module")}),
		pseudonymization(map[string]attr.Value{"additional_keys": stringList("module")}),
	} {
		_, err := collectionOptions(optionsWithAttributes(disabledOptions(), map[string]attr.Value{"pseudonymization": value}))
		if err == nil || !strings.Contains(err.Error(), "options.pseudonymization") {
			t.Errorf("accepted invalid pseudonymization %v: %v", value, err)
		}
	}
	predefined := telemetry.PseudonymizedProperties()
	for _, test := range []struct {
		value attr.Value
		want  []string
	}{
		{pseudonymization(map[string]attr.Value{}), nil},
		{pseudonymization(map[string]attr.Value{"enabled": types.BoolValue(false), "additional_keys": stringList()}), nil},
		{pseudonymization(map[string]attr.Value{"enabled": types.BoolValue(true)}), predefined},
		{types.MapValueMust(types.BoolType, map[string]attr.Value{"enabled": types.BoolValue(true)}), predefined},
		{pseudonymization(map[string]attr.Value{
			"enabled": types.BoolValue(true), "additional_keys": types.TupleValueMust([]attr.Type{types.StringType}, []attr.Value{types.StringValue("module")}),
		}), append(telemetry.PseudonymizedProperties(), "module")},
	} {
		opts, err := collectionOptions(optionsWithAttributes(disabledOptions(), map[string]attr.Value{"pseudonymization": test.value}))
		if err != nil || !reflect.DeepEqual(opts.pseudonymizedKeys, test.want) || len(opts.deduplicationKeys) != 0 {
			t.Errorf("%v: got %v, %v, want %v", test.value, opts.pseudonymizedKeys, err, test.want)
		}
	}
}

func TestIdentityKeysUsePseudonyms(t *testing.T) {
	resetDeduplication(t)
	var distinctID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			DistinctID string `json:"distinct_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		distinctID = payload.DistinctID
	}))
	defer server.Close()
	options := optionsWithAttributes(disabledOptions(), map[string]attr.Value{
		"pseudonymization": pseudonymization(map[string]attr.Value{"enabled": types.BoolValue(true), "additional_keys": stringList("module")}),
		"identity_keys":    stringList("module"),
	})
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{"module": types.StringType}, map[string]attr.Value{"module": types.StringValue("vpc")}))
	runCapture(t, context.Background(), connectionValue(server.URL), options, extra)
	pseudonymized := telemetry.Pseudonymize(map[string]any{"module": "vpc"}, []string{"module"}, "test-token")
	want := telemetry.IdentityID(pseudonymized, []string{"module"})
	if distinctID != want || want == telemetry.IdentityID(map[string]any{"module": "vpc"}, []string{"module"}) {
		t.Fatalf("distinct ID %q is not derived from the pseudonym, want %q", distinctID, want)
	}
}
