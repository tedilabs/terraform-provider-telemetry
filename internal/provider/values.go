package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

// Shared for the lifetime of this provider process, across function instances.
var processCollector = telemetry.NewCollector()
var processDeduplicator = &telemetry.Deduplicator{}

type captureOptions struct {
	collect              telemetry.Options
	cacheEnabled         bool
	deduplicationEnabled bool
	deduplicationKeys    []string
}

// Shared by capture functions for all telemetry destinations.
func optionsParameter() function.DynamicParameter {
	// A dynamic object preserves omitted optional attributes. ObjectParameter
	// requires every declared attribute and cannot express this default.
	return function.DynamicParameter{
		Name: "options", AllowNullValue: true, AllowUnknownValues: true,
		MarkdownDescription: "Object or map that enables metadata collectors and configures caching and deduplication. `machine`, `network`, `git`, `github`, and `github_actions` are required booleans; `terraform`, `toolchain`, `cache_enabled`, `deduplication_enabled`, and `deduplication_keys` are optional. A null value skips the capture.",
	}
}

func collectionOptions(options types.Dynamic) (captureOptions, bool) {
	opts := captureOptions{cacheEnabled: true, deduplicationEnabled: true}
	var attributes map[string]attr.Value
	switch value := options.UnderlyingValue().(type) {
	case types.Object:
		attributes = value.Attributes()
	case types.Map:
		attributes = value.Elements()
	default:
		return opts, false
	}
	for key, target := range map[string]*bool{
		"machine": &opts.collect.Machine, "network": &opts.collect.Network, "git": &opts.collect.Git,
		"github": &opts.collect.GitHub, "github_actions": &opts.collect.GitHubActions,
	} {
		value, ok := attributes[key].(types.Bool)
		if !ok || value.IsNull() || value.IsUnknown() {
			return opts, false
		}
		*target = value.ValueBool()
	}
	for key, target := range map[string]*bool{
		"terraform": &opts.collect.Terraform, "toolchain": &opts.collect.Toolchain,
		"cache_enabled": &opts.cacheEnabled, "deduplication_enabled": &opts.deduplicationEnabled,
	} {
		if value, exists := attributes[key]; exists {
			flag, ok := value.(types.Bool)
			if !ok || flag.IsNull() || flag.IsUnknown() {
				return opts, false
			}
			*target = flag.ValueBool()
		}
	}
	if value, exists := attributes["deduplication_keys"]; exists {
		if value.IsNull() || value.IsUnknown() {
			return opts, false
		}
		var elements []attr.Value
		switch list := value.(type) {
		case types.Tuple:
			elements = list.Elements()
		case types.List:
			elements = list.Elements()
		default:
			return opts, false
		}
		for _, element := range elements {
			key, ok := element.(types.String)
			if !ok || key.IsNull() || key.IsUnknown() {
				return opts, false
			}
			for _, part := range strings.Split(key.ValueString(), ".") {
				if strings.TrimSpace(part) == "" {
					return opts, false
				}
			}
			opts.deduplicationKeys = append(opts.deduplicationKeys, key.ValueString())
		}
	}
	return opts, true
}

func collectProperties(ctx context.Context, options types.Dynamic, extra types.Dynamic) (map[string]any, captureOptions, bool) {
	if options.IsNull() || !fullyKnown(ctx, options) || !fullyKnown(ctx, extra) {
		return nil, captureOptions{}, false
	}
	opts, ok := collectionOptions(options)
	if !ok {
		return nil, captureOptions{}, false
	}
	extraData := map[string]any{}
	if !extra.IsNull() && !extra.IsUnderlyingValueNull() {
		v, err := extra.UnderlyingValue().ToTerraformValue(ctx)
		if err != nil {
			return nil, captureOptions{}, false
		}
		decoded, err := jsonValue(v)
		if err != nil {
			return nil, captureOptions{}, false
		}
		var ok bool
		extraData, ok = decoded.(map[string]any)
		if !ok {
			return nil, captureOptions{}, false
		}
	}
	properties := processCollector.Collect(ctx, opts.collect, opts.cacheEnabled)
	properties = mergeProperties(properties, extraData)
	return properties, opts, true
}

// Objects merge recursively. Other values (including lists and null) are
// replaced by the caller's value. Neither input map is modified.
func mergeProperties(base, overrides map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(overrides))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range overrides {
		baseObject, baseOK := result[key].(map[string]any)
		overrideObject, overrideOK := value.(map[string]any)
		if baseOK && overrideOK {
			result[key] = mergeProperties(baseObject, overrideObject)
		} else {
			result[key] = value
		}
	}
	return result
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
