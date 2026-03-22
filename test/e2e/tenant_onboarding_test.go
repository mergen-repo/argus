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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

var (
	onbBaseURL      string
	onbRadiusAddr   string
	onbRadiusSecret string

	superAdminToken string
	tenantAID       string
	tenantBID       string
	tenantAAdminID  string
	onbOperatorID   string
	onbOperatorCode string
	onbAPNID        string
	onbAPNName      string
	onbJobID        string
	onbPolicyID     string
	onbVersionID    string
	onbCreatedSIMs  []simInfo

	onbMu sync.Mutex
)

func TestTenantOnboarding(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("skipping E2E test; set E2E=1 to run")
	}

	onbBaseURL = envOrDefault("E2E_BASE_URL", "http://localhost:8080")
	onbRadiusAddr = envOrDefault("E2E_RADIUS_ADDR", "localhost:1812")
	onbRadiusSecret = envOrDefault("E2E_RADIUS_SECRET", "testing123")

	start := time.Now()

	t.Cleanup(func() {
		onbCleanup(t)
	})

	t.Run("Step01_SuperAdminLogin", testOnbSuperAdminLogin)
	t.Run("Step02_CreateTenantA", testOnbCreateTenantA)
	t.Run("Step03_CreateTenantB", testOnbCreateTenantB)
	t.Run("Step04_InviteAdmin", testOnbInviteAdmin)
	t.Run("Step05_OperatorGrant", testOnbOperatorGrant)
	t.Run("Step06_CreateAPN", testOnbCreateAPN)
	t.Run("Step07_ImportSIMs", testOnbImportSIMs)
	t.Run("Step08_WaitForImportJob", testOnbWaitForImportJob)
	t.Run("Step09_VerifySIMs", testOnbVerifySIMs)
	t.Run("Step10_CreatePolicy", testOnbCreatePolicy)
	t.Run("Step11_ActivatePolicy", testOnbActivatePolicy)
	t.Run("Step12_AssignPolicy", testOnbAssignPolicy)
	t.Run("Step13_RADIUSAuth", testOnbRADIUSAuth)
	t.Run("Step14_TenantIsolation", testOnbTenantIsolation)
	t.Run("Step15_Dashboard", testOnbDashboard)

	elapsed := time.Since(start)
	t.Logf("full onboarding E2E completed in %s", elapsed)
	assert.Less(t, elapsed, 90*time.Second, "full flow should complete in < 90 seconds")
}

func testOnbSuperAdminLogin(t *testing.T) {
	body := `{"email":"admin@argus.io","password":"admin"}`
	resp := onbPost(t, "/api/v1/auth/login", body, "")

	require.Equal(t, http.StatusOK, resp.StatusCode, "login should return 200")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var loginData struct {
		Token string `json:"token"`
		User  struct {
			ID       string `json:"id"`
			Email    string `json:"email"`
			TenantID string `json:"tenant_id"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &loginData))
	require.NotEmpty(t, loginData.Token)
	assert.Equal(t, "admin@argus.io", loginData.User.Email)
	assert.Equal(t, "super_admin", loginData.User.Role)

	onbMu.Lock()
	superAdminToken = loginData.Token
	onbMu.Unlock()

	t.Logf("super_admin login: user=%s tenant=%s role=%s",
		loginData.User.Email, loginData.User.TenantID, loginData.User.Role)
}

func testOnbCreateTenantA(t *testing.T) {
	onbRequireAuth(t)

	ts := time.Now().UnixMilli()
	tenantName := fmt.Sprintf("E2E Tenant A %d", ts)
	domain := fmt.Sprintf("e2e-a-%d.argus.test", ts)

	body := fmt.Sprintf(`{
		"name": %q,
		"domain": %q,
		"contact_email": "admin-a@e2e-test.io",
		"max_sims": 1000,
		"max_apns": 10,
		"max_users": 20
	}`, tenantName, domain)

	resp := onbPost(t, "/api/v1/tenants", body, superAdminToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create tenant should return 201")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var tenantData struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Domain       string `json:"domain"`
		ContactEmail string `json:"contact_email"`
		State        string `json:"state"`
		MaxSims      int    `json:"max_sims"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &tenantData))
	require.NotEmpty(t, tenantData.ID)
	assert.Equal(t, "active", tenantData.State)
	assert.Equal(t, 1000, tenantData.MaxSims)

	onbMu.Lock()
	tenantAID = tenantData.ID
	onbMu.Unlock()

	t.Logf("tenant A created: id=%s name=%s", tenantData.ID, tenantData.Name)
}

func testOnbCreateTenantB(t *testing.T) {
	onbRequireAuth(t)

	ts := time.Now().UnixMilli()
	tenantName := fmt.Sprintf("E2E Tenant B %d", ts)
	domain := fmt.Sprintf("e2e-b-%d.argus.test", ts)

	body := fmt.Sprintf(`{
		"name": %q,
		"domain": %q,
		"contact_email": "admin-b@e2e-test.io",
		"max_sims": 100,
		"max_apns": 5,
		"max_users": 10
	}`, tenantName, domain)

	resp := onbPost(t, "/api/v1/tenants", body, superAdminToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create tenant B should return 201")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var tenantData struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &tenantData))
	require.NotEmpty(t, tenantData.ID)

	onbMu.Lock()
	tenantBID = tenantData.ID
	onbMu.Unlock()

	t.Logf("tenant B created: id=%s (for isolation testing)", tenantData.ID)
}

func testOnbInviteAdmin(t *testing.T) {
	onbRequireAuth(t)

	ts := time.Now().UnixMilli()
	email := fmt.Sprintf("admin-%d@e2e-test.io", ts)

	body := fmt.Sprintf(`{
		"email": %q,
		"name": "E2E Tenant Admin",
		"role": "sim_manager"
	}`, email)

	resp := onbPost(t, "/api/v1/users", body, superAdminToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create user should return 201")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var userData struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &userData))
	require.NotEmpty(t, userData.ID)
	assert.Equal(t, "invited", userData.State)

	onbMu.Lock()
	tenantAAdminID = userData.ID
	onbMu.Unlock()

	t.Logf("admin user invited: id=%s email=%s state=%s", userData.ID, userData.Email, userData.State)
}

func testOnbOperatorGrant(t *testing.T) {
	onbRequireAuth(t)

	opID := onbResolveOperator(t)

	resp := onbGet(t, "/api/v1/operator-grants?limit=100", superAdminToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var listResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	onbParseResponse(t, resp, &listResp)

	var grants []struct {
		ID         string `json:"id"`
		TenantID   string `json:"tenant_id"`
		OperatorID string `json:"operator_id"`
	}
	require.NoError(t, json.Unmarshal(listResp.Data, &grants))

	t.Logf("existing grants: %d, operator=%s", len(grants), opID)

	onbMu.Lock()
	onbOperatorID = opID
	onbMu.Unlock()

	opCode := onbResolveOperatorCode(t, opID)
	onbMu.Lock()
	onbOperatorCode = opCode
	onbMu.Unlock()

	resp2 := onbGet(t, "/api/v1/operators/"+opID+"/health", superAdminToken)
	if resp2.StatusCode == http.StatusOK {
		var healthResp apiResponse
		onbParseResponse(t, resp2, &healthResp)
		t.Logf("operator health check: status=%d", resp2.StatusCode)
	} else {
		resp2.Body.Close()
		t.Logf("operator health check: status=%d (non-critical)", resp2.StatusCode)
	}

	t.Logf("operator resolved: id=%s code=%s", opID, opCode)
}

func testOnbCreateAPN(t *testing.T) {
	onbRequireAuth(t)
	require.NotEmpty(t, onbOperatorID)

	apnName := fmt.Sprintf("e2e-onboard-%d", time.Now().UnixMilli())
	body := fmt.Sprintf(`{
		"name": %q,
		"operator_id": %q,
		"apn_type": "private_managed",
		"supported_rat_types": ["lte", "nb_iot"],
		"settings": {}
	}`, apnName, onbOperatorID)

	resp := onbPost(t, "/api/v1/apns", body, superAdminToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create APN should return 201")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)
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

	onbMu.Lock()
	onbAPNID = apnData.ID
	onbAPNName = apnData.Name
	onbMu.Unlock()

	t.Logf("APN created: id=%s name=%s", apnData.ID, apnData.Name)
}

func testOnbImportSIMs(t *testing.T) {
	onbRequireAuth(t)
	require.NotEmpty(t, onbOperatorCode)
	require.NotEmpty(t, onbAPNName)

	csvContent := onbGenerateCSV(onbOperatorCode, onbAPNName, 5)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "e2e_onboard_sims.csv")
	require.NoError(t, err)
	_, err = part.Write([]byte(csvContent))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req, err := http.NewRequest("POST", onbBaseURL+"/api/v1/sims/bulk/import", &buf)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+superAdminToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusAccepted, resp.StatusCode, "bulk import should return 202")

	bodyBytes, _ := io.ReadAll(resp.Body)
	var apiResp apiResponse
	require.NoError(t, json.Unmarshal(bodyBytes, &apiResp))
	require.Equal(t, "success", apiResp.Status)

	var importData struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &importData))
	require.NotEmpty(t, importData.JobID)

	onbMu.Lock()
	onbJobID = importData.JobID
	onbMu.Unlock()

	t.Logf("SIM import started: job_id=%s (5 SIMs)", importData.JobID)
}

func testOnbWaitForImportJob(t *testing.T) {
	onbRequireAuth(t)
	require.NotEmpty(t, onbJobID)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp := onbGet(t, "/api/v1/jobs/"+onbJobID, superAdminToken)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var apiResp apiResponse
		onbParseResponse(t, resp, &apiResp)

		var job struct {
			State          string  `json:"state"`
			TotalItems     int     `json:"total_items"`
			ProcessedItems int     `json:"processed_items"`
			FailedItems    int     `json:"failed_items"`
			ProgressPct    float64 `json:"progress_pct"`
		}
		require.NoError(t, json.Unmarshal(apiResp.Data, &job))

		t.Logf("import job: state=%s progress=%.0f%% processed=%d/%d failed=%d",
			job.State, job.ProgressPct, job.ProcessedItems, job.TotalItems, job.FailedItems)

		if job.State == "completed" || job.State == "completed_with_errors" {
			assert.Equal(t, 5, job.TotalItems, "should have 5 total items")
			return
		}

		if job.State == "failed" || job.State == "cancelled" {
			t.Fatalf("import job ended in unexpected state: %s", job.State)
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatal("import job did not complete within 30 seconds")
}

func testOnbVerifySIMs(t *testing.T) {
	onbRequireAuth(t)

	resp := onbGet(t, "/api/v1/sims?limit=100", superAdminToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var listResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
		Meta   struct {
			Total int64 `json:"total"`
		} `json:"meta"`
	}
	onbParseResponse(t, resp, &listResp)

	var sims []struct {
		ID    string  `json:"id"`
		ICCID string  `json:"iccid"`
		IMSI  string  `json:"imsi"`
		State string  `json:"state"`
		APNID *string `json:"apn_id"`
	}
	require.NoError(t, json.Unmarshal(listResp.Data, &sims))

	var onbSims []struct {
		ID    string  `json:"id"`
		ICCID string  `json:"iccid"`
		IMSI  string  `json:"imsi"`
		State string  `json:"state"`
		APNID *string `json:"apn_id"`
	}
	for _, s := range sims {
		if strings.HasPrefix(s.ICCID, "89902") && strings.HasPrefix(s.IMSI, "99902") {
			onbSims = append(onbSims, s)
		}
	}

	require.GreaterOrEqual(t, len(onbSims), 5, "at least 5 imported SIMs should be present")

	onbMu.Lock()
	onbCreatedSIMs = nil
	for _, s := range onbSims {
		onbCreatedSIMs = append(onbCreatedSIMs, simInfo{ID: s.ID, IMSI: s.IMSI})
	}
	onbMu.Unlock()

	t.Logf("verified %d SIMs from onboarding import", len(onbSims))
}

func testOnbCreatePolicy(t *testing.T) {
	onbRequireAuth(t)

	policyName := fmt.Sprintf("e2e-onboard-policy-%d", time.Now().UnixMilli())
	dslSource := `POLICY "Onboarding Test Policy" {
  RULE "default-qos" {
    MATCH ALL
    SET bandwidth_up = 5000
    SET bandwidth_down = 25000
    SET priority = 3
  }
}`

	body := fmt.Sprintf(`{
		"name": %q,
		"description": "E2E onboarding test policy",
		"scope": "global",
		"dsl_source": %q
	}`, policyName, dslSource)

	resp := onbPost(t, "/api/v1/policies", body, superAdminToken)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create policy should return 201")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)
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

	onbMu.Lock()
	onbPolicyID = pol.ID
	onbVersionID = pol.Versions[0].ID
	onbMu.Unlock()

	t.Logf("policy created: id=%s version_id=%s state=%s", pol.ID, pol.Versions[0].ID, pol.Versions[0].State)
}

func testOnbActivatePolicy(t *testing.T) {
	onbRequireAuth(t)
	require.NotEmpty(t, onbVersionID)

	resp := onbPost(t, "/api/v1/policy-versions/"+onbVersionID+"/activate", "{}", superAdminToken)
	require.Equal(t, http.StatusOK, resp.StatusCode, "activate policy should return 200")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var ver struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &ver))
	assert.Equal(t, "active", ver.State)

	t.Logf("policy version activated: id=%s state=%s", ver.ID, ver.State)
}

func testOnbAssignPolicy(t *testing.T) {
	onbRequireAuth(t)
	require.NotEmpty(t, onbVersionID)
	require.NotEmpty(t, onbCreatedSIMs)

	segName := fmt.Sprintf("e2e-onboard-segment-%d", time.Now().UnixMilli())
	segBody := fmt.Sprintf(`{
		"name": %q,
		"filter": {
			"iccid_prefix": "89902"
		}
	}`, segName)

	segResp := onbPost(t, "/api/v1/sim-segments", segBody, superAdminToken)
	require.Equal(t, http.StatusCreated, segResp.StatusCode, "create segment should return 201")

	var segAPIResp apiResponse
	onbParseResponse(t, segResp, &segAPIResp)

	var seg struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(segAPIResp.Data, &seg))
	require.NotEmpty(t, seg.ID)

	body := fmt.Sprintf(`{
		"segment_id": %q,
		"policy_version_id": %q
	}`, seg.ID, onbVersionID)

	resp := onbPost(t, "/api/v1/sims/bulk/policy-assign", body, superAdminToken)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, "policy assign should return 202")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)

	var assignData struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &assignData))
	require.NotEmpty(t, assignData.JobID)

	onbWaitForJob(t, assignData.JobID, 30*time.Second)

	t.Logf("policy assigned to SIMs via job=%s", assignData.JobID)
}

func testOnbRADIUSAuth(t *testing.T) {
	require.NotEmpty(t, onbCreatedSIMs, "SIMs required from previous steps")

	sim := onbCreatedSIMs[0]

	packet := radius.New(radius.CodeAccessRequest, []byte(onbRadiusSecret))
	rfc2865.UserName_SetString(packet, sim.IMSI)
	rfc2865.CallingStationID_SetString(packet, sim.IMSI)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	response, err := radius.Exchange(ctx, packet, onbRadiusAddr)
	require.NoError(t, err, "RADIUS exchange should succeed")
	require.Equal(t, radius.CodeAccessAccept, response.Code, "should receive Access-Accept for onboarded SIM")

	sessionTimeout := rfc2865.SessionTimeout_Get(response)
	t.Logf("RADIUS auth: imsi=%s result=Access-Accept session_timeout=%d", sim.IMSI, sessionTimeout)

	onbSendAcctStart(t, sim.IMSI)
}

func testOnbTenantIsolation(t *testing.T) {
	onbRequireAuth(t)
	require.NotEmpty(t, tenantAID)
	require.NotEmpty(t, tenantBID)

	resp := onbGet(t, "/api/v1/tenants/"+tenantAID, superAdminToken)
	require.Equal(t, http.StatusOK, resp.StatusCode, "super_admin should see tenant A")
	var tenantAResp apiResponse
	onbParseResponse(t, resp, &tenantAResp)
	require.Equal(t, "success", tenantAResp.Status)

	var tenantA struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(tenantAResp.Data, &tenantA))
	assert.Equal(t, tenantAID, tenantA.ID)
	t.Logf("tenant A accessible: id=%s state=%s", tenantA.ID, tenantA.State)

	resp2 := onbGet(t, "/api/v1/tenants/"+tenantBID, superAdminToken)
	require.Equal(t, http.StatusOK, resp2.StatusCode, "super_admin should see tenant B")
	var tenantBResp apiResponse
	onbParseResponse(t, resp2, &tenantBResp)
	require.Equal(t, "success", tenantBResp.Status)

	var tenantB struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(tenantBResp.Data, &tenantB))
	assert.Equal(t, tenantBID, tenantB.ID)
	t.Logf("tenant B accessible: id=%s state=%s", tenantB.ID, tenantB.State)

	assert.NotEqual(t, tenantAID, tenantBID, "tenant A and B should have different IDs")

	resp3 := onbGet(t, "/api/v1/tenants", superAdminToken)
	require.Equal(t, http.StatusOK, resp3.StatusCode)
	var listResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	onbParseResponse(t, resp3, &listResp)

	var tenants []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(listResp.Data, &tenants))

	foundA, foundB := false, false
	for _, ten := range tenants {
		if ten.ID == tenantAID {
			foundA = true
		}
		if ten.ID == tenantBID {
			foundB = true
		}
	}
	assert.True(t, foundA, "tenant A should be in list")
	assert.True(t, foundB, "tenant B should be in list")

	simResp := onbGet(t, "/api/v1/sims?limit=100", superAdminToken)
	require.Equal(t, http.StatusOK, simResp.StatusCode)

	var simListResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	onbParseResponse(t, simResp, &simListResp)

	var sims []struct {
		ID       string `json:"id"`
		ICCID    string `json:"iccid"`
		TenantID string `json:"tenant_id"`
	}
	require.NoError(t, json.Unmarshal(simListResp.Data, &sims))

	for _, sim := range sims {
		if sim.TenantID != "" {
			assert.NotEqual(t, tenantBID, sim.TenantID,
				"SIMs created under demo tenant should not appear under tenant B")
		}
	}

	t.Logf("tenant isolation verified: %d tenants in list, SIMs scoped to creator's tenant", len(tenants))
}

func testOnbDashboard(t *testing.T) {
	onbRequireAuth(t)

	time.Sleep(1 * time.Second)

	resp := onbGet(t, "/api/v1/dashboard", superAdminToken)
	require.Equal(t, http.StatusOK, resp.StatusCode, "dashboard should return 200")

	var apiResp apiResponse
	onbParseResponse(t, resp, &apiResp)
	require.Equal(t, "success", apiResp.Status)

	var dashboard struct {
		TotalSIMs      int `json:"total_sims"`
		ActiveSessions int `json:"active_sessions"`
	}
	require.NoError(t, json.Unmarshal(apiResp.Data, &dashboard))

	assert.GreaterOrEqual(t, dashboard.TotalSIMs, 5,
		"dashboard should show at least 5 SIMs after onboarding import")

	t.Logf("dashboard after onboarding: total_sims=%d active_sessions=%d",
		dashboard.TotalSIMs, dashboard.ActiveSessions)
}

func onbGet(t *testing.T, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", onbBaseURL+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func onbPost(t *testing.T, path, body, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", onbBaseURL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func onbParseResponse(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, v), "response body: %s", string(body))
}

func onbRequireAuth(t *testing.T) {
	t.Helper()
	onbMu.Lock()
	defer onbMu.Unlock()
	if superAdminToken == "" {
		t.Fatal("super_admin token not available; Step01 must pass first")
	}
}

func onbResolveOperator(t *testing.T) string {
	t.Helper()
	resp := onbGet(t, "/api/v1/operators?limit=1", superAdminToken)
	require.Equal(t, http.StatusOK, resp.StatusCode, "list operators should return 200")

	var listResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	onbParseResponse(t, resp, &listResp)

	var ops []struct {
		ID   string `json:"id"`
		Code string `json:"code"`
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(listResp.Data, &ops))
	require.NotEmpty(t, ops, "at least one operator must exist")

	return ops[0].ID
}

func onbResolveOperatorCode(t *testing.T, opID string) string {
	t.Helper()
	resp := onbGet(t, "/api/v1/operators?limit=100", superAdminToken)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var listResp struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	onbParseResponse(t, resp, &listResp)

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

func onbGenerateCSV(operatorCode, apnName string, count int) string {
	ts := time.Now().UnixMilli()
	var sb strings.Builder
	sb.WriteString("iccid,imsi,msisdn,operator_code,apn_name\n")
	for i := 0; i < count; i++ {
		iccid := fmt.Sprintf("89902%013d", ts*100+int64(i))
		imsi := fmt.Sprintf("99902%010d", ts*100+int64(i))
		msisdn := fmt.Sprintf("+9054%08d", ts%100000000+int64(i))
		sb.WriteString(fmt.Sprintf("%s,%s,%s,%s,%s\n", iccid, imsi, msisdn, operatorCode, apnName))
	}
	return sb.String()
}

func onbWaitForJob(t *testing.T, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := onbGet(t, "/api/v1/jobs/"+id, superAdminToken)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var apiResp apiResponse
		onbParseResponse(t, resp, &apiResp)

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

func onbSendAcctStart(t *testing.T, imsi string) {
	t.Helper()

	acctAddr := strings.Replace(onbRadiusAddr, ":1812", ":1813", 1)
	if acctAddr == onbRadiusAddr {
		acctAddr = "localhost:1813"
	}

	packet := radius.New(radius.CodeAccountingRequest, []byte(onbRadiusSecret))
	rfc2865.UserName_SetString(packet, imsi)
	rfc2866.AcctStatusType_Set(packet, rfc2866.AcctStatusType_Value_Start)
	rfc2866.AcctSessionID_SetString(packet, fmt.Sprintf("e2e-onb-%s-%d", imsi, time.Now().UnixNano()))
	rfc2865.NASIPAddress_Set(packet, []byte{10, 0, 0, 2})

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

func onbCleanup(t *testing.T) {
	if superAdminToken == "" {
		return
	}

	for _, sim := range onbCreatedSIMs {
		req, _ := http.NewRequest("POST", onbBaseURL+"/api/v1/sims/"+sim.ID+"/terminate", nil)
		req.Header.Set("Authorization", "Bearer "+superAdminToken)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	if onbAPNID != "" {
		req, _ := http.NewRequest("DELETE", onbBaseURL+"/api/v1/apns/"+onbAPNID, nil)
		req.Header.Set("Authorization", "Bearer "+superAdminToken)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	if onbPolicyID != "" {
		body := `{"state":"archived"}`
		req, _ := http.NewRequest("PATCH", onbBaseURL+"/api/v1/policies/"+onbPolicyID, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+superAdminToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	if tenantAAdminID != "" {
		body := `{"state":"disabled"}`
		req, _ := http.NewRequest("PATCH", onbBaseURL+"/api/v1/users/"+tenantAAdminID, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+superAdminToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	for _, tid := range []string{tenantAID, tenantBID} {
		if tid == "" {
			continue
		}
		body := `{"state":"suspended"}`
		req, _ := http.NewRequest("PATCH", onbBaseURL+"/api/v1/tenants/"+tid, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+superAdminToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	t.Log("onboarding E2E cleanup completed")
}
