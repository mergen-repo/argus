package system

import (
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/config"
)

type ConfigHandler struct {
	cfg       *config.Config
	version   string
	gitSHA    string
	buildTime string
	startedAt time.Time
}

func NewConfigHandler(cfg *config.Config, version, gitSHA, buildTime string) *ConfigHandler {
	return &ConfigHandler{
		cfg:       cfg,
		version:   version,
		gitSHA:    gitSHA,
		buildTime: buildTime,
		startedAt: time.Now(),
	}
}

type configResponse struct {
	config.RedactedConfig
	Version   string    `json:"version"`
	GitSHA    string    `json:"git_sha"`
	BuildTime string    `json:"build_time"`
	StartedAt time.Time `json:"started_at"`
}

func (h *ConfigHandler) Serve(w http.ResponseWriter, r *http.Request) {
	payload := configResponse{
		RedactedConfig: h.cfg.Redact(),
		Version:        h.version,
		GitSHA:         h.gitSHA,
		BuildTime:      h.buildTime,
		StartedAt:      h.startedAt,
	}
	apierr.WriteSuccess(w, http.StatusOK, payload)
}
