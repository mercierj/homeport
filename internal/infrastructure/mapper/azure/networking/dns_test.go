package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewDNSMapper(t *testing.T) {
	m := NewDNSMapper()
	if m == nil {
		t.Fatal("NewDNSMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureDNS {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureDNS)
	}
}

func TestDNSMapper_ResourceType(t *testing.T) {
	m := NewDNSMapper()
	got := m.ResourceType()
	want := resource.TypeAzureDNS

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestDNSConformanceManagedAToZ(t *testing.T) {
	result, err := NewDNSMapper().Map(context.Background(), managedDNSFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure DNS migration", result.ManualSteps)
	}
	if result.DockerService.Image != "coredns/coredns:1.11.1" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA CoreDNS target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/coredns/Corefile", "config/coredns/example.com.zone", "config/dns/app-change.env", "config/dns/generated-zone.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/dns/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_DNS_ZONE=example.com", "COREDNS_ENDPOINT=coredns:53"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_dns.sh", "export_dns_zone.sh", "validate_dns.sh", "backup_dns_zone.sh", "cutover_dns.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-dns-zone":               domainrunbook.StepTypeCommand,
		"publish-external-dns-records":  domainrunbook.StepTypeCommand,
		"poll-public-dns":               domainrunbook.StepTypeCommand,
		"backup-dns-zone":               domainrunbook.StepTypeCommand,
		"rollback-dns-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasDNSRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedDNSFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Network/dnsZones/example.com",
		Type: resource.TypeAzureDNS,
		Name: "example.com",
		Config: map[string]interface{}{
			"name":                  "example.com",
			"number_of_record_sets": float64(4),
		},
	}
}

func hasDNSRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestDNSMapper_Dependencies(t *testing.T) {
	m := NewDNSMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestDNSMapper_Validate(t *testing.T) {
	m := NewDNSMapper()

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
				Type: resource.TypeAzureDNS,
				Name: "test-dns",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureDNS,
				Name: "test-dns",
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

func TestDNSMapper_Map(t *testing.T) {
	m := NewDNSMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic DNS zone",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/dnsZones/example.com",
				Type: resource.TypeAzureDNS,
				Name: "example.com",
				Config: map[string]interface{}{
					"name": "example.com",
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
			name: "DNS zone with record sets",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/dnsZones/myzone.com",
				Type: resource.TypeAzureDNS,
				Name: "myzone.com",
				Config: map[string]interface{}{
					"name":                  "myzone.com",
					"number_of_record_sets": float64(25),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for record sets")
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
