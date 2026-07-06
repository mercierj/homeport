package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type StepFunctionsMapper struct {
	*mapper.BaseMapper
}

func NewStepFunctionsMapper() *StepFunctionsMapper {
	return &StepFunctionsMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeStepFunctionsStateMachine, nil)}
}

func (m *StepFunctionsMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	definition := res.GetConfigString("definition")
	if definition == "" {
		definition = `{"StartAt":"Start","States":{"Start":{"Type":"Pass","End":true}}}`
	}

	result := mapper.NewMappingResult("temporal")
	svc := result.DockerService
	svc.Image = "temporalio/auto-setup:1.23"
	svc.Ports = []string{"7233:7233", "8233:8233"}
	svc.Volumes = []string{"./data/temporal:/var/lib/temporal", "./config/stepfunctions:/etc/homeport/stepfunctions"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":                    "aws_sfn_state_machine",
		"homeport.state_machine":             name,
		"homeport.target":                    "temporal",
		"traefik.enable":                     "true",
		"traefik.http.routers.temporal.rule": "Host(`temporal.localhost`)",
		"traefik.http.services.temporal.loadbalancer.server.port": "8233",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "temporal", "operator", "cluster", "health"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/stepfunctions/asl.json", []byte(definition))
	result.AddConfig("config/stepfunctions/workflow-map.yaml", []byte(m.workflowMap(name)))
	result.AddConfig("config/stepfunctions/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/stepfunctions/generated-client.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_stepfunctions_state_machine.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_temporal_namespace.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_stepfunctions_workflow.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_temporal_workflow.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_stepfunctions_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_stepfunctions_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range stepFunctionsRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *StepFunctionsMapper) workflowMap(name string) string {
	return fmt.Sprintf(`state_machine: %s
target: temporal
namespace: default
task_queue: %s-task-queue
definition: config/stepfunctions/asl.json
worker_scaffold: config/stepfunctions/generated-client.patch
`, name, name)
}

func (m *StepFunctionsMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_STATE_MACHINE=%s
TARGET_WORKFLOW_ENGINE=temporal
TEMPORAL_ADDRESS=temporal:7233
TEMPORAL_NAMESPACE=default
TEMPORAL_TASK_QUEUE=%s-task-queue
GENERATED_PATCH=config/stepfunctions/generated-client.patch
`, name, name)
}

func (m *StepFunctionsMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/app/workflow_client.env
+++ b/app/workflow_client.env
@@
-STEPFUNCTIONS_STATE_MACHINE=%s
+TEMPORAL_ADDRESS=temporal:7233
+TEMPORAL_NAMESPACE=default
+TEMPORAL_TASK_QUEUE=%s-task-queue
+WORKFLOW_ENGINE=temporal
`, name, name)
}

func (m *StepFunctionsMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
STATE_MACHINE_NAME=%q
OUTPUT_DIR="./stepfunctions-export"
mkdir -p "$OUTPUT_DIR"
aws stepfunctions list-state-machines --region "$AWS_REGION" --output json > "$OUTPUT_DIR/state-machines.json"
state_machine_arn=$(jq -r --arg name "$STATE_MACHINE_NAME" '.stateMachines[] | select(.name == $name) | .stateMachineArn' "$OUTPUT_DIR/state-machines.json")
test -n "$state_machine_arn"
aws stepfunctions describe-state-machine --state-machine-arn "$state_machine_arn" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/state-machine.json"
jq -r '.definition' "$OUTPUT_DIR/state-machine.json" > config/stepfunctions/asl.json
`, region, name)
}

func (m *StepFunctionsMapper) provisionScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
temporal operator namespace describe default >/tmp/homeport-temporal-namespace.txt || temporal operator namespace create default
test -s config/stepfunctions/workflow-map.yaml
echo "Temporal namespace ready for %s"
`, name)
}

func (m *StepFunctionsMapper) migrateScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/stepfunctions/asl.json
test -s config/stepfunctions/workflow-map.yaml
jq -e '.StartAt and .States' config/stepfunctions/asl.json >/dev/null
echo "Step Functions state machine %s mapped to Temporal workflow"
`, name)
}

func (m *StepFunctionsMapper) validateScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
temporal operator cluster health >/tmp/homeport-temporal-health.txt
test -s config/stepfunctions/app-change.env
grep -q %q config/stepfunctions/workflow-map.yaml
`, name)
}

func (m *StepFunctionsMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-temporal-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/stepfunctions export_stepfunctions_state_machine.sh provision_temporal_namespace.sh migrate_stepfunctions_workflow.sh validate_temporal_workflow.sh cutover_stepfunctions_clients.sh
echo "$archive"
`, name)
}

func (m *StepFunctionsMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/stepfunctions/app-change.env
test "$SOURCE_STATE_MACHINE" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route workflow starts to Temporal"
`, name)
}

func stepFunctionsRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "workflow",
		"source":              "aws_sfn_state_machine",
		"state_machine":       name,
		"HOMEPORT_TARGET":     "temporal",
		"HOMEPORT_APP_CHANGE": "generated_patch",
		"TEMPORAL_ADDRESS":    "temporal:7233",
	}
	return []domainrunbook.Step{
		stepFunctionsStep("export-stepfunctions-state-machine", "Export Step Functions state machine", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_stepfunctions_state_machine.sh"}, "ASL definition and state machine metadata are exported", metadata),
		stepFunctionsStep("provision-temporal-namespace", "Provision Temporal namespace", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_temporal_namespace.sh"}, "Temporal namespace and task queue config are ready", metadata),
		stepFunctionsStep("migrate-stepfunctions-workflow", "Migrate ASL workflow", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_stepfunctions_workflow.sh"}, "ASL workflow is mapped to Temporal worker scaffold", metadata),
		stepFunctionsStep("validate-temporal-workflow", "Validate Temporal workflow target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_temporal_workflow.sh"}, "Temporal health and workflow mapping validate", metadata),
		stepFunctionsStep("backup-stepfunctions-config", "Backup Step Functions migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_stepfunctions_config.sh"}, "workflow migration artifacts are archived", metadata),
		stepFunctionsStep("cutover-stepfunctions-clients", "Cut over Step Functions clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_stepfunctions_clients.sh"}, "clients use generated Temporal patch", metadata),
		stepFunctionsStep("rollback-stepfunctions-source", "Keep Step Functions source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Step Functions remains authoritative until Temporal workflow validation passes", metadata),
	}
}

func stepFunctionsStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}
