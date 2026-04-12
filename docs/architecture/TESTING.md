# Testing Strategy — Argus

> Testing philosophy: Test behavior, not implementation. Every feature story has an acceptance criteria section that maps directly to test cases.

## Test Stack

| Layer | Tool | Package/Config | Purpose |
|-------|------|----------------|---------|
| Go Unit | `testing` + `testify` | `go test ./...` | Pure logic: parsers, evaluators, models, utilities |
| Go Integration | `testcontainers-go` | PostgreSQL, Redis, NATS containers | Store layer, cache layer, bus layer, full middleware chain |
| Go Benchmark | `testing.B` | `go test -bench=.` | AAA hot path, policy evaluation, IP allocation |
| Frontend Unit | Vitest + Testing Library | `web/vitest.config.ts` | Components, hooks, stores, utilities |
| Frontend Integration | Vitest + MSW | Mock Service Worker | API layer, auth flow, data fetching |
| E2E | Playwright | `e2e/playwright.config.ts` | Full user flows via browser |

## Test Directory Structure

```
argus/
├── internal/
│   ├── aaa/
│   │   ├── radius/
│   │   │   ├── server.go
│   │   │   └── server_test.go              # Unit tests
│   │   └── session/
│   │       ├── manager.go
│   │       └── manager_test.go
│   ├── policy/
│   │   ├── dsl/
│   │   │   ├── parser.go
│   │   │   ├── parser_test.go              # Unit tests (grammar, tokenizer)
│   │   │   └── parser_benchmark_test.go    # Benchmark tests
│   │   └── evaluator/
│   │       ├── evaluator.go
│   │       ├── evaluator_test.go
│   │       └── evaluator_benchmark_test.go
│   ├── store/
│   │   ├── sim_store.go
│   │   └── sim_store_test.go               # Integration tests (testcontainers)
│   └── ...
├── testdata/                                # Shared test fixtures
│   ├── policies/
│   │   ├── valid_basic.dsl
│   │   ├── valid_complex.dsl
│   │   ├── invalid_syntax.dsl
│   │   └── invalid_semantic.dsl
│   ├── csv/
│   │   ├── bulk_import_valid.csv
│   │   ├── bulk_import_invalid_rows.csv
│   │   └── bulk_import_large_10k.csv
│   ├── radius/
│   │   ├── access_request.bin              # Captured RADIUS packets
│   │   ├── accounting_start.bin
│   │   └── accounting_stop.bin
│   └── fixtures/
│       ├── tenant.go                       # Test tenant factory
│       ├── user.go                         # Test user factory
│       ├── sim.go                          # Test SIM factory
│       ├── operator.go                     # Test operator factory
│       └── seed.sql                        # SQL seed for integration tests
├── web/
│   └── src/
│       ├── components/
│       │   ├── atoms/
│       │   │   └── Button.test.tsx
│       │   └── organisms/
│       │       └── SimTable.test.tsx
│       ├── hooks/
│       │   └── useAuth.test.ts
│       └── api/
│           └── simApi.test.ts
└── e2e/
    ├── playwright.config.ts
    ├── auth.spec.ts
    ├── sim-management.spec.ts
    ├── policy-editor.spec.ts
    └── fixtures/
        └── auth.ts                          # Auth helper for Playwright
```

## Test Naming Convention

```
TestXxx_WhenCondition_ShouldResult
```

Examples:
```go
func TestSIMStore_WhenICCIDAlreadyExists_ShouldReturnICCIDExistsError(t *testing.T) { ... }
func TestPolicyParser_WhenMissingMatchBlock_ShouldReturnSyntaxError(t *testing.T) { ... }
func TestRADIUSServer_WhenSIMIsSuspended_ShouldReturnAccessReject(t *testing.T) { ... }
func TestRateLimiter_WhenLimitExceeded_ShouldReturn429WithRetryAfter(t *testing.T) { ... }
func TestAuditHashChain_WhenVerified_ShouldMatchRecomputedHashes(t *testing.T) { ... }
```

Frontend:
```typescript
describe('SimTable', () => {
  it('when filter applied, should show only matching SIMs', () => { ... })
  it('when row selected, should show bulk action bar', () => { ... })
  it('when page changes, should fetch next cursor', () => { ... })
})
```

## Coverage Targets

| Package | Target | Rationale |
|---------|--------|-----------|
| `internal/aaa/*` | 80% | Core business logic, high-risk |
| `internal/policy/*` | 80% | DSL parser correctness critical |
| `internal/store/*` | 80% | Data integrity |
| `internal/auth/*` | 80% | Security-critical |
| `internal/api/*` | 60% | Handler logic (mostly delegation) |
| `internal/gateway/*` | 60% | Middleware (integration-heavy) |
| `internal/ws/*` | 50% | WebSocket (hard to unit test) |
| `internal/notification/*` | 50% | External integrations |
| `internal/operator/*` | 70% | Routing and failover logic |
| `web/src/components/*` | 60% | UI components |
| `web/src/hooks/*` | 70% | Custom hooks |
| `web/src/api/*` | 70% | API layer |

Run coverage: `go test -coverprofile=coverage.out ./internal/... && go tool cover -html=coverage.out`

## Unit Tests

### Go Unit Test Guidelines

- **No database, no Redis, no NATS** in unit tests.
- Use interfaces and dependency injection. Every service depends on interfaces, not concrete implementations.
- Use `testify/assert` for assertions, `testify/require` for fatal preconditions.
- Use `testify/mock` for mock generation.
- Table-driven tests for functions with multiple input/output combinations.

```go
func TestPolicyEvaluator_WhenUsageExceedsThreshold_ShouldReturnThrottleAction(t *testing.T) {
    tests := []struct {
        name        string
        usageBytes  int64
        expectedBW  int64
        expectThrottle bool
    }{
        {"below threshold", 500 * 1024 * 1024, 1048576, false},
        {"above 1GB threshold", 1100 * 1024 * 1024, 65536, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            eval := policy.NewEvaluator(compiledRules)
            result := eval.Evaluate(context.Background(), &policy.SessionContext{
                UsageBytes: tt.usageBytes,
                RATType:    "lte_m",
            })
            assert.Equal(t, tt.expectedBW, result.BandwidthDown)
            if tt.expectThrottle {
                require.Contains(t, result.Actions, policy.ActionThrottle)
            }
        })
    }
}
```

### Frontend Unit Test Guidelines

- Use Vitest (not Jest) with `@testing-library/react`.
- Test user-visible behavior, not implementation details.
- Mock API calls with MSW (Mock Service Worker), not `jest.mock`.
- Avoid testing CSS/styling; test functionality and accessibility.

```typescript
import { render, screen, fireEvent } from '@testing-library/react'
import { SimTable } from './SimTable'

describe('SimTable', () => {
  it('when checkbox clicked, should show bulk action bar', async () => {
    render(<SimTable sims={mockSims} />)

    const checkbox = screen.getAllByRole('checkbox')[1]
    fireEvent.click(checkbox)

    expect(screen.getByText('Bulk Actions (1 selected)')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Activate' })).toBeInTheDocument()
  })
})
```

## Integration Tests

### testcontainers-go Setup

Integration tests spin up real PostgreSQL, Redis, and NATS containers. Each test function gets a fresh database state.

```go
func TestSIMStore_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    ctx := context.Background()

    // Start containers
    pgContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "timescale/timescaledb:latest-pg16",
            ExposedPorts: []string{"5432/tcp"},
            Env: map[string]string{
                "POSTGRES_DB":       "argus_test",
                "POSTGRES_USER":     "argus",
                "POSTGRES_PASSWORD": "test",
            },
            WaitingFor: wait.ForLog("database system is ready to accept connections"),
        },
        Started: true,
    })
    require.NoError(t, err)
    defer pgContainer.Terminate(ctx)

    // Run migrations
    dsn := fmt.Sprintf("postgres://argus:test@%s/argus_test?sslmode=disable", pgContainer.Endpoint(ctx, "5432"))
    runMigrations(t, dsn)

    // Create store
    store := store.NewSIMStore(pgPool)

    t.Run("WhenCreateSIM_ShouldPersistAndReturn", func(t *testing.T) {
        sim := fixtures.NewSIM(fixtures.WithTenantID(testTenantID))
        created, err := store.Create(ctx, sim)
        require.NoError(t, err)
        assert.Equal(t, sim.ICCID, created.ICCID)
    })

    t.Run("WhenDuplicateICCID_ShouldReturnICCIDExistsError", func(t *testing.T) {
        sim := fixtures.NewSIM(fixtures.WithICCID("duplicate-iccid"))
        _, _ = store.Create(ctx, sim)
        _, err := store.Create(ctx, sim)
        assert.ErrorIs(t, err, errors.ErrICCIDExists)
    })
}
```

### Running Integration Tests

```bash
# All tests (unit + integration)
go test ./...

# Unit tests only (skip integration)
go test -short ./...

# Integration tests only (name-based)
go test -run Integration ./internal/store/...
go test -run Integration ./internal/cache/...
go test -run Integration ./internal/bus/...

# Specific package with verbose output
go test -v -count=1 ./internal/policy/dsl/...
```

### Build-Tag-Gated Integration Tests

Some integration tests require external infrastructure (OTel Collector, Prometheus, in-memory exporters) and are gated behind the `integration` build tag to avoid running in standard CI. These tests are **not** skipped by `-short` — they must be explicitly opted in with `-tags integration`.

```bash
# Run build-tag-gated integration tests
go test -tags integration ./internal/observability/...

# Run with race detector (recommended)
go test -tags integration -race ./internal/observability/...
```

Gate pattern used in test files:
```go
//go:build integration

package observability_test

// Tests use tracetest.InMemoryExporter + sdktrace.NewSimpleSpanProcessor
// to assert span attributes without requiring a live OTLP endpoint.
```

Files using this pattern (as of STORY-065):
- `internal/observability/integration_test.go` — end-to-end trace + metrics + correlation_id propagation (19 tests)

## Benchmark Tests

### AAA Hot Path Benchmarks (STORY-052)

```go
func BenchmarkRADIUSAuthRequest(b *testing.B) {
    // Setup: pre-populate Redis with 10K SIMs and policies
    setupBenchmarkData(b)

    packet := buildAccessRequest(b, "286010123456789", "test-nas")

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        resp, err := server.HandleAccessRequest(context.Background(), packet)
        if err != nil {
            b.Fatal(err)
        }
        if resp.Code != radius.CodeAccessAccept {
            b.Fatalf("expected accept, got %v", resp.Code)
        }
    }
}

func BenchmarkPolicyEvaluation(b *testing.B) {
    compiled := loadCompiledPolicy(b, "testdata/policies/valid_complex.dsl")
    eval := policy.NewEvaluator(compiled)
    sessionCtx := &policy.SessionContext{
        IMSI:       "286010123456789",
        APN:        "iot.fleet",
        RATType:    "lte_m",
        UsageBytes: 750 * 1024 * 1024,
    }

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        _ = eval.Evaluate(context.Background(), sessionCtx)
    }
}

func BenchmarkIPAddressAllocation(b *testing.B) {
    // Setup: pool with 65534 addresses, 50% utilized
    setupIPPool(b, "10.0.0.0/16", 0.5)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := ipStore.Allocate(context.Background(), poolID, simID)
        if err != nil {
            b.Fatal(err)
        }
        // Release for next iteration
        _ = ipStore.Release(context.Background(), poolID, simID)
    }
}
```

### Benchmark Targets (p99)

| Operation | Target | Measured By |
|-----------|--------|-------------|
| RADIUS auth (cache hit) | < 1ms | `BenchmarkRADIUSAuthRequest` |
| RADIUS auth (cache miss, DB) | < 10ms | `BenchmarkRADIUSAuthRequest_CacheMiss` |
| Policy evaluation | < 0.5ms | `BenchmarkPolicyEvaluation` |
| IP allocation | < 2ms | `BenchmarkIPAddressAllocation` |
| Session lookup (Redis) | < 0.5ms | `BenchmarkSessionLookup` |

Run benchmarks:
```bash
go test -bench=. -benchmem -count=5 ./internal/aaa/... ./internal/policy/... ./internal/store/...
```

## Mock Operator Adapter

All tests use the mock operator adapter (`internal/operator/mock/`). No real operator connections in CI.

```go
type MockAdapter struct {
    AuthResponses     map[string]AuthResult  // IMSI → result
    HealthStatus      string                 // "healthy", "degraded", "down"
    LatencyMS         int                    // Simulated latency
    FailAfterN        int                    // Fail after N successful requests (circuit breaker testing)
    requestCount      int
}

func (m *MockAdapter) Authenticate(ctx context.Context, req AuthRequest) (*AuthResult, error) {
    m.requestCount++
    if m.FailAfterN > 0 && m.requestCount > m.FailAfterN {
        return nil, ErrOperatorDown
    }
    time.Sleep(time.Duration(m.LatencyMS) * time.Millisecond)
    if result, ok := m.AuthResponses[req.IMSI]; ok {
        return &result, nil
    }
    return &AuthResult{Code: AuthReject, Reason: "Unknown IMSI"}, nil
}
```

The mock adapter is registered as `operator_mock` in the adapter registry and is the default for all test tenants.

## E2E Tests (Playwright)

### Setup

E2E tests run against a full Docker Compose stack (`deploy/docker-compose.yml`) with the mock operator adapter.

```typescript
// e2e/playwright.config.ts
export default defineConfig({
  testDir: './e2e',
  baseURL: 'http://localhost:8084',
  use: {
    ignoreHTTPSErrors: false,
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: {
    command: 'docker compose -f deploy/docker-compose.yml up -d',
    url: 'http://localhost:8084/api/health',
    reuseExistingServer: true,
    timeout: 120000,
  },
})
```

### Auth Helper

```typescript
// e2e/fixtures/auth.ts
export async function loginAsAdmin(page: Page) {
  await page.goto('/login')
  await page.fill('[name="email"]', 'admin@test.argus.io')
  await page.fill('[name="password"]', 'TestPassword123!')
  await page.click('button[type="submit"]')
  await page.waitForURL('/')
}
```

### E2E Test Example

```typescript
// e2e/sim-management.spec.ts
import { test, expect } from '@playwright/test'
import { loginAsAdmin } from './fixtures/auth'

test.describe('SIM Management', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
  })

  test('when bulk import CSV, should show progress and results', async ({ page }) => {
    await page.goto('/sims')
    await page.click('text=Import SIMs')

    const fileChooserPromise = page.waitForEvent('filechooser')
    await page.click('text=Upload CSV')
    const fileChooser = await fileChooserPromise
    await fileChooser.setFiles('e2e/testdata/bulk_import_10.csv')

    await page.click('text=Start Import')

    // Wait for job completion
    await expect(page.locator('text=Import completed')).toBeVisible({ timeout: 30000 })
    await expect(page.locator('text=10 SIMs imported')).toBeVisible()
  })
})
```

## CI Integration

```makefile
# Makefile targets
test:
	go test -short -count=1 ./...

test-integration:
	go test -count=1 -tags=integration ./...

test-coverage:
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -func=coverage.out

test-benchmark:
	go test -bench=. -benchmem -count=3 ./internal/aaa/... ./internal/policy/...

test-frontend:
	cd web && npm run test

test-e2e:
	cd e2e && npx playwright test

test-all: test test-integration test-frontend test-e2e

lint-sql:
	@grep -rn "SELECT \*" internal/store/ && exit 1 || echo "lint-sql: PASS"
```

`make lint-sql` is a CI guard (added in STORY-064) that fails the build if any `SELECT *` pattern is found in the store layer (`internal/store/`). Enforces explicit column selection across all DB queries, preventing accidental schema drift exposure and query plan regressions.

## Test Data Fixtures

The `testdata/fixtures/` package provides factory functions for creating test entities with sensible defaults:

```go
// testdata/fixtures/sim.go
func NewSIM(opts ...SIMOption) *model.SIM {
    sim := &model.SIM{
        ID:         uuid.New(),
        TenantID:   DefaultTenantID,
        OperatorID: DefaultOperatorID,
        APNID:      DefaultAPNID,
        ICCID:      fmt.Sprintf("8990111%013d", rand.Int63n(1e13)),
        IMSI:       fmt.Sprintf("28601%010d", rand.Int63n(1e10)),
        State:      "active",
        SIMType:    "physical",
        CreatedAt:  time.Now(),
    }
    for _, opt := range opts {
        opt(sim)
    }
    return sim
}

func WithTenantID(id uuid.UUID) SIMOption {
    return func(s *model.SIM) { s.TenantID = id }
}

func WithState(state string) SIMOption {
    return func(s *model.SIM) { s.State = state }
}

func WithICCID(iccid string) SIMOption {
    return func(s *model.SIM) { s.ICCID = iccid }
}
```
