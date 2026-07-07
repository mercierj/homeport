package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type ProfilerMapper struct {
	*mapper.BaseMapper
}

func NewProfilerMapper() *ProfilerMapper {
	return &ProfilerMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeProfilerService, nil)}
}

func (m *ProfilerMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	serviceName := firstNonEmpty(res.GetConfigString("service"), res.GetConfigString("name"), "cloudprofiler.googleapis.com")

	result := mapper.NewMappingResult("pyroscope")
	svc := result.DockerService
	svc.Image = "grafana/pyroscope:1.7.1"
	svc.Command = []string{"server"}
	svc.Ports = []string{"4040:4040"}
	svc.Volumes = []string{"./config/pyroscope:/etc/pyroscope", "./data/pyroscope:/var/lib/pyroscope"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "wget --spider -q http://localhost:4040/ready || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeProfilerService), "homeport.service": serviceName, "homeport.target": "pyroscope"}

	result.AddConfig("config/pyroscope/config.yml", []byte(m.pyroscopeConfig()))
	result.AddConfig("config/profiler/app-change.env", []byte(m.appChange(serviceName)))
	result.AddConfig("config/profiler/generated-pyroscope.patch", []byte(m.generatedPatch()))
	result.AddScript("export_profiler_config.sh", []byte(m.exportScript(serviceName)))
	result.AddScript("provision_pyroscope.sh", []byte(m.provisionScript()))
	result.AddScript("migrate_profiler_agents.sh", []byte(m.migrateScript(serviceName)))
	result.AddScript("validate_pyroscope.sh", []byte(m.validateScript(serviceName)))
	result.AddScript("backup_profiler_config.sh", []byte(m.backupScript(serviceName)))
	result.AddScript("cutover_profiler_clients.sh", []byte(m.cutoverScript(serviceName)))
	for _, step := range profilerRunbook(serviceName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *ProfilerMapper) pyroscopeConfig() string {
	return "server:\n  http_listen_port: 4040\nstorage:\n  backend: filesystem\n"
}

func (m *ProfilerMapper) appChange(serviceName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_PROFILER_SERVICE=%s\nTARGET_PROFILING=pyroscope\nPYROSCOPE_SERVER_ADDRESS=http://pyroscope:4040\nGENERATED_PATCH=config/profiler/generated-pyroscope.patch\n", serviceName)
}

func (m *ProfilerMapper) generatedPatch() string {
	return "--- a/app/profiling.env\n+++ b/app/profiling.env\n@@\n-GOOGLE_CLOUD_PROFILER=true\n+PROFILING_BACKEND=pyroscope\n+PYROSCOPE_SERVER_ADDRESS=http://pyroscope:4040\n"
}

func (m *ProfilerMapper) exportScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p profiler-export\ngcloud services list --enabled --filter='config.name=%s' --format=json > profiler-export/service.json\n", serviceName)
}

func (m *ProfilerMapper) provisionScript() string {
	return "#!/bin/sh\nset -eu\ntest -s config/pyroscope/config.yml\necho \"Pyroscope configuration rendered\"\n"
}

func (m *ProfilerMapper) migrateScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ngrep -q %q config/profiler/app-change.env\necho \"Cloud Profiler service %s mapped to Pyroscope agents\"\n", serviceName, serviceName)
}

func (m *ProfilerMapper) validateScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/profiler/app-change.env\ngrep -q %q config/profiler/app-change.env\nwget --spider -q \"${PYROSCOPE_URL:-http://localhost:4040}/ready\"\n", serviceName)
}

func (m *ProfilerMapper) backupScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/profiler-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/pyroscope config/profiler profiler-export 2>/dev/null || tar -czf \"$archive\" config/pyroscope config/profiler\necho \"$archive\"\n", sanitizeName(serviceName))
}

func (m *ProfilerMapper) cutoverScript(serviceName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/profiler/app-change.env\ntest \"$SOURCE_PROFILER_SERVICE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and send profiles to $PYROSCOPE_SERVER_ADDRESS\"\n", serviceName)
}

func profilerRunbook(serviceName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "profiling", "source": "cloudprofiler.googleapis.com", "service": serviceName, "target": "pyroscope"}
	return []domainrunbook.Step{
		profilerStep("export-profiler-config", "Export Profiler config", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_profiler_config.sh"}, "Profiler service config is exported", metadata),
		profilerStep("provision-pyroscope", "Provision Pyroscope", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_pyroscope.sh"}, "Pyroscope config is rendered", metadata),
		profilerStep("migrate-profiler-agents", "Migrate profiler agents", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_profiler_agents.sh"}, "profiling agents are represented as Pyroscope targets", metadata),
		profilerStep("validate-pyroscope", "Validate Pyroscope", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_pyroscope.sh"}, "Pyroscope readiness and app-change config validate", metadata),
		profilerStep("backup-profiler-config", "Backup Profiler config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_profiler_config.sh"}, "profiling migration artifacts are archived", metadata),
		profilerStep("cutover-profiler-clients", "Cut over profiler clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_profiler_clients.sh"}, "clients use generated Pyroscope patch", metadata),
		profilerStep("rollback-profiler-source", "Keep Profiler source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Cloud Profiler remains authoritative until Pyroscope validation passes", metadata),
	}
}

func profilerStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
