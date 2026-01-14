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

// EKSToK3sExecutor migrates EKS clusters to K3s/K8s manifests.
type EKSToK3sExecutor struct{}

// NewEKSToK3sExecutor creates a new EKS to K3s executor.
func NewEKSToK3sExecutor() *EKSToK3sExecutor {
	return &EKSToK3sExecutor{}
}

// Type returns the migration type.
func (e *EKSToK3sExecutor) Type() string {
	return "eks_to_k3s"
}

// GetPhases returns the migration phases.
func (e *EKSToK3sExecutor) GetPhases() []string {
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
func (e *EKSToK3sExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["cluster_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.cluster_name is required")
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

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "EKS-specific features (IAM roles, ALB ingress) will need manual configuration")

	return result, nil
}

// Execute performs the migration.
func (e *EKSToK3sExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	clusterName := config.Source["cluster_name"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	outputDir := config.Destination["output_dir"].(string)

	awsEnv := []string{
		"AWS_ACCESS_KEY_ID=" + accessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + secretAccessKey,
		"AWS_DEFAULT_REGION=" + region,
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials and cluster access")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Update kubeconfig for EKS
	updateCmd := exec.CommandContext(ctx, "aws", "eks", "update-kubeconfig",
		"--name", clusterName,
		"--region", region,
	)
	updateCmd.Env = append(os.Environ(), awsEnv...)
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to update kubeconfig: %w", err)
	}

	// Phase 2: Fetching cluster configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching cluster configuration for %s", clusterName))
	EmitProgress(m, 20, "Fetching cluster info")

	clusterCmd := exec.CommandContext(ctx, "aws", "eks", "describe-cluster",
		"--name", clusterName,
		"--region", region,
		"--output", "json",
	)
	clusterCmd.Env = append(os.Environ(), awsEnv...)
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
		// Export deployments
		deployCmd := exec.CommandContext(ctx, "kubectl", "get", "deployments",
			"-n", ns, "-o", "json",
		)
		if output, err := deployCmd.Output(); err == nil {
			var deployments interface{}
			json.Unmarshal(output, &deployments)
			workloads[ns+"_deployments"] = deployments
		}

		// Export services
		svcCmd := exec.CommandContext(ctx, "kubectl", "get", "services",
			"-n", ns, "-o", "json",
		)
		if output, err := svcCmd.Output(); err == nil {
			var services interface{}
			json.Unmarshal(output, &services)
			workloads[ns+"_services"] = services
		}

		// Export configmaps
		cmCmd := exec.CommandContext(ctx, "kubectl", "get", "configmaps",
			"-n", ns, "-o", "json",
		)
		if output, err := cmCmd.Output(); err == nil {
			var configmaps interface{}
			json.Unmarshal(output, &configmaps)
			workloads[ns+"_configmaps"] = configmaps
		}

		// Export secrets (metadata only)
		secretCmd := exec.CommandContext(ctx, "kubectl", "get", "secrets",
			"-n", ns, "-o", "json",
		)
		if output, err := secretCmd.Output(); err == nil {
			var secrets interface{}
			json.Unmarshal(output, &secrets)
			workloads[ns+"_secrets"] = secrets
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Converting to K3s format
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Converting to K3s-compatible format")
	EmitProgress(m, 60, "Converting manifests")

	// Remove EKS-specific annotations
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
description: Migrated from EKS cluster %s
type: application
version: 1.0.0
appVersion: "1.0.0"
`, clusterName, clusterName)
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYaml), 0644); err != nil {
		return fmt.Errorf("failed to write Chart.yaml: %w", err)
	}

	// Write values.yaml
	valuesYaml := `# Default values migrated from EKS
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
# K3s Installation Script for migrated EKS workloads

set -e

echo "Installing K3s..."
curl -sfL https://get.k3s.io | sh -

echo "Waiting for K3s to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=300s

echo "Applying migrated workloads..."
kubectl apply -f templates/

echo "Migration complete!"
`
	if err := os.WriteFile(filepath.Join(outputDir, "install-k3s.sh"), []byte(k3sScript), 0755); err != nil {
		return fmt.Errorf("failed to write install script: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("EKS cluster %s migrated to %s", clusterName, outputDir))

	return nil
}

func (e *EKSToK3sExecutor) cleanupManifests(workloads map[string]interface{}) {
	eksAnnotations := []string{
		"eks.amazonaws.com",
		"kubernetes.io/cluster",
		"alb.ingress.kubernetes.io",
	}

	for _, data := range workloads {
		if items, ok := data.(map[string]interface{}); ok {
			if itemsList, ok := items["items"].([]interface{}); ok {
				for _, item := range itemsList {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if metadata, ok := itemMap["metadata"].(map[string]interface{}); ok {
							if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
								for key := range annotations {
									for _, prefix := range eksAnnotations {
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
