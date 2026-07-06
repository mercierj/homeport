package gcp_test

import (
	"testing"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
)

func TestAllGCPCoverageRowsAreFull(t *testing.T) {
	catalog, err := appcoverage.LoadDefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Services {
		if row.Provider != "gcp" {
			continue
		}
		if row.Status != domaincoverage.StatusFull || domaincoverage.ComputeStatus(row) != domaincoverage.StatusFull {
			t.Fatalf("GCP %s is not full: status=%s blocker=%q", row.Service, row.Status, row.Blocker)
		}
		if !row.ManualStepsResolved {
			t.Fatalf("GCP %s manual steps are not resolved", row.Service)
		}
	}
}
