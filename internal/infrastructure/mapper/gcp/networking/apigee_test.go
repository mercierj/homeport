package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestApigeeConformanceManagedAToZ(t *testing.T) {
	result, err := NewApigeeMapper().Map(context.Background(), managedApigeeFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Apigee migration", result.ManualSteps)
	}
	if result.DockerService.Image != "kong:3.6" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Kong target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/apigee/kong.yaml", "config/apigee/app-change.env", "config/apigee/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/apigee/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_APIGEE_ORG=payments", "TARGET_API_GATEWAY=kong"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_apigee_config.sh", "provision_kong_gateway.sh", "migrate_apigee_proxies.sh", "validate_kong_gateway.sh", "backup_apigee_config.sh", "cutover_apigee_routes.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-apigee-config":   domainrunbook.StepTypeCommand,
		"provision-kong-gateway": domainrunbook.StepTypeCommand,
		"migrate-apigee-proxies": domainrunbook.StepTypeCommand,
		"validate-kong-gateway":  domainrunbook.StepTypeCommand,
		"backup-apigee-config":   domainrunbook.StepTypeCommand,
		"cutover-apigee-routes":  domainrunbook.StepTypeAPICall,
		"rollback-apigee-source": domainrunbook.StepTypeRollback,
	} {
		if !hasApigeeRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewApigeeMapper(t *testing.T) {
	m := NewApigeeMapper()
	if m == nil {
		t.Fatal("NewApigeeMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeApigeeOrganization {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeApigeeOrganization)
	}
}

func managedApigeeFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "payments",
		Type:   resource.TypeApigeeOrganization,
		Name:   "payments",
		Region: "europe-west1",
		Config: map[string]interface{}{
			"name": "payments",
		},
	}
}

func hasApigeeRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
