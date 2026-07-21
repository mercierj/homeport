package coverage

import "testing"

func TestComputeStatusFullCoverage(t *testing.T) {
	row := ServiceCoverage{
		CompatibilityLevel: CompatibilityLevelL4,
		Discover:           true,
		Cost:               true,
		Provision:          true,
		Migrate:            true,
		APICompat:          true,
		EnvDNS:             true,
		HA:                 true,
		Backup:             true,
		Validate:           true,
		Cutover:            true,
		Rollback:           true,
	}

	if got := ComputeStatus(row); got != StatusFull {
		t.Fatalf("ComputeStatus() = %q, want %q", got, StatusFull)
	}
}

func TestComputeStatusFullCoverageWithoutL4IsNotFull(t *testing.T) {
	row := ServiceCoverage{
		CompatibilityLevel: CompatibilityLevelL3,
		Discover:           true,
		Cost:               true,
		Provision:          true,
		Migrate:            true,
		APICompat:          true,
		EnvDNS:             true,
		HA:                 true,
		Backup:             true,
		Validate:           true,
		Cutover:            true,
		Rollback:           true,
	}

	if got := ComputeStatus(row); got == StatusFull {
		t.Fatalf("ComputeStatus() = %q, want not %q", got, StatusFull)
	}
}

func TestComputeStatusAPICompatFalseIsNotFull(t *testing.T) {
	row := ServiceCoverage{
		Discover:  true,
		Cost:      true,
		Provision: true,
		Migrate:   true,
		APICompat: false,
		EnvDNS:    true,
		HA:        true,
		Backup:    true,
		Validate:  true,
		Cutover:   true,
		Rollback:  true,
	}

	if got := ComputeStatus(row); got == StatusFull {
		t.Fatalf("ComputeStatus() = %q, want not %q", got, StatusFull)
	}
}

func TestComputeStatusBlockerIsImpossible(t *testing.T) {
	row := ServiceCoverage{
		Discover:  true,
		Cost:      true,
		Provision: true,
		Migrate:   true,
		APICompat: true,
		EnvDNS:    true,
		HA:        true,
		Backup:    true,
		Validate:  true,
		Cutover:   true,
		Rollback:  true,
		Blocker:   "provider has no equivalent API",
	}

	if got := ComputeStatus(row); got != StatusImpossible {
		t.Fatalf("ComputeStatus() = %q, want %q", got, StatusImpossible)
	}
}
