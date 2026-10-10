package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

func TestCaptureIdentityKeys(t *testing.T) {
	var ids []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		id, _ := payload["distinct_id"].(string)
		ids = append(ids, id)
	}))
	defer server.Close()
	options := func(keys ...string) types.Dynamic {
		elements := make([]attr.Value, len(keys))
		for i, key := range keys {
			elements[i] = types.StringValue(key)
		}
		return optionsWithAttributes(disabledOptions(), map[string]attr.Value{
			"deduplication_enabled": types.BoolValue(false),
			"identity_keys":         types.ListValueMust(types.StringType, elements),
		})
	}
	var logs bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &logs)
	for _, call := range []struct {
		options types.Dynamic
		extra   types.Dynamic
	}{
		{options("module"), moduleExtra("vpc", 0)},
		{options("module"), moduleExtra("vpc", 1)},
		{options("module"), moduleExtra("subnet", 0)},
		{options("version"), moduleExtra("vpc", 0)},
		{options(), moduleExtra("vpc", 0)},
	} {
		runCapture(t, ctx, connectionValue(server.URL), call.options, call.extra)
	}
	if len(ids) != 5 {
		t.Fatalf("expected 5 events, got %d", len(ids))
	}
	hash, uuid := regexp.MustCompile(`^[0-9a-f]{32}$`), regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !hash.MatchString(ids[0]) || ids[1] != ids[0] {
		t.Errorf("events with the same selected value have different identities: %v", ids[:2])
	}
	if !hash.MatchString(ids[2]) || ids[2] == ids[0] {
		t.Errorf("events with different selected values share an identity: %v", ids[:3])
	}
	if !uuid.MatchString(ids[3]) || !uuid.MatchString(ids[4]) || ids[3] == ids[4] {
		t.Errorf("a missing path or no identity keys did not use random UUIDs: %v", ids[3:])
	}
	if n := strings.Count(logs.String(), "a path in identity_keys is missing"); n != 1 || strings.Contains(logs.String(), "vpc") {
		t.Errorf("expected one debug log for the missing path without values, got %d: %s", n, logs.String())
	}
}
