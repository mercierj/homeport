package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/app/migrate"
)

func TestAnalyzeReturnsAppChangeReport(t *testing.T) {
	handler := &MigrateHandler{service: migrate.NewService()}
	body := `{"type":"terraform","content":"resource \"aws_s3_bucket\" \"assets\" { bucket = \"assets\" tags = { endpoint = \"storage.googleapis.com\" } }"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/migrate/analyze", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.HandleAnalyze(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		AppChangeReport struct {
			Changes []struct {
				Service string `json:"service"`
				Mode    string `json:"mode"`
				Search  string `json:"search"`
			} `json:"changes"`
		} `json:"app_change_report"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.AppChangeReport.Changes) != 1 ||
		got.AppChangeReport.Changes[0].Service != "Cloud Storage" ||
		got.AppChangeReport.Changes[0].Mode != "generated_patch" ||
		got.AppChangeReport.Changes[0].Search != "storage.googleapis.com" {
		t.Fatalf("app_change_report = %#v", got.AppChangeReport)
	}
}
