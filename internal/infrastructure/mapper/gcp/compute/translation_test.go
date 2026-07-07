package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestTranslationConformanceManagedAToZ(t *testing.T) {
	result, err := NewTranslationMapper().Map(context.Background(), managedTranslationFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Translation migration", result.ManualSteps)
	}
	if result.DockerService.Image != "libretranslate/libretranslate:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA LibreTranslate target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/translation/language-map.yaml", "config/translation/app-change.env", "config/translation/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/translation/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_TRANSLATION_SERVICE=translate-api", "TARGET_TRANSLATION_ENGINE=libretranslate"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_translation_service.sh", "provision_libretranslate_translation.sh", "migrate_translation_clients.sh", "validate_libretranslate_translation.sh", "backup_translation_config.sh", "cutover_translation_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-translation-service":   domainrunbook.StepTypeCommand,
		"provision-libretranslate":     domainrunbook.StepTypeCommand,
		"migrate-translation-clients":  domainrunbook.StepTypeCommand,
		"validate-libretranslate":      domainrunbook.StepTypeCommand,
		"backup-translation-config":    domainrunbook.StepTypeCommand,
		"cutover-translation-clients":  domainrunbook.StepTypeAPICall,
		"rollback-translation-service": domainrunbook.StepTypeRollback,
	} {
		if !hasTranslationRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewTranslationMapper(t *testing.T) {
	if NewTranslationMapper().ResourceType() != resource.TypeTranslationService {
		t.Fatalf("Translation mapper type = %s, want %s", NewTranslationMapper().ResourceType(), resource.TypeTranslationService)
	}
}

func managedTranslationFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/services/translate.googleapis.com",
		Type: resource.TypeTranslationService,
		Name: "translate-api",
		Config: map[string]interface{}{
			"name":            "translate-api",
			"service":         "translate.googleapis.com",
			"source_language": "fr",
			"target_language": "en",
		},
	}
}

func hasTranslationRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
