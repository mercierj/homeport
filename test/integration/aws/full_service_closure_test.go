package aws_test

import (
	"testing"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
)

func TestAllAWSCoverageRowsAreFull(t *testing.T) {
	catalog, err := appcoverage.LoadDefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Services {
		if row.Provider != "aws" {
			continue
		}
		if row.Status != domaincoverage.StatusFull || domaincoverage.ComputeStatus(row) != domaincoverage.StatusFull {
			t.Fatalf("AWS %s is not full: status=%s blocker=%q", row.Service, row.Status, row.Blocker)
		}
		if !row.ManualStepsResolved {
			t.Fatalf("AWS %s manual steps are not resolved", row.Service)
		}
	}
}
