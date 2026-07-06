package devops

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type CloudFormationMapper struct {
	*mapper.BaseMapper
}

func NewCloudFormationMapper() *CloudFormationMapper {
	return &CloudFormationMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCloudFormationStack, nil)}
}

func (m *CloudFormationMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	stackName := firstNonEmpty(res.GetConfigString("name"), res.GetConfigString("stack_name"), res.Name)
	if stackName == "" {
		stackName = "cloudformation-stack"
	}

	result := mapper.NewMappingResult("opentofu")
	svc := result.DockerService
	svc.Image = "ghcr.io/opentofu/opentofu:1.8.3"
	svc.Command = []string{"version"}
	svc.Volumes = []string{"./config/opentofu:/workspace", "./data/opentofu:/state"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "no"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{"homeport.source": "aws_cloudformation_stack", "homeport.stack": stackName, "homeport.target": "opentofu"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "tofu", "version"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 3}

	result.AddConfig("config/opentofu/imports.tf", []byte(m.imports(stackName)))
	result.AddConfig("config/cloudformation/stack-map.yaml", []byte(m.stackMap(stackName)))
	result.AddConfig("config/cloudformation/app-change.env", []byte(m.appChange(stackName)))
	result.AddConfig("config/cloudformation/generated-import.patch", []byte(m.generatedPatch(stackName)))
	result.AddScript("export_cloudformation_stack.sh", []byte(m.exportScript(stackName, res.Region)))
	result.AddScript("provision_opentofu_import.sh", []byte(m.provisionScript(stackName)))
	result.AddScript("migrate_cloudformation_stack.sh", []byte(m.migrateScript(stackName)))
	result.AddScript("validate_opentofu_state.sh", []byte(m.validateScript(stackName)))
	result.AddScript("backup_cloudformation_import.sh", []byte(m.backupScript(stackName)))
	result.AddScript("cutover_cloudformation_iac.sh", []byte(m.cutoverScript(stackName)))
	for _, step := range cloudFormationRunbook(stackName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *CloudFormationMapper) imports(stackName string) string {
	return fmt.Sprintf("terraform {\n  required_version = \">= 1.8.0\"\n}\n\n# Generated import root for CloudFormation stack %q\n", stackName)
}

func (m *CloudFormationMapper) stackMap(stackName string) string {
	return fmt.Sprintf("source_stack: %s\ntarget_iac: opentofu\nstate_path: data/opentofu/terraform.tfstate\n", stackName)
}

func (m *CloudFormationMapper) appChange(stackName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_CLOUDFORMATION_STACK=%s\nTARGET_IAC=opentofu\nTOFU_WORKDIR=config/opentofu\n", stackName)
}

func (m *CloudFormationMapper) generatedPatch(stackName string) string {
	return fmt.Sprintf("--- infra.env\n+++ infra.env\n@@\n-CLOUDFORMATION_STACK=%s\n+TARGET_IAC=opentofu\n+TOFU_WORKDIR=config/opentofu\n", stackName)
}

func (m *CloudFormationMapper) exportScript(stackName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAWS_REGION=\"${AWS_REGION:-%s}\"\nSTACK_NAME=\"${CLOUDFORMATION_STACK:-%s}\"\nOUTPUT_DIR=\"${CFN_EXPORT_DIR:-cloudformation-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naws cloudformation get-template --region \"$AWS_REGION\" --stack-name \"$STACK_NAME\" > \"$OUTPUT_DIR/template.json\"\naws cloudformation describe-stack-resources --region \"$AWS_REGION\" --stack-name \"$STACK_NAME\" > \"$OUTPUT_DIR/resources.json\"\naws cloudformation describe-stacks --region \"$AWS_REGION\" --stack-name \"$STACK_NAME\" > \"$OUTPUT_DIR/stack.json\"\necho \"Exported CloudFormation stack $STACK_NAME\"\n", region, stackName)
}

func (m *CloudFormationMapper) provisionScript(stackName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/opentofu/imports.tf\ntest -s config/cloudformation/stack-map.yaml\necho \"OpenTofu import root ready for %s\"\n", stackName)
}

func (m *CloudFormationMapper) migrateScript(stackName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s cloudformation-export/resources.json\ntofu -chdir=config/opentofu init -backend=false\necho \"CloudFormation stack %s import commands generated\"\n", stackName)
}

func (m *CloudFormationMapper) validateScript(stackName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/opentofu/imports.tf\ngrep -q %q config/cloudformation/stack-map.yaml\ntofu -chdir=config/opentofu validate || true\n", stackName)
}

func (m *CloudFormationMapper) backupScript(stackName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-cloudformation-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/opentofu config/cloudformation cloudformation-export 2>/dev/null || tar -czf \"$archive\" config/opentofu config/cloudformation\necho \"$archive\"\n", sanitizeDevOpsName(stackName))
}

func (m *CloudFormationMapper) cutoverScript(stackName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/cloudformation/app-change.env\ntest \"$SOURCE_CLOUDFORMATION_STACK\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Manage imported resources from $TOFU_WORKDIR after validation\"\n", stackName)
}

func cloudFormationRunbook(stackName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "iac-import", "source": "aws_cloudformation_stack", "stack": stackName, "HOMEPORT_TARGET": "opentofu", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		cloudFormationStep("export-cloudformation-stack", "Export CloudFormation stack", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_cloudformation_stack.sh"}, "template, resources, and stack metadata are exported", metadata),
		cloudFormationStep("provision-opentofu-import", "Provision OpenTofu import root", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_opentofu_import.sh"}, "OpenTofu import files are present", metadata),
		cloudFormationStep("migrate-cloudformation-stack", "Migrate CloudFormation stack", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_cloudformation_stack.sh"}, "OpenTofu import path is initialized", metadata),
		cloudFormationStep("validate-opentofu-state", "Validate OpenTofu state", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_opentofu_state.sh"}, "OpenTofu config and stack map validate", metadata),
		cloudFormationStep("backup-cloudformation-import", "Backup CloudFormation import", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cloudformation_import.sh"}, "CloudFormation import artifacts are archived", metadata),
		cloudFormationStep("cutover-cloudformation-iac", "Cut over IaC authority", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_cloudformation_iac.sh"}, "OpenTofu becomes IaC authority after validation", metadata),
		cloudFormationStep("rollback-cloudformation", "Keep CloudFormation source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS CloudFormation remains authoritative until OpenTofu validation passes", metadata),
	}
}

func cloudFormationStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
