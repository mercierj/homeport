// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// BigtableMapper converts GCP Bigtable to Apache Cassandra containers.
type BigtableMapper struct {
	*mapper.BaseMapper
}

// NewBigtableMapper creates a new Bigtable mapper.
func NewBigtableMapper() *BigtableMapper {
	return &BigtableMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeBigtable, nil),
	}
}

// Map converts a Bigtable instance to a Cassandra service.
func (m *BigtableMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	instanceName := res.GetConfigString("name")
	if instanceName == "" {
		instanceName = res.Name
	}

	result := mapper.NewMappingResult("cassandra")
	svc := result.DockerService

	svc.Image = "cassandra:4.1"
	svc.Environment = map[string]string{
		"CASSANDRA_CLUSTER_NAME":  "cloudexit_cluster",
		"CASSANDRA_DC":            "dc1",
		"CASSANDRA_RACK":          "rack1",
		"CASSANDRA_ENDPOINT_SNITCH": "GossipingPropertyFileSnitch",
		"MAX_HEAP_SIZE":           "2G",
		"HEAP_NEWSIZE":            "512M",
	}
	svc.Ports = []string{"9042:9042", "7000:7000"}
	svc.Volumes = []string{"./data/cassandra:/var/lib/cassandra"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "cqlsh -e 'describe cluster' || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":   "google_bigtable_instance",
		"cloudexit.engine":   "cassandra",
		"cloudexit.instance": instanceName,
	}

	cassandraConfig := m.generateCassandraConfig()
	result.AddConfig("config/cassandra/cassandra.yaml", []byte(cassandraConfig))

	migrationScript := m.generateMigrationScript(instanceName)
	result.AddScript("migrate_bigtable.sh", []byte(migrationScript))

	result.AddWarning("Bigtable is a wide-column store. Cassandra provides similar functionality but with CQL syntax.")
	result.AddWarning("Bigtable row keys map to Cassandra partition keys. Review your data model.")
	result.AddWarning("Bigtable column families need to be converted to Cassandra tables.")

	result.AddManualStep("Update application code to use Cassandra driver")
	result.AddManualStep("Design Cassandra schema based on Bigtable table structure")
	result.AddManualStep("Export Bigtable data and import to Cassandra")

	return result, nil
}

func (m *BigtableMapper) generateCassandraConfig() string {
	return `# Cassandra Configuration
cluster_name: 'cloudexit_cluster'
num_tokens: 256
seed_provider:
  - class_name: org.apache.cassandra.locator.SimpleSeedProvider
    parameters:
      - seeds: "127.0.0.1"

listen_address: 0.0.0.0
rpc_address: 0.0.0.0
broadcast_rpc_address: 0.0.0.0

endpoint_snitch: GossipingPropertyFileSnitch

data_file_directories:
  - /var/lib/cassandra/data
commitlog_directory: /var/lib/cassandra/commitlog
saved_caches_directory: /var/lib/cassandra/saved_caches

compaction_throughput_mb_per_sec: 64
concurrent_reads: 32
concurrent_writes: 32
`
}

func (m *BigtableMapper) generateMigrationScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Bigtable to Cassandra Migration Script
set -e

echo "Bigtable to Cassandra Migration"
echo "================================"
echo "Instance: %s"

echo "Step 1: Export from Bigtable"
echo "  # Use cbt tool to read data"
echo "  cbt -project=PROJECT -instance=INSTANCE read TABLE > bigtable_data.txt"

echo "Step 2: Create Cassandra schema"
echo "  # Example schema creation"
echo "  cqlsh -e \"CREATE KEYSPACE IF NOT EXISTS cloudexit WITH REPLICATION = {'class': 'SimpleStrategy', 'replication_factor': 1};\""

echo "Step 3: Transform and import data"
echo "  # Write a custom script to transform Bigtable data to CQL INSERT statements"
echo "  # Consider using Apache Spark with both connectors for large datasets"

echo "For large-scale migrations, consider using Apache Spark with:"
echo "  - spark-bigtable-connector"
echo "  - spark-cassandra-connector"
`, instanceName)
}
