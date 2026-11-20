package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kcmhub/terraform-provider-kcm/internal/client"
)

// environmentDataSource reads an existing environment (a KCM group) by name or by uid, to reference it by its uid.
type environmentDataSource struct {
	data *providerData
}

type environmentModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	UID   types.String `tfsdk:"uid"`
	Color types.String `tfsdk:"color"`
}

func newEnvironmentDataSource() datasource.DataSource {
	return &environmentDataSource{}
}

func (d *environmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (d *environmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An existing environment (KCM group), found by name or by uid. Its `uid` does not change when the " +
			"environment is renamed: reference it with `environment_uid` (`kcm_kafka_cluster`).",
		Attributes: map[string]schema.Attribute{
			"id":    schema.StringAttribute{Computed: true, Description: "The uid of the environment."},
			"name":  schema.StringAttribute{Optional: true, Computed: true, Description: "Name of the environment. Exactly one of `name` and `uid` is set."},
			"uid":   schema.StringAttribute{Optional: true, Computed: true, Description: "Uid of the environment. Exactly one of `name` and `uid` is set."},
			"color": schema.StringAttribute{Computed: true, Description: "Color of the environment."},
		},
	}
}

func (d *environmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	d.data = data
}

// findEnvironment returns the environment with this name or uid (exactly one of them is given).
func findEnvironment(environments []client.Environment, name, uid string) (*client.Environment, error) {
	if name != "" && uid != "" {
		return nil, fmt.Errorf("name and uid are both set: give one of them")
	}
	if name == "" && uid == "" {
		return nil, fmt.Errorf("name or uid is required")
	}
	for i := range environments {
		e := &environments[i]
		if (uid != "" && e.EnvironmentUID == uid) || (name != "" && e.Name == name) {
			return e, nil
		}
	}
	if uid != "" {
		return nil, fmt.Errorf("no environment with the uid %q (or the token cannot see it)", uid)
	}
	return nil, fmt.Errorf("no environment named %q (or the token cannot see it)", name)
}

func (d *environmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var name, uid types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("uid"), &uid)...)
	if resp.Diagnostics.HasError() {
		return
	}
	environments, err := d.data.client.Environments(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Listing the environments of KCM", err.Error())
		return
	}
	environment, err := findEnvironment(environments, name.ValueString(), uid.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading the environment from KCM", err.Error())
		return
	}
	if environment.EnvironmentUID == "" {
		resp.Diagnostics.AddError("Reading the environment from KCM",
			"KCM gave no uid for the environment "+strconv.FormatInt(environment.ID, 10)+": uids need KCM 1.6.0 or later.")
		return
	}
	model := environmentModel{
		ID:    types.StringValue(environment.EnvironmentUID),
		Name:  types.StringValue(environment.Name),
		UID:   types.StringValue(environment.EnvironmentUID),
		Color: types.StringValue(environment.Color),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
