package stacks

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"

	"gopkg.in/yaml.v3"
)

// ComputeMerger consolidates serverless compute resources into OpenFaaS.
// It handles AWS Lambda, GCP Cloud Functions, Azure Functions, and similar
// services, mapping them to a unified OpenFaaS deployment.
type ComputeMerger struct {
	*consolidator.BaseMerger
}

// NewComputeMerger creates a new ComputeMerger instance.
func NewComputeMerger() *ComputeMerger {
	return &ComputeMerger{
		BaseMerger: consolidator.NewBaseMerger(stack.StackTypeCompute),
	}
}

// StackType returns the stack type this merger handles.
func (m *ComputeMerger) StackType() stack.StackType {
	return stack.StackTypeCompute
}

// CanMerge checks if this merger can handle the given results.
// Returns true if there are any serverless compute resources present.
func (m *ComputeMerger) CanMerge(results []*mapper.MappingResult) bool {
	if len(results) == 0 {
		return false
	}

	for _, result := range results {
		if result == nil {
			continue
		}
		resourceType := strings.ToLower(result.SourceResourceType)
		if isComputeResource(resourceType) {
			return true
		}
	}

	return false
}

// Merge creates a consolidated serverless compute stack with OpenFaaS.
// It generates:
// - OpenFaaS gateway service
// - OpenFaaS provider (faasd for Docker)
// - Function definitions mapped from Lambda/Functions/Azure Functions
// - stack.yml for function definitions
// - Deployment scripts
func (m *ComputeMerger) Merge(ctx context.Context, results []*mapper.MappingResult, opts *consolidator.MergeOptions) (*stack.Stack, error) {
	if opts == nil {
		opts = consolidator.DefaultOptions()
	}

	// Create the stack
	name := "compute"
	if opts.NamePrefix != "" {
		name = opts.NamePrefix + "-compute"
	}

	stk := stack.NewStack(stack.StackTypeCompute, name)
	stk.Description = "Serverless functions with OpenFaaS"

	// Create OpenFaaS gateway service
	gatewayService := m.createGatewayService(opts)
	stk.AddService(gatewayService)

	// Create faasd provider service (for Docker environments)
	providerService := m.createFaasdService(opts)
	stk.AddService(providerService)

	// Create Prometheus service for metrics (OpenFaaS dependency)
	prometheusService := m.createPrometheusService()
	stk.AddService(prometheusService)

	// Create NATS service for async invocations
	natsService := m.createNATSService()
	stk.AddService(natsService)

	// Add volumes
	stk.AddVolume(stack.Volume{
		Name:   "openfaas-data",
		Driver: "local",
		Labels: map[string]string{
			"homeport.io/stack": "compute",
			"homeport.io/role":  "data",
		},
	})

	stk.AddVolume(stack.Volume{
		Name:   "prometheus-data",
		Driver: "local",
		Labels: map[string]string{
			"homeport.io/stack": "compute",
			"homeport.io/role":  "metrics",
		},
	})

	// Convert cloud functions to OpenFaaS function definitions
	functions := make(map[string]*OpenFaaSFunction)
	var functionConfigs []FunctionConfig

	for _, result := range results {
		if result == nil {
			continue
		}

		// Add source resource
		res := &resource.Resource{
			Type: resource.Type(result.SourceResourceType),
			Name: result.SourceResourceName,
		}
		stk.AddSourceResource(res)

		// Convert to OpenFaaS function
		fn, cfg := m.convertToOpenFaaSFunction(result)
		if fn != nil {
			fnName := consolidator.NormalizeName(result.SourceResourceName)
			functions[fnName] = fn
			functionConfigs = append(functionConfigs, cfg)
		}

		// Merge warnings
		for _, warning := range result.Warnings {
			stk.Metadata["warning_"+result.SourceResourceName] = warning
		}
	}

	// Generate OpenFaaS stack.yml
	if len(functions) > 0 {
		openfaasStack := m.generateOpenFaaSStack(functions)
		stackYAML, err := yaml.Marshal(openfaasStack)
		if err != nil {
			stk.Metadata["error"] = fmt.Sprintf("Failed to generate stack.yml: %v", err)
		} else {
			stk.AddConfig("stack.yml", stackYAML)
		}
	}

	// Generate deployment script
	deployScript := m.generateDeployScript(functions)
	stk.AddScript("deploy-functions.sh", deployScript)

	// Generate function migration guide
	migrationGuide := m.generateMigrationGuide(functionConfigs)
	stk.AddConfig("migration-guide.md", migrationGuide)

	// Generate Prometheus configuration
	prometheusConfig := m.generatePrometheusConfig()
	stk.AddConfig("prometheus.yml", prometheusConfig)

	// Add manual steps
	stk.Metadata["manual_step_1"] = "Build function Docker images using function source code"
	stk.Metadata["manual_step_2"] = "Push images to your container registry"
	stk.Metadata["manual_step_3"] = "Update stack.yml with correct image names"
	stk.Metadata["manual_step_4"] = "Run deploy-functions.sh to deploy functions"
	stk.Metadata["manual_step_5"] = "Update application code to use OpenFaaS gateway URLs"

	// Add networks
	stk.AddNetwork(stack.Network{
		Name:   "openfaas-net",
		Driver: "bridge",
	})

	return stk, nil
}

// createGatewayService creates the OpenFaaS gateway service.
func (m *ComputeMerger) createGatewayService(opts *consolidator.MergeOptions) *stack.Service {
	svc := stack.NewService("gateway", "openfaas/gateway:latest")

	svc.Ports = []string{"8080:8080"}

	svc.Environment = map[string]string{
		"functions_provider_url":  "http://faasd-provider:8081/",
		"direct_functions":        "false",
		"read_timeout":            "65s",
		"write_timeout":           "65s",
		"upstream_timeout":        "60s",
		"faas_nats_address":       "nats",
		"faas_nats_port":          "4222",
		"faas_prometheus_host":    "prometheus",
		"faas_prometheus_port":    "9090",
		"basic_auth":              "true",
		"secret_mount_path":       "/run/secrets",
		"scale_from_zero":         "true",
		"max_idle_conns":          "1024",
		"max_idle_conns_per_host": "1024",
	}

	svc.Volumes = []string{
		"openfaas-data:/var/lib/faasd",
	}

	svc.Labels = map[string]string{
		"homeport.io/stack":     "compute",
		"homeport.io/role":      "gateway",
		"homeport.io/component": "openfaas",
	}

	svc.DependsOn = []string{"faasd-provider", "nats", "prometheus"}

	svc.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/healthz"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "10s",
	}

	svc.Restart = "unless-stopped"
	svc.Networks = []string{"openfaas-net"}

	svc.Deploy = &stack.DeployConfig{
		Replicas: 1,
		Resources: &stack.ResourceConfig{
			Limits: &stack.ResourceSpec{
				CPUs:   "1",
				Memory: "256M",
			},
			Reservations: &stack.ResourceSpec{
				CPUs:   "0.1",
				Memory: "64M",
			},
		},
	}

	return svc
}

// createFaasdService creates the faasd provider service for Docker environments.
func (m *ComputeMerger) createFaasdService(opts *consolidator.MergeOptions) *stack.Service {
	svc := stack.NewService("faasd-provider", "openfaas/faasd:latest")

	svc.Ports = []string{"8081:8081"}

	svc.Environment = map[string]string{
		"port":                  "8081",
		"sock_path":             "/var/run/docker.sock",
		"function_namespace":    "openfaas-fn",
		"read_timeout":          "60s",
		"write_timeout":         "60s",
		"image_pull_policy":     "Always",
		"gateway_invoke":        "true",
		"gateway_url":           "http://gateway:8080/",
		"service_type":          "ClusterIP",
		"enable_function_readiness_probe": "true",
	}

	svc.Volumes = []string{
		"/var/run/docker.sock:/var/run/docker.sock:ro",
		"openfaas-data:/var/lib/faasd",
	}

	svc.Labels = map[string]string{
		"homeport.io/stack":     "compute",
		"homeport.io/role":      "provider",
		"homeport.io/component": "faasd",
	}

	svc.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8081/healthz"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "10s",
	}

	svc.Restart = "unless-stopped"
	svc.Networks = []string{"openfaas-net"}

	return svc
}

// createPrometheusService creates the Prometheus service for OpenFaaS metrics.
func (m *ComputeMerger) createPrometheusService() *stack.Service {
	svc := stack.NewService("prometheus", "prom/prometheus:latest")

	svc.Ports = []string{"9090:9090"}

	svc.Command = []string{
		"--config.file=/etc/prometheus/prometheus.yml",
		"--storage.tsdb.path=/prometheus",
		"--storage.tsdb.retention.time=15d",
	}

	svc.Volumes = []string{
		"prometheus-data:/prometheus",
		"./prometheus.yml:/etc/prometheus/prometheus.yml:ro",
	}

	svc.Labels = map[string]string{
		"homeport.io/stack":     "compute",
		"homeport.io/role":      "metrics",
		"homeport.io/component": "prometheus",
	}

	svc.Restart = "unless-stopped"
	svc.Networks = []string{"openfaas-net"}

	return svc
}

// createNATSService creates the NATS service for async function invocations.
func (m *ComputeMerger) createNATSService() *stack.Service {
	svc := stack.NewService("nats", "nats:latest")

	svc.Ports = []string{"4222:4222", "8222:8222"}

	svc.Command = []string{"-js", "-m", "8222"}

	svc.Labels = map[string]string{
		"homeport.io/stack":     "compute",
		"homeport.io/role":      "messaging",
		"homeport.io/component": "nats",
	}

	svc.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8222/healthz"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "5s",
	}

	svc.Restart = "unless-stopped"
	svc.Networks = []string{"openfaas-net"}

	return svc
}

// OpenFaaS stack definition types

// OpenFaaSStack represents an OpenFaaS stack.yml structure.
type OpenFaaSStack struct {
	Version   string                      `yaml:"version"`
	Provider  OpenFaaSProvider            `yaml:"provider"`
	Functions map[string]OpenFaaSFunction `yaml:"functions"`
}

// OpenFaaSProvider represents the OpenFaaS provider configuration.
type OpenFaaSProvider struct {
	Name    string `yaml:"name"`
	Gateway string `yaml:"gateway"`
}

// OpenFaaSFunction represents an OpenFaaS function definition.
type OpenFaaSFunction struct {
	Lang        string            `yaml:"lang,omitempty"`
	Handler     string            `yaml:"handler,omitempty"`
	Image       string            `yaml:"image"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Secrets     []string          `yaml:"secrets,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Limits      *ResourceLimits   `yaml:"limits,omitempty"`
	Requests    *ResourceLimits   `yaml:"requests,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
	ReadOnly    bool              `yaml:"readonly_root_filesystem,omitempty"`
}

// ResourceLimits represents resource limits for a function.
type ResourceLimits struct {
	Memory string `yaml:"memory,omitempty"`
	CPU    string `yaml:"cpu,omitempty"`
}

// FunctionConfig holds the source configuration for migration documentation.
type FunctionConfig struct {
	Name          string
	SourceType    string
	SourceRuntime string
	SourceMemory  string
	SourceTimeout string
	Handler       string
	Description   string
}

// convertToOpenFaaSFunction converts a cloud function to OpenFaaS function.
func (m *ComputeMerger) convertToOpenFaaSFunction(result *mapper.MappingResult) (*OpenFaaSFunction, FunctionConfig) {
	fn := &OpenFaaSFunction{
		Environment: make(map[string]string),
		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}

	cfg := FunctionConfig{
		Name:       result.SourceResourceName,
		SourceType: result.SourceResourceType,
	}

	resourceType := strings.ToLower(result.SourceResourceType)

	// Extract environment variables from source
	if result.DockerService != nil && result.DockerService.Environment != nil {
		for k, v := range result.DockerService.Environment {
			fn.Environment[k] = v
		}
	}

	// Extract deploy config for resource limits
	if result.DockerService != nil && result.DockerService.Deploy != nil {
		if result.DockerService.Deploy.Resources != nil {
			if result.DockerService.Deploy.Resources.Limits != nil {
				fn.Limits = &ResourceLimits{
					Memory: result.DockerService.Deploy.Resources.Limits.Memory,
					CPU:    result.DockerService.Deploy.Resources.Limits.CPUs,
				}
				cfg.SourceMemory = result.DockerService.Deploy.Resources.Limits.Memory
			}
			if result.DockerService.Deploy.Resources.Reservations != nil {
				fn.Requests = &ResourceLimits{
					Memory: result.DockerService.Deploy.Resources.Reservations.Memory,
					CPU:    result.DockerService.Deploy.Resources.Reservations.CPUs,
				}
			}
		}
	}

	// Detect runtime and set defaults based on source type
	switch {
	case strings.Contains(resourceType, "lambda"):
		fn, cfg = m.convertLambdaToFunction(result, fn, cfg)

	case strings.Contains(resourceType, "cloud_function") || strings.Contains(resourceType, "cloudfunctions"):
		fn, cfg = m.convertCloudFunctionToFunction(result, fn, cfg)

	case strings.Contains(resourceType, "azure_function") || strings.Contains(resourceType, "function_app"):
		fn, cfg = m.convertAzureFunctionToFunction(result, fn, cfg)

	default:
		// Generic serverless function
		fn.Lang = "dockerfile"
		fn.Handler = "./"
		fn.Image = fmt.Sprintf("functions/%s:latest", consolidator.NormalizeName(result.SourceResourceName))
		cfg.SourceRuntime = "unknown"
		cfg.Description = "Generic serverless function"
	}

	// Add source tracking labels
	fn.Labels["homeport.source.type"] = result.SourceResourceType
	fn.Labels["homeport.source.name"] = result.SourceResourceName

	// Set read-only filesystem for security
	fn.ReadOnly = true

	return fn, cfg
}

// convertLambdaToFunction converts AWS Lambda to OpenFaaS.
func (m *ComputeMerger) convertLambdaToFunction(result *mapper.MappingResult, fn *OpenFaaSFunction, cfg FunctionConfig) (*OpenFaaSFunction, FunctionConfig) {
	cfg.Description = "AWS Lambda function"

	// Try to detect runtime from environment or defaults
	runtime := m.detectRuntime(result)
	fn.Lang = mapRuntimeToOpenFaaSLang(runtime)
	cfg.SourceRuntime = runtime

	// Set handler
	fn.Handler = "./"
	if result.DockerService != nil {
		if handler, ok := result.DockerService.Environment["AWS_LAMBDA_FUNCTION_HANDLER"]; ok {
			fn.Handler = handler
			cfg.Handler = handler
		}
	}

	// Set image
	fn.Image = fmt.Sprintf("functions/%s:latest", consolidator.NormalizeName(result.SourceResourceName))

	// Default Lambda limits if not set
	if fn.Limits == nil {
		fn.Limits = &ResourceLimits{
			Memory: "128Mi",
			CPU:    "200m",
		}
	}

	// Add Lambda-specific annotations
	fn.Annotations["com.openfaas.scale.min"] = "1"
	fn.Annotations["com.openfaas.scale.max"] = "10"
	fn.Annotations["com.openfaas.scale.zero"] = "true"

	return fn, cfg
}

// convertCloudFunctionToFunction converts GCP Cloud Function to OpenFaaS.
func (m *ComputeMerger) convertCloudFunctionToFunction(result *mapper.MappingResult, fn *OpenFaaSFunction, cfg FunctionConfig) (*OpenFaaSFunction, FunctionConfig) {
	cfg.Description = "GCP Cloud Function"

	// Detect runtime
	runtime := m.detectRuntime(result)
	fn.Lang = mapRuntimeToOpenFaaSLang(runtime)
	cfg.SourceRuntime = runtime

	// Set handler
	fn.Handler = "./"
	if result.DockerService != nil {
		if handler, ok := result.DockerService.Environment["FUNCTION_TARGET"]; ok {
			fn.Handler = handler
			cfg.Handler = handler
		}
	}

	// Set image
	fn.Image = fmt.Sprintf("functions/%s:latest", consolidator.NormalizeName(result.SourceResourceName))

	// Default Cloud Function limits
	if fn.Limits == nil {
		fn.Limits = &ResourceLimits{
			Memory: "256Mi",
			CPU:    "200m",
		}
	}

	return fn, cfg
}

// convertAzureFunctionToFunction converts Azure Function to OpenFaaS.
func (m *ComputeMerger) convertAzureFunctionToFunction(result *mapper.MappingResult, fn *OpenFaaSFunction, cfg FunctionConfig) (*OpenFaaSFunction, FunctionConfig) {
	cfg.Description = "Azure Function"

	// Detect runtime
	runtime := m.detectRuntime(result)
	fn.Lang = mapRuntimeToOpenFaaSLang(runtime)
	cfg.SourceRuntime = runtime

	// Set handler
	fn.Handler = "./"
	if result.DockerService != nil {
		if handler, ok := result.DockerService.Environment["AzureFunctionsJobHost__FunctionTimeout"]; ok {
			cfg.SourceTimeout = handler
		}
	}

	// Set image
	fn.Image = fmt.Sprintf("functions/%s:latest", consolidator.NormalizeName(result.SourceResourceName))

	// Default Azure Function limits
	if fn.Limits == nil {
		fn.Limits = &ResourceLimits{
			Memory: "256Mi",
			CPU:    "200m",
		}
	}

	return fn, cfg
}

// detectRuntime attempts to detect the function runtime from the result.
func (m *ComputeMerger) detectRuntime(result *mapper.MappingResult) string {
	if result.DockerService == nil {
		return "unknown"
	}

	// Check environment variables for runtime hints
	env := result.DockerService.Environment

	// AWS Lambda runtime
	if runtime, ok := env["AWS_LAMBDA_RUNTIME_API"]; ok {
		return normalizeRuntime(runtime)
	}
	if runtime, ok := env["LAMBDA_RUNTIME_DIR"]; ok {
		return normalizeRuntime(runtime)
	}

	// GCP runtime
	if runtime, ok := env["FUNCTION_RUNTIME"]; ok {
		return normalizeRuntime(runtime)
	}

	// Azure runtime
	if runtime, ok := env["FUNCTIONS_WORKER_RUNTIME"]; ok {
		return normalizeRuntime(runtime)
	}

	// Check image name for hints
	image := strings.ToLower(result.DockerService.Image)
	switch {
	case strings.Contains(image, "node") || strings.Contains(image, "nodejs"):
		return "node18"
	case strings.Contains(image, "python"):
		return "python3"
	case strings.Contains(image, "golang") || strings.Contains(image, "go"):
		return "go"
	case strings.Contains(image, "java"):
		return "java11"
	case strings.Contains(image, "dotnet") || strings.Contains(image, "csharp"):
		return "csharp"
	case strings.Contains(image, "ruby"):
		return "ruby"
	case strings.Contains(image, "rust"):
		return "rust"
	}

	return "unknown"
}

// normalizeRuntime normalizes cloud provider runtime names to standard format.
func normalizeRuntime(runtime string) string {
	runtime = strings.ToLower(runtime)

	switch {
	case strings.Contains(runtime, "nodejs") || strings.Contains(runtime, "node"):
		if strings.Contains(runtime, "18") {
			return "node18"
		} else if strings.Contains(runtime, "16") {
			return "node16"
		} else if strings.Contains(runtime, "20") {
			return "node20"
		}
		return "node18"

	case strings.Contains(runtime, "python"):
		if strings.Contains(runtime, "3.11") {
			return "python3.11"
		} else if strings.Contains(runtime, "3.10") {
			return "python3.10"
		} else if strings.Contains(runtime, "3.9") {
			return "python3.9"
		}
		return "python3"

	case strings.Contains(runtime, "go"):
		if strings.Contains(runtime, "1.21") {
			return "go1.21"
		} else if strings.Contains(runtime, "1.20") {
			return "go1.20"
		}
		return "go"

	case strings.Contains(runtime, "java"):
		if strings.Contains(runtime, "17") {
			return "java17"
		} else if strings.Contains(runtime, "11") {
			return "java11"
		}
		return "java11"

	case strings.Contains(runtime, "dotnet") || strings.Contains(runtime, "csharp"):
		return "csharp"

	case strings.Contains(runtime, "ruby"):
		return "ruby"

	case strings.Contains(runtime, "rust"):
		return "rust"
	}

	return runtime
}

// mapRuntimeToOpenFaaSLang maps runtime names to OpenFaaS language templates.
func mapRuntimeToOpenFaaSLang(runtime string) string {
	switch {
	case strings.HasPrefix(runtime, "node"):
		return "node18"
	case strings.HasPrefix(runtime, "python"):
		return "python3"
	case strings.HasPrefix(runtime, "go"):
		return "golang-http"
	case strings.HasPrefix(runtime, "java"):
		return "java11"
	case runtime == "csharp" || strings.HasPrefix(runtime, "dotnet"):
		return "csharp"
	case runtime == "ruby":
		return "ruby"
	case runtime == "rust":
		return "rust"
	default:
		return "dockerfile"
	}
}

// generateOpenFaaSStack generates the OpenFaaS stack.yml configuration.
func (m *ComputeMerger) generateOpenFaaSStack(functions map[string]*OpenFaaSFunction) *OpenFaaSStack {
	stackYML := &OpenFaaSStack{
		Version: "1.0",
		Provider: OpenFaaSProvider{
			Name:    "openfaas",
			Gateway: "http://gateway:8080",
		},
		Functions: make(map[string]OpenFaaSFunction),
	}

	for name, fn := range functions {
		stackYML.Functions[name] = *fn
	}

	return stackYML
}

// generateDeployScript generates a script to deploy functions to OpenFaaS.
func (m *ComputeMerger) generateDeployScript(functions map[string]*OpenFaaSFunction) []byte {
	tmpl := `#!/bin/bash
# OpenFaaS Function Deployment Script
# Generated by Homeport

set -e

OPENFAAS_URL="${OPENFAAS_URL:-http://localhost:8080}"
REGISTRY="${REGISTRY:-docker.io}"

echo "OpenFaaS Function Deployment"
echo "============================"
echo ""
echo "Gateway: $OPENFAAS_URL"
echo "Registry: $REGISTRY"
echo ""

# Check for faas-cli
if ! command -v faas-cli &> /dev/null; then
    echo "Installing faas-cli..."
    curl -sSL https://cli.openfaas.com | sudo sh
fi

# Login to OpenFaaS
echo "Logging in to OpenFaaS..."
if [ -z "$OPENFAAS_PASSWORD" ]; then
    echo "OPENFAAS_PASSWORD not set. Using basic auth file or interactive login."
    faas-cli login --gateway "$OPENFAAS_URL" || true
else
    echo "$OPENFAAS_PASSWORD" | faas-cli login --gateway "$OPENFAAS_URL" --password-stdin
fi

# Pull templates
echo "Pulling function templates..."
faas-cli template pull

# Build and deploy each function
echo ""
echo "Building and deploying functions..."

{{range $name, $fn := .Functions}}
echo "Processing function: {{$name}}"
echo "  Language: {{$fn.Lang}}"
echo "  Image: {{$fn.Image}}"

# Build the function
faas-cli build -f stack.yml --filter={{$name}} --tag=latest

# Push to registry (if not local)
if [ "$PUSH_TO_REGISTRY" = "true" ]; then
    faas-cli push -f stack.yml --filter={{$name}} --tag=latest
fi

# Deploy the function
faas-cli deploy -f stack.yml --filter={{$name}} --gateway="$OPENFAAS_URL"

echo "  Deployed: {{$name}}"
echo ""

{{end}}

echo "Deployment complete!"
echo ""
echo "List deployed functions:"
faas-cli list --gateway="$OPENFAAS_URL"
echo ""
echo "To invoke a function:"
echo "  curl -X POST $OPENFAAS_URL/function/<function-name> -d 'input'"
`

	t := template.Must(template.New("deploy").Parse(tmpl))

	var buf bytes.Buffer
	data := struct {
		Functions map[string]*OpenFaaSFunction
	}{
		Functions: functions,
	}

	if err := t.Execute(&buf, data); err != nil {
		return []byte("#!/bin/bash\n# Error generating deploy script\n")
	}

	return buf.Bytes()
}

// generateMigrationGuide generates documentation for function migration.
func (m *ComputeMerger) generateMigrationGuide(configs []FunctionConfig) []byte {
	var buf bytes.Buffer

	buf.WriteString("# Function Migration Guide\n\n")
	buf.WriteString("This guide explains how to migrate your cloud functions to OpenFaaS.\n\n")

	buf.WriteString("## Functions to Migrate\n\n")
	buf.WriteString("| Function | Source | Runtime | Memory | Handler |\n")
	buf.WriteString("|----------|--------|---------|--------|--------|\n")

	for _, cfg := range configs {
		buf.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			cfg.Name, cfg.SourceType, cfg.SourceRuntime, cfg.SourceMemory, cfg.Handler))
	}

	buf.WriteString("\n## Migration Steps\n\n")
	buf.WriteString("### 1. Prepare Function Code\n\n")
	buf.WriteString("Each function needs to be adapted for OpenFaaS:\n\n")
	buf.WriteString("**AWS Lambda:**\n")
	buf.WriteString("```python\n")
	buf.WriteString("# Before (Lambda)\n")
	buf.WriteString("def lambda_handler(event, context):\n")
	buf.WriteString("    return {'statusCode': 200, 'body': 'Hello'}\n")
	buf.WriteString("\n")
	buf.WriteString("# After (OpenFaaS)\n")
	buf.WriteString("def handle(req):\n")
	buf.WriteString("    return 'Hello'\n")
	buf.WriteString("```\n\n")

	buf.WriteString("**GCP Cloud Function:**\n")
	buf.WriteString("```python\n")
	buf.WriteString("# Before (GCP)\n")
	buf.WriteString("def hello_world(request):\n")
	buf.WriteString("    return 'Hello'\n")
	buf.WriteString("\n")
	buf.WriteString("# After (OpenFaaS)\n")
	buf.WriteString("def handle(req):\n")
	buf.WriteString("    return 'Hello'\n")
	buf.WriteString("```\n\n")

	buf.WriteString("**Azure Function:**\n")
	buf.WriteString("```python\n")
	buf.WriteString("# Before (Azure)\n")
	buf.WriteString("import azure.functions as func\n")
	buf.WriteString("def main(req: func.HttpRequest) -> func.HttpResponse:\n")
	buf.WriteString("    return func.HttpResponse('Hello')\n")
	buf.WriteString("\n")
	buf.WriteString("# After (OpenFaaS)\n")
	buf.WriteString("def handle(req):\n")
	buf.WriteString("    return 'Hello'\n")
	buf.WriteString("```\n\n")

	buf.WriteString("### 2. Create Function Directory\n\n")
	buf.WriteString("For each function, create a directory structure:\n")
	buf.WriteString("```\n")
	buf.WriteString("functions/\n")
	buf.WriteString("  my-function/\n")
	buf.WriteString("    handler.py    # Your function code\n")
	buf.WriteString("    requirements.txt  # Dependencies\n")
	buf.WriteString("```\n\n")

	buf.WriteString("### 3. Build and Deploy\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Build the function\n")
	buf.WriteString("faas-cli build -f stack.yml\n\n")
	buf.WriteString("# Deploy to OpenFaaS\n")
	buf.WriteString("faas-cli deploy -f stack.yml\n")
	buf.WriteString("```\n\n")

	buf.WriteString("### 4. Update Triggers\n\n")
	buf.WriteString("Replace cloud-specific triggers with OpenFaaS alternatives:\n\n")
	buf.WriteString("| Cloud Trigger | OpenFaaS Alternative |\n")
	buf.WriteString("|---------------|---------------------|\n")
	buf.WriteString("| API Gateway | OpenFaaS Gateway HTTP |\n")
	buf.WriteString("| S3 Events | MinIO + NATS connector |\n")
	buf.WriteString("| CloudWatch Events | Cron Connector |\n")
	buf.WriteString("| SQS/SNS | NATS Connector |\n")
	buf.WriteString("| Pub/Sub | NATS Connector |\n\n")

	buf.WriteString("## Invoking Functions\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Synchronous invocation\n")
	buf.WriteString("curl -X POST http://gateway:8080/function/my-function -d '{\"key\": \"value\"}'\n\n")
	buf.WriteString("# Async invocation\n")
	buf.WriteString("curl -X POST http://gateway:8080/async-function/my-function -d '{\"key\": \"value\"}'\n")
	buf.WriteString("```\n")

	return buf.Bytes()
}

// generatePrometheusConfig generates Prometheus configuration for OpenFaaS.
func (m *ComputeMerger) generatePrometheusConfig() []byte {
	config := `# Prometheus Configuration for OpenFaaS
# Generated by Homeport

global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'openfaas-gateway'
    static_configs:
      - targets: ['gateway:8080']
    metrics_path: /metrics

  - job_name: 'openfaas-functions'
    static_configs:
      - targets: ['gateway:8080']
    metrics_path: /function/
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: gateway:8080

  - job_name: 'nats'
    static_configs:
      - targets: ['nats:8222']
    metrics_path: /metrics
`
	return []byte(config)
}

// isComputeResource checks if a resource type is a serverless compute resource.
func isComputeResource(resourceType string) bool {
	computeTypes := []string{
		"lambda",
		"cloud_function",
		"cloudfunctions",
		"azure_function",
		"function_app",
		"serverless",
	}

	for _, t := range computeTypes {
		if strings.Contains(resourceType, t) {
			return true
		}
	}

	return false
}
