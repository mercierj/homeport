package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestShieldConformanceManagedAToZ(t *testing.T) {
	result, err := NewShieldMapper().Map(context.Background(), managedShieldFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Shield migration", result.ManualSteps)
	}
	if result.DockerService.Image != "owasp/modsecurity-crs:nginx" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA edge protection target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/shield/rate-limits.conf", "config/shield/protection-map.yaml", "config/shield/app-change.env", "config/shield/generated-edge.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/shield/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_SHIELD_PROTECTION=api-edge", "TARGET_EDGE_PROTECTION=edge-waf-ddos-controls", "EDGE_PROTECTION_UPSTREAM=http://edge-protection:8080"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_shield_protection.sh", "provision_edge_protection.sh", "migrate_shield_rules.sh", "validate_edge_protection.sh", "backup_shield_config.sh", "cutover_shield_routes.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-shield-protection":   domainrunbook.StepTypeCommand,
		"provision-edge-protection":  domainrunbook.StepTypeCommand,
		"migrate-shield-rules":       domainrunbook.StepTypeCommand,
		"validate-edge-protection":   domainrunbook.StepTypeCommand,
		"backup-shield-config":       domainrunbook.StepTypeCommand,
		"cutover-shield-routes":      domainrunbook.StepTypeAPICall,
		"rollback-shield-protection": domainrunbook.StepTypeRollback,
	} {
		if !hasShieldRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewShieldMapper(t *testing.T) {
	m := NewShieldMapper()
	if m == nil {
		t.Fatal("NewShieldMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeShieldProtection {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeShieldProtection)
	}
}

func managedShieldFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "arn:aws:shield::123456789012:protection/api-edge",
		Type:   resource.TypeShieldProtection,
		Name:   "api-edge",
		Region: "us-east-1",
		Config: map[string]interface{}{
			"name":         "api-edge",
			"resource_arn": "arn:aws:cloudfront::123456789012:distribution/E1234567890",
		},
	}
}

func hasShieldRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
