package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// objectDataSource reads any object of KCM as code, managed or not, as its spec in JSON.
type objectDataSource struct {
	data *providerData
}

type objectModel struct {
	Kind      types.String `tfsdk:"kind"`
	Name      types.String `tfsdk:"name"`
	ManagedBy types.String `tfsdk:"managed_by"`
	SpecJSON  types.String `tfsdk:"spec_json"`
}

func newObjectDataSource() datasource.DataSource {
	return &objectDataSource{}
}

func (d *objectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object"
}

func (d *objectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads one object of KCM as code, managed or not (an object not managed yet has its export name).",
		Attributes: map[string]schema.Attribute{
			"kind":       schema.StringAttribute{Required: true, Description: "Kind, e.g. `Topic`, `KafkaCluster`, `RoleBinding`."},
			"name":       schema.StringAttribute{Required: true, Description: "Name within the kind."},
			"managed_by": schema.StringAttribute{Computed: true, Description: "Source that manages it (`iac:<source>`), empty when not managed."},
			"spec_json":  schema.StringAttribute{Computed: true, Description: "Spec of the object, as JSON (secrets are references)."},
		},
	}
}

func (d *objectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *objectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model objectModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	doc, err := d.data.client.Get(ctx, model.Kind.ValueString(), model.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading "+model.Kind.ValueString()+" "+model.Name.ValueString()+" from KCM", err.Error())
		return
	}
	spec, _ := json.Marshal(doc.Spec)
	model.ManagedBy = types.StringValue(doc.Metadata.ManagedBy)
	model.SpecJSON = types.StringValue(string(spec))
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
