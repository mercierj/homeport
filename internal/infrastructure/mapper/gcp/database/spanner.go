// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// SpannerMapper converts GCP Spanner to CockroachDB containers.
type SpannerMapper struct {
	*mapper.BaseMapper
}

// NewSpannerMapper creates a new Spanner mapper.
func NewSpannerMapper() *SpannerMapper {
	return &SpannerMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeSpanner, nil),
	}
}

// Map converts a Spanner instance to a CockroachDB service.
func (m *SpannerMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	instanceName := res.GetConfigString("name")
	if instanceName == "" {
		instanceName = res.Name
	}

	result := mapper.NewMappingResult("cockroachdb")
	svc := result.DockerService

	svc.Image = "cockroachdb/cockroach:v23.2.0"
	svc.Ports = []string{"26257:26257", "8080:8080"}
	svc.Volumes = []string{"./data/cockroachdb:/cockroach/cockroach-data"}
	svc.Command = []string{
		"start-single-node",
		"--insecure",
		"--store=/cockroach/cockroach-data",
		"--listen-addr=0.0.0.0:26257",
		"--http-addr=0.0.0.0:8080",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:8080/health?ready=1"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":   "google_spanner_instance",
		"cloudexit.engine":   "cockroachdb",
		"cloudexit.instance": instanceName,
	}

	schemaScript := m.generateSchemaConversionScript()
	result.AddScript("convert_schema.sh", []byte(schemaScript))

	migrationScript := m.generateMigrationScript(instanceName)
	result.AddScript("migrate_spanner.sh", []byte(migrationScript))

	clusterConfig := m.generateClusterConfig()
	result.AddConfig("config/cockroachdb/cluster-config.yml", []byte(clusterConfig))

	result.AddWarning("Spanner and CockroachDB are both distributed SQL databases but have different SQL dialects.")
	result.AddWarning("Spanner-specific features like interleaved tables need schema redesign for CockroachDB.")
	result.AddWarning("CockroachDB uses PostgreSQL wire protocol - update your connection drivers.")

	result.AddManualStep("Convert Spanner DDL to CockroachDB-compatible schema")
	result.AddManualStep("Update application to use PostgreSQL driver for CockroachDB")
	result.AddManualStep("Export data from Spanner and import to CockroachDB")
	result.AddManualStep("Access CockroachDB Admin UI at http://localhost:8080")

	return result, nil
}

func (m *SpannerMapper) generateSchemaConversionScript() string {
	return `#!/bin/bash
# Spanner to CockroachDB Schema Conversion Guide
set -e

echo "Spanner to CockroachDB Schema Conversion"
echo "========================================="

echo "Key differences to address:"
echo ""
echo "1. Data Types:"
echo "   Spanner INT64   -> CockroachDB INT8"
echo "   Spanner FLOAT64 -> CockroachDB FLOAT8"
echo "   Spanner BYTES   -> CockroachDB BYTES"
echo "   Spanner STRING  -> CockroachDB STRING/VARCHAR"
echo "   Spanner DATE    -> CockroachDB DATE"
echo "   Spanner TIMESTAMP -> CockroachDB TIMESTAMP"
echo ""
echo "2. Primary Keys:"
echo "   Spanner allows composite primary keys in any order"
echo "   CockroachDB requires PRIMARY KEY definition similar to PostgreSQL"
echo ""
echo "3. Interleaved Tables:"
echo "   Spanner: INTERLEAVE IN PARENT table"
echo "   CockroachDB: No direct equivalent, use foreign keys instead"
echo ""
echo "4. Secondary Indexes:"
echo "   Spanner: CREATE INDEX ... INTERLEAVE IN ..."
echo "   CockroachDB: CREATE INDEX (standard PostgreSQL syntax)"
echo ""
echo "Example conversion:"
echo "  # Spanner"
echo "  CREATE TABLE Users ("
echo "    UserId INT64 NOT NULL,"
echo "    Name STRING(100)"
echo "  ) PRIMARY KEY (UserId)"
echo ""
echo "  # CockroachDB"
echo "  CREATE TABLE Users ("
echo "    user_id INT8 NOT NULL,"
echo "    name VARCHAR(100),"
echo "    PRIMARY KEY (user_id)"
echo "  )"
`
}

func (m *SpannerMapper) generateMigrationScript(instanceName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Spanner to CockroachDB Migration Script
set -e

echo "Spanner to CockroachDB Migration"
echo "================================="
echo "Instance: %s"

PROJECT="${PROJECT:-your-project}"
INSTANCE="${INSTANCE:-your-instance}"
DATABASE="${DATABASE:-your-database}"
COCKROACH_URL="postgresql://root@localhost:26257/defaultdb?sslmode=disable"

echo "Step 1: Export schema from Spanner"
echo "  gcloud spanner databases ddl describe $DATABASE \\"
echo "    --instance=$INSTANCE > spanner_schema.sql"

echo "Step 2: Convert schema (manual step)"
echo "  # Edit spanner_schema.sql to CockroachDB-compatible DDL"
echo "  # See convert_schema.sh for guidance"

echo "Step 3: Create schema in CockroachDB"
echo "  cockroach sql --url '$COCKROACH_URL' < cockroach_schema.sql"

echo "Step 4: Export data from Spanner"
echo "  # Using gcloud to export to Avro or CSV format"
echo "  gcloud spanner databases export $DATABASE \\"
echo "    --instance=$INSTANCE \\"
echo "    --destination-uri=gs://BUCKET/spanner-export"

echo "Step 5: Import data to CockroachDB"
echo "  # Using IMPORT INTO for CSV files"
echo "  cockroach sql --url '$COCKROACH_URL' -e \\"
echo "    \"IMPORT INTO table_name CSV DATA ('nodelocal:///path/to/data.csv')\""

echo "For large-scale migrations, consider using:"
echo "  - Striim for continuous replication"
echo "  - HarbourBridge (Google's migration tool)"
echo "  - Custom ETL with Apache Beam"
`, instanceName)
}

func (m *SpannerMapper) generateClusterConfig() string {
	return `# CockroachDB Cluster Configuration
# For production deployments, consider multi-node setup

# Single-node development configuration
# For production, deploy 3+ nodes across availability zones

# Example multi-node docker-compose addition:
#
# cockroachdb-1:
#   image: cockroachdb/cockroach:v23.2.0
#   command: start --insecure --join=cockroachdb-1,cockroachdb-2,cockroachdb-3
#
# cockroachdb-2:
#   image: cockroachdb/cockroach:v23.2.0
#   command: start --insecure --join=cockroachdb-1,cockroachdb-2,cockroachdb-3
#
# cockroachdb-3:
#   image: cockroachdb/cockroach:v23.2.0
#   command: start --insecure --join=cockroachdb-1,cockroachdb-2,cockroachdb-3

# After starting nodes, initialize the cluster:
# docker exec -it cockroachdb-1 ./cockroach init --insecure
`
}
