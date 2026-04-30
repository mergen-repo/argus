package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var apikeyCmd = &cobra.Command{
	Use:   "apikey",
	Short: "Manage tenant API keys",
}

var (
	apikeyRotateTenant      string
	apikeyRotateKey         string
	apikeyRotateGracePeriod time.Duration
)

var apikeyRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Rotate an API key (grace-period overlap for old key)",
	RunE:  runAPIKeyRotate,
}

func init() {
	apikeyRotateCmd.Flags().StringVar(&apikeyRotateTenant, "tenant", "", "tenant ID that owns the key (required)")
	apikeyRotateCmd.Flags().StringVar(&apikeyRotateKey, "key", "", "API key ID to rotate (required)")
	apikeyRotateCmd.Flags().DurationVar(&apikeyRotateGracePeriod, "grace-period", 24*time.Hour, "old key grace-period (hint to server)")
	_ = apikeyRotateCmd.MarkFlagRequired("tenant")
	_ = apikeyRotateCmd.MarkFlagRequired("key")

	apikeyCmd.AddCommand(apikeyRotateCmd)
}

type apikeyRotateDTO struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Prefix          string `json:"prefix"`
	Key             string `json:"key"`
	GracePeriodEnds string `json:"grace_period_ends"`
}

func runAPIKeyRotate(cmd *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	// NOTE: the server's apikey.Rotate handler derives tenant scope from the
	// caller's JWT, not the request body — the `tenant_id` and
	// `grace_period_sec` fields here are forwarded for forward-compatibility
	// and audit parity but are currently ignored server-side (grace window
	// is hardcoded to 24h in internal/api/apikey/handler.go). The --tenant
	// flag remains required so operators explicitly acknowledge the scope.
	body := map[string]interface{}{
		"tenant_id":        apikeyRotateTenant,
		"grace_period_sec": int(apikeyRotateGracePeriod.Seconds()),
	}

	var out apikeyRotateDTO
	path := "/api/v1/api-keys/" + apikeyRotateKey + "/rotate"
	if err := c.Do(ctx, "POST", path, body, &out); err != nil {
		return errExit(err)
	}

	if outputFmt == "json" {
		return printJSON(cmd.OutOrStdout(), out)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "API key rotated. Store the new secret securely — it is shown only once.")
	fmt.Fprintf(cmd.OutOrStdout(), "  id:                %s\n", out.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  name:              %s\n", out.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "  prefix:            %s\n", out.Prefix)
	fmt.Fprintf(cmd.OutOrStdout(), "  key (NEW):         %s\n", out.Key)
	fmt.Fprintf(cmd.OutOrStdout(), "  grace_period_ends: %s\n", out.GracePeriodEnds)
	return nil
}
