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

type HealthHandler struct {
	db      HealthChecker
	redis   HealthChecker
	nats    HealthChecker
	startAt time.Time
}

func NewHealthHandler(db, redis, nats HealthChecker) *HealthHandler {
	return &HealthHandler{
		db:      db,
		redis:   redis,
		nats:    nats,
		startAt: time.Now(),
	}
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

type healthData struct {
	DB     string `json:"db"`
	Redis  string `json:"redis"`
	NATS   string `json:"nats"`
	Uptime string `json:"uptime"`
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
