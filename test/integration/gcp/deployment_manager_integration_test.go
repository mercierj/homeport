package gcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
	gcpparser "github.com/agnostech/agnostech/internal/infrastructure/parser/gcp"
)

// TestDeploymentManagerParser_ParsesValidYAML tests parsing of GCP Deployment Manager YAML templates.
func TestDeploymentManagerParser_ParsesValidYAML(t *testing.T) {
	// Create a temporary Deployment Manager template
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "deployment.yaml")

	templateContent := `
resources:
  - name: my-vm-instance
    type: compute.v1.instance
    properties:
      zone: us-central1-a
      machineType: zones/us-central1-a/machineTypes/n1-standard-1
      disks:
        - boot: true
          autoDelete: true
          initializeParams:
            sourceImage: projects/debian-cloud/global/images/family/debian-11
      networkInterfaces:
        - network: global/networks/default
      labels:
        environment: test
        team: dev

  - name: my-storage-bucket
    type: storage.v1.bucket
    properties:
      location: US
      storageClass: STANDARD
      labels:
        app: myapp

outputs:
  - name: instanceIP
    value: $(ref.my-vm-instance.networkInterfaces[0].accessConfigs[0].natIP)
`
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create test template: %v", err)
	}

	// Create parser and parse
	p := gcpparser.NewDeploymentManagerParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("Failed to parse Deployment Manager template: %v", err)
	}

	// Verify infrastructure
	if infra == nil {
		t.Fatal("Expected infrastructure to be non-nil")
	}

	if len(infra.Resources) != 2 {
		t.Errorf("Expected 2 resources, got %d", len(infra.Resources))
	}

	// Verify GCE instance was parsed
	var vmFound, bucketFound bool
	for _, res := range infra.Resources {
		if res.Name == "my-vm-instance" {
			vmFound = true
			if res.Type != resource.TypeGCEInstance {
				t.Errorf("Expected type %s, got %s", resource.TypeGCEInstance, res.Type)
			}
			// Verify region extraction from zone
			if res.Region != "us-central1" {
				t.Errorf("Expected region us-central1, got %s", res.Region)
			}
			// Verify labels were extracted
			if res.Tags["environment"] != "test" {
				t.Errorf("Expected environment tag 'test', got '%s'", res.Tags["environment"])
			}
		}
		if res.Name == "my-storage-bucket" {
			bucketFound = true
			if res.Type != resource.TypeGCSBucket {
				t.Errorf("Expected type %s, got %s", resource.TypeGCSBucket, res.Type)
			}
		}
	}

	if !vmFound {
		t.Error("VM instance resource not found")
	}
	if !bucketFound {
		t.Error("Storage bucket resource not found")
	}

	t.Logf("Parsed %d resources from Deployment Manager template", len(infra.Resources))
}

// TestDeploymentManagerParser_ResourceTypeMapping tests the mapping from DM types to resource types.
func TestDeploymentManagerParser_ResourceTypeMapping(t *testing.T) {
	tests := []struct {
		name         string
		dmType       string
		expectedType resource.Type
	}{
		{
			name:         "Compute instance",
			dmType:       "compute.v1.instance",
			expectedType: resource.TypeGCEInstance,
		},
		{
			name:         "Storage bucket",
			dmType:       "storage.v1.bucket",
			expectedType: resource.TypeGCSBucket,
		},
		{
			name:         "CloudSQL instance",
			dmType:       "sqladmin.v1beta4.instance",
			expectedType: resource.TypeCloudSQL,
		},
		{
			name:         "Pub/Sub topic",
			dmType:       "pubsub.v1.topic",
			expectedType: resource.TypePubSubTopic,
		},
		{
			name:         "GKE cluster",
			dmType:       "container.v1.cluster",
			expectedType: resource.TypeGKE,
		},
		{
			name:         "Persistent disk",
			dmType:       "compute.v1.disk",
			expectedType: resource.TypePersistentDisk,
		},
		{
			name:         "VPC network",
			dmType:       "compute.v1.network",
			expectedType: resource.TypeGCPVPCNetwork,
		},
		{
			name:         "DNS zone",
			dmType:       "dns.v1.managedzone",
			expectedType: resource.TypeCloudDNS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			templatePath := filepath.Join(tmpDir, "test.yaml")

			templateContent := `
resources:
  - name: test-resource
    type: ` + tt.dmType + `
    properties:
      zone: us-central1-a
`
			if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
				t.Fatalf("Failed to create test template: %v", err)
			}

			p := gcpparser.NewDeploymentManagerParser()
			ctx := context.Background()
			opts := parser.NewParseOptions()

			infra, err := p.Parse(ctx, templatePath, opts)
			if err != nil {
				t.Fatalf("Failed to parse: %v", err)
			}

			if len(infra.Resources) != 1 {
				t.Fatalf("Expected 1 resource, got %d", len(infra.Resources))
			}

			for _, res := range infra.Resources {
				if res.Type != tt.expectedType {
					t.Errorf("Expected type %s, got %s", tt.expectedType, res.Type)
				}
			}
		})
	}
}

// TestDeploymentManagerParser_DependencyResolution tests extraction of resource references.
func TestDeploymentManagerParser_DependencyResolution(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "deps.yaml")

	templateContent := `
resources:
  - name: my-network
    type: compute.v1.network
    properties:
      autoCreateSubnetworks: false

  - name: my-instance
    type: compute.v1.instance
    properties:
      zone: us-central1-a
      machineType: zones/us-central1-a/machineTypes/n1-standard-1
      networkInterfaces:
        - network: $(ref.my-network.selfLink)
          subnetwork: $(ref.my-subnet.selfLink)
      disks:
        - source: $(ref.my-disk.selfLink)
          boot: false
`
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create test template: %v", err)
	}

	p := gcpparser.NewDeploymentManagerParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, templatePath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Find the instance and check dependencies
	for _, res := range infra.Resources {
		if res.Name == "my-instance" {
			if len(res.Dependencies) == 0 {
				t.Error("Expected instance to have dependencies extracted from $(ref.X) syntax")
			}

			expectedDeps := map[string]bool{
				"my-network": true,
				"my-subnet":  true,
				"my-disk":    true,
			}

			for _, dep := range res.Dependencies {
				if !expectedDeps[dep] {
					t.Errorf("Unexpected dependency: %s", dep)
				}
				delete(expectedDeps, dep)
			}

			for missing := range expectedDeps {
				t.Errorf("Missing expected dependency: %s", missing)
			}

			t.Logf("Instance dependencies: %v", res.Dependencies)
		}
	}
}

// TestDeploymentManagerParser_Validate tests the validator function.
func TestDeploymentManagerParser_Validate(t *testing.T) {
	p := gcpparser.NewDeploymentManagerParser()

	// Test with non-existent path
	err := p.Validate("/non/existent/path")
	if err == nil {
		t.Error("Expected error for non-existent path")
	}

	// Test with valid DM file
	tmpDir := t.TempDir()
	validPath := filepath.Join(tmpDir, "valid.yaml")
	validContent := `
resources:
  - name: test
    type: compute.v1.instance
    properties:
      zone: us-central1-a
`
	if err := os.WriteFile(validPath, []byte(validContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if err := p.Validate(validPath); err != nil {
		t.Errorf("Expected valid file to pass validation: %v", err)
	}
}

// TestDeploymentManagerParser_AutoDetect tests auto-detection of DM templates.
func TestDeploymentManagerParser_AutoDetect(t *testing.T) {
	p := gcpparser.NewDeploymentManagerParser()

	// Create valid DM file
	tmpDir := t.TempDir()
	dmPath := filepath.Join(tmpDir, "deployment.yaml")
	dmContent := `
resources:
  - name: test
    type: compute.v1.instance
    properties:
      zone: us-central1-a
`
	if err := os.WriteFile(dmPath, []byte(dmContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	canHandle, confidence := p.AutoDetect(dmPath)
	if !canHandle {
		t.Error("Expected AutoDetect to return true for DM file")
	}
	if confidence < 0.8 {
		t.Errorf("Expected high confidence for DM file, got %f", confidence)
	}

	// Test with non-DM file
	nonDMPath := filepath.Join(tmpDir, "random.yaml")
	nonDMContent := `
name: some-config
key: value
`
	if err := os.WriteFile(nonDMPath, []byte(nonDMContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	canHandle, _ = p.AutoDetect(nonDMPath)
	if canHandle {
		t.Error("Expected AutoDetect to return false for non-DM file")
	}
}

// TestDeploymentManagerParser_SupportedFormats verifies the supported format.
func TestDeploymentManagerParser_SupportedFormats(t *testing.T) {
	p := gcpparser.NewDeploymentManagerParser()

	formats := p.SupportedFormats()
	if len(formats) != 1 {
		t.Errorf("Expected 1 supported format, got %d", len(formats))
	}

	if formats[0] != parser.FormatDeploymentManager {
		t.Errorf("Expected format %s, got %s", parser.FormatDeploymentManager, formats[0])
	}
}

// TestDeploymentManagerParser_Provider verifies the provider.
func TestDeploymentManagerParser_Provider(t *testing.T) {
	p := gcpparser.NewDeploymentManagerParser()

	if p.Provider() != resource.ProviderGCP {
		t.Errorf("Expected provider %s, got %s", resource.ProviderGCP, p.Provider())
	}
}

// TestDeploymentManagerParser_DirectoryParsing tests parsing a directory of DM templates.
func TestDeploymentManagerParser_DirectoryParsing(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple DM files
	file1Content := `
resources:
  - name: vm-1
    type: compute.v1.instance
    properties:
      zone: us-central1-a
`
	file2Content := `
resources:
  - name: bucket-1
    type: storage.v1.bucket
    properties:
      location: US
`
	if err := os.WriteFile(filepath.Join(tmpDir, "compute.yaml"), []byte(file1Content), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "storage.yaml"), []byte(file2Content), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	p := gcpparser.NewDeploymentManagerParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	if len(infra.Resources) != 2 {
		t.Errorf("Expected 2 resources from directory, got %d", len(infra.Resources))
	}

	t.Logf("Parsed %d resources from directory", len(infra.Resources))
}
