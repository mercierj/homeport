package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestNewFirewallMapper(t *testing.T) {
	m := NewFirewallMapper()
	if m == nil {
		t.Fatal("NewFirewallMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureFirewall {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureFirewall)
	}
}

func TestFirewallMapper_ResourceType(t *testing.T) {
	m := NewFirewallMapper()
	got := m.ResourceType()
	want := resource.TypeAzureFirewall

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestFirewallConformanceManagedAToZ(t *testing.T) {
	result, err := NewFirewallMapper().Map(context.Background(), managedFirewallFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Azure Firewall migration", result.ManualSteps)
	}
	if result.DockerService.Image != "opnsense/opnsense:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA firewall target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/opnsense/config.xml", "config/firewall/nftables.conf", "config/firewall/suricata.yaml", "config/firewall/app-change.env", "config/firewall/generated-policy.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/firewall/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_AZURE_FIREWALL=checkout-fw", "FIREWALL_TARGET=opnsense"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"setup_iptables.sh", "validate_firewall.sh", "backup_firewall_config.sh", "cutover_firewall_policy.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"render-network-config":             domainrunbook.StepTypeCommand,
		"render-firewall-rules":             domainrunbook.StepTypeCommand,
		"validate-network-flows":            domainrunbook.StepTypeCommand,
		"backup-firewall-config":            domainrunbook.StepTypeCommand,
		"cutover-firewall-policy":           domainrunbook.StepTypeAPICall,
		"rollback-network-source-authority": domainrunbook.StepTypeRollback,
	} {
		if !hasFirewallRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedFirewallFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Network/azureFirewalls/checkout-fw",
		Type: resource.TypeAzureFirewall,
		Name: "checkout-fw",
		Config: map[string]interface{}{
			"name":              "checkout-fw",
			"sku_tier":          "Premium",
			"threat_intel_mode": "Alert",
		},
	}
}

func hasFirewallRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestFirewallMapper_Dependencies(t *testing.T) {
	m := NewFirewallMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestFirewallMapper_Validate(t *testing.T) {
	m := NewFirewallMapper()

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
				Type: resource.TypeAzureFirewall,
				Name: "test-firewall",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeAzureFirewall,
				Name: "test-firewall",
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

func TestFirewallMapper_Map(t *testing.T) {
	m := NewFirewallMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Azure Firewall",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/azureFirewalls/my-fw",
				Type: resource.TypeAzureFirewall,
				Name: "my-fw",
				Config: map[string]interface{}{
					"name":     "my-firewall",
					"sku_tier": "Standard",
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
			},
		},
		{
			name: "Premium tier Firewall",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/azureFirewalls/premium-fw",
				Type: resource.TypeAzureFirewall,
				Name: "premium-fw",
				Config: map[string]interface{}{
					"name":     "premium-firewall",
					"sku_tier": "Premium",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for Premium tier")
				}
			},
		},
		{
			name: "Firewall with threat intelligence",
			res: &resource.AWSResource{
				ID:   "/subscriptions/xxx/resourceGroups/rg/providers/Microsoft.Network/azureFirewalls/intel-fw",
				Type: resource.TypeAzureFirewall,
				Name: "intel-fw",
				Config: map[string]interface{}{
					"name":              "intel-firewall",
					"threat_intel_mode": "Alert",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if !result.HasWarnings() {
					t.Error("Expected warnings for threat intelligence")
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
