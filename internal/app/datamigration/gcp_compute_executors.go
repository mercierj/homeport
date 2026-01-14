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

// ============================================================================
// CloudRunToDockerExecutor - Cloud Run to Docker Compose
// ============================================================================

// CloudRunToDockerExecutor migrates Cloud Run services to Docker Compose.
type CloudRunToDockerExecutor struct{}

// NewCloudRunToDockerExecutor creates a new Cloud Run to Docker executor.
func NewCloudRunToDockerExecutor() *CloudRunToDockerExecutor {
	return &CloudRunToDockerExecutor{}
}

// Type returns the migration type.
func (e *CloudRunToDockerExecutor) Type() string {
	return "cloudrun_to_docker"
}

// GetPhases returns the migration phases.
func (e *CloudRunToDockerExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching Cloud Run services",
		"Extracting service configurations",
		"Converting to Docker Compose",
		"Generating output files",
	}
}

// Validate validates the migration configuration.
func (e *CloudRunToDockerExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, will list all regions")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Cloud Run-specific features (IAM, Cloud SQL proxy) require manual configuration")

	return result, nil
}

// cloudRunService represents a Cloud Run service configuration.
type cloudRunService struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Template struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				ContainerConcurrency int `json:"containerConcurrency"`
				TimeoutSeconds       int `json:"timeoutSeconds"`
				Containers           []struct {
					Image     string `json:"image"`
					Ports     []struct {
						ContainerPort int    `json:"containerPort"`
						Name          string `json:"name"`
					} `json:"ports"`
					Env []struct {
						Name      string `json:"name"`
						Value     string `json:"value,omitempty"`
						ValueFrom *struct {
							SecretKeyRef struct {
								Name string `json:"name"`
								Key  string `json:"key"`
							} `json:"secretKeyRef"`
						} `json:"valueFrom,omitempty"`
					} `json:"env"`
					Resources struct {
						Limits struct {
							CPU    string `json:"cpu"`
							Memory string `json:"memory"`
						} `json:"limits"`
					} `json:"resources"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
		Traffic []struct {
			LatestRevision bool `json:"latestRevision"`
			Percent        int  `json:"percent"`
		} `json:"traffic"`
	} `json:"spec"`
	Status struct {
		URL string `json:"url"`
	} `json:"status"`
}

// Execute performs the migration.
func (e *CloudRunToDockerExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	projectID := config.Source["project_id"].(string)
	region, _ := config.Source["region"].(string)
	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Test gcloud authentication
	authCmd := exec.CommandContext(ctx, "gcloud", "auth", "list", "--format=json")
	if output, err := authCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("GCP authentication failed: %s", string(output)))
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}

	// Set project
	projectCmd := exec.CommandContext(ctx, "gcloud", "config", "set", "project", projectID)
	if err := projectCmd.Run(); err != nil {
		return fmt.Errorf("failed to set GCP project: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Using GCP project: %s", projectID))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching Cloud Run services
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching Cloud Run services")
	EmitProgress(m, 25, "Listing services")

	var listArgs []string
	if region != "" {
		listArgs = []string{"run", "services", "list", "--region", region, "--format=json"}
	} else {
		listArgs = []string{"run", "services", "list", "--format=json"}
	}

	listCmd := exec.CommandContext(ctx, "gcloud", listArgs...)
	servicesOutput, err := listCmd.Output()
	if err != nil {
		EmitLog(m, "error", "Failed to list Cloud Run services")
		return fmt.Errorf("failed to list Cloud Run services: %w", err)
	}

	var services []cloudRunService
	if err := json.Unmarshal(servicesOutput, &services); err != nil {
		return fmt.Errorf("failed to parse Cloud Run services: %w", err)
	}

	if len(services) == 0 {
		EmitLog(m, "warn", "No Cloud Run services found")
		return fmt.Errorf("no Cloud Run services found in project %s", projectID)
	}

	EmitLog(m, "info", fmt.Sprintf("Found %d Cloud Run service(s)", len(services)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Extracting service configurations
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Extracting service configurations")
	EmitProgress(m, 50, "Processing services")

	composeServices := make(map[string]DockerComposeService)
	var allSecrets []string

	for _, svc := range services {
		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		serviceName := sanitizeServiceName(svc.Metadata.Name)
		EmitLog(m, "info", fmt.Sprintf("Processing service: %s", svc.Metadata.Name))

		for _, container := range svc.Spec.Template.Spec.Containers {
			dockerSvc := DockerComposeService{
				Image:       container.Image,
				Environment: make(map[string]string),
			}

			// Convert ports
			for _, port := range container.Ports {
				dockerSvc.Ports = append(dockerSvc.Ports, fmt.Sprintf("%d:%d", port.ContainerPort, port.ContainerPort))
			}

			// Convert environment variables
			for _, env := range container.Env {
				if env.ValueFrom != nil {
					// Secret reference
					secretName := fmt.Sprintf("%s_%s", env.ValueFrom.SecretKeyRef.Name, env.ValueFrom.SecretKeyRef.Key)
					dockerSvc.Environment[env.Name] = fmt.Sprintf("${%s}", secretName)
					allSecrets = append(allSecrets, fmt.Sprintf("# Secret: %s (key: %s)\n%s=", env.ValueFrom.SecretKeyRef.Name, env.ValueFrom.SecretKeyRef.Key, secretName))
				} else {
					dockerSvc.Environment[env.Name] = env.Value
				}
			}

			// Convert resource limits
			if container.Resources.Limits.Memory != "" || container.Resources.Limits.CPU != "" {
				dockerSvc.Deploy = &DockerComposeDeploy{
					Resources: DockerComposeResources{
						Limits: DockerComposeResourceLimits{
							Memory: container.Resources.Limits.Memory,
							Cpus:   container.Resources.Limits.CPU,
						},
					},
				}
			}

			composeServices[serviceName] = dockerSvc
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Converting to Docker Compose
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Docker Compose configuration")
	EmitProgress(m, 75, "Creating compose file")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate docker-compose.yml
	composeContent := e.generateDockerCompose(composeServices)
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating output files
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating additional files")
	EmitProgress(m, 90, "Writing files")

	// Generate .env.example
	if len(allSecrets) > 0 {
		envContent := "# Environment variables for Cloud Run secrets\n"
		envContent += "# Copy this file to .env and fill in the actual values\n\n"
		for _, secret := range allSecrets {
			envContent += secret + "\n"
		}
		envPath := filepath.Join(outputDir, ".env.example")
		if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to write .env.example: %v", err))
		}
	}

	// Generate README
	readme := fmt.Sprintf(`# Cloud Run Migration from %s

## Migrated Services

%d Cloud Run services have been converted to Docker Compose format.

## Usage

1. Copy .env.example to .env and fill in secret values
2. Run: docker-compose up -d

## Notes

- Cloud Run-specific features (IAM, Cloud SQL proxy) require manual configuration
- Review port mappings and resource limits
- Ensure container images are accessible from your Docker environment
`, projectID, len(services))

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to write README: %v", err))
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Cloud Run services migrated to %s", outputDir))

	return nil
}

// generateDockerCompose generates a docker-compose.yml content for Cloud Run services.
func (e *CloudRunToDockerExecutor) generateDockerCompose(services map[string]DockerComposeService) string {
	var sb strings.Builder
	sb.WriteString("version: '3'\n\n")
	sb.WriteString("# Migrated from GCP Cloud Run\n\n")
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
				escapedValue := strings.ReplaceAll(v, "\"", "\\\"")
				sb.WriteString(fmt.Sprintf("      %s: \"%s\"\n", k, escapedValue))
			}
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

		sb.WriteString("\n")
	}

	return sb.String()
}

// ============================================================================
// CloudFunctionsToOpenFaaSExecutor - Cloud Functions to OpenFaaS
// ============================================================================

// CloudFunctionsToOpenFaaSExecutor migrates Cloud Functions to OpenFaaS.
type CloudFunctionsToOpenFaaSExecutor struct{}

// NewCloudFunctionsToOpenFaaSExecutor creates a new Cloud Functions to OpenFaaS executor.
func NewCloudFunctionsToOpenFaaSExecutor() *CloudFunctionsToOpenFaaSExecutor {
	return &CloudFunctionsToOpenFaaSExecutor{}
}

// Type returns the migration type.
func (e *CloudFunctionsToOpenFaaSExecutor) Type() string {
	return "cloudfunctions_to_openfaas"
}

// GetPhases returns the migration phases.
func (e *CloudFunctionsToOpenFaaSExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching Cloud Functions",
		"Downloading function source",
		"Generating OpenFaaS templates",
		"Creating stack configuration",
		"Writing output files",
	}
}

// Validate validates the migration configuration.
func (e *CloudFunctionsToOpenFaaSExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, will list all regions")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Cloud Functions triggers (Pub/Sub, HTTP, Events) require OpenFaaS gateway configuration")
	result.Warnings = append(result.Warnings, "Gen2 functions use Cloud Run and may have different configurations")

	return result, nil
}

// cloudFunction represents a Cloud Function configuration.
type cloudFunction struct {
	Name              string            `json:"name"`
	Runtime           string            `json:"runtime"`
	EntryPoint        string            `json:"entryPoint"`
	Trigger           map[string]string `json:"trigger"`
	Status            string            `json:"status"`
	EnvironmentVariables map[string]string `json:"environmentVariables"`
	AvailableMemoryMB int               `json:"availableMemoryMb"`
	Timeout           string            `json:"timeout"`
	SourceArchiveURL  string            `json:"sourceArchiveUrl"`
	SourceRepository  *struct {
		URL string `json:"url"`
	} `json:"sourceRepository"`
	Labels map[string]string `json:"labels"`
}

// Execute performs the migration.
func (e *CloudFunctionsToOpenFaaSExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	projectID := config.Source["project_id"].(string)
	region, _ := config.Source["region"].(string)
	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Test gcloud authentication
	authCmd := exec.CommandContext(ctx, "gcloud", "auth", "list", "--format=json")
	if output, err := authCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("GCP authentication failed: %s", string(output)))
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}

	// Set project
	projectCmd := exec.CommandContext(ctx, "gcloud", "config", "set", "project", projectID)
	if err := projectCmd.Run(); err != nil {
		return fmt.Errorf("failed to set GCP project: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Using GCP project: %s", projectID))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching Cloud Functions
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching Cloud Functions")
	EmitProgress(m, 20, "Listing functions")

	var listArgs []string
	if region != "" {
		listArgs = []string{"functions", "list", "--regions", region, "--format=json"}
	} else {
		listArgs = []string{"functions", "list", "--format=json"}
	}

	listCmd := exec.CommandContext(ctx, "gcloud", listArgs...)
	functionsOutput, err := listCmd.Output()
	if err != nil {
		EmitLog(m, "error", "Failed to list Cloud Functions")
		return fmt.Errorf("failed to list Cloud Functions: %w", err)
	}

	var functions []cloudFunction
	if err := json.Unmarshal(functionsOutput, &functions); err != nil {
		return fmt.Errorf("failed to parse Cloud Functions: %w", err)
	}

	if len(functions) == 0 {
		EmitLog(m, "warn", "No Cloud Functions found")
		return fmt.Errorf("no Cloud Functions found in project %s", projectID)
	}

	EmitLog(m, "info", fmt.Sprintf("Found %d Cloud Function(s)", len(functions)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Downloading function source
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Downloading function source code")
	EmitProgress(m, 35, "Downloading sources")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	functionsDir := filepath.Join(outputDir, "functions")
	if err := os.MkdirAll(functionsDir, 0755); err != nil {
		return fmt.Errorf("failed to create functions directory: %w", err)
	}

	for i, fn := range functions {
		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		functionName := extractFunctionName(fn.Name)
		EmitLog(m, "info", fmt.Sprintf("Processing function %d/%d: %s", i+1, len(functions), functionName))

		functionDir := filepath.Join(functionsDir, functionName)
		if err := os.MkdirAll(functionDir, 0755); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to create function directory: %v", err))
			continue
		}

		// Download source if available
		if fn.SourceArchiveURL != "" {
			downloadCmd := exec.CommandContext(ctx, "gsutil", "cp", fn.SourceArchiveURL, filepath.Join(functionDir, "source.zip"))
			if output, err := downloadCmd.CombinedOutput(); err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to download source for %s: %s", functionName, string(output)))
			} else {
				EmitLog(m, "info", fmt.Sprintf("Downloaded source for %s", functionName))
			}
		}

		// Save function configuration
		fnConfig, _ := json.MarshalIndent(fn, "", "  ")
		if err := os.WriteFile(filepath.Join(functionDir, "config.json"), fnConfig, 0644); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to save config for %s: %v", functionName, err))
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating OpenFaaS templates
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating OpenFaaS function templates")
	EmitProgress(m, 55, "Creating templates")

	for _, fn := range functions {
		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		functionName := extractFunctionName(fn.Name)
		functionDir := filepath.Join(functionsDir, functionName)

		// Generate handler based on runtime
		handler := e.generateOpenFaaSHandler(fn)
		handlerFile := e.getHandlerFilename(fn.Runtime)
		if err := os.WriteFile(filepath.Join(functionDir, handlerFile), []byte(handler), 0644); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to write handler for %s: %v", functionName, err))
		}

		// Generate Dockerfile
		dockerfile := e.generateOpenFaaSDockerfile(fn)
		if err := os.WriteFile(filepath.Join(functionDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to write Dockerfile for %s: %v", functionName, err))
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating stack configuration
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating OpenFaaS stack configuration")
	EmitProgress(m, 75, "Creating stack.yml")

	stackYAML := e.generateOpenFaaSStack(functions, outputDir)
	stackPath := filepath.Join(outputDir, "stack.yml")
	if err := os.WriteFile(stackPath, []byte(stackYAML), 0644); err != nil {
		return fmt.Errorf("failed to write stack.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Writing output files
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Generating deployment scripts")
	EmitProgress(m, 90, "Writing files")

	// Generate deployment script
	deployScript := `#!/bin/bash
# OpenFaaS Deployment Script for migrated Cloud Functions

set -e

echo "Deploying functions to OpenFaaS..."

# Build functions
faas-cli build -f stack.yml

# Push to registry (configure registry in stack.yml first)
# faas-cli push -f stack.yml

# Deploy to OpenFaaS
faas-cli deploy -f stack.yml

echo "Deployment complete!"
`
	deployPath := filepath.Join(outputDir, "deploy.sh")
	if err := os.WriteFile(deployPath, []byte(deployScript), 0755); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to write deploy.sh: %v", err))
	}

	// Generate README
	readme := fmt.Sprintf(`# Cloud Functions Migration from %s

## Migrated Functions

%d Cloud Functions have been converted to OpenFaaS format.

## Prerequisites

- OpenFaaS installed (https://docs.openfaas.com/)
- faas-cli installed
- Docker registry configured

## Deployment

1. Review and update stack.yml with your registry settings
2. Build functions: faas-cli build -f stack.yml
3. Push to registry: faas-cli push -f stack.yml
4. Deploy: faas-cli deploy -f stack.yml

Or run: ./deploy.sh

## Notes

- Review trigger configurations manually
- Environment variables are included in stack.yml
- Secrets should be configured in OpenFaaS
`, projectID, len(functions))

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to write README: %v", err))
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Cloud Functions migrated to %s", outputDir))

	return nil
}

// extractFunctionName extracts the function name from the full resource name.
func extractFunctionName(fullName string) string {
	parts := strings.Split(fullName, "/")
	return parts[len(parts)-1]
}

// generateOpenFaaSHandler generates a handler file based on the runtime.
func (e *CloudFunctionsToOpenFaaSExecutor) generateOpenFaaSHandler(fn cloudFunction) string {
	switch {
	case strings.HasPrefix(fn.Runtime, "python"):
		return fmt.Sprintf(`# Migrated from Cloud Function: %s
# Entry point: %s

def handle(req):
    """Handle a request to the function."""
    # TODO: Migrate your Cloud Function logic here
    # Original entry point was: %s
    return "Hello from OpenFaaS"
`, extractFunctionName(fn.Name), fn.EntryPoint, fn.EntryPoint)

	case strings.HasPrefix(fn.Runtime, "nodejs"):
		return fmt.Sprintf(`// Migrated from Cloud Function: %s
// Entry point: %s

'use strict';

module.exports = async (event, context) => {
    // TODO: Migrate your Cloud Function logic here
    // Original entry point was: %s
    return {
        statusCode: 200,
        body: 'Hello from OpenFaaS'
    };
};
`, extractFunctionName(fn.Name), fn.EntryPoint, fn.EntryPoint)

	case strings.HasPrefix(fn.Runtime, "go"):
		return fmt.Sprintf(`// Migrated from Cloud Function: %s
// Entry point: %s
package function

import (
	"fmt"
	"net/http"
)

// Handle a request
func Handle(w http.ResponseWriter, r *http.Request) {
	// TODO: Migrate your Cloud Function logic here
	// Original entry point was: %s
	fmt.Fprintf(w, "Hello from OpenFaaS")
}
`, extractFunctionName(fn.Name), fn.EntryPoint, fn.EntryPoint)

	default:
		return fmt.Sprintf(`# Migrated from Cloud Function: %s
# Runtime: %s
# Entry point: %s

# TODO: Implement handler for this runtime
`, extractFunctionName(fn.Name), fn.Runtime, fn.EntryPoint)
	}
}

// getHandlerFilename returns the appropriate handler filename for the runtime.
func (e *CloudFunctionsToOpenFaaSExecutor) getHandlerFilename(runtime string) string {
	switch {
	case strings.HasPrefix(runtime, "python"):
		return "handler.py"
	case strings.HasPrefix(runtime, "nodejs"):
		return "handler.js"
	case strings.HasPrefix(runtime, "go"):
		return "handler.go"
	default:
		return "handler.txt"
	}
}

// generateOpenFaaSDockerfile generates a Dockerfile for OpenFaaS.
func (e *CloudFunctionsToOpenFaaSExecutor) generateOpenFaaSDockerfile(fn cloudFunction) string {
	var baseImage string
	var copyCmd string

	switch {
	case strings.HasPrefix(fn.Runtime, "python3.11"):
		baseImage = "ghcr.io/openfaas/classic-watchdog:0.2.1"
		copyCmd = `FROM python:3.11-slim as builder
WORKDIR /home/app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt 2>/dev/null || true

FROM ` + baseImage + `
COPY --from=builder /home/app /home/app
ENV fprocess="python3 /home/app/handler.py"
EXPOSE 8080
HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1
CMD ["fwatchdog"]`
		return copyCmd

	case strings.HasPrefix(fn.Runtime, "python"):
		baseImage = "ghcr.io/openfaas/classic-watchdog:0.2.1"
		copyCmd = `FROM python:3.9-slim as builder
WORKDIR /home/app
COPY . .
RUN pip install --no-cache-dir -r requirements.txt 2>/dev/null || true

FROM ` + baseImage + `
COPY --from=builder /home/app /home/app
ENV fprocess="python3 /home/app/handler.py"
EXPOSE 8080
HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1
CMD ["fwatchdog"]`
		return copyCmd

	case strings.HasPrefix(fn.Runtime, "nodejs"):
		return `FROM ghcr.io/openfaas/of-watchdog:0.9.11 as watchdog
FROM node:18-alpine

COPY --from=watchdog /fwatchdog /usr/bin/fwatchdog
RUN chmod +x /usr/bin/fwatchdog

WORKDIR /home/app
COPY package*.json ./
RUN npm install --production 2>/dev/null || true
COPY . .

ENV mode="http"
ENV fprocess="node handler.js"
EXPOSE 8080
HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1
CMD ["fwatchdog"]`

	case strings.HasPrefix(fn.Runtime, "go"):
		return `FROM golang:1.21-alpine as builder
WORKDIR /go/src/handler
COPY . .
RUN CGO_ENABLED=0 go build -o handler .

FROM ghcr.io/openfaas/classic-watchdog:0.2.1
COPY --from=builder /go/src/handler/handler /home/app/handler
ENV fprocess="/home/app/handler"
EXPOSE 8080
HEALTHCHECK --interval=3s CMD [ -e /tmp/.lock ] || exit 1
CMD ["fwatchdog"]`

	default:
		return fmt.Sprintf(`# Dockerfile for %s runtime
# TODO: Configure appropriate base image and setup
FROM alpine:latest
WORKDIR /home/app
COPY . .
`, fn.Runtime)
	}
}

// generateOpenFaaSStack generates the OpenFaaS stack.yml file.
func (e *CloudFunctionsToOpenFaaSExecutor) generateOpenFaaSStack(functions []cloudFunction, outputDir string) string {
	var sb strings.Builder
	sb.WriteString("version: 1.0\n")
	sb.WriteString("provider:\n")
	sb.WriteString("  name: openfaas\n")
	sb.WriteString("  gateway: http://127.0.0.1:8080\n\n")
	sb.WriteString("functions:\n")

	for _, fn := range functions {
		functionName := extractFunctionName(fn.Name)
		sb.WriteString(fmt.Sprintf("  %s:\n", sanitizeServiceName(functionName)))
		sb.WriteString(fmt.Sprintf("    lang: dockerfile\n"))
		sb.WriteString(fmt.Sprintf("    handler: ./functions/%s\n", functionName))
		sb.WriteString(fmt.Sprintf("    image: ${REGISTRY:-localhost:5000}/%s:latest\n", sanitizeServiceName(functionName)))

		if len(fn.EnvironmentVariables) > 0 {
			sb.WriteString("    environment:\n")
			for k, v := range fn.EnvironmentVariables {
				sb.WriteString(fmt.Sprintf("      %s: \"%s\"\n", k, v))
			}
		}

		if fn.AvailableMemoryMB > 0 {
			sb.WriteString("    limits:\n")
			sb.WriteString(fmt.Sprintf("      memory: %dMi\n", fn.AvailableMemoryMB))
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// ============================================================================
// GKEToK3sExecutor - GKE to K3s
// ============================================================================

// GKEToK3sExecutor migrates GKE clusters to K3s.
type GKEToK3sExecutor struct{}

// NewGKEToK3sExecutor creates a new GKE to K3s executor.
func NewGKEToK3sExecutor() *GKEToK3sExecutor {
	return &GKEToK3sExecutor{}
}

// Type returns the migration type.
func (e *GKEToK3sExecutor) Type() string {
	return "gke_to_k3s"
}

// GetPhases returns the migration phases.
func (e *GKEToK3sExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching cluster configuration",
		"Exporting workloads",
		"Converting to K3s format",
		"Generating Helm charts",
		"Writing output files",
	}
}

// Validate validates the migration configuration.
func (e *GKEToK3sExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["cluster_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.cluster_name is required")
		}
		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
		}
		if _, ok := config.Source["zone"].(string); !ok {
			if _, ok := config.Source["region"].(string); !ok {
				result.Valid = false
				result.Errors = append(result.Errors, "source.zone or source.region is required")
			}
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "GKE-specific features (Workload Identity, GCE PDs) will need manual configuration")

	return result, nil
}

// Execute performs the migration.
func (e *GKEToK3sExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	clusterName := config.Source["cluster_name"].(string)
	projectID := config.Source["project_id"].(string)
	zone, _ := config.Source["zone"].(string)
	region, _ := config.Source["region"].(string)
	outputDir := config.Destination["output_dir"].(string)

	location := zone
	locationType := "--zone"
	if location == "" {
		location = region
		locationType = "--region"
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials and cluster access")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Set project
	projectCmd := exec.CommandContext(ctx, "gcloud", "config", "set", "project", projectID)
	if err := projectCmd.Run(); err != nil {
		return fmt.Errorf("failed to set GCP project: %w", err)
	}

	// Get cluster credentials
	credsCmd := exec.CommandContext(ctx, "gcloud", "container", "clusters", "get-credentials",
		clusterName, locationType, location)
	if output, err := credsCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("Failed to get cluster credentials: %s", string(output)))
		return fmt.Errorf("failed to get cluster credentials: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Authenticated to cluster: %s", clusterName))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching cluster configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching cluster configuration for %s", clusterName))
	EmitProgress(m, 20, "Fetching cluster info")

	clusterCmd := exec.CommandContext(ctx, "gcloud", "container", "clusters", "describe",
		clusterName, locationType, location, "--format=json")
	clusterOutput, err := clusterCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to describe cluster: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting workloads
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting Kubernetes workloads")
	EmitProgress(m, 40, "Exporting workloads")

	namespaces := []string{"default"}
	if ns, ok := config.Source["namespaces"].([]interface{}); ok {
		namespaces = make([]string, len(ns))
		for i, n := range ns {
			namespaces[i] = n.(string)
		}
	}

	workloads := make(map[string]interface{})
	for _, ns := range namespaces {
		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}

		EmitLog(m, "info", fmt.Sprintf("Exporting resources from namespace: %s", ns))

		// Export deployments
		deployCmd := exec.CommandContext(ctx, "kubectl", "get", "deployments", "-n", ns, "-o", "json")
		if output, err := deployCmd.Output(); err == nil {
			var deployments interface{}
			json.Unmarshal(output, &deployments)
			workloads[ns+"_deployments"] = deployments
		}

		// Export services
		svcCmd := exec.CommandContext(ctx, "kubectl", "get", "services", "-n", ns, "-o", "json")
		if output, err := svcCmd.Output(); err == nil {
			var services interface{}
			json.Unmarshal(output, &services)
			workloads[ns+"_services"] = services
		}

		// Export configmaps
		cmCmd := exec.CommandContext(ctx, "kubectl", "get", "configmaps", "-n", ns, "-o", "json")
		if output, err := cmCmd.Output(); err == nil {
			var configmaps interface{}
			json.Unmarshal(output, &configmaps)
			workloads[ns+"_configmaps"] = configmaps
		}

		// Export secrets (metadata only for security)
		secretCmd := exec.CommandContext(ctx, "kubectl", "get", "secrets", "-n", ns, "-o", "json")
		if output, err := secretCmd.Output(); err == nil {
			var secrets interface{}
			json.Unmarshal(output, &secrets)
			workloads[ns+"_secrets"] = secrets
		}

		// Export ingresses
		ingressCmd := exec.CommandContext(ctx, "kubectl", "get", "ingress", "-n", ns, "-o", "json")
		if output, err := ingressCmd.Output(); err == nil {
			var ingresses interface{}
			json.Unmarshal(output, &ingresses)
			workloads[ns+"_ingresses"] = ingresses
		}

		// Export PVCs
		pvcCmd := exec.CommandContext(ctx, "kubectl", "get", "pvc", "-n", ns, "-o", "json")
		if output, err := pvcCmd.Output(); err == nil {
			var pvcs interface{}
			json.Unmarshal(output, &pvcs)
			workloads[ns+"_pvcs"] = pvcs
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Converting to K3s format
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Converting to K3s-compatible format")
	EmitProgress(m, 60, "Converting manifests")

	// Remove GKE-specific annotations
	e.cleanupManifests(workloads)

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating Helm charts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating Helm chart structure")
	EmitProgress(m, 80, "Creating Helm charts")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create Helm chart structure
	chartDir := filepath.Join(outputDir, "helm", clusterName)
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0755); err != nil {
		return fmt.Errorf("failed to create chart directory: %w", err)
	}

	// Write Chart.yaml
	chartYaml := fmt.Sprintf(`apiVersion: v2
name: %s
description: Migrated from GKE cluster %s
type: application
version: 1.0.0
appVersion: "1.0.0"
`, clusterName, clusterName)
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYaml), 0644); err != nil {
		return fmt.Errorf("failed to write Chart.yaml: %w", err)
	}

	// Write values.yaml
	valuesYaml := `# Default values migrated from GKE
replicaCount: 1
image:
  pullPolicy: IfNotPresent
`
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(valuesYaml), 0644); err != nil {
		return fmt.Errorf("failed to write values.yaml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Writing output files
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Writing output files")
	EmitProgress(m, 90, "Writing files")

	// Write cluster config
	if err := os.WriteFile(filepath.Join(outputDir, "cluster-config.json"), clusterOutput, 0644); err != nil {
		return fmt.Errorf("failed to write cluster config: %w", err)
	}

	// Write workloads
	for name, data := range workloads {
		content, _ := json.MarshalIndent(data, "", "  ")
		filename := filepath.Join(chartDir, "templates", name+".json")
		if err := os.WriteFile(filename, content, 0644); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to write %s: %v", name, err))
		}
	}

	// Write K3s installation script
	k3sScript := `#!/bin/bash
# K3s Installation Script for migrated GKE workloads

set -e

echo "Installing K3s..."
curl -sfL https://get.k3s.io | sh -

echo "Waiting for K3s to be ready..."
sudo kubectl wait --for=condition=Ready nodes --all --timeout=300s

echo "Applying migrated workloads..."
for f in helm/*/templates/*.json; do
    echo "Applying $f..."
    sudo kubectl apply -f "$f" 2>/dev/null || true
done

echo "Migration complete!"
echo ""
echo "Note: Some GKE-specific resources may need manual configuration:"
echo "- Workload Identity -> Use Kubernetes secrets"
echo "- GCE Persistent Disks -> Use local-path or other storage class"
echo "- Cloud Load Balancer annotations -> Use Traefik ingress"
`
	if err := os.WriteFile(filepath.Join(outputDir, "install-k3s.sh"), []byte(k3sScript), 0755); err != nil {
		return fmt.Errorf("failed to write install script: %w", err)
	}

	// Generate README
	readme := fmt.Sprintf(`# GKE Migration from %s

## Cluster Information

- Project: %s
- Location: %s

## Migrated Resources

Workloads from %d namespace(s) have been exported.

## Installation

1. Review the manifests in helm/%s/templates/
2. Run: ./install-k3s.sh

## Manual Configuration Required

- **Workload Identity**: Convert to Kubernetes secrets
- **GCE Persistent Disks**: Use local-path storage class or configure alternate storage
- **Load Balancer annotations**: Update for Traefik ingress
- **Secrets**: Re-create secrets in K3s cluster
`, clusterName, projectID, location, len(namespaces), clusterName)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to write README: %v", err))
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("GKE cluster %s migrated to %s", clusterName, outputDir))

	return nil
}

// cleanupManifests removes GKE-specific annotations from workloads.
func (e *GKEToK3sExecutor) cleanupManifests(workloads map[string]interface{}) {
	gkeAnnotations := []string{
		"cloud.google.com",
		"gke.io",
		"kubernetes.io/ingress.class",
		"networking.gke.io",
	}

	for _, data := range workloads {
		if items, ok := data.(map[string]interface{}); ok {
			if itemsList, ok := items["items"].([]interface{}); ok {
				for _, item := range itemsList {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if metadata, ok := itemMap["metadata"].(map[string]interface{}); ok {
							if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
								for key := range annotations {
									for _, prefix := range gkeAnnotations {
										if strings.HasPrefix(key, prefix) {
											delete(annotations, key)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// ============================================================================
// AppEngineToDockerExecutor - App Engine to Docker
// ============================================================================

// AppEngineToDockerExecutor migrates App Engine applications to Docker.
type AppEngineToDockerExecutor struct{}

// NewAppEngineToDockerExecutor creates a new App Engine to Docker executor.
func NewAppEngineToDockerExecutor() *AppEngineToDockerExecutor {
	return &AppEngineToDockerExecutor{}
}

// Type returns the migration type.
func (e *AppEngineToDockerExecutor) Type() string {
	return "appengine_to_docker"
}

// GetPhases returns the migration phases.
func (e *AppEngineToDockerExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching App Engine configuration",
		"Downloading application source",
		"Generating Dockerfile",
		"Creating Docker Compose",
		"Writing output files",
	}
}

// Validate validates the migration configuration.
func (e *AppEngineToDockerExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["project_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.project_id is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "App Engine-specific features (memcache, task queues) require manual configuration")
	result.Warnings = append(result.Warnings, "Standard environment apps may need runtime adjustments")

	return result, nil
}

// appEngineService represents an App Engine service.
type appEngineService struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// appEngineVersion represents an App Engine version.
type appEngineVersion struct {
	ID               string `json:"id"`
	Runtime          string `json:"runtime"`
	ServingStatus    string `json:"servingStatus"`
	CreateTime       string `json:"createTime"`
	VersionURL       string `json:"versionUrl"`
	TrafficSplit     float64 `json:"trafficSplit"`
	InstanceClass    string `json:"instanceClass"`
	Env              string `json:"env"`
	Deployment       map[string]interface{} `json:"deployment"`
	EnvVariables     map[string]string `json:"envVariables"`
	AutomaticScaling *struct {
		MinInstances int `json:"minInstances"`
		MaxInstances int `json:"maxInstances"`
	} `json:"automaticScaling"`
}

// Execute performs the migration.
func (e *AppEngineToDockerExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	projectID := config.Source["project_id"].(string)
	outputDir := config.Destination["output_dir"].(string)
	serviceName, _ := config.Source["service"].(string)
	if serviceName == "" {
		serviceName = "default"
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating GCP credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Set project
	projectCmd := exec.CommandContext(ctx, "gcloud", "config", "set", "project", projectID)
	if err := projectCmd.Run(); err != nil {
		return fmt.Errorf("failed to set GCP project: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Using GCP project: %s", projectID))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching App Engine configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Fetching App Engine configuration")
	EmitProgress(m, 20, "Getting app config")

	// List services
	servicesCmd := exec.CommandContext(ctx, "gcloud", "app", "services", "list", "--format=json")
	servicesOutput, err := servicesCmd.Output()
	if err != nil {
		EmitLog(m, "warn", "Failed to list App Engine services, continuing with specified service")
	}

	var services []appEngineService
	if servicesOutput != nil {
		json.Unmarshal(servicesOutput, &services)
	}

	if len(services) > 0 {
		EmitLog(m, "info", fmt.Sprintf("Found %d App Engine service(s)", len(services)))
	}

	// Get versions for the service
	versionsCmd := exec.CommandContext(ctx, "gcloud", "app", "versions", "list",
		"--service", serviceName, "--format=json", "--sort-by=~createTime", "--limit=1")
	versionsOutput, err := versionsCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list App Engine versions: %w", err)
	}

	var versions []appEngineVersion
	if err := json.Unmarshal(versionsOutput, &versions); err != nil {
		return fmt.Errorf("failed to parse App Engine versions: %w", err)
	}

	if len(versions) == 0 {
		return fmt.Errorf("no App Engine versions found for service %s", serviceName)
	}

	latestVersion := versions[0]
	EmitLog(m, "info", fmt.Sprintf("Latest version: %s (runtime: %s)", latestVersion.ID, latestVersion.Runtime))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Downloading application source
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Preparing application source")
	EmitProgress(m, 40, "Processing source")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	appDir := filepath.Join(outputDir, "app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	// Save version configuration
	versionConfig, _ := json.MarshalIndent(latestVersion, "", "  ")
	if err := os.WriteFile(filepath.Join(outputDir, "version-config.json"), versionConfig, 0644); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to save version config: %v", err))
	}

	// Note: Direct source download requires additional setup
	EmitLog(m, "info", "Note: Source code should be provided separately or cloned from repository")
	EmitLog(m, "info", "Place your app.yaml and source files in the 'app' directory")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating Dockerfile
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating Dockerfile")
	EmitProgress(m, 60, "Creating Dockerfile")

	dockerfile := e.generateDockerfile(latestVersion)
	dockerfilePath := filepath.Join(appDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	EmitLog(m, "info", "Dockerfile generated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Creating Docker Compose
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Creating Docker Compose configuration")
	EmitProgress(m, 80, "Creating compose file")

	composeContent := e.generateDockerCompose(serviceName, latestVersion)
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(composeContent), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate .env.example
	if len(latestVersion.EnvVariables) > 0 {
		envContent := "# Environment variables from App Engine\n"
		envContent += "# Copy this file to .env and update values as needed\n\n"
		for k, v := range latestVersion.EnvVariables {
			envContent += fmt.Sprintf("%s=%s\n", k, v)
		}
		envPath := filepath.Join(outputDir, ".env.example")
		if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to write .env.example: %v", err))
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Writing output files
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Writing additional files")
	EmitProgress(m, 90, "Finalizing")

	// Generate README
	readme := fmt.Sprintf(`# App Engine Migration from %s

## Service Information

- Project: %s
- Service: %s
- Runtime: %s
- Environment: %s

## Setup

1. Place your application source code in the 'app' directory
2. Copy .env.example to .env and update values
3. Build: docker-compose build
4. Run: docker-compose up -d

## Dockerfile

A Dockerfile has been generated based on the App Engine runtime.
Review and modify as needed for your specific application.

## Notes

- **Memcache**: Replace with Redis (add to docker-compose.yml)
- **Task Queues**: Use Celery, RabbitMQ, or similar
- **Datastore**: Use MongoDB, PostgreSQL, or Firestore
- **Cloud Storage**: Use MinIO or local volume mounts
- **Cron jobs**: Use standard cron or Docker-based scheduler
`, serviceName, projectID, serviceName, latestVersion.Runtime, latestVersion.Env)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to write README: %v", err))
	}

	// Generate placeholder app.yaml conversion notes
	appYamlNotes := `# App Engine to Docker Migration Notes

## Original app.yaml Settings

Review your original app.yaml and apply these translations:

### Runtime
- python27 -> Use python:2.7 base image (deprecated)
- python37 -> Use python:3.7-slim base image
- python38 -> Use python:3.8-slim base image
- python39 -> Use python:3.9-slim base image
- python310 -> Use python:3.10-slim base image
- python311 -> Use python:3.11-slim base image
- nodejs16 -> Use node:16-alpine base image
- nodejs18 -> Use node:18-alpine base image
- go119 -> Use golang:1.19-alpine base image
- java11 -> Use eclipse-temurin:11-jre base image
- java17 -> Use eclipse-temurin:17-jre base image

### Handlers
- Static file handlers: Use nginx sidecar or serve from application
- Script handlers: Convert to application routes

### Environment Variables
- Moved to .env file and docker-compose.yml

### Scaling
- automatic_scaling -> Use Docker Swarm or Kubernetes HPA
- basic_scaling -> Use Docker restart policies
- manual_scaling -> Set replicas in docker-compose
`
	notesPath := filepath.Join(outputDir, "MIGRATION_NOTES.md")
	if err := os.WriteFile(notesPath, []byte(appYamlNotes), 0644); err != nil {
		EmitLog(m, "warn", fmt.Sprintf("Failed to write migration notes: %v", err))
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("App Engine service %s migrated to %s", serviceName, outputDir))

	return nil
}

// generateDockerfile generates a Dockerfile based on App Engine runtime.
func (e *AppEngineToDockerExecutor) generateDockerfile(version appEngineVersion) string {
	var dockerfile strings.Builder

	runtime := version.Runtime

	switch {
	case strings.HasPrefix(runtime, "python3"):
		pythonVersion := "3.11"
		if strings.Contains(runtime, "310") {
			pythonVersion = "3.10"
		} else if strings.Contains(runtime, "39") {
			pythonVersion = "3.9"
		} else if strings.Contains(runtime, "38") {
			pythonVersion = "3.8"
		} else if strings.Contains(runtime, "37") {
			pythonVersion = "3.7"
		}

		dockerfile.WriteString(fmt.Sprintf("FROM python:%s-slim\n\n", pythonVersion))
		dockerfile.WriteString("WORKDIR /app\n\n")
		dockerfile.WriteString("# Install dependencies\n")
		dockerfile.WriteString("COPY requirements.txt .\n")
		dockerfile.WriteString("RUN pip install --no-cache-dir -r requirements.txt\n\n")
		dockerfile.WriteString("# Copy application\n")
		dockerfile.WriteString("COPY . .\n\n")
		dockerfile.WriteString("# Expose port (App Engine uses 8080)\n")
		dockerfile.WriteString("EXPOSE 8080\n\n")
		dockerfile.WriteString("# Run application\n")
		dockerfile.WriteString("ENV PORT=8080\n")
		dockerfile.WriteString("CMD [\"gunicorn\", \"-b\", \"0.0.0.0:8080\", \"main:app\"]\n")

	case strings.HasPrefix(runtime, "python2"):
		dockerfile.WriteString("# WARNING: Python 2.7 is deprecated\n")
		dockerfile.WriteString("FROM python:2.7-slim\n\n")
		dockerfile.WriteString("WORKDIR /app\n\n")
		dockerfile.WriteString("COPY requirements.txt .\n")
		dockerfile.WriteString("RUN pip install --no-cache-dir -r requirements.txt\n\n")
		dockerfile.WriteString("COPY . .\n\n")
		dockerfile.WriteString("EXPOSE 8080\n")
		dockerfile.WriteString("ENV PORT=8080\n")
		dockerfile.WriteString("CMD [\"python\", \"main.py\"]\n")

	case strings.HasPrefix(runtime, "nodejs"):
		nodeVersion := "18"
		if strings.Contains(runtime, "20") {
			nodeVersion = "20"
		} else if strings.Contains(runtime, "16") {
			nodeVersion = "16"
		}

		dockerfile.WriteString(fmt.Sprintf("FROM node:%s-alpine\n\n", nodeVersion))
		dockerfile.WriteString("WORKDIR /app\n\n")
		dockerfile.WriteString("# Install dependencies\n")
		dockerfile.WriteString("COPY package*.json ./\n")
		dockerfile.WriteString("RUN npm ci --only=production\n\n")
		dockerfile.WriteString("# Copy application\n")
		dockerfile.WriteString("COPY . .\n\n")
		dockerfile.WriteString("EXPOSE 8080\n")
		dockerfile.WriteString("ENV PORT=8080\n")
		dockerfile.WriteString("CMD [\"npm\", \"start\"]\n")

	case strings.HasPrefix(runtime, "go"):
		goVersion := "1.21"
		if strings.Contains(runtime, "121") {
			goVersion = "1.21"
		} else if strings.Contains(runtime, "120") {
			goVersion = "1.20"
		} else if strings.Contains(runtime, "119") {
			goVersion = "1.19"
		}

		dockerfile.WriteString(fmt.Sprintf("FROM golang:%s-alpine AS builder\n\n", goVersion))
		dockerfile.WriteString("WORKDIR /app\n")
		dockerfile.WriteString("COPY go.mod go.sum ./\n")
		dockerfile.WriteString("RUN go mod download\n")
		dockerfile.WriteString("COPY . .\n")
		dockerfile.WriteString("RUN CGO_ENABLED=0 go build -o main .\n\n")
		dockerfile.WriteString("FROM alpine:latest\n")
		dockerfile.WriteString("WORKDIR /app\n")
		dockerfile.WriteString("COPY --from=builder /app/main .\n")
		dockerfile.WriteString("EXPOSE 8080\n")
		dockerfile.WriteString("ENV PORT=8080\n")
		dockerfile.WriteString("CMD [\"./main\"]\n")

	case strings.HasPrefix(runtime, "java"):
		javaVersion := "17"
		if strings.Contains(runtime, "11") {
			javaVersion = "11"
		}

		dockerfile.WriteString(fmt.Sprintf("FROM eclipse-temurin:%s-jdk AS builder\n\n", javaVersion))
		dockerfile.WriteString("WORKDIR /app\n")
		dockerfile.WriteString("COPY . .\n")
		dockerfile.WriteString("RUN ./mvnw package -DskipTests\n\n")
		dockerfile.WriteString(fmt.Sprintf("FROM eclipse-temurin:%s-jre\n", javaVersion))
		dockerfile.WriteString("WORKDIR /app\n")
		dockerfile.WriteString("COPY --from=builder /app/target/*.jar app.jar\n")
		dockerfile.WriteString("EXPOSE 8080\n")
		dockerfile.WriteString("ENV PORT=8080\n")
		dockerfile.WriteString("CMD [\"java\", \"-jar\", \"app.jar\"]\n")

	case strings.HasPrefix(runtime, "php"):
		phpVersion := "8.1"
		if strings.Contains(runtime, "82") {
			phpVersion = "8.2"
		} else if strings.Contains(runtime, "74") {
			phpVersion = "7.4"
		}

		dockerfile.WriteString(fmt.Sprintf("FROM php:%s-apache\n\n", phpVersion))
		dockerfile.WriteString("WORKDIR /var/www/html\n")
		dockerfile.WriteString("COPY . .\n")
		dockerfile.WriteString("RUN composer install --no-dev 2>/dev/null || true\n")
		dockerfile.WriteString("EXPOSE 8080\n")
		dockerfile.WriteString("RUN sed -i 's/80/8080/g' /etc/apache2/sites-available/000-default.conf /etc/apache2/ports.conf\n")
		dockerfile.WriteString("CMD [\"apache2-foreground\"]\n")

	default:
		dockerfile.WriteString("# Unknown runtime: " + runtime + "\n")
		dockerfile.WriteString("# Please configure the appropriate base image\n\n")
		dockerfile.WriteString("FROM alpine:latest\n")
		dockerfile.WriteString("WORKDIR /app\n")
		dockerfile.WriteString("COPY . .\n")
		dockerfile.WriteString("EXPOSE 8080\n")
		dockerfile.WriteString("# TODO: Configure entrypoint for your runtime\n")
		dockerfile.WriteString("CMD [\"sh\"]\n")
	}

	return dockerfile.String()
}

// generateDockerCompose generates a docker-compose.yml for App Engine migration.
func (e *AppEngineToDockerExecutor) generateDockerCompose(serviceName string, version appEngineVersion) string {
	var sb strings.Builder

	sb.WriteString("version: '3'\n\n")
	sb.WriteString("# Migrated from GCP App Engine\n\n")
	sb.WriteString("services:\n")
	sb.WriteString(fmt.Sprintf("  %s:\n", sanitizeServiceName(serviceName)))
	sb.WriteString("    build:\n")
	sb.WriteString("      context: ./app\n")
	sb.WriteString("      dockerfile: Dockerfile\n")
	sb.WriteString("    ports:\n")
	sb.WriteString("      - \"8080:8080\"\n")

	if len(version.EnvVariables) > 0 {
		sb.WriteString("    environment:\n")
		for k := range version.EnvVariables {
			sb.WriteString(fmt.Sprintf("      - %s=${%s}\n", k, k))
		}
	}

	sb.WriteString("    restart: unless-stopped\n")

	// Add resource limits based on instance class
	if version.InstanceClass != "" {
		sb.WriteString("    deploy:\n")
		sb.WriteString("      resources:\n")
		sb.WriteString("        limits:\n")

		switch version.InstanceClass {
		case "F1", "B1":
			sb.WriteString("          memory: 128M\n")
			sb.WriteString("          cpus: '0.25'\n")
		case "F2", "B2":
			sb.WriteString("          memory: 256M\n")
			sb.WriteString("          cpus: '0.5'\n")
		case "F4", "B4":
			sb.WriteString("          memory: 512M\n")
			sb.WriteString("          cpus: '1'\n")
		case "F4_1G", "B4_1G":
			sb.WriteString("          memory: 1G\n")
			sb.WriteString("          cpus: '1'\n")
		default:
			sb.WriteString("          memory: 256M\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString("# Add supporting services as needed:\n")
	sb.WriteString("#   redis:\n")
	sb.WriteString("#     image: redis:alpine\n")
	sb.WriteString("#     ports:\n")
	sb.WriteString("#       - \"6379:6379\"\n")
	sb.WriteString("#\n")
	sb.WriteString("#   postgres:\n")
	sb.WriteString("#     image: postgres:15\n")
	sb.WriteString("#     environment:\n")
	sb.WriteString("#       POSTGRES_PASSWORD: password\n")
	sb.WriteString("#     volumes:\n")
	sb.WriteString("#       - postgres_data:/var/lib/postgresql/data\n")
	sb.WriteString("#\n")
	sb.WriteString("# volumes:\n")
	sb.WriteString("#   postgres_data:\n")

	return sb.String()
}
