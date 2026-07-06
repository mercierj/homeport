package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestSQSConformanceManagedAToZ(t *testing.T) {
	result, err := NewSQSMapper().Map(context.Background(), managedSQSFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated SQS migration", result.ManualSteps)
	}
	if result.DockerService.Image != "rabbitmq:3.12-management-alpine" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 3 {
		t.Fatalf("service does not provision HA RabbitMQ target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/rabbitmq/definitions.json", "config/rabbitmq/rabbitmq.conf", "config/rabbitmq/app-change.env", "config/rabbitmq/fifo-policy.json", "config/rabbitmq/dlq-policy.json", "config/rabbitmq/delay-policy.json"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	definitions := string(result.Configs["config/rabbitmq/definitions.json"])
	for _, want := range []string{"orders.fifo", "orders.fifo-dlq", "x-single-active-consumer", "x-dead-letter-exchange"} {
		if !strings.Contains(definitions, want) {
			t.Fatalf("definitions missing %q:\n%s", want, definitions)
		}
	}
	appEnv := string(result.Configs["config/rabbitmq/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=adapter", "SOURCE_QUEUE=orders.fifo", "AWS_ENDPOINT_URL_SQS=http://homeport:8080/api/v1/compat/aws/sqs", "HOMEPORT_COMPAT_BACKEND=rabbitmq"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_rabbitmq.sh", "export_sqs_queue.sh", "migrate_sqs_messages.sh", "validate_sqs_adapter.sh", "backup_sqs_config.sh", "cutover_sqs_adapter.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-sqs-queue":         domainrunbook.StepTypeCommand,
		"provision-rabbitmq-queue": domainrunbook.StepTypeCommand,
		"migrate-sqs-messages":     domainrunbook.StepTypeCommand,
		"validate-sqs-adapter":     domainrunbook.StepTypeCommand,
		"backup-sqs-config":        domainrunbook.StepTypeCommand,
		"cutover-sqs-clients":      domainrunbook.StepTypeAPICall,
		"rollback-sqs-source":      domainrunbook.StepTypeRollback,
	} {
		if !hasSQSRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewSQSMapper(t *testing.T) {
	m := NewSQSMapper()
	if m == nil {
		t.Fatal("NewSQSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSQSQueue {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSQSQueue)
	}
}

func managedSQSFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "arn:aws:sqs:eu-west-1:123456789012:orders.fifo",
		Type:   resource.TypeSQSQueue,
		Name:   "orders.fifo",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name":                       "orders.fifo",
			"fifo_queue":                 true,
			"redrive_policy":             `{"deadLetterTargetArn":"arn:aws:sqs:eu-west-1:123456789012:orders-dlq","maxReceiveCount":5}`,
			"visibility_timeout_seconds": float64(45),
			"message_retention_seconds":  float64(1209600),
			"delay_seconds":              float64(5),
		},
	}
}

func hasSQSRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
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
