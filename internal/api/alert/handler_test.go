package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// ---- Pure httptest (no DB) --------------------------------------------------

func TestList_RejectsInvalidSeverity(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?severity=warning", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeInvalidSeverity {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidSeverity)
	}
	if !strings.Contains(resp.Error.Message, "warning") {
		t.Errorf("message should cite offending value, got %q", resp.Error.Message)
	}
}

func TestList_RejectsInvalidState(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?state=zzz", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestList_RejectsInvalidSource(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?source=zzz", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestUpdateState_SuppressedNotAllowed_Returns409(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())
	tenantID := uuid.New()
	alertID := uuid.New()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", alertID.String())
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	body := `{"state":"suppressed"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alertID.String(), strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.UpdateState(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidStateTransition {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidStateTransition)
	}
}

func TestUpdateState_UnknownState_Returns409(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop())
	tenantID := uuid.New()
	alertID := uuid.New()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", alertID.String())
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	body := `{"state":"unknown"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+alertID.String(), strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.UpdateState(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

// ---- DB-gated tests ---------------------------------------------------------

func testAlertHandlerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("skip: cannot connect to postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Logf("skip: postgres ping failed: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func seedTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, contact_email) VALUES ($1, $2, $3)`,
		id, "alert-handler-tenant-"+id.String()[:8], "alert-handler-"+id.String()[:8]+"@test.invalid",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM alerts WHERE tenant_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func seedUser(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash, name, role) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, tenantID, "alert-user-"+id.String()[:8]+"@test.invalid", "$2a$10$placeholder", "Alert Handler Tester", "tenant_admin",
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE alerts SET acknowledged_by = NULL WHERE acknowledged_by = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

// captureAuditor records the last CreateEntry call.
type captureAuditor struct {
	called bool
	last   audit.CreateEntryParams
}

func (c *captureAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	c.called = true
	c.last = p
	return &audit.Entry{ID: 1, TenantID: p.TenantID, Action: p.Action, EntityType: p.EntityType, EntityID: p.EntityID, CreatedAt: time.Now()}, nil
}

func newTestHandler(pool *pgxpool.Pool, auditor audit.Auditor) *Handler {
	return NewHandler(store.NewAlertStore(pool), auditor, zerolog.Nop())
}

func alertCtx(tenantID uuid.UUID) context.Context {
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	return ctx
}

func withUser(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, apierr.UserIDKey, userID)
}

func withRouteID(ctx context.Context, id string) context.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

func TestList_CombinedFilters_ReturnsExpectedRows(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	// Seed 4 alerts with varying (source, severity). Only one matches source=operator + severity=critical.
	seeds := []store.CreateAlertParams{
		{TenantID: tenantID, Type: "operator.down", Severity: "critical", Source: "operator", Title: "match", Description: "d1"},
		{TenantID: tenantID, Type: "sim.data_spike", Severity: "critical", Source: "sim", Title: "no", Description: "d2"},
		{TenantID: tenantID, Type: "operator.degraded", Severity: "high", Source: "operator", Title: "no", Description: "d3"},
		{TenantID: tenantID, Type: "infra.disk", Severity: "critical", Source: "infra", Title: "no", Description: "d4"},
	}
	for _, p := range seeds {
		if _, err := s.Create(ctx, p); err != nil {
			t.Fatalf("seed alert: %v", err)
		}
	}

	h := newTestHandler(pool, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?source=operator&severity=critical", nil).WithContext(alertCtx(tenantID))
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string     `json:"status"`
		Data   []alertDTO `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("got %d rows, want 1", len(resp.Data))
	}
	if resp.Data[0].Source != "operator" || resp.Data[0].Severity != "critical" {
		t.Errorf("got %+v", resp.Data[0])
	}
}

func TestList_CursorPagination_DisjointPages(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	// Seed 5 alerts with monotonically decreasing fired_at so cursor ordering is deterministic.
	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		p := store.CreateAlertParams{
			TenantID:    tenantID,
			Type:        "sim.data_spike",
			Severity:    "high",
			Source:      "sim",
			Title:       "seed",
			Description: "d",
			FiredAt:     base.Add(time.Duration(i) * time.Minute),
		}
		if _, err := s.Create(ctx, p); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	h := newTestHandler(pool, nil)

	seen := make(map[string]bool)
	var cursor string
	pageCount := 0
	for {
		url := "/api/v1/alerts?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, url, nil).WithContext(alertCtx(tenantID))
		w := httptest.NewRecorder()
		h.List(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Data []alertDTO      `json:"data"`
			Meta apierr.ListMeta `json:"meta"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		pageCount++
		for _, row := range resp.Data {
			if seen[row.ID] {
				t.Fatalf("duplicate id across pages: %s", row.ID)
			}
			seen[row.ID] = true
		}
		if !resp.Meta.HasMore {
			break
		}
		cursor = resp.Meta.Cursor
		if cursor == "" {
			t.Fatal("has_more=true but cursor empty")
		}
		if pageCount > 10 {
			t.Fatal("too many pages — pagination not converging")
		}
	}
	if len(seen) != 5 {
		t.Errorf("saw %d distinct alerts across pages, want 5", len(seen))
	}
	if pageCount != 3 {
		t.Errorf("got %d pages, want 3 (2+2+1)", pageCount)
	}
}

func TestGet_NotFound_ReturnsAlertNotFound(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	h := newTestHandler(pool, nil)

	missingID := uuid.New()
	ctx := withRouteID(alertCtx(tenantID), missingID.String())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+missingID.String(), nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeAlertNotFound {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeAlertNotFound)
	}
}

func TestGet_CrossTenant_Returns404NotFound(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantA := seedTenant(t, pool)
	tenantB := seedTenant(t, pool)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantA, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "owned by A", Description: "d",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := newTestHandler(pool, nil)
	rctx := withRouteID(alertCtx(tenantB), a.ID.String())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+a.ID.String(), nil).WithContext(rctx)
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status = %d, want 404 (no leak)", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeAlertNotFound {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeAlertNotFound)
	}
}

func TestUpdateState_Open_To_Ack_SetsAckedAtAndBy(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	userID := seedUser(t, pool, tenantID)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "ack-me", Description: "d",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	auditor := &captureAuditor{}
	h := newTestHandler(pool, auditor)
	reqCtx := withUser(withRouteID(alertCtx(tenantID), a.ID.String()), userID)

	body := `{"state":"acknowledged","note":"checking"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+a.ID.String(), strings.NewReader(body)).WithContext(reqCtx)
	w := httptest.NewRecorder()
	h.UpdateState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data alertDTO `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.State != "acknowledged" {
		t.Errorf("state = %q, want acknowledged", resp.Data.State)
	}
	if resp.Data.AcknowledgedAt == nil || *resp.Data.AcknowledgedAt == "" {
		t.Errorf("acknowledged_at should be set; got %+v", resp.Data.AcknowledgedAt)
	}
	if resp.Data.AcknowledgedBy == nil || *resp.Data.AcknowledgedBy != userID.String() {
		t.Errorf("acknowledged_by = %v, want %q", resp.Data.AcknowledgedBy, userID.String())
	}
}

func TestUpdateState_Resolved_To_Open_Returns409InvalidStateTransition(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	userID := seedUser(t, pool, tenantID)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "to-be-resolved", Description: "d",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// open -> resolved (direct transition is allowed)
	if _, err := s.UpdateState(ctx, tenantID, a.ID, "resolved", nil); err != nil {
		t.Fatalf("seed transition to resolved: %v", err)
	}

	h := newTestHandler(pool, nil)
	reqCtx := withUser(withRouteID(alertCtx(tenantID), a.ID.String()), userID)
	// Handler pre-rejects "open" with 409 (not in allowed set).
	body := `{"state":"open"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+a.ID.String(), strings.NewReader(body)).WithContext(reqCtx)
	w := httptest.NewRecorder()
	h.UpdateState(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidStateTransition {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidStateTransition)
	}
}

func TestUpdateState_Resolved_To_Ack_Returns409_FromStore(t *testing.T) {
	// Regression: store-level rejection (already-resolved -> acknowledged) must
	// surface as 409 CodeInvalidStateTransition (not 500). Covers the errors.Is
	// branch on ErrInvalidAlertTransition.
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	userID := seedUser(t, pool, tenantID)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "resolved-then-ack", Description: "d",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.UpdateState(ctx, tenantID, a.ID, "resolved", nil); err != nil {
		t.Fatalf("seed resolve: %v", err)
	}

	h := newTestHandler(pool, nil)
	reqCtx := withUser(withRouteID(alertCtx(tenantID), a.ID.String()), userID)
	body := `{"state":"acknowledged"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+a.ID.String(), strings.NewReader(body)).WithContext(reqCtx)
	w := httptest.NewRecorder()
	h.UpdateState(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s, want 409", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidStateTransition {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidStateTransition)
	}
}

func TestUpdateState_EmitsAudit(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	userID := seedUser(t, pool, tenantID)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "ack-emits-audit", Description: "d",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	auditor := &captureAuditor{}
	h := newTestHandler(pool, auditor)
	reqCtx := withUser(withRouteID(alertCtx(tenantID), a.ID.String()), userID)
	body := `{"state":"acknowledged","note":"ok"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+a.ID.String(), strings.NewReader(body)).WithContext(reqCtx)
	w := httptest.NewRecorder()
	h.UpdateState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if !auditor.called {
		t.Fatal("audit.Emit not invoked")
	}
	if auditor.last.Action != "alert.update" {
		t.Errorf("action = %q, want alert.update", auditor.last.Action)
	}
	if auditor.last.EntityType != "alert" || auditor.last.EntityID != a.ID.String() {
		t.Errorf("entity = %q/%q, want alert/%s", auditor.last.EntityType, auditor.last.EntityID, a.ID.String())
	}
	if !strings.Contains(string(auditor.last.AfterData), `"state":"acknowledged"`) {
		t.Errorf("after_data missing state: %s", string(auditor.last.AfterData))
	}
	if !strings.Contains(string(auditor.last.AfterData), `"note":"ok"`) {
		t.Errorf("after_data missing note: %s", string(auditor.last.AfterData))
	}
}
