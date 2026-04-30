package job

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// fakeSMSSender implements SMSSender.
type fakeSMSSender struct {
	sid string
	err error
}

func (f *fakeSMSSender) SendSMSWithResult(_ context.Context, _, _ string) (string, error) {
	return f.sid, f.err
}

// fakeSMSJobStore implements smsOutboundStoreJob.
type fakeSMSJobStore struct {
	row    *store.SMSOutbound
	getErr error

	updatedID     uuid.UUID
	updatedStatus string
	updatedMsgID  string
	updatedErr    string
	updateErr     error
}

func (f *fakeSMSJobStore) GetByID(_ context.Context, id uuid.UUID) (*store.SMSOutbound, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.row != nil && f.row.ID == id {
		return f.row, nil
	}
	return nil, store.ErrSMSOutboundNotFound
}

func (f *fakeSMSJobStore) UpdateStatus(_ context.Context, id uuid.UUID, status, providerMsgID, errorCode string, _ *time.Time) error {
	f.updatedID = id
	f.updatedStatus = status
	f.updatedMsgID = providerMsgID
	f.updatedErr = errorCode
	return f.updateErr
}

// fakeSMSEventBus implements smsEventBus.
type fakeSMSEventBus struct {
	subjects []string
}

func (f *fakeSMSEventBus) Publish(_ context.Context, subject string, _ interface{}) error {
	f.subjects = append(f.subjects, subject)
	return nil
}

func newTestSMSJob(smsID uuid.UUID) *store.Job {
	payload, _ := json.Marshal(smsOutboundPayload{SMSID: smsID.String()})
	return &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Type:     JobTypeSMSOutboundSend,
		Payload:  payload,
	}
}

func newRedisForTest(t *testing.T) *redis.Client {
	t.Helper()
	rc := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 13})
	if rc.Ping(context.Background()).Err() != nil {
		t.Skip("Redis not available for SMS processor tests")
	}
	return rc
}

func TestSMSGatewayProcessor_QueuedToSent(t *testing.T) {
	rc := newRedisForTest(t)
	defer rc.FlushDB(context.Background())

	smsID := uuid.New()
	msisdn := "+905551234567"

	smsRow := &store.SMSOutbound{
		ID:     smsID,
		MSISDN: msisdn,
		Status: "queued",
	}

	textKey := "sms_text:" + smsID.String()
	rc.Set(context.Background(), textKey, "Hello World", time.Hour)

	st := &fakeSMSJobStore{row: smsRow}
	sender := &fakeSMSSender{sid: "SMabc123"}
	eb := &fakeSMSEventBus{}
	p := NewSMSGatewayProcessor(st, sender, rc, eb, zerolog.Nop())

	job := newTestSMSJob(smsID)
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if st.updatedStatus != "sent" {
		t.Errorf("status = %q, want sent", st.updatedStatus)
	}
	if st.updatedMsgID != "SMabc123" {
		t.Errorf("provider_message_id = %q, want SMabc123", st.updatedMsgID)
	}

	// Redis key should be deleted after GetDel
	val := rc.Get(context.Background(), textKey).Val()
	if val != "" {
		t.Error("expected redis key to be deleted after send")
	}
}

func TestSMSGatewayProcessor_NotQueued_Skips(t *testing.T) {
	rc := newRedisForTest(t)
	defer rc.FlushDB(context.Background())

	smsID := uuid.New()
	smsRow := &store.SMSOutbound{
		ID:     smsID,
		Status: "sent",
	}

	st := &fakeSMSJobStore{row: smsRow}
	sender := &fakeSMSSender{sid: "SMskipped"}
	p := NewSMSGatewayProcessor(st, sender, rc, nil, zerolog.Nop())

	job := newTestSMSJob(smsID)
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if st.updatedStatus != "" {
		t.Error("UpdateStatus should not be called when status != queued")
	}
}

func TestSMSGatewayProcessor_SendFails_StatusFailed(t *testing.T) {
	rc := newRedisForTest(t)
	defer rc.FlushDB(context.Background())

	smsID := uuid.New()
	msisdn := "+905551111"
	smsRow := &store.SMSOutbound{
		ID:     smsID,
		MSISDN: msisdn,
		Status: "queued",
	}

	textKey := "sms_text:" + smsID.String()
	rc.Set(context.Background(), textKey, "Hello", time.Hour)

	st := &fakeSMSJobStore{row: smsRow}
	sender := &fakeSMSSender{err: errors.New("twilio error")}
	eb := &fakeSMSEventBus{}
	p := NewSMSGatewayProcessor(st, sender, rc, eb, zerolog.Nop())

	job := newTestSMSJob(smsID)
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if st.updatedStatus != "failed" {
		t.Errorf("status = %q, want failed", st.updatedStatus)
	}

	if len(eb.subjects) == 0 {
		t.Error("expected delivery_failed notification to be emitted")
	}
}

func TestSMSGatewayProcessor_TextExpired_StatusFailed(t *testing.T) {
	rc := newRedisForTest(t)
	defer rc.FlushDB(context.Background())

	smsID := uuid.New()
	smsRow := &store.SMSOutbound{
		ID:     smsID,
		MSISDN: "+90555",
		Status: "queued",
	}

	st := &fakeSMSJobStore{row: smsRow}
	sender := &fakeSMSSender{sid: "SM_never"}
	eb := &fakeSMSEventBus{}
	p := NewSMSGatewayProcessor(st, sender, rc, eb, zerolog.Nop())

	job := newTestSMSJob(smsID)
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if st.updatedStatus != "failed" {
		t.Errorf("status = %q, want failed (text expired)", st.updatedStatus)
	}
	if st.updatedErr != "TEXT_EXPIRED" {
		t.Errorf("error_code = %q, want TEXT_EXPIRED", st.updatedErr)
	}

	if len(eb.subjects) == 0 {
		t.Error("expected notification event emitted")
	}
}

func TestSMSGatewayProcessor_Type(t *testing.T) {
	p := NewSMSGatewayProcessor(nil, nil, nil, nil, zerolog.Nop())
	if p.Type() != JobTypeSMSOutboundSend {
		t.Errorf("type = %q, want %q", p.Type(), JobTypeSMSOutboundSend)
	}
}
