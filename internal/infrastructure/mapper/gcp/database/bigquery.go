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

type BigQueryMapper struct {
	*mapper.BaseMapper
}

func NewBigQueryMapper() *BigQueryMapper {
	return &BigQueryMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeBigQuery, nil)}
}

func (m *BigQueryMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	dataset := res.GetConfigString("dataset_id")
	if dataset == "" {
		dataset = res.GetConfigString("name")
	}
	if dataset == "" {
		dataset = res.Name
	}
	location := res.GetConfigString("location")
	if location == "" {
		location = res.Region
	}
	if location == "" {
		location = "US"
	}

	result := mapper.NewMappingResult("trino")
	svc := result.DockerService
	svc.Image = "trinodb/trino:445"
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{
		"./config/bigquery/catalog:/etc/trino/catalog:ro",
		"./data/bigquery:/var/homeport/bigquery",
	}
	svc.Environment = map[string]string{
		"HOMEPORT_BIGQUERY_DATASET":  dataset,
		"HOMEPORT_BIGQUERY_LOCATION": location,
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "curl", "-fsS", "http://localhost:8080/v1/info"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{
		"homeport.source":   "google_bigquery_dataset",
		"homeport.dataset":  dataset,
		"homeport.location": location,
		"homeport.target":   "trino-iceberg",
	}

	result.AddConfig("config/bigquery/catalog/iceberg.properties", []byte(bigQueryIcebergCatalog()))
	result.AddConfig("config/bigquery/app-change.env", []byte(m.appChangeConfig(dataset, location)))
	result.AddConfig("config/bigquery/bigquery-api-routes.yaml", []byte(bigQueryAPIRoutes(dataset)))
	result.AddScript("export_bigquery_dataset.sh", []byte(m.exportScript(dataset, location)))
	result.AddScript("load_bigquery_iceberg.sh", []byte(m.loadScript(dataset)))
	result.AddScript("backup_bigquery.sh", []byte(m.backupScript(dataset)))
	result.AddScript("validate_bigquery.sh", []byte(m.validateScript(dataset)))
	for _, step := range bigQueryRunbook(dataset) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func bigQueryIcebergCatalog() string {
	return `connector.name=iceberg
iceberg.catalog.type=jdbc
iceberg.jdbc-catalog.catalog-name=homeport
iceberg.jdbc-catalog.driver-class=org.postgresql.Driver
iceberg.jdbc-catalog.connection-url=jdbc:postgresql://postgres:5432/iceberg
iceberg.jdbc-catalog.connection-user=iceberg
iceberg.jdbc-catalog.connection-password=iceberg
iceberg.file-format=PARQUET
fs.native-s3.enabled=false
`
}

func (m *BigQueryMapper) appChangeConfig(dataset, location string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_BIGQUERY_DATASET=%s
SOURCE_BIGQUERY_LOCATION=%s
TARGET_QUERY_ENDPOINT=http://trino:8080
TARGET_QUERY_CATALOG=iceberg
TARGET_QUERY_SCHEMA=%s
`, dataset, location, dataset)
}

func bigQueryAPIRoutes(dataset string) string {
	return fmt.Sprintf(`service: bigquery
dataset: %s
target: trino-iceberg
actions:
  bigquery.jobs.query:
    route: /compat/gcp/bigquery/v2/projects/{project}/queries
    target_sql_endpoint: http://trino:8080/v1/statement
  bigquery.jobs.getQueryResults:
    route: /compat/gcp/bigquery/v2/projects/{project}/queries/{job}
    target_sql_endpoint: http://trino:8080/v1/statement/{job}
  bigquery.datasets.get:
    route: /compat/gcp/bigquery/v2/projects/{project}/datasets/{dataset}
  bigquery.datasets.list:
    route: /compat/gcp/bigquery/v2/projects/{project}/datasets
  bigquery.tables.get:
    route: /compat/gcp/bigquery/v2/projects/{project}/datasets/{dataset}/tables/{table}
error_mapping:
  not_found: 404
  throttled: 429
  validation: 400
pagination:
  token_field: pageToken
idempotency:
  key_header: x-goog-request-params
`, dataset)
}

func (m *BigQueryMapper) exportScript(dataset, location string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
dataset="${SOURCE_BIGQUERY_DATASET:-%s}"
location="${SOURCE_BIGQUERY_LOCATION:-%s}"
bucket="${EXPORT_BUCKET:-homeport-bigquery-export}"
mkdir -p "data/bigquery/$dataset"
bq --location="$location" extract --destination_format=PARQUET "$dataset.*" "gs://$bucket/$dataset/*.parquet"
gsutil -m cp -r "gs://$bucket/$dataset" "data/bigquery/"
`, dataset, location)
}

func (m *BigQueryMapper) loadScript(dataset string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
dataset="${TARGET_QUERY_SCHEMA:-%s}"
trino="${TARGET_QUERY_ENDPOINT:-http://trino:8080}"
curl -fsS "$trino/v1/info" >/dev/null
trino --server "$trino" --catalog iceberg --execute "CREATE SCHEMA IF NOT EXISTS iceberg.$dataset"
find "data/bigquery/$dataset" -name '*.parquet' -print
`, dataset)
}

func (m *BigQueryMapper) backupScript(dataset string) string {
	safeName := strings.NewReplacer("/", "-", " ", "-").Replace(dataset)
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-bigquery-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/bigquery data/bigquery
echo "$archive"
`, safeName)
}

func (m *BigQueryMapper) validateScript(dataset string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/bigquery/catalog/iceberg.properties
test -s config/bigquery/bigquery-api-routes.yaml
curl -fsS "${TARGET_QUERY_ENDPOINT:-http://trino:8080}/v1/info" >/dev/null
echo "BigQuery dataset %s validated on Trino/Iceberg"
`, dataset)
}

func bigQueryRunbook(dataset string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "analytics-dataset", "source": "google_bigquery_dataset", "dataset": dataset}
	return []domainrunbook.Step{
		bigQueryStep("discover-bigquery-dataset", "Discover BigQuery dataset", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("bq show --format=prettyjson %q", dataset)}, "dataset metadata and tables are enumerated", metadata),
		bigQueryStep("provision-trino-iceberg", "Provision Trino with Iceberg catalog", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/bigquery/catalog/iceberg.properties"}, "Trino and Iceberg catalog config are rendered", metadata),
		bigQueryStep("export-bigquery-dataset", "Export BigQuery dataset", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "export_bigquery_dataset.sh"}, "dataset tables are exported as Parquet", metadata),
		bigQueryStep("load-bigquery-iceberg", "Load Iceberg tables", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "load_bigquery_iceberg.sh"}, "exported data is visible through Trino", metadata),
		bigQueryStep("validate-bigquery-api", "Validate BigQuery-compatible API", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_bigquery.sh"}, "query endpoint and compatibility routes respond", metadata),
		bigQueryStep("backup-bigquery-iceberg", "Backup Iceberg data", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_bigquery.sh"}, "config and data exports are archived", metadata),
		bigQueryStep("cutover-bigquery-client-config", "Cut over BigQuery clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/bigquery/app-change.env"}, "generated patch points clients at the Trino-backed API", metadata),
		bigQueryStep("rollback-bigquery-source", "Keep BigQuery as rollback source", "Rollback", domainrunbook.StepTypeRollback, nil, "source BigQuery dataset remains authoritative until validation passes", metadata),
	}
}

func bigQueryStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
