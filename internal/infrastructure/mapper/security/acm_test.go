package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewACMMapper(t *testing.T) {
	m := NewACMMapper()
	if m == nil {
		t.Fatal("NewACMMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeACMCertificate {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeACMCertificate)
	}
}

func TestACMMapper_ResourceType(t *testing.T) {
	m := NewACMMapper()
	got := m.ResourceType()
	want := resource.TypeACMCertificate

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestACMMapper_Dependencies(t *testing.T) {
	m := NewACMMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestACMMapper_Validate(t *testing.T) {
	m := NewACMMapper()

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
				Type: resource.TypeACMCertificate,
				Name: "test-cert",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeACMCertificate,
				Name: "test-cert",
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

func TestACMMapper_Map(t *testing.T) {
	m := NewACMMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic ACM certificate",
			res: &resource.AWSResource{
				ID:   "arn:aws:acm:us-east-1:123456789012:certificate/abc-123",
				Type: resource.TypeACMCertificate,
				Name: "example.com",
				Config: map[string]interface{}{
					"domain_name": "example.com",
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
				// Should use Traefik image
				if result.DockerService.Image != "traefik:v3.0" {
					t.Errorf("Expected Traefik image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "ACM certificate with SANs",
			res: &resource.AWSResource{
				ID:   "arn:aws:acm:us-east-1:123456789012:certificate/def-456",
				Type: resource.TypeACMCertificate,
				Name: "example.com",
				Config: map[string]interface{}{
					"domain_name": "example.com",
					"subject_alternative_names": []interface{}{
						"www.example.com",
						"api.example.com",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about SANs
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about SANs")
				}
			},
		},
		{
			name: "ACM certificate with DNS validation",
			res: &resource.AWSResource{
				ID:   "arn:aws:acm:us-east-1:123456789012:certificate/ghi-789",
				Type: resource.TypeACMCertificate,
				Name: "example.com",
				Config: map[string]interface{}{
					"domain_name":       "example.com",
					"validation_method": "DNS",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about DNS validation
				if len(result.Warnings) == 0 {
					t.Log("Expected warning about DNS validation")
				}
			},
		},
		{
			name: "ACM certificate with EMAIL validation",
			res: &resource.AWSResource{
				ID:   "arn:aws:acm:us-east-1:123456789012:certificate/jkl-012",
				Type: resource.TypeACMCertificate,
				Name: "example.com",
				Config: map[string]interface{}{
					"domain_name":       "example.com",
					"validation_method": "EMAIL",
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

func TestACMMapper_extractSANs(t *testing.T) {
	m := NewACMMapper()

	tests := []struct {
		name   string
		config map[string]interface{}
		want   int
	}{
		{
			name:   "no SANs",
			config: map[string]interface{}{},
			want:   0,
		},
		{
			name: "with SANs",
			config: map[string]interface{}{
				"subject_alternative_names": []interface{}{
					"www.example.com",
					"api.example.com",
				},
			},
			want: 2,
		},
		{
			name: "empty SANs",
			config: map[string]interface{}{
				"subject_alternative_names": []interface{}{},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &resource.AWSResource{
				ID:     "test",
				Type:   resource.TypeACMCertificate,
				Name:   "test",
				Config: tt.config,
			}
			got := m.extractSANs(res)
			if len(got) != tt.want {
				t.Errorf("extractSANs() returned %d SANs, want %d", len(got), tt.want)
			}
		})
	}
}

func TestACMMapper_MapBuildsManagedDNSChallengeWhenProviderIsKnown(t *testing.T) {
	result, err := NewACMMapper().Map(context.Background(), managedACMFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want none when DNS challenge provider is known", result.ManualSteps)
	}
	static := string(result.Configs["config/traefik/traefik.yml"])
	for _, want := range []string{
		"email: ops@example.com",
		"dnsChallenge:",
		"provider: route53",
	} {
		if !strings.Contains(static, want) {
			t.Fatalf("static config missing %q:\n%s", want, static)
		}
	}
	dynamic := string(result.Configs["config/traefik/dynamic.yml"])
	for _, want := range []string{
		"main: \"example.com\"",
		"www.example.com",
		"api.example.com",
	} {
		if !strings.Contains(dynamic, want) {
			t.Fatalf("dynamic config missing %q:\n%s", want, dynamic)
		}
	}
}

func TestACMConformanceManagedAToZ(t *testing.T) {
	result, err := NewACMMapper().Map(context.Background(), managedACMFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want fully generated certificate migration", result.ManualSteps)
	}
	if result.DockerService.Image != "traefik:v3.0" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA certificate proxy: %#v", result.DockerService)
	}
	if result.DockerService.HealthCheck == nil {
		t.Fatal("missing Traefik health check")
	}
	for _, file := range []string{"config/traefik/traefik.yml", "config/traefik/dynamic.yml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing %s", file)
		}
	}
	if _, ok := result.Scripts["setup_certificates.sh"]; !ok {
		t.Fatal("missing setup script")
	}
	if _, ok := result.Scripts["backup_acm_config.sh"]; !ok {
		t.Fatal("missing backup script")
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"provision-acme-dns-challenge":          domainrunbook.StepTypeCommand,
		"validate-certificate-renewal":          domainrunbook.StepTypeCommand,
		"backup-certificate-config":             domainrunbook.StepTypeCommand,
		"cutover-tls-termination-to-traefik":    domainrunbook.StepTypeDNSCheck,
		"rollback-certificate-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasACMRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedACMFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "arn:aws:acm:us-east-1:123456789012:certificate/abc-123",
		Type: resource.TypeACMCertificate,
		Name: "example.com",
		Config: map[string]interface{}{
			"domain_name":            "example.com",
			"acme_email":             "ops@example.com",
			"dns_challenge_provider": "route53",
			"validation_method":      "DNS",
			"subject_alternative_names": []interface{}{
				"www.example.com",
				"api.example.com",
			},
		},
	}
}

func hasACMRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
