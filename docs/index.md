---
page_title: "Provider: KCM"
description: |-
  Manages KCM (Kafka Cluster Manager) objects as code.
---

# KCM provider

Manages KCM objects as code through the KCM as code API (`/api/v1/iac`). It needs KCM 1.6.0 or later. Access (role
groups and role bindings in every edition, service accounts in the Team and Enterprise editions), and KCM groups, Kafka
clusters, their connections, topics and topic metadata in the Team and Enterprise editions.

## Example

```hcl
provider "kcm" {
  endpoint = "https://kcm.example.com"
  iac_source = "terraform:payments"
}

resource "kcm_topic" "orders" {
  name       = "prod-payments-orders"
  cluster    = "/groups/prod/clusters/payments"
  topic_name = "payments.orders"
  partitions = 6
  configs    = { "retention.ms" = "604800000" }
}
```

A cluster, a connection, a topic or topic metadata can name the object it belongs to by its stable `uid` instead of by
names (`environment_uid`, `cluster_uid`): the uid does not change when the object is renamed in KCM, and Terraform creates
a cluster before its topic. The `kcm_environment` and `kcm_kafka_cluster` data sources look up an existing object by name
or by uid. See the README.

## Schema

- `endpoint` (String) Base URL of KCM. Environment variable `KCM_ENDPOINT`.
- `token` (String, Sensitive) Token of a KCM service account. Environment variable `KCM_TOKEN`.
- `iac_source` (String) Source of the objects of this configuration: they are marked `iac:<source>` in KCM and read only
  elsewhere. One per configuration or workspace; default `terraform:default`. Environment variable `KCM_IAC_SOURCE`.
- `allow_destroy` (Boolean) Allow destroying resources with `deletion_policy = "Delete"`. Default false. Environment
  variable `KCM_ALLOW_DESTROY`.

## Behaviour

- Only the attributes set are managed; the others keep the value KCM has.
- Secrets are never in the configuration nor in the state: `*_file` attributes name files of the secrets directory of
  KCM (`KCM_IAC_SECRETS_DIR`).
  A new content in the same file is found by the next plan (a warning on refresh) and sent by the apply.
- A destroyed resource is kept in KCM and no longer managed, unless `deletion_policy = "Delete"` and
  `allow_destroy = true`.
- Import by name: `terraform import kcm_topic.orders <name>`; the first apply adopts an identical object.
- KCM authorizes each object: global ADMIN or SECURITY_ADMIN for access, ADMIN of the KCM group or Kafka cluster for
  clusters and connections, EDITOR of the cluster or the topic operation for topics.
