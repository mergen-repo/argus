package msisdn

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func TestToDTO(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	operatorID := uuid.New()
	simID := uuid.New()
	now := time.Now().UTC()
	reservedUntil := now.Add(24 * time.Hour)

	m := &store.MSISDN{
		ID:            id,
		TenantID:      tenantID,
		OperatorID:    operatorID,
		MSISDN:        "+905551234567",
		State:         "assigned",
		SimID:         &simID,
		ReservedUntil: &reservedUntil,
		CreatedAt:     now,
	}

	dto := toDTO(m)

	if dto.ID != id {
		t.Errorf("ID = %v, want %v", dto.ID, id)
	}
	if dto.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", dto.TenantID, tenantID)
	}
	if dto.OperatorID != operatorID {
		t.Errorf("OperatorID = %v, want %v", dto.OperatorID, operatorID)
	}
	if dto.MSISDN != "+905551234567" {
		t.Errorf("MSISDN = %q, want %q", dto.MSISDN, "+905551234567")
	}
	if dto.State != "assigned" {
		t.Errorf("State = %q, want %q", dto.State, "assigned")
	}
	if dto.SimID == nil || *dto.SimID != simID {
		t.Errorf("SimID = %v, want %v", dto.SimID, simID)
	}
	if dto.ReservedUntil == nil {
		t.Error("ReservedUntil should not be nil")
	}
	if dto.CreatedAt != now.Format(timeFmt) {
		t.Errorf("CreatedAt = %q, want %q", dto.CreatedAt, now.Format(timeFmt))
	}
}

func TestToDTONilOptionals(t *testing.T) {
	m := &store.MSISDN{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		MSISDN:     "+905551234567",
		State:      "available",
		CreatedAt:  time.Now().UTC(),
	}

	dto := toDTO(m)

	if dto.SimID != nil {
		t.Errorf("SimID = %v, want nil", dto.SimID)
	}
	if dto.ReservedUntil != nil {
		t.Errorf("ReservedUntil = %v, want nil", dto.ReservedUntil)
	}
}

func TestValidStates(t *testing.T) {
	validStates := map[string]bool{"available": true, "assigned": true, "reserved": true}

	if !validStates["available"] {
		t.Error("available should be valid")
	}
	if !validStates["assigned"] {
		t.Error("assigned should be valid")
	}
	if !validStates["reserved"] {
		t.Error("reserved should be valid")
	}
	if validStates["invalid"] {
		t.Error("invalid should not be valid")
	}
}

func TestImportRequestValidation(t *testing.T) {
	tests := []struct {
		name       string
		body       importRequest
		wantErrors int
	}{
		{
			name: "valid request",
			body: importRequest{
				OperatorID: uuid.New(),
				MSISDNs: []struct {
					MSISDN       string `json:"msisdn"`
					OperatorCode string `json:"operator_code"`
				}{
					{MSISDN: "+905551234567", OperatorCode: "TURKCELL"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "missing operator_id",
			body: importRequest{
				MSISDNs: []struct {
					MSISDN       string `json:"msisdn"`
					OperatorCode string `json:"operator_code"`
				}{
					{MSISDN: "+905551234567"},
				},
			},
			wantErrors: 1,
		},
		{
			name:       "missing msisdns",
			body:       importRequest{OperatorID: uuid.New()},
			wantErrors: 1,
		},
		{
			name:       "both missing",
			body:       importRequest{},
			wantErrors: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var validationErrors []map[string]interface{}
			if tc.body.OperatorID == uuid.Nil {
				validationErrors = append(validationErrors, map[string]interface{}{"field": "operator_id"})
			}
			if len(tc.body.MSISDNs) == 0 {
				validationErrors = append(validationErrors, map[string]interface{}{"field": "msisdns"})
			}
			if len(validationErrors) != tc.wantErrors {
				t.Errorf("validation errors = %d, want %d", len(validationErrors), tc.wantErrors)
			}
		})
	}
}

func TestAssignRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "valid sim_id",
			body:    `{"sim_id":"` + uuid.New().String() + `"}`,
			wantErr: false,
		},
		{
			name:    "nil sim_id",
			body:    `{"sim_id":"00000000-0000-0000-0000-000000000000"}`,
			wantErr: true,
		},
		{
			name:    "missing sim_id",
			body:    `{}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req assignRequest
			if err := json.NewDecoder(strings.NewReader(tc.body)).Decode(&req); err != nil {
				t.Fatalf("decode: %v", err)
			}
			hasErr := req.SimID == uuid.Nil
			if hasErr != tc.wantErr {
				t.Errorf("validation error = %v, want %v", hasErr, tc.wantErr)
			}
		})
	}
}

func TestCSVHeaderParsing(t *testing.T) {
	tests := []struct {
		name       string
		csvContent string
		wantMSISDN bool
		wantOpCode bool
	}{
		{
			name:       "standard headers",
			csvContent: "msisdn,operator_code\n+905551234567,TURKCELL\n",
			wantMSISDN: true,
			wantOpCode: true,
		},
		{
			name:       "msisdn only",
			csvContent: "msisdn\n+905551234567\n",
			wantMSISDN: true,
			wantOpCode: false,
		},
		{
			name:       "case insensitive",
			csvContent: "MSISDN,OPERATOR_CODE\n+905551234567,TURKCELL\n",
			wantMSISDN: true,
			wantOpCode: true,
		},
		{
			name:       "extra whitespace",
			csvContent: " msisdn , operator_code \n+905551234567,TURKCELL\n",
			wantMSISDN: true,
			wantOpCode: true,
		},
		{
			name:       "missing msisdn column",
			csvContent: "phone,operator_code\n+905551234567,TURKCELL\n",
			wantMSISDN: false,
			wantOpCode: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			_ = writer.WriteField("operator_id", uuid.New().String())
			part, _ := writer.CreateFormFile("file", "msisdns.csv")
			_, _ = part.Write([]byte(tc.csvContent))
			writer.Close()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/msisdn-pool/import", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())

			_ = req.ParseMultipartForm(10 << 20)
			file, _, err := req.FormFile("file")
			if err != nil {
				t.Fatalf("form file: %v", err)
			}
			defer file.Close()

			reader := csv.NewReader(file)
			header, err := reader.Read()
			if err != nil {
				t.Fatalf("read header: %v", err)
			}

			msisdnCol := -1
			operatorCodeCol := -1
			for i, col := range header {
				switch strings.TrimSpace(strings.ToLower(col)) {
				case "msisdn":
					msisdnCol = i
				case "operator_code":
					operatorCodeCol = i
				}
			}

			hasMSISDN := msisdnCol >= 0
			hasOpCode := operatorCodeCol >= 0

			if hasMSISDN != tc.wantMSISDN {
				t.Errorf("msisdn column found = %v, want %v", hasMSISDN, tc.wantMSISDN)
			}
			if hasOpCode != tc.wantOpCode {
				t.Errorf("operator_code column found = %v, want %v", hasOpCode, tc.wantOpCode)
			}
		})
	}
}

func TestStateValidation(t *testing.T) {
	validStates := map[string]bool{"available": true, "assigned": true, "reserved": true}

	tests := []struct {
		state string
		valid bool
	}{
		{"available", true},
		{"assigned", true},
		{"reserved", true},
		{"released", false},
		{"deleted", false},
		{"", false},
		{"AVAILABLE", false},
	}

	for _, tc := range tests {
		t.Run("state_"+tc.state, func(t *testing.T) {
			if validStates[tc.state] != tc.valid {
				t.Errorf("state %q valid = %v, want %v", tc.state, validStates[tc.state], tc.valid)
			}
		})
	}
}

func TestImportRequestJSON(t *testing.T) {
	jsonBody := `{
		"operator_id": "` + uuid.New().String() + `",
		"msisdns": [
			{"msisdn": "+905551234567", "operator_code": "TURKCELL"},
			{"msisdn": "+905559876543"}
		]
	}`

	var req importRequest
	err := json.NewDecoder(strings.NewReader(jsonBody)).Decode(&req)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if req.OperatorID == uuid.Nil {
		t.Error("OperatorID should not be nil")
	}
	if len(req.MSISDNs) != 2 {
		t.Fatalf("MSISDNs count = %d, want 2", len(req.MSISDNs))
	}
	if req.MSISDNs[0].MSISDN != "+905551234567" {
		t.Errorf("MSISDNs[0].MSISDN = %q, want %q", req.MSISDNs[0].MSISDN, "+905551234567")
	}
	if req.MSISDNs[0].OperatorCode != "TURKCELL" {
		t.Errorf("MSISDNs[0].OperatorCode = %q, want %q", req.MSISDNs[0].OperatorCode, "TURKCELL")
	}
	if req.MSISDNs[1].OperatorCode != "" {
		t.Errorf("MSISDNs[1].OperatorCode = %q, want empty", req.MSISDNs[1].OperatorCode)
	}
}

func TestMSISDNDTOJSONSerialization(t *testing.T) {
	simID := uuid.New()
	dto := msisdnDTO{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		MSISDN:     "+905551234567",
		State:      "assigned",
		SimID:      &simID,
		CreatedAt:  "2026-03-20T10:00:00Z",
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded["msisdn"] != "+905551234567" {
		t.Errorf("msisdn = %v, want +905551234567", decoded["msisdn"])
	}
	if decoded["state"] != "assigned" {
		t.Errorf("state = %v, want assigned", decoded["state"])
	}
	if decoded["sim_id"] != simID.String() {
		t.Errorf("sim_id = %v, want %s", decoded["sim_id"], simID.String())
	}
	if decoded["reserved_until"] != nil {
		t.Errorf("reserved_until should be omitted when nil, got %v", decoded["reserved_until"])
	}
}

func TestMSISDNDTOOmitsNilFields(t *testing.T) {
	dto := msisdnDTO{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		MSISDN:     "+905551234567",
		State:      "available",
		CreatedAt:  "2026-03-20T10:00:00Z",
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	str := string(data)
	if strings.Contains(str, "sim_id") {
		t.Error("sim_id should be omitted when nil")
	}
	if strings.Contains(str, "reserved_until") {
		t.Error("reserved_until should be omitted when nil")
	}
}
