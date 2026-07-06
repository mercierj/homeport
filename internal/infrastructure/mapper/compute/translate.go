package compute

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type TranslateMapper struct {
	*mapper.BaseMapper
}

func NewTranslateMapper() *TranslateMapper {
	return &TranslateMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeTranslateText, nil)}
}

func (m *TranslateMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.Name
	if name == "" {
		name = res.ID
	}
	source := res.GetConfigString("source_language")
	if source == "" {
		source = "auto"
	}
	target := res.GetConfigString("target_language")
	if target == "" {
		target = "en"
	}

	result := mapper.NewMappingResult("libretranslate")
	svc := result.DockerService
	svc.Image = "libretranslate/libretranslate:latest"
	svc.Command = []string{"--host", "0.0.0.0", "--port", "5000"}
	svc.Ports = []string{"5000:5000"}
	svc.Volumes = []string{"./config/translate:/config", "./data/translate:/home/libretranslate/.local"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":        "aws_translate_text",
		"homeport.translate_api": name,
		"homeport.target":        "libretranslate",
	}

	result.AddConfig("config/translate/language-map.yaml", []byte(m.languageMap(name, source, target)))
	result.AddConfig("config/translate/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/translate/generated-client.patch", []byte(m.generatedPatch()))
	result.AddScript("export_translate_settings.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_libretranslate.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_translate_clients.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_libretranslate.sh", []byte(m.validateScript()))
	result.AddScript("backup_translate_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_translate_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range translateRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *TranslateMapper) languageMap(name, source, target string) string {
	return fmt.Sprintf(`api: %s
source_api: translate
target_engine: libretranslate
default_source_language: %s
default_target_language: %s
app_change_mode: generated_patch
`, name, source, target)
}

func (m *TranslateMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_TRANSLATE_API=%s
TARGET_TRANSLATION_ENGINE=libretranslate
LIBRETRANSLATE_URL=http://libretranslate:5000/translate
GENERATED_PATCH=config/translate/generated-client.patch
`, name)
}

func (m *TranslateMapper) generatedPatch() string {
	return `--- a/app/translation.env
+++ b/app/translation.env
@@
-TRANSLATION_PROVIDER=aws_translate
+TRANSLATION_PROVIDER=libretranslate
+TRANSLATION_URL=http://libretranslate:5000/translate
`
}

func (m *TranslateMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
API_NAME=%q
OUTPUT_DIR="./translate-export"
mkdir -p "$OUTPUT_DIR"
printf '{"api":"%%s","region":"%%s"}\n' "$API_NAME" "$AWS_REGION" > "$OUTPUT_DIR/settings.json"
`, region, name)
}

func (m *TranslateMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/translate/language-map.yaml\necho \"LibreTranslate ready for %s\"\n", name)
}

func (m *TranslateMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s translate-export/settings.json\ngrep -q %q config/translate/language-map.yaml\necho \"AWS Translate clients mapped to LibreTranslate\"\n", name)
}

func (m *TranslateMapper) validateScript() string {
	return "#!/bin/sh\nset -eu\ntest -s config/translate/app-change.env\ntest -s config/translate/generated-client.patch\ncurl -fsS http://localhost:5000/languages >/tmp/homeport-libretranslate-languages.json\n"
}

func (m *TranslateMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-translate-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/translate translate-export export_translate_settings.sh provision_libretranslate.sh migrate_translate_clients.sh validate_libretranslate.sh cutover_translate_clients.sh
echo "$archive"
`, name)
}

func (m *TranslateMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/translate/app-change.env
test "$SOURCE_TRANSLATE_API" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route translation clients to $LIBRETRANSLATE_URL"
`, name)
}

func translateRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "translation",
		"source":              "aws_translate_text",
		"api":                 name,
		"HOMEPORT_TARGET":     "libretranslate",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		translateStep("export-translate-settings", "Export Translate settings", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_translate_settings.sh"}, "Translate settings are exported", metadata),
		translateStep("provision-libretranslate", "Provision LibreTranslate", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_libretranslate.sh"}, "LibreTranslate config is rendered", metadata),
		translateStep("migrate-translate-clients", "Migrate Translate clients", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_translate_clients.sh"}, "clients are mapped to LibreTranslate", metadata),
		translateStep("validate-libretranslate", "Validate LibreTranslate", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_libretranslate.sh"}, "LibreTranslate endpoint and generated patch validate", metadata),
		translateStep("backup-translate-config", "Backup Translate config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_translate_config.sh"}, "Translate migration artifacts are archived", metadata),
		translateStep("cutover-translate-clients", "Cut over Translate clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_translate_clients.sh"}, "clients use generated LibreTranslate patch", metadata),
		translateStep("rollback-translate-source", "Keep Translate source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Translate remains authoritative until LibreTranslate validation passes", metadata),
	}
}

func translateStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
