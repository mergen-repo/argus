package sba

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

type NFProfile struct {
	NFInstanceID string   `json:"nfInstanceId"`
	NFType       string   `json:"nfType"`
	NFStatus     string   `json:"nfStatus"`
	FQDN         string   `json:"fqdn,omitempty"`
	IPAddresses  []string `json:"ipv4Addresses,omitempty"`
	NFServices   []NFService `json:"nfServices,omitempty"`
	PlmnList     []PlmnID `json:"plmnList,omitempty"`
	AllowedNSSAI []SNSSAI `json:"allowedNssais,omitempty"`
}

type NFService struct {
	ServiceInstanceID string `json:"serviceInstanceId"`
	ServiceName       string `json:"serviceName"`
	Version           string `json:"version"`
	Scheme            string `json:"scheme"`
	Status            string `json:"nfServiceStatus"`
}

type NRFConfig struct {
	NRFURL        string `json:"nrf_url,omitempty"`
	NFInstanceID  string `json:"nf_instance_id,omitempty"`
	HeartbeatSec  int    `json:"heartbeat_sec,omitempty"`
}

type NRFRegistration struct {
	config  NRFConfig
	profile NFProfile
	logger  zerolog.Logger
}

func NewNRFRegistration(cfg NRFConfig, logger zerolog.Logger) *NRFRegistration {
	return &NRFRegistration{
		config: cfg,
		profile: NFProfile{
			NFInstanceID: cfg.NFInstanceID,
			NFType:       "AUSF",
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
		},
		logger: logger.With().Str("component", "sba_nrf").Logger(),
	}
}

func (n *NRFRegistration) Register() error {
	n.logger.Info().
		Str("nf_instance_id", n.config.NFInstanceID).
		Str("nrf_url", n.config.NRFURL).
		Msg("NRF registration placeholder — not yet connected to external NRF")
	return nil
}

func (n *NRFRegistration) Deregister() error {
	n.logger.Info().
		Str("nf_instance_id", n.config.NFInstanceID).
		Msg("NRF deregistration placeholder")
	return nil
}

func (n *NRFRegistration) Heartbeat() error {
	n.logger.Debug().
		Str("nf_instance_id", n.config.NFInstanceID).
		Msg("NRF heartbeat placeholder")
	return nil
}

func (n *NRFRegistration) GetProfile() NFProfile {
	return n.profile
}

func (n *NRFRegistration) HandleNFStatusNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is supported")
		return
	}

	var notification struct {
		Event       string    `json:"event"`
		NFInstanceID string   `json:"nfInstanceId"`
		NFStatus    string    `json:"nfStatus"`
		Timestamp   time.Time `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid notification body")
		return
	}

	n.logger.Info().
		Str("event", notification.Event).
		Str("nf_instance_id", notification.NFInstanceID).
		Str("nf_status", notification.NFStatus).
		Msg("NRF status notification received (placeholder)")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

func (n *NRFRegistration) HandleNFDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is supported")
		return
	}

	result := struct {
		ValidityPeriod int         `json:"validityPeriod"`
		NFInstances    []NFProfile `json:"nfInstances"`
	}{
		ValidityPeriod: 3600,
		NFInstances:    []NFProfile{n.profile},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}
