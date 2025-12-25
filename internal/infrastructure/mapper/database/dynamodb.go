// Package database provides mappers for AWS database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
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
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source": "aws_dynamodb_table",
		"cloudexit.table":  tableName,
		"cloudexit.engine": "scylla",
	}

	scyllaConfig := m.generateScyllaDBConfig()
	result.AddConfig("config/scylladb/scylla.yaml", []byte(scyllaConfig))

	tableScript := m.generateTableCreationScript(res, tableName)
	result.AddScript("create_table.cql", []byte(tableScript))

	migrationScript := m.generateMigrationScript(tableName)
	result.AddScript("migrate_dynamodb.sh", []byte(migrationScript))

	result.AddWarning("ScyllaDB Alternator provides DynamoDB-compatible API on port 8000. Update your SDK endpoint.")
	result.AddWarning("Consider using ScyllaDB Alternator for easier migration. See: https://www.scylladb.com/alternator/")

	if billingMode == "PAY_PER_REQUEST" {
		result.AddWarning("DynamoDB on-demand billing detected. ScyllaDB does not auto-scale - configure resources appropriately.")
	}

	if streamEnabled {
		result.AddWarning("DynamoDB Streams enabled. ScyllaDB CDC can provide similar functionality but requires configuration.")
		result.AddManualStep("Configure ScyllaDB CDC if you need change streams")
	}

	result.AddManualStep("Update application SDK endpoint to http://localhost:8000 for Alternator API")
	result.AddManualStep("Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY to any values for local development")
	result.AddManualStep("Review and execute the CQL table creation script if not using Alternator")

	return result, nil
}

func (m *DynamoDBMapper) generateScyllaDBConfig() string {
	return `# ScyllaDB Configuration with Alternator (DynamoDB-compatible API)
cluster_name: 'cloudexit_cluster'
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

CREATE KEYSPACE IF NOT EXISTS cloudexit
WITH REPLICATION = {
    'class': 'SimpleStrategy',
    'replication_factor': 1
};

USE cloudexit;

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

// Ensure strings package is used
var _ = strings.Contains
