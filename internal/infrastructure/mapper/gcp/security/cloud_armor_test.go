package security

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

func TestNewCloudArmorMapper(t *testing.T) {
	m := NewCloudArmorMapper()
	if m == nil {
		t.Fatal("NewCloudArmorMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeCloudArmor {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeCloudArmor)
	}
}

func TestCloudArmorMapper_ResourceType(t *testing.T) {
	m := NewCloudArmorMapper()
	got := m.ResourceType()
	want := resource.TypeCloudArmor

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestCloudArmorMapper_Dependencies(t *testing.T) {
	m := NewCloudArmorMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestCloudArmorMapper_Validate(t *testing.T) {
	m := NewCloudArmorMapper()

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
				Type: resource.TypeCloudArmor,
				Name: "test-policy",
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

func TestCloudArmorMapper_Map(t *testing.T) {
	m := NewCloudArmorMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Cloud Armor policy",
			res: &resource.AWSResource{
				ID:   "my-policy",
				Type: resource.TypeCloudArmor,
				Name: "my-policy",
				Config: map[string]interface{}{
					"name":        "my-policy",
					"description": "Test security policy",
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
				if !strings.Contains(result.DockerService.Image, "modsecurity") {
					t.Errorf("Expected ModSecurity image, got %s", result.DockerService.Image)
				}
				// Check labels
				if result.DockerService.Labels["homeport.policy_name"] != "my-policy" {
					t.Errorf("Expected policy_name label, got %s", result.DockerService.Labels["homeport.policy_name"])
				}
			},
		},
		{
			name: "Cloud Armor policy with IP deny rule",
			res: &resource.AWSResource{
				ID:   "ip-block-policy",
				Type: resource.TypeCloudArmor,
				Name: "ip-block-policy",
				Config: map[string]interface{}{
					"name": "ip-block-policy",
					"rule": []interface{}{
						map[string]interface{}{
							"action":      "deny(403)",
							"priority":    1000,
							"description": "Block bad IPs",
							"match": map[string]interface{}{
								"config": map[string]interface{}{
									"src_ip_ranges": []interface{}{"192.168.1.100", "10.0.0.0/8"},
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
				// Should have config files generated including IP blacklist
				hasIPBlacklist := false
				for path := range result.Configs {
					if strings.Contains(path, "ip-blacklist") {
						hasIPBlacklist = true
						break
					}
				}
				if !hasIPBlacklist {
					t.Error("Expected IP blacklist config file")
				}
			},
		},
		{
			name: "Cloud Armor policy with IP allow rule",
			res: &resource.AWSResource{
				ID:   "ip-allow-policy",
				Type: resource.TypeCloudArmor,
				Name: "ip-allow-policy",
				Config: map[string]interface{}{
					"name": "ip-allow-policy",
					"rule": []interface{}{
						map[string]interface{}{
							"action":      "allow",
							"priority":    1000,
							"description": "Allow trusted IPs",
							"match": map[string]interface{}{
								"config": map[string]interface{}{
									"src_ip_ranges": []interface{}{"10.0.0.0/8"},
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
				// Should have IP whitelist
				hasIPWhitelist := false
				for path := range result.Configs {
					if strings.Contains(path, "ip-whitelist") {
						hasIPWhitelist = true
						break
					}
				}
				if !hasIPWhitelist {
					t.Error("Expected IP whitelist config file")
				}
			},
		},
		{
			name: "Cloud Armor policy with expression-based rule",
			res: &resource.AWSResource{
				ID:   "expr-policy",
				Type: resource.TypeCloudArmor,
				Name: "expr-policy",
				Config: map[string]interface{}{
					"name": "expr-policy",
					"rule": []interface{}{
						map[string]interface{}{
							"action":      "deny(403)",
							"priority":    2000,
							"description": "Block SQL injection",
							"match": map[string]interface{}{
								"expr": map[string]interface{}{
									"expression": "evaluatePreconfiguredExpr('sqli-stable')",
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
				// Should have ModSecurity rules generated
				hasModSecRules := false
				for path := range result.Configs {
					if strings.Contains(path, "cloud-armor-custom.conf") {
						hasModSecRules = true
						break
					}
				}
				if !hasModSecRules {
					t.Error("Expected ModSecurity rules config file")
				}
			},
		},
		{
			name: "Cloud Armor policy with rate limiting",
			res: &resource.AWSResource{
				ID:   "rate-limit-policy",
				Type: resource.TypeCloudArmor,
				Name: "rate-limit-policy",
				Config: map[string]interface{}{
					"name": "rate-limit-policy",
					"rule": []interface{}{
						map[string]interface{}{
							"action":      "throttle",
							"priority":    3000,
							"description": "Rate limit API",
							"rate_limit_options": map[string]interface{}{
								"rate_limit_http_request_count":       100.0,
								"rate_limit_http_request_interval_sec": 60.0,
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
				// Should have nginx config with rate limiting
				hasNginxConfig := false
				for path := range result.Configs {
					if strings.Contains(path, "nginx.conf") {
						hasNginxConfig = true
						break
					}
				}
				if !hasNginxConfig {
					t.Error("Expected nginx config file")
				}
			},
		},
		{
			name: "Cloud Armor policy with adaptive protection",
			res: &resource.AWSResource{
				ID:   "adaptive-policy",
				Type: resource.TypeCloudArmor,
				Name: "adaptive-policy",
				Config: map[string]interface{}{
					"name": "adaptive-policy",
					"adaptive_protection_config": map[string]interface{}{
						"layer_7_ddos_defense_config": map[string]interface{}{
							"enable": true,
						},
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				// Should have warning about adaptive protection
				hasAdaptiveWarning := false
				for _, w := range result.Warnings {
					if strings.Contains(strings.ToLower(w), "adaptive") {
						hasAdaptiveWarning = true
						break
					}
				}
				if !hasAdaptiveWarning {
					t.Error("Expected warning about adaptive protection")
				}
				// Should have fail2ban config
				hasFail2ban := false
				for path := range result.Configs {
					if strings.Contains(path, "fail2ban") {
						hasFail2ban = true
						break
					}
				}
				if !hasFail2ban {
					t.Error("Expected fail2ban config for adaptive protection")
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

func TestCloudArmorMapper_generateModSecurityRules(t *testing.T) {
	m := NewCloudArmorMapper()

	res := &resource.AWSResource{
		ID:   "test-policy",
		Type: resource.TypeCloudArmor,
		Config: map[string]interface{}{
			"name": "test-policy",
			"rule": []interface{}{
				map[string]interface{}{
					"action":      "deny(403)",
					"priority":    1000,
					"description": "Test rule",
					"match": map[string]interface{}{
						"config": map[string]interface{}{
							"src_ip_ranges": []interface{}{"192.168.1.0/24"},
						},
					},
				},
			},
		},
	}

	rules := m.generateModSecurityRules(res)

	if rules == "" {
		t.Error("generateModSecurityRules returned empty string")
	}
	if !strings.Contains(rules, "Cloud Armor to ModSecurity") {
		t.Error("Rules should contain header comment")
	}
	if !strings.Contains(rules, "Priority: 1000") {
		t.Error("Rules should contain priority comment")
	}
}

func TestCloudArmorMapper_generateNginxConfig(t *testing.T) {
	m := NewCloudArmorMapper()

	config := m.generateNginxConfig("test-policy")

	if config == "" {
		t.Error("generateNginxConfig returned empty string")
	}
	if !strings.Contains(config, "test-policy") {
		t.Error("Config should contain policy name")
	}
	if !strings.Contains(config, "modsecurity on") {
		t.Error("Config should enable ModSecurity")
	}
	if !strings.Contains(config, "limit_req") {
		t.Error("Config should contain rate limiting")
	}
	if !strings.Contains(config, "healthz") {
		t.Error("Config should contain health check endpoint")
	}
}

func TestCloudArmorMapper_generateSetupScript(t *testing.T) {
	m := NewCloudArmorMapper()

	script := m.generateSetupScript("test-policy")

	if script == "" {
		t.Error("generateSetupScript returned empty string")
	}
	if !strings.Contains(script, "test-policy") {
		t.Error("Script should contain policy name")
	}
	if !strings.Contains(script, "mkdir") {
		t.Error("Script should create directories")
	}
}

func TestCloudArmorMapper_generateMigrationScript(t *testing.T) {
	m := NewCloudArmorMapper()

	script := m.generateMigrationScript("test-policy")

	if script == "" {
		t.Error("generateMigrationScript returned empty string")
	}
	if !strings.Contains(script, "test-policy") {
		t.Error("Script should contain policy name")
	}
	if !strings.Contains(script, "gcloud") {
		t.Error("Script should contain gcloud commands")
	}
	if !strings.Contains(script, "curl") {
		t.Error("Script should contain test commands")
	}
}

func TestCloudArmorMapper_generateIPLists(t *testing.T) {
	m := NewCloudArmorMapper()

	rules := []interface{}{
		map[string]interface{}{
			"action": "deny(403)",
			"match": map[string]interface{}{
				"config": map[string]interface{}{
					"src_ip_ranges": []interface{}{"192.168.1.100", "10.0.0.0/8"},
				},
			},
		},
		map[string]interface{}{
			"action": "allow",
			"match": map[string]interface{}{
				"config": map[string]interface{}{
					"src_ip_ranges": []interface{}{"172.16.0.0/12"},
				},
			},
		},
	}

	ipLists := m.generateIPLists(rules)

	// Check blacklist
	if !strings.Contains(ipLists.blacklist, "192.168.1.100") {
		t.Error("Blacklist should contain denied IP")
	}
	if !strings.Contains(ipLists.blacklist, "10.0.0.0/8") {
		t.Error("Blacklist should contain denied range")
	}

	// Check whitelist
	if !strings.Contains(ipLists.whitelist, "172.16.0.0/12") {
		t.Error("Whitelist should contain allowed range")
	}
}

func TestCloudArmorMapper_convertExpressionToModSec(t *testing.T) {
	m := NewCloudArmorMapper()

	tests := []struct {
		expression string
		action     string
		contains   []string
	}{
		{
			expression: "origin.region_code != 'US'",
			action:     "deny",
			contains:   []string{"Geographic restriction", "GEO:COUNTRY_CODE"},
		},
		{
			expression: "evaluatePreconfiguredExpr('sqli-stable')",
			action:     "deny",
			contains:   []string{"Preconfigured rule", "OWASP CRS"},
		},
		{
			expression: "request.headers['X-Custom'] == 'bad'",
			action:     "deny",
			contains:   []string{"Header inspection"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.expression, func(t *testing.T) {
			result := m.convertExpressionToModSec(100001, tt.expression, tt.action)

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected result to contain '%s', got: %s", expected, result)
				}
			}
		})
	}
}

func TestCloudArmorMapper_getDefaultRules(t *testing.T) {
	m := NewCloudArmorMapper()

	rules := m.getDefaultRules()

	if rules == "" {
		t.Error("getDefaultRules returned empty string")
	}
	if !strings.Contains(rules, "SecRule") {
		t.Error("Default rules should contain ModSecurity rules")
	}
	if !strings.Contains(rules, "passwd") {
		t.Error("Default rules should contain path traversal protection")
	}
}
