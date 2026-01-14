package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/provider"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/target"
	"github.com/spf13/cobra"
)

var (
	// providers compare flags
	compareInput      string
	compareOutput     string
	compareHALevel    string
	compareSourcePath string
)

// providersCmd represents the providers parent command
var providersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage and compare cloud providers",
	Long: `Manage and compare cloud providers for migration.

This command allows you to list available EU providers and compare
costs across different providers for your infrastructure.

Examples:
  # List all available providers
  homeport providers list

  # Compare costs from an analysis file
  homeport providers compare --input analysis.json

  # Compare costs from Terraform source
  homeport providers compare --source ./terraform --ha-level multi-server`,
}

// providersListCmd lists all available providers
var providersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available target providers",
	Long: `List all available target providers for migration.

Displays information about each EU provider including:
  - Provider name and display name
  - Available regions and locations
  - HA levels supported
  - Required credentials`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !IsQuiet() {
			ui.Header("Available Target Providers")
			ui.Divider()
		}

		// Get all supported EU providers
		providers := provider.SupportedProviders()

		for _, p := range providers {
			info := provider.GetProviderInfo(p)
			if info == nil {
				continue
			}

			// Get generator for this provider
			platform, ok := providerToPlatform(p)
			if !ok {
				continue
			}

			gen, err := generator.GetGenerator(platform)
			if err != nil {
				continue
			}

			if !IsQuiet() {
				fmt.Println()
				ui.Info(fmt.Sprintf("%s (%s)", info.DisplayName, string(p)))

				// Show regions
				fmt.Println("  Regions:")
				for _, r := range info.Regions {
					fmt.Printf("    - %s (%s, %s)\n", r.ID, r.Name, r.Location)
				}

				// Show HA levels
				haLevels := gen.SupportedHALevels()
				haStrs := make([]string, len(haLevels))
				for i, ha := range haLevels {
					haStrs[i] = string(ha)
				}
				fmt.Printf("  HA Levels: %s\n", strings.Join(haStrs, ", "))

				// Show required credentials
				if gen.RequiresCredentials() {
					creds := gen.RequiredCredentials()
					fmt.Printf("  Credentials: %s\n", strings.Join(creds, ", "))
				}

				// Show pricing info
				pricing := provider.GetProviderPricing(p)
				if pricing != nil && len(pricing.Instances) > 0 {
					lowestPrice := pricing.Instances[0].PricePerMonth
					for _, inst := range pricing.Instances {
						if inst.PricePerMonth < lowestPrice {
							lowestPrice = inst.PricePerMonth
						}
					}
					fmt.Printf("  Starting from: %.2f %s/month\n", lowestPrice, pricing.Instances[0].Currency)

					// Show egress info
					if pricing.Network.EgressPricePerGB == 0 {
						fmt.Println("  Egress: Unlimited free")
					} else if pricing.Network.FreeEgressGB > 0 {
						fmt.Printf("  Egress: %d GB free, then %.3f %s/GB\n",
							pricing.Network.FreeEgressGB,
							pricing.Network.EgressPricePerGB,
							pricing.Network.Currency)
					}
				}
			}
		}

		// Show as JSON if quiet mode
		if IsQuiet() {
			type providerJSON struct {
				ID          string   `json:"id"`
				DisplayName string   `json:"display_name"`
				Regions     []string `json:"regions"`
				HALevels    []string `json:"ha_levels"`
				Credentials []string `json:"credentials,omitempty"`
			}

			var output []providerJSON
			for _, p := range providers {
				info := provider.GetProviderInfo(p)
				if info == nil {
					continue
				}

				platform, ok := providerToPlatform(p)
				if !ok {
					continue
				}

				gen, err := generator.GetGenerator(platform)
				if err != nil {
					continue
				}

				regions := make([]string, len(info.Regions))
				for i, r := range info.Regions {
					regions[i] = r.ID
				}

				haLevels := gen.SupportedHALevels()
				haStrs := make([]string, len(haLevels))
				for i, ha := range haLevels {
					haStrs[i] = string(ha)
				}

				pj := providerJSON{
					ID:          string(p),
					DisplayName: info.DisplayName,
					Regions:     regions,
					HALevels:    haStrs,
				}

				if gen.RequiresCredentials() {
					pj.Credentials = gen.RequiredCredentials()
				}

				output = append(output, pj)
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(output)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Found %d available providers", len(providers)))
		}

		return nil
	},
}

// providersCompareCmd compares costs across providers
var providersCompareCmd = &cobra.Command{
	Use:   "compare",
	Short: "Compare costs across providers",
	Long: `Compare estimated costs across different EU providers.

This command analyzes your infrastructure and estimates costs for
deploying on each available provider (Hetzner, Scaleway, OVH).

The comparison can be done from:
  - An analysis.json file (from 'homeport analyze' command)
  - A Terraform source directory

Examples:
  # Compare from analysis file
  homeport providers compare --input analysis.json

  # Compare from Terraform source
  homeport providers compare --source ./terraform

  # Compare with specific HA level
  homeport providers compare --source ./terraform --ha-level cluster

  # Output as JSON
  homeport providers compare --input analysis.json --output json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate inputs
		if compareInput == "" && compareSourcePath == "" {
			return fmt.Errorf("either --input or --source is required")
		}

		// Parse HA level
		haLevel := target.HALevelBasic
		if compareHALevel != "" {
			parsed, ok := target.ParseHALevel(compareHALevel)
			if !ok {
				return fmt.Errorf("invalid HA level: %s (valid: none, basic, multi-server, cluster)", compareHALevel)
			}
			haLevel = parsed
		}

		if !IsQuiet() {
			ui.Header("Provider Cost Comparison")
			ui.Info(fmt.Sprintf("HA Level: %s", haLevel))
			ui.Divider()
		}

		// Get mapping results from source
		var mappingResults []*mapper.MappingResult
		var resourceCount int

		if compareSourcePath != "" {
			// Analyze from source
			analysis, err := performAnalysis(compareSourcePath)
			if err != nil {
				return fmt.Errorf("failed to analyze source: %w", err)
			}
			resourceCount = analysis.Statistics.TotalResources

			// Convert to mapping results
			ctx := context.Background()
			for _, resSummary := range analysis.Resources {
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
					mappingResults = append(mappingResults, result)
				}
			}
		} else if compareInput != "" {
			// Load from analysis file
			data, err := os.ReadFile(compareInput)
			if err != nil {
				return fmt.Errorf("failed to read input file: %w", err)
			}

			var analysis AnalysisResult
			if err := json.Unmarshal(data, &analysis); err != nil {
				return fmt.Errorf("failed to parse analysis file: %w", err)
			}
			resourceCount = analysis.Statistics.TotalResources

			// Convert to mapping results
			ctx := context.Background()
			for _, resSummary := range analysis.Resources {
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
					mappingResults = append(mappingResults, result)
				}
			}
		}

		if !IsQuiet() {
			ui.Info(fmt.Sprintf("Analyzing %d resources...", resourceCount))
			fmt.Println()
		}

		// Estimate costs for each provider
		type providerEstimate struct {
			Provider    provider.Provider
			DisplayName string
			Estimate    *generator.CostEstimate
			Supported   bool
			Error       string
		}

		var estimates []providerEstimate

		for _, p := range provider.SupportedProviders() {
			info := provider.GetProviderInfo(p)
			if info == nil {
				continue
			}

			platform, ok := providerToPlatform(p)
			if !ok {
				continue
			}

			gen, err := generator.GetGenerator(platform)
			if err != nil {
				estimates = append(estimates, providerEstimate{
					Provider:    p,
					DisplayName: info.DisplayName,
					Supported:   false,
					Error:       "Generator not available",
				})
				continue
			}

			// Check if HA level is supported
			haSupported := false
			for _, supported := range gen.SupportedHALevels() {
				if supported == haLevel {
					haSupported = true
					break
				}
			}

			if !haSupported {
				estimates = append(estimates, providerEstimate{
					Provider:    p,
					DisplayName: info.DisplayName,
					Supported:   false,
					Error:       fmt.Sprintf("HA level %s not supported", haLevel),
				})
				continue
			}

			// Create config for estimation
			config := generator.NewTargetConfig(platform)
			config.WithHALevel(haLevel)

			// Estimate cost
			estimate, err := gen.EstimateCost(mappingResults, config)
			if err != nil {
				estimates = append(estimates, providerEstimate{
					Provider:    p,
					DisplayName: info.DisplayName,
					Supported:   false,
					Error:       err.Error(),
				})
				continue
			}

			estimates = append(estimates, providerEstimate{
				Provider:    p,
				DisplayName: info.DisplayName,
				Estimate:    estimate,
				Supported:   true,
			})
		}

		// Sort by total cost (supported first, then by cost)
		sort.Slice(estimates, func(i, j int) bool {
			if estimates[i].Supported != estimates[j].Supported {
				return estimates[i].Supported
			}
			if !estimates[i].Supported {
				return false
			}
			return estimates[i].Estimate.Total < estimates[j].Estimate.Total
		})

		// Output
		if compareOutput == "json" {
			type jsonOutput struct {
				Provider string             `json:"provider"`
				Name     string             `json:"name"`
				Total    float64            `json:"total,omitempty"`
				Currency string             `json:"currency,omitempty"`
				Compute  float64            `json:"compute,omitempty"`
				Storage  float64            `json:"storage,omitempty"`
				Database float64            `json:"database,omitempty"`
				Network  float64            `json:"network,omitempty"`
				Error    string             `json:"error,omitempty"`
				Details  map[string]float64 `json:"details,omitempty"`
			}

			var output []jsonOutput
			for _, est := range estimates {
				jo := jsonOutput{
					Provider: string(est.Provider),
					Name:     est.DisplayName,
				}
				if est.Supported && est.Estimate != nil {
					jo.Total = est.Estimate.Total
					jo.Currency = est.Estimate.Currency
					jo.Compute = est.Estimate.Compute
					jo.Storage = est.Estimate.Storage
					jo.Database = est.Estimate.Database
					jo.Network = est.Estimate.Network
					jo.Details = est.Estimate.Details
				} else {
					jo.Error = est.Error
				}
				output = append(output, jo)
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(output)
		}

		// Table output
		table := ui.NewTable([]string{"Provider", "Total/mo", "Compute", "Storage", "Database", "Network", "Notes"})

		for _, est := range estimates {
			if !est.Supported {
				table.AddRow([]string{
					est.DisplayName,
					"-",
					"-",
					"-",
					"-",
					"-",
					est.Error,
				})
				continue
			}

			if est.Estimate == nil {
				table.AddRow([]string{
					est.DisplayName,
					"-",
					"-",
					"-",
					"-",
					"-",
					"No estimate available",
				})
				continue
			}

			notes := ""
			if len(est.Estimate.Notes) > 0 {
				notes = est.Estimate.Notes[0]
				if len(notes) > 30 {
					notes = notes[:27] + "..."
				}
			}

			table.AddRow([]string{
				est.DisplayName,
				fmt.Sprintf("%.2f %s", est.Estimate.Total, est.Estimate.Currency),
				fmt.Sprintf("%.2f", est.Estimate.Compute),
				fmt.Sprintf("%.2f", est.Estimate.Storage),
				fmt.Sprintf("%.2f", est.Estimate.Database),
				fmt.Sprintf("%.2f", est.Estimate.Network),
				notes,
			})
		}

		fmt.Print(table.Render())

		// Show recommendation
		if !IsQuiet() && len(estimates) > 0 && estimates[0].Supported {
			ui.Divider()
			ui.Success(fmt.Sprintf("Recommended: %s (%.2f %s/month)",
				estimates[0].DisplayName,
				estimates[0].Estimate.Total,
				estimates[0].Estimate.Currency))

			// Show savings compared to cloud
			if len(estimates) > 1 {
				mostExpensive := estimates[len(estimates)-1]
				if mostExpensive.Supported && mostExpensive.Estimate != nil {
					savings := mostExpensive.Estimate.Total - estimates[0].Estimate.Total
					if savings > 0 {
						fmt.Printf("  Savings vs %s: %.2f %s/month (%.0f%%)\n",
							mostExpensive.DisplayName,
							savings,
							estimates[0].Estimate.Currency,
							(savings/mostExpensive.Estimate.Total)*100)
					}
				}
			}
		}

		return nil
	},
}

// providerToPlatform maps a provider to its target platform
func providerToPlatform(p provider.Provider) (target.Platform, bool) {
	switch p {
	case provider.ProviderHetzner:
		return target.PlatformHetzner, true
	case provider.ProviderScaleway:
		return target.PlatformScaleway, true
	case provider.ProviderOVH:
		return target.PlatformOVH, true
	default:
		return "", false
	}
}

func init() {
	rootCmd.AddCommand(providersCmd)

	providersCmd.AddCommand(providersListCmd)
	providersCmd.AddCommand(providersCompareCmd)

	// Compare flags
	providersCompareCmd.Flags().StringVarP(&compareInput, "input", "i", "", "input analysis file (from 'homeport analyze')")
	providersCompareCmd.Flags().StringVarP(&compareSourcePath, "source", "s", "", "source infrastructure path")
	providersCompareCmd.Flags().StringVarP(&compareOutput, "output", "o", "table", "output format (table, json)")
	providersCompareCmd.Flags().StringVar(&compareHALevel, "ha-level", "basic", "HA level for comparison (none, basic, multi-server, cluster)")
}
