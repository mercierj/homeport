package migrate

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/domain/target"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
	infraMapper "github.com/homeport/homeport/internal/infrastructure/mapper"
	"github.com/homeport/homeport/internal/infrastructure/parser/aws"
)

// Service handles migration analysis and generation.
type Service struct {
	registry     *infraMapper.Registry
	stateStore   *StateStore
	consolidator *consolidator.Consolidator
}

// NewService creates a new migration service.
func NewService() *Service {
	return &Service{
		registry:     infraMapper.GlobalRegistry,
		consolidator: consolidator.New(),
	}
}

// NewServiceWithState creates a migration service with state persistence.
func NewServiceWithState(statePath string) (*Service, error) {
	store, err := NewStateStore(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state store: %w", err)
	}

	return &Service{
		registry:     infraMapper.GlobalRegistry,
		stateStore:   store,
		consolidator: consolidator.New(),
	}, nil
}

// NewServiceWithConsolidator creates a migration service with a custom consolidator.
func NewServiceWithConsolidator(c *consolidator.Consolidator) *Service {
	return &Service{
		registry:     infraMapper.GlobalRegistry,
		consolidator: c,
	}
}

// SaveDiscovery persists a discovery result for later use.
func (s *Service) SaveDiscovery(name string, resp *AnalyzeResponse) (*DiscoveryState, error) {
	if s.stateStore == nil {
		return nil, fmt.Errorf("state store not initialized")
	}

	return s.stateStore.Save(name, resp.Provider, nil, resp.Resources)
}

// ListDiscoveries returns all saved discoveries.
func (s *Service) ListDiscoveries() []*DiscoveryState {
	if s.stateStore == nil {
		return nil
	}

	return s.stateStore.List()
}

// GetDiscovery retrieves a saved discovery by ID.
func (s *Service) GetDiscovery(id string) (*DiscoveryState, error) {
	if s.stateStore == nil {
		return nil, fmt.Errorf("state store not initialized")
	}

	return s.stateStore.Get(id)
}

// DeleteDiscovery removes a saved discovery.
func (s *Service) DeleteDiscovery(id string) error {
	if s.stateStore == nil {
		return fmt.Errorf("state store not initialized")
	}

	return s.stateStore.Delete(id)
}

// RenameDiscovery updates the name of a saved discovery.
func (s *Service) RenameDiscovery(id string, name string) (*DiscoveryState, error) {
	if s.stateStore == nil {
		return nil, fmt.Errorf("state store not initialized")
	}

	return s.stateStore.Update(id, name)
}

// AnalyzeRequest represents an analyze request.
type AnalyzeRequest struct {
	Type    string `json:"type"`    // terraform, cloudformation, arm
	Content string `json:"content"` // File content
}

// AnalyzeResponse represents the analysis result.
type AnalyzeResponse struct {
	Resources []ResourceInfo `json:"resources"`
	Warnings  []string       `json:"warnings"`
	Provider  string         `json:"provider"`
}

// ResourceInfo represents a discovered resource.
type ResourceInfo struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Category     string                 `json:"category"`
	ARN          string                 `json:"arn,omitempty"`
	Region       string                 `json:"region,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty"`
	Dependencies []string               `json:"dependencies"`
	Tags         map[string]string      `json:"tags,omitempty"`
}

// ProgressEvent represents a progress update during discovery.
type ProgressEvent struct {
	Type           string `json:"type"`            // "progress", "error", "complete"
	Step           string `json:"step"`            // Current step: "init", "regions", "scanning"
	Message        string `json:"message"`         // Human-readable message
	Region         string `json:"region,omitempty"`// Current region being scanned
	Service        string `json:"service,omitempty"`// Current service being scanned
	CurrentRegion  int    `json:"current_region"`  // Current region index (1-based)
	TotalRegions   int    `json:"total_regions"`   // Total number of regions
	CurrentService int    `json:"current_service"` // Current service index (1-based)
	TotalServices  int    `json:"total_services"`  // Total number of services to scan
	ResourcesFound int    `json:"resources_found"` // Total resources found so far
}

// ProgressCallback is called during discovery to report progress.
type ProgressCallback func(event ProgressEvent)

// Analyze parses infrastructure files and returns discovered resources.
func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (*AnalyzeResponse, error) {
	// Write content to temp file
	tmpDir, err := os.MkdirTemp("", "homeport-analyze-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	ext := ".tf"
	switch req.Type {
	case "cloudformation":
		ext = ".yaml"
	case "arm":
		ext = ".json"
	}

	tmpFile := filepath.Join(tmpDir, "main"+ext)
	if err := os.WriteFile(tmpFile, []byte(req.Content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}

	// Use existing parser
	p := aws.NewTerraformParser()
	infra, err := p.Parse(ctx, tmpDir, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}

	// Convert to response format
	resp := &AnalyzeResponse{
		Resources: make([]ResourceInfo, 0, len(infra.Resources)),
		Warnings:  []string{},
		Provider:  string(infra.Provider),
	}

	for _, res := range infra.Resources {
		resp.Resources = append(resp.Resources, ResourceInfo{
			ID:           res.ID,
			Name:         res.Name,
			Type:         string(res.Type),
			Category:     string(res.Type.GetCategory()),
			ARN:          res.ARN,
			Region:       res.Region,
			Config:       res.Config,
			Dependencies: res.Dependencies,
			Tags:         res.Tags,
		})
	}

	return resp, nil
}

// DiscoverRequest represents a cloud API discovery request.
type DiscoverRequest struct {
	Provider string   `json:"provider"` // aws, gcp, azure
	Regions  []string `json:"regions,omitempty"`

	// AWS credentials
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	Region          string `json:"region,omitempty"`

	// GCP credentials
	ProjectID          string `json:"project_id,omitempty"`
	ServiceAccountJSON string `json:"service_account_json,omitempty"`

	// Azure credentials
	SubscriptionID string `json:"subscription_id,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
	ClientID       string `json:"client_id,omitempty"`
	ClientSecret   string `json:"client_secret,omitempty"`
}

// Discover uses cloud provider APIs to discover infrastructure.
func (s *Service) Discover(ctx context.Context, req DiscoverRequest) (*AnalyzeResponse, error) {
	return s.DiscoverWithProgress(ctx, req, nil)
}

// DiscoverWithProgress uses cloud provider APIs to discover infrastructure with progress callbacks.
func (s *Service) DiscoverWithProgress(ctx context.Context, req DiscoverRequest, onProgress ProgressCallback) (*AnalyzeResponse, error) {
	// Helper to safely call progress callback
	emitProgress := func(event ProgressEvent) {
		if onProgress != nil {
			onProgress(event)
		}
	}

	// Build credentials map based on provider
	creds := make(map[string]string)

	var provider resource.Provider
	switch req.Provider {
	case "aws":
		provider = resource.ProviderAWS
		creds["access_key_id"] = req.AccessKeyID
		creds["secret_access_key"] = req.SecretAccessKey
	case "gcp":
		provider = resource.ProviderGCP
		creds["project_id"] = req.ProjectID
		creds["service_account_json"] = req.ServiceAccountJSON
	case "azure":
		provider = resource.ProviderAzure
		creds["subscription_id"] = req.SubscriptionID
		creds["tenant_id"] = req.TenantID
		creds["client_id"] = req.ClientID
		creds["client_secret"] = req.ClientSecret
	default:
		return nil, fmt.Errorf("unknown provider: %s (supported: aws, gcp, azure)", req.Provider)
	}

	// Send initial progress event
	emitProgress(ProgressEvent{
		Type:    "progress",
		Step:    "init",
		Message: fmt.Sprintf("Connecting to %s...", strings.ToUpper(req.Provider)),
	})

	// Build regions list - if empty, parser will scan all regions
	regions := req.Regions
	if len(regions) == 0 && req.Region != "" {
		regions = []string{req.Region}
	}

	// Build parse options with progress callback
	opts := parser.NewParseOptions().
		WithIgnoreErrors(true).
		WithCredentials(creds).
		WithRegions(regions...)

	// Set the progress callback on parse options
	if onProgress != nil {
		opts = opts.WithProgressCallback(func(event parser.ProgressEvent) {
			emitProgress(ProgressEvent{
				Type:           "progress",
				Step:           event.Step,
				Message:        event.Message,
				Region:         event.Region,
				Service:        event.Service,
				CurrentRegion:  event.CurrentRegion,
				TotalRegions:   event.TotalRegions,
				CurrentService: event.CurrentService,
				TotalServices:  event.TotalServices,
				ResourcesFound: event.ResourcesFound,
			})
		})
	}

	infra, err := parser.DefaultRegistry().ParseWithProvider(ctx, "api://"+req.Provider, provider, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to discover infrastructure: %w", err)
	}

	// Convert to response format
	resp := &AnalyzeResponse{
		Resources: make([]ResourceInfo, 0, len(infra.Resources)),
		Warnings:  []string{},
		Provider:  string(infra.Provider),
	}

	for _, res := range infra.Resources {
		resp.Resources = append(resp.Resources, ResourceInfo{
			ID:           res.ID,
			Name:         res.Name,
			Type:         string(res.Type),
			Category:     string(res.Type.GetCategory()),
			ARN:          res.ARN,
			Region:       res.Region,
			Config:       res.Config,
			Dependencies: res.Dependencies,
			Tags:         res.Tags,
		})
	}

	return resp, nil
}

// GenerateRequest represents a generate request.
type GenerateRequest struct {
	Resources []ResourceInfo  `json:"resources"`
	Options   GenerateOptions `json:"options"`
}

// GenerateOptions configures generation behavior.
type GenerateOptions struct {
	HA                bool   `json:"ha"`
	IncludeMigration  bool   `json:"include_migration"`
	IncludeMonitoring bool   `json:"include_monitoring"`
	Domain            string `json:"domain"`
}

// GenerateResponse represents the generation result.
type GenerateResponse struct {
	Compose   string            `json:"compose"`
	Scripts   map[string]string `json:"scripts"`
	Docs      string            `json:"docs"`
	ZipBase64 string            `json:"zip_base64,omitempty"`
}

// Generate creates Docker Compose stack from resources.
func (s *Service) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	results := make([]*mapper.MappingResult, 0)
	warnings := []string{}

	for _, res := range req.Resources {
		awsRes := &resource.AWSResource{
			ID:           res.ID,
			Name:         res.Name,
			Type:         resource.Type(res.Type),
			Dependencies: res.Dependencies,
			Tags:         res.Tags,
		}

		result, err := s.registry.Map(ctx, awsRes)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Failed to map %s: %v", res.Name, err))
			continue
		}
		results = append(results, result)
	}

	// Deduplicate and merge services
	mergedResults := deduplicateResults(results)

	// Generate compose file
	compose := generateCompose(mergedResults)
	scripts := generateScripts(mergedResults, req.Options)
	docs := generateDocs(mergedResults, req.Options)

	return &GenerateResponse{
		Compose: compose,
		Scripts: scripts,
		Docs:    docs,
	}, nil
}

// GenerateZip creates a downloadable zip file with all generated artifacts.
func (s *Service) GenerateZip(ctx context.Context, req GenerateRequest) ([]byte, error) {
	results := make([]*mapper.MappingResult, 0)

	for _, res := range req.Resources {
		awsRes := &resource.AWSResource{
			ID:           res.ID,
			Name:         res.Name,
			Type:         resource.Type(res.Type),
			Dependencies: res.Dependencies,
			Tags:         res.Tags,
		}

		result, err := s.registry.Map(ctx, awsRes)
		if err != nil {
			continue
		}
		results = append(results, result)
	}

	// Deduplicate and merge services
	mergedResults := deduplicateResults(results)

	// Generate all content
	compose := generateCompose(mergedResults)
	scripts := generateScripts(mergedResults, req.Options)
	docs := generateDocs(mergedResults, req.Options)
	configs := collectConfigs(mergedResults)
	envExample := generateEnvExample(mergedResults)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Add compose file
	w, err := zw.Create("docker-compose.yml")
	if err != nil {
		return nil, fmt.Errorf("failed to create compose file in zip: %w", err)
	}
	if _, err := w.Write([]byte(compose)); err != nil {
		return nil, fmt.Errorf("failed to write compose file: %w", err)
	}

	// Add scripts
	for name, content := range scripts {
		w, err := zw.Create("scripts/" + name)
		if err != nil {
			return nil, fmt.Errorf("failed to create script in zip: %w", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return nil, fmt.Errorf("failed to write script: %w", err)
		}
	}

	// Add config files (rabbitmq, nats, traefik configs)
	for path, content := range configs {
		w, err := zw.Create(path)
		if err != nil {
			return nil, fmt.Errorf("failed to create config file in zip: %w", err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write config file: %w", err)
		}
	}

	// Add docs
	w, err = zw.Create("README.md")
	if err != nil {
		return nil, fmt.Errorf("failed to create README in zip: %w", err)
	}
	if _, err := w.Write([]byte(docs)); err != nil {
		return nil, fmt.Errorf("failed to write README: %w", err)
	}

	// Add .env.example
	w, err = zw.Create(".env.example")
	if err != nil {
		return nil, fmt.Errorf("failed to create .env.example in zip: %w", err)
	}
	if _, err := w.Write([]byte(envExample)); err != nil {
		return nil, fmt.Errorf("failed to write .env.example: %w", err)
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip: %w", err)
	}

	return buf.Bytes(), nil
}

// infrastructureServices maps service types that should be deduplicated (shared infrastructure)
var infrastructureServices = map[string]bool{
	"minio":    true, // S3 buckets -> single MinIO
	"rabbitmq": true, // SQS queues -> single RabbitMQ
	"nats":     true, // SNS topics -> single NATS
	"traefik":  true, // Load balancers -> single Traefik
	"scylladb": true, // DynamoDB tables -> single ScyllaDB
}

// deduplicateResults merges results that map to the same infrastructure service
func deduplicateResults(results []*mapper.MappingResult) []*mapper.MappingResult {
	serviceMap := make(map[string]*mapper.MappingResult)
	var uniqueResults []*mapper.MappingResult

	for _, r := range results {
		if r.DockerService == nil {
			continue
		}

		serviceName := r.DockerService.Name

		// Check if this is an infrastructure service that should be deduplicated
		if infrastructureServices[serviceName] {
			if existing, ok := serviceMap[serviceName]; ok {
				// Merge warnings and manual steps (deduplicated later)
				existing.Warnings = append(existing.Warnings, r.Warnings...)
				existing.ManualSteps = append(existing.ManualSteps, r.ManualSteps...)
				// Merge configs
				for k, v := range r.Configs {
					existing.Configs[k] = v
				}
				// Merge scripts
				for k, v := range r.Scripts {
					existing.Scripts[k] = v
				}
			} else {
				serviceMap[serviceName] = r
				uniqueResults = append(uniqueResults, r)
			}
		} else {
			// Non-infrastructure services (Lambda functions, EC2 instances) keep unique names
			uniqueResults = append(uniqueResults, r)
		}
	}

	return uniqueResults
}

// collectConfigs gathers all config files from results
func collectConfigs(results []*mapper.MappingResult) map[string][]byte {
	configs := make(map[string][]byte)

	for _, r := range results {
		for path, content := range r.Configs {
			configs[path] = content
		}
	}

	return configs
}

// generateEnvExample creates a .env.example file
func generateEnvExample(results []*mapper.MappingResult) string {
	var buf bytes.Buffer
	buf.WriteString("# Environment Configuration\n")
	buf.WriteString("# Generated by Homeport\n\n")

	buf.WriteString("# General\n")
	buf.WriteString("DOMAIN=localhost\n")
	buf.WriteString("TZ=UTC\n\n")

	// Collect unique env vars from services
	envVars := make(map[string]string)
	for _, r := range results {
		if r.DockerService != nil {
			for k, v := range r.DockerService.Environment {
				if _, exists := envVars[k]; !exists {
					// Mask sensitive values
					if containsSensitive(k) {
						envVars[k] = "CHANGE_ME"
					} else {
						envVars[k] = v
					}
				}
			}
		}
	}

	// Group by category
	buf.WriteString("# Service Configuration\n")
	for k, v := range envVars {
		buf.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}

	return buf.String()
}

func containsSensitive(key string) bool {
	key = strings.ToUpper(key)
	sensitivePatterns := []string{"PASSWORD", "SECRET", "KEY", "TOKEN", "CREDENTIAL"}
	for _, pattern := range sensitivePatterns {
		if strings.Contains(key, pattern) {
			return true
		}
	}
	return false
}

func generateCompose(results []*mapper.MappingResult) string {
	var buf bytes.Buffer
	buf.WriteString("services:\n")

	// Track used ports to avoid conflicts
	usedPorts := make(map[string]bool)
	portCounter := make(map[int]int) // base port -> counter for conflicts

	// Deduplicate services by name to avoid duplicate YAML keys
	seenServices := make(map[string]bool)

	for _, r := range results {
		if r.DockerService != nil {
			// Skip if we've already written this service
			if seenServices[r.DockerService.Name] {
				continue
			}
			seenServices[r.DockerService.Name] = true

			buf.WriteString(fmt.Sprintf("  %s:\n", r.DockerService.Name))

			// Add build context if specified (for locally-built images)
			if r.DockerService.Build != nil && r.DockerService.Build.Context != "" {
				buf.WriteString(fmt.Sprintf("    build: %s\n", r.DockerService.Build.Context))
			}
			buf.WriteString(fmt.Sprintf("    image: %s\n", r.DockerService.Image))

			// Handle ports with conflict resolution
			if len(r.DockerService.Ports) > 0 {
				buf.WriteString("    ports:\n")
				for _, p := range r.DockerService.Ports {
					resolvedPort := resolvePortConflict(p, usedPorts, portCounter)
					buf.WriteString(fmt.Sprintf("      - \"%s\"\n", resolvedPort))
				}
			}

			if len(r.DockerService.Environment) > 0 {
				buf.WriteString("    environment:\n")
				for k, v := range r.DockerService.Environment {
					buf.WriteString(fmt.Sprintf("      %s: \"%s\"\n", k, v))
				}
			}

			if len(r.DockerService.Volumes) > 0 {
				buf.WriteString("    volumes:\n")
				for _, v := range r.DockerService.Volumes {
					buf.WriteString(fmt.Sprintf("      - \"%s\"\n", v))
				}
			}

			// Add command if present
			if len(r.DockerService.Command) > 0 {
				buf.WriteString(fmt.Sprintf("    command: %v\n", r.DockerService.Command))
			}

			// Add networks
			buf.WriteString("    networks:\n")
			buf.WriteString("      - internal\n")
			if hasPublicPorts(r.DockerService.Ports) {
				buf.WriteString("      - web\n")
			}

			if r.DockerService.Restart != "" {
				buf.WriteString(fmt.Sprintf("    restart: %s\n", r.DockerService.Restart))
			} else {
				buf.WriteString("    restart: unless-stopped\n")
			}

			buf.WriteString("\n")
		}
	}

	// Add networks section
	buf.WriteString("networks:\n")
	buf.WriteString("  web:\n")
	buf.WriteString("    driver: bridge\n")
	buf.WriteString("  internal:\n")
	buf.WriteString("    driver: bridge\n")
	buf.WriteString("    internal: true\n")

	// Add volumes section if needed
	hasVolumes := false
	volumeSet := make(map[string]bool)
	for _, r := range results {
		if len(r.Volumes) > 0 {
			hasVolumes = true
			for _, v := range r.Volumes {
				volumeSet[v.Name] = true
			}
		}
	}

	if hasVolumes {
		buf.WriteString("\nvolumes:\n")
		for volName := range volumeSet {
			buf.WriteString(fmt.Sprintf("  %s:\n", volName))
		}
	}

	return buf.String()
}

// resolvePortConflict checks if a port is already used and resolves conflicts
func resolvePortConflict(portMapping string, usedPorts map[string]bool, portCounter map[int]int) string {
	parts := strings.Split(portMapping, ":")
	if len(parts) != 2 {
		return portMapping
	}

	hostPort := parts[0]
	containerPort := parts[1]

	// Check if host port is already used
	if usedPorts[hostPort] {
		// Find next available port
		basePort := 0
		fmt.Sscanf(hostPort, "%d", &basePort)
		if basePort > 0 {
			portCounter[basePort]++
			newPort := basePort + portCounter[basePort]*100
			hostPort = fmt.Sprintf("%d", newPort)
		}
	}

	usedPorts[hostPort] = true
	return fmt.Sprintf("%s:%s", hostPort, containerPort)
}

// hasPublicPorts checks if any ports should be on the public network
func hasPublicPorts(ports []string) bool {
	publicPorts := map[string]bool{"80": true, "443": true, "8080": true}
	for _, p := range ports {
		parts := strings.Split(p, ":")
		if len(parts) >= 1 {
			if publicPorts[parts[0]] {
				return true
			}
		}
	}
	return false
}

func generateScripts(results []*mapper.MappingResult, opts GenerateOptions) map[string]string {
	scripts := make(map[string]string)

	// Always generate startup script
	var startup bytes.Buffer
	startup.WriteString("#!/bin/bash\nset -e\n\n")
	startup.WriteString("echo 'Starting Homeport stack...'\n")
	startup.WriteString("docker compose up -d\n")
	startup.WriteString("echo 'Stack started successfully!'\n")
	scripts["start.sh"] = startup.String()

	// Generate shutdown script
	var shutdown bytes.Buffer
	shutdown.WriteString("#!/bin/bash\nset -e\n\n")
	shutdown.WriteString("echo 'Stopping Homeport stack...'\n")
	shutdown.WriteString("docker compose down\n")
	shutdown.WriteString("echo 'Stack stopped.'\n")
	scripts["stop.sh"] = shutdown.String()

	if opts.IncludeMigration {
		var migrate bytes.Buffer
		migrate.WriteString("#!/bin/bash\nset -e\n\n")
		migrate.WriteString("echo 'Running migration scripts...'\n")
		for _, r := range results {
			for name := range r.Scripts {
				migrate.WriteString(fmt.Sprintf("echo 'Running %s...'\n", name))
			}
		}
		migrate.WriteString("echo 'Migration completed!'\n")
		scripts["migrate.sh"] = migrate.String()
	}

	return scripts
}

func generateDocs(results []*mapper.MappingResult, opts GenerateOptions) string {
	var buf bytes.Buffer
	buf.WriteString("# Generated Homeport Stack\n\n")
	buf.WriteString("This stack was generated by Homeport to replace your cloud infrastructure.\n\n")

	buf.WriteString("## Services\n\n")
	for _, r := range results {
		if r.DockerService != nil {
			buf.WriteString(fmt.Sprintf("### %s\n\n", r.DockerService.Name))
			buf.WriteString(fmt.Sprintf("- **Image**: `%s`\n", r.DockerService.Image))
			if len(r.DockerService.Ports) > 0 {
				buf.WriteString(fmt.Sprintf("- **Ports**: %v\n", r.DockerService.Ports))
			}
			buf.WriteString("\n")
		}
	}

	buf.WriteString("## Quick Start\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Copy environment file\n")
	buf.WriteString("cp .env.example .env\n\n")
	buf.WriteString("# Start the stack\n")
	buf.WriteString("chmod +x scripts/*.sh\n")
	buf.WriteString("./scripts/start.sh\n")
	buf.WriteString("```\n\n")

	if opts.IncludeMigration {
		buf.WriteString("## Migration\n\n")
		buf.WriteString("Run the migration script to transfer data:\n\n")
		buf.WriteString("```bash\n")
		buf.WriteString("./scripts/migrate.sh\n")
		buf.WriteString("```\n\n")
	}

	// Collect and deduplicate warnings and manual steps
	warningSet := make(map[string]bool)
	stepSet := make(map[string]bool)
	var warnings []string
	var manualSteps []string

	for _, r := range results {
		for _, w := range r.Warnings {
			if !warningSet[w] {
				warningSet[w] = true
				warnings = append(warnings, w)
			}
		}
		for _, s := range r.ManualSteps {
			if !stepSet[s] {
				stepSet[s] = true
				manualSteps = append(manualSteps, s)
			}
		}
	}

	if len(warnings) > 0 {
		buf.WriteString("## Warnings\n\n")
		for _, w := range warnings {
			buf.WriteString(fmt.Sprintf("- %s\n", w))
		}
		buf.WriteString("\n")
	}

	if len(manualSteps) > 0 {
		buf.WriteString("## Manual Steps Required\n\n")
		for i, step := range manualSteps {
			buf.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
		buf.WriteString("\n")
	}

	buf.WriteString("## Configuration\n\n")
	buf.WriteString("All services are configured via environment variables.\n")
	buf.WriteString("Copy `.env.example` to `.env` and update values as needed.\n\n")

	buf.WriteString("## Network Architecture\n\n")
	buf.WriteString("- **web**: Public-facing network (Traefik, exposed services)\n")
	buf.WriteString("- **internal**: Private network for inter-service communication\n\n")

	return buf.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Consolidated Migration Types and Methods
// ─────────────────────────────────────────────────────────────────────────────

// MigrateConfig configures the migration generation process.
type MigrateConfig struct {
	// Consolidate enables stack consolidation (transforms 30+ containers into ~8 stacks)
	Consolidate bool `json:"consolidate"`

	// Platform is the target deployment platform
	Platform target.Platform `json:"platform"`

	// HALevel is the high availability level for the deployment
	HALevel target.HALevel `json:"ha_level"`

	// ProjectName is the name of the Docker Compose project
	ProjectName string `json:"project_name"`

	// Domain is the base domain for service routing
	Domain string `json:"domain"`

	// SSLEnabled enables SSL/TLS certificate generation
	SSLEnabled bool `json:"ssl_enabled"`

	// IncludeMonitoring enables the observability stack
	IncludeMonitoring bool `json:"include_monitoring"`

	// IncludeBackups enables backup configuration
	IncludeBackups bool `json:"include_backups"`

	// IncludeMigrationScripts includes data migration scripts
	IncludeMigrationScripts bool `json:"include_migration_scripts"`

	// ConsolidationOptions controls consolidation behavior
	ConsolidationOptions *ConsolidationOptions `json:"consolidation_options,omitempty"`
}

// ConsolidationOptions configures consolidation behavior.
type ConsolidationOptions struct {
	// EnabledStacks limits which stack types are processed
	// If empty, all stack types are enabled
	EnabledStacks []stack.StackType `json:"enabled_stacks,omitempty"`

	// DatabaseEngine specifies which database engine to use
	// Options: "postgres", "mysql", "mariadb"
	DatabaseEngine string `json:"database_engine,omitempty"`

	// MessagingBroker specifies which messaging broker to use
	// Options: "rabbitmq", "nats", "kafka"
	MessagingBroker string `json:"messaging_broker,omitempty"`

	// NamePrefix is added to generated stack and service names
	NamePrefix string `json:"name_prefix,omitempty"`

	// IncludeSupportServices enables support services (Grafana, pgBouncer, etc.)
	IncludeSupportServices bool `json:"include_support_services"`
}

// NewMigrateConfig creates a new migration config with sensible defaults.
func NewMigrateConfig() *MigrateConfig {
	return &MigrateConfig{
		Consolidate:             true,
		Platform:                target.PlatformDockerCompose,
		HALevel:                 target.HALevelNone,
		ProjectName:             "homeport",
		SSLEnabled:              true,
		IncludeMonitoring:       true,
		IncludeBackups:          true,
		IncludeMigrationScripts: true,
		ConsolidationOptions:    NewConsolidationOptions(),
	}
}

// NewConsolidationOptions creates new consolidation options with defaults.
func NewConsolidationOptions() *ConsolidationOptions {
	return &ConsolidationOptions{
		EnabledStacks:          nil, // All stacks enabled
		DatabaseEngine:         "postgres",
		MessagingBroker:        "rabbitmq",
		IncludeSupportServices: true,
	}
}

// MigrateResult represents the output of a consolidated migration.
type MigrateResult struct {
	// Output is the generated target output (docker-compose, terraform, etc.)
	Output *generator.TargetOutput `json:"output"`

	// ConsolidatedResult contains the consolidated stack information
	ConsolidatedResult *stack.ConsolidatedResult `json:"consolidated_result,omitempty"`

	// Metadata contains consolidation statistics
	Metadata *stack.ConsolidationMetadata `json:"metadata,omitempty"`

	// MappingResults are the individual mapping results (before consolidation)
	MappingResults []*mapper.MappingResult `json:"-"`

	// Warnings are non-fatal issues encountered during generation
	Warnings []string `json:"warnings"`

	// ManualSteps are steps that require manual intervention
	ManualSteps []string `json:"manual_steps"`

	// Summary provides a human-readable summary
	Summary string `json:"summary"`
}

// NewMigrateResult creates a new migrate result with initialized slices.
func NewMigrateResult() *MigrateResult {
	return &MigrateResult{
		Warnings:    make([]string, 0),
		ManualSteps: make([]string, 0),
	}
}

// GenerateConsolidated creates a consolidated migration output from resources.
// This method:
// 1. Maps each resource to its Docker equivalent using the mapper registry
// 2. Consolidates related resources into logical stacks using the consolidator
// 3. Uses the generator to produce deployment artifacts
func (s *Service) GenerateConsolidated(ctx context.Context, resources []ResourceInfo, config *MigrateConfig) (*MigrateResult, error) {
	if config == nil {
		config = NewMigrateConfig()
	}

	result := NewMigrateResult()

	// Step 1: Map each resource to its Docker equivalent
	mappingResults := make([]*mapper.MappingResult, 0, len(resources))

	for _, res := range resources {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		awsRes := resourceInfoToAWSResource(&res)

		mappingResult, err := s.registry.Map(ctx, awsRes)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to map %s (%s): %v", res.Name, res.Type, err))
			continue
		}

		// Store source resource info in the mapping result for consolidation
		mappingResult.SourceResourceType = res.Type
		mappingResult.SourceResourceName = res.Name
		mappingResult.SourceCategory = resource.Category(res.Category)

		mappingResults = append(mappingResults, mappingResult)
	}

	if len(mappingResults) == 0 {
		return nil, fmt.Errorf("no resources could be mapped")
	}

	result.MappingResults = mappingResults

	// Step 2: Consolidate if enabled
	if config.Consolidate {
		return s.generateWithConsolidation(ctx, mappingResults, config, result)
	}

	// Non-consolidated path: use legacy generation
	return s.generateWithoutConsolidation(ctx, mappingResults, config, result)
}

// generateWithConsolidation performs consolidated stack generation.
func (s *Service) generateWithConsolidation(
	ctx context.Context,
	mappingResults []*mapper.MappingResult,
	config *MigrateConfig,
	result *MigrateResult,
) (*MigrateResult, error) {
	// Build consolidation options
	mergeOpts := &consolidator.MergeOptions{
		DatabaseEngine:         "postgres",
		MessagingBroker:        "rabbitmq",
		IncludeSupportServices: true,
	}

	if config.ConsolidationOptions != nil {
		if config.ConsolidationOptions.DatabaseEngine != "" {
			mergeOpts.DatabaseEngine = config.ConsolidationOptions.DatabaseEngine
		}
		if config.ConsolidationOptions.MessagingBroker != "" {
			mergeOpts.MessagingBroker = config.ConsolidationOptions.MessagingBroker
		}
		if config.ConsolidationOptions.NamePrefix != "" {
			mergeOpts.NamePrefix = config.ConsolidationOptions.NamePrefix
		}
		if len(config.ConsolidationOptions.EnabledStacks) > 0 {
			mergeOpts.EnabledStacks = config.ConsolidationOptions.EnabledStacks
		}
		mergeOpts.IncludeSupportServices = config.ConsolidationOptions.IncludeSupportServices
	}

	// Consolidate mapping results into stacks
	consolidatedResult, err := s.consolidator.Consolidate(ctx, mappingResults, mergeOpts)
	if err != nil {
		return nil, fmt.Errorf("consolidation failed: %w", err)
	}

	result.ConsolidatedResult = consolidatedResult
	result.Metadata = consolidatedResult.Metadata

	// Copy warnings from consolidation
	result.Warnings = append(result.Warnings, consolidatedResult.Warnings...)
	result.ManualSteps = append(result.ManualSteps, consolidatedResult.ManualSteps...)

	// Step 3: Generate deployment artifacts using stack generator
	stackGen, err := generator.GetStackGenerator(config.Platform)
	if err != nil {
		return nil, fmt.Errorf("failed to get stack generator for platform %s: %w", config.Platform, err)
	}

	// Build target config
	targetConfig := generator.NewTargetConfig(config.Platform).
		WithHALevel(config.HALevel).
		WithProjectName(config.ProjectName).
		WithBaseURL(config.Domain).
		WithSSL(config.SSLEnabled).
		WithMonitoring(config.IncludeMonitoring).
		WithBackups(config.IncludeBackups)

	// Generate output from stacks
	output, err := stackGen.GenerateFromStacks(ctx, consolidatedResult, targetConfig)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	result.Output = output
	result.Summary = output.Summary

	// Copy additional warnings and manual steps from output
	result.Warnings = append(result.Warnings, output.Warnings...)
	result.ManualSteps = append(result.ManualSteps, output.ManualSteps...)

	return result, nil
}

// generateWithoutConsolidation generates without stack consolidation (legacy behavior).
func (s *Service) generateWithoutConsolidation(
	ctx context.Context,
	mappingResults []*mapper.MappingResult,
	config *MigrateConfig,
	result *MigrateResult,
) (*MigrateResult, error) {
	// Get the target generator
	gen, err := generator.GetGenerator(config.Platform)
	if err != nil {
		return nil, fmt.Errorf("failed to get generator for platform %s: %w", config.Platform, err)
	}

	// Build target config
	targetConfig := generator.NewTargetConfig(config.Platform).
		WithHALevel(config.HALevel).
		WithProjectName(config.ProjectName).
		WithBaseURL(config.Domain).
		WithSSL(config.SSLEnabled).
		WithMonitoring(config.IncludeMonitoring).
		WithBackups(config.IncludeBackups)

	// Generate output
	output, err := gen.Generate(ctx, mappingResults, targetConfig)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	result.Output = output
	result.Summary = output.Summary

	// Copy warnings and manual steps
	result.Warnings = append(result.Warnings, output.Warnings...)
	result.ManualSteps = append(result.ManualSteps, output.ManualSteps...)

	return result, nil
}

// GenerateConsolidatedZip creates a zip file containing all consolidated migration artifacts.
func (s *Service) GenerateConsolidatedZip(ctx context.Context, resources []ResourceInfo, config *MigrateConfig) ([]byte, error) {
	result, err := s.GenerateConsolidated(ctx, resources, config)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Add all generated files to the zip
	for filename, content := range result.Output.Files {
		w, err := zw.Create(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s in zip: %w", filename, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	// Add Docker files
	for filename, content := range result.Output.DockerFiles {
		if _, exists := result.Output.Files[filename]; exists {
			continue // Already added
		}
		w, err := zw.Create(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s in zip: %w", filename, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	// Add scripts
	for filename, content := range result.Output.Scripts {
		w, err := zw.Create("scripts/" + filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create script %s in zip: %w", filename, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write script %s: %w", filename, err)
		}
	}

	// Add configs
	for filename, content := range result.Output.Configs {
		w, err := zw.Create("config/" + filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create config %s in zip: %w", filename, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write config %s: %w", filename, err)
		}
	}

	// Add docs
	for filename, content := range result.Output.Docs {
		w, err := zw.Create("docs/" + filename)
		if err != nil {
			return nil, fmt.Errorf("failed to create doc %s in zip: %w", filename, err)
		}
		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write doc %s: %w", filename, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip: %w", err)
	}

	return buf.Bytes(), nil
}

// resourceInfoToAWSResource converts a ResourceInfo to an AWSResource for mapping.
func resourceInfoToAWSResource(info *ResourceInfo) *resource.AWSResource {
	return &resource.AWSResource{
		ID:           info.ID,
		Name:         info.Name,
		Type:         resource.Type(info.Type),
		ARN:          info.ARN,
		Region:       info.Region,
		Dependencies: info.Dependencies,
		Tags:         info.Tags,
	}
}

// GetConsolidator returns the service's consolidator for advanced configuration.
func (s *Service) GetConsolidator() *consolidator.Consolidator {
	return s.consolidator
}

// SetConsolidator replaces the service's consolidator.
func (s *Service) SetConsolidator(c *consolidator.Consolidator) {
	s.consolidator = c
}

// TerraformExportConfig configures Terraform export.
type TerraformExportConfig struct {
	Provider    string `json:"provider"`
	ProjectName string `json:"project_name"`
	Domain      string `json:"domain"`
	Region      string `json:"region"`
}

// GenerateTerraformZip creates a ZIP file with Terraform configuration for a cloud provider.
func (s *Service) GenerateTerraformZip(ctx context.Context, resources []ResourceInfo, config *TerraformExportConfig) ([]byte, error) {
	if config == nil {
		config = &TerraformExportConfig{
			Provider:    "hetzner",
			ProjectName: "homeport",
			Domain:      "example.com",
			Region:      "fsn1",
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Generate main.tf with provider configuration
	mainTf := generateMainTf(config)
	if w, err := zw.Create("terraform/main.tf"); err == nil {
		w.Write([]byte(mainTf))
	}

	// Generate variables.tf
	variablesTf := generateVariablesTf(config)
	if w, err := zw.Create("terraform/variables.tf"); err == nil {
		w.Write([]byte(variablesTf))
	}

	// Generate terraform.tfvars.example
	tfvarsExample := generateTfvarsExample(config)
	if w, err := zw.Create("terraform/terraform.tfvars.example"); err == nil {
		w.Write([]byte(tfvarsExample))
	}

	// Generate networking.tf
	networkingTf := generateNetworkingTf(config, resources)
	if w, err := zw.Create("terraform/networking.tf"); err == nil {
		w.Write([]byte(networkingTf))
	}

	// Generate compute.tf
	computeTf := generateComputeTf(config, resources)
	if w, err := zw.Create("terraform/compute.tf"); err == nil {
		w.Write([]byte(computeTf))
	}

	// Generate storage.tf
	storageTf := generateStorageTf(config, resources)
	if w, err := zw.Create("terraform/storage.tf"); err == nil {
		w.Write([]byte(storageTf))
	}

	// Generate outputs.tf
	outputsTf := generateOutputsTf(config)
	if w, err := zw.Create("terraform/outputs.tf"); err == nil {
		w.Write([]byte(outputsTf))
	}

	// Generate deploy.sh
	deployScript := generateDeployScript(config)
	if w, err := zw.Create("terraform/deploy.sh"); err == nil {
		w.Write([]byte(deployScript))
	}

	// Generate README.md
	readme := generateTerraformReadme(config)
	if w, err := zw.Create("terraform/README.md"); err == nil {
		w.Write([]byte(readme))
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip: %w", err)
	}

	return buf.Bytes(), nil
}

func generateMainTf(config *TerraformExportConfig) string {
	templates := map[string]string{
		"hetzner": `# Homeport Terraform Configuration for Hetzner Cloud
# Generated by Homeport - Cloud Migration Platform

terraform {
  required_version = ">= 1.0"
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.45"
    }
  }
}

provider "hcloud" {
  token = var.hcloud_token
}
`,
		"scaleway": `# Homeport Terraform Configuration for Scaleway
# Generated by Homeport - Cloud Migration Platform

terraform {
  required_version = ">= 1.0"
  required_providers {
    scaleway = {
      source  = "scaleway/scaleway"
      version = "~> 2.40"
    }
  }
}

provider "scaleway" {
  access_key = var.scw_access_key
  secret_key = var.scw_secret_key
  project_id = var.scw_project_id
  region     = var.region
  zone       = var.zone
}
`,
		"ovh": `# Homeport Terraform Configuration for OVH Cloud
# Generated by Homeport - Cloud Migration Platform

terraform {
  required_version = ">= 1.0"
  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = "~> 0.40"
    }
    openstack = {
      source  = "terraform-provider-openstack/openstack"
      version = "~> 1.54"
    }
  }
}

provider "ovh" {
  endpoint           = "ovh-eu"
  application_key    = var.ovh_application_key
  application_secret = var.ovh_application_secret
  consumer_key       = var.ovh_consumer_key
}

provider "openstack" {
  auth_url    = "https://auth.cloud.ovh.net/v3"
  domain_name = "Default"
  user_name   = var.openstack_username
  password    = var.openstack_password
  tenant_id   = var.openstack_tenant_id
  region      = var.region
}
`,
	}

	if tmpl, ok := templates[config.Provider]; ok {
		return tmpl
	}
	return templates["hetzner"]
}

func generateVariablesTf(config *TerraformExportConfig) string {
	templates := map[string]string{
		"hetzner": `# Variables for Hetzner Cloud deployment
# Copy terraform.tfvars.example to terraform.tfvars and fill in values

variable "hcloud_token" {
  description = "Hetzner Cloud API token"
  type        = string
  sensitive   = true
}

variable "project_name" {
  description = "Project name for resource naming"
  type        = string
  default     = "%s"
}

variable "domain" {
  description = "Domain name for the deployment"
  type        = string
  default     = "%s"
}

variable "location" {
  description = "Hetzner datacenter location"
  type        = string
  default     = "%s"
}

variable "server_type" {
  description = "Hetzner server type"
  type        = string
  default     = "cx21"
}

variable "ssh_keys" {
  description = "List of SSH key names to add to servers"
  type        = list(string)
  default     = []
}
`,
		"scaleway": `# Variables for Scaleway deployment
# Copy terraform.tfvars.example to terraform.tfvars and fill in values

variable "scw_access_key" {
  description = "Scaleway access key"
  type        = string
  sensitive   = true
}

variable "scw_secret_key" {
  description = "Scaleway secret key"
  type        = string
  sensitive   = true
}

variable "scw_project_id" {
  description = "Scaleway project ID"
  type        = string
}

variable "project_name" {
  description = "Project name for resource naming"
  type        = string
  default     = "%s"
}

variable "domain" {
  description = "Domain name for the deployment"
  type        = string
  default     = "%s"
}

variable "region" {
  description = "Scaleway region"
  type        = string
  default     = "%s"
}

variable "zone" {
  description = "Scaleway zone"
  type        = string
  default     = "%s-1"
}

variable "instance_type" {
  description = "Scaleway instance type"
  type        = string
  default     = "DEV1-S"
}
`,
		"ovh": `# Variables for OVH Cloud deployment
# Copy terraform.tfvars.example to terraform.tfvars and fill in values

variable "ovh_application_key" {
  description = "OVH application key"
  type        = string
  sensitive   = true
}

variable "ovh_application_secret" {
  description = "OVH application secret"
  type        = string
  sensitive   = true
}

variable "ovh_consumer_key" {
  description = "OVH consumer key"
  type        = string
  sensitive   = true
}

variable "openstack_username" {
  description = "OpenStack username"
  type        = string
}

variable "openstack_password" {
  description = "OpenStack password"
  type        = string
  sensitive   = true
}

variable "openstack_tenant_id" {
  description = "OpenStack tenant/project ID"
  type        = string
}

variable "project_name" {
  description = "Project name for resource naming"
  type        = string
  default     = "%s"
}

variable "domain" {
  description = "Domain name for the deployment"
  type        = string
  default     = "%s"
}

variable "region" {
  description = "OVH region"
  type        = string
  default     = "%s"
}

variable "flavor_name" {
  description = "OVH instance flavor"
  type        = string
  default     = "s1-2"
}
`,
	}

	tmpl := templates[config.Provider]
	if tmpl == "" {
		tmpl = templates["hetzner"]
	}

	region := config.Region
	if region == "" {
		region = "fsn1"
	}

	return fmt.Sprintf(tmpl, config.ProjectName, config.Domain, region, region)
}

func generateTfvarsExample(config *TerraformExportConfig) string {
	templates := map[string]string{
		"hetzner": `# Hetzner Cloud Configuration
# Copy this file to terraform.tfvars and fill in your values

hcloud_token = "YOUR_HETZNER_API_TOKEN"

project_name = "%s"
domain       = "%s"
location     = "%s"
server_type  = "cx21"

ssh_keys = ["your-ssh-key-name"]
`,
		"scaleway": `# Scaleway Configuration
# Copy this file to terraform.tfvars and fill in your values

scw_access_key = "YOUR_SCALEWAY_ACCESS_KEY"
scw_secret_key = "YOUR_SCALEWAY_SECRET_KEY"
scw_project_id = "YOUR_SCALEWAY_PROJECT_ID"

project_name  = "%s"
domain        = "%s"
region        = "%s"
zone          = "%s-1"
instance_type = "DEV1-S"
`,
		"ovh": `# OVH Cloud Configuration
# Copy this file to terraform.tfvars and fill in your values

ovh_application_key    = "YOUR_OVH_APPLICATION_KEY"
ovh_application_secret = "YOUR_OVH_APPLICATION_SECRET"
ovh_consumer_key       = "YOUR_OVH_CONSUMER_KEY"

openstack_username  = "YOUR_OPENSTACK_USERNAME"
openstack_password  = "YOUR_OPENSTACK_PASSWORD"
openstack_tenant_id = "YOUR_OPENSTACK_TENANT_ID"

project_name = "%s"
domain       = "%s"
region       = "%s"
flavor_name  = "s1-2"
`,
	}

	tmpl := templates[config.Provider]
	if tmpl == "" {
		tmpl = templates["hetzner"]
	}

	region := config.Region
	if region == "" {
		region = "fsn1"
	}

	return fmt.Sprintf(tmpl, config.ProjectName, config.Domain, region, region)
}

func generateNetworkingTf(config *TerraformExportConfig, resources []ResourceInfo) string {
	templates := map[string]string{
		"hetzner": `# Networking Resources for Hetzner Cloud

resource "hcloud_network" "main" {
  name     = "${var.project_name}-network"
  ip_range = "10.0.0.0/16"
}

resource "hcloud_network_subnet" "main" {
  network_id   = hcloud_network.main.id
  type         = "cloud"
  network_zone = "eu-central"
  ip_range     = "10.0.1.0/24"
}

resource "hcloud_firewall" "main" {
  name = "${var.project_name}-firewall"

  rule {
    direction = "in"
    protocol  = "tcp"
    port      = "22"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  rule {
    direction = "in"
    protocol  = "tcp"
    port      = "80"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  rule {
    direction = "in"
    protocol  = "tcp"
    port      = "443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
}
`,
		"scaleway": `# Networking Resources for Scaleway

resource "scaleway_vpc_private_network" "main" {
  name = "${var.project_name}-network"
}

resource "scaleway_instance_security_group" "main" {
  name                    = "${var.project_name}-sg"
  inbound_default_policy  = "drop"
  outbound_default_policy = "accept"

  inbound_rule {
    action = "accept"
    port   = 22
  }

  inbound_rule {
    action = "accept"
    port   = 80
  }

  inbound_rule {
    action = "accept"
    port   = 443
  }
}
`,
		"ovh": `# Networking Resources for OVH Cloud

resource "openstack_networking_network_v2" "main" {
  name           = "${var.project_name}-network"
  admin_state_up = true
}

resource "openstack_networking_subnet_v2" "main" {
  name       = "${var.project_name}-subnet"
  network_id = openstack_networking_network_v2.main.id
  cidr       = "10.0.1.0/24"
  ip_version = 4
}

resource "openstack_networking_secgroup_v2" "main" {
  name        = "${var.project_name}-secgroup"
  description = "Security group for ${var.project_name}"
}

resource "openstack_networking_secgroup_rule_v2" "ssh" {
  direction         = "ingress"
  ethertype         = "IPv4"
  protocol          = "tcp"
  port_range_min    = 22
  port_range_max    = 22
  remote_ip_prefix  = "0.0.0.0/0"
  security_group_id = openstack_networking_secgroup_v2.main.id
}

resource "openstack_networking_secgroup_rule_v2" "http" {
  direction         = "ingress"
  ethertype         = "IPv4"
  protocol          = "tcp"
  port_range_min    = 80
  port_range_max    = 80
  remote_ip_prefix  = "0.0.0.0/0"
  security_group_id = openstack_networking_secgroup_v2.main.id
}

resource "openstack_networking_secgroup_rule_v2" "https" {
  direction         = "ingress"
  ethertype         = "IPv4"
  protocol          = "tcp"
  port_range_min    = 443
  port_range_max    = 443
  remote_ip_prefix  = "0.0.0.0/0"
  security_group_id = openstack_networking_secgroup_v2.main.id
}
`,
	}

	if tmpl, ok := templates[config.Provider]; ok {
		return tmpl
	}
	return templates["hetzner"]
}

func generateComputeTf(config *TerraformExportConfig, resources []ResourceInfo) string {
	// Count compute resources
	computeCount := 0
	for _, r := range resources {
		if r.Category == "compute" {
			computeCount++
		}
	}
	if computeCount == 0 {
		computeCount = 1
	}

	templates := map[string]string{
		"hetzner": `# Compute Resources for Hetzner Cloud

data "hcloud_ssh_keys" "all" {}

resource "hcloud_server" "app" {
  count       = %d
  name        = "${var.project_name}-app-${count.index + 1}"
  server_type = var.server_type
  image       = "docker-ce"
  location    = var.location
  ssh_keys    = length(var.ssh_keys) > 0 ? var.ssh_keys : data.hcloud_ssh_keys.all.ssh_keys[*].name

  network {
    network_id = hcloud_network.main.id
  }

  firewall_ids = [hcloud_firewall.main.id]

  user_data = <<-EOF
    #cloud-config
    packages:
      - docker-compose
    runcmd:
      - systemctl enable docker
      - systemctl start docker
  EOF

  depends_on = [hcloud_network_subnet.main]
}
`,
		"scaleway": `# Compute Resources for Scaleway

resource "scaleway_instance_server" "app" {
  count = %d
  name  = "${var.project_name}-app-${count.index + 1}"
  type  = var.instance_type
  image = "docker"

  security_group_id = scaleway_instance_security_group.main.id

  private_network {
    pn_id = scaleway_vpc_private_network.main.id
  }

  user_data = {
    cloud-init = <<-EOF
      #cloud-config
      packages:
        - docker-compose
      runcmd:
        - systemctl enable docker
        - systemctl start docker
    EOF
  }
}
`,
		"ovh": `# Compute Resources for OVH Cloud

data "openstack_images_image_v2" "docker" {
  name        = "Docker"
  most_recent = true
}

resource "openstack_compute_instance_v2" "app" {
  count           = %d
  name            = "${var.project_name}-app-${count.index + 1}"
  flavor_name     = var.flavor_name
  image_id        = data.openstack_images_image_v2.docker.id
  security_groups = [openstack_networking_secgroup_v2.main.name]

  network {
    name = openstack_networking_network_v2.main.name
  }

  user_data = <<-EOF
    #cloud-config
    packages:
      - docker-compose
    runcmd:
      - systemctl enable docker
      - systemctl start docker
  EOF
}
`,
	}

	tmpl := templates[config.Provider]
	if tmpl == "" {
		tmpl = templates["hetzner"]
	}

	return fmt.Sprintf(tmpl, computeCount)
}

func generateStorageTf(config *TerraformExportConfig, resources []ResourceInfo) string {
	// Count storage resources
	storageCount := 0
	for _, r := range resources {
		if r.Category == "storage" {
			storageCount++
		}
	}
	if storageCount == 0 {
		storageCount = 1
	}

	templates := map[string]string{
		"hetzner": `# Storage Resources for Hetzner Cloud

resource "hcloud_volume" "data" {
  count    = %d
  name     = "${var.project_name}-data-${count.index + 1}"
  size     = 50
  location = var.location
  format   = "ext4"
}

resource "hcloud_volume_attachment" "data" {
  count     = %d
  volume_id = hcloud_volume.data[count.index].id
  server_id = hcloud_server.app[count.index %% length(hcloud_server.app)].id
  automount = true
}
`,
		"scaleway": `# Storage Resources for Scaleway

resource "scaleway_instance_volume" "data" {
  count      = %d
  name       = "${var.project_name}-data-${count.index + 1}"
  size_in_gb = 50
  type       = "b_ssd"
}

resource "scaleway_instance_server" "app_volume_attachment" {
  count = %d
  # Note: Attach volumes through additional_volume_ids in the server resource
}
`,
		"ovh": `# Storage Resources for OVH Cloud

resource "openstack_blockstorage_volume_v3" "data" {
  count       = %d
  name        = "${var.project_name}-data-${count.index + 1}"
  size        = 50
  description = "Data volume for ${var.project_name}"
}

resource "openstack_compute_volume_attach_v2" "data" {
  count       = %d
  instance_id = openstack_compute_instance_v2.app[count.index %% length(openstack_compute_instance_v2.app)].id
  volume_id   = openstack_blockstorage_volume_v3.data[count.index].id
}
`,
	}

	tmpl := templates[config.Provider]
	if tmpl == "" {
		tmpl = templates["hetzner"]
	}

	return fmt.Sprintf(tmpl, storageCount, storageCount)
}

func generateOutputsTf(config *TerraformExportConfig) string {
	templates := map[string]string{
		"hetzner": `# Outputs for Hetzner Cloud deployment

output "server_ips" {
  description = "Public IP addresses of the servers"
  value       = hcloud_server.app[*].ipv4_address
}

output "server_names" {
  description = "Names of the servers"
  value       = hcloud_server.app[*].name
}

output "network_id" {
  description = "ID of the private network"
  value       = hcloud_network.main.id
}

output "volume_ids" {
  description = "IDs of the data volumes"
  value       = hcloud_volume.data[*].id
}

output "app_url" {
  description = "URL to access the application"
  value       = "https://${var.domain}"
}

output "ssh_command" {
  description = "SSH command to connect to the first server"
  value       = "ssh root@${hcloud_server.app[0].ipv4_address}"
}
`,
		"scaleway": `# Outputs for Scaleway deployment

output "server_ips" {
  description = "Public IP addresses of the servers"
  value       = scaleway_instance_server.app[*].public_ip
}

output "server_names" {
  description = "Names of the servers"
  value       = scaleway_instance_server.app[*].name
}

output "private_network_id" {
  description = "ID of the private network"
  value       = scaleway_vpc_private_network.main.id
}

output "app_url" {
  description = "URL to access the application"
  value       = "https://${var.domain}"
}

output "ssh_command" {
  description = "SSH command to connect to the first server"
  value       = "ssh root@${scaleway_instance_server.app[0].public_ip}"
}
`,
		"ovh": `# Outputs for OVH Cloud deployment

output "server_ips" {
  description = "IP addresses of the servers"
  value       = openstack_compute_instance_v2.app[*].access_ip_v4
}

output "server_names" {
  description = "Names of the servers"
  value       = openstack_compute_instance_v2.app[*].name
}

output "network_id" {
  description = "ID of the private network"
  value       = openstack_networking_network_v2.main.id
}

output "volume_ids" {
  description = "IDs of the data volumes"
  value       = openstack_blockstorage_volume_v3.data[*].id
}

output "app_url" {
  description = "URL to access the application"
  value       = "https://${var.domain}"
}

output "ssh_command" {
  description = "SSH command to connect to the first server"
  value       = "ssh root@${openstack_compute_instance_v2.app[0].access_ip_v4}"
}
`,
	}

	if tmpl, ok := templates[config.Provider]; ok {
		return tmpl
	}
	return templates["hetzner"]
}

func generateDeployScript(config *TerraformExportConfig) string {
	return fmt.Sprintf(`#!/bin/bash
# Homeport Terraform Deployment Script
# Provider: %s
# Generated by Homeport - Cloud Migration Platform

set -e

echo "==================================="
echo "  Homeport Terraform Deployment"
echo "  Provider: %s"
echo "==================================="
echo

# Check for terraform
if ! command -v terraform &> /dev/null; then
    echo "Error: terraform is not installed"
    echo "Please install Terraform: https://www.terraform.io/downloads"
    exit 1
fi

# Check for tfvars file
if [ ! -f terraform.tfvars ]; then
    echo "Error: terraform.tfvars not found"
    echo "Please copy terraform.tfvars.example to terraform.tfvars and fill in your credentials"
    exit 1
fi

echo "Step 1: Initializing Terraform..."
terraform init

echo
echo "Step 2: Planning deployment..."
terraform plan -out=tfplan

echo
read -p "Do you want to apply this plan? (yes/no): " confirm
if [ "$confirm" != "yes" ]; then
    echo "Deployment cancelled"
    exit 0
fi

echo
echo "Step 3: Applying deployment..."
terraform apply tfplan

echo
echo "==================================="
echo "  Deployment Complete!"
echo "==================================="
echo
terraform output

echo
echo "Next steps:"
echo "1. SSH into your server using the command above"
echo "2. Deploy your Docker Compose stack"
echo "3. Configure your DNS to point to the server IP"
`, config.Provider, strings.ToUpper(config.Provider))
}

func generateTerraformReadme(config *TerraformExportConfig) string {
	providerDocs := map[string]string{
		"hetzner":  "https://registry.terraform.io/providers/hetznercloud/hcloud/latest/docs",
		"scaleway": "https://registry.terraform.io/providers/scaleway/scaleway/latest/docs",
		"ovh":      "https://registry.terraform.io/providers/ovh/ovh/latest/docs",
	}

	doc := providerDocs[config.Provider]
	if doc == "" {
		doc = providerDocs["hetzner"]
	}

	return fmt.Sprintf(`# Homeport Terraform Configuration

This Terraform configuration deploys your migrated infrastructure to **%s**.

## Prerequisites

1. **Terraform** (>= 1.0) - [Install Guide](https://www.terraform.io/downloads)
2. **%s Account** with API credentials
3. **SSH Key** configured in your %s account

## Quick Start

1. **Configure credentials:**
   `+"```bash"+`
   cp terraform.tfvars.example terraform.tfvars
   # Edit terraform.tfvars with your credentials
   `+"```"+`

2. **Deploy infrastructure:**
   `+"```bash"+`
   chmod +x deploy.sh
   ./deploy.sh
   `+"```"+`

   Or manually:
   `+"```bash"+`
   terraform init
   terraform plan
   terraform apply
   `+"```"+`

3. **Access your servers:**
   After deployment, Terraform will output SSH commands to connect to your servers.

## Files

| File | Description |
|------|-------------|
| main.tf | Provider configuration |
| variables.tf | Input variables |
| terraform.tfvars.example | Example variable values |
| networking.tf | VPC, subnets, firewalls |
| compute.tf | Server instances |
| storage.tf | Volumes and attachments |
| outputs.tf | Output values |
| deploy.sh | Deployment script |

## Customization

Edit `+"`variables.tf`"+` to change:
- Server types/sizes
- Number of instances
- Storage sizes
- Network configuration

## Documentation

- [%s Provider Docs](%s)
- [Terraform Documentation](https://www.terraform.io/docs)

## Cost Estimate

%s offers competitive EU-based pricing:
- Servers from ~€3-5/month
- Storage from ~€0.05/GB/month
- Outbound traffic included or low-cost

## Support

Generated by [Homeport](https://github.com/homeport/homeport) - Cloud Migration Platform
`, strings.Title(config.Provider), strings.Title(config.Provider), strings.Title(config.Provider),
		strings.Title(config.Provider), doc, strings.Title(config.Provider))
}
