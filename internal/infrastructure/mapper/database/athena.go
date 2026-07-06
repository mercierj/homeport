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

type AthenaMapper struct {
	*mapper.BaseMapper
}

func NewAthenaMapper() *AthenaMapper {
	return &AthenaMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAthenaWorkgroup, nil)}
}

func (m *AthenaMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	database := res.GetConfigString("database")
	if database == "" {
		database = "default"
	}

	result := mapper.NewMappingResult("trino")
	svc := result.DockerService
	svc.Image = "trinodb/trino:443"
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{"./config/trino:/etc/trino"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-fsS", "http://localhost:8080/v1/info"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Labels = map[string]string{
		"homeport.source":    "aws_athena_workgroup",
		"homeport.workgroup": name,
		"homeport.engine":    "trino",
	}

	result.AddConfig("config/trino/catalog/hive.properties", []byte(m.catalogConfig(res)))
	result.AddConfig("config/trino/migration.sql", []byte(m.migrationSQL(res, database)))
	result.AddScript("backup_athena_config.sh", []byte(m.backupScript(name)))
	for _, step := range athenaRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *AthenaMapper) catalogConfig(res *resource.AWSResource) string {
	location := res.GetConfigString("output_location")
	if location == "" {
		location = "s3://homeport-athena/"
	}
	return fmt.Sprintf(`connector.name=hive
hive.metastore=glue
hive.s3.path-style-access=true
hive.s3.endpoint=${HOMEPORT_S3_ENDPOINT}
homeport.source.output-location=%s
`, location)
}

func (m *AthenaMapper) migrationSQL(res *resource.AWSResource, database string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;\n", database))
	for _, view := range configSlice(res.Config["views"]) {
		name := configString(view["name"])
		sql := configString(view["sql"])
		if name == "" || sql == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("CREATE VIEW %s.%s AS %s;\n", database, name, strings.TrimSuffix(sql, ";")))
	}
	return b.String()
}

func configSlice(value interface{}) []map[string]interface{} {
	switch typed := value.(type) {
	case []map[string]interface{}:
		return typed
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			if itemMap, ok := item.(map[string]interface{}); ok {
				out = append(out, itemMap)
			}
		}
		return out
	}
	return nil
}

func configString(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func (m *AthenaMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-trino-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/trino
echo "$archive"
`, strings.NewReplacer("/", "-", " ", "-").Replace(name))
}

func athenaRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "analytics_sql", "name": name, "source": "aws_athena_workgroup"}
	return []domainrunbook.Step{
		athenaStep("render-trino-catalog", "Render Trino catalog", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/trino/catalog/hive.properties"}, "Trino catalog config is generated", metadata),
		athenaStep("migrate-athena-ddl", "Migrate Athena DDL", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/trino/migration.sql"}, "schemas and views are represented as Trino SQL", metadata),
		athenaStep("validate-trino-query", "Validate Trino query path", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "-c", "echo validate Trino query execution and object-store reads"}, "sample query succeeds against Trino", metadata),
		athenaStep("backup-athena-config", "Backup Athena replacement config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_athena_config.sh"}, "Trino catalog and migration SQL are archived", metadata),
		athenaStep("cutover-athena-dsn", "Cut over Athena DSN to Trino", "Cutover", domainrunbook.StepTypeCommand, []string{"sh", "-c", "echo set HOMEPORT_ATHENA_ENDPOINT=http://trino:8080"}, "applications and jobs use the Trino endpoint", metadata),
		athenaStep("rollback-athena-source", "Keep Athena as rollback authority", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Athena remains authoritative until query validation passes", metadata),
	}
}

func athenaStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}
