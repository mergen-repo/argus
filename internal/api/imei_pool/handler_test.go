package imeipool

// STORY-095 Task 4 — IMEI pool handler tests.
//
// Tests that exercise the store layer require a live Postgres and will
// t.Skip when DATABASE_URL is unset or the DB is unreachable.
// Tests that only exercise validation / early-return paths never touch
// the DB and always run.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func poolTestPool(t *testing.T) *pgxpool.Pool {
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

type poolTestFixture struct {
	tenantID uuid.UUID
}

func seedPoolFixture(t *testing.T, pool *pgxpool.Pool) poolTestFixture {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000

	var fix poolTestFixture
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, contact_email) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("imeipool-test-%d", nonce), fmt.Sprintf("ip%d@test.invalid", nonce),
	).Scan(&fix.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM imei_whitelist WHERE tenant_id = $1`, fix.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM imei_greylist WHERE tenant_id = $1`, fix.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM imei_blacklist WHERE tenant_id = $1`, fix.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, fix.tenantID)
	})
	return fix
}

func makePoolRequest(method, path string, body []byte, tenantID uuid.UUID, chiParams map[string]string) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	rctx := chi.NewRouteContext()
	for k, v := range chiParams {
		rctx.URLParams.Add(k, v)
	}
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

type fakePoolAuditor struct {
	entries []audit.CreateEntryParams
}

func (f *fakePoolAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	f.entries = append(f.entries, p)
	return &audit.Entry{}, nil
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestIMEIPoolHandler_List_ValidKind_200 verifies List returns 200 for a valid pool kind.
func TestIMEIPoolHandler_List_ValidKind_200(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	h := NewHandler(store.NewIMEIPoolStore(pg), nil, nil, nil, nil, nil, zerolog.Nop())

	req := makePoolRequest(http.MethodGet, "/api/v1/imei-pools/whitelist", nil, fix.tenantID, map[string]string{"kind": "whitelist"})
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("List(whitelist) status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
}

// TestIMEIPoolHandler_List_InvalidKind_400 verifies List returns 400 INVALID_POOL_KIND.
func TestIMEIPoolHandler_List_InvalidKind_400(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}

	req := makePoolRequest(http.MethodGet, "/api/v1/imei-pools/badpool", nil, tenantID, map[string]string{"kind": "badpool"})
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("List(badpool) status = %d, want 400", w.Code)
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeInvalidPoolKind {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidPoolKind)
	}
}

// TestIMEIPoolHandler_Add_FullIMEI_201 verifies adding a full_imei entry to whitelist returns 201.
func TestIMEIPoolHandler_Add_FullIMEI_201(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	h := NewHandler(store.NewIMEIPoolStore(pg), nil, nil, nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"kind":        "full_imei",
		"imei_or_tac": "123456789012345",
	})
	req := makePoolRequest(http.MethodPost, "/api/v1/imei-pools/whitelist", body, fix.tenantID, map[string]string{"kind": "whitelist"})
	w := httptest.NewRecorder()
	h.Add(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Add(full_imei) status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
}

// TestIMEIPoolHandler_Add_TACRange_201 verifies adding a tac_range entry to whitelist returns 201.
func TestIMEIPoolHandler_Add_TACRange_201(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	h := NewHandler(store.NewIMEIPoolStore(pg), nil, nil, nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"kind":        "tac_range",
		"imei_or_tac": "35123456",
	})
	req := makePoolRequest(http.MethodPost, "/api/v1/imei-pools/whitelist", body, fix.tenantID, map[string]string{"kind": "whitelist"})
	w := httptest.NewRecorder()
	h.Add(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Add(tac_range) status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

// TestIMEIPoolHandler_Add_InvalidIMEI_422 verifies 422 INVALID_IMEI for bad full_imei value.
func TestIMEIPoolHandler_Add_InvalidIMEI_422(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}

	body, _ := json.Marshal(map[string]interface{}{
		"kind":        "full_imei",
		"imei_or_tac": "123",
	})
	req := makePoolRequest(http.MethodPost, "/api/v1/imei-pools/whitelist", body, tenantID, map[string]string{"kind": "whitelist"})
	w := httptest.NewRecorder()
	h.Add(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Add(invalid imei) status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeInvalidIMEI {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidIMEI)
	}
}

// TestIMEIPoolHandler_Add_GreylistWithoutQuarantineReason_422 verifies 422 MISSING_QUARANTINE_REASON.
func TestIMEIPoolHandler_Add_GreylistWithoutQuarantineReason_422(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}

	body, _ := json.Marshal(map[string]interface{}{
		"kind":        "full_imei",
		"imei_or_tac": "123456789012345",
	})
	req := makePoolRequest(http.MethodPost, "/api/v1/imei-pools/greylist", body, tenantID, map[string]string{"kind": "greylist"})
	w := httptest.NewRecorder()
	h.Add(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Add(greylist,no quarantine) status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeMissingQuarantineReason {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeMissingQuarantineReason)
	}
}

// TestIMEIPoolHandler_Add_BlacklistWithoutImportedFrom_422 verifies 422 INVALID_IMPORTED_FROM.
func TestIMEIPoolHandler_Add_BlacklistWithoutImportedFrom_422(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}

	body, _ := json.Marshal(map[string]interface{}{
		"kind":         "full_imei",
		"imei_or_tac":  "123456789012345",
		"block_reason": "stolen",
	})
	req := makePoolRequest(http.MethodPost, "/api/v1/imei-pools/blacklist", body, tenantID, map[string]string{"kind": "blacklist"})
	w := httptest.NewRecorder()
	h.Add(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Add(blacklist,no imported_from) status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeInvalidImportedFrom {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidImportedFrom)
	}
}

// TestIMEIPoolHandler_Add_Duplicate_409 verifies 409 IMEI_POOL_DUPLICATE on re-insert.
func TestIMEIPoolHandler_Add_Duplicate_409(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	h := NewHandler(store.NewIMEIPoolStore(pg), nil, nil, nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"kind":        "full_imei",
		"imei_or_tac": "999888777666555",
	})

	for i := 0; i < 2; i++ {
		req := makePoolRequest(http.MethodPost, "/api/v1/imei-pools/whitelist", body, fix.tenantID, map[string]string{"kind": "whitelist"})
		w := httptest.NewRecorder()
		h.Add(w, req)
		if i == 0 && w.Code != http.StatusCreated {
			t.Fatalf("first Add status = %d, want 201; body: %s", w.Code, w.Body.String())
		}
		if i == 1 {
			if w.Code != http.StatusConflict {
				t.Fatalf("duplicate Add status = %d, want 409; body: %s", w.Code, w.Body.String())
			}
			var resp apierr.ErrorResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error.Code != apierr.CodeIMEIPoolDuplicate {
				t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeIMEIPoolDuplicate)
			}
		}
	}
}

// TestIMEIPoolHandler_Delete_Success_204 verifies successful delete returns 204.
func TestIMEIPoolHandler_Delete_Success_204(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	poolStore := store.NewIMEIPoolStore(pg)
	h := NewHandler(poolStore, nil, nil, nil, nil, nil, zerolog.Nop())

	entry, err := poolStore.Add(context.Background(), fix.tenantID, store.PoolWhitelist, store.AddEntryParams{
		Kind:      store.EntryKindFullIMEI,
		IMEIOrTAC: "111222333444555",
	})
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	req := makePoolRequest(http.MethodDelete, "/api/v1/imei-pools/whitelist/"+entry.ID.String(), nil, fix.tenantID, map[string]string{
		"kind": "whitelist",
		"id":   entry.ID.String(),
	})
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

// TestIMEIPoolHandler_Delete_NotFound_404 verifies 404 POOL_ENTRY_NOT_FOUND for missing entry.
func TestIMEIPoolHandler_Delete_NotFound_404(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	h := NewHandler(store.NewIMEIPoolStore(pg), nil, nil, nil, nil, nil, zerolog.Nop())

	missingID := uuid.New()
	req := makePoolRequest(http.MethodDelete, "/api/v1/imei-pools/whitelist/"+missingID.String(), nil, fix.tenantID, map[string]string{
		"kind": "whitelist",
		"id":   missingID.String(),
	})
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Delete(missing) status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodePoolEntryNotFound {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodePoolEntryNotFound)
	}
}

// ── fake stubs for BulkImport + Lookup tests ──────────────────────────────────

type fakeJobStore struct {
	created *store.Job
}

func (f *fakeJobStore) Create(_ context.Context, p store.CreateJobParams) (*store.Job, error) {
	j := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Type:     p.Type,
	}
	f.created = j
	return j, nil
}

type fakeEventBus struct{}

func (f *fakeEventBus) Publish(_ context.Context, _ string, _ interface{}) error {
	return nil
}

// makeMultipartCSV builds a multipart form-data request body with a CSV file field.
func makeMultipartCSV(t *testing.T, csvContent string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("file", "import.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = fw.Write([]byte(csvContent))
	w.Close()
	return body, w.FormDataContentType()
}

// makeBulkImportRequest creates an HTTP request for the BulkImport endpoint.
func makeBulkImportRequest(t *testing.T, csvContent string, tenantID uuid.UUID, kind string) *http.Request {
	t.Helper()
	body, contentType := makeMultipartCSV(t, csvContent)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/imei-pools/"+kind+"/import", body)
	req.Header.Set("Content-Type", contentType)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", kind)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

const validImportHeader = "imei_or_tac,kind,device_model,description,quarantine_reason,block_reason,imported_from\n"

// ── Task 5 tests ──────────────────────────────────────────────────────────────

// TestIMEIPoolHandler_BulkImport_HappyPath_202 verifies 3-row CSV → 202 with job_id.
func TestIMEIPoolHandler_BulkImport_HappyPath_202(t *testing.T) {
	tenantID := uuid.New()
	fakeJob := &fakeJobStore{}
	h := &Handler{
		logger:   zerolog.Nop(),
		jobStore: fakeJob,
		eventBus: &fakeEventBus{},
	}
	csvContent := validImportHeader +
		"123456789012345,full_imei,,,,,\n" +
		"234567890123456,full_imei,ModelX,desc,,block,manual\n" +
		"35400400,tac_range,,,,,\n"

	req := makeBulkImportRequest(t, csvContent, tenantID, "whitelist")
	w := httptest.NewRecorder()
	h.BulkImport(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("BulkImport status = %d, want 202; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data is not a map: %T", resp.Data)
	}
	if _, ok := dataMap["job_id"]; !ok {
		t.Errorf("response data missing job_id field")
	}
}

// TestIMEIPoolHandler_BulkImport_BadHeader_400 verifies wrong column order → 400 INVALID_CSV.
func TestIMEIPoolHandler_BulkImport_BadHeader_400(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}
	csvContent := "kind,imei_or_tac,device_model,description,quarantine_reason,block_reason,imported_from\n" +
		"full_imei,123456789012345,,,,,\n"

	req := makeBulkImportRequest(t, csvContent, tenantID, "whitelist")
	w := httptest.NewRecorder()
	h.BulkImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("BulkImport(bad header) status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != "INVALID_CSV" {
		t.Errorf("error code = %q, want INVALID_CSV", resp.Error.Code)
	}
}

// TestIMEIPoolHandler_BulkImport_EmptyFile_400 verifies 0-row CSV → 400 EMPTY_FILE.
func TestIMEIPoolHandler_BulkImport_EmptyFile_400(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}
	csvContent := validImportHeader

	req := makeBulkImportRequest(t, csvContent, tenantID, "whitelist")
	w := httptest.NewRecorder()
	h.BulkImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("BulkImport(empty) status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != "EMPTY_FILE" {
		t.Errorf("error code = %q, want EMPTY_FILE", resp.Error.Code)
	}
}

// TestIMEIPoolHandler_BulkImport_TooManyRows_422 verifies row-cap → 422 TOO_MANY_ROWS.
// Uses a stub limit of 3 rows to avoid heap pressure.
func TestIMEIPoolHandler_BulkImport_TooManyRows_422(t *testing.T) {
	orig := maxImportRows
	maxImportRows = 3
	t.Cleanup(func() { maxImportRows = orig })

	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}

	var sb strings.Builder
	sb.WriteString(validImportHeader)
	for i := 0; i < 4; i++ {
		sb.WriteString("123456789012345,full_imei,,,,,\n")
	}

	req := makeBulkImportRequest(t, sb.String(), tenantID, "whitelist")
	w := httptest.NewRecorder()
	h.BulkImport(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("BulkImport(too many) status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != "TOO_MANY_ROWS" {
		t.Errorf("error code = %q, want TOO_MANY_ROWS", resp.Error.Code)
	}
}

// TestIMEIPoolHandler_BulkImport_InvalidPoolKind_400 verifies kind=xyz → 400 INVALID_POOL_KIND.
func TestIMEIPoolHandler_BulkImport_InvalidPoolKind_400(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}
	csvContent := validImportHeader + "123456789012345,full_imei,,,,,\n"

	req := makeBulkImportRequest(t, csvContent, tenantID, "xyz")
	w := httptest.NewRecorder()
	h.BulkImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("BulkImport(bad kind) status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeInvalidPoolKind {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidPoolKind)
	}
}

// makeLookupRequest creates a GET request for the Lookup endpoint.
func makeLookupRequest(imei string, tenantID uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/imei-pools/lookup?imei="+imei, nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	return req.WithContext(ctx)
}

// TestIMEIPoolHandler_Lookup_ValidIMEI_200 verifies 15-digit IMEI → 200 with arrays.
func TestIMEIPoolHandler_Lookup_ValidIMEI_200(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	h := NewHandler(store.NewIMEIPoolStore(pg), nil, nil, nil, nil, nil, zerolog.Nop())

	req := makeLookupRequest("123456789012345", fix.tenantID)
	w := httptest.NewRecorder()
	h.Lookup(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Lookup status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
}

// TestIMEIPoolHandler_Lookup_InvalidIMEI_422 verifies 14-digit input → 422 INVALID_IMEI.
func TestIMEIPoolHandler_Lookup_InvalidIMEI_422(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}

	req := makeLookupRequest("12345678901234", tenantID)
	w := httptest.NewRecorder()
	h.Lookup(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Lookup(14-digit) status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != apierr.CodeInvalidIMEI {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidIMEI)
	}
}

// TestIMEIPoolHandler_Add_CSVInjectionRejected_422 (STORY-095 Gate F-A5)
// verifies the Add handler rejects operator-supplied strings starting with
// =, +, -, @, or tab with 422 CSV_INJECTION_REJECTED — parity with the bulk
// import worker so single-entry adds cannot smuggle spreadsheet formulas.
func TestIMEIPoolHandler_Add_CSVInjectionRejected_422(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}
	cases := []struct {
		field string
		body  map[string]interface{}
	}{
		{"description", map[string]interface{}{"kind": "full_imei", "imei_or_tac": "123456789012345", "description": "=cmd|'/c calc'!A1"}},
		{"device_model", map[string]interface{}{"kind": "full_imei", "imei_or_tac": "123456789012345", "device_model": "+1+1"}},
		{"description (tab)", map[string]interface{}{"kind": "full_imei", "imei_or_tac": "123456789012345", "description": "\tinjected"}},
		{"description (@)", map[string]interface{}{"kind": "full_imei", "imei_or_tac": "123456789012345", "description": "@SUM(A1)"}},
		{"description (-)", map[string]interface{}{"kind": "full_imei", "imei_or_tac": "123456789012345", "description": "-2+5"}},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := makePoolRequest(http.MethodPost, "/api/v1/imei-pools/whitelist", body, tenantID, map[string]string{"kind": "whitelist"})
			w := httptest.NewRecorder()
			h.Add(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("Add(%s) status = %d, want 422; body: %s", tc.field, w.Code, w.Body.String())
			}
			var resp apierr.ErrorResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error.Code != apierr.CodeCSVInjectionRejected {
				t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeCSVInjectionRejected)
			}
		})
	}
}

// TestIMEIPoolHandler_Add_BenignValues_NoCSVRejection (STORY-095 Gate F-A5)
// verifies the CSV-injection guard does NOT mis-fire on common device-model
// strings. We avoid hitting the real store by using an invalid IMEI length
// so the handler short-circuits at the IMEI-format check (returns 422
// INVALID_IMEI), never reaching poolStore.Add. We assert the response code
// is anything OTHER than CSV_INJECTION_REJECTED.
func TestIMEIPoolHandler_Add_BenignValues_NoCSVRejection(t *testing.T) {
	tenantID := uuid.New()
	h := &Handler{logger: zerolog.Nop()}
	for _, dm := range []string{"Quectel BG95", "5G CAT-M1", "0000-IoT", "fleet-A"} {
		body, _ := json.Marshal(map[string]interface{}{
			"kind":         "full_imei",
			"imei_or_tac":  "BADLEN", // forces 422 INVALID_IMEI before store dispatch
			"device_model": dm,
		})
		req := makePoolRequest(http.MethodPost, "/api/v1/imei-pools/whitelist", body, tenantID, map[string]string{"kind": "whitelist"})
		w := httptest.NewRecorder()
		h.Add(w, req)
		if w.Code == http.StatusUnprocessableEntity {
			var resp apierr.ErrorResponse
			_ = json.NewDecoder(w.Body).Decode(&resp)
			if resp.Error.Code == apierr.CodeCSVInjectionRejected {
				t.Errorf("benign device_model %q incorrectly rejected as CSV injection", dm)
			}
		}
	}
}

// TestIMEIPoolHandler_Lookup_NoMatches_200_EmptyArrays verifies IMEI in no pool → 200 with empty arrays.
func TestIMEIPoolHandler_Lookup_NoMatches_200_EmptyArrays(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	simStore := store.NewSIMStore(pg)
	historyStore := store.NewIMEIHistoryStore(pg, simStore)
	h := NewHandler(store.NewIMEIPoolStore(pg), simStore, historyStore, nil, nil, nil, zerolog.Nop())

	req := makeLookupRequest("999111222333444", fix.tenantID)
	w := httptest.NewRecorder()
	h.Lookup(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Lookup(no matches) status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		Data   struct {
			Lists     []interface{} `json:"lists"`
			BoundSIMs []interface{} `json:"bound_sims"`
			History   []interface{} `json:"history"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
	if len(resp.Data.Lists) != 0 {
		t.Errorf("lists len = %d, want 0", len(resp.Data.Lists))
	}
	if resp.Data.BoundSIMs == nil {
		t.Errorf("bound_sims should be empty array, not null")
	}
	if resp.Data.History == nil {
		t.Errorf("history should be empty array, not null")
	}
}

// TestIMEIPoolHandler_Lookup_PopulatesBoundSIMs verifies that a SIM with a
// matching bound_imei appears in the bound_sims array of the Lookup response.
func TestIMEIPoolHandler_Lookup_PopulatesBoundSIMs(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)

	const testIMEI = "445566778899001"

	var simID string
	err := pg.QueryRow(context.Background(), `
		INSERT INTO sims (tenant_id, iccid, msisdn, imsi, status, bound_imei, binding_mode, binding_status)
		VALUES ($1, $2, $3, $4, 'active', $5, 'strict', 'verified')
		RETURNING id::text
	`, fix.tenantID, "8988211234567890123", "905551234567", "286011234567890", testIMEI).Scan(&simID)
	if err != nil {
		t.Fatalf("seed sim: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pg.Exec(context.Background(), `DELETE FROM sims WHERE id = $1::uuid`, simID)
	})

	simStore := store.NewSIMStore(pg)
	historyStore := store.NewIMEIHistoryStore(pg, simStore)
	h := NewHandler(store.NewIMEIPoolStore(pg), simStore, historyStore, nil, nil, nil, zerolog.Nop())

	req := makeLookupRequest(testIMEI, fix.tenantID)
	w := httptest.NewRecorder()
	h.Lookup(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Lookup status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		Data   struct {
			BoundSIMs []struct {
				SIMID         string  `json:"sim_id"`
				ICCID         string  `json:"iccid"`
				BindingMode   *string `json:"binding_mode"`
				BindingStatus *string `json:"binding_status"`
			} `json:"bound_sims"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.BoundSIMs) == 0 {
		t.Fatalf("bound_sims is empty, want at least 1 row")
	}
	found := resp.Data.BoundSIMs[0]
	if found.SIMID != simID {
		t.Errorf("bound_sims[0].sim_id = %q, want %q", found.SIMID, simID)
	}
	if found.ICCID != "8988211234567890123" {
		t.Errorf("bound_sims[0].iccid = %q, want 8988211234567890123", found.ICCID)
	}
	if found.BindingMode == nil || *found.BindingMode != "strict" {
		t.Errorf("bound_sims[0].binding_mode = %v, want strict", found.BindingMode)
	}
	if found.BindingStatus == nil || *found.BindingStatus != "verified" {
		t.Errorf("bound_sims[0].binding_status = %v, want verified", found.BindingStatus)
	}
}

// TestIMEIPoolHandler_Lookup_PopulatesHistory verifies that imei_history rows
// for the observed IMEI appear in the history array of the Lookup response.
func TestIMEIPoolHandler_Lookup_PopulatesHistory(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)

	const testIMEI = "556677889900112"

	var simID string
	err := pg.QueryRow(context.Background(), `
		INSERT INTO sims (tenant_id, iccid, msisdn, imsi, status)
		VALUES ($1, $2, $3, $4, 'active')
		RETURNING id::text
	`, fix.tenantID, "8988219876543210987", "905559876543", "286019876543210").Scan(&simID)
	if err != nil {
		t.Fatalf("seed sim: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pg.Exec(context.Background(), `DELETE FROM imei_history WHERE sim_id = $1::uuid`, simID)
		_, _ = pg.Exec(context.Background(), `DELETE FROM sims WHERE id = $1::uuid`, simID)
	})

	_, err = pg.Exec(context.Background(), `
		INSERT INTO imei_history (tenant_id, sim_id, observed_imei, capture_protocol, was_mismatch, alarm_raised)
		VALUES ($1, $2::uuid, $3, 'radius', true, true)
	`, fix.tenantID, simID, testIMEI)
	if err != nil {
		t.Fatalf("seed imei_history: %v", err)
	}

	simStore := store.NewSIMStore(pg)
	historyStore := store.NewIMEIHistoryStore(pg, simStore)
	h := NewHandler(store.NewIMEIPoolStore(pg), simStore, historyStore, nil, nil, nil, zerolog.Nop())

	req := makeLookupRequest(testIMEI, fix.tenantID)
	w := httptest.NewRecorder()
	h.Lookup(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Lookup status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		Data   struct {
			History []struct {
				SIMID           string `json:"sim_id"`
				ICCID           string `json:"iccid"`
				CaptureProtocol string `json:"capture_protocol"`
				WasMismatch     bool   `json:"was_mismatch"`
				AlarmRaised     bool   `json:"alarm_raised"`
			} `json:"history"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.History) == 0 {
		t.Fatalf("history is empty, want at least 1 row")
	}
	h0 := resp.Data.History[0]
	if h0.CaptureProtocol != "radius" {
		t.Errorf("history[0].capture_protocol = %q, want radius", h0.CaptureProtocol)
	}
	if !h0.WasMismatch {
		t.Errorf("history[0].was_mismatch = false, want true")
	}
	if !h0.AlarmRaised {
		t.Errorf("history[0].alarm_raised = false, want true")
	}
}

// TestIMEIPoolHandler_Lookup_BothEmpty_StillReturns200 is the AC-6 regression:
// when no SIMs are bound to the queried IMEI and no history rows exist,
// the response must still be 200 with empty arrays (not null) for both fields.
func TestIMEIPoolHandler_Lookup_BothEmpty_StillReturns200(t *testing.T) {
	pg := poolTestPool(t)
	fix := seedPoolFixture(t, pg)
	simStore := store.NewSIMStore(pg)
	historyStore := store.NewIMEIHistoryStore(pg, simStore)
	h := NewHandler(store.NewIMEIPoolStore(pg), simStore, historyStore, nil, nil, nil, zerolog.Nop())

	req := makeLookupRequest("111222333444556", fix.tenantID)
	w := httptest.NewRecorder()
	h.Lookup(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Lookup(both empty) status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		Data   struct {
			BoundSIMs []interface{} `json:"bound_sims"`
			History   []interface{} `json:"history"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
	if resp.Data.BoundSIMs == nil {
		t.Errorf("bound_sims must be empty array [], not null")
	}
	if len(resp.Data.BoundSIMs) != 0 {
		t.Errorf("bound_sims len = %d, want 0", len(resp.Data.BoundSIMs))
	}
	if resp.Data.History == nil {
		t.Errorf("history must be empty array [], not null")
	}
	if len(resp.Data.History) != 0 {
		t.Errorf("history len = %d, want 0", len(resp.Data.History))
	}
}
