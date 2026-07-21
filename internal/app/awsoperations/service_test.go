package awsoperations

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/homeport/homeport/internal/app/migrate"
)

func TestServiceActivatesOnlyMigratedAWSServiceAfterCutover(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", []string{"eu-west-3"}, []migrate.ResourceInfo{
		{ID: "lambda-1", Name: "thumbnailer", Type: "aws_lambda_function", Region: "eu-west-3", Tags: map[string]string{"team": "media"}},
		{ID: "queue-1", Name: "images", Type: "aws_sqs_queue", Region: "eu-west-3"},
		{ID: "bucket-1", Name: "assets", Type: "aws_s3_bucket", Region: "eu-west-3"},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	workspaces, err := NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(discoveries, workspaces)

	if _, err := service.GetByDiscoveryID(discovery.ID); err == nil {
		t.Fatal("GetByDiscoveryID() before cutover error = nil, want not found")
	}

	workspace, err := service.Activate(ActivationInput{
		DiscoveryID:   discovery.ID,
		TargetStackID: "production",
		Activated:     []ServiceKey{ServiceLambda},
		LocalBindings: []LocalResourceBinding{{
			ImportedResourceID: "lambda-1",
			LocalResourceID:    "c3d6c8a1-7e03-4f99-b24a-a8ea88f22d1e",
			LocalStackID:       "production",
		}},
	})
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}

	if got := workspace.Services[ServiceLambda].Status; got != ServiceStatusAvailable {
		t.Errorf("lambda status = %q, want %q", got, ServiceStatusAvailable)
	}
	if got := workspace.Services[ServiceSQS].Status; got != ServiceStatusUnavailable {
		t.Errorf("sqs status = %q, want %q", got, ServiceStatusUnavailable)
	}
	if got := workspace.Services[ServiceKey("s3")].Status; got != ServiceStatusUnavailable {
		t.Errorf("s3 status = %q, want %q", got, ServiceStatusUnavailable)
	}
	if workspace.Services[ServiceKey("s3")].Reason == "" {
		t.Error("s3 unavailable state has no truthful reason")
	}
	if len(workspace.Bindings) != 3 {
		t.Fatalf("bindings = %d, want 3", len(workspace.Bindings))
	}
	if workspace.Bindings[0].ImportedResourceID != "lambda-1" || workspace.Bindings[0].LocalResourceID != "c3d6c8a1-7e03-4f99-b24a-a8ea88f22d1e" || workspace.Bindings[0].LocalStackID != "production" || workspace.Bindings[0].Name != "thumbnailer" || workspace.Bindings[0].Region != "eu-west-3" || workspace.Bindings[0].Tags["team"] != "media" {
		t.Errorf("lambda binding = %#v, want imported discovery data", workspace.Bindings[0])
	}
}

func TestServiceMakesEveryDiscoveredCatalogServiceVisibleWhenNoLocalDriverIsAvailable(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	resources := make([]migrate.ResourceInfo, 0)
	for index, metadata := range RegisteredServices() {
		resources = append(resources, migrate.ResourceInfo{ID: fmt.Sprintf("resource-%d", index), Name: metadata.DisplayName, Type: metadata.ResourceTypes[0]})
	}
	discovery, err := discoveries.Save("All services", "aws", nil, resources)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	workspace, err := NewService(discoveries, workspaces).Activate(ActivationInput{DiscoveryID: discovery.ID, TargetStackID: "default"})
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if len(workspace.Services) != len(RegisteredServices()) {
		t.Fatalf("visible services = %d, want %d", len(workspace.Services), len(RegisteredServices()))
	}
	if len(workspace.Bindings) != len(resources) {
		t.Fatalf("bindings = %d, want %d", len(workspace.Bindings), len(resources))
	}
	for _, metadata := range RegisteredServices() {
		state, found := workspace.Services[metadata.Key]
		if !found {
			t.Errorf("service %q was omitted from workspace", metadata.Key)
			continue
		}
		if metadata.Key == ServiceLambda || metadata.Key == ServiceSQS {
			continue
		}
		if state.Status != ServiceStatusUnavailable || state.Reason == "" {
			t.Errorf("service %q state = %#v, want unavailable with reason", metadata.Key, state)
		}
	}
}

func TestSQSExposesOnlySupportedCapabilities(t *testing.T) {
	capabilities := capabilitiesFor(ServiceSQS)
	want := []Capability{CapabilityList, CapabilityRead, CapabilityDelete, CapabilityPurge, CapabilityRetry}
	if !slices.Equal(capabilities, want) {
		t.Errorf("SQS capabilities = %v, want %v", capabilities, want)
	}
}

func TestServiceRejectsNonAWSDiscovery(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Other", "gcp", nil, []migrate.ResourceInfo{{ID: "function", Type: "google_cloudfunctions_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = NewService(discoveries, workspaces).Activate(ActivationInput{DiscoveryID: discovery.ID, TargetStackID: "default", Activated: []ServiceKey{ServiceLambda}})
	if err == nil {
		t.Fatal("Activate() error = nil, want non-AWS discovery rejected")
	}
}

func TestServiceRejectsActivationForServiceMissingFromDiscovery(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{{ID: "lambda-1", Type: "aws_lambda_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = NewService(discoveries, workspaces).ValidateActivation(ActivationInput{DiscoveryID: discovery.ID, Activated: []ServiceKey{ServiceSQS}})
	if err == nil {
		t.Fatal("ValidateActivation() error = nil, want service absent from discovery rejected")
	}
}

func TestServiceActivationCreatesOneWorkspaceForConcurrentRequests(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{{ID: "lambda-1", Type: "aws_lambda_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "workspaces.json")
	workspaces, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(discoveries, workspaces)

	const workers = 16
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			workspace, err := service.Activate(ActivationInput{DiscoveryID: discovery.ID, TargetStackID: "default", Activated: []ServiceKey{ServiceLambda}, LocalBindings: []LocalResourceBinding{{ImportedResourceID: "lambda-1", LocalResourceID: "c3d6c8a1-7e03-4f99-b24a-a8ea88f22d1e", LocalStackID: "default"}}})
			if err != nil {
				errs <- err
				return
			}
			ids <- workspace.ID
		}()
	}
	wait.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Errorf("Activate() error = %v", err)
	}
	var id string
	for got := range ids {
		if id == "" {
			id = got
		}
		if got != id {
			t.Errorf("workspace ID = %q, want %q", got, id)
		}
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() reload error = %v", err)
	}
	if len(reloaded.workspaces) != 1 {
		t.Fatalf("durable workspaces = %d, want 1", len(reloaded.workspaces))
	}
}

func TestServiceRevalidatesIdempotentActivationInput(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{{ID: "lambda-1", Type: "aws_lambda_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := NewService(discoveries, workspaces)
	if _, err := service.Activate(ActivationInput{DiscoveryID: discovery.ID, TargetStackID: "default", Activated: []ServiceKey{ServiceLambda}, LocalBindings: []LocalResourceBinding{{ImportedResourceID: "lambda-1", LocalResourceID: "local-lambda", LocalStackID: "default"}}}); err != nil {
		t.Fatalf("initial Activate() error = %v", err)
	}
	if _, err := service.Activate(ActivationInput{DiscoveryID: discovery.ID, TargetStackID: "default", Activated: []ServiceKey{"not-registered"}}); err == nil {
		t.Fatal("idempotent Activate() error = nil, want invalid service rejected")
	}
}

func TestStoreRejectsDuplicateDiscoveryIDsOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")
	if err := os.WriteFile(path, []byte(`{"one":{"id":"one","discovery_id":"discovery-1"},"two":{"id":"two","discovery_id":"discovery-1"}}`), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := NewStore(path); err == nil {
		t.Fatal("NewStore() error = nil, want duplicate discovery IDs rejected")
	}
}

func TestStoreCreateCoordinatesIndependentStoreInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspaces.json")
	first, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() first error = %v", err)
	}
	second, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() second error = %v", err)
	}

	results := make(chan *Workspace, 3)
	errs := make(chan error, 3)
	var wait sync.WaitGroup
	for _, input := range []struct {
		store     *Store
		workspace *Workspace
	}{
		{first, &Workspace{ID: "one", DiscoveryID: "discovery-1"}},
		{second, &Workspace{ID: "two", DiscoveryID: "discovery-1"}},
		{second, &Workspace{ID: "three", DiscoveryID: "discovery-2"}},
	} {
		wait.Add(1)
		go func(store *Store, workspace *Workspace) {
			defer wait.Done()
			created, err := store.Create(workspace)
			if err != nil {
				errs <- err
				return
			}
			results <- created
		}(input.store, input.workspace)
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("Create() error = %v", err)
	}

	var discoveryOneID string
	for workspace := range results {
		if workspace.DiscoveryID != "discovery-1" {
			continue
		}
		if discoveryOneID == "" {
			discoveryOneID = workspace.ID
		} else if workspace.ID != discoveryOneID {
			t.Errorf("discovery-1 workspace ID = %q, want %q", workspace.ID, discoveryOneID)
		}
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() reload error = %v", err)
	}
	if len(reloaded.workspaces) != 2 {
		t.Fatalf("durable workspaces = %d, want 2", len(reloaded.workspaces))
	}
	if _, err := reloaded.GetByDiscoveryID("discovery-1"); err != nil {
		t.Errorf("discovery-1 missing after concurrent create: %v", err)
	}
	if _, err := reloaded.GetByDiscoveryID("discovery-2"); err != nil {
		t.Errorf("discovery-2 missing after concurrent create: %v", err)
	}
}
