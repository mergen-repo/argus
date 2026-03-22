package store

import (
	"encoding/json"
	"testing"
)

func TestHashWithSalt_Deterministic(t *testing.T) {
	h1 := hashWithSalt("test-value", "test-salt")
	h2 := hashWithSalt("test-value", "test-salt")

	if h1 != h2 {
		t.Fatalf("hashWithSalt not deterministic: %s != %s", h1, h2)
	}

	if len(h1) != 64 {
		t.Fatalf("hash length = %d, want 64", len(h1))
	}
}

func TestHashWithSalt_DifferentSalts(t *testing.T) {
	h1 := hashWithSalt("test-value", "salt-a")
	h2 := hashWithSalt("test-value", "salt-b")

	if h1 == h2 {
		t.Fatal("different salts should produce different hashes")
	}
}

func TestHashWithSalt_DifferentValues(t *testing.T) {
	h1 := hashWithSalt("value-a", "salt")
	h2 := hashWithSalt("value-b", "salt")

	if h1 == h2 {
		t.Fatal("different values should produce different hashes")
	}
}

func TestAnonymizeJSONWithSalt_SensitiveFields(t *testing.T) {
	data := json.RawMessage(`{"imsi":"310260000000001","msisdn":"+14155551234","name":"TestSIM"}`)
	fields := []string{"imsi", "msisdn"}
	salt := "test-salt"

	result := anonymizeJSONWithSalt(data, fields, salt)

	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if m["imsi"] == "310260000000001" {
		t.Fatal("imsi should be anonymized")
	}
	if m["msisdn"] == "+14155551234" {
		t.Fatal("msisdn should be anonymized")
	}
	if m["name"] != "TestSIM" {
		t.Fatalf("name should be unchanged, got %v", m["name"])
	}

	if len(m["imsi"].(string)) != 64 {
		t.Fatal("anonymized imsi should be a 64-char SHA-256 hash")
	}
}

func TestAnonymizeJSONWithSalt_NoSensitiveFields(t *testing.T) {
	data := json.RawMessage(`{"name":"TestSIM","state":"active"}`)
	fields := []string{"imsi", "msisdn"}
	salt := "test-salt"

	result := anonymizeJSONWithSalt(data, fields, salt)

	if string(result) != string(data) {
		t.Fatalf("result should be unchanged when no sensitive fields present")
	}
}

func TestAnonymizeJSONWithSalt_EmptyData(t *testing.T) {
	result := anonymizeJSONWithSalt(nil, []string{"imsi"}, "salt")
	if result != nil {
		t.Fatal("nil input should return nil")
	}

	result = anonymizeJSONWithSalt(json.RawMessage{}, []string{"imsi"}, "salt")
	if len(result) != 0 {
		t.Fatal("empty input should return empty")
	}
}

func TestAnonymizeJSONWithSalt_InvalidJSON(t *testing.T) {
	data := json.RawMessage(`not-json`)
	result := anonymizeJSONWithSalt(data, []string{"imsi"}, "salt")

	if string(result) != string(data) {
		t.Fatal("invalid JSON should be returned as-is")
	}
}

func TestAnonymizeJSONWithSalt_Irreversible(t *testing.T) {
	data := json.RawMessage(`{"imsi":"310260000000001"}`)
	salt := "test-salt"

	result := anonymizeJSONWithSalt(data, []string{"imsi"}, salt)

	var m map[string]interface{}
	json.Unmarshal(result, &m)

	result2 := anonymizeJSONWithSalt(result, []string{"imsi"}, salt)
	var m2 map[string]interface{}
	json.Unmarshal(result2, &m2)

	if m["imsi"] == m2["imsi"] {
		t.Fatal("double-hashing should produce a different value (irreversible)")
	}
}
