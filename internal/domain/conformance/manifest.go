package conformance

import "strings"

type Check string

const (
	CheckDiscover  Check = "discover"
	CheckCost      Check = "cost"
	CheckProvision Check = "provision"
	CheckMigrate   Check = "migrate"
	CheckAPICompat Check = "api_compat"
	CheckEnvDNS    Check = "env_dns"
	CheckHA        Check = "ha"
	CheckBackup    Check = "backup"
	CheckValidate  Check = "validate"
	CheckCutover   Check = "cutover"
	CheckRollback  Check = "rollback"
)

func RequiredChecks() []Check {
	return []Check{CheckDiscover, CheckCost, CheckProvision, CheckMigrate, CheckAPICompat, CheckEnvDNS, CheckHA, CheckBackup, CheckValidate, CheckCutover, CheckRollback}
}

type Manifest struct {
	Provider string            `yaml:"provider" json:"provider"`
	Service  string            `yaml:"service" json:"service"`
	Checks   map[Check]string  `yaml:"checks" json:"checks"`
	Evidence map[string]string `yaml:"evidence" json:"evidence"`
}

func (m Manifest) MissingChecks() []Check {
	missing := []Check{}
	for _, check := range RequiredChecks() {
		if m.Checks[check] == "" {
			missing = append(missing, check)
		}
	}
	return missing
}

func (m Manifest) PromotionIssues() []string {
	issues := []string{}
	for _, check := range m.MissingChecks() {
		issues = append(issues, "missing "+string(check))
	}
	for check, command := range m.Checks {
		if strings.Contains(command, "/...") {
			issues = append(issues, "generic "+string(check)+" check is not service evidence")
		}
	}
	target := strings.TrimSpace(m.Evidence["target"])
	if target == "" || strings.EqualFold(target, "HomePort managed replacement") {
		issues = append(issues, "specific target evidence is required")
	}
	mode := strings.TrimSpace(m.Evidence["app_change_mode"])
	if mode == "" || mode == "adapter_or_generated_report" {
		issues = append(issues, "specific app change evidence is required")
	}
	return issues
}
