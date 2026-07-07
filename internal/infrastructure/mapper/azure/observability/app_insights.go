package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type AppInsightsMapper struct {
	*mapper.BaseMapper
}

func NewAppInsightsMapper() *AppInsightsMapper {
	return &AppInsightsMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAppInsights, nil)}
}

func (m *AppInsightsMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}
	location := res.GetConfigString("location")

	result := mapper.NewMappingResult("app-insights-otel")
	svc := result.DockerService
	svc.Image = "otel/opentelemetry-collector-contrib:0.103.0"
	svc.Command = []string{"--config=/etc/otelcol-contrib/config.yaml"}
	svc.Ports = []string{"4317:4317", "4318:4318", "8888:8888"}
	svc.Volumes = []string{"./config/app-insights/otel-collector.yaml:/etc/otelcol-contrib/config.yaml:ro"}
	svc.Environment = map[string]string{"SOURCE_APP_INSIGHTS": name, "SOURCE_LOCATION": location}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "wget -qO- http://localhost:8888/metrics >/dev/null"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeAppInsights), "homeport.app_insights": name, "homeport.target": "opentelemetry-grafana"}
	for _, service := range appInsightsSupportingServices() {
		result.AddService(service)
	}

	result.AddConfig("config/app-insights/otel-collector.yaml", []byte(appInsightsCollectorConfig(name)))
	result.AddConfig("config/app-insights/grafana-datasource.yaml", []byte(appInsightsGrafanaDatasource()))
	result.AddConfig("config/app-insights/app-change.env", []byte(appInsightsAppChange(name)))
	result.AddConfig("config/app-insights/generated-client.patch", []byte(appInsightsGeneratedPatch(name)))
	result.AddScript("export_app_insights.sh", []byte(appInsightsExportScript(name)))
	result.AddScript("provision_app_insights_observability.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/app-insights/otel-collector.yaml\necho \"OpenTelemetry collector ready for Azure App Insights %s\"\n", name)))
	result.AddScript("validate_app_insights_observability.sh", []byte("#!/bin/sh\nset -eu\ntest -s config/app-insights/app-change.env\ntest -s config/app-insights/grafana-datasource.yaml\ngrep -q OTLP_ENDPOINT config/app-insights/app-change.env\n"))
	result.AddScript("backup_app_insights_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/app-insights-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/app-insights app-insights-export 2>/dev/null || tar -czf \"$archive\" config/app-insights\necho \"$archive\"\n", name)))
	result.AddScript("cutover_app_insights_clients.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\n. config/app-insights/app-change.env\ntest \"$SOURCE_APP_INSIGHTS\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and send telemetry to $OTLP_ENDPOINT\"\n", name)))
	for _, step := range appInsightsRunbook(name) {
		result.AddRunbookStep(step)
	}
	result.AddWarning("Azure Application Insights query and sampling semantics are mapped to OpenTelemetry/Grafana artifacts; validate dashboard parity.")
	return result, nil
}

func appInsightsCollectorConfig(name string) string {
	return fmt.Sprintf("receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: 0.0.0.0:4317\n      http:\n        endpoint: 0.0.0.0:4318\nprocessors:\n  resource/appinsights:\n    attributes:\n      - key: source.app_insights\n        value: %s\n        action: upsert\n  batch: {}\nexporters:\n  prometheus:\n    endpoint: 0.0.0.0:8888\n  otlp/tempo:\n    endpoint: tempo:4317\n    tls:\n      insecure: true\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [resource/appinsights, batch]\n      exporters: [otlp/tempo]\n    metrics:\n      receivers: [otlp]\n      processors: [resource/appinsights, batch]\n      exporters: [prometheus]\n", name)
}

func appInsightsSupportingServices() []*mapper.DockerService {
	return []*mapper.DockerService{
		{
			Name:     "prometheus",
			Image:    "prom/prometheus:v2.53.0",
			Ports:    []string{"9090:9090"},
			Networks: []string{"homeport"},
			Restart:  "unless-stopped",
		},
		{
			Name:     "tempo",
			Image:    "grafana/tempo:2.5.0",
			Ports:    []string{"3200:3200"},
			Networks: []string{"homeport"},
			Restart:  "unless-stopped",
		},
		{
			Name:     "grafana",
			Image:    "grafana/grafana:11.1.0",
			Ports:    []string{"3000:3000"},
			Networks: []string{"homeport"},
			Restart:  "unless-stopped",
		},
	}
}

func appInsightsGrafanaDatasource() string {
	return "apiVersion: 1\ndatasources:\n  - name: Prometheus\n    type: prometheus\n    url: http://prometheus:9090\n  - name: Tempo\n    type: tempo\n    url: http://tempo:3200\n"
}

func appInsightsAppChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_APP_INSIGHTS=%s\nOTLP_ENDPOINT=http://app-insights-otel:4318\nOTLP_GRPC_ENDPOINT=http://app-insights-otel:4317\nGRAFANA_DASHBOARDS=config/app-insights/grafana-datasource.yaml\nGENERATED_PATCH=config/app-insights/generated-client.patch\n", name)
}

func appInsightsGeneratedPatch(name string) string {
	return fmt.Sprintf("--- a/app/telemetry.env\n+++ b/app/telemetry.env\n@@\n-APPLICATIONINSIGHTS_CONNECTION_STRING=%s\n+OTEL_EXPORTER_OTLP_ENDPOINT=http://app-insights-otel:4318\n+OTEL_SERVICE_NAME=${SERVICE_NAME:-homeport-app}\n", name)
}

func appInsightsExportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nCOMPONENT_NAME=%q\nOUTPUT_DIR=\"${OUTPUT_DIR:-./app-insights-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naz monitor app-insights component show --app \"$COMPONENT_NAME\" --resource-group \"${AZURE_RESOURCE_GROUP}\" > \"$OUTPUT_DIR/component.json\"\n", name)
}

func appInsightsRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "observability", "source": "azurerm_application_insights", "service": name, "target": "opentelemetry-grafana"}
	return []domainrunbook.Step{
		appInsightsStep("export-app-insights", "Export App Insights component", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_app_insights.sh"}, "App Insights component is exported", metadata),
		appInsightsStep("provision-app-insights", "Provision OpenTelemetry collector", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_app_insights_observability.sh"}, "collector config is rendered", metadata),
		appInsightsStep("validate-app-insights", "Validate observability handoff", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_app_insights_observability.sh"}, "OTLP handoff config validates", metadata),
		appInsightsStep("backup-app-insights", "Backup App Insights migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_app_insights_config.sh"}, "App Insights migration artifacts are archived", metadata),
		appInsightsStep("cutover-app-insights", "Cut over telemetry clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_app_insights_clients.sh"}, "clients use generated OTLP endpoint", metadata),
		appInsightsStep("rollback-app-insights", "Keep App Insights source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "App Insights remains authoritative until OTLP validation passes", metadata),
	}
}

func appInsightsStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
