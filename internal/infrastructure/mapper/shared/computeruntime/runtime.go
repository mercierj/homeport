package computeruntime

import (
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/mapper"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func FromDockerService(source string, svc *mapper.DockerService) mapper.AppUnit {
	unit := mapper.AppUnit{
		Name:        svc.Name,
		Source:      source,
		Image:       svc.Image,
		Command:     append([]string(nil), svc.Command...),
		Environment: clone(svc.Environment),
		Secrets:     map[string]string{},
		Ports:       append([]string(nil), svc.Ports...),
		Volumes:     append([]string(nil), svc.Volumes...),
		HealthCheck: svc.HealthCheck,
	}
	if svc.Build != nil {
		unit.SourcePath = svc.Build.Context
	}
	if svc.Deploy != nil {
		unit.Replicas = svc.Deploy.Replicas
	}
	if unit.Replicas == 0 {
		unit.Replicas = 1
	}
	if runtime := svc.Labels["homeport.runtime"]; runtime != "" {
		unit.Runtime = runtime
	}
	for key, value := range svc.Labels {
		if strings.HasPrefix(key, "traefik.http.routers.") && strings.HasSuffix(key, ".rule") {
			unit.Ingress = value
			break
		}
	}
	return unit
}

func ContainerApp(unit mapper.AppUnit, deployScript string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":        "compute-app",
		"app":         unit.Name,
		"source":      unit.Source,
		"image":       unit.Image,
		"source_path": unit.SourcePath,
	}
	resolveCommand := []string{"sh", "-c", fmt.Sprintf("docker image inspect %q >/dev/null 2>&1 || docker pull %q", unit.Image, unit.Image)}
	if unit.SourcePath != "" {
		resolveCommand = []string{"sh", "-c", fmt.Sprintf("docker build -t %q %q", unit.Image, unit.SourcePath)}
	}
	if unit.Image == "" {
		resolveCommand = []string{"sh", "-c", "echo provide app source or image before build"}
	}
	steps := []domainrunbook.Step{
		command("resolve-app-image", "Resolve app image or source", "Build", resolveCommand, "container image is available locally or source path is ready to build", metadata),
		command("deploy-compose-app", "Deploy Compose app", "Deploy", scriptCommand(deployScript), "Compose service is running", metadata),
		command("render-kubernetes-target", "Render Kubernetes target", "Deploy", []string{"sh", "-c", "echo render k3s/kubernetes manifests from app unit"}, "Kubernetes manifests include env, ports, volumes, healthchecks, and ingress", metadata),
		command("render-provider-targets", "Render provider targets", "Deploy", []string{"sh", "-c", "echo render hetzner scaleway ovh targets from app unit"}, "provider target generators receive normalized app unit", metadata),
		command("validate-app-health", "Validate app health", "Validate", []string{"sh", "-c", "echo validate container health and exposed ports"}, "container reaches healthy state and exposed ports respond", metadata),
		rollback("rollback-compute-source-authority", "Keep source compute runtime authoritative", metadata),
	}
	if unit.Image == "" || strings.Contains(unit.Image, "placeholder") {
		steps = append([]domainrunbook.Step{input("provide-app-source-or-image", "Provide app source or image", "Build", "operator supplied repository, source bundle, or replacement image and validation passed", metadata)}, steps...)
	}
	return steps
}

func ServerlessFunction(unit mapper.AppUnit, deployScript string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                    "serverless-function",
		"app":                     unit.Name,
		"source":                  unit.Source,
		"image":                   unit.Image,
		"source_path":             unit.SourcePath,
		"HOMEPORT_FUNCTION_URL":   "http://" + unit.Name + ":8080",
		"AWS_ENDPOINT_URL_LAMBDA": "http://homeport:8080/api/v1/compat/aws/lambda",
	}
	return []domainrunbook.Step{
		input("collect-function-event-samples", "Collect function event samples", "Build", "HTTP or event trigger samples are available for validation", metadata),
		command("build-function-image", "Build function image", "Build", scriptCommand(deployScript), "function container image builds and starts", metadata),
		command("validate-function-invoke", "Validate function invocation", "Validate", []string{"sh", "-c", "echo invoke function with original HTTP/event samples"}, "function returns expected response for sample events", metadata),
		rollback("rollback-function-source-authority", "Keep source function authoritative", metadata),
	}
}

func KubernetesCluster(clusterName, setupScript string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":    "kubernetes",
		"cluster": clusterName,
	}
	return []domainrunbook.Step{
		command("export-kubernetes-workloads", "Export Kubernetes workloads", "Discovery", []string{"sh", "-c", "echo export namespaces deployments services ingress configmaps secrets pvcs"}, "workloads exported with cloud-specific controllers flagged", metadata),
		command("provision-k3s-cluster", "Provision K3s cluster", "Deploy", scriptCommand(setupScript), "K3s API is reachable and kubeconfig is generated", metadata),
		command("apply-kubernetes-workloads", "Apply Kubernetes workloads", "Deploy", []string{"sh", "-c", "echo kubectl apply exported workloads"}, "workloads are applied to target cluster", metadata),
		command("validate-kubernetes-workloads", "Validate Kubernetes workloads", "Validate", []string{"sh", "-c", "kubectl get deploy && kubectl get svc && kubectl get ingress"}, "deployments, services, and ingress are reachable", metadata),
		rollback("rollback-kubernetes-source-authority", "Keep source Kubernetes cluster authoritative", metadata),
	}
}

func scriptCommand(script string) []string {
	if script == "" {
		return []string{"sh", "-c", "echo no deployment script generated"}
	}
	return []string{"sh", script}
}

func command(id, name, group string, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeCommand,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		SuccessCondition: success,
		Command:          command,
		Metadata:         clone(metadata),
	}
}

func input(id, name, group, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeInput,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "user",
		SuccessCondition: success,
		Metadata:         clone(metadata),
	}
}

func rollback(id, name string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            "Rollback",
		Type:             domainrunbook.StepTypeRollback,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "noop",
		SuccessCondition: "source remains authoritative until cutover passes",
		Metadata:         clone(metadata),
	}
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
