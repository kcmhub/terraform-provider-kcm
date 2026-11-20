package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kcmhub/terraform-provider-kcm/internal/client"
)

// clusterDataSource reads an existing Kafka cluster by uid, or by name within its environment, to reference it by its
// uid (`cluster_uid`) from topics, connections and topic metadata.
type clusterDataSource struct {
	data *providerData
}

type clusterModel struct {
	ID              types.String `tfsdk:"id"`
	UID             types.String `tfsdk:"uid"`
	Name            types.String `tfsdk:"name"`
	EnvironmentName types.String `tfsdk:"environment_name"`
	EnvironmentUID  types.String `tfsdk:"environment_uid"`
	BootstrapServer types.String `tfsdk:"bootstrap_servers"`
	Path            types.String `tfsdk:"path"`
}

func newClusterDataSource() datasource.DataSource {
	return &clusterDataSource{}
}

func (d *clusterDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kafka_cluster"
}

func (d *clusterDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An existing Kafka cluster of KCM, found by `uid`, or by `name` within its environment (`environment_name` or " +
			"`environment_uid`). Its `uid` does not change when the cluster or its environment is renamed: reference the cluster " +
			"with `cluster_uid` (topics, connections, topic metadata).",
		Attributes: map[string]schema.Attribute{
			"id":  schema.StringAttribute{Computed: true, Description: "The uid of the cluster."},
			"uid": schema.StringAttribute{Optional: true, Computed: true, Description: "Uid of the cluster. Set it, or `name` with the environment."},
			"name": schema.StringAttribute{Optional: true, Computed: true,
				Description: "Name of the cluster in its environment. Set it with `environment_name` or `environment_uid`, or set `uid`."},
			"environment_name": schema.StringAttribute{Optional: true, Computed: true, Description: "Name of the environment of the cluster."},
			"environment_uid":  schema.StringAttribute{Optional: true, Computed: true, Description: "Uid of the environment of the cluster."},
			"bootstrap_servers": schema.StringAttribute{Computed: true, Description: "Bootstrap servers of the cluster."},
			"path": schema.StringAttribute{Computed: true,
				Description: "The cluster as `/groups/{environment}/clusters/{cluster}` (names percent-encoded), for the `cluster` attribute of the objects that belong to it."},
		},
	}
}

func (d *clusterDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

// clusterPath is /groups/{environment}/clusters/{cluster} with the names percent-encoded.
func clusterPath(environment, cluster string) string {
	return "/groups/" + url.PathEscape(environment) + "/clusters/" + url.PathEscape(cluster)
}

// validateClusterLookup checks how the cluster is asked for: by uid, or by name within an environment.
func validateClusterLookup(name, uid, environmentName, environmentUID string) error {
	switch {
	case uid != "" && (name != "" || environmentName != "" || environmentUID != ""):
		return fmt.Errorf("uid is set with name or an environment: give the uid alone, or the name with its environment")
	case uid != "":
		return nil
	case name == "":
		return fmt.Errorf("uid, or name with environment_name or environment_uid, is required")
	case environmentName != "" && environmentUID != "":
		return fmt.Errorf("environment_name and environment_uid are both set: give one of them")
	case environmentName == "" && environmentUID == "":
		return fmt.Errorf("a name needs its environment: set environment_name or environment_uid")
	}
	return nil
}

func (d *clusterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var name, uid, environmentName, environmentUID types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("name"), &name)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("uid"), &uid)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("environment_name"), &environmentName)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("environment_uid"), &environmentUID)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateClusterLookup(name.ValueString(), uid.ValueString(), environmentName.ValueString(), environmentUID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Reading the Kafka cluster from KCM", err.Error())
		return
	}
	environments, err := d.data.client.Environments(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Listing the environments of KCM", err.Error())
		return
	}
	cluster, environment, err := d.find(ctx, environments, name.ValueString(), uid.ValueString(), environmentName.ValueString(), environmentUID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Reading the Kafka cluster from KCM", err.Error())
		return
	}
	if cluster.ClusterUID == "" {
		resp.Diagnostics.AddError("Reading the Kafka cluster from KCM", "KCM gave no uid for the cluster "+cluster.ClusterName)
		return
	}
	model := clusterModel{
		ID:              types.StringValue(cluster.ClusterUID),
		UID:             types.StringValue(cluster.ClusterUID),
		Name:            types.StringValue(cluster.ClusterName),
		EnvironmentName: types.StringValue(environment.Name),
		EnvironmentUID:  types.StringValue(environment.EnvironmentUID),
		BootstrapServer: types.StringValue(cluster.BootstrapServer),
		Path:            types.StringValue(clusterPath(environment.Name, cluster.ClusterName)),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// find looks for the cluster: by uid in every environment, or by name in the environment given.
func (d *clusterDataSource) find(ctx context.Context, environments []client.Environment, name, uid, environmentName, environmentUID string) (*client.Cluster, *client.Environment, error) {
	candidates := environments
	if uid == "" {
		environment, err := findEnvironment(environments, environmentName, environmentUID)
		if err != nil {
			return nil, nil, err
		}
		candidates = []client.Environment{*environment}
	}
	for i := range candidates {
		clusters, err := d.data.client.Clusters(ctx, candidates[i].ID)
		if err != nil {
			return nil, nil, fmt.Errorf("listing the Kafka clusters of the environment %q: %w", candidates[i].Name, err)
		}
		for j := range clusters {
			if (uid != "" && clusters[j].ClusterUID == uid) || (uid == "" && clusters[j].ClusterName == name) {
				return &clusters[j], &candidates[i], nil
			}
		}
	}
	if uid != "" {
		return nil, nil, fmt.Errorf("no Kafka cluster with the uid %q (or the token cannot see it)", uid)
	}
	return nil, nil, fmt.Errorf("no Kafka cluster named %q in the environment (or the token cannot see it)", name)
}
