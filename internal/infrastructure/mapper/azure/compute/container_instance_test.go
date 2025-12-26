package compute

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewContainerInstanceMapper(t *testing.T) {
	m := NewContainerInstanceMapper()
	if m == nil {
		t.Fatal("NewContainerInstanceMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeContainerInstance {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeContainerInstance)
	}
}

func TestContainerInstanceMapper_ResourceType(t *testing.T) {
	m := NewContainerInstanceMapper()
	got := m.ResourceType()
	want := resource.TypeContainerInstance

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestContainerInstanceMapper_Dependencies(t *testing.T) {
	m := NewContainerInstanceMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestContainerInstanceMapper_Validate(t *testing.T) {
	m := NewContainerInstanceMapper()

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
				Type: resource.TypeEC2Instance,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeContainerInstance,
				Name: "test-container-group",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeContainerInstance,
				Name: "test-container-group",
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

func TestContainerInstanceMapper_Map(t *testing.T) {
	m := NewContainerInstanceMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic container group",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":    "my-container-group",
					"os_type": "Linux",
					"container": []interface{}{
						map[string]interface{}{
							"name":   "my-app",
							"image":  "nginx:latest",
							"cpu":    float64(1),
							"memory": float64(1.5),
							"ports": []interface{}{
								map[string]interface{}{
									"port":     float64(80),
									"protocol": "TCP",
								},
							},
						},
					},
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
				if result.DockerService.Image != "nginx:latest" {
					t.Errorf("DockerService.Image = %v, want nginx:latest", result.DockerService.Image)
				}
				if len(result.DockerService.Ports) != 1 {
					t.Errorf("DockerService.Ports length = %v, want 1", len(result.DockerService.Ports))
				}
				if result.DockerService.Ports[0] != "80:80" {
					t.Errorf("DockerService.Ports[0] = %v, want 80:80", result.DockerService.Ports[0])
				}
				if result.DockerService.Deploy == nil {
					t.Error("DockerService.Deploy is nil")
				}
			},
		},
		{
			name: "container with environment variables",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":    "my-container-group",
					"os_type": "Linux",
					"container": []interface{}{
						map[string]interface{}{
							"name":   "my-app",
							"image":  "myapp:v1",
							"cpu":    float64(0.5),
							"memory": float64(0.5),
							"environment_variables": map[string]interface{}{
								"APP_ENV":  "production",
								"LOG_LEVEL": "info",
							},
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Environment == nil {
					t.Fatal("DockerService.Environment is nil")
				}
				if result.DockerService.Environment["APP_ENV"] != "production" {
					t.Errorf("Environment APP_ENV = %v, want production", result.DockerService.Environment["APP_ENV"])
				}
			},
		},
		{
			name: "container with DNS label",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":           "my-container-group",
					"os_type":        "Linux",
					"dns_name_label": "myapp-dns",
					"container": []interface{}{
						map[string]interface{}{
							"name":   "my-app",
							"image":  "nginx:latest",
							"cpu":    float64(1),
							"memory": float64(1),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for DNS label configuration")
				}
				if result.DockerService.Labels["cloudexit.dns_label"] != "myapp-dns" {
					t.Errorf("Label cloudexit.dns_label = %v, want myapp-dns", result.DockerService.Labels["cloudexit.dns_label"])
				}
			},
		},
		{
			name: "Windows container",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":    "my-container-group",
					"os_type": "Windows",
					"container": []interface{}{
						map[string]interface{}{
							"name":   "my-windows-app",
							"image":  "mcr.microsoft.com/windows/servercore:ltsc2019",
							"cpu":    float64(2),
							"memory": float64(4),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for Windows container")
				}
			},
		},
		{
			name: "multi-container group",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":    "my-container-group",
					"os_type": "Linux",
					"container": []interface{}{
						map[string]interface{}{
							"name":   "main-app",
							"image":  "myapp:latest",
							"cpu":    float64(1),
							"memory": float64(1),
						},
						map[string]interface{}{
							"name":   "sidecar-logging",
							"image":  "fluentd:latest",
							"cpu":    float64(0.5),
							"memory": float64(0.5),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for multi-container group")
				}
				if len(result.Configs) == 0 {
					t.Error("Expected sidecar config files")
				}
			},
		},
		{
			name: "container with restart policy",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":           "my-container-group",
					"os_type":        "Linux",
					"restart_policy": "OnFailure",
					"container": []interface{}{
						map[string]interface{}{
							"name":   "my-app",
							"image":  "myapp:latest",
							"cpu":    float64(1),
							"memory": float64(1),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Restart != "on-failure" {
					t.Errorf("DockerService.Restart = %v, want on-failure", result.DockerService.Restart)
				}
			},
		},
		{
			name: "container with volumes",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":    "my-container-group",
					"os_type": "Linux",
					"container": []interface{}{
						map[string]interface{}{
							"name":   "my-app",
							"image":  "myapp:latest",
							"cpu":    float64(1),
							"memory": float64(1),
							"volume": []interface{}{
								map[string]interface{}{
									"name":       "data-volume",
									"mount_path": "/data",
									"read_only":  false,
								},
							},
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if len(result.DockerService.Volumes) != 1 {
					t.Errorf("DockerService.Volumes length = %v, want 1", len(result.DockerService.Volumes))
				}
			},
		},
		{
			name: "container with UDP port",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":    "my-container-group",
					"os_type": "Linux",
					"container": []interface{}{
						map[string]interface{}{
							"name":   "dns-server",
							"image":  "coredns:latest",
							"cpu":    float64(0.5),
							"memory": float64(0.5),
							"ports": []interface{}{
								map[string]interface{}{
									"port":     float64(53),
									"protocol": "UDP",
								},
							},
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if len(result.DockerService.Ports) != 1 {
					t.Errorf("DockerService.Ports length = %v, want 1", len(result.DockerService.Ports))
				}
				if result.DockerService.Ports[0] != "53:53/udp" {
					t.Errorf("DockerService.Ports[0] = %v, want 53:53/udp", result.DockerService.Ports[0])
				}
			},
		},
		{
			name: "no containers defined",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/my-group",
				Type: resource.TypeContainerInstance,
				Name: "my-container-group",
				Config: map[string]interface{}{
					"name":    "my-container-group",
					"os_type": "Linux",
				},
			},
			wantErr: true,
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
				Type: resource.TypeEC2Instance,
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

func TestContainerInstanceMapper_sanitizeServiceName(t *testing.T) {
	m := NewContainerInstanceMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"My_Container", "my-container"},
		{"my container", "my-container"},
		{"MyApp", "myapp"},
		{"test_app_name", "test-app-name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeServiceName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeServiceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
