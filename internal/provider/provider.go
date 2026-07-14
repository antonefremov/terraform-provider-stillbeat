// Package provider implements the Stillbeat Terraform provider:
// checks-as-code over the Stillbeat JSON API, authenticated with a Stillbeat API key.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/antonefremov/terraform-provider-stillbeat/internal/client"
)

// Ensure the implementation satisfies the framework interface.
var _ provider.Provider = &stillbeatProvider{}

type stillbeatProvider struct {
	version string
}

// New returns the provider factory used by main and by acceptance tests.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &stillbeatProvider{version: version}
	}
}

func (p *stillbeatProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "stillbeat"
	resp.Version = p.version
}

// providerModel maps the provider configuration block.
type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIKey   types.String `tfsdk:"api_key"`
}

func (p *stillbeatProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [Stillbeat](https://stillbeat.app) cron/heartbeat checks as code.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "API base URL. Defaults to the production endpoint; override for staging/local. May also be set via `STILLBEAT_ENDPOINT`.",
			},
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Stillbeat API key (`dmf_...`), created in the dashboard under **API keys**. May also be set via `STILLBEAT_API_KEY` (preferred, keeps it out of state/config).",
			},
		},
	}
}

func (p *stillbeatProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Precedence: explicit config value, then environment variable, then
	// (for endpoint only) the built-in production default.
	endpoint := os.Getenv("STILLBEAT_ENDPOINT")
	if !cfg.Endpoint.IsNull() {
		endpoint = cfg.Endpoint.ValueString()
	}
	if endpoint == "" {
		endpoint = client.DefaultEndpoint
	}

	apiKey := os.Getenv("STILLBEAT_API_KEY")
	if !cfg.APIKey.IsNull() {
		apiKey = cfg.APIKey.ValueString()
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing API key",
			"Set the provider `api_key` argument or the STILLBEAT_API_KEY environment variable. Create a key in the dashboard under \"API keys\".",
		)
		return
	}

	c := client.New(endpoint, apiKey)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *stillbeatProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewCheckResource,
	}
}

func (p *stillbeatProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewCheckDataSource,
	}
}
