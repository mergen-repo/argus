//go:build integration

package sba

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	argussba "github.com/btopcu/argus/internal/aaa/sba"
	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/btopcu/argus/internal/simulator/discovery"
	"github.com/btopcu/argus/internal/simulator/metrics"
	"github.com/btopcu/argus/internal/simulator/radius"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

func init() {
	metrics.MustRegister(prometheus.NewRegistry())
}

// newArgusHandlers builds a combined http.ServeMux wiring the real
// AUSFHandler and UDMHandler — same route table as internal/aaa/sba/server.go
// but without TLS, NRF registration, or session manager overhead.
//
// AUSF paths:
//   /nausf-auth/v1/ue-authentications       (POST  → HandleAuthentication)
//   /nausf-auth/v1/ue-authentications/<id>/5g-aka-confirmation  (PUT → HandleConfirmation)
//
// UDM paths:
//   /nudm-uecm/v1/<supi>/registrations/amf-3gpp-access  (PUT → HandleRegistration; DELETE → 405)
func newArgusHandlers() http.Handler {
	ausf := argussba.NewAUSFHandler(nil, nil, zerolog.Nop())
	udm := argussba.NewUDMHandler(nil, nil, zerolog.Nop())

	mux := http.NewServeMux()

	mux.HandleFunc("/nausf-auth/v1/ue-authentications", ausf.HandleAuthentication)
	mux.HandleFunc("/nausf-auth/v1/ue-authentications/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/5g-aka-confirmation") {
			ausf.HandleConfirmation(w, r)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/nudm-uecm/v1/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/registrations/") {
			udm.HandleRegistration(w, r)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	return mux
}

// TestSimulator_AgainstArgusSBA is a single end-to-end scenario:
//
//  1. Spin up in-process AUSF + UDM handlers via httptest.
//  2. Build a simulator sba.Client pointed at the test server URL.
//  3. Call client.RunSession(ctx, sc) with a 200 ms context deadline.
//  4. Assert: Authenticate succeeded, Register succeeded; RunSession returns
//     context.DeadlineExceeded (the normal end-of-hold signal, not an error).
//  5. Assert metrics incremented:
//     SBARequestsTotal{service="ausf",op="authenticate"} > 0
//     SBARequestsTotal{service="ausf",op="confirm"} > 0
//     SBARequestsTotal{service="udm",op="register"} > 0
//
// Deregister is out of scope for STORY-084 — the minimum flow is POST
// authenticate → PUT confirm → PUT register. No DELETE requests are emitted.
func TestSimulator_AgainstArgusSBA(t *testing.T) {
	const opCode = "integration-sba-op"
	const imsi = "286010000000099"

	srv := httptest.NewServer(newArgusHandlers())
	t.Cleanup(srv.Close)

	defaults := config.SBADefaults{
		Host:                 "127.0.0.1",
		Port:                 0,
		TLSEnabled:           false,
		ServingNetworkName:   "5G:mnc001.mcc286.3gppnetwork.org",
		RequestTimeout:       5 * time.Second,
		AMFInstanceID:        "integ-amf-01",
		DeregCallbackURI:     "http://integ-amf.invalid/dereg",
		IncludeOptionalCalls: false,
	}
	op := config.OperatorConfig{
		Code:          opCode,
		NASIdentifier: "integ-nas",
		NASIP:         "127.0.0.1",
		SBA: &config.OperatorSBAConfig{
			Enabled:    true,
			Rate:       1.0,
			AuthMethod: "5G_AKA",
		},
	}

	client := New(op, defaults, zerolog.Nop())
	client.baseURL = srv.URL

	msisdn := "905000000099"
	sc := &radius.SessionContext{
		SIM: discovery.SIM{
			IMSI:   imsi,
			MSISDN: &msisdn,
		},
		NASIP:         "127.0.0.1",
		NASIdentifier: "integ-nas",
		AcctSessionID: "integ-sba-sess-001",
	}

	// Capture pre-run counter values (delta pattern avoids global accumulation
	// from unit tests sharing the same package binary). Label names follow the
	// plan §Metrics contract: operator, service, endpoint for requests/responses.
	beforeAuth := testutil.ToFloat64(metrics.SBARequestsTotal.WithLabelValues(opCode, "ausf", "authenticate"))
	beforeConfirm := testutil.ToFloat64(metrics.SBARequestsTotal.WithLabelValues(opCode, "ausf", "confirm"))
	beforeRegister := testutil.ToFloat64(metrics.SBARequestsTotal.WithLabelValues(opCode, "udm", "register"))
	beforeAuthSuccess := testutil.ToFloat64(metrics.SBAResponsesTotal.WithLabelValues(opCode, "ausf", "authenticate", "success"))
	beforeConfirmSuccess := testutil.ToFloat64(metrics.SBAResponsesTotal.WithLabelValues(opCode, "ausf", "confirm", "success"))
	beforeRegisterSuccess := testutil.ToFloat64(metrics.SBAResponsesTotal.WithLabelValues(opCode, "udm", "register", "success"))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := client.RunSession(ctx, sc)

	// RunSession blocks until ctx expires and returns ctx.Err() ==
	// context.DeadlineExceeded on the happy path.
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunSession: unexpected error %v", err)
	}

	// Request-counter assertions: each counter must have incremented by at least 1.
	afterAuth := testutil.ToFloat64(metrics.SBARequestsTotal.WithLabelValues(opCode, "ausf", "authenticate"))
	afterConfirm := testutil.ToFloat64(metrics.SBARequestsTotal.WithLabelValues(opCode, "ausf", "confirm"))
	afterRegister := testutil.ToFloat64(metrics.SBARequestsTotal.WithLabelValues(opCode, "udm", "register"))

	if delta := afterAuth - beforeAuth; delta <= 0 {
		t.Errorf("SBARequestsTotal{ausf,authenticate}: delta=%v, want >0", delta)
	}
	if delta := afterConfirm - beforeConfirm; delta <= 0 {
		t.Errorf("SBARequestsTotal{ausf,confirm}: delta=%v, want >0", delta)
	}
	if delta := afterRegister - beforeRegister; delta <= 0 {
		t.Errorf("SBARequestsTotal{udm,register}: delta=%v, want >0", delta)
	}

	// Response success-counter assertions (plan AC-2).
	afterAuthSuccess := testutil.ToFloat64(metrics.SBAResponsesTotal.WithLabelValues(opCode, "ausf", "authenticate", "success"))
	afterConfirmSuccess := testutil.ToFloat64(metrics.SBAResponsesTotal.WithLabelValues(opCode, "ausf", "confirm", "success"))
	afterRegisterSuccess := testutil.ToFloat64(metrics.SBAResponsesTotal.WithLabelValues(opCode, "udm", "register", "success"))

	if delta := afterAuthSuccess - beforeAuthSuccess; delta <= 0 {
		t.Errorf("SBAResponsesTotal{ausf,authenticate,success}: delta=%v, want >0", delta)
	}
	if delta := afterConfirmSuccess - beforeConfirmSuccess; delta <= 0 {
		t.Errorf("SBAResponsesTotal{ausf,confirm,success}: delta=%v, want >0", delta)
	}
	if delta := afterRegisterSuccess - beforeRegisterSuccess; delta <= 0 {
		t.Errorf("SBAResponsesTotal{udm,register,success}: delta=%v, want >0", delta)
	}
}

// TestSimulator_MandatoryIE_Negative exercises the plan Task 8 negative case:
// sending Authenticate with an empty serving_network_name causes the real
// Argus AUSF handler to return 400 MANDATORY_IE_INCORRECT, which the simulator
// must surface as ErrAuthFailed AND increment
// SBAServiceErrorsTotal{cause="MANDATORY_IE_INCORRECT"}.
//
// This test is the flip-side of the plan §Metrics `cause` label contract:
// consumers building PromQL on ProblemDetails.Cause enums depend on this path.
func TestSimulator_MandatoryIE_Negative(t *testing.T) {
	const opCode = "integration-sba-mie-op"

	srv := httptest.NewServer(newArgusHandlers())
	t.Cleanup(srv.Close)

	defaults := config.SBADefaults{
		Host:               "127.0.0.1",
		Port:               0,
		TLSEnabled:         false,
		ServingNetworkName: "5G:mnc001.mcc286.3gppnetwork.org",
		RequestTimeout:     5 * time.Second,
		AMFInstanceID:      "integ-amf-mie",
		DeregCallbackURI:   "http://integ-amf.invalid/dereg",
	}
	op := config.OperatorConfig{
		Code:          opCode,
		NASIdentifier: "integ-nas",
		NASIP:         "127.0.0.1",
		SBA: &config.OperatorSBAConfig{
			Enabled:    true,
			Rate:       1.0,
			AuthMethod: "5G_AKA",
		},
	}

	client := New(op, defaults, zerolog.Nop())
	client.baseURL = srv.URL
	client.servingNetworkName = "" // trigger MANDATORY_IE_INCORRECT in server

	before := testutil.ToFloat64(metrics.SBAServiceErrorsTotal.WithLabelValues(opCode, "ausf", "MANDATORY_IE_INCORRECT"))
	beforeResponse := testutil.ToFloat64(metrics.SBAResponsesTotal.WithLabelValues(opCode, "ausf", "authenticate", "error_4xx"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.AuthenticateViaAUSF(ctx, "286010123456789", "")
	if err == nil {
		t.Fatal("AuthenticateViaAUSF: expected error for empty servingNetworkName, got nil")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("expected ErrAuthFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "MANDATORY_IE_INCORRECT") {
		t.Errorf("error should surface cause MANDATORY_IE_INCORRECT, got: %v", err)
	}

	after := testutil.ToFloat64(metrics.SBAServiceErrorsTotal.WithLabelValues(opCode, "ausf", "MANDATORY_IE_INCORRECT"))
	if delta := after - before; delta <= 0 {
		t.Errorf("SBAServiceErrorsTotal{cause=MANDATORY_IE_INCORRECT}: delta=%v, want >0", delta)
	}

	afterResponse := testutil.ToFloat64(metrics.SBAResponsesTotal.WithLabelValues(opCode, "ausf", "authenticate", "error_4xx"))
	if delta := afterResponse - beforeResponse; delta <= 0 {
		t.Errorf("SBAResponsesTotal{result=error_4xx}: delta=%v, want >0", delta)
	}
}
