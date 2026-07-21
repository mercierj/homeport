package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	appcompat "github.com/homeport/homeport/internal/app/compat"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
)

func TestCompatHandlerRoutesServiceSubpathsToAdapter(t *testing.T) {
	registry := appcompat.NewRegistry()
	if err := registry.Register(compataws.NewLambdaAdapter()); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	NewCompatHandler(registry).RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/compat/aws/lambda/2015-03-31/functions", strings.NewReader(`{
		"FunctionName":"orders-handler",
		"Runtime":"nodejs20.x",
		"Role":"arn:aws:iam::000000000000:role/homeport",
		"Handler":"index.handler",
		"Code":{"ZipFile":"aG9tZXBvcnQ="}
	}`))
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s, want 201", resp.Code, resp.Body.String())
	}
}
