package compute

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewECSMapper(t *testing.T) {
	m := NewECSMapper()
	if m == nil {
		t.Fatal("NewECSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeECSService {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeECSService)
	}
}

func TestECSMapper_ResourceType(t *testing.T) {
	m := NewECSMapper()
	got := m.ResourceType()
	want := resource.TypeECSService

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestECSMapper_Dependencies(t *testing.T) {
	m := NewECSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestECSMapper_Validate(t *testing.T) {
	m := NewECSMapper()

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
				Type: resource.TypeECSService,
				Name: "test-service",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeECSService,
				Name: "test-service",
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

func TestECSMapper_Map(t *testing.T) {
	m := NewECSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic ECS service",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(2),
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
				if result.DockerService.Environment == nil {
					t.Error("DockerService.Environment is nil")
				}
				if result.DockerService.Labels == nil {
					t.Error("DockerService.Labels is nil")
				}
				if result.DockerService.Labels["homeport.source"] != "aws_ecs_service" {
					t.Errorf("Label homeport.source = %v, want aws_ecs_service", result.DockerService.Labels["homeport.source"])
				}
				if result.DockerService.Labels["homeport.service_name"] != "my-service" {
					t.Errorf("Label homeport.service_name = %v, want my-service", result.DockerService.Labels["homeport.service_name"])
				}
				// Check Deploy config
				if result.DockerService.Deploy == nil {
					t.Error("DockerService.Deploy is nil")
				} else if result.DockerService.Deploy.Replicas != 2 {
					t.Errorf("DockerService.Deploy.Replicas = %v, want 2", result.DockerService.Deploy.Replicas)
				}
			},
		},
		{
			name: "ECS service with container definitions",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(1),
					"container_definitions": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
							"environment": []interface{}{
								map[string]interface{}{
									"name":  "APP_ENV",
									"value": "production",
								},
							},
							"portMappings": []interface{}{
								map[string]interface{}{
									"containerPort": float64(80),
									"hostPort":      float64(8080),
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
				if result.DockerService.Image != "nginx:1.21" {
					t.Errorf("DockerService.Image = %v, want nginx:1.21", result.DockerService.Image)
				}
				if result.DockerService.Environment["APP_ENV"] != "production" {
					t.Errorf("Environment APP_ENV = %v, want production", result.DockerService.Environment["APP_ENV"])
				}
				// Check ports
				hasPort := false
				for _, p := range result.DockerService.Ports {
					if p == "8080:80" {
						hasPort = true
						break
					}
				}
				if !hasPort {
					t.Errorf("Expected port 8080:80, got %v", result.DockerService.Ports)
				}
			},
		},
		{
			name: "ECS service with resource limits",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(1),
					"cpu":             float64(512),
					"memory":          float64(1024),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Deploy == nil {
					t.Fatal("DockerService.Deploy is nil")
				}
				if result.DockerService.Deploy.Resources == nil {
					t.Fatal("DockerService.Deploy.Resources is nil")
				}
				if result.DockerService.Deploy.Resources.Limits == nil {
					t.Fatal("DockerService.Deploy.Resources.Limits is nil")
				}
				if result.DockerService.Deploy.Resources.Limits.CPUs != "0.50" {
					t.Errorf("CPUs = %v, want 0.50", result.DockerService.Deploy.Resources.Limits.CPUs)
				}
				if result.DockerService.Deploy.Resources.Limits.Memory != "1024M" {
					t.Errorf("Memory = %v, want 1024M", result.DockerService.Deploy.Resources.Limits.Memory)
				}
			},
		},
		{
			name: "ECS service with load balancer",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(1),
					"load_balancer": []interface{}{
						map[string]interface{}{
							"container_name": "web",
							"container_port": float64(80),
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have Traefik labels
				if result.DockerService.Labels["traefik.enable"] != "true" {
					t.Error("Expected traefik.enable label to be true")
				}
				// Should have a warning about load balancer
				hasLBWarning := false
				for _, w := range result.Warnings {
					if w == "ECS service uses load balancer. Traefik labels have been configured for routing." {
						hasLBWarning = true
						break
					}
				}
				if !hasLBWarning {
					t.Error("Expected warning about load balancer")
				}
			},
		},
		{
			name: "ECS service with service discovery",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(1),
					"service_registries": []interface{}{
						map[string]interface{}{
							"registry_arn": "arn:aws:servicediscovery:us-east-1:123456789012:service/srv-xxx",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have a warning about service discovery
				hasSDWarning := false
				for _, w := range result.Warnings {
					if w == "ECS Service Discovery is configured. Consider using Docker DNS or Traefik for service discovery." {
						hasSDWarning = true
						break
					}
				}
				if !hasSDWarning {
					t.Error("Expected warning about service discovery")
				}
			},
		},
		{
			name: "ECS service with circuit breaker",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(1),
					"deployment_circuit_breaker": map[string]interface{}{
						"enable":   true,
						"rollback": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have a warning about circuit breaker
				hasCBWarning := false
				for _, w := range result.Warnings {
					if w == "Deployment circuit breaker is enabled. Docker Compose has limited deployment rollback support." {
						hasCBWarning = true
						break
					}
				}
				if !hasCBWarning {
					t.Error("Expected warning about circuit breaker")
				}
			},
		},
		{
			name: "ECS service with health check",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(1),
					"container_definitions": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
							"healthCheck": map[string]interface{}{
								"command":  []interface{}{"CMD-SHELL", "curl -f http://localhost/ || exit 1"},
								"interval": float64(30),
								"timeout":  float64(5),
								"retries":  float64(3),
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
				if result.DockerService.HealthCheck == nil {
					t.Error("Expected HealthCheck to be set")
				} else {
					if len(result.DockerService.HealthCheck.Test) != 2 {
						t.Errorf("HealthCheck.Test length = %v, want 2", len(result.DockerService.HealthCheck.Test))
					}
					if result.DockerService.HealthCheck.Retries != 3 {
						t.Errorf("HealthCheck.Retries = %v, want 3", result.DockerService.HealthCheck.Retries)
					}
				}
			},
		},
		{
			name: "ECS service with volume mounts",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(1),
					"container_definitions": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
							"mountPoints": []interface{}{
								map[string]interface{}{
									"sourceVolume":  "app-data",
									"containerPath": "/var/www/html",
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
				hasVolume := false
				for _, v := range result.DockerService.Volumes {
					if v == "./data/app-data:/var/www/html" {
						hasVolume = true
						break
					}
				}
				if !hasVolume {
					t.Errorf("Expected volume ./data/app-data:/var/www/html, got %v", result.DockerService.Volumes)
				}
			},
		},
		{
			name: "ECS service generates setup script",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
					"desired_count":   float64(1),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if len(result.Scripts) < 1 {
					t.Error("Expected at least 1 script (setup_ecs_service.sh)")
				}
			},
		},
		{
			name: "ECS service with default desired count",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "my-service",
				Config: map[string]interface{}{
					"name":            "my-service",
					"task_definition": "my-task:1",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Deploy == nil {
					t.Fatal("DockerService.Deploy is nil")
				}
				// Default desired count should be 1
				if result.DockerService.Deploy.Replicas != 1 {
					t.Errorf("DockerService.Deploy.Replicas = %v, want 1 (default)", result.DockerService.Deploy.Replicas)
				}
			},
		},
		{
			name: "ECS service uses resource name when name config is missing",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:service/my-cluster/my-service",
				Type: resource.TypeECSService,
				Name: "fallback-service-name",
				Config: map[string]interface{}{
					"task_definition": "my-task:1",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Labels["homeport.service_name"] != "fallback-service-name" {
					t.Errorf("Label homeport.service_name = %v, want fallback-service-name", result.DockerService.Labels["homeport.service_name"])
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

func TestECSMapper_sanitizeServiceName(t *testing.T) {
	m := NewECSMapper()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "already valid",
			input: "my-service",
			want:  "my-service",
		},
		{
			name:  "uppercase to lowercase",
			input: "My-Service",
			want:  "my-service",
		},
		{
			name:  "underscores to hyphens",
			input: "my_service_name",
			want:  "my-service-name",
		},
		{
			name:  "spaces to hyphens",
			input: "my service name",
			want:  "my-service-name",
		},
		{
			name:  "special characters removed",
			input: "my@service#name!",
			want:  "myservicename",
		},
		{
			name:  "leading numbers removed",
			input: "123-my-service",
			want:  "my-service",
		},
		{
			name:  "leading hyphens removed",
			input: "---my-service",
			want:  "my-service",
		},
		{
			name:  "empty string returns service",
			input: "",
			want:  "service",
		},
		{
			name:  "only special chars returns service",
			input: "!!!@@@###",
			want:  "service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.sanitizeServiceName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeServiceName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewECSTaskDefMapper(t *testing.T) {
	m := NewECSTaskDefMapper()
	if m == nil {
		t.Fatal("NewECSTaskDefMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeECSTaskDef {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeECSTaskDef)
	}
}

func TestECSTaskDefMapper_ResourceType(t *testing.T) {
	m := NewECSTaskDefMapper()
	got := m.ResourceType()
	want := resource.TypeECSTaskDef

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestECSTaskDefMapper_Validate(t *testing.T) {
	m := NewECSTaskDefMapper()

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
				Type: resource.TypeECSTaskDef,
				Name: "test-task",
			},
			wantErr: false,
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

func TestECSTaskDefMapper_Map(t *testing.T) {
	m := NewECSTaskDefMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic task definition",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1",
				Type: resource.TypeECSTaskDef,
				Name: "my-task",
				Config: map[string]interface{}{
					"family": "my-task",
					"container_definitions": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
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
				if result.DockerService.Image != "nginx:1.21" {
					t.Errorf("DockerService.Image = %v, want nginx:1.21", result.DockerService.Image)
				}
				if result.DockerService.Labels["homeport.source"] != "aws_ecs_task_definition" {
					t.Errorf("Label homeport.source = %v, want aws_ecs_task_definition", result.DockerService.Labels["homeport.source"])
				}
			},
		},
		{
			name: "task definition with environment and ports",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1",
				Type: resource.TypeECSTaskDef,
				Name: "my-task",
				Config: map[string]interface{}{
					"family": "my-task",
					"container_definitions": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
							"environment": []interface{}{
								map[string]interface{}{
									"name":  "APP_ENV",
									"value": "production",
								},
							},
							"portMappings": []interface{}{
								map[string]interface{}{
									"containerPort": float64(80),
									"hostPort":      float64(8080),
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
				if result.DockerService.Environment["APP_ENV"] != "production" {
					t.Errorf("Environment APP_ENV = %v, want production", result.DockerService.Environment["APP_ENV"])
				}
				hasPort := false
				for _, p := range result.DockerService.Ports {
					if p == "8080:80" {
						hasPort = true
						break
					}
				}
				if !hasPort {
					t.Errorf("Expected port 8080:80, got %v", result.DockerService.Ports)
				}
			},
		},
		{
			name: "task definition with resource limits",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1",
				Type: resource.TypeECSTaskDef,
				Name: "my-task",
				Config: map[string]interface{}{
					"family": "my-task",
					"cpu":    float64(1024),
					"memory": float64(2048),
					"container_definitions": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService.Deploy == nil {
					t.Fatal("DockerService.Deploy is nil")
				}
				if result.DockerService.Deploy.Resources.Limits.CPUs != "1.00" {
					t.Errorf("CPUs = %v, want 1.00", result.DockerService.Deploy.Resources.Limits.CPUs)
				}
				if result.DockerService.Deploy.Resources.Limits.Memory != "2048M" {
					t.Errorf("Memory = %v, want 2048M", result.DockerService.Deploy.Resources.Limits.Memory)
				}
			},
		},
		{
			name: "task definition with multiple containers",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1",
				Type: resource.TypeECSTaskDef,
				Name: "my-task",
				Config: map[string]interface{}{
					"family": "my-task",
					"container_definitions": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
						},
						map[string]interface{}{
							"name":  "sidecar",
							"image": "envoyproxy/envoy:latest",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have a warning about multiple containers
				hasMultiContainerWarning := false
				for _, w := range result.Warnings {
					if len(w) > 0 && w[:20] == "Task definition has " {
						hasMultiContainerWarning = true
						break
					}
				}
				if !hasMultiContainerWarning {
					t.Error("Expected warning about multiple containers")
				}
			},
		},
		{
			name: "task definition with Fargate compatibility",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1",
				Type: resource.TypeECSTaskDef,
				Name: "my-task",
				Config: map[string]interface{}{
					"family":                   "my-task",
					"requires_compatibilities": "FARGATE",
					"container_definitions": []interface{}{
						map[string]interface{}{
							"name":  "web",
							"image": "nginx:1.21",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have a warning about Fargate
				hasFargateWarning := false
				for _, w := range result.Warnings {
					if w == "Task uses Fargate compatibility. Resource limits have been configured for Docker." {
						hasFargateWarning = true
						break
					}
				}
				if !hasFargateWarning {
					t.Error("Expected warning about Fargate compatibility")
				}
			},
		},
		{
			name: "task definition with no container definitions",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1",
				Type: resource.TypeECSTaskDef,
				Name: "my-task",
				Config: map[string]interface{}{
					"family": "my-task",
				},
			},
			wantErr: true,
		},
		{
			name: "task definition with empty container definitions",
			res: &resource.AWSResource{
				ID:   "arn:aws:ecs:us-east-1:123456789012:task-definition/my-task:1",
				Type: resource.TypeECSTaskDef,
				Name: "my-task",
				Config: map[string]interface{}{
					"family":                "my-task",
					"container_definitions": []interface{}{},
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
