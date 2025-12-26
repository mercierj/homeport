package cli

import (
	"fmt"

	"github.com/agnostech/agnostech/internal/api"
	"github.com/agnostech/agnostech/internal/cli/ui"
	"github.com/spf13/cobra"
)

var (
	servePort   int
	serveHost   string
	serveNoAuth bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the AgnosTech web dashboard",
	Long: `Start the web dashboard for infrastructure migration and management.

The dashboard provides:
  - Migration wizard for Terraform/CloudFormation/ARM
  - Infrastructure management for deployed stacks
  - Storage, database, and secrets management

Examples:
  agnostech serve                    # Start on localhost:8080
  agnostech serve --port 3000        # Custom port
  agnostech serve --host 0.0.0.0     # Listen on all interfaces`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().IntVarP(&servePort, "port", "p", 8080, "port to serve on")
	serveCmd.Flags().StringVarP(&serveHost, "host", "H", "localhost", "host to bind to")
	serveCmd.Flags().BoolVar(&serveNoAuth, "no-auth", false, "disable authentication (dev mode)")
}

func runServe(cmd *cobra.Command, args []string) error {
	if !IsQuiet() {
		ui.Header("AgnosTech Dashboard")
		ui.Info(fmt.Sprintf("Starting server on http://%s:%d", serveHost, servePort))
		ui.Divider()
	}

	server := api.NewServer(api.Config{
		Host:    serveHost,
		Port:    servePort,
		NoAuth:  serveNoAuth,
		Verbose: IsVerbose(),
	})

	if !IsQuiet() {
		ui.Success("Server started. Press Ctrl+C to stop.")
	}

	return server.Start()
}
