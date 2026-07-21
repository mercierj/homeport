package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/homeport/homeport/internal/app/awsoperations"
	"github.com/homeport/homeport/internal/app/functions"
	"github.com/homeport/homeport/internal/app/migrate"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestAWSOperationsListsOnlyPersistedAWSWorkspaces(t *testing.T) {
	handler, workspace := newAWSOperationsHandlerFixture(t)
	server := newAWSOperationsTestServer(t, handler)

	response := getAWSOperations(t, server, "/aws/operations/workspaces")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET workspaces status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var body struct {
		Workspaces []struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
		} `json:"workspaces"`
	}
	decodeAWSOperationsJSON(t, response, &body)
	if len(body.Workspaces) != 1 || body.Workspaces[0].ID != workspace.ID || body.Workspaces[0].Provider != "aws" {
		t.Fatalf("workspaces = %#v, want persisted AWS workspace %q", body.Workspaces, workspace.ID)
	}
}

func TestAWSOperationsDoesNotExposeNonAWSWorkspaceData(t *testing.T) {
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if _, err := store.Create(&awsoperations.Workspace{ID: "gcp-workspace", Provider: "gcp"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	router := chi.NewRouter()
	router.Route("/aws/operations", NewAWSOperationsHandler(awsoperations.NewService(nil, store)).RegisterRoutes)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	response := getAWSOperations(t, server, "/aws/operations/workspaces")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET workspaces status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var body struct {
		Workspaces []any `json:"workspaces"`
	}
	decodeAWSOperationsJSON(t, response, &body)
	if len(body.Workspaces) != 0 {
		t.Fatalf("workspaces = %#v, want no non-AWS data", body.Workspaces)
	}
}

func TestAWSOperationsWorkspaceDetailAndUnknownWorkspace(t *testing.T) {
	handler, workspace := newAWSOperationsHandlerFixture(t)
	server := newAWSOperationsTestServer(t, handler)

	response := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET workspace status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var body struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Bindings []any  `json:"bindings"`
		Services map[string]struct {
			Capabilities []string `json:"capabilities"`
		} `json:"services"`
	}
	decodeAWSOperationsJSON(t, response, &body)
	if body.ID != workspace.ID || body.Provider != "aws" || body.Bindings == nil {
		t.Fatalf("workspace = %#v, want AWS workspace with non-null bindings", body)
	}
	if !slices.Equal(body.Services["lambda"].Capabilities, []string{"list", "read", "update", "delete", "invoke", "logs"}) {
		t.Fatalf("workspace Lambda capabilities = %v, want Lambda capabilities", body.Services["lambda"].Capabilities)
	}

	missing := getAWSOperations(t, server, "/aws/operations/workspaces/missing")
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("GET missing workspace status = %d, want %d", missing.StatusCode, http.StatusNotFound)
	}
}

func TestAWSOperationsServicesExposeCapabilitiesAndOnlyAvailableResources(t *testing.T) {
	handler, workspace := newAWSOperationsHandlerFixture(t)
	server := newAWSOperationsTestServer(t, handler)

	services := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services")
	if services.StatusCode != http.StatusOK {
		t.Fatalf("GET services status = %d, want %d", services.StatusCode, http.StatusOK)
	}
	var serviceBody struct {
		WorkspaceID string `json:"workspace_id"`
		Services    []struct {
			Service      string   `json:"service"`
			Status       string   `json:"status"`
			Capabilities []string `json:"capabilities"`
		} `json:"services"`
	}
	decodeAWSOperationsJSON(t, services, &serviceBody)
	if serviceBody.WorkspaceID != workspace.ID || len(serviceBody.Services) != 2 {
		t.Fatalf("services = %#v, want workspace %q and two services", serviceBody, workspace.ID)
	}
	if serviceBody.Services[0].Service != "lambda" || serviceBody.Services[0].Status != "available" || !slices.Equal(serviceBody.Services[0].Capabilities, []string{"list", "read", "update", "delete", "invoke", "logs"}) {
		t.Fatalf("first service = %#v, want available Lambda capabilities", serviceBody.Services[0])
	}
	if serviceBody.Services[1].Service != "sqs" || !slices.Equal(serviceBody.Services[1].Capabilities, []string{"list", "read", "delete", "purge", "retry"}) {
		t.Fatalf("second service = %#v, want SQS capabilities", serviceBody.Services[1])
	}

	lambda := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services/lambda/resources")
	if lambda.StatusCode != http.StatusOK {
		t.Fatalf("GET Lambda resources status = %d, want %d", lambda.StatusCode, http.StatusOK)
	}
	var lambdaBody struct {
		WorkspaceID string `json:"workspace_id"`
		Service     string `json:"service"`
		Resources   []struct {
			Name         string `json:"name"`
			LocalStackID string `json:"local_stack_id"`
		} `json:"resources"`
	}
	decodeAWSOperationsJSON(t, lambda, &lambdaBody)
	if lambdaBody.WorkspaceID != workspace.ID || lambdaBody.Service != "lambda" || len(lambdaBody.Resources) != 1 || lambdaBody.Resources[0].Name != "thumbnailer" || lambdaBody.Resources[0].LocalStackID != "default" {
		t.Fatalf("Lambda resources = %#v, want bound Lambda resource", lambdaBody)
	}

	sqs := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services/sqs/resources")
	defer sqs.Body.Close()
	if sqs.StatusCode != http.StatusOK {
		t.Fatalf("GET unavailable SQS resources status = %d, want %d", sqs.StatusCode, http.StatusOK)
	}
}

func TestAWSOperationsListsEveryPersistedCatalogService(t *testing.T) {
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	states := make(map[awsoperations.ServiceKey]awsoperations.ServiceState)
	for _, metadata := range awsoperations.RegisteredServices() {
		states[metadata.Key] = awsoperations.ServiceState{Status: awsoperations.ServiceStatusUnavailable, Capabilities: []awsoperations.Capability{}, Reason: "local target is unavailable"}
	}
	workspace, err := store.Create(&awsoperations.Workspace{ID: "all-services", Provider: "aws", Services: states})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	server := newAWSOperationsTestServer(t, NewAWSOperationsHandler(awsoperations.NewService(nil, store)))
	response := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET services status = %d", response.StatusCode)
	}
	var body struct {
		Services []struct {
			Service string `json:"service"`
		} `json:"services"`
	}
	decodeAWSOperationsJSON(t, response, &body)
	if len(body.Services) != len(awsoperations.RegisteredServices()) {
		t.Fatalf("services = %d, want %d", len(body.Services), len(awsoperations.RegisteredServices()))
	}
}

func TestAWSOperationsListsUnavailableServiceResourcesThroughGenericRoute(t *testing.T) {
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	workspace, err := store.Create(&awsoperations.Workspace{ID: "s3-workspace", Provider: "aws", Services: map[awsoperations.ServiceKey]awsoperations.ServiceState{"s3": {Status: awsoperations.ServiceStatusUnavailable, Capabilities: []awsoperations.Capability{}, Reason: "local target unavailable"}}, Bindings: []awsoperations.ResourceBinding{{ImportedResourceID: "bucket-1", Service: "s3", Name: "assets", LocalStackID: "storage"}}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	server := newAWSOperationsTestServer(t, NewAWSOperationsHandler(awsoperations.NewService(nil, store)))
	response := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services/s3/resources")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET generic S3 resources status = %d", response.StatusCode)
	}
	var body struct {
		Service   string `json:"service"`
		Resources []struct {
			ImportedResourceID string `json:"imported_resource_id"`
			Status             string `json:"status"`
			Reason             string `json:"reason"`
		} `json:"resources"`
	}
	decodeAWSOperationsJSON(t, response, &body)
	if body.Service != "s3" || len(body.Resources) != 1 || body.Resources[0].ImportedResourceID != "bucket-1" || body.Resources[0].Status != "unavailable" || body.Resources[0].Reason == "" {
		t.Fatalf("generic S3 response = %#v, want truthful unavailable resource", body)
	}
}

func TestAWSOperationsGetsUnavailableServiceResourceThroughGenericRoute(t *testing.T) {
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	workspace, err := store.Create(&awsoperations.Workspace{ID: "s3-resource", Provider: "aws", Services: map[awsoperations.ServiceKey]awsoperations.ServiceState{"s3": {Status: awsoperations.ServiceStatusUnavailable, Capabilities: []awsoperations.Capability{}, Reason: "local target unavailable"}}, Bindings: []awsoperations.ResourceBinding{{ImportedResourceID: "bucket-1", Service: "s3", LocalResourceID: "assets-local", Name: "assets", LocalStackID: "storage"}}})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	server := newAWSOperationsTestServer(t, NewAWSOperationsHandler(awsoperations.NewService(nil, store)))
	response := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services/s3/resources/bucket-1")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET generic S3 resource status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var body struct {
		Service  string `json:"service"`
		Resource struct {
			ImportedResourceID string `json:"imported_resource_id"`
			Status             string `json:"status"`
			Reason             string `json:"reason"`
		} `json:"resource"`
	}
	decodeAWSOperationsJSON(t, response, &body)
	if body.Service != "s3" || body.Resource.ImportedResourceID != "bucket-1" || body.Resource.Status != "unavailable" || body.Resource.Reason == "" {
		t.Fatalf("generic S3 detail = %#v, want truthful unavailable resource", body)
	}
}

func TestAWSOperationsGenericRoutesCoverEveryCatalogService(t *testing.T) {
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	states := make(map[awsoperations.ServiceKey]awsoperations.ServiceState)
	bindings := make([]awsoperations.ResourceBinding, 0, len(awsoperations.RegisteredServices()))
	for index, metadata := range awsoperations.RegisteredServices() {
		states[metadata.Key] = awsoperations.ServiceState{Status: awsoperations.ServiceStatusUnavailable, Capabilities: []awsoperations.Capability{}, Reason: "local target unavailable"}
		bindings = append(bindings, awsoperations.ResourceBinding{ImportedResourceID: fmt.Sprintf("import-%d", index), Service: metadata.Key, Name: metadata.DisplayName, LocalStackID: "local"})
	}
	workspace, err := store.Create(&awsoperations.Workspace{ID: "all-services-generic", Provider: "aws", Services: states, Bindings: bindings})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	server := newAWSOperationsTestServer(t, NewAWSOperationsHandler(awsoperations.NewService(nil, store)))
	for _, metadata := range awsoperations.RegisteredServices() {
		response := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services/"+string(metadata.Key)+"/resources")
		if response.StatusCode != http.StatusOK {
			t.Errorf("GET resources for %q status = %d", metadata.Key, response.StatusCode)
		}
		response.Body.Close()
	}
}

func TestAWSOperationsRejectsUnboundResourcesAndUnavailableServices(t *testing.T) {
	handler, workspace := newAWSOperationsHandlerFixture(t)
	server := newAWSOperationsTestServer(t, handler)

	unbound := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services/lambda/resources/not-bound")
	defer unbound.Body.Close()
	if unbound.StatusCode != http.StatusForbidden {
		t.Fatalf("GET unbound function status = %d, want %d", unbound.StatusCode, http.StatusForbidden)
	}

	unavailable := getAWSOperations(t, server, "/aws/operations/workspaces/"+workspace.ID+"/services/sqs/resources")
	defer unavailable.Body.Close()
	if unavailable.StatusCode != http.StatusOK {
		t.Fatalf("GET unavailable SQS status = %d, want %d", unavailable.StatusCode, http.StatusOK)
	}
}

func TestAWSOperationsAuthorizesAndAuditsLambdaUpdateBeforeBackendDispatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed bool
	}{
		{name: "allowed", allowed: true},
		{name: "denied", allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			functions := &awsOperationsFunctionsBackend{}
			audit := authz.NewAuditLog()
			handler, workspace := newAuthorizedAWSOperationsHandler(t, functions, &awsOperationsQueuesBackend{}, tc.allowed, audit)
			server := newAWSOperationsTestServer(t, handler)

			request, err := http.NewRequest(http.MethodPut, server.URL+"/aws/operations/workspaces/"+workspace.ID+"/services/lambda/resources/function-local", strings.NewReader(`{"name":"thumbnailer"}`))
			if err != nil {
				t.Fatalf("NewRequest(): %v", err)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("PUT Lambda resource: %v", err)
			}
			defer response.Body.Close()

			wantStatus := http.StatusOK
			if !tc.allowed {
				wantStatus = http.StatusForbidden
			}
			if response.StatusCode != wantStatus {
				t.Fatalf("PUT Lambda resource status = %d, want %d", response.StatusCode, wantStatus)
			}
			if functions.updateCalls != boolToInt(tc.allowed) {
				t.Fatalf("backend update calls = %d, want %d", functions.updateCalls, boolToInt(tc.allowed))
			}
			assertAWSOperationsAuditDecision(t, audit.Decisions(), tc.allowed, "aws-operations:lambda:update", workspace.ID, "lambda", "function-local")
		})
	}
}

func TestAWSOperationsFailsClosedWhenMutationAuditIsUnavailable(t *testing.T) {
	functions := &awsOperationsFunctionsBackend{}
	handler, workspace := newAuthorizedAWSOperationsHandler(t, functions, &awsOperationsQueuesBackend{}, true, nil)
	server := newAWSOperationsTestServer(t, handler)
	request, err := http.NewRequest(http.MethodPut, server.URL+"/aws/operations/workspaces/"+workspace.ID+"/services/lambda/resources/function-local", strings.NewReader(`{"name":"thumbnailer"}`))
	if err != nil {
		t.Fatalf("NewRequest(): %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("PUT Lambda resource: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("PUT Lambda resource status = %d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}
	if functions.updateCalls != 0 {
		t.Fatalf("backend update calls = %d, want audit failure to prevent dispatch", functions.updateCalls)
	}
}

func TestAWSOperationsAuthorizesAndAuditsSQSPurgeBeforeBackendDispatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed bool
	}{
		{name: "allowed", allowed: true},
		{name: "denied", allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queues := &awsOperationsQueuesBackend{}
			audit := authz.NewAuditLog()
			handler, workspace := newAuthorizedAWSOperationsHandler(t, &awsOperationsFunctionsBackend{}, queues, tc.allowed, audit)
			server := newAWSOperationsTestServer(t, handler)

			request, err := http.NewRequest(http.MethodDelete, server.URL+"/aws/operations/workspaces/"+workspace.ID+"/services/sqs/resources/images-local/messages?status=failed", nil)
			if err != nil {
				t.Fatalf("NewRequest(): %v", err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("DELETE SQS messages: %v", err)
			}
			defer response.Body.Close()

			wantStatus := http.StatusOK
			if !tc.allowed {
				wantStatus = http.StatusForbidden
			}
			if response.StatusCode != wantStatus {
				t.Fatalf("DELETE SQS messages status = %d, want %d", response.StatusCode, wantStatus)
			}
			if queues.purgeCalls != boolToInt(tc.allowed) {
				t.Fatalf("backend purge calls = %d, want %d", queues.purgeCalls, boolToInt(tc.allowed))
			}
			assertAWSOperationsAuditDecision(t, audit.Decisions(), tc.allowed, "aws-operations:sqs:purge", workspace.ID, "sqs", "images-local")
		})
	}
}

func TestAWSOperationsDoesNotExposeLambdaCreateAndWrapsInvocation(t *testing.T) {
	handler, workspace := newAWSOperationsHandlerFixture(t)
	server := newAWSOperationsTestServer(t, handler)
	base := server.URL + "/aws/operations/workspaces/" + workspace.ID + "/services/lambda/resources"

	create, err := server.Client().Post(base, "application/json", bytes.NewBufferString(`{"name":"new","runtime":"nodejs20","handler":"index.handler"}`))
	if err != nil {
		t.Fatalf("POST create: %v", err)
	}
	defer create.Body.Close()
	if create.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST create status = %d, want %d", create.StatusCode, http.StatusMethodNotAllowed)
	}

	invocation, err := server.Client().Post(base+"/"+workspace.Bindings[0].LocalResourceID+"/invoke", "application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST invoke: %v", err)
	}
	var body struct {
		WorkspaceID string          `json:"workspace_id"`
		Service     string          `json:"service"`
		ResourceID  string          `json:"resource_id"`
		Status      string          `json:"status"`
		Result      json.RawMessage `json:"result"`
	}
	decodeAWSOperationsJSON(t, invocation, &body)
	if body.WorkspaceID != workspace.ID || body.Service != "lambda" || body.ResourceID != workspace.Bindings[0].LocalResourceID || body.Status != "invoked" || len(body.Result) == 0 {
		t.Fatalf("invocation response = %#v, want wrapped operation response", body)
	}
}

func newAWSOperationsHandlerFixture(t *testing.T) (*AWSOperationsHandler, *awsoperations.Workspace) {
	t.Helper()
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", []string{"eu-west-3"}, []migrate.ResourceInfo{
		{ID: "lambda-1", Name: "thumbnailer", Type: "aws_lambda_function", Region: "eu-west-3"},
		{ID: "queue-1", Name: "images", Type: "aws_sqs_queue", Region: "eu-west-3"},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	service := awsoperations.NewService(discoveries, store)
	functionsService, err := functions.NewService()
	if err != nil {
		t.Fatalf("NewService(): %v", err)
	}
	function, err := functionsService.CreateFunction(t.Context(), functions.FunctionConfig{Name: "thumbnailer", Runtime: functions.RuntimeNodeJS20, Handler: "index.handler"})
	if err != nil {
		t.Fatalf("CreateFunction(): %v", err)
	}
	workspace, err := service.Activate(awsoperations.ActivationInput{DiscoveryID: discovery.ID, TargetStackID: "default", Activated: []awsoperations.ServiceKey{awsoperations.ServiceLambda}, LocalBindings: []awsoperations.LocalResourceBinding{{ImportedResourceID: "lambda-1", LocalResourceID: function.ID, LocalStackID: "default"}}})
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	audit := authz.NewAuditLog()
	return NewAWSOperationsHandlerWithAuthorization(service, authz.AllowAll, func(decision authz.Decision) error { audit.Record(decision); return nil }, awsoperations.NewLambdaDriver(awsoperations.NewFunctionsBackend(functionsService))), workspace
}

func newAuthorizedAWSOperationsHandler(t *testing.T, functionsBackend awsoperations.FunctionsBackend, queuesBackend awsoperations.QueuesBackend, allowed bool, audit *authz.AuditLog) (*AWSOperationsHandler, *awsoperations.Workspace) {
	t.Helper()
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	workspace, err := store.Create(&awsoperations.Workspace{
		ID:       "operations-workspace",
		Provider: "aws",
		Services: map[awsoperations.ServiceKey]awsoperations.ServiceState{
			awsoperations.ServiceLambda: {Status: awsoperations.ServiceStatusAvailable, Capabilities: []awsoperations.Capability{awsoperations.CapabilityUpdate}},
			awsoperations.ServiceSQS:    {Status: awsoperations.ServiceStatusAvailable, Capabilities: []awsoperations.Capability{awsoperations.CapabilityPurge}},
		},
		Bindings: []awsoperations.ResourceBinding{
			{ImportedResourceID: "lambda-imported", Service: awsoperations.ServiceLambda, LocalResourceID: "function-local", LocalStackID: "stack-1"},
			{ImportedResourceID: "sqs-imported", Service: awsoperations.ServiceSQS, LocalResourceID: "images-local", LocalStackID: "stack-1"},
		},
	})
	if err != nil {
		t.Fatalf("Create workspace: %v", err)
	}
	authorizer := authz.AuthorizerFunc(func(_ context.Context, req authz.Request) (authz.Decision, error) {
		return authz.Decision{Request: req, Allowed: allowed, Reason: "test decision"}, nil
	})
	var auditSink func(authz.Decision) error
	if audit != nil {
		auditSink = func(decision authz.Decision) error { audit.Record(decision); return nil }
	}
	handler := NewAWSOperationsHandlerWithAuthorization(
		awsoperations.NewService(nil, store), authorizer, auditSink,
		awsoperations.NewLambdaDriver(functionsBackend), awsoperations.NewSQSDriver(queuesBackend),
	)
	return handler, workspace
}

func assertAWSOperationsAuditDecision(t *testing.T, decisions []authz.Decision, allowed bool, action, workspaceID, service, resourceID string) {
	t.Helper()
	if len(decisions) != 1 {
		t.Fatalf("audit decisions = %#v, want one", decisions)
	}
	decision := decisions[0]
	if decision.Allowed != allowed || decision.Request.Action != action {
		t.Fatalf("audit decision = %#v, want allowed=%t action=%q", decision, allowed, action)
	}
	if decision.Request.Context["workspace_id"] != workspaceID || decision.Request.Context["service"] != service || decision.Request.Context["bound_resource_id"] != resourceID {
		t.Fatalf("audit context = %#v, want workspace=%q service=%q bound resource=%q", decision.Request.Context, workspaceID, service, resourceID)
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type awsOperationsFunctionsBackend struct{ updateCalls int }

func (*awsOperationsFunctionsBackend) List(context.Context) ([]awsoperations.FunctionRecord, error) {
	return nil, nil
}
func (*awsOperationsFunctionsBackend) Get(context.Context, string) (*awsoperations.FunctionRecord, error) {
	return nil, nil
}
func (*awsOperationsFunctionsBackend) Create(context.Context, awsoperations.FunctionInput) (*awsoperations.FunctionRecord, error) {
	return nil, nil
}
func (b *awsOperationsFunctionsBackend) Update(_ context.Context, id string, _ awsoperations.FunctionInput) (*awsoperations.FunctionRecord, error) {
	b.updateCalls++
	return &awsoperations.FunctionRecord{ID: id}, nil
}
func (*awsOperationsFunctionsBackend) Delete(context.Context, string) error { return nil }
func (*awsOperationsFunctionsBackend) Invoke(context.Context, string, []byte) (*awsoperations.InvocationRecord, error) {
	return nil, nil
}
func (*awsOperationsFunctionsBackend) Logs(context.Context, string) ([]awsoperations.LogRecord, error) {
	return nil, nil
}

type awsOperationsQueuesBackend struct{ purgeCalls int }

func (*awsOperationsQueuesBackend) List(context.Context, string) ([]awsoperations.QueueRecord, error) {
	return nil, nil
}
func (*awsOperationsQueuesBackend) Messages(context.Context, string, string, string) ([]awsoperations.MessageRecord, error) {
	return nil, nil
}
func (*awsOperationsQueuesBackend) Retry(context.Context, string, string, string) error  { return nil }
func (*awsOperationsQueuesBackend) Delete(context.Context, string, string, string) error { return nil }
func (b *awsOperationsQueuesBackend) Purge(context.Context, string, string, string) (int64, error) {
	b.purgeCalls++
	return 1, nil
}

func newAWSOperationsTestServer(t *testing.T, handler *AWSOperationsHandler) *httptest.Server {
	t.Helper()
	router := chi.NewRouter()
	router.Route("/aws/operations", handler.RegisterRoutes)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func getAWSOperations(t *testing.T, server *httptest.Server, target string) *http.Response {
	t.Helper()
	response, err := server.Client().Get(server.URL + target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	return response

}

func decodeAWSOperationsJSON(t *testing.T, response *http.Response, into any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}
