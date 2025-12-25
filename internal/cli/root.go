package cli

import (
	"fmt"
	"os"

	"github.com/cloudexit/cloudexit/internal/cli/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	verbose bool
	quiet   bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cloudexit",
	Short: "Migrate AWS infrastructure to self-hosted Docker stack",
	Long: `CloudExit is a CLI tool that helps you migrate from AWS infrastructure
to a self-hosted Docker-based stack.

It analyzes your AWS infrastructure from Terraform state files or configurations,
and generates a complete self-hosted stack with Docker Compose, Traefik,
and all necessary configurations.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Load configuration
		if err := initConfig(); err != nil {
			if verbose {
				ui.Error(fmt.Sprintf("Error loading config: %v", err))
			}
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(func() {
		_ = initConfig()
	})

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cloudexit.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "quiet output (errors only)")

	// Bind flags to viper
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("quiet", rootCmd.PersistentFlags().Lookup("quiet"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() error {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		// Search config in home directory with name ".cloudexit" (without extension).
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".cloudexit")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		if verbose {
			ui.Info(fmt.Sprintf("Using config file: %s", viper.ConfigFileUsed()))
		}
	}

	return nil
}

// IsVerbose returns whether verbose mode is enabled
func IsVerbose() bool {
	return viper.GetBool("verbose")
}

// IsQuiet returns whether quiet mode is enabled
func IsQuiet() bool {
	return viper.GetBool("quiet")
}
