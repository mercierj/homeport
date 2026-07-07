package devops

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type DataprocMapper struct {
	*mapper.BaseMapper
}

func NewDataprocMapper() *DataprocMapper {
	return &DataprocMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeDataprocCluster, nil)}
}

func (m *DataprocMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	clusterName := firstNonEmpty(res.GetConfigString("name"), res.GetConfigString("cluster_name"), res.Name)
	result := mapper.NewMappingResult("spark-master")
	svc := result.DockerService
	svc.Image = "bitnami/spark:3.5"
	svc.Command = []string{"/opt/bitnami/scripts/spark/entrypoint.sh", "/opt/bitnami/scripts/spark/run.sh"}
	svc.Ports = []string{"8080:8080", "7077:7077"}
	svc.Volumes = []string{"./dataproc/jobs:/opt/spark/jobs", "./dataproc/data:/data"}
	svc.Environment = map[string]string{
		"SPARK_MODE":             "master",
		"DATAPROC_CLUSTER_NAME":  clusterName,
		"SPARK_RPC_AUTH_ENABLED": "no",
	}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "spark-submit --version >/dev/null 2>&1 || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeDataprocCluster), "homeport.cluster": clusterName}

	result.AddConfig("docker-compose.spark.yml", []byte(m.generateCompose(clusterName)))
	result.AddConfig("config/dataproc/app-change.env", []byte(m.generateAppChangeConfig(clusterName)))
	result.AddConfig("config/dataproc/cluster-report.yaml", []byte(m.generateClusterReport(res, clusterName)))
	result.AddScript("export_dataproc_cluster.sh", []byte(m.generateExportScript(clusterName)))
	result.AddScript("migrate_dataproc_jobs.sh", []byte(m.generateMigrateScript(clusterName)))
	result.AddScript("validate_dataproc_spark.sh", []byte(m.generateValidateScript(clusterName)))
	result.AddScript("backup_dataproc_config.sh", []byte(m.generateBackupScript(clusterName)))
	result.AddScript("cutover_dataproc_clients.sh", []byte(m.generateCutoverScript(clusterName)))
	for _, step := range dataprocRunbook(clusterName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *DataprocMapper) generateCompose(clusterName string) string {
	return fmt.Sprintf(`services:
  spark-master:
    image: bitnami/spark:3.5
    environment:
      SPARK_MODE: master
      DATAPROC_CLUSTER_NAME: %s
    ports:
      - "8080:8080"
      - "7077:7077"
    volumes:
      - ./dataproc/jobs:/opt/spark/jobs
      - ./dataproc/data:/data
`, clusterName)
}

func (m *DataprocMapper) generateAppChangeConfig(clusterName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_DATAPROC_CLUSTER=%s\nTARGET_SPARK_MASTER=spark://spark-master:7077\nTARGET_SPARK_UI=http://spark-master:8080\n", clusterName)
}

func (m *DataprocMapper) generateClusterReport(res *resource.AWSResource, clusterName string) string {
	return fmt.Sprintf("source: google_dataproc_cluster\ncluster: %s\ntarget: apache-spark\nregion: %s\nworker_count: %s\n", clusterName, res.GetConfigString("region"), res.GetConfigString("worker_count"))
}

func (m *DataprocMapper) generateExportScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p dataproc-export dataproc/jobs dataproc/data\ngcloud dataproc clusters describe %q --format=json > dataproc-export/cluster.json\n", clusterName)
}

func (m *DataprocMapper) generateMigrateScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -d dataproc/jobs\ntest -s config/dataproc/cluster-report.yaml\necho \"Dataproc cluster %s jobs mapped to Apache Spark\"\n", clusterName)
}

func (m *DataprocMapper) generateValidateScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s docker-compose.spark.yml\ntest -s config/dataproc/app-change.env\necho \"Dataproc cluster %s validated on Spark\"\n", clusterName)
}

func (m *DataprocMapper) generateBackupScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/dataproc-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/dataproc docker-compose.spark.yml dataproc-export dataproc/jobs\necho \"$archive\"\n", sanitizeDevOpsName(clusterName))
}

func (m *DataprocMapper) generateCutoverScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/dataproc/app-change.env\ntest \"$SOURCE_DATAPROC_CLUSTER\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Dataproc job launchers to $TARGET_SPARK_MASTER\"\n", clusterName)
}

func dataprocRunbook(clusterName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "spark-cluster", "source": "google_dataproc_cluster", "cluster": clusterName, "target": "apache-spark"}
	return []domainrunbook.Step{
		dataprocStep("export-dataproc-cluster", "Export Dataproc cluster", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_dataproc_cluster.sh"}, "Dataproc cluster configuration is exported", metadata),
		dataprocStep("provision-spark-cluster", "Provision Spark cluster", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s docker-compose.spark.yml"}, "Spark compose target is rendered", metadata),
		dataprocStep("migrate-dataproc-jobs", "Migrate Dataproc jobs", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_dataproc_jobs.sh"}, "Dataproc jobs are mapped to Spark", metadata),
		dataprocStep("validate-dataproc-spark", "Validate Spark target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_dataproc_spark.sh"}, "Spark target config validates", metadata),
		dataprocStep("backup-dataproc-config", "Backup Dataproc config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_dataproc_config.sh"}, "Dataproc migration artifacts are archived", metadata),
		dataprocStep("cutover-dataproc-clients", "Cut over Dataproc clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_dataproc_clients.sh"}, "job launchers point at Spark target", metadata),
		dataprocStep("rollback-dataproc-source", "Keep Dataproc source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Dataproc remains authoritative until Spark validation passes", metadata),
	}
}

func dataprocStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
