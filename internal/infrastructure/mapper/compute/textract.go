package compute

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type TextractMapper struct {
	*mapper.BaseMapper
}

func NewTextractMapper() *TextractMapper {
	return &TextractMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeTextractAdapter, nil)}
}

func (m *TextractMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}

	result := mapper.NewMappingResult("tesseract-ocr")
	svc := result.DockerService
	svc.Image = "ghcr.io/ocrmypdf/ocrmypdf:v16.10.4"
	svc.Command = []string{"--help"}
	svc.Volumes = []string{"./data/textract/input:/input", "./data/textract/output:/output", "./config/textract:/config"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":           "aws_textract_adapter",
		"homeport.textract_adapter": name,
		"homeport.target":           "tesseract-ocr",
	}

	result.AddConfig("config/textract/ocr-pipeline.yaml", []byte(m.pipeline(name)))
	result.AddConfig("config/textract/adapter-map.yaml", []byte(m.adapterMap(name)))
	result.AddConfig("config/textract/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/textract/generated-ocr.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_textract_adapters.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_tesseract_ocr.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_textract_adapters.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_tesseract_ocr.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_textract_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_textract_pipeline.sh", []byte(m.cutoverScript(name)))
	for _, step := range textractRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *TextractMapper) pipeline(name string) string {
	return fmt.Sprintf(`adapter: %s
source: aws_textract_adapter
target: tesseract
command:
  - ocrmypdf
  - --sidecar
  - /output/text.txt
  - /input/document.pdf
  - /output/document.pdf
`, name)
}

func (m *TextractMapper) adapterMap(name string) string {
	return fmt.Sprintf(`adapter: %s
source_api: textract
target_engine: tesseract
output_formats:
  - text
  - searchable_pdf
app_change_mode: generated_patch
`, name)
}

func (m *TextractMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_TEXTRACT_ADAPTER=%s
TARGET_OCR_ENGINE=tesseract
TARGET_OCR_COMMAND=ocrmypdf
GENERATED_PATCH=config/textract/generated-ocr.patch
`, name)
}

func (m *TextractMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/document/extraction.env
+++ b/document/extraction.env
@@
-AWS_TEXTRACT_ADAPTER=%s
+OCR_ENGINE=tesseract
+OCR_COMMAND=ocrmypdf
`, name)
}

func (m *TextractMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
ADAPTER_NAME=%q
OUTPUT_DIR="./textract-export"
mkdir -p "$OUTPUT_DIR"
aws textract list-adapters --region "$AWS_REGION" --output json > "$OUTPUT_DIR/adapters.json"
jq -e --arg name "$ADAPTER_NAME" '.Adapters[] | select(.AdapterName == $name)' "$OUTPUT_DIR/adapters.json" > "$OUTPUT_DIR/adapter.json"
`, region, name)
}

func (m *TextractMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/textract/ocr-pipeline.yaml\ntest -s config/textract/adapter-map.yaml\necho \"Tesseract OCR pipeline ready for %s\"\n", name)
}

func (m *TextractMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s textract-export/adapter.json\ngrep -q %q config/textract/adapter-map.yaml\necho \"Textract adapter %s mapped to Tesseract OCR\"\n", name, name)
}

func (m *TextractMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/textract/app-change.env\ntest -s config/textract/generated-ocr.patch\ngrep -q %q config/textract/ocr-pipeline.yaml\nocrmypdf --version >/tmp/homeport-ocrmypdf-version.txt\n", name)
}

func (m *TextractMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-textract-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/textract export_textract_adapters.sh provision_tesseract_ocr.sh migrate_textract_adapters.sh validate_tesseract_ocr.sh cutover_textract_pipeline.sh
echo "$archive"
`, name)
}

func (m *TextractMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/textract/app-change.env
test "$SOURCE_TEXTRACT_ADAPTER" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route document extraction through $TARGET_OCR_COMMAND"
`, name)
}

func textractRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "document-ocr",
		"source":              "aws_textract_adapter",
		"adapter":             name,
		"HOMEPORT_TARGET":     "tesseract-ocr",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		textractStep("export-textract-adapters", "Export Textract adapters", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_textract_adapters.sh"}, "Textract adapters are exported", metadata),
		textractStep("provision-tesseract-ocr", "Provision Tesseract OCR", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_tesseract_ocr.sh"}, "OCR pipeline config is rendered", metadata),
		textractStep("migrate-textract-adapters", "Migrate Textract adapters", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_textract_adapters.sh"}, "Textract adapter maps to OCR pipeline", metadata),
		textractStep("validate-tesseract-ocr", "Validate Tesseract OCR", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_tesseract_ocr.sh"}, "OCR command and generated patch validate", metadata),
		textractStep("backup-textract-config", "Backup Textract config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_textract_config.sh"}, "Textract migration artifacts are archived", metadata),
		textractStep("cutover-textract-pipeline", "Cut over Textract pipeline", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_textract_pipeline.sh"}, "document extraction uses Tesseract OCR", metadata),
		textractStep("rollback-textract-source", "Keep Textract source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Textract remains authoritative until OCR validation passes", metadata),
	}
}

func textractStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}
