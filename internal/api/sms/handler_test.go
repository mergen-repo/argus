package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// fakeSimStore implements simStore for tests.
type fakeSimStore struct {
	sim *store.SIM
	err error
}

func (f *fakeSimStore) GetByID(_ context.Context, _, _ uuid.UUID) (*store.SIM, error) {
	return f.sim, f.err
}

// fakeSMSOutboundStore implements smsOutboundStore for tests.
type fakeSMSOutboundStore struct {
	inserted *store.SMSOutbound
	rows     []*store.SMSOutbound
	err      error
}

func (f *fakeSMSOutboundStore) Insert(_ context.Context, m *store.SMSOutbound) (*store.SMSOutbound, error) {
	if f.err != nil {
		return nil, f.err
	}
	m.ID = uuid.New()
	m.QueuedAt = time.Now().UTC()
	f.inserted = m
	return m, nil
}

func (f *fakeSMSOutboundStore) List(_ context.Context, _ uuid.UUID, _ store.SMSListFilters, _ string, _ int) ([]*store.SMSOutbound, string, error) {
	return f.rows, "", f.err
}

// fakeJobStore implements jobStore.
type fakeJobStore struct {
	job *store.Job
	err error
}

func (f *fakeJobStore) CreateWithTenantID(_ context.Context, tenantID uuid.UUID, p store.CreateJobParams) (*store.Job, error) {
	if f.err != nil {
		return nil, f.err
	}
	j := &store.Job{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     p.Type,
	}
	f.job = j
	return j, nil
}

// fakeEventBus implements eventBus.
type fakeEventBus struct {
	subjects []string
}

func (f *fakeEventBus) Publish(_ context.Context, subject string, _ interface{}) error {
	f.subjects = append(f.subjects, subject)
	return nil
}

func newTestHandler(t *testing.T, simSt simStore, smsSt smsOutboundStore, jobSt jobStore, eb eventBus, redis *redis.Client) *Handler {
	t.Helper()
	return NewHandler(simSt, smsSt, jobSt, eb, redis, nil, 5, zerolog.Nop())
}

func withTenant(r *http.Request, tenantID uuid.UUID) *http.Request {
	ctx := context.WithValue(r.Context(), apierr.TenantIDKey, tenantID)
	return r.WithContext(ctx)
}

func withUser(r *http.Request, userID uuid.UUID) *http.Request {
	ctx := context.WithValue(r.Context(), apierr.UserIDKey, userID)
	return r.WithContext(ctx)
}

func makeRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	mr := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15,
	})
	return mr
}

func newMiniredisHandler(t *testing.T) (*Handler, *redis.Client, *fakeSMSOutboundStore, *fakeJobStore, *fakeEventBus) {
	t.Helper()
	msisdn := "+905551234567"
	sim := &store.SIM{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		MSISDN:   &msisdn,
	}
	simSt := &fakeSimStore{sim: sim}
	smsSt := &fakeSMSOutboundStore{}
	jobSt := &fakeJobStore{}
	eb := &fakeEventBus{}

	rc := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
		DB:   15,
	})

	h := NewHandler(simSt, smsSt, jobSt, eb, rc, nil, 100, zerolog.Nop())
	return h, rc, smsSt, jobSt, eb
}

func TestSMSSend_ValidBody_Returns202(t *testing.T) {
	h, rc, smsSt, jobSt, eb := newMiniredisHandler(t)
	if rc != nil {
		rc.FlushDB(context.Background())
	}

	tenantID := uuid.New()
	body := `{"sim_id":"` + uuid.New().String() + `","text":"Hello SIM","priority":"normal"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sms/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTenant(req, tenantID)
	req = withUser(req, uuid.New())

	w := httptest.NewRecorder()
	h.Send(w, req)

	if rc.Ping(context.Background()).Err() != nil {
		t.Skip("Redis not available")
	}

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202; body = %s", w.Code, w.Body.String())
		return
	}

	if smsSt.inserted == nil {
		t.Error("expected sms_outbound row to be inserted")
	}

	if jobSt.job == nil {
		t.Error("expected job to be created")
	}

	if len(eb.subjects) == 0 {
		t.Error("expected event bus publish")
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	data, _ := resp["data"].(map[string]interface{})
	if data["message_id"] == "" {
		t.Error("expected message_id in response")
	}
}

func TestSMSSend_TextTooLong_Returns422(t *testing.T) {
	msisdn := "+90555"
	sim := &store.SIM{ID: uuid.New(), TenantID: uuid.New(), MSISDN: &msisdn}
	h := NewHandler(&fakeSimStore{sim: sim}, &fakeSMSOutboundStore{}, &fakeJobStore{}, &fakeEventBus{}, nil, nil, 60, zerolog.Nop())

	longText := strings.Repeat("x", 481)
	body := `{"sim_id":"` + uuid.New().String() + `","text":"` + longText + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sms/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTenant(req, uuid.New())

	w := httptest.NewRecorder()
	h.Send(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
}

func TestSMSSend_EmptyText_Returns422(t *testing.T) {
	msisdn := "+90555"
	sim := &store.SIM{ID: uuid.New(), TenantID: uuid.New(), MSISDN: &msisdn}
	h := NewHandler(&fakeSimStore{sim: sim}, &fakeSMSOutboundStore{}, &fakeJobStore{}, &fakeEventBus{}, nil, nil, 60, zerolog.Nop())

	body := `{"sim_id":"` + uuid.New().String() + `","text":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sms/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTenant(req, uuid.New())

	w := httptest.NewRecorder()
	h.Send(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
}

func TestSMSSend_InvalidSimID_Returns422(t *testing.T) {
	h := NewHandler(&fakeSimStore{err: store.ErrSIMNotFound}, &fakeSMSOutboundStore{}, &fakeJobStore{}, &fakeEventBus{}, nil, nil, 60, zerolog.Nop())

	body := `{"sim_id":"not-a-uuid","text":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sms/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTenant(req, uuid.New())

	w := httptest.NewRecorder()
	h.Send(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
}

func TestSMSSend_SimNotFound_Returns422(t *testing.T) {
	h := NewHandler(&fakeSimStore{err: store.ErrSIMNotFound}, &fakeSMSOutboundStore{}, &fakeJobStore{}, &fakeEventBus{}, nil, nil, 60, zerolog.Nop())

	body := `{"sim_id":"` + uuid.New().String() + `","text":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sms/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTenant(req, uuid.New())

	w := httptest.NewRecorder()
	h.Send(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
}

func TestSMSSend_RateLimitExhausted_Returns429(t *testing.T) {
	rc := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 14})
	if rc.Ping(context.Background()).Err() != nil {
		t.Skip("Redis not available")
	}
	rc.FlushDB(context.Background())

	msisdn := "+90555"
	sim := &store.SIM{ID: uuid.New(), TenantID: uuid.New(), MSISDN: &msisdn}
	tenantID := uuid.New()

	h := NewHandler(&fakeSimStore{sim: sim}, &fakeSMSOutboundStore{}, &fakeJobStore{}, &fakeEventBus{}, rc, nil, 2, zerolog.Nop())

	sendReq := func() int {
		body := `{"sim_id":"` + uuid.New().String() + `","text":"hi"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sms/send", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withTenant(req, tenantID)
		w := httptest.NewRecorder()
		h.Send(w, req)
		return w.Code
	}

	for i := 0; i < 2; i++ {
		code := sendReq()
		if code == http.StatusTooManyRequests {
			t.Fatalf("rate limited too early at attempt %d", i+1)
		}
	}

	code := sendReq()
	if code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after rate limit exceeded, got %d", code)
	}
}

func TestSMSHistory_ReturnsList(t *testing.T) {
	rows := []*store.SMSOutbound{
		{
			ID:          uuid.New(),
			SimID:       uuid.New(),
			MSISDN:      "+90555",
			TextHash:    "abc123",
			TextPreview: "Hello",
			Status:      "sent",
			QueuedAt:    time.Now(),
		},
	}
	smsSt := &fakeSMSOutboundStore{rows: rows}
	h := NewHandler(&fakeSimStore{}, smsSt, &fakeJobStore{}, &fakeEventBus{}, nil, nil, 60, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sms/history", bytes.NewReader(nil))
	req = withTenant(req, uuid.New())

	w := httptest.NewRecorder()
	h.History(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %s", w.Code, w.Body.String())
		return
	}

	var resp struct {
		Data []smsOutboundDTO `json:"data"`
		Meta apierr.ListMeta  `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Errorf("data length = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].Status != "sent" {
		t.Errorf("status = %q, want sent", resp.Data[0].Status)
	}
}

func TestSMSHistory_StatusFilter(t *testing.T) {
	smsSt := &fakeSMSOutboundStore{rows: []*store.SMSOutbound{}}
	h := NewHandler(&fakeSimStore{}, smsSt, &fakeJobStore{}, &fakeEventBus{}, nil, nil, 60, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sms/history?status=failed&limit=10", bytes.NewReader(nil))
	req = withTenant(req, uuid.New())

	w := httptest.NewRecorder()
	h.History(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// Ensure bus.SubjectJobQueue is accessible (import verification).
var _ = bus.SubjectJobQueue
