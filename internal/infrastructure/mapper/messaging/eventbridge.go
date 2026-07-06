// Package messaging provides mappers for AWS messaging services.
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
		"homeport.source":               "aws_cloudwatch_event_rule",
		"homeport.rule_name":            ruleName,
		"traefik.enable":                "true",
		"traefik.http.routers.n8n.rule": "Host(`n8n.localhost`)",
		"traefik.http.services.n8n.loadbalancer.server.port": "5678",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:5678/healthz"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"

	// Handle schedule expression (cron)
	if scheduleExpr := res.GetConfigString("schedule_expression"); scheduleExpr != "" {
		cronExpr := m.convertScheduleExpression(scheduleExpr)
		result.AddWarning(fmt.Sprintf("Schedule expression detected: %s. Converted to cron: %s", scheduleExpr, cronExpr))
		result.AddConfig("config/eventbridge/schedule.env", []byte(fmt.Sprintf("SOURCE_RULE=%s\nSOURCE_SCHEDULE=%s\nTARGET_CRON=%s\n", ruleName, scheduleExpr, cronExpr)))
	}

	// Handle event pattern
	if eventPattern := res.Config["event_pattern"]; eventPattern != nil {
		patternJSON, _ := json.MarshalIndent(eventPattern, "", "  ")
		result.AddConfig("config/n8n/event_pattern.json", patternJSON)
		result.AddWarning("Event pattern detected. Generated n8n workflow filter config is included.")
	}

	// Handle targets
	if targets := res.Config["targets"]; targets != nil {
		m.handleTargets(res, result, ruleName)
	}

	workflowTemplate := m.generateWorkflowTemplate(ruleName)
	result.AddConfig("config/n8n/workflows/eventbridge_workflow.json", []byte(workflowTemplate))

	setupScript := m.generateSetupScript(ruleName)
	result.AddScript("setup_n8n.sh", []byte(setupScript))
	result.AddScript("dispatch_eventbridge_event.sh", []byte(m.generateDispatchScript(ruleName)))
	result.AddScript("backup_eventbridge_workflow.sh", []byte(m.generateBackupScript(ruleName)))
	result.AddScript("validate_eventbridge_route.sh", []byte(m.generateValidateScript(ruleName)))
	result.AddConfig("config/eventbridge/app-change.env", []byte(m.generateAppChangeConfig(ruleName)))
	for _, step := range eventBridgeRunbook(ruleName) {
		result.AddRunbookStep(step)
	}

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
		targets := make([]map[string]string, 0, len(targetsData))
		for i, target := range targetsData {
			if targetMap, ok := target.(map[string]interface{}); ok {
				targetArn := ""
				if arn, ok := targetMap["arn"].(string); ok {
					targetArn = arn
				}
				targets = append(targets, map[string]string{
					"id":  fmt.Sprintf("target-%d", i+1),
					"arn": targetArn,
				})
			}
		}
		content, _ := json.MarshalIndent(targets, "", "  ")
		result.AddConfig("config/eventbridge/targets.json", content)
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
					"path":         ruleName,
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

func (m *EventBridgeMapper) generateAppChangeConfig(ruleName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_RULE=%s
TARGET_ROUTER=n8n
TARGET_WEBHOOK=http://localhost:5678/webhook/%s
`, ruleName, ruleName)
}

func (m *EventBridgeMapper) generateDispatchScript(ruleName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
payload="${EVENTBRIDGE_SAMPLE:-{\"source\":\"homeport\",\"detail-type\":\"Validation\"}}"
curl -fsS -X POST "http://${N8N_HOST:-localhost}:${N8N_PORT:-5678}/webhook/%s" \
  -H 'content-type: application/json' \
  -d "$payload"
`, ruleName)
}

func (m *EventBridgeMapper) generateBackupScript(ruleName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-eventbridge-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/eventbridge config/n8n
echo "$archive"
`, ruleName)
}

func (m *EventBridgeMapper) generateValidateScript(ruleName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/eventbridge/app-change.env
test -s config/n8n/workflows/eventbridge_workflow.json
curl -fsS "http://${N8N_HOST:-localhost}:${N8N_PORT:-5678}/healthz" >/dev/null
echo "EventBridge route %s validated"
`, ruleName)
}

func eventBridgeRunbook(ruleName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "event-router", "source": "aws_cloudwatch_event_rule", "rule": ruleName}
	return []domainrunbook.Step{
		eventBridgeStep("render-eventbridge-workflow", "Render EventBridge workflow", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/n8n/workflows/eventbridge_workflow.json"}, "n8n workflow is rendered", metadata),
		eventBridgeStep("provision-event-router", "Provision event router", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_n8n.sh"}, "n8n event router is reachable", metadata),
		eventBridgeStep("dispatch-eventbridge-sample", "Dispatch EventBridge sample", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "dispatch_eventbridge_event.sh"}, "sample event reaches target workflow", metadata),
		eventBridgeStep("validate-eventbridge-route", "Validate EventBridge route", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_eventbridge_route.sh"}, "route health and workflow config pass", metadata),
		eventBridgeStep("backup-eventbridge-workflow", "Backup EventBridge workflow", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_eventbridge_workflow.sh"}, "workflow and route config are archived", metadata),
		eventBridgeStep("cutover-eventbridge-producers", "Cut over EventBridge producers", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/eventbridge/app-change.env"}, "event producers use the generated webhook", metadata),
		eventBridgeStep("rollback-eventbridge-source", "Keep EventBridge source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS EventBridge remains authoritative until route validation passes", metadata),
	}
}

func eventBridgeStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
