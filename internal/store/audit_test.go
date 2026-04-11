package store

import (
	"encoding/json"
	"testing"
)

const testAuditSalt = "test-audit-salt"

func TestAnonymizeJSON_ReplacesFields(t *testing.T) {
	data := json.RawMessage(`{"imsi":"286010123456789","msisdn":"+905551234567","name":"Test SIM","iccid":"8990111234567890"}`)
	fields := []string{"imsi", "msisdn", "iccid"}

	result := anonymizeJSONWithSalt(data, fields, testAuditSalt)
	if result == nil {
		t.Fatal("result should not be nil")
	}

	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if m["imsi"] == "286010123456789" {
		t.Fatal("imsi should be hashed")
	}
	if len(m["imsi"].(string)) != 64 {
		t.Fatalf("imsi hash length = %d, want 64", len(m["imsi"].(string)))
	}

	if m["msisdn"] == "+905551234567" {
		t.Fatal("msisdn should be hashed")
	}

	if m["iccid"] == "8990111234567890" {
		t.Fatal("iccid should be hashed")
	}

	if m["name"] != "Test SIM" {
		t.Fatalf("name = %v, want Test SIM (should not be changed)", m["name"])
	}
}

func TestAnonymizeJSON_NoSensitiveFields(t *testing.T) {
	data := json.RawMessage(`{"name":"Test SIM","state":"active"}`)
	fields := []string{"imsi", "msisdn", "iccid"}

	result := anonymizeJSONWithSalt(data, fields, testAuditSalt)

	var original, anonymized map[string]interface{}
	json.Unmarshal(data, &original)
	json.Unmarshal(result, &anonymized)

	if anonymized["name"] != original["name"] {
		t.Fatal("name should not change when no sensitive fields present")
	}
	if anonymized["state"] != original["state"] {
		t.Fatal("state should not change when no sensitive fields present")
	}
}

func TestAnonymizeJSON_EmptyData(t *testing.T) {
	result := anonymizeJSONWithSalt(nil, []string{"imsi"}, testAuditSalt)
	if result != nil {
		t.Fatal("should return nil for nil input")
	}

	result = anonymizeJSONWithSalt(json.RawMessage{}, []string{"imsi"}, testAuditSalt)
	if len(result) != 0 {
		t.Fatal("should return empty for empty input")
	}
}

func TestAnonymizeJSON_InvalidJSON(t *testing.T) {
	data := json.RawMessage(`not json`)
	result := anonymizeJSONWithSalt(data, []string{"imsi"}, testAuditSalt)

	if string(result) != string(data) {
		t.Fatal("should return original data for invalid JSON")
	}
}

func TestAnonymizeJSON_EmptyStringValue(t *testing.T) {
	data := json.RawMessage(`{"imsi":"","name":"test"}`)
	fields := []string{"imsi"}

	result := anonymizeJSONWithSalt(data, fields, testAuditSalt)

	var m map[string]interface{}
	json.Unmarshal(result, &m)

	if m["imsi"] != "" {
		t.Fatal("empty imsi should remain empty, not be hashed")
	}
}
