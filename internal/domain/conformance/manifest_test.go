package conformance

import "testing"

func TestMissingChecksReportsEmptyChecks(t *testing.T) {
	manifest := Manifest{Checks: map[Check]string{CheckDiscover: "go test ./x"}}
	missing := manifest.MissingChecks()
	if len(missing) != 10 {
		t.Fatalf("missing = %v, want 10 missing checks", missing)
	}
}
