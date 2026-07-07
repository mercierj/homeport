package devops

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type DatabricksMapper struct{ *mapper.BaseMapper }

func NewDatabricksMapper() *DatabricksMapper {
	return &DatabricksMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAzureDatabricks, nil)}
}

func (m *DatabricksMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmpty(res.GetConfigString("name"), res.Name)
	result := mapper.NewMappingResult("spark-master")
	svc := result.DockerService
	svc.Image = "bitnami/spark:3.5"
	svc.Command = []string{"/opt/bitnami/scripts/spark/entrypoint.sh", "/opt/bitnami/scripts/spark/run.sh"}
	svc.Ports = []string{"8080:8080", "7077:7077"}
	svc.Volumes = []string{"./databricks/jobs:/opt/spark/jobs", "./databricks/data:/data"}
	svc.Environment = map[string]string{"SPARK_MODE": "master", "DATABRICKS_WORKSPACE": name}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "spark-submit --version >/dev/null 2>&1 || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeAzureDatabricks), "homeport.workspace": name, "homeport.target": "apache-spark"}

	result.AddConfig("docker-compose.databricks.yml", []byte(m.compose(name)))
	result.AddConfig("config/databricks/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/databricks/workspace-report.yaml", []byte(m.report(res, name)))
	result.AddScript("export_databricks_workspace.sh", []byte(m.exportScript(name)))
	result.AddScript("migrate_databricks_jobs.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_databricks_spark.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_databricks_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_databricks_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range databricksRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *DatabricksMapper) compose(name string) string {
	return fmt.Sprintf("services:\n  spark-master:\n    image: bitnami/spark:3.5\n    environment:\n      SPARK_MODE: master\n      DATABRICKS_WORKSPACE: %s\n    ports:\n      - \"8080:8080\"\n      - \"7077:7077\"\n", name)
}

func (m *DatabricksMapper) appChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_DATABRICKS_WORKSPACE=%s\nTARGET_SPARK_MASTER=spark://spark-master:7077\nTARGET_SPARK_UI=http://spark-master:8080\n", name)
}

func (m *DatabricksMapper) report(res *resource.AWSResource, name string) string {
	return fmt.Sprintf("source: azurerm_databricks_workspace\nworkspace: %s\ntarget: apache-spark\nregion: %s\n", name, res.Region)
}

func (m *DatabricksMapper) exportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p databricks-export databricks/jobs databricks/data\naz databricks workspace show --name %q > databricks-export/workspace.json\n", name)
}

func (m *DatabricksMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -d databricks/jobs\ntest -s config/databricks/workspace-report.yaml\necho \"Databricks workspace %s jobs mapped to Apache Spark\"\n", name)
}

func (m *DatabricksMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s docker-compose.databricks.yml\ntest -s config/databricks/app-change.env\necho \"Databricks workspace %s validated on Spark\"\n", name)
}

func (m *DatabricksMapper) backupScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/databricks-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/databricks docker-compose.databricks.yml databricks-export databricks/jobs\necho \"$archive\"\n", sanitizeName(name))
}

func (m *DatabricksMapper) cutoverScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/databricks/app-change.env\ntest \"$SOURCE_DATABRICKS_WORKSPACE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Databricks launchers to $TARGET_SPARK_MASTER\"\n", name)
}

func databricksRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "spark", "source": string(resource.TypeAzureDatabricks), "workspace": name, "target": "apache-spark"}
	return []domainrunbook.Step{
		databricksStep("export-databricks-workspace", "Export Databricks workspace", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_databricks_workspace.sh"}, "workspace metadata is exported", metadata),
		databricksStep("provision-databricks-spark", "Provision Spark target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s docker-compose.databricks.yml"}, "Spark target is rendered", metadata),
		databricksStep("migrate-databricks-jobs", "Migrate Databricks jobs", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_databricks_jobs.sh"}, "jobs are mapped to Spark", metadata),
		databricksStep("validate-databricks-spark", "Validate Spark target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_databricks_spark.sh"}, "Spark target config validates", metadata),
		databricksStep("backup-databricks-config", "Backup Databricks config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_databricks_config.sh"}, "migration artifacts are archived", metadata),
		databricksStep("cutover-databricks-clients", "Cut over Databricks clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_databricks_clients.sh"}, "launchers point at Spark", metadata),
		databricksStep("rollback-databricks-source", "Keep Databricks source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Databricks remains authoritative until Spark validation passes", metadata),
	}
}

func databricksStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
		command = nil
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
