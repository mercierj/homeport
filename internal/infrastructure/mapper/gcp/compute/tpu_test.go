package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestTPUConformanceManagedAToZ(t *testing.T) {
	result, err := NewTPUNodeMapper().Map(context.Background(), managedTPUFixture(resource.TypeTPUNode))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated TPU migration", result.ManualSteps)
	}
	if result.DockerService.Image != "rancher/k3s:v1.29.0-k3s1" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Kubernetes target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/tpu/accelerator-job.yaml", "config/tpu/app-change.env", "config/tpu/generated-accelerator.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/tpu/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_TPU_RESOURCE=checkout-tpu", "TARGET_ACCELERATOR=kubernetes"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_tpu_config.sh", "provision_accelerator_cluster.sh", "migrate_tpu_workload.sh", "validate_accelerator_job.sh", "backup_tpu_config.sh", "cutover_tpu_workloads.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-tpu-config":             domainrunbook.StepTypeCommand,
		"provision-accelerator-cluster": domainrunbook.StepTypeCommand,
		"migrate-tpu-workload":          domainrunbook.StepTypeCommand,
		"validate-accelerator-job":      domainrunbook.StepTypeCommand,
		"backup-tpu-config":             domainrunbook.StepTypeCommand,
		"cutover-tpu-workloads":         domainrunbook.StepTypeAPICall,
		"rollback-tpu-source":           domainrunbook.StepTypeRollback,
	} {
		if !hasTPURunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewTPUMappers(t *testing.T) {
	if NewTPUNodeMapper().ResourceType() != resource.TypeTPUNode {
		t.Fatalf("node mapper type = %s, want %s", NewTPUNodeMapper().ResourceType(), resource.TypeTPUNode)
	}
	if NewTPUV2VMMapper().ResourceType() != resource.TypeTPUV2VM {
		t.Fatalf("v2 vm mapper type = %s, want %s", NewTPUV2VMMapper().ResourceType(), resource.TypeTPUV2VM)
	}
}

func managedTPUFixture(resType resource.Type) *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1-b/nodes/checkout-tpu",
		Type: resType,
		Name: "checkout-tpu",
		Config: map[string]interface{}{
			"name":               "checkout-tpu",
			"zone":               "europe-west1-b",
			"accelerator_type":   "v4-8",
			"tensorflow_version": "2.15",
		},
	}
}

func hasTPURunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
