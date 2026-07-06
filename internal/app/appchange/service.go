package appchange

import (
	"os"
	"path/filepath"
	"strings"

	domain "github.com/homeport/homeport/internal/domain/appchange"
)

type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) ScanPath(root string) (domain.Report, error) {
	report := domain.Report{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		if strings.Contains(text, "AWS_S3_ENDPOINT") || strings.Contains(text, "s3.amazonaws.com") {
			report.Changes = append(report.Changes, domain.Change{Service: "S3", Mode: domain.ModeAdapter, File: path, Reason: "S3 endpoint can be redirected to MinIO", AdapterURL: "http://minio:9000"})
		}
		if strings.Contains(text, "storage.googleapis.com") {
			report.Changes = append(report.Changes, domain.Change{Service: "Cloud Storage", Mode: domain.ModeGeneratedPatch, File: path, Search: "storage.googleapis.com", Replace: "${HOMEPORT_STORAGE_ENDPOINT}", Reason: "Native GCS endpoint must point to the HomePort storage adapter", ValidationCmd: "grep -R HOMEPORT_STORAGE_ENDPOINT ."})
		}
		if strings.Contains(text, "servicebus.windows.net") {
			report.Changes = append(report.Changes, domain.Change{Service: "Service Bus", Mode: domain.ModeGeneratedPatch, File: path, Search: "servicebus.windows.net", Replace: "${HOMEPORT_SERVICEBUS_ENDPOINT}", Reason: "Azure Service Bus SDK endpoint must point to the HomePort adapter", ValidationCmd: "grep -R HOMEPORT_SERVICEBUS_ENDPOINT ."})
		}
		return nil
	})
	return report, err
}
