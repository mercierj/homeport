package cutover

import (
	"context"
	"testing"
	"time"

	domaincutover "github.com/homeport/homeport/internal/domain/cutover"
)

func TestExecuteContinuesAfterCallerContextCancelled(t *testing.T) {
	service := NewService()
	plan, err := service.CreatePlan(&CreatePlanRequest{
		BundleID: "bundle-1",
		DryRun:   true,
		DNSChanges: []*domaincutover.DNSChange{{
			ID:         "dns-1",
			Domain:     "example.com",
			Name:       "@",
			RecordType: "A",
			NewValue:   "203.0.113.10",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan.DNSPropagationWait = 0

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make(chan CutoverEvent, 8)
	if err := service.Execute(ctx, plan.ID, func(event CutoverEvent) { events <- event }); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == "complete" {
				if !event.DryRun {
					t.Fatalf("DryRun = false, want true")
				}
				return
			}
		case <-deadline:
			exec, err := service.GetPlan(plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Fatalf("status = %q, error = %q, want complete event", exec.Plan.Status, exec.Plan.Error)
		}
	}
}
