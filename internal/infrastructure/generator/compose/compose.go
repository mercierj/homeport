// Package compose generates Docker Compose configurations from mapping results.
package compose

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/domain/target"
)

// Generator generates Docker Compose files.
// It implements both generator.TargetGenerator and generator.StackGenerator interfaces.
type Generator struct {
	projectName string
	network     *NetworkConfig
}

// NewGenerator creates a new Docker Compose generator.
func NewGenerator(projectName string) *Generator {
	return &Generator{
		projectName: projectName,
		network:     NewNetworkConfig(),
	}
}

// New creates a new Docker Compose generator with default project name.
func New() *Generator {
	return NewGenerator("homeport")
}

// ─────────────────────────────────────────────────────────────────────────────
// TargetGenerator Interface Implementation
// ─────────────────────────────────────────────────────────────────────────────

// Platform returns the target platform this generator handles.
func (g *Generator) Platform() target.Platform {
	return target.PlatformDockerCompose
}

// Name returns the name of this generator.
func (g *Generator) Name() string {
	return "docker-compose"
}

// Description returns a human-readable description.
func (g *Generator) Description() string {
	return "Generates Docker Compose files for single-server deployments"
}

// SupportedHALevels returns the HA levels this generator supports.
func (g *Generator) SupportedHALevels() []target.HALevel {
	return target.SupportedHALevelsForPlatform(target.PlatformDockerCompose)
}

// RequiresCredentials returns true if the platform needs cloud credentials.
func (g *Generator) RequiresCredentials() bool {
	return false
}

// RequiredCredentials returns the list of required credential keys.
func (g *Generator) RequiredCredentials() []string {
	return nil
}

// Validate checks if the mapping results can be deployed to Docker Compose.
func (g *Generator) Validate(results []*mapper.MappingResult, config *generator.TargetConfig) error {
	if len(results) == 0 {
		return fmt.Errorf("no mapping results provided")
	}

	if config == nil {
		return fmt.Errorf("target configuration is required")
	}

	// Validate HA level is supported
	supportedLevels := g.SupportedHALevels()
	levelSupported := false
	for _, level := range supportedLevels {
		if level == config.HALevel {
			levelSupported = true
			break
		}
	}
	if !levelSupported {
		return fmt.Errorf("HA level %q is not supported by Docker Compose (supported: none, basic)", config.HALevel)
	}

	// Validate that at least one result has a Docker service
	hasService := false
	for _, result := range results {
		if result != nil && result.DockerService != nil {
			hasService = true
			break
		}
	}
	if !hasService {
		return fmt.Errorf("no Docker services found in mapping results")
	}

	return nil
}

// Generate produces Docker Compose files for the target platform.
func (g *Generator) Generate(ctx context.Context, results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.TargetOutput, error) {
	if err := g.Validate(results, config); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Set project name from config if provided
	projectName := g.projectName
	if config.ProjectName != "" {
		projectName = config.ProjectName
	}

	output := generator.NewTargetOutput(target.PlatformDockerCompose)

	// Collect all services
	services := make(map[string]*Service)
	volumes := make(map[string]*Volume)

	for _, result := range results {
		if result.DockerService != nil {
			svc := g.convertService(result.DockerService)
			services[svc.Name] = svc

			// Collect named volumes
			for _, vol := range result.DockerService.Volumes {
				if isNamedVolume(vol) {
					volName := extractVolumeName(vol)
					if _, exists := volumes[volName]; !exists {
						volumes[volName] = &Volume{
							Name:   volName,
							Driver: "local",
						}
					}
				}
			}
		}

		// Add warnings to output
		for _, warning := range result.Warnings {
			output.AddWarning(warning)
		}

		// Add manual steps
		for _, step := range result.ManualSteps {
			output.AddManualStep(step)
		}
	}

	// Build dependency graph and sort services topologically
	sorted, err := g.topologicalSort(services)
	if err != nil {
		return nil, fmt.Errorf("failed to sort services: %w", err)
	}

	// Build compose structure
	compose := &ComposeFile{
		Version:  "3.8",
		Services: make(map[string]*Service),
		Networks: g.network.GetNetworks(),
		Volumes:  volumes,
	}

	for _, name := range sorted {
		compose.Services[name] = services[name]
	}

	// Generate YAML
	content, err := g.generateYAML(compose, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to generate YAML: %w", err)
	}

	output.AddDockerFile("docker-compose.yml", []byte(content))
	output.MainFile = "docker-compose.yml"
	output.Summary = fmt.Sprintf("Generated Docker Compose with %d services", len(services))

	// Add deployment manual steps
	output.AddManualStep("Start services: docker compose up -d")
	output.AddManualStep("View logs: docker compose logs -f")
	output.AddManualStep("Stop services: docker compose down")

	return output, nil
}

// EstimateCost estimates the monthly cost for the deployment.
func (g *Generator) EstimateCost(results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.CostEstimate, error) {
	estimate := generator.NewCostEstimate("EUR")
	estimate.AddNote("Docker Compose is open source with no licensing costs")
	estimate.AddNote("Costs depend on the underlying infrastructure (server, storage)")

	switch config.HALevel {
	case target.HALevelNone:
		estimate.Compute = 10.0
		estimate.AddNote("Single server setup - minimal costs")
	case target.HALevelBasic:
		estimate.Compute = 15.0
		estimate.Storage = 5.0
		estimate.AddNote("Single server with backup storage")
	}

	estimate.Calculate()
	return estimate, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// StackGenerator Interface Implementation
// ─────────────────────────────────────────────────────────────────────────────

// ValidateStacks checks if the consolidated stacks can be processed by this generator.
func (g *Generator) ValidateStacks(stacks *stack.ConsolidatedResult, config *generator.TargetConfig) error {
	if stacks == nil {
		return fmt.Errorf("consolidated stacks is nil")
	}

	if len(stacks.Stacks) == 0 && len(stacks.Passthrough) == 0 {
		return fmt.Errorf("no stacks or passthrough resources to generate")
	}

	if config == nil {
		return fmt.Errorf("target configuration is required")
	}

	// Validate HA level is supported
	supportedLevels := g.SupportedHALevels()
	levelSupported := false
	for _, level := range supportedLevels {
		if level == config.HALevel {
			levelSupported = true
			break
		}
	}
	if !levelSupported {
		return fmt.Errorf("HA level %q is not supported by Docker Compose (supported: none, basic)", config.HALevel)
	}

	return nil
}

// GenerateFromStacks produces output artifacts from consolidated stacks.
// This method takes the output from the consolidator and generates
// a single docker-compose.yml that combines all services from all stacks.
func (g *Generator) GenerateFromStacks(ctx context.Context, stacks *stack.ConsolidatedResult, config *generator.TargetConfig) (*generator.TargetOutput, error) {
	if err := g.ValidateStacks(stacks, config); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Set project name from config if provided
	projectName := g.projectName
	if config.ProjectName != "" {
		projectName = config.ProjectName
	}

	output := generator.NewTargetOutput(target.PlatformDockerCompose)

	// Collect all services and volumes from all stacks
	services := make(map[string]*Service)
	volumes := make(map[string]*Volume)
	networks := make(map[string]*Network)

	// Start with default networks
	for name, net := range g.network.GetNetworks() {
		networks[name] = net
	}

	// Process each stack in dependency order
	orderedStacks := g.orderStacksByDependency(stacks.Stacks)

	for _, stk := range orderedStacks {
		// Convert stack services to compose services
		for _, svc := range stk.Services {
			composeSvc := g.convertStackService(svc, stk.Type)
			services[composeSvc.Name] = composeSvc
		}

		// Collect volumes from stack
		for _, vol := range stk.Volumes {
			if _, exists := volumes[vol.Name]; !exists {
				volumes[vol.Name] = g.convertStackVolume(vol)
			}
		}

		// Collect networks from stack
		for _, net := range stk.Networks {
			if _, exists := networks[net.Name]; !exists {
				networks[net.Name] = g.convertStackNetwork(net)
			}
		}

		// Add stack configs and scripts to output
		for name, content := range stk.Configs {
			output.AddConfig(name, content)
		}
		for name, content := range stk.Scripts {
			output.AddScript(name, content)
		}
	}

	// Handle passthrough resources (VMs, EKS clusters, etc.)
	for _, res := range stacks.Passthrough {
		output.AddWarning(fmt.Sprintf("Passthrough resource '%s' (%s) requires manual handling", res.Name, res.Type))
		output.AddManualStep(fmt.Sprintf("Configure passthrough resource: %s (%s)", res.Name, res.Type))
	}

	// Build dependency graph and sort services topologically
	sorted, err := g.topologicalSort(services)
	if err != nil {
		return nil, fmt.Errorf("failed to sort services: %w", err)
	}

	// Build compose structure
	compose := &ComposeFile{
		Version:  "3.8",
		Services: make(map[string]*Service),
		Networks: networks,
		Volumes:  volumes,
	}

	for _, name := range sorted {
		compose.Services[name] = services[name]
	}

	// Generate YAML
	content, err := g.generateYAML(compose, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to generate YAML: %w", err)
	}

	output.AddDockerFile("docker-compose.yml", []byte(content))
	output.MainFile = "docker-compose.yml"

	// Generate .env file template
	envContent := g.generateEnvTemplate(stacks)
	if envContent != "" {
		output.AddConfig(".env.example", []byte(envContent))
	}

	// Copy warnings and manual steps from consolidation
	for _, warning := range stacks.Warnings {
		output.AddWarning(warning)
	}
	for _, step := range stacks.ManualSteps {
		output.AddManualStep(step)
	}

	// Add deployment instructions
	output.AddManualStep("Copy .env.example to .env and configure secrets")
	output.AddManualStep("Start services: docker compose up -d")
	output.AddManualStep("View logs: docker compose logs -f")
	output.AddManualStep("Stop services: docker compose down")

	// Build summary
	output.Summary = fmt.Sprintf("Generated Docker Compose from %d stacks with %d services (%d source resources consolidated)",
		len(stacks.Stacks), len(services), stacks.Metadata.TotalSourceResources)

	return output, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Legacy Generate Method (for backward compatibility)
// ─────────────────────────────────────────────────────────────────────────────

// GenerateLegacy creates a docker-compose.yml from mapping results (legacy API).
func (g *Generator) GenerateLegacy(results []*mapper.MappingResult) (*generator.Output, error) {
	output := generator.NewOutput()

	// Collect all services
	services := make(map[string]*Service)
	volumes := make(map[string]*Volume)

	for _, result := range results {
		// result.DockerService is now a single service, not a slice
		if result.DockerService != nil {
			svc := g.convertService(result.DockerService)
			services[svc.Name] = svc

			// Collect named volumes
			for _, vol := range result.DockerService.Volumes {
				if isNamedVolume(vol) {
					volName := extractVolumeName(vol)
					if _, exists := volumes[volName]; !exists {
						volumes[volName] = &Volume{
							Name:   volName,
							Driver: "local",
						}
					}
				}
			}
		}

		// Add warnings to output
		for _, warning := range result.Warnings {
			output.AddWarning(warning)
		}
	}

	// Build dependency graph and sort services topologically
	sorted, err := g.topologicalSort(services)
	if err != nil {
		return nil, fmt.Errorf("failed to sort services: %w", err)
	}

	// Build compose structure
	compose := &ComposeFile{
		Version:  "3.8",
		Services: make(map[string]*Service),
		Networks: g.network.GetNetworks(),
		Volumes:  volumes,
	}

	for _, name := range sorted {
		compose.Services[name] = services[name]
	}

	// Generate YAML
	content, err := g.generateYAML(compose, g.projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to generate YAML: %w", err)
	}

	output.AddFile("docker-compose.yml", []byte(content))
	output.AddMetadata("project_name", g.projectName)
	output.AddMetadata("services_count", fmt.Sprintf("%d", len(services)))
	output.AddMetadata("generated_at", time.Now().Format(time.RFC3339))

	return output, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper Methods
// ─────────────────────────────────────────────────────────────────────────────

// orderStacksByDependency orders stacks so dependencies come first.
func (g *Generator) orderStacksByDependency(stacks []*stack.Stack) []*stack.Stack {
	// Build a map for quick lookup
	stackMap := make(map[stack.StackType]*stack.Stack)
	for _, stk := range stacks {
		stackMap[stk.Type] = stk
	}

	// Build adjacency list
	graph := make(map[stack.StackType][]stack.StackType)
	inDegree := make(map[stack.StackType]int)

	for _, stk := range stacks {
		graph[stk.Type] = make([]stack.StackType, 0)
		inDegree[stk.Type] = 0
	}

	// Add edges based on dependencies
	for _, stk := range stacks {
		for _, dep := range stk.DependsOn {
			if _, exists := stackMap[dep]; exists {
				graph[dep] = append(graph[dep], stk.Type)
				inDegree[stk.Type]++
			}
		}
	}

	// Kahn's algorithm for topological sort
	var queue []stack.StackType
	for stackType, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, stackType)
		}
	}

	var ordered []*stack.Stack
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if stk, exists := stackMap[current]; exists {
			ordered = append(ordered, stk)
		}

		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	// If not all stacks were processed (cycle detected), just return original order
	if len(ordered) != len(stacks) {
		return stacks
	}

	return ordered
}

// convertStackService converts a stack.Service to a compose Service.
func (g *Generator) convertStackService(svc *stack.Service, stackType stack.StackType) *Service {
	composeSvc := &Service{
		Name:        svc.Name,
		Image:       svc.Image,
		Environment: make(map[string]string),
		Ports:       make([]string, len(svc.Ports)),
		Volumes:     make([]string, len(svc.Volumes)),
		Networks:    make([]string, 0),
		DependsOn:   make([]string, len(svc.DependsOn)),
		Command:     make([]string, len(svc.Command)),
		Labels:      make(map[string]string),
		Restart:     svc.Restart,
	}

	// Copy environment
	for k, v := range svc.Environment {
		composeSvc.Environment[k] = v
	}

	// Copy ports
	copy(composeSvc.Ports, svc.Ports)

	// Copy volumes
	copy(composeSvc.Volumes, svc.Volumes)

	// Copy command
	copy(composeSvc.Command, svc.Command)

	// Copy depends_on
	copy(composeSvc.DependsOn, svc.DependsOn)

	// Copy labels
	for k, v := range svc.Labels {
		composeSvc.Labels[k] = v
	}

	// Add stack type label
	composeSvc.Labels["com.homeport.stack"] = string(stackType)

	// Assign networks
	if len(svc.Networks) > 0 {
		composeSvc.Networks = svc.Networks
	} else {
		// Default: all services get internal, public-facing get web
		composeSvc.Networks = append(composeSvc.Networks, "internal")
		if len(svc.Ports) > 0 || hasTraefikLabels(svc.Labels) {
			composeSvc.Networks = append(composeSvc.Networks, "web")
		}
	}

	// Set default restart policy
	if composeSvc.Restart == "" {
		composeSvc.Restart = "unless-stopped"
	}

	// Convert health check
	if svc.HealthCheck != nil {
		composeSvc.HealthCheck = &HealthCheck{
			Test:        svc.HealthCheck.Test,
			Interval:    svc.HealthCheck.Interval,
			Timeout:     svc.HealthCheck.Timeout,
			Retries:     svc.HealthCheck.Retries,
			StartPeriod: svc.HealthCheck.StartPeriod,
		}
	}

	// Convert deploy config
	if svc.Deploy != nil {
		composeSvc.Deploy = &Deploy{}
		if svc.Deploy.Resources != nil {
			composeSvc.Deploy.Resources = &Resources{}
			if svc.Deploy.Resources.Limits != nil {
				composeSvc.Deploy.Resources.Limits = &ResourceLimits{
					CPUs:   svc.Deploy.Resources.Limits.CPUs,
					Memory: svc.Deploy.Resources.Limits.Memory,
				}
			}
			if svc.Deploy.Resources.Reservations != nil {
				composeSvc.Deploy.Resources.Reservations = &ResourceLimits{
					CPUs:   svc.Deploy.Resources.Reservations.CPUs,
					Memory: svc.Deploy.Resources.Reservations.Memory,
				}
			}
		}
	}

	return composeSvc
}

// convertStackVolume converts a stack.Volume to a compose Volume.
func (g *Generator) convertStackVolume(vol stack.Volume) *Volume {
	return &Volume{
		Name:       vol.Name,
		Driver:     vol.Driver,
		DriverOpts: vol.DriverOpts,
	}
}

// convertStackNetwork converts a stack.Network to a compose Network.
func (g *Generator) convertStackNetwork(net stack.Network) *Network {
	return &Network{
		Driver:   net.Driver,
		Internal: !net.Attachable, // If not attachable, it's internal
		Labels:   make(map[string]string),
	}
}

// generateEnvTemplate creates an .env.example file from stacks.
func (g *Generator) generateEnvTemplate(stacks *stack.ConsolidatedResult) string {
	var buf bytes.Buffer
	buf.WriteString("# Environment configuration template\n")
	buf.WriteString("# Generated by Homeport - " + time.Now().Format(time.RFC3339) + "\n")
	buf.WriteString("# Copy this file to .env and fill in the values\n\n")

	envVars := make(map[string]string)

	// Collect environment variables from all stacks
	for _, stk := range stacks.Stacks {
		buf.WriteString(fmt.Sprintf("# === %s Stack ===\n", stk.Type.DisplayName()))

		for _, svc := range stk.Services {
			for key, value := range svc.Environment {
				// Check if this is a placeholder or sensitive value
				if isSensitiveEnvVar(key) || isPlaceholderValue(value) {
					if _, exists := envVars[key]; !exists {
						envVars[key] = value
						buf.WriteString(fmt.Sprintf("%s=%s\n", key, sanitizeEnvValue(value)))
					}
				}
			}
		}
		buf.WriteString("\n")
	}

	if len(envVars) == 0 {
		return ""
	}

	return buf.String()
}

// convertService converts a mapper.DockerService to our internal Service type.
func (g *Generator) convertService(dockerSvc *mapper.DockerService) *Service {
	svc := &Service{
		Name:        dockerSvc.Name,
		Image:       dockerSvc.Image,
		Environment: dockerSvc.Environment,
		Ports:       dockerSvc.Ports,
		Volumes:     dockerSvc.Volumes,
		Networks:    g.assignNetworks(dockerSvc),
		DependsOn:   dockerSvc.DependsOn,
		Command:     dockerSvc.Command,
		Labels:      dockerSvc.Labels,
		Restart:     dockerSvc.Restart,
	}

	if svc.Restart == "" {
		svc.Restart = "unless-stopped"
	}

	// Convert build config
	if dockerSvc.Build != nil {
		svc.Build = &BuildConfig{
			Context:    dockerSvc.Build.Context,
			Dockerfile: dockerSvc.Build.Dockerfile,
		}
	}

	// Convert health check
	if dockerSvc.HealthCheck != nil {
		svc.HealthCheck = &HealthCheck{
			Test:        dockerSvc.HealthCheck.Test,
			Interval:    dockerSvc.HealthCheck.Interval.String(),
			Timeout:     dockerSvc.HealthCheck.Timeout.String(),
			Retries:     dockerSvc.HealthCheck.Retries,
			StartPeriod: "30s",
		}
	}

	// Convert resources
	if dockerSvc.Deploy != nil && dockerSvc.Deploy.Resources != nil {
		svc.Deploy = &Deploy{
			Resources: &Resources{},
		}
		if dockerSvc.Deploy.Resources.Limits != nil {
			svc.Deploy.Resources.Limits = &ResourceLimits{
				CPUs:   dockerSvc.Deploy.Resources.Limits.CPUs,
				Memory: dockerSvc.Deploy.Resources.Limits.Memory,
			}
		}
		if dockerSvc.Deploy.Resources.Reservations != nil {
			svc.Deploy.Resources.Reservations = &ResourceLimits{
				CPUs:   dockerSvc.Deploy.Resources.Reservations.CPUs,
				Memory: dockerSvc.Deploy.Resources.Reservations.Memory,
			}
		}
	}

	return svc
}

// assignNetworks assigns networks to a service based on labels and ports.
func (g *Generator) assignNetworks(svc *mapper.DockerService) []string {
	networks := []string{}

	// Check if service has public-facing labels (Traefik)
	hasPublicLabels := hasTraefikLabels(svc.Labels)

	// Check if service exposes ports
	hasPorts := len(svc.Ports) > 0

	if hasPublicLabels || hasPorts {
		networks = append(networks, "web")
	}

	// All services get the internal network
	networks = append(networks, "internal")

	return networks
}

// topologicalSort sorts services based on dependencies.
func (g *Generator) topologicalSort(services map[string]*Service) ([]string, error) {
	// Build adjacency list
	graph := make(map[string][]string)
	inDegree := make(map[string]int)

	for name := range services {
		graph[name] = []string{}
		inDegree[name] = 0
	}

	for name, svc := range services {
		for _, dep := range svc.DependsOn {
			if _, exists := services[dep]; exists {
				graph[dep] = append(graph[dep], name)
				inDegree[name]++
			}
		}
	}

	// Kahn's algorithm
	queue := []string{}
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	result := []string{}
	for len(queue) > 0 {
		// Sort queue for deterministic output
		sort.Strings(queue)

		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != len(services) {
		return nil, fmt.Errorf("circular dependency detected")
	}

	return result, nil
}

// generateYAML generates the YAML content for the compose file.
func (g *Generator) generateYAML(compose *ComposeFile, projectName string) (string, error) {
	var buf bytes.Buffer

	// Write header
	buf.WriteString(fmt.Sprintf("# Generated by Homeport - %s\n", time.Now().Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("# Project: %s\n\n", projectName))
	buf.WriteString(fmt.Sprintf("version: \"%s\"\n\n", compose.Version))

	// Write services
	buf.WriteString("services:\n")

	// Get sorted service names
	serviceNames := make([]string, 0, len(compose.Services))
	for name := range compose.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	for _, name := range serviceNames {
		svc := compose.Services[name]
		if err := g.writeService(&buf, svc); err != nil {
			return "", err
		}
	}

	// Write networks
	if len(compose.Networks) > 0 {
		buf.WriteString("\nnetworks:\n")

		networkNames := make([]string, 0, len(compose.Networks))
		for name := range compose.Networks {
			networkNames = append(networkNames, name)
		}
		sort.Strings(networkNames)

		for _, name := range networkNames {
			net := compose.Networks[name]
			buf.WriteString(fmt.Sprintf("  %s:\n", name))
			if net.Driver != "" {
				buf.WriteString(fmt.Sprintf("    driver: %s\n", net.Driver))
			}
			if net.Internal {
				buf.WriteString("    internal: true\n")
			}
			if len(net.Labels) > 0 {
				buf.WriteString("    labels:\n")
				for k, v := range net.Labels {
					buf.WriteString(fmt.Sprintf("      %s: %s\n", k, v))
				}
			}
		}
	}

	// Write volumes
	if len(compose.Volumes) > 0 {
		buf.WriteString("\nvolumes:\n")

		volumeNames := make([]string, 0, len(compose.Volumes))
		for name := range compose.Volumes {
			volumeNames = append(volumeNames, name)
		}
		sort.Strings(volumeNames)

		for _, name := range volumeNames {
			vol := compose.Volumes[name]
			buf.WriteString(fmt.Sprintf("  %s:\n", name))
			if vol.Driver != "" {
				buf.WriteString(fmt.Sprintf("    driver: %s\n", vol.Driver))
			}
			if len(vol.DriverOpts) > 0 {
				buf.WriteString("    driver_opts:\n")
				for k, v := range vol.DriverOpts {
					buf.WriteString(fmt.Sprintf("      %s: %s\n", k, v))
				}
			}
		}
	}

	return buf.String(), nil
}

// writeService writes a service definition to the buffer.
func (g *Generator) writeService(buf *bytes.Buffer, svc *Service) error {
	fmt.Fprintf(buf, "  %s:\n", svc.Name)
	fmt.Fprintf(buf, "    image: %s\n", svc.Image)

	if svc.Build != nil {
		buf.WriteString("    build:\n")
		fmt.Fprintf(buf, "      context: %s\n", svc.Build.Context)
		if svc.Build.Dockerfile != "" {
			fmt.Fprintf(buf, "      dockerfile: %s\n", svc.Build.Dockerfile)
		}
	}

	if svc.Restart != "" {
		fmt.Fprintf(buf, "    restart: %s\n", svc.Restart)
	}

	if len(svc.Environment) > 0 {
		buf.WriteString("    environment:\n")
		// Sort keys for deterministic output
		keys := make([]string, 0, len(svc.Environment))
		for k := range svc.Environment {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := svc.Environment[k]
			// Handle values with special characters
			if strings.ContainsAny(v, " \n\t:{}[]!@#$%^&*") {
				fmt.Fprintf(buf, "      %s: \"%s\"\n", k, escapeYAML(v))
			} else {
				fmt.Fprintf(buf, "      %s: %s\n", k, v)
			}
		}
	}

	if len(svc.Ports) > 0 {
		buf.WriteString("    ports:\n")
		for _, port := range svc.Ports {
			fmt.Fprintf(buf, "      - \"%s\"\n", port)
		}
	}

	if len(svc.Volumes) > 0 {
		buf.WriteString("    volumes:\n")
		for _, vol := range svc.Volumes {
			fmt.Fprintf(buf, "      - %s\n", vol)
		}
	}

	if len(svc.Networks) > 0 {
		buf.WriteString("    networks:\n")
		for _, net := range svc.Networks {
			fmt.Fprintf(buf, "      - %s\n", net)
		}
	}

	if len(svc.DependsOn) > 0 {
		buf.WriteString("    depends_on:\n")
		for _, dep := range svc.DependsOn {
			fmt.Fprintf(buf, "      - %s\n", dep)
		}
	}

	if len(svc.Command) > 0 {
		buf.WriteString("    command:\n")
		for _, cmd := range svc.Command {
			fmt.Fprintf(buf, "      - %s\n", cmd)
		}
	}

	if svc.HealthCheck != nil {
		buf.WriteString("    healthcheck:\n")
		if len(svc.HealthCheck.Test) > 0 {
			buf.WriteString("      test:\n")
			for _, t := range svc.HealthCheck.Test {
				fmt.Fprintf(buf, "        - %s\n", t)
			}
		}
		if svc.HealthCheck.Interval != "" {
			fmt.Fprintf(buf, "      interval: %s\n", svc.HealthCheck.Interval)
		}
		if svc.HealthCheck.Timeout != "" {
			fmt.Fprintf(buf, "      timeout: %s\n", svc.HealthCheck.Timeout)
		}
		if svc.HealthCheck.Retries > 0 {
			fmt.Fprintf(buf, "      retries: %d\n", svc.HealthCheck.Retries)
		}
		if svc.HealthCheck.StartPeriod != "" {
			fmt.Fprintf(buf, "      start_period: %s\n", svc.HealthCheck.StartPeriod)
		}
	}

	if svc.Deploy != nil && svc.Deploy.Resources != nil {
		buf.WriteString("    deploy:\n")
		buf.WriteString("      resources:\n")

		if svc.Deploy.Resources.Limits != nil {
			buf.WriteString("        limits:\n")
			if svc.Deploy.Resources.Limits.CPUs != "" {
				fmt.Fprintf(buf, "          cpus: '%s'\n", svc.Deploy.Resources.Limits.CPUs)
			}
			if svc.Deploy.Resources.Limits.Memory != "" {
				fmt.Fprintf(buf, "          memory: %s\n", svc.Deploy.Resources.Limits.Memory)
			}
		}

		if svc.Deploy.Resources.Reservations != nil {
			buf.WriteString("        reservations:\n")
			if svc.Deploy.Resources.Reservations.CPUs != "" {
				fmt.Fprintf(buf, "          cpus: '%s'\n", svc.Deploy.Resources.Reservations.CPUs)
			}
			if svc.Deploy.Resources.Reservations.Memory != "" {
				fmt.Fprintf(buf, "          memory: %s\n", svc.Deploy.Resources.Reservations.Memory)
			}
		}
	}

	if len(svc.Labels) > 0 {
		buf.WriteString("    labels:\n")
		// Sort keys for deterministic output
		keys := make([]string, 0, len(svc.Labels))
		for k := range svc.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := svc.Labels[k]
			fmt.Fprintf(buf, "      %s: \"%s\"\n", k, escapeYAML(v))
		}
	}

	buf.WriteString("\n")
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Utility Functions
// ─────────────────────────────────────────────────────────────────────────────

// escapeYAML escapes special characters in YAML strings.
func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// isNamedVolume checks if a volume string represents a named volume.
func isNamedVolume(vol string) bool {
	return !strings.Contains(vol, "/") && !strings.Contains(vol, ".")
}

// extractVolumeName extracts the volume name from a volume string.
func extractVolumeName(vol string) string {
	parts := strings.Split(vol, ":")
	if len(parts) > 0 {
		return parts[0]
	}
	return vol
}

// hasTraefikLabels checks if labels contain Traefik configuration.
func hasTraefikLabels(labels map[string]string) bool {
	for key := range labels {
		if strings.HasPrefix(key, "traefik.") {
			return true
		}
	}
	return false
}

// isSensitiveEnvVar checks if an environment variable name suggests sensitive data.
func isSensitiveEnvVar(key string) bool {
	sensitivePatterns := []string{
		"PASSWORD", "SECRET", "KEY", "TOKEN", "CREDENTIAL",
		"API_KEY", "AUTH", "PRIVATE",
	}
	upperKey := strings.ToUpper(key)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(upperKey, pattern) {
			return true
		}
	}
	return false
}

// isPlaceholderValue checks if a value is a placeholder.
func isPlaceholderValue(value string) bool {
	return strings.HasPrefix(value, "${") ||
		strings.Contains(value, "changeme") ||
		strings.Contains(value, "CHANGEME") ||
		strings.Contains(value, "<") ||
		value == ""
}

// sanitizeEnvValue sanitizes a value for the .env file.
func sanitizeEnvValue(value string) string {
	if value == "" {
		return "changeme"
	}
	// Remove ${...} wrapper if present
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		inner := value[2 : len(value)-1]
		// Check for default value syntax ${VAR:-default}
		if idx := strings.Index(inner, ":-"); idx != -1 {
			return inner[idx+2:]
		}
		return "changeme"
	}
	return value
}

// ─────────────────────────────────────────────────────────────────────────────
// Type Definitions
// ─────────────────────────────────────────────────────────────────────────────

// ComposeFile represents a complete docker-compose.yml structure.
type ComposeFile struct {
	Version  string
	Services map[string]*Service
	Networks map[string]*Network
	Volumes  map[string]*Volume
}

// Service represents a Docker Compose service.
type Service struct {
	Name        string
	Image       string
	Build       *BuildConfig
	Environment map[string]string
	Ports       []string
	Volumes     []string
	Networks    []string
	DependsOn   []string
	Command     []string
	Labels      map[string]string
	HealthCheck *HealthCheck
	Deploy      *Deploy
	Restart     string
}

// BuildConfig represents Docker build configuration.
type BuildConfig struct {
	Context    string
	Dockerfile string
}

// HealthCheck represents a health check configuration.
type HealthCheck struct {
	Test        []string
	Interval    string
	Timeout     string
	Retries     int
	StartPeriod string
}

// Deploy represents deployment configuration.
type Deploy struct {
	Resources *Resources
}

// Resources represents resource constraints.
type Resources struct {
	Limits       *ResourceLimits
	Reservations *ResourceLimits
}

// ResourceLimits represents CPU and memory limits.
type ResourceLimits struct {
	CPUs   string
	Memory string
}

// Network represents a Docker network.
type Network struct {
	Driver   string
	Internal bool
	Labels   map[string]string
}

// Volume represents a Docker volume.
type Volume struct {
	Name       string
	Driver     string
	DriverOpts map[string]string
}

// init registers the generator with the default registry.
func init() {
	generator.RegisterGenerator(New())
}
