// Package compute provides mappers for GCP compute services.
package compute

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/computeruntime"
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
		"K3S_TOKEN":               "homeport-gke-token",
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
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":       "google_container_cluster",
		"homeport.cluster_name": clusterName,
		"homeport.k8s_version":  minMasterVersion,
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
	result.AddConfig("config/gke/app-change.env", []byte(m.generateAppChangeConfig(clusterName)))
	result.AddConfig("config/gke/migration.env", []byte(m.generateMigrationConfig(res, clusterName)))
	result.AddConfig("config/gke/workload-export.env", []byte(m.generateWorkloadExportConfig(clusterName)))

	// Generate setup script
	setupScript := m.generateSetupScript(clusterName)
	result.AddScript("setup_k3s.sh", []byte(setupScript))
	result.AddScript("export_gke_workloads.sh", []byte(m.generateExportScript(clusterName)))
	result.AddScript("apply_k3s_workloads.sh", []byte(m.generateApplyScript(clusterName)))
	result.AddScript("validate_k3s_cluster.sh", []byte(m.generateValidateScript(clusterName)))
	result.AddScript("backup_gke_config.sh", []byte(m.generateBackupScript(clusterName)))
	result.AddScript("cutover_gke_clients.sh", []byte(m.generateCutoverScript(clusterName)))
	for _, step := range computeruntime.KubernetesCluster(clusterName, "setup_k3s.sh") {
		result.AddRunbookStep(step)
	}
	for _, step := range gkeCutoverRunbook(clusterName) {
		result.AddRunbookStep(step)
	}

	result.AddVolume(mapper.Volume{
		Name:   "k3s-server",
		Driver: "local",
	})

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
    K3S_TOKEN: homeport-gke-token
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

func (m *GKEMapper) generateAppChangeConfig(clusterName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_GKE_CLUSTER=%s\nTARGET_KUBECONFIG=./kubeconfig/kubeconfig.yaml\nTARGET_K8S_CONTEXT=k3s-%s\n", clusterName, clusterName)
}

func (m *GKEMapper) generateMigrationConfig(res *resource.AWSResource, clusterName string) string {
	return fmt.Sprintf("SOURCE_GKE_CLUSTER=%s\nSOURCE_LOCATION=%s\nSOURCE_VERSION=%s\nTARGET_DISTRIBUTION=k3s\n", clusterName, res.GetConfigString("location"), res.GetConfigString("min_master_version"))
}

func (m *GKEMapper) generateWorkloadExportConfig(clusterName string) string {
	return fmt.Sprintf("SOURCE_GKE_CLUSTER=%s\nEXPORT_PATH=./gke-export\nTARGET_MANIFEST_PATH=./k3s-manifests\n", clusterName)
}

func (m *GKEMapper) generateExportScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p gke-export\nkubectl --context %q get ns,deploy,svc,ingress,configmap,secret,pvc -A -o yaml > gke-export/workloads.yaml\n", clusterName)
}

func (m *GKEMapper) generateApplyScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s gke-export/workloads.yaml\nkubectl --kubeconfig ./kubeconfig/kubeconfig.yaml apply -f gke-export/workloads.yaml\necho \"GKE cluster %s workloads applied to K3s\"\n", clusterName)
}

func (m *GKEMapper) generateValidateScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/gke/app-change.env\nkubectl --kubeconfig ./kubeconfig/kubeconfig.yaml get nodes\nkubectl --kubeconfig ./kubeconfig/kubeconfig.yaml get deploy,svc -A\necho \"GKE cluster %s validated on K3s\"\n", clusterName)
}

func (m *GKEMapper) generateBackupScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/gke-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/gke config/k3s gke-export kubeconfig\necho \"$archive\"\n", sanitizeGKEName(clusterName))
}

func (m *GKEMapper) generateCutoverScript(clusterName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/gke/app-change.env\ntest \"$SOURCE_GKE_CLUSTER\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Kubernetes clients and CI deploy jobs to $TARGET_KUBECONFIG\"\n", clusterName)
}

func gkeCutoverRunbook(clusterName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "kubernetes", "source": "google_container_cluster", "cluster": clusterName, "target": "k3s"}
	return []domainrunbook.Step{
		gkeStep("backup-gke-config", "Backup GKE config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_gke_config.sh"}, "GKE migration artifacts are archived", metadata),
		gkeStep("cutover-gke-clients", "Cut over GKE clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_gke_clients.sh"}, "clients and deploy jobs use K3s kubeconfig", metadata),
	}
}

func gkeStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: "shell", Command: command, SuccessCondition: success, Metadata: metadata}
}

func sanitizeGKEName(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "cluster"
	}
	return value
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
