package job

import (
	"context"
	"fmt"
	"testing"
)

// fakeCleanupRecorder satisfies backupCleanupRecorder.
type fakeCleanupRecorder struct {
	kindKeys map[string][]string
	keepNs   map[string]int
}

func newFakeCleanupRecorder() *fakeCleanupRecorder {
	return &fakeCleanupRecorder{
		kindKeys: make(map[string][]string),
		keepNs:   make(map[string]int),
	}
}

func (f *fakeCleanupRecorder) ExpireOlderThan(_ context.Context, kind string, keepN int) ([]string, error) {
	f.keepNs[kind] = keepN
	all := f.kindKeys[kind]
	if keepN >= len(all) {
		return nil, nil
	}
	return all[keepN:], nil
}

func TestBackupCleanupProcessor_Type(t *testing.T) {
	p := NewBackupCleanupProcessor(BackupCleanupProcessorOpts{})
	if p.Type() != JobTypeBackupCleanup {
		t.Errorf("expected %s, got %s", JobTypeBackupCleanup, p.Type())
	}
}

func TestBackupCleanupProcessor_Defaults(t *testing.T) {
	p := NewBackupCleanupProcessor(BackupCleanupProcessorOpts{})
	if p.retentionDaily != 14 {
		t.Errorf("expected default daily=14, got %d", p.retentionDaily)
	}
	if p.retentionWeekly != 8 {
		t.Errorf("expected default weekly=8, got %d", p.retentionWeekly)
	}
	if p.retentionMonthly != 12 {
		t.Errorf("expected default monthly=12, got %d", p.retentionMonthly)
	}
}

func TestBackupCleanupProcessor_RespectsRetention(t *testing.T) {
	rec := newFakeCleanupRecorder()
	s3 := &fakeS3{}

	for i := 0; i < 20; i++ {
		rec.kindKeys["daily"] = append(rec.kindKeys["daily"], fmt.Sprintf("daily/run%02d.dump", i))
	}
	for i := 0; i < 10; i++ {
		rec.kindKeys["weekly"] = append(rec.kindKeys["weekly"], fmt.Sprintf("weekly/run%02d.dump", i))
	}
	for i := 0; i < 15; i++ {
		rec.kindKeys["monthly"] = append(rec.kindKeys["monthly"], fmt.Sprintf("monthly/run%02d.dump", i))
	}

	p := &BackupCleanupProcessor{
		store:            rec,
		uploader:         s3,
		bucket:           "backup-bucket",
		retentionDaily:   14,
		retentionWeekly:  8,
		retentionMonthly: 12,
		logger:           testLogger(),
	}

	if err := p.Process(context.Background(), nil); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if rec.keepNs["daily"] != 14 {
		t.Errorf("expected keepN=14 for daily, got %d", rec.keepNs["daily"])
	}
	if rec.keepNs["weekly"] != 8 {
		t.Errorf("expected keepN=8 for weekly, got %d", rec.keepNs["weekly"])
	}
	if rec.keepNs["monthly"] != 12 {
		t.Errorf("expected keepN=12 for monthly, got %d", rec.keepNs["monthly"])
	}

	// 20 daily - 14 = 6 expired
	// 10 weekly - 8 = 2 expired
	// 15 monthly - 12 = 3 expired
	// total = 11 S3 deletes
	totalExpected := (20 - 14) + (10 - 8) + (15 - 12)
	if len(s3.deleted) != totalExpected {
		t.Errorf("expected %d S3 deletes, got %d: %v", totalExpected, len(s3.deleted), s3.deleted)
	}
}

func TestBackupCleanupProcessor_NoExpiredWhenBelowRetention(t *testing.T) {
	rec := newFakeCleanupRecorder()
	s3 := &fakeS3{}

	for i := 0; i < 5; i++ {
		rec.kindKeys["daily"] = append(rec.kindKeys["daily"], fmt.Sprintf("daily/run%02d.dump", i))
	}

	p := &BackupCleanupProcessor{
		store:            rec,
		uploader:         s3,
		bucket:           "backup-bucket",
		retentionDaily:   14,
		retentionWeekly:  8,
		retentionMonthly: 12,
		logger:           testLogger(),
	}

	if err := p.Process(context.Background(), nil); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if len(s3.deleted) != 0 {
		t.Errorf("expected 0 S3 deletes (5 < 14), got %d", len(s3.deleted))
	}
}
