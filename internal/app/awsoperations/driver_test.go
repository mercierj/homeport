package awsoperations

import (
	"context"
	"errors"
	"testing"
)

func TestLambdaDriverRejectsUpdateOutsideWorkspaceBinding(t *testing.T) {
	driver := NewLambdaDriver(&fakeFunctions{})
	workspace := Workspace{Services: map[ServiceKey]ServiceState{ServiceLambda: {Status: ServiceStatusAvailable, Capabilities: []Capability{CapabilityUpdate}}}, Bindings: []ResourceBinding{{Service: ServiceLambda, LocalResourceID: "bound"}}}
	if _, err := driver.Update(context.Background(), workspace, "other", FunctionInput{}); err == nil {
		t.Fatal("Update() error = nil, want unbound resource rejected")
	}
}

func TestSQSDriverRejectsPurgeWhenCapabilityIsAbsent(t *testing.T) {
	driver := NewSQSDriver(&fakeQueues{})
	workspace := Workspace{Services: map[ServiceKey]ServiceState{ServiceSQS: {Status: ServiceStatusAvailable, Capabilities: []Capability{CapabilityList}}}, Bindings: []ResourceBinding{{Service: ServiceSQS, LocalResourceID: "images", LocalStackID: "default"}}}
	if _, err := driver.Purge(context.Background(), workspace, "images", "pending"); err == nil {
		t.Fatal("Purge() error = nil, want missing capability rejected")
	}
}

func TestLambdaDriverInvokesOnlyBoundFunction(t *testing.T) {
	backend := &fakeFunctions{}
	driver := NewLambdaDriver(backend)
	workspace := Workspace{Services: map[ServiceKey]ServiceState{ServiceLambda: {Status: ServiceStatusAvailable, Capabilities: []Capability{CapabilityInvoke}}}, Bindings: []ResourceBinding{{Service: ServiceLambda, LocalResourceID: "bound"}}}

	if _, err := driver.Invoke(context.Background(), workspace, "other", []byte(`{}`)); err == nil {
		t.Fatal("Invoke() error = nil, want unbound resource rejected")
	}
	if backend.invoked != "" {
		t.Fatalf("backend invoked %q, want no downstream call", backend.invoked)
	}
	if _, err := driver.Invoke(context.Background(), workspace, "bound", []byte(`{}`)); err != nil {
		t.Fatalf("Invoke(bound): %v", err)
	}
	if backend.invoked != "bound" {
		t.Fatalf("backend invoked %q, want bound", backend.invoked)
	}
}

func TestSQSDriverListsOnlyBoundQueues(t *testing.T) {
	backend := &fakeQueues{queues: []QueueRecord{{Name: "bound"}, {Name: "other"}}}
	driver := NewSQSDriver(backend)
	workspace := Workspace{Services: map[ServiceKey]ServiceState{ServiceSQS: {Status: ServiceStatusAvailable, Capabilities: []Capability{CapabilityList}}}, Bindings: []ResourceBinding{{ImportedResourceID: "queue-1", Service: ServiceSQS, LocalResourceID: "bound", LocalStackID: "stack-a", Region: "eu-west-3"}}}

	items, err := driver.List(context.Background(), workspace)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(items) != 1 || items[0].(QueueRecord).Name != "bound" {
		t.Fatalf("List() = %#v, want only bound queue", items)
	}
	queue := items[0].(QueueRecord)
	if queue.ImportedResourceID != "queue-1" || queue.Region != "eu-west-3" || queue.LocalStackID != "stack-a" {
		t.Fatalf("queue metadata = %#v, want binding metadata", queue)
	}
	if backend.stackID != "stack-a" {
		t.Fatalf("List() stack ID = %q, want stack-a", backend.stackID)
	}
}

func TestDriverRegistryDeclaresEveryCatalogServiceAndKeepsUnavailableResourcesVisible(t *testing.T) {
	registry, err := NewDriverRegistry()
	if err != nil {
		t.Fatalf("NewDriverRegistry() error = %v", err)
	}
	if registry.Len() != len(RegisteredServices()) {
		t.Fatalf("registered drivers = %d, want %d", registry.Len(), len(RegisteredServices()))
	}
	driver, found := registry.Get(ServiceKey("s3"))
	if !found {
		t.Fatal("s3 driver is not declared")
	}
	items, err := driver.List(context.Background(), Workspace{Bindings: []ResourceBinding{{ImportedResourceID: "bucket-import", Service: "s3", Name: "assets", LocalStackID: "storage"}}})
	if err != nil {
		t.Fatalf("unavailable S3 driver List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unavailable S3 items = %#v, want one bound resource", items)
	}
	resource, ok := items[0].(UnavailableResourceRecord)
	if !ok || resource.ImportedResourceID != "bucket-import" || resource.Reason == "" {
		t.Fatalf("unavailable S3 resource = %#v, want persisted resource and reason", items[0])
	}
}

func TestLambdaDriverAppliesBindingMetadataToUpdate(t *testing.T) {
	backend := &fakeFunctions{updated: &FunctionRecord{ID: "bound", Name: "new-name"}}
	driver := NewLambdaDriver(backend)
	workspace := Workspace{Services: map[ServiceKey]ServiceState{ServiceLambda: {Status: ServiceStatusAvailable, Capabilities: []Capability{CapabilityUpdate}}}, Bindings: []ResourceBinding{{ImportedResourceID: "lambda-1", Service: ServiceLambda, LocalResourceID: "bound", LocalStackID: "stack-a", Region: "eu-west-3", Tags: map[string]string{"team": "core"}}}}
	updated, err := driver.Update(context.Background(), workspace, "bound", FunctionInput{})
	if err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if updated.ImportedResourceID != "lambda-1" || updated.LocalStackID != "stack-a" || updated.Tags["team"] != "core" {
		t.Fatalf("updated metadata = %#v, want binding metadata", updated)
	}
}

type fakeFunctions struct {
	invoked string
	updated *FunctionRecord
}

func (*fakeFunctions) List(context.Context) ([]FunctionRecord, error)       { return nil, nil }
func (*fakeFunctions) Get(context.Context, string) (*FunctionRecord, error) { return nil, nil }
func (*fakeFunctions) Create(context.Context, FunctionInput) (*FunctionRecord, error) {
	return nil, nil
}
func (f *fakeFunctions) Update(context.Context, string, FunctionInput) (*FunctionRecord, error) {
	return f.updated, nil
}
func (*fakeFunctions) Delete(context.Context, string) error { return nil }
func (f *fakeFunctions) Invoke(_ context.Context, id string, _ []byte) (*InvocationRecord, error) {
	f.invoked = id
	if id == "" {
		return nil, errors.New("missing ID")
	}
	return &InvocationRecord{}, nil
}
func (*fakeFunctions) Logs(context.Context, string) ([]LogRecord, error) { return nil, nil }

type fakeQueues struct {
	queues  []QueueRecord
	stackID string
}

func (f *fakeQueues) List(_ context.Context, stackID string) ([]QueueRecord, error) {
	f.stackID = stackID
	return f.queues, nil
}
func (*fakeQueues) Messages(context.Context, string, string, string) ([]MessageRecord, error) {
	return nil, nil
}
func (*fakeQueues) Retry(context.Context, string, string, string) error          { return nil }
func (*fakeQueues) Delete(context.Context, string, string, string) error         { return nil }
func (*fakeQueues) Purge(context.Context, string, string, string) (int64, error) { return 0, nil }
