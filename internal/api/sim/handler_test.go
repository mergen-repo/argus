package sim

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func TestToSIMResponse(t *testing.T) {
	now := time.Now()
	apnID := uuid.New()
	ipAddrID := uuid.New()
	policyID := uuid.New()
	esimID := uuid.New()
	msisdn := "+905551234567"
	ratType := "lte"

	s := &store.SIM{
		ID:                    uuid.New(),
		TenantID:              uuid.New(),
		OperatorID:            uuid.New(),
		APNID:                 &apnID,
		ICCID:                 "8990123456789012345",
		IMSI:                  "286010123456789",
		MSISDN:                &msisdn,
		IPAddressID:           &ipAddrID,
		PolicyVersionID:       &policyID,
		ESimProfileID:         &esimID,
		SimType:               "physical",
		State:                 "active",
		RATType:               &ratType,
		MaxConcurrentSessions: 1,
		SessionIdleTimeoutSec: 3600,
		SessionHardTimeoutSec: 86400,
		Metadata:              json.RawMessage(`{"group":"fleet-a"}`),
		ActivatedAt:           &now,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	resp := toSIMResponse(s)

	if resp.ID != s.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, s.ID.String())
	}
	if resp.TenantID != s.TenantID.String() {
		t.Errorf("TenantID = %q, want %q", resp.TenantID, s.TenantID.String())
	}
	if resp.OperatorID != s.OperatorID.String() {
		t.Errorf("OperatorID = %q, want %q", resp.OperatorID, s.OperatorID.String())
	}
	if resp.ICCID != "8990123456789012345" {
		t.Errorf("ICCID = %q, want %q", resp.ICCID, "8990123456789012345")
	}
	if resp.IMSI != "286010123456789" {
		t.Errorf("IMSI = %q, want %q", resp.IMSI, "286010123456789")
	}
	if resp.MSISDN == nil || *resp.MSISDN != "+905551234567" {
		t.Error("MSISDN should be '+905551234567'")
	}
	if resp.SimType != "physical" {
		t.Errorf("SimType = %q, want %q", resp.SimType, "physical")
	}
	if resp.State != "active" {
		t.Errorf("State = %q, want %q", resp.State, "active")
	}
	if resp.RATType == nil || *resp.RATType != "lte" {
		t.Error("RATType should be 'lte'")
	}
	if resp.APNID == nil || *resp.APNID != apnID.String() {
		t.Error("APNID should match")
	}
	if resp.IPAddressID == nil || *resp.IPAddressID != ipAddrID.String() {
		t.Error("IPAddressID should match")
	}
	if resp.PolicyVersionID == nil || *resp.PolicyVersionID != policyID.String() {
		t.Error("PolicyVersionID should match")
	}
	if resp.ESimProfileID == nil || *resp.ESimProfileID != esimID.String() {
		t.Error("ESimProfileID should match")
	}
	if resp.ActivatedAt == nil {
		t.Error("ActivatedAt should not be nil")
	}
	if resp.MaxConcurrentSessions != 1 {
		t.Errorf("MaxConcurrentSessions = %d, want 1", resp.MaxConcurrentSessions)
	}
}

func TestToSIMResponseNilFields(t *testing.T) {
	now := time.Now()
	s := &store.SIM{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		OperatorID: uuid.New(),
		ICCID:     "8990123456789012345",
		IMSI:      "286010123456789",
		SimType:   "esim",
		State:     "ordered",
		Metadata:  json.RawMessage(`{}`),
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp := toSIMResponse(s)

	if resp.MSISDN != nil {
		t.Error("MSISDN should be nil when not set")
	}
	if resp.APNID != nil {
		t.Error("APNID should be nil when not set")
	}
	if resp.IPAddressID != nil {
		t.Error("IPAddressID should be nil when not set")
	}
	if resp.PolicyVersionID != nil {
		t.Error("PolicyVersionID should be nil when not set")
	}
	if resp.ESimProfileID != nil {
		t.Error("ESimProfileID should be nil when not set")
	}
	if resp.RATType != nil {
		t.Error("RATType should be nil when not set")
	}
	if resp.ActivatedAt != nil {
		t.Error("ActivatedAt should be nil when not set")
	}
	if resp.SuspendedAt != nil {
		t.Error("SuspendedAt should be nil when not set")
	}
	if resp.TerminatedAt != nil {
		t.Error("TerminatedAt should be nil when not set")
	}
	if resp.PurgeAt != nil {
		t.Error("PurgeAt should be nil when not set")
	}
}

func TestToHistoryResponse(t *testing.T) {
	now := time.Now()
	userID := uuid.New()
	jobID := uuid.New()
	fromState := "ordered"
	reason := "activated by admin"

	h := &store.SimStateHistory{
		ID:          42,
		SimID:       uuid.New(),
		FromState:   &fromState,
		ToState:     "active",
		Reason:      &reason,
		TriggeredBy: "user",
		UserID:      &userID,
		JobID:       &jobID,
		CreatedAt:   now,
	}

	resp := toHistoryResponse(h)

	if resp.ID != 42 {
		t.Errorf("ID = %d, want 42", resp.ID)
	}
	if resp.SimID != h.SimID.String() {
		t.Errorf("SimID = %q, want %q", resp.SimID, h.SimID.String())
	}
	if resp.FromState == nil || *resp.FromState != "ordered" {
		t.Error("FromState should be 'ordered'")
	}
	if resp.ToState != "active" {
		t.Errorf("ToState = %q, want %q", resp.ToState, "active")
	}
	if resp.Reason == nil || *resp.Reason != "activated by admin" {
		t.Error("Reason should be 'activated by admin'")
	}
	if resp.TriggeredBy != "user" {
		t.Errorf("TriggeredBy = %q, want %q", resp.TriggeredBy, "user")
	}
	if resp.UserID == nil || *resp.UserID != userID.String() {
		t.Error("UserID should match")
	}
	if resp.JobID == nil || *resp.JobID != jobID.String() {
		t.Error("JobID should match")
	}
}

func TestValidSIMTypes(t *testing.T) {
	valid := []string{"physical", "esim"}
	for _, st := range valid {
		if !validSIMTypes[st] {
			t.Errorf("SIM type %q should be valid", st)
		}
	}

	invalid := []string{"virtual", "embedded", "", "PHYSICAL"}
	for _, st := range invalid {
		if validSIMTypes[st] {
			t.Errorf("SIM type %q should be invalid", st)
		}
	}
}

func TestValidRATTypes(t *testing.T) {
	valid := []string{"nb_iot", "lte_m", "lte", "nr_5g"}
	for _, rt := range valid {
		if !validRATTypes[rt] {
			t.Errorf("RAT type %q should be valid", rt)
		}
	}

	invalid := []string{"3g", "2g", "5g", "", "NB_IOT"}
	for _, rt := range invalid {
		if validRATTypes[rt] {
			t.Errorf("RAT type %q should be invalid", rt)
		}
	}
}

func TestToHistoryResponseNilFields(t *testing.T) {
	now := time.Now()
	h := &store.SimStateHistory{
		ID:          1,
		SimID:       uuid.New(),
		ToState:     "ordered",
		TriggeredBy: "system",
		CreatedAt:   now,
	}

	resp := toHistoryResponse(h)

	if resp.FromState != nil {
		t.Error("FromState should be nil for initial state")
	}
	if resp.Reason != nil {
		t.Error("Reason should be nil when not set")
	}
	if resp.UserID != nil {
		t.Error("UserID should be nil when not set")
	}
	if resp.JobID != nil {
		t.Error("JobID should be nil when not set")
	}
}

func TestCreateSIMValidation(t *testing.T) {
	tests := []struct {
		name     string
		request  createSIMRequest
		wantErrs int
	}{
		{
			name:     "missing_iccid",
			request:  createSIMRequest{IMSI: "286010123456789", OperatorID: "550e8400-e29b-41d4-a716-446655440000", APNID: "550e8400-e29b-41d4-a716-446655440001", SimType: "physical"},
			wantErrs: 1,
		},
		{
			name:     "missing_imsi",
			request:  createSIMRequest{ICCID: "8990123456789012345", OperatorID: "550e8400-e29b-41d4-a716-446655440000", APNID: "550e8400-e29b-41d4-a716-446655440001", SimType: "physical"},
			wantErrs: 1,
		},
		{
			name:     "missing_operator_id",
			request:  createSIMRequest{ICCID: "8990123456789012345", IMSI: "286010123456789", APNID: "550e8400-e29b-41d4-a716-446655440001", SimType: "physical"},
			wantErrs: 1,
		},
		{
			name:     "missing_apn_id",
			request:  createSIMRequest{ICCID: "8990123456789012345", IMSI: "286010123456789", OperatorID: "550e8400-e29b-41d4-a716-446655440000", SimType: "physical"},
			wantErrs: 1,
		},
		{
			name:     "missing_sim_type",
			request:  createSIMRequest{ICCID: "8990123456789012345", IMSI: "286010123456789", OperatorID: "550e8400-e29b-41d4-a716-446655440000", APNID: "550e8400-e29b-41d4-a716-446655440001"},
			wantErrs: 1,
		},
		{
			name:     "invalid_sim_type",
			request:  createSIMRequest{ICCID: "8990123456789012345", IMSI: "286010123456789", OperatorID: "550e8400-e29b-41d4-a716-446655440000", APNID: "550e8400-e29b-41d4-a716-446655440001", SimType: "virtual"},
			wantErrs: 1,
		},
		{
			name:     "iccid_too_long",
			request:  createSIMRequest{ICCID: "89901234567890123456789", IMSI: "286010123456789", OperatorID: "550e8400-e29b-41d4-a716-446655440000", APNID: "550e8400-e29b-41d4-a716-446655440001", SimType: "physical"},
			wantErrs: 1,
		},
		{
			name:     "imsi_too_long",
			request:  createSIMRequest{ICCID: "8990123456789012345", IMSI: "2860101234567890", OperatorID: "550e8400-e29b-41d4-a716-446655440000", APNID: "550e8400-e29b-41d4-a716-446655440001", SimType: "physical"},
			wantErrs: 1,
		},
		{
			name:     "multiple_missing_fields",
			request:  createSIMRequest{},
			wantErrs: 5,
		},
		{
			name: "invalid_rat_type",
			request: func() createSIMRequest {
				rt := "3g"
				return createSIMRequest{ICCID: "8990123456789012345", IMSI: "286010123456789", OperatorID: "550e8400-e29b-41d4-a716-446655440000", APNID: "550e8400-e29b-41d4-a716-446655440001", SimType: "physical", RATType: &rt}
			}(),
			wantErrs: 1,
		},
		{
			name:     "valid_request",
			request:  createSIMRequest{ICCID: "8990123456789012345", IMSI: "286010123456789", OperatorID: "550e8400-e29b-41d4-a716-446655440000", APNID: "550e8400-e29b-41d4-a716-446655440001", SimType: "physical"},
			wantErrs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var validationErrors []map[string]string

			if tt.request.ICCID == "" {
				validationErrors = append(validationErrors, map[string]string{"field": "iccid", "message": "ICCID is required", "code": "required"})
			} else if len(tt.request.ICCID) > 22 {
				validationErrors = append(validationErrors, map[string]string{"field": "iccid", "message": "ICCID must be at most 22 characters", "code": "max_length"})
			}
			if tt.request.IMSI == "" {
				validationErrors = append(validationErrors, map[string]string{"field": "imsi", "message": "IMSI is required", "code": "required"})
			} else if len(tt.request.IMSI) > 15 {
				validationErrors = append(validationErrors, map[string]string{"field": "imsi", "message": "IMSI must be at most 15 characters", "code": "max_length"})
			}
			if tt.request.OperatorID == "" {
				validationErrors = append(validationErrors, map[string]string{"field": "operator_id", "message": "Operator ID is required", "code": "required"})
			}
			if tt.request.APNID == "" {
				validationErrors = append(validationErrors, map[string]string{"field": "apn_id", "message": "APN ID is required", "code": "required"})
			}
			if tt.request.SimType == "" {
				validationErrors = append(validationErrors, map[string]string{"field": "sim_type", "message": "SIM type is required", "code": "required"})
			} else if !validSIMTypes[tt.request.SimType] {
				validationErrors = append(validationErrors, map[string]string{"field": "sim_type", "message": "Invalid SIM type", "code": "invalid_enum"})
			}
			if tt.request.RATType != nil && *tt.request.RATType != "" && !validRATTypes[*tt.request.RATType] {
				validationErrors = append(validationErrors, map[string]string{"field": "rat_type", "message": "Invalid RAT type", "code": "invalid_enum"})
			}

			if len(validationErrors) != tt.wantErrs {
				t.Errorf("validation errors = %d, want %d (errors: %v)", len(validationErrors), tt.wantErrs, validationErrors)
			}
		})
	}
}

func TestListSIMsQueryParsing(t *testing.T) {
	tests := []struct {
		name        string
		queryString string
		wantLimit   int
		wantState   string
		wantQ       string
		wantCursor  string
	}{
		{"defaults", "", 50, "", "", ""},
		{"custom_limit", "limit=25", 25, "", "", ""},
		{"limit_too_high", "limit=200", 50, "", "", ""},
		{"limit_zero", "limit=0", 50, "", "", ""},
		{"limit_negative", "limit=-1", 50, "", "", ""},
		{"state_filter", "state=active", 50, "active", "", ""},
		{"search_query", "q=899012", 50, "", "899012", ""},
		{"cursor", "cursor=550e8400-e29b-41d4-a716-446655440000", 50, "", "", "550e8400-e29b-41d4-a716-446655440000"},
		{"combined", "limit=10&state=suspended&q=286&cursor=abc", 10, "suspended", "286", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit := 50
			if tt.queryString != "" {
				params := make(map[string]string)
				for _, p := range splitQueryParams(tt.queryString) {
					parts := splitKeyValue(p)
					if len(parts) == 2 {
						params[parts[0]] = parts[1]
					}
				}
				if v, ok := params["limit"]; ok {
					if n, err := parseInt(v); err == nil && n > 0 && n <= 100 {
						limit = n
					}
				}
				state := params["state"]
				q := params["q"]
				cursor := params["cursor"]

				if limit != tt.wantLimit {
					t.Errorf("limit = %d, want %d", limit, tt.wantLimit)
				}
				if state != tt.wantState {
					t.Errorf("state = %q, want %q", state, tt.wantState)
				}
				if q != tt.wantQ {
					t.Errorf("q = %q, want %q", q, tt.wantQ)
				}
				if cursor != tt.wantCursor {
					t.Errorf("cursor = %q, want %q", cursor, tt.wantCursor)
				}
			} else {
				if limit != tt.wantLimit {
					t.Errorf("default limit = %d, want %d", limit, tt.wantLimit)
				}
			}
		})
	}
}

func splitQueryParams(qs string) []string {
	var result []string
	for _, p := range splitString(qs, '&') {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func splitKeyValue(kv string) []string {
	return splitString(kv, '=')
}

func splitString(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func parseInt(s string) (int, error) {
	n := 0
	negative := false
	i := 0
	if len(s) > 0 && s[0] == '-' {
		negative = true
		i = 1
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(s[i]-'0')
	}
	if negative {
		n = -n
	}
	return n, nil
}

func TestSIMResponseTimestampFormat(t *testing.T) {
	now := time.Now()
	s := &store.SIM{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		ICCID:      "8990123456789012345",
		IMSI:       "286010123456789",
		SimType:    "physical",
		State:      "ordered",
		Metadata:   json.RawMessage(`{}`),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	resp := toSIMResponse(s)

	expectedCreatedAt := now.Format(time.RFC3339Nano)
	if resp.CreatedAt != expectedCreatedAt {
		t.Errorf("CreatedAt = %q, want %q", resp.CreatedAt, expectedCreatedAt)
	}
	expectedUpdatedAt := now.Format(time.RFC3339Nano)
	if resp.UpdatedAt != expectedUpdatedAt {
		t.Errorf("UpdatedAt = %q, want %q", resp.UpdatedAt, expectedUpdatedAt)
	}
}
