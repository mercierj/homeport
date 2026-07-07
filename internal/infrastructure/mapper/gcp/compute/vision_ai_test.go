package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestVisionAIConformanceManagedAToZ(t *testing.T) {
	result, err := NewVisionAIMapper().Map(context.Background(), managedVisionAIFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Vision AI migration", result.ManualSteps)
	}
	if result.DockerService.Image != "jrottenberg/opencv:4.6.0-ubuntu" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OpenCV target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.vision-ai.yml", "config/vision-ai/app-change.env", "config/vision-ai/service-report.yaml", "config/vision-ai/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/vision-ai/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_VISION_AI_SERVICE=vision-api", "TARGET_VISION_BACKEND=opencv"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_vision_ai_service.sh", "migrate_vision_ai_service.sh", "validate_vision_ai_service.sh", "backup_vision_ai_config.sh", "cutover_vision_ai_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-vision-ai-service":   domainrunbook.StepTypeCommand,
		"provision-opencv-vision":    domainrunbook.StepTypeCommand,
		"migrate-vision-ai-service":  domainrunbook.StepTypeCommand,
		"validate-opencv-vision":     domainrunbook.StepTypeCommand,
		"backup-vision-ai-config":    domainrunbook.StepTypeCommand,
		"cutover-vision-ai-clients":  domainrunbook.StepTypeAPICall,
		"rollback-vision-ai-service": domainrunbook.StepTypeRollback,
	} {
		if !hasVisionAIRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewVisionAIMapper(t *testing.T) {
	if NewVisionAIMapper().ResourceType() != resource.TypeVisionAIService {
		t.Fatalf("Vision AI mapper type = %s, want %s", NewVisionAIMapper().ResourceType(), resource.TypeVisionAIService)
	}
}

func managedVisionAIFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/services/vision.googleapis.com",
		Type: resource.TypeVisionAIService,
		Name: "vision-api",
		Config: map[string]interface{}{
			"name":     "vision-api",
			"service":  "vision.googleapis.com",
			"location": "global",
		},
	}
}

func hasVisionAIRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
