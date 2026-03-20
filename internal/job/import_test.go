package job

import (
	"encoding/json"
	"testing"
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
