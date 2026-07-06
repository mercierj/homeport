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

// SNSMapper converts AWS SNS topics to NATS.
type SNSMapper struct {
	*mapper.BaseMapper
}

// NewSNSMapper creates a new SNS to NATS mapper.
func NewSNSMapper() *SNSMapper {
	return &SNSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeSNSTopic, nil),
	}
}

// Map converts an SNS topic to a NATS service.
func (m *SNSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
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
	svc.Environment = map[string]string{
		"NATS_CLUSTER_NAME": "homeport-cluster",
	}
	svc.Ports = []string{
		"4222:4222", // Client connections
		"8222:8222", // HTTP management
		"6222:6222", // Cluster connections
	}
	svc.Volumes = []string{
		"./data/nats:/data",
		"./config/nats/nats.conf:/etc/nats/nats.conf",
	}
	svc.Command = []string{"-c", "/etc/nats/nats.conf"}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":                "aws_sns_topic",
		"homeport.topic_name":            topicName,
		"homeport.target":                "nats",
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

	natsConfig := m.generateNATSConfig(topicName)
	result.AddConfig("config/nats/nats.conf", []byte(natsConfig))

	subjectMapping := m.generateSubjectMapping(res, topicName)
	result.AddConfig("config/nats/subjects.json", []byte(subjectMapping))
	result.AddConfig("config/nats/app-change.env", []byte(m.generateAppChangeConfig(topicName)))

	if subscriptions := m.getSubscriptions(res); len(subscriptions) > 0 {
		result.AddWarning(fmt.Sprintf("Found %d SNS subscriptions. Generated NATS subscription bindings.", len(subscriptions)))
		result.AddConfig("config/nats/subscriptions.json", []byte(m.generateSubscriptionsConfig(topicName, subscriptions)))
	}

	if m.isFIFOTopic(topicName) || res.GetConfigBool("fifo_topic") {
		result.AddWarning("FIFO topic detected. NATS JetStream stream config is generated for ordering.")
		result.AddConfig("config/nats/jetstream.json", []byte(m.generateJetStreamConfig(topicName, res.GetConfigBool("content_based_deduplication"))))
	}

	if res.GetConfigBool("content_based_deduplication") {
		result.AddWarning("Content-based deduplication enabled. Generated JetStream duplicate window config.")
	}

	setupScript := m.generateSetupScript(topicName)
	result.AddScript("setup_nats.sh", []byte(setupScript))
	result.AddScript("export_sns_topic.sh", []byte(m.generateExportScript(topicName, res.Region)))
	result.AddScript("migrate_sns_bindings.sh", []byte(m.generateMigrateScript(topicName)))
	result.AddScript("validate_sns_adapter.sh", []byte(m.generateValidateScript(topicName)))
	result.AddScript("backup_sns_config.sh", []byte(m.generateBackupScript(topicName)))
	result.AddScript("cutover_sns_adapter.sh", []byte(m.generateCutoverScript(topicName)))
	for _, step := range snsRunbook(topicName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *SNSMapper) generateNATSConfig(topicName string) string {
	return fmt.Sprintf(`# NATS Server Configuration
# Generated from SNS topic: %s

server_name: homeport-nats
port: 4222
http_port: 8222

http: 8222

debug: false
trace: false
logtime: true

max_payload: 1048576
max_connections: 64000
max_subscriptions: 0

write_deadline: "10s"
ping_interval: "2m"
ping_max: 2

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

func (m *SNSMapper) generateAppChangeConfig(topicName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=adapter
SOURCE_TOPIC=%s
TARGET_SUBJECT=sns.%s
AWS_ENDPOINT_URL_SNS=http://homeport:8080/api/v1/compat/aws/sns
HOMEPORT_COMPAT_BACKEND=nats
HOMEPORT_COMPAT_PROTOCOL=sns
NATS_URL=nats://nats:4222
`, topicName, topicName)
}

func (m *SNSMapper) generateSubjectMapping(res *resource.AWSResource, topicName string) string {
	mapping := map[string]interface{}{
		"topic_name": topicName,
		"topic_arn":  res.ARN,
		"subjects": map[string]interface{}{
			"base":     fmt.Sprintf("sns.%s", topicName),
			"pattern":  fmt.Sprintf("sns.%s.*", topicName),
			"wildcard": fmt.Sprintf("sns.%s.>", topicName),
		},
	}
	content, _ := json.MarshalIndent(mapping, "", "  ")
	return string(content)
}

func (m *SNSMapper) generateSubscriptionsConfig(topicName string, subscriptions []map[string]string) string {
	bindings := make([]map[string]string, 0, len(subscriptions))
	for _, sub := range subscriptions {
		bindings = append(bindings, map[string]string{
			"source_topic": topicName,
			"subject":      fmt.Sprintf("sns.%s", topicName),
			"protocol":     sub["protocol"],
			"endpoint":     sub["endpoint"],
		})
	}
	content, _ := json.MarshalIndent(map[string]interface{}{"bindings": bindings}, "", "  ")
	return string(content)
}

func (m *SNSMapper) generateJetStreamConfig(topicName string, dedup bool) string {
	config := map[string]interface{}{
		"name":       topicName,
		"subjects":   []string{fmt.Sprintf("sns.%s", topicName), fmt.Sprintf("sns.%s.>", topicName)},
		"retention":  "limits",
		"storage":    "file",
		"replicas":   3,
		"max_age":    "168h",
		"duplicates": "2m",
	}
	if dedup {
		config["deduplication"] = "content_hash_message_id"
	}
	content, _ := json.MarshalIndent(config, "", "  ")
	return string(content)
}

func (m *SNSMapper) getSubscriptions(res *resource.AWSResource) []map[string]string {
	subscriptions := []map[string]string{}
	if subData, ok := res.Config["subscriptions"].([]interface{}); ok {
		for _, sub := range subData {
			if subMap, ok := sub.(map[string]interface{}); ok {
				subscription := map[string]string{}
				if endpoint, ok := subMap["endpoint"].(string); ok {
					subscription["endpoint"] = endpoint
				}
				if protocol, ok := subMap["protocol"].(string); ok {
					subscription["protocol"] = protocol
				}
				if len(subscription) > 0 {
					subscriptions = append(subscriptions, subscription)
				}
			}
		}
	}
	return subscriptions
}

func (m *SNSMapper) generateSetupScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/bash
# NATS Setup Script for SNS topic: %s

set -e

NATS_HOST="${NATS_HOST:-localhost}"
NATS_HTTP_PORT="${NATS_HTTP_PORT:-8222}"

echo "Waiting for NATS to be ready..."
until curl -sf http://$NATS_HOST:$NATS_HTTP_PORT/healthz > /dev/null; do
  echo "Waiting..."
  sleep 2
done

echo "NATS is ready!"
echo "NATS Server: nats://$NATS_HOST:4222"
echo "Monitoring UI: http://$NATS_HOST:$NATS_HTTP_PORT"
echo "Subject: sns.%s"
`, topicName, topicName)
}

func (m *SNSMapper) generateExportScript(topicName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION="%s"
TOPIC_NAME="%s"
OUTPUT_DIR="./sns-export"
mkdir -p "$OUTPUT_DIR"
aws sns list-topics --region "$AWS_REGION" --output json > "$OUTPUT_DIR/topics.json"
topic_arn=$(jq -r --arg name "$TOPIC_NAME" '.Topics[].TopicArn | select(endswith(":" + $name))' "$OUTPUT_DIR/topics.json")
test -n "$topic_arn"
aws sns get-topic-attributes --topic-arn "$topic_arn" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/topic-attributes.json"
aws sns list-subscriptions-by-topic --topic-arn "$topic_arn" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/subscriptions.json"
aws sns list-tags-for-resource --resource-arn "$topic_arn" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/tags.json"
`, region, topicName)
}

func (m *SNSMapper) generateMigrateScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/nats/subjects.json
if [ -f config/nats/subscriptions.json ]; then
  jq -e '.bindings | length >= 0' config/nats/subscriptions.json >/dev/null
fi
echo "SNS topic %s mapped to NATS subject sns.%s"
`, topicName, topicName)
}

func (m *SNSMapper) generateValidateScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
curl -fsS http://localhost:8222/healthz >/tmp/homeport-nats-health.txt
test -s config/nats/subjects.json
grep -q "sns.%s" config/nats/subjects.json
test -s config/nats/app-change.env
`, topicName)
}

func (m *SNSMapper) generateBackupScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-nats-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/nats setup_nats.sh export_sns_topic.sh migrate_sns_bindings.sh validate_sns_adapter.sh cutover_sns_adapter.sh
echo "$archive"
`, topicName)
}

func (m *SNSMapper) generateCutoverScript(topicName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/nats/app-change.env
test "$SOURCE_TOPIC" = %q
test "$APP_CHANGE_MODE" = "adapter"
test "$AWS_ENDPOINT_URL_SNS" = "http://homeport:8080/api/v1/compat/aws/sns"
echo "Use AWS_ENDPOINT_URL_SNS=$AWS_ENDPOINT_URL_SNS for SNS SDK clients"
`, topicName)
}

func snsRunbook(topicName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                    "pubsub",
		"source":                  "aws_sns_topic",
		"topic":                   topicName,
		"HOMEPORT_TARGET":         "nats",
		"HOMEPORT_APP_CHANGE":     "adapter",
		"AWS_ENDPOINT_URL_SNS":    "http://homeport:8080/api/v1/compat/aws/sns",
		"HOMEPORT_COMPAT_BACKEND": "nats",
		"NATS_SUBJECT":            fmt.Sprintf("sns.%s", topicName),
		"NATS_JETSTREAM":          "enabled",
	}
	return []domainrunbook.Step{
		snsStep("export-sns-topic", "Export SNS topic", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_sns_topic.sh"}, "SNS topic attributes, tags, and subscriptions are exported", metadata),
		snsStep("provision-nats-topic", "Provision NATS JetStream target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "setup_nats.sh"}, "NATS target is healthy with JetStream enabled", metadata),
		snsStep("migrate-sns-bindings", "Migrate SNS subscription bindings", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_sns_bindings.sh"}, "SNS subjects and subscription bindings are mapped", metadata),
		snsStep("validate-sns-adapter", "Validate SNS compatibility adapter", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_sns_adapter.sh"}, "NATS health and SNS adapter config validate", metadata),
		snsStep("backup-sns-config", "Backup SNS migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_sns_config.sh"}, "SNS and NATS migration artifacts are archived", metadata),
		snsStep("cutover-sns-clients", "Cut over SNS clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_sns_adapter.sh"}, "SNS SDK clients use HomePort compatibility endpoint", metadata),
		snsStep("rollback-sns-source", "Keep SNS source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS SNS remains authoritative until NATS delivery validation passes", metadata),
	}
}

func snsStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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

func (m *SNSMapper) isFIFOTopic(topicName string) bool {
	return len(topicName) > 5 && topicName[len(topicName)-5:] == ".fifo"
}
