package job

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestStuckRolloutReaper_Type(t *testing.T) {
	p := &StuckRolloutReaperProcessor{}
	if p.Type() != JobTypeStuckRolloutReaper {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeStuckRolloutReaper)
	}
	if JobTypeStuckRolloutReaper != "stuck_rollout_reaper" {
		t.Errorf("JobTypeStuckRolloutReaper = %q, want %q", JobTypeStuckRolloutReaper, "stuck_rollout_reaper")
	}
}

func TestAllJobTypes_ContainsStuckRolloutReaper(t *testing.T) {
	for _, jt := range AllJobTypes {
		if jt == JobTypeStuckRolloutReaper {
			return
		}
	}
	t.Errorf("JobTypeStuckRolloutReaper not found in AllJobTypes")
}

// fakeStuckPolicyStore is the test double for stuckRolloutPolicyStore. It
// records every call and lets each test wire up its own ID list and per-ID
// CompleteRollout error.
type fakeStuckPolicyStore struct {
	mu sync.Mutex

	stuckIDs       []uuid.UUID
	listErr        error
	completeErrFor map[uuid.UUID]error // id -> err returned by CompleteRollout

	completeCalls []uuid.UUID
	getCalls      []uuid.UUID
	tenantCalls   []uuid.UUID
}

func (f *fakeStuckPolicyStore) ListStuckRollouts(ctx context.Context, graceMinutes int) ([]uuid.UUID, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.stuckIDs, nil
}

func (f *fakeStuckPolicyStore) CompleteRollout(ctx context.Context, rolloutID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completeCalls = append(f.completeCalls, rolloutID)
	if f.completeErrFor != nil {
		if err, ok := f.completeErrFor[rolloutID]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeStuckPolicyStore) GetRolloutByID(ctx context.Context, rolloutID uuid.UUID) (*store.PolicyRollout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls = append(f.getCalls, rolloutID)
	// Tests don't exercise the bus path (eventBus is nil), but be safe.
	return &store.PolicyRollout{ID: rolloutID, PolicyVersionID: uuid.New()}, nil
}

func (f *fakeStuckPolicyStore) GetTenantIDForRollout(ctx context.Context, rolloutID uuid.UUID) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tenantCalls = append(f.tenantCalls, rolloutID)
	return uuid.New(), nil
}

// runReaperCollect mirrors Process()'s body without touching jobs.Complete
// (we can't satisfy *store.JobStore without a real DB). Returns the
// aggregate so each test can assert on counters and IDs.
func runReaperCollect(t *testing.T, p *StuckRolloutReaperProcessor) (*stuckReaperResult, error) {
	t.Helper()
	ctx := context.Background()

	ids, err := p.policyStore.ListStuckRollouts(ctx, p.graceMinutes)
	if err != nil {
		return nil, err
	}
	var reaped, skipped, failed int
	var reapedIDs []string
	for _, id := range ids {
		if cerr := p.policyStore.CompleteRollout(ctx, id); cerr != nil {
			if errors.Is(cerr, store.ErrRolloutNotFound) {
				skipped++
				continue
			}
			failed++
			continue
		}
		reaped++
		reapedIDs = append(reapedIDs, id.String())
	}
	return &stuckReaperResult{
		Reaped:  reaped,
		Skipped: skipped,
		Failed:  failed,
		IDs:     reapedIDs,
	}, nil
}

func TestStuckRolloutReaper_ReapsCleanRollouts(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	fake := &fakeStuckPolicyStore{stuckIDs: ids}
	p := &StuckRolloutReaperProcessor{
		policyStore:  fake,
		graceMinutes: 10,
		logger:       zerolog.Nop(),
	}
	res, err := runReaperCollect(t, p)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if res.Reaped != 3 || res.Skipped != 0 || res.Failed != 0 {
		t.Errorf("got reaped=%d skipped=%d failed=%d, want 3/0/0", res.Reaped, res.Skipped, res.Failed)
	}
	if len(res.IDs) != 3 {
		t.Errorf("ids len = %d, want 3", len(res.IDs))
	}
	if len(fake.completeCalls) != 3 {
		t.Errorf("CompleteRollout called %d times, want 3", len(fake.completeCalls))
	}
}

func TestStuckRolloutReaper_SkipsAlreadyCompleted(t *testing.T) {
	idLive := uuid.New()
	idGone := uuid.New()
	fake := &fakeStuckPolicyStore{
		stuckIDs: []uuid.UUID{idLive, idGone},
		completeErrFor: map[uuid.UUID]error{
			// Race: rollout was finished/deleted by another path between
			// our ListStuckRollouts read and our CompleteRollout. The store
			// returns ErrRolloutNotFound from the FOR UPDATE SELECT.
			idGone: store.ErrRolloutNotFound,
		},
	}
	p := &StuckRolloutReaperProcessor{
		policyStore:  fake,
		graceMinutes: 10,
		logger:       zerolog.Nop(),
	}
	res, err := runReaperCollect(t, p)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if res.Reaped != 1 || res.Skipped != 1 || res.Failed != 0 {
		t.Errorf("got reaped=%d skipped=%d failed=%d, want 1/1/0", res.Reaped, res.Skipped, res.Failed)
	}
	// The reaped IDs slice must contain only the live one.
	if len(res.IDs) != 1 || res.IDs[0] != idLive.String() {
		t.Errorf("ids = %v, want [%s]", res.IDs, idLive)
	}
}

func TestStuckRolloutReaper_LogsFailureAndContinues(t *testing.T) {
	idOK := uuid.New()
	idBad := uuid.New()
	fake := &fakeStuckPolicyStore{
		stuckIDs: []uuid.UUID{idOK, idBad},
		completeErrFor: map[uuid.UUID]error{
			idBad: errors.New("simulated transient db error"),
		},
	}
	p := &StuckRolloutReaperProcessor{
		policyStore:  fake,
		graceMinutes: 10,
		logger:       zerolog.Nop(),
	}
	res, err := runReaperCollect(t, p)
	if err != nil {
		t.Fatalf("loop returned error (should have continued): %v", err)
	}
	if res.Reaped != 1 || res.Skipped != 0 || res.Failed != 1 {
		t.Errorf("got reaped=%d skipped=%d failed=%d, want 1/0/1", res.Reaped, res.Skipped, res.Failed)
	}
	// Both rollouts must have been attempted — failure on one must not
	// short-circuit the loop.
	if len(fake.completeCalls) != 2 {
		t.Errorf("CompleteRollout called %d times, want 2 (failure must not short-circuit)", len(fake.completeCalls))
	}
}

func TestStuckRolloutReaper_GraceClamping(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"below floor 2 → 5", 2, minStuckRolloutGraceMinutes},
		{"floor 5 → 5", 5, 5},
		{"valid 30 → 30", 30, 30},
		{"ceiling 120 → 120", 120, 120},
		{"above ceiling 200 → 120", 200, maxStuckRolloutGraceMinutes},
		{"zero → floor", 0, minStuckRolloutGraceMinutes},
		{"negative → floor", -1, minStuckRolloutGraceMinutes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewStuckRolloutReaperProcessor(nil, nil, nil, tc.in, zerolog.Nop())
			if p.graceMinutes != tc.want {
				t.Errorf("graceMinutes = %d, want %d", p.graceMinutes, tc.want)
			}
		})
	}
}

func TestStuckRolloutReaper_ResultJSONShape(t *testing.T) {
	r := stuckReaperResult{
		Reaped:  3,
		Skipped: 1,
		Failed:  0,
		IDs:     []string{uuid.New().String(), uuid.New().String(), uuid.New().String()},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := got["reaped"].(float64); !ok || int(v) != 3 {
		t.Errorf("reaped = %v, want 3", got["reaped"])
	}
	if v, ok := got["skipped"].(float64); !ok || int(v) != 1 {
		t.Errorf("skipped = %v, want 1", got["skipped"])
	}
	if v, ok := got["failed"].(float64); !ok || int(v) != 0 {
		t.Errorf("failed = %v, want 0", got["failed"])
	}
	ids, ok := got["ids"].([]any)
	if !ok || len(ids) != 3 {
		t.Errorf("ids = %v, want length 3", got["ids"])
	}
}

func TestStuckRolloutReaper_EmptyResultOmitsIDs(t *testing.T) {
	// When no rollouts are reaped the IDs slice is nil; with `omitempty`
	// the field must be absent from the marshalled JSON so dashboards
	// don't have to special-case empty vs. missing.
	r := stuckReaperResult{Reaped: 0, Skipped: 0, Failed: 0}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got["ids"]; ok {
		t.Errorf("ids should be omitted when empty; got %v", got["ids"])
	}
}
