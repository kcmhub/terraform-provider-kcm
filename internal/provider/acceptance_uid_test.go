package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/kcmhub/terraform-provider-kcm/internal/client"
)

// TestAccUIDReferences references environments, clusters and connections by their uid (KCM 1.6.0), with the same
// variables as TestAccResources (KCM_ACC_KAFKA_BOOTSTRAP, KCM_ACC_KAFKA_USER, KCM_ACC_KAFKA_PASSWORD_FILE; a Team or
// Enterprise license):
//   - a cluster and its topic in the SAME apply: the topic takes kcm_kafka_cluster.main.uid, so that Terraform orders
//     the cluster first (a path by names would be known too early);
//   - a topic on a cluster that exists, through the kcm_kafka_cluster data source;
//   - the environment renamed, then the cluster renamed in KCM outside Terraform: the objects declared by uid are
//     not touched and the next plan is empty.
func TestAccUIDReferences(t *testing.T) {
	preCheck(t, "KCM_ACC_KAFKA_BOOTSTRAP", "KCM_ACC_KAFKA_USER", "KCM_ACC_KAFKA_PASSWORD_FILE")
	const source = "terraform:acc-uid"

	base := func(environmentName string) string {
		return fmt.Sprintf(`
provider "kcm" {
  iac_source    = %q
  allow_destroy = true
}

resource "kcm_group" "env" {
  name            = "tf-acc-uid-env"
  display_name    = %q
  color           = "#336699"
  deletion_policy = "Delete"
}

resource "kcm_kafka_cluster" "main" {
  name               = "tf-acc-uid-kafka"
  environment_uid    = kcm_group.env.uid
  bootstrap_servers  = %q
  security_protocol  = "SASL_PLAINTEXT"
  sasl_mechanism     = "PLAIN"
  sasl_username      = %q
  sasl_password_file = %q
  deletion_policy    = "Delete"
}

# the cluster and its topic in the same apply: the uid is only known once the cluster exists
resource "kcm_topic" "orders" {
  name               = "tf-acc-uid-orders"
  cluster_uid        = kcm_kafka_cluster.main.uid
  partitions         = 1
  replication_factor = 1
  deletion_policy    = "Delete"
}
`, source, environmentName, os.Getenv("KCM_ACC_KAFKA_BOOTSTRAP"), os.Getenv("KCM_ACC_KAFKA_USER"), os.Getenv("KCM_ACC_KAFKA_PASSWORD_FILE"))
	}
	withDataSources := func(environmentName string) string {
		return base(environmentName) + `
# an existing environment and cluster, by name and by uid
data "kcm_environment" "by_uid" {
  uid = kcm_group.env.uid
}

data "kcm_kafka_cluster" "by_uid" {
  uid = kcm_kafka_cluster.main.uid
}

data "kcm_kafka_cluster" "by_name" {
  name            = kcm_kafka_cluster.main.display_name
  environment_uid = data.kcm_environment.by_uid.uid
}

# a topic on a cluster that exists, through the data source
resource "kcm_topic" "audit" {
  name               = "tf-acc-uid-audit"
  cluster_uid        = data.kcm_kafka_cluster.by_uid.uid
  partitions         = 1
  replication_factor = 1
  deletion_policy    = "Delete"
}
`
	}
	const firstName = "TF acc uid env"
	const renamed = "TF acc uid env renamed"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Providers,
		Steps: []resource.TestStep{
			{
				Config: base(firstName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("kcm_group.env", "uid", uidPattern),
					resource.TestMatchResourceAttr("kcm_kafka_cluster.main", "uid", uidPattern),
					resource.TestCheckResourceAttrPair("kcm_kafka_cluster.main", "environment_uid", "kcm_group.env", "uid"),
					resource.TestCheckResourceAttrPair("kcm_topic.orders", "cluster_uid", "kcm_kafka_cluster.main", "uid"),
					resource.TestCheckResourceAttr("kcm_topic.orders", "topic_name", "tf-acc-uid-orders"),
				),
			},
			{
				// the data sources, and a topic on the existing cluster through them
				Config: withDataSources(firstName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.kcm_environment.by_uid", "name", "kcm_group.env", "display_name"),
					resource.TestCheckResourceAttrPair("data.kcm_kafka_cluster.by_uid", "uid", "kcm_kafka_cluster.main", "uid"),
					resource.TestCheckResourceAttrPair("data.kcm_kafka_cluster.by_name", "uid", "kcm_kafka_cluster.main", "uid"),
					resource.TestCheckResourceAttrPair("data.kcm_kafka_cluster.by_name", "environment_uid", "kcm_group.env", "uid"),
					resource.TestCheckResourceAttrPair("kcm_topic.audit", "cluster_uid", "kcm_kafka_cluster.main", "uid"),
				),
			},
			{
				// the environment is renamed: the cluster (by environment uid) and the topics (by cluster uid) stay as they are
				Config: withDataSources(renamed),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("kcm_group.env", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("kcm_kafka_cluster.main", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("kcm_topic.orders", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("kcm_topic.audit", plancheck.ResourceActionNoop),
				}},
				Check: resource.TestCheckResourceAttr("kcm_group.env", "display_name", renamed),
			},
			{
				// the cluster is renamed in KCM, outside Terraform: nothing is changed or replaced
				PreConfig: func() { renameClusterInKCM(t, source, "tf-acc-uid-env", "tf-acc-uid-kafka", "tf-acc-uid-kafka-renamed") },
				Config:    withDataSources(renamed),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("kcm_group.env", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("kcm_kafka_cluster.main", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("kcm_topic.orders", plancheck.ResourceActionNoop),
					plancheck.ExpectResourceAction("kcm_topic.audit", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kcm_kafka_cluster.main", "display_name", "tf-acc-uid-kafka-renamed"),
					resource.TestCheckResourceAttrPair("kcm_topic.orders", "cluster_uid", "kcm_kafka_cluster.main", "uid"),
					resource.TestCheckResourceAttrPair("data.kcm_kafka_cluster.by_name", "uid", "kcm_kafka_cluster.main", "uid"),
				),
			},
		},
	})
}

var uidPattern = regexpUID()

// renameClusterInKCM renames the cluster as a change made in KCM would (the declaration of its source with another name).
func renameClusterInKCM(t *testing.T, source, groupName, clusterName, newName string) {
	t.Helper()
	c := client.New(os.Getenv("KCM_ENDPOINT"), os.Getenv("KCM_TOKEN"), source)
	group, err := c.Get(context.Background(), "KcmGroup", groupName)
	if err != nil || group.Status == nil {
		t.Fatalf("reading the environment %s: %v", groupName, err)
	}
	// the declaration of the cluster as its source makes it (a declaration without the deletion policy or the secret
	// reference would change them), with another name
	doc := &client.Document{APIVersion: client.APIVersion, Kind: "KafkaCluster", Metadata: client.Metadata{Name: clusterName},
		Spec: map[string]interface{}{
			"environmentUid": group.Status.UID, "name": newName, "deletionPolicy": "Delete",
			"bootstrapServers": os.Getenv("KCM_ACC_KAFKA_BOOTSTRAP"), "securityProtocol": "SASL_PLAINTEXT", "saslMechanism": "PLAIN",
			"sasl": map[string]interface{}{"username": os.Getenv("KCM_ACC_KAFKA_USER"),
				"passwordFrom": map[string]interface{}{"file": os.Getenv("KCM_ACC_KAFKA_PASSWORD_FILE")}},
		}}
	plan, err := c.Put(context.Background(), doc, true, false)
	if err != nil || !plan.Applied {
		t.Fatalf("renaming the cluster in KCM: %v %v", err, plan)
	}
}
