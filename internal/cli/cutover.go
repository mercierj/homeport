package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	domainBundle "github.com/homeport/homeport/internal/domain/bundle"
	"github.com/homeport/homeport/internal/domain/cutover"
	infraBundle "github.com/homeport/homeport/internal/infrastructure/bundle"
	infraCutover "github.com/homeport/homeport/internal/infrastructure/cutover"
	"github.com/homeport/homeport/internal/infrastructure/cutover/dns"
	"github.com/spf13/cobra"
)

var (
	cutoverBundlePath   string
	cutoverDryRun       bool
	cutoverDNSProvider  string
	cutoverManual       bool
	cutoverRollback     bool
	cutoverTimeout      time.Duration
	cutoverSkipPreCheck bool
	cutoverAPIToken     string
	cutoverZoneID       string
)

// cutoverCmd represents the cutover command
var cutoverCmd = &cobra.Command{
	Use:   "cutover",
	Short: "Execute migration cutover (DNS switch)",
	Long: `Execute the final cutover from cloud infrastructure to self-hosted.

The cutover command handles the critical DNS switch that redirects traffic
from your cloud infrastructure to the self-hosted Docker stack. It includes:

  - Pre-cutover health checks (validate target is ready)
  - DNS record changes (switch traffic to new infrastructure)
  - Post-cutover validation (verify the migration succeeded)
  - Automatic rollback on failure

Examples:
  # Execute cutover plan from bundle
  homeport cutover --bundle migration.hprt

  # Dry run (show what would change without executing)
  homeport cutover --bundle migration.hprt --dry-run

  # Use Cloudflare for DNS changes
  homeport cutover --bundle migration.hprt --dns-provider cloudflare \
    --api-token CF_TOKEN --zone-id ZONE_ID

  # Use AWS Route 53 for DNS changes
  homeport cutover --bundle migration.hprt --dns-provider route53 \
    --zone-id HOSTED_ZONE_ID

  # Manual mode (generate instructions without executing)
  homeport cutover --bundle migration.hprt --manual

  # Rollback a previous cutover
  homeport cutover --bundle migration.hprt --rollback

  # Skip pre-cutover health checks
  homeport cutover --bundle migration.hprt --skip-pre-check`,
	RunE: runCutover,
}

func init() {
	rootCmd.AddCommand(cutoverCmd)

	cutoverCmd.Flags().StringVarP(&cutoverBundlePath, "bundle", "b", "", "path to .hprt bundle file (required)")
	cutoverCmd.Flags().BoolVar(&cutoverDryRun, "dry-run", false, "simulate cutover without making changes")
	cutoverCmd.Flags().StringVar(&cutoverDNSProvider, "dns-provider", "manual", "DNS provider (manual, cloudflare, route53)")
	cutoverCmd.Flags().BoolVar(&cutoverManual, "manual", false, "generate manual instructions instead of executing")
	cutoverCmd.Flags().BoolVar(&cutoverRollback, "rollback", false, "rollback a previous cutover")
	cutoverCmd.Flags().DurationVar(&cutoverTimeout, "timeout", 30*time.Minute, "maximum time for cutover execution")
	cutoverCmd.Flags().BoolVar(&cutoverSkipPreCheck, "skip-pre-check", false, "skip pre-cutover health checks")
	cutoverCmd.Flags().StringVar(&cutoverAPIToken, "api-token", "", "DNS provider API token")
	cutoverCmd.Flags().StringVar(&cutoverZoneID, "zone-id", "", "DNS zone ID (Cloudflare zone ID or Route53 hosted zone ID)")

	_ = cutoverCmd.MarkFlagRequired("bundle")
}

func runCutover(cmd *cobra.Command, args []string) error {
	if !IsQuiet() {
		ui.Header("Homeport - Migration Cutover")
		ui.Info(fmt.Sprintf("Bundle: %s", cutoverBundlePath))
		if cutoverDryRun {
			ui.Warning("DRY RUN MODE - No changes will be made")
		}
		if cutoverRollback {
			ui.Warning("ROLLBACK MODE - Reverting previous cutover")
		}
		ui.Divider()
	}

	ctx, cancel := context.WithTimeout(context.Background(), cutoverTimeout)
	defer cancel()

	// Step 1: Load the bundle
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(1, 5, "Loading bundle"))
	}

	plan, err := loadCutoverPlan(cutoverBundlePath)
	if err != nil {
		return fmt.Errorf("failed to load cutover plan: %w", err)
	}

	if IsVerbose() {
		ui.Info(fmt.Sprintf("Loaded cutover plan with %d DNS changes", len(plan.DNSChanges)))
		ui.Info(fmt.Sprintf("Pre-checks: %d, Post-checks: %d", len(plan.PreChecks), len(plan.PostChecks)))
	}

	// Step 2: Validate the plan
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(2, 5, "Validating cutover plan"))
	}

	if errs := plan.Validate(); len(errs) > 0 {
		for _, e := range errs {
			ui.Error(e)
		}
		return fmt.Errorf("cutover plan validation failed")
	}

	// Step 3: Set up DNS provider
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(3, 5, "Configuring DNS provider"))
	}

	orchestrator := infraCutover.NewOrchestrator()

	// Register DNS providers
	if err := setupDNSProviders(orchestrator); err != nil {
		return fmt.Errorf("failed to set up DNS provider: %w", err)
	}

	// Build orchestrator options
	opts := &infraCutover.OrchestratorOptions{
		DryRun:      cutoverDryRun,
		DNSProvider: cutoverDNSProvider,
		Manual:      cutoverManual,
		Timeout:     cutoverTimeout,
		Verbose:     IsVerbose(),
		OnStepStart: func(step *cutover.CutoverStep) {
			if IsVerbose() {
				ui.Info(fmt.Sprintf("Starting: %s", step.Description))
			}
		},
		OnStepComplete: func(step *cutover.CutoverStep) {
			if step.Status == cutover.CutoverStepStatusCompleted {
				if IsVerbose() {
					ui.Success(fmt.Sprintf("Completed: %s", step.Description))
				}
			} else if step.Status == cutover.CutoverStepStatusFailed {
				ui.Error(fmt.Sprintf("Failed: %s - %s", step.Description, step.Error))
			}
		},
		OnProgress: func(current, total int, message string) {
			if !IsQuiet() {
				fmt.Printf("\r%s", ui.SimpleProgress(current, total, message))
			}
		},
	}

	// Skip pre-checks if requested
	if cutoverSkipPreCheck {
		plan.PreChecks = nil
		plan.BuildSteps()
	}

	// Step 4: Execute cutover (or rollback)
	if !IsQuiet() {
		if cutoverRollback {
			fmt.Println(ui.SimpleProgress(4, 5, "Executing rollback"))
		} else {
			fmt.Println(ui.SimpleProgress(4, 5, "Executing cutover"))
		}
	}

	var result *infraCutover.ExecutionResult

	if cutoverRollback {
		err = orchestrator.Rollback(ctx, plan, opts)
		if err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
		result = &infraCutover.ExecutionResult{
			Plan:       plan,
			Success:    true,
			RolledBack: true,
		}
	} else {
		result, err = orchestrator.Execute(ctx, plan, opts)
		if err != nil {
			return fmt.Errorf("cutover failed: %w", err)
		}
	}

	// Step 5: Display results
	if !IsQuiet() {
		fmt.Println(ui.SimpleProgress(5, 5, "Generating report"))
	}

	displayCutoverResults(result)

	return nil
}

// loadCutoverPlan loads a cutover plan from a bundle file.
func loadCutoverPlan(bundlePath string) (*cutover.CutoverPlan, error) {
	// Check if the bundle file exists
	if _, err := os.Stat(bundlePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("bundle file not found: %s", bundlePath)
	}

	// Load the bundle
	archiver := infraBundle.NewArchiver()
	bundle, err := archiver.ExtractArchive(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract bundle: %w", err)
	}

	// Look for cutover plan in the bundle
	// Check dns/cutover.json first
	var planData []byte
	if file, ok := bundle.GetFile("dns/cutover.json"); ok {
		planData = file.Content
	} else if file, ok := bundle.GetFile("validation/cutover.json"); ok {
		planData = file.Content
	}

	if planData != nil {
		var plan cutover.CutoverPlan
		if err := json.Unmarshal(planData, &plan); err != nil {
			return nil, fmt.Errorf("failed to parse cutover plan: %w", err)
		}
		plan.BuildSteps()
		return &plan, nil
	}

	// If no explicit cutover plan, try to build one from DNS records and endpoints
	plan, err := buildCutoverPlanFromBundle(bundle)
	if err != nil {
		return nil, fmt.Errorf("failed to build cutover plan: %w", err)
	}

	return plan, nil
}

// buildCutoverPlanFromBundle creates a cutover plan from bundle components.
func buildCutoverPlanFromBundle(bundle *domainBundle.Bundle) (*cutover.CutoverPlan, error) {
	plan := cutover.NewCutoverPlan("cutover-"+time.Now().Format("20060102-150405"), bundle.Manifest.Source.Provider)

	// Determine if this is a local deployment (no domain configured)
	isLocalDeployment := bundle.Manifest.Target == nil ||
		bundle.Manifest.Target.Host == "" ||
		bundle.Manifest.Target.Host == "local" ||
		bundle.Manifest.Target.Host == "localhost"

	// Get domain from manifest if available
	domain := ""
	if bundle.Manifest.Target != nil && bundle.Manifest.Target.Domain != "" {
		domain = bundle.Manifest.Target.Domain
	}

	// Load DNS records from bundle
	if file, ok := bundle.GetFile("dns/records.json"); ok {
		var dnsChanges []*cutover.DNSChange
		if err := json.Unmarshal(file.Content, &dnsChanges); err != nil {
			return nil, fmt.Errorf("failed to parse DNS records: %w", err)
		}
		for _, change := range dnsChanges {
			plan.AddDNSChange(change)
		}
	}

	// Load health check endpoints from bundle
	if file, ok := bundle.GetFile("validation/endpoints.json"); ok {
		var endpoints []struct {
			Name           string `json:"name"`
			URL            string `json:"url"`
			ExpectedStatus int    `json:"expected_status"`
			Type           string `json:"type"`
		}
		if err := json.Unmarshal(file.Content, &endpoints); err != nil {
			return nil, fmt.Errorf("failed to parse endpoints: %w", err)
		}

		for i, ep := range endpoints {
			checkType := cutover.HealthCheckHTTP
			if ep.Type == "tcp" {
				checkType = cutover.HealthCheckTCP
			}

			// For local deployments, replace domain-based URLs with localhost
			url := ep.URL
			if isLocalDeployment && domain != "" {
				url = strings.ReplaceAll(url, domain, "localhost")
				url = strings.ReplaceAll(url, "https://", "http://")
			}

			check := cutover.NewHealthCheck(
				fmt.Sprintf("check-%d", i+1),
				ep.Name,
				checkType,
				url,
			)
			check.ExpectedStatus = ep.ExpectedStatus
			plan.AddPostCheck(check)
		}
	}

	// Load rollback triggers
	if file, ok := bundle.GetFile("validation/rollback-triggers.json"); ok {
		var triggers []*cutover.RollbackTrigger
		if err := json.Unmarshal(file.Content, &triggers); err != nil {
			return nil, fmt.Errorf("failed to parse rollback triggers: %w", err)
		}
		for _, trigger := range triggers {
			plan.AddRollbackTrigger(trigger)
		}
	}

	// For local deployments without DNS changes, create localhost-based health checks instead
	if len(plan.DNSChanges) == 0 {
		if isLocalDeployment {
			// For local deployment, add localhost health checks instead of DNS changes
			// Add a pre-check for Traefik
			preCheck := cutover.NewHTTPHealthCheck(
				"pre-1",
				"Traefik health check",
				"http://localhost:8080/ping",
				200,
			)
			plan.AddPreCheck(preCheck)

			// Add post-check for services
			postCheck := cutover.NewHTTPHealthCheck(
				"post-1",
				"Local services health check",
				"http://localhost/health",
				200,
			)
			postCheck.SkipTLSVerify = true
			plan.AddPostCheck(postCheck)

			// For local deployments, we don't need DNS changes
			// Create a placeholder that indicates local deployment
			plan.Name = "Local Deployment Validation"
			plan.Description = "Validates local Docker deployment without DNS changes"
		} else if bundle.Manifest.Target != nil && bundle.Manifest.Target.Host != "" && domain != "" {
			// Remote deployment with domain - create DNS change
			change := cutover.NewDNSChange(
				"dns-1",
				domain,
				"A",
				"@",
				"<current-cloud-ip>",
				bundle.Manifest.Target.Host,
			)
			change.TTL = 300
			plan.AddDNSChange(change)
		} else if bundle.Manifest.Target != nil && bundle.Manifest.Target.Host != "" {
			// Remote deployment without domain - skip DNS, just do health checks
			preCheck := cutover.NewHTTPHealthCheck(
				"pre-1",
				"Target server health check",
				fmt.Sprintf("http://%s:8080/ping", bundle.Manifest.Target.Host),
				200,
			)
			plan.AddPreCheck(preCheck)

			postCheck := cutover.NewHTTPHealthCheck(
				"post-1",
				"Services health check",
				fmt.Sprintf("http://%s/health", bundle.Manifest.Target.Host),
				200,
			)
			postCheck.SkipTLSVerify = true
			plan.AddPostCheck(postCheck)

			plan.Name = "Remote Deployment Validation (No DNS)"
			plan.Description = "Validates remote Docker deployment without DNS changes"
		} else {
			return nil, fmt.Errorf("no DNS changes found in bundle and no target host specified")
		}
	}

	plan.BuildSteps()
	return plan, nil
}

// setupDNSProviders configures DNS providers based on command flags.
func setupDNSProviders(orchestrator *infraCutover.Orchestrator) error {
	// Always register manual provider
	orchestrator.RegisterDNSProvider("manual", dns.NewManualProvider())

	// Get API token from flag or environment
	apiToken := cutoverAPIToken
	if apiToken == "" {
		switch cutoverDNSProvider {
		case "cloudflare":
			apiToken = os.Getenv("CLOUDFLARE_API_TOKEN")
			if apiToken == "" {
				apiToken = os.Getenv("CF_API_TOKEN")
			}
		case "route53":
			// AWS uses environment credentials
		}
	}

	// Get zone ID from flag or environment
	zoneID := cutoverZoneID
	if zoneID == "" {
		switch cutoverDNSProvider {
		case "cloudflare":
			zoneID = os.Getenv("CLOUDFLARE_ZONE_ID")
			if zoneID == "" {
				zoneID = os.Getenv("CF_ZONE_ID")
			}
		case "route53":
			zoneID = os.Getenv("AWS_HOSTED_ZONE_ID")
		}
	}

	// Register requested provider
	switch cutoverDNSProvider {
	case "manual":
		// Already registered
	case "cloudflare":
		if apiToken == "" {
			return fmt.Errorf("cloudflare requires --api-token or CLOUDFLARE_API_TOKEN environment variable")
		}
		if zoneID == "" {
			return fmt.Errorf("cloudflare requires --zone-id or CLOUDFLARE_ZONE_ID environment variable")
		}
		provider := dns.NewCloudflareProvider(&dns.CloudflareConfig{
			APIToken: apiToken,
			ZoneID:   zoneID,
		})
		orchestrator.RegisterDNSProvider("cloudflare", provider)

	case "route53":
		if zoneID == "" {
			return fmt.Errorf("route53 requires --zone-id or AWS_HOSTED_ZONE_ID environment variable")
		}
		provider := dns.NewRoute53Provider(&dns.Route53Config{
			HostedZoneID:    zoneID,
			Region:          os.Getenv("AWS_REGION"),
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		})
		orchestrator.RegisterDNSProvider("route53", provider)

	default:
		return fmt.Errorf("unsupported DNS provider: %s (supported: manual, cloudflare, route53)", cutoverDNSProvider)
	}

	return nil
}

// displayCutoverResults shows the cutover execution results.
func displayCutoverResults(result *infraCutover.ExecutionResult) {
	if result == nil {
		return
	}

	ui.Divider()

	// Show manual instructions if in manual mode
	if len(result.ManualInstructions) > 0 {
		ui.Header("Manual Cutover Instructions")
		fmt.Println()
		for _, line := range result.ManualInstructions {
			fmt.Println(line)
		}
		return
	}

	// Show execution summary
	if result.Success {
		if result.RolledBack {
			ui.Success("Rollback completed successfully")
		} else {
			ui.Success("Cutover completed successfully")
		}
	} else {
		ui.Error("Cutover failed")
		if result.Error != nil {
			ui.Error(fmt.Sprintf("Error: %v", result.Error))
		}
	}

	fmt.Println()

	// Show statistics
	fmt.Println("Execution Summary:")
	fmt.Printf("  Duration:        %s\n", result.Duration.Round(time.Second))
	fmt.Printf("  Steps completed: %d/%d\n", result.StepsCompleted, len(result.Plan.Steps))
	fmt.Printf("  Steps failed:    %d\n", result.StepsFailed)

	if result.Plan.DryRun {
		fmt.Println()
		ui.Warning("This was a dry run. No actual changes were made.")
	}

	// Show DNS changes summary
	if len(result.Plan.DNSChanges) > 0 {
		fmt.Println()
		fmt.Println("DNS Changes:")
		table := ui.NewTable([]string{"Domain", "Type", "Old Value", "New Value", "Status"})
		for _, change := range result.Plan.DNSChanges {
			status := string(change.Status)
			if change.Status == cutover.DNSChangeStatusApplied {
				status = "Applied"
			} else if change.Status == cutover.DNSChangeStatusRolledBack {
				status = "Rolled Back"
			}
			table.AddRow([]string{
				change.FullName(),
				change.RecordType,
				truncateString(change.OldValue, 20),
				truncateString(change.NewValue, 20),
				status,
			})
		}
		fmt.Println(table.Render())
	}

	// Show logs
	if IsVerbose() && len(result.Logs) > 0 {
		fmt.Println()
		fmt.Println("Execution Log:")
		for _, log := range result.Logs {
			fmt.Printf("  %s\n", log)
		}
	}

	// Show next steps
	if result.Success && !result.Plan.DryRun && !result.RolledBack {
		fmt.Println()
		ui.Info("Next steps:")
		fmt.Println("  1. Wait for DNS propagation (check with: dig +short <domain>)")
		fmt.Println("  2. Monitor your services for any issues")
		fmt.Println("  3. If problems occur, run: homeport cutover --bundle <bundle> --rollback")
	}

	// Show rollback instructions if failed
	if !result.Success && !result.RolledBack {
		fmt.Println()
		ui.Warning("To rollback, run:")
		fmt.Printf("  homeport cutover --bundle %s --rollback\n", cutoverBundlePath)
	}
}


// createSampleCutoverPlan creates a sample cutover plan for testing.
// This is exported for use in tests.
func createSampleCutoverPlan() *cutover.CutoverPlan {
	plan := cutover.NewCutoverPlan("sample-cutover", "sample-bundle")
	plan.Name = "Sample Local Deployment Validation"
	plan.Description = "Sample cutover plan for local Docker deployment"

	// Add a pre-check for Traefik
	preCheck := cutover.NewHTTPHealthCheck(
		"pre-1",
		"Traefik health check",
		"http://localhost:8080/ping",
		200,
	)
	plan.AddPreCheck(preCheck)

	// Add a post-check for services (localhost-based for local deployments)
	postCheck := cutover.NewHTTPHealthCheck(
		"post-1",
		"Local services health check",
		"http://localhost/health",
		200,
	)
	postCheck.SkipTLSVerify = true
	plan.AddPostCheck(postCheck)

	// Add rollback trigger
	trigger := cutover.NewHealthCheckTrigger("trigger-1", "post-1", 3, true)
	plan.AddRollbackTrigger(trigger)

	plan.BuildSteps()
	return plan
}

// Helper function to parse comma-separated list
func parseStringList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
