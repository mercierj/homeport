// Package mapper defines the core mapper interface and types for converting AWS resources
// to self-hosted alternatives.
package mapper

import (
	"context"

	"github.com/agnostech/agnostech/internal/domain/resource"
)

// Mapper converts an AWS resource to a self-hosted alternative.
type Mapper interface {
	// ResourceType returns the AWS resource type this mapper handles.
	ResourceType() resource.Type

	// Map converts an AWS resource to a Docker-based equivalent.
	// Returns a MappingResult containing the Docker service definition,
	// configuration files, scripts, and any warnings or manual steps.
	Map(ctx context.Context, res *resource.AWSResource) (*MappingResult, error)

	// Validate checks if the resource can be mapped.
	// Returns an error if the resource is invalid or cannot be mapped.
	Validate(res *resource.AWSResource) error

	// Dependencies returns the list of resource types this mapper depends on.
	// Used for determining mapping order in complex deployments.
	Dependencies() []resource.Type
}

// BaseMapper provides common functionality for all mappers.
// Concrete mappers should embed this struct.
type BaseMapper struct {
	resourceType resource.Type
	dependencies []resource.Type
}

// NewBaseMapper creates a new base mapper.
func NewBaseMapper(resourceType resource.Type, dependencies []resource.Type) *BaseMapper {
	if dependencies == nil {
		dependencies = []resource.Type{}
	}
	return &BaseMapper{
		resourceType: resourceType,
		dependencies: dependencies,
	}
}

// ResourceType returns the AWS resource type this mapper handles.
func (m *BaseMapper) ResourceType() resource.Type {
	return m.resourceType
}

// Dependencies returns the list of resource types this mapper depends on.
func (m *BaseMapper) Dependencies() []resource.Type {
	return m.dependencies
}

// Validate performs basic validation on the resource.
// Concrete mappers should override this method to add specific validation.
func (m *BaseMapper) Validate(res *resource.AWSResource) error {
	if res == nil {
		return ErrNilResource
	}
	if res.Type != m.resourceType {
		return NewErrInvalidResourceType(res.Type, m.resourceType)
	}
	if res.ID == "" {
		return ErrMissingResourceID
	}
	return nil
}

// Resource represents an AWS resource to be mapped.
type Resource struct {
	// Type is the AWS resource type (e.g., "aws_s3_bucket")
	Type resource.Type

	// Name is the resource name/identifier
	Name string

	// Attributes contains the raw resource attributes from Terraform state
	Attributes map[string]interface{}

	// Dependencies lists other resources this one depends on
	Dependencies []string

	// Tags are AWS tags applied to the resource
	Tags map[string]string
}

// NOTE: MappingResult, DockerService, HealthCheck, Resources, and other result types
// are defined in result.go. Error types are defined in errors.go.
