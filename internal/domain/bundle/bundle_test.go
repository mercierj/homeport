package bundle

import (
	"testing"
	"time"
)

func TestNewManifest(t *testing.T) {
	manifest := NewManifest()

	if manifest == nil {
		t.Fatal("Expected manifest to be created")
	}

	if manifest.Format != "hprt" {
		t.Errorf("Expected format hprt, got %s", manifest.Format)
	}

	if manifest.Created.IsZero() {
		t.Error("Expected created timestamp to be set")
	}
}

func TestManifestValidation(t *testing.T) {
	tests := []struct {
		name      string
		manifest  *Manifest
		wantError bool
	}{
		{
			name: "valid manifest",
			manifest: &Manifest{
				Version:         "1.0.0",
				Format:          "hprt",
				Created:         time.Now(),
				HomeportVersion: "1.0.0",
				Source: &SourceInfo{
					Provider:      "aws",
					ResourceCount: 5,
					AnalyzedAt:    time.Now(),
				},
				Target: &TargetInfo{
					Type:       "docker-compose",
					StackCount: 1,
				},
			},
			wantError: false,
		},
		{
			name: "missing version",
			manifest: &Manifest{
				Format:          "hprt",
				Created:         time.Now(),
				HomeportVersion: "1.0.0",
			},
			wantError: true,
		},
		{
			name: "missing source",
			manifest: &Manifest{
				Version:         "1.0.0",
				Format:          "hprt",
				Created:         time.Now(),
				HomeportVersion: "1.0.0",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.manifest.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSourceInfo(t *testing.T) {
	source := &SourceInfo{
		Provider:      "aws",
		Region:        "us-east-1",
		AccountID:     "123456789012",
		ResourceCount: 10,
		AnalyzedAt:    time.Now(),
	}

	if source.Provider != "aws" {
		t.Errorf("Expected provider aws, got %s", source.Provider)
	}

	if source.Region != "us-east-1" {
		t.Errorf("Expected region us-east-1, got %s", source.Region)
	}

	if source.ResourceCount != 10 {
		t.Errorf("Expected resource count 10, got %d", source.ResourceCount)
	}
}

func TestTargetInfo(t *testing.T) {
	target := &TargetInfo{
		Type:          "docker-compose",
		Consolidation: true,
		StackCount:    3,
	}

	if target.Type != "docker-compose" {
		t.Errorf("Expected type docker-compose, got %s", target.Type)
	}

	if !target.Consolidation {
		t.Error("Expected consolidation to be true")
	}

	if target.StackCount != 3 {
		t.Errorf("Expected stack count 3, got %d", target.StackCount)
	}
}

func TestDataSyncInfo(t *testing.T) {
	dataSync := &DataSyncInfo{
		TotalEstimatedSize: "10GB",
		Databases:          []string{"postgres", "redis"},
		Storage:            []string{"s3-bucket"},
		EstimatedDuration:  "2h",
	}

	if dataSync.TotalEstimatedSize != "10GB" {
		t.Errorf("Expected size 10GB, got %s", dataSync.TotalEstimatedSize)
	}

	if len(dataSync.Databases) != 2 {
		t.Errorf("Expected 2 databases, got %d", len(dataSync.Databases))
	}

	if len(dataSync.Storage) != 1 {
		t.Errorf("Expected 1 storage, got %d", len(dataSync.Storage))
	}
}

func TestRollbackInfo(t *testing.T) {
	rollback := &RollbackInfo{
		Supported:        true,
		SnapshotRequired: true,
	}

	if !rollback.Supported {
		t.Error("Expected rollback to be supported")
	}

	if !rollback.SnapshotRequired {
		t.Error("Expected snapshot to be required")
	}
}

func TestStackInfo(t *testing.T) {
	stack := &StackInfo{
		Name:                  "web-stack",
		Services:              []string{"nginx", "app", "postgres"},
		ResourcesConsolidated: 5,
		DataSyncRequired:      true,
		EstimatedSyncSize:     "1GB",
	}

	if stack.Name != "web-stack" {
		t.Errorf("Expected name web-stack, got %s", stack.Name)
	}

	if len(stack.Services) != 3 {
		t.Errorf("Expected 3 services, got %d", len(stack.Services))
	}

	if stack.ResourcesConsolidated != 5 {
		t.Errorf("Expected 5 resources consolidated, got %d", stack.ResourcesConsolidated)
	}
}
