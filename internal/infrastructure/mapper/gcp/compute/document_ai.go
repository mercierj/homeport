package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type DocumentAIMapper struct {
	*mapper.BaseMapper
}

func NewDocumentAIMapper() *DocumentAIMapper {
	return &DocumentAIMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeDocumentAIProcessor, nil)}
}

func (m *DocumentAIMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmptyDocumentAI(res.GetConfigString("name"), res.GetConfigString("display_name"), res.Name, "document-ai-processor")
	location := firstNonEmptyDocumentAI(res.GetConfigString("location"), res.GetConfigString("region"), "${GCP_LOCATION}")

	result := mapper.NewMappingResult("document-ai")
	svc := result.DockerService
	svc.Image = "tesseractshadow/tesseract4re:latest"
	svc.Command = []string{"sleep", "infinity"}
	svc.Environment = map[string]string{"SOURCE_DOCUMENT_AI_PROCESSOR": name, "TARGET_OCR_BACKEND": "tesseract"}
	svc.Volumes = []string{"./document-ai/input:/input", "./document-ai/output:/output"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "tesseract --version >/dev/null 2>&1 || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeDocumentAIProcessor), "homeport.processor": name, "homeport.target": "tesseract"}

	result.AddConfig("docker-compose.document-ai.yml", []byte(documentAICompose(name)))
	result.AddConfig("config/document-ai/app-change.env", []byte(documentAIAppChange(name)))
	result.AddConfig("config/document-ai/processor-report.yaml", []byte(documentAIReport(name, location, res.GetConfigString("type"))))
	result.AddConfig("config/document-ai/generated-client.patch", []byte(documentAIPatch(name)))
	result.AddScript("export_document_ai_processor.sh", []byte(documentAIExportScript(name, location)))
	result.AddScript("migrate_document_ai_processor.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/document-ai/processor-report.yaml\nmkdir -p document-ai/input document-ai/output\necho \"Document AI processor %s mapped to Tesseract OCR\"\n", name)))
	result.AddScript("validate_document_ai_processor.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s docker-compose.document-ai.yml\ngrep -q %q config/document-ai/app-change.env\n", name)))
	result.AddScript("backup_document_ai_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/document-ai-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/document-ai document-ai-export docker-compose.document-ai.yml 2>/dev/null || tar -czf \"$archive\" config/document-ai docker-compose.document-ai.yml\necho \"$archive\"\n", sanitizeComputeName(name))))
	result.AddScript("cutover_document_ai_clients.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\n. config/document-ai/app-change.env\ntest \"$SOURCE_DOCUMENT_AI_PROCESSOR\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and route OCR jobs to $TARGET_OCR_BACKEND\"\n", name)))
	for _, step := range documentAIRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func documentAICompose(name string) string {
	return fmt.Sprintf(`services:
  document-ai:
    image: tesseractshadow/tesseract4re:latest
    command: ["sleep", "infinity"]
    environment:
      SOURCE_DOCUMENT_AI_PROCESSOR: %s
      TARGET_OCR_BACKEND: tesseract
    volumes:
      - ./document-ai/input:/input
      - ./document-ai/output:/output
`, name)
}

func documentAIAppChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_DOCUMENT_AI_PROCESSOR=%s\nTARGET_OCR_BACKEND=tesseract\nGENERATED_PATCH=config/document-ai/generated-client.patch\n", name)
}

func documentAIReport(name, location, processorType string) string {
	return fmt.Sprintf("source: google_document_ai_processor\nprocessor: %s\nlocation: %s\ntype: %s\ntarget: tesseract\n", name, location, processorType)
}

func documentAIPatch(name string) string {
	return fmt.Sprintf("--- a/app/ocr.env\n+++ b/app/ocr.env\n@@\n-DOCUMENT_AI_PROCESSOR=%s\n+OCR_BACKEND=tesseract\n+OCR_INPUT_DIR=document-ai/input\n+OCR_OUTPUT_DIR=document-ai/output\n", name)
}

func documentAIExportScript(name, location string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nPROCESSOR_NAME=%q\nLOCATION=%q\nOUTPUT_DIR=\"${OUTPUT_DIR:-./document-ai-export}\"\nmkdir -p \"$OUTPUT_DIR\" document-ai/input document-ai/output\ngcloud documentai processors describe \"$PROCESSOR_NAME\" --location=\"$LOCATION\" --format=json > \"$OUTPUT_DIR/processor.json\"\n", name, location)
}

func documentAIRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "ocr", "source": "google_document_ai_processor", "processor": name, "target": "tesseract"}
	return []domainrunbook.Step{
		documentAIStep("export-document-ai-processor", "Export Document AI processor", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_document_ai_processor.sh"}, "Document AI processor is exported", metadata),
		documentAIStep("provision-tesseract-ocr", "Provision Tesseract OCR", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s docker-compose.document-ai.yml"}, "Tesseract compose target is rendered", metadata),
		documentAIStep("migrate-document-ai-processor", "Migrate Document AI processor", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_document_ai_processor.sh"}, "processor config is staged for Tesseract", metadata),
		documentAIStep("validate-tesseract-ocr", "Validate Tesseract OCR", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_document_ai_processor.sh"}, "Tesseract handoff config validates", metadata),
		documentAIStep("backup-document-ai-config", "Backup Document AI config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_document_ai_config.sh"}, "Document AI migration artifacts are archived", metadata),
		documentAIStep("cutover-document-ai-clients", "Cut over Document AI clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_document_ai_clients.sh"}, "clients use generated OCR patch", metadata),
		documentAIStep("rollback-document-ai-processor", "Keep Document AI source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Document AI remains authoritative until OCR validation passes", metadata),
	}
}

func documentAIStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmptyDocumentAI(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
