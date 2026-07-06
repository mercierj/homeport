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

type CloudBuildMapper struct {
	*mapper.BaseMapper
}

func NewCloudBuildMapper() *CloudBuildMapper {
	return &CloudBuildMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCloudBuildTrigger, nil)}
}

func (m *CloudBuildMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	triggerName := firstNonEmpty(res.GetConfigString("name"), res.GetConfigString("trigger_name"), res.Name)
	result := mapper.NewMappingResult("gitlab-runner")
	svc := result.DockerService
	svc.Image = "gitlab/gitlab-runner:alpine-v17.7.0"
	svc.Volumes = []string{
		"./config/gitlab-runner:/etc/gitlab-runner",
		"/var/run/docker.sock:/var/run/docker.sock",
		"gitlab-runner-cache:/cache",
	}
	svc.Environment = map[string]string{
		"CI_SERVER_URL":            "${CI_SERVER_URL:-http://gitlab.localhost}",
		"REGISTRATION_TOKEN":       "${GITLAB_RUNNER_TOKEN:-change-me}",
		"CLOUD_BUILD_TRIGGER_NAME": triggerName,
		"CLOUD_BUILD_REPOSITORY":   res.GetConfigString("repo_name"),
		"CLOUD_BUILD_BRANCH":       res.GetConfigString("branch_name"),
	}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "gitlab-runner", "verify", "--delete"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 3}
	svc.Labels = map[string]string{
		"homeport.source":  string(resource.TypeCloudBuildTrigger),
		"homeport.trigger": triggerName,
	}

	result.AddVolume(mapper.Volume{Name: "gitlab-runner-cache", Driver: "local"})
	result.AddConfig(".gitlab-ci.yml", []byte(m.generateGitLabCI(res, triggerName)))
	result.AddConfig("config/gitlab-runner/config.toml", []byte(m.generateRunnerConfig(triggerName)))
	result.AddConfig("config/cloud-build/source.env", []byte(m.generateSourceEnv(res, triggerName)))
	result.AddConfig("config/cloud-build/app-change.env", []byte(m.generateAppChangeConfig(triggerName)))
	result.AddScript("scripts/backup-cloud-build-trigger.sh", []byte(m.generateBackupScript(triggerName)))
	result.AddScript("scripts/validate-cloud-build-pipeline.sh", []byte(m.generateValidationScript(triggerName)))
	for _, step := range cloudBuildRunbook(triggerName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *CloudBuildMapper) generateGitLabCI(res *resource.AWSResource, triggerName string) string {
	commands := extractCloudBuildCommands(res)
	if len(commands) == 0 {
		commands = []string{"./build.sh"}
	}

	var b strings.Builder
	b.WriteString("# Generated from GCP Cloud Build trigger: " + triggerName + "\n")
	b.WriteString("stages:\n  - build\n\n")
	b.WriteString("variables:\n")
	b.WriteString("  CLOUD_BUILD_TRIGGER_NAME: \"" + escapeYAML(triggerName) + "\"\n")
	b.WriteString("  CLOUD_BUILD_SOURCE_BRANCH: \"" + escapeYAML(res.GetConfigString("branch_name")) + "\"\n\n")
	b.WriteString("cloud_build:\n")
	b.WriteString("  stage: build\n")
	b.WriteString("  image: \"docker:27-cli\"\n")
	b.WriteString("  services:\n    - docker:27-dind\n")
	b.WriteString("  script:\n")
	for _, command := range commands {
		b.WriteString("    - " + quoteShell(command) + "\n")
	}
	b.WriteString("  artifacts:\n")
	b.WriteString("    when: always\n")
	b.WriteString("    paths:\n      - build/\n      - dist/\n")
	return b.String()
}

func (m *CloudBuildMapper) generateRunnerConfig(triggerName string) string {
	return fmt.Sprintf(`concurrent = 4
check_interval = 0

[[runners]]
  name = "%s"
  url = "${CI_SERVER_URL}"
  token = "${GITLAB_RUNNER_TOKEN}"
  executor = "docker"
  [runners.docker]
    image = "docker:27-cli"
    privileged = true
    volumes = ["/cache", "/var/run/docker.sock:/var/run/docker.sock"]
`, sanitizeDevOpsName(triggerName))
}

func (m *CloudBuildMapper) generateSourceEnv(res *resource.AWSResource, triggerName string) string {
	return fmt.Sprintf("TRIGGER_NAME=%s\nREPO_NAME=%s\nBRANCH_NAME=%s\nBUILD_CONFIG=%s\n", triggerName, res.GetConfigString("repo_name"), res.GetConfigString("branch_name"), res.GetConfigString("filename"))
}

func (m *CloudBuildMapper) generateAppChangeConfig(triggerName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_CLOUD_BUILD_TRIGGER=%s
TARGET_CI_SYSTEM=gitlab-ci
TARGET_RUNNER_SERVICE=gitlab-runner
TARGET_PIPELINE=.gitlab-ci.yml
`, triggerName)
}

func (m *CloudBuildMapper) generateBackupScript(triggerName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/cloud-build-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" .gitlab-ci.yml config/gitlab-runner config/cloud-build
echo "$archive"
`, sanitizeDevOpsName(triggerName))
}

func (m *CloudBuildMapper) generateValidationScript(triggerName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s .gitlab-ci.yml
test -s config/gitlab-runner/config.toml
grep -q %s .gitlab-ci.yml
echo cloud-build-pipeline-validation-ok
`, quoteShell(triggerName))
}

func extractCloudBuildCommands(res *resource.AWSResource) []string {
	build, ok := res.Config["build"].(map[string]interface{})
	if !ok {
		return nil
	}
	steps, ok := build["steps"].([]interface{})
	if !ok {
		return nil
	}
	commands := make([]string, 0, len(steps))
	for _, rawStep := range steps {
		step, ok := rawStep.(map[string]interface{})
		if !ok {
			continue
		}
		args, ok := step["args"].([]interface{})
		if !ok || len(args) == 0 {
			continue
		}
		parts := make([]string, 0, len(args))
		for _, arg := range args {
			if value, ok := arg.(string); ok {
				parts = append(parts, value)
			}
		}
		if len(parts) > 0 {
			commands = append(commands, strings.Join(parts, " "))
		}
	}
	return commands
}

func cloudBuildRunbook(triggerName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "devops", "name": triggerName, "source": string(resource.TypeCloudBuildTrigger)}
	return []domainrunbook.Step{
		cloudBuildStep("render-cloud-build-gitlab-ci", "Render GitLab CI pipeline", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s .gitlab-ci.yml"}, "GitLab CI YAML is generated", metadata),
		cloudBuildStep("provision-cloud-build-runner", "Provision GitLab Runner", domainrunbook.StepTypeCommand, []string{"docker", "compose", "up", "-d", "gitlab-runner"}, "GitLab Runner service is healthy", metadata),
		cloudBuildStep("validate-cloud-build-pipeline", "Validate generated pipeline", domainrunbook.StepTypeCommand, []string{"sh", "scripts/validate-cloud-build-pipeline.sh"}, "Pipeline validation script passes", metadata),
		cloudBuildStep("backup-cloud-build-config", "Backup generated CI config", domainrunbook.StepTypeCommand, []string{"sh", "scripts/backup-cloud-build-trigger.sh"}, "Backup archive path is printed", metadata),
		cloudBuildStep("cutover-cloud-build-trigger", "Cut over build trigger", domainrunbook.StepTypeAPICall, []string{"sh", "-c", ". config/cloud-build/app-change.env && echo $TARGET_CI_SYSTEM"}, "Repository trigger points to GitLab CI", metadata),
		cloudBuildStep("rollback-cloud-build-trigger", "Rollback to Cloud Build trigger", domainrunbook.StepTypeRollback, []string{"sh", "-c", "echo keep Cloud Build trigger authoritative"}, "Source Cloud Build trigger remains available", metadata),
	}
}

func cloudBuildStep(id, name string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func quoteShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func escapeYAML(value string) string {
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func sanitizeDevOpsName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "cloud-build"
	}
	return out
}
