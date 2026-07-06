package database

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestDatabaseRunbookFixtureCoversPostgresRedisAndDynamoDB(t *testing.T) {
	fixture := []struct {
		name   string
		mapper interface {
			Map(context.Context, *resource.AWSResource) (*mapper.MappingResult, error)
		}
		resource *resource.AWSResource
		kind     string
	}{
		{
			name:   "postgres",
			mapper: NewRDSMapper(),
			resource: &resource.AWSResource{
				ID:   "db-1",
				Type: resource.TypeRDSInstance,
				Name: "appdb",
				Config: map[string]interface{}{
					"engine":  "postgres",
					"db_name": "app",
				},
			},
			kind: "sql",
		},
		{
			name:   "redis",
			mapper: NewElastiCacheMapper(),
			resource: &resource.AWSResource{
				ID:   "cache-1",
				Type: resource.TypeElastiCache,
				Name: "cache",
				Config: map[string]interface{}{
					"engine":     "redis",
					"cluster_id": "cache",
				},
			},
			kind: "redis",
		},
		{
			name:   "dynamodb",
			mapper: NewDynamoDBMapper(),
			resource: &resource.AWSResource{
				ID:   "table-1",
				Type: resource.TypeDynamoDBTable,
				Name: "events",
				Config: map[string]interface{}{
					"name":     "events",
					"hash_key": "id",
				},
			},
			kind: "dynamodb",
		},
	}

	for _, tt := range fixture {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(context.Background(), tt.resource)
			if err != nil {
				t.Fatalf("Map() error = %v", err)
			}
			if !hasDatabaseRunbookKind(result, tt.kind) {
				t.Fatalf("missing %s runbook steps: %#v", tt.kind, result.RunbookSteps)
			}
		})
	}
}

func hasDatabaseRunbookKind(result *mapper.MappingResult, kind string) bool {
	for _, step := range result.RunbookSteps {
		if step.Metadata["kind"] == kind {
			return true
		}
	}
	return false
}
