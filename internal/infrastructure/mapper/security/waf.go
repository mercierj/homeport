package security

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type WAFMapper struct {
	*mapper.BaseMapper
}

func NewWAFMapper() *WAFMapper {
	return &WAFMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeWAFWebACL, nil)}
}

func (m *WAFMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	scope := res.GetConfigString("scope")
	if scope == "" {
		scope = "REGIONAL"
	}

	result := mapper.NewMappingResult("modsecurity")
	svc := result.DockerService
	svc.Image = "owasp/modsecurity-crs:nginx"
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{"./config/waf:/etc/nginx/templates", "./logs/waf:/var/log/nginx"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":               "aws_wafv2_web_acl",
		"homeport.web_acl":              name,
		"homeport.scope":                scope,
		"homeport.target":               "modsecurity",
		"traefik.enable":                "true",
		"traefik.http.routers.waf.rule": "Host(`waf.localhost`)",
		"traefik.http.services.waf.loadbalancer.server.port": "8080",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:8080/healthz"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/waf/modsecurity.conf", []byte(m.modSecurityConfig()))
	result.AddConfig("config/waf/crs-setup.conf", []byte(m.crsSetup(name)))
	result.AddConfig("config/waf/aws-rules-map.yaml", []byte(m.rulesMap(name, scope)))
	result.AddConfig("config/waf/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/waf/generated-route.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_waf_web_acl.sh", []byte(m.exportScript(name, scope, res.Region)))
	result.AddScript("provision_modsecurity_waf.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_waf_rules.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_waf_rules.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_waf_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_waf_routes.sh", []byte(m.cutoverScript(name)))
	for _, step := range wafRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *WAFMapper) modSecurityConfig() string {
	return "SecRuleEngine On\nSecRequestBodyAccess On\nSecResponseBodyAccess Off\nSecAuditEngine RelevantOnly\nSecAuditLog /var/log/nginx/modsec_audit.log\n"
}

func (m *WAFMapper) crsSetup(name string) string {
	return fmt.Sprintf("# Generated from AWS WAF web ACL %s\nSecDefaultAction \"phase:1,log,auditlog,pass\"\nSecDefaultAction \"phase:2,log,auditlog,pass\"\n", name)
}

func (m *WAFMapper) rulesMap(name, scope string) string {
	return fmt.Sprintf(`web_acl: %s
scope: %s
target: modsecurity-owasp-crs
managed_rule_groups:
  - AWSManagedRulesCommonRuleSet
  - AWSManagedRulesKnownBadInputsRuleSet
custom_rules_file: config/waf/modsecurity.conf
`, name, scope)
}

func (m *WAFMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_WEB_ACL=%s
TARGET_WAF=modsecurity
WAF_UPSTREAM=http://modsecurity:8080
GENERATED_PATCH=config/waf/generated-route.patch
`, name)
}

func (m *WAFMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/edge/routes.env
+++ b/edge/routes.env
@@
-AWS_WAF_WEB_ACL=%s
+WAF_UPSTREAM=http://modsecurity:8080
+WAF_ENGINE=modsecurity
`, name)
}

func (m *WAFMapper) exportScript(name, scope, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
WEB_ACL_NAME=%q
SCOPE=%q
OUTPUT_DIR="./waf-export"
mkdir -p "$OUTPUT_DIR"
aws wafv2 list-web-acls --scope "$SCOPE" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/web-acls.json"
web_acl_id=$(jq -r --arg name "$WEB_ACL_NAME" '.WebACLs[] | select(.Name == $name) | .Id' "$OUTPUT_DIR/web-acls.json")
test -n "$web_acl_id"
aws wafv2 get-web-acl --name "$WEB_ACL_NAME" --id "$web_acl_id" --scope "$SCOPE" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/web-acl.json"
`, region, name, scope)
}

func (m *WAFMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/waf/modsecurity.conf\ntest -s config/waf/crs-setup.conf\necho \"ModSecurity WAF ready for %s\"\n", name)
}

func (m *WAFMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s waf-export/web-acl.json\ntest -s config/waf/aws-rules-map.yaml\necho \"AWS WAF web ACL %s mapped to ModSecurity CRS\"\n", name)
}

func (m *WAFMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ncurl -fsS http://localhost:8080/healthz >/tmp/homeport-waf-health.txt\ngrep -q %q config/waf/aws-rules-map.yaml\ntest -s config/waf/app-change.env\n", name)
}

func (m *WAFMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-waf-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/waf export_waf_web_acl.sh provision_modsecurity_waf.sh migrate_waf_rules.sh validate_waf_rules.sh cutover_waf_routes.sh
echo "$archive"
`, name)
}

func (m *WAFMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/waf/app-change.env
test "$SOURCE_WEB_ACL" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route edge traffic through $WAF_UPSTREAM"
`, name)
}

func wafRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "waf",
		"source":              "aws_wafv2_web_acl",
		"web_acl":             name,
		"HOMEPORT_TARGET":     "modsecurity",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		wafStep("export-waf-web-acl", "Export WAF web ACL", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_waf_web_acl.sh"}, "WAF rule groups and custom rules are exported", metadata),
		wafStep("provision-modsecurity-waf", "Provision ModSecurity WAF", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_modsecurity_waf.sh"}, "ModSecurity and OWASP CRS config are present", metadata),
		wafStep("migrate-waf-rules", "Migrate WAF rules", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_waf_rules.sh"}, "AWS WAF rules are mapped to CRS and custom rules", metadata),
		wafStep("validate-waf-rules", "Validate WAF rules", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_waf_rules.sh"}, "WAF health and generated rules validate", metadata),
		wafStep("backup-waf-config", "Backup WAF config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_waf_config.sh"}, "WAF migration artifacts are archived", metadata),
		wafStep("cutover-waf-routes", "Cut over WAF routes", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_waf_routes.sh"}, "edge routes use ModSecurity WAF", metadata),
		wafStep("rollback-waf-source", "Keep AWS WAF source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS WAF remains authoritative until ModSecurity validation passes", metadata),
	}
}

func wafStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
