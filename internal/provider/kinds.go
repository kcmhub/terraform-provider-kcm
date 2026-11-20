package provider

import "github.com/hashicorp/terraform-plugin-framework/resource"

// The kinds of KCM as code, as Terraform resources. Attributes are snake_case; their place in the spec of the
// document is in path.

func p(keys ...string) []string { return keys }

const deletionPolicyDoc = "`Retain` (default): when the resource is destroyed, the object stays in KCM and is no " +
	"longer managed. `Delete`: it is deleted, when the provider has `allow_destroy = true`."

func deletionPolicy() field {
	return field{attr: "deletion_policy", path: p("deletionPolicy"), kind: stringField, description: deletionPolicyDoc}
}

func clusterRef() field {
	return field{attr: "cluster", path: p("cluster"), kind: stringField, required: true, forceNew: true, scopePath: true,
		description: "Kafka cluster, as `/groups/{group}/clusters/{cluster}` (names percent-encoded)."}
}

var kinds = []*kindResource{
	{
		typeName: "role_group", kind: "RoleGroup",
		description: "A role group of KCM (access as code, every edition). `name` is the role group.",
		fields: []field{
			{attr: "description", path: p("description"), kind: stringField, description: "Description."},
			{attr: "enabled", path: p("enabled"), kind: boolField, description: "Whether the role group gives its grants."},
			{attr: "members", path: p("members"), kind: stringSetField, description: "Existing users, members of the role group (SSO memberships are not listed)."},
		},
	},
	{
		typeName: "service_account", kind: "ServiceAccount",
		description: "A service account of KCM (access as code). `name` is the service account; its tokens are created in KCM.",
		fields: []field{
			{attr: "description", path: p("description"), kind: stringField, description: "Description."},
			{attr: "enabled", path: p("enabled"), kind: boolField, description: "Disabled, its tokens stop working."},
		},
	},
	{
		typeName: "role_binding", kind: "RoleBinding",
		description: "A grant of KCM (access as code): a role on a scope, or operations on the resources matching a pattern.",
		fields: []field{
			{attr: "subject_kind", path: p("subject", "kind"), kind: stringField, required: true, description: "`User`, `ServiceAccount` or `RoleGroup`."},
			{attr: "subject_name", path: p("subject", "name"), kind: stringField, required: true, description: "Name of the subject."},
			{attr: "scope", path: p("scope"), kind: stringField, required: true, scopePath: true, description: "Scope, e.g. `/groups/prod/clusters/payments` or `/groups/prod/clusters/payments/topics/payments.`."},
			{attr: "role", path: p("role"), kind: stringField, description: "`viewer`, `editor` or `admin` (scopes of a KCM group, a cluster, a Connect cluster, a ksqlDB endpoint)."},
			{attr: "operations", path: p("operations"), kind: stringSetField, description: "Operations of a pattern grant, upper case (e.g. `READ`, `WRITE`, `DESCRIBE`)."},
			{attr: "pattern_type", path: p("patternType"), kind: stringField, description: "`LITERAL` (default) or `PREFIXED`."},
			{attr: "enabled", path: p("enabled"), kind: boolField, description: "Whether the grant applies."},
			{attr: "description", path: p("description"), kind: stringField, description: "Description."},
		},
	},
	{
		typeName: "group", kind: "KcmGroup",
		description: "A KCM group (Team and Enterprise editions).",
		fields: []field{
			{attr: "display_name", path: p("name"), kind: stringField, description: "Name of the group in KCM (default: `name`); renaming it renames the group."},
			{attr: "color", path: p("color"), kind: stringField, description: "Color of the group, e.g. `#d32f2f`."},
			deletionPolicy(),
		},
	},
	{
		typeName: "kafka_cluster", kind: "KafkaCluster",
		description: "A Kafka cluster of KCM (Team and Enterprise editions). Only the attributes set are managed; secrets " +
			"are names of files of the secrets directory of KCM (`KCM_IAC_SECRETS_DIR`), never values.",
		fields: []field{
			{attr: "group", path: p("group"), kind: stringField, required: true, forceNew: true, description: "KCM group of the cluster."},
			{attr: "display_name", path: p("name"), kind: stringField, description: "Name of the cluster in KCM (default: `name`)."},
			{attr: "bootstrap_servers", path: p("bootstrapServers"), kind: stringField, description: "Bootstrap servers."},
			{attr: "security_protocol", path: p("securityProtocol"), kind: stringField, description: "`PLAINTEXT`, `SSL`, `SASL_PLAINTEXT` or `SASL_SSL`."},
			{attr: "sasl_mechanism", path: p("saslMechanism"), kind: stringField, description: "e.g. `PLAIN`, `SCRAM-SHA-512`."},
			{attr: "sasl_username", path: p("sasl", "username"), kind: stringField, description: "SASL user name."},
			{attr: "sasl_password_file", path: p("sasl", "passwordFrom"), kind: secretFileField, description: "File of the secrets directory holding the SASL password."},
			{attr: "ssl_truststore_file", path: p("sslTruststoreFrom"), kind: secretFileField, description: "Truststore file of the secrets directory (a new content in the same file is applied by the next apply)."},
			{attr: "ssl_truststore_password_file", path: p("sslTruststorePasswordFrom"), kind: secretFileField, description: "File of the secrets directory holding the truststore password."},
			{attr: "kerberos_service_name", path: p("kerberosServiceName"), kind: stringField, description: "Kerberos service name."},
			{attr: "other_configs", path: p("otherConfigs"), kind: stringMapField, description: "Other client configurations, without secrets."},
			{attr: "metrics_enabled", path: p("metricsEnabled"), kind: boolField, description: "Whether KCM collects the metrics of the cluster."},
			{attr: "consumer_group_prefix", path: p("consumerGroupPrefix"), kind: stringField, description: "Prefix of the consumer groups of KCM."},
			deletionPolicy(),
		},
	},
	{
		typeName: "schema_registry", kind: "SchemaRegistry",
		description: "A Schema Registry of a Kafka cluster in KCM (Team and Enterprise editions).",
		fields: []field{
			clusterRef(),
			{attr: "display_name", path: p("name"), kind: stringField, description: "Name in KCM (default: `name`)."},
			{attr: "url", path: p("url"), kind: stringField, description: "URL of the registry."},
			{attr: "auth_username", path: p("auth", "username"), kind: stringField, description: "User name (basic authentication)."},
			{attr: "auth_password_file", path: p("auth", "passwordFrom"), kind: secretFileField, description: "File of the secrets directory holding the password."},
			{attr: "default_registry", path: p("defaultRegistry"), kind: boolField, description: "Default registry of the cluster."},
			{attr: "activated", path: p("activated"), kind: boolField, description: "Whether KCM uses the registry."},
			deletionPolicy(),
		},
	},
	{
		typeName: "kafka_connect", kind: "KafkaConnect",
		description: "A Kafka Connect cluster of a Kafka cluster in KCM (Team and Enterprise editions).",
		fields: []field{
			clusterRef(),
			{attr: "display_name", path: p("name"), kind: stringField, description: "Name in KCM (default: `name`)."},
			{attr: "url", path: p("url"), kind: stringField, description: "URL of the Connect REST API."},
			{attr: "basic_auth_username", path: p("basicAuth", "username"), kind: stringField, description: "User name (basic authentication)."},
			{attr: "basic_auth_password_file", path: p("basicAuth", "passwordFrom"), kind: secretFileField, description: "File of the secrets directory holding the password."},
			{attr: "config_topic", path: p("configTopic"), kind: stringField, description: "Configuration topic."},
			{attr: "status_topic", path: p("statusTopic"), kind: stringField, description: "Status topic."},
			{attr: "offset_topic", path: p("offsetTopic"), kind: stringField, description: "Offset topic."},
			{attr: "activated", path: p("activated"), kind: boolField, description: "Whether KCM uses the Connect cluster."},
			deletionPolicy(),
		},
	},
	{
		typeName: "ksql_endpoint", kind: "KsqlEndpoint",
		description: "A ksqlDB endpoint of a Kafka cluster in KCM (Team and Enterprise editions).",
		fields: []field{
			clusterRef(),
			{attr: "display_name", path: p("name"), kind: stringField, description: "Name in KCM (default: `name`)."},
			{attr: "base_url", path: p("baseUrl"), kind: stringField, description: "URL of the ksqlDB server."},
			{attr: "auth_type", path: p("authType"), kind: stringField, description: "`NONE`, `BASIC`, `BEARER` or `SSL`."},
			{attr: "basic_username", path: p("basic", "username"), kind: stringField, description: "User name (BASIC)."},
			{attr: "basic_password_file", path: p("basic", "passwordFrom"), kind: secretFileField, description: "File of the secrets directory holding the password (BASIC)."},
			{attr: "bearer_token_file", path: p("bearerTokenFrom"), kind: secretFileField, description: "File of the secrets directory holding the token (BEARER)."},
			{attr: "enabled", path: p("enabled"), kind: boolField, description: "Whether KCM uses the endpoint."},
			{attr: "allow_writes", path: p("allowWrites"), kind: boolField, description: "Whether statements that write are allowed."},
			{attr: "tls_verify_hostname", path: p("tlsVerifyHostname"), kind: boolField, description: "Whether the TLS host name is verified."},
			{attr: "connect_timeout_ms", path: p("connectTimeoutMs"), kind: intField, description: "Connection timeout."},
			{attr: "read_timeout_ms", path: p("readTimeoutMs"), kind: intField, description: "Read timeout."},
			deletionPolicy(),
		},
	},
	{
		typeName: "topic", kind: "Topic",
		description: "A Kafka topic managed through KCM (Team and Enterprise editions). Partitions can only grow; only the " +
			"configurations set are managed, one removed goes back to its default.",
		fields: []field{
			clusterRef(),
			{attr: "topic_name", path: p("name"), kind: stringField, forceNew: true, description: "Name of the topic (default: `name`)."},
			{attr: "partitions", path: p("partitions"), kind: intField, description: "Number of partitions (can only grow)."},
			{attr: "replication_factor", path: p("replicationFactor"), kind: intField, forceNew: true, description: "Replication factor, at creation."},
			{attr: "configs", path: p("configs"), kind: stringMapField, description: "Topic configurations, e.g. `retention.ms`."},
			deletionPolicy(),
		},
	},
	{
		typeName: "topic_metadata", kind: "TopicMetadata",
		description: "The governance metadata KCM keeps for a topic (Team and Enterprise editions, KCM_METADATA module): " +
			"description, tags, labels, classification, ownership, SLA, lifecycle, contract, producers, consumers. The " +
			"metadata is the whole `spec_json`: a section left out is cleared.",
		fields: []field{
			clusterRef(),
			{attr: "topic", path: p("topic"), kind: stringField, forceNew: true, description: "Name of the topic (default: `name`)."},
			{attr: "spec_json", path: p("spec"), kind: jsonField, required: true,
				description: "The metadata as a JSON object (`jsonencode({...})`), the fields of the TopicMetadata spec: " +
					"`description`, `tags`, `labels`, `classification` (required), `ownership`, `sla`, `lifecycle`, `contract`, `producers`, `consumers`."},
		},
	},
}

// resources returns a constructor per kind, as the provider wants them.
func resources() []func() resource.Resource {
	constructors := make([]func() resource.Resource, 0, len(kinds))
	for _, k := range kinds {
		definition := k
		constructors = append(constructors, func() resource.Resource {
			copied := *definition
			return &copied
		})
	}
	return constructors
}
