package devops

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type ComposerMapper struct {
	*mapper.BaseMapper
}

func NewComposerMapper() *ComposerMapper {
	return &ComposerMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeComposerEnvironment, nil)}
}

func (m *ComposerMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	envName := firstNonEmpty(res.GetConfigString("name"), res.Name)
	result := mapper.NewMappingResult("airflow")
	svc := result.DockerService
	svc.Image = "apache/airflow:2.9.3"
	svc.Command = []string{"standalone"}
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{"./dags:/opt/airflow/dags", "./logs:/opt/airflow/logs", "./plugins:/opt/airflow/plugins"}
	svc.Environment = map[string]string{
		"AIRFLOW__CORE__EXECUTOR":             "LocalExecutor",
		"AIRFLOW__DATABASE__SQL_ALCHEMY_CONN": "sqlite:////opt/airflow/airflow.db",
		"COMPOSER_ENVIRONMENT":                envName,
	}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "airflow jobs check || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeComposerEnvironment), "homeport.environment": envName}

	result.AddConfig("docker-compose.airflow.yml", []byte(m.generateCompose(envName)))
	result.AddConfig("config/airflow/airflow.cfg", []byte(m.generateAirflowConfig(envName)))
	result.AddConfig("config/composer/app-change.env", []byte(m.generateAppChangeConfig(envName)))
	result.AddConfig("config/composer/environment-report.yaml", []byte(m.generateEnvironmentReport(res, envName)))
	result.AddScript("export_composer_environment.sh", []byte(m.generateExportScript(envName)))
	result.AddScript("migrate_composer_dags.sh", []byte(m.generateMigrateScript(envName)))
	result.AddScript("validate_composer_airflow.sh", []byte(m.generateValidateScript(envName)))
	result.AddScript("backup_composer_config.sh", []byte(m.generateBackupScript(envName)))
	result.AddScript("cutover_composer_clients.sh", []byte(m.generateCutoverScript(envName)))
	for _, step := range composerRunbook(envName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *ComposerMapper) generateCompose(envName string) string {
	return fmt.Sprintf(`services:
  airflow:
    image: apache/airflow:2.9.3
    command: standalone
    environment:
      AIRFLOW__CORE__EXECUTOR: LocalExecutor
      COMPOSER_ENVIRONMENT: %s
    ports:
      - "8080:8080"
    volumes:
      - ./dags:/opt/airflow/dags
      - ./logs:/opt/airflow/logs
      - ./plugins:/opt/airflow/plugins
`, envName)
}

func (m *ComposerMapper) generateAirflowConfig(envName string) string {
	return fmt.Sprintf("[core]\ndags_folder = /opt/airflow/dags\nexecutor = LocalExecutor\n\n[homeport]\ncomposer_environment = %s\n", envName)
}

func (m *ComposerMapper) generateAppChangeConfig(envName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_COMPOSER_ENVIRONMENT=%s\nTARGET_AIRFLOW_URL=http://airflow:8080\nTARGET_DAGS_PATH=./dags\n", envName)
}

func (m *ComposerMapper) generateEnvironmentReport(res *resource.AWSResource, envName string) string {
	return fmt.Sprintf("source: google_composer_environment\nenvironment: %s\ntarget: apache-airflow\nregion: %s\n", envName, res.GetConfigString("region"))
}

func (m *ComposerMapper) generateExportScript(envName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p composer-export dags plugins\ngcloud composer environments describe %q --format=json > composer-export/environment.json\n", envName)
}

func (m *ComposerMapper) generateMigrateScript(envName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -d dags\ntest -s config/composer/environment-report.yaml\necho \"Composer environment %s DAG path mapped to ./dags\"\n", envName)
}

func (m *ComposerMapper) generateValidateScript(envName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/airflow/airflow.cfg\ntest -d dags\necho \"Composer environment %s validated on Airflow\"\n", envName)
}

func (m *ComposerMapper) generateBackupScript(envName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/composer-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/airflow config/composer docker-compose.airflow.yml dags plugins\necho \"$archive\"\n", sanitizeDevOpsName(envName))
}

func (m *ComposerMapper) generateCutoverScript(envName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/composer/app-change.env\ntest \"$SOURCE_COMPOSER_ENVIRONMENT\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Composer clients to $TARGET_AIRFLOW_URL and DAG path $TARGET_DAGS_PATH\"\n", envName)
}

func composerRunbook(envName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "workflow-orchestration", "source": "google_composer_environment", "environment": envName, "target": "apache-airflow"}
	return []domainrunbook.Step{
		composerStep("export-composer-environment", "Export Composer environment", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_composer_environment.sh"}, "Composer environment config is exported", metadata),
		composerStep("provision-airflow", "Provision Airflow", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s docker-compose.airflow.yml"}, "Airflow compose target is rendered", metadata),
		composerStep("migrate-composer-dags", "Migrate Composer DAGs", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_composer_dags.sh"}, "DAG and plugin paths are mapped", metadata),
		composerStep("validate-airflow", "Validate Airflow", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_composer_airflow.sh"}, "Airflow config and DAG path validate", metadata),
		composerStep("backup-composer-config", "Backup Composer config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_composer_config.sh"}, "Composer migration artifacts are archived", metadata),
		composerStep("cutover-composer-clients", "Cut over Composer clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_composer_clients.sh"}, "clients point at Airflow target", metadata),
		composerStep("rollback-composer-source", "Keep Composer source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Composer remains authoritative until Airflow validation passes", metadata),
	}
}

func composerStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
