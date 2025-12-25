// Package messaging provides mappers for GCP messaging services.
package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":     "google_cloud_tasks_queue",
		"cloudexit.queue_name": queueName,
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
		result.AddManualStep("Configure Celery task retry settings: autoretry_for, retry_kwargs, max_retries")
	}

	if dispatchDeadline := res.GetConfigString("dispatch_deadline"); dispatchDeadline != "" {
		result.AddWarning(fmt.Sprintf("Dispatch deadline: %s. Configure task time limits in Celery.", dispatchDeadline))
	}

	celeryConfig := m.generateCeleryConfig(queueName)
	result.AddConfig("config/celery/celeryconfig.py", []byte(celeryConfig))

	workerDockerfile := m.generateWorkerDockerfile()
	result.AddConfig("Dockerfile.celery-worker", []byte(workerDockerfile))

	tasksExample := m.generateTasksExample(queueName)
	result.AddConfig("config/celery/tasks.py", []byte(tasksExample))

	composeSnippet := m.generateComposeSnippet(queueName)
	result.AddConfig("docker-compose.celery.yml", []byte(composeSnippet))

	setupScript := m.generateSetupScript(queueName)
	result.AddScript("setup_celery.sh", []byte(setupScript))

	result.AddManualStep("Add Celery worker service to your docker-compose.yml using the generated snippet")
	result.AddManualStep("Install Celery in your application: pip install celery[redis]")
	result.AddManualStep("Convert Cloud Tasks enqueue calls to Celery task.apply_async() calls")
	result.AddManualStep("Consider installing Flower for monitoring: pip install flower")

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

func (m *CloudTasksMapper) generateTasksExample(queueName string) string {
	return fmt.Sprintf(`# Celery Tasks
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
`)
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
      - cloudexit
    volumes:
      - ./config/celery:/app/config/celery
      - ./app:/app
    environment:
      - CELERY_BROKER_URL=redis://redis:6379/0
      - CELERY_RESULT_BACKEND=redis://redis:6379/0
    restart: unless-stopped
    labels:
      cloudexit.source: google_cloud_tasks_queue
      cloudexit.queue: %s

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
      - cloudexit
    ports:
      - "5555:5555"
    environment:
      - CELERY_BROKER_URL=redis://redis:6379/0
      - CELERY_RESULT_BACKEND=redis://redis:6379/0
    restart: unless-stopped
    labels:
      cloudexit.service: flower
      traefik.enable: "true"
      traefik.http.routers.flower.rule: Host(` + "`flower.localhost`" + `)
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
