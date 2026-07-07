package devops

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type CloudDeployMapper struct {
	*mapper.BaseMapper
}

func NewCloudDeployPipelineMapper() *CloudDeployMapper {
	return &CloudDeployMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCloudDeployDeliveryPipeline, nil)}
}

func NewCloudDeployTargetMapper() *CloudDeployMapper {
	return &CloudDeployMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCloudDeployTarget, nil)}
}

func (m *CloudDeployMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmpty(res.GetConfigString("name"), res.GetConfigString("delivery_pipeline_id"), res.Name)
	if name == "" {
		name = "cloud-deploy"
	}
	kind := "delivery-pipeline"
	if res.Type == resource.TypeCloudDeployTarget {
		kind = "target"
	}

	result := mapper.NewMappingResult("argocd")
	svc := result.DockerService
	svc.Image = "quay.io/argoproj/argocd:v2.11.4"
	svc.Command = []string{"argocd-server", "--insecure", "--staticassets", "/shared/app"}
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{"./config/argocd:/etc/argocd", "./data/argocd:/var/lib/argocd"}
	svc.Environment = map[string]string{"CLOUD_DEPLOY_NAME": name, "CLOUD_DEPLOY_KIND": kind}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "argocd", "version", "--client"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 3}
	svc.Labels = map[string]string{"homeport.source": string(res.Type), "homeport.cloud_deploy": name, "homeport.target": "argocd"}

	result.AddConfig("config/argocd/application.yaml", []byte(m.application(name, kind)))
	result.AddConfig("config/clouddeploy/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/clouddeploy/generated-argocd.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_cloud_deploy_pipeline.sh", []byte(m.exportScript(name, kind, res.GetConfigString("region"))))
	result.AddScript("provision_argocd_app.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_cloud_deploy_targets.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_argocd_app.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_cloud_deploy_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_cloud_deploy_releases.sh", []byte(m.cutoverScript(name)))
	for _, step := range cloudDeployRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *CloudDeployMapper) application(name, kind string) string {
	return fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
spec:
  project: default
  source:
    repoURL: ${GITOPS_REPO_URL}
    targetRevision: HEAD
    path: deploy/%s
  destination:
    server: https://kubernetes.default.svc
    namespace: default
---
# source_cloud_deploy_kind: %s
`, sanitizeDevOpsName(name), sanitizeDevOpsName(name), kind)
}

func (m *CloudDeployMapper) appChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_CLOUD_DEPLOY_PIPELINE=%s\nTARGET_GITOPS=argocd\nARGOCD_APP=%s\n", name, sanitizeDevOpsName(name))
}

func (m *CloudDeployMapper) generatedPatch(name string) string {
	return fmt.Sprintf("--- deploy.env\n+++ deploy.env\n@@\n-CLOUD_DEPLOY_PIPELINE=%s\n+GITOPS_CONTROLLER=argocd\n+ARGOCD_APP=%s\n", name, sanitizeDevOpsName(name))
}

func (m *CloudDeployMapper) exportScript(name, kind, region string) string {
	if region == "" {
		region = "global"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nCLOUD_DEPLOY_NAME=%q\nCLOUD_DEPLOY_KIND=%q\nREGION=%q\nOUTPUT_DIR=\"${CLOUD_DEPLOY_EXPORT_DIR:-clouddeploy-export}\"\nmkdir -p \"$OUTPUT_DIR\"\ngcloud deploy \"$CLOUD_DEPLOY_KIND\"s describe \"$CLOUD_DEPLOY_NAME\" --region \"$REGION\" --format=json > \"$OUTPUT_DIR/$CLOUD_DEPLOY_KIND.json\"\necho \"Exported Cloud Deploy $CLOUD_DEPLOY_KIND $CLOUD_DEPLOY_NAME\"\n", name, kind, region)
}

func (m *CloudDeployMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/argocd/application.yaml\necho \"Argo CD application ready for Cloud Deploy pipeline %s\"\n", name)
}

func (m *CloudDeployMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/argocd/application.yaml\ngrep -q %q config/argocd/application.yaml\necho \"Cloud Deploy targets mapped to Argo CD application\"\n", sanitizeDevOpsName(name))
}

func (m *CloudDeployMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/argocd/application.yaml\ngrep -q %q config/clouddeploy/app-change.env\n", name)
}

func (m *CloudDeployMapper) backupScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-clouddeploy-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/argocd config/clouddeploy clouddeploy-export 2>/dev/null || tar -czf \"$archive\" config/argocd config/clouddeploy\necho \"$archive\"\n", sanitizeDevOpsName(name))
}

func (m *CloudDeployMapper) cutoverScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/clouddeploy/app-change.env\ntest \"$SOURCE_CLOUD_DEPLOY_PIPELINE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply Argo CD application $ARGOCD_APP and route releases through $TARGET_GITOPS\"\n", name)
}

func cloudDeployRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "deployment", "source": "google_clouddeploy", "pipeline": name, "HOMEPORT_TARGET": "argocd", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		cloudDeployStep("export-cloud-deploy-pipeline", "Export Cloud Deploy pipeline", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_cloud_deploy_pipeline.sh"}, "delivery pipeline and target metadata are exported", metadata),
		cloudDeployStep("provision-argocd-app", "Provision Argo CD application", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_argocd_app.sh"}, "Argo CD application manifest is present", metadata),
		cloudDeployStep("migrate-cloud-deploy-targets", "Migrate Cloud Deploy targets", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_cloud_deploy_targets.sh"}, "targets are represented as GitOps destinations", metadata),
		cloudDeployStep("validate-argocd-app", "Validate Argo CD application", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_argocd_app.sh"}, "Argo CD application and app-change config validate", metadata),
		cloudDeployStep("backup-cloud-deploy-config", "Backup Cloud Deploy config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloud_deploy_config.sh"}, "deployment migration artifacts are archived", metadata),
		cloudDeployStep("cutover-cloud-deploy-releases", "Cut over Cloud Deploy releases", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_cloud_deploy_releases.sh"}, "releases use Argo CD GitOps flow", metadata),
		cloudDeployStep("rollback-cloud-deploy-source", "Keep Cloud Deploy source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Cloud Deploy remains authoritative until Argo CD validation passes", metadata),
	}
}

func cloudDeployStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
