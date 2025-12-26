package compute

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewAppServiceMapper(t *testing.T) {
	m := NewAppServiceMapper()
	if m == nil {
		t.Fatal("NewAppServiceMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAppService {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAppService)
	}
}

func TestAppServiceMapper_ResourceType(t *testing.T) {
	m := NewAppServiceMapper()
	got := m.ResourceType()
	want := resource.TypeAppService

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestAppServiceMapper_Dependencies(t *testing.T) {
	m := NewAppServiceMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestAppServiceMapper_Validate(t *testing.T) {
	m := NewAppServiceMapper()

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
				Type: resource.TypeAppService,
				Name: "test-webapp",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAppService,
				Name: "test-webapp",
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

func TestAppServiceMapper_Map(t *testing.T) {
	m := NewAppServiceMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Node.js web app",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-webapp",
				Type: resource.TypeAppService,
				Name: "my-webapp",
				Config: map[string]interface{}{
					"name": "my-webapp",
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"node_version": "18",
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
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
				if result.DockerService.HealthCheck == nil {
					t.Error("HealthCheck is nil")
				}
				// Check for node image
				if result.DockerService.Image != "node:18-alpine" {
					t.Errorf("Expected node:18-alpine, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Python web app",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-python-app",
				Type: resource.TypeAppService,
				Name: "my-python-app",
				Config: map[string]interface{}{
					"name": "my-python-app",
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"python_version": "3.11",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "python:3.11-slim" {
					t.Errorf("Expected python:3.11-slim, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: ".NET web app",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-dotnet-app",
				Type: resource.TypeAppService,
				Name: "my-dotnet-app",
				Config: map[string]interface{}{
					"name": "my-dotnet-app",
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"dotnet_version": "8.0",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "mcr.microsoft.com/dotnet/aspnet:8.0" {
					t.Errorf("Expected mcr.microsoft.com/dotnet/aspnet:8.0, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Java web app",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-java-app",
				Type: resource.TypeAppService,
				Name: "my-java-app",
				Config: map[string]interface{}{
					"name": "my-java-app",
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"java_version": "17",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "eclipse-temurin:17-jre" {
					t.Errorf("Expected eclipse-temurin:17-jre, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "PHP web app",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-php-app",
				Type: resource.TypeAppService,
				Name: "my-php-app",
				Config: map[string]interface{}{
					"name": "my-php-app",
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"php_version": "8.2",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "php:8.2-apache" {
					t.Errorf("Expected php:8.2-apache, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "web app with connection strings",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-app",
				Type: resource.TypeAppService,
				Name: "my-app",
				Config: map[string]interface{}{
					"name": "my-app",
					"connection_string": []interface{}{
						map[string]interface{}{
							"name":  "DefaultConnection",
							"type":  "SQLAzure",
							"value": "Server=...",
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
					t.Error("Expected warnings for connection strings")
				}
			},
		},
		{
			name: "web app with HTTPS only",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/https-app",
				Type: resource.TypeAppService,
				Name: "https-app",
				Config: map[string]interface{}{
					"name":       "https-app",
					"https_only": true,
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have both ports
				if len(result.DockerService.Ports) != 2 {
					t.Errorf("Expected 2 ports for HTTPS app, got %d", len(result.DockerService.Ports))
				}
			},
		},
		{
			name: "web app with app settings",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/my-app",
				Type: resource.TypeAppService,
				Name: "my-app",
				Config: map[string]interface{}{
					"name": "my-app",
					"app_settings": map[string]interface{}{
						"CUSTOM_KEY": "custom-value",
						"API_KEY":    "secret",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Environment["CUSTOM_KEY"] != "custom-value" {
					t.Error("Expected CUSTOM_KEY to be set")
				}
				if result.DockerService.Environment["API_KEY"] != "secret" {
					t.Error("Expected API_KEY to be set")
				}
			},
		},
		{
			name: "web app with linux_fx_version",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Web/sites/linux-app",
				Type: resource.TypeAppService,
				Name: "linux-app",
				Config: map[string]interface{}{
					"name": "linux-app",
					"site_config": map[string]interface{}{
						"linux_fx_version": "NODE|20-lts",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Image != "node:20-alpine" {
					t.Errorf("Expected node:20-alpine, got %s", result.DockerService.Image)
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

func TestAppServiceMapper_getRuntime(t *testing.T) {
	m := NewAppServiceMapper()

	tests := []struct {
		name     string
		res      *resource.AWSResource
		expected string
	}{
		{
			name: "node from application_stack",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"node_version": "18",
						},
					},
				},
			},
			expected: "node",
		},
		{
			name: "python from application_stack",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"python_version": "3.11",
						},
					},
				},
			},
			expected: "python",
		},
		{
			name: "dotnet from application_stack",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"site_config": map[string]interface{}{
						"application_stack": map[string]interface{}{
							"dotnet_version": "8.0",
						},
					},
				},
			},
			expected: "dotnet",
		},
		{
			name: "node from linux_fx_version",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"site_config": map[string]interface{}{
						"linux_fx_version": "NODE|18-lts",
					},
				},
			},
			expected: "node",
		},
		{
			name: "python from linux_fx_version",
			res: &resource.AWSResource{
				Config: map[string]interface{}{
					"site_config": map[string]interface{}{
						"linux_fx_version": "PYTHON|3.11",
					},
				},
			},
			expected: "python",
		},
		{
			name: "default to node",
			res: &resource.AWSResource{
				Config: map[string]interface{}{},
			},
			expected: "node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.getRuntime(tt.res)
			if got != tt.expected {
				t.Errorf("getRuntime() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAppServiceMapper_sanitizeName(t *testing.T) {
	m := NewAppServiceMapper()

	tests := []struct {
		input    string
		expected string
	}{
		{"my-webapp", "my-webapp"},
		{"My_WebApp", "my-webapp"},
		{"123-app", "app"},
		{"---app", "app"},
		{"app123", "app123"},
		{"APP", "app"},
		{"my app!", "myapp"},
		{"", "webapp"},
		{"123", "webapp"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestAppServiceMapper_getDockerImage(t *testing.T) {
	m := NewAppServiceMapper()

	tests := []struct {
		runtime  string
		version  string
		expected string
	}{
		{"node", "18", "node:18-alpine"},
		{"node", "20", "node:20-alpine"},
		{"python", "3.11", "python:3.11-slim"},
		{"python", "3.12", "python:3.12-slim"},
		{"dotnet", "8.0", "mcr.microsoft.com/dotnet/aspnet:8.0"},
		{"dotnet", "7.0", "mcr.microsoft.com/dotnet/aspnet:7.0"},
		{"java", "17", "eclipse-temurin:17-jre"},
		{"java", "21", "eclipse-temurin:21-jre"},
		{"php", "8.2", "php:8.2-apache"},
		{"ruby", "3.2", "ruby:3.2-slim"},
		{"docker", "", "nginx:alpine"},
		{"unknown", "1.0", "node:20-alpine"},
	}

	for _, tt := range tests {
		t.Run(tt.runtime+"-"+tt.version, func(t *testing.T) {
			got := m.getDockerImage(tt.runtime, tt.version)
			if got != tt.expected {
				t.Errorf("getDockerImage(%q, %q) = %q, want %q", tt.runtime, tt.version, got, tt.expected)
			}
		})
	}
}
