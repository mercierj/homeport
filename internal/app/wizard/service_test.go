package wizard

import (
	"testing"

	domainwizard "github.com/homeport/homeport/internal/domain/wizard"
)

func TestServicePersistsSession(t *testing.T) {
	dir := t.TempDir()
	service := NewService(dir)
	session, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(session.ID, domainwizard.Session{CurrentStep: domainwizard.StepDeploy, BundleID: "bundle-1"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewService(dir).Get(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentStep != domainwizard.StepDeploy || reloaded.BundleID != "bundle-1" || reloaded.RunbookID != "bundle-1" {
		t.Fatalf("unexpected session: %#v", reloaded)
	}
}
