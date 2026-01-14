package mapper

import (
	"fmt"

	"github.com/homeport/homeport/internal/domain/resource"
)

// Domain errors for the mapper package.
var (
	// ErrNilResource is returned when a nil resource is provided.
	ErrNilResource = &MapperError{Message: "resource cannot be nil"}

	// ErrMissingResourceID is returned when a resource has no ID.
	ErrMissingResourceID = &MapperError{Message: "resource ID is required"}

	// ErrUnsupportedResource is returned when a resource type is not supported.
	ErrUnsupportedResource = &MapperError{Message: "unsupported resource type"}

	// ErrMappingFailed is returned when mapping fails.
	ErrMappingFailed = &MapperError{Message: "mapping failed"}

	// ErrValidationFailed is returned when resource validation fails.
	ErrValidationFailed = &MapperError{Message: "validation failed"}

	// ErrMapperNotFound is returned when no mapper is found for a resource type.
	ErrMapperNotFound = &MapperError{Message: "mapper not found"}

	// ErrMapperAlreadyRegistered is returned when trying to register a mapper twice.
	ErrMapperAlreadyRegistered = &MapperError{Message: "mapper already registered"}
)

// MapperError represents a domain error in the mapper layer.
type MapperError struct {
	Message string
	Err     error
}

// Error implements the error interface.
func (e *MapperError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

// Unwrap implements error unwrapping for Go 1.13+ error handling.
func (e *MapperError) Unwrap() error {
	return e.Err
}

// NewErrInvalidResourceType creates an error for invalid resource types.
func NewErrInvalidResourceType(got, expected resource.Type) error {
	return &MapperError{
		Message: fmt.Sprintf("invalid resource type: got %s, expected %s", got, expected),
	}
}

// NewErrUnsupportedResource creates an error for unsupported resources.
func NewErrUnsupportedResource(resourceType resource.Type) error {
	return &MapperError{
		Message: fmt.Sprintf("unsupported resource type: %s", resourceType),
	}
}

// NewErrMappingFailed creates an error for mapping failures.
func NewErrMappingFailed(resourceType resource.Type, err error) error {
	return &MapperError{
		Message: fmt.Sprintf("failed to map resource type %s", resourceType),
		Err:     err,
	}
}

// NewErrValidationFailed creates an error for validation failures.
func NewErrValidationFailed(resourceType resource.Type, reason string) error {
	return &MapperError{
		Message: fmt.Sprintf("validation failed for %s: %s", resourceType, reason),
	}
}

// NewErrMapperNotFound creates an error when no mapper is found.
func NewErrMapperNotFound(resourceType resource.Type) error {
	return &MapperError{
		Message: fmt.Sprintf("no mapper found for resource type: %s", resourceType),
	}
}

// NewErrMapperAlreadyRegistered creates an error when a mapper is already registered.
func NewErrMapperAlreadyRegistered(resourceType resource.Type) error {
	return &MapperError{
		Message: fmt.Sprintf("mapper already registered for resource type: %s", resourceType),
	}
}

// NewErrMissingRequiredField creates an error for missing required fields.
func NewErrMissingRequiredField(resourceType resource.Type, field string) error {
	return &MapperError{
		Message: fmt.Sprintf("missing required field '%s' for resource type %s", field, resourceType),
	}
}

// WrapError wraps an error with additional context.
func WrapError(msg string, err error) error {
	return &MapperError{
		Message: msg,
		Err:     err,
	}
}
