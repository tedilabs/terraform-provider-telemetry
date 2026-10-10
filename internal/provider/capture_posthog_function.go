package provider

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/tedilabs/terraform-provider-telemetry/internal/telemetry"
)

var _ function.Function = &CapturePostHogFunction{}

type CapturePostHogFunction struct{ providerVersion string }

func NewCapturePostHogFunction(providerVersion string) function.Function {
	return &CapturePostHogFunction{providerVersion: providerVersion}
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
	// Skips and failures are logged at the debug level only, without the project token or event properties.
	resp.Result = function.NewResultData(types.BoolValue(true))
	var connection types.Object
	var options, extra types.Dynamic
	if req.Arguments.Get(ctx, &connection, &options, &extra) != nil {
		skipCapture(ctx, "the arguments cannot be read")
		return
	}
	if connection.IsNull() {
		skipCapture(ctx, "connection is null")
		return
	}
	if !fullyKnown(ctx, connection) {
		skipCapture(ctx, "connection contains unknown values")
		return
	}
	var conn telemetry.PostHogConnection
	if connection.As(ctx, &conn, basetypes.ObjectAsOptions{}).HasError() || !conn.Valid() {
		skipCapture(ctx, "connection has an invalid host or an empty project token")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	properties, opts, err := collectProperties(ctx, options, extra)
	if err != nil {
		skipCapture(ctx, err.Error())
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
		"$lib": "terraform-provider-telemetry", "$lib_version": f.providerVersion,
	}, properties)
	if ctx.Err() != nil {
		skipCapture(ctx, "the time limit was reached while collecting metadata")
		return
	}
	// Pseudonyms are scoped to the PostHog project. Deduplication and identity_keys use the pseudonyms.
	properties = telemetry.Pseudonymize(properties, opts.pseudonymizedKeys, conn.ProjectToken)
	if opts.deduplicationEnabled && !processDeduplicator.Allow(
		[]string{"capture_posthog", strings.TrimRight(conn.Host, "/"), conn.ProjectToken},
		properties, opts.deduplicationKeys,
	) {
		skipCapture(ctx, "an identical event was already sent by this provider process")
		return
	}
	distinctID := telemetry.IdentityID(properties, opts.identityKeys)
	if distinctID == "" && len(opts.identityKeys) > 0 {
		tflog.Debug(ctx, "Using a random distinct ID because a path in identity_keys is missing")
	}
	if err := telemetry.CapturePostHog(ctx, conn, distinctID, properties, f.providerVersion); err != nil {
		tflog.Debug(ctx, "Failed to send telemetry event", map[string]any{"error": err.Error()})
		return
	}
	tflog.Debug(ctx, "Sent telemetry event")
}

func skipCapture(ctx context.Context, reason string) {
	tflog.Debug(ctx, "Skipped telemetry capture", map[string]any{"reason": reason})
}
