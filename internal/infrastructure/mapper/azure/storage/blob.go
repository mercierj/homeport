// Package storage provides mappers for Azure storage services.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// BlobMapper converts Azure Blob Storage containers to MinIO.
type BlobMapper struct {
	*mapper.BaseMapper
}

// NewBlobMapper creates a new Azure Blob Storage to MinIO mapper.
func NewBlobMapper() *BlobMapper {
	return &BlobMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeBlobStorage, nil),
	}
}

// Map converts an Azure Blob Storage container to a MinIO service.
func (m *BlobMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	containerName := res.GetConfigString("name")
	if containerName == "" {
		containerName = res.Name
	}

	// Create result using new API
	result := mapper.NewMappingResult("minio")
	svc := result.DockerService

	// Configure MinIO service
	svc.Image = "minio/minio:latest"
	svc.Environment = map[string]string{
		"MINIO_ROOT_USER":     "minioadmin",
		"MINIO_ROOT_PASSWORD": "minioadmin",
	}
	svc.Ports = []string{
		"9000:9000", // API
		"9001:9001", // Console
	}
	svc.Volumes = []string{
		"./data/minio:/data",
	}
	svc.Command = []string{"server", "/data", "--console-address", ":9001"}
	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:9000/minio/health/live"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}
	svc.Labels = map[string]string{
		"cloudexit.source":    "azurerm_storage_container",
		"cloudexit.container": containerName,
		"traefik.enable":      "true",
		"traefik.http.routers.minio.rule":                      "Host(`minio.localhost`)",
		"traefik.http.services.minio.loadbalancer.server.port": "9001",
	}

	// Generate MinIO client (mc) setup script
	mcScript := m.generateMCScript(res, containerName)
	result.AddScript("setup_minio.sh", []byte(mcScript))

	// Handle container access level (public/private)
	accessType := res.GetConfigString("container_access_type")
	if accessType == "" {
		accessType = "private"
	}

	switch accessType {
	case "blob", "container":
		result.AddManualStep(fmt.Sprintf("Set public read access: mc anonymous set download local/%s", containerName))
		result.AddWarning(fmt.Sprintf("Container has public access type '%s'. Ensure this is intentional in your self-hosted environment.", accessType))
	case "private":
		// Private is default, no action needed
	}

	// Handle metadata
	if metadata := res.Config["metadata"]; metadata != nil {
		if metaMap, ok := metadata.(map[string]interface{}); ok && len(metaMap) > 0 {
			result.AddWarning("Container metadata is configured. MinIO doesn't have direct container metadata support - consider using bucket tags.")
			metadataScript := m.generateMetadataScript(containerName, metaMap)
			result.AddScript("configure_metadata.sh", []byte(metadataScript))
		}
	}

	// Handle storage account name (parent resource)
	if storageAccountName := res.GetConfigString("storage_account_name"); storageAccountName != "" {
		result.AddWarning(fmt.Sprintf("Container belongs to storage account '%s'. MinIO manages buckets independently.", storageAccountName))
	}

	result.AddManualStep("Access MinIO Console at: http://localhost:9001")
	result.AddManualStep("Use credentials: minioadmin / minioadmin")
	result.AddManualStep(fmt.Sprintf("To use with Azure SDK, configure endpoint: http://localhost:9000/%s", containerName))

	return result, nil
}

// generateMCScript creates a MinIO client setup script.
func (m *BlobMapper) generateMCScript(res *resource.AWSResource, containerName string) string {
	return fmt.Sprintf(`#!/bin/bash
# MinIO Client (mc) Setup Script for Azure Blob Storage Container
# This script configures the MinIO client and creates the container (bucket)

set -e

echo "Setting up MinIO client..."

# Configure MinIO alias
mc alias set local http://localhost:9000 minioadmin minioadmin

# Create bucket (container equivalent)
echo "Creating bucket: %s"
mc mb --ignore-existing local/%s

# Set region (for compatibility with Azure Storage)
mc admin config set local region name=local

echo "MinIO bucket '%s' is ready!"
echo "Access MinIO Console at: http://localhost:9001"
echo "Credentials: minioadmin / minioadmin"
echo ""
echo "To use with Azure SDK, configure endpoint: http://localhost:9000"
echo "Container name: %s"
`, containerName, containerName, containerName, containerName)
}

// generateMetadataScript creates a script to document container metadata.
func (m *BlobMapper) generateMetadataScript(containerName string, metadata map[string]interface{}) string {
	script := fmt.Sprintf(`#!/bin/bash
# Container Metadata for: %s
# Note: MinIO doesn't support container-level metadata directly.
# Consider using bucket tags or storing this metadata elsewhere.

# Metadata:
`, containerName)

	for key, value := range metadata {
		script += fmt.Sprintf("# %s: %v\n", key, value)
	}

	script += `
# You can apply bucket tags using mc:
# mc tag set local/` + containerName + ` "key=value"
`

	return script
}
