package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
	_ "github.com/homeport/homeport/internal/infrastructure/parser/aws"   // Register AWS parsers
	_ "github.com/homeport/homeport/internal/infrastructure/parser/azure" // Register Azure parsers
	_ "github.com/homeport/homeport/internal/infrastructure/parser/gcp"   // Register GCP parsers
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	analyzeOutput      string
	analyzeFormat      string
	analyzeSource      string
	analyzeProfile     string
	analyzeProject     string
	analyzeRegions     []string
	analyzeCredentials string
)

// analyzeCmd represents the analyze command
var analyzeCmd = &cobra.Command{
	Use:   "analyze <path>",
	Short: "Analyze cloud infrastructure from various sources",
	Long: `Analyze cloud infrastructure from Terraform, CloudFormation, ARM templates, or live API.

The analyze command parses your cloud infrastructure configuration and generates
a detailed analysis of all resources, dependencies, and migration requirements.

Supported input sources:
  - terraform      : Terraform state files and HCL (*.tf, terraform.tfstate)
  - cloudformation : AWS CloudFormation templates (*.yaml, *.json, *.template)
  - arm            : Azure Resource Manager templates (*.json)
  - aws-api        : Live AWS API scanning (requires credentials)
  - gcp-api        : Live GCP API scanning (requires credentials)
  - azure-api      : Live Azure API scanning (requires credentials)

Examples:
  # Auto-detect and analyze Terraform directory
  homeport analyze ./infrastructure

  # Analyze specific Terraform state file
  homeport analyze terraform.tfstate

  # Analyze CloudFormation templates
  homeport analyze --source cloudformation ./templates

  # Analyze ARM templates
  homeport analyze --source arm ./arm-templates

  # Analyze live AWS infrastructure via API
  homeport analyze --source aws-api --profile production --region us-east-1

  # Analyze live GCP infrastructure via API
  homeport analyze --source gcp-api --project my-project --region us-central1

  # Analyze live Azure infrastructure via API
  homeport analyze --source azure-api --region eastus

  # Analyze multiple regions
  homeport analyze --source aws-api --region us-east-1 --region eu-west-1

  # Analyze with custom output
  homeport analyze ./infrastructure --output analysis.yaml --format yaml`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var inputPath string
		if len(args) > 0 {
			inputPath = args[0]
		} else if !isAPISource(analyzeSource) {
			return fmt.Errorf("path is required unless using an API source (aws-api, gcp-api, azure-api)")
		}

		if !IsQuiet() {
			ui.Header("Homeport - Infrastructure Analysis")
			if inputPath != "" {
				ui.Info(fmt.Sprintf("Analyzing: %s", inputPath))
			} else {
				switch analyzeSource {
				case "aws-api", "api":
					ui.Info(fmt.Sprintf("Analyzing via AWS API (profile: %s)", analyzeProfile))
				case "gcp-api":
					ui.Info(fmt.Sprintf("Analyzing via GCP API (project: %s)", analyzeProject))
				case "azure-api":
					ui.Info("Analyzing via Azure API")
				}
			}
			ui.Divider()
		}

		// Validate input path if provided
		if inputPath != "" {
			if _, err := os.Stat(inputPath); os.IsNotExist(err) {
				return fmt.Errorf("input path does not exist: %s", inputPath)
			}
		}

		// Perform analysis
		result, err := performAnalysis(inputPath)
		if err != nil {
			return fmt.Errorf("analysis failed: %w", err)
		}

		// Output results
		if err := outputAnalysisResults(result); err != nil {
			return fmt.Errorf("failed to output results: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success("Analysis completed successfully")
			if analyzeOutput != "-" {
				ui.Info(fmt.Sprintf("Results saved to: %s", analyzeOutput))
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)

	analyzeCmd.Flags().StringVarP(&analyzeOutput, "output", "o", "analysis.json", "output file path (use '-' for stdout)")
	analyzeCmd.Flags().StringVarP(&analyzeFormat, "format", "f", "json", "output format (json, yaml, table)")
	analyzeCmd.Flags().StringVarP(&analyzeSource, "source", "s", "", "source type (terraform, cloudformation, arm, aws-api, gcp-api, azure-api)")
	analyzeCmd.Flags().StringVarP(&analyzeProfile, "profile", "p", "", "AWS profile name (for aws-api source)")
	analyzeCmd.Flags().StringVar(&analyzeProject, "project", "", "GCP project ID (for gcp-api source)")
	analyzeCmd.Flags().StringSliceVarP(&analyzeRegions, "region", "r", nil, "Region(s)/location(s) to scan (for API sources)")
	analyzeCmd.Flags().StringVar(&analyzeCredentials, "credentials", "", "path to credentials file")
}

// isAPISource checks if the source is an API-based source.
func isAPISource(source string) bool {
	switch source {
	case "api", "aws-api", "gcp-api", "azure-api":
		return true
	default:
		return false
	}
}

// AnalysisResult represents the result of infrastructure analysis
type AnalysisResult struct {
	InputPath    string              `json:"input_path" yaml:"input_path"`
	ResourceType string              `json:"resource_type" yaml:"resource_type"`
	Resources    []ResourceSummary   `json:"resources" yaml:"resources"`
	Statistics   AnalysisStatistics  `json:"statistics" yaml:"statistics"`
	Dependencies []DependencyMapping `json:"dependencies" yaml:"dependencies"`
}

// ResourceSummary represents a summary of a single resource
type ResourceSummary struct {
	Type         string                 `json:"type" yaml:"type"`
	Name         string                 `json:"name" yaml:"name"`
	ID           string                 `json:"id" yaml:"id"`
	ARN          string                 `json:"arn,omitempty" yaml:"arn,omitempty"`
	Region       string                 `json:"region,omitempty" yaml:"region,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
	Tags         map[string]string      `json:"tags,omitempty" yaml:"tags,omitempty"`
	MigrateAs    string                 `json:"migrate_as" yaml:"migrate_as"`
	Dependencies []string               `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
}

// AnalysisStatistics represents statistics about the analysis
type AnalysisStatistics struct {
	TotalResources int                       `json:"total_resources" yaml:"total_resources"`
	ByType         map[string]int            `json:"by_type" yaml:"by_type"`
	ByRegion       map[string]int            `json:"by_region" yaml:"by_region"`
	Migration      MigrationStatistics       `json:"migration" yaml:"migration"`
}

// MigrationStatistics represents migration-specific statistics
type MigrationStatistics struct {
	Compute    int `json:"compute" yaml:"compute"`
	Storage    int `json:"storage" yaml:"storage"`
	Database   int `json:"database" yaml:"database"`
	Networking int `json:"networking" yaml:"networking"`
	Security   int `json:"security" yaml:"security"`
	Other      int `json:"other" yaml:"other"`
}

// DependencyMapping represents a dependency between resources
type DependencyMapping struct {
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
	Type string `json:"type" yaml:"type"`
}

// performAnalysis performs the actual analysis
func performAnalysis(inputPath string) (*AnalysisResult, error) {
	if IsVerbose() {
		ui.Info("Starting infrastructure analysis...")
	}

	ctx := context.Background()

	// Build parse options
	opts := parser.NewParseOptions()
	if len(analyzeRegions) > 0 {
		opts.WithRegions(analyzeRegions...)
	}

	// Add credentials based on source type
	creds := make(map[string]string)
	if analyzeProfile != "" {
		creds["profile"] = analyzeProfile
	}
	if analyzeProject != "" {
		creds["project"] = analyzeProject
	}
	if len(creds) > 0 {
		opts.WithCredentials(creds)
	}

	var infra *resource.Infrastructure
	var sourceType string

	// Determine parser based on source flag or auto-detect
	switch analyzeSource {
	case "api", "aws-api":
		sourceType = "aws_api"
		if IsVerbose() {
			ui.Info("Using AWS API scanner...")
		}
		// Set default region if none specified
		if len(analyzeRegions) == 0 {
			opts.WithRegions("us-east-1")
		}
		p, pErr := parser.DefaultRegistry().GetByFormat(resource.ProviderAWS, parser.FormatAPI)
		if pErr != nil {
			return nil, fmt.Errorf("AWS API parser not available: %w", pErr)
		}
		var parseErr error
		infra, parseErr = p.Parse(ctx, "", opts)
		if parseErr != nil {
			return nil, fmt.Errorf("AWS API scan failed: %w", parseErr)
		}

	case "gcp-api":
		sourceType = "gcp_api"
		if IsVerbose() {
			ui.Info("Using GCP API scanner...")
		}
		// Set default region if none specified
		if len(analyzeRegions) == 0 {
			opts.WithRegions("us-central1")
		}
		p, pErr := parser.DefaultRegistry().GetByFormat(resource.ProviderGCP, parser.FormatAPI)
		if pErr != nil {
			return nil, fmt.Errorf("GCP API parser not available: %w", pErr)
		}
		var parseErr error
		infra, parseErr = p.Parse(ctx, "", opts)
		if parseErr != nil {
			return nil, fmt.Errorf("GCP API scan failed: %w", parseErr)
		}

	case "azure-api":
		sourceType = "azure_api"
		if IsVerbose() {
			ui.Info("Using Azure API scanner...")
		}
		// Set default region if none specified
		if len(analyzeRegions) == 0 {
			opts.WithRegions("eastus")
		}
		p, pErr := parser.DefaultRegistry().GetByFormat(resource.ProviderAzure, parser.FormatAPI)
		if pErr != nil {
			return nil, fmt.Errorf("Azure API parser not available: %w", pErr)
		}
		var parseErr error
		infra, parseErr = p.Parse(ctx, "", opts)
		if parseErr != nil {
			return nil, fmt.Errorf("Azure API scan failed: %w", parseErr)
		}

	case "cloudformation":
		sourceType = "cloudformation"
		if IsVerbose() {
			ui.Info("Using CloudFormation parser...")
		}
		p, pErr := parser.DefaultRegistry().GetByFormat(resource.ProviderAWS, parser.FormatCloudFormation)
		if pErr != nil {
			return nil, fmt.Errorf("CloudFormation parser not available: %w", pErr)
		}
		var parseErr error
		infra, parseErr = p.Parse(ctx, inputPath, opts)
		if parseErr != nil {
			return nil, fmt.Errorf("CloudFormation parsing failed: %w", parseErr)
		}

	case "arm":
		sourceType = "arm"
		if IsVerbose() {
			ui.Info("Using ARM template parser...")
		}
		p, pErr := parser.DefaultRegistry().GetByFormat(resource.ProviderAzure, parser.FormatARM)
		if pErr != nil {
			return nil, fmt.Errorf("ARM parser not available: %w", pErr)
		}
		var parseErr error
		infra, parseErr = p.Parse(ctx, inputPath, opts)
		if parseErr != nil {
			return nil, fmt.Errorf("ARM parsing failed: %w", parseErr)
		}

	case "terraform":
		sourceType = "terraform"
		if IsVerbose() {
			ui.Info("Using Terraform parser...")
		}
		// Use ParseMulti to detect and parse from any provider
		var parseErr error
		infra, parseErr = parser.DefaultRegistry().ParseMulti(ctx, inputPath, opts)
		if parseErr != nil {
			return nil, fmt.Errorf("Terraform parsing failed: %w", parseErr)
		}

	default:
		// Auto-detect
		if IsVerbose() {
			ui.Info("Auto-detecting source type...")
		}
		p, detectErr := parser.DefaultRegistry().AutoDetect(inputPath)
		if detectErr != nil {
			return nil, fmt.Errorf("failed to detect source type: %w", detectErr)
		}
		sourceType = string(p.SupportedFormats()[0])
		if IsVerbose() {
			ui.Info(fmt.Sprintf("Detected: %s (%s)", sourceType, p.Provider()))
		}
		var parseErr error
		infra, parseErr = p.Parse(ctx, inputPath, opts)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing failed: %w", parseErr)
		}
	}

	// Build analysis result from infrastructure
	absPath := inputPath
	if inputPath != "" {
		absPath, _ = filepath.Abs(inputPath)
	}

	result := buildAnalysisResult(infra, absPath, sourceType)

	if IsVerbose() {
		ui.Info(fmt.Sprintf("Found %d resources", result.Statistics.TotalResources))
	}

	return result, nil
}

// buildAnalysisResult converts infrastructure to analysis result
func buildAnalysisResult(infra *resource.Infrastructure, absPath, sourceType string) *AnalysisResult {
	result := &AnalysisResult{
		InputPath:    absPath,
		ResourceType: sourceType,
		Resources:    make([]ResourceSummary, 0),
		Statistics: AnalysisStatistics{
			ByType:   make(map[string]int),
			ByRegion: make(map[string]int),
		},
		Dependencies: make([]DependencyMapping, 0),
	}

	// Convert resources
	for _, res := range infra.Resources {
		summary := ResourceSummary{
			Type:         string(res.Type),
			Name:         res.Name,
			ID:           res.ID,
			ARN:          res.ARN,
			Region:       res.Region,
			Config:       res.Config,
			Tags:         res.Tags,
			MigrateAs:    getMigrateAs(res.Type),
			Dependencies: res.Dependencies,
		}
		result.Resources = append(result.Resources, summary)

		// Update statistics
		result.Statistics.TotalResources++
		result.Statistics.ByType[string(res.Type)]++
		if res.Region != "" {
			result.Statistics.ByRegion[res.Region]++
		}

		// Update migration stats by category
		category := res.Type.GetCategory()
		switch category {
		case resource.CategoryCompute, resource.CategoryContainer, resource.CategoryServerless:
			result.Statistics.Migration.Compute++
		case resource.CategoryObjectStorage, resource.CategoryFileStorage:
			result.Statistics.Migration.Storage++
		case resource.CategorySQLDatabase, resource.CategoryNoSQLDatabase, resource.CategoryCache:
			result.Statistics.Migration.Database++
		case resource.CategoryLoadBalancer, resource.CategoryDNS, resource.CategoryCDN:
			result.Statistics.Migration.Networking++
		case resource.CategoryAuth, resource.CategorySecrets:
			result.Statistics.Migration.Security++
		default:
			result.Statistics.Migration.Other++
		}

		// Build dependency mappings
		for _, depID := range res.Dependencies {
			result.Dependencies = append(result.Dependencies, DependencyMapping{
				From: res.ID,
				To:   depID,
				Type: "depends_on",
			})
		}
	}

	return result
}

// getMigrateAs returns the suggested migration target for a resource type
func getMigrateAs(t resource.Type) string {
	mapping := map[resource.Type]string{
		resource.TypeEC2Instance:    "docker_container",
		resource.TypeLambdaFunction: "openfaas_function",
		resource.TypeS3Bucket:       "minio_bucket",
		resource.TypeRDSInstance:    "postgres_container",
		resource.TypeRDSCluster:     "postgres_cluster",
		resource.TypeDynamoDBTable:  "scylladb_table",
		resource.TypeElastiCache:    "redis_container",
		resource.TypeSQSQueue:       "rabbitmq_queue",
		resource.TypeSNSTopic:       "rabbitmq_exchange",
		resource.TypeALB:            "traefik_router",
		resource.TypeCognitoPool:    "keycloak_realm",
		resource.TypeEKSCluster:     "k3s_cluster",
		resource.TypeECSService:     "docker_service",
	}

	if target, ok := mapping[t]; ok {
		return target
	}
	return "manual_migration"
}

// outputAnalysisResults outputs the analysis results in the specified format
func outputAnalysisResults(result *AnalysisResult) error {
	switch analyzeFormat {
	case "json":
		return outputJSON(result)
	case "yaml":
		return outputYAML(result)
	case "table":
		return outputTable(result)
	default:
		return fmt.Errorf("unsupported output format: %s", analyzeFormat)
	}
}

// outputJSON outputs the result as JSON
func outputJSON(result *AnalysisResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}

	if analyzeOutput == "-" {
		fmt.Println(string(data))
		return nil
	}

	return os.WriteFile(analyzeOutput, data, 0644)
}

// outputYAML outputs the result as YAML
func outputYAML(result *AnalysisResult) error {
	data, err := yaml.Marshal(result)
	if err != nil {
		return err
	}

	if analyzeOutput == "-" {
		fmt.Println(string(data))
		return nil
	}

	return os.WriteFile(analyzeOutput, data, 0644)
}

// outputTable outputs the result as a formatted table
func outputTable(result *AnalysisResult) error {
	// Print resources table
	fmt.Println("\nResources:")
	table := ui.NewTable([]string{"Type", "Name", "ID", "Region", "Migrate As"})
	for _, resource := range result.Resources {
		table.AddRow([]string{
			resource.Type,
			resource.Name,
			resource.ID,
			resource.Region,
			resource.MigrateAs,
		})
	}
	fmt.Println(table.Render())

	// Print statistics
	fmt.Println("\nStatistics:")
	fmt.Printf("Total Resources: %d\n", result.Statistics.TotalResources)
	fmt.Printf("Compute:         %d\n", result.Statistics.Migration.Compute)
	fmt.Printf("Database:        %d\n", result.Statistics.Migration.Database)
	fmt.Printf("Storage:         %d\n", result.Statistics.Migration.Storage)
	fmt.Printf("Networking:      %d\n", result.Statistics.Migration.Networking)
	fmt.Printf("Security:        %d\n", result.Statistics.Migration.Security)
	fmt.Printf("Other:           %d\n", result.Statistics.Migration.Other)

	// Print dependencies
	if len(result.Dependencies) > 0 {
		fmt.Println("\nDependencies:")
		depTable := ui.NewTable([]string{"From", "To", "Type"})
		for _, dep := range result.Dependencies {
			depTable.AddRow([]string{dep.From, dep.To, dep.Type})
		}
		fmt.Println(depTable.Render())
	}

	// Save to file if not stdout
	if analyzeOutput != "-" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(analyzeOutput, data, 0644)
	}

	return nil
}
