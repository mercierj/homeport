package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestWAFConformanceManagedAToZ(t *testing.T) {
	result, err := NewWAFMapper().Map(context.Background(), managedWAFFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated WAF migration", result.ManualSteps)
	}
	if result.DockerService.Image != "owasp/modsecurity-crs:nginx" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA ModSecurity target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/waf/modsecurity.conf", "config/waf/crs-setup.conf", "config/waf/aws-rules-map.yaml", "config/waf/app-change.env", "config/waf/generated-route.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/waf/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_WEB_ACL=edge-acl", "TARGET_WAF=modsecurity", "WAF_UPSTREAM=http://modsecurity:8080"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_waf_web_acl.sh", "provision_modsecurity_waf.sh", "migrate_waf_rules.sh", "validate_waf_rules.sh", "backup_waf_config.sh", "cutover_waf_routes.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-waf-web-acl":        domainrunbook.StepTypeCommand,
		"provision-modsecurity-waf": domainrunbook.StepTypeCommand,
		"migrate-waf-rules":         domainrunbook.StepTypeCommand,
		"validate-waf-rules":        domainrunbook.StepTypeCommand,
		"backup-waf-config":         domainrunbook.StepTypeCommand,
		"cutover-waf-routes":        domainrunbook.StepTypeAPICall,
		"rollback-waf-source":       domainrunbook.StepTypeRollback,
	} {
		if !hasWAFRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewWAFMapper(t *testing.T) {
	m := NewWAFMapper()
	if m == nil {
		t.Fatal("NewWAFMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeWAFWebACL {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeWAFWebACL)
	}
}

func managedWAFFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "arn:aws:wafv2:eu-west-1:123456789012:regional/webacl/edge-acl/1234",
		Type:   resource.TypeWAFWebACL,
		Name:   "edge-acl",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name":  "edge-acl",
			"scope": "REGIONAL",
		},
	}
}

func hasWAFRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
