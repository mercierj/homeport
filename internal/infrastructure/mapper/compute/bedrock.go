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

type BedrockMapper struct {
	*mapper.BaseMapper
}

func NewBedrockMapper() *BedrockMapper {
	return &BedrockMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeBedrockModel, nil)}
}

func (m *BedrockMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	result := mapper.NewMappingResult("ollama")
	svc := result.DockerService
	svc.Image = "ollama/ollama:0.5.7"
	svc.Ports = []string{"11434:11434"}
	svc.Volumes = []string{"./data/ollama:/root/.ollama", "./config/bedrock:/etc/homeport/bedrock"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "ollama", "list"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Labels = map[string]string{
		"homeport.source": "aws_bedrock",
		"homeport.target": "ollama",
		"homeport.name":   name,
	}
	result.AddConfig("config/bedrock/models.yaml", []byte(m.modelsConfig(res)))
	result.AddConfig("config/bedrock/adapter.env", []byte("BEDROCK_ENDPOINT=http://ollama:11434\nHOMEPORT_BEDROCK_ADAPTER=ollama\n"))
	result.AddScript("backup_bedrock_config.sh", []byte(m.backupScript(name)))
	for _, step := range bedrockRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *BedrockMapper) modelsConfig(res *resource.AWSResource) string {
	var b strings.Builder
	b.WriteString("models:\n")
	models := configSlice(res.Config["models"])
	if len(models) == 0 {
		models = []map[string]interface{}{{"source": res.GetConfigString("model_id"), "target": "llama3.1"}}
	}
	for _, model := range models {
		source := configString(model["source"])
		if source == "" {
			source = "amazon.titan-text-lite"
		}
		target := configString(model["target"])
		if target == "" {
			target = "llama3.1"
		}
		b.WriteString(fmt.Sprintf("  - source: %s\n    target: %s\n", source, target))
	}
	return b.String()
}

func configSlice(value interface{}) []map[string]interface{} {
	switch typed := value.(type) {
	case []map[string]interface{}:
		return typed
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			if itemMap, ok := item.(map[string]interface{}); ok {
				out = append(out, itemMap)
			}
		}
		return out
	}
	return nil
}

func configString(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func (m *BedrockMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-bedrock-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/bedrock data/ollama
echo "$archive"
`, strings.NewReplacer("/", "-", " ", "-").Replace(name))
}

func bedrockRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "ai_model", "name": name, "source": "aws_bedrock"}
	return []domainrunbook.Step{
		bedrockStep("render-bedrock-model-map", "Render Bedrock model map", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/bedrock/models.yaml"}, "source Bedrock models map to local Ollama models", metadata),
		bedrockStep("provision-ollama-runtime", "Provision Ollama runtime", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "echo provision Ollama models"}, "Ollama serves the mapped models", metadata),
		bedrockStep("validate-bedrock-adapter", "Validate Bedrock adapter", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "-c", "echo invoke Bedrock-compatible prompt through Ollama adapter"}, "prompt invocation returns a model response", metadata),
		bedrockStep("backup-bedrock-config", "Backup Bedrock adapter config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_bedrock_config.sh"}, "model map and Ollama data are archived", metadata),
		bedrockStep("cutover-bedrock-endpoint", "Cut over Bedrock endpoint", "Cutover", domainrunbook.StepTypeCommand, []string{"sh", "-c", "echo set BEDROCK_ENDPOINT=http://ollama:11434"}, "applications use the HomePort Bedrock adapter endpoint", metadata),
		bedrockStep("rollback-bedrock-source", "Keep Bedrock as rollback authority", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Bedrock remains authoritative until adapter validation passes", metadata),
	}
}

func bedrockStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
