package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/cli/ui"
	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	migrateOutput            string
	migrateDomain            string
	migrateIncludeMigration  bool
	migrateIncludeMonitoring bool
)

// migrateCmd represents the migrate command
var migrateCmd = &cobra.Command{
	Use:   "migrate <path>",
	Short: "Generate self-hosted stack from AWS infrastructure",
	Long: `Generate a complete self-hosted Docker stack from AWS infrastructure.

The migrate command takes your AWS infrastructure configuration and generates
a complete self-hosted stack including:
  - Docker Compose configuration
  - Traefik reverse proxy setup
  - Service configurations
  - Environment files
  - Migration scripts
  - Documentation

The generated stack will include all necessary services to replace your
AWS infrastructure with self-hosted alternatives.

Examples:
  # Migrate from Terraform state
  cloudexit migrate terraform.tfstate

  # Migrate with custom output directory
  cloudexit migrate ./infrastructure --output ./my-stack

  # Migrate with domain configuration
  cloudexit migrate ./infrastructure --domain example.com

  # Include migration and monitoring tools
  cloudexit migrate ./infrastructure --include-migration --include-monitoring`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath := args[0]

		if !IsQuiet() {
			ui.Header("CloudExit - Infrastructure Migration")
			ui.Info(fmt.Sprintf("Input: %s", inputPath))
			ui.Info(fmt.Sprintf("Output: %s", migrateOutput))
			if migrateDomain != "" {
				ui.Info(fmt.Sprintf("Domain: %s", migrateDomain))
			}
			ui.Divider()
		}

		// Validate input path
		if _, err := os.Stat(inputPath); os.IsNotExist(err) {
			return fmt.Errorf("input path does not exist: %s", inputPath)
		}

		// Create output directory
		if err := os.MkdirAll(migrateOutput, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}

		// Perform migration
		if err := performMigration(inputPath); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success("Migration completed successfully")
			ui.Info(fmt.Sprintf("Stack generated in: %s", migrateOutput))
			ui.Info("Next steps:")
			fmt.Println("  1. Review the generated configuration")
			fmt.Println("  2. Update environment variables in .env files")
			fmt.Println("  3. Run 'docker-compose up -d' to start the stack")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)

	migrateCmd.Flags().StringVarP(&migrateOutput, "output", "o", "./output", "output directory path")
	migrateCmd.Flags().StringVarP(&migrateDomain, "domain", "d", "", "domain name for services")
	migrateCmd.Flags().BoolVar(&migrateIncludeMigration, "include-migration", false, "include migration tools and scripts")
	migrateCmd.Flags().BoolVar(&migrateIncludeMonitoring, "include-monitoring", false, "include monitoring stack (Prometheus, Grafana)")
}

// MigrationConfig represents the configuration for migration
type MigrationConfig struct {
	InputPath         string
	OutputPath        string
	Domain            string
	IncludeMigration  bool
	IncludeMonitoring bool
}

// performMigration performs the actual migration
func performMigration(inputPath string) error {
	config := &MigrationConfig{
		InputPath:         inputPath,
		OutputPath:        migrateOutput,
		Domain:            migrateDomain,
		IncludeMigration:  migrateIncludeMigration,
		IncludeMonitoring: migrateIncludeMonitoring,
	}

	// Step 1: Analyze infrastructure
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(1, 5, "Analyzing infrastructure"))
	}

	analysis, err := performAnalysis(inputPath)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	if IsVerbose() {
		ui.Info(fmt.Sprintf("Found %d resources to migrate", analysis.Statistics.TotalResources))
	}

	// Step 2: Generate Docker Compose
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(2, 5, "Generating Docker Compose"))
	}

	if err := generateDockerCompose(config, analysis); err != nil {
		return fmt.Errorf("failed to generate docker-compose: %w", err)
	}

	// Step 3: Generate Traefik configuration
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(3, 5, "Generating Traefik configuration"))
	}

	if err := generateTraefikConfig(config); err != nil {
		return fmt.Errorf("failed to generate Traefik config: %w", err)
	}

	// Step 4: Generate environment files
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(4, 5, "Generating environment files"))
	}

	if err := generateEnvFiles(config, analysis); err != nil {
		return fmt.Errorf("failed to generate env files: %w", err)
	}

	// Step 5: Generate documentation
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(5, 5, "Generating documentation"))
	}

	if err := generateDocumentation(config, analysis); err != nil {
		return fmt.Errorf("failed to generate documentation: %w", err)
	}

	return nil
}

// ComposeFile represents a Docker Compose file structure for YAML serialization.
type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
	Networks map[string]ComposeNetwork `yaml:"networks,omitempty"`
	Volumes  map[string]ComposeVolume  `yaml:"volumes,omitempty"`
}

// ComposeService represents a service in Docker Compose YAML.
type ComposeService struct {
	Image         string              `yaml:"image,omitempty"`
	ContainerName string              `yaml:"container_name,omitempty"`
	Restart       string              `yaml:"restart,omitempty"`
	Ports         []string            `yaml:"ports,omitempty"`
	Volumes       []string            `yaml:"volumes,omitempty"`
	Environment   map[string]string   `yaml:"environment,omitempty"`
	EnvFile       []string            `yaml:"env_file,omitempty"`
	Command       []string            `yaml:"command,omitempty"`
	Networks      []string            `yaml:"networks,omitempty"`
	DependsOn     []string            `yaml:"depends_on,omitempty"`
	Labels        map[string]string   `yaml:"labels,omitempty"`
	HealthCheck   *ComposeHealthCheck `yaml:"healthcheck,omitempty"`
	Deploy        *ComposeDeploy      `yaml:"deploy,omitempty"`
	CapAdd        []string            `yaml:"cap_add,omitempty"`
	CapDrop       []string            `yaml:"cap_drop,omitempty"`
	User          string              `yaml:"user,omitempty"`
	WorkingDir    string              `yaml:"working_dir,omitempty"`
	ExtraHosts    []string            `yaml:"extra_hosts,omitempty"`
}

// ComposeHealthCheck represents a Docker Compose health check.
type ComposeHealthCheck struct {
	Test        []string `yaml:"test,omitempty"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
}

// ComposeDeploy represents Docker Compose deploy configuration.
type ComposeDeploy struct {
	Replicas  int               `yaml:"replicas,omitempty"`
	Resources *ComposeResources `yaml:"resources,omitempty"`
}

// ComposeResources represents resource limits/reservations.
type ComposeResources struct {
	Limits       *ComposeResourceLimits `yaml:"limits,omitempty"`
	Reservations *ComposeResourceLimits `yaml:"reservations,omitempty"`
}

// ComposeResourceLimits represents resource constraints.
type ComposeResourceLimits struct {
	CPUs   string `yaml:"cpus,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

// ComposeNetwork represents a Docker Compose network.
type ComposeNetwork struct {
	Driver     string            `yaml:"driver,omitempty"`
	External   bool              `yaml:"external,omitempty"`
	DriverOpts map[string]string `yaml:"driver_opts,omitempty"`
}

// ComposeVolume represents a Docker Compose volume.
type ComposeVolume struct {
	Driver     string            `yaml:"driver,omitempty"`
	DriverOpts map[string]string `yaml:"driver_opts,omitempty"`
}

// generateDockerCompose generates the Docker Compose configuration
func generateDockerCompose(config *MigrationConfig, analysis *AnalysisResult) error {
	if IsVerbose() {
		ui.Info("Generating docker-compose.yml")
	}

	compose := &ComposeFile{
		Services: make(map[string]ComposeService),
		Networks: map[string]ComposeNetwork{
			"cloudexit": {
				Driver: "bridge",
			},
		},
		Volumes: make(map[string]ComposeVolume),
	}

	// Add Traefik as the default reverse proxy
	compose.Services["traefik"] = buildTraefikService(config)

	// Get the mapper registry and map each resource
	ctx := context.Background()

	// Track all generated configs and scripts
	allConfigs := make(map[string][]byte)
	allScripts := make(map[string][]byte)
	allWarnings := []string{}
	allManualSteps := []string{}

	// Process each resource from analysis
	for _, resSummary := range analysis.Resources {
		resType := resource.Type(resSummary.Type)

		// Get mapper for this resource type
		m, mapErr := mapper.DefaultRegistry.Get(resType)
		if mapErr != nil || m == nil {
			if IsVerbose() {
				ui.Info(fmt.Sprintf("No mapper for %s, skipping", resSummary.Type))
			}
			continue
		}

		// Build a resource for the mapper
		res := &resource.AWSResource{
			Type:   resType,
			Name:   resSummary.Name,
			ID:     resSummary.ID,
			Region: resSummary.Region,
			Tags:   resSummary.Tags,
			Config: make(map[string]interface{}),
		}

		// Map the resource
		result, err := m.Map(ctx, res)
		if err != nil {
			if IsVerbose() {
				ui.Info(fmt.Sprintf("Failed to map %s: %v", resSummary.Name, err))
			}
			continue
		}

		if result == nil || result.DockerService == nil {
			continue
		}

		// Add the service to compose
		svc := convertDockerService(result.DockerService)
		compose.Services[result.DockerService.Name] = svc

		// Collect configs, scripts, warnings, manual steps
		for path, content := range result.Configs {
			allConfigs[path] = content
		}
		for path, content := range result.Scripts {
			allScripts[path] = content
		}
		allWarnings = append(allWarnings, result.Warnings...)
		allManualSteps = append(allManualSteps, result.ManualSteps...)

		// Add volumes
		for _, vol := range result.Volumes {
			compose.Volumes[vol.Name] = ComposeVolume{
				Driver:     vol.Driver,
				DriverOpts: vol.DriverOpts,
			}
		}
	}

	// Add monitoring if requested
	if config.IncludeMonitoring {
		addMonitoringServices(compose)
	}

	// Write docker-compose.yml
	data, err := yaml.Marshal(compose)
	if err != nil {
		return fmt.Errorf("failed to marshal docker-compose: %w", err)
	}

	composePath := filepath.Join(config.OutputPath, "docker-compose.yml")
	if err := os.WriteFile(composePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Write config files
	for path, content := range allConfigs {
		fullPath := filepath.Join(config.OutputPath, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", path, err)
		}
		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write config %s: %w", path, err)
		}
	}

	// Write script files
	for path, content := range allScripts {
		fullPath := filepath.Join(config.OutputPath, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", path, err)
		}
		if err := os.WriteFile(fullPath, content, 0755); err != nil {
			return fmt.Errorf("failed to write script %s: %w", path, err)
		}
	}

	// Write warnings and manual steps to a file
	if len(allWarnings) > 0 || len(allManualSteps) > 0 {
		notesContent := generateMigrationNotes(allWarnings, allManualSteps)
		notesPath := filepath.Join(config.OutputPath, "MIGRATION_NOTES.md")
		if err := os.WriteFile(notesPath, []byte(notesContent), 0644); err != nil {
			return fmt.Errorf("failed to write migration notes: %w", err)
		}
	}

	return nil
}

// buildTraefikService creates the default Traefik service configuration.
func buildTraefikService(config *MigrationConfig) ComposeService {
	labels := map[string]string{
		"cloudexit.source": "traefik",
	}

	if config.Domain != "" {
		labels["traefik.http.routers.dashboard.rule"] = fmt.Sprintf("Host(`traefik.%s`)", config.Domain)
	}

	return ComposeService{
		Image:         "traefik:v3.0",
		ContainerName: "traefik",
		Restart:       "unless-stopped",
		Ports: []string{
			"80:80",
			"443:443",
			"8080:8080",
		},
		Volumes: []string{
			"/var/run/docker.sock:/var/run/docker.sock:ro",
			"./traefik:/etc/traefik",
			"./certs:/certs",
		},
		Command: []string{
			"--api.dashboard=true",
			"--api.insecure=true",
			"--providers.docker=true",
			"--providers.docker.exposedbydefault=false",
			"--providers.docker.network=cloudexit",
			"--providers.file.directory=/etc/traefik/dynamic",
			"--entrypoints.web.address=:80",
			"--entrypoints.websecure.address=:443",
			"--log.level=INFO",
		},
		Networks: []string{"cloudexit"},
		Labels:   labels,
		HealthCheck: &ComposeHealthCheck{
			Test:     []string{"CMD", "traefik", "healthcheck", "--ping"},
			Interval: "10s",
			Timeout:  "5s",
			Retries:  3,
		},
	}
}

// convertDockerService converts a mapper DockerService to ComposeService.
func convertDockerService(svc *mapper.DockerService) ComposeService {
	result := ComposeService{
		Image:      svc.Image,
		Restart:    svc.Restart,
		Ports:      svc.Ports,
		Volumes:    svc.Volumes,
		Command:    svc.Command,
		Networks:   svc.Networks,
		DependsOn:  svc.DependsOn,
		Labels:     svc.Labels,
		CapAdd:     svc.CapAdd,
		CapDrop:    svc.CapDrop,
		User:       svc.User,
		WorkingDir: svc.WorkingDir,
		ExtraHosts: svc.ExtraHosts,
	}

	// Convert environment map
	if len(svc.Environment) > 0 {
		result.Environment = svc.Environment
	}

	// Convert health check
	if svc.HealthCheck != nil {
		result.HealthCheck = &ComposeHealthCheck{
			Test:        svc.HealthCheck.Test,
			Interval:    formatDuration(svc.HealthCheck.Interval),
			Timeout:     formatDuration(svc.HealthCheck.Timeout),
			Retries:     svc.HealthCheck.Retries,
			StartPeriod: formatDuration(svc.HealthCheck.StartPeriod),
		}
	}

	// Convert deploy config
	if svc.Deploy != nil {
		result.Deploy = &ComposeDeploy{
			Replicas: svc.Deploy.Replicas,
		}
		if svc.Deploy.Resources != nil {
			result.Deploy.Resources = &ComposeResources{}
			if svc.Deploy.Resources.Limits != nil {
				result.Deploy.Resources.Limits = &ComposeResourceLimits{
					CPUs:   svc.Deploy.Resources.Limits.CPUs,
					Memory: svc.Deploy.Resources.Limits.Memory,
				}
			}
			if svc.Deploy.Resources.Reservations != nil {
				result.Deploy.Resources.Reservations = &ComposeResourceLimits{
					CPUs:   svc.Deploy.Resources.Reservations.CPUs,
					Memory: svc.Deploy.Resources.Reservations.Memory,
				}
			}
		}
	}

	return result
}

// formatDuration formats a duration for Docker Compose (e.g., "10s", "1m").
func formatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// addMonitoringServices adds Prometheus and Grafana to the compose file.
func addMonitoringServices(compose *ComposeFile) {
	// Add Prometheus
	compose.Services["prometheus"] = ComposeService{
		Image:         "prom/prometheus:v2.48.0",
		ContainerName: "prometheus",
		Restart:       "unless-stopped",
		Ports:         []string{"9090:9090"},
		Volumes: []string{
			"./monitoring/prometheus:/etc/prometheus",
			"prometheus_data:/prometheus",
		},
		Command: []string{
			"--config.file=/etc/prometheus/prometheus.yml",
			"--storage.tsdb.path=/prometheus",
			"--web.enable-lifecycle",
		},
		Networks: []string{"cloudexit"},
		Labels: map[string]string{
			"cloudexit.source": "monitoring",
		},
	}

	// Add Grafana
	compose.Services["grafana"] = ComposeService{
		Image:         "grafana/grafana:10.2.0",
		ContainerName: "grafana",
		Restart:       "unless-stopped",
		Ports:         []string{"3000:3000"},
		Volumes: []string{
			"./monitoring/grafana/provisioning:/etc/grafana/provisioning",
			"grafana_data:/var/lib/grafana",
		},
		Environment: map[string]string{
			"GF_SECURITY_ADMIN_USER":     "${GRAFANA_ADMIN_USER:-admin}",
			"GF_SECURITY_ADMIN_PASSWORD": "${GRAFANA_ADMIN_PASSWORD:-admin}",
		},
		DependsOn: []string{"prometheus"},
		Networks:  []string{"cloudexit"},
		Labels: map[string]string{
			"cloudexit.source":                         "monitoring",
			"traefik.enable":                           "true",
			"traefik.http.routers.grafana.rule":        "Host(`grafana.localhost`)",
			"traefik.http.routers.grafana.entrypoints": "web",
		},
	}

	// Add volumes for monitoring
	compose.Volumes["prometheus_data"] = ComposeVolume{Driver: "local"}
	compose.Volumes["grafana_data"] = ComposeVolume{Driver: "local"}
}

// generateMigrationNotes generates migration notes markdown content.
func generateMigrationNotes(warnings, manualSteps []string) string {
	var sb strings.Builder

	sb.WriteString("# Migration Notes\n\n")
	sb.WriteString("This file contains important notes, warnings, and manual steps for your migration.\n\n")

	if len(warnings) > 0 {
		sb.WriteString("## Warnings\n\n")
		// Deduplicate warnings
		seen := make(map[string]bool)
		for _, w := range warnings {
			if !seen[w] {
				sb.WriteString(fmt.Sprintf("- %s\n", w))
				seen[w] = true
			}
		}
		sb.WriteString("\n")
	}

	if len(manualSteps) > 0 {
		sb.WriteString("## Manual Steps Required\n\n")
		// Deduplicate and sort manual steps
		seen := make(map[string]bool)
		uniqueSteps := []string{}
		for _, s := range manualSteps {
			if !seen[s] {
				uniqueSteps = append(uniqueSteps, s)
				seen[s] = true
			}
		}
		sort.Strings(uniqueSteps)
		for i, s := range uniqueSteps {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Next Steps\n\n")
	sb.WriteString("1. Review the generated `docker-compose.yml` configuration\n")
	sb.WriteString("2. Update environment variables in `.env` file\n")
	sb.WriteString("3. Create the Docker network: `docker network create cloudexit`\n")
	sb.WriteString("4. Start the stack: `docker-compose up -d`\n")
	sb.WriteString("5. Complete any manual steps listed above\n")

	return sb.String()
}

// generateTraefikConfig generates the Traefik configuration
func generateTraefikConfig(config *MigrationConfig) error {
	if IsVerbose() {
		ui.Info("Generating Traefik configuration")
	}

	// Create traefik directory
	traefikDir := filepath.Join(config.OutputPath, "traefik")
	if err := os.MkdirAll(traefikDir, 0755); err != nil {
		return err
	}

	// Create dynamic config directory
	dynamicDir := filepath.Join(traefikDir, "dynamic")
	if err := os.MkdirAll(dynamicDir, 0755); err != nil {
		return err
	}

	// Generate main Traefik configuration
	email := "admin@example.com"
	if config.Domain != "" {
		email = fmt.Sprintf("admin@%s", config.Domain)
	}

	traefikContent := fmt.Sprintf(`# Traefik Static Configuration
# Generated by CloudExit

api:
  dashboard: true
  insecure: true  # Set to false in production

log:
  level: INFO
  format: json

accessLog:
  filePath: "/var/log/traefik/access.log"
  format: json

entryPoints:
  web:
    address: ":80"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
  websecure:
    address: ":443"
    http:
      tls:
        certResolver: letsencrypt

providers:
  docker:
    endpoint: "unix:///var/run/docker.sock"
    exposedByDefault: false
    network: cloudexit
  file:
    directory: "/etc/traefik/dynamic"
    watch: true

certificatesResolvers:
  letsencrypt:
    acme:
      email: %s
      storage: /certs/acme.json
      httpChallenge:
        entryPoint: web

ping:
  entryPoint: "web"
`, email)

	configPath := filepath.Join(traefikDir, "traefik.yml")
	if err := os.WriteFile(configPath, []byte(traefikContent), 0644); err != nil {
		return err
	}

	// Generate dynamic configuration template
	dynamicContent := `# Traefik Dynamic Configuration
# Add your custom routers, services, and middlewares here

http:
  middlewares:
    # Security headers middleware
    security-headers:
      headers:
        browserXssFilter: true
        contentTypeNosniff: true
        frameDeny: true
        sslRedirect: true
        stsIncludeSubdomains: true
        stsPreload: true
        stsSeconds: 31536000

    # Rate limiting middleware
    rate-limit:
      rateLimit:
        average: 100
        burst: 50

    # Compression middleware
    compress:
      compress: {}

    # Retry middleware
    retry:
      retry:
        attempts: 3
        initialInterval: 100ms
`

	dynamicPath := filepath.Join(dynamicDir, "middlewares.yml")
	return os.WriteFile(dynamicPath, []byte(dynamicContent), 0644)
}

// generateEnvFiles generates environment files
func generateEnvFiles(config *MigrationConfig, analysis *AnalysisResult) error {
	if IsVerbose() {
		ui.Info("Generating environment files")
	}

	// Build environment content based on analysis
	var envBuilder strings.Builder

	envBuilder.WriteString("# CloudExit Generated Environment Configuration\n")
	envBuilder.WriteString(fmt.Sprintf("# Generated from: %s\n\n", config.InputPath))

	envBuilder.WriteString("# Domain Configuration\n")
	envBuilder.WriteString(fmt.Sprintf("DOMAIN=%s\n\n", config.Domain))

	// Check for database resources
	hasPostgres := false
	hasMySQL := false
	hasRedis := false
	hasMongo := false

	for _, res := range analysis.Resources {
		switch res.Type {
		case string(resource.TypeRDSInstance), string(resource.TypeRDSCluster), string(resource.TypeAzurePostgres):
			hasPostgres = true
		case string(resource.TypeAzureMySQL):
			hasMySQL = true
		case string(resource.TypeElastiCache), string(resource.TypeAzureCache), string(resource.TypeMemorystore):
			hasRedis = true
		case string(resource.TypeCosmosDB), string(resource.TypeDynamoDBTable):
			hasMongo = true
		}
	}

	if hasPostgres {
		envBuilder.WriteString("# PostgreSQL Configuration\n")
		envBuilder.WriteString("POSTGRES_HOST=postgres\n")
		envBuilder.WriteString("POSTGRES_PORT=5432\n")
		envBuilder.WriteString("POSTGRES_USER=admin\n")
		envBuilder.WriteString("POSTGRES_PASSWORD=changeme\n")
		envBuilder.WriteString("POSTGRES_DB=myapp\n\n")
	}

	if hasMySQL {
		envBuilder.WriteString("# MySQL Configuration\n")
		envBuilder.WriteString("MYSQL_HOST=mysql\n")
		envBuilder.WriteString("MYSQL_PORT=3306\n")
		envBuilder.WriteString("MYSQL_USER=admin\n")
		envBuilder.WriteString("MYSQL_PASSWORD=changeme\n")
		envBuilder.WriteString("MYSQL_DATABASE=myapp\n\n")
	}

	if hasRedis {
		envBuilder.WriteString("# Redis Configuration\n")
		envBuilder.WriteString("REDIS_HOST=redis\n")
		envBuilder.WriteString("REDIS_PORT=6379\n")
		envBuilder.WriteString("REDIS_PASSWORD=changeme\n\n")
	}

	if hasMongo {
		envBuilder.WriteString("# MongoDB Configuration\n")
		envBuilder.WriteString("MONGO_HOST=mongodb\n")
		envBuilder.WriteString("MONGO_PORT=27017\n")
		envBuilder.WriteString("MONGO_USER=admin\n")
		envBuilder.WriteString("MONGO_PASSWORD=changeme\n")
		envBuilder.WriteString("MONGO_DATABASE=myapp\n\n")
	}

	if config.IncludeMonitoring {
		envBuilder.WriteString("# Monitoring Configuration\n")
		envBuilder.WriteString("GRAFANA_ADMIN_USER=admin\n")
		envBuilder.WriteString("GRAFANA_ADMIN_PASSWORD=changeme\n\n")
	}

	envBuilder.WriteString("# Traefik Configuration\n")
	envBuilder.WriteString("TRAEFIK_DASHBOARD=true\n")
	if config.Domain != "" {
		envBuilder.WriteString(fmt.Sprintf("ACME_EMAIL=admin@%s\n", config.Domain))
	} else {
		envBuilder.WriteString("ACME_EMAIL=admin@example.com\n")
	}

	envPath := filepath.Join(config.OutputPath, ".env.example")
	return os.WriteFile(envPath, []byte(envBuilder.String()), 0644)
}

// generateDocumentation generates documentation
func generateDocumentation(config *MigrationConfig, analysis *AnalysisResult) error {
	if IsVerbose() {
		ui.Info("Generating documentation")
	}

	// Build services section based on analysis
	var servicesSection strings.Builder

	servicesSection.WriteString("### Traefik (Reverse Proxy)\n")
	servicesSection.WriteString("- Dashboard: http://localhost:8080\n")
	servicesSection.WriteString("- HTTP: http://localhost\n")
	servicesSection.WriteString("- HTTPS: https://localhost\n\n")

	// Group resources by category
	computeCount := analysis.Statistics.Migration.Compute
	dbCount := analysis.Statistics.Migration.Database
	storageCount := analysis.Statistics.Migration.Storage
	networkCount := analysis.Statistics.Migration.Networking

	if computeCount > 0 {
		servicesSection.WriteString(fmt.Sprintf("### Compute Services (%d)\n", computeCount))
		servicesSection.WriteString("- Container-based workloads are mapped to Docker services\n")
		servicesSection.WriteString("- Lambda/Functions are mapped to OpenFaaS or native containers\n\n")
	}

	if dbCount > 0 {
		servicesSection.WriteString(fmt.Sprintf("### Database Services (%d)\n", dbCount))
		servicesSection.WriteString("- RDS/SQL databases are mapped to PostgreSQL or MySQL containers\n")
		servicesSection.WriteString("- NoSQL databases are mapped to MongoDB or ScyllaDB\n")
		servicesSection.WriteString("- Cache services are mapped to Redis\n\n")
	}

	if storageCount > 0 {
		servicesSection.WriteString(fmt.Sprintf("### Storage Services (%d)\n", storageCount))
		servicesSection.WriteString("- S3/Blob storage is mapped to MinIO\n")
		servicesSection.WriteString("- File storage is mapped to local volumes or NFS\n\n")
	}

	if networkCount > 0 {
		servicesSection.WriteString(fmt.Sprintf("### Networking Services (%d)\n", networkCount))
		servicesSection.WriteString("- Load balancers are mapped to Traefik\n")
		servicesSection.WriteString("- DNS is mapped to CoreDNS or external DNS providers\n\n")
	}

	if config.IncludeMonitoring {
		servicesSection.WriteString("### Monitoring Stack\n")
		servicesSection.WriteString("- Prometheus: http://localhost:9090\n")
		servicesSection.WriteString("- Grafana: http://localhost:3000 (admin/admin)\n\n")
	}

	var readmeBuilder strings.Builder
	readmeBuilder.WriteString("# CloudExit - Self-Hosted Stack\n\n")
	readmeBuilder.WriteString("This stack was generated by CloudExit from your cloud infrastructure.\n\n")
	readmeBuilder.WriteString("## Overview\n\n")
	readmeBuilder.WriteString("| Metric | Count |\n")
	readmeBuilder.WriteString("|--------|-------|\n")
	readmeBuilder.WriteString(fmt.Sprintf("| Total Resources | %d |\n", analysis.Statistics.TotalResources))
	readmeBuilder.WriteString(fmt.Sprintf("| Compute Services | %d |\n", analysis.Statistics.Migration.Compute))
	readmeBuilder.WriteString(fmt.Sprintf("| Database Services | %d |\n", analysis.Statistics.Migration.Database))
	readmeBuilder.WriteString(fmt.Sprintf("| Storage Services | %d |\n", analysis.Statistics.Migration.Storage))
	readmeBuilder.WriteString(fmt.Sprintf("| Networking Services | %d |\n", analysis.Statistics.Migration.Networking))
	readmeBuilder.WriteString(fmt.Sprintf("| Security Services | %d |\n\n", analysis.Statistics.Migration.Security))
	readmeBuilder.WriteString("## Getting Started\n\n")
	readmeBuilder.WriteString("1. Review and update the environment variables in `.env.example`\n")
	readmeBuilder.WriteString("2. Copy `.env.example` to `.env`\n")
	readmeBuilder.WriteString("3. Create the Docker network:\n")
	readmeBuilder.WriteString("   ```bash\n")
	readmeBuilder.WriteString("   docker network create cloudexit\n")
	readmeBuilder.WriteString("   ```\n")
	readmeBuilder.WriteString("4. Start the stack:\n")
	readmeBuilder.WriteString("   ```bash\n")
	readmeBuilder.WriteString("   docker-compose up -d\n")
	readmeBuilder.WriteString("   ```\n\n")
	readmeBuilder.WriteString("## Services\n\n")
	readmeBuilder.WriteString(servicesSection.String())
	readmeBuilder.WriteString("## Migration Notes\n\n")
	readmeBuilder.WriteString("- Review all generated configurations before deploying\n")
	readmeBuilder.WriteString("- Update passwords and secrets in environment files\n")
	readmeBuilder.WriteString("- Configure DNS records for your domain\n")
	readmeBuilder.WriteString("- Set up SSL certificates (Let's Encrypt is pre-configured)\n")
	readmeBuilder.WriteString("- Check MIGRATION_NOTES.md for service-specific warnings and manual steps\n\n")
	readmeBuilder.WriteString("## File Structure\n\n")
	readmeBuilder.WriteString("```\n")
	readmeBuilder.WriteString(fmt.Sprintf("%s/\n", filepath.Base(config.OutputPath)))
	readmeBuilder.WriteString("├── docker-compose.yml    # Main Docker Compose configuration\n")
	readmeBuilder.WriteString("├── .env.example          # Environment variables template\n")
	readmeBuilder.WriteString("├── traefik/\n")
	readmeBuilder.WriteString("│   ├── traefik.yml       # Traefik static configuration\n")
	readmeBuilder.WriteString("│   └── dynamic/          # Traefik dynamic configuration\n")
	readmeBuilder.WriteString("├── certs/                # SSL certificates directory\n")
	readmeBuilder.WriteString("└── MIGRATION_NOTES.md    # Warnings and manual steps\n")
	readmeBuilder.WriteString("```\n\n")
	readmeBuilder.WriteString("## Support\n\n")
	readmeBuilder.WriteString("For issues and questions, visit: https://github.com/agnostech/agnostech\n")

	readmeContent := readmeBuilder.String()

	readmePath := filepath.Join(config.OutputPath, "README.md")
	return os.WriteFile(readmePath, []byte(readmeContent), 0644)
}
