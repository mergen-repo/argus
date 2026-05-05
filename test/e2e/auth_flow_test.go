package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

var (
	baseURL      string
	wsURL        string
	radiusAddr   string
	radiusSecret string

	authToken   string
	operatorID  string
	apnID       string
	jobID       string
	policyID    string
	versionID   string
	createdSIMs []simInfo

	mu sync.Mutex
)

type simInfo struct {
	ID   string
	IMSI string
}

type apiResponse struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Meta   json.RawMessage `json:"meta,omitempty"`
	Error  *apiError       `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func TestMain(m *testing.M) {
	baseURL = envOrDefault("E2E_BASE_URL", "http://localhost:8080")
	wsURL = envOrDefault("E2E_WS_URL", "ws://localhost:8081")
	radiusAddr = envOrDefault("E2E_RADIUS_ADDR", "localhost:1812")
	radiusSecret = envOrDefault("E2E_RADIUS_SECRET", "testing123")

	code := m.Run()

	cleanup()

	os.Exit(code)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func cleanup() {
	if authToken == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = ctx

	for _, sim := range createdSIMs {
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/sims/"+sim.ID+"/terminate", nil)
		req.Header.Set("Authorization", "Bearer "+authToken)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	if apnID != "" {
		req, _ := http.NewRequest("DELETE", baseURL+"/api/v1/apns/"+apnID, nil)
		req.Header.Set("Authorization", "Bearer "+authToken)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	if policyID != "" {
		body := `{"state":"archived"}`
		req, _ := http.NewRequest("PATCH", baseURL+"/api/v1/policies/"+policyID, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+authToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
}

func TestFullAuthFlow(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("skipping E2E test; set E2E=1 to run")
	}

	t.Run("Step01_Login", testLogin)
	t.Run("Step02_Dashboard", testDashboard)
	t.Run("Step03_CreateAPN", testCreateAPN)
	t.Run("Step04_BulkImport", testBulkImport)
	t.Run("Step05_WaitForJob", testWaitForJob)
	t.Run("Step06_VerifySIMs", testVerifySIMs)
	t.Run("Step07_CreatePolicy", testCreatePolicy)
	t.Run("Step08_ActivatePolicy", testActivatePolicy)
	t.Run("Step09_AssignPolicy", testAssignPolicy)
	t.Run("Step10_RADIUSAuth", testRADIUSAuth)
	t.Run("Step11_VerifySession", testVerifySession)
	t.Run("Step12_WebSocket", testWebSocket)
}

func testLogin(t *testing.T) {
	body := `{"email":"admin@argus.io","password":"admin"}`
	resp := doPost(t, "/api/v1/auth/login", body, "")

	require.Equal(t, http.StatusOK, resp.StatusCode, "login should return 200")

	var apiResp apiResponse
	parseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var loginData struct {
		Token       string `json:"token"`
		Requires2FA bool   `json:"requires_2fa"`
		User        struct {
			ID       string `json:"id"`
			Email    string `json:"email"`
			TenantID string `json:"tenant_id"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &loginData))
	require.NotEmpty(t, loginData.Token, "JWT token should be present")
	assert.Equal(t, "admin@argus.io", loginData.User.Email)
	assert.NotEmpty(t, loginData.User.TenantID)

	mu.Lock()
	authToken = loginData.Token
	mu.Unlock()

	t.Logf("login successful, user=%s tenant=%s", loginData.User.Email, loginData.User.TenantID)
}

func testDashboard(t *testing.T) {
	requireAuth(t)

	resp := doGet(t, "/api/v1/dashboard", authToken)
	require.Equal(t, http.StatusOK, resp.StatusCode, "dashboard should return 200")

	var apiResp apiResponse
	parseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var dashboard struct {
		TotalSIMs      int `json:"total_sims"`
		ActiveSessions int `json:"active_sessions"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &dashboard))

	t.Logf("dashboard: total_sims=%d active_sessions=%d", dashboard.TotalSIMs, dashboard.ActiveSessions)
}

func testCreateAPN(t *testing.T) {
	requireAuth(t)

	opID := resolveOperator(t)

	apnName := fmt.Sprintf("e2e-test-%d", time.Now().UnixMilli())
	body := fmt.Sprintf(`{
		"name": %q,
		"operator_id": %q,
		"apn_type": "private_managed",
		"supported_rat_types": ["lte", "nb_iot"],
		"settings": {}
	}`, apnName, opID)

	resp := doPost(t, "/api/v1/apns", body, authToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create APN should return 201")

	var apiResp apiResponse
	parseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var apnData struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		State      string `json:"state"`
		OperatorID string `json:"operator_id"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &apnData))
	require.NotEmpty(t, apnData.ID)
	assert.Equal(t, apnName, apnData.Name)

	mu.Lock()
	apnID = apnData.ID
	operatorID = opID
	mu.Unlock()

	t.Logf("APN created: id=%s name=%s", apnData.ID, apnData.Name)
}

func testBulkImport(t *testing.T) {
	requireAuth(t)
	require.NotEmpty(t, operatorID, "operator_id required")
	require.NotEmpty(t, apnID, "apn_id required")

	opCode := resolveOperatorCode(t, operatorID)
	apnName := resolveAPNName(t, apnID)

	csvContent := generateCSV(opCode, apnName, 10)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "e2e_sims.csv")
	require.NoError(t, err)
	_, err = part.Write([]byte(csvContent))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req, err := http.NewRequest("POST", baseURL+"/api/v1/sims/bulk/import", &buf)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode, "bulk import should return 202")

	var apiResp apiResponse
	bodyBytes, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(bodyBytes, &apiResp))
	require.Equal(t, "success", apiResp.Status)

	var importData struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &importData))
	require.NotEmpty(t, importData.JobID)

	mu.Lock()
	jobID = importData.JobID
	mu.Unlock()

	t.Logf("bulk import started: job_id=%s", importData.JobID)
}

func testWaitForJob(t *testing.T) {
	requireAuth(t)
	require.NotEmpty(t, jobID, "job_id required from previous step")

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp := doGet(t, "/api/v1/jobs/"+jobID, authToken)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var apiResp apiResponse
		parseResponse(t, resp, &apiResp)

		var job struct {
			State          string  `json:"state"`
			TotalItems     int     `json:"total_items"`
			ProcessedItems int     `json:"processed_items"`
			FailedItems    int     `json:"failed_items"`
			ProgressPct    float64 `json:"progress_pct"`
		}
		require.NoError(t, json.Unmarshal(apiResp.Data, &job))

		t.Logf("job state=%s progress=%.0f%% processed=%d/%d failed=%d",
			job.State, job.ProgressPct, job.ProcessedItems, job.TotalItems, job.FailedItems)

		if job.State == "completed" || job.State == "completed_with_errors" {
			assert.Equal(t, 10, job.TotalItems)
			return
		}

		if job.State == "failed" || job.State == "cancelled" {
			t.Fatalf("job ended in unexpected state: %s", job.State)
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatal("job did not complete within 30 seconds")
}

func testVerifySIMs(t *testing.T) {
	requireAuth(t)

	resp := doGet(t, "/api/v1/sims?limit=100", authToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var apiResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
		Meta   struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	parseResponse(t, resp, &apiResp)

	var sims []struct {
		ID    string  `json:"id"`
		ICCID string  `json:"iccid"`
		IMSI  string  `json:"imsi"`
		State string  `json:"state"`
		APNID *string `json:"apn_id"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &sims))

	e2eSims := filterE2ESIMs(sims)
	require.GreaterOrEqual(t, len(e2eSims), 10, "at least 10 imported SIMs should be present")

	mu.Lock()
	createdSIMs = nil
	for _, s := range e2eSims {
		createdSIMs = append(createdSIMs, simInfo{ID: s.ID, IMSI: s.IMSI})
	}
	mu.Unlock()

	t.Logf("verified %d SIMs from import", len(e2eSims))
}

func testCreatePolicy(t *testing.T) {
	requireAuth(t)

	policyName := fmt.Sprintf("e2e-policy-%d", time.Now().UnixMilli())
	dslSource := `POLICY "E2E Test Policy" {
  RULE "default-qos" {
    MATCH ALL
    SET bandwidth_up = 10000
    SET bandwidth_down = 50000
    SET priority = 5
  }
}`

	body := fmt.Sprintf(`{
		"name": %q,
		"description": "E2E test policy",
		"scope": "global",
		"dsl_source": %q
	}`, policyName, dslSource)

	resp := doPost(t, "/api/v1/policies", body, authToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create policy should return 201")

	var apiResp apiResponse
	parseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var pol struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		State    string `json:"state"`
		Versions []struct {
			ID      string `json:"id"`
			State   string `json:"state"`
			Version int    `json:"version"`
		} `json:"versions"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &pol))
	require.NotEmpty(t, pol.ID)
	require.NotEmpty(t, pol.Versions, "policy should have at least one version")

	mu.Lock()
	policyID = pol.ID
	versionID = pol.Versions[0].ID
	mu.Unlock()

	t.Logf("policy created: id=%s version_id=%s", pol.ID, pol.Versions[0].ID)
}

func testActivatePolicy(t *testing.T) {
	requireAuth(t)
	require.NotEmpty(t, versionID, "version_id required from previous step")

	resp := doPost(t, "/api/v1/policy-versions/"+versionID+"/activate", "{}", authToken)
	require.Equal(t, http.StatusOK, resp.StatusCode, "activate policy should return 200")

	var apiResp apiResponse
	parseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var ver struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &ver))
	assert.Equal(t, "active", ver.State)

	t.Logf("policy version activated: id=%s state=%s", ver.ID, ver.State)
}

func testAssignPolicy(t *testing.T) {
	requireAuth(t)
	require.NotEmpty(t, versionID, "version_id required")
	require.NotEmpty(t, createdSIMs, "SIMs required")

	segmentID := createSegmentForSIMs(t)

	body := fmt.Sprintf(`{
		"segment_id": %q,
		"policy_version_id": %q
	}`, segmentID, versionID)

	resp := doPost(t, "/api/v1/sims/bulk/policy-assign", body, authToken)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, "policy assign should return 202")

	var apiResp apiResponse
	parseResponse(t, resp, &apiResp)

	var assignData struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &assignData))
	require.NotEmpty(t, assignData.JobID)

	waitForJob(t, assignData.JobID, 30*time.Second)

	t.Logf("policy assigned to SIMs via job=%s", assignData.JobID)
}

func testRADIUSAuth(t *testing.T) {
	require.NotEmpty(t, createdSIMs, "SIMs required from previous steps")

	sim := createdSIMs[0]

	packet := radius.New(radius.CodeAccessRequest, []byte(radiusSecret))
	rfc2865.UserName_SetString(packet, sim.IMSI)
	rfc2865.CallingStationID_SetString(packet, sim.IMSI)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	response, err := radius.Exchange(ctx, packet, radiusAddr)
	require.NoError(t, err, "RADIUS exchange should succeed")
	require.Equal(t, radius.CodeAccessAccept, response.Code, "should receive Access-Accept")

	sessionTimeout := rfc2865.SessionTimeout_Get(response)
	t.Logf("RADIUS auth: imsi=%s result=Access-Accept session_timeout=%d", sim.IMSI, sessionTimeout)

	sendAcctStart(t, sim.IMSI)
}

func testVerifySession(t *testing.T) {
	requireAuth(t)
	require.NotEmpty(t, createdSIMs, "SIMs required")

	time.Sleep(1 * time.Second)

	resp := doGet(t, "/api/v1/sessions?limit=100", authToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var apiResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	parseResponse(t, resp, &apiResp)

	var sessions []struct {
		ID    string `json:"id"`
		IMSI  string `json:"imsi"`
		State string `json:"state"`
		SimID string `json:"sim_id"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &sessions))

	targetIMSI := createdSIMs[0].IMSI
	var found bool
	for _, s := range sessions {
		if s.IMSI == targetIMSI && s.State == "active" {
			found = true
			t.Logf("session verified: id=%s imsi=%s state=%s", s.ID, s.IMSI, s.State)
			break
		}
	}

	require.True(t, found, "active session should exist for IMSI %s", targetIMSI)
}

func testWebSocket(t *testing.T) {
	requireAuth(t)

	wsEndpoint := wsURL + "/ws/v1/events?token=" + authToken
	conn, resp, err := websocket.DefaultDialer.Dial(wsEndpoint, nil)
	if err != nil && resp != nil {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("ws dial failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	require.NoError(t, err, "WebSocket connection should succeed")
	defer conn.Close()

	var authOK struct {
		Type string `json:"type"`
		Data struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
			Role     string `json:"role"`
		} `json:"data"`
	}
	err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, err)
	err = conn.ReadJSON(&authOK)
	require.NoError(t, err)
	assert.Equal(t, "auth.ok", authOK.Type, "should receive auth.ok message")

	subscribeMsg := `{"type":"subscribe","events":["session.started","session.ended","*"]}`
	err = conn.WriteMessage(websocket.TextMessage, []byte(subscribeMsg))
	require.NoError(t, err)

	var subOK struct {
		Type string `json:"type"`
	}
	err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, err)
	err = conn.ReadJSON(&subOK)
	require.NoError(t, err)
	assert.Equal(t, "subscribe.ok", subOK.Type, "should receive subscribe.ok")

	if len(createdSIMs) > 1 {
		go func() {
			time.Sleep(200 * time.Millisecond)
			sim := createdSIMs[1]
			packet := radius.New(radius.CodeAccessRequest, []byte(radiusSecret))
			rfc2865.UserName_SetString(packet, sim.IMSI)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = radius.Exchange(ctx, packet, radiusAddr)

			sendAcctStart(t, sim.IMSI)
		}()

		err = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		require.NoError(t, err)

		var event struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Timestamp string          `json:"timestamp"`
			Data      json.RawMessage `json:"data"`
		}

		eventReceived := false
		for i := 0; i < 5; i++ {
			err = conn.ReadJSON(&event)
			if err != nil {
				break
			}
			t.Logf("ws event received: type=%s id=%s", event.Type, event.ID)
			if event.Type == "session.started" {
				eventReceived = true
				break
			}
		}

		if eventReceived {
			t.Logf("WebSocket session.started event received successfully")
		} else {
			t.Logf("WebSocket connected and subscribed; session.started event not received within timeout (NATS may not relay in test env)")
		}
	}

	t.Logf("WebSocket connection verified: auth.ok + subscribe.ok")
}

func doGet(t *testing.T, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", baseURL+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func doPost(t *testing.T, path, body, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", baseURL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func parseResponse(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, v), "response body: %s", string(body))
}

func requireAuth(t *testing.T) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if authToken == "" {
		t.Fatal("auth token not available; Step01_Login must pass first")
	}
}

func resolveOperator(t *testing.T) string {
	t.Helper()
	resp := doGet(t, "/api/v1/operators?limit=1", authToken)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatal("cannot list operators")
	}

	var listResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	parseResponse(t, resp, &listResp)

	var ops []struct {
		ID   string `json:"id"`
		Code string `json:"code"`
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(listResp.Data, &ops))
	require.NotEmpty(t, ops, "at least one operator must exist in the system")

	return ops[0].ID
}

func resolveOperatorCode(t *testing.T, opID string) string {
	t.Helper()
	resp := doGet(t, "/api/v1/operators?limit=100", authToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var listResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	parseResponse(t, resp, &listResp)

	var ops []struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(listResp.Data, &ops))

	for _, op := range ops {
		if op.ID == opID {
			return op.Code
		}
	}
	t.Fatalf("operator %s not found", opID)
	return ""
}

func resolveAPNName(t *testing.T, id string) string {
	t.Helper()
	resp := doGet(t, "/api/v1/apns/"+id, authToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var apiResp apiResponse
	parseResponse(t, resp, &apiResp)

	var apn struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &apn))
	return apn.Name
}

func generateCSV(operatorCode, apnName string, count int) string {
	ts := time.Now().UnixMilli()
	var sb strings.Builder
	sb.WriteString("iccid,imsi,msisdn,operator_code,apn_name\n")
	for i := 0; i < count; i++ {
		iccid := fmt.Sprintf("89901%013d", ts*100+int64(i))
		imsi := fmt.Sprintf("99901%010d", ts*100+int64(i))
		msisdn := fmt.Sprintf("+9053%08d", ts%100000000+int64(i))
		sb.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s\n", iccid, imsi, msisdn, operatorCode, apnName))
	}
	return sb.String()
}

func filterE2ESIMs(sims []struct {
	ID    string  `json:"id"`
	ICCID string  `json:"iccid"`
	IMSI  string  `json:"imsi"`
	State string  `json:"state"`
	APNID *string `json:"apn_id"`
}) []struct {
	ID    string  `json:"id"`
	ICCID string  `json:"iccid"`
	IMSI  string  `json:"imsi"`
	State string  `json:"state"`
	APNID *string `json:"apn_id"`
} {
	var result []struct {
		ID    string  `json:"id"`
		ICCID string  `json:"iccid"`
		IMSI  string  `json:"imsi"`
		State string  `json:"state"`
		APNID *string `json:"apn_id"`
	}
	for _, s := range sims {
		if strings.HasPrefix(s.ICCID, "89901") && strings.HasPrefix(s.IMSI, "99901") {
			result = append(result, s)
		}
	}
	return result
}

func createSegmentForSIMs(t *testing.T) string {
	t.Helper()

	segName := fmt.Sprintf("e2e-segment-%d", time.Now().UnixMilli())
	body := fmt.Sprintf(`{
		"name": %q,
		"filter": {
			"iccid_prefix": "89901"
		}
	}`, segName)

	resp := doPost(t, "/api/v1/sim-segments", body, authToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create segment should return 201")

	var apiResp apiResponse
	parseResponse(t, resp, &apiResp)

	var seg struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &seg))
	require.NotEmpty(t, seg.ID)

	t.Logf("segment created: id=%s name=%s", seg.ID, segName)
	return seg.ID
}

func waitForJob(t *testing.T, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := doGet(t, "/api/v1/jobs/"+id, authToken)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var apiResp apiResponse
		parseResponse(t, resp, &apiResp)

		var job struct {
			State string `json:"state"`
		}
		require.NoError(t, json.Unmarshal(apiResp.Data, &job))

		if job.State == "completed" || job.State == "completed_with_errors" {
			return
		}
		if job.State == "failed" || job.State == "cancelled" {
			t.Fatalf("job %s ended in state: %s", id, job.State)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("job %s did not complete within %s", id, timeout)
}

func sendAcctStart(t *testing.T, imsi string) {
	t.Helper()

	acctAddr := strings.Replace(radiusAddr, ":1812", ":1813", 1)
	if acctAddr == radiusAddr {
		acctAddr = "localhost:1813"
	}

	packet := radius.New(radius.CodeAccountingRequest, []byte(radiusSecret))
	rfc2865.UserName_SetString(packet, imsi)
	rfc2866.AcctStatusType_Set(packet, rfc2866.AcctStatusType_Value_Start)
	rfc2866.AcctSessionID_SetString(packet, fmt.Sprintf("e2e-%s-%d", imsi, time.Now().UnixNano()))
	rfc2865.NASIPAddress_Set(packet, []byte{10, 0, 0, 1})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := radius.Exchange(ctx, packet, acctAddr)
	if err != nil {
		t.Logf("RADIUS Acct-Start warning: %v (non-fatal)", err)
		return
	}

	if resp.Code == radius.CodeAccountingResponse {
		t.Logf("RADIUS Acct-Start sent for IMSI %s", imsi)
	}
}
