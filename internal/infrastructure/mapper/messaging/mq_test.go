package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestMQConformanceManagedAToZ(t *testing.T) {
	result, err := NewMQMapper().Map(context.Background(), managedMQFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated MQ migration", result.ManualSteps)
	}
	if result.DockerService.Image != "rabbitmq:3.12-management-alpine" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA RabbitMQ target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/rabbitmq/mq-definitions.json", "config/rabbitmq/mq.conf", "config/mq/app-change.env", "config/mq/generated-broker.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/mq/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_MQ_BROKER=orders-broker", "TARGET_BROKER=rabbitmq", "AMQP_URL=amqp://guest:guest@rabbitmq:5672/"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_mq_broker.sh", "provision_rabbitmq_mq.sh", "migrate_mq_destinations.sh", "validate_mq_broker.sh", "backup_mq_config.sh", "cutover_mq_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-mq-broker":        domainrunbook.StepTypeCommand,
		"provision-rabbitmq-mq":   domainrunbook.StepTypeCommand,
		"migrate-mq-destinations": domainrunbook.StepTypeCommand,
		"validate-mq-broker":      domainrunbook.StepTypeCommand,
		"backup-mq-config":        domainrunbook.StepTypeCommand,
		"cutover-mq-clients":      domainrunbook.StepTypeAPICall,
		"rollback-mq-source":      domainrunbook.StepTypeRollback,
	} {
		if !hasMQRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewMQMapper(t *testing.T) {
	m := NewMQMapper()
	if m == nil {
		t.Fatal("NewMQMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeMQBroker {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeMQBroker)
	}
}

func managedMQFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "orders-broker",
		Type:   resource.TypeMQBroker,
		Name:   "orders-broker",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"broker_name": "orders-broker",
			"engine_type": "RABBITMQ",
		},
	}
}

func hasMQRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
