package aws_test

import (
	"testing"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
)

func TestAWSCoverageRowsDoNotClaimProviderGradeFullWithoutL4(t *testing.T) {
	catalog, err := appcoverage.LoadDefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Services {
		if row.Provider != "aws" {
			continue
		}
		if row.CompatibilityLevel != domaincoverage.CompatibilityLevelL4 && row.Status == domaincoverage.StatusFull {
			t.Fatalf("AWS %s claims provider-grade full without L4 evidence", row.Service)
		}
		if row.Status == domaincoverage.StatusFull && (domaincoverage.ComputeStatus(row) != domaincoverage.StatusFull || !row.ManualStepsResolved) {
			t.Fatalf("AWS %s has an invalid full closure", row.Service)
		}
	}
}
