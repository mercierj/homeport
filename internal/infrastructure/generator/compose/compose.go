// Package compose generates Docker Compose configurations from mapping results.
package compose

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/generator"
	"github.com/cloudexit/cloudexit/internal/domain/mapper"
)

// Generator generates Docker Compose files.
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

// Generate creates a docker-compose.yml from mapping results.
func (g *Generator) Generate(results []*mapper.MappingResult) (*generator.Output, error) {
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
				if !strings.Contains(vol, "/") && !strings.Contains(vol, ".") {
					// This is a named volume
					parts := strings.Split(vol, ":")
					if len(parts) > 0 {
						volName := parts[0]
						if _, exists := volumes[volName]; !exists {
							volumes[volName] = &Volume{
								Name:   volName,
								Driver: "local",
							}
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
	content, err := g.generateYAML(compose)
	if err != nil {
		return nil, fmt.Errorf("failed to generate YAML: %w", err)
	}

	output.AddFile("docker-compose.yml", []byte(content))
	output.AddMetadata("project_name", g.projectName)
	output.AddMetadata("services_count", fmt.Sprintf("%d", len(services)))
	output.AddMetadata("generated_at", time.Now().Format(time.RFC3339))

	return output, nil
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
	hasPublicLabels := false
	for key := range svc.Labels {
		if strings.HasPrefix(key, "traefik.") {
			hasPublicLabels = true
			break
		}
	}

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
func (g *Generator) generateYAML(compose *ComposeFile) (string, error) {
	var buf bytes.Buffer

	// Write header
	buf.WriteString(fmt.Sprintf("# Generated by CloudExit - %s\n", time.Now().Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("# Project: %s\n\n", g.projectName))
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
	buf.WriteString(fmt.Sprintf("  %s:\n", svc.Name))
	buf.WriteString(fmt.Sprintf("    image: %s\n", svc.Image))

	if svc.Restart != "" {
		buf.WriteString(fmt.Sprintf("    restart: %s\n", svc.Restart))
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
				buf.WriteString(fmt.Sprintf("      %s: \"%s\"\n", k, escapeYAML(v)))
			} else {
				buf.WriteString(fmt.Sprintf("      %s: %s\n", k, v))
			}
		}
	}

	if len(svc.Ports) > 0 {
		buf.WriteString("    ports:\n")
		for _, port := range svc.Ports {
			buf.WriteString(fmt.Sprintf("      - \"%s\"\n", port))
		}
	}

	if len(svc.Volumes) > 0 {
		buf.WriteString("    volumes:\n")
		for _, vol := range svc.Volumes {
			buf.WriteString(fmt.Sprintf("      - %s\n", vol))
		}
	}

	if len(svc.Networks) > 0 {
		buf.WriteString("    networks:\n")
		for _, net := range svc.Networks {
			buf.WriteString(fmt.Sprintf("      - %s\n", net))
		}
	}

	if len(svc.DependsOn) > 0 {
		buf.WriteString("    depends_on:\n")
		for _, dep := range svc.DependsOn {
			buf.WriteString(fmt.Sprintf("      - %s\n", dep))
		}
	}

	if len(svc.Command) > 0 {
		buf.WriteString("    command:\n")
		for _, cmd := range svc.Command {
			buf.WriteString(fmt.Sprintf("      - %s\n", cmd))
		}
	}

	if svc.HealthCheck != nil {
		buf.WriteString("    healthcheck:\n")
		if len(svc.HealthCheck.Test) > 0 {
			buf.WriteString("      test:\n")
			for _, t := range svc.HealthCheck.Test {
				buf.WriteString(fmt.Sprintf("        - %s\n", t))
			}
		}
		if svc.HealthCheck.Interval != "" {
			buf.WriteString(fmt.Sprintf("      interval: %s\n", svc.HealthCheck.Interval))
		}
		if svc.HealthCheck.Timeout != "" {
			buf.WriteString(fmt.Sprintf("      timeout: %s\n", svc.HealthCheck.Timeout))
		}
		if svc.HealthCheck.Retries > 0 {
			buf.WriteString(fmt.Sprintf("      retries: %d\n", svc.HealthCheck.Retries))
		}
		if svc.HealthCheck.StartPeriod != "" {
			buf.WriteString(fmt.Sprintf("      start_period: %s\n", svc.HealthCheck.StartPeriod))
		}
	}

	if svc.Deploy != nil && svc.Deploy.Resources != nil {
		buf.WriteString("    deploy:\n")
		buf.WriteString("      resources:\n")

		if svc.Deploy.Resources.Limits != nil {
			buf.WriteString("        limits:\n")
			if svc.Deploy.Resources.Limits.CPUs != "" {
				buf.WriteString(fmt.Sprintf("          cpus: '%s'\n", svc.Deploy.Resources.Limits.CPUs))
			}
			if svc.Deploy.Resources.Limits.Memory != "" {
				buf.WriteString(fmt.Sprintf("          memory: %s\n", svc.Deploy.Resources.Limits.Memory))
			}
		}

		if svc.Deploy.Resources.Reservations != nil {
			buf.WriteString("        reservations:\n")
			if svc.Deploy.Resources.Reservations.CPUs != "" {
				buf.WriteString(fmt.Sprintf("          cpus: '%s'\n", svc.Deploy.Resources.Reservations.CPUs))
			}
			if svc.Deploy.Resources.Reservations.Memory != "" {
				buf.WriteString(fmt.Sprintf("          memory: %s\n", svc.Deploy.Resources.Reservations.Memory))
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
			buf.WriteString(fmt.Sprintf("      %s: \"%s\"\n", k, escapeYAML(v)))
		}
	}

	buf.WriteString("\n")
	return nil
}

// escapeYAML escapes special characters in YAML strings.
func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

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
