package gcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	gcpparser "github.com/homeport/homeport/internal/infrastructure/parser/gcp"
)

// TestTFStateParser_ParsesValidState tests parsing of GCP Terraform state files.
func TestTFStateParser_ParsesValidState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "terraform.tfstate")

	// Create a mock Terraform state with GCP resources
	state := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.5.0",
		"resources": []map[string]interface{}{
			{
				"mode":     "managed",
				"type":     "google_compute_instance",
				"name":     "web-server",
				"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
				"instances": []map[string]interface{}{
					{
						"schema_version": 0,
						"attributes": map[string]interface{}{
							"id":           "projects/my-project/zones/us-central1-a/instances/web-server",
							"name":         "web-server",
							"machine_type": "n2-standard-4",
							"zone":         "us-central1-a",
							"self_link":    "https://www.googleapis.com/compute/v1/projects/my-project/zones/us-central1-a/instances/web-server",
							"labels": map[string]interface{}{
								"environment": "production",
								"team":        "platform",
							},
						},
						"dependencies": []string{
							"google_compute_network.main",
						},
					},
				},
			},
			{
				"mode":     "managed",
				"type":     "google_storage_bucket",
				"name":     "assets",
				"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
				"instances": []map[string]interface{}{
					{
						"schema_version": 0,
						"attributes": map[string]interface{}{
							"id":            "my-assets-bucket",
							"name":          "my-assets-bucket",
							"location":      "US",
							"storage_class": "STANDARD",
							"labels": map[string]interface{}{
								"app": "webapp",
							},
						},
					},
				},
			},
			{
				"mode":     "managed",
				"type":     "google_sql_database_instance",
				"name":     "main-db",
				"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
				"instances": []map[string]interface{}{
					{
						"schema_version": 0,
						"attributes": map[string]interface{}{
							"id":               "my-project:us-central1:main-db",
							"name":             "main-db",
							"database_version": "POSTGRES_14",
							"region":           "us-central1",
							"settings": map[string]interface{}{
								"tier":          "db-custom-2-7680",
								"disk_size":     100,
								"disk_type":     "PD_SSD",
							},
						},
					},
				},
			},
		},
	}

	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	if err := os.WriteFile(statePath, stateData, 0644); err != nil {
		t.Fatalf("Failed to write state file: %v", err)
	}

	p := gcpparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("Failed to parse TFState: %v", err)
	}

	if infra == nil {
		t.Fatal("Expected infrastructure to be non-nil")
	}

	if len(infra.Resources) != 3 {
		t.Errorf("Expected 3 resources, got %d", len(infra.Resources))
	}

	// Verify metadata
	if infra.Metadata["terraform_version"] != "1.5.0" {
		t.Errorf("Expected terraform_version 1.5.0, got %s", infra.Metadata["terraform_version"])
	}

	t.Logf("Parsed %d resources from TFState", len(infra.Resources))
}

// TestTFStateParser_ResourceTypeMapping tests mapping of google_* types.
func TestTFStateParser_ResourceTypeMapping(t *testing.T) {
	tests := []struct {
		name         string
		tfType       string
		expectedType resource.Type
	}{
		{
			name:         "Compute instance",
			tfType:       "google_compute_instance",
			expectedType: resource.TypeGCEInstance,
		},
		{
			name:         "Cloud Run service",
			tfType:       "google_cloud_run_service",
			expectedType: resource.TypeCloudRun,
		},
		{
			name:         "Cloud Function",
			tfType:       "google_cloudfunctions_function",
			expectedType: resource.TypeCloudFunction,
		},
		{
			name:         "GKE cluster",
			tfType:       "google_container_cluster",
			expectedType: resource.TypeGKE,
		},
		{
			name:         "Storage bucket",
			tfType:       "google_storage_bucket",
			expectedType: resource.TypeGCSBucket,
		},
		{
			name:         "Compute disk",
			tfType:       "google_compute_disk",
			expectedType: resource.TypePersistentDisk,
		},
		{
			name:         "CloudSQL instance",
			tfType:       "google_sql_database_instance",
			expectedType: resource.TypeCloudSQL,
		},
		{
			name:         "Firestore database",
			tfType:       "google_firestore_database",
			expectedType: resource.TypeFirestore,
		},
		{
			name:         "Bigtable instance",
			tfType:       "google_bigtable_instance",
			expectedType: resource.TypeBigtable,
		},
		{
			name:         "Redis instance",
			tfType:       "google_redis_instance",
			expectedType: resource.TypeMemorystore,
		},
		{
			name:         "Pub/Sub topic",
			tfType:       "google_pubsub_topic",
			expectedType: resource.TypePubSubTopic,
		},
		{
			name:         "Pub/Sub subscription",
			tfType:       "google_pubsub_subscription",
			expectedType: resource.TypePubSubSubscription,
		},
		{
			name:         "Cloud Tasks queue",
			tfType:       "google_cloud_tasks_queue",
			expectedType: resource.TypeCloudTasks,
		},
		{
			name:         "DNS managed zone",
			tfType:       "google_dns_managed_zone",
			expectedType: resource.TypeCloudDNS,
		},
		{
			name:         "Secret Manager secret",
			tfType:       "google_secret_manager_secret",
			expectedType: resource.TypeSecretManager,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "terraform.tfstate")

			state := map[string]interface{}{
				"version":           4,
				"terraform_version": "1.5.0",
				"resources": []map[string]interface{}{
					{
						"mode":     "managed",
						"type":     tt.tfType,
						"name":     "test-resource",
						"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
						"instances": []map[string]interface{}{
							{
								"schema_version": 0,
								"attributes": map[string]interface{}{
									"id":   "test-id",
									"name": "test-resource",
								},
							},
						},
					},
				},
			}

			stateData, _ := json.Marshal(state)
			if err := os.WriteFile(statePath, stateData, 0644); err != nil {
				t.Fatalf("Failed to write state: %v", err)
			}

			p := gcpparser.NewTFStateParser()
			ctx := context.Background()
			opts := parser.NewParseOptions()

			infra, err := p.Parse(ctx, statePath, opts)
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

// TestTFStateParser_ExtractsDependencies tests dependency extraction from state.
func TestTFStateParser_ExtractsDependencies(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "terraform.tfstate")

	state := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.5.0",
		"resources": []map[string]interface{}{
			{
				"mode":     "managed",
				"type":     "google_compute_network",
				"name":     "main",
				"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
				"instances": []map[string]interface{}{
					{
						"schema_version": 0,
						"attributes": map[string]interface{}{
							"id":   "main-network",
							"name": "main-network",
						},
					},
				},
			},
			{
				"mode":     "managed",
				"type":     "google_compute_instance",
				"name":     "server",
				"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
				"instances": []map[string]interface{}{
					{
						"schema_version": 0,
						"attributes": map[string]interface{}{
							"id":   "server-instance",
							"name": "server",
							"zone": "us-central1-a",
						},
						"dependencies": []string{
							"google_compute_network.main",
							"google_compute_subnetwork.subnet",
						},
					},
				},
			},
		},
	}

	stateData, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, stateData, 0644); err != nil {
		t.Fatalf("Failed to write state: %v", err)
	}

	p := gcpparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Find the instance and verify dependencies
	for _, res := range infra.Resources {
		if res.Name == "server" {
			if len(res.Dependencies) < 1 {
				t.Error("Expected instance to have dependencies")
			}
			t.Logf("Instance dependencies: %v", res.Dependencies)
		}
	}
}

// TestTFStateParser_ExtractsLabels tests label extraction.
func TestTFStateParser_ExtractsLabels(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "terraform.tfstate")

	state := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.5.0",
		"resources": []map[string]interface{}{
			{
				"mode":     "managed",
				"type":     "google_compute_instance",
				"name":     "labeled-instance",
				"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
				"instances": []map[string]interface{}{
					{
						"schema_version": 0,
						"attributes": map[string]interface{}{
							"id":   "labeled-instance",
							"name": "labeled-instance",
							"zone": "us-central1-a",
							"labels": map[string]interface{}{
								"environment": "staging",
								"owner":       "devops",
								"cost-center": "engineering",
							},
						},
					},
				},
			},
		},
	}

	stateData, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, stateData, 0644); err != nil {
		t.Fatalf("Failed to write state: %v", err)
	}

	p := gcpparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	for _, res := range infra.Resources {
		if res.Tags["environment"] != "staging" {
			t.Errorf("Expected environment label 'staging', got '%s'", res.Tags["environment"])
		}
		if res.Tags["owner"] != "devops" {
			t.Errorf("Expected owner label 'devops', got '%s'", res.Tags["owner"])
		}
		if res.Tags["cost-center"] != "engineering" {
			t.Errorf("Expected cost-center label 'engineering', got '%s'", res.Tags["cost-center"])
		}
	}
}

// TestTFStateParser_ExtractsRegion tests region extraction from various attributes.
func TestTFStateParser_ExtractsRegion(t *testing.T) {
	tests := []struct {
		name           string
		attributes     map[string]interface{}
		expectedRegion string
	}{
		{
			name: "From region attribute",
			attributes: map[string]interface{}{
				"id":     "test",
				"name":   "test",
				"region": "us-east1",
			},
			expectedRegion: "us-east1",
		},
		{
			name: "From zone attribute",
			attributes: map[string]interface{}{
				"id":   "test",
				"name": "test",
				"zone": "europe-west1-b",
			},
			expectedRegion: "europe-west1",
		},
		{
			name: "From location attribute",
			attributes: map[string]interface{}{
				"id":       "test",
				"name":     "test",
				"location": "asia-northeast1",
			},
			expectedRegion: "asia-northeast1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "terraform.tfstate")

			state := map[string]interface{}{
				"version":           4,
				"terraform_version": "1.5.0",
				"resources": []map[string]interface{}{
					{
						"mode":     "managed",
						"type":     "google_compute_instance",
						"name":     "test",
						"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
						"instances": []map[string]interface{}{
							{
								"schema_version": 0,
								"attributes":     tt.attributes,
							},
						},
					},
				},
			}

			stateData, _ := json.Marshal(state)
			if err := os.WriteFile(statePath, stateData, 0644); err != nil {
				t.Fatalf("Failed to write state: %v", err)
			}

			p := gcpparser.NewTFStateParser()
			ctx := context.Background()
			opts := parser.NewParseOptions()

			infra, err := p.Parse(ctx, statePath, opts)
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

// TestTFStateParser_Validate tests validation of state files.
func TestTFStateParser_Validate(t *testing.T) {
	p := gcpparser.NewTFStateParser()

	// Test with non-existent path
	err := p.Validate("/non/existent/path.tfstate")
	if err == nil {
		t.Error("Expected error for non-existent path")
	}

	// Test with valid GCP state file
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "terraform.tfstate")

	state := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.5.0",
		"resources": []map[string]interface{}{
			{
				"mode":     "managed",
				"type":     "google_compute_instance",
				"name":     "test",
				"provider": "provider[\"registry.terraform.io/hashicorp/google\"]",
				"instances": []map[string]interface{}{
					{
						"schema_version": 0,
						"attributes": map[string]interface{}{
							"id":   "test",
							"name": "test",
						},
					},
				},
			},
		},
	}

	stateData, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, stateData, 0644); err != nil {
		t.Fatalf("Failed to write state: %v", err)
	}

	if err := p.Validate(statePath); err != nil {
		t.Errorf("Expected valid state to pass validation: %v", err)
	}
}

// TestTFStateParser_AutoDetect tests auto-detection of GCP state files.
func TestTFStateParser_AutoDetect(t *testing.T) {
	p := gcpparser.NewTFStateParser()

	// Create state with GCP resources
	tmpDir := t.TempDir()
	gcpStatePath := filepath.Join(tmpDir, "gcp.tfstate")

	gcpState := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.5.0",
		"resources": []map[string]interface{}{
			{
				"mode": "managed",
				"type": "google_compute_instance",
				"name": "test",
				"instances": []map[string]interface{}{
					{"attributes": map[string]interface{}{"id": "test"}},
				},
			},
		},
	}

	stateData, _ := json.Marshal(gcpState)
	if err := os.WriteFile(gcpStatePath, stateData, 0644); err != nil {
		t.Fatalf("Failed to write state: %v", err)
	}

	canHandle, confidence := p.AutoDetect(gcpStatePath)
	if !canHandle {
		t.Error("Expected AutoDetect to return true for GCP state")
	}
	if confidence < 0.8 {
		t.Errorf("Expected high confidence for GCP-only state, got %f", confidence)
	}

	// Create state with AWS resources only
	awsStatePath := filepath.Join(tmpDir, "aws.tfstate")
	awsState := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.5.0",
		"resources": []map[string]interface{}{
			{
				"mode": "managed",
				"type": "aws_instance",
				"name": "test",
				"instances": []map[string]interface{}{
					{"attributes": map[string]interface{}{"id": "test"}},
				},
			},
		},
	}

	awsStateData, _ := json.Marshal(awsState)
	if err := os.WriteFile(awsStatePath, awsStateData, 0644); err != nil {
		t.Fatalf("Failed to write state: %v", err)
	}

	canHandle, _ = p.AutoDetect(awsStatePath)
	if canHandle {
		t.Error("Expected AutoDetect to return false for AWS-only state")
	}
}

// TestTFStateParser_Provider verifies the provider.
func TestTFStateParser_Provider(t *testing.T) {
	p := gcpparser.NewTFStateParser()

	if p.Provider() != resource.ProviderGCP {
		t.Errorf("Expected provider %s, got %s", resource.ProviderGCP, p.Provider())
	}
}

// TestTFStateParser_SupportedFormats verifies supported formats.
func TestTFStateParser_SupportedFormats(t *testing.T) {
	p := gcpparser.NewTFStateParser()

	formats := p.SupportedFormats()
	if len(formats) != 1 {
		t.Errorf("Expected 1 supported format, got %d", len(formats))
	}

	if formats[0] != parser.FormatTFState {
		t.Errorf("Expected format %s, got %s", parser.FormatTFState, formats[0])
	}
}

// TestTFStateParser_SkipsDataSources tests that data sources are not parsed.
func TestTFStateParser_SkipsDataSources(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "terraform.tfstate")

	state := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.5.0",
		"resources": []map[string]interface{}{
			{
				"mode": "data",
				"type": "google_compute_image",
				"name": "ubuntu",
				"instances": []map[string]interface{}{
					{"attributes": map[string]interface{}{"id": "ubuntu-image"}},
				},
			},
			{
				"mode": "managed",
				"type": "google_compute_instance",
				"name": "server",
				"instances": []map[string]interface{}{
					{"attributes": map[string]interface{}{"id": "server", "name": "server"}},
				},
			},
		},
	}

	stateData, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, stateData, 0644); err != nil {
		t.Fatalf("Failed to write state: %v", err)
	}

	p := gcpparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	infra, err := p.Parse(ctx, statePath, opts)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Should only have the managed resource, not the data source
	if len(infra.Resources) != 1 {
		t.Errorf("Expected 1 resource (data sources should be skipped), got %d", len(infra.Resources))
	}
}

// TestTFStateParser_DirectoryWithState tests parsing directory containing terraform.tfstate.
func TestTFStateParser_DirectoryWithState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "terraform.tfstate")

	state := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.5.0",
		"resources": []map[string]interface{}{
			{
				"mode": "managed",
				"type": "google_storage_bucket",
				"name": "data",
				"instances": []map[string]interface{}{
					{"attributes": map[string]interface{}{"id": "data-bucket", "name": "data-bucket"}},
				},
			},
		},
	}

	stateData, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, stateData, 0644); err != nil {
		t.Fatalf("Failed to write state: %v", err)
	}

	p := gcpparser.NewTFStateParser()
	ctx := context.Background()
	opts := parser.NewParseOptions()

	// Parse directory instead of file
	infra, err := p.Parse(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	if len(infra.Resources) != 1 {
		t.Errorf("Expected 1 resource from directory, got %d", len(infra.Resources))
	}
}
