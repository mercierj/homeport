// Package compute provides mappers for AWS compute services.
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

type ECRMapper struct {
	*mapper.BaseMapper
}

func NewECRMapper() *ECRMapper {
	return &ECRMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeECRRepository, nil)}
}

func (m *ECRMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	repository := res.GetConfigString("name")
	if repository == "" {
		repository = res.Name
	}
	repositoryURL := res.GetConfigString("repository_url")
	if repositoryURL == "" {
		repositoryURL = repository
	}

	result := mapper.NewMappingResult("oci-registry")
	svc := result.DockerService
	svc.Image = "registry:2"
	svc.Ports = []string{"5000:5000"}
	svc.Volumes = []string{"./data/registry:/var/lib/registry", "./config/ecr/registry.yml:/etc/docker/registry/config.yml:ro"}
	svc.Environment = map[string]string{
		"REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY": "/var/lib/registry",
		"REGISTRY_HTTP_ADDR":                        "0.0.0.0:5000",
	}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "-q", "--spider", "http://localhost:5000/v2/"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Labels = map[string]string{
		"homeport.source":     "aws_ecr_repository",
		"homeport.repository": repository,
		"homeport.target":     "oci-distribution",
	}

	result.AddConfig("config/ecr/registry.yml", []byte(ecrRegistryConfig()))
	result.AddConfig("config/ecr/app-change.env", []byte(m.appChangeConfig(repository, repositoryURL)))
	result.AddScript("sync_ecr_repository.sh", []byte(m.syncScript(repository, repositoryURL)))
	result.AddScript("backup_ecr_registry.sh", []byte(m.backupScript(repository)))
	result.AddScript("validate_ecr_registry.sh", []byte(m.validateScript(repository)))
	for _, step := range ecrRunbook(repository) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func ecrRegistryConfig() string {
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

func (m *ECRMapper) appChangeConfig(repository, repositoryURL string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_REPOSITORY=%s
SOURCE_REPOSITORY_URL=%s
TARGET_REGISTRY=registry:5000
TARGET_REPOSITORY=registry:5000/%s
`, repository, repositoryURL, repository)
}

func (m *ECRMapper) syncScript(repository, repositoryURL string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
repo="${SOURCE_REPOSITORY_URL:-%s}"
target="${TARGET_REPOSITORY:-registry:5000/%s}"
tags="${IMAGE_TAGS:-$(aws ecr describe-images --repository-name %s --query 'imageDetails[].imageTags[]' --output text)}"
for tag in $tags; do
  docker pull "$repo:$tag"
  docker tag "$repo:$tag" "$target:$tag"
  docker push "$target:$tag"
done
`, repositoryURL, repository, repository)
}

func (m *ECRMapper) backupScript(repository string) string {
	safeName := strings.NewReplacer("/", "-", " ", "-").Replace(repository)
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-ecr-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/ecr data/registry
echo "$archive"
`, safeName)
}

func (m *ECRMapper) validateScript(repository string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/ecr/app-change.env
test -s config/ecr/registry.yml
curl -fsS http://localhost:5000/v2/ >/dev/null
echo "OCI registry target for %s validated"
`, repository)
}

func ecrRunbook(repository string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "container-registry", "source": "aws_ecr_repository", "repository": repository}
	return []domainrunbook.Step{
		ecrStep("discover-ecr-images", "Discover ECR images", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("aws ecr describe-images --repository-name %q", repository)}, "source ECR image tags and digests are enumerated", metadata),
		ecrStep("provision-oci-registry", "Provision OCI registry", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/ecr/registry.yml"}, "self-hosted OCI registry config is rendered", metadata),
		ecrStep("sync-ecr-images", "Sync ECR images", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "sync_ecr_repository.sh"}, "ECR images are mirrored to the target registry", metadata),
		ecrStep("validate-oci-registry", "Validate OCI registry", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_ecr_registry.sh"}, "target registry exposes the Docker Registry API", metadata),
		ecrStep("backup-ecr-registry", "Backup OCI registry", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_ecr_registry.sh"}, "registry config and image data are archived", metadata),
		ecrStep("cutover-ecr-image-refs", "Cut over image references", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/ecr/app-change.env"}, "generated patch points workloads at the target registry", metadata),
		ecrStep("rollback-ecr-source-images", "Keep ECR as rollback source", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS ECR remains authoritative until image pull validation passes", metadata),
	}
}

func ecrStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}
