package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestDataplexConformanceManagedAToZ(t *testing.T) {
	result, err := NewDataplexLakeMapper().Map(context.Background(), managedDataplexFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Dataplex migration", result.ManualSteps)
	}
	if result.DockerService.Image != "apache/atlas:2.3.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Atlas target: %#v", result.DockerService)
	}
	for _, file := range []string{"docker-compose.atlas.yml", "config/dataplex/atlas-types.json", "config/dataplex/app-change.env", "config/dataplex/metadata-export.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/dataplex/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_DATAPLEX_ASSET=orders-lake", "TARGET_ATLAS_URL=http://atlas:21000"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_dataplex_metadata.sh", "provision_atlas_dataplex.sh", "migrate_dataplex_metadata.sh", "validate_dataplex_atlas.sh", "backup_dataplex_config.sh", "cutover_dataplex_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-dataplex-metadata":  domainrunbook.StepTypeCommand,
		"provision-atlas-dataplex":  domainrunbook.StepTypeCommand,
		"migrate-dataplex-metadata": domainrunbook.StepTypeCommand,
		"validate-dataplex-atlas":   domainrunbook.StepTypeCommand,
		"backup-dataplex-config":    domainrunbook.StepTypeCommand,
		"cutover-dataplex-clients":  domainrunbook.StepTypeAPICall,
		"rollback-dataplex-source":  domainrunbook.StepTypeRollback,
	} {
		if !hasDataplexRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewDataplexMappers(t *testing.T) {
	if NewDataplexLakeMapper().ResourceType() != resource.TypeDataplexLake {
		t.Fatalf("lake mapper type = %s, want %s", NewDataplexLakeMapper().ResourceType(), resource.TypeDataplexLake)
	}
	if NewDataplexZoneMapper().ResourceType() != resource.TypeDataplexZone {
		t.Fatalf("zone mapper type = %s, want %s", NewDataplexZoneMapper().ResourceType(), resource.TypeDataplexZone)
	}
}

func managedDataplexFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/lakes/orders-lake",
		Type: resource.TypeDataplexLake,
		Name: "orders-lake",
		Config: map[string]interface{}{
			"name":         "orders-lake",
			"display_name": "Orders lake",
			"region":       "europe-west1",
		},
	}
}

func hasDataplexRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
