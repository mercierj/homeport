package cli

import (
	"fmt"

	"github.com/cloudexit/cloudexit/pkg/version"
	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Long:  `Print the version, commit hash, and build date of the cloudexit CLI.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("CloudExit CLI\n")
		fmt.Printf("Version:    %s\n", version.Version)
		fmt.Printf("Commit:     %s\n", version.Commit)
		fmt.Printf("Build Date: %s\n", version.Date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
