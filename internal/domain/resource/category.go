// Package resource defines core domain types and entities for cloud resources.
package resource

// Category represents a normalized category of cloud resources across providers.
// This allows mapping between equivalent services from different cloud providers.
type Category string

const (
	// Compute categories
	CategoryCompute    Category = "compute"    // VMs, instances
	CategoryContainer  Category = "container"  // Container instances, Cloud Run, Container Instances
	CategoryServerless Category = "serverless" // Lambda, Cloud Functions, Azure Functions
	CategoryKubernetes Category = "kubernetes" // EKS, GKE, AKS

	// Storage categories
	CategoryObjectStorage Category = "object_storage" // S3, GCS, Blob Storage
	CategoryBlockStorage  Category = "block_storage"  // EBS, Persistent Disk, Managed Disks
	CategoryFileStorage   Category = "file_storage"   // EFS, Filestore, Azure Files

	// Database categories
	CategorySQLDatabase   Category = "sql_database"   // RDS, Cloud SQL, Azure SQL
	CategoryNoSQLDatabase Category = "nosql_database" // DynamoDB, Firestore, Cosmos DB
	CategoryCache         Category = "cache"          // ElastiCache, Memorystore, Azure Cache

	// Messaging categories
	CategoryQueue  Category = "queue"  // SQS, Pub/Sub, Service Bus Queue
	CategoryPubSub Category = "pubsub" // SNS, Pub/Sub Topics, Event Grid
	CategoryStream Category = "stream" // Kinesis, Pub/Sub, Event Hubs

	// Networking categories
	CategoryLoadBalancer Category = "load_balancer" // ALB/NLB, Cloud LB, Azure LB
	CategoryCDN          Category = "cdn"           // CloudFront, Cloud CDN, Azure CDN
	CategoryDNS          Category = "dns"           // Route53, Cloud DNS, Azure DNS
	CategoryAPIGateway   Category = "api_gateway"   // API Gateway, Cloud Endpoints, API Management
	CategoryVPC          Category = "vpc"           // VPC, VPC Network, VNet

	// Security categories
	CategoryAuth       Category = "auth"        // Cognito, Identity Platform, Azure AD B2C
	CategorySecrets    Category = "secrets"     // Secrets Manager, Secret Manager, Key Vault
	CategoryIAM        Category = "iam"         // IAM roles/policies
	CategoryFirewall   Category = "firewall"    // Security Groups, Cloud Armor, Azure Firewall
	CategoryCertificate Category = "certificate" // ACM, Certificate Manager, App Service Certificates

	// Monitoring categories
	CategoryMonitoring Category = "monitoring" // CloudWatch, Cloud Monitoring, Azure Monitor
	CategoryLogging    Category = "logging"    // CloudWatch Logs, Cloud Logging, Log Analytics
	CategoryTracing    Category = "tracing"    // X-Ray, Cloud Trace, Application Insights

	// Other
	CategoryUnknown Category = "unknown"
)

// String returns the string representation of the category.
func (c Category) String() string {
	return string(c)
}

// IsValid checks if the category is a recognized category.
func (c Category) IsValid() bool {
	switch c {
	case CategoryCompute, CategoryContainer, CategoryServerless, CategoryKubernetes,
		CategoryObjectStorage, CategoryBlockStorage, CategoryFileStorage,
		CategorySQLDatabase, CategoryNoSQLDatabase, CategoryCache,
		CategoryQueue, CategoryPubSub, CategoryStream,
		CategoryLoadBalancer, CategoryCDN, CategoryDNS, CategoryAPIGateway, CategoryVPC,
		CategoryAuth, CategorySecrets, CategoryIAM, CategoryFirewall, CategoryCertificate,
		CategoryMonitoring, CategoryLogging, CategoryTracing:
		return true
	default:
		return false
	}
}

// CategoryMapping maps resource types to their normalized categories.
// This is used to find equivalent resources across cloud providers.
var CategoryMapping = map[Type]Category{
	// ─────────────────────────────────────────────────────
	// AWS Resource Mappings (Complete)
	// ─────────────────────────────────────────────────────

	// AWS Compute
	TypeEC2Instance:    CategoryCompute,
	TypeLambdaFunction: CategoryServerless,
	TypeECSService:     CategoryContainer,
	TypeECSTaskDef:     CategoryContainer,
	TypeEKSCluster:     CategoryKubernetes,

	// AWS Storage
	TypeS3Bucket:  CategoryObjectStorage,
	TypeEBSVolume: CategoryBlockStorage,
	TypeEFSVolume: CategoryFileStorage,

	// AWS Database
	TypeRDSInstance:   CategorySQLDatabase,
	TypeRDSCluster:    CategorySQLDatabase,
	TypeDynamoDBTable: CategoryNoSQLDatabase,
	TypeElastiCache:   CategoryCache,

	// AWS Networking
	TypeALB:         CategoryLoadBalancer,
	TypeAPIGateway:  CategoryAPIGateway,
	TypeRoute53Zone: CategoryDNS,
	TypeCloudFront:  CategoryCDN,
	TypeVPC:         CategoryVPC,

	// AWS Security
	TypeCognitoPool:    CategoryAuth,
	TypeSecretsManager: CategorySecrets,
	TypeIAMRole:        CategoryIAM,
	TypeACMCertificate: CategoryCertificate,

	// AWS Messaging
	TypeSQSQueue:    CategoryQueue,
	TypeSNSTopic:    CategoryPubSub,
	TypeEventBridge: CategoryPubSub,
	TypeKinesis:     CategoryStream,

	// ─────────────────────────────────────────────────────
	// GCP Resource Mappings (Complete)
	// ─────────────────────────────────────────────────────

	// GCP Compute
	TypeGCEInstance:   CategoryCompute,
	TypeCloudRun:      CategoryContainer,
	TypeCloudFunction: CategoryServerless,
	TypeGKE:           CategoryKubernetes,
	TypeAppEngine:     CategoryCompute,

	// GCP Storage
	TypeGCSBucket:      CategoryObjectStorage,
	TypePersistentDisk: CategoryBlockStorage,
	TypeFilestore:      CategoryFileStorage,

	// GCP Database
	TypeCloudSQL:    CategorySQLDatabase,
	TypeFirestore:   CategoryNoSQLDatabase,
	TypeBigtable:    CategoryNoSQLDatabase,
	TypeMemorystore: CategoryCache,
	TypeSpanner:     CategorySQLDatabase,

	// GCP Networking
	TypeCloudLB:       CategoryLoadBalancer,
	TypeCloudDNS:      CategoryDNS,
	TypeCloudCDN:      CategoryCDN,
	TypeCloudArmor:    CategoryFirewall,
	TypeGCPVPCNetwork: CategoryVPC,

	// GCP Security
	TypeIdentityPlatform: CategoryAuth,
	TypeSecretManager:    CategorySecrets,
	TypeGCPIAM:           CategoryIAM,

	// GCP Messaging
	TypePubSubTopic:        CategoryPubSub,
	TypePubSubSubscription: CategoryPubSub,
	TypeCloudTasks:         CategoryQueue,
	TypeCloudScheduler:     CategoryPubSub,

	// ─────────────────────────────────────────────────────
	// Azure Resource Mappings (Complete)
	// ─────────────────────────────────────────────────────

	// Azure Compute
	TypeAzureVM:           CategoryCompute,
	TypeAzureVMWindows:    CategoryCompute,
	TypeAzureFunction:     CategoryServerless,
	TypeContainerInstance: CategoryContainer,
	TypeAKS:               CategoryKubernetes,
	TypeAppService:        CategoryCompute,

	// Azure Storage
	TypeBlobStorage:      CategoryObjectStorage,
	TypeAzureStorageAcct: CategoryObjectStorage,
	TypeManagedDisk:      CategoryBlockStorage,
	TypeAzureFiles:       CategoryFileStorage,

	// Azure Database
	TypeAzureSQL:      CategorySQLDatabase,
	TypeAzurePostgres: CategorySQLDatabase,
	TypeAzureMySQL:    CategorySQLDatabase,
	TypeCosmosDB:      CategoryNoSQLDatabase,
	TypeAzureCache:    CategoryCache,

	// Azure Networking
	TypeAzureLB:      CategoryLoadBalancer,
	TypeAppGateway:   CategoryLoadBalancer,
	TypeAzureDNS:     CategoryDNS,
	TypeAzureCDN:     CategoryCDN,
	TypeFrontDoor:    CategoryCDN,
	TypeAzureVNet:    CategoryVPC,
	TypeAzureFirewall: CategoryFirewall,

	// Azure Security
	TypeAzureADB2C: CategoryAuth,
	TypeKeyVault:   CategorySecrets,

	// Azure Messaging
	TypeServiceBus:      CategoryQueue,
	TypeServiceBusQueue: CategoryQueue,
	TypeEventHub:        CategoryStream,
	TypeEventGrid:       CategoryPubSub,
	TypeLogicApp:        CategoryServerless,
}

// GetCategory returns the category for a given resource type.
// Returns CategoryUnknown if the type is not mapped.
func GetCategory(t Type) Category {
	if cat, ok := CategoryMapping[t]; ok {
		return cat
	}
	return CategoryUnknown
}

// GetTypesForCategory returns all resource types that belong to a category.
func GetTypesForCategory(category Category) []Type {
	var types []Type
	for t, c := range CategoryMapping {
		if c == category {
			types = append(types, t)
		}
	}
	return types
}

// GetTypesForCategoryAndProvider returns resource types for a category filtered by provider.
func GetTypesForCategoryAndProvider(category Category, provider Provider) []Type {
	var types []Type
	for t, c := range CategoryMapping {
		if c == category && t.Provider() == provider {
			types = append(types, t)
		}
	}
	return types
}
