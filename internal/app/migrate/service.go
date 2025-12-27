package migrate

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
	infraMapper "github.com/agnostech/agnostech/internal/infrastructure/mapper"
	"github.com/agnostech/agnostech/internal/infrastructure/parser/aws"
)

// Service handles migration analysis and generation.
type Service struct {
	registry   *infraMapper.Registry
	stateStore *StateStore
}

// NewService creates a new migration service.
func NewService() *Service {
	return &Service{
		registry: infraMapper.GlobalRegistry,
	}
}

// NewServiceWithState creates a migration service with state persistence.
func NewServiceWithState(statePath string) (*Service, error) {
	store, err := NewStateStore(statePath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state store: %w", err)
	}

	return &Service{
		registry:   infraMapper.GlobalRegistry,
		stateStore: store,
	}, nil
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
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Category     string            `json:"category"`
	ARN          string            `json:"arn,omitempty"`
	Region       string            `json:"region,omitempty"`
	Dependencies []string          `json:"dependencies"`
	Tags         map[string]string `json:"tags,omitempty"`
}

// Analyze parses infrastructure files and returns discovered resources.
func (s *Service) Analyze(ctx context.Context, req AnalyzeRequest) (*AnalyzeResponse, error) {
	// Write content to temp file
	tmpDir, err := os.MkdirTemp("", "agnostech-analyze-*")
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
	// Build credentials map based on provider
	creds := make(map[string]string)

	var provider resource.Provider
	switch req.Provider {
	case "aws":
		provider = resource.ProviderAWS
		creds["access_key_id"] = req.AccessKeyID
		creds["secret_access_key"] = req.SecretAccessKey
		// Don't set region - let the parser discover all regions
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

	// Build regions list - if empty, parser will scan all regions
	regions := req.Regions
	if len(regions) == 0 && req.Region != "" {
		regions = []string{req.Region}
	}

	// Use parser registry to discover
	opts := parser.NewParseOptions().
		WithIgnoreErrors(true).
		WithCredentials(creds).
		WithRegions(regions...)

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
	buf.WriteString("# Generated by AgnosTech\n\n")

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
			if r.DockerService.Build != "" {
				buf.WriteString(fmt.Sprintf("    build: %s\n", r.DockerService.Build))
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
	startup.WriteString("echo 'Starting AgnosTech stack...'\n")
	startup.WriteString("docker compose up -d\n")
	startup.WriteString("echo 'Stack started successfully!'\n")
	scripts["start.sh"] = startup.String()

	// Generate shutdown script
	var shutdown bytes.Buffer
	shutdown.WriteString("#!/bin/bash\nset -e\n\n")
	shutdown.WriteString("echo 'Stopping AgnosTech stack...'\n")
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
	buf.WriteString("# Generated AgnosTech Stack\n\n")
	buf.WriteString("This stack was generated by AgnosTech to replace your cloud infrastructure.\n\n")

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
