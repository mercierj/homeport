package compute

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewEKSMapper(t *testing.T) {
	m := NewEKSMapper()
	if m == nil {
		t.Fatal("NewEKSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeEKSCluster {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeEKSCluster)
	}
}

func TestEKSMapper_ResourceType(t *testing.T) {
	m := NewEKSMapper()
	got := m.ResourceType()
	want := resource.TypeEKSCluster

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestEKSMapper_Dependencies(t *testing.T) {
	m := NewEKSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestEKSMapper_Validate(t *testing.T) {
	m := NewEKSMapper()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
	}{
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeS3Bucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeEKSCluster,
				Name: "test-cluster",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeEKSCluster,
				Name: "test-cluster",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEKSMapper_Map(t *testing.T) {
	m := NewEKSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic EKS cluster",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.28",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
				// Check for K3s image
				if result.DockerService.Image != "rancher/k3s:v1.28.5-k3s1" {
					t.Errorf("DockerService.Image = %v, want rancher/k3s:v1.28.5-k3s1", result.DockerService.Image)
				}
				if result.DockerService.Environment == nil {
					t.Error("DockerService.Environment is nil")
				}
				if _, ok := result.DockerService.Environment["K3S_TOKEN"]; !ok {
					t.Error("K3S_TOKEN not set in environment")
				}
				if _, ok := result.DockerService.Environment["KUBERNETES_CLUSTER_NAME"]; !ok {
					t.Error("KUBERNETES_CLUSTER_NAME not set in environment")
				}
				if result.DockerService.Labels == nil {
					t.Error("DockerService.Labels is nil")
				}
				if result.DockerService.Labels["cloudexit.source"] != "aws_eks_cluster" {
					t.Errorf("Label cloudexit.source = %v, want aws_eks_cluster", result.DockerService.Labels["cloudexit.source"])
				}
				if result.DockerService.Labels["cloudexit.cluster_name"] != "my-cluster" {
					t.Errorf("Label cloudexit.cluster_name = %v, want my-cluster", result.DockerService.Labels["cloudexit.cluster_name"])
				}
			},
		},
		{
			name: "EKS cluster with version 1.29",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.29",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "rancher/k3s:v1.29.0-k3s1" {
					t.Errorf("DockerService.Image = %v, want rancher/k3s:v1.29.0-k3s1", result.DockerService.Image)
				}
			},
		},
		{
			name: "EKS cluster with version 1.27",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.27",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "rancher/k3s:v1.27.9-k3s1" {
					t.Errorf("DockerService.Image = %v, want rancher/k3s:v1.27.9-k3s1", result.DockerService.Image)
				}
			},
		},
		{
			name: "EKS cluster with version 1.26",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.26",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "rancher/k3s:v1.26.12-k3s1" {
					t.Errorf("DockerService.Image = %v, want rancher/k3s:v1.26.12-k3s1", result.DockerService.Image)
				}
			},
		},
		{
			name: "EKS cluster with unknown version uses latest",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.25",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "rancher/k3s:latest" {
					t.Errorf("DockerService.Image = %v, want rancher/k3s:latest", result.DockerService.Image)
				}
			},
		},
		{
			name: "EKS cluster with node groups",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.28",
					"node_groups": []interface{}{
						map[string]interface{}{
							"name":          "workers",
							"instance_type": "t3.medium",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have a warning about node groups
				hasNodeGroupWarning := false
				for _, w := range result.Warnings {
					if w == "EKS node groups detected. K3s agents can be added for multi-node setup." {
						hasNodeGroupWarning = true
						break
					}
				}
				if !hasNodeGroupWarning {
					t.Error("Expected warning about node groups")
				}
			},
		},
		{
			name: "EKS cluster with VPC config",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.28",
					"vpc_config": map[string]interface{}{
						"subnet_ids": []string{"subnet-1", "subnet-2"},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have a warning about VPC configuration
				hasVPCWarning := false
				for _, w := range result.Warnings {
					if w == "VPC configuration detected. K3s uses Docker networking by default." {
						hasVPCWarning = true
						break
					}
				}
				if !hasVPCWarning {
					t.Error("Expected warning about VPC configuration")
				}
			},
		},
		{
			name: "EKS cluster with encryption config",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.28",
					"encryption_config": map[string]interface{}{
						"provider": map[string]interface{}{
							"key_arn": "arn:aws:kms:us-east-1:123456789012:key/1234",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have a warning about encryption
				hasEncryptionWarning := false
				for _, w := range result.Warnings {
					if w == "EKS encryption is configured. Consider enabling K3s secrets encryption." {
						hasEncryptionWarning = true
						break
					}
				}
				if !hasEncryptionWarning {
					t.Error("Expected warning about encryption configuration")
				}
			},
		},
		{
			name: "EKS cluster with logging enabled",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":                      "my-cluster",
					"version":                   "1.28",
					"enabled_cluster_log_types": []string{"api", "audit"},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have a warning about logging
				hasLoggingWarning := false
				for _, w := range result.Warnings {
					if w == "EKS logging is enabled. Consider setting up logging in K3s." {
						hasLoggingWarning = true
						break
					}
				}
				if !hasLoggingWarning {
					t.Error("Expected warning about logging configuration")
				}
			},
		},
		{
			name: "EKS cluster with add-ons",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.28",
					"addon": []interface{}{
						map[string]interface{}{
							"addon_name": "vpc-cni",
						},
						map[string]interface{}{
							"addon_name": "coredns",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about add-ons
				if len(result.Warnings) < 2 {
					t.Error("Expected warnings about add-ons")
				}
			},
		},
		{
			name: "EKS cluster generates scripts and configs",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.28",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Check for scripts
				if len(result.Scripts) < 2 {
					t.Error("Expected at least 2 scripts (setup_kubeconfig.sh and cluster_info.sh)")
				}
				// Check for configs
				if len(result.Configs) < 1 {
					t.Error("Expected at least 1 config (agent-compose.yml)")
				}
				// Check for volumes
				if len(result.Volumes) < 1 {
					t.Error("Expected at least 1 volume definition")
				}
			},
		},
		{
			name: "EKS cluster with ports and volumes",
			res: &resource.AWSResource{
				ID:   "arn:aws:eks:us-east-1:123456789012:cluster/my-cluster",
				Type: resource.TypeEKSCluster,
				Name: "my-cluster",
				Config: map[string]interface{}{
					"name":    "my-cluster",
					"version": "1.28",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Check ports include Kubernetes API
				hasAPIPort := false
				for _, p := range result.DockerService.Ports {
					if p == "6443:6443" {
						hasAPIPort = true
						break
					}
				}
				if !hasAPIPort {
					t.Error("Expected port 6443:6443 for Kubernetes API")
				}
				// Check volumes
				if len(result.DockerService.Volumes) < 1 {
					t.Error("Expected at least 1 volume mount")
				}
				// Check networks
				if len(result.DockerService.Networks) < 1 || result.DockerService.Networks[0] != "cloudexit" {
					t.Error("Expected network 'cloudexit'")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "wrong-id",
				Type: resource.TypeS3Bucket,
				Name: "wrong",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.Map(ctx, tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestEKSMapper_getK3sImage(t *testing.T) {
	m := NewEKSMapper()

	tests := []struct {
		name       string
		k8sVersion string
		want       string
	}{
		{
			name:       "version 1.29.x",
			k8sVersion: "1.29.1",
			want:       "rancher/k3s:v1.29.0-k3s1",
		},
		{
			name:       "version 1.28.x",
			k8sVersion: "1.28.5",
			want:       "rancher/k3s:v1.28.5-k3s1",
		},
		{
			name:       "version 1.27.x",
			k8sVersion: "1.27.9",
			want:       "rancher/k3s:v1.27.9-k3s1",
		},
		{
			name:       "version 1.26.x",
			k8sVersion: "1.26.12",
			want:       "rancher/k3s:v1.26.12-k3s1",
		},
		{
			name:       "unknown version",
			k8sVersion: "1.25.0",
			want:       "rancher/k3s:latest",
		},
		{
			name:       "empty version",
			k8sVersion: "",
			want:       "rancher/k3s:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.getK3sImage(tt.k8sVersion)
			if got != tt.want {
				t.Errorf("getK3sImage(%q) = %v, want %v", tt.k8sVersion, got, tt.want)
			}
		})
	}
}
