// Package storage provides mappers for Azure storage services.
package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// FilesMapper converts Azure Files shares to Samba/CIFS file shares.
type FilesMapper struct {
	*mapper.BaseMapper
}

// NewFilesMapper creates a new Azure Files to Samba mapper.
func NewFilesMapper() *FilesMapper {
	return &FilesMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureFiles, nil),
	}
}

// Map converts an Azure Files share to a Samba service.
func (m *FilesMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	shareName := res.GetConfigString("name")
	if shareName == "" {
		shareName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(shareName) + "-samba")
	svc := result.DockerService

	// Configure Samba service
	svc.Image = "dperson/samba:latest"
	svc.Environment = map[string]string{
		"USERID":  "1000",
		"GROUPID": "1000",
		"SHARE":   fmt.Sprintf("%s;/share/%s;yes;no;no;all;none", shareName, shareName),
		"USER":    "azureuser;azurepass",
	}
	svc.Ports = []string{
		"139:139", // NetBIOS
		"445:445", // SMB
	}
	svc.Volumes = []string{
		fmt.Sprintf("./data/shares/%s:/share/%s", shareName, shareName),
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "smbclient", "-L", "localhost", "-U", "%"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  3,
	}
	svc.Labels = map[string]string{
		"homeport.source":     "azurerm_storage_share",
		"homeport.share_name": shareName,
		"homeport.protocol":   "SMB/CIFS",
	}

	// Handle quota
	quotaGB := res.GetConfigInt("quota")
	if quotaGB == 0 {
		quotaGB = 5120 // Default 5TB max for Azure Files
	}
	svc.Labels["homeport.quota_gb"] = fmt.Sprintf("%d", quotaGB)
	result.AddWarning(fmt.Sprintf("Share quota is %d GB. Samba doesn't enforce quotas by default - consider using filesystem quotas if needed.", quotaGB))

	// Handle access tier
	accessTier := res.GetConfigString("access_tier")
	if accessTier != "" {
		svc.Labels["homeport.access_tier"] = accessTier
		result.AddWarning(fmt.Sprintf("Access tier '%s' is configured. Samba doesn't differentiate between Hot/Cool/Transaction Optimized tiers.", accessTier))
	}

	// Handle enabled protocol
	enabledProtocol := res.GetConfigString("enabled_protocol")
	if enabledProtocol == "" {
		enabledProtocol = "SMB"
	}
	svc.Labels["homeport.protocol_version"] = enabledProtocol

	if enabledProtocol == "NFS" {
		result.AddWarning("NFS protocol is enabled. Consider using an NFS server image instead of Samba.")
		result.AddManualStep("For NFS support, replace Samba with an NFS server: erichough/nfs-server")
		nfsConfig := m.generateNFSConfig(shareName)
		result.AddConfig(fmt.Sprintf("config/nfs/%s-nfs.txt", shareName), []byte(nfsConfig))
	}

	// Handle metadata
	if metadata := res.Config["metadata"]; metadata != nil {
		if metaMap, ok := metadata.(map[string]interface{}); ok && len(metaMap) > 0 {
			result.AddWarning("Share metadata is configured. Document metadata separately as Samba doesn't support share-level metadata.")
			metadataDoc := m.generateMetadataDoc(shareName, metaMap)
			result.AddConfig(fmt.Sprintf("config/shares/%s-metadata.txt", shareName), []byte(metadataDoc))
		}
	}

	// Handle ACL
	if acl := res.Config["acl"]; acl != nil {
		result.AddWarning("Azure Files ACL is configured. Map to Samba permissions using the configuration script.")
	}

	// Handle storage account name (parent resource)
	if storageAccountName := res.GetConfigString("storage_account_name"); storageAccountName != "" {
		svc.Labels["homeport.storage_account"] = storageAccountName
		result.AddWarning(fmt.Sprintf("Share belongs to storage account '%s'. Samba shares are independent.", storageAccountName))
	}

	// Generate mount instructions
	mountInstructions := m.generateMountInstructions(shareName)
	result.AddConfig(fmt.Sprintf("config/shares/%s-mount.txt", shareName), []byte(mountInstructions))

	// Generate Samba configuration
	sambaConfig := m.generateSambaConfig(shareName, quotaGB)
	result.AddScript(fmt.Sprintf("setup_%s.sh", shareName), []byte(sambaConfig))

	result.AddManualStep(fmt.Sprintf("Mount share on Linux: mount -t cifs //localhost/%s /mnt/share -o username=azureuser,password=azurepass", shareName))
	result.AddManualStep(fmt.Sprintf("Mount share on Windows: net use Z: \\\\localhost\\%s /user:azureuser azurepass", shareName))
	result.AddManualStep(fmt.Sprintf("Mount share on macOS: mount -t smbfs //azureuser:azurepass@localhost/%s /Volumes/share", shareName))
	result.AddManualStep("Default credentials: azureuser / azurepass (change in production)")

	return result, nil
}

// generateMountInstructions creates mount instructions for various operating systems.
func (m *FilesMapper) generateMountInstructions(shareName string) string {
	return fmt.Sprintf(`Azure Files Share: %s
SMB/CIFS Mount Instructions

Linux:
------
# Install CIFS utilities (if not already installed)
sudo apt-get install cifs-utils  # Ubuntu/Debian
sudo yum install cifs-utils      # CentOS/RHEL

# Create mount point
sudo mkdir -p /mnt/%s

# Mount the share
sudo mount -t cifs //localhost/%s /mnt/%s -o username=azureuser,password=azurepass,vers=3.0

# Add to /etc/fstab for persistent mount
//localhost/%s /mnt/%s cifs username=azureuser,password=azurepass,vers=3.0,_netdev 0 0

Windows:
--------
# Map network drive
net use Z: \\localhost\%s /user:azureuser azurepass

# Or use File Explorer:
# 1. Right-click "This PC"
# 2. Select "Map network drive"
# 3. Enter path: \\localhost\%s
# 4. Enter credentials: azureuser / azurepass

macOS:
------
# Create mount point
mkdir -p /Volumes/%s

# Mount the share
mount -t smbfs //azureuser:azurepass@localhost/%s /Volumes/%s

# Or use Finder:
# 1. Go > Connect to Server (Cmd+K)
# 2. Enter: smb://localhost/%s
# 3. Enter credentials: azureuser / azurepass

Docker Container:
-----------------
# Mount share in container
docker run -it --rm \
  -v %s-data:/data \
  alpine sh -c "apk add --no-cache cifs-utils && mount -t cifs //host.docker.internal/%s /data -o username=azureuser,password=azurepass"

Security Notes:
---------------
- Default credentials (azureuser/azurepass) are for testing only
- Change credentials for production use
- Consider using more secure authentication methods
- Use encrypted connections (SMB3) when possible
`, shareName, shareName, shareName, shareName, shareName, shareName, shareName, shareName, shareName, shareName, shareName, shareName, shareName, shareName)
}

// generateSambaConfig creates a Samba setup script.
func (m *FilesMapper) generateSambaConfig(shareName string, quotaGB int) string {
	return fmt.Sprintf(`#!/bin/bash
# Samba Setup Script for Azure Files Share: %s

set -e

echo "Setting up Samba share: %s"
echo ""
echo "Share configuration:"
echo "  Name: %s"
echo "  Path: /share/%s"
echo "  Quota: %d GB (advisory)"
echo "  Protocol: SMB/CIFS"
echo ""
echo "Access credentials:"
echo "  Username: azureuser"
echo "  Password: azurepass"
echo ""
echo "IMPORTANT: Change the default password in production!"
echo ""
echo "To change the password, update the SHARE environment variable in docker-compose.yml"
echo ""
echo "Mount instructions available in: config/shares/%s-mount.txt"
echo ""
echo "Testing access:"
echo "  smbclient //localhost/%s -U azureuser%%azurepass -c 'ls'"
`, shareName, shareName, shareName, shareName, quotaGB, shareName, shareName)
}

// generateMetadataDoc creates documentation for share metadata.
func (m *FilesMapper) generateMetadataDoc(shareName string, metadata map[string]interface{}) string {
	doc := fmt.Sprintf(`Azure Files Share Metadata: %s

The following metadata was configured on the Azure Files share.
Samba doesn't support share-level metadata, so this is documented here for reference.

Metadata:
`, shareName)

	for key, value := range metadata {
		doc += fmt.Sprintf("  %s: %v\n", key, value)
	}

	doc += `
Note: If you need to store metadata, consider:
1. Using extended file attributes (xattr) on individual files
2. Storing metadata in a separate database or configuration file
3. Using file/directory naming conventions
`

	return doc
}

// generateNFSConfig creates NFS configuration documentation.
func (m *FilesMapper) generateNFSConfig(shareName string) string {
	return fmt.Sprintf(`NFS Configuration for Azure Files Share: %s

To use NFS instead of SMB/CIFS, update your docker-compose.yml:

services:
  %s-nfs:
    image: erichough/nfs-server:latest
    cap_add:
      - SYS_ADMIN
      - SYS_MODULE
    environment:
      NFS_EXPORT_0: '/share/%s                  *(rw,sync,no_subtree_check,fsid=0)'
    volumes:
      - ./data/shares/%s:/share/%s
    ports:
      - "2049:2049"
    networks:
      - homeport
    restart: unless-stopped

Mount on Linux:
sudo mount -t nfs -o vers=4 localhost:/ /mnt/%s

Mount on macOS:
sudo mount -t nfs -o vers=4,resvport localhost:/ /Volumes/%s

Note:
- NFS requires privileged capabilities (SYS_ADMIN, SYS_MODULE)
- NFS v4 is recommended for better security and performance
- Adjust export options based on your security requirements
`, shareName, shareName, shareName, shareName, shareName, shareName, shareName)
}

// sanitizeName ensures the name is valid for Docker service names.
func (m *FilesMapper) sanitizeName(name string) string {
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
		validName = "share"
	}
	return validName
}
