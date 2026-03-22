package compliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Service struct {
	complianceStore *store.ComplianceStore
	auditStore      *store.AuditStore
	auditSvc        *audit.FullService
	logger          zerolog.Logger
}

func NewService(
	complianceStore *store.ComplianceStore,
	auditStore *store.AuditStore,
	auditSvc *audit.FullService,
	logger zerolog.Logger,
) *Service {
	return &Service{
		complianceStore: complianceStore,
		auditStore:      auditStore,
		auditSvc:        auditSvc,
		logger:          logger.With().Str("component", "compliance_service").Logger(),
	}
}

type PurgeResult struct {
	TotalPurged   int      `json:"total_purged"`
	FailedCount   int      `json:"failed_count"`
	FailedSIMs    []string `json:"failed_sims,omitempty"`
	PseudonymizedLogs int  `json:"pseudonymized_logs"`
}

func (s *Service) RunPurgeSweep(ctx context.Context, batchSize int) (*PurgeResult, error) {
	result := &PurgeResult{}

	sims, err := s.complianceStore.FindPurgableSIMs(ctx, batchSize)
	if err != nil {
		return nil, fmt.Errorf("find purgable sims: %w", err)
	}

	if len(sims) == 0 {
		s.logger.Info().Msg("no SIMs eligible for purge")
		return result, nil
	}

	s.logger.Info().Int("count", len(sims)).Msg("found SIMs eligible for purge")

	for _, sim := range sims {
		tenantSalt := deriveTenantSalt(sim.TenantID)

		if err := s.complianceStore.PurgeSIM(ctx, sim.ID, tenantSalt); err != nil {
			s.logger.Error().Err(err).Str("sim_id", sim.ID.String()).Msg("purge failed")
			result.FailedCount++
			result.FailedSIMs = append(result.FailedSIMs, sim.ID.String())
			continue
		}

		s.createPurgeAuditEntry(ctx, sim.TenantID, sim.ID)
		result.TotalPurged++
	}

	tenants := uniqueTenantIDs(sims)
	for _, tenantID := range tenants {
		chainResult, chainErr := s.auditSvc.VerifyChain(ctx, tenantID, 100)
		if chainErr != nil {
			s.logger.Error().Err(chainErr).Str("tenant_id", tenantID.String()).Msg("hash chain verification failed, skipping pseudonymization")
			continue
		}
		if !chainResult.Verified {
			s.logger.Warn().Str("tenant_id", tenantID.String()).Msg("audit chain tampered, skipping pseudonymization")
			continue
		}

		retentionDays, retErr := s.complianceStore.GetRetentionDays(ctx, tenantID)
		if retErr != nil {
			s.logger.Warn().Err(retErr).Str("tenant_id", tenantID.String()).Msg("failed to get retention days, using default 90")
			retentionDays = 90
		}
		tenantSalt := deriveTenantSalt(tenantID)
		pseudonymized, err := s.complianceStore.PseudonymizeAuditLogs(ctx, tenantID, retentionDays, tenantSalt)
		if err != nil {
			s.logger.Error().Err(err).Str("tenant_id", tenantID.String()).Msg("audit pseudonymization failed")
			continue
		}
		result.PseudonymizedLogs += pseudonymized
	}

	return result, nil
}

func (s *Service) DataSubjectAccess(ctx context.Context, tenantID, simID uuid.UUID) (*store.DataSubjectExport, error) {
	return s.complianceStore.ExportSIMData(ctx, tenantID, simID)
}

func (s *Service) RightToErasure(ctx context.Context, tenantID, simID uuid.UUID) error {
	chainResult, err := s.auditSvc.VerifyChain(ctx, tenantID, 100)
	if err != nil {
		return fmt.Errorf("verify audit chain: %w", err)
	}
	if !chainResult.Verified {
		return fmt.Errorf("audit chain verification failed — erasure blocked for data integrity")
	}

	tenantSalt := deriveTenantSalt(tenantID)
	if err := s.complianceStore.EarlyPurgeSIM(ctx, tenantID, simID, tenantSalt); err != nil {
		return fmt.Errorf("early purge: %w", err)
	}

	s.createPurgeAuditEntry(ctx, tenantID, simID)

	entityIDs := []string{simID.String()}
	if err := s.auditStore.Pseudonymize(ctx, tenantID, entityIDs); err != nil {
		s.logger.Error().Err(err).Str("sim_id", simID.String()).Msg("post-erasure pseudonymization failed")
	}

	return nil
}

type ComplianceDashboard struct {
	StateCounts    []store.StateCount `json:"state_counts"`
	PendingPurges  int                `json:"pending_purges"`
	OverduePurges  int                `json:"overdue_purges"`
	RetentionDays  int                `json:"retention_days"`
	CompliancePct  float64            `json:"compliance_pct"`
	ChainVerified  bool               `json:"chain_verified"`
}

func (s *Service) Dashboard(ctx context.Context, tenantID uuid.UUID, retentionDays int) (*ComplianceDashboard, error) {
	stateCounts, err := s.complianceStore.CountSIMsByState(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count by state: %w", err)
	}

	pending, err := s.complianceStore.CountPendingPurges(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count pending: %w", err)
	}

	overdue, err := s.complianceStore.CountOverduePurges(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count overdue: %w", err)
	}

	compliancePct := 100.0
	if pending > 0 {
		if overdue > 0 {
			compliancePct = float64(pending-overdue) / float64(pending) * 100.0
		}
	}

	chainResult, err := s.auditSvc.VerifyChain(ctx, tenantID, 100)
	chainVerified := false
	if err == nil {
		chainVerified = chainResult.Verified
	}

	return &ComplianceDashboard{
		StateCounts:   stateCounts,
		PendingPurges: pending,
		OverduePurges: overdue,
		RetentionDays: retentionDays,
		CompliancePct: compliancePct,
		ChainVerified: chainVerified,
	}, nil
}

type BTKReport struct {
	TenantID    uuid.UUID                `json:"tenant_id"`
	ReportMonth string                   `json:"report_month"`
	GeneratedAt string                   `json:"generated_at"`
	Operators   []store.BTKOperatorStats `json:"operators"`
	TotalActive int                      `json:"total_active"`
	TotalSIMs   int                      `json:"total_sims"`
}

func (s *Service) GenerateBTKReport(ctx context.Context, tenantID uuid.UUID) (*BTKReport, error) {
	stats, err := s.complianceStore.BTKMonthlyStats(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("btk stats: %w", err)
	}

	totalActive := 0
	totalSIMs := 0
	for _, st := range stats {
		totalActive += st.ActiveCount
		totalSIMs += st.TotalCount
	}

	now := time.Now().UTC()
	return &BTKReport{
		TenantID:    tenantID,
		ReportMonth: now.AddDate(0, -1, 0).Format("2006-01"),
		GeneratedAt: now.Format(time.RFC3339),
		Operators:   stats,
		TotalActive: totalActive,
		TotalSIMs:   totalSIMs,
	}, nil
}

func (s *Service) ExportBTKReportCSV(ctx context.Context, tenantID uuid.UUID) ([]byte, error) {
	report, err := s.GenerateBTKReport(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	w.Write([]string{"BTK Monthly SIM Report"})
	w.Write([]string{"Tenant ID", report.TenantID.String()})
	w.Write([]string{"Report Month", report.ReportMonth})
	w.Write([]string{"Generated At", report.GeneratedAt})
	w.Write([]string{""})
	w.Write([]string{"Operator", "Code", "Active", "Suspended", "Terminated", "Total"})

	for _, op := range report.Operators {
		w.Write([]string{
			op.OperatorName,
			op.OperatorCode,
			fmt.Sprintf("%d", op.ActiveCount),
			fmt.Sprintf("%d", op.SuspendedCount),
			fmt.Sprintf("%d", op.TerminatedCount),
			fmt.Sprintf("%d", op.TotalCount),
		})
	}

	w.Write([]string{""})
	w.Write([]string{"Total Active", fmt.Sprintf("%d", report.TotalActive)})
	w.Write([]string{"Total SIMs", fmt.Sprintf("%d", report.TotalSIMs)})

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}

	return buf.Bytes(), nil
}

func (s *Service) UpdateRetention(ctx context.Context, tenantID uuid.UUID, days int) error {
	if days < 30 || days > 365 {
		return fmt.Errorf("retention days must be between 30 and 365")
	}
	return s.complianceStore.UpdateRetentionDays(ctx, tenantID, days)
}

func (s *Service) createPurgeAuditEntry(ctx context.Context, tenantID uuid.UUID, simID uuid.UUID) {
	afterData, _ := json.Marshal(map[string]string{
		"state":  "purged",
		"reason": "auto-purge: retention period expired",
	})
	_, err := s.auditSvc.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:   tenantID,
		Action:     "purge",
		EntityType: "sim",
		EntityID:   simID.String(),
		AfterData:  afterData,
	})
	if err != nil {
		s.logger.Error().Err(err).
			Str("sim_id", simID.String()).
			Msg("failed to create purge audit entry")
	}
}

func deriveTenantSalt(tenantID uuid.UUID) string {
	h := sha256.Sum256([]byte("argus-compliance-salt:" + tenantID.String()))
	return hex.EncodeToString(h[:16])
}

func uniqueTenantIDs(sims []store.PurgableSIM) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	var result []uuid.UUID
	for _, s := range sims {
		if !seen[s.TenantID] {
			seen[s.TenantID] = true
			result = append(result, s.TenantID)
		}
	}
	return result
}
