package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestSpeechToTextConformanceManagedAToZ(t *testing.T) {
	result, err := NewSpeechCustomClassMapper().Map(context.Background(), managedSpeechFixture(resource.TypeSpeechCustomClass))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Speech-to-Text migration", result.ManualSteps)
	}
	if result.DockerService.Image != "onerahmet/openai-whisper-asr-webservice:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Whisper target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/speech-to-text/phrase-map.yaml", "config/speech-to-text/app-change.env", "config/speech-to-text/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/speech-to-text/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_SPEECH_RESOURCE=support-phrases", "TARGET_SPEECH_ENGINE=whisper"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_speech_to_text_config.sh", "provision_whisper_speech.sh", "migrate_speech_to_text_config.sh", "validate_whisper_speech.sh", "backup_speech_to_text_config.sh", "cutover_speech_to_text_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-speech-to-text-config":  domainrunbook.StepTypeCommand,
		"provision-whisper-speech":      domainrunbook.StepTypeCommand,
		"migrate-speech-to-text-config": domainrunbook.StepTypeCommand,
		"validate-whisper-speech":       domainrunbook.StepTypeCommand,
		"backup-speech-to-text-config":  domainrunbook.StepTypeCommand,
		"cutover-speech-to-text":        domainrunbook.StepTypeAPICall,
		"rollback-speech-to-text":       domainrunbook.StepTypeRollback,
	} {
		if !hasSpeechRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewSpeechToTextMappers(t *testing.T) {
	if NewSpeechCustomClassMapper().ResourceType() != resource.TypeSpeechCustomClass {
		t.Fatalf("custom class mapper type = %s, want %s", NewSpeechCustomClassMapper().ResourceType(), resource.TypeSpeechCustomClass)
	}
	if NewSpeechPhraseSetMapper().ResourceType() != resource.TypeSpeechPhraseSet {
		t.Fatalf("phrase set mapper type = %s, want %s", NewSpeechPhraseSetMapper().ResourceType(), resource.TypeSpeechPhraseSet)
	}
}

func managedSpeechFixture(resType resource.Type) *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/eu/phraseSets/support-phrases",
		Type: resType,
		Name: "support-phrases",
		Config: map[string]interface{}{
			"name":          "support-phrases",
			"display_name":  "Support phrases",
			"location":      "eu",
			"language_code": "fr-FR",
		},
	}
}

func hasSpeechRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
