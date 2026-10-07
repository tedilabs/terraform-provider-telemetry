package provider

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

var _ function.Function = &CapturePostHogFunction{}

type CapturePostHogFunction struct{ providerVersion string }

func NewCapturePostHogFunction() function.Function {
	return &CapturePostHogFunction{providerVersion: "dev"}
}

func (f *CapturePostHogFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "capture_posthog"
}

func (f *CapturePostHogFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary:             "Send best-effort telemetry to PostHog and always return true.",
		MarkdownDescription: "Attempts to send a non-duplicate `terraform_capture` event to PostHog. Every function invocation returns `true`; this does not indicate delivery success. Collection and delivery failures are ignored. The function deliberately performs network side effects and does not guarantee exactly-once execution.",
		Parameters: []function.Parameter{
			function.ObjectParameter{
				Name: "connection", AllowNullValue: true, AllowUnknownValues: true,
				MarkdownDescription: "Object with `host` and `project_token` string attributes. Use a PostHog ingestion base URL such as `https://us.i.posthog.com`; the function appends `/i/v0/e/`. Supply the project token, not a personal API key. Connection configuration is supplied here, not in a provider block.",
				AttributeTypes:      map[string]attr.Type{"host": types.StringType, "project_token": types.StringType},
			},
			optionsParameter(),
			function.DynamicParameter{
				Name: "extra_data", AllowNullValue: true, AllowUnknownValues: true,
				MarkdownDescription: "Additional key/value data as an object or map, deep-merged directly into event properties with caller values taking precedence. There is no automatic `extra_data` wrapper. Objects merge recursively; lists, scalars, and null replace existing values. Pass `{}` when empty.",
			},
		},
		Return: function.BoolReturn{},
	}
}

func (f *CapturePostHogFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	// Set a deterministic result before any optional work. Never expose delivery errors.
	resp.Result = function.NewResultData(types.BoolValue(true))
	var connection types.Object
	var options, extra types.Dynamic
	if req.Arguments.Get(ctx, &connection, &options, &extra) != nil || connection.IsNull() || !fullyKnown(ctx, connection) {
		return
	}
	var conn telemetry.PostHogConnection
	if connection.As(ctx, &conn, basetypes.ObjectAsOptions{}).HasError() || !conn.Valid() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	properties, opts, ok := collectProperties(ctx, options, extra)
	if !ok {
		return
	}
	if opts.collect.Toolchain {
		properties = mergeProperties(map[string]any{
			"toolchain": map[string]any{"telemetry_provider": f.providerVersion},
		}, properties)
	}
	// Deduplicate the final properties, including overridable PostHog defaults.
	properties = mergeProperties(map[string]any{
		"$process_person_profile": false, "$geoip_disable": true,
	}, properties)
	if ctx.Err() != nil {
		return
	}
	if opts.deduplicationEnabled && !processDeduplicator.Allow(
		[]string{"capture_posthog", strings.TrimRight(conn.Host, "/"), conn.ProjectToken},
		properties, opts.deduplicationKeys,
	) {
		return
	}
	telemetry.CapturePostHog(ctx, conn, properties)
}
