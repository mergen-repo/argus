package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

type AAAHealthChecker interface {
	Healthy() bool
	ActiveSessionCount(ctx context.Context) (int64, error)
}

type DiameterHealthChecker interface {
	Healthy() bool
	ActiveSessionCount(ctx context.Context) (int64, error)
}

type SBAHealthChecker interface {
	Healthy() bool
	ActiveSessionCount(ctx context.Context) (int64, error)
}

type probeResult struct {
	Status        string    `json:"status"`
	LatencyMs     int64     `json:"latency_ms"`
	LastCheckedAt time.Time `json:"last_checked_at"`
	Err           string    `json:"error,omitempty"`
}

type HealthHandler struct {
	db       HealthChecker
	redis    HealthChecker
	nats     HealthChecker
	aaa      AAAHealthChecker
	diameter DiameterHealthChecker
	sba      SBAHealthChecker
	startAt  time.Time
}

func NewHealthHandler(db, redis, nats HealthChecker) *HealthHandler {
	return &HealthHandler{
		db:      db,
		redis:   redis,
		nats:    nats,
		startAt: time.Now(),
	}
}

func (h *HealthHandler) SetAAAChecker(aaa AAAHealthChecker) {
	h.aaa = aaa
}

func (h *HealthHandler) SetDiameterChecker(d DiameterHealthChecker) {
	h.diameter = d
}

func (h *HealthHandler) SetSBAChecker(sba SBAHealthChecker) {
	h.sba = sba
}

type apiResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  *apiError   `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type aaaHealthData struct {
	Radius         probeResult `json:"radius"`
	Diameter       *probeResult `json:"diameter,omitempty"`
	SBA            *probeResult `json:"sba,omitempty"`
	SessionsActive int64        `json:"sessions_active"`
}

type healthData struct {
	DB     probeResult    `json:"db"`
	Redis  probeResult    `json:"redis"`
	NATS   probeResult    `json:"nats"`
	AAA    *aaaHealthData `json:"aaa,omitempty"`
	Uptime string         `json:"uptime"`
}

func runProbe(ctx context.Context, checker HealthChecker) probeResult {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	start := time.Now()
	err := checker.HealthCheck(probeCtx)
	elapsed := time.Since(start)

	if err != nil {
		return probeResult{
			Status:        "error: " + err.Error(),
			LatencyMs:     elapsed.Milliseconds(),
			LastCheckedAt: time.Now(),
			Err:           err.Error(),
		}
	}
	return probeResult{
		Status:        "ok",
		LatencyMs:     elapsed.Milliseconds(),
		LastCheckedAt: time.Now(),
	}
}

func boolProbe(fn func() bool) probeResult {
	start := time.Now()
	healthy := fn()
	elapsed := time.Since(start)
	if healthy {
		return probeResult{
			Status:        "ok",
			LatencyMs:     elapsed.Milliseconds(),
			LastCheckedAt: time.Now(),
		}
	}
	return probeResult{
		Status:        "stopped",
		LatencyMs:     elapsed.Milliseconds(),
		LastCheckedAt: time.Now(),
		Err:           "service not running",
	}
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	dbResult := runProbe(ctx, h.db)
	redisResult := runProbe(ctx, h.redis)
	natsResult := runProbe(ctx, h.nats)

	data := healthData{
		DB:     dbResult,
		Redis:  redisResult,
		NATS:   natsResult,
		Uptime: time.Since(h.startAt).Round(time.Second).String(),
	}

	healthy := dbResult.Err == "" && redisResult.Err == "" && natsResult.Err == ""

	if h.aaa != nil || h.diameter != nil || h.sba != nil {
		aaaData := &aaaHealthData{}

		if h.aaa != nil {
			aaaData.Radius = boolProbe(h.aaa.Healthy)
			if aaaData.Radius.Err != "" {
				healthy = false
			}
			if count, err := h.aaa.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		} else {
			aaaData.Radius = probeResult{Status: "pending"}
		}

		if h.diameter != nil {
			p := boolProbe(h.diameter.Healthy)
			aaaData.Diameter = &p
			if p.Err != "" {
				healthy = false
			}
			if count, err := h.diameter.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		}

		if h.sba != nil {
			p := boolProbe(h.sba.Healthy)
			aaaData.SBA = &p
			if p.Err != "" {
				healthy = false
			}
			if count, err := h.sba.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		}

		data.AAA = aaaData
	}

	w.Header().Set("Content-Type", "application/json")

	status := http.StatusOK
	resp := apiResponse{Status: "success", Data: data}

	if !healthy {
		status = http.StatusServiceUnavailable
		resp.Status = "error"
		resp.Error = &apiError{
			Code:    "SERVICE_UNAVAILABLE",
			Message: "one or more services are unhealthy",
		}
		resp.Data = data
	}

	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}
