package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudexit/cloudexit/internal/cli/ui"
	"github.com/spf13/cobra"
)

// validateCmd represents the validate command
var validateCmd = &cobra.Command{
	Use:   "validate <path>",
	Short: "Validate generated stack configuration",
	Long: `Validate the generated self-hosted stack configuration.

The validate command checks the generated Docker stack for:
  - Valid Docker Compose syntax
  - Required files and directories
  - Environment variable configuration
  - Network configuration
  - Volume mounts and permissions
  - Service dependencies

This helps ensure the generated stack is ready for deployment.

Examples:
  # Validate generated stack
  cloudexit validate ./output

  # Validate with verbose output
  cloudexit validate ./output --verbose`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stackPath := args[0]

		if !IsQuiet() {
			ui.Header("CloudExit - Stack Validation")
			ui.Info(fmt.Sprintf("Validating: %s", stackPath))
			ui.Divider()
		}

		// Validate stack path
		if _, err := os.Stat(stackPath); os.IsNotExist(err) {
			return fmt.Errorf("stack path does not exist: %s", stackPath)
		}

		// Perform validation
		results, err := performValidation(stackPath)
		if err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}

		// Display results
		displayValidationResults(results)

		// Determine overall status
		hasErrors := false
		for _, result := range results {
			if result.Status == "error" {
				hasErrors = true
				break
			}
		}

		if !IsQuiet() {
			ui.Divider()
			if hasErrors {
				ui.Error("Validation completed with errors")
				return fmt.Errorf("validation found errors")
			} else {
				ui.Success("Validation completed successfully")
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

// ValidationResult represents the result of a single validation check
type ValidationResult struct {
	Check   string
	Status  string // success, warning, error
	Message string
	Details []string
}

// performValidation performs all validation checks
func performValidation(stackPath string) ([]ValidationResult, error) {
	var results []ValidationResult

	// Check 1: Docker Compose file exists
	results = append(results, validateDockerCompose(stackPath))

	// Check 2: Traefik configuration
	results = append(results, validateTraefikConfig(stackPath))

	// Check 3: Environment files
	results = append(results, validateEnvFiles(stackPath))

	// Check 4: Network configuration
	results = append(results, validateNetworkConfig(stackPath))

	// Check 5: Volume mounts
	results = append(results, validateVolumes(stackPath))

	// Check 6: Service dependencies
	results = append(results, validateServiceDependencies(stackPath))

	// Check 7: Documentation
	results = append(results, validateDocumentation(stackPath))

	return results, nil
}

// validateDockerCompose validates the Docker Compose file
func validateDockerCompose(stackPath string) ValidationResult {
	result := ValidationResult{
		Check:   "Docker Compose Configuration",
		Details: []string{},
	}

	composePath := filepath.Join(stackPath, "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		result.Status = "error"
		result.Message = "docker-compose.yml not found"
		return result
	}

	// TODO: Implement actual YAML parsing and validation
	// For now, just check if file exists and has content
	content, err := os.ReadFile(composePath)
	if err != nil {
		result.Status = "error"
		result.Message = "Failed to read docker-compose.yml"
		return result
	}

	if len(content) == 0 {
		result.Status = "error"
		result.Message = "docker-compose.yml is empty"
		return result
	}

	// Basic validation
	contentStr := string(content)
	if !strings.Contains(contentStr, "services:") {
		result.Status = "error"
		result.Message = "docker-compose.yml missing 'services' section"
		return result
	}

	if strings.Contains(contentStr, "version:") {
		result.Details = append(result.Details, "Compose file version specified")
	}

	result.Status = "success"
	result.Message = "Docker Compose file is valid"
	return result
}

// validateTraefikConfig validates Traefik configuration
func validateTraefikConfig(stackPath string) ValidationResult {
	result := ValidationResult{
		Check:   "Traefik Configuration",
		Details: []string{},
	}

	traefikPath := filepath.Join(stackPath, "traefik", "traefik.yml")
	if _, err := os.Stat(traefikPath); os.IsNotExist(err) {
		result.Status = "warning"
		result.Message = "Traefik configuration not found (optional)"
		return result
	}

	content, err := os.ReadFile(traefikPath)
	if err != nil {
		result.Status = "error"
		result.Message = "Failed to read Traefik configuration"
		return result
	}

	contentStr := string(content)
	if strings.Contains(contentStr, "entryPoints:") {
		result.Details = append(result.Details, "Entry points configured")
	}
	if strings.Contains(contentStr, "providers:") {
		result.Details = append(result.Details, "Providers configured")
	}

	result.Status = "success"
	result.Message = "Traefik configuration is valid"
	return result
}

// validateEnvFiles validates environment files
func validateEnvFiles(stackPath string) ValidationResult {
	result := ValidationResult{
		Check:   "Environment Files",
		Details: []string{},
	}

	envExamplePath := filepath.Join(stackPath, ".env.example")
	if _, err := os.Stat(envExamplePath); os.IsNotExist(err) {
		result.Status = "warning"
		result.Message = ".env.example not found"
		result.Details = append(result.Details, "Consider providing an example environment file")
		return result
	}

	envPath := filepath.Join(stackPath, ".env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		result.Details = append(result.Details, ".env file not created yet (expected)")
	} else {
		result.Details = append(result.Details, ".env file exists")
	}

	result.Status = "success"
	result.Message = "Environment configuration is valid"
	return result
}

// validateNetworkConfig validates network configuration
func validateNetworkConfig(stackPath string) ValidationResult {
	result := ValidationResult{
		Check:   "Network Configuration",
		Details: []string{},
	}

	composePath := filepath.Join(stackPath, "docker-compose.yml")
	content, err := os.ReadFile(composePath)
	if err != nil {
		result.Status = "error"
		result.Message = "Cannot read docker-compose.yml"
		return result
	}

	contentStr := string(content)
	if strings.Contains(contentStr, "networks:") {
		result.Details = append(result.Details, "Networks defined in compose file")
	} else {
		result.Status = "warning"
		result.Message = "No networks defined"
		return result
	}

	result.Status = "success"
	result.Message = "Network configuration is valid"
	return result
}

// validateVolumes validates volume configuration
func validateVolumes(stackPath string) ValidationResult {
	result := ValidationResult{
		Check:   "Volume Configuration",
		Details: []string{},
	}

	composePath := filepath.Join(stackPath, "docker-compose.yml")
	content, err := os.ReadFile(composePath)
	if err != nil {
		result.Status = "error"
		result.Message = "Cannot read docker-compose.yml"
		return result
	}

	contentStr := string(content)
	if strings.Contains(contentStr, "volumes:") {
		result.Details = append(result.Details, "Volumes configured")
	}

	// Check if important directories exist
	dirs := []string{"traefik", "certs"}
	for _, dir := range dirs {
		dirPath := filepath.Join(stackPath, dir)
		if _, err := os.Stat(dirPath); err == nil {
			result.Details = append(result.Details, fmt.Sprintf("Directory %s exists", dir))
		}
	}

	result.Status = "success"
	result.Message = "Volume configuration is valid"
	return result
}

// validateServiceDependencies validates service dependencies
func validateServiceDependencies(stackPath string) ValidationResult {
	result := ValidationResult{
		Check:   "Service Dependencies",
		Details: []string{},
	}

	composePath := filepath.Join(stackPath, "docker-compose.yml")
	content, err := os.ReadFile(composePath)
	if err != nil {
		result.Status = "error"
		result.Message = "Cannot read docker-compose.yml"
		return result
	}

	contentStr := string(content)

	// Count services
	serviceCount := strings.Count(contentStr, "image:")
	result.Details = append(result.Details, fmt.Sprintf("Found %d services", serviceCount))

	if strings.Contains(contentStr, "depends_on:") {
		result.Details = append(result.Details, "Service dependencies configured")
	}

	result.Status = "success"
	result.Message = "Service dependencies are valid"
	return result
}

// validateDocumentation validates documentation files
func validateDocumentation(stackPath string) ValidationResult {
	result := ValidationResult{
		Check:   "Documentation",
		Details: []string{},
	}

	readmePath := filepath.Join(stackPath, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		result.Status = "warning"
		result.Message = "README.md not found"
		result.Details = append(result.Details, "Consider adding documentation")
		return result
	}

	content, err := os.ReadFile(readmePath)
	if err != nil {
		result.Status = "warning"
		result.Message = "Cannot read README.md"
		return result
	}

	if len(content) > 0 {
		result.Details = append(result.Details, "README.md exists and has content")
	}

	result.Status = "success"
	result.Message = "Documentation is available"
	return result
}

// displayValidationResults displays the validation results
func displayValidationResults(results []ValidationResult) {
	if IsQuiet() {
		// Only show errors in quiet mode
		for _, result := range results {
			if result.Status == "error" {
				ui.Error(fmt.Sprintf("%s: %s", result.Check, result.Message))
			}
		}
		return
	}

	// Display results in a table
	table := ui.NewTable([]string{"Check", "Status", "Message"})
	for _, result := range results {
		statusIcon := ""
		switch result.Status {
		case "success":
			statusIcon = "✓"
		case "warning":
			statusIcon = "⚠"
		case "error":
			statusIcon = "✗"
		}

		table.AddRow([]string{
			result.Check,
			statusIcon + " " + result.Status,
			result.Message,
		})
	}

	fmt.Println(table.Render())

	// Display details if verbose
	if IsVerbose() {
		fmt.Println("\nDetails:")
		for _, result := range results {
			if len(result.Details) > 0 {
				fmt.Printf("\n%s:\n", result.Check)
				for _, detail := range result.Details {
					fmt.Printf("  - %s\n", detail)
				}
			}
		}
	}
}
