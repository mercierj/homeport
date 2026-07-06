package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestRedshiftConformanceManagedAToZ(t *testing.T) {
	result, err := NewRedshiftMapper().Map(context.Background(), &resource.AWSResource{ID: "rs-1", Type: resource.TypeRedshiftCluster, Name: "orders-warehouse", Region: "eu-west-1", Config: map[string]interface{}{"cluster_identifier": "orders-warehouse", "database_name": "analytics"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Redshift migration", result.ManualSteps)
	}
	if result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA ClickHouse target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/redshift/schema-map.yaml", "config/redshift/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	if !strings.Contains(string(result.Configs["config/redshift/app-change.env"]), "APP_CHANGE_MODE=generated_patch") {
		t.Fatalf("missing generated_patch app-change env")
	}
	for _, file := range []string{"export_redshift_cluster.sh", "migrate_redshift_unload.sh", "validate_redshift_analytics.sh", "backup_redshift_target.sh", "cutover_redshift_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{"export-redshift-cluster": domainrunbook.StepTypeCommand, "provision-clickhouse": domainrunbook.StepTypeCommand, "migrate-redshift-unload": domainrunbook.StepTypeCommand, "validate-redshift-analytics": domainrunbook.StepTypeCommand, "backup-redshift-target": domainrunbook.StepTypeCommand, "cutover-redshift-clients": domainrunbook.StepTypeAPICall, "rollback-redshift-source": domainrunbook.StepTypeRollback} {
		if !hasRedshiftRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func hasRedshiftRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
