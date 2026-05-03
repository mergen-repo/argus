package sba

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type NFProfile struct {
	NFInstanceID string      `json:"nfInstanceId"`
	NFType       string      `json:"nfType"`
	NFStatus     string      `json:"nfStatus"`
	FQDN         string      `json:"fqdn,omitempty"`
	IPAddresses  []string    `json:"ipv4Addresses,omitempty"`
	NFServices   []NFService `json:"nfServices,omitempty"`
	PlmnList     []PlmnID    `json:"plmnList,omitempty"`
	AllowedNSSAI []SNSSAI    `json:"allowedNssais,omitempty"`
}

type NFService struct {
	ServiceInstanceID string `json:"serviceInstanceId"`
	ServiceName       string `json:"serviceName"`
	Version           string `json:"version"`
	Scheme            string `json:"scheme"`
	Status            string `json:"nfServiceStatus"`
}

type NRFConfig struct {
	NRFURL       string `json:"nrf_url,omitempty"`
	NFInstanceID string `json:"nf_instance_id,omitempty"`
	NFType       string `json:"nf_type,omitempty"`
	HeartbeatSec int    `json:"heartbeat_sec,omitempty"`
}

type NRFRegistration struct {
	config       NRFConfig
	profile      NFProfile
	logger       zerolog.Logger
	http         *http.Client
	nrfURL       string
	instanceID   string
	nfType       string
	subID        string
	disabledOnce sync.Once
}

func NewNRFRegistration(cfg NRFConfig, logger zerolog.Logger) *NRFRegistration {
	nfType := cfg.NFType
	if nfType == "" {
		nfType = "AUSF"
	}
	r := &NRFRegistration{
		config:     cfg,
		logger:     logger.With().Str("component", "sba_nrf").Logger(),
		http:       &http.Client{Timeout: 5 * time.Second},
		nrfURL:     cfg.NRFURL,
		instanceID: cfg.NFInstanceID,
		nfType:     nfType,
	}
	r.profile = NFProfile{
		NFInstanceID: cfg.NFInstanceID,
		NFType:       nfType,
		NFStatus:     "REGISTERED",
		NFServices: []NFService{
			{
				ServiceInstanceID: cfg.NFInstanceID + "-nausf-auth",
				ServiceName:       "nausf-auth",
				Version:           "v1",
				Scheme:            "https",
				Status:            "REGISTERED",
			},
			{
				ServiceInstanceID: cfg.NFInstanceID + "-nudm-ueau",
				ServiceName:       "nudm-ueau",
				Version:           "v1",
				Scheme:            "https",
				Status:            "REGISTERED",
			},
		},
	}
	return r
}

func (r *NRFRegistration) nrfDisabled() bool {
	return r.nrfURL == ""
}

func (r *NRFRegistration) logDisabledOnce() {
	r.disabledOnce.Do(func() {
		r.logger.Info().
			Str("nf_instance_id", r.instanceID).
			Msg("NRF disabled (dev); skipping registration")
	})
}

func (r *NRFRegistration) RegisterCtx(ctx context.Context) error {
	if r.nrfDisabled() {
		r.logDisabledOnce()
		return nil
	}

	body, err := json.Marshal(r.profile)
	if err != nil {
		return fmt.Errorf("nrf register: marshal profile: %w", err)
	}

	url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", r.nrfURL, r.instanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nrf register: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("nrf register: http PUT: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("nrf register: unexpected status %d", resp.StatusCode)
	}

	r.logger.Info().
		Str("nf_instance_id", r.instanceID).
		Str("nf_type", r.nfType).
		Str("nrf_url", r.nrfURL).
		Int("status_code", resp.StatusCode).
		Msg("NRF registration successful")
	return nil
}

func (r *NRFRegistration) HeartbeatCtx(ctx context.Context) error {
	if r.nrfDisabled() {
		return nil
	}

	patch := []map[string]string{
		{"op": "replace", "path": "/nfStatus", "value": "REGISTERED"},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("nrf heartbeat: marshal patch: %w", err)
	}

	url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", r.nrfURL, r.instanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nrf heartbeat: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json-patch+json")

	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("nrf heartbeat: http PATCH: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("nrf heartbeat: unexpected status %d", resp.StatusCode)
	}

	r.logger.Debug().
		Str("nf_instance_id", r.instanceID).
		Int("status_code", resp.StatusCode).
		Msg("NRF heartbeat sent")
	return nil
}

func (r *NRFRegistration) DeregisterCtx(ctx context.Context) error {
	if r.nrfDisabled() {
		return nil
	}

	url := fmt.Sprintf("%s/nnrf-nfm/v1/nf-instances/%s", r.nrfURL, r.instanceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("nrf deregister: build request: %w", err)
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return fmt.Errorf("nrf deregister: http DELETE: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("nrf deregister: unexpected status %d", resp.StatusCode)
	}

	r.logger.Info().
		Str("nf_instance_id", r.instanceID).
		Msg("NRF deregistration successful")
	return nil
}

func (r *NRFRegistration) Register() error {
	return r.RegisterCtx(context.Background())
}

func (r *NRFRegistration) Heartbeat() error {
	return r.HeartbeatCtx(context.Background())
}

func (r *NRFRegistration) Deregister() error {
	return r.DeregisterCtx(context.Background())
}

func (r *NRFRegistration) GetProfile() NFProfile {
	return r.profile
}

func (r *NRFRegistration) HandleNFStatusNotify(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is supported")
		return
	}

	var notification struct {
		Event        string    `json:"event"`
		NFInstanceID string    `json:"nfInstanceId"`
		NFStatus     string    `json:"nfStatus"`
		Timestamp    time.Time `json:"timestamp"`
	}
	if err := json.NewDecoder(req.Body).Decode(&notification); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid notification body")
		return
	}

	r.logger.Info().
		Str("event", notification.Event).
		Str("nf_instance_id", notification.NFInstanceID).
		Str("nf_status", notification.NFStatus).
		Time("timestamp", notification.Timestamp).
		Msg("NRF status notification received — processing acknowledged")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

func (r *NRFRegistration) HandleNFDiscover(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is supported")
		return
	}

	result := struct {
		ValidityPeriod int         `json:"validityPeriod"`
		NFInstances    []NFProfile `json:"nfInstances"`
	}{
		ValidityPeriod: 3600,
		NFInstances:    []NFProfile{r.profile},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}
