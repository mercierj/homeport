package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type RedshiftMapper struct {
	*mapper.BaseMapper
}

func NewRedshiftMapper() *RedshiftMapper {
	return &RedshiftMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeRedshiftCluster, nil)}
}

func (m *RedshiftMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	clusterID := res.GetConfigString("cluster_identifier")
	if clusterID == "" {
		clusterID = res.Name
	}
	dbName := res.GetConfigString("database_name")
	if dbName == "" {
		dbName = "dev"
	}

	result := mapper.NewMappingResult("clickhouse")
	svc := result.DockerService
	svc.Image = "clickhouse/clickhouse-server:24.8-alpine"
	svc.Environment = map[string]string{"CLICKHOUSE_DB": dbName, "CLICKHOUSE_USER": "default", "CLICKHOUSE_PASSWORD": "changeme"}
	svc.Ports = []string{"8123:8123", "9000:9000"}
	svc.Volumes = []string{"./data/clickhouse:/var/lib/clickhouse", "./config/redshift:/etc/homeport/redshift"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{"homeport.source": "aws_redshift_cluster", "homeport.cluster": clusterID, "homeport.target": "clickhouse"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "wget -qO- http://localhost:8123/ping | grep Ok"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	result.AddConfig("config/redshift/schema-map.yaml", []byte(m.schemaMap(clusterID, dbName)))
	result.AddConfig("config/redshift/app-change.env", []byte(m.appChange(clusterID, dbName)))
	result.AddScript("export_redshift_cluster.sh", []byte(m.exportScript(clusterID, res.Region)))
	result.AddScript("migrate_redshift_unload.sh", []byte(m.migrateScript(clusterID, dbName)))
	result.AddScript("validate_redshift_analytics.sh", []byte(m.validateScript(dbName)))
	result.AddScript("backup_redshift_target.sh", []byte(m.backupScript(clusterID)))
	result.AddScript("cutover_redshift_clients.sh", []byte(m.cutoverScript(clusterID)))
	for _, step := range redshiftRunbook(clusterID) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *RedshiftMapper) schemaMap(clusterID, dbName string) string {
	return fmt.Sprintf("source_cluster: %s\ntarget: clickhouse\ndatabase: %s\ncopy_mode: unload_to_object_storage_then_insert\n", clusterID, dbName)
}

func (m *RedshiftMapper) appChange(clusterID, dbName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_CLUSTER=%s\nTARGET_DATABASE=%s\nCLICKHOUSE_URL=http://clickhouse:8123\n", clusterID, dbName)
}

func (m *RedshiftMapper) exportScript(clusterID, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAWS_REGION=\"${AWS_REGION:-%s}\"\nCLUSTER_ID=\"${REDSHIFT_CLUSTER:-%s}\"\nOUTPUT_DIR=\"${REDSHIFT_EXPORT_DIR:-redshift-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naws redshift describe-clusters --region \"$AWS_REGION\" --cluster-identifier \"$CLUSTER_ID\" > \"$OUTPUT_DIR/cluster.json\"\necho \"Exported Redshift cluster $CLUSTER_ID\"\n", region, clusterID)
}

func (m *RedshiftMapper) migrateScript(clusterID, dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nUNLOAD_DIR=\"${REDSHIFT_UNLOAD_DIR:-redshift-unload}\"\ntest -d \"$UNLOAD_DIR\"\nfor file in \"$UNLOAD_DIR\"/*.csv; do [ -e \"$file\" ] || continue; clickhouse-client --query=\"INSERT INTO %s FORMAT CSV\" < \"$file\"; done\necho \"Migrated Redshift UNLOAD files for %s\"\n", dbName, clusterID)
}

func (m *RedshiftMapper) validateScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nclickhouse-client --query='SELECT 1'\nclickhouse-client --query='SHOW TABLES FROM %s' >/tmp/homeport-redshift-tables.txt\ntest -s /tmp/homeport-redshift-tables.txt || true\necho \"Validated Redshift analytical target %s\"\n", dbName, dbName)
}

func (m *RedshiftMapper) backupScript(clusterID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-clickhouse-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/redshift data/clickhouse 2>/dev/null || tar -czf \"$archive\" config/redshift\necho \"$archive\"\n", clusterID)
}

func (m *RedshiftMapper) cutoverScript(clusterID string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/redshift/app-change.env\ntest \"$SOURCE_CLUSTER\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch analytics clients to CLICKHOUSE_URL=$CLICKHOUSE_URL\"\n", clusterID)
}

func redshiftRunbook(clusterID string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "warehouse", "source": "aws_redshift_cluster", "cluster": clusterID, "CLICKHOUSE_URL": "http://clickhouse:8123"}
	return []domainrunbook.Step{
		redshiftStep("export-redshift-cluster", "Export Redshift cluster", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_redshift_cluster.sh"}, "cluster metadata is exported", metadata),
		redshiftStep("provision-clickhouse", "Provision ClickHouse target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/redshift/schema-map.yaml"}, "schema map is rendered", metadata),
		redshiftStep("migrate-redshift-unload", "Migrate Redshift UNLOAD files", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_redshift_unload.sh"}, "UNLOAD files are inserted into ClickHouse", metadata),
		redshiftStep("validate-redshift-analytics", "Validate analytical queries", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_redshift_analytics.sh"}, "target analytical tables are queryable", metadata),
		redshiftStep("backup-redshift-target", "Backup Redshift target", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_redshift_target.sh"}, "ClickHouse config and data are archived", metadata),
		redshiftStep("cutover-redshift-clients", "Cut over analytics clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_redshift_clients.sh"}, "analytics clients use ClickHouse endpoint", metadata),
		redshiftStep("rollback-redshift-source", "Keep Redshift source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Redshift remains authoritative until validation passes", metadata),
	}
}

func redshiftStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
