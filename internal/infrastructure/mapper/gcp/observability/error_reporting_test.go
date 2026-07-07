package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestErrorReportingConformanceManagedAToZ(t *testing.T) {
	result, err := NewErrorReportingMapper().Map(context.Background(), managedErrorReportingFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Error Reporting migration", result.ManualSteps)
	}
	if result.DockerService.Image != "getsentry/sentry:24.5.1" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Sentry target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/sentry/sentry.conf.py", "config/error-reporting/app-change.env", "config/error-reporting/generated-sentry.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/error-reporting/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_ERROR_REPORTING_SERVICE=clouderrorreporting.googleapis.com", "TARGET_ERROR_TRACKING=sentry", "SENTRY_DSN="} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_error_reporting_config.sh", "provision_sentry.sh", "migrate_error_reporting.sh", "validate_sentry.sh", "backup_error_reporting_config.sh", "cutover_error_reporting_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-error-reporting-config":   domainrunbook.StepTypeCommand,
		"provision-sentry":                domainrunbook.StepTypeCommand,
		"migrate-error-reporting":         domainrunbook.StepTypeCommand,
		"validate-sentry":                 domainrunbook.StepTypeCommand,
		"backup-error-reporting-config":   domainrunbook.StepTypeCommand,
		"cutover-error-reporting-clients": domainrunbook.StepTypeAPICall,
		"rollback-error-reporting-source": domainrunbook.StepTypeRollback,
	} {
		if !hasErrorReportingRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedErrorReportingFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/services/clouderrorreporting.googleapis.com",
		Type: resource.TypeErrorReportingService,
		Name: "clouderrorreporting.googleapis.com",
		Config: map[string]interface{}{
			"service": "clouderrorreporting.googleapis.com",
		},
	}
}

func hasErrorReportingRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
