package awsoperations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/app/coverage"
)

func TestCatalogHasOneRegisteredServiceForEveryAWSCoverageEntry(t *testing.T) {
	catalog, err := coverage.LoadDefaultCatalog()
	if err != nil {
		t.Fatalf("LoadDefaultCatalog() error = %v", err)
	}

	want := make(map[ServiceKey]struct{})
	for _, entry := range catalog.Services {
		if entry.Provider != "aws" {
			continue
		}
		key := NormalizeServiceKey(entry.Service)
		if _, duplicate := want[key]; duplicate {
			t.Fatalf("duplicate AWS service key %q derived from coverage catalogue", key)
		}
		want[key] = struct{}{}
	}
	if len(want) != 59 {
		t.Fatalf("AWS coverage service count = %d, want 59", len(want))
	}

	registered := RegisteredServices()
	if len(registered) != len(want) {
		t.Fatalf("registered service count = %d, want %d", len(registered), len(want))
	}
	seen := make(map[ServiceKey]struct{}, len(registered))
	for _, service := range registered {
		if _, duplicate := seen[service.Key]; duplicate {
			t.Errorf("duplicate registered service key %q", service.Key)
		}
		seen[service.Key] = struct{}{}
		if service.DisplayName == "" || len(service.ResourceTypes) == 0 || service.Target == "" || service.Family == "" || service.PanelKind == "" || service.Driver == "" {
			t.Errorf("incomplete metadata for %q: %#v", service.Key, service)
		}
	}
	for key := range want {
		if _, found := seen[key]; !found {
			t.Errorf("coverage service %q is not registered", key)
		}
	}
}

func TestOpenAPIServiceKeyContractMatchesRegisteredServices(t *testing.T) {
	path := filepath.Join("..", "..", "..", "api", "openapi.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	for _, service := range RegisteredServices() {
		if !strings.Contains(string(data), string(service.Key)) {
			t.Errorf("OpenAPI contract does not mention registered service key %q", service.Key)
		}
	}
}

func TestCatalogResolvesEveryRegisteredAWSResourceType(t *testing.T) {
	for _, service := range RegisteredServices() {
		for _, resourceType := range service.ResourceTypes {
			got, found := ServiceForResource(resourceType)
			if !found {
				t.Errorf("resource type %q for %q is not registered", resourceType, service.Key)
				continue
			}
			if got != service.Key {
				t.Errorf("resource type %q resolves to %q, want %q", resourceType, got, service.Key)
			}
		}
	}
}
