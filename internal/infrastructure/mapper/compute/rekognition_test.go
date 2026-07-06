package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestRekognitionConformanceManagedAToZ(t *testing.T) {
	result, err := NewRekognitionMapper().Map(context.Background(), managedRekognitionFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Rekognition migration", result.ManualSteps)
	}
	if result.DockerService.Image != "opencv/opencv:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA OpenCV target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/rekognition/vision-pipeline.yaml", "config/rekognition/app-change.env", "config/rekognition/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/rekognition/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_REKOGNITION_COLLECTION=faces", "TARGET_VISION_ENGINE=opencv"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_rekognition_assets.sh", "provision_opencv_pipeline.sh", "migrate_rekognition_assets.sh", "validate_opencv_pipeline.sh", "backup_rekognition_config.sh", "cutover_rekognition_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-rekognition-assets":   domainrunbook.StepTypeCommand,
		"provision-opencv-pipeline":   domainrunbook.StepTypeCommand,
		"migrate-rekognition-assets":  domainrunbook.StepTypeCommand,
		"validate-opencv-pipeline":    domainrunbook.StepTypeCommand,
		"backup-rekognition-config":   domainrunbook.StepTypeCommand,
		"cutover-rekognition-clients": domainrunbook.StepTypeAPICall,
		"rollback-rekognition-source": domainrunbook.StepTypeRollback,
	} {
		if !hasRekognitionRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewRekognitionMapper(t *testing.T) {
	m := NewRekognitionMapper()
	if m == nil {
		t.Fatal("NewRekognitionMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeRekognitionCollection {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeRekognitionCollection)
	}
}

func managedRekognitionFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "faces",
		Type:   resource.TypeRekognitionCollection,
		Name:   "faces",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"collection_id": "faces",
		},
	}
}

func hasRekognitionRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
