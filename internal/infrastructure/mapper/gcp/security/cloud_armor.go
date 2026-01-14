// Package security provides mappers for GCP security services.
package security

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// CloudArmorMapper converts GCP Cloud Armor security policies to ModSecurity + nginx.
type CloudArmorMapper struct {
	*mapper.BaseMapper
}

// NewCloudArmorMapper creates a new Cloud Armor to ModSecurity mapper.
func NewCloudArmorMapper() *CloudArmorMapper {
	return &CloudArmorMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudArmor, nil),
	}
}

// Map converts a GCP Cloud Armor security policy to ModSecurity/nginx configuration.
func (m *CloudArmorMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	policyName := res.GetConfigString("name")
	if policyName == "" {
		policyName = res.Name
	}

	result := mapper.NewMappingResult("nginx-modsecurity")
	svc := result.DockerService

	// Use nginx with ModSecurity (OWASP Core Rule Set)
	svc.Image = "owasp/modsecurity-crs:nginx-alpine"
	svc.Environment = map[string]string{
		"MODSEC_RULE_ENGINE":          "On",
		"MODSEC_AUDIT_LOG":            "/var/log/modsec_audit.log",
		"MODSEC_AUDIT_LOG_FORMAT":     "JSON",
		"PARANOIA":                    "1",
		"ANOMALY_INBOUND":             "5",
		"ANOMALY_OUTBOUND":            "4",
		"BLOCKING_PARANOIA":           "1",
		"EXECUTING_PARANOIA":          "1",
		"DETECTION_PARANOIA":          "1",
		"BACKEND":                     "http://upstream:8080",
		"PORT":                        "8080",
		"PROXY_SSL":                   "off",
		"MODSEC_RESP_BODY_ACCESS":     "On",
		"MODSEC_RESP_BODY_MIMETYPE":   "text/plain text/html text/xml application/json",
		"ALLOWED_METHODS":             "GET HEAD POST OPTIONS PUT PATCH DELETE",
		"ALLOWED_REQUEST_CONTENT_TYPE": "application/x-www-form-urlencoded|multipart/form-data|text/xml|application/xml|application/json",
		"MAX_NUM_ARGS":                "255",
		"ARG_NAME_LENGTH":             "100",
		"ARG_LENGTH":                  "400",
		"TOTAL_ARG_LENGTH":            "64000",
		"MAX_FILE_SIZE":               "1048576",
		"COMBINED_FILE_SIZES":         "1048576",
	}
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{
		"./config/modsecurity/rules:/etc/modsecurity.d/owasp-crs/rules/custom",
		"./config/nginx/nginx.conf:/etc/nginx/nginx.conf:ro",
		"./logs/modsecurity:/var/log/modsecurity",
	}
	svc.Networks = []string{"homeport"}
	svc.Labels = map[string]string{
		"homeport.source":      "google_compute_security_policy",
		"homeport.policy_name": policyName,
		"traefik.enable":        "true",
	}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/healthz || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}

	// Generate ModSecurity rules from Cloud Armor rules
	modsecRules := m.generateModSecurityRules(res)
	result.AddConfig("config/modsecurity/rules/cloud-armor-custom.conf", []byte(modsecRules))

	// Generate nginx configuration
	nginxConfig := m.generateNginxConfig(policyName)
	result.AddConfig("config/nginx/nginx.conf", []byte(nginxConfig))

	// Generate IP whitelist/blacklist if present
	if rules := res.Config["rule"]; rules != nil {
		ipLists := m.generateIPLists(rules)
		if ipLists.whitelist != "" {
			result.AddConfig("config/modsecurity/rules/ip-whitelist.conf", []byte(ipLists.whitelist))
		}
		if ipLists.blacklist != "" {
			result.AddConfig("config/modsecurity/rules/ip-blacklist.conf", []byte(ipLists.blacklist))
		}
	}

	// Generate setup script
	setupScript := m.generateSetupScript(policyName)
	result.AddScript("setup_waf.sh", []byte(setupScript))

	// Generate migration script
	migrationScript := m.generateMigrationScript(policyName)
	result.AddScript("migrate_cloud_armor.sh", []byte(migrationScript))

	// Handle adaptive protection if enabled
	if adaptiveProtection := res.Config["adaptive_protection_config"]; adaptiveProtection != nil {
		m.handleAdaptiveProtection(adaptiveProtection, result)
	}

	// Add manual steps and warnings
	result.AddManualStep("Update BACKEND environment variable to point to your upstream service")
	result.AddManualStep("Review generated ModSecurity rules in config/modsecurity/rules/")
	result.AddManualStep("Adjust PARANOIA level based on your security requirements (1-4)")
	result.AddManualStep("Configure rate limiting if needed using nginx limit_req module")
	result.AddWarning("Cloud Armor adaptive protection requires manual security monitoring setup")
	result.AddWarning("Preconfigured WAF rules may have different false positive rates than Cloud Armor")
	result.AddWarning("Review and test all custom rules before deploying to production")

	return result, nil
}

func (m *CloudArmorMapper) generateModSecurityRules(res *resource.AWSResource) string {
	var rules strings.Builder

	rules.WriteString("# Cloud Armor to ModSecurity Rules\n")
	rules.WriteString("# Generated by Homeport\n")
	rules.WriteString("# Review and adjust these rules for your environment\n\n")

	// Process Cloud Armor rules
	if rulesList := res.Config["rule"]; rulesList != nil {
		if rulesSlice, ok := rulesList.([]interface{}); ok {
			for _, rule := range rulesSlice {
				if ruleMap, ok := rule.(map[string]interface{}); ok {
					ruleConfig := m.convertCloudArmorRule(ruleMap)
					rules.WriteString(ruleConfig)
					rules.WriteString("\n")
				}
			}
		}
	}

	// Add default rules
	rules.WriteString("\n# Default protection rules\n")
	rules.WriteString(m.getDefaultRules())

	return rules.String()
}

type ipListConfig struct {
	whitelist string
	blacklist string
}

func (m *CloudArmorMapper) generateIPLists(rules interface{}) ipListConfig {
	var whitelist, blacklist strings.Builder

	whitelist.WriteString("# IP Whitelist - Cloud Armor Migration\n")
	whitelist.WriteString("# Add allowed IP addresses/ranges\n\n")

	blacklist.WriteString("# IP Blacklist - Cloud Armor Migration\n")
	blacklist.WriteString("# Add blocked IP addresses/ranges\n\n")

	if rulesSlice, ok := rules.([]interface{}); ok {
		for _, rule := range rulesSlice {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				action := ""
				if a, ok := ruleMap["action"].(string); ok {
					action = strings.ToLower(a)
				}

				if match := ruleMap["match"]; match != nil {
					if matchMap, ok := match.(map[string]interface{}); ok {
						if config := matchMap["config"]; config != nil {
							if configMap, ok := config.(map[string]interface{}); ok {
								if srcIPRanges, ok := configMap["src_ip_ranges"].([]interface{}); ok {
									for _, ip := range srcIPRanges {
										if ipStr, ok := ip.(string); ok {
											switch action {
											case "allow":
												whitelist.WriteString(fmt.Sprintf("SecRule REMOTE_ADDR \"@ipMatch %s\" \"id:100001,phase:1,pass,nolog,ctl:ruleEngine=Off\"\n", ipStr))
											case "deny", "deny(403)", "deny(404)":
												blacklist.WriteString(fmt.Sprintf("SecRule REMOTE_ADDR \"@ipMatch %s\" \"id:100002,phase:1,deny,status:403,msg:'IP blocked by policy'\"\n", ipStr))
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return ipListConfig{
		whitelist: whitelist.String(),
		blacklist: blacklist.String(),
	}
}

func (m *CloudArmorMapper) convertCloudArmorRule(rule map[string]interface{}) string {
	var ruleConfig strings.Builder

	priority := 0
	if p, ok := rule["priority"].(float64); ok {
		priority = int(p)
	} else if p, ok := rule["priority"].(int); ok {
		priority = p
	}

	action := ""
	if a, ok := rule["action"].(string); ok {
		action = strings.ToLower(a)
	}

	description := ""
	if d, ok := rule["description"].(string); ok {
		description = d
	}

	ruleConfig.WriteString(fmt.Sprintf("# Priority: %d - %s\n", priority, description))

	// Generate rule ID based on priority
	ruleID := 200000 + priority

	// Process match conditions
	if match := rule["match"]; match != nil {
		if matchMap, ok := match.(map[string]interface{}); ok {
			// Handle expression-based rules
			if expr, ok := matchMap["expr"].(map[string]interface{}); ok {
				if expression, ok := expr["expression"].(string); ok {
					ruleConfig.WriteString(m.convertExpressionToModSec(ruleID, expression, action))
				}
			}

			// Handle versioned expression
			if vExpr, ok := matchMap["versioned_expr"].(string); ok {
				ruleConfig.WriteString(m.convertVersionedExprToModSec(ruleID, vExpr, action))
			}

			// Handle config-based rules (IP ranges, etc.)
			if config := matchMap["config"]; config != nil {
				if configMap, ok := config.(map[string]interface{}); ok {
					ruleConfig.WriteString(m.convertConfigToModSec(ruleID, configMap, action))
				}
			}
		}
	}

	// Handle rate limiting
	if rateLimit := rule["rate_limit_options"]; rateLimit != nil {
		ruleConfig.WriteString(m.convertRateLimitToNginx(rateLimit))
	}

	return ruleConfig.String()
}

func (m *CloudArmorMapper) convertExpressionToModSec(ruleID int, expression, action string) string {
	var rule strings.Builder

	modsecAction := "pass"
	if action == "deny" || strings.HasPrefix(action, "deny(") {
		modsecAction = "deny,status:403"
	}

	// Common Cloud Armor expressions to ModSecurity
	expr := strings.ToLower(expression)

	switch {
	case strings.Contains(expr, "origin.region_code"):
		// Geographic restriction
		rule.WriteString(fmt.Sprintf("# Geographic restriction: %s\n", expression))
		rule.WriteString(fmt.Sprintf("# Note: Requires GeoIP module. Original expression: %s\n", expression))
		rule.WriteString(fmt.Sprintf("SecRule GEO:COUNTRY_CODE \"!@streq US\" \"id:%d,phase:1,%s,msg:'Geographic restriction'\"\n", ruleID, modsecAction))

	case strings.Contains(expr, "request.headers"):
		// Header-based rule
		rule.WriteString(fmt.Sprintf("# Header inspection: %s\n", expression))
		rule.WriteString(fmt.Sprintf("SecRule REQUEST_HEADERS \".*\" \"id:%d,phase:1,%s,msg:'Header inspection rule'\"\n", ruleID, modsecAction))

	case strings.Contains(expr, "request.path"):
		// Path-based rule
		rule.WriteString(fmt.Sprintf("# Path-based rule: %s\n", expression))
		rule.WriteString(fmt.Sprintf("SecRule REQUEST_URI \".*\" \"id:%d,phase:1,%s,msg:'Path inspection rule'\"\n", ruleID, modsecAction))

	case strings.Contains(expr, "evaluatepreconfiguredexpr"):
		// Preconfigured WAF rule
		rule.WriteString(fmt.Sprintf("# Preconfigured rule: %s\n", expression))
		rule.WriteString("# OWASP CRS rules are enabled by default\n")

	default:
		rule.WriteString(fmt.Sprintf("# Custom expression (requires manual conversion): %s\n", expression))
		rule.WriteString(fmt.Sprintf("# SecRule ... \"id:%d,phase:1,%s,msg:'Custom rule'\"\n", ruleID, modsecAction))
	}

	return rule.String()
}

func (m *CloudArmorMapper) convertVersionedExprToModSec(ruleID int, versionedExpr, action string) string {
	var rule strings.Builder

	modsecAction := "pass"
	if action == "deny" || strings.HasPrefix(action, "deny(") {
		modsecAction = "deny,status:403"
	}

	expr := strings.ToLower(versionedExpr)

	switch {
	case strings.Contains(expr, "src_ips_v1"):
		rule.WriteString("# Source IP validation (configured in IP lists)\n")
	case strings.Contains(expr, "recaptcha"):
		rule.WriteString("# reCAPTCHA protection requires application-level implementation\n")
		rule.WriteString("# Consider using hCaptcha or similar self-hosted solution\n")
	default:
		rule.WriteString(fmt.Sprintf("# Versioned expression: %s\n", versionedExpr))
		rule.WriteString(fmt.Sprintf("# SecRule ... \"id:%d,phase:1,%s,msg:'Versioned expr rule'\"\n", ruleID, modsecAction))
	}

	return rule.String()
}

func (m *CloudArmorMapper) convertConfigToModSec(ruleID int, config map[string]interface{}, action string) string {
	var rule strings.Builder

	modsecAction := "pass"
	if action == "deny" || strings.HasPrefix(action, "deny(") {
		modsecAction = "deny,status:403"
	}

	// Handle source IP ranges (handled separately in IP lists)
	if srcIPRanges, ok := config["src_ip_ranges"].([]interface{}); ok && len(srcIPRanges) > 0 {
		rule.WriteString("# Source IP ranges configured in IP whitelist/blacklist files\n")
	}

	// Handle header configs
	if headers, ok := config["headers"].([]interface{}); ok {
		for i, h := range headers {
			if headerMap, ok := h.(map[string]interface{}); ok {
				headerName := ""
				headerValue := ""
				if n, ok := headerMap["name"].(string); ok {
					headerName = n
				}
				if v, ok := headerMap["value"].(string); ok {
					headerValue = v
				}
				rule.WriteString(fmt.Sprintf("SecRule REQUEST_HEADERS:%s \"@contains %s\" \"id:%d,phase:1,%s,msg:'Header match rule'\"\n",
					headerName, headerValue, ruleID+i, modsecAction))
			}
		}
	}

	return rule.String()
}

func (m *CloudArmorMapper) convertRateLimitToNginx(rateLimit interface{}) string {
	var config strings.Builder

	config.WriteString("# Rate limiting configuration for nginx\n")
	config.WriteString("# Add to nginx.conf http block:\n")

	if rlMap, ok := rateLimit.(map[string]interface{}); ok {
		// Default values
		rate := "10r/s"
		burst := 20

		if ratePerKey, ok := rlMap["rate_limit_http_request_count"].(float64); ok {
			rate = fmt.Sprintf("%dr/s", int(ratePerKey))
		}
		if ratePeriodSec, ok := rlMap["rate_limit_http_request_interval_sec"].(float64); ok {
			if ratePeriodSec > 0 {
				burst = int(ratePeriodSec * 10) // Estimate burst based on interval
			}
		}

		config.WriteString(fmt.Sprintf("# limit_req_zone $binary_remote_addr zone=cloud_armor_limit:10m rate=%s;\n", rate))
		config.WriteString("# In location block:\n")
		config.WriteString(fmt.Sprintf("# limit_req zone=cloud_armor_limit burst=%d nodelay;\n", burst))
	}

	return config.String()
}

func (m *CloudArmorMapper) getDefaultRules() string {
	return `# SQL Injection Protection (enabled via OWASP CRS)
# XSS Protection (enabled via OWASP CRS)
# Local File Inclusion (enabled via OWASP CRS)
# Remote Code Execution (enabled via OWASP CRS)

# Additional custom rules
# Block common attack patterns
SecRule REQUEST_URI "@contains /etc/passwd" "id:300001,phase:1,deny,status:403,msg:'Path traversal attempt'"
SecRule REQUEST_URI "@contains /proc/self" "id:300002,phase:1,deny,status:403,msg:'Proc access attempt'"
SecRule REQUEST_HEADERS:User-Agent "@rx ^$" "id:300003,phase:1,deny,status:403,msg:'Empty User-Agent blocked'"

# Block known bad bots
SecRule REQUEST_HEADERS:User-Agent "@pmFromFile bad-user-agents.txt" "id:300004,phase:1,deny,status:403,msg:'Bad bot blocked'"
`
}

func (m *CloudArmorMapper) generateNginxConfig(policyName string) string {
	return fmt.Sprintf(`# Nginx configuration for Cloud Armor migration
# Policy: %s

load_module modules/ngx_http_modsecurity_module.so;

events {
    worker_connections 1024;
}

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;

    # Logging
    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for"';

    access_log /var/log/nginx/access.log main;
    error_log  /var/log/nginx/error.log warn;

    # Rate limiting zones
    limit_req_zone $binary_remote_addr zone=general:10m rate=10r/s;
    limit_req_zone $binary_remote_addr zone=api:10m rate=30r/s;

    # Connection limiting
    limit_conn_zone $binary_remote_addr zone=addr:10m;

    # ModSecurity
    modsecurity on;
    modsecurity_rules_file /etc/modsecurity.d/modsecurity.conf;

    # Upstream (configure your backend)
    upstream backend {
        server upstream:8080;
        keepalive 32;
    }

    server {
        listen 8080;
        server_name _;

        # Security headers
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-XSS-Protection "1; mode=block" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;

        # Rate limiting
        limit_req zone=general burst=20 nodelay;
        limit_conn addr 10;

        # Health check endpoint (bypasses WAF)
        location /healthz {
            access_log off;
            modsecurity off;
            return 200 "healthy\n";
            add_header Content-Type text/plain;
        }

        # Main location
        location / {
            proxy_pass http://backend;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header Connection "";

            # Timeouts
            proxy_connect_timeout 60s;
            proxy_send_timeout 60s;
            proxy_read_timeout 60s;

            # Buffer settings
            proxy_buffer_size 4k;
            proxy_buffers 8 16k;
            proxy_busy_buffers_size 24k;
        }

        # API endpoint with higher rate limit
        location /api/ {
            limit_req zone=api burst=50 nodelay;

            proxy_pass http://backend;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header Connection "";
        }
    }
}
`, policyName)
}

func (m *CloudArmorMapper) generateSetupScript(policyName string) string {
	return fmt.Sprintf(`#!/bin/bash
set -e

POLICY_NAME="%s"

echo "Cloud Armor to ModSecurity Setup"
echo "================================="

# Create required directories
mkdir -p config/modsecurity/rules
mkdir -p config/nginx
mkdir -p logs/modsecurity

# Create empty bad user agents file if not exists
if [ ! -f config/modsecurity/rules/bad-user-agents.txt ]; then
    cat > config/modsecurity/rules/bad-user-agents.txt << 'EOF'
# Bad User Agents
# Add patterns for bots to block
sqlmap
nikto
nessus
acunetix
nmap
masscan
zgrab
EOF
fi

echo "Configuration directories created"
echo ""
echo "Next steps:"
echo "1. Review and customize config/modsecurity/rules/cloud-armor-custom.conf"
echo "2. Update config/nginx/nginx.conf with your upstream backend"
echo "3. Adjust PARANOIA level in docker-compose environment (1=low, 4=high)"
echo "4. Start the service: docker-compose up -d"
echo "5. Test with: curl -v http://localhost:8080/healthz"
echo ""
echo "Monitor logs:"
echo "  docker-compose logs -f nginx-modsecurity"
echo "  tail -f logs/modsecurity/*.log"
`, policyName)
}

func (m *CloudArmorMapper) generateMigrationScript(policyName string) string {
	return fmt.Sprintf(`#!/bin/bash
set -e

POLICY_NAME="%s"

echo "Cloud Armor Migration Guide"
echo "============================"
echo ""
echo "Source Policy: $POLICY_NAME"
echo ""
echo "Migration Steps:"
echo ""
echo "1. Export current Cloud Armor policy:"
echo "   gcloud compute security-policies describe $POLICY_NAME --format=json > policy.json"
echo ""
echo "2. Review exported rules and compare with generated ModSecurity rules:"
echo "   cat config/modsecurity/rules/cloud-armor-custom.conf"
echo ""
echo "3. Test the WAF configuration:"
echo "   # Start the service"
echo "   docker-compose up -d"
echo "   "
echo "   # Test legitimate request"
echo "   curl -v http://localhost:8080/"
echo "   "
echo "   # Test blocked request (SQL injection)"
echo "   curl -v \"http://localhost:8080/?id=1' OR '1'='1\""
echo ""
echo "4. Tune false positives:"
echo "   - Check logs/modsecurity/modsec_audit.log"
echo "   - Add exceptions to config/modsecurity/rules/cloud-armor-custom.conf"
echo "   - Adjust PARANOIA level if needed"
echo ""
echo "5. Update DNS/load balancer to point to the new WAF"
echo ""
echo "Key differences from Cloud Armor:"
echo "  - Adaptive protection requires external monitoring (e.g., Fail2ban)"
echo "  - Geographic blocking requires GeoIP database"
echo "  - Rate limiting uses nginx limit_req module"
echo "  - Bot management requires additional configuration"
`, policyName)
}

func (m *CloudArmorMapper) handleAdaptiveProtection(adaptiveProtection interface{}, result *mapper.MappingResult) {
	result.AddWarning("Adaptive protection was enabled in Cloud Armor")
	result.AddManualStep("Consider setting up Fail2ban for adaptive IP blocking")
	result.AddManualStep("Configure CrowdSec for collaborative security intelligence")

	// Generate fail2ban configuration suggestion
	fail2banConfig := `# Fail2ban configuration for adaptive protection
# Install: apt-get install fail2ban
# Place in /etc/fail2ban/jail.local

[nginx-modsecurity]
enabled = true
filter = nginx-modsecurity
logpath = /var/log/modsecurity/modsec_audit.log
maxretry = 5
bantime = 3600
findtime = 600
action = iptables-multiport[name=modsecurity, port="80,443"]
`
	result.AddConfig("config/fail2ban/jail.local", []byte(fail2banConfig))

	fail2banFilter := `# Fail2ban filter for ModSecurity
# Place in /etc/fail2ban/filter.d/nginx-modsecurity.conf

[Definition]
failregex = ^.*\[client <HOST>\].*ModSecurity.*$
ignoreregex =
`
	result.AddConfig("config/fail2ban/filter.d/nginx-modsecurity.conf", []byte(fail2banFilter))
}
