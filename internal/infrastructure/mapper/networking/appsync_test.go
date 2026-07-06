package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestAppSyncMapperConformanceManagedAToZ(t *testing.T) {
	result, err := NewAppSyncMapper().Map(context.Background(), managedAppSyncFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated GraphQL migration", result.ManualSteps)
	}
	if result.DockerService.Image != "hasura/graphql-engine:v2.44.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Hasura: %#v", result.DockerService)
	}
	for _, file := range []string{"config/hasura/metadata/databases/databases.yaml", "config/hasura/metadata/actions.graphql"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing %s", file)
		}
	}
	actions := string(result.Configs["config/hasura/metadata/actions.graphql"])
	for _, want := range []string{"type Query", "getUser", "User"} {
		if !strings.Contains(actions, want) {
			t.Fatalf("actions config missing %q:\n%s", want, actions)
		}
	}
	if _, ok := result.Scripts["backup_appsync_config.sh"]; !ok {
		t.Fatal("missing backup script")
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-graphql-schema":              domainrunbook.StepTypeCommand,
		"validate-graphql-endpoint":          domainrunbook.StepTypeCommand,
		"backup-appsync-config":              domainrunbook.StepTypeCommand,
		"cutover-graphql-endpoint-to-hasura": domainrunbook.StepTypeDNSCheck,
		"rollback-appsync-source-authority":  domainrunbook.StepTypeRollback,
	} {
		if !hasAppSyncRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedAppSyncFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "shop-api",
		Type: resource.TypeAppSyncGraphQLAPI,
		Name: "shop-api",
		Config: map[string]interface{}{
			"name":   "shop-api",
			"schema": "type Query { getUser(id: ID!): User } type User { id: ID!, name: String! }",
		},
	}
}

func hasAppSyncRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
