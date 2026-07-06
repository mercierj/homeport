# Service Conformance And Promotion Gates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent any service from being marked `full` unless the full A-to-Z behavior is covered by runnable conformance tests.

**Architecture:** Add a conformance manifest per service and a test runner that checks discovery, cost, provision, migrate, compatibility, env/DNS, HA, backup, validation, cutover, rollback, and app-change reporting. Coverage promotion uses these manifests as evidence.

**Tech Stack:** Go test runner, YAML manifests, coverage CLI, existing parser/mapper/datamigration tests.

---

## Files

- Create: `internal/domain/conformance/manifest.go`
- Create: `internal/domain/conformance/manifest_test.go`
- Create: `internal/app/conformance/service.go`
- Create: `internal/app/conformance/service_test.go`
- Create directory: `test/conformance/services/`
- Modify: `internal/app/coverage/service.go`
- Modify: `internal/cli/coverage.go`
- Modify: `internal/cli/coverage_test.go`

## Task 1: Define the conformance manifest

- [ ] Create `internal/domain/conformance/manifest.go`:

```go
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
```

- [ ] Create `internal/domain/conformance/manifest_test.go`:

```go
package conformance

import "testing"

func TestMissingChecksReportsEmptyChecks(t *testing.T) {
	manifest := Manifest{Checks: map[Check]string{CheckDiscover: "go test ./x"}}
	missing := manifest.MissingChecks()
	if len(missing) != 10 {
		t.Fatalf("missing = %v, want 10 missing checks", missing)
	}
}
```

- [ ] Run:

```bash
go test ./internal/domain/conformance
```

Expected: pass.

## Task 2: Load conformance manifests

- [ ] Create `internal/app/conformance/service.go`:

```go
package conformance

import (
	"fmt"
	"os"
	"path/filepath"

	domain "github.com/homeport/homeport/internal/domain/conformance"
	"gopkg.in/yaml.v3"
)

type Service struct {
	dir string
}

func NewService(dir string) *Service { return &Service{dir: dir} }

func (s *Service) Load(provider, service string) (domain.Manifest, error) {
	path := filepath.Join(s.dir, provider+"-"+slug(service)+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Manifest{}, err
	}
	var manifest domain.Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return domain.Manifest{}, err
	}
	if manifest.Provider != provider || manifest.Service != service {
		return domain.Manifest{}, fmt.Errorf("manifest identity mismatch: got %s/%s", manifest.Provider, manifest.Service)
	}
	return manifest, nil
}

func slug(value string) string {
	out := make([]rune, 0, len(value))
	lastDash := false
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out = append(out, r)
			lastDash = false
			continue
		}
		if !lastDash {
			out = append(out, '-')
			lastDash = true
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
```

- [ ] Create `internal/app/conformance/service_test.go` with a temp manifest and assert `Load("aws", "S3")` returns all checks.

## Task 3: Require conformance before full promotion

- [ ] Modify `internal/app/coverage/service.go` so `Promote(...StatusFull...)` also loads the matching conformance manifest from `test/conformance/services` and refuses promotion when `MissingChecks()` is not empty.

- [ ] Add test:

```go
func TestPromoteRejectsFullWithoutConformanceManifest(t *testing.T) {
	catalog := Catalog{Services: []domaincoverage.ServiceCoverage{{
		Provider: "aws", Service: "S3", Status: domaincoverage.StatusMapped, ManualStepsResolved: true,
		Discover: true, Cost: true, Provision: true, Migrate: true, APICompat: true, EnvDNS: true, HA: true, Backup: true, Validate: true, Cutover: true, Rollback: true,
	}}}

	err := catalog.Promote("aws", "S3", domaincoverage.StatusFull)
	if err == nil || !strings.Contains(err.Error(), "conformance manifest") {
		t.Fatalf("expected conformance manifest guard, got %v", err)
	}
}
```

- [ ] Run:

```bash
go test ./internal/app/coverage -run TestPromoteRejectsFullWithoutConformanceManifest
```

Expected: fail before the guard, pass after implementation.

## Task 4: Generate service manifests for all rows

- [ ] For every row in `docs/coverage/services.yaml`, create a conformance manifest such as `test/conformance/services/aws-s3.yaml`, `test/conformance/services/gcp-cloud-storage.yaml`, and `test/conformance/services/azure-azure-storage.yaml`.

Each manifest must have this shape:

```yaml
provider: aws
service: S3
checks:
  discover: go test ./test/integration/aws -run S3
  cost: go test ./internal/domain/provider ./internal/app/providers
  provision: go test ./internal/infrastructure/mapper/aws/... -run S3
  migrate: go test ./internal/app/datamigration -run S3
  api_compat: go test ./test/compat -run S3
  env_dns: go test ./internal/app/runbook ./internal/app/cutover
  ha: go test ./internal/domain/provider ./internal/infrastructure/generator/...
  backup: go test ./internal/app/backup
  validate: go test ./internal/app/runbook ./internal/app/metrics
  cutover: go test ./internal/app/cutover
  rollback: go test ./internal/app/cutover ./internal/app/backup
evidence:
  target: MinIO
  app_change_mode: adapter
```

Use the real provider, service, target, test regex, and app change mode for each service.

## Task 5: Verify and commit conformance gates

- [ ] Run:

```bash
go test ./internal/domain/conformance ./internal/app/conformance ./internal/app/coverage ./internal/cli
go test ./test/integration/aws ./test/integration/gcp ./test/integration/azure
```

Expected: pass.

- [ ] Commit:

```bash
git add internal/domain/conformance internal/app/conformance internal/app/coverage/service.go internal/app/coverage/service_test.go internal/cli/coverage.go internal/cli/coverage_test.go test/conformance/services
git commit -m "test: require conformance evidence for full coverage"
```
