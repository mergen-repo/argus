package store

import (
	"encoding/json"
	"testing"
)

func TestOperatorStruct(t *testing.T) {
	o := Operator{
		Name:                     "Test Operator",
		Code:                     "test_op",
		MCC:                      "286",
		MNC:                      "01",
		AdapterType:              "mock",
		AdapterConfig:            json.RawMessage(`{}`),
		HealthStatus:             "unknown",
		HealthCheckIntervalSec:   30,
		FailoverPolicy:           "reject",
		FailoverTimeoutMs:        5000,
		CircuitBreakerThreshold:  5,
		CircuitBreakerRecoverySec: 60,
		State:                    "active",
	}

	if o.Name != "Test Operator" {
		t.Errorf("Name = %q, want %q", o.Name, "Test Operator")
	}
	if o.Code != "test_op" {
		t.Errorf("Code = %q, want %q", o.Code, "test_op")
	}
	if o.MCC != "286" {
		t.Errorf("MCC = %q, want %q", o.MCC, "286")
	}
	if o.MNC != "01" {
		t.Errorf("MNC = %q, want %q", o.MNC, "01")
	}
	if o.AdapterType != "mock" {
		t.Errorf("AdapterType = %q, want %q", o.AdapterType, "mock")
	}
	if o.HealthStatus != "unknown" {
		t.Errorf("HealthStatus = %q, want %q", o.HealthStatus, "unknown")
	}
	if o.HealthCheckIntervalSec != 30 {
		t.Errorf("HealthCheckIntervalSec = %d, want %d", o.HealthCheckIntervalSec, 30)
	}
	if o.FailoverPolicy != "reject" {
		t.Errorf("FailoverPolicy = %q, want %q", o.FailoverPolicy, "reject")
	}
	if o.CircuitBreakerThreshold != 5 {
		t.Errorf("CircuitBreakerThreshold = %d, want %d", o.CircuitBreakerThreshold, 5)
	}
	if o.CircuitBreakerRecoverySec != 60 {
		t.Errorf("CircuitBreakerRecoverySec = %d, want %d", o.CircuitBreakerRecoverySec, 60)
	}
}

func TestCreateOperatorParamsDefaults(t *testing.T) {
	p := CreateOperatorParams{
		Name:        "Test",
		Code:        "test",
		MCC:         "286",
		MNC:         "01",
		AdapterType: "mock",
	}

	if p.FailoverPolicy != nil {
		t.Error("FailoverPolicy should be nil (default applied in Create)")
	}
	if p.FailoverTimeoutMs != nil {
		t.Error("FailoverTimeoutMs should be nil (default applied in Create)")
	}
	if p.CircuitBreakerThreshold != nil {
		t.Error("CircuitBreakerThreshold should be nil (default applied in Create)")
	}
	if p.CircuitBreakerRecoverySec != nil {
		t.Error("CircuitBreakerRecoverySec should be nil (default applied in Create)")
	}
	if p.HealthCheckIntervalSec != nil {
		t.Error("HealthCheckIntervalSec should be nil (default applied in Create)")
	}
}

func TestUpdateOperatorParamsOptional(t *testing.T) {
	name := "Updated"
	p := UpdateOperatorParams{
		Name: &name,
	}

	if p.Name == nil || *p.Name != "Updated" {
		t.Error("Name should be set")
	}
	if p.FailoverPolicy != nil {
		t.Error("FailoverPolicy should be nil")
	}
	if p.State != nil {
		t.Error("State should be nil")
	}
}

func TestOperatorGrantStruct(t *testing.T) {
	g := OperatorGrant{
		Enabled: true,
	}

	if !g.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestOperatorHealthLogStruct(t *testing.T) {
	latency := 42
	h := OperatorHealthLog{
		Status:       "healthy",
		LatencyMs:    &latency,
		CircuitState: "closed",
	}

	if h.Status != "healthy" {
		t.Errorf("Status = %q, want %q", h.Status, "healthy")
	}
	if *h.LatencyMs != 42 {
		t.Errorf("LatencyMs = %d, want %d", *h.LatencyMs, 42)
	}
	if h.CircuitState != "closed" {
		t.Errorf("CircuitState = %q, want %q", h.CircuitState, "closed")
	}
}

func TestErrOperatorNotFound(t *testing.T) {
	if ErrOperatorNotFound.Error() != "store: operator not found" {
		t.Errorf("ErrOperatorNotFound = %q", ErrOperatorNotFound.Error())
	}
}

func TestErrOperatorCodeExists(t *testing.T) {
	if ErrOperatorCodeExists.Error() != "store: operator code already exists" {
		t.Errorf("ErrOperatorCodeExists = %q", ErrOperatorCodeExists.Error())
	}
}

func TestErrGrantNotFound(t *testing.T) {
	if ErrGrantNotFound.Error() != "store: operator grant not found" {
		t.Errorf("ErrGrantNotFound = %q", ErrGrantNotFound.Error())
	}
}

func TestErrGrantExists(t *testing.T) {
	if ErrGrantExists.Error() != "store: operator grant already exists" {
		t.Errorf("ErrGrantExists = %q", ErrGrantExists.Error())
	}
}

func TestOperatorSupportedRATTypes(t *testing.T) {
	o := Operator{
		SupportedRATTypes: []string{"nb_iot", "lte_m", "lte"},
	}

	if len(o.SupportedRATTypes) != 3 {
		t.Errorf("SupportedRATTypes len = %d, want 3", len(o.SupportedRATTypes))
	}
	if o.SupportedRATTypes[0] != "nb_iot" {
		t.Errorf("SupportedRATTypes[0] = %q, want %q", o.SupportedRATTypes[0], "nb_iot")
	}
}

func TestOperatorSLAUptimeTarget(t *testing.T) {
	target := 99.90
	o := Operator{
		SLAUptimeTarget: &target,
	}

	if o.SLAUptimeTarget == nil || *o.SLAUptimeTarget != 99.90 {
		t.Error("SLAUptimeTarget should be 99.90")
	}

	o2 := Operator{}
	if o2.SLAUptimeTarget != nil {
		t.Error("SLAUptimeTarget should be nil by default")
	}
}
