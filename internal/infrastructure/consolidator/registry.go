package consolidator

import (
	"sync"

	"github.com/homeport/homeport/internal/domain/stack"
)

// StackDefinition defines the default services for a stack type.
// It provides the base configuration used when creating new stacks.
type StackDefinition struct {
	// Type is the stack type this definition is for
	Type stack.StackType

	// Name is the default name for this stack type
	Name string

	// Description provides context about what this stack contains
	Description string

	// PrimaryImage is the main Docker image for this stack type
	PrimaryImage string

	// DefaultPorts lists the default port mappings
	DefaultPorts []string

	// Dependencies lists stack types this stack depends on
	Dependencies []stack.StackType

	// SupportServices are additional helper services (e.g., Grafana for observability)
	SupportServices []ServiceDefinition
}

// ServiceDefinition defines a supporting service within a stack.
type ServiceDefinition struct {
	// Name is the service name
	Name string

	// Image is the Docker image to use
	Image string

	// Ports lists port mappings
	Ports []string

	// Role describes the service's purpose (e.g., "primary", "ui", "backup")
	Role string
}

// Registry holds all merger implementations and stack definitions.
// It provides a central place to register and look up mergers for different stack types.
type Registry struct {
	mu          sync.RWMutex
	mergers     map[stack.StackType]Merger
	definitions map[stack.StackType]*StackDefinition
}

// NewRegistry creates a registry with default stack definitions.
func NewRegistry() *Registry {
	r := &Registry{
		mergers:     make(map[stack.StackType]Merger),
		definitions: make(map[stack.StackType]*StackDefinition),
	}

	// Register default definitions
	for stackType, def := range DefaultDefinitions {
		r.definitions[stackType] = def
	}

	return r
}

// Register adds a merger to the registry.
func (r *Registry) Register(merger Merger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mergers[merger.StackType()] = merger
}

// Get returns the merger for a stack type.
// Returns nil and false if no merger is registered for the type.
func (r *Registry) Get(stackType stack.StackType) (Merger, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	merger, ok := r.mergers[stackType]
	return merger, ok
}

// GetDefinition returns the stack definition for a type.
// Returns nil and false if no definition exists for the type.
func (r *Registry) GetDefinition(stackType stack.StackType) (*StackDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.definitions[stackType]
	return def, ok
}

// SetDefinition adds or updates a stack definition.
func (r *Registry) SetDefinition(def *StackDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.definitions[def.Type] = def
}

// ListMergers returns all registered stack types that have mergers.
func (r *Registry) ListMergers() []stack.StackType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]stack.StackType, 0, len(r.mergers))
	for t := range r.mergers {
		types = append(types, t)
	}
	return types
}

// ListDefinitions returns all registered stack types that have definitions.
func (r *Registry) ListDefinitions() []stack.StackType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]stack.StackType, 0, len(r.definitions))
	for t := range r.definitions {
		types = append(types, t)
	}
	return types
}

// DefaultDefinitions contains the standard stack definitions.
// These provide the base configuration for each stack type.
var DefaultDefinitions = map[stack.StackType]*StackDefinition{
	stack.StackTypeObservability: {
		Type:         stack.StackTypeObservability,
		Name:         "observability",
		Description:  "Metrics, logs, and alerting",
		PrimaryImage: "prom/prometheus:latest",
		DefaultPorts: []string{"9090:9090"},
		SupportServices: []ServiceDefinition{
			{Name: "grafana", Image: "grafana/grafana:latest", Ports: []string{"3000:3000"}, Role: "ui"},
			{Name: "loki", Image: "grafana/loki:latest", Ports: []string{"3100:3100"}, Role: "logs"},
			{Name: "alertmanager", Image: "prom/alertmanager:latest", Ports: []string{"9093:9093"}, Role: "alerts"},
		},
	},
	stack.StackTypeMessaging: {
		Type:         stack.StackTypeMessaging,
		Name:         "messaging",
		Description:  "Message queues and event streaming",
		PrimaryImage: "rabbitmq:3-management",
		DefaultPorts: []string{"5672:5672", "15672:15672"},
	},
	stack.StackTypeDatabase: {
		Type:         stack.StackTypeDatabase,
		Name:         "database",
		Description:  "Relational databases",
		PrimaryImage: "postgres:16",
		DefaultPorts: []string{"5432:5432"},
		SupportServices: []ServiceDefinition{
			{Name: "pgbouncer", Image: "edoburu/pgbouncer:latest", Ports: []string{"6432:6432"}, Role: "pooler"},
		},
	},
	stack.StackTypeCache: {
		Type:         stack.StackTypeCache,
		Name:         "cache",
		Description:  "In-memory caching",
		PrimaryImage: "redis:7",
		DefaultPorts: []string{"6379:6379"},
	},
	stack.StackTypeAuth: {
		Type:         stack.StackTypeAuth,
		Name:         "auth",
		Description:  "Identity and authentication",
		PrimaryImage: "quay.io/keycloak/keycloak:latest",
		DefaultPorts: []string{"8080:8080"},
		Dependencies: []stack.StackType{stack.StackTypeDatabase},
	},
	stack.StackTypeStorage: {
		Type:         stack.StackTypeStorage,
		Name:         "storage",
		Description:  "Object storage",
		PrimaryImage: "minio/minio:latest",
		DefaultPorts: []string{"9000:9000", "9001:9001"},
	},
	stack.StackTypeSecrets: {
		Type:         stack.StackTypeSecrets,
		Name:         "secrets",
		Description:  "Secret management",
		PrimaryImage: "hashicorp/vault:latest",
		DefaultPorts: []string{"8200:8200"},
	},
	stack.StackTypeCompute: {
		Type:         stack.StackTypeCompute,
		Name:         "compute",
		Description:  "Serverless functions",
		PrimaryImage: "openfaas/gateway:latest",
		DefaultPorts: []string{"8080:8080"},
	},
}

// GetDefaultImage returns the primary image for a stack type.
// Returns an empty string if the stack type has no definition.
func GetDefaultImage(stackType stack.StackType) string {
	if def, ok := DefaultDefinitions[stackType]; ok {
		return def.PrimaryImage
	}
	return ""
}

// GetDefaultPorts returns the default ports for a stack type.
// Returns nil if the stack type has no definition.
func GetDefaultPorts(stackType stack.StackType) []string {
	if def, ok := DefaultDefinitions[stackType]; ok {
		return def.DefaultPorts
	}
	return nil
}

// GetStackDependencies returns the dependencies for a stack type.
// Returns nil if the stack type has no dependencies.
func GetStackDependencies(stackType stack.StackType) []stack.StackType {
	if def, ok := DefaultDefinitions[stackType]; ok {
		return def.Dependencies
	}
	return nil
}

// CreateDefaultStack creates a new stack with default configuration for the given type.
func CreateDefaultStack(stackType stack.StackType, namePrefix string) *stack.Stack {
	def, ok := DefaultDefinitions[stackType]
	if !ok {
		// Return a minimal stack for unknown types
		return stack.NewStack(stackType, namePrefix+"-"+string(stackType))
	}

	name := def.Name
	if namePrefix != "" {
		name = namePrefix + "-" + def.Name
	}

	s := stack.NewStack(stackType, name)
	s.Description = def.Description

	// Add primary service
	primarySvc := stack.NewService(def.Name, def.PrimaryImage)
	primarySvc.Ports = def.DefaultPorts
	s.AddService(primarySvc)

	// Add support services
	for _, supportDef := range def.SupportServices {
		supportSvc := stack.NewService(supportDef.Name, supportDef.Image)
		supportSvc.Ports = supportDef.Ports
		supportSvc.Labels["role"] = supportDef.Role
		s.AddService(supportSvc)
	}

	// Add dependencies
	for _, dep := range def.Dependencies {
		s.AddDependency(dep)
	}

	return s
}

// GetAlternativeImages returns alternative image options for a stack type.
// This is useful for user selection or fallback scenarios.
func GetAlternativeImages(stackType stack.StackType) map[string]string {
	alternatives := make(map[string]string)

	switch stackType {
	case stack.StackTypeDatabase:
		alternatives["postgres"] = "postgres:16"
		alternatives["mysql"] = "mysql:8"
		alternatives["mariadb"] = "mariadb:11"
		alternatives["mongodb"] = "mongo:7"
	case stack.StackTypeMessaging:
		alternatives["rabbitmq"] = "rabbitmq:3-management"
		alternatives["nats"] = "nats:latest"
		alternatives["kafka"] = "bitnami/kafka:latest"
		alternatives["redis-streams"] = "redis:7"
	case stack.StackTypeCache:
		alternatives["redis"] = "redis:7"
		alternatives["memcached"] = "memcached:latest"
		alternatives["dragonfly"] = "docker.dragonflydb.io/dragonflydb/dragonfly"
	case stack.StackTypeAuth:
		alternatives["keycloak"] = "quay.io/keycloak/keycloak:latest"
		alternatives["authentik"] = "ghcr.io/goauthentik/server:latest"
		alternatives["authelia"] = "authelia/authelia:latest"
	case stack.StackTypeStorage:
		alternatives["minio"] = "minio/minio:latest"
		alternatives["seaweedfs"] = "chrislusf/seaweedfs:latest"
	case stack.StackTypeSecrets:
		alternatives["vault"] = "hashicorp/vault:latest"
		alternatives["infisical"] = "infisical/infisical:latest"
	case stack.StackTypeObservability:
		alternatives["prometheus"] = "prom/prometheus:latest"
		alternatives["victoriametrics"] = "victoriametrics/victoria-metrics:latest"
	case stack.StackTypeCompute:
		alternatives["openfaas"] = "openfaas/gateway:latest"
		alternatives["knative"] = "gcr.io/knative-releases/knative.dev/serving/cmd/controller:latest"
	}

	return alternatives
}
