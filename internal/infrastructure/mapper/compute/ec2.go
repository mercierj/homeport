// Package compute provides mappers for AWS compute services.
package compute

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// EC2Mapper converts AWS EC2 instances to Docker containers.
type EC2Mapper struct {
	*mapper.BaseMapper
}

// NewEC2Mapper creates a new EC2 to Docker container mapper.
func NewEC2Mapper() *EC2Mapper {
	return &EC2Mapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeEC2Instance, nil),
	}
}

// Map converts an EC2 instance to a Docker container service.
func (m *EC2Mapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	instanceName := res.Name
	result := mapper.NewMappingResult(m.sanitizeServiceName(instanceName))

	instanceType := res.GetConfigString("instance_type")
	ami := res.GetConfigString("ami")

	// Determine base image from AMI or tags
	baseImage := m.determineBaseImage(res, ami)

	// Extract user data script
	userData := m.extractUserData(res)

	// Configure the Docker service
	result.DockerService.Image = baseImage
	result.DockerService.Environment = map[string]string{
		"INSTANCE_NAME": instanceName,
		"INSTANCE_TYPE": instanceType,
	}
	result.DockerService.Volumes = []string{
		fmt.Sprintf("./data/%s:/data", instanceName),
	}
	result.DockerService.Restart = "unless-stopped"
	result.DockerService.Labels = map[string]string{
		"homeport.source":        "aws_instance",
		"homeport.instance_name": instanceName,
		"homeport.instance_type": instanceType,
	}

	// Map instance type to resources
	m.applyInstanceTypeResources(result.DockerService, instanceType)

	// Handle security groups as network ports
	if securityGroups := m.extractSecurityGroupRules(res); len(securityGroups) > 0 {
		for _, rule := range securityGroups {
			if rule.FromPort == rule.ToPort {
				result.DockerService.Ports = append(result.DockerService.Ports, fmt.Sprintf("%d:%d", rule.FromPort, rule.FromPort))
			} else {
				// Docker doesn't support port ranges in simple format
				result.AddWarning(fmt.Sprintf("Port range %d-%d detected. Map individual ports as needed.", rule.FromPort, rule.ToPort))
			}
		}
	}

	// Handle EBS volumes
	if volumes := m.extractEBSVolumes(res); len(volumes) > 0 {
		for _, vol := range volumes {
			result.DockerService.Volumes = append(result.DockerService.Volumes,
				fmt.Sprintf("./data/%s/volumes/%s:%s", instanceName, vol.Device, vol.MountPoint))
		}
		result.AddManualStep("Create volume directories and copy EBS data if migrating from AWS")
	}

	// Handle IAM instance profile
	if iamRole := res.GetConfigString("iam_instance_profile"); iamRole != "" {
		result.AddWarning(fmt.Sprintf("IAM instance profile '%s' detected. Configure equivalent permissions manually.", iamRole))
		result.AddManualStep("Review IAM policies and configure equivalent access controls")
	}

	// Create Dockerfile if user data contains setup scripts
	if userData != "" {
		dockerfilePath := fmt.Sprintf("docker/%s/Dockerfile", instanceName)
		dockerfileContent := m.generateDockerfileContent(baseImage, userData, instanceName)
		result.AddConfig(dockerfilePath, []byte(dockerfileContent))

		// Update service to use custom image
		result.DockerService.Image = fmt.Sprintf("%s:latest", instanceName)
		result.AddManualStep(fmt.Sprintf("Build custom image: docker build -f %s -t %s:latest .", dockerfilePath, instanceName))
	}

	// Create setup script
	setupScriptName := fmt.Sprintf("setup_%s.sh", instanceName)
	setupScriptContent := m.generateSetupScriptContent(res, instanceName, userData)
	result.AddScript(setupScriptName, []byte(setupScriptContent))

	// Add tags as environment variables
	for key, value := range res.Tags {
		envKey := "TAG_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		result.DockerService.Environment[envKey] = value
	}

	// Add warnings
	if res.GetConfigBool("monitoring") {
		result.AddWarning("Detailed monitoring is enabled. Set up custom monitoring for your self-hosted environment.")
	}

	if res.GetConfigString("key_name") != "" {
		result.AddWarning("SSH key pair detected. Configure SSH access to your Docker host as needed.")
	}

	result.AddManualStep("Review and customize the Dockerfile and setup scripts")
	result.AddManualStep("Ensure all required application dependencies are installed")
	result.AddManualStep("Test the container thoroughly before production use")

	return result, nil
}

// determineBaseImage determines the appropriate Docker base image from AMI.
func (m *EC2Mapper) determineBaseImage(res *resource.AWSResource, ami string) string {
	// Check tags for hints about the OS
	if osTag, ok := res.Tags["OS"]; ok {
		osTag = strings.ToLower(osTag)
		if strings.Contains(osTag, "ubuntu") {
			return "ubuntu:22.04"
		} else if strings.Contains(osTag, "debian") {
			return "debian:12"
		} else if strings.Contains(osTag, "alpine") {
			return "alpine:3.18"
		} else if strings.Contains(osTag, "amazon") || strings.Contains(osTag, "amzn") {
			return "amazonlinux:2023"
		}
	}

	// Try to infer from AMI name or description
	amiName := strings.ToLower(res.GetConfigString("ami_name"))
	if strings.Contains(amiName, "ubuntu") {
		return "ubuntu:22.04"
	} else if strings.Contains(amiName, "debian") {
		return "debian:12"
	} else if strings.Contains(amiName, "alpine") {
		return "alpine:3.18"
	} else if strings.Contains(amiName, "amazon") || strings.Contains(amiName, "amzn") {
		return "amazonlinux:2023"
	} else if strings.Contains(amiName, "rhel") || strings.Contains(amiName, "redhat") {
		return "redhat/ubi9:latest"
	}

	// Default to Ubuntu
	return "ubuntu:22.04"
}

// extractUserData extracts and decodes user data script.
func (m *EC2Mapper) extractUserData(res *resource.AWSResource) string {
	userData := res.GetConfigString("user_data")
	if userData == "" {
		return ""
	}

	// User data might be base64 encoded
	if decoded, err := base64.StdEncoding.DecodeString(userData); err == nil {
		// Check if decoded data looks like text
		if m.isTextData(decoded) {
			return string(decoded)
		}
	}

	return userData
}

// isTextData checks if data appears to be text.
func (m *EC2Mapper) isTextData(data []byte) bool {
	// Simple heuristic: check if most bytes are printable
	printable := 0
	for _, b := range data {
		if (b >= 32 && b <= 126) || b == '\n' || b == '\r' || b == '\t' {
			printable++
		}
	}
	return float64(printable)/float64(len(data)) > 0.95
}

// SecurityGroupRule represents a security group rule.
type SecurityGroupRule struct {
	FromPort int
	ToPort   int
	Protocol string
	CIDR     string
}

// extractSecurityGroupRules extracts security group rules from the instance.
func (m *EC2Mapper) extractSecurityGroupRules(res *resource.AWSResource) []SecurityGroupRule {
	var rules []SecurityGroupRule

	// Note: In a real implementation, you would need to look up the security group
	// resources separately and parse their ingress rules.
	// This is a simplified version that looks for vpc_security_group_ids.

	if sgIDs, ok := res.Config["vpc_security_group_ids"]; ok && sgIDs != nil {
		// Security group details would need to be fetched separately
		// For now, we'll add a placeholder
		_ = sgIDs
	}

	return rules
}

// EBSVolume represents an EBS volume attachment.
type EBSVolume struct {
	Device     string
	MountPoint string
	Size       int
}

// extractEBSVolumes extracts EBS volume information.
func (m *EC2Mapper) extractEBSVolumes(res *resource.AWSResource) []EBSVolume {
	var volumes []EBSVolume

	// Check for EBS block devices
	if blockDevicesRaw, ok := res.Config["ebs_block_device"]; ok {
		blockDevices, _ := blockDevicesRaw.([]interface{})
		if blockDevices != nil {
			for _, bd := range blockDevices {
				if bdMap, ok := bd.(map[string]interface{}); ok {
					device := ""
					if dn, ok := bdMap["device_name"].(string); ok {
						device = dn
					}

					size := 0
					if vs, ok := bdMap["volume_size"].(float64); ok {
						size = int(vs)
					} else if vs, ok := bdMap["volume_size"].(int); ok {
						size = vs
					}

					volumes = append(volumes, EBSVolume{
						Device:     device,
						MountPoint: m.guessMountPoint(device),
						Size:       size,
					})
				}
			}
		}
	}

	return volumes
}

// guessMountPoint guesses the mount point from device name.
func (m *EC2Mapper) guessMountPoint(device string) string {
	// Simple mapping of common device names to mount points
	switch device {
	case "/dev/sdf", "/dev/xvdf":
		return "/mnt/data"
	case "/dev/sdg", "/dev/xvdg":
		return "/mnt/data2"
	default:
		return "/mnt/volume"
	}
}

// generateDockerfileContent creates Dockerfile content based on user data.
func (m *EC2Mapper) generateDockerfileContent(baseImage, userData, instanceName string) string {
	installCmd := m.getPackageInstallCommand(baseImage)

	return fmt.Sprintf(`FROM %s

# Generated Dockerfile for EC2 instance: %s

# Install basic utilities
%s

# Copy user data script
COPY scripts/user-data.sh /docker-entrypoint-initdb.d/

# Make script executable
RUN chmod +x /docker-entrypoint-initdb.d/user-data.sh

# Run user data script during build (optional)
# Uncomment if you want to run setup during build instead of runtime
# RUN /docker-entrypoint-initdb.d/user-data.sh

# Set working directory
WORKDIR /app

# Default command
CMD ["/bin/bash"]
`, baseImage, instanceName, installCmd)
}

// getPackageInstallCommand returns the correct package installation command for the base image.
func (m *EC2Mapper) getPackageInstallCommand(baseImage string) string {
	switch {
	case strings.Contains(baseImage, "alpine"):
		return `RUN apk add --no-cache \
    curl \
    wget \
    vim \
    ca-certificates`
	case strings.Contains(baseImage, "amazonlinux"):
		return `RUN yum install -y \
    curl \
    wget \
    vim \
    ca-certificates \
    && yum clean all`
	case strings.Contains(baseImage, "ubi") || strings.Contains(baseImage, "redhat") || strings.Contains(baseImage, "centos") || strings.Contains(baseImage, "fedora"):
		return `RUN dnf install -y \
    curl \
    wget \
    vim \
    ca-certificates \
    && dnf clean all`
	default:
		// Debian/Ubuntu based
		return `RUN apt-get update && apt-get install -y \
    curl \
    wget \
    vim \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*`
	}
}

// generateSetupScriptContent creates setup script content.
func (m *EC2Mapper) generateSetupScriptContent(res *resource.AWSResource, instanceName, userData string) string {
	return fmt.Sprintf(`#!/bin/bash
# Setup script for EC2 instance: %s

set -e

echo "Setting up instance: %s"

# Create data directories
mkdir -p ./data/%s

# Create user data script if exists
if [ -n "$USER_DATA" ]; then
  mkdir -p ./scripts
  cat > ./scripts/user-data.sh <<'USERDATA'
%s
USERDATA
  chmod +x ./scripts/user-data.sh
  echo "User data script created at: ./scripts/user-data.sh"
fi

echo "Setup complete!"
`, instanceName, instanceName, instanceName, userData)
}

// applyInstanceTypeResources maps EC2 instance type to Docker resource limits.
func (m *EC2Mapper) applyInstanceTypeResources(service *mapper.DockerService, instanceType string) {
	// Parse instance type (e.g., t3.micro, m5.large)
	cpus := "1.0"
	memory := "1G"

	if strings.Contains(instanceType, "nano") {
		cpus = "0.25"
		memory = "512M"
	} else if strings.Contains(instanceType, "micro") {
		cpus = "0.5"
		memory = "1G"
	} else if strings.Contains(instanceType, "small") {
		cpus = "1.0"
		memory = "2G"
	} else if strings.Contains(instanceType, "medium") {
		cpus = "2.0"
		memory = "4G"
	} else if strings.Contains(instanceType, "large") {
		cpus = "2.0"
		memory = "8G"
	} else if strings.Contains(instanceType, "xlarge") {
		if strings.Contains(instanceType, "2xlarge") {
			cpus = "8.0"
			memory = "32G"
		} else if strings.Contains(instanceType, "4xlarge") {
			cpus = "16.0"
			memory = "64G"
		} else if strings.Contains(instanceType, "8xlarge") {
			cpus = "32.0"
			memory = "128G"
		} else {
			cpus = "4.0"
			memory = "16G"
		}
	}

	// Set up deployment configuration with resource limits
	service.Deploy = &mapper.DeployConfig{
		Resources: &mapper.Resources{
			Limits: &mapper.ResourceLimits{
				CPUs:   cpus,
				Memory: memory,
			},
			Reservations: &mapper.ResourceLimits{
				CPUs:   cpus,
				Memory: memory,
			},
		},
	}
}

// sanitizeServiceName sanitizes instance name for use as Docker service name.
func (m *EC2Mapper) sanitizeServiceName(name string) string {
	// Replace invalid characters with hyphens
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	// Remove invalid characters
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}

	// Ensure it doesn't start with a hyphen or number
	validName = strings.TrimLeft(validName, "-0123456789")

	if validName == "" {
		validName = "instance"
	}

	return validName
}
