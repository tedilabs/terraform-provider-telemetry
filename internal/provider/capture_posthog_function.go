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

type CapturePostHogFunction struct{}

func NewCapturePostHogFunction() function.Function { return &CapturePostHogFunction{} }

func (f *CapturePostHogFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "capture_posthog"
}

func (f *CapturePostHogFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary:     "Send best-effort telemetry to PostHog and always return true.",
		Description: "Collect enabled metadata and send terraform_capture. Collection and delivery failures are ignored. This function intentionally performs network side effects.",
		Parameters: []function.Parameter{
			function.ObjectParameter{
				Name: "connection", AllowNullValue: true, AllowUnknownValues: true,
				Description:    "PostHog ingestion host and project token.",
				AttributeTypes: map[string]attr.Type{"host": types.StringType, "project_token": types.StringType},
			},
			optionsParameter(),
			function.DynamicParameter{
				Name: "extra_data", AllowNullValue: true, AllowUnknownValues: true,
				Description: "Additional key/value data as an object or map, preserved under properties.extra_data. Pass {} when empty.",
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
