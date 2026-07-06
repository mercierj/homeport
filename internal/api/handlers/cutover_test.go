package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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
