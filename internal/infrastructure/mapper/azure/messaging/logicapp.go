// Package messaging provides mappers for Azure messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// LogicAppMapper converts Azure Logic Apps to n8n workflows.
type LogicAppMapper struct {
	*mapper.BaseMapper
}

// NewLogicAppMapper creates a new Logic App to n8n mapper.
func NewLogicAppMapper() *LogicAppMapper {
	return &LogicAppMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeLogicApp, nil),
	}
}

// Map converts a Logic App to an n8n service.
func (m *LogicAppMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	workflowName := res.GetConfigString("name")
	if workflowName == "" {
		workflowName = res.Name
	}

	result := mapper.NewMappingResult("n8n")
	svc := result.DockerService

	svc.Image = "n8nio/n8n:latest"
	svc.Environment = map[string]string{
		"N8N_BASIC_AUTH_ACTIVE":   "true",
		"N8N_BASIC_AUTH_USER":     "admin",
		"N8N_BASIC_AUTH_PASSWORD": "admin",
		"N8N_HOST":                "localhost",
		"N8N_PORT":                "5678",
		"N8N_PROTOCOL":            "http",
		"WEBHOOK_URL":             "http://localhost:5678/",
		"GENERIC_TIMEZONE":        "UTC",
		"N8N_ENCRYPTION_KEY":      "changeme",
	}
	svc.Ports = []string{"5678:5678"}
	svc.Volumes = []string{
		"./data/n8n:/home/node/.n8n",
		"./config/n8n/workflows:/workflows",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":                                    "azurerm_logic_app_workflow",
		"homeport.workflow_name":                             workflowName,
		"traefik.enable":                                     "true",
		"traefik.http.routers.n8n.rule":                      "Host(`n8n.localhost`)",
		"traefik.http.services.n8n.loadbalancer.server.port": "5678",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:5678/healthz"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}

	// Handle workflow definition
	if workflowDef := res.Config["workflow_definition"]; workflowDef != nil {
		result.AddWarning("Workflow definition detected. Generated n8n workflow covers HTTP routing; validate connector parity.")
		defJSON, _ := json.MarshalIndent(workflowDef, "", "  ")
		result.AddConfig("config/n8n/logic_app_definition.json", defJSON)
	}

	// Handle access control
	if res.Config["access_control"] != nil {
		result.AddWarning("Access control configured. Generated client patch routes through n8n basic auth/webhook endpoint.")
	}

	// Handle integration account
	if res.GetConfigString("integration_account_id") != "" {
		result.AddWarning("Integration account linked. Generated workflow preserves routing handoff; connector credentials must be validated.")
	}

	workflowTemplate := m.generateWorkflowTemplate(workflowName)
	result.AddConfig("config/n8n/workflows/logicapp_workflow.json", []byte(workflowTemplate))
	result.AddConfig("config/logicapp/app-change.env", []byte(m.generateAppChange(workflowName)))
	result.AddConfig("config/logicapp/generated-client.patch", []byte(m.generateClientPatch(workflowName)))

	setupScript := m.generateSetupScript(workflowName)
	result.AddScript("setup_n8n_logicapp.sh", []byte(setupScript))
	result.AddScript("validate_logicapp_workflow.sh", []byte(m.generateValidateScript(workflowName)))
	result.AddScript("backup_logicapp_config.sh", []byte(m.generateBackupScript(workflowName)))
	result.AddScript("cutover_logicapp_clients.sh", []byte(m.generateCutoverScript(workflowName)))

	migrationGuide := m.generateMigrationGuide(workflowName)
	result.AddConfig("config/n8n/migration_guide.md", []byte(migrationGuide))
	for _, step := range logicAppRunbook(workflowName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *LogicAppMapper) generateWorkflowTemplate(workflowName string) string {
	workflow := map[string]interface{}{
		"name": fmt.Sprintf("Logic App - %s", workflowName),
		"nodes": []map[string]interface{}{
			{
				"name":        "HTTP Trigger",
				"type":        "n8n-nodes-base.webhook",
				"typeVersion": 1,
				"position":    []int{250, 300},
				"parameters": map[string]interface{}{
					"path":         workflowName,
					"httpMethod":   "POST",
					"responseMode": "lastNode",
					"responseData": "allEntries",
				},
			},
			{
				"name":        "Process Request",
				"type":        "n8n-nodes-base.code",
				"typeVersion": 1,
				"position":    []int{450, 300},
				"parameters": map[string]interface{}{
					"jsCode": `// Process incoming request
const input = items[0].json;
console.log('Processing:', input);

// Add your workflow logic here
// This replaces Logic App actions

return [{
  json: {
    status: 'success',
    processedAt: new Date().toISOString(),
    data: input
  }
}];`,
				},
			},
			{
				"name":        "HTTP Response",
				"type":        "n8n-nodes-base.respondToWebhook",
				"typeVersion": 1,
				"position":    []int{650, 300},
				"parameters": map[string]interface{}{
					"respondWith": "allIncomingItems",
				},
			},
		},
		"connections": map[string]interface{}{
			"HTTP Trigger": map[string]interface{}{
				"main": [][]map[string]interface{}{
					{{
						"node":  "Process Request",
						"type":  "main",
						"index": 0,
					}},
				},
			},
			"Process Request": map[string]interface{}{
				"main": [][]map[string]interface{}{
					{{
						"node":  "HTTP Response",
						"type":  "main",
						"index": 0,
					}},
				},
			},
		},
	}
	content, _ := json.MarshalIndent(workflow, "", "  ")
	return string(content)
}

func (m *LogicAppMapper) generateSetupScript(workflowName string) string {
	return fmt.Sprintf(`#!/bin/bash
# n8n Setup Script for Logic App: %s

set -e

N8N_HOST="${N8N_HOST:-localhost}"
N8N_PORT="${N8N_PORT:-5678}"

echo "Waiting for n8n to be ready..."
until curl -sf http://$N8N_HOST:$N8N_PORT/healthz > /dev/null 2>&1; do
  echo "Waiting..."
  sleep 5
done

echo "n8n is ready!"
echo "n8n UI: http://$N8N_HOST:$N8N_PORT"
echo "Credentials: admin/admin"
echo ""
echo "Workflow: %s"
echo "Webhook URL: http://$N8N_HOST:$N8N_PORT/webhook/%s"
`, workflowName, workflowName, workflowName)
}

func (m *LogicAppMapper) generateMigrationGuide(workflowName string) string {
	return fmt.Sprintf(`# Logic App to n8n Migration Guide

## Workflow: %s

### Action Mapping

| Logic App Action | n8n Equivalent |
|-----------------|----------------|
| HTTP | HTTP Request node |
| Compose | Set node / Code node |
| Parse JSON | Code node |
| Condition | IF node |
| For each | Split In Batches node |
| Switch | Switch node |
| Delay | Wait node |
| Send Email | Email node (Gmail, SMTP, etc.) |
| Create Blob | AWS S3 / MinIO node |

### Trigger Mapping

| Logic App Trigger | n8n Equivalent |
|------------------|----------------|
| HTTP Request | Webhook node |
| Recurrence | Schedule Trigger node |
| Blob Created | Webhook + MinIO events |

### Connector Mapping

Common Logic App connectors and their n8n equivalents:

1. **Office 365**: Use Microsoft nodes (Outlook, Teams, etc.)
2. **SQL Server**: Use Postgres/MySQL nodes or HTTP Request
3. **Salesforce**: Use Salesforce node
4. **SharePoint**: Use Microsoft SharePoint node
5. **Azure Functions**: Use HTTP Request node
6. **Service Bus**: Use RabbitMQ or Kafka nodes

### Steps to Migrate

1. Export Logic App definition (saved in config/n8n/logic_app_definition.json)
2. Create new workflow in n8n
3. Map triggers to n8n trigger nodes
4. Convert each action to equivalent n8n node
5. Configure credentials for external services
6. Test workflow with sample data
7. Update application endpoints to use n8n webhooks

### Notes

- n8n uses JavaScript/TypeScript for code nodes
- Expression syntax differs from Logic App expressions
- Consider using n8n's built-in error handling nodes
`, workflowName)
}

func (m *LogicAppMapper) generateAppChange(workflowName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_LOGIC_APP=%s\nTARGET_LOGICAPP_WEBHOOK=http://n8n:5678/webhook/%s\nLOGICAPP_ROUTE_PATH=/webhook/%s\nGENERATED_PATCH=config/logicapp/generated-client.patch\n", workflowName, workflowName, workflowName)
}

func (m *LogicAppMapper) generateClientPatch(workflowName string) string {
	return fmt.Sprintf("--- a/app/workflow.env\n+++ b/app/workflow.env\n@@\n-AZURE_LOGIC_APP=%s\n+LOGICAPP_WEBHOOK_URL=http://n8n:5678/webhook/%s\n+WORKFLOW_ENGINE=n8n\n", workflowName, workflowName)
}

func (m *LogicAppMapper) generateValidateScript(workflowName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/n8n/workflows/logicapp_workflow.json\ntest -s config/logicapp/app-change.env\ngrep -q %q config/n8n/workflows/logicapp_workflow.json\ngrep -q \"TARGET_LOGICAPP_WEBHOOK=http://n8n:5678/webhook/%s\" config/logicapp/app-change.env\n", workflowName, workflowName)
}

func (m *LogicAppMapper) generateBackupScript(workflowName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/logicapp-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/logicapp config/n8n 2>/dev/null || tar -czf \"$archive\" config/logicapp config/n8n/workflows\necho \"$archive\"\n", workflowName)
}

func (m *LogicAppMapper) generateCutoverScript(workflowName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/logicapp/app-change.env\ntest \"$SOURCE_LOGIC_APP\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\ntest -s \"$GENERATED_PATCH\"\necho \"Apply $GENERATED_PATCH and route Logic App callers to $TARGET_LOGICAPP_WEBHOOK\"\n", workflowName)
}

func logicAppRunbook(workflowName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "workflow", "source": "azurerm_logic_app_workflow", "workflow": workflowName, "target": "n8n"}
	return []domainrunbook.Step{
		logicAppStep("export-logicapp-definition", "Export Logic App definition", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/n8n/logic_app_definition.json || test -s config/n8n/workflows/logicapp_workflow.json"}, "Logic App definition or generated workflow exists", metadata),
		logicAppStep("provision-logicapp-target", "Provision Logic App n8n target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_n8n_logicapp.sh"}, "n8n workflow endpoint is ready", metadata),
		logicAppStep("validate-logicapp-workflow", "Validate Logic App workflow", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_logicapp_workflow.sh"}, "generated workflow and handoff config validate", metadata),
		logicAppStep("backup-logicapp-config", "Backup Logic App config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_logicapp_config.sh"}, "Logic App migration artifacts are archived", metadata),
		logicAppStep("cutover-logicapp-clients", "Cut over Logic App clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_logicapp_clients.sh"}, "clients use generated n8n webhook", metadata),
		logicAppStep("rollback-logicapp-source", "Keep Logic App source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Azure Logic Apps remains authoritative until workflow validation passes", metadata),
	}
}

func logicAppStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
