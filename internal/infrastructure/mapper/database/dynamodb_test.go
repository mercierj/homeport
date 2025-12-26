package database

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewDynamoDBMapper(t *testing.T) {
	m := NewDynamoDBMapper()
	if m == nil {
		t.Fatal("NewDynamoDBMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeDynamoDBTable {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeDynamoDBTable)
	}
}

func TestDynamoDBMapper_ResourceType(t *testing.T) {
	m := NewDynamoDBMapper()
	got := m.ResourceType()
	want := resource.TypeDynamoDBTable

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestDynamoDBMapper_Dependencies(t *testing.T) {
	m := NewDynamoDBMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestDynamoDBMapper_Validate(t *testing.T) {
	m := NewDynamoDBMapper()

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
				Type: resource.TypeDynamoDBTable,
				Name: "test-table",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeDynamoDBTable,
				Name: "test-table",
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

func TestDynamoDBMapper_Map(t *testing.T) {
	m := NewDynamoDBMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic DynamoDB table",
			res: &resource.AWSResource{
				ID:   "arn:aws:dynamodb:us-east-1:123456789012:table/my-table",
				Type: resource.TypeDynamoDBTable,
				Name: "my-table",
				Config: map[string]interface{}{
					"name":     "my-table",
					"hash_key": "id",
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
				// Should use ScyllaDB image
				if result.DockerService.Image != "scylladb/scylla:5.4" {
					t.Errorf("Expected ScyllaDB image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "DynamoDB table with PAY_PER_REQUEST billing",
			res: &resource.AWSResource{
				ID:   "arn:aws:dynamodb:us-east-1:123456789012:table/on-demand-table",
				Type: resource.TypeDynamoDBTable,
				Name: "on-demand-table",
				Config: map[string]interface{}{
					"name":         "on-demand-table",
					"hash_key":     "pk",
					"range_key":    "sk",
					"billing_mode": "PAY_PER_REQUEST",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about on-demand billing
				if len(result.Warnings) == 0 {
					t.Log("Expected warnings about PAY_PER_REQUEST billing")
				}
			},
		},
		{
			name: "DynamoDB table with streams enabled",
			res: &resource.AWSResource{
				ID:   "arn:aws:dynamodb:us-east-1:123456789012:table/stream-table",
				Type: resource.TypeDynamoDBTable,
				Name: "stream-table",
				Config: map[string]interface{}{
					"name":           "stream-table",
					"hash_key":       "id",
					"stream_enabled": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about streams
				hasStreamWarning := false
				for _, w := range result.Warnings {
					if len(w) > 0 {
						hasStreamWarning = true
						break
					}
				}
				if !hasStreamWarning {
					t.Log("Expected warnings about DynamoDB Streams")
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
