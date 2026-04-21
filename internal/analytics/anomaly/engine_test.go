package anomaly

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockAnomalyStore struct {
	mu        sync.Mutex
	anomalies []mockAnomaly
}

type mockAnomaly struct {
	id       uuid.UUID
	tenantID uuid.UUID
	simID    *uuid.UUID
	typ      string
	severity string
}

func (m *mockAnomalyStore) Create(ctx context.Context, p CreateParams) (*AnomalyRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.New()
	m.anomalies = append(m.anomalies, mockAnomaly{
		id:       id,
		tenantID: p.TenantID,
		simID:    p.SimID,
		typ:      p.Type,
		severity: p.Severity,
	})
	return &AnomalyRecord{
		ID:         id,
		TenantID:   p.TenantID,
		SimID:      p.SimID,
		Type:       p.Type,
		Severity:   p.Severity,
		State:      StateOpen,
		DetectedAt: p.DetectedAt,
	}, nil
}

func (m *mockAnomalyStore) HasRecentAnomaly(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ string, _ time.Duration) (bool, error) {
	return false, nil
}

func (m *mockAnomalyStore) FindDataSpikeCandidates(_ context.Context, multiplier float64) ([]DataSpikeRow, error) {
	return []DataSpikeRow{
		{
			SimID:      uuid.New(),
			TenantID:   uuid.New(),
			TodayBytes: 1000000000,
			AvgBytes:   100000000,
		},
	}, nil
}

func (m *mockAnomalyStore) GetSimICCID(_ context.Context, _ uuid.UUID) (string, error) {
	return "8901234567890123456", nil
}

type mockPublisher struct {
	mu     sync.Mutex
	events []publishedEvent
}

type publishedEvent struct {
	subject string
	payload interface{}
}

func (p *mockPublisher) Publish(_ context.Context, subject string, payload interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, publishedEvent{subject: subject, payload: payload})
	return nil
}

type mockSuspender struct {
	mu        sync.Mutex
	suspended []uuid.UUID
}

func (s *mockSuspender) Suspend(_ context.Context, _ uuid.UUID, simID uuid.UUID, _ *uuid.UUID, _ *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suspended = append(s.suspended, simID)
	return nil
}

func TestBatchDetector_RunDataSpikeDetection(t *testing.T) {
	store := &mockAnomalyStore{}
	pub := &mockPublisher{}
	susp := &mockSuspender{}

	bd := NewBatchDetector(
		store,
		pub,
		susp,
		DefaultThresholds(),
		"alert.triggered",
		"anomaly.detected",
		zerolog.Nop(),
	)

	detected, err := bd.RunDataSpikeDetection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if detected != 1 {
		t.Errorf("expected 1 data spike detected, got %d", detected)
	}

	store.mu.Lock()
	if len(store.anomalies) != 1 {
		t.Errorf("expected 1 anomaly created, got %d", len(store.anomalies))
	} else {
		if store.anomalies[0].typ != TypeDataSpike {
			t.Errorf("expected type %q, got %q", TypeDataSpike, store.anomalies[0].typ)
		}
		if store.anomalies[0].severity != SeverityHigh {
			t.Errorf("expected severity %q, got %q", SeverityHigh, store.anomalies[0].severity)
		}
	}
	store.mu.Unlock()

	pub.mu.Lock()
	if len(pub.events) != 2 {
		t.Errorf("expected 2 events published (anomaly + alert), got %d", len(pub.events))
	}
	pub.mu.Unlock()
}

func TestAnomalyTitle(t *testing.T) {
	tests := []struct {
		typ   string
		iccid string
	}{
		{TypeSIMCloning, "89012345"},
		{TypeDataSpike, "89012345"},
		{TypeAuthFlood, "89012345"},
		{TypeNASFlood, ""},
	}

	for _, tt := range tests {
		title := anomalyTitle(tt.typ, tt.iccid)
		if title == "" {
			t.Errorf("anomalyTitle(%q, %q) returned empty string", tt.typ, tt.iccid)
		}
	}
}

func TestAnomalyDescription(t *testing.T) {
	for _, typ := range []string{TypeSIMCloning, TypeDataSpike, TypeAuthFlood, TypeNASFlood, "unknown"} {
		desc := anomalyDescription(typ, nil)
		if desc == "" {
			t.Errorf("anomalyDescription(%q) returned empty string", typ)
		}
	}
}

func TestDefaultThresholds(t *testing.T) {
	th := DefaultThresholds()

	if th.CloningWindowSec != 300 {
		t.Errorf("expected CloningWindowSec 300, got %d", th.CloningWindowSec)
	}
	if th.DataSpikeMultiplier != 3.0 {
		t.Errorf("expected DataSpikeMultiplier 3.0, got %f", th.DataSpikeMultiplier)
	}
	if th.AuthFloodMax != 100 {
		t.Errorf("expected AuthFloodMax 100, got %d", th.AuthFloodMax)
	}
	if th.NASFloodMax != 1000 {
		t.Errorf("expected NASFloodMax 1000, got %d", th.NASFloodMax)
	}
	if !th.AutoSuspendOnCloning {
		t.Error("expected AutoSuspendOnCloning true")
	}
	if !th.FilterBulkJobs {
		t.Error("expected FilterBulkJobs true")
	}
}

// TestAnomalyEngine_Publish_IncludesSimIDAndAnomalyIDForLinkage verifies that
// the alert payload published to SubjectAlertTriggered carries:
//   - top-level sim_id (populates alerts.sim_id via notification subscriber)
//   - metadata.anomaly_id (links back to anomaly rows without a dedicated FK)
//   - metadata.sim_id (defensive — meta is the durable JSONB snapshot)
//
// FIX-209: unified alerts ↔ anomalies linkage.
func TestAnomalyEngine_Publish_IncludesSimIDAndAnomalyIDForLinkage(t *testing.T) {
	pub := &mockPublisher{}
	susp := &mockSuspender{}
	anomalyStore := &mockAnomalyStore{}

	bd := NewBatchDetector(
		anomalyStore,
		pub,
		susp,
		DefaultThresholds(),
		"argus.events.alert.triggered",
		"argus.events.anomaly.detected",
		zerolog.Nop(),
	)

	detected, err := bd.RunDataSpikeDetection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if detected != 1 {
		t.Fatalf("detected = %d, want 1", detected)
	}

	// FIX-212: alert payload is now *bus.Envelope. sim_id lives in
	// envelope.entity (primary) and meta.sim_id (duplicated for the
	// persist layer lookup). anomaly_id lives in meta.
	pub.mu.Lock()
	defer pub.mu.Unlock()
	var alertEnv *bus.Envelope
	for _, evt := range pub.events {
		if evt.subject == "argus.events.alert.triggered" {
			e, ok := evt.payload.(*bus.Envelope)
			if !ok {
				t.Fatalf("alert payload is not *bus.Envelope: %T", evt.payload)
			}
			alertEnv = e
			break
		}
	}
	if alertEnv == nil {
		t.Fatal("no alert event captured")
	}

	if alertEnv.Entity == nil || alertEnv.Entity.Type != "sim" {
		t.Fatalf("entity = %+v, want type=sim", alertEnv.Entity)
	}
	simIDStr := alertEnv.Entity.ID
	if _, err := uuid.Parse(simIDStr); err != nil {
		t.Errorf("entity.id not a valid UUID: %v", err)
	}

	anomalyIDStr, _ := alertEnv.Meta["anomaly_id"].(string)
	if anomalyIDStr == "" {
		t.Fatal("meta.anomaly_id missing")
	}
	anomalyStore.mu.Lock()
	if len(anomalyStore.anomalies) != 1 {
		t.Fatalf("stored anomalies = %d, want 1", len(anomalyStore.anomalies))
	}
	wantAnomalyID := anomalyStore.anomalies[0].id.String()
	anomalyStore.mu.Unlock()
	if anomalyIDStr != wantAnomalyID {
		t.Errorf("meta.anomaly_id = %s, want %s", anomalyIDStr, wantAnomalyID)
	}

	metaSimIDStr, _ := alertEnv.Meta["sim_id"].(string)
	if metaSimIDStr != simIDStr {
		t.Errorf("meta.sim_id = %s, want %s (match entity.id)", metaSimIDStr, simIDStr)
	}
}

func TestAuthEventJSON(t *testing.T) {
	evt := AuthEvent{
		IMSI:      "001010000000001",
		SimID:     uuid.New(),
		TenantID:  uuid.New(),
		NASIP:     "10.0.0.1",
		Source:    "radius",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}

	var decoded AuthEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.IMSI != evt.IMSI {
		t.Errorf("IMSI mismatch: %q != %q", decoded.IMSI, evt.IMSI)
	}
	if decoded.NASIP != evt.NASIP {
		t.Errorf("NASIP mismatch: %q != %q", decoded.NASIP, evt.NASIP)
	}
}
