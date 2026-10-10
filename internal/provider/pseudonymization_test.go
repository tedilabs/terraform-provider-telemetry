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
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

func TestCapturePseudonymizesSelectedProperties(t *testing.T) {
	resetDeduplication(t)
	original := processCollector
	processCollector = telemetry.NewCollector()
	t.Cleanup(func() { processCollector = original })
	processCollector.LookupPublicIP = func(context.Context) string { return "203.0.113.7" }
	hostname, err := os.Hostname()
	if err != nil {
		t.Skip("no host name: ", err)
	}
	events := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		events <- string(body)
	}))
	defer server.Close()
	keys := types.TupleValueMust(
		[]attr.Type{types.StringType, types.StringType, types.StringType},
		[]attr.Value{types.StringValue("network.hostname"), types.StringValue("network.public_ip"), types.StringValue("module.version")},
	)
	pseudonymized := optionsWithAttributes(disabledOptions(), map[string]attr.Value{"network": types.BoolValue(true), "pseudonymized_keys": keys})
	raw := optionsWithAttributes(disabledOptions(), map[string]attr.Value{"network": types.BoolValue(true)})
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{"module": types.StringType}, map[string]attr.Value{"module": types.StringValue("vpc")}))
	runCapture(t, context.Background(), connectionValue(server.URL), pseudonymized, extra)
	runCapture(t, context.Background(), connectionValue(server.URL), raw, extra)
	if len(events) != 2 {
		t.Fatalf("expected two events, got %d", len(events))
	}
	bodies := []string{<-events, <-events}
	if strings.Contains(bodies[0], "203.0.113.7") || strings.Contains(bodies[0], `"`+hostname+`"`) {
		t.Fatalf("the event contains a pseudonymized value: %s", bodies[0])
	}
	network := func(body string) map[string]any {
		var payload struct{ Properties map[string]any }
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Properties["network"].(map[string]any)
	}
	pseudonym := func(value string) string {
		mac := hmac.New(sha256.New, []byte("test-token"))
		mac.Write([]byte(value))
		return hex.EncodeToString(mac.Sum(nil)[:16])
	}
	if got := network(bodies[0]); got["hostname"] != pseudonym(hostname) || got["public_ip"] != pseudonym("203.0.113.7") {
		t.Fatalf("unexpected pseudonyms: %v", got)
	}
	if !strings.Contains(bodies[0], `"module":"vpc"`) {
		t.Fatal("a path through a non-object value changed the event")
	}
	// Pseudonymization does not change the cached metadata of later calls.
	if got := network(bodies[1]); got["hostname"] != hostname || got["public_ip"] != "203.0.113.7" {
		t.Fatalf("the cached metadata was pseudonymized: %v", got)
	}
}

func TestPseudonymizedKeysOptionsValidation(t *testing.T) {
	for _, value := range []attr.Value{
		types.StringValue("network.hostname"), types.ListNull(types.StringType), types.ListUnknown(types.StringType),
		types.TupleValueMust([]attr.Type{types.BoolType}, []attr.Value{types.BoolValue(true)}),
		types.ListValueMust(types.StringType, []attr.Value{types.StringNull()}),
		types.ListValueMust(types.StringType, []attr.Value{types.StringUnknown()}),
		types.ListValueMust(types.StringType, []attr.Value{types.StringValue("")}),
		types.ListValueMust(types.StringType, []attr.Value{types.StringValue("network..hostname")}),
	} {
		_, err := collectionOptions(optionsWithAttributes(disabledOptions(), map[string]attr.Value{"pseudonymized_keys": value}))
		if err == nil || !strings.Contains(err.Error(), "options.pseudonymized_keys") {
			t.Errorf("accepted invalid pseudonymized_keys %v: %v", value, err)
		}
	}
	for _, keys := range []attr.Value{
		types.TupleValueMust([]attr.Type{types.StringType}, []attr.Value{types.StringValue("network.hostname")}),
		types.ListValueMust(types.StringType, []attr.Value{types.StringValue("network.hostname")}),
	} {
		opts, err := collectionOptions(optionsWithAttributes(disabledOptions(), map[string]attr.Value{"pseudonymized_keys": keys}))
		if err != nil || len(opts.pseudonymizedKeys) != 1 || opts.pseudonymizedKeys[0] != "network.hostname" || len(opts.deduplicationKeys) != 0 {
			t.Fatalf("valid keys were not decoded: %+v, %v", opts, err)
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
	module := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("module")})
	options := optionsWithAttributes(disabledOptions(), map[string]attr.Value{"pseudonymized_keys": module, "identity_keys": module})
	extra := types.DynamicValue(types.ObjectValueMust(map[string]attr.Type{"module": types.StringType}, map[string]attr.Value{"module": types.StringValue("vpc")}))
	runCapture(t, context.Background(), connectionValue(server.URL), options, extra)
	pseudonymized := telemetry.Pseudonymize(map[string]any{"module": "vpc"}, []string{"module"}, "test-token")
	want := telemetry.IdentityID(pseudonymized, []string{"module"})
	if distinctID != want || want == telemetry.IdentityID(map[string]any{"module": "vpc"}, []string{"module"}) {
		t.Fatalf("distinct ID %q is not derived from the pseudonym, want %q", distinctID, want)
	}
}
