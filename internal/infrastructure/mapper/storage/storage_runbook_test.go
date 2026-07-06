package storage

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestStorageRunbookFixtureCoversObjectBlockAndFile(t *testing.T) {
	fixture := []struct {
		name   string
		mapper interface {
			Map(context.Context, *resource.AWSResource) (*mapper.MappingResult, error)
		}
		resource *resource.AWSResource
		kind     string
	}{
		{
			name:   "s3",
			mapper: NewS3Mapper(),
			resource: &resource.AWSResource{
				ID:     "assets",
				Type:   resource.TypeS3Bucket,
				Name:   "assets",
				Config: map[string]interface{}{"bucket": "assets"},
			},
			kind: "object-storage",
		},
		{
			name:   "ebs",
			mapper: NewEBSMapper(),
			resource: &resource.AWSResource{
				ID:     "vol-1",
				Type:   resource.TypeEBSVolume,
				Name:   "data",
				Config: map[string]interface{}{"size": 20, "snapshot_id": "snap-1"},
			},
			kind: "block-storage",
		},
		{
			name:   "efs",
			mapper: NewEFSMapper(),
			resource: &resource.AWSResource{
				ID:     "fs-1",
				Type:   resource.TypeEFSVolume,
				Name:   "shared",
				Config: map[string]interface{}{"creation_token": "shared"},
			},
			kind: "file-storage",
		},
	}

	for _, tt := range fixture {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.mapper.Map(context.Background(), tt.resource)
			if err != nil {
				t.Fatalf("Map() error = %v", err)
			}
			if !hasRunbookKind(result, tt.kind) {
				t.Fatalf("missing %s runbook steps: %#v", tt.kind, result.RunbookSteps)
			}
		})
	}
}

func hasRunbookKind(result *mapper.MappingResult, kind string) bool {
	for _, step := range result.RunbookSteps {
		if step.Metadata["kind"] == kind {
			return true
		}
	}
	return false
}
