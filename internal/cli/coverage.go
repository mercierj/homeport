package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
	"github.com/spf13/cobra"
)

var (
	coverageFormat   string
	coverageProvider string
	coverageStrict   bool
	coverageCatalog  string
	coverageMarkdown string

	addMissingProvider    string
	addMissingService     string
	addMissingCategory    string
	addMissingSourceAPI   string
	addMissingResources   string
	addMissingTarget      string
	addMissingAPIStrategy string
	addMissingImpossible  string
	promoteProvider       string
	promoteService        string
	promoteStatus         string
	coverageChecklistDir  string
)

var coverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Report HomePort service coverage",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCoverageFormat(coverageFormat); err != nil {
			return err
		}
		if err := validateCoverageProvider(coverageProvider); err != nil {
			return err
		}

		catalog, err := appcoverage.LoadDefaultCatalog()
		if err != nil {
			return fmt.Errorf("load coverage catalog: %w", err)
		}

		service := appcoverage.NewService(*catalog)
		drift := service.FindDrift()
		services := filterCoverageServices(catalog.Services, coverageProvider)

		if err := printCoverage(cmd.OutOrStdout(), coverageFormat, services, drift); err != nil {
			return err
		}

		if coverageStrict && hasCoverageDrift(drift) {
			return fmt.Errorf("coverage drift: %d mapper without ledger, %d ledger without mapper",
				len(drift.MapperWithoutLedger), len(drift.LedgerWithoutMapper))
		}

		return nil
	},
}

func init() {
	coverageCmd.Flags().StringVar(&coverageFormat, "format", "table", "output format: table, json, markdown")
	coverageCmd.Flags().StringVar(&coverageProvider, "provider", "", "filter provider: aws, gcp, azure")
	coverageCmd.Flags().BoolVar(&coverageStrict, "strict", false, "exit non-zero on coverage drift")
	coverageCmd.AddCommand(coverageAddMissingCmd)
	coverageCmd.AddCommand(coveragePromoteCmd)
	rootCmd.AddCommand(coverageCmd)

	coverageAddMissingCmd.Flags().StringVar(&coverageCatalog, "catalog", "docs/coverage/services.yaml", "coverage catalog path")
	coverageAddMissingCmd.Flags().StringVar(&coverageMarkdown, "markdown", "", "optional markdown output path")
	coverageAddMissingCmd.Flags().StringVar(&addMissingProvider, "provider", "", "provider: aws, gcp, azure")
	coverageAddMissingCmd.Flags().StringVar(&addMissingService, "service", "", "service name")
	coverageAddMissingCmd.Flags().StringVar(&addMissingCategory, "category", "", "service category")
	coverageAddMissingCmd.Flags().StringVar(&addMissingSourceAPI, "source-api", "", "source provider API")
	coverageAddMissingCmd.Flags().StringVar(&addMissingResources, "resource-types", "", "comma-separated Terraform resource types")
	coverageAddMissingCmd.Flags().StringVar(&addMissingTarget, "target", "", "likely open-source target")
	coverageAddMissingCmd.Flags().StringVar(&addMissingAPIStrategy, "api-compat-strategy", "", "API compatibility strategy")
	coverageAddMissingCmd.Flags().StringVar(&addMissingImpossible, "impossibility-notes", "", "impossibility notes")

	coveragePromoteCmd.Flags().StringVar(&coverageCatalog, "catalog", "docs/coverage/services.yaml", "coverage catalog path")
	coveragePromoteCmd.Flags().StringVar(&coverageMarkdown, "markdown", "", "optional markdown output path")
	coveragePromoteCmd.Flags().StringVar(&coverageChecklistDir, "checklist-dir", "", "conformance checklist directory")
	coveragePromoteCmd.Flags().StringVar(&promoteProvider, "provider", "", "provider: aws, gcp, azure")
	coveragePromoteCmd.Flags().StringVar(&promoteService, "service", "", "service name")
	coveragePromoteCmd.Flags().StringVar(&promoteStatus, "status", "", "target status: missing, guided, mapped, full, impossible")
}

var coverageAddMissingCmd = &cobra.Command{
	Use:   "add-missing",
	Short: "Add a missing service to the coverage catalog",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCoverageProvider(addMissingProvider); err != nil {
			return err
		}
		catalog, err := appcoverage.LoadCatalog(coverageCatalog)
		if err != nil {
			return err
		}
		if err := catalog.AddMissing(domaincoverage.ServiceCoverage{
			Provider:                 addMissingProvider,
			Service:                  addMissingService,
			Category:                 addMissingCategory,
			SourceAPI:                addMissingSourceAPI,
			ResourceTypes:            []string{},
			TerraformResourceTypes:   splitCSV(addMissingResources),
			Target:                   addMissingTarget,
			APICompatibilityStrategy: addMissingAPIStrategy,
			ImpossibilityNotes:       addMissingImpossible,
		}); err != nil {
			return err
		}
		return saveCoverageCatalogAndMarkdown(cmd, *catalog)
	},
}

var coveragePromoteCmd = &cobra.Command{
	Use:   "promote",
	Short: "Promote a service coverage status with checklist guards",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateCoverageProvider(promoteProvider); err != nil {
			return err
		}
		status := domaincoverage.Status(promoteStatus)
		switch status {
		case domaincoverage.StatusMissing, domaincoverage.StatusGuided, domaincoverage.StatusMapped, domaincoverage.StatusFull, domaincoverage.StatusImpossible:
		default:
			return fmt.Errorf("invalid coverage status %q", promoteStatus)
		}
		catalog, err := appcoverage.LoadCatalog(coverageCatalog)
		if err != nil {
			return err
		}
		if err := catalog.Promote(promoteProvider, promoteService, status); err != nil {
			return err
		}
		checklist := conformanceChecklistPath(coverageCatalog, coverageChecklistDir, promoteProvider, promoteService)
		if err := os.MkdirAll(filepath.Dir(checklist), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(checklist, []byte("# Coverage Conformance Checklist\n\n- [ ] Parser\n- [ ] Mapper\n- [ ] Migration\n- [ ] Compatibility\n- [ ] Validation\n"), 0o644); err != nil {
			return err
		}
		return saveCoverageCatalogAndMarkdown(cmd, *catalog)
	},
}

func validateCoverageFormat(format string) error {
	switch format {
	case "table", "json", "markdown":
		return nil
	default:
		return fmt.Errorf("invalid coverage format %q: expected table, json, or markdown", format)
	}
}

func validateCoverageProvider(provider string) error {
	switch provider {
	case "", "aws", "gcp", "azure":
		return nil
	default:
		return fmt.Errorf("invalid coverage provider %q: expected aws, gcp, or azure", provider)
	}
}

func filterCoverageServices(services []domaincoverage.ServiceCoverage, provider string) []domaincoverage.ServiceCoverage {
	if provider == "" {
		return services
	}

	filtered := make([]domaincoverage.ServiceCoverage, 0, len(services))
	for _, service := range services {
		if service.Provider == provider {
			filtered = append(filtered, service)
		}
	}
	return filtered
}

func printCoverage(w io.Writer, format string, services []domaincoverage.ServiceCoverage, drift appcoverage.Drift) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Services []domaincoverage.ServiceCoverage `json:"services"`
			Drift    appcoverage.Drift                `json:"drift"`
		}{Services: services, Drift: drift})
	case "markdown":
		fmt.Fprintln(w, "| PROVIDER | SERVICE | STATUS | RESOURCES |")
		fmt.Fprintln(w, "| --- | --- | --- | --- |")
		for _, service := range services {
			fmt.Fprintf(w, "| %s | %s | %s | %s |\n",
				service.Provider, service.Service, service.Status, strings.Join(displayResourceTypes(service), ", "))
		}
	default:
		fmt.Fprintln(w, "PROVIDER SERVICE STATUS RESOURCES")
		for _, service := range services {
			fmt.Fprintf(w, "%-8s %-28s %-10s %s\n",
				service.Provider, service.Service, service.Status, strings.Join(displayResourceTypes(service), ", "))
		}
	}
	return nil
}

func displayResourceTypes(service domaincoverage.ServiceCoverage) []string {
	if len(service.ResourceTypes) > 0 {
		return service.ResourceTypes
	}
	return service.TerraformResourceTypes
}

func hasCoverageDrift(drift appcoverage.Drift) bool {
	return len(drift.MapperWithoutLedger) > 0 || len(drift.LedgerWithoutMapper) > 0
}

func saveCoverageCatalogAndMarkdown(cmd *cobra.Command, catalog appcoverage.Catalog) error {
	if err := appcoverage.SaveCatalog(coverageCatalog, catalog); err != nil {
		return err
	}
	if coverageMarkdown != "" {
		var buf strings.Builder
		if err := printCoverage(&buf, "markdown", catalog.Services, appcoverage.Drift{}); err != nil {
			return err
		}
		if err := os.WriteFile(coverageMarkdown, []byte(buf.String()), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "coverage catalog updated")
	return nil
}

func splitCSV(value string) []string {
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func conformanceChecklistPath(catalogPath, checklistDir, provider, service string) string {
	if checklistDir == "" {
		checklistDir = filepath.Join(filepath.Dir(catalogPath), "conformance")
	}
	slug := strings.ToLower(service)
	slug = strings.NewReplacer(" ", "-", "/", "-", "_", "-").Replace(slug)
	return filepath.Join(checklistDir, fmt.Sprintf("%s-%s.md", provider, slug))
}
