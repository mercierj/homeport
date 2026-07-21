package handlers

import (
	"path/filepath"
	"testing"

	"github.com/homeport/homeport/internal/app/awsoperations"
)

func TestAWSActivationStoreRetainsPendingPlanUntilExplicitCompletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	store, err := newAWSActivationStore(path)
	if err != nil {
		t.Fatalf("newAWSActivationStore() error = %v", err)
	}
	input := awsoperations.ActivationInput{DiscoveryID: "discovery", TargetStackID: "stack", Activated: []awsoperations.ServiceKey{"lambda"}, LocalBindings: []awsoperations.LocalResourceBinding{{ImportedResourceID: "import", LocalResourceID: "local", LocalStackID: "stack"}}}
	if err := store.putPlan("cutover", input); err != nil {
		t.Fatalf("putPlan() error = %v", err)
	}
	reloaded, err := newAWSActivationStore(path)
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	if got, found := reloaded.getPlan("cutover"); !found || got.DiscoveryID != input.DiscoveryID {
		t.Fatalf("getPlan() = %#v, %t", got, found)
	}
	if err := reloaded.deletePlan("cutover"); err != nil {
		t.Fatalf("deletePlan() error = %v", err)
	}
	if _, found := reloaded.getPlan("cutover"); found {
		t.Fatal("pending plan remains after deletePlan")
	}
}

func TestAWSActivationStorePersistsTrustedBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	store, err := newAWSActivationStore(path)
	if err != nil {
		t.Fatalf("newAWSActivationStore() error = %v", err)
	}
	bindings := []awsoperations.LocalResourceBinding{{ImportedResourceID: "import", LocalResourceID: "local", LocalStackID: "stack"}}
	if err := store.putBindings("discovery\x00stack", bindings); err != nil {
		t.Fatalf("putBindings() error = %v", err)
	}
	reloaded, err := newAWSActivationStore(path)
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	got := reloaded.getBindings("discovery\x00stack")
	if len(got) != 1 || got[0].LocalResourceID != "local" {
		t.Fatalf("getBindings() = %#v", got)
	}
}
