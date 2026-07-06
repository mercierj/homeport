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

// CodeBuildMapper converts AWS CodeBuild projects to GitLab CI with GitLab Runner.
type CodeBuildMapper struct {
	*mapper.BaseMapper
}

func NewCodeBuildMapper() *CodeBuildMapper {
	return &CodeBuildMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeCodeBuild, nil)}
}

func (m *CodeBuildMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	projectName := firstNonEmpty(res.GetConfigString("name"), res.GetConfigString("project_name"), res.Name)
	result := mapper.NewMappingResult("gitlab-runner")
	svc := result.DockerService
	svc.Image = "gitlab/gitlab-runner:alpine-v17.7.0"
	svc.Volumes = []string{
		"./config/gitlab-runner:/etc/gitlab-runner",
		"/var/run/docker.sock:/var/run/docker.sock",
		"gitlab-runner-cache:/cache",
	}
	svc.Environment = map[string]string{
		"CI_SERVER_URL":             "${CI_SERVER_URL:-http://gitlab.localhost}",
		"REGISTRATION_TOKEN":        "${GITLAB_RUNNER_TOKEN:-change-me}",
		"CODEBUILD_PROJECT_NAME":    projectName,
		"CODEBUILD_SOURCE_LOCATION": configString(res, "source.location", "source", "location"),
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
	svc.Labels = map[string]string{
		"homeport.source":  string(resource.TypeCodeBuild),
		"homeport.project": projectName,
	}

	result.AddVolume(mapper.Volume{Name: "gitlab-runner-cache", Driver: "local"})
	result.AddConfig(".gitlab-ci.yml", []byte(m.generateGitLabCI(res, projectName)))
	result.AddConfig("config/gitlab-runner/config.toml", []byte(m.generateRunnerConfig(projectName)))
	result.AddConfig("config/codebuild/source.env", []byte(m.generateSourceEnv(res, projectName)))
	result.AddScript("scripts/backup-codebuild-project.sh", []byte(m.generateBackupScript(projectName)))
	result.AddScript("scripts/validate-codebuild-pipeline.sh", []byte(m.generateValidationScript(projectName)))
	for _, step := range codeBuildRunbook(projectName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *CodeBuildMapper) generateGitLabCI(res *resource.AWSResource, projectName string) string {
	commands := extractBuildCommands(configString(res, "source.buildspec", "source", "buildspec"))
	if len(commands) == 0 {
		commands = []string{"./build.sh"}
	}
	image := firstNonEmpty(configString(res, "environment.image", "environment", "image"), "docker:27-cli")

	var b strings.Builder
	b.WriteString("# Generated from AWS CodeBuild project: " + projectName + "\n")
	b.WriteString("stages:\n  - build\n\n")
	b.WriteString("variables:\n")
	b.WriteString("  CODEBUILD_PROJECT_NAME: \"" + escapeYAML(projectName) + "\"\n")
	b.WriteString("  CODEBUILD_SOURCE_VERSION: \"${CI_COMMIT_SHA}\"\n\n")
	b.WriteString("codebuild_build:\n")
	b.WriteString("  stage: build\n")
	b.WriteString("  image: \"" + escapeYAML(image) + "\"\n")
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

func (m *CodeBuildMapper) generateRunnerConfig(projectName string) string {
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
`, sanitizeDevOpsName(projectName))
}

func (m *CodeBuildMapper) generateSourceEnv(res *resource.AWSResource, projectName string) string {
	return fmt.Sprintf("PROJECT_NAME=%s\nSOURCE_TYPE=%s\nSOURCE_LOCATION=%s\nARTIFACT_TYPE=%s\n", projectName, configString(res, "source.type", "source", "type"), configString(res, "source.location", "source", "location"), configString(res, "artifacts.type", "artifacts", "type"))
}

func (m *CodeBuildMapper) generateBackupScript(projectName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/codebuild-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" .gitlab-ci.yml config/gitlab-runner config/codebuild
echo "$archive"
`, sanitizeDevOpsName(projectName))
}

func (m *CodeBuildMapper) generateValidationScript(projectName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s .gitlab-ci.yml
test -s config/gitlab-runner/config.toml
grep -q %s .gitlab-ci.yml
echo codebuild-pipeline-validation-ok
`, quoteShell(projectName))
}

func extractBuildCommands(buildspec string) []string {
	var commands []string
	inCommands := false
	for _, line := range strings.Split(buildspec, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "commands:") {
			inCommands = true
			continue
		}
		if inCommands && strings.HasPrefix(trimmed, "- ") {
			commands = append(commands, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			continue
		}
		if inCommands && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "- ") {
			inCommands = false
		}
	}
	return commands
}

func codeBuildRunbook(projectName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "devops", "name": projectName, "source": string(resource.TypeCodeBuild)}
	return []domainrunbook.Step{
		codeBuildStep("render-codebuild-gitlab-ci", "Render GitLab CI pipeline", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s .gitlab-ci.yml"}, "GitLab CI YAML is generated", metadata),
		codeBuildStep("provision-gitlab-runner", "Provision GitLab Runner", domainrunbook.StepTypeCommand, []string{"docker", "compose", "up", "-d", "gitlab-runner"}, "GitLab Runner service is healthy", metadata),
		codeBuildStep("validate-codebuild-pipeline", "Validate generated pipeline", domainrunbook.StepTypeCommand, []string{"sh", "scripts/validate-codebuild-pipeline.sh"}, "Pipeline validation script passes", metadata),
		codeBuildStep("backup-codebuild-config", "Backup generated CI config", domainrunbook.StepTypeCommand, []string{"sh", "scripts/backup-codebuild-project.sh"}, "Backup archive path is printed", metadata),
		codeBuildStep("cutover-codebuild-webhook", "Cut over build webhook", domainrunbook.StepTypeAPICall, []string{"sh", "-c", ". config/codebuild/source.env && echo $SOURCE_LOCATION"}, "Repository webhook points to GitLab CI", metadata),
		codeBuildStep("rollback-codebuild-source", "Rollback to CodeBuild project", domainrunbook.StepTypeRollback, []string{"sh", "-c", "echo keep CodeBuild project authoritative"}, "Source CodeBuild project remains available", metadata),
	}
}

func codeBuildStep(id, name string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func configString(res *resource.AWSResource, flatKey, section, key string) string {
	if value := res.GetConfigString(flatKey); value != "" {
		return value
	}
	if nested, ok := res.Config[section].(map[string]interface{}); ok {
		if value, ok := nested[key].(string); ok {
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
		return "codebuild"
	}
	return out
}
