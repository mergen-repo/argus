package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Backup and restore operations",
	Long:  "Manage Argus backups and run point-in-time recovery (PITR) restores.",
}

var (
	backupRestoreFrom       string
	backupRestoreConfirm    bool
	backupRestorePITRTarget string
	backupRestoreDryRun     bool
)

var backupRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore from a backup using the PITR restore script",
	Long: `Restore the Argus database from a backup snapshot using PITR.

Without --confirm the command prints what would happen and exits non-zero.
Use --dry-run to print the pitr-restore.sh command without executing it.

Example:
  argusctl backup restore --from s3://argus-backup/2026-04-12 --with-pitr-target "2026-04-12 14:30:00 UTC" --confirm`,
	RunE: runBackupRestore,
}

func init() {
	backupRestoreCmd.Flags().StringVar(&backupRestoreFrom, "from", "", "backup snapshot identifier / S3 path (required)")
	backupRestoreCmd.Flags().BoolVar(&backupRestoreConfirm, "confirm", false, "explicit confirmation required for destructive restore operation")
	backupRestoreCmd.Flags().StringVar(&backupRestorePITRTarget, "with-pitr-target", "", "PITR target timestamp: 'YYYY-MM-DD HH:MM:SS UTC'")
	backupRestoreCmd.Flags().BoolVar(&backupRestoreDryRun, "dry-run", false, "print the pitr-restore.sh command without executing it")
	_ = backupRestoreCmd.MarkFlagRequired("from")

	backupCmd.AddCommand(backupRestoreCmd)
}

func buildPITRArgs(from, pitrTarget string, dryRun bool) []string {
	args := []string{}
	if pitrTarget != "" {
		args = append(args, "--target-time", pitrTarget)
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	return args
}

func scriptPath() string {
	exe, err := os.Executable()
	if err == nil {
		repoRoot := findRepoRoot(filepath.Dir(exe))
		if repoRoot != "" {
			candidate := filepath.Join(repoRoot, "deploy", "scripts", "pitr-restore.sh")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return "deploy/scripts/pitr-restore.sh"
}

func findRepoRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func runBackupRestore(cmd *cobra.Command, _ []string) error {
	script := scriptPath()
	args := buildPITRArgs(backupRestoreFrom, backupRestorePITRTarget, backupRestoreDryRun)

	if backupRestoreDryRun {
		parts := []string{script}
		parts = append(parts, args...)
		fmt.Fprintf(cmd.OutOrStdout(), "Would run: %s\n", strings.Join(parts, " "))
		fmt.Fprintf(cmd.OutOrStdout(), "  backup snapshot: %s\n", backupRestoreFrom)
		if backupRestorePITRTarget != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "  pitr target:     %s\n", backupRestorePITRTarget)
		}
		return nil
	}

	if !backupRestoreConfirm {
		fmt.Fprintf(cmd.OutOrStdout(), "This will restore the Argus database from backup snapshot: %s\n", backupRestoreFrom)
		if backupRestorePITRTarget != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "PITR target time: %s\n", backupRestorePITRTarget)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nThis is a DESTRUCTIVE operation. Pass --confirm to execute.")
		return fmt.Errorf("restore aborted: --confirm is required to proceed")
	}

	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("pitr-restore script not found at %s: %w", script, err)
	}

	c := exec.Command(script, args...)
	c.Stdout = io.MultiWriter(cmd.OutOrStdout(), os.Stdout)
	c.Stderr = io.MultiWriter(cmd.ErrOrStderr(), os.Stderr)

	fmt.Fprintf(cmd.OutOrStdout(), "Starting PITR restore from %s...\n", backupRestoreFrom)
	if err := c.Run(); err != nil {
		return fmt.Errorf("pitr-restore.sh failed: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Restore completed successfully.")
	return nil
}
