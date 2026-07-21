package coverage

type Status string
type CompatibilityLevel string

const (
	StatusFull       Status = "full"
	StatusMapped     Status = "mapped"
	StatusGuided     Status = "guided"
	StatusMissing    Status = "missing"
	StatusImpossible Status = "impossible"

	CompatibilityLevelL0 CompatibilityLevel = "L0"
	CompatibilityLevelL1 CompatibilityLevel = "L1"
	CompatibilityLevelL2 CompatibilityLevel = "L2"
	CompatibilityLevelL3 CompatibilityLevel = "L3"
	CompatibilityLevelL4 CompatibilityLevel = "L4"
)

type ServiceCoverage struct {
	Provider                 string             `json:"provider" yaml:"provider"`
	Service                  string             `json:"service" yaml:"service"`
	Category                 string             `json:"category,omitempty" yaml:"category,omitempty"`
	SourceAPI                string             `json:"source_api,omitempty" yaml:"source_api,omitempty"`
	ResourceTypes            []string           `json:"resource_types" yaml:"resource_types"`
	TerraformResourceTypes   []string           `json:"terraform_resource_types,omitempty" yaml:"terraform_resource_types,omitempty"`
	Target                   string             `json:"target,omitempty" yaml:"target,omitempty"`
	APICompatibilityStrategy string             `json:"api_compatibility_strategy,omitempty" yaml:"api_compatibility_strategy,omitempty"`
	CompatibilityLevel       CompatibilityLevel `json:"compatibility_level,omitempty" yaml:"compatibility_level,omitempty"`
	ImpossibilityNotes       string             `json:"impossibility_notes,omitempty" yaml:"impossibility_notes,omitempty"`
	ManualStepsResolved      bool               `json:"manual_steps_resolved,omitempty" yaml:"manual_steps_resolved,omitempty"`
	Discover                 bool               `json:"discover" yaml:"discover"`
	Cost                     bool               `json:"cost" yaml:"cost"`
	Provision                bool               `json:"provision" yaml:"provision"`
	Migrate                  bool               `json:"migrate" yaml:"migrate"`
	APICompat                bool               `json:"api_compat" yaml:"api_compat"`
	EnvDNS                   bool               `json:"env_dns" yaml:"env_dns"`
	HA                       bool               `json:"ha" yaml:"ha"`
	Backup                   bool               `json:"backup" yaml:"backup"`
	Validate                 bool               `json:"validate" yaml:"validate"`
	Cutover                  bool               `json:"cutover" yaml:"cutover"`
	Rollback                 bool               `json:"rollback" yaml:"rollback"`
	Status                   Status             `json:"status" yaml:"status"`
	Blocker                  string             `json:"blocker,omitempty" yaml:"blocker,omitempty"`
}

func ComputeStatus(row ServiceCoverage) Status {
	if row.Status == StatusMissing {
		return StatusMissing
	}
	if row.Status == StatusImpossible || row.Blocker != "" {
		return StatusImpossible
	}
	if !hasFullCoverage(row) || row.CompatibilityLevel != CompatibilityLevelL4 {
		if row.Status == StatusGuided {
			return StatusGuided
		}
		return StatusMapped
	}
	return StatusFull
}

func hasFullCoverage(row ServiceCoverage) bool {
	return row.Discover &&
		row.Cost &&
		row.Provision &&
		row.Migrate &&
		row.APICompat &&
		row.EnvDNS &&
		row.HA &&
		row.Backup &&
		row.Validate &&
		row.Cutover &&
		row.Rollback
}
