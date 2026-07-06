package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	appwizard "github.com/homeport/homeport/internal/app/wizard"
)

func TestWizardSessionCreatePatchGet(t *testing.T) {
	handler := NewWizardHandler(appwizard.NewService(t.TempDir()))
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/wizard/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	id := responseString(t, rec.Body.String(), `"id":"`, `"`)

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/wizard/sessions/"+id, bytes.NewBufferString(`{"current_step":"deploy","bundle_id":"bundle-1"}`))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"bundle_id":"bundle-1"`) {
		t.Fatalf("missing bundle id: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wizard/sessions/"+id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"current_step":"deploy"`) {
		t.Fatalf("missing patched step: %s", rec.Body.String())
	}
}

func TestWizardSessionPatchPreservesOmittedSecretsResolved(t *testing.T) {
	handler := NewWizardHandler(appwizard.NewService(t.TempDir()))
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/wizard/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	id := responseString(t, rec.Body.String(), `"id":"`, `"`)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/wizard/sessions/"+id, bytes.NewBufferString(`{"secrets_resolved":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch secrets status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/wizard/sessions/"+id, bytes.NewBufferString(`{"current_step":"deploy"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch step status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"secrets_resolved":true`) {
		t.Fatalf("secrets_resolved was not preserved: %s", rec.Body.String())
	}
}

func responseString(t *testing.T, body, prefix, suffix string) string {
	t.Helper()
	start := strings.Index(body, prefix)
	if start == -1 {
		t.Fatalf("missing %q in %s", prefix, body)
	}
	start += len(prefix)
	end := strings.Index(body[start:], suffix)
	if end == -1 {
		t.Fatalf("missing suffix %q in %s", suffix, body)
	}
	return body[start : start+end]
}
