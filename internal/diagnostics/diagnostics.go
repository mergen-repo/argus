package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	StatusPass = "pass"
	StatusWarn = "warn"
	StatusFail = "fail"

	OverallPass     = "PASS"
	OverallDegraded = "DEGRADED"
	OverallFail     = "FAIL"
)

type StepResult struct {
	Step       int    `json:"step"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type DiagnosticResult struct {
	SimID         string       `json:"sim_id"`
	OverallStatus string       `json:"overall_status"`
	Steps         []StepResult `json:"steps"`
	DiagnosedAt   time.Time    `json:"diagnosed_at"`
}

type Service struct {
	simStore      *store.SIMStore
	sessionStore  *store.RadiusSessionStore
	operatorStore *store.OperatorStore
	apnStore      *store.APNStore
	policyStore   *store.PolicyStore
	ippoolStore   *store.IPPoolStore
	logger        zerolog.Logger
}

func NewService(
	simStore *store.SIMStore,
	sessionStore *store.RadiusSessionStore,
	operatorStore *store.OperatorStore,
	apnStore *store.APNStore,
	policyStore *store.PolicyStore,
	ippoolStore *store.IPPoolStore,
	logger zerolog.Logger,
) *Service {
	return &Service{
		simStore:      simStore,
		sessionStore:  sessionStore,
		operatorStore: operatorStore,
		apnStore:      apnStore,
		policyStore:   policyStore,
		ippoolStore:   ippoolStore,
		logger:        logger.With().Str("component", "diagnostics").Logger(),
	}
}

func (s *Service) Diagnose(ctx context.Context, tenantID, simID uuid.UUID, includeTestAuth bool) (*DiagnosticResult, error) {
	sim, err := s.simStore.GetByID(ctx, tenantID, simID)
	if err != nil {
		return nil, err
	}

	result := &DiagnosticResult{
		SimID:       sim.ID.String(),
		DiagnosedAt: time.Now().UTC(),
	}

	result.Steps = append(result.Steps, s.checkSIMState(sim))
	result.Steps = append(result.Steps, s.checkLastAuth(ctx, sim))
	result.Steps = append(result.Steps, s.checkOperatorHealth(ctx, sim))
	result.Steps = append(result.Steps, s.checkAPNConfig(ctx, sim))
	result.Steps = append(result.Steps, s.checkPolicy(ctx, sim))
	result.Steps = append(result.Steps, s.checkIPPool(ctx, sim))

	if includeTestAuth {
		result.Steps = append(result.Steps, s.checkTestAuth(ctx, sim))
	}

	result.OverallStatus = computeOverall(result.Steps)

	return result, nil
}

func (s *Service) checkSIMState(sim *store.SIM) StepResult {
	step := StepResult{
		Step: 1,
		Name: "SIM State",
	}

	switch sim.State {
	case "active":
		step.Status = StatusPass
		step.Message = "SIM is active"
	case "suspended":
		step.Status = StatusFail
		step.Message = "SIM is suspended"
		step.Suggestion = "Activate or resume SIM"
	case "terminated":
		step.Status = StatusFail
		step.Message = "SIM is terminated"
		step.Suggestion = "SIM has been terminated and cannot be used"
	case "stolen_lost":
		step.Status = StatusFail
		step.Message = "SIM is reported as stolen/lost"
		step.Suggestion = "SIM has been marked as stolen/lost"
	case "ordered":
		step.Status = StatusFail
		step.Message = "SIM is in ordered state, not yet activated"
		step.Suggestion = "Activate the SIM first"
	default:
		step.Status = StatusFail
		step.Message = fmt.Sprintf("SIM is in unexpected state: %s", sim.State)
		step.Suggestion = "Check SIM state"
	}

	return step
}

func (s *Service) checkLastAuth(ctx context.Context, sim *store.SIM) StepResult {
	step := StepResult{
		Step: 2,
		Name: "Last Authentication",
	}

	if s.sessionStore == nil {
		step.Status = StatusWarn
		step.Message = "Session store unavailable, skipping auth check"
		return step
	}

	lastSession, err := s.sessionStore.GetLastSessionBySIM(ctx, sim.TenantID, sim.ID)
	if err != nil {
		s.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Msg("failed to query last session")
		step.Status = StatusWarn
		step.Message = "Unable to check last authentication"
		return step
	}

	if lastSession == nil {
		step.Status = StatusWarn
		step.Message = "SIM has never connected"
		step.Suggestion = "Verify SIM is properly provisioned and inserted in device"
		return step
	}

	if lastSession.TerminateCause != nil && *lastSession.TerminateCause == "access_reject" {
		step.Status = StatusFail
		step.Message = fmt.Sprintf("Last authentication was rejected: %s", *lastSession.TerminateCause)
		step.Suggestion = "Check SIM credentials and operator configuration"
		return step
	}

	if lastSession.SessionState == "active" {
		step.Status = StatusPass
		step.Message = "SIM has an active session"
		return step
	}

	since := time.Since(lastSession.StartedAt)
	if since > 24*time.Hour {
		step.Status = StatusWarn
		step.Message = fmt.Sprintf("No recent activity (last session: %s ago)", formatDuration(since))
		step.Suggestion = "Check device connectivity"
		return step
	}

	step.Status = StatusPass
	step.Message = fmt.Sprintf("Last session: %s ago", formatDuration(since))
	return step
}

func (s *Service) checkOperatorHealth(ctx context.Context, sim *store.SIM) StepResult {
	step := StepResult{
		Step: 3,
		Name: "Operator Health",
	}

	if s.operatorStore == nil {
		step.Status = StatusWarn
		step.Message = "Operator store unavailable, skipping health check"
		return step
	}

	op, err := s.operatorStore.GetByID(ctx, sim.OperatorID)
	if err != nil {
		step.Status = StatusFail
		step.Message = "Operator not found"
		step.Suggestion = "Verify SIM is assigned to a valid operator"
		return step
	}

	switch op.HealthStatus {
	case "healthy":
		step.Status = StatusPass
		step.Message = fmt.Sprintf("Operator %s is healthy", op.Name)
	case "degraded":
		step.Status = StatusWarn
		step.Message = fmt.Sprintf("Operator %s is experiencing issues", op.Name)
		step.Suggestion = "Monitor operator status, consider failover"
	case "down", "unhealthy":
		step.Status = StatusFail
		step.Message = fmt.Sprintf("Operator %s is down, failover policy: %s", op.Name, op.FailoverPolicy)
		step.Suggestion = "Check operator connectivity or initiate failover"
	default:
		step.Status = StatusWarn
		step.Message = fmt.Sprintf("Operator %s health status unknown: %s", op.Name, op.HealthStatus)
	}

	return step
}

func (s *Service) checkAPNConfig(ctx context.Context, sim *store.SIM) StepResult {
	step := StepResult{
		Step: 4,
		Name: "APN Configuration",
	}

	if sim.APNID == nil {
		step.Status = StatusFail
		step.Message = "No APN assigned to SIM"
		step.Suggestion = "Assign an APN to this SIM"
		return step
	}

	apn, err := s.apnStore.GetByID(ctx, sim.TenantID, *sim.APNID)
	if err != nil {
		step.Status = StatusFail
		step.Message = "APN not found"
		step.Suggestion = "Check APN-operator mapping"
		return step
	}

	if apn.State != "active" {
		step.Status = StatusFail
		step.Message = fmt.Sprintf("APN %s is not active (state: %s)", apn.Name, apn.State)
		step.Suggestion = "Activate the APN or assign a different APN"
		return step
	}

	if apn.OperatorID != sim.OperatorID {
		step.Status = StatusFail
		step.Message = fmt.Sprintf("APN %s is mapped to a different operator", apn.Name)
		step.Suggestion = "Check APN-operator mapping"
		return step
	}

	step.Status = StatusPass
	step.Message = fmt.Sprintf("APN %s is active and correctly mapped", apn.Name)
	return step
}

func (s *Service) checkPolicy(ctx context.Context, sim *store.SIM) StepResult {
	step := StepResult{
		Step: 5,
		Name: "Policy Verification",
	}

	if sim.PolicyVersionID == nil {
		step.Status = StatusWarn
		step.Message = "No policy version assigned to SIM"
		step.Suggestion = "Assign a policy to this SIM"
		return step
	}

	version, err := s.policyStore.GetVersionByID(ctx, *sim.PolicyVersionID)
	if err != nil {
		step.Status = StatusFail
		step.Message = "Policy version not found"
		step.Suggestion = "Reassign a valid policy version"
		return step
	}

	if version.State != "active" {
		step.Status = StatusFail
		step.Message = fmt.Sprintf("Policy version is not active (state: %s)", version.State)
		step.Suggestion = "Activate the policy version or assign a different one"
		return step
	}

	if isThrottledToZero(version.CompiledRules) {
		step.Status = StatusFail
		step.Message = "Policy is throttled to 0 bandwidth"
		step.Suggestion = "Update policy bandwidth"
		return step
	}

	step.Status = StatusPass
	step.Message = fmt.Sprintf("Policy version v%d is active", version.Version)
	return step
}

func (s *Service) checkIPPool(ctx context.Context, sim *store.SIM) StepResult {
	step := StepResult{
		Step: 6,
		Name: "IP Pool Availability",
	}

	if sim.APNID == nil {
		step.Status = StatusWarn
		step.Message = "No APN assigned, cannot check IP pool"
		return step
	}

	pools, _, err := s.ippoolStore.List(ctx, sim.TenantID, "", 100, sim.APNID)
	if err != nil {
		step.Status = StatusWarn
		step.Message = "Unable to check IP pools"
		return step
	}

	if len(pools) == 0 {
		step.Status = StatusFail
		step.Message = "No IP pools configured for the APN"
		step.Suggestion = "Create an IP pool for this APN"
		return step
	}

	hasAvailable := false
	for _, pool := range pools {
		if pool.State == "active" {
			available := pool.TotalAddresses - pool.UsedAddresses
			if available > 0 {
				hasAvailable = true
				break
			}
		}
	}

	if !hasAvailable {
		allExhausted := true
		for _, pool := range pools {
			if pool.State != "exhausted" && pool.State != "disabled" {
				allExhausted = false
				break
			}
		}
		if allExhausted {
			step.Status = StatusFail
			step.Message = "All IP pools are exhausted"
			step.Suggestion = "Expand IP pool or reclaim IPs"
			return step
		}
		step.Status = StatusWarn
		step.Message = "IP pools have limited availability"
		step.Suggestion = "Consider expanding IP pools"
		return step
	}

	step.Status = StatusPass
	step.Message = "IP pool has available addresses"
	return step
}

func (s *Service) checkTestAuth(_ context.Context, _ *store.SIM) StepResult {
	step := StepResult{
		Step: 7,
		Name: "Test Authentication",
	}

	step.Status = StatusWarn
	step.Message = "Test authentication not yet implemented"
	step.Suggestion = "Test auth requires operator adapter integration"
	return step
}

func computeOverall(steps []StepResult) string {
	hasFail := false
	hasWarn := false
	for _, s := range steps {
		switch s.Status {
		case StatusFail:
			hasFail = true
		case StatusWarn:
			hasWarn = true
		}
	}
	if hasFail {
		return OverallFail
	}
	if hasWarn {
		return OverallDegraded
	}
	return OverallPass
}

func isThrottledToZero(compiled json.RawMessage) bool {
	if len(compiled) == 0 {
		return false
	}
	var rules map[string]interface{}
	if err := json.Unmarshal(compiled, &rules); err != nil {
		return false
	}
	if bw, ok := rules["max_bandwidth"]; ok {
		switch v := bw.(type) {
		case float64:
			return v == 0
		case int:
			return v == 0
		}
	}
	if dl, ok := rules["download_rate"]; ok {
		if ul, ok2 := rules["upload_rate"]; ok2 {
			dlZero := isZeroNum(dl)
			ulZero := isZeroNum(ul)
			return dlZero && ulZero
		}
	}
	return false
}

func isZeroNum(v interface{}) bool {
	switch n := v.(type) {
	case float64:
		return n == 0
	case int:
		return n == 0
	}
	return false
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
