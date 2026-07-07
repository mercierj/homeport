package compatplan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type coverageCatalog struct {
	Services []coverageService `yaml:"services"`
}

type coverageService struct {
	Provider      string   `yaml:"provider"`
	Service       string   `yaml:"service"`
	ResourceTypes []string `yaml:"resource_types"`
	Target        string   `yaml:"target"`
}

func TestEveryCoverageServiceHasCompatibilityPlan(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs/coverage/services.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var catalog coverageCatalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}

	required := []string{
		"## Goal",
		"## Provider API Surface",
		"## Backend",
		"## Authz Model",
		"## Adapter",
		"## Generated Artifacts",
		"## Contract Tests",
		"## Compatibility Level",
		"Current level:",
		"Target level:",
		"Blocking gaps:",
	}
	banned := []string{
		"Actions supported at the first target level: resource CRUD, list/get operations, metadata, tags or labels, and validation calls needed by migrated workloads.",
		"Official SDK tests cover create/get/list/update/delete or equivalent lifecycle operations",
		"Action: provider action string",
		"Microsoft.AzureVM/*",
		"Microsoft.DevOps/pipelines",
		"Microsoft.Fabric/capacities",
		"Microsoft.PowerBIDedicated/capacities",
		"Backend: Docker, Kubernetes, K3s, OpenFaaS, or generated container runtime.",
		"HomePort implementation backed by an open-source service selected during service design",
		"Initial supported surface: HomePort import",
		"Initial supported surface: HomePort/",
		"Actions: HomePort import",
		"Actions: HomePort/",
		"First concrete resource model to add: `azurerm_",
		"First concrete resource model to add: `google_",
		"the new `azurerm_",
		"the new `google_",
		"service actions required by",
		"plus any provider-native path required by the official SDK",
		"exact method names are a blocking gap",
		"conformance-manifest method sequence",
		"generated conformance manifest",
		"contract tests named below do not exist yet",
		"add the resource model if missing",
		"HomePort-managed ",
		"Current level: L2/L3",
		"exercises create -> describe/get -> update/invoke -> delete",
		"exercises create -> get -> list -> patch/run -> delete",
		"exercises get/list -> create/update -> run/action -> delete",
		"provider-only managed behavior not required by migrated workloads",
		"blocked until a backend is selected",
		"the selected backend",
		"Actions explicitly not supported first: provider billing controls, proprietary fleet automation, and managed cross-region behavior outside the listed actions.",
		"Provider errors: return AWS access denied, not found, conflict/already-exists, validation, throttling/quota, and internal-error shapes with request/correlation ids.",
		"Provider errors: return Azure access denied, not found, conflict/already-exists, validation, throttling/quota, and internal-error shapes with request/correlation ids.",
		"Provider errors: return GCP access denied, not found, conflict/already-exists, validation, throttling/quota, and internal-error shapes with request/correlation ids.",
		"Context: tenant/account/project, region/location, source IP, request id, user agent, tags/labels, session age, and MFA/managed-identity claims when present.",
		"Conditions: exact/wildcard action match, resource prefix match, tag/label equality, requested region/location, source IP CIDR, time window, and principal attributes.",
		"Response mapping: return provider ids, lifecycle states, operation ids, etags/versions, pagination tokens, and timestamps from HomePort metadata.",
		"`.`",
		"none yet.",
		"provider ids, names, metadata, lifecycle state, pagination tokens, and operation status map to backend-native records without leaking backend internals.",
		"generated runtime manifest",
		"provider-managed fleet automation",
		"commercial billing/quota administration",
		"provider error families above",
	}

	requiredPaths := make(map[string]bool, len(catalog.Services))
	for _, service := range catalog.Services {
		path := filepath.Join(root, "docs/compat-plans", service.Provider, slug(service.Service)+".md")
		requiredPaths[path] = true
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s/%s missing compatibility plan at %s", service.Provider, service.Service, path)
		}
		text := string(body)
		for _, section := range required {
			if !strings.Contains(text, section) {
				t.Fatalf("%s/%s plan missing section %q", service.Provider, service.Service, section)
			}
		}
		for _, phrase := range banned {
			if strings.Contains(text, phrase) {
				t.Fatalf("%s/%s plan contains generic placeholder %q", service.Provider, service.Service, phrase)
			}
		}
		if strings.Contains(text, "no resource type currently modeled in the ledger") &&
			!strings.Contains(text, "First concrete resource model to add:") {
			t.Fatalf("%s/%s plan has no modeled resource without a concrete resource-model gap", service.Provider, service.Service)
		}
		ledgerResourceLine := regexp.MustCompile(`(?m)^- Ledger resource types: .*$`).FindString(text)
		if len(service.ResourceTypes) == 0 {
			if ledgerResourceLine != "- Ledger resource types: no resource type currently modeled in the ledger." {
				t.Fatalf("%s/%s plan resource types drift from ledger: %s", service.Provider, service.Service, ledgerResourceLine)
			}
		} else {
			for _, resourceType := range service.ResourceTypes {
				if !strings.Contains(ledgerResourceLine, resourceType) {
					t.Fatalf("%s/%s plan missing ledger resource type %q in %s", service.Provider, service.Service, resourceType, ledgerResourceLine)
				}
			}
		}
		if regexp.MustCompile(`\bsource [A-Za-z0-9 /-]+ resource model\b|\bhomeport_[a-z0-9_]+_resource\b`).MatchString(text) {
			t.Fatalf("%s/%s plan contains placeholder resource model", service.Provider, service.Service)
		}
		resourceLine := regexp.MustCompile(`(?m)^- Resource: .*$`).FindString(text)
		if regexp.MustCompile(`\b(aws|google|azurerm)_[a-z0-9_]+\b`).MatchString(resourceLine) {
			t.Fatalf("%s/%s plan uses Terraform type in provider resource shape: %s", service.Provider, service.Service, resourceLine)
		}
		contractLine := regexp.MustCompile(`(?m)^- .*(SDK|REST client).* exercises .*$`).FindString(text)
		if strings.Contains(contractLine, ":") || strings.Contains(contractLine, "/read") || strings.Contains(contractLine, "/write") || strings.Contains(contractLine, "/delete") {
			t.Fatalf("%s/%s plan contract test exercises action labels instead of SDK operations: %s", service.Provider, service.Service, contractLine)
		}
		if regexp.MustCompile(`\b[A-Z][A-Za-z0-9]+Create -> [A-Z][A-Za-z0-9]+Get -> [A-Z][A-Za-z0-9]+List -> [A-Z][A-Za-z0-9]+Patch\b`).MatchString(contractLine) {
			t.Fatalf("%s/%s plan contract test contains generated SDK method names: %s", service.Provider, service.Service, contractLine)
		}
		if service.Provider == "gcp" && regexp.MustCompile(` -> (get|list|patch|delete|update|create)( ->| against|;|\\.)`).MatchString(contractLine) {
			t.Fatalf("%s/%s plan contract test contains shortened GCP REST method names: %s", service.Provider, service.Service, contractLine)
		}
		if regexp.MustCompile(`\b(DescribeLoadBalancer|ListLoadBalancer|UpdateLoadBalancer)\b`).MatchString(text) {
			t.Fatalf("%s/%s plan contains generated ELBv2 SDK method names", service.Provider, service.Service)
		}
		if regexp.MustCompile(`\b(DescribeHostedZone|ListHostedZone|UpdateHostedZone|CreateQueryExecution|DescribeQueryExecution|ListQueryExecution|UpdateQueryExecution|DeleteQueryExecution)\b`).MatchString(text) {
			t.Fatalf("%s/%s plan contains generated AWS SDK method names", service.Provider, service.Service)
		}
		if service.Provider == "aws" {
			for _, op := range regexp.MustCompile(`\bList[A-Z][A-Za-z0-9]+\b`).FindAllString(contractLine, -1) {
				if !strings.HasSuffix(op, "s") && !regexp.MustCompile(`[0-9]$`).MatchString(op) {
					t.Fatalf("%s/%s plan contains singular generated AWS list operation: %s", service.Provider, service.Service, op)
				}
			}
		}
		if service.Provider == "gcp" && regexp.MustCompile(`; subscriptions\.(create|get|list|delete)\b`).MatchString(text) {
			t.Fatalf("%s/%s plan contains partially qualified GCP Pub/Sub methods", service.Provider, service.Service)
		}
		if regexp.MustCompile(`(?m)^- Storage and metadata: Service data lives in .*; HomePort stores provider ids, schema/config metadata, policy bindings, backup handles, and audit events\.$`).MatchString(text) {
			t.Fatalf("%s/%s plan contains generic storage mapping", service.Provider, service.Service)
		}
		if regexp.MustCompile(`(?m)^- Request mapping: provider id/name/location/tags/body fields map to HomePort records and .+; backend-only fields stay out of provider responses\.$`).MatchString(text) {
			t.Fatalf("%s/%s plan contains generic request mapping", service.Provider, service.Service)
		}
		backendLine := regexp.MustCompile(`(?m)^- Backend: .*$`).FindString(text)
		backend := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(backendLine, "- Backend:")), ".")
		target := strings.TrimSuffix(strings.TrimSpace(service.Target), ".")
		if target == "" {
			if backend != "Not selected in `docs/coverage/services.yaml`" {
				t.Fatalf("%s/%s plan backend drifts from missing ledger target: %s", service.Provider, service.Service, backendLine)
			}
		} else if backend != target {
			t.Fatalf("%s/%s plan backend %q drifts from ledger target %q", service.Provider, service.Service, backend, target)
		}
		if regexp.MustCompile(`^- Backend: [A-Z][A-Za-z0-9 /&+.-]+ adapter\.$`).MatchString(backendLine) &&
			!strings.Contains(backendLine, " with ") &&
			!strings.Contains(backendLine, "HomePort") &&
			!strings.Contains(backendLine, "Ollama") {
			t.Fatalf("%s/%s plan uses placeholder backend: %s", service.Provider, service.Service, backendLine)
		}
		if strings.Contains(text, "new the source ") || strings.Contains(text, " model model") {
			t.Fatalf("%s/%s plan contains a malformed generated model sentence", service.Provider, service.Service)
		}
		if service.Provider == "gcp" && regexp.MustCompile(`(?m)^- Initial supported surface: [a-z]+\.get, [a-z]+\.list, [a-z]+\.create`).MatchString(text) {
			t.Fatalf("%s/%s plan contains generated GCP pseudo-methods", service.Provider, service.Service)
		}
		if service.Provider == "gcp" && regexp.MustCompile(`(?m)^- Initial supported surface: [a-z0-9]+\.[A-Z][A-Za-z0-9]+Create`).MatchString(text) {
			t.Fatalf("%s/%s plan contains generated GCP SDK pseudo-methods", service.Provider, service.Service)
		}
	}

	plans, err := filepath.Glob(filepath.Join(root, "docs/compat-plans", "*", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range plans {
		if !requiredPaths[path] {
			t.Fatalf("compatibility plan %s is not in docs/coverage/services.yaml", path)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(value string) string {
	value = strings.ToLower(strings.ReplaceAll(value, "&", " and "))
	return strings.Trim(nonSlug.ReplaceAllString(value, "-"), "-")
}
