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

type VertexAIMapper struct {
	*mapper.BaseMapper
}

func NewVertexAIMapper() *VertexAIMapper {
	return &VertexAIMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeVertexAIEndpoint, nil)}
}

func (m *VertexAIMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	endpointName := firstNonEmptyVertexAI(res.GetConfigString("name"), res.GetConfigString("display_name"), res.Name)
	modelName := firstNonEmptyVertexAI(res.GetConfigString("model"), res.GetConfigString("deployed_model"), "local-model")

	result := mapper.NewMappingResult("vertex-ai")
	svc := result.DockerService
	svc.Image = "vllm/vllm-openai:latest"
	svc.Command = []string{"--host", "0.0.0.0", "--port", "8000", "--model", "${VERTEX_AI_MODEL:-" + modelName + "}"}
	svc.Ports = []string{"8000:8000"}
	svc.Volumes = []string{"./vertex-ai/models:/models", "./vertex-ai/cache:/root/.cache"}
	svc.Environment = map[string]string{
		"VERTEX_AI_ENDPOINT_NAME": endpointName,
		"VERTEX_AI_MODEL":         modelName,
	}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "curl -fsS http://localhost:8000/v1/models >/dev/null || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeVertexAIEndpoint), "homeport.endpoint": endpointName, "homeport.target": "vllm-openai"}

	result.AddConfig("docker-compose.vertex-ai.yml", []byte(m.generateCompose(endpointName, modelName)))
	result.AddConfig("config/vertex-ai/app-change.env", []byte(m.generateAppChangeConfig(endpointName)))
	result.AddConfig("config/vertex-ai/endpoint-report.yaml", []byte(m.generateEndpointReport(res, endpointName, modelName)))
	result.AddConfig("config/vertex-ai/generated-client.patch", []byte(m.generateClientPatch(endpointName)))
	result.AddScript("export_vertex_ai_endpoint.sh", []byte(m.generateExportScript(endpointName)))
	result.AddScript("migrate_vertex_ai_model.sh", []byte(m.generateMigrateScript(endpointName)))
	result.AddScript("validate_vertex_ai_endpoint.sh", []byte(m.generateValidateScript(endpointName)))
	result.AddScript("backup_vertex_ai_config.sh", []byte(m.generateBackupScript(endpointName)))
	result.AddScript("cutover_vertex_ai_clients.sh", []byte(m.generateCutoverScript(endpointName)))
	for _, step := range vertexAIRunbook(endpointName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *VertexAIMapper) generateCompose(endpointName, modelName string) string {
	return fmt.Sprintf(`services:
  vertex-ai:
    image: vllm/vllm-openai:latest
    command: ["--host", "0.0.0.0", "--port", "8000", "--model", "${VERTEX_AI_MODEL:-%s}"]
    environment:
      VERTEX_AI_ENDPOINT_NAME: %s
      VERTEX_AI_MODEL: %s
    ports:
      - "8000:8000"
    volumes:
      - ./vertex-ai/models:/models
      - ./vertex-ai/cache:/root/.cache
`, modelName, endpointName, modelName)
}

func (m *VertexAIMapper) generateAppChangeConfig(endpointName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_VERTEX_AI_ENDPOINT=%s
TARGET_OPENAI_BASE_URL=http://vertex-ai:8000/v1
TARGET_OPENAI_MODEL=${VERTEX_AI_MODEL:-local-model}
GENERATED_PATCH=config/vertex-ai/generated-client.patch
`, endpointName)
}

func (m *VertexAIMapper) generateEndpointReport(res *resource.AWSResource, endpointName, modelName string) string {
	return fmt.Sprintf(`source: google_vertex_ai_endpoint
endpoint: %s
region: %s
model: %s
target: vllm-openai
`, endpointName, res.GetConfigString("region"), modelName)
}

func (m *VertexAIMapper) generateClientPatch(endpointName string) string {
	return fmt.Sprintf(`--- a/app/ai.env
+++ b/app/ai.env
@@
-VERTEX_AI_ENDPOINT=%s
+OPENAI_BASE_URL=http://vertex-ai:8000/v1
+OPENAI_MODEL=${VERTEX_AI_MODEL:-local-model}
+AI_PROVIDER=openai-compatible
`, endpointName)
}

func (m *VertexAIMapper) generateExportScript(endpointName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
ENDPOINT_NAME=%q
OUTPUT_DIR="${OUTPUT_DIR:-./vertex-ai-export}"
mkdir -p "$OUTPUT_DIR" vertex-ai/models
gcloud ai endpoints describe "$ENDPOINT_NAME" --format=json > "$OUTPUT_DIR/endpoint.json"
gcloud ai models list --format=json > "$OUTPUT_DIR/models.json"
`, endpointName)
}

func (m *VertexAIMapper) generateMigrateScript(endpointName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/vertex-ai/endpoint-report.yaml
test -d vertex-ai/models
echo "Vertex AI endpoint %s mapped to vLLM OpenAI-compatible target"
`, endpointName)
}

func (m *VertexAIMapper) generateValidateScript(endpointName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s docker-compose.vertex-ai.yml
test -s config/vertex-ai/app-change.env
grep -q "SOURCE_VERTEX_AI_ENDPOINT=%s" config/vertex-ai/app-change.env
echo "Vertex AI endpoint %s validates against vLLM target"
`, endpointName, endpointName)
}

func (m *VertexAIMapper) generateBackupScript(endpointName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/vertex-ai-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/vertex-ai docker-compose.vertex-ai.yml vertex-ai-export vertex-ai/models
echo "$archive"
`, sanitizeVertexAIName(endpointName))
}

func (m *VertexAIMapper) generateCutoverScript(endpointName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/vertex-ai/app-change.env
test "$SOURCE_VERTEX_AI_ENDPOINT" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and point AI clients at $TARGET_OPENAI_BASE_URL"
`, endpointName)
}

func vertexAIRunbook(endpointName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "ai-inference", "source": "google_vertex_ai_endpoint", "endpoint": endpointName, "target": "vllm-openai"}
	return []domainrunbook.Step{
		vertexAIStep("export-vertex-ai-endpoint", "Export Vertex AI endpoint", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_vertex_ai_endpoint.sh"}, "Vertex AI endpoint and models are exported", metadata),
		vertexAIStep("provision-vllm-endpoint", "Provision vLLM endpoint", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s docker-compose.vertex-ai.yml"}, "vLLM compose target is rendered", metadata),
		vertexAIStep("migrate-vertex-ai-model", "Migrate Vertex AI model", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_vertex_ai_model.sh"}, "model artifacts are staged for vLLM", metadata),
		vertexAIStep("validate-vllm-endpoint", "Validate vLLM endpoint", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_vertex_ai_endpoint.sh"}, "vLLM target config validates", metadata),
		vertexAIStep("backup-vertex-ai-config", "Backup Vertex AI config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_vertex_ai_config.sh"}, "Vertex AI migration artifacts are archived", metadata),
		vertexAIStep("cutover-vertex-ai-clients", "Cut over Vertex AI clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_vertex_ai_clients.sh"}, "clients use generated OpenAI-compatible endpoint", metadata),
		vertexAIStep("rollback-vertex-ai-endpoint", "Keep Vertex AI source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Vertex AI remains authoritative until vLLM validation passes", metadata),
	}
}

func vertexAIStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmptyVertexAI(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "vertex-ai-endpoint"
}

func sanitizeVertexAIName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "-", " ", "-").Replace(value)
	if value == "" {
		return "vertex-ai"
	}
	return value
}
