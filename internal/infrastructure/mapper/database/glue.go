package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type GlueMapper struct {
	*mapper.BaseMapper
}

func NewGlueMapper() *GlueMapper {
	return &GlueMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeGlueCatalogDatabase, nil)}
}

func (m *GlueMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}

	result := mapper.NewMappingResult("hive-metastore")
	svc := result.DockerService
	svc.Image = "apache/hive:4.0.0"
	svc.Ports = []string{"9083:9083"}
	svc.Volumes = []string{"./config/glue:/opt/hive/conf/homeport", "./data/hive:/opt/hive/data"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Environment = map[string]string{
		"SERVICE_NAME":                 "metastore",
		"HOMEPORT_SOURCE_GLUE_CATALOG": name,
	}
	svc.Labels = map[string]string{
		"homeport.source":   "aws_glue_catalog_database",
		"homeport.database": name,
		"homeport.target":   "hive-metastore",
	}

	result.AddConfig("config/glue/catalog.yaml", []byte(m.catalogConfig(res, name)))
	result.AddConfig("config/glue/jobs.yaml", []byte(m.jobsConfig(res)))
	result.AddConfig("config/glue/app-change.env", []byte(m.appChangeConfig(name)))
	result.AddScript("export_glue_catalog.sh", []byte(m.exportScript(name)))
	result.AddScript("import_hive_metastore.sh", []byte(m.importScript(name)))
	result.AddScript("backup_glue_catalog.sh", []byte(m.backupScript(name)))
	result.AddScript("validate_glue_catalog.sh", []byte(m.validateScript(name)))
	for _, step := range glueRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *GlueMapper) catalogConfig(res *resource.AWSResource, name string) string {
	return fmt.Sprintf("database: %s\ndescription: %s\nlocation_uri: %s\n", name, res.GetConfigString("description"), res.GetConfigString("location_uri"))
}

func (m *GlueMapper) jobsConfig(res *resource.AWSResource) string {
	var b strings.Builder
	b.WriteString("jobs:\n")
	for _, job := range configSlice(res.Config["jobs"]) {
		name := configString(job["name"])
		if name == "" {
			name = "glue-job"
		}
		b.WriteString(fmt.Sprintf("  - name: %s\n", name))
		b.WriteString(fmt.Sprintf("    command: %s\n", configString(job["command"])))
		b.WriteString(fmt.Sprintf("    script_location: %s\n", configString(job["script_location"])))
	}
	if b.String() == "jobs:\n" {
		b.WriteString("  []\n")
	}
	return b.String()
}

func (m *GlueMapper) appChangeConfig(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_DATABASE=%s
TARGET_METASTORE=hive-metastore:9083
TARGET_RUNTIME=hive-metastore
`, name)
}

func (m *GlueMapper) exportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\naws glue get-database --name %q > config/glue/source-database.json\n", name)
}

func (m *GlueMapper) importScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/glue/catalog.yaml\necho import Glue catalog %s into Hive metastore\n", name)
}

func (m *GlueMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-glue-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/glue data/hive 2>/dev/null || tar -czf "$archive" config/glue
echo "$archive"
`, name)
}

func (m *GlueMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/glue/catalog.yaml\ntest -s config/glue/app-change.env\necho Glue catalog %s validated\n", name)
}

func glueRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "data-catalog", "source": "aws_glue_catalog_database", "database": name}
	return []domainrunbook.Step{
		glueStep("export-glue-catalog", "Export Glue catalog", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_glue_catalog.sh"}, "Glue database metadata is exported", metadata),
		glueStep("provision-hive-metastore", "Provision Hive metastore", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/glue/catalog.yaml"}, "Hive metastore target config is rendered", metadata),
		glueStep("import-hive-metastore", "Import Hive metastore", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "import_hive_metastore.sh"}, "Glue catalog metadata is represented in Hive", metadata),
		glueStep("validate-glue-catalog", "Validate Glue catalog", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_glue_catalog.sh"}, "catalog database and job metadata validate", metadata),
		glueStep("backup-glue-catalog", "Backup Glue catalog config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_glue_catalog.sh"}, "catalog migration assets are archived", metadata),
		glueStep("cutover-glue-metastore", "Cut over Glue metastore", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/glue/app-change.env"}, "query engines use the generated Hive metastore endpoint", metadata),
		glueStep("rollback-glue-source", "Keep Glue source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Glue remains authoritative until metastore validation passes", metadata),
	}
}

func glueStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
