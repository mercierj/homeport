package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestDocumentAIConformanceManagedAToZ(t *testing.T) {
	result, err := NewDocumentAIMapper().Map(context.Background(), managedDocumentAIFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Document AI migration", result.ManualSteps)
	}
	if result.DockerService.Image != "tesseractshadow/tesseract4re:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Tesseract target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.document-ai.yml", "config/document-ai/app-change.env", "config/document-ai/processor-report.yaml", "config/document-ai/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/document-ai/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_DOCUMENT_AI_PROCESSOR=invoice-parser", "TARGET_OCR_BACKEND=tesseract"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_document_ai_processor.sh", "migrate_document_ai_processor.sh", "validate_document_ai_processor.sh", "backup_document_ai_config.sh", "cutover_document_ai_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-document-ai-processor":   domainrunbook.StepTypeCommand,
		"provision-tesseract-ocr":        domainrunbook.StepTypeCommand,
		"migrate-document-ai-processor":  domainrunbook.StepTypeCommand,
		"validate-tesseract-ocr":         domainrunbook.StepTypeCommand,
		"backup-document-ai-config":      domainrunbook.StepTypeCommand,
		"cutover-document-ai-clients":    domainrunbook.StepTypeAPICall,
		"rollback-document-ai-processor": domainrunbook.StepTypeRollback,
	} {
		if !hasDocumentAIRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewDocumentAIMapper(t *testing.T) {
	if NewDocumentAIMapper().ResourceType() != resource.TypeDocumentAIProcessor {
		t.Fatalf("Document AI mapper type = %s, want %s", NewDocumentAIMapper().ResourceType(), resource.TypeDocumentAIProcessor)
	}
}

func managedDocumentAIFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/eu/processors/invoice-parser",
		Type: resource.TypeDocumentAIProcessor,
		Name: "invoice-parser",
		Config: map[string]interface{}{
			"name":         "invoice-parser",
			"display_name": "Invoice parser",
			"location":     "eu",
			"type":         "OCR_PROCESSOR",
		},
	}
}

func hasDocumentAIRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
