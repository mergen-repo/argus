package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check /health/ready — readiness probe for Argus",
	RunE:  runHealth,
}

func runHealth(cmd *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	data, _, err := c.DoRaw(ctx, "GET", "/health/ready", nil)
	if err != nil {
		return errExit(err)
	}

	// /health/ready does NOT use the success envelope; DoRaw gives us the
	// raw JSON body the server emitted.
	var pretty interface{}
	if err := json.Unmarshal(data, &pretty); err == nil {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(pretty)
	}
	// Fallback: print raw bytes.
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}
