// Package compute provides mappers for AWS compute services.
package compute

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// EKSMapper converts AWS EKS clusters to K3s.
type EKSMapper struct {
	*mapper.BaseMapper
}

// NewEKSMapper creates a new EKS to K3s mapper.
func NewEKSMapper() *EKSMapper {
	return &EKSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeEKSCluster, nil),
	}
}

// Map converts an EKS cluster to a K3s Docker service.
func (m *EKSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	clusterName := res.GetConfigString("name")
	if clusterName == "" {
		clusterName = res.Name
	}

	result := mapper.NewMappingResult("k3s-server")
	svc := result.DockerService

	// K3s version based on EKS version
	k8sVersion := res.GetConfigString("version")
	k3sImage := m.getK3sImage(k8sVersion)

	svc.Image = k3sImage
	svc.Command = []string{
		"server",
		"--disable=traefik", // We use our own Traefik
		"--tls-san=localhost",
		"--tls-san=k3s-server",
	}
	svc.Environment = map[string]string{
		"K3S_TOKEN":                    "homeport-cluster-token",
		"K3S_KUBECONFIG_OUTPUT":        "/output/kubeconfig.yaml",
		"K3S_KUBECONFIG_MODE":          "666",
		"KUBERNETES_CLUSTER_NAME":      clusterName,
	}
	svc.Ports = []string{
		"6443:6443", // Kubernetes API
		"80:80",     // Ingress HTTP
		"443:443",   // Ingress HTTPS
	}
	svc.Volumes = []string{
		"k3s-server:/var/lib/rancher/k3s",
		"./kubeconfig:/output",
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":       "aws_eks_cluster",
		"homeport.cluster_name": clusterName,
		"homeport.k8s_version":  k8sVersion,
	}

	// K3s requires privileged mode
	svc.CapAdd = []string{"NET_ADMIN", "SYS_ADMIN"}

	// Add K3s agent service config
	agentConfig := m.generateAgentConfig(clusterName)
	result.AddConfig("config/k3s/agent-compose.yml", []byte(agentConfig))

	// Generate kubeconfig setup script
	kubeconfigScript := m.generateKubeconfigScript(clusterName)
	result.AddScript("setup_kubeconfig.sh", []byte(kubeconfigScript))

	// Generate cluster info
	clusterInfoScript := m.generateClusterInfoScript(clusterName, k8sVersion)
	result.AddScript("cluster_info.sh", []byte(clusterInfoScript))

	// Handle node groups
	if nodeGroups := res.Config["node_groups"]; nodeGroups != nil {
		result.AddWarning("EKS node groups detected. K3s agents can be added for multi-node setup.")
		result.AddManualStep("Add K3s agent containers for each node group (see config/k3s/agent-compose.yml)")
	}

	// Handle VPC configuration
	if vpcConfig := res.Config["vpc_config"]; vpcConfig != nil {
		result.AddWarning("VPC configuration detected. K3s uses Docker networking by default.")
	}

	// Handle encryption
	if encryptionConfig := res.Config["encryption_config"]; encryptionConfig != nil {
		result.AddWarning("EKS encryption is configured. Consider enabling K3s secrets encryption.")
		result.AddManualStep("Enable K3s secrets encryption: https://docs.k3s.io/security/secrets-encryption")
	}

	// Handle logging
	if logging := res.Config["enabled_cluster_log_types"]; logging != nil {
		result.AddWarning("EKS logging is enabled. Consider setting up logging in K3s.")
		result.AddManualStep("Configure K3s logging as needed")
	}

	// Handle add-ons
	m.handleAddons(res, result)

	result.AddManualStep("Wait for K3s to start: docker-compose logs -f k3s-server")
	result.AddManualStep("Get kubeconfig: cat ./kubeconfig/kubeconfig.yaml")
	result.AddManualStep("Export kubeconfig: export KUBECONFIG=$(pwd)/kubeconfig/kubeconfig.yaml")
	result.AddManualStep("Verify cluster: kubectl get nodes")

	// Add volume definition
	result.AddVolume(mapper.Volume{
		Name:   "k3s-server",
		Driver: "local",
	})

	return result, nil
}

// getK3sImage returns the appropriate K3s image based on Kubernetes version.
func (m *EKSMapper) getK3sImage(k8sVersion string) string {
	// Map EKS versions to K3s versions
	switch {
	case strings.HasPrefix(k8sVersion, "1.29"):
		return "rancher/k3s:v1.29.0-k3s1"
	case strings.HasPrefix(k8sVersion, "1.28"):
		return "rancher/k3s:v1.28.5-k3s1"
	case strings.HasPrefix(k8sVersion, "1.27"):
		return "rancher/k3s:v1.27.9-k3s1"
	case strings.HasPrefix(k8sVersion, "1.26"):
		return "rancher/k3s:v1.26.12-k3s1"
	default:
		return "rancher/k3s:latest"
	}
}

// generateAgentConfig generates K3s agent configuration for multi-node setup.
func (m *EKSMapper) generateAgentConfig(clusterName string) string {
	return fmt.Sprintf(`# K3s Agent Configuration
# Add this to docker-compose.yml for multi-node cluster

k3s-agent:
  image: rancher/k3s:latest
  command:
    - agent
  environment:
    K3S_URL: https://k3s-server:6443
    K3S_TOKEN: homeport-cluster-token
  volumes:
    - k3s-agent:/var/lib/rancher/k3s
  networks:
    - homeport
  depends_on:
    - k3s-server
  restart: unless-stopped
  labels:
    homeport.source: eks_node_group
    homeport.cluster_name: %s

volumes:
  k3s-agent:
`, clusterName)
}

// generateKubeconfigScript generates a script to set up kubeconfig.
func (m *EKSMapper) generateKubeconfigScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Kubeconfig Setup Script for K3s cluster: %s

set -e

KUBECONFIG_PATH="./kubeconfig/kubeconfig.yaml"

echo "Waiting for kubeconfig to be generated..."
while [ ! -f "$KUBECONFIG_PATH" ]; do
    echo "Waiting for K3s to start..."
    sleep 5
done

echo "Kubeconfig found!"

# Fix server address for external access
sed -i 's/127.0.0.1/localhost/g' "$KUBECONFIG_PATH"
sed -i 's/0.0.0.0/localhost/g' "$KUBECONFIG_PATH"

echo ""
echo "Kubeconfig is ready!"
echo ""
echo "To use kubectl, run:"
echo "  export KUBECONFIG=$(pwd)/kubeconfig/kubeconfig.yaml"
echo ""
echo "Or copy to default location:"
echo "  mkdir -p ~/.kube"
echo "  cp $KUBECONFIG_PATH ~/.kube/config"
echo ""
echo "Verify cluster:"
echo "  kubectl get nodes"
echo "  kubectl get pods -A"
`, clusterName)
}

// generateClusterInfoScript generates a script to display cluster info.
func (m *EKSMapper) generateClusterInfoScript(clusterName, k8sVersion string) string {
	return fmt.Sprintf(`#!/bin/bash
# Cluster Information for: %s

echo "=================================="
echo "K3s Cluster: %s"
echo "Kubernetes Version: %s (K3s equivalent)"
echo "=================================="
echo ""
echo "API Server: https://localhost:6443"
echo "Kubeconfig: ./kubeconfig/kubeconfig.yaml"
echo ""
echo "Quick Commands:"
echo "  kubectl get nodes"
echo "  kubectl get pods -A"
echo "  kubectl cluster-info"
echo ""
echo "Dashboard (optional):"
echo "  kubectl apply -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.7.0/aio/deploy/recommended.yaml"
echo ""
`, clusterName, clusterName, k8sVersion)
}

// handleAddons processes EKS add-ons and maps to K3s equivalents.
func (m *EKSMapper) handleAddons(res *resource.AWSResource, result *mapper.MappingResult) {
	// Check for common EKS add-ons
	if addons := res.Config["addon"]; addons != nil {
		if addonSlice, ok := addons.([]interface{}); ok {
			for _, addon := range addonSlice {
				if addonMap, ok := addon.(map[string]interface{}); ok {
					addonName, _ := addonMap["addon_name"].(string)
					switch addonName {
					case "vpc-cni":
						result.AddWarning("EKS VPC CNI add-on: K3s uses flannel by default")
					case "coredns":
						result.AddWarning("EKS CoreDNS add-on: K3s includes CoreDNS by default")
					case "kube-proxy":
						result.AddWarning("EKS kube-proxy add-on: K3s includes kube-proxy by default")
					case "aws-ebs-csi-driver":
						result.AddWarning("EKS EBS CSI driver: Use local-path-provisioner in K3s")
						result.AddManualStep("K3s includes local-path-provisioner for storage")
					case "aws-efs-csi-driver":
						result.AddWarning("EKS EFS CSI driver: Configure NFS storage class in K3s")
						result.AddManualStep("Set up NFS storage class for shared storage")
					default:
						result.AddWarning(fmt.Sprintf("EKS add-on '%s': Manual configuration required", addonName))
					}
				}
			}
		}
	}
}
