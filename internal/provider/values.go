package provider

import (
	"context"
	"encoding/json"
	"errors"
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
	pseudonymizedKeys    []string
	identityKeys         []string
}

// Shared by capture functions for all telemetry destinations.
func optionsParameter() function.DynamicParameter {
	// A dynamic object preserves omitted optional attributes. ObjectParameter
	// requires every declared attribute and cannot express these defaults.
	return function.DynamicParameter{
		Name: "options", AllowNullValue: true, AllowUnknownValues: true,
		MarkdownDescription: "Object or map that selects metadata collectors and configures caching, deduplication, pseudonymization, and the event identity. All attributes are optional: the `machine`, `network`, `git`, `github_actions`, `hcp_terraform`, `terraform`, and `toolchain` collectors default to `true`, `github` defaults to `false`, `cache_enabled` and `deduplication_enabled` default to `true`, and `deduplication_keys`, `pseudonymized_keys`, and `identity_keys` default to `[]`. A null value or an unknown attribute, such as a misspelled collector, skips the capture.",
	}
}

func collectionOptions(options types.Dynamic) (captureOptions, error) {
	// Defaults for omitted attributes. GitHub requires an authenticated CLI and
	// identifies a person, so it is the only collector disabled by default.
	opts := captureOptions{
		collect: telemetry.Options{
			Machine: true, Network: true, Git: true, GitHub: false, GitHubActions: true, HCPTerraform: true, Terraform: true, Toolchain: true,
		},
		cacheEnabled: true, deduplicationEnabled: true,
	}
	// A typed null or unknown object has no attributes and must not fall back to the defaults.
	if options.IsUnderlyingValueNull() || options.IsUnderlyingValueUnknown() {
		return opts, errors.New("options is null or unknown")
	}
	var attributes map[string]attr.Value
	switch value := options.UnderlyingValue().(type) {
	case types.Object:
		attributes = value.Attributes()
	case types.Map:
		attributes = value.Elements()
	default:
		return opts, errors.New("options is not an object or a map")
	}
	flags := map[string]*bool{
		"machine": &opts.collect.Machine, "network": &opts.collect.Network, "git": &opts.collect.Git,
		"github": &opts.collect.GitHub, "github_actions": &opts.collect.GitHubActions, "hcp_terraform": &opts.collect.HCPTerraform,
		"terraform": &opts.collect.Terraform, "toolchain": &opts.collect.Toolchain,
		"cache_enabled": &opts.cacheEnabled, "deduplication_enabled": &opts.deduplicationEnabled,
	}
	paths := map[string]*[]string{
		"deduplication_keys": &opts.deduplicationKeys, "pseudonymized_keys": &opts.pseudonymizedKeys, "identity_keys": &opts.identityKeys,
	}
	for key, value := range attributes {
		if target, ok := paths[key]; ok {
			keys, err := propertyPaths(key, value)
			if err != nil {
				return opts, err
			}
			*target = keys
			continue
		}
		// Reject unknown attributes, so that a misspelled collector cannot stay enabled by default.
		target, known := flags[key]
		if !known {
			return opts, fmt.Errorf("options has an unknown attribute %q", key)
		}
		flag, ok := value.(types.Bool)
		if !ok || flag.IsNull() || flag.IsUnknown() {
			return opts, fmt.Errorf("options.%s is not a known boolean", key)
		}
		*target = flag.ValueBool()
	}
	return opts, nil
}

// propertyPaths parses an option that lists dot-separated property paths.
func propertyPaths(name string, value attr.Value) ([]string, error) {
	if value.IsNull() || value.IsUnknown() {
		return nil, fmt.Errorf("options.%s is null or unknown", name)
	}
	var elements []attr.Value
	switch list := value.(type) {
	case types.Tuple:
		elements = list.Elements()
	case types.List:
		elements = list.Elements()
	default:
		return nil, fmt.Errorf("options.%s is not a list", name)
	}
	var keys []string
	for _, element := range elements {
		key, ok := element.(types.String)
		if !ok || key.IsNull() || key.IsUnknown() {
			return nil, fmt.Errorf("options.%s has an element that is not a known string", name)
		}
		for _, part := range strings.Split(key.ValueString(), ".") {
			if strings.TrimSpace(part) == "" {
				return nil, fmt.Errorf("options.%s has a path with an empty segment: %q", name, key.ValueString())
			}
		}
		keys = append(keys, key.ValueString())
	}
	return keys, nil
}

func collectProperties(ctx context.Context, options types.Dynamic, extra types.Dynamic) (map[string]any, captureOptions, error) {
	if options.IsNull() {
		return nil, captureOptions{}, errors.New("options is null")
	}
	if !fullyKnown(ctx, options) || !fullyKnown(ctx, extra) {
		return nil, captureOptions{}, errors.New("options or extra_data contains unknown values")
	}
	opts, err := collectionOptions(options)
	if err != nil {
		return nil, captureOptions{}, err
	}
	extraData := map[string]any{}
	if !extra.IsNull() && !extra.IsUnderlyingValueNull() {
		v, err := extra.UnderlyingValue().ToTerraformValue(ctx)
		if err != nil {
			return nil, captureOptions{}, err
		}
		decoded, err := jsonValue(v)
		if err != nil {
			return nil, captureOptions{}, err
		}
		var ok bool
		extraData, ok = decoded.(map[string]any)
		if !ok {
			return nil, captureOptions{}, errors.New("extra_data is not an object or a map")
		}
	}
	properties := processCollector.Collect(ctx, opts.collect, opts.cacheEnabled)
	properties = mergeProperties(properties, extraData)
	return properties, opts, nil
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
