package compute

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type ComprehendMapper struct {
	*mapper.BaseMapper
}

func NewComprehendMapper() *ComprehendMapper {
	return &ComprehendMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeComprehendClassifier, nil)}
}

func (m *ComprehendMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	language := res.GetConfigString("language_code")
	if language == "" {
		language = "en"
	}

	result := mapper.NewMappingResult("spacy")
	svc := result.DockerService
	svc.Image = "spacy/spacy:latest"
	svc.Command = []string{"python", "-m", "http.server", "8080"}
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{"./models/comprehend:/models", "./config/comprehend:/config"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":           "aws_comprehend_document_classifier",
		"homeport.comprehend_model": name,
		"homeport.target":           "spacy",
	}

	result.AddConfig("config/comprehend/nlp-pipeline.yaml", []byte(m.pipeline(name, language)))
	result.AddConfig("config/comprehend/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/comprehend/generated-client.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_comprehend_models.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_spacy_pipeline.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_comprehend_models.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_spacy_pipeline.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_comprehend_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_comprehend_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range comprehendRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *ComprehendMapper) pipeline(name, language string) string {
	return fmt.Sprintf(`model: %s
source_api: comprehend
target_engine: spacy
language_code: %s
model_dir: /models/%s
app_change_mode: generated_patch
`, name, language, name)
}

func (m *ComprehendMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_COMPREHEND_MODEL=%s
TARGET_NLP_ENGINE=spacy
SPACY_PIPELINE_CONFIG=config/comprehend/nlp-pipeline.yaml
GENERATED_PATCH=config/comprehend/generated-client.patch
`, name)
}

func (m *ComprehendMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/app/nlp.env
+++ b/app/nlp.env
@@
-AWS_COMPREHEND_MODEL=%s
+NLP_ENGINE=spacy
+NLP_PIPELINE_CONFIG=config/comprehend/nlp-pipeline.yaml
`, name)
}

func (m *ComprehendMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
MODEL_NAME=%q
OUTPUT_DIR="./comprehend-export"
mkdir -p "$OUTPUT_DIR"
aws comprehend list-document-classifiers --region "$AWS_REGION" --output json > "$OUTPUT_DIR/document-classifiers.json"
aws comprehend list-entity-recognizers --region "$AWS_REGION" --output json > "$OUTPUT_DIR/entity-recognizers.json"
jq -e --arg name "$MODEL_NAME" '.DocumentClassifierPropertiesList[]? | select(.DocumentClassifierName == $name)' "$OUTPUT_DIR/document-classifiers.json" > "$OUTPUT_DIR/model.json" || true
`, region, name)
}

func (m *ComprehendMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p models/comprehend/%s\ntest -s config/comprehend/nlp-pipeline.yaml\necho \"spaCy pipeline ready for %s\"\n", name, name)
}

func (m *ComprehendMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s comprehend-export/document-classifiers.json\ngrep -q %q config/comprehend/nlp-pipeline.yaml\necho \"Comprehend model %s mapped to spaCy pipeline\"\n", name, name)
}

func (m *ComprehendMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/comprehend/app-change.env\ntest -s config/comprehend/generated-client.patch\ngrep -q %q config/comprehend/nlp-pipeline.yaml\npython -m spacy info >/tmp/homeport-spacy-info.txt\n", name)
}

func (m *ComprehendMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-comprehend-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/comprehend comprehend-export models/comprehend export_comprehend_models.sh provision_spacy_pipeline.sh migrate_comprehend_models.sh validate_spacy_pipeline.sh cutover_comprehend_clients.sh
echo "$archive"
`, name)
}

func (m *ComprehendMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/comprehend/app-change.env
test "$SOURCE_COMPREHEND_MODEL" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route NLP clients to $SPACY_PIPELINE_CONFIG"
`, name)
}

func comprehendRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "nlp",
		"source":              "aws_comprehend_document_classifier",
		"model":               name,
		"HOMEPORT_TARGET":     "spacy",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		comprehendStep("export-comprehend-models", "Export Comprehend models", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_comprehend_models.sh"}, "Comprehend model metadata is exported", metadata),
		comprehendStep("provision-spacy-pipeline", "Provision spaCy pipeline", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_spacy_pipeline.sh"}, "spaCy pipeline config is rendered", metadata),
		comprehendStep("migrate-comprehend-models", "Migrate Comprehend models", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_comprehend_models.sh"}, "Comprehend models map to spaCy pipeline", metadata),
		comprehendStep("validate-spacy-pipeline", "Validate spaCy pipeline", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_spacy_pipeline.sh"}, "spaCy runtime and generated patch validate", metadata),
		comprehendStep("backup-comprehend-config", "Backup Comprehend config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_comprehend_config.sh"}, "Comprehend migration artifacts are archived", metadata),
		comprehendStep("cutover-comprehend-clients", "Cut over Comprehend clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_comprehend_clients.sh"}, "clients use generated spaCy patch", metadata),
		comprehendStep("rollback-comprehend-source", "Keep Comprehend source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Comprehend remains authoritative until spaCy validation passes", metadata),
	}
}

func comprehendStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
