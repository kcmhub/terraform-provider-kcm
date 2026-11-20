// Package provider is the Terraform provider of KCM: the objects of KCM as code (/api/v1/iac) as resources.
package provider

import (
	"context"
	"os"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kcmhub/terraform-provider-kcm/internal/client"
)

// DefaultSource marks the objects of the provider in KCM (iac:terraform:default) when none is configured.
const DefaultSource = "terraform:default"

type kcmProvider struct {
	version string
}

// providerData is handed to the resources.
type providerData struct {
	client       *client.Client
	allowDestroy bool
}

type providerModel struct {
	Endpoint     types.String `tfsdk:"endpoint"`
	Token        types.String `tfsdk:"token"`
	Source       types.String `tfsdk:"iac_source"`
	AllowDestroy types.Bool   `tfsdk:"allow_destroy"`
}

var _ provider.Provider = (*kcmProvider)(nil)

// New returns the provider of a version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &kcmProvider{version: version}
	}
}

func (p *kcmProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "kcm"
	resp.Version = p.version
}

func (p *kcmProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages KCM (Kafka Cluster Manager) objects as code: access (role groups, service accounts, " +
			"role bindings), KCM groups, Kafka clusters and their connections, topics and topic metadata.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of KCM, e.g. `https://kcm.example.com`. Environment variable `KCM_ENDPOINT`.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "Token of a KCM service account (created in KCM; never stored by KCM as code). Environment " +
					"variable `KCM_TOKEN`.",
			},
			"iac_source": schema.StringAttribute{
				Optional: true,
				Description: "Source of the objects of this configuration in KCM: they are marked `iac:<source>` and read " +
					"only elsewhere. One per configuration or workspace (default `" + DefaultSource + "`). Environment variable `KCM_IAC_SOURCE`.",
			},
			"allow_destroy": schema.BoolAttribute{
				Optional: true,
				Description: "Allow the destructive changes: a destroyed resource with `deletion_policy = \"Delete\"` is " +
					"deleted in KCM (a topic with its messages). Default false: the destroy is refused. Environment variable `KCM_ALLOW_DESTROY`.",
			},
		},
	}
}

func (p *kcmProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := valueOr(config.Endpoint, os.Getenv("KCM_ENDPOINT"))
	token := valueOr(config.Token, os.Getenv("KCM_TOKEN"))
	source := valueOr(config.Source, os.Getenv("KCM_IAC_SOURCE"))
	if source == "" {
		source = DefaultSource
	}
	allowDestroy := false
	if !config.AllowDestroy.IsNull() && !config.AllowDestroy.IsUnknown() {
		allowDestroy = config.AllowDestroy.ValueBool()
	} else if env, err := strconv.ParseBool(os.Getenv("KCM_ALLOW_DESTROY")); err == nil {
		allowDestroy = env
	}
	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Missing KCM endpoint",
			"Set endpoint in the provider configuration or the KCM_ENDPOINT environment variable.")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing KCM token",
			"Set token in the provider configuration or the KCM_TOKEN environment variable (a service account token).")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	c := client.New(endpoint, token, source)
	c.UserAgent = "terraform-provider-kcm/" + p.version
	data := &providerData{client: c, allowDestroy: allowDestroy}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func valueOr(value types.String, fallback string) string {
	if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return fallback
	}
	return value.ValueString()
}

func (p *kcmProvider) Resources(_ context.Context) []func() resource.Resource {
	return resources()
}

func (p *kcmProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{newObjectDataSource, newEnvironmentDataSource, newClusterDataSource}
}
