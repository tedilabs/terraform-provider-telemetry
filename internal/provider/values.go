package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

// Shared for the lifetime of this provider process, across function instances.
var processCollector = telemetry.NewCollector()

// Shared by capture functions for all telemetry destinations.
func optionsParameter() function.DynamicParameter {
	// A dynamic object preserves an omitted cache_enabled attribute. ObjectParameter
	// requires every declared attribute and cannot express this default.
	return function.DynamicParameter{
		Name: "options", AllowNullValue: true, AllowUnknownValues: true,
		Description: "Object or map with required machine, network, git, github, and github_actions booleans. Optional cache_enabled defaults to true; false bypasses metadata cache reads and writes.",
	}
}

func collectionOptions(options types.Dynamic) (telemetry.Options, bool, bool) {
	opts := telemetry.Options{}
	var attributes map[string]attr.Value
	switch value := options.UnderlyingValue().(type) {
	case types.Object:
		attributes = value.Attributes()
	case types.Map:
		attributes = value.Elements()
	default:
		return opts, false, false
	}
	for key, target := range map[string]*bool{
		"machine": &opts.Machine, "network": &opts.Network, "git": &opts.Git,
		"github": &opts.GitHub, "github_actions": &opts.GitHubActions,
	} {
		value, ok := attributes[key].(types.Bool)
		if !ok || value.IsNull() || value.IsUnknown() {
			return opts, false, false
		}
		*target = value.ValueBool()
	}
	cache := true
	if value, exists := attributes["cache_enabled"]; exists {
		flag, ok := value.(types.Bool)
		if !ok || flag.IsNull() || flag.IsUnknown() {
			return opts, false, false
		}
		cache = flag.ValueBool()
	}
	return opts, cache, true
}

func collectProperties(ctx context.Context, options types.Dynamic, extra types.Dynamic) (map[string]any, bool) {
	if options.IsNull() || !fullyKnown(ctx, options) || !fullyKnown(ctx, extra) {
		return nil, false
	}
	opts, cache, ok := collectionOptions(options)
	if !ok {
		return nil, false
	}
	extraData := map[string]any{}
	if !extra.IsNull() && !extra.IsUnderlyingValueNull() {
		v, err := extra.UnderlyingValue().ToTerraformValue(ctx)
		if err != nil {
			return nil, false
		}
		decoded, err := jsonValue(v)
		if err != nil {
			return nil, false
		}
		var ok bool
		extraData, ok = decoded.(map[string]any)
		if !ok {
			return nil, false
		}
	}
	properties := processCollector.Collect(ctx, opts, cache)
	properties["extra_data"] = extraData
	return properties, true
}

func fullyKnown(ctx context.Context, value attr.Value) bool {
	v, err := value.ToTerraformValue(ctx)
	return err == nil && v.IsFullyKnown()
}

// Preserve heterogeneous objects, nested collections, nulls, and number precision.
func jsonValue(v tftypes.Value) (any, error) {
	if v.IsNull() {
		return nil, nil
	}
	switch {
	case v.Type().Is(tftypes.String):
		var s string
		err := v.As(&s)
		return s, err
	case v.Type().Is(tftypes.Bool):
		var b bool
		err := v.As(&b)
		return b, err
	case v.Type().Is(tftypes.Number):
		var n big.Float
		if err := v.As(&n); err != nil {
			return nil, err
		}
		return json.Number(n.Text('f', -1)), nil
	}
	switch v.Type().(type) {
	case tftypes.Object, tftypes.Map:
		var values map[string]tftypes.Value
		if err := v.As(&values); err != nil {
			return nil, err
		}
		result := make(map[string]any, len(values))
		for key, value := range values {
			converted, err := jsonValue(value)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	case tftypes.List, tftypes.Set, tftypes.Tuple:
		var values []tftypes.Value
		if err := v.As(&values); err != nil {
			return nil, err
		}
		result := make([]any, len(values))
		for i, value := range values {
			converted, err := jsonValue(value)
			if err != nil {
				return nil, err
			}
			result[i] = converted
		}
		return result, nil
	}
	return nil, fmt.Errorf("unsupported extra_data type")
}
