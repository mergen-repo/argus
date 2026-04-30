package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btopcu/argus/internal/observability/metrics"
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

	diskMounts      []string
	diskDegradedPct int
	diskUnhealthyPct int
	metricsReg      *metrics.Registry

	startupOnce      sync.Once
	startupLatched   atomic.Bool
	startupFailCount atomic.Int32
}

func NewHealthHandler(db, redis, nats HealthChecker) *HealthHandler {
	return &HealthHandler{
		db:               db,
		redis:            redis,
		nats:             nats,
		startAt:          time.Now(),
		diskDegradedPct:  85,
		diskUnhealthyPct: 95,
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

func (h *HealthHandler) SetDiskConfig(mounts []string, degradedPct, unhealthyPct int) {
	h.diskMounts = mounts
	h.diskDegradedPct = degradedPct
	h.diskUnhealthyPct = unhealthyPct
}

func (h *HealthHandler) SetMetricsRegistry(reg *metrics.Registry) {
	h.metricsReg = reg
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
	Radius         probeResult  `json:"radius"`
	Diameter       *probeResult `json:"diameter,omitempty"`
	SBA            *probeResult `json:"sba,omitempty"`
	SessionsActive int64        `json:"sessions_active"`
}

type readyData struct {
	State           string          `json:"state"`
	DB              probeResult     `json:"db"`
	Redis           probeResult     `json:"redis"`
	NATS            probeResult     `json:"nats"`
	AAA             *aaaHealthData  `json:"aaa,omitempty"`
	Disks           []DiskProbeResult `json:"disks,omitempty"`
	DegradedReasons []string        `json:"degraded_reasons,omitempty"`
	Uptime          string          `json:"uptime"`
}

type liveData struct {
	Status     string `json:"status"`
	Uptime     string `json:"uptime"`
	Goroutines int    `json:"goroutines"`
	GoVersion  string `json:"go_version"`
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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	data := liveData{
		Status:     "alive",
		Uptime:     time.Since(h.startAt).Round(time.Second).String(),
		Goroutines: runtime.NumGoroutine(),
		GoVersion:  runtime.Version(),
	}
	writeJSON(w, http.StatusOK, apiResponse{Status: "success", Data: data})
}

func (h *HealthHandler) runReadyCheck(ctx context.Context) (readyData, int) {
	dbResult := runProbe(ctx, h.db)
	redisResult := runProbe(ctx, h.redis)
	natsResult := runProbe(ctx, h.nats)

	data := readyData{
		DB:     dbResult,
		Redis:  redisResult,
		NATS:   natsResult,
		Uptime: time.Since(h.startAt).Round(time.Second).String(),
	}

	var degradedReasons []string

	coreUnhealthy := dbResult.Err != "" || redisResult.Err != "" || natsResult.Err != ""

	if h.aaa != nil || h.diameter != nil || h.sba != nil {
		aaaData := &aaaHealthData{}
		aaaUpCount := 0
		aaaDownCount := 0
		aaaTotalConfigured := 0

		if h.aaa != nil {
			aaaTotalConfigured++
			aaaData.Radius = boolProbe(h.aaa.Healthy)
			if aaaData.Radius.Err != "" {
				aaaDownCount++
			} else {
				aaaUpCount++
			}
			if count, err := h.aaa.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		} else {
			aaaData.Radius = probeResult{Status: "pending"}
		}

		if h.diameter != nil {
			aaaTotalConfigured++
			p := boolProbe(h.diameter.Healthy)
			aaaData.Diameter = &p
			if p.Err != "" {
				aaaDownCount++
			} else {
				aaaUpCount++
			}
			if count, err := h.diameter.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		}

		if h.sba != nil {
			aaaTotalConfigured++
			p := boolProbe(h.sba.Healthy)
			aaaData.SBA = &p
			if p.Err != "" {
				aaaDownCount++
			} else {
				aaaUpCount++
			}
			if count, err := h.sba.ActiveSessionCount(ctx); err == nil {
				aaaData.SessionsActive += count
			}
		}

		if aaaTotalConfigured > 0 && aaaDownCount == aaaTotalConfigured {
			coreUnhealthy = true
		} else if aaaDownCount > 0 && aaaUpCount > 0 {
			degradedReasons = append(degradedReasons, "aaa_partial")
		}

		data.AAA = aaaData
	}

	if len(h.diskMounts) > 0 {
		diskResults := diskProbe(h.diskMounts, h.diskDegradedPct, h.diskUnhealthyPct)
		data.Disks = diskResults

		for _, dr := range diskResults {
			if h.metricsReg != nil && h.metricsReg.DiskUsagePercent != nil {
				h.metricsReg.DiskUsagePercent.WithLabelValues(dr.Mount).Set(dr.UsedPct)
			}
			switch dr.Status {
			case "unhealthy":
				coreUnhealthy = true
			case "degraded":
				degradedReasons = append(degradedReasons, "disk_degraded:"+dr.Mount)
			}
		}
	}

	var httpStatus int
	if coreUnhealthy {
		data.State = "unhealthy"
		data.DegradedReasons = nil
		httpStatus = http.StatusServiceUnavailable
	} else if len(degradedReasons) > 0 {
		data.State = "degraded"
		data.DegradedReasons = degradedReasons
		httpStatus = http.StatusOK
	} else {
		data.State = "healthy"
		httpStatus = http.StatusOK
	}

	return data, httpStatus
}

func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data, httpStatus := h.runReadyCheck(ctx)

	if httpStatus == http.StatusServiceUnavailable {
		writeJSON(w, httpStatus, apiResponse{
			Status: "error",
			Error: &apiError{
				Code:    "SERVICE_UNAVAILABLE",
				Message: "one or more services are unhealthy",
			},
			Data: data,
		})
		return
	}
	writeJSON(w, httpStatus, apiResponse{Status: "success", Data: data})
}

func (h *HealthHandler) Startup(w http.ResponseWriter, r *http.Request) {
	if h.startupLatched.Load() {
		writeJSON(w, http.StatusOK, apiResponse{
			Status: "success",
			Data:   map[string]string{"state": "started"},
		})
		return
	}

	ctx := r.Context()
	_, httpStatus := h.runReadyCheck(ctx)

	if httpStatus == http.StatusOK {
		h.startupOnce.Do(func() {
			h.startupLatched.Store(true)
		})
		writeJSON(w, http.StatusOK, apiResponse{
			Status: "success",
			Data:   map[string]string{"state": "started"},
		})
		return
	}

	inStartupWindow := time.Since(h.startAt) < 60*time.Second
	if inStartupWindow {
		count := h.startupFailCount.Add(1)
		if count <= 3 {
			writeJSON(w, http.StatusServiceUnavailable, apiResponse{
				Status: "error",
				Error: &apiError{
					Code:    "NOT_STARTED",
					Message: "service not yet ready",
				},
				Data: map[string]interface{}{
					"state":        "starting",
					"fail_count":   count,
					"fail_allowed": 3,
				},
			})
			return
		}
	}

	h.startupOnce.Do(func() {
		h.startupLatched.Store(true)
	})
	writeJSON(w, http.StatusOK, apiResponse{
		Status: "success",
		Data:   map[string]string{"state": "started"},
	})
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	h.Ready(w, r)
}

func (h *HealthHandler) CheckStatus(ctx context.Context) (state string, httpStatus int, details interface{}) {
	data, status := h.runReadyCheck(ctx)
	return data.State, status, data
}
