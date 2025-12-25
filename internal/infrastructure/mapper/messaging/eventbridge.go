// Package messaging provides mappers for AWS messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// EventBridgeMapper converts AWS EventBridge rules to n8n.
type EventBridgeMapper struct {
	*mapper.BaseMapper
}

// NewEventBridgeMapper creates a new EventBridge to n8n mapper.
func NewEventBridgeMapper() *EventBridgeMapper {
	return &EventBridgeMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeEventBridge, nil),
	}
}

// Map converts an EventBridge rule to an n8n service.
func (m *EventBridgeMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	ruleName := res.GetConfigString("name")
	if ruleName == "" {
		ruleName = res.Name
	}

	result := mapper.NewMappingResult("n8n")
	svc := result.DockerService

	svc.Image = "n8nio/n8n:latest"
	svc.Environment = map[string]string{
		"N8N_BASIC_AUTH_ACTIVE":     "true",
		"N8N_BASIC_AUTH_USER":       "admin",
		"N8N_BASIC_AUTH_PASSWORD":   "admin",
		"N8N_HOST":                  "localhost",
		"N8N_PORT":                  "5678",
		"N8N_PROTOCOL":              "http",
		"WEBHOOK_URL":               "http://localhost:5678/",
		"GENERIC_TIMEZONE":          "UTC",
		"N8N_ENCRYPTION_KEY":        "changeme",
	}
	svc.Ports = []string{"5678:5678"}
	svc.Volumes = []string{
		"./data/n8n:/home/node/.n8n",
		"./config/n8n/workflows:/workflows",
	}
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":                                       "aws_cloudwatch_event_rule",
		"cloudexit.rule_name":                                    ruleName,
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

	// Handle schedule expression (cron)
	if scheduleExpr := res.GetConfigString("schedule_expression"); scheduleExpr != "" {
		cronExpr := m.convertScheduleExpression(scheduleExpr)
		result.AddWarning(fmt.Sprintf("Schedule expression detected: %s. Converted to cron: %s", scheduleExpr, cronExpr))
		result.AddManualStep(fmt.Sprintf("Create n8n Schedule Trigger workflow with cron: %s", cronExpr))
	}

	// Handle event pattern
	if eventPattern := res.Config["event_pattern"]; eventPattern != nil {
		patternJSON, _ := json.MarshalIndent(eventPattern, "", "  ")
		result.AddConfig("config/n8n/event_pattern.json", patternJSON)
		result.AddWarning("Event pattern detected. Create n8n Webhook trigger and filter in workflow.")
		result.AddManualStep("Configure webhook trigger in n8n to receive events")
		result.AddManualStep("Add IF node to filter events based on pattern")
	}

	// Handle targets
	if targets := res.Config["targets"]; targets != nil {
		m.handleTargets(res, result, ruleName)
	}

	workflowTemplate := m.generateWorkflowTemplate(ruleName)
	result.AddConfig("config/n8n/workflows/eventbridge_workflow.json", []byte(workflowTemplate))

	setupScript := m.generateSetupScript(ruleName)
	result.AddScript("setup_n8n.sh", []byte(setupScript))

	result.AddManualStep("Access n8n at http://localhost:5678")
	result.AddManualStep("Default credentials: admin/admin")
	result.AddManualStep("Import workflow template from config/n8n/workflows/")
	result.AddManualStep("Configure webhook endpoints for event sources")
	result.AddManualStep("Update application code to send events to n8n webhooks")

	return result, nil
}

func (m *EventBridgeMapper) convertScheduleExpression(expr string) string {
	// AWS format: rate(1 hour) or cron(0 12 * * ? *)
	// n8n format: standard cron
	if len(expr) > 5 && expr[:5] == "rate(" {
		return "0 * * * *" // Default to hourly
	}
	if len(expr) > 5 && expr[:5] == "cron(" {
		// Extract cron from AWS format
		cronPart := expr[5 : len(expr)-1]
		// AWS cron has 6 fields, standard has 5
		return cronPart
	}
	return "0 * * * *"
}

func (m *EventBridgeMapper) handleTargets(res *resource.AWSResource, result *mapper.MappingResult, ruleName string) {
	if targetsData, ok := res.Config["targets"].([]interface{}); ok {
		for i, target := range targetsData {
			if targetMap, ok := target.(map[string]interface{}); ok {
				targetArn := ""
				if arn, ok := targetMap["arn"].(string); ok {
					targetArn = arn
				}
				result.AddManualStep(fmt.Sprintf("Target %d: Configure n8n HTTP Request node to call: %s", i+1, targetArn))
			}
		}
	}
}

func (m *EventBridgeMapper) generateWorkflowTemplate(ruleName string) string {
	workflow := map[string]interface{}{
		"name": fmt.Sprintf("EventBridge - %s", ruleName),
		"nodes": []map[string]interface{}{
			{
				"name":        "Webhook Trigger",
				"type":        "n8n-nodes-base.webhook",
				"typeVersion": 1,
				"position":    []int{250, 300},
				"parameters": map[string]interface{}{
					"path":           ruleName,
					"httpMethod":     "POST",
					"responseMode":   "onReceived",
					"responseData":   "allEntries",
				},
			},
			{
				"name":        "Process Event",
				"type":        "n8n-nodes-base.code",
				"typeVersion": 1,
				"position":    []int{450, 300},
				"parameters": map[string]interface{}{
					"jsCode": "// Process EventBridge event\nconst event = items[0].json;\nconsole.log('Received event:', event);\nreturn items;",
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
		},
	}
	content, _ := json.MarshalIndent(workflow, "", "  ")
	return string(content)
}

func (m *EventBridgeMapper) generateSetupScript(ruleName string) string {
	return fmt.Sprintf(`#!/bin/bash
# n8n Setup Script for EventBridge rule: %s

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
`, ruleName, ruleName)
}
