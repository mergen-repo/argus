package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var complianceCmd = &cobra.Command{
	Use:   "compliance",
	Short: "Compliance report operations",
	Long:  "Export BTK/regulatory compliance reports in PDF, CSV, or JSON format.",
}

var (
	complianceExportTenantID string
	complianceExportFormat   string
	complianceExportFrom     string
	complianceExportTo       string
	complianceExportOutput   string
)

var complianceExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a compliance report to a file",
	Long: `Export a BTK/regulatory compliance report.

Supported formats: pdf, csv, json

Example:
  argusctl compliance export --tenant t-1 --format pdf --from 2026-01-01 --to 2026-03-31 --output report.pdf`,
	RunE: runComplianceExport,
}

func init() {
	complianceExportCmd.Flags().StringVar(&complianceExportTenantID, "tenant", "", "tenant ID (required)")
	complianceExportCmd.Flags().StringVar(&complianceExportFormat, "format", "json", "report format: pdf|csv|json")
	complianceExportCmd.Flags().StringVar(&complianceExportFrom, "from", "", "report start date YYYY-MM-DD")
	complianceExportCmd.Flags().StringVar(&complianceExportTo, "to", "", "report end date YYYY-MM-DD")
	complianceExportCmd.Flags().StringVar(&complianceExportOutput, "output", "", "output file path (required)")
	_ = complianceExportCmd.MarkFlagRequired("tenant")
	_ = complianceExportCmd.MarkFlagRequired("output")

	complianceCmd.AddCommand(complianceExportCmd)
}

func runComplianceExport(cmd *cobra.Command, _ []string) error {
	switch complianceExportFormat {
	case "pdf", "csv", "json":
	default:
		return fmt.Errorf("unsupported format %q; must be pdf, csv, or json", complianceExportFormat)
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("tenant_id", complianceExportTenantID)
	q.Set("format", complianceExportFormat)
	if complianceExportFrom != "" {
		q.Set("from", complianceExportFrom)
	}
	if complianceExportTo != "" {
		q.Set("to", complianceExportTo)
	}

	f, err := os.Create(complianceExportOutput)
	if err != nil {
		return fmt.Errorf("create output file %q: %w", complianceExportOutput, err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(cmd.Context(), 120*time.Second)
	defer cancel()

	fmt.Fprintf(cmd.ErrOrStderr(), "Exporting %s compliance report for tenant %s...\n",
		complianceExportFormat, complianceExportTenantID)

	if err := c.DoStream(ctx, "GET", "/api/v1/compliance/btk-report", q, f); err != nil {
		_ = os.Remove(complianceExportOutput)
		return errExit(err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Report written to %s\n", complianceExportOutput)
	return nil
}
