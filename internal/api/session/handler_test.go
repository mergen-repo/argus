package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sessModel "github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newTestHandler(t *testing.T) (*Handler, *sessModel.Manager) {
	t.Helper()
	logger := zerolog.Nop()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	mgr := sessModel.NewManager(nil, rdb, logger)
	h := NewHandler(mgr, nil, nil, nil, nil, logger)
	return h, mgr
}

func TestHandler_List_Empty(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp apierr.ListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
}

func TestHandler_List_WithSessions(t *testing.T) {
	h, mgr := newTestHandler(t)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		sess := &sessModel.Session{
			ID:            "list-sess-" + string(rune('a'+i)),
			SimID:         "sim-" + string(rune('a'+i)),
			TenantID:      "tenant-001",
			OperatorID:    "op-001",
			IMSI:          "28601010000000" + string(rune('1'+i)),
			AcctSessionID: "acct-" + string(rune('a'+i)),
			NASIP:         "10.0.0.1",
			StartedAt:     time.Now().UTC(),
		}
		if err := mgr.Create(ctx, sess); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=10", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandler_Stats(t *testing.T) {
	h, mgr := newTestHandler(t)

	ctx := context.Background()
	sess := &sessModel.Session{
		ID:         "stats-sess-001",
		SimID:      "sim-stats-001",
		TenantID:   "tenant-001",
		OperatorID: "op-stats",
		APNID:      "apn-stats",
		RATType:    "lte",
		IMSI:       "286010500000001",
		NASIP:      "10.0.0.1",
		BytesIn:    5000,
		BytesOut:   10000,
		StartedAt:  time.Now().UTC().Add(-30 * time.Minute),
	}
	if err := mgr.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/stats", nil)
	w := httptest.NewRecorder()

	h.Stats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
}

func TestHandler_Disconnect_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	r := chi.NewRouter()
	r.Post("/api/v1/sessions/{id}/disconnect", h.Disconnect)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/nonexistent-id/disconnect",
		strings.NewReader(`{"reason":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandler_Disconnect_Success(t *testing.T) {
	h, mgr := newTestHandler(t)

	ctx := context.Background()
	sess := &sessModel.Session{
		ID:            "disc-sess-001",
		SimID:         "sim-disc-001",
		TenantID:      "tenant-001",
		OperatorID:    "op-001",
		IMSI:          "286010600000001",
		AcctSessionID: "acct-disc-001",
		NASIP:         "10.0.0.1",
		SessionState:  "active",
		StartedAt:     time.Now().UTC(),
	}
	if err := mgr.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	r := chi.NewRouter()
	r.Post("/api/v1/sessions/{id}/disconnect", h.Disconnect)

	body := `{"reason":"testing disconnect"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/disc-sess-001/disconnect",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Status string             `json:"status"`
		Data   disconnectResponse `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.State != "terminated" {
		t.Errorf("state = %q, want terminated", resp.Data.State)
	}
}

func TestHandler_BulkDisconnect_MissingReason(t *testing.T) {
	h, _ := newTestHandler(t)

	body := `{"sim_ids":["sim-1"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/bulk/disconnect",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.BulkDisconnect(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

// STORY-075: Get — single session fetch
func TestHandler_Get_NotFound(t *testing.T) {
	h, _ := newTestHandler(t)

	r := chi.NewRouter()
	r.Get("/api/v1/sessions/{id}", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandler_Get_Success(t *testing.T) {
	h, mgr := newTestHandler(t)

	ctx := context.Background()
	sess := &sessModel.Session{
		ID:            "get-sess-001",
		SimID:         "sim-get-001",
		TenantID:      "tenant-001",
		OperatorID:    "op-001",
		IMSI:          "286010700000001",
		AcctSessionID: "acct-get-001",
		NASIP:         "10.0.0.1",
		SessionState:  "active",
		BytesIn:       1000,
		BytesOut:      2000,
		StartedAt:     time.Now().UTC(),
	}
	if err := mgr.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/api/v1/sessions/{id}", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/get-sess-001", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var resp struct {
		Status string           `json:"status"`
		Data   sessionDetailDTO `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ID != "get-sess-001" {
		t.Errorf("id = %q, want get-sess-001", resp.Data.ID)
	}
	if resp.Data.State != "active" {
		t.Errorf("state = %q, want active", resp.Data.State)
	}
}

func TestHandler_Get_MissingID(t *testing.T) {
	h, _ := newTestHandler(t)

	// chi strips trailing slash; manually register without {id}
	r := chi.NewRouter()
	r.Get("/api/v1/sessions/{id}", h.Get)

	// Test via direct invocation with empty URL param
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Chi returns 404 for no match, 400 from handler would need URL param
	if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest && w.Code != http.StatusMovedPermanently {
		t.Errorf("expected 404/400/301 for empty session id, got %d", w.Code)
	}
}

func TestHandler_BulkDisconnect_MissingSimIDs(t *testing.T) {
	h, _ := newTestHandler(t)

	body := `{"reason":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/bulk/disconnect",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.BulkDisconnect(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}
