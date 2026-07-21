// Package messaging provides mappers for GCP messaging services.
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

// PubSubSubscriptionMapper converts GCP Pub/Sub subscriptions to NATS JetStream consumers.
type PubSubSubscriptionMapper struct {
	*mapper.BaseMapper
}

// NewPubSubSubscriptionMapper creates a new Pub/Sub subscription to NATS JetStream mapper.
func NewPubSubSubscriptionMapper() *PubSubSubscriptionMapper {
	return &PubSubSubscriptionMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypePubSubSubscription, nil),
	}
}

// Map converts a Pub/Sub subscription to a NATS JetStream consumer.
func (m *PubSubSubscriptionMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	subscriptionName := res.GetConfigString("name")
	if subscriptionName == "" {
		subscriptionName = res.Name
	}
	topicName := res.GetConfigString("topic")
	ackDeadlineSec := res.GetConfigInt("ack_deadline_seconds")
	if ackDeadlineSec == 0 {
		ackDeadlineSec = 10
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
		"homeport.source":                "google_pubsub_subscription",
		"homeport.subscription_name":     subscriptionName,
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
	result.AddConfig("config/nats/pubsub-consumer.json", []byte(m.generateConsumerConfig(res, subscriptionName, topicName, ackDeadlineSec)))
	result.AddConfig("config/pubsub/app-change.env", []byte(m.generateAppChangeConfig(subscriptionName, topicName)))
	result.AddScript("scripts/migrate-pubsub-subscription.sh", []byte(m.generateMigrationScript(res, subscriptionName, topicName)))
	result.AddScript("scripts/validate-pubsub-subscription.sh", []byte(m.generateValidateScript(subscriptionName)))
	result.AddScript("scripts/backup-pubsub-subscription.sh", []byte(m.generateBackupScript(subscriptionName)))
	result.AddScript("scripts/cutover-pubsub-subscription.sh", []byte(m.generateCutoverScript(subscriptionName)))
	for _, step := range pubSubSubscriptionRunbook(subscriptionName) {
		result.AddRunbookStep(step)
	}
	m.addMigrationWarnings(result, res)

	return result, nil
}

func (m *PubSubSubscriptionMapper) generateConsumerConfig(res *resource.AWSResource, subscriptionName, topicName string, ackDeadlineSec int) string {
	config := map[string]interface{}{
		"stream_name":     pubSubStreamName(topicName),
		"durable_name":    pubSubStreamName(subscriptionName),
		"filter_subject":  fmt.Sprintf("pubsub.%s.>", topicName),
		"deliver_policy":  "all",
		"ack_policy":      "explicit",
		"ack_wait":        fmt.Sprintf("%ds", ackDeadlineSec),
		"max_deliver":     5,
		"replay_policy":   "instant",
		"source_resource": "google_pubsub_subscription",
	}
	if filter := res.GetConfigString("filter"); filter != "" {
		config["pubsub_filter"] = filter
	}
	content, _ := json.MarshalIndent(config, "", "  ")
	return string(content)
}

func (m *PubSubSubscriptionMapper) generateMigrationScript(res *resource.AWSResource, subscriptionName, topicName string) string {
	project := res.GetConfigString("project")
	if project == "" {
		project = "<YOUR_PROJECT_ID>"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
mkdir -p ./pubsub-export
gcloud pubsub subscriptions describe %s --project=%s --format=json > ./pubsub-export/subscription-config.json
test -s config/nats/pubsub-consumer.json
echo "Pub/Sub subscription %s mapped to NATS JetStream consumer %s on pubsub.%s.>"
`, subscriptionName, project, subscriptionName, pubSubStreamName(subscriptionName), topicName)
}

func (m *PubSubSubscriptionMapper) generateAppChangeConfig(subscriptionName, topicName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=adapter
SOURCE_PUBSUB_SUBSCRIPTION=%s
SOURCE_PUBSUB_TOPIC=%s
PUBSUB_EMULATOR_HOST=http://homeport:8080/api/v1/compat/gcp/pub-sub
HOMEPORT_COMPAT_BACKEND=nats-jetstream
HOMEPORT_COMPAT_PROTOCOL=gcp-pubsub
NATS_URL=nats://nats:4222
NATS_SUBJECT=pubsub.%s.>
NATS_CONSUMER=%s
`, subscriptionName, topicName, topicName, pubSubStreamName(subscriptionName))
}

func (m *PubSubSubscriptionMapper) generateValidateScript(subscriptionName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
curl -fsS http://localhost:8222/healthz >/tmp/homeport-nats-pubsub-subscription-health.txt
test -s config/nats/nats.conf
test -s config/nats/pubsub-consumer.json
grep -q %q config/nats/pubsub-consumer.json
test -s config/pubsub/app-change.env
grep -q "HOMEPORT_COMPAT_BACKEND=nats-jetstream" config/pubsub/app-change.env
`, pubSubStreamName(subscriptionName))
}

func (m *PubSubSubscriptionMapper) generateBackupScript(subscriptionName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/pubsub-subscription-%s-nats-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/nats config/pubsub scripts/migrate-pubsub-subscription.sh scripts/validate-pubsub-subscription.sh scripts/cutover-pubsub-subscription.sh pubsub-export
echo "$archive"
`, subscriptionName)
}

func (m *PubSubSubscriptionMapper) generateCutoverScript(subscriptionName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/pubsub/app-change.env
test "$SOURCE_PUBSUB_SUBSCRIPTION" = %q
test "$APP_CHANGE_MODE" = "adapter"
test "$HOMEPORT_COMPAT_BACKEND" = "nats-jetstream"
echo "Use PUBSUB_EMULATOR_HOST=$PUBSUB_EMULATOR_HOST for Pub/Sub subscription clients"
`, subscriptionName)
}

func pubSubSubscriptionRunbook(subscriptionName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                    "messaging",
		"source":                  "google_pubsub_subscription",
		"subscription":            subscriptionName,
		"target":                  "nats-jetstream",
		"HOMEPORT_COMPAT_BACKEND": "nats-jetstream",
		"PUBSUB_EMULATOR_HOST":    "http://homeport:8080/api/v1/compat/gcp/pub-sub",
		"NATS_CONSUMER":           pubSubStreamName(subscriptionName),
	}
	return []domainrunbook.Step{
		pubSubSubscriptionStep("export-pubsub-subscription", "Export Pub/Sub subscription", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "scripts/migrate-pubsub-subscription.sh"}, "subscription config is exported and consumer mapping exists", metadata),
		pubSubSubscriptionStep("validate-pubsub-subscription-adapter", "Validate Pub/Sub subscription adapter", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "scripts/validate-pubsub-subscription.sh"}, "NATS health and Pub/Sub subscription adapter config validate", metadata),
		pubSubSubscriptionStep("backup-pubsub-subscription", "Backup Pub/Sub subscription config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "scripts/backup-pubsub-subscription.sh"}, "Pub/Sub subscription and NATS migration artifacts are archived", metadata),
		pubSubSubscriptionStep("cutover-pubsub-subscription", "Cut over Pub/Sub subscription clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "scripts/cutover-pubsub-subscription.sh"}, "Pub/Sub clients use HomePort compatibility endpoint", metadata),
		pubSubSubscriptionStep("rollback-pubsub-subscription-source", "Keep Pub/Sub subscription source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Pub/Sub subscription remains authoritative until cutover passes", metadata),
	}
}

func pubSubSubscriptionStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func (m *PubSubSubscriptionMapper) addMigrationWarnings(result *mapper.MappingResult, res *resource.AWSResource) {
	if pushEndpoint := res.GetConfigString("push_endpoint"); pushEndpoint != "" {
		result.AddWarning(fmt.Sprintf("Push subscription to %s detected. Keep webhook delivery behind the Pub/Sub compatibility adapter.", pushEndpoint))
	}
	if res.GetConfigBool("enable_message_ordering") {
		result.AddWarning("Message ordering enabled. Generated JetStream consumer uses explicit ack policy.")
	}
	if res.GetConfigBool("enable_exactly_once_delivery") {
		result.AddWarning("Exactly-once delivery enabled. JetStream consumer config is durable, but provider parity still needs contract tests.")
	}
	if filter := res.GetConfigString("filter"); filter != "" {
		result.AddWarning(fmt.Sprintf("Subscription filter captured for adapter-side evaluation: %s", filter))
	}
	if res.Config["retry_policy"] != nil {
		result.AddWarning("Retry policy configured. Generated JetStream max_deliver seed requires provider contract validation.")
	}
	if deadLetterTopic := res.GetConfigString("dead_letter_topic"); deadLetterTopic != "" {
		result.AddWarning(fmt.Sprintf("Dead letter topic %s captured; durable delivery parity remains a backend contract gap.", deadLetterTopic))
	}
	result.AddVolume(mapper.Volume{Name: "nats-data", Driver: "local"})
}
