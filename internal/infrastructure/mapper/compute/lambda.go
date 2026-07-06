// Package compute provides mappers for AWS compute services.
package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/computeruntime"
)

// LambdaMapper converts AWS Lambda functions to OpenFaaS or Docker containers.
type LambdaMapper struct {
	*mapper.BaseMapper
}

// NewLambdaMapper creates a new Lambda to OpenFaaS/Docker mapper.
func NewLambdaMapper() *LambdaMapper {
	return &LambdaMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeLambdaFunction, nil),
	}
}

// Map converts a Lambda function to an OpenFaaS function or Docker container.
func (m *LambdaMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	functionName := res.GetConfigString("function_name")
	if functionName == "" {
		functionName = res.Name
	}

	result := mapper.NewMappingResult(m.sanitizeFunctionName(functionName))

	runtime := res.GetConfigString("runtime")
	handler := res.GetConfigString("handler")
	timeout := res.GetConfigInt("timeout")
	memorySize := res.GetConfigInt("memory_size")
	codeLocation := res.GetConfigString("code_location")

	// Create OpenFaaS service
	m.configureOpenFaaSService(result.DockerService, res, functionName, runtime, handler, timeout, memorySize)

	// Generate Dockerfile for the runtime
	dockerfilePath := fmt.Sprintf("functions/%s/Dockerfile", functionName)
	dockerfileContent := m.generateDockerfileContent(runtime, handler, functionName)
	result.AddConfig(dockerfilePath, []byte(dockerfileContent))

	if codeLocation != "" {
		result.AddConfig(fmt.Sprintf("functions/%s/source.url", functionName), []byte(codeLocation+"\n"))
	} else {
		// Generate a fallback function handler template when provider source location is unavailable.
		handlerPath, handlerContent := m.generateHandlerTemplate(runtime, handler, functionName)
		result.AddConfig(handlerPath, []byte(handlerContent))
	}
	result.AddConfig("config/lambda/app-change.env", []byte(m.generateAppChangeConfig(functionName)))
	result.AddConfig("config/lambda/events.yaml", []byte(m.generateEventsConfig(functionName, res)))
	result.AddConfig("config/lambda/permissions.json", []byte(m.generatePermissionsConfig(functionName, res)))
	result.AddConfig("config/lambda/layers.yaml", []byte(m.generateLayersConfig(functionName, res)))
	result.AddConfig("config/lambda/sample-event.json", []byte("{\"source\":\"homeport\",\"detail\":{}}\n"))

	// Handle environment variables
	if envVarsRaw, ok := res.Config["environment"]; ok {
		if envVars, ok := envVarsRaw.(map[string]interface{}); ok {
			if variables, ok := envVars["variables"].(map[string]interface{}); ok {
				for key, value := range variables {
					result.DockerService.Environment[key] = fmt.Sprintf("%v", value)
				}
			}
		}
	}

	// Handle VPC configuration
	if vpcConfigRaw, ok := res.Config["vpc_config"]; ok {
		if vpcConfig, ok := vpcConfigRaw.(map[string]interface{}); ok {
			m.handleVPCConfig(vpcConfig, result.DockerService, result)
		}
	}

	// Handle layers
	if layersRaw, ok := res.Config["layers"]; ok {
		if layers, ok := layersRaw.([]interface{}); ok && len(layers) > 0 {
			result.AddWarning(fmt.Sprintf("Lambda layers detected (%d). Generated layer manifest is included for source packaging.", len(layers)))
		}
	}

	// Handle dead letter queue
	if dlqConfigRaw, ok := res.Config["dead_letter_config"]; ok {
		if dlqConfig, ok := dlqConfigRaw.(map[string]interface{}); ok {
			if targetArn, ok := dlqConfig["target_arn"].(string); ok {
				result.AddWarning(fmt.Sprintf("Dead letter queue configured: %s. Set up equivalent error handling.", targetArn))
			}
		}
	}

	// Generate deployment script
	sanitizedName := m.sanitizeFunctionName(functionName)
	deployScriptName := fmt.Sprintf("deploy_%s.sh", sanitizedName)
	deployScriptContent := m.generateDeploymentScriptContent(sanitizedName, runtime)
	result.AddScript(deployScriptName, []byte(deployScriptContent))
	result.AddScript("export_lambda_config.sh", []byte(m.generateExportConfigScript(functionName, res.Region)))
	result.AddScript("package_lambda_source.sh", []byte(m.generatePackageSourceScript(functionName, codeLocation)))
	result.AddScript("validate_lambda_invoke.sh", []byte(m.generateValidateScript(functionName)))
	result.AddScript("backup_lambda_artifacts.sh", []byte(m.generateBackupScript(functionName)))
	result.AddScript("cutover_lambda_adapter.sh", []byte(m.generateCutoverScript(functionName)))
	appUnit := computeruntime.FromDockerService("aws_lambda_function", result.DockerService)
	result.AddAppUnit(appUnit)
	for _, step := range lambdaRunbook(appUnit, deployScriptName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

// configureOpenFaaSService configures the OpenFaaS service definition.
func (m *LambdaMapper) configureOpenFaaSService(service *mapper.DockerService, res *resource.AWSResource, functionName, runtime, handler string, timeout, memorySize int) {
	if timeout == 0 {
		timeout = 30
	}
	if memorySize == 0 {
		memorySize = 128
	}

	// Convert memory to Docker format
	memoryLimit := fmt.Sprintf("%dM", memorySize)

	// Estimate CPU from memory (rough approximation)
	cpus := "0.5"
	if memorySize >= 512 {
		cpus = "1.0"
	}
	if memorySize >= 1024 {
		cpus = "2.0"
	}

	sanitizedName := m.sanitizeFunctionName(functionName)
	service.Image = fmt.Sprintf("%s:latest", sanitizedName)
	service.Build = &mapper.DockerBuild{
		Context:    fmt.Sprintf("./functions/%s", functionName),
		Dockerfile: "Dockerfile",
	}
	service.Environment = map[string]string{
		"FUNCTION_NAME": functionName,
		"HANDLER":       handler,
		"TIMEOUT":       fmt.Sprintf("%d", timeout),
	}
	service.Labels = map[string]string{
		"homeport.source":        "aws_lambda_function",
		"homeport.function_name": functionName,
		"homeport.runtime":       runtime,
		"openfaas.scale.min":     "1",
		"openfaas.scale.max":     "5",
	}
	service.Restart = "unless-stopped"

	// Set up deployment configuration with resource limits
	service.Deploy = &mapper.DeployConfig{
		Replicas: 2,
		Resources: &mapper.Resources{
			Limits: &mapper.ResourceLimits{
				CPUs:   cpus,
				Memory: memoryLimit,
			},
		},
	}

	// Add health check for HTTP functions
	service.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "wget --quiet --tries=1 --spider http://localhost:8080/_/health || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}
}

func (m *LambdaMapper) generateAppChangeConfig(functionName string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=adapter
SOURCE_FUNCTION=%s
TARGET_FUNCTION_URL=http://%s:8080
AWS_ENDPOINT_URL_LAMBDA=http://homeport:8080/api/v1/compat/aws/lambda
HOMEPORT_COMPAT_BACKEND=openfaas
`, functionName, m.sanitizeFunctionName(functionName))
}

func (m *LambdaMapper) generateEventsConfig(functionName string, res *resource.AWSResource) string {
	return fmt.Sprintf(`function: %s
target_url: http://%s:8080
event_sources:
  - type: lambda-invoke
    adapter: homeport-lambda
`, functionName, m.sanitizeFunctionName(functionName))
}

func (m *LambdaMapper) generatePermissionsConfig(functionName string, res *resource.AWSResource) string {
	role := res.GetConfigString("role")
	if role == "" {
		role = "unknown"
	}
	return fmt.Sprintf(`{
  "function": %q,
  "source_role": %q,
  "target_policy": "least-privilege runtime environment generated from Lambda role metadata"
}
`, functionName, role)
}

func (m *LambdaMapper) generateLayersConfig(functionName string, res *resource.AWSResource) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("function: %s\nlayers:\n", functionName))
	switch layers := res.Config["layers"].(type) {
	case []interface{}:
		for _, layer := range layers {
			b.WriteString(fmt.Sprintf("  - %v\n", layer))
		}
	case []map[string]interface{}:
		for _, layer := range layers {
			b.WriteString(fmt.Sprintf("  - arn: %v\n    code_size: %v\n", layer["arn"], layer["code_size"]))
		}
	default:
		b.WriteString("  []\n")
	}
	return b.String()
}

// generateDockerfileContent creates Dockerfile content for the Lambda function.
func (m *LambdaMapper) generateDockerfileContent(runtime, handler, functionName string) string {
	switch {
	case strings.HasPrefix(runtime, "nodejs"):
		return m.generateNodeJSDockerfile(runtime, handler, functionName)
	case strings.HasPrefix(runtime, "python"):
		return m.generatePythonDockerfile(runtime, handler, functionName)
	case strings.HasPrefix(runtime, "go"):
		return m.generateGoDockerfile(runtime, handler, functionName)
	case strings.HasPrefix(runtime, "java"):
		return m.generateJavaDockerfile(runtime, handler, functionName)
	case strings.HasPrefix(runtime, "dotnet"):
		return m.generateDotNetDockerfile(runtime, handler, functionName)
	case strings.HasPrefix(runtime, "ruby"):
		return m.generateRubyDockerfile(runtime, handler, functionName)
	default:
		return m.generateGenericDockerfile(runtime, handler, functionName)
	}
}

// generateNodeJSDockerfile creates a Node.js Dockerfile.
func (m *LambdaMapper) generateNodeJSDockerfile(runtime, handler, functionName string) string {
	version := "18"
	if strings.Contains(runtime, "20") {
		version = "20"
	} else if strings.Contains(runtime, "18") {
		version = "18"
	} else if strings.Contains(runtime, "16") {
		version = "16"
	}

	return fmt.Sprintf(`FROM node:%s-alpine

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci --production

# Copy function code
COPY . .

# Expose port
EXPOSE 8080

# Set handler
ENV HANDLER=%s

# Run function
CMD ["node", "index.js"]
`, version, handler)
}

// generatePythonDockerfile creates a Python Dockerfile.
func (m *LambdaMapper) generatePythonDockerfile(runtime, handler, functionName string) string {
	version := "3.11"
	if strings.Contains(runtime, "3.12") {
		version = "3.12"
	} else if strings.Contains(runtime, "3.11") {
		version = "3.11"
	} else if strings.Contains(runtime, "3.10") {
		version = "3.10"
	} else if strings.Contains(runtime, "3.9") {
		version = "3.9"
	}

	return fmt.Sprintf(`FROM python:%s-slim

WORKDIR /app

# Copy requirements
COPY requirements.txt .

# Install dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Copy function code
COPY . .

# Expose port
EXPOSE 8080

# Set handler
ENV HANDLER=%s

# Run function
CMD ["python", "handler.py"]
`, version, handler)
}

// generateGoDockerfile creates a Go Dockerfile.
func (m *LambdaMapper) generateGoDockerfile(runtime, handler, functionName string) string {
	version := "1.21"
	if strings.Contains(runtime, "1.21") {
		version = "1.21"
	} else if strings.Contains(runtime, "1.20") {
		version = "1.20"
	}

	return fmt.Sprintf(`FROM golang:%s-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o handler .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/handler .

# Expose port
EXPOSE 8080

# Run
CMD ["./handler"]
`, version)
}

// generateJavaDockerfile creates a Java Dockerfile.
func (m *LambdaMapper) generateJavaDockerfile(runtime, handler, functionName string) string {
	version := "17"
	if strings.Contains(runtime, "21") {
		version = "21"
	} else if strings.Contains(runtime, "17") {
		version = "17"
	} else if strings.Contains(runtime, "11") {
		version = "11"
	}

	return fmt.Sprintf(`FROM maven:3.9-amazoncorretto-%s AS builder

WORKDIR /app

# Copy pom.xml
COPY pom.xml .

# Download dependencies
RUN mvn dependency:go-offline

# Copy source
COPY src ./src

# Build
RUN mvn package -DskipTests

# Final stage
FROM amazoncorretto:%s-alpine

WORKDIR /app

COPY --from=builder /app/target/*.jar app.jar

# Expose port
EXPOSE 8080

# Set handler
ENV HANDLER=%s

# Run
CMD ["java", "-jar", "app.jar"]
`, version, version, handler)
}

// generateDotNetDockerfile creates a .NET Dockerfile.
func (m *LambdaMapper) generateDotNetDockerfile(runtime, handler, functionName string) string {
	version := "8.0"
	if strings.Contains(runtime, "8") {
		version = "8.0"
	} else if strings.Contains(runtime, "6") {
		version = "6.0"
	}

	return fmt.Sprintf(`FROM mcr.microsoft.com/dotnet/sdk:%s AS builder

WORKDIR /app

# Copy csproj and restore
COPY *.csproj .
RUN dotnet restore

# Copy source and build
COPY . .
RUN dotnet publish -c Release -o out

# Final stage
FROM mcr.microsoft.com/dotnet/aspnet:%s

WORKDIR /app
COPY --from=builder /app/out .

# Expose port
EXPOSE 8080

# Set handler
ENV HANDLER=%s

# Run
ENTRYPOINT ["dotnet", "Function.dll"]
`, version, version, handler)
}

// generateRubyDockerfile creates a Ruby Dockerfile.
func (m *LambdaMapper) generateRubyDockerfile(runtime, handler, functionName string) string {
	version := "3.2"
	if strings.Contains(runtime, "3.2") {
		version = "3.2"
	} else if strings.Contains(runtime, "3.1") {
		version = "3.1"
	}

	return fmt.Sprintf(`FROM ruby:%s-alpine

WORKDIR /app

# Copy Gemfile
COPY Gemfile* ./

# Install dependencies
RUN bundle install

# Copy function code
COPY . .

# Expose port
EXPOSE 8080

# Set handler
ENV HANDLER=%s

# Run function
CMD ["ruby", "handler.rb"]
`, version, handler)
}

// generateGenericDockerfile creates a generic Dockerfile.
func (m *LambdaMapper) generateGenericDockerfile(runtime, handler, functionName string) string {
	return fmt.Sprintf(`FROM alpine:latest

WORKDIR /app

# TODO: Customize this Dockerfile for runtime: %s

# Copy function code
COPY . .

# Expose port
EXPOSE 8080

# Set handler
ENV HANDLER=%s

# Run function
CMD ["/bin/sh", "-c", "echo 'Please customize this Dockerfile for your runtime'"]
`, runtime, handler)
}

// generateHandlerTemplate creates a handler template file path and content.
func (m *LambdaMapper) generateHandlerTemplate(runtime, handler, functionName string) (string, string) {
	var content string
	var filename string

	switch {
	case strings.HasPrefix(runtime, "nodejs"):
		filename = "index.js"
		content = m.generateNodeJSHandler(handler)
	case strings.HasPrefix(runtime, "python"):
		filename = "handler.py"
		content = m.generatePythonHandler(handler)
	case strings.HasPrefix(runtime, "go"):
		filename = "main.go"
		content = m.generateGoHandler(handler)
	default:
		filename = "handler.txt"
		content = "// TODO: Implement handler for " + runtime
	}

	path := fmt.Sprintf("functions/%s/%s", functionName, filename)
	return path, content
}

// generateNodeJSHandler creates a Node.js handler template.
func (m *LambdaMapper) generateNodeJSHandler(handler string) string {
	return `const http = require('http');

// Lambda handler function
async function handler(event, context) {
    // TODO: Implement your Lambda function logic here

    return {
        statusCode: 200,
        body: JSON.stringify({
            message: 'Function executed successfully',
            input: event
        })
    };
}

// HTTP server for OpenFaaS/Docker
const server = http.createServer(async (req, res) => {
    let body = '';

    req.on('data', chunk => {
        body += chunk.toString();
    });

    req.on('end', async () => {
        try {
            const event = body ? JSON.parse(body) : {};
            const result = await handler(event, {});

            res.writeHead(result.statusCode || 200, {
                'Content-Type': 'application/json'
            });
            res.end(result.body);
        } catch (error) {
            res.writeHead(500, {'Content-Type': 'application/json'});
            res.end(JSON.stringify({error: error.message}));
        }
    });
});

const port = process.env.PORT || 8080;
server.listen(port, () => {
    console.log('Server running on port ' + port);
});
`
}

// generatePythonHandler creates a Python handler template.
func (m *LambdaMapper) generatePythonHandler(handler string) string {
	parts := strings.Split(handler, ".")
	funcName := "handler"
	if len(parts) > 1 {
		funcName = parts[len(parts)-1]
	}

	return fmt.Sprintf(`import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer

# Lambda handler function
def %s(event, context):
    """
    TODO: Implement your Lambda function logic here
    """
    return {
        'statusCode': 200,
        'body': json.dumps({
            'message': 'Function executed successfully',
            'input': event
        })
    }

# HTTP server for OpenFaaS/Docker
class RequestHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        content_length = int(self.headers.get('Content-Length', 0))
        body = self.rfile.read(content_length)

        try:
            event = json.loads(body) if body else {}
            result = %s(event, {})

            self.send_response(result.get('statusCode', 200))
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(result['body'].encode())
        except Exception as e:
            self.send_response(500)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({'error': str(e)}).encode())

    def do_GET(self):
        self.do_POST()

if __name__ == '__main__':
    port = int(os.environ.get('PORT', 8080))
    server = HTTPServer(('', port), RequestHandler)
    print(f'Server running on port {port}')
    server.serve_forever()
`, funcName, funcName)
}

// generateGoHandler creates a Go handler template.
func (m *LambdaMapper) generateGoHandler(handler string) string {
	return `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Event represents the input event
type Event struct {
	Body map[string]interface{} ` + "`json:\"body\"`" + `
}

// Response represents the function response
type Response struct {
	StatusCode int               ` + "`json:\"statusCode\"`" + `
	Body       string            ` + "`json:\"body\"`" + `
	Headers    map[string]string ` + "`json:\"headers,omitempty\"`" + `
}

// Handler is the Lambda function handler
func Handler(event Event) (Response, error) {
	// TODO: Implement your Lambda function logic here

	responseBody := map[string]interface{}{
		"message": "Function executed successfully",
		"input":   event,
	}

	body, _ := json.Marshal(responseBody)

	return Response{
		StatusCode: 200,
		Body:       string(body),
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
	}, nil
}

// HTTP server for OpenFaaS/Docker
func httpHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var event Event
	if len(body) > 0 {
		json.Unmarshal(body, &event)
	}

	response, err := Handler(event)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	w.Write([]byte(response.Body))
}

func main() {
	http.HandleFunc("/", httpHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server running on port %s\n", port)
	http.ListenAndServe(":"+port, nil)
}
`
}

// handleVPCConfig processes VPC configuration.
func (m *LambdaMapper) handleVPCConfig(vpcConfig map[string]interface{}, service *mapper.DockerService, result *mapper.MappingResult) {
	if subnetIDs, ok := vpcConfig["subnet_ids"].([]interface{}); ok && len(subnetIDs) > 0 {
		result.AddWarning(fmt.Sprintf("VPC configuration detected with %d subnets. Configure Docker networks accordingly.", len(subnetIDs)))
	}

	if sgIDs, ok := vpcConfig["security_group_ids"].([]interface{}); ok && len(sgIDs) > 0 {
		result.AddWarning(fmt.Sprintf("VPC security groups detected (%d). Configure firewall rules for your self-hosted environment.", len(sgIDs)))
	}

	// Add function to custom network
	service.Networks = []string{"homeport"}
}

// generateDeploymentScriptContent creates deployment script content.
func (m *LambdaMapper) generateDeploymentScriptContent(functionName, runtime string) string {
	return fmt.Sprintf(`#!/bin/bash
# Deployment script for Lambda function: %s

set -e

FUNCTION_NAME="%s"
RUNTIME="%s"

echo "Deploying function: $FUNCTION_NAME"

# Build Docker image
echo "Building Docker image..."
docker build -t $FUNCTION_NAME:latest ./functions/$FUNCTION_NAME

# Test the function locally
echo "Testing function locally..."
docker run --rm -p 8080:8080 $FUNCTION_NAME:latest &
CONTAINER_PID=$!

sleep 5

# Test HTTP endpoint
curl -X POST http://localhost:8080 \
  -H "Content-Type: application/json" \
  -d '{"test": "data"}' || true

# Stop test container
kill $CONTAINER_PID || true

echo "Deployment complete!"
echo "Start the function: docker-compose up -d $FUNCTION_NAME"
`, functionName, functionName, runtime)
}

func (m *LambdaMapper) generateExportConfigScript(functionName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION="${AWS_REGION:-%s}"
FUNCTION_NAME="${LAMBDA_FUNCTION:-%s}"
OUTPUT_DIR="${LAMBDA_EXPORT_DIR:-lambda-export}"
mkdir -p "$OUTPUT_DIR"
aws lambda get-function --region "$AWS_REGION" --function-name "$FUNCTION_NAME" > "$OUTPUT_DIR/function.json"
aws lambda get-policy --region "$AWS_REGION" --function-name "$FUNCTION_NAME" > "$OUTPUT_DIR/policy.json" 2>/dev/null || true
aws lambda list-event-source-mappings --region "$AWS_REGION" --function-name "$FUNCTION_NAME" > "$OUTPUT_DIR/event-source-mappings.json" 2>/dev/null || true
echo "Exported Lambda config for $FUNCTION_NAME into $OUTPUT_DIR"
`, region, functionName)
}

func (m *LambdaMapper) generatePackageSourceScript(functionName, codeLocation string) string {
	if codeLocation == "" {
		codeLocation = fmt.Sprintf("functions/%s/source.zip", functionName)
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
FUNCTION_NAME="${LAMBDA_FUNCTION:-%s}"
SOURCE_URL="${LAMBDA_CODE_LOCATION:-%s}"
FUNCTION_DIR="functions/$FUNCTION_NAME"
mkdir -p "$FUNCTION_DIR"
case "$SOURCE_URL" in
  http://*|https://*)
    curl -fsSL "$SOURCE_URL" -o "$FUNCTION_DIR/source.zip"
    ;;
  *)
    test -s "$SOURCE_URL"
    cp "$SOURCE_URL" "$FUNCTION_DIR/source.zip"
    ;;
esac
unzip -oq "$FUNCTION_DIR/source.zip" -d "$FUNCTION_DIR"
test -s "$FUNCTION_DIR/Dockerfile"
echo "Packaged Lambda source for $FUNCTION_NAME into $FUNCTION_DIR"
`, functionName, codeLocation)
}

func (m *LambdaMapper) generateValidateScript(functionName string) string {
	sanitizedName := m.sanitizeFunctionName(functionName)
	return fmt.Sprintf(`#!/bin/sh
set -eu
FUNCTION_NAME="${LAMBDA_FUNCTION:-%s}"
URL="${FUNCTION_URL:-http://localhost:8080}"
test -s config/lambda/sample-event.json
curl -fsS -X POST "$URL" -H "Content-Type: application/json" --data-binary @config/lambda/sample-event.json >/tmp/homeport-lambda-response.json
test -s /tmp/homeport-lambda-response.json
echo "Validated Lambda invoke path for $FUNCTION_NAME (%s)"
`, functionName, sanitizedName)
}

func (m *LambdaMapper) generateBackupScript(functionName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-lambda-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/lambda functions/%s export_lambda_config.sh package_lambda_source.sh validate_lambda_invoke.sh
echo "$archive"
`, functionName, functionName)
}

func (m *LambdaMapper) generateCutoverScript(functionName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
test -s config/lambda/app-change.env
. config/lambda/app-change.env
test "$SOURCE_FUNCTION" = %q
test "$APP_CHANGE_MODE" = "adapter"
echo "Use AWS_ENDPOINT_URL_LAMBDA=$AWS_ENDPOINT_URL_LAMBDA for Lambda SDK clients backed by $TARGET_FUNCTION_URL."
`, functionName)
}

func lambdaRunbook(unit mapper.AppUnit, deployScript string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                    "serverless-function",
		"app":                     unit.Name,
		"source":                  unit.Source,
		"image":                   unit.Image,
		"source_path":             unit.SourcePath,
		"HOMEPORT_FUNCTION_URL":   "http://" + unit.Name + ":8080",
		"AWS_ENDPOINT_URL_LAMBDA": "http://homeport:8080/api/v1/compat/aws/lambda",
		"HOMEPORT_COMPAT_BACKEND": "openfaas",
	}
	return []domainrunbook.Step{
		lambdaStep("export-lambda-config", "Export Lambda config", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_lambda_config.sh"}, "Lambda code location, policy, and event source mappings are exported", metadata),
		lambdaStep("package-lambda-source", "Package Lambda source", "Build", domainrunbook.StepTypeCommand, []string{"sh", "package_lambda_source.sh"}, "Lambda source archive is unpacked into the function build context", metadata),
		lambdaStep("build-function-image", "Build function image", "Build", domainrunbook.StepTypeCommand, []string{"sh", deployScript}, "function container image builds and starts", metadata),
		lambdaStep("validate-function-invoke", "Validate function invocation", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_lambda_invoke.sh"}, "function returns a response for the generated sample event", metadata),
		lambdaStep("backup-lambda-artifacts", "Backup Lambda artifacts", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_lambda_artifacts.sh"}, "function source, config, and scripts are archived", metadata),
		lambdaStep("cutover-lambda-adapter", "Cut over Lambda clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_lambda_adapter.sh"}, "Lambda SDK clients use the HomePort compatibility endpoint", metadata),
		lambdaStep("rollback-function-source", "Keep Lambda source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Lambda remains authoritative until function validation passes", metadata),
	}
}

func lambdaStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         metadata,
	}
}

// sanitizeFunctionName sanitizes function name for use as Docker service name.
func (m *LambdaMapper) sanitizeFunctionName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

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
