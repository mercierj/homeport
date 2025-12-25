package compute

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewEC2Mapper(t *testing.T) {
	m := NewEC2Mapper()
	if m == nil {
		t.Fatal("NewEC2Mapper() returned nil")
	}
	if m.ResourceType() != resource.TypeEC2Instance {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeEC2Instance)
	}
}

func TestEC2Mapper_ResourceType(t *testing.T) {
	m := NewEC2Mapper()
	got := m.ResourceType()
	want := resource.TypeEC2Instance

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestEC2Mapper_Dependencies(t *testing.T) {
	m := NewEC2Mapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestEC2Mapper_Validate(t *testing.T) {
	m := NewEC2Mapper()

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
				Type: resource.TypeEC2Instance,
				Name: "test-instance",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeEC2Instance,
				Name: "test-instance",
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

func TestEC2Mapper_Map(t *testing.T) {
	m := NewEC2Mapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic EC2 instance",
			res: &resource.AWSResource{
				ID:   "i-1234567890",
				Type: resource.TypeEC2Instance,
				Name: "web-server",
				Config: map[string]interface{}{
					"instance_type": "t3.medium",
					"ami":           "ami-12345",
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
				if result.DockerService.Environment == nil {
					t.Error("DockerService.Environment is nil")
				}
				if _, ok := result.DockerService.Environment["INSTANCE_NAME"]; !ok {
					t.Error("INSTANCE_NAME not set in environment")
				}
				if _, ok := result.DockerService.Environment["INSTANCE_TYPE"]; !ok {
					t.Error("INSTANCE_TYPE not set in environment")
				}
			},
		},
		{
			name: "EC2 with user data",
			res: &resource.AWSResource{
				ID:   "i-1234567890",
				Type: resource.TypeEC2Instance,
				Name: "web-server",
				Config: map[string]interface{}{
					"instance_type": "t3.medium",
					"ami":           "ami-12345",
					"user_data":     "#!/bin/bash\necho 'Hello World'",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if len(result.Configs) == 0 {
					t.Error("Expected Dockerfile config but got none")
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
