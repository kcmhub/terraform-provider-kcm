# Changelog

## 0.1.0 (unreleased)

- Resources for the access of KCM: `kcm_role_group`, `kcm_service_account`, `kcm_role_binding`.
- Resources for KCM groups, Kafka clusters and their connections: `kcm_group`, `kcm_kafka_cluster`,
  `kcm_schema_registry`, `kcm_kafka_connect`, `kcm_ksql_endpoint`.
- Resources for topics: `kcm_topic`, `kcm_topic_metadata`.
- Data source `kcm_object`.
- Provider settings: `endpoint`, `token`, `iac_source`, `allow_destroy` (or KCM_ENDPOINT, KCM_TOKEN, KCM_IAC_SOURCE,
  KCM_ALLOW_DESTROY).
- Needs KCM 1.6.0 or later.
