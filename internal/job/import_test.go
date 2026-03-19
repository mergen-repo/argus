package job

import (
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
