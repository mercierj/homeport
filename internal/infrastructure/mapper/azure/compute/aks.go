// Package compute provides mappers for Azure compute services.
package compute

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// AKSMapper converts Azure Kubernetes Service to K3s.
type AKSMapper struct {
	*mapper.BaseMapper
}

// NewAKSMapper creates a new AKS to K3s mapper.
func NewAKSMapper() *AKSMapper {
	return &AKSMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAKS, nil),
	}
}

// Map converts an AKS cluster to a K3s Docker service.
func (m *AKSMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	clusterName := res.GetConfigString("name")
	if clusterName == "" {
		clusterName = res.Name
	}

	result := mapper.NewMappingResult("k3s-server")
	svc := result.DockerService

	// Get K8s version
	k8sVersion := res.GetConfigString("kubernetes_version")
	k3sImage := m.getK3sImage(k8sVersion)

	svc.Image = k3sImage
	svc.Command = []string{
		"server",
		"--disable=traefik",
		"--tls-san=localhost",
		"--tls-san=k3s-server",
	}
	svc.Environment = map[string]string{
		"K3S_TOKEN":               "homeport-aks-token",
		"K3S_KUBECONFIG_OUTPUT":   "/output/kubeconfig.yaml",
		"K3S_KUBECONFIG_MODE":     "666",
		"KUBERNETES_CLUSTER_NAME": clusterName,
	}
	svc.Ports = []string{
		"6443:6443",
		"80:80",
		"443:443",
	}
	svc.Volumes = []string{
		"k3s-server:/var/lib/rancher/k3s",
		"./kubeconfig:/output",
	}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":       "azurerm_kubernetes_cluster",
		"homeport.cluster_name": clusterName,
		"homeport.k8s_version":  k8sVersion,
	}
	svc.CapAdd = []string{"NET_ADMIN", "SYS_ADMIN"}

	// Handle default node pool
	if defaultNodePool := res.Config["default_node_pool"]; defaultNodePool != nil {
		m.handleNodePool(defaultNodePool, result)
	}

	// Handle network profile
	if networkProfile := res.Config["network_profile"]; networkProfile != nil {
		m.handleNetworkProfile(networkProfile, result)
	}

	// Handle Azure AD integration
	if aadProfile := res.Config["azure_active_directory_role_based_access_control"]; aadProfile != nil {
		result.AddWarning("Azure AD RBAC is configured. Set up RBAC manually in K3s.")
	}

	// Handle managed identity
	if identity := res.Config["identity"]; identity != nil {
		result.AddWarning("Managed identity is configured. Configure service accounts manually.")
	}

	// Handle add-ons
	if httpAppRouting := res.Config["http_application_routing_enabled"]; httpAppRouting != nil {
		result.AddWarning("HTTP application routing is enabled. Use Traefik for ingress.")
	}

	// Generate agent config
	agentConfig := m.generateAgentConfig(clusterName)
	result.AddConfig("config/k3s/agent-compose.yml", []byte(agentConfig))

	// Generate setup script
	setupScript := m.generateSetupScript(clusterName)
	result.AddScript("setup_k3s.sh", []byte(setupScript))

	result.AddVolume(mapper.Volume{
		Name:   "k3s-server",
		Driver: "local",
	})

	result.AddManualStep("Start K3s: docker-compose up -d k3s-server")
	result.AddManualStep("Get kubeconfig: export KUBECONFIG=$(pwd)/kubeconfig/kubeconfig.yaml")
	result.AddManualStep("Verify: kubectl get nodes")

	return result, nil
}

func (m *AKSMapper) getK3sImage(version string) string {
	switch {
	case strings.HasPrefix(version, "1.29"):
		return "rancher/k3s:v1.29.0-k3s1"
	case strings.HasPrefix(version, "1.28"):
		return "rancher/k3s:v1.28.5-k3s1"
	case strings.HasPrefix(version, "1.27"):
		return "rancher/k3s:v1.27.9-k3s1"
	default:
		return "rancher/k3s:latest"
	}
}

func (m *AKSMapper) handleNodePool(nodePool interface{}, result *mapper.MappingResult) {
	if npMap, ok := nodePool.(map[string]interface{}); ok {
		name, _ := npMap["name"].(string)
		nodeCount := 1
		if nc, ok := npMap["node_count"].(float64); ok {
			nodeCount = int(nc)
		}
		vmSize, _ := npMap["vm_size"].(string)

		result.AddWarning(fmt.Sprintf("Node pool '%s' with %d nodes (size: %s). Add K3s agents for multi-node.", name, nodeCount, vmSize))
	}
}

func (m *AKSMapper) handleNetworkProfile(profile interface{}, result *mapper.MappingResult) {
	if profMap, ok := profile.(map[string]interface{}); ok {
		networkPlugin, _ := profMap["network_plugin"].(string)
		networkPolicy, _ := profMap["network_policy"].(string)

		if networkPlugin == "azure" {
			result.AddWarning("Azure CNI is used. K3s uses flannel by default.")
		}
		if networkPolicy == "calico" || networkPolicy == "azure" {
			result.AddWarning(fmt.Sprintf("Network policy '%s' is enabled. K3s supports network policies via CNI.", networkPolicy))
		}
	}
}

func (m *AKSMapper) generateAgentConfig(clusterName string) string {
	return fmt.Sprintf(`# K3s Agent Configuration for AKS cluster: %s

k3s-agent:
  image: rancher/k3s:latest
  command:
    - agent
  environment:
    K3S_URL: https://k3s-server:6443
    K3S_TOKEN: homeport-aks-token
  volumes:
    - k3s-agent:/var/lib/rancher/k3s
  networks:
    - homeport
  depends_on:
    - k3s-server
  restart: unless-stopped

volumes:
  k3s-agent:
`, clusterName)
}

func (m *AKSMapper) generateSetupScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/bash
# K3s Setup Script for AKS cluster: %s

set -e

echo "Starting K3s cluster..."
docker-compose up -d k3s-server

echo "Waiting for K3s to be ready..."
sleep 30

sed -i 's/127.0.0.1/localhost/g' ./kubeconfig/kubeconfig.yaml 2>/dev/null || true

export KUBECONFIG=$(pwd)/kubeconfig/kubeconfig.yaml

echo "Verifying cluster..."
kubectl get nodes

echo ""
echo "K3s cluster is ready!"
`, clusterName)
}
