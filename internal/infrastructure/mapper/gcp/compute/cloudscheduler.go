// Package compute provides mappers for GCP compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
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
		"JOB_NAME":    jobName,
		"JOB_SCHEDULE": schedule,
	}

	if timeZone != "" {
		svc.Environment["TZ"] = timeZone
	}

	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"

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

	result.AddManualStep("Review and customize the Ofelia configuration in scheduler/ofelia.ini")
	result.AddManualStep("Ensure the target service/endpoint is properly configured")
	result.AddManualStep("Test the scheduled job manually before relying on automation")

	return result, nil
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
			result.AddManualStep("Configure OAuth token for HTTP target authentication")
		}
		if targetMap["oidc_token"] != nil {
			result.AddWarning("OIDC token configured. Set up OIDC authentication manually.")
			result.AddManualStep("Configure OIDC token for HTTP target authentication")
		}
	}
}

// handlePubSubTarget processes Pub/Sub target configuration.
func (m *CloudSchedulerMapper) handlePubSubTarget(target interface{}, result *mapper.MappingResult, jobName, schedule string) {
	if targetMap, ok := target.(map[string]interface{}); ok {
		topicName, _ := targetMap["topic_name"].(string)
		data, _ := targetMap["data"].(string)

		result.AddWarning(fmt.Sprintf("Pub/Sub target: topic %s", topicName))
		result.AddManualStep("Configure message queue integration to replace Pub/Sub target")

		// Generate a sample script that could publish to a local message queue
		result.AddConfig(fmt.Sprintf("scheduler/jobs/%s.sh", jobName), []byte(fmt.Sprintf(`#!/bin/sh
# Cloud Scheduler Job: %s
# Schedule: %s
# Original target: Pub/Sub topic %s

# TODO: Replace with your message queue publishing command
# Example with RabbitMQ:
# rabbitmqadmin publish exchange=amq.default routing_key=%s payload='%s'

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
		result.AddManualStep("Update the target URL to point to your self-hosted App Engine service")

		// Generate curl-based job
		result.AddConfig(fmt.Sprintf("scheduler/jobs/%s.sh", jobName), []byte(fmt.Sprintf(`#!/bin/sh
# Cloud Scheduler Job: %s
# Schedule: %s
# Original target: App Engine service %s

# TODO: Update the URL to your self-hosted service
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
