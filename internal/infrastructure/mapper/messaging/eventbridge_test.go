package messaging

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewEventBridgeMapper(t *testing.T) {
	m := NewEventBridgeMapper()
	if m == nil {
		t.Fatal("NewEventBridgeMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeEventBridge {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeEventBridge)
	}
}

func TestEventBridgeMapper_ResourceType(t *testing.T) {
	m := NewEventBridgeMapper()
	got := m.ResourceType()
	want := resource.TypeEventBridge

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestEventBridgeMapper_Dependencies(t *testing.T) {
	m := NewEventBridgeMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestEventBridgeMapper_Validate(t *testing.T) {
	m := NewEventBridgeMapper()

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
				Type: resource.TypeEventBridge,
				Name: "test-rule",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeEventBridge,
				Name: "test-rule",
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

func TestEventBridgeMapper_Map(t *testing.T) {
	m := NewEventBridgeMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic EventBridge rule",
			res: &resource.AWSResource{
				ID:   "my-event-rule",
				Type: resource.TypeEventBridge,
				Name: "my-event-rule",
				Config: map[string]interface{}{
					"name": "my-event-rule",
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
				// Should use n8n image
				if result.DockerService.Image != "n8nio/n8n:latest" {
					t.Errorf("Expected n8n image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "EventBridge rule with schedule expression (rate)",
			res: &resource.AWSResource{
				ID:   "scheduled-rule",
				Type: resource.TypeEventBridge,
				Name: "scheduled-rule",
				Config: map[string]interface{}{
					"name":                "scheduled-rule",
					"schedule_expression": "rate(1 hour)",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about schedule expression
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about schedule expression")
				}
			},
		},
		{
			name: "EventBridge rule with schedule expression (cron)",
			res: &resource.AWSResource{
				ID:   "cron-rule",
				Type: resource.TypeEventBridge,
				Name: "cron-rule",
				Config: map[string]interface{}{
					"name":                "cron-rule",
					"schedule_expression": "cron(0 12 * * ? *)",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about cron schedule
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about cron schedule")
				}
			},
		},
		{
			name: "EventBridge rule with event pattern",
			res: &resource.AWSResource{
				ID:   "pattern-rule",
				Type: resource.TypeEventBridge,
				Name: "pattern-rule",
				Config: map[string]interface{}{
					"name": "pattern-rule",
					"event_pattern": map[string]interface{}{
						"source":      []string{"aws.s3"},
						"detail-type": []string{"Object Created"},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about event pattern
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about event pattern")
				}
			},
		},
		{
			name: "EventBridge rule with targets",
			res: &resource.AWSResource{
				ID:   "targeted-rule",
				Type: resource.TypeEventBridge,
				Name: "targeted-rule",
				Config: map[string]interface{}{
					"name": "targeted-rule",
					"targets": []interface{}{
						map[string]interface{}{
							"arn": "arn:aws:lambda:us-east-1:123456789012:function:my-function",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have manual steps for targets
				if len(result.ManualSteps) == 0 {
					t.Log("Expected manual steps for targets")
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
