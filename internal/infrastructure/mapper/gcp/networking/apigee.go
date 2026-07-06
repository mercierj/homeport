package networking

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type ApigeeMapper struct {
	*mapper.BaseMapper
}

func NewApigeeMapper() *ApigeeMapper {
	return &ApigeeMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeApigeeOrganization, nil)}
}

func (m *ApigeeMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}

	result := mapper.NewMappingResult("kong")
	svc := result.DockerService
	svc.Image = "kong:3.6"
	svc.Ports = []string{"8000:8000", "8001:8001", "8443:8443", "8444:8444"}
	svc.Volumes = []string{"./config/apigee:/kong/declarative"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Environment["KONG_DATABASE"] = "off"
	svc.Environment["KONG_DECLARATIVE_CONFIG"] = "/kong/declarative/kong.yaml"
	svc.Labels = map[string]string{
		"homeport.source":     "google_apigee_organization",
		"homeport.apigee_org": name,
		"homeport.target":     "kong",
	}

	result.AddConfig("config/apigee/kong.yaml", []byte(m.kongConfig(name)))
	result.AddConfig("config/apigee/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/apigee/generated-client.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_apigee_config.sh", []byte(m.exportScript(name)))
	result.AddScript("provision_kong_gateway.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_apigee_proxies.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_kong_gateway.sh", []byte(m.validateScript()))
	result.AddScript("backup_apigee_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_apigee_routes.sh", []byte(m.cutoverScript(name)))
	for _, step := range apigeeRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *ApigeeMapper) kongConfig(name string) string {
	return fmt.Sprintf(`_format_version: "3.0"
services:
  - name: %s-apigee-migration
    url: http://upstream.local
    routes:
      - name: %s-route
        paths: ["/"]
`, name, name)
}

func (m *ApigeeMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_APIGEE_ORG=%s
TARGET_API_GATEWAY=kong
KONG_PROXY_URL=http://kong:8000
GENERATED_PATCH=config/apigee/generated-client.patch
`, name)
}

func (m *ApigeeMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/app/api-gateway.env
+++ b/app/api-gateway.env
@@
-APIGEE_ORG=%s
+API_GATEWAY=kong
+API_GATEWAY_URL=http://kong:8000
`, name)
}

func (m *ApigeeMapper) exportScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
APIGEE_ORG=%q
OUTPUT_DIR="./apigee-export"
mkdir -p "$OUTPUT_DIR"
gcloud apigee apis list --organization="$APIGEE_ORG" --format=json > "$OUTPUT_DIR/apis.json"
gcloud apigee environments list --organization="$APIGEE_ORG" --format=json > "$OUTPUT_DIR/environments.json"
`, name)
}

func (m *ApigeeMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/apigee/kong.yaml\necho \"Kong gateway ready for Apigee org %s\"\n", name)
}

func (m *ApigeeMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s apigee-export/apis.json\ngrep -q %q config/apigee/kong.yaml\necho \"Apigee proxies mapped to Kong declarative config\"\n", name)
}

func (m *ApigeeMapper) validateScript() string {
	return "#!/bin/sh\nset -eu\ntest -s config/apigee/app-change.env\ntest -s config/apigee/generated-client.patch\nkong config parse config/apigee/kong.yaml >/tmp/homeport-kong-parse.txt\n"
}

func (m *ApigeeMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-apigee-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/apigee apigee-export export_apigee_config.sh provision_kong_gateway.sh migrate_apigee_proxies.sh validate_kong_gateway.sh cutover_apigee_routes.sh
echo "$archive"
`, name)
}

func (m *ApigeeMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/apigee/app-change.env
test "$SOURCE_APIGEE_ORG" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route API clients to $KONG_PROXY_URL"
`, name)
}

func apigeeRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "api-gateway",
		"source":              "google_apigee_organization",
		"organization":        name,
		"HOMEPORT_TARGET":     "kong",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		apigeeStep("export-apigee-config", "Export Apigee config", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_apigee_config.sh"}, "Apigee APIs and environments are exported", metadata),
		apigeeStep("provision-kong-gateway", "Provision Kong gateway", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_kong_gateway.sh"}, "Kong declarative config is rendered", metadata),
		apigeeStep("migrate-apigee-proxies", "Migrate Apigee proxies", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_apigee_proxies.sh"}, "Apigee proxies map to Kong routes", metadata),
		apigeeStep("validate-kong-gateway", "Validate Kong gateway", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_kong_gateway.sh"}, "Kong config and generated patch validate", metadata),
		apigeeStep("backup-apigee-config", "Backup Apigee config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_apigee_config.sh"}, "Apigee migration artifacts are archived", metadata),
		apigeeStep("cutover-apigee-routes", "Cut over Apigee routes", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_apigee_routes.sh"}, "clients use generated Kong patch", metadata),
		apigeeStep("rollback-apigee-source", "Keep Apigee source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "GCP Apigee remains authoritative until Kong validation passes", metadata),
	}
}

func apigeeStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
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
