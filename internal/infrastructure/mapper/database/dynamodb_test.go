package database

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewDynamoDBMapper(t *testing.T) {
	m := NewDynamoDBMapper()
	if m == nil {
		t.Fatal("NewDynamoDBMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeDynamoDBTable {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeDynamoDBTable)
	}
}

func TestDynamoDBConformanceManagedAToZ(t *testing.T) {
	result, err := NewDynamoDBMapper().Map(context.Background(), managedDynamoDBFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Alternator migration", result.ManualSteps)
	}
	if result.DockerService.Image != "scylladb/scylla:5.4" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Scylla Alternator: %#v", result.DockerService)
	}
	for _, file := range []string{"config/scylladb/scylla.yaml", "config/scylladb/cdc.yaml", "config/dynamodb/app-change.env"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/dynamodb/app-change.env"])
	if !strings.Contains(appEnv, "AWS_ENDPOINT_URL_DYNAMODB=http://scylladb:8000") {
		t.Fatalf("app-change env missing Alternator endpoint:\n%s", appEnv)
	}
	for _, file := range []string{"create_table.cql", "migrate_dynamodb.sh", "backup_dynamodb_alternator.sh", "validate_dynamodb_alternator.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"provision-scylla-alternator":        domainrunbook.StepTypeCommand,
		"migrate-dynamodb-table":             domainrunbook.StepTypeCommand,
		"validate-dynamodb-sdk":              domainrunbook.StepTypeCommand,
		"backup-dynamodb-alternator":         domainrunbook.StepTypeCommand,
		"cutover-dynamodb-endpoint":          domainrunbook.StepTypeAPICall,
		"rollback-dynamodb-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasDynamoDBRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedDynamoDBFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "arn:aws:dynamodb:us-east-1:123456789012:table/orders",
		Type: resource.TypeDynamoDBTable,
		Name: "orders",
		Config: map[string]interface{}{
			"name":           "orders",
			"hash_key":       "pk",
			"range_key":      "sk",
			"billing_mode":   "PAY_PER_REQUEST",
			"stream_enabled": true,
		},
	}
}

func hasDynamoDBRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestDynamoDBMapper_ResourceType(t *testing.T) {
	m := NewDynamoDBMapper()
	got := m.ResourceType()
	want := resource.TypeDynamoDBTable

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestDynamoDBMapper_Dependencies(t *testing.T) {
	m := NewDynamoDBMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestDynamoDBMapper_Validate(t *testing.T) {
	m := NewDynamoDBMapper()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
	}{
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeS3Bucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeDynamoDBTable,
				Name: "test-table",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeDynamoDBTable,
				Name: "test-table",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBMapper_Map(t *testing.T) {
	m := NewDynamoDBMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic DynamoDB table",
			res: &resource.AWSResource{
				ID:   "arn:aws:dynamodb:us-east-1:123456789012:table/my-table",
				Type: resource.TypeDynamoDBTable,
				Name: "my-table",
				Config: map[string]interface{}{
					"name":     "my-table",
					"hash_key": "id",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
				// Should use ScyllaDB image
				if result.DockerService.Image != "scylladb/scylla:5.4" {
					t.Errorf("Expected ScyllaDB image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "DynamoDB table with PAY_PER_REQUEST billing",
			res: &resource.AWSResource{
				ID:   "arn:aws:dynamodb:us-east-1:123456789012:table/on-demand-table",
				Type: resource.TypeDynamoDBTable,
				Name: "on-demand-table",
				Config: map[string]interface{}{
					"name":         "on-demand-table",
					"hash_key":     "pk",
					"range_key":    "sk",
					"billing_mode": "PAY_PER_REQUEST",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about on-demand billing
				if len(result.Warnings) == 0 {
					t.Log("Expected warnings about PAY_PER_REQUEST billing")
				}
			},
		},
		{
			name: "DynamoDB table with streams enabled",
			res: &resource.AWSResource{
				ID:   "arn:aws:dynamodb:us-east-1:123456789012:table/stream-table",
				Type: resource.TypeDynamoDBTable,
				Name: "stream-table",
				Config: map[string]interface{}{
					"name":           "stream-table",
					"hash_key":       "id",
					"stream_enabled": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about streams
				hasStreamWarning := false
				for _, w := range result.Warnings {
					if len(w) > 0 {
						hasStreamWarning = true
						break
					}
				}
				if !hasStreamWarning {
					t.Log("Expected warnings about DynamoDB Streams")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "wrong-id",
				Type: resource.TypeS3Bucket,
				Name: "wrong",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.Map(ctx, tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}
