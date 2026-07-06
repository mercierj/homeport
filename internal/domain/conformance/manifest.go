package conformance

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

type Manifest struct {
	Provider string            `yaml:"provider" json:"provider"`
	Service  string            `yaml:"service" json:"service"`
	Checks   map[Check]string  `yaml:"checks" json:"checks"`
	Evidence map[string]string `yaml:"evidence" json:"evidence"`
}

func (m Manifest) MissingChecks() []Check {
	required := []Check{CheckDiscover, CheckCost, CheckProvision, CheckMigrate, CheckAPICompat, CheckEnvDNS, CheckHA, CheckBackup, CheckValidate, CheckCutover, CheckRollback}
	missing := []Check{}
	for _, check := range required {
		if m.Checks[check] == "" {
			missing = append(missing, check)
		}
	}
	return missing
}
