package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/kcmhub/terraform-provider-kcm/internal/client"
)

// configOf is the configuration of a resource built from attribute values (the others are null).
func configOf(t *testing.T, r *kindResource, values map[string]tftypes.Value) tfsdk.Config {
	plan := planOf(t, r, values)
	return tfsdk.Config{Schema: plan.Schema, Raw: plan.Raw}
}

func validate(t *testing.T, r *kindResource, values map[string]tftypes.Value) resource.ValidateConfigResponse {
	var resp resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: configOf(t, r, values)}, &resp)
	return resp
}

func str(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

func TestTheCreatedObjectsHaveAUidAndTheChildrenTakeOne(t *testing.T) {
	for _, typeName := range []string{"group", "kafka_cluster", "schema_registry", "kafka_connect", "ksql_endpoint"} {
		r := kindByType(t, typeName)
		var resp resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
		uid, ok := resp.Schema.Attributes["uid"]
		if !ok || !uid.IsComputed() || uid.IsOptional() || uid.IsRequired() {
			t.Errorf("%s: uid is a computed attribute only: %v", typeName, uid)
		}
	}
	for _, typeName := range []string{"topic", "topic_metadata", "role_group", "service_account", "role_binding"} {
		r := kindByType(t, typeName)
		var resp resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
		if _, ok := resp.Schema.Attributes["uid"]; ok {
			t.Errorf("%s has no uid of its own", typeName)
		}
	}
	for _, typeName := range []string{"topic", "topic_metadata", "schema_registry", "kafka_connect", "ksql_endpoint"} {
		r := kindByType(t, typeName)
		var resp resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
		for _, name := range []string{"cluster", "cluster_uid"} {
			attribute, ok := resp.Schema.Attributes[name]
			if !ok || !attribute.IsOptional() || attribute.IsRequired() || attribute.IsComputed() {
				t.Errorf("%s: %s is optional, not computed (exactly one of the two is set): %v", typeName, name, attribute)
			}
		}
	}
	cluster := kindByType(t, "kafka_cluster")
	var resp resource.SchemaResponse
	cluster.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	for _, name := range []string{"group", "environment_uid"} {
		attribute := resp.Schema.Attributes[name]
		if attribute == nil || !attribute.IsOptional() || attribute.IsComputed() {
			t.Errorf("kafka_cluster: %s is optional, not computed: %v", name, attribute)
		}
	}
}

func TestAnObjectIsReferencedByNamesOrByUidNotBoth(t *testing.T) {
	topic := kindByType(t, "topic")

	byNames := validate(t, topic, map[string]tftypes.Value{"name": str("orders"), "cluster": str("/groups/prod/clusters/payments")})
	byUID := validate(t, topic, map[string]tftypes.Value{"name": str("orders"), "cluster_uid": str("payments-d4e5f6")})
	if byNames.Diagnostics.HasError() || byUID.Diagnostics.HasError() {
		t.Fatalf("one reference is valid: %v %v", byNames.Diagnostics, byUID.Diagnostics)
	}

	both := validate(t, topic, map[string]tftypes.Value{"name": str("orders"), "cluster": str("/groups/prod/clusters/payments"),
		"cluster_uid": str("payments-d4e5f6")})
	if !both.Diagnostics.HasError() || !strings.Contains(both.Diagnostics.Errors()[0].Summary(), "Both cluster and cluster_uid") {
		t.Errorf("both references are refused: %v", both.Diagnostics)
	}

	neither := validate(t, topic, map[string]tftypes.Value{"name": str("orders")})
	if !neither.Diagnostics.HasError() || !strings.Contains(neither.Diagnostics.Errors()[0].Summary(), "Missing cluster or cluster_uid") {
		t.Errorf("a missing reference is refused: %v", neither.Diagnostics)
	}
}

func TestAReferenceKnownOnlyAfterTheApplyIsAccepted(t *testing.T) {
	// cluster_uid = kcm_kafka_cluster.main.uid is unknown while Terraform validates: no error
	topic := kindByType(t, "topic")
	resp := validate(t, topic, map[string]tftypes.Value{"name": str("orders"), "cluster_uid": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)})
	if resp.Diagnostics.HasError() {
		t.Fatalf("an unknown uid is waited for: %v", resp.Diagnostics)
	}
	both := validate(t, topic, map[string]tftypes.Value{"name": str("orders"), "cluster": str("/groups/a/clusters/b"),
		"cluster_uid": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)})
	if both.Diagnostics.HasError() {
		t.Fatalf("an unknown value is not judged: %v", both.Diagnostics)
	}
}

func TestAClusterIsInAnEnvironmentByNameOrByUid(t *testing.T) {
	cluster := kindByType(t, "kafka_cluster")
	if validate(t, cluster, map[string]tftypes.Value{"name": str("payments"), "group": str("prod")}).Diagnostics.HasError() {
		t.Error("group alone is valid")
	}
	if validate(t, cluster, map[string]tftypes.Value{"name": str("payments"), "environment_uid": str("prod-1a2b3c")}).Diagnostics.HasError() {
		t.Error("environment_uid alone is valid")
	}
	both := validate(t, cluster, map[string]tftypes.Value{"name": str("payments"), "group": str("prod"), "environment_uid": str("prod-1a2b3c")})
	if !both.Diagnostics.HasError() {
		t.Error("group and environment_uid together are refused")
	}
	if !validate(t, cluster, map[string]tftypes.Value{"name": str("payments")}).Diagnostics.HasError() {
		t.Error("an environment is required")
	}
}

func TestTheDocumentCarriesTheUidReferenceInItsSpec(t *testing.T) {
	topic := kindByType(t, "topic")
	plan := planOf(t, topic, map[string]tftypes.Value{"name": str("orders"), "cluster_uid": str("payments-d4e5f6"), "partitions": tftypes.NewValue(tftypes.Number, 3)})
	doc, diags := topic.document(context.Background(), plan)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if doc.Spec["clusterUid"] != "payments-d4e5f6" {
		t.Errorf("spec %v", doc.Spec)
	}
	if _, set := doc.Spec["cluster"]; set {
		t.Error("the other reference is left out")
	}

	cluster := kindByType(t, "kafka_cluster")
	doc, diags = cluster.document(context.Background(), planOf(t, cluster, map[string]tftypes.Value{"name": str("payments"), "environment_uid": str("prod-1a2b3c")}))
	if diags.HasError() {
		t.Fatal(diags)
	}
	if doc.Spec["environmentUid"] != "prod-1a2b3c" {
		t.Errorf("spec %v", doc.Spec)
	}
	if _, set := doc.Spec["group"]; set {
		t.Error("the other reference is left out")
	}
}

func TestAReferenceByUidIsNeverReadBackAndOneByNamesOnlyWithoutUid(t *testing.T) {
	topic := kindByType(t, "topic")
	ctx := context.Background()

	// declared by uid: what KCM gives back as a path (cluster) must not turn into a difference
	state := planOf(t, topic, map[string]tftypes.Value{"name": str("orders"), "cluster_uid": str("payments-d4e5f6")})
	attrs := map[string]bool{}
	for _, f := range readable(ctx, topic.fields, state) {
		attrs[f.attr] = true
	}
	if attrs["cluster"] || attrs["cluster_uid"] {
		t.Errorf("a topic declared by uid keeps its configured references: %v", attrs)
	}
	if !attrs["partitions"] || !attrs["topic_name"] {
		t.Errorf("the other attributes are read back: %v", attrs)
	}

	// declared by names, or imported (neither set): the path is read back
	for _, values := range []map[string]tftypes.Value{
		{"name": str("orders"), "cluster": str("/groups/prod/clusters/payments")},
		{"name": str("orders")},
	} {
		names := map[string]bool{}
		for _, f := range readable(ctx, topic.fields, planOf(t, topic, values)) {
			names[f.attr] = true
		}
		if !names["cluster"] || names["cluster_uid"] {
			t.Errorf("a topic by names reads its path back and never a uid: %v", names)
		}
	}
}

func TestTheUidIsTheOneKCMGivesInTheStatus(t *testing.T) {
	if uidOf(nil) != "" || uidOf(&client.Document{}) != "" {
		t.Error("no status, no uid (a KCM older than 1.6.0)")
	}
	if got := uidOf(&client.Document{Status: &client.Status{UID: "payments-d4e5f6", EnvironmentUID: "prod-1a2b3c"}}); got != "payments-d4e5f6" {
		t.Errorf("got %q", got)
	}
}

func TestAnEnvironmentIsFoundByNameOrByUid(t *testing.T) {
	environments := []client.Environment{
		{ID: 1, Name: "prod", EnvironmentUID: "prod-1a2b3c", Color: "#d32f2f"},
		{ID: 2, Name: "dev", EnvironmentUID: "dev-445566"},
	}
	byName, err := findEnvironment(environments, "dev", "")
	if err != nil || byName.ID != 2 {
		t.Fatalf("by name: %v %v", byName, err)
	}
	byUID, err := findEnvironment(environments, "", "prod-1a2b3c")
	if err != nil || byUID.Name != "prod" {
		t.Fatalf("by uid: %v %v", byUID, err)
	}
	// a rename does not matter to a lookup by uid
	environments[0].Name = "Production"
	renamed, err := findEnvironment(environments, "", "prod-1a2b3c")
	if err != nil || renamed.Name != "Production" {
		t.Fatalf("renamed: %v %v", renamed, err)
	}
	for _, c := range []struct{ name, uid, want string }{
		{"", "", "name or uid is required"},
		{"prod", "prod-1a2b3c", "both set"},
		{"nobody", "", "no environment named"},
		{"", "nobody-000000", "no environment with the uid"},
	} {
		if _, err := findEnvironment(environments, c.name, c.uid); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v", c, err)
		}
	}
}

func TestAClusterIsAskedForByUidOrByNameWithItsEnvironment(t *testing.T) {
	for _, c := range []struct {
		name, uid, environmentName, environmentUID string
		want                                       string
	}{
		{"", "payments-d4e5f6", "", "", ""},
		{"payments", "", "prod", "", ""},
		{"payments", "", "", "prod-1a2b3c", ""},
		{"", "", "", "", "uid, or name with"},
		{"payments", "", "", "", "needs its environment"},
		{"payments", "", "prod", "prod-1a2b3c", "both set"},
		{"payments", "payments-d4e5f6", "", "", "alone"},
		{"", "payments-d4e5f6", "prod", "", "alone"},
	} {
		err := validateClusterLookup(c.name, c.uid, c.environmentName, c.environmentUID)
		if c.want == "" && err != nil {
			t.Errorf("%+v: %v", c, err)
		}
		if c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%+v: got %v", c, err)
		}
	}
}

func TestTheClusterPathEncodesTheNames(t *testing.T) {
	if got := clusterPath("Prod EU", "payments/v2"); got != "/groups/Prod%20EU/clusters/payments%2Fv2" {
		t.Errorf("got %s", got)
	}
	if !sameScopePath(clusterPath("Prod EU", "payments"), "/groups/Prod EU/clusters/payments") {
		t.Error("the path names the same cluster as the decoded one")
	}
}
