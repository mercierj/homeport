// Package messaging provides mappers for AWS messaging services.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
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
		"NATS_CLUSTER_NAME": "cloudexit-cluster",
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
	svc.Networks = []string{"cloudexit"}
	svc.Labels = map[string]string{
		"cloudexit.source":                                        "aws_sns_topic",
		"cloudexit.topic_name":                                    topicName,
		"traefik.enable":                                          "true",
		"traefik.http.routers.nats.rule":                          "Host(`nats.localhost`)",
		"traefik.http.services.nats.loadbalancer.server.port":     "8222",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8222/healthz"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	natsConfig := m.generateNATSConfig(topicName)
	result.AddConfig("config/nats/nats.conf", []byte(natsConfig))

	subjectMapping := m.generateSubjectMapping(res, topicName)
	result.AddConfig("config/nats/subjects.json", []byte(subjectMapping))

	if subscriptions := m.getSubscriptions(res); len(subscriptions) > 0 {
		result.AddWarning(fmt.Sprintf("Found %d SNS subscriptions. Configure NATS subscribers for these endpoints.", len(subscriptions)))
		for _, sub := range subscriptions {
			result.AddManualStep(fmt.Sprintf("Create NATS subscriber for: %s (protocol: %s)", sub["endpoint"], sub["protocol"]))
		}
	}

	if m.isFIFOTopic(topicName) || res.GetConfigBool("fifo_topic") {
		result.AddWarning("FIFO topic detected. NATS JetStream can provide message ordering. Consider enabling JetStream.")
		result.AddManualStep("Enable NATS JetStream for FIFO ordering: uncomment JetStream section in nats.conf")
	}

	if res.GetConfigBool("content_based_deduplication") {
		result.AddWarning("Content-based deduplication enabled. Implement deduplication logic in NATS subscribers.")
		result.AddManualStep("Implement message deduplication logic in your NATS subscribers")
	}

	setupScript := m.generateSetupScript(topicName)
	result.AddScript("setup_nats.sh", []byte(setupScript))

	result.AddManualStep("Access NATS monitoring dashboard at http://localhost:8222")
	result.AddManualStep("Update application code to use NATS client instead of SNS SDK")
	result.AddManualStep("Consider enabling NATS JetStream for persistent messaging")

	return result, nil
}

func (m *SNSMapper) generateNATSConfig(topicName string) string {
	return fmt.Sprintf(`# NATS Server Configuration
# Generated from SNS topic: %s

server_name: cloudexit-nats
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

# JetStream (enable for FIFO topics and persistence)
# jetstream {
#   store_dir: "/data/jetstream"
#   max_memory_store: 1GB
#   max_file_store: 10GB
# }

cluster {
  name: cloudexit-cluster
  port: 6222
}
`, topicName)
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

func (m *SNSMapper) isFIFOTopic(topicName string) bool {
	return len(topicName) > 5 && topicName[len(topicName)-5:] == ".fifo"
}
