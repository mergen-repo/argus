package cdr

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func peakTime() time.Time {
	return time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
}

func offPeakTime() time.Time {
	return time.Date(2026, 3, 22, 22, 0, 0, 0, time.UTC)
}

func mb(n int64) int64 {
	return n * 1024 * 1024
}

func gb(n int64) int64 {
	return n * 1024 * 1024 * 1024
}

func TestRatingConfig_Calculate_BasicCost(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(250), mb(250), "lte", peakTime(), 0)

	require.NotNil(t, result)
	assert.InDelta(t, 5.0, result.UsageCost, 0.001)
	assert.InDelta(t, 5.0, result.CarrierCost, 0.001)
	assert.Equal(t, 0.01, result.RatePerMB)
	assert.Equal(t, 1.0, result.RATMultiplier)
	assert.InDelta(t, 500.0, result.TotalMB, 0.01)
}

func TestRatingConfig_Calculate_5GMultiplier(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(250), mb(250), "nr_5g", peakTime(), 0)

	require.NotNil(t, result)
	assert.InDelta(t, 7.5, result.UsageCost, 0.001)
	assert.InDelta(t, 5.0, result.CarrierCost, 0.001)
	assert.Equal(t, 1.5, result.RATMultiplier)
}

func TestRatingConfig_Calculate_NBIoTMultiplier(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(100), 0, "nb_iot", peakTime(), 0)

	require.NotNil(t, result)
	assert.InDelta(t, 0.3, result.UsageCost, 0.001)
	assert.InDelta(t, 1.0, result.CarrierCost, 0.001)
	assert.Equal(t, 0.3, result.RATMultiplier)
}

func TestRatingConfig_Calculate_OffPeakDiscount(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(250), mb(250), "lte", offPeakTime(), 0)

	require.NotNil(t, result)
	assert.InDelta(t, 3.5, result.UsageCost, 0.001)
	assert.InDelta(t, 5.0, result.CarrierCost, 0.001)
}

func TestRatingConfig_Calculate_VolumeTier2(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(500), mb(500), "lte", peakTime(), gb(2))

	require.NotNil(t, result)
	assert.InDelta(t, 8.0, result.UsageCost, 0.001)
	assert.InDelta(t, 10.0, result.CarrierCost, 0.001)
}

func TestRatingConfig_Calculate_VolumeTier3(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(500), mb(500), "lte", peakTime(), gb(15))

	require.NotNil(t, result)
	assert.InDelta(t, 5.0, result.UsageCost, 0.001)
	assert.InDelta(t, 10.0, result.CarrierCost, 0.001)
}

func TestRatingConfig_Calculate_ZeroBytes(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(0, 0, "lte", peakTime(), 0)

	require.NotNil(t, result)
	assert.Equal(t, 0.0, result.UsageCost)
	assert.Equal(t, 0.0, result.CarrierCost)
	assert.Equal(t, 0.0, result.TotalMB)
}

func TestRatingConfig_Calculate_UnknownRATType(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(100), 0, "something_unknown", peakTime(), 0)

	require.NotNil(t, result)
	assert.Equal(t, 1.0, result.RATMultiplier)
	assert.InDelta(t, 1.0, result.UsageCost, 0.001)
}

func TestRatingConfig_Calculate_EmptyRATType(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(100), 0, "", peakTime(), 0)

	require.NotNil(t, result)
	assert.Equal(t, 1.0, result.RATMultiplier)
	assert.InDelta(t, 1.0, result.UsageCost, 0.001)
}

func TestRatingConfig_Calculate_ZeroCostPerMB(t *testing.T) {
	rc := NewRatingConfig(0.0)
	result := rc.Calculate(mb(500), mb(500), "nr_5g", peakTime(), 0)

	require.NotNil(t, result)
	assert.Equal(t, 0.0, result.UsageCost)
	assert.Equal(t, 0.0, result.CarrierCost)
}

func TestRatingConfig_Calculate_CombinedMultipliers(t *testing.T) {
	rc := NewRatingConfig(0.01)
	result := rc.Calculate(mb(250), mb(250), "nr_5g", offPeakTime(), gb(5))

	require.NotNil(t, result)
	expected := 500.0 * 0.01 * 1.5 * 0.7 * 0.8
	assert.InDelta(t, expected, result.UsageCost, 0.001)
}

func TestDefaultRATMultipliers(t *testing.T) {
	m := DefaultRATMultipliers()
	assert.Equal(t, 1.0, m["lte"])
	assert.Equal(t, 1.5, m["nr_5g"])
	assert.Equal(t, 0.3, m["nb_iot"])
	assert.Equal(t, 0.5, m["lte_m"])
	assert.Equal(t, 1.2, m["nr_5g_nsa"])
	assert.Equal(t, 0.5, m["geran"])
	assert.Equal(t, 1.0, m["utran"])
}

func TestDefaultVolumeTiers(t *testing.T) {
	tiers := DefaultVolumeTiers()
	require.Len(t, tiers, 3)
	assert.Equal(t, gb(1), tiers[0].UpToBytes)
	assert.Equal(t, 1.0, tiers[0].Multiplier)
	assert.Equal(t, gb(10), tiers[1].UpToBytes)
	assert.Equal(t, 0.8, tiers[1].Multiplier)
	assert.Equal(t, int64(math.MaxInt64), tiers[2].UpToBytes)
	assert.Equal(t, 0.5, tiers[2].Multiplier)
}

func TestRoundTo(t *testing.T) {
	assert.Equal(t, 5.25, roundTo(5.2499999, 2))
	assert.Equal(t, 0.0001, roundTo(0.00014, 4))
	assert.Equal(t, 3.141593, roundTo(3.14159265, 6))
}
