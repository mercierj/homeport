package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCodePipelineConformanceManagedAToZ(t *testing.T) {
	result, err := NewCodePipelineMapper().Map(context.Background(), managedCodePipelineFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated pipeline migration", result.ManualSteps)
	}
	if result.DockerService.Image != "gitlab/gitlab-runner:alpine-v17.7.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA GitLab Runner: %#v", result.DockerService)
	}
	for _, file := range []string{".gitlab-ci.yml", "config/gitlab-runner/config.toml", "config/codepipeline/pipeline.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	ci := string(result.Configs[".gitlab-ci.yml"])
	for _, want := range []string{"stages:", "source-checkout", "build-compile", "deploy-release", "CODEPIPELINE_NAME"} {
		if !strings.Contains(ci, want) {
			t.Fatalf("GitLab CI config missing %q:\n%s", want, ci)
		}
	}
	if strings.Contains(ci, "TODO") {
		t.Fatalf("GitLab CI config contains TODO:\n%s", ci)
	}
	for _, file := range []string{"scripts/backup-codepipeline.sh", "scripts/validate-codepipeline.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-codepipeline-gitlab-ci": domainrunbook.StepTypeCommand,
		"provision-codepipeline-runner": domainrunbook.StepTypeCommand,
		"validate-codepipeline":         domainrunbook.StepTypeCommand,
		"backup-codepipeline-config":    domainrunbook.StepTypeCommand,
		"cutover-codepipeline-webhook":  domainrunbook.StepTypeAPICall,
		"rollback-codepipeline-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasCodePipelineRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedCodePipelineFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "arn:aws:codepipeline:us-east-1:123456789012:shop-release",
		Type: resource.TypeCodePipeline,
		Name: "shop-release",
		Config: map[string]interface{}{
			"name": "shop-release",
			"stage": []interface{}{
				map[string]interface{}{
					"name": "source",
					"action": []interface{}{
						map[string]interface{}{"name": "checkout", "provider": "GitHub", "category": "Source"},
					},
				},
				map[string]interface{}{
					"name": "build",
					"action": []interface{}{
						map[string]interface{}{"name": "compile", "provider": "CodeBuild", "category": "Build"},
					},
				},
				map[string]interface{}{
					"name": "deploy",
					"action": []interface{}{
						map[string]interface{}{"name": "release", "provider": "CodeDeploy", "category": "Deploy"},
					},
				},
			},
		},
	}
}

func hasCodePipelineRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
