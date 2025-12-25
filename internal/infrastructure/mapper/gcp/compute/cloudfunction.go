// Package compute provides mappers for GCP compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// CloudFunctionMapper converts GCP Cloud Functions to Docker/OpenFaaS.
type CloudFunctionMapper struct {
	*mapper.BaseMapper
}

// NewCloudFunctionMapper creates a new Cloud Functions to Docker mapper.
func NewCloudFunctionMapper() *CloudFunctionMapper {
	return &CloudFunctionMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCloudFunction, nil),
	}
}

// Map converts a Cloud Function to a Docker service.
func (m *CloudFunctionMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	functionName := res.GetConfigString("name")
	if functionName == "" {
		functionName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeName(functionName))
	svc := result.DockerService

	runtime := res.GetConfigString("runtime")
	entryPoint := res.GetConfigString("entry_point")
	availableMemory := res.GetConfigString("available_memory")
	timeout := res.GetConfigInt("timeout")

	// Generate Docker image based on runtime
	svc.Image = fmt.Sprintf("%s:latest", m.sanitizeName(functionName))

	// Configure service
	svc.Environment = map[string]string{
		"FUNCTION_NAME":  functionName,
		"FUNCTION_ENTRY": entryPoint,
		"PORT":           "8080",
	}

	// Add any configured environment variables
	if envVars := res.Config["environment_variables"]; envVars != nil {
		if envMap, ok := envVars.(map[string]interface{}); ok {
			for k, v := range envMap {
				if strVal, ok := v.(string); ok {
					svc.Environment[k] = strVal
				}
			}
		}
	}

	svc.Ports = []string{"8080:8080"}
	svc.Networks = []string{"cloudexit"}
	svc.Restart = "unless-stopped"

	// Resource limits
	svc.Deploy = &mapper.DeployConfig{
		Resources: &mapper.Resources{
			Limits: &mapper.ResourceLimits{
				Memory: m.parseMemory(availableMemory),
				CPUs:   m.cpuFromMemory(availableMemory),
			},
		},
	}

	// Labels
	svc.Labels = map[string]string{
		"cloudexit.source":        "google_cloudfunctions_function",
		"cloudexit.function_name": functionName,
		"cloudexit.runtime":       runtime,
		"traefik.enable":          "true",
		"traefik.http.routers." + m.sanitizeName(functionName) + ".rule": fmt.Sprintf("Host(`%s.localhost`)", m.sanitizeName(functionName)),
	}

	// Health check
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8080/ || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  time.Duration(timeout) * time.Second,
		Retries:  3,
	}

	// Generate Dockerfile
	dockerfile := m.generateDockerfile(runtime, entryPoint, functionName)
	result.AddConfig(fmt.Sprintf("functions/%s/Dockerfile", functionName), []byte(dockerfile))

	// Generate handler template
	handlerPath, handlerContent := m.generateHandler(runtime, entryPoint, functionName)
	result.AddConfig(handlerPath, []byte(handlerContent))

	// Handle event triggers
	if eventTrigger := res.Config["event_trigger"]; eventTrigger != nil {
		m.handleEventTrigger(eventTrigger, result)
	}

	// Handle HTTP triggers
	if httpsTriggerURL := res.GetConfigString("https_trigger_url"); httpsTriggerURL != "" {
		result.AddWarning(fmt.Sprintf("HTTP trigger configured. Access at: http://%s.localhost", m.sanitizeName(functionName)))
	}

	// Handle VPC connector
	if vpcConnector := res.GetConfigString("vpc_connector"); vpcConnector != "" {
		result.AddWarning("VPC Connector is configured. Configure Docker networks accordingly.")
	}

	// Handle secrets
	if secretEnvVars := res.Config["secret_environment_variables"]; secretEnvVars != nil {
		result.AddWarning("Secret environment variables detected. Configure secrets manually.")
		result.AddManualStep("Set up secret environment variables from your secrets manager")
	}

	result.AddManualStep("Build function Docker image: docker build -t " + m.sanitizeName(functionName) + " ./functions/" + functionName)
	result.AddManualStep("Configure event triggers manually if using pub/sub or storage events")

	return result, nil
}

// generateDockerfile creates a Dockerfile for the Cloud Function.
func (m *CloudFunctionMapper) generateDockerfile(runtime, entryPoint, functionName string) string {
	switch {
	case strings.HasPrefix(runtime, "nodejs"):
		return m.generateNodeJSDockerfile(runtime, entryPoint)
	case strings.HasPrefix(runtime, "python"):
		return m.generatePythonDockerfile(runtime, entryPoint)
	case strings.HasPrefix(runtime, "go"):
		return m.generateGoDockerfile(runtime, entryPoint)
	default:
		return m.generateGenericDockerfile(runtime, entryPoint)
	}
}

func (m *CloudFunctionMapper) generateNodeJSDockerfile(runtime, entryPoint string) string {
	version := "18"
	if strings.Contains(runtime, "20") {
		version = "20"
	} else if strings.Contains(runtime, "16") {
		version = "16"
	}

	return fmt.Sprintf(`FROM node:%s-alpine

WORKDIR /app

COPY package*.json ./
RUN npm ci --production

COPY . .

ENV PORT=8080
ENV FUNCTION_TARGET=%s

EXPOSE 8080

CMD ["npm", "start"]
`, version, entryPoint)
}

func (m *CloudFunctionMapper) generatePythonDockerfile(runtime, entryPoint string) string {
	version := "3.11"
	if strings.Contains(runtime, "312") {
		version = "3.12"
	} else if strings.Contains(runtime, "310") {
		version = "3.10"
	} else if strings.Contains(runtime, "39") {
		version = "3.9"
	}

	return fmt.Sprintf(`FROM python:%s-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
RUN pip install --no-cache-dir functions-framework

COPY . .

ENV PORT=8080
ENV FUNCTION_TARGET=%s

EXPOSE 8080

CMD ["functions-framework", "--target=%s", "--port=8080"]
`, version, entryPoint, entryPoint)
}

func (m *CloudFunctionMapper) generateGoDockerfile(runtime, entryPoint string) string {
	version := "1.21"
	if strings.Contains(runtime, "122") {
		version = "1.22"
	} else if strings.Contains(runtime, "120") {
		version = "1.20"
	}

	return fmt.Sprintf(`FROM golang:%s-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o handler .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/handler .

ENV PORT=8080

EXPOSE 8080

CMD ["./handler"]
`, version)
}

func (m *CloudFunctionMapper) generateGenericDockerfile(runtime, entryPoint string) string {
	return fmt.Sprintf(`FROM alpine:latest

WORKDIR /app

# TODO: Customize for runtime: %s

COPY . .

ENV PORT=8080
ENV FUNCTION_TARGET=%s

EXPOSE 8080

CMD ["echo", "Configure this Dockerfile for your runtime"]
`, runtime, entryPoint)
}

// generateHandler creates a handler template.
func (m *CloudFunctionMapper) generateHandler(runtime, entryPoint, functionName string) (string, string) {
	switch {
	case strings.HasPrefix(runtime, "nodejs"):
		return fmt.Sprintf("functions/%s/index.js", functionName), m.generateNodeJSHandler(entryPoint)
	case strings.HasPrefix(runtime, "python"):
		return fmt.Sprintf("functions/%s/main.py", functionName), m.generatePythonHandler(entryPoint)
	case strings.HasPrefix(runtime, "go"):
		return fmt.Sprintf("functions/%s/main.go", functionName), m.generateGoHandler(entryPoint)
	default:
		return fmt.Sprintf("functions/%s/handler.txt", functionName), "// TODO: Implement handler"
	}
}

func (m *CloudFunctionMapper) generateNodeJSHandler(entryPoint string) string {
	return fmt.Sprintf(`const functions = require('@google-cloud/functions-framework');

functions.http('%s', (req, res) => {
    // TODO: Implement your function logic
    res.send('Hello from Cloud Function!');
});

// For local testing
if (require.main === module) {
    const port = process.env.PORT || 8080;
    require('http').createServer((req, res) => {
        res.writeHead(200, {'Content-Type': 'text/plain'});
        res.end('Hello from Cloud Function!');
    }).listen(port, () => {
        console.log('Server running on port ' + port);
    });
}
`, entryPoint)
}

func (m *CloudFunctionMapper) generatePythonHandler(entryPoint string) string {
	return fmt.Sprintf(`import functions_framework

@functions_framework.http
def %s(request):
    """HTTP Cloud Function."""
    # TODO: Implement your function logic
    return 'Hello from Cloud Function!'

if __name__ == '__main__':
    import os
    from flask import Flask
    app = Flask(__name__)

    @app.route('/', methods=['GET', 'POST'])
    def handler():
        return %s(None)

    port = int(os.environ.get('PORT', 8080))
    app.run(host='0.0.0.0', port=port)
`, entryPoint, entryPoint)
}

func (m *CloudFunctionMapper) generateGoHandler(entryPoint string) string {
	return `package main

import (
    "fmt"
    "net/http"
    "os"
)

func handler(w http.ResponseWriter, r *http.Request) {
    // TODO: Implement your function logic
    fmt.Fprint(w, "Hello from Cloud Function!")
}

func main() {
    http.HandleFunc("/", handler)

    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }

    fmt.Printf("Server running on port %s\n", port)
    http.ListenAndServe(":"+port, nil)
}
`
}

// handleEventTrigger processes event triggers.
func (m *CloudFunctionMapper) handleEventTrigger(trigger interface{}, result *mapper.MappingResult) {
	if triggerMap, ok := trigger.(map[string]interface{}); ok {
		eventType, _ := triggerMap["event_type"].(string)
		resource, _ := triggerMap["resource"].(string)

		result.AddWarning(fmt.Sprintf("Event trigger: %s on %s", eventType, resource))
		result.AddManualStep("Configure event-driven invocation (consider using message queues)")
	}
}

// parseMemory parses GCP memory format to Docker format.
func (m *CloudFunctionMapper) parseMemory(memory string) string {
	if memory == "" {
		return "256Mi"
	}
	// GCP format: "256M", "1G", etc.
	return strings.ReplaceAll(memory, "B", "")
}

// cpuFromMemory estimates CPU from memory allocation.
func (m *CloudFunctionMapper) cpuFromMemory(memory string) string {
	switch {
	case strings.Contains(memory, "128"):
		return "0.083"
	case strings.Contains(memory, "256"):
		return "0.167"
	case strings.Contains(memory, "512"):
		return "0.333"
	case strings.Contains(memory, "1G") || strings.Contains(memory, "1024"):
		return "0.583"
	case strings.Contains(memory, "2G") || strings.Contains(memory, "2048"):
		return "1"
	default:
		return "0.5"
	}
}

func (m *CloudFunctionMapper) sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	validName := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			validName += string(ch)
		}
	}
	validName = strings.TrimLeft(validName, "-0123456789")
	if validName == "" {
		validName = "function"
	}
	return validName
}
