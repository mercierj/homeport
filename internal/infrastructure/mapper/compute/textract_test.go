package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestTextractConformanceManagedAToZ(t *testing.T) {
	result, err := NewTextractMapper().Map(context.Background(), managedTextractFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Textract migration", result.ManualSteps)
	}
	if result.DockerService.Image != "ghcr.io/ocrmypdf/ocrmypdf:v16.10.4" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Tesseract OCR target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/textract/ocr-pipeline.yaml", "config/textract/adapter-map.yaml", "config/textract/app-change.env", "config/textract/generated-ocr.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/textract/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_TEXTRACT_ADAPTER=invoice-adapter", "TARGET_OCR_ENGINE=tesseract", "TARGET_OCR_COMMAND=ocrmypdf"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_textract_adapters.sh", "provision_tesseract_ocr.sh", "migrate_textract_adapters.sh", "validate_tesseract_ocr.sh", "backup_textract_config.sh", "cutover_textract_pipeline.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-textract-adapters":  domainrunbook.StepTypeCommand,
		"provision-tesseract-ocr":   domainrunbook.StepTypeCommand,
		"migrate-textract-adapters": domainrunbook.StepTypeCommand,
		"validate-tesseract-ocr":    domainrunbook.StepTypeCommand,
		"backup-textract-config":    domainrunbook.StepTypeCommand,
		"cutover-textract-pipeline": domainrunbook.StepTypeAPICall,
		"rollback-textract-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasTextractRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewTextractMapper(t *testing.T) {
	m := NewTextractMapper()
	if m == nil {
		t.Fatal("NewTextractMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeTextractAdapter {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeTextractAdapter)
	}
}

func managedTextractFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "invoice-adapter",
		Type:   resource.TypeTextractAdapter,
		Name:   "invoice-adapter",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name": "invoice-adapter",
		},
	}
}

func hasTextractRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
