//go:build integration
// +build integration

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	opSimBaseURL            string
	opSimMetricsURL         string
	opSimArgusMetricsURL    string
	opSimSuperAdminToken    string
	opSimHealthCheckerDelay = 45 * time.Second
)

var opSimRealOperatorCodes = []string{"turkcell", "vodafone_tr", "turk_telekom"}

type opSimOperator struct {
	ID               string   `json:"id"`
	Code             string   `json:"code"`
	Name             string   `json:"name"`
	EnabledProtocols []string `json:"enabled_protocols"`
}

type opSimTestResp struct {
	Success   bool   `json:"success"`
	LatencyMs int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

var opSimResolvedOperators []opSimOperator

func TestOperatorSimE2E(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("skipping operator-sim E2E test; set E2E=1 to run")
	}

	opSimBaseURL = envOrDefault("E2E_BASE_URL", "http://localhost:8080")
	opSimArgusMetricsURL = envOrDefault("E2E_ARGUS_METRICS_URL", opSimBaseURL+"/metrics")
	opSimMetricsURL = envOrDefault("E2E_OPERATOR_SIM_METRICS_URL", "http://localhost:9596/-/metrics")

	start := time.Now()

	t.Run("Step01_SuperAdminLogin", opSimSuperAdminLogin)
	t.Run("Step02_ResolveRealOperators", opSimResolveRealOperators)
	t.Run("Step03_AC7_PerProtocolHTTPTestRoundTrip", opSimAC7PerProtocolRoundTrip)
	t.Run("Step04_AC8_EnabledProtocolsContainsHTTP", opSimAC8EnabledProtocolsHTTP)
	t.Run("Step05_WaitForHealthChecker", opSimWaitForHealthChecker)
	t.Run("Step06_AC9_HealthCheckerGaugePopulated", opSimAC9HealthCheckerGauge)
	t.Run("Step07_AC11_OperatorSimMetricsPopulated", opSimAC11OperatorSimMetrics)

	t.Logf("operator-sim E2E completed in %s", time.Since(start))
}

func opSimSuperAdminLogin(t *testing.T) {
	body := `{"email":"admin@argus.io","password":"admin"}`
	resp := opSimPost(t, "/api/v1/auth/login", body, "")
	require.Equal(t, http.StatusOK, resp.StatusCode, "super_admin login should return 200")

	var apiResp apiResponse
	opSimParseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var loginData struct {
		Token string `json:"token"`
		User  struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &loginData))
	require.NotEmpty(t, loginData.Token)
	require.Equal(t, "super_admin", loginData.User.Role)

	opSimSuperAdminToken = loginData.Token
	t.Logf("super_admin login OK")
}

func opSimResolveRealOperators(t *testing.T) {
	require.NotEmpty(t, opSimSuperAdminToken, "super_admin token required")

	resp := opSimGet(t, "/api/v1/operators?limit=100", opSimSuperAdminToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var listResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	opSimParseResponse(t, resp, &listResp)
	require.Equal(t, "success", listResp.Status)

	var ops []opSimOperator
	require.NoError(t, json.Unmarshal(listResp.Data, &ops))

	byCode := make(map[string]opSimOperator, len(ops))
	for _, o := range ops {
		byCode[o.Code] = o
	}

	resolved := make([]opSimOperator, 0, len(opSimRealOperatorCodes))
	for _, code := range opSimRealOperatorCodes {
		op, ok := byCode[code]
		require.Truef(t, ok, "seed 003 operator with code=%s must exist", code)
		require.NotEmpty(t, op.ID)
		resolved = append(resolved, op)
	}

	opSimResolvedOperators = resolved
	t.Logf("resolved %d real operators from seed 003", len(resolved))
}

func opSimAC7PerProtocolRoundTrip(t *testing.T) {
	require.Len(t, opSimResolvedOperators, len(opSimRealOperatorCodes))

	for _, op := range opSimResolvedOperators {
		t.Run(op.Code, func(t *testing.T) {
			path := fmt.Sprintf("/api/v1/operators/%s/test/http", op.ID)
			resp := opSimPost(t, path, "{}", opSimSuperAdminToken)
			require.Equalf(t, http.StatusOK, resp.StatusCode,
				"AC-7: POST %s must return 200 for operator=%s", path, op.Code)

			var apiResp apiResponse
			opSimParseResponse(t, resp, &apiResp)
			require.Equal(t, "success", apiResp.Status)

			var tr opSimTestResp
			require.NoError(t, json.Unmarshal(apiResp.Data, &tr))

			assert.Truef(t, tr.Success,
				"AC-7: success must be true for operator=%s protocol=http (error=%q)", op.Code, tr.Error)
			assert.Greaterf(t, tr.LatencyMs, 0,
				"AC-7: latency_ms must be populated (>0) for operator=%s", op.Code)
			assert.Lessf(t, tr.LatencyMs, 500,
				"AC-7: latency_ms must be < 500 for operator=%s (got %d)", op.Code, tr.LatencyMs)

			t.Logf("AC-7 operator=%s protocol=http success=%v latency_ms=%d",
				op.Code, tr.Success, tr.LatencyMs)
		})
	}
}

func opSimAC8EnabledProtocolsHTTP(t *testing.T) {
	require.Len(t, opSimResolvedOperators, len(opSimRealOperatorCodes))

	for _, op := range opSimResolvedOperators {
		t.Run(op.Code, func(t *testing.T) {
			resp := opSimGet(t, "/api/v1/operators/"+op.ID, opSimSuperAdminToken)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			var apiResp apiResponse
			opSimParseResponse(t, resp, &apiResp)
			require.Equal(t, "success", apiResp.Status)

			var single opSimOperator
			require.NoError(t, json.Unmarshal(apiResp.Data, &single))

			assert.Containsf(t, single.EnabledProtocols, "http",
				"AC-8: enabled_protocols must include http for operator=%s (got %v)",
				op.Code, single.EnabledProtocols)

			t.Logf("AC-8 operator=%s enabled_protocols=%v", op.Code, single.EnabledProtocols)
		})
	}
}

func opSimWaitForHealthChecker(t *testing.T) {
	if v := os.Getenv("E2E_HEALTHCHECKER_DELAY_SECONDS"); v != "" {
		var sec int
		if _, err := fmt.Sscanf(v, "%d", &sec); err == nil && sec >= 0 {
			opSimHealthCheckerDelay = time.Duration(sec) * time.Second
		}
	}
	t.Logf("waiting %s for HealthChecker tick to populate gauges", opSimHealthCheckerDelay)
	time.Sleep(opSimHealthCheckerDelay)
}

func opSimAC9HealthCheckerGauge(t *testing.T) {
	require.Len(t, opSimResolvedOperators, len(opSimRealOperatorCodes))

	families := opSimScrapeMetrics(t, opSimArgusMetricsURL)
	family, ok := families["argus_operator_adapter_health_status"]
	require.Truef(t, ok,
		"AC-9: metric argus_operator_adapter_health_status must be exposed at %s", opSimArgusMetricsURL)

	for _, op := range opSimResolvedOperators {
		t.Run(op.Code, func(t *testing.T) {
			var found bool
			var value float64
			for _, m := range family.GetMetric() {
				if !opSimLabelsMatch(m.GetLabel(), map[string]string{
					"operator_id": op.ID,
					"protocol":    "http",
				}) {
					continue
				}
				found = true
				value = m.GetGauge().GetValue()
				break
			}
			require.Truef(t, found,
				"AC-9: gauge sample missing for operator_id=%s protocol=http", op.ID)
			assert.GreaterOrEqualf(t, value, 1.0,
				"AC-9: gauge must be populated (>=1; semantics 1=degraded, 2=healthy) for operator=%s (got %v)",
				op.Code, value)

			t.Logf("AC-9 operator=%s gauge=%v", op.Code, value)
		})
	}
}

func opSimAC11OperatorSimMetrics(t *testing.T) {
	families := opSimScrapeMetrics(t, opSimMetricsURL)

	reqTotal, ok := families["operator_sim_requests_total"]
	require.Truef(t, ok,
		"AC-11: operator_sim_requests_total must be exposed at %s", opSimMetricsURL)

	var turkcellHits float64
	for _, m := range reqTotal.GetMetric() {
		if opSimLabelValue(m.GetLabel(), "operator") != "turkcell" {
			continue
		}
		turkcellHits += m.GetCounter().GetValue()
	}
	assert.Greaterf(t, turkcellHits, 0.0,
		"AC-11: operator_sim_requests_total{operator=\"turkcell\",...} must be > 0 (got %v)", turkcellHits)

	durHist, ok := families["operator_sim_request_duration_seconds"]
	require.Truef(t, ok,
		"AC-11: operator_sim_request_duration_seconds histogram must be exposed at %s", opSimMetricsURL)

	var histSampleCount uint64
	var anyBucketNonZero bool
	for _, m := range durHist.GetMetric() {
		h := m.GetHistogram()
		histSampleCount += h.GetSampleCount()
		for _, b := range h.GetBucket() {
			if b.GetCumulativeCount() > 0 {
				anyBucketNonZero = true
			}
		}
	}
	assert.Greaterf(t, histSampleCount, uint64(0),
		"AC-11: operator_sim_request_duration_seconds must have > 0 observations")
	assert.Truef(t, anyBucketNonZero,
		"AC-11: at least one bucket of operator_sim_request_duration_seconds_bucket must be non-zero")

	t.Logf("AC-11 turkcell_requests_total=%v duration_sample_count=%d", turkcellHits, histSampleCount)
}

func opSimGet(t *testing.T, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", opSimBaseURL+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func opSimPost(t *testing.T, path, body, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", opSimBaseURL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func opSimParseResponse(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, v), "response body: %s", string(body))
}

func opSimScrapeMetrics(t *testing.T, url string) map[string]*dto.MetricFamily {
	t.Helper()

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoErrorf(t, err, "scrape %s", url)
	defer resp.Body.Close()
	require.Equalf(t, http.StatusOK, resp.StatusCode, "scrape %s must return 200", url)

	var parser expfmt.TextParser
	mf, err := parser.TextToMetricFamilies(resp.Body)
	require.NoErrorf(t, err, "parse prometheus exposition from %s", url)
	return mf
}

func opSimLabelsMatch(labels []*dto.LabelPair, want map[string]string) bool {
	for k, v := range want {
		if opSimLabelValue(labels, k) != v {
			return false
		}
	}
	return true
}

func opSimLabelValue(labels []*dto.LabelPair, name string) string {
	for _, l := range labels {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
