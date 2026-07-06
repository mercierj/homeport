package security

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type ShieldMapper struct {
	*mapper.BaseMapper
}

func NewShieldMapper() *ShieldMapper {
	return &ShieldMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeShieldProtection, nil)}
}

func (m *ShieldMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	resourceARN := res.GetConfigString("resource_arn")
	if resourceARN == "" {
		resourceARN = res.ID
	}

	result := mapper.NewMappingResult("edge-waf-ddos-controls")
	svc := result.DockerService
	svc.Image = "owasp/modsecurity-crs:nginx"
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{"./config/shield:/etc/nginx/templates", "./logs/shield:/var/log/nginx"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":                                       "aws_shield_protection",
		"homeport.shield_protection":                            name,
		"homeport.protected_resource_arn":                       resourceARN,
		"homeport.target":                                       "edge-waf-ddos-controls",
		"traefik.enable":                                        "true",
		"traefik.http.routers.shield.rule":                      "Host(`shield.localhost`)",
		"traefik.http.services.shield.loadbalancer.server.port": "8080",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:8080/healthz"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	result.AddConfig("config/shield/rate-limits.conf", []byte(m.rateLimits(name)))
	result.AddConfig("config/shield/protection-map.yaml", []byte(m.protectionMap(name, resourceARN)))
	result.AddConfig("config/shield/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/shield/generated-edge.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_shield_protection.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_edge_protection.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_shield_rules.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_edge_protection.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_shield_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_shield_routes.sh", []byte(m.cutoverScript(name)))
	for _, step := range shieldRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *ShieldMapper) rateLimits(name string) string {
	return fmt.Sprintf(`# Generated from AWS Shield protection %s
limit_req_zone $binary_remote_addr zone=shield_per_ip:10m rate=20r/s;
limit_conn_zone $binary_remote_addr zone=shield_conn_per_ip:10m;
server {
  listen 8080;
  location /healthz { return 200 "ok\n"; }
  location / {
    limit_req zone=shield_per_ip burst=40 nodelay;
    limit_conn shield_conn_per_ip 40;
    proxy_pass http://app_upstream;
  }
}
`, name)
}

func (m *ShieldMapper) protectionMap(name, resourceARN string) string {
	return fmt.Sprintf(`protection: %s
source: aws_shield_protection
protected_resource_arn: %s
target: edge-waf-ddos-controls
controls:
  - per_ip_rate_limit
  - concurrent_connection_limit
  - generated_edge_patch
`, name, resourceARN)
}

func (m *ShieldMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_SHIELD_PROTECTION=%s
TARGET_EDGE_PROTECTION=edge-waf-ddos-controls
EDGE_PROTECTION_UPSTREAM=http://edge-protection:8080
GENERATED_PATCH=config/shield/generated-edge.patch
`, name)
}

func (m *ShieldMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/edge/routes.env
+++ b/edge/routes.env
@@
-AWS_SHIELD_PROTECTION=%s
+EDGE_PROTECTION_UPSTREAM=http://edge-protection:8080
+EDGE_DDOS_CONTROLS=edge-waf-ddos-controls
`, name)
}

func (m *ShieldMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
PROTECTION_NAME=%q
OUTPUT_DIR="./shield-export"
mkdir -p "$OUTPUT_DIR"
aws shield list-protections --region "$AWS_REGION" --output json > "$OUTPUT_DIR/protections.json"
protection_id=$(jq -r --arg name "$PROTECTION_NAME" '.Protections[] | select(.Name == $name) | .Id' "$OUTPUT_DIR/protections.json")
test -n "$protection_id"
aws shield describe-protection --protection-id "$protection_id" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/protection.json"
`, region, name)
}

func (m *ShieldMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/shield/rate-limits.conf\ntest -s config/shield/protection-map.yaml\necho \"Edge WAF and DDoS controls ready for %s\"\n", name)
}

func (m *ShieldMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s shield-export/protection.json\ngrep -q %q config/shield/protection-map.yaml\necho \"AWS Shield protection %s mapped to edge controls\"\n", name, name)
}

func (m *ShieldMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ncurl -fsS http://localhost:8080/healthz >/tmp/homeport-shield-health.txt\ngrep -q %q config/shield/protection-map.yaml\ntest -s config/shield/app-change.env\ntest -s config/shield/generated-edge.patch\n", name)
}

func (m *ShieldMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-shield-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/shield export_shield_protection.sh provision_edge_protection.sh migrate_shield_rules.sh validate_edge_protection.sh cutover_shield_routes.sh
echo "$archive"
`, name)
}

func (m *ShieldMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/shield/app-change.env
test "$SOURCE_SHIELD_PROTECTION" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route edge traffic through $EDGE_PROTECTION_UPSTREAM"
`, name)
}

func shieldRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "shield",
		"source":              "aws_shield_protection",
		"protection":          name,
		"HOMEPORT_TARGET":     "edge-waf-ddos-controls",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		shieldStep("export-shield-protection", "Export Shield protection", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_shield_protection.sh"}, "Shield protected resource is exported", metadata),
		shieldStep("provision-edge-protection", "Provision edge protection", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_edge_protection.sh"}, "edge WAF and DDoS controls are present", metadata),
		shieldStep("migrate-shield-rules", "Migrate Shield controls", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_shield_rules.sh"}, "Shield protection is mapped to generated edge controls", metadata),
		shieldStep("validate-edge-protection", "Validate edge protection", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_edge_protection.sh"}, "edge protection health and generated patch validate", metadata),
		shieldStep("backup-shield-config", "Backup Shield config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_shield_config.sh"}, "Shield migration artifacts are archived", metadata),
		shieldStep("cutover-shield-routes", "Cut over Shield routes", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_shield_routes.sh"}, "edge routes use generated Shield controls", metadata),
		shieldStep("rollback-shield-protection", "Keep AWS Shield protection authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Shield remains authoritative until edge protection validation passes", metadata),
	}
}

func shieldStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
