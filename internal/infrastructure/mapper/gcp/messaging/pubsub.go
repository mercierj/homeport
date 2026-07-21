// Package messaging provides mappers for GCP messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// PubSubMapper converts GCP Pub/Sub topics to NATS JetStream.
type PubSubMapper struct {
	*mapper.BaseMapper
}

// NewPubSubMapper creates a new Pub/Sub to NATS JetStream mapper.
func NewPubSubMapper() *PubSubMapper {
	return &PubSubMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypePubSubTopic, nil),
	}
}

// Map converts a Pub/Sub topic to a NATS JetStream service.
func (m *PubSubMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	topicName := res.GetConfigString("name")
	if topicName == "" {
		topicName = res.Name
	}

	result := mapper.NewMappingResult("nats")
	svc := result.DockerService
	svc.Image = "nats:2.10-alpine"
	svc.Environment = map[string]string{"NATS_CLUSTER_NAME": "homeport-cluster"}
	svc.Ports = []string{"4222:4222", "8222:8222", "6222:6222"}
	svc.Volumes = []string{"./data/nats:/data", "./config/nats/nats.conf:/etc/nats/nats.conf"}
	svc.Command = []string{"-c", "/etc/nats/nats.conf"}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":                "google_pubsub_topic",
		"homeport.topic_name":            topicName,
		"homeport.target":                "nats-jetstream",
		"traefik.enable":                 "true",
		"traefik.http.routers.nats.rule": "Host(`nats.localhost`)",
		"traefik.http.services.nats.loadbalancer.server.port": "8222",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8222/healthz"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Deploy = &mapper.DeployConfig{Replicas: 3}
	svc.Restart = "unless-stopped"

	result.AddConfig("config/nats/nats.conf", []byte(generatePubSubNATSConfig(topicName)))
	result.AddConfig("config/nats/pubsub-stream.json", []byte(m.generateJetStreamConfig(res, topicName)))
	result.AddConfig("config/pubsub/app-change.env", []byte(m.generateAppChangeConfig(topicName)))

	if res.GetConfigBool("message_ordering_enabled") {
		result.AddWarning("Message ordering enabled. Generated JetStream stream uses file storage and replicas for ordered durable delivery.")
	}
	if deadLetterTopic := res.GetConfigString("dead_letter_topic"); deadLetterTopic != "" {
		result.AddWarning(fmt.Sprintf("Dead letter topic configured: %s", deadLetterTopic))
	}

	result.AddScript("setup_nats_pubsub.sh", []byte(m.generateSetupScript(topicName)))
	result.AddScript("validate_pubsub_adapter.sh", []byte(m.generateValidateScript(topicName)))
	result.AddScript("backup_pubsub_nats.sh", []byte(m.generateBackupScript(topicName)))
	result.AddScript("cutover_pubsub_adapter.sh", []byte(m.generateCutoverScript(topicName)))
	for _, step := range pubSubRunbook(topicName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func generatePubSubNATSConfig(topicName string) string {
	return fmt.Sprintf(`# NATS Server Configuration
# Generated from GCP Pub/Sub topic: %s

server_name: homeport-nats-pubsub
port: 4222
http_port: 8222
http: 8222
store_dir: "/data"

jetstream {
  store_dir: "/data/jetstream"
  max_memory_store: 1GB
  max_file_store: 10GB
}

cluster {
  name: homeport-cluster
  port: 6222
}
`, topicName)
}

func (m *PubSubMapper) generateJetStreamConfig(res *resource.AWSResource, topicName string) string {
	config := map[string]interface{}{
		"name":      pubSubStreamName(topicName),
		"subjects":  []string{fmt.Sprintf("pubsub.%s", topicName), fmt.Sprintf("pubsub.%s.>", topicName)},
		"retention": "limits",
		"storage":   "file",
		"replicas":  3,
		"max_age":   "168h",
	}
	if res.GetConfigBool("message_ordering_enabled") {
		config["ordered_delivery"] = true
	}
	content, _ := json.MarshalIndent(config, "", "  ")
	return string(content)
}

func (m *PubSubMapper) generateSetupScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/bash
# NATS JetStream Setup Script for Pub/Sub topic: %s

set -e

NATS_HOST="${NATS_HOST:-localhost}"
NATS_HTTP_PORT="${NATS_HTTP_PORT:-8222}"

echo "Waiting for NATS to be ready..."
until curl -sf http://$NATS_HOST:$NATS_HTTP_PORT/healthz > /dev/null; do
  echo "Waiting..."
  sleep 2
done

test -s config/nats/pubsub-stream.json
echo "NATS is ready for Pub/Sub topic %s"
`, topicName, topicName)
}

func (m *PubSubMapper) generateAppChangeConfig(topicName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=adapter
SOURCE_PUBSUB_TOPIC=%s
PUBSUB_EMULATOR_HOST=http://homeport:8080/api/v1/compat/gcp/pub-sub
HOMEPORT_COMPAT_BACKEND=nats-jetstream
HOMEPORT_COMPAT_PROTOCOL=gcp-pubsub
NATS_URL=nats://nats:4222
NATS_SUBJECT=pubsub.%s
`, topicName, topicName)
}

func (m *PubSubMapper) generateValidateScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
curl -fsS http://localhost:8222/healthz >/tmp/homeport-nats-pubsub-health.txt
test -s config/nats/nats.conf
test -s config/nats/pubsub-stream.json
grep -q "pubsub.%s" config/nats/pubsub-stream.json
test -s config/pubsub/app-change.env
grep -q "HOMEPORT_COMPAT_BACKEND=nats-jetstream" config/pubsub/app-change.env
`, topicName)
}

func (m *PubSubMapper) generateBackupScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/pubsub-%s-nats-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/nats config/pubsub setup_nats_pubsub.sh validate_pubsub_adapter.sh cutover_pubsub_adapter.sh
echo "$archive"
`, topicName)
}

func (m *PubSubMapper) generateCutoverScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/pubsub/app-change.env
test "$SOURCE_PUBSUB_TOPIC" = %q
test "$APP_CHANGE_MODE" = "adapter"
test "$HOMEPORT_COMPAT_BACKEND" = "nats-jetstream"
echo "Use PUBSUB_EMULATOR_HOST=$PUBSUB_EMULATOR_HOST for Pub/Sub clients"
`, topicName)
}

func pubSubRunbook(topicName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                    "messaging",
		"source":                  "google_pubsub_topic",
		"topic":                   topicName,
		"target":                  "nats-jetstream",
		"HOMEPORT_COMPAT_BACKEND": "nats-jetstream",
		"PUBSUB_EMULATOR_HOST":    "http://homeport:8080/api/v1/compat/gcp/pub-sub",
		"NATS_SUBJECT":            fmt.Sprintf("pubsub.%s", topicName),
	}
	return []domainrunbook.Step{
		pubSubStep("provision-pubsub-nats", "Provision NATS JetStream Pub/Sub target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_nats_pubsub.sh"}, "NATS target is healthy with JetStream config present", metadata),
		pubSubStep("validate-pubsub-adapter", "Validate Pub/Sub compatibility adapter", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_pubsub_adapter.sh"}, "NATS health and Pub/Sub adapter config validate", metadata),
		pubSubStep("backup-pubsub-nats", "Backup Pub/Sub NATS config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_pubsub_nats.sh"}, "Pub/Sub and NATS migration artifacts are archived", metadata),
		pubSubStep("cutover-pubsub-clients", "Cut over Pub/Sub clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_pubsub_adapter.sh"}, "Pub/Sub clients use HomePort compatibility endpoint", metadata),
		pubSubStep("rollback-pubsub-source-authority", "Keep Pub/Sub source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Pub/Sub remains authoritative until cutover passes", metadata),
	}
}

func pubSubStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func pubSubStreamName(topicName string) string {
	name := strings.NewReplacer(".", "_", "-", "_", "/", "_").Replace(topicName)
	return "PUBSUB_" + strings.ToUpper(name)
}
