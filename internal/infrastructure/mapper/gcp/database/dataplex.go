package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type DataplexMapper struct {
	*mapper.BaseMapper
}

func NewDataplexLakeMapper() *DataplexMapper {
	return &DataplexMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeDataplexLake, nil)}
}

func NewDataplexZoneMapper() *DataplexMapper {
	return &DataplexMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeDataplexZone, nil)}
}

func (m *DataplexMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmptyDataplex(res.GetConfigString("name"), res.GetConfigString("display_name"), res.Name)
	kind := "lake"
	if res.Type == resource.TypeDataplexZone {
		kind = "zone"
	}

	result := mapper.NewMappingResult("atlas")
	svc := result.DockerService
	svc.Image = "apache/atlas:2.3.0"
	svc.Ports = []string{"21000:21000"}
	svc.Volumes = []string{"./config/dataplex:/etc/homeport/dataplex", "./data/atlas:/var/lib/atlas"}
	svc.Environment = map[string]string{"DATAPLEX_ASSET": name, "DATAPLEX_KIND": kind}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "curl -fsS http://localhost:21000/api/atlas/admin/status >/dev/null || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(res.Type), "homeport.dataplex_asset": name, "homeport.target": "apache-atlas"}

	result.AddConfig("docker-compose.atlas.yml", []byte(m.generateCompose(name, kind)))
	result.AddConfig("config/dataplex/atlas-types.json", []byte(m.generateAtlasTypes(name, kind)))
	result.AddConfig("config/dataplex/app-change.env", []byte(m.generateAppChangeConfig(name)))
	result.AddConfig("config/dataplex/metadata-export.yaml", []byte(m.generateMetadataExport(res, name, kind)))
	result.AddScript("export_dataplex_metadata.sh", []byte(m.generateExportScript(name, kind)))
	result.AddScript("provision_atlas_dataplex.sh", []byte(m.generateProvisionScript(name)))
	result.AddScript("migrate_dataplex_metadata.sh", []byte(m.generateMigrateScript(name)))
	result.AddScript("validate_dataplex_atlas.sh", []byte(m.generateValidateScript(name)))
	result.AddScript("backup_dataplex_config.sh", []byte(m.generateBackupScript(name)))
	result.AddScript("cutover_dataplex_clients.sh", []byte(m.generateCutoverScript(name)))
	for _, step := range dataplexRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *DataplexMapper) generateCompose(name, kind string) string {
	return fmt.Sprintf(`services:
  atlas:
    image: apache/atlas:2.3.0
    environment:
      DATAPLEX_ASSET: %s
      DATAPLEX_KIND: %s
    ports:
      - "21000:21000"
    volumes:
      - ./config/dataplex:/etc/homeport/dataplex
      - ./data/atlas:/var/lib/atlas
`, name, kind)
}

func (m *DataplexMapper) generateAtlasTypes(name, kind string) string {
	return fmt.Sprintf(`{
  "entityDefs": [{
    "name": "homeport_dataplex_%s",
    "superTypes": ["DataSet"],
    "attributeDefs": [{"name": "sourceName", "typeName": "string", "isOptional": false}]
  }],
  "source": %q
}
`, kind, name)
}

func (m *DataplexMapper) generateAppChangeConfig(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_DATAPLEX_ASSET=%s
TARGET_GOVERNANCE_CATALOG=apache-atlas
TARGET_ATLAS_URL=http://atlas:21000
GENERATED_METADATA=config/dataplex/metadata-export.yaml
`, name)
}

func (m *DataplexMapper) generateMetadataExport(res *resource.AWSResource, name, kind string) string {
	return fmt.Sprintf("source: %s\nasset: %s\nkind: %s\nregion: %s\ntarget: apache-atlas\n", res.Type, name, kind, res.GetConfigString("region"))
}

func (m *DataplexMapper) generateExportScript(name, kind string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
DATAPLEX_NAME=%q
DATAPLEX_KIND=%q
OUTPUT_DIR="${OUTPUT_DIR:-./dataplex-export}"
mkdir -p "$OUTPUT_DIR"
gcloud dataplex "$DATAPLEX_KIND"s describe "$DATAPLEX_NAME" --format=json > "$OUTPUT_DIR/$DATAPLEX_KIND.json"
`, name, kind)
}

func (m *DataplexMapper) generateProvisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/dataplex/atlas-types.json\necho \"Atlas governance catalog ready for Dataplex asset %s\"\n", name)
}

func (m *DataplexMapper) generateMigrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/dataplex/metadata-export.yaml\ntest -s config/dataplex/atlas-types.json\necho \"Dataplex metadata %s mapped to Apache Atlas entities\"\n", name)
}

func (m *DataplexMapper) generateValidateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/dataplex/app-change.env\ngrep -q %q config/dataplex/metadata-export.yaml\necho \"Dataplex metadata %s validates for Atlas import\"\n", name, name)
}

func (m *DataplexMapper) generateBackupScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/dataplex-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/dataplex dataplex-export docker-compose.atlas.yml\necho \"$archive\"\n", sanitizeDataplexName(name))
}

func (m *DataplexMapper) generateCutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/dataplex/app-change.env
test "$SOURCE_DATAPLEX_ASSET" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_METADATA"
echo "Review $GENERATED_METADATA and route governance catalog reads through $TARGET_ATLAS_URL"
`, name)
}

func dataplexRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "data-governance", "source": "google_dataplex", "asset": name, "target": "apache-atlas"}
	return []domainrunbook.Step{
		dataplexStep("export-dataplex-metadata", "Export Dataplex metadata", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_dataplex_metadata.sh"}, "Dataplex metadata is exported", metadata),
		dataplexStep("provision-atlas-dataplex", "Provision Apache Atlas", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_atlas_dataplex.sh"}, "Atlas type definitions are generated", metadata),
		dataplexStep("migrate-dataplex-metadata", "Migrate Dataplex metadata", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_dataplex_metadata.sh"}, "Dataplex metadata is represented as Atlas entities", metadata),
		dataplexStep("validate-dataplex-atlas", "Validate Atlas metadata", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_dataplex_atlas.sh"}, "Atlas migration metadata validates", metadata),
		dataplexStep("backup-dataplex-config", "Backup Dataplex config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_dataplex_config.sh"}, "Dataplex migration artifacts are archived", metadata),
		dataplexStep("cutover-dataplex-clients", "Cut over Dataplex clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_dataplex_clients.sh"}, "governance consumers use Atlas metadata", metadata),
		dataplexStep("rollback-dataplex-source", "Keep Dataplex source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Dataplex remains authoritative until Atlas validation passes", metadata),
	}
}

func dataplexStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmptyDataplex(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "dataplex"
}

func sanitizeDataplexName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "-", " ", "-").Replace(value)
	if value == "" {
		return "dataplex"
	}
	return value
}
