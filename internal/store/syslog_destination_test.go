package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/notification/syslog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── PAT-022 structural tests ──────────────────────────────────────────────────

const syslogDestMigrationFilename = "20260509000001_syslog_destinations.up.sql"

func loadSyslogDestMigration(t *testing.T) string {
	t.Helper()
	path := filepath.Join(migrationsDir(t), syslogDestMigrationFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// extractEnumValuesFromCheckIn parses `<column> IN ('a','b',...)` patterns in
// the SQL and returns the union of all such tuples for the named column.
func extractEnumValuesFromCheckIn(sql, column string) []string {
	pattern := column + `\s+IN\s*\(([^)]*)\)`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(sql, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var values []string
	for _, m := range matches {
		for _, p := range strings.Split(m[1], ",") {
			v := strings.TrimSpace(p)
			v = strings.Trim(v, "'\"")
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			values = append(values, v)
		}
	}
	return values
}

// TestSyslogTransportConstSetMatchesCheckConstraint enforces PAT-022 for transport.
func TestSyslogTransportConstSetMatchesCheckConstraint(t *testing.T) {
	sql := loadSyslogDestMigration(t)
	sqlValues := extractEnumValuesFromCheckIn(sql, "transport")
	if len(sqlValues) == 0 {
		t.Fatalf("could not extract transport IN (...) tuple from migration")
	}
	assertSetEqual(t, "syslog_destinations.transport", syslog.Transports, sqlValues)
}

// TestSyslogFormatConstSetMatchesCheckConstraint enforces PAT-022 for format.
func TestSyslogFormatConstSetMatchesCheckConstraint(t *testing.T) {
	sql := loadSyslogDestMigration(t)
	sqlValues := extractEnumValuesFromCheckIn(sql, "format")
	if len(sqlValues) == 0 {
		t.Fatalf("could not extract format IN (...) tuple from migration")
	}
	assertSetEqual(t, "syslog_destinations.format", syslog.Formats, sqlValues)
}

// ── no-DB SKIP helpers ────────────────────────────────────────────────────────

func testSyslogDestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func setupSyslogDestTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, slug, plan) VALUES ($1, $2, $3, 'free')`,
		id, "syslog-test-"+id.String()[:8], "syslog-slug-"+id.String()[:8],
	)
	if err != nil {
		t.Fatalf("setupSyslogDestTenant: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM syslog_destinations WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func defaultUpsertParams() UpsertSyslogDestinationParams {
	return UpsertSyslogDestinationParams{
		Name:             "test-dest",
		Host:             "127.0.0.1",
		Port:             514,
		Transport:        "udp",
		Format:           "rfc3164",
		Facility:         1,
		FilterCategories: []string{"auth"},
		Enabled:          true,
	}
}

// ── store tests ───────────────────────────────────────────────────────────────

func TestSyslogDestinationStore_UpsertList(t *testing.T) {
	pool := testSyslogDestPool(t)
	ctx := context.Background()
	s := NewSyslogDestinationStore(pool)
	tenantID := setupSyslogDestTenant(t, pool)

	p := defaultUpsertParams()
	res, err := s.Upsert(ctx, tenantID, p)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !res.Inserted {
		t.Fatal("expected Inserted=true for first insert")
	}
	if res.Destination.Name != p.Name || res.Destination.Host != p.Host {
		t.Fatalf("unexpected destination: %+v", res.Destination)
	}

	list, err := s.List(ctx, tenantID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != res.Destination.ID {
		t.Fatalf("List returned wrong rows: %+v", list)
	}
}

func TestSyslogDestinationStore_UpsertConflict_Updates(t *testing.T) {
	pool := testSyslogDestPool(t)
	ctx := context.Background()
	s := NewSyslogDestinationStore(pool)
	tenantID := setupSyslogDestTenant(t, pool)

	p := defaultUpsertParams()
	first, err := s.Upsert(ctx, tenantID, p)
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if !first.Inserted {
		t.Fatal("expected first Inserted=true")
	}

	p.Host = "10.0.0.1"
	p.Port = 5514
	second, err := s.Upsert(ctx, tenantID, p)
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	if second.Inserted {
		t.Fatal("expected second Inserted=false (update)")
	}
	if second.Destination.ID != first.Destination.ID {
		t.Fatal("upsert must not change ID on conflict")
	}
	if second.Destination.Host != "10.0.0.1" || second.Destination.Port != 5514 {
		t.Fatalf("updated fields not reflected: %+v", second.Destination)
	}
}

func TestSyslogDestinationStore_SetEnabled(t *testing.T) {
	pool := testSyslogDestPool(t)
	ctx := context.Background()
	s := NewSyslogDestinationStore(pool)
	tenantID := setupSyslogDestTenant(t, pool)

	res, err := s.Upsert(ctx, tenantID, defaultUpsertParams())
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	id := res.Destination.ID

	updated, err := s.SetEnabled(ctx, tenantID, id, false)
	if err != nil {
		t.Fatalf("SetEnabled false: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected enabled=false after SetEnabled(false)")
	}

	updated2, err := s.SetEnabled(ctx, tenantID, id, true)
	if err != nil {
		t.Fatalf("SetEnabled true: %v", err)
	}
	if !updated2.Enabled {
		t.Fatal("expected enabled=true after SetEnabled(true)")
	}
}

func TestSyslogDestinationStore_Delete(t *testing.T) {
	pool := testSyslogDestPool(t)
	ctx := context.Background()
	s := NewSyslogDestinationStore(pool)
	tenantID := setupSyslogDestTenant(t, pool)

	res, err := s.Upsert(ctx, tenantID, defaultUpsertParams())
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.Delete(ctx, tenantID, res.Destination.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, err := s.List(ctx, tenantID)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list after delete, got %d rows", len(list))
	}
}

func TestSyslogDestinationStore_Delete_CrossTenant_NotFound(t *testing.T) {
	pool := testSyslogDestPool(t)
	ctx := context.Background()
	s := NewSyslogDestinationStore(pool)
	tenant1 := setupSyslogDestTenant(t, pool)
	tenant2 := setupSyslogDestTenant(t, pool)

	res, err := s.Upsert(ctx, tenant1, defaultUpsertParams())
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	err = s.Delete(ctx, tenant2, res.Destination.ID)
	if !errors.Is(err, ErrSyslogDestinationNotFound) {
		t.Fatalf("expected ErrSyslogDestinationNotFound, got %v", err)
	}
}

func TestSyslogDestinationStore_UpdateDelivery_Success_ClearsLastError(t *testing.T) {
	pool := testSyslogDestPool(t)
	ctx := context.Background()
	s := NewSyslogDestinationStore(pool)
	tenantID := setupSyslogDestTenant(t, pool)

	res, err := s.Upsert(ctx, tenantID, defaultUpsertParams())
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	id := res.Destination.ID

	if err := s.UpdateDelivery(ctx, tenantID, id, false, "connection refused"); err != nil {
		t.Fatalf("UpdateDelivery failure: %v", err)
	}

	if err := s.UpdateDelivery(ctx, tenantID, id, true, ""); err != nil {
		t.Fatalf("UpdateDelivery success: %v", err)
	}

	list, err := s.List(ctx, tenantID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("no rows after UpdateDelivery")
	}
	d := list[0]
	if d.LastError != nil {
		t.Fatalf("expected last_error=NULL after success, got %q", *d.LastError)
	}
	if d.LastDeliveryAt == nil {
		t.Fatal("expected last_delivery_at to be set after success")
	}
}

func TestSyslogDestinationStore_UpdateDelivery_Failure_StoresError(t *testing.T) {
	pool := testSyslogDestPool(t)
	ctx := context.Background()
	s := NewSyslogDestinationStore(pool)
	tenantID := setupSyslogDestTenant(t, pool)

	res, err := s.Upsert(ctx, tenantID, defaultUpsertParams())
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	id := res.Destination.ID

	longErr := strings.Repeat("x", 2000)
	if err := s.UpdateDelivery(ctx, tenantID, id, false, longErr); err != nil {
		t.Fatalf("UpdateDelivery: %v", err)
	}

	list, err := s.List(ctx, tenantID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("no rows")
	}
	d := list[0]
	if d.LastError == nil {
		t.Fatal("expected last_error to be set")
	}
	if len(*d.LastError) > 1024 {
		t.Fatalf("last_error exceeds 1024 bytes: %d", len(*d.LastError))
	}
	if d.LastDeliveryAt == nil {
		t.Fatal("expected last_delivery_at to be set")
	}
	_ = time.Now()
}
