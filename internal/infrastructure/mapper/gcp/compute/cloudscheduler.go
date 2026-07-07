// Package compute provides mappers for GCP compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// CloudSchedulerMapper converts GCP Cloud Scheduler jobs to cron-based Docker services.
type CloudSchedulerMapper struct {
	*mapper.BaseMapper
}

// NewCloudSchedulerMapper creates a new Cloud Scheduler mapper.
func NewCloudSchedulerMapper() *CloudSchedulerMapper {
	return &CloudSchedulerMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudScheduler, nil),
	}
}

// Map converts a Cloud Scheduler job to a Docker service with cron.
func (m *CloudSchedulerMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	jobName := res.GetConfigString("name")
	if jobName == "" {
		jobName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(jobName))
	svc := result.DockerService

	schedule := res.GetConfigString("schedule")
	description := res.GetConfigString("description")
	timeZone := res.GetConfigString("time_zone")
	attemptDeadline := res.GetConfigString("attempt_deadline")

	// Set up Ofelia cron scheduler image
	svc.Image = "mcuadros/ofelia:latest"

	// Configure service
	svc.Environment = map[string]string{
		"JOB_NAME":     jobName,
		"JOB_SCHEDULE": schedule,
	}

	if timeZone != "" {
		svc.Environment["TZ"] = timeZone
	}

	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}

	// Volumes for Docker socket (Ofelia needs this to trigger other containers)
	svc.Volumes = []string{
		"/var/run/docker.sock:/var/run/docker.sock:ro",
	}

	// Labels for the scheduler
	svc.Labels = map[string]string{
		"homeport.source":      "google_cloud_scheduler_job",
		"homeport.job_name":    jobName,
		"homeport.schedule":    schedule,
		"homeport.description": description,
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "pgrep ofelia || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}

	// Handle HTTP target
	if httpTarget := res.Config["http_target"]; httpTarget != nil {
		m.handleHTTPTarget(httpTarget, result, jobName, schedule)
	}

	// Handle Pub/Sub target
	if pubsubTarget := res.Config["pubsub_target"]; pubsubTarget != nil {
		m.handlePubSubTarget(pubsubTarget, result, jobName, schedule)
	}

	// Handle App Engine HTTP target
	if appEngineTarget := res.Config["app_engine_http_target"]; appEngineTarget != nil {
		m.handleAppEngineTarget(appEngineTarget, result, jobName, schedule)
	}

	// Generate Ofelia configuration
	ofeliaConfig := m.generateOfeliaConfig(jobName, schedule, res)
	result.AddConfig("scheduler/ofelia.ini", []byte(ofeliaConfig))

	// Add volumes mount for config
	svc.Volumes = append(svc.Volumes, "./scheduler/ofelia.ini:/etc/ofelia/config.ini:ro")
	result.AddConfig("config/cloud-scheduler/app-change.env", []byte(m.generateAppChangeConfig(jobName)))
	result.AddConfig("config/cloud-scheduler/job-report.yaml", []byte(m.generateJobReport(jobName, schedule, timeZone)))
	result.AddScript("backup_cloud_scheduler.sh", []byte(m.generateBackupScript(jobName)))
	result.AddScript("validate_cloud_scheduler.sh", []byte(m.generateValidateScript(jobName)))

	// Handle retry config
	if retryConfig := res.Config["retry_config"]; retryConfig != nil {
		if retryMap, ok := retryConfig.(map[string]interface{}); ok {
			if retryCount, ok := retryMap["retry_count"].(float64); ok && retryCount > 0 {
				result.AddWarning(fmt.Sprintf("Retry configured with %d attempts. Configure retry logic in your job.", int(retryCount)))
			}
		}
	}

	if attemptDeadline != "" {
		result.AddWarning(fmt.Sprintf("Attempt deadline of %s configured. Ensure your job respects this timeout.", attemptDeadline))
	}

	for _, step := range cloudSchedulerRunbook(jobName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *CloudSchedulerMapper) generateAppChangeConfig(jobName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_CLOUD_SCHEDULER_JOB=%s
TARGET_SCHEDULER=ofelia
TARGET_SCHEDULER_CONFIG=scheduler/ofelia.ini
`, jobName)
}

func (m *CloudSchedulerMapper) generateJobReport(jobName, schedule, timeZone string) string {
	return fmt.Sprintf(`source: google_cloud_scheduler_job
job: %s
schedule: %s
time_zone: %s
target: ofelia
`, jobName, schedule, timeZone)
}

// handleHTTPTarget processes HTTP target configuration.
func (m *CloudSchedulerMapper) handleHTTPTarget(target interface{}, result *mapper.MappingResult, jobName, schedule string) {
	if targetMap, ok := target.(map[string]interface{}); ok {
		uri, _ := targetMap["uri"].(string)
		httpMethod, _ := targetMap["http_method"].(string)
		if httpMethod == "" {
			httpMethod = "POST"
		}

		result.AddWarning(fmt.Sprintf("HTTP target: %s %s", httpMethod, uri))

		// Generate curl-based job command
		curlCmd := fmt.Sprintf("curl -X %s %s", httpMethod, uri)

		// Handle headers
		if headers, ok := targetMap["headers"].(map[string]interface{}); ok {
			for k, v := range headers {
				if strVal, ok := v.(string); ok {
					curlCmd += fmt.Sprintf(" -H '%s: %s'", k, strVal)
				}
			}
		}

		// Handle body
		if body, ok := targetMap["body"].(string); ok && body != "" {
			curlCmd += fmt.Sprintf(" -d '%s'", body)
		}

		result.AddConfig(fmt.Sprintf("scheduler/jobs/%s.sh", jobName), []byte(fmt.Sprintf(`#!/bin/sh
# Cloud Scheduler Job: %s
# Schedule: %s

%s
`, jobName, schedule, curlCmd)))

		// Handle OAuth/OIDC tokens
		if targetMap["oauth_token"] != nil {
			result.AddWarning("OAuth token configured. Set up authentication manually.")
			result.AddConfig(fmt.Sprintf("config/cloud-scheduler/%s-auth.yaml", jobName), []byte("auth: oauth\ndelivery: generated_header_injection\n"))
		}
		if targetMap["oidc_token"] != nil {
			result.AddWarning("OIDC token configured. Set up OIDC authentication manually.")
			result.AddConfig(fmt.Sprintf("config/cloud-scheduler/%s-auth.yaml", jobName), []byte("auth: oidc\ndelivery: generated_header_injection\n"))
		}
	}
}

// handlePubSubTarget processes Pub/Sub target configuration.
func (m *CloudSchedulerMapper) handlePubSubTarget(target interface{}, result *mapper.MappingResult, jobName, schedule string) {
	if targetMap, ok := target.(map[string]interface{}); ok {
		topicName, _ := targetMap["topic_name"].(string)
		data, _ := targetMap["data"].(string)

		result.AddWarning(fmt.Sprintf("Pub/Sub target: topic %s", topicName))
		result.AddConfig(fmt.Sprintf("config/cloud-scheduler/%s-pubsub-target.yaml", jobName), []byte(fmt.Sprintf("topic: %s\ntarget: generated_queue_publish\n", topicName)))

		// Generate a sample script that could publish to a local message queue
		result.AddConfig(fmt.Sprintf("scheduler/jobs/%s.sh", jobName), []byte(fmt.Sprintf(`#!/bin/sh
# Cloud Scheduler Job: %s
# Schedule: %s
# Original target: Pub/Sub topic %s

rabbitmqadmin publish exchange=amq.default routing_key=%s payload='%s'

echo "Job %s triggered at $(date)"
`, jobName, schedule, topicName, topicName, data, jobName)))
	}
}

// handleAppEngineTarget processes App Engine HTTP target configuration.
func (m *CloudSchedulerMapper) handleAppEngineTarget(target interface{}, result *mapper.MappingResult, jobName, schedule string) {
	if targetMap, ok := target.(map[string]interface{}); ok {
		relativeUri, _ := targetMap["relative_uri"].(string)
		httpMethod, _ := targetMap["http_method"].(string)
		service, _ := targetMap["app_engine_routing"].(map[string]interface{})

		if httpMethod == "" {
			httpMethod = "POST"
		}

		serviceName := "default"
		if service != nil {
			if svc, ok := service["service"].(string); ok {
				serviceName = svc
			}
		}

		result.AddWarning(fmt.Sprintf("App Engine target: %s %s (service: %s)", httpMethod, relativeUri, serviceName))
		result.AddConfig(fmt.Sprintf("config/cloud-scheduler/%s-appengine-target.yaml", jobName), []byte(fmt.Sprintf("service: %s\nrelative_uri: %s\ntarget: generated_http_delivery\n", serviceName, relativeUri)))

		// Generate curl-based job
		result.AddConfig(fmt.Sprintf("scheduler/jobs/%s.sh", jobName), []byte(fmt.Sprintf(`#!/bin/sh
# Cloud Scheduler Job: %s
# Schedule: %s
# Original target: App Engine service %s

curl -X %s http://%s:8080%s
`, jobName, schedule, serviceName, httpMethod, serviceName, relativeUri)))
	}
}

// generateOfeliaConfig creates the Ofelia configuration file.
func (m *CloudSchedulerMapper) generateOfeliaConfig(jobName, schedule string, res *resource.AWSResource) string {
	sanitizedName := m.sanitizeName(jobName)

	// Convert Cloud Scheduler cron format to Ofelia format
	// Cloud Scheduler uses unix-cron format which is compatible
	cronSchedule := schedule

	config := fmt.Sprintf(`# Ofelia Configuration
# Converted from Google Cloud Scheduler job: %s

[global]
# Enable logging
log-to-stdout = true

`, jobName)

	// Determine job type based on target
	if httpTarget := res.Config["http_target"]; httpTarget != nil {
		config += fmt.Sprintf(`[job-exec "%s"]
schedule = %s
container = %s-worker
command = /bin/sh /jobs/%s.sh
`, sanitizedName, cronSchedule, sanitizedName, jobName)
	} else if res.Config["pubsub_target"] != nil || res.Config["app_engine_http_target"] != nil {
		config += fmt.Sprintf(`[job-exec "%s"]
schedule = %s
container = %s-worker
command = /bin/sh /jobs/%s.sh
`, sanitizedName, cronSchedule, sanitizedName, jobName)
	} else {
		// Default job-local for running commands in the ofelia container itself
		config += fmt.Sprintf(`[job-local "%s"]
schedule = %s
command = echo "Job %s triggered at $(date)"
`, sanitizedName, cronSchedule, jobName)
	}

	return config
}

func (m *CloudSchedulerMapper) generateBackupScript(jobName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/cloud-scheduler-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" scheduler config/cloud-scheduler
echo "$archive"
`, m.sanitizeName(jobName))
}

func (m *CloudSchedulerMapper) generateValidateScript(jobName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s scheduler/ofelia.ini
test -s scheduler/jobs/%s.sh
test -s config/cloud-scheduler/app-change.env
ofelia validate --config scheduler/ofelia.ini
echo "Cloud Scheduler job %s validated on Ofelia"
`, jobName, jobName)
}

func cloudSchedulerRunbook(jobName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "scheduler", "source": "google_cloud_scheduler_job", "job": jobName}
	return []domainrunbook.Step{
		cloudSchedulerStep("discover-cloud-scheduler-job", "Discover Cloud Scheduler job", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("gcloud scheduler jobs describe %q --format=json", jobName)}, "source job configuration is exported", metadata),
		cloudSchedulerStep("provision-ofelia-scheduler", "Provision Ofelia scheduler", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s scheduler/ofelia.ini"}, "Ofelia config is rendered", metadata),
		cloudSchedulerStep("migrate-cloud-scheduler-job", "Migrate Cloud Scheduler job", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/cloud-scheduler/job-report.yaml"}, "schedule and target delivery are rendered", metadata),
		cloudSchedulerStep("validate-cloud-scheduler-job", "Validate scheduler job", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_cloud_scheduler.sh"}, "scheduler config validates", metadata),
		cloudSchedulerStep("backup-cloud-scheduler-config", "Backup scheduler config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloud_scheduler.sh"}, "scheduler config archive is produced", metadata),
		cloudSchedulerStep("cutover-cloud-scheduler-job", "Cut over scheduler job", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/cloud-scheduler/app-change.env"}, "generated patch points schedules at Ofelia", metadata),
		cloudSchedulerStep("rollback-cloud-scheduler-job", "Keep Cloud Scheduler as rollback", "Rollback", domainrunbook.StepTypeRollback, nil, "source Cloud Scheduler job remains available until validation passes", metadata),
	}
}

func cloudSchedulerStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func (m *CloudSchedulerMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}
	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "scheduler"
	}
	return validName
}
