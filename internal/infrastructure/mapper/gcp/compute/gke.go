// Package compute provides mappers for GCP compute services.
package compute

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// GKEMapper converts GCP GKE clusters to K3s.
type GKEMapper struct {
	*mapper.BaseMapper
}

// NewGKEMapper creates a new GKE to K3s mapper.
func NewGKEMapper() *GKEMapper {
	return &GKEMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeGKE, nil),
	}
}

// Map converts a GKE cluster to a K3s Docker service.
func (m *GKEMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
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
	minMasterVersion := res.GetConfigString("min_master_version")
	k3sImage := m.getK3sImage(minMasterVersion)

	svc.Image = k3sImage
	svc.Command = []string{
		"server",
		"--disable=traefik",
		"--tls-san=localhost",
		"--tls-san=k3s-server",
	}
	svc.Environment = map[string]string{
		"K3S_TOKEN":               "cloudexit-gke-token",
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
	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"cloudexit.source":       "google_container_cluster",
		"cloudexit.cluster_name": clusterName,
		"cloudexit.k8s_version":  minMasterVersion,
	}
	svc.CapAdd = []string{"NET_ADMIN", "SYS_ADMIN"}

	// Handle node pools
	if nodePools := res.Config["node_pool"]; nodePools != nil {
		m.handleNodePools(nodePools, result)
	}

	// Handle network policy
	if networkPolicy := res.Config["network_policy"]; networkPolicy != nil {
		result.AddWarning("GKE network policy enabled. K3s supports network policies via CNI.")
	}

	// Handle addons
	if addonsConfig := res.Config["addons_config"]; addonsConfig != nil {
		m.handleAddons(addonsConfig, result)
	}

	// Handle private cluster
	if privateCluster := res.Config["private_cluster_config"]; privateCluster != nil {
		result.AddWarning("GKE private cluster configuration detected. K3s runs in Docker network isolation.")
	}

	// Handle workload identity
	if workloadIdentity := res.Config["workload_identity_config"]; workloadIdentity != nil {
		result.AddWarning("Workload Identity is configured. Configure service accounts manually in K3s.")
	}

	// Generate agent configuration
	agentConfig := m.generateAgentConfig(clusterName)
	result.AddConfig("config/k3s/agent-compose.yml", []byte(agentConfig))

	// Generate setup script
	setupScript := m.generateSetupScript(clusterName)
	result.AddScript("setup_k3s.sh", []byte(setupScript))

	result.AddVolume(mapper.Volume{
		Name:   "k3s-server",
		Driver: "local",
	})

	result.AddManualStep("Wait for K3s: docker-compose logs -f k3s-server")
	result.AddManualStep("Get kubeconfig: export KUBECONFIG=$(pwd)/kubeconfig/kubeconfig.yaml")
	result.AddManualStep("Verify: kubectl get nodes")

	return result, nil
}

func (m *GKEMapper) getK3sImage(version string) string {
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

func (m *GKEMapper) handleNodePools(nodePools interface{}, result *mapper.MappingResult) {
	if npSlice, ok := nodePools.([]interface{}); ok {
		for _, np := range npSlice {
			if npMap, ok := np.(map[string]interface{}); ok {
				name, _ := npMap["name"].(string)
				nodeCount := 0
				if nc, ok := npMap["node_count"].(float64); ok {
					nodeCount = int(nc)
				}
				result.AddWarning(fmt.Sprintf("Node pool '%s' with %d nodes. Add K3s agents for multi-node setup.", name, nodeCount))
			}
		}
	}
}

func (m *GKEMapper) handleAddons(addons interface{}, result *mapper.MappingResult) {
	if addonsMap, ok := addons.(map[string]interface{}); ok {
		if httpLB, ok := addonsMap["http_load_balancing"].(map[string]interface{}); ok {
			if disabled, ok := httpLB["disabled"].(bool); ok && !disabled {
				result.AddWarning("GKE HTTP Load Balancing is enabled. Use Traefik or nginx ingress in K3s.")
			}
		}
		if hpa, ok := addonsMap["horizontal_pod_autoscaling"].(map[string]interface{}); ok {
			if disabled, ok := hpa["disabled"].(bool); ok && !disabled {
				result.AddWarning("HPA is enabled. K3s supports HPA by default.")
			}
		}
	}
}

func (m *GKEMapper) generateAgentConfig(clusterName string) string {
	return fmt.Sprintf(`# K3s Agent Configuration for GKE cluster: %s

k3s-agent:
  image: rancher/k3s:latest
  command:
    - agent
  environment:
    K3S_URL: https://k3s-server:6443
    K3S_TOKEN: cloudexit-gke-token
  volumes:
    - k3s-agent:/var/lib/rancher/k3s
  networks:
    - cloudexit
  depends_on:
    - k3s-server
  restart: unless-stopped

volumes:
  k3s-agent:
`, clusterName)
}

func (m *GKEMapper) generateSetupScript(clusterName string) string {
	return fmt.Sprintf(`#!/bin/bash
# K3s Setup Script for GKE cluster: %s

set -e

echo "Starting K3s cluster..."
docker-compose up -d k3s-server

echo "Waiting for K3s to be ready..."
sleep 30

echo "Fixing kubeconfig..."
sed -i 's/127.0.0.1/localhost/g' ./kubeconfig/kubeconfig.yaml 2>/dev/null || true

export KUBECONFIG=$(pwd)/kubeconfig/kubeconfig.yaml

echo "Verifying cluster..."
kubectl get nodes

echo ""
echo "K3s cluster is ready!"
echo "Run: export KUBECONFIG=$(pwd)/kubeconfig/kubeconfig.yaml"
`, clusterName)
}
