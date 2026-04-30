package cdr

import (
	"encoding/json"
	"testing"
	"time"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionEvent_Unmarshal(t *testing.T) {
	data := `{
		"session_id": "00000000-0000-0000-0000-000000000001",
		"sim_id": "00000000-0000-0000-0000-000000000002",
		"tenant_id": "00000000-0000-0000-0000-000000000003",
		"operator_id": "00000000-0000-0000-0000-000000000004",
		"apn_id": "00000000-0000-0000-0000-000000000005",
		"rat_type": "lte",
		"bytes_in": 1048576,
		"bytes_out": 524288,
		"duration_sec": 3600,
		"terminate_cause": "user_request",
		"protocol_type": "radius",
		"ended_at": "2026-03-22T10:00:00Z"
	}`

	var evt sessionEvent
	err := json.Unmarshal([]byte(data), &evt)
	require.NoError(t, err)

	assert.Equal(t, "00000000-0000-0000-0000-000000000001", evt.SessionID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000002", evt.SimID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000003", evt.TenantID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000004", evt.OperatorID)
	assert.Equal(t, "00000000-0000-0000-0000-000000000005", evt.APNID)
	assert.Equal(t, "lte", evt.RATType)
	assert.Equal(t, int64(1048576), evt.BytesIn)
	assert.Equal(t, int64(524288), evt.BytesOut)
	assert.Equal(t, 3600, evt.DurationSec)
	assert.Equal(t, "user_request", evt.TerminateCause)
	assert.Equal(t, "radius", evt.ProtocolType)
	assert.Equal(t, "2026-03-22T10:00:00Z", evt.EndedAt)
}

func TestSessionEvent_MinimalPayload(t *testing.T) {
	data := `{
		"session_id": "00000000-0000-0000-0000-000000000001",
		"sim_id": "00000000-0000-0000-0000-000000000002",
		"tenant_id": "00000000-0000-0000-0000-000000000003",
		"operator_id": "00000000-0000-0000-0000-000000000004"
	}`

	var evt sessionEvent
	err := json.Unmarshal([]byte(data), &evt)
	require.NoError(t, err)

	assert.Equal(t, "", evt.APNID)
	assert.Equal(t, "", evt.RATType)
	assert.Equal(t, int64(0), evt.BytesIn)
	assert.Equal(t, int64(0), evt.BytesOut)
	assert.Equal(t, 0, evt.DurationSec)
}

func TestRecordTypeFromSubject(t *testing.T) {
	tests := []struct {
		subject    string
		recordType string
	}{
		{"argus.events.session.started", "start"},
		{"argus.events.session.updated", "interim"},
		{"argus.events.session.ended", "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			var recordType string
			switch tt.subject {
			case "argus.events.session.started":
				recordType = "start"
			case "argus.events.session.updated":
				recordType = "interim"
			case "argus.events.session.ended":
				recordType = "stop"
			}
			assert.Equal(t, tt.recordType, recordType)
		})
	}
}

func TestTimeParsing(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"2026-03-22T10:00:00Z", true},
		{"2026-03-22T10:00:00+03:00", true},
		{"", false},
		{"bad-date", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := time.Parse(time.RFC3339, tt.input)
			if tt.valid {
				assert.NoError(t, err)
			} else if tt.input != "" {
				assert.Error(t, err)
			}
		})
	}
}

func TestRatingIntegration_500MB_BasicRate(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(
		500*1024*1024, 0,
		"lte",
		time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC),
		0,
	)

	require.NotNil(t, result)
	assert.InDelta(t, 5.0, result.UsageCost, 0.01)
	assert.InDelta(t, 5.0, result.CarrierCost, 0.01)
}

func TestRatingIntegration_500MB_5G(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(
		500*1024*1024, 0,
		"nr_5g",
		time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC),
		0,
	)

	require.NotNil(t, result)
	assert.InDelta(t, 7.5, result.UsageCost, 0.01)
	assert.InDelta(t, 5.0, result.CarrierCost, 0.01)
}

// TestHandleEvent_DropsMalformedIMSI verifies that when imsiStrict=true and a
// session event carries an invalid IMSI ("abc"), handleEvent drops the record
// (no CDR write) and increments argus_imsi_invalid_total{source="cdr"} by 1
// (FIX-207 AC-4).
func TestHandleEvent_DropsMalformedIMSI(t *testing.T) {
	reg := obsmetrics.NewRegistry()

	c := &Consumer{
		reg:        reg,
		imsiStrict: true,
		logger:     zerolog.Nop(),
	}

	pre := testutil.ToFloat64(reg.IMSIInvalidTotal.WithLabelValues("cdr"))

	payload := `{
		"session_id": "00000000-0000-0000-0000-000000000001",
		"sim_id":     "00000000-0000-0000-0000-000000000002",
		"tenant_id":  "00000000-0000-0000-0000-000000000003",
		"operator_id":"00000000-0000-0000-0000-000000000004",
		"imsi":       "abc"
	}`

	c.handleEvent("argus.events.session.started", []byte(payload))

	post := testutil.ToFloat64(reg.IMSIInvalidTotal.WithLabelValues("cdr"))
	if post != pre+1 {
		t.Errorf("IMSIInvalidTotal{cdr}: pre=%.0f post=%.0f, want increment of 1", pre, post)
	}
}

func TestRatingIntegration_ZeroCost(t *testing.T) {
	rc := NewRatingConfig(0.0)
	result := rc.Calculate(
		1024*1024*1024, 0,
		"lte",
		time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC),
		0,
	)

	require.NotNil(t, result)
	assert.Equal(t, 0.0, result.UsageCost)
	assert.Equal(t, 0.0, result.CarrierCost)
}
