package datamigration

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LambdaToDockerExecutor migrates Lambda functions to Docker containers.
type LambdaToDockerExecutor struct{}

// NewLambdaToDockerExecutor creates a new Lambda to Docker executor.
func NewLambdaToDockerExecutor() *LambdaToDockerExecutor {
	return &LambdaToDockerExecutor{}
}

// Type returns the migration type.
func (e *LambdaToDockerExecutor) Type() string {
	return "lambda_to_docker"
}

// GetPhases returns the migration phases.
func (e *LambdaToDockerExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching function configuration",
		"Downloading function code",
		"Extracting layers",
		"Generating Dockerfile",
		"Building container image",
	}
}

// Validate validates the migration configuration.
func (e *LambdaToDockerExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		// Check for function_arn or function_name (at least one is required)
		functionARN, hasARN := config.Source["function_arn"].(string)
		functionName, hasName := config.Source["function_name"].(string)
		if (!hasARN || functionARN == "") && (!hasName || functionName == "") {
			result.Valid = false
			result.Errors = append(result.Errors, "source.function_arn or source.function_name is required")
		}

		// Check for access_key_id (required)
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}

		// Check for secret_access_key (required)
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}

		// Check for region (warn if missing)
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default us-east-1")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["image_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.image_name is required")
		}
	}

	return result, nil
}

// lambdaFunctionConfig represents the Lambda function configuration from AWS CLI.
type lambdaFunctionConfig struct {
	Configuration struct {
		FunctionName string `json:"FunctionName"`
		FunctionARN  string `json:"FunctionArn"`
		Runtime      string `json:"Runtime"`
		Handler      string `json:"Handler"`
		MemorySize   int    `json:"MemorySize"`
		Timeout      int    `json:"Timeout"`
		Environment  struct {
			Variables map[string]string `json:"Variables"`
		} `json:"Environment"`
		Layers []struct {
			Arn      string `json:"Arn"`
			CodeSize int64  `json:"CodeSize"`
		} `json:"Layers"`
	} `json:"Configuration"`
	Code struct {
		Location string `json:"Location"`
	} `json:"Code"`
}

// Execute performs the migration.
func (e *LambdaToDockerExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	functionARN, _ := config.Source["function_arn"].(string)
	functionName, _ := config.Source["function_name"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	// Extract destination configuration
	imageName := config.Destination["image_name"].(string)
	imageTag, _ := config.Destination["image_tag"].(string)
	if imageTag == "" {
		imageTag = "latest"
	}
	shouldBuild, _ := config.Destination["build"].(bool)

	// Extract options
	includeLayers, _ := config.Source["include_layers"].(bool)

	// Determine function identifier (prefer ARN, fallback to name)
	functionIdentifier := functionARN
	if functionIdentifier == "" {
		functionIdentifier = functionName
	}

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 5, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Test AWS credentials by checking caller identity
	testCmd := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity", "--region", region)
	testCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)
	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("AWS credentials validation failed: %s", string(output)))
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}
	EmitLog(m, "info", "AWS credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching function configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching Lambda function configuration: %s", functionIdentifier))
	EmitProgress(m, 15, "Getting function config")

	getFunctionCmd := exec.CommandContext(ctx, "aws", "lambda", "get-function",
		"--function-name", functionIdentifier,
		"--region", region,
		"--output", "json",
	)
	getFunctionCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	output, err := getFunctionCmd.Output()
	if err != nil {
		EmitLog(m, "error", "Failed to get Lambda function configuration")
		return fmt.Errorf("failed to get Lambda function: %w", err)
	}

	var functionConfig lambdaFunctionConfig
	if err := json.Unmarshal(output, &functionConfig); err != nil {
		return fmt.Errorf("failed to parse function configuration: %w", err)
	}

	runtime := functionConfig.Configuration.Runtime
	handler := functionConfig.Configuration.Handler
	codeLocation := functionConfig.Code.Location

	EmitLog(m, "info", fmt.Sprintf("Function: %s, Runtime: %s, Handler: %s",
		functionConfig.Configuration.FunctionName, runtime, handler))
	EmitLog(m, "info", fmt.Sprintf("Memory: %dMB, Timeout: %ds",
		functionConfig.Configuration.MemorySize, functionConfig.Configuration.Timeout))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Downloading function code
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Downloading Lambda function code")
	EmitProgress(m, 30, "Downloading code")

	// Create staging directory
	stagingDir, err := os.MkdirTemp("", "lambda-migration-*")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	codeZipPath := filepath.Join(stagingDir, "function.zip")
	codePath := filepath.Join(stagingDir, "function")

	// Download the code ZIP from the presigned URL
	if err := downloadFile(ctx, codeLocation, codeZipPath); err != nil {
		EmitLog(m, "error", fmt.Sprintf("Failed to download function code: %v", err))
		return fmt.Errorf("failed to download function code: %w", err)
	}
	EmitLog(m, "info", "Function code downloaded successfully")

	// Extract the ZIP file
	if err := os.MkdirAll(codePath, 0755); err != nil {
		return fmt.Errorf("failed to create code directory: %w", err)
	}

	if err := extractZip(codeZipPath, codePath); err != nil {
		EmitLog(m, "error", fmt.Sprintf("Failed to extract function code: %v", err))
		return fmt.Errorf("failed to extract function code: %w", err)
	}
	EmitLog(m, "info", fmt.Sprintf("Function code extracted to %s", codePath))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Extracting layers
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Processing Lambda layers")
	EmitProgress(m, 50, "Extracting layers")

	layersPath := filepath.Join(stagingDir, "layers")
	if err := os.MkdirAll(layersPath, 0755); err != nil {
		return fmt.Errorf("failed to create layers directory: %w", err)
	}

	if includeLayers && len(functionConfig.Configuration.Layers) > 0 {
		EmitLog(m, "info", fmt.Sprintf("Found %d layers to process", len(functionConfig.Configuration.Layers)))
		for i, layer := range functionConfig.Configuration.Layers {
			EmitLog(m, "info", fmt.Sprintf("Downloading layer %d: %s", i+1, layer.Arn))

			// Get layer version URL
			getLayerCmd := exec.CommandContext(ctx, "aws", "lambda", "get-layer-version-by-arn",
				"--arn", layer.Arn,
				"--region", region,
				"--output", "json",
			)
			getLayerCmd.Env = append(os.Environ(),
				"AWS_ACCESS_KEY_ID="+accessKeyID,
				"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
				"AWS_DEFAULT_REGION="+region,
			)

			layerOutput, err := getLayerCmd.Output()
			if err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to get layer %s: %v", layer.Arn, err))
				continue
			}

			var layerInfo struct {
				Content struct {
					Location string `json:"Location"`
				} `json:"Content"`
			}
			if err := json.Unmarshal(layerOutput, &layerInfo); err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to parse layer info: %v", err))
				continue
			}

			layerZipPath := filepath.Join(stagingDir, fmt.Sprintf("layer_%d.zip", i))
			layerExtractPath := filepath.Join(layersPath, fmt.Sprintf("layer_%d", i))

			if err := downloadFile(ctx, layerInfo.Content.Location, layerZipPath); err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to download layer: %v", err))
				continue
			}

			if err := os.MkdirAll(layerExtractPath, 0755); err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to create layer directory: %v", err))
				continue
			}

			if err := extractZip(layerZipPath, layerExtractPath); err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to extract layer: %v", err))
				continue
			}

			EmitLog(m, "info", fmt.Sprintf("Layer %d extracted successfully", i+1))
		}
	} else {
		EmitLog(m, "info", "No layers to process or layer extraction disabled")
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating Dockerfile
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating Dockerfile")
	EmitProgress(m, 70, "Creating Dockerfile")

	dockerfile := generateDockerfile(runtime, handler, functionConfig.Configuration.Environment.Variables, includeLayers, len(functionConfig.Configuration.Layers))
	dockerfilePath := filepath.Join(stagingDir, "Dockerfile")

	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	EmitLog(m, "info", "Dockerfile generated successfully")
	EmitLog(m, "debug", fmt.Sprintf("Dockerfile:\n%s", dockerfile))

	// Copy function code to build context
	buildContextPath := filepath.Join(stagingDir, "build")
	if err := os.MkdirAll(buildContextPath, 0755); err != nil {
		return fmt.Errorf("failed to create build context directory: %w", err)
	}

	// Copy Dockerfile
	if err := copyFile(dockerfilePath, filepath.Join(buildContextPath, "Dockerfile")); err != nil {
		return fmt.Errorf("failed to copy Dockerfile: %w", err)
	}

	// Copy function code
	functionDestPath := filepath.Join(buildContextPath, "function")
	if err := copyDir(codePath, functionDestPath); err != nil {
		return fmt.Errorf("failed to copy function code: %w", err)
	}

	// Copy layers if present
	if includeLayers && len(functionConfig.Configuration.Layers) > 0 {
		layersDestPath := filepath.Join(buildContextPath, "layers")
		if err := copyDir(layersPath, layersDestPath); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to copy layers: %v", err))
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Building container image
	EmitPhase(m, phases[5], 6)

	if shouldBuild {
		EmitLog(m, "info", fmt.Sprintf("Building Docker image: %s:%s", imageName, imageTag))
		EmitProgress(m, 85, "Building image")

		fullImageName := fmt.Sprintf("%s:%s", imageName, imageTag)
		buildCmd := exec.CommandContext(ctx, "docker", "build",
			"-t", fullImageName,
			"-f", filepath.Join(buildContextPath, "Dockerfile"),
			buildContextPath,
		)

		buildOutput, err := buildCmd.CombinedOutput()
		if err != nil {
			EmitLog(m, "error", fmt.Sprintf("Docker build failed: %s", string(buildOutput)))
			return fmt.Errorf("failed to build Docker image: %w", err)
		}
		EmitLog(m, "info", fmt.Sprintf("Docker image %s built successfully", fullImageName))
	} else {
		EmitLog(m, "info", "Skipping Docker build (build=false)")
		EmitLog(m, "info", fmt.Sprintf("Build context ready at: %s", buildContextPath))
		EmitLog(m, "info", fmt.Sprintf("To build manually: docker build -t %s:%s %s", imageName, imageTag, buildContextPath))
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", "Lambda to Docker migration completed successfully")

	return nil
}

// generateDockerfile generates a Dockerfile based on the Lambda runtime.
func generateDockerfile(runtime, handler string, envVars map[string]string, includeLayers bool, layerCount int) string {
	var baseImage string
	var copyCmd string
	var cmdFormat string

	switch {
	case strings.HasPrefix(runtime, "python3.12"):
		baseImage = "public.ecr.aws/lambda/python:3.12"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "python3.11"):
		baseImage = "public.ecr.aws/lambda/python:3.11"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "python3.10"):
		baseImage = "public.ecr.aws/lambda/python:3.10"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "python3.9"):
		baseImage = "public.ecr.aws/lambda/python:3.9"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "python"):
		baseImage = "public.ecr.aws/lambda/python:3.9"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "nodejs20"):
		baseImage = "public.ecr.aws/lambda/nodejs:20"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "nodejs18"):
		baseImage = "public.ecr.aws/lambda/nodejs:18"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "nodejs16"):
		baseImage = "public.ecr.aws/lambda/nodejs:16"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "nodejs"):
		baseImage = "public.ecr.aws/lambda/nodejs:18"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "java21"):
		baseImage = "public.ecr.aws/lambda/java:21"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "java17"):
		baseImage = "public.ecr.aws/lambda/java:17"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "java11"):
		baseImage = "public.ecr.aws/lambda/java:11"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "java"):
		baseImage = "public.ecr.aws/lambda/java:11"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "go"):
		baseImage = "public.ecr.aws/lambda/provided:al2"
		copyCmd = "COPY function/bootstrap ${LAMBDA_RUNTIME_DIR}/"
		cmdFormat = "CMD [\"bootstrap\"]"
	case strings.HasPrefix(runtime, "dotnet8"):
		baseImage = "public.ecr.aws/lambda/dotnet:8"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "dotnet6"):
		baseImage = "public.ecr.aws/lambda/dotnet:6"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "dotnet"):
		baseImage = "public.ecr.aws/lambda/dotnet:6"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case strings.HasPrefix(runtime, "ruby"):
		baseImage = "public.ecr.aws/lambda/ruby:3.2"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	case runtime == "provided" || runtime == "provided.al2" || runtime == "provided.al2023":
		baseImage = "public.ecr.aws/lambda/provided:al2"
		copyCmd = "COPY function/bootstrap ${LAMBDA_RUNTIME_DIR}/"
		cmdFormat = "CMD [\"bootstrap\"]"
	default:
		// Default to provided runtime for custom runtimes
		baseImage = "public.ecr.aws/lambda/provided:al2"
		copyCmd = "COPY function/ ${LAMBDA_TASK_ROOT}/"
		cmdFormat = fmt.Sprintf("CMD [\"%s\"]", handler)
	}

	var dockerfile strings.Builder

	dockerfile.WriteString(fmt.Sprintf("FROM %s\n\n", baseImage))
	dockerfile.WriteString("# Lambda function code migrated from AWS\n\n")

	// Add environment variables
	if len(envVars) > 0 {
		dockerfile.WriteString("# Environment variables from Lambda configuration\n")
		for key, value := range envVars {
			// Escape special characters in the value
			escapedValue := strings.ReplaceAll(value, "\"", "\\\"")
			dockerfile.WriteString(fmt.Sprintf("ENV %s=\"%s\"\n", key, escapedValue))
		}
		dockerfile.WriteString("\n")
	}

	// Copy layers first (they go to /opt)
	if includeLayers && layerCount > 0 {
		dockerfile.WriteString("# Copy Lambda layers\n")
		for i := 0; i < layerCount; i++ {
			dockerfile.WriteString(fmt.Sprintf("COPY layers/layer_%d/ /opt/\n", i))
		}
		dockerfile.WriteString("\n")
	}

	// Copy function code
	dockerfile.WriteString("# Copy function code\n")
	dockerfile.WriteString(copyCmd + "\n\n")

	// Install dependencies if requirements.txt exists (Python)
	if strings.HasPrefix(runtime, "python") {
		dockerfile.WriteString("# Install Python dependencies if requirements.txt exists\n")
		dockerfile.WriteString("RUN if [ -f \"${LAMBDA_TASK_ROOT}/requirements.txt\" ]; then \\\n")
		dockerfile.WriteString("        pip install -r ${LAMBDA_TASK_ROOT}/requirements.txt -t ${LAMBDA_TASK_ROOT}/; \\\n")
		dockerfile.WriteString("    fi\n\n")
	}

	// Install dependencies if package.json exists (Node.js)
	if strings.HasPrefix(runtime, "nodejs") {
		dockerfile.WriteString("# Install Node.js dependencies if package.json exists\n")
		dockerfile.WriteString("RUN if [ -f \"${LAMBDA_TASK_ROOT}/package.json\" ]; then \\\n")
		dockerfile.WriteString("        cd ${LAMBDA_TASK_ROOT} && npm install --production; \\\n")
		dockerfile.WriteString("    fi\n\n")
	}

	// Set the CMD
	dockerfile.WriteString("# Set the handler\n")
	dockerfile.WriteString(cmdFormat + "\n")

	return dockerfile.String()
}

// downloadFile downloads a file from a URL to the specified path.
func downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// extractZip extracts a ZIP file to the specified directory.
func extractZip(zipPath, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(destPath, f.Name)

		// Check for ZipSlip vulnerability
		if !strings.HasPrefix(filepath.Clean(fpath), filepath.Clean(destPath)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open file in zip: %w", err)
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()

		if err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}

	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}
