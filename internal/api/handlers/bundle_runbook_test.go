package handlers

import "testing"

func TestBuildBundleRunbookUsesBundleIDAndGroups(t *testing.T) {
	book := buildBundleRunbook("bundle-1", true, []*SecretRef{{Name: "DB_PASSWORD", Required: true}})

	if book.ID != "bundle-1" {
		t.Fatalf("runbook id = %q, want bundle-1", book.ID)
	}
	wantGroups := []string{"Credentials", "Provision", "Sync", "Validate", "Cutover", "Rollback"}
	if len(book.Steps) != len(wantGroups) {
		t.Fatalf("len(Steps) = %d, want %d", len(book.Steps), len(wantGroups))
	}
	for i, group := range wantGroups {
		if book.Steps[i].Group != group {
			t.Fatalf("step %d group = %q, want %q", i, book.Steps[i].Group, group)
		}
	}
	if !book.Steps[len(book.Steps)-1].Optional {
		t.Fatal("rollback step should be optional for forward completion")
	}
}
