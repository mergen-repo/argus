package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSyslogDestinationNotFound is returned when a destination row does not
// exist for the given tenant (including cross-tenant attempts).
var ErrSyslogDestinationNotFound = errors.New("store: syslog destination not found")

// SyslogDestination is the read-model for a row in syslog_destinations.
// Nullable columns use pointer types (PAT-009).
type SyslogDestination struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Name              string
	Host              string
	Port              int
	Transport         string
	Format            string
	Facility          int
	SeverityFloor     *int
	FilterCategories  []string
	FilterMinSeverity *int
	TLSCAPEM          *string
	TLSClientCertPEM  *string
	TLSClientKeyPEM   *string
	Enabled           bool
	LastDeliveryAt    *time.Time
	LastError         *string
	CreatedBy         *uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// UpsertSyslogDestinationParams holds inputs for SyslogDestinationStore.Upsert.
type UpsertSyslogDestinationParams struct {
	Name              string
	Host              string
	Port              int
	Transport         string
	Format            string
	Facility          int
	SeverityFloor     *int
	FilterCategories  []string
	FilterMinSeverity *int
	TLSCAPEM          *string
	TLSClientCertPEM  *string
	TLSClientKeyPEM   *string
	Enabled           bool
	CreatedBy         *uuid.UUID
}

// SyslogDestinationStore manages the syslog_destinations table (TBL-61).
type SyslogDestinationStore struct {
	db *pgxpool.Pool
}

// NewSyslogDestinationStore constructs a SyslogDestinationStore.
func NewSyslogDestinationStore(db *pgxpool.Pool) *SyslogDestinationStore {
	return &SyslogDestinationStore{db: db}
}

const syslogDestCols = `id, tenant_id, name, host, port, transport, format,
	facility, severity_floor, filter_categories, filter_min_severity,
	tls_ca_pem, tls_client_cert_pem, tls_client_key_pem,
	enabled, last_delivery_at, last_error, created_by, created_at, updated_at`

func scanSyslogDestination(row pgx.Row) (*SyslogDestination, error) {
	d := &SyslogDestination{}
	err := row.Scan(
		&d.ID, &d.TenantID, &d.Name, &d.Host, &d.Port,
		&d.Transport, &d.Format,
		&d.Facility, &d.SeverityFloor, &d.FilterCategories, &d.FilterMinSeverity,
		&d.TLSCAPEM, &d.TLSClientCertPEM, &d.TLSClientKeyPEM,
		&d.Enabled, &d.LastDeliveryAt, &d.LastError,
		&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if d.FilterCategories == nil {
		d.FilterCategories = []string{}
	}
	return d, nil
}

// ListAllEnabled returns every enabled destination across all tenants ordered
// by (tenant_id, name). Used exclusively by the syslog Forwarder (STORY-098
// Task 5) to refresh its per-destination worker roster — bypasses tenant
// scoping by design because the forwarder is a singleton background worker
// pool that fans out events to every tenant's configured destinations.
//
// DO NOT call this from request-scoped handlers; use List(ctx, tenantID).
func (s *SyslogDestinationStore) ListAllEnabled(ctx context.Context) ([]SyslogDestination, error) {
	query := fmt.Sprintf(`SELECT %s FROM syslog_destinations
		WHERE enabled = TRUE
		ORDER BY tenant_id ASC, name ASC`, syslogDestCols)

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: list all enabled syslog destinations: %w", err)
	}
	defer rows.Close()

	var results []SyslogDestination
	for rows.Next() {
		d, err := scanSyslogDestination(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan syslog destination: %w", err)
		}
		results = append(results, *d)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("store: list all enabled syslog destinations rows: %w", rows.Err())
	}
	if results == nil {
		results = []SyslogDestination{}
	}
	return results, nil
}

// List returns all destinations for the tenant ordered by name.
func (s *SyslogDestinationStore) List(ctx context.Context, tenantID uuid.UUID) ([]SyslogDestination, error) {
	query := fmt.Sprintf(`SELECT %s FROM syslog_destinations
		WHERE tenant_id = $1
		ORDER BY name ASC`, syslogDestCols)

	rows, err := s.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: list syslog destinations: %w", err)
	}
	defer rows.Close()

	var results []SyslogDestination
	for rows.Next() {
		d, err := scanSyslogDestination(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan syslog destination: %w", err)
		}
		results = append(results, *d)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("store: list syslog destinations rows: %w", rows.Err())
	}
	if results == nil {
		results = []SyslogDestination{}
	}
	return results, nil
}

// UpsertResult carries the returned row plus a flag indicating whether this
// was an insert (true) or an update (false). Used by the handler to pick the
// correct audit action.
type UpsertSyslogDestinationResult struct {
	Destination SyslogDestination
	Inserted    bool
}

// Upsert inserts or updates a destination identified by (tenant_id, name).
// The bool in the result is true when a new row was inserted.
func (s *SyslogDestinationStore) Upsert(ctx context.Context, tenantID uuid.UUID, p UpsertSyslogDestinationParams) (*UpsertSyslogDestinationResult, error) {
	categories := p.FilterCategories
	if categories == nil {
		categories = []string{}
	}

	query := fmt.Sprintf(`
		INSERT INTO syslog_destinations
		  (tenant_id, name, host, port, transport, format,
		   facility, severity_floor, filter_categories, filter_min_severity,
		   tls_ca_pem, tls_client_cert_pem, tls_client_key_pem,
		   enabled, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (tenant_id, name) DO UPDATE SET
		  host               = EXCLUDED.host,
		  port               = EXCLUDED.port,
		  transport          = EXCLUDED.transport,
		  format             = EXCLUDED.format,
		  facility           = EXCLUDED.facility,
		  severity_floor     = EXCLUDED.severity_floor,
		  filter_categories  = EXCLUDED.filter_categories,
		  filter_min_severity= EXCLUDED.filter_min_severity,
		  tls_ca_pem         = EXCLUDED.tls_ca_pem,
		  tls_client_cert_pem= EXCLUDED.tls_client_cert_pem,
		  tls_client_key_pem = EXCLUDED.tls_client_key_pem,
		  enabled            = EXCLUDED.enabled,
		  updated_at         = NOW()
		RETURNING %s, (xmax = 0) AS inserted`, syslogDestCols)

	var d SyslogDestination
	var inserted bool
	err := s.db.QueryRow(ctx, query,
		tenantID, p.Name, p.Host, p.Port, p.Transport, p.Format,
		p.Facility, p.SeverityFloor, categories, p.FilterMinSeverity,
		p.TLSCAPEM, p.TLSClientCertPEM, p.TLSClientKeyPEM,
		p.Enabled, p.CreatedBy,
	).Scan(
		&d.ID, &d.TenantID, &d.Name, &d.Host, &d.Port,
		&d.Transport, &d.Format,
		&d.Facility, &d.SeverityFloor, &d.FilterCategories, &d.FilterMinSeverity,
		&d.TLSCAPEM, &d.TLSClientCertPEM, &d.TLSClientKeyPEM,
		&d.Enabled, &d.LastDeliveryAt, &d.LastError,
		&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
		&inserted,
	)
	if err != nil {
		return nil, fmt.Errorf("store: upsert syslog destination: %w", err)
	}
	if d.FilterCategories == nil {
		d.FilterCategories = []string{}
	}
	return &UpsertSyslogDestinationResult{Destination: d, Inserted: inserted}, nil
}

// SetEnabled toggles the enabled column. Returns ErrSyslogDestinationNotFound
// when the row does not exist for the tenant. Returns the updated row.
func (s *SyslogDestinationStore) SetEnabled(ctx context.Context, tenantID uuid.UUID, id uuid.UUID, enabled bool) (*SyslogDestination, error) {
	query := fmt.Sprintf(`UPDATE syslog_destinations
		SET enabled = $1, updated_at = NOW()
		WHERE id = $2 AND tenant_id = $3
		RETURNING %s`, syslogDestCols)

	d, err := scanSyslogDestination(s.db.QueryRow(ctx, query, enabled, id, tenantID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSyslogDestinationNotFound
		}
		return nil, fmt.Errorf("store: set syslog destination enabled: %w", err)
	}
	return d, nil
}

// Delete removes a destination. Returns ErrSyslogDestinationNotFound on
// cross-tenant or missing row.
func (s *SyslogDestinationStore) Delete(ctx context.Context, tenantID uuid.UUID, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM syslog_destinations WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("store: delete syslog destination: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSyslogDestinationNotFound
	}
	return nil
}

// UpdateDelivery atomically records the outcome of a delivery attempt.
// On success it sets last_delivery_at = NOW() and clears last_error.
// On failure it stores the (truncated) error message and also updates last_delivery_at.
func (s *SyslogDestinationStore) UpdateDelivery(ctx context.Context, tenantID uuid.UUID, id uuid.UUID, success bool, errMsg string) error {
	var query string
	var args []interface{}
	if success {
		query = `UPDATE syslog_destinations
			SET last_delivery_at = NOW(), last_error = NULL, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		// Truncate error to 1 KB.
		if len(errMsg) > 1024 {
			errMsg = errMsg[:1024]
		}
		query = `UPDATE syslog_destinations
			SET last_delivery_at = NOW(), last_error = $1, updated_at = NOW()
			WHERE id = $2 AND tenant_id = $3`
		args = []interface{}{errMsg, id, tenantID}
	}
	_, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store: update syslog delivery: %w", err)
	}
	return nil
}
