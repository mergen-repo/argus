package job

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestMapColumns(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		wantErr bool
	}{
		{
			name:    "valid headers",
			headers: []string{"iccid", "imsi", "msisdn", "operator_code", "apn_name"},
			wantErr: false,
		},
		{
			name:    "valid headers with mixed case",
			headers: []string{"ICCID", "IMSI", "MSISDN", "Operator_Code", "APN_Name"},
			wantErr: false,
		},
		{
			name:    "valid headers with extra whitespace",
			headers: []string{" iccid ", " imsi ", " msisdn ", " operator_code ", " apn_name "},
			wantErr: false,
		},
		{
			name:    "valid headers with extra columns",
			headers: []string{"iccid", "imsi", "msisdn", "operator_code", "apn_name", "extra"},
			wantErr: false,
		},
		{
			name:    "missing iccid",
			headers: []string{"imsi", "msisdn", "operator_code", "apn_name"},
			wantErr: true,
		},
		{
			name:    "missing multiple",
			headers: []string{"iccid"},
			wantErr: true,
		},
		{
			name:    "empty headers",
			headers: []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			colMap, err := mapColumns(tt.headers)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			for _, req := range requiredHeaders {
				if _, ok := colMap[req]; !ok {
					t.Errorf("missing column mapping for %s", req)
				}
			}
		})
	}
}

func TestValidateRow(t *testing.T) {
	tests := []struct {
		name         string
		iccid        string
		imsi         string
		operatorCode string
		apnName      string
		wantError    bool
	}{
		{
			name:         "valid row",
			iccid:        "8990111234567890123",
			imsi:         "286010123456789",
			operatorCode: "turkcell",
			apnName:      "iot.fleet",
			wantError:    false,
		},
		{
			name:         "empty iccid",
			iccid:        "",
			imsi:         "286010123456789",
			operatorCode: "turkcell",
			apnName:      "iot.fleet",
			wantError:    true,
		},
		{
			name:         "iccid too short",
			iccid:        "89901112345",
			imsi:         "286010123456789",
			operatorCode: "turkcell",
			apnName:      "iot.fleet",
			wantError:    true,
		},
		{
			name:         "iccid too long",
			iccid:        "89901112345678901234567",
			imsi:         "286010123456789",
			operatorCode: "turkcell",
			apnName:      "iot.fleet",
			wantError:    true,
		},
		{
			name:         "imsi wrong length",
			iccid:        "8990111234567890123",
			imsi:         "28601012345",
			operatorCode: "turkcell",
			apnName:      "iot.fleet",
			wantError:    true,
		},
		{
			name:         "empty operator_code",
			iccid:        "8990111234567890123",
			imsi:         "286010123456789",
			operatorCode: "",
			apnName:      "iot.fleet",
			wantError:    true,
		},
		{
			name:         "empty apn_name",
			iccid:        "8990111234567890123",
			imsi:         "286010123456789",
			operatorCode: "turkcell",
			apnName:      "",
			wantError:    true,
		},
		{
			name:         "all empty",
			iccid:        "",
			imsi:         "",
			operatorCode: "",
			apnName:      "",
			wantError:    true,
		},
		{
			name:         "22 digit iccid valid",
			iccid:        "8990111234567890123456",
			imsi:         "286010123456789",
			operatorCode: "turkcell",
			apnName:      "iot.fleet",
			wantError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateRow(tt.iccid, tt.imsi, tt.operatorCode, tt.apnName)
			if tt.wantError && result == "" {
				t.Error("expected validation error, got empty string")
			}
			if !tt.wantError && result != "" {
				t.Errorf("unexpected validation error: %s", result)
			}
		})
	}
}

func TestMapColumns_ReorderedHeaders(t *testing.T) {
	headers := []string{"apn_name", "operator_code", "msisdn", "imsi", "iccid"}
	colMap, err := mapColumns(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if colMap["iccid"] != 4 {
		t.Errorf("iccid index = %d, want 4", colMap["iccid"])
	}
	if colMap["imsi"] != 3 {
		t.Errorf("imsi index = %d, want 3", colMap["imsi"])
	}
	if colMap["msisdn"] != 2 {
		t.Errorf("msisdn index = %d, want 2", colMap["msisdn"])
	}
	if colMap["operator_code"] != 1 {
		t.Errorf("operator_code index = %d, want 1", colMap["operator_code"])
	}
	if colMap["apn_name"] != 0 {
		t.Errorf("apn_name index = %d, want 0", colMap["apn_name"])
	}
}

func TestValidateRow_BoundaryICCID(t *testing.T) {
	result19 := validateRow("8990111234567890123", "286010123456789", "tc", "apn")
	if result19 != "" {
		t.Errorf("19-char ICCID should be valid, got: %s", result19)
	}

	result22 := validateRow("8990111234567890123456", "286010123456789", "tc", "apn")
	if result22 != "" {
		t.Errorf("22-char ICCID should be valid, got: %s", result22)
	}

	result18 := validateRow("899011123456789012", "286010123456789", "tc", "apn")
	if result18 == "" {
		t.Error("18-char ICCID should be invalid")
	}

	result23 := validateRow("89901112345678901234567", "286010123456789", "tc", "apn")
	if result23 == "" {
		t.Error("23-char ICCID should be invalid")
	}
}

func TestImportResultSerialization(t *testing.T) {
	result := ImportResult{
		TotalRows:     100,
		SuccessCount:  95,
		FailureCount:  5,
		CreatedSIMIDs: []string{"uuid-1", "uuid-2"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ImportResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.TotalRows != 100 {
		t.Errorf("TotalRows = %d, want 100", decoded.TotalRows)
	}
	if decoded.SuccessCount != 95 {
		t.Errorf("SuccessCount = %d, want 95", decoded.SuccessCount)
	}
	if decoded.FailureCount != 5 {
		t.Errorf("FailureCount = %d, want 5", decoded.FailureCount)
	}
	if len(decoded.CreatedSIMIDs) != 2 {
		t.Errorf("CreatedSIMIDs count = %d, want 2", len(decoded.CreatedSIMIDs))
	}
}

func TestImportRowErrorSerialization(t *testing.T) {
	rowError := ImportRowError{
		Row:          5,
		ICCID:        "8990111234567890123",
		ErrorMessage: "ICCID already exists",
	}

	data, err := json.Marshal(rowError)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ImportRowError
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Row != 5 {
		t.Errorf("Row = %d, want 5", decoded.Row)
	}
	if decoded.ICCID != "8990111234567890123" {
		t.Errorf("ICCID = %q, want %q", decoded.ICCID, "8990111234567890123")
	}
	if decoded.ErrorMessage != "ICCID already exists" {
		t.Errorf("ErrorMessage = %q, want %q", decoded.ErrorMessage, "ICCID already exists")
	}
}

func TestImportPayloadSerialization(t *testing.T) {
	payload := ImportPayload{
		CSVData:  "iccid,imsi,msisdn,operator_code,apn_name\n123,456,789,tc,apn1\n",
		FileName: "test.csv",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ImportPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.FileName != "test.csv" {
		t.Errorf("FileName = %q, want %q", decoded.FileName, "test.csv")
	}
	if decoded.CSVData == "" {
		t.Error("CSVData should not be empty")
	}
}

type mockAuditor struct {
	mu      sync.Mutex
	entries []audit.CreateEntryParams
}

func (m *mockAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, p)
	return &audit.Entry{}, nil
}

func (m *mockAuditor) getEntries() []audit.CreateEntryParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]audit.CreateEntryParams, len(m.entries))
	copy(cp, m.entries)
	return cp
}

func TestEmitAudit_NilAuditor(t *testing.T) {
	p := &BulkImportProcessor{}
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
	cid := job.ID
	p.emitAudit(context.Background(), job, &cid, "sim.create", "sim", uuid.New().String(), nil, map[string]string{"iccid": "123"})
}

func TestEmitAudit_RecordsEntry(t *testing.T) {
	ma := &mockAuditor{}
	p := &BulkImportProcessor{auditor: ma}
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
	cid := job.ID
	simID := uuid.New().String()
	afterData := map[string]string{"iccid": "8990111234567890123"}

	p.emitAudit(context.Background(), job, &cid, "sim.create", "sim", simID, nil, afterData)

	entries := ma.getEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Action != "sim.create" {
		t.Errorf("Action = %q, want %q", e.Action, "sim.create")
	}
	if e.EntityType != "sim" {
		t.Errorf("EntityType = %q, want %q", e.EntityType, "sim")
	}
	if e.EntityID != simID {
		t.Errorf("EntityID = %q, want %q", e.EntityID, simID)
	}
	if e.TenantID != job.TenantID {
		t.Errorf("TenantID = %v, want %v", e.TenantID, job.TenantID)
	}
	if e.CorrelationID == nil || *e.CorrelationID != cid {
		t.Errorf("CorrelationID = %v, want %v", e.CorrelationID, cid)
	}
	if len(e.BeforeData) != 0 {
		t.Errorf("BeforeData should be nil, got %s", string(e.BeforeData))
	}
	if len(e.AfterData) == 0 {
		t.Error("AfterData should not be empty")
	}
}

func TestEmitAudit_WithBeforeAndAfter(t *testing.T) {
	ma := &mockAuditor{}
	p := &BulkImportProcessor{auditor: ma}
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
	cid := job.ID
	before := map[string]string{"state": "ordered"}
	after := map[string]string{"state": "active"}

	p.emitAudit(context.Background(), job, &cid, "sim.activate", "sim", uuid.New().String(), before, after)

	entries := ma.getEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Action != "sim.activate" {
		t.Errorf("Action = %q, want %q", e.Action, "sim.activate")
	}
	if len(e.BeforeData) == 0 {
		t.Error("BeforeData should not be empty for activate")
	}
	if len(e.AfterData) == 0 {
		t.Error("AfterData should not be empty for activate")
	}
}

func TestEmitSummaryAudit_EmitsJobEntity(t *testing.T) {
	ma := &mockAuditor{}
	p := &BulkImportProcessor{auditor: ma}
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
	cid := job.ID

	p.emitSummaryAudit(context.Background(), job, &cid, "test.csv", 100, 95, 5)

	entries := ma.getEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 summary audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Action != "sim.bulk_import" {
		t.Errorf("Action = %q, want %q", e.Action, "sim.bulk_import")
	}
	if e.EntityType != "job" {
		t.Errorf("EntityType = %q, want %q", e.EntityType, "job")
	}
	if e.EntityID != job.ID.String() {
		t.Errorf("EntityID = %q, want %q", e.EntityID, job.ID.String())
	}

	var summary map[string]interface{}
	if err := json.Unmarshal(e.AfterData, &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if v, ok := summary["total"]; !ok || v != float64(100) {
		t.Errorf("summary total = %v, want 100", v)
	}
	if v, ok := summary["success"]; !ok || v != float64(95) {
		t.Errorf("summary success = %v, want 95", v)
	}
	if v, ok := summary["failure"]; !ok || v != float64(5) {
		t.Errorf("summary failure = %v, want 5", v)
	}
	if v, ok := summary["file_name"]; !ok || v != "test.csv" {
		t.Errorf("summary file_name = %v, want test.csv", v)
	}
}

func TestEmitSummaryAudit_NilAuditor(t *testing.T) {
	p := &BulkImportProcessor{}
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
	cid := job.ID
	p.emitSummaryAudit(context.Background(), job, &cid, "test.csv", 10, 10, 0)
}

func TestEmitNotification_NilNotifier(t *testing.T) {
	p := &BulkImportProcessor{}
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
	p.emitNotification(context.Background(), job, "test.csv", 10, 10, 0)
}

func TestEmitAudit_MultipleSimsCreateCorrectCorrelation(t *testing.T) {
	ma := &mockAuditor{}
	p := &BulkImportProcessor{auditor: ma}
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
	cid := job.ID

	for i := 0; i < 10; i++ {
		simID := uuid.New().String()
		p.emitAudit(context.Background(), job, &cid, "sim.create", "sim", simID, nil, map[string]string{"idx": "create"})
		p.emitAudit(context.Background(), job, &cid, "sim.activate", "sim", simID, map[string]string{"state": "ordered"}, map[string]string{"state": "active"})
	}
	p.emitSummaryAudit(context.Background(), job, &cid, "batch.csv", 10, 10, 0)

	entries := ma.getEntries()
	expectedCount := 10*2 + 1
	if len(entries) != expectedCount {
		t.Fatalf("expected %d audit entries (10 create + 10 activate + 1 summary), got %d", expectedCount, len(entries))
	}

	createCount := 0
	activateCount := 0
	summaryCount := 0
	for _, e := range entries {
		if e.CorrelationID == nil || *e.CorrelationID != cid {
			t.Errorf("entry %q has wrong correlationID %v, want %v", e.Action, e.CorrelationID, cid)
		}
		switch e.Action {
		case "sim.create":
			createCount++
		case "sim.activate":
			activateCount++
		case "sim.bulk_import":
			summaryCount++
		}
	}
	if createCount != 10 {
		t.Errorf("sim.create count = %d, want 10", createCount)
	}
	if activateCount != 10 {
		t.Errorf("sim.activate count = %d, want 10", activateCount)
	}
	if summaryCount != 1 {
		t.Errorf("sim.bulk_import count = %d, want 1", summaryCount)
	}
}

func TestResolveAndAssignPolicy_NilPolicies(t *testing.T) {
	p := &BulkImportProcessor{}
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	cid := job.ID
	apnID := uuid.New()
	sim := &store.SIM{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		APNID:    &apnID,
	}
	apn := &store.APN{
		ID:   apnID,
		Name: "test.apn",
	}
	p.resolveAndAssignPolicy(context.Background(), job, &cid, sim, apn)
}

func TestResolveAndAssignPolicy_NilAPNID(t *testing.T) {
	p := &BulkImportProcessor{policies: &stubPolicyReader{}}
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	cid := job.ID
	sim := &store.SIM{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}
	apn := &store.APN{
		ID:   uuid.New(),
		Name: "test.apn",
	}
	p.resolveAndAssignPolicy(context.Background(), job, &cid, sim, apn)
}

func TestSetNotifier(t *testing.T) {
	p := &BulkImportProcessor{}
	if p.notifier != nil {
		t.Error("notifier should be nil initially")
	}
	p.SetNotifier(nil)
	if p.notifier != nil {
		t.Error("notifier should still be nil after SetNotifier(nil)")
	}
}

type stubPolicyReader struct {
	mu       sync.Mutex
	policies []store.Policy
	err      error
	calls    int
}

func (s *stubPolicyReader) ListReferencingAPN(_ context.Context, _ uuid.UUID, _ string, _ int, _ string) ([]store.Policy, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.policies, "", s.err
}

type stubSIMWriter struct {
	mu              sync.Mutex
	created         []store.CreateSIMParams
	transitioned    []uuid.UUID
	setIPPolicyCalls []setIPPolicyCall
	createErr       error
	transitionErr   error
	setPolicyErr    error
	returnPVID      *uuid.UUID
}

type setIPPolicyCall struct {
	SimID           uuid.UUID
	IPAddressID     *uuid.UUID
	PolicyVersionID *uuid.UUID
}

func (s *stubSIMWriter) Create(_ context.Context, _ uuid.UUID, p store.CreateSIMParams) (*store.SIM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return nil, s.createErr
	}
	simID := uuid.New()
	apnID := p.APNID
	s.created = append(s.created, p)
	return &store.SIM{
		ID:              simID,
		TenantID:        uuid.Nil,
		OperatorID:      p.OperatorID,
		APNID:           &apnID,
		ICCID:           p.ICCID,
		IMSI:            p.IMSI,
		MSISDN:          p.MSISDN,
		SimType:         p.SimType,
		State:           "ordered",
		PolicyVersionID: s.returnPVID,
	}, nil
}

func (s *stubSIMWriter) TransitionState(_ context.Context, simID uuid.UUID, _ string, _ *uuid.UUID, _ string, _ interface{}, _ int) (*store.SIM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.transitionErr != nil {
		return nil, s.transitionErr
	}
	s.transitioned = append(s.transitioned, simID)
	apnID := uuid.New()
	return &store.SIM{
		ID:              simID,
		TenantID:        uuid.Nil,
		APNID:           &apnID,
		State:           "active",
		PolicyVersionID: s.returnPVID,
	}, nil
}

func (s *stubSIMWriter) SetIPAndPolicy(_ context.Context, simID uuid.UUID, ipID *uuid.UUID, pvID *uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setIPPolicyCalls = append(s.setIPPolicyCalls, setIPPolicyCall{simID, ipID, pvID})
	return s.setPolicyErr
}

func (s *stubSIMWriter) getCreated() []store.CreateSIMParams {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]store.CreateSIMParams, len(s.created))
	copy(cp, s.created)
	return cp
}

func (s *stubSIMWriter) getSetIPPolicyCalls() []setIPPolicyCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]setIPPolicyCall, len(s.setIPPolicyCalls))
	copy(cp, s.setIPPolicyCalls)
	return cp
}

type stubJobStore struct {
	mu           sync.Mutex
	completed    bool
	progressCalls int
	completedResult json.RawMessage
}

func (s *stubJobStore) UpdateProgress(_ context.Context, _ uuid.UUID, _, _, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progressCalls++
	return nil
}

func (s *stubJobStore) CheckCancelled(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *stubJobStore) Complete(_ context.Context, _ uuid.UUID, _ json.RawMessage, result json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed = true
	s.completedResult = result
	return nil
}

type stubOperatorReader struct {
	op  *store.Operator
	err error
}

func (s *stubOperatorReader) GetByCode(_ context.Context, _ string) (*store.Operator, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.op, nil
}

type stubAPNReader struct {
	apn *store.APN
	err error
}

func (s *stubAPNReader) GetByName(_ context.Context, _, _ uuid.UUID, _ string) (*store.APN, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.apn, nil
}

type stubIPPoolManager struct{}

func (s *stubIPPoolManager) List(_ context.Context, _ uuid.UUID, _ string, _ int, _ *uuid.UUID) ([]store.IPPool, string, error) {
	return nil, "", nil
}

func (s *stubIPPoolManager) ReserveStaticIP(_ context.Context, _, _ uuid.UUID, _ *string) (*store.IPAddress, error) {
	return nil, fmt.Errorf("not implemented")
}

type stubEventPublisher struct {
	mu     sync.Mutex
	events []string
}

func (s *stubEventPublisher) Publish(_ context.Context, subject string, _ interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, subject)
	return nil
}

type stubNotifier struct {
	mu    sync.Mutex
	calls []notification.NotifyRequest
}

func (s *stubNotifier) Notify(_ context.Context, req notification.NotifyRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	return nil
}

func (s *stubNotifier) getCalls() []notification.NotifyRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]notification.NotifyRequest, len(s.calls))
	copy(cp, s.calls)
	return cp
}

func makeCSV(rows int) string {
	csv := "iccid,imsi,msisdn,operator_code,apn_name\n"
	for i := 0; i < rows; i++ {
		iccid := fmt.Sprintf("899011123456789%04d", i)
		imsi := fmt.Sprintf("28601012345%04d", i)
		csv += fmt.Sprintf("%s,%s,+905001234%04d,turkcell,iot.fleet\n", iccid, imsi, i)
	}
	return csv
}

func makeInvalidCSV(rows int) string {
	csv := "iccid,imsi,msisdn,operator_code,apn_name\n"
	for i := 0; i < rows; i++ {
		csv += fmt.Sprintf("short%d,bad%d,,tc,apn\n", i, i)
	}
	return csv
}

func buildProcessor(sims *stubSIMWriter, policies *stubPolicyReader, aud audit.Auditor, notif *stubNotifier) *BulkImportProcessor {
	opID := uuid.New()
	apnID := uuid.New()

	var pol importPolicyReader
	if policies != nil {
		pol = policies
	}
	var n importNotifier
	if notif != nil {
		n = notif
	}

	return &BulkImportProcessor{
		jobs:      &stubJobStore{},
		sims:      sims,
		operators: &stubOperatorReader{op: &store.Operator{ID: opID, Code: "turkcell"}},
		apns:      &stubAPNReader{apn: &store.APN{ID: apnID, Name: "iot.fleet"}},
		ipPools:   &stubIPPoolManager{},
		eventBus:  &stubEventPublisher{},
		auditor:   aud,
		notifier:  n,
		policies:  pol,
		logger:    zerolog.New(io.Discard),
	}
}

func makeJob(csv string) *store.Job {
	payload, _ := json.Marshal(ImportPayload{
		CSVData:  csv,
		FileName: "test.csv",
	})
	userID := uuid.New()
	return &store.Job{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Type:      JobTypeBulkImport,
		State:     "running",
		Payload:   payload,
		CreatedBy: &userID,
	}
}

func TestProcess_AuditCount_NoPolicyMatch(t *testing.T) {
	n := 3
	sims := &stubSIMWriter{}
	aud := &mockAuditor{}
	notif := &stubNotifier{}
	proc := buildProcessor(sims, &stubPolicyReader{}, aud, notif)

	job := makeJob(makeCSV(n))
	if err := proc.Process(context.Background(), job); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	entries := aud.getEntries()
	expectedAuditCount := 2*n + 1
	if len(entries) != expectedAuditCount {
		t.Fatalf("audit entries = %d, want %d (N create + N activate + 1 summary)", len(entries), expectedAuditCount)
	}

	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Action]++
	}
	if counts["sim.create"] != n {
		t.Errorf("sim.create count = %d, want %d", counts["sim.create"], n)
	}
	if counts["sim.activate"] != n {
		t.Errorf("sim.activate count = %d, want %d", counts["sim.activate"], n)
	}
	if counts["sim.bulk_import"] != 1 {
		t.Errorf("sim.bulk_import count = %d, want 1", counts["sim.bulk_import"])
	}

	created := sims.getCreated()
	if len(created) != n {
		t.Errorf("SIMs created = %d, want %d", len(created), n)
	}
}

func TestProcess_PolicyAssignment_UsesCurrentVersionID(t *testing.T) {
	n := 3
	sims := &stubSIMWriter{}
	aud := &mockAuditor{}
	notif := &stubNotifier{}

	policyID := uuid.New()
	versionID := uuid.New()
	polReader := &stubPolicyReader{
		policies: []store.Policy{
			{
				ID:               policyID,
				State:            "active",
				CurrentVersionID: &versionID,
			},
		},
	}
	proc := buildProcessor(sims, polReader, aud, notif)

	job := makeJob(makeCSV(n))
	if err := proc.Process(context.Background(), job); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	calls := sims.getSetIPPolicyCalls()
	if len(calls) != n {
		t.Fatalf("SetIPAndPolicy calls = %d, want %d", len(calls), n)
	}
	for i, c := range calls {
		if c.PolicyVersionID == nil || *c.PolicyVersionID != versionID {
			t.Errorf("call[%d] PolicyVersionID = %v, want %v", i, c.PolicyVersionID, versionID)
		}
	}

	entries := aud.getEntries()
	policyAutoCount := 0
	for _, e := range entries {
		if e.Action == "sim.policy_auto_assigned" {
			policyAutoCount++
		}
	}
	if policyAutoCount != n {
		t.Errorf("sim.policy_auto_assigned audit count = %d, want %d", policyAutoCount, n)
	}

	expectedTotal := 2*n + n + 1
	if len(entries) != expectedTotal {
		t.Errorf("total audit entries = %d, want %d (create+activate+policy_assign+summary)", len(entries), expectedTotal)
	}
}

func TestProcess_PolicyVersionIDGuard_SkipsAssignment(t *testing.T) {
	existingPVID := uuid.New()
	sims := &stubSIMWriter{returnPVID: &existingPVID}
	aud := &mockAuditor{}
	notif := &stubNotifier{}

	policyID := uuid.New()
	versionID := uuid.New()
	polReader := &stubPolicyReader{
		policies: []store.Policy{
			{
				ID:               policyID,
				State:            "active",
				CurrentVersionID: &versionID,
			},
		},
	}
	proc := buildProcessor(sims, polReader, aud, notif)

	job := makeJob(makeCSV(2))
	if err := proc.Process(context.Background(), job); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	calls := sims.getSetIPPolicyCalls()
	if len(calls) != 0 {
		t.Errorf("SetIPAndPolicy should not be called when PolicyVersionID already set, got %d calls", len(calls))
	}

	entries := aud.getEntries()
	for _, e := range entries {
		if e.Action == "sim.policy_auto_assigned" {
			t.Error("sim.policy_auto_assigned should not be emitted when PolicyVersionID already set")
		}
	}
}

func TestProcess_NoInsertHistory_OnlyTransitionState(t *testing.T) {
	n := 3
	sims := &stubSIMWriter{}
	aud := &mockAuditor{}
	notif := &stubNotifier{}
	proc := buildProcessor(sims, &stubPolicyReader{}, aud, notif)

	job := makeJob(makeCSV(n))
	if err := proc.Process(context.Background(), job); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	sims.mu.Lock()
	transitioned := len(sims.transitioned)
	sims.mu.Unlock()
	if transitioned != n {
		t.Errorf("TransitionState calls = %d, want %d", transitioned, n)
	}
}

func TestProcess_Notification_CalledOnCompletion(t *testing.T) {
	sims := &stubSIMWriter{}
	aud := &mockAuditor{}
	notif := &stubNotifier{}
	proc := buildProcessor(sims, &stubPolicyReader{}, aud, notif)

	job := makeJob(makeCSV(2))
	if err := proc.Process(context.Background(), job); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	calls := notif.getCalls()
	if len(calls) != 1 {
		t.Fatalf("notification calls = %d, want 1", len(calls))
	}
	if calls[0].EventType != notification.EventJobCompleted {
		t.Errorf("notification event type = %q, want %q", calls[0].EventType, notification.EventJobCompleted)
	}
	if calls[0].Severity != "info" {
		t.Errorf("notification severity = %q, want %q", calls[0].Severity, "info")
	}
}

func TestProcess_AllInvalidRows_ZeroSIMsAndNotification(t *testing.T) {
	sims := &stubSIMWriter{}
	aud := &mockAuditor{}
	notif := &stubNotifier{}
	proc := buildProcessor(sims, &stubPolicyReader{}, aud, notif)

	job := makeJob(makeInvalidCSV(3))
	if err := proc.Process(context.Background(), job); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	created := sims.getCreated()
	if len(created) != 0 {
		t.Errorf("SIMs created = %d, want 0 for all-invalid CSV", len(created))
	}

	calls := notif.getCalls()
	if len(calls) != 1 {
		t.Fatalf("notification calls = %d, want 1 even for all-invalid", len(calls))
	}
	if calls[0].Severity != "error" {
		t.Errorf("notification severity = %q, want %q for all-invalid", calls[0].Severity, "error")
	}

	entries := aud.getEntries()
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1 (summary only) for all-invalid", len(entries))
	}
	if entries[0].Action != "sim.bulk_import" {
		t.Errorf("audit action = %q, want %q", entries[0].Action, "sim.bulk_import")
	}
}

func TestProcess_EmptyCSV_NotificationAndSummaryAudit(t *testing.T) {
	sims := &stubSIMWriter{}
	aud := &mockAuditor{}
	notif := &stubNotifier{}
	proc := buildProcessor(sims, &stubPolicyReader{}, aud, notif)

	job := makeJob("iccid,imsi,msisdn,operator_code,apn_name\n")
	if err := proc.Process(context.Background(), job); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	calls := notif.getCalls()
	if len(calls) != 1 {
		t.Fatalf("notification calls = %d, want 1 for empty CSV", len(calls))
	}

	entries := aud.getEntries()
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1 (summary only) for empty CSV", len(entries))
	}

	jobStorePtr := proc.jobs.(*stubJobStore)
	jobStorePtr.mu.Lock()
	completed := jobStorePtr.completed
	jobStorePtr.mu.Unlock()
	if !completed {
		t.Error("job should be completed for empty CSV")
	}
}

func TestProcess_MixedValidInvalid_CorrectCounts(t *testing.T) {
	sims := &stubSIMWriter{}
	aud := &mockAuditor{}
	notif := &stubNotifier{}
	proc := buildProcessor(sims, &stubPolicyReader{}, aud, notif)

	csv := "iccid,imsi,msisdn,operator_code,apn_name\n"
	csv += "8990111234567890123,286010123456789,+905001234567,turkcell,iot.fleet\n"
	csv += "short,bad,,turkcell,iot.fleet\n"
	csv += "8990111234567890124,286010123456780,+905001234568,turkcell,iot.fleet\n"

	job := makeJob(csv)
	if err := proc.Process(context.Background(), job); err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	created := sims.getCreated()
	if len(created) != 2 {
		t.Errorf("SIMs created = %d, want 2", len(created))
	}

	calls := notif.getCalls()
	if len(calls) != 1 {
		t.Fatalf("notification calls = %d, want 1", len(calls))
	}
	if calls[0].Severity != "warning" {
		t.Errorf("notification severity = %q, want %q for mixed valid/invalid", calls[0].Severity, "warning")
	}

	entries := aud.getEntries()
	expectedAudit := 2*2 + 1
	if len(entries) != expectedAudit {
		t.Errorf("audit entries = %d, want %d (2 create + 2 activate + 1 summary)", len(entries), expectedAudit)
	}

	jobStorePtr := proc.jobs.(*stubJobStore)
	jobStorePtr.mu.Lock()
	resultJSON := jobStorePtr.completedResult
	jobStorePtr.mu.Unlock()
	var result ImportResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.TotalRows != 3 {
		t.Errorf("TotalRows = %d, want 3", result.TotalRows)
	}
	if result.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", result.SuccessCount)
	}
	if result.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", result.FailureCount)
	}
}
