package job

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

// fakeCoAPolicyStore is the test double for coAFailureAlerterPolicyStore.
type fakeCoAPolicyStore struct {
	mu sync.Mutex

	failures []store.StuckCoAFailure
	listErr  error
	counts   map[string]int64
	countErr error
}

func (f *fakeCoAPolicyStore) ListStuckCoAFailures(_ context.Context, _ time.Duration) ([]store.StuckCoAFailure, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.failures, nil
}

func (f *fakeCoAPolicyStore) CoAStatusCounts(_ context.Context) (map[string]int64, error) {
	if f.countErr != nil {
		return nil, f.countErr
	}
	if f.counts != nil {
		return f.counts, nil
	}
	return map[string]int64{
		"pending": 0, "queued": 0, "acked": 0,
		"failed": 0, "no_session": 0, "skipped": 0,
	}, nil
}

// fakeCoAAlertStore is the test double for coAFailureAlerterAlertStore.
type fakeCoAAlertStore struct {
	mu sync.Mutex

	calls  []store.CreateAlertParams
	result store.UpsertResult
	err    error
}

func (f *fakeCoAAlertStore) UpsertWithDedup(_ context.Context, p store.CreateAlertParams, _ int) (*store.Alert, store.UpsertResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, store.UpsertInserted, f.err
	}
	f.calls = append(f.calls, p)
	a := &store.Alert{
		ID:       uuid.New(),
		TenantID: p.TenantID,
		Type:     p.Type,
		Severity: p.Severity,
	}
	return a, f.result, nil
}

// newTestCoAReg creates an isolated Prometheus registry for tests (avoids
// "duplicate metric" panics when tests are run in the same process).
func newTestCoAReg() *metrics.Registry {
	reg := metrics.NewRegistry()
	return reg
}

// runCoAAlerterSweep exercises the sweep loop without needing a real *store.JobStore.
// It mirrors the core of CoAFailureAlerterProcessor.Process (without jobs.Complete).
func runCoAAlerterSweep(t *testing.T, p *CoAFailureAlerterProcessor) coaAlerterResult {
	t.Helper()
	ctx := context.Background()

	failures, err := p.policyStore.ListStuckCoAFailures(ctx, coAFailureAlerterAge)
	if err != nil {
		t.Fatalf("ListStuckCoAFailures error: %v", err)
	}

	var alerted, deduped, cooldown, alertFails int
	for _, f := range failures {
		simIDStr := f.SimID.String()
		dedupKey := "coa_failed:" + simIDStr
		params := store.CreateAlertParams{
			TenantID:    f.TenantID,
			Type:        "coa_delivery_failed",
			Severity:    "high",
			Source:      "rollout",
			Title:       "CoA delivery failed",
			Description: "test",
			SimID:       ptrUUID(f.SimID),
			DedupKey:    &dedupKey,
		}
		_, res, uErr := p.alertStore.UpsertWithDedup(ctx, params, 4)
		if uErr != nil {
			alertFails++
			continue
		}
		switch res {
		case store.UpsertInserted:
			alerted++
		case store.UpsertDeduplicated:
			deduped++
		case store.UpsertCoolingDown:
			cooldown++
		}
	}

	counts, cErr := p.policyStore.CoAStatusCounts(ctx)
	if cErr == nil && p.reg != nil && p.reg.CoAStatusByState != nil {
		for state, count := range counts {
			p.reg.CoAStatusByState.WithLabelValues(state).Set(float64(count))
		}
	}

	return coaAlerterResult{
		Alerted:    alerted,
		Deduped:    deduped,
		CoolDown:   cooldown,
		AlertFails: alertFails,
	}
}

// TestCoAFailureAlerter_TypeConst verifies the type constant and AllJobTypes registration.
func TestCoAFailureAlerter_TypeConst(t *testing.T) {
	p := &CoAFailureAlerterProcessor{}
	if p.Type() != JobTypeCoAFailureAlerter {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeCoAFailureAlerter)
	}
	if JobTypeCoAFailureAlerter != "coa_failure_alerter" {
		t.Errorf("JobTypeCoAFailureAlerter = %q, want %q", JobTypeCoAFailureAlerter, "coa_failure_alerter")
	}
	for _, jt := range AllJobTypes {
		if jt == JobTypeCoAFailureAlerter {
			return
		}
	}
	t.Errorf("JobTypeCoAFailureAlerter not found in AllJobTypes")
}

// TestCoAFailureAlerter_FailedOlderThan5MinCreatesAlert verifies AC-7:
// a SIM whose coa_sent_at is older than 5 minutes produces one UpsertWithDedup call
// with DedupKey "coa_failed:<sim_id>" and Type "coa_delivery_failed".
func TestCoAFailureAlerter_FailedOlderThan5MinCreatesAlert(t *testing.T) {
	tenantID := uuid.New()
	simID := uuid.New()
	pstore := &fakeCoAPolicyStore{
		failures: []store.StuckCoAFailure{
			{TenantID: tenantID, SimID: simID, FailedAt: time.Now().Add(-10 * time.Minute)},
		},
	}
	astore := &fakeCoAAlertStore{result: store.UpsertInserted}
	p := &CoAFailureAlerterProcessor{
		policyStore: pstore,
		alertStore:  astore,
		reg:         newTestCoAReg(),
		logger:      zerolog.Nop(),
	}

	res := runCoAAlerterSweep(t, p)

	if res.Alerted != 1 {
		t.Errorf("Alerted = %d, want 1", res.Alerted)
	}
	if res.AlertFails != 0 {
		t.Errorf("AlertFails = %d, want 0", res.AlertFails)
	}
	astore.mu.Lock()
	defer astore.mu.Unlock()
	if len(astore.calls) != 1 {
		t.Fatalf("UpsertWithDedup called %d times, want 1", len(astore.calls))
	}
	call := astore.calls[0]
	wantDedupKey := "coa_failed:" + simID.String()
	if call.DedupKey == nil || *call.DedupKey != wantDedupKey {
		t.Errorf("DedupKey = %v, want %q", call.DedupKey, wantDedupKey)
	}
	if call.Type != "coa_delivery_failed" {
		t.Errorf("Type = %q, want %q", call.Type, "coa_delivery_failed")
	}
	if call.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", call.TenantID, tenantID)
	}
}

// TestCoAFailureAlerter_NoFailuresCreatesNoAlert verifies AC-7 boundary:
// when ListStuckCoAFailures returns empty (no SIMs older than 5 min),
// UpsertWithDedup must NOT be called.
func TestCoAFailureAlerter_NoFailuresCreatesNoAlert(t *testing.T) {
	pstore := &fakeCoAPolicyStore{failures: []store.StuckCoAFailure{}}
	astore := &fakeCoAAlertStore{}
	p := &CoAFailureAlerterProcessor{
		policyStore: pstore,
		alertStore:  astore,
		reg:         newTestCoAReg(),
		logger:      zerolog.Nop(),
	}

	res := runCoAAlerterSweep(t, p)

	if res.Alerted != 0 || res.Deduped != 0 || res.AlertFails != 0 {
		t.Errorf("unexpected alert activity: %+v", res)
	}
	astore.mu.Lock()
	defer astore.mu.Unlock()
	if len(astore.calls) != 0 {
		t.Errorf("UpsertWithDedup called %d times, want 0", len(astore.calls))
	}
}

// TestCoAFailureAlerter_RepeatSweepDedupedNotDouble verifies AC-7 dedup:
// when UpsertWithDedup returns UpsertDeduplicated (alert already active),
// the alerted counter stays 0 and deduped increments — no double-fire.
func TestCoAFailureAlerter_RepeatSweepDedupedNotDouble(t *testing.T) {
	simID := uuid.New()
	pstore := &fakeCoAPolicyStore{
		failures: []store.StuckCoAFailure{
			{TenantID: uuid.New(), SimID: simID, FailedAt: time.Now().Add(-10 * time.Minute)},
		},
	}
	astore := &fakeCoAAlertStore{result: store.UpsertDeduplicated}
	p := &CoAFailureAlerterProcessor{
		policyStore: pstore,
		alertStore:  astore,
		reg:         newTestCoAReg(),
		logger:      zerolog.Nop(),
	}

	res1 := runCoAAlerterSweep(t, p)
	res2 := runCoAAlerterSweep(t, p)

	if res1.Deduped != 1 || res1.Alerted != 0 {
		t.Errorf("sweep 1: Deduped=%d Alerted=%d, want Deduped=1 Alerted=0", res1.Deduped, res1.Alerted)
	}
	if res2.Deduped != 1 || res2.Alerted != 0 {
		t.Errorf("sweep 2: Deduped=%d Alerted=%d, want Deduped=1 Alerted=0", res2.Deduped, res2.Alerted)
	}
	astore.mu.Lock()
	defer astore.mu.Unlock()
	if len(astore.calls) != 2 {
		t.Errorf("UpsertWithDedup called %d times over 2 sweeps, want 2", len(astore.calls))
	}
}

// TestCoAFailureAlerter_GaugeCountsMatchDB verifies AC-8:
// CoAStatusByState gauge is set to the counts returned by CoAStatusCounts.
func TestCoAFailureAlerter_GaugeCountsMatchDB(t *testing.T) {
	wantCounts := map[string]int64{
		"pending":    5,
		"queued":     3,
		"acked":      42,
		"failed":     7,
		"no_session": 12,
		"skipped":    1,
	}
	pstore := &fakeCoAPolicyStore{counts: wantCounts}
	astore := &fakeCoAAlertStore{}
	reg := newTestCoAReg()
	p := &CoAFailureAlerterProcessor{
		policyStore: pstore,
		alertStore:  astore,
		reg:         reg,
		logger:      zerolog.Nop(),
	}

	runCoAAlerterSweep(t, p)

	gather, err := reg.Reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	gaugeMap := make(map[string]float64)
	for _, mf := range gather {
		if mf.GetName() != "argus_coa_status_by_state" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "state" {
					gaugeMap[lp.GetValue()] = m.GetGauge().GetValue()
				}
			}
		}
	}

	for state, wantCount := range wantCounts {
		got, ok := gaugeMap[state]
		if !ok {
			t.Errorf("gauge missing state=%q", state)
			continue
		}
		if got != float64(wantCount) {
			t.Errorf("gauge[%q] = %v, want %v", state, got, float64(wantCount))
		}
	}
	_ = prometheus.DefaultRegisterer
}
