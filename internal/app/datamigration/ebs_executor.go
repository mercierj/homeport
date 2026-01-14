package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EBSToLocalExecutor migrates EBS volumes to local storage.
type EBSToLocalExecutor struct{}

// NewEBSToLocalExecutor creates a new EBS to local storage executor.
func NewEBSToLocalExecutor() *EBSToLocalExecutor {
	return &EBSToLocalExecutor{}
}

// Type returns the migration type.
func (e *EBSToLocalExecutor) Type() string {
	return "ebs_to_local"
}

// GetPhases returns the migration phases.
func (e *EBSToLocalExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Creating snapshot",
		"Waiting for snapshot",
		"Exporting snapshot",
		"Converting to raw format",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *EBSToLocalExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["volume_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.volume_id is required")
		}
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default us-east-1")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "EBS export requires S3 bucket for intermediate storage")
	result.Warnings = append(result.Warnings, "Large volumes may take significant time to export")

	return result, nil
}

// Execute performs the migration.
func (e *EBSToLocalExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	volumeID := config.Source["volume_id"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}
	outputDir := config.Destination["output_dir"].(string)
	s3Bucket, _ := config.Source["s3_bucket"].(string)

	awsEnv := []string{
		"AWS_ACCESS_KEY_ID=" + accessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + secretAccessKey,
		"AWS_DEFAULT_REGION=" + region,
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 10, "Checking credentials")

	describeCmd := exec.CommandContext(ctx, "aws", "ec2", "describe-volumes",
		"--volume-ids", volumeID,
		"--region", region,
		"--output", "json",
	)
	describeCmd.Env = append(os.Environ(), awsEnv...)
	volumeOutput, err := describeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to describe volume: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Creating snapshot
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Creating snapshot for volume %s", volumeID))
	EmitProgress(m, 20, "Creating snapshot")

	snapshotCmd := exec.CommandContext(ctx, "aws", "ec2", "create-snapshot",
		"--volume-id", volumeID,
		"--description", "Migration snapshot for "+volumeID,
		"--region", region,
		"--output", "json",
	)
	snapshotCmd.Env = append(os.Environ(), awsEnv...)
	snapshotOutput, err := snapshotCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	var snapshotResult struct {
		SnapshotID string `json:"SnapshotId"`
	}
	if err := json.Unmarshal(snapshotOutput, &snapshotResult); err != nil {
		return fmt.Errorf("failed to parse snapshot result: %w", err)
	}

	snapshotID := snapshotResult.SnapshotID
	EmitLog(m, "info", fmt.Sprintf("Snapshot created: %s", snapshotID))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Waiting for snapshot
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Waiting for snapshot to complete")
	EmitProgress(m, 40, "Waiting for snapshot")

	waitCmd := exec.CommandContext(ctx, "aws", "ec2", "wait", "snapshot-completed",
		"--snapshot-ids", snapshotID,
		"--region", region,
	)
	waitCmd.Env = append(os.Environ(), awsEnv...)
	if err := waitCmd.Run(); err != nil {
		return fmt.Errorf("snapshot wait failed: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Exporting snapshot
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Preparing export configuration")
	EmitProgress(m, 60, "Preparing export")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save volume info
	volumeInfoPath := filepath.Join(outputDir, "volume-info.json")
	if err := os.WriteFile(volumeInfoPath, volumeOutput, 0644); err != nil {
		return fmt.Errorf("failed to write volume info: %w", err)
	}

	// Save snapshot info
	snapshotInfoPath := filepath.Join(outputDir, "snapshot-info.json")
	if err := os.WriteFile(snapshotInfoPath, snapshotOutput, 0644); err != nil {
		return fmt.Errorf("failed to write snapshot info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Converting/Exporting
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating migration scripts")
	EmitProgress(m, 80, "Generating scripts")

	// Generate export script (actual export requires manual steps or ebs-direct-api)
	exportScript := fmt.Sprintf(`#!/bin/bash
# EBS Volume Migration Script
# Volume ID: %s
# Snapshot ID: %s

set -e

echo "EBS Volume Migration"
echo "===================="
echo ""
echo "This script helps migrate EBS volume data."
echo ""

# Option 1: Using AWS CLI (if S3 export is set up)
if [ -n "%s" ]; then
    echo "Exporting snapshot to S3..."
    aws ec2 create-store-image-task \
        --snapshot-id %s \
        --s3-export-location S3Bucket=%s,S3Prefix=ebs-export/ \
        --region %s
fi

# Option 2: Mount and copy (requires EC2 instance)
echo ""
echo "Alternative: Mount and copy method"
echo "1. Create a new EC2 instance in the same AZ"
echo "2. Attach volume %s to the instance"
echo "3. Mount the volume: sudo mount /dev/xvdf /mnt/ebs"
echo "4. Copy data: rsync -avz /mnt/ebs/ /destination/"
echo "5. Detach and terminate instance"

# Generate Docker volume creation
echo ""
echo "Creating Docker volume configuration..."
cat > docker-volume.yml << 'EOF'
version: '3.8'
volumes:
  migrated-ebs:
    driver: local
    driver_opts:
      type: none
      device: /data/migrated-ebs
      o: bind
EOF

echo "Migration preparation complete!"
`, volumeID, snapshotID, s3Bucket, snapshotID, s3Bucket, region, volumeID)

	scriptPath := filepath.Join(outputDir, "migrate-ebs.sh")
	if err := os.WriteFile(scriptPath, []byte(exportScript), 0755); err != nil {
		return fmt.Errorf("failed to write export script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 95, "Finalizing")

	// Generate README
	readme := fmt.Sprintf(`# EBS Volume Migration

## Source Volume
- Volume ID: %s
- Snapshot ID: %s
- Region: %s

## Migration Options

### Option 1: S3 Export (Recommended for large volumes)
Use the AWS EBS direct APIs or snapshot export feature.

### Option 2: Mount and Copy
1. Launch an EC2 instance in the same AZ
2. Attach the volume
3. Mount and rsync the data
4. Use the data locally

### Option 3: AMI Export
1. Create an AMI from the snapshot
2. Export the AMI to your local environment

## Files Generated
- volume-info.json: Original volume configuration
- snapshot-info.json: Snapshot details
- migrate-ebs.sh: Migration helper script
- docker-volume.yml: Docker volume configuration

## Notes
- Snapshot will remain in AWS (delete manually when done)
- Large volumes may incur data transfer costs
`, volumeID, snapshotID, region)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("EBS volume %s migration prepared at %s", volumeID, outputDir))

	return nil
}
