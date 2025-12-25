// Package generator defines the domain interface for generating output artifacts
// from mapped resources (Docker Compose files, Traefik configs, documentation, etc.).
package generator

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/target"
)

// Generator defines the interface for generating output artifacts from mapping results.
// Different generators produce different types of output (Docker Compose, Traefik, docs, etc.).
type Generator interface {
	// Name returns the name of this generator.
	Name() string

	// Generate produces output artifacts from the given mapping results.
	// The output is written to the provided writer.
	Generate(ctx context.Context, results []*mapper.MappingResult, opts *Options) (*Output, error)

	// Validate checks if the mapping results can be processed by this generator.
	Validate(results []*mapper.MappingResult) error

	// SupportedFormats returns the list of output formats this generator supports.
	SupportedFormats() []Format
}

// Options contains configuration options for generation.
type Options struct {
	// Format specifies the output format.
	Format Format

	// OutputDir is the directory where files should be written.
	OutputDir string

	// ProjectName is the name of the Docker Compose project.
	ProjectName string

	// IncludeComments controls whether to include explanatory comments.
	IncludeComments bool

	// NetworkName is the Docker network name to use.
	NetworkName string

	// BaseURL is the base URL for Traefik routing.
	BaseURL string

	// SSLEnabled controls whether to enable SSL/TLS.
	SSLEnabled bool

	// Additional generator-specific options.
	Extra map[string]interface{}
}

// Format represents the output format type.
type Format string

const (
	// FormatDockerCompose generates Docker Compose YAML files.
	FormatDockerCompose Format = "docker-compose"

	// FormatTraefik generates Traefik configuration files.
	FormatTraefik Format = "traefik"

	// FormatMarkdown generates Markdown documentation.
	FormatMarkdown Format = "markdown"

	// FormatHTML generates HTML documentation.
	FormatHTML Format = "html"

	// FormatShell generates shell scripts.
	FormatShell Format = "shell"

	// FormatJSON generates JSON output.
	FormatJSON Format = "json"

	// FormatYAML generates YAML output.
	FormatYAML Format = "yaml"
)

// String returns the string representation of the format.
func (f Format) String() string {
	return string(f)
}

// Output represents the generated output artifacts.
type Output struct {
	// Files contains the generated files (filename -> content).
	Files map[string][]byte

	// Metadata contains additional information about the generation.
	Metadata map[string]string

	// Warnings contains non-critical issues encountered during generation.
	Warnings []string

	// Summary provides a human-readable summary of what was generated.
	Summary string

	// GeneratedAt is when the output was generated.
	GeneratedAt time.Time
}

// NewOutput creates a new output with initialized maps.
func NewOutput() *Output {
	return &Output{
		Files:       make(map[string][]byte),
		Metadata:    make(map[string]string),
		Warnings:    make([]string, 0),
		GeneratedAt: time.Now(),
	}
}

// AddFile adds a file to the output.
func (o *Output) AddFile(filename string, content []byte) {
	o.Files[filename] = content
}

// AddFileString adds a file to the output from a string.
func (o *Output) AddFileString(filename string, content string) {
	o.Files[filename] = []byte(content)
}

// AddWarning adds a warning to the output.
func (o *Output) AddWarning(warning string) {
	o.Warnings = append(o.Warnings, warning)
}

// SetMetadata sets a metadata value.
func (o *Output) SetMetadata(key, value string) {
	o.Metadata[key] = value
}

// AddMetadata is an alias for SetMetadata for backward compatibility.
func (o *Output) AddMetadata(key, value string) {
	o.SetMetadata(key, value)
}

// WriteFiles writes all output files to the specified directory.
func (o *Output) WriteFiles(basePath string) error {
	// This will be implemented in the infrastructure layer
	// as it involves file system operations
	panic("not implemented: use infrastructure layer writer")
}

// WriteTo writes all output files to the provided writer.
// This is useful for testing or streaming output.
func (o *Output) WriteTo(w io.Writer) (int64, error) {
	// This will be implemented in the infrastructure layer
	panic("not implemented: use infrastructure layer writer")
}

// HasWarnings returns true if there are any warnings.
func (o *Output) HasWarnings() bool {
	return len(o.Warnings) > 0
}

// FileCount returns the number of generated files.
func (o *Output) FileCount() int {
	return len(o.Files)
}

// NewOptions creates a new options struct with default values.
func NewOptions() *Options {
	return &Options{
		Format:          FormatDockerCompose,
		IncludeComments: true,
		NetworkName:     "cloudexit",
		Extra:           make(map[string]interface{}),
	}
}

// WithFormat sets the output format.
func (o *Options) WithFormat(format Format) *Options {
	o.Format = format
	return o
}

// WithOutputDir sets the output directory.
func (o *Options) WithOutputDir(dir string) *Options {
	o.OutputDir = dir
	return o
}

// WithProjectName sets the project name.
func (o *Options) WithProjectName(name string) *Options {
	o.ProjectName = name
	return o
}

// WithComments enables or disables comments.
func (o *Options) WithComments(enabled bool) *Options {
	o.IncludeComments = enabled
	return o
}

// WithNetwork sets the network name.
func (o *Options) WithNetwork(name string) *Options {
	o.NetworkName = name
	return o
}

// WithBaseURL sets the base URL.
func (o *Options) WithBaseURL(url string) *Options {
	o.BaseURL = url
	return o
}

// WithSSL enables or disables SSL.
func (o *Options) WithSSL(enabled bool) *Options {
	o.SSLEnabled = enabled
	return o
}

// WithExtra sets an extra option.
func (o *Options) WithExtra(key string, value interface{}) *Options {
	o.Extra[key] = value
	return o
}

// ─────────────────────────────────────────────────────────────────────────────
// Multi-Target Generator Interface (Extended for v2)
// ─────────────────────────────────────────────────────────────────────────────

// TargetGenerator generates deployment artifacts for a specific target platform.
// This extends the base Generator interface with platform-specific capabilities.
type TargetGenerator interface {
	// Platform returns the target platform this generator handles.
	Platform() target.Platform

	// Name returns the name of this generator.
	Name() string

	// Description returns a human-readable description.
	Description() string

	// Generate produces output artifacts for the target platform.
	Generate(ctx context.Context, results []*mapper.MappingResult, config *TargetConfig) (*TargetOutput, error)

	// Validate checks if the mapping results can be deployed to this platform.
	Validate(results []*mapper.MappingResult, config *TargetConfig) error

	// SupportedHALevels returns the HA levels this generator supports.
	SupportedHALevels() []target.HALevel

	// RequiresCredentials returns true if the platform needs cloud credentials.
	RequiresCredentials() bool

	// RequiredCredentials returns the list of required credential keys.
	RequiredCredentials() []string

	// EstimateCost estimates the monthly cost for the deployment.
	EstimateCost(results []*mapper.MappingResult, config *TargetConfig) (*CostEstimate, error)
}

// TargetConfig holds configuration for target-specific generation.
type TargetConfig struct {
	// Platform is the target deployment platform
	Platform target.Platform

	// HALevel is the high availability level
	HALevel target.HALevel

	// HAConfig holds detailed HA configuration
	HAConfig *target.HAConfig

	// TargetConfig holds platform-specific configuration
	TargetConfig *target.TargetConfig

	// OutputDir is the directory where files should be written
	OutputDir string

	// ProjectName is the project name
	ProjectName string

	// BaseURL is the base URL for the deployment
	BaseURL string

	// SSLEnabled enables SSL/TLS
	SSLEnabled bool

	// IncludeMonitoring enables monitoring stack
	IncludeMonitoring bool

	// IncludeBackups enables backup configuration
	IncludeBackups bool

	// DryRun only shows what would be generated
	DryRun bool

	// Variables are user-provided variables
	Variables map[string]string
}

// NewTargetConfig creates a new target config with defaults.
func NewTargetConfig(platform target.Platform) *TargetConfig {
	return &TargetConfig{
		Platform:          platform,
		HALevel:           target.HALevelNone,
		ProjectName:       "cloudexit",
		SSLEnabled:        true,
		IncludeMonitoring: true,
		IncludeBackups:    true,
		Variables:         make(map[string]string),
	}
}

// WithHALevel sets the HA level.
func (c *TargetConfig) WithHALevel(level target.HALevel) *TargetConfig {
	c.HALevel = level
	c.HAConfig = target.NewHAConfig(level)
	return c
}

// WithOutputDir sets the output directory.
func (c *TargetConfig) WithOutputDir(dir string) *TargetConfig {
	c.OutputDir = dir
	return c
}

// WithProjectName sets the project name.
func (c *TargetConfig) WithProjectName(name string) *TargetConfig {
	c.ProjectName = name
	return c
}

// WithBaseURL sets the base URL.
func (c *TargetConfig) WithBaseURL(url string) *TargetConfig {
	c.BaseURL = url
	return c
}

// WithSSL enables or disables SSL.
func (c *TargetConfig) WithSSL(enabled bool) *TargetConfig {
	c.SSLEnabled = enabled
	return c
}

// WithMonitoring enables or disables monitoring.
func (c *TargetConfig) WithMonitoring(enabled bool) *TargetConfig {
	c.IncludeMonitoring = enabled
	return c
}

// WithBackups enables or disables backups.
func (c *TargetConfig) WithBackups(enabled bool) *TargetConfig {
	c.IncludeBackups = enabled
	return c
}

// WithDryRun enables dry run mode.
func (c *TargetConfig) WithDryRun(enabled bool) *TargetConfig {
	c.DryRun = enabled
	return c
}

// WithVariable sets a variable.
func (c *TargetConfig) WithVariable(key, value string) *TargetConfig {
	c.Variables[key] = value
	return c
}

// TargetOutput contains all generated artifacts for a target platform.
type TargetOutput struct {
	// Platform is the target platform
	Platform target.Platform

	// Files contains all generated files (filename -> content)
	Files map[string][]byte

	// MainFile is the primary deployment file
	MainFile string

	// Organized by type
	DockerFiles    map[string][]byte // docker-compose.*.yml
	TerraformFiles map[string][]byte // *.tf
	K8sManifests   map[string][]byte // Kubernetes YAML
	AnsibleFiles   map[string][]byte // Ansible playbooks
	HelmCharts     map[string][]byte // Helm chart files
	Scripts        map[string][]byte // Shell scripts
	Configs        map[string][]byte // Configuration files
	Docs           map[string][]byte // Documentation

	// Metadata
	Warnings    []string
	ManualSteps []string

	// Cost estimate (if available)
	EstimatedCost *CostEstimate

	// Summary
	Summary     string
	GeneratedAt time.Time
}

// NewTargetOutput creates a new target output with initialized maps.
func NewTargetOutput(platform target.Platform) *TargetOutput {
	return &TargetOutput{
		Platform:       platform,
		Files:          make(map[string][]byte),
		DockerFiles:    make(map[string][]byte),
		TerraformFiles: make(map[string][]byte),
		K8sManifests:   make(map[string][]byte),
		AnsibleFiles:   make(map[string][]byte),
		HelmCharts:     make(map[string][]byte),
		Scripts:        make(map[string][]byte),
		Configs:        make(map[string][]byte),
		Docs:           make(map[string][]byte),
		Warnings:       make([]string, 0),
		ManualSteps:    make([]string, 0),
		GeneratedAt:    time.Now(),
	}
}

// AddFile adds a file to the output.
func (o *TargetOutput) AddFile(filename string, content []byte) {
	o.Files[filename] = content
}

// AddDockerFile adds a Docker-related file.
func (o *TargetOutput) AddDockerFile(filename string, content []byte) {
	o.DockerFiles[filename] = content
	o.Files[filename] = content
}

// AddTerraformFile adds a Terraform file.
func (o *TargetOutput) AddTerraformFile(filename string, content []byte) {
	o.TerraformFiles[filename] = content
	o.Files[filename] = content
}

// AddK8sManifest adds a Kubernetes manifest.
func (o *TargetOutput) AddK8sManifest(filename string, content []byte) {
	o.K8sManifests[filename] = content
	o.Files[filename] = content
}

// AddAnsibleFile adds an Ansible file.
func (o *TargetOutput) AddAnsibleFile(filename string, content []byte) {
	o.AnsibleFiles[filename] = content
	o.Files[filename] = content
}

// AddScript adds a script file.
func (o *TargetOutput) AddScript(filename string, content []byte) {
	o.Scripts[filename] = content
	o.Files[filename] = content
}

// AddConfig adds a config file.
func (o *TargetOutput) AddConfig(filename string, content []byte) {
	o.Configs[filename] = content
	o.Files[filename] = content
}

// AddDoc adds a documentation file.
func (o *TargetOutput) AddDoc(filename string, content []byte) {
	o.Docs[filename] = content
	o.Files[filename] = content
}

// AddWarning adds a warning.
func (o *TargetOutput) AddWarning(warning string) {
	o.Warnings = append(o.Warnings, warning)
}

// AddManualStep adds a manual step.
func (o *TargetOutput) AddManualStep(step string) {
	o.ManualSteps = append(o.ManualSteps, step)
}

// HasWarnings returns true if there are warnings.
func (o *TargetOutput) HasWarnings() bool {
	return len(o.Warnings) > 0
}

// FileCount returns the number of generated files.
func (o *TargetOutput) FileCount() int {
	return len(o.Files)
}

// WriteTo writes all output files to the provided writer.
func (o *TargetOutput) WriteTo(w io.Writer) (int64, error) {
	panic("not implemented: use infrastructure layer writer")
}

// CostEstimate represents an estimated monthly cost.
type CostEstimate struct {
	Currency string             // e.g., "EUR", "USD"
	Compute  float64            // Compute costs
	Storage  float64            // Storage costs
	Database float64            // Database costs
	Network  float64            // Network/bandwidth costs
	Other    float64            // Other costs
	Total    float64            // Total monthly cost
	Details  map[string]float64 // Detailed breakdown
	Notes    []string           // Notes about the estimate
}

// NewCostEstimate creates a new cost estimate.
func NewCostEstimate(currency string) *CostEstimate {
	return &CostEstimate{
		Currency: currency,
		Details:  make(map[string]float64),
		Notes:    make([]string, 0),
	}
}

// Calculate calculates the total from components.
func (c *CostEstimate) Calculate() {
	c.Total = c.Compute + c.Storage + c.Database + c.Network + c.Other
}

// AddDetail adds a detailed cost item.
func (c *CostEstimate) AddDetail(name string, cost float64) {
	c.Details[name] = cost
}

// AddNote adds a note about the estimate.
func (c *CostEstimate) AddNote(note string) {
	c.Notes = append(c.Notes, note)
}

// ─────────────────────────────────────────────────────────────────────────────
// Generator Registry
// ─────────────────────────────────────────────────────────────────────────────

// Registry manages generator implementations.
type Registry struct {
	mu         sync.RWMutex
	generators map[target.Platform]TargetGenerator
	legacy     map[string]Generator // For backward compatibility
}

// NewRegistry creates a new generator registry.
func NewRegistry() *Registry {
	return &Registry{
		generators: make(map[target.Platform]TargetGenerator),
		legacy:     make(map[string]Generator),
	}
}

// Register adds a target generator to the registry.
func (r *Registry) Register(g TargetGenerator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generators[g.Platform()] = g
}

// RegisterLegacy adds a legacy generator for backward compatibility.
func (r *Registry) RegisterLegacy(name string, g Generator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.legacy[name] = g
}

// Get returns a generator for the specified platform.
func (r *Registry) Get(platform target.Platform) (TargetGenerator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if g, ok := r.generators[platform]; ok {
		return g, nil
	}
	return nil, ErrNoGeneratorFound
}

// GetLegacy returns a legacy generator by name.
func (r *Registry) GetLegacy(name string) (Generator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if g, ok := r.legacy[name]; ok {
		return g, nil
	}
	return nil, ErrNoGeneratorFound
}

// All returns all registered target generators.
func (r *Registry) All() []TargetGenerator {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]TargetGenerator, 0, len(r.generators))
	for _, g := range r.generators {
		result = append(result, g)
	}
	return result
}

// Platforms returns all platforms that have registered generators.
func (r *Registry) Platforms() []target.Platform {
	r.mu.RLock()
	defer r.mu.RUnlock()

	platforms := make([]target.Platform, 0, len(r.generators))
	for p := range r.generators {
		platforms = append(platforms, p)
	}
	return platforms
}

// SupportedHALevels returns HA levels supported by a platform's generator.
func (r *Registry) SupportedHALevels(platform target.Platform) []target.HALevel {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if g, ok := r.generators[platform]; ok {
		return g.SupportedHALevels()
	}
	return nil
}

// Generate uses the appropriate generator to produce output.
func (r *Registry) Generate(ctx context.Context, platform target.Platform, results []*mapper.MappingResult, config *TargetConfig) (*TargetOutput, error) {
	gen, err := r.Get(platform)
	if err != nil {
		return nil, err
	}

	if err := gen.Validate(results, config); err != nil {
		return nil, err
	}

	return gen.Generate(ctx, results, config)
}

// ErrNoGeneratorFound is returned when no suitable generator is found.
var ErrNoGeneratorFound = errors.New("no generator found for the specified platform")

// Global default registry
var defaultRegistry = NewRegistry()

// DefaultRegistry returns the default generator registry.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// RegisterGenerator adds a generator to the default registry.
func RegisterGenerator(g TargetGenerator) {
	defaultRegistry.Register(g)
}

// GetGenerator returns a generator from the default registry.
func GetGenerator(platform target.Platform) (TargetGenerator, error) {
	return defaultRegistry.Get(platform)
}

// Generate uses the default registry to generate output.
func Generate(ctx context.Context, platform target.Platform, results []*mapper.MappingResult, config *TargetConfig) (*TargetOutput, error) {
	return defaultRegistry.Generate(ctx, platform, results, config)
}
