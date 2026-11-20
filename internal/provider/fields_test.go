package provider

import (
	"github.com/kcmhub/terraform-provider-kcm/internal/client"

	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func kindByType(t *testing.T, typeName string) *kindResource {
	for _, k := range kinds {
		if k.typeName == typeName {
			copied := *k
			return &copied
		}
	}
	t.Fatalf("no kind %s", typeName)
	return nil
}

func TestPathsOfTheSpec(t *testing.T) {
	spec := map[string]interface{}{}
	setPath(spec, []string{"sasl", "passwordFrom"}, map[string]interface{}{"file": "x"})
	setPath(spec, []string{"sasl", "username"}, "kcm")
	value, ok := getPath(spec, []string{"sasl", "username"})
	if !ok || value != "kcm" {
		t.Errorf("got %v", value)
	}
	if _, ok := getPath(spec, []string{"sasl", "missing"}); ok {
		t.Error("missing path found")
	}
}

func TestJSONOfLeavesTheOtherFieldsOut(t *testing.T) {
	metadata := kindByType(t, "topic_metadata")
	got := jsonOf(metadata.fields, map[string]interface{}{
		"cluster": "/groups/prod/clusters/payments", "topic": "orders",
		"tags": []interface{}{"pii"}, "description": "Orders",
	})
	if got != `{"description":"Orders","tags":["pii"]}` {
		t.Errorf("got %s", got)
	}
}

func TestEveryKindHasAValidSchema(t *testing.T) {
	ctx := context.Background()
	seen := map[string]bool{}
	for _, constructor := range resources() {
		r := constructor()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "kcm"}, &meta)
		if seen[meta.TypeName] {
			t.Errorf("duplicate resource %s", meta.TypeName)
		}
		seen[meta.TypeName] = true
		var resp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s: %v", meta.TypeName, resp.Diagnostics)
		}
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: %v", meta.TypeName, diags)
		}
	}
	for _, expected := range []string{"kcm_role_group", "kcm_service_account", "kcm_role_binding", "kcm_group", "kcm_kafka_cluster",
		"kcm_schema_registry", "kcm_kafka_connect", "kcm_ksql_endpoint", "kcm_topic", "kcm_topic_metadata"} {
		if !seen[expected] {
			t.Errorf("missing resource %s", expected)
		}
	}
	var p provider.SchemaResponse
	New("test")().Schema(ctx, provider.SchemaRequest{}, &p)
	if p.Diagnostics.HasError() {
		t.Error(p.Diagnostics)
	}
}

// plan of a resource built from attribute values.
func planOf(t *testing.T, r *kindResource, values map[string]tftypes.Value) tfsdk.Plan {
	ctx := context.Background()
	var resp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &resp)
	objectType := resp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	all := map[string]tftypes.Value{}
	for name, attrType := range objectType.AttributeTypes {
		if v, ok := values[name]; ok {
			all[name] = v
		} else {
			all[name] = tftypes.NewValue(attrType, nil)
		}
	}
	return tfsdk.Plan{Schema: resp.Schema, Raw: tftypes.NewValue(objectType, all)}
}

func TestADocumentHoldsTheSetAttributesAndSecretReferences(t *testing.T) {
	cluster := kindByType(t, "kafka_cluster")
	plan := planOf(t, cluster, map[string]tftypes.Value{
		"name":               tftypes.NewValue(tftypes.String, "prod-payments"),
		"group":              tftypes.NewValue(tftypes.String, "prod"),
		"bootstrap_servers":  tftypes.NewValue(tftypes.String, "b1:9093"),
		"sasl_username":      tftypes.NewValue(tftypes.String, "kcm"),
		"sasl_password_file": tftypes.NewValue(tftypes.String, "payments-sasl"),
		"metrics_enabled":    tftypes.NewValue(tftypes.Bool, true),
		"other_configs": tftypes.NewValue(tftypes.Map{ElementType: tftypes.String},
			map[string]tftypes.Value{"client.dns.lookup": tftypes.NewValue(tftypes.String, "use_all_dns_ips")}),
		"consumer_group_prefix": tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
	})

	doc, diags := cluster.document(context.Background(), plan)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if doc.Kind != "KafkaCluster" || doc.Metadata.Name != "prod-payments" {
		t.Errorf("document %+v", doc)
	}
	sasl := doc.Spec["sasl"].(map[string]interface{})
	if sasl["username"] != "kcm" || sasl["passwordFrom"].(map[string]interface{})["file"] != "payments-sasl" {
		t.Errorf("sasl %v", sasl)
	}
	if doc.Spec["metricsEnabled"] != true || doc.Spec["otherConfigs"].(map[string]string)["client.dns.lookup"] != "use_all_dns_ips" {
		t.Errorf("spec %v", doc.Spec)
	}
	if _, set := doc.Spec["consumerGroupPrefix"]; set {
		t.Error("an unknown attribute is left out")
	}
	if _, set := doc.Spec["securityProtocol"]; set {
		t.Error("a null attribute is left out")
	}
}

func TestTopicMetadataMergesItsJSON(t *testing.T) {
	metadata := kindByType(t, "topic_metadata")
	plan := planOf(t, metadata, map[string]tftypes.Value{
		"name":      tftypes.NewValue(tftypes.String, "orders"),
		"cluster":   tftypes.NewValue(tftypes.String, "/groups/prod/clusters/payments"),
		"spec_json": tftypes.NewValue(tftypes.String, `{"description":"Orders","classification":{"dataSensitivity":"INTERNAL"}}`),
	})
	doc, diags := metadata.document(context.Background(), plan)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if doc.Spec["description"] != "Orders" || doc.Spec["cluster"] != "/groups/prod/clusters/payments" {
		t.Errorf("spec %v", doc.Spec)
	}
	if _, nested := doc.Spec["spec"]; nested {
		t.Error("the JSON is merged, not nested")
	}
}

func TestScopePathsAreComparedDecoded(t *testing.T) {
	if !sameScopePath("/groups/tf%20acc", "/groups/tf acc") || !sameScopePath("/groups/a", "/groups/a") {
		t.Fatal("percent-encoded and plain names are the same scope")
	}
	if sameScopePath("/groups/a", "/groups/b") || sameScopePath("/groups/a+b", "/groups/a b") {
		t.Fatal("different names are different scopes, a plus sign stays a plus sign")
	}
}

func TestASecretFileWhoseContentChangedIsFound(t *testing.T) {
	password := field{attr: "basic_auth_password_file", path: []string{"basicAuth", "passwordFrom"}, kind: secretFileField}
	token := field{attr: "bearer_token_file", path: []string{"bearerTokenFrom"}, kind: secretFileField}
	if secretDiffField(password) != "basicAuth.password" || secretDiffField(token) != "bearerToken" {
		t.Fatalf("diff fields: %s, %s", secretDiffField(password), secretDiffField(token))
	}
	plan := &client.Plan{Changes: []client.Change{
		{Action: "UPDATE", Kind: "KafkaConnect", Name: "c", Diff: []client.FieldChange{{Field: "basicAuth.password"}}},
		{Action: "UPDATE", Kind: "KafkaConnect", Name: "other", Diff: []client.FieldChange{{Field: "bearerToken"}}},
	}}
	changed := changedSecretFiles(plan, "KafkaConnect", "c", []field{password, token})
	if len(changed) != 1 || changed[0].attr != "basic_auth_password_file" {
		t.Fatalf("changed: %v", changed)
	}
}
