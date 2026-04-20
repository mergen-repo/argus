package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockSimStore struct {
	sims map[uuid.UUID]map[uuid.UUID]*store.SIM
}

func (m *mockSimStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*store.SIM, error) {
	tenantSims, ok := m.sims[tenantID]
	if !ok {
		return nil, store.ErrSIMNotFound
	}
	s, ok := tenantSims[id]
	if !ok {
		return nil, store.ErrSIMNotFound
	}
	return s, nil
}

type mockSessionStore struct {
	sessions map[uuid.UUID]*store.RadiusSession
}

func (m *mockSessionStore) GetLastSessionBySIM(ctx context.Context, tenantID, simID uuid.UUID) (*store.RadiusSession, error) {
	if sess, ok := m.sessions[simID]; ok {
		return sess, nil
	}
	return nil, nil
}

type mockAuditor struct {
	calls  int
	lastP  audit.CreateEntryParams
	retErr error
}

func (m *mockAuditor) CreateEntry(ctx context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	m.calls++
	m.lastP = p
	return &audit.Entry{}, m.retErr
}

type noopEnricher struct{}

func (n *noopEnricher) enrichSIMResponse(_ context.Context, _ uuid.UUID, _ *store.SIM, _ *simResponse) {
}

func makeSIM(tenantID uuid.UUID) *store.SIM {
	apnID := uuid.New()
	policyID := uuid.New()
	msisdn := "+905551234567"
	now := time.Now()
	return &store.SIM{
		ID:              uuid.New(),
		TenantID:        tenantID,
		OperatorID:      uuid.New(),
		APNID:           &apnID,
		ICCID:           "8990123456789012340",
		IMSI:            "286010123456780",
		MSISDN:          &msisdn,
		PolicyVersionID: &policyID,
		SimType:         "physical",
		State:           "active",
		Metadata:        json.RawMessage(`{}`),
		ActivatedAt:     &now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func doCompareRequest(t *testing.T, body any, tenantID *uuid.UUID, userID *uuid.UUID,
	ss compareSimStore, sessSt compareSessionStore, auditor audit.Auditor, enricher simEnricher) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/compare", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	ctx := req.Context()
	if tenantID != nil {
		ctx = context.WithValue(ctx, apierr.TenantIDKey, *tenantID)
	}
	if userID != nil {
		ctx = context.WithValue(ctx, apierr.UserIDKey, *userID)
	}
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	doCompare(w, req, ss, sessSt, auditor, enricher, zerolog.Nop())
	return w
}

func TestCompare_HappyPath(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	simA := makeSIM(tenantID)
	simB := makeSIM(tenantID)
	simB.ICCID = "8990999999999999999"
	simB.State = "suspended"

	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{
		tenantID: {simA.ID: simA, simB.ID: simB},
	}}
	sessID := uuid.New()
	sessSt := &mockSessionStore{sessions: map[uuid.UUID]*store.RadiusSession{
		simA.ID: {ID: sessID, SimID: simA.ID, TenantID: tenantID, OperatorID: simA.OperatorID, SessionState: "closed", StartedAt: time.Now(), ProtocolType: "radius"},
	}}
	auditor := &mockAuditor{}

	w := doCompareRequest(t, map[string]string{"sim_id_a": simA.ID.String(), "sim_id_b": simB.ID.String()},
		&tenantID, &userID, ss, sessSt, auditor, &noopEnricher{})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Status string          `json:"status"`
		Data   compareResponse `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
	if len(resp.Data.Diff) == 0 {
		t.Error("diff should not be empty")
	}

	diffMap := make(map[string]fieldDiff)
	for _, d := range resp.Data.Diff {
		diffMap[d.Field] = d
	}

	if d, ok := diffMap["iccid"]; !ok || d.Equal {
		t.Error("iccid diff should exist and not be equal")
	}
	if d, ok := diffMap["state"]; !ok || d.Equal {
		t.Error("state diff should exist and not be equal")
	}
	if d, ok := diffMap["last_session_id"]; !ok {
		t.Error("last_session_id diff should exist")
	} else if d.ValueA == nil {
		t.Error("last_session_id value_a should be set for simA")
	}

	if auditor.calls != 1 {
		t.Errorf("auditor calls = %d, want 1", auditor.calls)
	}
	if auditor.lastP.Action != "sim.compare" {
		t.Errorf("audit action = %q, want sim.compare", auditor.lastP.Action)
	}
	if auditor.lastP.EntityID != simA.ID.String() {
		t.Errorf("audit entity_id = %q, want %s", auditor.lastP.EntityID, simA.ID.String())
	}
	if auditor.lastP.EntityType != "sim" {
		t.Errorf("audit entity_type = %q, want sim", auditor.lastP.EntityType)
	}

	var afterMeta map[string]string
	json.Unmarshal(auditor.lastP.AfterData, &afterMeta)
	if afterMeta["sim_id_b"] != simB.ID.String() {
		t.Errorf("audit after_data sim_id_b = %q, want %s", afterMeta["sim_id_b"], simB.ID.String())
	}
}

func TestCompare_SameID(t *testing.T) {
	tenantID := uuid.New()
	id := uuid.New()
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{}}

	w := doCompareRequest(t, map[string]string{"sim_id_a": id.String(), "sim_id_b": id.String()},
		&tenantID, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %s", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestCompare_CrossTenant(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	simA := makeSIM(tenantA)
	simB := makeSIM(tenantB)

	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{
		tenantA: {simA.ID: simA},
		tenantB: {simB.ID: simB},
	}}

	w := doCompareRequest(t, map[string]string{"sim_id_a": simA.ID.String(), "sim_id_b": simB.ID.String()},
		&tenantA, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cross-tenant must not be 200 or 403)", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeNotFound {
		t.Errorf("error code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestCompare_MissingSIM(t *testing.T) {
	tenantID := uuid.New()
	simA := makeSIM(tenantID)
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{
		tenantID: {simA.ID: simA},
	}}

	w := doCompareRequest(t, map[string]string{"sim_id_a": simA.ID.String(), "sim_id_b": uuid.New().String()},
		&tenantID, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeNotFound {
		t.Errorf("error code = %q, want NOT_FOUND", resp.Error.Code)
	}
}

func TestCompare_MalformedUUID(t *testing.T) {
	tenantID := uuid.New()
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{}}

	w := doCompareRequest(t, map[string]string{"sim_id_a": "not-a-uuid", "sim_id_b": uuid.New().String()},
		&tenantID, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want INVALID_FORMAT", resp.Error.Code)
	}
}

func TestCompare_MissingTenantContext(t *testing.T) {
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{}}
	b, _ := json.Marshal(map[string]string{"sim_id_a": uuid.New().String(), "sim_id_b": uuid.New().String()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/compare", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	doCompare(w, req, ss, nil, nil, &noopEnricher{}, zerolog.Nop())

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeForbidden {
		t.Errorf("error code = %q, want FORBIDDEN", resp.Error.Code)
	}
}

func TestCompare_MissingFields(t *testing.T) {
	tenantID := uuid.New()
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{}}

	w := doCompareRequest(t, map[string]string{}, &tenantID, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCompare_AuditFailureDoesNotBreakResponse(t *testing.T) {
	tenantID := uuid.New()
	simA := makeSIM(tenantID)
	simB := makeSIM(tenantID)
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{
		tenantID: {simA.ID: simA, simB.ID: simB},
	}}
	auditor := &mockAuditor{retErr: errors.New("db down")}

	w := doCompareRequest(t, map[string]string{"sim_id_a": simA.ID.String(), "sim_id_b": simB.ID.String()},
		&tenantID, nil, ss, nil, auditor, &noopEnricher{})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when audit fails; body: %s", w.Code, w.Body.String())
	}
	if auditor.calls != 1 {
		t.Errorf("auditor.calls = %d, want 1", auditor.calls)
	}
}

func TestCompare_EqualFieldsMarkedEqual(t *testing.T) {
	tenantID := uuid.New()
	simA := makeSIM(tenantID)
	simB := makeSIM(tenantID)
	simB.State = simA.State
	simB.ICCID = simA.ICCID
	simB.IMSI = simA.IMSI
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{
		tenantID: {simA.ID: simA, simB.ID: simB},
	}}

	w := doCompareRequest(t, map[string]string{"sim_id_a": simA.ID.String(), "sim_id_b": simB.ID.String()},
		&tenantID, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Data compareResponse `json:"data"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	for _, d := range resp.Data.Diff {
		if d.Field == "iccid" && !d.Equal {
			t.Error("iccid should be equal when both SIMs have same ICCID")
		}
		if d.Field == "state" && !d.Equal {
			t.Error("state should be equal when both SIMs have same state")
		}
	}
}

func TestBuildDiff_FieldList(t *testing.T) {
	now := time.Now()
	tenantID := uuid.New()
	simA := makeSIM(tenantID)
	simB := makeSIM(tenantID)
	rA := toSIMResponseBase(simA)
	rB := toSIMResponseBase(simB)

	sessA := &store.RadiusSession{ID: uuid.New(), SimID: simA.ID, TenantID: tenantID, OperatorID: simA.OperatorID, SessionState: "closed", StartedAt: now, ProtocolType: "radius"}

	diffs := buildDiff(rA, rB, simA, simB, sessA, nil)

	expected := []string{
		"iccid", "imsi", "msisdn", "state", "state_changed_at",
		"operator_id", "operator_name", "apn_id", "apn_name",
		"policy_version_id", "static_ip", "esim_profile_id", "last_session_id",
	}
	diffFields := make(map[string]bool)
	for _, d := range diffs {
		diffFields[d.Field] = true
	}
	for _, f := range expected {
		if !diffFields[f] {
			t.Errorf("expected diff field %q not found in diff output", f)
		}
	}
}

func TestCompare_InvalidJSONBody(t *testing.T) {
	tenantID := uuid.New()
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/compare", bytes.NewReader([]byte("{{not json")))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	doCompare(w, req, ss, nil, nil, &noopEnricher{}, zerolog.Nop())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want INVALID_FORMAT", resp.Error.Code)
	}
}

func TestCompare_MalformedUUIDB(t *testing.T) {
	tenantID := uuid.New()
	ss := &mockSimStore{sims: map[uuid.UUID]map[uuid.UUID]*store.SIM{}}

	w := doCompareRequest(t, map[string]string{"sim_id_a": uuid.New().String(), "sim_id_b": "bad-uuid"},
		&tenantID, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want INVALID_FORMAT", resp.Error.Code)
	}
}

type errorSimStore struct {
	errForA bool
}

func (e *errorSimStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*store.SIM, error) {
	return nil, errors.New("db error")
}

type errorOnBSimStore struct {
	simA *store.SIM
}

func (e *errorOnBSimStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*store.SIM, error) {
	if e.simA != nil && id == e.simA.ID {
		return e.simA, nil
	}
	return nil, errors.New("db error for B")
}

func TestCompare_InternalErrorFetchingSimA(t *testing.T) {
	tenantID := uuid.New()
	ss := &errorSimStore{}

	w := doCompareRequest(t, map[string]string{"sim_id_a": uuid.New().String(), "sim_id_b": uuid.New().String()},
		&tenantID, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInternalError {
		t.Errorf("error code = %q, want INTERNAL_ERROR", resp.Error.Code)
	}
}

func TestCompare_InternalErrorFetchingSimB(t *testing.T) {
	tenantID := uuid.New()
	simA := makeSIM(tenantID)
	ss := &errorOnBSimStore{simA: simA}

	w := doCompareRequest(t, map[string]string{"sim_id_a": simA.ID.String(), "sim_id_b": uuid.New().String()},
		&tenantID, nil, ss, nil, nil, &noopEnricher{})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInternalError {
		t.Errorf("error code = %q, want INTERNAL_ERROR", resp.Error.Code)
	}
}

func TestCompare_WrapperMethod(t *testing.T) {
	tenantID := uuid.New()
	simA := makeSIM(tenantID)
	simB := makeSIM(tenantID)

	h := &Handler{
		simStore:     nil,
		sessionStore: nil,
		auditSvc:     nil,
	}
	_ = h

	b, _ := json.Marshal(map[string]string{"sim_id_a": simA.ID.String(), "sim_id_b": simB.ID.String()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/compare", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Compare(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("Compare wrapper: status = %d, want 403 (no tenant context)", w.Code)
	}
}

func TestStateChangedAt(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)

	t.Run("uses activated_at", func(t *testing.T) {
		s := &store.SIM{ActivatedAt: &now, UpdatedAt: earlier}
		v := stateChangedAt(s)
		if v == nil {
			t.Fatal("expected non-nil")
		}
		if *v != now.Format(time.RFC3339) {
			t.Errorf("got %q, want %q", *v, now.Format(time.RFC3339))
		}
	})

	t.Run("falls back to updated_at when no state timestamps", func(t *testing.T) {
		s := &store.SIM{UpdatedAt: earlier}
		v := stateChangedAt(s)
		if v == nil {
			t.Fatal("expected non-nil")
		}
		if *v != earlier.Format(time.RFC3339) {
			t.Errorf("got %q, want %q", *v, earlier.Format(time.RFC3339))
		}
	})

	t.Run("picks most recent of activated/suspended", func(t *testing.T) {
		s := &store.SIM{ActivatedAt: &earlier, SuspendedAt: &now, UpdatedAt: earlier}
		v := stateChangedAt(s)
		if *v != now.Format(time.RFC3339) {
			t.Errorf("got %q, want %q (suspended_at should win)", *v, now.Format(time.RFC3339))
		}
	})
}
