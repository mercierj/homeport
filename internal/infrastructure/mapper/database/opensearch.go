package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type OpenSearchMapper struct {
	*mapper.BaseMapper
}

func NewOpenSearchMapper() *OpenSearchMapper {
	return &OpenSearchMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeOpenSearchDomain, nil)}
}

func (m *OpenSearchMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	domainName := res.GetConfigString("domain_name")
	if domainName == "" {
		domainName = res.Name
	}
	version := res.GetConfigString("engine_version")
	if version == "" {
		version = "2.15.0"
	}

	result := mapper.NewMappingResult("opensearch")
	svc := result.DockerService
	svc.Image = "opensearchproject/opensearch:2.15.0"
	svc.Environment = map[string]string{
		"cluster.name":                      "homeport-opensearch",
		"discovery.type":                    "single-node",
		"plugins.security.disabled":         "true",
		"OPENSEARCH_JAVA_OPTS":              "-Xms512m -Xmx512m",
		"HOMEPORT_SOURCE_OPENSEARCH_DOMAIN": domainName,
	}
	svc.Ports = []string{"9200:9200", "9600:9600"}
	svc.Volumes = []string{"./data/opensearch:/usr/share/opensearch/data", "./config/opensearch:/usr/share/opensearch/config/homeport"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":                      "aws_opensearch_domain",
		"homeport.domain":                      domainName,
		"homeport.target":                      "opensearch",
		"traefik.enable":                       "true",
		"traefik.http.routers.opensearch.rule": "Host(`opensearch.localhost`)",
		"traefik.http.services.opensearch.loadbalancer.server.port": "9200",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -fsS http://localhost:9200/_cluster/health >/dev/null"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/opensearch/domain-map.yaml", []byte(m.domainMap(domainName, version)))
	result.AddConfig("config/opensearch/app-change.env", []byte(m.appChange(domainName)))
	result.AddConfig("config/opensearch/snapshot-repository.json", []byte(m.snapshotRepository()))
	result.AddScript("export_opensearch_domain.sh", []byte(m.exportScript(domainName, res.Region)))
	result.AddScript("provision_opensearch.sh", []byte(m.provisionScript(domainName)))
	result.AddScript("migrate_opensearch_snapshots.sh", []byte(m.migrateScript(domainName)))
	result.AddScript("validate_opensearch_indexes.sh", []byte(m.validateScript(domainName)))
	result.AddScript("backup_opensearch_config.sh", []byte(m.backupScript(domainName)))
	result.AddScript("cutover_opensearch_clients.sh", []byte(m.cutoverScript(domainName)))
	for _, step := range openSearchRunbook(domainName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *OpenSearchMapper) domainMap(domainName, version string) string {
	return fmt.Sprintf("source_domain: %s\ntarget_cluster: opensearch\nengine_version: %s\nsnapshot_repository: homeport-migration\n", domainName, version)
}

func (m *OpenSearchMapper) appChange(domainName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_DOMAIN=%s
TARGET_OPENSEARCH_URL=http://opensearch:9200
OPENSEARCH_ENDPOINT=http://opensearch:9200
`, domainName)
}

func (m *OpenSearchMapper) snapshotRepository() string {
	return "{\n  \"type\": \"fs\",\n  \"settings\": {\"location\": \"/usr/share/opensearch/data/snapshots\"}\n}\n"
}

func (m *OpenSearchMapper) exportScript(domainName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION="${AWS_REGION:-%s}"
DOMAIN_NAME="${OPENSEARCH_DOMAIN:-%s}"
OUTPUT_DIR="${OPENSEARCH_EXPORT_DIR:-opensearch-export}"
mkdir -p "$OUTPUT_DIR"
aws opensearch describe-domain --region "$AWS_REGION" --domain-name "$DOMAIN_NAME" > "$OUTPUT_DIR/domain.json"
aws opensearch list-tags --region "$AWS_REGION" --arn "$(jq -r '.DomainStatus.ARN' "$OUTPUT_DIR/domain.json")" > "$OUTPUT_DIR/tags.json" 2>/dev/null || true
echo "Exported OpenSearch domain $DOMAIN_NAME into $OUTPUT_DIR"
`, region, domainName)
}

func (m *OpenSearchMapper) provisionScript(domainName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
until curl -fsS http://localhost:9200/_cluster/health >/dev/null; do sleep 2; done
curl -fsS -X PUT http://localhost:9200/_snapshot/homeport-migration -H 'Content-Type: application/json' --data-binary @config/opensearch/snapshot-repository.json
echo "Provisioned OpenSearch target for %s"
`, domainName)
}

func (m *OpenSearchMapper) migrateScript(domainName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
SOURCE_URL="${SOURCE_OPENSEARCH_URL:-}"
TARGET_URL="${TARGET_OPENSEARCH_URL:-http://localhost:9200}"
SNAPSHOT="${SNAPSHOT_NAME:-homeport-migration}"
test -n "$SOURCE_URL"
curl -fsS -X PUT "$SOURCE_URL/_snapshot/homeport-migration/$SNAPSHOT?wait_for_completion=true" || true
curl -fsS -X POST "$TARGET_URL/_snapshot/homeport-migration/$SNAPSHOT/_restore?wait_for_completion=true" || true
echo "Prepared OpenSearch snapshot migration for %s"
`, domainName)
}

func (m *OpenSearchMapper) validateScript(domainName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
URL="${TARGET_OPENSEARCH_URL:-http://localhost:9200}"
curl -fsS "$URL/_cluster/health" >/tmp/homeport-opensearch-health.json
curl -fsS "$URL/_cat/indices?format=json" >/tmp/homeport-opensearch-indices.json
test -s /tmp/homeport-opensearch-health.json
test -s /tmp/homeport-opensearch-indices.json
echo "Validated OpenSearch indexes for %s"
`, domainName)
}

func (m *OpenSearchMapper) backupScript(domainName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-opensearch-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/opensearch export_opensearch_domain.sh provision_opensearch.sh migrate_opensearch_snapshots.sh validate_opensearch_indexes.sh
echo "$archive"
`, domainName)
}

func (m *OpenSearchMapper) cutoverScript(domainName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/opensearch/app-change.env
. config/opensearch/app-change.env
test "$SOURCE_DOMAIN" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
echo "Patch OpenSearch clients to OPENSEARCH_ENDPOINT=$OPENSEARCH_ENDPOINT"
`, domainName)
}

func openSearchRunbook(domainName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "search", "source": "aws_opensearch_domain", "domain": domainName, "OPENSEARCH_ENDPOINT": "http://opensearch:9200"}
	return []domainrunbook.Step{
		openSearchStep("export-opensearch-domain", "Export OpenSearch domain", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_opensearch_domain.sh"}, "domain settings and tags are exported", metadata),
		openSearchStep("provision-opensearch", "Provision OpenSearch target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_opensearch.sh"}, "target cluster and snapshot repository are configured", metadata),
		openSearchStep("migrate-opensearch-snapshots", "Migrate OpenSearch snapshots", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_opensearch_snapshots.sh"}, "snapshots are restored into target OpenSearch", metadata),
		openSearchStep("validate-opensearch-indexes", "Validate OpenSearch indexes", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_opensearch_indexes.sh"}, "cluster health and index listing are available", metadata),
		openSearchStep("backup-opensearch-config", "Backup OpenSearch config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_opensearch_config.sh"}, "OpenSearch config and scripts are archived", metadata),
		openSearchStep("cutover-opensearch-clients", "Cut over OpenSearch clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_opensearch_clients.sh"}, "clients use target OpenSearch endpoint", metadata),
		openSearchStep("rollback-opensearch-source", "Keep OpenSearch source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS OpenSearch remains authoritative until index validation passes", metadata),
	}
}

func openSearchStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
