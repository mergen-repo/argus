package sim

// STORY-094 Task 4 — device-binding handler tests.
//
// Tests that exercise the store layer require a live Postgres and will
// t.Skip when DATABASE_URL is unset or the DB is unreachable.
// Tests that only exercise validation / early-return paths (e.g. invalid IMEI)
// never touch the DB and always run.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func bindingTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

type bindingTestFixture struct {
	tenantID   uuid.UUID
	operatorID uuid.UUID
	apnID      uuid.UUID
	simID      uuid.UUID
}

func seedBindingFixture(t *testing.T, pool *pgxpool.Pool) bindingTestFixture {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000

	var fix bindingTestFixture
	fix.operatorID = uuid.MustParse("00000000-0000-0000-0000-000000000100")

	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, contact_email) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("binding-test-%d", nonce), fmt.Sprintf("b%d@test.invalid", nonce),
	).Scan(&fix.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	if err := pool.QueryRow(ctx,
		`INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		 VALUES ($1, $2, $3, 'BT APN', 'iot', 'active') RETURNING id`,
		fix.tenantID, fix.operatorID,
		fmt.Sprintf("binding-apn-%d", nonce),
	).Scan(&fix.apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	iccid := fmt.Sprintf("8992540%09d", nonce%1_000_000_000)
	imsi := fmt.Sprintf("28601%010d", nonce%1_000_000_000)
	if len(iccid) > 22 {
		iccid = iccid[:22]
	}
	if len(imsi) > 15 {
		imsi = imsi[:15]
	}

	if err := pool.QueryRow(ctx,
		`INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		 VALUES ($1, $2, $3, $4, $5, 'physical', 'active') RETURNING id`,
		fix.tenantID, fix.operatorID, fix.apnID, iccid, imsi,
	).Scan(&fix.simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM imei_history WHERE sim_id = $1`, fix.simID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, fix.simID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, fix.apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, fix.tenantID)
	})
	return fix
}

func makeBindingRequest(method, path string, body []byte, tenantID, simID uuid.UUID) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", simID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

type fakeBindingAuditor struct {
	entries []audit.CreateEntryParams
}

func (f *fakeBindingAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	f.entries = append(f.entries, p)
	return &audit.Entry{}, nil
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestDeviceBindingHandler_Get_Success verifies 200 + DeviceBinding DTO.
func TestDeviceBindingHandler_Get_Success(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	h := NewDeviceBindingHandler(simStore, histStore, nil, nil, zerolog.Nop())

	req := makeBindingRequest(http.MethodGet, "/api/v1/sims/"+fix.simID.String()+"/device-binding", nil, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Get status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
}

// TestDeviceBindingHandler_Get_CrossTenant_404 verifies cross-tenant returns 404.
func TestDeviceBindingHandler_Get_CrossTenant_404(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	foreignTenant := uuid.New()
	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	h := NewDeviceBindingHandler(simStore, histStore, nil, nil, zerolog.Nop())

	req := makeBindingRequest(http.MethodGet, "/api/v1/sims/"+fix.simID.String()+"/device-binding", nil, foreignTenant, fix.simID)
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Get(cross-tenant) status = %d, want 404", w.Code)
	}
}

// TestDeviceBindingHandler_Patch_ValidBindingMode_200 verifies a valid binding_mode update.
func TestDeviceBindingHandler_Patch_ValidBindingMode_200(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	h := NewDeviceBindingHandler(simStore, histStore, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{"binding_mode": "strict"})
	req := makeBindingRequest(http.MethodPatch, "/api/v1/sims/"+fix.simID.String()+"/device-binding", body, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.Patch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Patch(valid mode) status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Status string `json:"status"`
		Data   struct {
			BindingMode *string `json:"binding_mode"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.BindingMode == nil || *resp.Data.BindingMode != "strict" {
		t.Errorf("binding_mode = %v, want strict", resp.Data.BindingMode)
	}
}

// TestDeviceBindingHandler_Patch_InvalidBindingMode_422 verifies 422 INVALID_BINDING_MODE.
func TestDeviceBindingHandler_Patch_InvalidBindingMode_422(t *testing.T) {
	simID := uuid.New()
	tenantID := uuid.New()

	h := &DeviceBindingHandler{logger: zerolog.Nop()}

	body, _ := json.Marshal(map[string]interface{}{"binding_mode": "bad-mode"})
	req := makeBindingRequest(http.MethodPatch, "/api/v1/sims/"+simID.String()+"/device-binding", body, tenantID, simID)
	w := httptest.NewRecorder()
	h.Patch(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Patch(invalid mode) status = %d, want 422", w.Code)
	}

	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeInvalidBindingMode {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidBindingMode)
	}
}

// TestDeviceBindingHandler_Patch_InvalidIMEI_422 verifies 422 INVALID_IMEI.
func TestDeviceBindingHandler_Patch_InvalidIMEI_422(t *testing.T) {
	simID := uuid.New()
	tenantID := uuid.New()

	h := &DeviceBindingHandler{logger: zerolog.Nop()}

	body, _ := json.Marshal(map[string]interface{}{"bound_imei": "123"})
	req := makeBindingRequest(http.MethodPatch, "/api/v1/sims/"+simID.String()+"/device-binding", body, tenantID, simID)
	w := httptest.NewRecorder()
	h.Patch(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Patch(invalid imei) status = %d, want 422", w.Code)
	}

	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeInvalidIMEI {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidIMEI)
	}
}

// TestDeviceBindingHandler_Patch_NullClears verifies that null values clear columns.
func TestDeviceBindingHandler_Patch_NullClears(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	h := NewDeviceBindingHandler(simStore, histStore, nil, nil, zerolog.Nop())

	// First set a binding_mode
	ctx := context.Background()
	mode := "strict"
	_, err := simStore.SetDeviceBinding(ctx, fix.tenantID, fix.simID, &mode, nil, nil)
	if err != nil {
		t.Fatalf("pre-set binding: %v", err)
	}

	// Now PATCH with null to clear
	body := []byte(`{"binding_mode": null}`)
	req := makeBindingRequest(http.MethodPatch, "/api/v1/sims/"+fix.simID.String()+"/device-binding", body, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.Patch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Patch(null clear) status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			BindingMode *string `json:"binding_mode"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.BindingMode != nil {
		t.Errorf("binding_mode = %v, want nil (cleared)", resp.Data.BindingMode)
	}
}

// TestDeviceBindingHandler_Patch_AuditWritten verifies the audit entry is emitted.
func TestDeviceBindingHandler_Patch_AuditWritten(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	auditor := &fakeBindingAuditor{}
	h := NewDeviceBindingHandler(simStore, histStore, auditor, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{"binding_mode": "first-use"})
	req := makeBindingRequest(http.MethodPatch, "/api/v1/sims/"+fix.simID.String()+"/device-binding", body, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.Patch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Patch status = %d, want 200", w.Code)
	}
	if len(auditor.entries) == 0 {
		t.Fatal("expected audit entry, got none")
	}
	if auditor.entries[0].Action != "sim.binding_mode_changed" {
		t.Errorf("audit action = %q, want sim.binding_mode_changed", auditor.entries[0].Action)
	}
	if auditor.entries[0].EntityID != fix.simID.String() {
		t.Errorf("audit entity_id = %q, want %q", auditor.entries[0].EntityID, fix.simID.String())
	}
}

// TestDeviceBindingHandler_Patch_NoOp_DoesNotEmitAudit is the F-A6 regression
// guard: PATCH with payload identical to current state must NOT emit an audit
// entry. AC-14 still satisfied — audit is emitted on every state-changing
// operation, just not on no-ops.
func TestDeviceBindingHandler_Patch_NoOp_DoesNotEmitAudit(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	auditor := &fakeBindingAuditor{}
	h := NewDeviceBindingHandler(simStore, histStore, auditor, nil, zerolog.Nop())

	// First PATCH: set binding_mode='strict' — should emit one audit entry.
	body, _ := json.Marshal(map[string]interface{}{"binding_mode": "strict"})
	req := makeBindingRequest(http.MethodPatch, "/api/v1/sims/"+fix.simID.String()+"/device-binding", body, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.Patch(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first patch status = %d, want 200", w.Code)
	}
	if len(auditor.entries) != 1 {
		t.Fatalf("after first patch entries = %d, want 1", len(auditor.entries))
	}

	// Second PATCH: same binding_mode='strict' — should NOT emit a new audit entry.
	body2, _ := json.Marshal(map[string]interface{}{"binding_mode": "strict"})
	req2 := makeBindingRequest(http.MethodPatch, "/api/v1/sims/"+fix.simID.String()+"/device-binding", body2, fix.tenantID, fix.simID)
	w2 := httptest.NewRecorder()
	h.Patch(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second (no-op) patch status = %d, want 200", w2.Code)
	}
	if len(auditor.entries) != 1 {
		t.Errorf("F-A6 regression: no-op patch emitted audit (entries = %d, want 1)", len(auditor.entries))
	}
}

// TestDeviceBindingHandler_History_Pagination verifies cursor-based pagination.
func TestDeviceBindingHandler_History_Pagination(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)

	// Insert 3 history rows
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		imei := fmt.Sprintf("35%013d", i)
		if _, err := histStore.Append(ctx, fix.tenantID, store.AppendIMEIHistoryParams{
			SIMID:           fix.simID,
			ObservedIMEI:    imei,
			CaptureProtocol: "radius",
			WasMismatch:     false,
			AlarmRaised:     false,
		}); err != nil {
			t.Fatalf("append history row %d: %v", i, err)
		}
	}

	h := NewDeviceBindingHandler(simStore, histStore, nil, nil, zerolog.Nop())

	req := makeBindingRequest(http.MethodGet, "/api/v1/sims/"+fix.simID.String()+"/imei-history?limit=2", nil, fix.tenantID, fix.simID)
	req.URL.RawQuery = "limit=2"
	w := httptest.NewRecorder()
	h.GetIMEIHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetIMEIHistory status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Status string                   `json:"status"`
		Data   []imeiHistoryRowResponse `json:"data"`
		Meta   struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
			Limit      int    `json:"limit"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Errorf("page 1 rows = %d, want 2", len(resp.Data))
	}
	if !resp.Meta.HasMore {
		t.Error("expected has_more=true after page 1")
	}
	if resp.Meta.NextCursor == "" {
		t.Error("expected non-empty next_cursor after page 1")
	}
}

// TestDeviceBindingHandler_History_ProtocolFilter verifies the protocol filter works.
func TestDeviceBindingHandler_History_ProtocolFilter(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	ctx := context.Background()

	// Insert one radius + one 5g_sba row
	for i, proto := range []string{"radius", "5g_sba"} {
		imei := fmt.Sprintf("36%013d", i)
		if _, err := histStore.Append(ctx, fix.tenantID, store.AppendIMEIHistoryParams{
			SIMID:           fix.simID,
			ObservedIMEI:    imei,
			CaptureProtocol: proto,
			WasMismatch:     false,
			AlarmRaised:     false,
		}); err != nil {
			t.Fatalf("append %s: %v", proto, err)
		}
	}

	h := NewDeviceBindingHandler(simStore, histStore, nil, nil, zerolog.Nop())

	req := makeBindingRequest(http.MethodGet, "/api/v1/sims/"+fix.simID.String()+"/imei-history", nil, fix.tenantID, fix.simID)
	q := req.URL.Query()
	q.Set("protocol", "radius")
	req.URL.RawQuery = q.Encode()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.simID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.GetIMEIHistory(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetIMEIHistory(protocol=radius) = %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []imeiHistoryRowResponse `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, row := range resp.Data {
		if row.CaptureProtocol != "radius" {
			t.Errorf("expected only radius rows, got %q", row.CaptureProtocol)
		}
	}
}

// ── fakeBindingNotifier ───────────────────────────────────────────────────────

type fakeBindingNotifier struct {
	calls []struct {
		subject string
		payload interface{}
	}
}

func (f *fakeBindingNotifier) Publish(_ context.Context, subject string, payload interface{}) error {
	f.calls = append(f.calls, struct {
		subject string
		payload interface{}
	}{subject, payload})
	return nil
}

// ── RePair tests ──────────────────────────────────────────────────────────────

// TestDeviceBindingHandler_RePair_FirstCall_200_AuditAndNotif verifies that a
// first re-pair call clears bound_imei, sets binding_status=pending, emits one
// audit entry (sim.imei_repaired) and one notification (device.binding_re_paired).
func TestDeviceBindingHandler_RePair_FirstCall_200_AuditAndNotif(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	ctx := context.Background()
	imei := "359211080123456"
	mode := "strict"
	status := "verified"
	simStore := store.NewSIMStore(pool)
	if _, err := simStore.SetDeviceBinding(ctx, fix.tenantID, fix.simID, &mode, &imei, &status); err != nil {
		t.Fatalf("pre-set binding: %v", err)
	}

	histStore := store.NewIMEIHistoryStore(pool, simStore)
	auditor := &fakeBindingAuditor{}
	notifier := &fakeBindingNotifier{}
	h := NewDeviceBindingHandler(simStore, histStore, auditor, notifier, zerolog.Nop())

	req := makeBindingRequest(http.MethodPost, "/api/v1/sims/"+fix.simID.String()+"/device-binding/re-pair", nil, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.RePair(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("RePair status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			BoundIMEI     *string `json:"bound_imei"`
			BindingStatus *string `json:"binding_status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.BoundIMEI != nil {
		t.Errorf("bound_imei = %v, want nil after re-pair", resp.Data.BoundIMEI)
	}
	if resp.Data.BindingStatus == nil || *resp.Data.BindingStatus != "pending" {
		t.Errorf("binding_status = %v, want pending", resp.Data.BindingStatus)
	}
	if len(auditor.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(auditor.entries))
	}
	if auditor.entries[0].Action != "sim.imei_repaired" {
		t.Errorf("audit action = %q, want sim.imei_repaired", auditor.entries[0].Action)
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("notif calls = %d, want 1", len(notifier.calls))
	}
	if notifier.calls[0].subject != "device.binding_re_paired" {
		t.Errorf("notif subject = %q, want device.binding_re_paired", notifier.calls[0].subject)
	}
}

// TestDeviceBindingHandler_RePair_SecondCall_Idempotent_NoSideEffects verifies
// that calling re-pair on an already-cleared SIM returns 200 with no new audit
// or notification (AC-3 idempotency).
func TestDeviceBindingHandler_RePair_SecondCall_Idempotent_NoSideEffects(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	ctx := context.Background()
	pending := "pending"
	if _, err := store.NewSIMStore(pool).SetDeviceBinding(ctx, fix.tenantID, fix.simID, nil, nil, &pending); err != nil {
		t.Fatalf("pre-set cleared binding: %v", err)
	}

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	auditor := &fakeBindingAuditor{}
	notifier := &fakeBindingNotifier{}
	h := NewDeviceBindingHandler(simStore, histStore, auditor, notifier, zerolog.Nop())

	req := makeBindingRequest(http.MethodPost, "/api/v1/sims/"+fix.simID.String()+"/device-binding/re-pair", nil, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.RePair(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("RePair(idempotent) status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if len(auditor.entries) != 0 {
		t.Errorf("AC-3: idempotent call emitted audit (entries = %d, want 0)", len(auditor.entries))
	}
	if len(notifier.calls) != 0 {
		t.Errorf("AC-3: idempotent call emitted notification (calls = %d, want 0)", len(notifier.calls))
	}
}

// TestDeviceBindingHandler_RePair_PreservesBindingMode verifies that re-pair
// retains the existing binding_mode value.
func TestDeviceBindingHandler_RePair_PreservesBindingMode(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	ctx := context.Background()
	mode := "strict"
	imei := "359211080654321"
	status := "verified"
	if _, err := store.NewSIMStore(pool).SetDeviceBinding(ctx, fix.tenantID, fix.simID, &mode, &imei, &status); err != nil {
		t.Fatalf("pre-set binding: %v", err)
	}

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	h := NewDeviceBindingHandler(simStore, histStore, nil, nil, zerolog.Nop())

	req := makeBindingRequest(http.MethodPost, "/api/v1/sims/"+fix.simID.String()+"/device-binding/re-pair", nil, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.RePair(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("RePair status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			BindingMode *string `json:"binding_mode"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.BindingMode == nil || *resp.Data.BindingMode != "strict" {
		t.Errorf("binding_mode = %v, want strict (preserved)", resp.Data.BindingMode)
	}
}

// TestDeviceBindingHandler_RePair_CrossTenant_404 verifies cross-tenant returns 404.
func TestDeviceBindingHandler_RePair_CrossTenant_404(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	foreignTenant := uuid.New()
	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	h := NewDeviceBindingHandler(simStore, histStore, nil, nil, zerolog.Nop())

	req := makeBindingRequest(http.MethodPost, "/api/v1/sims/"+fix.simID.String()+"/device-binding/re-pair", nil, foreignTenant, fix.simID)
	w := httptest.NewRecorder()
	h.RePair(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("RePair(cross-tenant) status = %d, want 404", w.Code)
	}
}

// TestDeviceBindingHandler_RePair_RBAC_403 verifies that a missing tenant
// context (proxy for viewer/policy_author role bypass) returns 403.
func TestDeviceBindingHandler_RePair_RBAC_403(t *testing.T) {
	simID := uuid.New()

	h := &DeviceBindingHandler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+simID.String()+"/device-binding/re-pair", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", simID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.RePair(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("RePair(no tenant) status = %d, want 403", w.Code)
	}
}

// TestDeviceBindingHandler_RePair_AuditCarriesActor verifies the JWT user_id
// propagates to audit ActorID (UserID in CreateEntryParams).
func TestDeviceBindingHandler_RePair_AuditCarriesActor(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	ctx := context.Background()
	imei := "359211080111222"
	mode := "first-use"
	status := "verified"
	if _, err := store.NewSIMStore(pool).SetDeviceBinding(ctx, fix.tenantID, fix.simID, &mode, &imei, &status); err != nil {
		t.Fatalf("pre-set binding: %v", err)
	}

	actorID := uuid.New()
	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	auditor := &fakeBindingAuditor{}
	h := NewDeviceBindingHandler(simStore, histStore, auditor, nil, zerolog.Nop())

	req := makeBindingRequest(http.MethodPost, "/api/v1/sims/"+fix.simID.String()+"/device-binding/re-pair", nil, fix.tenantID, fix.simID)
	reqCtx := context.WithValue(req.Context(), apierr.UserIDKey, actorID)
	req = req.WithContext(reqCtx)

	w := httptest.NewRecorder()
	h.RePair(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("RePair status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if len(auditor.entries) == 0 {
		t.Fatal("expected audit entry, got none")
	}
	if auditor.entries[0].UserID == nil || *auditor.entries[0].UserID != actorID {
		t.Errorf("audit UserID = %v, want %v", auditor.entries[0].UserID, actorID)
	}
}

// TestDeviceBindingHandler_RePair_PreviousBoundIMEINotEmpty_RecordedInAudit
// verifies the audit before-payload carries the non-empty previous bound_imei.
func TestDeviceBindingHandler_RePair_PreviousBoundIMEINotEmpty_RecordedInAudit(t *testing.T) {
	pool := bindingTestPool(t)
	fix := seedBindingFixture(t, pool)

	ctx := context.Background()
	imei := "359211080999888"
	mode := "strict"
	status := "verified"
	if _, err := store.NewSIMStore(pool).SetDeviceBinding(ctx, fix.tenantID, fix.simID, &mode, &imei, &status); err != nil {
		t.Fatalf("pre-set binding: %v", err)
	}

	simStore := store.NewSIMStore(pool)
	histStore := store.NewIMEIHistoryStore(pool, simStore)
	auditor := &fakeBindingAuditor{}
	h := NewDeviceBindingHandler(simStore, histStore, auditor, nil, zerolog.Nop())

	req := makeBindingRequest(http.MethodPost, "/api/v1/sims/"+fix.simID.String()+"/device-binding/re-pair", nil, fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.RePair(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("RePair status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if len(auditor.entries) == 0 {
		t.Fatal("expected audit entry, got none")
	}

	var before bindingAuditPayload
	if err := json.Unmarshal(auditor.entries[0].BeforeData, &before); err != nil {
		t.Fatalf("unmarshal before payload: %v", err)
	}
	if before.BoundIMEI == nil || *before.BoundIMEI != imei {
		t.Errorf("audit before.bound_imei = %v, want %q", before.BoundIMEI, imei)
	}
}
