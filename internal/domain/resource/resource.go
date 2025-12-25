package resource

import "time"

// AWSResource represents a cloud resource discovered from AWS infrastructure.
// It contains all the metadata and configuration needed to map the resource
// to a self-hosted equivalent.
type AWSResource struct {
	// ID is the unique identifier of the resource (e.g., instance ID, bucket name)
	ID string

	// Name is the human-readable name of the resource
	Name string

	// Type is the AWS resource type (e.g., aws_instance, aws_s3_bucket)
	Type Type

	// ARN is the Amazon Resource Name
	ARN string

	// Region is the AWS region where the resource is located
	Region string

	// Config contains the full configuration of the resource as key-value pairs.
	// This includes all attributes needed for mapping to Docker equivalents.
	Config map[string]interface{}

	// Tags are the AWS tags associated with the resource
	Tags map[string]string

	// Dependencies lists the IDs of other resources this resource depends on.
	// Used for determining deployment order in the dependency graph.
	Dependencies []string

	// CreatedAt is when this resource was created in AWS
	CreatedAt time.Time
}

// NewAWSResource creates a new AWS resource with the given ID, name, and type.
func NewAWSResource(id, name string, resourceType Type) *AWSResource {
	return &AWSResource{
		ID:           id,
		Name:         name,
		Type:         resourceType,
		Config:       make(map[string]interface{}),
		Tags:         make(map[string]string),
		Dependencies: make([]string, 0),
		CreatedAt:    time.Now(),
	}
}

// GetConfig retrieves a configuration value by key.
// Returns nil if the key doesn't exist.
func (r *AWSResource) GetConfig(key string) interface{} {
	return r.Config[key]
}

// GetConfigString retrieves a configuration value as a string.
// Returns empty string if the key doesn't exist or value is not a string.
func (r *AWSResource) GetConfigString(key string) string {
	if val, ok := r.Config[key].(string); ok {
		return val
	}
	return ""
}

// GetConfigInt retrieves a configuration value as an int.
// Returns 0 if the key doesn't exist or value is not numeric.
// Handles int, int64, and float64 types (JSON parsing returns float64 for numbers).
func (r *AWSResource) GetConfigInt(key string) int {
	switch val := r.Config[key].(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case int32:
		return int(val)
	}
	return 0
}

// GetConfigBool retrieves a configuration value as a bool.
// Returns false if the key doesn't exist or value is not a bool.
func (r *AWSResource) GetConfigBool(key string) bool {
	if val, ok := r.Config[key].(bool); ok {
		return val
	}
	return false
}

// GetTag retrieves a tag value by key.
// Returns empty string if the tag doesn't exist.
func (r *AWSResource) GetTag(key string) string {
	return r.Tags[key]
}

// HasTag checks if a tag exists.
func (r *AWSResource) HasTag(key string) bool {
	_, ok := r.Tags[key]
	return ok
}

// AddDependency adds a dependency to another resource.
func (r *AWSResource) AddDependency(resourceID string) {
	r.Dependencies = append(r.Dependencies, resourceID)
}

// HasDependencies checks if the resource has any dependencies.
func (r *AWSResource) HasDependencies() bool {
	return len(r.Dependencies) > 0
}

// Resource is an alias for AWSResource to maintain compatibility with the Infrastructure model
type Resource = AWSResource
