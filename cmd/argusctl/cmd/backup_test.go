package cmd

import (
	"strings"
	"testing"
)

func TestBackupRestore_DryRun(t *testing.T) {
	out, _, err := runRoot(t, "backup", "restore",
		"--from=s3://argus-backup/2026-04-12",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("dry-run should not fail: %v", err)
	}
	if !strings.Contains(out, "pitr-restore.sh") {
		t.Errorf("expected pitr-restore.sh in dry-run output, got:\n%s", out)
	}
	if !strings.Contains(out, "s3://argus-backup/2026-04-12") {
		t.Errorf("expected backup from path in output, got:\n%s", out)
	}
}

func TestBackupRestore_DryRun_WithPITRTarget(t *testing.T) {
	out, _, err := runRoot(t, "backup", "restore",
		"--from=s3://argus-backup/2026-04-12",
		"--with-pitr-target=2026-04-12 14:30:00 UTC",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("dry-run should not fail: %v", err)
	}
	if !strings.Contains(out, "--target-time") {
		t.Errorf("expected --target-time in dry-run output, got:\n%s", out)
	}
	if !strings.Contains(out, "14:30:00") {
		t.Errorf("expected pitr target time in output, got:\n%s", out)
	}
}

func TestBackupRestore_RequiresConfirm(t *testing.T) {
	_, _, err := runRoot(t, "backup", "restore",
		"--from=s3://argus-backup/2026-04-12",
	)
	if err == nil {
		t.Fatal("expected error when --confirm is missing")
	}
	if !strings.Contains(err.Error(), "--confirm") {
		t.Errorf("error should mention --confirm, got: %v", err)
	}
}

func TestBackupRestore_RequiresFrom(t *testing.T) {
	_, _, err := runRoot(t, "backup", "restore",
		"--confirm",
	)
	if err == nil {
		t.Fatal("expected error when --from is missing")
	}
}

func TestBackupRestore_NoConfirmPrintsWhatWouldHappen(t *testing.T) {
	out, _, err := runRoot(t, "backup", "restore",
		"--from=s3://argus-backup/2026-04-12",
	)
	if err == nil {
		t.Fatal("expected error (no --confirm)")
	}
	if !strings.Contains(out, "DESTRUCTIVE") && !strings.Contains(out, "destructive") {
		t.Errorf("output should warn about destructive operation, got:\n%s", out)
	}
	if !strings.Contains(out, "s3://argus-backup/2026-04-12") {
		t.Errorf("output should show backup source, got:\n%s", out)
	}
}
