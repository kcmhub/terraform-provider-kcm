package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Acceptance tests against a running KCM: TF_ACC=1, KCM_ENDPOINT, KCM_TOKEN (a token of a global ADMIN or
// SECURITY_ADMIN account) and KCM_ACC_SCOPE (an existing scope, e.g. /groups/prod). They use the access kinds,
// available in every edition. KCM_ACC_ENTERPRISE=1 (Enterprise license) adds a service account. With KCM_ACC_CLUSTER
// (/groups/{group}/clusters/{cluster}) and a Team or Enterprise license, they also manage a topic.

var protoV6Providers = map[string]func() (tfprotov6.ProviderServer, error){
	"kcm": providerserver.NewProtocol6WithError(New("test")()),
}

func preCheck(t *testing.T, variables ...string) {
	for _, v := range append([]string{"KCM_ENDPOINT", "KCM_TOKEN"}, variables...) {
		if os.Getenv(v) == "" {
			t.Skipf("%s is not set", v)
		}
	}
}

func TestAccAccessAsCode(t *testing.T) {
	preCheck(t, "KCM_ACC_SCOPE")
	scope := os.Getenv("KCM_ACC_SCOPE")
	serviceAccount := ""
	if os.Getenv("KCM_ACC_ENTERPRISE") == "1" {
		serviceAccount = `
resource "kcm_service_account" "ci" {
  name        = "tf-acc-ci"
  description = "Terraform acceptance test"
}
`
	}
	config := func(description string) string {
		return fmt.Sprintf(`
provider "kcm" {
  iac_source = "terraform:acc"
}

resource "kcm_role_group" "team" {
  name        = "tf-acc-team"
  description = %q
}

%s
resource "kcm_role_binding" "team_viewer" {
  name         = "tf-acc-team-viewer"
  subject_kind = "RoleGroup"
  subject_name = kcm_role_group.team.name
  role         = "viewer"
  scope        = %q
}
`, description, serviceAccount, scope)
	}
	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr("kcm_role_group.team", "managed_by", "iac:terraform:acc"),
		resource.TestCheckResourceAttr("kcm_role_group.team", "enabled", "true"),
		resource.TestCheckResourceAttr("kcm_role_binding.team_viewer", "role", "viewer"),
	}
	if serviceAccount != "" {
		checks = append(checks, resource.TestCheckResourceAttr("kcm_service_account.ci", "id", "ServiceAccount/tf-acc-ci"))
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Providers,
		Steps: []resource.TestStep{
			{
				Config: config("Declared with Terraform"),
				Check:  resource.ComposeAggregateTestCheckFunc(checks...),
			},
			{
				Config: config("Changed with Terraform"),
				Check:  resource.TestCheckResourceAttr("kcm_role_group.team", "description", "Changed with Terraform"),
			},
			{
				ResourceName:      "kcm_role_group.team",
				ImportState:       true,
				ImportStateId:     "tf-acc-team",
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccTopic(t *testing.T) {
	preCheck(t, "KCM_ACC_CLUSTER")
	cluster := os.Getenv("KCM_ACC_CLUSTER")
	config := func(partitions int, retention string) string {
		return fmt.Sprintf(`
provider "kcm" {
  iac_source    = "terraform:acc"
  allow_destroy = true
}

resource "kcm_topic" "orders" {
  name            = "tf-acc-orders"
  cluster         = %q
  partitions      = %d
  configs         = { "retention.ms" = %q }
  deletion_policy = "Delete"
}
`, cluster, partitions, retention)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Providers,
		Steps: []resource.TestStep{
			{
				Config: config(1, "3600000"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kcm_topic.orders", "topic_name", "tf-acc-orders"),
					resource.TestCheckResourceAttr("kcm_topic.orders", "configs.retention.ms", "3600000"),
				),
			},
			{
				Config: config(2, "7200000"),
				Check:  resource.TestCheckResourceAttr("kcm_topic.orders", "partitions", "2"),
			},
		},
	})
}

// TestAccResources declares a KCM group, a Kafka cluster whose SASL password is a file of the secrets directory of
// KCM, a Schema Registry and a ksqlDB endpoint when their URLs are set, a topic and its metadata. It needs a Team or
// Enterprise license with the KCM_METADATA module, KCM_ACC_KAFKA_BOOTSTRAP (a SASL_PLAINTEXT / PLAIN listener),
// KCM_ACC_KAFKA_USER and KCM_ACC_KAFKA_PASSWORD_FILE (the name of the password file in KCM_IAC_SECRETS_DIR); optional:
// KCM_ACC_SCHEMA_REGISTRY_URL, KCM_ACC_KSQL_URL, KCM_ACC_CONNECT_URL with KCM_ACC_CONNECT_PASSWORD_FILE.
func TestAccResources(t *testing.T) {
	preCheck(t, "KCM_ACC_KAFKA_BOOTSTRAP", "KCM_ACC_KAFKA_USER", "KCM_ACC_KAFKA_PASSWORD_FILE")
	connections := ""
	if url := os.Getenv("KCM_ACC_SCHEMA_REGISTRY_URL"); url != "" {
		connections += fmt.Sprintf(`
resource "kcm_schema_registry" "registry" {
  name             = "tf-acc-registry"
  cluster          = local.cluster
  url              = %q
  default_registry = true
  deletion_policy  = "Delete"
}
`, url)
	}
	if url := os.Getenv("KCM_ACC_KSQL_URL"); url != "" {
		connections += fmt.Sprintf(`
resource "kcm_ksql_endpoint" "ksql" {
  name            = "tf-acc-ksql"
  cluster         = local.cluster
  base_url        = %q
  auth_type       = "NONE"
  allow_writes    = false
  deletion_policy = "Delete"
}
`, url)
	}
	if url := os.Getenv("KCM_ACC_CONNECT_URL"); url != "" {
		// the password file is sent as basic authentication; a Connect cluster without authentication ignores it
		connections += fmt.Sprintf(`
resource "kcm_kafka_connect" "connect" {
  name                     = "tf-acc-connect"
  cluster                  = local.cluster
  url                      = %q
  basic_auth_username      = "kcm"
  basic_auth_password_file = %q
  config_topic             = "tf-acc-connect-configs"
  status_topic             = "tf-acc-connect-status"
  offset_topic             = "tf-acc-connect-offsets"
  deletion_policy          = "Delete"
}
`, url, os.Getenv("KCM_ACC_CONNECT_PASSWORD_FILE"))
	}
	config := func(partitions int, retention, description string) string {
		return fmt.Sprintf(`
provider "kcm" {
  iac_source    = "terraform:acc-resources"
  allow_destroy = true
}

resource "kcm_group" "team" {
  name            = "tf-acc-group"
  display_name    = "TF acc group"
  color           = "#336699"
  deletion_policy = "Delete"
}

resource "kcm_kafka_cluster" "kafka" {
  name               = "tf-acc-kafka"
  group              = kcm_group.team.display_name
  display_name       = "tf-acc-kafka"
  bootstrap_servers  = %q
  security_protocol  = "SASL_PLAINTEXT"
  sasl_mechanism     = "PLAIN"
  sasl_username      = %q
  sasl_password_file = %q
  ssl_truststore_type     = "PKCS12"
  verify_broker_host_name = %t
  deletion_policy    = "Delete"
}

locals {
  cluster = "/groups/${replace(kcm_group.team.display_name, " ", "%%20")}/clusters/${kcm_kafka_cluster.kafka.display_name}"
}
%s
resource "kcm_topic" "orders" {
  name               = "tf-acc-orders"
  cluster            = local.cluster
  partitions         = %d
  replication_factor = 1
  configs            = { "retention.ms" = %q }
  deletion_policy    = "Delete"
}

resource "kcm_topic_metadata" "orders" {
  name    = "tf-acc-orders"
  cluster = local.cluster
  topic   = kcm_topic.orders.topic_name
  spec_json = jsonencode({
    description    = %q
    tags           = ["orders", "acc"]
    classification = { dataSensitivity = "INTERNAL", containsPii = false }
    ownership      = { ownerTeam = "payments" }
  })
}
`, os.Getenv("KCM_ACC_KAFKA_BOOTSTRAP"), os.Getenv("KCM_ACC_KAFKA_USER"), os.Getenv("KCM_ACC_KAFKA_PASSWORD_FILE"), partitions == 1,
			connections, partitions, retention, description)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Providers,
		Steps: []resource.TestStep{
			{
				Config: config(1, "3600000", "Orders"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kcm_kafka_cluster.kafka", "managed_by", "iac:terraform:acc-resources"),
					resource.TestCheckResourceAttr("kcm_kafka_cluster.kafka", "sasl_password_file", os.Getenv("KCM_ACC_KAFKA_PASSWORD_FILE")),
					resource.TestCheckResourceAttr("kcm_kafka_cluster.kafka", "ssl_truststore_type", "PKCS12"),
					resource.TestCheckResourceAttr("kcm_kafka_cluster.kafka", "verify_broker_host_name", "true"),
					resource.TestCheckResourceAttr("kcm_topic.orders", "topic_name", "tf-acc-orders"),
					resource.TestCheckResourceAttr("kcm_topic.orders", "configs.retention.ms", "3600000"),
					resource.TestCheckResourceAttr("kcm_topic_metadata.orders", "id", "TopicMetadata/tf-acc-orders"),
					func(s *terraform.State) error {
						if os.Getenv("KCM_ACC_CONNECT_URL") == "" {
							return nil
						}
						return resource.TestCheckResourceAttr("kcm_kafka_connect.connect", "basic_auth_password_file",
							os.Getenv("KCM_ACC_CONNECT_PASSWORD_FILE"))(s)
					},
				),
			},
			{
				Config: config(2, "7200000", "Orders placed by the customers"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kcm_kafka_cluster.kafka", "verify_broker_host_name", "false"),
					resource.TestCheckResourceAttr("kcm_topic.orders", "partitions", "2"),
					resource.TestCheckResourceAttr("kcm_topic.orders", "configs.retention.ms", "7200000"),
				),
			},
			{
				// KCM gives the cluster path with its own encoding (a space, not %20): the same path, updated in place
				ResourceName:            "kcm_topic.orders",
				ImportState:             true,
				ImportStateId:           "tf-acc-orders",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"cluster"},
			},
		},
	})
}
