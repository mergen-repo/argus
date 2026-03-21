package cdr

import (
	"math"
	"time"
)

type VolumeTier struct {
	UpToBytes  int64
	Multiplier float64
}

type RatingConfig struct {
	CostPerMB         float64
	RATMultipliers    map[string]float64
	PeakHoursStart    int
	PeakHoursEnd      int
	PeakMultiplier    float64
	OffPeakMultiplier float64
	VolumeTiers       []VolumeTier
}

type RatingResult struct {
	UsageCost     float64
	CarrierCost   float64
	RatePerMB     float64
	RATMultiplier float64
	TotalMB       float64
}

func DefaultRATMultipliers() map[string]float64 {
	return map[string]float64{
		"utran":     1.0,
		"geran":     0.5,
		"lte":       1.0,
		"nb_iot":    0.3,
		"lte_m":     0.5,
		"nr_5g":     1.5,
		"nr_5g_nsa": 1.2,
		"unknown":   1.0,
	}
}

func DefaultVolumeTiers() []VolumeTier {
	return []VolumeTier{
		{UpToBytes: 1 * 1024 * 1024 * 1024, Multiplier: 1.0},
		{UpToBytes: 10 * 1024 * 1024 * 1024, Multiplier: 0.8},
		{UpToBytes: math.MaxInt64, Multiplier: 0.5},
	}
}

func NewRatingConfig(costPerMB float64) *RatingConfig {
	return &RatingConfig{
		CostPerMB:         costPerMB,
		RATMultipliers:    DefaultRATMultipliers(),
		PeakHoursStart:    8,
		PeakHoursEnd:      20,
		PeakMultiplier:    1.0,
		OffPeakMultiplier: 0.7,
		VolumeTiers:       DefaultVolumeTiers(),
	}
}

func (rc *RatingConfig) Calculate(bytesIn, bytesOut int64, ratType string, timestamp time.Time, cumulativeSessionBytes int64) *RatingResult {
	totalBytes := bytesIn + bytesOut
	totalMB := float64(totalBytes) / (1024.0 * 1024.0)

	ratMultiplier := 1.0
	if ratType != "" {
		if m, ok := rc.RATMultipliers[ratType]; ok {
			ratMultiplier = m
		}
	}

	timeMultiplier := rc.getTimeMultiplier(timestamp)
	volumeMultiplier := rc.getVolumeMultiplier(cumulativeSessionBytes)

	usageCost := totalMB * rc.CostPerMB * ratMultiplier * timeMultiplier * volumeMultiplier
	carrierCost := totalMB * rc.CostPerMB

	return &RatingResult{
		UsageCost:     roundTo(usageCost, 6),
		CarrierCost:   roundTo(carrierCost, 6),
		RatePerMB:     rc.CostPerMB,
		RATMultiplier: ratMultiplier,
		TotalMB:       roundTo(totalMB, 4),
	}
}

func (rc *RatingConfig) getTimeMultiplier(t time.Time) float64 {
	hour := t.UTC().Hour()
	if hour >= rc.PeakHoursStart && hour < rc.PeakHoursEnd {
		return rc.PeakMultiplier
	}
	return rc.OffPeakMultiplier
}

func (rc *RatingConfig) getVolumeMultiplier(cumulativeBytes int64) float64 {
	for _, tier := range rc.VolumeTiers {
		if cumulativeBytes <= tier.UpToBytes {
			return tier.Multiplier
		}
	}
	if len(rc.VolumeTiers) > 0 {
		return rc.VolumeTiers[len(rc.VolumeTiers)-1].Multiplier
	}
	return 1.0
}

func roundTo(val float64, places int) float64 {
	pow := math.Pow(10, float64(places))
	return math.Round(val*pow) / pow
}
