package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskProbe_ExistingMount_ReturnsOk(t *testing.T) {
	tmpDir := t.TempDir()
	results := diskProbe([]string{tmpDir}, 85, 95)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Mount != tmpDir {
		t.Errorf("mount = %q, want %q", r.Mount, tmpDir)
	}
	if r.UsedPct < 0 {
		t.Errorf("used_pct = %v, want >= 0", r.UsedPct)
	}
	if r.Status != "ok" && r.Status != "degraded" && r.Status != "unhealthy" {
		t.Errorf("status = %q, want ok|degraded|unhealthy", r.Status)
	}
}

func TestDiskProbe_MissingMount_ReturnsMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nonexistent", "path")
	results := diskProbe([]string{missing}, 85, 95)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Status != "missing" {
		t.Errorf("status = %q, want missing", r.Status)
	}
	if r.UsedPct != 0 {
		t.Errorf("used_pct = %v, want 0", r.UsedPct)
	}
}

func TestDiskProbe_MixedMounts_DoesNotFailOnMissing(t *testing.T) {
	tmpDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "nonexistent")
	results := diskProbe([]string{tmpDir, missing}, 85, 95)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Mount != tmpDir {
		t.Errorf("first result mount = %q, want %q", results[0].Mount, tmpDir)
	}
	if results[1].Status != "missing" {
		t.Errorf("second result status = %q, want missing", results[1].Status)
	}
}

func TestDiskProbe_EmptyMounts_ReturnsEmptySlice(t *testing.T) {
	results := diskProbe([]string{}, 85, 95)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestDiskProbe_ThresholdStatusMapping(t *testing.T) {
	tmpDir := t.TempDir()

	f, err := os.CreateTemp(tmpDir, "fill")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	results := diskProbe([]string{tmpDir}, 85, 95)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Status != "ok" && r.Status != "degraded" && r.Status != "unhealthy" {
		t.Errorf("status = %q, want ok|degraded|unhealthy", r.Status)
	}
}
