package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudDNSConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudDNSMapper().Map(context.Background(), managedCloudDNSFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud DNS migration", result.ManualSteps)
	}
	if result.DockerService.Image != "coredns/coredns:1.11.1" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA CoreDNS target: %#v", result.DockerService)
	}
	for _, file := range []string{"coredns/Corefile", "coredns/example-com.zone", "config/cloud-dns/app-change.env", "config/cloud-dns/zone-report.yaml"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cloud-dns/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_DNS_ZONE=prod-zone", "TARGET_DNS_ENDPOINT=prod-zone:53"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	report := string(result.Configs["config/cloud-dns/zone-report.yaml"])
	for _, want := range []string{"google_dns_managed_zone", "dns_name: example.com.", "visibility: public"} {
		if !strings.Contains(report, want) {
			t.Fatalf("zone report missing %q:\n%s", want, report)
		}
	}
	for _, file := range []string{"migrate-dns-records.sh", "backup_cloud_dns.sh", "validate_cloud_dns.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-cloud-dns-zone":   domainrunbook.StepTypeCommand,
		"provision-coredns-zone":    domainrunbook.StepTypeCommand,
		"migrate-cloud-dns-records": domainrunbook.StepTypeCommand,
		"validate-coredns-zone":     domainrunbook.StepTypeCommand,
		"backup-cloud-dns-zone":     domainrunbook.StepTypeCommand,
		"cutover-cloud-dns-ns":      domainrunbook.StepTypeAPICall,
		"rollback-cloud-dns-zone":   domainrunbook.StepTypeRollback,
	} {
		if !hasCloudDNSRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewCloudDNSMapper(t *testing.T) {
	m := NewCloudDNSMapper()
	if m == nil {
		t.Fatal("NewCloudDNSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudDNS {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudDNS)
	}
}

func managedCloudDNSFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/managedZones/prod-zone",
		Type: resource.TypeCloudDNS,
		Name: "prod-zone",
		Config: map[string]interface{}{
			"name":        "prod-zone",
			"dns_name":    "example.com.",
			"description": "Production public zone",
			"visibility":  "public",
		},
	}
}

func hasCloudDNSRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestCloudDNSMapper_ResourceType(t *testing.T) {
	m := NewCloudDNSMapper()
	got := m.ResourceType()
	want := resource.TypeCloudDNS

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudDNSMapper_Dependencies(t *testing.T) {
	m := NewCloudDNSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudDNSMapper_Validate(t *testing.T) {
	m := NewCloudDNSMapper()

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
				Type: resource.TypeGCSBucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeCloudDNS,
				Name: "test-zone",
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

func TestCloudDNSMapper_Map(t *testing.T) {
	m := NewCloudDNSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Cloud DNS zone",
			res: &resource.AWSResource{
				ID:   "my-project/my-zone",
				Type: resource.TypeCloudDNS,
				Name: "my-zone",
				Config: map[string]interface{}{
					"name":     "my-zone",
					"dns_name": "example.com.",
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
				if result.DockerService.Image != "coredns/coredns:1.11.1" {
					t.Errorf("Expected CoreDNS image, got %s", result.DockerService.Image)
				}
			},
		},
		{
			name: "Cloud DNS with DNSSEC",
			res: &resource.AWSResource{
				ID:   "my-project/secure-zone",
				Type: resource.TypeCloudDNS,
				Name: "secure-zone",
				Config: map[string]interface{}{
					"name":     "secure-zone",
					"dns_name": "secure.example.com.",
					"dnssec_config": map[string]interface{}{
						"state": "on",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about DNSSEC
				hasDNSSECWarning := false
				for _, w := range result.Warnings {
					if containsDNS(w, "DNSSEC") {
						hasDNSSECWarning = true
						break
					}
				}
				if !hasDNSSECWarning {
					t.Error("Expected warning about DNSSEC")
				}
			},
		},
		{
			name: "Cloud DNS private zone",
			res: &resource.AWSResource{
				ID:   "my-project/private-zone",
				Type: resource.TypeCloudDNS,
				Name: "private-zone",
				Config: map[string]interface{}{
					"name":       "private-zone",
					"dns_name":   "internal.example.com.",
					"visibility": "private",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about private zone
				hasPrivateWarning := false
				for _, w := range result.Warnings {
					if containsDNS(w, "Private DNS zone") {
						hasPrivateWarning = true
						break
					}
				}
				if !hasPrivateWarning {
					t.Error("Expected warning about private zone")
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

func TestCloudDNSMapper_sanitizeZoneName(t *testing.T) {
	m := NewCloudDNSMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"example.com.", "example-com"},
		{"sub.example.com.", "sub-example-com"},
		{"test.", "test"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeZoneName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeZoneName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCloudDNSMapper_sanitizeName(t *testing.T) {
	m := NewCloudDNSMapper()

	tests := []struct {
		input string
		want  string
	}{
		{"my-zone", "my-zone"},
		{"MY_ZONE", "my-zone"},
		{"my zone", "my-zone"},
		{"example.com", "example-com"},
		{"123zone", "zone"},
		{"", "dns"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := m.sanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// containsDNS is a helper to check if a string contains a substring
func containsDNS(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
