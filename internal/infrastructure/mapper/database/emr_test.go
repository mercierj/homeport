package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestEMRConformanceManagedAToZ(t *testing.T) {
	if !resource.TypeEMRCluster.IsValid() {
		t.Fatalf("%s should be a valid AWS resource type", resource.TypeEMRCluster)
	}
	result, err := NewEMRMapper().Map(context.Background(), managedEMRFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated EMR Spark migration", result.ManualSteps)
	}
	if result.DockerService.Image != "bitnami/spark:3.5" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Spark target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/emr/spark-defaults.conf", "config/emr/app-change.env", "config/emr/steps.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/emr/app-change.env"])
	for _, want := range []string{"SOURCE_CLUSTER=orders-emr", "TARGET_RUNTIME=spark", "APP_CHANGE_MODE=generated_patch"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_emr_steps.sh", "submit_spark_jobs.sh", "backup_emr_config.sh", "validate_emr_runtime.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-emr-steps":       domainrunbook.StepTypeCommand,
		"provision-spark-target": domainrunbook.StepTypeCommand,
		"submit-spark-jobs":      domainrunbook.StepTypeCommand,
		"validate-spark-runtime": domainrunbook.StepTypeCommand,
		"backup-emr-config":      domainrunbook.StepTypeCommand,
		"cutover-emr-jobs":       domainrunbook.StepTypeAPICall,
		"rollback-emr-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasEMRRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedEMRFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "j-123",
		Type: resource.TypeEMRCluster,
		Name: "orders-emr",
		Config: map[string]interface{}{
			"name":          "orders-emr",
			"release_label": "emr-6.15.0",
			"applications":  []interface{}{"Spark", "Hive"},
			"step": []interface{}{
				map[string]interface{}{"name": "daily-orders", "jar": "command-runner.jar", "args": []interface{}{"spark-submit", "s3://jobs/orders.py"}},
			},
		},
	}
}

func hasEMRRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
