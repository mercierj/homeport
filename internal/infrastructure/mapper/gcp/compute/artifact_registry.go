package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type ArtifactRegistryMapper struct {
	*mapper.BaseMapper
}

func NewArtifactRegistryMapper() *ArtifactRegistryMapper {
	return &ArtifactRegistryMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeArtifactRegistryRepository, nil)}
}

func (m *ArtifactRegistryMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	repository := res.GetConfigString("name")
	if repository == "" {
		repository = res.Name
	}
	location := res.GetConfigString("location")
	if location == "" {
		location = res.Region
	}
	if location == "" {
		location = "us-central1"
	}
	source := fmt.Sprintf("%s-docker.pkg.dev/%s", location, repository)

	result := mapper.NewMappingResult("oci-registry")
	svc := result.DockerService
	svc.Image = "registry:2"
	svc.Ports = []string{"5000:5000"}
	svc.Volumes = []string{"./data/artifact-registry:/var/lib/registry", "./config/artifact-registry/registry.yml:/etc/docker/registry/config.yml:ro"}
	svc.Environment = map[string]string{
		"REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY": "/var/lib/registry",
		"REGISTRY_HTTP_ADDR":                        "0.0.0.0:5000",
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "wget", "-q", "--spider", "http://localhost:5000/v2/"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{
		"homeport.source":     "google_artifact_registry_repository",
		"homeport.repository": repository,
		"homeport.target":     "oci-distribution",
	}

	result.AddConfig("config/artifact-registry/registry.yml", []byte(artifactRegistryConfig()))
	result.AddConfig("config/artifact-registry/app-change.env", []byte(m.appChangeConfig(repository, source)))
	result.AddScript("sync_artifact_registry.sh", []byte(m.syncScript(repository, source)))
	result.AddScript("backup_artifact_registry.sh", []byte(m.backupScript(repository)))
	result.AddScript("validate_artifact_registry.sh", []byte(m.validateScript(repository)))
	for _, step := range artifactRegistryRunbook(repository) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func artifactRegistryConfig() string {
	return `version: 0.1
log:
  level: info
storage:
  filesystem:
    rootdirectory: /var/lib/registry
http:
  addr: :5000
`
}

func (m *ArtifactRegistryMapper) appChangeConfig(repository, source string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_ARTIFACT_REGISTRY=%s
SOURCE_REPOSITORY_URL=%s
TARGET_REGISTRY=registry:5000
TARGET_REPOSITORY=registry:5000/%s
`, repository, source, repository)
}

func (m *ArtifactRegistryMapper) syncScript(repository, source string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
repo="${SOURCE_REPOSITORY_URL:-%s}"
target="${TARGET_REPOSITORY:-registry:5000/%s}"
tags="${IMAGE_TAGS:-latest}"
for tag in $tags; do
  docker pull "$repo/$tag"
  docker tag "$repo/$tag" "$target:$tag"
  docker push "$target:$tag"
done
`, source, repository)
}

func (m *ArtifactRegistryMapper) backupScript(repository string) string {
	safeName := strings.NewReplacer("/", "-", " ", "-").Replace(repository)
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-artifact-registry-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/artifact-registry data/artifact-registry
echo "$archive"
`, safeName)
}

func (m *ArtifactRegistryMapper) validateScript(repository string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/artifact-registry/app-change.env
test -s config/artifact-registry/registry.yml
curl -fsS http://localhost:5000/v2/ >/dev/null
echo "OCI registry target for %s validated"
`, repository)
}

func artifactRegistryRunbook(repository string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "container-registry", "source": "google_artifact_registry_repository", "repository": repository}
	return []domainrunbook.Step{
		artifactRegistryStep("discover-artifact-registry-images", "Discover Artifact Registry images", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("gcloud artifacts docker images list %q", repository)}, "source image tags and digests are enumerated", metadata),
		artifactRegistryStep("provision-artifact-oci-registry", "Provision OCI registry", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/artifact-registry/registry.yml"}, "self-hosted OCI registry config is rendered", metadata),
		artifactRegistryStep("sync-artifact-registry-images", "Sync Artifact Registry images", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "sync_artifact_registry.sh"}, "Artifact Registry images are mirrored to the target registry", metadata),
		artifactRegistryStep("validate-artifact-oci-registry", "Validate OCI registry", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_artifact_registry.sh"}, "target registry exposes the Docker Registry API", metadata),
		artifactRegistryStep("backup-artifact-registry", "Backup OCI registry", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_artifact_registry.sh"}, "registry config and image data are archived", metadata),
		artifactRegistryStep("cutover-artifact-image-refs", "Cut over image references", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/artifact-registry/app-change.env"}, "generated patch points workloads at the target registry", metadata),
		artifactRegistryStep("rollback-artifact-source-images", "Keep Artifact Registry as rollback source", "Rollback", domainrunbook.StepTypeRollback, nil, "GCP Artifact Registry remains authoritative until image pull validation passes", metadata),
	}
}

func artifactRegistryStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
