package cmd

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage tenant users",
}

var (
	userPurgeTenantID string
	userPurgeUserID   string
	userPurgeConfirm  bool
)

var userPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "GDPR-delete a user (NULLs PII, revokes sessions, audit logged)",
	Long: `Permanently erase Personally Identifiable Information (PII) for a user in
accordance with GDPR Article 17 (right to erasure). The user row remains for
referential integrity but email, name, password_hash and TOTP secret are
nulled and all active sessions are revoked. This action is irreversible.`,
	RunE: runUserPurge,
}

func init() {
	userPurgeCmd.Flags().StringVar(&userPurgeTenantID, "tenant", "", "tenant ID the user belongs to (required)")
	userPurgeCmd.Flags().StringVar(&userPurgeUserID, "user", "", "user ID to purge (required)")
	userPurgeCmd.Flags().BoolVar(&userPurgeConfirm, "confirm", false, "explicit confirmation; required for destructive operation")
	_ = userPurgeCmd.MarkFlagRequired("tenant")
	_ = userPurgeCmd.MarkFlagRequired("user")

	userCmd.AddCommand(userPurgeCmd)
}

func runUserPurge(cmd *cobra.Command, _ []string) error {
	if !userPurgeConfirm {
		return fmt.Errorf("refusing to purge user %s: pass --confirm to execute the irreversible GDPR erasure", userPurgeUserID)
	}

	c, err := newClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	q := url.Values{}
	q.Set("gdpr", "1")
	q.Set("tenant_id", userPurgeTenantID)

	var out map[string]interface{}
	if err := c.DoQuery(ctx, "DELETE", "/api/v1/users/"+userPurgeUserID, q, nil, &out); err != nil {
		return errExit(err)
	}

	if outputFmt == "json" {
		return printJSON(cmd.OutOrStdout(), out)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "GDPR purge complete for user %s (tenant %s)\n", userPurgeUserID, userPurgeTenantID)
	if v, ok := out["sessions_revoked"]; ok {
		fmt.Fprintf(cmd.OutOrStdout(), "  sessions_revoked: %v\n", v)
	}
	if v, ok := out["purged_at"]; ok {
		fmt.Fprintf(cmd.OutOrStdout(), "  purged_at:        %v\n", v)
	}
	return nil
}
