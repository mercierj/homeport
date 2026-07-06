package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestTranslateConformanceManagedAToZ(t *testing.T) {
	result, err := NewTranslateMapper().Map(context.Background(), managedTranslateFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Translate migration", result.ManualSteps)
	}
	if result.DockerService.Image != "libretranslate/libretranslate:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA LibreTranslate target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/translate/language-map.yaml", "config/translate/app-change.env", "config/translate/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/translate/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_TRANSLATE_API=translate-text", "TARGET_TRANSLATION_ENGINE=libretranslate"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_translate_settings.sh", "provision_libretranslate.sh", "migrate_translate_clients.sh", "validate_libretranslate.sh", "backup_translate_config.sh", "cutover_translate_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-translate-settings": domainrunbook.StepTypeCommand,
		"provision-libretranslate":  domainrunbook.StepTypeCommand,
		"migrate-translate-clients": domainrunbook.StepTypeCommand,
		"validate-libretranslate":   domainrunbook.StepTypeCommand,
		"backup-translate-config":   domainrunbook.StepTypeCommand,
		"cutover-translate-clients": domainrunbook.StepTypeAPICall,
		"rollback-translate-source": domainrunbook.StepTypeRollback,
	} {
		if !hasTranslateRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewTranslateMapper(t *testing.T) {
	m := NewTranslateMapper()
	if m == nil {
		t.Fatal("NewTranslateMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeTranslateText {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeTranslateText)
	}
}

func managedTranslateFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "translate-text",
		Type:   resource.TypeTranslateText,
		Name:   "translate-text",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"source_language": "fr",
			"target_language": "en",
		},
	}
}

func hasTranslateRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
