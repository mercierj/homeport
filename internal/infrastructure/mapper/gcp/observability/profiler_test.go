package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestProfilerConformanceManagedAToZ(t *testing.T) {
	result, err := NewProfilerMapper().Map(context.Background(), managedProfilerFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Profiler migration", result.ManualSteps)
	}
	if result.DockerService.Image != "grafana/pyroscope:1.7.1" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Pyroscope target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/pyroscope/config.yml", "config/profiler/app-change.env", "config/profiler/generated-pyroscope.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/profiler/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_PROFILER_SERVICE=cloudprofiler.googleapis.com", "TARGET_PROFILING=pyroscope", "PYROSCOPE_SERVER_ADDRESS=http://pyroscope:4040"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_profiler_config.sh", "provision_pyroscope.sh", "migrate_profiler_agents.sh", "validate_pyroscope.sh", "backup_profiler_config.sh", "cutover_profiler_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-profiler-config":   domainrunbook.StepTypeCommand,
		"provision-pyroscope":      domainrunbook.StepTypeCommand,
		"migrate-profiler-agents":  domainrunbook.StepTypeCommand,
		"validate-pyroscope":       domainrunbook.StepTypeCommand,
		"backup-profiler-config":   domainrunbook.StepTypeCommand,
		"cutover-profiler-clients": domainrunbook.StepTypeAPICall,
		"rollback-profiler-source": domainrunbook.StepTypeRollback,
	} {
		if !hasProfilerRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedProfilerFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/services/cloudprofiler.googleapis.com",
		Type: resource.TypeProfilerService,
		Name: "cloudprofiler.googleapis.com",
		Config: map[string]interface{}{
			"service": "cloudprofiler.googleapis.com",
		},
	}
}

func hasProfilerRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
