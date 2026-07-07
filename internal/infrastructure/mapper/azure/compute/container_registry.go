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

type ContainerRegistryMapper struct{ *mapper.BaseMapper }

func NewContainerRegistryMapper() *ContainerRegistryMapper {
	return &ContainerRegistryMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAzureContainerRegistry, nil)}
}

func (m *ContainerRegistryMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	registry := res.GetConfigString("name")
	if registry == "" {
		registry = res.Name
	}
	loginServer := res.GetConfigString("login_server")
	if loginServer == "" {
		loginServer = registry + ".azurecr.io"
	}

	result := mapper.NewMappingResult("oci-registry")
	svc := result.DockerService
	svc.Image = "registry:2"
	svc.Ports = []string{"5000:5000"}
	svc.Volumes = []string{"./data/container-registry:/var/lib/registry", "./config/container-registry/registry.yml:/etc/docker/registry/config.yml:ro"}
	svc.Environment = map[string]string{
		"REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY": "/var/lib/registry",
		"REGISTRY_HTTP_ADDR":                        "0.0.0.0:5000",
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "wget", "-q", "--spider", "http://localhost:5000/v2/"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeAzureContainerRegistry), "homeport.registry": registry, "homeport.target": "oci-distribution"}

	result.AddConfig("config/container-registry/registry.yml", []byte(containerRegistryConfig()))
	result.AddConfig("config/container-registry/app-change.env", []byte(m.appChangeConfig(registry, loginServer)))
	result.AddScript("sync_container_registry.sh", []byte(m.syncScript(registry, loginServer)))
	result.AddScript("backup_container_registry.sh", []byte(m.backupScript(registry)))
	result.AddScript("validate_container_registry.sh", []byte(m.validateScript(registry)))
	for _, step := range containerRegistryRunbook(registry) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func containerRegistryConfig() string {
	return "version: 0.1\nlog:\n  level: info\nstorage:\n  filesystem:\n    rootdirectory: /var/lib/registry\nhttp:\n  addr: :5000\n"
}

func (m *ContainerRegistryMapper) appChangeConfig(registry, loginServer string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_AZURE_CONTAINER_REGISTRY=%s\nSOURCE_REGISTRY_URL=%s\nTARGET_REGISTRY=registry:5000\nTARGET_REPOSITORY=registry:5000/%s\n", registry, loginServer, registry)
}

func (m *ContainerRegistryMapper) syncScript(registry, loginServer string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nrepo=\"${SOURCE_REGISTRY_URL:-%s}\"\ntarget=\"${TARGET_REPOSITORY:-registry:5000/%s}\"\ntags=\"${IMAGE_TAGS:-latest}\"\nfor tag in $tags; do\n  docker pull \"$repo:$tag\"\n  docker tag \"$repo:$tag\" \"$target:$tag\"\n  docker push \"$target:$tag\"\ndone\n", loginServer, registry)
}

func (m *ContainerRegistryMapper) backupScript(registry string) string {
	safeName := strings.NewReplacer("/", "-", " ", "-").Replace(registry)
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-container-registry-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/container-registry data/container-registry\necho \"$archive\"\n", safeName)
}

func (m *ContainerRegistryMapper) validateScript(registry string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/container-registry/app-change.env\ntest -s config/container-registry/registry.yml\ncurl -fsS http://localhost:5000/v2/ >/dev/null\necho \"OCI registry target for %s validated\"\n", registry)
}

func containerRegistryRunbook(registry string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "container-registry", "source": string(resource.TypeAzureContainerRegistry), "registry": registry}
	return []domainrunbook.Step{
		containerRegistryStep("discover-container-registry-images", "Discover Container Registry images", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "-c", fmt.Sprintf("az acr repository list --name %q", registry)}, "source image tags and digests are enumerated", metadata),
		containerRegistryStep("provision-container-oci-registry", "Provision OCI registry", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/container-registry/registry.yml"}, "self-hosted OCI registry config is rendered", metadata),
		containerRegistryStep("sync-container-registry-images", "Sync Container Registry images", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "sync_container_registry.sh"}, "Container Registry images are mirrored to the target registry", metadata),
		containerRegistryStep("validate-container-oci-registry", "Validate OCI registry", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_container_registry.sh"}, "target registry exposes the Docker Registry API", metadata),
		containerRegistryStep("backup-container-registry", "Backup OCI registry", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_container_registry.sh"}, "registry config and image data are archived", metadata),
		containerRegistryStep("cutover-container-image-refs", "Cut over image references", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "-c", "test -s config/container-registry/app-change.env"}, "generated patch points workloads at the target registry", metadata),
		containerRegistryStep("rollback-container-source-images", "Keep Container Registry as rollback source", "Rollback", domainrunbook.StepTypeRollback, nil, "Azure Container Registry remains authoritative until image pull validation passes", metadata),
	}
}

func containerRegistryStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
		command = nil
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
