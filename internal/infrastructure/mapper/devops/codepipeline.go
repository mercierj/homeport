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

type CodePipelineMapper struct {
	*mapper.BaseMapper
}

func NewCodePipelineMapper() *CodePipelineMapper {
	return &CodePipelineMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCodePipeline, nil)}
}

func (m *CodePipelineMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	pipelineName := firstNonEmpty(res.GetConfigString("name"), res.GetConfigString("pipeline_name"), res.Name)
	stages := pipelineStages(res)
	if len(stages) == 0 {
		stages = []pipelineStage{{name: "build", actions: []pipelineAction{{name: "build", provider: "CodeBuild"}}}}
	}

	result := mapper.NewMappingResult("gitlab-runner")
	svc := result.DockerService
	svc.Image = "gitlab/gitlab-runner:alpine-v17.7.0"
	svc.Volumes = []string{"./config/gitlab-runner:/etc/gitlab-runner", "/var/run/docker.sock:/var/run/docker.sock", "gitlab-runner-cache:/cache"}
	svc.Environment = map[string]string{
		"CI_SERVER_URL":           "${CI_SERVER_URL:-http://gitlab.localhost}",
		"REGISTRATION_TOKEN":      "${GITLAB_RUNNER_TOKEN:-change-me}",
		"CODEPIPELINE_NAME":       pipelineName,
		"CODEPIPELINE_STAGE_LIST": stageNames(stages),
	}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "gitlab-runner", "verify", "--delete"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeCodePipeline), "homeport.pipeline": pipelineName}

	result.AddVolume(mapper.Volume{Name: "gitlab-runner-cache", Driver: "local"})
	result.AddConfig(".gitlab-ci.yml", []byte(m.generateGitLabCI(pipelineName, stages)))
	result.AddConfig("config/gitlab-runner/config.toml", []byte(m.generateRunnerConfig(pipelineName)))
	result.AddConfig("config/codepipeline/pipeline.env", []byte(m.generatePipelineEnv(pipelineName, stages)))
	result.AddScript("scripts/backup-codepipeline.sh", []byte(m.generateBackupScript(pipelineName)))
	result.AddScript("scripts/validate-codepipeline.sh", []byte(m.generateValidationScript(pipelineName)))
	for _, step := range codePipelineRunbook(pipelineName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *CodePipelineMapper) generateGitLabCI(pipelineName string, stages []pipelineStage) string {
	var b strings.Builder
	b.WriteString("# Generated from AWS CodePipeline: " + pipelineName + "\n")
	b.WriteString("stages:\n")
	for _, stage := range stages {
		b.WriteString("  - " + sanitizeDevOpsName(stage.name) + "\n")
	}
	b.WriteString("\nvariables:\n")
	b.WriteString("  CODEPIPELINE_NAME: \"" + escapeYAML(pipelineName) + "\"\n\n")
	for _, stage := range stages {
		for _, action := range stage.actions {
			job := sanitizeDevOpsName(stage.name + "-" + action.name)
			b.WriteString(job + ":\n")
			b.WriteString("  stage: " + sanitizeDevOpsName(stage.name) + "\n")
			b.WriteString("  image: alpine:3.20\n")
			b.WriteString("  script:\n")
			b.WriteString("    - " + quoteShell(actionCommand(action)) + "\n")
			b.WriteString("  artifacts:\n")
			b.WriteString("    when: always\n")
			b.WriteString("    paths:\n      - build/\n      - dist/\n\n")
		}
	}
	return b.String()
}

func (m *CodePipelineMapper) generatePipelineEnv(pipelineName string, stages []pipelineStage) string {
	return fmt.Sprintf("PIPELINE_NAME=%s\nPIPELINE_STAGES=%s\n", pipelineName, stageNames(stages))
}

func (m *CodePipelineMapper) generateRunnerConfig(pipelineName string) string {
	return fmt.Sprintf(`concurrent = 4
check_interval = 0

[[runners]]
  name = "%s"
  url = "${CI_SERVER_URL}"
  token = "${GITLAB_RUNNER_TOKEN}"
  executor = "docker"
  [runners.docker]
    image = "alpine:3.20"
    privileged = true
    volumes = ["/cache", "/var/run/docker.sock:/var/run/docker.sock"]
`, sanitizeDevOpsName(pipelineName))
}

func (m *CodePipelineMapper) generateBackupScript(pipelineName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/codepipeline-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" .gitlab-ci.yml config/gitlab-runner config/codepipeline
echo "$archive"
`, sanitizeDevOpsName(pipelineName))
}

func (m *CodePipelineMapper) generateValidationScript(pipelineName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s .gitlab-ci.yml
test -s config/codepipeline/pipeline.env
grep -q %s .gitlab-ci.yml
echo codepipeline-validation-ok
`, quoteShell(pipelineName))
}

type pipelineStage struct {
	name    string
	actions []pipelineAction
}

type pipelineAction struct {
	name     string
	provider string
	category string
}

func pipelineStages(res *resource.AWSResource) []pipelineStage {
	var stages []pipelineStage
	for _, rawStage := range mapList(res.Config["stage"]) {
		stage := pipelineStage{name: firstNonEmpty(stringMapValue(rawStage, "name"), "stage")}
		for _, rawAction := range mapList(rawStage["action"]) {
			stage.actions = append(stage.actions, pipelineAction{
				name:     firstNonEmpty(stringMapValue(rawAction, "name"), "action"),
				provider: stringMapValue(rawAction, "provider"),
				category: stringMapValue(rawAction, "category"),
			})
		}
		if len(stage.actions) == 0 {
			stage.actions = []pipelineAction{{name: stage.name, provider: "Shell"}}
		}
		stages = append(stages, stage)
	}
	return stages
}

func actionCommand(action pipelineAction) string {
	switch strings.ToLower(action.provider) {
	case "codebuild":
		return "echo run migrated CodeBuild job via generated GitLab CI"
	case "codedeploy":
		return "echo run deployment using generated GitLab environment job"
	case "s3":
		return "echo fetch or publish artifact using configured object storage"
	case "github", "codestarconnections":
		return "echo source is provided by GitLab checkout"
	default:
		if action.category != "" {
			return "echo run migrated " + action.category + " action " + action.name
		}
		return "echo run migrated action " + action.name
	}
}

func stageNames(stages []pipelineStage) string {
	names := make([]string, 0, len(stages))
	for _, stage := range stages {
		names = append(names, stage.name)
	}
	return strings.Join(names, ",")
}

func mapList(value interface{}) []map[string]interface{} {
	switch typed := value.(type) {
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]interface{}); ok {
				out = append(out, mapped)
			}
		}
		return out
	case []map[string]interface{}:
		return typed
	case map[string]interface{}:
		return []map[string]interface{}{typed}
	default:
		return nil
	}
}

func stringMapValue(values map[string]interface{}, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func codePipelineRunbook(pipelineName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "devops", "name": pipelineName, "source": string(resource.TypeCodePipeline)}
	return []domainrunbook.Step{
		codeBuildStep("render-codepipeline-gitlab-ci", "Render GitLab CI pipeline", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s .gitlab-ci.yml"}, "GitLab CI YAML is generated", metadata),
		codeBuildStep("provision-codepipeline-runner", "Provision GitLab Runner", domainrunbook.StepTypeCommand, []string{"docker", "compose", "up", "-d", "gitlab-runner"}, "GitLab Runner service is healthy", metadata),
		codeBuildStep("validate-codepipeline", "Validate generated pipeline", domainrunbook.StepTypeCommand, []string{"sh", "scripts/validate-codepipeline.sh"}, "Pipeline validation script passes", metadata),
		codeBuildStep("backup-codepipeline-config", "Backup generated pipeline config", domainrunbook.StepTypeCommand, []string{"sh", "scripts/backup-codepipeline.sh"}, "Backup archive path is printed", metadata),
		codeBuildStep("cutover-codepipeline-webhook", "Cut over repository webhook", domainrunbook.StepTypeAPICall, []string{"sh", "-c", ". config/codepipeline/pipeline.env && echo $PIPELINE_STAGES"}, "Repository webhook points to GitLab CI", metadata),
		codeBuildStep("rollback-codepipeline-source", "Rollback to CodePipeline", domainrunbook.StepTypeRollback, []string{"sh", "-c", "echo keep CodePipeline authoritative"}, "Source CodePipeline remains available", metadata),
	}
}
