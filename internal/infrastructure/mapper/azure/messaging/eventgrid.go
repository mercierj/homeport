// Package messaging provides mappers for Azure messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// EventGridMapper converts Azure Event Grid topics to n8n.
type EventGridMapper struct {
	*mapper.BaseMapper
}

// NewEventGridMapper creates a new Event Grid to n8n mapper.
func NewEventGridMapper() *EventGridMapper {
	return &EventGridMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeEventGrid, nil),
	}
}

// Map converts an Event Grid topic to an n8n service.
func (m *EventGridMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	topicName := res.GetConfigString("name")
	if topicName == "" {
		topicName = res.Name
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
		"homeport.source":                                       "azurerm_eventgrid_topic",
		"homeport.topic_name":                                   topicName,
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

	// Handle input schema
	if inputSchema := res.GetConfigString("input_schema"); inputSchema != "" {
		result.AddWarning("Custom input schema detected. Configure webhook validation in n8n.")
		result.AddManualStep("Set up JSON schema validation in n8n webhook node")
	}

	workflowTemplate := m.generateWorkflowTemplate(topicName)
	result.AddConfig("config/n8n/workflows/eventgrid_workflow.json", []byte(workflowTemplate))

	setupScript := m.generateSetupScript(topicName)
	result.AddScript("setup_n8n_eventgrid.sh", []byte(setupScript))

	result.AddManualStep("Access n8n at http://localhost:5678")
	result.AddManualStep("Default credentials: admin/admin")
	result.AddManualStep("Import workflow template from config/n8n/workflows/")
	result.AddManualStep(fmt.Sprintf("Webhook URL: http://localhost:5678/webhook/%s", topicName))
	result.AddManualStep("Update application code to send events to n8n webhooks")

	return result, nil
}

func (m *EventGridMapper) generateWorkflowTemplate(topicName string) string {
	workflow := map[string]interface{}{
		"name": fmt.Sprintf("Event Grid - %s", topicName),
		"nodes": []map[string]interface{}{
			{
				"name":        "Webhook Trigger",
				"type":        "n8n-nodes-base.webhook",
				"typeVersion": 1,
				"position":    []int{250, 300},
				"parameters": map[string]interface{}{
					"path":         topicName,
					"httpMethod":   "POST",
					"responseMode": "onReceived",
					"responseData": "allEntries",
				},
			},
			{
				"name":        "Process Event",
				"type":        "n8n-nodes-base.code",
				"typeVersion": 1,
				"position":    []int{450, 300},
				"parameters": map[string]interface{}{
					"jsCode": `// Process Event Grid event
const event = items[0].json;
console.log('Event Type:', event.eventType);
console.log('Subject:', event.subject);
console.log('Data:', event.data);
return items;`,
				},
			},
			{
				"name":        "Route by Event Type",
				"type":        "n8n-nodes-base.switch",
				"typeVersion": 1,
				"position":    []int{650, 300},
				"parameters": map[string]interface{}{
					"dataPropertyName": "eventType",
					"rules": map[string]interface{}{
						"rules": []map[string]interface{}{
							{"value": "Microsoft.Storage.BlobCreated"},
							{"value": "Microsoft.Storage.BlobDeleted"},
						},
					},
				},
			},
		},
		"connections": map[string]interface{}{
			"Webhook Trigger": map[string]interface{}{
				"main": [][]map[string]interface{}{
					{{
						"node":  "Process Event",
						"type":  "main",
						"index": 0,
					}},
				},
			},
			"Process Event": map[string]interface{}{
				"main": [][]map[string]interface{}{
					{{
						"node":  "Route by Event Type",
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

func (m *EventGridMapper) generateSetupScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/bash
# n8n Setup Script for Event Grid topic: %s

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
echo "Webhook URL: http://$N8N_HOST:$N8N_PORT/webhook/%s"
echo ""
echo "Event Grid event format:"
echo '{'
echo '  "id": "unique-id",'
echo '  "eventType": "Custom.Event.Type",'
echo '  "subject": "/myapp/items/1",'
echo '  "eventTime": "2024-01-01T00:00:00Z",'
echo '  "data": { "key": "value" },'
echo '  "dataVersion": "1.0"'
echo '}'
`, topicName, topicName)
}
