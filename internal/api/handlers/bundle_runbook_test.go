package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	apprunbook "github.com/homeport/homeport/internal/app/runbook"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestBuildBundleRunbookUsesBundleIDAndGroups(t *testing.T) {
	book := buildBundleRunbook("bundle-1", true, []*SecretRef{{Name: "DB_PASSWORD", Required: true}})

	if book.ID != "bundle-1" {
		t.Fatalf("runbook id = %q, want bundle-1", book.ID)
	}
	wantGroups := []string{"Credentials", "Provision", "Sync", "Validate", "Cutover", "Rollback"}
	if len(book.Steps) != len(wantGroups) {
		t.Fatalf("len(Steps) = %d, want %d", len(book.Steps), len(wantGroups))
	}
	for i, group := range wantGroups {
		if book.Steps[i].Group != group {
			t.Fatalf("step %d group = %q, want %q", i, book.Steps[i].Group, group)
		}
	}
	if !book.Steps[len(book.Steps)-1].Optional {
		t.Fatal("rollback step should be optional for forward completion")
	}
	if got := book.Steps[0].Status; got != "blocked" {
		t.Fatalf("credentials step status = %q, want blocked", got)
	}
	if got := book.Steps[0].Executor; got != "user" {
		t.Fatalf("credentials step executor = %q, want user", got)
	}
}

func TestProvideSecretsPassesCredentialsRunbookStep(t *testing.T) {
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatal(err)
		}
	})

	handler := NewBundleHandler()
	handler.bundles["bundle-1"] = &BundleInfo{
		ID:      "bundle-1",
		Secrets: []*SecretRef{{Name: "DB_PASSWORD", Required: true}},
	}
	if err := apprunbook.NewService(".").Save(buildBundleRunbook("bundle-1", true, handler.bundles["bundle-1"].Secrets)); err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(ProvideSecretsRequest{Secrets: map[string]string{"DB_PASSWORD": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/bundles/bundle-1/secrets", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Post("/bundles/{bundleId}/secrets", handler.ProvideSecrets)

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	book, err := apprunbook.NewService(".").Get("bundle-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := book.Steps[0].Status; got != domainrunbook.StepStatusPassed {
		t.Fatalf("credentials step status = %q, want passed", got)
	}
}

func TestProvideSecretsSurvivesHandlerRestart(t *testing.T) {
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatal(err)
		}
	})

	handler := NewBundleHandler()
	handler.bundles["bundle-1"] = &BundleInfo{ID: "bundle-1", Secrets: []*SecretRef{{Name: "DB_PASSWORD", Required: true}}}
	if err := apprunbook.NewService(".").Save(buildBundleRunbook("bundle-1", true, handler.bundles["bundle-1"].Secrets)); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"secrets":{"DB_PASSWORD":"secret"}}`)
	req := httptest.NewRequest(http.MethodPost, "/bundles/bundle-1/secrets", body)
	rec := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Post("/bundles/{bundleId}/secrets", handler.ProvideSecrets)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	restarted := NewBundleHandler()
	restarted.bundles["bundle-1"] = handler.bundles["bundle-1"]
	req = httptest.NewRequest(http.MethodPost, "/bundles/bundle-1/secrets", bytes.NewBufferString(`{"secrets":{}}`))
	rec = httptest.NewRecorder()
	router = chi.NewRouter()
	router.Post("/bundles/{bundleId}/secrets", restarted.ProvideSecrets)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("unexpected response after restart: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProvideSecretsDoesNotOverwriteStoredSecretWithBlankValue(t *testing.T) {
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatal(err)
		}
	})

	handler := NewBundleHandler()
	handler.bundles["bundle-1"] = &BundleInfo{ID: "bundle-1", Secrets: []*SecretRef{{Name: "DB_PASSWORD", Required: true}}}
	if err := apprunbook.NewService(".").Save(buildBundleRunbook("bundle-1", true, handler.bundles["bundle-1"].Secrets)); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Post("/bundles/{bundleId}/secrets", handler.ProvideSecrets)

	req := httptest.NewRequest(http.MethodPost, "/bundles/bundle-1/secrets", bytes.NewBufferString(`{"secrets":{"DB_PASSWORD":"secret"}}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("unexpected initial response: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/bundles/bundle-1/secrets", bytes.NewBufferString(`{"secrets":{"DB_PASSWORD":""}}`))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("blank value overwrote stored secret: %d %s", rec.Code, rec.Body.String())
	}
}
