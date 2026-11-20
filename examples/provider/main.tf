terraform {
  required_providers {
    kcm = {
      source = "kcmhub/kcm"
    }
  }
}

# KCM_ENDPOINT and KCM_TOKEN (a service account token) can replace endpoint and token.
provider "kcm" {
  endpoint   = "https://kcm.example.com"
  iac_source = "terraform:payments" # objects marked iac:terraform:payments, read only elsewhere in KCM
}

# ---- Access (role groups and role bindings: every edition; service accounts: Team and Enterprise) ----

resource "kcm_role_group" "payments" {
  name        = "payments-team"
  description = "Payments squad"
  members     = ["alice@corp.com", "bob@corp.com"]
}

resource "kcm_service_account" "payments_ci" {
  name        = "payments-ci"
  description = "Deployment pipeline of the payments services"
}

resource "kcm_role_binding" "payments_editor" {
  name         = "payments-team-editor-prod"
  subject_kind = "RoleGroup"
  subject_name = kcm_role_group.payments.name
  role         = "editor"
  scope        = "/groups/prod/clusters/payments"
}

resource "kcm_role_binding" "payments_ci_topics" {
  name         = "payments-ci-topics"
  subject_kind = "ServiceAccount"
  subject_name = kcm_service_account.payments_ci.name
  operations   = ["DESCRIBE", "READ", "WRITE"]
  pattern_type = "PREFIXED"
  scope        = "/groups/prod/clusters/payments/topics/payments."
}

# ---- Resources (Team and Enterprise editions) ----

resource "kcm_kafka_cluster" "payments" {
  name               = "prod-payments"
  group              = "prod"
  display_name       = "payments"
  bootstrap_servers  = "broker-1:9093,broker-2:9093"
  security_protocol  = "SASL_SSL"
  sasl_mechanism     = "SCRAM-SHA-512"
  sasl_username      = "kcm"
  sasl_password_file = "payments-sasl-password" # a file of KCM_IAC_SECRETS_DIR on the KCM side, never the value
}

resource "kcm_topic" "orders" {
  name               = "prod-payments-orders"
  cluster            = "/groups/prod/clusters/payments"
  topic_name         = "payments.orders"
  partitions         = 6
  replication_factor = 3
  configs = {
    "retention.ms"   = "604800000"
    "cleanup.policy" = "delete"
  }
  depends_on = [kcm_kafka_cluster.payments]
}

resource "kcm_topic_metadata" "orders" {
  name    = "prod-payments-orders"
  cluster = kcm_topic.orders.cluster
  topic   = kcm_topic.orders.topic_name
  spec_json = jsonencode({
    description    = "Orders placed by the customers"
    tags           = ["orders", "pii"]
    classification = { dataSensitivity = "CONFIDENTIAL", containsPii = true }
    ownership      = { ownerTeam = "payments", ownerContact = "payments@corp.com" }
  })
}
