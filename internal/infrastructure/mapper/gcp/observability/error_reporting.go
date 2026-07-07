package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type ErrorReportingMapper struct {
	*mapper.BaseMapper
}

func NewErrorReportingMapper() *ErrorReportingMapper {
	return &ErrorReportingMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeErrorReportingService, nil)}
}

func (m *ErrorReportingMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	serviceName := firstNonEmpty(res.GetConfigString("service"), res.GetConfigString("name"), "clouderrorreporting.googleapis.com")
	project := firstNonEmpty(res.GetConfigString("project"), res.GetConfigString("project_id"), "gcp-project")

	result := mapper.NewMappingResult("sentry")
	svc := result.DockerService
	svc.Image = "getsentry/sentry:24.5.1"
	svc.Ports = []string{"9000:9000"}
	svc.Volumes = []string{"./config/sentry:/etc/sentry", "./data/sentry:/var/lib/sentry/files"}
	svc.Environment = map[string]string{"SENTRY_SECRET_KEY": "${SENTRY_SECRET_KEY:-homeport-change-me}", "SENTRY_POSTGRES_HOST": "sentry-postgres", "SENTRY_REDIS_HOST": "sentry-redis"}
	svc.DependsOn = []string{"sentry-postgres", "sentry-redis"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "curl -fsS http://localhost:9000/_health/ >/dev/null || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeErrorReportingService), "homeport.service": serviceName, "homeport.target": "sentry"}

	result.AddService(&mapper.DockerService{Name: "sentry-postgres", Image: "postgres:15-alpine", Environment: map[string]string{"POSTGRES_USER": "sentry", "POSTGRES_PASSWORD": "${SENTRY_DB_PASSWORD:-sentry}", "POSTGRES_DB": "sentry"}, Volumes: []string{"./data/sentry-postgres:/var/lib/postgresql/data"}, Networks: []string{"homeport"}, Restart: "unless-stopped"})
	result.AddService(&mapper.DockerService{Name: "sentry-redis", Image: "redis:7-alpine", Volumes: []string{"./data/sentry-redis:/data"}, Networks: []string{"homeport"}, Restart: "unless-stopped"})
	result.AddConfig("config/sentry/sentry.conf.py", []byte(m.sentryConfig(project)))
	result.AddConfig("config/error-reporting/app-change.env", []byte(m.appChange(serviceName)))
	result.AddConfig("config/error-reporting/generated-sentry.patch", []byte(m.generatedPatch()))
	result.AddScript("export_error_reporting_config.sh", []byte(m.exportScript(serviceName, project)))
	result.AddScript("provision_sentry.sh", []byte(m.provisionScript()))
	result.AddScript("migrate_error_reporting.sh", []byte(m.migrateScript(serviceName)))
	result.AddScript("validate_sentry.sh", []byte(m.validateScript(serviceName)))
	result.AddScript("backup_error_reporting_config.sh", []byte(m.backupScript(serviceName)))
	result.AddScript("cutover_error_reporting_clients.sh", []byte(m.cutoverScript(serviceName)))
	for _, step := range errorReportingRunbook(serviceName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *ErrorReportingMapper) sentryConfig(project string) string {
	return fmt.Sprintf("system.url-prefix = 'http://sentry:9000'\n# source_gcp_project = %q\n", project)
}

func (m *ErrorReportingMapper) appChange(serviceName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_ERROR_REPORTING_SERVICE=%s\nTARGET_ERROR_TRACKING=sentry\nSENTRY_DSN=${SENTRY_DSN}\nGENERATED_PATCH=config/error-reporting/generated-sentry.patch\n", serviceName)
}

func (m *ErrorReportingMapper) generatedPatch() string {
	return "--- a/app/errors.env\n+++ b/app/errors.env\n@@\n-GOOGLE_CLOUD_ERROR_REPORTING=true\n+ERROR_TRACKING_BACKEND=sentry\n+SENTRY_DSN=${SENTRY_DSN}\n"
}

func (m *ErrorReportingMapper) exportScript(serviceName, project string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p error-reporting-export\ngcloud services list --enabled --project=%q --filter='config.name=%s' --format=json > error-reporting-export/service.json\ngcloud logging read 'severity>=ERROR' --project=%q --limit=\"${ERROR_SAMPLE_LIMIT:-1000}\" --format=json > error-reporting-export/error-sample.json\n", project, serviceName, project)
}

func (m *ErrorReportingMapper) provisionScript() string {
	return "#!/bin/sh\nset -eu\ntest -s config/sentry/sentry.conf.py\necho \"Sentry configuration rendered\"\n"
}

func (m *ErrorReportingMapper) migrateScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s error-reporting-export/error-sample.json\necho \"Error Reporting service %s mapped to Sentry ingest\"\n", serviceName)
}

func (m *ErrorReportingMapper) validateScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/error-reporting/app-change.env\ngrep -q %q config/error-reporting/app-change.env\ncurl -fsS \"${SENTRY_URL:-http://localhost:9000}/_health/\" >/dev/null\n", serviceName)
}

func (m *ErrorReportingMapper) backupScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/error-reporting-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/sentry config/error-reporting error-reporting-export 2>/dev/null || tar -czf \"$archive\" config/sentry config/error-reporting\necho \"$archive\"\n", sanitizeName(serviceName))
}

func (m *ErrorReportingMapper) cutoverScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/error-reporting/app-change.env\ntest \"$SOURCE_ERROR_REPORTING_SERVICE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\ntest -n \"$SENTRY_DSN\"\necho \"Apply $GENERATED_PATCH and route errors to Sentry\"\n", serviceName)
}

func errorReportingRunbook(serviceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "error-reporting", "source": "clouderrorreporting.googleapis.com", "service": serviceName, "target": "sentry"}
	return []domainrunbook.Step{
		errorReportingStep("export-error-reporting-config", "Export Error Reporting config", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_error_reporting_config.sh"}, "Error Reporting service config and samples are exported", metadata),
		errorReportingStep("provision-sentry", "Provision Sentry", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_sentry.sh"}, "Sentry config is rendered", metadata),
		errorReportingStep("migrate-error-reporting", "Migrate Error Reporting", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_error_reporting.sh"}, "error samples are represented for Sentry ingest", metadata),
		errorReportingStep("validate-sentry", "Validate Sentry", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_sentry.sh"}, "Sentry health and app-change config validate", metadata),
		errorReportingStep("backup-error-reporting-config", "Backup Error Reporting config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_error_reporting_config.sh"}, "error migration artifacts are archived", metadata),
		errorReportingStep("cutover-error-reporting-clients", "Cut over error clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_error_reporting_clients.sh"}, "clients use generated Sentry patch", metadata),
		errorReportingStep("rollback-error-reporting-source", "Keep Error Reporting source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Error Reporting remains authoritative until Sentry validation passes", metadata),
	}
}

func errorReportingStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
