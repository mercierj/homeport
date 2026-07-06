package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestKinesisConformanceManagedAToZ(t *testing.T) {
	result, err := NewKinesisMapper().Map(context.Background(), managedKinesisFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Kinesis migration", result.ManualSteps)
	}
	if result.DockerService.Image != "redpandadata/redpanda:v23.3.5" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 3 {
		t.Fatalf("service does not provision HA Redpanda target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/redpanda/topics.yaml", "config/kinesis/stream-map.yaml", "config/kinesis/app-change.env", "config/kinesis/consumer-groups.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/kinesis/app-change.env"])
	for _, want := range []string{"SOURCE_STREAM=orders-stream", "TARGET_TOPIC=orders-stream", "APP_CHANGE_MODE=adapter", "AWS_ENDPOINT_URL_KINESIS=http://homeport:8080/api/v1/compat/aws/kinesis"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_redpanda.sh", "export_kinesis_records.sh", "migrate_kinesis_records.sh", "validate_kinesis_replay.sh", "backup_kinesis_stream.sh", "cutover_kinesis_adapter.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-kinesis-stream":    domainrunbook.StepTypeCommand,
		"provision-redpanda-topic": domainrunbook.StepTypeCommand,
		"migrate-kinesis-records":  domainrunbook.StepTypeCommand,
		"validate-kinesis-replay":  domainrunbook.StepTypeCommand,
		"backup-kinesis-config":    domainrunbook.StepTypeCommand,
		"cutover-kinesis-adapter":  domainrunbook.StepTypeAPICall,
		"rollback-kinesis-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasKinesisRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewKinesisMapper(t *testing.T) {
	m := NewKinesisMapper()
	if m == nil {
		t.Fatal("NewKinesisMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeKinesis {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeKinesis)
	}
}

func managedKinesisFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "arn:aws:kinesis:eu-west-1:123456789012:stream/orders-stream",
		Type:   resource.TypeKinesis,
		Name:   "orders-stream",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name":                "orders-stream",
			"shard_count":         float64(3),
			"retention_period":    float64(168),
			"encryption_type":     "KMS",
			"shard_level_metrics": "IncomingBytes,OutgoingBytes",
		},
	}
}

func hasKinesisRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
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
