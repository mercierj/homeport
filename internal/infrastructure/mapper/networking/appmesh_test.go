package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestAppMeshConformanceManagedAToZ(t *testing.T) {
	result, err := NewAppMeshMapper().Map(context.Background(), managedAppMeshFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated App Mesh migration", result.ManualSteps)
	}
	if result.DockerService.Image != "istio/pilot:1.22.3" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Istio control plane target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/istio/virtual-service.yaml", "config/appmesh/mesh-map.yaml", "config/appmesh/app-change.env", "config/appmesh/generated-mesh.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/appmesh/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_APP_MESH=orders-mesh", "TARGET_SERVICE_MESH=istio", "ISTIO_NAMESPACE=istio-system"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_appmesh.sh", "provision_istio_mesh.sh", "migrate_appmesh_routes.sh", "validate_istio_mesh.sh", "backup_appmesh_config.sh", "cutover_appmesh_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-appmesh":          domainrunbook.StepTypeCommand,
		"provision-istio-mesh":    domainrunbook.StepTypeCommand,
		"migrate-appmesh-routes":  domainrunbook.StepTypeCommand,
		"validate-istio-mesh":     domainrunbook.StepTypeCommand,
		"backup-appmesh-config":   domainrunbook.StepTypeCommand,
		"cutover-appmesh-clients": domainrunbook.StepTypeAPICall,
		"rollback-appmesh-source": domainrunbook.StepTypeRollback,
	} {
		if !hasAppMeshRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewAppMeshMapper(t *testing.T) {
	m := NewAppMeshMapper()
	if m == nil {
		t.Fatal("NewAppMeshMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAppMeshMesh {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAppMeshMesh)
	}
}

func managedAppMeshFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "orders-mesh",
		Type:   resource.TypeAppMeshMesh,
		Name:   "orders-mesh",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"mesh_name": "orders-mesh",
		},
	}
}

func hasAppMeshRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
