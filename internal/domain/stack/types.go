// Package stack defines domain types for stack consolidation.
// It provides the core abstractions for grouping cloud resources into logical stacks
// that can be deployed as consolidated Docker Compose services.
package stack

// StackType represents a logical grouping of cloud resources.
// Resources of similar function are consolidated into a single stack type
// to reduce deployment complexity and improve manageability.
type StackType string

const (
	// StackTypeObservability groups monitoring, logging, and tracing resources.
	// Maps to: CloudWatch, Cloud Monitoring, Azure Monitor, etc.
	// Self-hosted: Prometheus + Grafana + Loki stack
	StackTypeObservability StackType = "observability"

	// StackTypeMessaging groups queue, pub/sub, and event streaming resources.
	// Maps to: SQS, SNS, Kinesis, Pub/Sub, Service Bus, Event Hub, etc.
	// Self-hosted: RabbitMQ, Redis Streams, or NATS
	StackTypeMessaging StackType = "messaging"

	// StackTypeDatabase groups SQL and NoSQL database resources.
	// Maps to: RDS, DynamoDB, Cloud SQL, Firestore, Azure SQL, Cosmos DB, etc.
	// Self-hosted: PostgreSQL, MySQL, MongoDB, etc.
	StackTypeDatabase StackType = "database"

	// StackTypeCache groups caching resources.
	// Maps to: ElastiCache, Memorystore, Azure Cache for Redis, etc.
	// Self-hosted: Redis, Memcached
	StackTypeCache StackType = "cache"

	// StackTypeAuth groups authentication and identity resources.
	// Maps to: Cognito, Identity Platform, Azure AD B2C, etc.
	// Self-hosted: Keycloak, Authentik
	StackTypeAuth StackType = "auth"

	// StackTypeStorage groups object and file storage resources.
	// Maps to: S3, GCS, Blob Storage, EFS, Filestore, Azure Files, etc.
	// Self-hosted: MinIO, SeaweedFS
	StackTypeStorage StackType = "storage"

	// StackTypeSecrets groups secret management resources.
	// Maps to: Secrets Manager, Secret Manager, Key Vault, etc.
	// Self-hosted: Vault, Doppler
	StackTypeSecrets StackType = "secrets"

	// StackTypeCompute groups general compute resources.
	// Maps to: Lambda, Cloud Functions, Azure Functions, etc.
	// Self-hosted: OpenFaaS, Knative
	StackTypeCompute StackType = "compute"

	// StackTypePassthrough represents resources that don't consolidate.
	// These remain as individual services: EC2, VMs, ECS, EKS, GKE, AKS, etc.
	// Each resource becomes its own Docker service.
	StackTypePassthrough StackType = "passthrough"
)

// allStackTypes contains all valid stack types for iteration.
var allStackTypes = []StackType{
	StackTypeObservability,
	StackTypeMessaging,
	StackTypeDatabase,
	StackTypeCache,
	StackTypeAuth,
	StackTypeStorage,
	StackTypeSecrets,
	StackTypeCompute,
	StackTypePassthrough,
}

// AllStackTypes returns all valid stack types.
// Useful for iteration, validation, and UI display.
func AllStackTypes() []StackType {
	result := make([]StackType, len(allStackTypes))
	copy(result, allStackTypes)
	return result
}

// String returns the string representation of the stack type.
func (s StackType) String() string {
	return string(s)
}

// IsValid checks if the stack type is a recognized type.
func (s StackType) IsValid() bool {
	for _, st := range allStackTypes {
		if s == st {
			return true
		}
	}
	return false
}

// DisplayName returns a human-friendly display name for the stack type.
func (s StackType) DisplayName() string {
	switch s {
	case StackTypeObservability:
		return "Observability"
	case StackTypeMessaging:
		return "Messaging"
	case StackTypeDatabase:
		return "Database"
	case StackTypeCache:
		return "Cache"
	case StackTypeAuth:
		return "Authentication"
	case StackTypeStorage:
		return "Storage"
	case StackTypeSecrets:
		return "Secrets"
	case StackTypeCompute:
		return "Compute"
	case StackTypePassthrough:
		return "Passthrough"
	default:
		return string(s)
	}
}

// Description returns a brief description of what the stack type contains.
func (s StackType) Description() string {
	switch s {
	case StackTypeObservability:
		return "Monitoring, logging, and tracing services"
	case StackTypeMessaging:
		return "Message queues, pub/sub, and event streaming"
	case StackTypeDatabase:
		return "SQL and NoSQL databases"
	case StackTypeCache:
		return "In-memory caching services"
	case StackTypeAuth:
		return "Authentication and identity management"
	case StackTypeStorage:
		return "Object and file storage"
	case StackTypeSecrets:
		return "Secret and credential management"
	case StackTypeCompute:
		return "Serverless functions and compute"
	case StackTypePassthrough:
		return "Individual services (VMs, containers, Kubernetes)"
	default:
		return "Unknown stack type"
	}
}
