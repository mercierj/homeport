package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/homeport/homeport/internal/domain/bundle"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
	"github.com/homeport/homeport/internal/infrastructure/secrets/detector"
	"github.com/homeport/homeport/pkg/version"
	"github.com/spf13/cobra"

	infraBundle "github.com/homeport/homeport/internal/infrastructure/bundle"

	_ "github.com/homeport/homeport/internal/infrastructure/parser/aws"   // Register AWS parsers
	_ "github.com/homeport/homeport/internal/infrastructure/parser/azure" // Register Azure parsers
	_ "github.com/homeport/homeport/internal/infrastructure/parser/gcp"   // Register GCP parsers
)

var (
	exportOutput        string
	exportSource        string
	exportConsolidate   bool
	exportDetectSecrets bool
	exportDomain        string
	exportProvider      string
	exportRegion        string
	exportProfile       string
	exportProject       string
)

// exportCmd represents the export command
var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export migration as a portable .hprt bundle",
	Long: `Export analyzed cloud infrastructure as a portable .hprt bundle file.

The export command creates a self-contained migration bundle that includes:
  - Docker Compose configuration
  - Service configurations (nginx, postgres, redis, etc.)
  - Migration scripts
  - Data sync scripts
  - Secret references (NO actual secrets - see security note below)

SECURITY: The bundle NEVER contains secret values. It stores references only.
Secrets are resolved at import time via:
  - Interactive prompts
  - Environment variables (HOMEPORT_SECRET_*)
  - --secrets-file option
  - --pull-secrets-from option (cloud provider)

Examples:
  # Basic export from terraform files
  homeport export --source ./terraform -o migration.hprt

  # Export with stack consolidation (reduces container count)
  homeport export --source ./infrastructure --consolidate -o migration.hprt

  # Export and detect secret references
  homeport export --source ./infra.tf --detect-secrets -o migration.hprt

  # Export with custom domain
  homeport export --source ./terraform --domain example.com -o migration.hprt

  # Export from specific provider with region
  homeport export --source ./terraform --provider aws --region us-east-1 -o migration.hprt`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if exportSource == "" {
			return fmt.Errorf("--source is required: specify path to terraform files, state, or cloud config")
		}

		if exportOutput == "" {
			return fmt.Errorf("--output (-o) is required: specify output .hprt file path")
		}

		// Ensure .hprt extension
		if !strings.HasSuffix(exportOutput, ".hprt") {
			exportOutput = exportOutput + ".hprt"
		}

		if !IsQuiet() {
			ui.Header("Homeport - Bundle Export")
			ui.Info(fmt.Sprintf("Source: %s", exportSource))
			ui.Info(fmt.Sprintf("Output: %s", exportOutput))
			if exportConsolidate {
				ui.Info("Consolidation: enabled")
			}
			if exportDetectSecrets {
				ui.Info("Secret detection: enabled")
			}
			ui.Divider()
		}

		// Validate source path
		if _, err := os.Stat(exportSource); os.IsNotExist(err) {
			return fmt.Errorf("source path does not exist: %s", exportSource)
		}

		// Perform export
		if err := performExport(); err != nil {
			return fmt.Errorf("export failed: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success("Bundle exported successfully")
			ui.Info(fmt.Sprintf("Output: %s", exportOutput))
			ui.Info("Next steps:")
			fmt.Println("  1. Transfer the bundle to your target server")
			fmt.Println("  2. Run: homeport import " + filepath.Base(exportOutput))
			fmt.Println("  3. Provide secrets when prompted")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "output .hprt bundle file path (required)")
	exportCmd.Flags().StringVarP(&exportSource, "source", "s", "", "source path (terraform files, state, or cloud config)")
	exportCmd.Flags().BoolVar(&exportConsolidate, "consolidate", false, "consolidate similar resources into unified stacks")
	exportCmd.Flags().BoolVar(&exportDetectSecrets, "detect-secrets", false, "detect and create secret references")
	exportCmd.Flags().StringVarP(&exportDomain, "domain", "d", "", "domain name for services")
	exportCmd.Flags().StringVar(&exportProvider, "provider", "", "cloud provider (aws, gcp, azure)")
	exportCmd.Flags().StringVarP(&exportRegion, "region", "r", "", "cloud region")
	exportCmd.Flags().StringVarP(&exportProfile, "profile", "p", "", "AWS profile name")
	exportCmd.Flags().StringVar(&exportProject, "project", "", "GCP project ID")

	_ = exportCmd.MarkFlagRequired("output")
	_ = exportCmd.MarkFlagRequired("source")
}

// performExport performs the bundle export
func performExport() error {
	ctx := context.Background()
	totalSteps := 6
	currentStep := 0

	// Step 1: Analyze source infrastructure
	currentStep++
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Analyzing source infrastructure"))
	}

	analysis, err := analyzeForExport(ctx)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	if analysis.Statistics.TotalResources == 0 {
		return fmt.Errorf("no resources found in source")
	}

	if IsVerbose() {
		ui.Info(fmt.Sprintf("Found %d resources", analysis.Statistics.TotalResources))
	}

	// Step 2: Map resources to Docker services
	currentStep++
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Mapping resources to Docker services"))
	}

	mappingResults, err := mapResourcesForExport(ctx, analysis)
	if err != nil {
		return fmt.Errorf("mapping failed: %w", err)
	}

	if IsVerbose() {
		ui.Info(fmt.Sprintf("Mapped %d resources", len(mappingResults)))
	}

	// Step 3: Consolidate stacks (if enabled)
	currentStep++
	var consolidatedResult *stack.ConsolidatedResult
	if exportConsolidate {
		if !IsQuiet() {
			fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Consolidating stacks"))
		}

		cons := consolidator.New()
		opts := consolidator.DefaultOptions()

		consolidatedResult, err = cons.Consolidate(ctx, mappingResults, opts)
		if err != nil {
			return fmt.Errorf("consolidation failed: %w", err)
		}

		if IsVerbose() {
			ui.Info(fmt.Sprintf("Consolidated into %d stacks", consolidatedResult.TotalStacks()))
		}
	} else {
		if !IsQuiet() {
			fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Building stacks (no consolidation)"))
		}
	}

	// Step 4: Generate output files in temp directory
	currentStep++
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Generating migration files"))
	}

	tempDir, err := os.MkdirTemp("", "homeport-export-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	if err := generateExportFiles(tempDir, analysis, mappingResults, consolidatedResult); err != nil {
		return fmt.Errorf("failed to generate files: %w", err)
	}

	// Step 5: Detect secrets (if enabled)
	currentStep++
	var secretRefs []*bundle.SecretReference
	if exportDetectSecrets {
		if !IsQuiet() {
			fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Detecting secrets"))
		}

		secretRefs = detectSecretReferences(analysis, mappingResults)
		if err := writeSecretsManifest(tempDir, secretRefs); err != nil {
			return fmt.Errorf("failed to write secrets manifest: %w", err)
		}

		if IsVerbose() && len(secretRefs) > 0 {
			ui.Info(fmt.Sprintf("Detected %d secret references", len(secretRefs)))
		}
	} else {
		if !IsQuiet() {
			fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Skipping secret detection"))
		}
	}

	// Step 6: Create bundle archive
	currentStep++
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Creating bundle archive"))
	}

	exporter := infraBundle.NewExporter(version.Version)
	opts := infraBundle.ExportOptions{
		OutputPath:     exportOutput,
		SourceProvider: detectProvider(analysis),
		SourceRegion:   exportRegion,
		ResourceCount:  analysis.Statistics.TotalResources,
		TargetType:     "docker-compose",
		Consolidation:  exportConsolidate,
		DetectSecrets:  exportDetectSecrets,
	}

	if err := exporter.Export(tempDir, opts); err != nil {
		return fmt.Errorf("failed to create bundle: %w", err)
	}

	// Validate the created bundle
	if err := exporter.ValidateExport(exportOutput); err != nil {
		return fmt.Errorf("bundle validation failed: %w", err)
	}

	// Display summary
	if !IsQuiet() {
		displayExportSummary(analysis, consolidatedResult, secretRefs)
	}

	return nil
}

// analyzeForExport analyzes source infrastructure for export
func analyzeForExport(ctx context.Context) (*AnalysisResult, error) {
	opts := parser.NewParseOptions()

	if exportRegion != "" {
		opts.WithRegions(exportRegion)
	}

	creds := make(map[string]string)
	if exportProfile != "" {
		creds["profile"] = exportProfile
	}
	if exportProject != "" {
		creds["project"] = exportProject
	}
	if len(creds) > 0 {
		opts.WithCredentials(creds)
	}

	// Auto-detect parser based on source
	p, err := parser.DefaultRegistry().AutoDetect(exportSource)
	if err != nil {
		return nil, fmt.Errorf("failed to detect source type: %w", err)
	}

	if IsVerbose() {
		ui.Info(fmt.Sprintf("Detected source type: %s (%s)", p.SupportedFormats()[0], p.Provider()))
	}

	infra, err := p.Parse(ctx, exportSource, opts)
	if err != nil {
		return nil, fmt.Errorf("parsing failed: %w", err)
	}

	absPath, _ := filepath.Abs(exportSource)
	sourceType := string(p.SupportedFormats()[0])

	return buildAnalysisResult(infra, absPath, sourceType), nil
}

// mapResourcesForExport maps analyzed resources to Docker services
func mapResourcesForExport(ctx context.Context, analysis *AnalysisResult) ([]*mapper.MappingResult, error) {
	var results []*mapper.MappingResult

	for _, resSummary := range analysis.Resources {
		resType := resource.Type(resSummary.Type)

		m, mapErr := mapper.DefaultRegistry.Get(resType)
		if mapErr != nil || m == nil {
			if IsVerbose() {
				ui.Info(fmt.Sprintf("No mapper for %s, skipping", resSummary.Type))
			}
			continue
		}

		res := &resource.AWSResource{
			Type:   resType,
			Name:   resSummary.Name,
			ID:     resSummary.ID,
			Region: resSummary.Region,
			Tags:   resSummary.Tags,
			Config: make(map[string]interface{}),
		}

		result, err := m.Map(ctx, res)
		if err != nil {
			if IsVerbose() {
				ui.Info(fmt.Sprintf("Failed to map %s: %v", resSummary.Name, err))
			}
			continue
		}

		if result != nil {
			result.SourceResourceType = string(resType)
			result.SourceResourceName = resSummary.Name
			results = append(results, result)
		}
	}

	return results, nil
}

// generateExportFiles generates all files needed for the bundle
func generateExportFiles(outputDir string, analysis *AnalysisResult, mappingResults []*mapper.MappingResult, consolidated *stack.ConsolidatedResult) error {
	// Create standard directory structure
	dirs := []string{
		"compose",
		"configs",
		"scripts",
		"migrations",
		"data-sync",
		"secrets",
		"dns",
		"validation",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(outputDir, dir), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Generate docker-compose.yml
	config := &MigrationConfig{
		InputPath:         exportSource,
		OutputPath:        outputDir,
		Domain:            exportDomain,
		IncludeMigration:  true,
		IncludeMonitoring: false,
		Consolidate:       exportConsolidate,
	}

	if err := generateDockerComposeForBundle(config, analysis, mappingResults, consolidated); err != nil {
		return fmt.Errorf("failed to generate docker-compose: %w", err)
	}

	// Generate Traefik config
	if err := generateTraefikConfigForBundle(config); err != nil {
		return fmt.Errorf("failed to generate Traefik config: %w", err)
	}

	// Generate env template (NOT actual secrets)
	if err := generateEnvTemplate(config, analysis); err != nil {
		return fmt.Errorf("failed to generate env template: %w", err)
	}

	// Generate README
	if err := generateBundleReadme(config, analysis); err != nil {
		return fmt.Errorf("failed to generate README: %w", err)
	}

	// Generate scripts
	if err := generateBundleScripts(config); err != nil {
		return fmt.Errorf("failed to generate scripts: %w", err)
	}

	return nil
}

// generateDockerComposeForBundle generates docker-compose.yml in the bundle structure
func generateDockerComposeForBundle(config *MigrationConfig, analysis *AnalysisResult, mappingResults []*mapper.MappingResult, consolidated *stack.ConsolidatedResult) error {
	// Use the existing migration logic but output to compose/ subdirectory
	composePath := filepath.Join(config.OutputPath, "compose")

	// Temporarily modify output path
	originalOutput := config.OutputPath
	config.OutputPath = composePath
	defer func() { config.OutputPath = originalOutput }()

	return generateDockerCompose(config, analysis)
}

// generateTraefikConfigForBundle generates Traefik config in bundle structure
func generateTraefikConfigForBundle(config *MigrationConfig) error {
	configsDir := filepath.Join(config.OutputPath, "configs", "traefik")
	if err := os.MkdirAll(configsDir, 0755); err != nil {
		return err
	}

	email := "admin@example.com"
	if config.Domain != "" {
		email = fmt.Sprintf("admin@%s", config.Domain)
	}

	traefikContent := fmt.Sprintf(`# Traefik Static Configuration
# Generated by Homeport

api:
  dashboard: true
  insecure: true  # Set to false in production

log:
  level: INFO
  format: json

entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"
    http:
      tls:
        certResolver: letsencrypt

providers:
  docker:
    endpoint: "unix:///var/run/docker.sock"
    exposedByDefault: false
    network: homeport
  file:
    directory: "/etc/traefik/dynamic"
    watch: true

certificatesResolvers:
  letsencrypt:
    acme:
      email: %s
      storage: /certs/acme.json
      httpChallenge:
        entryPoint: web

ping:
  entryPoint: "web"
`, email)

	return os.WriteFile(filepath.Join(configsDir, "traefik.yml"), []byte(traefikContent), 0644)
}

// generateEnvTemplate generates .env.template (placeholders only, NO secrets)
func generateEnvTemplate(config *MigrationConfig, analysis *AnalysisResult) error {
	var sb strings.Builder

	sb.WriteString("# Environment Variables Template\n")
	sb.WriteString("# Generated by Homeport\n")
	sb.WriteString("# \n")
	sb.WriteString("# IMPORTANT: This is a TEMPLATE. Copy to .env and fill in values.\n")
	sb.WriteString("# NEVER commit .env with real values to version control.\n")
	sb.WriteString("#\n\n")

	sb.WriteString("# Domain Configuration\n")
	if config.Domain != "" {
		sb.WriteString(fmt.Sprintf("DOMAIN=%s\n", config.Domain))
	} else {
		sb.WriteString("DOMAIN=example.com\n")
	}
	sb.WriteString("\n")

	// Detect database types and add placeholders
	hasPostgres := false
	hasMySQL := false
	hasRedis := false
	hasMongo := false

	for _, res := range analysis.Resources {
		switch res.Type {
		case string(resource.TypeRDSInstance), string(resource.TypeRDSCluster), string(resource.TypeAzurePostgres), string(resource.TypeCloudSQL):
			hasPostgres = true
		case string(resource.TypeAzureMySQL):
			hasMySQL = true
		case string(resource.TypeElastiCache), string(resource.TypeAzureCache), string(resource.TypeMemorystore):
			hasRedis = true
		case string(resource.TypeCosmosDB), string(resource.TypeDynamoDBTable), string(resource.TypeFirestore):
			hasMongo = true
		}
	}

	if hasPostgres {
		sb.WriteString("# PostgreSQL (replace with your values)\n")
		sb.WriteString("POSTGRES_USER=<YOUR_POSTGRES_USER>\n")
		sb.WriteString("POSTGRES_PASSWORD=<YOUR_POSTGRES_PASSWORD>\n")
		sb.WriteString("POSTGRES_DB=<YOUR_DATABASE_NAME>\n\n")
	}

	if hasMySQL {
		sb.WriteString("# MySQL (replace with your values)\n")
		sb.WriteString("MYSQL_USER=<YOUR_MYSQL_USER>\n")
		sb.WriteString("MYSQL_PASSWORD=<YOUR_MYSQL_PASSWORD>\n")
		sb.WriteString("MYSQL_ROOT_PASSWORD=<YOUR_MYSQL_ROOT_PASSWORD>\n")
		sb.WriteString("MYSQL_DATABASE=<YOUR_DATABASE_NAME>\n\n")
	}

	if hasRedis {
		sb.WriteString("# Redis (replace with your values)\n")
		sb.WriteString("REDIS_PASSWORD=<YOUR_REDIS_PASSWORD>\n\n")
	}

	if hasMongo {
		sb.WriteString("# MongoDB (replace with your values)\n")
		sb.WriteString("MONGO_INITDB_ROOT_USERNAME=<YOUR_MONGO_USER>\n")
		sb.WriteString("MONGO_INITDB_ROOT_PASSWORD=<YOUR_MONGO_PASSWORD>\n\n")
	}

	sb.WriteString("# Traefik / SSL\n")
	sb.WriteString("ACME_EMAIL=<YOUR_EMAIL_FOR_LETSENCRYPT>\n\n")

	templatesDir := filepath.Join(config.OutputPath, "secrets")
	return os.WriteFile(filepath.Join(templatesDir, ".env.template"), []byte(sb.String()), 0644)
}

// generateBundleReadme generates README.md for the bundle
func generateBundleReadme(config *MigrationConfig, analysis *AnalysisResult) error {
	var sb strings.Builder

	sb.WriteString("# Homeport Migration Bundle\n\n")
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	sb.WriteString("## Overview\n\n")
	sb.WriteString(fmt.Sprintf("- **Source Resources**: %d\n", analysis.Statistics.TotalResources))
	sb.WriteString(fmt.Sprintf("- **Compute**: %d\n", analysis.Statistics.Migration.Compute))
	sb.WriteString(fmt.Sprintf("- **Database**: %d\n", analysis.Statistics.Migration.Database))
	sb.WriteString(fmt.Sprintf("- **Storage**: %d\n", analysis.Statistics.Migration.Storage))
	sb.WriteString(fmt.Sprintf("- **Networking**: %d\n", analysis.Statistics.Migration.Networking))
	sb.WriteString(fmt.Sprintf("- **Security**: %d\n\n", analysis.Statistics.Migration.Security))

	sb.WriteString("## Quick Start\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("# Import the bundle\n")
	sb.WriteString("homeport import migration.hprt\n\n")
	sb.WriteString("# Or import with secrets file\n")
	sb.WriteString("homeport import migration.hprt --secrets-file .env.production\n\n")
	sb.WriteString("# Or import and deploy immediately\n")
	sb.WriteString("homeport import migration.hprt --deploy\n")
	sb.WriteString("```\n\n")

	sb.WriteString("## Bundle Contents\n\n")
	sb.WriteString("```\n")
	sb.WriteString("compose/           - Docker Compose configuration\n")
	sb.WriteString("configs/           - Service configurations (nginx, postgres, etc.)\n")
	sb.WriteString("scripts/           - Pre/post deployment scripts\n")
	sb.WriteString("migrations/        - Database schema migrations (DDL only)\n")
	sb.WriteString("data-sync/         - Data synchronization scripts\n")
	sb.WriteString("secrets/           - Secret templates and manifest (NO VALUES)\n")
	sb.WriteString("dns/               - DNS record configurations\n")
	sb.WriteString("validation/        - Health check configurations\n")
	sb.WriteString("```\n\n")

	sb.WriteString("## Security Notice\n\n")
	sb.WriteString("This bundle does NOT contain any secret values. Secrets must be provided\n")
	sb.WriteString("at import time via one of these methods:\n\n")
	sb.WriteString("1. **Interactive prompts** - Homeport will ask for each required secret\n")
	sb.WriteString("2. **Secrets file** - `--secrets-file .env.production`\n")
	sb.WriteString("3. **Environment variables** - `HOMEPORT_SECRET_*`\n")
	sb.WriteString("4. **Cloud provider** - `--pull-secrets-from aws`\n\n")

	sb.WriteString("## Requirements\n\n")
	sb.WriteString("- Docker >= 20.10\n")
	sb.WriteString("- Docker Compose >= 2.0\n")
	sb.WriteString("- homeport CLI\n\n")

	return os.WriteFile(filepath.Join(config.OutputPath, "README.md"), []byte(sb.String()), 0644)
}

// generateBundleScripts generates deployment scripts for the bundle
func generateBundleScripts(config *MigrationConfig) error {
	scriptsDir := filepath.Join(config.OutputPath, "scripts")

	// Pre-deploy script
	preDeployContent := `#!/bin/bash
# Pre-deployment script
# Run before docker-compose up

set -e

echo "Running pre-deployment checks..."

# Check Docker is running
if ! docker info >/dev/null 2>&1; then
    echo "Error: Docker is not running"
    exit 1
fi

# Create network if it doesn't exist
if ! docker network inspect homeport >/dev/null 2>&1; then
    echo "Creating homeport network..."
    docker network create homeport
fi

# Create required directories
mkdir -p ./data
mkdir -p ./logs
mkdir -p ./certs

echo "Pre-deployment checks complete"
`

	// Post-deploy script
	postDeployContent := `#!/bin/bash
# Post-deployment script
# Run after docker-compose up

set -e

echo "Running post-deployment checks..."

# Wait for services to be healthy
echo "Waiting for services to start..."
sleep 10

# Check if traefik is running
if docker ps | grep -q traefik; then
    echo "Traefik is running"
else
    echo "Warning: Traefik is not running"
fi

echo "Post-deployment checks complete"
echo ""
echo "Access points:"
echo "  - Traefik Dashboard: http://localhost:8080"
echo "  - HTTP:  http://localhost"
echo "  - HTTPS: https://localhost"
`

	// Healthcheck script
	healthcheckContent := `#!/bin/bash
# Health check script
# Validates deployment is working correctly

set -e

echo "Running health checks..."

FAILED=0

# Check Traefik
if curl -sf http://localhost:8080/ping >/dev/null 2>&1; then
    echo "[OK] Traefik is healthy"
else
    echo "[FAIL] Traefik health check failed"
    FAILED=1
fi

# Check containers
CONTAINERS=$(docker-compose ps -q 2>/dev/null | wc -l)
RUNNING=$(docker-compose ps -q --filter "status=running" 2>/dev/null | wc -l)

if [ "$CONTAINERS" -eq "$RUNNING" ]; then
    echo "[OK] All containers running ($RUNNING/$CONTAINERS)"
else
    echo "[FAIL] Some containers not running ($RUNNING/$CONTAINERS)"
    FAILED=1
fi

if [ $FAILED -eq 0 ]; then
    echo ""
    echo "All health checks passed!"
    exit 0
else
    echo ""
    echo "Some health checks failed!"
    exit 1
fi
`

	// Backup script
	backupContent := `#!/bin/bash
# Backup script for deployed services

set -e

BACKUP_DIR="./backups/$(date +%Y%m%d_%H%M%S)"
mkdir -p "$BACKUP_DIR"

echo "Creating backup in $BACKUP_DIR..."

# Backup Docker volumes
for volume in $(docker volume ls -q | grep homeport); do
    echo "Backing up volume: $volume"
    docker run --rm -v "$volume":/data -v "$(pwd)/$BACKUP_DIR":/backup alpine \
        tar czf "/backup/$volume.tar.gz" -C /data .
done

# Backup configs
cp -r ./configs "$BACKUP_DIR/"

echo "Backup complete: $BACKUP_DIR"
`

	scripts := map[string]string{
		"pre-deploy.sh":  preDeployContent,
		"post-deploy.sh": postDeployContent,
		"healthcheck.sh": healthcheckContent,
		"backup.sh":      backupContent,
	}

	for name, content := range scripts {
		path := filepath.Join(scriptsDir, name)
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}

// detectSecretReferences detects secrets in the analyzed resources using the proper detector registry.
func detectSecretReferences(analysis *AnalysisResult, mappingResults []*mapper.MappingResult) []*bundle.SecretReference {
	// Convert analysis resources to AWSResource format for the detector
	var resources []*resource.AWSResource
	for _, res := range analysis.Resources {
		awsRes := &resource.AWSResource{
			ID:     res.ID,
			Name:   res.Name,
			Type:   resource.Type(res.Type),
			ARN:    res.ARN,
			Region: res.Region,
			Config: res.Config,
			Tags:   res.Tags,
		}
		resources = append(resources, awsRes)
	}

	// Use the detector registry for proper secret detection
	// This handles cluster deduplication, managed secrets, etc.
	registry := detector.NewDefaultRegistry()
	ctx := context.Background()
	manifest, err := registry.DetectAll(ctx, resources)
	if err != nil {
		// Fall back to empty list on error
		return []*bundle.SecretReference{}
	}

	// Convert domain secrets to bundle format
	var refs []*bundle.SecretReference
	for _, secret := range manifest.Secrets {
		refs = append(refs, &bundle.SecretReference{
			Name:        secret.Name,
			Source:      string(secret.Source),
			Key:         secret.Key,
			Description: secret.Description,
			Required:    secret.Required,
			UsedBy:      secret.UsedBy,
		})
	}

	return refs
}

// writeSecretsManifest writes the secrets manifest file
func writeSecretsManifest(outputDir string, refs []*bundle.SecretReference) error {
	secretsDir := filepath.Join(outputDir, "secrets")

	manifest := &bundle.SecretsManifest{
		Secrets:     refs,
		EnvTemplate: ".env.template",
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal secrets manifest: %w", err)
	}

	manifestPath := filepath.Join(secretsDir, "secrets-manifest.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write secrets manifest: %w", err)
	}

	// Also write a README about secrets
	readmeContent := `# Secrets

This directory contains SECRET REFERENCES only, not actual secret values.

## Required Secrets

Check secrets-manifest.json for the list of required secrets.

## Providing Secrets

At import time, provide secrets via:

1. **Interactive prompts** (default)
   homeport import bundle.hprt

2. **Secrets file**
   homeport import bundle.hprt --secrets-file .env.production

3. **Environment variables**
   export HOMEPORT_SECRET_POSTGRES_PASSWORD=mysecret
   homeport import bundle.hprt

4. **Pull from cloud provider**
   homeport import bundle.hprt --pull-secrets-from aws
`

	readmePath := filepath.Join(secretsDir, "README.md")
	return os.WriteFile(readmePath, []byte(readmeContent), 0644)
}

// detectProvider determines the cloud provider from analysis
func detectProvider(analysis *AnalysisResult) string {
	if exportProvider != "" {
		return exportProvider
	}

	// Try to detect from resource types
	for _, res := range analysis.Resources {
		resType := resource.Type(res.Type)
		if strings.Contains(string(resType), "aws_") || strings.Contains(string(resType), "ec2") ||
			strings.Contains(string(resType), "s3") || strings.Contains(string(resType), "rds") {
			return "aws"
		}
		if strings.Contains(string(resType), "google_") || strings.Contains(string(resType), "gcp") ||
			strings.Contains(string(resType), "gce") || strings.Contains(string(resType), "gke") {
			return "gcp"
		}
		if strings.Contains(string(resType), "azure") || strings.Contains(string(resType), "azurerm") {
			return "azure"
		}
	}

	return "unknown"
}

// displayExportSummary displays the export summary
func displayExportSummary(analysis *AnalysisResult, consolidated *stack.ConsolidatedResult, secrets []*bundle.SecretReference) {
	ui.Divider()
	ui.Info("Export Summary")
	ui.Divider()

	fmt.Printf("  Source resources:     %d\n", analysis.Statistics.TotalResources)

	if consolidated != nil {
		fmt.Printf("  Consolidated stacks:  %d\n", consolidated.TotalStacks())
		fmt.Printf("  Total services:       %d\n", consolidated.TotalServices())
	}

	if len(secrets) > 0 {
		fmt.Printf("  Secret references:    %d\n", len(secrets))
	}

	fmt.Println()
	fmt.Println("  Resource breakdown:")
	fmt.Printf("    Compute:    %d\n", analysis.Statistics.Migration.Compute)
	fmt.Printf("    Database:   %d\n", analysis.Statistics.Migration.Database)
	fmt.Printf("    Storage:    %d\n", analysis.Statistics.Migration.Storage)
	fmt.Printf("    Networking: %d\n", analysis.Statistics.Migration.Networking)
	fmt.Printf("    Security:   %d\n", analysis.Statistics.Migration.Security)
}

