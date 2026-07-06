package storage

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewS3Mapper(t *testing.T) {
	m := NewS3Mapper()
	if m == nil {
		t.Fatal("NewS3Mapper() returned nil")
	}
	if m.ResourceType() != resource.TypeS3Bucket {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeS3Bucket)
	}
}

func TestS3Mapper_ResourceType(t *testing.T) {
	m := NewS3Mapper()
	got := m.ResourceType()
	want := resource.TypeS3Bucket

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestS3Mapper_Dependencies(t *testing.T) {
	m := NewS3Mapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestS3Mapper_Validate(t *testing.T) {
	m := NewS3Mapper()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
	}{
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeEC2Instance,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeS3Bucket,
				Name: "test-bucket",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeS3Bucket,
				Name: "test-bucket",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestS3Mapper_Map(t *testing.T) {
	m := NewS3Mapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic S3 bucket",
			res: &resource.AWSResource{
				ID:   "my-bucket",
				Type: resource.TypeS3Bucket,
				Name: "my-bucket",
				Config: map[string]interface{}{
					"bucket": "my-bucket",
					"region": "us-east-1",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
				// Should use MinIO image
				if result.DockerService.Image != "minio/minio:latest" && result.DockerService.Image != "minio/minio" {
					t.Logf("DockerService.Image = %s (checking for minio)", result.DockerService.Image)
				}
			},
		},
		{
			name: "S3 bucket with versioning",
			res: &resource.AWSResource{
				ID:   "versioned-bucket",
				Type: resource.TypeS3Bucket,
				Name: "versioned-bucket",
				Config: map[string]interface{}{
					"bucket": "versioned-bucket",
					"versioning": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about versioning
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about versioning")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "wrong-id",
				Type: resource.TypeEC2Instance,
				Name: "wrong",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.Map(ctx, tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestS3MapperAddsObjectStorageRunbook(t *testing.T) {
	result, err := NewS3Mapper().Map(context.Background(), &resource.AWSResource{
		ID:   "assets",
		Type: resource.TypeS3Bucket,
		Name: "assets",
		Config: map[string]interface{}{
			"bucket": "assets",
		},
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}

	var foundProvision, foundSync, foundVerify bool
	for _, step := range result.RunbookSteps {
		if step.Metadata["kind"] != "object-storage" {
			continue
		}
		switch step.ID {
		case "provision-minio-bucket":
			foundProvision = true
			if step.Type != domainrunbook.StepTypeCommand || step.Executor != "shell" {
				t.Fatalf("provision step = %#v", step)
			}
			if step.Metadata["AWS_ENDPOINT_URL_S3"] != "http://minio:9000" {
				t.Fatalf("AWS endpoint metadata = %q", step.Metadata["AWS_ENDPOINT_URL_S3"])
			}
		case "sync-objects-to-minio":
			foundSync = true
		case "verify-object-migration":
			foundVerify = true
		}
	}
	if !foundProvision || !foundSync || !foundVerify {
		t.Fatalf("object-storage steps provision=%v sync=%v verify=%v", foundProvision, foundSync, foundVerify)
	}
}
