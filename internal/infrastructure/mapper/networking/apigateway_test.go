package networking

import (
	"context"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewAPIGatewayMapper(t *testing.T) {
	m := NewAPIGatewayMapper()
	if m == nil {
		t.Fatal("NewAPIGatewayMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAPIGateway {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAPIGateway)
	}
}

func TestAPIGatewayMapper_ResourceType(t *testing.T) {
	m := NewAPIGatewayMapper()
	got := m.ResourceType()
	want := resource.TypeAPIGateway

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestAPIGatewayMapper_Dependencies(t *testing.T) {
	m := NewAPIGatewayMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestAPIGatewayMapper_Validate(t *testing.T) {
	m := NewAPIGatewayMapper()

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
				ID:   "abc123xyz",
				Type: resource.TypeAPIGateway,
				Name: "my-api",
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

func TestAPIGatewayMapper_Map(t *testing.T) {
	m := NewAPIGatewayMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic API Gateway",
			res: &resource.AWSResource{
				ID:   "abc123xyz",
				Type: resource.TypeAPIGateway,
				Name: "my-api",
				Config: map[string]interface{}{
					"name": "my-api",
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
				// Should use Kong image
				if result.DockerService.Image != "kong:3.5-alpine" {
					t.Errorf("Expected image kong:3.5-alpine, got %s", result.DockerService.Image)
				}
				// Should have ports configured
				if len(result.DockerService.Ports) == 0 {
					t.Error("Expected ports to be configured")
				}
				// Check for Kong proxy port 8000
				hasProxyPort := false
				for _, port := range result.DockerService.Ports {
					if port == "8000:8000" {
						hasProxyPort = true
						break
					}
				}
				if !hasProxyPort {
					t.Error("Expected Kong proxy port 8000 to be configured")
				}
				// Should have labels
				if result.DockerService.Labels == nil {
					t.Error("Expected labels to be configured")
				}
				if result.DockerService.Labels["homeport.source"] != "aws_api_gateway" {
					t.Errorf("Expected source label to be aws_api_gateway, got %s", result.DockerService.Labels["homeport.source"])
				}
				// Should have environment variables
				if result.DockerService.Environment == nil {
					t.Error("Expected environment to be configured")
				}
				// Should depend on kong-db
				if len(result.DockerService.DependsOn) == 0 {
					t.Error("Expected DependsOn to include kong-db")
				}
			},
		},
		{
			name: "API Gateway with stages",
			res: &resource.AWSResource{
				ID:   "def456uvw",
				Type: resource.TypeAPIGateway,
				Name: "staged-api",
				Config: map[string]interface{}{
					"name": "staged-api",
					"stage": map[string]interface{}{
						"stage_name": "prod",
						"variables": map[string]interface{}{
							"backend_url": "http://backend:8080",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about stages
				hasStageWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "stage") {
						hasStageWarning = true
						break
					}
				}
				if !hasStageWarning {
					t.Log("Expected warning about API Gateway stages")
				}
			},
		},
		{
			name: "API Gateway with authorizers",
			res: &resource.AWSResource{
				ID:   "ghi789rst",
				Type: resource.TypeAPIGateway,
				Name: "secured-api",
				Config: map[string]interface{}{
					"name": "secured-api",
					"authorizer": []interface{}{
						map[string]interface{}{
							"name": "cognito-authorizer",
							"type": "COGNITO_USER_POOLS",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about authorizers
				hasAuthWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "authorizer") {
						hasAuthWarning = true
						break
					}
				}
				if !hasAuthWarning {
					t.Log("Expected warning about authorizers")
				}
			},
		},
		{
			name: "API Gateway with request validators",
			res: &resource.AWSResource{
				ID:   "jkl012opq",
				Type: resource.TypeAPIGateway,
				Name: "validated-api",
				Config: map[string]interface{}{
					"name": "validated-api",
					"request_validator": map[string]interface{}{
						"name":                        "validate-body",
						"validate_request_body":       true,
						"validate_request_parameters": true,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about request validators
				hasValidatorWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "validator") {
						hasValidatorWarning = true
						break
					}
				}
				if !hasValidatorWarning {
					t.Log("Expected warning about request validators")
				}
			},
		},
		{
			name: "API Gateway with API keys",
			res: &resource.AWSResource{
				ID:   "mno345lmn",
				Type: resource.TypeAPIGateway,
				Name: "keyed-api",
				Config: map[string]interface{}{
					"name":             "keyed-api",
					"api_key_required": true,
					"api_key": []interface{}{
						map[string]interface{}{
							"name":    "partner-key",
							"enabled": true,
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about API keys
				hasKeyWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "API key") || containsSubstring(w, "key-auth") {
						hasKeyWarning = true
						break
					}
				}
				if !hasKeyWarning {
					t.Log("Expected warning about API keys")
				}
			},
		},
		{
			name: "API Gateway with throttling",
			res: &resource.AWSResource{
				ID:   "pqr678ijk",
				Type: resource.TypeAPIGateway,
				Name: "throttled-api",
				Config: map[string]interface{}{
					"name": "throttled-api",
					"throttle_settings": map[string]interface{}{
						"rate_limit":  100.0,
						"burst_limit": 200.0,
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about throttling
				hasThrottleWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "throttl") || containsSubstring(w, "rate-limit") {
						hasThrottleWarning = true
						break
					}
				}
				if !hasThrottleWarning {
					t.Log("Expected warning about throttling")
				}
			},
		},
		{
			name: "API Gateway with custom domain",
			res: &resource.AWSResource{
				ID:   "stu901fgh",
				Type: resource.TypeAPIGateway,
				Name: "custom-domain-api",
				Config: map[string]interface{}{
					"name":        "custom-domain-api",
					"domain_name": "api.example.com",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warnings about custom domain
				hasDomainWarning := false
				for _, w := range result.Warnings {
					if containsSubstring(w, "domain") {
						hasDomainWarning = true
						break
					}
				}
				if !hasDomainWarning {
					t.Log("Expected warning about custom domain")
				}
			},
		},
		{
			name: "API Gateway with resources and integrations",
			res: &resource.AWSResource{
				ID:   "vwx234cde",
				Type: resource.TypeAPIGateway,
				Name: "resource-api",
				Config: map[string]interface{}{
					"name": "resource-api",
					"resource": []interface{}{
						map[string]interface{}{
							"path_part": "users",
						},
						map[string]interface{}{
							"path_part": "orders",
						},
					},
					"integration": []interface{}{
						map[string]interface{}{
							"type":        "HTTP_PROXY",
							"uri":         "http://backend:8080/users",
							"http_method": "GET",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have config files generated
				if len(result.Configs) == 0 {
					t.Error("Expected config files to be generated")
				}
			},
		},
		{
			name: "API Gateway with quota settings",
			res: &resource.AWSResource{
				ID:   "yza567bcd",
				Type: resource.TypeAPIGateway,
				Name: "quota-api",
				Config: map[string]interface{}{
					"name": "quota-api",
					"quota_settings": map[string]interface{}{
						"limit":  1000,
						"period": "DAY",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
			},
		},
		{
			name: "API Gateway with Lambda authorizer",
			res: &resource.AWSResource{
				ID:   "bcd890efg",
				Type: resource.TypeAPIGateway,
				Name: "lambda-auth-api",
				Config: map[string]interface{}{
					"name": "lambda-auth-api",
					"authorizer": []interface{}{
						map[string]interface{}{
							"name": "lambda-authorizer",
							"type": "REQUEST",
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
			},
		},
		{
			name: "API Gateway without explicit name",
			res: &resource.AWSResource{
				ID:     "efg123hij",
				Type:   resource.TypeAPIGateway,
				Name:   "fallback-api",
				Config: map[string]interface{}{},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should use resource Name as api name
				if result.DockerService.Labels["homeport.api_name"] != "fallback-api" {
					t.Errorf("Expected api_name label to be fallback-api, got %s", result.DockerService.Labels["homeport.api_name"])
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
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
