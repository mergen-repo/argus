package alert

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// ---- Pure httptest (no DB) --------------------------------------------------

func TestList_RejectsInvalidSeverity(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
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
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
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
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
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
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
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
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
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
	return newTestHandlerWithCooldown(pool, auditor, 5)
}

func newTestHandlerWithCooldown(pool *pgxpool.Pool, auditor audit.Auditor, cooldownMinutes int) *Handler {
	return NewHandler(store.NewAlertStore(pool), auditor, zerolog.Nop(), cooldownMinutes)
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
	if _, err := s.UpdateState(ctx, tenantID, a.ID, "resolved", nil, 0); err != nil {
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
	if _, err := s.UpdateState(ctx, tenantID, a.ID, "resolved", nil, 0); err != nil {
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

// TestUpdateState_ResolveHandler_StampsCooldownFromConfig — FIX-210 Gate F-A1 regression.
// The handler-level resolve path MUST pass the configured cooldown minutes through
// to the store. A hardcoded 0 here would silently disable AC-5 in the primary
// (REST) resolve path even though store-level tests pass with explicit cooldown.
// ---- ListSimilar tests -------------------------------------------------------

func TestListSimilar_AnchorNotFound(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	h := newTestHandler(pool, nil)

	missingID := uuid.New()
	ctx := withRouteID(alertCtx(tenantID), missingID.String())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+missingID.String()+"/similar", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListSimilar(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeAlertNotFound {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeAlertNotFound)
	}
}

func TestListSimilar_LimitValidation(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	tenantID := uuid.New()
	alertID := uuid.New()

	for _, badLimit := range []string{"0", "51", "abc", "-1"} {
		ctx := withRouteID(alertCtx(tenantID), alertID.String())
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+alertID.String()+"/similar?limit="+badLimit, nil).WithContext(ctx)
		w := httptest.NewRecorder()
		h.ListSimilar(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("limit=%q: status = %d, want 400", badLimit, w.Code)
		}
		var resp apierr.ErrorResponse
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp.Error.Code != apierr.CodeInvalidFormat {
			t.Errorf("limit=%q: code = %q, want %q", badLimit, resp.Error.Code, apierr.CodeInvalidFormat)
		}
	}
}

func TestListSimilar_DedupKeyMatch(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	dk := "sim.data_spike:sim-abc"
	anchor, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "anchor", Description: "d", DedupKey: &dk,
	})
	if err != nil {
		t.Fatalf("seed anchor: %v", err)
	}
	similar, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "medium", Source: "sim",
		Title: "similar-1", Description: "d", DedupKey: &dk,
	})
	if err != nil {
		t.Fatalf("seed similar: %v", err)
	}
	_ = similar

	h := newTestHandler(pool, nil)
	reqCtx := withRouteID(alertCtx(tenantID), anchor.ID.String())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+anchor.ID.String()+"/similar?limit=10", nil).WithContext(reqCtx)
	w := httptest.NewRecorder()
	h.ListSimilar(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string     `json:"status"`
		Data   []alertDTO `json:"data"`
		Meta   struct {
			AnchorID      string `json:"anchor_id"`
			MatchStrategy string `json:"match_strategy"`
			Count         int    `json:"count"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
	if resp.Meta.MatchStrategy != "dedup_key" {
		t.Errorf("match_strategy = %q, want dedup_key", resp.Meta.MatchStrategy)
	}
	if resp.Meta.AnchorID != anchor.ID.String() {
		t.Errorf("anchor_id = %q, want %q", resp.Meta.AnchorID, anchor.ID.String())
	}
	if resp.Meta.Count != len(resp.Data) {
		t.Errorf("count = %d, does not match len(data) = %d", resp.Meta.Count, len(resp.Data))
	}
	if len(resp.Data) < 1 {
		t.Fatalf("expected at least 1 similar alert, got %d", len(resp.Data))
	}
	for _, d := range resp.Data {
		if d.ID == anchor.ID.String() {
			t.Error("anchor should not appear in similar results")
		}
	}
}

func TestListSimilar_TypeSourceFallback(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	anchor, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "infra.disk", Severity: "high", Source: "infra",
		Title: "anchor-no-dedup", Description: "d",
	})
	if err != nil {
		t.Fatalf("seed anchor: %v", err)
	}
	_, err = s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "infra.disk", Severity: "critical", Source: "infra",
		Title: "type-source-match", Description: "d",
	})
	if err != nil {
		t.Fatalf("seed similar: %v", err)
	}

	h := newTestHandler(pool, nil)
	reqCtx := withRouteID(alertCtx(tenantID), anchor.ID.String())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+anchor.ID.String()+"/similar", nil).WithContext(reqCtx)
	w := httptest.NewRecorder()
	h.ListSimilar(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string     `json:"status"`
		Data   []alertDTO `json:"data"`
		Meta   struct {
			MatchStrategy string `json:"match_strategy"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Meta.MatchStrategy != "type_source" {
		t.Errorf("match_strategy = %q, want type_source", resp.Meta.MatchStrategy)
	}
	if len(resp.Data) < 1 {
		t.Fatalf("expected at least 1 similar alert, got %d", len(resp.Data))
	}
}

func TestUpdateState_ResolveHandler_StampsCooldownFromConfig(t *testing.T) {
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
		Title: "resolve-via-handler", Description: "d",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	const cooldownMin = 7
	h := newTestHandlerWithCooldown(pool, nil, cooldownMin)
	reqCtx := withUser(withRouteID(alertCtx(tenantID), a.ID.String()), userID)
	body := `{"state":"resolved"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/alerts/"+a.ID.String(), strings.NewReader(body)).WithContext(reqCtx)
	w := httptest.NewRecorder()
	h.UpdateState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}

	got, err := s.GetByID(ctx, tenantID, a.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.State != "resolved" {
		t.Fatalf("state = %q, want resolved", got.State)
	}
	if got.CooldownUntil == nil {
		t.Fatal("cooldown_until is nil; handler did not thread config (F-A1 regression)")
	}
	wantMin := time.Now().UTC().Add(time.Duration(cooldownMin-1) * time.Minute)
	wantMax := time.Now().UTC().Add(time.Duration(cooldownMin+1) * time.Minute)
	if got.CooldownUntil.Before(wantMin) || got.CooldownUntil.After(wantMax) {
		t.Errorf("cooldown_until = %v, want within ~%dm of now", got.CooldownUntil, cooldownMin)
	}
}

// ---- Export tests -----------------------------------------------------------

var csvFilenameRe = regexp.MustCompile(`filename="alerts-\d{8}-\d{6}\.csv"`)
var jsonFilenameRe = regexp.MustCompile(`filename="alerts-\d{8}-\d{6}\.json"`)

// TestExportCSV_RejectsInvalidFilters — pure httptest; store nil because validation runs first.
func TestExportCSV_RejectsInvalidFilters(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.csv?severity=bogus", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ExportCSV(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidSeverity {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidSeverity)
	}
}

// TestExportJSON_RejectsInvalidFilters — same validation parity for JSON export.
func TestExportJSON_RejectsInvalidFilters(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.json?source=unknown", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ExportJSON(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

// TestExportCSV_StreamsRows — DB-gated: seeds 3 rows, asserts CSV header + 3 data rows + Content-Type/Disposition.
func TestExportCSV_StreamsRows(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated export test")
	}
	tenantID := seedTenant(t, pool)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	seeds := []store.CreateAlertParams{
		{TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim", Title: "r1", Description: "d1"},
		{TenantID: tenantID, Type: "operator.down", Severity: "critical", Source: "operator", Title: "r2", Description: "d2"},
		{TenantID: tenantID, Type: "infra.disk", Severity: "medium", Source: "infra", Title: "r3", Description: "d3"},
	}
	for _, p := range seeds {
		if _, err := s.Create(ctx, p); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	auditor := &captureAuditor{}
	h := newTestHandler(pool, auditor)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.csv", nil).WithContext(alertCtx(tenantID))
	w := httptest.NewRecorder()
	h.ExportCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}

	cd := w.Header().Get("Content-Disposition")
	if !csvFilenameRe.MatchString(cd) {
		t.Errorf("Content-Disposition = %q, want filename matching alerts-YYYYMMDD-HHMMSS.csv", cd)
	}

	// Count lines: 1 header + 3 data rows.
	body := w.Body.String()
	sc := bufio.NewScanner(strings.NewReader(body))
	var lines []string
	for sc.Scan() {
		if sc.Text() != "" {
			lines = append(lines, sc.Text())
		}
	}
	if len(lines) < 4 {
		t.Fatalf("expected >=4 CSV lines (header+3), got %d:\n%s", len(lines), body)
	}
	if !strings.HasPrefix(lines[0], "id,") {
		t.Errorf("first line should be CSV header, got: %q", lines[0])
	}

	if !auditor.called {
		t.Fatal("audit.Emit not invoked")
	}
	if auditor.last.Action != "alert.exported" {
		t.Errorf("audit action = %q, want alert.exported", auditor.last.Action)
	}
	if !strings.Contains(string(auditor.last.AfterData), `"format":"csv"`) {
		t.Errorf("audit after_data missing format:csv: %s", auditor.last.AfterData)
	}
}

// TestExportCSV_RespectsCap — DB-gated: verifies the handler issues paginated calls
// to the store (which caps at 100/call) and collects up to alertExportRowCap rows.
// Seeds 3 rows only; cap enforcement is validated by checking collected rows <= cap.
func TestExportCSV_RespectsCap(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated export test")
	}
	tenantID := seedTenant(t, pool)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := s.Create(ctx, store.CreateAlertParams{
			TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
			Title: "cap-test", Description: "d",
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	h := newTestHandler(pool, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.csv", nil).WithContext(alertCtx(tenantID))
	w := httptest.NewRecorder()
	h.ExportCSV(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	sc := bufio.NewScanner(strings.NewReader(w.Body.String()))
	dataRows := 0
	header := true
	for sc.Scan() {
		if sc.Text() == "" {
			continue
		}
		if header {
			header = false
			continue
		}
		dataRows++
	}
	if dataRows > alertExportRowCap {
		t.Errorf("got %d rows, exceeds cap %d", dataRows, alertExportRowCap)
	}
}

// TestExportJSON_RawArray — DB-gated: response is a JSON array, NOT enveloped.
func TestExportJSON_RawArray(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated export test")
	}
	tenantID := seedTenant(t, pool)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	if _, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "json-export", Description: "d",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := newTestHandler(pool, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.json", nil).WithContext(alertCtx(tenantID))
	w := httptest.NewRecorder()
	h.ExportJSON(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	body := strings.TrimSpace(w.Body.String())
	if !strings.HasPrefix(body, "[") || !strings.HasSuffix(body, "]") {
		preview := body
		if len(preview) > 100 {
			preview = preview[:100]
		}
		t.Errorf("body should be a JSON array, got: %q", preview)
	}
	if strings.Contains(body, `"status":"success"`) {
		t.Error("body must NOT contain standard envelope wrapper")
	}
}

// TestExportJSON_FilenamePattern — pure httptest checking filename pattern when no rows
// (tenant with no matching state). Uses DB to return empty array.
func TestExportJSON_FilenamePattern(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated export test")
	}
	tenantID := seedTenant(t, pool)

	h := newTestHandler(pool, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.json?state=resolved", nil).WithContext(alertCtx(tenantID))
	w := httptest.NewRecorder()
	h.ExportJSON(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	cd := w.Header().Get("Content-Disposition")
	if !jsonFilenameRe.MatchString(cd) {
		t.Errorf("Content-Disposition = %q, want filename matching alerts-YYYYMMDD-HHMMSS.json", cd)
	}
}

// ---- ExportPDF (FIX-229 Task 7 / DEV-338) ----------------------------------

var pdfFilenameRe = regexp.MustCompile(`filename="alerts-\d{8}-\d{6}\.pdf"`)

// stubReportEngine implements the alert handler's reportEngine interface.
// It returns a static PDF artifact so the handler path can be exercised
// without wiring the full report.StoreProvider.
type stubReportEngine struct {
	called  bool
	lastReq report.Request
	bytes   []byte
	err     error
}

func (s *stubReportEngine) Build(_ context.Context, req report.Request) (*report.Artifact, error) {
	s.called = true
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	body := s.bytes
	if len(body) == 0 {
		// Minimal valid PDF magic + trailer pad — enough for the handler's
		// Content-Length header and the bytes!=0 check.
		body = []byte("%PDF-1.4\n%stub\n")
		body = append(body, bytes.Repeat([]byte{' '}, 2048)...)
	}
	return &report.Artifact{
		Bytes:    body,
		MIME:     "application/pdf",
		Filename: "alerts-stub.pdf",
	}, nil
}

func TestExportPDF_NoEngine_Returns503(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.pdf", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ExportPDF(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeServiceUnavailable {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeServiceUnavailable)
	}
}

func TestExportPDF_NoTenant_Returns401(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	h.WithReportEngine(&stubReportEngine{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.pdf", nil)
	w := httptest.NewRecorder()
	h.ExportPDF(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestExportPDF_RejectsInvalidFilters(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	h.WithReportEngine(&stubReportEngine{})
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.pdf?severity=bogus", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ExportPDF(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidSeverity {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidSeverity)
	}
}

func TestExportPDF_NoData_Returns404(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated test")
	}
	tenantID := seedTenant(t, pool)

	h := newTestHandler(pool, nil)
	stub := &stubReportEngine{}
	h.WithReportEngine(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.pdf", nil).WithContext(alertCtx(tenantID))
	w := httptest.NewRecorder()
	h.ExportPDF(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeAlertNoData {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeAlertNoData)
	}
	if stub.called {
		t.Error("engine should NOT be called when probe returns zero rows")
	}
}

func TestExportPDF_BuildsArtifact(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated test")
	}
	tenantID := seedTenant(t, pool)

	s := store.NewAlertStore(pool)
	if _, err := s.Create(context.Background(), store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "pdf-export", Description: "d",
	}); err != nil {
		t.Fatalf("seed alert: %v", err)
	}

	auditor := &captureAuditor{}
	h := newTestHandler(pool, auditor)
	stub := &stubReportEngine{}
	h.WithReportEngine(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/export.pdf", nil).WithContext(alertCtx(tenantID))
	w := httptest.NewRecorder()
	h.ExportPDF(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !stub.called {
		t.Fatal("engine.Build was not invoked")
	}
	if stub.lastReq.Type != report.ReportAlertsExport {
		t.Errorf("engine req type = %q, want %q", stub.lastReq.Type, report.ReportAlertsExport)
	}
	if stub.lastReq.Format != report.FormatPDF {
		t.Errorf("engine req format = %q, want %q", stub.lastReq.Format, report.FormatPDF)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	cd := w.Header().Get("Content-Disposition")
	if !pdfFilenameRe.MatchString(cd) {
		t.Errorf("Content-Disposition = %q, want filename matching alerts-YYYYMMDD-HHMMSS.pdf", cd)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", w.Header().Get("Cache-Control"))
	}
	if w.Body.Len() == 0 {
		t.Error("body is empty")
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) {
		t.Error("body should start with PDF magic")
	}
	if !auditor.called {
		t.Error("audit.Emit not invoked")
	}
	if auditor.last.Action != "alert.exported" {
		t.Errorf("audit action = %q, want alert.exported", auditor.last.Action)
	}
	if !strings.Contains(string(auditor.last.AfterData), `"format":"pdf"`) {
		t.Errorf("audit after_data missing format:pdf: %s", auditor.last.AfterData)
	}
}

// ---- FIX-229 Task 8 — Suppression CRUD tests --------------------------------

// stubSuppressionStore captures Create / Delete arguments and lets each test
// dictate the rows returned by List. Used for pure (no-DB) tests where we want
// to assert handler-side validation, normalisation, and audit shape without
// reaching Postgres.
type stubSuppressionStore struct {
	createCalls   []store.CreateAlertSuppressionParams
	createReturn  *store.AlertSuppression
	createErr     error
	deletedID     uuid.UUID
	deleteErr     error
	listReturn    []store.AlertSuppression
	listCursor    *uuid.UUID
	listErr       error
	lastListParam store.ListAlertSuppressionsParams
}

func (s *stubSuppressionStore) Create(_ context.Context, p store.CreateAlertSuppressionParams) (*store.AlertSuppression, error) {
	s.createCalls = append(s.createCalls, p)
	if s.createErr != nil {
		return nil, s.createErr
	}
	if s.createReturn != nil {
		return s.createReturn, nil
	}
	return &store.AlertSuppression{
		ID:         uuid.New(),
		TenantID:   p.TenantID,
		ScopeType:  p.ScopeType,
		ScopeValue: p.ScopeValue,
		ExpiresAt:  p.ExpiresAt,
		Reason:     p.Reason,
		RuleName:   p.RuleName,
		CreatedBy:  p.CreatedBy,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func (s *stubSuppressionStore) List(_ context.Context, _ uuid.UUID, p store.ListAlertSuppressionsParams) ([]store.AlertSuppression, *uuid.UUID, error) {
	s.lastListParam = p
	if s.listErr != nil {
		return nil, nil, s.listErr
	}
	return s.listReturn, s.listCursor, nil
}

func (s *stubSuppressionStore) Delete(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	s.deletedID = id
	return s.deleteErr
}

// stubBackfillStore captures backfill / restore calls.
type stubBackfillStore struct {
	backfillTenantID, backfillSuppressionID uuid.UUID
	backfillScopeType, backfillScopeValue   string
	backfillReason                          string
	backfillCount                           int64
	backfillErr                             error
	restoreCount                            int64
	restoreErr                              error
}

func (s *stubBackfillStore) BackfillSuppression(_ context.Context, tenantID uuid.UUID, scopeType, scopeValue string, suppressionID uuid.UUID, reason string) (int64, error) {
	s.backfillTenantID = tenantID
	s.backfillSuppressionID = suppressionID
	s.backfillScopeType = scopeType
	s.backfillScopeValue = scopeValue
	s.backfillReason = reason
	return s.backfillCount, s.backfillErr
}

func (s *stubBackfillStore) RestoreSuppressedByMetaID(_ context.Context, _, _ uuid.UUID) (int64, error) {
	return s.restoreCount, s.restoreErr
}

func newSuppressionTestHandler(stub *stubSuppressionStore, backfill *stubBackfillStore, auditor audit.Auditor) *Handler {
	h := NewHandler(nil, auditor, zerolog.Nop(), 5)
	h.suppressionStore = stub
	if backfill != nil {
		h.withAlertBackfillStore(backfill)
	}
	return h
}

func TestCreateSuppression_NoStoreReturns503(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"scope_type":"type","scope_value":"sim.data_spike","duration":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeServiceUnavailable {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeServiceUnavailable)
	}
}

func TestCreateSuppression_RejectsBothDurationAndExpires(t *testing.T) {
	stub := &stubSuppressionStore{}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	exp := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"scope_type":"type","scope_value":"sim.data_spike","duration":"1h","expires_at":"` + exp + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
	}
	if len(stub.createCalls) != 0 {
		t.Errorf("Create should not be called on validation error, got %d calls", len(stub.createCalls))
	}
}

func TestCreateSuppression_RejectsExpiresOver30d(t *testing.T) {
	stub := &stubSuppressionStore{}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	tooFar := time.Now().Add(31 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"scope_type":"type","scope_value":"sim.data_spike","expires_at":"` + tooFar + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestCreateSuppression_RejectsInvalidScopeType(t *testing.T) {
	stub := &stubSuppressionStore{}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"scope_type":"unknown","scope_value":"x","duration":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestCreateSuppression_NormalizesUUIDCase(t *testing.T) {
	stub := &stubSuppressionStore{}
	backfill := &stubBackfillStore{}
	h := newSuppressionTestHandler(stub, backfill, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	mixedCase := "DEADBEEF-DEAD-BEEF-DEAD-BEEFDEADBEEF"
	expectedCanonical := "deadbeef-dead-beef-dead-beefdeadbeef"
	body := `{"scope_type":"operator","scope_value":"` + mixedCase + `","duration":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s, want 201", w.Code, w.Body.String())
	}
	if len(stub.createCalls) != 1 {
		t.Fatalf("Create called %d times, want 1", len(stub.createCalls))
	}
	if stub.createCalls[0].ScopeValue != expectedCanonical {
		t.Errorf("stored scope_value = %q, want canonical lowercase %q",
			stub.createCalls[0].ScopeValue, expectedCanonical)
	}
}

func TestCreateSuppression_RejectsInvalidUUIDForOperator(t *testing.T) {
	stub := &stubSuppressionStore{}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"scope_type":"operator","scope_value":"not-a-uuid","duration":"1h"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestCreateSuppression_DurationResolvesExpiresAt(t *testing.T) {
	stub := &stubSuppressionStore{}
	backfill := &stubBackfillStore{}
	h := newSuppressionTestHandler(stub, backfill, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	before := time.Now().UTC()
	body := `{"scope_type":"type","scope_value":"sim.data_spike","duration":"24h"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if len(stub.createCalls) != 1 {
		t.Fatalf("Create called %d times, want 1", len(stub.createCalls))
	}
	got := stub.createCalls[0].ExpiresAt
	min := before.Add(24 * time.Hour).Add(-2 * time.Second)
	max := before.Add(24 * time.Hour).Add(2 * time.Second)
	if got.Before(min) || got.After(max) {
		t.Errorf("ExpiresAt = %v, want roughly %v (+/-2s)", got, before.Add(24*time.Hour))
	}
}

func TestCreateSuppression_BackfillsAndAudits(t *testing.T) {
	stub := &stubSuppressionStore{}
	backfill := &stubBackfillStore{backfillCount: 7}
	auditor := &captureAuditor{}
	h := newSuppressionTestHandler(stub, backfill, auditor)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"scope_type":"type","scope_value":"sim.data_spike","duration":"1h","reason":"investigating"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if backfill.backfillScopeType != "type" || backfill.backfillScopeValue != "sim.data_spike" {
		t.Errorf("backfill called with scope=%q/%q, want type/sim.data_spike",
			backfill.backfillScopeType, backfill.backfillScopeValue)
	}
	if backfill.backfillReason != "investigating" {
		t.Errorf("backfill reason = %q, want investigating", backfill.backfillReason)
	}

	var resp struct {
		Data struct {
			ID           string `json:"id"`
			AppliedCount int64  `json:"applied_count"`
		} `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Data.AppliedCount != 7 {
		t.Errorf("applied_count = %d, want 7", resp.Data.AppliedCount)
	}

	if !auditor.called {
		t.Fatal("audit.Emit not invoked")
	}
	if auditor.last.Action != "alert.suppression.created" {
		t.Errorf("action = %q, want alert.suppression.created", auditor.last.Action)
	}
	if auditor.last.EntityType != "alert_suppression" {
		t.Errorf("entity_type = %q, want alert_suppression", auditor.last.EntityType)
	}
	if !strings.Contains(string(auditor.last.AfterData), `"applied_count":7`) {
		t.Errorf("audit after_data missing applied_count: %s", string(auditor.last.AfterData))
	}
}

func TestCreateSuppression_DuplicateRuleNameReturns409(t *testing.T) {
	stub := &stubSuppressionStore{createErr: store.ErrDuplicateRuleName}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	body := `{"scope_type":"type","scope_value":"sim.data_spike","duration":"1h","rule_name":"dup"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeDuplicate {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeDuplicate)
	}
}

func TestListSuppressions_NoStoreReturns503(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/suppressions", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListSuppressions(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestListSuppressions_DefaultActiveOnlyTrue(t *testing.T) {
	stub := &stubSuppressionStore{}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/suppressions", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListSuppressions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !stub.lastListParam.ActiveOnly {
		t.Errorf("ActiveOnly = false, want true (default)")
	}
}

func TestListSuppressions_ActiveOnlyFalse(t *testing.T) {
	stub := &stubSuppressionStore{}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/suppressions?active_only=false", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListSuppressions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if stub.lastListParam.ActiveOnly {
		t.Errorf("ActiveOnly = true, want false")
	}
}

func TestDeleteSuppression_NoStoreReturns503(t *testing.T) {
	h := NewHandler(nil, nil, zerolog.Nop(), 5)
	tenantID := uuid.New()
	id := uuid.New()
	ctx := withRouteID(context.WithValue(context.Background(), apierr.TenantIDKey, tenantID), id.String())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alerts/suppressions/"+id.String(), nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.DeleteSuppression(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestDeleteSuppression_BadIDReturns400(t *testing.T) {
	stub := &stubSuppressionStore{}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	ctx := withRouteID(context.WithValue(context.Background(), apierr.TenantIDKey, tenantID), "not-a-uuid")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alerts/suppressions/not-a-uuid", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.DeleteSuppression(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestDeleteSuppression_NotFound_Returns404(t *testing.T) {
	stub := &stubSuppressionStore{deleteErr: store.ErrAlertSuppressionNotFound}
	h := newSuppressionTestHandler(stub, nil, nil)
	tenantID := uuid.New()
	id := uuid.New()
	ctx := withRouteID(context.WithValue(context.Background(), apierr.TenantIDKey, tenantID), id.String())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alerts/suppressions/"+id.String(), nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.DeleteSuppression(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeSuppressionNotFound {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeSuppressionNotFound)
	}
}

func TestDeleteSuppression_RestoresAndAudits(t *testing.T) {
	stub := &stubSuppressionStore{}
	backfill := &stubBackfillStore{restoreCount: 3}
	auditor := &captureAuditor{}
	h := newSuppressionTestHandler(stub, backfill, auditor)
	tenantID := uuid.New()
	id := uuid.New()
	ctx := withRouteID(context.WithValue(context.Background(), apierr.TenantIDKey, tenantID), id.String())
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/alerts/suppressions/"+id.String(), nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.DeleteSuppression(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			DeletedID     string `json:"deleted_id"`
			RestoredCount int64  `json:"restored_count"`
		} `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Data.DeletedID != id.String() {
		t.Errorf("deleted_id = %q, want %q", resp.Data.DeletedID, id.String())
	}
	if resp.Data.RestoredCount != 3 {
		t.Errorf("restored_count = %d, want 3", resp.Data.RestoredCount)
	}
	if !auditor.called || auditor.last.Action != "alert.suppression.deleted" {
		t.Errorf("audit not emitted with action alert.suppression.deleted: %+v", auditor.last)
	}
	if auditor.last.EntityID != id.String() {
		t.Errorf("audit entity_id = %q, want %q (suppression id, not alert id) — PAT-016",
			auditor.last.EntityID, id.String())
	}
}

// ---- DB-gated suppression handler tests -------------------------------------

func seedSuppressionTestHandler(t *testing.T, pool *pgxpool.Pool, auditor audit.Auditor) *Handler {
	t.Helper()
	alertStore := store.NewAlertStore(pool)
	suppStore := store.NewAlertSuppressionStore(pool)
	alertStore.WithSuppressionStore(suppStore)
	h := NewHandler(alertStore, auditor, zerolog.Nop(), 5)
	h.WithSuppressionStore(suppStore)
	return h
}

func TestCreateSuppression_TypeScope_Backfills(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	userID := seedUser(t, pool, tenantID)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	// Three open alerts of type=operator.down (matching) + one other type (not matching).
	for i := 0; i < 3; i++ {
		if _, err := s.Create(ctx, store.CreateAlertParams{
			TenantID: tenantID, Type: "operator.down", Severity: "high", Source: "operator",
			Title: "down", Description: "d",
		}); err != nil {
			t.Fatalf("seed match: %v", err)
		}
	}
	if _, err := s.Create(ctx, store.CreateAlertParams{
		TenantID: tenantID, Type: "sim.data_spike", Severity: "high", Source: "sim",
		Title: "spike", Description: "d",
	}); err != nil {
		t.Fatalf("seed nomatch: %v", err)
	}

	h := seedSuppressionTestHandler(t, pool, &captureAuditor{})
	body := `{"scope_type":"type","scope_value":"operator.down","duration":"1h","reason":"maintenance"}`
	reqCtx := withUser(context.WithValue(context.Background(), apierr.TenantIDKey, tenantID), userID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(reqCtx)
	w := httptest.NewRecorder()
	h.CreateSuppression(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			ID           string `json:"id"`
			AppliedCount int64  `json:"applied_count"`
		} `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Data.AppliedCount != 3 {
		t.Errorf("applied_count = %d, want 3", resp.Data.AppliedCount)
	}

	// Verify DB: 3 operator.down rows are now suppressed, 1 sim.data_spike row remains open.
	var suppressed, otherOpen int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE tenant_id=$1 AND type='operator.down' AND state='suppressed'
		   AND meta->>'suppression_id' = $2`,
		tenantID, resp.Data.ID).Scan(&suppressed); err != nil {
		t.Fatalf("verify suppressed: %v", err)
	}
	if suppressed != 3 {
		t.Errorf("suppressed rows with matching meta = %d, want 3", suppressed)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE tenant_id=$1 AND type='sim.data_spike' AND state='open'`,
		tenantID).Scan(&otherOpen); err != nil {
		t.Fatalf("verify nomatch: %v", err)
	}
	if otherOpen != 1 {
		t.Errorf("non-matching open rows = %d, want 1 (untouched)", otherOpen)
	}
}

func TestCreateSuppression_DuplicateRuleName_Returns409(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	userID := seedUser(t, pool, tenantID)
	h := seedSuppressionTestHandler(t, pool, nil)

	body := `{"scope_type":"type","scope_value":"sim.data_spike","duration":"1h","rule_name":"daily-night"}`
	reqCtx := withUser(context.WithValue(context.Background(), apierr.TenantIDKey, tenantID), userID)

	// First create succeeds.
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(reqCtx)
	w1 := httptest.NewRecorder()
	h.CreateSuppression(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d body = %s", w1.Code, w1.Body.String())
	}
	// Cleanup any inserted rows when test exits — the seedTenant cleanup
	// already cascades to alert_suppressions via tenant FK.

	// Second create with the same rule_name → 409.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(body)).WithContext(reqCtx)
	w2 := httptest.NewRecorder()
	h.CreateSuppression(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second create: status = %d body = %s, want 409", w2.Code, w2.Body.String())
	}
	var resp apierr.ErrorResponse
	_ = json.NewDecoder(w2.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeDuplicate {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeDuplicate)
	}
}

func TestDeleteSuppression_RestoresAlerts(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	userID := seedUser(t, pool, tenantID)
	s := store.NewAlertStore(pool)
	ctx := context.Background()

	// Seed two open alerts, then create a suppression that backfills them.
	for i := 0; i < 2; i++ {
		if _, err := s.Create(ctx, store.CreateAlertParams{
			TenantID: tenantID, Type: "operator.down", Severity: "high", Source: "operator",
			Title: "down", Description: "d",
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	h := seedSuppressionTestHandler(t, pool, nil)
	createBody := `{"scope_type":"type","scope_value":"operator.down","duration":"1h"}`
	reqCtx := withUser(context.WithValue(context.Background(), apierr.TenantIDKey, tenantID), userID)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(createBody)).WithContext(reqCtx)
	createW := httptest.NewRecorder()
	h.CreateSuppression(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: status = %d", createW.Code)
	}
	var createResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(createW.Body).Decode(&createResp)
	suppID := createResp.Data.ID

	// Delete and confirm restoration.
	delCtx := withRouteID(reqCtx, suppID)
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/alerts/suppressions/"+suppID, nil).WithContext(delCtx)
	delW := httptest.NewRecorder()
	h.DeleteSuppression(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("delete: status = %d body = %s", delW.Code, delW.Body.String())
	}
	var delResp struct {
		Data struct {
			RestoredCount int64 `json:"restored_count"`
		} `json:"data"`
	}
	_ = json.NewDecoder(delW.Body).Decode(&delResp)
	if delResp.Data.RestoredCount != 2 {
		t.Errorf("restored_count = %d, want 2", delResp.Data.RestoredCount)
	}

	// Verify DB: alerts are back to open and meta.suppression_id is gone.
	var openAfter, stillSuppressed int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE tenant_id=$1 AND state='open' AND type='operator.down'`,
		tenantID).Scan(&openAfter); err != nil {
		t.Fatalf("verify open: %v", err)
	}
	if openAfter != 2 {
		t.Errorf("open alerts after delete = %d, want 2", openAfter)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE tenant_id=$1 AND meta ? 'suppression_id'`,
		tenantID).Scan(&stillSuppressed); err != nil {
		t.Fatalf("verify meta clean: %v", err)
	}
	if stillSuppressed != 0 {
		t.Errorf("alerts with meta.suppression_id = %d, want 0", stillSuppressed)
	}
}

func TestListSuppressions_ActiveOnlyFilter(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	tenantID := seedTenant(t, pool)
	suppStore := store.NewAlertSuppressionStore(pool)
	ctx := context.Background()

	// Seed 1 active + 1 expired suppression.
	now := time.Now().UTC()
	_, err := suppStore.Create(ctx, store.CreateAlertSuppressionParams{
		TenantID: tenantID, ScopeType: "type", ScopeValue: "sim.data_spike",
		ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("seed active: %v", err)
	}
	// Insert expired row directly via SQL (Create wouldn't accept past dates from API,
	// but the store layer accepts whatever we pass — and for this test we want
	// a row that is expired now).
	_, err = pool.Exec(ctx,
		`INSERT INTO alert_suppressions (tenant_id, scope_type, scope_value, expires_at)
		   VALUES ($1, 'type', 'sim.dataspike2', NOW() - INTERVAL '1 hour')`,
		tenantID)
	if err != nil {
		t.Fatalf("seed expired: %v", err)
	}

	h := seedSuppressionTestHandler(t, pool, nil)

	// active_only=true (default) → expect 1.
	reqCtx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/suppressions", nil).WithContext(reqCtx)
	w1 := httptest.NewRecorder()
	h.ListSuppressions(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("active list: status = %d", w1.Code)
	}
	var activeResp struct {
		Data []suppressionDTO `json:"data"`
	}
	_ = json.NewDecoder(w1.Body).Decode(&activeResp)
	if len(activeResp.Data) != 1 {
		t.Errorf("active list count = %d, want 1", len(activeResp.Data))
	}

	// active_only=false → expect 2.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/suppressions?active_only=false", nil).WithContext(reqCtx)
	w2 := httptest.NewRecorder()
	h.ListSuppressions(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("all list: status = %d", w2.Code)
	}
	var allResp struct {
		Data []suppressionDTO `json:"data"`
	}
	_ = json.NewDecoder(w2.Body).Decode(&allResp)
	if len(allResp.Data) != 2 {
		t.Errorf("all list count = %d, want 2", len(allResp.Data))
	}
}

// TestSuppression_FullLifecycle — DB-gated end-to-end regression for the full
// mute → trigger → suppress → unmute → restore pipeline (FIX-229).
//
// Phases:
//   1. Setup — seed tenant, two pre-existing open alerts (alert_A matches type
//      scope, alert_B is a control), wire handler + stores.
//   2. Create suppression via handler — assert 201 + applied_count==1 (alert_A
//      only), verify alert_A is 'suppressed' in DB, alert_B remains 'open'.
//   3. Trigger new alert via UpsertWithDedup — suppression-aware path must set
//      state='suppressed' and inject meta.suppression_id.
//   4. Delete suppression via handler — assert restored_count >= 2 (alert_A +
//      the trigger-time alert), verify both revert to 'open' with no
//      meta.suppression_id in DB.
//   5. Trigger again — no active suppression → new alert lands as 'open'.
func TestSuppression_FullLifecycle(t *testing.T) {
	pool := testAlertHandlerPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}

	tenantID := seedTenant(t, pool)
	userID := seedUser(t, pool, tenantID)

	// Register extra cleanup for alert_suppressions (seedTenant only cleans alerts + tenant).
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM alert_suppressions WHERE tenant_id = $1`, tenantID)
	})

	alertStore := store.NewAlertStore(pool)
	suppStore := store.NewAlertSuppressionStore(pool)
	alertStore.WithSuppressionStore(suppStore)
	h := NewHandler(alertStore, nil, zerolog.Nop(), 5)
	h.WithSuppressionStore(suppStore)

	ctx := context.Background()

	// ---- Phase 1: seed pre-existing open alerts ---------------------------------

	dedupKeyA := "operator_down:lifecycle-test"
	alertA, err := alertStore.Create(ctx, store.CreateAlertParams{
		TenantID:    tenantID,
		Type:        "operator_down",
		Severity:    "high",
		Source:      "operator",
		Title:       "Phase1-AlertA",
		Description: "should be suppressed by mute rule",
		DedupKey:    &dedupKeyA,
	})
	if err != nil {
		t.Fatalf("phase1 create alertA: %v", err)
	}
	if alertA.State != "open" {
		t.Fatalf("phase1 alertA initial state = %q, want open", alertA.State)
	}

	alertB, err := alertStore.Create(ctx, store.CreateAlertParams{
		TenantID:    tenantID,
		Type:        "other_event",
		Severity:    "low",
		Source:      "sim",
		Title:       "Phase1-AlertB-Control",
		Description: "must NOT be suppressed",
	})
	if err != nil {
		t.Fatalf("phase1 create alertB: %v", err)
	}

	// ---- Phase 2: create suppression via handler --------------------------------

	reqCtx := withUser(context.WithValue(context.Background(), apierr.TenantIDKey, tenantID), userID)
	createBody := `{"scope_type":"type","scope_value":"operator_down","duration":"1h","reason":"maintenance lifecycle test"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/suppressions", strings.NewReader(createBody)).WithContext(reqCtx)
	createW := httptest.NewRecorder()
	h.CreateSuppression(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("phase2 CreateSuppression: status = %d body = %s", createW.Code, createW.Body.String())
	}

	var createResp struct {
		Data struct {
			ID           string `json:"id"`
			AppliedCount int64  `json:"applied_count"`
		} `json:"data"`
	}
	if err := json.NewDecoder(createW.Body).Decode(&createResp); err != nil {
		t.Fatalf("phase2 decode response: %v", err)
	}
	suppID := createResp.Data.ID
	if suppID == "" {
		t.Fatal("phase2 suppression id is empty")
	}
	if createResp.Data.AppliedCount != 1 {
		t.Errorf("phase2 applied_count = %d, want 1 (only alertA matches type scope)", createResp.Data.AppliedCount)
	}

	// Verify alertA is now suppressed in DB with meta.suppression_id set.
	reloadedA, err := alertStore.GetByID(ctx, tenantID, alertA.ID)
	if err != nil {
		t.Fatalf("phase2 reload alertA: %v", err)
	}
	if reloadedA.State != "suppressed" {
		t.Errorf("phase2 alertA state = %q, want suppressed", reloadedA.State)
	}
	var metaA map[string]interface{}
	if err := json.Unmarshal(reloadedA.Meta, &metaA); err != nil {
		t.Fatalf("phase2 parse alertA meta: %v", err)
	}
	if metaA["suppression_id"] != suppID {
		t.Errorf("phase2 alertA meta.suppression_id = %v, want %q", metaA["suppression_id"], suppID)
	}

	// Control: alertB must remain open.
	reloadedB, err := alertStore.GetByID(ctx, tenantID, alertB.ID)
	if err != nil {
		t.Fatalf("phase2 reload alertB: %v", err)
	}
	if reloadedB.State != "open" {
		t.Errorf("phase2 alertB (control) state = %q, want open (must not be touched by mute rule)", reloadedB.State)
	}

	// ---- Phase 3: trigger new alert via UpsertWithDedup — suppression-aware ----

	dedupKeyTrigger := "operator_down:lifecycle-trigger"
	triggered, result, err := alertStore.UpsertWithDedup(ctx, store.CreateAlertParams{
		TenantID:    tenantID,
		Type:        "operator_down",
		Severity:    "high",
		Source:      "operator",
		Title:       "Phase3-Triggered",
		Description: "new alert during active mute",
		DedupKey:    &dedupKeyTrigger,
	}, 4)
	if err != nil {
		t.Fatalf("phase3 UpsertWithDedup: %v", err)
	}
	if triggered == nil {
		t.Fatal("phase3 UpsertWithDedup returned nil alert (UpsertCoolingDown?)")
	}
	_ = result
	if triggered.State != "suppressed" {
		t.Errorf("phase3 triggered alert state = %q, want suppressed (suppression-aware insert path)", triggered.State)
	}
	var metaTrigger map[string]interface{}
	if err := json.Unmarshal(triggered.Meta, &metaTrigger); err != nil {
		t.Fatalf("phase3 parse triggered meta: %v", err)
	}
	if metaTrigger["suppression_id"] != suppID {
		t.Errorf("phase3 triggered meta.suppression_id = %v, want %q", metaTrigger["suppression_id"], suppID)
	}

	// ---- Phase 4: delete suppression (unmute) -----------------------------------

	parsedSuppID, err := uuid.Parse(suppID)
	if err != nil {
		t.Fatalf("phase4 parse suppression uuid: %v", err)
	}
	delCtx := withRouteID(reqCtx, suppID)
	delBody := `{"reason":"lifecycle test done"}`
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/alerts/suppressions/"+suppID, strings.NewReader(delBody)).WithContext(delCtx)
	delW := httptest.NewRecorder()
	h.DeleteSuppression(delW, delReq)
	if delW.Code != http.StatusOK {
		t.Fatalf("phase4 DeleteSuppression: status = %d body = %s", delW.Code, delW.Body.String())
	}

	var delResp struct {
		Data struct {
			RestoredCount int64 `json:"restored_count"`
		} `json:"data"`
	}
	if err := json.NewDecoder(delW.Body).Decode(&delResp); err != nil {
		t.Fatalf("phase4 decode delete response: %v", err)
	}
	if delResp.Data.RestoredCount < 2 {
		t.Errorf("phase4 restored_count = %d, want >= 2 (alertA + triggered)", delResp.Data.RestoredCount)
	}

	// Verify alertA is back to open and meta.suppression_id is removed.
	afterDeleteA, err := alertStore.GetByID(ctx, tenantID, alertA.ID)
	if err != nil {
		t.Fatalf("phase4 reload alertA after delete: %v", err)
	}
	if afterDeleteA.State != "open" {
		t.Errorf("phase4 alertA state = %q after unmute, want open", afterDeleteA.State)
	}
	var metaAfterA map[string]interface{}
	if err := json.Unmarshal(afterDeleteA.Meta, &metaAfterA); err != nil {
		t.Fatalf("phase4 parse alertA meta after delete: %v", err)
	}
	if _, hasSuppID := metaAfterA["suppression_id"]; hasSuppID {
		t.Errorf("phase4 alertA meta still contains suppression_id after unmute: %v", metaAfterA)
	}

	// Verify triggered alert is also restored.
	afterDeleteTriggered, err := alertStore.GetByID(ctx, tenantID, triggered.ID)
	if err != nil {
		t.Fatalf("phase4 reload triggered alert after delete: %v", err)
	}
	if afterDeleteTriggered.State != "open" {
		t.Errorf("phase4 triggered alert state = %q after unmute, want open", afterDeleteTriggered.State)
	}
	_ = parsedSuppID

	// ---- Phase 5: trigger again — no active suppression → must land as open ----

	dedupKeyPhase5 := "operator_down:lifecycle-phase5"
	newAlert, _, err := alertStore.UpsertWithDedup(ctx, store.CreateAlertParams{
		TenantID:    tenantID,
		Type:        "operator_down",
		Severity:    "high",
		Source:      "operator",
		Title:       "Phase5-NoMute",
		Description: "trigger after suppression deleted — must be open",
		DedupKey:    &dedupKeyPhase5,
	}, 4)
	if err != nil {
		t.Fatalf("phase5 UpsertWithDedup: %v", err)
	}
	if newAlert == nil {
		t.Fatal("phase5 UpsertWithDedup returned nil alert")
	}
	if newAlert.State != "open" {
		t.Errorf("phase5 new alert state = %q, want open (suppression deleted, no active rule)", newAlert.State)
	}
}
