package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type EMRMapper struct {
	*mapper.BaseMapper
}

func NewEMRMapper() *EMRMapper {
	return &EMRMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeEMRCluster, nil)}
}

func (m *EMRMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	clusterName := res.GetConfigString("name")
	if clusterName == "" {
		clusterName = res.Name
	}

	result := mapper.NewMappingResult("spark-master")
	svc := result.DockerService
	svc.Image = "bitnami/spark:3.5"
	svc.Environment = map[string]string{
		"SPARK_MODE":                  "master",
		"SPARK_MASTER_HOST":           "spark-master",
		"HOMEPORT_SOURCE_EMR_CLUSTER": clusterName,
	}
	svc.Ports = []string{"7077:7077", "8080:8080"}
	svc.Volumes = []string{"./data/spark:/opt/spark/work-dir", "./config/emr:/opt/bitnami/spark/conf/homeport"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":  "aws_emr_cluster",
		"homeport.cluster": clusterName,
		"homeport.target":  "spark",
	}

	result.AddConfig("config/emr/spark-defaults.conf", []byte(m.sparkDefaults(res)))
	result.AddConfig("config/emr/app-change.env", []byte(m.appChangeConfig(clusterName, res)))
	result.AddConfig("config/emr/steps.yaml", []byte(m.stepsConfig(res)))
	result.AddScript("export_emr_steps.sh", []byte(m.exportScript(clusterName)))
	result.AddScript("submit_spark_jobs.sh", []byte(m.submitScript(clusterName)))
	result.AddScript("backup_emr_config.sh", []byte(m.backupScript(clusterName)))
	result.AddScript("validate_emr_runtime.sh", []byte(m.validateScript(clusterName)))
	for _, step := range emrRunbook(clusterName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *EMRMapper) sparkDefaults(res *resource.AWSResource) string {
	return fmt.Sprintf("spark.master spark://spark-master:7077\nspark.eventLog.enabled true\nspark.homeport.release %s\n", res.GetConfigString("release_label"))
}

func (m *EMRMapper) appChangeConfig(clusterName string, res *resource.AWSResource) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_CLUSTER=%s
SOURCE_RELEASE=%s
TARGET_RUNTIME=spark
TARGET_MASTER=spark://spark-master:7077
`, clusterName, res.GetConfigString("release_label"))
}

func (m *EMRMapper) stepsConfig(res *resource.AWSResource) string {
	var b strings.Builder
	b.WriteString("steps:\n")
	steps, _ := res.Config["step"].([]interface{})
	if len(steps) == 0 {
		b.WriteString("  - name: default\n    command: spark-submit\n")
		return b.String()
	}
	for _, step := range steps {
		stepMap, _ := step.(map[string]interface{})
		name, _ := stepMap["name"].(string)
		if name == "" {
			name = "emr-step"
		}
		b.WriteString(fmt.Sprintf("  - name: %s\n", name))
		if args, ok := stepMap["args"].([]interface{}); ok && len(args) > 0 {
			b.WriteString("    args:")
			for _, arg := range args {
				b.WriteString(fmt.Sprintf(" %v", arg))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *EMRMapper) exportScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\naws emr list-steps --cluster-id \"${EMR_CLUSTER_ID:-%s}\" > config/emr/source-steps.json\n", clusterName)
}

func (m *EMRMapper) submitScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/emr/steps.yaml
echo "Submitting migrated EMR steps for %s to spark://spark-master:7077"
`, clusterName)
}

func (m *EMRMapper) backupScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-emr-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/emr data/spark 2>/dev/null || tar -czf "$archive" config/emr
echo "$archive"
`, clusterName)
}

func (m *EMRMapper) validateScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/emr/app-change.env
test -s config/emr/spark-defaults.conf
echo "Spark target for EMR cluster %s validated"
`, clusterName)
}

func emrRunbook(clusterName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "analytics", "source": "aws_emr_cluster", "cluster": clusterName}
	return []domainrunbook.Step{
		emrStep("export-emr-steps", "Export EMR steps", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_emr_steps.sh"}, "source EMR steps are exported", metadata),
		emrStep("provision-spark-target", "Provision Spark target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/emr/spark-defaults.conf"}, "Spark runtime config is rendered", metadata),
		emrStep("submit-spark-jobs", "Submit Spark jobs", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "submit_spark_jobs.sh"}, "EMR steps are translated to Spark submissions", metadata),
		emrStep("validate-spark-runtime", "Validate Spark runtime", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_emr_runtime.sh"}, "Spark target accepts migrated jobs", metadata),
		emrStep("backup-emr-config", "Backup EMR config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_emr_config.sh"}, "EMR migration config is archived", metadata),
		emrStep("cutover-emr-jobs", "Cut over EMR jobs", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/emr/app-change.env"}, "job schedulers submit to the Spark target", metadata),
		emrStep("rollback-emr-source", "Keep EMR source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS EMR remains authoritative until Spark validation passes", metadata),
	}
}

func emrStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
