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

type FoundryOpenAIMapper struct {
	*mapper.BaseMapper
}

func NewFoundryOpenAIMapper() *FoundryOpenAIMapper {
	return &FoundryOpenAIMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAzureFoundryOpenAI, nil)}
}

func (m *FoundryOpenAIMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	accountName := firstNonEmptyFoundryOpenAI(res.GetConfigString("name"), res.GetConfigString("account_name"), res.Name)
	modelName := firstNonEmptyFoundryOpenAI(res.GetConfigString("model"), res.GetConfigString("deployment_model"), "mistralai/Mistral-7B-Instruct-v0.2")

	result := mapper.NewMappingResult("foundry-openai")
	svc := result.DockerService
	svc.Image = "vllm/vllm-openai:latest"
	svc.Command = []string{"--host", "0.0.0.0", "--port", "8000", "--model", "${FOUNDRY_OPENAI_MODEL:-" + modelName + "}"}
	svc.Ports = []string{"8000:8000"}
	svc.Volumes = []string{"./foundry-openai/models:/models", "./foundry-openai/cache:/root/.cache"}
	svc.Environment = map[string]string{"AZURE_OPENAI_ACCOUNT": accountName, "FOUNDRY_OPENAI_MODEL": modelName}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "curl -fsS http://localhost:8000/v1/models >/dev/null || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeAzureFoundryOpenAI), "homeport.account": accountName, "homeport.target": "vllm-openai"}

	result.AddConfig("docker-compose.foundry-openai.yml", []byte(m.generateCompose(accountName, modelName)))
	result.AddConfig("config/foundry-openai/app-change.env", []byte(m.generateAppChangeConfig(accountName)))
	result.AddConfig("config/foundry-openai/account-report.yaml", []byte(m.generateAccountReport(res, accountName, modelName)))
	result.AddConfig("config/foundry-openai/generated-client.patch", []byte(m.generateClientPatch(accountName)))
	result.AddScript("export_foundry_openai_account.sh", []byte(m.generateExportScript(accountName)))
	result.AddScript("migrate_foundry_openai_models.sh", []byte(m.generateMigrateScript(accountName)))
	result.AddScript("validate_foundry_openai_endpoint.sh", []byte(m.generateValidateScript(accountName)))
	result.AddScript("backup_foundry_openai_config.sh", []byte(m.generateBackupScript(accountName)))
	result.AddScript("cutover_foundry_openai_clients.sh", []byte(m.generateCutoverScript(accountName)))
	for _, step := range foundryOpenAIRunbook(accountName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *FoundryOpenAIMapper) generateCompose(accountName, modelName string) string {
	return fmt.Sprintf(`services:
  foundry-openai:
    image: vllm/vllm-openai:latest
    command: ["--host", "0.0.0.0", "--port", "8000", "--model", "${FOUNDRY_OPENAI_MODEL:-%s}"]
    environment:
      AZURE_OPENAI_ACCOUNT: %s
      FOUNDRY_OPENAI_MODEL: %s
    ports:
      - "8000:8000"
    volumes:
      - ./foundry-openai/models:/models
      - ./foundry-openai/cache:/root/.cache
`, modelName, accountName, modelName)
}

func (m *FoundryOpenAIMapper) generateAppChangeConfig(accountName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_AZURE_OPENAI_ACCOUNT=%s
TARGET_OPENAI_BASE_URL=http://foundry-openai:8000/v1
TARGET_OPENAI_MODEL=${FOUNDRY_OPENAI_MODEL:-local-model}
GENERATED_PATCH=config/foundry-openai/generated-client.patch
`, accountName)
}

func (m *FoundryOpenAIMapper) generateAccountReport(res *resource.AWSResource, accountName, modelName string) string {
	return fmt.Sprintf(`source: azurerm_cognitive_account
service: foundry-openai
account: %s
kind: %s
location: %s
model: %s
target: vllm-openai
`, accountName, res.GetConfigString("kind"), res.GetConfigString("location"), modelName)
}

func (m *FoundryOpenAIMapper) generateClientPatch(accountName string) string {
	return fmt.Sprintf(`--- a/app/ai.env
+++ b/app/ai.env
@@
-AZURE_OPENAI_ACCOUNT=%s
+OPENAI_BASE_URL=http://foundry-openai:8000/v1
+OPENAI_MODEL=${FOUNDRY_OPENAI_MODEL:-local-model}
+AI_PROVIDER=openai-compatible
`, accountName)
}

func (m *FoundryOpenAIMapper) generateExportScript(accountName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
ACCOUNT_NAME=%q
OUTPUT_DIR="${OUTPUT_DIR:-./foundry-openai-export}"
mkdir -p "$OUTPUT_DIR" foundry-openai/models
az cognitiveservices account show --name "$ACCOUNT_NAME" --resource-group "${AZURE_RESOURCE_GROUP}" > "$OUTPUT_DIR/account.json"
az cognitiveservices account deployment list --name "$ACCOUNT_NAME" --resource-group "${AZURE_RESOURCE_GROUP}" > "$OUTPUT_DIR/deployments.json"
`, accountName)
}

func (m *FoundryOpenAIMapper) generateMigrateScript(accountName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/foundry-openai/account-report.yaml
test -d foundry-openai/models
echo "Foundry/OpenAI account %s mapped to vLLM OpenAI-compatible target"
`, accountName)
}

func (m *FoundryOpenAIMapper) generateValidateScript(accountName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s docker-compose.foundry-openai.yml
test -s config/foundry-openai/app-change.env
grep -q "SOURCE_AZURE_OPENAI_ACCOUNT=%s" config/foundry-openai/app-change.env
echo "Foundry/OpenAI account %s validates against vLLM target"
`, accountName, accountName)
}

func (m *FoundryOpenAIMapper) generateBackupScript(accountName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/foundry-openai-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/foundry-openai docker-compose.foundry-openai.yml foundry-openai-export foundry-openai/models
echo "$archive"
`, sanitizeFoundryOpenAIName(accountName))
}

func (m *FoundryOpenAIMapper) generateCutoverScript(accountName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/foundry-openai/app-change.env
test "$SOURCE_AZURE_OPENAI_ACCOUNT" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and point AI clients at $TARGET_OPENAI_BASE_URL"
`, accountName)
}

func foundryOpenAIRunbook(accountName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "ai-inference", "source": "azurerm_cognitive_account", "account": accountName, "target": "vllm-openai"}
	return []domainrunbook.Step{
		foundryOpenAIStep("export-foundry-openai-account", "Export Foundry/OpenAI account", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_foundry_openai_account.sh"}, "Foundry/OpenAI account and deployments are exported", metadata),
		foundryOpenAIStep("provision-foundry-openai-vllm", "Provision Foundry/OpenAI vLLM target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s docker-compose.foundry-openai.yml"}, "vLLM compose target is rendered", metadata),
		foundryOpenAIStep("migrate-foundry-openai-models", "Migrate Foundry/OpenAI models", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_foundry_openai_models.sh"}, "model artifacts are staged for vLLM", metadata),
		foundryOpenAIStep("validate-foundry-openai-vllm", "Validate Foundry/OpenAI vLLM target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_foundry_openai_endpoint.sh"}, "vLLM target config validates", metadata),
		foundryOpenAIStep("backup-foundry-openai-config", "Backup Foundry/OpenAI config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_foundry_openai_config.sh"}, "Foundry/OpenAI migration artifacts are archived", metadata),
		foundryOpenAIStep("cutover-foundry-openai-clients", "Cut over Foundry/OpenAI clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_foundry_openai_clients.sh"}, "clients use generated OpenAI-compatible endpoint", metadata),
		foundryOpenAIStep("rollback-foundry-openai-account", "Keep Foundry/OpenAI source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Foundry/OpenAI remains authoritative until vLLM validation passes", metadata),
	}
}

func foundryOpenAIStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmptyFoundryOpenAI(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "foundry-openai"
}

func sanitizeFoundryOpenAIName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "-", " ", "-").Replace(value)
	if value == "" {
		return "foundry-openai"
	}
	return value
}
