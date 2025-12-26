package networking

import (
	"context"
	"testing"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

func TestNewCDNMapper(t *testing.T) {
	m := NewCDNMapper()
	if m == nil {
		t.Fatal("NewCDNMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureCDN {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureCDN)
	}
}

func TestCDNMapper_ResourceType(t *testing.T) {
	m := NewCDNMapper()
	got := m.ResourceType()
	want := resource.TypeAzureCDN

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCDNMapper_Dependencies(t *testing.T) {
	m := NewCDNMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCDNMapper_Validate(t *testing.T) {
	m := NewCDNMapper()

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
				Type: resource.TypeAzureCDN,
				Name: "test-cdn",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureCDN,
				Name: "test-cdn",
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

func TestCDNMapper_Map(t *testing.T) {
	m := NewCDNMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic CDN profile",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Cdn/profiles/my-cdn",
				Type: resource.TypeAzureCDN,
				Name: "my-cdn",
				Config: map[string]interface{}{
					"name": "my-cdn-profile",
					"sku":  "Standard_Microsoft",
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
			},
		},
		{
			name: "CDN with endpoint",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Cdn/profiles/my-cdn",
				Type: resource.TypeAzureCDN,
				Name: "my-cdn",
				Config: map[string]interface{}{
					"name": "my-cdn-profile",
					"sku":  "Standard_Verizon",
					"endpoint": []interface{}{
						map[string]interface{}{
							"name":             "my-endpoint",
							"origin_host_name": "origin.example.com",
							"origin_path":      "/content",
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
					t.Error("Expected warnings for CDN endpoint")
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
