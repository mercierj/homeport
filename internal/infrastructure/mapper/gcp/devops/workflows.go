package devops

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type WorkflowsMapper struct {
	*mapper.BaseMapper
}

func NewWorkflowsMapper() *WorkflowsMapper {
	return &WorkflowsMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeWorkflowsWorkflow, nil)}
}

func (m *WorkflowsMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmpty(res.GetConfigString("name"), res.Name)
	if name == "" {
		name = "workflow"
	}
	source := firstNonEmpty(res.GetConfigString("source_contents"), "main:\n  steps:\n    - done:\n        return: ok\n")

	result := mapper.NewMappingResult("temporal")
	svc := result.DockerService
	svc.Image = "temporalio/auto-setup:1.23"
	svc.Ports = []string{"7233:7233", "8233:8233"}
	svc.Volumes = []string{"./data/temporal:/var/lib/temporal", "./config/workflows:/etc/homeport/workflows"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "temporal", "operator", "cluster", "health"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeWorkflowsWorkflow), "homeport.workflow": name, "homeport.target": "temporal"}

	result.AddConfig("config/workflows/source.yaml", []byte(source))
	result.AddConfig("config/workflows/workflow-map.yaml", []byte(m.workflowMap(name)))
	result.AddConfig("config/workflows/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/workflows/generated-client.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_gcp_workflow.sh", []byte(m.exportScript(name)))
	result.AddScript("provision_temporal_workflow_namespace.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_gcp_workflow.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_temporal_gcp_workflow.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_gcp_workflow_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_gcp_workflow_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range workflowsRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *WorkflowsMapper) workflowMap(name string) string {
	return fmt.Sprintf(`workflow: %s
source: google_workflows_workflow
target: temporal
namespace: default
task_queue: %s-task-queue
definition: config/workflows/source.yaml
worker_scaffold: config/workflows/generated-client.patch
`, name, sanitizeDevOpsName(name))
}

func (m *WorkflowsMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_GCP_WORKFLOW=%s
TARGET_WORKFLOW_ENGINE=temporal
TEMPORAL_ADDRESS=temporal:7233
TEMPORAL_NAMESPACE=default
TEMPORAL_TASK_QUEUE=%s-task-queue
GENERATED_PATCH=config/workflows/generated-client.patch
`, name, sanitizeDevOpsName(name))
}

func (m *WorkflowsMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/app/workflow_client.env
+++ b/app/workflow_client.env
@@
-GCP_WORKFLOW=%s
+TEMPORAL_ADDRESS=temporal:7233
+TEMPORAL_NAMESPACE=default
+TEMPORAL_TASK_QUEUE=%s-task-queue
+WORKFLOW_ENGINE=temporal
`, name, sanitizeDevOpsName(name))
}

func (m *WorkflowsMapper) exportScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
WORKFLOW_NAME=%q
OUTPUT_DIR="./workflows-export"
mkdir -p "$OUTPUT_DIR" config/workflows
gcloud workflows describe "$WORKFLOW_NAME" --format=json > "$OUTPUT_DIR/workflow.json"
gcloud workflows describe "$WORKFLOW_NAME" --format='value(sourceContents)' > config/workflows/source.yaml
`, name)
}

func (m *WorkflowsMapper) provisionScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
temporal operator namespace describe default >/tmp/homeport-temporal-namespace.txt || temporal operator namespace create default
test -s config/workflows/workflow-map.yaml
echo "Temporal namespace ready for GCP workflow %s"
`, name)
}

func (m *WorkflowsMapper) migrateScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/workflows/source.yaml
test -s config/workflows/workflow-map.yaml
echo "GCP workflow %s mapped to Temporal workflow scaffold"
`, name)
}

func (m *WorkflowsMapper) validateScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
temporal operator cluster health >/tmp/homeport-temporal-health.txt
test -s config/workflows/app-change.env
grep -q %q config/workflows/workflow-map.yaml
`, name)
}

func (m *WorkflowsMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/gcp-workflow-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/workflows workflows-export export_gcp_workflow.sh provision_temporal_workflow_namespace.sh migrate_gcp_workflow.sh validate_temporal_gcp_workflow.sh cutover_gcp_workflow_clients.sh
echo "$archive"
`, sanitizeDevOpsName(name))
}

func (m *WorkflowsMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/workflows/app-change.env
test "$SOURCE_GCP_WORKFLOW" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route workflow starts to Temporal"
`, name)
}

func workflowsRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "workflow", "source": "google_workflows_workflow", "workflow": name, "target": "temporal", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		workflowsStep("export-gcp-workflow", "Export GCP workflow", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_gcp_workflow.sh"}, "workflow source and metadata are exported", metadata),
		workflowsStep("provision-temporal-workflow", "Provision Temporal workflow", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_temporal_workflow_namespace.sh"}, "Temporal namespace and task queue config are ready", metadata),
		workflowsStep("migrate-gcp-workflow", "Migrate GCP workflow", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_gcp_workflow.sh"}, "workflow YAML is mapped to Temporal worker scaffold", metadata),
		workflowsStep("validate-temporal-gcp-workflow", "Validate Temporal workflow", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_temporal_gcp_workflow.sh"}, "Temporal health and workflow mapping validate", metadata),
		workflowsStep("backup-gcp-workflow-config", "Backup GCP workflow config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_gcp_workflow_config.sh"}, "workflow migration artifacts are archived", metadata),
		workflowsStep("cutover-gcp-workflow-clients", "Cut over GCP workflow clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_gcp_workflow_clients.sh"}, "clients use generated Temporal patch", metadata),
		workflowsStep("rollback-gcp-workflow-authority", "Keep GCP workflow authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "GCP Workflows remains authoritative until Temporal validation passes", metadata),
	}
}

func workflowsStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
