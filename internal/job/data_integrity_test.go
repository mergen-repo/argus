package job

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
)

// --- fake DB ---

// fakeDIExecResult implements pgconn.CommandTag interface subset.
type fakeDIExecResult struct {
	rowsAffected int64
}

func (f fakeDIExecResult) RowsAffected() int64 { return f.rowsAffected }

// fakeDIRow implements pgx.Row for QueryRow results.
type fakeDIRow struct {
	val int
	err error
}

func (r *fakeDIRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > 0 {
		if p, ok := dest[0].(*int); ok {
			*p = r.val
		}
	}
	return nil
}

// fakeDIDB is a configurable fake for the diDB interface.
// execResults is a queue consumed in order for each Exec call.
// queryRowValues is a queue consumed in order for each QueryRow call.
type fakeDIDB struct {
	execResults    []pgconn.CommandTag
	execErrs       []error
	execIdx        int
	queryRowValues []int
	queryRowErrs   []error
	queryRowIdx    int
}

func (f *fakeDIDB) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	i := f.execIdx
	f.execIdx++
	if i < len(f.execErrs) && f.execErrs[i] != nil {
		return pgconn.CommandTag{}, f.execErrs[i]
	}
	if i < len(f.execResults) {
		return f.execResults[i], nil
	}
	return pgconn.NewCommandTag("INSERT 0 0"), nil
}

func (f *fakeDIDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	i := f.queryRowIdx
	f.queryRowIdx++
	var val int
	var err error
	if i < len(f.queryRowValues) {
		val = f.queryRowValues[i]
	}
	if i < len(f.queryRowErrs) {
		err = f.queryRowErrs[i]
	}
	return &fakeDIRow{val: val, err: err}
}

// fakeDataIntegrityJobStore is a minimal in-memory dataIntegrityJobStore.
type fakeDataIntegrityJobStore struct {
	completedJobs []uuid.UUID
	results       []json.RawMessage
}

func (f *fakeDataIntegrityJobStore) Complete(_ context.Context, jobID uuid.UUID, _ json.RawMessage, result json.RawMessage) error {
	f.completedJobs = append(f.completedJobs, jobID)
	f.results = append(f.results, result)
	return nil
}

// fakeDataIntegrityMetrics records IncDataIntegrity calls.
type fakeDataIntegrityMetrics struct {
	calls map[string]float64
}

func newFakeDataIntegrityMetrics() *fakeDataIntegrityMetrics {
	return &fakeDataIntegrityMetrics{calls: make(map[string]float64)}
}

// helper to build a DataIntegrityDetector wired to fakes.
func newTestDIDetector(
	db diDB,
	jobs dataIntegrityJobStore,
	bus busPublisher,
	buf *bytes.Buffer,
) *DataIntegrityDetector {
	logger := zerolog.New(buf)
	return &DataIntegrityDetector{
		db:       db,
		jobs:     jobs,
		eventBus: bus,
		metrics:  nil,
		logger:   logger,
	}
}

func makeTestDIJob() *store.Job {
	return &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Type:     JobTypeDataIntegrityScan,
	}
}

// TestDataIntegrityDetector_Type verifies the Type() constant.
func TestDataIntegrityDetector_Type(t *testing.T) {
	d := &DataIntegrityDetector{}
	if d.Type() != JobTypeDataIntegrityScan {
		t.Errorf("Type() = %q, want %q", d.Type(), JobTypeDataIntegrityScan)
	}
	if JobTypeDataIntegrityScan != "data_integrity_scan" {
		t.Errorf("JobTypeDataIntegrityScan = %q, want %q", JobTypeDataIntegrityScan, "data_integrity_scan")
	}
}

// TestDataIntegrityDetector_Run_EmitsDebugOnCleanDB verifies that when all 4 scans
// return zero counts, no WARN is emitted and all 4 DEBUG log lines appear.
func TestDataIntegrityDetector_Run_EmitsDebugOnCleanDB(t *testing.T) {
	db := &fakeDIDB{
		execResults:    []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 0"), pgconn.NewCommandTag("INSERT 0 0")},
		queryRowValues: []int{0, 0},
	}
	jobs := &fakeDataIntegrityJobStore{}
	bus := &fakeBusPublisher{}

	var buf bytes.Buffer
	d := newTestDIDetector(db, jobs, bus, &buf)

	if err := d.Process(context.Background(), makeTestDIJob()); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out := buf.String()

	for _, kind := range []string{kindNegDurationSession, kindNegDurationCDR, kindFramedIPOutsidePool, kindIMSIMalformed} {
		if !bytes.Contains([]byte(out), []byte(kind)) {
			t.Errorf("expected kind %q in DEBUG log output, got: %s", kind, out)
		}
	}

	if bytes.Contains([]byte(out), []byte(`"level":"warn"`)) {
		t.Errorf("expected no WARN log on clean DB, got: %s", out)
	}

	if len(jobs.completedJobs) != 1 {
		t.Errorf("expected 1 job completed, got %d", len(jobs.completedJobs))
	}
}

// TestDataIntegrityDetector_Run_EmitsWarnOnViolations verifies that WARN is emitted
// when any count > 0, and the job is still completed.
func TestDataIntegrityDetector_Run_EmitsWarnOnViolations(t *testing.T) {
	db := &fakeDIDB{
		execResults:    []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 3"), pgconn.NewCommandTag("INSERT 0 0")},
		queryRowValues: []int{0, 0},
	}
	jobs := &fakeDataIntegrityJobStore{}
	bus := &fakeBusPublisher{}

	var buf bytes.Buffer
	d := newTestDIDetector(db, jobs, bus, &buf)

	if err := d.Process(context.Background(), makeTestDIJob()); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte(`"level":"warn"`)) {
		t.Errorf("expected WARN log on violations, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte(kindNegDurationSession)) {
		t.Errorf("expected neg_duration_session kind in warn log, got: %s", out)
	}
}

// TestDataIntegrityDetector_Run_ReportsIMSIViolations verifies that
// imsi_malformed count is read correctly from query #4 and emits WARN.
func TestDataIntegrityDetector_Run_ReportsIMSIViolations(t *testing.T) {
	const imsiViolations = 7

	db := &fakeDIDB{
		execResults:    []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 0"), pgconn.NewCommandTag("INSERT 0 0")},
		queryRowValues: []int{0, imsiViolations},
	}
	jobs := &fakeDataIntegrityJobStore{}
	bus := &fakeBusPublisher{}

	var buf bytes.Buffer
	d := newTestDIDetector(db, jobs, bus, &buf)

	if err := d.Process(context.Background(), makeTestDIJob()); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte(kindIMSIMalformed)) {
		t.Errorf("expected imsi_malformed in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte(`"level":"warn"`)) {
		t.Errorf("expected WARN on imsi_malformed violations, got: %s", out)
	}

	if len(jobs.results) == 0 {
		t.Fatal("expected job result to be recorded")
	}
	var result map[string]any
	if err := json.Unmarshal(jobs.results[0], &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	countsRaw, ok := result["counts"]
	if !ok {
		t.Fatal("result missing 'counts' key")
	}
	countsMap, ok := countsRaw.(map[string]any)
	if !ok {
		t.Fatalf("counts is not a map: %T", countsRaw)
	}
	got, _ := countsMap[kindIMSIMalformed].(float64)
	if int(got) != imsiViolations {
		t.Errorf("imsi_malformed count = %d, want %d", int(got), imsiViolations)
	}
}

// TestDataIntegrityDetector_Run_ReportsFramedIPOutsidePool verifies that the
// framed_ip_outside_pool count (query #3) is read correctly from queryRowValues[0]
// and emits WARN + metric increment with kind="framed_ip_outside_pool".
func TestDataIntegrityDetector_Run_ReportsFramedIPOutsidePool(t *testing.T) {
	const ipOutsideViolations = 4

	db := &fakeDIDB{
		execResults:    []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 0"), pgconn.NewCommandTag("INSERT 0 0")},
		queryRowValues: []int{ipOutsideViolations, 0},
	}
	jobs := &fakeDataIntegrityJobStore{}
	bus := &fakeBusPublisher{}
	var buf bytes.Buffer

	reg := metrics.NewRegistry()
	d := &DataIntegrityDetector{
		db:       db,
		jobs:     jobs,
		eventBus: bus,
		logger:   zerolog.New(&buf),
		metrics:  reg,
	}

	if err := d.Process(context.Background(), makeTestDIJob()); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte(kindFramedIPOutsidePool)) {
		t.Errorf("expected framed_ip_outside_pool kind in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte(`"level":"warn"`)) {
		t.Errorf("expected WARN on framed_ip_outside_pool violations, got: %s", out)
	}

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()

	body := scrapeMetrics(t, srv.URL)
	want := `argus_data_integrity_violations_total{kind="framed_ip_outside_pool"} 4`
	if !strings.Contains(body, want) {
		t.Errorf("expected metric line %q\nfull metrics:\n%s", want, body)
	}

	if len(jobs.results) == 0 {
		t.Fatal("expected job result to be recorded")
	}
	var result map[string]any
	if err := json.Unmarshal(jobs.results[0], &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	countsRaw, ok := result["counts"]
	if !ok {
		t.Fatal("result missing 'counts' key")
	}
	countsMap, ok := countsRaw.(map[string]any)
	if !ok {
		t.Fatalf("counts is not a map: %T", countsRaw)
	}
	got, _ := countsMap[kindFramedIPOutsidePool].(float64)
	if int(got) != ipOutsideViolations {
		t.Errorf("framed_ip_outside_pool count = %d, want %d", int(got), ipOutsideViolations)
	}
}

// TestDataIntegrityDetector_Run_MetricIncrement verifies that IncDataIntegrity
// is called with the correct kind and count when violations are found.
func TestDataIntegrityDetector_Run_MetricIncrement(t *testing.T) {
	const nIMSI = 5

	db := &fakeDIDB{
		execResults:    []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 0"), pgconn.NewCommandTag("INSERT 0 0")},
		queryRowValues: []int{0, nIMSI},
	}
	jobs := &fakeDataIntegrityJobStore{}
	bus := &fakeBusPublisher{}
	var buf bytes.Buffer

	reg := metrics.NewRegistry()
	d := &DataIntegrityDetector{
		db:       db,
		jobs:     jobs,
		eventBus: bus,
		logger:   zerolog.New(&buf),
		metrics:  reg,
	}

	if err := d.Process(context.Background(), makeTestDIJob()); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()

	body := scrapeMetrics(t, srv.URL)
	want := `argus_data_integrity_violations_total{kind="imsi_malformed"} 5`
	if !strings.Contains(body, want) {
		t.Errorf("expected metric line %q\nfull metrics:\n%s", want, body)
	}
}

// TestDataIntegrityDetector_Run_BoundedScan verifies the 24h window predicate
// appears in the SQL strings embedded in the processor (compile-time constant check).
func TestDataIntegrityDetector_Run_BoundedScan(t *testing.T) {
	const marker = "INTERVAL '24 hours'"
	src := qNegSessionSQL + qNegCDRSQL + qIPOutsideSQL
	if !bytes.Contains([]byte(src), []byte(marker)) {
		t.Errorf("expected %q in scan SQL, bounded scan missing", marker)
	}
}

// TestAllJobTypes_ContainsDataIntegrityScan verifies the constant is registered
// in AllJobTypes so the scheduler-runner wiring is complete.
func TestAllJobTypes_ContainsDataIntegrityScan(t *testing.T) {
	for _, jt := range AllJobTypes {
		if jt == JobTypeDataIntegrityScan {
			return
		}
	}
	t.Errorf("JobTypeDataIntegrityScan not found in AllJobTypes")
}
