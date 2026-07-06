package devops

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type CodeDeployMapper struct {
	*mapper.BaseMapper
}

func NewCodeDeployMapper() *CodeDeployMapper {
	return &CodeDeployMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCodeDeployApp, nil)}
}

func (m *CodeDeployMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	appName := firstNonEmpty(res.GetConfigString("app_name"), res.GetConfigString("name"), res.Name)
	if appName == "" {
		appName = "codedeploy-app"
	}
	groupName := firstNonEmpty(res.GetConfigString("deployment_group_name"), appName+"-group")

	result := mapper.NewMappingResult("argo-rollouts")
	svc := result.DockerService
	svc.Image = "quay.io/argoproj/argo-rollouts:v1.7.2"
	svc.Command = []string{"rollouts-controller"}
	svc.Volumes = []string{"./config/argo-rollouts:/etc/argo-rollouts"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{"homeport.source": "aws_codedeploy_app", "homeport.app": appName, "homeport.target": "argo-rollouts"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "argo-rollouts", "version", "--short"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 3}

	result.AddConfig("config/argo-rollouts/rollout.yaml", []byte(m.rollout(appName, groupName)))
	result.AddConfig("config/codedeploy/app-change.env", []byte(m.appChange(appName)))
	result.AddConfig("config/codedeploy/generated-rollout.patch", []byte(m.generatedPatch(appName)))
	result.AddScript("export_codedeploy_app.sh", []byte(m.exportScript(appName, groupName, res.Region)))
	result.AddScript("provision_argo_rollouts.sh", []byte(m.provisionScript(appName)))
	result.AddScript("migrate_codedeploy_group.sh", []byte(m.migrateScript(appName)))
	result.AddScript("validate_argo_rollout.sh", []byte(m.validateScript(appName)))
	result.AddScript("backup_codedeploy_config.sh", []byte(m.backupScript(appName)))
	result.AddScript("cutover_codedeploy_traffic.sh", []byte(m.cutoverScript(appName)))
	for _, step := range codeDeployRunbook(appName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *CodeDeployMapper) rollout(appName, groupName string) string {
	return fmt.Sprintf("apiVersion: argoproj.io/v1alpha1\nkind: Rollout\nmetadata:\n  name: %s\nspec:\n  strategy:\n    canary:\n      steps:\n        - setWeight: 20\n        - pause: {}\n  selector:\n    matchLabels:\n      app: %s\n  template:\n    metadata:\n      labels:\n        app: %s\n    spec:\n      containers: []\n---\n# source_deployment_group: %s\n", sanitizeDevOpsName(appName), sanitizeDevOpsName(appName), sanitizeDevOpsName(appName), groupName)
}

func (m *CodeDeployMapper) appChange(appName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_CODEDEPLOY_APP=%s\nTARGET_ROLLOUTS=argo-rollouts\nROLLOUT_NAMESPACE=default\n", appName)
}

func (m *CodeDeployMapper) generatedPatch(appName string) string {
	return fmt.Sprintf("--- deploy.env\n+++ deploy.env\n@@\n-CODEDEPLOY_APP=%s\n+ROLLOUT_CONTROLLER=argo-rollouts\n+ROLLOUT_NAMESPACE=default\n", appName)
}

func (m *CodeDeployMapper) exportScript(appName, groupName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAWS_REGION=\"${AWS_REGION:-%s}\"\nAPP_NAME=\"${CODEDEPLOY_APP:-%s}\"\nGROUP_NAME=\"${CODEDEPLOY_GROUP:-%s}\"\nOUTPUT_DIR=\"${CODEDEPLOY_EXPORT_DIR:-codedeploy-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naws deploy get-application --region \"$AWS_REGION\" --application-name \"$APP_NAME\" > \"$OUTPUT_DIR/application.json\"\naws deploy get-deployment-group --region \"$AWS_REGION\" --application-name \"$APP_NAME\" --deployment-group-name \"$GROUP_NAME\" > \"$OUTPUT_DIR/deployment-group.json\"\necho \"Exported CodeDeploy app $APP_NAME\"\n", region, appName, groupName)
}

func (m *CodeDeployMapper) provisionScript(appName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/argo-rollouts/rollout.yaml\necho \"Argo Rollout ready for CodeDeploy app %s\"\n", appName)
}

func (m *CodeDeployMapper) migrateScript(appName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s codedeploy-export/deployment-group.json\ngrep -q %q config/argo-rollouts/rollout.yaml\necho \"CodeDeploy deployment group mapped to Argo Rollouts\"\n", sanitizeDevOpsName(appName))
}

func (m *CodeDeployMapper) validateScript(appName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/argo-rollouts/rollout.yaml\ngrep -q %q config/codedeploy/app-change.env\n", appName)
}

func (m *CodeDeployMapper) backupScript(appName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-codedeploy-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/argo-rollouts config/codedeploy export_codedeploy_app.sh migrate_codedeploy_group.sh validate_argo_rollout.sh cutover_codedeploy_traffic.sh\necho \"$archive\"\n", sanitizeDevOpsName(appName))
}

func (m *CodeDeployMapper) cutoverScript(appName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/codedeploy/app-change.env\ntest \"$SOURCE_CODEDEPLOY_APP\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply rollout manifest and shift traffic through $TARGET_ROLLOUTS\"\n", appName)
}

func codeDeployRunbook(appName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "deployment", "source": "aws_codedeploy_app", "app": appName, "HOMEPORT_TARGET": "argo-rollouts", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		codeDeployStep("export-codedeploy-app", "Export CodeDeploy application", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_codedeploy_app.sh"}, "application and deployment group are exported", metadata),
		codeDeployStep("provision-argo-rollouts", "Provision Argo Rollouts", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_argo_rollouts.sh"}, "rollout manifest is present", metadata),
		codeDeployStep("migrate-codedeploy-group", "Migrate CodeDeploy deployment group", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_codedeploy_group.sh"}, "deployment group strategy is represented as a rollout", metadata),
		codeDeployStep("validate-argo-rollout", "Validate Argo Rollout", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_argo_rollout.sh"}, "rollout and app-change config validate", metadata),
		codeDeployStep("backup-codedeploy-config", "Backup CodeDeploy migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_codedeploy_config.sh"}, "deployment migration artifacts are archived", metadata),
		codeDeployStep("cutover-codedeploy-traffic", "Cut over deployment traffic", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_codedeploy_traffic.sh"}, "deployments use Argo Rollouts", metadata),
		codeDeployStep("rollback-codedeploy-source", "Keep CodeDeploy source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS CodeDeploy remains authoritative until rollout validation passes", metadata),
	}
}

func codeDeployStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
