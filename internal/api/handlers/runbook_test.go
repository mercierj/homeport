package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	apprunbook "github.com/homeport/homeport/internal/app/runbook"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestRunbookHandlerGet(t *testing.T) {
	handler := newTestRunbookHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runbooks/test", nil)
	rec := httptest.NewRecorder()

	handlerRouter(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRunbookHandlerRunStep(t *testing.T) {
	handler := newTestRunbookHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runbooks/test/steps/first/run", nil)
	rec := httptest.NewRecorder()

	handlerRouter(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	book, err := handler.service.Get("test")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := book.Steps[0].Status; got != domainrunbook.StepStatusPassed {
		t.Fatalf("step status = %q, want passed", got)
	}
}

func TestRunbookHandlerRunAll(t *testing.T) {
	handler := newTestRunbookHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runbooks/test/run", nil)
	rec := httptest.NewRecorder()

	handlerRouter(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRunbookHandlerRollback(t *testing.T) {
	handler := newTestRunbookHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runbooks/test/rollback", nil)
	rec := httptest.NewRecorder()

	handlerRouter(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func newTestRunbookHandler(t *testing.T) *RunbookHandler {
	t.Helper()
	service := apprunbook.NewService(t.TempDir())
	if err := service.Save(&domainrunbook.Runbook{
		ID:   "test",
		Name: "Test",
		Steps: []domainrunbook.Step{{
			ID:               "first",
			Name:             "First",
			Type:             domainrunbook.StepTypeCommand,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "passed",
		}, {
			ID:               "rollback",
			Name:             "Rollback",
			Type:             domainrunbook.StepTypeRollback,
			Status:           domainrunbook.StepStatusPending,
			Executor:         "noop",
			SuccessCondition: "passed",
		}},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return NewRunbookHandler(service)
}

func handlerRouter(handler *RunbookHandler) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", handler.RegisterRoutes)
	return r
}
