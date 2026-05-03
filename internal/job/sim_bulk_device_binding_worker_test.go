package job

// STORY-094 Gate F-A2 regression — bulk worker must NOT clobber existing
// binding_status when only updating binding_mode/bound_imei. SetDeviceBinding
// writes binding_status = $5 unconditionally; passing nil would drop a 'verified'
// row to NULL. The fix calls GetDeviceBinding first and re-passes the existing
// status as statusOverride.
//
// DB-gated: skipped when DATABASE_URL unset / postgres unreachable.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

func bulkBindingTestPool(t *testing.T) *pgxpool.Pool {
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

type bulkBindingFixture struct {
	tenantID   uuid.UUID
	operatorID uuid.UUID
	apnID      uuid.UUID
	simID      uuid.UUID
	iccid      string
}

func seedBulkBindingFixture(t *testing.T, pool *pgxpool.Pool) bulkBindingFixture {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000

	var fix bulkBindingFixture
	fix.operatorID = uuid.MustParse("00000000-0000-0000-0000-000000000100")

	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, contact_email) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("bulk-bind-test-%d", nonce), fmt.Sprintf("bb%d@test.invalid", nonce),
	).Scan(&fix.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	if err := pool.QueryRow(ctx,
		`INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		 VALUES ($1, $2, $3, 'BB APN', 'iot', 'active') RETURNING id`,
		fix.tenantID, fix.operatorID,
		fmt.Sprintf("bulk-bind-apn-%d", nonce),
	).Scan(&fix.apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	fix.iccid = fmt.Sprintf("8992541%012d", nonce%1_000_000_000)
	if len(fix.iccid) > 22 {
		fix.iccid = fix.iccid[:22]
	}
	imsi := fmt.Sprintf("28602%010d", nonce%1_000_000_000)
	if len(imsi) > 15 {
		imsi = imsi[:15]
	}

	if err := pool.QueryRow(ctx,
		`INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		 VALUES ($1, $2, $3, $4, $5, 'physical', 'active') RETURNING id`,
		fix.tenantID, fix.operatorID, fix.apnID, fix.iccid, imsi,
	).Scan(&fix.simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM imei_history WHERE sim_id = $1`, fix.simID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, fix.simID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, fix.apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, fix.tenantID)
	})
	return fix
}

// TestBulkDeviceBindings_PreservesExistingBindingStatus is the F-A2 regression
// guard. Pre-seeds binding_status='verified' on a SIM, runs processRow with a
// new binding_mode, asserts binding_status remains 'verified' (NOT dropped to
// NULL by the bulk worker's SetDeviceBinding call).
func TestBulkDeviceBindings_PreservesExistingBindingStatus(t *testing.T) {
	pool := bulkBindingTestPool(t)
	fix := seedBulkBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	ctx := context.Background()

	// Pre-seed: binding_mode='strict', bound_imei='490154203237518', binding_status='verified'.
	preMode := "strict"
	preIMEI := "490154203237518"
	preStatus := "verified"
	if _, err := simStore.SetDeviceBinding(ctx, fix.tenantID, fix.simID, &preMode, &preIMEI, &preStatus); err != nil {
		t.Fatalf("pre-seed binding: %v", err)
	}

	// Verify pre-seed worked.
	pre, err := simStore.GetDeviceBinding(ctx, fix.tenantID, fix.simID)
	if err != nil {
		t.Fatalf("get pre-seed: %v", err)
	}
	if pre.BindingStatus == nil || *pre.BindingStatus != "verified" {
		t.Fatalf("pre-seed binding_status = %v, want verified", pre.BindingStatus)
	}

	// Run processRow with a binding_mode change. Auditor is nil — we only need
	// to validate that binding_status is preserved through the worker write.
	p := &BulkDeviceBindingsProcessor{
		sims:   simStore,
		logger: zerolog.Nop(),
	}
	j := &store.Job{
		ID:       uuid.New(),
		TenantID: fix.tenantID,
	}
	row := DeviceBindingsBulkRowSpec{
		ICCID:       fix.iccid,
		BoundIMEI:   "490154203237518",
		BindingMode: "first-use", // changing mode only
	}

	outcome, errMsg := p.processRow(ctx, j, row)
	if outcome != "success" {
		t.Fatalf("processRow outcome = %q (%q), want success", outcome, errMsg)
	}

	// Critical assertion: binding_status MUST still be 'verified', not NULL.
	post, err := simStore.GetDeviceBinding(ctx, fix.tenantID, fix.simID)
	if err != nil {
		t.Fatalf("get post-update: %v", err)
	}
	if post.BindingStatus == nil {
		t.Fatalf("F-A2 regression: binding_status was clobbered to NULL by bulk worker")
	}
	if *post.BindingStatus != "verified" {
		t.Errorf("binding_status = %q, want verified (preserved)", *post.BindingStatus)
	}
	if post.BindingMode == nil || *post.BindingMode != "first-use" {
		t.Errorf("binding_mode = %v, want first-use", post.BindingMode)
	}
}

// TestBulkDeviceBindings_NullStatusStaysNull confirms that when the existing
// binding_status is NULL, the bulk worker leaves it NULL (no spurious writes).
func TestBulkDeviceBindings_NullStatusStaysNull(t *testing.T) {
	pool := bulkBindingTestPool(t)
	fix := seedBulkBindingFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	ctx := context.Background()

	// Pre-seed binding_mode only, binding_status remains NULL.
	preMode := "strict"
	if _, err := simStore.SetDeviceBinding(ctx, fix.tenantID, fix.simID, &preMode, nil, nil); err != nil {
		t.Fatalf("pre-seed: %v", err)
	}

	p := &BulkDeviceBindingsProcessor{
		sims:   simStore,
		logger: zerolog.Nop(),
	}
	j := &store.Job{
		ID:       uuid.New(),
		TenantID: fix.tenantID,
	}
	row := DeviceBindingsBulkRowSpec{
		ICCID:       fix.iccid,
		BoundIMEI:   "490154203237518",
		BindingMode: "soft",
	}
	outcome, errMsg := p.processRow(ctx, j, row)
	if outcome != "success" {
		t.Fatalf("processRow outcome = %q (%q), want success", outcome, errMsg)
	}

	post, err := simStore.GetDeviceBinding(ctx, fix.tenantID, fix.simID)
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if post.BindingStatus != nil {
		t.Errorf("binding_status = %v, want nil (was nil pre-update)", post.BindingStatus)
	}
}
