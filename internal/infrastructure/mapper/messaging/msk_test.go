package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestMSKConformanceManagedAToZ(t *testing.T) {
	if !resource.TypeMSKCluster.IsValid() {
		t.Fatalf("%s should be a valid AWS resource type", resource.TypeMSKCluster)
	}
	result, err := NewMSKMapper().Map(context.Background(), managedMSKFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated MSK migration", result.ManualSteps)
	}
	if result.DockerService.Image != "redpandadata/redpanda:v23.3.5" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 3 {
		t.Fatalf("service does not provision HA Redpanda target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/redpanda/msk-topics.yaml", "config/msk/cluster-map.yaml", "config/msk/consumer-groups.yaml", "config/msk/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/msk/app-change.env"])
	for _, want := range []string{"SOURCE_CLUSTER=orders-msk", "KAFKA_BOOTSTRAP_SERVERS=redpanda:9092", "APP_CHANGE_MODE=no_change"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_msk_cluster.sh", "provision_redpanda_msk.sh", "migrate_msk_topics.sh", "validate_msk_replay.sh", "backup_msk_config.sh", "cutover_msk_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-msk-cluster":     domainrunbook.StepTypeCommand,
		"provision-redpanda-msk": domainrunbook.StepTypeCommand,
		"migrate-msk-topics":     domainrunbook.StepTypeCommand,
		"validate-msk-replay":    domainrunbook.StepTypeCommand,
		"backup-msk-config":      domainrunbook.StepTypeCommand,
		"cutover-msk-clients":    domainrunbook.StepTypeAPICall,
		"rollback-msk-source":    domainrunbook.StepTypeRollback,
	} {
		if !hasMSKRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedMSKFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "arn:aws:kafka:eu-west-1:123456789012:cluster/orders-msk/abc",
		Type:   resource.TypeMSKCluster,
		Name:   "orders-msk",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"cluster_name":           "orders-msk",
			"number_of_broker_nodes": float64(3),
			"kafka_version":          "3.6.0",
			"retention_hours":        float64(168),
		},
	}
}

func hasMSKRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
