package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORSAllowsPatch(t *testing.T) {
	server, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/wizard/sessions/session-1", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	rec := httptest.NewRecorder()

	server.Router().ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPatch) {
		t.Fatalf("Access-Control-Allow-Methods = %q, want PATCH", got)
	}
}
