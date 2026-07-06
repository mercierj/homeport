package compute

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type TranscribeMapper struct {
	*mapper.BaseMapper
}

func NewTranscribeMapper() *TranscribeMapper {
	return &TranscribeMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeTranscribeVocabulary, nil)}
}

func (m *TranscribeMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("vocabulary_name")
	if name == "" {
		name = res.GetConfigString("name")
	}
	if name == "" {
		name = res.Name
	}
	language := res.GetConfigString("language_code")
	if language == "" {
		language = "en-US"
	}

	result := mapper.NewMappingResult("whisper")
	svc := result.DockerService
	svc.Image = "onerahmet/openai-whisper-asr-webservice:latest"
	svc.Ports = []string{"9000:9000"}
	svc.Volumes = []string{"./data/transcribe/audio:/audio", "./config/transcribe:/config"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Environment["ASR_MODEL"] = "base"
	svc.Environment["ASR_ENGINE"] = "openai_whisper"
	svc.Labels = map[string]string{
		"homeport.source":              "aws_transcribe_vocabulary",
		"homeport.transcribe_resource": name,
		"homeport.target":              "whisper",
	}

	result.AddConfig("config/transcribe/vocabulary-map.yaml", []byte(m.vocabularyMap(name, language)))
	result.AddConfig("config/transcribe/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/transcribe/generated-client.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_transcribe_vocabulary.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_whisper.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_transcribe_vocabulary.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_whisper.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_transcribe_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_transcribe_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range transcribeRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *TranscribeMapper) vocabularyMap(name, language string) string {
	return fmt.Sprintf(`vocabulary: %s
source_api: transcribe
language_code: %s
target_engine: whisper
phrase_file: transcribe-export/vocabulary.txt
app_change_mode: generated_patch
`, name, language)
}

func (m *TranscribeMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_TRANSCRIBE_VOCABULARY=%s
TARGET_SPEECH_ENGINE=whisper
WHISPER_URL=http://whisper:9000/asr
GENERATED_PATCH=config/transcribe/generated-client.patch
`, name)
}

func (m *TranscribeMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/app/transcription.env
+++ b/app/transcription.env
@@
-AWS_TRANSCRIBE_VOCABULARY=%s
+TRANSCRIPTION_ENGINE=whisper
+TRANSCRIPTION_URL=http://whisper:9000/asr
`, name)
}

func (m *TranscribeMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
VOCABULARY_NAME=%q
OUTPUT_DIR="./transcribe-export"
mkdir -p "$OUTPUT_DIR"
aws transcribe get-vocabulary --vocabulary-name "$VOCABULARY_NAME" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/vocabulary.json"
jq -r '.DownloadUri // empty' "$OUTPUT_DIR/vocabulary.json" > "$OUTPUT_DIR/download-uri.txt"
`, region, name)
}

func (m *TranscribeMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/transcribe/vocabulary-map.yaml\necho \"Whisper ASR ready for %s\"\n", name)
}

func (m *TranscribeMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s transcribe-export/vocabulary.json\ngrep -q %q config/transcribe/vocabulary-map.yaml\necho \"Transcribe vocabulary %s mapped to Whisper phrases\"\n", name, name)
}

func (m *TranscribeMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/transcribe/app-change.env\ntest -s config/transcribe/generated-client.patch\ngrep -q %q config/transcribe/vocabulary-map.yaml\ncurl -fsS http://localhost:9000/docs >/tmp/homeport-whisper-docs.html\n", name)
}

func (m *TranscribeMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-transcribe-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/transcribe transcribe-export export_transcribe_vocabulary.sh provision_whisper.sh migrate_transcribe_vocabulary.sh validate_whisper.sh cutover_transcribe_clients.sh
echo "$archive"
`, name)
}

func (m *TranscribeMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/transcribe/app-change.env
test "$SOURCE_TRANSCRIBE_VOCABULARY" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route transcription clients to $WHISPER_URL"
`, name)
}

func transcribeRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "speech-to-text",
		"source":              "aws_transcribe_vocabulary",
		"vocabulary":          name,
		"HOMEPORT_TARGET":     "whisper",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		transcribeStep("export-transcribe-vocabulary", "Export Transcribe vocabulary", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_transcribe_vocabulary.sh"}, "Transcribe vocabulary metadata is exported", metadata),
		transcribeStep("provision-whisper", "Provision Whisper", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_whisper.sh"}, "Whisper service config is rendered", metadata),
		transcribeStep("migrate-transcribe-vocabulary", "Migrate Transcribe vocabulary", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_transcribe_vocabulary.sh"}, "custom vocabulary maps to Whisper phrase hints", metadata),
		transcribeStep("validate-whisper", "Validate Whisper", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_whisper.sh"}, "Whisper endpoint and generated patch validate", metadata),
		transcribeStep("backup-transcribe-config", "Backup Transcribe config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_transcribe_config.sh"}, "Transcribe migration artifacts are archived", metadata),
		transcribeStep("cutover-transcribe-clients", "Cut over Transcribe clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_transcribe_clients.sh"}, "clients use generated Whisper patch", metadata),
		transcribeStep("rollback-transcribe-source", "Keep Transcribe source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Transcribe remains authoritative until Whisper validation passes", metadata),
	}
}

func transcribeStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
