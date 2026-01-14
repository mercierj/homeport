package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ECSToComposeExecutor converts ECS task definitions to Docker Compose format.
type ECSToComposeExecutor struct{}

// NewECSToComposeExecutor creates a new ECS to Docker Compose executor.
func NewECSToComposeExecutor() *ECSToComposeExecutor {
	return &ECSToComposeExecutor{}
}

// Type returns the migration type.
func (e *ECSToComposeExecutor) Type() string {
	return "ecs_to_compose"
}

// GetPhases returns the migration phases.
func (e *ECSToComposeExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching task definitions",
		"Converting to Docker Compose",
		"Extracting secrets",
		"Generating files",
	}
}

// Validate validates the migration configuration.
func (e *ECSToComposeExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		// At least one of cluster_arn or task_definition_arns is required
		_, hasClusterARN := config.Source["cluster_arn"].(string)
		taskDefArns, hasTaskDefArns := config.Source["task_definition_arns"]
		hasValidTaskDefArns := false
		if hasTaskDefArns {
			if arns, ok := taskDefArns.([]interface{}); ok && len(arns) > 0 {
				hasValidTaskDefArns = true
			}
			if arns, ok := taskDefArns.([]string); ok && len(arns) > 0 {
				hasValidTaskDefArns = true
			}
		}

		if !hasClusterARN && !hasValidTaskDefArns {
			result.Valid = false
			result.Errors = append(result.Errors, "source.cluster_arn or source.task_definition_arns is required")
		}

		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default us-east-1")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	return result, nil
}

// ECSTaskDefinition represents an ECS task definition.
type ECSTaskDefinition struct {
	TaskDefinition struct {
		Family               string                   `json:"family"`
		ContainerDefinitions []ECSContainerDefinition `json:"containerDefinitions"`
		Cpu                  string                   `json:"cpu,omitempty"`
		Memory               string                   `json:"memory,omitempty"`
	} `json:"taskDefinition"`
}

// ECSContainerDefinition represents an ECS container definition.
type ECSContainerDefinition struct {
	Name         string              `json:"name"`
	Image        string              `json:"image"`
	Cpu          int                 `json:"cpu,omitempty"`
	Memory       int                 `json:"memory,omitempty"`
	MemoryReservation int            `json:"memoryReservation,omitempty"`
	PortMappings []ECSPortMapping    `json:"portMappings,omitempty"`
	Environment  []ECSEnvVar         `json:"environment,omitempty"`
	Secrets      []ECSSecret         `json:"secrets,omitempty"`
	Essential    bool                `json:"essential,omitempty"`
	Command      []string            `json:"command,omitempty"`
	EntryPoint   []string            `json:"entryPoint,omitempty"`
	WorkingDirectory string          `json:"workingDirectory,omitempty"`
	HealthCheck  *ECSHealthCheck     `json:"healthCheck,omitempty"`
	DependsOn    []ECSContainerDependency `json:"dependsOn,omitempty"`
	Links        []string            `json:"links,omitempty"`
	VolumesFrom  []ECSVolumeFrom     `json:"volumesFrom,omitempty"`
	MountPoints  []ECSMountPoint     `json:"mountPoints,omitempty"`
}

// ECSPortMapping represents an ECS port mapping.
type ECSPortMapping struct {
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

// ECSEnvVar represents an ECS environment variable.
type ECSEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ECSSecret represents an ECS secret reference.
type ECSSecret struct {
	Name      string `json:"name"`
	ValueFrom string `json:"valueFrom"`
}

// ECSHealthCheck represents an ECS health check.
type ECSHealthCheck struct {
	Command     []string `json:"command"`
	Interval    int      `json:"interval,omitempty"`
	Timeout     int      `json:"timeout,omitempty"`
	Retries     int      `json:"retries,omitempty"`
	StartPeriod int      `json:"startPeriod,omitempty"`
}

// ECSContainerDependency represents an ECS container dependency.
type ECSContainerDependency struct {
	ContainerName string `json:"containerName"`
	Condition     string `json:"condition"`
}

// ECSVolumeFrom represents an ECS volumes_from entry.
type ECSVolumeFrom struct {
	SourceContainer string `json:"sourceContainer"`
	ReadOnly        bool   `json:"readOnly,omitempty"`
}

// ECSMountPoint represents an ECS mount point.
type ECSMountPoint struct {
	SourceVolume  string `json:"sourceVolume"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly,omitempty"`
}

// DockerComposeService represents a Docker Compose service.
type DockerComposeService struct {
	Image       string                       `yaml:"image"`
	Ports       []string                     `yaml:"ports,omitempty"`
	Environment map[string]string            `yaml:"environment,omitempty"`
	Command     []string                     `yaml:"command,omitempty"`
	Entrypoint  []string                     `yaml:"entrypoint,omitempty"`
	WorkingDir  string                       `yaml:"working_dir,omitempty"`
	Deploy      *DockerComposeDeploy         `yaml:"deploy,omitempty"`
	HealthCheck *DockerComposeHealthCheck    `yaml:"healthcheck,omitempty"`
	DependsOn   []string                     `yaml:"depends_on,omitempty"`
	Links       []string                     `yaml:"links,omitempty"`
	Volumes     []string                     `yaml:"volumes,omitempty"`
}

// DockerComposeDeploy represents Docker Compose deploy section.
type DockerComposeDeploy struct {
	Resources DockerComposeResources `yaml:"resources,omitempty"`
}

// DockerComposeResources represents Docker Compose resource limits.
type DockerComposeResources struct {
	Limits DockerComposeResourceLimits `yaml:"limits,omitempty"`
}

// DockerComposeResourceLimits represents Docker Compose resource limits.
type DockerComposeResourceLimits struct {
	Memory string `yaml:"memory,omitempty"`
	Cpus   string `yaml:"cpus,omitempty"`
}

// DockerComposeHealthCheck represents a Docker Compose health check.
type DockerComposeHealthCheck struct {
	Test        []string `yaml:"test,omitempty"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
}

// Execute performs the migration.
func (e *ECSToComposeExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract configuration
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	clusterARN, _ := config.Source["cluster_arn"].(string)
	outputDir := config.Destination["output_dir"].(string)

	// Get task definition ARNs
	var taskDefArns []string
	if arns, ok := config.Source["task_definition_arns"].([]interface{}); ok {
		for _, arn := range arns {
			if s, ok := arn.(string); ok {
				taskDefArns = append(taskDefArns, s)
			}
		}
	}
	if arns, ok := config.Source["task_definition_arns"].([]string); ok {
		taskDefArns = arns
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 10, "Checking credentials")

	// Test credentials by calling STS get-caller-identity
	testCmd := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity", "--region", region)
	testCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)
	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("AWS credential validation failed: %s", string(output)))
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}
	EmitLog(m, "info", "AWS credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching task definitions
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching ECS task definitions")
	EmitProgress(m, 25, "Fetching task definitions")

	// If cluster ARN is provided but no task definition ARNs, list tasks from cluster
	if clusterARN != "" && len(taskDefArns) == 0 {
		EmitLog(m, "info", fmt.Sprintf("Listing services from cluster: %s", clusterARN))

		// List services in the cluster
		listServicesCmd := exec.CommandContext(ctx, "aws", "ecs", "list-services",
			"--cluster", clusterARN,
			"--region", region,
		)
		listServicesCmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID="+accessKeyID,
			"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
			"AWS_DEFAULT_REGION="+region,
		)
		servicesOutput, err := listServicesCmd.Output()
		if err != nil {
			EmitLog(m, "warn", "Failed to list services, trying to list task definitions directly")
		} else {
			var servicesResp struct {
				ServiceArns []string `json:"serviceArns"`
			}
			if err := json.Unmarshal(servicesOutput, &servicesResp); err == nil && len(servicesResp.ServiceArns) > 0 {
				// Describe services to get task definitions
				describeServicesCmd := exec.CommandContext(ctx, "aws", "ecs", "describe-services",
					"--cluster", clusterARN,
					"--services", strings.Join(servicesResp.ServiceArns, " "),
					"--region", region,
				)
				describeServicesCmd.Env = append(os.Environ(),
					"AWS_ACCESS_KEY_ID="+accessKeyID,
					"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
					"AWS_DEFAULT_REGION="+region,
				)
				descOutput, err := describeServicesCmd.Output()
				if err == nil {
					var descResp struct {
						Services []struct {
							TaskDefinition string `json:"taskDefinition"`
						} `json:"services"`
					}
					if err := json.Unmarshal(descOutput, &descResp); err == nil {
						for _, svc := range descResp.Services {
							if svc.TaskDefinition != "" {
								taskDefArns = append(taskDefArns, svc.TaskDefinition)
							}
						}
					}
				}
			}
		}

		// If still no task definitions found, list all task definitions
		if len(taskDefArns) == 0 {
			listTaskDefsCmd := exec.CommandContext(ctx, "aws", "ecs", "list-task-definitions",
				"--region", region,
				"--status", "ACTIVE",
			)
			listTaskDefsCmd.Env = append(os.Environ(),
				"AWS_ACCESS_KEY_ID="+accessKeyID,
				"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
				"AWS_DEFAULT_REGION="+region,
			)
			taskDefsOutput, err := listTaskDefsCmd.Output()
			if err != nil {
				EmitLog(m, "error", "Failed to list task definitions")
				return fmt.Errorf("failed to list task definitions: %w", err)
			}
			var taskDefsResp struct {
				TaskDefinitionArns []string `json:"taskDefinitionArns"`
			}
			if err := json.Unmarshal(taskDefsOutput, &taskDefsResp); err == nil {
				taskDefArns = taskDefsResp.TaskDefinitionArns
			}
		}
	}

	if len(taskDefArns) == 0 {
		EmitLog(m, "warn", "No task definitions found")
		return fmt.Errorf("no task definitions found to convert")
	}

	EmitLog(m, "info", fmt.Sprintf("Found %d task definition(s) to convert", len(taskDefArns)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Converting to Docker Compose
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Converting task definitions to Docker Compose format")
	EmitProgress(m, 50, "Converting to Docker Compose")

	services := make(map[string]DockerComposeService)
	var allSecrets []ECSSecret

	for i, taskDefARN := range taskDefArns {
		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		EmitLog(m, "info", fmt.Sprintf("Processing task definition %d/%d: %s", i+1, len(taskDefArns), taskDefARN))

		// Describe task definition
		describeCmd := exec.CommandContext(ctx, "aws", "ecs", "describe-task-definition",
			"--task-definition", taskDefARN,
			"--region", region,
		)
		describeCmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID="+accessKeyID,
			"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
			"AWS_DEFAULT_REGION="+region,
		)

		output, err := describeCmd.Output()
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to describe task definition %s: %v", taskDefARN, err))
			continue
		}

		var taskDef ECSTaskDefinition
		if err := json.Unmarshal(output, &taskDef); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to parse task definition %s: %v", taskDefARN, err))
			continue
		}

		// Convert each container definition to a Docker Compose service
		for _, container := range taskDef.TaskDefinition.ContainerDefinitions {
			service := e.convertContainerToService(container)
			serviceName := sanitizeServiceName(container.Name)
			services[serviceName] = service

			// Collect secrets
			allSecrets = append(allSecrets, container.Secrets...)
		}
	}

	if len(services) == 0 {
		EmitLog(m, "error", "No services could be converted")
		return fmt.Errorf("no services could be converted from task definitions")
	}

	EmitLog(m, "info", fmt.Sprintf("Converted %d service(s)", len(services)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Extracting secrets
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Extracting secrets to .env.example")
	EmitProgress(m, 75, "Extracting secrets")

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate .env.example for secrets
	if len(allSecrets) > 0 {
		envContent := e.generateEnvExample(allSecrets)
		envPath := filepath.Join(outputDir, ".env.example")
		if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to write .env.example: %v", err))
		} else {
			EmitLog(m, "info", fmt.Sprintf("Created .env.example with %d secret placeholder(s)", len(allSecrets)))
		}
	} else {
		EmitLog(m, "info", "No secrets found in task definitions")
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating files
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating docker-compose.yml")
	EmitProgress(m, 90, "Writing files")

	// Generate docker-compose.yml content
	composeContent := e.generateDockerCompose(services)

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("Successfully created docker-compose.yml at %s", composePath))
	EmitProgress(m, 100, "Conversion complete")

	return nil
}

// convertContainerToService converts an ECS container definition to a Docker Compose service.
func (e *ECSToComposeExecutor) convertContainerToService(container ECSContainerDefinition) DockerComposeService {
	service := DockerComposeService{
		Image: container.Image,
	}

	// Convert port mappings
	for _, pm := range container.PortMappings {
		hostPort := pm.HostPort
		if hostPort == 0 {
			hostPort = pm.ContainerPort
		}
		portStr := fmt.Sprintf("%d:%d", hostPort, pm.ContainerPort)
		if pm.Protocol != "" && pm.Protocol != "tcp" {
			portStr += "/" + pm.Protocol
		}
		service.Ports = append(service.Ports, portStr)
	}

	// Convert environment variables
	if len(container.Environment) > 0 {
		service.Environment = make(map[string]string)
		for _, env := range container.Environment {
			service.Environment[env.Name] = env.Value
		}
	}

	// Add secret placeholders to environment
	if len(container.Secrets) > 0 {
		if service.Environment == nil {
			service.Environment = make(map[string]string)
		}
		for _, secret := range container.Secrets {
			// Reference the environment variable from .env file
			service.Environment[secret.Name] = fmt.Sprintf("${%s}", secret.Name)
		}
	}

	// Convert command
	if len(container.Command) > 0 {
		service.Command = container.Command
	}

	// Convert entrypoint
	if len(container.EntryPoint) > 0 {
		service.Entrypoint = container.EntryPoint
	}

	// Convert working directory
	if container.WorkingDirectory != "" {
		service.WorkingDir = container.WorkingDirectory
	}

	// Convert resource limits
	memory := container.Memory
	if memory == 0 {
		memory = container.MemoryReservation
	}
	if memory > 0 || container.Cpu > 0 {
		service.Deploy = &DockerComposeDeploy{
			Resources: DockerComposeResources{
				Limits: DockerComposeResourceLimits{},
			},
		}
		if memory > 0 {
			service.Deploy.Resources.Limits.Memory = fmt.Sprintf("%dM", memory)
		}
		if container.Cpu > 0 {
			// ECS CPU units are in 1/1024 of a vCPU, convert to fractional CPUs
			cpus := float64(container.Cpu) / 1024.0
			service.Deploy.Resources.Limits.Cpus = fmt.Sprintf("%.2f", cpus)
		}
	}

	// Convert health check
	if container.HealthCheck != nil && len(container.HealthCheck.Command) > 0 {
		service.HealthCheck = &DockerComposeHealthCheck{
			Test: container.HealthCheck.Command,
		}
		if container.HealthCheck.Interval > 0 {
			service.HealthCheck.Interval = fmt.Sprintf("%ds", container.HealthCheck.Interval)
		}
		if container.HealthCheck.Timeout > 0 {
			service.HealthCheck.Timeout = fmt.Sprintf("%ds", container.HealthCheck.Timeout)
		}
		if container.HealthCheck.Retries > 0 {
			service.HealthCheck.Retries = container.HealthCheck.Retries
		}
		if container.HealthCheck.StartPeriod > 0 {
			service.HealthCheck.StartPeriod = fmt.Sprintf("%ds", container.HealthCheck.StartPeriod)
		}
	}

	// Convert dependencies
	for _, dep := range container.DependsOn {
		service.DependsOn = append(service.DependsOn, sanitizeServiceName(dep.ContainerName))
	}

	// Convert links
	service.Links = container.Links

	// Convert mount points to volumes
	for _, mp := range container.MountPoints {
		volumeStr := fmt.Sprintf("%s:%s", mp.SourceVolume, mp.ContainerPath)
		if mp.ReadOnly {
			volumeStr += ":ro"
		}
		service.Volumes = append(service.Volumes, volumeStr)
	}

	return service
}

// generateEnvExample generates .env.example content from secrets.
func (e *ECSToComposeExecutor) generateEnvExample(secrets []ECSSecret) string {
	var sb strings.Builder
	sb.WriteString("# Environment variables for ECS secrets\n")
	sb.WriteString("# Copy this file to .env and fill in the actual values\n\n")

	seen := make(map[string]bool)
	for _, secret := range secrets {
		if seen[secret.Name] {
			continue
		}
		seen[secret.Name] = true
		sb.WriteString(fmt.Sprintf("# Source: %s\n", secret.ValueFrom))
		sb.WriteString(fmt.Sprintf("%s=\n\n", secret.Name))
	}

	return sb.String()
}

// generateDockerCompose generates a docker-compose.yml content.
func (e *ECSToComposeExecutor) generateDockerCompose(services map[string]DockerComposeService) string {
	var sb strings.Builder
	sb.WriteString("version: '3'\n\n")
	sb.WriteString("services:\n")

	for name, svc := range services {
		sb.WriteString(fmt.Sprintf("  %s:\n", name))
		sb.WriteString(fmt.Sprintf("    image: %s\n", svc.Image))

		if len(svc.Ports) > 0 {
			sb.WriteString("    ports:\n")
			for _, port := range svc.Ports {
				sb.WriteString(fmt.Sprintf("      - \"%s\"\n", port))
			}
		}

		if len(svc.Environment) > 0 {
			sb.WriteString("    environment:\n")
			for k, v := range svc.Environment {
				// Escape special characters in value
				escapedValue := strings.ReplaceAll(v, "\"", "\\\"")
				sb.WriteString(fmt.Sprintf("      %s: \"%s\"\n", k, escapedValue))
			}
		}

		if len(svc.Command) > 0 {
			sb.WriteString("    command:\n")
			for _, cmd := range svc.Command {
				sb.WriteString(fmt.Sprintf("      - \"%s\"\n", cmd))
			}
		}

		if len(svc.Entrypoint) > 0 {
			sb.WriteString("    entrypoint:\n")
			for _, ep := range svc.Entrypoint {
				sb.WriteString(fmt.Sprintf("      - \"%s\"\n", ep))
			}
		}

		if svc.WorkingDir != "" {
			sb.WriteString(fmt.Sprintf("    working_dir: %s\n", svc.WorkingDir))
		}

		if svc.Deploy != nil && (svc.Deploy.Resources.Limits.Memory != "" || svc.Deploy.Resources.Limits.Cpus != "") {
			sb.WriteString("    deploy:\n")
			sb.WriteString("      resources:\n")
			sb.WriteString("        limits:\n")
			if svc.Deploy.Resources.Limits.Memory != "" {
				sb.WriteString(fmt.Sprintf("          memory: %s\n", svc.Deploy.Resources.Limits.Memory))
			}
			if svc.Deploy.Resources.Limits.Cpus != "" {
				sb.WriteString(fmt.Sprintf("          cpus: '%s'\n", svc.Deploy.Resources.Limits.Cpus))
			}
		}

		if svc.HealthCheck != nil {
			sb.WriteString("    healthcheck:\n")
			if len(svc.HealthCheck.Test) > 0 {
				sb.WriteString("      test:\n")
				for _, t := range svc.HealthCheck.Test {
					sb.WriteString(fmt.Sprintf("        - \"%s\"\n", t))
				}
			}
			if svc.HealthCheck.Interval != "" {
				sb.WriteString(fmt.Sprintf("      interval: %s\n", svc.HealthCheck.Interval))
			}
			if svc.HealthCheck.Timeout != "" {
				sb.WriteString(fmt.Sprintf("      timeout: %s\n", svc.HealthCheck.Timeout))
			}
			if svc.HealthCheck.Retries > 0 {
				sb.WriteString(fmt.Sprintf("      retries: %d\n", svc.HealthCheck.Retries))
			}
			if svc.HealthCheck.StartPeriod != "" {
				sb.WriteString(fmt.Sprintf("      start_period: %s\n", svc.HealthCheck.StartPeriod))
			}
		}

		if len(svc.DependsOn) > 0 {
			sb.WriteString("    depends_on:\n")
			for _, dep := range svc.DependsOn {
				sb.WriteString(fmt.Sprintf("      - %s\n", dep))
			}
		}

		if len(svc.Links) > 0 {
			sb.WriteString("    links:\n")
			for _, link := range svc.Links {
				sb.WriteString(fmt.Sprintf("      - %s\n", link))
			}
		}

		if len(svc.Volumes) > 0 {
			sb.WriteString("    volumes:\n")
			for _, vol := range svc.Volumes {
				sb.WriteString(fmt.Sprintf("      - %s\n", vol))
			}
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// sanitizeServiceName converts a container name to a valid Docker Compose service name.
func sanitizeServiceName(name string) string {
	// Docker Compose service names should be lowercase and use hyphens
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
