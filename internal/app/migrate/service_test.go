package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	domainappchange "github.com/homeport/homeport/internal/domain/appchange"
)

type failingAppChangeScanner struct{}

func (failingAppChangeScanner) ScanPath(string) (domainappchange.Report, error) {
	return domainappchange.Report{}, errors.New("scan failed")
}

func TestAnalyzeReturnsAppChangeScanErrors(t *testing.T) {
	service := NewService()
	service.appChangeScanner = failingAppChangeScanner{}

	_, err := service.Analyze(context.Background(), AnalyzeRequest{
		Type:    "terraform",
		Content: `resource "aws_s3_bucket" "assets" { bucket = "assets" }`,
	})

	if err == nil || !strings.Contains(err.Error(), "scan application changes") {
		t.Fatalf("expected app-change scan error, got %v", err)
	}
}
