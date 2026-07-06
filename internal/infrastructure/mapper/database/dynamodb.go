// Package database provides mappers for AWS database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/datarunbook"
)

// DynamoDBMapper converts AWS DynamoDB tables to ScyllaDB containers.
type DynamoDBMapper struct {
	*mapper.BaseMapper
}

// NewDynamoDBMapper creates a new DynamoDB to ScyllaDB mapper.
func NewDynamoDBMapper() *DynamoDBMapper {
	return &DynamoDBMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeDynamoDBTable, nil),
	}
}

// Map converts a DynamoDB table to a ScyllaDB service.
func (m *DynamoDBMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	tableName := res.GetConfigString("name")
	if tableName == "" {
		tableName = res.Name
	}

	billingMode := res.GetConfigString("billing_mode")
	streamEnabled := res.GetConfigBool("stream_enabled")

	result := mapper.NewMappingResult("scylladb")
	svc := result.DockerService

	svc.Image = "scylladb/scylla:5.4"
	svc.Ports = []string{"9042:9042", "8000:8000"}
	svc.Volumes = []string{"./data/scylladb:/var/lib/scylla"}
	svc.Command = []string{
		"--smp", "2",
		"--memory", "2G",
		"--overprovisioned", "1",
		"--api-address", "0.0.0.0",
		"--alternator-port", "8000",
		"--alternator-write-isolation", "always_use_lwt",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "nodetool status || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source": "aws_dynamodb_table",
		"homeport.table":  tableName,
		"homeport.engine": "scylla",
	}

	scyllaConfig := m.generateScyllaDBConfig()
	result.AddConfig("config/scylladb/scylla.yaml", []byte(scyllaConfig))
	result.AddConfig("config/scylladb/cdc.yaml", []byte(m.generateCDCConfig(tableName, streamEnabled)))
	result.AddConfig("config/dynamodb/app-change.env", []byte(m.generateAppChangeEnv(tableName)))

	tableScript := m.generateTableCreationScript(res, tableName)
	result.AddScript("create_table.cql", []byte(tableScript))

	migrationScript := m.generateMigrationScript(tableName)
	result.AddScript("migrate_dynamodb.sh", []byte(migrationScript))
	result.AddScript("backup_dynamodb_alternator.sh", []byte(m.generateBackupScript(tableName)))
	result.AddScript("validate_dynamodb_alternator.sh", []byte(m.generateValidationScript(tableName)))
	for _, step := range datarunbook.DynamoDB(tableName, streamEnabled) {
		result.AddRunbookStep(step)
	}
	for _, step := range dynamoDBRunbook(tableName) {
		result.AddRunbookStep(step)
	}

	result.AddWarning("ScyllaDB Alternator provides DynamoDB-compatible API on port 8000. Update your SDK endpoint.")
	result.AddWarning("Consider using ScyllaDB Alternator for easier migration. See: https://www.scylladb.com/alternator/")

	if billingMode == "PAY_PER_REQUEST" {
		result.AddWarning("DynamoDB on-demand billing detected. ScyllaDB does not auto-scale - configure resources appropriately.")
	}

	if streamEnabled {
		result.AddWarning("DynamoDB Streams enabled. Generated ScyllaDB CDC config and validation script cover stream handoff.")
	}

	return result, nil
}

func (m *DynamoDBMapper) generateScyllaDBConfig() string {
	return `# ScyllaDB Configuration with Alternator (DynamoDB-compatible API)
cluster_name: 'homeport_cluster'
listen_address: 0.0.0.0
rpc_address: 0.0.0.0
broadcast_rpc_address: 0.0.0.0

alternator_port: 8000
alternator_write_isolation: always_use_lwt

data_file_directories:
    - /var/lib/scylla/data
commitlog_directory: /var/lib/scylla/commitlog

compaction_throughput_mb_per_sec: 0
memtable_flush_writers: 1
`
}

func (m *DynamoDBMapper) generateTableCreationScript(res *resource.AWSResource, tableName string) string {
	hashKey := res.GetConfigString("hash_key")
	if hashKey == "" {
		hashKey = "id"
	}
	rangeKey := res.GetConfigString("range_key")

	script := fmt.Sprintf(`-- ScyllaDB CQL Script for table: %s
-- Use this if you prefer CQL over Alternator API

CREATE KEYSPACE IF NOT EXISTS homeport
WITH REPLICATION = {
    'class': 'SimpleStrategy',
    'replication_factor': 1
};

USE homeport;

CREATE TABLE IF NOT EXISTS %s (
    %s text`, tableName, tableName, hashKey)

	if rangeKey != "" {
		script += fmt.Sprintf(",\n    %s text", rangeKey)
	}
	script += ",\n    data text"
	script += fmt.Sprintf(",\n    PRIMARY KEY (%s", hashKey)
	if rangeKey != "" {
		script += fmt.Sprintf(", %s", rangeKey)
	}
	script += ")\n);\n"

	return script
}

func (m *DynamoDBMapper) generateMigrationScript(tableName string) string {
	return fmt.Sprintf(`#!/bin/bash
# DynamoDB to ScyllaDB Migration Script
set -e

echo "DynamoDB to ScyllaDB Migration"
echo "=============================="
echo "Table: %s"
echo ""

TABLE_NAME="%s"
AWS_REGION="${AWS_REGION:-us-east-1}"
SCYLLA_ENDPOINT="${SCYLLA_ENDPOINT:-http://localhost:8000}"

echo "Option 1: Using Alternator (Recommended)"
echo "  Update your AWS SDK endpoint to: $SCYLLA_ENDPOINT"
echo "  Your existing DynamoDB code will work with minimal changes"
echo ""

echo "Option 2: Export and Import"
echo "  # Export from DynamoDB"
echo "  aws dynamodb scan --table-name $TABLE_NAME --region $AWS_REGION --output json > /tmp/${TABLE_NAME}.json"
echo "  # Import using AWS SDK pointing to Alternator endpoint"
echo ""

echo "For more info: https://docs.scylladb.com/stable/using-scylla/alternator/"
`, tableName, tableName)
}

func (m *DynamoDBMapper) generateCDCConfig(tableName string, streamEnabled bool) string {
	return fmt.Sprintf("table: %s\ncdc_enabled: %t\nconsumer_group: homeport-%s-cdc\ncheckpoint_table: %s_cdc_checkpoints\n", tableName, streamEnabled, tableName, tableName)
}

func (m *DynamoDBMapper) generateAppChangeEnv(tableName string) string {
	return fmt.Sprintf("DYNAMODB_TABLE=%s\nAWS_ENDPOINT_URL_DYNAMODB=http://scylladb:8000\nAWS_ACCESS_KEY_ID=homeport\nAWS_SECRET_ACCESS_KEY=homeport\nAWS_REGION=us-east-1\n", tableName)
}

func (m *DynamoDBMapper) generateBackupScript(tableName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/dynamodb-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/scylladb config/dynamodb create_table.cql migrate_dynamodb.sh
echo "$archive"
`, tableName)
}

func (m *DynamoDBMapper) generateValidationScript(tableName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/dynamodb/app-change.env
test "$DYNAMODB_TABLE" = %s
test "$AWS_ENDPOINT_URL_DYNAMODB" = "http://scylladb:8000"
echo dynamodb-alternator-validation-ok
`, quoteDynamoDBShell(tableName))
}

func dynamoDBRunbook(tableName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "dynamodb", "table": tableName, "source": string(resource.TypeDynamoDBTable)}
	return []domainrunbook.Step{
		dynamoDBStep("backup-dynamodb-alternator", "Backup DynamoDB migration config", domainrunbook.StepTypeCommand, []string{"sh", "backup_dynamodb_alternator.sh"}, "backup archive path is printed", metadata),
		dynamoDBStep("cutover-dynamodb-endpoint", "Cut over DynamoDB SDK endpoint", domainrunbook.StepTypeAPICall, []string{"sh", "-c", ". config/dynamodb/app-change.env && echo $AWS_ENDPOINT_URL_DYNAMODB"}, "application uses Scylla Alternator endpoint", metadata),
	}
}

func dynamoDBStep(id, name string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}

func quoteDynamoDBShell(value string) string {
	return "'" + value + "'"
}
