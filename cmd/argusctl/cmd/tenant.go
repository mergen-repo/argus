package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var tenantCmd = &cobra.Command{
	Use:   "tenant",
	Short: "Manage tenants",
	Long:  "Create, list, and manage lifecycle state of Argus tenants.",
}

var (
	tenantCreateName         string
	tenantCreateAdminEmail   string
	tenantCreateContactPhone string
)

var tenantCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new tenant",
	RunE:  runTenantCreate,
}

var tenantListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tenants",
	RunE:  runTenantList,
}

var tenantSuspendCmd = &cobra.Command{
	Use:   "suspend <tenant-id>",
	Short: "Suspend a tenant (blocks all traffic; data retained)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTenantStateChange(cmd, args[0], "suspended")
	},
}

var tenantResumeCmd = &cobra.Command{
	Use:   "resume <tenant-id>",
	Short: "Resume a suspended tenant",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTenantStateChange(cmd, args[0], "active")
	},
}

func init() {
	tenantCreateCmd.Flags().StringVar(&tenantCreateName, "name", "", "tenant name (required)")
	tenantCreateCmd.Flags().StringVar(&tenantCreateAdminEmail, "admin-email", "", "contact/admin email for the tenant (required)")
	tenantCreateCmd.Flags().StringVar(&tenantCreateContactPhone, "contact-phone", "", "optional contact phone")
	_ = tenantCreateCmd.MarkFlagRequired("name")
	_ = tenantCreateCmd.MarkFlagRequired("admin-email")

	tenantCmd.AddCommand(tenantCreateCmd)
	tenantCmd.AddCommand(tenantListCmd)
	tenantCmd.AddCommand(tenantSuspendCmd)
	tenantCmd.AddCommand(tenantResumeCmd)
}

type tenantDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	State        string `json:"state"`
	ContactEmail string `json:"contact_email"`
	CreatedAt    string `json:"created_at"`
}

func runTenantCreate(cmd *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	body := map[string]interface{}{
		"name":          tenantCreateName,
		"contact_email": tenantCreateAdminEmail,
	}
	if tenantCreateContactPhone != "" {
		body["contact_phone"] = tenantCreateContactPhone
	}

	// The server returns the full tenant plus, when generated, a temporary
	// admin credential under the `admin_password` field. Use a loose map so
	// we surface whatever the server chose to include.
	var out map[string]interface{}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	if err := c.Do(ctx, "POST", "/api/v1/tenants", body, &out); err != nil {
		return errExit(err)
	}

	if outputFmt == "json" {
		return printJSON(cmd.OutOrStdout(), out)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Tenant created:\n  id:    %v\n  name:  %v\n  state: %v\n",
		out["id"], out["name"], out["state"])
	if v, ok := out["admin_password"]; ok {
		fmt.Fprintf(cmd.OutOrStdout(), "  admin_password (shown ONCE): %v\n", v)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "  admin_password: <not returned by this server build; bootstrap admin user separately>")
	}
	return nil
}

func runTenantList(cmd *cobra.Command, _ []string) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	var tenants []tenantDTO
	if err := c.Do(ctx, "GET", "/api/v1/tenants", nil, &tenants); err != nil {
		return errExit(err)
	}

	if outputFmt == "json" {
		return printJSON(cmd.OutOrStdout(), tenants)
	}
	return renderTenantTable(cmd.OutOrStdout(), tenants)
}

func runTenantStateChange(cmd *cobra.Command, idStr, desiredState string) error {
	c, err := newClient()
	if err != nil {
		return err
	}

	reqCtx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	// The core state-transition endpoint is PATCH /api/v1/tenants/{id} with
	// body {"state": "<state>"}. validTenantTransitions on the server gates
	// which transitions are allowed (e.g., active → suspended, suspended →
	// active|terminated). The CLI uses PATCH rather than dedicated
	// /suspend and /resume endpoints to stay consistent with ADR-001
	// ("argusctl is a thin HTTP client over the existing API").
	body := map[string]interface{}{"state": desiredState}
	var out tenantDTO
	if err := c.Do(reqCtx, "PATCH", "/api/v1/tenants/"+idStr, body, &out); err != nil {
		return errExit(err)
	}

	if outputFmt == "json" {
		return printJSON(cmd.OutOrStdout(), out)
	}

	action := "suspended"
	if desiredState == "active" {
		action = "resumed"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Tenant %s %s (state=%s)\n", out.ID, action, out.State)
	return nil
}

func renderTenantTable(w io.Writer, tenants []tenantDTO) error {
	if len(tenants) == 0 {
		fmt.Fprintln(w, "No tenants found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSTATE\tCONTACT EMAIL\tCREATED")
	for _, t := range tenants {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Name, t.State, t.ContactEmail, t.CreatedAt)
	}
	return tw.Flush()
}

func printJSON(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
