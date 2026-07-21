package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/homeport/homeport/internal/app/awsoperations"
	"github.com/homeport/homeport/internal/app/migrate"
)

func TestCutoverPreview(t *testing.T) {
	handler := NewCutoverHandler()
	req := httptest.NewRequest(http.MethodPost, "/cutover/preview", strings.NewReader(`{"bundle_id":"b1","domain":"example.com","target_ip":"203.0.113.10"}`))
	rec := httptest.NewRecorder()
	router := chi.NewRouter()
	handler.RegisterRoutes(router)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "203.0.113.10") {
		t.Fatalf("missing target IP in response: %s", rec.Body.String())
	}
}

func TestCutoverActivatesAWSWorkspaceOnlyAfterSuccessfulCompletion(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{
		{ID: "lambda-1", Name: "thumbnailer", Type: "aws_lambda_function"},
		{ID: "queue-1", Name: "images", Type: "aws_sqs_queue"},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	operations := awsoperations.NewService(discoveries, workspaces)
	handler := NewCutoverHandlerWithAWSOperations(operations)
	registerTrustedLambdaBinding(t, handler, discovery.ID)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	start := httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","discovery_id":"`+discovery.ID+`","target_stack_id":"default","activated_services":["lambda"]}`))
	startRec := httptest.NewRecorder()
	router.ServeHTTP(startRec, start)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", startRec.Code, startRec.Body.String())
	}
	var started CreateCutoverResponse
	if err := json.Unmarshal(startRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if _, err := operations.GetByDiscoveryID(discovery.ID); err == nil {
		t.Fatal("workspace exists before successful cutover completion")
	}

	stream := httptest.NewRequest(http.MethodGet, "/cutover/"+started.CutoverID+"/stream", nil)
	streamRec := httptest.NewRecorder()
	router.ServeHTTP(streamRec, stream)
	if streamRec.Code != http.StatusOK {
		t.Fatalf("stream status = %d: %s", streamRec.Code, streamRec.Body.String())
	}

	workspace, err := operations.GetByDiscoveryID(discovery.ID)
	if err != nil {
		t.Fatalf("GetByDiscoveryID() after successful cutover error = %v", err)
	}
	if got := workspace.Services[awsoperations.ServiceLambda].Status; got != awsoperations.ServiceStatusAvailable {
		t.Errorf("lambda status = %q, want available", got)
	}
	if got := workspace.Services[awsoperations.ServiceSQS].Status; got != awsoperations.ServiceStatusUnavailable {
		t.Errorf("sqs status = %q, want unavailable", got)
	}
}

func TestCutoverActivatesAWSWorkspaceWithoutOpeningStream(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{{ID: "lambda-1", Name: "thumbnailer", Type: "aws_lambda_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	operations := awsoperations.NewService(discoveries, workspaces)
	handler := NewCutoverHandlerWithAWSOperations(operations)
	registerTrustedLambdaBinding(t, handler, discovery.ID)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	start := httptest.NewRecorder()
	router.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","discovery_id":"`+discovery.ID+`","target_stack_id":"default","activated_services":["lambda"]}`)))
	if start.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", start.Code, start.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for {
		workspace, getErr := operations.GetByDiscoveryID(discovery.ID)
		if getErr == nil {
			if workspace.Services[awsoperations.ServiceLambda].Status != awsoperations.ServiceStatusAvailable {
				t.Fatalf("lambda status = %q, want available", workspace.Services[awsoperations.ServiceLambda].Status)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace was not activated without stream: %v", getErr)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCutoverRejectsIncompleteAWSActivationRequest(t *testing.T) {
	handler := NewCutoverHandler()
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","discovery_id":"discovery-1"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestValidateAWSActivationAcceptsEveryRegisteredCatalogService(t *testing.T) {
	for _, metadata := range awsoperations.RegisteredServices() {
		err := validateAWSActivation(CreateCutoverRequest{
			DiscoveryID:       "discovery-1",
			TargetStackID:     "default",
			ActivatedServices: []awsoperations.ServiceKey{metadata.Key},
		})
		if err != nil {
			t.Errorf("validateAWSActivation(%q) error = %v", metadata.Key, err)
		}
	}
}

func TestCutoverRejectsUnknownActivationRequestField(t *testing.T) {
	handler := NewCutoverHandler()
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","capabilities":["invoke"]}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCutoverDryRunRejectsAWSActivation(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{{ID: "lambda-1", Type: "aws_lambda_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	operations := awsoperations.NewService(discoveries, store)
	handler := NewCutoverHandlerWithAWSOperations(operations)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","dry_run":true,"discovery_id":"`+discovery.ID+`","target_stack_id":"default","activated_services":["lambda"]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := operations.GetByDiscoveryID(discovery.ID); err == nil {
		t.Fatal("workspace exists for dry-run cutover")
	}
}

func TestCutoverValidatesAWSDiscoveryBeforeCreatingPlan(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	unsupported, err := discoveries.Save("Unsupported", "aws", nil, []migrate.ResourceInfo{{ID: "bucket-1", Type: "aws_s3_bucket"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	nonAWS, err := discoveries.Save("Non AWS", "gcp", nil, []migrate.ResourceInfo{{ID: "function-1", Type: "google_cloudfunctions_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	handler := NewCutoverHandlerWithAWSOperations(awsoperations.NewService(discoveries, workspaces))
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	for _, body := range []string{
		`{"bundle_id":"b1","discovery_id":"missing","target_stack_id":"default","activated_services":["lambda"]}`,
		`{"bundle_id":"b1","discovery_id":"` + nonAWS.ID + `","target_stack_id":"default","activated_services":["lambda"]}`,
		`{"bundle_id":"b1","discovery_id":"` + unsupported.ID + `","target_stack_id":"default","activated_services":["lambda"]}`,
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	}
}

func TestCutoverFailedActivationDoesNotCreateAWSWorkspace(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{{ID: "lambda-1", Name: "thumbnailer", Type: "aws_lambda_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	operations := awsoperations.NewService(discoveries, workspaces)
	handler := NewCutoverHandlerWithAWSOperations(operations)
	registerTrustedLambdaBinding(t, handler, discovery.ID)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	start := httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","discovery_id":"`+discovery.ID+`","target_stack_id":"default","activated_services":["lambda"],"pre_checks":[{"id":"broken","type":"unsupported","endpoint":"ignored"}]}`))
	startRec := httptest.NewRecorder()
	router.ServeHTTP(startRec, start)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", startRec.Code, startRec.Body.String())
	}
	var started CreateCutoverResponse
	if err := json.Unmarshal(startRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}

	streamRec := httptest.NewRecorder()
	router.ServeHTTP(streamRec, httptest.NewRequest(http.MethodGet, "/cutover/"+started.CutoverID+"/stream", nil))
	if !strings.Contains(streamRec.Body.String(), "step_failed") {
		t.Fatalf("stream = %q, want step_failed event", streamRec.Body.String())
	}
	if _, err := operations.GetByDiscoveryID(discovery.ID); err == nil {
		t.Fatal("workspace exists after failed cutover")
	}
}

func TestCutoverStreamDeliversRollbackTerminalEventsAfterPostCheckFailure(t *testing.T) {
	handler := NewCutoverHandler()
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	start := httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","post_checks":[{"id":"broken","type":"unsupported","endpoint":"ignored"}]}`))
	startRec := httptest.NewRecorder()
	router.ServeHTTP(startRec, start)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", startRec.Code, startRec.Body.String())
	}
	var started CreateCutoverResponse
	if err := json.Unmarshal(startRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}

	streamRec := httptest.NewRecorder()
	router.ServeHTTP(streamRec, httptest.NewRequest(http.MethodGet, "/cutover/"+started.CutoverID+"/stream", nil))
	stream := streamRec.Body.String()
	for _, event := range []string{"event: step_failed", "event: rollback", "event: complete"} {
		if !strings.Contains(stream, event) {
			t.Errorf("stream = %q, missing %q", stream, event)
		}
	}
}

func TestCutoverCancelledActivationDoesNotCreateAWSWorkspace(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{{ID: "lambda-1", Name: "thumbnailer", Type: "aws_lambda_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	workspaces, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	operations := awsoperations.NewService(discoveries, workspaces)
	handler := NewCutoverHandlerWithAWSOperations(operations)
	registerTrustedLambdaBinding(t, handler, discovery.ID)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	start := httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","discovery_id":"`+discovery.ID+`","target_stack_id":"default","activated_services":["lambda"],"pre_checks":[{"id":"slow","type":"http","endpoint":"http://example.invalid"}]}`))
	startRec := httptest.NewRecorder()
	router.ServeHTTP(startRec, start)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", startRec.Code, startRec.Body.String())
	}
	var started CreateCutoverResponse
	if err := json.Unmarshal(startRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}

	ctx, stopStream := context.WithCancel(context.Background())
	defer stopStream()
	done := make(chan struct{})
	go func() {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/cutover/"+started.CutoverID+"/stream", nil).WithContext(ctx))
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		exec, err := handler.service.GetPlan(started.CutoverID)
		if err == nil && exec.StartedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cutover did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancelRec := httptest.NewRecorder()
	router.ServeHTTP(cancelRec, httptest.NewRequest(http.MethodPost, "/cutover/"+started.CutoverID+"/cancel", nil))
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d: %s", cancelRec.Code, cancelRec.Body.String())
	}
	stopStream()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not stop after cancellation")
	}
	if _, err := operations.GetByDiscoveryID(discovery.ID); err == nil {
		t.Fatal("workspace exists after cancelled cutover")
	}
}

func TestCutoverDoesNotRetryActivationFromSSEAfterWorkspacePersistenceFailure(t *testing.T) {
	discoveries, err := migrate.NewStateStore(filepath.Join(t.TempDir(), "discoveries.json"))
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	discovery, err := discoveries.Save("Production", "aws", nil, []migrate.ResourceInfo{{ID: "lambda-1", Type: "aws_lambda_function"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	store, err := awsoperations.NewStore(filepath.Join(t.TempDir(), "workspaces.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	operations := awsoperations.NewService(discoveries, &failOnceWorkspaceStore{store: store})
	handler := NewCutoverHandlerWithAWSOperations(operations)
	registerTrustedLambdaBinding(t, handler, discovery.ID)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	start := httptest.NewRequest(http.MethodPost, "/cutover/start", strings.NewReader(`{"bundle_id":"b1","discovery_id":"`+discovery.ID+`","target_stack_id":"default","activated_services":["lambda"]}`))
	startRec := httptest.NewRecorder()
	router.ServeHTTP(startRec, start)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", startRec.Code, startRec.Body.String())
	}
	var started CreateCutoverResponse
	if err := json.Unmarshal(startRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/cutover/"+started.CutoverID+"/stream", nil))
	if !strings.Contains(first.Body.String(), "activation_error") || strings.Contains(first.Body.String(), "event: complete") {
		t.Fatalf("first stream = %q, want activation_error without complete", first.Body.String())
	}
	if _, err := operations.GetByDiscoveryID(discovery.ID); err == nil {
		t.Fatal("workspace exists after failed persistence")
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/cutover/"+started.CutoverID+"/stream", nil))
	if !strings.Contains(second.Body.String(), "activation_error") || strings.Contains(second.Body.String(), "event: complete") {
		t.Fatalf("second stream = %q, want replayed activation_error without SSE retry", second.Body.String())
	}
	if _, err := operations.GetByDiscoveryID(discovery.ID); err == nil {
		t.Fatal("workspace exists after failed activation replay")
	}
}

type failOnceWorkspaceStore struct {
	store *awsoperations.Store
	once  sync.Once
}

func (s *failOnceWorkspaceStore) Create(workspace *awsoperations.Workspace) (*awsoperations.Workspace, error) {
	failed := false
	s.once.Do(func() { failed = true })
	if failed {
		return nil, fmt.Errorf("simulated workspace persistence failure")
	}
	return s.store.Create(workspace)
}

func (s *failOnceWorkspaceStore) Get(id string) (*awsoperations.Workspace, error) {
	return s.store.Get(id)
}

func (s *failOnceWorkspaceStore) List() ([]*awsoperations.Workspace, error) {
	return s.store.List()
}

func (s *failOnceWorkspaceStore) GetByDiscoveryID(discoveryID string) (*awsoperations.Workspace, error) {
	return s.store.GetByDiscoveryID(discoveryID)
}

func registerTrustedLambdaBinding(t *testing.T, handler *CutoverHandler, discoveryID string) {
	t.Helper()
	if err := handler.RegisterAWSLocalBindings(discoveryID, "default", []awsoperations.LocalResourceBinding{{
		ImportedResourceID: "lambda-1", LocalResourceID: "c3d6c8a1-7e03-4f99-b24a-a8ea88f22d1e", LocalStackID: "default",
	}}); err != nil {
		t.Fatalf("RegisterAWSLocalBindings() error = %v", err)
	}
}
