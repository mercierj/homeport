package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCodeBuildConformanceManagedAToZ(t *testing.T) {
	result, err := NewCodeBuildMapper().Map(context.Background(), managedCodeBuildFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated CI migration", result.ManualSteps)
	}
	if result.DockerService.Image != "gitlab/gitlab-runner:alpine-v17.7.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA GitLab Runner: %#v", result.DockerService)
	}
	for _, file := range []string{".gitlab-ci.yml", "config/gitlab-runner/config.toml", "config/codebuild/source.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	ci := string(result.Configs[".gitlab-ci.yml"])
	for _, want := range []string{"npm ci", "npm test", "docker build -t app .", "CODEBUILD_PROJECT_NAME"} {
		if !strings.Contains(ci, want) {
			t.Fatalf("GitLab CI config missing %q:\n%s", want, ci)
		}
	}
	if strings.Contains(ci, "TODO") {
		t.Fatalf("GitLab CI config contains TODO:\n%s", ci)
	}
	for _, file := range []string{"scripts/backup-codebuild-project.sh", "scripts/validate-codebuild-pipeline.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-codebuild-gitlab-ci":  domainrunbook.StepTypeCommand,
		"provision-gitlab-runner":     domainrunbook.StepTypeCommand,
		"validate-codebuild-pipeline": domainrunbook.StepTypeCommand,
		"backup-codebuild-config":     domainrunbook.StepTypeCommand,
		"cutover-codebuild-webhook":   domainrunbook.StepTypeAPICall,
		"rollback-codebuild-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasCodeBuildRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedCodeBuildFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "arn:aws:codebuild:us-east-1:123456789012:project/app-build",
		Type: resource.TypeCodeBuild,
		Name: "app-build",
		Config: map[string]interface{}{
			"name": "app-build",
			"source": map[string]interface{}{
				"type":     "GITHUB",
				"location": "https://github.com/example/app",
				"buildspec": `version: 0.2
phases:
  install:
    commands:
      - npm ci
  build:
    commands:
      - npm test
      - docker build -t app .
`,
			},
			"environment": map[string]interface{}{
				"image": "node:22-alpine",
			},
			"artifacts": map[string]interface{}{
				"type": "NO_ARTIFACTS",
			},
		},
	}
}

func hasCodeBuildRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
