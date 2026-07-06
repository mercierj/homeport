package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
	"github.com/spf13/cobra"
)

var (
	coverageFormat   string
	coverageProvider string
	coverageStrict   bool
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
	rootCmd.AddCommand(coverageCmd)
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
				service.Provider, service.Service, service.Status, strings.Join(service.ResourceTypes, ", "))
		}
	default:
		fmt.Fprintln(w, "PROVIDER SERVICE STATUS RESOURCES")
		for _, service := range services {
			fmt.Fprintf(w, "%-8s %-28s %-10s %s\n",
				service.Provider, service.Service, service.Status, strings.Join(service.ResourceTypes, ", "))
		}
	}
	return nil
}

func hasCoverageDrift(drift appcoverage.Drift) bool {
	return len(drift.MapperWithoutLedger) > 0 || len(drift.LedgerWithoutMapper) > 0
}
