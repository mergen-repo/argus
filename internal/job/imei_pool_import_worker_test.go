package job

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// fakeIMEIPoolAdder implements imeiPoolAdder for unit tests (no Postgres needed).
type fakeIMEIPoolAdder struct {
	// existing tracks (imei_or_tac) values already "inserted" for duplicate simulation.
	existing map[string]struct{}
}

func newFakeIMEIPoolAdder(existing ...string) *fakeIMEIPoolAdder {
	f := &fakeIMEIPoolAdder{existing: make(map[string]struct{})}
	for _, v := range existing {
		f.existing[v] = struct{}{}
	}
	return f
}

func (f *fakeIMEIPoolAdder) Add(_ context.Context, _ uuid.UUID, _ store.PoolKind, p store.AddEntryParams) (*store.PoolEntry, error) {
	if _, dup := f.existing[p.IMEIOrTAC]; dup {
		return nil, store.ErrPoolEntryDuplicate
	}
	f.existing[p.IMEIOrTAC] = struct{}{}
	return &store.PoolEntry{IMEIOrTAC: p.IMEIOrTAC}, nil
}

// fakeJobStore only implements the methods BulkIMEIPoolImportProcessor calls.
// The full *store.JobStore is not needed for unit tests that do not call
// Process() end-to-end — we only exercise processRow() directly.

// makeTestProcessor builds a processor backed by a fake store.
func makeTestProcessor(adder *fakeIMEIPoolAdder) *BulkIMEIPoolImportProcessor {
	return &BulkIMEIPoolImportProcessor{
		pool:   adder,
		logger: zerolog.Nop(),
	}
}

// makeIMEITestJob returns a minimal *store.Job with a fresh tenant and no CreatedBy.
func makeIMEITestJob() *store.Job {
	return &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
}

// -------------------------------------------------------------------------
// Test 1: HappyPath
// -------------------------------------------------------------------------

// TestBulkIMEIPoolImport_HappyPath tests 3 rows against the whitelist:
//   - row 1: valid full_imei  → "success"
//   - row 2: valid tac_range  → "success"
//   - row 3: duplicate IMEI   → "imei_pool_duplicate"
func TestBulkIMEIPoolImport_HappyPath(t *testing.T) {
	const validIMEI = "490154203237518"
	const validTAC = "49015420"
	const dupIMEI = "490154203237518"

	adder := newFakeIMEIPoolAdder()
	proc := makeTestProcessor(adder)
	j := makeIMEITestJob()

	tests := []struct {
		row     IMEIPoolImportRowSpec
		want    string
		wantDup bool
	}{
		{row: IMEIPoolImportRowSpec{Kind: "full_imei", IMEIOrTAC: validIMEI}, want: "success"},
		{row: IMEIPoolImportRowSpec{Kind: "tac_range", IMEIOrTAC: validTAC}, want: "success"},
		{row: IMEIPoolImportRowSpec{Kind: "full_imei", IMEIOrTAC: dupIMEI}, want: "imei_pool_duplicate"},
	}

	successCount := 0
	for i, tc := range tests {
		outcome, msg := proc.processRow(context.Background(), j, store.PoolWhitelist, tc.row)
		if outcome != tc.want {
			t.Errorf("row %d: outcome = %q (msg=%q), want %q", i+1, outcome, msg, tc.want)
		}
		if outcome == "success" {
			successCount++
		}
	}

	if successCount != 2 {
		t.Errorf("success_count = %d, want 2", successCount)
	}
}

// -------------------------------------------------------------------------
// Test 2: InvalidIMEILength
// -------------------------------------------------------------------------

// TestBulkIMEIPoolImport_InvalidIMEILength checks that a 14-digit IMEI for
// full_imei kind produces "invalid_imei_length".
func TestBulkIMEIPoolImport_InvalidIMEILength(t *testing.T) {
	proc := makeTestProcessor(newFakeIMEIPoolAdder())
	j := makeIMEITestJob()

	row := IMEIPoolImportRowSpec{Kind: "full_imei", IMEIOrTAC: "49015420323751"} // 14 digits
	outcome, msg := proc.processRow(context.Background(), j, store.PoolWhitelist, row)
	if outcome != "invalid_imei_length" {
		t.Errorf("outcome = %q (msg=%q), want invalid_imei_length", outcome, msg)
	}
}

// -------------------------------------------------------------------------
// Test 3: GreylistMissingQuarantineReason
// -------------------------------------------------------------------------

// TestBulkIMEIPoolImport_GreylistMissingQuarantineReason checks that a valid
// IMEI destined for the greylist without quarantine_reason is rejected with
// "missing_quarantine_reason".
func TestBulkIMEIPoolImport_GreylistMissingQuarantineReason(t *testing.T) {
	proc := makeTestProcessor(newFakeIMEIPoolAdder())
	j := makeIMEITestJob()

	row := IMEIPoolImportRowSpec{
		Kind:      "full_imei",
		IMEIOrTAC: "490154203237518",
		// QuarantineReason deliberately omitted
	}
	outcome, msg := proc.processRow(context.Background(), j, store.PoolGreylist, row)
	if outcome != "missing_quarantine_reason" {
		t.Errorf("outcome = %q (msg=%q), want missing_quarantine_reason", outcome, msg)
	}
}

// -------------------------------------------------------------------------
// Test 4: BlacklistMissingFields
// -------------------------------------------------------------------------

// TestBulkIMEIPoolImport_BlacklistMissingFields checks both missing block_reason
// and missing imported_from (as separate rows) against the blacklist.
func TestBulkIMEIPoolImport_BlacklistMissingFields(t *testing.T) {
	proc := makeTestProcessor(newFakeIMEIPoolAdder())
	j := makeIMEITestJob()

	cases := []struct {
		name string
		row  IMEIPoolImportRowSpec
		want string
	}{
		{
			name: "missing block_reason",
			row: IMEIPoolImportRowSpec{
				Kind:         "full_imei",
				IMEIOrTAC:    "490154203237518",
				ImportedFrom: "manual",
				// BlockReason omitted
			},
			want: "missing_block_reason",
		},
		{
			name: "missing imported_from",
			row: IMEIPoolImportRowSpec{
				Kind:        "full_imei",
				IMEIOrTAC:   "490154203237519",
				BlockReason: "stolen device",
				// ImportedFrom omitted
			},
			want: "missing_imported_from",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, msg := proc.processRow(context.Background(), j, store.PoolBlacklist, tc.row)
			if outcome != tc.want {
				t.Errorf("outcome = %q (msg=%q), want %q", outcome, msg, tc.want)
			}
		})
	}
}

// -------------------------------------------------------------------------
// Test 5: CSVInjectionRejected
// -------------------------------------------------------------------------

// TestBulkIMEIPoolImport_CSVInjectionRejected checks that any field starting
// with =, +, -, @, or tab is rejected with "invalid_csv_injection" regardless
// of other field validity.
func TestBulkIMEIPoolImport_CSVInjectionRejected(t *testing.T) {
	proc := makeTestProcessor(newFakeIMEIPoolAdder())
	j := makeIMEITestJob()

	cases := []struct {
		name string
		row  IMEIPoolImportRowSpec
	}{
		{
			name: "description with formula prefix",
			row: IMEIPoolImportRowSpec{
				Kind:        "full_imei",
				IMEIOrTAC:   "490154203237518",
				Description: "=cmd|/c calc'!A1",
			},
		},
		{
			name: "imei_or_tac with plus prefix",
			row: IMEIPoolImportRowSpec{
				Kind:      "full_imei",
				IMEIOrTAC: "+490154203237518",
			},
		},
		{
			name: "device_model with at prefix",
			row: IMEIPoolImportRowSpec{
				Kind:        "full_imei",
				IMEIOrTAC:   "490154203237518",
				DeviceModel: "@SUM(1+1)",
			},
		},
		{
			name: "quarantine_reason with tab prefix",
			row: IMEIPoolImportRowSpec{
				Kind:             "full_imei",
				IMEIOrTAC:        "490154203237518",
				QuarantineReason: "\tsuspicious",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, msg := proc.processRow(context.Background(), j, store.PoolWhitelist, tc.row)
			if outcome != "invalid_csv_injection" {
				t.Errorf("outcome = %q (msg=%q), want invalid_csv_injection", outcome, msg)
			}
		})
	}
}

// -------------------------------------------------------------------------
// JSON round-trip — verifies BulkIMEIPoolImportResult serialises correctly.
// -------------------------------------------------------------------------

func TestBulkIMEIPoolImportResult_JSONRoundtrip(t *testing.T) {
	result := BulkIMEIPoolImportResult{
		Total:        3,
		SuccessCount: 2,
		FailedCount:  1,
		Rows: []IMEIPoolImportRowResult{
			{RowNumber: 1, Outcome: "success"},
			{RowNumber: 2, Outcome: "success"},
			{RowNumber: 3, Outcome: "imei_pool_duplicate", Message: "already exists"},
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got BulkIMEIPoolImportResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Total != 3 || got.SuccessCount != 2 || got.FailedCount != 1 {
		t.Errorf("counts mismatch: %+v", got)
	}
	if len(got.Rows) != 3 {
		t.Errorf("rows len = %d, want 3", len(got.Rows))
	}
	if got.Rows[2].Outcome != "imei_pool_duplicate" {
		t.Errorf("row 3 outcome = %q, want imei_pool_duplicate", got.Rows[2].Outcome)
	}
}

// -------------------------------------------------------------------------
// hasCSVInjectionPrefix unit checks.
// -------------------------------------------------------------------------

func TestHasCSVInjectionPrefix(t *testing.T) {
	yes := []string{"=SUM(1)", "+1", "-1", "@user", "\tcell"}
	no := []string{"", "normal", "490154203237518", "49015420"}

	for _, s := range yes {
		if !hasCSVInjectionPrefix(s) {
			t.Errorf("hasCSVInjectionPrefix(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if hasCSVInjectionPrefix(s) {
			t.Errorf("hasCSVInjectionPrefix(%q) = true, want false", s)
		}
	}
}

// TestBulkIMEIPoolImportProcessor_Type (STORY-095 Gate F-A1) verifies the
// processor returns the registered JobType so jobRunner.Register() correctly
// indexes it. Regression guard: in STORY-095 first ship the worker constructor
// existed but the Register call was missing from cmd/argus/main.go, causing
// AC-5 to silently fail in production.
func TestBulkIMEIPoolImportProcessor_Type(t *testing.T) {
	proc := makeTestProcessor(newFakeIMEIPoolAdder())
	if proc.Type() != JobTypeBulkIMEIPoolImport {
		t.Errorf("Type() = %q, want %q", proc.Type(), JobTypeBulkIMEIPoolImport)
	}
	if JobTypeBulkIMEIPoolImport != "bulk_imei_pool_import" {
		t.Errorf("JobTypeBulkIMEIPoolImport drift = %q, want %q",
			JobTypeBulkIMEIPoolImport, "bulk_imei_pool_import")
	}
}

// TestBulkIMEIPoolImport_RegisteredInAllJobTypes (STORY-095 Gate F-A1) keeps
// the type registered in AllJobTypes so the dispatcher hot-path can match
// queued messages to the processor.
func TestBulkIMEIPoolImport_RegisteredInAllJobTypes(t *testing.T) {
	for _, jt := range AllJobTypes {
		if jt == JobTypeBulkIMEIPoolImport {
			return
		}
	}
	t.Fatalf("JobTypeBulkIMEIPoolImport (%q) not found in AllJobTypes", JobTypeBulkIMEIPoolImport)
}

// ensure ErrPoolEntryDuplicate sentinel is properly threaded through the fake.
func TestFakeAdder_Duplicate(t *testing.T) {
	adder := newFakeIMEIPoolAdder("490154203237518")
	_, err := adder.Add(context.Background(), uuid.New(), store.PoolWhitelist, store.AddEntryParams{
		Kind:      "full_imei",
		IMEIOrTAC: "490154203237518",
	})
	if !errors.Is(err, store.ErrPoolEntryDuplicate) {
		t.Errorf("expected ErrPoolEntryDuplicate, got %v", err)
	}
}
