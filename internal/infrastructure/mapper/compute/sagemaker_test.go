package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestSageMakerConformanceManagedAToZ(t *testing.T) {
	result, err := NewSageMakerMapper().Map(context.Background(), managedSageMakerFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated SageMaker migration", result.ManualSteps)
	}
	if result.DockerService.Image != "nvcr.io/nvidia/tritonserver:24.05-py3" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Triton target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/sagemaker/model-map.yaml", "config/sagemaker/app-change.env", "config/sagemaker/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/sagemaker/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_SAGEMAKER_ENDPOINT=fraud-endpoint", "TARGET_INFERENCE_SERVICE=triton", "TRITON_HTTP_URL=http://triton:8000/v2/models/fraud-model/infer"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_sagemaker_endpoint.sh", "provision_triton_model_repo.sh", "migrate_sagemaker_model.sh", "validate_triton_inference.sh", "backup_sagemaker_config.sh", "cutover_sagemaker_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-sagemaker-endpoint":   domainrunbook.StepTypeCommand,
		"provision-triton-model-repo": domainrunbook.StepTypeCommand,
		"migrate-sagemaker-model":     domainrunbook.StepTypeCommand,
		"validate-triton-inference":   domainrunbook.StepTypeCommand,
		"backup-sagemaker-config":     domainrunbook.StepTypeCommand,
		"cutover-sagemaker-clients":   domainrunbook.StepTypeAPICall,
		"rollback-sagemaker-source":   domainrunbook.StepTypeRollback,
	} {
		if !hasSageMakerRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewSageMakerMapper(t *testing.T) {
	m := NewSageMakerMapper()
	if m == nil {
		t.Fatal("NewSageMakerMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeSageMakerEndpoint {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeSageMakerEndpoint)
	}
}

func managedSageMakerFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "arn:aws:sagemaker:eu-west-1:123456789012:endpoint/fraud-endpoint",
		Type:   resource.TypeSageMakerEndpoint,
		Name:   "fraud-endpoint",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"endpoint_name": "fraud-endpoint",
			"model_name":    "fraud-model",
			"instance_type": "ml.m5.xlarge",
		},
	}
}

func hasSageMakerRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
