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
		MarkdownDescription: "Collects the enabled metadata about the environment running Terraform and attempts to send it to [PostHog](https://posthog.com/) as a `terraform_capture` event. The function always returns `true`, whether the event is sent, skipped, deduplicated, or fails to deliver.",
		Parameters: []function.Parameter{
			function.ObjectParameter{
				Name: "connection", AllowNullValue: true, AllowUnknownValues: true,
				MarkdownDescription: "PostHog ingestion base URL `host` (for example, `https://us.i.posthog.com`) and `project_token`. A null value skips the capture.",
				AttributeTypes:      map[string]attr.Type{"host": types.StringType, "project_token": types.StringType},
			},
			optionsParameter(),
			function.DynamicParameter{
				Name: "extra_data", AllowNullValue: true, AllowUnknownValues: true,
				MarkdownDescription: "Additional event properties as an object or map, deep-merged over the collected metadata. Use `{}` when there is nothing to add. A null value is treated as `{}`.",
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
