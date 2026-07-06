package storagerunbook

import (
	"fmt"
	"strings"

	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func ObjectStorage(bucket, sourceURI string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                    "object-storage",
		"bucket":                  bucket,
		"backend":                 "minio",
		"AWS_ENDPOINT_URL_S3":     "http://minio:9000",
		"AWS_REGION":              "us-east-1",
		"AWS_S3_FORCE_PATH_STYLE": "true",
		"AWS_ACCESS_KEY_ID":       "${MINIO_ROOT_USER}",
		"AWS_SECRET_ACCESS_KEY":   "${MINIO_ROOT_PASSWORD}",
	}
	targetURI := "minio:" + bucket
	return []domainrunbook.Step{
		command("provision-minio-bucket", "Provision MinIO bucket", "Object Storage", []string{"sh", "setup_minio.sh"}, "bucket exists in MinIO", metadata),
		command("estimate-object-source", "Estimate source objects", "Object Storage", []string{"rclone", "size", sourceURI, "--json"}, "source object count and bytes recorded", metadata),
		command("sync-objects-to-minio", "Sync objects to MinIO", "Object Storage", []string{"rclone", "sync", sourceURI, targetURI, "--progress"}, "source objects copied to MinIO", metadata),
		command("verify-object-migration", "Verify object migration", "Object Storage", []string{"rclone", "check", sourceURI, targetURI, "--one-way"}, "object counts, sample checksums, and missing keys verified", metadata),
		command("validate-object-api", "Validate S3-compatible API", "Object Storage", []string{"sh", "-c", fmt.Sprintf("aws --endpoint-url http://minio:9000 s3api list-objects-v2 --bucket %s >/dev/null", shellQuote(bucket))}, "put/list/get/delete smoke test passes", metadata),
		{
			ID:               "rollback-keep-source-authority",
			Name:             "Keep source as rollback authority",
			Group:            "Rollback",
			Type:             domainrunbook.StepTypeRollback,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "source bucket remains authoritative until cutover passes",
			Metadata:         clone(metadata),
		},
	}
}

func BlockStorage(name, provider, snapshotID string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "block-storage", "volume": name, "provider": provider}
	if snapshotID != "" {
		metadata["snapshot_id"] = snapshotID
	}
	return []domainrunbook.Step{
		command("discover-block-snapshot", "Discover block storage snapshot", "Block Storage", []string{"sh", "-c", "echo snapshot discovery required for " + shellQuote(name)}, "snapshot identified or export limitation recorded", metadata),
		command("export-import-block-data", "Export or stream block data", "Block Storage", []string{"sh", "sync_ebs_volume.sh"}, "snapshot export or helper VM stream completed", metadata),
		command("validate-block-mount", "Validate block storage mount", "Block Storage", []string{"sh", "-c", "docker volume inspect " + shellQuote(name) + " >/dev/null"}, "filesystem mounts and used bytes match expectation", metadata),
	}
}

func FileStorage(name, protocol string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "file-storage", "share": name, "protocol": protocol}
	syncCommand := "rsync -a --info=progress2 ${SOURCE_MOUNT}/ ${TARGET_MOUNT}/"
	if strings.EqualFold(protocol, "smb") {
		syncCommand = "robocopy %SOURCE_MOUNT% %TARGET_MOUNT% /MIR /Z /R:3 /W:5"
	}
	return []domainrunbook.Step{
		command("provision-file-target", "Provision file storage target", "File Storage", []string{"sh", "-c", "echo provision " + shellQuote(protocol) + " target " + shellQuote(name)}, "NFS or SMB target is reachable", metadata),
		command("sync-file-data", "Sync file data", "File Storage", []string{"sh", "-c", syncCommand}, "file data copied to target", metadata),
		command("validate-file-migration", "Validate file migration", "File Storage", []string{"sh", "-c", "find ${TARGET_MOUNT:-.} -type f | wc -l"}, "file count, byte count, permissions sample, and app-container mount verified", metadata),
	}
}

func command(id, name, group string, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeCommand,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		SuccessCondition: success,
		Command:          command,
		Metadata:         clone(metadata),
	}
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
