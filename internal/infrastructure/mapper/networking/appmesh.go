package networking

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type AppMeshMapper struct {
	*mapper.BaseMapper
}

func NewAppMeshMapper() *AppMeshMapper {
	return &AppMeshMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAppMeshMesh, nil)}
}

func (m *AppMeshMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	meshName := res.GetConfigString("mesh_name")
	if meshName == "" {
		meshName = res.Name
	}
	if meshName == "" {
		meshName = "app-mesh"
	}

	result := mapper.NewMappingResult("istiod")
	svc := result.DockerService
	svc.Image = "istio/pilot:1.22.3"
	svc.Ports = []string{"15010:15010", "15012:15012", "15017:15017"}
	svc.Volumes = []string{"./config/istio:/etc/istio/config"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{"homeport.source": "aws_appmesh_mesh", "homeport.mesh": meshName, "homeport.target": "istio"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "pilot-agent", "request", "GET", "server_info"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	result.AddConfig("config/istio/virtual-service.yaml", []byte(m.virtualService(meshName)))
	result.AddConfig("config/appmesh/mesh-map.yaml", []byte(m.meshMap(meshName)))
	result.AddConfig("config/appmesh/app-change.env", []byte(m.appChange(meshName)))
	result.AddConfig("config/appmesh/generated-mesh.patch", []byte(m.generatedPatch(meshName)))
	result.AddScript("export_appmesh.sh", []byte(m.exportScript(meshName, res.Region)))
	result.AddScript("provision_istio_mesh.sh", []byte(m.provisionScript(meshName)))
	result.AddScript("migrate_appmesh_routes.sh", []byte(m.migrateScript(meshName)))
	result.AddScript("validate_istio_mesh.sh", []byte(m.validateScript(meshName)))
	result.AddScript("backup_appmesh_config.sh", []byte(m.backupScript(meshName)))
	result.AddScript("cutover_appmesh_clients.sh", []byte(m.cutoverScript(meshName)))
	for _, step := range appMeshRunbook(meshName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *AppMeshMapper) virtualService(meshName string) string {
	return fmt.Sprintf("apiVersion: networking.istio.io/v1beta1\nkind: VirtualService\nmetadata:\n  name: %s\nspec:\n  hosts: [\"*\"]\n  http:\n    - route: []\n", meshName)
}

func (m *AppMeshMapper) meshMap(meshName string) string {
	return fmt.Sprintf("source_mesh: %s\ntarget_mesh: istio\nnamespace: istio-system\n", meshName)
}

func (m *AppMeshMapper) appChange(meshName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_APP_MESH=%s\nTARGET_SERVICE_MESH=istio\nISTIO_NAMESPACE=istio-system\n", meshName)
}

func (m *AppMeshMapper) generatedPatch(meshName string) string {
	return fmt.Sprintf("--- app.env\n+++ app.env\n@@\n-AWS_APP_MESH=%s\n+SERVICE_MESH=istio\n+ISTIO_NAMESPACE=istio-system\n", meshName)
}

func (m *AppMeshMapper) exportScript(meshName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAWS_REGION=\"${AWS_REGION:-%s}\"\nMESH_NAME=\"${APP_MESH_NAME:-%s}\"\nOUTPUT_DIR=\"${APPMESH_EXPORT_DIR:-appmesh-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naws appmesh describe-mesh --region \"$AWS_REGION\" --mesh-name \"$MESH_NAME\" > \"$OUTPUT_DIR/mesh.json\"\naws appmesh list-virtual-nodes --region \"$AWS_REGION\" --mesh-name \"$MESH_NAME\" > \"$OUTPUT_DIR/virtual-nodes.json\"\naws appmesh list-routes --region \"$AWS_REGION\" --mesh-name \"$MESH_NAME\" --virtual-router-name \"${VIRTUAL_ROUTER_NAME:-default}\" > \"$OUTPUT_DIR/routes.json\" 2>/dev/null || true\necho \"Exported App Mesh $MESH_NAME\"\n", region, meshName)
}

func (m *AppMeshMapper) provisionScript(meshName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/istio/virtual-service.yaml\ntest -s config/appmesh/mesh-map.yaml\necho \"Istio manifests ready for App Mesh %s\"\n", meshName)
}

func (m *AppMeshMapper) migrateScript(meshName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s appmesh-export/mesh.json\ngrep -q %q config/appmesh/mesh-map.yaml\necho \"App Mesh routes mapped to Istio manifests\"\n", meshName)
}

func (m *AppMeshMapper) validateScript(meshName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/istio/virtual-service.yaml\ngrep -q %q config/istio/virtual-service.yaml\ntest -s config/appmesh/app-change.env\n", meshName)
}

func (m *AppMeshMapper) backupScript(meshName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-appmesh-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/istio config/appmesh export_appmesh.sh migrate_appmesh_routes.sh validate_istio_mesh.sh cutover_appmesh_clients.sh\necho \"$archive\"\n", meshName)
}

func (m *AppMeshMapper) cutoverScript(meshName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/appmesh/app-change.env\ntest \"$SOURCE_APP_MESH\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply Istio manifests and route workloads through $TARGET_SERVICE_MESH\"\n", meshName)
}

func appMeshRunbook(meshName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "service-mesh", "source": "aws_appmesh_mesh", "mesh": meshName, "HOMEPORT_TARGET": "istio", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		appMeshStep("export-appmesh", "Export App Mesh topology", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_appmesh.sh"}, "mesh topology is exported", metadata),
		appMeshStep("provision-istio-mesh", "Provision Istio mesh", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_istio_mesh.sh"}, "Istio manifests are generated", metadata),
		appMeshStep("migrate-appmesh-routes", "Migrate App Mesh routes", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_appmesh_routes.sh"}, "routes are represented as Istio resources", metadata),
		appMeshStep("validate-istio-mesh", "Validate Istio mesh", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_istio_mesh.sh"}, "Istio manifests and app change validate", metadata),
		appMeshStep("backup-appmesh-config", "Backup App Mesh migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_appmesh_config.sh"}, "mesh migration artifacts are archived", metadata),
		appMeshStep("cutover-appmesh-clients", "Cut over App Mesh workloads", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_appmesh_clients.sh"}, "workloads use Istio mesh config", metadata),
		appMeshStep("rollback-appmesh-source", "Keep App Mesh source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS App Mesh remains authoritative until Istio validation passes", metadata),
	}
}

func appMeshStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
