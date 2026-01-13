package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"firefly-ec2-drift-detector/aws"
	flog "firefly-ec2-drift-detector/logger"
	"firefly-ec2-drift-detector/models"
	"firefly-ec2-drift-detector/service"
	"firefly-ec2-drift-detector/terraform"
)

var (
	terraformStatePath string
	instanceIDs        []string
	attributes         []string
	outputFormat       string
	awsRegion          string
)

var detectorCmd = &cobra.Command{
	Use:   "detector",
	Short: "Detect drift between AWS and Terraform state",
	Long: `Detect configuration drift between live AWS EC2 instances and their 
Terraform state definitions. Compares specified attributes and reports
any discrepancies found.

This command initializes both AWS and Terraform clients to fetch current
infrastructure state and compare against expected state defined in Terraform.

Examples:
  # Check single instance for instance type drift
  firefly detector -s terraform.tfstate -i i-1234567890abcdef0 -a InstanceType

  # Check multiple instances for multiple attributes
  firefly detector -s terraform.tfstate \
    -i i-123,i-456 \
    -a InstanceType,SecurityGroups,Tags \
    -f json

  # Check all instances in state file
  firefly detector -s terraform.tfstate -a InstanceType,Monitoring
  
  # Enable verbose logging
  firefly detector -v -s terraform.tfstate -a InstanceType`,
	SilenceUsage: true,
	RunE:         runDetector,
}

func init() {
	rootCmd.AddCommand(detectorCmd)

	detectorCmd.Flags().StringVarP(&terraformStatePath, "state", "s", "", "Path to Terraform state file (required)")
	detectorCmd.Flags().StringSliceVarP(&instanceIDs, "instances", "i", []string{}, "Comma-separated list of instance IDs (empty = all instances in state)")
	detectorCmd.Flags().StringSliceVarP(&attributes, "attributes", "a", []string{"InstanceType"}, "Comma-separated list of attributes to check")
	detectorCmd.Flags().StringVarP(&outputFormat, "format", "f", "text", "Output format: text or json")
	detectorCmd.Flags().StringVarP(&awsRegion, "region", "r", "us-east-1", "AWS region")

	detectorCmd.MarkFlagRequired("state")
}

func runDetector(cmd *cobra.Command, args []string) error {
	logger.Info("firefly drift detection started",
		zap.String("version", "1.0.0"),
		zap.String("terraform_state", terraformStatePath),
		zap.Strings("instance_ids", instanceIDs),
		zap.Strings("attributes", attributes),
		zap.String("output_format", outputFormat),
		zap.String("aws_region", awsRegion),
	)

	// Check if state file exists before proceeding
	if _, err := os.Stat(terraformStatePath); os.IsNotExist(err) {
		return fmt.Errorf("terraform state file not found: %s\n\nPlease ensure the file exists or provide the correct path using -s flag", terraformStatePath)
	}

	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(awsRegion))
	if err != nil {
		logger.Error("failed to load AWS config",
			zap.String("region", awsRegion),
			zap.Error(err),
		)
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	awsClient, err := aws.NewAWSClient(ctx, awsRegion, ec2.NewFromConfig(cfg), logger)
	if err != nil {
		return fmt.Errorf("failed to initialize AWS client: %w", err)
	}

	awsProvider := aws.NewStateProvider(awsClient)
	tfClient := terraform.NewTerraformClient(logger)
	comparator := models.NewAttributeComparator(logger)
	driftService := service.NewDriftService(awsProvider, tfClient, comparator, logger)

	reports, err := driftService.DetectDrift(ctx, terraformStatePath, instanceIDs, attributes)
	if err != nil {
		return handleDriftError(err, reports, outputFormat, logger)
	}

	return outputReports(reports, outputFormat, logger)
}

func handleDriftError(err error, reports []*models.DriftReport, format string, logger *flog.Logger) error {
	if len(reports) > 0 {
		fmt.Fprintf(os.Stderr, "\n⚠️  Warning: Drift detection completed with partial failures\n")
		fmt.Fprintf(os.Stderr, "Successfully checked: %d instance(s)\n", len(reports))
		fmt.Fprintf(os.Stderr, "Error details: %v\n\n", err)

		logger.Warn("partial failure during drift detection", zap.Error(err))

		return outputReports(reports, format, logger)
	}

	return fmt.Errorf("drift detection failed: %w", err)
}

func outputReports(reports []*models.DriftReport, format string, logger *flog.Logger) error {
	switch format {
	case "json":
		return outputJSON(reports, logger)
	case "text":
		return outputText(reports, logger)
	default:
		return fmt.Errorf("unsupported output format: %s (use 'text' or 'json')", format)
	}
}

func outputJSON(reports []*models.DriftReport, logger *flog.Logger) error {
	logger.Debug("formatting output as JSON")

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(reports); err != nil {
		return fmt.Errorf("failed to encode JSON output: %w", err)
	}

	return nil
}

func outputText(reports []*models.DriftReport, logger *flog.Logger) error {
	logger.Debug("formatting output as text")

	fmt.Printf("\n")
	fmt.Printf("╔═══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║           FIREFLY DRIFT DETECTION REPORT                  ║\n")
	fmt.Printf("╚═══════════════════════════════════════════════════════════╝\n")
	fmt.Printf("\n")

	if len(reports) == 0 {
		fmt.Printf("No instances were checked.\n\n")
		return nil
	}

	totalDrifts := 0
	for _, report := range reports {
		fmt.Printf("Instance: %s\n", report.InstanceID)
		fmt.Printf("Status: %s\n", getDriftStatus(report.HasDrift))

		if report.HasDrift {
			totalDrifts++
			fmt.Printf("Drifted Attributes (%d):\n", len(report.Drifts))
			for _, drift := range report.Drifts {
				fmt.Printf("  • %s:\n", drift.AttributeName)
				fmt.Printf("    Expected: %v\n", formatValue(drift.ExpectedValue))
				fmt.Printf("    Actual:   %v\n", formatValue(drift.ActualValue))
				fmt.Printf("    Type:     %s\n", drift.DriftType)
			}
		}
		fmt.Println()
	}

	fmt.Printf("───────────────────────────────────────────────────────────\n")
	fmt.Printf("Summary: %d/%d instances have drift\n", totalDrifts, len(reports))
	fmt.Printf("\n")

	logger.Info("drift report generated",
		zap.Int("total_instances", len(reports)),
		zap.Int("instances_with_drift", totalDrifts),
	)

	return nil
}

func getDriftStatus(hasDrift bool) string {
	if hasDrift {
		return "⚠  DRIFT DETECTED"
	}
	return "✓ NO DRIFT"
}

func formatValue(v interface{}) string {
	switch val := v.(type) {
	case []string:
		return fmt.Sprintf("[%s]", strings.Join(val, ", "))
	case map[string]string:
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}
