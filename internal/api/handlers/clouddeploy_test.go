package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/homeport/homeport/internal/app/clouddeploy"
	"github.com/homeport/homeport/internal/app/migrate"
)

func TestCloudDeployStartRejectsInvalidProvider(t *testing.T) {
	handler := NewCloudDeployHandler(clouddeploy.NewService(t.TempDir()), migrate.NewService())
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/cloud-deploy/start", strings.NewReader(`{"config":{"provider":"aws"}}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCloudDeployGetMissingJob(t *testing.T) {
	handler := NewCloudDeployHandler(clouddeploy.NewService(t.TempDir()), migrate.NewService())
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/cloud-deploy/missing", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}
