package compute

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewLambdaMapper(t *testing.T) {
	m := NewLambdaMapper()
	if m == nil {
		t.Fatal("NewLambdaMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeLambdaFunction {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeLambdaFunction)
	}
}

func TestLambdaMapper_ResourceType(t *testing.T) {
	m := NewLambdaMapper()
	got := m.ResourceType()
	want := resource.TypeLambdaFunction

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestLambdaMapper_Dependencies(t *testing.T) {
	m := NewLambdaMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestLambdaMapper_Validate(t *testing.T) {
	m := NewLambdaMapper()

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
				Type: resource.TypeLambdaFunction,
				Name: "test-lambda",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeLambdaFunction,
				Name: "test-lambda",
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

func TestLambdaMapper_Map(t *testing.T) {
	m := NewLambdaMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Lambda function - Node.js",
			res: &resource.AWSResource{
				ID:   "arn:aws:lambda:us-east-1:123456789012:function:my-function",
				Type: resource.TypeLambdaFunction,
				Name: "my-function",
				Config: map[string]interface{}{
					"function_name": "my-function",
					"runtime":       "nodejs18.x",
					"handler":       "index.handler",
					"memory_size":   float64(256),
					"timeout":       float64(30),
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
			name: "Lambda function - Python",
			res: &resource.AWSResource{
				ID:   "arn:aws:lambda:us-east-1:123456789012:function:py-function",
				Type: resource.TypeLambdaFunction,
				Name: "py-function",
				Config: map[string]interface{}{
					"function_name": "py-function",
					"runtime":       "python3.11",
					"handler":       "main.handler",
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
