// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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
		"CASSANDRA_CLUSTER_NAME":    "homeport_cluster",
		"CASSANDRA_DC":              "dc1",
		"CASSANDRA_RACK":            "rack1",
		"CASSANDRA_ENDPOINT_SNITCH": "GossipingPropertyFileSnitch",
		"MAX_HEAP_SIZE":             "2G",
		"HEAP_NEWSIZE":              "512M",
	}
	svc.Ports = []string{"9042:9042", "7000:7000"}
	svc.Volumes = []string{"./data/cassandra:/var/lib/cassandra"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "cqlsh -e 'describe cluster' || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":   "google_bigtable_instance",
		"homeport.engine":   "cassandra",
		"homeport.instance": instanceName,
	}

	cassandraConfig := m.generateCassandraConfig()
	result.AddConfig("config/cassandra/cassandra.yaml", []byte(cassandraConfig))
	result.AddConfig("config/bigtable/app-change.env", []byte(m.appChangeConfig(instanceName)))
	result.AddConfig("config/bigtable/bigtable-api-routes.yaml", []byte(bigtableAPIRoutes(instanceName)))

	result.AddScript("export_bigtable.sh", []byte(m.exportScript(instanceName)))
	result.AddScript("load_bigtable_cassandra.sh", []byte(m.loadScript(instanceName)))
	result.AddScript("backup_bigtable.sh", []byte(m.backupScript(instanceName)))
	result.AddScript("validate_bigtable.sh", []byte(m.validateScript(instanceName)))
	for _, step := range bigtableRunbook(instanceName) {
		result.AddRunbookStep(step)
	}

	result.AddWarning("Bigtable is a wide-column store. Cassandra provides similar functionality but with CQL syntax.")
	result.AddWarning("Bigtable row keys map to Cassandra partition keys. Review your data model.")
	result.AddWarning("Bigtable column families need to be converted to Cassandra tables.")

	return result, nil
}

func (m *BigtableMapper) generateCassandraConfig() string {
	return `# Cassandra Configuration
cluster_name: 'homeport_cluster'
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

func (m *BigtableMapper) appChangeConfig(instanceName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_BIGTABLE_INSTANCE=%s
TARGET_BIGTABLE_ENDPOINT=http://bigtable-adapter:8086
TARGET_CASSANDRA_CONTACT_POINTS=cassandra:9042
TARGET_KEYSPACE=%s
`, instanceName, sanitizeBigtableName(instanceName))
}

func bigtableAPIRoutes(instanceName string) string {
	return fmt.Sprintf(`service: bigtable
instance: %s
target: cassandra
grpc_endpoint: http://bigtable-adapter:8086
methods:
  google.bigtable.v2.Bigtable.ReadRows:
    target: cassandra_select
    pagination: row_key_resume_token
  google.bigtable.v2.Bigtable.MutateRow:
    target: cassandra_upsert
    idempotency: row_key_mutation_hash
  google.bigtable.v2.Bigtable.MutateRows:
    target: cassandra_batch
    idempotency: mutation_entry_hash
  google.bigtable.v2.Bigtable.SampleRowKeys:
    target: cassandra_token_ranges
  google.bigtable.admin.v2.BigtableTableAdmin.GetTable:
    target: cassandra_schema
  google.bigtable.admin.v2.BigtableTableAdmin.ListTables:
    target: cassandra_keyspace_tables
errors:
  not_found: NOT_FOUND
  throttled: RESOURCE_EXHAUSTED
  validation: INVALID_ARGUMENT
authz:
  read: bigtable.tables.readRows
  write: bigtable.tables.mutateRows
`, instanceName)
}

func (m *BigtableMapper) exportScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
instance="${SOURCE_BIGTABLE_INSTANCE:-%s}"
mkdir -p "data/bigtable/$instance"
cbt -instance="$instance" ls > "data/bigtable/$instance/tables.txt"
while IFS= read -r table; do
  [ -n "$table" ] || continue
  cbt -instance="$instance" read "$table" > "data/bigtable/$instance/$table.rows"
done < "data/bigtable/$instance/tables.txt"
`, instanceName)
}

func (m *BigtableMapper) loadScript(instanceName string) string {
	keyspace := sanitizeBigtableName(instanceName)
	return fmt.Sprintf(`#!/bin/sh
set -eu
instance="${SOURCE_BIGTABLE_INSTANCE:-%s}"
keyspace="${TARGET_KEYSPACE:-%s}"
cqlsh cassandra 9042 -e "CREATE KEYSPACE IF NOT EXISTS $keyspace WITH REPLICATION = {'class': 'SimpleStrategy', 'replication_factor': 2};"
while IFS= read -r table; do
  [ -n "$table" ] || continue
  cqlsh cassandra 9042 -e "CREATE TABLE IF NOT EXISTS $keyspace.$table (row_key text PRIMARY KEY, cells map<text, blob>);"
done < "data/bigtable/$instance/tables.txt"
`, instanceName, keyspace)
}

func (m *BigtableMapper) backupScript(instanceName string) string {
	safeName := strings.NewReplacer("/", "-", " ", "-").Replace(instanceName)
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-bigtable-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/bigtable config/cassandra data/bigtable data/cassandra
echo "$archive"
`, safeName)
}

func (m *BigtableMapper) validateScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/bigtable/app-change.env
test -s config/bigtable/bigtable-api-routes.yaml
cqlsh cassandra 9042 -e 'describe keyspaces' >/dev/null
echo "Bigtable instance %s validated on Cassandra adapter"
`, instanceName)
}

func bigtableRunbook(instanceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "wide-column-database", "source": "google_bigtable_instance", "instance": instanceName}
	return []domainrunbook.Step{
		bigtableStep("discover-bigtable-tables", "Discover Bigtable tables", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("cbt -instance=%q ls", instanceName)}, "source tables and column families are enumerated", metadata),
		bigtableStep("provision-cassandra-bigtable", "Provision Cassandra Bigtable target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/cassandra/cassandra.yaml"}, "Cassandra target and Bigtable adapter config are rendered", metadata),
		bigtableStep("export-bigtable-tables", "Export Bigtable tables", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "export_bigtable.sh"}, "Bigtable rows are exported by table", metadata),
		bigtableStep("load-bigtable-cassandra", "Load Cassandra tables", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "load_bigtable_cassandra.sh"}, "exported rows are loaded into Cassandra keyspace", metadata),
		bigtableStep("validate-bigtable-api-adapter", "Validate Bigtable-compatible API", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_bigtable.sh"}, "adapter route config and Cassandra backend validate", metadata),
		bigtableStep("backup-bigtable-cassandra", "Backup Cassandra Bigtable target", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_bigtable.sh"}, "config and data are archived", metadata),
		bigtableStep("cutover-bigtable-client-config", "Cut over Bigtable clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/bigtable/app-change.env"}, "generated patch points clients at the Bigtable adapter", metadata),
		bigtableStep("rollback-bigtable-source", "Keep Bigtable as rollback source", "Rollback", domainrunbook.StepTypeRollback, nil, "source Bigtable instance remains authoritative until validation passes", metadata),
	}
}

func bigtableStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func sanitizeBigtableName(name string) string {
	name = strings.ToLower(name)
	name = strings.NewReplacer("-", "_", ".", "_", "/", "_", " ", "_").Replace(name)
	if name == "" {
		return "bigtable"
	}
	return name
}
