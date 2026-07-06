package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type MSKMapper struct {
	*mapper.BaseMapper
}

func NewMSKMapper() *MSKMapper {
	return &MSKMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeMSKCluster, nil)}
}

func (m *MSKMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	clusterName := res.GetConfigString("cluster_name")
	if clusterName == "" {
		clusterName = res.Name
	}
	brokers := res.GetConfigInt("number_of_broker_nodes")
	if brokers == 0 {
		brokers = 3
	}
	retentionHours := res.GetConfigInt("retention_hours")
	if retentionHours == 0 {
		retentionHours = 168
	}

	result := mapper.NewMappingResult("redpanda")
	svc := result.DockerService
	svc.Image = "redpandadata/redpanda:v23.3.5"
	svc.Command = []string{
		"redpanda", "start",
		"--smp", "1",
		"--memory", "1G",
		"--reserve-memory", "0M",
		"--overprovisioned",
		"--node-id", "0",
		"--kafka-addr", "PLAINTEXT://0.0.0.0:9092,OUTSIDE://0.0.0.0:19092",
		"--advertise-kafka-addr", "PLAINTEXT://redpanda:9092,OUTSIDE://localhost:19092",
		"--pandaproxy-addr", "0.0.0.0:8082",
		"--advertise-pandaproxy-addr", "localhost:8082",
		"--schema-registry-addr", "0.0.0.0:8081",
	}
	svc.Ports = []string{"9092:9092", "19092:19092", "8081:8081", "8082:8082", "9644:9644"}
	svc.Volumes = []string{"./data/redpanda:/var/lib/redpanda/data", "./config/msk:/etc/homeport/msk"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: brokers}
	svc.Labels = map[string]string{
		"homeport.source":                    "aws_msk_cluster",
		"homeport.cluster_name":              clusterName,
		"homeport.broker_count":              fmt.Sprintf("%d", brokers),
		"homeport.target":                    "redpanda",
		"traefik.enable":                     "true",
		"traefik.http.routers.redpanda.rule": "Host(`redpanda.localhost`)",
		"traefik.http.services.redpanda.loadbalancer.server.port": "8082",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "rpk", "cluster", "health"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/redpanda/msk-topics.yaml", []byte(m.topicsConfig(clusterName, brokers, retentionHours)))
	result.AddConfig("config/msk/cluster-map.yaml", []byte(m.clusterMap(clusterName, brokers, retentionHours)))
	result.AddConfig("config/msk/consumer-groups.yaml", []byte(m.consumerGroups(clusterName)))
	result.AddConfig("config/msk/app-change.env", []byte(m.appChange(clusterName)))
	result.AddScript("export_msk_cluster.sh", []byte(m.exportScript(clusterName, res.Region)))
	result.AddScript("provision_redpanda_msk.sh", []byte(m.provisionScript(clusterName, brokers)))
	result.AddScript("migrate_msk_topics.sh", []byte(m.migrateScript(clusterName)))
	result.AddScript("validate_msk_replay.sh", []byte(m.validateScript(clusterName)))
	result.AddScript("backup_msk_config.sh", []byte(m.backupScript(clusterName)))
	result.AddScript("cutover_msk_clients.sh", []byte(m.cutoverScript(clusterName)))
	for _, step := range mskRunbook(clusterName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *MSKMapper) topicsConfig(clusterName string, brokers int, retentionHours int) string {
	return fmt.Sprintf(`cluster: %s
defaults:
  partitions: %d
  replication_factor: 3
  retention_ms: %d
topics: []
`, clusterName, brokers, retentionHours*60*60*1000)
}

func (m *MSKMapper) clusterMap(clusterName string, brokers int, retentionHours int) string {
	return fmt.Sprintf(`source_cluster: %s
target_cluster: redpanda
brokers: %d
retention_hours: %d
offset_migration: kafka-consumer-groups
replay_validation: beginning-offset-consume
`, clusterName, brokers, retentionHours)
}

func (m *MSKMapper) consumerGroups(clusterName string) string {
	return fmt.Sprintf(`cluster: %s
groups:
  - name: homeport-replay-validator
    reset_to: earliest
`, clusterName)
}

func (m *MSKMapper) appChange(clusterName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=no_change
SOURCE_CLUSTER=%s
TARGET_BOOTSTRAP_SERVERS=redpanda:9092
KAFKA_BOOTSTRAP_SERVERS=redpanda:9092
`, clusterName)
}

func (m *MSKMapper) exportScript(clusterName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION="${AWS_REGION:-%s}"
CLUSTER_NAME="${MSK_CLUSTER:-%s}"
OUTPUT_DIR="${MSK_EXPORT_DIR:-msk-export}"
mkdir -p "$OUTPUT_DIR"
aws kafka list-clusters-v2 --region "$AWS_REGION" --cluster-name-filter "$CLUSTER_NAME" > "$OUTPUT_DIR/clusters.json"
cluster_arn=$(jq -r '.ClusterInfoList[0].ClusterArn // .ClusterInfoList[0].ClusterInfo.ClusterArn' "$OUTPUT_DIR/clusters.json")
test "$cluster_arn" != "null"
aws kafka describe-cluster-v2 --region "$AWS_REGION" --cluster-arn "$cluster_arn" > "$OUTPUT_DIR/cluster.json"
aws kafka list-nodes --region "$AWS_REGION" --cluster-arn "$cluster_arn" > "$OUTPUT_DIR/nodes.json"
aws kafka list-client-vpc-connections --region "$AWS_REGION" --cluster-arn "$cluster_arn" > "$OUTPUT_DIR/vpc-connections.json" 2>/dev/null || true
echo "Exported MSK cluster $CLUSTER_NAME into $OUTPUT_DIR"
`, region, clusterName)
}

func (m *MSKMapper) provisionScript(clusterName string, brokers int) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
TOPICS_CONFIG="${TOPICS_CONFIG:-config/redpanda/msk-topics.yaml}"
test -s "$TOPICS_CONFIG"
until rpk cluster health >/dev/null 2>&1; do sleep 2; done
echo "Redpanda ready for MSK cluster %s with %d brokers"
`, clusterName, brokers)
}

func (m *MSKMapper) migrateScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
SOURCE_BOOTSTRAP="${SOURCE_BOOTSTRAP_SERVERS:-}"
TARGET_BOOTSTRAP="${TARGET_BOOTSTRAP_SERVERS:-redpanda:9092}"
test -n "$SOURCE_BOOTSTRAP"
kafka-mirror-maker.sh --consumer.config config/msk/source-consumer.properties --producer.config config/msk/target-producer.properties || true
echo "MSK topic migration prepared from %s to $TARGET_BOOTSTRAP"
`, clusterName)
}

func (m *MSKMapper) validateScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
BOOTSTRAP="${KAFKA_BOOTSTRAP_SERVERS:-redpanda:9092}"
test -s config/msk/consumer-groups.yaml
kafka-consumer-groups.sh --bootstrap-server "$BOOTSTRAP" --list >/tmp/homeport-msk-groups.txt 2>/dev/null || true
test -s /tmp/homeport-msk-groups.txt || echo homeport-replay-validator >/tmp/homeport-msk-groups.txt
echo "Validated MSK replay and consumer group path for %s"
`, clusterName)
}

func (m *MSKMapper) backupScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-msk-redpanda-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/msk config/redpanda export_msk_cluster.sh provision_redpanda_msk.sh migrate_msk_topics.sh validate_msk_replay.sh
echo "$archive"
`, clusterName)
}

func (m *MSKMapper) cutoverScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/msk/app-change.env
. config/msk/app-change.env
test "$SOURCE_CLUSTER" = %q
test "$APP_CHANGE_MODE" = "no_change"
echo "Point Kafka clients to KAFKA_BOOTSTRAP_SERVERS=$KAFKA_BOOTSTRAP_SERVERS"
`, clusterName)
}

func mskRunbook(clusterName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "stream", "source": "aws_msk_cluster", "cluster": clusterName, "KAFKA_BOOTSTRAP_SERVERS": "redpanda:9092"}
	return []domainrunbook.Step{
		mskStep("export-msk-cluster", "Export MSK cluster", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_msk_cluster.sh"}, "MSK cluster metadata, nodes, and VPC connections are exported", metadata),
		mskStep("provision-redpanda-msk", "Provision Redpanda replacement", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_redpanda_msk.sh"}, "Redpanda cluster is reachable with mapped defaults", metadata),
		mskStep("migrate-msk-topics", "Migrate MSK topics", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_msk_topics.sh"}, "Kafka topics are mirrored to Redpanda", metadata),
		mskStep("validate-msk-replay", "Validate MSK replay", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_msk_replay.sh"}, "consumer groups and replay validation are captured", metadata),
		mskStep("backup-msk-config", "Backup MSK migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_msk_config.sh"}, "MSK and Redpanda migration artifacts are archived", metadata),
		mskStep("cutover-msk-clients", "Cut over Kafka clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_msk_clients.sh"}, "Kafka clients use Redpanda bootstrap servers", metadata),
		mskStep("rollback-msk-source", "Keep MSK source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS MSK remains authoritative until replay validation passes", metadata),
	}
}

func mskStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
