package gcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	gcpparser "github.com/homeport/homeport/internal/infrastructure/parser/gcp"
)

// TestHCLParser_ParsesValidTerraformFile tests parsing of GCP Terraform HCL files.
func TestHCLParser_ParsesValidTerraformFile(t *testing.T) {
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "main.tf")

	tfContent := `
provider "google" {
  project = "my-project"
  region  = "us-central1"
}

resource "google_compute_instance" "web" {
  name         = "web-server"
  machine_type = "n2-standard-2"
  zone         = "us-central1-a"

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-11"
    }
  }

  network_interface {
    network = "default"
  }

  labels = {
    environment = "production"
    team        = "platform"
  }
}

resource "google_storage_bucket" "assets" {
  name     = "my-app-assets"
  location = "US"

  versioning {
    enabled = true
  }

  labels = {
    app = "webapp"
  }
}

resource "google_sql_database_instance" "main" {
  name             = "main-db"
  database_version = "POSTGRES_14"
  region           = "us-central1"

  settings {
    tier = "db-f1-micro"
  }
}
`
	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	p := gcpparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, tfPath, opts)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if infra == nil {
		t.Fatal("Expected infrastructure to be non-nil")
	}

	if len(infra.Resources) != 3 {
		t.Errorf("Expected 3 resources, got %d", len(infra.Resources))
	}

	// Verify resource types
	typeCount := make(map[resource.Type]int)
	for _, res := range infra.Resources {
		typeCount[res.Type]++
	}

	if typeCount[resource.TypeGCEInstance] != 1 {
		t.Errorf("Expected 1 GCE instance, got %d", typeCount[resource.TypeGCEInstance])
	}
	if typeCount[resource.TypeGCSBucket] != 1 {
		t.Errorf("Expected 1 GCS bucket, got %d", typeCount[resource.TypeGCSBucket])
	}
	if typeCount[resource.TypeCloudSQL] != 1 {
		t.Errorf("Expected 1 CloudSQL instance, got %d", typeCount[resource.TypeCloudSQL])
	}

	t.Logf("Parsed %d resources from HCL", len(infra.Resources))
}

// TestHCLParser_ResourceExtraction tests extraction of resource attributes.
func TestHCLParser_ResourceExtraction(t *testing.T) {
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "instance.tf")

	tfContent := `
resource "google_compute_instance" "test" {
  name         = "test-instance"
  machine_type = "e2-medium"
  zone         = "us-west1-a"
}
`
	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	p := gcpparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, tfPath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if len(infra.Resources) != 1 {
		t.Fatalf("Expected 1 resource, got %d", len(infra.Resources))
	}

	for _, res := range infra.Resources {
		// Check resource ID format
		expectedID := "google_compute_instance.test"
		if res.ID != expectedID {
			t.Errorf("Expected ID %s, got %s", expectedID, res.ID)
		}

		// Check name extraction
		if res.Name != "test-instance" && res.Name != "test" {
			t.Errorf("Expected name 'test-instance' or 'test', got '%s'", res.Name)
		}

		// Check type
		if res.Type != resource.TypeGCEInstance {
			t.Errorf("Expected type %s, got %s", resource.TypeGCEInstance, res.Type)
		}

		// Check region extraction from zone
		if res.Region != "us-west1" {
			t.Errorf("Expected region 'us-west1' (extracted from zone), got '%s'", res.Region)
		}

		// Check terraform_type in config
		if res.Config["terraform_type"] != "google_compute_instance" {
			t.Errorf("Expected terraform_type in config, got %v", res.Config["terraform_type"])
		}
	}
}

// TestHCLParser_SkipsNonGCPResources tests that non-GCP resources are skipped.
func TestHCLParser_SkipsNonGCPResources(t *testing.T) {
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "mixed.tf")

	tfContent := `
provider "google" {
  project = "my-project"
}

provider "aws" {
  region = "us-east-1"
}

resource "google_compute_instance" "gcp_vm" {
  name         = "gcp-vm"
  machine_type = "n1-standard-1"
  zone         = "us-central1-a"
}

resource "aws_instance" "aws_vm" {
  ami           = "ami-12345678"
  instance_type = "t2.micro"
}

resource "google_storage_bucket" "bucket" {
  name     = "my-bucket"
  location = "US"
}
`
	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	p := gcpparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, tfPath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Should only have GCP resources
	if len(infra.Resources) != 2 {
		t.Errorf("Expected 2 GCP resources (AWS should be skipped), got %d", len(infra.Resources))
	}

	for _, res := range infra.Resources {
		if res.Type.Provider() != resource.ProviderGCP {
			t.Errorf("Expected only GCP resources, got %s", res.Type)
		}
	}
}

// TestHCLParser_DirectoryParsing tests parsing a directory of .tf files.
func TestHCLParser_DirectoryParsing(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple .tf files
	files := map[string]string{
		"compute.tf": `
resource "google_compute_instance" "vm1" {
  name         = "vm1"
  machine_type = "n1-standard-1"
  zone         = "us-central1-a"
}
`,
		"storage.tf": `
resource "google_storage_bucket" "bucket1" {
  name     = "bucket1"
  location = "US"
}
`,
		"database.tf": `
resource "google_sql_database_instance" "db1" {
  name             = "db1"
  database_version = "MYSQL_8_0"
  region           = "us-central1"
}
`,
	}

	for filename, content := range files {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", filename, err)
		}
	}

	p := gcpparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	if len(infra.Resources) != 3 {
		t.Errorf("Expected 3 resources from directory, got %d", len(infra.Resources))
	}

	t.Logf("Parsed %d resources from directory", len(infra.Resources))
}

// TestHCLParser_Validate tests validation of HCL files.
func TestHCLParser_Validate(t *testing.T) {
	p := gcpparser.NewHCLParser()

	// Test with non-existent path
	err := p.Validate("/non/existent/path.tf")
	if err == nil {
		t.Error("Expected error for non-existent path")
	}

	// Test with valid GCP HCL file
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "valid.tf")

	tfContent := `
resource "google_compute_instance" "test" {
  name = "test"
}
`
	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if err := p.Validate(tfPath); err != nil {
		t.Errorf("Expected valid file to pass validation: %v", err)
	}

	// Test with non-GCP HCL file
	awsPath := filepath.Join(tmpDir, "aws.tf")
	awsContent := `
resource "aws_instance" "test" {
  ami = "ami-12345"
}
`
	if err := os.WriteFile(awsPath, []byte(awsContent), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if err := p.Validate(awsPath); err == nil {
		t.Error("Expected validation to fail for non-GCP file")
	}
}

// TestHCLParser_AutoDetect tests auto-detection of GCP HCL files.
func TestHCLParser_AutoDetect(t *testing.T) {
	p := gcpparser.NewHCLParser()

	// Create GCP HCL file
	tmpDir := t.TempDir()
	gcpPath := filepath.Join(tmpDir, "gcp.tf")

	gcpContent := `
provider "google" {
  project = "my-project"
}

resource "google_compute_instance" "test" {
  name = "test"
}
`
	if err := os.WriteFile(gcpPath, []byte(gcpContent), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	canHandle, confidence := p.AutoDetect(gcpPath)
	if !canHandle {
		t.Error("Expected AutoDetect to return true for GCP HCL file")
	}
	if confidence < 0.8 {
		t.Errorf("Expected high confidence for GCP HCL file, got %f", confidence)
	}

	// Create non-GCP HCL file
	awsPath := filepath.Join(tmpDir, "aws.tf")
	awsContent := `
provider "aws" {
  region = "us-east-1"
}

resource "aws_instance" "test" {
  ami = "ami-12345"
}
`
	if err := os.WriteFile(awsPath, []byte(awsContent), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	canHandle, _ = p.AutoDetect(awsPath)
	if canHandle {
		t.Error("Expected AutoDetect to return false for AWS-only HCL file")
	}
}

// TestHCLParser_Provider verifies the provider.
func TestHCLParser_Provider(t *testing.T) {
	p := gcpparser.NewHCLParser()

	if p.Provider() != resource.ProviderGCP {
		t.Errorf("Expected provider %s, got %s", resource.ProviderGCP, p.Provider())
	}
}

// TestHCLParser_SupportedFormats verifies supported formats.
func TestHCLParser_SupportedFormats(t *testing.T) {
	p := gcpparser.NewHCLParser()

	formats := p.SupportedFormats()
	if len(formats) != 1 {
		t.Errorf("Expected 1 supported format, got %d", len(formats))
	}

	if formats[0] != parser.FormatTerraform {
		t.Errorf("Expected format %s, got %s", parser.FormatTerraform, formats[0])
	}
}

// TestHCLParser_MultipleResourceTypes tests parsing various GCP resource types.
func TestHCLParser_MultipleResourceTypes(t *testing.T) {
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "resources.tf")

	tfContent := `
resource "google_compute_instance" "vm" {
  name = "vm"
}

resource "google_cloud_run_service" "api" {
  name     = "api-service"
  location = "us-central1"
}

resource "google_container_cluster" "gke" {
  name     = "gke-cluster"
  location = "us-central1"
}

resource "google_pubsub_topic" "events" {
  name = "events-topic"
}

resource "google_redis_instance" "cache" {
  name           = "cache"
  memory_size_gb = 1
  region         = "us-central1"
}

resource "google_dns_managed_zone" "zone" {
  name     = "my-zone"
  dns_name = "example.com."
}
`
	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	p := gcpparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, tfPath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	expectedTypes := map[resource.Type]bool{
		resource.TypeGCEInstance:   false,
		resource.TypeCloudRun:      false,
		resource.TypeGKE:           false,
		resource.TypePubSubTopic:   false,
		resource.TypeMemorystore:   false,
		resource.TypeCloudDNS:      false,
	}

	for _, res := range infra.Resources {
		if _, ok := expectedTypes[res.Type]; ok {
			expectedTypes[res.Type] = true
		}
	}

	for resType, found := range expectedTypes {
		if !found {
			t.Errorf("Expected resource type %s not found", resType)
		}
	}
}

// TestHCLParser_FilterByType tests filtering resources by type.
func TestHCLParser_FilterByType(t *testing.T) {
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "all.tf")

	tfContent := `
resource "google_compute_instance" "vm1" {
  name = "vm1"
}

resource "google_compute_instance" "vm2" {
  name = "vm2"
}

resource "google_storage_bucket" "bucket" {
  name     = "bucket"
  location = "US"
}
`
	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	p := gcpparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions().WithFilterTypes(resource.TypeGCEInstance)

	infra, err := p.Parse(ctx, tfPath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Should only have GCE instances
	if len(infra.Resources) != 2 {
		t.Errorf("Expected 2 GCE instances (filtered), got %d", len(infra.Resources))
	}

	for _, res := range infra.Resources {
		if res.Type != resource.TypeGCEInstance {
			t.Errorf("Expected only GCE instances, got %s", res.Type)
		}
	}
}

// TestHCLParser_IgnoresDataBlocks tests that data blocks are not parsed as resources.
func TestHCLParser_IgnoresDataBlocks(t *testing.T) {
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "data.tf")

	tfContent := `
data "google_compute_image" "ubuntu" {
  family  = "ubuntu-2204-lts"
  project = "ubuntu-os-cloud"
}

resource "google_compute_instance" "vm" {
  name         = "vm"
  machine_type = "n1-standard-1"
  zone         = "us-central1-a"

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
    }
  }
}
`
	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	p := gcpparser.NewHCLParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, tfPath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Should only have the resource, not the data block
	if len(infra.Resources) != 1 {
		t.Errorf("Expected 1 resource (data block should be ignored), got %d", len(infra.Resources))
	}
}

// TestHCLParser_RegionExtraction tests region extraction from different attributes.
func TestHCLParser_RegionExtraction(t *testing.T) {
	tests := []struct {
		name           string
		tfContent      string
		expectedRegion string
	}{
		{
			name: "From region attribute",
			tfContent: `
resource "google_sql_database_instance" "db" {
  name   = "db"
  region = "europe-west1"
}
`,
			expectedRegion: "europe-west1",
		},
		{
			name: "From zone attribute",
			tfContent: `
resource "google_compute_instance" "vm" {
  name = "vm"
  zone = "asia-east1-b"
}
`,
			expectedRegion: "asia-east1",
		},
		{
			name: "From location attribute",
			tfContent: `
resource "google_storage_bucket" "bucket" {
  name     = "bucket"
  location = "US-CENTRAL1"
}
`,
			expectedRegion: "US-CENTRAL1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tfPath := filepath.Join(tmpDir, "test.tf")

			if err := os.WriteFile(tfPath, []byte(tt.tfContent), 0644); err != nil {
				t.Fatalf("Failed to create file: %v", err)
			}

			p := gcpparser.NewHCLParser()
			ctx := context.Background()
			opts := parser.NewParseOptions()

			infra, err := p.Parse(ctx, tfPath, opts)
			if err != nil {
				t.Fatalf("Failed to parse: %v", err)
			}

			for _, res := range infra.Resources {
				if res.Region != tt.expectedRegion {
					t.Errorf("Expected region %s, got %s", tt.expectedRegion, res.Region)
				}
			}
		})
	}
}
