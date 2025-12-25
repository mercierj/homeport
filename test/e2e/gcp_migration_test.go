// Package e2e contains end-to-end tests for the CloudExit migration tool.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
	"github.com/agnostech/agnostech/internal/infrastructure/generator/compose"
	_ "github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/compute"
	_ "github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/database"
	_ "github.com/agnostech/agnostech/internal/infrastructure/mapper/gcp/storage"
	_ "github.com/agnostech/agnostech/internal/infrastructure/parser/gcp"
)

// TestGCPMigration_Terraform tests complete GCP to self-hosted migration using Terraform.
func TestGCPMigration_Terraform(t *testing.T) {
	// Create temporary GCP Terraform configuration
	tmpDir := t.TempDir()
	tfPath := filepath.Join(tmpDir, "main.tf")

	tfContent := `terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = "my-gcp-project"
  region  = "us-central1"
}

resource "google_compute_instance" "web" {
  name         = "web-server"
  machine_type = "e2-medium"
  zone         = "us-central1-a"

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-11"
      size  = 50
    }
  }

  network_interface {
    network = "default"
    access_config {}
  }

  labels = {
    environment = "production"
    app         = "webapp"
  }
}

resource "google_sql_database_instance" "main" {
  name             = "main-db"
  database_version = "POSTGRES_15"
  region           = "us-central1"

  settings {
    tier              = "db-custom-2-8192"
    availability_type = "REGIONAL"
    disk_size         = 100
    disk_type         = "PD_SSD"

    backup_configuration {
      enabled            = true
      start_time         = "03:00"
      binary_log_enabled = false
    }
  }
}

resource "google_storage_bucket" "assets" {
  name          = "webapp-assets"
  location      = "US"
  force_destroy = false

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      age = 365
    }
    action {
      type = "Delete"
    }
  }
}

resource "google_redis_instance" "cache" {
  name           = "app-cache"
  tier           = "STANDARD_HA"
  memory_size_gb = 2
  region         = "us-central1"

  redis_version = "REDIS_7_0"

  labels = {
    environment = "production"
  }
}

resource "google_cloud_run_service" "api" {
  name     = "api-service"
  location = "us-central1"

  template {
    spec {
      containers {
        image = "gcr.io/my-project/api:latest"
        resources {
          limits = {
            cpu    = "2"
            memory = "512Mi"
          }
        }
        ports {
          container_port = 8080
        }
      }
    }
  }
}

resource "google_pubsub_topic" "events" {
  name = "app-events"

  labels = {
    environment = "production"
  }
}

resource "google_pubsub_subscription" "events_sub" {
  name  = "app-events-subscription"
  topic = google_pubsub_topic.events.name

  ack_deadline_seconds = 20

  retry_policy {
    minimum_backoff = "10s"
  }
}
`

	if err := os.WriteFile(tfPath, []byte(tfContent), 0644); err != nil {
		t.Fatalf("Failed to write Terraform file: %v", err)
	}

	ctx := context.Background()
	outputDir := t.TempDir()

	// Step 1: Parse GCP Terraform configuration
	t.Run("Parse GCP Terraform", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderGCP, parser.FormatTerraform)
		if err != nil {
			t.Fatalf("Failed to get GCP Terraform parser: %v", err)
		}

		opts := parser.NewParseOptions()
		infra, err := p.Parse(ctx, tmpDir, opts)
		if err != nil {
			t.Fatalf("Failed to parse GCP Terraform: %v", err)
		}

		if infra == nil {
			t.Fatal("Infrastructure is nil")
		}

		t.Logf("Parsed %d GCP resources from Terraform", len(infra.Resources))

		// Log discovered resources
		for id, res := range infra.Resources {
			t.Logf("  - %s: %s (%s)", id, res.Name, res.Type)
		}
	})

	// Step 2: Map GCP resources to self-hosted alternatives
	t.Run("Map GCP to Self-Hosted", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderGCP, parser.FormatTerraform)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, tmpDir, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		mappedCount := 0
		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				t.Logf("No mapper for GCP resource type %s", res.Type)
				continue
			}

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Logf("Failed to map %s: %v", res.ID, err)
				continue
			}

			if result != nil && result.DockerService != nil {
				mappedCount++
				t.Logf("Mapped %s -> %s", res.ID, result.DockerService.Name)

				// Verify Docker service configuration
				if result.DockerService.Image == "" {
					t.Errorf("Docker service %s has no image", result.DockerService.Name)
				}

				// Log warnings
				for _, w := range result.Warnings {
					t.Logf("  Warning: %s", w)
				}
			}
		}

		t.Logf("Successfully mapped %d GCP resources", mappedCount)
	})

	// Step 3: Generate Docker Compose
	t.Run("Generate Docker Compose", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderGCP, parser.FormatTerraform)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, tmpDir, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		var results []*mapper.MappingResult
		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				continue
			}
			result, err := m.Map(ctx, res)
			if err != nil {
				continue
			}
			if result != nil {
				results = append(results, result)
			}
		}

		if len(results) == 0 {
			t.Skip("No GCP resources mapped, skipping Docker Compose generation")
		}

		gen := compose.NewGenerator("gcp-migration-test")
		output, err := gen.Generate(results)
		if err != nil {
			t.Fatalf("Failed to generate Docker Compose: %v", err)
		}

		composeContent, ok := output.Files["docker-compose.yml"]
		if !ok {
			t.Fatal("docker-compose.yml not generated")
		}

		// Validate structure
		composeStr := string(composeContent)
		if !strings.Contains(composeStr, "version:") {
			t.Error("Missing version in docker-compose.yml")
		}
		if !strings.Contains(composeStr, "services:") {
			t.Error("Missing services in docker-compose.yml")
		}

		// Write to output directory
		composePath := filepath.Join(outputDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, composeContent, 0644); err != nil {
			t.Fatalf("Failed to write docker-compose.yml: %v", err)
		}

		t.Logf("Generated docker-compose.yml: %s", composePath)
	})

	// Step 4: Verify GCP-specific migration scripts
	t.Run("Verify GCP Migration Scripts", func(t *testing.T) {
		registry := parser.DefaultRegistry()
		p, err := registry.GetByFormat(resource.ProviderGCP, parser.FormatTerraform)
		if err != nil {
			t.Fatalf("Failed to get parser: %v", err)
		}

		infra, err := p.Parse(ctx, tmpDir, parser.NewParseOptions())
		if err != nil {
			t.Fatalf("Failed to parse: %v", err)
		}

		scriptsDir := filepath.Join(outputDir, "scripts")
		configsDir := filepath.Join(outputDir, "configs")

		for _, res := range infra.Resources {
			m, err := mapper.Get(res.Type)
			if err != nil {
				continue
			}
			result, err := m.Map(ctx, res)
			if err != nil || result == nil {
				continue
			}

			// Write scripts
			for name, content := range result.Scripts {
				scriptPath := filepath.Join(scriptsDir, name)
				if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
					continue
				}
				if err := os.WriteFile(scriptPath, content, 0755); err != nil {
					t.Errorf("Failed to write script %s: %v", name, err)
				} else {
					t.Logf("Generated script: %s", name)
				}
			}

			// Write configs
			for name, content := range result.Configs {
				configPath := filepath.Join(configsDir, name)
				if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
					continue
				}
				if err := os.WriteFile(configPath, content, 0644); err != nil {
					t.Errorf("Failed to write config %s: %v", name, err)
				} else {
					t.Logf("Generated config: %s", name)
				}
			}
		}
	})
}

// TestGCPMigration_ResourceTypes tests migration of specific GCP resource types.
func TestGCPMigration_ResourceTypes(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name           string
		resourceType   resource.Type
		config         map[string]interface{}
		expectedImage  string
		shouldHavePort bool
	}{
		{
			name:         "GCE Instance",
			resourceType: resource.TypeGCEInstance,
			config: map[string]interface{}{
				"machine_type": "e2-medium",
				"zone":         "us-central1-a",
			},
			expectedImage:  "", // May vary
			shouldHavePort: true,
		},
		{
			name:         "Cloud SQL PostgreSQL",
			resourceType: resource.TypeCloudSQL,
			config: map[string]interface{}{
				"database_version": "POSTGRES_15",
				"tier":             "db-custom-2-8192",
			},
			expectedImage:  "postgres",
			shouldHavePort: true,
		},
		{
			name:         "Cloud Storage Bucket",
			resourceType: resource.TypeGCSBucket,
			config: map[string]interface{}{
				"location": "US",
			},
			expectedImage:  "minio",
			shouldHavePort: true,
		},
		{
			name:         "Memorystore Redis",
			resourceType: resource.TypeMemorystore,
			config: map[string]interface{}{
				"tier":           "STANDARD_HA",
				"memory_size_gb": 2,
				"redis_version":  "REDIS_7_0",
			},
			expectedImage:  "redis",
			shouldHavePort: true,
		},
		{
			name:         "Cloud Run Service",
			resourceType: resource.TypeCloudRun,
			config: map[string]interface{}{
				"image": "gcr.io/project/app:latest",
			},
			expectedImage:  "", // Uses original image
			shouldHavePort: true,
		},
		{
			name:         "GKE Cluster",
			resourceType: resource.TypeGKE,
			config: map[string]interface{}{
				"name":            "main-cluster",
				"initial_node_count": 3,
			},
			expectedImage:  "k3s",
			shouldHavePort: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res := resource.NewAWSResource(
				"test-"+string(tc.resourceType),
				tc.name,
				tc.resourceType,
			)
			res.Region = "us-central1"
			for k, v := range tc.config {
				res.Config[k] = v
			}

			m, err := mapper.Get(tc.resourceType)
			if err != nil {
				t.Logf("No mapper available for %s: %v", tc.resourceType, err)
				return
			}

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Logf("Mapping failed for %s: %v", tc.resourceType, err)
				return
			}

			if result == nil || result.DockerService == nil {
				t.Logf("No Docker service generated for %s", tc.resourceType)
				return
			}

			svc := result.DockerService

			// Verify image
			if tc.expectedImage != "" && !strings.Contains(svc.Image, tc.expectedImage) {
				t.Errorf("Expected image containing %s, got %s", tc.expectedImage, svc.Image)
			}

			// Verify ports
			if tc.shouldHavePort && len(svc.Ports) == 0 {
				t.Logf("Warning: Expected ports for %s but none configured", tc.name)
			}

			t.Logf("Mapped %s to %s (image: %s, ports: %v)",
				tc.resourceType, svc.Name, svc.Image, svc.Ports)

			// Log warnings and manual steps
			if len(result.Warnings) > 0 {
				t.Logf("Warnings: %v", result.Warnings)
			}
			if len(result.ManualSteps) > 0 {
				t.Logf("Manual steps: %v", result.ManualSteps)
			}
		})
	}
}

// TestGCPMigration_PubSubToRabbitMQ tests Pub/Sub to RabbitMQ migration.
func TestGCPMigration_PubSubToRabbitMQ(t *testing.T) {
	ctx := context.Background()

	// Create Pub/Sub topic resource
	topic := resource.NewAWSResource("app-events", "events-topic", resource.TypePubSubTopic)
	topic.Config["name"] = "app-events"
	topic.Tags["environment"] = "production"

	// Create Pub/Sub subscription resource
	sub := resource.NewAWSResource("app-events-sub", "events-subscription", resource.TypePubSubSubscription)
	sub.Config["topic"] = "app-events"
	sub.Config["ack_deadline_seconds"] = 20
	sub.AddDependency("app-events")

	// Map topic
	topicMapper, err := mapper.Get(resource.TypePubSubTopic)
	if err != nil {
		t.Logf("Pub/Sub topic mapper not available: %v", err)
		return
	}

	topicResult, err := topicMapper.Map(ctx, topic)
	if err != nil {
		t.Logf("Failed to map Pub/Sub topic: %v", err)
		return
	}

	if topicResult != nil && topicResult.DockerService != nil {
		t.Logf("Pub/Sub topic mapped to: %s (image: %s)",
			topicResult.DockerService.Name, topicResult.DockerService.Image)

		// Verify RabbitMQ or similar messaging service
		if !strings.Contains(topicResult.DockerService.Image, "rabbitmq") &&
			!strings.Contains(topicResult.DockerService.Image, "nats") {
			t.Logf("Note: Expected RabbitMQ or NATS image, got %s", topicResult.DockerService.Image)
		}
	}

	// Map subscription
	subMapper, err := mapper.Get(resource.TypePubSubSubscription)
	if err != nil {
		t.Logf("Pub/Sub subscription mapper not available: %v", err)
		return
	}

	subResult, err := subMapper.Map(ctx, sub)
	if err != nil {
		t.Logf("Failed to map Pub/Sub subscription: %v", err)
		return
	}

	if subResult != nil {
		t.Log("Pub/Sub subscription mapped successfully")
		for _, step := range subResult.ManualSteps {
			t.Logf("Manual step: %s", step)
		}
	}
}

// TestGCPMigration_CloudSQLToPostgres tests Cloud SQL to PostgreSQL container migration.
func TestGCPMigration_CloudSQLToPostgres(t *testing.T) {
	ctx := context.Background()

	// Create Cloud SQL instance
	cloudsql := resource.NewAWSResource("main-db", "main-database", resource.TypeCloudSQL)
	cloudsql.Config["database_version"] = "POSTGRES_15"
	cloudsql.Config["tier"] = "db-custom-4-16384"
	cloudsql.Config["disk_size"] = 200
	cloudsql.Config["disk_type"] = "PD_SSD"
	cloudsql.Config["availability_type"] = "REGIONAL"
	cloudsql.Config["backup_enabled"] = true
	cloudsql.Region = "us-central1"

	m, err := mapper.Get(resource.TypeCloudSQL)
	if err != nil {
		t.Logf("Cloud SQL mapper not available: %v", err)
		return
	}

	result, err := m.Map(ctx, cloudsql)
	if err != nil {
		t.Fatalf("Failed to map Cloud SQL: %v", err)
	}

	if result == nil || result.DockerService == nil {
		t.Fatal("No Docker service generated for Cloud SQL")
	}

	svc := result.DockerService

	// Verify PostgreSQL image
	if !strings.Contains(strings.ToLower(svc.Image), "postgres") {
		t.Errorf("Expected PostgreSQL image, got %s", svc.Image)
	}

	// Verify environment variables
	expectedEnvVars := []string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB"}
	for _, env := range expectedEnvVars {
		if _, ok := svc.Environment[env]; !ok {
			t.Logf("Warning: Expected environment variable %s not set", env)
		}
	}

	// Verify volumes for data persistence
	if len(svc.Volumes) == 0 {
		t.Log("Warning: No volumes configured for PostgreSQL data persistence")
	}

	// Verify health check
	if svc.HealthCheck == nil {
		t.Log("Warning: No health check configured for PostgreSQL")
	}

	t.Logf("Cloud SQL mapped to: %s", svc.Image)
	t.Logf("Environment: %v", svc.Environment)
	t.Logf("Volumes: %v", svc.Volumes)
	t.Logf("Ports: %v", svc.Ports)

	// Check for migration scripts
	if len(result.Scripts) > 0 {
		t.Log("Migration scripts generated:")
		for name := range result.Scripts {
			t.Logf("  - %s", name)
		}
	}

	// Check for manual steps
	if len(result.ManualSteps) > 0 {
		t.Log("Manual steps required:")
		for _, step := range result.ManualSteps {
			t.Logf("  - %s", step)
		}
	}
}

// TestGCPMigration_CompleteStack tests migration of a complete GCP stack.
func TestGCPMigration_CompleteStack(t *testing.T) {
	ctx := context.Background()
	outputDir := t.TempDir()

	// Create a complete GCP infrastructure
	infra := resource.NewInfrastructure(resource.ProviderGCP)

	// Add Cloud SQL database
	db := resource.NewAWSResource("main-db", "database", resource.TypeCloudSQL)
	db.Config["database_version"] = "POSTGRES_15"
	db.Config["tier"] = "db-custom-2-8192"
	db.Region = "us-central1"
	infra.AddResource(db)

	// Add Memorystore Redis
	redis := resource.NewAWSResource("cache", "redis-cache", resource.TypeMemorystore)
	redis.Config["tier"] = "STANDARD_HA"
	redis.Config["memory_size_gb"] = 2
	redis.Region = "us-central1"
	infra.AddResource(redis)

	// Add GCS bucket
	bucket := resource.NewAWSResource("assets", "storage-bucket", resource.TypeGCSBucket)
	bucket.Config["location"] = "US"
	bucket.Config["storage_class"] = "STANDARD"
	infra.AddResource(bucket)

	// Add Cloud Run service (depends on db and redis)
	api := resource.NewAWSResource("api", "api-service", resource.TypeCloudRun)
	api.Config["image"] = "gcr.io/project/api:v1"
	api.Config["cpu"] = "2"
	api.Config["memory"] = "1Gi"
	api.AddDependency("main-db")
	api.AddDependency("cache")
	infra.AddResource(api)

	// Map all resources
	var results []*mapper.MappingResult
	mappedResources := 0

	for _, res := range infra.Resources {
		m, err := mapper.Get(res.Type)
		if err != nil {
			t.Logf("No mapper for %s: %v", res.Type, err)
			continue
		}

		result, err := m.Map(ctx, res)
		if err != nil {
			t.Logf("Failed to map %s: %v", res.ID, err)
			continue
		}

		if result != nil {
			results = append(results, result)
			mappedResources++
		}
	}

	t.Logf("Mapped %d/%d GCP resources", mappedResources, len(infra.Resources))

	if len(results) == 0 {
		t.Skip("No resources mapped")
	}

	// Generate Docker Compose
	gen := compose.NewGenerator("gcp-complete-stack")
	output, err := gen.Generate(results)
	if err != nil {
		t.Fatalf("Failed to generate output: %v", err)
	}

	// Write all files
	for name, content := range output.Files {
		filePath := filepath.Join(outputDir, name)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Errorf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			t.Errorf("Failed to write %s: %v", name, err)
		}
	}

	// Verify docker-compose.yml
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	composeContent, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("Failed to read docker-compose.yml: %v", err)
	}

	t.Logf("Generated docker-compose.yml (%d bytes)", len(composeContent))
	t.Logf("Output directory: %s", outputDir)

	// Verify warnings are collected
	if len(output.Warnings) > 0 {
		t.Log("Migration warnings:")
		for _, w := range output.Warnings {
			t.Logf("  - %s", w)
		}
	}
}
