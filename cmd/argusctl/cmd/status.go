package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/btopcu/argus/cmd/argusctl/internal/client"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Fetch aggregate platform status (falls back to /api/v1/health)",
	Long: `Fetches the platform status digest from /api/v1/status (aggregate endpoint
planned for STORY-067 Task 9). If the server does not yet expose that route
this command transparently falls back to /api/v1/health.`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	data, _, err := c.DoRaw(ctx, "GET", "/api/v1/status", nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			// Fallback: /api/v1/status is planned for Task 9.
			fmt.Fprintln(cmd.ErrOrStderr(), "note: /api/v1/status not yet available — falling back to /api/v1/health (this will switch once STORY-067 Task 9 lands)")
			data, _, err = c.DoRaw(ctx, "GET", "/api/v1/health", nil)
		}
		if err != nil {
			return errExit(err)
		}
	}

	var pretty interface{}
	if err := json.Unmarshal(data, &pretty); err == nil {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(pretty)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
