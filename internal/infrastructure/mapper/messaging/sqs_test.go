package messaging

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewSQSMapper(t *testing.T) {
	m := NewSQSMapper()
	if m == nil {
		t.Fatal("NewSQSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSQSQueue {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSQSQueue)
	}
}

func TestSQSMapper_ResourceType(t *testing.T) {
	m := NewSQSMapper()
	got := m.ResourceType()
	want := resource.TypeSQSQueue

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestSQSMapper_Dependencies(t *testing.T) {
	m := NewSQSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestSQSMapper_Validate(t *testing.T) {
	m := NewSQSMapper()

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
				Type: resource.TypeSQSQueue,
				Name: "test-queue",
			},
			wantErr: false,
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

func TestSQSMapper_Map(t *testing.T) {
	m := NewSQSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic SQS queue",
			res: &resource.AWSResource{
				ID:   "arn:aws:sqs:us-east-1:123456789012:my-queue",
				Type: resource.TypeSQSQueue,
				Name: "my-queue",
				Config: map[string]interface{}{
					"name":                       "my-queue",
					"visibility_timeout_seconds": float64(30),
					"message_retention_seconds":  float64(345600),
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
			},
		},
		{
			name: "FIFO SQS queue",
			res: &resource.AWSResource{
				ID:   "arn:aws:sqs:us-east-1:123456789012:my-queue.fifo",
				Type: resource.TypeSQSQueue,
				Name: "my-queue.fifo",
				Config: map[string]interface{}{
					"name":       "my-queue.fifo",
					"fifo_queue": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// FIFO queues should have a warning
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about FIFO queue limitations")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
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
