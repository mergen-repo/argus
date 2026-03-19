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
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   14,
	})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	client.FlushDB(ctx)
	t.Cleanup(func() {
		client.FlushDB(ctx)
		client.Close()
	})
	return client
}

func newTestHandler(t *testing.T) (*Handler, *sessModel.Manager) {
	t.Helper()
	rc := newTestRedis(t)
	logger := zerolog.Nop()
	mgr := sessModel.NewManager(rc, logger)
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
