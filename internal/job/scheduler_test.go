package job

import (
	"testing"
	"time"
)

func TestShouldFire_Weekly(t *testing.T) {
	monday2am := time.Date(2026, 3, 23, 2, 0, 0, 0, time.UTC)
	if !shouldFire("@weekly", monday2am) {
		t.Error("expected @weekly to fire on Monday 2:00 AM")
	}

	tuesday2am := time.Date(2026, 3, 24, 2, 0, 0, 0, time.UTC)
	if shouldFire("@weekly", tuesday2am) {
		t.Error("expected @weekly to not fire on Tuesday")
	}
}

func TestShouldFire_Monthly(t *testing.T) {
	first2am := time.Date(2026, 4, 1, 2, 0, 0, 0, time.UTC)
	if !shouldFire("@monthly", first2am) {
		t.Error("expected @monthly to fire on 1st at 2:00 AM")
	}

	second2am := time.Date(2026, 4, 2, 2, 0, 0, 0, time.UTC)
	if shouldFire("@monthly", second2am) {
		t.Error("expected @monthly to not fire on 2nd")
	}
}

func TestCronEntry_Fields(t *testing.T) {
	entry := CronEntry{
		Name:     "test_purge",
		Schedule: "@daily",
		JobType:  JobTypePurgeSweep,
	}

	if entry.Name != "test_purge" {
		t.Errorf("Name = %s, want test_purge", entry.Name)
	}
	if entry.Schedule != "@daily" {
		t.Errorf("Schedule = %s, want @daily", entry.Schedule)
	}
	if entry.JobType != "purge_sweep" {
		t.Errorf("JobType = %s, want purge_sweep", entry.JobType)
	}
}

func TestMatchCronExpr_DayOfWeek(t *testing.T) {
	sunday := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
	if sunday.Weekday() != time.Sunday {
		t.Fatalf("expected Sunday, got %s", sunday.Weekday())
	}

	if !matchCronExpr("0 0 * * 0", sunday) {
		t.Error("expected cron '0 0 * * 0' to match Sunday 0:00")
	}

	if matchCronExpr("0 0 * * 1", sunday) {
		t.Error("expected cron '0 0 * * 1' to NOT match Sunday")
	}
}

func TestMatchCronExpr_MonthField(t *testing.T) {
	march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	if !matchCronExpr("0 0 1 3 *", march) {
		t.Error("expected cron to match March 1st")
	}

	if matchCronExpr("0 0 1 4 *", march) {
		t.Error("expected cron to NOT match March for April spec")
	}
}

func TestFieldMatches_StepWithBase(t *testing.T) {
	if !fieldMatches("5/10", 5, 0, 59) {
		t.Error("5/10 should match 5")
	}
	if !fieldMatches("5/10", 15, 0, 59) {
		t.Error("5/10 should match 15")
	}
	if !fieldMatches("5/10", 25, 0, 59) {
		t.Error("5/10 should match 25")
	}
	if fieldMatches("5/10", 6, 0, 59) {
		t.Error("5/10 should not match 6")
	}
	if fieldMatches("5/10", 3, 0, 59) {
		t.Error("5/10 should not match 3 (below base)")
	}
}
