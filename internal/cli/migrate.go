package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/provider"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/domain/target"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	migrateOutput            string
	migrateDomain            string
	migrateIncludeMigration  bool
	migrateIncludeMonitoring bool
	migrateConsolidate       bool
	// New flags for Sprint 7
	migrateProvider     string
	migrateRegion       string
	migrateHALevel      string
	migrateInstanceType string
	migrateSSL          bool
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

Use --consolidate to reduce container sprawl by grouping similar cloud
resources into consolidated stacks (e.g., multiple RDS instances into
a single PostgreSQL container, multiple SQS queues into one RabbitMQ).

Examples:
  # Migrate from Terraform state
  homeport migrate terraform.tfstate

  # Migrate with custom output directory
  homeport migrate ./infrastructure --output ./my-stack

  # Migrate with domain configuration
  homeport migrate ./infrastructure --domain example.com

  # Include migration and monitoring tools
  homeport migrate ./infrastructure --include-migration --include-monitoring

  # Use stack consolidation to reduce container count
  homeport migrate ./infrastructure --consolidate

  # Migrate to a specific EU provider
  homeport migrate ./terraform --provider hetzner --region fsn1 --output ./deploy

  # Migrate with specific HA level
  homeport migrate ./terraform --provider scaleway --region fr-par-1 --ha-level cluster

  # Migrate with custom instance type
  homeport migrate ./terraform --provider ovh --instance-type b2-15 --ssl=true`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		inputPath := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Infrastructure Migration")
			ui.Info(fmt.Sprintf("Input: %s", inputPath))
			ui.Info(fmt.Sprintf("Output: %s", migrateOutput))
			if migrateDomain != "" {
				ui.Info(fmt.Sprintf("Domain: %s", migrateDomain))
			}
			if migrateProvider != "" {
				provInfo := provider.GetProviderInfo(provider.Provider(migrateProvider))
				if provInfo != nil {
					ui.Info(fmt.Sprintf("Provider: %s", provInfo.DisplayName))
				} else {
					ui.Info(fmt.Sprintf("Provider: %s", migrateProvider))
				}
				if migrateRegion != "" {
					ui.Info(fmt.Sprintf("Region: %s", migrateRegion))
				}
			}
			ui.Info(fmt.Sprintf("HA Level: %s", migrateHALevel))
			ui.Info(fmt.Sprintf("SSL: %v", migrateSSL))
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
	migrateCmd.Flags().BoolVar(&migrateConsolidate, "consolidate", false, "consolidate similar resources into unified stacks (reduces container count)")

	// Provider flags (Sprint 7)
	migrateCmd.Flags().StringVar(&migrateProvider, "provider", "", "target cloud provider (hetzner, scaleway, ovh)")
	migrateCmd.Flags().StringVar(&migrateRegion, "region", "", "provider-specific region (e.g., fsn1, fr-par-1, gra)")
	migrateCmd.Flags().StringVar(&migrateHALevel, "ha-level", "basic", "high availability level (none, basic, multi-server, cluster)")
	migrateCmd.Flags().StringVar(&migrateInstanceType, "instance-type", "", "override default instance type selection")
	migrateCmd.Flags().BoolVar(&migrateSSL, "ssl", true, "enable SSL/TLS for services")
}

// MigrationConfig represents the configuration for migration
type MigrationConfig struct {
	InputPath         string
	OutputPath        string
	Domain            string
	IncludeMigration  bool
	IncludeMonitoring bool
	Consolidate       bool
	// Provider options (Sprint 7)
	Provider     string
	Region       string
	HALevel      string
	InstanceType string
	SSL          bool
}

// performMigration performs the actual migration
func performMigration(inputPath string) error {
	config := &MigrationConfig{
		InputPath:         inputPath,
		OutputPath:        migrateOutput,
		Domain:            migrateDomain,
		IncludeMigration:  migrateIncludeMigration,
		IncludeMonitoring: migrateIncludeMonitoring,
		Consolidate:       migrateConsolidate,
		Provider:          migrateProvider,
		Region:            migrateRegion,
		HALevel:           migrateHALevel,
		InstanceType:      migrateInstanceType,
		SSL:               migrateSSL,
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

	// If a cloud provider is specified, use provider-specific generation
	if config.Provider != "" {
		return performProviderMigration(config, analysis)
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

// performProviderMigration generates output for a specific cloud provider (Hetzner, Scaleway, OVH)
func performProviderMigration(config *MigrationConfig, analysis *AnalysisResult) error {
	// Parse HA level
	haLevel := target.HALevelBasic
	if config.HALevel != "" {
		parsed, ok := target.ParseHALevel(config.HALevel)
		if !ok {
			return fmt.Errorf("invalid HA level: %s (valid: none, basic, multi-server, cluster)", config.HALevel)
		}
		haLevel = parsed
	}

	// Map provider to platform
	var platform target.Platform
	switch config.Provider {
	case "hetzner":
		platform = target.PlatformHetzner
	case "scaleway":
		platform = target.PlatformScaleway
	case "ovh":
		platform = target.PlatformOVH
	default:
		return fmt.Errorf("unsupported provider: %s (valid: hetzner, scaleway, ovh)", config.Provider)
	}

	// Get the generator
	gen, err := generator.GetGenerator(platform)
	if err != nil {
		return fmt.Errorf("no generator found for provider %s: %w", config.Provider, err)
	}

	// Check if HA level is supported
	haSupported := false
	for _, supported := range gen.SupportedHALevels() {
		if supported == haLevel {
			haSupported = true
			break
		}
	}
	if !haSupported {
		return fmt.Errorf("HA level %s not supported by provider %s", haLevel, config.Provider)
	}

	// Step 2: Map resources
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(2, 5, "Mapping resources"))
	}

	ctx := context.Background()
	var mappingResults []*mapper.MappingResult

	for _, resSummary := range analysis.Resources {
		resType := resource.Type(resSummary.Type)
		m, mapErr := mapper.DefaultRegistry.Get(resType)
		if mapErr != nil || m == nil {
			if IsVerbose() {
				ui.Info(fmt.Sprintf("No mapper for %s, skipping", resSummary.Type))
			}
			continue
		}

		res := &resource.AWSResource{
			Type:   resType,
			Name:   resSummary.Name,
			ID:     resSummary.ID,
			Region: resSummary.Region,
			Tags:   resSummary.Tags,
			Config: make(map[string]interface{}),
		}

		result, err := m.Map(ctx, res)
		if err != nil {
			if IsVerbose() {
				ui.Info(fmt.Sprintf("Failed to map %s: %v", resSummary.Name, err))
			}
			continue
		}
		if result != nil {
			mappingResults = append(mappingResults, result)
		}
	}

	if IsVerbose() {
		ui.Info(fmt.Sprintf("Mapped %d resources", len(mappingResults)))
	}

	// Step 3: Configure generator
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(3, 5, "Configuring generator"))
	}

	genConfig := generator.NewTargetConfig(platform)
	genConfig.WithHALevel(haLevel)
	genConfig.WithOutputDir(config.OutputPath)
	genConfig.WithSSL(config.SSL)
	genConfig.WithMonitoring(config.IncludeMonitoring)
	genConfig.WithBackups(true)

	if config.Domain != "" {
		genConfig.WithBaseURL(config.Domain)
	}

	// Set region in target config
	if config.Region != "" {
		if genConfig.TargetConfig == nil {
			genConfig.TargetConfig = target.NewTargetConfig(platform)
		}
		genConfig.TargetConfig.WithRegion(config.Region)
	}

	// Set instance type override
	if config.InstanceType != "" {
		genConfig.WithVariable("instance_type", config.InstanceType)
	}

	// Step 4: Generate output
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(4, 5, "Generating Terraform files"))
	}

	output, err := gen.Generate(ctx, mappingResults, genConfig)
	if err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(config.OutputPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Step 5: Write files
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(5, 5, "Writing output files"))
	}

	// Write all generated files
	for filename, content := range output.Files {
		fullPath := filepath.Join(config.OutputPath, filename)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", filename, err)
		}
		perm := os.FileMode(0644)
		if strings.HasSuffix(filename, ".sh") {
			perm = 0755
		}
		if err := os.WriteFile(fullPath, content, perm); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	// Display summary
	if !IsQuiet() {
		ui.Divider()
		ui.Success(fmt.Sprintf("Generated %d files for %s", output.FileCount(), config.Provider))

		// Show file breakdown
		if len(output.TerraformFiles) > 0 {
			fmt.Printf("  Terraform files: %d\n", len(output.TerraformFiles))
		}
		if len(output.Scripts) > 0 {
			fmt.Printf("  Scripts: %d\n", len(output.Scripts))
		}
		if len(output.Docs) > 0 {
			fmt.Printf("  Documentation: %d\n", len(output.Docs))
		}

		// Show cost estimate if available
		if output.EstimatedCost != nil {
			fmt.Printf("\n  Estimated cost: %.2f %s/month\n",
				output.EstimatedCost.Total,
				output.EstimatedCost.Currency)
		}

		// Show warnings
		if len(output.Warnings) > 0 {
			ui.Divider()
			ui.Warning(fmt.Sprintf("%d warning(s):", len(output.Warnings)))
			for _, w := range output.Warnings {
				fmt.Printf("  - %s\n", w)
			}
		}

		// Show manual steps
		if len(output.ManualSteps) > 0 {
			ui.Divider()
			ui.Info("Manual steps required:")
			for i, step := range output.ManualSteps {
				fmt.Printf("  %d. %s\n", i+1, step)
			}
		}

		// Show next steps
		ui.Divider()
		ui.Info("Next steps:")
		fmt.Println("  1. Review the generated Terraform configuration")
		fmt.Println("  2. Update terraform.tfvars with your credentials")
		fmt.Println("  3. Run 'terraform init' to initialize")
		fmt.Println("  4. Run 'terraform plan' to preview changes")
		fmt.Println("  5. Run 'terraform apply' to deploy")
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

	// If consolidation is enabled, use the consolidated generation path
	if config.Consolidate {
		return generateConsolidatedDockerCompose(config, analysis)
	}

	compose := &ComposeFile{
		Services: make(map[string]ComposeService),
		Networks: map[string]ComposeNetwork{
			"homeport": {
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

// generateConsolidatedDockerCompose generates Docker Compose with stack consolidation
func generateConsolidatedDockerCompose(config *MigrationConfig, analysis *AnalysisResult) error {
	if IsVerbose() {
		ui.Info("Generating consolidated docker-compose.yml")
	}

	ctx := context.Background()

	// Step 1: Map all resources to get MappingResults
	var mappingResults []*mapper.MappingResult
	sourceResourceCount := 0

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

		if result != nil {
			// Preserve source resource info for consolidation
			result.SourceResourceType = string(resType)
			result.SourceResourceName = resSummary.Name
			mappingResults = append(mappingResults, result)
			sourceResourceCount++
		}
	}

	// Step 2: Consolidate using the consolidator
	cons := consolidator.New()
	opts := consolidator.DefaultOptions()
	opts.IncludeSupportServices = config.IncludeMonitoring

	consolidatedResult, err := cons.Consolidate(ctx, mappingResults, opts)
	if err != nil {
		return fmt.Errorf("consolidation failed: %w", err)
	}

	// Display consolidation summary
	if !IsQuiet() {
		displayConsolidationSummary(consolidatedResult, sourceResourceCount)
	}

	// Step 3: Build compose file from consolidated stacks
	compose := &ComposeFile{
		Services: make(map[string]ComposeService),
		Networks: map[string]ComposeNetwork{
			"homeport": {
				Driver: "bridge",
			},
		},
		Volumes: make(map[string]ComposeVolume),
	}

	// Add Traefik as the default reverse proxy
	compose.Services["traefik"] = buildTraefikService(config)

	// Track all generated configs and scripts
	allConfigs := make(map[string][]byte)
	allScripts := make(map[string][]byte)

	// Process each consolidated stack
	for _, stk := range consolidatedResult.Stacks {
		// Add services from the stack
		for _, svc := range stk.Services {
			composeSvc := convertStackService(svc)
			compose.Services[svc.Name] = composeSvc
		}

		// Add volumes from the stack
		for _, vol := range stk.Volumes {
			compose.Volumes[vol.Name] = ComposeVolume{
				Driver:     vol.Driver,
				DriverOpts: vol.DriverOpts,
			}
		}

		// Add networks from the stack
		for _, net := range stk.Networks {
			if _, exists := compose.Networks[net.Name]; !exists {
				compose.Networks[net.Name] = ComposeNetwork{
					Driver:   net.Driver,
					External: net.External,
				}
			}
		}

		// Collect configs and scripts
		for path, content := range stk.Configs {
			allConfigs[path] = content
		}
		for path, content := range stk.Scripts {
			allScripts[path] = content
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
	if consolidatedResult.HasWarnings() || consolidatedResult.HasManualSteps() {
		notesContent := generateMigrationNotes(consolidatedResult.Warnings, consolidatedResult.ManualSteps)
		notesPath := filepath.Join(config.OutputPath, "MIGRATION_NOTES.md")
		if err := os.WriteFile(notesPath, []byte(notesContent), 0644); err != nil {
			return fmt.Errorf("failed to write migration notes: %w", err)
		}

		// Display manual steps warning
		if !IsQuiet() && consolidatedResult.HasManualSteps() {
			ui.Divider()
			ui.Warning(fmt.Sprintf("%d manual step(s) required - see MIGRATION_NOTES.md", len(consolidatedResult.ManualSteps)))
		}
	}

	return nil
}

// displayConsolidationSummary shows the consolidation results
func displayConsolidationSummary(result *stack.ConsolidatedResult, sourceCount int) {
	ui.Divider()
	ui.Info("Stack Consolidation Summary")
	ui.Divider()

	// Show overall consolidation
	totalServices := result.TotalServices()
	ratio := 1.0
	if totalServices > 0 {
		ratio = float64(sourceCount) / float64(totalServices)
	}

	fmt.Printf("  Source resources:     %d\n", sourceCount)
	fmt.Printf("  Consolidated stacks:  %d\n", result.TotalStacks())
	fmt.Printf("  Total services:       %d\n", totalServices)
	fmt.Printf("  Consolidation ratio:  %.1fx reduction\n", ratio)
	fmt.Println()

	// Show breakdown by stack type
	if result.Metadata != nil && len(result.Metadata.ByStackType) > 0 {
		fmt.Println("  Stack breakdown:")
		for _, stk := range result.Stacks {
			resourceCount := stk.SourceResourceCount()
			serviceCount := stk.ServiceCount()
			fmt.Printf("    %s: %d resources -> %d service(s)\n",
				stk.Type.DisplayName(), resourceCount, serviceCount)
		}
		if len(result.Passthrough) > 0 {
			fmt.Printf("    Passthrough: %d resources (individual services)\n", len(result.Passthrough))
		}
	}
}

// convertStackService converts a stack.Service to ComposeService
func convertStackService(svc *stack.Service) ComposeService {
	result := ComposeService{
		Image:       svc.Image,
		Restart:     svc.Restart,
		Ports:       svc.Ports,
		Volumes:     svc.Volumes,
		Command:     svc.Command,
		Networks:    svc.Networks,
		DependsOn:   svc.DependsOn,
		Labels:      svc.Labels,
		Environment: svc.Environment,
	}

	// Convert health check
	if svc.HealthCheck != nil {
		result.HealthCheck = &ComposeHealthCheck{
			Test:        svc.HealthCheck.Test,
			Interval:    svc.HealthCheck.Interval,
			Timeout:     svc.HealthCheck.Timeout,
			Retries:     svc.HealthCheck.Retries,
			StartPeriod: svc.HealthCheck.StartPeriod,
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

// buildTraefikService creates the default Traefik service configuration.
func buildTraefikService(config *MigrationConfig) ComposeService {
	labels := map[string]string{
		"homeport.source": "traefik",
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
			"--providers.docker.network=homeport",
			"--providers.file.directory=/etc/traefik/dynamic",
			"--entrypoints.web.address=:80",
			"--entrypoints.websecure.address=:443",
			"--log.level=INFO",
		},
		Networks: []string{"homeport"},
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
		Networks: []string{"homeport"},
		Labels: map[string]string{
			"homeport.source": "monitoring",
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
		Networks:  []string{"homeport"},
		Labels: map[string]string{
			"homeport.source":                         "monitoring",
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
	sb.WriteString("3. Create the Docker network: `docker network create homeport`\n")
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
# Generated by Homeport

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
    network: homeport
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

	envBuilder.WriteString("# Homeport Generated Environment Configuration\n")
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
	readmeBuilder.WriteString("# Homeport - Self-Hosted Stack\n\n")
	readmeBuilder.WriteString("This stack was generated by Homeport from your cloud infrastructure.\n\n")
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
	readmeBuilder.WriteString("   docker network create homeport\n")
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
	readmeBuilder.WriteString("For issues and questions, visit: https://github.com/homeport/homeport\n")

	readmeContent := readmeBuilder.String()

	readmePath := filepath.Join(config.OutputPath, "README.md")
	return os.WriteFile(readmePath, []byte(readmeContent), 0644)
}
