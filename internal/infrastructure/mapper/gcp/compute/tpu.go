package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type TPUMapper struct {
	*mapper.BaseMapper
}

func NewTPUNodeMapper() *TPUMapper {
	return &TPUMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeTPUNode, nil)}
}

func NewTPUV2VMMapper() *TPUMapper {
	return &TPUMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeTPUV2VM, nil)}
}

func (m *TPUMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmptyTPU(res.GetConfigString("name"), res.Name, "tpu-workload")
	accelerator := firstNonEmptyTPU(res.GetConfigString("accelerator_type"), res.GetConfigString("accelerator_config.type"), "portable-accelerator")

	result := mapper.NewMappingResult("k3s-server")
	svc := result.DockerService
	svc.Image = "rancher/k3s:v1.29.0-k3s1"
	svc.Command = []string{"server", "--disable=traefik", "--tls-san=localhost", "--tls-san=k3s-server"}
	svc.Environment = map[string]string{"K3S_TOKEN": "homeport-tpu-token", "K3S_KUBECONFIG_OUTPUT": "/output/kubeconfig.yaml", "K3S_KUBECONFIG_MODE": "666"}
	svc.Ports = []string{"6443:6443"}
	svc.Volumes = []string{"k3s-tpu:/var/lib/rancher/k3s", "./kubeconfig:/output"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "kubectl get nodes >/dev/null 2>&1 || exit 1"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(res.Type), "homeport.tpu": name, "homeport.target": "kubernetes"}

	result.AddVolume(mapper.Volume{Name: "k3s-tpu", Driver: "local"})
	result.AddConfig("config/tpu/accelerator-job.yaml", []byte(tpuJob(name, accelerator)))
	result.AddConfig("config/tpu/app-change.env", []byte(tpuAppChange(name)))
	result.AddConfig("config/tpu/generated-accelerator.patch", []byte(tpuPatch(name)))
	result.AddScript("export_tpu_config.sh", []byte(tpuExportScript(name, res.GetConfigString("zone"))))
	result.AddScript("provision_accelerator_cluster.sh", []byte("#!/bin/sh\nset -eu\ntest -s config/tpu/accelerator-job.yaml\necho \"Kubernetes accelerator cluster config rendered\"\n"))
	result.AddScript("migrate_tpu_workload.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ngrep -q %q config/tpu/accelerator-job.yaml\necho \"TPU workload mapped to portable accelerator job\"\n", name)))
	result.AddScript("validate_accelerator_job.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/tpu/app-change.env\ngrep -q %q config/tpu/app-change.env\n", name)))
	result.AddScript("backup_tpu_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/tpu-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/tpu tpu-export 2>/dev/null || tar -czf \"$archive\" config/tpu\necho \"$archive\"\n", sanitizeComputeName(name))))
	result.AddScript("cutover_tpu_workloads.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\n. config/tpu/app-change.env\ntest \"$SOURCE_TPU_RESOURCE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and run workload via $TARGET_ACCELERATOR\"\n", name)))
	for _, step := range tpuRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func tpuJob(name, accelerator string) string {
	return fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: %s
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: workload
          image: ${ACCELERATOR_WORKLOAD_IMAGE}
          resources:
            limits:
              homeport.io/accelerator: %q
`, sanitizeComputeName(name), accelerator)
}

func tpuAppChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_TPU_RESOURCE=%s\nTARGET_ACCELERATOR=kubernetes\nGENERATED_PATCH=config/tpu/generated-accelerator.patch\n", name)
}

func tpuPatch(name string) string {
	return fmt.Sprintf("--- a/workload.env\n+++ b/workload.env\n@@\n-GOOGLE_TPU_NAME=%s\n+ACCELERATOR_BACKEND=kubernetes\n+KUBERNETES_JOB=config/tpu/accelerator-job.yaml\n", name)
}

func tpuExportScript(name, zone string) string {
	if zone == "" {
		zone = "${GCP_ZONE}"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p tpu-export\ngcloud compute tpus tpu-vm describe %q --zone %q --format=json > tpu-export/tpu.json 2>/dev/null || gcloud compute tpus describe %q --zone %q --format=json > tpu-export/tpu.json\n", name, zone, name, zone)
}

func tpuRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "accelerator", "source": "google_tpu", "name": name, "target": "kubernetes"}
	return []domainrunbook.Step{
		tpuStep("export-tpu-config", "Export TPU config", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_tpu_config.sh"}, "TPU config is exported", metadata),
		tpuStep("provision-accelerator-cluster", "Provision accelerator cluster", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_accelerator_cluster.sh"}, "accelerator job manifest is rendered", metadata),
		tpuStep("migrate-tpu-workload", "Migrate TPU workload", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_tpu_workload.sh"}, "TPU workload is mapped to Kubernetes job", metadata),
		tpuStep("validate-accelerator-job", "Validate accelerator job", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_accelerator_job.sh"}, "accelerator handoff config validates", metadata),
		tpuStep("backup-tpu-config", "Backup TPU config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_tpu_config.sh"}, "TPU migration artifacts are archived", metadata),
		tpuStep("cutover-tpu-workloads", "Cut over TPU workloads", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_tpu_workloads.sh"}, "workloads use generated Kubernetes accelerator patch", metadata),
		tpuStep("rollback-tpu-source", "Keep TPU source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "TPU remains authoritative until accelerator job validation passes", metadata),
	}
}

func tpuStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmptyTPU(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sanitizeComputeName(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "workload"
	}
	return value
}
