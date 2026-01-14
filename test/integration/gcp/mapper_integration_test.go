package gcp_test

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/compute"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/database"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/messaging"
	"github.com/homeport/homeport/internal/infrastructure/mapper/gcp/storage"
)

// TestGCSToMinIO_BasicMapping tests GCS bucket to MinIO mapping.
func TestGCSToMinIO_BasicMapping(t *testing.T) {
	m := storage.NewGCSMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_storage_bucket.assets",
		"my-app-assets",
		resource.TypeGCSBucket,
	)
	res.Config["name"] = "my-app-assets"
	res.Config["location"] = "US"
	res.Config["storage_class"] = "STANDARD"

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map GCS bucket: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil mapping result")
	}

	// Verify Docker service configuration
	svc := result.DockerService
	if svc == nil {
		t.Fatal("Expected non-nil Docker service")
	}

	// Check image
	if svc.Image != "minio/minio:latest" {
		t.Errorf("Expected minio/minio:latest image, got %s", svc.Image)
	}

	// Check ports
	if len(svc.Ports) < 2 {
		t.Errorf("Expected at least 2 ports (API + Console), got %d", len(svc.Ports))
	}

	// Check environment variables
	if svc.Environment["MINIO_ROOT_USER"] == "" {
		t.Error("Expected MINIO_ROOT_USER environment variable")
	}
	if svc.Environment["MINIO_ROOT_PASSWORD"] == "" {
		t.Error("Expected MINIO_ROOT_PASSWORD environment variable")
	}

	// Check labels
	if svc.Labels["homeport.source"] != "google_storage_bucket" {
		t.Errorf("Expected homeport.source label, got %s", svc.Labels["homeport.source"])
	}

	t.Logf("GCS bucket mapped to MinIO service: %s", svc.Image)
}

// TestGCSToMinIO_WithVersioning tests GCS bucket with versioning.
func TestGCSToMinIO_WithVersioning(t *testing.T) {
	m := storage.NewGCSMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_storage_bucket.versioned",
		"versioned-bucket",
		resource.TypeGCSBucket,
	)
	res.Config["name"] = "versioned-bucket"
	res.Config["location"] = "US"
	res.Config["versioning"] = map[string]interface{}{
		"enabled": true,
	}

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map: %v", err)
	}

	// Should have manual step for versioning
	hasVersioningStep := false
	for _, step := range result.ManualSteps {
		if containsString(step, "versioning") || containsString(step, "version") {
			hasVersioningStep = true
			break
		}
	}

	if !hasVersioningStep {
		t.Error("Expected manual step for versioning configuration")
	}
}

// TestGCSToMinIO_GeneratesSetupScript tests that setup script is generated.
func TestGCSToMinIO_GeneratesSetupScript(t *testing.T) {
	m := storage.NewGCSMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_storage_bucket.test",
		"test-bucket",
		resource.TypeGCSBucket,
	)
	res.Config["name"] = "test-bucket"
	res.Config["location"] = "US"

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map: %v", err)
	}

	// Check for setup script
	if len(result.Scripts) == 0 {
		t.Error("Expected at least one setup script")
	}

	foundSetupScript := false
	for name := range result.Scripts {
		if containsString(name, "setup") || containsString(name, "minio") {
			foundSetupScript = true
			break
		}
	}

	if !foundSetupScript {
		t.Error("Expected setup script for MinIO")
	}
}

// TestGCEToDocker_BasicMapping tests GCE instance to Docker mapping.
func TestGCEToDocker_BasicMapping(t *testing.T) {
	m := compute.NewGCEMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_compute_instance.web",
		"web-server",
		resource.TypeGCEInstance,
	)
	res.Config["name"] = "web-server"
	res.Config["machine_type"] = "n2-standard-4"
	res.Config["zone"] = "us-central1-a"
	res.Config["boot_disk"] = map[string]interface{}{
		"initialize_params": map[string]interface{}{
			"image": "ubuntu-2204-lts",
		},
	}

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map GCE instance: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil mapping result")
	}

	svc := result.DockerService
	if svc == nil {
		t.Fatal("Expected non-nil Docker service")
	}

	// Check that base image is set
	if svc.Image == "" {
		t.Error("Expected Docker image to be set")
	}

	// Check labels
	if svc.Labels["homeport.source"] != "google_compute_instance" {
		t.Errorf("Expected homeport.source label")
	}

	// Check resource limits are set for machine type
	if svc.Deploy != nil && svc.Deploy.Resources != nil {
		t.Logf("Resource limits: CPUs=%s, Memory=%s",
			svc.Deploy.Resources.Limits.CPUs,
			svc.Deploy.Resources.Limits.Memory)
	}

	t.Logf("GCE instance mapped to Docker: %s", svc.Image)
}

// TestGCEToDocker_MachineTypeMapping tests machine type to resource limits mapping.
func TestGCEToDocker_MachineTypeMapping(t *testing.T) {
	tests := []struct {
		machineType    string
		expectedCPUs   string
		expectedMemory string
	}{
		{"e2-micro", "0.25", "256M"},
		{"e2-small", "0.5", "512M"},
		{"e2-medium", "1", "2G"},
		{"n2-standard-2", "2", "8G"},
		{"n2-standard-4", "4", "16G"},
		{"n2-standard-8", "8", "32G"},
	}

	for _, tt := range tests {
		t.Run(tt.machineType, func(t *testing.T) {
			m := compute.NewGCEMapper()
			ctx := context.Background()

			res := resource.NewAWSResource(
				"google_compute_instance.test",
				"test",
				resource.TypeGCEInstance,
			)
			res.Config["name"] = "test"
			res.Config["machine_type"] = tt.machineType
			res.Config["zone"] = "us-central1-a"

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Fatalf("Failed to map: %v", err)
			}

			svc := result.DockerService
			if svc.Deploy == nil || svc.Deploy.Resources == nil || svc.Deploy.Resources.Limits == nil {
				t.Fatal("Expected resource limits to be set")
			}

			if svc.Deploy.Resources.Limits.CPUs != tt.expectedCPUs {
				t.Errorf("Expected CPUs %s, got %s", tt.expectedCPUs, svc.Deploy.Resources.Limits.CPUs)
			}

			if svc.Deploy.Resources.Limits.Memory != tt.expectedMemory {
				t.Errorf("Expected Memory %s, got %s", tt.expectedMemory, svc.Deploy.Resources.Limits.Memory)
			}
		})
	}
}

// TestGCEToDocker_ImageMapping tests GCP image to Docker image mapping.
func TestGCEToDocker_ImageMapping(t *testing.T) {
	tests := []struct {
		gcpImage    string
		dockerImage string
	}{
		{"ubuntu-2204-lts", "ubuntu:22.04"},
		{"ubuntu-2004-lts", "ubuntu:20.04"},
		{"debian-11", "debian:bullseye"},
		{"debian-12", "debian:bookworm"},
		{"centos-7", "centos:7"},
	}

	for _, tt := range tests {
		t.Run(tt.gcpImage, func(t *testing.T) {
			m := compute.NewGCEMapper()
			ctx := context.Background()

			res := resource.NewAWSResource(
				"google_compute_instance.test",
				"test",
				resource.TypeGCEInstance,
			)
			res.Config["name"] = "test"
			res.Config["machine_type"] = "n1-standard-1"
			res.Config["zone"] = "us-central1-a"
			res.Config["boot_disk"] = map[string]interface{}{
				"initialize_params": map[string]interface{}{
					"image": tt.gcpImage,
				},
			}

			result, err := m.Map(ctx, res)
			if err != nil {
				t.Fatalf("Failed to map: %v", err)
			}

			if result.DockerService.Image != tt.dockerImage {
				t.Errorf("Expected Docker image %s, got %s", tt.dockerImage, result.DockerService.Image)
			}
		})
	}
}

// TestCloudSQLToPostgres_BasicMapping tests CloudSQL PostgreSQL to PostgreSQL Docker mapping.
func TestCloudSQLToPostgres_BasicMapping(t *testing.T) {
	m := database.NewCloudSQLMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_sql_database_instance.main",
		"main-db",
		resource.TypeCloudSQL,
	)
	res.Config["name"] = "main-db"
	res.Config["database_version"] = "POSTGRES_14"
	res.Config["region"] = "us-central1"
	res.Config["settings"] = map[string]interface{}{
		"tier": "db-custom-2-7680",
	}

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map CloudSQL: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil mapping result")
	}

	svc := result.DockerService
	if svc == nil {
		t.Fatal("Expected non-nil Docker service")
	}

	// Check image contains postgres
	if !containsString(svc.Image, "postgres") {
		t.Errorf("Expected postgres image, got %s", svc.Image)
	}

	// Check version is extracted
	if !containsString(svc.Image, "14") {
		t.Errorf("Expected postgres:14 in image, got %s", svc.Image)
	}

	// Check environment
	if svc.Environment["POSTGRES_DB"] == "" {
		t.Error("Expected POSTGRES_DB environment variable")
	}
	if svc.Environment["POSTGRES_USER"] == "" {
		t.Error("Expected POSTGRES_USER environment variable")
	}
	if svc.Environment["POSTGRES_PASSWORD"] == "" {
		t.Error("Expected POSTGRES_PASSWORD environment variable")
	}

	// Check ports
	hasPort := false
	for _, port := range svc.Ports {
		if containsString(port, "5432") {
			hasPort = true
			break
		}
	}
	if !hasPort {
		t.Error("Expected port 5432 for PostgreSQL")
	}

	// Check labels
	if svc.Labels["homeport.engine"] != "postgres" {
		t.Errorf("Expected engine label 'postgres', got %s", svc.Labels["homeport.engine"])
	}

	t.Logf("CloudSQL PostgreSQL mapped to: %s", svc.Image)
}

// TestCloudSQLToMySQL_BasicMapping tests CloudSQL MySQL to MySQL Docker mapping.
func TestCloudSQLToMySQL_BasicMapping(t *testing.T) {
	m := database.NewCloudSQLMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_sql_database_instance.mysql",
		"mysql-db",
		resource.TypeCloudSQL,
	)
	res.Config["name"] = "mysql-db"
	res.Config["database_version"] = "MYSQL_8_0"
	res.Config["region"] = "us-central1"

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map CloudSQL MySQL: %v", err)
	}

	svc := result.DockerService

	// Check image contains mysql
	if !containsString(svc.Image, "mysql") {
		t.Errorf("Expected mysql image, got %s", svc.Image)
	}

	// Check environment
	if svc.Environment["MYSQL_ROOT_PASSWORD"] == "" {
		t.Error("Expected MYSQL_ROOT_PASSWORD environment variable")
	}

	// Check ports
	hasPort := false
	for _, port := range svc.Ports {
		if containsString(port, "3306") {
			hasPort = true
			break
		}
	}
	if !hasPort {
		t.Error("Expected port 3306 for MySQL")
	}

	// Check labels
	if svc.Labels["homeport.engine"] != "mysql" {
		t.Errorf("Expected engine label 'mysql', got %s", svc.Labels["homeport.engine"])
	}

	t.Logf("CloudSQL MySQL mapped to: %s", svc.Image)
}

// TestCloudSQL_GeneratesMigrationScript tests migration script generation.
func TestCloudSQL_GeneratesMigrationScript(t *testing.T) {
	m := database.NewCloudSQLMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_sql_database_instance.db",
		"db",
		resource.TypeCloudSQL,
	)
	res.Config["name"] = "db"
	res.Config["database_version"] = "POSTGRES_15"
	res.Config["region"] = "us-central1"

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map: %v", err)
	}

	// Check for migration script
	if len(result.Scripts) == 0 {
		t.Error("Expected migration script to be generated")
	}

	foundMigrationScript := false
	for name := range result.Scripts {
		if containsString(name, "migrate") {
			foundMigrationScript = true
			break
		}
	}

	if !foundMigrationScript {
		t.Error("Expected migration script for CloudSQL")
	}
}

// TestPubSubToRabbitMQ_BasicMapping tests Pub/Sub to RabbitMQ mapping.
func TestPubSubToRabbitMQ_BasicMapping(t *testing.T) {
	m := messaging.NewPubSubMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_pubsub_topic.events",
		"events-topic",
		resource.TypePubSubTopic,
	)
	res.Config["name"] = "events-topic"

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map Pub/Sub topic: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil mapping result")
	}

	svc := result.DockerService
	if svc == nil {
		t.Fatal("Expected non-nil Docker service")
	}

	// Check image contains rabbitmq
	if !containsString(svc.Image, "rabbitmq") {
		t.Errorf("Expected rabbitmq image, got %s", svc.Image)
	}

	// Check management plugin is included
	if !containsString(svc.Image, "management") {
		t.Errorf("Expected management image variant, got %s", svc.Image)
	}

	// Check ports (AMQP and Management)
	amqpPortFound := false
	mgmtPortFound := false
	for _, port := range svc.Ports {
		if containsString(port, "5672") {
			amqpPortFound = true
		}
		if containsString(port, "15672") {
			mgmtPortFound = true
		}
	}

	if !amqpPortFound {
		t.Error("Expected AMQP port 5672")
	}
	if !mgmtPortFound {
		t.Error("Expected Management port 15672")
	}

	// Check environment
	if svc.Environment["RABBITMQ_DEFAULT_USER"] == "" {
		t.Error("Expected RABBITMQ_DEFAULT_USER environment variable")
	}
	if svc.Environment["RABBITMQ_DEFAULT_PASS"] == "" {
		t.Error("Expected RABBITMQ_DEFAULT_PASS environment variable")
	}

	// Check labels
	if svc.Labels["homeport.source"] != "google_pubsub_topic" {
		t.Errorf("Expected homeport.source label")
	}
	if svc.Labels["homeport.topic_name"] != "events-topic" {
		t.Errorf("Expected topic name in labels")
	}

	t.Logf("Pub/Sub topic mapped to: %s", svc.Image)
}

// TestPubSubToRabbitMQ_GeneratesDefinitions tests RabbitMQ definitions generation.
func TestPubSubToRabbitMQ_GeneratesDefinitions(t *testing.T) {
	m := messaging.NewPubSubMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_pubsub_topic.orders",
		"orders-topic",
		resource.TypePubSubTopic,
	)
	res.Config["name"] = "orders-topic"

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map: %v", err)
	}

	// Check for RabbitMQ definitions config
	if len(result.Configs) == 0 {
		t.Error("Expected RabbitMQ definitions config")
	}

	foundDefinitions := false
	for path := range result.Configs {
		if containsString(path, "definitions") {
			foundDefinitions = true
			break
		}
	}

	if !foundDefinitions {
		t.Error("Expected definitions.json config file")
	}
}

// TestPubSubToRabbitMQ_MessageOrdering tests handling of message ordering.
func TestPubSubToRabbitMQ_MessageOrdering(t *testing.T) {
	m := messaging.NewPubSubMapper()
	ctx := context.Background()

	res := resource.NewAWSResource(
		"google_pubsub_topic.ordered",
		"ordered-topic",
		resource.TypePubSubTopic,
	)
	res.Config["name"] = "ordered-topic"
	res.Config["message_ordering_enabled"] = true

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map: %v", err)
	}

	// Should have warning about message ordering
	hasOrderingWarning := false
	for _, warning := range result.Warnings {
		if containsString(warning, "ordering") || containsString(warning, "order") {
			hasOrderingWarning = true
			break
		}
	}

	if !hasOrderingWarning {
		t.Error("Expected warning about message ordering")
	}
}

// TestMapperResourceTypeValidation tests that mappers validate resource types.
func TestMapperResourceTypeValidation(t *testing.T) {
	tests := []struct {
		name         string
		mapper       mapper.Mapper
		expectedType resource.Type
	}{
		{
			name:         "GCS Mapper",
			mapper:       storage.NewGCSMapper(),
			expectedType: resource.TypeGCSBucket,
		},
		{
			name:         "GCE Mapper",
			mapper:       compute.NewGCEMapper(),
			expectedType: resource.TypeGCEInstance,
		},
		{
			name:         "CloudSQL Mapper",
			mapper:       database.NewCloudSQLMapper(),
			expectedType: resource.TypeCloudSQL,
		},
		{
			name:         "PubSub Mapper",
			mapper:       messaging.NewPubSubMapper(),
			expectedType: resource.TypePubSubTopic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify ResourceType method
			if tt.mapper.ResourceType() != tt.expectedType {
				t.Errorf("Expected resource type %s, got %s",
					tt.expectedType, tt.mapper.ResourceType())
			}

			// Test validation with wrong type
			wrongRes := resource.NewAWSResource("test", "test", resource.TypeS3Bucket)
			err := tt.mapper.Validate(wrongRes)
			if err == nil {
				t.Error("Expected validation error for wrong resource type")
			}

			// Test validation with correct type
			correctRes := resource.NewAWSResource("test", "test", tt.expectedType)
			err = tt.mapper.Validate(correctRes)
			if err != nil {
				t.Errorf("Expected validation to pass for correct type: %v", err)
			}

			// Test validation with nil resource
			err = tt.mapper.Validate(nil)
			if err == nil {
				t.Error("Expected validation error for nil resource")
			}
		})
	}
}

// TestMapperHealthChecks tests that mappers configure health checks.
func TestMapperHealthChecks(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		mapper mapper.Mapper
		res    *resource.AWSResource
	}{
		{
			name:   "GCS/MinIO health check",
			mapper: storage.NewGCSMapper(),
			res: func() *resource.AWSResource {
				r := resource.NewAWSResource("test", "test", resource.TypeGCSBucket)
				r.Config["name"] = "test"
				r.Config["location"] = "US"
				return r
			}(),
		},
		{
			name:   "GCE/Docker health check",
			mapper: compute.NewGCEMapper(),
			res: func() *resource.AWSResource {
				r := resource.NewAWSResource("test", "test", resource.TypeGCEInstance)
				r.Config["name"] = "test"
				r.Config["machine_type"] = "n1-standard-1"
				r.Config["zone"] = "us-central1-a"
				return r
			}(),
		},
		{
			name:   "CloudSQL/Postgres health check",
			mapper: database.NewCloudSQLMapper(),
			res: func() *resource.AWSResource {
				r := resource.NewAWSResource("test", "test", resource.TypeCloudSQL)
				r.Config["name"] = "test"
				r.Config["database_version"] = "POSTGRES_14"
				return r
			}(),
		},
		{
			name:   "PubSub/RabbitMQ health check",
			mapper: messaging.NewPubSubMapper(),
			res: func() *resource.AWSResource {
				r := resource.NewAWSResource("test", "test", resource.TypePubSubTopic)
				r.Config["name"] = "test"
				return r
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(ctx, tt.res)
			if err != nil {
				t.Fatalf("Failed to map: %v", err)
			}

			svc := result.DockerService
			if svc.HealthCheck == nil {
				t.Error("Expected health check to be configured")
				return
			}

			if len(svc.HealthCheck.Test) == 0 {
				t.Error("Expected health check test command")
			}

			if svc.HealthCheck.Interval == 0 {
				t.Error("Expected health check interval to be set")
			}

			if svc.HealthCheck.Retries == 0 {
				t.Error("Expected health check retries to be set")
			}

			t.Logf("Health check: %v", svc.HealthCheck.Test)
		})
	}
}

// TestMapperNetworkConfiguration tests that mappers configure networks.
func TestMapperNetworkConfiguration(t *testing.T) {
	ctx := context.Background()

	m := storage.NewGCSMapper()
	res := resource.NewAWSResource("test", "test", resource.TypeGCSBucket)
	res.Config["name"] = "test"
	res.Config["location"] = "US"

	result, err := m.Map(ctx, res)
	if err != nil {
		t.Fatalf("Failed to map: %v", err)
	}

	svc := result.DockerService

	// Should have network configured
	if len(svc.Networks) == 0 {
		t.Error("Expected networks to be configured")
	}

	// Should have homeport network
	hasHomeportNetwork := false
	for _, network := range svc.Networks {
		if network == "homeport" {
			hasHomeportNetwork = true
			break
		}
	}

	if !hasHomeportNetwork {
		t.Error("Expected 'homeport' network to be configured")
	}
}

// containsString checks if s contains substr
func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
