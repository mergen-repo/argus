package rollout

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// --- Test doubles (interfaces defined in coa_session_resender.go) ---

type stubCoAStoreReader struct {
	coaStatus string
	coaSentAt *time.Time
	err       error
}

func (s *stubCoAStoreReader) GetAssignmentBySIMForResend(_ context.Context, _ uuid.UUID) (string, *time.Time, error) {
	return s.coaStatus, s.coaSentAt, s.err
}

type stubCoAResendDispatcher struct {
	calls  int
	simIDs []uuid.UUID
	err    error
}

func (s *stubCoAResendDispatcher) ResendCoA(_ context.Context, simID uuid.UUID) error {
	s.calls++
	s.simIDs = append(s.simIDs, simID)
	return s.err
}

// --- Helpers ---

func buildLegacyPayload(tenantID, simID string) []byte {
	b, _ := json.Marshal(map[string]string{
		"tenant_id": tenantID,
		"sim_id":    simID,
	})
	return b
}

// --- Test cases ---

// (a) handler skips when coa_status != no_session (e.g., "acked")
func TestResender_SkipsWhenStatusNotNoSession(t *testing.T) {
	store := &stubCoAStoreReader{coaStatus: CoAStatusAcked}
	svc := &stubCoAResendDispatcher{}
	r := NewResender(store, svc, zerolog.Nop())

	tenantID := uuid.New().String()
	simID := uuid.New().String()
	r.handle(context.Background(), "", buildLegacyPayload(tenantID, simID))

	if svc.calls != 0 {
		t.Errorf("expected 0 ResendCoA calls for status=%q, got %d", CoAStatusAcked, svc.calls)
	}
}

// (b) handler skips when coa_status == no_session AND coa_sent_at is within dedup window (< 60s ago)
func TestResender_SkipsWithinDedupWindow(t *testing.T) {
	recentTime := time.Now().Add(-30 * time.Second)
	store := &stubCoAStoreReader{
		coaStatus: CoAStatusNoSession,
		coaSentAt: &recentTime,
	}
	svc := &stubCoAResendDispatcher{}
	r := NewResender(store, svc, zerolog.Nop())

	tenantID := uuid.New().String()
	simID := uuid.New().String()
	r.handle(context.Background(), "", buildLegacyPayload(tenantID, simID))

	if svc.calls != 0 {
		t.Errorf("expected 0 ResendCoA calls within dedup window, got %d", svc.calls)
	}
}

// (c) handler dispatches when coa_status == no_session AND coa_sent_at IS NULL
func TestResender_DispatchesWhenNoSessionAndNullSentAt(t *testing.T) {
	store := &stubCoAStoreReader{
		coaStatus: CoAStatusNoSession,
		coaSentAt: nil,
	}
	svc := &stubCoAResendDispatcher{}
	r := NewResender(store, svc, zerolog.Nop())

	tenantID := uuid.New().String()
	simID := uuid.New()
	r.handle(context.Background(), "", buildLegacyPayload(tenantID, simID.String()))

	if svc.calls != 1 {
		t.Errorf("expected 1 ResendCoA call, got %d", svc.calls)
	}
	if len(svc.simIDs) != 1 || svc.simIDs[0] != simID {
		t.Errorf("expected simID %s, got %v", simID, svc.simIDs)
	}
}

// (c) handler dispatches when coa_status == no_session AND coa_sent_at is older than dedup window
func TestResender_DispatchesWhenNoSessionAndExpiredDedupWindow(t *testing.T) {
	oldTime := time.Now().Add(-2 * dedupResendWindow)
	store := &stubCoAStoreReader{
		coaStatus: CoAStatusNoSession,
		coaSentAt: &oldTime,
	}
	svc := &stubCoAResendDispatcher{}
	r := NewResender(store, svc, zerolog.Nop())

	tenantID := uuid.New().String()
	simID := uuid.New()
	r.handle(context.Background(), "", buildLegacyPayload(tenantID, simID.String()))

	if svc.calls != 1 {
		t.Errorf("expected 1 ResendCoA call after dedup window, got %d", svc.calls)
	}
	if len(svc.simIDs) != 1 || svc.simIDs[0] != simID {
		t.Errorf("expected simID %s, got %v", simID, svc.simIDs)
	}
}

// (d) handler tolerates malformed envelope (returns nil, logs warning — no panic)
func TestResender_ToleratesMalformedEnvelope(t *testing.T) {
	store := &stubCoAStoreReader{}
	svc := &stubCoAResendDispatcher{}
	r := NewResender(store, svc, zerolog.Nop())

	r.handle(context.Background(), "", []byte(`{"not_valid": true}`))

	if svc.calls != 0 {
		t.Errorf("expected 0 ResendCoA calls for malformed envelope, got %d", svc.calls)
	}
}

// (d) handler tolerates completely invalid JSON (no panic)
func TestResender_ToleratesGarbagePayload(t *testing.T) {
	store := &stubCoAStoreReader{}
	svc := &stubCoAResendDispatcher{}
	r := NewResender(store, svc, zerolog.Nop())

	r.handle(context.Background(), "", []byte(`not-json-at-all`))

	if svc.calls != 0 {
		t.Errorf("expected 0 ResendCoA calls for garbage payload, got %d", svc.calls)
	}
}
