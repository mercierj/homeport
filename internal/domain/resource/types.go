// Package resource defines core domain types and entities for AWS resources.
package resource

// Type represents the type of AWS resource being processed.
// These constants map to Terraform resource types for consistency.
type Type string

const (
	// ─────────────────────────────────────────────────────
	// AWS Resource Types
	// ─────────────────────────────────────────────────────

	// AWS Compute
	TypeEC2Instance    Type = "aws_instance"
	TypeLambdaFunction Type = "aws_lambda_function"
	TypeECSService     Type = "aws_ecs_service"
	TypeECSTaskDef     Type = "aws_ecs_task_definition"
	TypeEKSCluster     Type = "aws_eks_cluster"

	// AWS Storage
	TypeS3Bucket  Type = "aws_s3_bucket"
	TypeEBSVolume Type = "aws_ebs_volume"
	TypeEFSVolume Type = "aws_efs_file_system"

	// AWS Database
	TypeRDSInstance   Type = "aws_db_instance"
	TypeRDSCluster    Type = "aws_rds_cluster"
	TypeDynamoDBTable Type = "aws_dynamodb_table"
	TypeElastiCache   Type = "aws_elasticache_cluster"

	// AWS Networking
	TypeALB         Type = "aws_lb"
	TypeAPIGateway  Type = "aws_api_gateway_rest_api"
	TypeRoute53Zone Type = "aws_route53_zone"
	TypeCloudFront  Type = "aws_cloudfront_distribution"
	TypeVPC         Type = "aws_vpc"

	// AWS Security
	TypeCognitoPool    Type = "aws_cognito_user_pool"
	TypeSecretsManager Type = "aws_secretsmanager_secret"
	TypeIAMRole        Type = "aws_iam_role"
	TypeACMCertificate Type = "aws_acm_certificate"

	// AWS Messaging
	TypeSQSQueue    Type = "aws_sqs_queue"
	TypeSNSTopic    Type = "aws_sns_topic"
	TypeEventBridge Type = "aws_cloudwatch_event_rule"
	TypeKinesis     Type = "aws_kinesis_stream"
	TypeSESIdentity Type = "aws_ses_domain_identity"

	// AWS Security (additional)
	TypeKMSKey Type = "aws_kms_key"

	// AWS Monitoring
	TypeCloudWatchMetricAlarm Type = "aws_cloudwatch_metric_alarm"
	TypeCloudWatchLogGroup    Type = "aws_cloudwatch_log_group"
	TypeCloudWatchDashboard   Type = "aws_cloudwatch_dashboard"

	// ─────────────────────────────────────────────────────
	// GCP Resource Types
	// ─────────────────────────────────────────────────────

	// GCP Compute
	TypeGCEInstance   Type = "google_compute_instance"
	TypeCloudRun      Type = "google_cloud_run_service"
	TypeCloudFunction Type = "google_cloudfunctions_function"
	TypeGKE           Type = "google_container_cluster"
	TypeAppEngine     Type = "google_app_engine_application"

	// GCP Storage
	TypeGCSBucket      Type = "google_storage_bucket"
	TypePersistentDisk Type = "google_compute_disk"
	TypeFilestore      Type = "google_filestore_instance"

	// GCP Database
	TypeCloudSQL    Type = "google_sql_database_instance"
	TypeFirestore   Type = "google_firestore_database"
	TypeBigtable    Type = "google_bigtable_instance"
	TypeMemorystore Type = "google_redis_instance"
	TypeSpanner     Type = "google_spanner_instance"

	// GCP Networking
	TypeCloudLB        Type = "google_compute_backend_service"
	TypeCloudDNS       Type = "google_dns_managed_zone"
	TypeCloudCDN       Type = "google_compute_backend_bucket"
	TypeCloudArmor     Type = "google_compute_security_policy"
	TypeGCPVPCNetwork  Type = "google_compute_network"

	// GCP Security
	TypeIdentityPlatform Type = "google_identity_platform_config"
	TypeSecretManager    Type = "google_secret_manager_secret"
	TypeGCPIAM           Type = "google_project_iam_member"

	// GCP Messaging
	TypePubSubTopic        Type = "google_pubsub_topic"
	TypePubSubSubscription Type = "google_pubsub_subscription"
	TypeCloudTasks         Type = "google_cloud_tasks_queue"
	TypeCloudScheduler     Type = "google_cloud_scheduler_job"

	// ─────────────────────────────────────────────────────
	// Azure Resource Types
	// ─────────────────────────────────────────────────────

	// Azure Compute
	TypeAzureVM           Type = "azurerm_linux_virtual_machine"
	TypeAzureVMWindows    Type = "azurerm_windows_virtual_machine"
	TypeAzureFunction     Type = "azurerm_function_app"
	TypeContainerInstance Type = "azurerm_container_group"
	TypeAKS               Type = "azurerm_kubernetes_cluster"
	TypeAppService        Type = "azurerm_app_service"

	// Azure Storage
	TypeBlobStorage       Type = "azurerm_storage_container"
	TypeAzureStorageAcct  Type = "azurerm_storage_account"
	TypeManagedDisk       Type = "azurerm_managed_disk"
	TypeAzureFiles        Type = "azurerm_storage_share"

	// Azure Database
	TypeAzureSQL          Type = "azurerm_mssql_database"
	TypeAzurePostgres     Type = "azurerm_postgresql_flexible_server"
	TypeAzureMySQL        Type = "azurerm_mysql_flexible_server"
	TypeCosmosDB          Type = "azurerm_cosmosdb_account"
	TypeAzureCache        Type = "azurerm_redis_cache"

	// Azure Networking
	TypeAzureLB           Type = "azurerm_lb"
	TypeAppGateway        Type = "azurerm_application_gateway"
	TypeAzureDNS          Type = "azurerm_dns_zone"
	TypeAzureCDN          Type = "azurerm_cdn_profile"
	TypeFrontDoor         Type = "azurerm_frontdoor"
	TypeAzureVNet         Type = "azurerm_virtual_network"

	// Azure Security
	TypeAzureADB2C        Type = "azurerm_aadb2c_directory"
	TypeKeyVault          Type = "azurerm_key_vault"
	TypeAzureFirewall     Type = "azurerm_firewall"

	// Azure Messaging
	TypeServiceBus        Type = "azurerm_servicebus_namespace"
	TypeServiceBusQueue   Type = "azurerm_servicebus_queue"
	TypeEventHub          Type = "azurerm_eventhub"
	TypeEventGrid         Type = "azurerm_eventgrid_topic"
	TypeLogicApp          Type = "azurerm_logic_app_workflow"
)

// String returns the string representation of the resource type.
func (t Type) String() string {
	return string(t)
}

// IsValid checks if the resource type is a recognized type.
func (t Type) IsValid() bool {
	return t.Provider() != ""
}

// Provider returns the cloud provider for this resource type.
func (t Type) Provider() Provider {
	s := string(t)
	switch {
	case len(s) > 4 && s[:4] == "aws_":
		return ProviderAWS
	case len(s) > 7 && s[:7] == "google_":
		return ProviderGCP
	case len(s) > 8 && s[:8] == "azurerm_":
		return ProviderAzure
	default:
		return ""
	}
}

// GetCategory returns the normalized category of the resource type.
// Uses the CategoryMapping defined in category.go.
func (t Type) GetCategory() Category {
	return GetCategory(t)
}

// Provider represents a cloud provider
type Provider string

const (
	ProviderAWS   Provider = "aws"
	ProviderGCP   Provider = "gcp"
	ProviderAzure Provider = "azure"
)

// String returns the string representation of the provider
func (p Provider) String() string {
	return string(p)
}
