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

type TranslationMapper struct {
	*mapper.BaseMapper
}

func NewTranslationMapper() *TranslationMapper {
	return &TranslationMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeTranslationService, nil)}
}

func (m *TranslationMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmptyTranslation(res.GetConfigString("name"), res.Name, "translation")
	source := firstNonEmptyTranslation(res.GetConfigString("source_language"), "auto")
	target := firstNonEmptyTranslation(res.GetConfigString("target_language"), "en")

	result := mapper.NewMappingResult("libretranslate")
	svc := result.DockerService
	svc.Image = "libretranslate/libretranslate:latest"
	svc.Command = []string{"--host", "0.0.0.0", "--port", "5000"}
	svc.Ports = []string{"5000:5000"}
	svc.Volumes = []string{"./config/translation:/config", "./data/translation:/home/libretranslate/.local"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "curl -fsS http://localhost:5000/languages >/dev/null || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeTranslationService), "homeport.translate_api": name, "homeport.target": "libretranslate"}

	result.AddConfig("config/translation/language-map.yaml", []byte(translationLanguageMap(name, source, target)))
	result.AddConfig("config/translation/app-change.env", []byte(translationAppChange(name)))
	result.AddConfig("config/translation/generated-client.patch", []byte(translationPatch()))
	result.AddScript("export_translation_service.sh", []byte(translationExportScript(name)))
	result.AddScript("provision_libretranslate_translation.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/translation/language-map.yaml\necho \"LibreTranslate ready for %s\"\n", name)))
	result.AddScript("migrate_translation_clients.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ngrep -q %q config/translation/language-map.yaml\necho \"Translation clients mapped to LibreTranslate\"\n", name)))
	result.AddScript("validate_libretranslate_translation.sh", []byte("#!/bin/sh\nset -eu\ntest -s config/translation/app-change.env\ntest -s config/translation/generated-client.patch\n"))
	result.AddScript("backup_translation_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/translation-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/translation translation-export 2>/dev/null || tar -czf \"$archive\" config/translation\necho \"$archive\"\n", sanitizeComputeName(name))))
	result.AddScript("cutover_translation_clients.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\n. config/translation/app-change.env\ntest \"$SOURCE_TRANSLATION_SERVICE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and route translation clients to $LIBRETRANSLATE_URL\"\n", name)))
	for _, step := range translationRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func translationLanguageMap(name, source, target string) string {
	return fmt.Sprintf("api: %s\nsource_api: translate\ntarget_engine: libretranslate\ndefault_source_language: %s\ndefault_target_language: %s\napp_change_mode: generated_patch\n", name, source, target)
}

func translationAppChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_TRANSLATION_SERVICE=%s\nTARGET_TRANSLATION_ENGINE=libretranslate\nLIBRETRANSLATE_URL=http://libretranslate:5000/translate\nGENERATED_PATCH=config/translation/generated-client.patch\n", name)
}

func translationPatch() string {
	return "--- a/app/translation.env\n+++ b/app/translation.env\n@@\n-TRANSLATION_PROVIDER=gcp_translate\n+TRANSLATION_PROVIDER=libretranslate\n+TRANSLATION_URL=http://libretranslate:5000/translate\n"
}

func translationExportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nSERVICE_NAME=%q\nOUTPUT_DIR=\"${OUTPUT_DIR:-./translation-export}\"\nmkdir -p \"$OUTPUT_DIR\"\ngcloud services list --enabled --filter=\"name:translate.googleapis.com\" --format=json > \"$OUTPUT_DIR/service.json\"\necho \"$SERVICE_NAME\" > \"$OUTPUT_DIR/source-service.txt\"\n", name)
}

func translationRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "translation", "source": "google_translation_service", "api": name, "target": "libretranslate"}
	return []domainrunbook.Step{
		translationStep("export-translation-service", "Export Translation service", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_translation_service.sh"}, "Translation service is exported", metadata),
		translationStep("provision-libretranslate", "Provision LibreTranslate", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_libretranslate_translation.sh"}, "LibreTranslate config is rendered", metadata),
		translationStep("migrate-translation-clients", "Migrate Translation clients", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_translation_clients.sh"}, "clients are mapped to LibreTranslate", metadata),
		translationStep("validate-libretranslate", "Validate LibreTranslate", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_libretranslate_translation.sh"}, "LibreTranslate handoff config validates", metadata),
		translationStep("backup-translation-config", "Backup Translation config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_translation_config.sh"}, "Translation migration artifacts are archived", metadata),
		translationStep("cutover-translation-clients", "Cut over Translation clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_translation_clients.sh"}, "clients use generated LibreTranslate patch", metadata),
		translationStep("rollback-translation-service", "Keep Translation source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Translation remains authoritative until LibreTranslate validation passes", metadata),
	}
}

func translationStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmptyTranslation(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
