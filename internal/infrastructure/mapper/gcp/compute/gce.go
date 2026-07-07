// Package compute provides mappers for GCP compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/computeruntime"
)

// GCEMapper converts GCP Compute Engine instances to Docker containers.
type GCEMapper struct {
	*mapper.BaseMapper
}

// NewGCEMapper creates a new GCE to Docker mapper.
func NewGCEMapper() *GCEMapper {
	return &GCEMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeGCEInstance, nil),
	}
}

// Map converts a GCE instance to a Docker service.
func (m *GCEMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	instanceName := res.GetConfigString("name")
	if instanceName == "" {
		instanceName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(instanceName))
	svc := result.DockerService

	// Determine base image from machine image or boot disk
	baseImage := m.determineBaseImage(res)
	svc.Image = baseImage

	// Extract machine type for resource limits
	machineType := res.GetConfigString("machine_type")
	if machineType != "" {
		m.applyMachineType(svc, machineType)
	}

	// Extract startup script
	if startupScript := m.extractStartupScript(res); startupScript != "" {
		result.AddScript("startup-script.sh", []byte(startupScript))
	} else {
		result.AddScript("startup-script.sh", []byte("#!/bin/sh\nset -eu\n"))
	}

	if svc.Deploy == nil {
		svc.Deploy = &mapper.DeployConfig{}
	}
	svc.Deploy.Replicas = 2

	// Configure networking
	svc.Networks = []string{"homeport"}

	// Handle network tags (security groups equivalent)
	if tags := res.Config["tags"]; tags != nil {
		if tagSlice, ok := tags.([]interface{}); ok {
			for _, tag := range tagSlice {
				if tagStr, ok := tag.(string); ok {
					svc.Labels["gcp.network.tag."+tagStr] = "true"
				}
			}
		}
	}

	// Handle attached disks
	m.handleDisks(res, svc, result)

	// Handle service account
	if serviceAccount := res.Config["service_account"]; serviceAccount != nil {
		result.AddWarning("GCE instance uses service account. Configure appropriate credentials for Docker.")
	}

	svc.Restart = "unless-stopped"
	svc.Labels["homeport.source"] = "google_compute_instance"
	svc.Labels["homeport.instance_name"] = instanceName
	svc.Labels["homeport.machine_type"] = machineType

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "echo 'healthy' || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Generate Dockerfile
	dockerfile := m.generateDockerfile(baseImage, instanceName)
	dockerfilePath := fmt.Sprintf("Dockerfile.%s", instanceName)
	result.AddConfig(dockerfilePath, []byte(dockerfile))
	result.AddConfig("config/gce/app-change.env", []byte(m.generateAppChangeConfig(instanceName)))
	result.AddConfig("config/gce/instance-report.yaml", []byte(m.generateInstanceReport(res, instanceName, baseImage)))
	result.AddScript("deploy_gce_container.sh", []byte(m.generateDeployScript(instanceName)))
	result.AddScript("validate_gce_container.sh", []byte(m.generateValidateScript(instanceName)))
	result.AddScript("backup_gce_config.sh", []byte(m.generateBackupScript(instanceName)))
	result.AddScript("cutover_gce_container.sh", []byte(m.generateCutoverScript(instanceName)))
	svc.Build = &mapper.DockerBuild{Context: ".", Dockerfile: dockerfilePath}
	appUnit := computeruntime.FromDockerService("google_compute_instance", svc)
	result.AddAppUnit(appUnit)
	for _, step := range computeruntime.ContainerApp(appUnit, "deploy_gce_container.sh") {
		result.AddRunbookStep(step)
	}
	for _, step := range gceRunbook(instanceName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

// determineBaseImage determines the base Docker image from GCE image.
func (m *GCEMapper) determineBaseImage(res *resource.AWSResource) string {
	// Check boot disk image
	if bootDisk := res.Config["boot_disk"]; bootDisk != nil {
		if bdMap, ok := bootDisk.(map[string]interface{}); ok {
			if initParams, ok := bdMap["initialize_params"].(map[string]interface{}); ok {
				if image, ok := initParams["image"].(string); ok {
					return m.gcpImageToDocker(image)
				}
			}
		}
	}

	return "ubuntu:22.04"
}

// gcpImageToDocker maps GCP images to Docker images.
func (m *GCEMapper) gcpImageToDocker(gcpImage string) string {
	switch {
	case strings.Contains(gcpImage, "ubuntu"):
		if strings.Contains(gcpImage, "2204") || strings.Contains(gcpImage, "22.04") {
			return "ubuntu:22.04"
		}
		if strings.Contains(gcpImage, "2004") || strings.Contains(gcpImage, "20.04") {
			return "ubuntu:20.04"
		}
		return "ubuntu:latest"
	case strings.Contains(gcpImage, "debian"):
		if strings.Contains(gcpImage, "12") || strings.Contains(gcpImage, "bookworm") {
			return "debian:bookworm"
		}
		if strings.Contains(gcpImage, "11") || strings.Contains(gcpImage, "bullseye") {
			return "debian:bullseye"
		}
		return "debian:latest"
	case strings.Contains(gcpImage, "centos"):
		return "centos:7"
	case strings.Contains(gcpImage, "rocky"):
		return "rockylinux:9"
	case strings.Contains(gcpImage, "alpine"):
		return "alpine:latest"
	case strings.Contains(gcpImage, "cos") || strings.Contains(gcpImage, "container-optimized"):
		return "gcr.io/google-containers/toolbox:latest"
	default:
		return "ubuntu:22.04"
	}
}

// applyMachineType sets resource limits based on GCP machine type.
func (m *GCEMapper) applyMachineType(svc *mapper.DockerService, machineType string) {
	// Parse machine type (e.g., n2-standard-4, e2-medium)
	svc.Deploy = &mapper.DeployConfig{
		Resources: &mapper.Resources{
			Limits: &mapper.ResourceLimits{},
		},
	}

	switch {
	case strings.Contains(machineType, "micro"):
		svc.Deploy.Resources.Limits.CPUs = "0.25"
		svc.Deploy.Resources.Limits.Memory = "256M"
	case strings.Contains(machineType, "small"):
		svc.Deploy.Resources.Limits.CPUs = "0.5"
		svc.Deploy.Resources.Limits.Memory = "512M"
	case strings.Contains(machineType, "medium"):
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "2G"
	case strings.Contains(machineType, "standard-2"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "8G"
	case strings.Contains(machineType, "standard-4"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "16G"
	case strings.Contains(machineType, "standard-8"):
		svc.Deploy.Resources.Limits.CPUs = "8"
		svc.Deploy.Resources.Limits.Memory = "32G"
	case strings.Contains(machineType, "highcpu"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "4G"
	case strings.Contains(machineType, "highmem"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "16G"
	default:
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "2G"
	}
}

// extractStartupScript extracts startup script from metadata.
func (m *GCEMapper) extractStartupScript(res *resource.AWSResource) string {
	if metadata := res.Config["metadata"]; metadata != nil {
		if metaMap, ok := metadata.(map[string]interface{}); ok {
			if script, ok := metaMap["startup-script"].(string); ok {
				return script
			}
		}
	}

	if metadataStartupScript := res.Config["metadata_startup_script"]; metadataStartupScript != nil {
		if script, ok := metadataStartupScript.(string); ok {
			return script
		}
	}

	return ""
}

// handleDisks processes attached disks.
func (m *GCEMapper) handleDisks(res *resource.AWSResource, svc *mapper.DockerService, result *mapper.MappingResult) {
	if attachedDisks := res.Config["attached_disk"]; attachedDisks != nil {
		if diskSlice, ok := attachedDisks.([]interface{}); ok {
			for i, disk := range diskSlice {
				if diskMap, ok := disk.(map[string]interface{}); ok {
					deviceName, _ := diskMap["device_name"].(string)
					if deviceName == "" {
						deviceName = fmt.Sprintf("disk-%d", i)
					}
					svc.Volumes = append(svc.Volumes, fmt.Sprintf("./data/%s:/mnt/%s", deviceName, deviceName))
				}
			}
		}
		result.AddWarning("Attached disks detected. Docker volumes have been configured.")
	}
}

// generateDockerfile generates a Dockerfile for the GCE instance.
func (m *GCEMapper) generateDockerfile(baseImage, instanceName string) string {
	return fmt.Sprintf(`FROM %s

# Generated Dockerfile for GCE instance: %s

# Install basic utilities
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    vim \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy startup script if present
COPY startup-script.sh /docker-entrypoint.d/startup-script.sh
RUN chmod +x /docker-entrypoint.d/startup-script.sh

WORKDIR /app

CMD ["/bin/bash"]
`, baseImage, instanceName)
}

func (m *GCEMapper) generateAppChangeConfig(instanceName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_GCE_INSTANCE=%s\nTARGET_CONTAINER=%s\nTARGET_RUNTIME=docker-compose\n", instanceName, m.sanitizeName(instanceName))
}

func (m *GCEMapper) generateInstanceReport(res *resource.AWSResource, instanceName, baseImage string) string {
	return fmt.Sprintf(`source: google_compute_instance
instance: %s
target: docker-compose
image: %s
machine_type: %s
zone: %s
`, instanceName, baseImage, res.GetConfigString("machine_type"), res.GetConfigString("zone"))
}

func (m *GCEMapper) generateDeployScript(instanceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ndocker compose build %s\ndocker compose up -d %s\n", m.sanitizeName(instanceName), m.sanitizeName(instanceName))
}

func (m *GCEMapper) generateValidateScript(instanceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ndocker compose ps %s\ntest -s config/gce/app-change.env\necho \"GCE instance %s validated as container\"\n", m.sanitizeName(instanceName), instanceName)
}

func (m *GCEMapper) generateBackupScript(instanceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-gce-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/gce Dockerfile.%s startup-script.sh deploy_gce_container.sh validate_gce_container.sh cutover_gce_container.sh\necho \"$archive\"\n", m.sanitizeName(instanceName), instanceName)
}

func (m *GCEMapper) generateCutoverScript(instanceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/gce/app-change.env\ntest \"$SOURCE_GCE_INSTANCE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Route traffic to container $TARGET_CONTAINER\"\n", instanceName)
}

func gceRunbook(instanceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "compute-instance", "source": "google_compute_instance", "instance": instanceName}
	return []domainrunbook.Step{
		gceStep("backup-gce-config", "Backup GCE config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_gce_config.sh"}, "Compute Engine migration artifacts are archived", metadata),
		gceStep("cutover-gce-container", "Cut over GCE traffic", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_gce_container.sh"}, "traffic points at the container target", metadata),
	}
}

func gceStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
}

// sanitizeName sanitizes the name for Docker.
func (m *GCEMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}

	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "instance"
	}

	return validName
}
