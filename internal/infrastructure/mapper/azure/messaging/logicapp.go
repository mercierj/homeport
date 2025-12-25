// Package messaging provides mappers for Azure messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":                                       "azurerm_logic_app_workflow",
		"cloudexit.workflow_name":                                workflowName,
		"traefik.enable":                                         "true",
		"traefik.http.routers.n8n.rule":                          "Host(`n8n.localhost`)",
		"traefik.http.services.n8n.loadbalancer.server.port":     "5678",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:5678/healthz"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"

	// Handle workflow definition
	if workflowDef := res.Config["workflow_definition"]; workflowDef != nil {
		result.AddWarning("Workflow definition detected. Manual conversion to n8n workflow required.")
		defJSON, _ := json.MarshalIndent(workflowDef, "", "  ")
		result.AddConfig("config/n8n/logic_app_definition.json", defJSON)
		result.AddManualStep("Review Logic App definition and recreate workflow in n8n")
	}

	// Handle access control
	if res.Config["access_control"] != nil {
		result.AddWarning("Access control configured. Set up authentication in n8n.")
		result.AddManualStep("Configure n8n authentication and webhook signatures")
	}

	// Handle integration account
	if res.GetConfigString("integration_account_id") != "" {
		result.AddWarning("Integration account linked. Set up equivalent integrations in n8n.")
		result.AddManualStep("Configure third-party integrations using n8n credential system")
	}

	workflowTemplate := m.generateWorkflowTemplate(workflowName)
	result.AddConfig("config/n8n/workflows/logicapp_workflow.json", []byte(workflowTemplate))

	setupScript := m.generateSetupScript(workflowName)
	result.AddScript("setup_n8n_logicapp.sh", []byte(setupScript))

	migrationGuide := m.generateMigrationGuide(workflowName)
	result.AddConfig("config/n8n/migration_guide.md", []byte(migrationGuide))

	result.AddManualStep("Access n8n at http://localhost:5678")
	result.AddManualStep("Default credentials: admin/admin")
	result.AddManualStep("Import workflow template from config/n8n/workflows/")
	result.AddManualStep("Review Logic App definition and recreate actions in n8n")
	result.AddManualStep("Set up webhook triggers for HTTP-triggered Logic Apps")

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
