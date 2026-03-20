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
	Radius         string `json:"radius"`
	Diameter       string `json:"diameter,omitempty"`
	SBA            string `json:"sba,omitempty"`
	SessionsActive int64  `json:"sessions_active"`
}

type healthData struct {
	DB     string         `json:"db"`
	Redis  string         `json:"redis"`
	NATS   string         `json:"nats"`
	AAA    *aaaHealthData `json:"aaa,omitempty"`
	Uptime string         `json:"uptime"`
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := healthData{
		DB:     "ok",
		Redis:  "ok",
		NATS:   "ok",
		Uptime: time.Since(h.startAt).Round(time.Second).String(),
	}

	healthy := true

	if err := h.db.HealthCheck(ctx); err != nil {
		data.DB = "error: " + err.Error()
		healthy = false
	}
	if err := h.redis.HealthCheck(ctx); err != nil {
		data.Redis = "error: " + err.Error()
		healthy = false
	}
	if err := h.nats.HealthCheck(ctx); err != nil {
		data.NATS = "error: " + err.Error()
		healthy = false
	}

	if h.aaa != nil || h.diameter != nil || h.sba != nil {
		aaaData := &aaaHealthData{
			Radius:   "stopped",
			Diameter: "stopped",
			SBA:      "stopped",
		}
		if h.aaa != nil {
			if h.aaa.Healthy() {
				aaaData.Radius = "ok"
			}
			if count, err := h.aaa.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		}
		if h.diameter != nil {
			if h.diameter.Healthy() {
				aaaData.Diameter = "ok"
			}
			if count, err := h.diameter.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		} else {
			aaaData.Diameter = ""
		}
		if h.sba != nil {
			if h.sba.Healthy() {
				aaaData.SBA = "ok"
			}
			if count, err := h.sba.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		} else {
			aaaData.SBA = ""
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
