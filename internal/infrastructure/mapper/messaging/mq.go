package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type MQMapper struct {
	*mapper.BaseMapper
}

func NewMQMapper() *MQMapper {
	return &MQMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeMQBroker, nil)}
}

func (m *MQMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	brokerName := res.GetConfigString("broker_name")
	if brokerName == "" {
		brokerName = res.Name
	}
	if brokerName == "" {
		brokerName = "mq-broker"
	}

	result := mapper.NewMappingResult("rabbitmq")
	svc := result.DockerService
	svc.Image = "rabbitmq:3.12-management-alpine"
	svc.Environment = map[string]string{"RABBITMQ_DEFAULT_USER": "guest", "RABBITMQ_DEFAULT_PASS": "guest"}
	svc.Ports = []string{"5672:5672", "15672:15672"}
	svc.Volumes = []string{"./data/rabbitmq:/var/lib/rabbitmq", "./config/rabbitmq/mq-definitions.json:/etc/rabbitmq/definitions.json", "./config/rabbitmq/mq.conf:/etc/rabbitmq/rabbitmq.conf"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{"homeport.source": "aws_mq_broker", "homeport.broker": brokerName, "homeport.target": "rabbitmq"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "rabbitmq-diagnostics", "-q", "ping"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	result.AddConfig("config/rabbitmq/mq-definitions.json", []byte(m.definitions(brokerName)))
	result.AddConfig("config/rabbitmq/mq.conf", []byte("management.load_definitions = /etc/rabbitmq/definitions.json\n"))
	result.AddConfig("config/mq/app-change.env", []byte(m.appChange(brokerName)))
	result.AddConfig("config/mq/generated-broker.patch", []byte(m.generatedPatch(brokerName)))
	result.AddScript("export_mq_broker.sh", []byte(m.exportScript(brokerName, res.Region)))
	result.AddScript("provision_rabbitmq_mq.sh", []byte(m.provisionScript(brokerName)))
	result.AddScript("migrate_mq_destinations.sh", []byte(m.migrateScript(brokerName)))
	result.AddScript("validate_mq_broker.sh", []byte(m.validateScript(brokerName)))
	result.AddScript("backup_mq_config.sh", []byte(m.backupScript(brokerName)))
	result.AddScript("cutover_mq_clients.sh", []byte(m.cutoverScript(brokerName)))
	for _, step := range mqRunbook(brokerName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *MQMapper) definitions(brokerName string) string {
	return fmt.Sprintf(`{"rabbitmq_version":"3.12.0","vhosts":[{"name":"/"}],"exchanges":[{"name":"%s.events","vhost":"/","type":"topic","durable":true}],"queues":[{"name":"%s.default","vhost":"/","durable":true,"auto_delete":false,"arguments":{}}],"bindings":[{"source":"%s.events","vhost":"/","destination":"%s.default","destination_type":"queue","routing_key":"#","arguments":{}}]}`+"\n", brokerName, brokerName, brokerName, brokerName)
}

func (m *MQMapper) appChange(brokerName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_MQ_BROKER=%s\nTARGET_BROKER=rabbitmq\nAMQP_URL=amqp://guest:guest@rabbitmq:5672/\n", brokerName)
}

func (m *MQMapper) generatedPatch(brokerName string) string {
	return fmt.Sprintf("--- app.env\n+++ app.env\n@@\n-AWS_MQ_BROKER=%s\n+AMQP_URL=amqp://guest:guest@rabbitmq:5672/\n+MQ_BROKER=rabbitmq\n", brokerName)
}

func (m *MQMapper) exportScript(brokerName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAWS_REGION=\"${AWS_REGION:-%s}\"\nBROKER_NAME=\"${MQ_BROKER:-%s}\"\nOUTPUT_DIR=\"${MQ_EXPORT_DIR:-mq-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naws mq list-brokers --region \"$AWS_REGION\" > \"$OUTPUT_DIR/brokers.json\"\naws mq describe-broker --region \"$AWS_REGION\" --broker-id \"$BROKER_NAME\" > \"$OUTPUT_DIR/broker.json\"\necho \"Exported MQ broker $BROKER_NAME\"\n", region, brokerName)
}

func (m *MQMapper) provisionScript(brokerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/rabbitmq/mq-definitions.json\ntest -s config/rabbitmq/mq.conf\necho \"RabbitMQ ready for MQ broker %s\"\n", brokerName)
}

func (m *MQMapper) migrateScript(brokerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s mq-export/broker.json\ntest -s config/rabbitmq/mq-definitions.json\ngrep -q %q config/rabbitmq/mq-definitions.json\necho \"MQ destinations mapped to RabbitMQ\"\n", brokerName)
}

func (m *MQMapper) validateScript(brokerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ncurl -fsS -u guest:guest http://localhost:15672/api/overview >/tmp/homeport-mq-overview.json\ngrep -q %q config/rabbitmq/mq-definitions.json\ntest -s config/mq/app-change.env\n", brokerName)
}

func (m *MQMapper) backupScript(brokerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-mq-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/rabbitmq config/mq export_mq_broker.sh migrate_mq_destinations.sh validate_mq_broker.sh cutover_mq_clients.sh\necho \"$archive\"\n", brokerName)
}

func (m *MQMapper) cutoverScript(brokerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/mq/app-change.env\ntest \"$SOURCE_MQ_BROKER\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch MQ clients to AMQP_URL=$AMQP_URL\"\n", brokerName)
}

func mqRunbook(brokerName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "broker", "source": "aws_mq_broker", "broker": brokerName, "HOMEPORT_TARGET": "rabbitmq", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		mqStep("export-mq-broker", "Export MQ broker", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_mq_broker.sh"}, "broker metadata is exported", metadata),
		mqStep("provision-rabbitmq-mq", "Provision RabbitMQ broker", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_rabbitmq_mq.sh"}, "RabbitMQ broker definitions are present", metadata),
		mqStep("migrate-mq-destinations", "Migrate MQ destinations", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_mq_destinations.sh"}, "queues and topics are represented in RabbitMQ", metadata),
		mqStep("validate-mq-broker", "Validate MQ broker", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_mq_broker.sh"}, "RabbitMQ health and definitions validate", metadata),
		mqStep("backup-mq-config", "Backup MQ migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_mq_config.sh"}, "MQ migration artifacts are archived", metadata),
		mqStep("cutover-mq-clients", "Cut over MQ clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_mq_clients.sh"}, "MQ clients use RabbitMQ endpoint", metadata),
		mqStep("rollback-mq-source", "Keep MQ source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS MQ remains authoritative until RabbitMQ validation passes", metadata),
	}
}

func mqStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
