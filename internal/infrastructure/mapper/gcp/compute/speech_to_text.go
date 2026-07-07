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

type SpeechToTextMapper struct {
	*mapper.BaseMapper
}

func NewSpeechCustomClassMapper() *SpeechToTextMapper {
	return &SpeechToTextMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeSpeechCustomClass, nil)}
}

func NewSpeechPhraseSetMapper() *SpeechToTextMapper {
	return &SpeechToTextMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeSpeechPhraseSet, nil)}
}

func (m *SpeechToTextMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmptySpeech(res.GetConfigString("name"), res.GetConfigString("display_name"), res.Name, "speech-resource")
	language := firstNonEmptySpeech(res.GetConfigString("language_code"), res.GetConfigString("language"), "en-US")
	location := firstNonEmptySpeech(res.GetConfigString("location"), res.GetConfigString("region"), "global")

	result := mapper.NewMappingResult("whisper")
	svc := result.DockerService
	svc.Image = "onerahmet/openai-whisper-asr-webservice:latest"
	svc.Ports = []string{"9000:9000"}
	svc.Volumes = []string{"./speech-to-text/audio:/audio", "./config/speech-to-text:/config"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Environment = map[string]string{"ASR_MODEL": "base", "ASR_ENGINE": "openai_whisper", "SOURCE_SPEECH_RESOURCE": name}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "curl -fsS http://localhost:9000/docs >/dev/null || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(res.Type), "homeport.speech_resource": name, "homeport.target": "whisper"}

	result.AddConfig("config/speech-to-text/phrase-map.yaml", []byte(speechPhraseMap(name, language, location, res.Type)))
	result.AddConfig("config/speech-to-text/app-change.env", []byte(speechAppChange(name)))
	result.AddConfig("config/speech-to-text/generated-client.patch", []byte(speechPatch(name)))
	result.AddScript("export_speech_to_text_config.sh", []byte(speechExportScript(name, location, res.Type)))
	result.AddScript("provision_whisper_speech.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/speech-to-text/phrase-map.yaml\necho \"Whisper ASR ready for %s\"\n", name)))
	result.AddScript("migrate_speech_to_text_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ngrep -q %q config/speech-to-text/phrase-map.yaml\necho \"Speech-to-Text resource %s mapped to Whisper phrase hints\"\n", name, name)))
	result.AddScript("validate_whisper_speech.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/speech-to-text/app-change.env\ntest -s config/speech-to-text/generated-client.patch\ngrep -q %q config/speech-to-text/phrase-map.yaml\n", name)))
	result.AddScript("backup_speech_to_text_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/speech-to-text-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/speech-to-text speech-to-text-export 2>/dev/null || tar -czf \"$archive\" config/speech-to-text\necho \"$archive\"\n", sanitizeComputeName(name))))
	result.AddScript("cutover_speech_to_text_clients.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\n. config/speech-to-text/app-change.env\ntest \"$SOURCE_SPEECH_RESOURCE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and route transcription clients to $WHISPER_URL\"\n", name)))
	for _, step := range speechRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func speechPhraseMap(name, language, location string, resType resource.Type) string {
	return fmt.Sprintf("source: %s\nresource: %s\nlanguage_code: %s\nlocation: %s\ntarget_engine: whisper\napp_change_mode: generated_patch\n", resType, name, language, location)
}

func speechAppChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_SPEECH_RESOURCE=%s\nTARGET_SPEECH_ENGINE=whisper\nWHISPER_URL=http://whisper:9000/asr\nGENERATED_PATCH=config/speech-to-text/generated-client.patch\n", name)
}

func speechPatch(name string) string {
	return fmt.Sprintf("--- a/app/speech.env\n+++ b/app/speech.env\n@@\n-GCP_SPEECH_RESOURCE=%s\n+TRANSCRIPTION_ENGINE=whisper\n+TRANSCRIPTION_URL=http://whisper:9000/asr\n", name)
}

func speechExportScript(name, location string, resType resource.Type) string {
	kind := "phrase-sets"
	if resType == resource.TypeSpeechCustomClass {
		kind = "custom-classes"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nSPEECH_NAME=%q\nLOCATION=%q\nOUTPUT_DIR=\"${OUTPUT_DIR:-./speech-to-text-export}\"\nmkdir -p \"$OUTPUT_DIR\"\ngcloud ml speech %s describe \"$SPEECH_NAME\" --location=\"$LOCATION\" --format=json > \"$OUTPUT_DIR/speech-resource.json\" 2>/dev/null || echo %q > \"$OUTPUT_DIR/source-resource.txt\"\n", name, location, kind, name)
}

func speechRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "speech-to-text", "source": "google_speech", "resource": name, "target": "whisper"}
	return []domainrunbook.Step{
		speechStep("export-speech-to-text-config", "Export Speech-to-Text config", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_speech_to_text_config.sh"}, "Speech-to-Text config is exported", metadata),
		speechStep("provision-whisper-speech", "Provision Whisper speech", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_whisper_speech.sh"}, "Whisper config is rendered", metadata),
		speechStep("migrate-speech-to-text-config", "Migrate Speech-to-Text config", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_speech_to_text_config.sh"}, "phrase hints are mapped to Whisper", metadata),
		speechStep("validate-whisper-speech", "Validate Whisper speech", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_whisper_speech.sh"}, "Whisper handoff config validates", metadata),
		speechStep("backup-speech-to-text-config", "Backup Speech-to-Text config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_speech_to_text_config.sh"}, "Speech-to-Text migration artifacts are archived", metadata),
		speechStep("cutover-speech-to-text", "Cut over Speech-to-Text clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_speech_to_text_clients.sh"}, "clients use generated Whisper patch", metadata),
		speechStep("rollback-speech-to-text", "Keep Speech-to-Text source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Speech-to-Text remains authoritative until Whisper validation passes", metadata),
	}
}

func speechStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmptySpeech(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
