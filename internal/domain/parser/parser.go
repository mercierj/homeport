// Package parser defines the interface for parsing cloud infrastructure configurations.
package parser

import (
	"context"
	"errors"

	"github.com/agnostech/agnostech/internal/domain/resource"
)

// Parser defines the interface for parsing cloud infrastructure.
// Different implementations handle different sources (Terraform, API, CloudFormation, etc.)
// and different providers (AWS, GCP, Azure).
type Parser interface {
	// Provider returns the cloud provider this parser handles.
	Provider() resource.Provider

	// Parse analyzes the infrastructure at the given path and returns discovered resources.
	Parse(ctx context.Context, path string, opts *ParseOptions) (*resource.Infrastructure, error)

	// Validate checks if the path contains valid infrastructure for this parser.
	// Returns nil if valid, or an error describing why it's invalid.
	Validate(path string) error

	// SupportedFormats returns the configuration formats this parser can handle.
	SupportedFormats() []Format

	// AutoDetect checks if this parser can handle the given path.
	// Returns true if it can handle it, along with a confidence score (0.0-1.0).
	// Higher scores indicate more confidence that this is the right parser.
	AutoDetect(path string) (canHandle bool, confidence float64)
}

// Format represents a configuration format that can be parsed.
type Format string

const (
	// Terraform formats
	FormatTerraform   Format = "terraform"    // Terraform HCL files
	FormatTerragrunt  Format = "terragrunt"   // Terragrunt configurations
	FormatTFState     Format = "tfstate"      // Terraform state files
	FormatTFPlan      Format = "tfplan"       // Terraform plan files

	// Cloud-specific formats
	FormatCloudFormation    Format = "cloudformation"     // AWS CloudFormation
	FormatDeploymentManager Format = "deployment_manager" // GCP Deployment Manager
	FormatARM               Format = "arm"                // Azure Resource Manager
	FormatBicep             Format = "bicep"              // Azure Bicep

	// API-based
	FormatAPI Format = "api" // Direct API scanning

	// Container orchestration
	FormatDockerCompose Format = "docker-compose" // Docker Compose files
	FormatKubernetes    Format = "kubernetes"     // Kubernetes manifests
	FormatHelm          Format = "helm"           // Helm charts
)

// String returns the string representation of the format.
func (f Format) String() string {
	return string(f)
}

// ParseOptions configures parsing behavior.
type ParseOptions struct {
	// IncludeSensitive includes sensitive data (secrets, passwords) in output.
	// Default: false for security.
	IncludeSensitive bool

	// FilterTypes limits parsing to specific resource types.
	// Empty means parse all types.
	FilterTypes []resource.Type

	// FilterCategories limits parsing to specific resource categories.
	// Empty means parse all categories.
	FilterCategories []resource.Category

	// FollowModules follows and parses Terraform modules.
	FollowModules bool

	// MaxDepth limits module recursion depth. 0 means unlimited.
	MaxDepth int

	// IgnoreErrors continues parsing even if some resources fail.
	IgnoreErrors bool

	// APICredentials for API-based parsing.
	APICredentials map[string]string

	// Regions to scan for API-based parsing.
	Regions []string

	// ExcludePatterns are glob patterns for paths to exclude.
	ExcludePatterns []string

	// IncludePatterns are glob patterns for paths to include.
	// Empty means include all.
	IncludePatterns []string
}

// NewParseOptions creates parse options with sensible defaults.
func NewParseOptions() *ParseOptions {
	return &ParseOptions{
		IncludeSensitive: false,
		FollowModules:    true,
		MaxDepth:         10,
		IgnoreErrors:     false,
	}
}

// WithSensitive enables including sensitive data.
func (o *ParseOptions) WithSensitive(include bool) *ParseOptions {
	o.IncludeSensitive = include
	return o
}

// WithFilterTypes sets type filters.
func (o *ParseOptions) WithFilterTypes(types ...resource.Type) *ParseOptions {
	o.FilterTypes = types
	return o
}

// WithFilterCategories sets category filters.
func (o *ParseOptions) WithFilterCategories(categories ...resource.Category) *ParseOptions {
	o.FilterCategories = categories
	return o
}

// WithFollowModules enables or disables module following.
func (o *ParseOptions) WithFollowModules(follow bool) *ParseOptions {
	o.FollowModules = follow
	return o
}

// WithMaxDepth sets the maximum module depth.
func (o *ParseOptions) WithMaxDepth(depth int) *ParseOptions {
	o.MaxDepth = depth
	return o
}

// WithIgnoreErrors enables ignoring parse errors.
func (o *ParseOptions) WithIgnoreErrors(ignore bool) *ParseOptions {
	o.IgnoreErrors = ignore
	return o
}

// WithRegions sets regions to scan.
func (o *ParseOptions) WithRegions(regions ...string) *ParseOptions {
	o.Regions = regions
	return o
}

// WithCredentials sets API credentials.
func (o *ParseOptions) WithCredentials(creds map[string]string) *ParseOptions {
	o.APICredentials = creds
	return o
}

// ParseResult contains the result of parsing infrastructure.
type ParseResult struct {
	// Infrastructure is the parsed infrastructure.
	Infrastructure *resource.Infrastructure

	// Errors contains non-fatal errors encountered during parsing.
	Errors []error

	// Warnings contains warnings about the parsed infrastructure.
	Warnings []string

	// Stats contains parsing statistics.
	Stats ParseStats
}

// ParseStats contains statistics about the parsing operation.
type ParseStats struct {
	// FilesScanned is the number of files scanned.
	FilesScanned int

	// ResourcesFound is the total number of resources found.
	ResourcesFound int

	// ResourcesByType maps resource types to counts.
	ResourcesByType map[resource.Type]int

	// ResourcesByCategory maps categories to counts.
	ResourcesByCategory map[resource.Category]int

	// ModulesFollowed is the number of modules followed.
	ModulesFollowed int

	// Errors is the number of errors encountered.
	ErrorCount int
}

// Common parsing errors.
var (
	ErrNoFilesFound     = errors.New("no infrastructure files found")
	ErrInvalidPath      = errors.New("invalid path")
	ErrUnsupportedFormat = errors.New("unsupported configuration format")
	ErrParseFailure     = errors.New("failed to parse infrastructure")
	ErrNoCredentials    = errors.New("API credentials required but not provided")
	ErrInvalidCredentials = errors.New("invalid API credentials")
)

// ParseError represents a parsing error with context.
type ParseError struct {
	File    string // File where error occurred
	Line    int    // Line number (if applicable)
	Message string
	Err     error
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e.File != "" {
		if e.Line > 0 {
			return e.File + ":" + string(rune(e.Line)) + ": " + e.Message
		}
		return e.File + ": " + e.Message
	}
	return e.Message
}

// Unwrap returns the underlying error.
func (e *ParseError) Unwrap() error {
	return e.Err
}
