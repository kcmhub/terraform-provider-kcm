# Changelog

## 0.1.0 (November 22, 2026)

- Resources for the access of KCM: `kcm_role_group`, `kcm_service_account`, `kcm_role_binding`.
- Resources for KCM groups, Kafka clusters and their connections: `kcm_group`, `kcm_kafka_cluster`,
  `kcm_schema_registry`, `kcm_kafka_connect`, `kcm_ksql_endpoint`.
- Resources for topics: `kcm_topic`, `kcm_topic_metadata`.
- Data source `kcm_object`.
- Provider settings: `endpoint`, `token`, `iac_source`, `allow_destroy` (or KCM_ENDPOINT, KCM_TOKEN, KCM_IAC_SOURCE,
  KCM_ALLOW_DESTROY).
- `kcm_kafka_cluster`: `verify_broker_host_name` (checks the broker certificates against the broker host names) and
  `ssl_truststore_type` (`JKS`, `PKCS12` or `PEM`).
- Uids: the computed attribute `uid` of `kcm_group`, `kcm_kafka_cluster`, `kcm_schema_registry`, `kcm_kafka_connect` and
  `kcm_ksql_endpoint`; `environment_uid` (instead of `group`) on `kcm_kafka_cluster`, `cluster_uid` (instead of `cluster`)
  on connections, topics and topic metadata. A uid does not change when the object is renamed in KCM, and lets Terraform
  order a cluster before its topic in the same apply. Names and uid together, or neither, are refused.
- Data sources `kcm_environment` and `kcm_kafka_cluster`: an existing environment or cluster by name or by uid.
- Needs KCM 1.6.0 or later.
