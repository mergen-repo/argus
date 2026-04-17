package system

import (
	"context"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
)

type HealthStatusChecker interface {
	CheckStatus(ctx context.Context) (state string, httpStatus int, details interface{})
}

type TenantCounter interface {
	CountActive(ctx context.Context) (int64, error)
}

type ErrorRateSource interface {
	Recent5xxCount() int64
}

type StatusHandler struct {
	health    HealthStatusChecker
	tenants   TenantCounter
	errSrc    ErrorRateSource
	version   string
	gitSHA    string
	buildTime string
	startedAt time.Time
}

func NewStatusHandler(
	health HealthStatusChecker,
	tenants TenantCounter,
	errSrc ErrorRateSource,
	version, gitSHA, buildTime string,
) *StatusHandler {
	return &StatusHandler{
		health:    health,
		tenants:   tenants,
		errSrc:    errSrc,
		version:   version,
		gitSHA:    gitSHA,
		buildTime: buildTime,
		startedAt: time.Now(),
	}
}

type statusData struct {
	Service        string `json:"service"`
	Overall        string `json:"overall"`
	Version        string `json:"version"`
	GitSHA         string `json:"git_sha"`
	BuildTime      string `json:"build_time"`
	Uptime         string `json:"uptime"`
	ActiveTenants  int64  `json:"active_tenants"`
	RecentError5m  int64  `json:"recent_error_5m"`
}

type statusResponseWithMeta struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data"`
	Meta   interface{} `json:"meta,omitempty"`
}

type statusMeta struct {
	Details interface{} `json:"details,omitempty"`
}

func (h *StatusHandler) Serve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	state, httpStatus, _ := h.health.CheckStatus(ctx)

	overall := state
	if overall == "" {
		overall = "unhealthy"
	}

	uptime := time.Since(h.startedAt).Round(time.Second).String()

	var activeTenants int64
	if h.tenants != nil {
		if count, err := h.tenants.CountActive(ctx); err == nil {
			activeTenants = count
		}
	}

	var recentError5m int64
	if h.errSrc != nil {
		recentError5m = h.errSrc.Recent5xxCount()
	}

	data := statusData{
		Service:       "argus",
		Overall:       overall,
		Version:       h.version,
		GitSHA:        h.gitSHA,
		BuildTime:     h.buildTime,
		Uptime:        uptime,
		ActiveTenants: activeTenants,
		RecentError5m: recentError5m,
	}

	code := http.StatusOK
	if httpStatus == http.StatusServiceUnavailable {
		code = http.StatusServiceUnavailable
	}

	apierr.WriteSuccess(w, code, data)
}

func (h *StatusHandler) ServeDetails(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	state, httpStatus, details := h.health.CheckStatus(ctx)

	overall := state
	if overall == "" {
		overall = "unhealthy"
	}

	uptime := time.Since(h.startedAt).Round(time.Second).String()

	var activeTenants int64
	if h.tenants != nil {
		if count, err := h.tenants.CountActive(ctx); err == nil {
			activeTenants = count
		}
	}

	var recentError5m int64
	if h.errSrc != nil {
		recentError5m = h.errSrc.Recent5xxCount()
	}

	data := statusData{
		Service:       "argus",
		Overall:       overall,
		Version:       h.version,
		GitSHA:        h.gitSHA,
		BuildTime:     h.buildTime,
		Uptime:        uptime,
		ActiveTenants: activeTenants,
		RecentError5m: recentError5m,
	}

	meta := &statusMeta{Details: details}

	code := http.StatusOK
	if httpStatus == http.StatusServiceUnavailable {
		code = http.StatusServiceUnavailable
	}

	apierr.WriteJSON(w, code, statusResponseWithMeta{
		Status: "success",
		Data:   data,
		Meta:   meta,
	})
}

