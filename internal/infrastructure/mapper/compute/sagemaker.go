package compute

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type SageMakerMapper struct {
	*mapper.BaseMapper
}

func NewSageMakerMapper() *SageMakerMapper {
	return &SageMakerMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeSageMakerEndpoint, nil)}
}

func (m *SageMakerMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	endpointName := res.GetConfigString("endpoint_name")
	if endpointName == "" {
		endpointName = res.GetConfigString("name")
	}
	if endpointName == "" {
		endpointName = res.Name
	}
	modelName := res.GetConfigString("model_name")
	if modelName == "" {
		modelName = endpointName
	}

	result := mapper.NewMappingResult("triton")
	svc := result.DockerService
	svc.Image = "nvcr.io/nvidia/tritonserver:24.05-py3"
	svc.Command = []string{"tritonserver", "--model-repository=/models", "--http-port=8000", "--grpc-port=8001", "--metrics-port=8002"}
	svc.Ports = []string{"8000:8000", "8001:8001", "8002:8002"}
	svc.Volumes = []string{"./models/triton:/models", "./config/sagemaker:/etc/homeport/sagemaker"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":                  "aws_sagemaker_endpoint",
		"homeport.endpoint":                endpointName,
		"homeport.target":                  "triton",
		"traefik.enable":                   "true",
		"traefik.http.routers.triton.rule": "Host(`triton.localhost`)",
		"traefik.http.services.triton.loadbalancer.server.port": "8000",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:8000/v2/health/ready"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/sagemaker/model-map.yaml", []byte(m.modelMap(endpointName, modelName, res)))
	result.AddConfig("config/sagemaker/app-change.env", []byte(m.appChange(endpointName, modelName)))
	result.AddConfig("config/sagemaker/generated-client.patch", []byte(m.generatedPatch(endpointName, modelName)))
	result.AddScript("export_sagemaker_endpoint.sh", []byte(m.exportScript(endpointName, res.Region)))
	result.AddScript("provision_triton_model_repo.sh", []byte(m.provisionScript(modelName)))
	result.AddScript("migrate_sagemaker_model.sh", []byte(m.migrateScript(endpointName, modelName)))
	result.AddScript("validate_triton_inference.sh", []byte(m.validateScript(modelName)))
	result.AddScript("backup_sagemaker_config.sh", []byte(m.backupScript(endpointName)))
	result.AddScript("cutover_sagemaker_clients.sh", []byte(m.cutoverScript(endpointName)))
	for _, step := range sagemakerRunbook(endpointName, modelName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *SageMakerMapper) modelMap(endpointName, modelName string, res *resource.AWSResource) string {
	instanceType := res.GetConfigString("instance_type")
	if instanceType == "" {
		instanceType = "ml.m5.large"
	}
	return fmt.Sprintf(`endpoint: %s
model: %s
target: triton
source_instance_type: %s
model_artifacts: sagemaker-export/model.tar.gz
model_repository: models/triton/%s/1
protocols:
  http: http://triton:8000/v2/models/%s/infer
  grpc: triton:8001
`, endpointName, modelName, instanceType, modelName, modelName)
}

func (m *SageMakerMapper) appChange(endpointName, modelName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_SAGEMAKER_ENDPOINT=%s
TARGET_INFERENCE_SERVICE=triton
TRITON_HTTP_URL=http://triton:8000/v2/models/%s/infer
TRITON_GRPC_URL=triton:8001
GENERATED_PATCH=config/sagemaker/generated-client.patch
`, endpointName, modelName)
}

func (m *SageMakerMapper) generatedPatch(endpointName, modelName string) string {
	return fmt.Sprintf(`--- a/app/inference_client.env
+++ b/app/inference_client.env
@@
-SAGEMAKER_ENDPOINT_NAME=%s
+TRITON_HTTP_URL=http://triton:8000/v2/models/%s/infer
+INFERENCE_CLIENT=triton
`, endpointName, modelName)
}

func (m *SageMakerMapper) exportScript(endpointName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
ENDPOINT_NAME=%q
OUTPUT_DIR="./sagemaker-export"
mkdir -p "$OUTPUT_DIR"
aws sagemaker describe-endpoint --endpoint-name "$ENDPOINT_NAME" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/endpoint.json"
endpoint_config=$(jq -r '.EndpointConfigName' "$OUTPUT_DIR/endpoint.json")
aws sagemaker describe-endpoint-config --endpoint-config-name "$endpoint_config" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/endpoint-config.json"
model_name=$(jq -r '.ProductionVariants[0].ModelName' "$OUTPUT_DIR/endpoint-config.json")
aws sagemaker describe-model --model-name "$model_name" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/model.json"
`, region, endpointName)
}

func (m *SageMakerMapper) provisionScript(modelName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
mkdir -p models/triton/%s/1
test -s config/sagemaker/model-map.yaml
echo "Place converted model artifacts under models/triton/%s/1"
`, modelName, modelName)
}

func (m *SageMakerMapper) migrateScript(endpointName, modelName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s sagemaker-export/model.json
test -d models/triton/%s/1
echo "SageMaker endpoint %s mapped to Triton model %s"
`, modelName, endpointName, modelName)
}

func (m *SageMakerMapper) validateScript(modelName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
curl -fsS http://localhost:8000/v2/health/ready >/tmp/homeport-triton-health.txt
curl -fsS http://localhost:8000/v2/models/%s >/tmp/homeport-triton-model.json
test -s config/sagemaker/app-change.env
`, modelName)
}

func (m *SageMakerMapper) backupScript(endpointName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-triton-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/sagemaker models/triton export_sagemaker_endpoint.sh provision_triton_model_repo.sh migrate_sagemaker_model.sh validate_triton_inference.sh cutover_sagemaker_clients.sh
echo "$archive"
`, endpointName)
}

func (m *SageMakerMapper) cutoverScript(endpointName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/sagemaker/app-change.env
test "$SOURCE_SAGEMAKER_ENDPOINT" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route inference clients to $TRITON_HTTP_URL"
`, endpointName)
}

func sagemakerRunbook(endpointName, modelName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "ml-inference",
		"source":              "aws_sagemaker_endpoint",
		"endpoint":            endpointName,
		"model":               modelName,
		"HOMEPORT_TARGET":     "triton",
		"HOMEPORT_APP_CHANGE": "generated_patch",
		"TRITON_HTTP_URL":     fmt.Sprintf("http://triton:8000/v2/models/%s/infer", modelName),
	}
	return []domainrunbook.Step{
		sagemakerStep("export-sagemaker-endpoint", "Export SageMaker endpoint", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_sagemaker_endpoint.sh"}, "endpoint, endpoint config, and model metadata are exported", metadata),
		sagemakerStep("provision-triton-model-repo", "Provision Triton model repository", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_triton_model_repo.sh"}, "Triton model repository is initialized", metadata),
		sagemakerStep("migrate-sagemaker-model", "Migrate SageMaker model", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_sagemaker_model.sh"}, "model artifacts are mapped into Triton layout", metadata),
		sagemakerStep("validate-triton-inference", "Validate Triton inference", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_triton_inference.sh"}, "Triton health and model endpoints validate", metadata),
		sagemakerStep("backup-sagemaker-config", "Backup SageMaker migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_sagemaker_config.sh"}, "SageMaker and Triton artifacts are archived", metadata),
		sagemakerStep("cutover-sagemaker-clients", "Cut over SageMaker clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_sagemaker_clients.sh"}, "clients use generated Triton patch", metadata),
		sagemakerStep("rollback-sagemaker-source", "Keep SageMaker source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS SageMaker remains authoritative until Triton inference validation passes", metadata),
	}
}

func sagemakerStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
