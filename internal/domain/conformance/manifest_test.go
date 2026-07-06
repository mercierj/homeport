package conformance

import "testing"

func TestMissingChecksReportsEmptyChecks(t *testing.T) {
	manifest := Manifest{Checks: map[Check]string{CheckDiscover: "go test ./x"}}
	missing := manifest.MissingChecks()
	if len(missing) != 10 {
		t.Fatalf("missing = %v, want 10 missing checks", missing)
	}
}

func TestPromotionIssuesRejectPlaceholderEvidence(t *testing.T) {
	manifest := completeManifest()
	manifest.Evidence["target"] = "HomePort managed replacement"
	manifest.Evidence["app_change_mode"] = "adapter_or_generated_report"

	if issues := manifest.PromotionIssues(); len(issues) == 0 {
		t.Fatal("placeholder evidence should not satisfy promotion")
	}
}

func TestPromotionIssuesAcceptSpecificEvidence(t *testing.T) {
	manifest := completeManifest()

	if issues := manifest.PromotionIssues(); len(issues) != 0 {
		t.Fatalf("issues = %v, want none", issues)
	}
}

func completeManifest() Manifest {
	return Manifest{
		Checks: map[Check]string{
			CheckDiscover:  "go test ./test/integration/aws -run S3",
			CheckCost:      "go test ./internal/domain/coverage",
			CheckProvision: "go test ./internal/infrastructure/mapper/storage -run S3",
			CheckMigrate:   "go test ./internal/app/datamigration -run S3",
			CheckAPICompat: "go test ./test/compat -run S3",
			CheckEnvDNS:    "go test ./internal/app/cutover -run S3",
			CheckHA:        "go test ./internal/domain/provider -run S3",
			CheckBackup:    "go test ./internal/app/backup -run S3",
			CheckValidate:  "go test ./internal/app/metrics -run S3",
			CheckCutover:   "go test ./internal/app/cutover -run S3",
			CheckRollback:  "go test ./internal/app/backup -run S3",
		},
		Evidence: map[string]string{
			"target":          "MinIO",
			"app_change_mode": "adapter",
		},
	}
}
