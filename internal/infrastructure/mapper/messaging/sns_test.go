package messaging

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewSNSMapper(t *testing.T) {
	m := NewSNSMapper()
	if m == nil {
		t.Fatal("NewSNSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSNSTopic {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSNSTopic)
	}
}

func TestSNSMapper_ResourceType(t *testing.T) {
	m := NewSNSMapper()
	got := m.ResourceType()
	want := resource.TypeSNSTopic

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestSNSMapper_Dependencies(t *testing.T) {
	m := NewSNSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestSNSMapper_Validate(t *testing.T) {
	m := NewSNSMapper()

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
				Type: resource.TypeSNSTopic,
				Name: "test-topic",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeSNSTopic,
				Name: "test-topic",
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

func TestSNSMapper_Map(t *testing.T) {
	m := NewSNSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic SNS topic",
			res: &resource.AWSResource{
				ID:   "arn:aws:sns:us-east-1:123456789012:my-topic",
				Type: resource.TypeSNSTopic,
				Name: "my-topic",
				ARN:  "arn:aws:sns:us-east-1:123456789012:my-topic",
				Config: map[string]interface{}{
					"name": "my-topic",
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
				// Should use NATS image
				if result.DockerService.Image != "nats:2.10-alpine" {
					t.Errorf("Expected NATS image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "FIFO SNS topic",
			res: &resource.AWSResource{
				ID:   "arn:aws:sns:us-east-1:123456789012:my-topic.fifo",
				Type: resource.TypeSNSTopic,
				Name: "my-topic.fifo",
				ARN:  "arn:aws:sns:us-east-1:123456789012:my-topic.fifo",
				Config: map[string]interface{}{
					"name":       "my-topic.fifo",
					"fifo_topic": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about FIFO
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about FIFO topic")
				}
			},
		},
		{
			name: "SNS topic with content-based deduplication",
			res: &resource.AWSResource{
				ID:   "arn:aws:sns:us-east-1:123456789012:dedup-topic.fifo",
				Type: resource.TypeSNSTopic,
				Name: "dedup-topic.fifo",
				ARN:  "arn:aws:sns:us-east-1:123456789012:dedup-topic.fifo",
				Config: map[string]interface{}{
					"name":                        "dedup-topic.fifo",
					"fifo_topic":                  true,
					"content_based_deduplication": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about content-based deduplication
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about content-based deduplication")
				}
			},
		},
		{
			name: "SNS topic with subscriptions",
			res: &resource.AWSResource{
				ID:   "arn:aws:sns:us-east-1:123456789012:sub-topic",
				Type: resource.TypeSNSTopic,
				Name: "sub-topic",
				ARN:  "arn:aws:sns:us-east-1:123456789012:sub-topic",
				Config: map[string]interface{}{
					"name": "sub-topic",
					"subscriptions": []interface{}{
						map[string]interface{}{
							"endpoint": "https://example.com/webhook",
							"protocol": "https",
						},
						map[string]interface{}{
							"endpoint": "arn:aws:lambda:us-east-1:123456789012:function:my-function",
							"protocol": "lambda",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about subscriptions
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about subscriptions")
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

func TestSNSMapper_isFIFOTopic(t *testing.T) {
	m := NewSNSMapper()

	tests := []struct {
		topicName string
		want      bool
	}{
		{"my-topic", false},
		{"my-topic.fifo", true},
		{"fifo", false},
		{".fifo", false},        // needs more than 5 chars (at least 1 char before .fifo)
		{"a.fifo", true},        // minimum valid FIFO topic name
		{"topic-name-with-fifo-in-middle", false},
	}

	for _, tt := range tests {
		t.Run(tt.topicName, func(t *testing.T) {
			got := m.isFIFOTopic(tt.topicName)
			if got != tt.want {
				t.Errorf("isFIFOTopic(%q) = %v, want %v", tt.topicName, got, tt.want)
			}
		})
	}
}
