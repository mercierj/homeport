// Package k8s generates Kubernetes manifests from mapping results.
package k8s

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/domain/target"
)

// Generator generates Kubernetes manifests.
// It implements both generator.TargetGenerator and generator.StackGenerator interfaces.
type Generator struct {
	platform target.Platform
}

// New creates a Kubernetes generator.
func New() *Generator {
	return &Generator{platform: target.PlatformKubernetes}
}

// NewK3s creates a K3s generator.
func NewK3s() *Generator {
	return &Generator{platform: target.PlatformK3s}
}

// Platform returns the target platform.
func (g *Generator) Platform() target.Platform {
	return g.platform
}

// Name returns the generator name.
func (g *Generator) Name() string {
	return string(g.platform) + "-generator"
}

// Description returns description.
func (g *Generator) Description() string {
	return "Generates Kubernetes manifests (Deployments, Services, ConfigMaps, Secrets, PVCs, Ingress) and Helm charts"
}

// SupportedHALevels returns supported HA levels.
func (g *Generator) SupportedHALevels() []target.HALevel {
	return []target.HALevel{
		target.HALevelNone,
		target.HALevelBasic,
		target.HALevelMultiServer,
		target.HALevelCluster,
		target.HALevelGeo,
	}
}

// RequiresCredentials returns false.
func (g *Generator) RequiresCredentials() bool {
	return false
}

// RequiredCredentials returns empty.
func (g *Generator) RequiredCredentials() []string {
	return nil
}

// Validate validates inputs.
func (g *Generator) Validate(results []*mapper.MappingResult, config *generator.TargetConfig) error {
	if len(results) == 0 {
		return fmt.Errorf("no mapping results")
	}
	return nil
}

// Generate produces Kubernetes manifests.
func (g *Generator) Generate(ctx context.Context, results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.TargetOutput, error) {
	if err := g.Validate(results, config); err != nil {
		return nil, err
	}

	output := generator.NewTargetOutput(g.platform)
	namespace := sanitizeName(config.ProjectName)
	replicas := g.getReplicaCount(config.HALevel)

	var allManifests bytes.Buffer

	// Namespace
	ns := g.generateNamespace(namespace)
	allManifests.WriteString(ns)
	output.AddK8sManifest("00-namespace.yaml", []byte(ns))

	// Collect services
	var services []*mapper.DockerService
	for _, r := range results {
		if r.DockerService != nil {
			services = append(services, r.DockerService)
		}
		services = append(services, r.AdditionalServices...)
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	for i, svc := range services {
		prefix := fmt.Sprintf("%02d-%s", i+1, sanitizeName(svc.Name))

		// ConfigMap & Secret
		if len(svc.Environment) > 0 {
			configVars, secretVars := splitEnvVars(svc.Environment)
			if len(configVars) > 0 {
				cm := g.generateConfigMap(svc.Name, namespace, configVars)
				allManifests.WriteString("\n---\n" + cm)
				output.AddK8sManifest(prefix+"-configmap.yaml", []byte(cm))
			}
			if len(secretVars) > 0 {
				secret := g.generateSecret(svc.Name, namespace, secretVars)
				allManifests.WriteString("\n---\n" + secret)
				output.AddK8sManifest(prefix+"-secret.yaml", []byte(secret))
			}
		}

		// PVCs
		for _, vol := range svc.Volumes {
			volName, _, isNamed := parseVolume(vol)
			if isNamed {
				pvc := g.generatePVC(volName, namespace)
				allManifests.WriteString("\n---\n" + pvc)
				output.AddK8sManifest(prefix+"-pvc-"+sanitizeName(volName)+".yaml", []byte(pvc))
			}
		}

		// Deployment
		deploy := g.generateDeployment(svc, namespace, replicas)
		allManifests.WriteString("\n---\n" + deploy)
		output.AddK8sManifest(prefix+"-deployment.yaml", []byte(deploy))

		// Service
		if len(svc.Ports) > 0 {
			k8sSvc := g.generateService(svc, namespace)
			allManifests.WriteString("\n---\n" + k8sSvc)
			output.AddK8sManifest(prefix+"-service.yaml", []byte(k8sSvc))
		}

		// Ingress
		if config.BaseURL != "" && isExposed(svc) {
			ingress := g.generateIngress(svc, namespace, config)
			allManifests.WriteString("\n---\n" + ingress)
			output.AddK8sManifest(prefix+"-ingress.yaml", []byte(ingress))
		}
	}

	output.AddK8sManifest("manifests.yaml", allManifests.Bytes())
	output.MainFile = "manifests.yaml"
	output.Summary = fmt.Sprintf("Generated %d K8s manifests for %d services", len(output.K8sManifests), len(services))
	output.AddManualStep("Apply: kubectl apply -f manifests.yaml")

	return output, nil
}

// EstimateCost returns estimate.
func (g *Generator) EstimateCost(results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.CostEstimate, error) {
	estimate := generator.NewCostEstimate("EUR")
	estimate.AddNote("Self-hosted K8s - costs depend on infrastructure")
	return estimate, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// StackGenerator Interface Implementation
// ─────────────────────────────────────────────────────────────────────────────

// ValidateStacks checks if the consolidated stacks can be processed by this generator.
func (g *Generator) ValidateStacks(stacks *stack.ConsolidatedResult, config *generator.TargetConfig) error {
	if stacks == nil {
		return fmt.Errorf("consolidated stacks is nil")
	}

	if len(stacks.Stacks) == 0 && len(stacks.Passthrough) == 0 {
		return fmt.Errorf("no stacks or passthrough resources to generate")
	}

	if config == nil {
		return fmt.Errorf("target configuration is required")
	}

	return nil
}

// GenerateFromStacks produces output artifacts from consolidated stacks.
// This method generates Helm charts organized by stack type, plus a unified
// manifests.yaml for direct kubectl apply.
func (g *Generator) GenerateFromStacks(ctx context.Context, stacks *stack.ConsolidatedResult, config *generator.TargetConfig) (*generator.TargetOutput, error) {
	if err := g.ValidateStacks(stacks, config); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	output := generator.NewTargetOutput(g.platform)
	namespace := sanitizeName(config.ProjectName)
	replicas := g.getReplicaCount(config.HALevel)

	var allManifests bytes.Buffer

	// Namespace
	ns := g.generateNamespace(namespace)
	allManifests.WriteString(ns)
	output.AddK8sManifest("00-namespace.yaml", []byte(ns))

	// Order stacks by dependencies
	orderedStacks := g.orderStacksByDependency(stacks.Stacks)

	// Generate per-stack Helm charts and manifests
	for stackIdx, stk := range orderedStacks {
		stackPrefix := fmt.Sprintf("%02d-%s", stackIdx+1, stk.Type.String())

		// Generate Helm chart for this stack
		helmChart := g.generateHelmChart(stk, namespace, config)
		for filename, content := range helmChart {
			chartPath := fmt.Sprintf("charts/%s/%s", stk.Type.String(), filename)
			output.AddFile(chartPath, content)
			output.HelmCharts[chartPath] = content
		}

		// Generate raw K8s manifests for each service in the stack
		for svcIdx, svc := range stk.Services {
			svcPrefix := fmt.Sprintf("%s-%02d-%s", stackPrefix, svcIdx+1, sanitizeName(svc.Name))

			// ConfigMap & Secret
			if len(svc.Environment) > 0 {
				configVars, secretVars := splitStackEnvVars(svc.Environment)
				if len(configVars) > 0 {
					cm := g.generateConfigMap(svc.Name, namespace, configVars)
					allManifests.WriteString("\n---\n" + cm)
					output.AddK8sManifest(svcPrefix+"-configmap.yaml", []byte(cm))
				}
				if len(secretVars) > 0 {
					secret := g.generateSecret(svc.Name, namespace, secretVars)
					allManifests.WriteString("\n---\n" + secret)
					output.AddK8sManifest(svcPrefix+"-secret.yaml", []byte(secret))
				}
			}

			// PVCs
			for _, vol := range svc.Volumes {
				volName, _, isNamed := parseVolume(vol)
				if isNamed {
					pvc := g.generatePVC(volName, namespace)
					allManifests.WriteString("\n---\n" + pvc)
					output.AddK8sManifest(svcPrefix+"-pvc-"+sanitizeName(volName)+".yaml", []byte(pvc))
				}
			}

			// Deployment
			deploy := g.generateStackDeployment(svc, namespace, replicas, stk.Type)
			allManifests.WriteString("\n---\n" + deploy)
			output.AddK8sManifest(svcPrefix+"-deployment.yaml", []byte(deploy))

			// Service
			if len(svc.Ports) > 0 {
				k8sSvc := g.generateStackService(svc, namespace, stk.Type)
				allManifests.WriteString("\n---\n" + k8sSvc)
				output.AddK8sManifest(svcPrefix+"-service.yaml", []byte(k8sSvc))
			}

			// Ingress
			if config.BaseURL != "" && isStackServiceExposed(svc) {
				ingress := g.generateStackIngress(svc, namespace, config, stk.Type)
				allManifests.WriteString("\n---\n" + ingress)
				output.AddK8sManifest(svcPrefix+"-ingress.yaml", []byte(ingress))
			}
		}

		// Add stack configs and scripts
		for name, content := range stk.Configs {
			output.AddConfig(fmt.Sprintf("%s/%s", stk.Type.String(), name), content)
		}
		for name, content := range stk.Scripts {
			output.AddScript(fmt.Sprintf("%s/%s", stk.Type.String(), name), content)
		}
	}

	// Handle passthrough resources
	for _, res := range stacks.Passthrough {
		output.AddWarning(fmt.Sprintf("Passthrough resource '%s' (%s) requires manual handling", res.Name, res.Type))
		output.AddManualStep(fmt.Sprintf("Configure passthrough resource: %s (%s)", res.Name, res.Type))
	}

	// Copy warnings and manual steps from consolidation
	for _, warning := range stacks.Warnings {
		output.AddWarning(warning)
	}
	for _, step := range stacks.ManualSteps {
		output.AddManualStep(step)
	}

	// Add unified manifests file
	output.AddK8sManifest("manifests.yaml", allManifests.Bytes())
	output.MainFile = "manifests.yaml"

	// Generate umbrella Helm chart (Chart.yaml + values.yaml for all stacks)
	umbrellaChart := g.generateUmbrellaChart(stacks, namespace, config)
	for filename, content := range umbrellaChart {
		output.AddFile(filename, content)
		output.HelmCharts[filename] = content
	}

	// Generate installation instructions
	output.AddManualStep("Option 1 - Direct apply: kubectl apply -f manifests.yaml")
	output.AddManualStep("Option 2 - Helm install: helm install " + namespace + " ./charts/" + namespace)

	output.Summary = fmt.Sprintf("Generated K8s manifests and Helm charts from %d stacks with %d services",
		len(stacks.Stacks), stacks.Metadata.TotalServices)

	return output, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helm Chart Generation
// ─────────────────────────────────────────────────────────────────────────────

// generateHelmChart creates a Helm chart for a single stack.
func (g *Generator) generateHelmChart(stk *stack.Stack, namespace string, config *generator.TargetConfig) map[string][]byte {
	files := make(map[string][]byte)
	replicas := g.getReplicaCount(config.HALevel)

	// Chart.yaml
	chartYaml := fmt.Sprintf(`apiVersion: v2
name: %s
description: %s - Generated by Homeport
type: application
version: 0.1.0
appVersion: "1.0.0"
keywords:
  - homeport
  - %s
  - self-hosted
home: https://github.com/homeport/homeport
maintainers:
  - name: Homeport
`, stk.Type.String(), stk.Type.DisplayName(), stk.Type.String())
	files["Chart.yaml"] = []byte(chartYaml)

	// values.yaml
	var valuesYaml bytes.Buffer
	valuesYaml.WriteString(fmt.Sprintf(`# Default values for %s stack
# Generated by Homeport - %s

global:
  namespace: %s
  replicas: %d

`, stk.Type.String(), time.Now().Format(time.RFC3339), namespace, replicas))

	for _, svc := range stk.Services {
		svcName := sanitizeName(svc.Name)
		valuesYaml.WriteString(fmt.Sprintf(`%s:
  enabled: true
  image: %s
  replicas: %d
`, svcName, svc.Image, replicas))

		// Add ports if any
		if len(svc.Ports) > 0 {
			valuesYaml.WriteString("  ports:\n")
			for _, p := range svc.Ports {
				port := extractContainerPort(p)
				valuesYaml.WriteString(fmt.Sprintf("    - %s\n", port))
			}
		}

		// Add environment variables
		if len(svc.Environment) > 0 {
			valuesYaml.WriteString("  env:\n")
			for k, v := range svc.Environment {
				if isSensitiveEnvVar(k) {
					valuesYaml.WriteString(fmt.Sprintf("    %s: \"\"  # SENSITIVE - configure in secrets\n", k))
				} else {
					valuesYaml.WriteString(fmt.Sprintf("    %s: \"%s\"\n", k, escapeYAML(v)))
				}
			}
		}

		valuesYaml.WriteString("\n")
	}
	files["values.yaml"] = valuesYaml.Bytes()

	// templates/_helpers.tpl
	helpersTpl := fmt.Sprintf(`{{/*
Expand the name of the chart.
*/}}
{{- define "%s.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "%s.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%%s-%%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "%s.labels" -}}
helm.sh/chart: {{ include "%s.name" . }}
{{ include "%s.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/stack: %s
{{- end }}

{{/*
Selector labels
*/}}
{{- define "%s.selectorLabels" -}}
app.kubernetes.io/name: {{ include "%s.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
`, stk.Type.String(), stk.Type.String(), stk.Type.String(), stk.Type.String(), stk.Type.String(), stk.Type.String(), stk.Type.String(), stk.Type.String())
	files["templates/_helpers.tpl"] = []byte(helpersTpl)

	// Generate templates for each service
	for _, svc := range stk.Services {
		svcName := sanitizeName(svc.Name)

		// Deployment template
		deployTpl := g.generateHelmDeploymentTemplate(svc, stk.Type)
		files[fmt.Sprintf("templates/%s-deployment.yaml", svcName)] = []byte(deployTpl)

		// Service template
		if len(svc.Ports) > 0 {
			serviceTpl := g.generateHelmServiceTemplate(svc, stk.Type)
			files[fmt.Sprintf("templates/%s-service.yaml", svcName)] = []byte(serviceTpl)
		}

		// ConfigMap template
		configVars, secretVars := splitStackEnvVars(svc.Environment)
		if len(configVars) > 0 {
			configMapTpl := g.generateHelmConfigMapTemplate(svc, stk.Type)
			files[fmt.Sprintf("templates/%s-configmap.yaml", svcName)] = []byte(configMapTpl)
		}

		// Secret template
		if len(secretVars) > 0 {
			secretTpl := g.generateHelmSecretTemplate(svc, stk.Type)
			files[fmt.Sprintf("templates/%s-secret.yaml", svcName)] = []byte(secretTpl)
		}
	}

	return files
}

// generateUmbrellaChart creates an umbrella Helm chart that includes all stack charts.
func (g *Generator) generateUmbrellaChart(stacks *stack.ConsolidatedResult, namespace string, config *generator.TargetConfig) map[string][]byte {
	files := make(map[string][]byte)

	// Chart.yaml with dependencies
	var chartDeps bytes.Buffer
	chartDeps.WriteString(`apiVersion: v2
name: ` + namespace + `
description: Umbrella chart for all stacks - Generated by Homeport
type: application
version: 0.1.0
appVersion: "1.0.0"
dependencies:
`)
	for _, stk := range stacks.Stacks {
		chartDeps.WriteString(fmt.Sprintf(`  - name: %s
    version: "0.1.0"
    repository: "file://charts/%s"
    condition: %s.enabled
`, stk.Type.String(), stk.Type.String(), stk.Type.String()))
	}
	files["charts/"+namespace+"/Chart.yaml"] = chartDeps.Bytes()

	// values.yaml
	var valuesYaml bytes.Buffer
	valuesYaml.WriteString(`# Umbrella chart values
# Generated by Homeport - ` + time.Now().Format(time.RFC3339) + `

global:
  namespace: ` + namespace + `

`)
	for _, stk := range stacks.Stacks {
		valuesYaml.WriteString(fmt.Sprintf(`%s:
  enabled: true

`, stk.Type.String()))
	}
	files["charts/"+namespace+"/values.yaml"] = valuesYaml.Bytes()

	return files
}

// generateHelmDeploymentTemplate generates a Helm template for a Deployment.
func (g *Generator) generateHelmDeploymentTemplate(svc *stack.Service, stackType stack.StackType) string {
	svcName := sanitizeName(svc.Name)
	chartName := stackType.String()

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(`{{- if .Values.%s.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "%s.fullname" . }}-%s
  labels:
    {{- include "%s.labels" . | nindent 4 }}
    app.kubernetes.io/component: %s
spec:
  replicas: {{ .Values.%s.replicas | default .Values.global.replicas }}
  selector:
    matchLabels:
      {{- include "%s.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: %s
  template:
    metadata:
      labels:
        {{- include "%s.selectorLabels" . | nindent 8 }}
        app.kubernetes.io/component: %s
    spec:
      containers:
        - name: %s
          image: {{ .Values.%s.image }}
          imagePullPolicy: IfNotPresent
`, svcName, chartName, svcName, chartName, svcName, svcName, chartName, svcName, chartName, svcName, svcName, svcName))

	// Ports
	if len(svc.Ports) > 0 {
		buf.WriteString("          ports:\n")
		buf.WriteString(fmt.Sprintf(`          {{- range .Values.%s.ports }}
            - containerPort: {{ . }}
          {{- end }}
`, svcName))
	}

	// Environment from ConfigMap/Secret
	if len(svc.Environment) > 0 {
		buf.WriteString("          envFrom:\n")
		configVars, secretVars := splitStackEnvVars(svc.Environment)
		if len(configVars) > 0 {
			buf.WriteString(fmt.Sprintf("            - configMapRef:\n                name: {{ include \"%s.fullname\" . }}-%s-config\n", chartName, svcName))
		}
		if len(secretVars) > 0 {
			buf.WriteString(fmt.Sprintf("            - secretRef:\n                name: {{ include \"%s.fullname\" . }}-%s-secret\n", chartName, svcName))
		}
	}

	// Resources
	buf.WriteString(`          resources:
            requests:
              memory: "128Mi"
              cpu: "100m"
            limits:
              memory: "512Mi"
              cpu: "500m"
{{- end }}
`)

	return buf.String()
}

// generateHelmServiceTemplate generates a Helm template for a Service.
func (g *Generator) generateHelmServiceTemplate(svc *stack.Service, stackType stack.StackType) string {
	svcName := sanitizeName(svc.Name)
	chartName := stackType.String()

	return fmt.Sprintf(`{{- if .Values.%s.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "%s.fullname" . }}-%s
  labels:
    {{- include "%s.labels" . | nindent 4 }}
spec:
  selector:
    {{- include "%s.selectorLabels" . | nindent 4 }}
    app.kubernetes.io/component: %s
  ports:
    {{- range .Values.%s.ports }}
    - port: {{ . }}
      targetPort: {{ . }}
    {{- end }}
{{- end }}
`, svcName, chartName, svcName, chartName, chartName, svcName, svcName)
}

// generateHelmConfigMapTemplate generates a Helm template for a ConfigMap.
func (g *Generator) generateHelmConfigMapTemplate(svc *stack.Service, stackType stack.StackType) string {
	svcName := sanitizeName(svc.Name)
	chartName := stackType.String()

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(`{{- if .Values.%s.enabled }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "%s.fullname" . }}-%s-config
  labels:
    {{- include "%s.labels" . | nindent 4 }}
data:
`, svcName, chartName, svcName, chartName))

	configVars, _ := splitStackEnvVars(svc.Environment)
	for k := range configVars {
		buf.WriteString(fmt.Sprintf(`  %s: {{ .Values.%s.env.%s | quote }}
`, k, svcName, k))
	}

	buf.WriteString("{{- end }}\n")
	return buf.String()
}

// generateHelmSecretTemplate generates a Helm template for a Secret.
func (g *Generator) generateHelmSecretTemplate(svc *stack.Service, stackType stack.StackType) string {
	svcName := sanitizeName(svc.Name)
	chartName := stackType.String()

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(`{{- if .Values.%s.enabled }}
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "%s.fullname" . }}-%s-secret
  labels:
    {{- include "%s.labels" . | nindent 4 }}
type: Opaque
data:
`, svcName, chartName, svcName, chartName))

	_, secretVars := splitStackEnvVars(svc.Environment)
	for k := range secretVars {
		buf.WriteString(fmt.Sprintf(`  %s: {{ .Values.%s.env.%s | b64enc | quote }}
`, k, svcName, k))
	}

	buf.WriteString("{{- end }}\n")
	return buf.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Stack-aware Manifest Generation
// ─────────────────────────────────────────────────────────────────────────────

// generateStackDeployment generates a Deployment from a stack service.
func (g *Generator) generateStackDeployment(svc *stack.Service, namespace string, replicas int, stackType stack.StackType) string {
	name := sanitizeName(svc.Name)
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
    app.kubernetes.io/stack: %s
    app.kubernetes.io/managed-by: homeport
spec:
  replicas: %d
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
        app.kubernetes.io/stack: %s
    spec:
      containers:
        - name: %s
          image: %s
          imagePullPolicy: IfNotPresent
`, name, namespace, name, stackType.String(), replicas, name, name, stackType.String(), name, svc.Image))

	// Ports
	if len(svc.Ports) > 0 {
		buf.WriteString("          ports:\n")
		for _, p := range svc.Ports {
			port := extractContainerPort(p)
			buf.WriteString(fmt.Sprintf("            - containerPort: %s\n", port))
		}
	}

	// Env from ConfigMap/Secret
	if len(svc.Environment) > 0 {
		buf.WriteString("          envFrom:\n")
		configVars, secretVars := splitStackEnvVars(svc.Environment)
		if len(configVars) > 0 {
			buf.WriteString(fmt.Sprintf("            - configMapRef:\n                name: %s-config\n", name))
		}
		if len(secretVars) > 0 {
			buf.WriteString(fmt.Sprintf("            - secretRef:\n                name: %s-secret\n", name))
		}
	}

	// Volume mounts
	if len(svc.Volumes) > 0 {
		buf.WriteString("          volumeMounts:\n")
		for _, vol := range svc.Volumes {
			volName, mountPath, isNamed := parseVolume(vol)
			if isNamed && mountPath != "" {
				buf.WriteString(fmt.Sprintf("            - name: %s\n              mountPath: %s\n", sanitizeName(volName), mountPath))
			}
		}
		buf.WriteString("      volumes:\n")
		for _, vol := range svc.Volumes {
			volName, _, isNamed := parseVolume(vol)
			if isNamed {
				buf.WriteString(fmt.Sprintf("        - name: %s\n          persistentVolumeClaim:\n            claimName: %s\n", sanitizeName(volName), sanitizeName(volName)))
			}
		}
	}

	// Health check -> probes
	if svc.HealthCheck != nil && len(svc.HealthCheck.Test) > 0 {
		buf.WriteString("          livenessProbe:\n")
		buf.WriteString("            exec:\n")
		buf.WriteString(fmt.Sprintf("              command: %v\n", svc.HealthCheck.Test))
	}

	// Resources
	buf.WriteString("          resources:\n")
	buf.WriteString("            requests:\n")
	buf.WriteString("              memory: \"128Mi\"\n")
	buf.WriteString("              cpu: \"100m\"\n")
	buf.WriteString("            limits:\n")
	buf.WriteString("              memory: \"512Mi\"\n")
	buf.WriteString("              cpu: \"500m\"\n")

	return buf.String()
}

// generateStackService generates a K8s Service from a stack service.
func (g *Generator) generateStackService(svc *stack.Service, namespace string, stackType stack.StackType) string {
	name := sanitizeName(svc.Name)
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/stack: %s
spec:
  selector:
    app: %s
  ports:
`, name, namespace, stackType.String(), name))

	for _, p := range svc.Ports {
		port := extractContainerPort(p)
		buf.WriteString(fmt.Sprintf("    - port: %s\n      targetPort: %s\n", port, port))
	}

	return buf.String()
}

// generateStackIngress generates an Ingress from a stack service.
func (g *Generator) generateStackIngress(svc *stack.Service, namespace string, config *generator.TargetConfig, stackType stack.StackType) string {
	name := sanitizeName(svc.Name)
	host := config.BaseURL
	if strings.HasPrefix(host, "http://") {
		host = strings.TrimPrefix(host, "http://")
	}
	if strings.HasPrefix(host, "https://") {
		host = strings.TrimPrefix(host, "https://")
	}

	port := "80"
	if len(svc.Ports) > 0 {
		port = extractContainerPort(svc.Ports[0])
	}

	return fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/stack: %s
  annotations:
    kubernetes.io/ingress.class: traefik
spec:
  rules:
    - host: %s.%s
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: %s
                port:
                  number: %s
`, name, namespace, stackType.String(), name, host, name, port)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper Functions
// ─────────────────────────────────────────────────────────────────────────────

// orderStacksByDependency orders stacks so dependencies come first.
func (g *Generator) orderStacksByDependency(stacks []*stack.Stack) []*stack.Stack {
	stackMap := make(map[stack.StackType]*stack.Stack)
	for _, stk := range stacks {
		stackMap[stk.Type] = stk
	}

	graph := make(map[stack.StackType][]stack.StackType)
	inDegree := make(map[stack.StackType]int)

	for _, stk := range stacks {
		graph[stk.Type] = make([]stack.StackType, 0)
		inDegree[stk.Type] = 0
	}

	for _, stk := range stacks {
		for _, dep := range stk.DependsOn {
			if _, exists := stackMap[dep]; exists {
				graph[dep] = append(graph[dep], stk.Type)
				inDegree[stk.Type]++
			}
		}
	}

	var queue []stack.StackType
	for stackType, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, stackType)
		}
	}

	var ordered []*stack.Stack
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if stk, exists := stackMap[current]; exists {
			ordered = append(ordered, stk)
		}

		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(ordered) != len(stacks) {
		return stacks
	}

	return ordered
}

func (g *Generator) getReplicaCount(level target.HALevel) int {
	switch level {
	case target.HALevelGeo, target.HALevelCluster:
		return 3
	case target.HALevelMultiServer:
		return 2
	default:
		return 1
	}
}

func (g *Generator) generateNamespace(name string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    app.kubernetes.io/managed-by: homeport
`, name)
}

func (g *Generator) generateConfigMap(name, namespace string, data map[string]string) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s-config
  namespace: %s
data:
`, sanitizeName(name), namespace))
	for k, v := range data {
		buf.WriteString(fmt.Sprintf("  %s: \"%s\"\n", k, escapeYAML(v)))
	}
	return buf.String()
}

func (g *Generator) generateSecret(name, namespace string, data map[string]string) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s-secret
  namespace: %s
type: Opaque
data:
`, sanitizeName(name), namespace))
	for k, v := range data {
		buf.WriteString(fmt.Sprintf("  %s: %s\n", k, base64.StdEncoding.EncodeToString([]byte(v))))
	}
	return buf.String()
}

func (g *Generator) generatePVC(name, namespace string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
`, sanitizeName(name), namespace)
}

func (g *Generator) generateDeployment(svc *mapper.DockerService, namespace string, replicas int) string {
	name := sanitizeName(svc.Name)
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  replicas: %d
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
        - name: %s
          image: %s
          imagePullPolicy: IfNotPresent
`, name, namespace, name, replicas, name, name, name, svc.Image))

	// Ports
	if len(svc.Ports) > 0 {
		buf.WriteString("          ports:\n")
		for _, p := range svc.Ports {
			port := extractContainerPort(p)
			buf.WriteString(fmt.Sprintf("            - containerPort: %s\n", port))
		}
	}

	// Env from ConfigMap/Secret
	if len(svc.Environment) > 0 {
		buf.WriteString("          envFrom:\n")
		configVars, secretVars := splitEnvVars(svc.Environment)
		if len(configVars) > 0 {
			buf.WriteString(fmt.Sprintf("            - configMapRef:\n                name: %s-config\n", name))
		}
		if len(secretVars) > 0 {
			buf.WriteString(fmt.Sprintf("            - secretRef:\n                name: %s-secret\n", name))
		}
	}

	// Volume mounts
	if len(svc.Volumes) > 0 {
		buf.WriteString("          volumeMounts:\n")
		for _, vol := range svc.Volumes {
			volName, mountPath, isNamed := parseVolume(vol)
			if isNamed && mountPath != "" {
				buf.WriteString(fmt.Sprintf("            - name: %s\n              mountPath: %s\n", sanitizeName(volName), mountPath))
			}
		}
		buf.WriteString("      volumes:\n")
		for _, vol := range svc.Volumes {
			volName, _, isNamed := parseVolume(vol)
			if isNamed {
				buf.WriteString(fmt.Sprintf("        - name: %s\n          persistentVolumeClaim:\n            claimName: %s\n", sanitizeName(volName), sanitizeName(volName)))
			}
		}
	}

	// Health check -> probes
	if svc.HealthCheck != nil && len(svc.HealthCheck.Test) > 0 {
		buf.WriteString("          livenessProbe:\n")
		buf.WriteString("            exec:\n")
		buf.WriteString(fmt.Sprintf("              command: %v\n", svc.HealthCheck.Test))
		buf.WriteString(fmt.Sprintf("            periodSeconds: %d\n", int(svc.HealthCheck.Interval.Seconds())))
		buf.WriteString(fmt.Sprintf("            timeoutSeconds: %d\n", int(svc.HealthCheck.Timeout.Seconds())))
	}

	// Resources
	buf.WriteString("          resources:\n")
	buf.WriteString("            requests:\n")
	buf.WriteString("              memory: \"128Mi\"\n")
	buf.WriteString("              cpu: \"100m\"\n")
	buf.WriteString("            limits:\n")
	buf.WriteString("              memory: \"512Mi\"\n")
	buf.WriteString("              cpu: \"500m\"\n")

	return buf.String()
}

func (g *Generator) generateService(svc *mapper.DockerService, namespace string) string {
	name := sanitizeName(svc.Name)
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  selector:
    app: %s
  ports:
`, name, namespace, name))

	for _, p := range svc.Ports {
		port := extractContainerPort(p)
		buf.WriteString(fmt.Sprintf("    - port: %s\n      targetPort: %s\n", port, port))
	}

	return buf.String()
}

func (g *Generator) generateIngress(svc *mapper.DockerService, namespace string, config *generator.TargetConfig) string {
	name := sanitizeName(svc.Name)
	host := config.BaseURL
	if strings.HasPrefix(host, "http://") {
		host = strings.TrimPrefix(host, "http://")
	}
	if strings.HasPrefix(host, "https://") {
		host = strings.TrimPrefix(host, "https://")
	}

	port := "80"
	if len(svc.Ports) > 0 {
		port = extractContainerPort(svc.Ports[0])
	}

	return fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: %s
  namespace: %s
  annotations:
    kubernetes.io/ingress.class: traefik
spec:
  rules:
    - host: %s.%s
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: %s
                port:
                  number: %s
`, name, namespace, name, host, name, port)
}

// ─────────────────────────────────────────────────────────────────────────────
// Utility Functions
// ─────────────────────────────────────────────────────────────────────────────

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, ".", "-")
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.Trim(name, "-")
}

func splitEnvVars(env map[string]string) (config, secrets map[string]string) {
	config = make(map[string]string)
	secrets = make(map[string]string)
	sensitivePatterns := []string{"PASSWORD", "SECRET", "KEY", "TOKEN", "CREDENTIAL"}
	for k, v := range env {
		isSensitive := false
		upper := strings.ToUpper(k)
		for _, p := range sensitivePatterns {
			if strings.Contains(upper, p) {
				isSensitive = true
				break
			}
		}
		if isSensitive {
			secrets[k] = v
		} else {
			config[k] = v
		}
	}
	return
}

func splitStackEnvVars(env map[string]string) (config, secrets map[string]string) {
	return splitEnvVars(env)
}

func isSensitiveEnvVar(key string) bool {
	sensitivePatterns := []string{"PASSWORD", "SECRET", "KEY", "TOKEN", "CREDENTIAL", "API_KEY", "AUTH", "PRIVATE"}
	upper := strings.ToUpper(key)
	for _, p := range sensitivePatterns {
		if strings.Contains(upper, p) {
			return true
		}
	}
	return false
}

func parseVolume(vol string) (name, path string, isNamed bool) {
	parts := strings.Split(vol, ":")
	if len(parts) < 2 {
		return vol, "", false
	}
	name = parts[0]
	path = parts[1]
	isNamed = !strings.Contains(name, "/") && !strings.Contains(name, ".")
	return
}

func extractContainerPort(portSpec string) string {
	parts := strings.Split(portSpec, ":")
	if len(parts) == 2 {
		return parts[1]
	}
	return strings.Split(parts[0], "/")[0]
}

func isExposed(svc *mapper.DockerService) bool {
	for k := range svc.Labels {
		if strings.HasPrefix(k, "traefik.") {
			return true
		}
	}
	for _, p := range svc.Ports {
		if strings.Contains(p, "80") || strings.Contains(p, "443") || strings.Contains(p, "8080") {
			return true
		}
	}
	return false
}

func isStackServiceExposed(svc *stack.Service) bool {
	for k := range svc.Labels {
		if strings.HasPrefix(k, "traefik.") {
			return true
		}
	}
	for _, p := range svc.Ports {
		if strings.Contains(p, "80") || strings.Contains(p, "443") || strings.Contains(p, "8080") {
			return true
		}
	}
	return false
}

func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func init() {
	generator.RegisterGenerator(New())
	generator.RegisterGenerator(NewK3s())
}
