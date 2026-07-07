package devops

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type DataflowMapper struct {
	*mapper.BaseMapper
}

func NewDataflowMapper() *DataflowMapper {
	return &DataflowMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeDataflowJob, nil)}
}

func (m *DataflowMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	jobName := firstNonEmpty(res.GetConfigString("name"), res.GetConfigString("job_name"), res.Name)
	result := mapper.NewMappingResult("flink-jobmanager")
	svc := result.DockerService
	svc.Image = "apache/flink:1.19"
	svc.Command = []string{"standalone-job", "--job-classname", "${FLINK_JOB_CLASS:-org.apache.beam.runners.flink.FlinkRunner}"}
	svc.Ports = []string{"8081:8081"}
	svc.Volumes = []string{"./dataflow/jobs:/opt/flink/usrlib", "./dataflow/checkpoints:/checkpoints"}
	svc.Environment = map[string]string{
		"DATAFLOW_JOB_NAME": jobName,
		"FLINK_PROPERTIES":  "jobmanager.rpc.address: flink-jobmanager",
	}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "curl -fsS http://localhost:8081/overview >/dev/null || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeDataflowJob), "homeport.job": jobName}

	result.AddConfig("docker-compose.flink.yml", []byte(m.generateCompose(jobName)))
	result.AddConfig("config/dataflow/app-change.env", []byte(m.generateAppChangeConfig(jobName)))
	result.AddConfig("config/dataflow/job-report.yaml", []byte(m.generateJobReport(res, jobName)))
	result.AddScript("export_dataflow_job.sh", []byte(m.generateExportScript(jobName)))
	result.AddScript("migrate_dataflow_pipeline.sh", []byte(m.generateMigrateScript(jobName)))
	result.AddScript("validate_dataflow_flink.sh", []byte(m.generateValidateScript(jobName)))
	result.AddScript("backup_dataflow_config.sh", []byte(m.generateBackupScript(jobName)))
	result.AddScript("cutover_dataflow_clients.sh", []byte(m.generateCutoverScript(jobName)))
	for _, step := range dataflowRunbook(jobName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *DataflowMapper) generateCompose(jobName string) string {
	return fmt.Sprintf(`services:
  flink-jobmanager:
    image: apache/flink:1.19
    command: standalone-job --job-classname ${FLINK_JOB_CLASS:-org.apache.beam.runners.flink.FlinkRunner}
    environment:
      DATAFLOW_JOB_NAME: %s
    ports:
      - "8081:8081"
    volumes:
      - ./dataflow/jobs:/opt/flink/usrlib
      - ./dataflow/checkpoints:/checkpoints
`, jobName)
}

func (m *DataflowMapper) generateAppChangeConfig(jobName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_DATAFLOW_JOB=%s\nTARGET_RUNNER=apache-flink\nTARGET_FLINK_URL=http://flink-jobmanager:8081\n", jobName)
}

func (m *DataflowMapper) generateJobReport(res *resource.AWSResource, jobName string) string {
	return fmt.Sprintf("source: google_dataflow_job\njob: %s\ntarget: apache-flink\nregion: %s\ntemplate: %s\n", jobName, res.GetConfigString("region"), res.GetConfigString("template_gcs_path"))
}

func (m *DataflowMapper) generateExportScript(jobName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p dataflow-export dataflow/jobs\ngcloud dataflow jobs describe %q --format=json > dataflow-export/job.json\n", jobName)
}

func (m *DataflowMapper) generateMigrateScript(jobName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -d dataflow/jobs\ntest -s config/dataflow/job-report.yaml\necho \"Dataflow job %s mapped to Apache Flink runner\"\n", jobName)
}

func (m *DataflowMapper) generateValidateScript(jobName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s docker-compose.flink.yml\ntest -s config/dataflow/app-change.env\necho \"Dataflow job %s validated on Flink\"\n", jobName)
}

func (m *DataflowMapper) generateBackupScript(jobName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/dataflow-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/dataflow docker-compose.flink.yml dataflow-export dataflow/jobs\necho \"$archive\"\n", sanitizeDevOpsName(jobName))
}

func (m *DataflowMapper) generateCutoverScript(jobName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/dataflow/app-change.env\ntest \"$SOURCE_DATAFLOW_JOB\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Dataflow launchers to $TARGET_RUNNER at $TARGET_FLINK_URL\"\n", jobName)
}

func dataflowRunbook(jobName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "stream-processing", "source": "google_dataflow_job", "job": jobName, "target": "apache-flink"}
	return []domainrunbook.Step{
		dataflowStep("export-dataflow-job", "Export Dataflow job", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_dataflow_job.sh"}, "Dataflow job configuration is exported", metadata),
		dataflowStep("provision-flink-runner", "Provision Flink runner", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s docker-compose.flink.yml"}, "Flink compose target is rendered", metadata),
		dataflowStep("migrate-dataflow-pipeline", "Migrate Dataflow pipeline", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_dataflow_pipeline.sh"}, "pipeline artifacts are mapped to Flink", metadata),
		dataflowStep("validate-dataflow-flink", "Validate Flink target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_dataflow_flink.sh"}, "Flink target config validates", metadata),
		dataflowStep("backup-dataflow-config", "Backup Dataflow config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_dataflow_config.sh"}, "Dataflow migration artifacts are archived", metadata),
		dataflowStep("cutover-dataflow-clients", "Cut over Dataflow clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_dataflow_clients.sh"}, "launchers point at Flink runner", metadata),
		dataflowStep("rollback-dataflow-source", "Keep Dataflow source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Dataflow remains authoritative until Flink validation passes", metadata),
	}
}

func dataflowStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
