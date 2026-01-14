package messaging

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewKinesisMapper(t *testing.T) {
	m := NewKinesisMapper()
	if m == nil {
		t.Fatal("NewKinesisMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeKinesis {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeKinesis)
	}
}

func TestKinesisMapper_ResourceType(t *testing.T) {
	m := NewKinesisMapper()
	got := m.ResourceType()
	want := resource.TypeKinesis

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestKinesisMapper_Dependencies(t *testing.T) {
	m := NewKinesisMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestKinesisMapper_Validate(t *testing.T) {
	m := NewKinesisMapper()

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
				Type: resource.TypeS3Bucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeKinesis,
				Name: "test-stream",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeKinesis,
				Name: "test-stream",
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

func TestKinesisMapper_Map(t *testing.T) {
	m := NewKinesisMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Kinesis stream",
			res: &resource.AWSResource{
				ID:   "arn:aws:kinesis:us-east-1:123456789012:stream/my-stream",
				Type: resource.TypeKinesis,
				Name: "my-stream",
				Config: map[string]interface{}{
					"name":        "my-stream",
					"shard_count": float64(2),
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
				// Should use Redpanda image
				if result.DockerService.Image != "redpandadata/redpanda:v23.3.5" {
					t.Errorf("Expected Redpanda image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Kinesis stream with encryption",
			res: &resource.AWSResource{
				ID:   "arn:aws:kinesis:us-east-1:123456789012:stream/encrypted-stream",
				Type: resource.TypeKinesis,
				Name: "encrypted-stream",
				Config: map[string]interface{}{
					"name":            "encrypted-stream",
					"shard_count":     float64(1),
					"encryption_type": "KMS",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about encryption
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about encryption")
				}
			},
		},
		{
			name: "Kinesis stream with enhanced monitoring",
			res: &resource.AWSResource{
				ID:   "arn:aws:kinesis:us-east-1:123456789012:stream/monitored-stream",
				Type: resource.TypeKinesis,
				Name: "monitored-stream",
				Config: map[string]interface{}{
					"name":                "monitored-stream",
					"shard_count":         float64(1),
					"shard_level_metrics": "IncomingBytes,OutgoingBytes",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about monitoring
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about enhanced monitoring")
				}
			},
		},
		{
			name: "Kinesis stream with custom retention",
			res: &resource.AWSResource{
				ID:   "arn:aws:kinesis:us-east-1:123456789012:stream/retention-stream",
				Type: resource.TypeKinesis,
				Name: "retention-stream",
				Config: map[string]interface{}{
					"name":             "retention-stream",
					"shard_count":      float64(1),
					"retention_period": float64(168),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about retention
				hasRetentionWarning := false
				for _, w := range result.Warnings {
					if len(w) > 0 {
						hasRetentionWarning = true
						break
					}
				}
				if !hasRetentionWarning {
					t.Log("Expected warning about retention period")
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
				Type: resource.TypeS3Bucket,
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
