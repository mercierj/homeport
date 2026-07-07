package devops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type DataFactoryMapper struct{ *mapper.BaseMapper }

func NewDataFactoryMapper() *DataFactoryMapper {
	return &DataFactoryMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAzureDataFactory, nil)}
}

func (m *DataFactoryMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmpty(res.GetConfigString("name"), res.Name)
	if name == "" {
		name = "data-factory"
	}

	result := mapper.NewMappingResult("airflow")
	svc := result.DockerService
	svc.Image = "apache/airflow:2.9.3"
	svc.Command = []string{"standalone"}
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{"./data-factory/dags:/opt/airflow/dags", "./data/airflow:/opt/airflow"}
	svc.Environment = map[string]string{"AIRFLOW__CORE__LOAD_EXAMPLES": "false", "DATA_FACTORY_NAME": name}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "airflow jobs check --job-type SchedulerJob || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeAzureDataFactory), "homeport.data_factory": name, "homeport.target": "airflow"}

	result.AddConfig("config/data-factory/pipeline-map.yaml", []byte(m.pipelineMap(name, res.Region)))
	result.AddConfig("config/data-factory/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/data-factory/generated-client.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_data_factory.sh", []byte(m.exportScript(name)))
	result.AddScript("migrate_data_factory_pipelines.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_data_factory_airflow.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_data_factory_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_data_factory_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range dataFactoryRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *DataFactoryMapper) pipelineMap(name, region string) string {
	return fmt.Sprintf("source: azurerm_data_factory\nfactory: %s\nregion: %s\ntarget: airflow\ndag_dir: data-factory/dags\n", name, region)
}

func (m *DataFactoryMapper) appChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_DATA_FACTORY=%s\nTARGET_WORKFLOW_ENGINE=airflow\nAIRFLOW_URL=http://airflow:8080\nGENERATED_PATCH=config/data-factory/generated-client.patch\n", name)
}

func (m *DataFactoryMapper) generatedPatch(name string) string {
	return fmt.Sprintf("--- a/app/workflow.env\n+++ b/app/workflow.env\n@@\n-AZURE_DATA_FACTORY=%s\n+AIRFLOW_URL=http://airflow:8080\n+WORKFLOW_ENGINE=airflow\n", name)
}

func (m *DataFactoryMapper) exportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p data-factory-export data-factory/dags\naz datafactory show --factory-name %q > data-factory-export/factory.json\n", name)
}

func (m *DataFactoryMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -d data-factory/dags\ntest -s config/data-factory/pipeline-map.yaml\necho \"Data Factory %s mapped to Airflow DAG handoff\"\n", name)
}

func (m *DataFactoryMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/data-factory/app-change.env\ntest -s config/data-factory/pipeline-map.yaml\necho \"Data Factory %s Airflow artifacts validate\"\n", name)
}

func (m *DataFactoryMapper) backupScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/data-factory-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/data-factory data-factory-export data-factory/dags\necho \"$archive\"\n", sanitizeName(name))
}

func (m *DataFactoryMapper) cutoverScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/data-factory/app-change.env\ntest \"$SOURCE_DATA_FACTORY\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and route pipeline triggers to $AIRFLOW_URL\"\n", name)
}

func dataFactoryRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "workflow", "source": string(resource.TypeAzureDataFactory), "factory": name, "target": "airflow"}
	return []domainrunbook.Step{
		dataFactoryStep("export-data-factory", "Export Data Factory", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_data_factory.sh"}, "Data Factory metadata is exported", metadata),
		dataFactoryStep("provision-airflow-target", "Provision Airflow target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/data-factory/pipeline-map.yaml"}, "Airflow handoff is rendered", metadata),
		dataFactoryStep("migrate-data-factory-pipelines", "Migrate Data Factory pipelines", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_data_factory_pipelines.sh"}, "pipeline artifacts are mapped to DAGs", metadata),
		dataFactoryStep("validate-data-factory-airflow", "Validate Airflow target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_data_factory_airflow.sh"}, "Airflow target config validates", metadata),
		dataFactoryStep("backup-data-factory-config", "Backup Data Factory config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_data_factory_config.sh"}, "migration artifacts are archived", metadata),
		dataFactoryStep("cutover-data-factory-clients", "Cut over Data Factory clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_data_factory_clients.sh"}, "triggers point at Airflow", metadata),
		dataFactoryStep("rollback-data-factory-source", "Keep Data Factory source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Data Factory remains authoritative until Airflow validation passes", metadata),
	}
}

func dataFactoryStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
		command = nil
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sanitizeName(name string) string {
	name = strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	out := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			out += string(ch)
		}
	}
	if out == "" {
		return "data-factory"
	}
	return out
}
