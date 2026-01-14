// Package compute provides mappers for Azure compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// VMMapper converts Azure Virtual Machines to Docker containers.
type VMMapper struct {
	*mapper.BaseMapper
}

// NewVMMapper creates a new Azure VM to Docker mapper.
func NewVMMapper() *VMMapper {
	return &VMMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureVM, nil),
	}
}

// NewWindowsVMMapper creates a new Azure Windows VM to Docker mapper.
func NewWindowsVMMapper() *VMMapper {
	return &VMMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureVMWindows, nil),
	}
}

// Map converts an Azure VM to a Docker service.
func (m *VMMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	vmName := res.GetConfigString("name")
	if vmName == "" {
		vmName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(vmName))
	svc := result.DockerService

	// Determine if Windows or Linux
	isWindows := res.Type == resource.TypeAzureVMWindows || m.isWindowsVM(res)

	// Get base image
	baseImage := m.determineBaseImage(res, isWindows)
	svc.Image = baseImage

	// Get VM size for resource limits
	vmSize := res.GetConfigString("size")
	if vmSize != "" {
		m.applyVMSize(svc, vmSize)
	}

	// Extract custom data (startup script)
	if customData := res.GetConfigString("custom_data"); customData != "" {
		result.AddScript("custom-data.sh", []byte(customData))
		result.AddManualStep("Review custom data script and incorporate into Dockerfile")
	}

	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":  "azurerm_linux_virtual_machine",
		"homeport.vm_name": vmName,
		"homeport.vm_size": vmSize,
	}

	if isWindows {
		svc.Labels["homeport.source"] = "azurerm_windows_virtual_machine"
		result.AddWarning("Windows VM detected. Docker may require Windows containers or WSL2.")
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "echo 'healthy' || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}

	// Handle data disks
	if dataDisks := res.Config["data_disk"]; dataDisks != nil {
		m.handleDataDisks(dataDisks, svc, result)
	}

	// Handle network interfaces
	if networkIDs := res.Config["network_interface_ids"]; networkIDs != nil {
		result.AddWarning("Network interfaces configured. Map ports as needed.")
	}

	// Handle identity
	if identity := res.Config["identity"]; identity != nil {
		result.AddWarning("Managed identity is configured. Configure equivalent service credentials.")
	}

	// Generate Dockerfile
	dockerfile := m.generateDockerfile(baseImage, vmName, isWindows)
	result.AddConfig(fmt.Sprintf("Dockerfile.%s", vmName), []byte(dockerfile))

	result.AddManualStep("Review Dockerfile and customize for your application")
	result.AddManualStep("Configure required ports and environment variables")

	return result, nil
}

// isWindowsVM checks if the VM is Windows based on image reference.
func (m *VMMapper) isWindowsVM(res *resource.AWSResource) bool {
	if sourceImageRef := res.Config["source_image_reference"]; sourceImageRef != nil {
		if refMap, ok := sourceImageRef.(map[string]interface{}); ok {
			publisher, _ := refMap["publisher"].(string)
			offer, _ := refMap["offer"].(string)
			if strings.Contains(strings.ToLower(publisher), "microsoft") ||
				strings.Contains(strings.ToLower(offer), "windows") {
				return true
			}
		}
	}
	return false
}

// determineBaseImage determines Docker base image from Azure image reference.
func (m *VMMapper) determineBaseImage(res *resource.AWSResource, isWindows bool) string {
	if isWindows {
		return "mcr.microsoft.com/windows/servercore:ltsc2022"
	}

	if sourceImageRef := res.Config["source_image_reference"]; sourceImageRef != nil {
		if refMap, ok := sourceImageRef.(map[string]interface{}); ok {
			publisher, _ := refMap["publisher"].(string)
			offer, _ := refMap["offer"].(string)
			sku, _ := refMap["sku"].(string)

			return m.azureImageToDocker(publisher, offer, sku)
		}
	}

	return "ubuntu:22.04"
}

// azureImageToDocker maps Azure images to Docker images.
func (m *VMMapper) azureImageToDocker(publisher, offer, sku string) string {
	publisher = strings.ToLower(publisher)
	offer = strings.ToLower(offer)
	sku = strings.ToLower(sku)

	switch {
	case strings.Contains(publisher, "canonical"):
		if strings.Contains(sku, "22") || strings.Contains(sku, "jammy") {
			return "ubuntu:22.04"
		}
		if strings.Contains(sku, "20") || strings.Contains(sku, "focal") {
			return "ubuntu:20.04"
		}
		return "ubuntu:latest"
	case strings.Contains(publisher, "redhat"):
		if strings.Contains(sku, "9") {
			return "redhat/ubi9:latest"
		}
		return "redhat/ubi8:latest"
	case strings.Contains(publisher, "openlogic") || strings.Contains(offer, "centos"):
		return "centos:7"
	case strings.Contains(offer, "debian"):
		if strings.Contains(sku, "12") {
			return "debian:bookworm"
		}
		return "debian:bullseye"
	case strings.Contains(offer, "alpine"):
		return "alpine:latest"
	default:
		return "ubuntu:22.04"
	}
}

// applyVMSize sets resource limits based on Azure VM size.
func (m *VMMapper) applyVMSize(svc *mapper.DockerService, vmSize string) {
	svc.Deploy = &mapper.DeployConfig{
		Resources: &mapper.Resources{
			Limits: &mapper.ResourceLimits{},
		},
	}

	vmSize = strings.ToLower(vmSize)

	switch {
	case strings.Contains(vmSize, "b1s"):
		svc.Deploy.Resources.Limits.CPUs = "1"
		svc.Deploy.Resources.Limits.Memory = "1G"
	case strings.Contains(vmSize, "b2s"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "4G"
	case strings.Contains(vmSize, "d2s") || strings.Contains(vmSize, "ds2"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "8G"
	case strings.Contains(vmSize, "d4s") || strings.Contains(vmSize, "ds4"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "16G"
	case strings.Contains(vmSize, "d8s") || strings.Contains(vmSize, "ds8"):
		svc.Deploy.Resources.Limits.CPUs = "8"
		svc.Deploy.Resources.Limits.Memory = "32G"
	case strings.Contains(vmSize, "e2s"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "16G"
	case strings.Contains(vmSize, "e4s"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "32G"
	case strings.Contains(vmSize, "f2s"):
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "4G"
	case strings.Contains(vmSize, "f4s"):
		svc.Deploy.Resources.Limits.CPUs = "4"
		svc.Deploy.Resources.Limits.Memory = "8G"
	default:
		svc.Deploy.Resources.Limits.CPUs = "2"
		svc.Deploy.Resources.Limits.Memory = "4G"
	}
}

// handleDataDisks processes data disk attachments.
func (m *VMMapper) handleDataDisks(dataDisks interface{}, svc *mapper.DockerService, result *mapper.MappingResult) {
	if diskSlice, ok := dataDisks.([]interface{}); ok {
		for i, disk := range diskSlice {
			if diskMap, ok := disk.(map[string]interface{}); ok {
				lun := i
				if lunVal, ok := diskMap["lun"].(float64); ok {
					lun = int(lunVal)
				}
				name, _ := diskMap["name"].(string)
				if name == "" {
					name = fmt.Sprintf("datadisk-%d", lun)
				}
				svc.Volumes = append(svc.Volumes, fmt.Sprintf("./data/%s:/mnt/disk%d", name, lun))
			}
		}
		result.AddWarning("Data disks detected. Docker volumes have been configured.")
	}
}

// generateDockerfile generates a Dockerfile for the Azure VM.
func (m *VMMapper) generateDockerfile(baseImage, vmName string, isWindows bool) string {
	if isWindows {
		return fmt.Sprintf(`# Windows container for Azure VM: %s
FROM %s

# Install PowerShell and common tools
RUN powershell -Command \
    Set-ExecutionPolicy Bypass -Scope Process -Force

WORKDIR /app

CMD ["powershell"]
`, vmName, baseImage)
	}

	return fmt.Sprintf(`FROM %s

# Generated Dockerfile for Azure VM: %s

RUN apt-get update && apt-get install -y \
    curl \
    wget \
    vim \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

CMD ["/bin/bash"]
`, baseImage, vmName)
}

func (m *VMMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}
	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "vm"
	}
	return validName
}
