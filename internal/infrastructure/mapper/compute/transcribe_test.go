package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestTranscribeConformanceManagedAToZ(t *testing.T) {
	result, err := NewTranscribeMapper().Map(context.Background(), managedTranscribeFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Transcribe migration", result.ManualSteps)
	}
	if result.DockerService.Image != "onerahmet/openai-whisper-asr-webservice:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Whisper target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/transcribe/vocabulary-map.yaml", "config/transcribe/app-change.env", "config/transcribe/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/transcribe/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_TRANSCRIBE_VOCABULARY=support-vocabulary", "TARGET_SPEECH_ENGINE=whisper"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_transcribe_vocabulary.sh", "provision_whisper.sh", "migrate_transcribe_vocabulary.sh", "validate_whisper.sh", "backup_transcribe_config.sh", "cutover_transcribe_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-transcribe-vocabulary":  domainrunbook.StepTypeCommand,
		"provision-whisper":             domainrunbook.StepTypeCommand,
		"migrate-transcribe-vocabulary": domainrunbook.StepTypeCommand,
		"validate-whisper":              domainrunbook.StepTypeCommand,
		"backup-transcribe-config":      domainrunbook.StepTypeCommand,
		"cutover-transcribe-clients":    domainrunbook.StepTypeAPICall,
		"rollback-transcribe-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasTranscribeRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewTranscribeMapper(t *testing.T) {
	m := NewTranscribeMapper()
	if m == nil {
		t.Fatal("NewTranscribeMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeTranscribeVocabulary {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeTranscribeVocabulary)
	}
}

func managedTranscribeFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "support-vocabulary",
		Type:   resource.TypeTranscribeVocabulary,
		Name:   "support-vocabulary",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"vocabulary_name": "support-vocabulary",
			"language_code":   "fr-FR",
		},
	}
}

func hasTranscribeRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
