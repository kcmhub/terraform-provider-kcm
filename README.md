# Terraform provider for KCM

Manages [KCM (Kafka Cluster Manager)](https://kcmhub.io) objects as code with Terraform, through the KCM as code API
(`/api/v1/iac`): access (role groups, service accounts, role bindings), KCM groups, Kafka clusters and their
connections, topics and topic metadata.

## Installation

```hcl
terraform {
  required_providers {
    kcm = {
      source  = "kcmhub/kcm"
      version = "~> 0.1"
    }
  }
}

provider "kcm" {
  endpoint   = "https://kcm.example.com" # or KCM_ENDPOINT
  # token    = ...                       # KCM_TOKEN: a token of a KCM service account
  iac_source = "terraform:payments"      # or KCM_IAC_SOURCE
}
```

## Requirements

- Terraform 1.0 or later (plugin protocol 6).
- **KCM 1.6.0 or later.** The provider uses the single object API of KCM as code (`/api/v1/iac/objects`), which
  arrived in KCM 1.6.0. KCM 1.5.0 introduced access as code (role groups, service accounts, role bindings) but does
  not have this API, so the provider does not work with it, not even for access.
- Editions of KCM:

| Resource | KCM kind | KCM version | Edition |
|----------|----------|-------------|---------|
| `kcm_role_group` | RoleGroup | 1.6.0 (access as code: 1.5.0) | every edition |
| `kcm_role_binding` | RoleBinding | 1.6.0 (access as code: 1.5.0) | every edition |
| `kcm_service_account` | ServiceAccount | 1.6.0 (access as code: 1.5.0) | Team, Enterprise (the user management module) |
| `kcm_group` | KcmGroup | 1.6.0 | Team, Enterprise |
| `kcm_kafka_cluster` | KafkaCluster | 1.6.0 | Team, Enterprise |
| `kcm_schema_registry` | SchemaRegistry | 1.6.0 | Team, Enterprise |
| `kcm_kafka_connect` | KafkaConnect | 1.6.0 | Team, Enterprise |
| `kcm_ksql_endpoint` | KsqlEndpoint | 1.6.0 | Team, Enterprise |
| `kcm_topic` | Topic | 1.6.0 | Team, Enterprise |
| `kcm_topic_metadata` | TopicMetadata | 1.6.0 | Team, Enterprise (KCM_METADATA module) |

The data source `kcm_object` reads any object, managed or not.

The limits of your KCM edition apply to what Terraform declares as they do to what is created in the UI.

## Example

```hcl
resource "kcm_role_group" "payments" {
  name    = "payments-team"
  members = ["alice@corp.com", "bob@corp.com"]
}

resource "kcm_role_binding" "payments_editor" {
  name         = "payments-team-editor-prod"
  subject_kind = "RoleGroup"
  subject_name = kcm_role_group.payments.name
  role         = "editor"
  scope        = "/groups/prod/clusters/payments"
}

resource "kcm_kafka_cluster" "payments" {
  name               = "prod-payments"
  group              = "prod"
  display_name       = "payments"
  bootstrap_servers  = "broker-1:9093,broker-2:9093"
  security_protocol  = "SASL_SSL"
  sasl_mechanism     = "SCRAM-SHA-512"
  sasl_username      = "kcm"
  sasl_password_file = "payments-sasl-password" # a file of the secrets directory of KCM, never the value
}

resource "kcm_topic" "orders" {
  name               = "prod-payments-orders"
  cluster            = "/groups/prod/clusters/payments"
  topic_name         = "payments.orders"
  partitions         = 6
  replication_factor = 3
  configs            = { "retention.ms" = "604800000" }
  depends_on         = [kcm_kafka_cluster.payments]
}
```

More in [examples/provider/main.tf](examples/provider/main.tf) and [docs/index.md](docs/index.md).

## How it works

- Each resource is one document of KCM as code, applied with `PUT /api/v1/iac/objects/{kind}/{name}` under the
  provider `iac_source`. KCM marks it `iac:<source>`: it becomes read only in the KCM UI and API.
- KCM authorizes each object as its own routes do: the service account of the token needs the global ADMIN or
  SECURITY_ADMIN role for access, the ADMIN role on the KCM group or Kafka cluster for clusters and connections, the
  EDITOR role on the cluster (or the topic operation) for topics.
- Only the attributes set are managed; the others keep the value KCM has.
- Secrets are never in the configuration nor in the state: `*_file` attributes name files of the secrets directory of
  KCM (`KCM_IAC_SECRETS_DIR`, for example a mounted Kubernetes Secret). A secret left out keeps the value entered in
  KCM. A new content in the same file is found by the next plan (a warning on refresh) and sent by the apply.
- A topic is declared after its cluster exists: Terraform orders it when the cluster is a resource of the same
  configuration (use `depends_on`).
- `terraform destroy` follows `deletion_policy`: `Retain` (default) keeps the object in KCM and stops managing it;
  `Delete` deletes it, only when the provider has `allow_destroy = true` (a topic is deleted with its messages).
- Existing objects are imported by name: `terraform import kcm_topic.orders <name>` (the name of a managed object, or
  its export name, listed by `GET /api/v1/iac/export?kinds=Topic`). The first apply adopts an identical object without
  changing it.

## Development

Unit tests (no KCM needed):

```sh
go build ./...
go vet ./...
go test ./...
```

Acceptance tests run against a KCM that you start yourself. They create and remove their own objects (names starting
with `tf-acc`) and are skipped unless `TF_ACC=1` and the variables of the test are set:

```sh
export TF_ACC=1
export KCM_ENDPOINT=http://localhost:9090
export KCM_TOKEN=...                      # token of a global ADMIN or SECURITY_ADMIN service account
export KCM_ACC_SCOPE=/groups/prod         # an existing scope, for the access test

go test ./internal/provider -run 'TestAcc' -v
```

More variables enable more tests:

| Variable | Test |
|----------|------|
| `KCM_ACC_ENTERPRISE=1` | adds a service account to the access test (a KCM with service accounts) |
| `KCM_ACC_CLUSTER=/groups/{group}/clusters/{cluster}` | `TestAccTopic`: a topic on an existing cluster (Team or Enterprise) |
| `KCM_ACC_KAFKA_BOOTSTRAP`, `KCM_ACC_KAFKA_USER`, `KCM_ACC_KAFKA_PASSWORD_FILE` | `TestAccResources`: a group, a cluster (SASL_PLAINTEXT, PLAIN), a topic and its metadata; the password file is a file of the secrets directory of KCM; needs the KCM_METADATA module |
| `KCM_ACC_SCHEMA_REGISTRY_URL`, `KCM_ACC_KSQL_URL` | adds a Schema Registry and a ksqlDB endpoint to `TestAccResources` |
| `KCM_ACC_CONNECT_URL`, `KCM_ACC_CONNECT_PASSWORD_FILE` | adds a Kafka Connect cluster to `TestAccResources` |

## Releasing

Push a tag `vX.Y.Z`: the workflow `.github/workflows/release.yml` runs goreleaser, which builds the archives, signs the
`SHA256SUMS` file with GPG and attaches the registry manifest. It needs the repository secrets `GPG_PRIVATE_KEY` (the
armored private key) and `PASSPHRASE`; the public key is registered in the Terraform Registry.

## License

Apache License 2.0, see [LICENSE](LICENSE).
