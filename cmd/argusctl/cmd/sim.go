package cmd

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var simCmd = &cobra.Command{
	Use:   "sim",
	Short: "SIM card operations",
	Long:  "Manage SIM cards including bulk state-change operations.",
}

var (
	simBulkTenantID  string
	simBulkOperation string
	simBulkSegmentID string
)

var simBulkOpCmd = &cobra.Command{
	Use:   "bulk-op",
	Short: "Run a bulk state-change operation on a SIM segment",
	Long: `Submit a bulk state-change job targeting all SIMs in a segment.

Operations: suspend, resume, activate

Example:
  argusctl sim bulk-op --tenant t-1 --operation suspend --segment seg-abc`,
	RunE: runSimBulkOp,
}

func init() {
	simBulkOpCmd.Flags().StringVar(&simBulkTenantID, "tenant", "", "tenant ID (required)")
	simBulkOpCmd.Flags().StringVar(&simBulkOperation, "operation", "", "operation: suspend|resume|activate (required)")
	simBulkOpCmd.Flags().StringVar(&simBulkSegmentID, "segment", "", "segment ID (required)")
	_ = simBulkOpCmd.MarkFlagRequired("tenant")
	_ = simBulkOpCmd.MarkFlagRequired("operation")
	_ = simBulkOpCmd.MarkFlagRequired("segment")

	simCmd.AddCommand(simBulkOpCmd)
}

type bulkStateChangeResponse struct {
	JobID          string `json:"job_id"`
	Status         string `json:"status"`
	EstimatedCount int64  `json:"estimated_count"`
}

type jobStatusResponse struct {
	ID             string  `json:"id"`
	State          string  `json:"state"`
	TotalItems     int     `json:"total_items"`
	ProcessedItems int     `json:"processed_items"`
	FailedItems    int     `json:"failed_items"`
	ProgressPct    float64 `json:"progress_pct"`
}

func runSimBulkOp(cmd *cobra.Command, _ []string) error {
	targetState, err := mapBulkOperation(simBulkOperation)
	if err != nil {
		return err
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
	defer cancel()

	body := map[string]interface{}{
		"segment_id":   simBulkSegmentID,
		"target_state": targetState,
	}

	var jobResp bulkStateChangeResponse
	submitCtx, submitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer submitCancel()

	if err := c.Do(submitCtx, "POST", "/api/v1/sims/bulk/state-change", body, &jobResp); err != nil {
		return errExit(err)
	}

	jobID := jobResp.JobID
	if jobID == "" {
		return fmt.Errorf("server did not return a job_id")
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Job %s queued (operation=%s segment=%s)\n",
		jobID, simBulkOperation, simBulkSegmentID)

	return pollJobProgress(ctx, c, cmd, jobID)
}

func mapBulkOperation(op string) (string, error) {
	switch op {
	case "suspend":
		return "suspended", nil
	case "resume", "activate":
		return "active", nil
	default:
		return "", fmt.Errorf("invalid operation %q; must be suspend, resume, or activate", op)
	}
}

func pollJobProgress(ctx context.Context, c interface {
	Do(ctx context.Context, method, path string, body interface{}, out interface{}) error
}, cmd *cobra.Command, jobID string) error {
	stderr := cmd.ErrOrStderr()
	const pollInterval = 2 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pollCtx, pollCancel := context.WithTimeout(ctx, 15*time.Second)
		var job jobStatusResponse
		err := c.Do(pollCtx, "GET", "/api/v1/jobs/"+jobID, nil, &job)
		pollCancel()

		if err != nil {
			fmt.Fprintf(stderr, "\nPoll error: %v\n", err)
			return errExit(err)
		}

		bar := renderProgressBar(job.ProgressPct, job.ProcessedItems, job.TotalItems)
		fmt.Fprintf(stderr, "\r%s", bar)

		switch job.State {
		case "completed":
			fmt.Fprintln(stderr)
			fmt.Fprintf(cmd.OutOrStdout(), "Job %s completed: %d processed, %d failed\n",
				jobID, job.ProcessedItems, job.FailedItems)
			if job.FailedItems > 0 {
				return fmt.Errorf("job completed with %d failed items", job.FailedItems)
			}
			return nil
		case "failed", "cancelled":
			fmt.Fprintln(stderr)
			return fmt.Errorf("job %s ended with state=%s", jobID, job.State)
		}

		time.Sleep(pollInterval)
	}
}

func renderProgressBar(pct float64, processed, total int) string {
	const width = 8
	filled := int(math.Round(pct / 100.0 * width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)

	var eta string
	if pct > 0 && pct < 100 {
		remaining := float64(total-processed) / (pct / 100.0 * float64(total)) * 2
		if remaining > 0 && !math.IsInf(remaining, 0) && !math.IsNaN(remaining) {
			eta = fmt.Sprintf(" eta %.0fs", remaining)
		}
	}

	return fmt.Sprintf("[%s] %.0f%% (%d/%d)%s", bar, pct, processed, total, eta)
}
