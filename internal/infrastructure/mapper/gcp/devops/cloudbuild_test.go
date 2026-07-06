package devops

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudBuildConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudBuildMapper().Map(context.Background(), managedCloudBuildFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud Build migration", result.ManualSteps)
	}
	if result.DockerService.Image != "gitlab/gitlab-runner:alpine-v17.7.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA GitLab Runner: %#v", result.DockerService)
	}
	for _, file := range []string{
		".gitlab-ci.yml",
		"config/gitlab-runner/config.toml",
		"config/cloud-build/source.env",
		"config/cloud-build/app-change.env",
	} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	ci := string(result.Configs[".gitlab-ci.yml"])
	for _, want := range []string{"npm ci", "npm test", "docker build -t app .", "CLOUD_BUILD_TRIGGER_NAME"} {
		if !strings.Contains(ci, want) {
			t.Fatalf("GitLab CI config missing %q:\n%s", want, ci)
		}
	}
	appEnv := string(result.Configs["config/cloud-build/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_BUILD_TRIGGER=app-build", "TARGET_CI_SYSTEM=gitlab-ci"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"scripts/backup-cloud-build-trigger.sh", "scripts/validate-cloud-build-pipeline.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-cloud-build-gitlab-ci":  domainrunbook.StepTypeCommand,
		"provision-cloud-build-runner":  domainrunbook.StepTypeCommand,
		"validate-cloud-build-pipeline": domainrunbook.StepTypeCommand,
		"backup-cloud-build-config":     domainrunbook.StepTypeCommand,
		"cutover-cloud-build-trigger":   domainrunbook.StepTypeAPICall,
		"rollback-cloud-build-trigger":  domainrunbook.StepTypeRollback,
	} {
		if !hasCloudBuildRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCloudBuildMapper(t *testing.T) {
	m := NewCloudBuildMapper()
	if m == nil {
		t.Fatal("NewCloudBuildMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudBuildTrigger {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudBuildTrigger)
	}
}

func managedCloudBuildFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/triggers/app-build",
		Type: resource.TypeCloudBuildTrigger,
		Name: "app-build",
		Config: map[string]interface{}{
			"name":        "app-build",
			"repo_name":   "app",
			"branch_name": "main",
			"filename":    "cloudbuild.yaml",
			"build": map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"name": "node:22-alpine", "args": []interface{}{"npm", "ci"}},
					map[string]interface{}{"name": "node:22-alpine", "args": []interface{}{"npm", "test"}},
					map[string]interface{}{"name": "docker:27-cli", "args": []interface{}{"docker", "build", "-t", "app", "."}},
				},
			},
		},
	}
}

func hasCloudBuildRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
