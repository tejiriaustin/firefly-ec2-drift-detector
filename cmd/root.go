package cmd

import (
	"fmt"
	"os"

	flog "firefly-ec2-drift-detector/logger"
	"github.com/spf13/cobra"
)

var (
	logger  *flog.Logger
	verbose bool
	err     error
)

var rootCmd = &cobra.Command{
	Use:   "firefly",
	Short: "EC2 drift detection tool",
	Long: `Firefly is a CLI tool for detecting configuration drift between 
AWS EC2 instances and their Terraform state definitions.

It compares live infrastructure state with expected state defined in 
Terraform and reports any discrepancies found.`,
}

func buildLogger() {
	logLevel := "error"
	if verbose {
		logLevel = "info"
	}

	logCfg := flog.Config{
		LogLevel:    logLevel,
		DevMode:     false,
		ServiceName: "firefly-ec2-drift-detector",
	}
	logger, err = flog.NewLogger(logCfg)
	if err != nil {
		panic(fmt.Errorf("failed to create logger: %v", err))
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	cobra.OnInitialize(buildLogger)
}
