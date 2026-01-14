package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/homeport/homeport/internal/domain/bundle"
	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	infraBundle "github.com/homeport/homeport/internal/infrastructure/bundle"
	"github.com/homeport/homeport/pkg/version"
	_ "github.com/homeport/homeport/internal/infrastructure/parser/aws"   // Register AWS parsers
	_ "github.com/homeport/homeport/internal/infrastructure/parser/azure" // Register Azure parsers
	_ "github.com/homeport/homeport/internal/infrastructure/parser/gcp"   // Register GCP parsers
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	// Common flags
	importOutput string
	importFormat string
	importRegion string

	// AWS-specific flags
	importAWSProfile string

	// GCP-specific flags
	importGCPProject string

	// Azure-specific flags
	importAzureSubscription  string
	importAzureResourceGroup string

	// Bundle import flags
	bundleTarget          string
	bundleSecretsFile     string
	bundlePullSecretsFrom string
	bundleDeploy          bool
	bundleDryRun          bool
	bundleOutputDir       string
	bundleSkipValidation  bool
)

// importCmd represents the import command
var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import cloud infrastructure or .hprt bundles",
	Long: `Import cloud infrastructure by connecting to cloud provider APIs,
or import a .hprt migration bundle for deployment.

Subcommands:
  - bundle : Import a .hprt migration bundle
  - aws    : Import from Amazon Web Services
  - gcp    : Import from Google Cloud Platform
  - azure  : Import from Microsoft Azure

Examples:
  # Import a .hprt bundle
  homeport import bundle migration.hprt

  # Import bundle to remote server
  homeport import bundle migration.hprt --target user@192.168.1.100

  # Import bundle and deploy immediately
  homeport import bundle migration.hprt --deploy

  # Import AWS infrastructure
  homeport import aws --profile production --region us-east-1

  # Import GCP infrastructure
  homeport import gcp --project my-project-id

  # Import Azure infrastructure
  homeport import azure --subscription 12345-abcd`,
}

// importAWSCmd represents the import aws command
var importAWSCmd = &cobra.Command{
	Use:   "aws",
	Short: "Import infrastructure from AWS",
	Long: `Import infrastructure from Amazon Web Services.

This command connects to AWS using your configured credentials and
discovers resources across specified regions.

Credentials are loaded from (in order):
  1. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
  2. Shared credentials file (~/.aws/credentials)
  3. IAM role (if running on EC2)

Examples:
  # Import from default profile
  homeport import aws

  # Import from specific profile
  homeport import aws --profile production

  # Import from specific region
  homeport import aws --region us-east-1

  # Import from multiple regions (scans all enabled regions if not specified)
  homeport import aws --region us-east-1 --region eu-west-1

  # Save to file
  homeport import aws --output aws-resources.json`,
	RunE: runImportAWS,
}

// importGCPCmd represents the import gcp command
var importGCPCmd = &cobra.Command{
	Use:   "gcp",
	Short: "Import infrastructure from GCP",
	Long: `Import infrastructure from Google Cloud Platform.

This command connects to GCP using your configured credentials and
discovers resources in the specified project.

Credentials are loaded from (in order):
  1. GOOGLE_APPLICATION_CREDENTIALS environment variable
  2. Application Default Credentials (gcloud auth application-default login)
  3. Metadata service (if running on GCP)

Examples:
  # Import using default project from gcloud config
  homeport import gcp

  # Import from specific project
  homeport import gcp --project my-project-id

  # Import from specific region
  homeport import gcp --project my-project-id --region us-central1

  # Save to file
  homeport import gcp --project my-project-id --output gcp-resources.yaml --format yaml`,
	RunE: runImportGCP,
}

// importAzureCmd represents the import azure command
var importAzureCmd = &cobra.Command{
	Use:   "azure",
	Short: "Import infrastructure from Azure",
	Long: `Import infrastructure from Microsoft Azure.

This command connects to Azure using your configured credentials and
discovers resources in the specified subscription.

Credentials are loaded from (in order):
  1. Environment variables (AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, AZURE_TENANT_ID)
  2. Azure CLI credentials (az login)
  3. Managed Identity (if running on Azure)

Examples:
  # Import using default subscription
  homeport import azure

  # Import from specific subscription
  homeport import azure --subscription 12345678-abcd-efgh-ijkl-1234567890ab

  # Import from specific resource group
  homeport import azure --subscription 12345 --resource-group my-rg

  # Import from specific region
  homeport import azure --region eastus

  # Save to file
  homeport import azure --output azure-resources.json`,
	RunE: runImportAzure,
}

// importBundleCmd represents the import bundle command
var importBundleCmd = &cobra.Command{
	Use:   "bundle <path.hprt>",
	Short: "Import a .hprt migration bundle",
	Long: `Import a .hprt migration bundle for deployment.

The bundle import command extracts a migration bundle and optionally
deploys it to a local or remote Docker environment.

SECRETS RESOLUTION ORDER:
  1. --secrets-file       : Load secrets from provided file
  2. --pull-secrets-from  : Pull from cloud provider (aws, gcp, azure)
  3. Environment variables: HOMEPORT_SECRET_* prefix
  4. Interactive prompts  : Ask for remaining required secrets

Examples:
  # Import bundle locally (prompts for secrets interactively)
  homeport import bundle migration.hprt

  # Import to remote target via SSH
  homeport import bundle migration.hprt --target user@192.168.1.100

  # Import and deploy immediately
  homeport import bundle migration.hprt --deploy

  # Provide secrets via file
  homeport import bundle migration.hprt --secrets-file .env.production

  # Pull secrets from source cloud (requires credentials)
  homeport import bundle migration.hprt --pull-secrets-from aws

  # Dry run (validate without extracting)
  homeport import bundle migration.hprt --dry-run

  # Specify output directory
  homeport import bundle migration.hprt --output ./my-stack`,
	Args: cobra.ExactArgs(1),
	RunE: runImportBundle,
}

func init() {
	// Add import command to root
	rootCmd.AddCommand(importCmd)

	// Add provider subcommands
	importCmd.AddCommand(importAWSCmd)
	importCmd.AddCommand(importGCPCmd)
	importCmd.AddCommand(importAzureCmd)
	importCmd.AddCommand(importBundleCmd)

	// Common flags for import command (inherited by subcommands)
	importCmd.PersistentFlags().StringVarP(&importOutput, "output", "o", "", "output file path (default: stdout)")
	importCmd.PersistentFlags().StringVarP(&importFormat, "format", "f", "table", "output format (json, yaml, table)")
	importCmd.PersistentFlags().StringVarP(&importRegion, "region", "r", "", "cloud region to scan")

	// AWS-specific flags
	importAWSCmd.Flags().StringVarP(&importAWSProfile, "profile", "p", "", "AWS profile name")

	// GCP-specific flags
	importGCPCmd.Flags().StringVar(&importGCPProject, "project", "", "GCP project ID")

	// Azure-specific flags
	importAzureCmd.Flags().StringVar(&importAzureSubscription, "subscription", "", "Azure subscription ID")
	importAzureCmd.Flags().StringVar(&importAzureResourceGroup, "resource-group", "", "Azure resource group")

	// Bundle import flags
	importBundleCmd.Flags().StringVarP(&bundleTarget, "target", "t", "", "SSH target for remote deployment (user@host)")
	importBundleCmd.Flags().StringVar(&bundleSecretsFile, "secrets-file", "", "path to secrets file (.env format)")
	importBundleCmd.Flags().StringVar(&bundlePullSecretsFrom, "pull-secrets-from", "", "pull secrets from cloud provider (aws, gcp, azure)")
	importBundleCmd.Flags().BoolVar(&bundleDeploy, "deploy", false, "deploy immediately after import")
	importBundleCmd.Flags().BoolVar(&bundleDryRun, "dry-run", false, "validate only, don't extract")
	importBundleCmd.Flags().StringVarP(&bundleOutputDir, "output", "o", "", "output directory for extracted bundle")
	importBundleCmd.Flags().BoolVar(&bundleSkipValidation, "skip-validation", false, "skip bundle validation")
}

// runImportAWS executes the AWS import command
func runImportAWS(cmd *cobra.Command, args []string) error {
	if !IsQuiet() {
		ui.Header("Homeport - AWS Infrastructure Import")
		if importAWSProfile != "" {
			ui.Info(fmt.Sprintf("Profile: %s", importAWSProfile))
		}
		if importRegion != "" {
			ui.Info(fmt.Sprintf("Region: %s", importRegion))
		} else {
			ui.Info("Region: all enabled regions")
		}
		ui.Divider()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Build parse options
	opts := parser.NewParseOptions()
	opts.IgnoreErrors = true

	if importRegion != "" {
		opts.WithRegions(importRegion)
	}

	// Add AWS-specific credentials
	creds := make(map[string]string)
	if importAWSProfile != "" {
		creds["profile"] = importAWSProfile
	}
	if len(creds) > 0 {
		opts.WithCredentials(creds)
	}

	// Get the AWS API parser
	p, err := parser.DefaultRegistry().GetByFormat(resource.ProviderAWS, parser.FormatAPI)
	if err != nil {
		return fmt.Errorf("AWS API parser not available: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(1, 3, "Connecting to AWS"))
	}

	// Validate credentials
	if err := p.Validate(""); err != nil {
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(2, 3, "Discovering resources"))
	}

	// Parse/discover resources
	infra, err := p.Parse(ctx, "", opts)
	if err != nil {
		return fmt.Errorf("failed to discover AWS resources: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(3, 3, "Processing results"))
	}

	// Output results
	if err := outputImportResults(infra, "AWS"); err != nil {
		return fmt.Errorf("failed to output results: %w", err)
	}

	return nil
}

// runImportGCP executes the GCP import command
func runImportGCP(cmd *cobra.Command, args []string) error {
	if !IsQuiet() {
		ui.Header("Homeport - GCP Infrastructure Import")
		if importGCPProject != "" {
			ui.Info(fmt.Sprintf("Project: %s", importGCPProject))
		}
		if importRegion != "" {
			ui.Info(fmt.Sprintf("Region: %s", importRegion))
		} else {
			ui.Info("Region: us-central1 (default)")
		}
		ui.Divider()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Build parse options
	opts := parser.NewParseOptions()
	opts.IgnoreErrors = true

	if importRegion != "" {
		opts.WithRegions(importRegion)
	}

	// Add GCP-specific credentials
	creds := make(map[string]string)
	if importGCPProject != "" {
		creds["project"] = importGCPProject
	}
	if len(creds) > 0 {
		opts.WithCredentials(creds)
	}

	// Get the GCP API parser
	p, err := parser.DefaultRegistry().GetByFormat(resource.ProviderGCP, parser.FormatAPI)
	if err != nil {
		return fmt.Errorf("GCP API parser not available: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(1, 3, "Connecting to GCP"))
	}

	// Validate credentials
	if err := p.Validate(""); err != nil {
		return fmt.Errorf("failed to validate GCP credentials: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(2, 3, "Discovering resources"))
	}

	// Parse/discover resources
	infra, err := p.Parse(ctx, "", opts)
	if err != nil {
		return fmt.Errorf("failed to discover GCP resources: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(3, 3, "Processing results"))
	}

	// Output results
	if err := outputImportResults(infra, "GCP"); err != nil {
		return fmt.Errorf("failed to output results: %w", err)
	}

	return nil
}

// runImportAzure executes the Azure import command
func runImportAzure(cmd *cobra.Command, args []string) error {
	if !IsQuiet() {
		ui.Header("Homeport - Azure Infrastructure Import")
		if importAzureSubscription != "" {
			ui.Info(fmt.Sprintf("Subscription: %s", importAzureSubscription))
		}
		if importAzureResourceGroup != "" {
			ui.Info(fmt.Sprintf("Resource Group: %s", importAzureResourceGroup))
		}
		if importRegion != "" {
			ui.Info(fmt.Sprintf("Region: %s", importRegion))
		} else {
			ui.Info("Region: eastus (default)")
		}
		ui.Divider()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Build parse options
	opts := parser.NewParseOptions()
	opts.IgnoreErrors = true

	if importRegion != "" {
		opts.WithRegions(importRegion)
	}

	// Add Azure-specific credentials
	creds := make(map[string]string)
	if importAzureSubscription != "" {
		creds["subscription"] = importAzureSubscription
	}
	if importAzureResourceGroup != "" {
		creds["resource_group"] = importAzureResourceGroup
	}
	if len(creds) > 0 {
		opts.WithCredentials(creds)
	}

	// Get the Azure API parser
	p, err := parser.DefaultRegistry().GetByFormat(resource.ProviderAzure, parser.FormatAPI)
	if err != nil {
		return fmt.Errorf("Azure API parser not available: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(1, 3, "Connecting to Azure"))
	}

	// Validate credentials
	if err := p.Validate(""); err != nil {
		return fmt.Errorf("failed to validate Azure credentials: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(2, 3, "Discovering resources"))
	}

	// Parse/discover resources
	infra, err := p.Parse(ctx, "", opts)
	if err != nil {
		return fmt.Errorf("failed to discover Azure resources: %w", err)
	}

	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(3, 3, "Processing results"))
	}

	// Output results
	if err := outputImportResults(infra, "Azure"); err != nil {
		return fmt.Errorf("failed to output results: %w", err)
	}

	return nil
}

// ImportResult represents the result of an import operation
type ImportResult struct {
	Provider   string                   `json:"provider" yaml:"provider"`
	Region     string                   `json:"region,omitempty" yaml:"region,omitempty"`
	Metadata   map[string]string        `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Resources  []ImportedResource       `json:"resources" yaml:"resources"`
	Statistics ImportStatistics         `json:"statistics" yaml:"statistics"`
}

// ImportedResource represents a single imported resource
type ImportedResource struct {
	Type         string            `json:"type" yaml:"type"`
	Name         string            `json:"name" yaml:"name"`
	ID           string            `json:"id" yaml:"id"`
	Region       string            `json:"region,omitempty" yaml:"region,omitempty"`
	ARN          string            `json:"arn,omitempty" yaml:"arn,omitempty"`
	Tags         map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
}

// ImportStatistics contains statistics about the import
type ImportStatistics struct {
	TotalResources int            `json:"total_resources" yaml:"total_resources"`
	ByType         map[string]int `json:"by_type" yaml:"by_type"`
	ByRegion       map[string]int `json:"by_region" yaml:"by_region"`
	ByCategory     map[string]int `json:"by_category" yaml:"by_category"`
}

// outputImportResults outputs the import results in the specified format
func outputImportResults(infra *resource.Infrastructure, providerName string) error {
	result := buildImportResult(infra, providerName)

	switch importFormat {
	case "json":
		return outputImportJSON(result)
	case "yaml":
		return outputImportYAML(result)
	case "table":
		return outputImportTable(result)
	default:
		return fmt.Errorf("unsupported output format: %s", importFormat)
	}
}

// buildImportResult builds the import result from infrastructure
func buildImportResult(infra *resource.Infrastructure, providerName string) *ImportResult {
	result := &ImportResult{
		Provider:  providerName,
		Region:    infra.Region,
		Metadata:  infra.Metadata,
		Resources: make([]ImportedResource, 0, len(infra.Resources)),
		Statistics: ImportStatistics{
			ByType:     make(map[string]int),
			ByRegion:   make(map[string]int),
			ByCategory: make(map[string]int),
		},
	}

	for _, res := range infra.Resources {
		imported := ImportedResource{
			Type:         string(res.Type),
			Name:         res.Name,
			ID:           res.ID,
			Region:       res.Region,
			ARN:          res.ARN,
			Tags:         res.Tags,
			Config:       res.Config,
			Dependencies: res.Dependencies,
		}
		result.Resources = append(result.Resources, imported)

		// Update statistics
		result.Statistics.TotalResources++
		result.Statistics.ByType[string(res.Type)]++
		if res.Region != "" {
			result.Statistics.ByRegion[res.Region]++
		}

		category := res.Type.GetCategory()
		result.Statistics.ByCategory[string(category)]++
	}

	return result
}

// outputImportJSON outputs the result as JSON
func outputImportJSON(result *ImportResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}

	if importOutput == "" || importOutput == "-" {
		fmt.Println(string(data))
		return nil
	}

	return os.WriteFile(importOutput, data, 0644)
}

// outputImportYAML outputs the result as YAML
func outputImportYAML(result *ImportResult) error {
	data, err := yaml.Marshal(result)
	if err != nil {
		return err
	}

	if importOutput == "" || importOutput == "-" {
		fmt.Println(string(data))
		return nil
	}

	return os.WriteFile(importOutput, data, 0644)
}

// outputImportTable outputs the result as a formatted table
func outputImportTable(result *ImportResult) error {
	if !IsQuiet() {
		ui.Divider()
		ui.Success(fmt.Sprintf("Discovered %d resources from %s", result.Statistics.TotalResources, result.Provider))
		fmt.Println()
	}

	// Print resources table
	if len(result.Resources) > 0 {
		fmt.Println("Resources:")
		table := ui.NewTable([]string{"Type", "Name", "ID", "Region"})
		for _, res := range result.Resources {
			table.AddRow([]string{
				res.Type,
				res.Name,
				truncateString(res.ID, 40),
				res.Region,
			})
		}
		fmt.Println(table.Render())
	}

	// Print statistics
	fmt.Println("Statistics:")
	fmt.Printf("  Total Resources: %d\n", result.Statistics.TotalResources)

	if len(result.Statistics.ByCategory) > 0 {
		fmt.Println("\n  By Category:")
		for category, count := range result.Statistics.ByCategory {
			fmt.Printf("    %-15s: %d\n", category, count)
		}
	}

	if len(result.Statistics.ByRegion) > 0 {
		fmt.Println("\n  By Region:")
		for region, count := range result.Statistics.ByRegion {
			fmt.Printf("    %-15s: %d\n", region, count)
		}
	}

	// Save to file if output specified
	if importOutput != "" && importOutput != "-" {
		// Default to JSON when saving from table view
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(importOutput, data, 0644); err != nil {
			return err
		}
		if !IsQuiet() {
			fmt.Println()
			ui.Info(fmt.Sprintf("Results saved to: %s", importOutput))
		}
	}

	return nil
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// runImportBundle executes the bundle import command
func runImportBundle(cmd *cobra.Command, args []string) error {
	bundlePath := args[0]

	// Validate bundle file exists and has correct extension
	if _, err := os.Stat(bundlePath); os.IsNotExist(err) {
		return fmt.Errorf("bundle file not found: %s", bundlePath)
	}

	if !strings.HasSuffix(bundlePath, ".hprt") {
		ui.Warning("File does not have .hprt extension, attempting to import anyway")
	}

	if !IsQuiet() {
		ui.Header("Homeport - Bundle Import")
		ui.Info(fmt.Sprintf("Bundle: %s", bundlePath))
		if bundleTarget != "" {
			ui.Info(fmt.Sprintf("Target: %s", bundleTarget))
		} else {
			ui.Info("Target: local")
		}
		if bundleDryRun {
			ui.Info("Mode: dry run (validation only)")
		} else if bundleDeploy {
			ui.Info("Mode: import + deploy")
		}
		ui.Divider()
	}

	// Create importer
	importer := infraBundle.NewImporter(version.Version)

	// Build import options
	opts := infraBundle.ImportOptions{
		OutputDir:           bundleOutputDir,
		TargetHost:          extractHost(bundleTarget),
		TargetUser:          extractUser(bundleTarget),
		SecretsFile:         bundleSecretsFile,
		PullSecretsFrom:     bundlePullSecretsFrom,
		SkipValidation:      bundleSkipValidation,
		SkipDependencyCheck: false,
		DryRun:              bundleDryRun,
		Deploy:              bundleDeploy,
	}

	// Default output directory if not specified
	if opts.OutputDir == "" {
		bundleName := strings.TrimSuffix(filepath.Base(bundlePath), ".hprt")
		opts.OutputDir = filepath.Join(".", bundleName+"-import")
	}

	totalSteps := 5
	if bundleDryRun {
		totalSteps = 3
	}
	currentStep := 0

	// Step 1: Read and validate bundle
	currentStep++
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Reading bundle"))
	}

	result, err := importer.Import(bundlePath, opts)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Step 2: Display bundle info
	currentStep++
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Validating bundle"))
		displayBundleInfo(result.Bundle)
	}

	// Display validation results if available
	if result.Validation != nil {
		if !IsQuiet() {
			displayBundleValidation(result.Validation)
		}
		if result.Validation.HasFatalErrors() {
			return fmt.Errorf("bundle validation failed")
		}
	}

	// Handle dry run
	if bundleDryRun {
		currentStep++
		if !IsQuiet() {
			fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Checking secrets"))
			displaySecretsStatus(result)
			ui.Divider()
			if result.Ready {
				ui.Success("Dry run complete - bundle is valid and ready for import")
			} else {
				ui.Warning("Dry run complete - bundle is valid but missing secrets")
				displayMissingSecrets(result.MissingSecrets)
			}
		}
		return nil
	}

	// Step 3: Resolve secrets
	currentStep++
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Resolving secrets"))
	}

	if len(result.MissingSecrets) > 0 {
		// Try to resolve missing secrets interactively
		if err := resolveSecretsInteractively(result, opts.OutputDir); err != nil {
			return fmt.Errorf("failed to resolve secrets: %w", err)
		}
	}

	// Step 4: Extract bundle
	currentStep++
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Extracting bundle"))
		ui.Info(fmt.Sprintf("Extracted to: %s", result.ExtractedTo))
	}

	// Step 5: Deploy if requested
	if bundleDeploy {
		currentStep++
		if !IsQuiet() {
			fmt.Println(ui.SimpleProgress(currentStep, totalSteps, "Deploying stack"))
		}

		if bundleTarget != "" {
			if err := deployRemote(result.ExtractedTo, opts); err != nil {
				return fmt.Errorf("remote deployment failed: %w", err)
			}
		} else {
			if err := deployLocal(result.ExtractedTo); err != nil {
				return fmt.Errorf("local deployment failed: %w", err)
			}
		}
	}

	// Display summary
	if !IsQuiet() {
		ui.Divider()
		ui.Success("Bundle import completed successfully")
		ui.Info(fmt.Sprintf("Output: %s", result.ExtractedTo))

		if !bundleDeploy {
			fmt.Println("\nNext steps:")
			fmt.Println("  1. cd " + result.ExtractedTo)
			fmt.Println("  2. Review docker-compose.yml and .env files")
			fmt.Println("  3. Run: docker-compose up -d")
		}
	}

	return nil
}

// extractHost extracts the host from a user@host string
func extractHost(target string) string {
	if target == "" {
		return ""
	}
	if strings.Contains(target, "@") {
		parts := strings.SplitN(target, "@", 2)
		return parts[1]
	}
	return target
}

// extractUser extracts the user from a user@host string
func extractUser(target string) string {
	if target == "" {
		return ""
	}
	if strings.Contains(target, "@") {
		parts := strings.SplitN(target, "@", 2)
		return parts[0]
	}
	return "root" // Default user
}

// displayBundleInfo shows bundle metadata
func displayBundleInfo(b *bundle.Bundle) {
	if b == nil || b.Manifest == nil {
		return
	}

	m := b.Manifest
	fmt.Println()
	fmt.Println("Bundle Information:")
	fmt.Printf("  Version:         %s\n", m.Version)
	fmt.Printf("  Created:         %s\n", m.Created.Format(time.RFC3339))
	fmt.Printf("  Homeport:        %s\n", m.HomeportVersion)

	if m.Source != nil {
		fmt.Printf("  Source Provider: %s\n", m.Source.Provider)
		if m.Source.Region != "" {
			fmt.Printf("  Source Region:   %s\n", m.Source.Region)
		}
		fmt.Printf("  Resources:       %d\n", m.Source.ResourceCount)
	}

	if m.Target != nil {
		fmt.Printf("  Target Type:     %s\n", m.Target.Type)
		fmt.Printf("  Stacks:          %d\n", m.Target.StackCount)
		if m.Target.Consolidation {
			fmt.Printf("  Consolidation:   enabled\n")
		}
	}

	fmt.Printf("  Files:           %d\n", b.FileCount())
	fmt.Println()
}

// displayBundleValidation shows bundle validation results
func displayBundleValidation(v *infraBundle.ValidationResult) {
	if v.Valid {
		ui.Success("Bundle validation passed")
	} else {
		ui.Error("Bundle validation failed")
	}

	if len(v.Errors) > 0 {
		for _, err := range v.Errors {
			severity := "ERROR"
			if !err.Fatal {
				severity = "WARNING"
			}
			fmt.Printf("  [%s] %s: %s\n", severity, err.Field, err.Message)
		}
	}

	if len(v.Warnings) > 0 && IsVerbose() {
		fmt.Println("\nWarnings:")
		for _, w := range v.Warnings {
			fmt.Printf("  - %s\n", w)
		}
	}
}

// displaySecretsStatus shows which secrets are provided/missing
func displaySecretsStatus(result *infraBundle.ImportResult) {
	if len(result.RequiredSecrets) == 0 {
		ui.Info("No secrets required")
		return
	}

	fmt.Println("\nSecrets Status:")
	for _, secret := range result.RequiredSecrets {
		status := "MISSING"
		if result.ProvidedSecrets[secret.Name] {
			status = "OK"
		}

		required := ""
		if secret.Required {
			required = " (required)"
		}

		fmt.Printf("  [%s] %s%s\n", status, secret.Name, required)
	}
}

// displayMissingSecrets lists missing secrets
func displayMissingSecrets(missing []string) {
	if len(missing) == 0 {
		return
	}

	fmt.Println("\nMissing secrets:")
	for _, name := range missing {
		fmt.Printf("  - %s\n", name)
	}
	fmt.Println("\nProvide secrets using:")
	fmt.Println("  --secrets-file .env.production")
	fmt.Println("  --pull-secrets-from aws")
	fmt.Println("  Environment: HOMEPORT_SECRET_<NAME>=value")
}

// resolveSecretsInteractively prompts for missing secrets
func resolveSecretsInteractively(result *infraBundle.ImportResult, outputDir string) error {
	if len(result.MissingSecrets) == 0 {
		return nil
	}

	ui.Info("Some secrets are missing. Please provide them now.")
	fmt.Println()

	secrets := make(map[string]string)

	for _, secretName := range result.MissingSecrets {
		// Find the secret reference
		var desc string
		for _, ref := range result.RequiredSecrets {
			if ref.Name == secretName {
				desc = ref.Description
				break
			}
		}

		prompt := secretName
		if desc != "" {
			prompt = fmt.Sprintf("%s (%s)", secretName, desc)
		}

		value, err := ui.Prompt(prompt)
		if err != nil {
			return fmt.Errorf("failed to read secret %s: %w", secretName, err)
		}

		if value == "" {
			ui.Warning(fmt.Sprintf("Skipped %s (empty value)", secretName))
		} else {
			secrets[secretName] = value
		}
	}

	// Write secrets to .env file in the output directory
	if len(secrets) > 0 {
		envPath := filepath.Join(outputDir, "secrets", ".env")
		if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
			return fmt.Errorf("failed to create secrets directory: %w", err)
		}

		var sb strings.Builder
		sb.WriteString("# Secrets provided during import\n")
		sb.WriteString(fmt.Sprintf("# Generated: %s\n\n", time.Now().Format(time.RFC3339)))

		for name, value := range secrets {
			sb.WriteString(fmt.Sprintf("%s=%s\n", name, value))
		}

		if err := os.WriteFile(envPath, []byte(sb.String()), 0600); err != nil {
			return fmt.Errorf("failed to write secrets file: %w", err)
		}

		ui.Success(fmt.Sprintf("Secrets written to %s", envPath))
	}

	return nil
}

// deployLocal deploys the stack locally using docker-compose
func deployLocal(extractedDir string) error {
	// Find docker-compose.yml
	composePath := filepath.Join(extractedDir, "compose", "docker-compose.yml")
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		composePath = filepath.Join(extractedDir, "docker-compose.yml")
		if _, err := os.Stat(composePath); os.IsNotExist(err) {
			return fmt.Errorf("docker-compose.yml not found")
		}
	}

	composeDir := filepath.Dir(composePath)

	// Run pre-deploy script if exists
	preDeployScript := filepath.Join(extractedDir, "scripts", "pre-deploy.sh")
	if _, err := os.Stat(preDeployScript); err == nil {
		if IsVerbose() {
			ui.Info("Running pre-deploy script...")
		}
		cmd := exec.Command("bash", preDeployScript)
		cmd.Dir = extractedDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			ui.Warning(fmt.Sprintf("Pre-deploy script failed: %v", err))
		}
	}

	// Run docker-compose up
	if IsVerbose() {
		ui.Info("Starting docker-compose up...")
	}

	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker-compose up failed: %w", err)
	}

	// Run post-deploy script if exists
	postDeployScript := filepath.Join(extractedDir, "scripts", "post-deploy.sh")
	if _, err := os.Stat(postDeployScript); err == nil {
		if IsVerbose() {
			ui.Info("Running post-deploy script...")
		}
		cmd := exec.Command("bash", postDeployScript)
		cmd.Dir = extractedDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			ui.Warning(fmt.Sprintf("Post-deploy script failed: %v", err))
		}
	}

	ui.Success("Stack deployed successfully")
	return nil
}

// deployRemote deploys the stack to a remote server via SSH
func deployRemote(extractedDir string, opts infraBundle.ImportOptions) error {
	target := opts.TargetUser + "@" + opts.TargetHost
	remotePath := "/opt/homeport"

	if IsVerbose() {
		ui.Info(fmt.Sprintf("Deploying to remote target: %s", target))
	}

	// Create remote directory
	if err := runSSHCommand(target, fmt.Sprintf("mkdir -p %s", remotePath)); err != nil {
		return fmt.Errorf("failed to create remote directory: %w", err)
	}

	// Copy files to remote using rsync or scp
	if IsVerbose() {
		ui.Info("Copying files to remote server...")
	}

	// Try rsync first, fall back to scp
	rsyncCmd := exec.Command("rsync", "-avz", "--progress",
		extractedDir+"/",
		fmt.Sprintf("%s:%s/", target, remotePath))
	rsyncCmd.Stdout = os.Stdout
	rsyncCmd.Stderr = os.Stderr

	if err := rsyncCmd.Run(); err != nil {
		// Fall back to scp
		if IsVerbose() {
			ui.Warning("rsync failed, trying scp...")
		}
		scpCmd := exec.Command("scp", "-r",
			extractedDir,
			fmt.Sprintf("%s:%s", target, remotePath))
		scpCmd.Stdout = os.Stdout
		scpCmd.Stderr = os.Stderr

		if err := scpCmd.Run(); err != nil {
			return fmt.Errorf("failed to copy files: %w", err)
		}
	}

	// Run pre-deploy script remotely
	preDeployScript := filepath.Join(remotePath, "scripts", "pre-deploy.sh")
	if err := runSSHCommand(target, fmt.Sprintf("test -f %s && bash %s || true", preDeployScript, preDeployScript)); err != nil {
		ui.Warning(fmt.Sprintf("Pre-deploy script warning: %v", err))
	}

	// Run docker-compose up remotely
	composeDir := filepath.Join(remotePath, "compose")
	if err := runSSHCommand(target, fmt.Sprintf("cd %s && docker compose up -d", composeDir)); err != nil {
		return fmt.Errorf("docker-compose up failed on remote: %w", err)
	}

	// Run post-deploy script remotely
	postDeployScript := filepath.Join(remotePath, "scripts", "post-deploy.sh")
	if err := runSSHCommand(target, fmt.Sprintf("test -f %s && bash %s || true", postDeployScript, postDeployScript)); err != nil {
		ui.Warning(fmt.Sprintf("Post-deploy script warning: %v", err))
	}

	ui.Success(fmt.Sprintf("Stack deployed successfully to %s", opts.TargetHost))
	return nil
}

// runSSHCommand executes a command on a remote host via SSH
func runSSHCommand(target, command string) error {
	cmd := exec.Command("ssh", target, command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
