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

type EventarcMapper struct {
	*mapper.BaseMapper
}

func NewEventarcMapper() *EventarcMapper {
	return &EventarcMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeEventarcTrigger, nil)}
}

func (m *EventarcMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	triggerName := res.GetConfigString("name")
	if triggerName == "" {
		triggerName = res.GetConfigString("trigger_name")
	}
	if triggerName == "" {
		triggerName = res.Name
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
	svc.Volumes = []string{"./data/n8n:/home/node/.n8n", "./config/n8n/workflows:/workflows"}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeEventarcTrigger), "homeport.trigger": triggerName}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:5678/healthz"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"

	result.AddConfig("config/n8n/workflows/eventarc_workflow.json", []byte(m.generateWorkflow(triggerName)))
	result.AddConfig("config/eventarc/app-change.env", []byte(m.generateAppChangeConfig(triggerName)))
	result.AddConfig("config/eventarc/trigger-filter.json", []byte(m.generateTriggerFilter(res)))
	result.AddScript("setup_n8n_eventarc.sh", []byte(m.generateSetupScript(triggerName)))
	result.AddScript("dispatch_eventarc_event.sh", []byte(m.generateDispatchScript(triggerName)))
	result.AddScript("backup_eventarc_workflow.sh", []byte(m.generateBackupScript(triggerName)))
	result.AddScript("validate_eventarc_route.sh", []byte(m.generateValidateScript(triggerName)))
	for _, step := range eventarcRunbook(triggerName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *EventarcMapper) generateWorkflow(triggerName string) string {
	workflow := map[string]interface{}{
		"name": fmt.Sprintf("Eventarc - %s", triggerName),
		"nodes": []map[string]interface{}{
			{
				"name":        "Webhook Trigger",
				"type":        "n8n-nodes-base.webhook",
				"typeVersion": 1,
				"position":    []int{250, 300},
				"parameters": map[string]interface{}{
					"path":         triggerName,
					"httpMethod":   "POST",
					"responseMode": "onReceived",
				},
			},
			{
				"name":        "Process CloudEvent",
				"type":        "n8n-nodes-base.code",
				"typeVersion": 1,
				"position":    []int{450, 300},
				"parameters": map[string]interface{}{
					"jsCode": "const event = items[0].json;\nreturn [{ json: event }];",
				},
			},
		},
		"connections": map[string]interface{}{
			"Webhook Trigger": map[string]interface{}{
				"main": [][]map[string]interface{}{{{"node": "Process CloudEvent", "type": "main", "index": 0}}},
			},
		},
	}
	content, _ := json.MarshalIndent(workflow, "", "  ")
	return string(content)
}

func (m *EventarcMapper) generateTriggerFilter(res *resource.AWSResource) string {
	filter := map[string]interface{}{
		"source":            "google_eventarc_trigger",
		"matching_criteria": res.Config["matching_criteria"],
		"destination":       res.Config["destination"],
	}
	content, _ := json.MarshalIndent(filter, "", "  ")
	return string(content)
}

func (m *EventarcMapper) generateAppChangeConfig(triggerName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_EVENTARC_TRIGGER=%s\nTARGET_ROUTER=n8n\nTARGET_WEBHOOK=http://localhost:5678/webhook/%s\n", triggerName, triggerName)
}

func (m *EventarcMapper) generateSetupScript(triggerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nN8N_HOST=\"${N8N_HOST:-localhost}\"\nN8N_PORT=\"${N8N_PORT:-5678}\"\nuntil curl -sf \"http://$N8N_HOST:$N8N_PORT/healthz\" >/dev/null 2>&1; do sleep 5; done\necho \"Eventarc trigger %s webhook: http://$N8N_HOST:$N8N_PORT/webhook/%s\"\n", triggerName, triggerName)
}

func (m *EventarcMapper) generateDispatchScript(triggerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\npayload='{\"specversion\":\"1.0\",\"type\":\"google.cloud.audit.log.v1.written\",\"source\":\"homeport\",\"id\":\"validation\"}'\ncurl -fsS -X POST \"http://${N8N_HOST:-localhost}:${N8N_PORT:-5678}/webhook/%s\" -H 'content-type: application/cloudevents+json' -d \"$payload\"\n", triggerName)
}

func (m *EventarcMapper) generateBackupScript(triggerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/eventarc-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/eventarc config/n8n\necho \"$archive\"\n", triggerName)
}

func (m *EventarcMapper) generateValidateScript(triggerName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/eventarc/app-change.env\ntest -s config/n8n/workflows/eventarc_workflow.json\ncurl -fsS \"http://${N8N_HOST:-localhost}:${N8N_PORT:-5678}/healthz\" >/dev/null\necho \"Eventarc trigger %s route validated\"\n", triggerName)
}

func eventarcRunbook(triggerName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "event-router", "source": "google_eventarc_trigger", "trigger": triggerName, "target": "n8n"}
	return []domainrunbook.Step{
		eventarcStep("render-eventarc-workflow", "Render Eventarc workflow", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/n8n/workflows/eventarc_workflow.json"}, "n8n workflow is rendered", metadata),
		eventarcStep("provision-eventarc-router", "Provision event router", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_n8n_eventarc.sh"}, "n8n event router is reachable", metadata),
		eventarcStep("dispatch-eventarc-sample", "Dispatch Eventarc sample", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "dispatch_eventarc_event.sh"}, "sample CloudEvent reaches target workflow", metadata),
		eventarcStep("validate-eventarc-route", "Validate Eventarc route", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_eventarc_route.sh"}, "route health and workflow config pass", metadata),
		eventarcStep("backup-eventarc-workflow", "Backup Eventarc workflow", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_eventarc_workflow.sh"}, "workflow and route config are archived", metadata),
		eventarcStep("cutover-eventarc-producers", "Cut over Eventarc producers", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/eventarc/app-change.env"}, "event producers use the generated webhook", metadata),
		eventarcStep("rollback-eventarc-source", "Keep Eventarc source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Eventarc remains authoritative until route validation passes", metadata),
	}
}

func eventarcStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
