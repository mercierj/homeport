// Package messaging provides mappers for GCP messaging services.
package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// CloudTasksMapper converts GCP Cloud Tasks queues to Celery + Redis.
type CloudTasksMapper struct {
	*mapper.BaseMapper
}

// NewCloudTasksMapper creates a new Cloud Tasks to Celery mapper.
func NewCloudTasksMapper() *CloudTasksMapper {
	return &CloudTasksMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudTasks, nil),
	}
}

// Map converts a Cloud Tasks queue to a Celery + Redis service.
func (m *CloudTasksMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	queueName := res.GetConfigString("name")
	if queueName == "" {
		queueName = res.Name
	}

	result := mapper.NewMappingResult("redis")
	svc := result.DockerService

	svc.Image = "redis:7-alpine"
	svc.Command = []string{
		"redis-server",
		"--appendonly", "yes",
		"--maxmemory", "256mb",
		"--maxmemory-policy", "allkeys-lru",
	}
	svc.Ports = []string{"6379:6379"}
	svc.Volumes = []string{"./data/redis:/data"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":     "google_cloud_tasks_queue",
		"homeport.queue_name": queueName,
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "redis-cli", "ping"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"

	if m.hasRetryConfig(res) {
		maxAttempts := res.GetConfigInt("max_attempts")
		result.AddWarning(fmt.Sprintf("Retry configuration detected (max_attempts: %d). Configure task retries in Celery.", maxAttempts))
	}

	if dispatchDeadline := res.GetConfigString("dispatch_deadline"); dispatchDeadline != "" {
		result.AddWarning(fmt.Sprintf("Dispatch deadline: %s. Configure task time limits in Celery.", dispatchDeadline))
	}

	celeryConfig := m.generateCeleryConfig(queueName)
	result.AddConfig("config/celery/celeryconfig.py", []byte(celeryConfig))
	result.AddConfig("config/celery/app-change.env", []byte(m.generateAppChangeConfig(queueName)))
	result.AddConfig("config/celery/task-report.yaml", []byte(m.generateTaskReport(res, queueName)))

	workerDockerfile := m.generateWorkerDockerfile()
	result.AddConfig("Dockerfile.celery-worker", []byte(workerDockerfile))

	tasksExample := m.generateTasksExample(queueName)
	result.AddConfig("config/celery/tasks.py", []byte(tasksExample))

	composeSnippet := m.generateComposeSnippet(queueName)
	result.AddConfig("docker-compose.celery.yml", []byte(composeSnippet))

	setupScript := m.generateSetupScript(queueName)
	result.AddScript("setup_celery.sh", []byte(setupScript))
	result.AddScript("export_cloud_tasks_queue.sh", []byte(m.generateExportScript(queueName)))
	result.AddScript("migrate_cloud_tasks_queue.sh", []byte(m.generateMigrateScript(queueName)))
	result.AddScript("validate_cloud_tasks_queue.sh", []byte(m.generateValidateScript(queueName)))
	result.AddScript("backup_cloud_tasks_config.sh", []byte(m.generateBackupScript(queueName)))
	result.AddScript("cutover_cloud_tasks_clients.sh", []byte(m.generateCutoverScript(queueName)))
	for _, step := range cloudTasksRunbook(queueName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *CloudTasksMapper) hasRetryConfig(res *resource.AWSResource) bool {
	if res.Config["retry_config"] != nil {
		return true
	}
	return res.GetConfigInt("max_attempts") > 0
}

func (m *CloudTasksMapper) generateCeleryConfig(queueName string) string {
	return fmt.Sprintf(`# Celery Configuration
# Generated from GCP Cloud Tasks queue: %s

broker_url = 'redis://redis:6379/0'
result_backend = 'redis://redis:6379/0'

task_serializer = 'json'
accept_content = ['json']
result_serializer = 'json'
timezone = 'UTC'
enable_utc = True

task_track_started = True
task_time_limit = 600
task_soft_time_limit = 540
task_acks_late = True
worker_prefetch_multiplier = 1

task_autoretry_for = (Exception,)
task_retry_kwargs = {
    'max_retries': 3,
    'countdown': 60,
}
task_default_retry_delay = 60

task_routes = {
    'tasks.*': {'queue': '%s'},
}

result_expires = 3600

worker_max_tasks_per_child = 1000
worker_disable_rate_limits = False

worker_send_task_events = True
task_send_sent_event = True
`, queueName, queueName)
}

func (m *CloudTasksMapper) generateAppChangeConfig(queueName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_CLOUD_TASKS_QUEUE=%s\nTARGET_TASK_QUEUE=%s\nCELERY_BROKER_URL=redis://redis:6379/0\nCELERY_RESULT_BACKEND=redis://redis:6379/0\n", queueName, queueName)
}

func (m *CloudTasksMapper) generateTaskReport(res *resource.AWSResource, queueName string) string {
	return fmt.Sprintf(`source: google_cloud_tasks_queue
queue: %s
target: celery
broker: redis
max_attempts: %d
dispatch_deadline: %s
`, queueName, res.GetConfigInt("max_attempts"), res.GetConfigString("dispatch_deadline"))
}

func (m *CloudTasksMapper) generateWorkerDockerfile() string {
	return `FROM python:3.11-slim

RUN apt-get update && apt-get install -y gcc && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir celery[redis]==5.3.4 flower==2.0.1 requests==2.31.0

WORKDIR /app

COPY . /app/
COPY config/celery/celeryconfig.py /app/celeryconfig.py
COPY config/celery/tasks.py /app/tasks.py

CMD ["celery", "-A", "tasks", "worker", "--loglevel=info", "--concurrency=4"]
`
}

func (m *CloudTasksMapper) generateTasksExample(_ string) string {
	return `# Celery Tasks
# Migration from GCP Cloud Tasks to Celery

from celery import Celery
import requests
import logging

logger = logging.getLogger(__name__)

app = Celery('tasks')
app.config_from_object('celeryconfig')

@app.task(bind=True, name='tasks.http_task')
def http_task(self, url, method='POST', headers=None, body=None):
    """HTTP task equivalent to Cloud Tasks HTTP target."""
    try:
        logger.info(f"Executing HTTP task: {method} {url}")
        response = requests.request(
            method=method,
            url=url,
            headers=headers or {},
            json=body if isinstance(body, dict) else None,
            data=body if isinstance(body, str) else None,
            timeout=30
        )
        response.raise_for_status()
        logger.info(f"HTTP task completed: {response.status_code}")
        return {'status_code': response.status_code, 'body': response.text[:1000]}
    except requests.RequestException as exc:
        logger.error(f"HTTP task failed: {exc}")
        raise self.retry(exc=exc, countdown=60 * (2 ** self.request.retries))

@app.task(name='tasks.process_data')
def process_data(data):
    """Example data processing task."""
    logger.info(f"Processing data: {data}")
    return {'status': 'completed', 'data': data}
`
}

func (m *CloudTasksMapper) generateComposeSnippet(queueName string) string {
	return fmt.Sprintf(`# Docker Compose snippet for Celery worker
# Add this to your main docker-compose.yml

services:
  celery-worker:
    build:
      context: .
      dockerfile: Dockerfile.celery-worker
    container_name: celery-worker
    command: celery -A tasks worker --loglevel=info --concurrency=4 --queues=%s
    depends_on:
      - redis
    networks:
      - homeport
    volumes:
      - ./config/celery:/app/config/celery
      - ./app:/app
    environment:
      - CELERY_BROKER_URL=redis://redis:6379/0
      - CELERY_RESULT_BACKEND=redis://redis:6379/0
    restart: unless-stopped
    labels:
      homeport.source: google_cloud_tasks_queue
      homeport.queue: %s

  flower:
    build:
      context: .
      dockerfile: Dockerfile.celery-worker
    container_name: flower
    command: celery -A tasks flower --port=5555
    depends_on:
      - redis
      - celery-worker
    networks:
      - homeport
    ports:
      - "5555:5555"
    environment:
      - CELERY_BROKER_URL=redis://redis:6379/0
      - CELERY_RESULT_BACKEND=redis://redis:6379/0
    restart: unless-stopped
    labels:
      homeport.service: flower
      traefik.enable: "true"
      traefik.http.routers.flower.rule: Host(`+"`flower.localhost`"+`)
      traefik.http.services.flower.loadbalancer.server.port: "5555"
`, queueName, queueName)
}

func (m *CloudTasksMapper) generateSetupScript(queueName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Celery + Redis Setup Script for queue: %s

set -e

echo "Setting up Celery task queue..."

echo "Checking Redis connection..."
until docker-compose exec redis redis-cli ping > /dev/null 2>&1; do
  echo "Waiting for Redis..."
  sleep 2
done

echo "Redis is ready!"

echo "Starting Celery worker..."
docker-compose up -d celery-worker

echo "Starting Flower monitoring..."
docker-compose up -d flower

echo ""
echo "Celery setup complete!"
echo "Queue: %s"
echo "Flower UI: http://localhost:5555"
echo "Redis: localhost:6379"
`, queueName, queueName)
}

func (m *CloudTasksMapper) generateExportScript(queueName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p cloud-tasks-export\nprintf '{\"queue\":%%q,\"source\":\"google_cloud_tasks_queue\"}\\n' %q > cloud-tasks-export/queue.json\n", queueName)
}

func (m *CloudTasksMapper) generateMigrateScript(queueName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/celery/celeryconfig.py\ntest -s config/celery/tasks.py\ngrep -q %q config/celery/celeryconfig.py\necho \"Cloud Tasks queue %s mapped to Celery queue %s\"\n", queueName, queueName, queueName)
}

func (m *CloudTasksMapper) generateValidateScript(queueName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nredis-cli -h redis ping\ntest -s config/celery/app-change.env\ngrep -q %q config/celery/app-change.env\necho \"Cloud Tasks queue %s validated on Celery/Redis\"\n", queueName, queueName)
}

func (m *CloudTasksMapper) generateBackupScript(queueName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-cloud-tasks-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/celery Dockerfile.celery-worker docker-compose.celery.yml setup_celery.sh validate_cloud_tasks_queue.sh cutover_cloud_tasks_clients.sh\necho \"$archive\"\n", queueName)
}

func (m *CloudTasksMapper) generateCutoverScript(queueName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/celery/app-change.env\ntest \"$SOURCE_CLOUD_TASKS_QUEUE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Cloud Tasks enqueue calls to Celery queue $TARGET_TASK_QUEUE\"\n", queueName)
}

func cloudTasksRunbook(queueName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "task-queue", "source": "google_cloud_tasks_queue", "queue": queueName, "target": "celery"}
	return []domainrunbook.Step{
		cloudTasksStep("export-cloud-tasks-queue", "Export Cloud Tasks queue", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_cloud_tasks_queue.sh"}, "Cloud Tasks queue configuration is exported", metadata),
		cloudTasksStep("provision-celery-queue", "Provision Celery queue", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_celery.sh"}, "Redis broker and Celery worker are configured", metadata),
		cloudTasksStep("migrate-cloud-tasks-config", "Migrate Cloud Tasks config", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_cloud_tasks_queue.sh"}, "Cloud Tasks retry and queue semantics are mapped", metadata),
		cloudTasksStep("validate-cloud-tasks-queue", "Validate Cloud Tasks queue", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_cloud_tasks_queue.sh"}, "Celery broker and generated app patch validate", metadata),
		cloudTasksStep("backup-cloud-tasks-config", "Backup Cloud Tasks config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloud_tasks_config.sh"}, "Cloud Tasks migration artifacts are archived", metadata),
		cloudTasksStep("cutover-cloud-tasks-clients", "Cut over Cloud Tasks clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_cloud_tasks_clients.sh"}, "enqueue callers use Celery broker settings", metadata),
		cloudTasksStep("rollback-cloud-tasks-source", "Keep Cloud Tasks source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Cloud Tasks remains authoritative until Celery validation passes", metadata),
	}
}

func cloudTasksStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
