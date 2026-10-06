package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ provider.ProviderWithFunctions = &TelemetryProvider{}

type TelemetryProvider struct{ version string }

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &TelemetryProvider{version: version} }
}

func (p *TelemetryProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "telemetry"
	resp.Version = p.version
}

func (p *TelemetryProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Best-effort telemetry functions. No resources or data sources are managed."}
}

func (p *TelemetryProvider) Configure(context.Context, provider.ConfigureRequest, *provider.ConfigureResponse) {
}

func (p *TelemetryProvider) Resources(context.Context) []func() resource.Resource { return nil }
func (p *TelemetryProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
func (p *TelemetryProvider) Functions(context.Context) []func() function.Function {
	return []func() function.Function{NewCapturePostHogFunction}
}
