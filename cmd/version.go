package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Long:  `Display version information for Firefly EC2 Drift Detector.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Firefly EC2 Drift Detector")
		fmt.Println("Version: 1.0.0")
		fmt.Println("Built with Go")
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
