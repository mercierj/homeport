package cli

import (
	"context"
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
	infraBundle "github.com/homeport/homeport/internal/infrastructure/bundle"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
	"github.com/homeport/homeport/pkg/version"
	"github.com/spf13/cobra"

	_ "github.com/homeport/homeport/internal/infrastructure/parser/aws"   // Register AWS parsers
	_ "github.com/homeport/homeport/internal/infrastructure/parser/azure" // Register Azure parsers
	_ "github.com/homeport/homeport/internal/infrastructure/parser/gcp"   // Register GCP parsers
)

var (
	wizardSource   string
	wizardTarget   string
	wizardDomain   string
	wizardOutput   string
	wizardYes      bool
	wizardProfile  string
	wizardProject  string
	wizardRegion   string
)

// WizardState holds the current state of the wizard
type WizardState struct {
	// Step 1: Source
	SourceType   string
	SourcePath   string
	SourceFormat parser.Format

	// Analysis results
	Analysis       *AnalysisResult
	MappingResults []*mapper.MappingResult
	Consolidated   *stack.ConsolidatedResult

	// Step 3: Configuration
	Domain          string
	Consolidate     bool
	DetectSecrets   bool
	SecretRefs      []*bundle.SecretReference

	// Step 4: Export
	BundlePath  string
	OutputDir   string

	// Step 5: Deploy
	DeployTarget string
	Deploy       bool
}

// wizardCmd represents the wizard command
var wizardCmd = &cobra.Command{
	Use:   "wizard",
	Short: "Interactive migration wizard",
	Long: `Unified interactive migration wizard that guides you through the entire process.

The wizard walks you through 5 steps:
  1. Analyze Source   - Select source type and path
  2. Review Resources - View discovered resources
  3. Configure Export - Set domain, consolidation, secrets
  4. Export Bundle    - Create .hprt bundle
  5. Deploy (optional) - Deploy to target server

The wizard can run interactively (default) or non-interactively with --yes flag.

Examples:
  # Interactive wizard
  homeport wizard

  # Wizard with source path
  homeport wizard --source ./terraform

  # Non-interactive mode (use defaults)
  homeport wizard --source ./terraform --target 192.168.1.100 --yes

  # With all options specified
  homeport wizard --source ./infra.tf --domain example.com --output migration.hprt --yes`,
	RunE: runWizard,
}

func init() {
	rootCmd.AddCommand(wizardCmd)

	wizardCmd.Flags().StringVarP(&wizardSource, "source", "s", "", "source path (terraform files, state, or cloud config)")
	wizardCmd.Flags().StringVarP(&wizardTarget, "target", "t", "", "deployment target (SSH: user@host or 'local')")
	wizardCmd.Flags().StringVarP(&wizardDomain, "domain", "d", "", "domain name for services")
	wizardCmd.Flags().StringVarP(&wizardOutput, "output", "o", "", "output bundle path (default: migration-TIMESTAMP.hprt)")
	wizardCmd.Flags().BoolVarP(&wizardYes, "yes", "y", false, "non-interactive mode (use defaults, skip confirmations)")
	wizardCmd.Flags().StringVarP(&wizardProfile, "profile", "p", "", "AWS profile name (for AWS source)")
	wizardCmd.Flags().StringVar(&wizardProject, "project", "", "GCP project ID (for GCP source)")
	wizardCmd.Flags().StringVarP(&wizardRegion, "region", "r", "", "cloud region")
}

func runWizard(cmd *cobra.Command, args []string) error {
	state := &WizardState{}

	if !IsQuiet() {
		displayWizardHeader()
	}

	// Step 1: Analyze Source
	if err := wizardStep1AnalyzeSource(state); err != nil {
		return fmt.Errorf("step 1 failed: %w", err)
	}

	// Step 2: Review Resources
	if err := wizardStep2ReviewResources(state); err != nil {
		return fmt.Errorf("step 2 failed: %w", err)
	}

	// Step 3: Configure Export
	if err := wizardStep3ConfigureExport(state); err != nil {
		return fmt.Errorf("step 3 failed: %w", err)
	}

	// Step 4: Export Bundle
	if err := wizardStep4ExportBundle(state); err != nil {
		return fmt.Errorf("step 4 failed: %w", err)
	}

	// Step 5: Deploy (optional)
	if err := wizardStep5Deploy(state); err != nil {
		return fmt.Errorf("step 5 failed: %w", err)
	}

	// Display final summary
	displayWizardSummary(state)

	return nil
}

// displayWizardHeader shows the wizard header
func displayWizardHeader() {
	ui.Header("HOMEPORT MIGRATION WIZARD")
	fmt.Println()
	fmt.Println("  This wizard will guide you through migrating your cloud infrastructure")
	fmt.Println("  to a self-hosted Docker stack.")
	fmt.Println()
	ui.Divider()
}

// displayStepHeader shows a step header
func displayStepHeader(step, total int, title string) {
	if IsQuiet() {
		return
	}
	fmt.Println()
	fmt.Printf("  Step %d/%d: %s\n", step, total, title)
	fmt.Println("  " + strings.Repeat("-", 60))
	fmt.Println()
}

// wizardStep1AnalyzeSource handles source selection and analysis
func wizardStep1AnalyzeSource(state *WizardState) error {
	displayStepHeader(1, 5, "Analyze Source")

	ctx := context.Background()

	// Determine source type
	if wizardSource != "" {
		state.SourcePath = wizardSource
	} else if !wizardYes {
		// Interactive: ask for source type
		sourceTypes := []string{
			"Terraform files (*.tf)",
			"Terraform state (terraform.tfstate)",
			"CloudFormation templates (*.yaml, *.json)",
			"ARM templates (*.json)",
			"AWS API (live scan)",
			"GCP API (live scan)",
			"Azure API (live scan)",
		}

		if !IsQuiet() {
			fmt.Println("  ? Select source type:")
		}

		idx, err := ui.PromptSelect("Select source type", sourceTypes)
		if err != nil {
			return fmt.Errorf("failed to select source type: %w", err)
		}

		switch idx {
		case 0, 1:
			state.SourceType = "terraform"
		case 2:
			state.SourceType = "cloudformation"
		case 3:
			state.SourceType = "arm"
		case 4:
			state.SourceType = "aws-api"
		case 5:
			state.SourceType = "gcp-api"
		case 6:
			state.SourceType = "azure-api"
		}

		// Ask for path if not API source
		if !isAPISource(state.SourceType) {
			path, err := ui.Prompt("Enter source path")
			if err != nil {
				return fmt.Errorf("failed to get source path: %w", err)
			}
			state.SourcePath = path
		}
	} else {
		return fmt.Errorf("--source is required in non-interactive mode")
	}

	// Validate source path if provided
	if state.SourcePath != "" {
		if _, err := os.Stat(state.SourcePath); os.IsNotExist(err) {
			return fmt.Errorf("source path does not exist: %s", state.SourcePath)
		}
	}

	// Build parse options
	opts := parser.NewParseOptions()
	if wizardRegion != "" {
		opts.WithRegions(wizardRegion)
	}

	creds := make(map[string]string)
	if wizardProfile != "" {
		creds["profile"] = wizardProfile
	}
	if wizardProject != "" {
		creds["project"] = wizardProject
	}
	if len(creds) > 0 {
		opts.WithCredentials(creds)
	}

	// Show progress
	if !IsQuiet() {
		fmt.Println("  Analyzing source infrastructure...")
		fmt.Println()
	}

	// Perform analysis based on source type
	var infra *resource.Infrastructure
	var sourceType string

	if isAPISource(state.SourceType) {
		// API-based analysis
		var provider resource.Provider
		switch state.SourceType {
		case "aws-api":
			provider = resource.ProviderAWS
			sourceType = "aws_api"
			if len(opts.Regions) == 0 {
				opts.WithRegions("us-east-1")
			}
		case "gcp-api":
			provider = resource.ProviderGCP
			sourceType = "gcp_api"
			if len(opts.Regions) == 0 {
				opts.WithRegions("us-central1")
			}
		case "azure-api":
			provider = resource.ProviderAzure
			sourceType = "azure_api"
			if len(opts.Regions) == 0 {
				opts.WithRegions("eastus")
			}
		}

		p, err := parser.DefaultRegistry().GetByFormat(provider, parser.FormatAPI)
		if err != nil {
			return fmt.Errorf("API parser not available: %w", err)
		}

		infra, err = p.Parse(ctx, "", opts)
		if err != nil {
			return fmt.Errorf("API scan failed: %w", err)
		}
	} else {
		// File-based analysis
		p, err := parser.DefaultRegistry().AutoDetect(state.SourcePath)
		if err != nil {
			return fmt.Errorf("failed to detect source type: %w", err)
		}

		sourceType = string(p.SupportedFormats()[0])
		if IsVerbose() {
			ui.Info(fmt.Sprintf("Detected: %s (%s)", sourceType, p.Provider()))
		}

		infra, err = p.Parse(ctx, state.SourcePath, opts)
		if err != nil {
			return fmt.Errorf("parsing failed: %w", err)
		}
	}

	// Build analysis result
	absPath := state.SourcePath
	if state.SourcePath != "" {
		absPath, _ = filepath.Abs(state.SourcePath)
	}
	state.Analysis = buildAnalysisResult(infra, absPath, sourceType)

	if state.Analysis.Statistics.TotalResources == 0 {
		return fmt.Errorf("no resources found in source")
	}

	// Show summary
	if !IsQuiet() {
		fmt.Printf("  Found %d resources:\n", state.Analysis.Statistics.TotalResources)
		if state.Analysis.Statistics.Migration.Compute > 0 {
			fmt.Printf("    - %d compute resources\n", state.Analysis.Statistics.Migration.Compute)
		}
		if state.Analysis.Statistics.Migration.Database > 0 {
			fmt.Printf("    - %d database resources\n", state.Analysis.Statistics.Migration.Database)
		}
		if state.Analysis.Statistics.Migration.Storage > 0 {
			fmt.Printf("    - %d storage resources\n", state.Analysis.Statistics.Migration.Storage)
		}
		if state.Analysis.Statistics.Migration.Networking > 0 {
			fmt.Printf("    - %d networking resources\n", state.Analysis.Statistics.Migration.Networking)
		}
		if state.Analysis.Statistics.Migration.Security > 0 {
			fmt.Printf("    - %d security resources\n", state.Analysis.Statistics.Migration.Security)
		}
		fmt.Println()
		ui.Success("Analysis complete")
	}

	return nil
}

// wizardStep2ReviewResources shows found resources and gets confirmation
func wizardStep2ReviewResources(state *WizardState) error {
	displayStepHeader(2, 5, "Review Resources")

	// Show resource table
	if !IsQuiet() && len(state.Analysis.Resources) > 0 {
		// Show first 20 resources (or all if less)
		maxShow := 20
		if len(state.Analysis.Resources) < maxShow {
			maxShow = len(state.Analysis.Resources)
		}

		table := ui.NewTable([]string{"Type", "Name", "Region", "Migrate As"})
		for i := 0; i < maxShow; i++ {
			res := state.Analysis.Resources[i]
			table.AddRow([]string{
				res.Type,
				truncateString(res.Name, 30),
				res.Region,
				res.MigrateAs,
			})
		}
		fmt.Println(table.Render())

		if len(state.Analysis.Resources) > maxShow {
			fmt.Printf("  ... and %d more resources\n", len(state.Analysis.Resources)-maxShow)
		}
		fmt.Println()
	}

	// Map resources to Docker services
	ctx := context.Background()
	for _, resSummary := range state.Analysis.Resources {
		resType := resource.Type(resSummary.Type)

		m, mapErr := mapper.DefaultRegistry.Get(resType)
		if mapErr != nil || m == nil {
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
			continue
		}

		if result != nil {
			result.SourceResourceType = string(resType)
			result.SourceResourceName = resSummary.Name
			state.MappingResults = append(state.MappingResults, result)
		}
	}

	if !IsQuiet() {
		fmt.Printf("  %d resources can be mapped to Docker services\n", len(state.MappingResults))
		fmt.Println()
	}

	// Confirm to proceed
	if !wizardYes {
		proceed := ui.PromptYesNo("Continue with these resources?", true)
		if !proceed {
			return fmt.Errorf("wizard cancelled by user")
		}
	}

	if !IsQuiet() {
		ui.Success("Resources confirmed")
	}

	return nil
}

// wizardStep3ConfigureExport handles export configuration
func wizardStep3ConfigureExport(state *WizardState) error {
	displayStepHeader(3, 5, "Configure Export")

	// Domain configuration
	if wizardDomain != "" {
		state.Domain = wizardDomain
	} else if !wizardYes {
		domain, err := ui.Prompt("Enter domain name (or leave empty)")
		if err != nil {
			return fmt.Errorf("failed to get domain: %w", err)
		}
		state.Domain = domain
	}

	// Consolidation
	if !wizardYes {
		state.Consolidate = ui.PromptYesNo("Consolidate similar resources into unified stacks?", true)
	} else {
		state.Consolidate = true // Default in non-interactive mode
	}

	// Secret detection
	if !wizardYes {
		state.DetectSecrets = ui.PromptYesNo("Detect and create secret references?", true)
	} else {
		state.DetectSecrets = true // Default in non-interactive mode
	}

	// Perform consolidation if enabled
	if state.Consolidate && len(state.MappingResults) > 0 {
		if !IsQuiet() {
			fmt.Println()
			fmt.Println("  Consolidating stacks...")
		}

		ctx := context.Background()
		cons := consolidator.New()
		opts := consolidator.DefaultOptions()

		var err error
		state.Consolidated, err = cons.Consolidate(ctx, state.MappingResults, opts)
		if err != nil {
			if IsVerbose() {
				ui.Warning(fmt.Sprintf("Consolidation failed: %v", err))
			}
			state.Consolidate = false
		} else if !IsQuiet() {
			fmt.Printf("  Consolidated into %d stacks with %d services\n",
				state.Consolidated.TotalStacks(), state.Consolidated.TotalServices())
		}
	}

	// Detect secrets if enabled
	if state.DetectSecrets {
		state.SecretRefs = detectSecretReferences(state.Analysis, state.MappingResults)
		if !IsQuiet() && len(state.SecretRefs) > 0 {
			fmt.Printf("  Detected %d secret references\n", len(state.SecretRefs))
		}
	}

	// Output path
	if wizardOutput != "" {
		state.BundlePath = wizardOutput
	} else if !wizardYes {
		defaultName := fmt.Sprintf("migration-%s.hprt", time.Now().Format("20060102-150405"))
		path, err := ui.Prompt(fmt.Sprintf("Output bundle path [%s]", defaultName))
		if err != nil {
			return fmt.Errorf("failed to get output path: %w", err)
		}
		if path == "" {
			path = defaultName
		}
		state.BundlePath = path
	} else {
		state.BundlePath = fmt.Sprintf("migration-%s.hprt", time.Now().Format("20060102-150405"))
	}

	// Ensure .hprt extension
	if !strings.HasSuffix(state.BundlePath, ".hprt") {
		state.BundlePath += ".hprt"
	}

	if !IsQuiet() {
		fmt.Println()
		ui.Success("Configuration complete")
	}

	return nil
}

// wizardStep4ExportBundle creates the .hprt bundle
func wizardStep4ExportBundle(state *WizardState) error {
	displayStepHeader(4, 5, "Export Bundle")

	// Create temp directory for bundle contents
	tempDir, err := os.MkdirTemp("", "homeport-wizard-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	state.OutputDir = tempDir

	totalSteps := 5
	currentStep := 0

	// Step 1: Create directory structure
	currentStep++
	if !IsQuiet() {
		fmt.Printf("  %s\n", ui.SimpleProgress(currentStep, totalSteps, "Creating directory structure"))
	}

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
		if err := os.MkdirAll(filepath.Join(tempDir, dir), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Step 2: Generate files
	currentStep++
	if !IsQuiet() {
		fmt.Printf("  %s\n", ui.SimpleProgress(currentStep, totalSteps, "Generating migration files"))
	}

	config := &MigrationConfig{
		InputPath:         state.SourcePath,
		OutputPath:        tempDir,
		Domain:            state.Domain,
		IncludeMigration:  true,
		IncludeMonitoring: false,
		Consolidate:       state.Consolidate,
	}

	if err := generateExportFiles(tempDir, state.Analysis, state.MappingResults, state.Consolidated); err != nil {
		return fmt.Errorf("failed to generate files: %w", err)
	}

	// Step 3: Write secrets manifest
	currentStep++
	if !IsQuiet() {
		fmt.Printf("  %s\n", ui.SimpleProgress(currentStep, totalSteps, "Creating secrets manifest"))
	}

	if state.DetectSecrets && len(state.SecretRefs) > 0 {
		if err := writeSecretsManifest(tempDir, state.SecretRefs); err != nil {
			return fmt.Errorf("failed to write secrets manifest: %w", err)
		}
	}

	// Also generate env template
	if err := generateEnvTemplate(config, state.Analysis); err != nil {
		return fmt.Errorf("failed to generate env template: %w", err)
	}

	// Step 4: Create bundle archive
	currentStep++
	if !IsQuiet() {
		fmt.Printf("  %s\n", ui.SimpleProgress(currentStep, totalSteps, "Creating bundle archive"))
	}

	exporter := infraBundle.NewExporter(version.Version)

	// Determine target host - default to "local" if not specified
	targetHost := "local"
	if wizardTarget != "" {
		targetHost = wizardTarget
	}

	exportOpts := infraBundle.ExportOptions{
		OutputPath:     state.BundlePath,
		SourceProvider: detectProvider(state.Analysis),
		SourceRegion:   wizardRegion,
		ResourceCount:  state.Analysis.Statistics.TotalResources,
		TargetType:     "docker-compose",
		TargetHost:     targetHost,
		Domain:         state.Domain,
		Consolidation:  state.Consolidate,
		DetectSecrets:  state.DetectSecrets,
	}

	if err := exporter.Export(tempDir, exportOpts); err != nil {
		return fmt.Errorf("failed to create bundle: %w", err)
	}

	// Step 5: Validate bundle
	currentStep++
	if !IsQuiet() {
		fmt.Printf("  %s\n", ui.SimpleProgress(currentStep, totalSteps, "Validating bundle"))
	}

	if err := exporter.ValidateExport(state.BundlePath); err != nil {
		return fmt.Errorf("bundle validation failed: %w", err)
	}

	// Get bundle info
	fileInfo, _ := os.Stat(state.BundlePath)
	if !IsQuiet() {
		fmt.Println()
		fmt.Printf("  Bundle created: %s\n", state.BundlePath)
		if fileInfo != nil {
			fmt.Printf("  Size: %s\n", formatBytesHuman(fileInfo.Size()))
		}
		fmt.Println()
		ui.Success("Bundle exported successfully")
	}

	return nil
}

// wizardStep5Deploy handles optional deployment
func wizardStep5Deploy(state *WizardState) error {
	displayStepHeader(5, 5, "Deploy (optional)")

	// Check if we should deploy
	if wizardTarget != "" {
		state.DeployTarget = wizardTarget
		state.Deploy = true
	} else if !wizardYes {
		state.Deploy = ui.PromptYesNo("Deploy the bundle now?", false)

		if state.Deploy {
			deployOptions := []string{
				"Local (this machine)",
				"Remote (SSH)",
			}

			idx, err := ui.PromptSelect("Deploy target", deployOptions)
			if err != nil {
				return fmt.Errorf("failed to select deploy target: %w", err)
			}

			if idx == 0 {
				state.DeployTarget = "local"
			} else {
				target, err := ui.Prompt("Enter SSH target (user@host)")
				if err != nil {
					return fmt.Errorf("failed to get SSH target: %w", err)
				}
				state.DeployTarget = target
			}
		}
	}

	if !state.Deploy {
		if !IsQuiet() {
			fmt.Println("  Skipping deployment.")
			fmt.Println()
			fmt.Println("  To deploy later, run:")
			fmt.Printf("    homeport import bundle %s --deploy\n", state.BundlePath)
			fmt.Println()
		}
		return nil
	}

	// Import and deploy the bundle
	if !IsQuiet() {
		fmt.Println()
		fmt.Println("  Importing and deploying bundle...")
		fmt.Println()
	}

	importer := infraBundle.NewImporter(version.Version)

	// Determine output directory for extraction
	bundleName := strings.TrimSuffix(filepath.Base(state.BundlePath), ".hprt")
	outputDir := filepath.Join(".", bundleName+"-deploy")

	importOpts := infraBundle.ImportOptions{
		OutputDir:           outputDir,
		TargetHost:          extractHost(state.DeployTarget),
		TargetUser:          extractUser(state.DeployTarget),
		SkipValidation:      false,
		SkipDependencyCheck: false,
		DryRun:              false,
		Deploy:              true,
	}

	// Handle local deployment
	if state.DeployTarget == "local" {
		importOpts.TargetHost = ""
		importOpts.TargetUser = ""
	}

	result, err := importer.Import(state.BundlePath, importOpts)
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	// Handle missing secrets
	if len(result.MissingSecrets) > 0 && !wizardYes {
		if !IsQuiet() {
			fmt.Println()
			ui.Warning("Some secrets are required. Please provide them now.")
			fmt.Println()
		}

		if err := resolveSecretsInteractively(result, outputDir); err != nil {
			return fmt.Errorf("failed to resolve secrets: %w", err)
		}
	}

	// Deploy
	if state.DeployTarget == "local" || state.DeployTarget == "" {
		if err := deployLocal(result.ExtractedTo); err != nil {
			return fmt.Errorf("local deployment failed: %w", err)
		}
	} else if importOpts.TargetHost != "" {
		if err := deployRemote(result.ExtractedTo, importOpts); err != nil {
			return fmt.Errorf("remote deployment failed: %w", err)
		}
	}

	if !IsQuiet() {
		fmt.Println()
		ui.Success("Deployment complete")
	}

	return nil
}

// displayWizardSummary shows the final summary
func displayWizardSummary(state *WizardState) {
	if IsQuiet() {
		return
	}

	ui.Divider()
	fmt.Println()
	ui.Success("Migration Wizard Complete!")
	fmt.Println()

	fmt.Println("  Summary:")
	fmt.Printf("    Source:            %s\n", state.SourcePath)
	fmt.Printf("    Resources found:   %d\n", state.Analysis.Statistics.TotalResources)
	fmt.Printf("    Resources mapped:  %d\n", len(state.MappingResults))

	if state.Consolidate && state.Consolidated != nil {
		fmt.Printf("    Consolidated to:   %d stacks\n", state.Consolidated.TotalStacks())
	}

	fmt.Printf("    Bundle:            %s\n", state.BundlePath)

	if state.Domain != "" {
		fmt.Printf("    Domain:            %s\n", state.Domain)
	}

	if state.Deploy {
		fmt.Printf("    Deployed to:       %s\n", state.DeployTarget)
	}

	fmt.Println()

	// Show next steps
	if !state.Deploy {
		fmt.Println("  Next steps:")
		fmt.Printf("    1. Transfer bundle to target server: scp %s user@server:/path/\n", state.BundlePath)
		fmt.Printf("    2. Import and deploy: homeport import bundle %s --deploy\n", filepath.Base(state.BundlePath))
		fmt.Println("    3. Provide secrets when prompted")
		fmt.Println("    4. Run cutover when ready: homeport cutover --bundle <bundle>")
		fmt.Println()
	} else {
		fmt.Println("  Next steps:")
		fmt.Println("    1. Monitor your services for any issues")
		fmt.Println("    2. Configure DNS records for your domain")
		fmt.Println("    3. Run cutover when ready: homeport cutover --bundle <bundle>")
		fmt.Println()
	}

	ui.Divider()
}

// formatBytesHuman formats bytes to human readable format
// Note: This uses a different name from backup.go's formatBytes to avoid redeclaration
func formatBytesHuman(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
